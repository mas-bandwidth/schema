package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// §5.8 row `cfloat_res_refine`: the NEW build reads old_cfloat_res_refine.bin, the
// OLD writer's `aim` at resolution 0.1 against the NEW reader's 0.01, range held.
// This row takes NO forged bytes — a value off the old grid is still a float32 the
// reader lands, and a moved step's refusal is the lock's and the hash's, not a
// counter's. `aim` is asserted EXACTLY by bits (0x3E99999A) and `Clamped` EXACTLY
// `== 0`: 0.1 is a whole multiple of 0.01, so a reader that requantized onto its
// own grid would move those bits, and a `>= 0` clamp check would pass the read this
// row exists to catch.
func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_cfloat_res_refine")
	newer := readSchema(t, "VNEW_cfloat_res_refine")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("math"; "os"; "testing")

func TestCfloatResRefine(t *testing.T) {
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
	if back[0].Lead != 1 || back[0].Trail != 2 {
		t.Fatalf("the bracketing fields moved: %%+v", back[0])
	}
	if bits := math.Float32bits(back[0].Aim); bits != 0x3E99999A {
		t.Fatalf("aim moved: %%#x, want 0x3E99999A (%%v)", bits, back[0].Aim)
	}
	if r.Clamped != 0 {
		t.Fatalf("clamped %%d, want 0: a whole-multiple refinement requantizes nothing", r.Clamped)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("a counter that must not move did: %%+v", r)
	}
}
`, filepath.Join(corpus, "old_cfloat_res_refine.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
