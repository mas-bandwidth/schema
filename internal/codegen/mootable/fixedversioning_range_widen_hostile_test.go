package mootable_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vector every leg reads. hostile_range_widen.bin is the
// OLD writer's | 0..100 file with `v` forged to 150, inside the NEW reader's
// | 0..200; the NEW reader clamps it to the WRITER's max and counts `clamped`
// once. The answer is the manifest's `expect=`, which is what the C++
// reference's own NEW reader landed from the same bytes.
func TestMooFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	older, newer := readTestSchema(t, "VOLD_range_widen"), readTestSchema(t, "VNEW_range_widen")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	got := runFixedLoad(t, g, "VNEW_range_widenTable", "RangeWiden", corpusFile(t, corpus, fixedvector.RangeWidenHostile))
	t.Logf("range_widen_hostile: landed v=%s, head %s", got.dump["r0.v"], got.head)
	if head := fmt.Sprintf("ok 1 u0 k0 c%d w0", want.Clamped); got.head != head {
		t.Errorf("head %s, want %s: the forged 150 clamps to the WRITER's max and counts once", got.head, head)
	}
	for _, path := range []string{"r0.lead", "r0.v", "r0.trail"} {
		w, err := want.Int(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.dump[path] != strconv.FormatInt(w, 10) {
			t.Errorf("%s landed %s, want %d", path, got.dump[path], w)
		}
	}
}
