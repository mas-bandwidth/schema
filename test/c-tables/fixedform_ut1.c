/* UT1's half of TWO LANES, BECAUSE THEY ARE TWO FACTS (docs/SPEC-TABLES.md
   §3.4). A plan entry carries the ordinal the GUARD byte must hold for the
   entry to run, and the argument the entry's OWN OP takes — for a text entry,
   its flavour. They had one lane between them, and a string(N) under a union's
   arm could be guarded correctly or read with the right flavour and could not
   be both.

   The string sits under the SECOND arm on purpose: a string(N)'s flavour is 1
   and the second arm's ordinal is 2, so the collision does not merely lose a
   fact — it turns a byte string into a WIDE one, halving the bound and
   terminating two bytes at a time. This translation unit is the ONLY one that
   names tblut1's types; see fixedform.h for why there is more than one. */

#include <string.h>

#include "UT1Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

static void fill( UtRoot * value )
{
    ut_root_reset( value );
    value->head = 42;
    value->tail = 99;
    value->pick.type = PICK_TYPE_B; /* the SECOND arm */
    memcpy( value->pick.as.b.label, "seven77", 8 );
    value->pick.as.b.label_length = 7;
    value->pick.as.b.m = 1234;
}

int64_t fixed_ut1_bytes( void ) { return ut_root_fixed_measure( 1 ); }

int64_t fixed_ut1_write( uint8_t * buffer, int64_t capacity )
{
    UtRoot v;
    fill( &v );
    return ut_root_fixed_save( &v, 1, buffer, capacity );
}

static int32_t ut1_compile_own( TableFixedEntry * plan, int32_t capacity, int32_t * guarded )
{
    TableFixedLayoutView parsed;
    TableReport r;
    int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
    int32_t made;

    /* C identity rewrites the baked plan to copies, so the TEXT op — and the
       two lanes it carries — live on a plan compiled from this layout. */
    memset( &r, 0, sizeof( r ) );
    if ( !table_fixed_parse_layout( ut_root_fixed_layout, (int64_t) sizeof( ut_root_fixed_layout ), &parsed, &why ) )
    {
        return -1;
    }
    made = table_fixed_compile( &parsed, ut_root_fixed_layout, (int32_t) sizeof( ut_root_fixed_layout ),
                                ut_root_fixed_dst, plan, capacity, guarded, &r );
    return made;
}

void fixed_ut1_read_own( const uint8_t * data, int64_t bytes )
{
    UtRoot back;
    TableReport r;
    TableFixedEntry compiled[16];
    int32_t guarded = 0;
    int32_t made, i, text_i;

    made = ut1_compile_own( compiled, 16, &guarded );
    fixed_check( made > 0, "C two lanes: own layout compiles" );
    text_i = -1;
    for ( i = 0; i < made; i++ )
    {
        if ( compiled[i].op == kTableFixedText ) { text_i = i; break; }
    }
    fixed_check( text_i >= 0, "C two lanes: a compiled plan from this layout carries a text entry" );
    if ( text_i >= 0 )
    {
        fixed_check( compiled[text_i].arg == 2, "C two lanes: arg is the SECOND arm's ordinal" );
        fixed_check( compiled[text_i].meta == kTableFixedTextUtf8, "C two lanes: meta is the utf8 flavour" );
    }

    memset( &r, 0, sizeof( r ) );
    fixed_check( ut_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r ) == 1,
                 "C two lanes: the record reads" );
    fixed_check( back.pick.type == PICK_TYPE_B, "C two lanes: the arm" );
    fixed_check( back.pick.as.b.label_length == 7 && strcmp( back.pick.as.b.label, "seven77" ) == 0,
                 "C two lanes: the arm's string(8), whole" );
    fixed_check( back.pick.as.b.m == 1234 && back.head == 42 && back.tail == 99,
                 "C two lanes: the rest of the record" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused, "C two lanes: a clean read moves no counter" );
}

/* THE NEGATIVE CONTROL — the bug itself, watched failing. The flavour is put
   back into the guard's lane, which is exactly what the one shared lane did,
   and the SAME record read by the SAME loop comes out wrong: the arm's ordinal
   2 reads as kTableFixedTextWide, so the length is halved and the terminator
   lands two bytes early. */
void fixed_ut1_shared_lane_control( const uint8_t * data, int64_t bytes )
{
    static TableFixedEntry shared[16];
    UtRoot wrong;
    TableReport r;
    const uint8_t * body;
    int32_t i, guarded = 0, made, text_i;

    (void) bytes;
    made = ut1_compile_own( shared, 16, &guarded );
    text_i = -1;
    for ( i = 0; i < made; i++ )
    {
        if ( shared[i].op == kTableFixedText ) { text_i = i; break; }
    }
    if ( text_i >= 0 ) { shared[text_i].meta = shared[text_i].arg; } /* ONE LANE, as it was */

    ut_root_reset( &wrong );
    memset( &r, 0, sizeof( r ) );
    body = data + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( ut_root_fixed_layout ) + 8;
    table_fixed_run( shared, made, guarded, body, (uint8_t *) &wrong, &r );
    fixed_check( wrong.pick.as.b.label_length != 7 || strcmp( wrong.pick.as.b.label, "seven77" ) != 0,
                 "C NEGATIVE CONTROL: one shared lane really does read the arm's string wrong" );
}

/* ...and the other direction: a UT2 record whose arm is `b` at ordinal 3. */
void fixed_ut1_read_ut2( const uint8_t * data, int64_t bytes )
{
    UtRoot back;
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    fixed_check( ut_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r ) == 1,
                 "C two lanes, back: the record reads" );
    fixed_check( back.pick.type == PICK_TYPE_B, "C two lanes, back: arm 3 lands as arm 2, by name" );
    fixed_check( back.pick.as.b.label_length == 5 && strcmp( back.pick.as.b.label, "third" ) == 0,
                 "C two lanes, back: the arm's string(8), whole" );
    fixed_check( back.pick.as.b.m == 555, "C two lanes, back: the arm's other field" );
}

/* A UNION TAG PAST THE ARM COUNT names no arm: it lands None — the same nothing
   an unset union holds — and COUNTS as a clamp (docs/SPEC-TABLES.md §3.4). */
void fixed_ut1_bounds( void )
{
    static uint8_t file[4096];
    UtRoot v, back;
    TableReport r;
    int64_t n;

    ut_root_reset( &v );
    v.head = 1;
    v.tail = 2;
    v.pick.type = (PickType) 7; /* UT1 declares two arms */
    n = ut_root_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
    fixed_check( n == ut_root_fixed_measure( 1 ), "C bounds: UT1 save" );

    memset( &r, 0, sizeof( r ) );
    fixed_check( ut_root_fixed_load( &back, 1, file, n, g_plan, PlanCapacity, &r ) == 1,
                 "C bounds: the union record reads" );
    fixed_check( back.pick.type == PICK_TYPE_NONE, "C ORDINAL: a tag past the arm count lands None" );
    fixed_check( r.clamped == 1, "C ORDINAL: and counts as a clamp" );
    fixed_check( back.head == 1 && back.tail == 2, "C ORDINAL: the rest of the record stands" );
}
