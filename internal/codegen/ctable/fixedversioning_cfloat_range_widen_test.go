package ctable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// cfloat_range_widen (docs/FIXED-FORM-VERSIONING-TESTS.md): the reader's aim
// range WIDENS -1..1 -> -2..2. On a cross-version read a compressed float is
// held to the WRITER's range the lineage plan carries, like every ranged kind
// (docs/rulings.json `strict-only-read`, which overturned schema#1164's reading
// that the pass was the reader's; docs/FIXED-FORM-ALGORITHM.md §4.6). old_ (0.5)
// is the control, a writer range NARROWER than the reader's whose value lands
// whole; hostile_ (forged 1.5, past the writer but inside the reader) REFUSES
// the whole load out_of_range, landing nothing; past_ (forged 5.0) is past both
// ranges and refuses the same way. hostile_ is the read that proves the
// writer's range is held: past_ alone refuses under the reader's range too.
//
// `aim` IS COMPARED BY BITS against the corpus manifest's `values=r0.aim`,
// never a hardcoded literal. The check is spliced at @AIM@ rather than
// formatted in, so nothing in the C body has to have its printf verbs escaped.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_cfloat_range_widen")
	older := cReadSchema(t, "VOLD_cfloat_range_widen")

	oldAim := fmt.Sprintf(`    { uint32_t aimbits = 0; memcpy( &aimbits, &back[0].aim, 4 );
      if ( aimbits != %#[1]xu ) { printf( "cfloat_range_widen old_: aim is %%#x, not %#[1]x — the writer's own value, from the corpus manifest\n", aimbits ); return 1; } }`,
		cManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"))

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

	for _, which := range []string{"hostile_", "past_"} {
		body := cRefusedBody("cfloat_range_widen "+which, "SCHEMA_TABLE_REFUSE_OUT_OF_RANGE")
		out, err = cRunVersionProbe(t, newer, []string{older}, 0,
			filepath.Join(corpus, which+"cfloat_range_widen.bin"), body, "")
		if err != nil {
			t.Fatalf("cfloat_range_widen %s: %v\n%s", which, err, out)
		}
	}
}
