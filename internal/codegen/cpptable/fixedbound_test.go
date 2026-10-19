package cpptable

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCPPFixedCountOverBoundRefusesByName is the cpp leg of the OWNER RULE
// (2026-10-15): a fixed-form count or length past the PLAN's bound refuses BY
// NAME, `count_over_bound` / `length_over_bound`, before any destination byte
// is written — it never clamps (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.5). The Go oracle is
// internal/codegen/gotable/fixedbound_test.go at d808c3f54
// (TestFixedFormCountAndLengthOverBoundRefuseByName), whose rows this test
// transcribes; the edited Go rows are
// internal/codegen/gotable/fixedversioning_count_clamp_ends_test.go.
func TestCPPFixedCountOverBoundRefusesByName(t *testing.T) {
	t.Parallel()

	// IDENTITY plan: the build's own hash takes the baked identity plan. This
	// transcribes Go's `identity count/length` rows.
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "bound.schema")
	src := `package probe
fixed table Root {
    n   [..95]int32
    txt string(41)
}
`
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `
    Root one;
    RootReset( one );
    one.n_count = 1;
    one.n[0] = 0x11223344;
    one.txt_length = 1;
    one.txt[0] = '!';
    const int64_t need = RootFixedMeasure( 1 );
    n = RootFixedSave( &one, 1, data, (int64_t) sizeof( data ) );
    if ( n != need ) { printf( "identity: save %lld, want %lld\n", (long long) n, (long long) need ); return 1; }
    const uint8_t count_needle[4] = { 0x44, 0x33, 0x22, 0x11 };
    const uint8_t len_needle[5] = { 1, 0, 0, 0, '!' };
    const int64_t cAt = find_bytes( data, n, count_needle, 4 );
    const int64_t lAt = find_bytes( data, n, len_needle, 5 );
    if ( cAt < 4 || lAt < 0 ) { printf( "identity: field not found c=%lld l=%lld\n", (long long) cAt, (long long) lAt ); return 1; }
    const int64_t countOff = cAt - 4;
    const int64_t lenOff = lAt;

    /* assertRefuse: the Go row's reason, zero counters and untouched
       destination (docs/FIXED-FORM-ALGORITHM.md §5.8: not one destination
       byte). */
    #define ASSERT_REFUSE( name, want ) \
    { \
        memset( back, 0x5A, sizeof( Root ) * 4 ); \
        memset( &r, 0, sizeof( r ) ); \
        const int64_t got = RootFixedLoad( back, 4, data, n, plan, 4096, NULL, &r ); \
        if ( got >= 0 || r.reason != (want) ) \
            { printf( "%s: n=%lld reason=%d, want %d\n", (name), (long long) got, (int) r.reason, (int) (want) ); return 1; } \
        if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 || r.malformed ) \
            { printf( "%s: refusal moved a counter or malformed: clamped=%d widened=%d malformed=%d\n", (name), r.clamped, r.widened, (int) r.malformed ); return 1; } \
        const uint8_t * raw = (const uint8_t *) back; \
        for ( size_t i = 0; i < sizeof( Root ) * 4; ++i ) \
            if ( raw[i] != 0x5A ) { printf( "%s: wrote destination byte %zu: 0x%02X\n", (name), i, raw[i] ); return 1; } \
    }

    TableFixedPut32( data + countOff, 200 );
    ASSERT_REFUSE( "identity count over bound", count_over_bound );
    TableFixedPut32( data + countOff, 0xFFFFFFFFu );
    ASSERT_REFUSE( "identity count negative", count_over_bound );
    TableFixedPut32( data + countOff, 1 );
    TableFixedPut32( data + lenOff, 99 );
    ASSERT_REFUSE( "identity length over bound", length_over_bound );
    TableFixedPut32( data + lenOff, 0xFFFFFFFFu );
    ASSERT_REFUSE( "identity length negative", length_over_bound );
    TableFixedPut32( data + lenOff, 1 );

    /* WITH SLACK BEHIND THE BOUND: garbage past the bound changes nothing. */
    for ( int64_t i = countOff + 4; i < n && i < countOff + 4 + 95 * 4; ++i ) { data[i] = 0xFF; }
    TableFixedPut32( data + countOff, 200 );
    ASSERT_REFUSE( "identity count over bound with slack", count_over_bound );
    TableFixedPut32( data + countOff, 1 );
    for ( int64_t i = countOff + 4; i < n && i < countOff + 4 + 95 * 4; ++i ) { data[i] = 0; }
    for ( int64_t i = lenOff + 4; i < n && i < lenOff + 4 + 41; ++i ) { data[i] = 0xFF; }
    TableFixedPut32( data + lenOff, 99 );
    ASSERT_REFUSE( "identity length over bound with slack", length_over_bound );
    printf( "identity count/length over bound refuse by name\n" );
`
	out, err := cppRunFixedProbe(t, schemaPath, "Root", body)
	if err != nil {
		t.Fatalf("identity: %v\n%s", err, out)
	}

	// COMPILED LINEAGE, count and length: the Go oracle's
	// TestFixedFormCountAndLengthOverBoundRefuseByName compiled rows, read
	// through cppRunVersionProbe's VOLD_/VNEW_ lineage.
	corpus := cppFixedCorpus(t)
	countForge := `
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
            if ( memcmp( data + i, needle, 8 ) == 0 ) { if ( found >= 0 ) { printf( "lineage count: non-unique needle\n" ); return 1; } found = i; }
        if ( found < 0 ) { printf( "lineage count: field not found\n" ); return 1; }
        TableFixedPut32( data + found + 4, 200 );
    }
`
	countBody := `
    if ( n >= 0 || !r.refused || r.reason != count_over_bound )
        { printf( "lineage count: n=%lld refused=%d reason=%d, want count_over_bound\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 || r.malformed )
        { printf( "lineage count: refusal moved a counter or malformed\n" ); return 1; }
    printf( "lineage count over bound refuses by name\n" );
`
	if out, err := cppRunVersionProbe(t, "array_bounded_grow", filepath.Join(corpus, "old_array_bounded_grow.bin"), countBody, countForge); err != nil {
		t.Fatalf("lineage count: %v\n%s", err, out)
	}

	lenForge := `
    {
        static const uint8_t needle[8] = { 0xAA, 0xAA, 0xAA, 0xAA, 0x08, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 8 <= len; ++i )
            if ( memcmp( data + i, needle, 8 ) == 0 ) { if ( found >= 0 ) { printf( "lineage length: non-unique needle\n" ); return 1; } found = i; }
        if ( found < 0 ) { printf( "lineage length: field not found\n" ); return 1; }
        TableFixedPut32( data + found + 4, 99 );
    }
`
	lenBody := `
    if ( n >= 0 || !r.refused || r.reason != length_over_bound )
        { printf( "lineage length: n=%lld refused=%d reason=%d, want length_over_bound\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 || r.malformed )
        { printf( "lineage length: refusal moved a counter or malformed\n" ); return 1; }
    printf( "lineage length over bound refuses by name\n" );
`
	if out, err := cppRunVersionProbe(t, "string_grow", filepath.Join(corpus, "old_string_grow.bin"), lenBody, lenForge); err != nil {
		t.Fatalf("lineage length: %v\n%s", err, out)
	}
}
