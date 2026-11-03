/* THE WRITE CONTRACT's case runner (docs/SPEC.md §5.1; FORMAT.md,
 * `write-refusal`). C refuses two ways and both answer `refused`: the
 * every-build refusal is a negative return from the save, and the range of an
 * integer is schema_assert, debug-only, which this driver keeps live (no
 * NDEBUG) and does not route. An assert aborts, so EVERY CASE RUNS IN A CHILD:
 * the child loads, sets, saves and exits with a code for wrote or
 * refused-by-return; the parent reads the status, and death by SIGABRT is the
 * assert's refusal. The child's stderr goes to /dev/null so the assert's text
 * stays out of the harness log. */

#define _POSIX_C_SOURCE 200809L

#include <fcntl.h>
#include <signal.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "driver.h"

enum
{
    kWroteExit = 10,
    kRefusedExit = 11,
    kNoSetterExit = 12
};

int conformance_write_refusal_case( const ConformanceCodec * codec, const uint8_t * wire, int64_t bytes,
                                    const char * root, const char * path, const char * kind, const char * text )
{
    pid_t child;
    int status = 0;
    fflush( stdout );
    fflush( stderr );
    child = fork();
    if ( child < 0 ) { return CONFORMANCE_BROKEN; }
    if ( child == 0 )
    {
        ConformanceReport report;
        void * value = codec->storage();
        size_t capacity = (size_t) 1 << 20;
        uint8_t * buffer;
        int quiet;
        int64_t wrote;
        memset( &report, 0, sizeof( report ) );
        if ( !codec->load( value, wire, bytes, &report ) ) { _exit( 2 ); }
        if ( !conformance_write_refusal_set_tabledemo( root, value, path, kind, text ) ) { _exit( kNoSetterExit ); }
        buffer = (uint8_t *) malloc( capacity );
        if ( buffer == NULL ) { _exit( 2 ); }
        quiet = open( "/dev/null", O_WRONLY );
        if ( quiet >= 0 ) { dup2( quiet, 2 ); }
        wrote = codec->save( value, buffer, (int64_t) capacity );
        _exit( wrote < 0 ? kRefusedExit : kWroteExit );
    }
    if ( waitpid( child, &status, 0 ) != child ) { return CONFORMANCE_BROKEN; }
    if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT ) { return CONFORMANCE_REFUSED; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kRefusedExit ) { return CONFORMANCE_REFUSED; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit ) { return CONFORMANCE_WROTE; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kNoSetterExit ) { return CONFORMANCE_NO_SETTER; }
    return CONFORMANCE_BROKEN;
}
