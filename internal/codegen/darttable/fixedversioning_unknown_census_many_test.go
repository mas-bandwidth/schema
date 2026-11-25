package darttable

import (
	"path/filepath"
	"testing"
)

// `many_unknown_census.bin`: THREE records under the DELIBERATELY UNLAWFUL
// entry (Item.drop removed, §5.8 row 11). The entry is refused, never counted
// (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #30; the Go reference's
// TestFixedVersioningUnknownCensusManyRecords): the whole load refuses
// layout_malformed (this leg's layoutDamaged) before any of the three records
// lands, no counter moves, and every row handed in is at its declared
// defaults. This row once asserted the retired census, unknown == 1.
func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	body := `  final data = File(FILE).readAsBytesSync();
  for (var k = 0; k < 3; k++) {
    values[k].lead = 90 + k; // poisoned: a refusal resets every row handed in
  }
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1 && report.refused == t.TableFixedRefusal.layoutDamaged,
      'the unlawful lineage entry was not refused layout_malformed: n=$n ${why(report)}');
  check(report.unknown == 0 && report.widened == 0,
      'a refusal moved a counter: ${why(report)}');
  for (var k = 0; k < 3; k++) {
    check(values[k].lead == 1 && values[k].trail == 2 && values[k].items[0].a == 0,
        'record $k is off its declared defaults after the refusal: lead=${values[k].lead} trail=${values[k].trail}');
  }
`
	out, err := runVersionProbe(t, dartBin, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		filepath.Join(corpus, "many_unknown_census.bin"), body)
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
