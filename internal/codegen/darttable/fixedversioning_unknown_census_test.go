package darttable

import (
	"path/filepath"
	"testing"
)

// §5.8 row 11, `unknown_census`, on the DART leg: the NEW build reads
// `old_unknown_census.bin` through a HANDED-IN lineage entry (§5.1 refuses the
// removal, so no lock). The bytes are the C++ reference's own: one record,
// four `items`, `a` = 10..13 and `drop` = 900..903.
//
// THE ENTRY IS UNLAWFUL AND IS REFUSED, NEVER COUNTED (docs/FIXED-FORM-ALGORITHM.md
// §5.4, §5.9 #30; the Go reference's TestFixedVersioningUnknownCensus): the
// compile lays no plan for a writer field no reader field names, so the load
// refuses layout_malformed (this leg's layoutDamaged) before any record lands,
// moves no counter, leaves every row handed in at its declared defaults
// (SPEC-TABLES §6.5), and a SECOND read refuses the same way. This row once
// asserted the retired census, unknown == 1.
func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	body := `  final data = File(FILE).readAsBytesSync();
  values[0].lead = 99; // poisoned: a refusal resets every row handed in
  values[0].items[0].a = 77;
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1 && report.refused == t.TableFixedRefusal.layoutDamaged,
      'the unlawful lineage entry was not refused layout_malformed: n=$n ${why(report)}');
  check(report.unknown == 0 && report.widened == 0,
      'a refusal moved a counter: ${why(report)}');
  check(values[0].lead == 1 && values[0].trail == 2 && values[0].items[0].a == 0,
      'a refusal left a row off its declared defaults: lead=${values[0].lead} trail=${values[0].trail} items[0].a=${values[0].items[0].a}');
  // THE SECOND READ IS REFUSED THE SAME WAY: the lane's refusal is the build's.
  final values2 = <t.Census>[for (var i = 0; i < 8; i++) t.Census()];
  for (final v in values2) {
    t.initCensus(v);
  }
  final plan2 = t.censusFixedNewPlan();
  final report2 = t.TableFixedReport();
  final n2 = LOAD(values2, values2.length, data, data.length, plan2, report2);
  check(n2 == -1 && report2.refused == t.TableFixedRefusal.layoutDamaged && report2.unknown == 0,
      'the second read was not refused layout_malformed: n=$n2 ${why(report2)}');
`
	out, err := runVersionProbe(t, dartBin, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		filepath.Join(corpus, "old_unknown_census.bin"), body)
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
