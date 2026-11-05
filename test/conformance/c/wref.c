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
    kNoSetterExit = 12
};

/* the units the rows name, each with its setter and its writer */
static const struct
{
    const char * unit;
    int ( *set )( const char * root, void * value, const char * path, const char * kind, const char * text );
    int ( *write )( int surface, const char * root, const void * value, const uint8_t * announcement,
                    int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote );
} write_units[] = {
    { "tabledemo", conformance_write_refusal_set_tabledemo, conformance_write_refusal_write_tabledemo },
    { "widedemo", conformance_write_refusal_set_widedemo, conformance_write_refusal_write_widedemo },
    { "backenddemo", conformance_write_refusal_set_backenddemo, conformance_write_refusal_write_backenddemo },
};

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

int conformance_write_refusal_units( uint16_t * storage, int32_t capacity, int32_t * length, const char * kind, const char * text )
{
    int32_t i, n;
    uint32_t units[64];
    if ( strcmp( kind, "units" ) != 0 || !write_refusal_hex( text, 4, units, capacity, &n ) ) { return 0; }
    for ( i = 0; i < n; i++ ) { storage[i] = (uint16_t) units[i]; }
    *length = n;
    return 1;
}

/* `wrote` on the text surface is the text written AND the field's JSON string
   spelling the zero as \u0000 (FORMAT.md, `write-refusal-json`): the key is the
   path's last name, and its string is read to its closing quote. */
static int write_refusal_json_holds_zero( const char * json, const char * path )
{
    const char * name = strrchr( path, '.' );
    char key[128];
    const char * at;
    name = name != NULL ? name + 1 : path;
    if ( strlen( name ) + 3 > sizeof( key ) ) { return 0; }
    snprintf( key, sizeof( key ), "\"%s\"", name );
    at = strstr( json, key );
    if ( at == NULL ) { return 0; }
    at = strchr( at + strlen( key ), '"' );
    if ( at == NULL ) { return 0; }
    for ( at = at + 1; *at != 0 && *at != '"'; at++ )
    {
        if ( *at != '\\' ) { continue; }
        if ( strncmp( at, "\\u0000", 6 ) == 0 ) { return 1; }
        if ( at[1] != 0 ) { at++; }
    }
    return 0;
}

/* The writer a surface names, over the one value the row set: the child's
   exit code. */
static int write_refusal_write( int surface, int which, const ConformanceCodec * codec, const char * root, const char * path,
                                const void * value, const uint8_t * announcement, int64_t announcement_bytes )
{
    size_t capacity = (size_t) 1 << 20;
    uint8_t * buffer;
    int64_t wrote = -1;
    if ( surface == CONFORMANCE_WRITE_JSON )
    {
        int64_t size = codec->to_json( value, NULL, 0 );
        char * text;
        if ( size < 0 ) { return kRefusedExit; }
        text = (char *) malloc( (size_t) size + 1 );
        if ( text == NULL ) { return 2; }
        if ( codec->to_json( value, text, size + 1 ) != size ) { return kRefusedExit; }
        text[size] = 0;
        return write_refusal_json_holds_zero( text, path ) ? kWroteExit : kRefusedExit;
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
    return CONFORMANCE_BROKEN;
}
