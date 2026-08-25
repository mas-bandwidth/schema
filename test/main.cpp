// Includes every generated header, round-trips generated types and messages
// through the classic serialize runtime, and prints OK. second.cpp includes
// the same headers into a second translation unit, so a successful link also
// proves the headers are multiple-inclusion safe.

#include <cmath> // the gate-7 discrimination tripwires (std::fma, std::floor)
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <type_traits>
#include <thread>

#include "ConstantsWire.h"
#include "ContextsWire.h"
#include "EnumsWire.h"
#include "MessagesWire.h"
#include "ObjectsWire.h"
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
static bool golden_wire( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/%s.bin", name );
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
    return true;
}

int main()
{
    using namespace example;

    // constants fold and export (SPEC §4.2)
    static_assert(MaxPositionUnits == MaxWorldMeters * PositionUnits, "constants compose");
    static_assert(NumTeams == 2, "NumTeams = Team.Max — entry 0 is the None sentinel in every enum, so the count of real things is always Enum.Max");
    static_assert(ProtocolId != 0, "the unit has a protocol id");

    // enums: None = 0 implicit, variants dense from 1 (SPEC §4.2)
    static_assert(static_cast<int>(Team::None) == 0, "None = 0");
    static_assert(static_cast<int>(Team::Blue) == 2, "variants pack from 1");
    static_assert(static_cast<int>(MessageType::Block) == 1, "message tags sorted by name");
    static_assert(static_cast<int>(ObjectType::DynamicProp) == 1, "object tags sorted by name");

    // flags: one bit per variant from bit 0 (SPEC §4.2)
    static_assert(ShipFlags_FiringLaser == 1ull << 0, "flags bits assign in declaration order");
    static_assert(ShipFlags_Aiming == 1ull << 3, "flags bits assign in declaration order");

    // worst-case bounds exist and are sane (SPEC §6.1 item 4)
    static_assert(RigidBodyMaxBits > 0 && RigidBodyMaxBytes >= RigidBodyMaxBits / 8, "MaxBits/MaxBytes");
    static_assert(MessageMaxBytes > 0, "message-level bound");

    // zero initialization is the rule (Glenn, 2026-08-05)
    ClientShipState client_ship;
    check( !client_ship.predicted_explode && client_ship.num_colliders == 0 );
    ServerMissileState missile;
    check( missile.timer == 0.0 );

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

    // ---- Test message: ranged integers validate on read (SPEC §5) ----
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

    // ---- the message tag wire (SPEC §4.8) ----
    {
        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteMessageType( ws, MessageType::Chat ) );
        check( WriteMessageType( ws, MessageType::None ) ); // the stream terminator
        ws.Flush();

        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        MessageType tag = MessageType::None;
        check( ReadMessageType( rs, tag ) );
        check( tag == MessageType::Chat );
        check( ReadMessageType( rs, tag ) );
        check( tag == MessageType::None );
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
        check( golden_wire( "shipcreate_flags", buffer, ws.GetBytesProcessed() ) );

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
        check( golden_wire( "rigidbody_moving", buffer, ws.GetBytesProcessed() ) );

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
        check( golden_wire( "rigidbody_at_rest", buffer, ws2.GetBytesProcessed() ) );
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
        check( golden_wire( "chat", buffer, ws.GetBytesProcessed() ) );
    }

    // ---- the Message dispatch surface (default: tagged union) ----
    {
        static_assert(std::is_trivially_copyable<Message>::value,
                      "the tagged union is trivially copyable — no heap anywhere");

        Message stream_out[3]; // constructed as the None message: type == None
        check( stream_out[2].type == MessageType::None );
        // a writer SELECTS an arm by assigning it — the assignment establishes
        // the arm's storage as its zero value (default member initializers);
        // construction alone initializes only the tag
        stream_out[0].type = MessageType::Chat;
        stream_out[0].chat = Chat{};
        std::memcpy( stream_out[0].chat.text, "dispatch", 8 );
        stream_out[0].chat.text_length = 8;
        stream_out[1].type = MessageType::Test;
        stream_out[1].test = Test{};
        stream_out[1].test.test_b = 42;

        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        for ( const Message & m : stream_out )
            check( WriteMessage( ws, m ) );
        ws.Flush();

        check( golden_wire( "message_stream", buffer, ws.GetBytesProcessed() ) );

        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        Message in;
        check( ReadMessage( rs, in ) );
        check( GetMessageType( in ) == MessageType::Chat );
        check( in.chat.text_length == 8 );
        check( std::strcmp( in.chat.text, "dispatch" ) == 0 );
        check( ReadMessage( rs, in ) );
        check( GetMessageType( in ) == MessageType::Test );
        check( in.test.test_b == 42 );
        check( ReadMessage( rs, in ) );
        check( GetMessageType( in ) == MessageType::None ); // the terminator
    }

    // ---- union arm re-init: a re-read must not leak a previous arm's bytes ----
    // SPEC §5: read success fully initializes relative to a ZERO-INITIALIZED
    // output object — ReadMessage provides that baseline for exactly the arm
    // the wire tag selects, so a Block full of nonzero bytes decoded into a
    // Message must not remain readable through the chat arm after a smaller
    // Chat is decoded into the SAME value. This pins the arm re-init: remove
    // `message.chat = Chat{};` from ReadMessage and this fails.
    {
        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        Message big;
        big.type = MessageType::Block;
        big.block = Block{};
        big.block.data_length = MaxBlockSize;
        std::memset( big.block.data, 0xFF, MaxBlockSize ); // stand-in attacker bytes
        check( WriteMessage( ws, big ) );
        Message small;
        small.type = MessageType::Chat;
        small.chat = Chat{};
        std::memcpy( small.chat.text, "hi", 2 );
        small.chat.text_length = 2;
        check( WriteMessage( ws, small ) );
        ws.Flush();

        Message reused; // ONE value, reused across reads — the arm-swap case
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadMessage( rs, reused ) );
        check( GetMessageType( reused ) == MessageType::Block );
        check( reused.block.data_length == MaxBlockSize );
        check( reused.block.data[0] == 0xFF && reused.block.data[MaxBlockSize - 1] == 0xFF );
        check( ReadMessage( rs, reused ) ); // the same value, now a Chat
        check( GetMessageType( reused ) == MessageType::Chat );
        check( reused.chat.text_length == 2 );
        check( std::strcmp( reused.chat.text, "hi" ) == 0 );
        for ( int i = 3; i < MaxChatLength + 1; i++ )
        {
            check( reused.chat.text[i] == 0 ); // no Block byte survives in the arm
        }
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
        check( golden_wire( "probe_header", buffer, ws.GetBytesProcessed() ) );
        ProbeHeader out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadProbeHeader( rs, out ) );
        check( out.version == 5 && out.probe_id == 0x1122334455667788ull );
        buffer[0] = 0xAC; // corrupt the wire constant
        serialize::ReadStream rs2( buffer, ws.GetBytesProcessed() );
        check( !ReadProbeHeader( rs2, out ) );
    }

    // ---- the object views: Quantize/Unquantize and the two wires (SPEC §4.8) ----
    {
        ShipData_Interpolate interp;
        interp.ship_type = ShipType::Corvette;
        interp.position = { 1.5, -2.25, 100.0 };
        interp.rotation = { 0.0, 0.0, 0.0, 1.0 };
        interp.linear_velocity = { 3.0, 0.0, -1.0 };
        interp.flags = ShipFlags_Boosting;
        interp.team = Team::Red;
        interp.health = 750; // wire-int domain (rule 5)
        interp.thrust = 55;

        ShipData_Shallow q;
        QuantizeShip( interp, q );
        check( q.position_x == 1536 );          // 1.5 * 1024
        check( q.position_y == -2304 );         // -2.25 * 1024
        check( q.rotation_w == 1024 );          // 1.0 * 1024
        check( q.health == 750 && q.thrust == 55 ); // projected fields copy
        check( q.team == Team::Red && q.flags == ShipFlags_Boosting );

        // the shallow wire round-trips the quantized struct
        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteShipData_Shallow( ws, q ) );
        ws.Flush();
        check( golden_wire( "ship_shallow", buffer, ws.GetBytesProcessed() ) );
        ShipData_Shallow q2;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( ReadShipData_Shallow( rs, q2 ) );
        check( q2.position_x == 1536 && q2.rotation_w == 1024 && q2.health == 750 );

        // unquantize recovers within one step of the scale
        ShipData_Interpolate back;
        UnquantizeShip( q2, back );
        check( back.position.x == 1536.0 / 1024.0 );
        check( back.position.y == -2304.0 / 1024.0 );
        check( back.rotation.w == 1.0 );
        check( back.health == 750 && back.team == Team::Red );

        // the deep wire: [interpolate] float triples ride as BARE floats here —
        // the view-encoding attributes describe the shallow wire only (SPEC §4.8)
        ShipData_Deep deep;
        deep.ship_type = ShipType::Carrier;
        deep.health = 333.25f; // fractional survives the deep wire exactly
        deep.laser_index = 15;
        deep.target.object_id = 4242;
        deep.lock_start_time = 12.5;
        serialize::WriteStream ws2( buffer, sizeof( buffer ) );
        check( WriteShipData_Deep( ws2, deep ) );
        ws2.Flush();
        ShipData_Deep deep2;
        serialize::ReadStream rs2( buffer, ws2.GetBytesProcessed() );
        check( ReadShipData_Deep( rs2, deep2 ) );
        check( deep2.ship_type == ShipType::Carrier );
        check( deep2.health == 333.25f );
        check( deep2.laser_index == 15 && deep2.target.object_id == 4242 );
        check( deep2.lock_start_time == 12.5 );
    }

    // ---- the object tag wire ----
    {
        serialize::WriteStream ws( buffer, sizeof( buffer ) );
        check( WriteObjectType( ws, ObjectType::Turret ) );
        check( WriteObjectType( ws, ObjectType::None ) ); // the baseline sentinel
        ws.Flush();
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        ObjectType tag = ObjectType::None;
        check( ReadObjectType( rs, tag ) );
        check( tag == ObjectType::Turret );
        check( ReadObjectType( rs, tag ) );
        check( tag == ObjectType::None );
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
        check( golden_wire( "testdata", buffer, ws.GetBytesProcessed() ) );

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
        check( golden_wire( "compressed_probe", buffer, ws.GetBytesProcessed() ) );

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
        check( golden_wire( "inputpacket", buffer, ws.GetBytesProcessed() ) );
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
        check( golden_wire( "probebits", buffer, ws.GetBytesProcessed() ) );
    }

    // ---- ProbeCollider: first-class one-of (SPEC §4.8) — construction is
    // None, round trip through a selected arm, a None arm beside it, an
    // array of unions, the wire golden, and the refusal negative controls ----
    {
        static_assert( std::is_trivially_copyable<ProbeShape>::value,
                       "the union representation is trivially copyable, the message union's contract (SPEC §4.8)" );
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
        check( golden_wire( "probecollider", buffer, ws.GetBytesProcessed() ) );

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

        // the write side validates the tag BEFORE it rides (SPEC §4.8,
        // WriteMessage's rule): an out-of-set tag writes nothing
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
        check( golden_wire( "probearray", buffer, ws.GetBytesProcessed() ) );

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
