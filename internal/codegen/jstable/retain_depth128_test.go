package jstable

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestJsRetainKeepsDepth128 holds JavaScript's resolving walk to §4's one
// nesting bound: retained unknown fields round trip at every accepted shape,
// while the just-over-limit shapes refuse the retaining load.
func TestJsRetainKeepsDepth128(t *testing.T) {
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
	var vectors []vector
	for _, name := range []string{
		"depth_tables_128", "depth_arrays_128", "depth_union_tables_64", "depth_keyed_128",
		"depth_tables_129", "depth_arrays_129", "depth_union_tables_65", "depth_keyed_129",
	} {
		vectors = append(vectors, vector{Name: name, Read: name == "depth_tables_128" || name == "depth_arrays_128" || name == "depth_union_tables_64" || name == "depth_keyed_128"})
	}
	files := make(map[string][]byte, len(vectors))
	for _, v := range vectors {
		data, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "framing", v.Name+".bin"))
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		files[v.Name+".bin"] = data
	}
	// Two more legal shapes at exactly 128, counted as §4 counts: a keyed
	// slot adds a level only when its element is structural, and a union adds
	// one for a structural arm while its own frame adds none. Each reuses
	// depth_tables_128's id table and count, the file's last sixteen bytes.
	tables128 := files["depth_tables_128.bin"]
	tail := tables128[len(tables128)-16:]
	framed := func(prefix []byte, content []byte) []byte {
		out := append([]byte{}, prefix...)
		out = depthBoundaryAppendLeb(out, uint64(len(content)))
		return append(out, content...)
	}
	file := func(body []byte) []byte {
		out := append([]byte{tables128[0]}, body...)
		return append(out, tail...)
	}
	// 128 keyed bodies nested by value; the innermost body's one slot is an i32
	keyed := []byte{4, 1, 1, 4, 7, 0, 0, 0}
	for i := 1; i < 128; i++ {
		keyed = framed([]byte{16, 1, 1}, keyed)
	}
	files["depth_keyed_scalar_128.bin"] = file(append(framed([]byte{1, 16}, keyed), 0))
	vectors = append(vectors, vector{Name: "depth_keyed_scalar_128", Read: true})
	// 127 tables, then a union whose arm is a union whose arm is an i32
	body := append(framed([]byte{1, 15, 1, 15}, []byte{1, 4, 4, 7, 0, 0, 0}), 0)
	for range 127 {
		body = append(framed([]byte{1, 13}, body), 0)
	}
	files["depth_tables_union_union_128.bin"] = file(body)
	vectors = append(vectors, vector{Name: "depth_tables_union_union_128", Read: true})
	rows, err := json.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	jsDiscardRun(t, u, files, fmt.Sprintf(`
for (const row of %s) {
  const wire = new Uint8Array(readFileSync(row.name + ".bin"));
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 20), new Array(1 << 16));
  const report = new m.TableVariableReport();
  const value = m.SceneLoadRetain(wire, retain, report);
  if (!row.read) {
    if (value !== null || report.err() === null) {
      fail(row.name + ": retaining load did not refuse over-limit wire: " + JSON.stringify(report));
    }
    continue;
  }
  if (value === null || report.err() !== null) {
    fail(row.name + ": retaining load refused a legal wire: " + JSON.stringify(report));
    continue;
  }
  const saved = m.SceneSaveRetain(value, retain, report);
  if (saved === null || report.err() !== null || report.retainLost !== 0) {
    fail(row.name + ": retaining save refused or lost retention: " + JSON.stringify(report));
    continue;
  }
  if (Buffer.compare(Buffer.from(saved), Buffer.from(wire)) !== 0) {
    fail(row.name + ": retained bytes differ (saved=" + saved.length + " input=" + wire.length + ")");
  }
}
`, string(rows)))
}
