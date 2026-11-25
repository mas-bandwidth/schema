package cpptable

// TestFixedFormUnder20Bytes is F7 of schema#876, the fixed_form_under_20_bytes
// row: a file shorter than the twenty-byte fixed header REFUSES, recommended
// name `truncated`, with nothing landed (docs/FIXED-FORM-ALGORITHM.md §5.3's
// refusal table: "no bytes at all ... | truncated", "shorter than 20 bytes, the
// form byte having already answered | truncated"; "There is no `malformed` flag
// on this form's report"). `refused` is set, `reason` is truncated and
// `damaged` stays false: the verdict is compared, the name is diagnostic. It is not TableMessageReason::layout_malformed: that name is for a
// KNOWN hash whose bytes lie, and a file under twenty bytes has not got far
// enough to carry one.
//
// WHAT GUARDS bytes == 0 ON THIS LEG: the emitted FixedLoad's step-1 line
// `if ( data == NULL || bytes < 1 ) { report->refused = true; report->reason = truncated; return -1; }`
// (fixedform.go, emitted just ABOVE the form-byte read), read verbatim, and it
// gives the SAME five-part answer as bytes 1..19 — so the loop runs k = 0 ..
// kTableFixedHeaderBytes + 4 exclusive, the constant (16) plus the layout's
// u32 length being the doc's twenty, never a hardcoded 20. The destination is
// proved untouched by memcmp of the whole back array against `fresh`, the
// reader's own default image memcpy'd over every slot before each short read.
//
// CONTROL 2 (the short-file guard disabled) is recorded in RESULT.md.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedFormUnder20Bytes(t *testing.T) {
	corpus := cppFixedCorpus(t)
	row := "field_append"
	file := filepath.Join(corpus, "new_field_append.bin")

	u := loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VNEW_"+row+".schema"))
	root := cppFixedRootName(t, u)

	body := fmt.Sprintf(`
    if ( n < 1 ) { printf( "under20: the full-length file did not read: n=%%lld reason=%%d\n", (long long) n, (int) r.reason ); return 1; }
    if ( r.damaged || r.refused )
        { printf( "under20: the full-length file must read clean: malformed=%%d refused=%%d\n", (int) r.damaged, (int) r.refused ); return 1; }
    {
        %s fresh;
        memset( &fresh, 0, sizeof( fresh ) );
        %sReset( fresh );
        for ( int64_t k = 0; k < kTableFixedHeaderBytes + 4; ++k )
        {
            memset( &r, 0, sizeof( r ) );
            for ( int64_t s = 0; s < 8; ++s ) { memcpy( &back[s], &fresh, sizeof( fresh ) ); }
            n = %sFixedLoad( back, 8, data, k, plan, 4096, NULL, &r );
            if ( n != -1 ) { printf( "under20: at k=%%lld a file shorter than the header refuses, not n=%%lld\n", (long long) k, (long long) n ); return 1; }
            if ( !r.refused ) { printf( "under20: at k=%%lld a short file refuses truncated: refused=%%d\n", (long long) k, (int) r.refused ); return 1; }
            if ( r.damaged ) { printf( "under20: at k=%%lld the fixed form's report has no malformed flag (§5.3): malformed=%%d\n", (long long) k, (int) r.damaged ); return 1; }
            if ( r.reason != TableFixedReason::truncated ) { printf( "under20: at k=%%lld reason is %%d, not truncated\n", (long long) k, (int) r.reason ); return 1; }
            if ( r.widened != 0 || r.unknown != 0 )
                { printf( "under20: at k=%%lld REFUSE is total: no counter moves (w=%%d u=%%d km=%%d c=%%d d=%%d)\n", (long long) k, r.widened, r.unknown, r.refused, r.refused, r.refused ); return 1; }
            for ( int64_t s = 0; s < 8; ++s )
            {
                if ( memcmp( &back[s], &fresh, sizeof( fresh ) ) != 0 ) { printf( "under20: at k=%%lld slot %%lld: a refused read wrote destination bytes\n", (long long) k, (long long) s ); return 1; }
            }
        }
    }
`, root, root, root)

	out, err := cppRunVersionProbe(t, row, file, body, "")
	if err != nil {
		t.Fatalf("fixed_form_under_20_bytes: %v\n%s", err, out)
	}
}
