package cpptable

// §5.4 closes the second half of its own sentence: the compile census lands
// ONCE PER PEER AND NEVER PER RECORD. The landed one-record row reads
// old_unknown_census.bin, which holds ONE record, so "once per peer" and
// "once per record" are the same number there and the clause never got a gate.
// A per-record Unknown++ really exists (the union-tag path), so a three-record
// file is the only read that tells the two apart. many_unknown_census.bin
// carries THREE records of the same Census root the landed row uses; the one
// dropped field, Item.drop, is ONE field of ONE peer, so the counter is '1'
// EXACTLY — never 3, never '>='.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := `
    if ( n != 3 ) { printf( "unknown_census_many: n == %lld, not 3\n", (long long) n ); return 1; }
    {
        const int32_t want[3][4] = { { 10, 11, 12, 13 }, { 20, 21, 22, 23 }, { 30, 31, 32, 33 } };
        int rec, k;
        for ( rec = 0; rec < 3; ++rec )
        {
            if ( back[rec].lead != 1u || back[rec].trail != 2u )
                { printf( "unknown_census_many: record %d bracket moved: lead=%u trail=%u\n", rec, back[rec].lead, back[rec].trail ); return 1; }
            for ( k = 0; k < 4; ++k )
                if ( back[rec].items[k].a != want[rec][k] ) { printf( "unknown_census_many: record %d items[%d].a is not %d: %d\n", rec, k, want[rec][k], back[rec].items[k].a ); return 1; }
        }
    }
    if ( r.unknown != 1 ) { printf( "unknown is not exactly 1: %d\n", r.unknown ); return 1; }
    if ( r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "unknown_census_many: a counter moved on a clean backward read: unknown=%d km=%d w=%d c=%d dup=%d\n", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "unknown_census_many: a clean read refused: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
`
	out, err := cppRunVersionProbe(t, "unknown_census",
		filepath.Join(corpus, "many_unknown_census.bin"), body, "")
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
