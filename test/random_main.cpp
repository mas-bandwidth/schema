// Randomized round-trips over every generated wire type (SPEC §7.2 gate 4's
// seed): fill with random values respecting every range, branch and count;
// write; read into a DIRTY object (so a field the read misses cannot hide);
// compare field-by-field. Deterministic seed, overridable with
// SCHEMA_RANDOM_SEED for exploration. Also checks bytes <= MaxBytes live.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "ConstantsWire.h"
#include "EnumsWire.h"
#include "TypesWire.h"
#include "WireWire.h"

#define check( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s (%s:%d) seed=%llu iter=%d\n", #condition,     \
                    __FILE__, __LINE__, (unsigned long long) g_seed, g_iter );\
            return false;                                                     \
        }                                                                     \
    } while ( 0 )

static uint64_t g_seed = 0;
static int g_iter = 0;

struct Rng
{
    uint64_t state;
    uint64_t next()
    {
        // xorshift64* — deterministic everywhere, no library dependence
        state ^= state >> 12;
        state ^= state << 25;
        state ^= state >> 27;
        return state * 0x2545F4914F6CDD1Dull;
    }
    int64_t range( int64_t lo, int64_t hi ) // inclusive
    {
        return lo + (int64_t) ( next() % (uint64_t) ( hi - lo + 1 ) );
    }
    bool coin() { return ( next() & 1 ) != 0; }
    double real( double lo, double hi )
    {
        return lo + ( hi - lo ) * ( (double) ( next() >> 11 ) / (double) ( 1ull << 53 ) );
    }
    float realf( float lo, float hi ) { return (float) real( lo, hi ); }
};

using namespace example;

// ---- fills and equality, per type ------------------------------------------

static void fill( Rng & r, Vec3 & v )
{
    v.x = r.real( -10000, 10000 );
    v.y = r.real( -10000, 10000 );
    v.z = r.real( -10000, 10000 );
}
static bool equal( const Vec3 & a, const Vec3 & b )
{
    return a.x == b.x && a.y == b.y && a.z == b.z;
}

static void fill( Rng & r, Quat & q )
{
    q.x = r.real( -1, 1 );
    q.y = r.real( -1, 1 );
    q.z = r.real( -1, 1 );
    q.w = r.real( -1, 1 );
}
static bool equal( const Quat & a, const Quat & b )
{
    return a.x == b.x && a.y == b.y && a.z == b.z && a.w == b.w;
}

static void fill( Rng & r, Handle & h )
{
    h.object_id = (int32_t) r.range( 0, MaxObjects - 1 );
    h.object_sequence = (uint8_t) r.range( 0, 255 );
}
static bool equal( const Handle & a, const Handle & b )
{
    return a.object_id == b.object_id && a.object_sequence == b.object_sequence;
}

static void fill( Rng & r, QuantizedPosition & q )
{
    q.x = (int32_t) r.range( -MaxPositionUnits, MaxPositionUnits );
    q.y = (int32_t) r.range( -MaxPositionUnits, MaxPositionUnits );
    q.z = (int32_t) r.range( -MaxPositionUnits, MaxPositionUnits );
}
static bool equal( const QuantizedPosition & a, const QuantizedPosition & b )
{
    return a.x == b.x && a.y == b.y && a.z == b.z;
}

static void fill( Rng & r, QuantizedVelocity & q )
{
    q.x = (int32_t) r.range( -MaxVelocityUnits, MaxVelocityUnits );
    q.y = (int32_t) r.range( -MaxVelocityUnits, MaxVelocityUnits );
    q.z = (int32_t) r.range( -MaxVelocityUnits, MaxVelocityUnits );
}
static bool equal( const QuantizedVelocity & a, const QuantizedVelocity & b )
{
    return a.x == b.x && a.y == b.y && a.z == b.z;
}

static void fill( Rng & r, QuantizedRotation & q )
{
    q.x = (int16_t) r.range( -RotationUnits, RotationUnits );
    q.y = (int16_t) r.range( -RotationUnits, RotationUnits );
    q.z = (int16_t) r.range( -RotationUnits, RotationUnits );
    q.w = (int16_t) r.range( -RotationUnits, RotationUnits );
}
static bool equal( const QuantizedRotation & a, const QuantizedRotation & b )
{
    return a.x == b.x && a.y == b.y && a.z == b.z && a.w == b.w;
}

static void fill( Rng & r, RigidBody & body )
{
    fill( r, body.position );
    fill( r, body.orientation );
    body.at_rest = r.coin();
    fill( r, body.linear_velocity );
    fill( r, body.angular_velocity );
}
static bool equal( const RigidBody & a, const RigidBody & b )
{
    if ( !equal( a.position, b.position ) || !equal( a.orientation, b.orientation ) )
        return false;
    if ( a.at_rest != b.at_rest )
        return false;
    if ( a.at_rest )
        return true; // untaken branch: the read zeroed them; write never sent them
    return equal( a.linear_velocity, b.linear_velocity ) && equal( a.angular_velocity, b.angular_velocity );
}

static void fill( Rng & r, Input & in )
{
    in.stick_x = r.realf( -1, 1 );
    in.stick_y = r.realf( -1, 1 );
    in.throttle = r.realf( 0, 1 );
    in.yaw = r.realf( -180, 180 );
    in.pitch = r.realf( -90, 90 );
    in.fire = r.coin();
    in.alt_fire = r.coin();
    in.boost = r.coin();
    in.brake = r.coin();
    in.aim = r.coin();
    in.lock_on = r.coin();
    in.zoom = r.coin();
    in.ping = r.coin();
}
static bool equal( const Input & a, const Input & b )
{
    return a.stick_x == b.stick_x && a.stick_y == b.stick_y && a.throttle == b.throttle &&
           a.yaw == b.yaw && a.pitch == b.pitch && a.fire == b.fire && a.alt_fire == b.alt_fire &&
           a.boost == b.boost && a.brake == b.brake && a.aim == b.aim && a.lock_on == b.lock_on &&
           a.zoom == b.zoom && a.ping == b.ping;
}

static void fill( Rng & r, InputPacket & p )
{
    p.synchronize_sequence = (uint16_t) r.range( 0, 65535 );
    p.current_frame = r.next();
    p.start_frame = r.next();
    p.inputs_count = (int32_t) r.range( 0, MaxInputsPerPacket );
    for ( int32_t i = 0; i < p.inputs_count; i++ )
        fill( r, p.inputs[i] );
}
static bool equal( const InputPacket & a, const InputPacket & b )
{
    if ( a.synchronize_sequence != b.synchronize_sequence || a.current_frame != b.current_frame ||
         a.start_frame != b.start_frame || a.inputs_count != b.inputs_count )
        return false;
    for ( int32_t i = 0; i < a.inputs_count; i++ )
        if ( !equal( a.inputs[i], b.inputs[i] ) )
            return false;
    return true;
}

static void fill( Rng & r, ShipCreate & s )
{
    s.ship_type = (ShipType) r.range( 0, 5 );
    fill( r, s.position );
    fill( r, s.rotation );
    fill( r, s.linear_velocity );
    s.has_flags = r.coin();
    s.flags = ( uint64_t ) r.range( 0, 15 ); // ShipFlags: 4 wire bits
    s.team = (Team) r.range( 0, 2 );
    s.health = (int16_t) r.range( 0, MaxHealth );
    s.thrust = (int8_t) r.range( 0, 100 );
}
static bool equal( const ShipCreate & a, const ShipCreate & b )
{
    if ( a.ship_type != b.ship_type || !equal( a.position, b.position ) ||
         !equal( a.rotation, b.rotation ) || !equal( a.linear_velocity, b.linear_velocity ) )
        return false;
    if ( a.has_flags != b.has_flags )
        return false;
    if ( a.has_flags && a.flags != b.flags )
        return false; // untaken: read zeroes flags, write never sent them
    return a.team == b.team && a.health == b.health && a.thrust == b.thrust;
}

// ---- the Wire.schema report shapes ----

static void fill( Rng & r, Test & t )
{
    t.test_a = (uint16_t) r.range( 0, 65535 );
    t.test_b = (int16_t) r.range( 0, 1000 );
    t.test_c = (int16_t) r.range( 0, 1000 );
    t.test_d = (int16_t) r.range( 0, 1000 );
}
static bool equal( const Test & a, const Test & b )
{
    return a.test_a == b.test_a && a.test_b == b.test_b && a.test_c == b.test_c && a.test_d == b.test_d;
}

static void fill( Rng & r, Block & blk )
{
    blk.data_length = (int32_t) r.range( 0, MaxBlockSize );
    for ( int32_t i = 0; i < blk.data_length; i++ )
        blk.data[i] = (uint8_t) r.range( 0, 255 );
}
static bool equal( const Block & a, const Block & b )
{
    return a.data_length == b.data_length &&
           std::memcmp( a.data, b.data, (size_t) a.data_length ) == 0;
}

// fill_utf8 fills buffer with random well-formed UTF-8 — string(N) payloads
// are well-formed UTF-8 by contract and the write path debug-asserts it
// (SPEC §4.7). Random code points over all four encoded lengths,
// skipping 0 and the surrogate block, truncated to fit. Returns bytes used.
static int32_t fill_utf8( Rng & r, char * buffer, int32_t budget )
{
    int32_t used = 0;
    while ( used < budget )
    {
        uint32_t cp = (uint32_t) r.range( 1, 0x10FFFF );
        if ( cp >= 0xD800 && cp <= 0xDFFF )
        {
            continue; // surrogates never appear in well-formed UTF-8
        }
        int32_t bytes = cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
        if ( used + bytes > budget )
        {
            break; // never truncate mid-sequence
        }
        if ( bytes == 1 )
        {
            buffer[used++] = (char) cp;
        }
        else if ( bytes == 2 )
        {
            buffer[used++] = (char) ( 0xC0 | ( cp >> 6 ) );
            buffer[used++] = (char) ( 0x80 | ( cp & 0x3F ) );
        }
        else if ( bytes == 3 )
        {
            buffer[used++] = (char) ( 0xE0 | ( cp >> 12 ) );
            buffer[used++] = (char) ( 0x80 | ( ( cp >> 6 ) & 0x3F ) );
            buffer[used++] = (char) ( 0x80 | ( cp & 0x3F ) );
        }
        else
        {
            buffer[used++] = (char) ( 0xF0 | ( cp >> 18 ) );
            buffer[used++] = (char) ( 0x80 | ( ( cp >> 12 ) & 0x3F ) );
            buffer[used++] = (char) ( 0x80 | ( ( cp >> 6 ) & 0x3F ) );
            buffer[used++] = (char) ( 0x80 | ( cp & 0x3F ) );
        }
    }
    return used;
}

static void fill( Rng & r, Chat & c )
{
    c.text_length = fill_utf8( r, c.text, (int32_t) r.range( 0, MaxChatLength ) );
    c.text[c.text_length] = 0;
}
static bool equal( const Chat & a, const Chat & b )
{
    return a.text_length == b.text_length &&
           std::memcmp( a.text, b.text, (size_t) a.text_length ) == 0;
}

// ---- the Wire.schema coverage types ----

static void fill( Rng & r, ProbeHeader & p )
{
    p.version = (uint32_t) r.range( 0, 7 );
    p.probe_id = r.next();
}
static bool equal( const ProbeHeader & a, const ProbeHeader & b )
{
    return a.version == b.version && a.probe_id == b.probe_id;
}

static void fill( Rng & r, ProbeBits & p )
{
    p.small = (uint32_t) r.range( 0, 511 );
    p.boundary = r.next() & ( ( 1ull << 33 ) - 1 );
    p.wide = r.next();
    p.sensor = (uint32_t) r.next();
    p.nonce = r.next();
}
static bool equal( const ProbeBits & a, const ProbeBits & b )
{
    return a.small == b.small && a.boundary == b.boundary && a.wide == b.wide &&
           a.sensor == b.sensor && a.nonce == b.nonce;
}

// one compressed-float step, with headroom for the float arithmetic
static bool near_step( float a, float b, float step )
{
    float d = a - b;
    if ( d < 0 )
        d = -d;
    return d <= step * 1.01f;
}

static void fill( Rng & r, ProbeSample & p )
{
    p.active = r.coin();
    p.orientation = r.realf( -180, 180 );
    p.raw_delta = (int32_t) r.next();
    p.big_delta = (int64_t) r.next();
    p.weapon = (Weapon) r.range( 0, 15 ); // headroom: non-variant values are wire-legal
    p.has_target = r.coin();
    p.target_id = (uint16_t) r.range( 0, 65535 );
    p.idle_ticks = (uint32_t) r.next();
    p.samples_count = (int32_t) r.range( 1, 8 );
    for ( int32_t i = 0; i < p.samples_count; i++ )
        p.samples[i] = (uint16_t) r.range( 0, 65535 );
}
static bool equal( const ProbeSample & a, const ProbeSample & b )
{
    if ( a.active != b.active || !near_step( a.orientation, b.orientation, 0.01f ) )
        return false;
    if ( a.raw_delta != b.raw_delta || a.big_delta != b.big_delta )
        return false;
    if ( a.active )
    {
        if ( a.weapon != b.weapon || a.has_target != b.has_target )
            return false;
        if ( a.has_target && a.target_id != b.target_id )
            return false;
    }
    else if ( a.idle_ticks != b.idle_ticks )
        return false;
    if ( a.samples_count != b.samples_count )
        return false;
    for ( int32_t i = 0; i < a.samples_count; i++ )
        if ( a.samples[i] != b.samples[i] )
            return false;
    return true;
}

static void fill( Rng & r, ProbeConfig & p )
{
    p.retries = (int32_t) r.next();
    p.preferred = (Weapon) r.range( 0, 15 );
}
static bool equal( const ProbeConfig & a, const ProbeConfig & b )
{
    return a.retries == b.retries && a.preferred == b.preferred;
}

static void fill( Rng & r, ProbeArray & p )
{
    for ( int i = 0; i < 2; i++ )
    {
        fill( r, p.samples[i] );
    }
    fill( r, p.config );
}

static bool equal( const ProbeArray & a, const ProbeArray & b )
{
    for ( int i = 0; i < 2; i++ )
    {
        if ( !equal( a.samples[i], b.samples[i] ) )
        {
            return false;
        }
    }
    return equal( a.config, b.config );
}

static void fill( Rng & r, ProbeReport & p )
{
    fill( r, p.header );
    p.flags = (uint64_t) r.range( 0, 255 ); // ProbeFlags: 8 wire bits
    fill( r, p.echo );
}
static bool equal( const ProbeReport & a, const ProbeReport & b )
{
    return equal( a.header, b.header ) && a.flags == b.flags && equal( a.echo, b.echo );
}

static void fill( Rng & r, TestData & t )
{
    t.a = (int32_t) r.range( -100, 100 );
    t.b = (int32_t) r.range( -100, 100 );
    t.c = (int32_t) r.range( -100, 150 );
    t.d = (uint32_t) r.range( 0, 255 );
    t.e = (uint32_t) r.range( 0, 255 );
    t.f = (uint32_t) r.range( 0, 255 );
    t.g = r.coin();
    t.items_count = (int32_t) r.range( 0, 16 );
    for ( int32_t i = 0; i < t.items_count; i++ )
        t.items[i] = (int32_t) r.range( 0, 255 );
    t.float_value = r.realf( -1000, 1000 );
    t.compressed_float_value = r.realf( 0, 10 );
    t.double_value = r.real( -1000, 1000 );
    t.int8_value = (int8_t) r.range( -128, 127 );   // the sign-extension regression class
    t.int16_value = (int16_t) r.range( -32768, 32767 );
    t.uint8_value = (uint8_t) r.range( 0, 255 );
    t.uint16_value = (uint16_t) r.range( 0, 65535 );
    t.uint32_value = (uint32_t) r.next();
    t.uint64_value = r.next();
    t.int64_full = (int64_t) r.next();
    t.int64_range = r.range( -1000000000000ll, 1000000000000ll );
    for ( int i = 0; i < 17; i++ )
        t.fixed_bytes[i] = (uint8_t) r.range( 0, 255 );
    t.text_length = fill_utf8( r, t.text, (int32_t) r.range( 0, 255 ) ); // well-formed UTF-8 by contract (SPEC §4.7)
    t.text[t.text_length] = 0;
}
static bool equal( const TestData & a, const TestData & b )
{
    if ( a.a != b.a || a.b != b.b || a.c != b.c || a.d != b.d || a.e != b.e || a.f != b.f || a.g != b.g )
        return false;
    if ( a.items_count != b.items_count )
        return false;
    for ( int32_t i = 0; i < a.items_count; i++ )
        if ( a.items[i] != b.items[i] )
            return false;
    if ( a.float_value != b.float_value || a.double_value != b.double_value )
        return false;
    if ( !near_step( a.compressed_float_value, b.compressed_float_value, 0.01f ) )
        return false;
    if ( a.int8_value != b.int8_value || a.int16_value != b.int16_value )
        return false;
    if ( a.uint8_value != b.uint8_value || a.uint16_value != b.uint16_value ||
         a.uint32_value != b.uint32_value || a.uint64_value != b.uint64_value )
        return false;
    if ( a.int64_full != b.int64_full || a.int64_range != b.int64_range )
        return false;
    if ( std::memcmp( a.fixed_bytes, b.fixed_bytes, 17 ) != 0 )
        return false;
    return a.text_length == b.text_length &&
           std::memcmp( a.text, b.text, (size_t) a.text_length ) == 0;
}

// ---- the driver -------------------------------------------------------------

template <typename T>
static bool roundtrip( Rng & r,
                       void ( *fillFn )( Rng &, T & ),
                       bool ( *eqFn )( const T &, const T & ),
                       bool ( *writeFn )( serialize::WriteStream &, const T & ),
                       bool ( *readFn )( serialize::ReadStream &, T & ),
                       int64_t maxBytes )
{
    alignas( 8 ) static uint8_t buffer[4096 + 8];  // + 8: read buffer allocations extend 8 bytes past the data (the reader loads 64-bit windows)
    T in;
    fillFn( r, in );
    serialize::WriteStream ws( buffer, sizeof( buffer ) );
    check( writeFn( ws, in ) );
    ws.Flush();
    check( ws.GetBytesProcessed() <= maxBytes );
    T out;
    fillFn( r, out ); // dirty target: a field the read misses cannot hide
    serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
    check( readFn( rs, out ) );
    check( eqFn( in, out ) );
    return true;
}

int main()
{
    g_seed = 0x5EED0001;
    if ( const char * env = std::getenv( "SCHEMA_RANDOM_SEED" ) )
        g_seed = strtoull( env, nullptr, 0 );
    Rng r{ g_seed };

    const int iterations = 2000;
    for ( g_iter = 0; g_iter < iterations; g_iter++ )
    {
        if ( !roundtrip<Vec3>( r, fill, equal, WriteVec3, ReadVec3, Vec3MaxBytes ) ) return 1;
        if ( !roundtrip<Quat>( r, fill, equal, WriteQuat, ReadQuat, QuatMaxBytes ) ) return 1;
        if ( !roundtrip<Handle>( r, fill, equal, WriteHandle, ReadHandle, HandleMaxBytes ) ) return 1;
        if ( !roundtrip<QuantizedPosition>( r, fill, equal, WriteQuantizedPosition, ReadQuantizedPosition, QuantizedPositionMaxBytes ) ) return 1;
        if ( !roundtrip<QuantizedVelocity>( r, fill, equal, WriteQuantizedVelocity, ReadQuantizedVelocity, QuantizedVelocityMaxBytes ) ) return 1;
        if ( !roundtrip<QuantizedRotation>( r, fill, equal, WriteQuantizedRotation, ReadQuantizedRotation, QuantizedRotationMaxBytes ) ) return 1;
        if ( !roundtrip<RigidBody>( r, fill, equal, WriteRigidBody, ReadRigidBody, RigidBodyMaxBytes ) ) return 1;
        if ( !roundtrip<Input>( r, fill, equal, WriteInput, ReadInput, InputMaxBytes ) ) return 1;
        if ( !roundtrip<InputPacket>( r, fill, equal, WriteInputPacket, ReadInputPacket, InputPacketMaxBytes ) ) return 1;
        if ( !roundtrip<ShipCreate>( r, fill, equal, WriteShipCreate, ReadShipCreate, ShipCreateMaxBytes ) ) return 1;
        if ( !roundtrip<Test>( r, fill, equal, WriteTest, ReadTest, TestMaxBytes ) ) return 1;
        if ( !roundtrip<Block>( r, fill, equal, WriteBlock, ReadBlock, BlockMaxBytes ) ) return 1;
        if ( !roundtrip<Chat>( r, fill, equal, WriteChat, ReadChat, ChatMaxBytes ) ) return 1;
        if ( !roundtrip<ProbeHeader>( r, fill, equal, WriteProbeHeader, ReadProbeHeader, ProbeHeaderMaxBytes ) ) return 1;
        if ( !roundtrip<ProbeBits>( r, fill, equal, WriteProbeBits, ReadProbeBits, ProbeBitsMaxBytes ) ) return 1;
        if ( !roundtrip<ProbeSample>( r, fill, equal, WriteProbeSample, ReadProbeSample, ProbeSampleMaxBytes ) ) return 1;
        if ( !roundtrip<ProbeConfig>( r, fill, equal, WriteProbeConfig, ReadProbeConfig, ProbeConfigMaxBytes ) ) return 1;
        if ( !roundtrip<ProbeArray>( r, fill, equal, WriteProbeArray, ReadProbeArray, ProbeArrayMaxBytes ) ) return 1;
        if ( !roundtrip<ProbeReport>( r, fill, equal, WriteProbeReport, ReadProbeReport, ProbeReportMaxBytes ) ) return 1;
        if ( !roundtrip<TestData>( r, fill, equal, WriteTestData, ReadTestData, TestDataMaxBytes ) ) return 1;
    }

    // the specified defaults are the constructed state (SPEC §4.2)
    {
        ProbeConfig fresh;
        if ( fresh.retries != -1 || fresh.preferred != Weapon::Railgun )
        {
            printf( "FAILED: specified defaults\n" );
            return 1;
        }
        ProbeSample sample_fresh;
        if ( !sample_fresh.active )
        {
            printf( "FAILED: bool default true\n" );
            return 1;
        }
        ProbeArray array_fresh; // transitive: defaults reach through composition
        if ( !array_fresh.samples[0].active || !array_fresh.samples[1].active ||
             array_fresh.config.retries != -1 || array_fresh.config.preferred != Weapon::Railgun )
        {
            printf( "FAILED: transitive defaults through ProbeArray\n" );
            return 1;
        }
    }

    printf( "OK (%d iterations, seed %llu)\n", iterations, (unsigned long long) g_seed );
    return 0;
}
