/* FU1's half of TEXT UNDER AN ARM (docs/SPEC-TABLES.md §3.4, §15). A
   `string(8)` sits in the union's SECOND arm, with a scalar either side so a
   mislaid length or a mislaid guard moves a neighbour. This translation unit
   is the ONLY one that names tblfu1's types; see fixedform.h for why there is
   more than one.

   THE IDENTITY SAVE is this side's job: two records, labelled then plain, so
   the arm that is right by accident for flavour 1 sits beside the arm that is
   not. The compiled read of the same bytes is FU2's. */

#include <string.h>

#include "FU1Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

static void fill_labelled( FuRoot * value )
{
    fu_root_reset( value );
    value->flag = 1;
    value->note = 44;
    value->note_present = 1;
    value->pick.type = PICK_TYPE_LABELLED; /* the SECOND arm */
    value->pick.as.labelled.lead = 101;
    strcpy( value->pick.as.labelled.label, "hello" );
    value->pick.as.labelled.label_length = 5;
    value->pick.as.labelled.trail = 202;
    value->tail = 11;
}

static void fill_plain( FuRoot * value )
{
    fu_root_reset( value );
    value->pick.type = PICK_TYPE_PLAIN;
    value->pick.as.plain.n = 303;
    value->tail = 12;
}

int64_t fixed_fu1_bytes( void ) { return fu_root_fixed_measure( 2 ); }

int64_t fixed_fu1_write( uint8_t * buffer, int64_t capacity )
{
    FuRoot v[2];
    fill_labelled( &v[0] );
    fill_plain( &v[1] );
    return fu_root_fixed_save( v, 2, buffer, capacity );
}

void fixed_fu1_read_own( const uint8_t * data, int64_t bytes )
{
    FuRoot back[2];
    TableReport r;

    memset( &r, 0, sizeof( r ) );
    fixed_check( fu_root_fixed_load( back, 2, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 2,
                 "C text under an arm: the identity read takes both records" );
    fixed_check( back[0].pick.type == PICK_TYPE_LABELLED, "C text under an arm: identity, the SECOND arm" );
    fixed_check( back[0].pick.as.labelled.lead == 101,
                 "C text under an arm: identity, the scalar BEFORE the text" );
    fixed_check( back[0].pick.as.labelled.label_length == 5 &&
                 strcmp( back[0].pick.as.labelled.label, "hello" ) == 0,
                 "C text under an arm: the IDENTITY read lands the text" );
    fixed_check( back[0].pick.as.labelled.trail == 202,
                 "C text under an arm: identity, the scalar AFTER the text" );
    fixed_check( back[0].flag == 1 && back[0].note_present == 1 && back[0].note == 44 && back[0].tail == 11,
                 "C text under an arm: identity, the rest of the labelled record" );
    fixed_check( back[1].pick.type == PICK_TYPE_PLAIN && back[1].pick.as.plain.n == 303 && back[1].tail == 12,
                 "C text under an arm: identity, the FIRST arm as well" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused,
                 "C text under an arm: identity, a clean read moves no counter" );
}
