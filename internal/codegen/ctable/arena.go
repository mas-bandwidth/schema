// The VARIABLE-LENGTH table runtime in C, emitted once per package and only
// when the unit declares a table with a pointer anywhere in its by-value
// closure (docs/SPEC-TABLES.md §2). A unit of pointer-free tables emits none of
// this and its generated output is byte-identical to a build with this file
// deleted — that zero-cost property is the point of deriving the mode.
//
// The two pieces:
//
//	TableArena  — the MUTABLE form: one logical arena of equal-size segments,
//	              handed out to workers in thread-local slabs with a single
//	              atomic per slab. Nodes are born at their final offsets and
//	              segments never move, so a T* stays valid for the arena's
//	              whole life and growth never invalidates anyone.
//	the region  — the CONST form: one exact-packed block, nodes laid back to
//	              back, references SELF-RELATIVE so a deref is one add and a
//	              whole region relocates by pure memcpy with zero fix-up.
//
// C99 HAS NO ATOMICS, and the slab handout needs one compare-exchange. The
// spelling is FEATURE TESTED rather than assumed, exactly as the packet
// emitter's inlining demand is: C11's <stdatomic.h> where the compiler has it,
// the gcc/clang __atomic builtins under -std=c99, the MSVC interlocked
// intrinsics on Windows, and a plain non-atomic fallback everywhere else —
// which is single-threaded-correct and says so, rather than silently claiming
// a contract it cannot hold.
package ctable

import "strings"

// tableArenaRuntime is the variable-length runtime, guarded per package like
// tablePrimitives so one definition survives any include order.
func tableArenaRuntime(pkg string) string {
	guard := "SCHEMA_" + strings.ToUpper(pkg) + "_TABLE_ARENA"
	return `#ifndef ` + guard + `
#define ` + guard + `

/* ---- variable-length tables: tuning constants (docs/SPEC-TABLES.md) ----

   The segment size and the count multiply to exactly 2^32: the u32 arena
   offset is the arena's hard ceiling, and these constants saturate it rather
   than leaving address space unreachable. Slab handout costs one atomic per
   slab, so per-node allocation costs no synchronization at all. */

#define kTableSegmentBits 22u                      /* 4 MiB segments */
#define kTableSegmentSize ( 1u << kTableSegmentBits )
#define kTableSegmentMask ( kTableSegmentSize - 1u )
#define kTableMaxSegments ( 1u << ( 32 - kTableSegmentBits ) ) /* 1024 -> 4 GiB */
#define kTableSlabBytes   ( 64u * 1024u )          /* one atomic per slab */
#define kTableAlign       8                        /* every node starts 8-aligned */
#define kTableAllocFailed 0xFFFFFFFFu

/* The pointer-chain depth cap. It bounds recursion on every walk — measure,
   save, load and pack — so a data cycle is an ERROR and never a hang, and a
   hostile wire cannot drive the C stack into the ground. A pointer chain's
   WIRE nesting equals its length (§3), so this also caps chain length: wide
   structures are unbounded, deep ones are not. */
#define kTableMaxDepth 128

/* ---- the one atomic this runtime needs, FEATURE TESTED ----

   TableArenaCas32 is a compare-exchange on a uint32_t with acquire-release
   ordering. C99 has no atomics of its own, so the spelling is tested for
   rather than assumed — the same shape the packet emitter's inlining demand
   takes. A compiler with none of the four gets the plain fallback, which is
   correct for a SINGLE-THREADED builder and says so here rather than
   pretending to §6.4's contract. */
#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L && !defined( __STDC_NO_ATOMICS__ )
#include <stdatomic.h>
#define SCHEMA_TABLE_ATOMIC 1
typedef _Atomic( uint32_t ) TableAtomicU32;
typedef _Atomic( uint8_t * ) TableAtomicPtr;
static SCHEMA_UNUSED int TableArenaCas32( TableAtomicU32 * slot, uint32_t * expected, uint32_t desired )
{
    return atomic_compare_exchange_weak_explicit( slot, expected, desired, memory_order_acq_rel, memory_order_acquire );
}
#define TableAtomicLoad32( slot ) atomic_load_explicit( ( slot ), memory_order_acquire )
#define TableAtomicStore32( slot, v ) atomic_store_explicit( ( slot ), ( v ), memory_order_relaxed )
#define TableAtomicLoadPtr( slot ) atomic_load_explicit( ( slot ), memory_order_acquire )
#define TableAtomicStorePtr( slot, v ) atomic_store_explicit( ( slot ), ( v ), memory_order_relaxed )
static SCHEMA_UNUSED int TableArenaCasPtr( TableAtomicPtr * slot, uint8_t ** expected, uint8_t * desired )
{
    return atomic_compare_exchange_strong_explicit( slot, expected, desired, memory_order_acq_rel, memory_order_acquire );
}
#elif defined( __GNUC__ ) || defined( __clang__ )
#define SCHEMA_TABLE_ATOMIC 1
typedef uint32_t TableAtomicU32;
typedef uint8_t * TableAtomicPtr;
static SCHEMA_UNUSED int TableArenaCas32( TableAtomicU32 * slot, uint32_t * expected, uint32_t desired )
{
    return __atomic_compare_exchange_n( slot, expected, desired, 1, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE );
}
#define TableAtomicLoad32( slot ) __atomic_load_n( ( slot ), __ATOMIC_ACQUIRE )
#define TableAtomicStore32( slot, v ) __atomic_store_n( ( slot ), ( v ), __ATOMIC_RELAXED )
#define TableAtomicLoadPtr( slot ) __atomic_load_n( ( slot ), __ATOMIC_ACQUIRE )
#define TableAtomicStorePtr( slot, v ) __atomic_store_n( ( slot ), ( v ), __ATOMIC_RELAXED )
static SCHEMA_UNUSED int TableArenaCasPtr( TableAtomicPtr * slot, uint8_t ** expected, uint8_t * desired )
{
    return __atomic_compare_exchange_n( slot, expected, desired, 0, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE );
}
#elif defined( _MSC_VER )
#define SCHEMA_TABLE_ATOMIC 1
#include <intrin.h>
typedef volatile uint32_t TableAtomicU32;
typedef uint8_t * volatile TableAtomicPtr;
static int TableArenaCas32( TableAtomicU32 * slot, uint32_t * expected, uint32_t desired )
{
    long was = _InterlockedCompareExchange( (volatile long *) slot, (long) desired, (long) *expected );
    if ( (uint32_t) was == *expected ) { return 1; }
    *expected = (uint32_t) was;
    return 0;
}
#define TableAtomicLoad32( slot ) ( *( slot ) )
#define TableAtomicStore32( slot, v ) ( *( slot ) = ( v ) )
#define TableAtomicLoadPtr( slot ) ( *( slot ) )
#define TableAtomicStorePtr( slot, v ) ( *( slot ) = ( v ) )
static int TableArenaCasPtr( TableAtomicPtr * slot, uint8_t ** expected, uint8_t * desired )
{
    void * was = _InterlockedCompareExchangePointer( (void * volatile *) slot, desired, *expected );
    if ( was == *expected ) { return 1; }
    *expected = (uint8_t *) was;
    return 0;
}
#else
/* NO ATOMIC SPELLING THIS COMPILER OFFERS. The builder is then
   SINGLE-THREADED: allocate from one worker, on one thread. Everything else —
   the wire, the region, the cook, the block — is unaffected, because none of
   them touches the arena. */
#define SCHEMA_TABLE_ATOMIC 0
typedef uint32_t TableAtomicU32;
typedef uint8_t * TableAtomicPtr;
static SCHEMA_UNUSED int TableArenaCas32( TableAtomicU32 * slot, uint32_t * expected, uint32_t desired )
{
    if ( *slot != *expected ) { *expected = *slot; return 0; }
    *slot = desired;
    return 1;
}
#define TableAtomicLoad32( slot ) ( *( slot ) )
#define TableAtomicStore32( slot, v ) ( *( slot ) = ( v ) )
#define TableAtomicLoadPtr( slot ) ( *( slot ) )
#define TableAtomicStorePtr( slot, v ) ( *( slot ) = ( v ) )
static SCHEMA_UNUSED int TableArenaCasPtr( TableAtomicPtr * slot, uint8_t ** expected, uint8_t * desired )
{
    if ( *slot != *expected ) { *expected = *slot; return 0; }
    *slot = desired;
    return 1;
}
#endif

/* ---- TableRef: a relocatable reference (never a machine pointer) ----

   Two encodings, one slot, and the FORM says which is in force:

     in the arena  — the node's arena offset (segment index in the high bits)
     in a region   — the SELF-RELATIVE byte delta from this slot's own address,
                     so a deref is one add, needs no base pointer, and a whole
                     region relocates by memcpy with zero fix-up

   0 is null in both, and a slot can never name the node that contains it, so
   zero names nothing real in either form.

   A REGION DELTA HAS NO REQUIRED SIGN (§6.3). A region is packed depth-first,
   so a node's FIRST reference points forward; every LATER reference to that
   same node points BACK at the one body it already has, which is exactly what
   makes one node one node in a region. Sharing and a back-reference are the
   same fact, and nothing validates a reference by its sign.

   IT IS EIGHT BYTES, SIGNED, so ONE REGION REACHES EVERYTHING (§6.3, §7). */
typedef struct TableRef
{
    int64_t value;
} TableRef;

static SCHEMA_UNUSED int TableRefNull( const TableRef * ref ) { return ref->value == 0; }

static SCHEMA_UNUSED uint32_t TableAlignUp( uint32_t bytes ) { return ( bytes + kTableAlign - 1 ) & ~( (uint32_t) kTableAlign - 1 ); }
static SCHEMA_UNUSED int64_t TableAlignUp64( int64_t bytes ) { return ( bytes + kTableAlign - 1 ) & ~( (int64_t) kTableAlign - 1 ); }

/* ---- the arena: segmented, slab-handed, lock-free by ownership ----

   Allocation is thread-local inside a worker's slab — no atomics on the node
   path. A worker takes its next slab with ONE compare-exchange, and a new
   segment is published with one more. Nothing ever moves: a segment, once
   allocated, lives untouched until the arena is torn down, so a T* obtained
   from an Emplace stays valid while other workers allocate, and an offset
   stays correct while the arena grows.

   The model this DELIBERATELY refuses: one buffer under a lock, grown by
   realloc. A realloc moves the buffer under workers mid-write; offsets fix
   identity but not the raw references already resolved from them, and the
   resulting corruption is invisible until much later. Segments never move, so
   that bug class cannot be written here.

   Slack: at most one slab tail per worker plus one slab per segment (a slab
   that will not fit is skipped rather than split), i.e. under 2% of a segment
   plus threads x 64 KiB. That is the price of never synchronizing per node. */
typedef struct TableArena
{
    TableAtomicPtr segments[ kTableMaxSegments ];
    TableAtomicU32 cursor; /* (segment << kTableSegmentBits) | bytes handed out */
    int locked;            /* MONOTONIC: Lock is one-way, there is no unlock */
} TableArena;

static SCHEMA_UNUSED void TableArenaInit( TableArena * arena )
{
    uint32_t i;
    for ( i = 0; i < kTableMaxSegments; i++ )
    {
        TableAtomicStorePtr( &arena->segments[i], (uint8_t *) NULL );
    }
    TableAtomicStore32( &arena->cursor, 0 );
    arena->locked = 0;
}

static SCHEMA_UNUSED void TableArenaShutdown( TableArena * arena )
{
    uint32_t i;
    for ( i = 0; i < kTableMaxSegments; i++ )
    {
        uint8_t * segment = TableAtomicLoadPtr( &arena->segments[i] );
        if ( segment != NULL )
        {
            uint8_t * expected = segment;
            if ( TableArenaCasPtr( &arena->segments[i], &expected, (uint8_t *) NULL ) ) { free( segment ); }
        }
    }
    TableAtomicStore32( &arena->cursor, 0 );
}

/* one L1 load plus an add: the segment table is 8 KiB and stays hot */
static SCHEMA_UNUSED uint8_t * TableArenaAt( const TableArena * arena, uint32_t offset )
{
    TableAtomicPtr * slot = (TableAtomicPtr *) &arena->segments[ offset >> kTableSegmentBits ];
    return TableAtomicLoadPtr( slot ) + ( offset & kTableSegmentMask );
}

/* TableArenaGrabSlab hands one worker its next private slab. Returns
   kTableAllocFailed when the arena's address space or the allocator is
   exhausted — a loud refusal, never a silent smaller slab. */
static SCHEMA_UNUSED uint32_t TableArenaGrabSlab( TableArena * arena )
{
    for ( ;; )
    {
        uint32_t cursor = TableAtomicLoad32( &arena->cursor );
        uint32_t segment = cursor >> kTableSegmentBits;
        uint32_t used = cursor & kTableSegmentMask;
        uint32_t next_segment;
        /* strictly less: a slab is never split across segments, and the tail
           is the documented slack */
        if ( used + kTableSlabBytes < kTableSegmentSize )
        {
            if ( TableAtomicLoadPtr( &arena->segments[segment] ) == NULL )
            {
                /* calloc, NOT malloc: Lock copies whole nodes, PADDING
                   INCLUDED, so anything uninitialised here reaches a packed
                   region. It costs nothing measurable: a fresh segment is
                   untouched pages either way, and calloc has the kernel hand
                   them over zeroed. */
                uint8_t * memory = (uint8_t *) calloc( 1, kTableSegmentSize );
                uint8_t * expected = NULL;
                if ( memory == NULL ) { return kTableAllocFailed; }
                if ( !TableArenaCasPtr( &arena->segments[segment], &expected, memory ) )
                {
                    free( memory ); /* another worker published this segment first */
                }
            }
            if ( TableArenaCas32( &arena->cursor, &cursor, cursor + kTableSlabBytes ) )
            {
                return ( segment << kTableSegmentBits ) | used;
            }
            continue;
        }
        next_segment = segment + 1;
        if ( next_segment >= kTableMaxSegments ) { return kTableAllocFailed; } /* 4 GiB: the arena offset's ceiling */
        TableArenaCas32( &arena->cursor, &cursor, next_segment << kTableSegmentBits );
    }
}

/* ---- TableWorker: one thread's allocation front ----

   The threading contract, stated plainly:
     * Allocating on YOUR OWN worker is safe concurrently with any other
       worker's. No locks, no atomics per node.
     * Writing fields of a node ANOTHER worker allocated is your own
       synchronization problem — this runtime does not arbitrate it.
     * Lock and Save are single-threaded: call them after the workers have
       joined. */
typedef struct TableWorker
{
    TableArena * arena;
    uint32_t next;
    uint32_t end;
} TableWorker;

/* one worker per thread: take one, allocate on it, and synchronize your own
   writes to nodes another worker allocated */
static SCHEMA_UNUSED TableWorker TableWorkerMake( TableArena * arena )
{
    TableWorker worker;
    worker.arena = arena;
    worker.next = 0;
    worker.end = 0;
    return worker;
}

/* TableWorkerBump reserves the bytes of arena for one node and hands back its
   arena offset, or kTableAllocFailed. It is the untyped half of every
   generated <Name>Emplace: the type's own size and its Reset stay in the
   generated code, where they can be spelled. */
static SCHEMA_UNUSED uint32_t TableWorkerBump( TableWorker * worker, uint32_t bytes )
{
    uint32_t at;
    if ( worker->arena == NULL || worker->arena->locked ) { return kTableAllocFailed; }
    bytes = TableAlignUp( bytes );
    if ( bytes > kTableSlabBytes ) { return kTableAllocFailed; } /* a node larger than a slab: refused, never split */
    if ( worker->end == 0 || worker->next + bytes > worker->end )
    {
        uint32_t offset = TableArenaGrabSlab( worker->arena );
        if ( offset == kTableAllocFailed ) { return kTableAllocFailed; }
        worker->next = offset;
        worker->end = offset + kTableSlabBytes;
        if ( worker->next == 0 ) { worker->next = kTableAlign; } /* offset 0 is null: the arena's head stays reserved */
    }
    at = worker->next;
    worker->next += bytes;
    return at;
}

/* ---- the resolution context: which encoding a walk is reading ----

   C++ spells this as two context types and resolves between them by overload.
   C has one struct and one rule: a NULL arena means a REGION, where a slot
   holds a self-relative delta; a non-NULL arena means the mutable form, where
   a slot holds an arena offset. A NULL ctx is a region too, so a consumer
   dereferencing inside a cooked region writes <Name>At( NULL, &slot ). */
typedef struct TableCtx
{
    const TableArena * arena;
} TableCtx;

/* ---- the region sink: bump-allocating into the caller's exact buffer ----

   LoadMeasure sized the buffer exactly, so this allocates nothing and nothing
   moves. References come out SELF-RELATIVE, which is the region encoding. */
typedef struct TableRegionSink
{
    uint8_t * base;
    int64_t capacity;
    int64_t used;
} TableRegionSink;

/* WHERE A NODE COMES FROM, and the C form of the Sink template parameter the
   reference threads through its load path. Exactly one member is non-NULL: a
   region sink bumps into the caller's exact region and leaves slots
   self-relative; a worker allocates in the arena and leaves slots holding the
   arena offset. */
typedef struct TableSink
{
    TableRegionSink * region;
    TableWorker * worker;
} TableSink;

#endif /* ` + guard + ` */
`
}
