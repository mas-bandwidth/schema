/* THE REFUSAL, HELD IN THE CONFIGURATION THAT WOULD DROP IT — the C leg's twin
 * of test/tables/keyed_none_ndebug_main.cpp (docs/SPEC-TABLES.md §2.4).
 *
 * Indexing an enum-keyed array by None is a program error in EVERY build: the
 * storage shifts left and holds no slot for None, so a build that let the index
 * through would read one element BEFORE the array.
 *
 * This translation unit is compiled -DNDEBUG, which is exactly the
 * configuration a game ships and exactly the one that removes an assert. The
 * child must still die. Its Makefile gate requires that.
 *
 * C's accessor is a macro over table_keyed_slot rather than an operator[], which
 * is the ONE spelling that differs from the reference; the refusal inside it is
 * the same assert plus the same abort, and this gate is what says so.
 */

#include <stdio.h>
#include <sys/wait.h>
#include <unistd.h>

#include "KeyedTable.h"

int main( void )
{
#ifndef NDEBUG
    printf( "FAILED: this gate is meaningless without -DNDEBUG\n" );
    return 1;
#else
    pid_t child;
    int status = 0;
    fflush( stdout );
    child = fork();
    if ( child == 0 )
    {
        /* the abort message is the point of the child, not of this log */
        FILE * quiet = freopen( "/dev/null", "w", stderr );
        KeyedConfig cfg;
        /* a None key is what a defaulted enum field holds, which is how one
           reaches an accessor in a real program */
        Team key = TEAM_NONE;
        (void) quiet;
        keyed_config_reset( &cfg );
        SCHEMA_TABLE_KEYED_AT( cfg.teams, key ).spawn_count = 1; /* never reached */
        _exit( 0 );
    }
    if ( child < 0 )
    {
        printf( "FAILED: fork\n" );
        return 1;
    }
    if ( waitpid( child, &status, 0 ) != child )
    {
        printf( "FAILED: waitpid\n" );
        return 1;
    }
    if ( !WIFSIGNALED( status ) )
    {
        printf( "FAILED: under -DNDEBUG a None index did NOT end the program — "
                "the refusal was compiled out, and a shipped build would read "
                "one element before the array (docs/SPEC-TABLES.md §2.4)\n" );
        return 1;
    }
    printf( "keyed None refusal under -DNDEBUG, C: the index ended the program (signal %d)\n",
            WTERMSIG( status ) );
    return 0;
#endif
}
