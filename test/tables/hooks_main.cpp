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
//   the arena, the pack walk, the numbering, the region or the node directory
//   would show up as a fallback count above zero, or as an alloc the counter
//   never saw.

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

        // Lock: the pack map, its growth, and the packed region
        CHECK( builder.Lock() );
        CHECK( builder.RegionBytes() > 0 );

        // and the same two walks over the LOCKED region
        CHECK( graphdemo::SceneMeasure( builder ) == need );
        CHECK( graphdemo::SceneSave( builder, wire, (int64_t) sizeof( wire ) ) == need );
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

int main()
{
    test_refusal_reaches_the_caller();
    test_allocator_sees_everything();
    if ( failures != 0 )
    {
        printf( "hooks test FAILED: %d\n", failures );
        return 1;
    }
    printf( "hooks test passed: the refusal and every allocation went through the caller's own\n" );
    return 0;
}
