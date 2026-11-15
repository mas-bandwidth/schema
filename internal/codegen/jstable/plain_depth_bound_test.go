package jstable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestJsPlainReadRefusesPastDepthBound holds JavaScript's plain variable load
// to §4's one nesting bound: the root body is at depth 0, each structural
// payload inside it is one level deeper, depth 128 passes and 129 refuses. The
// held depth vectors ride an unknown field, so the refusal comes from the
// reader that names no declaration for them. A legal control is read so a
// reader that refused everything would be red here too.
func TestJsPlainReadRefusesPastDepthBound(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	type vector struct {
		Name string `json:"name"`
		Read bool   `json:"read"`
	}
	vectors := []vector{
		{Name: "depth_union_tables_64", Read: true},
		{Name: "depth_union_tables_65", Read: false},
		{Name: "depth_keyed_129", Read: false},
	}
	files := make(map[string][]byte, len(vectors))
	for _, v := range vectors {
		data, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "framing", v.Name+".bin"))
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		files[v.Name+".bin"] = data
	}
	rows, err := json.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	jsDiscardRun(t, u, files, `
for (const row of `+string(rows)+`) {
  const wire = new Uint8Array(readFileSync(row.name + ".bin"));
  const report = new m.TableVariableReport();
  const value = m.SceneLoad(wire, report);
  const read = value !== null && report.err() === null;
  if (read !== row.read) {
    fail(row.name + ": expected read=" + row.read + ", got read=" + read + " " + JSON.stringify(report));
  }
}
`)
}
