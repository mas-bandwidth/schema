// THE UNION-ARM TRAVERSAL GATE (docs/SPEC-TABLES.md §2.6, §2.9, §3.1, §6.3,
// §7.6). One binary over the `tables/arms` unit: five shapes where a union arm
// hides a pointer or a collection extent, and the cross of two of them, each
// crossed by every walk the reference has. Measure and Save, LoadMeasure and
// Load into an exact region, the tool's path into a builder, Lock and a
// dereference after it, a plain memcpy relocation of the locked region, and a
// cook from the arena and from the region, opened and walked. Every reference
// a const form holds is shown to resolve INSIDE the block that holds it before
// it is followed, so an arena offset that survived a Lock is a red CHECK and
// not a crash.
//
// Compiled WITHOUT the serialize include path: the Table headers stand alone.
//
//   schema_test_arms    every shape

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <new>

#include "RingTable.h"
#include "CarryTable.h"
#include "NestTable.h"
#include "GateTable.h"

using namespace armdemo;

static int failures = 0;
static int checks = 0; // every CHECK, CHECK_EQ and report line, counted for the record

// the shape under test, named in every red line
static const char * g_shape = "";

#define CHECK( condition )                                                    \
    do                                                                        \
    {                                                                         \
        checks++;                                                             \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAIL %s:%d: [%s] %s\n", __FILE__, __LINE__, g_shape, #condition ); \
            fflush( stdout );                                                 \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

#define CHECK_EQ( actual, expected )                                          \
    do                                                                        \
    {                                                                         \
        checks++;                                                             \
        const long long a_ = (long long) ( actual );                          \
        const long long e_ = (long long) ( expected );                        \
        if ( a_ != e_ )                                                       \
        {                                                                     \
            printf( "FAIL %s:%d: [%s] %s = %lld, want %lld\n",                \
                    __FILE__, __LINE__, g_shape, #actual, a_, e_ );           \
            fflush( stdout );                                                 \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

// ---- every allocation sized from a measure goes through here ----
//
// A measure is an answer from the code under test, so a region sized from one
// is the single place a broken measure reaches the allocator. The ceiling is
// CHECKED first, and a measure past it is a red CHECK rather than a call to
// calloc (test/tables/maps_main.cpp says why).

static const int64_t kMeasureCeiling = 256 * 1024 * 1024;

static void * measured_calloc( int64_t measure, int64_t extra, const char * expr, const char * file, int line )
{
    if ( measure < 0 || measure > kMeasureCeiling )
    {
        printf( "FAIL %s:%d: [%s] %s = %lld, past the %lld byte measure ceiling\n",
                file, line, g_shape, expr, (long long) measure, (long long) kMeasureCeiling );
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
// them deliberately (make update-goldens).

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
// host's own order, so the open-and-walk below holds on the big-endian leg too.
static TableByteOrder host_byte_order()
{
    const uint16_t probe = 1;
    return *(const uint8_t *) &probe == 1 ? TableByteOrder::Little : TableByteOrder::Big;
}

static TableByteOrder foreign_byte_order()
{
    return host_byte_order() == TableByteOrder::Little ? TableByteOrder::Big : TableByteOrder::Little;
}

// THE WIRES THE GO TOOL READS BACK (docs/SPEC-TABLES.md §3.1): when the
// Makefile names a directory, every wire this gate writes is saved there. The
// tool reads each into its text form and writes it again, re-deriving the
// numbering from the graph alone, and must land on the same bytes.
static void save_wire( const char * name, const void * data, int64_t bytes )
{
    const char * dir = getenv( "SCHEMA_ARMS_DIR" );
    if ( dir == NULL ) { return; }
    char path[512];
    snprintf( path, sizeof( path ), "%s/%s.bin", dir, name );
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { printf( "FAIL cannot write %s\n", path ); failures++; return; }
    fwrite( data, 1, (size_t) bytes, f );
    fclose( f );
}

static void report_silent( const TableReport & r, const char * where )
{
    checks++;
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 || r.malformed )
    {
        printf( "FAIL [%s] %s: the report is not silent (unknown %d, kind_mismatch %d, clamped %d, duplicate %d, malformed %d)\n",
                g_shape, where, r.unknown, r.kind_mismatch, r.clamped, r.duplicate, (int) r.malformed );
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

// A REFERENCE RESOLVES INSIDE THE BLOCK THAT HOLDS IT (docs/SPEC-TABLES.md
// §6.3): a const form's slot is a self-relative delta, so the bytes it names
// lie inside the region, the loaded block or the cook file. An arena offset
// copied into a region by a walk that never rewrote it names bytes anywhere,
// and following it is a crash where this is a red line.
static bool ref_inside( const uint8_t * base, int64_t bytes, const TableRef & ref, int64_t size )
{
    if ( ref.value == 0 ) { return false; }
    const uint8_t * at = (const uint8_t *) &ref + ref.value;
    return at >= base && at + size <= base + bytes;
}

// ---- the surface every root has, named once per root ----
//
// One template exercises every shape, so the crossings are one text and a red
// line names the crossing; what differs per root is the value, the builder and
// the free functions the header claims, which this forwards.

#define ROOT_SURFACE( Root )                                                                                             \
    struct Root##Surface                                                                                                 \
    {                                                                                                                    \
        typedef Root Value;                                                                                              \
        typedef Root##Builder Builder;                                                                                   \
        static int64_t Measure( const Builder & b ) { return Root##Measure( b ); }                                       \
        static int64_t Save( const Builder & b, uint8_t * out, int64_t capacity ) { return Root##Save( b, out, capacity ); } \
        static int64_t Measure( const Root * r ) { return Root##Measure( r ); }                                          \
        static int64_t Save( const Root * r, uint8_t * out, int64_t capacity ) { return Root##Save( r, out, capacity ); } \
        static int64_t LoadMeasure( const uint8_t * wire, int64_t bytes ) { return Root##LoadMeasure( wire, bytes ); }   \
        static const Root * Load( uint8_t * region, int64_t region_bytes, const uint8_t * wire, int64_t bytes, TableReport * report ) \
        {                                                                                                                \
            return Root##Load( region, region_bytes, wire, bytes, report );                                              \
        }                                                                                                                \
        static bool LoadBuilder( Builder & into, const uint8_t * wire, int64_t bytes, TableReport * report )             \
        {                                                                                                                \
            return Root##LoadBuilder( into, wire, bytes, report );                                                       \
        }                                                                                                                \
        static int64_t CookMeasure( const Builder & b ) { return Root##CookMeasure( b ); }                               \
        static bool Cook( const Builder & b, void * out, uint64_t capacity, TableByteOrder order ) { return Root##Cook( b, out, capacity, order ); } \
        static int64_t CookMeasure( const Root * r ) { return Root##CookMeasure( r ); }                                  \
        static bool Cook( const Root * r, void * out, uint64_t capacity, TableByteOrder order ) { return Root##Cook( r, out, capacity, order ); } \
        static const Root * Open( const void * bytes, uint64_t length ) { return Root##Open( bytes, length ); }          \
    }

ROOT_SURFACE( Ring );
ROOT_SURFACE( Holder );
ROOT_SURFACE( Nest );
ROOT_SURFACE( Hand );
ROOT_SURFACE( Chain );
ROOT_SURFACE( Gate );

// a const form is checked against the block it lies in, so every reference it
// holds can be shown to resolve inside that block before it is followed
template <typename Root>
struct Checker
{
    typedef void ( * Fn )( const Root * root, const uint8_t * base, int64_t bytes, const char * where );
};

// exercise crosses one instance through every walk. `name` is the pin's name,
// `build` fills a fresh builder, `check` walks a const form of it.
template <typename S>
static void exercise( const char * name, void ( * build )( typename S::Builder & ), typename Checker<typename S::Value>::Fn check )
{
    typedef typename S::Value Root;
    typedef typename S::Builder Builder;
    g_shape = name;
    static uint8_t wire[1u << 16];
    static uint8_t again[1u << 16];

    // MEASURE and SAVE from the builder: the numbering walk over the arena
    Builder b;
    build( b );
    const int64_t measured = S::Measure( b );
    const int64_t saved = S::Save( b, wire, sizeof( wire ) );
    CHECK( measured > 0 );
    CHECK_EQ( saved, measured );
    if ( saved > 0 )
    {
        pin_golden( name, wire, saved );
        save_wire( name, wire, saved );
    }

    // LoadMeasure and Load into the caller's exact region, then the walk; and
    // the TOOL's path into a builder, which produces the same report and
    // writes the same bytes
    if ( saved > 0 )
    {
        const int64_t need = S::LoadMeasure( wire, saved );
        CHECK( need > 0 );
        uint8_t * region = (uint8_t *) MEASURED_CALLOC( need, 0 );
        if ( region != NULL )
        {
            TableReport report;
            const Root * loaded = S::Load( region, need, wire, saved, &report );
            CHECK( loaded != NULL );
            report_silent( report, "a loaded region" );
            if ( loaded != NULL )
            {
                check( loaded, region, need, "a loaded region" );
                CHECK_EQ( S::Save( loaded, again, sizeof( again ) ), saved );
                CHECK( memcmp( again, wire, (size_t) saved ) == 0 );
            }
            Builder into;
            TableReport tool;
            CHECK( S::LoadBuilder( into, wire, saved, &tool ) );
            reports_agree( tool, report );
            CHECK_EQ( S::Save( into, again, sizeof( again ) ), saved );
            CHECK( memcmp( again, wire, (size_t) saved ) == 0 );
            // the region is EXACT: one byte short is refused. Load zeroes the
            // region it is handed before it refuses, so this is the last act.
            TableReport short_report;
            CHECK( S::Load( region, need - 1, wire, saved, &short_report ) == NULL );
            free( region );
        }
    }

    // the COOK from the ARENA, before Lock: the numbering and the extent walked
    // over arena references
    const int64_t arena_cook_bytes = S::CookMeasure( b );
    CHECK( arena_cook_bytes > 0 );
    void * from_arena = arena_cook_bytes > 0 ? MEASURED_CALLOC( arena_cook_bytes, 0 ) : NULL;
    if ( from_arena != NULL )
    {
        CHECK( S::Cook( b, from_arena, (uint64_t) arena_cook_bytes, host_byte_order() ) );
    }

    // LOCK, then DEREFERENCE: every reference the region holds resolves inside
    // it, and a region re-saves the bytes the builder saved
    CHECK( b.Lock() );
    const Root * locked = b.AsConst();
    CHECK( locked != NULL );
    if ( locked != NULL )
    {
        check( locked, b.Region(), b.RegionBytes(), "the locked region" );
        if ( saved > 0 )
        {
            CHECK_EQ( S::Save( locked, again, sizeof( again ) ), saved );
            CHECK( memcmp( again, wire, (size_t) saved ) == 0 );
        }

        // RELOCATION: a region is one relocatable block whose references are
        // self-relative, so a plain memcpy of it walks the same (§6.3, §9)
        alignas( 64 ) static uint8_t relocated[1u << 16];
        CHECK( b.RegionBytes() <= (int64_t) sizeof( relocated ) );
        if ( b.RegionBytes() <= (int64_t) sizeof( relocated ) )
        {
            memset( relocated, 0, sizeof( relocated ) );
            memcpy( relocated, b.Region(), (size_t) b.RegionBytes() );
            check( (const Root *) relocated, relocated, b.RegionBytes(), "the relocated region" );
        }

        // the COOK from the REGION: opened and walked in the host's order, one
        // artifact with the arena's cook, and the other order's file refused at
        // the header by this build, as §7 has it
        const int64_t cook_bytes = S::CookMeasure( locked );
        CHECK( cook_bytes > 0 );
        CHECK_EQ( cook_bytes, arena_cook_bytes );
        void * cooked = cook_bytes > 0 ? MEASURED_CALLOC( cook_bytes, 0 ) : NULL;
        if ( cooked != NULL )
        {
            CHECK( S::Cook( locked, cooked, (uint64_t) cook_bytes, host_byte_order() ) );
            if ( from_arena != NULL && arena_cook_bytes == cook_bytes )
            {
                CHECK( memcmp( cooked, from_arena, (size_t) cook_bytes ) == 0 );
            }
            char cook_name[128];
            snprintf( cook_name, sizeof( cook_name ), "%s_cook", name );
            // THE COOK IS PINNED on a little-endian host, so a layout defect
            // goes red on a CHECK before any Open trusts the bytes. The
            // big-endian leg writes the other order and skips the pin.
            const bool trusted = host_byte_order() != TableByteOrder::Little || pin_golden( cook_name, (const uint8_t *) cooked, cook_bytes );
            const Root * opened = trusted ? S::Open( cooked, (uint64_t) cook_bytes ) : NULL;
            CHECK( opened != NULL );
            if ( opened != NULL )
            {
                check( opened, (const uint8_t *) cooked, cook_bytes, "an opened cook" );
            }

            void * other = MEASURED_CALLOC( cook_bytes, 0 );
            if ( other != NULL )
            {
                CHECK( S::Cook( locked, other, (uint64_t) cook_bytes, foreign_byte_order() ) );
                CHECK( S::Open( other, (uint64_t) cook_bytes ) == NULL ); // the other build's file
                free( other );
            }
            free( cooked );
        }
    }
    if ( from_arena != NULL ) { free( from_arena ); }
}

// ---- the five shapes (schema#565) ----

// C1: a LIST OF UNIONS whose set arm is a pointer, two elements naming one node
// beside a scalar arm and a None element. The element's arm is the pointer
// edge, so the node is numbered, packed and cooked where the list sits.
static void build_ring( RingBuilder & b )
{
    Ring * ring = b.GetRoot();
    Slot * first = RingItemsAdd( b.main, ring->items );
    first->type = SlotType::Node;
    Node * shared = NodeEmplace( b.main, first->node );
    shared->v = 7;
    Slot * second = RingItemsAdd( b.main, ring->items );
    second->type = SlotType::Node;
    second->node = first->node; // two elements, one node
    Slot * third = RingItemsAdd( b.main, ring->items );
    third->type = SlotType::Plain;
    third->plain = 5;
    RingItemsAdd( b.main, ring->items ); // a None element in its place
    ring->after = 11;
}

static void check_ring( const Ring * r, const uint8_t * base, int64_t bytes, const char * where )
{
    (void) where;
    CHECK_EQ( r->items.size(), 4 );
    if ( r->items.size() != 4 ) { return; }
    const Slot & first = r->items[0];
    const Slot & second = r->items[1];
    CHECK( first.type == SlotType::Node );
    CHECK( second.type == SlotType::Node );
    CHECK( ref_inside( base, bytes, first.node, (int64_t) sizeof( Node ) ) );
    CHECK( ref_inside( base, bytes, second.node, (int64_t) sizeof( Node ) ) );
    if ( ref_inside( base, bytes, first.node, (int64_t) sizeof( Node ) ) && ref_inside( base, bytes, second.node, (int64_t) sizeof( Node ) ) )
    {
        const Node * a = NodeAt( first.node );
        const Node * again = NodeAt( second.node );
        CHECK( a != NULL && a == again ); // one node, two elements
        if ( a != NULL ) { CHECK_EQ( a->v, 7 ); }
    }
    CHECK( r->items[2].type == SlotType::Plain && r->items[2].plain == 5 );
    CHECK( r->items[3].type == SlotType::None );
    CHECK_EQ( r->after, 11 );
}

// C2: a BY-VALUE TABLE ARM holding a non-empty list, with a container after
// it. The arm's array is the holder's extent, laid where the arm sits, and the
// tail follows it.
static void build_holder( HolderBuilder & b )
{
    Holder * holder = b.GetRoot();
    holder->carry.type = CarryType::Leaf;
    LeafReset( holder->carry.leaf );
    *LeafItemsAdd( b.main, holder->carry.leaf.items ) = 7;
    *HolderTailAdd( b.main, holder->tail ) = 9;
}

static void check_holder( const Holder * h, const uint8_t * base, int64_t bytes, const char * where )
{
    (void) where;
    CHECK( h->carry.type == CarryType::Leaf );
    if ( h->carry.type != CarryType::Leaf ) { return; }
    const Leaf & leaf = h->carry.leaf;
    CHECK_EQ( leaf.items.size(), 1 );
    CHECK( ref_inside( base, bytes, leaf.items.elements, (int64_t) sizeof( int32_t ) * leaf.items.size() ) );
    if ( leaf.items.size() == 1 && ref_inside( base, bytes, leaf.items.elements, (int64_t) sizeof( int32_t ) ) )
    {
        CHECK_EQ( leaf.items[0], 7 );
    }
    CHECK_EQ( h->tail.size(), 1 );
    CHECK( ref_inside( base, bytes, h->tail.elements, (int64_t) sizeof( int32_t ) * h->tail.size() ) );
    if ( h->tail.size() == 1 && ref_inside( base, bytes, h->tail.elements, (int64_t) sizeof( int32_t ) ) )
    {
        CHECK_EQ( h->tail[0], 9 );
    }
}

// C8: a NESTED UNION, Outer holding Inner holding a Leaf with a list, beside a
// sibling tail. The leaf's array is reached through two tags.
static void build_nest( NestBuilder & b )
{
    Nest * nest = b.GetRoot();
    nest->outer.type = OuterType::Inner;
    nest->outer.inner = Inner();
    nest->outer.inner.type = InnerType::Leaf;
    LeafReset( nest->outer.inner.leaf );
    *LeafItemsAdd( b.main, nest->outer.inner.leaf.items ) = 3;
    *LeafItemsAdd( b.main, nest->outer.inner.leaf.items ) = 4;
    *NestTailAdd( b.main, nest->tail ) = 8;
}

static void check_nest( const Nest * n, const uint8_t * base, int64_t bytes, const char * where )
{
    (void) where;
    CHECK( n->outer.type == OuterType::Inner );
    if ( n->outer.type != OuterType::Inner ) { return; }
    CHECK( n->outer.inner.type == InnerType::Leaf );
    if ( n->outer.inner.type != InnerType::Leaf ) { return; }
    const Leaf & leaf = n->outer.inner.leaf;
    CHECK_EQ( leaf.items.size(), 2 );
    CHECK( ref_inside( base, bytes, leaf.items.elements, (int64_t) sizeof( int32_t ) * leaf.items.size() ) );
    if ( leaf.items.size() == 2 && ref_inside( base, bytes, leaf.items.elements, 2 * (int64_t) sizeof( int32_t ) ) )
    {
        CHECK_EQ( leaf.items[0], 3 );
        CHECK_EQ( leaf.items[1], 4 );
    }
    CHECK_EQ( n->tail.size(), 1 );
    CHECK( ref_inside( base, bytes, n->tail.elements, (int64_t) sizeof( int32_t ) * n->tail.size() ) );
    if ( n->tail.size() == 1 && ref_inside( base, bytes, n->tail.elements, (int64_t) sizeof( int32_t ) ) )
    {
        CHECK_EQ( n->tail[0], 8 );
    }
}

// C9: a BOUNDED ARRAY OF UNIONS with a populated list in a selected element.
// The wire carries an array of arm headers under kind 14, and the framing scan
// that sums the extent has to read that shape.
static void build_hand( HandBuilder & b )
{
    Hand * hand = b.GetRoot();
    hand->entries_count = 2;
    hand->entries[0].type = CarryType::Plain;
    hand->entries[0].plain = 1;
    hand->entries[1].type = CarryType::Leaf;
    LeafReset( hand->entries[1].leaf );
    *LeafItemsAdd( b.main, hand->entries[1].leaf.items ) = 5;
    *LeafItemsAdd( b.main, hand->entries[1].leaf.items ) = 6;
    *LeafItemsAdd( b.main, hand->entries[1].leaf.items ) = 7;
    hand->after = 2;
}

static void check_hand( const Hand * h, const uint8_t * base, int64_t bytes, const char * where )
{
    (void) where;
    CHECK_EQ( h->entries_count, 2 );
    CHECK( h->entries[0].type == CarryType::Plain && h->entries[0].plain == 1 );
    CHECK( h->entries[1].type == CarryType::Leaf );
    if ( h->entries[1].type != CarryType::Leaf ) { return; }
    const Leaf & leaf = h->entries[1].leaf;
    CHECK_EQ( leaf.items.size(), 3 );
    CHECK( ref_inside( base, bytes, leaf.items.elements, (int64_t) sizeof( int32_t ) * leaf.items.size() ) );
    if ( leaf.items.size() == 3 && ref_inside( base, bytes, leaf.items.elements, 3 * (int64_t) sizeof( int32_t ) ) )
    {
        CHECK_EQ( leaf.items[0], 5 );
        CHECK_EQ( leaf.items[2], 7 );
    }
    CHECK_EQ( h->after, 2 );
}

// C1 crossed with C2: a LIST OF UNIONS whose set arm is a table holding a
// list, beside a plain arm, a None element and a leaf with an empty list. The
// elements' arms are visited after the whole element array, in index order.
static void build_chain( ChainBuilder & b )
{
    Chain * chain = b.GetRoot();
    Carry * first = ChainLinksAdd( b.main, chain->links );
    first->type = CarryType::Leaf;
    LeafReset( first->leaf );
    *LeafItemsAdd( b.main, first->leaf.items ) = 1;
    *LeafItemsAdd( b.main, first->leaf.items ) = 2;
    Carry * second = ChainLinksAdd( b.main, chain->links );
    second->type = CarryType::Plain;
    second->plain = 3;
    ChainLinksAdd( b.main, chain->links ); // a None element in its place
    Carry * fourth = ChainLinksAdd( b.main, chain->links );
    fourth->type = CarryType::Leaf;
    LeafReset( fourth->leaf ); // a selected leaf whose list is empty
    chain->after = 9;
}

static void check_chain( const Chain * c, const uint8_t * base, int64_t bytes, const char * where )
{
    (void) where;
    CHECK_EQ( c->links.size(), 4 );
    if ( c->links.size() != 4 ) { return; }
    CHECK( ref_inside( base, bytes, c->links.elements, (int64_t) sizeof( Carry ) * c->links.size() ) );
    if ( !ref_inside( base, bytes, c->links.elements, (int64_t) sizeof( Carry ) * c->links.size() ) ) { return; }
    const Carry & first = c->links[0];
    CHECK( first.type == CarryType::Leaf );
    if ( first.type == CarryType::Leaf )
    {
        CHECK_EQ( first.leaf.items.size(), 2 );
        CHECK( ref_inside( base, bytes, first.leaf.items.elements, (int64_t) sizeof( int32_t ) * first.leaf.items.size() ) );
        if ( first.leaf.items.size() == 2 && ref_inside( base, bytes, first.leaf.items.elements, 2 * (int64_t) sizeof( int32_t ) ) )
        {
            CHECK_EQ( first.leaf.items[0], 1 );
            CHECK_EQ( first.leaf.items[1], 2 );
        }
    }
    CHECK( c->links[1].type == CarryType::Plain && c->links[1].plain == 3 );
    CHECK( c->links[2].type == CarryType::None );
    CHECK( c->links[3].type == CarryType::Leaf );
    if ( c->links[3].type == CarryType::Leaf ) { CHECK_EQ( c->links[3].leaf.items.size(), 0 ); }
    CHECK_EQ( c->after, 9 );
}

// C10: a NODE REACHABLE ONLY THROUGH A UNION ARM, and a string blob likewise.
// No pointer field names Only or a *string anywhere in the closure, so the arm
// alone puts them in the set a load places and a cook lays out.
static void build_gate( GateBuilder & b )
{
    Gate * gate = b.GetRoot();
    gate->reach.type = ReachType::Only;
    OnlyEmplace( b.main, gate->reach.only )->w = 3;
    gate->after = 4;
}

static void check_gate( const Gate * g, const uint8_t * base, int64_t bytes, const char * where )
{
    (void) where;
    CHECK( g->reach.type == ReachType::Only );
    if ( g->reach.type != ReachType::Only ) { return; }
    CHECK( ref_inside( base, bytes, g->reach.only, (int64_t) sizeof( Only ) ) );
    if ( ref_inside( base, bytes, g->reach.only, (int64_t) sizeof( Only ) ) )
    {
        const Only * only = OnlyAt( g->reach.only );
        CHECK( only != NULL );
        if ( only != NULL ) { CHECK_EQ( only->w, 3 ); }
    }
    CHECK_EQ( g->after, 4 );
}

static void build_gate_text( GateBuilder & b )
{
    Gate * gate = b.GetRoot();
    gate->reach.type = ReachType::Text;
    gate->reach.text = TableRef();
    CHECK( TableStringEmplace( b.main, gate->reach.text, "arm", 3 ) != NULL );
    gate->after = 6;
}

static void check_gate_text( const Gate * g, const uint8_t * base, int64_t bytes, const char * where )
{
    (void) where;
    CHECK( g->reach.type == ReachType::Text );
    if ( g->reach.type != ReachType::Text ) { return; }
    CHECK( ref_inside( base, bytes, g->reach.text, kTableBlobHeader + 3 + 1 ) );
    if ( ref_inside( base, bytes, g->reach.text, kTableBlobHeader + 3 + 1 ) )
    {
        const TableStringView text = TableStringAt( g->reach.text );
        CHECK( text.data != NULL && text.length == 3 );
        if ( text.data != NULL && text.length == 3 ) { CHECK( memcmp( text.data, "arm", 4 ) == 0 ); }
    }
    CHECK_EQ( g->after, 6 );
}

int main()
{
    exercise<RingSurface>( "arms_ring", build_ring, check_ring );
    exercise<HolderSurface>( "arms_holder", build_holder, check_holder );
    exercise<NestSurface>( "arms_nest", build_nest, check_nest );
    exercise<HandSurface>( "arms_hand", build_hand, check_hand );
    exercise<ChainSurface>( "arms_chain", build_chain, check_chain );
    exercise<GateSurface>( "arms_gate", build_gate, check_gate );
    exercise<GateSurface>( "arms_gate_text", build_gate_text, check_gate_text );

    if ( failures != 0 )
    {
        printf( "\n%d union-arm check(s) failed\n", failures );
        return 1;
    }
    printf( "arms: all %d checks passed (docs/SPEC-TABLES.md §2.6, §2.9, §3.1, §7.6)\n", checks );
    return 0;
}
