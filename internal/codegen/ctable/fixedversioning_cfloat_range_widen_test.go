package ctable

import (
	"path/filepath"
	"testing"
)

// cfloat_range_widen (docs/FIXED-FORM-VERSIONING-TESTS.md): the reader's aim
// range WIDENS -1..1 -> -2..2, and the bounds pass on a read is the READER's,
// so the old writer's bound has nowhere to live once the record is resolved.
// old_ (0.5) lands 0.5 whole; hostile_ (forged 1.5, past the writer but inside
// the reader) lands 1.5 WHOLE; past_ (forged 5.0) is past the READER too and
// clamps to the reader's own 2.0. clamped is EXACT — 0, 0, 1 across the three
// — because a leg that counts twice or counts the wrong read would move it. The
// hostile_ read alone proves nothing: delete the pass and it stays green, and
// only the past_ read goes red.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_cfloat_range_widen")
	older := cReadSchema(t, "VOLD_cfloat_range_widen")

	oldBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen old_: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "cfloat_range_widen old_: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].aim != 0.5f ) { printf( "cfloat_range_widen old_: the writer's 0.5 did not land exactly: %f\n", (double) back[0].aim ); return 1; }
    if ( r.clamped != 0 ) { printf( "cfloat_range_widen old_: clamped == %d, not 0\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_range_widen old_: a counter moved on a clean read: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_range_widen old_: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "cfloat_range_widen old_: reason == %d, not 0\n", r.reason ); return 1; }
`

	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "old_cfloat_range_widen.bin"), oldBody, "")
	if err != nil {
		t.Fatalf("cfloat_range_widen old_: %v\n%s", err, out)
	}

	hostileBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen hostile_: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "cfloat_range_widen hostile_: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].aim != 1.5f ) { printf( "cfloat_range_widen hostile_: the forged 1.5 did not land WHOLE: %f\n", (double) back[0].aim ); return 1; }
    if ( r.clamped != 0 ) { printf( "cfloat_range_widen hostile_: clamped == %d, not 0\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_range_widen hostile_: a counter moved on a clean read: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_range_widen hostile_: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "cfloat_range_widen hostile_: reason == %d, not 0\n", r.reason ); return 1; }
`

	out, err = cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"), hostileBody, "")
	if err != nil {
		t.Fatalf("cfloat_range_widen hostile_: %v\n%s", err, out)
	}

	pastBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen past_: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "cfloat_range_widen past_: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].aim != 2.0f ) { printf( "cfloat_range_widen past_: the forged 5.0 did not clamp to the reader's max 2.0: %f\n", (double) back[0].aim ); return 1; }
    if ( r.clamped != 1 ) { printf( "cfloat_range_widen past_: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "cfloat_range_widen past_: a counter moved on a clean read: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "cfloat_range_widen past_: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "cfloat_range_widen past_: reason == %d, not 0\n", r.reason ); return 1; }
`

	out, err = cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), pastBody, "")
	if err != nil {
		t.Fatalf("cfloat_range_widen past_: %v\n%s", err, out)
	}
}
