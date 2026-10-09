package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// §5.8 row 11, `unknown_census`: the NEW build reads `old_unknown_census.bin`
// through a HANDED-IN lineage entry (§5.1 refuses the removal, so no lock).
// The forged bytes are the C++ reference's own — one record, four `items`,
// `a` = 10..13 and `drop` = 900..903 — the divergence is in the COUNTING, not
// damaged data. `r.Unknown` is asserted EXACTLY `== 1` because `Item.drop` is
// one field of one peer, and a leg that counts once per element lands `4` and
// passes `>= 1`; that `4` is the whole reason this row exists.
func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_unknown_census")
	newer := readSchema(t, "VNEW_unknown_census")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestUnknownCensus(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("the newer reader did not read the older writer's one record: n=%%d %%+v", n, r)
	}
	if r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("a clean NEW-READS-OLD is not a refusal: %%+v", r)
	}
	if r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Fatalf("counters that must not move did: %%+v", r)
	}
	if r.Unknown != 1 {
		t.Fatalf("unknown == %%d, want 1: Item.drop is ONE field of ONE peer, not one per element", r.Unknown)
	}
	if back[0].Lead != 1 || back[0].Trail != 2 {
		t.Fatalf("the bracketing fields moved: %%+v", back[0])
	}
	want := []int32{10, 11, 12, 13}
	for k := range want {
		if back[0].Items[k].A != want[k] {
			t.Fatalf("items[%%d].a == %%d, want %%d (%%+v)", k, back[0].Items[k].A, want[k], back[0])
		}
	}
	back2 := make([]%[2]s, 8)
	plan2 := make([]TableFixedEntry, 4096)
	var r2 TableReport
	n2 := %[2]sFixedLoad(back2, data, plan2, &r2)
	if n2 != 1 || r2.Unknown != 1 || r2.Malformed || r2.Verdict == TableOpenRefused {
		t.Fatalf("the second read owes the same census: n=%%d %%+v", n2, r2)
	}
}
`, filepath.Join(corpus, "old_unknown_census.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
