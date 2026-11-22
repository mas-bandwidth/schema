package ctable

// TestFixedVersioningRefuseWritesNothing is §5.8 row 9 of
// docs/FIXED-FORM-VERSIONING-TESTS.md. The NEW reader is handed the OLD
// lineage entry and reads old_nested_append.bin with record 0's per-record
// hash inverted. The header's hash is left alone so it still selects the OLD
// entry, whose COMPILED plan carries a NONEMPTY fill list (Vec.w = 88) when
// step 11 compares the record's own hash. Only the record hash is forged — a
// header-hash forge is a different row — and the refusal must be TOTAL:
// wire_damaged is NOT set because a refusal by name is not a damaged file.
// RULING R4 replaces "the caller's storage, poisoned 0x5A, stays 0x5A": every
// row handed in, all 8, holds its declared defaults after the refusal,
// compared with the same poisoned bytes reset.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_nested_append")
	older := cReadSchema(t, "VOLD_nested_append")

	poison := `    memset( back, 0x5A, sizeof( back ) );`

	pre := `
    {
        uint32_t lb = table_fixed_get32( data + kTableFixedHeaderBytes );
        int64_t off = kTableFixedHeaderBytes + 4 + (int64_t) lb;
        int i;
        if ( len < off + 8 ) { printf( "refuse_writes_nothing: record 0 is out of the file: len=%lld off=%lld\n", (long long) len, (long long) off ); return 1; }
        for ( i = 0; i < 8; ++i )
        {
            data[off + i] = (uint8_t)~data[off + i]; /* the forge */
        }
    }
`

	body := `
    if ( n != -1 ) { printf( "refuse_writes_nothing: the forged record hash did not refuse: n=%lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "refuse_writes_nothing: refused is false\n" ); return 1; }
    if ( r.reason != SCHEMA_TABLE_NO_LAYOUT ) { printf( "refuse_writes_nothing: reason=%d, not no_layout\n", r.reason ); return 1; }
    if ( r.refuse_reason != 0 ) { printf( "refuse_writes_nothing: refuse_reason=%d beside the name — a refusal by name is not a damaged file\n", r.refuse_reason ); return 1; }
    if ( r.unknown != 0 || r.widened != 0 )
        { printf( "refuse_writes_nothing: a counter moved: unknown=%d widened=%d\n", r.unknown, r.widened ); return 1; }
    {
        /* RULING R4: every row handed in, all 8, holds its declared defaults
           after the refusal: the image of the same poisoned bytes, reset. */
        static probe_row_t expect[8];
        memset( expect, 0x5A, sizeof( expect ) );
        for ( k = 0; k < 8; ++k ) { PROBE_RESET( &expect[k] ); }
        for ( k = 0; k < 8; ++k )
        {
            if ( memcmp( &back[k], &expect[k], sizeof( back[k] ) ) != 0 ) { printf( "refuse_writes_nothing: row %d is not its declared defaults after the refusal\n", k ); return 1; }
        }
    }
`

	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "old_nested_append.bin"), body, poison, pre)
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
	}
}
