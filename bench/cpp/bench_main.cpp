// schema bench — the C++ runner.
//
// Measures the schema-GENERATED C++ code (generated/cpp) against the classic
// serialize runtime: write path and read path, ops/sec and MB/sec, over
// the pinned corpus instances (the same instances test/main.cpp pins to the
// wire goldens in testdata/wire).
//
// Methodology (follows the serialize repo's bench.cpp conventions — see the
// const-params experiment for the reasoning behind the escape barriers and
// the per-iteration LCG variation):
//   - every write loop varies fields per iteration through a serially
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
#include "EnumsWire.h"
#include "TypesWire.h"
#include "WireWire.h"
#include "RealWorldWire.h"      // generated/bench/cpp — the §1.7 realistic snapshot (real_packet)

// TWO schema UNITS ride in this one translation unit (package example above,
// package bench below). The generated string helpers schema_utf8_valid and
// schema_interior_null are emitted INSIDE each unit's namespace but guarded by
// a translation-unit-wide #define, so the second unit's namespace never gets
// them and its string reads do not compile. Clearing the guards makes the
// bench unit emit its own copies. LEG-LOCAL by necessity: the emitters stay
// byte-unchanged in this PR. The guard-vs-namespace mismatch is an emitter
// defect owned by issue #189; REMOVE THIS WORKAROUND when #189 lands.
#undef SCHEMA_UTF8_VALID_DEFINED
#undef SCHEMA_INTERIOR_NULL_DEFINED
#include "BenchWire.h"          // generated/bench/cpp — the Bench corpus GENERATED (the gen twins of the rt rows, issue #177)

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
static bool g_quick = false;    // --quick: bench_mixed only, 3 measured runs — the iteration instrument, never the certification instrument
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
// 4096 covers the largest pinned shape (2008 bytes) with slack on both contracts.
const int BufferSize = 4096;
// §2.7 variant-buffer stride: the 64 rotating read buffers are allocated at
// BufferSize + 64 per slot, NOT packed at exact 4096. At stride 4096 every
// head line maps into one of 4 L1 set-groups on the M2 (set bits [13:6]:
// 4096 >> 6 = 64 sets per step, 64k mod 256 cycles {0,64,128,192}), and a
// fully-inlined memory-bound read feels every background conflict miss in
// those sets — measured 2026-08-15 as 8–18% cpp read spreads beside 0.1%
// out-of-line C reads. At 4160 the step is 65 and gcd(65,256) = 1: 64 head
// lines, 64 distinct sets. Identical in all five runners. The buffer passed
// to the streams stays BufferSize; the pad is address spacing only.
const int VariantStride = BufferSize + 64;
alignas( 8 ) static uint8_t g_buffer[BufferSize];
alignas( 8 ) static uint8_t g_twin[BufferSize];
alignas( 8 ) static uint8_t g_variants[NumVariants][VariantStride];

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
// the per-shape benchmark driver
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

// real_packet — BENCH-STANDARD.md §1.7's realistic snapshot, measured through
// the GENERATED code (bench/corpus/RealWorld.schema -> generated/bench/cpp).
// The pinned instance is the ALL-DEFAULTS instance: realworld::RealPacket{}
// serialized unmodified, 1629 bits = 204 wire bytes, pinned to
// testdata/wire/real_packet.bin by test/bench/main.cpp. The four branch gates
// (f012 true, f043 false, f050 true, f074 false) are STRUCTURE (§2.7): they
// keep their schema defaults here, so the same branch bodies ride every
// iteration and bytes/op is constant. Variation covers every field kind the
// packet carries — ranged ints of assorted widths, bits(N) narrow and wide,
// bool, float32/float64, compressed float, fixed/ufixed, enum, flags, and the
// full-width 64s — spread across the packet's whole span including the two
// TAKEN branch bodies; fields under the false gates do not ride and are not
// varied. Every mapping keeps its field inside its declared wire range
// (comments give the bound it stays within). Ports reproduce these mappings
// exactly.
static void vary_real_packet( realworld::RealPacket & m, uint64_t rng )
{
    // ranged ints, assorted widths, signed and unsigned
    m.f001_int  = int32_t( ( rng >> 8 ) & 0xFFFFF ) - 0x80000;              // +/-2^19 within +/-805495
    m.f003_int  = int32_t( ( rng >> 12 ) & 0xFFFFF ) - 0x80000;             // within +/-835897
    m.f005_uint = uint16_t( ( rng >> 20 ) & 0xFFF );                        // <=4095 within [0, 7316]
    m.f006_int  = int16_t( int32_t( ( rng >> 26 ) & 0x7FF ) - 1024 );       // +/-1024 within +/-1513
    m.f009_int  = int8_t( int32_t( ( rng >> 33 ) & 31 ) - 16 );             // +/-16 within +/-22
    m.f033_uint = uint32_t( ( rng >> 37 ) & 0x1FFFF );                      // <=131071 within [0, 142780]
    m.f041_int  = int8_t( int32_t( ( rng >> 42 ) & 63 ) - 32 );             // +/-32 within +/-55
    m.f062_uint = uint16_t( ( rng >> 47 ) & 255 );                          // <=255 within [0, 503]
    m.f088_int  = int16_t( int32_t( ( rng >> 52 ) & 0x3FF ) - 512 );        // +/-512 within +/-694
    m.f090_uint = uint8_t( ( rng >> 57 ) & 127 );                           // <=127 within [0, 214]
    // bits(N), narrow and wide
    m.f011_bits = uint32_t( rng ) & 0x3FF;                                  // 10 bits
    m.f023_bits = uint32_t( rng >> 5 ) & 0x1FFFFFF;                         // 25 bits
    m.f042_bits = uint32_t( rng >> 3 ) & 0x3FFFFFFF;                        // 30 bits
    m.f081_bits = uint32_t( rng >> 7 ) & 0x1FFFFFFF;                        // 29 bits
    m.f089_bits = rng & 0xFFFFFFFFFFFFull;                                  // 48 bits
    m.f093_bits = rng ^ 0x5555555555555555ull;                              // 64 bits
    m.f097_bits = uint32_t( rng >> 11 ) & 0xFFF;                            // 12 bits
    // bools (NEVER the four branch gates — those are structure, §2.7)
    m.f037_bool = ( rng & 1 ) != 0;
    m.f055_bool = ( rng & 2 ) != 0;
    m.f092_bool = ( rng & 4 ) != 0;
    // float32 / float64
    m.f007_f32 = float( uint32_t( rng ) & 0xFFFF );
    m.f020_f32 = float( uint32_t( rng >> 16 ) & 0xFFFF ) * 0.5f;
    m.f058_f32 = float( uint32_t( rng >> 24 ) & 0xFFFF ) * 0.25f;
    m.f002_f64 = double( int64_t( rng >> 8 ) & 0xFFFFFF ) * 0.5;
    m.f059_f64 = double( int64_t( rng >> 16 ) & 0xFFFFFF ) * 0.25;
    m.f087_f64 = double( int64_t( rng >> 24 ) & 0xFFFFFF ) * 0.125;
    // compressed floats (in range by construction)
    m.f004_cf32 = float( uint32_t( rng ) & 0x3FFF ) * 0.1f;                 // <=1638.3 within [0, 2000]
    m.f061_cf32 = -90.0f + float( uint32_t( rng >> 9 ) & 255 ) * 0.5f;      // within [-90, 90] (max 37.5)
    m.f067_cf32 = -100.0f + float( uint32_t( rng >> 18 ) & 511 ) * 0.25f;   // within [-100, 100] (max 27.75)
    m.f072_cf32 = float( uint32_t( rng >> 27 ) & 8191 ) * 0.01f;            // <=81.91 within [0, 100]
    // fixed / ufixed (raw storage scaled by 2^F; bounds are whole units)
    m.f016_fixed  = int32_t( ( rng >> 10 ) & 0x3FFFFFF ) - 0x2000000;       // +/-2^25 within +/-36*2^20
    m.f025_fixed  = int16_t( int32_t( ( rng >> 18 ) & 0x7FFF ) - 0x4000 );  // +/-2^14 within +/-119*2^8
    m.f095_fixed  = int32_t( ( rng >> 22 ) & 0x7FFFFFF ) - 0x4000000;       // +/-2^26 within +/-1577*2^16
    m.f021_ufixed = uint32_t( rng >> 30 ) & 0x3FFFFFF;                      // <=2^26-1 within 25141*2^12
    m.f049_ufixed = uint16_t( ( rng >> 36 ) & 0x7FFF );                     // <=32767 within 3*2^14
    m.f084_ufixed = uint8_t( ( rng >> 44 ) & 0x7F );                        // <=127 within 1*2^7
    // enum / flags (wire-valid by construction)
    m.f036_enum  = realworld::PacketMode( uint32_t( rng >> 30 ) & 3 );      // within wire range [0, 5]
    m.f083_enum  = realworld::PacketMode( uint32_t( rng >> 34 ) & 3 );
    m.f091_flags = rng & 31;                                                // 5 wire bits
    // full-width 64-bit
    m.f008_u64 = rng;
    m.f029_i64 = int64_t( rng * 3 );
    m.f063_i64 = int64_t( rng * 5 );
    // fields riding inside the TAKEN branches (f012 true, f050 true)
    m.f013_f32  = float( uint32_t( rng >> 4 ) & 0xFFFF );
    m.f014_uint = uint16_t( ( rng >> 21 ) & 511 );                          // <=511 within [0, 775]
    m.f015_int  = int8_t( int32_t( ( rng >> 40 ) & 31 ) - 16 );             // +/-16 within +/-21
    m.f017_uint = uint16_t( ( rng >> 29 ) & 0xFFF );                        // <=4095 within [0, 4606]
    m.f051_bool = ( rng & 8 ) != 0;
    m.f052_int  = int8_t( int32_t( ( rng >> 38 ) & 63 ) - 32 );             // +/-32 within +/-57
    m.f053_f32  = float( uint32_t( rng >> 40 ) & 0xFFFF ) * 0.125f;
    m.f054_int  = int8_t( int32_t( ( rng >> 45 ) & 63 ) - 32 );             // +/-32 within +/-35
}

// ------------------------------------------------------------------------------------------
// family gen over the Bench corpus (issue #177): the four Bench.schema shapes
// measured through the GENERATED code (generated/bench/cpp/BenchWire.h) — the
// gen twins of the rt rows below, which serialize the same shapes BY HAND
// against the runtime API. Same golden files, same pinned values, same LCG
// field mappings, same bench_message discipline as every gen row above; the
// family column carries the subject, and relative.go refuses gen-vs-rt
// ratios. Generated best case per the profiling doctrine (#170): the plain
// optimized release build of the emitted code, no PGO (#175 runs later).
// ------------------------------------------------------------------------------------------

static bench::BenchPacket pin_gen_packet()
{
    bench::BenchPacket in;
    in.a = -37; in.b = 12345; in.c = 987654;
    in.bits7 = 97; in.bits13 = 5000; in.bits23 = 1234567;
    in.flag = true;
    in.x = 1.5f; in.y = -3.25f; in.z = 100.125f;
    in.big = 0x123456789ABCDEF0ull;
    for ( int i = 0; i < 17; i++ )
        in.blob[i] = (uint8_t) ( i * 31 );
    return in;
}

static bench::BenchInts pin_gen_ints()
{
    bench::BenchInts in;
    in.f0 = -37; in.f1 = 12345; in.f2 = 987654; in.f3 = 2; in.f4 = -15;
    in.f5 = 777; in.f6 = -2048; in.f7 = 200; in.f8 = -543210; in.f9 = 99;
    return in;
}

static bench::BenchBits pin_gen_bits()
{
    bench::BenchBits in;
    in.b7 = 97; in.b13 = 5000; in.b23 = 1234567; in.b3 = 5;
    in.b32 = 0xDEADBEEFu; in.b11 = 1024; in.b19 = 333333;
    in.b48 = 0xFEDCBA987654ull;
    return in;
}

// BenchMixed — THE canonical benchmark shape (issue #184). The pin is
// test/bench/main.cpp's, transcribed exactly; STRUCTURE fields (the two
// array counts, the two used lengths, the union tag, the `if` gate) are set
// here and never touched by vary_*, so bytes/op is constant (§2.7).
static bench::BenchMixed pin_gen_mixed()
{
    bench::BenchMixed in;
    in.sequence = 52428; in.ack_sequence = 12345; in.ack_bits = 0xA5A5A5A5u;
    in.session_id = 0x123456789ABCDEF0ull; in.client_id = 0xDEADBEEFu;
    in.nonce = 0xFEDCBA9876543210ull; in.world_time = -987654321000ll;
    in.frame_tick = 0x123456789ABCull; in.server_time = 12345678;
    in.entities_count = 8;
    for ( int i = 0; i < 8; i++ )
    {
        bench::MixedEntity & e = in.entities[i];
        e.entity_id = (uint32_t) ( 2049 + i * 17 );
        e.pos_x = -16383 + i * 4096; e.pos_y = 16383 - i * 4096; e.pos_z = -1 + i * 2048;
        e.yaw = (uint32_t) ( 511 - i * 64 ); e.pitch = (uint32_t) ( i * 73 );
        e.vel_x = -2048 + i * 512; e.vel_y = 2047 - i * 512; e.vel_z = -1024 + i * 256;
        e.health = 1000 - i * 100;
        e.weapon = bench::MixedWeapon( 1 + i );
        e.damage = (bench::MixedDamage) ( 0x5A + i );
        e.moving = ( i % 2 ) == 0; e.firing = ( i % 3 ) == 0;
    }
    in.stats_count = 80;
    for ( int i = 0; i < 80; i++ )
    {
        in.stats[i].stat_id = (uint32_t) ( ( i * 3 ) % 256 );
        in.stats[i].delta = -512 + ( i * 13 ) % 1024;
    }
    in.game_event.type = bench::MixedEventType::Hit;
    in.game_event.hit.target_id = 4095; in.game_event.hit.damage = 4095;
    in.game_event.hit.hit_kind = 7; in.game_event.hit.crit = true;
    in.loadout[0] = 0x11; in.loadout[1] = 0x22; in.loadout[2] = 0x33; in.loadout[3] = 0x44;
    memcpy( in.player_name, "Rowan_01", 8 ); in.player_name_length = 8;
    static const uint8_t pinned_payload[8] = { 0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04 };
    memcpy( in.payload, pinned_payload, 8 ); in.payload_length = 8;
    in.aim_x = 0.5f; in.aim_y = -0.25f; in.aim_z = 0.75f;
    in.recoil = 1.5f; in.drift = -3.25;
    in.wide_key = ( serialize::uint128_t( 0x0123456789ABCDEFull ) << 64 ) | serialize::uint128_t( 0xFEDCBA9876543210ull );
    in.flux = ( serialize::int128_t( 1 ) << 99 ) + serialize::int128_t( 7 );
    in.ping = 12345; in.crc_hint = 0xABCDEFu;
    in.has_extra = true; in.extra = 200;
    return in;
}

static void vary_gen_packet( bench::BenchPacket & p, uint64_t rng )
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

static void vary_gen_ints( bench::BenchInts & f, uint64_t rng )
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

static void vary_gen_bits( bench::BenchBits & f, uint64_t rng )
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

// The LCG field mapping for BenchMixed, reproduced in every runner. VALUE
// fields only: every array count, used length, union tag and branch gate is
// STRUCTURE and stays where pin_* put it (§2.7). Every entity element varies;
// the 80 stats vary their `delta` (stat_id stays pinned) — the family
// convention of varying a representative subset, stated out loud.
static void vary_gen_mixed( bench::BenchMixed & f, uint64_t rng )
{
    f.sequence = uint32_t( rng >> 8 ) & 65535;
    f.ack_sequence = int32_t( uint32_t( rng >> 24 ) & 65535 );
    f.ack_bits = uint32_t( rng >> 16 );
    f.session_id = rng;
    f.client_id = uint32_t( rng >> 32 );
    f.nonce = rng ^ 0xA5A5A5A5A5A5A5A5ull;
    f.world_time = int64_t( ( rng >> 12 ) & 0xFFFFFFFFFull ) - 34359738368ll;   // within +/-1e12
    f.frame_tick = rng & 0xFFFFFFFFFFFFull;
    f.server_time = int32_t( ( rng >> 20 ) & 0x7FFFFF );                        // raw Q24.8 <= 65535 << 8
    for ( int i = 0; i < 8; i++ )
    {
        bench::MixedEntity & e = f.entities[i];
        e.entity_id = uint32_t( ( rng >> i ) & 4095 );
        e.pos_x = int32_t( ( rng >> ( i + 4 ) ) & 16383 ) - 8192;
        e.pos_y = int32_t( ( rng >> ( i + 12 ) ) & 16383 ) - 8192;
        e.health = int32_t( ( rng >> ( i + 20 ) ) & 511 );                      // within [0, 1000]
        e.weapon = bench::MixedWeapon( ( rng >> ( i + 40 ) ) & 15 );
        e.damage = (bench::MixedDamage) ( ( rng >> ( i + 28 ) ) & 255 );
        e.moving = ( ( rng >> i ) & 1 ) != 0;
    }
    for ( int i = 0; i < 80; i++ )
        f.stats[i].delta = int32_t( ( rng >> ( i & 31 ) ) & 1023 ) - 512;
    f.game_event.hit.target_id = uint32_t( ( rng >> 6 ) & 4095 );
    f.game_event.hit.damage = int32_t( ( rng >> 18 ) & 4095 );
    f.game_event.hit.hit_kind = int32_t( ( rng >> 30 ) & 7 );
    f.game_event.hit.crit = ( rng & 4 ) != 0;
    f.loadout[0] = uint8_t( rng >> 56 );
    f.player_name[7] = char( 65 + ( ( rng >> 50 ) & 15 ) );                     // stays ASCII, never NUL
    f.payload[0] = uint8_t( rng >> 48 );
    f.aim_x = float( uint32_t( rng >> 2 ) & 255 ) * ( 1.0f / 256.0f ) - 0.5f;   // within [-1, 1]
    f.aim_y = float( uint32_t( rng >> 10 ) & 255 ) * ( 1.0f / 256.0f ) - 0.5f;
    f.aim_z = float( uint32_t( rng >> 18 ) & 255 ) * ( 1.0f / 256.0f ) - 0.5f;
    f.recoil = float( uint32_t( rng ) & 0xFFFF );
    f.drift = double( int64_t( ( rng >> 8 ) & 0xFFFFFF ) ) * 0.5;
    f.wide_key = ( serialize::uint128_t( rng >> 1 ) << 64 ) | serialize::uint128_t( rng );
    f.flux = serialize::int128_t( int64_t( rng >> 16 ) );                       // well within +/-2^100
    f.ping = uint16_t( ( rng >> 40 ) & 0x7FFF );                                // raw UQ8.8 <= 250 << 8
    f.crc_hint = uint32_t( ( rng >> 24 ) & 0xFFFFFF );
    f.extra = int32_t( ( rng >> 52 ) & 255 );
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

// BenchMixed by hand (issue #184). Every serialize_* stream operation the
// schema language expresses, in the order BenchWire.h emits them; the §1.5
// oracle gate byte-compares this against the generated code's golden.
struct RtMixedEntity
{
    uint32_t entity_id = 0;
    int32_t pos_x = 0, pos_y = 0, pos_z = 0;
    uint32_t yaw = 0, pitch = 0;
    int32_t vel_x = 0, vel_y = 0, vel_z = 0;
    int32_t health = 0;
    int32_t weapon = 0;      // the enum wire: serialize_int over [0, Max]
    uint64_t damage = 0;     // the flags wire: raw bits, one per variant
    bool moving = false, firing = false;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_bits( stream, entity_id, 12 );
        serialize_int( stream, pos_x, -16383, +16383 );
        serialize_int( stream, pos_y, -16383, +16383 );
        serialize_int( stream, pos_z, -16383, +16383 );
        serialize_bits( stream, yaw, 9 );
        serialize_bits( stream, pitch, 9 );
        serialize_int( stream, vel_x, -2048, +2047 );
        serialize_int( stream, vel_y, -2048, +2047 );
        serialize_int( stream, vel_z, -2048, +2047 );
        serialize_int( stream, health, 0, 1000 );
        serialize_int( stream, weapon, 0, 15 );
        serialize_bits( stream, damage, 8 );
        serialize_bool( stream, moving );
        serialize_bool( stream, firing );
        return true;
    }
};

struct RtMixedStat
{
    uint32_t stat_id = 0;
    int32_t delta = 0;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_bits( stream, stat_id, 8 );
        serialize_int( stream, delta, -512, +511 );
        return true;
    }
};

struct RtMixedHitEvent
{
    uint32_t target_id = 0;
    int32_t damage = 0, hit_kind = 0;
    bool crit = false;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_bits( stream, target_id, 12 );
        serialize_int( stream, damage, 0, 4095 );
        serialize_int( stream, hit_kind, 0, 7 );
        serialize_bool( stream, crit );
        return true;
    }
};

struct RtMixedChatEvent
{
    int32_t channel = 0;
    uint32_t speaker = 0;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_int( stream, channel, 0, 3 );
        serialize_bits( stream, speaker, 12 );
        return true;
    }
};

struct RtMixedPickupEvent
{
    uint32_t item_id = 0;
    int32_t amount = 0;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_bits( stream, item_id, 10 );
        serialize_int( stream, amount, 0, 255 );
        return true;
    }
};

struct RtBenchMixed
{
    uint32_t magic = 0xC0DE;
    uint32_t sequence = 0;
    int32_t ack_sequence = 0;
    uint32_t ack_bits = 0;
    uint64_t session_id = 0;
    uint32_t client_id = 0;
    uint64_t nonce = 0;
    int64_t world_time = 0;
    uint64_t frame_tick = 0;
    int32_t server_time = 0;              // raw Q24.8

    int32_t entities_count = 0;
    RtMixedEntity entities[8];
    int32_t stats_count = 0;
    RtMixedStat stats[80];

    int32_t event_type = 0;               // the union tag: 0 = None
    RtMixedHitEvent hit;
    RtMixedChatEvent chat;
    RtMixedPickupEvent pickup;

    uint8_t loadout[4] = {};
    int32_t player_name_length = 0;
    char player_name[16] = {};
    int32_t payload_length = 0;
    uint8_t payload[16] = {};

    float aim_x = 0, aim_y = 0, aim_z = 0;
    float recoil = 0;
    double drift = 0;
    serialize::uint128_t wide_key = 0;
    serialize::int128_t flux = 0;
    uint16_t ping = 0;                     // raw UQ8.8
    uint32_t reserved_bits = 0;
    uint32_t crc_hint = 0;
    bool has_extra = true;
    int32_t extra = 0, idle_ticks = 0;

    template <typename Stream> bool Serialize( Stream & stream )
    {
        serialize_bits( stream, magic, 16 );
        if ( magic != 0xC0DE )
            return false;                  // const(0xC0DE, 16): a read REJECTS any other value
        serialize_bits( stream, sequence, 16 );
        serialize_int( stream, ack_sequence, 0, 65535 );
        serialize_bits( stream, ack_bits, 32 );
        serialize_uint64( stream, session_id );
        serialize_uint32( stream, client_id );
        serialize_bits( stream, nonce, 64 );   // the full-unsigned ranged path is width-computed bits
        serialize_int64( stream, world_time, -1000000000000ll, 1000000000000ll );
        serialize_bits( stream, frame_tick, 48 );
        serialize_fixed( stream, server_time, 24, 8, 0, 65535 );

        serialize_int( stream, entities_count, 1, 8 );
        for ( int i = 0; i < entities_count; i++ )
            serialize_object( stream, entities[i] );

        serialize_int( stream, stats_count, 0, 80 );
        for ( int i = 0; i < stats_count; i++ )
            serialize_object( stream, stats[i] );

        serialize_int( stream, event_type, 0, 3 );
        switch ( event_type )
        {
            case 1: serialize_object( stream, hit ); break;
            case 2: serialize_object( stream, chat ); break;
            case 3: serialize_object( stream, pickup ); break;
            default: break;                // None: the tag only
        }

        for ( int i = 0; i < 4; i++ )
            serialize_uint8( stream, loadout[i] );

        // string(15) and bytes(16) ride as their §4.3 decomposition — the
        // length prefix then the used bytes — in EVERY rt leg. serialize_string
        // exists and is wire-identical, but its C++ form pays strlen + UTF-8
        // validation while the Go and C# ports allocate a string per read; the
        // decomposition is what every GENERATED target emits, so gen-vs-rt and
        // language-vs-language both stay apples to apples (§2.7).
        serialize_int( stream, player_name_length, 0, 15 );
        serialize_bytes( stream, (uint8_t *) player_name, player_name_length );

        serialize_int( stream, payload_length, 0, 16 );
        serialize_bytes( stream, payload, payload_length );

        serialize_compressed_float( stream, aim_x, -1.0f, 1.0f, 0.01f );
        serialize_compressed_float( stream, aim_y, -1.0f, 1.0f, 0.01f );
        serialize_compressed_float( stream, aim_z, -1.0f, 1.0f, 0.01f );
        serialize_float( stream, recoil );
        serialize_double( stream, drift );
        serialize_uint128( stream, wide_key );
        serialize_int128( stream, flux,
                          -( ( serialize::int128_t( 1 ) << 100 ) ),
                          ( serialize::int128_t( 1 ) << 100 ) );
        serialize_fixed( stream, ping, 8, 8, 0, 250 );

        serialize_bits( stream, reserved_bits, 4 );
        if ( reserved_bits != 0 )
            return false;                  // reserved(4): a read rejects nonzero
        serialize_align( stream );
        serialize_bits( stream, crc_hint, 24 );
        serialize_bool( stream, has_extra );
        if ( has_extra )
            serialize_int( stream, extra, 0, 255 );
        else
            serialize_int( stream, idle_ticks, 0, 15 );
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
    in.sequence = 52428; in.ack_sequence = 12345; in.ack_bits = 0xA5A5A5A5u;
    in.session_id = 0x123456789ABCDEF0ull; in.client_id = 0xDEADBEEFu;
    in.nonce = 0xFEDCBA9876543210ull; in.world_time = -987654321000ll;
    in.frame_tick = 0x123456789ABCull; in.server_time = 12345678;
    in.entities_count = 8;
    for ( int i = 0; i < 8; i++ )
    {
        RtMixedEntity & e = in.entities[i];
        e.entity_id = (uint32_t) ( 2049 + i * 17 );
        e.pos_x = -16383 + i * 4096; e.pos_y = 16383 - i * 4096; e.pos_z = -1 + i * 2048;
        e.yaw = (uint32_t) ( 511 - i * 64 ); e.pitch = (uint32_t) ( i * 73 );
        e.vel_x = -2048 + i * 512; e.vel_y = 2047 - i * 512; e.vel_z = -1024 + i * 256;
        e.health = 1000 - i * 100;
        e.weapon = 1 + i;
        e.damage = (uint64_t) ( 0x5A + i );
        e.moving = ( i % 2 ) == 0; e.firing = ( i % 3 ) == 0;
    }
    in.stats_count = 80;
    for ( int i = 0; i < 80; i++ )
    {
        in.stats[i].stat_id = (uint32_t) ( ( i * 3 ) % 256 );
        in.stats[i].delta = -512 + ( i * 13 ) % 1024;
    }
    in.event_type = 1;   // Hit
    in.hit.target_id = 4095; in.hit.damage = 4095; in.hit.hit_kind = 7; in.hit.crit = true;
    in.loadout[0] = 0x11; in.loadout[1] = 0x22; in.loadout[2] = 0x33; in.loadout[3] = 0x44;
    memcpy( in.player_name, "Rowan_01", 8 ); in.player_name_length = 8;
    static const uint8_t pinned_payload[8] = { 0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04 };
    memcpy( in.payload, pinned_payload, 8 ); in.payload_length = 8;
    in.aim_x = 0.5f; in.aim_y = -0.25f; in.aim_z = 0.75f;
    in.recoil = 1.5f; in.drift = -3.25;
    in.wide_key = ( serialize::uint128_t( 0x0123456789ABCDEFull ) << 64 ) | serialize::uint128_t( 0xFEDCBA9876543210ull );
    in.flux = ( serialize::int128_t( 1 ) << 99 ) + serialize::int128_t( 7 );
    in.ping = 12345; in.crc_hint = 0xABCDEFu;
    in.has_extra = true; in.extra = 200;
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
    f.sequence = uint32_t( rng >> 8 ) & 65535;
    f.ack_sequence = int32_t( uint32_t( rng >> 24 ) & 65535 );
    f.ack_bits = uint32_t( rng >> 16 );
    f.session_id = rng;
    f.client_id = uint32_t( rng >> 32 );
    f.nonce = rng ^ 0xA5A5A5A5A5A5A5A5ull;
    f.world_time = int64_t( ( rng >> 12 ) & 0xFFFFFFFFFull ) - 34359738368ll;
    f.frame_tick = rng & 0xFFFFFFFFFFFFull;
    f.server_time = int32_t( ( rng >> 20 ) & 0x7FFFFF );
    for ( int i = 0; i < 8; i++ )
    {
        RtMixedEntity & e = f.entities[i];
        e.entity_id = uint32_t( ( rng >> i ) & 4095 );
        e.pos_x = int32_t( ( rng >> ( i + 4 ) ) & 16383 ) - 8192;
        e.pos_y = int32_t( ( rng >> ( i + 12 ) ) & 16383 ) - 8192;
        e.health = int32_t( ( rng >> ( i + 20 ) ) & 511 );
        e.weapon = int32_t( ( rng >> ( i + 40 ) ) & 15 );
        e.damage = ( rng >> ( i + 28 ) ) & 255;
        e.moving = ( ( rng >> i ) & 1 ) != 0;
    }
    for ( int i = 0; i < 80; i++ )
        f.stats[i].delta = int32_t( ( rng >> ( i & 31 ) ) & 1023 ) - 512;
    f.hit.target_id = uint32_t( ( rng >> 6 ) & 4095 );
    f.hit.damage = int32_t( ( rng >> 18 ) & 4095 );
    f.hit.hit_kind = int32_t( ( rng >> 30 ) & 7 );
    f.hit.crit = ( rng & 4 ) != 0;
    f.loadout[0] = uint8_t( rng >> 56 );
    f.player_name[7] = char( 65 + ( ( rng >> 50 ) & 15 ) );
    f.payload[0] = uint8_t( rng >> 48 );
    f.aim_x = float( uint32_t( rng >> 2 ) & 255 ) * ( 1.0f / 256.0f ) - 0.5f;
    f.aim_y = float( uint32_t( rng >> 10 ) & 255 ) * ( 1.0f / 256.0f ) - 0.5f;
    f.aim_z = float( uint32_t( rng >> 18 ) & 255 ) * ( 1.0f / 256.0f ) - 0.5f;
    f.recoil = float( uint32_t( rng ) & 0xFFFF );
    f.drift = double( int64_t( ( rng >> 8 ) & 0xFFFFFF ) ) * 0.5;
    f.wide_key = ( serialize::uint128_t( rng >> 1 ) << 64 ) | serialize::uint128_t( rng );
    f.flux = serialize::int128_t( int64_t( rng >> 16 ) );
    f.ping = uint16_t( ( rng >> 40 ) & 0x7FFF );
    f.crc_hint = uint32_t( ( rng >> 24 ) & 0xFFFFFF );
    f.extra = int32_t( ( rng >> 52 ) & 255 );
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
        else if ( strcmp( argv[i], "--quick" ) == 0 )
            g_quick = true;
        else
        {
            fprintf( stderr, "usage: %s [--csv] [--round K] [--quick] [--wire-dir <dir>]\n", argv[0] );
            return 1;
        }
    }
    if ( g_quick && g_num_runs == MaxNumRuns )
        g_num_runs = 3;

#if defined(NDEBUG)
    fprintf( stderr, "schema bench (cpp, Release)\n" );
#else
    fprintf( stderr, "schema bench (cpp, Debug — only release numbers are meaningful)\n" );
#endif

    if ( g_csv )
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n" );


    if ( g_quick )
    {
        // --quick: bench_mixed only — golden gate unconditional (both
        // bench_message and bench_rt gate before timing). The gen row is the
        // schema subject (the blended table's row); the rt row rides beside
        // it as the hand-written-usage subject.
        fprintf( stderr, "--quick: iteration instrument, not certification\n" );
        bench_message( "bench_mixed", "bench_mixed", 4000000L, pin_gen_mixed(), bench::WriteBenchMixed, bench::ReadBenchMixed, vary_gen_mixed );
        bench_rt( "bench_mixed", 4000000L, pin_rt_mixed(), rt_bench_mixed_write_loop, rt_bench_mixed_read_loop, vary_rt_mixed );
        flush_csv();
        if ( failed )
        {
            fprintf( stderr, "BENCH FAILED (corpus_id %s)\n", corpus_id().c_str() );
            return 1;
        }
        fprintf( stderr, "OK (corpus_id %s)\n", corpus_id().c_str() );
        ( void ) g_sink;
        return 0;
    }

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
    bench_message( "probe_header", "probe_header", 256000000L, pin_probe_header(), WriteProbeHeader, ReadProbeHeader, vary_probe_header );
    bench_message( "probebits", "probebits", 128000000L, pin_probebits(), WriteProbeBits, ReadProbeBits, vary_probebits );
    bench_message( "probearray", "probearray", 20000000L, pin_probearray(), WriteProbeArray, ReadProbeArray, vary_probearray );
    bench_message( "testdata", "testdata", 8000000L, pin_testdata(), WriteTestData, ReadTestData, vary_testdata );

    // real_packet (§1.7): the realistic snapshot — ~93 riding individually
    // serialized small fields, 204 wire bytes, 0% bulk share by bits. The pin
    // is the ALL-DEFAULTS instance (RealPacket{} — test/bench/main.cpp).
    // Sized by the same §2.1 logic as the counts above: 8M iters puts the
    // 200 ms floor at 40 M msg/s, margin over any plausible fastest language
    // leg — the cpp legs smoke-checked at ~14 M msg/s both paths on the M2
    // (~0.55 s per measured run), and no other language's leg is plausibly
    // near 3x the fully-inlined header-only C++.
    bench_message( "real_packet", "real_packet", 8000000L, realworld::RealPacket{}, realworld::WriteRealPacket, realworld::ReadRealPacket, vary_real_packet );

    // family gen over the Bench corpus (issue #177): the generated twins of
    // the rt rows below — same shapes, same goldens, same pins, same vary
    // mappings, same iteration counts (fixed and identical across all five
    // runners, §2.1); only the subject differs, and the family column says so.
    bench_message( "bench_packet", "bench_packet", 32000000L, pin_gen_packet(), bench::WriteBenchPacket, bench::ReadBenchPacket, vary_gen_packet );
    bench_message( "bench_ints", "bench_ints", 40000000L, pin_gen_ints(), bench::WriteBenchInts, bench::ReadBenchInts, vary_gen_ints );
    bench_message( "bench_bits", "bench_bits", 48000000L, pin_gen_bits(), bench::WriteBenchBits, bench::ReadBenchBits, vary_gen_bits );
    bench_message( "bench_mixed", "bench_mixed", 4000000L, pin_gen_mixed(), bench::WriteBenchMixed, bench::ReadBenchMixed, vary_gen_mixed );

    // family rt (§1.3/§1.5): the runtime API by hand, oracle-gated against
    // the goldens the generated code pinned. Iteration counts are fixed and
    // identical across all five runners, sized so the FASTEST language's
    // fastest path exceeds §2.1's 200 ms floor on the M2 (measured
    // 2026-08-14; the C++ reads at ~110-180 M msg/s are the binding paths).
    bench_rt( "bench_packet", 32000000L, pin_rt_packet(), rt_bench_packet_write_loop, rt_bench_packet_read_loop, vary_rt_packet );
    bench_rt( "bench_ints", 40000000L, pin_rt_ints(), rt_bench_ints_write_loop, rt_bench_ints_read_loop, vary_rt_ints );
    bench_rt( "bench_bits", 48000000L, pin_rt_bits(), rt_bench_bits_write_loop, rt_bench_bits_read_loop, vary_rt_bits );
    bench_rt( "bench_mixed", 4000000L, pin_rt_mixed(), rt_bench_mixed_write_loop, rt_bench_mixed_read_loop, vary_rt_mixed );

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
