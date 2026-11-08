package cpptable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. The reader clamps to the WRITER's
// range and counts `out_of_range` once: a scalar, one element of a ranged fixed
// array (held the same as a scalar), and a uint64 at or above 2^63 (compared
// unsigned, so it lands the max). The answer is the manifest's `expect=`, which
// the dump wrote from this leg's own reader over the same bytes; the probe here
// reads the FILE, so the generated lineage reader is held to it too.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "a C++ compiler (it runs the generated NEW reader over the forged file)")
	corpus := cppFixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			leaves, err := want.Leaves()
			if err != nil {
				t.Fatal(err)
			}
			var body strings.Builder
			_ = leaves
			if want.Clamped > 0 {
				fmt.Fprintf(&body, "    if ( n != -1 || !r.refused || r.refuse_reason != out_of_range ) { printf( \"%s: strict read did not refuse out_of_range: n=%%lld refused=%%d reason=%%d\\n\", (long long) n, (int) r.refused, (int) r.refuse_reason ); return 1; }\n", vec.Name)
			} else {
				fmt.Fprintf(&body, "    if ( n != 1 || r.refused ) { printf( \"%s: clean widen did not read one record\n\" ); return 1; }\n", vec.Name)
			}
			out, err := cppRunVersionProbe(t, vec.Row, filepath.Join(corpus, vec.File), body.String(), "")
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
