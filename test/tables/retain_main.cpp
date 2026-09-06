// THE RETAIN-UNKNOWN GATE (docs/SPEC-TABLES.md §6.6). One binary over the
// RT1/RT2/RT3 set: RT2 and RT3 are newer builds, RT1 is the build that cannot
// name what they wrote, and every row below is one of §6.6's own: the round
// trip at three depths, idempotence, the six excluded classes, the two
// capacities, the never-clobber condition, the step pair under a map, an
// unbounded array element and a union arm, and the resolving walk's verdict on
// damage inside sound outer framing.
//
// Every row says what sabotage turns it red.
//
//   schema_test_retain

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <chrono>
#include <new>

#include "RT1Table.h"
#include "RT2Table.h"
#include "RT3Table.h"
#include "wirebuilder.h"

static int failures = 0;

// THE ALLOCATION AUDIT (docs/SPEC-TABLES.md §6.6): retention allocates
// nothing, in any port. The bytes and the id list are both the caller's, both
// declared, and neither grows. CONTROL: an allocation planted in the capture
// or in the tail turns this red.
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

#define CHECK( c ) check( ( c ), #c, __LINE__ )

static void check( bool ok, const char * what, int line )
{
    if ( ok ) { return; }
    printf( "FAIL line %d: %s\n", line, what );
    failures++;
}

// ---- the wire RT2 writes, and the region RT1 loads it into ----

struct Region
{
    uint8_t * base = NULL;
    int64_t bytes = 0;
    uint8_t * raw = NULL;

    ~Region() { free( raw ); }

    void size( int64_t needed )
    {
        free( raw );
        raw = (uint8_t *) malloc( (size_t) needed + 16 );
        base = (uint8_t *) ( ( (uintptr_t) raw + 15 ) & ~(uintptr_t) 15 );
        bytes = needed;
    }
};

// ONE RT2 INSTANCE, with an unknown field at THREE DEPTHS: `extra` in the root
// body, `future` in the nested `inner` body, and `future` again inside an
// element of `items`, a slot of `banks`, an entry of `entries`, an element of
// `list` and the union's own arm body.
static int64_t write_rt2( uint8_t * buffer, int64_t capacity, bool with_excluded )
{
    tblrt2::NodeBuilder builder;
    tblrt2::Node * root = builder.GetRoot();
    memcpy( root->name, "world", 6 );
    root->name_length = 5;
    root->tier = tblrt2::Slot::Extra; // a variant RT1 cannot name

    root->extra = 11;
    root->inner.hits = 3;
    root->inner.future = 21;
    memcpy( root->inner.tag, "in", 3 );
    root->inner.tag_length = 2;

    root->items_count = 2;
    root->items[0].hits = 1; root->items[0].future = 31;
    root->items[1].hits = 2; root->items[1].future = 32;

    root->banks.slots[0].hits = 4; root->banks.slots[0].future = 41;
    root->banks.slots[2].hits = 6; root->banks.slots[2].future = 43; // the Extra slot

    root->pick.type = tblrt2::PickType::Alpha;
    tblrt2::InnerReset( root->pick.alpha ); // an arm's payload is overlay storage: the tag is not a value until it is set
    root->pick.alpha.hits = 5;
    root->pick.alpha.future = 51;

    tblrt2::TableSlot<tblrt2::Leaf> leaf = builder.Alloc<tblrt2::Leaf>();
    leaf->value = 77;
    root->head = leaf;

    tblrt2::Inner * one = tblrt2::NodeEntriesInsert( builder.main, root->entries, "k1" );
    if ( one != NULL ) { one->hits = 7; one->future = 61; }

    tblrt2::Inner * item = tblrt2::NodeListAdd( builder.main, root->list );
    if ( item != NULL ) { item->hits = 8; item->future = 71; }

    if ( with_excluded )
    {
        tblrt2::TableSlot<tblrt2::Leaf> ghost = builder.Alloc<tblrt2::Leaf>();
        ghost->value = 88;
        root->ghost = ghost;            // a field of kind 17
        root->ghosts_count = 1;
        root->ghosts[0] = ghost;        // an array whose element kind is 17
        root->outer.plain = 99;         // an unknown scalar in the outer field
        tblrt2::TableSlot<tblrt2::Leaf> deep = builder.Alloc<tblrt2::Leaf>();
        root->outer.mid.deeper.link = deep; // and a 17 three bodies down
    }

    if ( !builder.Lock() ) { return -1; }
    const int64_t n = tblrt2::NodeMeasure( builder );
    if ( n < 0 || n > capacity ) { return -1; }
    return tblrt2::NodeSave( builder, buffer, capacity );
}

// ---- the rows ----

static void round_trip()
{
    uint8_t wire[ 8192 ];
    const int64_t n = write_rt2( wire, sizeof( wire ), false );
    CHECK( n > 0 );

    Region region;
    region.size( tblrt1::NodeLoadMeasure( wire, n ) );

    uint8_t storage[ 8192 ];
    tblrt1::TableRetain::Id ids[ 1024 ];
    tblrt1::TableRetain retain;
    retain.bytes = storage;
    retain.capacity = sizeof( storage );
    retain.ids = ids;
    retain.id_capacity = 1024;

    tblrt1::TableReport report;
    const long long before = allocations;
    const tblrt1::Node * root = tblrt1::NodeLoadRetain( region.base, region.bytes, wire, n, &retain, &report );
    CHECK( root != NULL );
    CHECK( !report.malformed );
    // EIGHT BODIES carry an unknown FIELD: the root, `inner`, two elements of
    // `items`, a slot of `banks`, the union's arm body, a map entry's value and
    // a list element, and each is retained where it sat.
    CHECK( report.retained == 8 );
    // TWO MORE UNKNOWNS are vocabulary rather than fields: the enum variant
    // `tier` names and the keyed slot RT2's third variant writes. Each is an
    // EXCLUDED CLASS and each counts one retain_lost (§6.6).
    CHECK( report.unknown == 10 );
    CHECK( report.retain_lost == 2 );
    CHECK( allocations == before );   // RETENTION ALLOCATES NOTHING (§6.6)

    tblrt1::TableReport save_report;
    const int64_t m = tblrt1::NodeMeasureRetain( root, &retain );
    CHECK( m > 0 );
    uint8_t out[ 8192 ];
    const int64_t w = tblrt1::NodeSaveRetain( root, &retain, out, m, &save_report );
    CHECK( w == m ); // MEASURE AND SAVE DROP THE SAME RECORDS UNDER THE SAME WALK
    CHECK( save_report.retain_lost == 0 );

    // THE SAVE IS BIGGER THAN THE PLAIN ONE, because the retained fields rode
    uint8_t plain[ 8192 ];
    const int64_t p = tblrt1::NodeSave( root, plain, sizeof( plain ) );
    CHECK( p > 0 && w > p );

    // AND RT2 READS ITS OWN FIELDS BACK, in the bodies they came from
    Region back;
    back.size( tblrt2::NodeLoadMeasure( out, w ) );
    tblrt2::TableReport read_report;
    const tblrt2::Node * reread = tblrt2::NodeLoad( back.base, back.bytes, out, w, &read_report );
    CHECK( reread != NULL );
    CHECK( !read_report.malformed );
    CHECK( reread->extra == 11 );
    CHECK( reread->inner.future == 21 );
    CHECK( reread->items_count == 2 && reread->items[0].future == 31 && reread->items[1].future == 32 );
    CHECK( reread->banks.slots[0].future == 41 );
    CHECK( reread->pick.type == tblrt2::PickType::Alpha && reread->pick.alpha.future == 51 );

    // IDEMPOTENCE: the same region saved twice is the same bytes, and a second
    // round trip through RT1 reproduces the first save exactly.
    uint8_t out2[ 8192 ];
    tblrt1::TableReport second;
    const int64_t w2 = tblrt1::NodeSaveRetain( root, &retain, out2, sizeof( out2 ), &second );
    CHECK( w2 == w && memcmp( out, out2, (size_t) w ) == 0 );

    Region again_region;
    again_region.size( tblrt1::NodeLoadMeasure( out, w ) );
    uint8_t storage2[ 8192 ];
    tblrt1::TableRetain::Id ids2[ 1024 ];
    tblrt1::TableRetain retain2;
    retain2.bytes = storage2;
    retain2.capacity = sizeof( storage2 );
    retain2.ids = ids2;
    retain2.id_capacity = 1024;
    tblrt1::TableReport third;
    const tblrt1::Node * root2 = tblrt1::NodeLoadRetain( again_region.base, again_region.bytes, out, w, &retain2, &third );
    CHECK( root2 != NULL );
    uint8_t out3[ 8192 ];
    tblrt1::TableReport fourth;
    const int64_t w3 = tblrt1::NodeSaveRetain( root2, &retain2, out3, sizeof( out3 ), &fourth );
    CHECK( w3 == w && memcmp( out, out3, (size_t) w ) == 0 );
}


// ---- THE SIX EXCLUDED CLASSES (docs/SPEC-TABLES.md §6.6) ----
//
// The table is the law and its rows are the count. Each row below is one
// class, and each pins retain_lost where the class is the only thing on the
// wire this reader cannot keep.

// A helper: load one RT2 wire under RT1 with retention on, and hand back the
// report and the region.
struct Loaded
{
    Region region;
    uint8_t storage[ 8192 ];
    tblrt1::TableRetain::Id ids[ 1024 ];
    tblrt1::TableRetain retain;
    tblrt1::TableReport report;
    const tblrt1::Node * root = NULL;

    void load( const uint8_t * wire, int64_t n )
    {
        region.size( tblrt1::NodeLoadMeasure( wire, n ) );
        retain.bytes = storage;
        retain.capacity = sizeof( storage );
        retain.ids = ids;
        retain.id_capacity = 1024;
        root = tblrt1::NodeLoadRetain( region.base, region.bytes, wire, n, &retain, &report );
    }
};

// ONE RT2 instance carrying exactly one excluded class and nothing else this
// reader cannot name. `which` picks the class.
static int64_t write_excluded( uint8_t * buffer, int64_t capacity, int which )
{
    tblrt2::NodeBuilder builder;
    tblrt2::Node * root = builder.GetRoot();
    root->inner.hits = 1;
    switch ( which )
    {
        case 0: // A FIELD OF KIND 17: a node index this reader neither keeps nor retains
        {
            tblrt2::TableSlot<tblrt2::Leaf> ghost = builder.Alloc<tblrt2::Leaf>();
            ghost->value = 88;
            root->ghost = ghost;
            break;
        }
        case 1: // AN ARRAY whose element kind is 17
        {
            tblrt2::TableSlot<tblrt2::Leaf> ghost = builder.Alloc<tblrt2::Leaf>();
            ghost->value = 88;
            root->ghosts_count = 1;
            root->ghosts[0] = ghost;
            break;
        }
        case 2: // A TABLE whose recursively walked payload meets a 17 three bodies
                // down, beside an unknown SCALAR in that same outer field. The
                // WHOLE record goes, and the sibling with it.
        {
            tblrt2::TableSlot<tblrt2::Leaf> deep = builder.Alloc<tblrt2::Leaf>();
            deep->value = 5;
            root->outer.plain = 99;
            root->outer.mid.deeper.link = deep;
            break;
        }
        case 3: root->tier = tblrt2::Slot::Extra; break;                 // an unknown ENUM VARIANT
        case 4: root->pick.type = tblrt2::PickType::Gamma;                // an unknown UNION ARM
                root->pick.gamma = 3; break;
        case 5: tblrt2::InnerReset( root->banks.slots[2] );               // an unknown KEYED SLOT
                root->banks.slots[2].hits = 6; break;
        default: break;
    }
    if ( !builder.Lock() ) { return -1; }
    return tblrt2::NodeSave( builder, buffer, capacity );
}

static void excluded_classes()
{
    // the node-index class, its array form, and the RECURSIVE shape: each one
    // record dropped, each counted ONCE, and never a partial one
    static const int32_t retained_of[] = { 0, 0, 0, 0, 0, 0 };
    for ( int which = 0; which < 6; which++ )
    {
        uint8_t wire[ 8192 ];
        const int64_t n = write_excluded( wire, sizeof( wire ), which );
        CHECK( n > 0 );
        Loaded l;
        l.load( wire, n );
        CHECK( l.root != NULL );
        CHECK( !l.report.malformed );
        if ( l.report.retain_lost != 1 || l.report.retained != retained_of[ which ] )
        {
            printf( "FAIL excluded class %d: retained=%d retain_lost=%d\n", which, l.report.retained, l.report.retain_lost );
            failures++;
        }
        // and the save carries nothing of it
        tblrt1::TableReport save_report;
        uint8_t out[ 8192 ];
        const int64_t m = tblrt1::NodeMeasureRetain( l.root, &l.retain );
        const int64_t w = tblrt1::NodeSaveRetain( l.root, &l.retain, out, m, &save_report );
        CHECK( w == m && w > 0 );
        CHECK( save_report.retain_lost == 0 ); // the load already counted it, and the save has nothing to place
    }
}

// A NODE RECORD whose type id this reader cannot name is the sixth class, and
// RT3 is what isolates it: `head` is the field RT1 already has, at the same id
// and the same kind, pointing at a table RT1 never heard of.
static void unknown_node_record()
{
    uint8_t wire[ 4096 ];
    int64_t n = 0;
    {
        tblrt3::NodeBuilder builder;
        tblrt3::Node * root = builder.GetRoot();
        memcpy( root->name, "beam", 5 );
        root->name_length = 4;
        tblrt3::TableSlot<tblrt3::Beam> beam = builder.Alloc<tblrt3::Beam>();
        beam->value = 3;
        root->head = beam;
        CHECK( builder.Lock() );
        n = tblrt3::NodeSave( builder, wire, sizeof( wire ) );
    }
    CHECK( n > 0 );
    Loaded l;
    l.load( wire, n );
    CHECK( l.root != NULL );
    CHECK( !l.report.malformed );
    CHECK( l.report.unknown == 1 );     // the record, counted once and not once per pointer
    CHECK( l.report.retain_lost == 1 ); // a whole node: nothing to append it to
    CHECK( l.report.retained == 0 );
}


// ---- THE TWO CAPACITIES (docs/SPEC-TABLES.md §6.6) ----
//
// REFUSAL IS PER RECORD AND NEVER PARTIAL: a record the remaining capacity
// cannot hold whole is not written at all, retain_lost counts one, the read
// continues, and the buffer never holds a truncated field.
static void capacities()
{
    uint8_t wire[ 8192 ];
    const int64_t n = write_rt2( wire, sizeof( wire ), false );
    CHECK( n > 0 );

    Loaded full;
    full.load( wire, n );
    CHECK( full.root != NULL );
    const int64_t used = full.retain.used;
    const int32_t records = full.retain.count;
    CHECK( records == 8 );

    // ONE BYTE SHORT OF THE LAST RECORD
    {
        Region region;
        region.size( tblrt1::NodeLoadMeasure( wire, n ) );
        uint8_t storage[ 8192 ];
        tblrt1::TableRetain::Id ids[ 1024 ];
        tblrt1::TableRetain retain;
        retain.bytes = storage;
        retain.capacity = used - 1;
        retain.ids = ids;
        retain.id_capacity = 1024;
        tblrt1::TableReport report;
        const tblrt1::Node * root = tblrt1::NodeLoadRetain( region.base, region.bytes, wire, n, &retain, &report );
        CHECK( root != NULL );
        CHECK( report.retained == records - 1 );
        CHECK( report.retain_lost == 3 ); // the two excluded vocabularies, and the record with no room
        // THE READ'S OWN COUNTERS ARE UNMOVED: a full buffer degrades to the
        // default behavior one field at a time
        CHECK( report.unknown == 10 );
        CHECK( report.kind_mismatch == 0 && report.clamped == 0 && report.widened == 0 );
        CHECK( !report.malformed );
        CHECK( retain.used <= used - 1 ); // and the truncated record was never written
    }

    // AN ID LIST ONE ENTRY SHORT of the retained ids the wire carries. The
    // record whose id has no entry is dropped, the save answers the size the
    // measure gave, and the save is NEVER REFUSED.
    {
        Region region;
        region.size( tblrt1::NodeLoadMeasure( wire, n ) );
        uint8_t storage[ 8192 ];
        tblrt1::TableRetain::Id ids[ 2 ];
        tblrt1::TableRetain retain;
        retain.bytes = storage;
        retain.capacity = sizeof( storage );
        retain.ids = ids;
        retain.id_capacity = 1; // the wire carries TWO distinct retained ids
        tblrt1::TableReport report;
        const tblrt1::Node * root = tblrt1::NodeLoadRetain( region.base, region.bytes, wire, n, &retain, &report );
        CHECK( root != NULL );
        CHECK( report.retained == records ); // LOAD writes into neither list
        tblrt1::TableReport save_report;
        const int64_t m = tblrt1::NodeMeasureRetain( root, &retain );
        CHECK( m > 0 );
        uint8_t out[ 8192 ];
        const int64_t w = tblrt1::NodeSaveRetain( root, &retain, out, m, &save_report );
        CHECK( w == m );                   // the measure saw the same overflow
        CHECK( save_report.retain_lost > 0 ); // and the records with no entry were dropped
        CHECK( retain.id_used == 1 );
        // the file still reads, and the other retained id rode
        Region back;
        back.size( tblrt2::NodeLoadMeasure( out, w ) );
        tblrt2::TableReport read_report;
        const tblrt2::Node * reread = tblrt2::NodeLoad( back.base, back.bytes, out, w, &read_report );
        CHECK( reread != NULL && !read_report.malformed );
    }
}

// ---- RECORD LIFETIME (docs/SPEC-TABLES.md §6.6) ----
//
// A body carrying `inner { future = 7 }` and then `inner { hits = 2 }`: the
// second occurrence resets the first, the save carries `hits = 2` and no
// `future`, retain_lost is 0, and retained is unchanged by the discard.
static void record_lifetime()
{
    WireBuilder b;
    b.field( "inner", 13 );
    const int64_t first = b.open_len();
    b.field( "future", 4 );
    b.u32( 7 );
    b.end();
    b.close_len( first );
    b.field( "inner", 13 );
    const int64_t second = b.open_len();
    b.field( "hits", 4 );
    b.u32( 2 );
    b.end();
    b.close_len( second );
    b.end();
    uint8_t wire[ 4096 ];
    const int64_t n = b.finish( wire );

    Loaded l;
    l.load( wire, n );
    CHECK( l.root != NULL );
    CHECK( !l.report.malformed );
    CHECK( l.root->inner.hits == 2 );
    CHECK( l.report.unknown == 1 );
    CHECK( l.report.retained == 1 );    // counted when its bytes were kept
    CHECK( l.report.retain_lost == 0 ); // and a reset is the writer's act, not a loss
    CHECK( l.retain.count == 0 );       // the record went with the occurrence that carried it

    tblrt1::TableReport save_report;
    uint8_t out[ 4096 ];
    const int64_t m = tblrt1::NodeMeasureRetain( l.root, &l.retain );
    const int64_t w = tblrt1::NodeSaveRetain( l.root, &l.retain, out, m, &save_report );
    CHECK( w == m && w > 0 );
    CHECK( save_report.retain_lost == 0 );

    Region back;
    back.size( tblrt2::NodeLoadMeasure( out, w ) );
    tblrt2::TableReport read_report;
    const tblrt2::Node * reread = tblrt2::NodeLoad( back.base, back.bytes, out, w, &read_report );
    CHECK( reread != NULL );
    CHECK( reread->inner.hits == 2 );
    CHECK( reread->inner.future == 0 ); // and `7` was never resurrected beside it
}

// ---- THE RESOLVING WALK'S VERDICT (docs/SPEC-TABLES.md §6.6) ----
//
// A retained field whose INNER structure is damaged inside sound outer
// framing: retain_lost at one, malformed at ZERO, and every sibling field's
// value intact. Retention can lose a field; it can never turn a good read into
// a bad one.
static void damaged_inner_structure()
{
    WireBuilder b;
    b.field( "name", 12 ); // a sibling the reader does name, before the damage
    b.leb( 5 );
    b.raw( "world", 5 );
    b.field( "future", 13 ); // an unknown TABLE whose body never reaches a terminator
    const int64_t at = b.open_len();
    b.leb( b.ref( "deeper" ) );
    b.u8( 4 );
    b.u32( 1 ); // and the body ends here, with no zero reference: the L is sound
    b.close_len( at );
    b.end();
    uint8_t wire[ 4096 ];
    const int64_t n = b.finish( wire );

    Loaded l;
    l.load( wire, n );
    CHECK( l.root != NULL );
    CHECK( !l.report.malformed ); // THE OUTER FRAMING WAS SOUND
    CHECK( l.root->name_length == 5 ); // and every sibling field's value is intact
    CHECK( l.report.unknown == 1 );
    CHECK( l.report.retained == 0 );
    CHECK( l.report.retain_lost == 1 );
}

// ---- THE WALK IS ONE PASS EACH WAY (docs/SPEC-TABLES.md §6.6) ----
//
// The resolving walk "cannot make a read fail, allocate, or take a path it
// would not otherwise take", and a cost that doubles at every level of a
// nesting THE FILE CHOOSES is such a path. Every framed length in the resolved
// form is a fixed slot the capture writes after the content it frames, so a
// record costs one pass in and one pass out, and its time is linear in its own
// bytes.
//
// CONTROL: restore a probe walk at any framed length and the rows below run
// for longer than anyone will wait.

// one unknown field whose payload is `depth` nested table bodies, with a
// scalar at the bottom
static int64_t deep_wire( uint8_t * out, int32_t depth )
{
    WireBuilder b;
    int64_t marks[ 128 ];
    b.field( "future", 13 );
    marks[0] = b.open_len();
    for ( int32_t d = 1; d < depth; d++ )
    {
        b.field( "deeper", 13 );
        marks[d] = b.open_len();
    }
    b.field( "hits", 4 );
    b.u32( 9 );
    for ( int32_t d = depth - 1; d >= 0; d-- ) { b.end(); b.close_len( marks[d] ); }
    b.end();
    return b.finish( out );
}

static void walk_is_linear()
{
    static const int32_t depths[] = { 8, 20, 40 };
    for ( int32_t i = 0; i < 3; i++ )
    {
        uint8_t wire[ 4096 ];
        const int64_t n = deep_wire( wire, depths[i] );
        CHECK( n > 0 );
        Region region;
        region.size( tblrt1::NodeLoadMeasure( wire, n ) );
        uint8_t storage[ 8192 ];
        tblrt1::TableRetain::Id ids[ 64 ];
        tblrt1::TableRetain retain;
        retain.bytes = storage;
        retain.capacity = sizeof( storage );
        retain.ids = ids;
        retain.id_capacity = 64;
        tblrt1::TableReport report;
        const std::chrono::steady_clock::time_point began = std::chrono::steady_clock::now();
        const tblrt1::Node * root = tblrt1::NodeLoadRetain( region.base, region.bytes, wire, n, &retain, &report );
        const double ms = std::chrono::duration<double, std::milli>( std::chrono::steady_clock::now() - began ).count();
        CHECK( root != NULL );
        CHECK( !report.malformed );
        CHECK( report.retained == 1 );
        CHECK( report.retain_lost == 0 );
        // AN ABSOLUTE BOUND, and not a ratio to noise: a walk linear in the
        // record's bytes is microseconds at every depth the cap admits.
        if ( ms > 50.0 )
        {
            printf( "FAIL walk depth %d: %.3f ms\n", depths[i], ms );
            failures++;
        }
        printf( "retain: walk depth %d, %.3f ms\n", depths[i], ms );

        // and the record rides back out, which is the save side's own pass
        tblrt1::TableReport save_report;
        uint8_t save[ 8192 ];
        const int64_t m = tblrt1::NodeMeasureRetain( root, &retain );
        const int64_t w = tblrt1::NodeSaveRetain( root, &retain, save, m, &save_report );
        CHECK( w == m && w > 0 );
        CHECK( save_report.retain_lost == 0 );
    }

    // THE CAP IS A SMALL STATED CONSTANT and it counts NESTED BODIES: a record
    // one past it is dropped on the same rule as any other shape the walk
    // cannot take, and the last depth it admits still rides.
    {
        uint8_t wire[ 4096 ];
        const int64_t n = deep_wire( wire, 65 );
        Loaded l;
        l.load( wire, n );
        CHECK( l.root != NULL );
        CHECK( !l.report.malformed );
        CHECK( l.report.retained == 0 );
        CHECK( l.report.retain_lost == 1 );
    }
    {
        uint8_t wire[ 4096 ];
        const int64_t n = deep_wire( wire, 64 );
        Loaded l;
        l.load( wire, n );
        CHECK( l.root != NULL );
        CHECK( !l.report.malformed );
        CHECK( l.report.retained == 1 );
        CHECK( l.report.retain_lost == 0 );
    }
}

// ---- THE TRAILER IS PERMUTED, AND EVERY REFERENCE MOVES WITH IT ----
//
// Moving a field changes the order ids are first used in, and the id table is
// in FIRST-USE ORDER over the whole wire (§3), so the trailer's entries are
// permuted and every reference in the body is renumbered with them. A retained
// record that had been copied VERBATIM would point at other names, silently,
// which is what the resolved form exists to prevent, and what RT2 reading its
// own values back out of the permuted file proves.
static void trailer_is_permuted()
{
    uint8_t wire[ 8192 ];
    const int64_t n = write_rt2( wire, sizeof( wire ), false );
    Loaded l;
    l.load( wire, n );
    CHECK( l.root != NULL );
    uint8_t out[ 8192 ];
    tblrt1::TableReport save_report;
    const int64_t m = tblrt1::NodeMeasureRetain( l.root, &l.retain );
    const int64_t w = tblrt1::NodeSaveRetain( l.root, &l.retain, out, m, &save_report );
    CHECK( w == m );

    // the two trailers, read from the END of each file (§3)
    uint64_t in_count = 0, out_count = 0;
    for ( int i = 7; i >= 0; i-- ) { in_count = ( in_count << 8 ) | wire[ n - 8 + i ]; }
    for ( int i = 7; i >= 0; i-- ) { out_count = ( out_count << 8 ) | out[ w - 8 + i ]; }
    const uint8_t * in_first = wire + n - (int64_t) in_count * 8 - 8;
    const uint8_t * out_first = out + w - (int64_t) out_count * 8 - 8;
    bool same_order = in_count == out_count;
    if ( same_order ) { same_order = memcmp( in_first, out_first, (size_t) in_count * 8 ) == 0; }
    CHECK( !same_order ); // the move permuted it, which is what makes the check real
}

int main( int argc, char ** argv )
{
    (void) argc;
    (void) argv;
    round_trip();
    excluded_classes();
    unknown_node_record();
    capacities();
    record_lifetime();
    damaged_inner_structure();
    walk_is_linear();
    trailer_is_permuted();
    if ( failures != 0 )
    {
        printf( "retain: %d failure(s)\n", failures );
        return 1;
    }
    printf( "retain: ok\n" );
    return 0;
}
