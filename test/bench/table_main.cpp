// THE TABLES BENCH CORPUS PRODUCER AND ORACLE (bench/tables/README.md).
//
//   table_main pin      rewrite the golden and the variant corpus
//   table_main verify   rebuild both in memory and byte-compare (the default)
//
// This is the tables leg's twin of test/bench/main.cpp: it is the ONE place
// that names a field of bench/corpus/BenchTable.schema, so the language legs
// under bench/tables/ never do. They read the committed data blind, exactly as
// the type legs read bench/corpus/variants/bench_mixed.variants.bin
// (BENCH-STANDARD.md §1.5, §1.9). It carries no clock and measures nothing.
//
// WHAT IT WRITES
//
//   testdata/wire/bench_table.bin                     record 0, the pinned instance
//   bench/corpus/variants/bench_table.variants.bin    64 records, record 0 first
//
// WHAT IT REFUSES, and why each refusal exists
//
//   * A record whose length differs from record 0's. The table wire ELIDES a
//     field holding its declared default (docs/SPEC-TABLES.md §3), so unlike
//     the bitpacked type wire it frames by PRESENCE, not by width: a varied
//     value that lands on zero would silently shorten the record and move
//     bytes/op under §2.7. Every value below is therefore held OFF its
//     default by construction, and this check is what proves the construction
//     held. It is the tables leg's form of §2.7's "structure fields stay
//     fixed".
//   * A record that does not survive Load -> Save byte-identically.
//   * Two records that are equal. §2.7 rotates 64 buffers so no single buffer
//     can be memorized; 64 copies of one instance would defeat that silently.
//
// The vary mapping is the type corpus's LCG (Knuth MMIX), one step per field,
// so records decorrelate field by field rather than sharing one seed's low
// bits. Record k is the state after k * fields_per_record steps, and record 0
// is the pinned instance.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>

#include "BenchTableTable.h"

using namespace benchtable;

static const int NumVariants = 64;

// the structure pins (§2.7): held fixed across every variant, so the record
// length cannot move
static const int PinnedEntities = 8;
static const int PinnedStats = 80;
static const int PinnedNameLength = 8;
static const int PinnedPayloadLength = 8;
static const uint16_t PinnedMagic = 0xC0DE;

static uint64_t g_rng = 1;

static uint64_t next_rng()
{
    g_rng = g_rng * 6364136223846793005ULL + 1442695040888963407ULL;
    return g_rng;
}

// Every helper below returns a value that CANNOT be the field's declared
// default, which is the elision rule (§3) turned into arithmetic.

static uint64_t in_span( uint64_t span )     // 1 .. span, never 0
{
    return 1 + ( next_rng() % span );
}

static int64_t signed_off_zero( int64_t lo, int64_t hi )
{
    const uint64_t span = (uint64_t) ( hi - lo );
    int64_t v = lo + (int64_t) ( next_rng() % ( span + 1 ) );
    if ( v == 0 ) v = hi;
    return v;
}

static void fill_entity( TableEntity & e )
{
    e.entity_id = (uint32_t) in_span( 4095 );
    e.pos_x = (int32_t) signed_off_zero( -16383, 16383 );
    e.pos_y = (int32_t) signed_off_zero( -16383, 16383 );
    e.pos_z = (int32_t) signed_off_zero( -16383, 16383 );
    e.yaw = (uint32_t) in_span( 511 );
    e.pitch = (uint32_t) in_span( 511 );
    e.vel_x = (int32_t) signed_off_zero( -2048, 2047 );
    e.vel_y = (int32_t) signed_off_zero( -2048, 2047 );
    e.vel_z = (int32_t) signed_off_zero( -2048, 2047 );
    e.health = (int32_t) in_span( 1000 );
    // never None: an enum at None elides, and None is also the value a
    // declared variant must never collide with (§3)
    e.weapon = (TableWeapon) in_span( 15 );
    e.damage = (TableDamage) in_span( 255 );
    e.moving = true;    // structure: a false bool is the default and elides
    e.firing = true;
}

static void fill_stat( TableStat & s )
{
    s.stat_id = (uint32_t) in_span( 255 );
    s.delta = (int32_t) signed_off_zero( -512, 511 );
}

static void fill( TableMixed & v )
{
    TableMixedReset( v );

    v.protocol_magic = PinnedMagic;
    v.sequence = (uint32_t) in_span( 65535 );
    v.ack_sequence = (int32_t) in_span( 65535 );
    v.ack_bits = (uint32_t) in_span( 0xFFFFFFFEull );
    v.session_id = next_rng() | 1;
    v.client_id = (uint32_t) next_rng() | 1u;
    v.nonce = ( next_rng() >> 1 ) | 1;    // inside the declared bound, never 0
    v.world_time = signed_off_zero( -1000000000000LL, 1000000000000LL );
    v.frame_tick = ( next_rng() & 0xFFFFFFFFFFFFull ) | 1;
    // in [0, 65535] and exactly representable in f32, so nothing clamps on
    // load and the bit pattern is stable across hosts
    v.server_time = 1.0f + (float) ( next_rng() % 65534 );

    v.entities_count = PinnedEntities;
    for ( int i = 0; i < PinnedEntities; i++ ) fill_entity( v.entities[i] );

    v.stats_count = PinnedStats;
    for ( int i = 0; i < PinnedStats; i++ ) fill_stat( v.stats[i] );

    // the pinned arm, and the largest: the pinned wire is the union's worst case
    v.game_event.hit = TableHitEvent{};
    v.game_event.type = TableEventType::Hit;
    v.game_event.hit.target_id = (uint32_t) in_span( 4095 );
    v.game_event.hit.damage = (int32_t) in_span( 4095 );
    v.game_event.hit.hit_kind = (int32_t) in_span( 7 );
    v.game_event.hit.crit = true;

    for ( int i = 0; i < 4; i++ ) v.loadout[i] = (uint8_t) in_span( 255 );

    v.player_name_length = PinnedNameLength;
    for ( int i = 0; i < PinnedNameLength; i++ )
        v.player_name[i] = (char) ( 'a' + ( next_rng() % 26 ) );
    v.player_name[PinnedNameLength] = '\0';

    v.payload_length = PinnedPayloadLength;
    for ( int i = 0; i < PinnedPayloadLength; i++ )
        v.payload[i] = (uint8_t) in_span( 255 );

    // in [-1, 1], never 0
    v.aim_x = (float) signed_off_zero( -100, 100 ) / 100.0f;
    v.aim_y = (float) signed_off_zero( -100, 100 ) / 100.0f;
    v.aim_z = (float) signed_off_zero( -100, 100 ) / 100.0f;
    v.recoil = 1.0f + (float) ( next_rng() % 1000 );
    v.drift = 1.0 + (double) ( next_rng() % 1000000 );
    v.wide_key = next_rng() | 1;
    v.flux = signed_off_zero( -1000000000000000000LL, 1000000000000000000LL );
    v.ping = 1.0f + (float) ( next_rng() % 249 );
    v.crc_hint = (uint32_t) ( next_rng() & 0xFFFFFFull ) | 1u;

    v.has_extra = true;                     // structure: the guard, pinned true
    v.extra = (int32_t) in_span( 255 );
    // idle_ticks never rides: the else branch is off, and a field under a
    // false guard is elided whatever its storage holds (§3)
}

// ---------------------------------------------------------------------------

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) return false;
    uint8_t chunk[65536];
    size_t n;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 ) out.insert( out.end(), chunk, chunk + n );
    fclose( f );
    return true;
}

static bool spill( const char * path, const uint8_t * data, size_t bytes )
{
    FILE * f = fopen( path, "wb" );
    if ( f == NULL )
    {
        fprintf( stderr, "cannot write %s\n", path );
        return false;
    }
    const bool ok = bytes == 0 || fwrite( data, 1, bytes, f ) == bytes;
    fclose( f );
    return ok;
}

static const char * const GoldenPath = "testdata/wire/bench_table.bin";
static const char * const VariantPath = "bench/corpus/variants/bench_table.variants.bin";

int main( int argc, char ** argv )
{
    const char * mode = argc > 1 ? argv[1] : "verify";
    if ( strcmp( mode, "pin" ) != 0 && strcmp( mode, "verify" ) != 0 )
    {
        fprintf( stderr, "usage: %s [pin|verify]   (run from the repository root)\n", argv[0] );
        return 1;
    }

    static TableMixed instance;
    std::vector<uint8_t> corpus;
    int64_t record = -1;

    std::vector<uint8_t> buffer( 65536 );
    std::vector<uint8_t> twin( 65536 );

    for ( int k = 0; k < NumVariants; k++ )
    {
        fill( instance );

        const int64_t measured = TableMixedMeasure( instance );
        const int64_t saved = TableMixedSave( instance, buffer.data(), (int64_t) buffer.size() );
        if ( saved < 0 || saved != measured )
        {
            fprintf( stderr, "record %d: Save wrote %lld bytes, Measure said %lld\n",
                     k, (long long) saved, (long long) measured );
            return 1;
        }

        if ( record < 0 )
        {
            record = saved;
        }
        else if ( saved != record )
        {
            fprintf( stderr, "record %d is %lld bytes, record 0 is %lld — the table wire elides a field at its"
                             " default, so a varied value landed on one. Fix the vary mapping, not this check.\n",
                     k, (long long) saved, (long long) record );
            return 1;
        }

        // the round-trip gate: what a reader gets back re-saves identically
        static TableMixed decoded;
        TableMixedReset( decoded );
        TableReport report;
        if ( !TableMixedLoad( decoded, buffer.data(), saved, &report ) )
        {
            fprintf( stderr, "record %d: Load refused its own Save\n", k );
            return 1;
        }
        if ( report.malformed || report.unknown != 0 || report.kind_mismatch != 0 ||
             report.clamped != 0 || report.duplicate != 0 )
        {
            fprintf( stderr, "record %d: a clean round-trip reported u=%d k=%d c=%d d=%d m=%d\n",
                     k, report.unknown, report.kind_mismatch, report.clamped,
                     report.duplicate, (int) report.malformed );
            return 1;
        }
        const int64_t rewrote = TableMixedSave( decoded, twin.data(), (int64_t) twin.size() );
        if ( rewrote != saved || memcmp( twin.data(), buffer.data(), (size_t) saved ) != 0 )
        {
            fprintf( stderr, "record %d: re-save after load differs\n", k );
            return 1;
        }

        // §2.7: 64 DISTINCT buffers, or the rotation buys nothing
        for ( int j = 0; j < k; j++ )
        {
            if ( memcmp( corpus.data() + (size_t) j * (size_t) record, buffer.data(), (size_t) record ) == 0 )
            {
                fprintf( stderr, "record %d duplicates record %d\n", k, j );
                return 1;
            }
        }

        corpus.insert( corpus.end(), buffer.begin(), buffer.begin() + saved );
    }

    if ( strcmp( mode, "pin" ) == 0 )
    {
        if ( !spill( GoldenPath, corpus.data(), (size_t) record ) ) return 1;
        if ( !spill( VariantPath, corpus.data(), corpus.size() ) ) return 1;
        printf( "pinned %s (%lld bytes) and %s (%d x %lld)\n",
                GoldenPath, (long long) record, VariantPath, NumVariants, (long long) record );
        return 0;
    }

    std::vector<uint8_t> golden;
    std::vector<uint8_t> committed;
    if ( !slurp( GoldenPath, golden ) )
    {
        fprintf( stderr, "missing %s — run `make bench-table-corpus` from the repository root\n", GoldenPath );
        return 1;
    }
    if ( !slurp( VariantPath, committed ) )
    {
        fprintf( stderr, "missing %s — run `make bench-table-corpus` from the repository root\n", VariantPath );
        return 1;
    }
    if ( golden.size() != (size_t) record || memcmp( golden.data(), corpus.data(), (size_t) record ) != 0 )
    {
        fprintf( stderr, "GOLDEN MISMATCH: %s is %zu bytes, this build produces %lld\n",
                 GoldenPath, golden.size(), (long long) record );
        return 1;
    }
    if ( committed != corpus )
    {
        fprintf( stderr, "VARIANT CORPUS MISMATCH: %s is %zu bytes, this build produces %zu\n",
                 VariantPath, committed.size(), corpus.size() );
        return 1;
    }
    printf( "bench table corpus OK: %d records of %lld bytes\n", NumVariants, (long long) record );
    return 0;
}
