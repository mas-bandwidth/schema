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
// `list` and the union's own arm body. `parcel` is the whole table RT1 cannot
// name, and its body is what the RESOLVING WALK is measured on: an enum's
// variant reference, a union's arm, an enum-keyed body's keys, a nested table
// body and an array of table bodies.
static int64_t write_rt2( uint8_t * buffer, int64_t capacity )
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

    root->parcel.grade = tblrt2::Grade::Silver;         // kind 30, a variant reference
    root->parcel.fit.type = tblrt2::FittingType::Bolt;  // kind 15, a table arm
    tblrt2::BoltReset( root->parcel.fit.bolt );
    root->parcel.fit.bolt.weight = 101;
    root->parcel.bins.slots[0].weight = 102;            // kind 16, keyed by Grade
    root->parcel.bins.slots[1].weight = 103;
    root->parcel.core.weight = 104;                     // kind 13, a nested body
    root->parcel.stack_count = 2;                       // kind 14, table elements
    root->parcel.stack[0].weight = 105;
    root->parcel.stack[1].weight = 106;

    if ( !builder.Lock() ) { return -1; }
    const int64_t n = tblrt2::NodeMeasure( builder );
    if ( n < 0 || n > capacity ) { return -1; }
    return tblrt2::NodeSave( builder, buffer, capacity );
}

// THE ENTRY ONE ID TAKES in a saved file's trailer, counted from zero, and -1
// for an id the file never named. The table is in FIRST-USE ORDER over the
// whole wire (§3), so an entry's position is the order the walk reached it in.
static int32_t id_slot( const uint8_t * wire, int64_t bytes, uint64_t id )
{
    uint64_t count = 0;
    for ( int i = 7; i >= 0; i-- ) { count = ( count << 8 ) | wire[ bytes - 8 + i ]; }
    const uint8_t * first = wire + bytes - (int64_t) count * 8 - 8;
    for ( uint64_t i = 0; i < count; i++ )
    {
        uint64_t entry = 0;
        for ( int k = 7; k >= 0; k-- ) { entry = ( entry << 8 ) | first[ i * 8 + k ]; }
        if ( entry == id ) { return (int32_t) i; }
    }
    return -1;
}

// ---- the rows ----

static void round_trip()
{
    uint8_t wire[ 8192 ];
    const int64_t n = write_rt2( wire, sizeof( wire ) );
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
    // a list element, and each is retained where it sat. The root carries a
    // SECOND unknown field, `parcel`, whose whole table body the resolving walk
    // reads: nine records.
    CHECK( report.retained == 9 );
    // TWO MORE UNKNOWNS are vocabulary rather than fields: the enum variant
    // `tier` names and the keyed slot RT2's third variant writes. Each is an
    // EXCLUDED CLASS and each counts one retain_lost (§6.6).
    CHECK( report.unknown == 11 );
    CHECK( report.retain_lost == 2 );
    CHECK( allocations == before );   // RETENTION ALLOCATES NOTHING (§6.6)
    // and the audit BRACKETS THE WHOLE FAMILY, not the load alone: the measure
    // and the save are checked against this same mark at the end of the row.

    tblrt1::TableReport save_report;
    const int64_t m = tblrt1::NodeMeasureRetain( root, &retain );
    CHECK( m > 0 );
    uint8_t out[ 8192 ];
    const int64_t w = tblrt1::NodeSaveRetain( root, &retain, out, m, &save_report );
    CHECK( w == m ); // MEASURE AND SAVE DROP THE SAME RECORDS UNDER THE SAME WALK
    CHECK( save_report.retain_lost == 0 );
    // THE FIRST SAVE, PINNED AS A BYTE STRING (§6.6). Loaded and saved once,
    // the unknown fields have moved to the end of each body and the trailer is
    // permuted, so the first save does NOT equal the original: it is a golden
    // like any other rather than a comparison modulo the move. It pins the
    // resolved form end to end, every record's placement with it.
    static const uint8_t kFirstSave[] = {
        0x01, 0x01, 0x0c, 0x05, 0x77, 0x6f, 0x72, 0x6c, 0x64, 0x02, 0x0d, 0x12,
        0x03, 0x0c, 0x02, 0x69, 0x6e, 0x04, 0x04, 0x03, 0x00, 0x00, 0x00, 0x05,
        0x04, 0x15, 0x00, 0x00, 0x00, 0x00, 0x06, 0x0e, 0x1e, 0x0d, 0x02, 0x0d,
        0x04, 0x04, 0x01, 0x00, 0x00, 0x00, 0x05, 0x04, 0x1f, 0x00, 0x00, 0x00,
        0x00, 0x0d, 0x04, 0x04, 0x02, 0x00, 0x00, 0x00, 0x05, 0x04, 0x20, 0x00,
        0x00, 0x00, 0x00, 0x07, 0x10, 0x11, 0x0d, 0x01, 0x08, 0x0d, 0x04, 0x04,
        0x04, 0x00, 0x00, 0x00, 0x05, 0x04, 0x29, 0x00, 0x00, 0x00, 0x00, 0x09,
        0x0f, 0x0a, 0x0d, 0x0d, 0x04, 0x04, 0x05, 0x00, 0x00, 0x00, 0x05, 0x04,
        0x33, 0x00, 0x00, 0x00, 0x00, 0x0b, 0x11, 0x02, 0x0c, 0x0e, 0x19, 0x0d,
        0x01, 0x16, 0x0d, 0x0c, 0x02, 0x6b, 0x31, 0x0e, 0x0d, 0x0d, 0x04, 0x04,
        0x07, 0x00, 0x00, 0x00, 0x05, 0x04, 0x3d, 0x00, 0x00, 0x00, 0x00, 0x00,
        0x0f, 0x0e, 0x10, 0x0d, 0x01, 0x0d, 0x04, 0x04, 0x08, 0x00, 0x00, 0x00,
        0x05, 0x04, 0x47, 0x00, 0x00, 0x00, 0x00, 0x10, 0x04, 0x0b, 0x00, 0x00,
        0x00, 0x11, 0x0d, 0x46, 0x12, 0x1e, 0x13, 0x14, 0x0f, 0x15, 0x0d, 0x07,
        0x16, 0x04, 0x65, 0x00, 0x00, 0x00, 0x00, 0x17, 0x10, 0x14, 0x0d, 0x02,
        0x18, 0x07, 0x16, 0x04, 0x66, 0x00, 0x00, 0x00, 0x00, 0x13, 0x07, 0x16,
        0x04, 0x67, 0x00, 0x00, 0x00, 0x00, 0x19, 0x0d, 0x07, 0x16, 0x04, 0x68,
        0x00, 0x00, 0x00, 0x00, 0x1a, 0x0e, 0x12, 0x0d, 0x02, 0x07, 0x16, 0x04,
        0x69, 0x00, 0x00, 0x00, 0x00, 0x07, 0x16, 0x04, 0x6a, 0x00, 0x00, 0x00,
        0x00, 0x00, 0x1b, 0x0c, 0x0a, 0x01, 0x1c, 0x07, 0x0e, 0x04, 0x4d, 0x00,
        0x00, 0x00, 0x00, 0x00, 0x86, 0x1b, 0x63, 0x8e, 0xba, 0xad, 0xbc, 0xc4,
        0x87, 0x92, 0x88, 0x5f, 0x8f, 0x06, 0x4c, 0x2d, 0xf3, 0xa4, 0x48, 0x44,
        0x19, 0xab, 0xd7, 0x56, 0xbb, 0xf0, 0x0c, 0x9b, 0xcc, 0xfb, 0x2d, 0x73,
        0x56, 0x30, 0xb3, 0xea, 0xd4, 0x61, 0x80, 0xd8, 0x6f, 0x2c, 0x41, 0x4f,
        0xbf, 0x84, 0x78, 0x3e, 0x0c, 0xe9, 0x52, 0xe1, 0x81, 0xb9, 0x9e, 0xdd,
        0xc1, 0x52, 0x85, 0xb8, 0x19, 0xa3, 0xf3, 0x24, 0xd8, 0xbb, 0x13, 0xc5,
        0x0d, 0xc4, 0x0b, 0xbf, 0x2b, 0x20, 0xed, 0x85, 0xbb, 0x25, 0xc6, 0x8a,
        0x03, 0x0c, 0x9a, 0x5f, 0xcc, 0x12, 0x8f, 0x0a, 0x53, 0xa2, 0x45, 0x08,
        0x2c, 0xa7, 0xb2, 0xc5, 0xec, 0x10, 0x5b, 0x36, 0x19, 0x4a, 0xc9, 0x3d,
        0xea, 0x0c, 0xe8, 0x30, 0x94, 0xfd, 0xe4, 0x7c, 0x41, 0x81, 0x74, 0x69,
        0xad, 0x9a, 0x77, 0xbf, 0x69, 0xcb, 0x79, 0xa9, 0x12, 0xee, 0x29, 0xfd,
        0x30, 0x9e, 0xbe, 0xe6, 0xd7, 0xf1, 0x22, 0x54, 0xd4, 0x8a, 0xc4, 0x77,
        0x29, 0x9b, 0xa8, 0x32, 0x80, 0x25, 0xf1, 0xea, 0xde, 0x1a, 0xe5, 0xc3,
        0xec, 0x34, 0xc9, 0xfe, 0x18, 0x7a, 0x9e, 0xdc, 0xc6, 0x54, 0xb2, 0xc6,
        0x9b, 0xda, 0x32, 0xcd, 0x19, 0xbf, 0x27, 0x1f, 0xde, 0xf8, 0x11, 0x69,
        0xd7, 0xbc, 0xb6, 0xd0, 0x9b, 0x3f, 0xc3, 0xde, 0x91, 0xc5, 0xc4, 0xaa,
        0x39, 0x77, 0x92, 0xc4, 0x9e, 0x75, 0x80, 0x19, 0x91, 0xfb, 0xf2, 0x0b,
        0x9f, 0x79, 0xd8, 0x45, 0xad, 0x94, 0x90, 0xee, 0xff, 0xff, 0xff, 0xff,
        0xff, 0xff, 0xff, 0xff, 0xb5, 0xd1, 0x73, 0xbb, 0xb4, 0x85, 0xc0, 0xa5,
        0x1c, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    };
    CHECK( w == (int64_t) sizeof( kFirstSave ) && memcmp( out, kFirstSave, sizeof( kFirstSave ) ) == 0 );
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
    // THE STEP PAIR UNDER A MAP AND AN UNBOUNDED ARRAY: a record retained in an
    // entry's value body and one retained in a list element come back in the
    // entry and the element they were held for, and not merely somewhere.
    const tblrt2::Inner * entry = reread->entries.Find( "k1" );
    CHECK( entry != NULL && entry->future == 61 );
    CHECK( reread->list.size() == 1 && reread->list[0].future == 71 );

    // ---- THE RESOLUTION HALF (§6.6) ----
    //
    // `parcel` is a table this reader cannot name, and every kind the walk
    // RESOLVES rather than copies is inside it. Each value below travelled as
    // a reference RT1 rewrote to an id and RT1 rewrote back to a reference of
    // its own trailer, and RT2 read it under the vocabulary it wrote.
    CHECK( reread->parcel.grade == tblrt2::Grade::Silver );          // kind 30
    CHECK( reread->parcel.fit.type == tblrt2::FittingType::Bolt );   // kind 15
    CHECK( reread->parcel.fit.bolt.weight == 101 );
    CHECK( reread->parcel.bins.slots[0].weight == 102 );             // kind 16
    CHECK( reread->parcel.bins.slots[1].weight == 103 );
    CHECK( reread->parcel.core.weight == 104 );                      // kind 13
    CHECK( reread->parcel.stack_count == 2 );                        // kind 14
    CHECK( reread->parcel.stack[0].weight == 105 );
    CHECK( reread->parcel.stack[1].weight == 106 );

    // ---- THE ID TABLE IS IN FIRST-USE ORDER, AND THE TAIL IS BEFORE THE
    // NODE TABLE (§3, §3.1, §6.6) ----
    const int32_t at_name = id_slot( out, w, field_id( "name" ) );
    const int32_t at_list = id_slot( out, w, field_id( "list" ) );
    const int32_t at_future = id_slot( out, w, field_id( "future" ) );
    const int32_t at_extra = id_slot( out, w, field_id( "extra" ) );
    const int32_t at_parcel = id_slot( out, w, field_id( "parcel" ) );
    const int32_t at_nodes = id_slot( out, w, tblrt1::kTableNodeTableFieldId );
    CHECK( at_name == 0 ); // the root's first declared field names the first entry
    // `future` interns inside `inner`'s own tail, which the walk reaches
    // before the ROOT's tail
    CHECK( at_future > at_name && at_future < at_extra );
    // a retained id enters AFTER its body's own fields, and `list` is the last
    // field the root declares
    CHECK( at_list > at_name && at_extra > at_list );
    // and the root's two records intern in the order they were retained
    CHECK( at_parcel == at_extra + 1 );
    // THE TAIL IS PINNED BEFORE THE NODE-TABLE FIELD, so its ids are first
    // used before the node table's is
    CHECK( at_nodes > at_parcel );

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

    // THE ALLOCATION AUDIT, over the WHOLE retain family and not LoadRetain
    // alone: two loads, one measure and three saves have run since the mark,
    // and none of them reached the allocator. CONTROL: an allocation planted
    // in the capture, in the measure or in the tail turns this red.
    CHECK( allocations == before );
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
    // record dropped, each counted ONCE, and never a partial one. The class is
    // the ONLY thing on the wire this reader cannot keep, so nothing is
    // retained under any of them.
    for ( int which = 0; which < 6; which++ )
    {
        uint8_t wire[ 8192 ];
        const int64_t n = write_excluded( wire, sizeof( wire ), which );
        CHECK( n > 0 );
        Loaded l;
        l.load( wire, n );
        CHECK( l.root != NULL );
        CHECK( !l.report.malformed );
        if ( l.report.retain_lost != 1 || l.report.retained != 0 )
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
    const int64_t n = write_rt2( wire, sizeof( wire ) );
    CHECK( n > 0 );

    Loaded full;
    full.load( wire, n );
    CHECK( full.root != NULL );
    const int64_t used = full.retain.used;
    const int32_t records = full.retain.count;
    CHECK( records == 9 );

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
        CHECK( report.unknown == 11 );
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
        tblrt1::TableRetain::Id ids[ 4 ];
        tblrt1::TableRetain retain;
        retain.bytes = storage;
        retain.capacity = sizeof( storage );
        retain.ids = ids;
        retain.id_capacity = 2; // the wire carries THREE distinct retained ids
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
        // EXACTLY ONE record names the id there was no entry for, and exactly
        // that record is dropped: the count is per record, as the page pins
        CHECK( save_report.retain_lost == 1 );
        CHECK( retain.id_used == 2 );
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

// ---- A SECOND OCCURRENCE REPLACES ONLY WHAT IT CARRIES (§6.6) ----
//
// A discard taken at the FIELD is right only where a second occurrence
// replaces the first WHOLE. An ENUM-KEYED ARRAY never does: it overwrites the
// slots it carries and leaves every other slot standing. A LIST's repeat can
// be INERT, replacing nothing at all.
//
// CONTROL: discard at the field on either of these and the records under the
// slots the writer never touched die, with no counter moving to say so.
static void disjoint_occurrences()
{
    // the two Slot variant ids, as the wire names them (§3.2)
    static const uint64_t kLow = 0x24f3a319b88552c1ull;
    static const uint64_t kHigh = 0x9deeefd89ca8a81dull;

    // `banks` twice, with DISJOINT slots, each slot carrying an unknown field
    WireBuilder b;
    for ( int32_t pass = 0; pass < 2; pass++ )
    {
        b.field( "banks", 16 );
        const int64_t at = b.open_len();
        b.u8( 13 );  // the element kind: a table body
        b.leb( 1 );  // one triple
        b.leb( b.ref( pass == 0 ? kLow : kHigh ) );
        const int64_t slot = b.open_len();
        b.field( "future", 4 );
        b.u32( pass == 0 ? 41 : 42 );
        b.field( "hits", 4 );
        b.u32( pass == 0 ? 4 : 5 );
        b.end();
        b.close_len( slot );
        b.close_len( at );
    }
    b.end();
    uint8_t wire[ 4096 ];
    const int64_t n = b.finish( wire );

    Loaded l;
    l.load( wire, n );
    CHECK( l.root != NULL );
    CHECK( !l.report.malformed );
    CHECK( l.root->banks.slots[0].hits == 4 );
    CHECK( l.root->banks.slots[1].hits == 5 ); // both slots landed, from different occurrences
    CHECK( l.report.retained == 2 );
    CHECK( l.report.retain_lost == 0 );
    CHECK( l.retain.count == 2 ); // and NEITHER record went with the other slot's occurrence

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
    CHECK( reread != NULL && !read_report.malformed );
    CHECK( reread->banks.slots[0].future == 41 );
    CHECK( reread->banks.slots[1].future == 42 );

    // `list` twice, the second occurrence INERT: a body too short for its own
    // header keeps the value the field has (§4), so it replaces nothing and
    // the first occurrence's record stands.
    WireBuilder c;
    c.field( "list", 14 );
    const int64_t at = c.open_len();
    c.u8( 13 );
    c.leb( 1 );
    const int64_t element = c.open_len();
    c.field( "future", 4 );
    c.u32( 71 );
    c.field( "hits", 4 );
    c.u32( 8 );
    c.end();
    c.close_len( element );
    c.close_len( at );
    c.field( "list", 14 );
    c.leb( 0 ); // fewer than two bytes of body, and INERT
    c.end();
    uint8_t inert[ 4096 ];
    const int64_t in = c.finish( inert );

    Loaded l2;
    l2.load( inert, in );
    CHECK( l2.root != NULL );
    CHECK( !l2.report.malformed );
    CHECK( l2.report.retained == 1 );
    CHECK( l2.report.retain_lost == 0 );
    CHECK( l2.retain.count == 1 ); // the inert repeat replaced nothing, and took nothing

    tblrt1::TableReport inert_save;
    uint8_t inert_out[ 4096 ];
    const int64_t im = tblrt1::NodeMeasureRetain( l2.root, &l2.retain );
    const int64_t iw = tblrt1::NodeSaveRetain( l2.root, &l2.retain, inert_out, im, &inert_save );
    CHECK( iw == im && iw > 0 );
    CHECK( inert_save.retain_lost == 0 );
    uint8_t inert_plain[ 4096 ];
    const int64_t ip = tblrt1::NodeSave( l2.root, inert_plain, sizeof( inert_plain ) );
    CHECK( ip > 0 && iw > ip ); // the record rode
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
    const int64_t n = write_rt2( wire, sizeof( wire ) );
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
    disjoint_occurrences();
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
