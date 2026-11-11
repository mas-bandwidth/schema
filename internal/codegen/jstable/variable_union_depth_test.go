package jstable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJsUnknownUnionCountsItsLevel(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"depth_tables_128.bin":      true,
		"depth_arrays_128.bin":      true,
		"depth_union_tables_64.bin": true,
		"depth_keyed_128.bin":       true,
		"depth_tables_129.bin":      false,
		"depth_arrays_129.bin":      false,
		"depth_union_tables_65.bin": false,
		"depth_keyed_129.bin":       false,
		"depth_tables_5000.bin":     false,
	}
	type row struct {
		Name string `json:"name"`
		File string `json:"file"`
		Read bool   `json:"read"`
	}
	rows := make([]row, 0, len(cases))
	wires := make(map[string][]byte, len(cases))
	for name, read := range cases {
		path := filepath.Join(repo, "testdata/wire/tables/framing", name)
		wire, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row{Name: name, File: name, Read: read})
		wires[name] = wire
	}
	want, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	model := tabletext.NewModel(u)
	root := model.Lookup("Scene")
	if root == nil {
		t.Fatal("no table Scene in tables/pointers")
	}
	for name, expected := range cases {
		var report tabletext.Report
		read, err := tablewire.Decode(model, model.New(root), wires[name], &report)
		if err != nil && !tablewire.ReadRefusal(err) {
			t.Fatalf("oracle Decode %s: %v", name, err)
		}
		oracleRead := read && err == nil
		if oracleRead != expected {
			t.Fatalf("oracle verdict for %s: read=%v err=%v, want read=%v", name, read, err, expected)
		}
	}
	jsDiscardRun(t, u, wires, `
for (const row of `+string(want)+`) {
  const wire = new Uint8Array(readFileSync(row.file));
  const report = new m.TableVariableReport();
  const value = m.SceneLoad(wire, report);
  const read = value !== null && report.err() === null;
  if (read !== row.read) {
    fail(row.name + ": expected read=" + row.read + ", got read=" + read);
  }
}
`)
}
