package ctable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE VARIABLE FORM CARRIES NO FIXED-TABLE PATHS (docs/SPEC-TABLES.md §3).
// Form byte 1 is the variable form. A table whose class is fixed encodes as
// form 3 and never as form 1, so nothing in this emitter may exist to make a
// fixed table FAST on form 1. Two things were exactly that and are gone:
//
//   - THE CONSTANT-WIDTH MEASURE. A table of plain scalar leaves got a
//     measure-only early branch that summed 1 + kind + declared width per
//     riding field instead of walking the body. It was gated on the table not
//     being variable, because only a fixed body has constant leaf widths.
//   - THE SIZING-TO-WRITE ELEMENT CACHE. An array of fixed child tables got a
//     bounded `element_sizes` scratch so the write pass could reuse the length
//     the sizing pass measured, plus the wireFrame variant that kept the
//     probe's interned ids to make those lengths reusable. It was gated on
//     both the owner and the element being non-variable.
//
// What stays, and why it is not this: the force-inline qualifier on a fixed
// class's save_body/load_body is a LEGALITY switch, not a specialisation — a
// pointered body is directly recursive through the depth-carrying form and a
// recursive always_inline is a compile error under gcc, so the class test is
// where force-inline stops being legal, not where it starts being wanted. The
// check_default probe pass is the nested-table elision rule of §3 and every
// class rides it. Neither is a fixed-table path.
func TestNoFixedOnlyPathsInThisEmitter(t *testing.T) {
	gone := []string{
		"emitWireScalarLeafMeasure",
		"wireElementCacheSlots",
		"wireFrameWithProbeIDs",
		"element_sizes",
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

// The generated C a pointer-free unit produces carries neither the scratch nor
// the measure-only early return.
func TestGeneratedCCarriesNoFixedOnlyPaths(t *testing.T) {
	for name, data := range generate(t, `package probe
table Leaf {
 n int32
 ok bool
 x float32
}
table Root {
 leaf Leaf
 leaves [..8]Leaf
}
`) {
		src := string(data)
		for _, gone := range []string{"element_sizes", "bounded sizing-to-write cache", "kind and fixed-width payload"} {
			if strings.Contains(src, gone) {
				t.Errorf("%s still carries %q", name, gone)
			}
		}
	}
}
