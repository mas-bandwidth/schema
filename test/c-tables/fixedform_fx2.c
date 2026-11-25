/* FX2's half of the fixed form's versioning conformance (docs/SPEC-TABLES.md
   §3.4). The only translation unit that names tblfx2's types. */

#include <string.h>

#include "FX2Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static tblfx2_table_fixed_entry_t g_plan[PlanCapacity];

int64_t fixed_fx2_bytes( void ) { return tblfx2_fx_root_fixed_measure( 1 ); }

int64_t fixed_fx2_write( uint8_t * buffer, int64_t capacity )
{
    tblfx2_fx_root_t two;
    tblfx2_fx_root_reset( &two );
    two.keep = 5150u;
    two.narrow = 70000u;      /* past uint16: FX1 must NOT narrow it */
    two.renamed_to = 808;
    two.added = 12;
    two.nested.a = 33;
    two.nested.b = 44;
    two.extra.x = 5;
    two.extra.y = 6;
    return tblfx2_fx_root_fixed_save( &two, 1, buffer, capacity );
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
    static tblfx2_table_fixed_dst_t swapped[64];
    static uint64_t storage[ ( sizeof( tblfx2_fx_root_t ) / 8 ) + 16 ];
    tblfx2_table_fixed_layout_view_t theirs;
    int why = 0;
    const uint8_t * body;
    size_t rows = sizeof( tblfx2_fx_root_fixed_dst ) / sizeof( tblfx2_fx_root_fixed_dst[0] );
    size_t i, found = rows;
    uint32_t d;
    int pass;
    static uint8_t own[4096];
    static tblfx2_fx_root_t own_value;
    int64_t own_bytes;

    (void) bytes;
    fixed_check( rows <= 64, "C bytes row: the rows fit the control's copy" );
    for ( i = 0; i < rows; i++ )
    {
        swapped[i] = tblfx2_fx_root_fixed_dst[i];
        if ( found == rows && swapped[i].stride == 1u && swapped[i].counted != 0u ) { found = i; }
    }
    fixed_check( found != rows, "C bytes row: the `bytes(N)` row is the one with stride one and a live count" );
    d = swapped[found].dst;
    swapped[found].dst = swapped[found].aux; /* the TEXT convention, as it was */
    swapped[found].aux = d;

    /* FX1'S LAYOUT IS AN UNLAWFUL LINEAGE ENTRY FOR FX2: FX1 carries `gone`,
       which FX2 names nowhere, so COMPILE lays no plan and answers -3, the
       entry a load refuses layout_malformed (docs/FIXED-FORM-ALGORITHM.md §5.2
       MATCH, §5.4, §5.9 #30). The control therefore reads a record FX2 wrote
       itself: the rows under test are FX2's own, and so is the layout. */
    fixed_check( tblfx2_table_fixed_parse_layout( data + TBLFX2_K_TABLE_FIXED_HEADER_BYTES + 4,
                                           (int64_t) tblfx2_table_fixed_get32( data + TBLFX2_K_TABLE_FIXED_HEADER_BYTES ),
                                           &theirs, &why ),
                 "C bytes row: FX1's layout parses" );
    {
        int32_t g = 0, fc = 0;
        uint32_t fa = 0;
        fixed_check( tblfx2_table_fixed_compile( &theirs, tblfx2_fx_root_fixed_layout, (int32_t) sizeof( tblfx2_fx_root_fixed_layout ),
                                                 tblfx2_fx_root_fixed_dst, tblfx2_fx_root_fixed_cover, tblfx2_fx_root_fixed_cover_count,
                                                 g_plan, PlanCapacity, &g, &fa, &fc, NULL ) == -3,
                     "UNLAWFUL: FX1's `gone`, a field FX2 names nowhere, lays no plan (-3, layout_malformed)" );
    }
    tblfx2_fx_root_reset( &own_value );
    own_value.blob_length = 4;
    own_value.blob[0] = 0xDE; own_value.blob[1] = 0xAD; own_value.blob[2] = 0xBE; own_value.blob[3] = 0xEF;
    own_bytes = tblfx2_fx_root_fixed_save( &own_value, 1, own, (int64_t) sizeof( own ) );
    fixed_check( own_bytes > 0, "C bytes row: FX2 saves its own bytes(N) record" );
    if ( own_bytes <= 0 ) { return; }
    fixed_check( tblfx2_table_fixed_parse_layout( own + TBLFX2_K_TABLE_FIXED_HEADER_BYTES + 4,
                                           (int64_t) tblfx2_table_fixed_get32( own + TBLFX2_K_TABLE_FIXED_HEADER_BYTES ),
                                           &theirs, &why ),
                 "C bytes row: the writer's layout parses" );
    body = own + TBLFX2_K_TABLE_FIXED_HEADER_BYTES + 4 + (int64_t) tblfx2_table_fixed_get32( own + TBLFX2_K_TABLE_FIXED_HEADER_BYTES ) + 8;

    for ( pass = 0; pass < 2; pass++ )
    {
        const tblfx2_table_fixed_dst_t * rowset = pass == 0 ? tblfx2_fx_root_fixed_dst : swapped;
        int32_t guarded = 0;
        int32_t made;
        tblfx2_fx_root_t * back;
        tblfx2_table_report_t r;
        int right;

        uint32_t fill_at = 0;
        int32_t fill_count = 0;

        made = tblfx2_table_fixed_compile( &theirs, tblfx2_fx_root_fixed_layout, (int32_t) sizeof( tblfx2_fx_root_fixed_layout ),
                                    rowset, tblfx2_fx_root_fixed_cover, tblfx2_fx_root_fixed_cover_count,
                                    g_plan, PlanCapacity, &guarded, &fill_at, &fill_count, NULL );
        fixed_check( made > 0, "C bytes row: the plan compiles either way — the rows are not what refuses" );
        memset( storage, 0, sizeof( storage ) );
        back = (tblfx2_fx_root_t *) (void *) storage;
        tblfx2_fx_root_reset( back );
        memset( &r, 0, sizeof( r ) );
        tblfx2_table_fixed_run( g_plan, made, guarded, body, (uint8_t *) back, &r, NULL, NULL );
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
   wrongly. Every one of those must come out as a refusal or a read that lands
   values — and never as a step outside the buffer, which is what the sanitized
   build of this file is here to say. */
void fixed_fx2_probe( const uint8_t * data, int64_t bytes )
{
    tblfx2_fx_root_t back;
    tblfx2_table_report_t r;
    memset( &r, 0, sizeof( r ) );
    tblfx2_fx_root_reset( &back );
    (void) tblfx2_fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r );
}
