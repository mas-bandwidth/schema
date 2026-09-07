// THE MAP GATE (docs/SPEC-TABLES.md §2.8). One binary over the `tables/maps`
// unit: the builder's five, the sort the four writing walks hold, the node
// extent a region and a cook carry, every reader rule, and the negative
// controls §2.8 names — each row here is one of them, and the comment says
// which sabotage it turns red.
//
// Compiled WITHOUT the serialize include path: the Table headers stand alone.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <new>

#include "FleetTable.h"
#include "RowsTable.h"
#include "DepthTable.h"
#include "wirebuilder.h"

using namespace mapdemo;

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

#define CHECK_EQ( actual, expected )                                          \
    do                                                                        \
    {                                                                         \
        const long long a_ = (long long) ( actual );                          \
        const long long e_ = (long long) ( expected );                        \
        if ( a_ != e_ )                                                       \
        {                                                                     \
            printf( "FAIL %s:%d: %s = %lld, want %lld\n",                     \
                    __FILE__, __LINE__, #actual, a_, e_ );                    \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

// ---- every allocation sized from a measure goes through here ----
//
// A measure is an answer from the code under test, so a region sized from one
// is the single place a broken measure reaches the allocator. The ceiling is
// CHECKED first, and a measure past it is a red CHECK on every platform rather
// than a call to calloc: glibc answers NULL for a request of that size and the
// caller's own CHECK fires, while macOS accepts it and the kernel kills the
// process, which loses the buffered CHECK output the negative controls read.
// That difference is what made the `fit` control pass on Linux and fail on
// macOS with `Killed: 9`.
//
// 256 MiB: every measure this corpus produces is under 64 KiB, the size of the
// buffers its wire bodies are built in, so the ceiling leaves more than three
// orders of magnitude of headroom and still sits far below what a sabotaged
// measure asks for — the `fit` control's answer is 85899345944, about 86 GB.

static const int64_t kMeasureCeiling = 256 * 1024 * 1024;

static void * measured_calloc( int64_t measure, int64_t extra, const char * expr, const char * file, int line )
{
    if ( measure < 0 || measure > kMeasureCeiling )
    {
        printf( "FAIL %s:%d: %s = %lld, past the %lld byte measure ceiling\n",
                file, line, expr, (long long) measure, (long long) kMeasureCeiling );
        failures++;
        return NULL;
    }
    return calloc( 1, (size_t) ( measure + extra ) );
}

// The caller takes NULL back and stops: the CHECK is already recorded, and
// nothing past a refused measure is worth reading.
#define MEASURED_CALLOC( measure, extra )                                     \
    measured_calloc( ( measure ), ( extra ), #measure, __FILE__, __LINE__ )

// ---- the shared golden wire (docs/SPEC-TABLES.md §3) ----
//
// The C++ reference is the writer: these instances' encodings are pinned into
// testdata/wire/tables/<name>.bin. A break here under an unchanged schema is
// stop-the-line, never a quiet re-pin — SCHEMA_UPDATE_WIRE_GOLDENS=1 rewrites
// them deliberately (make update-goldens).

static void pin_golden( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/tables/%s.bin", name );
    if ( getenv( "SCHEMA_UPDATE_WIRE_GOLDENS" ) )
    {
        FILE * f = fopen( path, "wb" );
        if ( f == NULL ) { printf( "FAIL cannot write %s\n", path ); failures++; return; }
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
    const size_t n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int64_t) n != bytes || memcmp( expected, data, n ) != 0 )
    {
        printf( "FAIL table wire golden %s: %lld bytes written, %lld pinned\n",
                name, (long long) bytes, (long long) n );
        failures++;
    }
}

// A COOK IS WRITTEN FOR THE BUILD THAT OPENS IT (docs/SPEC-TABLES.md §7): the
// host's own order, so the round trip below holds on the big-endian leg too.
static TableByteOrder host_byte_order()
{
    const uint16_t probe = 1;
    return *(const uint8_t *) &probe == 1 ? TableByteOrder::Little : TableByteOrder::Big;
}

// ---- the instances (docs/SPEC-TABLES.md §2.8) ----

// The FLEET instance, built OUT OF KEY ORDER on purpose, with two keys naming
// one node and a map of maps at three depths. `reversed` builds the same map
// from the opposite insertion order: ONE MAP FROM TWO INSERTION ORDERS
// produces one image on every form, and the byte compare between them goes red
// if the sort is dropped.
static void build_fleet( FleetBuilder & b, bool reversed )
{
    Fleet * fleet = b.GetRoot();

    const char * names[3] = { "bomber", "fighter", "scout" };
    const int32_t health[3] = { 400, 100, 30 };
    for ( int i = 0; i < 3; i++ )
    {
        const int k = reversed ? 2 - i : i;
        ShipConfig * ship = FleetShipsInsert( b.main, fleet->ships, names[k] );
        CHECK( ship != NULL );
        memcpy( ship->name, names[k], strlen( names[k] ) );
        ship->name_length = (int32_t) strlen( names[k] );
        ship->health = health[k];
    }

    // a map of *T: TWO KEYS, ONE NODE. The map is DECLARED BEFORE `flagship`,
    // so the walk reaches the shared node here and numbers it first — a walk
    // that grouped maps after the pointer fields would number them the other
    // way round and the pinned wire below says so.
    TableRef * slot = FleetByIdInsert( b.main, fleet->by_id, 7 );
    ShipConfig * shared = ShipConfigEmplace( b.main, *slot );
    CHECK( shared != NULL );
    memcpy( shared->name, "shared", 6 );
    shared->name_length = 6;
    shared->health = 50;
    *FleetByIdInsert( b.main, fleet->by_id, 12 ) = *slot;
    fleet->flagship = *slot; // the SAME node a third time, through a pointer field

    // a map of maps, by value, recursing
    TableMap<FleetLoadoutsEntryValueEntry> * kit = FleetLoadoutsInsert( b.main, fleet->loadouts, "kit" );
    CHECK( kit != NULL );
    FleetLoadoutsEntryValueInsert( b.main, *kit, (uint8_t) 9 )->count = 99;
    FleetLoadoutsEntryValueInsert( b.main, *kit, (uint8_t) 2 )->count = 22;

    // a SIGNED key: -3 sorts before 2, which an unsigned compare gets wrong
    FleetTiersInsert( b.main, fleet->tiers, (int16_t) 2 )->count = 2;
    FleetTiersInsert( b.main, fleet->tiers, (int16_t) -3 )->count = -3;
}

static uint8_t wire_full[1u << 16];
static uint8_t wire_reversed[1u << 16];
static int64_t bytes_full = 0;

// ---- the writer (docs/SPEC-TABLES.md §2.8) ----

static void test_writer()
{
    {
        FleetBuilder b;
        build_fleet( b, false );
        const int64_t measured = FleetMeasure( b );
        bytes_full = FleetSave( b, wire_full, sizeof( wire_full ) );
        // measure == save over a map is a real check on TWO SORTS AGREEING:
        // nothing passes between the two walks (§2.8)
        CHECK_EQ( measured, bytes_full );
        pin_golden( "map_full", wire_full, bytes_full );
        // MEASURE EQUALS SAVE AT EXACT CAPACITY: the buffer is the measure and
        // not a byte more, so a write that ran over would refuse rather than
        // fit by accident, and one short of it refuses
        static uint8_t exact[1u << 16];
        CHECK_EQ( FleetSave( b, exact, measured ), measured );
        CHECK( memcmp( exact, wire_full, (size_t) measured ) == 0 );
        CHECK_EQ( FleetSave( b, exact, measured - 1 ), -1 );
    }
    {
        // CONTROL: the writer emits INSERTION order instead of sorted. This
        // instance is built out of key order, so the byte compare goes red
        // while measure == save still holds — which says the sabotage is the
        // sort and not the arithmetic.
        FleetBuilder b;
        build_fleet( b, true );
        const int64_t measured = FleetMeasure( b );
        const int64_t n = FleetSave( b, wire_reversed, sizeof( wire_reversed ) );
        CHECK_EQ( measured, n );
        CHECK_EQ( n, bytes_full );
        CHECK( memcmp( wire_full, wire_reversed, (size_t) bytes_full ) == 0 );
    }
    {
        // CONTROL: `Save` emits a DEAD entry. An instance that ERASES meets
        // it: the byte compare against the pinned wire goes red while
        // measure == save still holds.
        FleetBuilder b;
        build_fleet( b, false );
        ShipConfig * doomed = FleetShipsInsert( b.main, b.GetRoot()->ships, "hauler" );
        CHECK( doomed != NULL );
        doomed->health = 7;
        CHECK( FleetShipsErase( b.arena, b.GetRoot()->ships, "hauler" ) );
        CHECK( !FleetShipsErase( b.arena, b.GetRoot()->ships, "hauler" ) ); // false when absent
        CHECK_EQ( b.GetRoot()->ships.count, 3 );
        static uint8_t erased[1u << 16];
        const int64_t measured = FleetMeasure( b );
        const int64_t n = FleetSave( b, erased, sizeof( erased ) );
        CHECK_EQ( measured, n );
        CHECK_EQ( n, bytes_full );
        CHECK( memcmp( wire_full, erased, (size_t) bytes_full ) == 0 );
    }
    {
        // an EMPTY map is elided under §3's by-value rule: a fresh Fleet is
        // the framing and nothing else — the form byte, the zero reference and
        // the entry count of an id table with no entries
        FleetBuilder b;
        static uint8_t empty[64];
        const int64_t n = FleetSave( b, empty, sizeof( empty ) );
        CHECK_EQ( n, empty_wire_bytes );
        pin_golden( "map_empty", empty, n );
    }
}

// ---- the builder's five (docs/SPEC-TABLES.md §2.8) ----

static void test_builder()
{
    FleetBuilder b;
    Fleet * fleet = b.GetRoot();

    // INSERT hands the value back at its defaults; a DUPLICATE key REPLACES,
    // the value reset and the entry's address unchanged
    ShipConfig * first = FleetShipsInsert( b.main, fleet->ships, "fighter" );
    first->health = 100;
    ShipConfig * again = FleetShipsInsert( b.main, fleet->ships, "fighter" );
    CHECK( again == first );      // the same entry
    CHECK_EQ( again->health, 0 ); // reset to its defaults
    CHECK_EQ( fleet->ships.count, 1 );
    again->health = 100;

    // FIND on the builder is the linear scan; NULL when absent
    CHECK( FleetShipsFind( b.arena, fleet->ships, "fighter" ) == first );
    CHECK( FleetShipsFind( b.arena, fleet->ships, "absent" ) == NULL );

    // CONTROL: a key longer than N on insert is REFUSED. The insert's NULL
    // goes red if a control clamps instead — a truncated key is a merged entry.
    char over[64];
    memset( over, 'k', sizeof( over ) );
    over[40] = 0;
    CHECK( FleetShipsInsert( b.main, fleet->ships, over ) == NULL );
    CHECK_EQ( fleet->ships.count, 1 );
    over[32] = 0; // exactly the bound: accepted
    CHECK( FleetShipsInsert( b.main, fleet->ships, over ) != NULL );
    CHECK_EQ( fleet->ships.count, 2 );

    // EACH on the builder: INSERTION order, live entries only
    FleetShipsInsert( b.main, fleet->ships, "alpha" )->health = 1;
    CHECK( FleetShipsErase( b.arena, fleet->ships, over ) );
    int seen = 0;
    const char * order[2] = { "fighter", "alpha" };
    for ( auto [ key, ship ] : FleetShipsEach( b.arena, fleet->ships ) )
    {
        if ( seen < 2 ) { CHECK( strcmp( key, order[seen] ) == 0 ); }
        (void) ship;
        seen++;
    }
    CHECK_EQ( seen, 2 );

    // MORE THAN ONE SEGMENT: an entry's address is stable for the arena's life,
    // so a value handed back by an early insert survives every later one
    FleetBuilder many;
    Fleet * big = many.GetRoot();
    Item * held = FleetTiersInsert( many.main, big->tiers, (int16_t) 0 );
    held->count = 1234;
    for ( int32_t i = 1; i < 200; i++ )
    {
        CHECK( FleetTiersInsert( many.main, big->tiers, (int16_t) i ) != NULL );
    }
    CHECK_EQ( big->tiers.count, 200 );
    CHECK_EQ( held->count, 1234 ); // NOTHING EVER MOVES (§6.4)
    CHECK( many.Lock() );
    const TableMap<FleetTiersEntry> & sorted = many.AsConst()->tiers;
    CHECK_EQ( sorted.size(), 200 );
    for ( int32_t i = 0; i < 200; i++ ) { CHECK( sorted.Find( (int16_t) i ) != NULL ); }
    CHECK_EQ( sorted.begin().at->key, 0 ); // and the sort put them in order
}

// ---- the const form: a locked region, a loaded one, an opened cook ----

static void check_const_form( const Fleet * f, const char * where )
{
    if ( f == NULL ) { printf( "FAIL %s: no root\n", where ); failures++; return; }
    CHECK_EQ( f->ships.size(), 3 );

    // FIND: a binary search over the sorted array, in place
    const ShipConfig * fighter = f->ships.Find( "fighter" );
    CHECK( fighter != NULL );
    if ( fighter != NULL ) { CHECK_EQ( fighter->health, 100 ); }
    CHECK( f->ships.Find( "absent" ) == NULL );

    // ITERATION is ASCENDING, whatever the insertion order was
    const char * ascending[3] = { "bomber", "fighter", "scout" };
    int i = 0;
    for ( auto [ key, ship ] : f->ships )
    {
        if ( i < 3 ) { CHECK( strcmp( key, ascending[i] ) == 0 ); }
        (void) ship;
        i++;
    }
    CHECK_EQ( i, 3 );

    // a map[K]*T's Find answers the RESOLVED pointer, and TWO KEYS HOLD ONE
    // NODE: a shared node written twice would show up here and in the byte count
    const ShipConfig * by7 = f->by_id.Find( (uint32_t) 7 );
    const ShipConfig * by12 = f->by_id.Find( (uint32_t) 12 );
    CHECK( by7 != NULL && by7 == by12 );
    if ( by7 != NULL ) { CHECK_EQ( by7->health, 50 ); }
    CHECK( ShipConfigAt( f->flagship ) == by7 ); // and the pointer field names it too

    // a map of maps, at three depths
    const TableMap<FleetLoadoutsEntryValueEntry> * kit = f->loadouts.Find( "kit" );
    CHECK( kit != NULL );
    if ( kit != NULL )
    {
        CHECK_EQ( kit->size(), 2 );
        const Item * nine = kit->Find( (uint8_t) 9 );
        const Item * two = kit->Find( (uint8_t) 2 );
        CHECK( nine != NULL && nine->count == 99 );
        CHECK( two != NULL && two->count == 22 );
    }

    // A SIGNED KEY COMPARES BY VALUE: -3 before 2, which an unsigned compare
    // would put the other way round
    CHECK_EQ( f->tiers.size(), 2 );
    if ( f->tiers.size() == 2 ) { CHECK_EQ( f->tiers.begin().at->key, -3 ); }
    const Item * low = f->tiers.Find( (int16_t) -3 );
    CHECK( low != NULL && low->count == -3 );

    // the OPTIONAL INDEX answers what Find answers, and allocates nothing past
    // the storage the caller handed in
    const int64_t bytes = FleetShipsIndexMeasure( f->ships );
    CHECK( bytes > 0 );
    void * storage = MEASURED_CALLOC( bytes, 0 );
    if ( storage == NULL ) { return; }
    TableMapIndex index = FleetShipsIndex( f->ships, storage, bytes );
    CHECK( index.good );
    const char * probes[4] = { "bomber", "fighter", "scout", "absent" };
    for ( int p = 0; p < 4; p++ )
    {
        CHECK( FleetShipsIndexFind( index, f->ships, probes[p] ) == f->ships.Find( probes[p] ) );
    }
    free( storage );
}

static void test_const_forms()
{
    // LOCK: sorts once, drops dead entries, and produces the region
    FleetBuilder b;
    build_fleet( b, true );
    CHECK( b.Lock() );
    check_const_form( b.AsConst(), "the locked region" );

    // a locked region re-saves the same bytes as the builder did
    static uint8_t again[1u << 16];
    const int64_t n = FleetSave( b.AsConst(), again, sizeof( again ) );
    CHECK_EQ( n, bytes_full );
    CHECK( memcmp( again, wire_full, (size_t) bytes_full ) == 0 );

    // LOAD into the caller's exact-sized region
    const int64_t need = FleetLoadMeasure( wire_full, bytes_full );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return; }
    TableReport report;
    const Fleet * loaded = FleetLoad( region, need, wire_full, bytes_full, &report );
    check_const_form( loaded, "a loaded region" );
    CHECK_EQ( report.unknown, 0 );
    CHECK_EQ( report.kind_mismatch, 0 );
    CHECK_EQ( report.clamped, 0 );
    CHECK_EQ( report.duplicate, 0 );
    CHECK( !report.malformed );

    // and a loaded region re-saves the same bytes
    const int64_t back = FleetSave( loaded, again, sizeof( again ) );
    CHECK_EQ( back, bytes_full );
    CHECK( memcmp( again, wire_full, (size_t) bytes_full ) == 0 );

    // LoadBuilder is the TOOL's path, and it produces EXACTLY the same report
    FleetBuilder into;
    TableReport tool;
    CHECK( FleetLoadBuilder( into, wire_full, bytes_full, &tool ) );
    CHECK_EQ( tool.unknown, report.unknown );
    CHECK_EQ( tool.kind_mismatch, report.kind_mismatch );
    CHECK_EQ( tool.clamped, report.clamped );
    CHECK_EQ( tool.duplicate, report.duplicate );
    CHECK_EQ( (int) tool.malformed, (int) report.malformed );
    CHECK_EQ( into.GetRoot()->ships.count, 3 );
    const int64_t relocked = FleetSave( into, again, sizeof( again ) );
    CHECK_EQ( relocked, bytes_full );
    CHECK( memcmp( again, wire_full, (size_t) bytes_full ) == 0 );

    // the COOK: a region written verbatim, opened O(1) and searched in place
    const int64_t cook_bytes = FleetCookMeasure( loaded );
    CHECK( cook_bytes > 0 );
    void * cooked = MEASURED_CALLOC( cook_bytes, 0 );
    if ( cooked == NULL ) { free( region ); return; }
    CHECK( FleetCook( loaded, cooked, (uint64_t) cook_bytes, host_byte_order() ) );
    check_const_form( FleetOpen( cooked, (uint64_t) cook_bytes ), "an opened cook" );
    // and two cooks of one instance are ONE artifact
    void * twice = MEASURED_CALLOC( cook_bytes, 0 );
    if ( twice == NULL ) { free( cooked ); free( region ); return; }
    CHECK( FleetCook( loaded, twice, (uint64_t) cook_bytes, host_byte_order() ) );
    CHECK( memcmp( cooked, twice, (size_t) cook_bytes ) == 0 );
    free( twice );
    free( cooked );
    free( region );
}

// ---- the reader's rules, each on a hand-made body (§2.8) ----

// ---- a Row body written FROM THE GRAMMAR (docs/SPEC-TABLES.md §2.8, §3) ----
//
// A map rides as kind 14 over element kind 13 — an array of the generated
// `{ key, value }` entry — so nothing here is a map-specific framing rule. The
// controls vary the ENTRY LIST and one knob each rather than patching a saved
// wire, because a canonical LEB128 length cannot be patched in place and a
// forgery has to say one thing.
struct RowWire
{
    uint8_t bytes[4096];
    int64_t size;
};

struct RowEntrySpec
{
    const char * key;
    int32_t key_bytes; // -1: the key's own length
    int32_t count;
    uint8_t key_kind;  // 0: the declaration's own kind
};

struct RowSpec
{
    RowEntrySpec entries[8];
    int32_t n;
    uint8_t element_kind; // 0: kind 13, an array of entry bodies
    int64_t declared_n;   // -1: the entry count itself
    int32_t after;
};

static RowSpec ascending_spec()
{
    RowSpec spec = {};
    const char * keys[3] = { "aa", "bb", "cc" };
    for ( int i = 0; i < 3; i++ )
    {
        spec.entries[i].key = keys[i];
        spec.entries[i].key_bytes = -1;
        spec.entries[i].count = 10 + i;
        spec.entries[i].key_kind = 0;
    }
    spec.n = 3;
    spec.element_kind = 0;
    spec.declared_n = -1;
    spec.after = 777;
    return spec;
}

static RowWire build_row( const RowSpec & spec )
{
    WireBuilder b;
    b.field( "entries", 14 );
    const int64_t map_body = b.open_len();
    b.u8( spec.element_kind != 0 ? spec.element_kind : 13 );
    b.leb( spec.declared_n >= 0 ? (uint64_t) spec.declared_n : (uint64_t) spec.n );
    for ( int32_t i = 0; i < spec.n; i++ )
    {
        const RowEntrySpec & e = spec.entries[i];
        const int64_t entry = b.open_len();
        const int32_t key_bytes = e.key_bytes >= 0 ? e.key_bytes : (int32_t) strlen( e.key );
        b.field( "key", e.key_kind != 0 ? e.key_kind : 12 );
        b.leb( (uint64_t) key_bytes );
        for ( int32_t k = 0; k < key_bytes; k++ ) { b.u8( (uint8_t) e.key[ k < (int32_t) strlen( e.key ) ? k : (int32_t) strlen( e.key ) - 1 ] ); }
        b.field( "value", 13 );
        const int64_t value = b.open_len();
        b.field( "count", 4 );
        b.u32( (uint32_t) e.count );
        b.end();
        b.close_len( value );
        b.end();
        b.close_len( entry );
    }
    b.close_len( map_body );
    b.field( "after", 4 );
    b.u32( (uint32_t) spec.after );
    b.end();
    RowWire w;
    w.size = b.finish( w.bytes );
    return w;
}

static RowWire good_row() { return build_row( ascending_spec() ); }

struct Verdict
{
    int32_t count;
    int32_t unknown, kind_mismatch, clamped, duplicate;
    bool malformed;
    int32_t after;
    int32_t decoded[3];
};

static Verdict read_row( const RowWire & w )
{
    Verdict v = { -1, 0, 0, 0, 0, false, 0, { 0, 0, 0 } };
    const int64_t need = RowLoadMeasure( w.bytes, w.size );
    if ( need < 0 ) { v.count = -2; return v; } // the measure REFUSED the framing
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return v; }
    TableReport r;
    const Row * row = RowLoad( region, need, w.bytes, w.size, &r );
    if ( row != NULL )
    {
        v.count = row->entries.size();
        v.after = row->after;
        int i = 0;
        for ( auto [ key, item ] : row->entries )
        {
            (void) key;
            if ( i < 3 ) { v.decoded[i] = item->count; }
            i++;
        }
    }
    v.unknown = r.unknown;
    v.kind_mismatch = r.kind_mismatch;
    v.clamped = r.clamped;
    v.duplicate = r.duplicate;
    v.malformed = r.malformed;
    free( region );

    // EVERY LOAD PATH PRODUCES ONE REPORT (§2.8): the region load of §6.5 and
    // LoadBuilder never disagree about a wire, which is what lets one set of
    // counters be the expectation whichever path read it.
    RowBuilder into;
    TableReport t;
    RowLoadBuilder( into, w.bytes, w.size, &t );
    CHECK_EQ( t.unknown, v.unknown );
    CHECK_EQ( t.kind_mismatch, v.kind_mismatch );
    CHECK_EQ( t.clamped, v.clamped );
    CHECK_EQ( t.duplicate, v.duplicate );
    CHECK_EQ( (int) t.malformed, (int) v.malformed );
    CHECK_EQ( into.GetRoot()->entries.count, v.count < 0 ? 0 : v.count );
    return v;
}

static void test_reader()
{
    {
        const Verdict v = read_row( good_row() );
        CHECK_EQ( v.count, 3 );
        CHECK_EQ( v.after, 777 );
        CHECK_EQ( v.unknown + v.kind_mismatch + v.clamped + v.duplicate + (int) v.malformed, 0 );
    }
    {
        // CONTROL: the reader's ascending check is dropped. A SHUFFLED map
        // meets it, and the row's `malformed` flag goes red.
        RowSpec spec = ascending_spec();
        const RowEntrySpec hold = spec.entries[0];
        spec.entries[0] = spec.entries[2];
        spec.entries[2] = hold;
        const Verdict v = read_row( build_row( spec ) );
        CHECK( v.malformed );
        CHECK_EQ( v.count, 1 ); // the ascending prefix it has
    }
    {
        // CONTROL: the parent STOPS at a descending key instead of reading on.
        // The holder carries `after` past the map, and its decoded value goes
        // red — that is §4's framing-damage rule, at the map.
        RowSpec spec = ascending_spec();
        spec.entries[2] = spec.entries[0]; // `aa` again, behind `bb`
        const Verdict v = read_row( build_row( spec ) );
        CHECK( v.malformed );
        CHECK_EQ( v.count, 2 );
        CHECK_EQ( v.after, 777 ); // the PARENT read on past the field's length
    }
    {
        // CONTROL: the duplicate rule is dropped, first wins or both kept. The
        // repeat ELIDES a field the first occurrence set, so a reader that
        // overlays instead of resetting reads 11 where the rule reads 0 — and
        // it agrees with the rule on every other body.
        RowSpec spec = ascending_spec();
        spec.entries[2] = spec.entries[1]; // a second `bb`
        spec.entries[2].count = 0;         // at its default, so the field elides
        const Verdict v = read_row( build_row( spec ) );
        CHECK( !v.malformed );
        CHECK_EQ( v.count, 2 );      // the map's count EXCLUDES the repeat
        CHECK_EQ( v.duplicate, 1 );
        CHECK_EQ( v.decoded[1], 0 ); // LAST WINS WHOLE
        CHECK_EQ( v.after, 777 );
    }
    {
        // CONTROL: the reader CLAMPS a key instead of dropping its entry. The
        // DECODED VALUE is the half that says it — the `clamped` count alone
        // cannot separate a merged entry from a dropped one. KEYS NEVER CLAMP.
        RowSpec spec = ascending_spec();
        spec.entries[1].key_bytes = 14; // past string(8)
        const Verdict v = read_row( build_row( spec ) );
        CHECK( !v.malformed );
        CHECK_EQ( v.count, 2 );
        CHECK_EQ( v.clamped, 1 );     // one count per entry
        CHECK_EQ( v.decoded[0], 10 ); // `aa`
        CHECK_EQ( v.decoded[1], 12 ); // `cc`: the skipped entry neither reordered nor collided
        CHECK_EQ( v.after, 777 );
    }
    {
        // CONTROL: an ELEMENT KIND that is not 13 is §4's ordinary array kind
        // mismatch, and nothing about a map is special-cased.
        RowSpec spec = ascending_spec();
        spec.element_kind = 12;
        const Verdict v = read_row( build_row( spec ) );
        CHECK_EQ( v.count, 0 );
        CHECK_EQ( v.kind_mismatch, 1 );
        CHECK( !v.malformed );
        CHECK_EQ( v.after, 777 );
    }
    {
        // An `N` past the int32 cap, in a row of a few dozen bytes: LoadMeasure
        // refuses from the framing alone and read_row never reaches Load
        // (§6.5). The cap is tested before the map's `L`, so this row cannot
        // see the `L` check, and test_measure_refusals asserts both refusals
        // by their reason.
        RowSpec spec = ascending_spec();
        spec.declared_n = 0xFFFFFFFFll;
        RowWire w = build_row( spec );
        CHECK_EQ( RowLoadMeasure( w.bytes, w.size ), -1 );
        const Verdict v = read_row( w );
        CHECK_EQ( v.count, -2 );
    }
}

// ---- the KEY KIND is the reader's declaration, never the first entry's ----

static void test_key_kind()
{
    // A row written under a CHANGED KEY KIND: `Row`'s string keys read by
    // `WideRow`'s uint32 declaration. CONTROL: the key-kind rule decodes
    // anyway — the five counters go red, because an entry would land under a
    // defaulted key where the rule says the map is EMPTY.
    RowWire w = good_row();
    const int64_t need = WideRowLoadMeasure( w.bytes, w.size );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return; }
    TableReport r;
    const WideRow * wide = WideRowLoad( region, need, w.bytes, w.size, &r );
    CHECK( wide != NULL );
    CHECK_EQ( wide->entries.size(), 0 ); // the map RESETS TO EMPTY
    CHECK_EQ( r.kind_mismatch, 1 );      // ONE for the map, never one per entry
    CHECK_EQ( r.unknown, 0 );
    CHECK_EQ( r.clamped, 0 );
    CHECK_EQ( r.duplicate, 0 );
    CHECK( !r.malformed );
    CHECK_EQ( wide->after, 777 );        // the parent reads on
    free( region );

    // CONTROL: the key-kind event is counted PER ENTRY instead of once for the
    // map. This row's SECOND entry is the first to disagree, so a per-entry
    // count would read 2 where the rule reads 1. The body is built from the
    // grammar with a uint32 key on entries one and three and a STRING key on
    // entry two.
    static uint8_t mixed[4096];
    int64_t size = 0;
    {
        WireBuilder b;
        b.field( "entries", 14 );
        const int64_t map_body = b.open_len();
        b.u8( 13 );
        b.leb( 3 );
        // KEYS FROM ONE, not zero: a key at its default elides under §3's rule
        for ( uint32_t i = 1; i <= 3; i++ )
        {
            const int64_t entry = b.open_len();
            b.field( "key", i == 2 ? 12 : 8 ); // kind 12 is an opaque byte payload (§3)
            if ( i == 2 ) { b.leb( 4 ); b.u32( i ); } else { b.u32( i ); }
            b.field( "value", 13 );
            const int64_t value = b.open_len();
            b.field( "count", 4 );
            b.u32( 10 + i );
            b.end();
            b.close_len( value );
            b.end();
            b.close_len( entry );
        }
        b.close_len( map_body );
        b.field( "after", 4 );
        b.u32( 555 );
        b.end();
        size = b.finish( mixed );
    }
    const int64_t need2 = WideRowLoadMeasure( mixed, size );
    uint8_t * region2 = (uint8_t *) MEASURED_CALLOC( need2, 0 );
    if ( region2 == NULL ) { return; }
    TableReport r2;
    const WideRow * mixed_row = WideRowLoad( region2, need2, mixed, size, &r2 );
    CHECK( mixed_row != NULL );
    CHECK_EQ( mixed_row->entries.size(), 0 ); // a map with half its keys is not a map
    CHECK_EQ( r2.kind_mismatch, 1 );          // ONE, at the first entry that disagrees
    CHECK_EQ( mixed_row->after, 555 ); // the parent reads on past the map's L
    free( region2 );
}

// ---- LoadMeasure over a MAP OF MAPS (docs/SPEC-TABLES.md §2.8, §6.5) ----

static void test_load_measure_depth()
{
    // CONTROL: `LoadMeasure`'s term is summed at ONE DEPTH only. The instance's
    // value is itself a map, so the measure goes red against the region Load
    // fills.
    FleetBuilder b;
    Fleet * fleet = b.GetRoot();
    for ( int i = 0; i < 4; i++ )
    {
        char key[8];
        snprintf( key, sizeof( key ), "kit%d", i );
        TableMap<FleetLoadoutsEntryValueEntry> * kit = FleetLoadoutsInsert( b.main, fleet->loadouts, key );
        CHECK( kit != NULL );
        for ( uint8_t slot = 0; slot < 5; slot++ )
        {
            FleetLoadoutsEntryValueInsert( b.main, *kit, slot )->count = 100 * i + slot;
        }
    }
    static uint8_t wire[1u << 16];
    const int64_t n = FleetSave( b, wire, sizeof( wire ) );
    const int64_t need = FleetLoadMeasure( wire, n );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return; }
    TableReport r;
    const Fleet * loaded = FleetLoad( region, need, wire, n, &r );
    CHECK( loaded != NULL );
    CHECK( !r.malformed );
    CHECK_EQ( loaded->loadouts.size(), 4 );
    for ( int i = 0; i < 4; i++ )
    {
        char key[8];
        snprintf( key, sizeof( key ), "kit%d", i );
        const TableMap<FleetLoadoutsEntryValueEntry> * kit = loaded->loadouts.Find( key );
        CHECK( kit != NULL );
        if ( kit == NULL ) { continue; }
        CHECK_EQ( kit->size(), 5 );
        for ( uint8_t slot = 0; slot < 5; slot++ )
        {
            const Item * item = kit->Find( slot );
            CHECK( item != NULL );
            if ( item != NULL ) { CHECK_EQ( item->count, 100 * i + slot ); }
        }
    }
    // A REGION EXACTLY ONE BYTE SHORT IS REFUSED, which is what says the
    // measure is the measure and not an over-estimate with slack to spare
    TableReport short_report;
    CHECK( FleetLoad( region, need - 1, wire, n, &short_report ) == NULL );
    free( region );
}

// ---- the LoadMeasure REFUSALS at a map (docs/SPEC-TABLES.md §2.8, §6.5) ----
//
// A unit test and not a `report` row, because a refusal produces no counters.
// Each wire is built in memory with a SYNTHETIC map count rather than a
// golden: a count above the int32 cap, which no golden could carry because
// the file would be two gigabytes, a count whose entries cannot fit the map's
// own L, and a clean wire beside them, which must measure and load. The
// REASON is asserted by name, because the cap is tested before the L: a count
// past both answers count_over_extent_cap, and only a count under the cap
// reaches the L check, so a dropped L check goes red on the 100000 row and
// nowhere a count past the cap is used.

struct Wire
{
    uint8_t bytes[4096];
    int64_t size;
};

// a `Fleet` body written FROM THE GRAMMAR: its `ships` map as a kind 14 array
// of kind 13 `{ key, value }` entries (§2.8), with the declared count a knob
// of its own and the entries actually written a second one
static Wire build_fleet_wire( uint64_t ships, int32_t real_ships )
{
    WireBuilder b;
    b.field( "ships", 14 );
    const int64_t body = b.open_len();
    b.u8( 13 );
    b.leb( ships );
    for ( int32_t i = 0; i < real_ships; i++ )
    {
        const int64_t entry = b.open_len();
        b.field( "key", 12 );
        b.leb( 1 );
        b.u8( (uint8_t) ( 'a' + i ) );
        b.field( "value", 13 );
        const int64_t ship = b.open_len();
        b.field( "health", 4 );
        b.u32( (uint32_t) ( 100 + i ) );
        b.end();
        b.close_len( ship );
        b.end();
        b.close_len( entry );
    }
    b.close_len( body );
    b.end();
    Wire w;
    w.size = b.finish( w.bytes );
    return w;
}

static void report_silent( const TableReport & r, const char * where )
{
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 || r.malformed )
    {
        printf( "FAIL %s: the report is not silent (unknown %d, kind_mismatch %d, clamped %d, duplicate %d, malformed %d)\n",
                where, r.unknown, r.kind_mismatch, r.clamped, r.duplicate, (int) r.malformed );
        failures++;
    }
}

static void test_measure_refusals()
{
    // a count above the int32 cap: the cap answers first
    {
        Wire w = build_fleet_wire( 0x80000000ull, 1 );
        TableRefuseReason reason = count_over_length;
        CHECK_EQ( FleetLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_extent_cap );
    }
    // a count under the cap that the map's L cannot carry: the L answers
    {
        Wire w = build_fleet_wire( 100000, 1 );
        TableRefuseReason reason = count_over_extent_cap;
        CHECK_EQ( FleetLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_length );
    }
    // and a clean wire beside them, which must measure and load silently
    {
        Wire w = build_fleet_wire( 2, 2 );
        TableRefuseReason reason = count_over_length;
        const int64_t need = FleetLoadMeasure( w.bytes, w.size, NULL, &reason );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Fleet * fleet = FleetLoad( region, need, w.bytes, w.size, &r );
        CHECK( fleet != NULL );
        report_silent( r, "the clean wire beside the refusals" );
        if ( fleet != NULL )
        {
            CHECK_EQ( fleet->ships.size(), 2 );
            const ShipConfig * ship = fleet->ships.Find( "b" );
            CHECK( ship != NULL );
            if ( ship != NULL ) { CHECK_EQ( ship->health, 101 ); }
        }
        free( region );
    }
}

// ---- the TEXT form (docs/SPEC-TABLES.md §2.8, §16) ----

static void test_text_allocation_refusals();

static void test_text()
{
    FleetBuilder b;
    build_fleet( b, true );
    CHECK( b.Lock() );
    const Fleet * locked = b.AsConst();

    const int64_t need = FleetToJsonMeasure( locked );
    CHECK( need > 0 );
    char * text = (char *) MEASURED_CALLOC( need, 1 );
    if ( text == NULL ) { return; }
    const int64_t written = FleetToJson( locked, text, need );
    CHECK_EQ( written, need );

    // A PLAIN JSON OBJECT KEYED BY THE KEY, in ASCENDING key order, with an
    // integer key quoted — so `unpack` then `pack` is byte-stable and a diff of
    // two texts is a diff of two maps.
    CHECK( strstr( text, "\"bomber\"" ) != NULL );
    CHECK( strstr( text, "\"fighter\"" ) != NULL );
    CHECK( strstr( text, "\"7\"" ) != NULL );   // an integer key, quoted
    CHECK( strstr( text, "\"12\"" ) != NULL );
    CHECK( strstr( text, "\"-3\"" ) != NULL );  // and a SIGNED one
    const char * bomber = strstr( text, "\"bomber\"" );
    const char * fighter = strstr( text, "\"fighter\"" );
    CHECK( bomber != NULL && fighter != NULL && bomber < fighter ); // ASCENDING

    // and the text reads back: one instance, one text, both ways
    FleetBuilder into;
    TableReport report;
    CHECK( FleetFromJson( into, text, written, &report ) );
    CHECK_EQ( report.unknown, 0 );
    CHECK_EQ( report.kind_mismatch, 0 );
    CHECK_EQ( report.clamped, 0 );
    CHECK_EQ( report.duplicate, 0 );
    CHECK( !report.malformed );
    CHECK_EQ( into.GetRoot()->ships.count, 3 );
    CHECK_EQ( into.GetRoot()->by_id.count, 2 );
    CHECK_EQ( into.GetRoot()->tiers.count, 2 );

    // the ROUND TRIP is byte-stable: the same instance, the same text
    CHECK( into.Lock() );
    const int64_t again_bytes = FleetToJsonMeasure( into.AsConst() );
    char * again = (char *) MEASURED_CALLOC( again_bytes, 1 );
    if ( again == NULL ) { free( text ); return; }
    CHECK_EQ( FleetToJson( into.AsConst(), again, again_bytes ), again_bytes );
    CHECK_EQ( again_bytes, written );
    CHECK( memcmp( again, text, (size_t) written ) == 0 );
    // and so is the WIRE, which is what says the text carried the map whole
    static uint8_t from_text[1u << 16];
    CHECK_EQ( FleetSave( into.AsConst(), from_text, sizeof( from_text ) ), bytes_full );
    CHECK( memcmp( from_text, wire_full, (size_t) bytes_full ) == 0 );
    free( again );
    free( text );

    // A REPEATED KEY is LAST-WINS and counted duplicate, the object rule
    // applied inside the map; an EMPTY OBJECT is an empty map; a key past the
    // bound drops its entry and counts clamped; a key outside the key kind's
    // range is kind_mismatch for that entry, dropped and never clamped.
    struct Row { const char * text; int32_t count; int32_t dup; int32_t clamp; int32_t mismatch; };
    const Row rows[] = {
        { "{\"entries\":{},\"after\":5}", 0, 0, 0, 0 },
        { "{\"entries\":{\"aa\":{\"count\":1},\"aa\":{\"count\":2}},\"after\":5}", 1, 1, 0, 0 },
        { "{\"entries\":{\"aaaaaaaaaaaa\":{\"count\":1}},\"after\":5}", 0, 0, 1, 0 },
    };
    for ( int i = 0; i < 3; i++ )
    {
        RowBuilder rb;
        TableReport r;
        CHECK( RowFromJson( rb, rows[i].text, (int64_t) strlen( rows[i].text ), &r ) );
        CHECK_EQ( rb.GetRoot()->entries.count, rows[i].count );
        CHECK_EQ( r.duplicate, rows[i].dup );
        CHECK_EQ( r.clamped, rows[i].clamp );
        CHECK_EQ( r.kind_mismatch, rows[i].mismatch );
        CHECK_EQ( rb.GetRoot()->after, 5 );
    }
    {
        // the duplicate's value is the LAST one, whole
        const char * t = "{\"entries\":{\"aa\":{\"count\":1},\"aa\":{}}}";
        RowBuilder rb;
        TableReport r;
        CHECK( RowFromJson( rb, t, (int64_t) strlen( t ), &r ) );
        CHECK_EQ( rb.GetRoot()->entries.count, 1 );
        CHECK_EQ( RowEntriesFind( rb.arena, rb.GetRoot()->entries, "aa" )->count, 0 );
    }
    {
        // AN INTEGER KEY IS READ BY §16.2's RULE AND BY NOTHING ELSE: "2.0" is
        // the integer 2 and "1e3" is 1000, and a value outside the kind's
        // range is kind_mismatch for that entry, dropped and never clamped.
        const char * t = "{\"entries\":{\"2.0\":{\"count\":7},\"1e3\":{\"count\":8},\"99999999999\":{\"count\":9}}}";
        WideRowBuilder rb;
        TableReport r;
        CHECK( WideRowFromJson( rb, t, (int64_t) strlen( t ), &r ) );
        CHECK_EQ( rb.GetRoot()->entries.count, 2 );
        CHECK_EQ( r.kind_mismatch, 1 );
        CHECK_EQ( r.clamped, 0 );
        CHECK( WideRowEntriesFind( rb.arena, rb.GetRoot()->entries, (uint32_t) 2 ) != NULL );
        CHECK( WideRowEntriesFind( rb.arena, rb.GetRoot()->entries, (uint32_t) 1000 ) != NULL );
    }
    {
        // a MALFORMED key stops the read where §16.1's rule stops it
        const char * t = "{\"entries\":{\"nope\":{\"count\":1}}}";
        WideRowBuilder rb;
        TableReport r;
        CHECK( !WideRowFromJson( rb, t, (int64_t) strlen( t ), &r ) );
        CHECK( r.malformed );
    }
    {
        // A KEY IS DATA AND A LENGTH, never a run to the first NUL (§2.8, §3):
        // the wire and the text both carry an interior U+0000 in a string(N)
        // key, so "a" and "a\0b" are TWO keys. A lookup that recomputes the
        // length merges them and relabels the entry it found, which is a
        // deletion the report never mentions.
        const char * t = "{\"entries\":{\"a\":{\"count\":1},\"a\\u0000b\":{\"count\":2}}}";
        RowBuilder rb;
        TableReport r;
        CHECK( RowFromJson( rb, t, (int64_t) strlen( t ), &r ) );
        CHECK_EQ( rb.GetRoot()->entries.count, 2 );
        CHECK_EQ( r.duplicate, 0 );
        CHECK_EQ( r.clamped, 0 );
    }
    {
        // and the SAME interior-zero key twice IS one key: one entry, counted
        // duplicate, never a second entry wearing an identity the map holds
        const char * t = "{\"entries\":{\"a\\u0000b\":{\"count\":1},\"a\\u0000b\":{\"count\":2}}}";
        RowBuilder rb;
        TableReport r;
        CHECK( RowFromJson( rb, t, (int64_t) strlen( t ), &r ) );
        CHECK_EQ( rb.GetRoot()->entries.count, 1 );
        CHECK_EQ( r.duplicate, 1 );
    }
    {
        // A KEY THE WALKER CANNOT HOLD WHOLE MUST NOT BECOME A DIFFERENT KEY
        // (§2.8). The text walker scans a key into a fixed buffer, so a key
        // longer than that buffer is truncated there, and two 256-byte keys
        // that share 255 bytes then merge into ONE entry keyed by a prefix the
        // text never spelled, while `clamped` -- which the scan counts either
        // way -- says nothing about it. `names` is string(300), so the
        // DECLARED bound holds both keys and only the walker's does not.
        //
        // What the page states is the IDENTITY, so that is what this pins:
        // every entry the map holds is a key the text spelled. Whether a key
        // past the walker's buffer but inside the declared bound should be
        // DROPPED or held WHOLE is the page's to say, and this row is green
        // under either answer.
        char first[257];
        char second[257];
        memset( first, 'a', 255 );
        first[255] = 'b';
        first[256] = 0;
        memcpy( second, first, sizeof( first ) );
        second[255] = 'c';
        char both[1024];
        snprintf( both, sizeof( both ), "{\"names\":{\"%s\":{\"count\":1},\"%s\":{\"count\":2}},\"after\":5}",
                  first, second );
        EdgeRowBuilder eb;
        TableReport r;
        CHECK( EdgeRowFromJson( eb, both, (int64_t) strlen( both ), &r ) );
        CHECK_EQ( r.duplicate, 0 );
        for ( auto [ key, item ] : EdgeRowNamesEach( eb.arena, eb.GetRoot()->names ) )
        {
            (void) item;
            if ( strcmp( key, first ) != 0 && strcmp( key, second ) != 0 )
            {
                printf( "FAIL %s:%d: the map holds a key the text never spelled, %d bytes long\n",
                        __FILE__, __LINE__, (int) strlen( key ) );
                failures++;
            }
        }
        CHECK_EQ( eb.GetRoot()->after, 5 ); // and the parent read on
    }
    {
        // A uint64 KEY is read by §16.2's integer rule and by nothing else, and
        // its domain is established BEFORE any cast: 2.0 is 2 and 1e19 is a
        // magnitude the kind holds, while -1 and 1e30 are outside it and drop
        // their entries as kind_mismatch. A key is never clamped.
        const char * t = "{\"ids\":{\"2.0\":{\"count\":1},\"1e19\":{\"count\":2},"
                         "\"18446744073709551615\":{\"count\":3},\"-1\":{\"count\":4},\"1e30\":{\"count\":5}}}";
        EdgeRowBuilder eb;
        TableReport r;
        CHECK( EdgeRowFromJson( eb, t, (int64_t) strlen( t ), &r ) );
        CHECK_EQ( eb.GetRoot()->ids.count, 3 );
        CHECK_EQ( r.kind_mismatch, 2 );
        CHECK_EQ( r.clamped, 0 );
        CHECK( EdgeRowIdsFind( eb.arena, eb.GetRoot()->ids, (uint64_t) 2 ) != NULL );
        CHECK( EdgeRowIdsFind( eb.arena, eb.GetRoot()->ids, 10000000000000000000ull ) != NULL );
        CHECK( EdgeRowIdsFind( eb.arena, eb.GetRoot()->ids, 18446744073709551615ull ) != NULL );
    }
    {
        // A KEY IS THE INTEGER'S SPELLING AND NOTHING AROUND IT (§16.2). The
        // integer rule reads RFC 8259's number grammar, which has no room in it
        // for whitespace, so a padded spelling is not a JSON number at all: it
        // is malformed on the same terms as "1-2", and the read stops there
        // with the instance holding what was placed before the stop (§16.1).
        //
        // A walker that steps over the padding on its way in hands the digit
        // path a byte that is not a digit, and what comes back is a key of its
        // own making. This text spells 2402 once and a padded 2 beside it, and
        // the two are never one entry.
        const char * t = "{\"ids\":{\"2402\":{\"count\":7},\" 2\":{\"count\":1}}}";
        EdgeRowBuilder eb;
        TableReport r;
        CHECK( !EdgeRowFromJson( eb, t, (int64_t) strlen( t ), &r ) );
        CHECK( r.malformed );
        CHECK_EQ( r.duplicate, 0 );
        for ( auto [ key, item ] : EdgeRowIdsEach( eb.arena, eb.GetRoot()->ids ) )
        {
            (void) item;
            CHECK_EQ( key, 2402 );
        }
    }
    {
        // AN INTEGER KEY SPELLED WITH A DECIMAL POINT IS PARSED AS AN INTEGER,
        // never through a double (§16.2). A double carries 53 bits of mantissa,
        // so it cannot tell 9007199254740993 from its neighbour, and a key read
        // through one lands on the neighbour's identity: two keys the text
        // spells separately become one entry.
        const char * t = "{\"ids\":{\"9007199254740992\":{\"count\":1},"
                         "\"9007199254740993.0\":{\"count\":2}}}";
        EdgeRowBuilder eb;
        TableReport r;
        CHECK( EdgeRowFromJson( eb, t, (int64_t) strlen( t ), &r ) );
        CHECK_EQ( eb.GetRoot()->ids.count, 2 );
        CHECK_EQ( r.duplicate, 0 );
        CHECK_EQ( r.kind_mismatch, 0 );
        CHECK_EQ( r.clamped, 0 );
        CHECK( EdgeRowIdsFind( eb.arena, eb.GetRoot()->ids, 9007199254740992ull ) != NULL );
        CHECK( EdgeRowIdsFind( eb.arena, eb.GetRoot()->ids, 9007199254740993ull ) != NULL );
    }
    {
        // THE SAME VALUE SPELLED TWO WAYS IS THE SAME KEY (§16.2): 2 and 2.0
        // are the integer 2 wherever an integer is read, and that holds at the
        // top of the uint64 domain as well as at the bottom. Read through a
        // double, 18446744073709551615.0 rounds UP to 2^64 and is dropped as
        // outside the kind, where the digits alone are exactly the key the kind
        // holds.
        const char * t = "{\"ids\":{\"18446744073709551615\":{\"count\":1},"
                         "\"18446744073709551615.0\":{\"count\":2}}}";
        EdgeRowBuilder eb;
        TableReport r;
        CHECK( EdgeRowFromJson( eb, t, (int64_t) strlen( t ), &r ) );
        CHECK_EQ( eb.GetRoot()->ids.count, 1 );
        CHECK_EQ( r.kind_mismatch, 0 );
        CHECK_EQ( r.clamped, 0 );
        CHECK_EQ( r.duplicate, 1 ); // the two spellings are one key
        const Item * item = EdgeRowIdsFind( eb.arena, eb.GetRoot()->ids, 18446744073709551615ull );
        CHECK( item != NULL );
        if ( item != NULL ) { CHECK_EQ( item->count, 2 ); } // last wins
    }
    {
        // AN INTEGER KEY PAST THE SCAN'S BUFFER DROPS AS kind_mismatch, in this
        // walker and in the tool's (#609). The bytes a scan keeps are a PREFIX,
        // and a prefix is a different token, so the entry drops rather than a
        // truncation being read as a value.
        //
        // The length alone does not settle it, which is what this row holds:
        // the key below is 256 bytes and spells the integer 1, a value the kind
        // holds, and the prefix that fits the buffer spells 1 as well. A read
        // that truncated would merge it into the entry beside it and call two
        // keys one. The map keeps one entry, and it is the one the text spells
        // as 1.
        char t[512];
        int n = snprintf( t, sizeof( t ), "{\"ids\":{\"1\":{\"count\":7},\"1e" );
        for ( int i = 0; i < 254; i++ ) { t[n++] = '0'; }
        n += snprintf( t + n, sizeof( t ) - (size_t) n, "\":{\"count\":9}}}" );
        EdgeRowBuilder eb;
        TableReport r;
        CHECK( EdgeRowFromJson( eb, t, (int64_t) n, &r ) );
        CHECK_EQ( eb.GetRoot()->ids.count, 1 );
        CHECK_EQ( r.kind_mismatch, 1 );
        CHECK_EQ( r.duplicate, 0 );
        const Item * item = EdgeRowIdsFind( eb.arena, eb.GetRoot()->ids, 1ull );
        CHECK( item != NULL );
        if ( item != NULL ) { CHECK_EQ( item->count, 7 ); }
    }
    test_text_allocation_refusals();
}

// ---- AN ALLOCATION FAILURE IS NOT AN OVERSIZED KEY (§2.8, §16.1) ----
//
// The arena refusing is the READ failing, the way the neighboring list, blob
// and pointer paths fail on one. Reporting it as `clamped` says the key did not
// fit a bound it fits perfectly, hands back an instance missing entries the
// text spelled, and calls the read a SUCCESS.
//
// The arena allocates one SEGMENT at a time, so a text has to cross a segment
// boundary before the allocator is asked anything at all. Reaching that
// boundary through the map alone would need tens of thousands of entries and an
// insert scan quadratic in them, so the arena is filled through the builder's
// own Alloc first, which is a constant-time call, and the READ is left the
// last stretch to cross. How many nodes that takes is MEASURED here rather than
// written down, so the row cannot rot when a node's storage or the segment's
// size moves.

struct TextBudget
{
    int64_t seen;
    int64_t refuse_from; // 0 = never refuse
};

static void * text_budget_alloc( void * context, int64_t bytes )
{
    TextBudget * budget = (TextBudget *) context;
    budget->seen++;
    if ( budget->refuse_from > 0 && budget->seen >= budget->refuse_from ) { return NULL; }
    return calloc( 1, (size_t) bytes );
}

static void text_budget_free( void * context, void * pointer ) { (void) context; free( pointer ); }

// one `loadouts` object of `count` entries, each an inner map of one entry
static void build_loadouts_text( char * text, int64_t capacity, int32_t count, int64_t & length )
{
    int64_t at = 0;
    at += snprintf( text + at, (size_t) ( capacity - at ), "{\"loadouts\":{" );
    for ( int32_t i = 0; i < count; i++ )
    {
        at += snprintf( text + at, (size_t) ( capacity - at ), "%s\"k%07d\":{\"1\":{\"count\":%d}}",
                        i > 0 ? "," : "", i, i );
    }
    at += snprintf( text + at, (size_t) ( capacity - at ), "}}" );
    length = at;
}

// the nodes one arena segment holds, MEASURED: allocate until the allocator is
// asked for a second segment, and answer with what the first one took
static int64_t nodes_per_segment()
{
    TextBudget probe = { 0, 0 };
    TableAllocator counting;
    counting.alloc = text_budget_alloc;
    counting.free = text_budget_free;
    counting.context = &probe;
    FleetBuilder fb( counting );
    if ( fb.GetRoot() == NULL ) { return 0; }
    const int64_t first = probe.seen;
    int64_t nodes = 0;
    while ( probe.seen == first && nodes < ( 1 << 24 ) )
    {
        if ( fb.Alloc<ShipConfig>().null() ) { return 0; }
        nodes++;
    }
    return probe.seen > first ? nodes : 0;
}

static void test_text_allocation_refusals()
{
    const int64_t fill = nodes_per_segment();
    CHECK( fill > 4096 ); // a segment holds far more than the headroom left below
    if ( fill <= 4096 ) { return; }

    // 4096 entries, each an inner map of one, is more arena than the 2048 nodes
    // of headroom left below, so the READ is what crosses into the segment the
    // allocator refuses, whatever the entry's storage happens to be
    const int64_t capacity = 1024 * 1024;
    char * text = (char *) malloc( (size_t) capacity );
    CHECK( text != NULL );
    if ( text == NULL ) { return; }
    int64_t length = 0;
    build_loadouts_text( text, capacity, 4096, length );

    TextBudget budget = { 0, 0 };
    TableAllocator pair;
    pair.alloc = text_budget_alloc;
    pair.free = text_budget_free;
    pair.context = &budget;
    FleetBuilder fb( pair );
    CHECK( fb.GetRoot() != NULL );
    for ( int64_t i = 0; i < fill - 2048; i++ ) { CHECK( !fb.Alloc<ShipConfig>().null() ); }
    const int64_t before = budget.seen;
    budget.refuse_from = before + 1; // the arena can carve nothing more

    TableReport r;
    if ( FleetFromJson( fb, text, length, &r ) )
    {
        printf( "FAIL %s:%d: the arena refused and the read still called itself clean"
                " (clamped=%d, entries=%d)\n",
                __FILE__, __LINE__, r.clamped, fb.GetRoot()->loadouts.count );
        failures++;
    }
    else
    {
        CHECK( r.malformed );
        CHECK_EQ( r.clamped, 0 ); // an arena refusal is not a key over its bound
    }
    CHECK( budget.seen > before ); // the read did reach the allocator, or this proves nothing
    free( text );
}

// ---- WHERE ELSE A MAP RIDES IN A HOLDER'S EXTENT (§2.8) ----
//
// One array per map reachable BY VALUE from the record, in depth-first field
// order — which reaches a nested table, an array of them, an enum-keyed array
// of them and a union arm. Each is a different framing the load's extent scan
// has to walk, and each is a place the measure can under-count.

static void test_depth()
{
    DepthBuilder b;
    Depth * d = b.GetRoot();
    SquadRosterInsert( b.main, d->one.roster, (uint8_t) 3 )->count = 33;
    d->many_count = 2;
    SquadRosterInsert( b.main, d->many[0].roster, (uint8_t) 1 )->count = 11;
    SquadRosterInsert( b.main, d->many[1].roster, (uint8_t) 2 )->count = 22;
    SquadRosterInsert( b.main, d->keyed[Slot::Alpha].roster, (uint8_t) 5 )->count = 55;
    // AN ARM IS ESTABLISHED AT SELECTION (§2.6): the tag alone is the value's
    // identity, and the payload is the caller's to reset before it is filled
    d->arm.type = ForceType::Squad;
    SquadReset( d->arm.squad );
    SquadRosterInsert( b.main, d->arm.squad.roster, (uint8_t) 7 )->count = 77;
    d->after = 9;

    const int64_t measured = DepthMeasure( b );
    static uint8_t wire[1u << 16];
    const int64_t n = DepthSave( b, wire, sizeof( wire ) );
    CHECK_EQ( measured, n );
    pin_golden( "map_depth", wire, n );

    const int64_t need = DepthLoadMeasure( wire, n );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return; }
    TableReport report;
    const Depth * loaded = DepthLoad( region, need, wire, n, &report );
    CHECK( loaded != NULL );
    CHECK( !report.malformed );
    if ( loaded != NULL )
    {
        const Item * one = loaded->one.roster.Find( (uint8_t) 3 );
        CHECK( one != NULL && one->count == 33 );
        CHECK_EQ( loaded->many_count, 2 );
        if ( loaded->many_count == 2 )
        {
            const Item * first = loaded->many[0].roster.Find( (uint8_t) 1 );
            const Item * second = loaded->many[1].roster.Find( (uint8_t) 2 );
            CHECK( first != NULL && first->count == 11 );
            CHECK( second != NULL && second->count == 22 );
        }
        const Item * slot = loaded->keyed[Slot::Alpha].roster.Find( (uint8_t) 5 );
        CHECK( slot != NULL && slot->count == 55 );
        CHECK_EQ( loaded->keyed[Slot::Beta].roster.size(), 0 );
        CHECK( loaded->arm.type == ForceType::Squad );
        if ( loaded->arm.type == ForceType::Squad )
        {
            const Item * armed = loaded->arm.squad.roster.Find( (uint8_t) 7 );
            CHECK( armed != NULL && armed->count == 77 );
        }
        CHECK_EQ( loaded->after, 9 ); // and the parent read past all of them

        // the region is EXACT: one byte short is refused, so the extent scan
        // counted every one of those four reaches and not one twice
        static uint8_t again[1u << 16];
        CHECK_EQ( DepthSave( loaded, again, sizeof( again ) ), n );
        CHECK( memcmp( again, wire, (size_t) n ) == 0 );
        TableReport short_report;
        CHECK( DepthLoad( region, need - 1, wire, n, &short_report ) == NULL );
    }
    free( region );

    // AN UNREACHED NON-EMPTY MAP SLOT IS REFUSED by Cook and by Lock, the same
    // refusal §7.6 gives a pointer in that position: a counted array's slots
    // past its live count are storage the walk does not reach, so a non-empty
    // map in one names entries the region will not hold and the write answers
    // false with nothing partial written. The WIRE is not refused — a counted
    // array rides its live slots and the unreached one simply does not ride.
    {
        DepthBuilder past;
        Depth * p = past.GetRoot();
        p->many_count = 1;
        SquadRosterInsert( past.main, p->many[0].roster, (uint8_t) 1 )->count = 11;
        SquadRosterInsert( past.main, p->many[2].roster, (uint8_t) 9 )->count = 99; // past the count
        static uint8_t rides[1u << 16];
        const int64_t measured_past = DepthMeasure( past );
        CHECK_EQ( DepthSave( past, rides, sizeof( rides ) ), measured_past );
        CHECK( !past.Lock() );
        CHECK( past.AsConst() == NULL ); // nothing partial
    }

    // and the TOOL's path reads the same four reaches into a builder
    DepthBuilder into;
    TableReport tool;
    CHECK( DepthLoadBuilder( into, wire, n, &tool ) );
    CHECK_EQ( (int) tool.malformed, (int) report.malformed );
    CHECK_EQ( into.GetRoot()->one.roster.count, 1 );
    CHECK_EQ( into.GetRoot()->many_count, 2 );
    CHECK_EQ( into.GetRoot()->keyed[Slot::Alpha].roster.count, 1 );
    CHECK_EQ( into.GetRoot()->arm.squad.roster.count, 1 );
    static uint8_t relocked[1u << 16];
    CHECK_EQ( DepthSave( into, relocked, sizeof( relocked ) ), n );
    CHECK( memcmp( relocked, wire, (size_t) n ) == 0 );
}

// ---- the MESSAGE FORM over maps (docs/SPEC-TABLES.md §2.8, §3.3) ----
//
// A map rides on the message wire as its thirty-two bit count and its entries
// in ascending key order, each a bitpacked body with no length, and the
// reader carves the entries from the node's extent read off the framing. The
// fleet's three depths, its shared node and its signed key all ride: the
// loaded region's FILE form is the builder's, byte for byte.

static void test_message_form()
{
    // THE RESOLVED VOCABULARY'S STORAGE IS THE CALLER'S (§3.3)
    static TableMessageEntry entries[ kTableMessageEntriesHere ];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    static uint8_t announcement[16384];
    const int64_t announced = Announce( announcement, sizeof( announcement ) );
    CHECK( announced == AnnounceMeasure() );
    CHECK( AnnounceRead( vocabulary, announcement, announced, NULL ) );
    pin_golden( "map_conn", announcement, announced );

    FleetBuilder b;
    build_fleet( b, false );
    CHECK( b.Lock() );
    const Fleet * roots[1] = { b.AsConst() };
    static uint8_t message[1u << 16];
    TableReport report;
    const int64_t bytes = FleetSaveMessages( roots, 1, message, sizeof( message ), &report );
    CHECK( bytes > 0 && bytes == FleetMeasureMessages( roots, 1, &report ) );
    CHECK( bytes < bytes_full ); // the message form is the wire optimized for bandwidth
    pin_golden( "map_full_message", message, bytes );

    int64_t attribution = 0;
    const int64_t need = FleetLoadMeasure( vocabulary, message, bytes, &attribution );
    CHECK( need > 0 );
    static uint8_t region[1u << 16];
    const Fleet * loaded[1] = { NULL };
    int64_t count = 1;
    CHECK( FleetLoadMessages( loaded, &count, region, need, vocabulary, message, bytes, &report ) );
    CHECK( count == 1 && loaded[0] != NULL );
    CHECK( !report.malformed && !report.refused && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0 );
    if ( loaded[0] != NULL )
    {
        const Fleet * fleet = loaded[0];
        CHECK( fleet->ships.size() == 3 );
        const ShipConfig * bomber = fleet->ships.Find( "bomber" );
        CHECK( bomber != NULL && bomber->health == 400 );
        // TWO KEYS, ONE NODE, and the pointer field naming it a third time
        const ShipConfig * seven = fleet->by_id.Find( (uint32_t) 7 );
        const ShipConfig * twelve = fleet->by_id.Find( (uint32_t) 12 );
        CHECK( seven != NULL && twelve != NULL && seven == twelve && seven == ShipConfigAt( fleet->flagship ) );
        const TableMap<FleetLoadoutsEntryValueEntry> * kit = fleet->loadouts.Find( "kit" );
        CHECK( kit != NULL && kit->size() == 2 );
        const Item * nine = kit != NULL ? kit->Find( (uint8_t) 9 ) : NULL;
        CHECK( nine != NULL && nine->count == 99 );
        const Item * low = fleet->tiers.Find( (int16_t) -3 );
        CHECK( low != NULL && low->count == -3 );
        // THE LOADED REGION'S FILE FORM IS THE BUILDER'S, byte for byte
        static uint8_t again[1u << 16];
        const int64_t file_bytes = FleetSave( fleet, again, sizeof( again ) );
        CHECK( file_bytes == bytes_full && memcmp( again, wire_full, (size_t) bytes_full ) == 0 );
        // and the message re-saves to itself
        static uint8_t again_message[1u << 16];
        CHECK( FleetSaveMessages( loaded, 1, again_message, sizeof( again_message ), &report ) == bytes );
        CHECK( memcmp( again_message, message, (size_t) bytes ) == 0 );
    }
}
int main( int argc, char ** argv )
{
    if ( argc > 1 && strcmp( argv[1], "measure-refusals" ) == 0 )
    {
        test_measure_refusals();
        if ( failures != 0 )
        {
            printf( "\n%d measure refusal check(s) failed\n", failures );
            return 1;
        }
        printf( "map measure refusals: two -1s with their reasons, one clean measure, no counter moved (docs/SPEC-TABLES.md §2.8, §6.5)\n" );
        return 0;
    }
    test_writer();
    test_builder();
    test_const_forms();
    test_reader();
    test_key_kind();
    test_load_measure_depth();
    test_measure_refusals();
    test_text();
    test_depth();
    test_message_form();

    if ( failures != 0 )
    {
        printf( "\n%d map check(s) failed\n", failures );
        return 1;
    }
    printf( "maps: all checks passed (docs/SPEC-TABLES.md §2.8)\n" );
    return 0;
}
