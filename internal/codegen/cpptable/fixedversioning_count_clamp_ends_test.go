package cpptable

import (
	"path/filepath"
	"testing"
)

// roadmap cpp/C1 and cpp/C2, quoted verbatim from docs/roadmap.sexp, were the
// two ends of the count clamp: "count clamp v<0" and "count clamp v>Max". The
// OWNER RULE (2026-10-15) replaced the count clamp with a NAMED REFUSAL: a
// count past the PLAN's bound — negative or over — refuses `count_over_bound`
// before any destination byte is written (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.5). This is the cpp transcription of the
// edited Go rows in internal/codegen/gotable/
// fixedversioning_count_clamp_ends_test.go at d808c3f54: both TestCountClampEndsC1
// (0xFFFFFFFF) and TestCountClampEndsC2 (9) now expect `count_over_bound`, not
// a clamp. `clamped` is asserted zero because REFUSE is total (§5.8: no
// counter moves).
func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := cppFixedCorpus(t)
	file := filepath.Join(corpus, "old_array_bounded_grow.bin")

	c1Forge := `
    /* C1, the negative end: the OLD record is lead 0xAAAAAAAA then vals_count 4,
       both little-endian uint32, so the eight-byte needle is
       AA AA AA AA 04 00 00 00. Find it, assert it occurs EXACTLY once, and
       write 0xFFFFFFFF over the count word — -1 as the wire's int32. */
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
        TableFixedPut32( data + found + 4, 0xFFFFFFFFu );
    }
`
	c2Forge := `
    /* C2, past the reader's bound: same locator and same EXACTLY-once check,
       but write 9 over the count word — past the writer's 4 AND past the
       reader's 8. */
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
        TableFixedPut32( data + found + 4, 9 );
    }
`
	body := `
    if ( n >= 0 || !r.refused || r.reason != count_over_bound )
        { printf( "count_over_bound_ends: n=%lld refused=%d reason=%d, want count_over_bound\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 || r.malformed )
        { printf( "count_over_bound_ends: the refusal moved a counter or marked malformed\n" ); return 1; }
    printf( "count_over_bound_ends refuses by name\n" );
`
	out, err := cppRunVersionProbe(t, "array_bounded_grow", file, body, c1Forge)
	if err != nil {
		t.Fatalf("count_over_bound_ends C1 (negative): %v\n%s", err, out)
	}
	out, err = cppRunVersionProbe(t, "array_bounded_grow", file, body, c2Forge)
	if err != nil {
		t.Fatalf("count_over_bound_ends C2 (past the reader's bound): %v\n%s", err, out)
	}
}
