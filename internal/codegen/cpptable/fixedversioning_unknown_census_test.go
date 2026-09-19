package cpptable

import (
	"path/filepath"
	"testing"
)

// §5.8 row 11, `unknown_census`: the NEW build reads `old_unknown_census.bin`
// through the lineage the sibling convention hands in (the pair is unlawful —
// §5.1 refuses a removal — so no lock). The bytes are lawful: one record, four
// `items`, `a` = 10..13 and `drop` = 900..903; the divergence is the COUNTING,
// not damaged data. `r.unknown` is asserted EXACTLY `== 1` because `Item.drop`
// is one field of one peer, and a leg that counts once per element lands `4`
// and would pass `>= 1`; that `4` is the whole reason this row exists.
func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := `
    if ( n != 1 ) { printf( "n is not 1: %lld\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    {
        const int32_t want[4] = { 10, 11, 12, 13 };
        int k;
        for ( k = 0; k < 4; ++k )
            if ( back[0].items[k].a != want[k] ) { printf( "items[%d].a is not the old writer's %d: %d\n", k, want[k], back[0].items[k].a ); return 1; }
    }
    if ( r.unknown != 1 ) { printf( "unknown is not exactly 1: %d\n", r.unknown ); return 1; }
    if ( r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "counters moved on a clean read: kind_mismatch=%d widened=%d clamped=%d duplicate=%d\n", r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "a clean read is not a refusal: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
    {
        TableReport r2;
        memset( &r2, 0, sizeof( r2 ) );
        int64_t n2 = CensusFixedLoad( back, 8, data, len, plan, 4096, NULL, &r2 );
        if ( n2 != 1 ) { printf( "second read n is not 1: %lld\n", (long long) n2 ); return 1; }
        if ( r2.unknown != 1 ) { printf( "second read unknown is not exactly 1: %d\n", r2.unknown ); return 1; }
        if ( r2.malformed || r2.refused ) { printf( "second read refused: malformed=%d refused=%d\n", (int) r2.malformed, (int) r2.refused ); return 1; }
    }
`
	out, err := cppRunVersionProbe(t, "unknown_census",
		filepath.Join(corpus, "old_unknown_census.bin"), body, "")
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
