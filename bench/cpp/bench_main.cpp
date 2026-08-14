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
#include <map>
#include <string>
#include <vector>

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

const int MaxNumRuns = 7;       // median of 7 (N >= 5), after 1 warmup run
static int g_num_runs = MaxNumRuns; // --round K drops this to 1 (§2.4: one warmup + one measured run per round; the driver aggregates across rounds)
const int NumVariants = 64;     // read-path variant buffers

#if defined(NDEBUG)
const long IterScale = 1;       // Release: full fixed counts
#else
const long IterScale = 8;       // Debug: fixed counts / 8 (recorded in the iters column)
#endif

static bool g_csv = false;
static const char * g_wire_dir = "testdata/wire";

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// Rows are buffered and emitted at exit so every row carries the corpus_id
// (§1.6): FNV-1a-64 over the goldens THIS RUN actually loaded — for each file
// in sorted basename order, the basename bytes, a 0x00 byte, the contents.
// The per-runner constants: family gen (these are the generated-code
// benchmarks), linkage hdr (serialize.h is header-only, same TU as the
// caller), checks removed (-DNDEBUG / SERIALIZE_RELEASE compiles range and
// bounds checks away), opt from the build (run.sh sets BENCH_OPT beside the
// -O flag itself), inline unknown until the verdict pass (§4.2) backfills it.
#ifndef BENCH_OPT
#define BENCH_OPT "O3"
#endif
// family is per ROW now (gen | rt | bits — §5.1); linkage/checks/opt/inline
// stay per-runner constants
static const char * g_csv_suffix = "hdr,removed," BENCH_OPT ",unknown";
static std::vector<std::pair<std::string, std::string>> g_csv_rows;   // (first 11 columns, family)
static std::map<std::string, std::vector<uint8_t>> g_goldens_loaded;

static uint64_t fnv1a64( uint64_t h, const uint8_t * data, size_t n )
{
    for ( size_t i = 0; i < n; i++ )
    {
        h ^= data[i];
        h *= 0x100000001b3ULL;
    }
    return h;
}

static std::string corpus_id()
{
    uint64_t h = 0xcbf29ce484222325ULL;
    for ( const auto & g : g_goldens_loaded )   // std::map iterates in sorted basename order
    {
        h = fnv1a64( h, (const uint8_t *) g.first.data(), g.first.size() );
        const uint8_t zero = 0;
        h = fnv1a64( h, &zero, 1 );
        h = fnv1a64( h, g.second.data(), g.second.size() );
    }
    char buf[17];
    snprintf( buf, sizeof( buf ), "%016llx", (unsigned long long) h );
    return std::string( buf );
}

static bool failed = false;

static void flush_csv()
{
    if ( !g_csv )
        return;
    if ( failed )
    {
        // §1.5: a failing run emits NO rows — the exit code and stderr are
        // the whole output. Numbers from a run whose gate refused are not
        // numbers.
        fprintf( stderr, "refusing to emit CSV rows from a failing run\n" );
        return;
    }
    const std::string id = corpus_id();
    for ( const auto & row : g_csv_rows )
        printf( "%s,%s,%s,%s\n", row.first.c_str(), id.c_str(), row.second.c_str(), g_csv_suffix );
}

// buffers: write buffers must be a multiple of 8 bytes (qword-flush contract);
// read allocations extend >= 8 bytes past the packet (64-bit window contract).
// 4096 covers MessageMaxBytes (2008) with slack on both contracts.
const int BufferSize = 4096;
alignas( 8 ) static uint8_t g_buffer[BufferSize];
alignas( 8 ) static uint8_t g_twin[BufferSize];
alignas( 8 ) static uint8_t g_variants[NumVariants][BufferSize];

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
    g_goldens_loaded[std::string( name ) + ".bin"].assign( expected, expected + n );
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

static void report( const char * bench, const char * path, long iters, int64_t bytes_per_op, const RunStats & s, const char * family = "gen" )
{
    const double mbps = s.median_rate * double( bytes_per_op ) / ( 1024.0 * 1024.0 );
    fprintf( stderr, "%-18s %-5s %10.2f M msg/s %10.1f MB/s   (min %.2f, max %.2f, spread %.1f%%)\n",
             bench, path, s.median_rate / 1e6, mbps, s.min_rate / 1e6, s.max_rate / 1e6, s.spread_pct );
    if ( g_csv )
    {
        char row[256];
        snprintf( row, sizeof( row ), "cpp,%s,%s,%ld,%lld,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
                  bench, path, iters, (long long) bytes_per_op, g_num_runs,
                  s.median_rate, s.min_rate, s.max_rate, mbps, s.spread_pct );
        g_csv_rows.push_back( { row, family } );
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

    double write_rates[MaxNumRuns];
    double read_rates[MaxNumRuns];

    // write path: 1 warmup + NumRuns measured
    for ( int run = -1; run < g_num_runs; run++ )
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
    for ( int run = -1; run < g_num_runs; run++ )
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

    report( name, "write", iters, bytes_per_op, run_stats( write_rates, g_num_runs ) );
    report( name, "read", iters, bytes_per_op, run_stats( read_rates, g_num_runs ) );
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
const int BatchPasses = 6400;

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

    double write_rates[MaxNumRuns];
    double read_rates[MaxNumRuns];
    uint64_t rng = 999;

    // write path: whole batch per pass; one message mutates per pass so the
    // batch is never loop-invariant
    for ( int run = -1; run < g_num_runs; run++ )
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
    for ( int run = -1; run < g_num_runs; run++ )
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
    report( "message_batch", "write", total_msgs, bytes_per_msg, run_stats( write_rates, g_num_runs ) );
    report( "message_batch", "read", total_msgs, bytes_per_msg, run_stats( read_rates, g_num_runs ) );

    free( batch_buffer );
}

// ------------------------------------------------------------------------------------------
// family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize runtime API called
// BY HAND — the four Bench.schema shapes as hand-written packets over the
// serialize_* macro surface, the way serialize/bench.cpp writes them. The
// §1.5 oracle gate byte-compares the hand-written wire against the goldens
// the GENERATED code pinned (testdata/wire/bench_*.bin) and round-trips
// before any number is produced. Internal linkage per §3.1 (anonymous
// namespace). Per §3.2 every benched Serialize instantiation has EXACTLY two
// call sites: the untimed oracle/setup helper and its timed loop.
// ------------------------------------------------------------------------------------------

#if defined(_MSC_VER)
#define BENCH_NOINLINE __declspec( noinline )
#else
#define BENCH_NOINLINE __attribute__(( noinline ))
#endif

namespace {

struct RtBenchPacket
{
    int32_t a = 0, b = 0, c = 0;
    uint32_t bits7 = 0, bits13 = 0, bits23 = 0;
    bool flag = false;
    float x = 0, y = 0, z = 0;
    uint64_t big = 0;
    uint8_t blob[17] = {};

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_int( stream, a, -100, +100 );
        serialize_int( stream, b, 0, 65535 );
        serialize_int( stream, c, -1000000, +1000000 );
        serialize_bits( stream, bits7, 7 );
        serialize_bits( stream, bits13, 13 );
        serialize_bits( stream, bits23, 23 );
        serialize_bool( stream, flag );
        serialize_float( stream, x );
        serialize_float( stream, y );
        serialize_float( stream, z );
        serialize_uint64( stream, big );
        serialize_bytes( stream, blob, (int) sizeof( blob ) );  // aligns internally — the schema says `align` out loud
        return true;
    }
};

struct RtBenchInts
{
    int32_t f0 = 0, f1 = 0, f2 = 0, f3 = 0, f4 = 0, f5 = 0, f6 = 0, f7 = 0, f8 = 0, f9 = 0;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_int( stream, f0, -100, +100 );
        serialize_int( stream, f1, 0, 65535 );
        serialize_int( stream, f2, -1000000, +1000000 );
        serialize_int( stream, f3, 0, 3 );
        serialize_int( stream, f4, -15, +15 );
        serialize_int( stream, f5, 0, 1000 );
        serialize_int( stream, f6, -2048, +2047 );
        serialize_int( stream, f7, 0, 255 );
        serialize_int( stream, f8, -600000, +600000 );
        serialize_int( stream, f9, 0, 100 );
        return true;
    }
};

struct RtBenchBits
{
    uint32_t b7 = 0, b13 = 0, b23 = 0, b3 = 0, b32 = 0, b11 = 0, b19 = 0;
    uint64_t b48 = 0;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_bits( stream, b7, 7 );
        serialize_bits( stream, b13, 13 );
        serialize_bits( stream, b23, 23 );
        serialize_bits( stream, b3, 3 );
        serialize_bits( stream, b32, 32 );
        serialize_bits( stream, b11, 11 );
        serialize_bits( stream, b19, 19 );
        serialize_bits( stream, b48, 48 );
        return true;
    }
};

struct RtBenchMixed
{
    int32_t sequence = 0;
    uint32_t ack_bits = 0, entity_id = 0;
    int32_t pos_x = 0, pos_y = 0, pos_z = 0;
    uint32_t yaw = 0;
    bool moving = false, firing = false;
    uint64_t timestamp = 0;
    int32_t weapon = 0;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_int( stream, sequence, 0, 65535 );
        serialize_bits( stream, ack_bits, 32 );
        serialize_bits( stream, entity_id, 12 );
        serialize_int( stream, pos_x, -16384, +16383 );
        serialize_int( stream, pos_y, -16384, +16383 );
        serialize_int( stream, pos_z, -16384, +16383 );
        serialize_bits( stream, yaw, 9 );
        serialize_bool( stream, moving );
        serialize_bool( stream, firing );
        serialize_bits( stream, timestamp, 48 );
        serialize_int( stream, weapon, 0, 15 );
        return true;
    }
};

// ---- pinned instances: test/bench/main.cpp (the golden producer), verbatim ----

RtBenchPacket pin_rt_packet()
{
    RtBenchPacket in;
    in.a = -37; in.b = 12345; in.c = 987654;
    in.bits7 = 97; in.bits13 = 5000; in.bits23 = 1234567;
    in.flag = true;
    in.x = 1.5f; in.y = -3.25f; in.z = 100.125f;
    in.big = 0x123456789ABCDEF0ull;
    for ( int i = 0; i < 17; i++ )
        in.blob[i] = (uint8_t) ( i * 31 );
    return in;
}

RtBenchInts pin_rt_ints()
{
    RtBenchInts in;
    in.f0 = -37; in.f1 = 12345; in.f2 = 987654; in.f3 = 2; in.f4 = -15;
    in.f5 = 777; in.f6 = -2048; in.f7 = 200; in.f8 = -543210; in.f9 = 99;
    return in;
}

RtBenchBits pin_rt_bits()
{
    RtBenchBits in;
    in.b7 = 97; in.b13 = 5000; in.b23 = 1234567; in.b3 = 5;
    in.b32 = 0xDEADBEEFu; in.b11 = 1024; in.b19 = 333333;
    in.b48 = 0xFEDCBA987654ull;
    return in;
}

RtBenchMixed pin_rt_mixed()
{
    RtBenchMixed in;
    in.sequence = 52428; in.ack_bits = 0xA5A5A5A5u; in.entity_id = 2049;
    in.pos_x = -16384; in.pos_y = 16383; in.pos_z = -1;
    in.yaw = 511; in.moving = true; in.firing = false;
    in.timestamp = 0x123456789ABCull; in.weapon = 15;
    return in;
}

// ---- vary functions: serialize/bench.cpp's field mappings, rng advanced by
// the caller (the schema-bench convention); every runner reproduces these ----

void vary_rt_packet( RtBenchPacket & p, uint64_t rng )
{
    p.a = int32_t( ( rng >> 8 ) & 63 ) - 32;
    p.b = int32_t( uint32_t( rng >> 16 ) & 65535 );
    p.c = int32_t( ( rng >> 24 ) & 0xFFFFF ) - 500000;
    p.bits7 = uint32_t( rng ) & 127;
    p.bits13 = uint32_t( rng >> 3 ) & 8191;
    p.bits23 = uint32_t( rng >> 5 ) & 8388607;
    p.flag = ( rng & 1 ) != 0;
    p.x = float( uint32_t( rng ) & 0xFFFF );
    p.big = rng;
    p.blob[0] = uint8_t( rng >> 32 );
}

void vary_rt_ints( RtBenchInts & f, uint64_t rng )
{
    f.f0 = int32_t( ( rng >> 8 ) & 63 ) - 32;
    f.f1 = int32_t( uint32_t( rng >> 16 ) & 65535 );
    f.f2 = int32_t( ( rng >> 24 ) & 0xFFFFF ) - 500000;
    f.f3 = int32_t( uint32_t( rng >> 2 ) & 3 );
    f.f4 = int32_t( ( rng >> 11 ) & 15 ) - 8;
    f.f5 = int32_t( uint32_t( rng >> 22 ) & 511 );
    f.f6 = int32_t( ( rng >> 33 ) & 2047 ) - 1024;
    f.f7 = int32_t( uint32_t( rng >> 40 ) & 255 );
    f.f8 = int32_t( ( rng >> 30 ) & 0xFFFFF ) - 500000;
    f.f9 = int32_t( uint32_t( rng >> 57 ) & 63 );
}

void vary_rt_bits( RtBenchBits & f, uint64_t rng )
{
    f.b7 = uint32_t( rng ) & 127;
    f.b13 = uint32_t( rng >> 3 ) & 8191;
    f.b23 = uint32_t( rng >> 5 ) & 8388607;
    f.b3 = uint32_t( rng >> 29 ) & 7;
    f.b32 = uint32_t( rng >> 16 );
    f.b11 = uint32_t( rng >> 37 ) & 2047;
    f.b19 = uint32_t( rng >> 44 ) & 524287;
    f.b48 = rng & 0xFFFFFFFFFFFFull;
}

void vary_rt_mixed( RtBenchMixed & f, uint64_t rng )
{
    f.sequence = int32_t( uint32_t( rng >> 8 ) & 65535 );
    f.ack_bits = uint32_t( rng >> 16 );
    f.entity_id = uint32_t( rng ) & 4095;
    f.pos_x = int32_t( ( rng >> 20 ) & 32767 ) - 16384;
    f.pos_y = int32_t( ( rng >> 25 ) & 32767 ) - 16384;
    f.pos_z = int32_t( ( rng >> 30 ) & 32767 ) - 16384;
    f.yaw = uint32_t( rng >> 3 ) & 511;
    f.moving = ( rng & 1 ) != 0;
    f.firing = ( rng & 2 ) != 0;
    f.timestamp = rng & 0xFFFFFFFFFFFFull;
    f.weapon = int32_t( uint32_t( rng >> 60 ) & 15 );
}

// ---- the single untimed call sites (§3.2): every write outside the timed
// loop goes through rt_write_once, every read through rt_read_once ----

template <typename T> int64_t rt_write_once( T & msg, uint8_t * buffer )
{
    serialize::WriteStream stream( buffer, BufferSize );
    if ( !msg.Serialize( stream ) )
        return -1;
    stream.Flush();
    return stream.GetBytesProcessed();
}

template <typename T> bool rt_read_once( T & out, const uint8_t * buffer, int64_t bytes )
{
    serialize::ReadStream stream( buffer, (int) bytes );
    return out.Serialize( stream );
}

// ---- the timed loops, one symbol per (shape, path) so the §4.1 verdict is
// a direct transitive call count over the loop body ----

#define RT_LOOPS( SHAPE, TYPE, VARY )                                                       \
BENCH_NOINLINE bool rt_##SHAPE##_write_loop( TYPE & base, long iters, uint64_t & rng )      \
{                                                                                           \
    for ( long i = 0; i < iters; i++ )                                                      \
    {                                                                                       \
        rng = bench_rng( rng );                                                             \
        VARY( base, rng );                                                                  \
        serialize::WriteStream stream( g_buffer, BufferSize );                              \
        if ( !base.Serialize( stream ) )                                                    \
            return false;                                                                   \
        stream.Flush();                                                                     \
        bench_escape( g_buffer );                                                           \
        g_sink = g_sink + (uint64_t) stream.GetBytesProcessed();                            \
    }                                                                                       \
    return true;                                                                            \
}                                                                                           \
BENCH_NOINLINE bool rt_##SHAPE##_read_loop( TYPE & out, long iters, int64_t bytes_per_op )  \
{                                                                                           \
    for ( long i = 0; i < iters; i++ )                                                      \
    {                                                                                       \
        serialize::ReadStream stream( g_variants[i & ( NumVariants - 1 )], (int) bytes_per_op ); \
        if ( !out.Serialize( stream ) )                                                     \
            return false;                                                                   \
        bench_escape( &out );                                                               \
        g_sink = g_sink + 1;                                                                \
    }                                                                                       \
    return true;                                                                            \
}

RT_LOOPS( bench_packet, RtBenchPacket, vary_rt_packet )
RT_LOOPS( bench_ints, RtBenchInts, vary_rt_ints )
RT_LOOPS( bench_bits, RtBenchBits, vary_rt_bits )
RT_LOOPS( bench_mixed, RtBenchMixed, vary_rt_mixed )

// ---- the family rt driver: §1.5 oracle gate, then the timed loops ----

template <typename T, typename WriteLoop, typename ReadLoop, typename VaryFn>
void bench_rt( const char * name, long base_iters, const T & pinned,
               WriteLoop write_loop, ReadLoop read_loop, VaryFn vary_fn )
{
    const long iters = base_iters / IterScale;

    // oracle 1: the pinned instance through the HAND-WRITTEN path must match
    // the golden the GENERATED code pinned, byte for byte
    T base = pinned;
    const int64_t bytes_per_op = rt_write_once( base, g_buffer );
    if ( bytes_per_op < 0 )
    {
        fail( name, "write of pinned instance failed" );
        return;
    }
    if ( !check_golden( name, g_buffer, bytes_per_op ) )
    {
        failed = true;
        return;
    }

    // oracle 2: round-trip write -> read -> re-write -> identical bytes
    T out;
    if ( !rt_read_once( out, g_buffer, bytes_per_op ) )
    {
        fail( name, "read of pinned instance failed" );
        return;
    }
    if ( rt_write_once( out, g_twin ) != bytes_per_op ||
         memcmp( g_buffer, g_twin, (size_t) bytes_per_op ) != 0 )
    {
        fail( name, "round-trip bytes differ" );
        return;
    }

    // variant buffers for the read path (and proof that variation keeps bytes/op constant)
    uint64_t rng = 1;
    for ( int k = 0; k < NumVariants; k++ )
    {
        rng = bench_rng( rng );
        vary_fn( base, rng );
        if ( rt_write_once( base, g_variants[k] ) != bytes_per_op )
        {
            fail( name, "variation changed bytes/op — vary must keep structure fields fixed" );
            return;
        }
    }

    double write_rates[MaxNumRuns];
    double read_rates[MaxNumRuns];

    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        if ( !write_loop( base, iters, rng ) )
        {
            fail( name, "write failed in loop" );
            return;
        }
        double time = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = double( iters ) / time;
    }

    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        if ( !read_loop( out, iters, bytes_per_op ) )
        {
            fail( name, "read failed in loop" );
            return;
        }
        double time = time_now() - start;
        if ( run >= 0 )
            read_rates[run] = double( iters ) / time;
    }

    report( name, "write", iters, bytes_per_op, run_stats( write_rates, g_num_runs ), "rt" );
    report( name, "read", iters, bytes_per_op, run_stats( read_rates, g_num_runs ), "rt" );
}

// ------------------------------------------------------------------------------------------
// family bits (BENCH-STANDARD.md §1.4): the raw bit packer, the ONE bitpacker
// workload in the estate — the 16-width table (227 bits/group) over a
// 65536-byte buffer. Values vary per pass through the LCG (widths are the
// structure and stay fixed; bytes/pass is asserted constant), reads rotate 64
// pre-written variant buffers, and setup verifies every variant reads back
// exactly the values written before any number is produced.
// ------------------------------------------------------------------------------------------

const int BitsNumWidths = 16;
const int bits_widths[BitsNumWidths] = { 1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22 };   // 227 bits/group
const int BitsBufferSize = 65536;

alignas( 8 ) uint8_t g_bits_buffer[BitsBufferSize + 8];
alignas( 8 ) uint8_t g_bits_variants[NumVariants][BitsBufferSize + 8];

uint32_t bits_mask( int width )
{
    return ( width == 32 ) ? 0xFFFFFFFFu : ( ( 1u << width ) - 1 );
}

// the per-pass value variation: one LCG step per pass, values from its bits
void vary_bits_values( uint32_t * values, uint64_t rng )
{
    for ( int i = 0; i < BitsNumWidths; i++ )
        values[i] = uint32_t( rng >> i ) & bits_mask( bits_widths[i] );
}

// the single untimed WriteBits call site (§3.2): fills one buffer with groups
int64_t bits_write_pass( uint8_t * buffer, const uint32_t * values )
{
    serialize::BitWriter writer( buffer, BitsBufferSize );
    while ( writer.GetBitsAvailable() >= 256 )
    {
        for ( int i = 0; i < BitsNumWidths; i++ )
            writer.WriteBits( values[i], bits_widths[i] );
    }
    writer.FlushBits();
    return writer.GetBytesWritten();
}

// the single untimed ReadBits call site (§3.2): verifies a buffer reads back
// exactly the values written into it — the bits family's refuse-to-bench gate
bool bits_read_verify( const uint8_t * buffer, const uint32_t * values )
{
    serialize::BitReader reader( buffer, BitsBufferSize );
    while ( reader.GetBitsRemaining() >= 256 )
    {
        for ( int i = 0; i < BitsNumWidths; i++ )
        {
            if ( reader.ReadBits( bits_widths[i] ) != values[i] )
                return false;
        }
    }
    return true;
}

BENCH_NOINLINE bool bitpacker_write_loop( long passes, int64_t bytes_per_pass, uint64_t & rng, uint32_t * values )
{
    for ( long pass = 0; pass < passes; pass++ )
    {
        rng = bench_rng( rng );
        vary_bits_values( values, rng );
        serialize::BitWriter writer( g_bits_buffer, BitsBufferSize );
        while ( writer.GetBitsAvailable() >= 256 )
        {
            for ( int i = 0; i < BitsNumWidths; i++ )
                writer.WriteBits( values[i], bits_widths[i] );
        }
        writer.FlushBits();
        if ( writer.GetBytesWritten() != bytes_per_pass )
            return false;       // the bytes_per_op assertion (§2.7)
        bench_escape( g_bits_buffer );
        g_sink = g_sink + (uint64_t) writer.GetBytesWritten();
    }
    return true;
}

BENCH_NOINLINE bool bitpacker_read_loop( long passes )
{
    for ( long pass = 0; pass < passes; pass++ )
    {
        serialize::BitReader reader( g_bits_variants[pass & ( NumVariants - 1 )], BitsBufferSize );
        uint64_t sum = 0;
        while ( reader.GetBitsRemaining() >= 256 )
        {
            for ( int i = 0; i < BitsNumWidths; i++ )
                sum += reader.ReadBits( bits_widths[i] );
        }
        g_sink = g_sink + sum;
    }
    return true;
}

void bench_bitpacker( long base_passes )
{
    const long passes = base_passes / IterScale;
    uint32_t values[BitsNumWidths];

    // setup: fill the 64 read variants (each its own LCG pass values), assert
    // bytes/pass constant, and verify every variant reads back exactly
    uint64_t rng = 1;
    int64_t bytes_per_pass = -1;
    for ( int k = 0; k < NumVariants; k++ )
    {
        rng = bench_rng( rng );
        vary_bits_values( values, rng );
        const int64_t wrote = bits_write_pass( g_bits_variants[k], values );
        if ( bytes_per_pass < 0 )
            bytes_per_pass = wrote;
        if ( wrote != bytes_per_pass )
        {
            fail( "bitpacker", "variation changed bytes/pass — widths are the structure and must stay fixed" );
            return;
        }
        if ( !bits_read_verify( g_bits_variants[k], values ) )
        {
            fail( "bitpacker", "read-back disagrees with written values — refusing to bench" );
            return;
        }
    }

    double write_rates[MaxNumRuns];
    double read_rates[MaxNumRuns];

    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        if ( !bitpacker_write_loop( passes, bytes_per_pass, rng, values ) )
        {
            fail( "bitpacker", "bytes/pass changed in the timed loop (§2.7 assertion)" );
            return;
        }
        double time = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = double( passes ) / time;
    }

    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        if ( !bitpacker_read_loop( passes ) )
        {
            fail( "bitpacker", "read loop failed" );
            return;
        }
        double time = time_now() - start;
        if ( run >= 0 )
            read_rates[run] = double( passes ) / time;
    }

    report( "bitpacker", "write", passes, bytes_per_pass, run_stats( write_rates, g_num_runs ), "bits" );
    report( "bitpacker", "read", passes, bytes_per_pass, run_stats( read_rates, g_num_runs ), "bits" );
}

} // anonymous namespace

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
        else if ( strcmp( argv[i], "--round" ) == 0 && i + 1 < argc )
        {
            // §2.4: one warmup + one measured run of every benchmark, then
            // exit. K only identifies the round to the interleaved driver,
            // which aggregates max/median/min/spread across rounds itself.
            char * end = NULL;
            long k = strtol( argv[++i], &end, 10 );
            if ( end == argv[i] || *end != '\0' || k < 0 )
            {
                fprintf( stderr, "--round takes a non-negative integer, got '%s'\n", argv[i] );
                return 1;
            }
            g_num_runs = 1;
        }
        else
        {
            fprintf( stderr, "usage: %s [--csv] [--round K] [--wire-dir <dir>]\n", argv[0] );
            return 1;
        }
    }

#if defined(NDEBUG)
    fprintf( stderr, "schema bench (cpp, Release)\n" );
#else
    fprintf( stderr, "schema bench (cpp, Debug — only release numbers are meaningful)\n" );
#endif

    if ( g_csv )
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n" );

    check_message_stream_golden();

    // rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
    RigidBody at_rest = pin_rigidbody_moving();
    at_rest.at_rest = true;

    bench_message( "rigidbody_moving", "rigidbody_moving", 24000000L, pin_rigidbody_moving(), WriteRigidBody, ReadRigidBody, vary_rigidbody );
    bench_message( "rigidbody_at_rest", "rigidbody_at_rest", 32000000L, at_rest,
                   WriteRigidBody, ReadRigidBody,
                   []( RigidBody & m, uint64_t rng ) {
                       m.position.x = double( int64_t( rng >> 8 ) & 0xFFFF ) * 0.25;
                       m.position.y = double( int64_t( rng >> 16 ) & 0xFFFF ) * 0.5;
                       m.orientation.x = double( int64_t( rng ) & 0xFF ) * 0.001;
                   } );
    bench_message( "chat", "chat", 48000000L, pin_chat(), WriteChat, ReadChat, vary_chat );
    bench_message( "test", (const char *) NULL, 192000000L, Test{}, WriteTest, ReadTest, vary_test );
    bench_message( "inputpacket", "inputpacket", 16000000L, pin_inputpacket(), WriteInputPacket, ReadInputPacket, vary_inputpacket );
    bench_message( "shipcreate", "shipcreate_flags", 32000000L, pin_shipcreate(), WriteShipCreate, ReadShipCreate, vary_shipcreate );
    bench_message( "ship_shallow", "ship_shallow", 32000000L, pin_ship_shallow(), WriteShipData_Shallow, ReadShipData_Shallow, vary_ship_shallow );
    bench_message( "probe_header", "probe_header", 256000000L, pin_probe_header(), WriteProbeHeader, ReadProbeHeader, vary_probe_header );
    bench_message( "probebits", "probebits", 128000000L, pin_probebits(), WriteProbeBits, ReadProbeBits, vary_probebits );
    bench_message( "probearray", "probearray", 20000000L, pin_probearray(), WriteProbeArray, ReadProbeArray, vary_probearray );
    bench_message( "testdata", "testdata", 8000000L, pin_testdata(), WriteTestData, ReadTestData, vary_testdata );

    bench_batch();

    // family rt (§1.3/§1.5): the runtime API by hand, oracle-gated against
    // the goldens the generated code pinned. Iteration counts are fixed and
    // identical across all five runners, sized so the FASTEST language's
    // fastest path exceeds §2.1's 200 ms floor on the M2 (measured
    // 2026-08-14; the C++ reads at ~110-180 M msg/s are the binding paths).
    bench_rt( "bench_packet", 32000000L, pin_rt_packet(), rt_bench_packet_write_loop, rt_bench_packet_read_loop, vary_rt_packet );
    bench_rt( "bench_ints", 40000000L, pin_rt_ints(), rt_bench_ints_write_loop, rt_bench_ints_read_loop, vary_rt_ints );
    bench_rt( "bench_bits", 48000000L, pin_rt_bits(), rt_bench_bits_write_loop, rt_bench_bits_read_loop, vary_rt_bits );
    bench_rt( "bench_mixed", 40000000L, pin_rt_mixed(), rt_bench_mixed_write_loop, rt_bench_mixed_read_loop, vary_rt_mixed );

    // family bits (§1.4): the one bitpacker workload in the estate. 24576
    // passes, not the historical 4096 — at 4096 the C++ read leg finishes in
    // ~170 ms, under §2.1's 200 ms floor (measured; §1.4 records this).
    bench_bitpacker( 24576L );

    flush_csv();    // rows carry the corpus_id of the goldens this run loaded

    if ( failed )
    {
        fprintf( stderr, "BENCH FAILED (corpus_id %s)\n", corpus_id().c_str() );
        return 1;
    }

    fprintf( stderr, "OK (corpus_id %s)\n", corpus_id().c_str() );
    ( void ) g_sink;
    return 0;
}
