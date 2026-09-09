/* the tables bench — the C runner.
 *
 * Measures ONE thing: a representative fixed table written and read on the
 * TOLERANT WIRE (docs/SPEC-TABLES.md §3), through the generated table codec.
 * That is the number a reader who knows protobuf or flatbuffers already has a
 * comparison for, and it is the per-language release gate for the tables layer
 * (bench/tables/README.md).
 *
 * It is the C++ runner's sibling and follows the same contract
 * (BENCH-STANDARD.md): the committed variant corpus drives it, the golden gate
 * runs before the clock, the loops are escape-barriered against dead code
 * elimination, and the report is 1 warmup + 7 measured runs with the median
 * beside min/max/spread. What differs is the language and nothing else — the
 * corpus, the shape, the iteration count and the row spellings are the same.
 *
 * THIS FILE IS SHAPE-BLIND. It names the generated TYPE at one call site and
 * nothing else: no field, no pinned value, no wire size.
 *
 * Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
 */

#if defined( __linux__ ) && !defined( _POSIX_C_SOURCE )
#define _POSIX_C_SOURCE 199309L
#endif

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <time.h>

#if defined(BENCH_MATCHED)
#include "BenchTable.h"
#define TableMixed BenchMixed
#define table_mixed_reset bench_mixed_reset
#define table_mixed_save bench_mixed_save
#define table_mixed_load bench_mixed_load
#else
#include "BenchTableTable.h"
#endif

static volatile uint64_t g_sink = 0; /* defeats dead code elimination of computed values */

#if defined( _MSC_VER )
#include <intrin.h>
static void bench_escape( const void * data ) { (void) data; _ReadWriteBarrier(); }
#else
static void bench_escape( const void * data )
{
    __asm__ __volatile__( "" : : "g"( data ) : "memory" );
}
#endif

static double time_now( void )
{
    struct timespec ts;
    clock_gettime( CLOCK_MONOTONIC, &ts );
    return (double) ts.tv_sec + (double) ts.tv_nsec * 1e-9;
}

#define MaxNumRuns 7      /* median of 7 (N >= 5), after 1 warmup run */
#define NumVariants 64    /* read-path variant buffers */
static long g_iterations = 0; /* explicit diagnostic count; defaults stay standard */
static int g_num_runs = MaxNumRuns; /* --round K drops this to 1 (§2.4) */

#if defined( NDEBUG )
#define IterScale 1
#else
#define IterScale 8
#endif

static int g_csv = 0;
static int g_gate = 0;
static int g_indexed = 0;
static int64_t g_lengths[NumVariants];
static const char * g_wire_dir = "testdata/wire";
static const char * g_variant_dir = "bench/corpus/variants";

/* ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
   family `table` (§1.9): the tolerant table wire, which is a DIFFERENT wire
   over a different corpus, so a tools refusal to divide it against a `gen` row
   is correct and automatic. linkage hdr — the generated table codec is
   static-inline in this translation unit and names no runtime at all; the
   descriptors and the text form live in the .c beside the header and this
   runner compiles neither. checks contract — caller-error asserts are dormant
   under NDEBUG while the reader's wire-contract validation is unconditional. */
#ifndef BENCH_OPT
#define BENCH_OPT "O3"
#endif
static const char * g_csv_suffix = "hdr,contract," BENCH_OPT ",unknown";

#define MaxCsvRows 8
static char g_csv_rows[MaxCsvRows][256];
static int g_num_csv_rows = 0;

/* the goldens this run LOADED, in sorted basename order, for the corpus id */
#define MaxGoldens 4
typedef struct Golden
{
    char name[64];
    uint8_t * data;
    size_t bytes;
} Golden;
static Golden g_goldens[MaxGoldens];
static int g_num_goldens = 0;

static uint64_t fnv1a64( uint64_t h, const uint8_t * data, size_t n )
{
    size_t i;
    for ( i = 0; i < n; i++ )
    {
        h ^= data[i];
        h *= 0x100000001b3ull;
    }
    return h;
}

static void corpus_id( char * out, size_t size )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int i, j;
    /* sorted basename order, the same order std::map gives the reference */
    for ( i = 0; i < g_num_goldens; i++ )
    {
        for ( j = i + 1; j < g_num_goldens; j++ )
        {
            if ( strcmp( g_goldens[j].name, g_goldens[i].name ) < 0 )
            {
                Golden t = g_goldens[i];
                g_goldens[i] = g_goldens[j];
                g_goldens[j] = t;
            }
        }
    }
    for ( i = 0; i < g_num_goldens; i++ )
    {
        const uint8_t zero = 0;
        h = fnv1a64( h, (const uint8_t *) g_goldens[i].name, strlen( g_goldens[i].name ) );
        h = fnv1a64( h, &zero, 1 );
        h = fnv1a64( h, g_goldens[i].data, g_goldens[i].bytes );
    }
    snprintf( out, size, "%016llx", (unsigned long long) h );
}

static int failed = 0;

static void remember_golden( const char * name, const uint8_t * data, size_t bytes )
{
    if ( g_num_goldens >= MaxGoldens ) { return; }
    snprintf( g_goldens[g_num_goldens].name, sizeof( g_goldens[g_num_goldens].name ), "%s", name );
    g_goldens[g_num_goldens].data = (uint8_t *) malloc( bytes ? bytes : 1 );
    memcpy( g_goldens[g_num_goldens].data, data, bytes );
    g_goldens[g_num_goldens].bytes = bytes;
    g_num_goldens++;
}

static void flush_csv( void )
{
    char id[17];
    int i;
    if ( !g_csv ) { return; }
    if ( failed )
    {
        /* §1.5: a failing run emits NO rows. */
        fprintf( stderr, "refusing to emit CSV rows from a failing run\n" );
        return;
    }
    corpus_id( id, sizeof( id ) );
    for ( i = 0; i < g_num_csv_rows; i++ )
    {
        printf( "%s,%s,table,%s\n", g_csv_rows[i], id, g_csv_suffix );
    }
}

/* The tolerant wire spends bytes on ids, kinds and lengths, so a table record
   is several times its equivalent type's. The buffer is sized from the corpus
   at run time and this is only the ceiling the runner refuses past. */
#define BufferSize 65536
/* §2.7's variant stride, for the same reason and by the same arithmetic as the
   type runner's: a power-of-two stride maps every head line into a handful of
   L1 set groups and a memory-bound read then feels every background conflict
   miss. */
#define VariantStride ( BufferSize + 64 )
static uint8_t g_buffer[BufferSize];
static uint8_t g_twin[BufferSize];
static uint8_t * g_variants;

static uint8_t * variant( int k ) { return g_variants + (size_t) k * (size_t) VariantStride; }

static void fail( const char * name, const char * what )
{
    fprintf( stderr, "FAILED: %s: %s\n", name, what );
    failed = 1;
}

static uint8_t * read_file( const char * path, size_t * bytes )
{
    FILE * f = fopen( path, "rb" );
    long size;
    uint8_t * out;
    if ( f == NULL ) { return NULL; }
    fseek( f, 0, SEEK_END );
    size = ftell( f );
    fseek( f, 0, SEEK_SET );
    out = (uint8_t *) malloc( (size_t) ( size > 0 ? size : 1 ) );
    if ( out == NULL || ( size > 0 && fread( out, 1, (size_t) size, f ) != (size_t) size ) )
    {
        fclose( f );
        free( out );
        return NULL;
    }
    fclose( f );
    *bytes = (size_t) size;
    return out;
}

static int check_golden( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[512];
    char basename[80];
    uint8_t * expected;
    size_t size = 0;
    snprintf( path, sizeof( path ), "%s/%s.bin", g_wire_dir, name );
    expected = read_file( path, &size );
    if ( expected == NULL )
    {
        fprintf( stderr, "missing wire golden %s — run from the schema repo root (or pass --wire-dir)\n", path );
        return 0;
    }
    snprintf( basename, sizeof( basename ), "%s.bin", name );
    remember_golden( basename, expected, size );
    if ( (int64_t) size != bytes || memcmp( expected, data, (size_t) bytes ) != 0 )
    {
        fprintf( stderr, "WIRE GOLDEN MISMATCH: %s (%zu golden vs %lld actual bytes) — refusing to bench code that does not match the corpus\n",
                 name, size, (long long) bytes );
        free( expected );
        return 0;
    }
    free( expected );
    return 1;
}

typedef struct RunStats
{
    double median_rate;
    double min_rate;
    double max_rate;
    double spread_pct;
} RunStats;

static RunStats run_stats( double * rates, int n )
{
    RunStats s;
    int i, j;
    for ( i = 0; i < n; i++ )
    {
        for ( j = i + 1; j < n; j++ )
        {
            if ( rates[j] < rates[i] ) { double t = rates[i]; rates[i] = rates[j]; rates[j] = t; }
        }
    }
    s.median_rate = rates[n / 2];
    s.min_rate = rates[0];
    s.max_rate = rates[n - 1];
    s.spread_pct = ( s.max_rate - s.min_rate ) / s.median_rate * 100.0;
    return s;
}

static void report( const char * bench, const char * path, long iters, double bytes_per_op, const RunStats * s )
{
    const double mbps = s->median_rate * (double) bytes_per_op / ( 1024.0 * 1024.0 );
    fprintf( stderr, "%-18s %-11s %10.3f M msg/s %10.1f MB/s   (min %.3f, max %.3f, spread %.1f%%)\n",
             bench, path, s->median_rate / 1e6, mbps, s->min_rate / 1e6, s->max_rate / 1e6, s->spread_pct );
    if ( g_csv && g_num_csv_rows < MaxCsvRows )
    {
        snprintf( g_csv_rows[g_num_csv_rows], sizeof( g_csv_rows[0] ),
                  "c,%s,%s,%ld,%.17g,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
                  bench, path, iters, bytes_per_op, g_num_runs,
                  s->median_rate, s->min_rate, s->max_rate, mbps, s->spread_pct );
        g_num_csv_rows++;
    }
}

/* Load 64 concatenated records. --indexed requires 64 little-endian uint32
   lengths; otherwise the historical corpus has one fixed stride. Return the
   exact mean encoded bytes per record; buffer padding is never counted. */
static double load_variants( const char * name )
{
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s.variants.bin", g_variant_dir, name );
    size_t size = 0, offset = 0;
    uint8_t * packed = read_file( path, &size );
    int k;
    if ( packed == NULL ) return -1;
    snprintf( path, sizeof( path ), "%s.variants.bin", name );
    remember_golden( path, packed, size );
    if ( g_indexed )
    {
        size_t length_bytes = 0;
        uint8_t * lengths;
        snprintf( path, sizeof( path ), "%s/%s.lengths", g_variant_dir, name );
        lengths = read_file( path, &length_bytes );
        if ( lengths == NULL || length_bytes != NumVariants * 4 ) { free(lengths); free(packed); return -1; }
        snprintf( path, sizeof( path ), "%s.lengths", name );
        remember_golden( path, lengths, length_bytes );
        for ( k = 0; k < NumVariants; ++k )
        {
            uint32_t n = 0;
            unsigned j;
            for ( j = 0; j < 4; ++j ) n |= (uint32_t) lengths[k * 4 + j] << (j * 8);
            g_lengths[k] = n;
        }
        free( lengths );
    }
    else
    {
        if ( size == 0 || size % NumVariants ) { free(packed); return -1; }
        for ( k = 0; k < NumVariants; ++k ) g_lengths[k] = size / NumVariants;
    }
    g_variants = (uint8_t *) calloc( (size_t) NumVariants * VariantStride, 1 );
    if ( !g_variants ) { free(packed); return -1; }
    for ( k = 0; k < NumVariants; ++k )
    {
        const int64_t n = g_lengths[k];
        if ( n <= 0 || n > BufferSize || (size_t) n > size - offset ) { free(packed); return -1; }
        memcpy( variant(k), packed + offset, (size_t) n );
        offset += (size_t) n;
    }
    free( packed );
    if ( offset != size ) return -1;
    return (double) size / NumVariants;
}

/* ------------------------------------------------------------------------
   the one measured shape
   ------------------------------------------------------------------------

   Public Load restores declared defaults before overlaying the fields on the
   wire (§3). That work stays inside the clock; the runner adds no separate reset. */

static TableMixed g_instances[NumVariants];
static TableMixed g_out;

static int bench_load( TableMixed * value, const uint8_t * bytes, int64_t size )
{
    TableReport report;
    memset( &report, 0, sizeof( report ) );
    return table_mixed_load( value, bytes, size, &report ) && !report.malformed;
}

static void bench_table( const char * name, const char * golden, long base_iters )
{
    const long iters = g_iterations ? g_iterations : base_iters / IterScale;
    const double bytes_per_op = load_variants( name );
    double write_rates[MaxNumRuns];
    double roundtrip_rates[MaxNumRuns];
    RunStats w, rt;
    double read_time;
    int k, run;
    long i;

    if ( bytes_per_op < 0 ) { failed = 1; return; }

    /* gate 1 (§1.5): variant 0 IS the pinned instance. */
    if ( !check_golden( golden, variant( 0 ), g_lengths[0] ) ) { failed = 1; return; }

    /* gate 2: every variant loads, re-saves, and comes back byte-identical at
       the same length — before any clock starts. */
    for ( k = 0; k < NumVariants; k++ )
    {
        int64_t wrote;
        table_mixed_reset( &g_instances[k] );
        if ( !bench_load( &g_instances[k], variant( k ), g_lengths[k] ) )
        {
            fail( name, "load of a variant failed" );
            return;
        }
        wrote = table_mixed_save( &g_instances[k], g_twin, BufferSize );
        if ( wrote != g_lengths[k] || memcmp( g_twin, variant( k ), (size_t) g_lengths[k] ) != 0 )
        {
            fail( name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus" );
            return;
        }
    }

    /* gate 3: reused storage must also round-trip, including the last-to-first
       transition. Public Load owns default restoration; do not reset between loads. */
    for ( k = 0; k < 2 * NumVariants; k++ )
    {
        const int index = k & ( NumVariants - 1 );
        int64_t wrote;
        if ( !bench_load( &g_out, variant( index ), g_lengths[index] ) )
        {
            fail( name, "load into reused target failed" );
            return;
        }
        wrote = table_mixed_save( &g_out, g_twin, BufferSize );
        if ( wrote != g_lengths[index] || memcmp( g_twin, variant( index ), (size_t) g_lengths[index] ) != 0 )
        {
            fail( name, "reused target round-trip bytes differ" );
            return;
        }
    }

    if ( g_gate ) return;

    /* WRITE: save the 64 pre-loaded instances round-robin. */
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( i = 0; i < iters; i++ )
        {
            const int64_t wrote = table_mixed_save( &g_instances[i & ( NumVariants - 1 )], g_buffer, BufferSize );
            if ( wrote != g_lengths[i & ( NumVariants - 1 )] ) { fail( name, "save failed in loop" ); return; }
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) wrote;
        }
        elapsed = time_now() - start;
        if ( run >= 0 ) { write_rates[run] = (double) iters / elapsed; }
    }

    /* ROUND-TRIP: load a variant buffer, then re-save what came out. */
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( i = 0; i < iters; i++ )
        {
            int64_t wrote;
            if ( !bench_load( &g_out, variant( i & ( NumVariants - 1 ) ), g_lengths[i & ( NumVariants - 1 )] ) )
            {
                fail( name, "load failed in loop" );
                return;
            }
            wrote = table_mixed_save( &g_out, g_buffer, BufferSize );
            if ( wrote != g_lengths[i & ( NumVariants - 1 )] ) { fail( name, "re-save failed in loop" ); return; }
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) wrote;
        }
        elapsed = time_now() - start;
        if ( run >= 0 ) { roundtrip_rates[run] = (double) iters / elapsed; }
    }

    w = run_stats( write_rates, g_num_runs );
    rt = run_stats( roundtrip_rates, g_num_runs );
    report( name, "write", iters, bytes_per_op, &w );
    report( name, "round_trip", iters, bytes_per_op, &rt );

    /* READ is DERIVED, never measured: round-trip time minus write time. It
       prints for continuity and is NOT a CSV row — a derived number in the CSV
       would be divided as if it had been measured (§2.9). */
    read_time = 1.0 / rt.median_rate - 1.0 / w.median_rate;
    if ( read_time > 0 )
    {
        fprintf( stderr, "%-18s %-11s %10.3f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n",
                 name, "read", 1e-6 / read_time );
    }
}

int main( int argc, char ** argv )
{
    char id[17];
    int i;
    for ( i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--indexed" ) == 0 ) { g_indexed = 1; }
        else if ( strcmp( argv[i], "--gate" ) == 0 ) { g_gate = 1; }
        else if ( strcmp( argv[i], "--csv" ) == 0 ) { g_csv = 1; }
        else if ( strcmp( argv[i], "--wire-dir" ) == 0 && i + 1 < argc ) { g_wire_dir = argv[++i]; }
        else if ( strcmp( argv[i], "--variant-dir" ) == 0 && i + 1 < argc ) { g_variant_dir = argv[++i]; }
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

#if defined( NDEBUG )
    fprintf( stderr, "schema tables bench (c, Release)\n" );
#else
    fprintf( stderr, "schema tables bench (c, Debug — only release numbers are meaningful)\n" );
#endif

    if ( g_csv )
    {
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n" );
    }

    /* The one measured shape, named once — the generated type at the call site
       and nothing else about it (bench/SHAPE-GATE.allow). */
    bench_table( "bench_table", "bench_table", 400000L );

    flush_csv();

    corpus_id( id, sizeof( id ) );
    if ( failed )
    {
        fprintf( stderr, "TABLES BENCH FAILED (corpus_id %s)\n", id );
        return 1;
    }
    fprintf( stderr, "OK (corpus_id %s)\n", id );
    (void) g_sink;
    return 0;
}
