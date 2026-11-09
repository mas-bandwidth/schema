package mootable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE TEXT LIST IN THE TEXT FORM (docs/SPEC-TABLES.md §2.13, §16.2), held to
// internal/tabletext's readTextList and writeTextList: a plain list's element
// is its string in the value model, so the reader places strings and the
// writer spells them; an unbounded element is never clamped; a list past its
// total is out of range, and the one read refuses it whole. Each case is read
// by the Go engine and by moo, and the verdict is the contract (docs/rulings.json
// refusal-verdict-only: a reason name is the reader's own):
//
//	FromJson's verdict (read/refused) == the engine's
//	and, for a text both read:
//	ToJson of what FromJson read      == the engine's text
//	Save of what FromJson read        == the engine's wire
//	ToJson of the engine's wire, read
//	by the one read (ReadStrict)      == the engine's text
const textListSchema = `package probe

table Lists
{
    rules [..4]string(8) | total = 8
    words []string
    repos []string(8) | total = 4
    bases []?string(8)
}

table Sealed | integrity = "crc32c"
{
    id uint32
    names []string(8)
}
`

type textListCase struct{ name, root, text string }

var textListCases = []textListCase{
	{"within", "Lists", `{"rules":["é","schema"],"words":["an unbounded element longer than any bound","",""],"repos":["ab","cd"],"bases":[null,"b",""]}`},
	{"over_total", "Lists", `{"rules":["abcde","fgh","i","j"],"words":[],"repos":["ab","c","de","f"],"bases":[]}`},
	{"sealed", "Sealed", `{"id":9,"names":["a","bc"]}`},
}

func textListUnit(t *testing.T) *ir.Unit {
	t.Helper()
	src := []byte(textListSchema)
	tree, errs := parser.Parse("Probe.schema", src)
	if len(errs) != 0 {
		t.Fatalf("parse: %v", errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: src, AST: tree}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

// textListWant is the engine's answer for one case: the verdict, and for a
// text it reads the canonical text and the wire.
func textListWant(t *testing.T, u *ir.Unit, c textListCase) (verdict, text, wire string) {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(c.root))
	var r tabletext.Report
	if !m.Read(inst, []byte(c.text), &r) {
		return "refused", "", ""
	}
	out, err := m.Write(inst)
	if err != nil {
		t.Fatalf("%s: the engine does not write it: %v", c.name, err)
	}
	// the engine's Encode seals an integrity root with its trailer (§2.14)
	b, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("%s: the engine does not encode it: %v", c.name, err)
	}
	// the canonical text ends in one newline, as every golden does (§16.3)
	return "read", string(out) + "\n", string(b)
}

// textListRun runs every case through moo with runtime overriding runtime
// modules by name, and returns the names of the cases whose answer is not
// the engine's, each with the first difference. A probe that stops is every
// case named.
func textListRun(t *testing.T, runtime map[string][]byte) []string {
	t.Helper()
	u := textListUnit(t)
	dir := generateUnitDir(t, u, runtime)
	var b strings.Builder
	b.WriteString(probePrelude)
	b.WriteString(`local Save: any = require("./table_save")
local Load: any = require("./table_load")
local function text(mod: any, root: string, v: any): string
	local size = mod[root .. "ToJsonMeasure"](v)
	if size < 0 then
		return "write refused"
	end
	local out = buffer.create(size)
	mod[root .. "ToJson"](v, out)
	return buffer.tostring(out)
end
local function run(mod: any, root: string, input: buffer, wire: buffer)
	local d = mod[root .. "Descriptor"]
	local value = mod["New" .. root]()
	local report: any = {}
	local ok = mod[root .. "FromJson"](value, input, report)
	hexs(if ok then "read" else "refused")
	hexs(text(mod, root, value))
	local bytes, why = Save.save(d, value)
	hexs(if bytes ~= nil then buffer.tostring(bytes) else "save refused: " .. tostring(why and why.name))
	local loaded = Load.load(root, wire, Load.newReport())
	hexs(if loaded ~= nil then text(mod, root, loaded) else "load refused")
end
`)
	inputs := map[string][]byte{}
	type want struct{ verdict, text, wire string }
	wants := make([]want, len(textListCases))
	for i, c := range textListCases {
		r, tx, w := textListWant(t, u, c)
		wants[i] = want{r, tx, w}
		inputs[fmt.Sprintf("t%d", i)] = []byte(c.text)
		inputs[fmt.Sprintf("w%d", i)] = []byte(w)
		fmt.Fprintf(&b, "run(require(\"./%s\"), %q, inputs.t%d, inputs.w%d)\n", tableModule(u, c.root), c.root, i, i)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := moorun.RunErr(t, dir, "main.moo", inputs)
	var bad []string
	if err != nil {
		for _, c := range textListCases {
			bad = append(bad, c.name+": the probe stopped: "+firstLine(err.Error()))
		}
		return bad
	}
	lines := decodeHexLines(t, out)
	if len(lines) != 4*len(textListCases) {
		t.Fatalf("the probe printed %d lines for %d cases", len(lines), len(textListCases))
	}
	for i, c := range textListCases {
		got := lines[4*i : 4*i+4]
		w := wants[i]
		switch {
		case got[0] != w.verdict:
			bad = append(bad, fmt.Sprintf("%s: FromJson %s, the engine %s", c.name, got[0], w.verdict))
		case w.verdict == "refused":
			// a refused read lands no value: nothing more to compare
		case got[1] != w.text:
			bad = append(bad, fmt.Sprintf("%s: ToJson of the read value wrote\n%s\nthe engine\n%s", c.name, got[1], w.text))
		case got[2] != w.wire:
			bad = append(bad, fmt.Sprintf("%s: Save of the read value is not the engine's wire (%q)", c.name, firstLine(got[2])))
		case got[3] != w.text:
			bad = append(bad, fmt.Sprintf("%s: ToJson of the engine's wire wrote\n%s\nthe engine\n%s", c.name, got[3], w.text))
		}
	}
	return bad
}

// TestMooJsonTextLists: every case answers as the engine does.
func TestMooJsonTextLists(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated text form)")
	t.Parallel()
	for _, b := range textListRun(t, nil) {
		t.Error(b)
	}
}

// TestMooJsonTextListsNegativeControl breaks each rule the gate holds, one at
// a time, in the runtime the emitter embeds, and requires the gate red on the
// cases that rule decides and green on the rest.
func TestMooJsonTextListsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged text form)")
	t.Parallel()
	controls := []struct {
		name, file, site, broken string
		red                      []string
	}{
		// the writer spells a plain element as the element record
		{"plain_write", "json.moo", "writeString(w :: any, el)\n", "writeString(w :: any, el.Value)\n",
			[]string{"within", "over_total", "sealed"}},
		// the reader places a plain element as the element record
		{"plain_read", "json.moo", "arr[placed + 1] = out\n", "arr[placed + 1] = { Value = out }\n",
			[]string{"within", "over_total", "sealed"}},
		// an unbounded element is clamped at zero bytes
		{"unbounded", "json.moo", "scanStored(r, tl.bound)", "scanStored(r, if tl.bound < 0 then 0 else tl.bound)",
			[]string{"within"}},
		// a list's total is never kept, so a list past it reads
		{"total", "json.moo", "if placed > before and fd.total ~= nil then", "if false then",
			[]string{"over_total"}},
		// the one read is handed the trailer with the file
		{"integrity", "table_load.moo", "\tlocal out = buffer.create(n - 4)\n\tbuffer.copy(out, 0, b, 0, n - 4)\n\treturn out\n", "\treturn b\n",
			[]string{"sealed"}},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := string(Runtime()[c.file])
			if strings.Count(src, c.site) != 1 {
				t.Fatalf("the control's site moved: %s no longer holds %q once", c.file, c.site)
			}
			bad := textListRun(t, map[string][]byte{c.file: []byte(strings.Replace(src, c.site, c.broken, 1))})
			red := map[string]bool{}
			for _, b := range bad {
				name, _, _ := strings.Cut(b, ":")
				red[name] = true
			}
			for _, tc := range textListCases {
				want := false
				for _, r := range c.red {
					want = want || r == tc.name
				}
				if red[tc.name] != want {
					t.Errorf("case %s: red %v, want %v (%v)", tc.name, red[tc.name], want, bad)
				}
			}
		})
	}
}
