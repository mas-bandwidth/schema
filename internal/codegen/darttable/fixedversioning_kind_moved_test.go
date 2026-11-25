package darttable

import (
	"path/filepath"
	"testing"
)

// kind_moved, the second half of the UNLAWFUL lineage entry, on the DART leg:
// the OLD writer carries `v int32`, the NEW reader declares `v float32`, a kind
// off every ladder (§5.1 "ladder"). EMIT refuses a kind that moved rather than
// reinterpreting it or counting it and reading on (docs/FIXED-FORM-ALGORITHM.md
// §5.2 EMIT, §5.4, §5.9 #30; the Go reference's c.unlawful = 1): no plan is
// laid, the load refuses layout_malformed (this leg's layoutDamaged), no
// counter moves, and every row handed in is back at its declared defaults
// (SPEC-TABLES §6.5: lead = 1, v = 0, trail = 2). Both kinds are four bytes,
// so a reinterpreting leg lands the bits of int32 7 as a float and a leg that
// skips the field lands 0.0 with the brackets read: either loads, and this
// probe is red. A SECOND load, into rows poisoned again, refuses the same way.
func TestFixedVersioningKindMoved(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	body := `  final data = File(FILE).readAsBytesSync();
  for (var pass = 0; pass < 2; pass++) {
    for (final v in values) {
      v.lead = 99; // poisoned: a refusal resets every row handed in
      v.v = 5.5;
      v.trail = 88;
    }
    report.reset();
    final n = LOAD(values, values.length, data, data.length, plan, report);
    check(n == -1 && report.refused == t.TableFixedRefusal.layoutDamaged,
        'load $pass: the kind that moved was not refused layout_malformed: n=$n ${why(report)}');
    check(report.unknown == 0 && report.widened == 0,
        'load $pass: a refusal moved a counter: ${why(report)}');
    for (var k = 0; k < values.length; k++) {
      check(values[k].lead == 1 && values[k].v == 0.0 && values[k].trail == 2,
          'load $pass: row $k is off its declared defaults after the refusal: '
          'lead=${values[k].lead} v=${values[k].v} trail=${values[k].trail}');
    }
  }
`
	out, err := runVersionProbe(t, dartBin, "VNEW_kind_moved", []string{"VOLD_kind_moved"}, 0,
		filepath.Join(corpus, "old_kind_moved.bin"), body)
	if err != nil {
		t.Fatalf("kind_moved: %v\n%s", err, out)
	}
}
