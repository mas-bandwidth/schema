package darttable

import (
	"path/filepath"
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
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			if _, err := fixedvector.Read(corpus, vec.File); err != nil {
				t.Fatal(err)
			}
			// STRICT VERDICT: a value past the writer's range refuses the
			// whole read (out_of_range); no clamped value lands.
			body := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1 && report.refused == t.TableFixedRefusal.outOfRange,
      'the forged file refuses out_of_range: n=$n ${why(report)}');
  check(fresh(values[0]), 'a refused read lands nothing: ${why(report)}');
  check(report.unknown == 0 && report.widened == 0,
      'a counter that must not move did: ${why(report)}');`
			out, err := runVersionProbe(t, dartBin, "VNEW_"+vec.Row, []string{"VOLD_" + vec.Row}, 0,
				filepath.Join(corpus, vec.File), body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
