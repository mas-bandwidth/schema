package cpptable

// §5.8 row cfloat_res_refine: `aim float32 | min = -1, max = 1, resolution =
// 0.1` on the OLD side, `resolution = 0.01` on the NEW — the step REFINED, this
// form's widening. This row takes NO hostile file ("Hostile rows": a value off
// the old grid is still a float32 the reader lands; a moved step is the LOCK's
// and HASH's refusal, not a counter's). The NEW build reads
// old_cfloat_res_refine.bin: `aim` lands EXACTLY the old writer's 0x3E99999A —
// 0.3 on the 0.1 grid is a whole multiple of 0.01, so nothing requantizes — and
// `clamped` is asserted `== 0` EXACTLY: a leg that requantizes through its own
// finer step lands `clamped == 1` (and 0x3E999998), which a `clamped <= 1` read
// would pass.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := cppFixedCorpus(t)
	// THE NUMBER COMES FROM THE CORPUS MANIFEST, not from a literal here
	// (schema#1164). This leg already compared BITS, which is the right
	// comparison; what it did not do is take the number from the REFERENCE.
	aim := fmt.Sprintf("0x%08X", cppManifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim"))
	body := `
    if ( n != 1 ) { printf( "cfloat_res_refine: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1u || back[0].trail != 2u )
        { printf( "cfloat_res_refine: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    {
        uint32_t bits = 0;
        memcpy( &bits, &back[0].aim, sizeof( back[0].aim ) );
        if ( bits != @AIM@u ) { printf( "cfloat_res_refine: aim did not land BIT-EXACT: 0x%08X, the manifest says @AIM@\n", (unsigned) bits ); return 1; }
    }
    if ( r.clamped != 0 ) { printf( "cfloat_res_refine: clamped is not exactly 0: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_res_refine: a counter moved on a clean backward read: unknown=%d km=%d w=%d dup=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_res_refine: a clean read refused: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
`
	out, err := cppRunVersionProbe(t, "cfloat_res_refine",
		filepath.Join(corpus, "old_cfloat_res_refine.bin"), strings.ReplaceAll(body, "@AIM@", aim), "")
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
