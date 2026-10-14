package cpptable

import (
	"path/filepath"
	"testing"
)

// roadmap cpp/C1 and cpp/C2, quoted verbatim from docs/roadmap.sexp, are the
// two ends of the count clamp: "count clamp v<0" and "count clamp v>Max". The
// landed row 4 writer_bound_count forges the count word to 7, which is past the
// PLAN's bound of 4 but neither negative nor past the READER's own max of 8, so
// it exercises neither end. On 2026-09-19 the negative arm of the count clamp
// was deleted from the emitted runtime of five legs and not one of their landed
// fixed-table tests went red, so the negative arm is guarded by nothing; this
// row forges both ends. `clamped` is asserted `== 1` and never `>= 1` because a
// leg that counts the clamp once in the count op and again in a bounds pass
// lands 2. The C2 count lands on 4, the WRITER's bound carried by the plan,
// never the reader's own 8 and never the forged 9.
func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := cppFixedCorpus(t)
	file := filepath.Join(corpus, "old_array_bounded_grow.bin")

	c1Body := `
    if ( n != 1 ) { printf( "n is not 1: %lld\n", (long long) n ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].vals_count != 0 ) { printf( "the negative count does not clamp to zero: %d\n", back[0].vals_count ); return 1; }
    if ( r.clamped != 1 ) { printf( "clamped is not exactly 1: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "counters moved on a clean read: unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "a clean read is not a refusal: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.reason != newer_form ) { printf( "reason is not the clean value: %d\n", (int) r.reason ); return 1; }
`
	c1Forge := `
    /* C1: the OLD record is lead 0xAAAAAAAA then vals_count 4, both
       little-endian uint32, so the eight-byte needle is AA AA AA AA 04 00 00 00.
       Find it, assert it occurs EXACTLY once, and write 0xFFFFFFFF over the
       count word — -1 read as the little-endian int32 the wire carries. */
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator matched twice: %lld and %lld\n", (long long) found, (long long) i ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator matched nowhere\n" ); return 1; }
        TableFixedPut32( data + found + 4, 0xFFFFFFFFu ); /* the forge: -1 as the wire's little-endian int32 */
    }
`

	c2Body := `
    if ( n != 1 ) { printf( "n is not 1: %lld\n", (long long) n ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].vals_count != 4 ) { printf( "vals_count is not the WRITER's 4 (never the reader's 8, never the forged 9): %d\n", back[0].vals_count ); return 1; }
    {
        const int32_t want[4] = { 1000, 1001, 1002, 1003 };
        int k;
        for ( k = 0; k < 4; ++k )
            if ( back[0].vals[k] != want[k] ) { printf( "vals[%d] is not the old writer's %d: %d\n", k, want[k], back[0].vals[k] ); return 1; }
        for ( k = 4; k < 8; ++k )
            if ( back[0].vals[k] != 0 ) { printf( "vals[%d] is not the reader's declared default 0: %d\n", k, back[0].vals[k] ); return 1; }
    }
    if ( r.clamped != 1 ) { printf( "clamped is not exactly 1: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "counters moved on a clean read: unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "a clean read is not a refusal: malformed=%d refused=%d\n", (int) r.malformed, (int) r.refused ); return 1; }
    if ( r.reason != newer_form ) { printf( "reason is not the clean value: %d\n", (int) r.reason ); return 1; }
`
	c2Forge := `
    /* C2: same locator and same EXACTLY-once check, but write 9 over the count
       word — past the writer's 4 AND past the reader's 8, which is what makes
       this C2 and not row 4's forge of 7. */
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 8 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator matched twice: %lld and %lld\n", (long long) found, (long long) i ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator matched nowhere\n" ); return 1; }
        TableFixedPut32( data + found + 4, 9 ); /* the forge: past the reader's own bound */
    }
`

	out, err := cppRunVersionProbe(t, "array_bounded_grow", file, c1Body, c1Forge)
	if err != nil {
		t.Fatalf("count_clamp_ends C1 (negative): %v\n%s", err, out)
	}
	out, err = cppRunVersionProbe(t, "array_bounded_grow", file, c2Body, c2Forge)
	if err != nil {
		t.Fatalf("count_clamp_ends C2 (past the reader's bound): %v\n%s", err, out)
	}
}
