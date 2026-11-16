// The storage check in a debug build (docs/SPEC.md §5.1): an [..8] count of 9
// and a string(8) length of 9 each index past the storage they describe, and
// the writer's schema_assert stops them before the first byte. C++ is an
// assert leg: the check is debug-only, NDEBUG removes it, and a release build
// carries no write-side check at all. This translation unit is compiled
// WITHOUT NDEBUG. An assert aborts, so each writer runs in a child and death
// by SIGABRT is the refusal.

#include <csignal>
#include <cstdint>
#include <cstdio>

#include <fcntl.h>
#include <sys/wait.h>
#include <unistd.h>

#include "StorageTable.h"

#ifdef NDEBUG
#error "the storage debug gate must be compiled without NDEBUG"
#endif

static void write_once( bool count, bool save )
{
    static storagerelease::Root value{};
    static uint8_t buffer[4096];
    storagerelease::TableReport report{};

    if ( count )
    {
        value.items_count = 9; // the declared bound is 8
    }
    else
    {
        value.wide_length = 9; // the declared bound is 8
    }
    if ( save )
    {
        (void) storagerelease::RootSaveMessages( &value, 1, buffer, (int64_t) sizeof buffer, &report );
    }
    else
    {
        (void) storagerelease::RootMeasureMessages( &value, 1, &report );
    }
}

// true when the writer died on the assert, false when it returned
static bool asserted( bool count, bool save )
{
    std::fflush( stdout );
    int status = 0;
    pid_t child = fork();
    if ( child < 0 )
    {
        std::printf( "FAIL storage debug: fork failed\n" );
        return false;
    }
    if ( child == 0 )
    {
        int null = open( "/dev/null", O_WRONLY );
        if ( null >= 0 )
        {
            dup2( null, 2 );
        }
        write_once( count, save );
        _exit( 0 );
    }
    if ( waitpid( child, &status, 0 ) != child )
    {
        return false;
    }
    return WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT;
}

int main()
{
    if ( !asserted( true, false ) )
    {
        std::printf( "FAIL storage debug: RootMeasureMessages measured a count past its storage\n" );
        return 1;
    }
    if ( !asserted( true, true ) )
    {
        std::printf( "FAIL storage debug: RootSaveMessages wrote a count past its storage\n" );
        return 1;
    }
    if ( !asserted( false, false ) )
    {
        std::printf( "FAIL storage debug: RootMeasureMessages measured a string past its storage\n" );
        return 1;
    }
    if ( !asserted( false, true ) )
    {
        std::printf( "FAIL storage debug: RootSaveMessages wrote a string past its storage\n" );
        return 1;
    }
    std::printf( "storage debug gate: a count and a string past their storage hit the assert in a debug build\n" );
    return 0;
}
