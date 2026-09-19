package darttable

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag did
// not select is UNDEFINED — one word, for every target (Glenn, 2026-09-19: "as
// designed it is 'undefined'"). A union read DEFINES the tag and the SELECTED arm
// and nothing else, and a consumer reads the selected arm only. THE ROW'S WHOLE
// CONTENT IS THE ASSERTION IT REFUSES TO MAKE: this test does NOT compare
// pick.beta or pick.gamma. That Dart happens to leave the caller's bytes in an
// unselected arm is an OBSERVATION AND NOT A GUARANTEE, and nothing may be relied
// on it. The poison here is laid field by field on the destination objects, not
// through the record image. A future reader who "completes" this test by asserting
// pick.gamma.p == 0 has reversed a ruling and should read the issue first. Named
// *_arm_row_test.go (not *_arm_test.go): Go reads a trailing _arm before _test.go
// as a GOARCH build constraint and silently skips the file.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE SELECTED ARM AND THE TAG ARE THE WHOLE OF WHAT A UNION READ PROMISES.
	// Nothing here names beta or gamma.
	checks := `  check(n == 1, 'the file carries one record, not $n: ${why(report)}');
  check(values[0].pick.type == 1,
      'pick.type is the writer alpha arm (1), not ${values[0].pick.type}');
  check(values[0].pick.alpha.m == 7,
      'pick.alpha.m is the writer 7, not ${values[0].pick.alpha.m}');
  check(values[0].seq == 15,
      'seq is the writer 15, not ${values[0].seq}');
  check(report.clamped == 0 && report.unknown == 0 && report.kindMismatch == 0 &&
      report.widened == 0 && report.duplicate == 0,
      'a clean read moved a counter: ${why(report)}');
  check(!report.malformed, 'a clean backward read is not malformed: ${why(report)}');
  check(report.refused == 0, 'a clean backward read is not a refusal: ${why(report)}');`

	// probe 1, the compiled column: reader VNEW, older VOLD. The poison is laid
	// field by field on the destination value, because the question is what the
	// image-to-value scatter does to an unselected arm.
	compiled := fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
  values[0].pick.type = 0x5A5A5A5A;
  values[0].pick.alpha.m = 0x5A5A5A5A;
  values[0].pick.beta.n = 0x5A5A5A5A;
  values[0].pick.gamma.p = 0x5A5A5A5A;
  values[0].seq = 0x5A5A5A5A;
  final n = LOAD(values, values.length, data, data.length, plan, report);
%s
`, checks)
	if out, err := runVersionProbe(t, dartBin, "VNEW_union_append", []string{"VOLD_union_append"}, 0, file, compiled); err != nil {
		t.Fatalf("union_unselected_arm, compiled column: %v\n%s", err, out)
	}

	// probe 2, the identity column: reader VOLD, no older. Its own hash, its own
	// plan, the same file, the same poison — without the arm the old build does
	// not declare.
	identity := fmt.Sprintf(`  final data = File(FILE).readAsBytesSync();
  values[0].pick.type = 0x5A5A5A5A;
  values[0].pick.alpha.m = 0x5A5A5A5A;
  values[0].pick.beta.n = 0x5A5A5A5A;
  values[0].seq = 0x5A5A5A5A;
  final n = LOAD(values, values.length, data, data.length, plan, report);
%s
`, checks)
	if out, err := runVersionProbe(t, dartBin, "VOLD_union_append", nil, 0, file, identity); err != nil {
		t.Fatalf("union_unselected_arm, identity column: %v\n%s", err, out)
	}
}
