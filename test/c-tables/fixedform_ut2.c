/* UT2's half of TWO LANES (docs/SPEC-TABLES.md §3.4). UT2 inserted an arm IN
   THE MIDDLE, so the string(8) that is arm 2 in a UT1 record is arm 3 here and
   the read is a COMPILED plan: the entries for b's fields must be guarded by
   THEIR ordinal while the tag this reader stores is MY ordinal, and the text
   entry's flavour is neither number. This translation unit is the ONLY one
   that names tblut2's types; see fixedform.h for why there is more than one. */

#include <string.h>

#include "UT2Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static tblut2_table_fixed_entry_t g_plan[PlanCapacity];

/* GCC -O2 can inline the fixed-copy unroll through this legacy-plan read into
   the caller's 32-byte UtRoot object and combine a static destination offset
   with a different entry's maximum copy width, reporting a false array bound.
   Keep the typed read boundary opaque just as ctable/identity_test.go does for
   its small stack destination; every plan entry still runs through the same
   checked fixed loader. */
#if defined(__GNUC__)
__attribute__((noinline))
#endif
static int64_t fixed_ut2_load_tolerant( tblut2_ut_root_t * back, const uint8_t * data, int64_t bytes, tblut2_table_report_t * report )
{
    return tblut2_ut_root_fixed_load_tolerant( back, 1, data, bytes, g_plan, PlanCapacity, NULL, report );
}

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

void fixed_ut2_read_ut1( const uint8_t * data, int64_t bytes )
{
    tblut2_ut_root_t back = { 0 };
    tblut2_table_report_t r;
    memset( &r, 0, sizeof( r ) );
    fixed_check( fixed_ut2_load_tolerant( &back, data, bytes, &r ) == 1,
                 "C two lanes, compiled: the record reads" );
    fixed_check( back.pick.type == TBLUT2_PICK_TYPE_B,
                 "C two lanes, compiled: the arm is remapped by NAME, not by ordinal" );
    fixed_check( back.pick.as.b.label_length == 7 && strcmp( back.pick.as.b.label, "seven77" ) == 0,
                 "C two lanes, compiled: the arm's string(8), whole" );
    fixed_check( back.pick.as.b.m == 1234 && back.head == 42 && back.tail == 99,
                 "C two lanes, compiled: the rest of the record" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused,
                 "C two lanes, compiled: a clean read moves no counter" );
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
        tblut2_table_fixed_run( plan, tblut2_ut_root_fixed_plan_count, tblut2_ut_root_fixed_plan_guarded, body, (uint8_t *) &read, &r, tblut2_ut_root_fixed_guards );
        whole = read.pick.as.b.label_length == 8 && memcmp( read.pick.as.b.label, "eighteen", 9 ) == 0;
        if ( lanes == 2 ) { fixed_check( whole, "C two lanes: the full-length string reads terminated" ); }
        else { fixed_check( !whole, "C NEGATIVE CONTROL: one shared lane really does read the arm's string wrong" ); }
    }
}
