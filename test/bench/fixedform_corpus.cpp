// THE FIXED FORM'S CORPUS, written by the C++ REFERENCE (docs/SPEC-TABLES.md
// §3.4). The paired bench's own sixty-four logical records — decoded from the
// canonical PACKET corpus, so no field name and no independent value generator
// lives here — saved with the reference's form-3 writer.
//
// It answers two files, and a port that reads them is checked twice over:
//
//   <out.bin>     the bytes. Another port's writer must produce these EXACTLY,
//                 and its reader must read them back.
//   <oracle.json> the VALUES those bytes carry, stated independently. A reader
//                 and a writer that share one offset mistake round trip
//                 perfectly and are both wrong, so the bytes alone are not
//                 enough and the values are published beside them.
//
// The reference also reads its own file back through its own plan-driven loop
// before writing either, so a corpus that ships is one the reference agrees
// with end to end.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>
#include "BenchWire.h"
#include "BenchTable.h"
#include "FixedTableTable.h"

static bool read_file( const char * path, std::vector<uint8_t> & data )
{
    FILE * f = std::fopen( path, "rb" );
    if ( !f ) return false;
    uint8_t b[65536];
    size_t n;
    while ( ( n = std::fread( b, 1, sizeof( b ), f ) ) != 0 )
        data.insert( data.end(), b, b + n );
    const bool ok = !std::ferror( f );
    std::fclose( f );
    return ok;
}

int main( int argc, char ** argv )
{
    if ( argc < 3 || argc > 4 ) { std::fprintf( stderr, "usage: %s <variants.bin> <out.bin> [oracle.json]\n", argv[0] ); return 1; }
    std::vector<uint8_t> packet;
    if ( !read_file( argv[1], packet ) ) { std::fprintf( stderr, "cannot read %s\n", argv[1] ); return 1; }
    constexpr size_t Count = 64;
    const size_t stride = packet.size() / Count;
    if ( stride * Count != packet.size() ) { std::fprintf( stderr, "not 64 records\n" ); return 1; }

    std::vector<bench::FixedTable> values( Count );
    alignas( 8 ) uint8_t source[65536] = {};
    for ( size_t k = 0; k < Count; ++k )
    {
        std::memcpy( source, packet.data() + k * stride, stride );
        bench::BenchMixed value;
        serialize::ReadStream rs( source, (int) stride );
        if ( !bench::ReadBenchMixed( rs, value ) ) { std::fprintf( stderr, "packet read %zu failed\n", k ); return 1; }
        values[k].value = value;
    }

    const int64_t need = bench::FixedTableFixedMeasure( (int64_t) Count );
    std::vector<uint8_t> out( (size_t) need );
    const int64_t wrote = bench::FixedTableFixedSave( values.data(), (int64_t) Count, out.data(), need );
    if ( wrote != need ) { std::fprintf( stderr, "fixed save failed: %lld\n", (long long) wrote ); return 1; }

    FILE * f = std::fopen( argv[2], "wb" );
    if ( !f ) return 1;
    const bool ok = std::fwrite( out.data(), 1, out.size(), f ) == out.size();
    if ( std::fclose( f ) != 0 || !ok ) return 1;

    std::printf( "fixed corpus: %zu records, layout %lld bytes, body %lld, record %lld, total %lld, hash 0x%016llx\n",
                 Count, (long long) bench::FixedTableFixedLayoutBytes,
                 (long long) bench::FixedTableFixedBodyBytes,
                 (long long) bench::FixedTableFixedRecordBytes,
                 (long long) need, (unsigned long long) bench::FixedTableFixedHash );

    // and it reads back through the reference's OWN plan-driven loop
    std::vector<bench::FixedTable> back( Count );
    std::vector<bench::TableFixedEntry> plan( 8192 );
    bench::TableReport report;
    const int64_t n = bench::FixedTableFixedLoad( back.data(), (int64_t) Count, out.data(), need,
                                                  plan.data(), (int32_t) plan.size(), NULL, &report );
    if ( n != (int64_t) Count ) { std::fprintf( stderr, "fixed load failed: %lld\n", (long long) n ); return 1; }
    for ( size_t k = 0; k < Count; ++k )
    {
        alignas( 8 ) uint8_t a[65536] = {}, b[65536] = {};
        const int64_t sa = bench::BenchMixedSave( values[k].value, a, sizeof( a ) );
        const int64_t sb = bench::BenchMixedSave( back[k].value, b, sizeof( b ) );
        if ( sa <= 0 || sa != sb || std::memcmp( a, b, (size_t) sa ) )
        {
            std::fprintf( stderr, "record %zu did not survive the fixed round trip\n", k );
            return 1;
        }
    }
    std::printf( "reference round trip: 64/64 identical; unknown %d kind_mismatch %d clamped %d widened %d\n",
                 (int) report.unknown, (int) report.kind_mismatch, (int) report.clamped, (int) report.widened );

    // THE VALUE ORACLE, so another port checks its DECODED VALUES and not only
    // its bytes: a reader and a writer that share one offset mistake round trip
    // perfectly and are both wrong, so the values are stated independently here.
    if ( argc >= 4 )
    {
        FILE * j = std::fopen( argv[3], "wb" );
        if ( !j ) return 1;
        std::fprintf( j, "[\n" );
        for ( size_t k = 0; k < Count; ++k )
        {
            const bench::BenchMixed & v = values[k].value;
            std::fprintf( j, "%s{", k ? ",\n" : "" );
            std::fprintf( j, "\"sequence\":%u,", (unsigned) v.sequence );
            std::fprintf( j, "\"ack_sequence\":%d,", (int) v.ack_sequence );
            std::fprintf( j, "\"ack_bits\":%u,", (unsigned) v.ack_bits );
            std::fprintf( j, "\"session_id\":\"%llu\",", (unsigned long long) v.session_id );
            std::fprintf( j, "\"client_id\":%u,", (unsigned) v.client_id );
            std::fprintf( j, "\"nonce\":\"%llu\",", (unsigned long long) v.nonce );
            std::fprintf( j, "\"world_time\":\"%lld\",", (long long) v.world_time );
            std::fprintf( j, "\"frame_tick\":\"%llu\",", (unsigned long long) v.frame_tick );
            std::fprintf( j, "\"server_time\":%d,", (int) v.server_time );
            std::fprintf( j, "\"entities_count\":%d,", (int) v.entities_count );
            std::fprintf( j, "\"entities\":[" );
            for ( int i = 0; i < v.entities_count; ++i )
            {
                const bench::MixedEntity & e = v.entities[i];
                std::fprintf( j, "%s{\"entity_id\":%u,\"pos_x\":%d,\"pos_y\":%d,\"pos_z\":%d,"
                                 "\"yaw\":%u,\"pitch\":%u,\"vel_x\":%d,\"vel_y\":%d,\"vel_z\":%d,"
                                 "\"health\":%d,\"weapon\":%d,\"damage\":\"%llu\",\"moving\":%s,\"firing\":%s}",
                              i ? "," : "", (unsigned) e.entity_id, (int) e.pos_x, (int) e.pos_y, (int) e.pos_z,
                              (unsigned) e.yaw, (unsigned) e.pitch, (int) e.vel_x, (int) e.vel_y, (int) e.vel_z,
                              (int) e.health, (int) e.weapon, (unsigned long long) e.damage,
                              e.moving ? "true" : "false", e.firing ? "true" : "false" );
            }
            std::fprintf( j, "]," );
            std::fprintf( j, "\"stats_count\":%d,", (int) v.stats_count );
            std::fprintf( j, "\"stats\":[" );
            for ( int i = 0; i < v.stats_count; ++i )
                std::fprintf( j, "%s{\"stat_id\":%u,\"delta\":%d}", i ? "," : "",
                              (unsigned) v.stats[i].stat_id, (int) v.stats[i].delta );
            std::fprintf( j, "]," );
            std::fprintf( j, "\"game_event_type\":%d,", (int) v.game_event.type );
            std::fprintf( j, "\"hit\":{\"target_id\":%u,\"damage\":%d,\"hit_kind\":%d,\"crit\":%s},",
                          (unsigned) v.game_event.hit.target_id, (int) v.game_event.hit.damage,
                          (int) v.game_event.hit.hit_kind, v.game_event.hit.crit ? "true" : "false" );
            std::fprintf( j, "\"chat\":{\"channel\":%d,\"speaker\":%u},",
                          (int) v.game_event.chat.channel, (unsigned) v.game_event.chat.speaker );
            std::fprintf( j, "\"pickup\":{\"item_id\":%u,\"amount\":%d},",
                          (unsigned) v.game_event.pickup.item_id, (int) v.game_event.pickup.amount );
            std::fprintf( j, "\"loadout\":[%u,%u,%u,%u],",
                          (unsigned) v.loadout[0], (unsigned) v.loadout[1],
                          (unsigned) v.loadout[2], (unsigned) v.loadout[3] );
            std::fprintf( j, "\"player_name_length\":%d,\"player_name\":[", (int) v.player_name_length );
            for ( int i = 0; i < 15; ++i ) std::fprintf( j, "%s%u", i ? "," : "", (unsigned) (uint8_t) v.player_name[i] );
            std::fprintf( j, "]," );
            std::fprintf( j, "\"payload_length\":%d,\"payload\":[", (int) v.payload_length );
            for ( int i = 0; i < 16; ++i ) std::fprintf( j, "%s%u", i ? "," : "", (unsigned) v.payload[i] );
            std::fprintf( j, "]," );
            uint32_t bx, by, bz, br; uint64_t bd;
            std::memcpy( &bx, &v.aim_x, 4 ); std::memcpy( &by, &v.aim_y, 4 );
            std::memcpy( &bz, &v.aim_z, 4 ); std::memcpy( &br, &v.recoil, 4 );
            std::memcpy( &bd, &v.drift, 8 );
            // floats ride as their BIT PATTERNS so a comparison is exact, with
            // no canonicalisation on this form (docs/SPEC-TABLES.md §3.4)
            std::fprintf( j, "\"aim_x_bits\":%u,\"aim_y_bits\":%u,\"aim_z_bits\":%u,", bx, by, bz );
            std::fprintf( j, "\"recoil_bits\":%u,\"drift_bits\":\"%llu\",", br, (unsigned long long) bd );
            std::fprintf( j, "\"wide_key_lo\":\"%llu\",\"wide_key_hi\":\"%llu\",",
                          (unsigned long long) (uint64_t) v.wide_key,
                          (unsigned long long) (uint64_t) ( v.wide_key >> 64 ) );
            std::fprintf( j, "\"flux_lo\":\"%llu\",\"flux_hi\":\"%llu\",",
                          (unsigned long long) (uint64_t) (serialize::uint128_t) v.flux,
                          (unsigned long long) (uint64_t) ( (serialize::uint128_t) v.flux >> 64 ) );
            std::fprintf( j, "\"ping\":%u,\"crc_hint\":%u,", (unsigned) v.ping, (unsigned) v.crc_hint );
            std::fprintf( j, "\"has_extra\":%s,\"extra\":%d,\"idle_ticks\":%d",
                          v.has_extra ? "true" : "false", (int) v.extra, (int) v.idle_ticks );
            std::fprintf( j, "}" );
        }
        std::fprintf( j, "\n]\n" );
        if ( std::fclose( j ) != 0 ) return 1;
        std::printf( "value oracle written: %s\n", argv[3] );
    }
    return 0;
}
