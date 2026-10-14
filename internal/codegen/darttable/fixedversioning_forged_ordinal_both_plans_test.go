package darttable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// forged_ordinal_both_plans is §5.8 row 12 of the fixed-form versioning law:
// VOLD_/VNEW_enum_append read the SAME forged bytes, once through the NEW
// build's COMPILED plan (selected by the lineage) and once through the OLD
// build's IDENTITY plan (its own hash). The forge turns old_enum_append.bin's
// r0.tier from 3 (Gold) into 4, an ordinal past the writer's three variants.
// `clamped` is asserted EXACTLY `== 1` on both plans, never `>= 1`: a leg that
// counts in the `ordinal` op as well lands 2 and passes a looser read — the
// row exists to catch that double count.

func TestFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)

	old := filepath.Join(corpus, "old_enum_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// The manifest says old_enum_append.bin holds r0.tier=3,r0.seq=9: the enum
	// ordinal (one byte) followed by its neighbour, the int32 scalar, LE.
	needle := []byte{3, 9, 0, 0, 0}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("tier=3 followed by seq=9 is not in %s", old)
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatalf("tier=3 followed by seq=9 occurs more than once in %s", old)
	}
	data[at] = 4 // the forge
	forged := filepath.Join(t.TempDir(), "hostile_enum_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'the forged file carries one record: n=$n ${why(report)}');
  check(report.refused == 0 && !report.malformed,
      'a forged ordinal is not a refusal and not malformed: ${why(report)}');
  check(values[0].tier == 0, 'a forged ordinal lands None: ${values[0].tier}');
  check(values[0].tier != 4, 'a forged ordinal never lands Platinum: ${values[0].tier}');
  check(values[0].seq == 9, 'the scalar after the enum is untouched: ${values[0].seq}');
  check(report.clamped == 1,
      'the forged ordinal is counted exactly once: ${why(report)}');
  check(report.unknown == 0 && report.kindMismatch == 0 && report.widened == 0 &&
      report.duplicate == 0,
      'no other counter moved: ${why(report)}');`

	out, err := runVersionProbe(t, dartBin, "VNEW_enum_append", []string{"VOLD_enum_append"}, 0, forged, body)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged ordinal: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, dartBin, "VOLD_enum_append", nil, 0, forged, body)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged ordinal: %v\n%s", err, out)
	}
}
