// The TABLE-wire conformance test (SPEC-TABLES.md). Three generated units in
// one binary: the tables corpus (tabledemo), and the two-generation evolution
// pair (tblv1/tblv2) whose schemas disagree on purpose. Compiled WITHOUT the
// serialize include path — the Table headers must stand alone.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <thread>
#include <vector>

#include "TablesTable.h"
#include "KeyedTable.h"
#include "NestedTable.h"
#include "WideTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "GraphTable.h"
#include "PartsTable.h"
#include "MarksTable.h"
#include "P1Table.h"
#include "P2Table.h"
#include "P3Table.h"

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

// an independent implementation of the table-wire field id —
// fold16(fnv1a32(name)), 0 rebounds to 1 — pinning the compiler's hash
// against a second implementation written from the spec alone
static uint16_t field_id( const char * name )
{
    uint32_t h = 0x811C9DC5u;
    for ( const char * p = name; *p; ++p )
    {
        h ^= (uint32_t) (uint8_t) *p;
        h *= 0x01000193u;
    }
    uint16_t id = (uint16_t) ( ( h ^ ( h >> 16 ) ) & 0xFFFF );
    return id == 0 ? 1 : id;
}

static const tabledemo::TableFieldInfo * demo_field( const tabledemo::TableTypeInfo * type, const char * name )
{
    for ( int32_t i = 0; i < type->num_fields; i++ )
        if ( strcmp( type->fields[i].name, name ) == 0 )
            return &type->fields[i];
    return NULL;
}

static const tblv1::TableFieldInfo * v1_field( const tblv1::TableTypeInfo * type, const char * name )
{
    for ( int32_t i = 0; i < type->num_fields; i++ )
        if ( strcmp( type->fields[i].name, name ) == 0 )
            return &type->fields[i];
    return NULL;
}

static void set_string( char * dest, int32_t & length, const char * s )
{
    length = (int32_t) strlen( s );
    memcpy( dest, s, length + 1 );
}

static void le16( uint8_t * p, uint16_t v ) { p[0] = (uint8_t) v; p[1] = (uint8_t) ( v >> 8 ); }
static void le32( uint8_t * p, uint32_t v ) { p[0] = (uint8_t) v; p[1] = (uint8_t) ( v >> 8 ); p[2] = (uint8_t) ( v >> 16 ); p[3] = (uint8_t) ( v >> 24 ); }

// check_exact_capacity is the go-wide guarantee: <X>Save into a buffer of
// EXACTLY <X>Measure's size succeeds and byte-matches a roomy save — a
// scatter-writing worker gets exactly sizes[i] and nothing more.
template <typename T>
static void check_exact_capacity( const T & value,
                                  int64_t (*measure)( const T & ),
                                  int64_t (*save)( const T &, uint8_t *, int64_t ) )
{
    static uint8_t roomy[65536];
    static uint8_t exact[65536];
    int64_t need = measure( value );
    CHECK( need >= 2 && need <= (int64_t) sizeof( roomy ) );
    CHECK( save( value, roomy, sizeof( roomy ) ) == need );
    CHECK( save( value, exact, need ) == need ); // exactly measure's answer
    CHECK( memcmp( roomy, exact, (size_t) need ) == 0 );
    if ( need > 2 )
    {
        CHECK( save( value, exact, need - 1 ) == -1 ); // one byte short refuses
    }
}

// ---- round trip: the corpus root, every kind populated ----

static void test_round_trip()
{
    tabledemo::RootConfig root;
    set_string( root.version_note, root.version_note_length, "v-one" );

    root.weapons_count = 2;
    root.weapons[0].damage = 40.0f;           // non-default: rides
    root.weapons[0].channel = 45;             // bits(6)
    root.weapons[0].homing = true;
    root.weapons[0].effect.type = tabledemo::EffectType::Buff;
    root.weapons[0].effect.buff = tabledemo::Buff{};
    root.weapons[0].effect.buff.multiplier = 3.0f;
    // weapons[1] stays all-default: a counted element still rides (length-prefixed)

    root.profiles_count = 1;
    tabledemo::ProfileConfig & p = root.profiles[0];
    set_string( p.name, p.name_length, "player one" );
    p.icon[0] = 1; p.icon[1] = 2; p.icon[2] = 250; p.icon_length = 3;
    p.experience = 777;
    p.tilt = -12;                 // i8
    p.heading = -30000;           // i16
    p.timestamp = -5000000000ll;  // i64
    p.badge = 200;                // u8
    p.port = 40000;               // u16
    p.epoch = 0x1122334455667788ull; // u64
    p.precision = 2.5;            // f64
    p.ratings[2] = 0.5f;
    p.has_loadout = true;
    p.loadout.grade = tabledemo::Grade::Gold;
    p.loadout.perks = tabledemo::Perks_Cloaked;
    p.loadout.primary.penetration = 7;
    p.loadout.backups[0].damage = 1.0f;
    p.loadout.attachments_count = 1;
    p.loadout.attachments[0].slot = 3;
    p.loadout.attachments[0].power = 2.0f;

    uint8_t buffer[16384];
    int64_t need = tabledemo::RootConfigMeasure( root );
    int64_t wrote = tabledemo::RootConfigSave( root, buffer, sizeof( buffer ) );
    CHECK( wrote > 0 );
    CHECK( need == wrote ); // measure/save split: measure is EXACT

    tabledemo::TableReport report;
    tabledemo::RootConfig out;
    CHECK( tabledemo::RootConfigLoad( out, buffer, wrote, &report ) );
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && !report.malformed );

    CHECK( strcmp( out.version_note, "v-one" ) == 0 && out.version_note_length == 5 );
    CHECK( out.weapons_count == 2 );
    CHECK( out.weapons[0].damage == 40.0f );
    CHECK( out.weapons[0].speed == 500.0f ); // default survived elision
    CHECK( out.weapons[0].channel == 45 );
    CHECK( out.weapons[0].homing == true );
    CHECK( out.weapons[0].effect.type == tabledemo::EffectType::Buff );
    CHECK( out.weapons[0].effect.buff.multiplier == 3.0f );
    CHECK( out.weapons[1].damage == 21.0f && out.weapons[1].effect.type == tabledemo::EffectType::None );
    CHECK( out.profiles_count == 1 );
    CHECK( strcmp( out.profiles[0].name, "player one" ) == 0 );
    CHECK( out.profiles[0].icon_length == 3 && out.profiles[0].icon[2] == 250 );
    CHECK( out.profiles[0].experience == 777 );
    CHECK( out.profiles[0].tilt == -12 );
    CHECK( out.profiles[0].heading == -30000 );
    CHECK( out.profiles[0].timestamp == -5000000000ll );
    CHECK( out.profiles[0].badge == 200 );
    CHECK( out.profiles[0].port == 40000 );
    CHECK( out.profiles[0].epoch == 0x1122334455667788ull );
    CHECK( out.profiles[0].precision == 2.5 );
    CHECK( out.profiles[0].ratings[2] == 0.5f && out.profiles[0].ratings[0] == 0.0f );
    CHECK( out.profiles[0].has_loadout == true );
    CHECK( out.profiles[0].loadout.grade == tabledemo::Grade::Gold );
    CHECK( out.profiles[0].loadout.perks == tabledemo::Perks_Cloaked );
    CHECK( out.profiles[0].loadout.primary.penetration == 7 );
    CHECK( out.profiles[0].loadout.backups[0].damage == 1.0f );
    CHECK( out.profiles[0].loadout.backups[1].damage == 21.0f );
    CHECK( out.profiles[0].loadout.attachments_count == 1 );
    CHECK( out.profiles[0].loadout.attachments[0].slot == 3 );
    CHECK( out.profiles[0].loadout.attachments[0].power == 2.0f );

    // relocatable: memcpy the decoded value and read the copy
    static tabledemo::RootConfig moved;
    memcpy( (void *) &moved, (const void *) &out, sizeof( out ) );
    CHECK( moved.profiles[0].experience == 777 );
    CHECK( strcmp( moved.profiles[0].name, "player one" ) == 0 );

    // a too-small buffer refuses instead of truncating
    uint8_t tiny[8];
    CHECK( tabledemo::RootConfigSave( root, tiny, sizeof( tiny ) ) == -1 );

    // the exact-capacity guarantee holds for the fully-populated root
    check_exact_capacity( root, tabledemo::RootConfigMeasure, tabledemo::RootConfigSave );
}

// ---- exact capacity: measure's answer IS the buffer size, corpus-wide ----

static void test_exact_capacity()
{
    // the reviewer's repro shape: non-default fields around an ALL-DEFAULT
    // nested table — the elided field must not touch the buffer at all, so
    // saving into exactly measure's size succeeds
    tblv1::Cfg cfg;
    cfg.a = 9;
    cfg.b = 8.5f;
    set_string( cfg.name, cfg.name_length, "exact" );
    // cfg.inner stays all-default: elides
    check_exact_capacity( cfg, tblv1::CfgMeasure, tblv1::CfgSave );

    // all-default everything (2 bytes)
    tblv1::Cfg empty;
    check_exact_capacity( empty, tblv1::CfgMeasure, tblv1::CfgSave );
    tabledemo::WeaponConfig weapon;
    check_exact_capacity( weapon, tabledemo::WeaponConfigMeasure, tabledemo::WeaponConfigSave );

    // an elided nested table inside a GUARDED group
    tabledemo::ProfileConfig profile;
    profile.experience = 1;
    profile.has_loadout = true; // loadout itself stays all-default: elides
    check_exact_capacity( profile, tabledemo::ProfileConfigMeasure, tabledemo::ProfileConfigSave );

    // nested tables of nested tables, some elided, some riding
    static tabledemo::ArchiveConfig archive;
    archive.count = 9;
    archive.root.weapons_count = 1; // weapons[0] all-default: rides as an element, its nested union elides
    check_exact_capacity( archive, tabledemo::ArchiveConfigMeasure, tabledemo::ArchiveConfigSave );

    // loadout: fixed array of tables (always rides) around all-default elements
    tabledemo::LoadoutConfig loadout;
    loadout.grade = tabledemo::Grade::Gold;
    check_exact_capacity( loadout, tabledemo::LoadoutConfigMeasure, tabledemo::LoadoutConfigSave );

    // enum ARRAYS, both shapes: a counted one and a fixed one, each riding
    // u16 variant hashes per element (SPEC-TABLES.md §3)
    tabledemo::LoadoutConfig enums;
    enums.grades_count = 3;
    enums.grades[0] = tabledemo::Grade::Bronze;
    enums.grades[1] = tabledemo::Grade::None;   // None is a legal element: it rides as 0
    enums.grades[2] = tabledemo::Grade::Gold;
    enums.podium[0] = tabledemo::Grade::Gold;
    enums.podium[1] = tabledemo::Grade::Silver;
    enums.podium[2] = tabledemo::Grade::Bronze;
    check_exact_capacity( enums, tabledemo::LoadoutConfigMeasure, tabledemo::LoadoutConfigSave );

    // the v2 evolution shape
    tblv2::Cfg v2;
    v2.a = 1.0f;
    v2.inner.gain = 2.0f;
    check_exact_capacity( v2, tblv2::CfgMeasure, tblv2::CfgSave );
}

// ---- storage invariants: the write side validates what it reads ----

static void test_storage_invariants()
{
    uint8_t buffer[4096];

    // a count above the bound must not read out of the array into the wire
    tabledemo::RootConfig root;
    root.weapons_count = 9; // bound is 8
    CHECK( tabledemo::RootConfigMeasure( root ) == -1 );
    CHECK( tabledemo::RootConfigSave( root, buffer, sizeof( buffer ) ) == -1 );

    // a negative length must not memcpy a huge size_t
    tblv1::Cfg cfg;
    cfg.name_length = -1;
    CHECK( tblv1::CfgMeasure( cfg ) == -1 );
    CHECK( tblv1::CfgSave( cfg, buffer, sizeof( buffer ) ) == -1 );

    // a negative count refuses too
    tblv1::Cfg cfg2;
    cfg2.items_count = -3;
    CHECK( tblv1::CfgMeasure( cfg2 ) == -1 );
    CHECK( tblv1::CfgSave( cfg2, buffer, sizeof( buffer ) ) == -1 );

    // a violation deep inside a nested, guarded table propagates up
    tabledemo::ProfileConfig profile;
    profile.has_loadout = true;
    profile.loadout.attachments_count = -5;
    CHECK( tabledemo::ProfileConfigMeasure( profile ) == -1 );
    CHECK( tabledemo::ProfileConfigSave( profile, buffer, sizeof( buffer ) ) == -1 );

    // an out-of-range union tag refuses in measure exactly as in write
    tabledemo::WeaponConfig weapon;
    weapon.effect.type = (tabledemo::EffectType) 9;
    CHECK( tabledemo::WeaponConfigMeasure( weapon ) == -1 );
    CHECK( tabledemo::WeaponConfigSave( weapon, buffer, sizeof( buffer ) ) == -1 );
}

// ---- bounded elements: a count the body length cannot cover never reads
// ---- the following fields' bytes (SPEC-TABLES.md: skipped, NEVER misdecoded)

static void test_bounded_elements()
{
    const tblv1::TableFieldInfo * items = v1_field( tblv1::CfgTableType(), "items" );
    const tblv1::TableFieldInfo * a = v1_field( tblv1::CfgTableType(), "a" );
    CHECK( items != NULL && a != NULL );

    // body_len = 5 covers only elem_kind + count; count claims 2 elements —
    // the elements would have to come from the NEXT field's bytes. They must
    // not: prefix kept (empty), malformed flagged, and a = 42 still decodes.
    uint8_t wire[32];
    int n = 0;
    le16( wire + n, items->id ); n += 2;
    wire[n++] = 14;              // kArray
    le32( wire + n, 5 ); n += 4; // body_len: header only, no element bytes
    wire[n++] = 4;               // elem_kind kI32
    le32( wire + n, 2 ); n += 4; // count 2 — a lie
    le16( wire + n, a->id ); n += 2;
    wire[n++] = 4;               // kI32
    le32( wire + n, 42 ); n += 4;
    le16( wire + n, 0 ); n += 2; // terminator

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, n, &report ) );
    CHECK( report.malformed );        // the lie is framing damage, flagged
    CHECK( out.items_count == 0 );    // no fabricated elements
    CHECK( out.a == 42 );             // the parent continued at the next field

    // a body covering one full element plus slack: the decoded PREFIX is kept
    n = 0;
    le16( wire + n, items->id ); n += 2;
    wire[n++] = 14;
    le32( wire + n, 5 + 4 + 2 ); n += 4; // one i32 element + 2 slack bytes
    wire[n++] = 4;
    le32( wire + n, 2 ); n += 4;         // count 2, body holds 1.5
    le32( wire + n, 10 ); n += 4;        // element 0
    wire[n++] = 0; wire[n++] = 0;        // the half element
    le16( wire + n, a->id ); n += 2;
    wire[n++] = 4;
    le32( wire + n, 42 ); n += 4;
    le16( wire + n, 0 ); n += 2;

    tblv1::TableReport report2;
    tblv1::Cfg out2;
    CHECK( tblv1::CfgLoad( out2, wire, n, &report2 ) );
    CHECK( report2.malformed );
    CHECK( out2.items_count == 1 && out2.items[0] == 10 ); // bounded prefix
    CHECK( out2.a == 42 );
}

// ---- all-default: everything elides, decode restores every default ----

static void test_all_default()
{
    tabledemo::WeaponConfig weapon;
    uint8_t buffer[64];
    int64_t wrote = tabledemo::WeaponConfigSave( weapon, buffer, sizeof( buffer ) );
    CHECK( wrote == 2 ); // bare terminator
    CHECK( tabledemo::WeaponConfigMeasure( weapon ) == 2 );

    tabledemo::TableReport report;
    tabledemo::WeaponConfig out;
    out.damage = -1.0f; // garbage that the prefill must erase
    CHECK( tabledemo::WeaponConfigLoad( out, buffer, wrote, &report ) );
    CHECK( out.damage == 21.0f && out.speed == 500.0f && out.penetration == 1 );
    CHECK( !report.malformed && report.unknown == 0 );
}

// ---- guarded fields stay off the wire when the guard says so ----

static void test_guard()
{
    tabledemo::ProfileConfig p;
    p.has_loadout = false;
    p.loadout.grade = tabledemo::Grade::Gold; // junk behind an untaken guard

    uint8_t buffer[512];
    int64_t wrote = tabledemo::ProfileConfigSave( p, buffer, sizeof( buffer ) );
    CHECK( wrote == 2 ); // guard false + everything else default: all elides
    CHECK( tabledemo::ProfileConfigMeasure( p ) == wrote );

    tabledemo::TableReport report;
    tabledemo::ProfileConfig out;
    CHECK( tabledemo::ProfileConfigLoad( out, buffer, wrote, &report ) );
    CHECK( out.loadout.grade == tabledemo::Grade::Silver ); // untaken side decodes to defaults
}

// ---- evolution, both directions (SPEC-TABLES.md: any reader x any data) ----

static void test_evolution_old_reader_new_data()
{
    tblv2::Cfg v2;
    v2.a = 7.5f;
    v2.c = false; // non-default, rides
    v2.mode = tblv2::Mode::Alpha;
    set_string( v2.title, v2.title_length, "fresh" );
    v2.inner.factor = 9.5f;
    v2.inner.gain = 4.0f;
    v2.items[0] = 10;
    v2.items_count = 1;

    uint8_t wire[1024];
    int64_t bytes = tblv2::CfgSave( v2, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv2::CfgMeasure( v2 ) );

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed );
    CHECK( report.unknown == 2 );       // c (top level) + gain (nested)
    CHECK( report.kind_mismatch == 1 ); // a: f32 wire vs v1's i32 expectation
    CHECK( out.a == 5 );                // a skipped -> v1 default, never misdecoded
    CHECK( out.b == 1.5f );             // removed in v2 -> absent -> v1 default
    CHECK( strcmp( out.name, "fresh" ) == 0 ); // v2 title carries was = "name": identity survived the rename
    CHECK( out.mode == tblv1::Mode::Alpha );
    CHECK( out.inner.factor == 9.5f );
    CHECK( out.items_count == 1 && out.items[0] == 10 );
}

static void test_evolution_new_reader_old_data()
{
    tblv1::Cfg v1;
    v1.a = 9;
    v1.b = 8.5f;
    set_string( v1.name, v1.name_length, "aged" );
    v1.inner.factor = 1.25f;

    uint8_t wire[1024];
    int64_t bytes = tblv1::CfgSave( v1, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( v1 ) );

    tblv2::TableReport report;
    tblv2::Cfg out;
    CHECK( tblv2::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed );
    CHECK( report.unknown == 1 );       // b, removed in v2
    CHECK( report.kind_mismatch == 1 ); // a: i32 wire vs v2's f32 expectation
    CHECK( out.a == 5.0f );             // v2 default
    CHECK( out.c == true );             // added in v2, absent in old data -> default
    CHECK( strcmp( out.title, "aged" ) == 0 ); // old "name" data lands in the renamed field
    CHECK( out.mode == tblv2::Mode::Beta );
    CHECK( out.inner.factor == 1.25f );
    CHECK( out.inner.gain == 1.0f );    // nested added field defaults
}

// ---- a variant inserted IN THE MIDDLE: identity is the NAME, not the ordinal
// ---- (SPEC-TABLES.md §5). V2 inserts Silver between Bronze and Gold, so Gold
// ---- slides from ordinal 2 to 3 — and every stored Gold still reads as Gold.

static void test_evolution_enum_insert_old_data()
{
    tblv1::Cfg v1;
    v1.grade = tblv1::Grade::Gold; // ordinal 2 in V1, ordinal 3 in V2

    uint8_t wire[1024];
    int64_t bytes = tblv1::CfgSave( v1, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( v1 ) );

    // the value on the wire is the NAME hash, not an ordinal — pinned against
    // the independent id implementation above
    const tblv1::TableFieldInfo * grade = v1_field( tblv1::CfgTableType(), "grade" );
    CHECK( grade != NULL && grade->kind == 7 ); // kU16: every enum, every width
    CHECK( grade->variant_id != NULL && grade->variant_id( 2 ) == field_id( "Gold" ) );

    tblv2::TableReport report;
    tblv2::Cfg out;
    CHECK( tblv2::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed && report.unknown == 0 && report.clamped == 0 );
    CHECK( out.grade == tblv2::Grade::Gold ); // NOT Silver, which holds ordinal 2 in V2

    // and the same inside an enum ARRAY, both shapes: elements carry variant
    // hashes one by one, so a middle insert leaves every stored element alone
    tblv1::Cfg arrays;
    arrays.grades_count = 2;
    arrays.grades[0] = tblv1::Grade::Gold;
    arrays.grades[1] = tblv1::Grade::Bronze;
    arrays.podium[0] = tblv1::Grade::Bronze;
    arrays.podium[1] = tblv1::Grade::Gold;
    arrays.podium[2] = tblv1::Grade::None;
    bytes = tblv1::CfgSave( arrays, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( arrays ) );

    tblv2::TableReport report2;
    tblv2::Cfg out2;
    CHECK( tblv2::CfgLoad( out2, wire, bytes, &report2 ) );
    CHECK( !report2.malformed && report2.unknown == 0 && report2.clamped == 0 );
    CHECK( out2.grades_count == 2 );
    CHECK( out2.grades[0] == tblv2::Grade::Gold && out2.grades[1] == tblv2::Grade::Bronze );
    CHECK( out2.podium[0] == tblv2::Grade::Bronze );
    CHECK( out2.podium[1] == tblv2::Grade::Gold );
    CHECK( out2.podium[2] == tblv2::Grade::None );
}

static void test_evolution_enum_insert_new_data()
{
    tblv2::Cfg v2;
    v2.grade = tblv2::Grade::Silver; // V1 has no name for it at all

    uint8_t wire[1024];
    int64_t bytes = tblv2::CfgSave( v2, wire, sizeof( wire ) );
    CHECK( bytes > 0 );

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed );
    CHECK( report.unknown == 1 );                 // an id this reader cannot name
    CHECK( out.grade == tblv1::Grade::None );     // never a neighbour's variant

    // a variant V1 DOES know, whose ordinal moved: it lands correctly
    tblv2::Cfg gold;
    gold.grade = tblv2::Grade::Gold; // ordinal 3 in V2, 2 in V1
    bytes = tblv2::CfgSave( gold, wire, sizeof( wire ) );
    tblv1::TableReport report2;
    tblv1::Cfg out2;
    CHECK( tblv1::CfgLoad( out2, wire, bytes, &report2 ) );
    CHECK( !report2.malformed && report2.unknown == 0 );
    CHECK( out2.grade == tblv1::Grade::Gold );

    // the array direction: one element V1 knows, one it does not
    tblv2::Cfg arrays;
    arrays.grades_count = 2;
    arrays.grades[0] = tblv2::Grade::Gold;   // V1 names it
    arrays.grades[1] = tblv2::Grade::Silver; // V1 does not
    bytes = tblv2::CfgSave( arrays, wire, sizeof( wire ) );
    tblv1::TableReport report3;
    tblv1::Cfg out3;
    CHECK( tblv1::CfgLoad( out3, wire, bytes, &report3 ) );
    CHECK( !report3.malformed && report3.unknown == 1 );
    CHECK( out3.grades_count == 2 );
    CHECK( out3.grades[0] == tblv1::Grade::Gold );
    CHECK( out3.grades[1] == tblv1::Grade::None );
}

static void test_evolution_union_insert_old_data()
{
    tblv1::Cfg v1;
    v1.effect.type = tblv1::EffectType::Ward; // tag 2 in V1, tag 3 in V2
    v1.effect.ward = tblv1::Ward{};
    v1.effect.ward.charge = 7.5f;

    uint8_t wire[1024];
    int64_t bytes = tblv1::CfgSave( v1, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( v1 ) );

    const tblv1::TableFieldInfo * effect = v1_field( tblv1::CfgTableType(), "effect" );
    CHECK( effect != NULL && effect->kind == 15 && effect->enum_max == 2 );
    CHECK( effect->variant_id != NULL && effect->variant_id( 2 ) == field_id( "ward" ) );
    CHECK( effect->enum_name != NULL && strcmp( effect->enum_name( 2 ), "ward" ) == 0 );

    tblv2::TableReport report;
    tblv2::Cfg out;
    CHECK( tblv2::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed && report.unknown == 0 );
    CHECK( out.effect.type == tblv2::EffectType::Ward ); // NOT hex, which holds tag 2 in V2
    CHECK( out.effect.ward.charge == 7.5f );
}

static void test_evolution_union_insert_new_data()
{
    tblv2::Cfg v2;
    v2.effect.type = tblv2::EffectType::Hex; // V1 has no name for this arm
    v2.effect.hex = tblv2::Hex{};
    v2.effect.hex.level = 4;

    uint8_t wire[1024];
    int64_t bytes = tblv2::CfgSave( v2, wire, sizeof( wire ) );
    CHECK( bytes > 0 );

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed );
    CHECK( report.unknown == 1 );                          // an arm id V1 cannot name
    CHECK( out.effect.type == tblv1::EffectType::None );   // empty, never a neighbour's arm

    // an arm V1 DOES know, whose tag moved: it lands correctly
    tblv2::Cfg ward;
    ward.effect.type = tblv2::EffectType::Ward; // tag 3 in V2, 2 in V1
    ward.effect.ward = tblv2::Ward{};
    ward.effect.ward.charge = -2.0f;
    bytes = tblv2::CfgSave( ward, wire, sizeof( wire ) );
    tblv1::TableReport report2;
    tblv1::Cfg out2;
    CHECK( tblv1::CfgLoad( out2, wire, bytes, &report2 ) );
    CHECK( !report2.malformed && report2.unknown == 0 );
    CHECK( out2.effect.type == tblv1::EffectType::Ward && out2.effect.ward.charge == -2.0f );
}

// ---- hostile: a REPEATED field id whose second occurrence names an arm or a
// ---- variant this build cannot name. "Reads as empty" (SPEC-TABLES.md §4) is
// ---- a value the reader must WRITE, not one the prefill happens to leave —
// ---- an earlier occurrence of the same id may have decoded a real arm.

static void test_repeated_id_unnameable_variant()
{
    const tblv1::TableFieldInfo * effect = v1_field( tblv1::CfgTableType(), "effect" );
    const tblv1::TableFieldInfo * grade = v1_field( tblv1::CfgTableType(), "grade" );
    CHECK( effect != NULL && grade != NULL );

    // occurrence one is a real ward arm, written by the generator itself
    tblv1::Cfg src;
    src.effect.type = tblv1::EffectType::Ward;
    src.effect.ward = tblv1::Ward{};
    src.effect.ward.charge = 0.5f;
    src.grade = tblv1::Grade::Gold;

    uint8_t wire[512];
    int64_t saved = tblv1::CfgSave( src, wire, sizeof( wire ) );
    CHECK( saved > 2 );

    // occurrence two, spliced over the terminator: the same ids, an arm id and
    // a variant id no build names
    int n = (int) ( saved - 2 );
    le16( wire + n, effect->id ); n += 2;
    wire[n++] = 15;                  // kUnion
    le16( wire + n, 0xBEEF ); n += 2; // an arm id this reader cannot name
    le32( wire + n, 2 ); n += 4;
    le16( wire + n, 0 ); n += 2;     // the arm body: a bare terminator
    le16( wire + n, grade->id ); n += 2;
    wire[n++] = 7;                   // kU16
    le16( wire + n, 0xBEEF ); n += 2; // a variant id this reader cannot name
    le16( wire + n, 0 ); n += 2;     // the table terminator

    tblv1::TableReport report;
    tblv1::Cfg out;
    out.effect.type = tblv1::EffectType::Boost; // junk the prefill must erase
    CHECK( tblv1::CfgLoad( out, wire, n, &report ) );
    CHECK( !report.malformed );
    CHECK( report.unknown == 2 ); // one arm id, one variant id
    // both vocabularies land on the SAME answer: the reader's empty value,
    // never the arm or variant an earlier occurrence decoded
    CHECK( out.effect.type == tblv1::EffectType::None );
    CHECK( out.grade == tblv1::Grade::None );
}

// ---- an array's BOUND is not wire identity (SPEC-TABLES.md §4). V2 shrinks
// ---- MaxSlots 6 -> 3, and tally is sized [Grade.Max + 1], which GROWS 3 -> 4
// ---- when Grade gains a variant. The storage struct changes size; the table
// ---- on the wire does not, because identity is the field name hash and kind.

static void test_evolution_array_bounds()
{
    // WRITER'S BOUND LARGER: the count exceeds the reader's, so the reader
    // keeps the bounded PREFIX and counts clamped — not malformed, which is
    // reserved for a count the BODY cannot cover
    tblv1::Cfg wide;
    wide.slots_count = 6;                                    // v2's bound is 3
    for ( int32_t i = 0; i < 6; i++ ) wide.slots[i] = 100 + i;
    for ( int32_t i = 0; i < 3; i++ ) wide.tally[i] = 10 + i; // v2's tally is [4]

    uint8_t wire[1024];
    int64_t bytes = tblv1::CfgSave( wide, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( wide ) );

    tblv2::TableReport report;
    tblv2::Cfg out;
    CHECK( tblv2::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed );          // a shrunk bound is not framing damage
    CHECK( report.clamped == 1 );        // slots: 6 offered, 3 kept
    CHECK( out.slots_count == 3 );
    CHECK( out.slots[0] == 100 && out.slots[1] == 101 && out.slots[2] == 102 );
    // tally GREW in v2: the count is short of the bound, so the tail defaults
    CHECK( out.tally[0] == 10 && out.tally[1] == 11 && out.tally[2] == 12 );
    CHECK( out.tally[3] == 0 );

    // WRITER'S BOUND SMALLER, the other direction: nothing clamps, and the
    // reader's extra capacity holds its declared defaults
    tblv2::Cfg narrow;
    narrow.slots_count = 3;
    for ( int32_t i = 0; i < 3; i++ ) narrow.slots[i] = 200 + i;
    for ( int32_t i = 0; i < 4; i++ ) narrow.tally[i] = 20 + i; // v1's tally is [3]

    bytes = tblv2::CfgSave( narrow, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv2::CfgMeasure( narrow ) );

    tblv1::TableReport report2;
    tblv1::Cfg out2;
    CHECK( tblv1::CfgLoad( out2, wire, bytes, &report2 ) );
    CHECK( !report2.malformed );
    CHECK( report2.clamped == 1 );       // tally: 4 offered, v1 keeps 3
    CHECK( out2.slots_count == 3 );      // under v1's bound of 6: no clamp
    CHECK( out2.slots[0] == 200 && out2.slots[2] == 202 );
    CHECK( out2.slots[3] == 0 && out2.slots[5] == 0 ); // the unwritten tail
    CHECK( out2.tally[0] == 20 && out2.tally[1] == 21 && out2.tally[2] == 22 );
}

// a value no variant names has no wire identity: measure and save refuse it,
// exactly as they refuse an out-of-range union tag — in a scalar field, and in
// EITHER array shape, where the check runs per element
static void test_unnameable_enum_refused()
{
    uint8_t buffer[256];
    tblv1::Cfg cfg;
    cfg.grade = (tblv1::Grade) 9;
    CHECK( tblv1::CfgMeasure( cfg ) == -1 );
    CHECK( tblv1::CfgSave( cfg, buffer, sizeof( buffer ) ) == -1 );

    // a counted array: only the elements BELOW the count are examined
    tblv1::Cfg counted;
    counted.grades_count = 2;
    counted.grades[0] = tblv1::Grade::Gold;
    counted.grades[1] = (tblv1::Grade) 9;
    CHECK( tblv1::CfgMeasure( counted ) == -1 );
    CHECK( tblv1::CfgSave( counted, buffer, sizeof( buffer ) ) == -1 );
    counted.grades_count = 1; // the bad element is now above the count
    CHECK( tblv1::CfgMeasure( counted ) > 0 );

    // a fixed array: every element rides, so every element is examined — but
    // only when the array is not all-default, which cannot hold a bad value
    tblv1::Cfg fixed;
    fixed.podium[2] = (tblv1::Grade) 9;
    CHECK( tblv1::CfgMeasure( fixed ) == -1 );
    CHECK( tblv1::CfgSave( fixed, buffer, sizeof( buffer ) ) == -1 );
}

// the READ side of an enum array: an element id this build cannot name lands
// on None and counts, and its neighbours decode normally
static void test_unnameable_enum_element_read()
{
    const tblv1::TableFieldInfo * grades = v1_field( tblv1::CfgTableType(), "grades" );
    CHECK( grades != NULL && grades->kind == 7 && grades->is_array );

    uint8_t wire[32];
    int n = 0;
    le16( wire + n, grades->id ); n += 2;
    wire[n++] = 14;                    // kArray
    le32( wire + n, 5 + 6 ); n += 4;   // header + three u16 elements
    wire[n++] = 7;                     // elem_kind kU16
    le32( wire + n, 3 ); n += 4;
    le16( wire + n, field_id( "Gold" ) ); n += 2;
    le16( wire + n, 0xBEEF ); n += 2;  // an element id no build names
    le16( wire + n, field_id( "Bronze" ) ); n += 2;
    le16( wire + n, 0 ); n += 2;       // terminator

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, n, &report ) );
    CHECK( !report.malformed && report.unknown == 1 );
    CHECK( out.grades_count == 3 );
    CHECK( out.grades[0] == tblv1::Grade::Gold );
    CHECK( out.grades[1] == tblv1::Grade::None );   // never a neighbour's variant
    CHECK( out.grades[2] == tblv1::Grade::Bronze ); // and the element after it decodes
}

// ---- FLAGS STAY POSITIONAL: a mask is a set of bits, and a bit has no cheap
// ---- name-identified form. It rides as its raw uint64 storage, so variants
// ---- are APPENDED AT THE END only (SPEC-TABLES.md §4, §5).

static void test_flags_are_positional()
{
    const tabledemo::TableFieldInfo * perks = demo_field( tabledemo::LoadoutConfigTableType(), "perks" );
    CHECK( perks != NULL );
    CHECK( perks->kind == 9 );            // kU64: the mask's raw storage
    CHECK( perks->variant_id == NULL );   // no per-variant wire id exists to carry
    CHECK( perks->enum_max == -1 );

    tabledemo::LoadoutConfig loadout;
    loadout.perks = tabledemo::Perks_Cloaked; // bit 1
    uint8_t buffer[1024];
    int64_t wrote = tabledemo::LoadoutConfigSave( loadout, buffer, sizeof( buffer ) );
    CHECK( wrote > 0 );

    // the payload is the mask itself: bit position IS the identity
    bool found = false;
    for ( int64_t i = 0; i + 11 <= wrote; i++ )
    {
        if ( buffer[i] == (uint8_t) ( perks->id & 0xff ) && buffer[i+1] == (uint8_t) ( perks->id >> 8 ) && buffer[i+2] == 9 )
        {
            CHECK( buffer[i+3] == 2 ); // 1 << 1, little-endian, low byte
            found = true;
        }
    }
    CHECK( found );
}

// ---- extents past 65535: u32 lengths and u32 counts (SPEC-TABLES.md §3) ----

static void test_wide_extents()
{
    static tabledemo::WideBlob blob;
    blob.label_length = 70000;
    memset( blob.label, 'w', 70000 );
    blob.label[70000] = 0;
    blob.payload_length = 70000;
    for ( int32_t i = 0; i < 70000; i++ ) blob.payload[i] = (uint8_t) ( i & 0xff );
    blob.samples_count = 70000;
    for ( int32_t i = 0; i < 70000; i++ ) blob.samples[i] = (uint16_t) ( i & 0xffff );

    int64_t need = tabledemo::WideBlobMeasure( blob );
    CHECK( need > 65535 * 3 );
    uint8_t * buffer = (uint8_t *) malloc( (size_t) need );
    CHECK( buffer != NULL );
    CHECK( tabledemo::WideBlobSave( blob, buffer, need ) == need ); // exact capacity holds out here too
    CHECK( tabledemo::WideBlobSave( blob, buffer, need - 1 ) == -1 );

    tabledemo::TableReport report;
    static tabledemo::WideBlob out;
    CHECK( tabledemo::WideBlobLoad( out, buffer, need, &report ) );
    CHECK( !report.malformed && report.unknown == 0 && report.clamped == 0 );
    CHECK( out.label_length == 70000 && out.label[69999] == 'w' && out.label[70000] == 0 );
    CHECK( out.payload_length == 70000 && out.payload[69999] == (uint8_t) ( 69999 & 0xff ) );
    CHECK( out.samples_count == 70000 && out.samples[69999] == (uint16_t) 69999 );

    // the wide case of the bounded-elements rule: a u32 count the body cannot
    // cover yields the bounded PREFIX and malformed, never a fabricated value
    const tabledemo::TableFieldInfo * samples = demo_field( tabledemo::WideBlobTableType(), "samples" );
    const tabledemo::TableFieldInfo * label = demo_field( tabledemo::WideBlobTableType(), "label" );
    CHECK( samples != NULL && label != NULL );
    uint8_t wire[64];
    int n = 0;
    le16( wire + n, samples->id ); n += 2;
    wire[n++] = 14;                    // kArray
    le32( wire + n, 5 + 4 ); n += 4;   // body: header + two u16 elements
    wire[n++] = 7;                     // elem_kind kU16
    le32( wire + n, 70000 ); n += 4;   // a count no uint16 could even hold — a lie
    le16( wire + n, 11 ); n += 2;      // element 0
    le16( wire + n, 22 ); n += 2;      // element 1
    le16( wire + n, label->id ); n += 2;
    wire[n++] = 12;                    // kString
    le32( wire + n, 2 ); n += 4;
    wire[n++] = 'o'; wire[n++] = 'k';
    le16( wire + n, 0 ); n += 2;       // terminator

    tabledemo::TableReport report2;
    static tabledemo::WideBlob out2;
    CHECK( tabledemo::WideBlobLoad( out2, wire, n, &report2 ) );
    CHECK( report2.malformed );                                  // the lie is framing damage
    CHECK( out2.samples_count == 2 );                            // the bounded prefix, nothing fabricated
    CHECK( out2.samples[0] == 11 && out2.samples[1] == 22 );
    CHECK( out2.label_length == 2 && strcmp( out2.label, "ok" ) == 0 ); // the parent read on
}

// ---- clamping: hostile or stale numerics clamp and count ----

static void test_clamping()
{
    // a wider writer sent a = 2000; v1's a is [0, 1000] — crafted from the
    // descriptor's own id so the bytes are exactly what such a writer emits
    const tblv1::TableFieldInfo * a = v1_field( tblv1::CfgTableType(), "a" );
    CHECK( a != NULL && a->kind == 4 && a->has_range && a->range_max == 1000.0 );

    uint8_t wire[16];
    int n = 0;
    le16( wire + n, a->id ); n += 2;
    wire[n++] = 4; // kI32
    le32( wire + n, (uint32_t) 2000 ); n += 4;
    le16( wire + n, 0 ); n += 2; // terminator

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, n, &report ) );
    CHECK( report.clamped == 1 && out.a == 1000 );

    // bits(6) width clamp: a u8 payload of 200 clamps to 63
    const tabledemo::TableFieldInfo * ch = demo_field( tabledemo::WeaponConfigTableType(), "channel" );
    CHECK( ch != NULL && ch->kind == 6 );
    uint8_t wire2[8];
    n = 0;
    le16( wire2 + n, ch->id ); n += 2;
    wire2[n++] = 6; // kU8
    wire2[n++] = 200;
    le16( wire2 + n, 0 ); n += 2;

    tabledemo::TableReport report2;
    tabledemo::WeaponConfig weapon;
    CHECK( tabledemo::WeaponConfigLoad( weapon, wire2, n, &report2 ) );
    CHECK( report2.clamped == 1 && weapon.channel == 63 );

    // an over-long string — a future schema widened string(32) — clamps to
    // capacity, stays NUL-terminated, and counts
    const tblv1::TableFieldInfo * nameInfo = v1_field( tblv1::CfgTableType(), "name" );
    uint8_t wire4[64];
    n = 0;
    le16( wire4 + n, nameInfo->id ); n += 2;
    wire4[n++] = 12; // kString
    le32( wire4 + n, 40 ); n += 4; // longer than string(32)
    for ( int i = 0; i < 40; i++ ) wire4[n++] = 'y';
    le16( wire4 + n, 0 ); n += 2;

    tblv1::TableReport report4;
    tblv1::Cfg out4;
    CHECK( tblv1::CfgLoad( out4, wire4, n, &report4 ) );
    CHECK( report4.clamped == 1 && out4.name_length == 32 && out4.name[32] == 0 );
}

// ---- malformed framing: decode stops, partial result kept, flag raised ----

static void test_malformed()
{
    tblv1::TableReport report;
    tblv1::Cfg out;

    uint8_t one_byte[1] = { 0x34 };
    CHECK( !tblv1::CfgLoad( out, one_byte, 1, &report ) );
    CHECK( report.malformed );

    // a valid field id whose payload is truncated mid-scalar
    const tblv1::TableFieldInfo * a = v1_field( tblv1::CfgTableType(), "a" );
    uint8_t truncated[5];
    le16( truncated, a->id );
    truncated[2] = 4;  // kI32 wants 4 payload bytes; only 2 follow
    truncated[3] = 0;
    truncated[4] = 0;
    tblv1::TableReport report2;
    CHECK( !tblv1::CfgLoad( out, truncated, 5, &report2 ) );
    CHECK( report2.malformed );

    // damage after good fields: the good prefix survives (partial result)
    tblv1::Cfg src;
    src.a = 42;
    uint8_t wire[256];
    int64_t bytes = tblv1::CfgSave( src, wire, sizeof( wire ) );
    CHECK( bytes > 0 );
    tblv1::TableReport report3;
    tblv1::Cfg out3;
    CHECK( !tblv1::CfgLoad( out3, wire, bytes - 2, &report3 ) ); // terminator cut off
    CHECK( report3.malformed && out3.a == 42 );
}

// ---- reflection: walk, identify, and read fields with no schema files ----

static void test_reflection()
{
    const tabledemo::TableTypeInfo * weapon = tabledemo::WeaponConfigTableType();
    CHECK( strcmp( weapon->name, "WeaponConfig" ) == 0 );
    CHECK( weapon->size == sizeof( tabledemo::WeaponConfig ) );

    // the was rename: speed's wire id is hash("velocity"), not hash("speed") —
    // pinned against an independent C++ implementation of the id function
    const tabledemo::TableFieldInfo * speed = demo_field( weapon, "speed" );
    CHECK( speed != NULL );
    CHECK( speed->id == field_id( "velocity" ) );
    CHECK( speed->id != field_id( "speed" ) );
    const tabledemo::TableFieldInfo * damage = demo_field( weapon, "damage" );
    CHECK( damage != NULL && damage->id == field_id( "damage" ) );
    CHECK( damage->kind == 10 ); // kF32

    // ranges surface for editors
    const tabledemo::TableFieldInfo * pen = demo_field( weapon, "penetration" );
    CHECK( pen != NULL && pen->has_range && pen->range_min == 0.0 && pen->range_max == 10.0 );

    // enum name function: value -> name, out-of-set included
    const tabledemo::TableFieldInfo * grade = demo_field( tabledemo::LoadoutConfigTableType(), "grade" );
    CHECK( grade != NULL && grade->enum_max == 3 && grade->enum_name != NULL );
    CHECK( strcmp( grade->enum_name( 3 ), "Gold" ) == 0 );
    CHECK( strcmp( grade->enum_name( 9 ), "???" ) == 0 );

    // and the id each name rides under: the vocabulary and its wire identity
    // are both reachable with no schema files (SPEC-TABLES.md §5, §8)
    CHECK( grade->variant_id != NULL );
    CHECK( grade->variant_id( 0 ) == 0 ); // None is the reserved id
    CHECK( grade->variant_id( 1 ) == field_id( "Bronze" ) );
    CHECK( grade->variant_id( 2 ) == field_id( "Silver" ) );
    CHECK( grade->variant_id( 3 ) == field_id( "Gold" ) );
    CHECK( grade->variant_id( 9 ) == 0 ); // no variant names it

    // nested-table descriptors chain, arrays carry bounds and companions
    const tabledemo::TableTypeInfo * rootType = tabledemo::RootConfigTableType();
    const tabledemo::TableFieldInfo * profiles = demo_field( rootType, "profiles" );
    CHECK( profiles != NULL && profiles->kind == 13 && profiles->is_array && profiles->counted );
    CHECK( profiles->array_bound == 4 );
    CHECK( profiles->table == tabledemo::ProfileConfigTableType() );

    // guards surface machine-usable
    const tabledemo::TableFieldInfo * loadout = demo_field( tabledemo::ProfileConfigTableType(), "loadout" );
    CHECK( loadout != NULL && strcmp( loadout->guard, "has_loadout" ) == 0 );

    // every one of the 15 wire kinds rides somewhere in the corpus battery;
    // the scalar kinds pin here by descriptor (containers pinned above and
    // in the round trip: 12 string, 13 table, 14 array, 15 union; 1 bool on
    // homing, 10 f32 on damage)
    const tabledemo::TableTypeInfo * profileType = tabledemo::ProfileConfigTableType();
    struct { const char * field; uint8_t kind; } scalar_kinds[] = {
        { "tilt", 2 },       // i8
        { "heading", 3 },    // i16
        { "timestamp", 5 },  // i64
        { "badge", 6 },      // u8
        { "port", 7 },       // u16
        { "experience", 8 }, // u32
        { "epoch", 9 },      // u64
        { "precision", 11 }, // f64
    };
    for ( const auto & sk : scalar_kinds )
    {
        const tabledemo::TableFieldInfo * field = demo_field( profileType, sk.field );
        CHECK( field != NULL && field->kind == sk.kind );
    }
    const tabledemo::TableFieldInfo * homing = demo_field( weapon, "homing" );
    CHECK( homing != NULL && homing->kind == 1 ); // bool
    const tabledemo::TableFieldInfo * effect = demo_field( weapon, "effect" );
    CHECK( effect != NULL && effect->kind == 15 ); // union
    // a union's arms are a vocabulary too: [0, enum_max], names and ids
    CHECK( effect->enum_max == 2 && effect->enum_name != NULL && effect->variant_id != NULL );
    CHECK( strcmp( effect->enum_name( 0 ), "None" ) == 0 );
    CHECK( strcmp( effect->enum_name( 1 ), "buff" ) == 0 );
    CHECK( effect->variant_id( 0 ) == 0 );
    CHECK( effect->variant_id( 2 ) == field_id( "debuff" ) );
    const tabledemo::TableFieldInfo * nameF = demo_field( profileType, "name" );
    CHECK( nameF != NULL && nameF->kind == 12 ); // string

    // generic field access through offset — an editor walking properties
    tabledemo::ProfileConfig p;
    p.experience = 777;
    const tabledemo::TableFieldInfo * exp = demo_field( tabledemo::ProfileConfigTableType(), "experience" );
    CHECK( exp != NULL );
    uint32_t read_back;
    memcpy( &read_back, (const uint8_t *) &p + exp->offset, sizeof( read_back ) );
    CHECK( read_back == 777 );
}

// ---- cross-file nesting: ArchiveConfig (Nested.schema) holds RootConfig ----

static void test_cross_file()
{
    static tabledemo::ArchiveConfig archive; // static: the value is large
    set_string( archive.root.version_note, archive.root.version_note_length, "deep" );
    archive.root.weapons_count = 1;
    archive.root.weapons[0].homing = true;
    archive.count = 5;

    static uint8_t buffer[16384];
    int64_t wrote = tabledemo::ArchiveConfigSave( archive, buffer, sizeof( buffer ) );
    CHECK( wrote > 0 && wrote == tabledemo::ArchiveConfigMeasure( archive ) );

    tabledemo::TableReport report;
    static tabledemo::ArchiveConfig out;
    CHECK( tabledemo::ArchiveConfigLoad( out, buffer, wrote, &report ) );
    CHECK( strcmp( out.root.version_note, "deep" ) == 0 );
    CHECK( out.root.weapons_count == 1 && out.root.weapons[0].homing );
    CHECK( out.count == 5 );
}

// ---- parallel generation: measure subtables, prefix-sum, scatter-write ----

static void test_parallel_shape()
{
    // the go-wide pattern from USAGE.md, single-threaded here for
    // determinism: measure each profile, hand each a disjoint range, write
    // independently, and the concatenation decodes as if written serially
    tabledemo::ProfileConfig profiles[3];
    for ( int i = 0; i < 3; i++ )
    {
        profiles[i].experience = 100 + i;
    }
    int64_t sizes[3];
    int64_t total = 0;
    for ( int i = 0; i < 3; i++ )
    {
        sizes[i] = tabledemo::ProfileConfigMeasure( profiles[i] );
        total += sizes[i];
    }
    static uint8_t buffer[4096];
    int64_t offset = 0;
    for ( int i = 0; i < 3; i++ ) // each iteration is independent: a worker
    {
        int64_t wrote = tabledemo::ProfileConfigSave( profiles[i], buffer + offset, sizes[i] );
        CHECK( wrote == sizes[i] );
        offset += wrote;
    }
    CHECK( offset == total );
    offset = 0;
    for ( int i = 0; i < 3; i++ )
    {
        tabledemo::TableReport report;
        tabledemo::ProfileConfig out;
        CHECK( tabledemo::ProfileConfigLoad( out, buffer + offset, sizes[i], &report ) );
        CHECK( out.experience == (uint32_t) ( 100 + i ) );
        offset += sizes[i];
    }
}


// ============================================================================
// POINTER SEMANTICS (SPEC-TABLES.md §2, §6). Types remain value semantics;
// tables allow pointer semantics — and everything below is a consequence.
// ============================================================================

static const graphdemo::TableFieldInfo * graph_field( const graphdemo::TableTypeInfo * type, const char * name )
{
    for ( int32_t i = 0; i < type->num_fields; i++ )
        if ( strcmp( type->fields[i].name, name ) == 0 )
            return &type->fields[i];
    return NULL;
}

// build_scene populates a builder with a list, a tree, an optional subtable, a
// by-value nested variable table and an array of them. Returns the number of
// list nodes on the chain.
static int build_scene( graphdemo::SceneBuilder & builder )
{
    graphdemo::Scene * root = builder.GetRoot();
    set_string( root->name, root->name_length, "graph" );
    root->version = 7;
    root->meta.build = 42;
    set_string( root->meta.tag, root->meta.tag_length, "m" );

    graphdemo::TableSlot<graphdemo::Settings> settings = builder.Alloc<graphdemo::Settings>();
    settings->quality = 4;
    set_string( settings->label, settings->label_length, "high" );
    root->settings = settings;

    graphdemo::TableSlot<graphdemo::ListNode> n0 = builder.Alloc<graphdemo::ListNode>();
    graphdemo::TableSlot<graphdemo::ListNode> n1 = builder.Alloc<graphdemo::ListNode>();
    graphdemo::TableSlot<graphdemo::ListNode> n2 = builder.Alloc<graphdemo::ListNode>();
    n0->value = 10; set_string( n0->name, n0->name_length, "a" );
    n1->value = 20; set_string( n1->name, n1->name_length, "b" );
    n2->value = 30;
    n0->next = n1;
    n1->next = n2;
    root->head = n0;

    graphdemo::TableSlot<graphdemo::TreeNode> t = builder.Alloc<graphdemo::TreeNode>();
    graphdemo::TableSlot<graphdemo::TreeNode> tl = builder.Alloc<graphdemo::TreeNode>();
    graphdemo::TableSlot<graphdemo::TreeNode> tr = builder.Alloc<graphdemo::TreeNode>();
    set_string( t->label, t->label_length, "root" );
    set_string( tl->label, tl->label_length, "left" );
    set_string( tr->label, tr->label_length, "right" );
    t->left = tl;
    t->right = tr;
    root->tree = t;

    // a variable table nested BY VALUE, with a pointer of its own
    root->ground.depth = 3;
    graphdemo::TableSlot<graphdemo::ListNode> g0 = builder.Alloc<graphdemo::ListNode>();
    g0->value = 99;
    root->ground.head = g0;

    // a bounded array of variable tables
    root->layers_count = 2;
    root->layers[0].depth = 1;
    root->layers[1].depth = 2;
    graphdemo::TableSlot<graphdemo::ListNode> l1 = builder.Alloc<graphdemo::ListNode>();
    l1->value = 55;
    root->layers[1].head = l1;

    return 3;
}

// check_scene reads a scene through a CONST root — the only way a
// pointer-bearing table is ever read: a region and a root view, never a value.
static void check_scene( const graphdemo::Scene * scene )
{
    CHECK( scene != NULL );
    if ( scene == NULL ) return;
    CHECK( strcmp( scene->name, "graph" ) == 0 );
    CHECK( scene->version == 7 );
    CHECK( scene->meta.build == 42 );

    const graphdemo::ListNode * a = graphdemo::ListNodeAt( scene->head );
    CHECK( a != NULL && a->value == 10 && strcmp( a->name, "a" ) == 0 );
    const graphdemo::ListNode * b = graphdemo::ListNodeAt( a->next );
    CHECK( b != NULL && b->value == 20 );
    const graphdemo::ListNode * c = graphdemo::ListNodeAt( b->next );
    CHECK( c != NULL && c->value == 30 );
    CHECK( graphdemo::ListNodeAt( c->next ) == NULL ); // the chain ends in null

    const graphdemo::TreeNode * t = graphdemo::TreeNodeAt( scene->tree );
    CHECK( t != NULL && strcmp( t->label, "root" ) == 0 );
    CHECK( strcmp( graphdemo::TreeNodeAt( t->left )->label, "left" ) == 0 );
    CHECK( strcmp( graphdemo::TreeNodeAt( t->right )->label, "right" ) == 0 );
    CHECK( graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( t->left )->left ) == NULL );

    const graphdemo::Settings * settings = graphdemo::SettingsAt( scene->settings );
    CHECK( settings != NULL && settings->quality == 4 && strcmp( settings->label, "high" ) == 0 );

    CHECK( scene->ground.depth == 3 );
    CHECK( graphdemo::ListNodeAt( scene->ground.head )->value == 99 );
    CHECK( scene->layers_count == 2 );
    CHECK( scene->layers[0].depth == 1 );
    CHECK( graphdemo::ListNodeAt( scene->layers[0].head ) == NULL ); // never set: null
    CHECK( graphdemo::ListNodeAt( scene->layers[1].head )->value == 55 );
}

// ---- the lifecycle: build -> Lock -> the region; wire out and back ----

static void test_pointer_lifecycle()
{
    graphdemo::SceneBuilder builder;
    build_scene( builder );

    // measure/save straight out of the MUTABLE arena
    int64_t need = graphdemo::SceneMeasure( builder );
    CHECK( need > 0 );
    static uint8_t wire[8192];
    int64_t wrote = graphdemo::SceneSave( builder, wire, sizeof( wire ) );
    CHECK( wrote == need );

    // the exact-capacity guarantee, across a pointer graph
    static uint8_t exact[8192];
    CHECK( graphdemo::SceneSave( builder, exact, need ) == need );
    CHECK( memcmp( wire, exact, (size_t) need ) == 0 );
    CHECK( graphdemo::SceneSave( builder, exact, need - 1 ) == -1 );

    // Lock: one way, and it IS the compaction
    CHECK( builder.Lock() );
    CHECK( builder.Locked() );
    CHECK( builder.GetRoot() == NULL );      // the mutable life is over
    CHECK( builder.Alloc<graphdemo::ListNode>().null() ); // and Alloc refuses
    CHECK( builder.Lock() );              // idempotent, never a second compaction

    const graphdemo::Scene * locked = builder.AsConst();
    check_scene( locked );

    // the packed region has zero slack and every node is 8-aligned
    CHECK( builder.RegionBytes() > 0 );
    CHECK( ( builder.RegionBytes() % 8 ) == 0 );

    // saving from the locked region gives byte-identical wire to saving from
    // the arena: one structure, two representations, one meaning
    static uint8_t after_lock[8192];
    int64_t wrote2 = graphdemo::SceneSave( locked, after_lock, sizeof( after_lock ) );
    CHECK( wrote2 == wrote );
    CHECK( memcmp( wire, after_lock, (size_t) wrote ) == 0 );

    // the region relocates by PURE MEMCPY: self-relative references need no
    // fix-up, so a copy at a different address reads the same
    uint8_t * moved = (uint8_t *) malloc( (size_t) builder.RegionBytes() );
    memcpy( moved, builder.Region(), (size_t) builder.RegionBytes() );
    check_scene( (const graphdemo::Scene *) moved );
    free( moved );

    // wire -> const region, sized EXACTLY by the pre-pass
    int64_t region_need = graphdemo::SceneLoadMeasure( wire, wrote );
    CHECK( region_need == builder.RegionBytes() ); // the two forms agree
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport report;
    const graphdemo::Scene * loaded = graphdemo::SceneLoad( region, region_need, wire, wrote, &report );
    CHECK( loaded != NULL );
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && !report.malformed );
    check_scene( loaded );

    // a loaded region re-saves to the same bytes
    static uint8_t again[8192];
    CHECK( graphdemo::SceneSave( loaded, again, sizeof( again ) ) == wrote );
    CHECK( memcmp( wire, again, (size_t) wrote ) == 0 );

    // a region one byte short refuses rather than overrunning
    uint8_t * tight = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport short_report;
    const graphdemo::Scene * partial = graphdemo::SceneLoad( tight, region_need - 8, wire, wrote, &short_report );
    CHECK( partial != NULL && short_report.malformed ); // partial result, flagged
    free( tight );

    free( region );
}

// ---- Lock's byte layout is deterministic: same input, same region ----

static void test_lock_layout_stable()
{
    graphdemo::SceneBuilder first;
    build_scene( first );
    CHECK( first.Lock() );

    graphdemo::SceneBuilder second;
    build_scene( second );
    CHECK( second.Lock() );

    CHECK( first.RegionBytes() == second.RegionBytes() );
    CHECK( memcmp( first.Region(), second.Region(), (size_t) first.RegionBytes() ) == 0 );

    // and cooking twice gives identical files
    int64_t need = graphdemo::SceneCookMeasure( first );
    uint8_t * a = (uint8_t *) malloc( (size_t) need );
    uint8_t * b = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneCook( first, a, need ) == need );
    CHECK( graphdemo::SceneCook( second, b, need ) == need );
    CHECK( memcmp( a, b, (size_t) need ) == 0 );
    free( a );
    free( b );
}

// ---- aliasing: two pointers to one node are TWO nodes, everywhere ----

static void test_pointer_alias()
{
    graphdemo::SceneBuilder builder;
    graphdemo::Scene * root = builder.GetRoot();
    graphdemo::TableSlot<graphdemo::ListNode> shared = builder.Alloc<graphdemo::ListNode>();
    shared->value = 1234;
    root->head = shared;
    root->alias = shared; // the SAME node named twice

    CHECK( builder.Lock() );
    const graphdemo::Scene * locked = builder.AsConst();
    const graphdemo::ListNode * viaHead = graphdemo::ListNodeAt( locked->head );
    const graphdemo::ListNode * viaAlias = graphdemo::ListNodeAt( locked->alias );
    CHECK( viaHead != NULL && viaAlias != NULL );
    CHECK( viaHead->value == 1234 && viaAlias->value == 1234 );
    // wire v1 is a TREE: identity is NOT preserved, and the packed form says so
    // in the same voice — two references, two nodes (SPEC-TABLES.md §3)
    CHECK( viaHead != viaAlias );

    static uint8_t wire[1024];
    int64_t wrote = graphdemo::SceneSave( locked, wire, sizeof( wire ) );
    CHECK( wrote > 0 );
    int64_t region_need = graphdemo::SceneLoadMeasure( wire, wrote );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport report;
    const graphdemo::Scene * loaded = graphdemo::SceneLoad( region, region_need, wire, wrote, &report );
    CHECK( loaded != NULL && !report.malformed );
    CHECK( graphdemo::ListNodeAt( loaded->head )->value == 1234 );
    CHECK( graphdemo::ListNodeAt( loaded->alias )->value == 1234 );
    CHECK( graphdemo::ListNodeAt( loaded->head ) != graphdemo::ListNodeAt( loaded->alias ) );
    free( region );
}

// ---- a data cycle is an ERROR, never a hang ----

static void test_pointer_cycle_refused()
{
    graphdemo::SceneBuilder builder;
    graphdemo::Scene * root = builder.GetRoot();
    graphdemo::TableSlot<graphdemo::ListNode> loop = builder.Alloc<graphdemo::ListNode>();
    loop->value = 1;
    loop->next = loop;   // a node pointing at itself
    root->head = loop;

    static uint8_t buffer[4096];
    CHECK( graphdemo::SceneMeasure( builder ) == -1 );
    CHECK( graphdemo::SceneSave( builder, buffer, sizeof( buffer ) ) == -1 );
    CHECK( graphdemo::SceneCookMeasure( builder ) == -1 );
    CHECK( !builder.Lock() ); // the compaction refuses too
}

// ---- the depth cap: a chain past it refuses, and the wire stops at it ----

static void test_pointer_depth_cap()
{
    // a chain exactly AT the cap saves; the cap counts nesting levels, and the
    // root is level 1
    {
        graphdemo::SceneBuilder builder;
        graphdemo::Scene * root = builder.GetRoot();
        graphdemo::TableSlot<graphdemo::ListNode> head = builder.Alloc<graphdemo::ListNode>();
        head->value = 0;
        root->head = head;
        graphdemo::ListNode * tail = head;
        for ( int i = 1; i < graphdemo::kTableMaxDepth - 1; i++ )
        {
            graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
            node->value = i;
            tail->next = node;
            tail = node;
        }
        CHECK( graphdemo::SceneMeasure( builder ) > 0 );
    }
    // one link past it refuses instead of recursing away
    {
        graphdemo::SceneBuilder builder;
        graphdemo::Scene * root = builder.GetRoot();
        graphdemo::TableSlot<graphdemo::ListNode> head = builder.Alloc<graphdemo::ListNode>();
        root->head = head;
        graphdemo::ListNode * tail = head;
        for ( int i = 1; i < graphdemo::kTableMaxDepth + 8; i++ )
        {
            graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
            node->value = i;
            tail->next = node;
            tail = node;
        }
        static uint8_t buffer[65536];
        CHECK( graphdemo::SceneMeasure( builder ) == -1 );
        CHECK( graphdemo::SceneSave( builder, buffer, sizeof( buffer ) ) == -1 );
        CHECK( !builder.Lock() );
    }
}

// ---- the arena grows without invalidating anything ----

static void test_builder_grow()
{
    graphdemo::SceneBuilder builder;
    graphdemo::Scene * root = builder.GetRoot();

    // enough nodes to cross many 64 KiB slabs and more than one 4 MiB segment
    const int count = 200000;
    graphdemo::TableSlot<graphdemo::ListNode> first = builder.Alloc<graphdemo::ListNode>();
    first->value = 1;
    graphdemo::ListNode * held = first;          // a pointer held ACROSS all the growth
    graphdemo::TableSlot<graphdemo::ListNode> middle;
    for ( int i = 1; i < count; i++ )
    {
        graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
        CHECK( !node.null() || i == 0 );
        node->value = i + 1;
        if ( i == count / 2 ) { middle = node; }
    }
    // segments never move: the pointer taken before 200k allocations is still
    // the same node, and the root the builder handed out is still valid
    CHECK( held->value == 1 );
    CHECK( middle.ptr != NULL && middle->value == count / 2 + 1 );
    CHECK( builder.GetRoot() == root );

    root->head = first;
    CHECK( graphdemo::SceneMeasure( builder ) > 0 );
}

// ---- many workers, one arena: allocation is lock-free by ownership ----

static void test_builder_workers()
{
    // deterministic first: four workers, interleaved, single-threaded
    graphdemo::SceneBuilder builder;
    graphdemo::Scene * root = builder.GetRoot();
    graphdemo::TableWorker workers[4];
    for ( int i = 0; i < 4; i++ ) { workers[i] = builder.Worker(); }

    graphdemo::TableSlot<graphdemo::ListNode> head = workers[0].Alloc<graphdemo::ListNode>();
    head->value = 0;
    root->head = head;
    graphdemo::ListNode * tail = head;
    for ( int i = 1; i < 64; i++ )
    {
        // each link allocated by a DIFFERENT worker: a cross-worker reference
        // is an ordinary reference, because offsets are arena-global
        graphdemo::TableSlot<graphdemo::ListNode> node = workers[i % 4].Alloc<graphdemo::ListNode>();
        node->value = i;
        tail->next = node;
        tail = node;
    }
    CHECK( builder.Lock() );
    const graphdemo::ListNode * walk = graphdemo::ListNodeAt( builder.AsConst()->head );
    for ( int i = 0; i < 64; i++ )
    {
        CHECK( walk != NULL && walk->value == i );
        if ( walk == NULL ) break;
        walk = graphdemo::ListNodeAt( walk->next );
    }
    CHECK( walk == NULL );

    // then the real thing: four threads allocating concurrently on their own
    // workers. Each thread owns its nodes; nothing is shared, nothing is locked.
    graphdemo::SceneBuilder threaded;
    const int per_thread = 20000;
    std::vector<graphdemo::TableRef> heads( 4 );
    std::vector<std::thread> threads;
    for ( int t = 0; t < 4; t++ )
    {
        threads.emplace_back( [&threaded, &heads, t]() {
            graphdemo::TableWorker worker = threaded.Worker();
            graphdemo::TableSlot<graphdemo::ListNode> first = worker.Alloc<graphdemo::ListNode>();
            first->value = t;
            graphdemo::ListNode * tail = first;
            for ( int i = 1; i < per_thread; i++ )
            {
                graphdemo::TableSlot<graphdemo::ListNode> node = worker.Alloc<graphdemo::ListNode>();
                node->value = t * 1000000 + i;
                tail->next = node;
                tail = node;
            }
            heads[t] = first.ref;
        } );
    }
    for ( auto & thread : threads ) { thread.join(); }
    // the join is the barrier; everything below is single-threaded
    graphdemo::Scene * threaded_root = threaded.GetRoot();
    threaded_root->layers_count = 4;
    for ( int t = 0; t < 4; t++ )
    {
        threaded_root->layers[t].depth = t;
        threaded_root->layers[t].head = heads[t];
        const graphdemo::ListNode * node = graphdemo::ListNodeAt( threaded.arena, heads[t] );
        CHECK( node != NULL && node->value == t );
    }
}

// ---- the cooked form: point at it, or refuse loudly ----

static void test_cooked_form()
{
    graphdemo::SceneBuilder builder;
    build_scene( builder );
    CHECK( builder.Lock() );

    int64_t need = graphdemo::SceneCookMeasure( builder );
    CHECK( need == graphdemo::kTableCookedHeaderBytes + builder.RegionBytes() );
    uint8_t * cooked = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneCook( builder, cooked, need ) == need );
    CHECK( graphdemo::SceneCook( builder, cooked, need - 1 ) == -1 ); // short buffer refuses

    // open by POINTING: no copy, no decode
    const graphdemo::Scene * opened = graphdemo::SceneOpen( cooked, need );
    CHECK( opened != NULL );
    CHECK( (const uint8_t *) opened == cooked + graphdemo::kTableCookedHeaderBytes );
    check_scene( opened );

    // cook -> open -> cook is stable
    uint8_t * again = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneCook( opened, again, need ) == need );
    CHECK( memcmp( cooked, again, (size_t) need ) == 0 );
    free( again );

    // the wire still reads out of a cooked root: the two forms agree
    static uint8_t wire[8192];
    int64_t wrote = graphdemo::SceneSave( opened, wire, sizeof( wire ) );
    CHECK( wrote == graphdemo::SceneMeasure( builder ) );

    // ---- the refusal battery ----
    uint8_t * broken = (uint8_t *) malloc( (size_t) need );

    memcpy( broken, cooked, (size_t) need );
    broken[0] ^= 0xFF; // magic: wrong form, or a foreign byte order
    CHECK( graphdemo::SceneOpen( broken, need ) == NULL );

    memcpy( broken, cooked, (size_t) need );
    broken[4] ^= 0xFF; // layout id: the schema or the ABI moved
    CHECK( graphdemo::SceneOpen( broken, need ) == NULL );

    memcpy( broken, cooked, (size_t) need );
    CHECK( graphdemo::SceneOpen( broken, need - 1 ) == NULL ); // truncated region

    memcpy( broken, cooked, (size_t) need );
    CHECK( graphdemo::SceneOpen( broken, 4 ) == NULL ); // shorter than the header

    memcpy( broken, cooked, (size_t) need );
    CHECK( graphdemo::SceneOpen( broken + 1, need - 1 ) == NULL ); // unaligned base

    // an offset graph that leaves the region: the bounds walk catches it
    memcpy( broken, cooked, (size_t) need );
    {
        graphdemo::Scene * root = (graphdemo::Scene *) ( broken + graphdemo::kTableCookedHeaderBytes );
        root->head.value = 0x7FFFFFF0u; // far past the end
        CHECK( graphdemo::SceneOpen( broken, need ) == NULL );
    }
    // a BACKWARD reference: impossible in a packed region, so it is corruption
    memcpy( broken, cooked, (size_t) need );
    {
        graphdemo::Scene * root = (graphdemo::Scene *) ( broken + graphdemo::kTableCookedHeaderBytes );
        root->head.value = 0xFFFFFFF8u; // -8 as an int32
        CHECK( graphdemo::SceneOpen( broken, need ) == NULL );
    }
    // a misaligned reference
    memcpy( broken, cooked, (size_t) need );
    {
        graphdemo::Scene * root = (graphdemo::Scene *) ( broken + graphdemo::kTableCookedHeaderBytes );
        root->head.value += 1;
        CHECK( graphdemo::SceneOpen( broken, need ) == NULL );
    }
    // a count companion outside its declared bound
    memcpy( broken, cooked, (size_t) need );
    {
        graphdemo::Scene * root = (graphdemo::Scene *) ( broken + graphdemo::kTableCookedHeaderBytes );
        root->layers_count = 99;
        CHECK( graphdemo::SceneOpen( broken, need ) == NULL );
    }
    // and the untouched file still opens: the battery broke nothing else
    CHECK( graphdemo::SceneOpen( cooked, need ) != NULL );

    free( broken );
    free( cooked );
}

// ---- evolution across a pointer field, both directions ----

static void test_pointer_evolution_old_reader_new_data()
{
    // P2 writes a chain of two Links through a POINTER field
    tblp2::ChainBuilder builder;
    tblp2::Chain * root = builder.GetRoot();
    set_string( root->name, root->name_length, "chain" );
    tblp2::TableSlot<tblp2::Link> first = builder.Alloc<tblp2::Link>();
    tblp2::TableSlot<tblp2::Link> second = builder.Alloc<tblp2::Link>();
    first->value = 11;
    set_string( first->tag, first->tag_length, "one" );
    second->value = 22;
    first->next = second;
    root->link = first;

    uint8_t wire[512];
    int64_t wrote = tblp2::ChainSave( builder, wire, sizeof( wire ) );
    CHECK( wrote > 0 );

    // P1 has `link` as a BY-VALUE nesting and no `next` at all. A pointer rides
    // with a by-value nesting's framing, so the first Link decodes cleanly and
    // the chain's tail lands as one unknown field.
    tblp1::Chain out;
    tblp1::TableReport report;
    CHECK( tblp1::ChainLoad( out, wire, wrote, &report ) );
    CHECK( !report.malformed );
    CHECK( report.unknown == 1 ); // Link.next, which P1 cannot name
    CHECK( strcmp( out.name, "chain" ) == 0 );
    CHECK( out.link.value == 11 );
    CHECK( strcmp( out.link.tag, "one" ) == 0 );
}

static void test_pointer_evolution_new_reader_old_data()
{
    // P1 writes a by-value nesting
    tblp1::Chain v1;
    set_string( v1.name, v1.name_length, "aged" );
    v1.link.value = 77;
    set_string( v1.link.tag, v1.link.tag_length, "old" );

    uint8_t wire[512];
    int64_t wrote = tblp1::ChainSave( v1, wire, sizeof( wire ) );
    CHECK( wrote > 0 && wrote == tblp1::ChainMeasure( v1 ) );

    // P2 has `link` as a POINTER: the same body allocates one node
    int64_t region_need = tblp2::ChainLoadMeasure( wire, wrote );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    tblp2::TableReport report;
    const tblp2::Chain * out = tblp2::ChainLoad( region, region_need, wire, wrote, &report );
    CHECK( out != NULL );
    CHECK( !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 );
    CHECK( strcmp( out->name, "aged" ) == 0 );
    const tblp2::Link * link = tblp2::LinkAt( out->link );
    CHECK( link != NULL && link->value == 77 && strcmp( link->tag, "old" ) == 0 );
    CHECK( tblp2::LinkAt( link->next ) == NULL ); // a field old data never wrote is null
    free( region );

    // and into a BUILDER — the tool's path: load, edit, lock again
    tblp2::ChainBuilder builder;
    tblp2::TableReport builder_report;
    CHECK( tblp2::ChainLoadBuilder( builder, wire, wrote, &builder_report ) );
    CHECK( !builder_report.malformed );
    tblp2::Link * loaded = tblp2::LinkAt( builder.arena, builder.GetRoot()->link );
    CHECK( loaded != NULL && loaded->value == 77 );
    tblp2::TableSlot<tblp2::Link> added = builder.Alloc<tblp2::Link>();
    added->value = 88;
    loaded->next = added;
    CHECK( builder.Lock() );
    CHECK( tblp2::LinkAt( tblp2::LinkAt( builder.AsConst()->link )->next )->value == 88 );
}

// ---- a null pointer elides; a non-null one rides even when all-default ----

static void test_pointer_null_and_empty()
{
    graphdemo::SceneBuilder empty;
    int64_t bare = graphdemo::SceneMeasure( empty );
    CHECK( bare == 2 ); // every pointer null, everything else default: nothing rides

    graphdemo::SceneBuilder one;
    graphdemo::TableSlot<graphdemo::ListNode> node = one.Alloc<graphdemo::ListNode>();
    one.GetRoot()->head = node; // an ALL-DEFAULT pointee behind a non-null pointer
    int64_t with_empty = graphdemo::SceneMeasure( one );
    CHECK( with_empty == 2 + 3 + 4 + 2 ); // id + kind + length + the empty body

    uint8_t wire[64];
    int64_t wrote = graphdemo::SceneSave( one, wire, sizeof( wire ) );
    CHECK( wrote == with_empty );
    int64_t region_need = graphdemo::SceneLoadMeasure( wire, wrote );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport report;
    const graphdemo::Scene * loaded = graphdemo::SceneLoad( region, region_need, wire, wrote, &report );
    // the distinction survives: an empty pointee is NOT null
    CHECK( loaded != NULL && graphdemo::ListNodeAt( loaded->head ) != NULL );
    CHECK( graphdemo::ListNodeAt( loaded->head )->value == 0 );
    free( region );
}

// ---- reflection: the derived mode and the pointer kind are visible ----

static void test_pointer_reflection()
{
    const graphdemo::TableTypeInfo * scene = graphdemo::SceneTableType();
    const graphdemo::TableTypeInfo * meta = graphdemo::MetaTableType();
    const graphdemo::TableTypeInfo * list = graphdemo::ListNodeTableType();

    // the MODE is derived and surfaced: nobody declared either of these
    CHECK( scene->variable );
    CHECK( list->variable );
    CHECK( !meta->variable );
    CHECK( !graphdemo::SettingsTableType()->variable ); // pointed at, still fixed
    CHECK( graphdemo::LayerTableType()->variable );     // a pointer of its own

    const graphdemo::TableFieldInfo * head = graph_field( scene, "head" );
    CHECK( head != NULL && head->is_pointer );
    CHECK( head->kind == 13 ); // a pointer rides as a nested table body
    CHECK( head->elem_size == sizeof( graphdemo::TableRef ) );
    CHECK( head->table == graphdemo::ListNodeTableType() ); // the TARGET's descriptor
    CHECK( !head->is_array && !head->counted );

    // a self-referential pointer resolves to its own type's descriptor
    const graphdemo::TableFieldInfo * next = graph_field( list, "next" );
    CHECK( next != NULL && next->is_pointer && next->table == list );

    // a by-value nesting is not a pointer
    const graphdemo::TableFieldInfo * ground = graph_field( scene, "ground" );
    CHECK( ground != NULL && !ground->is_pointer && ground->kind == 13 );
    const graphdemo::TableFieldInfo * meta_field = graph_field( scene, "meta" );
    CHECK( meta_field != NULL && !meta_field->is_pointer );

    // a pointer field's id is its name's hash, exactly as any other field's
    CHECK( head->id == field_id( "head" ) );

    // relocatability holds with pointers in the struct: the slot is four bytes
    CHECK( sizeof( graphdemo::TableRef ) == 4 );
}


// ============================================================================
// REGRESSIONS from the adversarial review. Each is the reviewer's own repro,
// reduced to an assertion that goes red if the defect returns.
// ============================================================================

// ---- B1: Open's walk is LINEAR, not exponential ----
//
// The repro: a legal cooked file whose TreeNode chain runs down `left`, then
// forged so every node's `right` ALIASES its `left`. Every reference stays
// forward, in range and aligned, so a walk with no order state explores 2^n
// paths — 26 nodes took 312 ms and ~60 nodes never returned. The high-water
// mark refuses the second (aliasing) reference outright, in linear time.
static void test_open_walk_is_linear()
{
    for ( int n : { 16, 26, 40, 64 } )
    {
        graphdemo::SceneBuilder builder;
        graphdemo::TableRef * slot = &builder.GetRoot()->tree;
        for ( int i = 0; i < n; i++ )
        {
            graphdemo::TableSlot<graphdemo::TreeNode> node = builder.Alloc<graphdemo::TreeNode>();
            *slot = node.ref;
            slot = &node.ptr->left;
        }
        CHECK( builder.Lock() );
        int64_t need = graphdemo::SceneCookMeasure( builder );
        uint8_t * file = (uint8_t *) malloc( (size_t) need );
        CHECK( graphdemo::SceneCook( builder, file, need ) == need );
        CHECK( graphdemo::SceneOpen( file, need ) != NULL ); // the genuine file opens

        // forge: every node's right aliases its left — forward, in range, aligned
        uint8_t * base = file + graphdemo::kTableCookedHeaderBytes;
        int64_t region = need - graphdemo::kTableCookedHeaderBytes;
        graphdemo::Scene * root = (graphdemo::Scene *) base;
        int64_t at = ( (uint8_t *) &root->tree - base ) + (int64_t) root->tree.value;
        int patched = 0;
        while ( at + (int64_t) sizeof( graphdemo::TreeNode ) <= region )
        {
            graphdemo::TreeNode * node = (graphdemo::TreeNode *) ( base + at );
            if ( node->left.value == 0 ) { break; }
            int64_t next_at = ( (uint8_t *) &node->left - base ) + (int64_t) node->left.value;
            node->right.value = (uint32_t) ( ( base + next_at ) - (uint8_t *) &node->right );
            patched++;
            at = next_at;
        }
        CHECK( patched == n - 1 );
        // REFUSED, and refused in linear time: the mark makes each accepted
        // reference consume region bytes, so the walk cannot revisit a node.
        // At n = 64 an unbounded walk would not return in the age of the
        // universe; this returns before the next line runs.
        CHECK( graphdemo::SceneOpen( file, need ) == NULL );
        free( file );
    }
}

// ---- B1's companion: the mark also closes mid-node overlap ----

static void test_open_refuses_overlap()
{
    graphdemo::SceneBuilder builder;
    graphdemo::TableSlot<graphdemo::ListNode> a = builder.Alloc<graphdemo::ListNode>();
    graphdemo::TableSlot<graphdemo::ListNode> b = builder.Alloc<graphdemo::ListNode>();
    a->value = 1;
    b->value = 2;
    a->next = b;
    builder.GetRoot()->head = a;
    CHECK( builder.Lock() );
    int64_t need = graphdemo::SceneCookMeasure( builder );
    uint8_t * file = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneCook( builder, file, need ) == need );
    CHECK( graphdemo::SceneOpen( file, need ) != NULL );

    // point head at a node-sized window that STARTS inside the first node:
    // in range, aligned, forward — and an overlap the mark refuses
    uint8_t * base = file + graphdemo::kTableCookedHeaderBytes;
    graphdemo::Scene * root = (graphdemo::Scene *) base;
    root->head.value += 8;
    CHECK( graphdemo::SceneOpen( file, need ) == NULL );
    free( file );
}

// ---- B2: Lock and Cook are deterministic on a DIRTIED heap ----
//
// Lock and Cook memcpy whole nodes, struct PADDING included. Value-initialising
// a node zeroes its members and not its padding, so before the arena's segments
// were zeroed this test passed only when the allocator happened to hand back
// fresh zero pages — and a cooked FILE carried heap bytes to disk.

static void dirty_the_heap( int pattern )
{
    void * blocks[6];
    for ( int i = 0; i < 6; i++ )
    {
        blocks[i] = malloc( 4u << 20 );
        memset( blocks[i], pattern, 4u << 20 );
    }
    for ( int i = 0; i < 6; i++ ) { free( blocks[i] ); }
}

static void test_lock_deterministic_on_dirty_heap()
{
    dirty_the_heap( 0xA5 );
    graphdemo::SceneBuilder first;
    build_scene( first );
    CHECK( first.Lock() );
    int64_t bytes = first.RegionBytes();
    uint8_t * saved = (uint8_t *) malloc( (size_t) bytes );
    memcpy( saved, first.Region(), (size_t) bytes );
    int64_t cook_bytes = graphdemo::SceneCookMeasure( first );
    uint8_t * cooked_first = (uint8_t *) malloc( (size_t) cook_bytes );
    CHECK( graphdemo::SceneCook( first, cooked_first, cook_bytes ) == cook_bytes );

    dirty_the_heap( 0x5C ); // a DIFFERENT pattern, so any leak shows as a diff
    graphdemo::SceneBuilder second;
    build_scene( second );
    CHECK( second.Lock() );
    CHECK( second.RegionBytes() == bytes );
    CHECK( memcmp( saved, second.Region(), (size_t) bytes ) == 0 );

    uint8_t * cooked_second = (uint8_t *) malloc( (size_t) cook_bytes );
    CHECK( graphdemo::SceneCook( second, cooked_second, cook_bytes ) == cook_bytes );
    CHECK( memcmp( cooked_first, cooked_second, (size_t) cook_bytes ) == 0 );

    // and no padding byte carries heap content: the region's tail padding of
    // the root's string is zero, whatever the allocator handed back
    const uint8_t * region = second.Region();
    for ( int64_t i = (int64_t) offsetof( graphdemo::Scene, name ) + 6;
          i < (int64_t) offsetof( graphdemo::Scene, name ) + 24; i++ )
    {
        CHECK( region[i] == 0 );
    }
    free( saved );
    free( cooked_first );
    free( cooked_second );
}

// ---- C1: the four walks agree about what depth costs ----
//
// By-value nesting charges depth in NEITHER walk, so a chain that Locks and
// Cooks is a chain the wire accepts. Before the fix, measure/save/load charged
// a by-value nesting and pack/cook/open did not, and a structure reachable
// through Scene::ground was lockable and cookable but unsaveable.

static void test_depth_agrees_through_by_value_nesting()
{
    graphdemo::SceneBuilder builder;
    // the chain hangs off `ground`, a VARIABLE table nested BY VALUE
    graphdemo::TableSlot<graphdemo::ListNode> head = builder.Alloc<graphdemo::ListNode>();
    head->value = 0;
    builder.GetRoot()->ground.head = head;
    graphdemo::ListNode * tail = head;
    for ( int i = 1; i < graphdemo::kTableMaxDepth - 1; i++ ) // 127 nodes
    {
        graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
        node->value = i;
        tail->next = node;
        tail = node;
    }

    // every walk accepts it: measure, save, pack (Lock), cook, open, load
    int64_t need = graphdemo::SceneMeasure( builder );
    CHECK( need > 0 );
    uint8_t * wire = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneSave( builder, wire, need ) == need );
    CHECK( builder.Lock() );
    int64_t cook_bytes = graphdemo::SceneCookMeasure( builder );
    CHECK( cook_bytes > 0 );
    uint8_t * cooked = (uint8_t *) malloc( (size_t) cook_bytes );
    CHECK( graphdemo::SceneCook( builder, cooked, cook_bytes ) == cook_bytes );
    CHECK( graphdemo::SceneOpen( cooked, cook_bytes ) != NULL );

    int64_t region_need = graphdemo::SceneLoadMeasure( wire, need );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport report;
    const graphdemo::Scene * loaded = graphdemo::SceneLoad( region, region_need, wire, need, &report );
    CHECK( loaded != NULL && !report.malformed ); // the wire agrees with Lock
    int walked = 0;
    for ( const graphdemo::ListNode * node = graphdemo::ListNodeAt( loaded->ground.head );
          node != NULL; node = graphdemo::ListNodeAt( node->next ) )
    {
        CHECK( node->value == walked );
        walked++;
    }
    CHECK( walked == graphdemo::kTableMaxDepth - 1 );
    free( wire );
    free( cooked );
    free( region );
}

// ---- C2: Open bounds the companions of by-value nested FIXED tables ----

static void test_open_bounds_nested_fixed_companions()
{
    graphdemo::SceneBuilder builder;
    build_scene( builder );
    CHECK( builder.Lock() );
    int64_t need = graphdemo::SceneCookMeasure( builder );
    uint8_t * file = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneCook( builder, file, need ) == need );
    CHECK( graphdemo::SceneOpen( file, need ) != NULL );

    // Meta is FIXED-SIZE and nested BY VALUE in Scene. Its tag_length bounds
    // any walk of tag[8]; an unbounded one is the over-read.
    graphdemo::Scene * root = (graphdemo::Scene *) ( file + graphdemo::kTableCookedHeaderBytes );
    int32_t good = root->meta.tag_length;
    root->meta.tag_length = 30000;
    CHECK( graphdemo::SceneOpen( file, need ) == NULL );
    root->meta.tag_length = -1;
    CHECK( graphdemo::SceneOpen( file, need ) == NULL );
    root->meta.tag_length = good;
    CHECK( graphdemo::SceneOpen( file, need ) != NULL );

    // the same for a fixed table reached through a POINTER
    graphdemo::Settings * settings = (graphdemo::Settings *) graphdemo::SettingsAt( root->settings );
    CHECK( settings != NULL );
    settings->label_length = 9999;
    CHECK( graphdemo::SceneOpen( file, need ) == NULL );
    free( file );
}

// ---- the cooked header's reserved words are reserved ----

static void test_open_refuses_dirty_reserved()
{
    graphdemo::SceneBuilder builder;
    build_scene( builder );
    CHECK( builder.Lock() );
    int64_t need = graphdemo::SceneCookMeasure( builder );
    uint8_t * file = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneCook( builder, file, need ) == need );
    CHECK( graphdemo::SceneOpen( file, need ) != NULL );
    for ( int64_t at = 12; at < graphdemo::kTableCookedHeaderBytes; at++ )
    {
        file[at] = 0x7F; // a writer that used a reserved word
        CHECK( graphdemo::SceneOpen( file, need ) == NULL );
        file[at] = 0;
    }
    CHECK( graphdemo::SceneOpen( file, need ) != NULL );
    free( file );
}

// ---- C3: the reflection surface is immutable constant data ----

static void test_descriptors_are_constant()
{
    // no lazy link, so a self-referential target is already resolved on the
    // first read from any thread — the descriptors carry no mutable state
    const graphdemo::TableTypeInfo * list = graphdemo::ListNodeTableType();
    CHECK( graph_field( list, "next" )->table == list );
    CHECK( graphdemo::ListNodeTableType() == list ); // one instance, always
    // reading from several threads at once is a plain read of constant data
    std::vector<std::thread> readers;
    for ( int t = 0; t < 4; t++ )
    {
        readers.emplace_back( [list]() {
            for ( int i = 0; i < 2000; i++ )
            {
                CHECK( graphdemo::ListNodeTableType() == list );
                CHECK( graphdemo::SceneTableType()->variable );
            }
        } );
    }
    for ( auto & reader : readers ) { reader.join(); }
}


// ---- R1: a multi-file pointered unit, across every kind of member ----
//
// The gap that let the defect through: `Album` nests a plain TYPE and a FIXED
// table declared in Parts.schema — a file that declares no variable table and
// owns no pointer target — plus a VARIABLE table from Marks.schema. The Open
// walk for each lives in its DECLARING file's header, so a file-scoped
// emission rule leaves Parts's two undefined and the referencing header will
// not compile. Colour is native-mapped as well, so the walk is reached through
// a derived-to-base conversion.

static void test_cross_file_pointer_unit()
{
    graphdemo::AlbumBuilder builder;
    graphdemo::Album * album = builder.GetRoot();
    set_string( album->name, album->name_length, "cross" );

    // a native-mapped TYPE from another file: the storage speaks ::ColourMath
    album->tint = ColourMath( 10, 20, 30 );
    CHECK( album->tint.packed() == 0x0A141Eu );

    // a FIXED table from a file with no variable table of its own
    set_string( album->stamp.tag, album->stamp.tag_length, "s1" );
    album->stamp.seq = 42;

    // a VARIABLE table from a third file, nested by value AND pointed at
    set_string( album->marker.label, album->marker.label_length, "by-val" );
    graphdemo::TableSlot<graphdemo::Tally> tally = builder.Alloc<graphdemo::Tally>();
    tally->hits = 7;
    album->marker.note = tally;

    graphdemo::TableSlot<graphdemo::Marker> pinned = builder.Alloc<graphdemo::Marker>();
    set_string( pinned->label, pinned->label_length, "pinned" );
    graphdemo::TableSlot<graphdemo::Tally> pinned_tally = builder.Alloc<graphdemo::Tally>();
    pinned_tally->hits = 9;
    pinned->note = pinned_tally;
    album->pin = pinned;

    graphdemo::TableSlot<graphdemo::ListNode> head = builder.Alloc<graphdemo::ListNode>();
    head->value = 5;
    album->head = head;

    // the wire, exactly measured
    int64_t need = graphdemo::AlbumMeasure( builder );
    CHECK( need > 0 );
    uint8_t wire[2048];
    CHECK( graphdemo::AlbumSave( builder, wire, sizeof( wire ) ) == need );
    CHECK( graphdemo::AlbumSave( builder, wire, need ) == need );
    CHECK( graphdemo::AlbumSave( builder, wire, need - 1 ) == -1 );

    CHECK( builder.Lock() );
    const graphdemo::Album * locked = builder.AsConst();
    CHECK( locked != NULL );

    // every cross-file member survives the compaction
    CHECK( strcmp( locked->name, "cross" ) == 0 );
    CHECK( locked->tint.r == 10 && locked->tint.g == 20 && locked->tint.b == 30 );
    CHECK( locked->tint.packed() == 0x0A141Eu ); // the native behaviour still rides
    CHECK( strcmp( locked->stamp.tag, "s1" ) == 0 && locked->stamp.seq == 42 );
    CHECK( strcmp( locked->marker.label, "by-val" ) == 0 );
    CHECK( graphdemo::TallyAt( locked->marker.note )->hits == 7 );
    CHECK( strcmp( graphdemo::MarkerAt( locked->pin )->label, "pinned" ) == 0 );
    CHECK( graphdemo::TallyAt( graphdemo::MarkerAt( locked->pin )->note )->hits == 9 );
    CHECK( graphdemo::ListNodeAt( locked->head )->value == 5 );

    // and the round trip through the wire
    int64_t region_need = graphdemo::AlbumLoadMeasure( wire, need );
    CHECK( region_need == builder.RegionBytes() );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport report;
    const graphdemo::Album * loaded = graphdemo::AlbumLoad( region, region_need, wire, need, &report );
    CHECK( loaded != NULL );
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && !report.malformed );
    CHECK( loaded->tint.b == 30 && loaded->stamp.seq == 42 );
    CHECK( graphdemo::TallyAt( loaded->marker.note )->hits == 7 );
    CHECK( graphdemo::TallyAt( graphdemo::MarkerAt( loaded->pin )->note )->hits == 9 );
    free( region );

    // ---- and Open bounds the CROSS-FILE members' companions ----
    int64_t cook_need = graphdemo::AlbumCookMeasure( builder );
    uint8_t * file = (uint8_t *) malloc( (size_t) cook_need );
    CHECK( graphdemo::AlbumCook( builder, file, cook_need ) == cook_need );
    CHECK( graphdemo::AlbumOpen( file, cook_need ) != NULL );

    graphdemo::Album * forged = (graphdemo::Album *) ( file + graphdemo::kTableCookedHeaderBytes );

    // a FIXED table declared in a file with no variable table at all
    int32_t good_tag = forged->stamp.tag_length;
    forged->stamp.tag_length = 30000;
    CHECK( graphdemo::AlbumOpen( file, cook_need ) == NULL );
    forged->stamp.tag_length = good_tag;

    // a VARIABLE table declared in a third file, nested by value
    int32_t good_label = forged->marker.label_length;
    forged->marker.label_length = -4;
    CHECK( graphdemo::AlbumOpen( file, cook_need ) == NULL );
    forged->marker.label_length = good_label;

    // its pointer, out of the region
    uint32_t good_note = forged->marker.note.value;
    forged->marker.note.value = 0x7FFFFFF0u;
    CHECK( graphdemo::AlbumOpen( file, cook_need ) == NULL );
    forged->marker.note.value = good_note;

    CHECK( graphdemo::AlbumOpen( file, cook_need ) != NULL ); // nothing else broke
    free( file );
}

// ---- optional fields: `?T` (SPEC-TABLES.md §2.3) ----
// ---- PRESENCE decides whether the field rides, never content. ----

static void test_optional_round_trip()
{
    uint8_t wire[1024];

    // ABSENT: nothing rides at all, and a fresh load says absent with the
    // value at its declared defaults
    tblv1::Cfg none;
    int64_t bytes = tblv1::CfgSave( none, wire, sizeof( wire ) );
    CHECK( bytes == 2 ); // the terminator alone: every field is default or absent

    tblv1::Cfg out;
    out.tier_present = true;              // junk the prefill must erase
    out.tier = 99;
    tblv1::TableReport absent_report;
    CHECK( tblv1::CfgLoad( out, wire, bytes, &absent_report ) );
    CHECK( !absent_report.malformed && absent_report.unknown == 0 );
    CHECK( !out.extra_present && !out.tier_present && !out.mark_present );
    CHECK( out.extra.factor == 2.5f );    // absent reads as the declared default
    CHECK( out.tier == 0 );
    CHECK( out.mark == tblv1::Grade::None );

    // PRESENT with content
    tblv1::Cfg set;
    set.extra_present = true;
    set.extra.factor = 8.5f;
    set.tier_present = true;
    set.tier = 42;
    set.mark_present = true;
    set.mark = tblv1::Grade::Gold;
    bytes = tblv1::CfgSave( set, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( set ) );

    tblv1::TableReport set_report;
    tblv1::Cfg back;
    CHECK( tblv1::CfgLoad( back, wire, bytes, &set_report ) );
    CHECK( !set_report.malformed && set_report.unknown == 0 && set_report.kind_mismatch == 0 );
    CHECK( back.extra_present && back.extra.factor == 8.5f );
    CHECK( back.tier_present && back.tier == 42 );
    CHECK( back.mark_present && back.mark == tblv1::Grade::Gold );

    // PRESENT and entirely default — the case content-based elision would
    // lose. Presence is not content: the field rides with nothing to say, and
    // it reads back present.
    tblv1::Cfg empty_present;
    empty_present.extra_present = true;   // factor stays 2.5, its declared default
    empty_present.tier_present = true;    // value stays 0
    empty_present.mark_present = true;    // value stays None
    int64_t need = tblv1::CfgMeasure( empty_present );
    CHECK( need > 2 );
    bytes = tblv1::CfgSave( empty_present, wire, sizeof( wire ) );
    CHECK( bytes == need );

    tblv1::TableReport empty_report;
    tblv1::Cfg empty_back;
    CHECK( tblv1::CfgLoad( empty_back, wire, bytes, &empty_report ) );
    CHECK( !empty_report.malformed && empty_report.unknown == 0 );
    CHECK( empty_back.extra_present );    // <- the assertion elision would break
    CHECK( empty_back.tier_present );
    CHECK( empty_back.mark_present );
    CHECK( empty_back.extra.factor == 2.5f );
    CHECK( empty_back.tier == 0 );
    CHECK( empty_back.mark == tblv1::Grade::None );

    check_exact_capacity( empty_present, tblv1::CfgMeasure, tblv1::CfgSave );
    check_exact_capacity( set, tblv1::CfgMeasure, tblv1::CfgSave );
}

// ---- ?T, *T and a plain nesting are ONE framing: the three-way evolution
// ---- (SPEC-TABLES.md §2.3, §3.1). P1 nests by value, P2 points, P3 marks
// ---- optional, and every pair reads both directions.

static void test_optional_three_way_evolution()
{
    uint8_t wire[512];
    uint8_t other[512];

    // the bytes themselves: the same field, written three ways, is the same
    // field on the wire
    tblp1::Chain by_value;
    set_string( by_value.name, by_value.name_length, "one" );
    by_value.link.value = 66;
    int64_t w_value = tblp1::ChainSave( by_value, wire, sizeof( wire ) );
    CHECK( w_value > 0 && w_value == tblp1::ChainMeasure( by_value ) );

    tblp3::Chain optional;
    set_string( optional.name, optional.name_length, "one" );
    optional.link_present = true;
    optional.link.value = 66;
    int64_t w_opt = tblp3::ChainSave( optional, other, sizeof( other ) );
    CHECK( w_opt == w_value );
    CHECK( memcmp( wire, other, (size_t) w_value ) == 0 );

    tblp2::ChainBuilder builder;
    tblp2::Chain * root = builder.GetRoot();
    set_string( root->name, root->name_length, "one" );
    tblp2::TableSlot<tblp2::Link> node = builder.Alloc<tblp2::Link>();
    node->value = 66;
    root->link = node;
    int64_t w_ptr = tblp2::ChainSave( builder, other, sizeof( other ) );
    CHECK( w_ptr == w_value );
    CHECK( memcmp( wire, other, (size_t) w_value ) == 0 );

    // ?T -> by value: the optional's body decodes as a nesting
    int64_t wrote = tblp3::ChainSave( optional, wire, sizeof( wire ) );
    tblp1::Chain into_value;
    tblp1::TableReport r1;
    CHECK( tblp1::ChainLoad( into_value, wire, wrote, &r1 ) );
    CHECK( !r1.malformed && r1.unknown == 0 && r1.kind_mismatch == 0 );
    CHECK( into_value.link.value == 66 && strcmp( into_value.name, "one" ) == 0 );

    // ?T -> *T: the same body allocates one node
    int64_t region_need = tblp2::ChainLoadMeasure( wire, wrote );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    tblp2::TableReport r2;
    const tblp2::Chain * into_pointer = tblp2::ChainLoad( region, region_need, wire, wrote, &r2 );
    CHECK( into_pointer != NULL && !r2.malformed && r2.unknown == 0 );
    const tblp2::Link * link = tblp2::LinkAt( into_pointer->link );
    CHECK( link != NULL && link->value == 66 );
    free( region );

    // by value -> ?T: a nested body that rode lands PRESENT
    wrote = tblp1::ChainSave( by_value, wire, sizeof( wire ) );
    tblp3::Chain from_value;
    tblp3::TableReport r3;
    CHECK( tblp3::ChainLoad( from_value, wire, wrote, &r3 ) );
    CHECK( !r3.malformed && r3.unknown == 0 && r3.kind_mismatch == 0 );
    CHECK( from_value.link_present && from_value.link.value == 66 );

    // *T -> ?T: a non-null pointee lands PRESENT
    wrote = tblp2::ChainSave( builder, wire, sizeof( wire ) );
    tblp3::Chain from_pointer;
    tblp3::TableReport r4;
    CHECK( tblp3::ChainLoad( from_pointer, wire, wrote, &r4 ) );
    CHECK( !r4.malformed && r4.unknown == 0 );
    CHECK( from_pointer.link_present && from_pointer.link.value == 66 );

    // ---- and where the three DIVERGE, which is at all-default. A by-value T
    // ---- has no presence bit, so it cannot tell "absent" from "present with
    // ---- nothing to say" and it ELIDES; ?T and *T both ride. Forced, and the
    // ---- one asymmetry of the three-way promise (SPEC-TABLES.md §2.3).
    tblp1::Chain bare_value;
    set_string( bare_value.name, bare_value.name_length, "one" );
    int64_t w_bare_value = tblp1::ChainSave( bare_value, wire, sizeof( wire ) );

    tblp3::Chain bare_optional;
    set_string( bare_optional.name, bare_optional.name_length, "one" );
    bare_optional.link_present = true;              // present, and entirely default
    int64_t w_bare_optional = tblp3::ChainSave( bare_optional, other, sizeof( other ) );
    CHECK( w_bare_optional > w_bare_value );        // presence costs the nine bytes

    tblp2::ChainBuilder bare_builder;
    set_string( bare_builder.GetRoot()->name, bare_builder.GetRoot()->name_length, "one" );
    bare_builder.GetRoot()->link = bare_builder.Alloc<tblp2::Link>(); // non-null, all-default
    static uint8_t third[512];
    int64_t w_bare_pointer = tblp2::ChainSave( bare_builder, third, sizeof( third ) );
    CHECK( w_bare_pointer == w_bare_optional );     // ?T and *T still agree exactly
    CHECK( memcmp( other, third, (size_t) w_bare_optional ) == 0 );

    // and the by-value writer's bytes read back as ABSENT, with a clean report:
    // nothing was lost, the elision IS the meaning
    tblp3::Chain from_bare;
    tblp3::TableReport r7;
    CHECK( tblp3::ChainLoad( from_bare, wire, w_bare_value, &r7 ) );
    CHECK( !r7.malformed && r7.unknown == 0 && r7.kind_mismatch == 0 );
    CHECK( !from_bare.link_present );

    // a NULL pointer and an ABSENT optional are the same absence
    tblp2::ChainBuilder bare;
    set_string( bare.GetRoot()->name, bare.GetRoot()->name_length, "one" );
    wrote = tblp2::ChainSave( bare, wire, sizeof( wire ) );
    tblp3::Chain from_null;
    tblp3::TableReport r5;
    CHECK( tblp3::ChainLoad( from_null, wire, wrote, &r5 ) );
    CHECK( !r5.malformed && !from_null.link_present );

    // and a PRESENT all-default optional reads back as a non-null pointer to
    // an empty node: presence survives the trip in both languages
    tblp3::Chain empty_present;
    set_string( empty_present.name, empty_present.name_length, "one" );
    empty_present.link_present = true;
    wrote = tblp3::ChainSave( empty_present, wire, sizeof( wire ) );
    region_need = tblp2::ChainLoadMeasure( wire, wrote );
    region = (uint8_t *) malloc( (size_t) region_need );
    tblp2::TableReport r6;
    const tblp2::Chain * empty_pointer = tblp2::ChainLoad( region, region_need, wire, wrote, &r6 );
    CHECK( empty_pointer != NULL );
    CHECK( tblp2::LinkAt( empty_pointer->link ) != NULL ); // NOT null: presence rode
    free( region );
}

// ---- enum-keyed arrays: `[E]T` (SPEC-TABLES.md §2.4, §3.2) ----

static void test_keyed_round_trip()
{
    tabledemo::KeyedConfig cfg;
    tabledemo::TeamConfig & blue = cfg.teams[tabledemo::Team::Blue];
    blue.spawn_count = 8;
    set_string( blue.banner, blue.banner_length, "blue" );

    tabledemo::HullConfig & gunship = cfg.hulls[tabledemo::Hull::Gunship];
    gunship.health = 400.0f;
    tabledemo::TurretConfig & missile = gunship.turrets[tabledemo::Weapon::Missile];
    missile.damage = 75.0f;
    missile.gunner_present = true;
    missile.gunner.reaction = 0.05f;

    // ScoreBoard is a `type`, so its keyed field keeps its PACKET storage — a
    // raw array indexed by the value, with no keyed accessor. The WIRE is
    // keyed either way (SPEC-TABLES.md §2.4).
    cfg.scores.per_team[int32_t( tabledemo::Team::Red )] = 1200;

    uint8_t wire[8192];
    int64_t bytes = tabledemo::KeyedConfigSave( cfg, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tabledemo::KeyedConfigMeasure( cfg ) );
    check_exact_capacity( cfg, tabledemo::KeyedConfigMeasure, tabledemo::KeyedConfigSave );

    tabledemo::TableReport report;
    tabledemo::KeyedConfig back;
    CHECK( tabledemo::KeyedConfigLoad( back, wire, bytes, &report ) );
    CHECK( !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 );

    CHECK( back.teams[tabledemo::Team::Blue].spawn_count == 8 );
    CHECK( strcmp( back.teams[tabledemo::Team::Blue].banner, "blue" ) == 0 );
    // a slot nobody set never rode, and reads as its declared default
    CHECK( back.teams[tabledemo::Team::Red].spawn_count == 4 );
    // slot 0 is None's: it never rides and the accessor refuses to name it,
    // so it stays at the declared defaults forever
    CHECK( back.teams.slots[0].spawn_count == 4 );
    CHECK( back.hulls[tabledemo::Hull::Gunship].health == 400.0f );
    CHECK( back.hulls[tabledemo::Hull::Interceptor].health == 100.0f );
    // keyed arrays nest, and an optional inside one survives the trip
    const tabledemo::TurretConfig & back_missile =
        back.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Missile];
    CHECK( back_missile.damage == 75.0f );
    CHECK( back_missile.gunner_present && back_missile.gunner.reaction == 0.05f );
    CHECK( !back.hulls[tabledemo::Hull::Gunship]
                .turrets[tabledemo::Weapon::Cannon].gunner_present );
    // a `type`'s keyed array rides keyed too
    CHECK( back.scores.per_team[int32_t( tabledemo::Team::Red )] == 1200 );
    CHECK( back.scores.per_team[int32_t( tabledemo::Team::Blue )] == 0 );

    // an all-default keyed array elides whole
    tabledemo::KeyedConfig empty;
    CHECK( tabledemo::KeyedConfigMeasure( empty ) == 2 );
}

// ---- the edit that breaks a positional slot array: V2 REMOVES Gamma and
// ---- INSERTS Omega in the middle, so Beta slides from ordinal 2 to 3. Every
// ---- surviving slot lands by NAME, in both directions.

static void test_keyed_evolution_old_data()
{
    tblv1::Cfg v1;
    v1.bank[tblv1::Slot::Alpha].power = 10;
    v1.bank[tblv1::Slot::Beta].power = 20;   // ordinal 2 here, 3 in V2
    v1.bank[tblv1::Slot::Gamma].power = 30;  // V2 has no name for it
    v1.bank[tblv1::Slot::Delta].power = 40;
    set_string( v1.bank[tblv1::Slot::Beta].label,
                v1.bank[tblv1::Slot::Beta].label_length, "b" );
    v1.tokens[tblv1::Slot::Beta] = 7;
    v1.ranks[tblv1::Slot::Beta] = tblv1::Grade::Gold;

    uint8_t wire[2048];
    int64_t bytes = tblv1::CfgSave( v1, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( v1 ) );
    check_exact_capacity( v1, tblv1::CfgMeasure, tblv1::CfgSave );

    tblv2::TableReport report;
    tblv2::Cfg out;
    CHECK( tblv2::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed && report.kind_mismatch == 0 );
    CHECK( report.unknown == 1 ); // Gamma's slot: a key this reader cannot name

    CHECK( out.bank[tblv2::Slot::Alpha].power == 10 );
    CHECK( out.bank[tblv2::Slot::Beta].power == 20 ); // <- moved 2 -> 3
    CHECK( strcmp( out.bank[tblv2::Slot::Beta].label, "b" ) == 0 );
    // Omega holds ordinal 2 in V2: under a positional encoding Beta's slot
    // would land here, silently
    CHECK( out.bank[tblv2::Slot::Omega].power == 0 );
    CHECK( out.bank[tblv2::Slot::Delta].power == 40 );
    CHECK( out.bank[tblv2::Slot::Sigma].power == 0 ); // a slot V1 never had
    CHECK( out.bank.slots[0].power == 0 ); // slot 0 is None's: never written, never named
    CHECK( out.tokens[tblv2::Slot::Beta] == 7 );
    CHECK( out.tokens[tblv2::Slot::Omega] == 0 );
    CHECK( out.ranks[tblv2::Slot::Beta] == tblv2::Grade::Gold );
    CHECK( out.ranks[tblv2::Slot::Omega] == tblv2::Grade::None );
}

static void test_keyed_evolution_new_data()
{
    tblv2::Cfg v2;
    v2.bank[tblv2::Slot::Alpha].power = 1;
    v2.bank[tblv2::Slot::Omega].power = 2; // V1 has no name for it
    v2.bank[tblv2::Slot::Beta].power = 3;  // ordinal 3 here, 2 in V1
    v2.bank[tblv2::Slot::Delta].power = 4;
    v2.bank[tblv2::Slot::Sigma].power = 5; // nor for it
    v2.tokens[tblv2::Slot::Sigma] = 9;

    uint8_t wire[2048];
    int64_t bytes = tblv2::CfgSave( v2, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv2::CfgMeasure( v2 ) );

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, bytes, &report ) );
    CHECK( !report.malformed && report.kind_mismatch == 0 );
    CHECK( report.unknown == 3 ); // Omega and Sigma in bank, Sigma in tokens

    CHECK( out.bank[tblv1::Slot::Alpha].power == 1 );
    CHECK( out.bank[tblv1::Slot::Beta].power == 3 );  // <- moved 3 -> 2
    CHECK( out.bank[tblv1::Slot::Gamma].power == 0 ); // removed in V2
    CHECK( out.bank[tblv1::Slot::Delta].power == 4 );
    CHECK( out.tokens[tblv1::Slot::Beta] == 0 );
}

// ---- a POSITIONAL array and a KEYED one are different KINDS, so the edit
// ---- between them is reported, never misdecoded. V1 spells `ledger` as
// ---- [Grade.Max + 1]int32 and V2 spells the same field [Grade]int32: two
// ---- incompatible bodies under one field id, and the kind byte is what keeps
// ---- them apart (SPEC-TABLES.md §3.2).

static void test_keyed_versus_positional_is_a_kind_mismatch()
{
    uint8_t wire[1024];

    // POSITIONAL writer -> KEYED reader
    tblv1::Cfg positional;
    positional.ledger[1] = 5;
    positional.ledger[2] = 7;
    int64_t bytes = tblv1::CfgSave( positional, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv1::CfgMeasure( positional ) );

    tblv2::TableReport to_keyed;
    tblv2::Cfg keyed_out;
    CHECK( tblv2::CfgLoad( keyed_out, wire, bytes, &to_keyed ) );
    CHECK( !to_keyed.malformed && to_keyed.unknown == 0 );
    CHECK( to_keyed.kind_mismatch == 1 );  // seen, counted, skipped
    for ( int32_t i = 0; i < 4; i++ )      // and NEVER decoded as slots
    {
        CHECK( keyed_out.ledger.slots[i] == 0 );
    }

    // KEYED writer -> POSITIONAL reader
    tblv2::Cfg keyed;
    keyed.ledger[tblv2::Grade::Gold] = 9;
    bytes = tblv2::CfgSave( keyed, wire, sizeof( wire ) );
    CHECK( bytes > 0 && bytes == tblv2::CfgMeasure( keyed ) );
    CHECK( wire[2] == 16 );                // the keyed body's own kind

    tblv1::TableReport to_positional;
    tblv1::Cfg positional_out;
    CHECK( tblv1::CfgLoad( positional_out, wire, bytes, &to_positional ) );
    CHECK( !to_positional.malformed && to_positional.unknown == 0 );
    CHECK( to_positional.kind_mismatch == 1 );
    for ( int32_t i = 0; i < 3; i++ )
    {
        CHECK( positional_out.ledger[i] == 0 );
    }
}

// ---- a stored key of 0 is DAMAGE, not an unknown name: None is the null key
// ---- and slot 0 is never valid (SPEC-TABLES.md §3.2).

static void test_keyed_none_key_is_malformed()
{
    const tblv1::TableFieldInfo * tokens = v1_field( tblv1::CfgTableType(), "tokens" );
    CHECK( tokens != NULL );

    // one good slot, written by the generator itself
    tblv1::Cfg src;
    src.tokens[tblv1::Slot::Beta] = 7;
    uint8_t wire[512];
    int64_t saved = tblv1::CfgSave( src, wire, sizeof( wire ) );
    CHECK( saved > 2 );

    // a second occurrence carrying ONE pair whose key is 0
    int n = (int) ( saved - 2 );
    le16( wire + n, tokens->id ); n += 2;
    wire[n++] = 16;                          // the keyed kind
    le32( wire + n, 5 + 2 + 4 + 4 ); n += 4; // element kind, count, one pair
    wire[n++] = 4;                           // element kind kI32
    le32( wire + n, 1 ); n += 4;
    le16( wire + n, 0 ); n += 2;             // THE NULL KEY
    le32( wire + n, 4 ); n += 4;
    le32( wire + n, 99 ); n += 4;
    le16( wire + n, 0 ); n += 2;             // the table terminator

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, n, &report ) );
    CHECK( report.malformed );               // damage, not an unknown name
    CHECK( report.unknown == 0 );
    CHECK( out.tokens.slots[0] == 0 );       // and slot 0 was never written
}

// ---- #261: a repeated enum-ARRAY id whose second occurrence carries an
// ---- element no build names must RESET the slot — the prefill cannot stand
// ---- in, because an earlier occurrence already wrote it.

static void test_repeated_id_unnameable_enum_element()
{
    const tblv1::TableFieldInfo * grades = v1_field( tblv1::CfgTableType(), "grades" );
    CHECK( grades != NULL );

    // occurrence one: two nameable variants, written by the generator itself
    tblv1::Cfg src;
    src.grades_count = 2;
    src.grades[0] = tblv1::Grade::Gold;
    src.grades[1] = tblv1::Grade::Bronze;

    uint8_t wire[512];
    int64_t saved = tblv1::CfgSave( src, wire, sizeof( wire ) );
    CHECK( saved > 2 );

    // occurrence two, spliced over the terminator: the same id, two element
    // ids no build names
    int n = (int) ( saved - 2 );
    le16( wire + n, grades->id ); n += 2;
    wire[n++] = 14;                        // kArray
    le32( wire + n, 5 + 2 + 2 ); n += 4;   // body: element kind, count, two u16s
    wire[n++] = 7;                         // element kind kU16
    le32( wire + n, 2 ); n += 4;
    le16( wire + n, 0xBEEF ); n += 2;
    le16( wire + n, 0xBEEF ); n += 2;
    le16( wire + n, 0 ); n += 2;           // the table terminator

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, n, &report ) );
    CHECK( !report.malformed );
    CHECK( report.unknown == 2 ); // two element ids this reader cannot name
    // the LAST occurrence wins, and an element with no name lands on None —
    // never on the variant the FIRST occurrence decoded into that slot
    CHECK( out.grades_count == 2 );
    CHECK( out.grades[0] == tblv1::Grade::None );
    CHECK( out.grades[1] == tblv1::Grade::None );
}

// ---- the two constructs in the VARIABLE class: a keyed array of variable
// ---- tables, and an optional beside them. Every pointer-era walk — the
// ---- region pre-pass, Pack, OpenWalk — has to know both framings.

static void test_keyed_and_optional_in_a_variable_table()
{
    graphdemo::DepotBuilder builder;
    graphdemo::Depot * root = builder.GetRoot();
    set_string( root->name, root->name_length, "depot" );
    root->spare_present = true;
    root->spare.build = 7;

    // two of the three slots carry a chain; Layer is VARIABLE, so each slot's
    // pointee is a node the region has to hold
    for ( int32_t tier = int32_t( graphdemo::Tier::Low ); tier <= int32_t( graphdemo::Tier::High ); tier++ )
    {
        graphdemo::Layer & layer = root->banks.slots[tier];
        layer.depth = tier * 3;
        graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
        node->value = 100 + tier;
        layer.head = node;
    }
    CHECK( builder.Lock() );

    static uint8_t wire[8192];
    int64_t wrote = graphdemo::DepotSave( builder, wire, sizeof( wire ) );
    CHECK( wrote > 0 && wrote == graphdemo::DepotMeasure( builder ) );

    // the region pre-pass reads FRAMING ONLY, and a keyed element's length
    // sits past its variant id: get that wrong and the size is wrong
    int64_t need = graphdemo::DepotLoadMeasure( wire, wrote );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) malloc( (size_t) need );
    graphdemo::TableReport report;
    const graphdemo::Depot * loaded = graphdemo::DepotLoad( region, need, wire, wrote, &report );
    CHECK( loaded != NULL );
    CHECK( !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 );
    CHECK( strcmp( loaded->name, "depot" ) == 0 );
    CHECK( loaded->spare_present && loaded->spare.build == 7 );
    for ( int32_t tier = int32_t( graphdemo::Tier::Low ); tier <= int32_t( graphdemo::Tier::High ); tier++ )
    {
        const graphdemo::Layer & layer = loaded->banks.slots[tier];
        CHECK( layer.depth == tier * 3 );
        const graphdemo::ListNode * node = graphdemo::ListNodeAt( layer.head );
        CHECK( node != NULL && node->value == 100 + tier );
    }
    // slot 0 is None's and is never valid: nothing ever wrote it, nothing
    // ever rode for it, and the accessor refuses to name it
    CHECK( loaded->banks.slots[0].depth == 0 );
    CHECK( graphdemo::ListNodeAt( loaded->banks.slots[0].head ) == NULL );

    // and the cooked form: Pack lays every slot's pointee out, OpenWalk
    // validates them, and the round trip is byte-stable
    int64_t cook_need = graphdemo::DepotCookMeasure( builder );
    uint8_t * cooked = (uint8_t *) malloc( (size_t) cook_need );
    CHECK( graphdemo::DepotCook( builder, cooked, cook_need ) == cook_need );
    const graphdemo::Depot * opened = graphdemo::DepotOpen( cooked, cook_need );
    CHECK( opened != NULL );
    CHECK( graphdemo::ListNodeAt( opened->banks[graphdemo::Tier::High].head )->value == 102 );
    CHECK( opened->spare_present );
    CHECK( graphdemo::DepotSave( opened, wire, sizeof( wire ) ) == wrote );

    free( cooked );
    free( region );
}

// ---- the same ground, RANDOMISED. One pinned shape proves the walks agree
// ---- on one shape; this sweeps which slots ride, how long each slot's chain
// ---- is, the string lengths and the optional's presence, and asserts the
// ---- five properties every shape owes: measure == save, LoadMeasure sizes a
// ---- region Load fits, every field survives, Cook -> Open agrees, and a
// ---- re-save is byte-stable. (Against the pre-fix region pre-pass this fails
// ---- in the thousands; the single pinned shape caught it too, but only just.)

static uint32_t oracle_rand( uint32_t & state )
{
    state ^= state << 13;
    state ^= state >> 17;
    state ^= state << 5;
    return state;
}

static void test_keyed_variable_oracle()
{
    static uint8_t wire[16384];
    static uint8_t again[16384];
    uint32_t state = 0x1234567u;
    const int before = failures; // this sweep reports ITS OWN first failure, not one inherited
    const int kIterations = 30000;

    for ( int iteration = 0; iteration < kIterations; iteration++ )
    {
        graphdemo::DepotBuilder builder;
        graphdemo::Depot * root = builder.GetRoot();

        char name[16];
        int32_t name_len = (int32_t) ( oracle_rand( state ) % 13 );
        for ( int32_t i = 0; i < name_len; i++ ) name[i] = (char) ( 'a' + oracle_rand( state ) % 26 );
        name[name_len] = 0;
        set_string( root->name, root->name_length, name );

        root->spare_present = ( oracle_rand( state ) & 1 ) != 0;
        if ( root->spare_present ) root->spare.build = (int32_t) ( oracle_rand( state ) % 1001 );

        int32_t depths[8] = {};
        int32_t chains[8] = {};
        for ( int32_t slot = 1; slot < 3; slot++ ) // slot 0 is None's: never touched
        {
            if ( ( oracle_rand( state ) & 1 ) == 0 ) continue; // this slot stays default
            graphdemo::Layer & layer = root->banks.slots[slot];
            depths[slot] = (int32_t) ( oracle_rand( state ) % 65 );
            layer.depth = depths[slot];
            chains[slot] = (int32_t) ( oracle_rand( state ) % 4 );
            graphdemo::TableSlot<graphdemo::ListNode> head;
            graphdemo::ListNode * previous = NULL;
            for ( int32_t link = 0; link < chains[slot]; link++ )
            {
                graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
                node->value = slot * 1000 + link;
                if ( previous == NULL ) head = node; else previous->next = node;
                previous = &*node;
            }
            if ( chains[slot] > 0 ) layer.head = head;
        }
        CHECK( builder.Lock() );

        int64_t need = graphdemo::DepotMeasure( builder );
        CHECK( need > 0 && need <= (int64_t) sizeof( wire ) );
        CHECK( graphdemo::DepotSave( builder, wire, need ) == need ); // measure == save, exact capacity

        int64_t region_need = graphdemo::DepotLoadMeasure( wire, need );
        CHECK( region_need > 0 );
        uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
        graphdemo::TableReport report;
        const graphdemo::Depot * loaded = graphdemo::DepotLoad( region, region_need, wire, need, &report );
        CHECK( loaded != NULL );
        CHECK( !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 );
        CHECK( strcmp( loaded->name, name ) == 0 );
        CHECK( loaded->spare_present == root->spare_present );
        for ( int32_t slot = 1; slot < 3; slot++ )
        {
            CHECK( loaded->banks.slots[slot].depth == depths[slot] );
            const graphdemo::ListNode * node = graphdemo::ListNodeAt( loaded->banks.slots[slot].head );
            for ( int32_t link = 0; link < chains[slot]; link++ )
            {
                CHECK( node != NULL && node->value == slot * 1000 + link );
                if ( node == NULL ) break;
                node = graphdemo::ListNodeAt( node->next );
            }
            CHECK( node == NULL ); // and no more than the chain that was built
        }

        // a re-save out of the loaded root is byte-stable
        CHECK( graphdemo::DepotSave( loaded, again, sizeof( again ) ) == need );
        CHECK( memcmp( wire, again, (size_t) need ) == 0 );

        // and the cooked form agrees with the wire one
        int64_t cook_need = graphdemo::DepotCookMeasure( builder );
        uint8_t * cooked = (uint8_t *) malloc( (size_t) cook_need );
        CHECK( graphdemo::DepotCook( builder, cooked, cook_need ) == cook_need );
        const graphdemo::Depot * opened = graphdemo::DepotOpen( cooked, cook_need );
        CHECK( opened != NULL );
        CHECK( graphdemo::DepotSave( opened, again, sizeof( again ) ) == need );
        CHECK( memcmp( wire, again, (size_t) need ) == 0 );

        free( cooked );
        free( region );
        if ( failures > before ) return; // one shape is enough to name; do not print 30000
    }
}

// ---- reflection: an optional's presence companion, and a keyed array's key

static void test_optional_and_keyed_reflection()
{
    const tblv1::TableTypeInfo * cfg = tblv1::CfgTableType();

    const tblv1::TableFieldInfo * extra = v1_field( cfg, "extra" );
    CHECK( extra != NULL );
    CHECK( extra->optional );
    CHECK( extra->present_offset == (uint32_t) offsetof( tblv1::Cfg, extra_present ) );
    CHECK( extra->kind == 13 ); // a table body: the framing *T and a nesting use
    const tblv1::TableFieldInfo * tier = v1_field( cfg, "tier" );
    CHECK( tier != NULL && tier->optional && tier->kind == 4 );
    const tblv1::TableFieldInfo * grade = v1_field( cfg, "grade" );
    CHECK( grade != NULL && !grade->optional && grade->present_offset == 0xffffffffu );

    const tblv1::TableFieldInfo * bank = v1_field( cfg, "bank" );
    CHECK( bank != NULL );
    CHECK( bank->is_array && !bank->counted );
    CHECK( bank->array_bound == 5 );  // Slot.Max + 1: None's slot plus four
    CHECK( bank->count_offset == 0xffffffffu ); // every slot exists: no count
    CHECK( bank->key_type_name != NULL && strcmp( bank->key_type_name, "Slot" ) == 0 );
    // key_name and key_id take the SLOT INDEX, which IS the variant's value:
    // slot 2 is Beta's in V1, and a walker steps [0, array_bound) with no
    // arithmetic of its own (SPEC-TABLES.md §8)
    CHECK( bank->key_name != NULL && strcmp( bank->key_name( 2 ), "Beta" ) == 0 );
    CHECK( bank->key_id != NULL && bank->key_id( 2 ) == field_id( "Beta" ) );
    // SLOT 0 IS MARKED INVALID by the one id no declared name can hold: it is
    // None's slot, it never rides, and indexing it is an error
    CHECK( bank->key_id( 0 ) == 0 );
    CHECK( strcmp( bank->key_name( 0 ), "None" ) == 0 );
    for ( int32_t slot = 1; slot < bank->array_bound; slot++ )
    {
        CHECK( bank->key_id( (uint64_t) slot ) != 0 ); // every other slot is nameable
    }

    // an enum-keyed array of enums carries BOTH vocabularies: the key's and
    // the element's
    const tblv1::TableFieldInfo * ranks = v1_field( cfg, "ranks" );
    CHECK( ranks != NULL && ranks->key_type_name != NULL );
    CHECK( ranks->enum_name != NULL && strcmp( ranks->enum_name( 2 ), "Gold" ) == 0 );
    CHECK( strcmp( ranks->key_name( 1 ), "Alpha" ) == 0 );

    // a POSITIONAL fixed array names no key — the contrast the feature exists for
    const tblv1::TableFieldInfo * tally = v1_field( cfg, "tally" );
    CHECK( tally != NULL && tally->is_array );
    CHECK( tally->key_type_name == NULL && tally->key_name == NULL && tally->key_id == NULL );
}

int main()
{
    test_round_trip();
    test_exact_capacity();
    test_storage_invariants();
    test_bounded_elements();
    test_all_default();
    test_guard();
    test_evolution_old_reader_new_data();
    test_evolution_new_reader_old_data();
    test_evolution_enum_insert_old_data();
    test_evolution_enum_insert_new_data();
    test_evolution_union_insert_old_data();
    test_evolution_union_insert_new_data();
    test_repeated_id_unnameable_variant();
    test_repeated_id_unnameable_enum_element();
    test_evolution_array_bounds();
    test_optional_round_trip();
    test_optional_three_way_evolution();
    test_keyed_round_trip();
    test_keyed_evolution_old_data();
    test_keyed_evolution_new_data();
    test_keyed_versus_positional_is_a_kind_mismatch();
    test_keyed_none_key_is_malformed();
    test_optional_and_keyed_reflection();
    test_keyed_and_optional_in_a_variable_table();
    test_keyed_variable_oracle();
    test_unnameable_enum_refused();
    test_unnameable_enum_element_read();
    test_flags_are_positional();
    test_wide_extents();
    test_clamping();
    test_malformed();
    test_reflection();
    test_cross_file();
    test_parallel_shape();

    test_pointer_lifecycle();
    test_lock_layout_stable();
    test_pointer_alias();
    test_pointer_cycle_refused();
    test_pointer_depth_cap();
    test_builder_grow();
    test_builder_workers();
    test_cooked_form();
    test_pointer_evolution_old_reader_new_data();
    test_pointer_evolution_new_reader_old_data();
    test_pointer_null_and_empty();
    test_pointer_reflection();

    test_open_walk_is_linear();
    test_open_refuses_overlap();
    test_lock_deterministic_on_dirty_heap();
    test_depth_agrees_through_by_value_nesting();
    test_open_bounds_nested_fixed_companions();
    test_open_refuses_dirty_reserved();
    test_descriptors_are_constant();
    test_cross_file_pointer_unit();

    if ( failures > 0 )
    {
        printf( "tables test: %d failure(s)\n", failures );
        return 1;
    }
    printf( "tables test passed\n" );
    return 0;
}
