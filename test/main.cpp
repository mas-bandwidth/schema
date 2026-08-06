// Includes every generated header, round-trips generated types and messages
// through the classic serialize runtime, and prints OK. second.cpp includes
// the same headers into a second translation unit, so a successful link also
// proves the headers are multiple-inclusion safe.

#include <cstdio>
#include <cstring>

#include "Constants.h"
#include "Contexts.h"
#include "Enums.h"
#include "Messages.h"
#include "Objects.h"
#include "Types.h"

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

int main()
{
    using namespace example;

    // constants fold and export (SPEC §4.2)
    static_assert(MaxPositionUnits == MaxWorldMeters * PositionUnits, "constants compose");
    static_assert(NumTeams == 3, "NumTeams = Team.Max + 1 (None rides the wire, Max = 2)");
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

    alignas( 8 ) uint8_t buffer[2048];

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

    if ( touch_generated_types() != 0 )
        return 1;

    printf( "OK\n" );
    return 0;
}
