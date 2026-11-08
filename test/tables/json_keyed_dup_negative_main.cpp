// The NEGATIVE CONTROL for a keyed object's repeated key (docs/SPEC-TABLES.md
// §16.2, §4).
//
// A keyed object's keys ARE keys, so a variant named twice is a repeated key,
// and a strict read REFUSES the whole value. The old reading was last-wins
// plus a count; the strict one has no partial value to look at, so the only
// thing a test can see is the verdict, and that is what this control reads.
//
// It runs twice. Against a walker whose repeated-key refusal has been
// deliberately removed it must SEE the read accept (and last-wins, which is
// what the walker does once nothing stops it); against the honest walker it
// must see a refusal with the value at its defaults. So it has three exits:
// 0 when the read accepted the repeat (the sabotaged arm requires this), 2 when
// it refused cleanly (the honest arm requires this), and 1 for anything else.
// A binary that answered the same both ways would read nothing.

#include <cstdio>
#include <cstring>

#include "KeyedTable.h"

int main()
{
    tabledemo::KeyedConfig value;
    tabledemo::TableReport report;
    const char * text = "{ \"teams\": { \"Red\": { \"spawn_count\": 5 }, \"Red\": { \"spawn_count\": 9 } } }";
    if ( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) )
    {
        // the sabotage removes the refusal, not the re-establishment of the
        // slot, so an accepted read must be last-wins; anything else is a
        // different defect and proves nothing about the refusal
        if ( value.teams[tabledemo::Team::Red].spawn_count != 9 )
        {
            printf( "FAIL keyed duplicate negative control: the repeat was accepted but the value\n"
                    "      is not last-wins (spawn_count=%d), so this run met a different defect\n",
                    (int) value.teams[tabledemo::Team::Red].spawn_count );
            return 1;
        }
        printf( "keyed duplicate negative control: the repeated key was accepted — red, as required\n" );
        return 0;
    }

    // a refusal must leave the value at its defaults and name the reason; a
    // refusal that kept half a value would be the tolerant read come back
    tabledemo::KeyedConfig defaults;
    if ( value.teams[tabledemo::Team::Red].spawn_count != defaults.teams[tabledemo::Team::Red].spawn_count )
    {
        printf( "FAIL keyed duplicate negative control: the read refused but kept spawn_count=%d\n",
                (int) value.teams[tabledemo::Team::Red].spawn_count );
        return 1;
    }

    printf( "keyed duplicate negative control: the read refused the repeated key\n"
            "      (refuse_reason=%d) with the value at its defaults: the honest verdict\n",
            (int) report.refuse_reason );
    return 2;
}
