package ctable

import (
	"path/filepath"
	"testing"
)

// §5.4's compile census lands once per peer AND never per record, but the
// landed one-record row reads 'old_unknown_census.bin', ONE record, where
// "once per peer" and "once per record" are the SAME number, 1. A per-record
// Unknown++ really exists in these runtimes (the union-tag path), and only a
// many-record file tells it apart. 'many_unknown_census.bin' carries THREE
// Census records; the counter is asserted == 1 EXACTLY, never >= 1, because a
// leg that counts once per record would land 3 and must not pass.
func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_unknown_census")
	older := cReadSchema(t, "VOLD_unknown_census")

	body := `
    if ( n != 3 ) { printf( "unknown_census_many: n == %lld, not 3\n", (long long) n ); return 1; }
    if ( back[0].lead != 1 || back[0].trail != 2 )
        { printf( "unknown_census_many: record 0's bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[1].lead != 1 || back[1].trail != 2 )
        { printf( "unknown_census_many: record 1's bracket moved: lead=%u trail=%u\n", back[1].lead, back[1].trail ); return 1; }
    if ( back[2].lead != 1 || back[2].trail != 2 )
        { printf( "unknown_census_many: record 2's bracket moved: lead=%u trail=%u\n", back[2].lead, back[2].trail ); return 1; }
    if ( back[0].items[0].a != 10 || back[0].items[1].a != 11 || back[0].items[2].a != 12 || back[0].items[3].a != 13 )
        { printf( "unknown_census_many: record 0's four a values did not land: %d %d %d %d\n", back[0].items[0].a, back[0].items[1].a, back[0].items[2].a, back[0].items[3].a ); return 1; }
    if ( back[1].items[0].a != 20 || back[1].items[1].a != 21 || back[1].items[2].a != 22 || back[1].items[3].a != 23 )
        { printf( "unknown_census_many: record 1's four a values did not land: %d %d %d %d\n", back[1].items[0].a, back[1].items[1].a, back[1].items[2].a, back[1].items[3].a ); return 1; }
    if ( back[2].items[0].a != 30 || back[2].items[1].a != 31 || back[2].items[2].a != 32 || back[2].items[3].a != 33 )
        { printf( "unknown_census_many: record 2's four a values did not land: %d %d %d %d\n", back[2].items[0].a, back[2].items[1].a, back[2].items[2].a, back[2].items[3].a ); return 1; }
    if ( r.unknown != 1 ) { printf( "unknown_census_many: unknown == %d, not exactly 1\n", r.unknown ); return 1; }
    if ( r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "unknown_census_many: a counter moved on a clean backward read: u=%d km=%d w=%d c=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "unknown_census_many: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "unknown_census_many: reason == %d, not 0\n", r.reason ); return 1; }
    {
        int64_t n2;
        memset( &r, 0, sizeof( r ) );
        n2 = census_fixed_load( back, 8, data, len, plan, 4096, NULL, &r );
        if ( n2 != 3 ) { printf( "unknown_census_many: the SECOND read did not return three records: %lld\n", (long long) n2 ); return 1; }
        if ( r.unknown != 1 ) { printf( "unknown_census_many: the SECOND read owes unknown == 1 again, not %d\n", r.unknown ); return 1; }
    }
`

	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "many_unknown_census.bin"), body, "")
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
