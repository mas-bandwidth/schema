package goosetable

// A REPEATED KEY REFUSES IN EVERY OBJECT OF THE TEXT (docs/SPEC-TABLES.md
// §16.2, §16.12), the objects a read skips included, and keys are compared as
// DECODED bytes. The compiler's own engine is the oracle: every text below is
// read in-process through internal/tabletext, and both Goose readers — the
// typed <Root>FromJson walk and the flat json_load_table walk — must answer
// the same verdict.
//
// The negative control removes the skip's decoded-key set from a copy of the
// runtime; every text whose repeat sits inside a skipped object must then go
// red by name, and no other row may move.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type repeatedKeyCase struct {
	name, root, text string
	refuse           bool // the engine refuses it; false is a read
	skip             bool // the repeat sits inside a value the read skips
}

func repeatedKeyCases() []repeatedKeyCase {
	var many strings.Builder
	many.WriteString(`{ "teams": {`)
	for i := 0; i < 65; i++ {
		fmt.Fprintf(&many, " \"Unknown%d\": 1,", i)
	}
	many.WriteString(` "Unknown64": 2 } }`)
	return []repeatedKeyCase{
		{name: "root-unknown", root: "TeamConfig", text: `{ "mystery": 1, "mystery": 2 }`, refuse: true},
		{name: "skipped-object", root: "TeamConfig", text: `{ "mystery": { "a": 1, "a": 2 } }`, refuse: true, skip: true},
		{name: "skipped-array", root: "TeamConfig", text: `{ "mystery": [ { "a": 1, "a": 2 } ] }`, refuse: true, skip: true},
		{name: "unknown-known-unknown", root: "TeamConfig", text: `{ "mystery": 1, "spawn_count": 5, "mystery": 2 }`, refuse: true},
		{name: "skipped-escaped", root: "TeamConfig", text: `{ "mystery": { "k": 1, "\u006B": 2 } }`, refuse: true, skip: true},
		{name: "root-escaped-unknown", root: "TeamConfig", text: `{ "mystery": 1, "\u006Dystery": 2 }`, refuse: true},
		{name: "keyed-past-64", root: "KeyedConfig", text: many.String(), refuse: true},
		{name: "one-unknown", root: "TeamConfig", text: `{ "mystery": 1 }`},
		{name: "two-unknown", root: "TeamConfig", text: `{ "mystery": 1, "other": 2 }`},
		{name: "skipped-distinct", root: "TeamConfig", text: `{ "mystery": { "a": 1, "b": 2 } }`},
		{name: "siblings-same-inner", root: "TeamConfig", text: `{ "first": { "a": 1 }, "second": { "a": 2 } }`},
		{name: "escaped-known", root: "TeamConfig", text: `{ "\u0073pawn_count": 5 }`},
	}
}

func repeatedKeyUnit(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fixedRoot, "tables", "examples", "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("tables/examples has no .schema files")
	}
	u, err := loadSchemaPaths(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	return u
}

func repeatedKeyProbe(t *testing.T, u *ir.Unit, files map[string][]byte, dir string, cases []repeatedKeyCase) string {
	t.Helper()
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u)
	roots := map[string]bool{}
	for _, c := range cases {
		roots[c.root] = true
	}
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n")
	for _, root := range slicesSortedKeys(roots) {
		fmt.Fprintf(&b, "fn typed_%s(name: const u8[:]) -> bool {\n", root)
		b.WriteString("    var text: u8[>..] = [];\n    if !read_file(name, text) { print(\"read-failed\"); return false; }\n")
		fmt.Fprintf(&b, "    var v = %s::New%s();\n    var r = schema_rt::json_report();\n", ns, root)
		fmt.Fprintf(&b, "    return %s::%sFromJson(v, text, r);\n}\n", ns, root)
		fmt.Fprintf(&b, "fn flat_%s(name: const u8[:]) -> bool {\n", root)
		b.WriteString("    var text: u8[>..] = [];\n    if !read_file(name, text) { print(\"read-failed\"); return false; }\n")
		b.WriteString("    var data: u8[>..] = [];\n    var vals: schema_rt::LoadVal[>..] = [];\n    var nodes: schema_rt::LoadNode[>..] = [];\n    var r = schema_rt::json_report();\n")
		fmt.Fprintf(&b, "    let at = schema_rt::json_load_table(%s::TableUnit, %s::TextUnit, \"%s\", text, data, vals, nodes, r);\n", ns, ns, root)
		b.WriteString("    return at >= 0 && !r.damaged;\n}\n\n")
	}
	b.WriteString("fn main() {\n")
	for i, c := range cases {
		fmt.Fprintf(&b, "    print(typed_%s(\"in%d.json\"));\n", c.root, i)
		fmt.Fprintf(&b, "    print(flat_%s(\"in%d.json\"));\n", c.root, i)
	}
	b.WriteString("}\n")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return "main.goose"
}

func slicesSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// repeatedKeyFails runs the case table and answers every row one of the Goose
// readers disagrees with the engine on, in table order.
func repeatedKeyFails(t *testing.T, edit func(map[string][]byte)) []string {
	t.Helper()
	u := repeatedKeyUnit(t)
	cases := repeatedKeyCases()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	main := repeatedKeyProbe(t, u, files, dir, cases)
	inputs := map[string][]byte{}
	for i, c := range cases {
		inputs[fmt.Sprintf("in%d.json", i)] = []byte(c.text)
	}
	out := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, main, inputs)), "\n"), "\n")
	if len(out) != 2*len(cases) {
		t.Fatalf("the probe printed %d lines, want %d:\n%s", len(out), 2*len(cases), strings.Join(out, "\n"))
	}
	m := tabletext.NewModel(u)
	var fails []string
	for i, c := range cases {
		st := m.Lookup(c.root)
		if st == nil {
			t.Fatalf("no table %s", c.root)
		}
		inst := m.New(st)
		var report tabletext.Report
		ok := m.Read(inst, []byte(c.text), &report)
		if ok == c.refuse {
			t.Errorf("%s: the engine says read=%v; the case table expects refuse=%v (%+v)", c.name, ok, c.refuse, report)
		}
		typed, flat := out[2*i] == "true", out[2*i+1] == "true"
		if typed != ok || flat != ok {
			fails = append(fails, fmt.Sprintf("%s: typed read=%v, flat read=%v, engine read=%v", c.name, typed, flat, ok))
		}
	}
	return fails
}

func TestGooseJSONRepeatedKeyEveryObject(t *testing.T) {
	t.Parallel()
	for _, f := range repeatedKeyFails(t, nil) {
		t.Error(f)
	}
}

const repeatedKeySkipMarker = "// A SKIPPED OBJECT'S KEYS ARE HELD TO THE RULE TOO (§16.2, §16.12).\n"

// repeatedKeySabotageSkip removes the skip's decoded-key set from a copy of
// the runtime: the marker line and the check beneath it, in both readers.
func repeatedKeySabotageSkip(t *testing.T, files map[string][]byte) {
	t.Helper()
	for _, name := range []string{"json_value.goose", "schema_json.goose"} {
		src := string(files[name])
		at := strings.Index(src, repeatedKeySkipMarker)
		if at < 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: no skip set marker in %s", name)
		}
		next := strings.Index(src[at+len(repeatedKeySkipMarker):], "\n")
		if next < 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the check after the skip marker in %s is not a line", name)
		}
		files[name] = []byte(src[:at] + src[at+len(repeatedKeySkipMarker)+next+1:])
	}
}

func TestGooseJSONRepeatedKeyEveryObjectNegativeControl(t *testing.T) {
	t.Parallel()
	fails := repeatedKeyFails(t, func(files map[string][]byte) { repeatedKeySabotageSkip(t, files) })
	var red []string
	for _, f := range fails {
		name, _, _ := strings.Cut(f, ":")
		red = append(red, name)
	}
	var want []string
	for _, c := range repeatedKeyCases() {
		if c.skip {
			want = append(want, c.name)
		}
	}
	if strings.Join(red, ",") != strings.Join(want, ",") {
		t.Fatalf("NEGATIVE CONTROL FAILED: removing the skip's set check turned %v red, want exactly %v", red, want)
	}
	t.Logf("repeated-key skip negative control: red on exactly %v", red)
}
