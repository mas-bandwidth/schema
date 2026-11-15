/* C packet Write text contract: each case runs in a child so a debug assert
   is observed as the refusal rather than terminating the case list. */
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "TextWire.h"

static char *trim( char *text )
{
    char *end;
    while ( *text == ' ' || *text == '\t' ) { text++; }
    end = text + strlen( text );
    while ( end > text && ( end[-1] == ' ' || end[-1] == '\t' || end[-1] == '\r' || end[-1] == '\n' ) ) { *--end = 0; }
    return text;
}

static int run_case( const unsigned char * bytes, int length )
{
    uint32_t encoded[32] = {0};
    unsigned char input[32] = {0};
    packetwritetext_packet_text_t value, decoded;
    serialize_write_stream_t writer;
    serialize_read_stream_t reader;
    int i, written;

    memcpy( input, bytes, (size_t) length );
    memset( &value, 0, sizeof( value ) );
    value.text_length = length;
    for ( i = 0; i < length; i++ ) { value.text[i] = (char) input[i]; }

    serialize_write_stream_init( &writer, (unsigned char *) encoded, (int) sizeof( encoded ) );
    if ( !packetwritetext_write_packet_text( &writer, &value ) ) { return 10; }
    serialize_write_flush( &writer );
    written = serialize_write_bytes_processed( &writer );

    memset( &decoded, 0, sizeof( decoded ) );
    serialize_read_stream_init( &reader, (const unsigned char *) encoded, written );
    if ( !packetwritetext_read_packet_text( &reader, &decoded ) ) { return 11; }
    if ( decoded.text_length != length || memcmp( decoded.text, input, (size_t) length ) != 0 ) { return 12; }
    if ( serialize_read_bits_processed( &reader ) != serialize_write_bits_processed( &writer ) ) { return 13; }
    return 0;
}

static int parse_hex( const char * text, unsigned char * bytes )
{
    char copy[256];
    char *token;
    int length = 0;
    if ( strcmp( text, "-" ) == 0 ) { return 0; }
    if ( strlen( text ) >= sizeof( copy ) ) { return -1; }
    strcpy( copy, text );
    token = strtok( copy, " \t" );
    while ( token != NULL )
    {
        unsigned value;
        char extra;
        if ( sscanf( token, "%2x%c", &value, &extra ) != 1 || value > 255 || length >= 32 ) { return -1; }
        bytes[length++] = (unsigned char) value;
        token = strtok( NULL, " \t" );
    }
    return length;
}

int main( int argc, char ** argv )
{
    FILE * cases;
    char line[512];
    int failed = 0;
    if ( argc != 2 ) { fprintf( stderr, "usage: %s cases.txt\n", argv[0] ); return 2; }
    cases = fopen( argv[1], "r" );
    if ( cases == NULL ) { perror( argv[1] ); return 2; }
    while ( fgets( line, sizeof( line ), cases ) != NULL )
    {
        char *name, *hex, *verdict, *first;
        unsigned char bytes[32] = {0};
        int length;
        pid_t child;
        int wait_status;

        first = trim( line );
        if ( *first == 0 || *first == '#' ) { continue; }
        name = strtok( first, "|" );
        hex = strtok( NULL, "|" );
        verdict = strtok( NULL, "|" );
        if ( name == NULL || hex == NULL || verdict == NULL ) { fprintf( stderr, "invalid case row\n" ); failed = 1; break; }
        name = trim( name ); hex = trim( hex ); verdict = trim( verdict );
        length = parse_hex( hex, bytes );
        if ( length < 0 ) { fprintf( stderr, "invalid bytes for %s\n", name ); failed = 1; break; }

        child = fork();
        if ( child < 0 ) { perror( "fork" ); failed = 1; break; }
        if ( child == 0 )
        {
            (void) freopen( "/dev/null", "w", stderr );
            _exit( run_case( bytes, length ) );
        }
        if ( waitpid( child, &wait_status, 0 ) < 0 ) { perror( "waitpid" ); failed = 1; break; }
        if ( strcmp( verdict, "refused" ) == 0 )
        {
            if ( !WIFSIGNALED( wait_status ) || WTERMSIG( wait_status ) != SIGABRT )
            {
                fprintf( stderr, "FAILED: packet-write-text %s: expected debug assert, child status %d\n", name, wait_status );
                failed = 1;
            }
        }
        else if ( strcmp( verdict, "written" ) == 0 )
        {
            if ( !WIFEXITED( wait_status ) || WEXITSTATUS( wait_status ) != 0 )
            {
                fprintf( stderr, "FAILED: packet-write-text %s: expected byte-exact round trip, child status %d\n", name, wait_status );
                failed = 1;
            }
        }
        else { fprintf( stderr, "invalid verdict for %s\n", name ); failed = 1; }
        if ( failed ) { break; }
    }
    if ( ferror( cases ) ) { perror( "read cases" ); failed = 1; }
    fclose( cases );
    if ( failed ) { return 1; }
    puts( "packet-write-text C: all shared cases passed" );
    return 0;
}
