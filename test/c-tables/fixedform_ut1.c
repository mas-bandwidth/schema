/* UT1's half of TWO LANES, BECAUSE THEY ARE TWO FACTS (docs/SPEC-TABLES.md
   §3.4). A plan entry carries the ordinal the GUARD byte must hold for the
   entry to run, and the argument the entry's OWN OP takes — for a text entry,
   its flavour. They had one lane between them, and a string(N) under a union's
   arm could be guarded correctly or read with the right flavour and could not
   be both.

   The string sits under the SECOND arm, whose ordinal names no flavour; the
   collision is watched where it bites, under UT2's third arm (fixedform_ut2.c).
   This translation unit is the ONLY one that names tblut1's types; see
   fixedform.h for why there is more than one. */

#include <string.h>

#include "UT1Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static tblut1_table_fixed_entry_t g_plan[PlanCapacity];

static void fill( tblut1_ut_root_t * value )
{
    tblut1_ut_root_reset( value );
    value->head = 42;
    value->tail = 99;
    value->pick.type = TBLUT1_PICK_TYPE_B; /* the SECOND arm */
    memcpy( value->pick.as.b.label, "seven77", 8 );
    value->pick.as.b.label_length = 7;
    value->pick.as.b.m = 1234;
}

int64_t fixed_ut1_bytes( void ) { return tblut1_ut_root_fixed_measure( 1 ); }

int64_t fixed_ut1_write( uint8_t * buffer, int64_t capacity )
{
    tblut1_ut_root_t v;
    fill( &v );
    return tblut1_ut_root_fixed_save( &v, 1, buffer, capacity );
}

void fixed_ut1_read_own( const uint8_t * data, int64_t bytes )
{
    tblut1_ut_root_t back;
    tblut1_table_report_t r;

    fixed_check( tblut1_ut_root_fixed_plan[3].op == TBLUT1_K_TABLE_FIXED_TEXT, "C two lanes: the plan's fourth entry is the text" );
    fixed_check( tblut1_ut_root_fixed_plan[3].arg == 2, "C two lanes: arg is the SECOND arm's ordinal" );
    fixed_check( tblut1_ut_root_fixed_plan[3].meta == TBLUT1_K_TABLE_FIXED_TEXT_UTF8, "C two lanes: meta is the utf8 flavour" );

    memset( &r, 0, sizeof( r ) );
    fixed_check( tblut1_ut_root_fixed_load_tolerant( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 1,
                 "C two lanes: the record reads" );
    fixed_check( back.pick.type == TBLUT1_PICK_TYPE_B, "C two lanes: the arm" );
    fixed_check( back.pick.as.b.label_length == 7 && strcmp( back.pick.as.b.label, "seven77" ) == 0,
                 "C two lanes: the arm's string(8), whole" );
    fixed_check( back.pick.as.b.m == 1234 && back.head == 42 && back.tail == 99,
                 "C two lanes: the rest of the record" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused, "C two lanes: a clean read moves no counter" );
}

/* ...and the other direction: a UT2 record whose arm is `b` at ordinal 3. */
void fixed_ut1_read_ut2( const uint8_t * data, int64_t bytes )
{
    tblut1_ut_root_t back;
    tblut1_table_report_t r;
    memset( &r, 0, sizeof( r ) );
    fixed_check( tblut1_ut_root_fixed_load_tolerant( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 1,
                 "C two lanes, back: the record reads" );
    fixed_check( back.pick.type == TBLUT1_PICK_TYPE_B, "C two lanes, back: arm 3 lands as arm 2, by name" );
    fixed_check( back.pick.as.b.label_length == 5 && strcmp( back.pick.as.b.label, "third" ) == 0,
                 "C two lanes, back: the arm's string(8), whole" );
    fixed_check( back.pick.as.b.m == 555, "C two lanes, back: the arm's other field" );
}

/* A UNION TAG PAST THE ARM COUNT names no arm: it lands None — the same nothing
   an unset union holds — and COUNTS as a clamp (docs/SPEC-TABLES.md §3.4). */
void fixed_ut1_bounds( void )
{
    static uint8_t file[4096];
    tblut1_ut_root_t v, back;
    tblut1_table_report_t r;
    int64_t n;

    tblut1_ut_root_reset( &v );
    v.head = 1;
    v.tail = 2;
    v.pick.type = (tblut1_pick_type_t) 7; /* UT1 declares two arms */
    n = tblut1_ut_root_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
    fixed_check( n == tblut1_ut_root_fixed_measure( 1 ), "C bounds: UT1 save" );

    memset( &r, 0, sizeof( r ) );
    fixed_check( tblut1_ut_root_fixed_load_tolerant( &back, 1, file, n, g_plan, PlanCapacity, NULL, &r ) == 1,
                 "C bounds: the union record reads" );
    fixed_check( back.pick.type == TBLUT1_PICK_TYPE_NONE, "C ORDINAL: a tag past the arm count lands None" );
    fixed_check( r.clamped == 1, "C ORDINAL: and counts as a clamp" );
    fixed_check( back.head == 1 && back.tail == 2, "C ORDINAL: the rest of the record stands" );
}
