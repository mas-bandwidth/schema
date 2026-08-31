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
//
// bench_mixed — THE canonical benchmark (§1.3a) — is DATA-DRIVEN instead
// (issue #191): its 64 instances come from the committed variant data
// (bench/corpus/variants, `make bench-variants`), its rows are write and
// round-trip, and its driver names no field of the shape. See
// bench_datadriven below for what that buys and what it costs.
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
#include "BenchWire.h"          // generated/bench/cpp — the Bench corpus GENERATED (issue #177)

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
static const char * g_variant_dir = "bench/corpus/variants";

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
// family is per ROW now (gen | bits — §5.1); linkage/checks/opt/inline
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
// the DATA-DRIVEN benchmark driver (issue #191)
// ------------------------------------------------------------------------------------------
//
// THE PROPERTY: nothing below names a field of the shape it measures. Shape
// knowledge lives in the committed variant DATA (bench/corpus/variants,
// emitted by bench/tools/variantgen) and in the generated codec, and nowhere
// else — so this driver cannot drift from another language's driver in what
// it measures, which is the whole reason the design exists. If a change here
// ever needs a field name, the design has failed and that is the finding.
//
// It replaces bench_message for bench_mixed only. bench_message still drives
// every shape whose harness code is not yet data-driven.

static bool read_file( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( !f )
        return false;
    uint8_t chunk[65536];
    size_t n;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 )
        out.insert( out.end(), chunk, chunk + n );
    fclose( f );
    return true;
}

// Loads <variant-dir>/<name>.variants.bin into the NumVariants §2.7-staggered
// slots and returns the record size, or -1. The records are fixed-width by
// construction (§2.7 pins every structure field), so the file needs no index:
// the record size IS file size / NumVariants, and a file that does not divide
// evenly is a refusal.
static int64_t load_variants( const char * name )
{
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s.variants.bin", g_variant_dir, name );
    std::vector<uint8_t> packed;
    if ( !read_file( path, packed ) )
    {
        fprintf( stderr, "missing variant data %s — run `make bench-variants`, and run the bench from the schema repo root (or pass --variant-dir)\n", path );
        return -1;
    }
    if ( packed.size() == 0 || packed.size() % NumVariants != 0 )
    {
        fprintf( stderr, "variant data %s is %zu bytes, not a multiple of %d records — refusing to bench data whose stride is not the record size\n",
                 path, packed.size(), NumVariants );
        return -1;
    }
    const size_t record = packed.size() / NumVariants;
    if ( record > (size_t) BufferSize )
    {
        fprintf( stderr, "variant data %s has %zu-byte records, over the %d-byte buffer\n", path, record, BufferSize );
        return -1;
    }
    for ( int k = 0; k < NumVariants; k++ )
        memcpy( g_variants[k], packed.data() + k * record, record );

    // The variant data is corpus (§1.6): it defines the work inside the timed
    // loops, so it rides in corpus_id exactly as the wire goldens do. A run
    // against drifted variant data reports a different id and the tools refuse
    // the ratio, instead of publishing a number for different work.
    const char * base = strrchr( path, '/' );
    g_goldens_loaded[std::string( base ? base + 1 : path )] = packed;
    return (int64_t) record;
}

// T — the generated message type — is explicit at the call site: it appears
// only inside the body, so there is nothing to deduce it from. A TYPE name is
// not a field name; the driver still knows nothing about the shape's contents.
template <typename T, typename WriteFn, typename ReadFn>
static void bench_datadriven( const char * name, const char * golden, long base_iters,
                              WriteFn write_fn, ReadFn read_fn )
{
    const long iters = base_iters / IterScale;

    const int64_t bytes_per_op = load_variants( name );
    if ( bytes_per_op < 0 )
    {
        failed = true;
        return;
    }

    // gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
    // file is bound to the wire golden by one byte-compare.
    if ( !check_golden( golden, g_variants[0], bytes_per_op ) )
    {
        failed = true;
        return;
    }

    // gate 2: every variant decodes, re-encodes, and comes back byte-identical
    // at the same length. This is stronger than the pinned-instance-only gate
    // bench_message applies — §1.5's named residual (the 64 varied buffers
    // length-checked but never value-checked) closes here, for every variant.
    static T instances[NumVariants];
    for ( int k = 0; k < NumVariants; k++ )
    {
        serialize::ReadStream rs( g_variants[k], (int) bytes_per_op );
        if ( !read_fn( rs, instances[k] ) )
        {
            fail( name, "decode of a variant failed" );
            return;
        }
        serialize::WriteStream ws( g_twin, BufferSize );
        if ( !write_fn( ws, instances[k] ) )
        {
            fail( name, "re-encode of a decoded variant failed" );
            return;
        }
        ws.Flush();
        if ( ws.GetBytesProcessed() != bytes_per_op ||
             memcmp( g_twin, g_variants[k], (size_t) bytes_per_op ) != 0 )
        {
            fail( name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus" );
            return;
        }
    }

    double write_rates[MaxNumRuns];
    double roundtrip_rates[MaxNumRuns];

    // WRITE: encode the 64 pre-decoded instances round-robin. Rotating the
    // instances is what §2.7's per-iteration LCG mutation bought — the
    // encoder never sees the same input twice in a row and cannot precompute
    // scratch words — with none of the per-language mutation code, and with
    // bytes/op constant by construction rather than by assertion. The sink is
    // the byte fold: every iteration's result is a value the loop cannot drop.
    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i++ )
        {
            serialize::WriteStream stream( g_buffer, BufferSize );
            if ( !write_fn( stream, instances[i & ( NumVariants - 1 )] ) )
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

    // ROUND-TRIP: decode a variant buffer, then re-encode what came out. The
    // decode needs no sink discipline of its own — its output IS the encode's
    // input, so every decoded field is observed by construction, in every
    // language, with no per-language fold to audit (§2.7's read-side sink
    // problem dissolved rather than equalized). The decode target is hoisted
    // and reused, as everywhere else.
    T out;
    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i++ )
        {
            serialize::ReadStream rs( g_variants[i & ( NumVariants - 1 )], (int) bytes_per_op );
            if ( !read_fn( rs, out ) )
            {
                fail( name, "read failed in loop" );
                return;
            }
            serialize::WriteStream ws( g_buffer, BufferSize );
            if ( !write_fn( ws, out ) )
            {
                fail( name, "re-write failed in loop" );
                return;
            }
            ws.Flush();
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) ws.GetBytesProcessed();
        }
        double time = time_now() - start;
        if ( run >= 0 )
            roundtrip_rates[run] = double( iters ) / time;
    }

    const RunStats w = run_stats( write_rates, g_num_runs );
    const RunStats rt = run_stats( roundtrip_rates, g_num_runs );
    report( name, "write", iters, bytes_per_op, w );
    report( name, "round_trip", iters, bytes_per_op, rt );

    // READ is DERIVED, never measured: round-trip time minus write time. It
    // prints for continuity with the read rows the rest of the corpus still
    // reports and is NOT a CSV row — a derived number in the CSV would be
    // divided as if it had been measured.
    const double read_time = 1.0 / rt.median_rate - 1.0 / w.median_rate;
    if ( read_time > 0 )
    {
        fprintf( stderr, "%-18s %-5s %10.2f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n",
                 name, "read", 1e-6 / read_time );
    }
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
// measured through the GENERATED code (generated/bench/cpp/BenchWire.h) —
// same golden files, same pinned values, same LCG field mappings, same
// bench_message discipline as every gen row above. Generated best case per
// the profiling doctrine (#170): the plain optimized release build of the
// emitted code, no PGO (#175 runs later).
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

#if defined(_MSC_VER)
#define BENCH_NOINLINE __declspec( noinline )
#else
#define BENCH_NOINLINE __attribute__(( noinline ))
#endif

namespace {

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
        else if ( strcmp( argv[i], "--variant-dir" ) == 0 && i + 1 < argc )
            g_variant_dir = argv[++i];
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
            fprintf( stderr, "usage: %s [--csv] [--round K] [--quick] [--wire-dir <dir>] [--variant-dir <dir>]\n", argv[0] );
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
        // --quick: bench_mixed only — golden gate unconditional
        // (bench_message gates before timing).
        fprintf( stderr, "--quick: iteration instrument, not certification\n" );
        bench_datadriven<bench::BenchMixed>( "bench_mixed", "bench_mixed", 4000000L, bench::WriteBenchMixed, bench::ReadBenchMixed );
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

    // family gen over the Bench corpus (issue #177): the four Bench.schema
    // shapes through the generated code — same goldens, same pins, same vary
    // mappings, same iteration counts (fixed and identical across all five
    // runners, §2.1).
    bench_message( "bench_packet", "bench_packet", 32000000L, pin_gen_packet(), bench::WriteBenchPacket, bench::ReadBenchPacket, vary_gen_packet );
    bench_message( "bench_ints", "bench_ints", 40000000L, pin_gen_ints(), bench::WriteBenchInts, bench::ReadBenchInts, vary_gen_ints );
    bench_message( "bench_bits", "bench_bits", 48000000L, pin_gen_bits(), bench::WriteBenchBits, bench::ReadBenchBits, vary_gen_bits );
    bench_datadriven<bench::BenchMixed>( "bench_mixed", "bench_mixed", 4000000L, bench::WriteBenchMixed, bench::ReadBenchMixed );

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
