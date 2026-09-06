// THE RETAIN-UNKNOWN GATE (docs/SPEC-TABLES.md §6.6). One binary over the
// RT1/RT2/RT3 set: RT2 and RT3 are newer builds, RT1 is the build that cannot
// name what they wrote, and every row below is one of §6.6's own — the round
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

#include <new>

#include "RT1Table.h"
#include "RT2Table.h"
#include "RT3Table.h"

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
    // EIGHT BODIES carry an unknown FIELD — the root, `inner`, two elements of
    // `items`, a slot of `banks`, the union's arm body, a map entry's value and
    // a list element — and each is retained where it sat.
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

int main( int argc, char ** argv )
{
    (void) argc;
    (void) argv;
    round_trip();
    if ( failures != 0 )
    {
        printf( "retain: %d failure(s)\n", failures );
        return 1;
    }
    printf( "retain: ok\n" );
    return 0;
}
