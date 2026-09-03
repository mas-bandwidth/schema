// THE OTHER END OF THE SAME REFUSAL, HELD IN THE CONFIGURATION THAT WOULD DROP
// IT (docs/SPEC-TABLES.md §2.4). An enum-keyed array holds one slot per NAMED
// variant, so a key past Max names a variant this enum does not have: it is
// the same program error as None, and it is refused the same way in EVERY
// build. A build that let it through would read past the end of the storage.
//
// The suite's own fork test proves the refusal fires, but it is compiled with
// asserts LIVE — so it cannot tell an unconditional refusal from an assert.
// This translation unit is compiled -DNDEBUG, which is exactly the
// configuration a game ships and exactly the one that removes an assert. The
// child must still die. Its Makefile gate requires that.

#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#include "KeyedTable.h"

int main()
{
#ifndef NDEBUG
    printf( "FAILED: this gate is meaningless without -DNDEBUG\n" );
    return 1;
#else
    fflush( stdout );
    pid_t child = fork();
    if ( child == 0 )
    {
        // the abort message is the point of the child, not of this log
        FILE * quiet = freopen( "/dev/null", "w", stderr );
        (void) quiet;
        tabledemo::KeyedConfig cfg;
        // Max + 1: the key an older writer's extra variant arrives as, which is
        // how a key past the end reaches an accessor in a real program
        tabledemo::Team key = (tabledemo::Team) ( (int32_t) tabledemo::Team::Max + 1 );
        tabledemo::TeamConfig & past = cfg.teams[key];
        past.spawn_count = 1; // never reached: the accessor refused
        _exit( 0 );
    }
    if ( child < 0 )
    {
        printf( "FAILED: fork\n" );
        return 1;
    }
    int status = 0;
    if ( waitpid( child, &status, 0 ) != child )
    {
        printf( "FAILED: waitpid\n" );
        return 1;
    }
    if ( !WIFSIGNALED( status ) )
    {
        printf( "FAILED: under -DNDEBUG a key past Max did NOT end the program — "
                "the refusal was compiled out, and a shipped build would read "
                "past the end of the storage (docs/SPEC-TABLES.md §2.4)\n" );
        return 1;
    }
    printf( "keyed past-Max refusal under -DNDEBUG: the index ended the program (signal %d)\n",
            WTERMSIG( status ) );
    return 0;
#endif
}
