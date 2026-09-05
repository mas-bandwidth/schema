// THE LIST GATE (docs/SPEC-TABLES.md §2.9). One binary over the `tables/lists`
// unit: the builder's three, the four writing walks in INDEX order, the node
// extent a region and a cook carry, every reader rule, the migration golden,
// and the negative controls §2.9 names. Each row here is one of them, and the
// comment says which sabotage it turns red.
//
// Compiled WITHOUT the serialize include path: the Table headers stand alone.
//
//   schema_test_lists                    every battery
//   schema_test_lists measure-refusals   the six LoadMeasure refusals alone
//                                        (make tables-list-measure-refusals)

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <new>

#include "SaveTable.h"
#include "SharedTable.h"
#include "HoldersTable.h"
#include "MigrateTable.h"
#include "ReportTable.h"
#include "wirebuilder.h"

using namespace listdemo;

static int failures = 0;

// ---- THE ALLOCATION AUDIT (docs/SPEC-TABLES.md §2.9, §6.5) ----
//
// The reading path allocates nothing of its own: LoadMeasure, Load into the
// caller's region, the const indexing and iteration, and Open. Every
// allocation the program makes through operator new is counted here, and the
// audit requires the count to stay where it was across all of them. CONTROL:
// an allocation is planted in Load or in the const indexing, and the audit
// goes red.
static long long allocations = 0;

void * operator new( size_t bytes )
{
    allocations++;
    void * p = malloc( bytes != 0 ? bytes : 1 );
    if ( p == NULL ) { abort(); }
    return p;
}
void operator delete( void * p ) noexcept { free( p ); }
void operator delete( void * p, size_t ) noexcept { free( p ); }

#define CHECK( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAIL %s:%d: %s\n", __FILE__, __LINE__, #condition );     \
            fflush( stdout );                                                 \
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
            fflush( stdout );                                                 \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

// ---- every allocation sized from a measure goes through here ----
//
// A measure is an answer from the code under test, so a region sized from one
// is the single place a broken measure reaches the allocator. The ceiling is
// CHECKED first, and a measure past it is a red CHECK on every platform rather
// than a call to calloc (test/tables/maps_main.cpp says why).
//
// 256 MiB: the largest measure this corpus produces is the 100,000-element
// clamp control's, well under a megabyte.

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

#define MEASURED_CALLOC( measure, extra )                                     \
    measured_calloc( ( measure ), ( extra ), #measure, __FILE__, __LINE__ )

// ---- the shared golden wire (docs/SPEC-TABLES.md §3) ----
//
// The C++ reference is the writer: these instances' encodings are pinned into
// testdata/wire/tables/<name>.bin. A break here under an unchanged schema is
// stop-the-line, never a quiet re-pin. SCHEMA_UPDATE_WIRE_GOLDENS=1 rewrites
// them deliberately (make update-goldens). It answers whether the bytes are
// the pinned ones, because a COOK the pin refused is a file no Open may trust:
// Open matches the header and points (§7), so a layout sabotage that reached
// it would crash rather than fail a CHECK.

static bool pin_golden( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/tables/%s.bin", name );
    if ( getenv( "SCHEMA_UPDATE_WIRE_GOLDENS" ) )
    {
        FILE * f = fopen( path, "wb" );
        if ( f == NULL ) { printf( "FAIL cannot write %s\n", path ); fflush( stdout ); failures++; return false; }
        fwrite( data, 1, (size_t) bytes, f );
        fclose( f );
        return true;
    }
    FILE * f = fopen( path, "rb" );
    if ( f == NULL )
    {
        printf( "FAIL missing table wire golden %s (run: make update-goldens)\n", path );
        fflush( stdout );
        failures++;
        return false;
    }
    static uint8_t expected[1u << 20];
    const size_t n = fread( expected, 1, sizeof( expected ), f );
    fclose( f );
    if ( (int64_t) n != bytes || memcmp( expected, data, n ) != 0 )
    {
        printf( "FAIL table wire golden %s: %lld bytes written, %lld pinned\n",
                name, (long long) bytes, (long long) n );
        fflush( stdout );
        failures++;
        return false;
    }
    return true;
}

// A COOK IS WRITTEN FOR THE BUILD THAT OPENS IT (docs/SPEC-TABLES.md §7): the
// host's own order, so the round trip below holds on the big-endian leg too.
static TableByteOrder host_byte_order()
{
    const uint16_t probe = 1;
    return *(const uint8_t *) &probe == 1 ? TableByteOrder::Little : TableByteOrder::Big;
}

// THE COOKS `schema cook-check` READS (docs/SPEC-TABLES.md §7.4): when the
// Makefile names a directory, the cooks this gate writes are saved there, and
// beside one of them a FORGERY whose list slot points its array past the
// holder's extent, which the tool must refuse.
static void save_cook( const char * name, const void * data, int64_t bytes )
{
    const char * dir = getenv( "SCHEMA_LIST_COOK_DIR" );
    if ( dir == NULL ) { return; }
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s.cook", dir, name );
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { printf( "FAIL cannot write %s\n", path ); failures++; return; }
    fwrite( data, 1, (size_t) bytes, f );
    fclose( f );
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

static void reports_agree( const TableReport & a, const TableReport & b )
{
    CHECK_EQ( a.unknown, b.unknown );
    CHECK_EQ( a.kind_mismatch, b.kind_mismatch );
    CHECK_EQ( a.clamped, b.clamped );
    CHECK_EQ( a.duplicate, b.duplicate );
    CHECK_EQ( (int) a.malformed, (int) b.malformed );
}

// ---- the instances (docs/SPEC-TABLES.md §2.9) ----

// §2.9's own example: three placements, a []*T whose two slots name one node
// beside a null slot, and three scalars: `list_tables`.
static void build_save( SaveBuilder & b )
{
    Save * save = b.GetRoot();
    for ( int i = 0; i < 3; i++ )
    {
        Placement * placement = SavePlacementsAdd( b.main, save->placements );
        CHECK( placement != NULL );
        placement->x = 1.0f + (float) i;
        placement->y = 2.0f * (float) i;
        placement->model = (uint32_t) ( 3 + i );
    }
    // a pointer element: Add hands back the SLOT at null, Emplace fills it as it
    // fills any pointer slot, and a second slot holds the same reference
    TableRef * slot = SaveLogAdd( b.main, save->log );
    CHECK( slot != NULL );
    LogEntry * shared = LogEntryEmplace( b.main, *slot );
    CHECK( shared != NULL );
    shared->tick = 7;
    *SaveLogAdd( b.main, save->log ) = *slot; // two slots, one node
    SaveLogAdd( b.main, save->log );          // and a null slot
    *SaveScoresAdd( b.main, save->scores ) = 10;
    *SaveScoresAdd( b.main, save->scores ) = 20;
    *SaveScoresAdd( b.main, save->scores ) = 30;
}

static uint8_t wire_tables[1u << 16];
static int64_t bytes_tables = 0;

// ---- the writer (docs/SPEC-TABLES.md §2.9) ----

static void test_writer()
{
    {
        SaveBuilder b;
        build_save( b );
        const int64_t measured = SaveMeasure( b );
        bytes_tables = SaveSave( b, wire_tables, sizeof( wire_tables ) );
        CHECK_EQ( measured, bytes_tables ); // measure == save over a list is a check on the arithmetic alone (§2.9)
        pin_golden( "list_tables", wire_tables, bytes_tables );
        // MEASURE EQUALS SAVE AT EXACT CAPACITY, and one short of it refuses
        static uint8_t exact[1u << 16];
        CHECK_EQ( SaveSave( b, exact, measured ), measured );
        CHECK( memcmp( exact, wire_tables, (size_t) measured ) == 0 );
        CHECK_EQ( SaveSave( b, exact, measured - 1 ), -1 );
    }
    {
        // CONTROL: the writer emits the elements OUT OF ORDER. `list_scalars`
        // meets it: the byte compare against its pinned wire goes red while
        // measure == save still holds.
        SaveBuilder b;
        Save * save = b.GetRoot();
        *SaveScoresAdd( b.main, save->scores ) = 10;
        *SaveScoresAdd( b.main, save->scores ) = 20;
        *SaveScoresAdd( b.main, save->scores ) = 30;
        static uint8_t wire[1u << 16];
        const int64_t measured = SaveMeasure( b );
        const int64_t n = SaveSave( b, wire, sizeof( wire ) );
        CHECK_EQ( measured, n );
        pin_golden( "list_scalars", wire, n );
    }
    {
        // `list_empty`: an EMPTY list beside a full one elides under §3's
        // by-value rule, and a fresh Save is the empty wire
        SaveBuilder b;
        Save * save = b.GetRoot();
        SavePlacementsAdd( b.main, save->placements )->model = 1;
        SavePlacementsAdd( b.main, save->placements )->model = 2;
        static uint8_t wire[1u << 16];
        const int64_t n = SaveSave( b, wire, sizeof( wire ) );
        CHECK_EQ( SaveMeasure( b ), n );
        pin_golden( "list_empty", wire, n );
        SaveBuilder fresh;
        static uint8_t empty[64];
        CHECK_EQ( SaveSave( fresh, empty, sizeof( empty ) ), empty_wire_bytes );
    }
    {
        // CONTROL: `Save` emits a DEAD element. `list_erased` meets it, an
        // erase from the MIDDLE with an add after it, so a writer that merely
        // truncates still goes red, and the byte compare against the same
        // five elements added directly says the sabotage is the skip and not
        // the arithmetic.
        SaveBuilder b;
        Save * save = b.GetRoot();
        Placement * held[5] = { NULL, NULL, NULL, NULL, NULL };
        for ( int i = 0; i < 5; i++ )
        {
            held[i] = SavePlacementsAdd( b.main, save->placements );
            held[i]->model = (uint32_t) ( 100 + i );
        }
        CHECK( SavePlacementsErase( b.arena, save->placements, held[2] ) );
        CHECK( !SavePlacementsErase( b.arena, save->placements, held[2] ) ); // already erased: false
        Placement foreign;
        CHECK( !SavePlacementsErase( b.arena, save->placements, &foreign ) ); // not this list's: false
        SavePlacementsAdd( b.main, save->placements )->model = 105;
        CHECK_EQ( save->placements.count, 5 );
        static uint8_t erased[1u << 16];
        const int64_t measured = SaveMeasure( b );
        const int64_t n = SaveSave( b, erased, sizeof( erased ) );
        CHECK_EQ( measured, n );
        pin_golden( "list_erased", erased, n );

        SaveBuilder direct;
        const uint32_t models[5] = { 100, 101, 103, 104, 105 };
        for ( int i = 0; i < 5; i++ ) { SavePlacementsAdd( direct.main, direct.GetRoot()->placements )->model = models[i]; }
        static uint8_t straight[1u << 16];
        const int64_t m = SaveSave( direct, straight, sizeof( straight ) );
        CHECK_EQ( m, n );
        CHECK( memcmp( straight, erased, (size_t) n ) == 0 );
    }
    {
        // the five element classes are [..N]T's exactly: an ENUM, a FLAGS mask
        // and a UNION are elements as they are in a bounded array, and a bar
        // attribute qualifies the ELEMENT
        MixedBuilder b;
        Mixed * mixed = b.GetRoot();
        *MixedGradesAdd( b.main, mixed->grades ) = Grade::A;
        *MixedGradesAdd( b.main, mixed->grades ) = Grade::C;
        *MixedGradesAdd( b.main, mixed->grades ) = Grade::B;
        *MixedPermsAdd( b.main, mixed->perms ) = Perm_Read | Perm_Write;
        *MixedPermsAdd( b.main, mixed->perms ) = Perm_Own;
        Hit * point = MixedHitsAdd( b.main, mixed->hits );
        point->type = HitType::Point;
        PointReset( point->point );
        point->point.x = 1;
        point->point.y = 2;
        Hit * damage = MixedHitsAdd( b.main, mixed->hits );
        damage->type = HitType::Damage;
        damage->damage = 7;
        MixedHitsAdd( b.main, mixed->hits ); // a None element in its place
        *MixedBoundsAdd( b.main, mixed->bounds ) = 0;
        *MixedBoundsAdd( b.main, mixed->bounds ) = 50;
        *MixedBoundsAdd( b.main, mixed->bounds ) = 100;
        static uint8_t wire[1u << 16];
        const int64_t measured = MixedMeasure( b );
        const int64_t n = MixedSave( b, wire, sizeof( wire ) );
        CHECK_EQ( measured, n );
        pin_golden( "list_mixed", wire, n );

        const int64_t need = MixedLoadMeasure( wire, n );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport report;
        const Mixed * loaded = MixedLoad( region, need, wire, n, &report );
        CHECK( loaded != NULL );
        report_silent( report, "list_mixed" );
        if ( loaded != NULL )
        {
            CHECK_EQ( loaded->grades.size(), 3 );
            CHECK( loaded->grades[1] == Grade::C );
            CHECK_EQ( loaded->perms.size(), 2 );
            CHECK_EQ( loaded->perms[0], Perm_Read | Perm_Write );
            CHECK_EQ( loaded->hits.size(), 3 );
            CHECK( loaded->hits[0].type == HitType::Point && loaded->hits[0].point.y == 2 );
            CHECK( loaded->hits[1].type == HitType::Damage && loaded->hits[1].damage == 7 );
            CHECK( loaded->hits[2].type == HitType::None );
            CHECK_EQ( loaded->bounds.size(), 3 );
            CHECK_EQ( loaded->bounds[2], 100 );
        }
        free( region );
    }
}

// ---- the builder's three (docs/SPEC-TABLES.md §2.9) ----

static void test_builder()
{
    SaveBuilder b;
    Save * save = b.GetRoot();

    // ADD hands the element back at its declared defaults
    Placement * first = SavePlacementsAdd( b.main, save->placements );
    CHECK( first != NULL );
    CHECK( first->x == 0.0f && first->model == 0 );
    CHECK_EQ( save->placements.count, 1 );
    first->model = 11;

    // MORE THAN ONE SEGMENT: an element's address is stable for the arena's
    // life, so a pointer handed back by an early Add survives every later one
    for ( int i = 1; i < 200; i++ )
    {
        Placement * p = SavePlacementsAdd( b.main, save->placements );
        CHECK( p != NULL );
        p->model = (uint32_t) ( 11 + i );
    }
    CHECK_EQ( save->placements.count, 200 );
    CHECK_EQ( first->model, 11 ); // NOTHING EVER MOVES (§6.4)

    // ERASE by the element's own pointer, from the MIDDLE. EACH on the builder
    // is INDEX order, live elements only
    int32_t seen = 0;
    Placement * third = NULL;
    for ( Placement * p : SavePlacementsEach( b.arena, save->placements ) )
    {
        if ( seen == 2 ) { third = p; }
        seen++;
    }
    CHECK_EQ( seen, 200 );
    CHECK( third != NULL && third->model == 13 );
    CHECK( SavePlacementsErase( b.arena, save->placements, third ) );
    CHECK_EQ( save->placements.count, 199 );
    seen = 0;
    for ( Placement * p : SavePlacementsEach( b.arena, save->placements ) )
    {
        CHECK( p->model != 13 ); // the dead element is skipped
        if ( seen == 2 ) { CHECK_EQ( p->model, 14 ); } // INDICES ARE NOT STABLE ACROSS AN ERASE
        seen++;
    }
    CHECK_EQ( seen, 199 );

    // and the const form agrees once locked: what was index 3 is index 2
    CHECK( b.Lock() );
    const Save * locked = b.AsConst();
    CHECK( locked != NULL );
    if ( locked != NULL )
    {
        CHECK_EQ( locked->placements.size(), 199 );
        CHECK_EQ( locked->placements[2].model, 14 );
        CHECK_EQ( locked->placements[198].model, 210 );
    }

    // a []*T: Add hands back the SLOT at null
    SaveBuilder p;
    TableRef * slot = SaveLogAdd( p.main, p.GetRoot()->log );
    CHECK( slot != NULL && slot->value == 0 );
    CHECK_EQ( p.GetRoot()->log.count, 1 );
    LogEntry * entry = LogEntryEmplace( p.main, *slot );
    CHECK( entry != NULL );
    entry->tick = 3;
    int32_t slots = 0;
    for ( TableRef * s : SaveLogEach( p.arena, p.GetRoot()->log ) ) { CHECK( LogEntryAt( p.arena, *s ) == entry ); slots++; }
    CHECK_EQ( slots, 1 );
}

// ---- the const form: a locked region, a loaded one, an opened cook ----

static void check_const_form( const Save * s, const char * where )
{
    if ( s == NULL ) { printf( "FAIL %s: no root\n", where ); failures++; return; }
    CHECK_EQ( s->placements.size(), 3 );
    if ( s->placements.size() == 3 )
    {
        CHECK( s->placements[0].x == 1.0f );
        CHECK_EQ( s->placements[2].model, 5 );
        int i = 0;
        for ( const Placement & p : s->placements ) { CHECK_EQ( p.model, 3 + i ); i++; }
        CHECK_EQ( i, 3 );
    }
    // a []*T's const operator[] answers the RESOLVED pointer: TWO SLOTS, ONE
    // NODE, and a null slot answers NULL
    CHECK_EQ( s->log.size(), 3 );
    if ( s->log.size() == 3 )
    {
        const LogEntry * a = s->log[0];
        const LogEntry * again = s->log[1];
        CHECK( a != NULL && a == again );
        if ( a != NULL ) { CHECK_EQ( a->tick, 7 ); }
        CHECK( s->log[2] == NULL );
        int i = 0;
        for ( const LogEntry * e : s->log ) { if ( i < 2 ) { CHECK( e == a ); } else { CHECK( e == NULL ); } i++; }
        CHECK_EQ( i, 3 );
    }
    CHECK_EQ( s->scores.size(), 3 );
    if ( s->scores.size() == 3 ) { CHECK_EQ( s->scores[1], 20 ); }
}

static void test_const_forms()
{
    SaveBuilder b;
    build_save( b );
    CHECK( b.Lock() );
    check_const_form( b.AsConst(), "the locked region" );

    // a locked region re-saves the same bytes as the builder did
    static uint8_t again[1u << 16];
    const int64_t n = SaveSave( b.AsConst(), again, sizeof( again ) );
    CHECK_EQ( n, bytes_tables );
    CHECK( memcmp( again, wire_tables, (size_t) bytes_tables ) == 0 );

    // LOAD into the caller's exact-sized region
    const int64_t need = SaveLoadMeasure( wire_tables, bytes_tables );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return; }
    TableReport report;
    const Save * loaded = SaveLoad( region, need, wire_tables, bytes_tables, &report );
    check_const_form( loaded, "a loaded region" );
    report_silent( report, "a loaded region" );
    const int64_t back = SaveSave( loaded, again, sizeof( again ) );
    CHECK_EQ( back, bytes_tables );
    CHECK( memcmp( again, wire_tables, (size_t) bytes_tables ) == 0 );

    // LoadBuilder is the TOOL's path, and it produces EXACTLY the same report
    SaveBuilder into;
    TableReport tool;
    CHECK( SaveLoadBuilder( into, wire_tables, bytes_tables, &tool ) );
    reports_agree( tool, report );
    CHECK_EQ( into.GetRoot()->placements.count, 3 );
    CHECK_EQ( into.GetRoot()->log.count, 3 );
    const int64_t relocked = SaveSave( into, again, sizeof( again ) );
    CHECK_EQ( relocked, bytes_tables );
    CHECK( memcmp( again, wire_tables, (size_t) bytes_tables ) == 0 );

    // the COOK: a region written verbatim, opened O(1) and indexed in place
    const int64_t cook_bytes = SaveCookMeasure( loaded );
    CHECK( cook_bytes > 0 );
    void * cooked = MEASURED_CALLOC( cook_bytes, 0 );
    if ( cooked == NULL ) { free( region ); return; }
    CHECK( SaveCook( loaded, cooked, (uint64_t) cook_bytes, host_byte_order() ) );
    check_const_form( SaveOpen( cooked, (uint64_t) cook_bytes ), "an opened cook" );
    save_cook( "save", cooked, cook_bytes );
    // and two cooks of one instance are ONE artifact, from the region and from the builder alike
    void * twice = MEASURED_CALLOC( cook_bytes, 0 );
    if ( twice == NULL ) { free( cooked ); free( region ); return; }
    CHECK( SaveCook( loaded, twice, (uint64_t) cook_bytes, host_byte_order() ) );
    CHECK( memcmp( cooked, twice, (size_t) cook_bytes ) == 0 );
    CHECK_EQ( SaveCookMeasure( into ), cook_bytes );
    CHECK( SaveCook( into, twice, (uint64_t) cook_bytes, host_byte_order() ) );
    CHECK( memcmp( cooked, twice, (size_t) cook_bytes ) == 0 );
    free( twice );
    free( cooked );
    // the region is EXACT: one byte short is refused. Load zeroes the region
    // it is handed before it refuses, so this probe is the block's last act.
    TableReport short_report;
    CHECK( SaveLoad( region, need - 1, wire_tables, bytes_tables, &short_report ) == NULL );
    free( region );
}

// ---- the reader's rules, each on a hand-made body (§2.9) ----

// an `Ints` body written FROM THE GRAMMAR: a kind 14 array of kind 4 (int32)
// elements, `after` behind it, and one knob each for the controls
struct IntsSpec
{
    int32_t n;            // elements written
    int64_t declared_n;   // -1: n itself
    uint8_t element_kind; // 0: int32's own kind
    int64_t body_len;     // -1: the body's own length; else a FORGED array body length
    int32_t after;
};

static IntsSpec ints_spec()
{
    IntsSpec spec = { 3, -1, 0, -1, 777 };
    return spec;
}

struct Wire
{
    uint8_t bytes[4096];
    int64_t size;
};

static Wire build_ints( const IntsSpec & spec )
{
    WireBuilder b;
    b.field( "values", 14 );
    if ( spec.body_len < 0 )
    {
        const int64_t body = b.open_len();
        b.u8( spec.element_kind != 0 ? spec.element_kind : 4 );
        b.leb( spec.declared_n >= 0 ? (uint64_t) spec.declared_n : (uint64_t) spec.n );
        for ( int32_t i = 0; i < spec.n; i++ ) { b.u32( (uint32_t) ( 10 * ( i + 1 ) ) ); }
        b.close_len( body );
    }
    else
    {
        // a body TOO SHORT for its own header, or short of its count
        b.leb( (uint64_t) spec.body_len );
        for ( int64_t i = 0; i < spec.body_len; i++ ) { b.u8( i == 0 ? 4 : 0 ); }
    }
    b.field( "after", 4 );
    b.u32( (uint32_t) spec.after );
    b.end();
    Wire w;
    w.size = b.finish( w.bytes );
    return w;
}

struct Verdict
{
    int32_t count;
    TableReport report;
    int32_t after;
    int32_t decoded[3];
};

static Verdict read_ints( const Wire & w )
{
    Verdict v = { -1, TableReport(), 0, { 0, 0, 0 } };
    const int64_t need = IntsLoadMeasure( w.bytes, w.size );
    if ( need < 0 ) { v.count = -2; return v; } // the measure REFUSED the framing
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return v; }
    const Ints * ints = IntsLoad( region, need, w.bytes, w.size, &v.report );
    if ( ints != NULL )
    {
        v.count = ints->values.size();
        v.after = ints->after;
        for ( int32_t i = 0; i < v.count && i < 3; i++ ) { v.decoded[i] = ints->values[i]; }
    }
    free( region );

    // EVERY LOAD PATH PRODUCES ONE REPORT (§2.9): the two paths agree on every
    // wire either of them decodes
    IntsBuilder into;
    TableReport t;
    IntsLoadBuilder( into, w.bytes, w.size, &t );
    reports_agree( t, v.report );
    CHECK_EQ( into.GetRoot()->values.count, v.count < 0 ? 0 : v.count );
    return v;
}

static void test_reader()
{
    {
        const Verdict v = read_ints( build_ints( ints_spec() ) );
        CHECK_EQ( v.count, 3 );
        CHECK_EQ( v.decoded[2], 30 );
        CHECK_EQ( v.after, 777 );
        report_silent( v.report, "a good Ints wire" );
    }
    {
        // CONTROL: the element-kind rule decodes anyway. An `Ints` wire read by
        // `Floats` is §3's element-kind mismatch: the field reads EMPTY, one
        // kind_mismatch counts, and the parent reads on. And the reverse.
        Wire w = build_ints( ints_spec() );
        const int64_t need = FloatsLoadMeasure( w.bytes, w.size );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Floats * floats = FloatsLoad( region, need, w.bytes, w.size, &r );
        CHECK( floats != NULL );
        if ( floats != NULL )
        {
            CHECK_EQ( floats->values.size(), 0 );
            CHECK_EQ( floats->after, 777 );
        }
        CHECK_EQ( r.kind_mismatch, 1 );
        CHECK_EQ( r.unknown + r.clamped + r.duplicate + (int) r.malformed, 0 );
        free( region );

        IntsSpec spec = ints_spec();
        spec.element_kind = 10; // float32's kind, under Ints' declaration
        const Verdict v = read_ints( build_ints( spec ) );
        CHECK_EQ( v.count, 0 );
        CHECK_EQ( v.report.kind_mismatch, 1 );
        CHECK( !v.report.malformed );
        CHECK_EQ( v.after, 777 );
    }
    {
        // A COUNT THE BODY CANNOT COVER (§2.9): into a REGION, LoadMeasure
        // answers -1 with the reason count_over_length and no Load runs. Into a
        // BUILDER, the prefix the body covers lands, malformed counts, and the
        // parent reads on past the field's L
        IntsSpec spec = ints_spec();
        spec.declared_n = 1000;
        Wire w = build_ints( spec );
        TableRefuseReason reason = count_over_extent_cap;
        CHECK_EQ( IntsLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_length );
        IntsBuilder into;
        TableReport t;
        CHECK( IntsLoadBuilder( into, w.bytes, w.size, &t ) );
        CHECK( t.malformed );
        CHECK_EQ( t.kind_mismatch + t.clamped + t.unknown + t.duplicate, 0 );
        CHECK_EQ( into.GetRoot()->values.count, 3 ); // the prefix the body covers
        CHECK_EQ( into.GetRoot()->after, 777 );
    }
    {
        // A BODY TOO SHORT TO CARRY ITS OWN HEADER IS INERT (§4): no element,
        // no counter, the field keeps the value it has
        IntsSpec spec = ints_spec();
        spec.body_len = 1;
        const Verdict v = read_ints( build_ints( spec ) );
        CHECK_EQ( v.count, 0 );
        report_silent( v.report, "an inert list body" );
        CHECK_EQ( v.after, 777 );
    }
    {
        // an EMPTY list on the wire: a header and a zero count
        IntsSpec spec = ints_spec();
        spec.n = 0;
        const Verdict v = read_ints( build_ints( spec ) );
        CHECK_EQ( v.count, 0 );
        report_silent( v.report, "an empty list body" );
        CHECK_EQ( v.after, 777 );
    }
    {
        // A DAMAGED ELEMENT inside a good count: a list of TABLES whose third
        // element's L runs past the body keeps the two it decoded, counts
        // malformed, and the parent reads on past the field's L
        WireBuilder b;
        b.field( "items", 14 );
        const int64_t body = b.open_len();
        b.u8( 13 );
        b.leb( 3 );
        for ( int i = 0; i < 2; i++ )
        {
            const int64_t elem = b.open_len();
            b.field( "v", 4 );
            b.u32( (uint32_t) ( 5 + i ) );
            b.end();
            b.close_len( elem );
        }
        b.leb( 100 ); // the third element's L, past the body
        b.close_len( body );
        b.field( "label", 4 );
        b.u32( 9 );
        b.end();
        Wire w;
        w.size = b.finish( w.bytes );
        const int64_t need = RowLoadMeasure( w.bytes, w.size );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Row * row = RowLoad( region, need, w.bytes, w.size, &r );
        CHECK( row != NULL );
        CHECK( r.malformed );
        if ( row != NULL )
        {
            CHECK_EQ( row->items.size(), 2 ); // the element that never landed is not counted
            if ( row->items.size() == 2 ) { CHECK_EQ( row->items[1].v, 6 ); }
            CHECK_EQ( row->label, 9 );
        }
        RowBuilder into;
        TableReport t;
        RowLoadBuilder( into, w.bytes, w.size, &t );
        reports_agree( t, r );
        CHECK_EQ( into.GetRoot()->items.count, 2 );
        free( region );
    }
}

// ---- THE CLAMP CONTROL AT 100,000 (docs/SPEC-TABLES.md §2.9) ----
//
// CONTROL: the reader CLAMPS the count against something. A []uint8 carrying
// 100,000 elements is past 2^16 and so past any bound a schema on the page
// declares, so a clamp any control author happened to pick would show, and the
// decoded count goes red, and `clamped` stays at zero.
static void test_clamp_control()
{
    static const int32_t kElements = 100000;
    BytesBuilder b;
    Bytes * bytes = b.GetRoot();
    for ( int32_t i = 0; i < kElements; i++ )
    {
        uint8_t * e = BytesDataAdd( b.main, bytes->data );
        if ( e == NULL ) { printf( "FAIL: Add answered NULL at %d\n", i ); failures++; return; }
        *e = (uint8_t) ( i & 0xff );
    }
    bytes->after = 4242;
    CHECK_EQ( bytes->data.count, kElements );
    const int64_t measured = BytesMeasure( b );
    uint8_t * wire = (uint8_t *) MEASURED_CALLOC( measured, 0 );
    if ( wire == NULL ) { return; }
    const int64_t n = BytesSave( b, wire, measured );
    CHECK_EQ( n, measured );

    const int64_t need = BytesLoadMeasure( wire, n );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { free( wire ); return; }
    TableReport r;
    const Bytes * loaded = BytesLoad( region, need, wire, n, &r );
    CHECK( loaded != NULL );
    report_silent( r, "the clamp control" );
    if ( loaded != NULL )
    {
        CHECK_EQ( loaded->data.size(), kElements );
        CHECK_EQ( r.clamped, 0 );
        bool intact = loaded->data.size() == kElements;
        for ( int32_t i = 0; intact && i < kElements; i++ ) { if ( loaded->data[i] != (uint8_t) ( i & 0xff ) ) { intact = false; } }
        CHECK( intact );
        CHECK_EQ( loaded->after, 4242 );
    }
    BytesBuilder into;
    TableReport t;
    CHECK( BytesLoadBuilder( into, wire, n, &t ) );
    reports_agree( t, r );
    CHECK_EQ( into.GetRoot()->data.count, kElements );
    free( region );
    free( wire );
}

// ---- THE SIX LoadMeasure REFUSALS (docs/SPEC-TABLES.md §2.8, §2.9, §6.5) ----
//
// A unit test and not a `report` row, because a refusal produces no counters.
// Each wire is built in memory with a SYNTHETIC count rather than a golden:
// a count above the int32 cap, which no golden could carry because the file
// would be two gigabytes, a count whose elements cannot fit the field's L, the
// same two at DEPTH, inside an element's own list, the same two inside an
// element's MAP, which answers by the one rule a list does, and a clean wire beside
// them, which must measure. Red if any of the six answers something other
// than -1 with its own reason, if the clean one refuses, or if any of them
// moves one of the report's counters.

static Wire build_sheet( uint64_t rows, uint64_t items, int32_t real_items )
{
    WireBuilder b;
    b.field( "rows", 14 );
    const int64_t body = b.open_len();
    b.u8( 13 );
    b.leb( rows );
    {
        const int64_t row = b.open_len();
        b.field( "items", 14 );
        const int64_t inner = b.open_len();
        b.u8( 13 );
        b.leb( items );
        for ( int32_t i = 0; i < real_items; i++ )
        {
            const int64_t sample = b.open_len();
            b.field( "v", 4 );
            b.u32( (uint32_t) i );
            b.end();
            b.close_len( sample );
        }
        b.close_len( inner );
        b.field( "label", 4 );
        b.u32( 1 );
        b.end();
        b.close_len( row );
    }
    b.close_len( body );
    b.end();
    Wire w;
    w.size = b.finish( w.bytes );
    return w;
}

// an `Army` body written FROM THE GRAMMAR: a kind 14 array of kind 13 `Squad`
// elements, each holding its `roster` MAP as a kind 14 array of kind 13
// entries (§2.8), with the map's declared count a knob of its own
static Wire build_army( uint64_t squads, uint64_t entries, int32_t real_entries )
{
    WireBuilder b;
    b.field( "squads", 14 );
    const int64_t body = b.open_len();
    b.u8( 13 );
    b.leb( squads );
    {
        const int64_t squad = b.open_len();
        b.field( "roster", 14 );
        const int64_t inner = b.open_len();
        b.u8( 13 );
        b.leb( entries );
        for ( int32_t i = 0; i < real_entries; i++ )
        {
            const int64_t entry = b.open_len();
            b.field( "key", 6 );
            b.u8( (uint8_t) ( 2 + i ) );
            b.field( "value", 13 );
            const int64_t item = b.open_len();
            b.field( "count", 4 );
            b.u32( (uint32_t) ( 20 + i ) );
            b.end();
            b.close_len( item );
            b.end();
            b.close_len( entry );
        }
        b.close_len( inner );
        b.field( "name", 4 );
        b.u32( 50 );
        b.end();
        b.close_len( squad );
    }
    b.close_len( body );
    b.field( "after", 4 );
    b.u32( 8 );
    b.end();
    Wire w;
    w.size = b.finish( w.bytes );
    return w;
}

static void test_measure_refusals()
{
    // a count above the int32 cap, at the ROOT
    {
        IntsSpec spec = ints_spec();
        spec.declared_n = 0x80000000ll;
        Wire w = build_ints( spec );
        TableRefuseReason reason = count_over_length;
        CHECK_EQ( IntsLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_extent_cap );
        // and into a BUILDER it is the refusal LoadBuilder answers NULL for,
        // the report holding what it held when the count was met: nothing
        IntsBuilder into;
        TableReport t;
        CHECK( !IntsLoadBuilder( into, w.bytes, w.size, &t ) );
        report_silent( t, "the over-cap refusal into a builder" );
    }
    // a count the field's L cannot carry, at the ROOT
    {
        IntsSpec spec = ints_spec();
        spec.declared_n = 100000;
        Wire w = build_ints( spec );
        TableRefuseReason reason = count_over_extent_cap;
        CHECK_EQ( IntsLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_length );
    }
    // the same two at DEPTH, inside an element's own list
    {
        Wire w = build_sheet( 1, 0x80000000ull, 1 );
        TableRefuseReason reason = count_over_length;
        CHECK_EQ( SheetLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_extent_cap );
        SheetBuilder into;
        TableReport t;
        CHECK( !SheetLoadBuilder( into, w.bytes, w.size, &t ) );
        report_silent( t, "the over-cap refusal at depth into a builder" );
    }
    {
        Wire w = build_sheet( 1, 100000, 1 );
        TableRefuseReason reason = count_over_extent_cap;
        CHECK_EQ( SheetLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_length );
    }
    // the same two at DEPTH, inside an element's MAP (§2.8): a map's term
    // answers the reasons a list's does, the int32 cap first, one rule for
    // both constructs (§6.5)
    {
        Wire w = build_army( 1, 0x80000000ull, 1 );
        TableRefuseReason reason = count_over_length;
        CHECK_EQ( ArmyLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_extent_cap );
    }
    {
        Wire w = build_army( 1, 100000, 1 );
        TableRefuseReason reason = count_over_extent_cap;
        CHECK_EQ( ArmyLoadMeasure( w.bytes, w.size, NULL, &reason ), -1 );
        CHECK( reason == count_over_length );
    }
    // and a clean map-holding wire beside them, which must measure and load silently
    {
        Wire w = build_army( 1, 2, 2 );
        TableRefuseReason reason = count_over_length;
        const int64_t need = ArmyLoadMeasure( w.bytes, w.size, NULL, &reason );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Army * army = ArmyLoad( region, need, w.bytes, w.size, &r );
        CHECK( army != NULL );
        report_silent( r, "the clean map-holding wire beside the refusals" );
        if ( army != NULL && army->squads.size() == 1 )
        {
            CHECK_EQ( army->squads[0].roster.size(), 2 );
            const Item * item = army->squads[0].roster.Find( (uint8_t) 3 );
            CHECK( item != NULL && item->count == 21 );
        }
        free( region );
    }
    // and a clean wire beside them, which must measure and load silently
    {
        Wire w = build_sheet( 1, 2, 2 );
        TableRefuseReason reason = count_over_length;
        const int64_t need = SheetLoadMeasure( w.bytes, w.size, NULL, &reason );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Sheet * sheet = SheetLoad( region, need, w.bytes, w.size, &r );
        CHECK( sheet != NULL );
        report_silent( r, "the clean wire beside the refusals" );
        if ( sheet != NULL )
        {
            CHECK_EQ( sheet->rows.size(), 1 );
            if ( sheet->rows.size() == 1 ) { CHECK_EQ( sheet->rows[0].items.size(), 2 ); }
        }
        free( region );
    }
}

// ---- sharing and the walk order (docs/SPEC-TABLES.md §2.9, §3.1) ----

static void test_shared()
{
    {
        // `list_shared`: two slots naming one node beside a null slot. CONTROL:
        // a shared node is written TWICE: the region's byte count and the
        // text round trip's &node resolution go red.
        AlbumBuilder b;
        Album * album = b.GetRoot();
        TableRef * slot = AlbumPhotosAdd( b.main, album->photos );
        Photo * photo = PhotoEmplace( b.main, *slot );
        photo->width = 640;
        photo->height = 480;
        *AlbumPhotosAdd( b.main, album->photos ) = *slot;
        AlbumPhotosAdd( b.main, album->photos ); // null
        static uint8_t wire[1u << 16];
        const int64_t n = AlbumSave( b, wire, sizeof( wire ) );
        CHECK_EQ( AlbumMeasure( b ), n );
        pin_golden( "list_shared", wire, n );

        const int64_t need = AlbumLoadMeasure( wire, n );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Album * loaded = AlbumLoad( region, need, wire, n, &r );
        CHECK( loaded != NULL );
        report_silent( r, "list_shared" );
        if ( loaded != NULL )
        {
            CHECK_EQ( loaded->photos.size(), 3 );
            if ( loaded->photos.size() == 3 )
            {
                CHECK( loaded->photos[0] != NULL && loaded->photos[0] == loaded->photos[1] );
                CHECK( loaded->photos[2] == NULL );
                if ( loaded->photos[0] != NULL ) { CHECK_EQ( loaded->photos[0]->width, 640 ); }
            }
            CHECK( PhotoAt( loaded->cover ) == NULL );
            // ONE node in the region: the attribution names the root and one photo
            int64_t attribution = 0;
            CHECK( AlbumLoadMeasure( wire, n, &attribution ) == need );
            CHECK_EQ( attribution, 2 * (int64_t) sizeof( TableNodeDirEntry ) );
        }
        // the text: one definition and one &node reference, a null in its place
        CHECK( b.Lock() );
        const int64_t text_bytes = AlbumToJsonMeasure( b.AsConst() );
        char * text = (char *) MEASURED_CALLOC( text_bytes, 1 );
        if ( text == NULL ) { free( region ); return; }
        CHECK_EQ( AlbumToJson( b.AsConst(), text, text_bytes ), text_bytes );
        CHECK( strstr( text, "&node" ) != NULL );
        CHECK( strstr( text, "null" ) != NULL );
        AlbumBuilder into;
        TableReport t;
        CHECK( AlbumFromJson( into, text, text_bytes, &t ) );
        report_silent( t, "list_shared from text" );
        static uint8_t from_text[1u << 16];
        CHECK_EQ( AlbumSave( into, from_text, sizeof( from_text ) ), n );
        CHECK( memcmp( from_text, wire, (size_t) n ) == 0 );
        free( text );
        free( region );
    }
    {
        // `list_before_pointer`: the []*T is DECLARED BEFORE `cover` and reaches
        // the shared node first, so it numbers it first. CONTROL: the walk
        // visits lists out of declaration order, grouped after the pointer
        // fields, and the pinned wire goes red on the node numbering.
        AlbumBuilder b;
        Album * album = b.GetRoot();
        TableRef * first = AlbumPhotosAdd( b.main, album->photos );
        Photo * b_node = PhotoEmplace( b.main, *first );
        b_node->width = 2;
        TableRef * second = AlbumPhotosAdd( b.main, album->photos );
        Photo * a_node = PhotoEmplace( b.main, *second );
        a_node->width = 1;
        album->cover = *second; // the SAME node, through the pointer field declared after
        static uint8_t wire[1u << 16];
        const int64_t n = AlbumSave( b, wire, sizeof( wire ) );
        CHECK_EQ( AlbumMeasure( b ), n );
        pin_golden( "list_before_pointer", wire, n );
        const int64_t need = AlbumLoadMeasure( wire, n );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Album * loaded = AlbumLoad( region, need, wire, n, &r );
        CHECK( loaded != NULL );
        report_silent( r, "list_before_pointer" );
        if ( loaded != NULL )
        {
            CHECK_EQ( loaded->photos.size(), 2 );
            CHECK( PhotoAt( loaded->cover ) != NULL && PhotoAt( loaded->cover ) == loaded->photos[1] );
            CHECK( loaded->photos[0] != loaded->photos[1] );
        }
        free( region );
    }
}

// ---- where else a list rides in a holder's extent (§2.9) ----

static void test_nested()
{
    {
        // `list_nested`: a list of tables that hold lists, and a pointed-at
        // holder with a list of its own. CONTROL: LoadMeasure's term summed at
        // ONE DEPTH only, and the measure goes red against the region Load fills.
        SheetBuilder b;
        Sheet * sheet = b.GetRoot();
        for ( int i = 0; i < 3; i++ )
        {
            Row * row = SheetRowsAdd( b.main, sheet->rows );
            row->label = 10 + i;
            for ( int k = 0; k <= i; k++ ) { RowItemsAdd( b.main, row->items )->v = 100 * i + k; }
        }
        Row * pinned = RowEmplace( b.main, sheet->pinned );
        pinned->label = 99;
        RowItemsAdd( b.main, pinned->items )->v = 1;
        RowItemsAdd( b.main, pinned->items )->v = 2;
        static uint8_t wire[1u << 16];
        const int64_t n = SheetSave( b, wire, sizeof( wire ) );
        CHECK_EQ( SheetMeasure( b ), n );
        pin_golden( "list_nested", wire, n );

        const int64_t need = SheetLoadMeasure( wire, n );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Sheet * loaded = SheetLoad( region, need, wire, n, &r );
        CHECK( loaded != NULL );
        report_silent( r, "list_nested" );
        if ( loaded != NULL )
        {
            CHECK_EQ( loaded->rows.size(), 3 );
            for ( int32_t i = 0; i < loaded->rows.size(); i++ )
            {
                const Row & row = loaded->rows[i];
                CHECK_EQ( row.label, 10 + i );
                CHECK_EQ( row.items.size(), i + 1 );
                for ( int32_t k = 0; k < row.items.size(); k++ ) { CHECK_EQ( row.items[k].v, 100 * i + k ); }
            }
            const Row * p = RowAt( loaded->pinned );
            CHECK( p != NULL );
            if ( p != NULL )
            {
                CHECK_EQ( p->label, 99 );
                CHECK_EQ( p->items.size(), 2 );
                if ( p->items.size() == 2 ) { CHECK_EQ( p->items[1].v, 2 ); }
            }
            static uint8_t again[1u << 16];
            CHECK_EQ( SheetSave( loaded, again, sizeof( again ) ), n );
            CHECK( memcmp( again, wire, (size_t) n ) == 0 );

            // the COOK, from the region and from the builder, one artifact
            const int64_t cook_bytes = SheetCookMeasure( loaded );
            CHECK( cook_bytes > 0 );
            void * cooked = MEASURED_CALLOC( cook_bytes, 0 );
            if ( cooked != NULL )
            {
                CHECK( SheetCook( loaded, cooked, (uint64_t) cook_bytes, host_byte_order() ) );
                // THE COOK IS PINNED on a little-endian host, so a LAYOUT
                // sabotage (the element array laid after a nested container's,
                // a wrong alignment, a dropped pre-order) goes red on a CHECK
                // before any Open trusts the bytes. The big-endian leg writes
                // the other order and skips the pin.
                const bool trusted = host_byte_order() != TableByteOrder::Little || pin_golden( "list_nested_cook", (const uint8_t *) cooked, cook_bytes );
                const Sheet * opened = trusted ? SheetOpen( cooked, (uint64_t) cook_bytes ) : NULL;
                CHECK( opened != NULL );
                if ( opened != NULL )
                {
                    CHECK_EQ( opened->rows.size(), 3 );
                    if ( opened->rows.size() == 3 ) { CHECK_EQ( opened->rows[2].items[2].v, 202 ); }
                    const Row * op = RowAt( opened->pinned );
                    CHECK( op != NULL && op->items.size() == 2 && op->items[0].v == 1 );
                }
                CHECK_EQ( SheetCookMeasure( b ), cook_bytes );
                void * twice = MEASURED_CALLOC( cook_bytes, 0 );
                if ( twice != NULL )
                {
                    CHECK( SheetCook( b, twice, (uint64_t) cook_bytes, host_byte_order() ) );
                    CHECK( memcmp( cooked, twice, (size_t) cook_bytes ) == 0 );
                    free( twice );
                }
                save_cook( "sheet", cooked, cook_bytes );
                // THE FORGERY for `schema cook-check`: the root's `rows` slot is the
                // first sixteen bytes of the data part, and its delta is pointed
                // past the region, which §7.4's containment clause must refuse
                if ( getenv( "SCHEMA_LIST_COOK_DIR" ) != NULL )
                {
                    uint8_t * forged = (uint8_t *) cooked;
                    const int64_t delta = cook_bytes; // past every byte the file has
                    memcpy( forged + 64, &delta, sizeof( delta ) );
                    save_cook( "sheet-forged", forged, cook_bytes );
                }
                free( cooked );
            }
        }
        // and the TOOL's path reads every depth into a builder
        SheetBuilder into;
        TableReport t;
        CHECK( SheetLoadBuilder( into, wire, n, &t ) );
        reports_agree( t, r );
        CHECK_EQ( into.GetRoot()->rows.count, 3 );
        static uint8_t relocked[1u << 16];
        CHECK_EQ( SheetSave( into, relocked, sizeof( relocked ) ), n );
        CHECK( memcmp( relocked, wire, (size_t) n ) == 0 );
        // the region is EXACT: one byte short is refused, so the extent scan
        // counted every depth and every node and none twice. Load zeroes the
        // region before it refuses, so this is the block's last act.
        TableReport short_report;
        CHECK( SheetLoad( region, need - 1, wire, n, &short_report ) == NULL );
        free( region );
    }
    {
        // `list_of_maps`: an element that holds a MAP. CONTROL: the element
        // array is laid out AFTER a nested container's, breaking the pre-order
        // rule, and the region's byte compare goes red.
        ArmyBuilder b;
        Army * army = b.GetRoot();
        for ( int i = 0; i < 2; i++ )
        {
            Squad * squad = ArmySquadsAdd( b.main, army->squads );
            squad->name = 50 + i;
            SquadRosterInsert( b.main, squad->roster, (uint8_t) ( 9 - i ) )->count = 90 + i;
            SquadRosterInsert( b.main, squad->roster, (uint8_t) ( 2 + i ) )->count = 20 + i;
        }
        army->after = 8;
        static uint8_t wire[1u << 16];
        const int64_t n = ArmySave( b, wire, sizeof( wire ) );
        CHECK_EQ( ArmyMeasure( b ), n );
        pin_golden( "list_of_maps", wire, n );
        const int64_t need = ArmyLoadMeasure( wire, n );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Army * loaded = ArmyLoad( region, need, wire, n, &r );
        CHECK( loaded != NULL );
        report_silent( r, "list_of_maps" );
        if ( loaded != NULL )
        {
            CHECK_EQ( loaded->squads.size(), 2 );
            for ( int32_t i = 0; i < loaded->squads.size(); i++ )
            {
                const Squad & squad = loaded->squads[i];
                CHECK_EQ( squad.name, 50 + i );
                CHECK_EQ( squad.roster.size(), 2 );
                const Item * low = squad.roster.Find( (uint8_t) ( 2 + i ) );
                CHECK( low != NULL && low->count == 20 + i );
                if ( squad.roster.size() == 2 ) { CHECK_EQ( squad.roster.begin().at->key, 2 + i ); } // sorted
            }
            CHECK_EQ( loaded->after, 8 );
            static uint8_t again[1u << 16];
            CHECK_EQ( ArmySave( loaded, again, sizeof( again ) ), n );
            CHECK( memcmp( again, wire, (size_t) n ) == 0 );
            // the cook lays the element array FIRST, then each element's map
            const int64_t cook_bytes = ArmyCookMeasure( loaded );
            void * cooked = MEASURED_CALLOC( cook_bytes, 0 );
            if ( cooked != NULL )
            {
                CHECK( ArmyCook( loaded, cooked, (uint64_t) cook_bytes, host_byte_order() ) );
                const bool trusted = host_byte_order() != TableByteOrder::Little || pin_golden( "list_of_maps_cook", (const uint8_t *) cooked, cook_bytes );
                const Army * opened = trusted ? ArmyOpen( cooked, (uint64_t) cook_bytes ) : NULL;

                CHECK( opened != NULL );

                if ( opened != NULL && opened->squads.size() == 2 )
                {
                    const Item * item = opened->squads[1].roster.Find( (uint8_t) 8 );
                    CHECK( item != NULL && item->count == 91 );
                }
                save_cook( "army", cooked, cook_bytes );
                free( cooked );
            }
        }
        ArmyBuilder into;
        TableReport t;
        CHECK( ArmyLoadBuilder( into, wire, n, &t ) );
        reports_agree( t, r );
        static uint8_t relocked[1u << 16];
        CHECK_EQ( ArmySave( into, relocked, sizeof( relocked ) ), n );
        CHECK( memcmp( relocked, wire, (size_t) n ) == 0 );
        TableReport short_report; // one byte short is refused; last, because Load zeroes the region first
        CHECK( ArmyLoad( region, need - 1, wire, n, &short_report ) == NULL );
        free( region );
    }

    {
        // AN UNREACHED NON-EMPTY LIST SLOT IS REFUSED by Cook and by Lock, the
        // same refusal §7.6 gives a pointer in that position. The WIRE is not
        // refused, a counted array rides its live slots.
        DeckBuilder past;
        Deck * d = past.GetRoot();
        d->hands_count = 1;
        RowItemsAdd( past.main, d->hands[0].items )->v = 1;
        RowItemsAdd( past.main, d->hands[2].items )->v = 9; // past the count
        static uint8_t rides[1u << 16];
        const int64_t measured = DeckMeasure( past );
        CHECK_EQ( DeckSave( past, rides, sizeof( rides ) ), measured );
        CHECK( !past.Lock() );
        CHECK( past.AsConst() == NULL ); // nothing partial
        CHECK_EQ( DeckCookMeasure( past ), -1 );
    }
}

// ---- the migration itself (docs/SPEC-TABLES.md §2.9) ----

static void test_migrates()
{
    // ONE content, TWO declarations of the holder, [..8]Unit and []Unit, ONE
    // pinned wire both write byte for byte and both read into equal values,
    // the report silent in both directions. The bound is above the count, so
    // the row proves the framing and not the clamp.
    static uint8_t bounded_wire[1u << 16];
    static uint8_t unbounded_wire[1u << 16];
    int64_t bounded_bytes = 0, unbounded_bytes = 0;
    {
        Bounded value;
        BoundedReset( value );
        value.items_count = 3;
        for ( int i = 0; i < 3; i++ ) { value.items[i].v = 7 * ( i + 1 ); }
        value.tag = 42;
        bounded_bytes = BoundedSave( value, bounded_wire, sizeof( bounded_wire ) );
        CHECK_EQ( BoundedMeasure( value ), bounded_bytes );
    }
    {
        UnboundedBuilder b;
        Unbounded * value = b.GetRoot();
        for ( int i = 0; i < 3; i++ ) { UnboundedItemsAdd( b.main, value->items )->v = 7 * ( i + 1 ); }
        value->tag = 42;
        unbounded_bytes = UnboundedSave( b, unbounded_wire, sizeof( unbounded_wire ) );
        CHECK_EQ( UnboundedMeasure( b ), unbounded_bytes );
    }
    CHECK_EQ( unbounded_bytes, bounded_bytes );
    CHECK( memcmp( bounded_wire, unbounded_wire, (size_t) bounded_bytes ) == 0 );
    pin_golden( "list_migrates", unbounded_wire, unbounded_bytes );

    // each reads the other's wire silently, into equal values
    {
        Bounded back;
        TableReport r;
        CHECK( BoundedLoad( back, unbounded_wire, unbounded_bytes, &r ) );
        report_silent( r, "the bounded reader over the unbounded wire" );
        CHECK_EQ( back.items_count, 3 );
        CHECK_EQ( back.items[2].v, 21 );
        CHECK_EQ( back.tag, 42 );
    }
    {
        const int64_t need = UnboundedLoadMeasure( bounded_wire, bounded_bytes );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region == NULL ) { return; }
        TableReport r;
        const Unbounded * back = UnboundedLoad( region, need, bounded_wire, bounded_bytes, &r );
        CHECK( back != NULL );
        report_silent( r, "the unbounded reader over the bounded wire" );
        if ( back != NULL )
        {
            CHECK_EQ( back->items.size(), 3 );
            if ( back->items.size() == 3 ) { CHECK_EQ( back->items[2].v, 21 ); }
            CHECK_EQ( back->tag, 42 );
        }
        free( region );
    }
}

// ---- the TEXT form (docs/SPEC-TABLES.md §2.9, §16) ----

static void test_text()
{
    SaveBuilder b;
    build_save( b );
    CHECK( b.Lock() );
    const Save * locked = b.AsConst();

    const int64_t need = SaveToJsonMeasure( locked );
    CHECK( need > 0 );
    char * text = (char *) MEASURED_CALLOC( need, 1 );
    if ( text == NULL ) { return; }
    const int64_t written = SaveToJson( locked, text, need );
    CHECK_EQ( written, need );

    // A JSON ARRAY, in INDEX order, the pointer row per element of a []*T
    CHECK( strstr( text, "\"placements\": [" ) != NULL );
    CHECK( strstr( text, "\"scores\": [" ) != NULL );
    CHECK( strstr( text, "&node" ) != NULL );
    CHECK( strstr( text, "null" ) != NULL );
    const char * ten = strstr( text, "10" );
    const char * thirty = strstr( text, "30" );
    CHECK( ten != NULL && thirty != NULL && ten < thirty );

    // and the text reads back: one instance, one text, both ways
    SaveBuilder into;
    TableReport report;
    CHECK( SaveFromJson( into, text, written, &report ) );
    report_silent( report, "list_tables from text" );
    CHECK_EQ( into.GetRoot()->placements.count, 3 );
    CHECK_EQ( into.GetRoot()->log.count, 3 );
    CHECK_EQ( into.GetRoot()->scores.count, 3 );
    CHECK( into.Lock() );
    const int64_t again_bytes = SaveToJsonMeasure( into.AsConst() );
    char * again = (char *) MEASURED_CALLOC( again_bytes, 1 );
    if ( again == NULL ) { free( text ); return; }
    CHECK_EQ( SaveToJson( into.AsConst(), again, again_bytes ), again_bytes );
    CHECK_EQ( again_bytes, written );
    CHECK( memcmp( again, text, (size_t) written ) == 0 ); // byte-stable
    static uint8_t from_text[1u << 16];
    CHECK_EQ( SaveSave( into.AsConst(), from_text, sizeof( from_text ) ), bytes_tables );
    CHECK( memcmp( from_text, wire_tables, (size_t) bytes_tables ) == 0 );
    free( again );
    free( text );

    // `[]` is an empty list, null is kind_mismatch, a wrong-shaped element
    // counts and keeps its slot at defaults, and EVERY element the text
    // carries is read, because there is no bound to drop a tail against
    struct TextRow { const char * text; int32_t count; int32_t mismatch; int32_t after; };
    const TextRow rows[] = {
        { "{\"values\":[],\"after\":5}", 0, 0, 5 },
        { "{\"values\":null,\"after\":5}", 0, 1, 5 },
        { "{\"values\":[1,\"x\",3],\"after\":5}", 3, 1, 5 },
        { "{\"values\":[1,2],\"values\":[9],\"after\":5}", 1, 0, 5 }, // LAST WINS, whole
    };
    for ( int i = 0; i < 4; i++ )
    {
        IntsBuilder rb;
        TableReport r;
        CHECK( IntsFromJson( rb, rows[i].text, (int64_t) strlen( rows[i].text ), &r ) );
        CHECK_EQ( rb.GetRoot()->values.count, rows[i].count );
        CHECK_EQ( r.kind_mismatch, rows[i].mismatch );
        CHECK_EQ( r.clamped, 0 );
        CHECK_EQ( rb.GetRoot()->after, rows[i].after );
    }
    {
        static char many[8192];
        int at = snprintf( many, sizeof( many ), "{\"values\":[" );
        for ( int i = 0; i < 300; i++ ) { at += snprintf( many + at, sizeof( many ) - (size_t) at, "%s%d", i > 0 ? "," : "", i ); }
        snprintf( many + at, sizeof( many ) - (size_t) at, "]}" );
        IntsBuilder rb;
        TableReport r;
        CHECK( IntsFromJson( rb, many, (int64_t) strlen( many ), &r ) );
        report_silent( r, "300 elements from text" );
        CHECK_EQ( rb.GetRoot()->values.count, 300 );
        int32_t i = 0;
        for ( const int32_t * v : IntsValuesEach( rb.arena, rb.GetRoot()->values ) ) { if ( *v != i ) { failures++; printf( "FAIL element %d read as %d\n", i, *v ); break; } i++; }
    }
}

// ---- the reading path allocates nothing (§2.9, §6.5) ----

static void test_allocation_audit()
{
    const long long before = allocations;
    const int64_t need = SaveLoadMeasure( wire_tables, bytes_tables );
    CHECK( need > 0 );
    uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
    if ( region == NULL ) { return; }
    TableReport r;
    const Save * loaded = SaveLoad( region, need, wire_tables, bytes_tables, &r );
    CHECK( loaded != NULL );
    long long sum = 0;
    if ( loaded != NULL )
    {
        for ( int32_t i = 0; i < loaded->placements.size(); i++ ) { sum += loaded->placements[i].model; }
        for ( const Placement & p : loaded->placements ) { sum += p.model; }
        for ( const LogEntry * e : loaded->log ) { if ( e != NULL ) { sum += e->tick; } }
        for ( int32_t i = 0; i < loaded->scores.size(); i++ ) { sum += loaded->scores[i]; }
    }
    CHECK( sum > 0 );
    const int64_t cook_bytes = SaveCookMeasure( loaded );
    void * cooked = MEASURED_CALLOC( cook_bytes, 0 );
    if ( cooked != NULL )
    {
        CHECK( SaveCook( loaded, cooked, (uint64_t) cook_bytes, host_byte_order() ) );
        const Save * opened = SaveOpen( cooked, (uint64_t) cook_bytes );
        CHECK( opened != NULL );
        if ( opened != NULL ) { for ( const Placement & p : opened->placements ) { sum += p.model; } }
        free( cooked );
    }
    free( region );
    CHECK_EQ( allocations - before, 0 ); // not one operator new on the reading path
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
        printf( "list measure refusals: six -1s with their reasons, two clean measures, no counter moved (docs/SPEC-TABLES.md §2.8, §2.9, §6.5)\n" );
        return 0;
    }
    test_writer();
    test_builder();
    test_const_forms();
    test_reader();
    test_clamp_control();
    test_measure_refusals();
    test_shared();
    test_nested();
    test_migrates();
    test_text();
    test_allocation_audit();

    if ( failures != 0 )
    {
        printf( "\n%d list check(s) failed\n", failures );
        return 1;
    }
    printf( "lists: all checks passed (docs/SPEC-TABLES.md §2.9)\n" );
    return 0;
}
