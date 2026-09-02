// The PACK GOLDEN (SPEC-TABLES.md §17.4, schema#257). `schema pack` carries an
// IR-driven engine written in Go — the compiler cannot run the code it emits —
// and this driver is what holds that engine to the generated C++ byte for
// byte: the same instance, built here BY HAND, saved through the generated
// codec, and compared against the bytes `schema pack` produced from the
// directory tree at tables/pack/config.
//
// Three gates, in order:
//
//   1. `schema pack` bytes == PackConfigSave of the hand-built instance.
//   2. PackConfigLoad of pack's bytes reports ALL ZERO — the wire the Go
//      engine wrote is one this build reads with nothing skipped, nothing
//      renamed and nothing cut down.
//   3. The instance that comes back equals the one built by hand, field for
//      field, so a byte match cannot be a coincidence of two identical bugs.
//   4. §17.1's THIRD golden: the text `schema unpack --one-file` wrote for that
//      instance is byte-identical to the one the generated `ToJson` writes.
//      Two implementations of §16's writer, one text — the gate that catches a
//      vocabulary error a round trip cannot see (reader and writer share the
//      name function, so a wrong spelling round-trips perfectly) and the
//      pretty-print contract drifting.
//
// Two roots: PackConfig (tables/pack/config) for the fixed-class collection
// shape — enum-keyed arrays of records, an optional section, a nested global
// block, a bounded array of records — and RootConfig (tables/pack/root) for
// the kinds the first one does not reach: a union with two arms, a flags mask,
// a guarded group, `bytes(N)`, `bits(N)`, every integer width, a fixed array
// of enums and a fixed array of tables.
//
// usage: schema_test_pack <PackConfig.bin> <RootConfig.bin> <PackConfig.json> <RootConfig.json>

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "PackTable.h"
#include "TablesTable.h"

static int failures = 0;

#define CHECK( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAIL %s:%d: %s\n", __FILE__, __LINE__, #condition );     \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

using namespace tabledemo;

static void set_string( char * storage, int32_t & length, const char * text )
{
    length = (int32_t) strlen( text );
    memcpy( storage, text, (size_t) length + 1 );
}

// The instance the tree at tables/pack/config declares, built by hand. Every
// value here is read off those files; a divergence is what the golden exists
// to catch.
static void build( PackConfig & config )
{
    config.version = 7;

    config.global.tick_rate = 120;
    config.global.difficulty = Difficulty::Hard;
    set_string( config.global.build_note, config.global.build_note_length, "corpus" );
    config.global.spawn_delays[0] = 0.5f;
    config.global.spawn_delays[1] = 1.25f;
    config.global.spawn_delays[2] = 2.0f;

    ShipEntry & fighter = config.ships[ShipType::Fighter];
    set_string( fighter.display_name, fighter.display_name_length, "Fighter" );
    fighter.health = 120.0f;
    fighter.mass = 0.75f;
    fighter.hardpoints[0] = 1;
    fighter.hardpoints[1] = 2;
    fighter.hardpoints[2] = 3;
    fighter.hardpoints_count = 3;
    fighter.gunner_present = true;
    fighter.gunner.reaction = 0.35f;
    fighter.gunner.tracking = true;
    set_string( fighter.gunner.callsign, fighter.gunner.callsign_length, "Vixen" );

    ShipEntry & bomber = config.ships[ShipType::Bomber];
    set_string( bomber.display_name, bomber.display_name_length, "Bomber" );
    bomber.health = 260.0f;
    bomber.mass = 3.5f;
    bomber.hardpoints[0] = 4;
    bomber.hardpoints[1] = 5;
    bomber.hardpoints_count = 2;

    ShipEntry & scout = config.ships[ShipType::Scout];
    set_string( scout.display_name, scout.display_name_length, "Scout" );
    scout.health = 80.0f;
    scout.mass = 0.5f;
    // an optional whose object carried nothing but a default: PRESENCE, not
    // content, is what puts it on the wire (SPEC-TABLES.md §2.3)
    scout.gunner_present = true;
    scout.gunner.tracking = false;

    config.thresholds[Difficulty::Easy] = 10;
    config.thresholds[Difficulty::Hard] = 750;

    set_string( config.reserves[0].display_name, config.reserves[0].display_name_length, "Alpha" );
    config.reserves[0].health = 55.0f;
    set_string( config.reserves[1].display_name, config.reserves[1].display_name_length, "Beta" );
    config.reserves[1].mass = 2.0f;
    config.reserves[1].hardpoints[0] = 7;
    config.reserves[1].hardpoints_count = 1;
    config.reserves_count = 2;
}

static bool same_ship( const ShipEntry & a, const ShipEntry & b )
{
    if ( a.display_name_length != b.display_name_length ) { return false; }
    if ( memcmp( a.display_name, b.display_name, (size_t) a.display_name_length ) != 0 ) { return false; }
    if ( a.health != b.health || a.mass != b.mass ) { return false; }
    if ( a.hardpoints_count != b.hardpoints_count ) { return false; }
    for ( int32_t i = 0; i < a.hardpoints_count; i++ )
    {
        if ( a.hardpoints[i] != b.hardpoints[i] ) { return false; }
    }
    if ( a.gunner_present != b.gunner_present ) { return false; }
    if ( !a.gunner_present ) { return true; }
    if ( a.gunner.reaction != b.gunner.reaction || a.gunner.tracking != b.gunner.tracking ) { return false; }
    if ( a.gunner.callsign_length != b.gunner.callsign_length ) { return false; }
    return memcmp( a.gunner.callsign, b.gunner.callsign, (size_t) a.gunner.callsign_length ) == 0;
}

static bool same_config( const PackConfig & a, const PackConfig & b )
{
    if ( a.version != b.version ) { return false; }
    if ( a.global.tick_rate != b.global.tick_rate ) { return false; }
    if ( a.global.difficulty != b.global.difficulty ) { return false; }
    if ( a.global.build_note_length != b.global.build_note_length ) { return false; }
    if ( memcmp( a.global.build_note, b.global.build_note, (size_t) a.global.build_note_length ) != 0 ) { return false; }
    for ( int32_t i = 0; i < 3; i++ )
    {
        if ( a.global.spawn_delays[i] != b.global.spawn_delays[i] ) { return false; }
    }
    // slot 0 is None's and is never valid, so the comparison starts at 1
    for ( int32_t i = 1; i < 4; i++ )
    {
        if ( !same_ship( a.ships.slots[i], b.ships.slots[i] ) ) { return false; }
        if ( a.thresholds.slots[i] != b.thresholds.slots[i] ) { return false; }
    }
    if ( a.reserves_count != b.reserves_count ) { return false; }
    for ( int32_t i = 0; i < a.reserves_count; i++ )
    {
        if ( !same_ship( a.reserves[i], b.reserves[i] ) ) { return false; }
    }
    return true;
}

// The RootConfig instance the tree at tables/pack/root declares.
static void build_root( RootConfig & root )
{
    set_string( root.version_note, root.version_note_length, "corpus v1" );

    WeaponConfig & cannon = root.weapons[0];
    cannon.damage = 42.5f;
    cannon.speed = 750.0f;
    cannon.penetration = 7;
    cannon.channel = 33;
    cannon.homing = true;
    cannon.effect.type = EffectType::Buff;
    cannon.effect.buff.multiplier = 2.5f;

    WeaponConfig & mine = root.weapons[1];
    mine.damage = 21.0f;
    mine.channel = 63; // bits(6) at its implied maximum: the boundary the
                       // text form clamps against on the way in
    mine.effect.type = EffectType::Debuff;
    mine.effect.debuff.amount = 15;
    root.weapons_count = 2;

    ProfileConfig & ace = root.profiles[0];
    set_string( ace.name, ace.name_length, "Ace" );
    for ( int32_t i = 0; i < 16; i++ ) { ace.icon[i] = (uint8_t) i; }
    ace.icon_length = 16;
    ace.experience = 4000000000u;
    ace.tilt = -7;
    ace.heading = -1200;
    ace.timestamp = -9007199254740993ll;
    ace.badge = 200;
    ace.port = 40000;
    ace.epoch = 18446744073709551615ull;
    ace.precision = 0.1;
    ace.ratings[0] = 1.5f;
    ace.ratings[1] = 2.5f;
    ace.ratings[2] = 3.5f;
    ace.ratings[3] = 4.5f;
    ace.has_loadout = true;
    ace.loadout.grade = Grade::Gold;
    ace.loadout.grades[0] = Grade::Bronze;
    ace.loadout.grades[1] = Grade::Gold;
    ace.loadout.grades_count = 2;
    ace.loadout.podium[0] = Grade::Gold;
    ace.loadout.podium[1] = Grade::Silver;
    ace.loadout.podium[2] = Grade::Bronze;
    ace.loadout.perks = (Perks) ( ( 1u << 0 ) | ( 1u << 2 ) ); // Shielded, Turbo
    ace.loadout.primary.damage = 9.5f;
    ace.loadout.primary.channel = 12;
    ace.loadout.backups[0].damage = 1.0f;
    ace.loadout.backups[1].speed = 2.0f;
    ace.loadout.attachments[0].slot = 3;
    ace.loadout.attachments[0].power = 2.5f;
    ace.loadout.attachments_count = 1;

    ProfileConfig & rookie = root.profiles[1];
    set_string( rookie.name, rookie.name_length, "Rookie" );
    rookie.has_loadout = false;
    root.profiles_count = 2;
}

static uint8_t * slurp( const char * path, long & size )
{
    FILE * file = fopen( path, "rb" );
    if ( file == NULL ) { return NULL; }
    fseek( file, 0, SEEK_END );
    size = ftell( file );
    fseek( file, 0, SEEK_SET );
    uint8_t * bytes = (uint8_t *) malloc( (size_t) size );
    if ( bytes == NULL || fread( bytes, 1, (size_t) size, file ) != (size_t) size )
    {
        fclose( file );
        free( bytes );
        return NULL;
    }
    fclose( file );
    return bytes;
}

// compare_bytes reports the first byte a pack and a Save disagree on, which is
// the diagnostic a framing divergence needs.
static void compare_bytes( const char * what, const uint8_t * packed, long packed_size,
                           const uint8_t * saved, int64_t size )
{
    if ( size != packed_size )
    {
        printf( "FAIL %s: schema pack wrote %ld bytes, Save wrote %lld\n", what, packed_size, (long long) size );
        failures++;
        return;
    }
    for ( long i = 0; i < packed_size; i++ )
    {
        if ( saved[i] != packed[i] )
        {
            printf( "FAIL %s: first byte difference at %ld: pack 0x%02x, Save 0x%02x\n",
                    what, i, packed[i], saved[i] );
            failures++;
            return;
        }
    }
}

// compare_text is §17.1's third golden for one root: the engine's text and the
// backend's, byte for byte.
template <typename T>
static void compare_text( const char * what, const T & value, const char * path,
                          int64_t ( *to_json_measure )( const T & ),
                          int64_t ( *to_json )( const T &, char *, int64_t ) )
{
    long size = 0;
    uint8_t * expected = slurp( path, size );
    if ( expected == NULL )
    {
        printf( "FAIL %s: cannot read %s\n", what, path );
        failures++;
        return;
    }
    // schema unpack ends the file with a newline; ToJson writes the text alone
    while ( size > 0 && expected[size - 1] == '\n' ) { size--; }

    int64_t measured = to_json_measure( value );
    if ( measured < 0 )
    {
        printf( "FAIL %s: ToJsonMeasure refused the instance\n", what );
        failures++;
        free( expected );
        return;
    }
    char * text = (char *) malloc( (size_t) measured + 1 );
    if ( to_json( value, text, measured ) != measured )
    {
        printf( "FAIL %s: ToJson disagreed with ToJsonMeasure\n", what );
        failures++;
        free( text );
        free( expected );
        return;
    }
    text[measured] = 0;
    if ( measured != size )
    {
        printf( "FAIL %s: schema unpack wrote %ld text bytes, ToJson wrote %lld\n",
                what, size, (long long) measured );
        failures++;
    }
    else if ( memcmp( text, expected, (size_t) size ) != 0 )
    {
        for ( long i = 0; i < size; i++ )
        {
            if ( (uint8_t) text[i] != expected[i] )
            {
                printf( "FAIL %s: the two texts differ at %ld: unpack 0x%02x, ToJson 0x%02x\n",
                        what, i, expected[i], (uint8_t) text[i] );
                break;
            }
        }
        failures++;
    }
    free( text );
    free( expected );
}

static void root_golden( const char * path, const char * text_path )
{
    long packed_size = 0;
    uint8_t * packed = slurp( path, packed_size );
    if ( packed == NULL )
    {
        printf( "FAIL: cannot read %s\n", path );
        failures++;
        return;
    }
    RootConfig root;
    build_root( root );
    int64_t size = RootConfigMeasure( root );
    CHECK( size > 0 );
    uint8_t * saved = (uint8_t *) malloc( (size_t) size );
    CHECK( RootConfigSave( root, saved, size ) == size );
    compare_bytes( "RootConfig", packed, packed_size, saved, size );

    RootConfig loaded;
    TableReport report;
    CHECK( RootConfigLoad( loaded, packed, packed_size, &report ) );
    CHECK( report.unknown == 0 );
    CHECK( report.kind_mismatch == 0 );
    CHECK( report.clamped == 0 );
    CHECK( report.duplicate == 0 );
    CHECK( !report.malformed );

    // the value that comes back saves to the same bytes, which is the whole
    // of "the instance survived the trip" without a hand-written comparator
    int64_t again = RootConfigMeasure( loaded );
    CHECK( again == size );
    uint8_t * resaved = (uint8_t *) malloc( (size_t) again );
    CHECK( RootConfigSave( loaded, resaved, again ) == again );
    compare_bytes( "RootConfig reload", packed, packed_size, resaved, again );

    compare_text<RootConfig>( "RootConfig text", loaded, text_path,
                              RootConfigToJsonMeasure, RootConfigToJson );

    free( resaved );
    free( saved );
    free( packed );
}

int main( int argc, char ** argv )
{
    if ( argc < 5 )
    {
        printf( "usage: %s <PackConfig.bin> <RootConfig.bin> <PackConfig.json> <RootConfig.json>\n", argv[0] );
        return 2;
    }
    long packed_size = 0;
    uint8_t * packed = slurp( argv[1], packed_size );
    if ( packed == NULL )
    {
        printf( "FAIL: cannot read %s\n", argv[1] );
        return 1;
    }

    PackConfig config;
    build( config );

    // 1. the same instance, saved by the generated codec
    int64_t size = PackConfigMeasure( config );
    CHECK( size > 0 );
    uint8_t * saved = (uint8_t *) malloc( (size_t) size );
    CHECK( PackConfigSave( config, saved, size ) == size );

    compare_bytes( "PackConfig", packed, packed_size, saved, size );

    // 2. this build reads what the Go engine wrote, with nothing to report
    PackConfig loaded;
    TableReport report;
    CHECK( PackConfigLoad( loaded, packed, packed_size, &report ) );
    CHECK( report.unknown == 0 );
    CHECK( report.kind_mismatch == 0 );
    CHECK( report.clamped == 0 );
    CHECK( report.duplicate == 0 );
    CHECK( !report.malformed );

    // 3. and the value that comes back is the one that went in
    CHECK( same_config( config, loaded ) );

    // 4. and the text the engine wrote for it is the text this build writes
    compare_text<PackConfig>( "PackConfig text", loaded, argv[3],
                              PackConfigToJsonMeasure, PackConfigToJson );

    free( saved );
    free( packed );

    root_golden( argv[2], argv[4] );

    if ( failures == 0 )
    {
        printf( "pack golden: schema pack == Save and schema unpack == ToJson for both roots\n" );
        return 0;
    }
    printf( "pack golden: %d failure(s)\n", failures );
    return 1;
}
