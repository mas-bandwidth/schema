package darttable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// union_tag_both_plans forges a union TAG, not an ordinal: it overwrites the
// ONE byte that carries old_union_append.bin's r0.pick.type with 9, a tag past
// the OLD writer's two arms AND the NEW reader's three. The tag is one byte, so
// the nine-byte needle (tag 1, then the alpha arm's m=7, then seq=15) must
// occur EXACTLY once in the file — zero or twice is a failure by name, because
// a search that is not a locator is not a forge. The SAME forged bytes are read
// through BOTH plans, the COMPILED plan (reader VNEW, older VOLD) and the
// IDENTITY plan (reader VOLD, no older), and both must refuse the whole read
// as out_of_range (docs/plans/strict-only.md). The bound this row guards — the
// decode default arm in fixeddart.go and the None const in the run loop — is
// held on both plans; THE CONTROL reads the unforged file through the same two
// plans and lands the alpha arm, so the refusal is the forge's.

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)

	old := filepath.Join(corpus, "old_union_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// The manifest says old_union_append.bin holds r0.pick.type=1,
	// r0.pick.alpha.m=7, r0.seq=15: the union tag (ONE byte) followed by the
	// alpha arm's int32 and then the neighbour int32, all little-endian.
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("pick.type=1 followed by alpha.m=7 and seq=15 is not in %s", old)
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatalf("pick.type=1 followed by alpha.m=7 and seq=15 occurs more than once in %s", old)
	}
	data[at] = 9 // the forge: a tag past the OLD writer's two arms AND the NEW reader's three
	forged := filepath.Join(t.TempDir(), "forged_union_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == -1, 'a forged tag refuses the whole read: n=$n ${why(report)}');
  check(report.refused == t.TableFixedRefusal.outOfRange,
      'the refusal is named out_of_range: ${why(report)}');
  check(values[0].pick.type == 0,
      'nothing lands, not the forged 9 nor the writer 1: ${values[0].pick.type}');
  check(report.unknown == 0 && report.widened == 0,
      'a refusal moves no counter: ${why(report)}');`
	control := `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, values.length, data, data.length, plan, report);
  check(n == 1, 'CONTROL: the unforged file reads: n=$n ${why(report)}');
  check(report.refused == 0, 'CONTROL: and refuses nothing: ${why(report)}');
  check(values[0].pick.type == 1 && values[0].seq == 15,
      'CONTROL: the alpha arm and its neighbour land: ${values[0].pick.type} ${values[0].seq}');`
	for _, plan := range []struct {
		name, build string
		lineage     []string
	}{
		{"COMPILED", "VNEW_union_append", []string{"VOLD_union_append"}},
		{"IDENTITY", "VOLD_union_append", nil},
	} {
		if out, err := runVersionProbe(t, dartBin, plan.build, plan.lineage, 0, old, control); err != nil {
			t.Fatalf("the %s plan read of the unforged file (the control): %v\n%s", plan.name, err, out)
		}
	}

	out, err := runVersionProbe(t, dartBin, "VNEW_union_append", []string{"VOLD_union_append"}, 0, forged, body)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged tag: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, dartBin, "VOLD_union_append", nil, 0, forged, body)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged tag: %v\n%s", err, out)
	}
}
