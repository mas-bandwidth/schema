/* W16: AN ARM INSIDE AN ARM ANSWERS TO THE OUTER TAG (docs/FIXED-FORM-ALGORITHM.md
   §4.1, §5.8 row 6). A union's guard is stamped over EVERYTHING its arm produced,
   and a nested union's own arm selection must SURVIVE that stamp — so the two tags
   are CONJOINED: an inner arm's entries answer to the inner tag on the guard lane
   and to the OUTER tag on the guard2 lane, and they run only when BOTH hold their
   ordinal. Neither half stands alone.

   THE C++ REFERENCE IS test/tables/fixedform_main.cpp:nested_union_case, whose
   fixture is test/tables/FG1.schema. This translation unit is the ONLY one that
   names tblfg1's types; see fixedform.h for why there is more than one. */

#include <string.h>

#include "FG1Table.h"
#include "fixedform.h"

void fixed_fg1_nested_union( void )
{
    /* 1. THE PLAN SAYS IT. Every guarded entry answers to the OUTER tag — on the
       first lane when it is the outer union's own, on the SECOND when it belongs
       to an arm inside an arm — and not one answers to the inner tag alone. */
    uint32_t outer_tag_at = 0xFFFFFFFFu;
    uint32_t inner_tag_dst = 0xFFFFFFFFu;
    uint32_t other_arm_dst = 0xFFFFFFFFu;
    uint8_t inner_arm_arg = 0;
    int saw_inner_tag = 0;
    int32_t i;

    for ( i = 0; i < fh_root_fixed_plan_count; ++i )
    {
        const TableFixedEntry e = fh_root_fixed_plan[i];
        uint32_t outer;
        if ( e.guard == SCHEMA_TABLE_FIXED_NO_GUARD ) { continue; }
        outer = ( e.guard2 == SCHEMA_TABLE_FIXED_NO_GUARD ) ? e.guard : e.guard2;
        if ( outer_tag_at == 0xFFFFFFFFu ) { outer_tag_at = outer; }
        fixed_check( outer == outer_tag_at, "W16: every guarded entry of a nested union answers to ONE OUTER tag" );
    }
    fixed_check( outer_tag_at != 0xFFFFFFFFu, "W16: the plan really has guarded entries" );

    for ( i = 0; i < fh_root_fixed_plan_count; ++i )
    {
        const TableFixedEntry e = fh_root_fixed_plan[i];
        if ( e.guard == SCHEMA_TABLE_FIXED_NO_GUARD ) { continue; }
        if ( e.src == outer_tag_at + 1u && e.size == 1u )
        {
            saw_inner_tag = 1;
            inner_tag_dst = e.dst;
            inner_arm_arg = (uint8_t) e.arg;
        }
        fixed_check( e.guard == outer_tag_at || e.guard2 == outer_tag_at,
                     "W16: an inner entry answers to the inner tag AND the outer one, never the inner alone" );
    }
    fixed_check( saw_inner_tag, "W16: the inner union's own tag byte is one of the entries the OUTER tag guards" );

    /* AND THE TWO ARMS SHARE STORAGE, which is what makes the record half below
       an assertion and not a hope: the OTHER arm's entry lands at the same
       destination the inner tag does. An inner entry that ran under the wrong tag
       would be READABLE IN THE OTHER ARM'S OWN FIELD. */
    for ( i = 0; i < fh_root_fixed_plan_count; ++i )
    {
        const TableFixedEntry e = fh_root_fixed_plan[i];
        if ( e.guard != outer_tag_at || e.guard2 != SCHEMA_TABLE_FIXED_NO_GUARD || e.arg == inner_arm_arg ) { continue; }
        other_arm_dst = e.dst;
    }
    fixed_check( other_arm_dst != 0xFFFFFFFFu && other_arm_dst == inner_tag_dst,
                 "W16: the OTHER arm's field and the inner tag land at ONE destination — so that field is where a stray inner entry would show" );

    /* 2. AND THE RECORD SAYS IT. The outer tag selects B, so not one inner entry
       may run — and the measurement of that is THE B ARM'S OWN FIELD, which
       shares its destination with the inner tag. */
    {
        static uint8_t file[4096];
        static TableFixedEntry plan[1024];
        FhRoot one, back;
        TableReport r;
        const uint8_t * stain_at;
        uint32_t stain_after = 0;
        int64_t n;

        fh_root_reset( &one );
        one.tier = TIER_SILVER;
        one.head = 77;
        one.pick.type = OUTER_TYPE_B;
        one.pick.as.b.m = 555;
        fixed_check( fh_root_fixed_measure( 1 ) <= (int64_t) sizeof( file ), "W16: the B record fits the buffer" );
        n = fh_root_fixed_save( &one, 1, file, (int64_t) sizeof( file ) );
        fixed_check( n == fh_root_fixed_measure( 1 ), "W16: the B record saves" );

        fh_root_reset( &back );
        back.pick.type = OUTER_TYPE_A;
        back.pick.as.a.inner.type = INNER_TYPE_OTHER;
        back.pick.as.a.inner.as.other.k = 0x5A5A5A5A; /* the stain, so nothing below is vacuous */
        fixed_check( back.pick.as.a.inner.as.other.k == 0x5A5A5A5A, "CONTROL: the inner arm's storage really is stained" );
        stain_at = (const uint8_t *) (const void *) &back.pick.as.a.inner.as.other.k;
        memset( &r, 0, sizeof( r ) );
        fixed_check( fh_root_fixed_load( &back, 1, file, n, plan, 1024, NULL, &r ) == 1, "W16: the B record reads" );
        fixed_check( back.pick.type == OUTER_TYPE_B, "W16: the outer tag" );
        fixed_check( back.pick.as.b.m == 555,
                     "W16: THE B ARM'S OWN FIELD IS WHOLE — no inner entry's bytes landed in the storage it shares with the inner tag" );
        fixed_check( back.tier == TIER_SILVER && back.head == 77, "W16: the rest of the record" );
        memcpy( &stain_after, stain_at, sizeof( stain_after ) );
        fixed_check( stain_after == 0x5A5A5A5Au,
                     "W16: and the storage no entry of this read owns is UNTOUCHED — the identity plan prefills nothing (§4.3)" );
        fixed_check( r.clamped == 0 && !r.malformed && !r.refused, "W16: a clean read moves no counter" );
    }

    /* 3. THE OTHER WAY: the outer tag selects A, and the inner union's arm lands
       whole — the stamp does not cost the inner selection. */
    {
        static uint8_t file[4096];
        static TableFixedEntry plan[1024];
        FhRoot one, back;
        TableReport r;
        int64_t n;

        fh_root_reset( &one );
        one.tier = TIER_BRONZE;
        one.head = 11;
        one.pick.type = OUTER_TYPE_A;
        one.pick.as.a.edge = 22;
        one.pick.as.a.inner.type = INNER_TYPE_OTHER;
        one.pick.as.a.inner.as.other.k = 33;
        n = fh_root_fixed_save( &one, 1, file, (int64_t) sizeof( file ) );
        fixed_check( n == fh_root_fixed_measure( 1 ), "W16: the A record saves" );

        fh_root_reset( &back );
        memset( &r, 0, sizeof( r ) );
        fixed_check( fh_root_fixed_load( &back, 1, file, n, plan, 1024, NULL, &r ) == 1, "W16: the A record reads" );
        fixed_check( back.pick.type == OUTER_TYPE_A, "W16: the outer arm" );
        fixed_check( back.pick.as.a.inner.type == INNER_TYPE_OTHER && back.pick.as.a.inner.as.other.k == 33,
                     "W16: THE INNER UNION'S OWN ARM SELECTION SURVIVES THE OUTER STAMP" );
        fixed_check( back.pick.as.a.edge == 22, "W16: and the outer arm's other field" );
        fixed_check( r.clamped == 0 && !r.malformed && !r.refused, "W16: a clean read moves no counter" );
    }
}
