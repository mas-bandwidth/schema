// The TABLE-wire conformance test (SPEC-TABLES.md). Three generated units in
// one binary: the tables corpus (tabledemo), and the two-generation evolution
// pair (tblv1/tblv2) whose schemas disagree on purpose. Compiled WITHOUT the
// serialize include path — the Table headers must stand alone.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "TablesTable.h"
#include "NestedTable.h"
#include "V1Table.h"
#include "V2Table.h"

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

// check_exact_capacity is the go-wide guarantee: TableSave into a buffer of
// EXACTLY TableMeasure's size succeeds and byte-matches a roomy save — a
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

    // body_len = 3 covers only elem_kind + count; count claims 2 elements —
    // the elements would have to come from the NEXT field's bytes. They must
    // not: prefix kept (empty), malformed flagged, and a = 42 still decodes.
    uint8_t wire[32];
    int n = 0;
    le16( wire + n, items->id ); n += 2;
    wire[n++] = 14;              // kArray
    le32( wire + n, 3 ); n += 4; // body_len: header only, no element bytes
    wire[n++] = 4;               // elem_kind kI32
    le16( wire + n, 2 ); n += 2; // count 2 — a lie
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
    le32( wire + n, 3 + 4 + 2 ); n += 4; // one i32 element + 2 slack bytes
    wire[n++] = 4;
    le16( wire + n, 2 ); n += 2;         // count 2, body holds 1.5
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
    le16( wire4 + n, 40 ); n += 2; // longer than string(32)
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
    test_clamping();
    test_malformed();
    test_reflection();
    test_cross_file();
    test_parallel_shape();

    if ( failures > 0 )
    {
        printf( "tables test: %d failure(s)\n", failures );
        return 1;
    }
    printf( "tables test passed\n" );
    return 0;
}
