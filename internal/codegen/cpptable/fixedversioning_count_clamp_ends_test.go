package cpptable

import (
	"path/filepath"
	"testing"
)

// roadmap cpp/C1 and cpp/C2, quoted verbatim from docs/roadmap.sexp, are the
// two ends of the count clamp: "count clamp v<0" and "count clamp v>Max". The
// OWNER RULE (2026-10-15) replaced the count clamp with a NAMED REFUSAL: a
// count past the PLAN's bound — negative or over — refuses `count_over_bound`
// before any destination byte is written (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.5). The landed row 4 writer_bound_count forges
// the count word to 7, which is past the PLAN's bound of 4 but neither negative
// nor past the READER's own max of 8, so it exercises neither end. This row
// forges both ends and each asserts the refusal — the Go oracle's
// count_over_bound_ends rows C1/C2 (internal/codegen/gotable/
// fixedversioning_count_clamp_ends_test.go). Nothing is decoded and no counter
// moves.
func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := cppFixedCorpus(t)
	file := filepath.Join(corpus, "old_array_bounded_grow.bin")

	c1Body := `
    if ( n != -1 ) { printf( "n is not -1: %lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "refused is false\n" ); return 1; }
    if ( r.reason != count_over_bound ) { printf( "reason=%d, not count_over_bound\n", (int) r.reason ); return 1; }
    if ( r.malformed ) { printf( "malformed set — a refusal by name is not malformed\n" ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "a counter moved on a refusal: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
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
    if ( n != -1 ) { printf( "n is not -1: %lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "refused is false\n" ); return 1; }
    if ( r.reason != count_over_bound ) { printf( "reason=%d, not count_over_bound\n", (int) r.reason ); return 1; }
    if ( r.malformed ) { printf( "malformed set — a refusal by name is not malformed\n" ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "a counter moved on a refusal: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
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
