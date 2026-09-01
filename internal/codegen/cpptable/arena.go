// The VARIABLE-LENGTH table runtime, emitted once per package and only when
// the unit declares a table with a pointer anywhere in its by-value closure
// (SPEC-TABLES.md §2). A unit of pointer-free tables emits none of this and
// its generated output is byte-identical to a build with this file deleted —
// that zero-cost property is the point of deriving the mode.
//
// The three pieces:
//
//	TableArena  — the MUTABLE form: one logical arena of equal-size segments,
//	              handed out to workers in thread-local slabs with a single
//	              atomic per slab. Nodes are born at their final offsets and
//	              segments never move, so a T* stays valid for the arena's
//	              whole life and growth never invalidates anyone.
//	the region  — the CONST form: one exact-packed block, nodes laid back to
//	              back, references SELF-RELATIVE so a deref is one add and a
//	              whole region relocates by pure memcpy with zero fix-up.
//	the header  — the cooked form's guard: magic, layout id, size. A cooked
//	              file is an accelerator, build-locked; a mismatch refuses
//	              loudly and the caller falls back to a wire load.
package cpptable

import "strings"

// tableArenaRuntime is the variable-length runtime, guarded per package like
// tablePrimitives so one definition survives any include order.
func tableArenaRuntime(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_ARENA"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// ---- variable-length tables: tuning constants (SPEC-TABLES.md) ----
//
// The segment size and the count multiply to exactly 2^32: the u32 reference
// is the arena's hard ceiling, and these constants saturate it rather than
// leaving address space unreachable. Slab handout costs one atomic per slab,
// so per-node allocation costs no synchronization at all.

static const uint32_t kTableSegmentBits = 22;                          // 4 MiB segments
static const uint32_t kTableSegmentSize = 1u << kTableSegmentBits;
static const uint32_t kTableSegmentMask = kTableSegmentSize - 1u;
static const uint32_t kTableMaxSegments = 1u << ( 32 - kTableSegmentBits ); // 1024 -> 4 GiB
static const uint32_t kTableSlabBytes   = 64u * 1024u;                 // one atomic per slab
static const uint32_t kTableAlign       = 8;                           // every node starts 8-aligned
static const uint32_t kTableAllocFailed = 0xFFFFFFFFu;

// The pointer-chain depth cap. It bounds recursion on every walk — save,
// load, cook and open — so a data cycle is an ERROR and never a hang, and a
// hostile wire cannot drive the C stack into the ground. A pointer chain's
// WIRE nesting equals its length (§3), so this also caps chain length: wide
// structures are unbounded, deep ones are not. Lifting it wants a flat,
// indexed node encoding — a named follow-on (§15).
static const int32_t kTableMaxDepth = 128;

// ---- TableRef: a relocatable reference (never a machine pointer) ----
//
// Two encodings, one slot, and the FORM says which is in force:
//
//   in the arena  — the node's arena offset (segment index in the high bits)
//   in a region   — the SELF-RELATIVE byte delta from this slot's own address,
//                   so a deref is one add, needs no base pointer, and a whole
//                   region relocates by memcpy with zero fix-up
//
// 0 is null in both. Region deltas are always POSITIVE: a region is packed by
// a depth-first walk, so a child always sits after the slot that names it —
// which is also what makes the cooked-form bounds walk cycle-free by
// construction.
struct TableRef
{
    uint32_t value = 0;
    bool null() const { return value == 0; }
};

// TableSlot is what Alloc hands back: usable as the node pointer (write
// fields through it) AND as the reference to store in a pointer field.
template <typename T> struct TableSlot
{
    T * ptr = NULL;
    TableRef ref;
    T * operator->() const { return ptr; }
    T & operator*() const { return *ptr; }
    operator T *() const { return ptr; }
    operator TableRef() const { return ref; }
    bool null() const { return ptr == NULL; }
};

inline uint32_t TableAlignUp( uint32_t bytes ) { return ( bytes + kTableAlign - 1 ) & ~( kTableAlign - 1 ); }
inline int64_t TableAlignUp64( int64_t bytes ) { return ( bytes + kTableAlign - 1 ) & ~( int64_t( kTableAlign ) - 1 ); }

// ---- the arena: segmented, slab-handed, lock-free by ownership ----
//
// Allocation is thread-local inside a worker's slab — no atomics on the node
// path. A worker takes its next slab with ONE compare-exchange, and a new
// segment is published with one more. Nothing ever moves: a segment, once
// allocated, lives untouched until the arena is torn down, so a T* obtained
// from Alloc stays valid while other workers allocate, and an offset stays
// correct while the arena grows.
//
// The model this DELIBERATELY refuses: one buffer under a lock, grown by
// realloc. A realloc moves the buffer under workers mid-write; offsets fix
// identity but not the raw references already resolved from them, and the
// resulting corruption is invisible until much later. Segments never move, so
// that bug class cannot be written here.
//
// Slack: at most one slab tail per worker plus one slab per segment (a slab
// that will not fit is skipped rather than split), i.e. under 2% of a segment
// plus threads x 64 KiB. That is the price of never synchronizing per node.
struct TableArena
{
    std::atomic<uint8_t *> segments[ kTableMaxSegments ];
    std::atomic<uint32_t> cursor; // (segment << kTableSegmentBits) | bytes handed out
    bool locked = false;          // MONOTONIC: Lock() is one-way, there is no unlock
};

inline void TableArenaInit( TableArena & arena )
{
    for ( uint32_t i = 0; i < kTableMaxSegments; i++ )
    {
        arena.segments[i].store( NULL, std::memory_order_relaxed );
    }
    arena.cursor.store( 0, std::memory_order_relaxed );
    arena.locked = false;
}

inline void TableArenaShutdown( TableArena & arena )
{
    for ( uint32_t i = 0; i < kTableMaxSegments; i++ )
    {
        uint8_t * segment = arena.segments[i].exchange( NULL, std::memory_order_acq_rel );
        if ( segment != NULL ) { free( segment ); }
    }
    arena.cursor.store( 0, std::memory_order_relaxed );
}

// one L1 load plus an add: the segment table is 8 KiB and stays hot
inline uint8_t * TableArenaAt( const TableArena & arena, uint32_t offset )
{
    return arena.segments[ offset >> kTableSegmentBits ].load( std::memory_order_relaxed ) + ( offset & kTableSegmentMask );
}

// TableArenaGrabSlab hands one worker its next private slab. Returns
// kTableAllocFailed when the arena's address space or the allocator is
// exhausted — a loud refusal, never a silent smaller slab.
inline uint32_t TableArenaGrabSlab( TableArena & arena )
{
    for ( ;; )
    {
        uint32_t cursor = arena.cursor.load( std::memory_order_acquire );
        uint32_t segment = cursor >> kTableSegmentBits;
        uint32_t used = cursor & kTableSegmentMask;
        // strictly less: a slab is never split across segments, and the tail
        // is the documented slack
        if ( used + kTableSlabBytes < kTableSegmentSize )
        {
            if ( arena.segments[segment].load( std::memory_order_acquire ) == NULL )
            {
                uint8_t * memory = (uint8_t *) malloc( kTableSegmentSize );
                if ( memory == NULL ) { return kTableAllocFailed; }
                uint8_t * expected = NULL;
                if ( !arena.segments[segment].compare_exchange_strong( expected, memory, std::memory_order_acq_rel ) )
                {
                    free( memory ); // another worker published this segment first
                }
            }
            if ( arena.cursor.compare_exchange_weak( cursor, cursor + kTableSlabBytes, std::memory_order_acq_rel ) )
            {
                return ( segment << kTableSegmentBits ) | used;
            }
            continue;
        }
        uint32_t next_segment = segment + 1;
        if ( next_segment >= kTableMaxSegments ) { return kTableAllocFailed; } // 4 GiB: the u32 reference's ceiling
        arena.cursor.compare_exchange_weak( cursor, next_segment << kTableSegmentBits, std::memory_order_acq_rel );
    }
}

// ---- TableWorker: one thread's allocation front ----
//
// The threading contract, stated plainly:
//   * Alloc on YOUR OWN worker is safe concurrently with any other worker's.
//     No locks, no atomics per node.
//   * Writing fields of a node ANOTHER worker allocated is your own
//     synchronization problem — this runtime does not arbitrate it.
//   * Lock, Save, Cook and Open are single-threaded: call them after the
//     workers have joined.
struct TableWorker
{
    TableArena * arena = NULL;
    uint32_t next = 0;
    uint32_t end = 0;

    template <typename T> TableSlot<T> Alloc()
    {
        static_assert( alignof( T ) <= kTableAlign, "a table node's alignment must fit the arena's" );
        TableSlot<T> slot;
        if ( arena == NULL || arena->locked ) { return slot; }
        uint32_t bytes = TableAlignUp( (uint32_t) sizeof( T ) );
        if ( bytes > kTableSlabBytes ) { return slot; } // a node larger than a slab: refused, never split
        if ( end == 0 || next + bytes > end )
        {
            uint32_t offset = TableArenaGrabSlab( *arena );
            if ( offset == kTableAllocFailed ) { return slot; }
            next = offset;
            end = offset + kTableSlabBytes;
            if ( next == 0 ) { next = kTableAlign; } // offset 0 is null: the arena's head stays reserved
        }
        uint32_t at = next;
        next += bytes;
        slot.ptr = new ( TableArenaAt( *arena, at ) ) T{};
        slot.ref.value = at;
        return slot;
    }
};

// ---- resolution contexts: which encoding a walk is reading ----

struct TableArenaCtx { const TableArena * arena; };
struct TableRegionCtx {};

// ---- the region sink: bump-allocating into the caller's exact buffer ----
//
// LoadMeasure sized the buffer exactly, so this allocates nothing and nothing
// moves. References come out SELF-RELATIVE, which is the region encoding.
struct TableRegionSink
{
    uint8_t * base = NULL;
    int64_t capacity = 0;
    int64_t used = 0;
};

// ---- the cooked form's header ----
//
// The cooked form answers one requirement: load a big file, point at its
// root, without copying it and without parsing it. It is an accelerator
// beside the wire, not an archive — the tolerant wire (§3) stays the format
// of record, and a cooked file is regenerated whenever the schema moves.
// The header is what makes the build-locking honest:
//
//   magic     — identifies the form AND the byte order (a foreign-endian
//               writer's magic reads wrong, and Open refuses)
//   layout_id — a compile-time digest of the packed-layout facts: the field
//               shape from the schema, mixed with each closure type's sizeof
//               as this build compiled it, so schema drift AND ABI drift both
//               refuse
//   bytes     — the packed region's length
//
// 32 bytes, so a root at base + 32 keeps the alignment the allocator gave the
// base (mmap gives page alignment for free).
static const uint32_t kTableCookedMagic = 0x314B4353u; // 'S','C','K','1'
static const int64_t kTableCookedHeaderBytes = 32;

struct TableRegionHeader
{
    uint32_t magic;
    uint32_t layout_id;
    uint32_t bytes;
    uint32_t reserved[5];
};

inline void TableCookedHeaderWrite( uint8_t * at, uint32_t layout_id, uint32_t bytes )
{
    TableRegionHeader header = {};
    header.magic = kTableCookedMagic;
    header.layout_id = layout_id;
    header.bytes = bytes;
    memcpy( at, &header, sizeof( header ) );
}

// TableCookedHeaderCheck refuses a region this build cannot point at. Every
// refusal is loud and the caller's fallback is a real wire Load.
inline bool TableCookedHeaderCheck( const uint8_t * bytes, int64_t size, uint32_t layout_id, int64_t * region_bytes )
{
    if ( bytes == NULL || size < kTableCookedHeaderBytes ) { return false; }
    if ( ( ( (uintptr_t) bytes ) & ( kTableAlign - 1 ) ) != 0 ) { return false; } // an unaligned base cannot be pointed at
    TableRegionHeader header;
    memcpy( &header, bytes, sizeof( header ) );
    if ( header.magic != kTableCookedMagic ) { return false; } // wrong form, or foreign byte order
    if ( header.layout_id != layout_id ) { return false; }     // schema or ABI drift: regenerate the cooked file
    if ( (int64_t) header.bytes > size - kTableCookedHeaderBytes ) { return false; }
    *region_bytes = (int64_t) header.bytes;
    return true;
}

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}
