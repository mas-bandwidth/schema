/*
    The bench-corpus C verifier (BENCH-STANDARD.md §6 step 6).

    Writes the four pinned family `rt` instances through the schema-GENERATED
    C code (generated/bench/c) and byte-checks them against the wire goldens
    testdata/wire/bench_*.bin that test/bench/main.cpp (the C++ producer)
    pinned, then round-trips write -> read -> re-write -> memcmp. This proves
    the generated C compiles under the repo's strict C99 flags AND that the C
    emitter produces the same bytes as the C++ emitter for the bench corpus.

    Pinned values: test/bench/main.cpp, transcribed exactly.
*/

#include <stdio.h>
#include <string.h>

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

static int golden_wire( const char * name, const serialize_uint8_t * data, int bytes )
{
    char path[256];
    static serialize_uint8_t expected[4096];
    FILE * f;
    size_t n;
    sprintf( path, "testdata/wire/%s.bin", name );
    f = fopen( path, "rb" );
    if ( !f )
    {
        printf( "missing wire golden %s (run: make update-goldens)\n", path );
        return 0;
    }
    n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int) n != bytes || memcmp( expected, data, (size_t) bytes ) != 0 )
    {
        printf( "WIRE GOLDEN MISMATCH: %s (%d golden vs %d actual bytes) — the C emitter disagrees with the C++ producer, stop-the-line (SPEC §3.1)\n",
                name, (int) n, bytes );
        return 0;
    }
    return 1;
}

/* one expansion per shape: write pinned -> golden -> read -> re-write -> memcmp */
#define PIN_SHAPE( NAME, TYPE, WRITE, READ, EXPECTED_BYTES )                   \
    static int pin_##NAME( const TYPE * pinned )                               \
    {                                                                          \
        static serialize_uint64_t buffer_storage[32 + 1];  /* + 1 word = 8 bytes: read buffer allocations extend 8 bytes past the data */ \
        static serialize_uint64_t twin_storage[32];        /* write-only twin: no read slack needed */ \
        serialize_uint8_t * buffer = (serialize_uint8_t *) buffer_storage;     \
        serialize_uint8_t * twin = (serialize_uint8_t *) twin_storage;         \
        serialize_write_stream_t ws, tws;                                      \
        serialize_read_stream_t rs;                                            \
        TYPE out;                                                              \
        int bytes;                                                             \
        memset( buffer_storage, 0, sizeof( buffer_storage ) );                 \
        memset( twin_storage, 0, sizeof( twin_storage ) );                     \
        memset( &out, 0, sizeof( out ) );                                      \
        serialize_write_stream_init( &ws, buffer, sizeof( buffer_storage ) );  \
        check( WRITE( &ws, pinned ) );                                         \
        serialize_write_flush( &ws );                                          \
        bytes = serialize_write_bytes_processed( &ws );                        \
        printf( "%-14s %3d bytes on the wire (BENCH-STANDARD.md §1.3 says %d)\n", \
                #NAME, bytes, EXPECTED_BYTES );                                \
        check( bytes == EXPECTED_BYTES );                                      \
        check( golden_wire( #NAME, buffer, bytes ) );                          \
        serialize_read_stream_init( &rs, buffer, bytes );                      \
        check( READ( &rs, &out ) );                                            \
        serialize_write_stream_init( &tws, twin, sizeof( twin_storage ) );     \
        check( WRITE( &tws, &out ) );                                          \
        serialize_write_flush( &tws );                                         \
        check( serialize_write_bytes_processed( &tws ) == bytes );             \
        check( memcmp( buffer, twin, (size_t) bytes ) == 0 );                  \
        return 0;                                                              \
    }

PIN_SHAPE( bench_packet, BenchPacket, write_bench_packet, read_bench_packet, 49 )
PIN_SHAPE( bench_ints, BenchInts, write_bench_ints, read_bench_ints, 14 )
PIN_SHAPE( bench_bits, BenchBits, write_bench_bits, read_bench_bits, 20 )
PIN_SHAPE( bench_mixed, BenchMixed, write_bench_mixed, read_bench_mixed, 21 )

int main( void )
{
    int i;

    BenchPacket packet;
    BenchInts ints;
    BenchBits bits;
    BenchMixed mixed;

    memset( &packet, 0, sizeof( packet ) );
    packet.a = -37;
    packet.b = 12345;
    packet.c = 987654;
    packet.bits7 = 97;
    packet.bits13 = 5000;
    packet.bits23 = 1234567;
    packet.flag = 1;
    packet.x = 1.5f;
    packet.y = -3.25f;
    packet.z = 100.125f;
    packet.big = 0x123456789ABCDEF0ULL;
    for ( i = 0; i < 17; i++ )
    {
        packet.blob[i] = (serialize_uint8_t) ( i * 31 );
    }

    memset( &ints, 0, sizeof( ints ) );
    ints.f0 = -37;
    ints.f1 = 12345;
    ints.f2 = 987654;
    ints.f3 = 2;
    ints.f4 = -15;
    ints.f5 = 777;
    ints.f6 = -2048;
    ints.f7 = 200;
    ints.f8 = -543210;
    ints.f9 = 99;

    memset( &bits, 0, sizeof( bits ) );
    bits.b7 = 97;
    bits.b13 = 5000;
    bits.b23 = 1234567;
    bits.b3 = 5;
    bits.b32 = 0xDEADBEEFu;
    bits.b11 = 1024;
    bits.b19 = 333333;
    bits.b48 = 0xFEDCBA987654ULL;

    memset( &mixed, 0, sizeof( mixed ) );
    mixed.sequence = 52428;
    mixed.ack_bits = 0xA5A5A5A5u;
    mixed.entity_id = 2049;
    mixed.pos_x = -16384;
    mixed.pos_y = 16383;
    mixed.pos_z = -1;
    mixed.yaw = 511;
    mixed.moving = 1;
    mixed.firing = 0;
    mixed.timestamp = 0x123456789ABCULL;
    mixed.weapon = 15;

    if ( pin_bench_packet( &packet ) ) return 1;
    if ( pin_bench_ints( &ints ) ) return 1;
    if ( pin_bench_bits( &bits ) ) return 1;
    if ( pin_bench_mixed( &mixed ) ) return 1;

    printf( "OK\n" );
    return 0;
}
