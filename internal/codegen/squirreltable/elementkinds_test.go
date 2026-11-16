package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE CLOSED ARRAY ELEMENT KINDS (docs/SPEC-TABLES.md §3, §4). An array body
// (kind 14) may carry element kinds 1–13, 15, 17 and 18–30; an enum-keyed
// body (kind 16) may carry 1–11, 13 and 18–30. Any other element kind is
// damage under any count, a count of 0 included, in a field the reader cannot
// name as in one it declares. Each vector rides an unknown root field of
// graphdemo Scene, so only the wire-alone walk can see it.
var sqElementKindVectors = []struct {
	name string
	read bool
}{
	{"array_elem_kind_32_count_2", false},
	{"array_elem_kind_31_count_1", false},
	{"array_elem_kind_16_count_1", false},
	{"array_elem_unassigned_count_0", false},
	{"array_elem_reserved_count_0", false},
	{"array_elem_zero_count_0", false},
	// Valid element kinds do not make a malformed body readable, and a
	// mismatch with a declared field is checked against that unit below.
	{"array_elem_terminator_early", false},
	{"array_elem_L_past_body", false},
	// READ CONTROLS: element kind 17 is on the array list, at count 0 and at
	// count 1 where its node index names a reachable record.
	{"array_elem_kind_17_count_0", true},
	{"array_elem_kind_17_count_1", true},
}

// sqElementKindPrelude reads one input through this leg's strict load and
// prints its verdict alone, never a reason (docs/rulings.json
// refusal-verdict-only).
const sqElementKindPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("graphdemo/schema_module.nut", true);
local T = schema.require(%q);
local R = schema.require("schema_table_refusal");
local run = function(key) {
  local verdict = R.ReadStrict(T, "Scene", inputs[key], {}) == null ? "refused" : "read";
  print(key + " " + verdict + "\n");
};
`

// TestSquirrelArrayElementKindsClosed loads every pinned array_elem_* vector
// through the Squirrel strict read of graphdemo.Scene and holds each to the
// verdict the closed lists give it.
func TestSquirrelArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, u := range manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")) {
		if u.key == "graphdemo" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no graphdemo unit")
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "graphdemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	var p strings.Builder
	fmt.Fprintf(&p, sqElementKindPrelude, squirreltable.ModuleName(unit))
	inputs := map[string][]byte{}
	want := map[string]string{}
	for _, v := range sqElementKindVectors {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", v.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[v.name] = data
		want[v.name] = "refused"
		if v.read {
			want[v.name] = "read"
		}
		fmt.Fprintf(&p, "run(%q);\n", v.name)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	seen := 0
	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("probe line %q is not a vector and a verdict", line)
		}
		seen++
		if fields[1] != want[fields[0]] {
			t.Errorf("%s: the strict read answered %s, want %s", fields[0], fields[1], want[fields[0]])
		}
	}
	if seen != len(sqElementKindVectors) {
		t.Errorf("the probe answered %d loads, want %d", seen, len(sqElementKindVectors))
	}
}

// A structurally valid array with a different element kind is judged against
// the field the receiving schema declares, not against an unrelated unit.
func TestSquirrelArrayElementKindMatchesDeclaration(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", "array_elem_kind_mismatch.bin"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, schema, root, want string }{
		{"declared_enum", "", "Cfg", "kind_mismatch"},
		{"matching_i32", "package probe\ntable Cfg { grades [..4]int32 }\n", "Cfg", "ok"},
		{"widen_i64", "package probe\ntable Cfg { grades [..4]int64 }\n", "Cfg", "ok"},
		{"unknown_field", "package probe\ntable Cfg { other int32 }\n", "Cfg", "ok"},
	}
	dir := t.TempDir()
	var script strings.Builder
	script.WriteString(`local inputs = dofile("inputs.nut", true);` + "\n")
	for _, c := range cases {
		var u *ir.Unit
		if c.schema == "" {
			u = loadUnit(t, root, []string{"test/tables/R1.schema"})
		} else {
			u = unitFromSchema(t, c.schema)
		}
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, c.name)
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		for n, b := range files {
			writeFile(t, filepath.Join(out, n), b)
		}
		for n, b := range tables {
			writeFile(t, filepath.Join(out, n), b)
		}
		fmt.Fprintf(&script, `{
 local schema = dofile(%q, true); local T = schema.require(%q); local R = schema.require("schema_table_refusal");
 local report = {}; local v = R.ReadStrict(T, %q, inputs.wire, report);
 local result = v == null ? report.reason : "ok";
 print(%q + " " + result + "\n");
}`+"\n", c.name+"/schema_module.nut", squirreltable.ModuleName(u), c.root, c.name)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wire})
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("bad result %q", line)
		}
		got[fields[0]] = fields[1]
	}
	for _, c := range cases {
		if got[c.name] != c.want {
			t.Errorf("%s = %q, want %q", c.name, got[c.name], c.want)
		}
	}
	if len(got) != len(cases) {
		t.Fatalf("got %d results, want %d", len(got), len(cases))
	}
}
