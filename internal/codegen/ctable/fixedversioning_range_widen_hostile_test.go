package ctable

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
// range and counts `clamped` once: a scalar, one element of a ranged fixed
// array (held the same as a scalar), and a uint64 at or above 2^63 (compared
// unsigned, so it lands the max). The answer is the manifest's `expect=`, which
// is what the C++ reference's own NEW reader landed from the same bytes.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "a C compiler (it runs the generated NEW reader over the forged file)")
	corpus := cFixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			body, err := cWriterRangeChecks(vec, want)
			if err != nil {
				t.Fatal(err)
			}
			newer := cReadSchema(t, "VNEW_"+vec.Row)
			older := cReadSchema(t, "VOLD_"+vec.Row)
			out, err := cRunVersionProbe(t, newer, []string{older}, 0, filepath.Join(corpus, vec.File), body, "")
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}

// cWriterRangeChecks spells a vector's expect= as the probe body: each landed
// value compared as a 64-bit integer (a uint64 past 2^63 reads negative, which
// is never an expected value), then the counters.
func cWriterRangeChecks(vec fixedvector.Vector, want fixedvector.Expect) (string, error) {
	leaves, err := want.Leaves()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "    if ( n != 1 || r.malformed || r.refused ) { printf( \"%[1]s: n=%%lld malformed=%%d refused=%%d, want one record\\n\", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }\n", vec.Name)
	for _, l := range leaves {
		expr := "back[0]." + l.Field
		if l.Index >= 0 {
			expr += fmt.Sprintf("[%d]", l.Index)
		}
		fmt.Fprintf(&b, "    if ( (long long) %[1]s != %[2]dLL ) { printf( \"%[3]s: %[4]s landed %%lld, want %[2]d: the forged value clamps to the WRITER's range\\n\", (long long) %[1]s ); return 1; }\n",
			expr, l.Value, vec.Name, l.Path)
	}
	fmt.Fprintf(&b, "    if ( r.clamped != %[1]d ) { printf( \"%[2]s: clamped=%%d, want exactly %[1]d: one forged value, one clamp\\n\", (int) r.clamped ); return 1; }\n", want.Clamped, vec.Name)
	fmt.Fprintf(&b, "    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 ) { printf( \"%s: a counter that must not move did\\n\" ); return 1; }\n", vec.Name)
	return b.String(), nil
}
