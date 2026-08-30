// harness-contract decomposition instrument (issue #170 forensics, lightweight).
// One shape (bench_packet, 49 B), TWO subjects x TWO harness contracts:
//   subject gen = generated C++ codec (generated/bench/cpp/BenchWire.h)
//   subject rt  = hand-written serialize runtime API (bench/cpp/bench_main.cpp verbatim)
//   contract A  = BENCH-STANDARD style: 1 warmup + 7 measured runs, 32M iters,
//                 LCG vary per iter, 64 variant read buffers at stride 4160,
//                 asm escape barriers, noinline loop symbols, median-of-7
//   contract B  = serialize-family style: 5 trials best-of, 1M iters,
//                 tight loop, sink += consumption, 64 packed 256 B variants
// Prints every raw run rate so both statistics are derivable from one output.

#include <cstdio>
#include <cstring>
#include <cstdint>
#include <ctime>
#include <algorithm>

#include "serialize.h"
#include "Bench.h"
#include "BenchWire.h"

#if defined(_MSC_VER)
#define BENCH_NOINLINE __declspec( noinline )
#else
#define BENCH_NOINLINE __attribute__(( noinline ))
#endif

static double time_now()
{
    struct timespec ts;
    clock_gettime( CLOCK_MONOTONIC, &ts );
    return double( ts.tv_sec ) + double( ts.tv_nsec ) * 1e-9;
}

static inline uint64_t bench_rng( uint64_t rng )
{
    return rng * 0x5851F42D4C957F2Dull + 0x14057B7EF767814Full;
}

static inline void bench_escape( void * p )
{
    __asm__ __volatile__( "" : : "g"( p ) : "memory" );
}

static uint64_t g_sink = 0;

// ---- subject rt: hand-written runtime API packet (bench_main.cpp verbatim) ----

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
        serialize_bytes( stream, blob, (int) sizeof( blob ) );
        return true;
    }
};

template <typename T> static void pin_packet( T & in )
{
    in.a = -37; in.b = 12345; in.c = 987654;
    in.bits7 = 97; in.bits13 = 5000; in.bits23 = 1234567;
    in.flag = true;
    in.x = 1.5f; in.y = -3.25f; in.z = 100.125f;
    in.big = 0x123456789ABCDEF0ull;
    for ( int i = 0; i < 17; i++ )
        in.blob[i] = (uint8_t) ( i * 31 );
}

template <typename T> static void vary_packet( T & p, uint64_t rng )
{
    p.a = int32_t( ( rng >> 8 ) & 63 ) - 32;
    p.b = int32_t( uint32_t( rng >> 16 ) & 65535 );
    p.c = int32_t( uint32_t( rng >> 24 ) & 0xFFFFF ) - 500000;
    p.bits7 = uint32_t( rng ) & 127;
    p.bits13 = uint32_t( rng >> 3 ) & 8191;
    p.bits23 = uint32_t( rng >> 5 ) & 8388607;
    p.flag = ( rng & 1 ) != 0;
    p.x = float( rng & 0xFFFF );
    p.big = rng;
    p.blob[0] = (uint8_t) ( ( rng >> 32 ) & 0xFF );
}

// ---- write/read once per subject ----

static int64_t gen_write_once( bench::BenchPacket & msg, uint8_t * buffer, int buffer_size )
{
    serialize::WriteStream stream( buffer, buffer_size );
    if ( !bench::WriteBenchPacket( stream, msg ) )
        return -1;
    stream.Flush();
    return stream.GetBytesProcessed();
}

static bool gen_read_once( bench::BenchPacket & out, const uint8_t * buffer, int64_t bytes )
{
    serialize::ReadStream stream( buffer, (int) bytes );
    return bench::ReadBenchPacket( stream, out );
}

static int64_t rt_write_once( RtBenchPacket & msg, uint8_t * buffer, int buffer_size )
{
    serialize::WriteStream stream( buffer, buffer_size );
    if ( !msg.Serialize( stream ) )
        return -1;
    stream.Flush();
    return stream.GetBytesProcessed();
}

static bool rt_read_once( RtBenchPacket & out, const uint8_t * buffer, int64_t bytes )
{
    serialize::ReadStream stream( buffer, (int) bytes );
    return out.Serialize( stream );
}

// ---- contract A buffers: 4096 B slots at stride 4160 (BENCH-STANDARD §2.7) ----

const int NumVariants = 64;
const int BufferSize = 4096;
const int VariantStride = BufferSize + 64;
alignas( 8 ) static uint8_t g_buffer[BufferSize];
alignas( 8 ) static uint8_t g_variants_a[NumVariants][VariantStride];

// ---- contract B buffers: packed 256 B (the family runners' shape) ----
alignas( 8 ) static uint8_t g_variants_b[NumVariants][256];
alignas( 8 ) static uint8_t g_buffer_b[256];

// ---- contract A timed loops: noinline symbols, escape barriers ----

template <typename T, typename WriteOnce>
BENCH_NOINLINE double a_write_run( T & base, long iters, uint64_t & rng, WriteOnce write_once )
{
    double start = time_now();
    for ( long i = 0; i < iters; i++ )
    {
        rng = bench_rng( rng );
        vary_packet( base, rng );
        int64_t n = write_once( base, g_buffer, BufferSize );
        if ( n < 0 ) { fprintf( stderr, "write failed\n" ); exit( 1 ); }
        bench_escape( g_buffer );
        g_sink += (uint64_t) n;
    }
    return time_now() - start;
}

template <typename T, typename ReadOnce>
BENCH_NOINLINE double a_read_run( T & out, long iters, int64_t bytes_per_op, ReadOnce read_once )
{
    double start = time_now();
    for ( long i = 0; i < iters; i++ )
    {
        if ( !read_once( out, g_variants_a[i & ( NumVariants - 1 )], bytes_per_op ) )
        { fprintf( stderr, "read failed\n" ); exit( 1 ); }
        bench_escape( &out );
        g_sink += 1;
    }
    return time_now() - start;
}

// ---- contract B timed loops: tight, family-style (sink += result) ----

template <typename T, typename WriteOnce>
double b_write_trial( T & base, long iters, uint64_t & rng, WriteOnce write_once )
{
    double start = time_now();
    for ( long i = 0; i < iters; i++ )
    {
        rng = bench_rng( rng );
        vary_packet( base, rng );
        g_sink += (uint64_t) write_once( base, g_buffer_b, 256 );
    }
    return time_now() - start;
}

template <typename T, typename ReadOnce>
double b_read_trial( T & out, long iters, int64_t bytes_per_op, ReadOnce read_once )
{
    double start = time_now();
    for ( long i = 0; i < iters; i++ )
    {
        if ( !read_once( out, g_variants_b[i & ( NumVariants - 1 )], bytes_per_op ) )
        { fprintf( stderr, "read failed\n" ); exit( 1 ); }
        g_sink += (uint64_t) out.b;
    }
    return time_now() - start;
}

template <typename T, typename WriteOnce, typename ReadOnce>
static void run_subject( const char * subject, WriteOnce write_once, ReadOnce read_once,
                         long a_iters, long b_iters )
{
    // setup + oracle: pinned instance, then 64 variants in BOTH buffer layouts
    T base;
    pin_packet( base );
    int64_t bytes_per_op = write_once( base, g_buffer, BufferSize );
    printf( "%s: bytes_per_op %lld\n", subject, (long long) bytes_per_op );
    uint64_t rng = 1;
    for ( int k = 0; k < NumVariants; k++ )
    {
        rng = bench_rng( rng );
        vary_packet( base, rng );
        if ( write_once( base, g_variants_a[k], BufferSize ) != bytes_per_op ) { fprintf( stderr, "variant size\n" ); exit( 1 ); }
        memcpy( g_variants_b[k], g_variants_a[k], 256 );
    }

    // contract A: 1 warmup + 7 measured, median + all raw rates
    {
        double wr[7], rr[7];
        uint64_t arng = rng;
        T out;
        for ( int run = -1; run < 7; run++ )
        {
            double t = a_write_run( base, a_iters, arng, write_once );
            if ( run >= 0 ) wr[run] = double( a_iters ) / t;
        }
        for ( int run = -1; run < 7; run++ )
        {
            double t = a_read_run( out, a_iters, bytes_per_op, read_once );
            if ( run >= 0 ) rr[run] = double( a_iters ) / t;
        }
        printf( "%s A(std,32M,noinline,escape) write raw:", subject );
        for ( int i = 0; i < 7; i++ ) printf( " %.1f", wr[i] / 1e6 );
        std::sort( wr, wr + 7 );
        printf( "  median %.1f max %.1f M/s\n", wr[3] / 1e6, wr[6] / 1e6 );
        printf( "%s A(std,32M,noinline,escape) read  raw:", subject );
        for ( int i = 0; i < 7; i++ ) printf( " %.1f", rr[i] / 1e6 );
        std::sort( rr, rr + 7 );
        printf( "  median %.1f max %.1f M/s\n", rr[3] / 1e6, rr[6] / 1e6 );
    }

    // contract B: 5 trials best-of, tight loops, family consumption
    {
        double bw = 1e30, br = 1e30;
        double wraw[5], rraw[5];
        uint64_t brng = 1;
        T out;
        for ( int trial = 0; trial < 5; trial++ )
        {
            double tw = b_write_trial( base, b_iters, brng, write_once );
            double tr = b_read_trial( out, b_iters, bytes_per_op, read_once );
            wraw[trial] = double( b_iters ) / tw;
            rraw[trial] = double( b_iters ) / tr;
            bw = std::min( bw, tw );
            br = std::min( br, tr );
        }
        printf( "%s B(family,%ldK,tight,sink) write raw:", subject, b_iters / 1000 );
        for ( int i = 0; i < 5; i++ ) printf( " %.1f", wraw[i] / 1e6 );
        printf( "  best %.1f M/s\n", double( b_iters ) / bw / 1e6 );
        printf( "%s B(family,%ldK,tight,sink) read  raw:", subject, b_iters / 1000 );
        for ( int i = 0; i < 5; i++ ) printf( " %.1f", rraw[i] / 1e6 );
        printf( "  best %.1f M/s\n", double( b_iters ) / br / 1e6 );
    }

    // cross: contract B statistic/loop-style at contract A's iteration count
    {
        double bw = 1e30, br = 1e30;
        uint64_t brng = 1;
        T out;
        for ( int trial = 0; trial < 5; trial++ )
        {
            double tw = b_write_trial( base, a_iters, brng, write_once );
            double tr = b_read_trial( out, a_iters, bytes_per_op, read_once );
            bw = std::min( bw, tw );
            br = std::min( br, tr );
        }
        printf( "%s B-at-32M(tight,sink,best5): write best %.1f M/s  read best %.1f M/s\n",
                subject, double( a_iters ) / bw / 1e6, double( a_iters ) / br / 1e6 );
    }
}

int main()
{
    run_subject<bench::BenchPacket>( "gen", gen_write_once, gen_read_once, 32000000L, 1000000L );
    run_subject<RtBenchPacket>( "rt ", rt_write_once, rt_read_once, 32000000L, 1000000L );
    fprintf( stderr, "sink %llu\n", (unsigned long long) g_sink );
    return 0;
}
