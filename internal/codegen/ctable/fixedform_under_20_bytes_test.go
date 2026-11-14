package ctable

// TestCFixedFormUnder20Bytes is F7 of schema#876, the fixed_form_under_20_bytes
// row: a file shorter than the twenty-byte header is MALFORMED, and no leg
// tested it — a gap on all nine. The two answers are `refused` (plus a `reason`
// name) and `malformed`, and they are NEVER both set; `reason` stays UNTOUCHED
// here because a short file is the residue of a truncation, not a refusal by
// name, so asserting layout_malformed would be wrong — that name is for a KNOWN
// hash whose bytes lie. The destination is proved untouched by memcmp against
// `fresh`, the reader's own default image, memcpy'd back into slot 0 before
// every short read.
//
// WHAT GUARDS bytes == 0: step 1's `if ( values == NULL || data == NULL ||
// bytes < 1 )`, read verbatim from the emitter above the form byte, and it
// gives the SAME five-part answer as bytes 1..19 — so the loop runs k = 0 ..
// kTableFixedHeaderBytes + 4 exclusive.
//
// CONTROL 2 (the line-640 short-file guard disabled) went RED at k=1 with
// `under20: at k=1 malformed=0, not 1` — a clean red, no crash, because the
// probe's `data` holds the WHOLE file, so the read behind the guard lands on the
// real header and the reader answers layout_malformed (refused) where it owes
// malformed. The guard is live; without it the two answers swap.

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
    if ( r.refused || r.reason != 0 || r.refuse_reason != 0 )
        { printf( "under20: the full-length file must read clean: refused=%%d reason=%%d refuse_reason=%%d\n", r.refused, r.reason, r.refuse_reason ); return 1; }
    for ( k = 0; k < kTableFixedHeaderBytes + 4; ++k )
    {
        memset( &r, 0, sizeof( r ) );
        memcpy( &back[0], &fresh, sizeof( fresh ) ); /* the sentinel: the reader's own default image */
        n = %s( back, 8, data, k, plan, 4096, NULL, &r );
        if ( n != -1 ) { printf( "under20: at k=%%d a file shorter than the header is malformed, not n=%%lld\n", k, (long long) n ); return 1; }
        if ( r.refused != 1 || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
            { printf( "under20: at k=%%d refused=%%d refuse_reason=%%d, not wire_damaged\n", k, r.refused, r.refuse_reason ); return 1; }
        if ( r.reason != 0 ) { printf( "under20: at k=%%d a short file is damage, never refused by name: reason=%%d\n", k, r.reason ); return 1; }
        if ( r.widened != 0 || r.unknown != 0 )
            { printf( "under20: at k=%%d damage is total: no counter moves (w=%%d u=%%d)\n", k, r.widened, r.unknown ); return 1; }
        if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 ) { printf( "under20: at k=%%d a damaged read wrote destination bytes\n", k ); return 1; }
    }
`, load)

	out, err := cRunVersionProbe(t, newer, nil, 0, file, body, "")
	if err != nil {
		t.Fatalf("fixed_form_under_20_bytes: %v\n%s", err, out)
	}
}
