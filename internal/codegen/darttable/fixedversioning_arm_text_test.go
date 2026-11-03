package darttable

import (
	"path/filepath"
	"strings"
	"testing"
)

// The independent C++ FU1 corpus keeps text between scalars in arm two.
// FU1 reads by identity; FU2's locked FU1 lineage uses a compiled plan.
func TestFixedVersioningSecondArmText(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	for _, reader := range []string{"FU1", "FU2"} {
		t.Run(reader, func(t *testing.T) {
			var older []string
			extra := ""
			widened := "0"
			if reader == "FU2" {
				older = []string{"FU1"}
				widened = "6"
				extra = `
  for (var i = 0; i < 3; i++) {
    check(values[i].extra == 11, 'appended default on record $i');
  }
`
			}
			body := `
  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 3 && report.refused == 0 && !report.malformed,
      'three independent records: n=$n ' + why(report));
  check(report.unknown == 0 && report.kindMismatch == 0 &&
      report.clamped == 0 && report.duplicate == 0 &&
      report.widened == WIDENED, 'exact report ' + why(report));
  check(values[0].flag && values[0].notePresent && values[0].note == 44 &&
      values[0].tail == 11, 'first record scalar/optional neighbours');
  check(values[0].pick.type == 2 &&
      values[0].pick.labelled.lead == 101 &&
      values[0].pick.labelled.trail == 202 &&
      values[0].pick.labelled.labelLength == 5 &&
      String.fromCharCodes(values[0].pick.labelled.label.take(5)) == 'hello',
      'second-arm text and both neighbours');
  check(!values[1].flag && !values[1].notePresent && values[1].tail == 12 &&
      values[1].pick.type == 1 && values[1].pick.plain.n == 303,
      'first arm and absent optional after second arm');
  check(values[2].pick.type == 2 && values[2].pick.labelled.lead == -5 &&
      values[2].pick.labelled.labelLength == 0 &&
      values[2].pick.labelled.trail == 606 &&
      values[2].note == -7 && values[2].tail == 13,
      'empty second-arm text on reused image');
  const marks = [-1, -32768, 32767];
  for (var i = 0; i < 3; i++) {
    check(values[i].mark == marks[i], 'signed width record $i');
  }
  check(values[0].heat > 0 && values[0].heat < 1e-40 &&
      values[1].heat == 0 && values[1].heat.isNegative &&
      values[2].heat == 1.5, 'float widening preserves subnormal and minus zero');
`
			body = strings.ReplaceAll(body, "WIDENED", widened) + extra
			if reader == "FU2" {
				body += `
  // Corrupt the arm guard on the compiled copy covering the text and
  // its neighbours. Identical text bounds coalesce this span into a copy.
  var changed = 0;
  for (final locked in t.fuRootFixedLineagePlans) {
    for (var i = 0; i < locked.count; i++) {
      final b = i * t.TableFixedLane.lanes;
      if (locked.entries[b + t.TableFixedLane.op] == t.TableFixedOp.copy &&
          locked.entries[b + t.TableFixedLane.arg] == 2) {
        locked.entries[b + t.TableFixedLane.arg] =
            1;
        locked.entries[b + t.TableFixedLane.meta] = 0;
        changed++;
      }
    }
  }
  check(changed == 1, 'guard control touched exactly one second-arm copy');
  final broken = <t.FuRoot>[for (var i = 0; i < 3; i++) t.FuRoot()];
  for (final v in broken) { t.initFuRoot(v); }
  final brokenReport = t.TableFixedReport();
  final brokenN = LOAD(broken, broken.length, data, data.length,
      t.fuRootFixedNewPlan(), brokenReport);
  check(brokenN == 3 && brokenReport.refused == 0 && !brokenReport.malformed,
      'wrong guard stays silently readable');
  check(broken[0].pick.type == 2 &&
      broken[0].pick.labelled.labelLength == 0,
      'wrong guard must lose the live text');
`
			}
			out, err := runVersionProbe(t, dartBin, reader, older, 0,
				filepath.Join(corpus, "fu1.bin"), body)
			if err != nil {
				t.Fatalf("second arm text: %v\n%s", err, out)
			}
		})
	}
}
