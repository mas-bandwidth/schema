package cpptable

// §5.8 row 11, unknown_census: the NEW reader (Item.drop REMOVED) reads the OLD
// writer's lawful `old_unknown_census.bin` through the handed-in lineage entry.
// Four elements each carry `a` and the removed `drop`, but `drop` is ONE field
// of ONE peer (Item), so the compile census counts it ONCE: `unknown == 1`, and
// a leg that counts once per ELEMENT would land `4` and pass a `>= 1` read. The
// check asserts `== 1` exactly.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := `
    if ( n != 1 ) { printf( "unknown_census: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1u || back[0].trail != 2u )
        { printf( "unknown_census: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    {
        const int32_t want[4] = { 10, 11, 12, 13 };
        int k;
        for ( k = 0; k < 4; ++k )
            if ( back[0].items[k].a != want[k] ) { printf( "unknown_census: items[%d].a is not %d: %d\n", k, want[k], back[0].items[k].a ); return 1; }
    }
    if ( r.unknown != 1 ) { printf( "unknown is not exactly 1: %d\n", r.unknown ); return 1; }
    if ( r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 )
        { printf( "unknown_census: a counter moved on a clean backward read: unknown=%d km=%d w=%d c=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped ); return 1; }
    if ( r.malformed || r.refused ) { printf( "unknown_census: a clean read refused: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
`
	out, err := cppRunVersionProbe(t, "unknown_census",
		filepath.Join(corpus, "old_unknown_census.bin"), body, "")
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
