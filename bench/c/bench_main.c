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

#define NumRuns 7           /* median of 7 (N >= 5), after 1 warmup run */
#define NumVariants 64      /* read-path variant buffers */

#if defined(NDEBUG)
#define IterScale 1         /* Release: full fixed counts */
#else
#define IterScale 8         /* Debug: fixed counts / 8 (recorded in the iters column) */
#endif

static int g_csv = 0;
static const char * g_wire_dir = "testdata/wire";

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

static int failed = 0;

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

static void report( const char * bench, const char * path, long iters, int bytes_per_op, RunStats s )
{
    const double mbps = s.median_rate * (double) bytes_per_op / ( 1024.0 * 1024.0 );
    fprintf( stderr, "%-18s %-5s %10.2f M msg/s %10.1f MB/s   (min %.2f, max %.2f, spread %.1f%%)\n",
             bench, path, s.median_rate / 1e6, mbps, s.min_rate / 1e6, s.max_rate / 1e6, s.spread_pct );
    if ( g_csv )
    {
        printf( "c,%s,%s,%ld,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f\n",
                bench, path, iters, bytes_per_op, NumRuns,
                s.median_rate, s.min_rate, s.max_rate, mbps, s.spread_pct );
    }
}

/* ------------------------------------------------------------------------------------------
   the per-message benchmark driver — one expansion per message type (C's
   answer to the reference's function template). TYPE is the generated struct,
   WRITE/READ the generated functions, VARY the per-iteration mutation.
   ------------------------------------------------------------------------------------------ */

#define BENCH_MESSAGE( SUFFIX, TYPE, WRITE, READ, VARY )                                        \
static void bench_message_##SUFFIX( const char * name, const char * golden, long base_iters,    \
                                    const TYPE * pinned )                                       \
{                                                                                               \
    const long iters = base_iters / IterScale;                                                  \
    TYPE base = *pinned;                                                                        \
    TYPE out;                                                                                   \
    serialize_write_stream_t ws;                                                                \
    int bytes_per_op;                                                                           \
    uint64_t rng;                                                                               \
    double write_rates[NumRuns];                                                                \
    double read_rates[NumRuns];                                                                 \
    int run, k;                                                                                 \
    long i;                                                                                     \
                                                                                                \
    /* self-check 1: the pinned instance matches its wire golden byte-for-byte */               \
    serialize_write_stream_init( &ws, g_buffer, BufferSize );                                   \
    if ( !WRITE( &ws, &base ) )                                                                 \
    {                                                                                           \
        fail( name, "write of pinned instance failed" );                                        \
        return;                                                                                 \
    }                                                                                           \
    serialize_write_flush( &ws );                                                               \
    bytes_per_op = serialize_write_bytes_processed( &ws );                                      \
    if ( golden && !check_golden( golden, g_buffer, bytes_per_op ) )                            \
    {                                                                                           \
        failed = 1;                                                                             \
        return;                                                                                 \
    }                                                                                           \
                                                                                                \
    /* self-check 2: round-trip write -> read -> re-write -> identical bytes */                 \
    {                                                                                           \
        serialize_read_stream_t rs;                                                             \
        serialize_write_stream_t tws;                                                           \
        memset( &out, 0, sizeof( out ) );                                                       \
        serialize_read_stream_init( &rs, g_buffer, bytes_per_op );                              \
        if ( !READ( &rs, &out ) )                                                               \
        {                                                                                       \
            fail( name, "read of pinned instance failed" );                                     \
            return;                                                                             \
        }                                                                                       \
        serialize_write_stream_init( &tws, g_twin, BufferSize );                                \
        if ( !WRITE( &tws, &out ) )                                                             \
        {                                                                                       \
            fail( name, "re-write of decoded instance failed" );                                \
            return;                                                                             \
        }                                                                                       \
        serialize_write_flush( &tws );                                                          \
        if ( serialize_write_bytes_processed( &tws ) != bytes_per_op ||                         \
             memcmp( g_buffer, g_twin, (size_t) bytes_per_op ) != 0 )                           \
        {                                                                                       \
            fail( name, "round-trip bytes differ" );                                            \
            return;                                                                             \
        }                                                                                       \
    }                                                                                           \
                                                                                                \
    /* variant buffers for the read path (and proof that variation keeps bytes/op constant) */  \
    rng = 1;                                                                                    \
    for ( k = 0; k < NumVariants; k++ )                                                         \
    {                                                                                           \
        serialize_write_stream_t vs;                                                            \
        rng = bench_rng( rng );                                                                 \
        VARY( &base, rng );                                                                     \
        serialize_write_stream_init( &vs, g_variant( k ), BufferSize );                         \
        if ( !WRITE( &vs, &base ) )                                                             \
        {                                                                                       \
            fail( name, "write of varied instance failed" );                                    \
            return;                                                                             \
        }                                                                                       \
        serialize_write_flush( &vs );                                                           \
        if ( serialize_write_bytes_processed( &vs ) != bytes_per_op )                           \
        {                                                                                       \
            fail( name, "variation changed bytes/op — vary must keep structure fields fixed" ); \
            return;                                                                             \
        }                                                                                       \
    }                                                                                           \
                                                                                                \
    /* write path: 1 warmup + NumRuns measured */                                               \
    for ( run = -1; run < NumRuns; run++ )                                                      \
    {                                                                                           \
        double start = time_now();                                                              \
        double elapsed;                                                                         \
        for ( i = 0; i < iters; i++ )                                                           \
        {                                                                                       \
            serialize_write_stream_t stream;                                                    \
            rng = bench_rng( rng );                                                             \
            VARY( &base, rng );                                                                 \
            serialize_write_stream_init( &stream, g_buffer, BufferSize );                       \
            if ( !WRITE( &stream, &base ) )                                                     \
            {                                                                                   \
                fail( name, "write failed in loop" );                                           \
                return;                                                                         \
            }                                                                                   \
            serialize_write_flush( &stream );                                                   \
            bench_escape( g_buffer );                                                           \
            g_sink = g_sink + (uint64_t) serialize_write_bytes_processed( &stream );            \
        }                                                                                       \
        elapsed = time_now() - start;                                                           \
        if ( run >= 0 )                                                                         \
            write_rates[run] = (double) iters / elapsed;                                        \
    }                                                                                           \
                                                                                                \
    /* read path: 1 warmup + NumRuns measured; ONE decode instance hoisted out                  \
       of the loop and reused, matching the reference (a fresh instance per                     \
       iteration is constructed+zeroed harness overhead, not serialize work) */                 \
    for ( run = -1; run < NumRuns; run++ )                                                      \
    {                                                                                           \
        double start = time_now();                                                              \
        double elapsed;                                                                         \
        for ( i = 0; i < iters; i++ )                                                           \
        {                                                                                       \
            serialize_read_stream_t stream;                                                     \
            serialize_read_stream_init( &stream, g_variant( i & ( NumVariants - 1 ) ), bytes_per_op ); \
            if ( !READ( &stream, &out ) )                                                       \
            {                                                                                   \
                fail( name, "read failed in loop" );                                            \
                return;                                                                         \
            }                                                                                   \
            bench_escape( &out );   /* every decoded field is observed */                       \
            g_sink = g_sink + 1;                                                                \
        }                                                                                       \
        elapsed = time_now() - start;                                                           \
        if ( run >= 0 )                                                                         \
            read_rates[run] = (double) iters / elapsed;                                         \
    }                                                                                           \
                                                                                                \
    report( name, "write", iters, bytes_per_op, run_stats( write_rates, NumRuns ) );            \
    report( name, "read", iters, bytes_per_op, run_stats( read_rates, NumRuns ) );              \
}

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

BENCH_MESSAGE( rigidbody, RigidBody, write_rigid_body, read_rigid_body, vary_rigidbody )
BENCH_MESSAGE( rigidbody_at_rest, RigidBody, write_rigid_body, read_rigid_body, vary_rigidbody_at_rest )
BENCH_MESSAGE( chat, Chat, write_chat, read_chat, vary_chat )
BENCH_MESSAGE( test, Test, write_test, read_test, vary_test )
BENCH_MESSAGE( inputpacket, InputPacket, write_input_packet, read_input_packet, vary_inputpacket )
BENCH_MESSAGE( shipcreate, ShipCreate, write_ship_create, read_ship_create, vary_shipcreate )
BENCH_MESSAGE( ship_shallow, ShipData_Shallow, write_ship_shallow, read_ship_shallow, vary_ship_shallow )
BENCH_MESSAGE( probe_header, ProbeHeader, write_probe_header, read_probe_header, vary_probe_header )
BENCH_MESSAGE( probebits, ProbeBits, write_probe_bits, read_probe_bits, vary_probebits )
BENCH_MESSAGE( probearray, ProbeArray, write_probe_array, read_probe_array, vary_probearray )
BENCH_MESSAGE( testdata, TestData, write_test_data, read_test_data, vary_testdata )

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
#define BatchPasses 800

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
    double write_rates[NumRuns];
    double read_rates[NumRuns];
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
    for ( run = -1; run < NumRuns; run++ )
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
    for ( run = -1; run < NumRuns; run++ )
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

    report( "message_batch", "write", total_msgs, batch_bytes / NumBatchMessages, run_stats( write_rates, NumRuns ) );
    report( "message_batch", "read", total_msgs, batch_bytes / NumBatchMessages, run_stats( read_rates, NumRuns ) );

    free( batch_buffer );
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
        else
        {
            fprintf( stderr, "usage: %s [--csv] [--wire-dir <dir>]\n", argv[0] );
            return 1;
        }
    }

#if defined(NDEBUG)
    fprintf( stderr, "schema bench (c, Release)\n" );
#else
    fprintf( stderr, "schema bench (c, Debug — only release numbers are meaningful)\n" );
#endif

    if ( g_csv )
        printf( "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct\n" );

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

    bench_message_rigidbody( "rigidbody_moving", "rigidbody_moving", 2000000L, &moving );
    bench_message_rigidbody_at_rest( "rigidbody_at_rest", "rigidbody_at_rest", 4000000L, &at_rest );
    bench_message_chat( "chat", "chat", 4000000L, &chat );
    bench_message_test( "test", NULL, 16000000L, &test );
    bench_message_inputpacket( "inputpacket", "inputpacket", 2000000L, &inputpacket );
    bench_message_shipcreate( "shipcreate", "shipcreate_flags", 4000000L, &shipcreate );
    bench_message_ship_shallow( "ship_shallow", "ship_shallow", 4000000L, &ship_shallow );
    bench_message_probe_header( "probe_header", "probe_header", 16000000L, &probe_header );
    bench_message_probebits( "probebits", "probebits", 4000000L, &probebits );
    bench_message_probearray( "probearray", "probearray", 2000000L, &probearray );
    bench_message_testdata( "testdata", "testdata", 1000000L, &testdata );

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
