package luautable_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestLuauToJsonAgreesWithTheEngine holds the Luau text writer to the
// engine's at every root of tables/examples (docs/SPEC-TABLES.md §16.1,
// §16.5): the engine writes a root's text, the generated module reads it
// with FromJson and writes it back with ToJson, and the bytes must be the
// engine's. Each root is written at its defaults, and a few roots also at
// chosen values: the guarded table with each branch taken (§3: a field under
// a false guard is not written) and the unsigned ranges at their bounds.
func TestLuauToJsonAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	g := generateUnit(t, map[string][]string{"examples": {"tables/examples"}}, "examples")
	if !g.loaded {
		t.Fatalf("tables/examples is not generated for luau: %v", g.err)
	}

	chosen := map[string][]string{
		"Patrol": {
			`{"active": true, "has_target": true, "target_id": 3}`,
			`{"active": true, "has_target": false, "wander": 2}`,
			`{"active": false, "note": "abc"}`,
		},
		"RangedUnsigned": {
			`{"u64_span": 18446744073709551615, "u64_low": 18446744073709551614, "u64_high": 18446744073709551615, "u64_inside": 9223372036854775808, "u32_span": 4294967295, "counts": [18446744073709551615, 0]}`,
		},
	}
	type row struct{ root, label, text string }
	var rows []row
	var names []string
	for name, st := range u.Tables {
		if !st.IsMapEntry() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		inst := m.New(m.Lookup(name))
		text, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s: the engine does not write its defaults: %v", name, err)
		}
		rows = append(rows, row{name, "defaults", string(text)})
		for i, src := range chosen[name] {
			inst := m.New(m.Lookup(name))
			var rep tabletext.Report
			if !m.Read(inst, []byte(src), &rep) {
				t.Fatalf("%s: the engine does not read the chosen text %s", name, src)
			}
			text, err := m.Write(inst)
			if err != nil {
				t.Fatalf("%s: the engine does not write the chosen text %s: %v", name, src, err)
			}
			rows = append(rows, row{name, "chosen_" + strconv.Itoa(i), string(text)})
		}
	}
	for name := range chosen {
		if m.Lookup(name) == nil {
			t.Fatalf("tables/examples declares no %s", name)
		}
	}

	var main strings.Builder
	main.WriteString(jsonModules(g) + jsonFlags(g) + `local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local function hex(s: string): string
	return (string.gsub(s, ".", function(c) return string.format("%02x", string.byte(c)) end))
end
local function row(i: number, root: string)
	local r: any = {}
	local v = json.FromJson(root, inputs["t" .. i], r)
	if v == nil then print(i, "from", tostring(r.reason)); return end
	local w: any = {}
	local out = json.ToJson(root, v, w)
	if out == nil then print(i, "to", tostring(w.reason)); return end
	print(i, "ok", hex(buffer.tostring(out)))
end
`)
	inputs := map[string][]byte{}
	for i, r := range rows {
		inputs["t"+strconv.Itoa(i)] = []byte(r.text)
		fmt.Fprintf(&main, "row(%d, %q)\n", i, r.root)
	}
	out := runProbe(t, g, main.String(), inputs)

	seen := 0
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("the probe printed %q", line)
		}
		i, err := strconv.Atoi(f[0])
		if err != nil || i < 0 || i >= len(rows) {
			t.Fatalf("the probe printed %q", line)
		}
		r := rows[i]
		seen++
		switch f[1] {
		case "from":
			t.Errorf("%s/%s: FromJson refused the engine's text (%s)", r.root, r.label, f[2])
		case "to":
			t.Errorf("%s/%s: ToJson refused (%s) where the engine writes\n%s", r.root, r.label, f[2], r.text)
		default:
			// a root's text ends in one newline (§16.10); the engine's Write
			// is the value's text without that framing
			if want := hexBytes([]byte(r.text + "\n")); f[2] != want {
				t.Errorf("%s/%s: ToJson writes\n%s\nthe engine\n%s", r.root, r.label, unhex(t, f[2]), r.text)
			}
		}
	}
	if seen != len(rows) {
		t.Fatalf("the probe answered %d of %d rows", seen, len(rows))
	}
	t.Logf("%d texts over %d roots", len(rows), len(names))
}

func unhex(t *testing.T, s string) string {
	t.Helper()
	b := make([]byte, len(s)/2)
	for i := range b {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			t.Fatalf("hex %q: %v", s, err)
		}
		b[i] = byte(v)
	}
	return string(b)
}
