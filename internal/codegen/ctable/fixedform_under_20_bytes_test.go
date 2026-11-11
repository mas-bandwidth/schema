package ctable

// TestCFixedFormUnder20Bytes is F7 of schema#876, the fixed_form_under_20_bytes
// row: a file shorter than the twenty-byte header is refused, and no leg tested
// it — a gap on all nine. The answer is a refusal (plus a reason name): no bytes
// at all is step 1's guard, and the rest of the short header is step 2's, which
// names the framing damage `truncated` beside wire_damaged
// (docs/FIXED-FORM-ALGORITHM.md §5.3, the Go oracle's TableFixedReasonTruncated).
// `reason` is NOT the message name `layout_malformed`: that name is for a KNOWN
// hash whose bytes lie, and a file under twenty bytes has not got far enough to
// carry one. The destination is proved untouched by memcmp against `fresh`, the
// reader's own default image, memcpy'd back into slot 0 before every short read.
//
// WHAT GUARDS bytes == 0: step 1's `if ( values == NULL || data == NULL ||
// bytes < 1 )`, read verbatim from the emitter above the form byte, and it
// answers wire_damaged with no name, so the loop runs k = 0 ..
// kTableFixedHeaderBytes + 4 exclusive and k >= 1 owes step 2's name.
//
// A CONTROL that disables the short-file guard moves the read onto the real
// header (the probe's `data` holds the WHOLE file), where the reader answers
// layout_malformed instead of the short-header refusal, so the k >= 1 reason
// check goes RED. The guard is live; without it the two answers swap.

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCFixedFormUnder20Bytes(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_field_append")
	file := filepath.Join(corpus, "new_field_append.bin")
	load := ir.RustSnake(cFixedRootName(t, cUnitOf(t, newer))) + "_fixed_load"

	body := fmt.Sprintf(`
    if ( n < 1 ) { printf( "under20: the full-length file did not read: n=%%lld reason=%%d\n", (long long) n, r.reason ); return 1; }
    if ( r.refused || r.refused || r.reason != 0 )
        { printf( "under20: the full-length file must read clean: refused=%%d reason=%%d\n", r.refused, r.reason ); return 1; }
    for ( k = 0; k < kTableFixedHeaderBytes + 4; ++k )
    {
        const int want_reason = ( k == 0 ) ? 0 : SCHEMA_TABLE_REFUSE_TRUNCATED;
        memset( &r, 0, sizeof( r ) );
        memcpy( &back[0], &fresh, sizeof( fresh ) ); /* the sentinel: the reader's own default image */
        n = %s( back, 8, data, k, plan, 4096, NULL, &r );
        if ( n != -1 ) { printf( "under20: at k=%%d a file shorter than the header is refused, not n=%%lld\n", k, (long long) n ); return 1; }
        if ( r.refused != 1 ) { printf( "under20: at k=%%d refused=%%d, not 1\n", k, r.refused ); return 1; }
        if ( r.reason != want_reason ) { printf( "under20: at k=%%d reason=%%d, want %%d\n", k, r.reason, want_reason ); return 1; }
        if ( r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED ) { printf( "under20: at k=%%d refuse_reason=%%d, not wire_damaged\n", k, r.refuse_reason ); return 1; }
        if ( r.widened != 0 || r.unknown != 0 || r.layout_hash != 0 )
            { printf( "under20: at k=%%d a short-header refusal is total: no counter moves (w=%%d u=%%d h=%%llx)\n", k, r.widened, r.unknown, (unsigned long long) r.layout_hash ); return 1; }
        if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 ) { printf( "under20: at k=%%d a refused read wrote destination bytes\n", k ); return 1; }
    }
`, load)

	out, err := cRunVersionProbe(t, newer, nil, 0, file, body, "")
	if err != nil {
		t.Fatalf("fixed_form_under_20_bytes: %v\n%s", err, out)
	}
}
