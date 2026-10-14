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
// counter's. `aim` is asserted EXACTLY BY BITS and `Clamped` EXACTLY `== 0`: 0.1
// is a whole multiple of 0.01, so a reader that requantized onto its own grid
// would move those bits, and a `>= 0` clamp check would pass the read this row
// exists to catch.
//
// THE BITS COME FROM THE CORPUS MANIFEST AND NOT FROM A LITERAL (schema#1164,
// Glenn 2026-09-19: "do whatever is needed to make sure that a new reader can
// read an old writer"). The manifest carries `r0.aim=0.300000012|0x3E99999A`,
// and until now every leg's compressed-float row hardcoded that number instead
// — an oracle written by the same hand as the assertion. `manifestFloatBits`
// reads the reference's own answer and fails loudly if the manifest, the row or
// the value is missing, because a float oracle that can degrade to "not
// checked" is the vacuity this repairs.
func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_cfloat_res_refine")
	newer := readSchema(t, "VNEW_cfloat_res_refine")
	table := fixedRootName(t, older)
	aim := manifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim")
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
	if bits := math.Float32bits(back[0].Aim); bits != %#[3]x {
		t.Fatalf("aim moved: %%#x, want %#[3]x from the corpus manifest (%%v)", bits, back[0].Aim)
	}
	if r.Clamped != 0 {
		t.Fatalf("clamped %%d, want 0: a whole-multiple refinement requantizes nothing", r.Clamped)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("a counter that must not move did: %%+v", r)
	}
}
`, filepath.Join(corpus, "old_cfloat_res_refine.bin"), table, aim)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
