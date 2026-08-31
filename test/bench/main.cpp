// The bench-corpus golden pinner (BENCH-STANDARD.md §1.5, §6 step 6, §1.7).
//
// Writes the pinned bench-corpus instances through the schema-GENERATED
// C++ code (generated/bench/cpp) and byte-checks them against the wire
// goldens testdata/wire/{bench_*,real_packet}.bin, then round-trips
// write -> read -> re-write -> memcmp. THE GOLDENS PRODUCED HERE ARE THE
// AUTHORITY for the oracle gate in all five bench runners: a runner must
// reproduce these bytes exactly or refuse to bench.
//
// SCHEMA_UPDATE_WIRE_GOLDENS=1 re-pins deliberately (make update-goldens);
// a break under an unchanged schema is stop-the-line (SPEC §3.1), never a
// quiet re-pin.
//
// The pinned values are transcribed from serialize/bench.cpp BenchPacket::
// Init() for BenchPacket; BenchInts and BenchBits pin nonzero, in-range,
// boundary-flavored values chosen here, and BenchMixed — THE canonical
// benchmark since issue #184 — pins the values below. RealPacket (bench/corpus/
// RealWorld.schema, the §1.7 realistic snapshot) pins the ALL-DEFAULTS
// instance: a RealPacket constructed and serialized unmodified, every field
// at its declared default — the four branch gates carry theirs in the schema
// (f012 true, f043 false, f050 true, f074 false), so 1629 bits = 204 bytes
// ride. Every bench runner carries the same pins — the goldens are what keep
// the transcriptions honest.

#include <cstdio>
#include <cstdlib>
#include <cstring>

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

// BenchMixed — THE canonical benchmark shape (issue #184). This pin is the
// definition every one of the nine bench legs transcribes; the golden keeps
// the transcriptions honest. Structure fields (the two array counts, the two
// used lengths, the union tag, the `if` gate) are pinned here and held FIXED
// under §2.7 variation in every leg, so bytes/op never moves.
static BenchMixed pin_bench_mixed()
{
    BenchMixed in;
    in.sequence = 52428;
    in.ack_sequence = 12345;
    in.ack_bits = 0xA5A5A5A5u;
    in.session_id = 0x123456789ABCDEF0ull;
    in.client_id = 0xDEADBEEFu;
    in.nonce = 0xFEDCBA9876543210ull;
    in.world_time = -987654321000ll;
    in.frame_tick = 0x123456789ABCull;
    in.server_time = 12345678;   // raw Q24.8 (48225.30 whole units), <= 65535 << 8

    in.entities_count = 8;
    for ( int i = 0; i < 8; i++ )
    {
        MixedEntity & e = in.entities[i];
        e.entity_id = (uint32_t) ( 2049 + i * 17 );
        e.pos_x = -16383 + i * 4096;
        e.pos_y = 16383 - i * 4096;
        e.pos_z = -1 + i * 2048;
        e.yaw = (uint32_t) ( 511 - i * 64 );
        e.pitch = (uint32_t) ( i * 73 );
        e.vel_x = -2048 + i * 512;
        e.vel_y = 2047 - i * 512;
        e.vel_z = -1024 + i * 256;
        e.health = 1000 - i * 100;
        e.weapon = MixedWeapon( 1 + i );
        e.damage = (MixedDamage) ( 0x5A + i );
        e.moving = ( i % 2 ) == 0;
        e.firing = ( i % 3 ) == 0;
    }

    in.stats_count = 80;
    for ( int i = 0; i < 80; i++ )
    {
        in.stats[i].stat_id = (uint32_t) ( ( i * 3 ) % 256 );
        in.stats[i].delta = -512 + ( i * 13 ) % 1024;
    }

    in.game_event.type = MixedEventType::Hit;
    in.game_event.hit.target_id = 4095;
    in.game_event.hit.damage = 4095;
    in.game_event.hit.hit_kind = 7;
    in.game_event.hit.crit = true;

    in.loadout[0] = 0x11; in.loadout[1] = 0x22; in.loadout[2] = 0x33; in.loadout[3] = 0x44;

    memcpy( in.player_name, "Rowan_01", 8 );
    in.player_name_length = 8;

    static const uint8_t payload[8] = { 0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04 };
    memcpy( in.payload, payload, 8 );
    in.payload_length = 8;

    in.aim_x = 0.5f;
    in.aim_y = -0.25f;
    in.aim_z = 0.75f;
    in.recoil = 1.5f;
    in.drift = -3.25;

    in.wide_key = ( serialize::uint128_t( 0x0123456789ABCDEFull ) << 64 )
                | serialize::uint128_t( 0xFEDCBA9876543210ull );
    in.flux = ( serialize::int128_t( 1 ) << 99 ) + serialize::int128_t( 7 );
    in.ping = 12345;             // raw UQ8.8 (48.22 whole units), <= 250 << 8
    in.crc_hint = 0xABCDEFu;
    in.has_extra = true;
    in.extra = 200;
    return in;
}

// write pinned -> golden check -> read -> re-write -> memcmp, and report the
// measured wire size so the documented byte claim (`where` names the document
// making it) is checked against reality
template <typename T, typename WriteFn, typename ReadFn>
static int pin_shape( const char * name, int expected_bytes, const char * where, const T & pinned, WriteFn write_fn, ReadFn read_fn )
{
    alignas( 8 ) static uint8_t buffer[512 + 8];  // + 8: read buffer allocations extend 8 bytes past the data
    alignas( 8 ) static uint8_t twin[512];       // write-only twin: no read slack needed
    memset( buffer, 0, sizeof( buffer ) );
    memset( twin, 0, sizeof( twin ) );

    T in = pinned;
    serialize::WriteStream ws( buffer, sizeof( buffer ) );
    check( write_fn( ws, in ) );
    ws.Flush();
    const int64_t bytes = ws.GetBytesProcessed();
    printf( "%-14s %3lld bytes on the wire (%s says %d)\n",
            name, (long long) bytes, where, expected_bytes );
    check( bytes == expected_bytes );   // the documented claim vs the generated code — the goldens win
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


// ---- refusal vectors on BenchMixed's new constructs (issue #184) ----
//
// The redefined shape brought in constructs the bench corpus never carried.
// Reads must REJECT malformed bytes, never clamp (SPEC §4.3, §5). The bit
// offsets come from the accounting table bench/corpus/budget_test.go prints;
// each vector asserts the bits it is about to doctor still hold the pinned
// value, so a shape change makes this loud instead of quietly testing the
// wrong bit, and each carries its own verdict so the set can be reordered
// or extended without an index cutoff to keep in step.
//
// COVERED here: the const magic, reserved bits, the explicit align padding,
// a bytes(N) length above its bound, a counted-array count above its bound,
// and a ranged integer above its range.
//
// NOT COVERED, and why — these constructs have no refusable encoding on this
// shape, so there is nothing to doctor:
//   * the entities count. [1..8] encodes count − 1 in 3 bits, and all eight
//     3-bit values are in range. (stats [..80] IS refusable — 7 bits carry
//     0..127 against a bound of 80 — and is a vector below.)
//   * the union tag. 3 variants + implicit None is [0, 3] in 2 bits, and all
//     four values are legal.
//   * the string(15) length prefix. [0, 15] in 4 bits — every value legal.
//     bytes(16) is the refusable twin: [0, 16] needs 5 bits, so 17..31 are
//     encodable and must be refused.
// A wider shape would make the first three refusable; if one ever does, the
// vector belongs here.
static void set_bits( uint8_t * data, int64_t offset, int64_t bits, uint64_t value )
{
    for ( int64_t i = 0; i < bits; i++ )
    {
        const int64_t at = offset + i;
        const uint8_t mask = (uint8_t) ( 1u << ( at % 8 ) );
        if ( ( value >> i ) & 1 )
            data[at / 8] |= mask;
        else
            data[at / 8] = (uint8_t) ( data[at / 8] & ~mask );
    }
}

static uint64_t get_bits( const uint8_t * data, int64_t offset, int64_t bits )
{
    uint64_t out = 0;
    for ( int64_t i = 0; i < bits; i++ )
    {
        const int64_t at = offset + i;
        out |= (uint64_t) ( ( data[at / 8] >> ( at % 8 ) ) & 1 ) << i;
    }
    return out;
}

static int refusal_vectors()
{
    alignas( 8 ) static uint8_t pinned[512];
    memset( pinned, 0, sizeof( pinned ) );
    BenchMixed in = pin_bench_mixed();
    serialize::WriteStream ws( pinned, sizeof( pinned ) );
    check( WriteBenchMixed( ws, in ) );
    ws.Flush();
    const int64_t bytes = ws.GetBytesProcessed();
    check( bytes == 438 );

    struct Vector
    {
        const char * what;
        int64_t offset;
        int64_t bits;
        uint64_t pinned_value;   // what the pin puts there — asserted before doctoring
        uint64_t doctored;       // what is written over it
        bool must_refuse;        // false = an in-range control that must be ACCEPTED
    };

    static const Vector vectors[] = {
        { "const(0xC0DE, 16) magic doctored",          0, 16, 0xC0DE, 0xC0DF, true },
        { "reserved(4) nonzero",                    3454,  4,      0,      1, true },
        { "align padding nonzero",                  3458,  6,      0,      1, true },
        { "bytes(16) length above its bound",       3016,  5,      8,     17, true },
        { "stats count above its bound",            1436,  7,     80,    127, true },
        { "entity health above its range",           467, 10,   1000,   1001, true },
        { "CONTROL: hit_kind rewritten IN range",   2909,  3,      7,      0, false },
    };
    int refuse_count = 0;

    alignas( 8 ) static uint8_t doctored[512];
    BenchMixed out;
    for ( int v = 0; v < (int) ( sizeof( vectors ) / sizeof( vectors[0] ) ); v++ )
    {
        const Vector & vec = vectors[v];
        if ( get_bits( pinned, vec.offset, vec.bits ) != vec.pinned_value )
        {
            printf( "REFUSAL VECTOR STALE: %s — bits at %lld are not the pinned value; "
                    "re-derive the offsets from bench/corpus/budget_test.go\n",
                    vec.what, (long long) vec.offset );
            return 1;
        }
        memcpy( doctored, pinned, (size_t) bytes );
        set_bits( doctored, vec.offset, vec.bits, vec.doctored );
        serialize::ReadStream rs( doctored, (int) bytes );
        const bool accepted = ReadBenchMixed( rs, out );
        if ( vec.must_refuse )
        {
            refuse_count++;
            if ( accepted )
            {
                printf( "REFUSAL VECTOR FAILED: %s was ACCEPTED — reads must reject, never clamp\n", vec.what );
                return 1;
            }
        }
        else if ( !accepted )
        {
            printf( "REFUSAL CONTROL FAILED: %s is in range and must be accepted\n", vec.what );
            return 1;
        }
    }
    printf( "%-14s %d refusal vectors + 1 in-range control\n", "bench_mixed", refuse_count );
    return 0;
}

int main()
{
    static_assert( BenchPacketMaxBits == 392, "§1.3: BenchPacket is 392 bits" );
    static_assert( BenchIntsMaxBits == 110, "§1.3: BenchInts is 110 bits" );
    static_assert( BenchBitsMaxBits == 156, "§1.3: BenchBits is 156 bits" );
    static_assert( BenchMixedMaxBits == 3626, "§1.3: BenchMixed's worst case is 3626 bits (the pinned wire is 3504 = 438 bytes)" );
    // worst case includes the 181 bits of untaken branch bodies; the pinned
    // all-defaults wire is 1629 bits = 204 bytes (RealWorld.schema header)
    static_assert( realworld::RealPacketMaxBits == 1810, "RealWorld.schema: RealPacket worst case is 1810 bits" );

    if ( pin_shape( "bench_packet", 49, "BENCH-STANDARD.md §1.3", pin_bench_packet(), WriteBenchPacket, ReadBenchPacket ) ) return 1;
    if ( pin_shape( "bench_ints", 14, "BENCH-STANDARD.md §1.3", pin_bench_ints(), WriteBenchInts, ReadBenchInts ) ) return 1;
    if ( pin_shape( "bench_bits", 20, "BENCH-STANDARD.md §1.3", pin_bench_bits(), WriteBenchBits, ReadBenchBits ) ) return 1;
    if ( pin_shape( "bench_mixed", 438, "BENCH-STANDARD.md §1.3", pin_bench_mixed(), WriteBenchMixed, ReadBenchMixed ) ) return 1;

    if ( refusal_vectors() ) return 1;

    // §1.7: the realistic snapshot — the all-defaults instance IS the pin
    if ( pin_shape( "real_packet", 204, "RealWorld.schema", realworld::RealPacket{}, realworld::WriteRealPacket, realworld::ReadRealPacket ) ) return 1;

    printf( "OK\n" );
    return 0;
}
