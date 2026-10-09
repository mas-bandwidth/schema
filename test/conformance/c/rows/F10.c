/* F10 — no_layout, the C leg of the schema matrix cell (docs/roadmap.sexp
   row group "plan-selection"; docs/FIXED-FORM-ALGORITHM.md:148 step 7):

       "per record: if `LE(8, at) != h`, `REFUSE no_layout`; then §4.4"

   A FIXED-form record whose per-record hash is not the file's own header hash
   refuses under the NAME `no_layout`, before one destination byte is written.
   The header hash is untouched, so the file still selects this build's own
   known layout — only the record's hash is forged, which is the no_layout
   law and not layout_newer (hash outside the lineage) or layout_malformed
   (a known hash over different layout bytes).

   VECTOR DERIVATION. A clean one-record V1 `Cell` file is what the generated
   writer itself lays down; `cell_fixed_save` stamps every record's per-record
   hash with the header's own hash, LE-encoded. That record hash sits at:

       header (kTableFixedHeaderBytes = 16)
     + the layout's u32 length (4)
     + the layout itself (cell_fixed_layout_bytes)
     = rec_at

   Inverting one byte there makes LE(8, at) != h while every earlier check
   (form byte, reserved bytes, layout length, hash selection, layout memcmp,
   tail a whole record) still passes, so the load reaches the per-record hash
   check and refuses there. The caller's storage is poisoned 0x5A first:
   REFUSE is total (§5.8), so no counter may move and no byte may be written.

   SELF-CONTAINED: reaches the generated fixed form through the SAME include
   path the conformance driver uses (build/tables-generated-c/v1), includes
   only V1Table.h, and depends on no other rows/ file and no shared fixture. */

#include <stdio.h>
#include <stdint.h>
#include <string.h>

#include "V1Table.h"

int main( void )
{
    Cell source;
    Cell back;
    TableReport r;
    uint8_t wire[256];
    int64_t bytes, rec_at, i, n;

    cell_reset( &source );
    source.power = 7;
    memcpy( source.label, "hello", 6 );
    source.label_length = 5;

    bytes = cell_fixed_measure( 1 );
    if ( bytes < 0 || bytes > (int64_t) sizeof( wire ) )
    {
        printf( "F10 no_layout: measure out of range: %lld\n", (long long) bytes );
        return 1;
    }
    if ( cell_fixed_save( &source, 1, wire, bytes ) != bytes )
    {
        printf( "F10 no_layout: the clean save failed\n" );
        return 1;
    }

    /* the forge: record 0's own hash, one byte inverted */
    rec_at = kTableFixedHeaderBytes + 4 + cell_fixed_layout_bytes;
    wire[rec_at] ^= 0xff;

    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );

    n = cell_fixed_load( &back, 1, wire, bytes, NULL, 0, NULL, &r );

    if ( n != -1 )
    {
        printf( "F10 no_layout: the forged record hash did not refuse: n=%lld\n", (long long) n );
        return 1;
    }
    if ( !r.refused )
    {
        printf( "F10 no_layout: refused not set\n" );
        return 1;
    }
    if ( r.reason != SCHEMA_TABLE_NO_LAYOUT )
    {
        printf( "F10 no_layout: reason=%d, not no_layout\n", r.reason );
        return 1;
    }
    if ( r.malformed )
    {
        printf( "F10 no_layout: malformed set — a refusal by name is not a malformed file\n" );
        return 1;
    }
    if ( r.clamped || r.unknown || r.kind_mismatch || r.widened || r.duplicate )
    {
        printf( "F10 no_layout: a counter moved: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n",
                r.clamped, r.unknown, r.kind_mismatch, r.widened, r.duplicate );
        return 1;
    }
    for ( i = 0; i < (int64_t) sizeof( back ); ++i )
    {
        if ( ( (const uint8_t *) &back )[i] != 0x5A )
        {
            printf( "F10 no_layout: byte %lld is 0x%02x — REFUSE wrote a destination byte\n",
                    (long long) i, ( (const uint8_t *) &back )[i] );
            return 1;
        }
    }

    printf( "F10 no_layout: a record whose hash names no held layout refuses no_layout; nothing decoded, no byte written\n" );
    return 0;
}
