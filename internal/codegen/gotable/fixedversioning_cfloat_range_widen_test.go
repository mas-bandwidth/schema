package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// §5.8 row `cfloat_range_widen`: the NEW reader widens aim's range from [-1, 1]
// to [-2, 2] and reads three files. old_ is the OLD writer's 0.5, which lands
// whole: the control, a writer range NARROWER than the reader's that the value
// keeps. hostile_ is the old bytes with aim FORGED to 1.5 — outside the OLD
// writer's range, inside the NEW reader's — and REFUSES the whole load
// out_of_range, because on a cross-version read a compressed float is held to
// the WRITER's range the lineage plan carries, like every ranged kind
// (docs/rulings.json `strict-only-read`, which overturned schema#1164's reading
// that the pass was the reader's; docs/FIXED-FORM-ALGORITHM.md §4.6). past_ is
// the same bytes forged to 5.0, outside both ranges, and refuses the same way.
// hostile_ is the read that proves the writer's range is held at all: past_
// alone refuses under the reader's range too.
//
// old_'s 0.5 is the manifest's `values=r0.aim`, never a literal.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_cfloat_range_widen")
	newer := readSchema(t, "VNEW_cfloat_range_widen")
	table := fixedRootName(t, older)
	oldAim := manifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim")
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
	if r.Reason != "" || (r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed") || r.Verdict == TableOpenRefused {
		t.Fatalf("old: a clean NEW-READS-OLD is not a refusal: %%+v", r)
	}
	if back[0].Lead != 1 || back[0].Trail != 2 {
		t.Fatalf("old: the bracketing fields moved: %%+v", back[0])
	}
	if bits := math.Float32bits(back[0].Aim); bits != %#[5]x {
		t.Fatalf("old: aim moved: %%#x, want %#[5]x from the corpus manifest (%%v)", bits, back[0].Aim)
	}
	if r.Refused() {
		t.Fatalf("old: refused %%d, want none", r.Refusal)
	}
	if r.Unknown != 0 || r.Widened != 0 {
		t.Fatalf("old: a counter that must not move did: %%+v", r)
	}

	for _, c := range []struct {
		name string
		data []byte
	}{{"hostile", hostileData}, {"past", pastData}} {
		r = TableReport{}
		n = %[4]sFixedLoad(back, c.data, plan, &r)
		if n != -1 {
			t.Fatalf("%%s: a value past the WRITER's range must refuse the whole load, not read %%d records (%%+v)", c.name, n, r)
		}
		if r.Verdict != TableOpenRefused || r.Reason != "out_of_range" || !r.FixedRaised {
			t.Fatalf("%%s: expected out_of_range refusal, got %%+v", c.name, r)
		}
		if r.Unknown != 0 || r.Widened != 0 {
			t.Fatalf("%%s: a counter that must not move did: %%+v", c.name, r)
		}
	}
}
`, filepath.Join(corpus, "old_cfloat_range_widen.bin"),
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"),
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), table, oldAim)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("cfloat_range_widen: %v\n%s", err, out)
	}
}
