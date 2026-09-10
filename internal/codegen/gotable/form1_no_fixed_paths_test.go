package gotable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE VARIABLE FORM CARRIES NO FIXED-TABLE PATHS (docs/SPEC-TABLES.md §3).
// Form byte 1 is the variable form. A table whose class is fixed encodes as
// form 3 and never as form 1, so nothing in this emitter may exist to make a
// fixed table FAST on form 1. These names were exactly that, and this test is
// the refuser that keeps them from coming back under another patch:
//
//   - the arithmetic MeasureBody (a constant-width body summed from the
//     declared leaf widths, gated on the table not being variable), and
//   - the array-length scratch cache (gated on the whole unit having no
//     variable table at all, so it could only ever fire in an all-fixed unit).
//
// What stays is what the variable form itself needs and shares: the dry-run
// SaveBody walk every MeasureBody is, the nested-table MeasureBody reuse (not
// gated on class), refAtHit and the ordinal it takes, the region/arena mode
// bit, and the form-1 file-root Save/Load — a reader must still read a form-1
// file whatever shape the table has.
func TestNoFixedOnlyPathsInThisEmitter(t *testing.T) {
	gone := []string{
		"arithMeasureEligible",
		"arithMeasureFieldEligible",
		"emitArithMeasureBody",
		"emitArithMeasureField",
		"cacheArrayLengths",
		"arrayLengthScratchSlots",
		"arrayLengthCacheBytes",
		"arrayLengths",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || name == "form1_no_fixed_paths_test.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, symbol := range gone {
			if strings.Contains(string(data), symbol) {
				t.Errorf("%s still names the removed form-1 fixed-table path %q", name, symbol)
			}
		}
	}
}

// The generated Go a schema of pointer-free tables produces carries none of it
// either: no arithmetic body, no scratch array.
func TestGeneratedGoCarriesNoFixedOnlyPaths(t *testing.T) {
	for _, data := range generate(t, `package probe
enum Grade { Gold, Silver }
type Child { n int32 }
table Leaf {
 n int32
 grade Grade
 ok bool
}
table Root {
 children [..8]Child
 pair [2]Child
}
`) {
		src := string(data)
		for _, gone := range []string{"var arrayLengths [", "bytes := int64(1)"} {
			if strings.Contains(src, gone) {
				t.Errorf("generated source still carries %q", gone)
			}
		}
	}
}
