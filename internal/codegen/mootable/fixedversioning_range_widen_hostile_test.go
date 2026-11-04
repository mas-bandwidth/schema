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
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. The reader clamps to the WRITER's
// range and counts `clamped` once: a scalar, one element of a ranged fixed
// array (held the same as a scalar), and a uint64 at or above 2^63 (compared
// unsigned, so it lands the max). The answer is the manifest's `expect=`, which
// is what the C++ reference's own NEW reader landed from the same bytes.
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
			older, newer := readTestSchema(t, "VOLD_"+vec.Row), readTestSchema(t, "VNEW_"+vec.Row)
			g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
			got := runFixedLoad(t, g, "VNEW_"+vec.Row+"Table", vec.Root, corpusFile(t, corpus, vec.File))
			if head := fmt.Sprintf("ok 1 u0 k0 c%d w0", want.Clamped); got.head != head {
				t.Errorf("head %s, want %s: one forged value, one clamp", got.head, head)
			}
			for _, path := range want.Paths {
				if w := strconv.FormatInt(want.Values[path], 10); got.dump[path] != w {
					t.Errorf("%s landed %s, want %s: the forged value clamps to the WRITER's range", path, got.dump[path], w)
				}
			}
		})
	}
}
