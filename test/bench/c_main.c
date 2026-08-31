/*
    The bench-corpus C verifier (BENCH-STANDARD.md §6 step 6, §1.7).

    Writes the pinned bench-corpus instances through the schema-GENERATED
    C code (generated/bench/c) and byte-checks them against the wire goldens
    testdata/wire/{bench_*,real_packet}.bin that test/bench/main.cpp (the C++
    producer) pinned, then round-trips write -> read -> re-write -> memcmp.
    This proves the generated C compiles under the repo's strict C99 flags
    AND that the C emitter produces the same bytes as the C++ emitter for the
    bench corpus.

    Pinned values: test/bench/main.cpp, transcribed exactly. RealPacket's pin
    is the ALL-DEFAULTS instance — new_real_packet() serialized unmodified
    (the four branch gates carry their declared defaults; 1629 bits = 204
    bytes ride).
*/

#include <stdio.h>
#include <string.h>

#include "BenchWire.h"
#include "RealWorldWire.h"

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

/* one expansion per shape: write pinned -> golden -> read -> re-write ->
   memcmp; WHERE names the document claiming EXPECTED_BYTES */
#define PIN_SHAPE( NAME, TYPE, WRITE, READ, EXPECTED_BYTES, WHERE )            \
    static int pin_##NAME( const TYPE * pinned )                               \
    {                                                                          \
        static serialize_uint64_t buffer_storage[64 + 1];  /* + 1 word = 8 bytes: read buffer allocations extend 8 bytes past the data */ \
        static serialize_uint64_t twin_storage[64];        /* write-only twin: no read slack needed */ \
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
        printf( "%-14s %3d bytes on the wire (%s says %d)\n",                  \
                #NAME, bytes, WHERE, EXPECTED_BYTES );                         \
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

PIN_SHAPE( bench_packet, BenchPacket, write_bench_packet, read_bench_packet, 49, "BENCH-STANDARD.md §1.3" )
PIN_SHAPE( bench_ints, BenchInts, write_bench_ints, read_bench_ints, 14, "BENCH-STANDARD.md §1.3" )
PIN_SHAPE( bench_bits, BenchBits, write_bench_bits, read_bench_bits, 20, "BENCH-STANDARD.md §1.3" )
PIN_SHAPE( bench_mixed, BenchMixed, write_bench_mixed, read_bench_mixed, 438, "BENCH-STANDARD.md §1.3" )
PIN_SHAPE( real_packet, RealPacket, write_real_packet, read_real_packet, 204, "RealWorld.schema" )

int main( void )
{
    int i;

    BenchPacket packet;
    BenchInts ints;
    BenchBits bits;
    BenchMixed mixed;
    RealPacket real;

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
    mixed.ack_sequence = 12345;
    mixed.ack_bits = 0xA5A5A5A5u;
    mixed.session_id = 0x123456789ABCDEF0ULL;
    mixed.client_id = 0xDEADBEEFu;
    mixed.nonce = 0xFEDCBA9876543210ULL;
    mixed.world_time = -987654321000LL;
    mixed.frame_tick = 0x123456789ABCULL;
    mixed.server_time = 12345678;
    mixed.entities_count = 8;
    for ( i = 0; i < 8; i++ )
    {
        mixed.entities[i].entity_id = (uint32_t) ( 2049 + i * 17 );
        mixed.entities[i].pos_x = -16383 + i * 4096;
        mixed.entities[i].pos_y = 16383 - i * 4096;
        mixed.entities[i].pos_z = -1 + i * 2048;
        mixed.entities[i].yaw = (uint32_t) ( 511 - i * 64 );
        mixed.entities[i].pitch = (uint32_t) ( i * 73 );
        mixed.entities[i].vel_x = -2048 + i * 512;
        mixed.entities[i].vel_y = 2047 - i * 512;
        mixed.entities[i].vel_z = -1024 + i * 256;
        mixed.entities[i].health = 1000 - i * 100;
        mixed.entities[i].weapon = (MixedWeapon) ( 1 + i );
        mixed.entities[i].damage = (MixedDamage) ( 0x5A + i );
        mixed.entities[i].moving = ( i % 2 ) == 0;
        mixed.entities[i].firing = ( i % 3 ) == 0;
    }
    mixed.stats_count = 80;
    for ( i = 0; i < 80; i++ )
    {
        mixed.stats[i].stat_id = (uint32_t) ( ( i * 3 ) % 256 );
        mixed.stats[i].delta = -512 + ( i * 13 ) % 1024;
    }
    mixed.game_event.type = MIXED_EVENT_TYPE_HIT;
    mixed.game_event.as.hit.target_id = 4095;
    mixed.game_event.as.hit.damage = 4095;
    mixed.game_event.as.hit.hit_kind = 7;
    mixed.game_event.as.hit.crit = 1;
    mixed.loadout[0] = 0x11; mixed.loadout[1] = 0x22;
    mixed.loadout[2] = 0x33; mixed.loadout[3] = 0x44;
    memcpy( mixed.player_name, "Rowan_01", 8 );
    mixed.player_name_length = 8;
    memcpy( mixed.payload, "\xDE\xAD\xBE\xEF\x01\x02\x03\x04", 8 );
    mixed.payload_length = 8;
    mixed.aim_x = 0.5f;
    mixed.aim_y = -0.25f;
    mixed.aim_z = 0.75f;
    mixed.recoil = 1.5f;
    mixed.drift = -3.25;
    mixed.wide_key = serialize_uint128_make( 0x0123456789ABCDEFULL, 0xFEDCBA9876543210ULL );
    mixed.flux = serialize_int128_make( 0x800000000ULL, 7ULL );  /* 2^99 + 7 */
    mixed.ping = 12345;
    mixed.crc_hint = 0xABCDEFu;
    mixed.has_extra = 1;
    mixed.extra = 200;

    /* §1.7: the all-defaults instance IS the pin (gate defaults included) */
    real = new_real_packet();

    if ( pin_bench_packet( &packet ) ) return 1;
    if ( pin_bench_ints( &ints ) ) return 1;
    if ( pin_bench_bits( &bits ) ) return 1;
    if ( pin_bench_mixed( &mixed ) ) return 1;
    if ( pin_real_packet( &real ) ) return 1;

    printf( "OK\n" );
    return 0;
}
