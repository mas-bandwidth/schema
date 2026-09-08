package main

// The collection extent contract is independently implemented by the C++
// reference. Exercise it and C with the shared hostile-wire mutators, including
// exact LoadMeasure answers for partial and duplicated container fields.
import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCCollectionDifferential(t *testing.T) { cCollectionDifferential(t, "") }

func TestCCollectionCookDifferential(t *testing.T) {
	for _, mode := range []string{"--cook-le", "--cook-be"} {
		t.Run(mode, func(t *testing.T) { cCollectionDifferential(t, mode) })
	}
}

func cCollectionDifferential(t *testing.T, mode string) {
	driver, reference := os.Getenv("SCHEMA_C_COLLECTIONS_DRIVER"), os.Getenv("SCHEMA_CPP_COLLECTIONS_DRIVER")
	if driver == "" || reference == "" {
		t.Skip("run make tables-c-collections-fuzz to build both native legs")
	}
	t.Chdir(conformanceRoot(t))
	driver += " " + mode
	reference += " " + mode
	var roots []*wireRoot
	var seeds []*wireSeed
	var indexes []int
	cases := []struct{ unit, root, name string }{
		{"mapdemo", "Depth", "map_depth"},
		{"mapdemo", "Text", "map_text"},
		{"mapdemo", "Cells", "map_cells"},
		{"mapdemo", "Runs", "map_runs"},
		{"mapdemo", "Slots", "map_slots"},
		{"mapdemo", "Spans", "map_spans"},
		{"mapdemo", "Docs", "map_docs"},
		{"mapdemo", "Chunks", "map_chunks"},
		{"mapdemo", "Pairs", "map_pairs"},
		{"mapdemo", "Crews", "map_crews"},
		{"mapdemo", "Trails", "map_trails"},
		{"mapdemo", "Fleet", "map_full"},
		{"mapdemo", "Fleet", "map_empty"},
		{"listdemo", "Save", "list_tables"},
		{"listdemo", "Save", "list_scalars"},
		{"listdemo", "Save", "list_empty"},
		{"listdemo", "Save", "list_erased"},
		{"listdemo", "Mixed", "list_mixed"},
		{"listdemo", "Album", "list_shared"},
		{"listdemo", "Album", "list_before_pointer"},
		{"listdemo", "Sheet", "list_nested"},
		{"listdemo", "Army", "list_of_maps"},
		{"listdemo", "Unbounded", "list_migrates"},
	}
	for _, c := range cases {
		path := filepath.Join("testdata/wire/tables", c.name+".bin")
		wire, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, &wireRoot{unit: c.unit, root: c.root, variable: true})
		seeds = append(seeds, &wireSeed{name: c.name, unit: c.unit, root: c.root, wire: wire, frame: frameWire(wire)})
		indexes = append(indexes, len(roots)-1)
	}
	actual, err := startWireLeg(driver, roots)
	if err != nil {
		t.Fatal(err)
	}
	defer actual.kill()
	expected, err := startWireLeg(reference, roots)
	if err != nil {
		t.Fatal(err)
	}
	defer expected.kill()
	for i := range roots {
		if !actual.known[i] || !expected.known[i] {
			t.Fatalf("codec absent for %s.%s", roots[i].unit, roots[i].root)
		}
	}
	count := 0
	check := func(seed *wireSeed, index int, pass string, wire []byte) {
		t.Helper()
		if err := actual.send(index, wire); err != nil {
			t.Fatal(err)
		}
		a, err := actual.receive()
		if err != nil {
			t.Fatalf("C died: %v\n%s", err, actual.stderr.String())
		}
		if err := expected.send(index, wire); err != nil {
			t.Fatal(err)
		}
		e, err := expected.receive()
		if err != nil {
			t.Fatalf("C++ died: %v\n%s", err, expected.stderr.String())
		}
		if a.loaded != e.loaded || a.measure != e.measure || a.report != e.report || (a.loaded && (a.saveFail != e.saveFail || !bytes.Equal(a.saved, e.saved))) {
			dir := filepath.Join("build", "c-collections-failures")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "failed.bin"), wire, 0644); err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(dir, "c.bin"), a.saved, 0644)
			_ = os.WriteFile(filepath.Join(dir, "cpp.bin"), e.saved, 0644)
			t.Fatalf("after %d mutants: %s %s\nC: loaded=%t measure=%d report=%s save-failed=%t\nC++: loaded=%t measure=%d report=%s save-failed=%t\nwire=%x", count, seed.name, pass, a.loaded, a.measure, a.report, a.saveFail, e.loaded, e.measure, e.report, e.saveFail, wire)
		}
		count++
	}
	for i, s := range seeds {
		enumerated(s, func(pass string, wire []byte) { check(s, indexes[i], pass, wire) })
	}
	for i := 0; i < 20000; i++ {
		m := randomMutant(seeds, 1, i)
		index := 0
		for j, s := range seeds {
			if s == m.seed {
				index = j
				break
			}
		}
		check(m.seed, index, m.pass, m.data)
	}
	t.Logf("C/C++ collections %s: %d mutants, identical reports, output bytes, and measured sizes", mode, count)
}
