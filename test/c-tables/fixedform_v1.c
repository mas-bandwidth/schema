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
    fixed_check( cfg_fixed_load( &back, 1, file, n, plan, 8192, NULL, &r ) == 1,
                 "C bounds: the enum record reads" );
    fixed_check( back.grade == GRADE_NONE, "C ORDINAL: an ordinal past the enum's top value lands None" );
    fixed_check( r.clamped == 1, "C ORDINAL: and counts as a clamp" );
}

/* AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS (docs/SPEC-TABLES.md
   §3.4): the payload rides WHOLE whether or not it is present, and when the
   flag is 0 what rides is ZERO. One `if` in the writer, and what it buys is
   that a caller's untouched payload storage never reaches the wire — an absent
   optional is a hole in the record and not a window into the writer's memory.

   THE CONTROL IS THE STAIN, as it is for every other kind of slack: the payload
   storage is filled with a byte a clean record carries nowhere, the test proves
   the stain IS there, then that the WIRE carries none of it, and then that the
   SAME payload PRESENT does put those bytes on the wire — so the check is
   discriminating and not passing because the writer never wrote a payload at
   all. */
void fixed_v1_absent_optional( void )
{
    static uint8_t file[16384];
    Cfg v;
    const uint8_t * body;
    size_t body_bytes;
    int64_t n;

    cfg_reset( &v );
    memset( &v.extra, 0xA7, sizeof( v.extra ) );
    v.extra_present = 0;
    fixed_check( ( (const uint8_t *) &v.extra )[0] == 0xA7u,
                 "C CONTROL: the absent payload really is stained in storage" );

    n = cfg_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
    fixed_check( n == cfg_fixed_measure( 1 ), "C absent optional: the record saves" );
    body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( cfg_fixed_layout ) + 8;
    body_bytes = (size_t) cfg_fixed_body_bytes;
    fixed_check( memchr( body, 0xA7, body_bytes ) == NULL,
                 "C ABSENT OPTIONAL: not one byte of the absent payload reached the wire" );

    /* THE DISCRIMINATING HALF: the same payload, PRESENT. */
    v.extra_present = 1;
    n = cfg_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
    fixed_check( n == cfg_fixed_measure( 1 ), "C absent optional: the present twin saves" );
    fixed_check( memchr( body, 0xA7, body_bytes ) != NULL,
                 "C NEGATIVE CONTROL: the SAME payload PRESENT really does reach the wire" );
}
