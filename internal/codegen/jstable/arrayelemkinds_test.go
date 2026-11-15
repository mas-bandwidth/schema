package jstable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// jsHeldElementKindVectors are the pinned array_elem_* scripts and the verdict
// docs/SPEC-TABLES.md §3's closed element-kind lists give each: an array body
// (kind 14) may carry 1–13, 15, 17 and 18–30, so every other kind is damage
// under ANY count, a count of 0 included, in a field this reader cannot name
// as in one it declares. The two kind-17 rows are the read controls.
var jsHeldElementKindVectors = []struct {
	name string
	load bool
}{
	{name: "array_elem_kind_32_count_2", load: false},
	{name: "array_elem_kind_31_count_1", load: false},
	{name: "array_elem_kind_16_count_1", load: false},
	{name: "array_elem_unassigned_count_0", load: false},
	{name: "array_elem_reserved_count_0", load: false},
	{name: "array_elem_zero_count_0", load: false},
	{name: "array_elem_kind_17_count_0", load: true},
	{name: "array_elem_kind_17_count_1", load: true},
}

// TestJsArrayElementKindsClosed is the JavaScript leg's gate for THE ELEMENT
// KINDS ARE CLOSED (docs/SPEC-TABLES.md §3, §4): every pinned array_elem_*
// vector rides an unknown root field of graphdemo Scene, so the wire-alone walk
// judges the body's element kind under any count. A vector whose kind is on the
// list loads; one off it refuses the whole load. Verdicts only, never a reason
// (docs/rulings.json refusal-verdict-only).
func TestJsArrayElementKindsClosed(t *testing.T) {
	t.Parallel()

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	type row struct {
		Name string `json:"name"`
		File string `json:"file"`
		Load bool   `json:"load"`
	}
	rows := make([]row, 0, len(jsHeldElementKindVectors))
	for _, v := range jsHeldElementKindVectors {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "framing", v.name+".bin"))
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		name := v.name + ".bin"
		files[name] = wire
		rows = append(rows, row{Name: v.name, File: name, Load: v.load})
	}
	js, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	jsDiscardRun(t, u, files, `
for (const row of `+string(js)+`) {
  const wire = new Uint8Array(readFileSync(row.file));
  const r = new m.TableVariableReport();
  const v = m.SceneLoad(wire, r);
  if (row.load) {
    if (v === null || r.err() !== null) { fail(row.name + ": a vector on the closed list did not load: " + JSON.stringify(r)); }
  } else if (v !== null || r.err() === null) {
    fail(row.name + ": an element kind off the closed list loaded (reason=" + r.err() + ")");
  }
}
`)
}
