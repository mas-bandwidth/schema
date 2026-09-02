// The NEGATIVE CONTROL for a keyed object's duplicate counting
// (SPEC-TABLES.md §16.2).
//
// A keyed object's keys ARE keys, so a variant named twice is last-wins AND
// counted. Last-wins was already true — the slot is re-established before
// each placement — which is exactly why the missing count was invisible to
// every round-trip test: the VALUE was right and only the ledger was wrong.
// A test that can only see values could never have caught it.
//
// This binary is built against a walker whose duplicate increment has been
// deliberately removed, and it PASSES only when the count comes back wrong.

#include <cstdio>
#include <cstring>

#include "KeyedTable.h"

int main()
{
    tabledemo::KeyedConfig value;
    tabledemo::TableReport report;
    const char * text = "{ \"teams\": { \"Red\": { \"spawn_count\": 5 }, \"Red\": { \"spawn_count\": 9 } } }";
    if ( !tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) )
    {
        printf( "keyed duplicate negative control: the sabotaged walker would not read at all — red\n" );
        return 0;
    }

    // last-wins must STILL hold: the sabotage removes the ledger entry, not
    // the re-establishment, and a control that passed for the wrong reason
    // would prove nothing
    if ( value.teams[tabledemo::Team::Red].spawn_count != 9 )
    {
        printf( "keyed duplicate negative control: last-wins broke as well — red, but for the wrong reason\n" );
        return 0;
    }

    if ( report.duplicate == 0 )
    {
        printf( "keyed duplicate negative control: the repeat went uncounted — red, as required\n" );
        return 0;
    }

    printf( "FAIL keyed duplicate negative control: a walker with its duplicate increment\n"
            "      removed still reported duplicate=%d. The suite cannot see an uncounted\n"
            "      repeat inside a keyed object.\n", report.duplicate );
    return 1;
}
