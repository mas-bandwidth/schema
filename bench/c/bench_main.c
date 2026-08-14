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
      - the generated C dispatch surface has no message-level MAX_BYTES
        constant (the other four targets emit one — MessageMaxBytes /
        MESSAGE_MAX_BYTES), so the batch buffer is sized from the largest arm's
        BLOCK_MAX_BYTES plus the tag. That is the same bound by a different
        spelling; it is not a measurement difference.

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
#include "ContextsWire.h"
#include "EnumsWire.h"
#include "MessagesWire.h"
#include "ObjectsWire.h"
#include "TypesWire.h"
#include "WireWire.h"

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

/* ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
   Rows are buffered and emitted at exit so every row carries the corpus_id
   (§1.6): FNV-1a-64 over the goldens THIS RUN actually loaded — for each file
   in sorted basename order, the basename bytes, a 0x00 byte, the contents.
   The per-runner constants: family gen (these are the generated-code
   benchmarks), linkage tu (serialize.c is a compiled translation unit, no
   LTO — the packaging the C row has always carried), checks CONTRACT
   (§3.4's third value, added 2026-08-15: -DNDEBUG compiles serialize.c's
   caller-error asserts away like `removed`, but the write-capacity check
   that doubles as the sticky-error flag is unconditional by contract in
   every build — serialize_write_bits keeps it as a real check where the
   C++ BitWriter only asserts, and that hybrid was the entire C-vs-C++
   write residual story; neither `removed` nor `always` could say it),
   opt from the build (run.sh sets BENCH_OPT beside the -O flag itself),
   inline unknown until the verdict pass (§4.2) backfills it. */
#ifndef BENCH_OPT
#define BENCH_OPT "O3"
#endif
/* family is per ROW now (gen | rt | bits — §5.1); linkage/checks/opt/inline
   stay per-runner constants */
#define CsvSuffix "tu,contract," BENCH_OPT ",unknown"
#define MaxCsvRows 64
#define MaxGoldens 24
static char g_csv_rows[MaxCsvRows][256];
static const char * g_csv_family[MaxCsvRows];
static int g_csv_row_count = 0;
static struct
{
    char name[64];
    uint8_t data[4096];
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
static uint64_t g_buffer_storage[BufferSize / 8];
static uint64_t g_twin_storage[BufferSize / 8];
static uint64_t g_variant_storage[NumVariants][BufferSize / 8];
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

static void vary_ship_shallow( ShipData_Shallow * m, uint64_t rng )
{
    m->position_x = (int32_t) ( ( rng >> 8 ) & 0xFFFFF ) - 0x80000;
    m->position_y = (int32_t) ( ( rng >> 16 ) & 0xFFFFF ) - 0x80000;
    m->position_z = (int32_t) ( ( rng >> 24 ) & 0xFFFFF ) - 0x80000;
    m->rotation_x = (int16_t) ( (int32_t) ( rng & 0x7FF ) - 1024 );
    m->rotation_w = (int16_t) ( (int32_t) ( ( rng >> 11 ) & 0x7FF ) - 1024 );
    m->linear_velocity_x = (int32_t) ( ( rng >> 32 ) & 0x3FFFFF ) - 2097152;
    m->flags = rng & 15;
    m->health = (uint16_t) ( ( rng >> 5 ) & 511 );
    m->thrust = (uint8_t) ( ( rng >> 14 ) & 63 );
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
#define BM_SUFFIX ship_shallow
#define BM_TYPE ShipData_Shallow
#define BM_WRITE write_ship_shallow
#define BM_READ read_ship_shallow
#define BM_VARY vary_ship_shallow
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

static ShipData_Shallow pin_ship_shallow( void )
{
    ShipData_Interpolate interp;
    ShipData_Shallow q;
    memset( &interp, 0, sizeof( interp ) );
    memset( &q, 0, sizeof( q ) );
    interp.ship_type = SHIP_TYPE_CORVETTE;
    interp.position.x = 1.5; interp.position.y = -2.25; interp.position.z = 100.0;
    interp.rotation.x = 0.0; interp.rotation.y = 0.0; interp.rotation.z = 0.0; interp.rotation.w = 1.0;
    interp.linear_velocity.x = 3.0; interp.linear_velocity.y = 0.0; interp.linear_velocity.z = -1.0;
    interp.flags = SHIP_FLAGS_BOOSTING;
    interp.team = TEAM_RED;
    interp.health = 750;
    interp.thrust = 55;
    quantize_ship( &interp, &q );
    return q;
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

/* ------------------------------------------------------------------------------------------
   the synthetic steady-state batch: NumBatchMessages messages through the
   message dispatch surface (write_message/read_message) plus the None
   terminator, mixed types driven by the LCG — the same mix as the reference.
   ------------------------------------------------------------------------------------------ */

#define NumBatchMessages 4096
#define BatchPasses 6400

/* the generated C dispatch surface carries no message-level MAX_BYTES (see the
   header comment); BLOCK is the largest arm and the tag adds 3 bits */
#define BenchMessageMaxBytes ( BLOCK_MAX_BYTES + 8 )

static Message g_batch[NumBatchMessages];

static int build_batch( int * batch_bytes, uint8_t * batch_buffer, int batch_buffer_size )
{
    serialize_write_stream_t ws;
    Message terminator;
    uint64_t rng = 12345;
    int k, i;

    for ( k = 0; k < NumBatchMessages; k++ )
    {
        Message * m = &g_batch[k];
        int pick;
        rng = bench_rng( rng );
        pick = (int) ( ( rng >> 32 ) % 20 );
        memset( m, 0, sizeof( *m ) );
        if ( pick < 5 )                     /* 25% Chat */
        {
            m->type = MESSAGE_TYPE_CHAT;
            m->as.chat.text_length = 16 + (int) ( rng & 15 );
            for ( i = 0; i < m->as.chat.text_length; i++ )
                m->as.chat.text[i] = (char) ( 'a' + ( ( rng >> ( i & 7 ) ) & 15 ) );
        }
        else if ( pick < 10 )               /* 25% Test */
        {
            m->type = MESSAGE_TYPE_TEST;
            m->as.test.test_a = (uint16_t) rng;
            m->as.test.test_b = (int16_t) ( ( rng >> 16 ) & 511 );
            m->as.test.test_c = (int16_t) ( ( rng >> 25 ) & 511 );
            m->as.test.test_d = (int16_t) ( ( rng >> 34 ) & 511 );
        }
        else if ( pick < 13 )               /* 15% Synchronize */
        {
            m->type = MESSAGE_TYPE_SYNCHRONIZE;
            m->as.synchronize.sync_frame = rng;
            m->as.synchronize.sync_sequence = (uint16_t) ( rng >> 8 );
        }
        else if ( pick < 16 )               /* 15% Timescale */
        {
            m->type = MESSAGE_TYPE_TIMESCALE;
            m->as.timescale.scale = (double) ( (uint32_t) rng & 0xFFFF ) / 65536.0;
            m->as.timescale.frame_a = (uint32_t) ( rng >> 16 );
            m->as.timescale.frame_b = (uint32_t) ( rng >> 24 );
        }
        else if ( pick < 18 )               /* 10% Heartbeat */
        {
            m->type = MESSAGE_TYPE_HEARTBEAT;
        }
        else                                /* 10% Block */
        {
            m->type = MESSAGE_TYPE_BLOCK;
            m->as.block.data_length = 64 + (int) ( rng & 127 );
            for ( i = 0; i < m->as.block.data_length; i++ )
                m->as.block.data[i] = (uint8_t) ( rng >> ( i & 31 ) );
        }
    }

    serialize_write_stream_init( &ws, batch_buffer, batch_buffer_size );
    for ( k = 0; k < NumBatchMessages; k++ )
    {
        if ( !write_message( &ws, &g_batch[k] ) )
        {
            fail( "message_batch", "batch write failed during setup" );
            return 0;
        }
    }
    memset( &terminator, 0, sizeof( terminator ) );     /* type == None */
    if ( !write_message( &ws, &terminator ) )
    {
        fail( "message_batch", "terminator write failed during setup" );
        return 0;
    }
    serialize_write_flush( &ws );
    *batch_bytes = serialize_write_bytes_processed( &ws );
    return 1;
}

static void bench_batch( void )
{
    /* worst case is NumBatchMessages * the largest message; actual is far
       less. + 8 read slack, and the size is a multiple of 8 (write contract) */
    const int batch_buffer_size = ( NumBatchMessages + 1 ) * BenchMessageMaxBytes + 8;
    uint8_t * batch_buffer = (uint8_t *) malloc( (size_t) batch_buffer_size );
    int batch_bytes = 0;
    long passes, total_msgs, pass;
    double write_rates[MaxNumRuns];
    double read_rates[MaxNumRuns];
    uint64_t rng = 999;
    Message m;
    int run, k;

    if ( !batch_buffer )
    {
        fail( "message_batch", "allocation failed" );
        return;
    }
    memset( batch_buffer, 0, (size_t) batch_buffer_size );

    if ( !build_batch( &batch_bytes, batch_buffer, batch_buffer_size ) )
    {
        free( batch_buffer );
        return;
    }

    passes = BatchPasses / ( IterScale > 4 ? 4 : IterScale );    /* debug: /4 only — the batch is already slow */
    total_msgs = passes * NumBatchMessages;

    /* write path: whole batch per pass; one message mutates per pass so the
       batch is never loop-invariant */
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( pass = 0; pass < passes; pass++ )
        {
            serialize_write_stream_t ws;
            Message terminator;
            Message * mutate;
            rng = bench_rng( rng );
            mutate = &g_batch[( rng >> 16 ) % NumBatchMessages];
            if ( mutate->type == MESSAGE_TYPE_SYNCHRONIZE )
                mutate->as.synchronize.sync_frame = rng;
            else if ( mutate->type == MESSAGE_TYPE_TEST )
                mutate->as.test.test_a = (uint16_t) rng;
            serialize_write_stream_init( &ws, batch_buffer, batch_buffer_size );
            for ( k = 0; k < NumBatchMessages; k++ )
            {
                if ( !write_message( &ws, &g_batch[k] ) )
                {
                    fail( "message_batch", "write failed in loop" );
                    free( batch_buffer );
                    return;
                }
            }
            memset( &terminator, 0, sizeof( terminator ) );
            write_message( &ws, &terminator );
            serialize_write_flush( &ws );
            bench_escape( batch_buffer );
            g_sink = g_sink + (uint64_t) serialize_write_bytes_processed( &ws );
        }
        elapsed = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = (double) total_msgs / elapsed;
    }

    /* the read buffer: rebuild once from the final batch state so bytes match */
    if ( !build_batch( &batch_bytes, batch_buffer, batch_buffer_size ) )
    {
        free( batch_buffer );
        return;
    }

    /* read path: read messages until the None terminator, whole batch per
       pass; ONE reused Message hoisted out of the loop — read_message
       re-establishes the selected arm itself (it zeroes before decoding), so
       reuse is exact and a fresh Message per read is pure setup overhead */
    memset( &m, 0, sizeof( m ) );
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( pass = 0; pass < passes; pass++ )
        {
            serialize_read_stream_t rs;
            long count = 0;
            serialize_read_stream_init( &rs, batch_buffer, batch_bytes );
            for ( ;; )
            {
                if ( !read_message( &rs, &m ) )
                {
                    fail( "message_batch", "read failed in loop" );
                    free( batch_buffer );
                    return;
                }
                if ( m.type == MESSAGE_TYPE_NONE )
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
        elapsed = time_now() - start;
        if ( run >= 0 )
            read_rates[run] = (double) total_msgs / elapsed;
    }

    report( "message_batch", "write", total_msgs, batch_bytes / NumBatchMessages, run_stats( write_rates, g_num_runs ), "gen" );
    report( "message_batch", "read", total_msgs, batch_bytes / NumBatchMessages, run_stats( read_rates, g_num_runs ), "gen" );

    free( batch_buffer );
}

/* ------------------------------------------------------------------------------------------
   family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize.c runtime API
   called BY HAND — the four Bench.schema shapes as hand-written packets, the
   way serialize.c/bench.c writes them. The §1.5 oracle gate byte-compares the
   hand-written wire against the goldens the GENERATED code pinned
   (testdata/wire/bench_*.bin) and round-trips before any number. Internal
   linkage per §3.1 (static). Per §3.2 every benched op has EXACTLY two call
   sites: the untimed once-helper and its timed loop.
   ------------------------------------------------------------------------------------------ */

#define BENCH_NOINLINE __attribute__(( noinline ))

typedef struct RtBenchPacket
{
    serialize_int32_t a, b, c;
    serialize_uint32_t bits7, bits13, bits23;
    int flag;
    float x, y, z;
    serialize_uint64_t big;
    serialize_uint8_t blob[17];
} RtBenchPacket;

static int rt_write_bench_packet( serialize_write_stream_t * stream, const RtBenchPacket * p )
{
    if ( !serialize_write_int( stream, p->a, -100, +100 ) ) { return 0; }
    if ( !serialize_write_int( stream, p->b, 0, 65535 ) ) { return 0; }
    if ( !serialize_write_int( stream, p->c, -1000000, +1000000 ) ) { return 0; }
    if ( !serialize_write_bits( stream, p->bits7, 7 ) ) { return 0; }
    if ( !serialize_write_bits( stream, p->bits13, 13 ) ) { return 0; }
    if ( !serialize_write_bits( stream, p->bits23, 23 ) ) { return 0; }
    if ( !serialize_write_bool( stream, p->flag ) ) { return 0; }
    if ( !serialize_write_float( stream, p->x ) ) { return 0; }
    if ( !serialize_write_float( stream, p->y ) ) { return 0; }
    if ( !serialize_write_float( stream, p->z ) ) { return 0; }
    if ( !serialize_write_uint64( stream, p->big ) ) { return 0; }
    if ( !serialize_write_bytes( stream, p->blob, (int) sizeof( p->blob ) ) ) { return 0; }  /* aligns internally */
    return 1;
}

static int rt_read_bench_packet( serialize_read_stream_t * stream, RtBenchPacket * p )
{
    if ( !serialize_read_int( stream, &p->a, -100, +100 ) ) { return 0; }
    if ( !serialize_read_int( stream, &p->b, 0, 65535 ) ) { return 0; }
    if ( !serialize_read_int( stream, &p->c, -1000000, +1000000 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &p->bits7, 7 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &p->bits13, 13 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &p->bits23, 23 ) ) { return 0; }
    if ( !serialize_read_bool( stream, &p->flag ) ) { return 0; }
    if ( !serialize_read_float( stream, &p->x ) ) { return 0; }
    if ( !serialize_read_float( stream, &p->y ) ) { return 0; }
    if ( !serialize_read_float( stream, &p->z ) ) { return 0; }
    if ( !serialize_read_uint64( stream, &p->big ) ) { return 0; }
    if ( !serialize_read_bytes( stream, p->blob, (int) sizeof( p->blob ) ) ) { return 0; }
    return 1;
}

typedef struct RtBenchInts
{
    serialize_int32_t f0, f1, f2, f3, f4, f5, f6, f7, f8, f9;
} RtBenchInts;

static int rt_write_bench_ints( serialize_write_stream_t * stream, const RtBenchInts * f )
{
    if ( !serialize_write_int( stream, f->f0, -100, +100 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f1, 0, 65535 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f2, -1000000, +1000000 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f3, 0, 3 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f4, -15, +15 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f5, 0, 1000 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f6, -2048, +2047 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f7, 0, 255 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f8, -600000, +600000 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->f9, 0, 100 ) ) { return 0; }
    return 1;
}

static int rt_read_bench_ints( serialize_read_stream_t * stream, RtBenchInts * f )
{
    if ( !serialize_read_int( stream, &f->f0, -100, +100 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f1, 0, 65535 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f2, -1000000, +1000000 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f3, 0, 3 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f4, -15, +15 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f5, 0, 1000 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f6, -2048, +2047 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f7, 0, 255 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f8, -600000, +600000 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->f9, 0, 100 ) ) { return 0; }
    return 1;
}

typedef struct RtBenchBits
{
    serialize_uint32_t b7, b13, b23, b3, b32, b11, b19;
    serialize_uint64_t b48;
} RtBenchBits;

/* 48 bits ride as lo32 + hi16, low first — the wire order the generated C
   emits and serialize.c's 64-bit convention */
static int rt_write_bench_bits( serialize_write_stream_t * stream, const RtBenchBits * f )
{
    if ( !serialize_write_bits( stream, f->b7, 7 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->b13, 13 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->b23, 23 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->b3, 3 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->b32, 32 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->b11, 11 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->b19, 19 ) ) { return 0; }
    if ( !serialize_write_bits( stream, (serialize_uint32_t) ( f->b48 & 0xFFFFFFFFu ), 32 ) ) { return 0; }
    if ( !serialize_write_bits( stream, (serialize_uint32_t) ( f->b48 >> 32 ), 16 ) ) { return 0; }
    return 1;
}

static int rt_read_bench_bits( serialize_read_stream_t * stream, RtBenchBits * f )
{
    serialize_uint32_t lo, hi;
    if ( !serialize_read_bits( stream, &f->b7, 7 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->b13, 13 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->b23, 23 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->b3, 3 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->b32, 32 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->b11, 11 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->b19, 19 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &lo, 32 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &hi, 16 ) ) { return 0; }
    f->b48 = (serialize_uint64_t) lo | ( (serialize_uint64_t) hi << 32 );
    return 1;
}

typedef struct RtBenchMixed
{
    serialize_int32_t sequence;
    serialize_uint32_t ack_bits, entity_id;
    serialize_int32_t pos_x, pos_y, pos_z;
    serialize_uint32_t yaw;
    int moving, firing;
    serialize_uint64_t timestamp;
    serialize_int32_t weapon;
} RtBenchMixed;

static int rt_write_bench_mixed( serialize_write_stream_t * stream, const RtBenchMixed * f )
{
    if ( !serialize_write_int( stream, f->sequence, 0, 65535 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->ack_bits, 32 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->entity_id, 12 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->pos_x, -16384, +16383 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->pos_y, -16384, +16383 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->pos_z, -16384, +16383 ) ) { return 0; }
    if ( !serialize_write_bits( stream, f->yaw, 9 ) ) { return 0; }
    if ( !serialize_write_bool( stream, f->moving ) ) { return 0; }
    if ( !serialize_write_bool( stream, f->firing ) ) { return 0; }
    if ( !serialize_write_bits( stream, (serialize_uint32_t) ( f->timestamp & 0xFFFFFFFFu ), 32 ) ) { return 0; }
    if ( !serialize_write_bits( stream, (serialize_uint32_t) ( f->timestamp >> 32 ), 16 ) ) { return 0; }
    if ( !serialize_write_int( stream, f->weapon, 0, 15 ) ) { return 0; }
    return 1;
}

static int rt_read_bench_mixed( serialize_read_stream_t * stream, RtBenchMixed * f )
{
    serialize_uint32_t lo, hi;
    if ( !serialize_read_int( stream, &f->sequence, 0, 65535 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->ack_bits, 32 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->entity_id, 12 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->pos_x, -16384, +16383 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->pos_y, -16384, +16383 ) ) { return 0; }
    if ( !serialize_read_int( stream, &f->pos_z, -16384, +16383 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &f->yaw, 9 ) ) { return 0; }
    if ( !serialize_read_bool( stream, &f->moving ) ) { return 0; }
    if ( !serialize_read_bool( stream, &f->firing ) ) { return 0; }
    if ( !serialize_read_bits( stream, &lo, 32 ) ) { return 0; }
    if ( !serialize_read_bits( stream, &hi, 16 ) ) { return 0; }
    f->timestamp = (serialize_uint64_t) lo | ( (serialize_uint64_t) hi << 32 );
    if ( !serialize_read_int( stream, &f->weapon, 0, 15 ) ) { return 0; }
    return 1;
}

/* ---- vary functions: bench/cpp/bench_main.cpp's rt mappings exactly ---- */

static void vary_rt_packet( RtBenchPacket * p, uint64_t rng )
{
    p->a = (serialize_int32_t) ( ( rng >> 8 ) & 63 ) - 32;
    p->b = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 16 ) & 65535 );
    p->c = (serialize_int32_t) ( ( rng >> 24 ) & 0xFFFFF ) - 500000;
    p->bits7 = (serialize_uint32_t) rng & 127;
    p->bits13 = (serialize_uint32_t) ( rng >> 3 ) & 8191;
    p->bits23 = (serialize_uint32_t) ( rng >> 5 ) & 8388607;
    p->flag = ( rng & 1 ) != 0;
    p->x = (float) ( (serialize_uint32_t) rng & 0xFFFF );
    p->big = rng;
    p->blob[0] = (serialize_uint8_t) ( rng >> 32 );
}

static void vary_rt_ints( RtBenchInts * f, uint64_t rng )
{
    f->f0 = (serialize_int32_t) ( ( rng >> 8 ) & 63 ) - 32;
    f->f1 = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 16 ) & 65535 );
    f->f2 = (serialize_int32_t) ( ( rng >> 24 ) & 0xFFFFF ) - 500000;
    f->f3 = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 2 ) & 3 );
    f->f4 = (serialize_int32_t) ( ( rng >> 11 ) & 15 ) - 8;
    f->f5 = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 22 ) & 511 );
    f->f6 = (serialize_int32_t) ( ( rng >> 33 ) & 2047 ) - 1024;
    f->f7 = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 40 ) & 255 );
    f->f8 = (serialize_int32_t) ( ( rng >> 30 ) & 0xFFFFF ) - 500000;
    f->f9 = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 57 ) & 63 );
}

static void vary_rt_bits( RtBenchBits * f, uint64_t rng )
{
    f->b7 = (serialize_uint32_t) rng & 127;
    f->b13 = (serialize_uint32_t) ( rng >> 3 ) & 8191;
    f->b23 = (serialize_uint32_t) ( rng >> 5 ) & 8388607;
    f->b3 = (serialize_uint32_t) ( rng >> 29 ) & 7;
    f->b32 = (serialize_uint32_t) ( rng >> 16 );
    f->b11 = (serialize_uint32_t) ( rng >> 37 ) & 2047;
    f->b19 = (serialize_uint32_t) ( rng >> 44 ) & 524287;
    f->b48 = rng & 0xFFFFFFFFFFFFULL;
}

static void vary_rt_mixed( RtBenchMixed * f, uint64_t rng )
{
    f->sequence = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 8 ) & 65535 );
    f->ack_bits = (serialize_uint32_t) ( rng >> 16 );
    f->entity_id = (serialize_uint32_t) rng & 4095;
    f->pos_x = (serialize_int32_t) ( ( rng >> 20 ) & 32767 ) - 16384;
    f->pos_y = (serialize_int32_t) ( ( rng >> 25 ) & 32767 ) - 16384;
    f->pos_z = (serialize_int32_t) ( ( rng >> 30 ) & 32767 ) - 16384;
    f->yaw = (serialize_uint32_t) ( rng >> 3 ) & 511;
    f->moving = ( rng & 1 ) != 0;
    f->firing = ( rng & 2 ) != 0;
    f->timestamp = rng & 0xFFFFFFFFFFFFULL;
    f->weapon = (serialize_int32_t) ( (serialize_uint32_t) ( rng >> 60 ) & 15 );
}

/* ---- the rt driver: once-helpers carry the single untimed call site per op
   (§3.2), the noinline loops carry the timed one, and the §1.5 oracle gate
   runs before any number ---- */

#define BENCH_RT( SUFFIX, TYPE, WRITE, READ, VARY )                                             \
static int rt_once_write_##SUFFIX( TYPE * msg, uint8_t * buffer )                               \
{                                                                                               \
    serialize_write_stream_t stream;                                                            \
    serialize_write_stream_init( &stream, buffer, BufferSize );                                 \
    if ( !WRITE( &stream, msg ) )                                                               \
        return -1;                                                                              \
    serialize_write_flush( &stream );                                                           \
    return serialize_write_bytes_processed( &stream );                                          \
}                                                                                               \
static int rt_once_read_##SUFFIX( TYPE * out, const uint8_t * buffer, int bytes )               \
{                                                                                               \
    serialize_read_stream_t stream;                                                             \
    serialize_read_stream_init( &stream, buffer, bytes );                                       \
    return READ( &stream, out );                                                                \
}                                                                                               \
BENCH_NOINLINE static int rt_##SUFFIX##_write_loop( TYPE * base, long iters, uint64_t * rng )   \
{                                                                                               \
    long i;                                                                                     \
    for ( i = 0; i < iters; i++ )                                                               \
    {                                                                                           \
        serialize_write_stream_t stream;                                                        \
        *rng = bench_rng( *rng );                                                               \
        VARY( base, *rng );                                                                     \
        serialize_write_stream_init( &stream, g_buffer, BufferSize );                           \
        if ( !WRITE( &stream, base ) )                                                          \
            return 0;                                                                           \
        serialize_write_flush( &stream );                                                       \
        bench_escape( g_buffer );                                                               \
        g_sink = g_sink + (uint64_t) serialize_write_bytes_processed( &stream );                \
    }                                                                                           \
    return 1;                                                                                   \
}                                                                                               \
BENCH_NOINLINE static int rt_##SUFFIX##_read_loop( TYPE * out, long iters, int bytes_per_op )   \
{                                                                                               \
    long i;                                                                                     \
    for ( i = 0; i < iters; i++ )                                                               \
    {                                                                                           \
        serialize_read_stream_t stream;                                                         \
        serialize_read_stream_init( &stream, g_variant( i & ( NumVariants - 1 ) ), bytes_per_op ); \
        if ( !READ( &stream, out ) )                                                            \
            return 0;                                                                           \
        bench_escape( out );                                                                    \
        g_sink = g_sink + 1;                                                                    \
    }                                                                                           \
    return 1;                                                                                   \
}                                                                                               \
static void bench_rt_##SUFFIX( const char * name, long base_iters, const TYPE * pinned )        \
{                                                                                               \
    const long iters = base_iters / IterScale;                                                  \
    TYPE base = *pinned;                                                                        \
    TYPE out;                                                                                   \
    int bytes_per_op;                                                                           \
    uint64_t rng;                                                                               \
    double write_rates[MaxNumRuns];                                                             \
    double read_rates[MaxNumRuns];                                                              \
    int run, k;                                                                                 \
                                                                                                \
    /* oracle 1: the hand-written wire must equal the generated-code golden */                  \
    bytes_per_op = rt_once_write_##SUFFIX( &base, g_buffer );                                   \
    if ( bytes_per_op < 0 )                                                                     \
    {                                                                                           \
        fail( name, "write of pinned instance failed" );                                        \
        return;                                                                                 \
    }                                                                                           \
    if ( !check_golden( name, g_buffer, bytes_per_op ) )                                        \
    {                                                                                           \
        failed = 1;                                                                             \
        return;                                                                                 \
    }                                                                                           \
                                                                                                \
    /* oracle 2: round-trip write -> read -> re-write -> identical bytes */                     \
    memset( &out, 0, sizeof( out ) );                                                           \
    if ( !rt_once_read_##SUFFIX( &out, g_buffer, bytes_per_op ) )                               \
    {                                                                                           \
        fail( name, "read of pinned instance failed" );                                         \
        return;                                                                                 \
    }                                                                                           \
    if ( rt_once_write_##SUFFIX( &out, g_twin ) != bytes_per_op ||                              \
         memcmp( g_buffer, g_twin, (size_t) bytes_per_op ) != 0 )                               \
    {                                                                                           \
        fail( name, "round-trip bytes differ" );                                                \
        return;                                                                                 \
    }                                                                                           \
                                                                                                \
    /* variant buffers (and proof that variation keeps bytes/op constant) */                    \
    rng = 1;                                                                                    \
    for ( k = 0; k < NumVariants; k++ )                                                         \
    {                                                                                           \
        rng = bench_rng( rng );                                                                 \
        VARY( &base, rng );                                                                     \
        if ( rt_once_write_##SUFFIX( &base, g_variant( k ) ) != bytes_per_op )                  \
        {                                                                                       \
            fail( name, "variation changed bytes/op — vary must keep structure fields fixed" ); \
            return;                                                                             \
        }                                                                                       \
    }                                                                                           \
                                                                                                \
    for ( run = -1; run < g_num_runs; run++ )                                                   \
    {                                                                                           \
        double start = time_now();                                                              \
        double elapsed;                                                                         \
        if ( !rt_##SUFFIX##_write_loop( &base, iters, &rng ) )                                  \
        {                                                                                       \
            fail( name, "write failed in loop" );                                               \
            return;                                                                             \
        }                                                                                       \
        elapsed = time_now() - start;                                                           \
        if ( run >= 0 )                                                                         \
            write_rates[run] = (double) iters / elapsed;                                        \
    }                                                                                           \
                                                                                                \
    for ( run = -1; run < g_num_runs; run++ )                                                   \
    {                                                                                           \
        double start = time_now();                                                              \
        double elapsed;                                                                         \
        if ( !rt_##SUFFIX##_read_loop( &out, iters, bytes_per_op ) )                            \
        {                                                                                       \
            fail( name, "read failed in loop" );                                                \
            return;                                                                             \
        }                                                                                       \
        elapsed = time_now() - start;                                                           \
        if ( run >= 0 )                                                                         \
            read_rates[run] = (double) iters / elapsed;                                         \
    }                                                                                           \
                                                                                                \
    report( name, "write", iters, bytes_per_op, run_stats( write_rates, g_num_runs ), "rt" );   \
    report( name, "read", iters, bytes_per_op, run_stats( read_rates, g_num_runs ), "rt" );     \
}

BENCH_RT( bench_packet, RtBenchPacket, rt_write_bench_packet, rt_read_bench_packet, vary_rt_packet )
BENCH_RT( bench_ints, RtBenchInts, rt_write_bench_ints, rt_read_bench_ints, vary_rt_ints )
BENCH_RT( bench_bits, RtBenchBits, rt_write_bench_bits, rt_read_bench_bits, vary_rt_bits )
BENCH_RT( bench_mixed, RtBenchMixed, rt_write_bench_mixed, rt_read_bench_mixed, vary_rt_mixed )

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

/* the message_stream golden: dispatch wire self-check (not a benchmark) */
static void check_message_stream_golden( void )
{
    serialize_write_stream_t ws;
    Message m;

    serialize_write_stream_init( &ws, g_buffer, BufferSize );

    memset( &m, 0, sizeof( m ) );
    m.type = MESSAGE_TYPE_CHAT;
    memcpy( m.as.chat.text, "dispatch", 8 );
    m.as.chat.text_length = 8;
    if ( !write_message( &ws, &m ) )
    {
        fail( "message_stream", "dispatch write failed" );
        return;
    }

    memset( &m, 0, sizeof( m ) );
    m.type = MESSAGE_TYPE_TEST;
    m.as.test.test_b = 42;
    if ( !write_message( &ws, &m ) )
    {
        fail( "message_stream", "dispatch write failed" );
        return;
    }

    memset( &m, 0, sizeof( m ) );
    m.type = MESSAGE_TYPE_NONE;
    if ( !write_message( &ws, &m ) )
    {
        fail( "message_stream", "dispatch write failed" );
        return;
    }

    serialize_write_flush( &ws );
    if ( !check_golden( "message_stream", g_buffer, serialize_write_bytes_processed( &ws ) ) )
        failed = 1;
}

int main( int argc, char ** argv )
{
    int i;
    RigidBody moving, at_rest;
    Chat chat;
    Test test;
    InputPacket inputpacket;
    ShipCreate shipcreate;
    ShipData_Shallow ship_shallow;
    ProbeHeader probe_header;
    ProbeBits probebits;
    ProbeArray probearray;
    TestData testdata;

    for ( i = 1; i < argc; i++ )
    {
        if ( strcmp( argv[i], "--csv" ) == 0 )
            g_csv = 1;
        else if ( strcmp( argv[i], "--wire-dir" ) == 0 && i + 1 < argc )
            g_wire_dir = argv[++i];
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
        else
        {
            fprintf( stderr, "usage: %s [--csv] [--round K] [--wire-dir <dir>]\n", argv[0] );
            return 1;
        }
    }

#if defined(NDEBUG)
    fprintf( stderr, "schema bench (c, Release)\n" );
#else
    fprintf( stderr, "schema bench (c, Debug — only release numbers are meaningful)\n" );
#endif

    if ( g_csv )
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n" );

    check_message_stream_golden();

    moving = pin_rigidbody_moving();
    at_rest = moving;
    at_rest.at_rest = 1;
    chat = pin_chat();
    memset( &test, 0, sizeof( test ) );
    inputpacket = pin_inputpacket();
    shipcreate = pin_shipcreate();
    ship_shallow = pin_ship_shallow();
    probe_header = pin_probe_header();
    probebits = pin_probebits();
    probearray = pin_probearray();
    testdata = pin_testdata();

    bench_message_rigidbody( "rigidbody_moving", "rigidbody_moving", 24000000L, &moving );
    bench_message_rigidbody_at_rest( "rigidbody_at_rest", "rigidbody_at_rest", 32000000L, &at_rest );
    bench_message_chat( "chat", "chat", 48000000L, &chat );
    bench_message_test( "test", NULL, 192000000L, &test );
    bench_message_inputpacket( "inputpacket", "inputpacket", 16000000L, &inputpacket );
    bench_message_shipcreate( "shipcreate", "shipcreate_flags", 32000000L, &shipcreate );
    bench_message_ship_shallow( "ship_shallow", "ship_shallow", 32000000L, &ship_shallow );
    bench_message_probe_header( "probe_header", "probe_header", 256000000L, &probe_header );
    bench_message_probebits( "probebits", "probebits", 128000000L, &probebits );
    bench_message_probearray( "probearray", "probearray", 20000000L, &probearray );
    bench_message_testdata( "testdata", "testdata", 8000000L, &testdata );

    bench_batch();

    /* family rt (§1.3/§1.5): the runtime API by hand, oracle-gated against
       the goldens the generated code pinned. Iteration counts are fixed and
       identical across all five runners (§2.1; sized in the C++ reference). */
    {
        RtBenchPacket rt_packet;
        RtBenchInts rt_ints;
        RtBenchBits rt_bits;
        RtBenchMixed rt_mixed;

        memset( &rt_packet, 0, sizeof( rt_packet ) );
        rt_packet.a = -37; rt_packet.b = 12345; rt_packet.c = 987654;
        rt_packet.bits7 = 97; rt_packet.bits13 = 5000; rt_packet.bits23 = 1234567;
        rt_packet.flag = 1;
        rt_packet.x = 1.5f; rt_packet.y = -3.25f; rt_packet.z = 100.125f;
        rt_packet.big = 0x123456789ABCDEF0ULL;
        for ( i = 0; i < 17; i++ )
            rt_packet.blob[i] = (serialize_uint8_t) ( i * 31 );

        memset( &rt_ints, 0, sizeof( rt_ints ) );
        rt_ints.f0 = -37; rt_ints.f1 = 12345; rt_ints.f2 = 987654; rt_ints.f3 = 2; rt_ints.f4 = -15;
        rt_ints.f5 = 777; rt_ints.f6 = -2048; rt_ints.f7 = 200; rt_ints.f8 = -543210; rt_ints.f9 = 99;

        memset( &rt_bits, 0, sizeof( rt_bits ) );
        rt_bits.b7 = 97; rt_bits.b13 = 5000; rt_bits.b23 = 1234567; rt_bits.b3 = 5;
        rt_bits.b32 = 0xDEADBEEFu; rt_bits.b11 = 1024; rt_bits.b19 = 333333;
        rt_bits.b48 = 0xFEDCBA987654ULL;

        memset( &rt_mixed, 0, sizeof( rt_mixed ) );
        rt_mixed.sequence = 52428; rt_mixed.ack_bits = 0xA5A5A5A5u; rt_mixed.entity_id = 2049;
        rt_mixed.pos_x = -16384; rt_mixed.pos_y = 16383; rt_mixed.pos_z = -1;
        rt_mixed.yaw = 511; rt_mixed.moving = 1; rt_mixed.firing = 0;
        rt_mixed.timestamp = 0x123456789ABCULL; rt_mixed.weapon = 15;

        bench_rt_bench_packet( "bench_packet", 32000000L, &rt_packet );
        bench_rt_bench_ints( "bench_ints", 40000000L, &rt_ints );
        bench_rt_bench_bits( "bench_bits", 48000000L, &rt_bits );
        bench_rt_bench_mixed( "bench_mixed", 40000000L, &rt_mixed );
    }

    /* family bits (§1.4): the one bitpacker workload in the estate */
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
