/*
    The C corpus test — the fifth target's half of the cross-language gate.

    Every golden here is the SAME file the C++, C#, Go and Rust tests are held
    to. That is the whole point: a target that only checks itself proves
    nothing about the property this project exists to provide.
*/

#include <stdio.h>
#include <string.h>

#include "TypesWire.h"
#include "MessagesWire.h"
#include "EnumsWire.h"
#include "WireWire.h"

static int failed = 0;

static void check( int condition, const char * message )
{
    if ( !condition )
    {
        printf( "FAILED: %s\n", message );
        failed = 1;
    }
}

/* golden_wire byte-compares written wire against the C++-pinned golden. */
static void golden_wire( const char * name, const unsigned char * data, int bytes )
{
    char path[512];
    static unsigned char expected[8192];
    FILE * file;
    size_t n;
    int i;

    sprintf( path, "../../testdata/wire/%s.bin", name );
    file = fopen( path, "rb" );
    if ( !file )
    {
        printf( "FAILED: cannot open wire golden %s\n", path );
        failed = 1;
        return;
    }
    n = fread( expected, 1, sizeof( expected ), file );
    fclose( file );

    if ( (int) n != bytes )
    {
        printf( "FAILED: wire golden %s — C wrote %d bytes, golden is %d\n", name, bytes, (int) n );
        failed = 1;
        return;
    }
    for ( i = 0; i < bytes; i++ )
    {
        if ( data[i] != expected[i] )
        {
            printf( "FAILED: wire golden %s — first difference at byte %d: C=%02x golden=%02x\n",
                    name, i, data[i], expected[i] );
            failed = 1;
            return;
        }
    }
}

static void fill_test_data( TestData * in )
{
    int i;
    memset( in, 0, sizeof( *in ) );
    in->a = -100;
    in->b = 100;
    in->c = 149;
    in->d = 0x11;
    in->e = 0x22;
    in->f = 0x33;
    in->g = 1;
    in->items_count = 3;
    in->items[0] = 0;
    in->items[1] = 128;
    in->items[2] = 255;
    in->float_value = 3.1415926f;
    in->compressed_float_value = 2.5f;
    in->double_value = 1.0 / 3.0;
    in->int8_value = -128;
    in->int16_value = -32768;
    in->uint8_value = 255;
    in->uint16_value = 65535;
    in->uint32_value = 4294967295u;
    in->uint64_value = 18446744073709551615ULL;
    in->int64_full = (-9223372036854775807LL - 1);
    in->int64_range = -999999999999LL;
    for ( i = 0; i < (int) sizeof( in->fixed_bytes ); i++ )
    {
        in->fixed_bytes[i] = (uint8_t) ( i * 3 );
    }
    memcpy( in->text, "the quick brown fox", 19 );
    in->text_length = 19;
}

int main( void )
{
    static unsigned char buffer[8192];
    serialize_write_stream_t w;
    serialize_read_stream_t r;

    /* ---- ShipCreate: the bool-gated flags branch, both ways ---- */
    {
        ShipCreate in, out;
        memset( &in, 0, sizeof( in ) );
        in.ship_type = SHIP_TYPE_BOMBER;
        in.position.x = 1000; in.position.y = -2000; in.position.z = 3000;
        in.has_flags = 1;
        in.flags = SHIP_FLAGS_BOOSTING | SHIP_FLAGS_AIMING;
        in.team = TEAM_BLUE;
        in.health = 750;
        in.thrust = 55;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_ship_create( &w, &in ), "write ShipCreate" );
        serialize_write_flush( &w );
        golden_wire( "shipcreate_flags", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_ship_create( &r, &out ), "read ShipCreate" );
        check( out.ship_type == SHIP_TYPE_BOMBER && out.team == TEAM_BLUE, "ShipCreate enums round-trip" );
        check( out.health == 750 && out.thrust == 55, "ShipCreate ranged ints round-trip" );
        check( out.flags == ( SHIP_FLAGS_BOOSTING | SHIP_FLAGS_AIMING ), "ShipCreate flags round-trip" );

        /* the untaken branch must read as zero (SPEC §5) */
        in.has_flags = 0;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_ship_create( &w, &in ), "write ShipCreate no flags" );
        serialize_write_flush( &w );
        memset( &out, 0, sizeof( out ) );
        out.flags = 0xFF; /* dirty — the read must clear it */
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_ship_create( &r, &out ), "read ShipCreate no flags" );
        check( !out.has_flags && out.flags == 0, "untaken branch reads as zero (SPEC §5)" );
    }

    /* ---- RigidBody: the back-reference example, both branch sides ---- */
    {
        RigidBody in, out;
        memset( &in, 0, sizeof( in ) );
        in.position.x = 1.5; in.position.y = -2.5; in.position.z = 3.25;
        in.orientation.x = 0.1; in.orientation.y = 0.2; in.orientation.z = 0.3; in.orientation.w = 0.9;
        in.at_rest = 0;
        in.linear_velocity.x = 10.0; in.linear_velocity.y = 20.0; in.linear_velocity.z = -3.0;
        in.angular_velocity.x = 0.25; in.angular_velocity.y = 0.5; in.angular_velocity.z = 0.75;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_rigid_body( &w, &in ), "write RigidBody moving" );
        serialize_write_flush( &w );
        golden_wire( "rigidbody_moving", buffer, serialize_write_bytes_processed( &w ) );

        in.at_rest = 1;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_rigid_body( &w, &in ), "write RigidBody at rest" );
        serialize_write_flush( &w );
        golden_wire( "rigidbody_at_rest", buffer, serialize_write_bytes_processed( &w ) );

        /* the at-rest read must ZERO both velocities (SPEC §5) */
        memset( &out, 0, sizeof( out ) );
        out.linear_velocity.x = 99.0;
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_rigid_body( &r, &out ), "read RigidBody at rest" );
        check( out.at_rest, "at_rest reads true" );
        check( out.linear_velocity.x == 0.0 && out.linear_velocity.y == 0.0 && out.linear_velocity.z == 0.0 &&
               out.angular_velocity.x == 0.0 && out.angular_velocity.y == 0.0 && out.angular_velocity.z == 0.0,
               "velocities read as zero under the taken at-rest branch (SPEC §5)" );
    }

    /* ---- Chat: the string framing ---- */
    {
        Chat in, out;
        memset( &in, 0, sizeof( in ) );
        memcpy( in.text, "wire parity", 11 );
        in.text_length = 11;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_chat( &w, &in ), "write Chat" );
        serialize_write_flush( &w );
        golden_wire( "chat", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_chat( &r, &out ), "read Chat" );
        check( out.text_length == 11 && memcmp( out.text, "wire parity", 11 ) == 0, "Chat round-trips" );
    }

    /* ---- ProbeBits: the full-range uint32/uint64 paths ---- */
    {
        ProbeBits in, out;
        memset( &in, 0, sizeof( in ) );
        in.small = 0x1FF;
        in.boundary = 0x1FFFFFFFFULL;
        in.wide = 0xFEDCBA9876543210ULL;
        in.sensor = 4294967295u;
        in.nonce = 18446744073709551615ULL;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_probe_bits( &w, &in ), "write ProbeBits" );
        serialize_write_flush( &w );
        golden_wire( "probebits", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_probe_bits( &r, &out ), "read ProbeBits" );
        check( out.small == in.small && out.boundary == in.boundary && out.wide == in.wide &&
               out.sensor == in.sensor && out.nonce == in.nonce,
               "ProbeBits round-trips — 9/33/64-bit and full-range paths" );
    }

    /* ---- TestData: signed narrows, full-range ints, align, fixed bytes, string ---- */
    {
        TestData in, out;
        fill_test_data( &in );

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_test_data( &w, &in ), "write TestData" );
        serialize_write_flush( &w );
        golden_wire( "testdata", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_test_data( &r, &out ), "read TestData" );
        check( out.a == in.a && out.b == in.b && out.c == in.c, "TestData ranged ints" );
        check( out.uint32_value == in.uint32_value && out.uint64_value == in.uint64_value,
               "TestData full-range unsigned" );
        check( out.int64_full == in.int64_full && out.int64_range == in.int64_range,
               "TestData int64 paths" );
        check( out.text_length == 19 && memcmp( out.text, "the quick brown fox", 19 ) == 0,
               "TestData string" );
        check( memcmp( out.fixed_bytes, in.fixed_bytes, sizeof( in.fixed_bytes ) ) == 0,
               "TestData fixed bytes" );
    }

    /* ---- InputPacket: the counted array ---- */
    {
        InputPacket packet;
        memset( &packet, 0, sizeof( packet ) );
        packet.synchronize_sequence = 7;
        packet.current_frame = 123456789;
        packet.start_frame = 123456780;
        packet.inputs_count = 2;
        packet.inputs[0].throttle = 0.5f;
        packet.inputs[0].fire = 1;
        packet.inputs[1].stick_x = -0.25f;
        packet.inputs[1].boost = 1;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_input_packet( &w, &packet ), "write InputPacket" );
        serialize_write_flush( &w );
        golden_wire( "inputpacket", buffer, serialize_write_bytes_processed( &w ) );
    }

    if ( failed )
    {
        printf( "FAILED\n" );
        return 1;
    }
    printf( "OK\n" );
    return 0;
}
