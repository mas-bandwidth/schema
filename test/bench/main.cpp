// The bench-corpus golden pinner (BENCH-STANDARD.md §1.5, §6 step 6).
//
// Writes the four pinned family `rt` instances through the schema-GENERATED
// C++ code (generated/bench/cpp) and byte-checks them against the wire
// goldens testdata/wire/bench_*.bin, then round-trips write -> read ->
// re-write -> memcmp. THE GOLDENS PRODUCED HERE ARE THE AUTHORITY for the
// family `rt` oracle gate in all five bench runners: hand-written runtime
// code must reproduce these bytes exactly or refuse to bench.
//
// SCHEMA_UPDATE_WIRE_GOLDENS=1 re-pins deliberately (make update-goldens);
// a break under an unchanged schema is stop-the-line (SPEC §3.1), never a
// quiet re-pin.
//
// The pinned values are transcribed from serialize/bench.cpp BenchPacket::
// Init() for BenchPacket; the other three shapes pin nonzero, in-range,
// boundary-flavored values chosen here. Every bench runner carries the same
// pins — the goldens are what keep the transcriptions honest.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "BenchWire.h"

#define check( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s (%s:%d)\n", #condition, __FILE__, __LINE__ ); \
            return 1;                                                         \
        }                                                                     \
    } while ( 0 )

static bool golden_wire( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/%s.bin", name );
    if ( std::getenv( "SCHEMA_UPDATE_WIRE_GOLDENS" ) )
    {
        FILE * f = fopen( path, "wb" );
        if ( !f )
        {
            printf( "cannot write %s\n", path );
            return false;
        }
        fwrite( data, 1, (size_t) bytes, f );
        fclose( f );
        return true;
    }
    FILE * f = fopen( path, "rb" );
    if ( !f )
    {
        printf( "missing wire golden %s (run: make update-goldens)\n", path );
        return false;
    }
    static uint8_t expected[4096];
    size_t n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int64_t) n != bytes || std::memcmp( expected, data, (size_t) bytes ) != 0 )
    {
        printf( "WIRE GOLDEN MISMATCH: %s (%lld golden vs %lld actual bytes) — stop-the-line (SPEC §3.1, §7.2 gate 7)\n",
                name, (long long) n, (long long) bytes );
        return false;
    }
    return true;
}

using namespace bench;

// ---- the pinned instances (transcribed into every bench runner) ----

static BenchPacket pin_bench_packet()
{
    BenchPacket in;      // serialize/bench.cpp BenchPacket::Init(), verbatim
    in.a = -37;
    in.b = 12345;
    in.c = 987654;
    in.bits7 = 97;
    in.bits13 = 5000;
    in.bits23 = 1234567;
    in.flag = true;
    in.x = 1.5f;
    in.y = -3.25f;
    in.z = 100.125f;
    in.big = 0x123456789ABCDEF0ull;
    for ( int i = 0; i < 17; i++ )
        in.blob[i] = (uint8_t) ( i * 31 );
    return in;
}

static BenchInts pin_bench_ints()
{
    BenchInts in;
    in.f0 = -37;
    in.f1 = 12345;
    in.f2 = 987654;
    in.f3 = 2;
    in.f4 = -15;
    in.f5 = 777;
    in.f6 = -2048;
    in.f7 = 200;
    in.f8 = -543210;
    in.f9 = 99;
    return in;
}

static BenchBits pin_bench_bits()
{
    BenchBits in;
    in.b7 = 97;
    in.b13 = 5000;
    in.b23 = 1234567;
    in.b3 = 5;
    in.b32 = 0xDEADBEEFu;
    in.b11 = 1024;
    in.b19 = 333333;
    in.b48 = 0xFEDCBA987654ull;
    return in;
}

static BenchMixed pin_bench_mixed()
{
    BenchMixed in;
    in.sequence = 52428;
    in.ack_bits = 0xA5A5A5A5u;
    in.entity_id = 2049;
    in.pos_x = -16384;
    in.pos_y = 16383;
    in.pos_z = -1;
    in.yaw = 511;
    in.moving = true;
    in.firing = false;
    in.timestamp = 0x123456789ABCull;
    in.weapon = 15;
    return in;
}

// write pinned -> golden check -> read -> re-write -> memcmp, and report the
// measured wire size so §1.3's byte table is checked against reality
template <typename T, typename WriteFn, typename ReadFn>
static int pin_shape( const char * name, int expected_bytes, const T & pinned, WriteFn write_fn, ReadFn read_fn )
{
    alignas( 8 ) static uint8_t buffer[256];
    alignas( 8 ) static uint8_t twin[256];
    memset( buffer, 0, sizeof( buffer ) );
    memset( twin, 0, sizeof( twin ) );

    T in = pinned;
    serialize::WriteStream ws( buffer, sizeof( buffer ) );
    check( write_fn( ws, in ) );
    ws.Flush();
    const int64_t bytes = ws.GetBytesProcessed();
    printf( "%-14s %3lld bytes on the wire (BENCH-STANDARD.md §1.3 says %d)\n",
            name, (long long) bytes, expected_bytes );
    check( bytes == expected_bytes );   // §1.3's table vs the generated code — the goldens win
    check( golden_wire( name, buffer, bytes ) );

    T out;
    serialize::ReadStream rs( buffer, (int) bytes );
    check( read_fn( rs, out ) );
    serialize::WriteStream tws( twin, sizeof( twin ) );
    check( write_fn( tws, out ) );
    tws.Flush();
    check( tws.GetBytesProcessed() == bytes );
    check( memcmp( buffer, twin, (size_t) bytes ) == 0 );
    return 0;
}

int main()
{
    static_assert( BenchPacketMaxBits == 392, "§1.3: BenchPacket is 392 bits" );
    static_assert( BenchIntsMaxBits == 110, "§1.3: BenchInts is 110 bits" );
    static_assert( BenchBitsMaxBits == 156, "§1.3: BenchBits is 156 bits" );
    static_assert( BenchMixedMaxBits == 168, "§1.3: BenchMixed is 168 bits" );

    if ( pin_shape( "bench_packet", 49, pin_bench_packet(), WriteBenchPacket, ReadBenchPacket ) ) return 1;
    if ( pin_shape( "bench_ints", 14, pin_bench_ints(), WriteBenchInts, ReadBenchInts ) ) return 1;
    if ( pin_shape( "bench_bits", 20, pin_bench_bits(), WriteBenchBits, ReadBenchBits ) ) return 1;
    if ( pin_shape( "bench_mixed", 21, pin_bench_mixed(), WriteBenchMixed, ReadBenchMixed ) ) return 1;

    printf( "OK\n" );
    return 0;
}
