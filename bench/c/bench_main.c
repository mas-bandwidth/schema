/*
    schema bench — the C runner.

    Measures the schema-GENERATED C code (generated/bench/c) against the
    serialize.c runtime over THE one benchmark shape, BenchMixed
    (BENCH-STANDARD.md §1.3a): write path and round-trip path, messages/sec
    and MB/sec, driven entirely by the committed variant corpus. Plus family
    bits (§1.4), the raw bitpacker, which is not a shape at all.

    This is a port of bench/cpp/bench_main.cpp, the reference runner, per the
    runner contract in bench/README.md: same benchmark set, same variant
    corpus, same self-check gate (golden byte-compare + per-variant round trip
    BEFORE any number is produced), same median-of-7-after-a-warmup reporting.

    One shape difference from the C++ reference, forced by C and not touching
    what is measured: the per-message driver is an #include template that
    expands one static function per message type, where the reference uses a
    function template. Same code, same direct (inlinable) calls to the
    generated writer and reader; a driver over void pointers and function
    pointers would have inserted an indirect call the reference does not pay.

    Output: a human table on stderr, and with --csv, CSV rows on stdout in the
    cross-language format (see bench/README.md), with `c` as the lang value.
*/

#define _POSIX_C_SOURCE 199309L

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <time.h>

#include "BenchWire.h"          /* generated/bench/c — the Bench corpus GENERATED (issue #177) */

static volatile uint64_t g_sink = 0;    /* defeats dead code elimination of computed values */

/* Tells the compiler the memory at data is observed, so stores to it cannot be
   dead code eliminated (the standard empty-asm escape, as in serialize
   bench.cpp and the C++ runner). */
static void bench_escape( const void * data )
{
    __asm__ __volatile__( "" : : "g"( data ) : "memory" );
}

static double time_now( void )
{
    struct timespec ts;
    clock_gettime( CLOCK_MONOTONIC, &ts );
    return (double) ts.tv_sec + (double) ts.tv_nsec * 1e-9;
}

/* The LCG every runner must use (Knuth MMIX, as in serialize bench.cpp). */
static uint64_t bench_rng( uint64_t rng )
{
    return rng * 6364136223846793005ULL + 1442695040888963407ULL;
}

#define MaxNumRuns 7        /* median of 7 (N >= 5), after 1 warmup run */
static int g_quick = 0;             /* --quick: bench_mixed only, 3 measured runs —
                                       the iteration instrument, never certification */
static int g_num_runs = MaxNumRuns; /* --round K drops this to 1 (§2.4: one warmup +
                                       one measured run per round; the driver
                                       aggregates across rounds) */
#define NumVariants 64      /* read-path variant buffers */

#if defined(NDEBUG)
#define IterScale 1         /* Release: full fixed counts */
#else
#define IterScale 8         /* Debug: fixed counts / 8 (recorded in the iters column) */
#endif

static int g_csv = 0;
static const char * g_wire_dir = "testdata/wire";
static const char * g_variant_dir = "bench/corpus/variants";

/* ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
   Rows are buffered and emitted at exit so every row carries the corpus_id
   (§1.6): FNV-1a-64 over the goldens THIS RUN actually loaded — for each file
   in sorted basename order, the basename bytes, a 0x00 byte, the contents.
   The per-runner constants: family gen (these are the generated-code
   benchmarks), linkage HDR (serialize.c ruling of 2026-08-17, "everything
   in C should be inlined!!!" — serialize.c #25 moved the entire library
   into serialize.h; the old serialize.c TU is an empty compatibility stub,
   nothing links it here, and no call in any path crosses a library TU
   boundary. This runner said `hdr+tu` from 2026-08-15 to 2026-08-17, while
   the bulk and branchy bodies still lived in the compiled TU; with both
   runners at `hdr` the rel tool ratios C vs C++ without the linkage
   caption — the last caption, retired), checks REMOVED
   (§3.4: -DNDEBUG compiles serialize.c's asserts and checks away exactly
   as the C++ build does. This runner said `contract` from 2026-08-15 to
   2026-08-16, while serialize_write_bits kept an unconditional capacity
   check the C++ BitWriter only asserts — serialize.c ruling #20 removed
   it: "MINIMAL runtime checking in release, compiling to ZERO runtime
   checking in release for C and C++", so the hybrid `contract` described
   died and the two runners' check models are now the same word. With
   both sides `removed`, the rel tool ratios C vs C++ without the checks
   caption; with linkage also matched, no caption remains),
   opt from the build (run.sh sets BENCH_OPT beside the -O flag itself),
   inline unknown until the verdict pass (§4.2) backfills it. */
#ifndef BENCH_OPT
#define BENCH_OPT "O3"
#endif
/* family is per ROW now (gen | bits — §5.1); linkage/checks/opt/inline
   stay per-runner constants */
#define CsvSuffix "hdr,removed," BENCH_OPT ",unknown"
#define MaxCsvRows 64
#define MaxGoldens 24
static char g_csv_rows[MaxCsvRows][256];
static const char * g_csv_family[MaxCsvRows];
static int g_csv_row_count = 0;
static struct
{
    char name[64];
    /* sized for the LARGEST corpus member, which is no longer a wire golden:
       the bench_mixed variant file is 64 x 438 = 28032 bytes and hashes into
       corpus_id exactly as the goldens do (§1.6, issue #191). */
    uint8_t data[32768];
    int len;
} g_goldens_loaded[MaxGoldens];
static int g_golden_count = 0;

static void record_golden( const char * basename, const uint8_t * data, int len )
{
    int i;
    for ( i = 0; i < g_golden_count; i++ )
    {
        if ( strcmp( g_goldens_loaded[i].name, basename ) == 0 )
            return;     /* already recorded (loaded once per run anyway) */
    }
    if ( g_golden_count == MaxGoldens || len > (int) sizeof( g_goldens_loaded[0].data ) )
    {
        fprintf( stderr, "corpus_id: golden table overflow — raise MaxGoldens\n" );
        exit( 1 );
    }
    strncpy( g_goldens_loaded[g_golden_count].name, basename, sizeof( g_goldens_loaded[0].name ) - 1 );
    memcpy( g_goldens_loaded[g_golden_count].data, data, (size_t) len );
    g_goldens_loaded[g_golden_count].len = len;
    g_golden_count++;
}

static uint64_t fnv1a64( uint64_t h, const uint8_t * data, size_t n )
{
    size_t i;
    for ( i = 0; i < n; i++ )
    {
        h ^= data[i];
        h *= 0x100000001b3ULL;
    }
    return h;
}

static int compare_golden_name( const void * a, const void * b )
{
    return strcmp( (const char *) a, (const char *) b );    /* name is the first member */
}

static int failed = 0;

static void corpus_id_str( char * id )
{
    uint64_t h = 0xcbf29ce484222325ULL;
    uint8_t zero = 0;
    int i;
    qsort( g_goldens_loaded, (size_t) g_golden_count, sizeof( g_goldens_loaded[0] ), compare_golden_name );
    for ( i = 0; i < g_golden_count; i++ )
    {
        h = fnv1a64( h, (const uint8_t *) g_goldens_loaded[i].name, strlen( g_goldens_loaded[i].name ) );
        h = fnv1a64( h, &zero, 1 );
        h = fnv1a64( h, g_goldens_loaded[i].data, (size_t) g_goldens_loaded[i].len );
    }
    sprintf( id, "%016llx", (unsigned long long) h );
}

static void flush_csv( void )
{
    char id[17];
    int i;
    if ( !g_csv )
        return;
    if ( failed )
    {
        /* §1.5: a failing run emits NO rows — the exit code and stderr are
           the whole output. Numbers from a run whose gate refused are not
           numbers. */
        fprintf( stderr, "refusing to emit CSV rows from a failing run\n" );
        return;
    }
    corpus_id_str( id );
    for ( i = 0; i < g_csv_row_count; i++ )
        printf( "%s,%s,%s,%s\n", g_csv_rows[i], id, g_csv_family[i], CsvSuffix );
}

/* buffers: write buffers must be a multiple of 4 bytes and 4-byte aligned;
   read allocations extend >= 8 bytes past the packet (64-bit window contract).
   uint64_t storage gives both, portably — C99 has no alignas. 4096 covers the
   largest message (BLOCK_MAX_BYTES 2008) with slack on both contracts. */
#define BufferSize 4096
/* §2.7 variant-buffer stride: the 64 rotating read buffers are allocated at
   BufferSize + 64 per slot, NOT packed at exact 4096. At stride 4096 every
   head line maps into one of 4 L1 set-groups on the M2 (set bits [13:6]:
   4096 >> 6 = 64 sets per step, 64k mod 256 cycles {0,64,128,192}), and a
   fully-inlined memory-bound read feels every background conflict miss in
   those sets. At 4160 the step is 65 and gcd(65,256) = 1: 64 head lines,
   64 distinct sets. Identical in all five runners. The buffer passed to
   the streams stays BufferSize; the pad is address spacing only. */
#define VariantStride ( BufferSize + 64 )
static uint64_t g_buffer_storage[BufferSize / 8];
static uint64_t g_twin_storage[BufferSize / 8];
static uint64_t g_variant_storage[NumVariants][VariantStride / 8];
#define g_buffer ( (uint8_t *) g_buffer_storage )
#define g_twin ( (uint8_t *) g_twin_storage )
#define g_variant( k ) ( (uint8_t *) g_variant_storage[k] )

static void fail( const char * name, const char * what )
{
    fprintf( stderr, "FAILED: %s: %s\n", name, what );
    failed = 1;
}

static int check_golden( const char * name, const uint8_t * data, int bytes )
{
    char path[512];
    static uint8_t expected[BufferSize];
    FILE * f;
    size_t n;

    sprintf( path, "%s/%s.bin", g_wire_dir, name );
    f = fopen( path, "rb" );
    if ( !f )
    {
        fprintf( stderr, "missing wire golden %s — run from the schema repo root (or pass --wire-dir)\n", path );
        return 0;
    }
    n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    {
        char basename[64];
        sprintf( basename, "%s.bin", name );
        record_golden( basename, expected, (int) n );
    }
    if ( (int) n != bytes || memcmp( expected, data, (size_t) bytes ) != 0 )
    {
        fprintf( stderr, "WIRE GOLDEN MISMATCH: %s (%d golden vs %d actual bytes) — refusing to bench code that does not match the corpus\n",
                 name, (int) n, bytes );
        return 0;
    }
    return 1;
}

/* Loads <variant-dir>/<name>.variants.bin into the NumVariants §2.7-staggered
   slots and returns the record size, or -1. The records are fixed-width by
   construction (§2.7 pins every structure field), so the file needs no index:
   the record size IS file size / NumVariants, and a file that does not divide
   evenly is a refusal. */
static int load_variants( const char * name )
{
    char path[512];
    static uint8_t packed[32768];
    FILE * f;
    size_t n;
    int record, k;

    sprintf( path, "%s/%s.variants.bin", g_variant_dir, name );
    f = fopen( path, "rb" );
    if ( !f )
    {
        fprintf( stderr, "missing variant data %s — run `make bench-variants`, and run the bench from the schema repo root (or pass --variant-dir)\n", path );
        return -1;
    }
    n = fread( packed, 1, sizeof( packed ), f );
    fclose( f );
    if ( n == 0 || n % NumVariants != 0 )
    {
        fprintf( stderr, "variant data %s is %d bytes, not a multiple of %d records — refusing to bench data whose stride is not the record size\n",
                 path, (int) n, NumVariants );
        return -1;
    }
    record = (int) n / NumVariants;
    if ( record > BufferSize )
    {
        fprintf( stderr, "variant data %s has %d-byte records, over the %d-byte buffer\n", path, record, BufferSize );
        return -1;
    }
    for ( k = 0; k < NumVariants; k++ )
        memcpy( g_variant( k ), packed + k * record, (size_t) record );

    /* The variant data is corpus (§1.6): it defines the work inside the timed
       loops, so it rides in corpus_id exactly as the wire goldens do. A run
       against drifted variant data reports a different id and the tools refuse
       the ratio, instead of publishing a number for different work. */
    {
        char basename[64];
        sprintf( basename, "%s.variants.bin", name );
        record_golden( basename, packed, (int) n );
    }
    return record;
}

typedef struct RunStats
{
    double median_rate;     /* ops/sec */
    double min_rate;
    double max_rate;
    double spread_pct;      /* (max - min) / median * 100 */
} RunStats;

static int compare_double( const void * a, const void * b )
{
    const double x = *(const double *) a;
    const double y = *(const double *) b;
    if ( x < y ) return -1;
    if ( x > y ) return 1;
    return 0;
}

static RunStats run_stats( double * rates, int n )
{
    RunStats s;
    qsort( rates, (size_t) n, sizeof( double ), compare_double );
    s.median_rate = rates[n / 2];
    s.min_rate = rates[0];
    s.max_rate = rates[n - 1];
    s.spread_pct = ( s.max_rate - s.min_rate ) / s.median_rate * 100.0;
    return s;
}

static void report( const char * bench, const char * path, long iters, int bytes_per_op, RunStats s, const char * family )
{
    const double mbps = s.median_rate * (double) bytes_per_op / ( 1024.0 * 1024.0 );
    fprintf( stderr, "%-18s %-5s %10.2f M msg/s %10.1f MB/s   (min %.2f, max %.2f, spread %.1f%%)\n",
             bench, path, s.median_rate / 1e6, mbps, s.min_rate / 1e6, s.max_rate / 1e6, s.spread_pct );
    if ( g_csv )
    {
        if ( g_csv_row_count == MaxCsvRows )
        {
            fprintf( stderr, "csv row buffer overflow — raise MaxCsvRows\n" );
            exit( 1 );
        }
        /* sprintf, not snprintf: _POSIX_C_SOURCE 199309L predates C99's
           snprintf. Bounded by construction: 11 fields, none over 20 chars. */
        sprintf( g_csv_rows[g_csv_row_count],
                 "c,%s,%s,%ld,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
                 bench, path, iters, bytes_per_op, g_num_runs,
                 s.median_rate, s.min_rate, s.max_rate, mbps, s.spread_pct );
        g_csv_family[g_csv_row_count] = family;
        g_csv_row_count++;
    }
}

/* ------------------------------------------------------------------------------------------
   family gen over the Bench corpus: BenchMixed measured through the GENERATED
   code (generated/bench/c/BenchWire.h), driven entirely by the committed
   variant corpus — no hand-written pin, vary or sink code participates.
   Generated best case per the profiling doctrine (#170): the plain optimized
   release build, no PGO.
   ------------------------------------------------------------------------------------------ */

#define BM_SUFFIX gen_bench_mixed
#define BM_TYPE BenchMixed
#define BM_WRITE write_bench_mixed
#define BM_READ read_bench_mixed
#include "bench_datadriven.inc"

#define BENCH_NOINLINE __attribute__(( noinline ))

/* ------------------------------------------------------------------------------------------
   family bits (BENCH-STANDARD.md §1.4): the raw bit surface — serialize.c
   merged the bitpacker into its streams, so the raw layer here is
   serialize_write_bits / serialize_read_bits over a stream, exactly as
   serialize.c/bench.c has always benched it. The 16-width table (227
   bits/group) over a 65536-byte buffer; values vary per pass through the LCG
   (widths are the structure and stay fixed; bytes/pass asserted constant);
   reads rotate 64 pre-written variant buffers, each verified to read back
   exactly what was written before any number is produced.
   ------------------------------------------------------------------------------------------ */

#define BitsNumWidths 16
static const int bits_widths[BitsNumWidths] = { 1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22 };  /* 227 bits/group */
#define BitsBufferSize 65536

static serialize_uint64_t g_bits_buffer_storage[( BitsBufferSize + 8 ) / 8];
static serialize_uint64_t g_bits_variant_storage[NumVariants][( BitsBufferSize + 8 ) / 8];
#define g_bits_buffer ( (serialize_uint8_t *) g_bits_buffer_storage )
#define g_bits_variant( k ) ( (serialize_uint8_t *) g_bits_variant_storage[k] )

static serialize_uint32_t bits_mask( int width )
{
    return ( width == 32 ) ? 0xFFFFFFFFu : ( ( 1u << width ) - 1 );
}

/* the per-pass value variation: one LCG step per pass, values from its bits */
static void vary_bits_values( serialize_uint32_t * values, uint64_t rng )
{
    int i;
    for ( i = 0; i < BitsNumWidths; i++ )
        values[i] = (serialize_uint32_t) ( rng >> i ) & bits_mask( bits_widths[i] );
}

/* the single untimed serialize_write_bits call site (§3.2) */
static int bits_write_pass( serialize_uint8_t * buffer, const serialize_uint32_t * values )
{
    serialize_write_stream_t writer;
    int i;
    serialize_write_stream_init( &writer, buffer, BitsBufferSize );
    while ( serialize_write_bits_available( &writer ) >= 256 )
    {
        for ( i = 0; i < BitsNumWidths; i++ )
        {
            if ( !serialize_write_bits( &writer, values[i], bits_widths[i] ) )
                return -1;
        }
    }
    serialize_write_flush( &writer );
    return serialize_write_bytes_processed( &writer );
}

/* the single untimed serialize_read_bits call site (§3.2): the buffer must
   read back exactly the values written — the bits family's refusal gate */
static int bits_read_verify( const serialize_uint8_t * buffer, const serialize_uint32_t * values )
{
    serialize_read_stream_t reader;
    int i;
    serialize_read_stream_init( &reader, buffer, BitsBufferSize );
    while ( serialize_read_bits_remaining( &reader ) >= 256 )
    {
        for ( i = 0; i < BitsNumWidths; i++ )
        {
            serialize_uint32_t value = 0;
            if ( !serialize_read_bits( &reader, &value, bits_widths[i] ) )
                return 0;
            if ( value != values[i] )
                return 0;
        }
    }
    return 1;
}

BENCH_NOINLINE static int bitpacker_write_loop( long passes, int bytes_per_pass, uint64_t * rng, serialize_uint32_t * values )
{
    long pass;
    int i;
    for ( pass = 0; pass < passes; pass++ )
    {
        serialize_write_stream_t writer;
        *rng = bench_rng( *rng );
        vary_bits_values( values, *rng );
        serialize_write_stream_init( &writer, g_bits_buffer, BitsBufferSize );
        while ( serialize_write_bits_available( &writer ) >= 256 )
        {
            for ( i = 0; i < BitsNumWidths; i++ )
                serialize_write_bits( &writer, values[i], bits_widths[i] );
        }
        serialize_write_flush( &writer );
        if ( serialize_write_bytes_processed( &writer ) != bytes_per_pass )
            return 0;       /* the bytes_per_op assertion (§2.7) */
        bench_escape( g_bits_buffer );
        g_sink = g_sink + (uint64_t) serialize_write_bytes_processed( &writer );
    }
    return 1;
}

BENCH_NOINLINE static int bitpacker_read_loop( long passes )
{
    long pass;
    int i;
    for ( pass = 0; pass < passes; pass++ )
    {
        serialize_read_stream_t reader;
        uint64_t sum = 0;
        serialize_read_stream_init( &reader, g_bits_variant( pass & ( NumVariants - 1 ) ), BitsBufferSize );
        while ( serialize_read_bits_remaining( &reader ) >= 256 )
        {
            for ( i = 0; i < BitsNumWidths; i++ )
            {
                serialize_uint32_t value = 0;
                serialize_read_bits( &reader, &value, bits_widths[i] );
                sum += value;
            }
        }
        g_sink = g_sink + sum;
    }
    return 1;
}

static void bench_bitpacker( long base_passes )
{
    const long passes = base_passes / IterScale;
    serialize_uint32_t values[BitsNumWidths];
    double write_rates[MaxNumRuns];
    double read_rates[MaxNumRuns];
    uint64_t rng = 1;
    int bytes_per_pass = -1;
    int run, k;

    for ( k = 0; k < NumVariants; k++ )
    {
        int wrote;
        rng = bench_rng( rng );
        vary_bits_values( values, rng );
        wrote = bits_write_pass( g_bits_variant( k ), values );
        if ( bytes_per_pass < 0 )
            bytes_per_pass = wrote;
        if ( wrote < 0 || wrote != bytes_per_pass )
        {
            fail( "bitpacker", "variation changed bytes/pass — widths are the structure and must stay fixed" );
            return;
        }
        if ( !bits_read_verify( g_bits_variant( k ), values ) )
        {
            fail( "bitpacker", "read-back disagrees with written values — refusing to bench" );
            return;
        }
    }

    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        if ( !bitpacker_write_loop( passes, bytes_per_pass, &rng, values ) )
        {
            fail( "bitpacker", "bytes/pass changed in the timed loop (§2.7 assertion)" );
            return;
        }
        elapsed = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = (double) passes / elapsed;
    }

    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        if ( !bitpacker_read_loop( passes ) )
        {
            fail( "bitpacker", "read loop failed" );
            return;
        }
        elapsed = time_now() - start;
        if ( run >= 0 )
            read_rates[run] = (double) passes / elapsed;
    }

    report( "bitpacker", "write", passes, bytes_per_pass, run_stats( write_rates, g_num_runs ), "bits" );
    report( "bitpacker", "read", passes, bytes_per_pass, run_stats( read_rates, g_num_runs ), "bits" );
}


int main( int argc, char ** argv )
{
    int i;

    for ( i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--csv" ) == 0 )
            g_csv = 1;
        else if ( strcmp( argv[i], "--wire-dir" ) == 0 && i + 1 < argc )
            g_wire_dir = argv[++i];
        else if ( strcmp( argv[i], "--variant-dir" ) == 0 && i + 1 < argc )
            g_variant_dir = argv[++i];
        else if ( strcmp( argv[i], "--round" ) == 0 && i + 1 < argc )
        {
            /* §2.4: one warmup + one measured run of every benchmark, then
               exit. K only identifies the round to the interleaved driver,
               which aggregates max/median/min/spread across rounds itself. */
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
            g_quick = 1;
        else
        {
            fprintf( stderr, "usage: %s [--csv] [--round K] [--quick] [--wire-dir <dir>] [--variant-dir <dir>]\n", argv[0] );
            return 1;
        }
    }
    if ( g_quick && g_num_runs == MaxNumRuns )
        g_num_runs = 3;

#if defined(NDEBUG)
    fprintf( stderr, "schema bench (c, Release)\n" );
#else
    fprintf( stderr, "schema bench (c, Debug — only release numbers are meaningful)\n" );
#endif
    if ( g_quick )
        fprintf( stderr, "--quick: iteration instrument, not certification\n" );

    if ( g_csv )
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n" );


    /* family gen over the Bench corpus: BenchMixed through the generated code,
       fed by the committed variant corpus — same goldens, same iteration count
       in every runner (§2.1). This is the whole of the Bench-corpus leg, in
       --quick and in the full sweep alike. */
    bench_datadriven_gen_bench_mixed( "bench_mixed", "bench_mixed", 4000000L );

    /* family bits (§1.4): the one bitpacker workload in the estate */
    if ( !g_quick )
        bench_bitpacker( 24576L );

    flush_csv();    /* rows carry the corpus_id of the goldens this run loaded */

    {
        char id[17];
        corpus_id_str( id );
        if ( failed )
        {
            fprintf( stderr, "BENCH FAILED (corpus_id %s)\n", id );
            return 1;
        }
        fprintf( stderr, "OK (corpus_id %s)\n", id );
    }
    ( void ) g_sink;
    return 0;
}
