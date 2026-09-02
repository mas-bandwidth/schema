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
#include "NestedTable.h"
#include "WideTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "GraphTable.h"
#include "PartsTable.h"
#include "MarksTable.h"
#include "P1Table.h"
#include "P2Table.h"

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

// a value no variant names has no wire identity: measure and save refuse it,
// exactly as they refuse an out-of-range union tag
static void test_unnameable_enum_refused()
{
    uint8_t buffer[256];
    tblv1::Cfg cfg;
    cfg.grade = (tblv1::Grade) 9;
    CHECK( tblv1::CfgMeasure( cfg ) == -1 );
    CHECK( tblv1::CfgSave( cfg, buffer, sizeof( buffer ) ) == -1 );
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
    test_unnameable_enum_refused();
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
