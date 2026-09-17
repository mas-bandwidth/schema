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

#include <stdio.h>
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

void fixed_ut1_read_own( const uint8_t * data, int64_t bytes )
{
    UtRoot back;
    TableReport r;

    fixed_check( ut_root_fixed_plan[3].op == kTableFixedText, "C two lanes: the plan's fourth entry is the text" );
    fixed_check( ut_root_fixed_plan[3].arg == 2, "C two lanes: arg is the SECOND arm's ordinal" );
    fixed_check( ut_root_fixed_plan[3].meta == kTableFixedTextUtf8, "C two lanes: meta is the utf8 flavour" );

    memset( &r, 0, sizeof( r ) );
    fixed_check( ut_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 1,
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
    int32_t i;

    (void) bytes;
    for ( i = 0; i < ut_root_fixed_plan_count; i++ ) { shared[i] = ut_root_fixed_plan[i]; }
    shared[3].meta = shared[3].arg; /* ONE LANE, as it was */

    ut_root_reset( &wrong );
    memset( &r, 0, sizeof( r ) );
    body = data + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( ut_root_fixed_layout ) + 8;
    table_fixed_run( shared, ut_root_fixed_plan_count, ut_root_fixed_plan_guarded, body, (uint8_t *) &wrong, &r );
    fixed_check( wrong.pick.as.b.label_length != 7 || strcmp( wrong.pick.as.b.label, "seven77" ) != 0,
                 "C NEGATIVE CONTROL: one shared lane really does read the arm's string wrong" );
}

/* ...and the other direction: a UT2 record whose arm is `b` at ordinal 3. */
void fixed_ut1_read_ut2( const uint8_t * data, int64_t bytes )
{
    UtRoot back;
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    fixed_check( ut_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 1,
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
    fixed_check( ut_root_fixed_load( &back, 1, file, n, g_plan, PlanCapacity, NULL, &r ) == 1,
                 "C bounds: the union record reads" );
    fixed_check( back.pick.type == PICK_TYPE_NONE, "C ORDINAL: a tag past the arm count lands None" );
    fixed_check( r.clamped == 1, "C ORDINAL: and counts as a clamp" );
    fixed_check( back.head == 1 && back.tail == 2, "C ORDINAL: the rest of the record stands" );
}

/* W6: THE PLAN IS PARTITIONED (docs/FIXED-FORM-ALGORITHM.md §4.1, §4.4, fix 6).
   One definition, called from the translation unit that names each generation's
   types — because C has no namespace to put them all in one. The plan arrives
   as a void pointer so this header names no generated type; the runtime's
   TableFixedEntry is the only type behind it. */
void fixed_partition_is_held( const void * plan_raw, int32_t count, int32_t split, const char * who )
{
    const TableFixedEntry * plan = (const TableFixedEntry *) plan_raw;
    char what[256];
    int32_t i;

    snprintf( what, sizeof( what ), "W6: %s — split is in range", who );
    fixed_check( split >= 0 && split <= count, what );

    for ( i = 0; i < count; ++i )
    {
        int guarded = plan[i].guard != SCHEMA_TABLE_FIXED_NO_GUARD;
        if ( i < split && guarded )
        {
            snprintf( what, sizeof( what ), "W6: %s — entry %d is below the split and carries a GUARD", who, (int) i );
            fixed_check( 0, what );
        }
        if ( i >= split && !guarded )
        {
            snprintf( what, sizeof( what ), "W6: %s — entry %d is above the split and carries NO guard", who, (int) i );
            fixed_check( 0, what );
        }
    }

    /* THE BOUNDARY PAIR: had the coalescer merged across the split, the entry
       below it and the entry at it would be one entry. They are two, and this
       says why they have to be. */
    if ( split > 0 && split < count )
    {
        const TableFixedEntry * a = &plan[split - 1];
        const TableFixedEntry * b = &plan[split];
        int mergeable = a->op == kTableFixedCopy && b->op == kTableFixedCopy &&
                        a->guard == b->guard && a->arg == b->arg &&
                        a->src + a->size == b->src && a->dst + a->size == b->dst;
        snprintf( what, sizeof( what ), "W6: %s — the pair at the split was not coalesced across it", who );
        fixed_check( !mergeable, what );
    }
}

void fixed_ut1_partition( void )
{
    fixed_partition_is_held( ut_root_fixed_plan, ut_root_fixed_plan_count, ut_root_fixed_plan_guarded, "UT1 identity plan" );
    fixed_check( ut_root_fixed_plan_guarded < ut_root_fixed_plan_count,
                 "W6: UT1's identity plan really has a guarded half, so the case is not vacuous" );
    fixed_check( ut_root_fixed_plan_guarded > 0, "W6: and an unguarded one" );

    /* AND A COMPILED PLAN, which is the half an identity plan cannot speak for:
       this build's own layout compiled to a plan and the same property read off
       it — once a guarded entry has been seen, no unguarded entry follows. The
       stranger's cross-generation half is retired by §5.6 and owed to the
       lineage harness, so the compiled walk is proved against its own layout. */
    {
        TableFixedLayoutView parsed;
        static TableFixedEntry compiled[1024];
        TableReport cr;
        int why = SCHEMA_TABLE_LAYOUT_MALFORMED;
        int32_t guarded = 0;
        uint32_t fill_at = 0;
        int32_t fill_count = 0;
        int32_t made, seen_guarded = -1, guarded_count = 0, i;

        memset( &cr, 0, sizeof( cr ) );
        fixed_check( table_fixed_parse_layout( ut_root_fixed_layout, (int64_t) sizeof( ut_root_fixed_layout ), &parsed, &why ) != 0,
                     "W6: UT1's own layout parses" );
        made = table_fixed_compile( &parsed, ut_root_fixed_layout, (int32_t) sizeof( ut_root_fixed_layout ),
                                    ut_root_fixed_dst, ut_root_fixed_cover, ut_root_fixed_cover_count,
                                    compiled, 1024, &guarded, &fill_at, &fill_count, &cr );
        fixed_check( made > 0, "W6: UT1's own layout compiles" );
        for ( i = 0; i < made; ++i )
        {
            if ( compiled[i].guard != SCHEMA_TABLE_FIXED_NO_GUARD )
            {
                if ( seen_guarded < 0 ) { seen_guarded = i; }
                guarded_count++;
            }
            else
            {
                fixed_check( seen_guarded < 0, "W6: a COMPILED plan puts every unguarded entry in front of every guarded one" );
            }
        }
        fixed_check( guarded_count > 0, "W6: the compiled plan really has guarded entries, so the case is not vacuous" );
    }
}
