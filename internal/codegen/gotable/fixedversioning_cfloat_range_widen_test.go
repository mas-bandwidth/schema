package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// §5.8 row `cfloat_range_widen`: the NEW reader widens aim's range from [-1, 1]
// to [-2, 2] and reads three files. old_ is the OLD writer's 0.5, which lands
// whole. hostile_ is the old bytes with aim FORGED to 1.5 — outside the OLD
// writer's range, inside the NEW reader's — and lands 1.5 WHOLE because the
// bounds pass on a read is the READER's, not the writer's. past_ is the same
// bytes forged to 5.0, outside the reader's range too, and lands the READER's
// own max 2.0. Clamped is asserted EXACTLY (0, 0, 1), never >=: hostile_ alone
// reads the same whether the pass is the reader's or missing entirely, so past_
// is the read that proves there is a pass at all.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_cfloat_range_widen")
	newer := readSchema(t, "VNEW_cfloat_range_widen")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("math"; "os"; "testing")

func TestCfloatRangeWiden(t *testing.T) {
	oldData, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	hostileData, err := os.ReadFile(%[2]q)
	if err != nil {
		t.Fatal(err)
	}
	pastData, err := os.ReadFile(%[3]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[4]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport

	n := %[4]sFixedLoad(back, oldData, plan, &r)
	if n != 1 {
		t.Fatalf("old: the newer reader did not read the older writer's one record: n=%%d %%+v", n, r)
	}
	if r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("old: a clean NEW-READS-OLD is not a refusal: %%+v", r)
	}
	if back[0].Lead != 1 || back[0].Trail != 2 {
		t.Fatalf("old: the bracketing fields moved: %%+v", back[0])
	}
	if bits := math.Float32bits(back[0].Aim); bits != 0x3F000000 {
		t.Fatalf("old: aim moved: %%#x, want 0x3F000000 (%%v)", bits, back[0].Aim)
	}
	if r.Clamped != 0 {
		t.Fatalf("old: clamped %%d, want 0", r.Clamped)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("old: a counter that must not move did: %%+v", r)
	}

	n = %[4]sFixedLoad(back, hostileData, plan, &r)
	if n != 1 {
		t.Fatalf("hostile: the forged file reads one record, not %%d (%%+v)", n, r)
	}
	if r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("hostile: a forged read is not a refusal: %%+v", r)
	}
	if back[0].Lead != 1 || back[0].Trail != 2 {
		t.Fatalf("hostile: the bracketing fields moved: %%+v", back[0])
	}
	if bits := math.Float32bits(back[0].Aim); bits != 0x3FC00000 {
		t.Fatalf("hostile: aim landed %%#x, want 0x3FC00000 (%%v)", bits, back[0].Aim)
	}
	if r.Clamped != 0 {
		t.Fatalf("hostile: clamped %%d, want 0", r.Clamped)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("hostile: a counter that must not move did: %%+v", r)
	}

	n = %[4]sFixedLoad(back, pastData, plan, &r)
	if n != 1 {
		t.Fatalf("past: the forged file reads one record, not %%d (%%+v)", n, r)
	}
	if r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("past: a forged read is not a refusal: %%+v", r)
	}
	if back[0].Lead != 1 || back[0].Trail != 2 {
		t.Fatalf("past: the bracketing fields moved: %%+v", back[0])
	}
	if bits := math.Float32bits(back[0].Aim); bits != 0x40000000 {
		t.Fatalf("past: aim landed %%#x, want 0x40000000 (%%v)", bits, back[0].Aim)
	}
	if r.Clamped != 1 {
		t.Fatalf("past: clamped %%d, want 1", r.Clamped)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("past: a counter that must not move did: %%+v", r)
	}
}
`, filepath.Join(corpus, "old_cfloat_range_widen.bin"),
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"),
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("cfloat_range_widen: %v\n%s", err, out)
	}
}
