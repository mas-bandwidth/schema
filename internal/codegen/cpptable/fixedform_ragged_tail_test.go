package cpptable

// TestFixedFormRaggedTail is F8 of schema#876, the fixed_form_ragged_tail row:
// a record region that is not a whole number of records is MALFORMED, and no
// leg tested it — a gap on go, cpp and cs, only partial on c and elixir. It is
// F7's direct sibling, the very next guard in the same emitted function, and
// go closed this row as #1291. The reader has exactly two answers — refused
// plus a reason name, or malformed — and they are NEVER both set; a ragged
// tail is the RESIDUE of a bad file, so it sets malformed and leaves reason
// UNTOUCHED (0). record_bytes is the selected lineage entry's record size,
// compiled into the reader as %sFixedRecordBytes, so the guard's
// record_bytes <= 8 arm is NOT reachable from a file at all. rest is the
// file's bytes after the header and layout, from the file's own length, so
// this row forges ONLY the second arm, rest %% record_bytes != 0, by appending
// 1 .. record_bytes-1 bytes. extra == record_bytes is one more whole record,
// not a ragged tail, and is asserted separately. The destination is filled
// with the reader's own default image (`fresh`) and proved still that image.
// CONTROL 2 turns only the ragged arm into false and the row goes RED.
//
// WHAT ALREADY COVERS THIS ON CPP: nothing. The versioning rows assert
// `malformed` and `refused` on clean/refused reads, but no cpp test appends a
// ragged tail, so the second arm rest %% record_bytes != 0 is untested here.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedFormRaggedTail(t *testing.T) {
	corpus := cppFixedCorpus(t)
	row := "field_append"
	file := filepath.Join(corpus, "new_field_append.bin")

	u := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VNEW_"+row+".schema"))
	root := cppFixedRootName(t, u)

	body := fmt.Sprintf(`
    if ( n < 1 ) { printf( "ragged: the full-length file did not read: n=%%lld reason=%%d\n", (long long) n, (int) r.reason ); return 1; }
    if ( r.malformed || r.refused )
        { printf( "ragged: the full-length file must read clean: malformed=%%d refused=%%d\n", (int) r.malformed, (int) r.refused ); return 1; }
    {
        const int64_t record_bytes = %sFixedRecordBytes;
        %s fresh;
        memset( &fresh, 0, sizeof( fresh ) );
        %sReset( fresh );
        for ( int64_t extra = 1; extra < record_bytes; ++extra )
        {
            memset( &r, 0, sizeof( r ) );
            for ( int64_t s = 0; s < 8; ++s ) { memcpy( &back[s], &fresh, sizeof( fresh ) ); }
            n = %sFixedLoad( back, 8, data, len + extra, plan, 4096, NULL, &r );
            if ( n != -1 ) { printf( "ragged: at extra=%%lld a ragged tail is malformed, not n=%%lld\n", (long long) extra, (long long) n ); return 1; }
            if ( r.malformed != 1 ) { printf( "ragged: at extra=%%lld malformed=%%d, not 1\n", (long long) extra, (int) r.malformed ); return 1; }
            if ( r.refused ) { printf( "ragged: at extra=%%lld a ragged tail is residue, never refused by name: refused=%%d\n", (long long) extra, (int) r.refused ); return 1; }
            if ( r.reason != 0 ) { printf( "ragged: at extra=%%lld reason stays untouched, not %%d\n", (long long) extra, (int) r.reason ); return 1; }
            if ( r.widened != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 )
                { printf( "ragged: at extra=%%lld MALFORMED is total: no counter moves (w=%%d u=%%d km=%%d c=%%d d=%%d)\n", (long long) extra, r.widened, r.unknown, r.kind_mismatch, r.clamped, r.duplicate ); return 1; }
            for ( int64_t s = 0; s < 8; ++s )
            {
                if ( memcmp( &back[s], &fresh, sizeof( fresh ) ) != 0 ) { printf( "ragged: at extra=%%lld slot %%lld: a malformed read wrote destination bytes\n", (long long) extra, (long long) s ); return 1; }
            }
        }
        memset( &r, 0, sizeof( r ) );
        for ( int64_t s = 0; s < 8; ++s ) { memcpy( &back[s], &fresh, sizeof( fresh ) ); }
        n = %sFixedLoad( back, 8, data, len + record_bytes, plan, 4096, NULL, &r );
        if ( r.malformed ) { printf( "ragged: extra==record_bytes is one more whole record, not malformed: malformed=%%d\n", (int) r.malformed ); return 1; }
        if ( !r.refused || r.reason != no_layout || n != -1 )
            { printf( "ragged: extra==record_bytes walks off the tail: n=%%lld refused=%%d reason=%%d\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    }
`, root, root, root, root, root)

	out, err := cppRunVersionProbe(t, row, file, body, "")
	if err != nil {
		t.Fatalf("fixed_form_ragged_tail: %v\n%s", err, out)
	}
}
