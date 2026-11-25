package cpptable

// TestFixedFormRaggedTail is F8 of schema#876, the fixed_form_ragged_tail row:
// a record region that is not a whole number of records REFUSES, recommended
// name `truncated` (docs/FIXED-FORM-ALGORITHM.md §5.3's refusal table: "a ragged
// tail: rest mod record_bytes != 0 | truncated | the name"; "There is no
// `malformed` flag on this form's report"). `refused` is set, `reason` is
// truncated, `damaged` stays false. It is F7's direct sibling, the very next
// guard in the same emitted function. record_bytes is the selected lineage entry's record size,
// compiled into the reader as %sFixedRecordBytes, so the guard's
// record_bytes <= 8 arm is NOT reachable from a file at all. rest is the
// file's bytes after the header and layout, from the file's own length, so
// this row forges ONLY the second arm, rest %% record_bytes != 0, by appending
// 1 .. record_bytes-1 bytes. extra == record_bytes is one more whole record,
// not a ragged tail, and is asserted separately. The destination is filled
// with the reader's own default image (`fresh`) and proved still that image.
// PLANT: the emitter's ragged guard `report->refused = true` put back to
// `report->damaged = true` turns this row RED (refused=0, malformed=1).

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
    if ( r.damaged || r.refused )
        { printf( "ragged: the full-length file must read clean: malformed=%%d refused=%%d\n", (int) r.damaged, (int) r.refused ); return 1; }
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
            if ( n != -1 ) { printf( "ragged: at extra=%%lld a ragged tail refuses, not n=%%lld\n", (long long) extra, (long long) n ); return 1; }
            if ( !r.refused ) { printf( "ragged: at extra=%%lld a ragged tail refuses truncated: refused=%%d\n", (long long) extra, (int) r.refused ); return 1; }
            if ( r.damaged ) { printf( "ragged: at extra=%%lld the fixed form's report has no malformed flag (§5.3): malformed=%%d\n", (long long) extra, (int) r.damaged ); return 1; }
            if ( r.reason != TableFixedReason::truncated ) { printf( "ragged: at extra=%%lld reason is %%d, not truncated\n", (long long) extra, (int) r.reason ); return 1; }
            if ( r.widened != 0 || r.unknown != 0 )
                { printf( "ragged: at extra=%%lld REFUSE is total: no counter moves (w=%%d u=%%d km=%%d c=%%d d=%%d)\n", (long long) extra, r.widened, r.unknown, r.refused, r.refused, r.refused ); return 1; }
            for ( int64_t s = 0; s < 8; ++s )
            {
                if ( memcmp( &back[s], &fresh, sizeof( fresh ) ) != 0 ) { printf( "ragged: at extra=%%lld slot %%lld: a refused read wrote destination bytes\n", (long long) extra, (long long) s ); return 1; }
            }
        }
        memset( &r, 0, sizeof( r ) );
        for ( int64_t s = 0; s < 8; ++s ) { memcpy( &back[s], &fresh, sizeof( fresh ) ); }
        n = %sFixedLoad( back, 8, data, len + record_bytes, plan, 4096, NULL, &r );
        if ( r.damaged ) { printf( "ragged: extra==record_bytes is one more whole record, no malformed flag: malformed=%%d\n", (int) r.damaged ); return 1; }
        if ( !r.refused || r.reason != TableMessageReason::no_layout || n != -1 )
            { printf( "ragged: extra==record_bytes walks off the tail: n=%%lld refused=%%d reason=%%d\n", (long long) n, (int) r.refused, (int) r.reason ); return 1; }
    }
`, root, root, root, root, root)

	out, err := cppRunVersionProbe(t, row, file, body, "")
	if err != nil {
		t.Fatalf("fixed_form_ragged_tail: %v\n%s", err, out)
	}
}
