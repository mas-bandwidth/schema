// THE HOOKS, HELD BY A TRANSLATION UNIT THAT SUPPLIES ITS OWN (schema#382).
//
// A game passes in its own assert, its own fatal handler and its own
// allocate/free pair, and the generated table runtime has to route EVERY call
// through them. This unit defines all four before including a single generated
// header, so what it observes is what a consumer observes.
//
// The two questions it answers, and neither can be answered by reading the
// source:
//
//   THE REFUSAL LANDS IN THE CALLER'S HANDLER. Indexing an enum-keyed array by
//   None is a program error in every build (docs/SPEC-TABLES.md §2.4). Here it
//   raises the caller's assert and then the caller's fatal, which escapes by
//   longjmp instead of ending the process — so the refusal is OBSERVED rather
//   than inferred from a signal.
//
//   THE ALLOCATOR SEES EVERYTHING, WITH ZERO BYPASSES. The builder is handed a
//   counting TableAllocator, and the DEFAULT pair — schema_allocate /
//   schema_release, which is where a bypassing call would land — is defined
//   here to count separately. A single malloc, calloc, realloc or free left in
//   the arena, the pack walk, the numbering, the region, the node directory or
//   the cook's write side would show up as a fallback count above zero, or as
//   an alloc the counter never saw.

#include <setjmp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// ---- the caller's hooks, defined BEFORE the generated headers ----

static int g_asserts = 0;
static int g_fatals = 0;
static jmp_buf g_escape;

#define schema_assert( condition )                                            \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) ) { g_asserts++; }                                \
    }                                                                         \
    while ( 0 )

// A fatal handler never returns to its call site — the accessor that raised it
// would index one element before the array. This one leaves by longjmp, which
// is the C spelling of "my handler took over".
#define schema_fatal()                                                        \
    do                                                                        \
    {                                                                         \
        g_fatals++;                                                           \
        longjmp( g_escape, 1 );                                               \
    }                                                                         \
    while ( 0 )

// The DEFAULT allocator pair, replaced. Nothing in this program should reach
// it: every allocation belongs to the counting pair handed to the builder.
static int g_fallback_allocs = 0;
static int g_fallback_frees = 0;

static void * fallback_allocate( long long bytes )
{
    g_fallback_allocs++;
    void * memory = calloc( (size_t) 1, (size_t) bytes );
    return memory;
}

static void fallback_release( void * pointer )
{
    if ( pointer != NULL ) { g_fallback_frees++; }
    free( pointer );
}

#define schema_allocate( bytes ) fallback_allocate( (long long) ( bytes ) )
#define schema_release( pointer ) fallback_release( pointer )

#include "GraphTable.h"
// the BYTE BUFFER unit (docs/SPEC-TABLES.md §2.5), under the same hooks: its
// builder allocates blobs through the caller's pair, and its READ path — the
// load, the views — allocates nothing at all
#include "AssetsTable.h"

static int failures = 0;

#define CHECK( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAIL %s:%d: %s\n", __FILE__, __LINE__, #condition );     \
            failures++;                                                       \
        }                                                                     \
    }                                                                         \
    while ( 0 )

// ---- the counting allocator the builder is handed ----

struct Counters
{
    int allocs;
    int frees;
    long long bytes;
};

static void * counting_alloc( void * context, int64_t bytes )
{
    Counters * counters = (Counters *) context;
    counters->allocs++;
    counters->bytes += (long long) bytes;
    return calloc( (size_t) 1, (size_t) bytes ); // ZEROED: the allocator's contract
}

static void counting_free( void * context, void * pointer )
{
    Counters * counters = (Counters *) context;
    if ( pointer != NULL ) { counters->frees++; }
    free( pointer );
}

// ---- the refusal, observed in the caller's own handler ----

static void test_refusal_reaches_the_caller()
{
    graphdemo::Depot depot;
    g_asserts = 0;
    g_fatals = 0;
    if ( setjmp( g_escape ) == 0 )
    {
        graphdemo::Tier key = graphdemo::Tier::None;
        graphdemo::Layer & none = depot.banks[key];
        none.depth = 1; // never reached: the accessor refused
        printf( "FAIL: a None index returned instead of refusing\n" );
        failures++;
    }
    CHECK( g_asserts == 1 ); // the caller's assert carried the message
    CHECK( g_fatals == 1 );  // and the caller's fatal took the program over
}

// ---- every allocation, through the caller's pair ----

static void test_allocator_sees_everything()
{
    g_fallback_allocs = 0;
    g_fallback_frees = 0;

    Counters counters = { 0, 0, 0 };
    graphdemo::TableAllocator pair;
    pair.alloc = counting_alloc;
    pair.free = counting_free;
    pair.context = &counters;

    static uint8_t wire[65536];
    int64_t wrote = 0;
    {
        graphdemo::SceneBuilder builder( pair );
        CHECK( counters.allocs > 0 ); // the arena's first segment, already counted

        graphdemo::Scene * root = builder.GetRoot();
        CHECK( root != NULL );
        // a chain long enough that the numbering's entry array grows at least
        // once, so the grow path is counted too
        graphdemo::TableSlot<graphdemo::ListNode> previous;
        for ( int i = 0; i < 400; i++ )
        {
            graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
            CHECK( !node.null() );
            node->value = i;
            if ( previous.null() ) { root->head = node; }
            else { previous->next = node; }
            previous = node;
        }

        // the wire walks: numbering, entry array, and their teardown
        int64_t need = graphdemo::SceneMeasure( builder );
        CHECK( need > 0 );
        wrote = graphdemo::SceneSave( builder, wire, (int64_t) sizeof( wire ) );
        CHECK( wrote == need );

        // the cook's write side (docs/SPEC-TABLES.md §7.6): its numbering and
        // its offsets, through the builder's own pair, over the arena
        static uint8_t cook[65536];
        const int64_t cooked = graphdemo::SceneCookMeasure( builder );
        CHECK( cooked > 0 && cooked <= (int64_t) sizeof( cook ) );
        CHECK( graphdemo::SceneCook( builder, cook, (uint64_t) cooked, graphdemo::TableByteOrder::Little ) );

        // Lock: the pack map, its growth, and the packed region
        CHECK( builder.Lock() );
        CHECK( builder.RegionBytes() > 0 );

        // and the same walks over the LOCKED region
        CHECK( graphdemo::SceneMeasure( builder ) == need );
        CHECK( graphdemo::SceneSave( builder, wire, (int64_t) sizeof( wire ) ) == need );
        CHECK( graphdemo::SceneCookMeasure( builder ) == cooked );
        CHECK( graphdemo::SceneCook( builder, cook, (uint64_t) cooked, graphdemo::TableByteOrder::Little ) );
    }

    // the tool path: a fresh builder loaded from the wire, whose node
    // directory is the last allocation site on the pointer path
    {
        graphdemo::SceneBuilder builder( pair );
        graphdemo::TableReport report;
        CHECK( graphdemo::SceneLoadBuilder( builder, wire, wrote, &report ) );
    }

    printf( "counting allocator: %d allocations, %d frees, %lld bytes\n",
            counters.allocs, counters.frees, counters.bytes );
    CHECK( counters.allocs > 4 );          // segments, map, numbering, region, directory
    CHECK( counters.allocs == counters.frees ); // and every one released
    // THE ZERO-BYPASS CLAIM. The default pair is where a leftover calloc,
    // malloc, realloc or free would land, and nothing may reach it.
    CHECK( g_fallback_allocs == 0 );
    CHECK( g_fallback_frees == 0 );
}

// ---- the BYTE BUFFER's two allocation claims, at run time ----
//
// THE BUILDER'S BLOBS GO THROUGH THE PAIR — a small blob inside a slab and a
// blob past one slab, which takes a span of the arena's own — and THE READ
// PATH ALLOCATES NOTHING: LoadMeasure, Load into the caller's region and the
// views run with both counters frozen. A scan cannot hold that claim; these
// counters can (docs/SPEC-TABLES.md §2.5, §6.5).

static void test_blob_read_path_allocates_nothing()
{
    g_fallback_allocs = 0;
    g_fallback_frees = 0;

    Counters counters = { 0, 0, 0 };
    blobdemo::TableAllocator pair;
    pair.alloc = counting_alloc;
    pair.free = counting_free;
    pair.context = &counters;

    static uint8_t wire[1 << 18];
    int64_t wrote = 0;
    {
        blobdemo::CatalogBuilder builder( pair );
        blobdemo::Catalog * root = builder.GetRoot();
        CHECK( root != NULL );
        blobdemo::TableBytesSlot small = builder.AllocBytes( 24 );
        CHECK( !small.null() );
        for ( int i = 0; i < 24; i++ ) { small.data[i] = (uint8_t) i; }
        root->thumb = small;
        root->alias = small; // one node, two names
        blobdemo::TableStringSlot note = builder.AllocString( 5 );
        CHECK( !note.null() );
        memcpy( note.data, "blobs", 5 );
        root->note = note;
        blobdemo::TableSlot<blobdemo::Asset> asset = builder.Alloc<blobdemo::Asset>();
        CHECK( !asset.null() );
        blobdemo::TableBytesSlot big = builder.AllocBytes( 70000 ); // past a slab: a span, counted
        CHECK( !big.null() );
        for ( int64_t i = 0; i < 70000; i++ ) { big.data[i] = (uint8_t) ( i & 0xff ); }
        asset->data = big;
        root->head = asset;
        wrote = blobdemo::CatalogSave( builder, wire, (int64_t) sizeof( wire ) );
        CHECK( wrote > 70000 );
    }
    CHECK( counters.allocs > 0 );
    CHECK( counters.allocs == counters.frees ); // the builder's whole life, released
    CHECK( g_fallback_allocs == 0 );            // and none of it bypassed the pair

    // the read path, with both counters FROZEN. The region is the CALLER'S
    // buffer and the caller owes it alignment — the suite refuses a
    // misaligned base by design.
    alignas( 16 ) static uint8_t region[1 << 18];
    const int fallback_before = g_fallback_allocs;
    const int counted_before = counters.allocs;
    int64_t need = blobdemo::CatalogLoadMeasure( wire, wrote );
    CHECK( need > 0 && need <= (int64_t) sizeof( region ) );
    blobdemo::TableReport report;
    const blobdemo::Catalog * loaded = blobdemo::CatalogLoad( region, need, wire, wrote, &report );
    CHECK( loaded != NULL && !report.malformed );
    if ( loaded == NULL ) return;
    blobdemo::TableBytesView thumb = blobdemo::TableBytesAt( loaded->thumb );
    blobdemo::TableBytesView alias = blobdemo::TableBytesAt( loaded->alias );
    blobdemo::TableStringView note = blobdemo::TableStringAt( loaded->note );
    const blobdemo::Asset * asset = blobdemo::AssetAt( blobdemo::TableRegionCtx{}, loaded->head );
    CHECK( asset != NULL );
    CHECK( thumb.data != NULL && thumb.length == 24 && thumb.data == alias.data );
    CHECK( note.data != NULL && note.length == 5 && strcmp( note.data, "blobs" ) == 0 );
    if ( asset != NULL )
    {
        blobdemo::TableBytesView data = blobdemo::TableBytesAt( asset->data );
        CHECK( data.data != NULL && data.length == 70000 && data.data[1] == 1 );
    }
    CHECK( g_fallback_allocs == fallback_before ); // ZERO ALLOCATION ON THE READ PATH
    CHECK( counters.allocs == counted_before );    // through either pair
}

int main()
{
    test_refusal_reaches_the_caller();
    test_allocator_sees_everything();
    test_blob_read_path_allocates_nothing();
    if ( failures != 0 )
    {
        printf( "hooks test FAILED: %d\n", failures );
        return 1;
    }
    printf( "hooks test passed: the refusal and every allocation went through the caller's own\n" );
    return 0;
}
