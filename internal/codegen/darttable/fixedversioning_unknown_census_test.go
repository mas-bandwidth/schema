package darttable

import (
	"path/filepath"
	"testing"
)

// §5.8 row 11, `unknown_census`, on the DART leg: the NEW build reads
// `old_unknown_census.bin` through a HANDED-IN lineage entry (§5.1 refuses the
// removal, so no lock). The forged bytes are the C++ reference's own — one
// record, four `items`, `a` = 10..13 and `drop` = 900..903 — and the divergence
// this row exists for is in the COUNTING, not in damaged data.
//
// `report.unknown` is asserted EXACTLY `== 1` because `Item.drop` is ONE field
// of ONE peer (§5.4: the census is once per field per peer, and §5.9 #6 lands
// it once per read that returns). A leg that censuses inside the array's
// element loop lands `4` and would pass any `>= 1`; that `4` is the whole
// reason this row is a file of its own.
func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	body := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the newer reader did not read the older writer one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a clean NEW-READS-OLD is not a refusal: ${why(report)}');
  check(
    report.kindMismatch == 0 && report.widened == 0 && report.clamped == 0 && report.duplicate == 0,
    'counters that must not move did: ${why(report)}',
  );
  check(report.unknown == 1,
      'unknown == ${report.unknown}, want 1: Item.drop is ONE field of ONE peer, not one per element');
  check(values[0].lead == 1 && values[0].trail == 2,
      'the bracketing fields moved: lead=${values[0].lead} trail=${values[0].trail}');
  for (var k = 0; k < 4; k++) {
    check(values[0].items[k].a == 10 + k,
        'items[$k].a == ${values[0].items[k].a}, want ${10 + k}');
  }
  // THE SECOND READ OWES THE SAME CENSUS. The census is the plan's own number,
  // carried onto a report ONCE per read that returns, so a second read into a
  // fresh report reads 1 again — never 2, and never the element count.
  final values2 = <t.Census>[for (var i = 0; i < 8; i++) t.Census()];
  for (final v in values2) {
    t.initCensus(v);
  }
  final plan2 = t.censusFixedNewPlan();
  final report2 = t.TableFixedReport();
  final n2 = LOAD(values2, values2.length, data, data.length, plan2, report2);
  check(n2 == 1 && report2.unknown == 1 && report2.refused == 0 && !report2.malformed,
      'the second read owes the same census: n=$n2 ${why(report2)}');
`
	out, err := runVersionProbe(t, dartBin, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		filepath.Join(corpus, "old_unknown_census.bin"), body)
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
