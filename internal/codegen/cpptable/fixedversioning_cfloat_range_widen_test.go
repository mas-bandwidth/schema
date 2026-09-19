package cpptable

// §5.8 row cfloat_range_widen: `aim float32 | min = -1, max = 1,
// resolution = 0.01` on the OLD side, `min = -2, max = 2` on the NEW — the
// range WIDENED. The bounds pass on a read is the READER's, not the writer's:
// old_cfloat_range_widen.bin lands 0.5 (0x3F000000) exactly, and
// hostile_cfloat_range_widen.bin — the old bytes with aim FORGED to 1.5,
// past the OLD writer's max 1.0 but inside the NEW reader's [-2, 2] — lands
// 1.5 WHOLE with clamp 0. Only past_cfloat_range_widen.bin (aim forged to
// 5.0, past the reader's own 2.0) clamps, to the READER's max 2.0, and the
// clamp counter is asserted EXACTLY 1, never >= 1. The hostile_ read alone
// is not enough: a reader with the emitter's float bounds pass deleted would
// still land 1.5 and only the past_ read would go red — that is what says
// there is a pass at all.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := cppFixedCorpus(t)
	// TWO OF THE THREE NUMBERS COME FROM THE CORPUS MANIFEST AND THE THIRD
	// CANNOT (schema#1164). `old_`'s is `values=r0.aim`, `hostile_`'s is the
	// manifest's own `forged=r0.aim@104` -- the value the reference wrote into
	// those bytes. `past_`'s 0x40000000 stays a literal and says why: the file
	// carries 5.0 and what lands is THIS READER'S OWN DECLARED MAX 2.0, which is
	// the whole content of that column.
	oldAim := fmt.Sprintf("0x%08X", cppManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"))
	hostileAim := fmt.Sprintf("0x%08X", cppManifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim"))

	oldBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen(old): n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1u || back[0].trail != 2u )
        { printf( "cfloat_range_widen(old): a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    {
        uint32_t bits = 0;
        memcpy( &bits, &back[0].aim, sizeof( back[0].aim ) );
        if ( bits != @AIM@u ) { printf( "cfloat_range_widen(old): aim did not land BIT-EXACT: 0x%08X, the manifest says @AIM@\n", (unsigned) bits ); return 1; }
    }
    if ( r.clamped != 0 ) { printf( "cfloat_range_widen(old): clamped is not exactly 0: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_range_widen(old): a counter moved on a clean backward read: unknown=%d km=%d w=%d dup=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_range_widen(old): a clean read refused: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
`
	out, err := cppRunVersionProbe(t, "cfloat_range_widen",
		filepath.Join(corpus, "old_cfloat_range_widen.bin"), strings.ReplaceAll(oldBody, "@AIM@", oldAim), "")
	if err != nil {
		t.Fatalf("cfloat_range_widen(old): %v\n%s", err, out)
	}

	hostileBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen(hostile): n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1u || back[0].trail != 2u )
        { printf( "cfloat_range_widen(hostile): a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    {
        uint32_t bits = 0;
        memcpy( &bits, &back[0].aim, sizeof( back[0].aim ) );
        if ( bits != @AIM@u ) { printf( "cfloat_range_widen(hostile): the manifest's own forged value did not land WHOLE: 0x%08X, want @AIM@\n", (unsigned) bits ); return 1; }
    }
    if ( r.clamped != 0 ) { printf( "cfloat_range_widen(hostile): clamped is not exactly 0: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_range_widen(hostile): a counter moved on a clean backward read: unknown=%d km=%d w=%d dup=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_range_widen(hostile): a clean read refused: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
`
	out, err = cppRunVersionProbe(t, "cfloat_range_widen",
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"), strings.ReplaceAll(hostileBody, "@AIM@", hostileAim), "")
	if err != nil {
		t.Fatalf("cfloat_range_widen(hostile): %v\n%s", err, out)
	}

	pastBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen(past): n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1u || back[0].trail != 2u )
        { printf( "cfloat_range_widen(past): a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    {
        uint32_t bits = 0;
        memcpy( &bits, &back[0].aim, sizeof( back[0].aim ) );
        if ( bits != 0x40000000u ) { printf( "cfloat_range_widen(past): aim did not clamp to THIS READER'S OWN declared max 2.0 (0x40000000, not a manifest value): 0x%08X\n", (unsigned) bits ); return 1; }
    }
    if ( r.clamped != 1 ) { printf( "cfloat_range_widen(past): clamped is not exactly 1: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_range_widen(past): a counter moved on a clean backward read: unknown=%d km=%d w=%d dup=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_range_widen(past): a clean read refused: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
`
	out, err = cppRunVersionProbe(t, "cfloat_range_widen",
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), pastBody, "")
	if err != nil {
		t.Fatalf("cfloat_range_widen(past): %v\n%s", err, out)
	}
}
