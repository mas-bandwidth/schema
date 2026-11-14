package ctable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// cfloat_range_widen (docs/FIXED-FORM-VERSIONING-TESTS.md): the reader's aim
// range WIDENS -1..1 -> -2..2, and the bounds pass on a read is the READER's,
// so the old writer's bound has nowhere to live once the record is resolved.
// old_ (0.5) lands 0.5 whole; hostile_ (forged 1.5, past the writer but inside
// the reader) lands 1.5 WHOLE; past_ (forged 5.0) is past the READER too and
// REFUSES out_of_range in the bounds pass, landing nothing
// (docs/FIXED-FORM-ALGORITHM.md §4.6; the strict read, SPEC-TABLES §6.5). The
// hostile_ read alone proves nothing: delete the pass and it stays green, and
// only the past_ read goes red.
//
// `aim` IS COMPARED BY BITS, AND TWO OF THE THREE COME FROM THE CORPUS MANIFEST
// (schema#1164, Glenn 2026-09-19: "do whatever is needed to make sure that a new
// reader can read an old writer"). These were `back[0].aim != 0.5f` and friends —
// float comparisons against HARDCODED literals, an oracle written by the same
// hand as the code under test. `old_`'s value is the manifest's `values=r0.aim`,
// `hostile_`'s is the manifest's own `forged=r0.aim@104` — the value the
// reference wrote into those bytes. `past_` lands no value at all.
//
// The check is spliced at @AIM@ rather than formatted in, so nothing in these C
// bodies has to have its printf verbs escaped.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_cfloat_range_widen")
	older := cReadSchema(t, "VOLD_cfloat_range_widen")

	aimCheck := func(which string, bits uint32, note string) string {
		return fmt.Sprintf(`    { uint32_t aimbits = 0; memcpy( &aimbits, &back[0].aim, 4 );
      if ( aimbits != %#[2]xu ) { printf( "cfloat_range_widen %[1]s: aim is %%#x, not %#[2]x — %[3]s\n", aimbits ); return 1; } }`,
			which, bits, note)
	}
	oldAim := aimCheck("old_", cManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"),
		"the writer's own value, from the corpus manifest")
	hostileAim := aimCheck("hostile_", cManifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim"),
		"the manifest's own forged value, landed WHOLE because the bounds pass is the READER's")

	oldBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen old_: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "cfloat_range_widen old_: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
@AIM@
    if ( r.unknown != 0 || r.widened != 0 )
        { printf( "cfloat_range_widen old_: a counter moved on a clean read: u=%d w=%d\n", r.unknown, r.widened ); return 1; }
    if ( r.refused || r.refuse_reason != 0 ) { printf( "cfloat_range_widen old_: a clean read refused: refused=%d refuse_reason=%d\n", r.refused, r.refuse_reason ); return 1; }
    if ( r.reason != 0 ) { printf( "cfloat_range_widen old_: reason == %d, not 0\n", r.reason ); return 1; }
`

	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "old_cfloat_range_widen.bin"), strings.ReplaceAll(oldBody, "@AIM@", oldAim), "")
	if err != nil {
		t.Fatalf("cfloat_range_widen old_: %v\n%s", err, out)
	}

	hostileBody := `
    if ( n != 1 ) { printf( "cfloat_range_widen hostile_: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "cfloat_range_widen hostile_: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
@AIM@
    if ( r.unknown != 0 || r.widened != 0 )
        { printf( "cfloat_range_widen hostile_: a counter moved on a clean read: u=%d w=%d\n", r.unknown, r.widened ); return 1; }
    if ( r.refused || r.refuse_reason != 0 ) { printf( "cfloat_range_widen hostile_: a clean read refused: refused=%d refuse_reason=%d\n", r.refused, r.refuse_reason ); return 1; }
    if ( r.reason != 0 ) { printf( "cfloat_range_widen hostile_: reason == %d, not 0\n", r.reason ); return 1; }
`

	out, err = cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"), strings.ReplaceAll(hostileBody, "@AIM@", hostileAim), "")
	if err != nil {
		t.Fatalf("cfloat_range_widen hostile_: %v\n%s", err, out)
	}

	pastBody := cRefusedBody("cfloat_range_widen past_", "SCHEMA_TABLE_REFUSE_OUT_OF_RANGE")

	out, err = cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), pastBody, "")
	if err != nil {
		t.Fatalf("cfloat_range_widen past_: %v\n%s", err, out)
	}
}
