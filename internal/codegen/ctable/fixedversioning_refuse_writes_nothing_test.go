package ctable

// TestFixedVersioningRefuseWritesNothing is §5.8 row 9 of
// docs/FIXED-FORM-VERSIONING-TESTS.md. The NEW reader is handed the OLD
// lineage entry and reads old_nested_append.bin with record 0's per-record
// hash inverted. The header's hash is left alone so it still selects the OLD
// entry, whose COMPILED plan carries a NONEMPTY fill list (Vec.w = 88) when
// step 11 compares the record's own hash. Only the record hash is forged — a
// header-hash forge is a different row — and the refusal must be TOTAL:
// malformed is FALSE because a refusal by name is not a malformed file, and
// the caller's storage, poisoned 0x5A before the load, stays 0x5A in every
// byte — not the prefill's 88, not a zero — which is what "writes nothing"
// means.

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
    if ( r.malformed ) { printf( "refuse_writes_nothing: malformed set — a refusal by name is not a malformed file\n" ); return 1; }
    if ( r.clamped != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "refuse_writes_nothing: a counter moved: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    {
        const uint8_t * p = (const uint8_t *) back;
        int64_t i;
        for ( i = 0; i < (int64_t) sizeof( back ); ++i )
        {
            if ( p[i] != 0x5A ) { printf( "refuse_writes_nothing: byte %lld is 0x%02x — REFUSE wrote a destination byte\n", (long long) i, p[i] ); return 1; }
        }
    }
`

	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "old_nested_append.bin"), body, poison, pre)
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
	}
}
