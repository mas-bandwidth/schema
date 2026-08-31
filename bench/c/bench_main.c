/*
    schema bench — the C runner.

    Measures the schema-GENERATED C code (generated/c) against the serialize.c
    runtime: write path and read path, messages/sec and MB/sec, over the pinned
    corpus instances (the same instances test/main.cpp pins to the wire goldens
    in testdata/wire) plus one large synthetic message batch for steady-state
    dispatch throughput.

    This is a port of bench/cpp/bench_main.cpp, the reference runner, per the
    runner contract in bench/README.md: same benchmark set, same pinned
    instances, same LCG, same vary-function field mappings, same self-check
    gate (golden byte-compare + round trip BEFORE any number is produced), same
    median-of-7-after-a-warmup reporting.

    Two shape differences from the C++ reference, both forced by C and neither
    touching what is measured:

      - the per-message driver is a MACRO that expands one static function per
        message type, where the reference uses a function template. Same code,
        same direct (inlinable) calls to the generated writer and reader; a
        driver over void pointers and function pointers would have inserted an
        indirect call the reference does not pay.

    Output: a human table on stderr, and with --csv, CSV rows on stdout in the
    cross-language format (see bench/README.md), with `c` as the lang value.
*/

#define _POSIX_C_SOURCE 199309L

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <time.h>

#include "ConstantsWire.h"
#include "EnumsWire.h"
#include "TypesWire.h"
#include "WireWire.h"
#include "RealWorldWire.h"      /* generated/bench/c — the §1.7 realistic snapshot (real_packet) */
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
   the per-message benchmark driver — one expansion per message type, via
   bench_message.inc (#define BM_* + #include). The include template expands
   to the exact tokens the old BENCH_MESSAGE macro produced — the measured
   code is unchanged — but every expansion carries real line numbers, which
   is what lets bench/tools/inline-verdict.sh attribute a remaining runtime
   call to a bench's timed write loop, timed read loop, or untimed setup
   (see the header of bench_message.inc).
   ------------------------------------------------------------------------------------------ */

/* ------------------------------------------------------------------------------------------
   vary functions — mutate VALUE fields within wire ranges through the LCG;
   structure fields (counts, lengths, branch bools) stay fixed so bytes/op is
   constant. These reproduce bench/cpp/bench_main.cpp's mappings exactly.
   ------------------------------------------------------------------------------------------ */

static void vary_rigidbody( RigidBody * m, uint64_t rng )
{
    m->position.x = (double) ( (int64_t) ( rng >> 8 ) & 0xFFFF ) * 0.25;
    m->position.y = (double) ( (int64_t) ( rng >> 16 ) & 0xFFFF ) * 0.5;
    m->position.z = (double) ( (int64_t) ( rng >> 24 ) & 0xFFFF ) * 0.125;
    m->orientation.x = (double) ( (int64_t) rng & 0xFF ) * 0.001;
    m->linear_velocity.x = (double) ( (int64_t) ( rng >> 32 ) & 0xFFF ) * 0.25;
    m->angular_velocity.z = (double) ( (int64_t) ( rng >> 40 ) & 0xFFF ) * 0.125;
}

/* the at-rest twin varies only the fields its taken branch still writes */
static void vary_rigidbody_at_rest( RigidBody * m, uint64_t rng )
{
    m->position.x = (double) ( (int64_t) ( rng >> 8 ) & 0xFFFF ) * 0.25;
    m->position.y = (double) ( (int64_t) ( rng >> 16 ) & 0xFFFF ) * 0.5;
    m->orientation.x = (double) ( (int64_t) rng & 0xFF ) * 0.001;
}

static void vary_chat( Chat * m, uint64_t rng )
{
    int i;
    for ( i = 0; i < m->text_length; i++ )
        m->text[i] = (char) ( 'a' + ( ( rng >> ( i & 7 ) ) & 15 ) );    /* never zero */
}

static void vary_test( Test * m, uint64_t rng )
{
    m->test_a = (uint16_t) rng;
    m->test_b = (int16_t) ( ( rng >> 16 ) & 511 );      /* within [0, 1000] */
    m->test_c = (int16_t) ( ( rng >> 25 ) & 511 );
    m->test_d = (int16_t) ( ( rng >> 34 ) & 511 );
}

static void vary_inputpacket( InputPacket * m, uint64_t rng )
{
    m->synchronize_sequence = (uint16_t) rng;
    m->current_frame = rng;
    m->start_frame = rng >> 1;
    m->inputs[0].throttle = (float) ( (uint32_t) rng & 0xFF ) / 256.0f;
    m->inputs[0].fire = ( rng & 1 ) != 0;
    m->inputs[1].stick_x = (float) ( (uint32_t) ( rng >> 8 ) & 0xFF ) / 256.0f - 0.5f;
    m->inputs[1].boost = ( rng & 2 ) != 0;
}

static void vary_shipcreate( ShipCreate * m, uint64_t rng )
{
    m->position.x = (int32_t) ( ( rng >> 8 ) & 0xFFFFF ) - 0x80000;     /* within [-8388608, 8388608] */
    m->position.y = (int32_t) ( ( rng >> 16 ) & 0xFFFFF ) - 0x80000;
    m->position.z = (int32_t) ( ( rng >> 24 ) & 0xFFFFF ) - 0x80000;
    m->rotation.x = (int16_t) ( (int32_t) ( rng & 0x7FF ) - 1024 );     /* within [-1024, 1024] */
    m->linear_velocity.x = (int32_t) ( ( rng >> 32 ) & 0x3FFFFF ) - 2097152;
    m->flags = rng & 15;                                                /* 4 wire bits, has_flags stays true */
    m->health = (int16_t) ( ( rng >> 5 ) & 511 );                       /* within [0, 1000] */
    m->thrust = (int8_t) ( ( rng >> 14 ) & 63 );                        /* within [0, 100] */
}


static void vary_probe_header( ProbeHeader * m, uint64_t rng )
{
    m->version = (uint32_t) rng & 7;        /* 3 wire bits */
    m->probe_id = rng;
}

static void vary_probebits( ProbeBits * m, uint64_t rng )
{
    m->small = (uint32_t) rng & 511;                        /* 9 bits */
    m->boundary = rng & ( ( 1ULL << 33 ) - 1 );             /* 33 bits */
    m->wide = rng * 3;
    m->sensor = (uint32_t) ( rng >> 16 );
    m->nonce = rng ^ 0x5555555555555555ULL;
}

static void vary_probearray( ProbeArray * m, uint64_t rng )
{
    m->samples[0].orientation = -180.0f + (float) ( (uint32_t) rng & 0x3FFF ) * 0.02f;
    m->samples[0].raw_delta = (int32_t) ( (uint32_t) ( rng >> 8 ) );
    m->samples[0].big_delta = (int64_t) ( rng * 5 );
    m->samples[0].target_id = (uint16_t) ( rng >> 24 );
    m->samples[0].samples[0] = (uint16_t) ( rng >> 40 );
    m->samples[1].orientation = -180.0f + (float) ( (uint32_t) ( rng >> 3 ) & 0x3FFF ) * 0.02f;
    m->samples[1].idle_ticks = (uint32_t) ( rng >> 32 );
    m->samples[1].samples[0] = (uint16_t) ( rng >> 4 );
    m->samples[1].samples[1] = (uint16_t) ( rng >> 12 );
    m->config.retries = (int32_t) ( (uint32_t) ( rng >> 20 ) );
}

static void vary_testdata( TestData * m, uint64_t rng )
{
    int i;
    m->a = (int32_t) ( rng & 127 ) - 64;                        /* within [-100, 100] */
    m->b = (int32_t) ( ( rng >> 7 ) & 127 ) - 64;
    m->c = (int32_t) ( ( rng >> 14 ) & 127 ) - 64;              /* within [-100, 150] */
    m->d = (uint32_t) rng & 255;
    m->e = (uint32_t) ( rng >> 8 ) & 255;
    m->f = (uint32_t) ( rng >> 16 ) & 255;
    m->items[0] = (int32_t) ( rng & 255 );                      /* items_count stays 3 */
    m->items[1] = (int32_t) ( ( rng >> 8 ) & 255 );
    m->items[2] = (int32_t) ( ( rng >> 16 ) & 255 );
    m->float_value = (float) ( (uint32_t) rng & 0xFFFF );
    m->compressed_float_value = (float) ( (uint32_t) rng & 1023 ) * 0.005f;     /* within [0, 10] (max 5.115) */
    m->double_value = (double) ( (int64_t) ( rng >> 16 ) & 0xFFFFFF ) * 0.5;
    m->int8_value = (int8_t) rng;
    m->int16_value = (int16_t) ( rng >> 8 );
    m->uint8_value = (uint8_t) ( rng >> 16 );
    m->uint16_value = (uint16_t) ( rng >> 24 );
    m->uint32_value = (uint32_t) ( rng >> 32 );
    m->uint64_value = rng * 7;
    m->int64_full = (int64_t) ( rng * 11 );
    m->int64_range = (int64_t) ( ( rng >> 24 ) & ( ( 1ULL << 37 ) - 1 ) ) - ( 1LL << 36 );  /* within +/- 1e12 */
    m->fixed_bytes[0] = (uint8_t) rng;
    m->fixed_bytes[16] = (uint8_t) ( rng >> 8 );
    for ( i = 0; i < m->text_length; i++ )
        m->text[i] = (char) ( 'a' + ( ( rng >> ( i & 7 ) ) & 15 ) );    /* never zero */
}

/* real_packet — BENCH-STANDARD.md §1.7's realistic snapshot, measured through
   the GENERATED code (bench/corpus/RealWorld.schema -> generated/bench/c).
   The pinned instance is the ALL-DEFAULTS instance: new_real_packet()
   serialized unmodified, 1629 bits = 204 wire bytes, pinned to
   testdata/wire/real_packet.bin by test/bench/main.cpp. The four branch gates
   (f012 true, f043 false, f050 true, f074 false) are STRUCTURE (§2.7): they
   keep their schema defaults here, so the same branch bodies ride every
   iteration and bytes/op is constant. These mappings reproduce
   bench/cpp/bench_main.cpp's vary_real_packet exactly — fields under the
   false gates do not ride and are not varied; every mapping keeps its field
   inside its declared wire range (comments give the bound it stays within). */
static void vary_real_packet( RealPacket * m, uint64_t rng )
{
    /* ranged ints, assorted widths, signed and unsigned */
    m->f001_int  = (int32_t) ( ( rng >> 8 ) & 0xFFFFF ) - 0x80000;              /* +/-2^19 within +/-805495 */
    m->f003_int  = (int32_t) ( ( rng >> 12 ) & 0xFFFFF ) - 0x80000;             /* within +/-835897 */
    m->f005_uint = (uint16_t) ( ( rng >> 20 ) & 0xFFF );                        /* <=4095 within [0, 7316] */
    m->f006_int  = (int16_t) ( (int32_t) ( ( rng >> 26 ) & 0x7FF ) - 1024 );    /* +/-1024 within +/-1513 */
    m->f009_int  = (int8_t) ( (int32_t) ( ( rng >> 33 ) & 31 ) - 16 );          /* +/-16 within +/-22 */
    m->f033_uint = (uint32_t) ( ( rng >> 37 ) & 0x1FFFF );                      /* <=131071 within [0, 142780] */
    m->f041_int  = (int8_t) ( (int32_t) ( ( rng >> 42 ) & 63 ) - 32 );          /* +/-32 within +/-55 */
    m->f062_uint = (uint16_t) ( ( rng >> 47 ) & 255 );                          /* <=255 within [0, 503] */
    m->f088_int  = (int16_t) ( (int32_t) ( ( rng >> 52 ) & 0x3FF ) - 512 );     /* +/-512 within +/-694 */
    m->f090_uint = (uint8_t) ( ( rng >> 57 ) & 127 );                           /* <=127 within [0, 214] */
    /* bits(N), narrow and wide */
    m->f011_bits = (uint32_t) rng & 0x3FF;                                      /* 10 bits */
    m->f023_bits = (uint32_t) ( rng >> 5 ) & 0x1FFFFFF;                         /* 25 bits */
    m->f042_bits = (uint32_t) ( rng >> 3 ) & 0x3FFFFFFF;                        /* 30 bits */
    m->f081_bits = (uint32_t) ( rng >> 7 ) & 0x1FFFFFFF;                        /* 29 bits */
    m->f089_bits = rng & 0xFFFFFFFFFFFFULL;                                     /* 48 bits */
    m->f093_bits = rng ^ 0x5555555555555555ULL;                                 /* 64 bits */
    m->f097_bits = (uint32_t) ( rng >> 11 ) & 0xFFF;                            /* 12 bits */
    /* bools (NEVER the four branch gates — those are structure, §2.7) */
    m->f037_bool = ( rng & 1 ) != 0;
    m->f055_bool = ( rng & 2 ) != 0;
    m->f092_bool = ( rng & 4 ) != 0;
    /* float32 / float64 */
    m->f007_f32 = (float) ( (uint32_t) rng & 0xFFFF );
    m->f020_f32 = (float) ( (uint32_t) ( rng >> 16 ) & 0xFFFF ) * 0.5f;
    m->f058_f32 = (float) ( (uint32_t) ( rng >> 24 ) & 0xFFFF ) * 0.25f;
    m->f002_f64 = (double) ( (int64_t) ( rng >> 8 ) & 0xFFFFFF ) * 0.5;
    m->f059_f64 = (double) ( (int64_t) ( rng >> 16 ) & 0xFFFFFF ) * 0.25;
    m->f087_f64 = (double) ( (int64_t) ( rng >> 24 ) & 0xFFFFFF ) * 0.125;
    /* compressed floats (in range by construction) */
    m->f004_cf32 = (float) ( (uint32_t) rng & 0x3FFF ) * 0.1f;                  /* <=1638.3 within [0, 2000] */
    m->f061_cf32 = -90.0f + (float) ( (uint32_t) ( rng >> 9 ) & 255 ) * 0.5f;   /* within [-90, 90] (max 37.5) */
    m->f067_cf32 = -100.0f + (float) ( (uint32_t) ( rng >> 18 ) & 511 ) * 0.25f;    /* within [-100, 100] (max 27.75) */
    m->f072_cf32 = (float) ( (uint32_t) ( rng >> 27 ) & 8191 ) * 0.01f;         /* <=81.91 within [0, 100] */
    /* fixed / ufixed (raw storage scaled by 2^F; bounds are whole units) */
    m->f016_fixed  = (int32_t) ( ( rng >> 10 ) & 0x3FFFFFF ) - 0x2000000;       /* +/-2^25 within +/-36*2^20 */
    m->f025_fixed  = (int16_t) ( (int32_t) ( ( rng >> 18 ) & 0x7FFF ) - 0x4000 );   /* +/-2^14 within +/-119*2^8 */
    m->f095_fixed  = (int32_t) ( ( rng >> 22 ) & 0x7FFFFFF ) - 0x4000000;       /* +/-2^26 within +/-1577*2^16 */
    m->f021_ufixed = (uint32_t) ( rng >> 30 ) & 0x3FFFFFF;                      /* <=2^26-1 within 25141*2^12 */
    m->f049_ufixed = (uint16_t) ( ( rng >> 36 ) & 0x7FFF );                     /* <=32767 within 3*2^14 */
    m->f084_ufixed = (uint8_t) ( ( rng >> 44 ) & 0x7F );                        /* <=127 within 1*2^7 */
    /* enum / flags (wire-valid by construction) */
    m->f036_enum  = (PacketMode) ( (uint32_t) ( rng >> 30 ) & 3 );              /* within wire range [0, 5] */
    m->f083_enum  = (PacketMode) ( (uint32_t) ( rng >> 34 ) & 3 );
    m->f091_flags = rng & 31;                                                   /* 5 wire bits */
    /* full-width 64-bit */
    m->f008_u64 = rng;
    m->f029_i64 = (int64_t) ( rng * 3 );
    m->f063_i64 = (int64_t) ( rng * 5 );
    /* fields riding inside the TAKEN branches (f012 true, f050 true) */
    m->f013_f32  = (float) ( (uint32_t) ( rng >> 4 ) & 0xFFFF );
    m->f014_uint = (uint16_t) ( ( rng >> 21 ) & 511 );                          /* <=511 within [0, 775] */
    m->f015_int  = (int8_t) ( (int32_t) ( ( rng >> 40 ) & 31 ) - 16 );          /* +/-16 within +/-21 */
    m->f017_uint = (uint16_t) ( ( rng >> 29 ) & 0xFFF );                        /* <=4095 within [0, 4606] */
    m->f051_bool = ( rng & 8 ) != 0;
    m->f052_int  = (int8_t) ( (int32_t) ( ( rng >> 38 ) & 63 ) - 32 );          /* +/-32 within +/-57 */
    m->f053_f32  = (float) ( (uint32_t) ( rng >> 40 ) & 0xFFFF ) * 0.125f;
    m->f054_int  = (int8_t) ( (int32_t) ( ( rng >> 45 ) & 63 ) - 32 );          /* +/-32 within +/-35 */
}

#define BM_SUFFIX rigidbody
#define BM_TYPE RigidBody
#define BM_WRITE write_rigid_body
#define BM_READ read_rigid_body
#define BM_VARY vary_rigidbody
#include "bench_message.inc"
#define BM_SUFFIX rigidbody_at_rest
#define BM_TYPE RigidBody
#define BM_WRITE write_rigid_body
#define BM_READ read_rigid_body
#define BM_VARY vary_rigidbody_at_rest
#include "bench_message.inc"
#define BM_SUFFIX chat
#define BM_TYPE Chat
#define BM_WRITE write_chat
#define BM_READ read_chat
#define BM_VARY vary_chat
#include "bench_message.inc"
#define BM_SUFFIX test
#define BM_TYPE Test
#define BM_WRITE write_test
#define BM_READ read_test
#define BM_VARY vary_test
#include "bench_message.inc"
#define BM_SUFFIX inputpacket
#define BM_TYPE InputPacket
#define BM_WRITE write_input_packet
#define BM_READ read_input_packet
#define BM_VARY vary_inputpacket
#include "bench_message.inc"
#define BM_SUFFIX shipcreate
#define BM_TYPE ShipCreate
#define BM_WRITE write_ship_create
#define BM_READ read_ship_create
#define BM_VARY vary_shipcreate
#include "bench_message.inc"
#define BM_SUFFIX probe_header
#define BM_TYPE ProbeHeader
#define BM_WRITE write_probe_header
#define BM_READ read_probe_header
#define BM_VARY vary_probe_header
#include "bench_message.inc"
#define BM_SUFFIX probebits
#define BM_TYPE ProbeBits
#define BM_WRITE write_probe_bits
#define BM_READ read_probe_bits
#define BM_VARY vary_probebits
#include "bench_message.inc"
#define BM_SUFFIX probearray
#define BM_TYPE ProbeArray
#define BM_WRITE write_probe_array
#define BM_READ read_probe_array
#define BM_VARY vary_probearray
#include "bench_message.inc"
#define BM_SUFFIX testdata
#define BM_TYPE TestData
#define BM_WRITE write_test_data
#define BM_READ read_test_data
#define BM_VARY vary_testdata
#include "bench_message.inc"
#define BM_SUFFIX real_packet
#define BM_TYPE RealPacket
#define BM_WRITE write_real_packet
#define BM_READ read_real_packet
#define BM_VARY vary_real_packet
#include "bench_message.inc"

/* ------------------------------------------------------------------------------------------
   family gen over the Bench corpus (issue #177): the four Bench.schema shapes
   measured through the GENERATED code (generated/bench/c/BenchWire.h) — same
   golden files, same pinned values, same LCG field mappings, same
   bench_message.inc discipline as every gen row above. Generated best case
   per the profiling doctrine (#170): the plain optimized release build, no
   PGO.
   ------------------------------------------------------------------------------------------ */

static void vary_gen_packet( BenchPacket * p, uint64_t rng )
{
    p->a = (int32_t) ( ( rng >> 8 ) & 63 ) - 32;
    p->b = (int32_t) ( (uint32_t) ( rng >> 16 ) & 65535 );
    p->c = (int32_t) ( ( rng >> 24 ) & 0xFFFFF ) - 500000;
    p->bits7 = (uint32_t) rng & 127;
    p->bits13 = (uint32_t) ( rng >> 3 ) & 8191;
    p->bits23 = (uint32_t) ( rng >> 5 ) & 8388607;
    p->flag = ( rng & 1 ) != 0;
    p->x = (float) ( (uint32_t) rng & 0xFFFF );
    p->big = rng;
    p->blob[0] = (uint8_t) ( rng >> 32 );
}

static void vary_gen_ints( BenchInts * f, uint64_t rng )
{
    f->f0 = (int32_t) ( ( rng >> 8 ) & 63 ) - 32;
    f->f1 = (int32_t) ( (uint32_t) ( rng >> 16 ) & 65535 );
    f->f2 = (int32_t) ( ( rng >> 24 ) & 0xFFFFF ) - 500000;
    f->f3 = (int32_t) ( (uint32_t) ( rng >> 2 ) & 3 );
    f->f4 = (int32_t) ( ( rng >> 11 ) & 15 ) - 8;
    f->f5 = (int32_t) ( (uint32_t) ( rng >> 22 ) & 511 );
    f->f6 = (int32_t) ( ( rng >> 33 ) & 2047 ) - 1024;
    f->f7 = (int32_t) ( (uint32_t) ( rng >> 40 ) & 255 );
    f->f8 = (int32_t) ( ( rng >> 30 ) & 0xFFFFF ) - 500000;
    f->f9 = (int32_t) ( (uint32_t) ( rng >> 57 ) & 63 );
}

static void vary_gen_bits( BenchBits * f, uint64_t rng )
{
    f->b7 = (uint32_t) rng & 127;
    f->b13 = (uint32_t) ( rng >> 3 ) & 8191;
    f->b23 = (uint32_t) ( rng >> 5 ) & 8388607;
    f->b3 = (uint32_t) ( rng >> 29 ) & 7;
    f->b32 = (uint32_t) ( rng >> 16 );
    f->b11 = (uint32_t) ( rng >> 37 ) & 2047;
    f->b19 = (uint32_t) ( rng >> 44 ) & 524287;
    f->b48 = rng & 0xFFFFFFFFFFFFULL;
}

#define BM_SUFFIX gen_bench_packet
#define BM_TYPE BenchPacket
#define BM_WRITE write_bench_packet
#define BM_READ read_bench_packet
#define BM_VARY vary_gen_packet
#include "bench_message.inc"
#define BM_SUFFIX gen_bench_ints
#define BM_TYPE BenchInts
#define BM_WRITE write_bench_ints
#define BM_READ read_bench_ints
#define BM_VARY vary_gen_ints
#include "bench_message.inc"
#define BM_SUFFIX gen_bench_bits
#define BM_TYPE BenchBits
#define BM_WRITE write_bench_bits
#define BM_READ read_bench_bits
#define BM_VARY vary_gen_bits
#include "bench_message.inc"
#define BM_SUFFIX gen_bench_mixed
#define BM_TYPE BenchMixed
#define BM_WRITE write_bench_mixed
#define BM_READ read_bench_mixed
#include "bench_datadriven.inc"

/* ------------------------------------------------------------------------------------------
   pinned corpus instances — the same values test/main.cpp pins to the goldens
   ------------------------------------------------------------------------------------------ */

static RigidBody pin_rigidbody_moving( void )
{
    RigidBody in;
    memset( &in, 0, sizeof( in ) );
    in.position.x = 1.5; in.position.y = -2.5; in.position.z = 3.25;
    in.orientation.x = 0.1; in.orientation.y = 0.2; in.orientation.z = 0.3; in.orientation.w = 0.9;
    in.at_rest = 0;
    in.linear_velocity.x = 10.0; in.linear_velocity.y = 20.0; in.linear_velocity.z = -3.0;
    in.angular_velocity.x = 0.25; in.angular_velocity.y = 0.5; in.angular_velocity.z = 0.75;
    return in;
}

static Chat pin_chat( void )
{
    Chat in;
    memset( &in, 0, sizeof( in ) );
    memcpy( in.text, "wire parity", 11 );
    in.text_length = 11;
    return in;
}

static InputPacket pin_inputpacket( void )
{
    InputPacket in;
    memset( &in, 0, sizeof( in ) );
    in.synchronize_sequence = 7;
    in.current_frame = 123456789ULL;
    in.start_frame = 123456780ULL;
    in.inputs_count = 2;
    in.inputs[0].throttle = 0.5f;
    in.inputs[0].fire = 1;
    in.inputs[1].stick_x = -0.25f;
    in.inputs[1].boost = 1;
    return in;
}

static ShipCreate pin_shipcreate( void )
{
    ShipCreate in;
    memset( &in, 0, sizeof( in ) );
    in.ship_type = SHIP_TYPE_BOMBER;
    in.position.x = 1000; in.position.y = -2000; in.position.z = 3000;
    in.has_flags = 1;
    in.flags = SHIP_FLAGS_BOOSTING | SHIP_FLAGS_AIMING;
    in.team = TEAM_BLUE;
    in.health = 750;
    in.thrust = 55;
    return in;
}


static ProbeHeader pin_probe_header( void )
{
    ProbeHeader h;
    memset( &h, 0, sizeof( h ) );
    h.version = 5;
    h.probe_id = 0x1122334455667788ULL;
    return h;
}

static ProbeBits pin_probebits( void )
{
    ProbeBits in;
    memset( &in, 0, sizeof( in ) );
    in.small = 0x1FF;
    in.boundary = 0x1FFFFFFFFULL;
    in.wide = 0xFEDCBA9876543210ULL;
    in.sensor = 4294967295u;
    in.nonce = 18446744073709551615ULL;
    return in;
}

static ProbeArray pin_probearray( void )
{
    /* new_probe_array installs the SPECIFIED defaults (active = true,
       retries = -1, preferred = Railgun) exactly as C++ construction does —
       the pinned instance overrides only what test/main.cpp overrides */
    ProbeArray in = new_probe_array();
    in.samples[0].orientation = 90.0f;
    in.samples[0].raw_delta = -5;
    in.samples[0].big_delta = -1234567890123LL;
    in.samples[0].weapon = WEAPON_LASER;
    in.samples[0].has_target = 1;
    in.samples[0].target_id = 777;
    in.samples[0].samples_count = 1;
    in.samples[0].samples[0] = 42;
    in.samples[1].active = 0;
    in.samples[1].orientation = -45.5f;
    in.samples[1].raw_delta = 7;
    in.samples[1].big_delta = 99;
    in.samples[1].idle_ticks = 1000;
    in.samples[1].samples_count = 2;
    in.samples[1].samples[0] = 7;
    in.samples[1].samples[1] = 8;
    in.config.retries = 3;
    in.config.preferred = WEAPON_MISSILE;
    return in;
}

static TestData pin_testdata( void )
{
    TestData in;
    int i;
    memset( &in, 0, sizeof( in ) );
    in.a = -100;
    in.b = 100;
    in.c = 149;
    in.d = 0x11;
    in.e = 0x22;
    in.f = 0x33;
    in.g = 1;
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
    in.uint64_value = 18446744073709551615ULL;
    in.int64_full = ( -9223372036854775807LL - 1 );
    in.int64_range = -999999999999LL;
    for ( i = 0; i < 17; i++ )
    {
        in.fixed_bytes[i] = (uint8_t) ( i * 3 );
    }
    memcpy( in.text, "the quick brown fox", 19 );
    in.text_length = 19;
    return in;
}


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
    RigidBody moving, at_rest;
    Chat chat;
    Test test;
    InputPacket inputpacket;
    ShipCreate shipcreate;
    ProbeHeader probe_header;
    ProbeBits probebits;
    ProbeArray probearray;
    TestData testdata;
    RealPacket real_packet;

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


    moving = pin_rigidbody_moving();
    at_rest = moving;
    at_rest.at_rest = 1;
    chat = pin_chat();
    memset( &test, 0, sizeof( test ) );
    inputpacket = pin_inputpacket();
    shipcreate = pin_shipcreate();
    probe_header = pin_probe_header();
    probebits = pin_probebits();
    probearray = pin_probearray();
    testdata = pin_testdata();
    /* the §1.7 pin is the ALL-DEFAULTS instance: new_real_packet() installs
       the SPECIFIED defaults (the four branch gates: f012 true, f043 false,
       f050 true, f074 false) over zero, exactly as C++ RealPacket{} does */
    real_packet = new_real_packet();

    if ( !g_quick )
    {
        bench_message_rigidbody( "rigidbody_moving", "rigidbody_moving", 24000000L, &moving );
        bench_message_rigidbody_at_rest( "rigidbody_at_rest", "rigidbody_at_rest", 32000000L, &at_rest );
        bench_message_chat( "chat", "chat", 48000000L, &chat );
        bench_message_test( "test", NULL, 192000000L, &test );
        bench_message_inputpacket( "inputpacket", "inputpacket", 16000000L, &inputpacket );
        bench_message_shipcreate( "shipcreate", "shipcreate_flags", 32000000L, &shipcreate );
        bench_message_probe_header( "probe_header", "probe_header", 256000000L, &probe_header );
        bench_message_probebits( "probebits", "probebits", 128000000L, &probebits );
        bench_message_probearray( "probearray", "probearray", 20000000L, &probearray );
        bench_message_testdata( "testdata", "testdata", 8000000L, &testdata );

        /* real_packet (§1.7): the realistic snapshot — ~93 riding individually
           serialized small fields, 204 wire bytes, 0% bulk share by bits.
           base_iters sized in the C++ reference (§2.1: 8M puts the 200 ms floor
           at 40 M msg/s). */
        bench_message_real_packet( "real_packet", "real_packet", 8000000L, &real_packet );
    }

    /* family gen over the Bench corpus (issue #177): the four Bench.schema
       shapes through the generated code — same goldens, same pins, same vary
       mappings, same iteration counts (fixed and identical across all five
       runners, §2.1). --quick runs the gen bench_mixed. */
    {
        BenchPacket gen_packet;
        BenchInts gen_ints;
        BenchBits gen_bits;

        memset( &gen_packet, 0, sizeof( gen_packet ) );
        gen_packet.a = -37; gen_packet.b = 12345; gen_packet.c = 987654;
        gen_packet.bits7 = 97; gen_packet.bits13 = 5000; gen_packet.bits23 = 1234567;
        gen_packet.flag = 1;
        gen_packet.x = 1.5f; gen_packet.y = -3.25f; gen_packet.z = 100.125f;
        gen_packet.big = 0x123456789ABCDEF0ULL;
        for ( i = 0; i < 17; i++ )
            gen_packet.blob[i] = (uint8_t) ( i * 31 );

        memset( &gen_ints, 0, sizeof( gen_ints ) );
        gen_ints.f0 = -37; gen_ints.f1 = 12345; gen_ints.f2 = 987654; gen_ints.f3 = 2; gen_ints.f4 = -15;
        gen_ints.f5 = 777; gen_ints.f6 = -2048; gen_ints.f7 = 200; gen_ints.f8 = -543210; gen_ints.f9 = 99;

        memset( &gen_bits, 0, sizeof( gen_bits ) );
        gen_bits.b7 = 97; gen_bits.b13 = 5000; gen_bits.b23 = 1234567; gen_bits.b3 = 5;
        gen_bits.b32 = 0xDEADBEEFu; gen_bits.b11 = 1024; gen_bits.b19 = 333333;
        gen_bits.b48 = 0xFEDCBA987654ULL;

        if ( !g_quick )
        {
            bench_message_gen_bench_packet( "bench_packet", "bench_packet", 32000000L, &gen_packet );
            bench_message_gen_bench_ints( "bench_ints", "bench_ints", 40000000L, &gen_ints );
            bench_message_gen_bench_bits( "bench_bits", "bench_bits", 48000000L, &gen_bits );
        }
        /* --quick runs exactly this gen leg */
        bench_datadriven_gen_bench_mixed( "bench_mixed", "bench_mixed", 4000000L );
    }

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
