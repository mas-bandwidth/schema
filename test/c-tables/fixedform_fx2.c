/* FX2's half of the fixed form's versioning conformance (docs/SPEC-TABLES.md
   §3.4). The only translation unit that names tblfx2's types. */

#include <string.h>

#include "FX2Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

int64_t fixed_fx2_bytes( void ) { return fx_root_fixed_measure( 1 ); }

int64_t fixed_fx2_write( uint8_t * buffer, int64_t capacity )
{
    FxRoot two;
    fx_root_reset( &two );
    two.keep = 5150u;
    two.narrow = 70000u;      /* past uint16: FX1 must NOT narrow it, it must skip */
    two.renamed_to = 808;
    two.added = 12;
    two.nested.a = 33;
    two.nested.b = 44;
    two.extra.x = 5;
    two.extra.y = 6;
    return fx_root_fixed_save( &two, 1, buffer, capacity );
}

/* CASE 2: AN OLDER WRITER — a WIDENED field decoded exactly and counted, a
   RENAMED field arriving under `was =`, a field this reader has that the
   record does not (the prefill's declared default stands), a whole nested type
   the writer never carried, and the one field the writer has that this reader
   cannot name. */
void fixed_fx2_read_fx1( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    int64_t n;
    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r );
    fixed_check( n == 1, "older writer: one record" );
    fixed_check( back.keep == 4242u, "older writer: an unmoved field" );
    fixed_check( back.narrow == 40000u, "WIDENED: uint16 into uint32, exactly" );
    fixed_check( r.widened == 1, "WIDENED: one widened counts" );
    fixed_check( back.renamed_to == 321, "RENAMED: `was =` keeps the wire id" );
    fixed_check( back.added == 11, "MISSING: a field the writer does not carry takes its declared default" );
    fixed_check( back.extra.x == 0 && back.extra.y == 0, "MISSING: a whole nested type takes its defaults" );
    fixed_check( back.nested.a == 111 && back.nested.b == 222, "older writer: the nesting" );
    fixed_check( back.blob_length == 4 && back.blob[0] == 0xDE && back.blob[1] == 0xAD &&
                 back.blob[2] == 0xBE && back.blob[3] == 0xEF,
                 "BYTES(N): the compiled plan lands the buffer in the buffer and the length in the length" );
    fixed_check( back.blob[4] == 0 && back.blob[5] == 0, "BYTES(N): the slack past the live length is zero" );
    fixed_check( r.unknown == 1, "older writer: `gone` is the one field this reader cannot name" );
    fixed_check( r.kind_mismatch == 0 && !r.malformed && !r.refused, "older writer: nothing else fired" );
}

/* THE NEGATIVE CONTROL FOR THE `bytes(N)` ROW. The two columns are swapped
   back to the TEXT convention on a copy of this build's own rows, the same plan
   is compiled from the same layout, and the same record comes out WRONG — the
   count written into the buffer and the bytes written over the length.

   The row is found BY ITS SHAPE and never by its index: stride one and a live
   count is a `bytes(N)` and nothing else, so the control does not quietly stop
   pointing at it the day a field moves. The destination is OVERSIZED on
   purpose: the wrong rows put an element destination where the length field is,
   and six bytes of elements past a four-byte field is a step outside the
   storage — the control gives it room to be wrong, so what reports the bug is
   the value that comes back and not the sanitizer. */
void fixed_fx2_bytes_row_control( const uint8_t * data, int64_t bytes )
{
    static TableFixedDst swapped[64];
    static uint64_t storage[ ( sizeof( FxRoot ) / 8 ) + 16 ];
    TableFixedLayoutView theirs;
    int why = 0;
    const uint8_t * body;
    size_t rows = sizeof( fx_root_fixed_dst ) / sizeof( fx_root_fixed_dst[0] );
    size_t i, found = rows;
    uint32_t d;
    int pass;

    (void) bytes;
    fixed_check( rows <= 64, "C bytes row: the rows fit the control's copy" );
    for ( i = 0; i < rows; i++ )
    {
        swapped[i] = fx_root_fixed_dst[i];
        if ( found == rows && swapped[i].stride == 1u && swapped[i].counted != 0u ) { found = i; }
    }
    fixed_check( found != rows, "C bytes row: the `bytes(N)` row is the one with stride one and a live count" );
    d = swapped[found].dst;
    swapped[found].dst = swapped[found].aux; /* the TEXT convention, as it was */
    swapped[found].aux = d;

    fixed_check( table_fixed_parse_layout( data + kTableFixedHeaderBytes + 4,
                                           (int64_t) table_fixed_get32( data + kTableFixedHeaderBytes ),
                                           &theirs, &why ),
                 "C bytes row: the writer's layout parses" );
    body = data + kTableFixedHeaderBytes + 4 + (int64_t) table_fixed_get32( data + kTableFixedHeaderBytes ) + 8;

    for ( pass = 0; pass < 2; pass++ )
    {
        const TableFixedDst * rowset = pass == 0 ? fx_root_fixed_dst : swapped;
        int32_t guarded = 0;
        int32_t made;
        FxRoot * back;
        TableReport r;
        int right;

        made = table_fixed_compile( &theirs, fx_root_fixed_layout, (int32_t) sizeof( fx_root_fixed_layout ),
                                    rowset, g_plan, PlanCapacity, &guarded, NULL );
        fixed_check( made > 0, "C bytes row: the plan compiles either way — the rows are not what refuses" );
        memset( storage, 0, sizeof( storage ) );
        back = (FxRoot *) (void *) storage;
        fx_root_reset( back );
        memset( &r, 0, sizeof( r ) );
        table_fixed_run( g_plan, made, guarded, body, (uint8_t *) back, &r );
        right = back->blob_length == 4 && back->blob[0] == 0xDE && back->blob[1] == 0xAD &&
                back->blob[2] == 0xBE && back->blob[3] == 0xEF;
        if ( pass == 0 )
        {
            fixed_check( right, "C BYTES(N): the array row lands the buffer in the buffer and the length in the length" );
        }
        else
        {
            fixed_check( !right, "C NEGATIVE CONTROL: the text row really does write the count into the buffer" );
        }
    }
}

/* A READ THAT CLAIMS NOTHING ABOUT THE VALUES. A form-3 file with one bit
   flipped is a layout whose child sizes may not sum to its parent's, a tree that
   may not close, a chain a thousand deep, or a record count that divides
   wrongly. Every one of those must come out as a refusal by name, a malformed
   read, or a read that lands values — and never as a step outside the buffer,
   which is what the sanitized build of this file is here to say. */
void fixed_fx2_probe( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    fx_root_reset( &back );
    (void) fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r );
}
