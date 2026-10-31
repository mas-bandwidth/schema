package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauJsonTextZeroByteRefused pins docs/SPEC-TABLES.md §16.3: a JSON
// string the Luau FromJson would PLACE in text storage that decodes to
// U+0000 refuses the read, the report is Malformed, and its reason is
// `text_holds_zero: <Table>.<field>` naming the innermost field. The forms
// are the rows of TestGoTextZeroByteTakesOneRuleInEveryForm the Luau leg
// carries: string(8), *string, a nested table's string(8), wstring(8), a
// map[string(8)]int32 key and a map[int32]int32 key, with the zero at the
// front, middle and end. A `\u0001` control in the same place reads clean and
// round-trips, and a zero the read only SKIPS (an unknown key's value, or an
// object key naming no field) is not the event.
//
// Every expected verdict, reason, wire and text comes from schema-go's own
// engine (internal/tabletext and internal/tablewire) over the same unit,
// never typed by hand.
func TestLuauJsonTextZeroByteRefused(t *testing.T) {
	t.Parallel()
	const schemaSrc = `package p

table Inner
{
    n string(8)
}

table Root
{
    s *string
    t string(8)
    w wstring(8)
    i Inner
    m map[string(8)]int32
    k map[int32]int32
}
`
	g := generateFiles(t, map[string]string{"Zero.schema": schemaSrc})

	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "Zero.schema")
	if err := os.WriteFile(schemaPath, []byte(schemaSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Root")
	if st == nil {
		t.Fatal("the Go reference unit has no Root table")
	}

	// every other field is present, so the read value's unread defaults are
	// not what this test measures.
	const base = `{"s":"sv","t":"tv","w":"wv","i":{"n":"nv"},"m":{"ok":1},"k":{"7":1}}`
	forms := []struct {
		name string
		text string
	}{
		{"s", strings.Replace(base, `"s":"sv"`, `"s":"%s"`, 1)},
		{"t", strings.Replace(base, `"t":"tv"`, `"t":"%s"`, 1)},
		{"w", strings.Replace(base, `"w":"wv"`, `"w":"%s"`, 1)},
		{"i", strings.Replace(base, `"i":{"n":"nv"}`, `"i":{"n":"%s"}`, 1)},
		{"m", strings.Replace(base, `"m":{"ok":1}`, `"m":{"ok":1,"%s":2}`, 1)},
		{"k", strings.Replace(base, `"k":{"7":1}`, `"k":{"7":1,"%s":2}`, 1)},
	}
	zeros := []string{`\u0000ab`, `a\u0000b`, `ab\u0000`}

	for _, f := range forms {
		for _, at := range zeros {
			text := strings.Replace(f.text, "%s", at, 1)
			what := f.name + " " + text
			inst := model.New(st)
			var rep tabletext.Report
			if model.Read(inst, []byte(text), &rep) {
				t.Fatalf("%s: the Go reference FromJson accepted a zero (%+v)", what, rep)
			}
			if !rep.Malformed || !strings.HasPrefix(rep.Reason, "text_holds_zero: ") {
				t.Fatalf("%s: Go report %+v is not Malformed text_holds_zero", what, rep)
			}
			got := runJsonTextZeroProbe(t, g, text)
			if got.malformed != rep.Malformed || got.reason != rep.Reason {
				t.Errorf("%s: Luau reports malformed=%v reason=%q, Go reports malformed=%v reason=%q",
					what, got.malformed, got.reason, rep.Malformed, rep.Reason)
			}
		}
		// A map[int32]int32 key's control is no integer, as the Go row
		// says; every other form's control is ordinary text.
		if f.name == "k" {
			continue
		}
		for _, at := range zeros {
			zero := strings.Replace(f.text, "%s", at, 1)
			control := strings.Replace(zero, `\u0000`, `\u0001`, 1)
			what := f.name + " control " + control
			inst := model.New(st)
			var rep tabletext.Report
			if !model.Read(inst, []byte(control), &rep) || rep.Malformed || rep.KindMismatch != 0 || rep.Unknown != 0 {
				t.Fatalf("%s: the Go reference did not read the control clean (%+v)", what, rep)
			}
			wantText, err := model.Write(inst)
			if err != nil {
				t.Fatalf("%s: the Go reference ToJson: %v", what, err)
			}
			wantWire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("%s: the Go reference wire: %v", what, err)
			}
			got := runJsonTextZeroProbe(t, g, control)
			if got.malformed || got.reason != "" {
				t.Errorf("%s: Luau reports malformed=%v reason=%q, want a clean read", what, got.malformed, got.reason)
				continue
			}
			if got.text != hexBytes(append(wantText, '\n')) {
				t.Errorf("%s: Luau ToJson is %s, want %s", what, got.text, hexBytes(append(wantText, '\n')))
			}
			if got.wire != hexBytes(wantWire) {
				t.Errorf("%s: Luau Save is %s, want %s", what, got.wire, hexBytes(wantWire))
			}
		}
	}

	// a zero the read only SKIPS stores nothing and is not the event
	skipped := []string{
		strings.TrimSuffix(base, "}") + `,"zz":"a\u0000b"}`,
		strings.TrimSuffix(base, "}") + `,"t\u0000":"x"}`,
	}
	for _, text := range skipped {
		inst := model.New(st)
		var rep tabletext.Report
		if !model.Read(inst, []byte(text), &rep) || rep.Malformed || rep.Unknown != 1 {
			t.Fatalf("%s: the Go reference did not skip the zero (%+v)", text, rep)
		}
		wantText, err := model.Write(inst)
		if err != nil {
			t.Fatalf("%s: the Go reference ToJson: %v", text, err)
		}
		wantWire, err := tablewire.Encode(model, inst)
		if err != nil {
			t.Fatalf("%s: the Go reference wire: %v", text, err)
		}
		got := runJsonTextZeroProbe(t, g, text)
		if got.malformed || got.reason != "" || got.unknown != rep.Unknown {
			t.Errorf("%s: Luau reports malformed=%v reason=%q unknown=%d, want a clean read with unknown=%d",
				text, got.malformed, got.reason, got.unknown, rep.Unknown)
			continue
		}
		if got.text != hexBytes(append(wantText, '\n')) {
			t.Errorf("%s: Luau ToJson is %s, want %s", text, got.text, hexBytes(append(wantText, '\n')))
		}
		if got.wire != hexBytes(wantWire) {
			t.Errorf("%s: Luau Save is %s, want %s", text, got.wire, hexBytes(wantWire))
		}
	}
}

type jsonTextZeroResult struct {
	malformed bool
	reason    string
	unknown   int
	text      string
	wire      string
}

// runJsonTextZeroProbe reads one text under the emitted json runtime and
// prints the report's verdict, reason and unknown count, then the text and
// the wire it produces from the read value (docs/SPEC-TABLES.md §16.3).
func runJsonTextZeroProbe(t *testing.T, g *generatedUnit, text string) jsonTextZeroResult {
	t.Helper()
	const probe = `local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local r = report()
local v = json.FromJson("Root", inputs.text, r)
print("malformed=" .. tostring(r.malformed))
print("reason=" .. tostring(r.reason or ""))
print("unknown=" .. tostring(r.unknown))
if v == nil then print("text=nil"); print("wire=nil"); return end
print("text=" .. hex(json.ToJson("Root", v)))
print("wire=" .. hex(save.save(St.lookup("Root"), v)))
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonModules(g)+jsonFlags(g)+probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(text)}))
	got := jsonTextZeroResult{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "malformed":
			got.malformed = value == "true"
		case "reason":
			got.reason = value
		case "unknown":
			if _, err := fmt.Sscanf(value, "%d", &got.unknown); err != nil {
				t.Fatalf("%s: unknown line %q: %v", text, line, err)
			}
		case "text":
			got.text = value
		case "wire":
			got.wire = value
		}
	}
	return got
}
