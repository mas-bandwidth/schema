// THE REFUSAL, HELD IN THE CONFIGURATION THAT WOULD DROP IT (SPEC-TABLES.md
// §2.4). Indexing an enum-keyed array by None is a program error in EVERY
// build: the storage shifts left and holds no slot for None, so a build that
// let the index through would read one element BEFORE the array.
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
        // a None key is what a defaulted enum field holds, which is how one
        // reaches an accessor in a real program
        tabledemo::Team key = tabledemo::Team::None;
        tabledemo::TeamConfig & none = cfg.teams[key];
        none.spawn_count = 1; // never reached: the accessor refused
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
        printf( "FAILED: under -DNDEBUG a None index did NOT end the program — "
                "the refusal was compiled out, and a shipped build would read "
                "one element before the array (SPEC-TABLES.md §2.4)\n" );
        return 1;
    }
    printf( "keyed None refusal under -DNDEBUG: the index ended the program (signal %d)\n",
            WTERMSIG( status ) );
    return 0;
#endif
}
