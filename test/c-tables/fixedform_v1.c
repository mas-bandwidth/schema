/* V1's half of the fixed form's versioning conformance (docs/SPEC-TABLES.md
   §3.4): the ENUM, UNION and KEYED cases, where a variant, an arm and a key
   were each inserted IN THE MIDDLE. This translation unit is the only one that
   names tblv1's types. */

#include <string.h>

#include "V1Table.h"
#include "fixedform.h"

int64_t fixed_v1_bytes( void ) { return cfg_fixed_measure( 1 ); }

int64_t fixed_v1_write( uint8_t * buffer, int64_t capacity )
{
    Cfg one;
    cfg_reset( &one );
    one.a = 42;
    strcpy( one.name, "hello" );
    one.name_length = 5;
    one.grade = GRADE_GOLD;                 /* V2 inserts Silver BEFORE Gold */
    one.effect.type = EFFECT_TYPE_WARD;
    one.effect.as.ward.charge = 0.75f;      /* V2 inserts hex BEFORE ward */
    SCHEMA_TABLE_KEYED_AT( one.tokens, SLOT_ALPHA, SLOT_MAX ) = 21;
    SCHEMA_TABLE_KEYED_AT( one.tokens, SLOT_DELTA, SLOT_MAX ) = 24; /* V2 slides Beta and keeps Delta */
    one.tier_present = 1;
    one.tier = 77;
    return cfg_fixed_save( &one, 1, buffer, capacity );
}

/* AN ENUM ORDINAL PAST THE ENUM'S TOP VALUE is not a variant this generation
   cannot name — it is a number the enum cannot hold at all. It lands None and
   COUNTS as a clamp (docs/SPEC-TABLES.md §3.4). */
void fixed_v1_bounds( void )
{
    static uint8_t file[16384];
    static TableFixedEntry plan[8192];
    Cfg v, back;
    TableReport r;
    int64_t n;

    cfg_reset( &v );
    v.grade = (Grade) 9; /* V1's Grade tops out at Gold */
    n = cfg_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
    fixed_check( n == cfg_fixed_measure( 1 ), "C bounds: V1 save" );

    memset( &r, 0, sizeof( r ) );
    fixed_check( cfg_fixed_load( &back, 1, file, n, plan, 8192, &r ) == 1,
                 "C bounds: the enum record reads" );
    fixed_check( back.grade == GRADE_NONE, "C ORDINAL: an ordinal past the enum's top value lands None" );
    fixed_check( r.clamped == 1, "C ORDINAL: and counts as a clamp" );
}
