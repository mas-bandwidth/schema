/* The C packet Write's compressed-float case driver (docs/SPEC.md §5.1 row 10,
   docs/PACKET-WIRE-ALGORITHM.md §2.5). It reads the shared cases beside this
   directory and runs one child per case: the debug build's serialize_assert
   aborts, so death by SIGABRT is a refusal. A written case must read back to
   the value the case names (its declared value, or the quoted reads-back
   value). The child's stderr goes to /dev/null so an assert stays out of the
   driver log. */

#define _POSIX_C_SOURCE 200809L

#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "PacketCFloatWire.h"

enum
{
    kWroteExit = 10,
    kReadFailedExit = 11,
    kMismatchExit = 12,
    kWireExit = 13
};

typedef struct
{
    char name[64];
    float value;
    int refused;
    float reads_back;
    int ratio;
    int wire_bytes;
    uint8_t wire[16];
} cfloat_case_t;

static int parse_hex( const char * text, uint8_t * out, int capacity )
{
    int n = 0;
    size_t i;
    size_t length = strlen( text );
    if ( length == 0 || length % 2 != 0 || (int) ( length / 2 ) > capacity )
    {
        return -1;
    }
    for ( i = 0; i < length; i += 2 )
    {
        unsigned byte;
        char pair[3];
        char * end = NULL;
        pair[0] = text[i];
        pair[1] = text[i + 1];
        pair[2] = 0;
        byte = (unsigned) strtoul( pair, &end, 16 );
        if ( end != pair + 2 )
        {
            return -1;
        }
        out[n++] = (uint8_t) byte;
    }
    return n;
}

static int parse_float( const char * text, float * value )
{
    char * end = NULL;
    *value = strtof( text, &end );
    return end != text && *end == 0;
}

static int parse_case( char * line, cfloat_case_t * out )
{
    char * name;
    char * value;
    char * verdict;
    char * word;
    char * reads_back;
    char * save = NULL;

    line[strcspn( line, "\r\n" )] = 0;
    for ( name = line; *name == ' ' || *name == '\t'; name++ )
    {
    }
    if ( *name == 0 || *name == '#' )
    {
        return 0;
    }
    name = strtok_r( name, " \t", &save );
    value = strtok_r( NULL, " \t", &save );
    verdict = strtok_r( NULL, " \t", &save );
    if ( name == NULL || value == NULL || verdict == NULL )
    {
        return -1;
    }
    if ( strlen( name ) + 1 > sizeof( out->name ) || !parse_float( value, &out->value ) )
    {
        return -1;
    }
    snprintf( out->name, sizeof( out->name ), "%s", name );
    out->reads_back = out->value;
    if ( strcmp( verdict, "refused" ) == 0 )
    {
        out->refused = 1;
        return 1;
    }
    if ( strcmp( verdict, "written" ) != 0 )
    {
        return -1;
    }
    out->refused = 0;
    out->ratio = 0;
    out->wire_bytes = -1;
    while ( ( word = strtok_r( NULL, " \t", &save ) ) != NULL )
    {
        char * operand = strtok_r( NULL, " \t", &save );
        if ( operand == NULL )
        {
            return -1;
        }
        if ( strcmp( word, "reads-back" ) == 0 )
        {
            reads_back = operand;
            if ( !parse_float( reads_back, &out->reads_back ) )
            {
                return -1;
            }
        }
        else if ( strcmp( word, "type" ) == 0 )
        {
            if ( strcmp( operand, "RatioEndpoint" ) != 0 )
            {
                return -1;
            }
            out->ratio = 1;
        }
        else if ( strcmp( word, "wire" ) == 0 )
        {
            out->wire_bytes = parse_hex( operand, out->wire, (int) sizeof( out->wire ) );
            if ( out->wire_bytes < 0 )
            {
                return -1;
            }
        }
        else
        {
            return -1;
        }
    }
    return 1;
}

static unsigned float_bits( float value )
{
    unsigned bits;
    memcpy( &bits, &value, sizeof( bits ) );
    return bits;
}

/* The child: write the one value, read it back, and exit with what happened. */
static int run_child( const cfloat_case_t * test )
{
    cfloat_c_float_t in;
    cfloat_c_float_t out;
    cfloat_ratio_endpoint_t ratio_in;
    cfloat_ratio_endpoint_t ratio_out;
    uint8_t buffer[64];
    serialize_write_stream_t writer;
    serialize_read_stream_t reader;
    int bytes;
    int quiet;
    float got;

    quiet = open( "/dev/null", O_WRONLY );
    if ( quiet >= 0 )
    {
        dup2( quiet, 2 );
    }

    memset( &in, 0, sizeof( in ) );
    memset( &ratio_in, 0, sizeof( ratio_in ) );
    in.x = test->value;
    ratio_in.ratio = test->value;
    serialize_write_stream_init( &writer, buffer, (int) sizeof( buffer ) );
    if ( test->ratio ? !cfloat_write_ratio_endpoint( &writer, &ratio_in ) : !cfloat_write_c_float( &writer, &in ) )
    {
        return kReadFailedExit;
    }
    serialize_write_flush( &writer );
    if ( serialize_write_error( &writer ) )
    {
        return kReadFailedExit;
    }
    bytes = (int) serialize_write_bytes_processed( &writer );
    if ( test->wire_bytes >= 0 && ( bytes != test->wire_bytes || memcmp( buffer, test->wire, (size_t) bytes ) != 0 ) )
    {
        return kWireExit;
    }

    memset( &out, 0, sizeof( out ) );
    memset( &ratio_out, 0, sizeof( ratio_out ) );
    serialize_read_stream_init( &reader, buffer, bytes );
    if ( test->ratio ? !cfloat_read_ratio_endpoint( &reader, &ratio_out ) : !cfloat_read_c_float( &reader, &out ) )
    {
        return kReadFailedExit;
    }
    got = test->ratio ? ratio_out.ratio : out.x;
    if ( float_bits( got ) != float_bits( test->reads_back ) )
    {
        return kMismatchExit;
    }
    return kWroteExit;
}

int main( int argc, char ** argv )
{
    FILE * file;
    char line[512];
    int failed = 0;
    int cases = 0;
    struct rlimit no_core;

    if ( argc != 2 )
    {
        fprintf( stderr, "usage: driver %s\n", argv[0] );
        return 2;
    }
    file = fopen( argv[1], "r" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot open %s\n", argv[1] );
        return 2;
    }
    no_core.rlim_cur = 0;
    no_core.rlim_max = 0;
    setrlimit( RLIMIT_CORE, &no_core );

    while ( fgets( line, sizeof( line ), file ) )
    {
        cfloat_case_t test;
        pid_t child;
        int status = 0;
        long mark;
        int parsed = parse_case( line, &test );
        int refused = 0;
        int wrote = 0;
        int broke = 0;

        if ( parsed < 0 )
        {
            printf( "FAILED: packet-write-cfloat cases.txt: malformed case %s\n", line );
            failed = 1;
            continue;
        }
        if ( parsed == 0 )
        {
            continue;
        }
        cases++;
        fflush( stdout );
        fflush( stderr );
        /* The child's abort flushes this FILE and lseeks the shared open-file
           description by whatever the child still had buffered. Remember the
           logical position and put it back, or the next read repeats the file. */
        mark = ftell( file );
        if ( mark < 0 )
        {
            printf( "FAILED: packet-write-cfloat %s: could not mark the case file\n", test.name );
            failed = 1;
            break;
        }
        child = fork();
        if ( child < 0 )
        {
            printf( "FAILED: packet-write-cfloat %s: could not fork\n", test.name );
            failed = 1;
            continue;
        }
        if ( child == 0 )
        {
            _exit( run_child( &test ) );
        }
        if ( waitpid( child, &status, 0 ) != child )
        {
            printf( "FAILED: packet-write-cfloat %s: could not wait for the child\n", test.name );
            failed = 1;
            if ( fseek( file, mark, SEEK_SET ) != 0 )
            {
                break;
            }
            continue;
        }
        if ( fseek( file, mark, SEEK_SET ) != 0 )
        {
            printf( "FAILED: packet-write-cfloat %s: could not restore the case file\n", test.name );
            failed = 1;
            break;
        }
        if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT )
        {
            refused = 1;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit )
        {
            wrote = 1;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kMismatchExit )
        {
            wrote = 1;
            broke = 1;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWireExit )
        {
            wrote = 1;
            broke = 2;
        }
        else
        {
            broke = 1;
        }

        if ( test.refused && !refused )
        {
            printf( "FAILED: packet-write-cfloat %s: refused case was written\n", test.name );
            failed = 1;
        }
        else if ( !test.refused && refused )
        {
            printf( "FAILED: packet-write-cfloat %s: written case was refused\n", test.name );
            failed = 1;
        }
        else if ( !test.refused && !wrote )
        {
            printf( "FAILED: packet-write-cfloat %s: the write or read did not finish\n", test.name );
            failed = 1;
        }
        else if ( !test.refused && broke == 2 )
        {
            printf( "FAILED: packet-write-cfloat %s: the written bytes are not the case's wire\n", test.name );
            failed = 1;
        }
        else if ( !test.refused && broke )
        {
            printf( "FAILED: packet-write-cfloat %s: read back did not match its quantized value (want 0x%08x)\n",
                    test.name, float_bits( test.reads_back ) );
            failed = 1;
        }
    }
    fclose( file );

    if ( cases == 0 )
    {
        printf( "FAILED: packet-write-cfloat: no cases in %s\n", argv[1] );
        failed = 1;
    }
    if ( failed )
    {
        return 1;
    }
    printf( "packet-write-cfloat: %d cases hold the write contract\n", cases );
    return 0;
}
