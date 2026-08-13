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
#include "ObjectsWire.h"

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

    /* ---- the object SHALLOW view: the quantized wire ----
       The quantized values are the ones QuantizeShip produces for the Go and
       C++ tests' interpolate input; populating the shallow view directly tests
       the WIRE independently of the quantize pair. */
    {
        ShipData_Shallow q;
        memset( &q, 0, sizeof( q ) );
        q.ship_type = SHIP_TYPE_CORVETTE;
        q.position_x = 1536;      /* 1.5 * 1024 */
        q.position_y = -2304;     /* -2.25 * 1024 */
        q.position_z = 102400;    /* 100.0 * 1024 */
        q.rotation_x = 0; q.rotation_y = 0; q.rotation_z = 0; q.rotation_w = 1024;
        q.linear_velocity_x = 3072; q.linear_velocity_y = 0; q.linear_velocity_z = -1024;
        q.flags = SHIP_FLAGS_BOOSTING;
        q.team = TEAM_RED;
        q.health = 750;
        q.thrust = 55;

        /* the Quantize pair must produce exactly these values from the
           interpolate domain — the same check the Go and C++ tests make */
        {
            ShipData_Interpolate interp;
            ShipData_Shallow qq;
            memset( &interp, 0, sizeof( interp ) );
            interp.ship_type = SHIP_TYPE_CORVETTE;
            interp.position.x = 1.5; interp.position.y = -2.25; interp.position.z = 100.0;
            interp.rotation.x = 0.0; interp.rotation.y = 0.0; interp.rotation.z = 0.0; interp.rotation.w = 1.0;
            interp.linear_velocity.x = 3.0; interp.linear_velocity.y = 0.0; interp.linear_velocity.z = -1.0;
            interp.flags = SHIP_FLAGS_BOOSTING;
            interp.team = TEAM_RED;
            interp.health = 750;
            interp.thrust = 55;

            memset( &qq, 0, sizeof( qq ) );
            quantize_ship( &interp, &qq );
            check( qq.position_x == 1536, "1.5 * 1024 quantizes to 1536" );
            check( qq.position_y == -2304, "-2.25 * 1024 quantizes to -2304" );
            check( qq.position_z == 102400, "100.0 * 1024 quantizes to 102400" );
            check( qq.rotation_w == 1024, "1.0 * 1024 quantizes to 1024" );
            check( qq.linear_velocity_x == 3072 && qq.linear_velocity_z == -1024,
                   "velocity quantizes" );
            check( qq.health == 750 && qq.thrust == 55, "projected fields copy" );
            check( qq.team == TEAM_RED && qq.flags == SHIP_FLAGS_BOOSTING, "discrete fields copy" );

            /* and back: unquantize returns the representable quantum */
            {
                ShipData_Interpolate back;
                memset( &back, 0, sizeof( back ) );
                unquantize_ship( &qq, &back );
                check( back.position.x == 1.5, "1536 / 1024 unquantizes to 1.5 exactly" );
                check( back.position.y == -2.25, "-2304 / 1024 unquantizes to -2.25 exactly" );
                check( back.rotation.w == 1.0, "1024 / 1024 unquantizes to 1.0 exactly" );
            }
        }

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_ship_shallow( &w, &q ), "write ShipData_Shallow" );
        serialize_write_flush( &w );
        golden_wire( "ship_shallow", buffer, serialize_write_bytes_processed( &w ) );

        {
            ShipData_Shallow back;
            memset( &back, 0, sizeof( back ) );
            serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
            check( read_ship_shallow( &r, &back ), "read ShipData_Shallow" );
            check( back.position_x == 1536 && back.position_y == -2304 && back.position_z == 102400,
                   "shallow position round-trips" );
            check( back.rotation_w == 1024, "shallow rotation round-trips" );
            check( back.health == 750 && back.thrust == 55, "shallow projected fields round-trip" );
        }
    }

    /* ---- the Message dispatch surface: tag + union, and the terminator ---- */
    {
        Message m;
        memset( &m, 0, sizeof( m ) );

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );

        m.type = MESSAGE_TYPE_CHAT;
        memset( &m.as.chat, 0, sizeof( m.as.chat ) );
        memcpy( m.as.chat.text, "dispatch", 8 );
        m.as.chat.text_length = 8;
        check( write_message( &w, &m ), "write Message chat" );

        m.type = MESSAGE_TYPE_TEST;
        memset( &m.as.test, 0, sizeof( m.as.test ) );
        m.as.test.test_b = 42;
        check( write_message( &w, &m ), "write Message test" );

        m.type = MESSAGE_TYPE_NONE;
        check( write_message( &w, &m ), "write Message terminator" );

        serialize_write_flush( &w );
        golden_wire( "message_stream", buffer, serialize_write_bytes_processed( &w ) );

        {
            Message back;
            serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
            memset( &back, 0, sizeof( back ) );
            check( read_message( &r, &back ), "read Message 1" );
            check( back.type == MESSAGE_TYPE_CHAT, "first message is Chat" );
            check( back.as.chat.text_length == 8 && memcmp( back.as.chat.text, "dispatch", 8 ) == 0,
                   "Chat arm decodes" );
            check( read_message( &r, &back ), "read Message 2" );
            check( back.type == MESSAGE_TYPE_TEST && back.as.test.test_b == 42, "Test arm decodes" );
            check( read_message( &r, &back ), "read Message terminator" );
            check( back.type == MESSAGE_TYPE_NONE, "terminator decodes as None" );
        }
    }

    /* ---- SPEC §5: BOTH untaken sides zero on read ----
       The backend used to zero only the then-fields in the else arm, so taking
       the then arm left the else-fields holding whatever the caller's memory
       had. Nothing failed: the wire was correct and the storage was stale. */
    {
        ProbeSample in, out;

        memset( &in, 0, sizeof( in ) );
        in.active = 1;
        in.orientation = 45.0f;
        in.raw_delta = 7;
        in.big_delta = 9;
        in.weapon = WEAPON_RAILGUN;
        in.has_target = 1;
        in.target_id = 4242;
        in.samples_count = 1;   /* [1..8]: the minimum is 1, not 0 */
        in.samples[0] = 99;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_probe_sample( &w, &in ), "write ProbeSample active" );
        serialize_write_flush( &w );

        /* dirty every member the ACTIVE arm does not write */
        memset( &out, 0xEF, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_probe_sample( &r, &out ), "read ProbeSample active" );
        check( out.active, "active reads true" );
        check( out.weapon == WEAPON_RAILGUN, "the taken arm decodes" );
        check( out.idle_ticks == 0,
               "the UNTAKEN else field is zeroed when the then arm is taken (SPEC §5)" );

        /* and the mirror: taking the else arm zeroes the then fields */
        in.active = 0;
        in.idle_ticks = 1234;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_probe_sample( &w, &in ), "write ProbeSample idle" );
        serialize_write_flush( &w );

        memset( &out, 0xEF, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_probe_sample( &r, &out ), "read ProbeSample idle" );
        check( !out.active && out.idle_ticks == 1234, "the else arm decodes" );
        check( out.weapon == 0 && out.target_id == 0,
               "the UNTAKEN then fields are zeroed when the else arm is taken (SPEC §5)" );
    }

    if ( failed )
    {
        printf( "FAILED\n" );
        return 1;
    }
    printf( "OK\n" );
    return 0;
}
