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
