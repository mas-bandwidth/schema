package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// EVERY rr-depth-* ROW OF THE SHARED CORPUS IS ANSWERED BY THE REFERENCE
// ENGINE as its manifest line says (docs/SPEC-TABLES.md §16.2): the deepest
// container of a -128 row is at depth 128 and reads, and of a -129 row at depth
// 129 and refuses. The cap counts every container, placed or skipped: a
// by-value table, an array, a list, a map, an enum-keyed object, a union, a
// pointer, an array of pointers, and a value skipped under an unknown table
// key, enum key or union arm (docs/fix-ledger-1122.md FL-12). The rows are
// the data every other port is held to, so the oracle is held to them here.
func TestDepthRowsAnsweredByTheEngine(t *testing.T) {
	root := "../.."
	man, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]string{}
	type row struct{ name, unit, table, dir, want string }
	var rows []row
	for _, line := range strings.Split(string(man), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "unit" {
			units[f[1]] = f[2]
		}
		if len(f) == 6 && f[0] == "json-hostile" && strings.HasPrefix(f[1], "rr-depth-") {
			rows = append(rows, row{f[1], f[2], f[3], f[4], f[5]})
		}
	}
	if len(rows) < 22 {
		t.Fatalf("only %d rr-depth rows in the manifest", len(rows))
	}
	models := map[string]*tabletext.Model{}
	for _, r := range rows {
		m := models[r.unit]
		if m == nil {
			paths, err := compiler.GatherPaths([]string{filepath.Join(root, units[r.unit])})
			if err != nil {
				t.Fatal(err)
			}
			u, err := compiler.New().Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			m = tabletext.NewModel(u)
			models[r.unit] = m
		}
		st := m.Lookup(r.table)
		if st == nil {
			t.Fatalf("%s: no table %s", r.name, r.table)
		}
		text, err := os.ReadFile(filepath.Join(root, r.dir, r.table+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var rep tabletext.Report
		ok := m.Read(m.New(st), text, &rep)
		if want := r.want == "read"; ok != want {
			t.Errorf("%s: engine read %v, manifest says %s", r.name, ok, r.want)
		}
	}
}
