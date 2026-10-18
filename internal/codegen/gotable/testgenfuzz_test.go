package gotable

import (
	"bytes"
	"maps"
	"regexp"
	"testing"
)

// TestGoTestgenFuzzTargets is the gate of the per-table fuzz targets of
// `schema testgen --lang go` (docs/SPEC-TABLES.md §16.11). It generates
// tables/examples, writes the generated tests beside it in a t.TempDir() and
// runs them: every Fuzz<Name>{Wire,Text,Json} target RUNs its seed corpus
// (every sample encoding and every single-byte flip) and PASSes. Then it
// removes one bounds check of one emitted reader and a target has to fail.
func TestGoTestgenFuzzTargets(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../..", []string{"tables/examples"})
	tests, err := Testgen(u)
	if err != nil {
		t.Fatalf("testgen: %v", err)
	}
	var roots int
	var fuzz []string
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsMapEntry() {
				roots++
			}
		}
	}
	for _, src := range tests {
		for _, m := range regexp.MustCompile(`(?m)^func (Fuzz\w+)\(f \*testing\.F\)`).FindAllSubmatch(src, -1) {
			fuzz = append(fuzz, string(m[1]))
		}
	}
	if len(fuzz) != 3*roots {
		t.Fatalf("testgen emitted %d fuzz targets, want 3 per root table (roots=%d): %v", len(fuzz), roots, fuzz)
	}
	stub := "package " + u.Package + "\n"
	add := func(files map[string][]byte) { maps.Copy(files, tests) }

	t.Run("green", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, u, stub, add, "-v")
		if err != nil {
			t.Fatalf("emitted fuzz seed corpus: %v\n%s", err, out)
		}
		for _, fn := range fuzz {
			if !bytes.Contains(out, []byte("=== RUN   "+fn+"\n")) {
				t.Fatalf("fuzz target %s did not RUN its seed corpus:\n%s", fn, out)
			}
			if !bytes.Contains(out, []byte("--- PASS: "+fn+" ")) && !bytes.Contains(out, []byte("--- PASS: "+fn+"/")) {
				t.Fatalf("fuzz target %s seed corpus did not all PASS:\n%s", fn, out)
			}
		}
		t.Logf("%d fuzz targets ran their seed corpus and passed", len(fuzz))
	})

	t.Run("sabotaged_reader", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, u, stub, func(files map[string][]byte) {
			add(files)
			src := files["GuardedTable.go"]
			at := bytes.Index(src, []byte("func PatrolLoadBody(r *TableReader"))
			if at < 0 {
				t.Fatal("no PatrolLoadBody in GuardedTable.go")
			}
			cut := bytes.Index(src[at:], []byte("\t\tif len(r"))
			if cut < 0 {
				t.Fatal("no bounds check in PatrolLoadBody")
			}
			lineEnd := bytes.IndexByte(src[at+cut:], '\n')
			if lineEnd < 0 {
				t.Fatal("bounds check has no newline")
			}
			lo, hi := at+cut, at+cut+lineEnd+1
			sabotaged := append(append([]byte{}, src[:lo]...), src[hi:]...)
			files["GuardedTable.go"] = sabotaged
		}, "-run", "^FuzzPatrol")
		if err == nil || !bytes.Contains(out, []byte("--- FAIL: FuzzPatrol")) {
			t.Fatalf("a sabotaged Patrol reader went unnoticed by the fuzz targets: %v\n%s", err, out)
		}
	})
}
