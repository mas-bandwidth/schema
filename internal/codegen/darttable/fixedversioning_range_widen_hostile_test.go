package darttable

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
	slowtest.Gate(t, "dart (it runs the generated NEW reader over the forged file)")
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
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
			var checks strings.Builder
			for _, l := range leaves {
				expr := "values[0]." + l.Field
				if l.Index >= 0 {
					expr += fmt.Sprintf("[%d]", l.Index)
				}
				fmt.Fprintf(&checks, "  check('${%[1]s}' == '%[2]d',\n      '%[3]s landed ${%[1]s}, want %[2]d: the forged value clamps to the WRITER range');\n",
					expr, l.Value, l.Path)
			}
			body := fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1 && report.refused == 0 && !report.malformed,
      'the forged file reads one record: n=$n ${why(report)}');
%[1]s  check(report.clamped == %[2]d,
      'clamped=${report.clamped}, want exactly %[2]d: one forged value, one clamp');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.duplicate == 0,
      'a counter that must not move did: ${why(report)}');`, checks.String(), want.Clamped)
			out, err := runVersionProbe(t, dartBin, "VNEW_"+vec.Row, []string{"VOLD_" + vec.Row}, 0,
				filepath.Join(corpus, vec.File), body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
