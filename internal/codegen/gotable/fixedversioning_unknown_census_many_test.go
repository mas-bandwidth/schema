package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// docs/FIXED-FORM-ALGORITHM.md §5.4: the compile census lands once per peer
// AND never per record. The landed one-record row cannot close the second half
// of that clause — with one record, once-per-peer and once-per-record are the
// same number, 1 — so this row reads many_unknown_census.bin, THREE records of
// the same Census root, and `r.Unknown` is asserted EXACTLY `== 1`: Item.drop
// is one field of one peer, and a per-record Unknown++ would land 3 here.
func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_unknown_census")
	newer := readSchema(t, "VNEW_unknown_census")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestUnknownCensusMany(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 3 {
		t.Fatalf("the newer reader did not read the older writer's three records: n=%%d %%+v", n, r)
	}
	if r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("a clean NEW-READS-OLD is not a refusal: %%+v", r)
	}
	if r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Fatalf("counters that must not move did: %%+v", r)
	}
	if r.Unknown != 1 {
		t.Fatalf("unknown == %%d, want 1: the compile census is once per peer, never per record", r.Unknown)
	}
	want := [3][4]int32{{10, 11, 12, 13}, {20, 21, 22, 23}, {30, 31, 32, 33}}
	for i := 0; i < 3; i++ {
		if back[i].Lead != 1 || back[i].Trail != 2 {
			t.Fatalf("record %%d: the bracketing fields moved: %%+v", i, back[i])
		}
		for k := 0; k < 4; k++ {
			if back[i].Items[k].A != want[i][k] {
				t.Fatalf("record %%d items[%%d].a == %%d, want %%d (%%+v)", i, k, back[i].Items[k].A, want[i][k], back[i])
			}
		}
	}
	back2 := make([]%[2]s, 8)
	plan2 := make([]TableFixedEntry, 4096)
	var r2 TableReport
	n2 := %[2]sFixedLoad(back2, data, plan2, &r2)
	if n2 != 3 || r2.Unknown != 1 || r2.Malformed || r2.Verdict == TableOpenRefused {
		t.Fatalf("the second read owes the same census: n=%%d %%+v", n2, r2)
	}
}
`, filepath.Join(corpus, "many_unknown_census.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
