// schema bench — the C++ runner.
//
// Measures the schema-GENERATED C++ code (generated/cpp) against the classic
// serialize runtime: write path and read path, messages/sec and MB/sec, over
// the pinned corpus instances (the same instances test/main.cpp pins to the
// wire goldens in testdata/wire) plus one large synthetic message batch for
// steady-state dispatch throughput.
//
// Methodology (follows the serialize repo's bench.cpp conventions — see the
// const-params experiment for the reasoning behind the escape barriers and
// the per-iteration LCG variation):
//   - every write loop varies message fields per iteration through a serially
//     dependent LCG the compiler cannot fold; structure fields (counts,
//     lengths, branch bools) stay fixed so bytes/op is constant
//   - every read loop reads from 64 pre-written variant buffers round-robin,
//     and the decoded object is observed through an escape barrier
//   - before benching, every pinned instance is byte-compared against its
//     wire golden (testdata/wire/<name>.bin) and round-tripped write→read→
//     re-write→memcmp; a mismatch refuses to bench
//   - fixed iteration counts, one warmup run per path, then NumRuns measured
//     runs; the report is the MEDIAN rate with min/max and spread
//   - MB/s means MiB/s (1024*1024), following serialize bench.cpp
//
// Output: a human table on stderr, and with --csv, CSV rows on stdout in the
// cross-language format (see bench/README.md). Other language runners must
// implement the same benchmarks, the same pinned instances, the same LCG and
// variation scheme — this file is the reference implementation.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <chrono>
#include <algorithm>

#include "ConstantsWire.h"
#include "ContextsWire.h"
#include "EnumsWire.h"
#include "MessagesWire.h"
#include "ObjectsWire.h"
#include "TypesWire.h"
#include "WireWire.h"

static volatile uint64_t g_sink = 0;    // defeats dead code elimination of computed values

// Tells the compiler the memory at data is observed, so stores to it cannot be
// dead code eliminated (the standard empty-asm escape, as in serialize bench.cpp).
#if defined(_MSC_VER)
#include <intrin.h>
inline void bench_escape( const void * data )
{
    (void) data;
    _ReadWriteBarrier();
}
#else // #if defined(_MSC_VER)
inline void bench_escape( const void * data )
{
    asm volatile( "" : : "g"( data ) : "memory" );
}
#endif // #if defined(_MSC_VER)

inline double time_now()
{
    return std::chrono::duration<double>( std::chrono::steady_clock::now().time_since_epoch() ).count();
}

// The LCG every runner must use (Knuth MMIX, as in serialize bench.cpp).
inline uint64_t bench_rng( uint64_t rng )
{
    return rng * 6364136223846793005ULL + 1442695040888963407ULL;
}

const int NumRuns = 7;          // median of 7 (N >= 5), after 1 warmup run
const int NumVariants = 64;     // read-path variant buffers

#if defined(NDEBUG)
const long IterScale = 1;       // Release: full fixed counts
#else
const long IterScale = 8;       // Debug: fixed counts / 8 (recorded in the iters column)
#endif

static bool g_csv = false;
static const char * g_wire_dir = "testdata/wire";

// buffers: write buffers must be a multiple of 8 bytes (qword-flush contract);
// read allocations extend >= 8 bytes past the packet (64-bit window contract).
// 4096 covers MessageMaxBytes (2008) with slack on both contracts.
const int BufferSize = 4096;
alignas( 8 ) static uint8_t g_buffer[BufferSize];
alignas( 8 ) static uint8_t g_twin[BufferSize];
alignas( 8 ) static uint8_t g_variants[NumVariants][BufferSize];

static bool failed = false;

static void fail( const char * name, const char * what )
{
    fprintf( stderr, "FAILED: %s: %s\n", name, what );
    failed = true;
}

static bool check_golden( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s.bin", g_wire_dir, name );
    FILE * f = fopen( path, "rb" );
    if ( !f )
    {
        fprintf( stderr, "missing wire golden %s — run from the schema repo root (or pass --wire-dir)\n", path );
        return false;
    }
    static uint8_t expected[BufferSize];
    size_t n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int64_t) n != bytes || memcmp( expected, data, (size_t) bytes ) != 0 )
    {
        fprintf( stderr, "WIRE GOLDEN MISMATCH: %s (%lld golden vs %lld actual bytes) — refusing to bench code that does not match the corpus\n",
                 name, (long long) n, (long long) bytes );
        return false;
    }
    return true;
}

struct RunStats
{
    double median_rate;     // ops/sec
    double min_rate;
    double max_rate;
    double spread_pct;      // (max - min) / median * 100
};

static RunStats run_stats( double * rates, int n )
{
    std::sort( rates, rates + n );
    RunStats s;
    s.median_rate = rates[n / 2];
    s.min_rate = rates[0];
    s.max_rate = rates[n - 1];
    s.spread_pct = ( s.max_rate - s.min_rate ) / s.median_rate * 100.0;
    return s;
}

static void report( const char * bench, const char * path, long iters, int64_t bytes_per_op, const RunStats & s )
{
    const double mbps = s.median_rate * double( bytes_per_op ) / ( 1024.0 * 1024.0 );
    fprintf( stderr, "%-18s %-5s %10.2f M msg/s %10.1f MB/s   (min %.2f, max %.2f, spread %.1f%%)\n",
             bench, path, s.median_rate / 1e6, mbps, s.min_rate / 1e6, s.max_rate / 1e6, s.spread_pct );
    if ( g_csv )
    {
        printf( "cpp,%s,%s,%ld,%lld,%d,%.0f,%.0f,%.0f,%.2f,%.2f\n",
                bench, path, iters, (long long) bytes_per_op, NumRuns,
                s.median_rate, s.min_rate, s.max_rate, mbps, s.spread_pct );
    }
}

// ------------------------------------------------------------------------------------------
// the per-message benchmark driver
// ------------------------------------------------------------------------------------------

template <typename T, typename WriteFn, typename ReadFn, typename VaryFn>
static void bench_message( const char * name, const char * golden, long base_iters,
                           const T & pinned, WriteFn write_fn, ReadFn read_fn, VaryFn vary_fn )
{
    const long iters = base_iters / IterScale;

    // self-check 1: the pinned instance matches its wire golden byte-for-byte
    T base = pinned;
    serialize::WriteStream ws( g_buffer, BufferSize );
    if ( !write_fn( ws, base ) )
    {
        fail( name, "write of pinned instance failed" );
        return;
    }
    ws.Flush();
    const int64_t bytes_per_op = ws.GetBytesProcessed();
    if ( golden && !check_golden( golden, g_buffer, bytes_per_op ) )
    {
        failed = true;
        return;
    }

    // self-check 2: round-trip write -> read -> re-write -> identical bytes
    {
        T out;
        serialize::ReadStream rs( g_buffer, (int) bytes_per_op );
        if ( !read_fn( rs, out ) )
        {
            fail( name, "read of pinned instance failed" );
            return;
        }
        serialize::WriteStream tws( g_twin, BufferSize );
        if ( !write_fn( tws, out ) )
        {
            fail( name, "re-write of decoded instance failed" );
            return;
        }
        tws.Flush();
        if ( tws.GetBytesProcessed() != bytes_per_op ||
             memcmp( g_buffer, g_twin, (size_t) bytes_per_op ) != 0 )
        {
            fail( name, "round-trip bytes differ" );
            return;
        }
    }

    // variant buffers for the read path (and proof that variation keeps bytes/op constant)
    uint64_t rng = 1;
    for ( int k = 0; k < NumVariants; k++ )
    {
        rng = bench_rng( rng );
        vary_fn( base, rng );
        serialize::WriteStream vs( g_variants[k], BufferSize );
        if ( !write_fn( vs, base ) )
        {
            fail( name, "write of varied instance failed" );
            return;
        }
        vs.Flush();
        if ( vs.GetBytesProcessed() != bytes_per_op )
        {
            fail( name, "variation changed bytes/op — vary must keep structure fields fixed" );
            return;
        }
    }

    double write_rates[NumRuns];
    double read_rates[NumRuns];

    // write path: 1 warmup + NumRuns measured
    for ( int run = -1; run < NumRuns; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i++ )
        {
            rng = bench_rng( rng );
            vary_fn( base, rng );
            serialize::WriteStream stream( g_buffer, BufferSize );
            if ( !write_fn( stream, base ) )
            {
                fail( name, "write failed in loop" );
                return;
            }
            stream.Flush();
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) stream.GetBytesProcessed();
        }
        double time = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = double( iters ) / time;
    }

    // read path: 1 warmup + NumRuns measured; ONE decode instance hoisted out
    // of the loop and reused, matching the write loop's hoisted base — a
    // fresh T per iteration is constructed+zeroed harness overhead, not
    // serialize work (every field a read decodes is overwritten every
    // iteration; structure fields are fixed across variants)
    T out;
    for ( int run = -1; run < NumRuns; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i++ )
        {
            serialize::ReadStream stream( g_variants[i & ( NumVariants - 1 )], (int) bytes_per_op );
            if ( !read_fn( stream, out ) )
            {
                fail( name, "read failed in loop" );
                return;
            }
            bench_escape( &out );       // every decoded field is observed
            g_sink = g_sink + 1;
        }
        double time = time_now() - start;
        if ( run >= 0 )
            read_rates[run] = double( iters ) / time;
    }

    report( name, "write", iters, bytes_per_op, run_stats( write_rates, NumRuns ) );
    report( name, "read", iters, bytes_per_op, run_stats( read_rates, NumRuns ) );
}

// ------------------------------------------------------------------------------------------
// pinned corpus instances — copied exactly from test/main.cpp (the golden pins)
// ------------------------------------------------------------------------------------------

using namespace example;

static RigidBody pin_rigidbody_moving()
{
    RigidBody in;
    in.position = { 1.5, -2.5, 3.25 };
    in.orientation = { 0.1, 0.2, 0.3, 0.9 };
    in.at_rest = false;
    in.linear_velocity = { 10.0, 20.0, -3.0 };
    in.angular_velocity = { 0.25, 0.5, 0.75 };
    return in;
}

static Chat pin_chat()
{
    Chat in;
    memcpy( in.text, "wire parity", 11 );
    in.text_length = 11;
    return in;
}

static InputPacket pin_inputpacket()
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
    return in;
}

static ShipCreate pin_shipcreate()
{
    ShipCreate in;
    in.ship_type = ShipType::Bomber;
    in.position = { 1000, -2000, 3000 };
    in.has_flags = true;
    in.flags = ShipFlags_Boosting | ShipFlags_Aiming;
    in.team = Team::Blue;
    in.health = 750;
    in.thrust = 55;
    return in;
}

static ShipData_Shallow pin_ship_shallow()
{
    ShipData_Interpolate interp;
    interp.ship_type = ShipType::Corvette;
    interp.position = { 1.5, -2.25, 100.0 };
    interp.rotation = { 0.0, 0.0, 0.0, 1.0 };
    interp.linear_velocity = { 3.0, 0.0, -1.0 };
    interp.flags = ShipFlags_Boosting;
    interp.team = Team::Red;
    interp.health = 750;
    interp.thrust = 55;
    ShipData_Shallow q;
    QuantizeShip( interp, q );
    return q;
}

static ProbeHeader pin_probe_header()
{
    ProbeHeader h;
    h.version = 5;
    h.probe_id = 0x1122334455667788ull;
    return h;
}

static ProbeBits pin_probebits()
{
    ProbeBits in;
    in.small = 0x1FF;
    in.boundary = 0x1FFFFFFFFull;
    in.wide = 0xFEDCBA9876543210ull;
    in.sensor = 4294967295u;
    in.nonce = 18446744073709551615ull;
    return in;
}

static ProbeArray pin_probearray()
{
    ProbeArray in;
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
    return in;
}

static TestData pin_testdata()
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
    memcpy( in.text, "the quick brown fox", 19 );
    in.text_length = 19;
    return in;
}

// ------------------------------------------------------------------------------------------
// vary functions — mutate VALUE fields within wire ranges through the LCG;
// structure fields (counts, lengths, branch bools) stay fixed so bytes/op is
// constant. Ports must reproduce these mappings exactly.
// ------------------------------------------------------------------------------------------

static void vary_rigidbody( RigidBody & m, uint64_t rng )
{
    m.position.x = double( int64_t( rng >> 8 ) & 0xFFFF ) * 0.25;
    m.position.y = double( int64_t( rng >> 16 ) & 0xFFFF ) * 0.5;
    m.position.z = double( int64_t( rng >> 24 ) & 0xFFFF ) * 0.125;
    m.orientation.x = double( int64_t( rng ) & 0xFF ) * 0.001;
    m.linear_velocity.x = double( int64_t( rng >> 32 ) & 0xFFF ) * 0.25;
    m.angular_velocity.z = double( int64_t( rng >> 40 ) & 0xFFF ) * 0.125;
}

static void vary_chat( Chat & m, uint64_t rng )
{
    for ( int i = 0; i < m.text_length; i++ )
        m.text[i] = char( 'a' + ( ( rng >> ( i & 7 ) ) & 15 ) );    // never zero
}

static void vary_test( Test & m, uint64_t rng )
{
    m.test_a = uint16_t( rng );
    m.test_b = int16_t( ( rng >> 16 ) & 511 );      // within [0, 1000]
    m.test_c = int16_t( ( rng >> 25 ) & 511 );
    m.test_d = int16_t( ( rng >> 34 ) & 511 );
}

static void vary_inputpacket( InputPacket & m, uint64_t rng )
{
    m.synchronize_sequence = uint16_t( rng );
    m.current_frame = rng;
    m.start_frame = rng >> 1;
    m.inputs[0].throttle = float( uint32_t( rng ) & 0xFF ) / 256.0f;
    m.inputs[0].fire = ( rng & 1 ) != 0;
    m.inputs[1].stick_x = float( uint32_t( rng >> 8 ) & 0xFF ) / 256.0f - 0.5f;
    m.inputs[1].boost = ( rng & 2 ) != 0;
}

static void vary_shipcreate( ShipCreate & m, uint64_t rng )
{
    m.position.x = int32_t( ( rng >> 8 ) & 0xFFFFF ) - 0x80000;     // within [-8388608, 8388608]
    m.position.y = int32_t( ( rng >> 16 ) & 0xFFFFF ) - 0x80000;
    m.position.z = int32_t( ( rng >> 24 ) & 0xFFFFF ) - 0x80000;
    m.rotation.x = int16_t( int32_t( rng & 0x7FF ) - 1024 );        // within [-1024, 1024]
    m.linear_velocity.x = int32_t( ( rng >> 32 ) & 0x3FFFFF ) - 2097152;
    m.flags = rng & 15;                                             // 4 wire bits, has_flags stays true
    m.health = int16_t( ( rng >> 5 ) & 511 );                       // within [0, 1000]
    m.thrust = int8_t( ( rng >> 14 ) & 63 );                        // within [0, 100]
}

static void vary_ship_shallow( ShipData_Shallow & m, uint64_t rng )
{
    m.position_x = int32_t( ( rng >> 8 ) & 0xFFFFF ) - 0x80000;
    m.position_y = int32_t( ( rng >> 16 ) & 0xFFFFF ) - 0x80000;
    m.position_z = int32_t( ( rng >> 24 ) & 0xFFFFF ) - 0x80000;
    m.rotation_x = int16_t( int32_t( rng & 0x7FF ) - 1024 );
    m.rotation_w = int16_t( int32_t( ( rng >> 11 ) & 0x7FF ) - 1024 );
    m.linear_velocity_x = int32_t( ( rng >> 32 ) & 0x3FFFFF ) - 2097152;
    m.flags = rng & 15;
    m.health = uint16_t( ( rng >> 5 ) & 511 );
    m.thrust = uint8_t( ( rng >> 14 ) & 63 );
}

static void vary_probe_header( ProbeHeader & m, uint64_t rng )
{
    m.version = uint32_t( rng ) & 7;        // 3 wire bits
    m.probe_id = rng;
}

static void vary_probebits( ProbeBits & m, uint64_t rng )
{
    m.small = uint32_t( rng ) & 511;                        // 9 bits
    m.boundary = rng & ( ( 1ull << 33 ) - 1 );              // 33 bits
    m.wide = rng * 3;
    m.sensor = uint32_t( rng >> 16 );
    m.nonce = rng ^ 0x5555555555555555ull;
}

static void vary_probearray( ProbeArray & m, uint64_t rng )
{
    m.samples[0].orientation = -180.0f + float( uint32_t( rng ) & 0x3FFF ) * 0.02f;
    m.samples[0].raw_delta = int32_t( uint32_t( rng >> 8 ) );
    m.samples[0].big_delta = int64_t( rng * 5 );
    m.samples[0].target_id = uint16_t( rng >> 24 );
    m.samples[0].samples[0] = uint16_t( rng >> 40 );
    m.samples[1].orientation = -180.0f + float( uint32_t( rng >> 3 ) & 0x3FFF ) * 0.02f;
    m.samples[1].idle_ticks = uint32_t( rng >> 32 );
    m.samples[1].samples[0] = uint16_t( rng >> 4 );
    m.samples[1].samples[1] = uint16_t( rng >> 12 );
    m.config.retries = int32_t( uint32_t( rng >> 20 ) );
}

static void vary_testdata( TestData & m, uint64_t rng )
{
    m.a = int32_t( rng & 127 ) - 64;                        // within [-100, 100]
    m.b = int32_t( ( rng >> 7 ) & 127 ) - 64;
    m.c = int32_t( ( rng >> 14 ) & 127 ) - 64;              // within [-100, 150]
    m.d = uint32_t( rng ) & 255;
    m.e = uint32_t( rng >> 8 ) & 255;
    m.f = uint32_t( rng >> 16 ) & 255;
    m.items[0] = int32_t( rng & 255 );                      // items_count stays 3
    m.items[1] = int32_t( ( rng >> 8 ) & 255 );
    m.items[2] = int32_t( ( rng >> 16 ) & 255 );
    m.float_value = float( uint32_t( rng ) & 0xFFFF );
    m.compressed_float_value = float( uint32_t( rng ) & 1023 ) * 0.005f;    // within [0, 10] (max 5.115)
    m.double_value = double( int64_t( rng >> 16 ) & 0xFFFFFF ) * 0.5;
    m.int8_value = int8_t( rng );
    m.int16_value = int16_t( rng >> 8 );
    m.uint8_value = uint8_t( rng >> 16 );
    m.uint16_value = uint16_t( rng >> 24 );
    m.uint32_value = uint32_t( rng >> 32 );
    m.uint64_value = rng * 7;
    m.int64_full = int64_t( rng * 11 );
    m.int64_range = int64_t( ( rng >> 24 ) & ( ( 1ull << 37 ) - 1 ) ) - ( 1ll << 36 );  // within +/- 1e12
    m.fixed_bytes[0] = uint8_t( rng );
    m.fixed_bytes[16] = uint8_t( rng >> 8 );
    for ( int i = 0; i < m.text_length; i++ )
        m.text[i] = char( 'a' + ( ( rng >> ( i & 7 ) ) & 15 ) );    // never zero
}

// ------------------------------------------------------------------------------------------
// the synthetic steady-state batch: NumBatchMessages messages through the
// Message dispatch surface (WriteMessage/ReadMessage) plus the None
// terminator, mixed types driven by the LCG. Larger than L1/L2 working sets
// on both bench hosts.
// ------------------------------------------------------------------------------------------

const int NumBatchMessages = 4096;
const int BatchPasses = 800;

static Message * build_batch( int64_t & batch_bytes, uint8_t * batch_buffer, int batch_buffer_size )
{
    static Message messages[NumBatchMessages];

    uint64_t rng = 12345;
    for ( int k = 0; k < NumBatchMessages; k++ )
    {
        rng = bench_rng( rng );
        Message & m = messages[k];
        const int pick = int( ( rng >> 32 ) % 20 );
        if ( pick < 5 )                     // 25% Chat
        {
            m.type = MessageType::Chat;
            m.chat.text_length = 16 + int( rng & 15 );
            for ( int i = 0; i < m.chat.text_length; i++ )
                m.chat.text[i] = char( 'a' + ( ( rng >> ( i & 7 ) ) & 15 ) );
        }
        else if ( pick < 10 )               // 25% Test
        {
            m.type = MessageType::Test;
            m.test.test_a = uint16_t( rng );
            m.test.test_b = int16_t( ( rng >> 16 ) & 511 );
            m.test.test_c = int16_t( ( rng >> 25 ) & 511 );
            m.test.test_d = int16_t( ( rng >> 34 ) & 511 );
        }
        else if ( pick < 13 )               // 15% Synchronize
        {
            m.type = MessageType::Synchronize;
            m.synchronize.sync_frame = rng;
            m.synchronize.sync_sequence = uint16_t( rng >> 8 );
        }
        else if ( pick < 16 )               // 15% Timescale
        {
            m.type = MessageType::Timescale;
            m.timescale.scale = double( uint32_t( rng ) & 0xFFFF ) / 65536.0;
            m.timescale.frame_a = uint32_t( rng >> 16 );
            m.timescale.frame_b = uint32_t( rng >> 24 );
        }
        else if ( pick < 18 )               // 10% Heartbeat
        {
            m.type = MessageType::Heartbeat;
        }
        else                                // 10% Block
        {
            m.type = MessageType::Block;
            m.block.data_length = 64 + int( rng & 127 );
            for ( int i = 0; i < m.block.data_length; i++ )
                m.block.data[i] = uint8_t( rng >> ( i & 31 ) );
        }
    }

    serialize::WriteStream ws( batch_buffer, batch_buffer_size );
    for ( int k = 0; k < NumBatchMessages; k++ )
    {
        if ( !WriteMessage( ws, messages[k] ) )
        {
            fail( "message_batch", "batch write failed during setup" );
            return NULL;
        }
    }
    Message terminator;     // zero-initialized: type == None
    if ( !WriteMessage( ws, terminator ) )
    {
        fail( "message_batch", "terminator write failed during setup" );
        return NULL;
    }
    ws.Flush();
    batch_bytes = ws.GetBytesProcessed();
    return messages;
}

static void bench_batch()
{
    // worst case is NumBatchMessages * MessageMaxBytes; actual is far less.
    // + 8 read slack, and the size is a multiple of 8 (write contract).
    const int batch_buffer_size = int( ( NumBatchMessages + 1 ) * MessageMaxBytes + 8 );
    uint8_t * batch_buffer = (uint8_t *) malloc( (size_t) batch_buffer_size );
    if ( !batch_buffer )
    {
        fail( "message_batch", "allocation failed" );
        return;
    }
    memset( batch_buffer, 0, (size_t) batch_buffer_size );

    int64_t batch_bytes = 0;
    Message * messages = build_batch( batch_bytes, batch_buffer, batch_buffer_size );
    if ( !messages )
    {
        free( batch_buffer );
        return;
    }

    const long passes = BatchPasses / ( IterScale > 4 ? 4 : IterScale );    // debug: /4 only — the batch is already slow
    const long total_msgs = passes * NumBatchMessages;

    double write_rates[NumRuns];
    double read_rates[NumRuns];
    uint64_t rng = 999;

    // write path: whole batch per pass; one message mutates per pass so the
    // batch is never loop-invariant
    for ( int run = -1; run < NumRuns; run++ )
    {
        double start = time_now();
        for ( long pass = 0; pass < passes; pass++ )
        {
            rng = bench_rng( rng );
            Message & mutate = messages[( rng >> 16 ) % NumBatchMessages];
            if ( mutate.type == MessageType::Synchronize )
                mutate.synchronize.sync_frame = rng;
            else if ( mutate.type == MessageType::Test )
                mutate.test.test_a = uint16_t( rng );
            serialize::WriteStream ws( batch_buffer, batch_buffer_size );
            for ( int k = 0; k < NumBatchMessages; k++ )
            {
                if ( !WriteMessage( ws, messages[k] ) )
                {
                    fail( "message_batch", "write failed in loop" );
                    free( batch_buffer );
                    return;
                }
            }
            Message terminator;
            WriteMessage( ws, terminator );
            ws.Flush();
            bench_escape( batch_buffer );
            g_sink = g_sink + (uint64_t) ws.GetBytesProcessed();
        }
        double time = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = double( total_msgs ) / time;
    }

    // the read buffer: rebuild once from the final batch state so bytes match
    build_batch( batch_bytes, batch_buffer, batch_buffer_size );

    // read path: read messages until the None terminator, whole batch per
    // pass; ONE reused Message hoisted out of the loop (the C++ shape of the
    // go/cs MessageStorage discipline) — ReadMessage re-establishes the
    // selected arm itself (`message.arm = Arm{}` at dispatch), so reuse is
    // exact and a fresh Message per read is pure constructor overhead
    Message m;
    for ( int run = -1; run < NumRuns; run++ )
    {
        double start = time_now();
        for ( long pass = 0; pass < passes; pass++ )
        {
            serialize::ReadStream rs( batch_buffer, (int) batch_bytes );
            long count = 0;
            for ( ;; )
            {
                if ( !ReadMessage( rs, m ) )
                {
                    fail( "message_batch", "read failed in loop" );
                    free( batch_buffer );
                    return;
                }
                if ( GetMessageType( m ) == MessageType::None )
                    break;
                bench_escape( &m );
                count++;
            }
            if ( count != NumBatchMessages )
            {
                fail( "message_batch", "batch message count mismatch on read" );
                free( batch_buffer );
                return;
            }
            g_sink = g_sink + (uint64_t) count;
        }
        double time = time_now() - start;
        if ( run >= 0 )
            read_rates[run] = double( total_msgs ) / time;
    }

    const int64_t bytes_per_msg = batch_bytes / NumBatchMessages;
    report( "message_batch", "write", total_msgs, bytes_per_msg, run_stats( write_rates, NumRuns ) );
    report( "message_batch", "read", total_msgs, bytes_per_msg, run_stats( read_rates, NumRuns ) );

    free( batch_buffer );
}

// ------------------------------------------------------------------------------------------

// the message_stream golden: dispatch wire self-check (not a benchmark)
static void check_message_stream_golden()
{
    Message stream_out[3];
    // an arm is SELECTED by assigning it (establishes the arm zeroed);
    // construction alone initializes only the tag (None)
    stream_out[0].type = MessageType::Chat;
    stream_out[0].chat = Chat{};
    memcpy( stream_out[0].chat.text, "dispatch", 8 );
    stream_out[0].chat.text_length = 8;
    stream_out[1].type = MessageType::Test;
    stream_out[1].test = Test{};
    stream_out[1].test.test_b = 42;

    serialize::WriteStream ws( g_buffer, BufferSize );
    for ( const Message & m : stream_out )
    {
        if ( !WriteMessage( ws, m ) )
        {
            fail( "message_stream", "dispatch write failed" );
            return;
        }
    }
    ws.Flush();
    if ( !check_golden( "message_stream", g_buffer, ws.GetBytesProcessed() ) )
        failed = true;
}

int main( int argc, char ** argv )
{
    for ( int i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--csv" ) == 0 )
            g_csv = true;
        else if ( strcmp( argv[i], "--wire-dir" ) == 0 && i + 1 < argc )
            g_wire_dir = argv[++i];
        else
        {
            fprintf( stderr, "usage: %s [--csv] [--wire-dir <dir>]\n", argv[0] );
            return 1;
        }
    }

#if defined(NDEBUG)
    fprintf( stderr, "schema bench (cpp, Release)\n" );
#else
    fprintf( stderr, "schema bench (cpp, Debug — only release numbers are meaningful)\n" );
#endif

    if ( g_csv )
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct\n" );

    check_message_stream_golden();

    // rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
    RigidBody at_rest = pin_rigidbody_moving();
    at_rest.at_rest = true;

    bench_message( "rigidbody_moving", "rigidbody_moving", 2000000L, pin_rigidbody_moving(), WriteRigidBody, ReadRigidBody, vary_rigidbody );
    bench_message( "rigidbody_at_rest", "rigidbody_at_rest", 4000000L, at_rest,
                   WriteRigidBody, ReadRigidBody,
                   []( RigidBody & m, uint64_t rng ) {
                       m.position.x = double( int64_t( rng >> 8 ) & 0xFFFF ) * 0.25;
                       m.position.y = double( int64_t( rng >> 16 ) & 0xFFFF ) * 0.5;
                       m.orientation.x = double( int64_t( rng ) & 0xFF ) * 0.001;
                   } );
    bench_message( "chat", "chat", 4000000L, pin_chat(), WriteChat, ReadChat, vary_chat );
    bench_message( "test", (const char *) NULL, 16000000L, Test{}, WriteTest, ReadTest, vary_test );
    bench_message( "inputpacket", "inputpacket", 2000000L, pin_inputpacket(), WriteInputPacket, ReadInputPacket, vary_inputpacket );
    bench_message( "shipcreate", "shipcreate_flags", 4000000L, pin_shipcreate(), WriteShipCreate, ReadShipCreate, vary_shipcreate );
    bench_message( "ship_shallow", "ship_shallow", 4000000L, pin_ship_shallow(), WriteShipData_Shallow, ReadShipData_Shallow, vary_ship_shallow );
    bench_message( "probe_header", "probe_header", 16000000L, pin_probe_header(), WriteProbeHeader, ReadProbeHeader, vary_probe_header );
    bench_message( "probebits", "probebits", 4000000L, pin_probebits(), WriteProbeBits, ReadProbeBits, vary_probebits );
    bench_message( "probearray", "probearray", 2000000L, pin_probearray(), WriteProbeArray, ReadProbeArray, vary_probearray );
    bench_message( "testdata", "testdata", 1000000L, pin_testdata(), WriteTestData, ReadTestData, vary_testdata );

    bench_batch();

    if ( failed )
    {
        fprintf( stderr, "BENCH FAILED\n" );
        return 1;
    }

    fprintf( stderr, "OK\n" );
    ( void ) g_sink;
    return 0;
}
