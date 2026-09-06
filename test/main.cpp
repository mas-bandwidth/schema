// Includes every generated header, round-trips generated types
// through the classic serialize runtime, and prints OK. second.cpp includes
// the same headers into a second translation unit, so a successful link also
// proves the headers are multiple-inclusion safe.

#include <cmath> // the gate-7 discrimination tripwires (std::fma, std::floor)
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <type_traits>
#include <thread>
#include <new> // placement new — the raw-struct scatter constructs in place

#include "ArmDefaultsWire.h"
#include "ClausesWire.h"
#include "ConstantsWire.h"
#include "DegenerateWire.h"
#include "EnumsWire.h"
#include "JoinsWire.h"
#include "RenderWire.h"
#include "TypesWire.h"
#include "WireWire.h"


// defined in second.cpp — proves cross-TU linkage over the same headers
int touch_generated_types();

// The wire oracle (SPEC §7.2 gate 3): hand-written classic-serialize twins.
// Generated code must produce byte-identical output to what a careful expert
// writes against the runtime by hand — the serialize README's own RigidBody.
namespace oracle
{
    struct Vector
    {
        double x = 0, y = 0, z = 0;
        template <typename Stream> bool Serialize( Stream & stream )
        {
            serialize_double( stream, x );
            serialize_double( stream, y );
            serialize_double( stream, z );
            return true;
        }
    };

    struct Quaternion
    {
        double x = 0, y = 0, z = 0, w = 0;
        template <typename Stream> bool Serialize( Stream & stream )
        {
            serialize_double( stream, x );
            serialize_double( stream, y );
            serialize_double( stream, z );
            serialize_double( stream, w );
            return true;
        }
    };

    struct RigidBody
    {
        Vector position;
        Quaternion orientation;
        Vector linearVelocity;
        Vector angularVelocity;
        bool atRest = false;

        template <typename Stream> bool Serialize( Stream & stream )
        {
            serialize_object( stream, position );
            serialize_object( stream, orientation );
            serialize_bool( stream, atRest );
            if ( !atRest )
            {
                serialize_object( stream, linearVelocity );
                serialize_object( stream, angularVelocity );
            }
            else if ( Stream::IsReading )
            {
                linearVelocity.x = linearVelocity.y = linearVelocity.z = 0.0;
                angularVelocity.x = angularVelocity.y = angularVelocity.z = 0.0;
            }
            return true;
        }
    };
}

#define check( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s (%s:%d)\n", #condition, __FILE__, __LINE__ ); \
            return 1;                                                         \
        }                                                                     \
    } while ( 0 )

// Golden wire bytes (SPEC §7.2 gate 7): every pinned instance's encoding is
// checked byte-for-byte against testdata/wire/<name>.bin. A break here under
// an unchanged schema is the stop-the-line event of SPEC §3.1, never a quiet
// re-pin. SCHEMA_UPDATE_WIRE_GOLDENS=1 rewrites the goldens deliberately.
static bool golden_wire( const char * name, const uint8_t * data, int64_t bytes, int64_t bits )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/%s.bin", name );
    char bits_path[256];
    snprintf( bits_path, sizeof( bits_path ), "testdata/wire/%s.bits", name );
    if ( std::getenv( "SCHEMA_UPDATE_WIRE_GOLDENS" ) )
    {
        FILE * f = fopen( path, "wb" );
        if ( !f )
        {
            printf( "cannot write %s\n", path );
            return false;
        }
        fwrite( data, 1, (size_t) bytes, f );
        fclose( f );
        // the bit-exact measure oracle (#163): the reference writer's exact
        // bit count, pinned beside the bytes so a measure error confined to
        // the final byte cannot hide behind byte-granularity gates. bits < 0
        // = no oracle: a concatenated multi-instance stream has no single
        // writer, and its chunks are byte-aligned anyway.
        if ( bits < 0 )
            return true;
        f = fopen( bits_path, "wb" );
        if ( !f )
        {
            printf( "cannot write %s\n", bits_path );
            return false;
        }
        fprintf( f, "%lld\n", (long long) bits );
        fclose( f );
        return true;
    }
    FILE * f = fopen( path, "rb" );
    if ( !f )
    {
        printf( "missing wire golden %s (run: make update-goldens)\n", path );
        return false;
    }
    static uint8_t expected[4096];
    size_t n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int64_t) n != bytes || std::memcmp( expected, data, (size_t) bytes ) != 0 )
    {
        printf( "WIRE GOLDEN MISMATCH: %s (%lld golden vs %lld actual bytes) — stop-the-line (SPEC §3.1, §7.2 gate 7)\n",
                name, (long long) n, (long long) bytes );
        return false;
    }
    if ( bits < 0 )
        return true;
    f = fopen( bits_path, "rb" );
    if ( !f )
    {
        printf( "missing bits golden %s (run: make update-goldens)\n", bits_path );
        return false;
    }
    long long expected_bits = -1;
    if ( fscanf( f, "%lld", &expected_bits ) != 1 )
        expected_bits = -1;
    fclose( f );
    if ( expected_bits != (long long) bits )
    {
        printf( "BITS GOLDEN MISMATCH: %s (%lld golden vs %lld actual bits) — stop-the-line (SPEC §3.1, §7.2 gate 7)\n",
                name, expected_bits, (long long) bits );
        return false;
    }
    return true;
}

static bool selected_arm_defaults( const example::DefaultArm & arm )
{
    if ( arm.entries_count != 0 || arm.marker != 5 )
        return false;
    for ( const auto & entry : arm.entries )
        if ( entry.retries != -1 || entry.preferred != example::Weapon::Railgun )
            return false;
    return true;
}

static int test_selected_arm_defaults()
{
    using namespace example;
    // Independent bit oracle: tag 1 in two bits, count 0 in two, marker 5 in three.
    alignas(8) const uint8_t wire[8] = { 0x51 };
    DefaultChoice in;
    ::new ( (void*) &in.first ) DefaultArm{};
    in.type = DefaultChoiceType::First;
    in.first.marker = 5;
    check( selected_arm_defaults( in.first ) );
    alignas(8) uint8_t buffer[64] = {};
    serialize::WriteStream writer( buffer, sizeof(buffer) );
    check( WriteDefaultChoice( writer, in ) );
    check( writer.GetBitsProcessed() == 7 );
    writer.Flush();
    check( writer.GetBytesProcessed() == 1 && buffer[0] == 0x51 );

    DefaultChoice out;
    ::new ( (void*) &out.second ) DefaultArm{};
    out.type = DefaultChoiceType::Second;
    for ( int repeat = 0; repeat < 2; ++repeat )
    {
        auto & prior = repeat == 0 ? out.second : out.first;
        prior.entries_count = 2;
        for ( auto & entry : prior.entries )
        {
            entry.retries = 99;
            entry.preferred = Weapon::Laser;
        }
        serialize::ReadStream reader( wire, 1 );
        check( ReadDefaultChoice( reader, out ) );
        check( reader.GetBitsProcessed() == 7 );
        check( out.type == DefaultChoiceType::First );
        check( selected_arm_defaults( out.first ) );
    }

    // A bulk field selects the ordinary read path in targets with scalar batching.
    DefaultBulkChoice bulk;
    ::new ( (void*) &bulk.first ) DefaultBulkArm{};
    bulk.type = DefaultBulkChoiceType::First;
    bulk.first.payload.marker = 5;
    serialize::WriteStream bulk_writer( buffer, sizeof(buffer) );
    check( WriteDefaultBulkChoice( bulk_writer, bulk ) );
    check( bulk_writer.GetBitsProcessed() == 16 );
    bulk_writer.Flush();
    check( bulk_writer.GetBytesProcessed() == 2 && buffer[0] == 0x51 && buffer[1] == 0 );
    bulk.first.payload.entries[0].retries = 99;
    bulk.first.payload.entries[1].retries = 98;
    serialize::ReadStream bulk_reader( buffer, 2 );
    check( ReadDefaultBulkChoice( bulk_reader, bulk ) );
    check( bulk_reader.GetBitsProcessed() == 16 );
    check( selected_arm_defaults( bulk.first.payload ) );
    return 0;
}

int main()
{
    using namespace example;
    check( test_selected_arm_defaults() == 0 );

    // constants fold and export (SPEC §4.2)
    static_assert(MaxPositionUnits == MaxWorldMeters * PositionUnits, "constants compose");
    static_assert(NumTeams == 2, "NumTeams = Team.Max — entry 0 is the None sentinel in every enum, so the count of real things is always Enum.Max");
    static_assert(ProtocolId != 0, "the unit has a protocol id");

    // enums: None = 0 implicit, variants dense from 1 (SPEC §4.2)
    static_assert(static_cast<int>(Team::None) == 0, "None = 0");
    static_assert(static_cast<int>(Team::Blue) == 2, "variants pack from 1");

    // flags: one bit per variant from bit 0 (SPEC §4.2)
    static_assert(ShipFlags_FiringLaser == 1ull << 0, "flags bits assign in declaration order");
    static_assert(ShipFlags_Aiming == 1ull << 3, "flags bits assign in declaration order");

    // worst-case bounds exist and are sane (SPEC §6.1 item 4)
    static_assert(RigidBodyMaxBits > 0 && RigidBodyMaxBytes >= RigidBodyMaxBits / 8, "MaxBits/MaxBytes");

    alignas( 8 ) uint8_t buffer[2048 + 8];  // + 8: read buffer allocations extend 8 bytes past the data (the reader loads 64-bit windows)

    // ---- RigidBody, moving: the serialize README example, generated ----
    {
        RigidBody in;
        in.position = { 1.5, -2.5, 3.25 };
        in.orientation = { 0.0, 0.0, 0.0, 1.0 };
        in.at_rest = false;
        in.linear_velocity = { 10.0, 0.0, -3.0 };
        in.angular_velocity = { 0.25, 0.5, 0.75 };

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteRigidBody( ws, in ) );
        ws.Flush();

        RigidBody out;
        out.linear_velocity = { 99.0, 99.0, 99.0 }; // dirty — the read must overwrite
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadRigidBody( rs, out ) );
        check( out.position.x == 1.5 && out.position.y == -2.5 && out.position.z == 3.25 );
        check( out.orientation.w == 1.0 );
        check( !out.at_rest );
        check( out.linear_velocity.x == 10.0 && out.linear_velocity.z == -3.0 );
        check( out.angular_velocity.y == 0.5 );
    }

    // ---- RigidBody, at rest: the untaken branch reads as zero (SPEC §5) ----
    {
        RigidBody in;
        in.position = { 4.0, 5.0, 6.0 };
        in.at_rest = true;
        in.linear_velocity = { 7.0, 8.0, 9.0 }; // untaken — write must not send these

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteRigidBody( ws, in ) );
        ws.Flush();

        RigidBody out;
        out.linear_velocity = { 99.0, 99.0, 99.0 };  // dirty — the read must zero
        out.angular_velocity = { 99.0, 99.0, 99.0 };
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadRigidBody( rs, out ) );
        check( out.at_rest );
        check( out.linear_velocity.x == 0.0 && out.linear_velocity.y == 0.0 && out.linear_velocity.z == 0.0 );
        check( out.angular_velocity.x == 0.0 && out.angular_velocity.y == 0.0 && out.angular_velocity.z == 0.0 );
    }

    // ---- a read CAN fail: truncated stream (SPEC §5, buffer exhaustion) ----
    {
        serialize::ReadStream rs( buffer, 1 );
        RigidBody out;
        check( !ReadRigidBody( rs, out ) );
    }

    // ---- Chat: string(N) framing, interior-null rule (SPEC §4.7) ----
    {
        Chat in;
        std::memcpy( in.text, "hello", 5 );
        in.text_length = 5;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteChat( ws, in ) );
        ws.Flush();

        Chat out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadChat( rs, out ) );
        check( out.text_length == 5 );
        check( std::strcmp( out.text, "hello" ) == 0 );
    }

    // ---- Chat: an interior null is content the read REFUSES (SPEC §4.7) ----
    // A conforming writer cannot produce one (the write side asserts), so the
    // hostile stream is doctored after the write: the wire is 9 length bits
    // riding bytes 0-1, align padding to byte 2, then the text bytes —
    // buffer[2 + k] is text[k]. The vectors pin every branch of the word-wise
    // scan: a null inside a full word, a null in the final byte of an
    // eight-multiple payload, a null the overlapping tail word judges, and a
    // null on the short-string per-byte path.
    {
        Chat in;
        std::memcpy( in.text, "interior null defense s", 23 ); // 2 words + 7-byte tail
        in.text_length = 23;
        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteChat( ws, in ) );
        ws.Flush();

        buffer[2 + 11] = 0; // inside the second full scan word
        {
            Chat out;
            serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
            check( !ReadChat( rs, out ) );
        }
        buffer[2 + 11] = 'x';
        buffer[2 + 22] = 0; // the FINAL payload byte — the overlapping tail word judges it
        {
            Chat out;
            serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
            check( !ReadChat( rs, out ) );
        }

        std::memcpy( in.text, "sixteen bytes ok", 16 ); // two exact words, no tail
        in.text_length = 16;
        serialize::WriteStream ws2( buffer, sizeof( buffer ) );
        check( WriteChat( ws2, in ) );
        ws2.Flush();
        buffer[2 + 15] = 0; // last byte of an eight-multiple payload
        {
            Chat out;
            serialize::ReadStream rs( buffer, ws2.GetBytesProcessed() );
            check( !ReadChat( rs, out ) );
        }

        std::memcpy( in.text, "hello", 5 ); // below one word: the per-byte path
        in.text_length = 5;
        serialize::WriteStream ws3( buffer, sizeof( buffer ) );
        check( WriteChat( ws3, in ) );
        ws3.Flush();
        buffer[2 + 2] = 0;
        {
            Chat out;
            serialize::ReadStream rs( buffer, ws3.GetBytesProcessed() );
            check( !ReadChat( rs, out ) );
        }
    }

    // ---- Chat: the accept neighbors — 0x00 nowhere in [0, length) passes at
    // every word-scan boundary: empty, one byte, one exact word, a word plus
    // a one-byte tail, two exact words (SPEC §4.7) ----
    {
        const int32_t boundary_lengths[] = { 0, 1, 8, 9, 16 };
        for ( int32_t length : boundary_lengths )
        {
            Chat in;
            for ( int32_t i = 0; i < length; i++ )
                in.text[i] = char( 'a' + ( i % 26 ) );
            in.text_length = length;
            serialize::WriteStream ws( buffer, sizeof( buffer ) );
            check( WriteChat( ws, in ) );
            ws.Flush();

            Chat out;
            out.text[length] = 'Z'; // dirty — the reader must supply the terminator
            serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
            check( ReadChat( rs, out ) );
            check( out.text_length == length );
            check( std::memcmp( out.text, in.text, size_t( length ) ) == 0 );
            check( out.text[length] == 0 );
        }
    }

    // ---- InputPacket: counted array of a nested type ----
    {
        InputPacket in;
        in.synchronize_sequence = 7;
        in.current_frame = 123456789ull;
        in.start_frame = 42;
        in.inputs_count = 2;
        in.inputs[0].throttle = 0.5f;
        in.inputs[0].fire = true;
        in.inputs[1].stick_x = -0.25f;
        in.inputs[1].boost = true;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteInputPacket( ws, in ) );
        ws.Flush();

        InputPacket out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadInputPacket( rs, out ) );
        check( out.synchronize_sequence == 7 );
        check( out.current_frame == 123456789ull );
        check( out.inputs_count == 2 );
        check( out.inputs[0].throttle == 0.5f && out.inputs[0].fire );
        check( out.inputs[1].stick_x == -0.25f && out.inputs[1].boost );
        check( !out.inputs[1].fire );
    }

    // ---- Test: ranged integers validate on read (SPEC §5) ----
    {
        Test in;
        in.test_a = 1000;
        in.test_b = 1000; // the range's own max
        in.test_c = 0;
        in.test_d = 500;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteTest( ws, in ) );
        ws.Flush();

        Test out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadTest( rs, out ) );
        check( out.test_a == 1000 && out.test_b == 1000 && out.test_c == 0 && out.test_d == 500 );
    }

    // ---- ShipCreate: the bool-gated flags branch, both ways ----
    {
        ShipCreate in;
        in.ship_type = ShipType::Bomber;
        in.position = { 1000, -2000, 3000 };
        in.has_flags = true;
        in.flags = ShipFlags_Boosting | ShipFlags_Aiming;
        in.team = Team::Blue;
        in.health = 750;
        in.thrust = 55;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteShipCreate( ws, in ) );
        ws.Flush();
        check( golden_wire( "shipcreate_flags", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );

        ShipCreate out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadShipCreate( rs, out ) );
        check( out.ship_type == ShipType::Bomber );
        check( out.position.x == 1000 && out.position.y == -2000 );
        check( out.has_flags && out.flags == ( ShipFlags_Boosting | ShipFlags_Aiming ) );
        check( out.team == Team::Blue && out.health == 750 && out.thrust == 55 );

        in.has_flags = false; // untaken branch: flags must read back zero
        serialize::WriteStream ws2( buffer, sizeof( buffer ) );
        check( WriteShipCreate( ws2, in ) );
        ws2.Flush();
        serialize::ReadStream rs2( buffer, ws2.GetBytesProcessed() );
        check( ReadShipCreate( rs2, out ) );
        check( !out.has_flags && out.flags == 0 );
    }

    // ---- the wire oracle: generated bytes == hand-written classic bytes ----
    {
        RigidBody in;
        in.position = { 1.5, -2.5, 3.25 };
        in.orientation = { 0.1, 0.2, 0.3, 0.9 };
        in.at_rest = false;
        in.linear_velocity = { 10.0, 20.0, -3.0 };
        in.angular_velocity = { 0.25, 0.5, 0.75 };

        oracle::RigidBody twin;
        twin.position = { 1.5, -2.5, 3.25 };
        twin.orientation = { 0.1, 0.2, 0.3, 0.9 };
        twin.atRest = false;
        twin.linearVelocity = { 10.0, 20.0, -3.0 };
        twin.angularVelocity = { 0.25, 0.5, 0.75 };

        alignas( 8 ) uint8_t twin_buffer[2048];
        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        serialize::WriteStream tws( twin_buffer, sizeof( twin_buffer ) );
        check( WriteRigidBody( ws, in ) );
        check( twin.Serialize( tws ) );
        ws.Flush();
        tws.Flush();
        check( ws.GetBytesProcessed() == tws.GetBytesProcessed() );
        check( std::memcmp( buffer, twin_buffer, (size_t) ws.GetBytesProcessed() ) == 0 );
        check( golden_wire( "rigidbody_moving", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );

        // and the at-rest wire, which drops the branch
        in.at_rest = true;
        twin.atRest = true;
        serialize::WriteStream ws2( buffer, sizeof( buffer ) );
        serialize::WriteStream tws2( twin_buffer, sizeof( twin_buffer ) );
        check( WriteRigidBody( ws2, in ) );
        check( twin.Serialize( tws2 ) );
        ws2.Flush();
        tws2.Flush();
        check( ws2.GetBytesProcessed() == tws2.GetBytesProcessed() );
        check( std::memcmp( buffer, twin_buffer, (size_t) ws2.GetBytesProcessed() ) == 0 );
        check( golden_wire( "rigidbody_at_rest", buffer, ws2.GetBytesProcessed(), ws2.GetBitsProcessed() ) );
    }

    // ---- the string framing == classic serialize_string over buffer N + 1 ----
    {
        Chat in;
        std::memcpy( in.text, "wire parity", 11 );
        in.text_length = 11;

        char twin_text[MaxChatLength + 1] = "wire parity";
        alignas( 8 ) uint8_t twin_buffer[2048];

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        serialize::WriteStream tws( twin_buffer, sizeof( twin_buffer ) );
        check( WriteChat( ws, in ) );
        write_string( tws, twin_text, MaxChatLength + 1 );
        ws.Flush();
        tws.Flush();
        check( ws.GetBytesProcessed() == tws.GetBytesProcessed() );
        check( std::memcmp( buffer, twin_buffer, (size_t) ws.GetBytesProcessed() ) == 0 );
        check( golden_wire( "chat", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );
    }

    // ---- the wire constants: const(0xAB, 8) leads, reserved holds zero,
    // and a corrupted constant is REJECTED on read (SPEC §4.3) ----
    {
        ProbeHeader h;
        h.version = 5;
        h.probe_id = 0x1122334455667788ull;
        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteProbeHeader( ws, h ) );
        ws.Flush();
        check( buffer[0] == 0xAB );
        check( golden_wire( "probe_header", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );
        ProbeHeader out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadProbeHeader( rs, out ) );
        check( out.version == 5 && out.probe_id == 0x1122334455667788ull );
        buffer[0] = 0xAC; // corrupt the wire constant
        serialize::ReadStream rs2( buffer, ws.GetBytesProcessed() );
        check( !ReadProbeHeader( rs2, out ) );
    }

    // ---- cross-language pins: the shapes the Go test round-trips get C++
    // wire goldens too, so byte identity is enforced rather than assumed ----
    {
        TestData in;
        in.a = -100;
        in.b = 100;
        in.c = 149;
        in.d = 0x11;
        in.e = 0x22;
        in.f = 0x33;
        in.g = true;
        in.items_count = 3;
        in.items[0] = 0;
        in.items[1] = 128;
        in.items[2] = 255;
        in.float_value = 3.1415926f;
        in.compressed_float_value = 2.5f;
        in.double_value = 1.0 / 3.0;
        in.int8_value = -128;
        in.int16_value = -32768;
        in.uint8_value = 255;
        in.uint16_value = 65535;
        in.uint32_value = 4294967295u;
        in.uint64_value = 18446744073709551615ull;
        in.int64_full = ( -9223372036854775807ll - 1 );
        in.int64_range = -999999999999ll;
        for ( int i = 0; i < 17; i++ )
        {
            in.fixed_bytes[i] = (uint8_t) ( i * 3 );
        }
        std::memcpy( in.text, "the quick brown fox", 19 );
        in.text_length = 19;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteTestData( ws, in ) );
        ws.Flush();
        check( golden_wire( "testdata", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );

        TestData out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadTestData( rs, out ) );
        check( out.int8_value == -128 && out.int16_value == -32768 );
        check( out.int64_full == ( -9223372036854775807ll - 1 ) );
        check( out.uint64_value == 18446744073709551615ull );
    }

    // ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
    // 0.005 over [0,10]@0.01 quantizes to 1 under the float32 two-rounding
    // law; a fused build says 0 and a double build says 0. -4.8585 over
    // [-5,5]@0.001 quantizes to 142; a double build says 141. Values found
    // by sweeping the ranges — the on-quantum 2.5 in TestData above cannot
    // see any of this, which is why these exist.
    {
        CompressedProbe in;
        in.boundary = 0.005f;
        in.offset = -4.8585f;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteCompressedProbe( ws, in ) );
        ws.Flush();
        check( golden_wire( "compressed_probe", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );

        CompressedProbe out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadCompressedProbe( rs, out ) );
        check( out.boundary == 1.0f / 1000.0f * 10.0f );          // integer 1 reconstructed
        check( out.offset == 142.0f / 10000.0f * 10.0f - 5.0f );  // integer 142 reconstructed

        // the discrimination property itself is asserted, so the vectors
        // cannot quietly stop discriminating: the fused and double forms
        // must disagree with the pinned law on these exact values
        {
            volatile float boundary_n = ( 0.005f - 0.0f ) / 10.0f;
            volatile float boundary_scaled = boundary_n * 1000.0f;
            check( (uint32_t) std::floor( boundary_scaled + 0.5f ) == 1 );                          // the law: float32, two roundings
            check( (uint32_t) std::floor( std::fma( (float) boundary_n, 1000.0f, 0.5f ) ) == 0 );   // a fused build diverges
            check( (uint32_t) std::floor( (double) boundary_n * 1000.0 + 0.5 ) == 0 );              // a double build diverges

            volatile float offset_n = ( -4.8585f - -5.0f ) / 10.0f;
            volatile float offset_scaled = offset_n * 10000.0f;
            check( (uint32_t) std::floor( offset_scaled + 0.5f ) == 142 );                          // the law
            check( (uint32_t) std::floor( (double) offset_n * 10000.0 + 0.5 ) == 141 );             // a double build diverges
        }
    }

    // ---- the string UTF-8 contract's validator can FAIL (SPEC §4.7) ----
    // string(N) payloads are well-formed UTF-8 by contract, writer-trusted,
    // debug-asserted through schema_utf8_valid. The enforcement predicate is
    // proven able to reject each malformation class — an assert whose
    // predicate cannot fail is no assert at all.
    {
        check( schema_utf8_valid( (const uint8_t *) "plain ascii", 11 ) );
        check( schema_utf8_valid( (const uint8_t *) "h\xC3\xA9llo", 6 ) );            // 2-byte é
        check( schema_utf8_valid( (const uint8_t *) "\xE2\x82\xAC", 3 ) );            // 3-byte €
        check( schema_utf8_valid( (const uint8_t *) "\xF0\x9F\x9A\x80", 4 ) );        // 4-byte astral
        check( schema_utf8_valid( (const uint8_t *) "", 0 ) );                        // empty is well-formed
        check( !schema_utf8_valid( (const uint8_t *) "\xFF", 1 ) );                   // no such lead byte
        check( !schema_utf8_valid( (const uint8_t *) "\x80", 1 ) );                   // bare continuation
        check( !schema_utf8_valid( (const uint8_t *) "ok\xC3", 3 ) );                 // truncated sequence
        check( !schema_utf8_valid( (const uint8_t *) "\xC0\xAF", 2 ) );               // overlong slash
        check( !schema_utf8_valid( (const uint8_t *) "\xED\xA0\x80", 3 ) );           // encoded surrogate
        check( !schema_utf8_valid( (const uint8_t *) "\xF4\x90\x80\x80", 4 ) );       // above U+10FFFF
        check( !schema_utf8_valid( (const uint8_t *) "\xE0\x80\xA0", 3 ) );           // overlong 3-byte
    }
    {
        InputPacket in;
        in.synchronize_sequence = 7;
        in.current_frame = 123456789ull;
        in.start_frame = 123456780ull;
        in.inputs_count = 2;
        in.inputs[0].throttle = 0.5f;
        in.inputs[0].fire = true;
        in.inputs[1].stick_x = -0.25f;
        in.inputs[1].boost = true;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteInputPacket( ws, in ) );
        ws.Flush();
        check( golden_wire( "inputpacket", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );
    }
    {
        ProbeBits in;
        in.small = 0x1FF;
        in.boundary = 0x1FFFFFFFFull;
        in.wide = 0xFEDCBA9876543210ull;
        in.sensor = 4294967295u;
        in.nonce = 18446744073709551615ull;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteProbeBits( ws, in ) );
        ws.Flush();
        check( golden_wire( "probebits", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );
    }

    // ---- ProbeCollider: first-class one-of (SPEC §4.8) — construction is
    // None, round trip through a selected arm, a None arm beside it, an
    // array of unions, the wire golden, and the refusal negative controls ----
    {
        static_assert( std::is_trivially_copyable<ProbeShape>::value,
                       "the union representation is trivially copyable (SPEC §4.8)" );
        ProbeCollider in;
        check( in.shape.type == ProbeShapeType::None ); // constructed as the empty union
        check( ProbeShapeMaxBits == 2 + 16 );           // tag + the largest arm (SPEC §4.8)

        in.armor = 7;
        in.shape.type = ProbeShapeType::Slab;
        in.shape.slab = ProbeSlab{};
        in.shape.slab.width = 42;
        in.shape.slab.height = 9;
        // in.backup stays None — the empty arm costs the tag bits only
        in.extras_count = 1;
        in.extras[0].type = ProbeShapeType::Ring;
        in.extras[0].ring = ProbeRing{};
        in.extras[0].ring.radius = 777;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteProbeCollider( ws, in ) );
        ws.Flush();
        check( golden_wire( "probecollider", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );

        ProbeCollider out;
        out.backup.type = ProbeShapeType::Ring; // dirty — the read must restore None
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadProbeCollider( rs, out ) );
        check( out.armor == 7 );
        check( out.shape.type == ProbeShapeType::Slab );
        check( out.shape.slab.width == 42 && out.shape.slab.height == 9 );
        check( out.backup.type == ProbeShapeType::None );
        check( out.extras_count == 1 );
        check( out.extras[0].type == ProbeShapeType::Ring );
        check( out.extras[0].ring.radius == 777 );

        // NEGATIVE CONTROL — perturb the tag: the shape tag rides 2 bits at
        // bit offset 8 (after the armor byte), range [0, 2]; forcing both
        // bits makes it 3, and the reader must refuse (SPEC §4.8)
        alignas( 8 ) static uint8_t corrupt[2048 + 8]; // + 8: the reader loads 64-bit windows
        memcpy( corrupt, buffer, (size_t) ws.GetBytesProcessed() );
        corrupt[1] |= 0x03;
        {
            serialize::ReadStream crs( corrupt, ws.GetBytesProcessed() );
            ProbeCollider bad;
            check( !ReadProbeCollider( crs, bad ) );
        }

        // NEGATIVE CONTROL — corrupt the selected arm's payload: width rides
        // 7 bits at bit offset 10 with range [0, 100]; forcing all seven
        // bits decodes 127, above the range, and the reader must refuse
        memcpy( corrupt, buffer, (size_t) ws.GetBytesProcessed() );
        corrupt[1] |= 0xFC;
        corrupt[2] |= 0x01;
        {
            serialize::ReadStream crs( corrupt, ws.GetBytesProcessed() );
            ProbeCollider bad;
            check( !ReadProbeCollider( crs, bad ) );
        }

        // the write side validates the tag BEFORE it rides (SPEC §4.8):
        // an out-of-set tag writes nothing
        ProbeShape rogue;
        rogue.type = ProbeShapeType( 3 );
        serialize::WriteStream bs( buffer, sizeof( buffer ) );
        check( !WriteProbeShape( bs, rogue ) );
    }
    {
        ProbeArray in; // defaults are the constructed state, transitively
        check( in.samples[0].active && in.samples[1].active );
        check( in.config.retries == -1 && in.config.preferred == Weapon::Railgun );

        in.samples[0].orientation = 90.0f;
        in.samples[0].raw_delta = -5;
        in.samples[0].big_delta = -1234567890123ll;
        in.samples[0].weapon = Weapon::Laser;
        in.samples[0].has_target = true;
        in.samples[0].target_id = 777;
        in.samples[0].samples_count = 1;
        in.samples[0].samples[0] = 42;
        in.samples[1].active = false;
        in.samples[1].orientation = -45.5f;
        in.samples[1].raw_delta = 7;
        in.samples[1].big_delta = 99;
        in.samples[1].idle_ticks = 1000;
        in.samples[1].samples_count = 2;
        in.samples[1].samples[0] = 7;
        in.samples[1].samples[1] = 8;
        in.config.retries = 3;
        in.config.preferred = Weapon::Missile;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteProbeArray( ws, in ) );
        ws.Flush();
        check( golden_wire( "probearray", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );

        ProbeArray out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadProbeArray( rs, out ) );
        check( !out.samples[1].active && out.samples[1].idle_ticks == 1000 );
        // the untaken active-branch fields of samples[1] read as ZERO (SPEC §5)
        check( out.samples[1].weapon == Weapon::None && !out.samples[1].has_target );
        check( out.config.retries == 3 && out.config.preferred == Weapon::Missile );
    }

    // ---- EnumName: debug names for every enum value, out-of-set included ----
    {
        check( strcmp( EnumName( Weapon::Laser ), "Laser" ) == 0 );
        check( strcmp( EnumName( Weapon::None ), "None" ) == 0 );
        check( strcmp( EnumName( Weapon( 200 ) ), "???" ) == 0 );
        check( strcmp( EnumName( Team::Blue ), "Blue" ) == 0 ); // overload resolution across enums
    }

    // ---- FlagName / FlagNames: the flags twin — per-bit names and the set
    // renderer ("A|B", "0" empty, unknown high bits as hex) ----
    {
        check( strcmp( FlagNameShipFlags( 0 ), "FiringLaser" ) == 0 );
        check( strcmp( FlagNameShipFlags( 9 ), "???" ) == 0 );
        char buffer[ShipFlagsNamesMax];
        check( strcmp( FlagNamesShipFlags( 0, buffer, sizeof( buffer ) ), "0" ) == 0 );
        check( strcmp( FlagNamesShipFlags( ShipFlags_FiringLaser | ShipFlags_Braking, buffer, sizeof( buffer ) ), "FiringLaser|Braking" ) == 0 );
        check( strcmp( FlagNamesShipFlags( ShipFlags_Aiming | ( 1ull << 63 ), buffer, sizeof( buffer ) ), "Aiming|0x8000000000000000" ) == 0 );
    }

    // ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
    //
    // Twelve shapes the corpus's realistic types cannot reach, written back
    // to back into ONE stream and pinned as one golden. One stream because
    // the point of these types is what an emitter does with a whole message
    // body of a given arrangement, not how they compose: twelve one-message
    // goldens would be twelve times the harness in nine languages for the
    // same bytes. The read-back below gives per-type attribution when the
    // byte compare fails.
    {
        Vec2 vec2;
        vec2.x = 1.5;
        vec2.y = -2.25;

        SpanF64 span_f64;
        span_f64.values[0] = 3.5;
        span_f64.values[1] = -4.75;

        SpanU64 span_u64;
        span_u64.values[0] = 0xDEADBEEFCAFEBABEull;
        span_u64.values[1] = 1;

        SpanI64 span_i64;
        span_i64.values[0] = -1234567890123ll;
        span_i64.values[1] = 42;

        SpanOne span_one;
        span_one.values[0] = 0x0123456789ABCDEFull;

        SpanChunk span_chunk;
        span_chunk.values[0] = 0x1111;
        span_chunk.values[1] = 0x2222;
        span_chunk.values[2] = 0x3333;
        span_chunk.values[3] = 0x4444;

        SpanTail span_tail;
        span_tail.values[0] = 6.125;
        span_tail.values[1] = -7.0;
        span_tail.tail = 0xFEEDFACEu;

        SpanTwice span_twice;
        span_twice.a[0] = 8.5;
        span_twice.a[1] = 9.5;
        span_twice.b[0] = -10.5;
        span_twice.b[1] = -11.5;

        Trio trio;
        trio.a = 0xABCDE;
        trio.b = 0x12345;
        trio.c = 0xFFFFF;

        TrioSole trio_sole;
        trio_sole.inner.a = 1;
        trio_sole.inner.b = 2;
        trio_sole.inner.c = 3;

        TrioFirst trio_first;
        trio_first.inner.a = 0xAAAAA;
        trio_first.inner.b = 0x55555;
        trio_first.inner.c = 0xF0F0F;
        trio_first.trailer = 0xBEEF;

        TrioStraddle straddle;
        straddle.pad0 = 0x0011223344556677ull;
        straddle.pad1 = 0x8899AABBCCDDEEFFull;
        straddle.pad2 = 0xFFFFFFFFFFFFFFFFull;
        straddle.pad3 = 0;
        straddle.pad4 = 0x123456789ABCDEF0ull;
        straddle.pad5 = 0xABCDEFu;
        straddle.inner.a = 0x11111;
        straddle.inner.b = 0x22222;
        straddle.inner.c = 0x33333;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteVec2( ws, vec2 ) );
        check( WriteSpanF64( ws, span_f64 ) );
        check( WriteSpanU64( ws, span_u64 ) );
        check( WriteSpanI64( ws, span_i64 ) );
        check( WriteSpanOne( ws, span_one ) );
        check( WriteSpanChunk( ws, span_chunk ) );
        check( WriteSpanTail( ws, span_tail ) );
        check( WriteSpanTwice( ws, span_twice ) );
        check( WriteTrio( ws, trio ) );
        check( WriteTrioSole( ws, trio_sole ) );
        check( WriteTrioFirst( ws, trio_first ) );
        check( WriteTrioStraddle( ws, straddle ) );

        // Each shape's own width, stated so a doubled or dropped element is
        // named rather than smeared across one total. A fixed scalar array
        // emitted twice is exactly the defect these types exist to catch, and
        // the total is taken BEFORE Flush so no padding hides it.
        check( Vec2MaxBits == 128 && SpanF64MaxBits == 128 && SpanU64MaxBits == 128 );
        check( SpanI64MaxBits == 128 && SpanOneMaxBits == 64 && SpanChunkMaxBits == 64 );
        check( SpanTailMaxBits == 160 && SpanTwiceMaxBits == 256 && TrioMaxBits == 64 );
        check( TrioSoleMaxBits == 64 && TrioFirstMaxBits == 80 && TrioStraddleMaxBits == 408 );
        check( ws.GetBitsProcessed() == 128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408 );

        ws.Flush();
        check( golden_wire( "degenerate", buffer, ws.GetBytesProcessed(), ws.GetBitsProcessed() ) );

        Vec2 r_vec2;
        SpanF64 r_span_f64;
        SpanU64 r_span_u64;
        SpanI64 r_span_i64;
        SpanOne r_span_one;
        SpanChunk r_span_chunk;
        SpanTail r_span_tail;
        SpanTwice r_span_twice;
        Trio r_trio;
        TrioSole r_trio_sole;
        TrioFirst r_trio_first;
        TrioStraddle r_straddle;

        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadVec2( rs, r_vec2 ) );
        check( ReadSpanF64( rs, r_span_f64 ) );
        check( ReadSpanU64( rs, r_span_u64 ) );
        check( ReadSpanI64( rs, r_span_i64 ) );
        check( ReadSpanOne( rs, r_span_one ) );
        check( ReadSpanChunk( rs, r_span_chunk ) );
        check( ReadSpanTail( rs, r_span_tail ) );
        check( ReadSpanTwice( rs, r_span_twice ) );
        check( ReadTrio( rs, r_trio ) );
        check( ReadTrioSole( rs, r_trio_sole ) );
        check( ReadTrioFirst( rs, r_trio_first ) );
        check( ReadTrioStraddle( rs, r_straddle ) );

        check( r_vec2.x == 1.5 && r_vec2.y == -2.25 );
        check( r_span_f64.values[0] == 3.5 && r_span_f64.values[1] == -4.75 );
        check( r_span_u64.values[0] == 0xDEADBEEFCAFEBABEull && r_span_u64.values[1] == 1 );
        check( r_span_i64.values[0] == -1234567890123ll && r_span_i64.values[1] == 42 );
        check( r_span_one.values[0] == 0x0123456789ABCDEFull );
        check( r_span_chunk.values[0] == 0x1111 && r_span_chunk.values[3] == 0x4444 );
        check( r_span_tail.values[0] == 6.125 && r_span_tail.values[1] == -7.0 && r_span_tail.tail == 0xFEEDFACEu );
        check( r_span_twice.a[0] == 8.5 && r_span_twice.a[1] == 9.5 );
        check( r_span_twice.b[0] == -10.5 && r_span_twice.b[1] == -11.5 );
        check( r_trio.a == 0xABCDE && r_trio.b == 0x12345 && r_trio.c == 0xFFFFF );
        check( r_trio_sole.inner.a == 1 && r_trio_sole.inner.b == 2 && r_trio_sole.inner.c == 3 );
        check( r_trio_first.inner.a == 0xAAAAA && r_trio_first.inner.c == 0xF0F0F && r_trio_first.trailer == 0xBEEF );
        check( r_straddle.pad0 == 0x0011223344556677ull && r_straddle.pad4 == 0x123456789ABCDEF0ull );
        check( r_straddle.pad5 == 0xABCDEFu );
        check( r_straddle.inner.a == 0x11111 && r_straddle.inner.b == 0x22222 && r_straddle.inner.c == 0x33333 );
    }

    // ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----
    //
    // Degenerate.schema's standing property is that every type in it is a
    // whole number of bytes. That is load bearing for what it catches, and
    // it is also a ceiling: no clause boundary inside it ever lands mid-byte,
    // so an emitter that groups array elements picks the same group size on
    // the write and the read side of every type in the file. These two units
    // are chosen so it does not. At 13 bits a write clause takes four
    // elements (52 bits, the whole group budget) and a read clause three (39,
    // inside the 49-bit window): the two sides disagree, and both boundaries
    // fall mid-byte. Joins.schema does the same to the static-offset state
    // machine — arms that agree and disagree on width, an absent arm, a
    // branch inside a branch, an align that regains staticness on one path
    // only, an array that gives it up on one path only, and unions of
    // unequal arms at mid-byte offsets.
    //
    // Unlike Degenerate, these shapes are NOT byte-aligned, so a single
    // stream would not equal a concatenation of the shapes written alone —
    // and the Elixir emitter returns each message as its own binary from bit
    // zero, so it has no way to write a shared stream at all. Every shape is
    // therefore written to its OWN stream and flushed, and the golden is the
    // concatenation of those. Every leg can reproduce that, and each shape's
    // bytes stay individually attributable.
    //
    // Each emit states the shape's own width, so a doubled or dropped clause
    // is named rather than smeared into one total.
    {
        alignas( 8 ) uint8_t shape[64];  // one shape's own stream
        alignas( 8 ) uint8_t stream[1024];
        int64_t stream_len = 0;

        auto emit = [&]( int64_t expect_bits, auto && fn ) -> bool
        {
            serialize::WriteStream ws( shape, sizeof( shape ) );
            if ( !fn( ws ) ) return false;
            if ( ws.GetBitsProcessed() != expect_bits ) return false;
            ws.Flush();
            const int64_t n = ws.GetBytesProcessed();
            if ( n != ( expect_bits + 7 ) / 8 ) return false;
            if ( stream_len + n > (int64_t) sizeof( stream ) ) return false;
            memcpy( stream + stream_len, shape, (size_t) n );
            stream_len += n;
            return true;
        };

        int64_t read_off = 0;
        auto consume = [&]( int64_t expect_bits, auto && fn ) -> bool
        {
            const int64_t n = ( expect_bits + 7 ) / 8;
            alignas( 8 ) uint8_t slice[64] = {}; // read allocations extend past the data
            memcpy( slice, stream + read_off, (size_t) n );
            serialize::ReadStream rs( slice, n );
            if ( !fn( rs ) ) return false;
            read_off += n;
            return true;
        };

        // ---- Clauses.schema ----

        const int w13_counts[] = { 0, 1, 3, 4, 5, 7, 12 };
        for ( int c : w13_counts )
        {
            W13 v;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) v.items[i] = (uint16_t) ( 8191 - i * 733 );
            check( emit( 4 + 13 * c, [&]( serialize::WriteStream & ws ) { return WriteW13( ws, v ); } ) );
        }

        const int w17_counts[] = { 0, 1, 2, 3, 4, 9 };
        for ( int c : w17_counts )
        {
            W17 v;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) v.items[i] = (uint32_t) ( 131071 - i * 11117 );
            check( emit( 4 + 17 * c, [&]( serialize::WriteStream & ws ) { return WriteW17( ws, v ); } ) );
        }

        const int w26_counts[] = { 0, 1, 2, 3, 6 };
        for ( int c : w26_counts )
        {
            W26 v;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) v.items[i] = (uint32_t) ( 67108863 - i * 5555555 );
            check( emit( 3 + 26 * c, [&]( serialize::WriteStream & ws ) { return WriteW26( ws, v ); } ) );
        }

        const int w1_counts[] = { 0, 1, 3, 4, 5, 20 };
        for ( int c : w1_counts )
        {
            W1 v;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) v.items[i] = (uint8_t) ( i % 2 );
            check( emit( 5 + 1 * c, [&]( serialize::WriteStream & ws ) { return WriteW1( ws, v ); } ) );
        }

        for ( int c = 0; c <= 3; c++ )
        {
            W52 v;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) v.items[i] = 4503599627370495ull - (uint64_t) i * 123456789ull;
            check( emit( 2 + 52 * c, [&]( serialize::WriteStream & ws ) { return WriteW52( ws, v ); } ) );
        }

        for ( int c = 0; c <= 3; c++ )
        {
            W50 v;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) v.items[i] = 1125899906842623ull - (uint64_t) i * 987654321ull;
            check( emit( 2 + 50 * c, [&]( serialize::WriteStream & ws ) { return WriteW50( ws, v ); } ) );
        }

        {
            F13 v;
            for ( int i = 0; i < 7; i++ ) v.items[i] = (uint16_t) ( 8191 - i * 911 );
            check( emit( 91, [&]( serialize::WriteStream & ws ) { return WriteF13( ws, v ); } ) );
        }

        const int tri_counts[] = { 0, 1, 3, 4, 5, 10 };
        for ( int c : tri_counts )
        {
            ArrTri3 v;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) { v.items[i].a = (uint32_t) ( i % 2 ); v.items[i].b = (uint32_t) ( i % 4 ); }
            check( emit( 4 + 3 * c, [&]( serialize::WriteStream & ws ) { return WriteArrTri3( ws, v ); } ) );
        }

        {
            ArrEleven v;
            for ( int i = 0; i < 9; i++ ) { v.items[i].a = (uint32_t) ( i % 8 ); v.items[i].b = (uint32_t) ( 255 - i * 17 ); }
            check( emit( 99, [&]( serialize::WriteStream & ws ) { return WriteArrEleven( ws, v ); } ) );
        }

        // lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag
        {
            HoldsEmptyUnion none;
            none.lead = 21;
            none.tail = 99;
            check( emit( 14, [&]( serialize::WriteStream & ws ) { return WriteHoldsEmptyUnion( ws, none ); } ) );

            HoldsEmptyUnion arm_a;
            arm_a.lead = 21;
            arm_a.tail = 99;
            arm_a.u.type = EmptyUnionType::A;
            arm_a.u.a = EmptyA{};
            check( emit( 14, [&]( serialize::WriteStream & ws ) { return WriteHoldsEmptyUnion( ws, arm_a ); } ) );

            HoldsEmptyUnion arm_b;
            arm_b.lead = 21;
            arm_b.tail = 99;
            arm_b.u.type = EmptyUnionType::B;
            arm_b.u.b = EmptyB{};
            check( emit( 14, [&]( serialize::WriteStream & ws ) { return WriteHoldsEmptyUnion( ws, arm_b ); } ) );
        }

        // Strs: lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s
        // bytes, b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail.
        // The 5-bit lead is what puts the align at a non-zero offset.
        {
            Strs empty;
            empty.lead = 21;
            empty.tail = 5;
            check( emit( 27, [&]( serialize::WriteStream & ws ) { return WriteStrs( ws, empty ); } ) );

            Strs full;
            full.lead = 21;
            full.tail = 5;
            memcpy( full.s, "abcdefgh", 8 );
            full.s_length = 8;
            for ( int i = 0; i < 8; i++ ) full.b[i] = (uint8_t) ( 0xF0 + i );
            full.b_length = 8;
            check( emit( 155, [&]( serialize::WriteStream & ws ) { return WriteStrs( ws, full ); } ) );

            Strs part;
            part.lead = 21;
            part.tail = 5;
            memcpy( part.s, "xyz", 3 );
            part.s_length = 3;
            for ( int i = 0; i < 3; i++ ) part.b[i] = (uint8_t) ( i + 1 );
            part.b_length = 3;
            check( emit( 75, [&]( serialize::WriteStream & ws ) { return WriteStrs( ws, part ); } ) );
        }

        for ( int c = 0; c <= 4; c++ )
        {
            ArrNested v;
            v.lead = 21;
            v.tail = 5;
            v.items_count = c;
            for ( int i = 0; i < c; i++ ) { v.items[i].a = (uint32_t) ( i % 8 ); v.items[i].b = (uint32_t) ( 200 - i * 7 ); }
            check( emit( 11 + 11 * c, [&]( serialize::WriteStream & ws ) { return WriteArrNested( ws, v ); } ) );
        }

        {
            Sole v;
            v.only = 5555;
            check( emit( 13, [&]( serialize::WriteStream & ws ) { return WriteSole( ws, v ); } ) );
        }

        check( W13MaxBits == 160 && W17MaxBits == 157 && W26MaxBits == 159 && W1MaxBits == 25 );
        check( W52MaxBits == 158 && W50MaxBits == 152 && F13MaxBits == 91 && ArrTri3MaxBits == 34 );
        check( ArrElevenMaxBits == 99 && HoldsEmptyUnionMaxBits == 14 && StrsMaxBits == 158 );
        check( ArrNestedMaxBits == 55 && SoleMaxBits == 13 );

        check( golden_wire( "clauses", stream, stream_len, -1 ) );

        // Read each shape back out of its own slice. A clause that decodes a
        // different number of elements than the writer encoded shows up here
        // even where the byte compare above happens to pass.
        for ( int c : w13_counts )
        {
            W13 r;
            check( consume( 4 + 13 * c, [&]( serialize::ReadStream & rs ) { return ReadW13( rs, r ); } ) );
            check( r.items_count == c );
            for ( int i = 0; i < c; i++ ) check( r.items[i] == (uint16_t) ( 8191 - i * 733 ) );
        }
        for ( int c : w17_counts )
        {
            W17 r;
            check( consume( 4 + 17 * c, [&]( serialize::ReadStream & rs ) { return ReadW17( rs, r ); } ) );
            check( r.items_count == c );
            for ( int i = 0; i < c; i++ ) check( r.items[i] == (uint32_t) ( 131071 - i * 11117 ) );
        }
        for ( int c : w26_counts )
        {
            W26 r;
            check( consume( 3 + 26 * c, [&]( serialize::ReadStream & rs ) { return ReadW26( rs, r ); } ) );
            check( r.items_count == c );
            for ( int i = 0; i < c; i++ ) check( r.items[i] == (uint32_t) ( 67108863 - i * 5555555 ) );
        }
        for ( int c : w1_counts )
        {
            W1 r;
            check( consume( 5 + 1 * c, [&]( serialize::ReadStream & rs ) { return ReadW1( rs, r ); } ) );
            check( r.items_count == c );
            for ( int i = 0; i < c; i++ ) check( r.items[i] == (uint8_t) ( i % 2 ) );
        }
        for ( int c = 0; c <= 3; c++ )
        {
            W52 r;
            check( consume( 2 + 52 * c, [&]( serialize::ReadStream & rs ) { return ReadW52( rs, r ); } ) );
            check( r.items_count == c );
            for ( int i = 0; i < c; i++ ) check( r.items[i] == 4503599627370495ull - (uint64_t) i * 123456789ull );
        }
        for ( int c = 0; c <= 3; c++ )
        {
            W50 r;
            check( consume( 2 + 50 * c, [&]( serialize::ReadStream & rs ) { return ReadW50( rs, r ); } ) );
            check( r.items_count == c );
            for ( int i = 0; i < c; i++ ) check( r.items[i] == 1125899906842623ull - (uint64_t) i * 987654321ull );
        }
        {
            F13 r;
            check( consume( 91, [&]( serialize::ReadStream & rs ) { return ReadF13( rs, r ); } ) );
            for ( int i = 0; i < 7; i++ ) check( r.items[i] == (uint16_t) ( 8191 - i * 911 ) );
        }
        for ( int c : tri_counts )
        {
            ArrTri3 r;
            check( consume( 4 + 3 * c, [&]( serialize::ReadStream & rs ) { return ReadArrTri3( rs, r ); } ) );
            check( r.items_count == c );
            for ( int i = 0; i < c; i++ ) check( r.items[i].a == (uint32_t) ( i % 2 ) && r.items[i].b == (uint32_t) ( i % 4 ) );
        }
        {
            ArrEleven r;
            check( consume( 99, [&]( serialize::ReadStream & rs ) { return ReadArrEleven( rs, r ); } ) );
            for ( int i = 0; i < 9; i++ ) check( r.items[i].a == (uint32_t) ( i % 8 ) && r.items[i].b == (uint32_t) ( 255 - i * 17 ) );
        }
        {
            HoldsEmptyUnion r_none, r_a, r_b;
            check( consume( 14, [&]( serialize::ReadStream & rs ) { return ReadHoldsEmptyUnion( rs, r_none ); } ) );
            check( consume( 14, [&]( serialize::ReadStream & rs ) { return ReadHoldsEmptyUnion( rs, r_a ); } ) );
            check( consume( 14, [&]( serialize::ReadStream & rs ) { return ReadHoldsEmptyUnion( rs, r_b ); } ) );
            check( r_none.u.type == EmptyUnionType::None && r_none.lead == 21 && r_none.tail == 99 );
            check( r_a.u.type == EmptyUnionType::A && r_b.u.type == EmptyUnionType::B );
        }
        {
            Strs r_empty, r_full, r_part;
            check( consume( 27, [&]( serialize::ReadStream & rs ) { return ReadStrs( rs, r_empty ); } ) );
            check( consume( 155, [&]( serialize::ReadStream & rs ) { return ReadStrs( rs, r_full ); } ) );
            check( consume( 75, [&]( serialize::ReadStream & rs ) { return ReadStrs( rs, r_part ); } ) );
            check( r_empty.s_length == 0 && r_empty.b_length == 0 && r_empty.lead == 21 && r_empty.tail == 5 );
            check( r_full.s_length == 8 && strcmp( r_full.s, "abcdefgh" ) == 0 && r_full.b_length == 8 && r_full.b[7] == 0xF7 );
            check( r_part.s_length == 3 && strcmp( r_part.s, "xyz" ) == 0 && r_part.b_length == 3 && r_part.b[2] == 3 );
        }
        for ( int c = 0; c <= 4; c++ )
        {
            ArrNested r;
            check( consume( 11 + 11 * c, [&]( serialize::ReadStream & rs ) { return ReadArrNested( rs, r ); } ) );
            check( r.items_count == c && r.lead == 21 && r.tail == 5 );
            for ( int i = 0; i < c; i++ ) check( r.items[i].a == (uint32_t) ( i % 8 ) && r.items[i].b == (uint32_t) ( 200 - i * 7 ) );
        }
        {
            Sole r;
            check( consume( 13, [&]( serialize::ReadStream & rs ) { return ReadSole( rs, r ); } ) );
            check( r.only == 5555 );
        }
        check( read_off == stream_len ); // the reads consume the whole golden

        // ---- Joins.schema ----

        stream_len = 0;
        read_off = 0;

        for ( int f = 0; f < 2; f++ )
        {
            ArmsAgree v;
            // the arms agree on WIDTH but not on value, so a join that keeps
            // the wrong arm is a value mismatch and not just a width one
            v.lead = 21; v.flag = f != 0; v.a = 1234; v.b = 1500; v.tail = 99;
            check( emit( 24, [&]( serialize::WriteStream & ws ) { return WriteArmsAgree( ws, v ); } ) );

            ArmsDisagree d;
            d.lead = 21; d.flag = f != 0; d.a = 1234; d.b = 5; d.tail = 99;
            check( emit( f ? 24 : 16, [&]( serialize::WriteStream & ws ) { return WriteArmsDisagree( ws, d ); } ) );

            ArmEmpty e;
            e.lead = 21; e.flag = f != 0; e.a = 456789; e.tail = 99;
            check( emit( f ? 32 : 13, [&]( serialize::WriteStream & ws ) { return WriteArmEmpty( ws, e ); } ) );

            ArmAlign s;
            s.lead = 21; s.flag = f != 0; memcpy( s.s, "abcd", 4 ); s.s_length = 4; s.b = 1000; s.tail = 99;
            check( emit( f ? 55 : 23, [&]( serialize::WriteStream & ws ) { return WriteArmAlign( ws, s ); } ) );

            ArmAlign z;
            z.lead = 21; z.flag = f != 0; z.s_length = 0; z.b = 1000; z.tail = 99;
            check( emit( 23, [&]( serialize::WriteStream & ws ) { return WriteArmAlign( ws, z ); } ) );
        }

        for ( int o = 0; o < 2; o++ )
        {
            for ( int i = 0; i < 2; i++ )
            {
                ArmsNested v;
                v.lead = 5; v.outer = o != 0; v.inner = i != 0;
                v.x = 500000000; v.y = 17; v.z = 4000; v.tail = 33;
                check( emit( o ? ( i ? 40 : 16 ) : 23, [&]( serialize::WriteStream & ws ) { return WriteArmsNested( ws, v ); } ) );
            }
        }

        for ( int f = 0; f < 2; f++ )
        {
            for ( int c = 0; c <= 3; c++ )
            {
                ArmArray v;
                v.lead = 21; v.flag = f != 0; v.items_count = c; v.b = 300; v.tail = 99;
                for ( int i = 0; i < c; i++ ) v.items[i] = (uint16_t) ( 8191 - i * 777 );
                check( emit( f ? 15 + 13 * c : 22, [&]( serialize::WriteStream & ws ) { return WriteArmArray( ws, v ); } ) );
            }
        }

        // lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits
        {
            HoldsUneven none;
            none.lead = 21; none.tail = 1500;
            check( emit( 18, [&]( serialize::WriteStream & ws ) { return WriteHoldsUneven( ws, none ); } ) );

            HoldsUneven narrow;
            narrow.lead = 21; narrow.tail = 1500;
            narrow.u.type = UnevenType::Narrow;
            narrow.u.narrow = Narrow{};
            narrow.u.narrow.n = 5;
            check( emit( 21, [&]( serialize::WriteStream & ws ) { return WriteHoldsUneven( ws, narrow ); } ) );

            HoldsUneven wide;
            wide.lead = 21; wide.tail = 1500;
            wide.u.type = UnevenType::Wide;
            wide.u.wide = Wide{};
            wide.u.wide.w = 123456789012ull;
            check( emit( 55, [&]( serialize::WriteStream & ws ) { return WriteHoldsUneven( ws, wide ); } ) );
        }

        // alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37)
        const int64_t uneven_item_bits[] = { 0, 5, 44, 49 };
        for ( int c = 0; c <= 3; c++ )
        {
            ArrUneven v;
            v.lead = 21; v.tail = 5; v.items_count = c;
            for ( int i = 0; i < c; i++ )
            {
                if ( i % 2 == 0 )
                {
                    v.items[i].type = UnevenType::Narrow;
                    v.items[i].narrow = Narrow{};
                    v.items[i].narrow.n = (uint32_t) ( i % 8 );
                }
                else
                {
                    v.items[i].type = UnevenType::Wide;
                    v.items[i].wide = Wide{};
                    v.items[i].wide.w = 99887766554ull + (uint64_t) i;
                }
            }
            check( emit( 10 + uneven_item_bits[c], [&]( serialize::WriteStream & ws ) { return WriteArrUneven( ws, v ); } ) );
        }

        // lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s,
        // then a 32 + 29 + 19 + 4 static run after the align regains it
        for ( int c = 0; c <= 3; c++ )
        {
            for ( int sl = 0; sl <= 4; sl += 4 )
            {
                RegainAfterAlign v;
                v.lead = 21; v.items_count = c; v.s_length = sl;
                if ( sl ) memcpy( v.s, "wxyz", 4 );
                for ( int i = 0; i < c; i++ ) v.items[i] = (uint16_t) ( 8191 - i * 999 );
                v.p = 0xDEADBEEFu; v.q = ( 1u << 29 ) - 7; v.r = ( 1u << 19 ) - 3; v.tail = 9;
                const int64_t after_align = ( ( 5 + 2 + 13 * c + 3 ) + 7 ) / 8 * 8;
                check( emit( after_align + 8 * sl + 84, [&]( serialize::WriteStream & ws ) { return WriteRegainAfterAlign( ws, v ); } ) );
            }
        }

        check( ArmsAgreeMaxBits == 24 && ArmsDisagreeMaxBits == 24 && ArmEmptyMaxBits == 32 );
        check( ArmsNestedMaxBits == 40 && ArmAlignMaxBits == 55 && ArmArrayMaxBits == 54 );
        check( HoldsUnevenMaxBits == 55 && ArrUnevenMaxBits == 127 && RegainAfterAlignMaxBits == 172 );

        check( golden_wire( "joins", stream, stream_len, -1 ) );

        for ( int f = 0; f < 2; f++ )
        {
            ArmsAgree v;
            check( consume( 24, [&]( serialize::ReadStream & rs ) { return ReadArmsAgree( rs, v ); } ) );
            check( v.lead == 21 && v.flag == ( f != 0 ) && v.tail == 99 );
            // the untaken side reads as zero (SPEC §5)
            check( f ? ( v.a == 1234 && v.b == 0 ) : ( v.b == 1500 && v.a == 0 ) );

            ArmsDisagree d;
            check( consume( f ? 24 : 16, [&]( serialize::ReadStream & rs ) { return ReadArmsDisagree( rs, d ); } ) );
            check( d.lead == 21 && d.tail == 99 );
            check( f ? ( d.a == 1234 && d.b == 0 ) : ( d.b == 5 && d.a == 0 ) );

            ArmEmpty e;
            check( consume( f ? 32 : 13, [&]( serialize::ReadStream & rs ) { return ReadArmEmpty( rs, e ); } ) );
            check( e.lead == 21 && e.tail == 99 );
            check( e.a == ( f ? 456789u : 0u ) );

            ArmAlign s;
            check( consume( f ? 55 : 23, [&]( serialize::ReadStream & rs ) { return ReadArmAlign( rs, s ); } ) );
            check( s.lead == 21 && s.tail == 99 );
            check( f ? ( s.s_length == 4 && strcmp( s.s, "abcd" ) == 0 && s.b == 0 ) : ( s.b == 1000 && s.s_length == 0 ) );

            ArmAlign z;
            check( consume( 23, [&]( serialize::ReadStream & rs ) { return ReadArmAlign( rs, z ); } ) );
            check( z.lead == 21 && z.tail == 99 );
            check( f ? ( z.s_length == 0 && z.b == 0 ) : ( z.b == 1000 ) );
        }

        for ( int o = 0; o < 2; o++ )
        {
            for ( int i = 0; i < 2; i++ )
            {
                ArmsNested v;
                check( consume( o ? ( i ? 40 : 16 ) : 23, [&]( serialize::ReadStream & rs ) { return ReadArmsNested( rs, v ); } ) );
                check( v.lead == 5 && v.tail == 33 && v.outer == ( o != 0 ) );
                if ( o )
                {
                    check( v.inner == ( i != 0 ) && v.z == 0 );
                    check( i ? ( v.x == 500000000u && v.y == 0 ) : ( v.y == 17 && v.x == 0 ) );
                }
                else
                {
                    check( v.z == 4000 && v.x == 0 && v.y == 0 );
                }
            }
        }

        for ( int f = 0; f < 2; f++ )
        {
            for ( int c = 0; c <= 3; c++ )
            {
                ArmArray v;
                check( consume( f ? 15 + 13 * c : 22, [&]( serialize::ReadStream & rs ) { return ReadArmArray( rs, v ); } ) );
                check( v.lead == 21 && v.tail == 99 );
                if ( f )
                {
                    check( v.items_count == c && v.b == 0 );
                    for ( int i = 0; i < c; i++ ) check( v.items[i] == (uint16_t) ( 8191 - i * 777 ) );
                }
                else
                {
                    check( v.b == 300 && v.items_count == 0 );
                }
            }
        }

        {
            HoldsUneven r_none, r_narrow, r_wide;
            check( consume( 18, [&]( serialize::ReadStream & rs ) { return ReadHoldsUneven( rs, r_none ); } ) );
            check( consume( 21, [&]( serialize::ReadStream & rs ) { return ReadHoldsUneven( rs, r_narrow ); } ) );
            check( consume( 55, [&]( serialize::ReadStream & rs ) { return ReadHoldsUneven( rs, r_wide ); } ) );
            check( r_none.u.type == UnevenType::None && r_none.lead == 21 && r_none.tail == 1500 );
            check( r_narrow.u.type == UnevenType::Narrow && r_narrow.u.narrow.n == 5 );
            check( r_wide.u.type == UnevenType::Wide && r_wide.u.wide.w == 123456789012ull );
        }

        for ( int c = 0; c <= 3; c++ )
        {
            ArrUneven v;
            check( consume( 10 + uneven_item_bits[c], [&]( serialize::ReadStream & rs ) { return ReadArrUneven( rs, v ); } ) );
            check( v.items_count == c && v.lead == 21 && v.tail == 5 );
            for ( int i = 0; i < c; i++ )
            {
                if ( i % 2 == 0 )
                {
                    check( v.items[i].type == UnevenType::Narrow && v.items[i].narrow.n == (uint32_t) ( i % 8 ) );
                }
                else
                {
                    check( v.items[i].type == UnevenType::Wide && v.items[i].wide.w == 99887766554ull + (uint64_t) i );
                }
            }
        }

        for ( int c = 0; c <= 3; c++ )
        {
            for ( int sl = 0; sl <= 4; sl += 4 )
            {
                RegainAfterAlign v;
                const int64_t after_align = ( ( 5 + 2 + 13 * c + 3 ) + 7 ) / 8 * 8;
                check( consume( after_align + 8 * sl + 84, [&]( serialize::ReadStream & rs ) { return ReadRegainAfterAlign( rs, v ); } ) );
                check( v.lead == 21 && v.items_count == c && v.s_length == sl );
                check( v.p == 0xDEADBEEFu && v.q == ( 1u << 29 ) - 7 && v.r == ( 1u << 19 ) - 3 && v.tail == 9 );
                for ( int i = 0; i < c; i++ ) check( v.items[i] == (uint16_t) ( 8191 - i * 999 ) );
            }
        }
        check( read_off == stream_len );
    }

    // ---- parallel scatter/gather (Render.schema): threads build render
    // types independently and the result is byte-identical to serial — the
    // relocatable-by-construction property doing real work. flatbuffers
    // could not express this: one block, serial offsets.
    {
        const int NumWorkers = 4;
        const int BlocksPerWorker = 8;
        const int NumBlocks = NumWorkers * BlocksPerWorker;

        auto fill_block = []( RenderBlock & block, int blockIndex )
        {
            new ( &block ) RenderBlock{};
            block.worker_index = (uint32_t) ( blockIndex / BlocksPerWorker );
            block.sprite_count_hint = (uint32_t) ( blockIndex * 3 );
            block.sprites_count = 1 + ( blockIndex % 4 );
            for ( int s = 0; s < block.sprites_count; s++ )
            {
                RenderSprite & sprite = block.sprites[s];
                sprite.sort_key = 0x1000000000000000ull + (uint64_t) blockIndex * 256 + (uint64_t) s;
                sprite.mesh_id = (uint32_t) ( 100 + blockIndex );
                sprite.material_id = (uint32_t) ( 7 + s );
                sprite.layer = (uint8_t) ( blockIndex % 3 );
                sprite.team = ( s % 2 ) ? Team::Red : Team::Blue;
            }
        };

        // 1) raw-struct scatter: workers write disjoint slices of ONE shared
        // array in parallel (the render-blob pattern), vs the same serially
        static RenderBlock parallel_blocks[NumBlocks];
        static RenderBlock serial_blocks[NumBlocks];

        {
            std::thread workers[NumWorkers];
            for ( int w = 0; w < NumWorkers; w++ )
            {
                workers[w] = std::thread( [w, &fill_block]()
                {
                    for ( int b = w * BlocksPerWorker; b < ( w + 1 ) * BlocksPerWorker; b++ )
                    {
                        fill_block( parallel_blocks[b], b );
                    }
                });
            }
            for ( int w = 0; w < NumWorkers; w++ )
            {
                workers[w].join();
            }
        }

        for ( int b = 0; b < NumBlocks; b++ )
        {
            fill_block( serial_blocks[b], b );
        }

        check( memcmp( parallel_blocks, serial_blocks, sizeof( serial_blocks ) ) == 0 );

        // the relocatability contract these tests ride on, stated directly
        static_assert( std::is_trivially_copyable<RenderBlock>::value, "parallel scatter requires relocatable types" );
    }

    if ( touch_generated_types() != 0 )
        return 1;

    printf( "OK\n" );
    return 0;
}
