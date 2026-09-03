// The TABLE-wire conformance test (docs/SPEC-TABLES.md). Three generated units in
// one binary: the tables corpus (tabledemo), and the two-generation evolution
// pair (tblv1/tblv2) whose schemas disagree on purpose. Compiled WITHOUT the
// serialize include path — the Table headers must stand alone.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <new>

#include <sys/wait.h>
#include <unistd.h>

#include <iterator>
#include <thread>
#include <vector>

#include "TablesTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "NestedTable.h"
#include "WideTable.h"
#include "GuardedTable.h"
#include "KeyedTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "GraphTable.h"
#include "PartsTable.h"
#include "MarksTable.h"
#include "P1Table.h"
#include "P2Table.h"
#include "P3Table.h"
#include "JsonKeysTable.h"

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

static const jsonkeys::TableFieldInfo * jsonkeys_field( const jsonkeys::TableTypeInfo * type, const char * name )
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

// a `type` body's keyed array is a PLAIN ARRAY (docs/SPEC-TABLES.md §2.4): no wrapper
// and no accessor, so the STORAGE INDEX is spelled here — the key minus one,
// the same shift TableKeyed does for a table body.
template <typename E>
static int32_t keyed_index( E key ) { return (int32_t) key - 1; }

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
    // u16 variant hashes per element (docs/SPEC-TABLES.md §3)
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
// ---- the following fields' bytes (docs/SPEC-TABLES.md: skipped, NEVER misdecoded)

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

// ---- evolution, both directions (docs/SPEC-TABLES.md: any reader x any data) ----

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
// ---- (docs/SPEC-TABLES.md §5). V2 inserts Silver between Bronze and Gold, so Gold
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
// ---- variant this build cannot name. "Reads as empty" (docs/SPEC-TABLES.md §4) is
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

// ---- an array's BOUND is not wire identity (docs/SPEC-TABLES.md §4). V2 shrinks
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
// ---- are APPENDED AT THE END only (docs/SPEC-TABLES.md §4, §5).

static void test_flags_are_positional()
{
    const tabledemo::TableFieldInfo * perks = demo_field( tabledemo::LoadoutConfigTableType(), "perks" );
    CHECK( perks != NULL );
    CHECK( perks->kind == 9 );            // kU64: the mask's raw storage
    CHECK( perks->variant_id == NULL );   // no per-variant wire id exists to carry
    // the bits DO have names, and the descriptor carries them: enum_max is the
    // highest declared BIT INDEX and enum_name spells one bit (docs/SPEC-TABLES.md
    // §8). The missing variant_id beside a present enum_name is what says
    // "positional vocabulary" — a bit has a name, never a wire id.
    CHECK( perks->enum_max == 2 );
    CHECK( perks->enum_name != NULL );
    CHECK( strcmp( perks->enum_name( 0 ), "Shielded" ) == 0 );
    CHECK( strcmp( perks->enum_name( 1 ), "Cloaked" ) == 0 );
    CHECK( strcmp( perks->enum_name( 2 ), "Turbo" ) == 0 );

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

// ---- extents past 65535: u32 lengths and u32 counts (docs/SPEC-TABLES.md §3) ----

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

    free( buffer );
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
    // are both reachable with no schema files (docs/SPEC-TABLES.md §5, §8)
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
    // and the arms carry their PAYLOAD: where it sits in the union storage and
    // what it looks like, so a walker can enter a union with no schema files
    // (docs/SPEC-TABLES.md §8). Arm 0 is the empty arm and has none.
    CHECK( effect->arms != NULL );
    {
        const tabledemo::TableUnionInfo * arms = effect->arms();
        CHECK( arms->tag_offset == offsetof( tabledemo::Effect, type ) );
        CHECK( arms->tag_size == sizeof( tabledemo::Effect{}.type ) );
        CHECK( arms->arms[0].table == NULL );
        CHECK( arms->arms[1].table == tabledemo::BuffTableType() );
        CHECK( arms->arms[1].offset == offsetof( tabledemo::Effect, buff ) );
        CHECK( arms->arms[2].table == tabledemo::DebuffTableType() );

        // the walk a tool actually does: reach the arm's payload by name
        tabledemo::WeaponConfig w;
        w.effect.type = tabledemo::EffectType::Buff;
        w.effect.buff.multiplier = 3.5f;
        uint8_t * base = (uint8_t *) &w;
        uint8_t tag = *( base + effect->offset + arms->tag_offset );
        CHECK( strcmp( effect->enum_name( tag ), "buff" ) == 0 );
        const void * payload = base + effect->offset + arms->arms[tag].offset;
        const tabledemo::TableFieldInfo * mult = demo_field( arms->arms[tag].table, "multiplier" );
        CHECK( mult != NULL );
        float read = 0.0f;
        memcpy( &read, (const uint8_t *) payload + mult->offset, sizeof( read ) );
        CHECK( read == 3.5f );
    }

    // reset: a generic walker can put any instance back at its declared
    // defaults with no type to spell (docs/SPEC-TABLES.md §8)
    {
        tabledemo::WeaponConfig w;
        w.damage = 999.0f;
        w.homing = true;
        weapon->reset( &w );
        CHECK( w.damage == 21.0f ); // the DECLARED default, not zero
        CHECK( w.homing == false );
    }

    // the text form's key rides beside the name (docs/SPEC-TABLES.md §16.3); with
    // no json attribute in the corpus the two are the same string's content
    CHECK( strcmp( damage->json, "damage" ) == 0 );
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
    // the go-wide pattern from docs/USAGE.md, single-threaded here for
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
// POINTER SEMANTICS (docs/SPEC-TABLES.md §2, §6). Types remain value semantics;
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

    // wire -> const region, sized EXACTLY by the pre-pass. LoadMeasure reports
    // the DATA bytes and the ATTRIBUTION bytes separately (docs/SPEC-TABLES.md
    // §6.3, §6.5), and it is the DATA half a locked region answers with: the
    // node directory is what a LOADED region gains and Lock does not write yet.
    int64_t attribution = 0;
    int64_t region_need = graphdemo::SceneLoadMeasure( wire, wrote, &attribution );
    CHECK( attribution > 0 );
    CHECK( region_need - attribution == builder.RegionBytes() ); // the two forms agree on the data
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

    // a region short of what LoadMeasure asked for REFUSES rather than
    // overrunning, and NULL is the answer that says the CALLER's buffer was
    // wrong — not a partial decode of the writer's data
    uint8_t * tight = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport short_report;
    const graphdemo::Scene * partial = graphdemo::SceneLoad( tight, region_need - 8, wire, wrote, &short_report );
    CHECK( partial == NULL && short_report.malformed );
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
}

// ---- a SHARED node is packed ONCE, and identity survives Lock ----
//
// docs/SPEC-TABLES.md §6.2: Lock's walk carries one entry per node, so it
// terminates in one visit per node and a shared node is packed ONCE. §6.3: a
// node's FIRST reference points forward and every later reference points BACK
// at the one body it already has, so a region delta has no required sign.
//
// THE REGION BYTE COUNT IS THE MEASUREMENT. Two pointers reading equal could
// be an accident of layout; a region exactly the size of a once-packed graph
// could not be produced by a pack that duplicates.

static int64_t scene_bytes() { return graphdemo::TableAlignUp64( (int64_t) sizeof( graphdemo::Scene ) ); }
static int64_t list_node_bytes() { return graphdemo::TableAlignUp64( (int64_t) sizeof( graphdemo::ListNode ) ); }
static int64_t tree_node_bytes() { return graphdemo::TableAlignUp64( (int64_t) sizeof( graphdemo::TreeNode ) ); }

static void test_pointer_shared_node()
{
    // TWO REFERENCES, ONE NODE — the second reference is read from the root
    // itself, so it points FORWARD at a node already placed
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
        CHECK( viaHead == viaAlias ); // ONE node, named twice
        CHECK( viaHead->value == 1234 );
        CHECK( builder.RegionBytes() == scene_bytes() + list_node_bytes() ); // one body, not two
    }

    // A SHARED NODE REACHED THROUGH A CHAIN: the root names it a second time
    // after the chain has already placed it
    {
        graphdemo::SceneBuilder builder;
        graphdemo::Scene * root = builder.GetRoot();
        graphdemo::TableSlot<graphdemo::ListNode> first = builder.Alloc<graphdemo::ListNode>();
        graphdemo::TableSlot<graphdemo::ListNode> shared = builder.Alloc<graphdemo::ListNode>();
        first->value = 1;
        shared->value = 2;
        first->next = shared;
        root->head = first;
        root->alias = shared;

        CHECK( builder.Lock() );
        const graphdemo::Scene * locked = builder.AsConst();
        const graphdemo::ListNode * head = graphdemo::ListNodeAt( locked->head );
        CHECK( head != NULL && head->value == 1 );
        CHECK( graphdemo::ListNodeAt( head->next ) == graphdemo::ListNodeAt( locked->alias ) );
        CHECK( graphdemo::ListNodeAt( locked->alias )->value == 2 );
        CHECK( builder.RegionBytes() == scene_bytes() + 2 * list_node_bytes() );
    }

    // A DIAMOND: the closing reference lives in a node packed AFTER the shared
    // one, so its delta is NEGATIVE — sharing and a back-reference are one
    // fact, and nothing validates a reference by its sign (§6.3)
    {
        graphdemo::SceneBuilder builder;
        graphdemo::Scene * root = builder.GetRoot();
        graphdemo::TableSlot<graphdemo::TreeNode> top = builder.Alloc<graphdemo::TreeNode>();
        graphdemo::TableSlot<graphdemo::TreeNode> left = builder.Alloc<graphdemo::TreeNode>();
        graphdemo::TableSlot<graphdemo::TreeNode> right = builder.Alloc<graphdemo::TreeNode>();
        graphdemo::TableSlot<graphdemo::TreeNode> shared = builder.Alloc<graphdemo::TreeNode>();
        set_string( shared->label, shared->label_length, "shared" );
        top->left = left;
        top->right = right;
        left->left = shared;
        right->left = shared; // the diamond closes
        root->tree = top;

        CHECK( builder.Lock() );
        const graphdemo::Scene * locked = builder.AsConst();
        const graphdemo::TreeNode * t = graphdemo::TreeNodeAt( locked->tree );
        CHECK( t != NULL );
        const graphdemo::TreeNode * l = graphdemo::TreeNodeAt( t->left );
        const graphdemo::TreeNode * r = graphdemo::TreeNodeAt( t->right );
        CHECK( l != NULL && r != NULL );
        CHECK( graphdemo::TreeNodeAt( l->left ) == graphdemo::TreeNodeAt( r->left ) );
        CHECK( strcmp( graphdemo::TreeNodeAt( r->left )->label, "shared" ) == 0 );
        CHECK( r->left.value < 0 ); // the later reference points BACK
        CHECK( builder.RegionBytes() == scene_bytes() + 4 * tree_node_bytes() ); // four nodes, not five

        // A DAG ROUND-TRIPS AS A DAG (docs/SPEC-TABLES.md §3.1): the wire
        // numbers the shared node once, so a loader materializes one node and
        // stores that index in both slots — four records for five references.
        static uint8_t dag_wire[1024];
        const int64_t dag_wrote = graphdemo::SceneSave( builder.AsConst(), dag_wire, sizeof( dag_wire ) );
        CHECK( dag_wrote > 0 );
        int64_t dag_attribution = 0;
        const int64_t dag_need = graphdemo::SceneLoadMeasure( dag_wire, dag_wrote, &dag_attribution );
        CHECK( dag_attribution == 5 * 16 ); // the root and its four nodes
        uint8_t * dag_region = (uint8_t *) malloc( (size_t) dag_need );
        graphdemo::TableReport dag_report;
        const graphdemo::Scene * dag = graphdemo::SceneLoad( dag_region, dag_need, dag_wire, dag_wrote, &dag_report );
        CHECK( dag != NULL && !dag_report.malformed && dag_report.unknown == 0 && dag_report.kind_mismatch == 0 );
        const graphdemo::TreeNode * dt = graphdemo::TreeNodeAt( dag->tree );
        CHECK( graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( dt->left )->left ) ==
               graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( dt->right )->left ) );
        CHECK( strcmp( graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( dt->right )->left )->label, "shared" ) == 0 );
        CHECK( dag_need - dag_attribution == builder.RegionBytes() ); // and the same data bytes
        free( dag_region );

        // a region holding a back-reference still relocates by pure memcpy
        uint8_t * moved = (uint8_t *) malloc( (size_t) builder.RegionBytes() );
        memcpy( moved, builder.Region(), (size_t) builder.RegionBytes() );
        const graphdemo::Scene * copy = (const graphdemo::Scene *) moved;
        const graphdemo::TreeNode * mt = graphdemo::TreeNodeAt( copy->tree );
        CHECK( graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( mt->left )->left ) ==
               graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( mt->right )->left ) );
        free( moved );
    }

    // AND THE WIRE HOLDS IT TOO (docs/SPEC-TABLES.md §3.1): the numbering is by
    // first visit, so a node two slots name takes ONE index and writes ONE
    // record, and a loader materializes one node and stores that index in both.
    {
        graphdemo::SceneBuilder builder;
        graphdemo::Scene * root = builder.GetRoot();
        graphdemo::TableSlot<graphdemo::ListNode> shared = builder.Alloc<graphdemo::ListNode>();
        shared->value = 1234;
        root->head = shared;
        root->alias = shared;
        CHECK( builder.Lock() );

        static uint8_t wire[1024];
        int64_t wrote = graphdemo::SceneSave( builder.AsConst(), wire, sizeof( wire ) );
        CHECK( wrote > 0 );
        int64_t region_need = graphdemo::SceneLoadMeasure( wire, wrote );
        uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
        graphdemo::TableReport report;
        const graphdemo::Scene * loaded = graphdemo::SceneLoad( region, region_need, wire, wrote, &report );
        CHECK( loaded != NULL && !report.malformed );
        CHECK( graphdemo::ListNodeAt( loaded->head )->value == 1234 );
        CHECK( graphdemo::ListNodeAt( loaded->head ) == graphdemo::ListNodeAt( loaded->alias ) );
        free( region );
    }
}

// ---- a data cycle is an ERROR, never a hang ----
//
// Lock now carries the identity map (§3.1), so its refusal is the map's and
// not the depth cap's: a reference to a node whose descent is still OPEN is a
// cycle. The map is what makes a shared node one node, and the same one bit
// is what tells a shared node apart from a cycle.

static void test_pointer_cycle_refused()
{
    // a node pointing at itself
    {
        graphdemo::SceneBuilder builder;
        graphdemo::Scene * root = builder.GetRoot();
        graphdemo::TableSlot<graphdemo::ListNode> loop = builder.Alloc<graphdemo::ListNode>();
        loop->value = 1;
        loop->next = loop;
        root->head = loop;

        static uint8_t buffer[4096];
        CHECK( graphdemo::SceneMeasure( builder ) == -1 );
        CHECK( graphdemo::SceneSave( builder, buffer, sizeof( buffer ) ) == -1 );
        CHECK( !builder.Lock() ); // the compaction refuses too
    }
    // a two-node cycle: the closing reference names a node the walk is still
    // inside, which a shared-node map must not mistake for sharing
    {
        graphdemo::SceneBuilder builder;
        graphdemo::Scene * root = builder.GetRoot();
        graphdemo::TableSlot<graphdemo::ListNode> a = builder.Alloc<graphdemo::ListNode>();
        graphdemo::TableSlot<graphdemo::ListNode> b = builder.Alloc<graphdemo::ListNode>();
        a->value = 1;
        b->value = 2;
        a->next = b;
        b->next = a;
        root->head = a;

        static uint8_t buffer[4096];
        CHECK( graphdemo::SceneMeasure( builder ) == -1 );
        CHECK( graphdemo::SceneSave( builder, buffer, sizeof( buffer ) ) == -1 );
        CHECK( !builder.Lock() );
        CHECK( builder.AsConst() == NULL ); // nothing partial is published
    }
}

// ---- A CHAIN'S LENGTH IS NOT A DEPTH (docs/SPEC-TABLES.md §3.1) ----
//
// The flat node table removed the wire's depth cap: no pointer edge is a
// nesting level, so a chain of any length is a flat list of records and the
// LOAD side is a scan with no traversal bound at all. A 260-node chain is the
// case that could not be saved at all under the nested form, where 129 links
// were already too many.

static void test_pointer_long_chain()
{
    const int count = 260;

    graphdemo::SceneBuilder builder;
    graphdemo::Scene * root = builder.GetRoot();
    graphdemo::TableSlot<graphdemo::ListNode> head = builder.Alloc<graphdemo::ListNode>();
    head->value = 0;
    root->head = head;
    graphdemo::ListNode * tail = head;
    for ( int i = 1; i < count; i++ )
    {
        graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
        node->value = i;
        tail->next = node;
        tail = node;
    }

    const int64_t need = graphdemo::SceneMeasure( builder );
    CHECK( need > 0 );
    uint8_t * wire = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneSave( builder, wire, need ) == need );

    // and back: every value in order, through a region the caller sized
    int64_t attribution = 0;
    const int64_t region_need = graphdemo::SceneLoadMeasure( wire, need, &attribution );
    CHECK( attribution == (int64_t) ( count + 1 ) * 16 ); // one entry a node, the root's included
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport report;
    const graphdemo::Scene * loaded = graphdemo::SceneLoad( region, region_need, wire, need, &report );
    CHECK( loaded != NULL && !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 );
    int walked = 0;
    for ( const graphdemo::ListNode * at = graphdemo::ListNodeAt( loaded->head ); at != NULL; at = graphdemo::ListNodeAt( at->next ) )
    {
        CHECK( at->value == walked );
        walked++;
    }
    CHECK( walked == count );

    // a loaded region re-saves to the same bytes
    uint8_t * again = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneSave( loaded, again, need ) == need );
    CHECK( memcmp( wire, again, (size_t) need ) == 0 );

    // and the compaction takes it too
    CHECK( builder.Lock() );

    free( again );
    free( region );
    free( wire );
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
            graphdemo::ListNode * last = first;
            for ( int i = 1; i < per_thread; i++ )
            {
                graphdemo::TableSlot<graphdemo::ListNode> node = worker.Alloc<graphdemo::ListNode>();
                node->value = t * 1000000 + i;
                last->next = node;
                last = node;
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

    // P1 has `link` as a BY-VALUE nesting, so it meets an INDEX where it wants
    // a body: a kind mismatch, counted, and the field keeps its declared
    // defaults. The node table rides under a reserved id P1 cannot name, so it
    // is skipped by its length and counted UNKNOWN once per transport field —
    // which is what an "a build without kind 17" difference looks like (§4).
    // The rest of the root's body reads on, which is the whole point of writing
    // the table LAST.
    tblp1::Chain out;
    tblp1::TableReport report;
    CHECK( tblp1::ChainLoad( out, wire, wrote, &report ) );
    CHECK( !report.malformed );
    CHECK( report.kind_mismatch == 1 );
    CHECK( report.unknown == 1 );
    CHECK( strcmp( out.name, "chain" ) == 0 );
    CHECK( out.link.value == 0 );      // the declared default: nothing was decoded
    CHECK( out.link.tag_length == 0 );
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

    // P2 has `link` as a POINTER: it meets a BODY where it wants an index, and
    // that is a kind mismatch, counted, with the pointer left null. It is the
    // edit kind 17 exists to make loud — four bytes of index and four bytes of
    // a plausible length are the same four bytes to a shared kind.
    int64_t region_need = tblp2::ChainLoadMeasure( wire, wrote );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    tblp2::TableReport report;
    const tblp2::Chain * out = tblp2::ChainLoad( region, region_need, wire, wrote, &report );
    CHECK( out != NULL );
    CHECK( !report.malformed && report.unknown == 0 );
    CHECK( report.kind_mismatch == 1 );
    CHECK( strcmp( out->name, "aged" ) == 0 );
    CHECK( tblp2::LinkAt( out->link ) == NULL );
    free( region );

    // and into a BUILDER — the tool's path: the same verdict, and the builder
    // is still usable afterwards
    tblp2::ChainBuilder builder;
    tblp2::TableReport builder_report;
    CHECK( tblp2::ChainLoadBuilder( builder, wire, wrote, &builder_report ) );
    CHECK( !builder_report.malformed && builder_report.kind_mismatch == 1 );
    CHECK( strcmp( builder.GetRoot()->name, "aged" ) == 0 );
    CHECK( builder.GetRoot()->link.null() );
    tblp2::TableSlot<tblp2::Link> added = builder.Alloc<tblp2::Link>();
    added->value = 88;
    builder.GetRoot()->link = added;
    CHECK( builder.Lock() );
    CHECK( tblp2::LinkAt( builder.AsConst()->link )->value == 88 );
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
    // the root: an index field (id, kind, u32) and the terminator; the node
    // table: one field (id, kind, L) with the count and one record of an empty
    // body (§3.1)
    CHECK( with_empty == ( 3 + 4 + 2 ) + ( 3 + 4 + 8 + 12 + 2 ) );

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
    CHECK( head->kind == 17 ); // a pointer rides as a u32 NODE INDEX (§3.1)
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

    // Relocatability holds with pointers in the struct, and THE SLOT IS EIGHT
    // BYTES AT EIGHT (docs/SPEC-TABLES.md §6.3, ruled 2026-09-03). In a region it
    // holds a SELF-RELATIVE byte delta, so its width is what bounds one
    // region's reach: at four bytes that was 2 GiB, which is a ceiling a mesh
    // or texture catalogue is exactly the thing to meet. It is SIGNED because
    // a shared node's later references point BACK at the one body it has.
    CHECK( sizeof( graphdemo::TableRef ) == 8 );
    CHECK( alignof( graphdemo::TableRef ) == 8 );
    CHECK( graphdemo::TableRef().value == 0 ); // null in both encodings
    {
        graphdemo::TableRef back;
        back.value = -16;
        CHECK( back.value < 0 ); // a back-reference is an ordinary value here
    }
    // and the STORAGE the compiler's layout model computes is the storage the
    // compiler emitted — the pointer field's offset is what a cook writes at
    CHECK( offsetof( graphdemo::Layer, head ) % 8 == 0 );
}


// ============================================================================
// REGRESSIONS from the adversarial review. Each is the reviewer's own repro,
// reduced to an assertion that goes red if the defect returns.
// ============================================================================

// ---- B2: Lock is deterministic on a DIRTIED heap ----
//
// Lock memcpys whole nodes, struct PADDING included. Value-initialising a node
// zeroes its members and not its padding, so before the arena's segments were
// zeroed this test passed only when the allocator happened to hand back fresh
// zero pages — and the region carried heap bytes.

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

    dirty_the_heap( 0x5C ); // a DIFFERENT pattern, so any leak shows as a diff
    graphdemo::SceneBuilder second;
    build_scene( second );
    CHECK( second.Lock() );
    CHECK( second.RegionBytes() == bytes );
    CHECK( memcmp( saved, second.Region(), (size_t) bytes ) == 0 );

    // and no padding byte carries heap content: the region's tail padding of
    // the root's string is zero, whatever the allocator handed back
    const uint8_t * region = second.Region();
    for ( int64_t i = (int64_t) offsetof( graphdemo::Scene, name ) + 6;
          i < (int64_t) offsetof( graphdemo::Scene, name ) + 24; i++ )
    {
        CHECK( region[i] == 0 );
    }
    free( saved );
}

// ---- C1: the four walks agree about what depth costs ----
//
// By-value nesting charges depth in NEITHER walk, so a chain that Locks is a
// chain the wire accepts. Before the fix, measure/save/load charged a by-value
// nesting and pack did not, and a structure reachable through Scene::ground was
// lockable but unsaveable.

static void test_depth_agrees_through_by_value_nesting()
{
    graphdemo::SceneBuilder builder;
    // the chain hangs off `ground`, a VARIABLE table nested BY VALUE
    graphdemo::TableSlot<graphdemo::ListNode> head = builder.Alloc<graphdemo::ListNode>();
    head->value = 0;
    builder.GetRoot()->ground.head = head;
    graphdemo::ListNode * tail = head;
    const int chain = 300; // past anything a nesting-based form could carry
    for ( int i = 1; i < chain; i++ )
    {
        graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
        node->value = i;
        tail->next = node;
        tail = node;
    }

    // every walk accepts it: measure, save, pack (Lock), load
    int64_t need = graphdemo::SceneMeasure( builder );
    CHECK( need > 0 );
    uint8_t * wire = (uint8_t *) malloc( (size_t) need );
    CHECK( graphdemo::SceneSave( builder, wire, need ) == need );
    CHECK( builder.Lock() );

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
    CHECK( walked == chain );
    free( wire );
    free( region );
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
    int64_t album_attribution = 0;
    int64_t region_need = graphdemo::AlbumLoadMeasure( wire, need, &album_attribution );
    CHECK( region_need - album_attribution == builder.RegionBytes() );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    graphdemo::TableReport report;
    const graphdemo::Album * loaded = graphdemo::AlbumLoad( region, region_need, wire, need, &report );
    CHECK( loaded != NULL );
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && !report.malformed );
    CHECK( loaded->tint.b == 30 && loaded->stamp.seq == 42 );
    CHECK( graphdemo::TallyAt( loaded->marker.note )->hits == 7 );
    CHECK( graphdemo::TallyAt( graphdemo::MarkerAt( loaded->pin )->note )->hits == 9 );
    free( region );
}

// ---- optional fields: `?T` (docs/SPEC-TABLES.md §2.3) ----
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

// ---- T and ?T are ONE framing; *T is its OWN (docs/SPEC-TABLES.md §2.3,
// ---- §3.1). P1 nests by value, P3 marks optional, and those two are wire
// ---- identical in both directions. P2 POINTS, and under the flat node table a
// ---- pointer field rides as a u32 index under kind 17 — so moving a field
// ---- between the two shapes is an ordinary, REPORTED kind mismatch and never
// ---- a silent reinterpretation. That is the whole reason kind 17 is spent.

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

    // and the POINTER is a different framing, by design: seven bytes for the
    // index where the other two carry a body, plus the node table the body
    // moved into
    tblp2::ChainBuilder builder;
    tblp2::Chain * root = builder.GetRoot();
    set_string( root->name, root->name_length, "one" );
    tblp2::TableSlot<tblp2::Link> node = builder.Alloc<tblp2::Link>();
    node->value = 66;
    root->link = node;
    int64_t w_ptr = tblp2::ChainSave( builder, other, sizeof( other ) );
    CHECK( w_ptr > 0 && w_ptr == tblp2::ChainMeasure( builder ) );
    CHECK( w_ptr != w_value );

    // ?T -> by value: the optional's body decodes as a nesting
    int64_t wrote = tblp3::ChainSave( optional, wire, sizeof( wire ) );
    tblp1::Chain into_value;
    tblp1::TableReport r1;
    CHECK( tblp1::ChainLoad( into_value, wire, wrote, &r1 ) );
    CHECK( !r1.malformed && r1.unknown == 0 && r1.kind_mismatch == 0 );
    CHECK( into_value.link.value == 66 && strcmp( into_value.name, "one" ) == 0 );

    // ?T -> *T: a NESTED BODY where an INDEX is declared is a kind mismatch,
    // counted and skipped, and the pointer stays null. The old promise was that
    // this reinterpreted silently; the flat form spends a kind so it cannot.
    int64_t region_need = tblp2::ChainLoadMeasure( wire, wrote );
    uint8_t * region = (uint8_t *) malloc( (size_t) region_need );
    tblp2::TableReport r2;
    const tblp2::Chain * into_pointer = tblp2::ChainLoad( region, region_need, wire, wrote, &r2 );
    CHECK( into_pointer != NULL && !r2.malformed );
    CHECK( r2.kind_mismatch == 1 && r2.unknown == 0 );
    CHECK( tblp2::LinkAt( into_pointer->link ) == NULL );
    CHECK( strcmp( into_pointer->name, "one" ) == 0 ); // the rest of the body reads on
    free( region );

    // by value -> ?T: a nested body that rode lands PRESENT
    wrote = tblp1::ChainSave( by_value, wire, sizeof( wire ) );
    tblp3::Chain from_value;
    tblp3::TableReport r3;
    CHECK( tblp3::ChainLoad( from_value, wire, wrote, &r3 ) );
    CHECK( !r3.malformed && r3.unknown == 0 && r3.kind_mismatch == 0 );
    CHECK( from_value.link_present && from_value.link.value == 66 );

    // *T -> ?T: the INDEX is a kind mismatch where a body is declared, and the
    // node table rides under a reserved id this reader cannot name, so it is
    // skipped by its length and counted UNKNOWN — which is exactly the
    // difference §4 exists to report: "a build without kind 17".
    wrote = tblp2::ChainSave( builder, wire, sizeof( wire ) );
    tblp3::Chain from_pointer;
    tblp3::TableReport r4;
    CHECK( tblp3::ChainLoad( from_pointer, wire, wrote, &r4 ) );
    CHECK( !r4.malformed );
    CHECK( r4.kind_mismatch == 1 && r4.unknown == 1 );
    CHECK( !from_pointer.link_present );
    CHECK( strcmp( from_pointer.name, "one" ) == 0 );

    // ---- and where the three DIVERGE, which is at all-default. A by-value T
    // ---- has no presence bit, so it cannot tell "absent" from "present with
    // ---- nothing to say" and it ELIDES; ?T and *T both ride. Forced, and the
    // ---- one asymmetry of the three-way promise (docs/SPEC-TABLES.md §2.3).
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
    CHECK( w_bare_pointer > w_bare_value );         // a non-null pointer ALWAYS rides

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

    // and a PRESENT all-default optional read as a POINTER is the same reported
    // mismatch as any other: presence rode, and the reader that wanted an index
    // says so rather than inventing a node
    tblp3::Chain empty_present;
    set_string( empty_present.name, empty_present.name_length, "one" );
    empty_present.link_present = true;
    wrote = tblp3::ChainSave( empty_present, wire, sizeof( wire ) );
    region_need = tblp2::ChainLoadMeasure( wire, wrote );
    region = (uint8_t *) malloc( (size_t) region_need );
    tblp2::TableReport r6;
    const tblp2::Chain * empty_pointer = tblp2::ChainLoad( region, region_need, wire, wrote, &r6 );
    CHECK( empty_pointer != NULL );
    CHECK( r6.kind_mismatch == 1 );
    CHECK( tblp2::LinkAt( empty_pointer->link ) == NULL );
    free( region );
}

// ---- enum-keyed arrays: `[E]T` (docs/SPEC-TABLES.md §2.4, §3.2) ----

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
    // keyed either way (docs/SPEC-TABLES.md §2.4).
    cfg.scores.per_team[keyed_index( tabledemo::Team::Red )] = 1200;

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
    // there is no None slot to check: the storage holds one element per named
    // variant and the key k lives at index k-1 (docs/SPEC-TABLES.md §2.4)
    CHECK( sizeof( back.teams ) == 3 * sizeof( tabledemo::TeamConfig ) );
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
    CHECK( back.scores.per_team[keyed_index( tabledemo::Team::Red )] == 1200 );
    CHECK( back.scores.per_team[keyed_index( tabledemo::Team::Blue )] == 0 );

    // an all-default keyed array elides whole
    tabledemo::KeyedConfig empty;
    CHECK( tabledemo::KeyedConfigMeasure( empty ) == 2 );
}

// ---- iteration over the VALID slots (docs/SPEC-TABLES.md §2.4) ----
//
// A consumer of a keyed array wants the WHOLE array, and the shape it wants is
// (key, element) over the slots that can hold data. Iteration is that shape,
// and it is where the key rule lives: the walk yields KEYS 1..E.Max over
// storage 0..E.Max-1, so nothing out here spells a bound, a cast, a shift or
// an E.Max of its own. The accessor REFUSES None in every build, so both
// surfaces are safe in a shipped build; this one is safe without a key at all.

// the sweep every keyed array in the corpus goes through: walk it, and prove
// the keys are exactly 1..E.Max, ascending. Written once, against no
// particular enum, so a keyed array added to the corpus is one line away from
// being covered.
template <typename Keyed>
static int32_t iterate_and_check_keys( const Keyed & keyed )
{
    int32_t seen = 0;
    int32_t expect = 1; // slots arrive in ascending variant order, from 1
    for ( auto [ key, element ] : keyed )
    {
        (void) element;
        CHECK( (int32_t) key != 0 );
        CHECK( (int32_t) key == expect );
        expect++;
        seen++;
    }
    return seen;
}

static void test_keyed_iteration()
{
    tabledemo::KeyedConfig cfg;

    // WRITING through the iteration: the element is a REFERENCE to the slot,
    // so filling a whole keyed array is the same loop as reading one
    int32_t spawn = 10;
    for ( auto [ team, config ] : cfg.teams )
    {
        CHECK( team != tabledemo::Team::None );
        config.spawn_count = spawn++;
    }
    CHECK( cfg.teams[tabledemo::Team::Red].spawn_count == 10 );
    CHECK( cfg.teams[tabledemo::Team::Blue].spawn_count == 11 );
    CHECK( cfg.teams[tabledemo::Team::Green].spawn_count == 12 );

    // reading it back through a CONST keyed array: the same range, const
    // elements
    const tabledemo::KeyedConfig & const_cfg = cfg;
    int32_t total = 0;
    for ( auto [ team, config ] : const_cfg.teams )
    {
        CHECK( team != tabledemo::Team::None );
        total += config.spawn_count;
    }
    CHECK( total == 10 + 11 + 12 );

    // EVERY keyed array in the corpus, iterated, and None yielded by none of
    // them. Team, Hull and Weapon each declare three variants, so each
    // iteration is exactly E.Max long — one slot per NAMED variant.
    CHECK( iterate_and_check_keys( cfg.teams ) == 3 );
    CHECK( iterate_and_check_keys( cfg.hulls ) == 3 );
    for ( auto [ hull, config ] : cfg.hulls )
    {
        CHECK( hull != tabledemo::Hull::None );
        CHECK( iterate_and_check_keys( config.turrets ) == 3 ); // nested
    }

    // the space-shaped corpus: one record per ship type, one threshold per
    // difficulty — the config bin this construct exists for
    tabledemo::PackConfig pack;
    CHECK( iterate_and_check_keys( pack.ships ) == 3 );
    CHECK( iterate_and_check_keys( pack.thresholds ) == 3 );
    for ( auto [ ship_type, entry ] : pack.ships )
    {
        CHECK( ship_type != tabledemo::ShipType::None );
        entry.mass = 2.0f;
    }
    CHECK( pack.ships[tabledemo::ShipType::Bomber].mass == 2.0f );

    // the evolution unit's keyed arrays, BOTH generations: tables, scalars and
    // enums as elements, over an enum whose variants V2 inserts into and
    // removes from. Each array is E.Max long in the generation that declares
    // it — V1's Slot has four variants, V2's five — so the iteration follows
    // the enum without a consumer touching anything.
    tblv1::Cfg v1;
    CHECK( iterate_and_check_keys( v1.bank ) == 4 );
    CHECK( iterate_and_check_keys( v1.tokens ) == 4 );
    CHECK( iterate_and_check_keys( v1.ranks ) == 4 );

    tblv2::Cfg v2;
    CHECK( iterate_and_check_keys( v2.bank ) == 5 );
    CHECK( iterate_and_check_keys( v2.tokens ) == 5 );
    CHECK( iterate_and_check_keys( v2.ranks ) == 5 );
    CHECK( iterate_and_check_keys( v2.ledger ) == 3 );

    // and the VARIABLE class's keyed array, which is the same storage type
    graphdemo::Depot depot;
    CHECK( iterate_and_check_keys( depot.banks ) == 2 );

    // the iterators carry their traits typedefs, so a forward pass over one
    // works without the range-for
    CHECK( std::distance( cfg.teams.begin(), cfg.teams.end() ) == 3 );
    CHECK( std::distance( const_cfg.teams.begin(), const_cfg.teams.end() ) == 3 );

    // a scalar element iterates as a reference too
    for ( auto [ slot, tokens ] : v2.tokens )
    {
        tokens = (int32_t) slot * 10;
    }
    CHECK( v2.tokens[tblv2::Slot::Alpha] == 10 );
    CHECK( v2.tokens[tblv2::Slot::Sigma] == 50 );

    // a value filled by iteration rides and reads back by name
    uint8_t wire[8192];
    int64_t bytes = tabledemo::KeyedConfigSave( cfg, wire, sizeof( wire ) );
    CHECK( bytes > 0 );
    tabledemo::KeyedConfig back;
    tabledemo::TableReport report;
    CHECK( tabledemo::KeyedConfigLoad( back, wire, bytes, &report ) );
    CHECK( !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 );
    for ( auto [ team, config ] : back.teams )
    {
        CHECK( config.spawn_count == 9 + (int32_t) team );
    }
}

// ---- operator[] REFUSES None, and in every build (docs/SPEC-TABLES.md §2.4) ----
//
// The refusal is unconditional — NDEBUG does not remove it — so it is worth a
// test that shows it firing rather than a comment claiming it does. A forked
// child indexes by None and must die; the parent goes red if the child returns
// instead. This unit compiles with asserts LIVE, so it cannot tell the refusal
// from an assert: `make tables-keyed-none-refusal-ndebug` is the half that
// can, and its own negative control proves that gate has teeth.

static void test_keyed_none_index_refused()
{
    fflush( stdout );
    pid_t child = fork();
    if ( child == 0 )
    {
        // the abort message is the point of the child, not of this log
        FILE * quiet = freopen( "/dev/null", "w", stderr );
        (void) quiet;
        tabledemo::KeyedConfig cfg;
        tabledemo::TeamConfig & none = cfg.teams[tabledemo::Team::None];
        none.spawn_count = 1; // never reached: the accessor refused
        _exit( 0 );
    }
    CHECK( child > 0 );
    int status = 0;
    CHECK( waitpid( child, &status, 0 ) == child );
    CHECK( WIFSIGNALED( status ) ); // the refusal ended the child
    if ( WIFEXITED( status ) )
    {
        printf( "FAIL keyed None index: the child returned %d — the assert is gone\n",
                WEXITSTATUS( status ) );
        failures++;
    }
}

// ---- and operator[] REFUSES A KEY PAST Max, the same way (docs/SPEC-TABLES.md §2.4) ----
//
// The storage holds one slot per NAMED variant, so a key above Max names a
// variant this enum does not have and there is no slot for it to land in: the
// same program error as None, refused at the same compare, in every build.
// `make tables-keyed-max-refusal-ndebug` is the half that proves NDEBUG does
// not remove it, and its own negative control proves that gate has teeth.

static void test_keyed_past_max_index_refused()
{
    fflush( stdout );
    pid_t child = fork();
    if ( child == 0 )
    {
        // the abort message is the point of the child, not of this log
        FILE * quiet = freopen( "/dev/null", "w", stderr );
        (void) quiet;
        tabledemo::KeyedConfig cfg;
        tabledemo::Team key = (tabledemo::Team) ( (int32_t) tabledemo::Team::Max + 1 );
        tabledemo::TeamConfig & past = cfg.teams[key];
        past.spawn_count = 1; // never reached: the accessor refused
        _exit( 0 );
    }
    CHECK( child > 0 );
    int status = 0;
    CHECK( waitpid( child, &status, 0 ) == child );
    CHECK( WIFSIGNALED( status ) ); // the refusal ended the child
    if ( WIFEXITED( status ) )
    {
        printf( "FAIL keyed past-Max index: the child returned %d — the refusal is gone\n",
                WEXITSTATUS( status ) );
        failures++;
    }
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
    // there is no None slot: the storage holds one element per named variant
    CHECK( sizeof( out.bank ) == 5 * sizeof( tblv2::Cell ) );
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
// ---- them apart (docs/SPEC-TABLES.md §3.2).

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
    for ( auto [ grade, tokens ] : keyed_out.ledger )  // and NEVER decoded as slots
    {
        (void) grade;
        CHECK( tokens == 0 );
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
// ---- and it keys no slot (docs/SPEC-TABLES.md §3.2).

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

    // a second occurrence carrying TWO pairs: the null key, then a perfectly
    // good one behind it. Damage STOPS the body (§3.2, §4), so the good pair
    // behind the damage must NOT land — the reader keeps what it decoded and
    // reads on past the field's length, it does not step over the bad pair.
    int n = (int) ( saved - 2 );
    le16( wire + n, tokens->id ); n += 2;
    wire[n++] = 16;                              // the keyed kind
    le32( wire + n, 5 + 2 * ( 2 + 4 + 4 ) ); n += 4; // element kind, count, two pairs
    wire[n++] = 4;                               // element kind kI32
    le32( wire + n, 2 ); n += 4;
    le16( wire + n, 0 ); n += 2;                 // THE NULL KEY
    le32( wire + n, 4 ); n += 4;
    le32( wire + n, 99 ); n += 4;
    le16( wire + n, field_id( "Delta" ) ); n += 2; // a good key, behind the damage
    le32( wire + n, 4 ); n += 4;
    le32( wire + n, 42 ); n += 4;
    le16( wire + n, 0 ); n += 2;                 // the table terminator

    tblv1::TableReport report;
    tblv1::Cfg out;
    CHECK( tblv1::CfgLoad( out, wire, n, &report ) );
    CHECK( report.malformed );                   // damage, not an unknown name
    CHECK( report.unknown == 0 );
    CHECK( out.tokens[tblv1::Slot::Delta] == 0 ); // and the body stopped at the damage
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
// ---- region pre-pass, Pack — has to know both framings.

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
        graphdemo::Layer & layer = root->banks[graphdemo::Tier( tier )];
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
        const graphdemo::Layer & layer = loaded->banks[graphdemo::Tier( tier )];
        CHECK( layer.depth == tier * 3 );
        const graphdemo::ListNode * node = graphdemo::ListNodeAt( layer.head );
        CHECK( node != NULL && node->value == 100 + tier );
    }
    // there is no None slot: the storage holds one element per named variant,
    // so nothing was ever reserved for the null key
    CHECK( sizeof( loaded->banks ) == 2 * sizeof( graphdemo::Layer ) );

    // and the PACKED REGION: Lock lays every slot's pointee out, the const
    // form reads them back, and a save from it is byte-stable
    const graphdemo::Depot * packed = builder.AsConst();
    CHECK( packed != NULL );
    CHECK( graphdemo::ListNodeAt( packed->banks[graphdemo::Tier::High].head )->value == 102 );
    CHECK( packed->spare_present );
    CHECK( graphdemo::DepotSave( packed, wire, sizeof( wire ) ) == wrote );

    free( region );
}

// ---- the same ground, RANDOMISED. One pinned shape proves the walks agree
// ---- on one shape; this sweeps which slots ride, how long each slot's chain
// ---- is, the string lengths and the optional's presence, and asserts the
// ---- five properties every shape owes: measure == save, LoadMeasure sizes a
// ---- region Load fits, every field survives, the packed region agrees, and a
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

        // CAPTURED BEFORE Lock(), exactly as name, depths[] and chains[] are:
        // Lock() compacts the arena into the region and releases the mutable
        // life, so `root` dangles from that point on (docs/SPEC-TABLES.md §6.2).
        // Every value an assertion below compares against has to outlive it.
        const bool spare_present = ( oracle_rand( state ) & 1 ) != 0;
        root->spare_present = spare_present;
        if ( spare_present ) root->spare.build = (int32_t) ( oracle_rand( state ) % 1001 );

        int32_t depths[8] = {};
        int32_t chains[8] = {};
        for ( int32_t slot = 1; slot < 3; slot++ ) // the KEYS, 1..Tier.Max
        {
            if ( ( oracle_rand( state ) & 1 ) == 0 ) continue; // this slot stays default
            graphdemo::Layer & layer = root->banks[graphdemo::Tier( slot )];
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
        CHECK( loaded->spare_present == spare_present );
        for ( int32_t slot = 1; slot < 3; slot++ )
        {
            const graphdemo::Layer & layer = loaded->banks[graphdemo::Tier( slot )];
            CHECK( layer.depth == depths[slot] );
            const graphdemo::ListNode * node = graphdemo::ListNodeAt( layer.head );
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

        // and the PACKED REGION agrees with the wire one
        const graphdemo::Depot * packed = builder.AsConst();
        CHECK( packed != NULL );
        CHECK( graphdemo::DepotSave( packed, again, sizeof( again ) ) == need );
        CHECK( memcmp( wire, again, (size_t) need ) == 0 );

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
    CHECK( bank->array_bound == 4 );  // Slot.Max: one slot per named variant
    CHECK( bank->count_offset == 0xffffffffu ); // every slot exists: no count
    CHECK( bank->key_type_name != NULL && strcmp( bank->key_type_name, "Slot" ) == 0 );
    // key_name and key_id are functions of the KEY, not of the storage index:
    // a walker steps [0, array_bound) and asks about index + 1, which is the
    // key that index holds (docs/SPEC-TABLES.md §2.4, §8)
    CHECK( bank->key_name != NULL && strcmp( bank->key_name( 2 ), "Beta" ) == 0 );
    CHECK( bank->key_id != NULL && bank->key_id( 2 ) == field_id( "Beta" ) );
    // None is a KEY the enum has and it names no slot: the reserved id says so
    CHECK( bank->key_id( 0 ) == 0 );
    CHECK( strcmp( bank->key_name( 0 ), "None" ) == 0 );
    for ( int32_t slot = 0; slot < bank->array_bound; slot++ )
    {
        // every STORED slot holds a nameable key, and none of them holds None
        CHECK( bank->key_id( (uint64_t) ( slot + 1 ) ) != 0 );
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

// ---- the SHARED GOLDEN WIRE (docs/SPEC-TABLES.md §3, the cross-language gate) ----
//
// C++ is the reference writer: these instances' encodings are pinned into
// testdata/wire/tables/<name>.bin, and every other table backend byte-compares
// its own Save of THE SAME instance against them, then loads these very bytes.
// A break here under an unchanged schema is stop-the-line, never a quiet
// re-pin — SCHEMA_UPDATE_WIRE_GOLDENS=1 rewrites them deliberately
// (make update-goldens).

static void pin_table_golden( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/tables/%s.bin", name );
    if ( std::getenv( "SCHEMA_UPDATE_WIRE_GOLDENS" ) )
    {
        FILE * f = fopen( path, "wb" );
        if ( f == NULL )
        {
            printf( "FAIL cannot write %s\n", path );
            failures++;
            return;
        }
        fwrite( data, 1, (size_t) bytes, f );
        fclose( f );
        return;
    }
    FILE * f = fopen( path, "rb" );
    if ( f == NULL )
    {
        printf( "FAIL missing table wire golden %s (run: make update-goldens)\n", path );
        failures++;
        return;
    }
    static uint8_t expected[1u << 20];
    size_t n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int64_t) n != bytes || memcmp( expected, data, n ) != 0 )
    {
        printf( "FAIL table wire golden %s: %lld bytes written, %lld pinned\n",
                name, (long long) bytes, (long long) n );
        failures++;
    }
}

// ---- and the goldens READ BACK (docs/SPEC-TABLES.md §3) --------------------------
//
// pin_table_golden proves the WRITER: the bytes this build produces are the
// bytes on disk. This proves the READER against those same files — every
// golden is loaded from disk into a fresh instance and re-saved, and the bytes
// must come back identical.
//
// On a little-endian host that is a round trip through a file. On the
// BIG-ENDIAN leg (Makefile: tables-big-endian) it is the claim itself: the
// files were written by a little-endian build, so a codec that reached for
// host byte order anywhere would read them wrong and write them back
// differently. §3's "little-endian, byte-oriented throughout" is a property of
// the WIRE, not of the machine, and this is where that costs something.

static uint8_t golden_pinned[1u << 20];
static uint8_t golden_again[1u << 20];

static int64_t read_table_golden( const char * name )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/tables/%s.bin", name );
    FILE * f = fopen( path, "rb" );
    if ( f == NULL )
    {
        printf( "FAIL missing table wire golden %s (run: make update-goldens)\n", path );
        failures++;
        return -1;
    }
    size_t n = fread( golden_pinned, 1, sizeof( golden_pinned ), f );
    fclose( f );
    return (int64_t) n;
}

template <typename T, typename Report>
static void reload_table_golden( const char * name,
                                 bool ( *load )( T &, const uint8_t *, int64_t, Report * ),
                                 int64_t ( *save )( const T &, uint8_t *, int64_t ) )
{
    const int64_t pinned = read_table_golden( name );
    if ( pinned < 0 ) return;

    static T value;
    new ( &value ) T(); // the read path's own reset: value-init, in place
    Report report;
    if ( !load( value, golden_pinned, pinned, &report ) || report.malformed )
    {
        printf( "FAIL table wire golden %s does not load\n", name );
        failures++;
        return;
    }
    // its own writer's bytes: nothing in them is unknown, nothing mismatches
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 );

    const int64_t wrote = save( value, golden_again, sizeof( golden_again ) );
    if ( wrote != pinned || memcmp( golden_again, golden_pinned, (size_t) pinned ) != 0 )
    {
        printf( "FAIL table wire golden %s re-saves differently: %lld bytes out, %lld pinned\n",
                name, (long long) wrote, (long long) pinned );
        failures++;
    }
}

// A VARIABLE-class golden reloads through its OWN reader, because there is no
// twin any more: a pointer field rides as a u32 index into the flat node table
// and a by-value nesting rides as a body, so only the writer's own reader can
// answer in bytes. The form is a region and a root pointer, never a value
// (docs/SPEC-TABLES.md §6.2).
static void reload_pointer_golden( const char * name )
{
    const int64_t pinned = read_table_golden( name );
    if ( pinned < 0 ) return;
    const int64_t need = tblp2::ChainLoadMeasure( golden_pinned, pinned );
    uint8_t * region = (uint8_t *) malloc( (size_t) need );
    tblp2::TableReport report;
    const tblp2::Chain * root = tblp2::ChainLoad( region, need, golden_pinned, pinned, &report );
    if ( root == NULL || report.malformed )
    {
        printf( "FAIL table wire golden %s does not load\n", name );
        failures++;
        free( region );
        return;
    }
    // its own writer's bytes: nothing in them is unknown, nothing mismatches —
    // and the reserved node-table id is NOT unknown to a reader holding the
    // numbering (§3.1)
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 );
    const int64_t wrote = tblp2::ChainSave( root, golden_again, sizeof( golden_again ) );
    if ( wrote != pinned || memcmp( golden_again, golden_pinned, (size_t) pinned ) != 0 )
    {
        printf( "FAIL table wire golden %s re-saves differently: %lld bytes out, %lld pinned\n",
                name, (long long) wrote, (long long) pinned );
        failures++;
    }
    free( region );
}

// Every file in testdata/wire/tables, read by the type that wrote it. T and ?T
// are ONE FRAMING, so the by-value and optional goldens read each other; *T is
// its own, so the two pointer goldens read through their own reader (§3.1).
static void test_golden_reload()
{
    reload_table_golden( "root_full", tabledemo::RootConfigLoad, tabledemo::RootConfigSave );
    reload_table_golden( "root_default", tabledemo::RootConfigLoad, tabledemo::RootConfigSave );
    reload_table_golden( "profile_elide", tabledemo::ProfileConfigLoad, tabledemo::ProfileConfigSave );
    reload_table_golden( "loadout_full", tabledemo::LoadoutConfigLoad, tabledemo::LoadoutConfigSave );
    reload_table_golden( "wide_blob", tabledemo::WideBlobLoad, tabledemo::WideBlobSave );
    reload_table_golden( "archive", tabledemo::ArchiveConfigLoad, tabledemo::ArchiveConfigSave );
    reload_table_golden( "keyed_config", tabledemo::KeyedConfigLoad, tabledemo::KeyedConfigSave );
    reload_table_golden( "keyed_default", tabledemo::KeyedConfigLoad, tabledemo::KeyedConfigSave );
    reload_table_golden( "v1_cfg", tblv1::CfgLoad, tblv1::CfgSave );
    reload_table_golden( "v1_seams", tblv1::CfgLoad, tblv1::CfgSave );
    reload_table_golden( "v2_cfg", tblv2::CfgLoad, tblv2::CfgSave );
    reload_table_golden( "v2_seams", tblv2::CfgLoad, tblv2::CfgSave );
    reload_table_golden( "chain_value", tblp1::ChainLoad, tblp1::ChainSave );
    reload_table_golden( "chain_value_empty", tblp1::ChainLoad, tblp1::ChainSave );
    reload_pointer_golden( "chain_pointer" );
    reload_table_golden( "chain_optional", tblp3::ChainLoad, tblp3::ChainSave );
    reload_table_golden( "chain_optional_empty", tblp3::ChainLoad, tblp3::ChainSave );
    reload_pointer_golden( "chain_pointer_empty" );
}

// The pinned instances, built here and mirrored VALUE FOR VALUE by every port
// (test/cs-tables/src/Program.cs is the C# twin). Keep the two in step: a
// divergence in the instance is a divergence in the gate.

static void build_golden_root( tabledemo::RootConfig & root )
{
    set_string( root.version_note, root.version_note_length, "golden-v1" );

    root.weapons_count = 2;
    root.weapons[0].damage = 40.5f;
    root.weapons[0].speed = 250.0f;
    root.weapons[0].penetration = 7;
    root.weapons[0].channel = 45;
    root.weapons[0].homing = true;
    root.weapons[0].effect.type = tabledemo::EffectType::Buff;
    root.weapons[0].effect.buff.multiplier = 3.25f;
    root.weapons[1].effect.type = tabledemo::EffectType::Debuff;
    root.weapons[1].effect.debuff.amount = 42;

    root.profiles_count = 1;
    tabledemo::ProfileConfig & p = root.profiles[0];
    set_string( p.name, p.name_length, "player one" );
    p.icon[0] = 1; p.icon[1] = 2; p.icon[2] = 250; p.icon_length = 3;
    p.experience = 777;
    p.tilt = -12;
    p.heading = -30000;
    p.timestamp = -5000000000ll;
    p.badge = 200;
    p.port = 40000;
    p.epoch = 0x1122334455667788ull;
    p.precision = 2.5;
    p.ratings[2] = 0.5f;
    p.has_loadout = true;
    p.loadout.grade = tabledemo::Grade::Gold;
    p.loadout.grades_count = 2;
    p.loadout.grades[0] = tabledemo::Grade::Bronze;
    p.loadout.grades[1] = tabledemo::Grade::Gold;
    p.loadout.podium[0] = tabledemo::Grade::Gold;
    p.loadout.podium[2] = tabledemo::Grade::Silver;
    p.loadout.perks = tabledemo::Perks_Cloaked | tabledemo::Perks_Turbo;
    p.loadout.primary.penetration = 7;
    p.loadout.backups[0].damage = 1.0f;
    p.loadout.attachments_count = 2;
    p.loadout.attachments[0].slot = 3;
    p.loadout.attachments[0].power = 2.0f;
    p.loadout.attachments[1].slot = 5;
}

static void build_golden_loadout( tabledemo::LoadoutConfig & loadout )
{
    loadout.grade = tabledemo::Grade::Bronze;
    loadout.grades_count = 3;
    loadout.grades[0] = tabledemo::Grade::Gold;
    loadout.grades[1] = tabledemo::Grade::Silver;
    loadout.grades[2] = tabledemo::Grade::Bronze;
    loadout.podium[1] = tabledemo::Grade::Bronze;
    loadout.perks = tabledemo::Perks_Shielded;
    loadout.primary.damage = 12.5f;
    loadout.primary.homing = true;
    loadout.primary.effect.type = tabledemo::EffectType::Buff;
    loadout.primary.effect.buff.multiplier = 0.5f;
    loadout.backups[1].channel = 63;
    loadout.attachments_count = 1;
    loadout.attachments[0].slot = 6;
    loadout.attachments[0].power = 0.25f;
}

static void build_golden_wide( tabledemo::WideBlob & blob )
{
    blob.label_length = 70000;
    for ( int32_t i = 0; i < 70000; i++ ) { blob.label[i] = (char) ( 'a' + ( i % 26 ) ); }
    blob.label[70000] = 0;
    blob.payload_length = 100;
    for ( int32_t i = 0; i < 100; i++ ) { blob.payload[i] = (uint8_t) ( i * 7 + 3 ); }
    blob.samples_count = 70000;
    for ( int32_t i = 0; i < 70000; i++ ) { blob.samples[i] = (uint16_t) ( i * 37 + 11 ); }
}

static void build_golden_v1( tblv1::Cfg & cfg )
{
    cfg.a = 9;
    cfg.b = 8.5f;
    cfg.mode = tblv1::Mode::Alpha;
    set_string( cfg.name, cfg.name_length, "aged" );
    cfg.inner.factor = 1.25f;
    cfg.items_count = 3;
    cfg.items[0] = 1; cfg.items[1] = 20; cfg.items[2] = 255;
    cfg.grade = tblv1::Grade::Gold;
    cfg.grades_count = 2;
    cfg.grades[0] = tblv1::Grade::Gold;
    cfg.grades[1] = tblv1::Grade::Bronze;
    cfg.podium[0] = tblv1::Grade::Bronze;
    cfg.podium[2] = tblv1::Grade::Gold;
    cfg.slots_count = 4;
    cfg.slots[0] = 11; cfg.slots[1] = 22; cfg.slots[2] = 33; cfg.slots[3] = 44;
    cfg.tally[0] = 5; cfg.tally[2] = 7;
    cfg.effect.type = tblv1::EffectType::Ward;
    cfg.effect.ward.charge = 0.75f;
}

static void build_golden_v2( tblv2::Cfg & cfg )
{
    cfg.a = 7.5f;
    cfg.c = false;
    cfg.mode = tblv2::Mode::Alpha;
    set_string( cfg.title, cfg.title_length, "fresh" );
    cfg.inner.factor = 9.5f;
    cfg.inner.gain = 4.0f;
    cfg.items_count = 2;
    cfg.items[0] = 10; cfg.items[1] = 200;
    cfg.grade = tblv2::Grade::Gold;
    cfg.grades_count = 3;
    cfg.grades[0] = tblv2::Grade::Silver;
    cfg.grades[1] = tblv2::Grade::Gold;
    cfg.grades[2] = tblv2::Grade::Bronze;
    cfg.podium[1] = tblv2::Grade::Silver;
    cfg.slots_count = 3;
    cfg.slots[0] = 7; cfg.slots[1] = 8; cfg.slots[2] = 9;
    cfg.tally[1] = 3; cfg.tally[3] = 9;
    cfg.effect.type = tblv2::EffectType::Hex;
    cfg.effect.hex.level = 6;
}

static void test_golden_wire()
{
    static uint8_t buffer[1u << 20];

    {
        static tabledemo::RootConfig root;
        build_golden_root( root );
        int64_t need = tabledemo::RootConfigMeasure( root );
        int64_t wrote = tabledemo::RootConfigSave( root, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == need );
        pin_table_golden( "root_full", buffer, wrote );
    }
    {
        static tabledemo::RootConfig root; // untouched: everything elides
        int64_t wrote = tabledemo::RootConfigSave( root, buffer, sizeof( buffer ) );
        CHECK( wrote == 2 );
        pin_table_golden( "root_default", buffer, wrote );
    }
    {
        // the ELISION shape: non-default fields around an all-default nested
        // table inside a taken guard. Its bytes are what pin the "all-default
        // nested elides" decision across languages — nothing else in this set
        // carries an all-default nesting.
        static tabledemo::ProfileConfig profile;
        profile.experience = 1;
        profile.has_loadout = true; // loadout itself stays all-default: elides
        int64_t wrote_elide = tabledemo::ProfileConfigSave( profile, buffer, sizeof( buffer ) );
        CHECK( wrote_elide > 0 && wrote_elide == tabledemo::ProfileConfigMeasure( profile ) );
        pin_table_golden( "profile_elide", buffer, wrote_elide );
    }
    {
        static tabledemo::LoadoutConfig loadout;
        build_golden_loadout( loadout );
        int64_t wrote = tabledemo::LoadoutConfigSave( loadout, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tabledemo::LoadoutConfigMeasure( loadout ) );
        pin_table_golden( "loadout_full", buffer, wrote );
    }
    {
        static tabledemo::WideBlob blob;
        build_golden_wide( blob );
        int64_t wrote = tabledemo::WideBlobSave( blob, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tabledemo::WideBlobMeasure( blob ) );
        pin_table_golden( "wide_blob", buffer, wrote );
    }
    {
        static tabledemo::ArchiveConfig archive;
        build_golden_root( archive.root );
        archive.count = 5;
        int64_t wrote = tabledemo::ArchiveConfigSave( archive, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tabledemo::ArchiveConfigMeasure( archive ) );
        pin_table_golden( "archive", buffer, wrote );
    }
    {
        static tblv1::Cfg cfg;
        build_golden_v1( cfg );
        int64_t wrote = tblv1::CfgSave( cfg, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tblv1::CfgMeasure( cfg ) );
        pin_table_golden( "v1_cfg", buffer, wrote );
    }
    {
        static tblv2::Cfg cfg;
        build_golden_v2( cfg );
        int64_t wrote = tblv2::CfgSave( cfg, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tblv2::CfgMeasure( cfg ) );
        pin_table_golden( "v2_cfg", buffer, wrote );
    }
}

// ---- the TEXT form (docs/SPEC-TABLES.md §16) ------------------------------------
//
// ONE walk over the reflection descriptors, so these tests are about the
// WALK, not about any table: what holds for the corpus root holds for every
// table in the closure by construction, and the round-trip loop below says
// so table by table anyway.

// the text form's contract in one call: write it, read it back, and prove the
// instance that came back saves to THE SAME WIRE BYTES as the one that went
// out. The wire is the arbiter — a text that round-trips through it has lost
// nothing the wire would have carried.
template <typename T>
static void check_json_round_trip( const T & value,
                                   int64_t ( *measure )( const T & ),
                                   int64_t ( *to_json )( const T &, char *, int64_t ),
                                   bool ( *from_json )( T &, const char *, int64_t, tabledemo::TableReport * ),
                                   int64_t ( *wire_measure )( const T & ),
                                   int64_t ( *wire_save )( const T &, uint8_t *, int64_t ),
                                   const char * what )
{
    int64_t size = measure( value );
    if ( size < 0 ) { printf( "FAIL %s: ToJsonMeasure refused\n", what ); failures++; return; }

    // measure == write at EXACT capacity, the wire's invariant carried across
    std::vector<char> text( (size_t) size );
    CHECK( to_json( value, text.data(), size ) == size );
    if ( size > 0 )
    {
        std::vector<char> tight( (size_t) size - 1 );
        CHECK( to_json( value, tight.data(), size - 1 ) == -1 ); // one byte short REFUSES
    }
    // and a roomy write produces the same bytes as the exact one
    std::vector<char> roomy( (size_t) size + 64 );
    CHECK( to_json( value, roomy.data(), size + 64 ) == size );
    CHECK( memcmp( roomy.data(), text.data(), (size_t) size ) == 0 );

    T back;
    tabledemo::TableReport report;
    if ( !from_json( back, text.data(), size, &report ) )
    {
        printf( "FAIL %s: FromJson refused its own text\n", what );
        failures++;
        return;
    }
    if ( report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 ||
         report.duplicate != 0 || report.malformed )
    {
        printf( "FAIL %s: a text this build wrote reported %d/%d/%d/%d/%d\n", what,
                report.unknown, report.kind_mismatch, report.clamped, report.duplicate,
                (int) report.malformed );
        failures++;
    }

    int64_t wire_a = wire_measure( value );
    int64_t wire_b = wire_measure( back );
    if ( wire_a < 0 || wire_a != wire_b ) { printf( "FAIL %s: wire sizes differ\n", what ); failures++; return; }
    std::vector<uint8_t> a( (size_t) wire_a ), b( (size_t) wire_b );
    CHECK( wire_save( value, a.data(), wire_a ) == wire_a );
    CHECK( wire_save( back, b.data(), wire_b ) == wire_b );
    if ( memcmp( a.data(), b.data(), (size_t) wire_a ) != 0 )
    {
        printf( "FAIL %s: round trip changed the wire\n", what );
        failures++;
    }
}

#define JSON_ROUND_TRIP( ns, type, value )                                    \
    check_json_round_trip<ns::type>( value, ns::type##ToJsonMeasure,           \
        ns::type##ToJson, ns::type##FromJson, ns::type##Measure, ns::type##Save, #type )

// a corpus root with every kind of the closure populated away from its
// default, so nothing round-trips green by riding a default
static tabledemo::RootConfig json_corpus_root()
{
    tabledemo::RootConfig root;
    set_string( root.version_note, root.version_note_length, "note \"q\"\n" );
    root.weapons_count = 2;
    root.weapons[0].damage = 33.5f;
    root.weapons[0].speed = 0.1f;          // a value no short decimal names exactly
    root.weapons[0].penetration = 7;
    root.weapons[0].channel = 63;          // bits(6) at its ceiling
    root.weapons[0].homing = true;
    root.weapons[0].effect.type = tabledemo::EffectType::Buff;
    root.weapons[0].effect.buff.multiplier = 2.25f;
    root.weapons[1].effect.type = tabledemo::EffectType::Debuff;
    root.weapons[1].effect.debuff.amount = 99;

    root.profiles_count = 1;
    tabledemo::ProfileConfig & p = root.profiles[0];
    set_string( p.name, p.name_length, "Ada \xc3\xa9 \xe2\x9c\x93" ); // multi-byte UTF-8
    const uint8_t icon[5] = { 0x00, 0x01, 0xfe, 0xff, 0x7f };
    memcpy( p.icon, icon, sizeof( icon ) );
    p.icon_length = (int32_t) sizeof( icon );
    p.experience = 4294967295u;            // u32 at its ceiling
    p.tilt = -128;                         // i8 at its floor
    p.heading = 32767;
    p.timestamp = -9223372036854775807LL - 1; // i64 at its floor
    p.badge = 255;
    p.port = 65535;
    p.epoch = 18446744073709551615ull;     // u64 at its ceiling: past int64
    p.precision = 0.1;                     // a double no short decimal names exactly
    p.ratings[0] = 1.5f;
    p.ratings[3] = -2.25f;
    p.has_loadout = true;
    p.loadout.grade = tabledemo::Grade::Gold;
    p.loadout.grades_count = 3;
    p.loadout.grades[0] = tabledemo::Grade::Bronze;
    p.loadout.grades[1] = tabledemo::Grade::None;
    p.loadout.grades[2] = tabledemo::Grade::Silver;
    p.loadout.podium[0] = tabledemo::Grade::Silver;
    p.loadout.podium[2] = tabledemo::Grade::Gold;
    p.loadout.perks = tabledemo::Perks_Shielded | tabledemo::Perks_Turbo;
    p.loadout.primary.damage = 12.0f;
    p.loadout.backups[1].homing = true;
    p.loadout.attachments_count = 2;
    p.loadout.attachments[0].slot = 7;
    p.loadout.attachments[0].power = 3.5f;
    p.loadout.attachments[1].slot = 0;
    return root;
}

// ---- every table in tables/examples round-trips ToJson -> FromJson -> Save,
// ---- byte-identical to the Save of the instance that went out

static void test_json_corpus_round_trip()
{
    tabledemo::RootConfig root = json_corpus_root();
    JSON_ROUND_TRIP( tabledemo, RootConfig, root );
    JSON_ROUND_TRIP( tabledemo, WeaponConfig, root.weapons[0] );
    JSON_ROUND_TRIP( tabledemo, WeaponConfig, root.weapons[1] );
    JSON_ROUND_TRIP( tabledemo, ProfileConfig, root.profiles[0] );
    JSON_ROUND_TRIP( tabledemo, LoadoutConfig, root.profiles[0].loadout );
    JSON_ROUND_TRIP( tabledemo, Attachment, root.profiles[0].loadout.attachments[0] );
    JSON_ROUND_TRIP( tabledemo, Buff, root.weapons[0].effect.buff );
    JSON_ROUND_TRIP( tabledemo, Debuff, root.weapons[1].effect.debuff );

    // an ALL-DEFAULT instance: the text carries every field anyway (a text is
    // for people, and one that elides is one a reader must know the schema to
    // complete), and the wire it comes back as is the empty one
    tabledemo::RootConfig empty;
    JSON_ROUND_TRIP( tabledemo, RootConfig, empty );
    tabledemo::WeaponConfig plain;
    JSON_ROUND_TRIP( tabledemo, WeaponConfig, plain );

    // cross-file: ArchiveConfig nests RootConfig from another file
    tabledemo::ArchiveConfig archive;
    archive.root = root;
    archive.count = 42;
    JSON_ROUND_TRIP( tabledemo, ArchiveConfig, archive );

    // extents past the old 16-bit ceiling ride the text form too
    tabledemo::WideBlob wide;
    for ( int32_t i = 0; i < 70000; i++ ) { wide.label[i] = char( 'a' + ( i % 26 ) ); }
    wide.label[70000] = 0;
    wide.label_length = 70000;
    for ( int32_t i = 0; i < 66000; i++ ) { wide.payload[i] = (uint8_t) ( i * 7 ); }
    wide.payload_length = 66000;
    wide.samples_count = 70000;
    for ( int32_t i = 0; i < 70000; i++ ) { wide.samples[i] = (uint16_t) ( i * 3 ); }
    JSON_ROUND_TRIP( tabledemo, WideBlob, wide );

    // the evolution pair's tables are ordinary tables to the walk
    tblv1::Cfg cfg;
    cfg.a = 900;
    cfg.b = 2.5f;
    cfg.mode = tblv1::Mode::Alpha;
    set_string( cfg.name, cfg.name_length, "v1" );
    cfg.inner.factor = 9.0f;
    cfg.items_count = 2; cfg.items[0] = 3; cfg.items[1] = 250;
    cfg.grade = tblv1::Grade::Gold;
    cfg.effect.type = tblv1::EffectType::Ward;
    cfg.effect.ward.charge = 0.25f;
    {
        int64_t size = tblv1::CfgToJsonMeasure( cfg );
        CHECK( size > 0 );
        std::vector<char> text( (size_t) size );
        CHECK( tblv1::CfgToJson( cfg, text.data(), size ) == size );
        tblv1::Cfg back;
        tblv1::TableReport report;
        CHECK( tblv1::CfgFromJson( back, text.data(), size, &report ) );
        uint8_t a[4096], b[4096];
        int64_t na = tblv1::CfgSave( cfg, a, sizeof( a ) );
        int64_t nb = tblv1::CfgSave( back, b, sizeof( b ) );
        CHECK( na > 0 && na == nb && memcmp( a, b, (size_t) na ) == 0 );
    }
}

// ---- the dialect: what the walk accepts, and what it will not guess at ----

static void test_json_dialect()
{
    tabledemo::Attachment value;
    tabledemo::TableReport report;

    // trailing commas: the authoring files this section exists for carry them
    const char * trailing = "{ \"slot\": 3, \"power\": 1.5, }";
    CHECK( tabledemo::AttachmentFromJson( value, trailing, (int64_t) strlen( trailing ), &report ) );
    CHECK( value.slot == 3 && value.power == 1.5f );
    CHECK( report.unknown == 0 && !report.malformed );

    // ... in arrays too
    tabledemo::LoadoutConfig loadout;
    const char * array_trailing = "{ \"grades\": [ \"Gold\", \"Bronze\", ], }";
    CHECK( tabledemo::LoadoutConfigFromJson( loadout, array_trailing, (int64_t) strlen( array_trailing ), &report ) );
    CHECK( loadout.grades_count == 2 && loadout.grades[0] == tabledemo::Grade::Gold );

    // comments are not JSON, and a walk that guessed at one would be reading
    // a dialect nobody wrote down
    tabledemo::TableReport comment_report;
    const char * comment = "{ // a note\n \"slot\": 3 }";
    CHECK( !tabledemo::AttachmentFromJson( value, comment, (int64_t) strlen( comment ), &comment_report ) );
    CHECK( comment_report.malformed );

    // whitespace anywhere, and a text that is not an object at all
    const char * spaced = "  \t\n { \n \"slot\" \t : \n 2 \n } \n ";
    CHECK( tabledemo::AttachmentFromJson( value, spaced, (int64_t) strlen( spaced ), &report ) );
    CHECK( value.slot == 2 );
    tabledemo::TableReport not_object;
    const char * scalar = "42";
    CHECK( !tabledemo::AttachmentFromJson( value, scalar, 2, &not_object ) );
    CHECK( not_object.malformed );

    // trailing rubbish after the one text is not one text
    tabledemo::TableReport trailing_junk;
    const char * two = "{ \"slot\": 1 } { \"slot\": 2 }";
    CHECK( !tabledemo::AttachmentFromJson( value, two, (int64_t) strlen( two ), &trailing_junk ) );
    CHECK( trailing_junk.malformed );

    // an absent key keeps the field's DECLARED default, exactly as an absent
    // field does on the wire (docs/SPEC-TABLES.md §4)
    tabledemo::WeaponConfig weapon;
    const char * partial = "{ \"homing\": true }";
    CHECK( tabledemo::WeaponConfigFromJson( weapon, partial, (int64_t) strlen( partial ), &report ) );
    CHECK( weapon.homing && weapon.damage == 21.0f && weapon.speed == 500.0f );

    // unknown keys are skipped and counted, whatever they carry
    tabledemo::TableReport unknown;
    const char * strange = "{ \"nope\": { \"deep\": [ 1, 2, { \"x\": null } ] }, \"slot\": 4, \"also\": \"text\" }";
    CHECK( tabledemo::AttachmentFromJson( value, strange, (int64_t) strlen( strange ), &unknown ) );
    CHECK( value.slot == 4 );
    CHECK( unknown.unknown == 2 && !unknown.malformed );

    // a duplicate key is LAST WINS, and the repeat is counted
    tabledemo::TableReport duplicate;
    const char * repeated = "{ \"slot\": 1, \"slot\": 6, \"slot\": 2 }";
    CHECK( tabledemo::AttachmentFromJson( value, repeated, (int64_t) strlen( repeated ), &duplicate ) );
    CHECK( value.slot == 2 && duplicate.duplicate == 2 );

    // the wire imposes no encoding on a string (docs/SPEC-TABLES.md §3), so the
    // text must not either: a stray lead byte rides as its own byte and does
    // NOT swallow the character after it — including the closing quote
    {
        tabledemo::ProfileConfig stray;
        char text[64];
        int n = snprintf( text, sizeof( text ), "{ \"name\": \"ab\xc3\" }" );
        CHECK( n > 0 );
        tabledemo::TableReport raw;
        CHECK( tabledemo::ProfileConfigFromJson( stray, text, n, &raw ) );
        CHECK( stray.name_length == 3 && (unsigned char) stray.name[2] == 0xc3 );
        CHECK( !raw.malformed );

        // ... but the WRITER owes a valid JSON text (RFC 8259 §8.1), so the
        // byte it cannot spell is written as U+FFFD, one per bad byte. The
        // round trip is therefore NOT byte-identical for invalid UTF-8, and
        // that is the trade: a text a conforming parser can read, rather than
        // one only this walk can (docs/SPEC-TABLES.md §16.2).
        int64_t size = tabledemo::ProfileConfigToJsonMeasure( stray );
        std::vector<char> out( (size_t) size + 1 );
        CHECK( tabledemo::ProfileConfigToJson( stray, out.data(), size ) == size );
        out[(size_t) size] = 0;
        CHECK( strstr( out.data(), "\"ab\xef\xbf\xbd\"" ) != NULL );
        tabledemo::ProfileConfig back;
        CHECK( tabledemo::ProfileConfigFromJson( back, out.data(), size, &raw ) );
        CHECK( back.name_length == 5 );
        CHECK( (unsigned char) back.name[2] == 0xef && (unsigned char) back.name[3] == 0xbf &&
               (unsigned char) back.name[4] == 0xbd );
        // and the SECOND trip is stable: a text the walk wrote reads back and
        // writes the same bytes again
        int64_t again = tabledemo::ProfileConfigToJsonMeasure( back );
        std::vector<char> twice( (size_t) again );
        CHECK( tabledemo::ProfileConfigToJson( back, twice.data(), again ) == again );
        CHECK( again == size && memcmp( twice.data(), out.data(), (size_t) size ) == 0 );
    }

    // a lone surrogate ESCAPE is valid JSON input with no UTF-8 encoding:
    // it reads as U+FFFD rather than manufacturing CESU-8 out of it
    {
        tabledemo::ProfileConfig lone;
        tabledemo::TableReport lone_report;
        const char * text = "{ \"name\": \"\\udc00x\\ud800\" }";
        CHECK( tabledemo::ProfileConfigFromJson( lone, text, (int64_t) strlen( text ), &lone_report ) );
        CHECK( lone.name_length == 7 );
        CHECK( strcmp( lone.name, "\xef\xbf\xbd" "x" "\xef\xbf\xbd" ) == 0 );
        // ... and a well-formed pair still decodes to the character it names
        tabledemo::ProfileConfig pair;
        const char * emoji = "{ \"name\": \"\\ud83d\\ude00\" }";
        CHECK( tabledemo::ProfileConfigFromJson( pair, emoji, (int64_t) strlen( emoji ), &lone_report ) );
        CHECK( pair.name_length == 4 && strcmp( pair.name, "\xf0\x9f\x98\x80" ) == 0 );
    }

    // string escapes both ways, including a surrogate pair
    tabledemo::ProfileConfig profile;
    const char * escapes = "{ \"name\": \"a\\\"b\\\\c\\nd\\u00e9e\\ud83d\\ude00\" }";
    CHECK( tabledemo::ProfileConfigFromJson( profile, escapes, (int64_t) strlen( escapes ), &report ) );
    CHECK( strcmp( profile.name, "a\"b\\c\nd\xc3\xa9""e\xf0\x9f\x98\x80" ) == 0 );
}

// ---- hostile: the wrong JSON type at every kind, and every width overflowed

static void test_json_hostile_kinds()
{
    // a key present with the WRONG JSON TYPE is skipped and counted, never
    // coerced: the field keeps its default and the read carries on
    struct { const char * text; const char * what; } wrong[] = {
        { "{ \"experience\": \"12\" }",      "string where a number is declared" },
        { "{ \"experience\": true }",        "bool where a number is declared" },
        { "{ \"experience\": null }",        "null where a number is declared" },
        { "{ \"experience\": [ 1 ] }",       "array where a number is declared" },
        { "{ \"experience\": { \"a\": 1 } }", "object where a number is declared" },
        { "{ \"name\": 5 }",                 "number where a string is declared" },
        { "{ \"name\": [ \"a\" ] }",         "array where a string is declared" },
        { "{ \"icon\": 7 }",                 "number where bytes are declared" },
        { "{ \"has_loadout\": 1 }",          "number where a bool is declared" },
        { "{ \"has_loadout\": \"true\" }",   "string where a bool is declared" },
        { "{ \"ratings\": 3.5 }",            "number where an array is declared" },
        { "{ \"loadout\": [ ] }",            "array where a table is declared" },
        { "{ \"precision\": \"1e3\" }",      "string where a float is declared" },
    };
    for ( const auto & row : wrong )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        bool ok = tabledemo::ProfileConfigFromJson( value, row.text, (int64_t) strlen( row.text ), &report );
        if ( !ok || report.kind_mismatch != 1 || report.malformed )
        {
            printf( "FAIL json kind mismatch (%s): ok=%d mismatch=%d malformed=%d\n",
                    row.what, ok, report.kind_mismatch, (int) report.malformed );
            failures++;
        }
        CHECK( value.experience == 0 && value.name_length == 0 && !value.has_loadout );
    }

    // a vocabulary field wants a NAME, not a number
    {
        tabledemo::LoadoutConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"grade\": 3, \"perks\": 5 }";
        CHECK( tabledemo::LoadoutConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.kind_mismatch == 2 );
        CHECK( value.grade == tabledemo::Grade::Silver ); // the declared default
        CHECK( value.perks == 0 );
    }

    // a name no variant names reads as None and counts as UNKNOWN — the same
    // event an unknown variant id is on the wire (docs/SPEC-TABLES.md §4)
    {
        tabledemo::LoadoutConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"grade\": \"Platinum\", \"perks\": [ \"Turbo\", \"Warp\" ] }";
        CHECK( tabledemo::LoadoutConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.grade == tabledemo::Grade::None );
        CHECK( value.perks == tabledemo::Perks_Turbo );
        CHECK( report.unknown == 2 );
    }

    // a fraction where an INTEGER is declared is the wrong shape for the kind,
    // not framing damage: counted and skipped, never rounded into place
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"experience\": 12.5, \"badge\": 3 }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.experience == 0 && value.badge == 3 );
        CHECK( report.kind_mismatch == 1 && !report.malformed );
    }
}

static void test_json_hostile_overflow()
{
    // every integer width, over its ceiling and under its floor: CLAMPED and
    // counted, never wrapped (docs/SPEC-TABLES.md §4)
    struct { const char * text; const char * field; int64_t expect; } rows[] = {
        { "{ \"tilt\": 999 }",                          "tilt+",  127 },
        { "{ \"tilt\": -999 }",                         "tilt-",  -128 },
        { "{ \"badge\": 300 }",                         "badge",  255 },
        { "{ \"badge\": -1 }",                          "badge-", 0 },
        { "{ \"heading\": 40000 }",                     "head+",  32767 },
        { "{ \"heading\": -40000 }",                    "head-",  -32768 },
        { "{ \"port\": 70000 }",                        "port",   65535 },
        { "{ \"experience\": 5000000000 }",             "exp",    4294967295ll },
        { "{ \"timestamp\": 99999999999999999999 }",    "time+",  9223372036854775807ll },
        { "{ \"timestamp\": -99999999999999999999 }",   "time-",  -9223372036854775807ll - 1 },
    };
    for ( const auto & row : rows )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        CHECK( tabledemo::ProfileConfigFromJson( value, row.text, (int64_t) strlen( row.text ), &report ) );
        int64_t got = 0;
        if ( strncmp( row.field, "tilt", 4 ) == 0 ) { got = value.tilt; }
        else if ( strncmp( row.field, "badge", 5 ) == 0 ) { got = value.badge; }
        else if ( strncmp( row.field, "head", 4 ) == 0 ) { got = value.heading; }
        else if ( strncmp( row.field, "port", 4 ) == 0 ) { got = value.port; }
        else if ( strncmp( row.field, "exp", 3 ) == 0 ) { got = (int64_t) value.experience; }
        else { got = value.timestamp; }
        if ( got != row.expect || report.clamped == 0 || report.malformed )
        {
            printf( "FAIL json overflow (%s): got %lld want %lld clamped=%d\n",
                    row.field, (long long) got, (long long) row.expect, report.clamped );
            failures++;
        }
    }

    // u64 past int64 is NOT an overflow — it is the top of the field
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"epoch\": 18446744073709551615 }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.epoch == 18446744073709551615ull && report.clamped == 0 );
    }
    // ... and past THAT it saturates at the ceiling
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"epoch\": 99999999999999999999999 }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.epoch == 18446744073709551615ull && report.clamped == 1 );
    }

    // a DECLARED range clamps before the storage width does
    {
        tabledemo::WeaponConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"penetration\": 500, \"channel\": 4000 }";
        CHECK( tabledemo::WeaponConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.penetration == 10 );  // | min = 0, max = 10
        CHECK( value.channel == 63 );      // bits(6): the width IS the range
        CHECK( report.clamped == 2 );
    }

    // a float range clamps the same way
    {
        tabledemo::Attachment value;
        tabledemo::TableReport report;
        const char * text = "{ \"slot\": -5 }";
        CHECK( tabledemo::AttachmentFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.slot == 0 && report.clamped == 1 );
    }
}

static void test_json_hostile_extents()
{
    // a string longer than the field CLAMPS AT A CODE POINT BOUNDARY: the
    // storage never holds half a character
    {
        tabledemo::RootConfig value;   // version_note is string(16)
        tabledemo::TableReport report;
        // 15 ASCII bytes, then a 2-byte character that cannot fit in the 16th
        const char * text = "{ \"version_note\": \"123456789012345\xc3\xa9\" }";
        CHECK( tabledemo::RootConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.version_note_length == 15 );
        CHECK( strcmp( value.version_note, "123456789012345" ) == 0 );
        CHECK( report.clamped == 1 );
    }
    {
        // and a 3-byte character that WOULD have fit at 14 does
        tabledemo::RootConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"version_note\": \"1234567890123\xe2\x9c\x93X\" }";
        CHECK( tabledemo::RootConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.version_note_length == 16 );
        CHECK( report.clamped == 1 ); // the trailing X did not fit
    }

    // more array elements than the reader's bound: the bounded prefix is kept
    // and the excess counts, the wire's rule
    {
        tabledemo::LoadoutConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"grades\": [ \"Gold\", \"Gold\", \"Gold\", \"Gold\", \"Gold\", \"Gold\" ] }";
        CHECK( tabledemo::LoadoutConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.grades_count == 4 && report.clamped == 2 );
    }
    // fewer than a FIXED array's extent: the tail keeps its defaults
    {
        tabledemo::LoadoutConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"podium\": [ \"Gold\" ] }";
        CHECK( tabledemo::LoadoutConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.podium[0] == tabledemo::Grade::Gold );
        CHECK( value.podium[1] == tabledemo::Grade::None && value.podium[2] == tabledemo::Grade::None );
        CHECK( report.clamped == 0 );
    }

    // base64 both ways, and a body that is not base64 at all
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"icon\": \"AQID/w==\" }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.icon_length == 4 );
        CHECK( value.icon[0] == 0x01 && value.icon[1] == 0x02 && value.icon[2] == 0x03 && value.icon[3] == 0xff );
        CHECK( report.kind_mismatch == 0 );
    }
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"icon\": \"not base64 at all!\" }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.icon_length == 0 && report.kind_mismatch == 1 && !report.malformed );
    }
    {
        // a base64 body longer than bytes(16) clamps and counts
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"icon\": \"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=\" }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.icon_length == 16 && report.clamped == 1 );
    }
}

static void test_json_hostile_unions()
{
    tabledemo::WeaponConfig value;
    tabledemo::TableReport report;

    // {} and an absent key are both None
    const char * empty = "{ \"effect\": {} }";
    CHECK( tabledemo::WeaponConfigFromJson( value, empty, (int64_t) strlen( empty ), &report ) );
    CHECK( value.effect.type == tabledemo::EffectType::None );

    // ONE key, the arm's name
    const char * one = "{ \"effect\": { \"debuff\": { \"amount\": 12 } } }";
    CHECK( tabledemo::WeaponConfigFromJson( value, one, (int64_t) strlen( one ), &report ) );
    CHECK( value.effect.type == tabledemo::EffectType::Debuff && value.effect.debuff.amount == 12 );

    // an arm this build cannot name reads as None and counts
    tabledemo::TableReport unknown_arm;
    const char * strange = "{ \"effect\": { \"hex\": { \"power\": 3 } } }";
    CHECK( tabledemo::WeaponConfigFromJson( value, strange, (int64_t) strlen( strange ), &unknown_arm ) );
    CHECK( value.effect.type == tabledemo::EffectType::None && unknown_arm.unknown == 1 );

    // TWO keys is not a one-of, and the walk will not choose for you
    tabledemo::TableReport two;
    const char * both = "{ \"effect\": { \"buff\": { \"multiplier\": 2.0 }, \"debuff\": { \"amount\": 1 } } }";
    CHECK( !tabledemo::WeaponConfigFromJson( value, both, (int64_t) strlen( both ), &two ) );
    CHECK( two.malformed );

    // a selected arm is RE-ESTABLISHED, never overlaid on the last one: a
    // repeated key that names a different arm leaves nothing of the first
    tabledemo::TableReport repeat;
    const char * again = "{ \"effect\": { \"buff\": { \"multiplier\": 9.0 } }, "
                         "\"effect\": { \"debuff\": { \"amount\": 4 } } }";
    CHECK( tabledemo::WeaponConfigFromJson( value, again, (int64_t) strlen( again ), &repeat ) );
    CHECK( value.effect.type == tabledemo::EffectType::Debuff && value.effect.debuff.amount == 4 );
    CHECK( repeat.duplicate == 1 );
}

static void test_json_hostile_framing()
{
    tabledemo::Attachment value;

    struct { const char * text; const char * what; } broken[] = {
        { "",                          "empty" },
        { "{",                         "unterminated object" },
        { "{ \"slot\"",                "key with no colon" },
        { "{ \"slot\": }",             "colon with no value" },
        { "{ \"slot\" 3 }",            "no colon" },
        { "{ slot: 3 }",               "unquoted key" },
        { "{ \"slot\": 3",             "no close" },
        { "{ \"slot\": [ 1, 2 }",      "array closed as an object" },
        { "{ \"slot\": \"unterminated }", "unterminated string" },
        { "{ \"slot\": tru }",         "truncated literal" },
        { "{ \"slot\": - }",           "a sign with no digits" },
        { "{ \"slot\": \"\\q\" }",     "an escape that is not one" },
        { "{ \"slot\": \"\\u00\" }",   "a short unicode escape" },
    };
    for ( const auto & row : broken )
    {
        tabledemo::TableReport report;
        bool ok = tabledemo::AttachmentFromJson( value, row.text, (int64_t) strlen( row.text ), &report );
        if ( ok || !report.malformed )
        {
            printf( "FAIL json framing (%s): ok=%d malformed=%d\n", row.what, ok, (int) report.malformed );
            failures++;
        }
    }

    // nesting past the cap is refused rather than run off the C stack
    {
        std::vector<char> deep;
        const char * head = "{ \"nope\": ";
        deep.insert( deep.end(), head, head + strlen( head ) );
        for ( int i = 0; i < 400; i++ ) { deep.push_back( '[' ); }
        for ( int i = 0; i < 400; i++ ) { deep.push_back( ']' ); }
        deep.push_back( '}' );
        tabledemo::TableReport report;
        CHECK( !tabledemo::AttachmentFromJson( value, deep.data(), (int64_t) deep.size(), &report ) );
        CHECK( report.malformed );
    }
    // a nesting the walk CAN take is still taken
    {
        std::vector<char> deep;
        const char * head = "{ \"nope\": ";
        deep.insert( deep.end(), head, head + strlen( head ) );
        for ( int i = 0; i < 100; i++ ) { deep.push_back( '[' ); }
        for ( int i = 0; i < 100; i++ ) { deep.push_back( ']' ); }
        const char * tail = ", \"slot\": 5 }";
        deep.insert( deep.end(), tail, tail + strlen( tail ) );
        tabledemo::TableReport report;
        CHECK( tabledemo::AttachmentFromJson( value, deep.data(), (int64_t) deep.size(), &report ) );
        CHECK( value.slot == 5 && report.unknown == 1 && !report.malformed );
    }

    // a NULL text, and a negative size
    {
        tabledemo::TableReport report;
        CHECK( !tabledemo::AttachmentFromJson( value, NULL, 0, &report ) );
        CHECK( report.malformed );
    }
    // ... and no report at all is legal: the walk owns one when the caller
    // does not
    CHECK( !tabledemo::AttachmentFromJson( value, "{", 1, NULL ) );
}

// ---- the writer refuses what the text cannot name, exactly as measure and
// ---- save refuse what the wire cannot name (docs/SPEC-TABLES.md §5)

static void test_json_writer_refusals()
{
    {
        tabledemo::LoadoutConfig value;
        value.grade = (tabledemo::Grade) 9; // no variant names it
        CHECK( tabledemo::LoadoutConfigToJsonMeasure( value ) == -1 );
    }
    {
        tabledemo::LoadoutConfig value;
        value.perks = uint64_t( 1 ) << 40; // no variant names that bit
        CHECK( tabledemo::LoadoutConfigToJsonMeasure( value ) == -1 );
    }
    {
        tabledemo::WeaponConfig value;
        value.effect.type = (tabledemo::EffectType) 7; // no arm names it
        CHECK( tabledemo::WeaponConfigToJsonMeasure( value ) == -1 );
    }
    {
        // a non-finite float has no JSON spelling at all, and the writer will
        // not lose one silently
        tabledemo::WeaponConfig value;
        value.damage = 1.0f / 0.0f - 1.0f / 0.0f; // NaN, computed so no literal is needed
        CHECK( tabledemo::WeaponConfigToJsonMeasure( value ) == -1 );
        value.damage = 3.4e38f * 10.0f;           // +inf
        CHECK( tabledemo::WeaponConfigToJsonMeasure( value ) == -1 );
    }
}

// ---- guards: the wire's elision, carried into the text (docs/SPEC-TABLES.md §16.2)

static void test_json_guards()
{
    tabledemo::ProfileConfig off;
    off.has_loadout = false;
    off.loadout.grade = tabledemo::Grade::Gold; // stale, and off the wire
    int64_t size = tabledemo::ProfileConfigToJsonMeasure( off );
    CHECK( size > 0 );
    std::vector<char> text( (size_t) size + 1 );
    CHECK( tabledemo::ProfileConfigToJson( off, text.data(), size ) == size );
    text[(size_t) size] = 0;
    CHECK( strstr( text.data(), "\"loadout\"" ) == NULL ); // guarded off, as on the wire
    CHECK( strstr( text.data(), "\"has_loadout\"" ) != NULL ); // the guard is a plain key

    tabledemo::ProfileConfig on;
    on.has_loadout = true;
    on.loadout.grade = tabledemo::Grade::Gold;
    size = tabledemo::ProfileConfigToJsonMeasure( on );
    std::vector<char> text_on( (size_t) size + 1 );
    CHECK( tabledemo::ProfileConfigToJson( on, text_on.data(), size ) == size );
    text_on[(size_t) size] = 0;
    CHECK( strstr( text_on.data(), "\"loadout\"" ) != NULL );

    // reading INFERS NOTHING: the guard is an ordinary bool key, and a guarded
    // field's key is placed whether or not the guard came first — key order in
    // an object is nobody's contract
    tabledemo::ProfileConfig value;
    tabledemo::TableReport report;
    const char * after = "{ \"loadout\": { \"grade\": \"Gold\" }, \"has_loadout\": true }";
    CHECK( tabledemo::ProfileConfigFromJson( value, after, (int64_t) strlen( after ), &report ) );
    CHECK( value.has_loadout && value.loadout.grade == tabledemo::Grade::Gold );

    // and a guarded field placed with the guard FALSE is elided on the way
    // out, so the wire never sees it either
    tabledemo::ProfileConfig ignored;
    const char * no_guard = "{ \"loadout\": { \"grade\": \"Gold\" } }";
    CHECK( tabledemo::ProfileConfigFromJson( ignored, no_guard, (int64_t) strlen( no_guard ), &report ) );
    CHECK( !ignored.has_loadout );
    uint8_t wire[512];
    int64_t wrote = tabledemo::ProfileConfigSave( ignored, wire, sizeof( wire ) );
    tabledemo::ProfileConfig fresh;
    CHECK( wrote == tabledemo::ProfileConfigMeasure( fresh ) ); // the empty body
}

// ---- the json = "key" attribute: the text's vocabulary, not the wire's ----

static void test_json_key_attribute()
{
    const tabledemo::TableFieldInfo * f = demo_field( tabledemo::WeaponConfigTableType(), "damage" );
    CHECK( f != NULL && strcmp( f->json, "damage" ) == 0 );

    // jsonkeys::Ship renames two fields in the TEXT and moves no wire byte:
    // the id is still the hash of the schema name (docs/SPEC-TABLES.md §16.3)
    const jsonkeys::TableFieldInfo * ship_type = jsonkeys_field( jsonkeys::ShipTableType(), "ship_type" );
    CHECK( ship_type != NULL );
    CHECK( strcmp( ship_type->json, "type" ) == 0 );
    CHECK( ship_type->id == field_id( "ship_type" ) );

    jsonkeys::Ship ship;
    jsonkeys::TableReport report;
    const char * text = "{ \"type\": 3, \"label\": \"Hauler\", \"hp\": 250 }";
    CHECK( jsonkeys::ShipFromJson( ship, text, (int64_t) strlen( text ), &report ) );
    CHECK( ship.ship_type == 3 && ship.hit_points == 250 );
    CHECK( strcmp( ship.name, "Hauler" ) == 0 );
    CHECK( report.unknown == 0 && !report.malformed );

    // the SCHEMA name is not a key: it reads as unknown
    jsonkeys::Ship other;
    jsonkeys::TableReport by_name;
    const char * schema_names = "{ \"ship_type\": 3, \"name\": \"Hauler\" }";
    CHECK( jsonkeys::ShipFromJson( other, schema_names, (int64_t) strlen( schema_names ), &by_name ) );
    CHECK( other.ship_type == 0 && by_name.unknown == 2 );

    // and the text the writer produces uses the keys, round-tripping clean
    int64_t size = jsonkeys::ShipToJsonMeasure( ship );
    std::vector<char> out( (size_t) size + 1 );
    CHECK( jsonkeys::ShipToJson( ship, out.data(), size ) == size );
    out[(size_t) size] = 0;
    CHECK( strstr( out.data(), "\"type\"" ) != NULL );
    CHECK( strstr( out.data(), "\"ship_type\"" ) == NULL );
    jsonkeys::Ship back;
    CHECK( jsonkeys::ShipFromJson( back, out.data(), size, &report ) );
    uint8_t a[512], b[512];
    int64_t na = jsonkeys::ShipSave( ship, a, sizeof( a ) );
    int64_t nb = jsonkeys::ShipSave( back, b, sizeof( b ) );
    CHECK( na > 0 && na == nb && memcmp( a, b, (size_t) na ) == 0 );
}

// ---- the tokenizer under a mutation fuzz: no input, however malformed,
// ---- may crash, hang, or read past the text it was handed

static void test_json_fuzz_tokenizer()
{
    // one seed corpus, mutated deterministically: the point is not coverage
    // of the schema, it is that the TOKENIZER survives arbitrary bytes
    static const char * seeds[] = {
        "{ \"slot\": 3, \"power\": 1.5 }",
        "{ \"name\": \"a\\u00e9b\", \"icon\": \"AQID\", \"ratings\": [ 1, 2, 3, 4 ] }",
        "{ \"effect\": { \"buff\": { \"multiplier\": 2.0 } } }",
        "{ \"grades\": [ \"Gold\", \"Bronze\" ], \"perks\": [ \"Turbo\" ] }",
        "{ \"loadout\": { \"primary\": { \"damage\": 1e30 } }, \"has_loadout\": true }",
    };
    uint64_t state = 0x9e3779b97f4a7c15ull;
    auto next = [&state]() -> uint32_t
    {
        state ^= state << 13; state ^= state >> 7; state ^= state << 17;
        return (uint32_t) ( state >> 32 );
    };
    std::vector<char> work;
    for ( int iteration = 0; iteration < 20000; iteration++ )
    {
        const char * seed = seeds[ next() % ( sizeof( seeds ) / sizeof( seeds[0] ) ) ];
        work.assign( seed, seed + strlen( seed ) );
        int mutations = 1 + (int) ( next() % 6 );
        for ( int m = 0; m < mutations && !work.empty(); m++ )
        {
            uint32_t at = next() % (uint32_t) work.size();
            switch ( next() % 4 )
            {
                case 0: work[at] = (char) ( next() & 0xff ); break;      // flip a byte
                case 1: work.erase( work.begin() + at ); break;          // cut one
                case 2: work.insert( work.begin() + at, (char) ( next() & 0xff ) ); break;
                case 3: work.resize( (size_t) at ); break;               // truncate
            }
        }
        // every table the walk can enter, over the same mutated bytes: a
        // tokenizer defect that only one descriptor shape reaches is still a
        // defect
        tabledemo::TableReport report;
        tabledemo::ProfileConfig profile;
        tabledemo::ProfileConfigFromJson( profile, work.data(), (int64_t) work.size(), &report );
        tabledemo::WeaponConfig weapon;
        tabledemo::WeaponConfigFromJson( weapon, work.data(), (int64_t) work.size(), &report );
        tabledemo::LoadoutConfig loadout;
        tabledemo::LoadoutConfigFromJson( loadout, work.data(), (int64_t) work.size(), &report );
        tabledemo::RootConfig root;
        tabledemo::RootConfigFromJson( root, work.data(), (int64_t) work.size(), &report );
        // whatever came back is a legal instance: it must still save
        CHECK( tabledemo::RootConfigMeasure( root ) > 0 );

        // AND it must still be WRITABLE as a text. §16.1 sells one invariant —
        // a text that reads writes back — and the way to lose it is to let a
        // conversion put something in storage that has no JSON spelling. No
        // mutated text, however hostile, may reach that state.
        tabledemo::TableReport clean;
        tabledemo::ProfileConfig checked;
        if ( tabledemo::ProfileConfigFromJson( checked, work.data(), (int64_t) work.size(), &clean ) )
        {
            if ( tabledemo::ProfileConfigToJsonMeasure( checked ) <= 0 )
            {
                printf( "FAIL json fuzz: a text that READ produced an instance that cannot be WRITTEN\n" );
                failures++;
                break;
            }
        }
    }
}

// ---- the PINNED TEXT: what ToJson actually spells ------------------------
//
// Every other test in this section round-trips through the writer, so reader
// and writer share `enum_name` and a vocabulary error cancels itself out: a
// walker emitting "???" for a bit it cannot name round-trips green. This test
// is the one that cannot: a known instance against a known LITERAL text, so
// every spelling the form promises — an enum variant, a flags bit, a union
// arm name, None, base64, a bool, an integer, a float, the guard's elision
// and the pretty-printed shape — is pinned to the page rather than to the
// code's own opinion of itself.

static void test_json_pinned_text()
{
    tabledemo::LoadoutConfig loadout;
    loadout.grade = tabledemo::Grade::Gold;
    loadout.grades_count = 2;
    loadout.grades[0] = tabledemo::Grade::Bronze;
    loadout.grades[1] = tabledemo::Grade::None;
    loadout.podium[0] = tabledemo::Grade::Silver;
    loadout.perks = tabledemo::Perks_Shielded | tabledemo::Perks_Turbo;
    loadout.primary.damage = 2.5f;
    loadout.primary.effect.type = tabledemo::EffectType::Buff;
    loadout.primary.effect.buff.multiplier = 4.0f;
    loadout.backups[0].effect.type = tabledemo::EffectType::Debuff;
    loadout.backups[0].effect.debuff.amount = 7;
    loadout.attachments_count = 1;
    loadout.attachments[0].slot = 3;
    loadout.attachments[0].power = 1.5f;

    static const char * expected =
        "{\n"
        "  \"grade\": \"Gold\",\n"
        "  \"grades\": [\n"
        "    \"Bronze\",\n"
        "    \"None\"\n"
        "  ],\n"
        "  \"podium\": [\n"
        "    \"Silver\",\n"
        "    \"None\",\n"
        "    \"None\"\n"
        "  ],\n"
        "  \"perks\": [\n"
        "    \"Shielded\",\n"
        "    \"Turbo\"\n"
        "  ],\n"
        "  \"primary\": {\n"
        "    \"damage\": 2.5,\n"
        "    \"speed\": 500,\n"
        "    \"penetration\": 1,\n"
        "    \"channel\": 0,\n"
        "    \"homing\": false,\n"
        "    \"effect\": {\n"
        "      \"buff\": {\n"
        "        \"multiplier\": 4\n"
        "      }\n"
        "    }\n"
        "  },\n"
        "  \"backups\": [\n"
        "    {\n"
        "      \"damage\": 21,\n"
        "      \"speed\": 500,\n"
        "      \"penetration\": 1,\n"
        "      \"channel\": 0,\n"
        "      \"homing\": false,\n"
        "      \"effect\": {\n"
        "        \"debuff\": {\n"
        "          \"amount\": 7\n"
        "        }\n"
        "      }\n"
        "    },\n"
        "    {\n"
        "      \"damage\": 21,\n"
        "      \"speed\": 500,\n"
        "      \"penetration\": 1,\n"
        "      \"channel\": 0,\n"
        "      \"homing\": false,\n"
        "      \"effect\": {}\n"
        "    }\n"
        "  ],\n"
        "  \"attachments\": [\n"
        "    {\n"
        "      \"slot\": 3,\n"
        "      \"power\": 1.5\n"
        "    }\n"
        "  ]\n"
        // the canonical text ends with exactly ONE newline (docs/SPEC-TABLES.md
        // §16.1): every writer emits it, so the pin carries it
        "}\n";

    int64_t size = tabledemo::LoadoutConfigToJsonMeasure( loadout );
    if ( size < 0 )
    {
        // a refusal here means a spelling the writer could not name: report
        // it as the failure it is rather than indexing a buffer by -1
        printf( "FAIL json pinned text: ToJsonMeasure refused an instance the page can spell\n" );
        failures++;
        return;
    }
    CHECK( size == (int64_t) strlen( expected ) );
    std::vector<char> text( (size_t) size + 1 );
    CHECK( tabledemo::LoadoutConfigToJson( loadout, text.data(), size ) == size );
    text[(size_t) size] = 0;
    if ( strcmp( text.data(), expected ) != 0 )
    {
        printf( "FAIL json pinned text: ToJson does not spell what the page says\n--- got ---\n%s\n--- want ---\n%s\n",
                text.data(), expected );
        failures++;
    }

    // the empty vocabulary spellings, pinned too: an empty flags mask is [],
    // an empty union is {}, and a defaulted enum is its variant's name
    tabledemo::LoadoutConfig plain;
    static const char * plain_head =
        "{\n"
        "  \"grade\": \"Silver\",\n"
        "  \"grades\": [],\n";
    int64_t plain_size = tabledemo::LoadoutConfigToJsonMeasure( plain );
    if ( plain_size < 0 ) { printf( "FAIL json pinned text: an all-default instance was refused\n" ); failures++; return; }
    std::vector<char> plain_text( (size_t) plain_size + 1 );
    CHECK( tabledemo::LoadoutConfigToJson( plain, plain_text.data(), plain_size ) == plain_size );
    plain_text[(size_t) plain_size] = 0;
    CHECK( strncmp( plain_text.data(), plain_head, strlen( plain_head ) ) == 0 );
    CHECK( strstr( plain_text.data(), "\"perks\": []" ) != NULL );
    CHECK( strstr( plain_text.data(), "\"effect\": {}" ) != NULL );

    // bytes are base64 AND PADDED on the way out, at all three lengths
    struct { const uint8_t * bytes; int32_t length; const char * spelling; } base64[] = {
        { (const uint8_t *) "\x01\x02\x03", 3, "\"icon\": \"AQID\"" },
        { (const uint8_t *) "\x01\x02",     2, "\"icon\": \"AQI=\"" },
        { (const uint8_t *) "\x01",         1, "\"icon\": \"AQ==\"" },
        { (const uint8_t *) "",             0, "\"icon\": \"\"" },
    };
    for ( const auto & row : base64 )
    {
        tabledemo::ProfileConfig profile;
        memcpy( profile.icon, row.bytes, (size_t) row.length );
        profile.icon_length = row.length;
        int64_t n = tabledemo::ProfileConfigToJsonMeasure( profile );
        if ( n < 0 ) { printf( "FAIL json pinned base64: refused\n" ); failures++; continue; }
        std::vector<char> out( (size_t) n + 1 );
        CHECK( tabledemo::ProfileConfigToJson( profile, out.data(), n ) == n );
        out[(size_t) n] = 0;
        if ( strstr( out.data(), row.spelling ) == NULL )
        {
            printf( "FAIL json pinned base64: %s not in the text\n", row.spelling );
            failures++;
        }
    }
    // ... and UNPADDED base64 still reads, so a text from elsewhere lands
    {
        tabledemo::ProfileConfig profile;
        tabledemo::TableReport report;
        const char * unpadded = "{ \"icon\": \"AQI\" }";
        CHECK( tabledemo::ProfileConfigFromJson( profile, unpadded, (int64_t) strlen( unpadded ), &report ) );
        CHECK( profile.icon_length == 2 && profile.icon[0] == 1 && profile.icon[1] == 2 );
        CHECK( !report.malformed );
    }
}

// ---- numbers: JSON has ONE number type, and no value the field cannot hold
// ---- ever reaches storage (docs/SPEC-TABLES.md §16.2)

static void test_json_number_grammar()
{
    // JSON's number production, and nothing else. A typo in an authoring file
    // is a DIAGNOSTIC, not a value: the worst failure mode for a config
    // pipeline is a garbled number that arrives as a clamped integer.
    static const char * not_json[] = {
        "{ \"heading\": 1-2 }",
        "{ \"heading\": 5+ }",
        "{ \"precision\": 1.2.3 }",
        "{ \"precision\": 1e5e5 }",
        "{ \"precision\": --3 }",
        "{ \"heading\": +7 }",
        "{ \"heading\": 007 }",
        "{ \"precision\": .5 }",
        "{ \"precision\": 3. }",
        "{ \"precision\": 1e }",
        "{ \"precision\": 1e+ }",
        "{ \"heading\": - }",
        "{ \"heading\": 0x10 }",
    };
    for ( const char * text : not_json )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        bool ok = tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report );
        if ( ok || !report.malformed || report.clamped != 0 )
        {
            printf( "FAIL json number grammar (%s): ok=%d malformed=%d clamped=%d\n",
                    text, ok, (int) report.malformed, report.clamped );
            failures++;
        }
    }

    // and the whole production IS accepted
    struct { const char * text; double expect; } good[] = {
        { "{ \"precision\": 0 }",        0.0 },
        { "{ \"precision\": -0 }",       0.0 },
        { "{ \"precision\": 1e3 }",      1000.0 },
        { "{ \"precision\": 1E3 }",      1000.0 },
        { "{ \"precision\": 1e+3 }",     1000.0 },
        { "{ \"precision\": 1e-3 }",     0.001 },
        { "{ \"precision\": -2.5 }",     -2.5 },
        { "{ \"precision\": 0.5 }",      0.5 },
        { "{ \"precision\": 123456.75 }", 123456.75 },
    };
    for ( const auto & row : good )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        CHECK( tabledemo::ProfileConfigFromJson( value, row.text, (int64_t) strlen( row.text ), &report ) );
        if ( value.precision != row.expect || report.malformed || report.kind_mismatch != 0 )
        {
            printf( "FAIL json number accepted (%s): got %g want %g\n", row.text, value.precision, row.expect );
            failures++;
        }
    }
}

static void test_json_integral_spellings()
{
    // JSON has one number type: 2.0 IS the integer 2 and 1e3 IS 1000. A text
    // written by any library that round-trips numbers through a double spells
    // integers this way — including this walker's own float writer — and
    // §16.3 exists so a declaration can meet a text that already exists.
    struct { const char * text; int64_t expect; } integral[] = {
        { "{ \"experience\": 1e3 }",     1000 },
        { "{ \"experience\": 2E0 }",     2 },
        { "{ \"experience\": 2.0 }",     2 },
        { "{ \"experience\": 2.00000 }", 2 },
        { "{ \"experience\": 1.5e3 }",   1500 },
        { "{ \"experience\": 4 }",       4 },
        { "{ \"experience\": 1e0 }",     1 },
    };
    for ( const auto & row : integral )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        CHECK( tabledemo::ProfileConfigFromJson( value, row.text, (int64_t) strlen( row.text ), &report ) );
        if ( (int64_t) value.experience != row.expect || report.kind_mismatch != 0 || report.malformed )
        {
            printf( "FAIL json integral spelling (%s): got %u want %lld kind=%d\n",
                    row.text, value.experience, (long long) row.expect, report.kind_mismatch );
            failures++;
        }
    }

    // a GENUINELY fractional value is still the wrong shape for an integer
    static const char * fractional[] = {
        "{ \"experience\": 2.5 }",
        "{ \"experience\": 1e-3 }",
        "{ \"experience\": 0.1 }",
        "{ \"experience\": -2.5 }",
    };
    for ( const char * text : fractional )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        if ( value.experience != 0 || report.kind_mismatch != 1 || report.malformed )
        {
            printf( "FAIL json fraction (%s): experience=%u kind=%d\n", text, value.experience, report.kind_mismatch );
            failures++;
        }
    }

    // an integral spelling still meets the width and the declared range
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"badge\": 3e2 }"; // 300 into a uint8
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.badge == 255 && report.clamped == 1 );
    }
    {
        // signed, negative, and past the width
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"tilt\": -2e3 }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.tilt == -128 && report.clamped == 1 );
    }
    {
        // an integral value past every integer width saturates and counts
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"epoch\": 1e30 }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.epoch == 18446744073709551615ull && report.clamped == 1 );
    }
}

static void test_json_no_infinity_reaches_storage()
{
    // A magnitude the field's format cannot hold never lands: storing the
    // infinity the conversion produced would leave an instance this walk
    // called CLEAN that ToJsonMeasure refuses forever, and §16.1's whole
    // invariant is that a text which reads clean writes back.
    struct { const char * text; const char * what; } overflow[] = {
        { "{ \"precision\": 1e400 }",   "float64 over" },
        { "{ \"precision\": -1e400 }",  "float64 under" },
        { "{ \"ratings\": [ 1e400 ] }", "float32 array over" },
        { "{ \"ratings\": [ -1e400 ] }", "float32 array under" },
        { "{ \"ratings\": [ 1e300 ] }", "float64 magnitude into a float32" },
    };
    for ( const auto & row : overflow )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        bool ok = tabledemo::ProfileConfigFromJson( value, row.text, (int64_t) strlen( row.text ), &report );
        if ( !ok || report.kind_mismatch != 1 )
        {
            printf( "FAIL json overflow (%s): ok=%d kind=%d\n", row.what, ok, report.kind_mismatch );
            failures++;
        }
        // the field kept its default, and the instance is still writable
        if ( tabledemo::ProfileConfigToJsonMeasure( value ) <= 0 )
        {
            printf( "FAIL json overflow (%s): the instance can no longer be written\n", row.what );
            failures++;
        }
    }

    // -0 IS zero for an unsigned field, and reports nothing: a clamp count
    // is an event, and that event did not happen
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"experience\": -0, \"precision\": -0 }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.experience == 0 && report.clamped == 0 && report.kind_mismatch == 0 );
    }
    {
        // ... while a real negative for an unsigned field does clamp, and says so
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"experience\": -5 }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.experience == 0 && report.clamped == 1 );
    }

    // ... and a magnitude that DOES fit still lands exactly
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"precision\": 1e308, \"ratings\": [ 3.4e38 ] }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.precision == 1e308 && report.kind_mismatch == 0 );
        CHECK( tabledemo::ProfileConfigToJsonMeasure( value ) > 0 );
    }
}

static void test_json_duplicate_arrays_last_wins()
{
    // "last wins" has to be true of an ARRAY key too, and it is wire-visible:
    // a fixed array writes every slot, so a second, shorter occurrence must
    // not leave the first occurrence's tail standing.
    {
        tabledemo::LoadoutConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"podium\": [ \"Bronze\", \"Silver\", \"Gold\" ], \"podium\": [ \"Gold\" ] }";
        CHECK( tabledemo::LoadoutConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.podium[0] == tabledemo::Grade::Gold );
        CHECK( value.podium[1] == tabledemo::Grade::None );
        CHECK( value.podium[2] == tabledemo::Grade::None );
        CHECK( report.duplicate == 1 );

        // and it matches the instance the SECOND occurrence alone describes
        tabledemo::LoadoutConfig once;
        const char * single = "{ \"podium\": [ \"Gold\" ] }";
        CHECK( tabledemo::LoadoutConfigFromJson( once, single, (int64_t) strlen( single ), &report ) );
        uint8_t a[1024], b[1024];
        int64_t na = tabledemo::LoadoutConfigSave( value, a, sizeof( a ) );
        int64_t nb = tabledemo::LoadoutConfigSave( once, b, sizeof( b ) );
        CHECK( na > 0 && na == nb && memcmp( a, b, (size_t) na ) == 0 );
    }
    {
        // a counted array, and a fixed array of TABLES (whose elements go back
        // to their own declared defaults, not to zero)
        tabledemo::LoadoutConfig value;
        tabledemo::TableReport report;
        const char * text =
            "{ \"grades\": [ \"Gold\", \"Bronze\", \"Silver\" ], \"grades\": [ \"Bronze\" ],"
            "  \"backups\": [ { \"damage\": 1 }, { \"damage\": 2 } ], \"backups\": [ { \"damage\": 3 } ] }";
        CHECK( tabledemo::LoadoutConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.grades_count == 1 && value.grades[0] == tabledemo::Grade::Bronze );
        CHECK( value.backups[0].damage == 3 );     // the SECOND occurrence's element
        CHECK( value.backups[1].damage == 21.0f ); // the DECLARED default, not 2
        CHECK( report.duplicate == 2 );
    }
    {
        // bytes too
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"icon\": \"AQIDBAUG\", \"icon\": \"/w==\" }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.icon_length == 1 && value.icon[0] == 0xff );
        CHECK( report.duplicate == 1 );
    }
    {
        // and a float array, the review's own repro
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"ratings\": [ 1.5, 2.5, 3.5, 4.5 ], \"ratings\": [ 9.5 ] }";
        CHECK( tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.ratings[0] == 9.5f && value.ratings[1] == 0.0f &&
               value.ratings[2] == 0.0f && value.ratings[3] == 0.0f );
    }
}

static void test_json_null_is_a_kind_mismatch()
{
    // null is a JSON value with no field kind to land in — there is no
    // pointer row on this page — so it is the wrong shape everywhere, and it
    // is skipped and counted like any other wrong shape.
    static const char * nulls[] = {
        "{ \"experience\": null }",
        "{ \"name\": null }",
        "{ \"icon\": null }",
        "{ \"ratings\": null }",
        "{ \"has_loadout\": null }",
        "{ \"loadout\": null }",
        "{ \"precision\": null }",
    };
    for ( const char * text : nulls )
    {
        tabledemo::ProfileConfig value;
        tabledemo::TableReport report;
        bool ok = tabledemo::ProfileConfigFromJson( value, text, (int64_t) strlen( text ), &report );
        if ( !ok || report.kind_mismatch != 1 || report.malformed )
        {
            printf( "FAIL json null (%s): ok=%d kind=%d malformed=%d\n",
                    text, ok, report.kind_mismatch, (int) report.malformed );
            failures++;
        }
    }
    // and inside a vocabulary array, and inside a union
    {
        tabledemo::LoadoutConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"grades\": [ \"Gold\", null ], \"perks\": [ null ] }";
        CHECK( tabledemo::LoadoutConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.grades_count == 2 && value.grades[0] == tabledemo::Grade::Gold );
        CHECK( report.kind_mismatch == 2 );
    }
}

// ---- composed guards: nested and negated branches ------------------------
//
// Tables.schema's only guard is a flat `if has_loadout`, so the composition
// TableJsonGuardHolds actually has to parse — "active && !has_target" — was
// never reached by this suite. Patrol reaches all four states.

static void test_json_nested_guards()
{
    struct { bool active; bool has_target; } states[] = {
        { false, false }, { false, true }, { true, false }, { true, true },
    };
    for ( const auto & state : states )
    {
        tabledemo::Patrol value;
        value.active = state.active;
        value.has_target = state.has_target;
        value.speed = 3.5f;
        value.target_id = 42;
        value.wander = 9.5f;
        set_string( value.note, value.note_length, "patrol" );

        int64_t size = tabledemo::PatrolToJsonMeasure( value );
        CHECK( size > 0 );
        std::vector<char> text( (size_t) size + 1 );
        CHECK( tabledemo::PatrolToJson( value, text.data(), size ) == size );
        text[(size_t) size] = 0;

        // the text carries exactly the fields the WIRE would carry: a guard
        // decides both, from one composition
        CHECK( ( strstr( text.data(), "\"speed\"" ) != NULL ) == state.active );
        CHECK( ( strstr( text.data(), "\"has_target\"" ) != NULL ) == state.active );
        CHECK( ( strstr( text.data(), "\"target_id\"" ) != NULL ) == ( state.active && state.has_target ) );
        CHECK( ( strstr( text.data(), "\"wander\"" ) != NULL ) == ( state.active && !state.has_target ) );
        CHECK( ( strstr( text.data(), "\"note\"" ) != NULL ) == !state.active );
        CHECK( strstr( text.data(), "\"active\"" ) != NULL ); // the guard itself is a plain key

        // and the round trip lands the same wire
        tabledemo::Patrol back;
        tabledemo::TableReport report;
        CHECK( tabledemo::PatrolFromJson( back, text.data(), size, &report ) );
        CHECK( !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 );
        uint8_t a[512], b[512];
        int64_t na = tabledemo::PatrolSave( value, a, sizeof( a ) );
        int64_t nb = tabledemo::PatrolSave( back, b, sizeof( b ) );
        if ( na <= 0 || na != nb || memcmp( a, b, (size_t) na ) != 0 )
        {
            printf( "FAIL json nested guard (active=%d has_target=%d): wire %lld vs %lld\n",
                    (int) state.active, (int) state.has_target, (long long) na, (long long) nb );
            failures++;
        }
    }

    // reading is ORDER-FREE: every key placed before either guard is named,
    // and the instance still matches the one built by hand
    {
        tabledemo::Patrol value;
        tabledemo::TableReport report;
        const char * text =
            "{ \"target_id\": 7, \"wander\": 1.5, \"note\": \"x\", \"speed\": 2.5,"
            "  \"has_target\": true, \"active\": true }";
        CHECK( tabledemo::PatrolFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.active && value.has_target && value.target_id == 7 && value.speed == 2.5f );

        tabledemo::Patrol hand;
        hand.active = true;
        hand.has_target = true;
        hand.target_id = 7;
        hand.speed = 2.5f;
        hand.wander = 1.5f;
        set_string( hand.note, hand.note_length, "x" );
        uint8_t a[512], b[512];
        int64_t na = tabledemo::PatrolSave( value, a, sizeof( a ) );
        int64_t nb = tabledemo::PatrolSave( hand, b, sizeof( b ) );
        CHECK( na > 0 && na == nb && memcmp( a, b, (size_t) na ) == 0 );
    }

    JSON_ROUND_TRIP( tabledemo, Patrol, ( []() { tabledemo::Patrol p; p.active = true; p.has_target = false; p.speed = 8.0f; p.wander = 2.5f; return p; }() ) );
}

// ---- the two constructs #265 added, in the text form --------------------
//
// `?T` (docs/SPEC-TABLES.md §2.3): PRESENCE OF THE KEY IS THE PRESENCE.
// `[E]T` (§2.4): an OBJECT KEYED BY VARIANT NAME, because that is what the
// storage is — one slot per NAMED variant, addressed by the variant, with
// nothing stored for None.

static tabledemo::KeyedConfig json_keyed_instance()
{
    tabledemo::KeyedConfig cfg;
    cfg.teams[tabledemo::Team::Red].spawn_count = 9;
    set_string( cfg.teams[tabledemo::Team::Blue].banner,
                cfg.teams[tabledemo::Team::Blue].banner_length, "blue" );
    cfg.hulls[tabledemo::Hull::Gunship].health = 55.0f;
    cfg.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Missile].damage = 7.5f;
    cfg.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Missile].gunner_present = true;
    cfg.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Missile].gunner.reaction = 0.9f;
    cfg.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Missile].gunner.tracking = true;
    cfg.scores.per_team[keyed_index( tabledemo::Team::Green )] = 1234;
    return cfg;
}

static void test_json_keyed_and_optional_round_trip()
{
    tabledemo::KeyedConfig cfg = json_keyed_instance();
    JSON_ROUND_TRIP( tabledemo, KeyedConfig, cfg );
    JSON_ROUND_TRIP( tabledemo, HullConfig, cfg.hulls[tabledemo::Hull::Gunship] );
    JSON_ROUND_TRIP( tabledemo, TeamConfig, cfg.teams[tabledemo::Team::Red] );
    JSON_ROUND_TRIP( tabledemo, ScoreBoard, cfg.scores );
    // an optional PRESENT and an optional ABSENT are different values, and
    // both have to survive the trip
    JSON_ROUND_TRIP( tabledemo, TurretConfig,
                     cfg.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Missile] );
    JSON_ROUND_TRIP( tabledemo, TurretConfig,
                     cfg.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Cannon] );
    // ... including a PRESENT optional holding nothing but defaults, which is
    // the case elision by content would destroy
    tabledemo::TurretConfig defaulted;
    defaulted.gunner_present = true;
    JSON_ROUND_TRIP( tabledemo, TurretConfig, defaulted );

    tabledemo::KeyedConfig empty;
    JSON_ROUND_TRIP( tabledemo, KeyedConfig, empty );
}

static void test_json_optional_presence()
{
    // an absent optional writes NO KEY: nothing else would read back absent
    {
        tabledemo::TurretConfig absent;
        int64_t size = tabledemo::TurretConfigToJsonMeasure( absent );
        CHECK( size > 0 );
        std::vector<char> text( (size_t) size + 1 );
        CHECK( tabledemo::TurretConfigToJson( absent, text.data(), size ) == size );
        text[(size_t) size] = 0;
        CHECK( strstr( text.data(), "\"gunner\"" ) == NULL );
    }
    // a present one writes its key, even holding only defaults
    {
        tabledemo::TurretConfig present;
        present.gunner_present = true;
        int64_t size = tabledemo::TurretConfigToJsonMeasure( present );
        std::vector<char> text( (size_t) size + 1 );
        CHECK( tabledemo::TurretConfigToJson( present, text.data(), size ) == size );
        text[(size_t) size] = 0;
        CHECK( strstr( text.data(), "\"gunner\"" ) != NULL );
    }

    // PRESENCE OF THE KEY IS THE PRESENCE — whatever the value
    struct { const char * text; bool present; int kind_mismatch; const char * what; } rows[] = {
        { "{ \"gunner\": { \"reaction\": 0.5 } }", true,  0, "a value" },
        { "{ \"gunner\": {} }",                    true,  0, "an empty object" },
        { "{ \"damage\": 1 }",                     false, 0, "no key at all" },
        { "{ \"gunner\": null }",                  false, 0, "null reads as ABSENT" },
        { "{ \"gunner\": 7 }",                     true,  1, "a wrong-typed value is still a present key" },
        { "{ \"gunner\": \"x\" }",                 true,  1, "likewise a string" },
    };
    for ( const auto & row : rows )
    {
        tabledemo::TurretConfig value;
        tabledemo::TableReport report;
        bool ok = tabledemo::TurretConfigFromJson( value, row.text, (int64_t) strlen( row.text ), &report );
        if ( !ok || value.gunner_present != row.present || report.kind_mismatch != row.kind_mismatch )
        {
            printf( "FAIL json optional (%s): ok=%d present=%d want %d kind=%d want %d\n",
                    row.what, ok, (int) value.gunner_present, (int) row.present,
                    report.kind_mismatch, row.kind_mismatch );
            failures++;
        }
    }

    // null is ABSENT for a ?T and a KIND MISMATCH for everything else
    {
        tabledemo::TurretConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"damage\": null, \"gunner\": null }";
        CHECK( tabledemo::TurretConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.kind_mismatch == 1 );       // damage only
        CHECK( !value.gunner_present );
        CHECK( value.damage == 10.0f );           // the declared default
    }

    // last wins, and a null LAST leaves nothing of the value before it
    {
        tabledemo::TurretConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"gunner\": { \"reaction\": 9.5 }, \"gunner\": null }";
        CHECK( tabledemo::TurretConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( !value.gunner_present && report.duplicate == 1 );
        CHECK( value.gunner.reaction == 0.2f );   // back at its declared default
    }

    // an absent optional and a present-all-default one are DIFFERENT wires,
    // and the text distinguishes them
    {
        tabledemo::TurretConfig absent, present;
        present.gunner_present = true;
        uint8_t a[512], b[512];
        int64_t na = tabledemo::TurretConfigSave( absent, a, sizeof( a ) );
        int64_t nb = tabledemo::TurretConfigSave( present, b, sizeof( b ) );
        CHECK( na != nb ); // presence is the payload, not content
    }
}

static void test_json_keyed_arrays()
{
    // an absent key keeps that slot's defaults; the others still land
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Green\": { \"spawn_count\": 7 } } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.teams[tabledemo::Team::Green].spawn_count == 7 );
        CHECK( value.teams[tabledemo::Team::Red].spawn_count == 4 );  // the declared default
        CHECK( value.teams[tabledemo::Team::Blue].spawn_count == 4 );
        CHECK( report.unknown == 0 && !report.malformed );
    }

    // "None" NAMES NO SLOT (§2.4): nothing is stored for it, so the key is
    // unknown like any other name this reader cannot place
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"None\": { \"spawn_count\": 3 }, \"Red\": { \"spawn_count\": 5 } } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.unknown == 1 );
        CHECK( value.teams[tabledemo::Team::Red].spawn_count == 5 );
    }
    // ... and the writer never emits it
    {
        tabledemo::KeyedConfig value;
        int64_t size = tabledemo::KeyedConfigToJsonMeasure( value );
        std::vector<char> text( (size_t) size + 1 );
        CHECK( tabledemo::KeyedConfigToJson( value, text.data(), size ) == size );
        text[(size_t) size] = 0;
        CHECK( strstr( text.data(), "\"None\"" ) == NULL );
        CHECK( strstr( text.data(), "\"Red\"" ) != NULL );
    }

    // a variant this build cannot name is skipped and counted
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Violet\": { \"spawn_count\": 3 } } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.unknown == 1 && !report.malformed );
    }

    // a keyed array is an OBJECT: an array is the wrong shape for it
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": [ { \"spawn_count\": 3 } ] }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.kind_mismatch == 1 && !report.malformed );
    }
    // ... and a slot whose value is the wrong shape is counted, not coerced
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Red\": 5 } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.kind_mismatch == 1 );
        CHECK( value.teams[tabledemo::Team::Red].spawn_count == 4 );
    }

    // last wins for a repeated keyed field: the second object replaces the
    // first outright rather than overlaying it
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Red\": { \"spawn_count\": 5 }, \"Blue\": { \"spawn_count\": 6 } },"
                            "  \"teams\": { \"Blue\": { \"spawn_count\": 7 } } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.teams[tabledemo::Team::Blue].spawn_count == 7 );
        CHECK( value.teams[tabledemo::Team::Red].spawn_count == 4 ); // back at its default
        CHECK( report.duplicate == 1 );
    }

    // trailing commas inside a keyed object, like everywhere else
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Red\": { \"spawn_count\": 5 }, }, }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.teams[tabledemo::Team::Red].spawn_count == 5 && !report.malformed );
    }

    // a keyed array of SCALARS, on a plain `type`, keyed the same way
    {
        tabledemo::ScoreBoard value;
        tabledemo::TableReport report;
        const char * text = "{ \"per_team\": { \"Blue\": 900, \"Green\": 200000 } }";
        CHECK( tabledemo::ScoreBoardFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.per_team[keyed_index( tabledemo::Team::Blue )] == 900 );
        CHECK( value.per_team[keyed_index( tabledemo::Team::Green )] == 100000 ); // clamped to max
        CHECK( report.clamped == 1 );
    }
}

// ---- the pinned texts for both constructs -------------------------------

static void test_json_pinned_keyed_and_optional()
{
    // an optional PRESENT, and the object it writes
    tabledemo::TurretConfig turret;
    turret.damage = 7.5f;
    turret.gunner_present = true;
    turret.gunner.reaction = 0.9f;
    turret.gunner.tracking = true;
    static const char * present =
        "{\n"
        "  \"damage\": 7.5,\n"
        "  \"cooldown\": 0.5,\n"
        "  \"gunner\": {\n"
        "    \"reaction\": 0.9,\n"
        "    \"tracking\": true\n"
        "  }\n"
        "}\n";
    {
        int64_t size = tabledemo::TurretConfigToJsonMeasure( turret );
        if ( size < 0 ) { printf( "FAIL json pinned optional: refused\n" ); failures++; return; }
        std::vector<char> text( (size_t) size + 1 );
        CHECK( tabledemo::TurretConfigToJson( turret, text.data(), size ) == size );
        text[(size_t) size] = 0;
        if ( strcmp( text.data(), present ) != 0 )
        {
            printf( "FAIL json pinned optional (present)\n--- got ---\n%s\n--- want ---\n%s\n", text.data(), present );
            failures++;
        }
    }
    // ... and ABSENT: the key is simply not there
    static const char * absent =
        "{\n"
        "  \"damage\": 10,\n"
        "  \"cooldown\": 0.5\n"
        "}\n";
    {
        tabledemo::TurretConfig plain;
        int64_t size = tabledemo::TurretConfigToJsonMeasure( plain );
        if ( size < 0 ) { printf( "FAIL json pinned optional: refused\n" ); failures++; return; }
        std::vector<char> text( (size_t) size + 1 );
        CHECK( tabledemo::TurretConfigToJson( plain, text.data(), size ) == size );
        text[(size_t) size] = 0;
        if ( strcmp( text.data(), absent ) != 0 )
        {
            printf( "FAIL json pinned optional (absent)\n--- got ---\n%s\n--- want ---\n%s\n", text.data(), absent );
            failures++;
        }
    }

    // an enum-keyed array of scalars: one entry per SLOT, keyed by the
    // variant that owns it, and NO "None" row
    tabledemo::ScoreBoard scores;
    scores.per_team[keyed_index( tabledemo::Team::Red )] = 10;
    scores.per_team[keyed_index( tabledemo::Team::Green )] = 1234;
    static const char * keyed =
        "{\n"
        "  \"per_team\": {\n"
        "    \"Red\": 10,\n"
        "    \"Blue\": 0,\n"
        "    \"Green\": 1234\n"
        "  }\n"
        "}\n";
    {
        int64_t size = tabledemo::ScoreBoardToJsonMeasure( scores );
        if ( size < 0 ) { printf( "FAIL json pinned keyed: refused\n" ); failures++; return; }
        std::vector<char> text( (size_t) size + 1 );
        CHECK( tabledemo::ScoreBoardToJson( scores, text.data(), size ) == size );
        text[(size_t) size] = 0;
        if ( strcmp( text.data(), keyed ) != 0 )
        {
            printf( "FAIL json pinned keyed\n--- got ---\n%s\n--- want ---\n%s\n", text.data(), keyed );
            failures++;
        }
    }
}

// ---- the SEAM instances: `?T` (§2.3) and `[E]T` (§2.4) ----
//
// Built here and mirrored VALUE FOR VALUE by every port. The C# twin lives in
// test/cs-tables/src/Program.cs; a divergence in the instance is a divergence
// in the gate.

static void build_golden_keyed( tabledemo::KeyedConfig & cfg )
{
    tabledemo::TeamConfig & red = cfg.teams[tabledemo::Team::Red];
    red.spawn_count = 8;
    set_string( red.banner, red.banner_length, "red" );
    tabledemo::TeamConfig & green = cfg.teams[tabledemo::Team::Green];
    green.spawn_count = 2;
    set_string( green.banner, green.banner_length, "green" );
    // Blue's slot stays entirely default: a default slot ELIDES (§3.2)

    tabledemo::HullConfig & gunship = cfg.hulls[tabledemo::Hull::Gunship];
    gunship.health = 250.0f;
    gunship.mass = 3.5f;
    tabledemo::TurretConfig & cannon = gunship.turrets[tabledemo::Weapon::Cannon];
    cannon.damage = 40.0f;
    cannon.gunner_present = true;              // present, and entirely DEFAULT: it still rides
    tabledemo::TurretConfig & mine = gunship.turrets[tabledemo::Weapon::Mine];
    mine.damage = 5.0f;
    mine.cooldown = 9.0f;
    mine.gunner_present = true;
    mine.gunner.reaction = 0.75f;
    mine.gunner.tracking = true;

    tabledemo::HullConfig & freighter = cfg.hulls[tabledemo::Hull::Freighter];
    freighter.mass = 12.0f;                    // turrets all default: the keyed array elides whole

    // a `type`.s keyed field: PLAIN ARRAY storage, shifted like any other
    // keyed array (key k at index k-1), and a keyed BODY on this wire
    cfg.scores.per_team[keyed_index( tabledemo::Team::Red )] = 10;
    cfg.scores.per_team[keyed_index( tabledemo::Team::Green )] = 30;
}

static void build_golden_v1_seams( tblv1::Cfg & cfg )
{
    cfg.a = 3;
    cfg.bank[tblv1::Slot::Alpha].power = 11;
    set_string( cfg.bank[tblv1::Slot::Alpha].label, cfg.bank[tblv1::Slot::Alpha].label_length, "a1" );
    cfg.bank[tblv1::Slot::Beta].power = 22;    // ordinal 2 in V1, 3 in V2
    cfg.bank[tblv1::Slot::Gamma].power = 33;   // REMOVED in V2
    // Delta's slot stays default: it elides
    cfg.tokens[tblv1::Slot::Alpha] = 101;
    cfg.tokens[tblv1::Slot::Beta] = 102;
    cfg.tokens[tblv1::Slot::Delta] = 104;
    cfg.ranks[tblv1::Slot::Alpha] = tblv1::Grade::Gold;
    cfg.ranks[tblv1::Slot::Gamma] = tblv1::Grade::Bronze;
    cfg.ledger[0] = 7; cfg.ledger[2] = 9;      // POSITIONAL in V1, KEYED in V2: kind 14 vs 16
    cfg.extra_present = true;
    cfg.extra.factor = 6.25f;
    cfg.tier_present = true;
    cfg.tier = 41;
    cfg.mark_present = true;
    cfg.mark = tblv1::Grade::Gold;
}

static void build_golden_v2_seams( tblv2::Cfg & cfg )
{
    cfg.a = 1.5f;
    cfg.bank[tblv2::Slot::Alpha].power = 11;
    set_string( cfg.bank[tblv2::Slot::Alpha].label, cfg.bank[tblv2::Slot::Alpha].label_length, "a1" );
    cfg.bank[tblv2::Slot::Omega].power = 44;   // INSERTED in V2; V1 cannot name it
    cfg.bank[tblv2::Slot::Beta].power = 22;    // slid from ordinal 2 to 3
    cfg.bank[tblv2::Slot::Sigma].power = 55;   // appended; V1 cannot name it
    cfg.tokens[tblv2::Slot::Alpha] = 101;
    cfg.tokens[tblv2::Slot::Beta] = 102;
    cfg.ranks[tblv2::Slot::Alpha] = tblv2::Grade::Gold;
    cfg.ledger[tblv2::Grade::Bronze] = 7;
    cfg.ledger[tblv2::Grade::Gold] = 9;        // KEYED in V2
    cfg.extra_present = true;
    cfg.extra.factor = 6.25f;
    cfg.tier_present = false;                  // absent: nothing rides
    cfg.mark_present = true;
    cfg.mark = tblv2::Grade::Gold;
}

// the three-way T / *T / ?T set (§2.3, §3.1): one framing, three spellings.
// Byte-identical for content that is not entirely default; DIFFERENT at the
// empty end, and that asymmetry is pinned too.
static void build_golden_chain_value( tblp1::Chain & chain )
{
    set_string( chain.name, chain.name_length, "chain" );
    chain.link.value = 7;
    set_string( chain.link.tag, chain.link.tag_length, "tip" );
}

static void test_golden_seams()
{
    static uint8_t buffer[1u << 20];

    {
        static tabledemo::KeyedConfig cfg;
        build_golden_keyed( cfg );
        int64_t wrote = tabledemo::KeyedConfigSave( cfg, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tabledemo::KeyedConfigMeasure( cfg ) );
        pin_table_golden( "keyed_config", buffer, wrote );
    }
    {
        static tabledemo::KeyedConfig cfg; // every slot default: every keyed array elides
        int64_t wrote = tabledemo::KeyedConfigSave( cfg, buffer, sizeof( buffer ) );
        CHECK( wrote == 2 );
        pin_table_golden( "keyed_default", buffer, wrote );
    }
    {
        static tblv1::Cfg cfg;
        build_golden_v1_seams( cfg );
        int64_t wrote = tblv1::CfgSave( cfg, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tblv1::CfgMeasure( cfg ) );
        pin_table_golden( "v1_seams", buffer, wrote );
    }
    {
        static tblv2::Cfg cfg;
        build_golden_v2_seams( cfg );
        int64_t wrote = tblv2::CfgSave( cfg, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 && wrote == tblv2::CfgMeasure( cfg ) );
        pin_table_golden( "v2_seams", buffer, wrote );
    }

    // the SAME BYTES across the generation step, both directions, with the
    // counts pinned here and in the C# leg: two Gamma slots V2 cannot name,
    // Omega and Sigma V1 cannot, and `a` and `ledger` changing kind each way
    // (ledger is positional in V1 and KEYED in V2 — different kinds, so a
    // reported edit rather than a quiet one, docs/SPEC-TABLES.md §3.2)
    {
        static tblv1::Cfg v1;
        build_golden_v1_seams( v1 );
        int64_t v1_bytes = tblv1::CfgSave( v1, buffer, sizeof( buffer ) );
        tblv2::TableReport forward;
        static tblv2::Cfg new_reader;
        CHECK( tblv2::CfgLoad( new_reader, buffer, v1_bytes, &forward ) );
        CHECK( !forward.malformed && forward.unknown == 2 && forward.kind_mismatch == 2 && forward.clamped == 0 );
        CHECK( new_reader.bank[tblv2::Slot::Beta].power == 22 ); // by NAME, not by ordinal
        CHECK( new_reader.bank[tblv2::Slot::Omega].power == 0 );

        static tblv2::Cfg v2;
        build_golden_v2_seams( v2 );
        int64_t v2_bytes = tblv2::CfgSave( v2, buffer, sizeof( buffer ) );
        tblv1::TableReport back;
        static tblv1::Cfg old_reader;
        CHECK( tblv1::CfgLoad( old_reader, buffer, v2_bytes, &back ) );
        CHECK( !back.malformed && back.unknown == 2 && back.kind_mismatch == 2 );
        CHECK( old_reader.bank[tblv1::Slot::Beta].power == 22 );
        CHECK( old_reader.bank[tblv1::Slot::Gamma].power == 0 );
    }

    // the three spellings over NON-DEFAULT content: byte-identical
    {
        tblp1::Chain value;
        build_golden_chain_value( value );
        int64_t wrote = tblp1::ChainSave( value, buffer, sizeof( buffer ) );
        CHECK( wrote > 0 );
        pin_table_golden( "chain_value", buffer, wrote );

        tblp3::Chain optional;
        set_string( optional.name, optional.name_length, "chain" );
        optional.link_present = true;
        optional.link.value = 7;
        set_string( optional.link.tag, optional.link.tag_length, "tip" );
        static uint8_t other[4096];
        int64_t w_opt = tblp3::ChainSave( optional, other, sizeof( other ) );
        CHECK( w_opt == wrote && memcmp( buffer, other, (size_t) wrote ) == 0 );
        pin_table_golden( "chain_optional", other, w_opt );

        // the POINTER spelling is its OWN bytes: a u32 index under kind 17 and
        // the body moved into the node table (docs/SPEC-TABLES.md §3.1)
        tblp2::ChainBuilder builder;
        tblp2::Chain * root = builder.GetRoot();
        set_string( root->name, root->name_length, "chain" );
        tblp2::TableSlot<tblp2::Link> node = builder.Alloc<tblp2::Link>();
        node->value = 7;
        set_string( node->tag, node->tag_length, "tip" );
        root->link = node;
        CHECK( builder.Lock() );
        int64_t w_ptr = tblp2::ChainSave( builder, other, sizeof( other ) );
        CHECK( w_ptr > wrote );
        pin_table_golden( "chain_pointer", other, w_ptr );
    }

    // and the ASYMMETRY at the empty end: a by-value nesting at its defaults
    // writes nothing, while a PRESENT optional and a non-null pointer write
    // their body anyway (§2.3, §3.1)
    {
        tblp1::Chain value; // link entirely default: elides
        int64_t w_value = tblp1::ChainSave( value, buffer, sizeof( buffer ) );
        CHECK( w_value == 2 );
        pin_table_golden( "chain_value_empty", buffer, w_value );

        tblp3::Chain optional;
        optional.link_present = true; // present and all-default: it RIDES
        static uint8_t other[4096];
        int64_t w_opt = tblp3::ChainSave( optional, other, sizeof( other ) );
        CHECK( w_opt > w_value );
        pin_table_golden( "chain_optional_empty", other, w_opt );

        tblp2::ChainBuilder builder;
        tblp2::Chain * root = builder.GetRoot();
        root->link = builder.Alloc<tblp2::Link>(); // non-null and all-default: it RIDES
        CHECK( builder.Lock() );
        static uint8_t pointered[4096];
        int64_t w_ptr = tblp2::ChainSave( builder, pointered, sizeof( pointered ) );
        CHECK( w_ptr > w_value ); // the index rides, and so does its node's record
        pin_table_golden( "chain_pointer_empty", pointered, w_ptr );
    }
}

// ---- a keyed object's keys ARE keys (docs/SPEC-TABLES.md §16.2) ---------------
//
// Last-wins inside a keyed object was always true — each placement
// re-establishes the slot — so the VALUE was right and only the ledger was
// wrong, which is precisely why no round-trip test could see the repeat go
// uncounted. It took the pack engine's two-sided gate to find it.

static void test_json_keyed_duplicate_keys()
{
    // one variant named twice: last wins, and the repeat is COUNTED
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Red\": { \"spawn_count\": 5 }, \"Red\": { \"spawn_count\": 9 } } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.teams[tabledemo::Team::Red].spawn_count == 9 );
        CHECK( report.duplicate == 1 );
        CHECK( report.unknown == 0 && report.kind_mismatch == 0 && !report.malformed );
    }

    // every repeat past the first counts, across variants
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Red\": {}, \"Red\": {}, \"Red\": {}, \"Blue\": {}, \"Blue\": {} } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.duplicate == 3 ); // two extra Reds, one extra Blue
    }

    // DISTINCT variants are not duplicates — the whole point of the key
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Red\": {}, \"Blue\": {}, \"Green\": {} } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.duplicate == 0 );
    }

    // a repeated key that names NO slot is unknown twice, never a duplicate:
    // it was never placed, so there is nothing for a second one to replace
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Violet\": {}, \"Violet\": {}, \"None\": {}, \"None\": {} } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.unknown == 4 && report.duplicate == 0 );
    }

    // last-wins is a WHOLE-VALUE replacement here too: the second occurrence
    // does not overlay the first, it re-establishes the slot
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"teams\": { \"Red\": { \"spawn_count\": 9, \"banner\": \"first\" },"
                            "              \"Red\": { \"spawn_count\": 2 } } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.teams[tabledemo::Team::Red].spawn_count == 2 );
        CHECK( value.teams[tabledemo::Team::Red].banner_length == 0 ); // nothing of the first survives
        CHECK( report.duplicate == 1 );

        // and the instance is the one the SECOND occurrence alone describes
        tabledemo::KeyedConfig once;
        const char * single = "{ \"teams\": { \"Red\": { \"spawn_count\": 2 } } }";
        CHECK( tabledemo::KeyedConfigFromJson( once, single, (int64_t) strlen( single ), &report ) );
        uint8_t a[4096], b[4096];
        int64_t na = tabledemo::KeyedConfigSave( value, a, sizeof( a ) );
        int64_t nb = tabledemo::KeyedConfigSave( once, b, sizeof( b ) );
        CHECK( na > 0 && na == nb && memcmp( a, b, (size_t) na ) == 0 );
    }

    // a keyed array of SCALARS counts the same way
    {
        tabledemo::ScoreBoard value;
        tabledemo::TableReport report;
        const char * text = "{ \"per_team\": { \"Blue\": 1, \"Blue\": 2, \"Blue\": 3 } }";
        CHECK( tabledemo::ScoreBoardFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.per_team[keyed_index( tabledemo::Team::Blue )] == 3 );
        CHECK( report.duplicate == 2 );
    }

    // a repeat NESTED one keyed object down still counts
    {
        tabledemo::KeyedConfig value;
        tabledemo::TableReport report;
        const char * text = "{ \"hulls\": { \"Gunship\": { \"turrets\": {"
                            "   \"Missile\": { \"damage\": 1 }, \"Missile\": { \"damage\": 2 } } } } }";
        CHECK( tabledemo::KeyedConfigFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( value.hulls[tabledemo::Hull::Gunship].turrets[tabledemo::Weapon::Missile].damage == 2.0f );
        CHECK( report.duplicate == 1 );
    }

    // the PINNED TEXT: a repeated key is a READ-side event only, so what the
    // writer emits for the instance it lands on carries each variant exactly
    // once — the repeat leaves no trace in the text, only in the ledger
    {
        tabledemo::ScoreBoard value;
        tabledemo::TableReport report;
        const char * text = "{ \"per_team\": { \"Red\": 4, \"Green\": 7, \"Green\": 9 } }";
        CHECK( tabledemo::ScoreBoardFromJson( value, text, (int64_t) strlen( text ), &report ) );
        CHECK( report.duplicate == 1 );

        static const char * expected =
            "{\n"
            "  \"per_team\": {\n"
            "    \"Red\": 4,\n"
            "    \"Blue\": 0,\n"
            "    \"Green\": 9\n"
            "  }\n"
            "}\n";
        int64_t size = tabledemo::ScoreBoardToJsonMeasure( value );
        if ( size < 0 ) { printf( "FAIL json keyed duplicate pinned text: refused\n" ); failures++; return; }
        std::vector<char> out( (size_t) size + 1 );
        CHECK( tabledemo::ScoreBoardToJson( value, out.data(), size ) == size );
        out[(size_t) size] = 0;
        if ( strcmp( out.data(), expected ) != 0 )
        {
            printf( "FAIL json keyed duplicate pinned text\n--- got ---\n%s\n--- want ---\n%s\n",
                    out.data(), expected );
            failures++;
        }
    }
}

int main()
{
    test_golden_wire();
    test_golden_seams();
    test_golden_reload();
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
    test_keyed_iteration();
    test_keyed_none_index_refused();
    test_keyed_past_max_index_refused();
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
    test_pointer_shared_node();
    test_pointer_cycle_refused();
    test_pointer_long_chain();
    test_builder_grow();
    test_builder_workers();
    test_pointer_evolution_old_reader_new_data();
    test_pointer_evolution_new_reader_old_data();
    test_pointer_null_and_empty();
    test_pointer_reflection();

    test_lock_deterministic_on_dirty_heap();
    test_depth_agrees_through_by_value_nesting();
    test_descriptors_are_constant();
    test_cross_file_pointer_unit();

    test_json_corpus_round_trip();
    test_json_dialect();
    test_json_hostile_kinds();
    test_json_hostile_overflow();
    test_json_hostile_extents();
    test_json_hostile_unions();
    test_json_hostile_framing();
    test_json_writer_refusals();
    test_json_guards();
    test_json_key_attribute();
    test_json_number_grammar();
    test_json_integral_spellings();
    test_json_no_infinity_reaches_storage();
    test_json_duplicate_arrays_last_wins();
    test_json_null_is_a_kind_mismatch();
    test_json_nested_guards();
    test_json_keyed_and_optional_round_trip();
    test_json_optional_presence();
    test_json_keyed_arrays();
    test_json_keyed_duplicate_keys();
    test_json_pinned_keyed_and_optional();
    test_json_pinned_text();
    test_json_fuzz_tokenizer();

    if ( failures > 0 )
    {
        printf( "tables test: %d failure(s)\n", failures );
        return 1;
    }
    printf( "tables test passed\n" );
    return 0;
}
