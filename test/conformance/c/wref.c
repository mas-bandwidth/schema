/* THE WRITE CONTRACT's case runner (docs/SPEC.md §5.1; FORMAT.md,
 * `write-refusal` and the three surfaces beside it). C refuses two ways and
 * both answer `refused`: the every-build refusal is a negative or zero return
 * from the writer, and the debug-only refusal is schema_assert, which this
 * driver keeps live (no NDEBUG) and does not route. An assert aborts, so EVERY
 * CASE RUNS IN A CHILD: the child loads, sets, writes and exits with a code for
 * wrote or refused-by-return; the parent reads the status, and death by
 * SIGABRT is the assert's refusal. The child's stderr goes to /dev/null so the
 * assert's text stays out of the harness log. */

#define _POSIX_C_SOURCE 200809L

#include <errno.h>
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
    kNoSetterExit = 12,
    kUnrepresentableExit = 13
};

#ifndef CONFORMANCE_NO_WREF_CASE
/* the units the rows name, each with its setter and its writer */
static const struct
{
    const char * unit;
    int ( *set )( const char * root, void * value, const char * path, const char * kind, const char * text );
    int ( *write )( int surface, const char * root, const void * value, const uint8_t * announcement,
                    int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
} write_units[] = {
    { "tabledemo", conformance_write_refusal_set_tabledemo, conformance_write_refusal_write_tabledemo },
    { "backenddemo", conformance_write_refusal_set_backenddemo, conformance_write_refusal_write_backenddemo },
    { "scalars", conformance_write_refusal_set_scalars, conformance_write_refusal_write_scalars },
    { "cooktorture", conformance_write_refusal_set_cooktorture, conformance_write_refusal_write_cooktorture },
    { "tblvarevo3", conformance_write_refusal_set_tblvarevo3, conformance_write_refusal_write_tblvarevo3 },
    { "tblsurf", conformance_write_refusal_set_tblsurf, conformance_write_refusal_write_tblsurf },
    { "mapdemo", conformance_write_refusal_set_mapdemo, conformance_write_refusal_write_none },
    { "graphdemo", conformance_write_refusal_set_graphdemo, conformance_write_refusal_write_none },
};
#endif

/* The hex of a `bytes` or `units` value: two digits a byte, or four a unit. */
static int write_refusal_hex( const char * text, int digits, uint32_t * out, int32_t capacity, int32_t * count )
{
    size_t length = strlen( text ), i;
    int32_t n = 0;
    if ( length % (size_t) digits != 0 ) { return 0; }
    for ( i = 0; i < length; i += (size_t) digits )
    {
        char piece[5];
        char * end = NULL;
        if ( n >= capacity ) { return 0; }
        memcpy( piece, text + i, (size_t) digits );
        piece[digits] = 0;
        out[n++] = (uint32_t) strtoul( piece, &end, 16 );
        if ( end == NULL || *end != 0 ) { return 0; }
    }
    *count = n;
    return 1;
}

int conformance_write_refusal_text( char * storage, int32_t capacity, int32_t * length, const char * kind, const char * text )
{
    int32_t i, n;
    uint32_t bytes[64];
    if ( strcmp( kind, "length" ) == 0 )
    {
        /* past storage only the length moves, and the writer must refuse
           before it reads past the array */
        n = (int32_t) strtoll( text, NULL, 10 );
        for ( i = 0; i < n && i < capacity; i++ ) { storage[i] = 'a'; }
        *length = n;
        return 1;
    }
    if ( strcmp( kind, "bytes" ) == 0 )
    {
        if ( !write_refusal_hex( text, 2, bytes, capacity, &n ) ) { return 0; }
        for ( i = 0; i < n; i++ ) { storage[i] = (char) (uint8_t) bytes[i]; }
        *length = n;
        return 1;
    }
    return 0;
}

int conformance_write_refusal_bytes( uint8_t * storage, int32_t capacity, int32_t * length, const char * kind, const char * text )
{
    int32_t i, n;
    uint32_t bytes[64];
    if ( strcmp( kind, "length" ) == 0 )
    {
        n = (int32_t) strtoll( text, NULL, 10 );
        for ( i = 0; i < n && i < capacity; i++ ) { storage[i] = 0xAA; }
        *length = n;
        return 1;
    }
    if ( strcmp( kind, "bytes" ) == 0 )
    {
        if ( !write_refusal_hex( text, 2, bytes, capacity, &n ) ) { return 0; }
        for ( i = 0; i < n; i++ ) { storage[i] = (uint8_t) bytes[i]; }
        *length = n;
        return 1;
    }
    return 0;
}

int conformance_write_refusal_units( uint16_t * storage, int32_t capacity, int32_t * length, const char * kind, const char * text )
{
    int32_t i, n;
    uint32_t units[64];
    if ( strcmp( kind, "units" ) != 0 || !write_refusal_hex( text, 4, units, capacity, &n ) ) { return 0; }
    for ( i = 0; i < n; i++ ) { storage[i] = (uint16_t) units[i]; }
    *length = n;
    return 1;
}

int conformance_write_refusal_int( const char * text, long long min, long long max, long long * out )
{
    char * end = NULL;
    long long n;
    errno = 0;
    n = strtoll( text, &end, 10 );
    if ( end == NULL || end == text || *end != 0 || errno != 0 ) { return 0; }
    if ( n < min || n > max ) { return CONFORMANCE_SET_UNREPRESENTABLE; }
    *out = n;
    return 1;
}

int conformance_write_refusal_elems( const char * text, long long storage )
{
    char * end = NULL;
    const long long n = strtoll( text, &end, 10 );
    if ( end == NULL || end == text || *end != 0 ) { return 0; }
    return n == storage ? 1 : CONFORMANCE_SET_UNREPRESENTABLE;
}

int conformance_write_refusal_write_none( int surface, const char * root, const void * value, const uint8_t * announcement,
                                          int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    (void) surface; (void) root; (void) value; (void) announcement; (void) announcement_bytes; (void) buffer; (void) capacity; (void) wrote;
    return 0;
}

#ifndef CONFORMANCE_NO_WREF_CASE
/* The writer a surface names, over the one value the row set: the child's
   exit code. */
static int write_refusal_write( int surface, int which, const ConformanceCodec * codec, const char * root, const char * path,
                                const void * value, const uint8_t * announcement, int64_t announcement_bytes )
{
    size_t capacity = (size_t) 1 << 20;
    uint8_t * buffer;
    int64_t wrote = -1;
    (void) path;
    if ( surface == CONFORMANCE_WRITE_JSON )
    {
        int64_t size = codec->to_json( value, NULL, 0 );
        char * text;
        if ( size < 0 ) { return kRefusedExit; }
        text = (char *) malloc( (size_t) size + 1 );
        if ( text == NULL ) { return 2; }
        if ( codec->to_json( value, text, size + 1 ) != size ) { return kRefusedExit; }
        return kWroteExit;
    }
    if ( surface == CONFORMANCE_WRITE_COOK )
    {
        /* THE COOK WRITER (docs/SPEC-TABLES.md §7.6): cook_measure, then cook
           in little-endian order; `wrote` when both complete */
        const int64_t need = codec->cook_measure( value );
        uint8_t * file;
        if ( need < 0 ) { return kRefusedExit; }
        file = (uint8_t *) malloc( (size_t) ( need > 0 ? need : 1 ) );
        if ( file == NULL ) { return 2; }
        return codec->cook( value, file, (uint64_t) need, 0 ) ? kWroteExit : kRefusedExit;
    }
    buffer = (uint8_t *) malloc( capacity );
    if ( buffer == NULL ) { return 2; }
    if ( surface == CONFORMANCE_WRITE_VARIABLE ) { wrote = codec->save( value, buffer, (int64_t) capacity ); }
    else if ( !write_units[which].write( surface, root, value, announcement, announcement_bytes, buffer, (int64_t) capacity, &wrote ) )
    {
        return kNoSetterExit;
    }
    return wrote < 0 ? kRefusedExit : kWroteExit;
}

/* A writer that walks a cycle forever ends its case by this alarm, which the
   parent names and answers nothing for. */
static const unsigned kWrefChildSeconds = 30;

int conformance_write_refusal_case( int surface, const ConformanceCodec * codec, const char * unit, const uint8_t * wire, int64_t bytes,
                                    const uint8_t * announcement, int64_t announcement_bytes,
                                    const char * root, const char * path, const char * kind, const char * text )
{
    pid_t child;
    int status = 0, which = -1, i;
    for ( i = 0; i < (int) ( sizeof( write_units ) / sizeof( write_units[0] ) ); i++ )
    {
        if ( strcmp( write_units[i].unit, unit ) == 0 ) { which = i; }
    }
    if ( which < 0 ) { return CONFORMANCE_NO_SETTER; }
    fflush( stdout );
    fflush( stderr );
    child = fork();
    if ( child < 0 ) { return CONFORMANCE_BROKEN; }
    if ( child == 0 )
    {
        ConformanceReport report;
        void * value = codec->storage();
        int quiet, set;
        alarm( kWrefChildSeconds );
        memset( &report, 0, sizeof( report ) );
        /* a NULL wire is the row's `-`: the root's FRESH value (a fixed table
           past the 65536-byte ceiling has no file form to load) */
        if ( wire != NULL && !codec->load( value, wire, bytes, &report ) ) { _exit( 2 ); }
        set = write_units[which].set( root, value, path, kind, text );
        if ( set == CONFORMANCE_SET_UNREPRESENTABLE ) { _exit( kUnrepresentableExit ); }
        if ( set != 1 ) { _exit( kNoSetterExit ); }
        quiet = open( "/dev/null", O_WRONLY );
        if ( quiet >= 0 ) { dup2( quiet, 2 ); }
        _exit( write_refusal_write( surface, which, codec, root, path, value, announcement, announcement_bytes ) );
    }
    if ( waitpid( child, &status, 0 ) != child ) { return CONFORMANCE_BROKEN; }
    if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT ) { return CONFORMANCE_REFUSED; }
    if ( WIFSIGNALED( status ) )
    {
        fprintf( stderr, "driver: the writer's child died by signal %d%s\n", WTERMSIG( status ),
                 WTERMSIG( status ) == SIGALRM ? " (it ran past the alarm)" : "" );
        return CONFORMANCE_DIED;
    }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kRefusedExit ) { return CONFORMANCE_REFUSED; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit ) { return CONFORMANCE_WROTE; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kNoSetterExit ) { return CONFORMANCE_NO_SETTER; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kUnrepresentableExit ) { return CONFORMANCE_UNREPRESENTABLE; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == 2 ) { return CONFORMANCE_ROW_WIRE; }
    return CONFORMANCE_BROKEN;
}
#endif
