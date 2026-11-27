// Per-PR happy path for the C++ leg (ci-fast.yml): one ShipCreate written,
// read back, every field compared. The heavy C++ gates run nightly.

#include <cstdio>

#include "HappyWire.h"

static int fail( const char * why )
{
    std::fprintf( stderr, "happy cpp: FAIL: %s\n", why );
    return 1;
}

int main()
{
    // + 8: a read buffer must extend 8 bytes past the data (the reader loads 64-bit windows)
    alignas( 8 ) static uint8_t buffer[happy::ShipCreateMaxBytes + 8] = {};

    happy::ShipCreate in;
    in.ship_type = happy::ShipType::Corvette;
    in.position = { 1.5, -2.25, 3.0 };
    in.health = 777;
    in.alive = true;

    serialize::WriteStream writer( buffer, happy::ShipCreateMaxBytes );
    if ( !happy::WriteShipCreate( writer, in ) )
        return fail( "write ShipCreate" );
    writer.Flush();
    const int64_t bytes = writer.GetBytesProcessed();

    happy::ShipCreate out;
    serialize::ReadStream reader( buffer, bytes );
    if ( !happy::ReadShipCreate( reader, out ) )
        return fail( "read ShipCreate" );

    if ( out.ship_type != happy::ShipType::Corvette ) return fail( "ship_type mismatch" );
    if ( out.position.x != 1.5 ) return fail( "position.x mismatch" );
    if ( out.position.y != -2.25 ) return fail( "position.y mismatch" );
    if ( out.position.z != 3.0 ) return fail( "position.z mismatch" );
    if ( out.health != 777 ) return fail( "health mismatch" );
    if ( out.alive != true ) return fail( "alive mismatch" );

    std::printf( "happy cpp: ok (%d bytes)\n", int( bytes ) );
    return 0;
}
