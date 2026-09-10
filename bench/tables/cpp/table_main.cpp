// the tables bench — the C++ runner, the reference implementation.
//
// Measures ONE thing: a representative fixed table written and read on the
// TOLERANT WIRE (docs/SPEC-TABLES.md §3), through the generated table codec.
// That is the number a reader who knows protobuf or flatbuffers already has a
// comparison for, and it is the per-language release gate for the tables
// layer (bench/tables/README.md).
//
// It is a sibling of bench/cpp/bench_main.cpp and follows the same contract
// (BENCH-STANDARD.md): the committed variant corpus drives it, the golden
// gate runs before the clock, the loops are escape-barriered against dead
// code elimination, and the report is 1 warmup + 7 measured runs with the
// median beside min/max/spread. What differs is the corpus, the codec and the
// family column — and the CSV therefore never mixes with the type board's.
//
// THIS FILE IS SHAPE-BLIND. It names the generated TYPE at one call site and
// nothing else: no field, no pinned value, no wire size. Shape knowledge
// lives in bench/corpus/BenchTable.schema, in the code generated from it, and
// in the committed data test/bench/table_main.cpp produced. `make shape-gate`
// holds that mechanically.
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <chrono>
#include <algorithm>
#include <map>
#include <string>
#include <vector>

#if defined(BENCH_MATCHED)
#include "BenchTable.h"
// THE FIXED FORM'S HALF (docs/SPEC-TABLES.md §3.4) rides in the MATCHED build
// only: the paired generation is the one that carries FixedTable.schema, and
// the standalone table corpus has no fixed root to measure.
#include "FixedTableTable.h"
namespace benchtable = bench;
#define TableMixed BenchMixed
#define TableMixedReset BenchMixedReset
#define TableMixedSave BenchMixedSave
#define TableMixedLoad BenchMixedLoad
#else
#include "BenchTableTable.h"
#endif

static volatile uint64_t g_sink = 0;    // defeats dead code elimination of computed values

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

const int MaxNumRuns = 7;           // median of 7 (N >= 5), after 1 warmup run
static long g_iterations = 0; /* explicit diagnostic count; defaults stay standard */
static int g_num_runs = MaxNumRuns; // --round K drops this to 1 (§2.4)
const int NumVariants = 64;         // read-path variant buffers

#if defined(NDEBUG)
const long IterScale = 1;
#else
const long IterScale = 8;
#endif

static bool g_csv = false;
static bool g_gate = false;
static bool g_indexed = false;
static int64_t g_lengths[NumVariants];
static const char * g_wire_dir = "testdata/wire";
static const char * g_variant_dir = "bench/corpus/variants";

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// family `table` (§1.9): the tolerant table wire, which is a DIFFERENT wire
// over a different corpus, so a tools refusal to divide it against a `gen`
// row is correct and automatic. linkage hdr — the generated table codec is
// header-only inline in this translation unit and names no runtime at all
// (docs/SPEC-TABLES.md, the C# standalone gate says the same of its twin).
// checks contract — caller-error asserts are dormant under NDEBUG while the
// reader's wire-contract validation is unconditional, which is §3.4's word
// for exactly this. inline unknown until the §4 verdict pass has a branch.
#ifndef BENCH_OPT
#define BENCH_OPT "O3"
#endif
static const char * g_csv_suffix = "hdr,contract," BENCH_OPT ",unknown";
static std::vector<std::pair<std::string, std::string>> g_csv_rows;
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
        // §1.5: a failing run emits NO rows.
        fprintf( stderr, "refusing to emit CSV rows from a failing run\n" );
        return;
    }
    const std::string id = corpus_id();
    for ( const auto & row : g_csv_rows )
        printf( "%s,%s,%s,%s\n", row.first.c_str(), id.c_str(), row.second.c_str(), g_csv_suffix );
}

// The tolerant wire spends bytes on ids, kinds and lengths, so a table record
// is several times its equivalent type's. The buffer is sized from the
// corpus at run time and this is only the ceiling the runner refuses past.
const int BufferSize = 65536;
// §2.7's variant stride, for the same reason and by the same arithmetic as
// the type runner's: a power-of-two stride maps every head line into a
// handful of L1 set groups and a memory-bound read then feels every
// background conflict miss.
const int VariantStride = BufferSize + 64;
alignas( 8 ) static uint8_t g_buffer[BufferSize];
alignas( 8 ) static uint8_t g_twin[BufferSize];
static std::vector<uint8_t> g_variants;     // NumVariants slots of VariantStride

static uint8_t * variant( int k )
{
    return g_variants.data() + (size_t) k * (size_t) VariantStride;
}

static void fail( const char * name, const char * what )
{
    fprintf( stderr, "FAILED: %s: %s\n", name, what );
    failed = true;
}

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

static bool check_golden( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s.bin", g_wire_dir, name );
    std::vector<uint8_t> expected;
    if ( !read_file( path, expected ) )
    {
        fprintf( stderr, "missing wire golden %s — run from the schema repo root (or pass --wire-dir)\n", path );
        return false;
    }
    g_goldens_loaded[std::string( name ) + ".bin"] = expected;
    if ( (int64_t) expected.size() != bytes || memcmp( expected.data(), data, (size_t) bytes ) != 0 )
    {
        fprintf( stderr, "WIRE GOLDEN MISMATCH: %s (%zu golden vs %lld actual bytes) — refusing to bench code that does not match the corpus\n",
                 name, expected.size(), (long long) bytes );
        return false;
    }
    return true;
}

struct RunStats
{
    double median_rate;
    double min_rate;
    double max_rate;
    double spread_pct;
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

static void report( const char * bench, const char * path, long iters, double bytes_per_op, const RunStats & s )
{
    const double mbps = s.median_rate * double( bytes_per_op ) / ( 1024.0 * 1024.0 );
    fprintf( stderr, "%-18s %-11s %10.3f M msg/s %10.1f MB/s   (min %.3f, max %.3f, spread %.1f%%)\n",
             bench, path, s.median_rate / 1e6, mbps, s.min_rate / 1e6, s.max_rate / 1e6, s.spread_pct );
    if ( g_csv )
    {
        char row[256];
        snprintf( row, sizeof( row ), "cpp,%s,%s,%ld,%.17g,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
                  bench, path, iters, bytes_per_op, g_num_runs,
                  s.median_rate, s.min_rate, s.max_rate, mbps, s.spread_pct );
        g_csv_rows.push_back( { row, "table" } );
    }
}

// Load 64 concatenated records. --indexed requires a 64-entry uint32
// length index; otherwise retain the historical fixed-stride format.
// Return the exact mean encoded bytes; padding is never counted.
static double load_variants( const char * name )
{
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s.variants.bin", g_variant_dir, name );
    std::vector<uint8_t> packed;
    if ( !read_file( path, packed ) ) return -1;
    const size_t size = packed.size();
    g_goldens_loaded[std::string( name ) + ".variants.bin"] = packed;
    if ( g_indexed )
    {
        snprintf( path, sizeof( path ), "%s/%s.lengths", g_variant_dir, name );
        std::vector<uint8_t> lengths;
        if ( !read_file( path, lengths ) || lengths.size() != NumVariants * 4 ) return -1;
        g_goldens_loaded[std::string( name ) + ".lengths"] = lengths;
        for ( int k = 0; k < NumVariants; ++k )
        {
            uint32_t n = 0;
            for ( unsigned j = 0; j < 4; ++j ) n |= uint32_t( lengths[k * 4 + j] ) << (j * 8);
            g_lengths[k] = n;
        }
    }
    else
    {
        if ( size == 0 || size % NumVariants ) return -1;
        for ( int k = 0; k < NumVariants; ++k ) g_lengths[k] = size / NumVariants;
    }
    g_variants.assign( (size_t) NumVariants * VariantStride, 0 );
    size_t offset = 0;
    for ( int k = 0; k < NumVariants; ++k )
    {
        const int64_t n = g_lengths[k];
        if ( n <= 0 || n > BufferSize || (size_t) n > size - offset ) return -1;
        memcpy( variant(k), packed.data() + offset, (size_t) n );
        offset += (size_t) n;
    }
    if ( offset != size ) return -1;
    return double( size ) / NumVariants;
}

// ------------------------------------------------------------------------------------------
// the data-driven table driver
// ------------------------------------------------------------------------------------------
//
// Public Load restores declared defaults before overlaying the fields on the
// wire (§3). That work stays inside the clock; the runner adds no separate reset.
template <typename T, typename ResetFn, typename SaveFn, typename LoadFn>
static void bench_table( const char * name, const char * golden, long base_iters,
                         ResetFn reset_fn, SaveFn save_fn, LoadFn load_fn )
{
    const long iters = g_iterations ? g_iterations : base_iters / IterScale;

    const double bytes_per_op = load_variants( name );
    if ( bytes_per_op < 0 )
    {
        failed = true;
        return;
    }

    // gate 1 (§1.5): variant 0 IS the pinned instance.
    if ( !check_golden( golden, variant( 0 ), g_lengths[0] ) )
    {
        failed = true;
        return;
    }

    // gate 2: every variant loads, re-saves, and comes back byte-identical at
    // the same length — before any clock starts.
    static T instances[NumVariants];
    for ( int k = 0; k < NumVariants; k++ )
    {
        reset_fn( instances[k] );
        if ( !load_fn( instances[k], variant( k ), g_lengths[k] ) )
        {
            fail( name, "load of a variant failed" );
            return;
        }
        const int64_t wrote = save_fn( instances[k], g_twin, BufferSize );
        if ( wrote != g_lengths[k] || memcmp( g_twin, variant( k ), (size_t) g_lengths[k] ) != 0 )
        {
            fail( name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus" );
            return;
        }
    }

    // gate 3: reused storage must also round-trip, including the last-to-first
    // transition. Public Load owns default restoration; do not reset between loads.
    static T out;
    for ( int k = 0; k < 2 * NumVariants; k++ )
    {
        const int index = k & ( NumVariants - 1 );
        if ( !load_fn( out, variant( index ), g_lengths[index] ) )
        {
            fail( name, "load into reused target failed" );
            return;
        }
        const int64_t wrote = save_fn( out, g_twin, BufferSize );
        if ( wrote != g_lengths[index] || memcmp( g_twin, variant( index ), (size_t) g_lengths[index] ) != 0 )
        {
            fail( name, "reused target round-trip bytes differ" );
            return;
        }
    }

    if ( g_gate ) return;

    double write_rates[MaxNumRuns];
    double roundtrip_rates[MaxNumRuns];

    // WRITE: save the 64 pre-loaded instances round-robin. Rotating the
    // instances is the §2.7 variation: the encoder never sees the same input
    // twice in a row, and bytes/op is constant by construction rather than by
    // assertion. The sink is the byte fold.
    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i++ )
        {
            const int64_t wrote = save_fn( instances[i & ( NumVariants - 1 )], g_buffer, BufferSize );
            if ( wrote != g_lengths[i & ( NumVariants - 1 )] )
            {
                fail( name, "save failed in loop" );
                return;
            }
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) wrote;
        }
        double time = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = double( iters ) / time;
    }

    // ROUND-TRIP: load a variant buffer, then re-save what came out.
    // The load needs no sink discipline of its own — its output IS the save's
    // input, so every loaded field is observed by construction (§2.7's
    // read-side sink problem dissolved rather than equalized).
    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i++ )
        {
            if ( !load_fn( out, variant( i & ( NumVariants - 1 ) ), g_lengths[i & ( NumVariants - 1 )] ) )
            {
                fail( name, "load failed in loop" );
                return;
            }
            const int64_t wrote = save_fn( out, g_buffer, BufferSize );
            if ( wrote != g_lengths[i & ( NumVariants - 1 )] )
            {
                fail( name, "re-save failed in loop" );
                return;
            }
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) wrote;
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
    // prints for continuity and is NOT a CSV row — a derived number in the
    // CSV would be divided as if it had been measured (§2.9).
    const double read_time = 1.0 / rt.median_rate - 1.0 / w.median_rate;
    if ( read_time > 0 )
    {
        fprintf( stderr, "%-18s %-11s %10.3f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n",
                 name, "read", 1e-6 / read_time );
    }
}

#if defined(BENCH_MATCHED)
// ------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ------------------------------------------------------------------------
//
// The same 64 logical records, on the same table wire, in the form that spends
// no byte on ids, kinds, lengths or terminators. A FILE is the unit here and
// not a record: the LAYOUT rides once, the records follow it to the
// end, and every one of them is the same size — so the write is one call for
// all 64 and the read is one call back, and an OP is one RECORD of that call.
//
// The gates before the clock are the same three in a different spelling, plus
// the one this form adds: THE LAYOUT THIS BUILD EMITS IS THE CORPUS'S LAYOUT,
// byte for byte. That is the whole claim a port makes on this form — the record
// layout, the ids, the kinds and the hash are all in those bytes — so it is
// checked first and by itself.
static const int FixedCount = 64;
static const int32_t FixedPlanCapacity = 4096;
static benchtable::FixedTable g_fixed_values[FixedCount];
static benchtable::FixedTable g_fixed_out[FixedCount];
// the plan storage the caller owns; the identity path never touches it, and a
// stranger's layout compiles into it (§3.4: this codec never allocates)
static benchtable::TableFixedEntry g_fixed_plan[FixedPlanCapacity];

static int64_t fixed_load_all( benchtable::FixedTable * values, const uint8_t * bytes, int64_t size )
{
    benchtable::TableReport report;
    return benchtable::FixedTableFixedLoad( values, FixedCount, bytes, size, g_fixed_plan, FixedPlanCapacity, &report );
}

static void bench_fixed( const char * name, long base_iters )
{
    const long iters = g_iterations ? g_iterations : base_iters / IterScale;
    char path[512];
    std::vector<uint8_t> file, layout;
    snprintf( path, sizeof( path ), "%s/bench_fixed.bin", g_variant_dir );
    if ( !read_file( path, file ) || file.empty() )
    {
        fprintf( stderr, "missing fixed corpus %s — run from the schema repo root (or pass --variant-dir)\n", path );
        failed = true;
        return;
    }
    snprintf( path, sizeof( path ), "%s/bench_fixed.layout", g_variant_dir );
    if ( !read_file( path, layout ) )
    {
        fprintf( stderr, "missing fixed corpus %s — run from the schema repo root (or pass --variant-dir)\n", path );
        failed = true;
        return;
    }
    g_goldens_loaded["bench_fixed.bin"] = file;
    g_goldens_loaded["bench_fixed.layout"] = layout;
    const double bytes_per_op = double( file.size() ) / FixedCount;
    std::vector<uint8_t> twin( file.size() );

    // gate 1: THE LAYOUT IS THE CORPUS'S LAYOUT. Every other fact about the form
    // — the positions, the ids, the kinds, the record's size, the hash every
    // record carries — is settled by these bytes, so this one comparison is
    // what says this leg speaks the form and not a near miss.
    if ( (int64_t) layout.size() != benchtable::FixedTableFixedLayoutBytes ||
         memcmp( layout.data(), benchtable::FixedTableFixedLayout, layout.size() ) != 0 )
    {
        fail( name, "this build's LAYOUT is not the corpus's, byte for byte" );
        return;
    }

    // gate 2: the whole file loads clean.
    if ( fixed_load_all( g_fixed_values, file.data(), (int64_t) file.size() ) != FixedCount )
    {
        fail( name, "the fixed corpus did not load" );
        return;
    }

    // gate 3: writing them back reproduces the file, byte for byte.
    if ( benchtable::FixedTableFixedSave( g_fixed_values, FixedCount, twin.data(), (int64_t) twin.size() ) != (int64_t) file.size() ||
         memcmp( twin.data(), file.data(), file.size() ) != 0 )
    {
        fail( name, "round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus" );
        return;
    }

    // gate 4: reused storage round-trips too. The load owns the prefill, so
    // nothing is reset between the two passes.
    for ( int k = 0; k < 2; k++ )
    {
        if ( fixed_load_all( g_fixed_out, file.data(), (int64_t) file.size() ) != FixedCount ||
             benchtable::FixedTableFixedSave( g_fixed_out, FixedCount, twin.data(), (int64_t) twin.size() ) != (int64_t) file.size() ||
             memcmp( twin.data(), file.data(), file.size() ) != 0 )
        {
            fail( name, "reused target round-trip bytes differ" );
            return;
        }
    }

    if ( g_gate ) return;

    double write_rates[MaxNumRuns];
    double roundtrip_rates[MaxNumRuns];

    // WRITE: one call lays down all 64 records; an op is one record.
    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i += FixedCount )
        {
            const int64_t wrote = benchtable::FixedTableFixedSave( g_fixed_values, FixedCount, twin.data(), (int64_t) twin.size() );
            if ( wrote != (int64_t) file.size() ) { fail( name, "save failed in loop" ); return; }
            bench_escape( twin.data() );
            g_sink = g_sink + (uint64_t) wrote;
        }
        double time = time_now() - start;
        if ( run >= 0 ) write_rates[run] = double( iters ) / time;
    }

    // ROUND-TRIP: read the file back, then write what came out.
    for ( int run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < iters; i += FixedCount )
        {
            if ( fixed_load_all( g_fixed_out, file.data(), (int64_t) file.size() ) != FixedCount )
            {
                fail( name, "load failed in loop" );
                return;
            }
            const int64_t wrote = benchtable::FixedTableFixedSave( g_fixed_out, FixedCount, twin.data(), (int64_t) twin.size() );
            if ( wrote != (int64_t) file.size() ) { fail( name, "re-save failed in loop" ); return; }
            bench_escape( twin.data() );
            g_sink = g_sink + (uint64_t) wrote;
        }
        double time = time_now() - start;
        if ( run >= 0 ) roundtrip_rates[run] = double( iters ) / time;
    }

    const RunStats w = run_stats( write_rates, g_num_runs );
    const RunStats rt = run_stats( roundtrip_rates, g_num_runs );
    report( name, "write", iters, bytes_per_op, w );
    report( name, "round_trip", iters, bytes_per_op, rt );

    const double read_time = 1.0 / rt.median_rate - 1.0 / w.median_rate;
    if ( read_time > 0 )
    {
        fprintf( stderr, "%-18s %-11s %10.3f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n",
                 name, "read", 1e-6 / read_time );
    }
}
#endif // BENCH_MATCHED

int main( int argc, char ** argv )
{
    for ( int i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--indexed" ) == 0 ) { g_indexed = 1; }
        else if ( strcmp( argv[i], "--gate" ) == 0 ) { g_gate = true; }
        else if ( strcmp( argv[i], "--csv" ) == 0 )
            g_csv = true;
        else if ( strcmp( argv[i], "--wire-dir" ) == 0 && i + 1 < argc )
            g_wire_dir = argv[++i];
        else if ( strcmp( argv[i], "--variant-dir" ) == 0 && i + 1 < argc )
            g_variant_dir = argv[++i];
        else if ( strcmp( argv[i], "--iterations" ) == 0 && i + 1 < argc )
        {
            char * end = NULL;
            long n = strtol( argv[++i], &end, 10 );
            if ( end == argv[i] || *end != '\0' || n <= 0 || n > 2147483584L || n % NumVariants != 0 )
            {
                fprintf( stderr, "--iterations requires a positive multiple of 64 up to 2147483584\n" );
                return 1;
            }
            g_iterations = n;
        }
        else if ( strcmp( argv[i], "--round" ) == 0 && i + 1 < argc )
        {
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
            fprintf( stderr, "usage: %s [--indexed] [--gate] [--csv] [--round K] [--iterations N] [--wire-dir <dir>] [--variant-dir <dir>]\n", argv[0] );
            return 1;
        }
    }

#if defined(NDEBUG)
    fprintf( stderr, "schema tables bench (cpp, Release)\n" );
#else
    fprintf( stderr, "schema tables bench (cpp, Debug — only release numbers are meaningful)\n" );
#endif

    if ( g_csv )
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n" );

    // The measured shape, named once — the generated type at the call site and
    // nothing else about it (bench/SHAPE-GATE.allow).
    //
    // IN THE MATCHED BUILD THE MEASURED WIRE IS THE FIXED FORM (§3.4), and the
    // tolerant form still runs every correctness gate it always ran, without a
    // clock: the corpus is one corpus, both forms carry the same 64 logical
    // records, and a run that stopped loading the tolerant half would report a
    // corpus id the paired driver could not match.
#if defined(BENCH_MATCHED)
    const bool measured = g_gate;
    g_gate = true;
#endif
    bench_table<benchtable::TableMixed>(
        "bench_table", "bench_table", 400000L,
        []( benchtable::TableMixed & v ) { benchtable::TableMixedReset( v ); },
        []( const benchtable::TableMixed & v, uint8_t * b, int64_t n ) { return benchtable::TableMixedSave( v, b, n ); },
        []( benchtable::TableMixed & v, const uint8_t * b, int64_t n ) {
            benchtable::TableReport report;
            return benchtable::TableMixedLoad( v, b, n, &report ) && !report.malformed;
        } );
#if defined(BENCH_MATCHED)
    g_gate = measured;
    bench_fixed( "bench_fixed", 400000L );
#endif

    flush_csv();

    if ( failed )
    {
        fprintf( stderr, "TABLES BENCH FAILED (corpus_id %s)\n", corpus_id().c_str() );
        return 1;
    }

    fprintf( stderr, "OK (corpus_id %s)\n", corpus_id().c_str() );
    ( void ) g_sink;
    return 0;
}
