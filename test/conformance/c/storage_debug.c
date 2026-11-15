/* The storage check in a debug build (docs/SPEC.md §5.1): a string(8) whose
   used length is 9 indexes past the storage it describes, and the writer's
   schema_assert stops it before the first byte. C is an assert leg: the check
   is debug-only, NDEBUG removes it, and a release build carries no write-side
   check at all. This translation unit is compiled WITHOUT NDEBUG. An assert
   aborts, so each writer runs in a child and death by SIGABRT is the refusal. */

#define _POSIX_C_SOURCE 200809L

#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#include "StorageTable.h"

#ifdef NDEBUG
#error "the storage debug gate must be compiled without NDEBUG"
#endif

static int write_once( int save )
{
    storagerelease_root_t value;
    uint8_t buffer[4096];
    storagerelease_table_report_t report;

    memset( &value, 0, sizeof value );
    value.wide_length = 9; /* the declared bound is 8 */
    memset( &report, 0, sizeof report );
    if ( save )
    {
        return (int) storagerelease_root_save_messages( &value, 1, buffer, (int64_t) sizeof buffer, &report );
    }
    return (int) storagerelease_root_measure_messages( &value, 1, &report );
}

/* 1 when the writer died on the assert, 0 when it returned */
static int asserted( int save )
{
    int status = 0;
    pid_t child = fork();
    if ( child < 0 )
    {
        printf( "FAIL storage debug: fork failed\n" );
        return 0;
    }
    if ( child == 0 )
    {
        int null = open( "/dev/null", O_WRONLY );
        if ( null >= 0 )
        {
            dup2( null, 2 );
        }
        (void) write_once( save );
        _exit( 0 );
    }
    if ( waitpid( child, &status, 0 ) != child )
    {
        return 0;
    }
    return WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT;
}

int main( void )
{
    if ( !asserted( 0 ) )
    {
        printf( "FAIL storage debug: MeasureMessages measured a string past its storage\n" );
        return 1;
    }
    if ( !asserted( 1 ) )
    {
        printf( "FAIL storage debug: SaveMessages wrote a string past its storage\n" );
        return 1;
    }
    printf( "storage debug gate: a string past its storage hits the assert in a debug build\n" );
    return 0;
}
