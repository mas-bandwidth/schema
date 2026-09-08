/* THE OTHER END OF THE SAME REFUSAL, HELD IN THE CONFIGURATION THAT WOULD DROP
 * IT — the C leg's twin of test/tables/keyed_max_ndebug_main.cpp
 * (docs/SPEC-TABLES.md §2.4).
 *
 * An enum-keyed array holds one slot per NAMED variant, so a key past Max names
 * a variant this enum does not have: the same program error as None, refused
 * the same way in EVERY build. A build that let it through would read past the
 * end of the storage.
 *
 * This translation unit is compiled -DNDEBUG, which is exactly the
 * configuration a game ships and exactly the one that removes an assert. The
 * child must still die. Its Makefile gate requires that.
 *
 * C's accessor is a macro over table_keyed_slot rather than an operator[]; the
 * refusal inside it is the same unsigned compare plus the same abort.
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
        FILE * quiet = freopen( "/dev/null", "w", stderr );
        KeyedConfig cfg;
        Team key = (Team) ( (int32_t) TEAM_MAX + 1 );
        (void) quiet;
        keyed_config_reset( &cfg );
        SCHEMA_TABLE_KEYED_AT( cfg.teams, key, TEAM_MAX ).spawn_count = 1; /* never reached */
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
        printf( "FAILED: under -DNDEBUG a key past Max did NOT end the program — "
                "the refusal was compiled out, and a shipped build would read "
                "past the end of the storage (docs/SPEC-TABLES.md §2.4)\n" );
        return 1;
    }
    printf( "keyed past-Max refusal under -DNDEBUG, C: the index ended the program (signal %d)\n",
            WTERMSIG( status ) );
    return 0;
#endif
}
