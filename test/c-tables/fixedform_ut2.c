/* UT2.s half of TWO LANES (docs/SPEC-TABLES.md §3.4). UT2 inserted an arm IN
   THE MIDDLE, so its string(8) is arm 3: the text entry.s guard is that
   ordinal and its flavour is neither number, and the shared-lane control below
   watches one lane read the same record wrong. This translation unit is the ONLY one
   that names tblut2's types; see fixedform.h for why there is more than one. */

#include <string.h>

#include "UT2Table.h"
#include "fixedform.h"

int64_t fixed_ut2_bytes( void ) { return tblut2_ut_root_fixed_measure( 1 ); }

int64_t fixed_ut2_write( uint8_t * buffer, int64_t capacity )
{
    tblut2_ut_root_t v;
    tblut2_ut_root_reset( &v );
    v.head = 7;
    v.tail = 8;
    v.pick.type = TBLUT2_PICK_TYPE_B;
    memcpy( v.pick.as.b.label, "third", 6 );
    v.pick.as.b.label_length = 5;
    v.pick.as.b.m = 555;
    return tblut2_ut_root_fixed_save( &v, 1, buffer, capacity );
}

/* THE NEGATIVE CONTROL — the bug itself, watched failing. With one shared lane
   the flavour IS the guard's ordinal, and UT2's string is the THIRD arm: 3 is
   the bytes flavour, so a full-length string read through the identity plan
   with the flavour put back into the guard's lane reads as bytes and is never
   terminated. The same record through the two lanes is. */
void fixed_ut2_shared_lane_control( void )
{
    static uint8_t data[512];
    static tblut2_table_fixed_entry_t plan[64];
    tblut2_ut_root_t full;
    tblut2_ut_root_t read;
    tblut2_table_report_t r;
    const uint8_t * body;
    int64_t n;
    int32_t i, text = -1, lanes;

    tblut2_ut_root_reset( &full );
    full.head = 5;
    full.tail = 6;
    full.pick.type = TBLUT2_PICK_TYPE_B;
    memcpy( full.pick.as.b.label, "eighteen", 9 ); /* eight characters: no slack to terminate it */
    full.pick.as.b.label_length = 8;
    full.pick.as.b.m = 77;
    n = tblut2_ut_root_fixed_save( &full, 1, data, (int64_t) sizeof( data ) );
    fixed_check( n == tblut2_ut_root_fixed_measure( 1 ), "C UT2 full-length save" );
    for ( i = 0; i < tblut2_ut_root_fixed_plan_count; i++ )
    {
        if ( tblut2_ut_root_fixed_plan[i].op == TBLUT2_K_TABLE_FIXED_TEXT ) { text = i; }
    }
    fixed_check( text >= 0 && tblut2_ut_root_fixed_plan[text].arg == 3 && tblut2_ut_root_fixed_plan[text].meta == TBLUT2_K_TABLE_FIXED_TEXT_UTF8,
                 "C two lanes: UT2's text entry is the third arm's, flavoured utf8" );
    if ( text < 0 ) { return; }
    body = data + TBLUT2_K_TABLE_FIXED_HEADER_BYTES + 4 + (int64_t) sizeof( tblut2_ut_root_fixed_layout ) + 8;
    for ( lanes = 2; lanes >= 1; lanes-- )
    {
        int whole;
        for ( i = 0; i < tblut2_ut_root_fixed_plan_count; i++ ) { plan[i] = tblut2_ut_root_fixed_plan[i]; }
        if ( lanes == 1 ) { plan[text].meta = (uint8_t) plan[text].arg; } /* ONE LANE, as it was */
        tblut2_ut_root_reset( &read );
        memset( read.pick.as.b.label, 'X', sizeof( read.pick.as.b.label ) );
        memset( &r, 0, sizeof( r ) );
        tblut2_table_fixed_run( plan, tblut2_ut_root_fixed_plan_count, tblut2_ut_root_fixed_plan_guarded, body, (uint8_t *) &read, &r, tblut2_ut_root_fixed_guards, NULL );
        whole = read.pick.as.b.label_length == 8 && memcmp( read.pick.as.b.label, "eighteen", 9 ) == 0;
        if ( lanes == 2 ) { fixed_check( whole, "C two lanes: the full-length string reads terminated" ); }
        else { fixed_check( !whole, "C NEGATIVE CONTROL: one shared lane really does read the arm's string wrong" ); }
    }
}
