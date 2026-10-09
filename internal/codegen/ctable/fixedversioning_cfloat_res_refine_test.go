package ctable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// cfloat_res_refine (docs/FIXED-FORM-VERSIONING-TESTS.md, the rows table):
// the NEW reader's `aim` resolution REFINES 0.1 -> 0.01, this form's widening.
// The forged bytes are NONE — a value off the old grid is still a float32 the
// reader lands, and a moved step is the LOCK's and the HASH's refusal, not a
// counter's. `clamped` is asserted == 0 EXACTLY: the old 0.3 sits on a whole
// multiple of 0.01 so nothing requantizes, and a leg that re-quantized anyway
// would move `clamped` to 1 — a looser read would pass that re-quantize-and-count bug.
//
// `aim` IS COMPARED BY BITS, AGAINST THE CORPUS MANIFEST (schema#1164, Glenn
// 2026-09-19: "do whatever is needed to make sure that a new reader can read an
// old writer"). It was `back[0].aim != 0.3f` — a float comparison against a
// HARDCODED literal, an oracle written by the same hand as the code under test,
// and one that cannot tell 0.3 from the float32 beside it. The manifest carries
// the reference's own answer, bits and all.
func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_cfloat_res_refine")
	older := cReadSchema(t, "VOLD_cfloat_res_refine")
	aim := cManifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim")

	body := fmt.Sprintf(`
    if ( n != 1 ) { printf( "cfloat_res_refine: n == %%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "cfloat_res_refine: a bracket moved: lead=%%u trail=%%u\n", back[0].lead, back[0].trail ); return 1; }
    { uint32_t aimbits = 0; memcpy( &aimbits, &back[0].aim, 4 ); if ( aimbits != %#[1]xu ) { printf( "cfloat_res_refine: aim is %%#x, not the manifest's %#[1]x\n", aimbits, %#[1]xu ); return 1; } }
    if ( r.clamped != 0 ) { printf( "cfloat_res_refine: clamped == %%d, not 0\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_res_refine: a counter moved on a clean backward read: u=%%d km=%%d w=%%d d=%%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_res_refine: a clean read refused: malformed=%%d refused=%%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "cfloat_res_refine: reason == %%d, not 0\n", r.reason ); return 1; }
`, aim)

	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "old_cfloat_res_refine.bin"), body, "")
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
