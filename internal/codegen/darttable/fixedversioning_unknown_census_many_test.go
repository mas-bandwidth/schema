package darttable

import (
	"path/filepath"
	"testing"
)

// §5.4 says the compile census is once per peer AND NEVER PER RECORD. The
// landed one-record row reads `old_unknown_census.bin`, a single record, where
// "once per peer" and "once per record" are the SAME number (1) — so the second
// half of that clause has never been under a gate. `many_unknown_census.bin`
// carries THREE records of the same `Census` root, and only then does a
// per-record `Unknown++` land 3 instead of 1.
//
// `report.unknown` is asserted EXACTLY `== 1`: `Item.drop` is ONE field of ONE
// peer, so three records still owe ONE unknown — never 3, never the record count.
func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	body := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 3, 'the newer reader did not read the older writer three records: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a clean NEW-READS-OLD is not a refusal: ${why(report)}');
  check(
    report.kindMismatch == 0 && report.widened == 0 && report.clamped == 0 && report.duplicate == 0,
    'counters that must not move did: ${why(report)}',
  );
  check(report.unknown == 1,
      'unknown == ${report.unknown}, want 1: Item.drop is ONE field of ONE peer, not one per record');
  for (var k = 0; k < 3; k++) {
    check(values[k].lead == 1 && values[k].trail == 2,
        'record $k moved the bracketing fields: lead=${values[k].lead} trail=${values[k].trail}');
    for (var i = 0; i < 4; i++) {
      check(values[k].items[i].a == (k + 1) * 10 + i,
          'record $k items[$i].a == ${values[k].items[i].a}, want ${(k + 1) * 10 + i}');
    }
  }
`
	out, err := runVersionProbe(t, dartBin, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		filepath.Join(corpus, "many_unknown_census.bin"), body)
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
