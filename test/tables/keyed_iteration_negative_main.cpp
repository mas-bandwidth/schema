// The NEGATIVE CONTROL for a keyed array's ITERATION RANGE
// (SPEC-TABLES.md §2.4).
//
// The whole promise of the iteration surface is the RANGE: 1..E.Max, so slot
// 0 — None's, and never valid — cannot reach a call site. A suite that only
// checks the elements it visits would pass just as happily over a range that
// started at 0, because slot 0 holds the same declared defaults every other
// untouched slot does. So the guarantee is worth nothing until the test that
// asserts it is shown capable of going red.
//
// This binary is built against a generated header whose begin() has been
// deliberately moved to slot 0, and it PASSES only when the iteration hands
// out None's slot.

#include <cstdio>

#include "KeyedTable.h"

int main()
{
    tabledemo::KeyedConfig cfg;

    // the corpus test's own sweep, in one line: is slot 0 in the range?
    bool saw_slot_zero = false;
    int32_t seen = 0;
    for ( auto [ team, config ] : cfg.teams )
    {
        (void) config;
        if ( (int32_t) team == 0 )
        {
            saw_slot_zero = true;
        }
        seen++;
    }

    if ( saw_slot_zero )
    {
        printf( "keyed iteration negative control: slot 0 was handed out — red, as required\n" );
        return 0;
    }

    printf( "FAIL keyed iteration negative control: begin() was moved to slot 0 and the\n"
            "      walk still visited %d slots without None's among them. The suite cannot\n"
            "      see an iteration range that reaches slot 0.\n", seen );
    return 1;
}
