// schema bench — the C++ runner, the reference implementation.
//
// Measures the schema-GENERATED C++ code (generated/bench/cpp) against the
// classic serialize runtime over THE one benchmark shape, BenchMixed
// (BENCH-STANDARD.md §1.3a): write path and round-trip path, ops/sec and
// MB/sec. Plus family bits (§1.4), the raw bitpacker, which is not a shape.
//
// Methodology (follows the serialize repo's bench.cpp conventions — see the
// const-params experiment for the reasoning behind the escape barriers):
//   - the measured shape is DATA-DRIVEN (issue #191): its 64 instances come
//     from the committed variant data (bench/corpus/variants, `make
//     bench-variants`), its rows are write and round-trip, and its driver
//     names no field of the shape. See bench_datadriven below for what that
//     buys and what it costs.
//   - before benching, variant 0 is byte-compared against its wire golden
//     (testdata/wire/<name>.bin) and every variant is round-tripped
//     read→re-write→memcmp; a mismatch refuses to bench
//   - fixed iteration counts, one warmup run per path, then NumRuns measured
//     runs; the report is the MEDIAN rate with min/max and spread
//   - MB/s means MiB/s (1024*1024), following serialize bench.cpp
//
// Output: a human table on stderr, and with --csv, CSV rows on stdout in the
// cross-language format (see bench/README.md). Other language runners must
// implement the same benchmarks over the same variant corpus — this file is
// the reference implementation.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <chrono>
#include <algorithm>
#include <map>
#include <string>
#include <vector>

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
// the DATA-DRIVEN benchmark driver (issue #191)
// ------------------------------------------------------------------------------------------
//
// THE PROPERTY: nothing below names a field of the shape it measures. Shape
// knowledge lives in the committed variant DATA (bench/corpus/variants,
// emitted by bench/tools/variantgen) and in the generated codec, and nowhere
// else — so this driver cannot drift from another language's driver in what
// it measures, which is the whole reason the design exists. If a change here
// ever needs a field name, the design has failed and that is the finding.

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
    // at the same length. This is stronger than a pinned-instance-only gate —
    // §1.5's named residual (the 64 varied buffers length-checked but never
    // value-checked) closes here, for every variant.
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
        fprintf( stderr, "--quick: iteration instrument, not certification\n" );

    // family gen over the Bench corpus: BenchMixed through the generated code,
    // fed by the committed variant corpus — same goldens, same iteration count
    // in every runner (§2.1). No hand-written pin, vary or sink code
    // participates in this leg.
    bench_datadriven<bench::BenchMixed>( "bench_mixed", "bench_mixed", 4000000L, bench::WriteBenchMixed, bench::ReadBenchMixed );

    // family bits (§1.4): the one bitpacker workload in the estate. 24576
    // passes, not the historical 4096 — at 4096 the C++ read leg finishes in
    // ~170 ms, under §2.1's 200 ms floor (measured; §1.4 records this).
    if ( !g_quick )
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
