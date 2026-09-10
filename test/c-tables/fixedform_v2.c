/* V2's half of the fixed form's versioning conformance (docs/SPEC-TABLES.md
   §3.4). The only translation unit that names tblv2's types.

   EVERY ANSWER BELOW IS BY NAME. Under ordinals the stored Gold would read
   back as Silver, the stored ward as hex, and the stored Beta as Omega — an
   enum variant, a union arm and an enum key each moved in the middle, and the
   plan compiled from V1's layout is what puts every one of them back. */

#include <string.h>

#include "V2Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

void fixed_v2_read_v1( const uint8_t * data, int64_t bytes )
{
    Cfg back;
    TableReport r;
    int64_t n;
    memset( &r, 0, sizeof( r ) );
    n = cfg_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r );
    fixed_check( n == 1, "V2 reads V1: one record" );
    fixed_check( back.grade == GRADE_GOLD, "ENUM: a variant inserted in the middle is remapped by NAME" );
    fixed_check( back.effect.type == EFFECT_TYPE_WARD, "UNION: an arm inserted in the middle is remapped by NAME" );
    fixed_check( back.effect.as.ward.charge == 0.75f, "UNION: the arm's payload lands" );
    fixed_check( strcmp( back.title, "hello" ) == 0 && back.title_length == 5, "RENAMED: title reads name's bytes" );
    fixed_check( SCHEMA_TABLE_KEYED_AT( back.tokens, SLOT_ALPHA, SLOT_MAX ) == 21, "KEYED: a slot whose key did not move" );
    fixed_check( SCHEMA_TABLE_KEYED_AT( back.tokens, SLOT_DELTA, SLOT_MAX ) == 24, "KEYED: a slot whose key SLID keeps its value" );
    fixed_check( SCHEMA_TABLE_KEYED_AT( back.tokens, SLOT_SIGMA, SLOT_MAX ) == 0, "KEYED: a key the writer has no name for takes its default" );
    fixed_check( back.tier_present && back.tier == 77, "OPTIONAL: the present flag and the payload" );
    fixed_check( back.c, "MISSING: V2's own `c` takes its declared default" );
    fixed_check( back.a == 5.0f, "KIND MOVED: int32 -> float32 leaves the declared default" );
    fixed_check( !r.malformed && !r.refused, "V2 reads V1: no damage and no refusal" );
}
