package mootable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.6), the
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. Under the one read the reader
// REFUSES THE WHOLE LOAD out_of_range — a scalar, one element of a ranged fixed
// array, and a uint64 at or above 2^63 compared unsigned — exactly as the C++
// reference's own NEW reader does (the manifest line carries no expect=). The
// negative control holds only the reader's range and requires the scalar
// vector to land.
func TestMooFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if !want.Refused {
				t.Fatalf("the C++ reference landed %s: %+v", vec.File, want)
			}
			older, newer := readTestSchema(t, "VOLD_"+vec.Row), readTestSchema(t, "VNEW_"+vec.Row)
			g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
			got := runFixedLoad(t, g, "VNEW_"+vec.Row+"Table", vec.Root, corpusFile(t, corpus, vec.File))
			field := vec.Root + "." + strings.SplitN(strings.TrimPrefix(want.ForgedPath, "r0."), "[", 2)[0]
			if head := "refused out_of_range " + field + " false -"; got.head != head {
				t.Errorf("head %s, want %s: one forged value refuses the whole load", got.head, head)
			}
		})
	}
	older, newer := readTestSchema(t, "VOLD_range_widen"), readTestSchema(t, "VNEW_range_widen")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	plant(t, g, "\tif n.wlo ~= nil and (", "\tif false and (")
	if got := runFixedLoad(t, g, "VNEW_range_widenTable", "RangeWiden", corpusFile(t, corpus, fixedvector.RangeWidenHostile)); !strings.HasPrefix(got.head, "ok 1 ") || got.dump["r0.v"] != "150" {
		t.Errorf("the negative control: the reader's range alone still refuses the writer's overrun: %s %v", got.head, got.dump)
	}
}
