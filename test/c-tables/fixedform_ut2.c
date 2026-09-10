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
static TableFixedEntry g_plan[PlanCapacity];

int64_t fixed_ut2_bytes( void ) { return ut_root_fixed_measure( 1 ); }

int64_t fixed_ut2_write( uint8_t * buffer, int64_t capacity )
{
    UtRoot v;
    ut_root_reset( &v );
    v.head = 7;
    v.tail = 8;
    v.pick.type = PICK_TYPE_B;
    memcpy( v.pick.as.b.label, "third", 6 );
    v.pick.as.b.label_length = 5;
    v.pick.as.b.m = 555;
    return ut_root_fixed_save( &v, 1, buffer, capacity );
}

void fixed_ut2_read_ut1( const uint8_t * data, int64_t bytes )
{
    UtRoot back;
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    fixed_check( ut_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r ) == 1,
                 "C two lanes, compiled: the record reads" );
    fixed_check( back.pick.type == PICK_TYPE_B,
                 "C two lanes, compiled: the arm is remapped by NAME, not by ordinal" );
    fixed_check( back.pick.as.b.label_length == 7 && strcmp( back.pick.as.b.label, "seven77" ) == 0,
                 "C two lanes, compiled: the arm's string(8), whole" );
    fixed_check( back.pick.as.b.m == 1234 && back.head == 42 && back.tail == 99,
                 "C two lanes, compiled: the rest of the record" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused,
                 "C two lanes, compiled: a clean read moves no counter" );
}
