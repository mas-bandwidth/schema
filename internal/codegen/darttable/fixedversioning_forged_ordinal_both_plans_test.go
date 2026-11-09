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
// Under the one read (docs/plans/strict-only.md) BOTH plans refuse the whole
// read as out_of_range and nothing lands. THE CONTROL reads the unforged file
// through the same two plans and lands Gold, so the refusal is the forge's.

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
  check(n == -1, 'a forged ordinal refuses the whole read: n=$n ${why(report)}');
  check(report.refused == t.TableFixedRefusal.outOfRange,
      'the refusal is named out_of_range: ${why(report)}');
  check(values[0].tier != 4, 'a forged ordinal never lands Platinum: ${values[0].tier}');
  check(values[0].seq != 9, 'nothing lands, not even the neighbour: ${values[0].seq}');
  check(report.unknown == 0 && report.widened == 0,
      'a refusal moves no counter: ${why(report)}');`
	control := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'CONTROL: the unforged file reads: n=$n ${why(report)}');
  check(report.refused == 0, 'CONTROL: and refuses nothing: ${why(report)}');
  check(values[0].tier == 3 && values[0].seq == 9,
      'CONTROL: Gold and its neighbour land: ${values[0].tier} ${values[0].seq}');`
	for _, plan := range []struct {
		name, build string
		lineage     []string
	}{
		{"COMPILED", "VNEW_enum_append", []string{"VOLD_enum_append"}},
		{"IDENTITY", "VOLD_enum_append", nil},
	} {
		if out, err := runVersionProbe(t, dartBin, plan.build, plan.lineage, 0, old, control); err != nil {
			t.Fatalf("the %s plan read of the unforged file (the control): %v\n%s", plan.name, err, out)
		}
	}

	out, err := runVersionProbe(t, dartBin, "VNEW_enum_append", []string{"VOLD_enum_append"}, 0, forged, body)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged ordinal: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, dartBin, "VOLD_enum_append", nil, 0, forged, body)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged ordinal: %v\n%s", err, out)
	}
}
