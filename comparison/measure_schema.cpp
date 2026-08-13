// Measure the schema wire size of ShipCreate, encoding the values in VALUES.md.
// This encodes for real and reports the byte count the writer produced - it does
// not read the compiler's ShipCreateMaxBits constant, so it is a measurement and
// not a restatement of what the compiler already believes.
#include <cstdio>
#include <cstdint>
#include "Types.h"
#include "TypesWire.h"

using namespace example;

int main()
{
    ShipCreate ship;
    ship.ship_type = ShipType::Destroyer;
    ship.position.x = 1234567;  ship.position.y = -2345678; ship.position.z = 3456789;
    ship.rotation.x = 100; ship.rotation.y = -200; ship.rotation.z = 300; ship.rotation.w = 400;
    ship.linear_velocity.x = 12345; ship.linear_velocity.y = -23456; ship.linear_velocity.z = 34567;
    ship.has_flags = true;
    ship.flags = ShipFlags_Boosting | ShipFlags_Aiming;
    ship.team = Team::Blue;
    ship.health = 750;
    ship.thrust = 42;

    uint8_t buffer[256];
    serialize::WriteStream ws( buffer, sizeof( buffer ) );
    if ( !WriteShipCreate( ws, ship ) ) { printf( "write failed\n" ); return 1; }
    ws.Flush();
    const int bytes = ws.GetBytesProcessed();

    // Round-trip it, so the number reported belongs to a buffer that actually decodes.
    ShipCreate back;
    serialize::ReadStream rs( buffer, bytes );
    if ( !ReadShipCreate( rs, back ) ) { printf( "read failed\n" ); return 1; }
    if ( back.health != 750 || back.thrust != 42 || back.position.y != -2345678 ||
         back.rotation.w != 400 || back.team != Team::Blue || back.flags != ship.flags ) {
        printf( "round-trip mismatch\n" ); return 1;
    }

    printf( "SCHEMA: %d bytes (%d bits used of %lld max)\n",
            bytes, (int) ws.GetBitsProcessed(), (long long) ShipCreateMaxBits );
    return 0;
}
