package goosetable

import (
	"fmt"
	"strconv"
	"strings"
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
func TestGooseFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated NEW reader over the forged file)")
	t.Parallel()
	corpus := fixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Row, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			leaves, err := want.Leaves()
			if err != nil {
				t.Fatal(err)
			}
			var print []string
			for _, l := range leaves {
				expr := "values[0]." + strings.ToUpper(l.Field[:1]) + l.Field[1:]
				if l.Index >= 0 {
					expr += fmt.Sprintf("[%d]", l.Index)
				}
				print = append(print, expr)
			}
			older := evolutionLoad(t, "test/tables/VOLD_"+vec.Row+".schema")
			newer := evolutionLoad(t, "test/tables/VNEW_"+vec.Row+".schema")
			got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: vec.Root,
				capacity: 1, data: gooseCorpusFile(t, corpus, vec.File), print: print})
			got.reads(t, 1)
			if len(got.printed) != len(leaves) {
				t.Fatalf("printed %v, want %d values", got.printed, len(leaves))
			}
			for k, l := range leaves {
				if got.printed[k] != strconv.FormatInt(l.Value, 10) {
					t.Errorf("%s landed %s, want %d: the forged value clamps to the WRITER's range", l.Path, got.printed[k], l.Value)
				}
			}
			if got.clamped != want.Clamped {
				t.Errorf("clamped=%d, want exactly %d: one forged value, one clamp", got.clamped, want.Clamped)
			}
			if got.widened != 0 || got.unknown != 0 || got.kindMismatch != 0 {
				t.Errorf("a counter that must not move did: %s", got.raw)
			}
		})
	}
}
