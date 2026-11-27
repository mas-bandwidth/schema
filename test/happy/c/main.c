/* Per-PR happy path for the C leg (ci-fast.yml): one ShipCreate written,
   read back, every field compared. The heavy C gates run nightly. */

#include <stdio.h>
#include <string.h>

#include "HappyWire.h"

#define FAIL( why ) do { fprintf( stderr, "happy c: FAIL: %s\n", why ); return 1; } while ( 0 )

int main( void )
{
    /* + 8: a read buffer must extend 8 bytes past the data (serialize.c loads 64-bit windows) */
    static unsigned char buffer[HAPPY_SHIP_CREATE_MAX_BYTES + 8];
    serialize_write_stream_t w;
    serialize_read_stream_t r;
    happy_ship_create_t in, out;
    int bytes;

    memset( &in, 0, sizeof( in ) );
    in.ship_type = HAPPY_SHIP_TYPE_CORVETTE;
    in.position.x = 1.5;
    in.position.y = -2.25;
    in.position.z = 3.0;
    in.health = 777;
    in.alive = true;

    serialize_write_stream_init( &w, buffer, HAPPY_SHIP_CREATE_MAX_BYTES );
    if ( !happy_write_ship_create( &w, &in ) )
        FAIL( "write ShipCreate" );
    serialize_write_flush( &w );
    if ( serialize_write_error( &w ) )
        FAIL( "write stream error" );
    bytes = (int) serialize_write_bytes_processed( &w );

    memset( &out, 0, sizeof( out ) );
    serialize_read_stream_init( &r, buffer, bytes );
    if ( !happy_read_ship_create( &r, &out ) )
        FAIL( "read ShipCreate" );

    if ( out.ship_type != HAPPY_SHIP_TYPE_CORVETTE ) FAIL( "ship_type mismatch" );
    if ( out.position.x != 1.5 ) FAIL( "position.x mismatch" );
    if ( out.position.y != -2.25 ) FAIL( "position.y mismatch" );
    if ( out.position.z != 3.0 ) FAIL( "position.z mismatch" );
    if ( out.health != 777 ) FAIL( "health mismatch" );
    if ( out.alive != true ) FAIL( "alive mismatch" );

    printf( "happy c: ok (%d bytes)\n", bytes );
    return 0;
}
