/* FU2's half of TEXT UNDER AN ARM (docs/SPEC-TABLES.md §3.4, §15). FU2 appends
   `extra` and nothing else moves, so a read of FU1's bytes is a COMPILED plan
   rather than the identity one — and the compiled plan is the path where an
   overloaded plan lane dropped a union arm's text (reference-fix 12). This
   translation unit is the ONLY one that names tblfu2's types. */

#include <string.h>

#include "FU2Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

void fixed_fu2_read_fu1( const uint8_t * data, int64_t bytes )
{
    FuRoot back[2];
    TableReport r;

    memset( &r, 0, sizeof( r ) );
    fixed_check( fu_root_fixed_load( back, 2, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 2,
                 "C text under an arm: the compiled read takes both records" );
    fixed_check( back[0].pick.type == PICK_TYPE_LABELLED, "C text under an arm: compiled, the SECOND arm" );
    fixed_check( back[0].pick.as.labelled.lead == 101,
                 "C text under an arm: compiled, the scalar BEFORE the text" );
    fixed_check( back[0].pick.as.labelled.label_length == 5 &&
                 strcmp( back[0].pick.as.labelled.label, "hello" ) == 0,
                 "C text under an arm: the COMPILED read still sees the text" );
    fixed_check( back[0].pick.as.labelled.trail == 202,
                 "C text under an arm: compiled, the scalar AFTER the text" );
    fixed_check( back[0].flag == 1 && back[0].note_present == 1 && back[0].note == 44 && back[0].tail == 11,
                 "C text under an arm: compiled, the rest of the labelled record" );
    fixed_check( back[0].extra == 11,
                 "C text under an arm: the field FU1 does not carry took its declared default" );
    fixed_check( back[1].pick.type == PICK_TYPE_PLAIN && back[1].pick.as.plain.n == 303 && back[1].tail == 12,
                 "C text under an arm: compiled, the FIRST arm as well" );
    fixed_check( back[1].extra == 11,
                 "C text under an arm: compiled, `extra` defaults on the FIRST arm too" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused,
                 "C text under an arm: compiled, a clean read moves no counter" );
}
