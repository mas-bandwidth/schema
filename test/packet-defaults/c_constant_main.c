/* The C driver for the constant-default pins (hexadecimal-addition,
   unsigned-subtraction): a fresh Value whose defaults come from constants writes
   the pinned bytes and bit count, and the pin reads back over zeroed storage to
   a fresh Value. Built once per schema of
   test/conformance/packet/testdata/constant-defaults, each generated into its
   own directory (-I), since every one declares package arithmetic; PIN_NAME
   names the pin. Never skips: a missing or short pin fails. */
#include <stdio.h>
#include <string.h>
#include "DefaultsWire.h"

static int failed;

static int check( int condition, const char * name )
{
    if ( !condition ) { fprintf( stderr, "FAILED: %s\n", name ); failed = 1; }
    return condition;
}

static void run( const char * directory )
{
    uint64_t written[8] = { 0 }, expected[8] = { 0 };
    arithmetic_value_t fresh = arithmetic_new_value(), decoded;
    serialize_write_stream_t writer;
    serialize_read_stream_t reader;
    char path[1024];
    FILE * file;
    int bits = -1;
    size_t bytes;
    snprintf( path, sizeof( path ), "%s/%s.bin", directory, PIN_NAME );
    file = fopen( path, "rb" );
    if ( !check( file != NULL, path ) ) return;
    bytes = fread( expected, 1, sizeof( expected ), file );
    check( !ferror( file ) && feof( file ), "read pin bytes" );
    fclose( file );
    if ( !check( bytes > 0 && bytes <= sizeof( expected ) - 8, "pin has padded allocation" ) ) return;
    snprintf( path, sizeof( path ), "%s/%s.bits", directory, PIN_NAME );
    file = fopen( path, "rb" );
    if ( !check( file != NULL, path ) ) return;
    check( fscanf( file, "%d", &bits ) == 1, "read pin bit count" );
    fclose( file );
    serialize_write_stream_init( &writer, written, sizeof( written ) );
    if ( !check( arithmetic_write_value( &writer, &fresh ), "write fresh Value" ) ) return;
    serialize_write_flush( &writer );
    check( serialize_write_bytes_processed( &writer ) == (int) bytes && memcmp( written, expected, bytes ) == 0,
           "C++ pin " PIN_NAME " bytes" );
    check( serialize_write_bits_processed( &writer ) == bits, "C++ pin " PIN_NAME " bits" );
    memset( &decoded, 0, sizeof( decoded ) );
    serialize_read_stream_init( &reader, expected, (int) bytes );
    if ( !check( arithmetic_read_value( &reader, &decoded ), "read pin " PIN_NAME ) ) return;
    check( serialize_read_bits_processed( &reader ) == ( ( bits + 7 ) & ~7 ), "pin " PIN_NAME " read consumed bits" );
    check( memcmp( &decoded, &fresh, sizeof( decoded ) ) == 0, "pin " PIN_NAME " reads back to a fresh Value" );
}

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { fprintf( stderr, "usage: %s <golden-directory>\n", argv[0] ); return 2; }
    run( argv[1] );
    if ( failed ) return 1;
    puts( "packet defaults C: constant-default pin " PIN_NAME " OK" );
    return 0;
}
