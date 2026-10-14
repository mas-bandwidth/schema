package ctable

import (
	"path/filepath"
	"testing"
)

// §5.8 row 11, unknown_census: the NEW reader (Item.drop REMOVED) reads the OLD
// writer's lawful `old_unknown_census.bin` through the handed-in lineage entry.
// Four elements each carry `a` and the removed `drop`, but `drop` is ONE field
// of ONE peer (Item), so the compile census counts it ONCE: `unknown == 1`, and
// a leg that counts once per ELEMENT would land `4` and pass a `>= 1` read. The
// check asserts `== 1` exactly, and reads the same peer a SECOND time to prove
// the census lands on every returning read (§5.9 #6), not once per run.
func TestFixedVersioningUnknownCensus(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_unknown_census")
	older := cReadSchema(t, "VOLD_unknown_census")

	body := `
    if ( n != 1 ) { printf( "unknown_census: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "unknown_census: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].items[0].a != 10 || back[0].items[1].a != 11 || back[0].items[2].a != 12 || back[0].items[3].a != 13 )
        { printf( "unknown_census: the four a values did not land: %d %d %d %d\n", back[0].items[0].a, back[0].items[1].a, back[0].items[2].a, back[0].items[3].a ); return 1; }
    if ( r.unknown != 1 ) { printf( "unknown_census: unknown == %d, not exactly 1\n", r.unknown ); return 1; }
    if ( r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "unknown_census: a counter moved on a clean backward read: u=%d km=%d w=%d c=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "unknown_census: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "unknown_census: reason == %d, not 0\n", r.reason ); return 1; }
    {
        int64_t n2;
        memset( &r, 0, sizeof( r ) );
        n2 = census_fixed_load( back, 8, data, len, plan, 4096, NULL, &r );
        if ( n2 != 1 ) { printf( "unknown_census: the SECOND read did not return one record: %lld\n", (long long) n2 ); return 1; }
        if ( r.unknown != 1 ) { printf( "unknown_census: the SECOND read owes unknown == 1 again, not %d\n", r.unknown ); return 1; }
    }
`

	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "old_unknown_census.bin"), body, "")
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
