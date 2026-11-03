package darttable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vector every leg reads. hostile_range_widen.bin is the
// OLD writer's | 0..100 file with `v` forged to 150, inside the NEW reader's
// | 0..200; the NEW reader clamps it to the WRITER's max and counts `clamped`
// once. The answer is the manifest's `expect=`, which is what the C++
// reference's own NEW reader landed from the same bytes.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "dart (it runs the generated NEW reader over the forged file)")
	corpus := fixedCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := want.Int("r0.v")
	if err != nil {
		t.Fatal(err)
	}
	lead, err := want.Int("r0.lead")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := want.Int("r0.trail")
	if err != nil {
		t.Fatal(err)
	}
	dartBin := dartBinary(t)
	body := fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  print('range_widen_hostile: landed v=${values[0].v} clamped=${report.clamped}');
  check(n == 1 && report.refused == 0 && !report.malformed,
      'the forged file reads one record: n=$n ${why(report)}');
  check(values[0].v == %[1]d && report.clamped == %[2]d,
      'v=${values[0].v} clamped=${report.clamped}, want v=%[1]d clamped=%[2]d: the forged 150 clamps to the WRITER max and counts once');
  check(values[0].lead == %[3]d && values[0].trail == %[4]d,
      'the bracketing fields moved: lead=${values[0].lead} trail=${values[0].trail}');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.duplicate == 0,
      'a counter that must not move did: ${why(report)}');`, v, want.Clamped, lead, trail)
	out, err := runVersionProbe(t, dartBin, "VNEW_range_widen", []string{"VOLD_range_widen"}, 0,
		filepath.Join(corpus, fixedvector.RangeWidenHostile), body)
	if err != nil {
		t.Fatalf("range_widen_hostile: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
