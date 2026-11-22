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

int conformance_write_refusal_integer( const char * text, int is_signed, size_t bytes, void * storage )
{
    const char * p = text;
    const int negative = *p == '-';
    const int bits = (int) bytes * 8;
    uint64_t magnitude = 0, most = 0;
    int past = 0;
    if ( *p == '-' || *p == '+' ) { p++; }
    if ( *p == 0 ) { past = 1; }
    for ( ; *p != 0; p++ )
    {
        const uint64_t digit = (uint64_t) ( *p - '0' );
        if ( *p < '0' || *p > '9' || magnitude > ( UINT64_MAX - digit ) / 10 ) { past = 1; break; }
        magnitude = magnitude * 10 + digit;
    }
    if ( bytes != 1 && bytes != 2 && bytes != 4 && bytes != 8 ) { past = 1; }
    else if ( !is_signed ) { most = bits == 64 ? UINT64_MAX : ( (uint64_t) 1 << bits ) - 1; if ( negative && magnitude != 0 ) { past = 1; } }
    else { most = ( (uint64_t) 1 << ( bits - 1 ) ) - ( negative ? 0 : 1 ); }
    if ( past || magnitude > most ) { _exit( kUnrepresentableExit ); }
    /* two's complement of the magnitude is the negative value at any width */
    if ( negative ) { magnitude = (uint64_t) 0 - magnitude; }
    if ( bytes == 1 ) { const uint8_t v = (uint8_t) magnitude; memcpy( storage, &v, 1 ); }
    else if ( bytes == 2 ) { const uint16_t v = (uint16_t) magnitude; memcpy( storage, &v, 2 ); }
    else if ( bytes == 4 ) { const uint32_t v = (uint32_t) magnitude; memcpy( storage, &v, 4 ); }
    else { memcpy( storage, &magnitude, 8 ); }
    return 1;
}

int32_t conformance_write_refusal_count( const char * text )
{
    int32_t n = 0;
    CONFORMANCE_WREF_SIGNED( n, text );
    return n;
}

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
        n = conformance_write_refusal_count( text );
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
        n = conformance_write_refusal_count( text );
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
    buffer = (uint8_t *) malloc( capacity );
    if ( buffer == NULL ) { return 2; }
    if ( surface == CONFORMANCE_WRITE_VARIABLE ) { wrote = codec->save( value, buffer, (int64_t) capacity ); }
    else if ( !write_units[which].write( surface, root, value, announcement, announcement_bytes, buffer, (int64_t) capacity, &wrote ) )
    {
        return kNoSetterExit;
    }
    return wrote < 0 ? kRefusedExit : kWroteExit;
}

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
        int quiet;
        memset( &report, 0, sizeof( report ) );
        if ( !codec->load( value, wire, bytes, &report ) ) { _exit( 2 ); }
        if ( !write_units[which].set( root, value, path, kind, text ) ) { _exit( kNoSetterExit ); }
        quiet = open( "/dev/null", O_WRONLY );
        if ( quiet >= 0 ) { dup2( quiet, 2 ); }
        _exit( write_refusal_write( surface, which, codec, root, path, value, announcement, announcement_bytes ) );
    }
    if ( waitpid( child, &status, 0 ) != child ) { return CONFORMANCE_BROKEN; }
    if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT ) { return CONFORMANCE_REFUSED; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kRefusedExit ) { return CONFORMANCE_REFUSED; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit ) { return CONFORMANCE_WROTE; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kNoSetterExit ) { return CONFORMANCE_NO_SETTER; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kUnrepresentableExit ) { return CONFORMANCE_UNREPRESENTABLE; }
    return CONFORMANCE_BROKEN;
}
#endif
