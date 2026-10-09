package ctable

// TestCFixedFormRaggedTail is F8 of schema#876, the fixed_form_ragged_tail
// row: a file whose record region is not a whole number of records is
// MALFORMED. It is a gap on go, cpp and cs and PARTIAL on c and elixir; on c
// nothing already exercises `rest % record_bytes != 0` — F7 (under-20) covers
// only the short-file half of the malformed row and the refusal rows read the
// true length — so this leg still owes the tail. It is F7's DIRECT SIBLING:
// the very next guard in the same emitted load. The two answers are `refused`
// (plus a `reason`) and `malformed`, and they are NEVER both set; a ragged tail
// is the RESIDUE of a bad file, not a refusal by name, so refused stays 0 and
// reason stays 0. `record_bytes` is the SELECTED lineage entry's record size
// (`{snake}_fixed_known[pick].record_bytes`), compiled into the reader, so the
// guard's FIRST arm `record_bytes <= 8` is NOT reachable from a file at all —
// only a reader whose own lineage declares a record under nine bytes; THIS row
// is the SECOND arm. `rest = bytes - 20 - 4 - layout_bytes` is the length the
// CALLER passes, so handing the reader `len + extra` for each extra in
// 1..record_bytes-1 makes rest short of a whole record by exactly extra;
// extra == record_bytes is one MORE whole record, not a ragged tail, so it is
// not asserted malformed. The destination is proved untouched by memcmp against
// `fresh`, the reader's own default image, memcpy'd back into slot 0 before
// every ragged read.

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCFixedFormRaggedTail(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_field_append")
	file := filepath.Join(corpus, "new_field_append.bin")
	snake := ir.RustSnake(cFixedRootName(t, cUnitOf(t, newer)))
	load := snake + "_fixed_load"
	recordBytes := snake + "_fixed_record_bytes"

	body := fmt.Sprintf(`
    if ( n < 1 ) { printf( "ragged: the full-length file did not read: n=%%lld reason=%%d\n", (long long) n, r.reason ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 )
        { printf( "ragged: the full-length file must read clean: malformed=%%d refused=%%d reason=%%d\n", r.malformed, r.refused, r.reason ); return 1; }
    for ( k = 1; k < (int) %s; ++k )
    {
        memset( &r, 0, sizeof( r ) );
        memcpy( &back[0], &fresh, sizeof( fresh ) ); /* the sentinel: the reader's own default image */
        n = %s( back, 8, data, len + k, plan, 4096, NULL, &r );
        if ( n != -1 ) { printf( "ragged: at extra=%%d a tail short of a whole record is malformed, not n=%%lld\n", k, (long long) n ); return 1; }
        if ( r.malformed != 1 ) { printf( "ragged: at extra=%%d malformed=%%d, not 1\n", k, r.malformed ); return 1; }
        if ( r.refused ) { printf( "ragged: at extra=%%d a ragged tail is the residue, never refused by name: refused=%%d\n", k, r.refused ); return 1; }
        if ( r.reason != 0 ) { printf( "ragged: at extra=%%d reason stays untouched, not %%d\n", k, r.reason ); return 1; }
        if ( r.widened != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 )
            { printf( "ragged: at extra=%%d MALFORMED is total: no counter moves (w=%%d u=%%d km=%%d c=%%d d=%%d)\n", k, r.widened, r.unknown, r.kind_mismatch, r.clamped, r.duplicate ); return 1; }
        if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 ) { printf( "ragged: at extra=%%d a malformed read wrote destination bytes\n", k ); return 1; }
    }
`, recordBytes, load)

	out, err := cRunVersionProbe(t, newer, nil, 0, file, body, "")
	if err != nil {
		t.Fatalf("fixed_form_ragged_tail: %v\n%s", err, out)
	}
}
