/* The packet carrier's C driver (test/conformance/packet/carrier.go states
   the contract): write|read <codec> <in> <out>. The generated headers sit
   beside it; one codec per seed root. */

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "SeedWire.h"

#define MAX_FIELDS 256
#define MAX_PACKET 4096
#define MAX_LINE 65536

typedef int ( *write_fn )( const long long * fields, int count, uint8_t * packet, int * bytes );
typedef int ( *read_fn )( const uint8_t * packet, int bytes, long long * fields, int * count );

static int write_seed_t( const long long * f, int count, uint8_t * packet, int * bytes )
{
    SEED_T v;
    int at = 0;
    int i;
    uint64_t buffer[( SEED_T_MAX_BYTES + 8 ) / 8];
    serialize_write_stream_t stream;
    memset( &v, 0, sizeof( v ) );
    if ( count < 1 )
        return 0;
    if ( f[at++] == 1 )
    {
        if ( at >= count )
            return 0;
        seed_u_select_a( &v.u )->v = (uint8_t) f[at++];
    }
    for ( i = 0; i < 4; i++ )
    {
        if ( at >= count )
            return 0;
        v.data[i] = (uint8_t) f[at++];
    }
    if ( at != count )
        return 0;
    memset( buffer, 0, sizeof( buffer ) );
    serialize_write_stream_init( &stream, buffer, (int) sizeof( buffer ) );
    if ( !seed_write_t( &stream, &v ) )
        return 0;
    serialize_write_flush( &stream );
    if ( serialize_write_error( &stream ) )
        return 0;
    *bytes = (int) serialize_write_bytes_processed( &stream );
    memcpy( packet, buffer, (size_t) *bytes );
    return 1;
}

static int read_seed_t( const uint8_t * packet, int bytes, long long * f, int * count )
{
    SEED_T v;
    int i;
    uint8_t buffer[MAX_PACKET + 8];
    serialize_read_stream_t stream;
    memset( buffer, 0, sizeof( buffer ) );
    memcpy( buffer, packet, (size_t) bytes );
    serialize_read_stream_init( &stream, buffer, bytes );
    memset( &v, 0, sizeof( v ) );
    if ( !seed_read_t( &stream, &v ) )
        return 0;
    *count = 0;
    f[( *count )++] = (long long) v.u.type;
    if ( v.u.type == SEED_U_TYPE_A )
        f[( *count )++] = (long long) v.u.as.a.v;
    for ( i = 0; i < 4; i++ )
        f[( *count )++] = (long long) v.data[i];
    return 1;
}

static int hexval( char c )
{
    if ( c >= '0' && c <= '9' )
        return c - '0';
    if ( c >= 'a' && c <= 'f' )
        return c - 'a' + 10;
    return -1;
}

int main( int argc, char ** argv )
{
    write_fn writer = NULL;
    read_fn reader = NULL;
    int writing;
    FILE * in;
    FILE * out;
    static char line[MAX_LINE];
    if ( argc != 5 )
    {
        fprintf( stderr, "usage: driver write|read <codec> <in> <out>\n" );
        return 1;
    }
    if ( strcmp( argv[2], "union-before-bytes" ) == 0 )
    {
        writer = write_seed_t;
        reader = read_seed_t;
    }
    else
    {
        fprintf( stderr, "driver: no codec %s\n", argv[2] );
        return 1;
    }
    if ( strcmp( argv[1], "write" ) == 0 )
        writing = 1;
    else if ( strcmp( argv[1], "read" ) == 0 )
        writing = 0;
    else
    {
        fprintf( stderr, "driver: no verb %s\n", argv[1] );
        return 1;
    }
    in = fopen( argv[3], "r" );
    out = fopen( argv[4], "w" );
    if ( !in || !out )
    {
        fprintf( stderr, "driver: cannot open %s or %s\n", argv[3], argv[4] );
        return 1;
    }
    while ( fgets( line, sizeof( line ), in ) )
    {
        size_t n = strcspn( line, "\r\n" );
        line[n] = 0;
        if ( writing )
        {
            long long fields[MAX_FIELDS];
            int count = 0;
            char * p = line;
            char * end;
            uint8_t packet[MAX_PACKET];
            int bytes = 0;
            int i;
            for ( ;; )
            {
                long long v = strtoll( p, &end, 10 );
                if ( end == p )
                    break;
                if ( count == MAX_FIELDS )
                {
                    fprintf( stderr, "driver: too many fields\n" );
                    return 1;
                }
                fields[count++] = v;
                p = end;
            }
            if ( writer( fields, count, packet, &bytes ) )
            {
                for ( i = 0; i < bytes; i++ )
                    fprintf( out, "%02x", packet[i] );
                fprintf( out, "\n" );
            }
            else
            {
                fprintf( out, "refused\n" );
            }
        }
        else
        {
            uint8_t packet[MAX_PACKET];
            long long fields[MAX_FIELDS];
            int bytes = 0;
            int count = 0;
            size_t i;
            if ( n % 2 != 0 || n / 2 > MAX_PACKET )
            {
                fprintf( stderr, "driver: bad hex line\n" );
                return 1;
            }
            for ( i = 0; i < n; i += 2 )
            {
                int hi = hexval( line[i] );
                int lo = hexval( line[i + 1] );
                if ( hi < 0 || lo < 0 )
                {
                    fprintf( stderr, "driver: bad hex line\n" );
                    return 1;
                }
                packet[bytes++] = (uint8_t) ( hi * 16 + lo );
            }
            if ( reader( packet, bytes, fields, &count ) )
            {
                fprintf( out, "ok" );
                for ( i = 0; i < (size_t) count; i++ )
                    fprintf( out, " %lld", fields[i] );
                fprintf( out, "\n" );
            }
            else
            {
                fprintf( out, "refused\n" );
            }
        }
    }
    fclose( in );
    return fclose( out ) == 0 ? 0 : 1;
}
