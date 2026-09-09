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
