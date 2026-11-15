// The VARIABLE-LENGTH table runtime, emitted once per package and only when
// the unit declares a table with a pointer anywhere in its by-value closure
// (docs/SPEC-TABLES.md §2). A unit of pointer-free tables emits none of this and
// its generated output is byte-identical to a build with this file deleted —
// that zero-cost property is the point of deriving the mode.
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
package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppBlobThunks is what the numbering stores for a BYTE BUFFER record
// (docs/SPEC-TABLES.md §2.5, §3.1), and it is emitted only into a unit that
// DECLARES one. The two are named at exactly one site in this emitter — the
// `blob:` arm of the pointer edge walk in pointers.go — so a unit that
// declares no *bytes and no *string has no call site for them. The two
// RESERVED TYPE IDS stay in EVERY pointered unit: the retain path compares
// against them whether or not this unit writes a blob of its own.
//
// THE CENSUS IS ir.BlobPointerFields AND NOT ir.PointerReachableBlobs, though
// the two agree on every schema the corpus holds. The emitter's pointer edge
// walk and the numbering walk ir.PointerReachableBlobs takes its census with
// are two pieces of code, not one, and a gate that emits a DEFINITION must not
// rest on the second agreeing with the first: the arms gate's negative
// controls (schema#565) sabotage the numbering walk on purpose, and a census
// taken with it then withholds a definition the emitter's own walk has already
// written a call to — the control dies in the compiler instead of turning the
// gate red on its CHECK. A declaration scan is a superset of both walks and
// cannot lose that race.
const cppBlobThunks = `
template <typename Ctx>
inline int64_t TableBlobMeasureThunk( const void *, const TableNumbering &, TableIds &, const void * node )
{
    return (int64_t) ( (const TableBlob *) node )->length;
}

template <typename Ctx>
inline bool TableBlobSaveThunk( const void *, const TableNumbering &, TableWriter & w, TableIds &, const void * node )
{
    const TableBlob * blob = (const TableBlob *) node;
    w.raw( (const void *) ( blob + 1 ), (int64_t) blob->length );
    return true;
}
`

// tableArenaRuntime is the variable-length runtime, guarded per package like
// tablePrimitives so one definition survives any include order.
func tableArenaRuntime(u *ir.Unit, anyExtent bool) string {
	pkg := u.Package
	align := ir.TableRegionAlign(u)
	alignComment := fmt.Sprintf("every node starts %d-aligned", align)
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_ARENA"
	// THE BLOB THUNKS, only where a blob is declared. The census is taken over
	// the WHOLE unit rather than one root, because the arena runtime sits
	// behind ONE package-scoped guard: whichever file a translation unit
	// includes first defines it for every other file of the package.
	blobThunks := "\n" // the blank line the block used to open with
	if unitDeclaresBlob(u) {
		blobThunks = cppBlobThunks
	}
	// A UNIT WITH NEITHER A MAP NOR A LIST CARRIES NOT ONE SYMBOL OF THE EXTENT
	// MACHINERY (docs/SPEC-TABLES.md §2.2, §2.8, §2.9), the node map's extent
	// cursor included: a pointered unit with neither emits exactly the arena
	// runtime it always emitted, to the byte.
	// AllocRaw is an EXTENT symbol (docs/SPEC-TABLES.md §2.8, §2.9) and stays
	// out of such a unit's header with the rest of them: nothing else allocates
	// storage that is not a node.
	allocRaw := ""
	carveDecl, carveMember := "\n", ""
	if anyExtent {
		allocRaw = `    // RAW, ZEROED storage of the bytes asked for, at the alignment asked for: a MAP's or a LIST's builder
    // head and its segments (docs/SPEC-TABLES.md §2.8, §2.9). It is not a node: it carries
    // no type id, takes no index and has no Reset, so it goes through the same
    // slab and span the blob path uses rather than through Alloc.
    uint8_t * AllocRaw( int64_t bytes, int64_t align, uint32_t & at )
    {
        at = 0;
        if ( arena == NULL || arena->locked ) { return NULL; }
        if ( bytes <= 0 || align > (int64_t) kTableAlign ) { return NULL; }
        const int64_t rounded = TableAlignUp64( bytes );
        if ( rounded > (int64_t) kTableSlabBytes )
        {
            at = TableArenaGrabSpan( *arena, rounded );
            if ( at == kTableAllocFailed )
            {
                at = 0;
                return NULL;
            }
            return TableArenaAt( *arena, at );
        }
        if ( end == 0 || next + (uint32_t) rounded > end )
        {
            uint32_t offset = TableArenaGrabSlab( *arena );
            if ( offset == kTableAllocFailed ) { return NULL; }
            next = offset;
            end = offset + kTableSlabBytes;
            if ( next == 0 ) { next = kTableAlign; } // offset 0 is null: the arena's head stays reserved
        }
        at = next;
        next += (uint32_t) rounded;
        return TableArenaAt( *arena, at ); // the segment came back zeroed
    }
`
		carveDecl = "\n\n// the node's extent cursor, defined with the extent runtime (docs/SPEC-TABLES.md\n// §2.8, §2.9); the node map names it only through a pointer.\nstruct TableExtentCarve;\n"
		carveMember = "\n    // WHERE A MAP'S ENTRIES AND A LIST'S ELEMENTS LAND while this node's body\n" +
			"    // decodes (docs/SPEC-TABLES.md §2.8, §2.9): the node's own extent on the\n" +
			"    // region path and the builder's arena on the tool's. It is MUTABLE\n" +
			"    // because the cursor belongs to ONE node's decode and the dispatch that\n" +
			"    // owns that node holds the map by const reference, exactly as it did\n" +
			"    // before either construct existed. The decoder's signature does not\n" +
			"    // move for a construct it may not carry.\n    mutable TableExtentCarve * carve = NULL;\n" +
			"    // and the TOOL's path's allocation front, set once: there the arrays\n" +
			"    // are the builder's arena's rather than a node's extent.\n" +
			"    TableWorker * worker = NULL;\n" +
			"    // THE TOOL PATH'S REFUSAL (docs/SPEC-TABLES.md §2.9): a count above the\n" +
			"    // int32 cap met while a body decoded. LoadBuilder answers NULL for it\n" +
			"    // and moves no counter; mutable for the reason the cursor is.\n" +
			"    mutable bool refused = false;"
	}

	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// ---- variable-length tables: tuning constants (docs/SPEC-TABLES.md) ----
//
// The segment size and the count multiply to exactly 2^32: the u32 reference
// is the arena's hard ceiling, and these constants saturate it rather than
// leaving address space unreachable. Slab handout costs one atomic per slab,
// so per-node allocation costs no synchronization at all.

static const uint32_t kTableSegmentBits = 22;                          // 4 MiB segments
static const uint32_t kTableSegmentSize = 1u << kTableSegmentBits;
static const uint32_t kTableSegmentMask = kTableSegmentSize - 1u;
static const uint32_t kTableMaxSegments = 1u << ( 32 - kTableSegmentBits ); // 1024 -> 4 GiB
static const uint32_t kTableSlabBytes = 64u * 1024u;                 // one atomic per slab
static const uint32_t kTableAlign = ` + fmt.Sprint(align) + `;` + strings.Repeat(" ", 28-len(fmt.Sprint(align))) + `// ` + alignComment + `
static const uint32_t kTableAllocFailed = 0xFFFFFFFFu;

// ---- THE CALLER'S ALLOCATOR (docs/SPEC-TABLES.md §6.5) ----
//
// Every allocation the variable-length runtime makes goes through one of
// these — the arena's segments, the pack walk's identity map, the numbering's
// entry array, the packed region, and the tool path's node directory. There is
// no other call to the C library on this path, so a counting allocator sees
// every byte and a game's own heap can own all of it.
//
// It is the shape TableBlockAllocator already has (§19.1): two function
// pointers and a context the caller carries. What it adds is a CONTRACT ON
// alloc — the bytes come back ZEROED. Lock copies whole nodes, PADDING
// INCLUDED, so anything left uninitialized reaches a packed region; the default
// pair reaches that through calloc, which costs nothing measurable because a
// fresh segment is untouched pages either way.
struct TableAllocator
{
    void * ( *alloc )( void * context, int64_t bytes ); // ZEROED bytes, NULL on failure
    void ( *free )( void * context, void * pointer );
    void * context;
};

// The default pair, and it is the one every entry point takes when the caller
// names none. It calls schema_allocate / schema_release, so a program with its
// own C-library replacement can move the floor without writing a struct at all.
inline void * table_default_alloc( void * context, int64_t bytes )
{
    (void) context;
    return schema_allocate( bytes );
}
inline void table_default_free( void * context, void * pointer )
{
    (void) context;
    schema_release( pointer );
}

inline TableAllocator TableDefaultAllocator()
{
    TableAllocator allocator;
    allocator.alloc = table_default_alloc;
    allocator.free = table_default_free;
    allocator.context = NULL;
    return allocator;
}

// Both callbacks are required together. An incomplete pair refuses before the
// first callback, the same way C's table_allocate does.
inline bool table_allocator_complete( TableAllocator const & a )
{
    return a.alloc != NULL && a.free != NULL;
}
inline void * table_allocate( TableAllocator const & a, int64_t bytes )
{
    if ( !table_allocator_complete( a ) || bytes <= 0 ) return NULL;
    return a.alloc( a.context, bytes );
}
inline void table_release( TableAllocator const & a, void * pointer )
{
    if ( a.free != NULL ) a.free( a.context, pointer );
}

// ---- TableRef: a relocatable reference (never a machine pointer) ----
//
// Two encodings, one slot, and the FORM says which is in force:
//
//   in the arena  — the node's arena offset (segment index in the high bits)
//   in a region   — the SELF-RELATIVE byte delta from this slot's own address,
//                   so a deref is one add, needs no base pointer, and a whole
//                   region relocates by memcpy with zero fix-up
//
// 0 is null in both, and a slot can never name the node that contains it, so
// zero names nothing real in either form.
//
// A REGION DELTA HAS NO REQUIRED SIGN (§6.3). A region is packed depth-first,
// so a node's FIRST reference points forward; every LATER reference to that
// same node points BACK at the one body it already has, which is exactly what
// makes one node one node in a region. Sharing and a back-reference are the
// same fact, and nothing validates a reference by its sign.
//
// IT IS EIGHT BYTES, SIGNED, so ONE REGION REACHES EVERYTHING (§6.3, §7): a
// four-byte slot bounded a region at 2 GiB, and the scale a cook exists for is
// *"100mbs or many gigabytes of data in Assets.bin"*.
struct TableRef
{
    int64_t value = 0;
    bool null() const { return value == 0; }
};

// TableSlot is what Alloc hands back: usable as the node pointer (write
// fields through it) AND as the reference to store in a pointer field.
template <typename T>
struct TableSlot
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

// ---- a BYTE BUFFER's node (docs/SPEC-TABLES.md §2.5, §6.3) ----
//
// A *bytes or *string slot is a TableRef like every pointer slot, and it names
// a BLOB NODE: this eight-byte header and then the bytes, at offset eight so
// the data is eight-aligned. A *string blob carries one more zero byte after
// its data, so a region hands back a C string with no copy. The node's extent
// is the header plus its bytes, rounded to the arena's alignment like every
// node's; on the wire it is a record whose body is the bytes (§3.1).
struct TableBlob
{
    uint32_t length;
    uint32_t zero;
};

static const int64_t kTableBlobHeader = 8;             // length (u32), then four zero bytes
static const int64_t kTableBlobMaxLength = 0xFFFFFFFF; // a record's length is a u32 (§3.1)

// the node's storage: the header, the bytes, a string's terminator, rounded
// to the arena's alignment like every node
inline int64_t TableBlobStorage( int64_t length, bool terminated )
{
    return TableAlignUp64( kTableBlobHeader + length + ( terminated ? 1 : 0 ) );
}

// What a read answers: a pointer INTO the region and the length, NULL and
// zero for a null slot. Off a locked region, a loaded one or an opened cook
// the pointer is one add from the slot, and nothing is copied.
struct TableBytesView
{
    const uint8_t * data;
    int64_t length;
};

struct TableStringView
{
    const char * data; // zero-terminated
    int64_t length;
};

// What AllocBytes and AllocString hand back: the bytes to write through, the
// length asked for, and the reference to store in the slot — the three
// answers TableSlot gives for a table node.
struct TableBytesSlot
{
    uint8_t * data = NULL;
    int64_t length = 0;
    TableRef ref;
    bool null() const { return data == NULL; }
    operator TableRef() const { return ref; }
};

struct TableStringSlot
{
    char * data = NULL; // room for length bytes and the terminator, already zero
    int64_t length = 0;
    TableRef ref;
    bool null() const { return data == NULL; }
    operator TableRef() const { return ref; }
};

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
    std::atomic<uint8_t *> segments[kTableMaxSegments];
    std::atomic<uint32_t> cursor; // (segment << kTableSegmentBits) | bytes handed out
    bool locked = false;          // MONOTONIC: Lock() is one-way, there is no unlock
    // THE ARENA CARRIES ITS OWN, so everything downstream of a builder —
    // segments, pack map, numbering, region, node directory — allocates through
    // the one pair the caller named, with nothing to thread by hand.
    TableAllocator allocator;
};

inline void TableArenaInit( TableArena & arena, TableAllocator allocator )
{
    for ( uint32_t i = 0; i < kTableMaxSegments; i++ )
    {
        arena.segments[i].store( NULL, std::memory_order_relaxed );
    }
    arena.cursor.store( 0, std::memory_order_relaxed );
    arena.locked = false;
    arena.allocator = allocator;
}

inline void TableArenaShutdown( TableArena & arena )
{
    for ( uint32_t i = 0; i < kTableMaxSegments; i++ )
    {
        uint8_t * segment = arena.segments[i].exchange( NULL, std::memory_order_acq_rel );
        if ( segment != NULL ) { table_release( arena.allocator, segment ); }
    }
    arena.cursor.store( 0, std::memory_order_relaxed );
}

// one L1 load plus an add: the segment table is 8 KiB and stays hot
inline uint8_t * TableArenaAt( const TableArena & arena, uint32_t offset )
{
    return arena.segments[offset >> kTableSegmentBits].load( std::memory_order_relaxed ) + ( offset & kTableSegmentMask );
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
                // THE SEGMENT COMES BACK ZEROED, which is the allocator's
                // contract and not an extra pass here: Lock copies whole nodes,
                // PADDING INCLUDED, so anything uninitialized reaches a packed
                // region. Value-initializing a node with placement new zeroes
                // its MEMBERS and not its padding, so the zeroing has to happen
                // at the segment or not at all. It costs nothing measurable: a
                // fresh segment is untouched pages either way, and the default
                // pair's calloc has the kernel hand them over zeroed.
                uint8_t * memory = (uint8_t *) table_allocate( arena.allocator, (int64_t) kTableSegmentSize );
                if ( memory == NULL ) { return kTableAllocFailed; }
                uint8_t * expected = NULL;
                if ( !arena.segments[segment].compare_exchange_strong( expected, memory, std::memory_order_acq_rel ) )
                {
                    // another worker published this segment first
                    table_release( arena.allocator, memory );
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

// TableArenaGrabSpan reserves a SPAN of the arena's address space for one node
// larger than a slab — a BYTE BUFFER of any size (docs/SPEC-TABLES.md §2.5) —
// and allocates it as one contiguous block. It takes whole segment indices
// from the cursor, starting at the index after the cursor's so nothing else
// is ever handed out inside the span, and publishes the block under the first
// of them; the indices the span covers past that one stay NULL, which is
// enough, because only a node's START is ever resolved through the segment
// table and a blob's bytes follow its header inside the one allocation. The
// unused tail of the segment the cursor was in is slack, like a slab tail.
// Returns kTableAllocFailed when the address space or the allocator is
// exhausted — a loud refusal, never a smaller blob.
inline uint32_t TableArenaGrabSpan( TableArena & arena, int64_t bytes )
{
    if ( bytes <= 0 || bytes > ( (int64_t) kTableMaxSegments - 2 ) * (int64_t) kTableSegmentSize ) { return kTableAllocFailed; }
    const uint32_t spanned = (uint32_t) ( ( bytes + kTableSegmentSize - 1 ) >> kTableSegmentBits );
    for ( ;; )
    {
        uint32_t cursor = arena.cursor.load( std::memory_order_acquire );
        uint32_t start = ( cursor >> kTableSegmentBits ) + 1;
        if ( start + spanned >= kTableMaxSegments ) { return kTableAllocFailed; } // 4 GiB: the u32 reference's ceiling
        uint32_t next = ( start + spanned ) << kTableSegmentBits;
        if ( !arena.cursor.compare_exchange_weak( cursor, next, std::memory_order_acq_rel ) ) { continue; }
        // the span is this worker's now: nothing else can publish under its
        // first index, so a plain store suffices, and the block comes back
        // ZEROED like every segment — the blob's bytes and its tail are zeros
        // until written
        uint8_t * memory = (uint8_t *) table_allocate( arena.allocator, bytes );
        if ( memory == NULL ) { return kTableAllocFailed; }
        arena.segments[start].store( memory, std::memory_order_release );
        return start << kTableSegmentBits;
    }
}

// ---- TableWorker: one thread's allocation front ----
//
// The threading contract, stated plainly:
//   * Alloc on YOUR OWN worker is safe concurrently with any other worker's.
//     No locks, no atomics per node.
//   * Writing fields of a node ANOTHER worker allocated is your own
//     synchronization problem — this runtime does not arbitrate it.
//   * Lock and Save are single-threaded: call them after the workers have
//     joined.
struct TableWorker
{
    TableArena * arena = NULL;
    uint32_t next = 0;
    uint32_t end = 0;

    template <typename T>
    TableSlot<T> Alloc()
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
        // A NODE IS BORN IN TWO HALVES: start its lifetime in the raw
        // storage, then write the declared defaults ONE MEMBER AT A TIME.
        //
        // It is "T", not "T{}". Value-initialising the whole aggregate says
        // the same thing and costs cl O(BYTES) TO COMPILE — it expands element
        // by element in its front end — while both halves here cost
        // O(declarations). The slab cap below refuses a large node at RUN
        // TIME and bounds nothing at compile time: the cost is paid by
        // whatever T a caller instantiates this with.
        // Padding is not the difference: value-initialisation zeroes MEMBERS
        // and not padding either way, which is why the segment is calloc'd.
        //
        // TableReset is an OVERLOAD SET, one per closure member, reached from
        // this template by argument-dependent lookup on T's own namespace —
        // Alloc is a template and cannot spell <Name>Reset.
        //
        // The reset is here because ONE DEFINITION SAYS WHAT THE DECLARED
        // DEFAULTS ARE, and it is <Name>Reset. Default-initialisation lands on
        // the same values today, because a member with a non-zero default
        // carries a member initializer that says so — but that is the class
        // definition agreeing with Reset, not the arena reading it, and #320's
        // fix was itself a pass that MOVED initialisation between the two.
        // The arena reads the definition.
        slot.ptr = new ( TableArenaAt( *arena, at ) ) T;
        TableReset( *slot.ptr );
        slot.ref.value = at;
        return slot;
    }

    // Alloc a BYTE BUFFER's node of exactly length bytes (docs/SPEC-TABLES.md
    // §2.5): the blob header and its bytes, zeroed, in this thread's slab when
    // it fits and in a span of the arena's own when it does not. NULL is the
    // arena locked, a length below zero or past a record's u32, or the
    // allocator refusing. The offset comes back for the reference.
    TableBlob * AllocBlob( int64_t length, bool terminated, uint32_t & at )
    {
        at = 0;
        if ( arena == NULL || arena->locked ) { return NULL; }
        if ( length < 0 || length > kTableBlobMaxLength ) { return NULL; }
        const int64_t bytes = TableBlobStorage( length, terminated );
        if ( bytes > (int64_t) kTableSlabBytes )
        {
            at = TableArenaGrabSpan( *arena, bytes );
            if ( at == kTableAllocFailed )
            {
                at = 0;
                return NULL;
            }
        }
        else
        {
            if ( end == 0 || next + (uint32_t) bytes > end )
            {
                uint32_t offset = TableArenaGrabSlab( *arena );
                if ( offset == kTableAllocFailed ) { return NULL; }
                next = offset;
                end = offset + kTableSlabBytes;
                if ( next == 0 ) { next = kTableAlign; } // offset 0 is null: the arena's head stays reserved
            }
            at = next;
            next += (uint32_t) bytes;
        }
        TableBlob * blob = (TableBlob *) TableArenaAt( *arena, at );
        blob->length = (uint32_t) length; // the bytes after it are the segment's zeros
        blob->zero = 0;
        return blob;
    }

` + allocRaw + `    // a *bytes node: the bytes to write through, and the reference to store
    TableBytesSlot AllocBytes( int64_t length )
    {
        TableBytesSlot slot;
        uint32_t at = 0;
        TableBlob * blob = AllocBlob( length, false, at );
        if ( blob == NULL ) { return slot; }
        slot.data = (uint8_t *) ( blob + 1 );
        slot.length = length;
        slot.ref.value = at;
        return slot;
    }

    // a *string node: room for length bytes and the zero byte after them
    TableStringSlot AllocString( int64_t length )
    {
        TableStringSlot slot;
        uint32_t at = 0;
        TableBlob * blob = AllocBlob( length, true, at );
        if ( blob == NULL ) { return slot; }
        slot.data = (char *) ( blob + 1 );
        slot.length = length;
        slot.ref.value = at;
        return slot;
    }
};

// ---- TablePackMap: the pack walk's identity map (docs/SPEC-TABLES.md §3.1, §6.2) ----
//
// ONE ENTRY PER REACHABLE NODE, and that map IS identity: a node must know
// where it landed to be named a second time, so Lock packs a shared node ONCE
// and every later reference resolves to the one body it already has. That is
// the same first-visit numbering the wire uses, so the pack order and the node
// order are one order.
//
// COLOURING AN ENTRY WHILE ITS DESCENT IS OPEN COSTS ONE BIT, and it is what
// makes a data cycle free to refuse: a reference to an entry still open is a
// cycle, and Lock returns failure rather than recursing away. The ROOT's entry
// is open for the whole walk.
//
// The map is proportional to NODES, never to bytes, and it lives on the
// AUTHORING side, where §6.5 licenses allocation. Nothing on the reading path
// ever builds one.
struct TablePackEntry
{
    const void * key;   // the node's address in the graph being packed
    int64_t offset;     // where that node landed in the region
    uint8_t open;       // its descent is still open: a reference here is a cycle
};

struct TablePackMap
{
    TablePackEntry * entries = NULL;
    int64_t capacity = 0; // a power of two, or zero while empty
    int64_t count = 0;
    TableAllocator allocator; // the caller's, carried from the walk that built it
};

inline void TablePackMapInit( TablePackMap & map, TableAllocator allocator )
{
    map.entries = NULL;
    map.capacity = 0;
    map.count = 0;
    map.allocator = allocator;
}

inline void TablePackMapShutdown( TablePackMap & map )
{
    table_release( map.allocator, map.entries );
    TablePackMapInit( map, map.allocator );
}

// The two walks behind Lock re-derive the SAME map from the same graph — the
// numbering is never carried between them (§3.1) — so the second starts from
// an empty map and keeps the capacity the first paid for.
inline void TablePackMapReset( TablePackMap & map )
{
    if ( map.entries != NULL ) { memset( map.entries, 0, (size_t) map.capacity * sizeof( TablePackEntry ) ); }
    map.count = 0;
}

// open addressing, linear probing, a multiply-shift hash over the address: a
// node key is a pointer and its low bits are alignment, so the low bits alone
// would collide on every node of one type
inline int64_t TablePackMapSlot( const TablePackMap & map, const void * key )
{
    uint64_t hash = (uint64_t) (uintptr_t) key;
    hash *= 0x9E3779B97F4A7C15ull;
    hash ^= hash >> 29;
    int64_t mask = map.capacity - 1;
    int64_t at = (int64_t) ( hash & (uint64_t) mask );
    while ( map.entries[at].key != NULL && map.entries[at].key != key )
    {
        at = ( at + 1 ) & mask;
    }
    return at;
}

inline TablePackEntry * TablePackMapFind( TablePackMap & map, const void * key )
{
    if ( map.capacity == 0 ) { return NULL; }
    TablePackEntry * entry = &map.entries[TablePackMapSlot( map, key )];
    return entry->key == key ? entry : NULL;
}

// QUADRUPLING, not doubling, and the reason is measured: growth rehashes every
// entry, and on a graph of 131,071 nodes the doubling schedule spent 45% of
// Lock in rehashing alone. Quadrupling from 1024 buys 1.35x on that graph and
// keeps the map NODE-proportional (§6.2) — under 128 bytes a node at its
// worst, right after a grow, and about 64 on average.
inline bool TablePackMapGrow( TablePackMap & map )
{
    TablePackMap grown;
    grown.allocator = map.allocator;
    grown.capacity = map.capacity != 0 ? map.capacity * 4 : 1024;
    grown.entries = (TablePackEntry *) table_allocate( map.allocator, grown.capacity * (int64_t) sizeof( TablePackEntry ) );
    if ( grown.entries == NULL ) { return false; }
    for ( int64_t i = 0; i < map.capacity; i++ )
    {
        if ( map.entries[i].key == NULL ) { continue; }
        grown.entries[TablePackMapSlot( grown, map.entries[i].key )] = map.entries[i];
        grown.count++;
    }
    table_release( map.allocator, map.entries );
    map = grown;
    return true;
}

// REACH a node: one probe answers both questions the walk has. A true "taken"
// says this is a FIRST visit, and the entry is now the node's, coloured open
// at "offset"; otherwise the entry is the one the node already has, and its
// open bit says cycle or sharing. NULL is an allocation failure, and it is a
// refusal like any other: Lock fails rather than packing a graph it cannot
// track.
//
// It is one call and not a find followed by an insert because the walk asks
// this question twice per node — once to measure, once to pack — and every
// probe is a miss into a table larger than L2.
inline TablePackEntry * TablePackMapReach( TablePackMap & map, const void * key, int64_t offset, bool & taken, int64_t & slot )
{
    if ( ( map.count + 1 ) * 4 >= map.capacity * 3 ) // keep the load factor under three quarters
    {
        if ( !TablePackMapGrow( map ) ) { return NULL; }
    }
    slot = TablePackMapSlot( map, key );
    TablePackEntry * entry = &map.entries[slot];
    taken = entry->key != key; // an empty slot is a first visit; the key is never NULL
    if ( taken )
    {
        entry->key = key;
        entry->offset = offset;
        entry->open = 1;
        map.count++;
    }
    return entry;
}

// The descent finished: the node keeps its entry — identity outlives the
// descent — and stops being a cycle. The "hint" is the slot Reach returned, and it
// is checked against the key rather than trusted, so a rehash between the two
// costs a second probe instead of correctness.
inline void TablePackMapClose( TablePackMap & map, const void * key, int64_t hint )
{
    if ( hint >= 0 && hint < map.capacity && map.entries[hint].key == key )
    {
        map.entries[hint].open = 0;
        return;
    }
    TablePackEntry * entry = TablePackMapFind( map, key );
    if ( entry != NULL ) { entry->open = 0; }
}

// ---- resolution contexts: which encoding a walk is reading ----

struct TableArenaCtx
{ const TableArena * arena; };
struct TableRegionCtx
{};

// ---- a BYTE BUFFER's resolution (docs/SPEC-TABLES.md §2.5, §6.3) ----
//
// The same two encodings a table pointer has, resolved the same way: a
// self-relative delta in a region — one add, no base — and an arena offset
// while the builder is mutable. The blob is reached through its header, and a
// view is the header plus eight and the header's first word. Nothing here
// allocates and nothing copies: off a locked region, a loaded one or an
// opened cook the view points INTO the region.
//
// The add is done on the ADDRESS (uintptr_t), not on a pointer to the slot:
// the target lies elsewhere in the region, outside the eight-byte TableRef, so
// pointer arithmetic from &ref would leave the slot's object, and an optimizer
// that tracks object sizes (gcc 13 at -O2) then refuses strlen on a view whose
// terminator is really there. The same holds for every node's At.
inline const TableBlob * TableBlobAt( const TableRef & ref )
{
    return ref.value != 0 ? (const TableBlob *) ( (uintptr_t) &ref + (uintptr_t) ref.value ) : NULL;
}
inline const TableBlob * TableBlobAt( const TableRegionCtx &, const TableRef & ref ) { return TableBlobAt( ref ); }
inline const TableBlob * TableBlobAt( const TableArenaCtx & ctx, const TableRef & ref )
{
    return ref.value != 0 ? (const TableBlob *) TableArenaAt( *ctx.arena, (uint32_t) ref.value ) : NULL;
}
inline const TableBlob * TableBlobAt( const TableArena & arena, const TableRef & ref )
{
    return ref.value != 0 ? (const TableBlob *) TableArenaAt( arena, (uint32_t) ref.value ) : NULL;
}

inline TableBytesView TableBytesViewOf( const TableBlob * blob )
{
    TableBytesView view = { NULL, 0 };
    if ( blob != NULL )
    {
        view.data = (const uint8_t *) ( blob + 1 );
        view.length = (int64_t) blob->length;
    }
    return view;
}
inline TableStringView TableStringViewOf( const TableBlob * blob )
{
    TableStringView view = { NULL, 0 };
    if ( blob != NULL )
    {
        view.data = (const char *) ( blob + 1 );
        view.length = (int64_t) blob->length;
    }
    return view;
}

// the const form's hot path: one add, no base
inline TableBytesView TableBytesAt( const TableRef & ref ) { return TableBytesViewOf( TableBlobAt( ref ) ); }
inline TableStringView TableStringAt( const TableRef & ref ) { return TableStringViewOf( TableBlobAt( ref ) ); }
// and the context forms a walk uses: a region context, an arena context, or
// the arena itself while the builder is mutable
template <typename Ctx>
inline TableBytesView TableBytesAt( const Ctx & ctx, const TableRef & ref ) { return TableBytesViewOf( TableBlobAt( ctx, ref ) ); }
template <typename Ctx>
inline TableStringView TableStringAt( const Ctx & ctx, const TableRef & ref ) { return TableStringViewOf( TableBlobAt( ctx, ref ) ); }

// allocate a blob in the arena and point the slot at it; the slot holds the
// arena offset, as every slot does while the builder is mutable
inline uint8_t * TableBytesEmplace( TableWorker & worker, TableRef & slot, int64_t length )
{
    TableBytesSlot allocated = worker.AllocBytes( length );
    slot = allocated.ref;
    return allocated.data;
}
// the text is copied in when one is given; a NULL text leaves the zeros for
// the caller to fill
inline char * TableStringEmplace( TableWorker & worker, TableRef & slot, const char * text, int64_t length )
{
    TableStringSlot allocated = worker.AllocString( length );
    slot = allocated.ref;
    if ( allocated.data != NULL && text != NULL && length > 0 ) { memcpy( allocated.data, text, (size_t) length ); }
    return allocated.data;
}

// ---- the FLAT NODE TABLE (docs/SPEC-TABLES.md §3.1) ----
//
// A pointered save writes every reachable node ONCE, into a node table, and a
// pointer field rides as an INDEX into it under kind 17. The encoding is
// flat: no pointer edge is a nesting level, so a chain's length is not a depth,
// and two references to one node are one node.
//
// THE FIELD RIDES ONCE: an L with sixty-four bits of capability frames a
// numbering of any size, so the whole numbering is one contiguous payload and a
// save's node bodies have no aggregate ceiling.

static const uint64_t kTableNodeIndexNull = 0;         // absence and null are one value
static const uint64_t kTableNodeIndexRoot = 1;         // the body that hosts the table

// The not-materialized sentinel (§6.3): a record whose type id this build could
// not name. Distinct from every real offset including the root's 0, so an index
// resolving through it yields NULL and can never fabricate the root.
static const uint64_t kTableNodeAbsent = 0xFFFFFFFFFFFFFFFFull;

// What a node's storage answers when the FRAMING ITSELF is refused rather than
// merely unnameable: a count its L cannot carry, one above the int32 cap, or a
// blob past the size cap (docs/SPEC-TABLES.md §3.1, §6.5). An unnameable type
// id commands no storage and keeps its index. This one makes the whole measure
// answer -1 with its reason.
static const int64_t kTableNodeRefused = -2;

// ---- the numbering, on the SAVE side ----
//
// One entry per reachable node in FIRST-VISIT order, so entry k is node index
// k + 2. The two thunks are what let one loop write a table of mixed types: the
// numbering walk knows each target's type STATICALLY at the site it numbers it,
// so it stores the instantiation there and the loop never asks what a node is.
struct TableNumbering;

struct TableNodeEntry
{
    const void * node;
    uint64_t type_id;
    int64_t ( *measure )( const void * ctx, const TableNumbering & numbering, TableIds & ids, const void * node );
    bool ( *save )( const void * ctx, const TableNumbering & numbering, TableWriter & w, TableIds & ids, const void * node );
    // The same entry drives Lock's region pack: one node's own bytes, its copy
    // and extent, and the rewrite of the slots inside it. They are per node so
    // no pass descends a pointer; the numbering's order IS the region's order
    // (docs/SPEC-TABLES.md §3.1, §6.3).
    int64_t ( *pack_bytes )( const void * ctx, const void * node );
    bool ( *pack_copy )( const void * ctx, const void * src, void * dst, uint8_t * base, int64_t capacity );
    bool ( *pack_slots )( const void * ctx, TablePackMap & seen, const void * src, void * dst, uint8_t * base );
    // The numbering's pending work: one node still to number, or a close marker
    // that ends its descent. The stack is the explicit work list that replaces
    // a call per pointer edge (docs/SPEC-TABLES.md §3.1).
    bool ( *walk )( const void * ctx, TableNumbering & numbering, const void * node );
    bool close;
    int64_t hint;
};

struct TableNumbering
{
    TablePackMap seen;              // node -> index; the ROOT is index 1, open for the whole walk
    TableNodeEntry * entries = NULL;
    int64_t count = 0;
    int64_t capacity = 0;
    // THE PENDING WORK, a stack of node entries in first-visit order.
    TableNodeEntry * work = NULL;
    int64_t work_count = 0;
    int64_t work_capacity = 0;
    // ONE BLOB RECORD NAMED FROM TWO SLOTS (docs/SPEC-TABLES.md §2.5, §16.7):
    // every writer refuses the graph by name, blob_shared, rather than writing
    // a copy per slot. The Go oracle is internal/codegen/gotable/regions.go
    // (TableNumbering.shared).
    bool shared = false;
};

// The numbering allocates through the map's pair rather than carrying a second
// copy of it: one numbering is one walk, and a walk has one allocator.
inline void TableNumberingInit( TableNumbering & n, TableAllocator allocator )
{
    TablePackMapInit( n.seen, allocator );
    n.entries = NULL;
    n.count = 0;
    n.capacity = 0;
    n.work = NULL;
    n.work_count = 0;
    n.work_capacity = 0;
    n.shared = false;
}

inline void TableNumberingShutdown( TableNumbering & n )
{
    TableAllocator allocator = n.seen.allocator;
    TablePackMapShutdown( n.seen );
    table_release( allocator, n.entries );
    table_release( allocator, n.work );
    n.entries = NULL;
    n.count = 0;
    n.capacity = 0;
    n.work = NULL;
    n.work_count = 0;
    n.work_capacity = 0;
}

// The index a numbered node was given, for the save that writes it into a
// pointer slot. False means the two walks disagree about the graph, which is a
// refusal and never a guess.
inline bool TableNumberingIndex( const TableNumbering & n, const void * node, uint64_t & index )
{
    if ( n.seen.capacity == 0 ) { return false; }
    const TablePackEntry & entry = n.seen.entries[TablePackMapSlot( n.seen, node )];
    if ( entry.key != node ) { return false; }
    index = (uint64_t) entry.offset;
    return true;
}

inline bool TableNumberingAppend( TableNumbering & n, const TableNodeEntry & entry )
{
    if ( n.count == n.capacity )
    {
        // GROW BY COPY, never by realloc: the allocator hook is a PAIR, and a
        // game's heap is not required to have a resize primitive at all. The
        // schedule quadruples, so the copying is amortized to a constant per
        // entry and the growth is the same growth it always was.
        int64_t capacity = n.capacity != 0 ? n.capacity * 4 : 256;
        TableAllocator allocator = n.seen.allocator;
        TableNodeEntry * grown = (TableNodeEntry *) table_allocate( allocator, capacity * (int64_t) sizeof( TableNodeEntry ) );
        if ( grown == NULL ) { return false; }
        if ( n.entries != NULL )
        {
            memcpy( grown, n.entries, (size_t) n.count * sizeof( TableNodeEntry ) );
            table_release( allocator, n.entries );
        }
        n.entries = grown;
        n.capacity = capacity;
    }
    n.entries[n.count++] = entry;
    return true;
}

// TableNumberingWorkPush appends one pending node (or close marker) to the
// explicit work list. It grows by copy on the same allocator the numbering
// carries, so the walk itself has no recursion and no fixed depth.
inline bool TableNumberingWorkPush( TableNumbering & n, const TableNodeEntry & entry )
{
    if ( n.work_count == n.work_capacity )
    {
        int64_t capacity = n.work_capacity != 0 ? n.work_capacity * 4 : 256;
        TableAllocator allocator = n.seen.allocator;
        TableNodeEntry * grown = (TableNodeEntry *) table_allocate( allocator, capacity * (int64_t) sizeof( TableNodeEntry ) );
        if ( grown == NULL ) { return false; }
        if ( n.work != NULL )
        {
            memcpy( grown, n.work, (size_t) n.work_count * sizeof( TableNodeEntry ) );
            table_release( allocator, n.work );
        }
        n.work = grown;
        n.work_capacity = capacity;
    }
    n.work[n.work_count++] = entry;
    return true;
}

// The thunks the numbering stores. Each resolves to the closure member's own
// MeasureBody / SaveBodyFields through an overload set in the member's DECLARING
// file, reached by argument-dependent lookup at instantiation — the same bridge
// the arena's TableReset uses, and the reason a numbering may span the files of
// one unit without any file naming another's members.
template <typename Ctx, typename T>
inline int64_t TableNodeMeasureThunk( const void * ctx, const TableNumbering & numbering, TableIds & ids, const void * node )
{
    return TableNodeMeasure( *(const Ctx *) ctx, numbering, ids, *(const T *) node );
}

template <typename Ctx, typename T>
inline bool TableNodeSaveThunk( const void * ctx, const TableNumbering & numbering, TableWriter & w, TableIds & ids, const void * node )
{
    return TableNodeSave( *(const Ctx *) ctx, numbering, w, ids, *(const T *) node );
}
// ---- a BYTE BUFFER's record (docs/SPEC-TABLES.md §2.5, §3.1) ----
//
// A blob rides as a node record under one of two RESERVED type ids — the fold
// a table's name takes, over the keywords "bytes" and "string", which no table
// can be named — with the bytes as its body and nothing framed inside. These
// two thunks are what the numbering stores for a blob, as it stores a
// member's codec for a table: the length, and the bytes verbatim.
static const uint64_t kTableBytesTypeId = ` + fmt.Sprintf("0x%016xull", ir.BytesWireTypeId) + `;  // fnv1a64( "bytes" )
static const uint64_t kTableStringTypeId = ` + fmt.Sprintf("0x%016xull", ir.StringWireTypeId) + `; // fnv1a64( "string" )
` + blobThunks + `// TableNodeTableMeasure and TableNodeTableSave are the framing, and they are
// ONE fill rule written twice — measure derives it from the graph and save
// derives the same one, which is what makes measure == save hold across a
// pointer graph (§3.1).
//
// The field rides ONCE, under the reserved id, kind 12: the payload opens with
// the count and then carries the records back to back, each a type id
// REFERENCE, a length and a body. The reserved id is interned BEFORE the
// records, and a record's type id before its body, which is the first-use order
// the trailer is written in (§3).
template <typename Ctx>
inline int64_t TableNodeTablePayload( const Ctx & ctx, TableIds & ids, const TableNumbering & n )
{
    int64_t payload = TableLebBytes( (uint64_t) n.count );
    for ( int64_t k = 0; k < n.count; k++ )
    {
        payload += TableLebBytes( ids.ref( n.entries[k].type_id ) );
        const int64_t body = n.entries[k].measure( (const void *) &ctx, n, ids, n.entries[k].node );
        if ( body < 0 ) { return -1; }
        payload += TableLebBytes( (uint64_t) body ) + body;
    }
    return payload;
}

template <typename Ctx>
inline int64_t TableNodeTableMeasure( const Ctx & ctx, TableIds & ids, const TableNumbering & n )
{
    if ( n.count == 0 ) { return 0; } // a root that reaches no nodes writes none of them
    const uint64_t ref = ids.ref( kTableNodeTableFieldId );
    const int64_t payload = TableNodeTablePayload( ctx, ids, n );
    if ( payload < 0 ) { return -1; }
    return TableLebBytes( ref ) + 1 + TableLebBytes( (uint64_t) payload ) + payload;
}

template <typename Ctx>
inline bool TableNodeTableSave( const Ctx & ctx, TableWriter & w, TableIds & ids, const TableNumbering & n )
{
    if ( n.count == 0 ) { return true; }
    const uint64_t ref = ids.ref( kTableNodeTableFieldId );
    const int64_t payload = TableNodeTablePayload( ctx, ids, n );
    if ( payload < 0 ) { return false; }
    w.header( ref, 12 ); // kind 12 is the opaque byte payload: a reader that cannot name the id skips by L
    w.putleb( (uint64_t) payload );
    w.putleb( (uint64_t) n.count );
    for ( int64_t k = 0; k < n.count; k++ )
    {
        w.putleb( ids.ref( n.entries[k].type_id ) );
        const int64_t body = n.entries[k].measure( (const void *) &ctx, n, ids, n.entries[k].node );
        if ( body < 0 ) { return false; }
        w.putleb( (uint64_t) body );
        if ( !n.entries[k].save( (const void *) &ctx, n, w, ids, n.entries[k].node ) ) { return false; }
    }
    return true;
}

// ---- the numbering, on the LOAD side: a region's NODE DIRECTORY (§6.3) ----
//
// The wire's numbering made resident: one entry per numbered node, in index
// order, position i describing node index i + 1 — so position 0 is the ROOT at
// offset 0. It is ATTRIBUTION, and attribution is separable: nothing that reads
// a structure touches it, a deref is one add on a self-relative offset, and a
// caller may release it once Load returns.
struct TableNodeDirEntry
{
    uint64_t offset;
    uint64_t type_id;
};` + carveDecl + `
// TableNodeMap is what a pointer slot resolves through while a body decodes.
struct TableNodeMap
{
    uint8_t * base = NULL;
    // MUTABLE because an ONCE read marks a blob's entry when a slot first
    // names it (docs/SPEC-TABLES.md §2.5): a second slot names the same node
    // and is counted, never decoded again.
    TableNodeDirEntry * entries = NULL;
    int64_t count = 0;   // the ROOT's entry included, so it is records + 1
    bool good = false;   // the node table read whole; a numbering that failed resolves nothing
    // WHERE THE NODES LIVE, and therefore what a resolved slot holds: a region
    // takes the SELF-RELATIVE delta so a deref is one add, and the tool's
    // builder path takes the node's ARENA OFFSET (§6.3).
    bool arena = false;` + carveMember + `
    // During construction, keep validated TABLE indices for the cycle check.
    // Blob references resolve normally. No root is returned in this state.
    bool cycle_indices = false;
    // ONCE is the file and message REGION reads' rule (§2.5, §16.7): the first
    // slot naming a blob marks its directory entry, and every later slot naming
    // it is one blob_shared event and resolves to the ONE decoded node. The Go
    // oracle is internal/codegen/gotable/regions.go (TableNodeMap.Once).
    bool once = false;
    // DECLARED answers whether this reader declares a record's type id, so a
    // record it placed no storage for can still be told apart: a type it does
    // not declare reads null, and a type it declares that is not the slot's
    // target is a wrong-type pointer target (docs/SPEC-TABLES.md §3.1).
    bool ( *declared )( uint64_t type_id ) = NULL;
};

// TableNodeWrongTarget: a pointer naming a record of a type this reader
// declares that is not the type its slot requires. It is damage, so every read,
// the retaining one included, discards the whole load (docs/SPEC-TABLES.md
// §3.1, §4); the reason stays the one the strict read has always named.
inline void TableNodeWrongTarget( TableReport * report )
{
    report->damaged = true;
    report->refused = true;
    report->refuse_reason = kind_mismatch;
}

// TableNodeResolve places one node index in a pointer slot, and every failure
// is one of §4's events with the pointer left null. The declared TARGET type id
// is checked at every index, the root's included: the root carries no record
// and therefore no wire type id, so the READER'S OWN root type is what the
// claim is checked against.
inline void TableNodeResolve( const TableNodeMap & map, TableRef & slot, uint64_t index, uint64_t target, TableReport * report )
{
    slot.value = 0;
    if ( index == kTableNodeIndexNull || !map.good ) { return; }
    if ( index - 1 >= (uint64_t) map.count )
    {
        report->damaged = true; // an index above node_count + 1
        return;
    }
    const TableNodeDirEntry & entry = map.entries[index - 1];
    if ( entry.offset == kTableNodeAbsent )
    {
        // a node whose type id this build could not name KEEPS ITS INDEX, and
        // every pointer naming it reads null. The unknown was counted once, at
        // the node, not once per pointer. A node of a type this reader DOES
        // declare that is not the slot's target is a wrong-type pointer
        // target, below, though this root placed no storage for it.
        if ( entry.type_id != target && map.declared != NULL && map.declared( entry.type_id ) )
        {
            TableNodeWrongTarget( report );
        }
        return;
    }
    if ( entry.type_id != target )
    {
        // A blob named from two slots (docs/SPEC-TABLES.md §2.5, §16.7): the
        // first slot marked the entry with the complement of its type id, so a
        // second slot naming it is blob_shared and still resolves to the one
        // node. No other target has this reading.
        if ( !map.once || entry.type_id != ~target ||
            ( target != kTableBytesTypeId && target != kTableStringTypeId ) )
        {
            TableNodeWrongTarget( report );
            return;
        }
        report->refused = true;
        report->refuse_reason = blob_shared;
    }
    else if ( map.once && ( target == kTableBytesTypeId || target == kTableStringTypeId ) )
    {
        map.entries[index - 1].type_id = ~target;
    }
    if ( map.cycle_indices && target != kTableBytesTypeId && target != kTableStringTypeId )
    {
        slot.value = (int64_t) index;
        return;
    }
    slot.value = map.arena ? (int64_t) entry.offset
                           : (int64_t) ( ( map.base + entry.offset ) - (const uint8_t *) &slot );
}

// ---- A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4) ----
//
// During construction table pointer slots hold validated wire indices. Kahn's
// two passes therefore reach the directory directly: each node is entered once
// and each slot is visited a fixed number of times, with no sorting or reverse
// offset lookup. A final pass resolves the slots to their published offsets.
// The directory's type_id word carries the degree and stack link; its offset
// high bits carry the type ordinal. The caller restores both before publishing
// a region. Region loads allocate nothing and no pointer graph is recursed.
static const uint64_t kTableCycleAbsent = 1ull << 63;
static const uint64_t kTableCycleVertex = 1ull << 62;
static const int kTableCycleOrdinalShift = 48;
static const uint64_t kTableCycleOffsetMask = ( 1ull << 48 ) - 1;

struct TableCycle
{
    TableNodeDirEntry * entries = NULL;
    int64_t count = 0;
    const uint8_t * base = NULL;
    const TableArena * arena = NULL;
    uint32_t head = 0;
    int64_t vertices = 0;
    int64_t processed = 0;
    bool releasing = false;
    bool resolving = false;
    bool broken = false;
};

inline bool TableCycleIsVertex( const TableCycle & c, int64_t p )
{
    return ( c.entries[p].offset & ( kTableCycleAbsent | kTableCycleVertex ) ) == kTableCycleVertex;
}

inline uint32_t TableCycleOrdinal( const TableCycle & c, int64_t p )
{
    return (uint32_t) ( ( c.entries[p].offset >> kTableCycleOrdinalShift ) & 0x3FFFu );
}

inline const void * TableCycleNode( const TableCycle & c, int64_t p )
{
    const uint64_t offset = c.entries[p].offset & kTableCycleOffsetMask;
    if ( c.arena != NULL ) { return (const void *) TableArenaAt( *c.arena, (uint32_t) offset ); }
    return (const void *) ( c.base + offset );
}

inline bool TableCycleBegin( TableCycle & c, TableNodeDirEntry * entries, int64_t count, const uint8_t * base, const TableArena * arena, uint32_t ( *ordinal )( uint64_t type_id ) )
{
    c.entries = entries;
    c.count = count;
    c.base = base;
    c.arena = arena;
    if ( count <= 0 || count >= ( 1ll << 31 ) )
    {
        c.broken = true;
        return false;
    }
    for ( int64_t p = 0; p < count; p++ )
    {
        TableNodeDirEntry & e = entries[p];
        if ( e.offset == kTableNodeAbsent )
        {
            e.offset = kTableCycleAbsent;
            continue;
        }
        if ( e.offset > kTableCycleOffsetMask )
        {
            c.broken = true;
            return false;
        }
        const uint32_t kind = ordinal( e.type_id );
        if ( kind == 0 ) { continue; } // a byte buffer or an already marked blob
        e.offset |= kTableCycleVertex | ( (uint64_t) kind << kTableCycleOrdinalShift );
        e.type_id = 0;
        c.vertices++;
    }
    return true;
}

inline void TableCyclePush( TableCycle & c, int64_t p )
{
    c.entries[p].type_id = ( c.entries[p].type_id & 0xFFFFFFFFull ) | ( (uint64_t) c.head << 32 );
    c.head = (uint32_t) ( p + 1 );
}

// TableCycleEdge is one stored pointer slot: pass one counts the in-degree of
// the node it names, and pass two takes the count down and stacks the node
// when the last slot naming it has been met.
inline bool TableCycleEdge( TableCycle & c, const TableRef & slot )
{
    if ( slot.value == 0 ) { return true; }
    const uint64_t index = (uint64_t) slot.value;
    if ( index - 1 >= (uint64_t) c.count )
    {
        c.broken = true;
        return false;
    }
    const int64_t p = (int64_t) index - 1;
    if ( !TableCycleIsVertex( c, p ) )
    {
        c.broken = true;
        return false;
    }
    if ( c.resolving )
    {
        const uint64_t offset = c.entries[p].offset & kTableCycleOffsetMask;
        // The visitor borrows const views, but these are this load's mutable
        // construction slots. No caller can observe their temporary indices.
        TableRef & output = const_cast<TableRef &>( slot );
        output.value = c.arena != NULL ? (int64_t) offset
                                       : (int64_t) ( ( c.base + offset ) - (const uint8_t *) &slot );
        return true;
    }
    const uint32_t degree = (uint32_t) c.entries[p].type_id;
    if ( !c.releasing )
    {
        if ( degree == 0xFFFFFFFFu )
        {
            c.broken = true;
            return false;
        }
        c.entries[p].type_id += 1;
        return true;
    }
    if ( degree == 0 )
    {
        c.broken = true;
        return false;
    }
    c.entries[p].type_id -= 1;
    if ( degree == 1 ) { TableCyclePush( c, p ); } // the last slot naming it: its descent is free
    return true;
}

// TableCycleAcyclic checks the graph, then resolves its slots. VISIT enumerates one node's stored
// slots through TableCycleEdge. True is a graph with no cycle; false is a
// cycle, or a directory this check could not carry.
template <typename Ctx, typename Visit>
inline bool TableCycleAcyclic( TableCycle & c, const Ctx & ctx, Visit visit )
{
    if ( c.broken ) { return false; }
    for ( int64_t p = 0; p < c.count; p++ )
    {
        if ( TableCycleIsVertex( c, p ) && !visit( ctx, c, p ) )
        {
            c.broken = true;
            return false;
        }
    }
    c.releasing = true;
    for ( int64_t p = 0; p < c.count; p++ )
    {
        if ( TableCycleIsVertex( c, p ) && (uint32_t) c.entries[p].type_id == 0 ) { TableCyclePush( c, p ); }
    }
    while ( c.head != 0 )
    {
        const int64_t p = (int64_t) c.head - 1;
        c.head = (uint32_t) ( c.entries[p].type_id >> 32 );
        // a node is entered ONCE: more entries than nodes is a walk gone wrong
        if ( ++c.processed > c.vertices )
        {
            c.broken = true;
            return false;
        }
        if ( !visit( ctx, c, p ) )
        {
            c.broken = true;
            return false;
        }
    }
    if ( c.broken || c.processed != c.vertices ) { return false; } // an unfreed node sits on a cycle
    c.resolving = true;
    for ( int64_t p = 0; p < c.count; p++ )
    {
        if ( TableCycleIsVertex( c, p ) && !visit( ctx, c, p ) )
        {
            c.broken = true;
            return false;
        }
    }
    return !c.broken;
}

// TableCycleRestore puts back one table node's type id, the one the wire
// carries; a byte buffer's entry was never touched.
inline void TableCycleRestore( TableCycle & c, int64_t p, uint64_t type_id )
{
    if ( p >= 0 && p < c.count && TableCycleIsVertex( c, p ) ) { c.entries[p].type_id = type_id; }
}

// TableCycleEnd removes the construction tags from directory offsets.
inline void TableCycleEnd( TableCycle & c )
{
    for ( int64_t p = 0; p < c.count && c.entries != NULL; p++ )
    {
        uint64_t & offset = c.entries[p].offset;
        if ( ( offset & kTableCycleAbsent ) != 0 ) { offset = kTableNodeAbsent; }
        else
        {
            offset &= kTableCycleOffsetMask;
        }
    }
}

// ---- the record SCAN, and it is the whole of load's bound (§3.1) ----
//
// Reading follows no reference. The scan walks the root body's top-level fields,
// finds the ONE under the reserved id, and reads records out of its payload in
// order — the field rides once, so nothing is copied to make a body contiguous
// and the generated body decoder never learns the transport exists.
struct TableNodeScan
{
    TableReader fields;        // over the ROOT body, skipping past everything else
    const uint8_t * payload;   // the node-table field's payload
    int64_t payload_size;
    int64_t payload_offset;
    bool opened;               // the root body has been walked for the field
    uint64_t declared;
    int64_t records;
    bool present;              // the root body carries a node table at all
    bool damaged;
    const TableIdTable * ids;
    uint64_t type_ref;
};

inline TableNodeScan TableNodeScanBegin( const uint8_t * body, int64_t size, TableReport * report, const TableIdTable * ids )
{
    TableNodeScan s = { TableReader( body, size, report, ids ), NULL, 0, 0, false, 0, 0, false, false, ids, 0 };
    return s;
}

// find the node-table field, or answer false when the root body has none. A
// body carrying an id more than once is legal input and THE LAST OCCURRENCE
// WINS (docs/SPEC-TABLES.md §3), so the walk runs to the terminator and keeps
// the last rather than stopping at the first.
inline bool TableNodeScanOpen( TableNodeScan & s )
{
    if ( s.opened ) { return false; }
    s.opened = true;
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !s.fields.getleb( ref ) ) { break; }
        if ( ref == 0 ) { break; } // the terminator
        if ( s.ids == NULL || ref > (uint64_t) s.ids->count ) { break; }
        const uint64_t id = s.ids->at( ref );
        if ( !s.fields.has( 1 ) ) { break; }
        const uint8_t kind = s.fields.get8();
        if ( id == kTableNodeTableFieldId )
        {
            s.present = true;
            if ( kind != 12 )
            {
                s.damaged = true;
                return false;
            }
            uint64_t length = 0;
            if ( !s.fields.getleb( length ) || !s.fields.room( length ) )
            {
                s.damaged = true;
                return false;
            }
            s.payload = s.fields.buffer + s.fields.offset;
            s.payload_size = (int64_t) length;
            s.fields.offset += (int64_t) length;
            continue;
        }
        if ( !s.fields.skip( kind ) ) { break; }
    }
    if ( s.payload == NULL ) { return false; }
    TableReader head( s.payload, s.payload_size, s.fields.report, s.ids );
    if ( !head.getleb( s.declared ) )
    {
        s.damaged = true;
        return false;
    }
    s.payload_offset = head.offset;
    return true;
}

// the next record, or false at the end of the table — s.damaged says whether
// the end was the end or the framing giving out
inline bool TableNodeScanNext( TableNodeScan & s, uint64_t & type_id, const uint8_t *& body, int64_t & length )
{
    if ( !s.opened && !TableNodeScanOpen( s ) ) { return false; }
    if ( s.payload == NULL || s.payload_offset >= s.payload_size ) { return false; }
    TableReader rec( s.payload, s.payload_size, s.fields.report, s.ids );
    rec.offset = s.payload_offset;
    uint64_t ref = 0;
    if ( !rec.getleb( ref ) || ref == 0 || s.ids == NULL || ref > (uint64_t) s.ids->count )
    {
        s.damaged = true; // a type id reference of 0, or one past the table
        return false;
    }
    s.type_ref = ref;
    type_id = s.ids->at( ref );
    // A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS
    // MALFORMED (docs/SPEC-TABLES.md §3.1, §3.3): the node table, the build
    // version and the vocabulary are transports, so no record's TYPE reference
    // may resolve to one of their ids. The two blob type ids are lawful.
    if ( type_id == kTableNodeTableFieldId || type_id == kTableBuildVersionFieldId || type_id == kTableMessageVocabularyFieldId )
    {
        s.damaged = true;
        return false;
    }
    uint64_t declared_length = 0;
    if ( !rec.getleb( declared_length ) )
    {
        s.damaged = true; // a record whose length is damaged
        return false;
    }
    if ( declared_length > (uint64_t) ( s.payload_size - rec.offset ) )
    {
        s.damaged = true; // a record whose length runs past its field
        return false;
    }
    body = s.payload + rec.offset;
    length = (int64_t) declared_length;
    s.payload_offset = rec.offset + length;
    s.records++;
    return true;
}

// The record scan is AUTHORITATIVE: node_count is data from the wire, and a
// count that disagrees with the scan is malformed. Nothing is sized from it
// before the scan has confirmed it.
inline bool TableNodeScanWhole( TableNodeScan & s )
{
    if ( s.damaged ) { return false; }
    if ( !s.present ) { return true; } // no node table at all is not a broken one
    return s.declared == (uint64_t) s.records;
}

// ---- THE GRAPH, OVER EVERY KIND-17 PAYLOAD (docs/SPEC-TABLES.md §3.1, §6.5) ----
//
// The graph conditions are checked over the WHOLE input, not over the fields
// this reader can name: every record body and the root body are walked by the
// wire's own framing, every kind-17 payload is collected, and a cycle, an
// unreachable record, a blob record named from two slots and an ill-formed
// string blob record are judged wherever they sit. A record under a type id
// this build cannot name still contributes its edges, so a cycle through it is
// still refused; when it is reached and acyclic it reads null, which is
// evolution and stays a read (§3.1, §4).
//
// THE POINTER WALK IS ITERATIVE: the scratch is the caller's node directory,
// borrowed before it is filled, and carries the work list and its links, so a
// legal chain of any length costs no recursion and no allocation. The nested
// framing is the wire's own 128-level bound (§4), not a pointer edge.
static const uint64_t kTableGraphBlob = 0x8000000000000000ull;      // scratch offset: a blob record
static const uint64_t kTableGraphBodyMask = 0x7FFFFFFFFFFFFFFFull;  // scratch offset: the body's start
static const uint64_t kTableGraphVisited = 0x8000000000000000ull;   // scratch type: reached
static const uint64_t kTableGraphQueued = 0x8000000000000000ull;    // scratch type: on the work list
static const uint64_t kTableGraphLinkMask = 0x7FFFFFFFFFFFFFFFull;  // scratch type: the next link
static const int kTableGraphMaxDepth = 128;

struct TableGraphCollect
{
    const TableIdTable * ids;
    void * ctx;
    bool ( *edge )( void * ctx, uint64_t index );
    bool damaged;
    bool closed_kind; // an array or keyed header off §3's closed element-kind list
};

inline int32_t TableGraphWidth( uint8_t kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: return 1;
        case 3: case 7: case 21: case 26: return 2;
        case 4: case 8: case 10: case 22: case 27: return 4;
        case 5: case 9: case 11: case 23: case 28: case 36: return 8;
        case 18: case 19: case 24: case 29: return 16;
    }
    return 0;
}

inline bool TableGraphUnion( TableGraphCollect & c, TableReader & r, int depth );
inline bool TableGraphContent( TableGraphCollect & c, const uint8_t * b, int64_t n, uint8_t kind, int depth );

inline bool TableGraphPayload( TableGraphCollect & c, TableReader & r, uint8_t kind, int depth )
{
    const int32_t width = TableGraphWidth( kind );
    if ( width > 0 )
    {
        if ( !r.has( width ) ) { c.damaged = true; return false; }
        r.offset += width;
        return true;
    }
    switch ( kind )
    {
        case 17:
        {
            uint64_t index = 0;
            if ( !r.getleb( index ) ) { c.damaged = true; return false; }
            if ( !c.edge( c.ctx, index ) ) { c.damaged = true; return false; }
            return true;
        }
        case 30:
        {
            uint64_t ref = 0;
            if ( !r.getleb( ref ) || c.ids == NULL || ref > (uint64_t) c.ids->count ) { c.damaged = true; return false; }
            return true;
        }
        case 15:
            return TableGraphUnion( c, r, depth );
        case 12: case 31: case 32: case 13: case 14: case 16:
        {
            uint64_t n = 0;
            if ( !r.getleb( n ) || !r.room( n ) ) { c.damaged = true; return false; }
            const uint8_t * b = r.buffer + r.offset;
            r.offset += (int64_t) n;
            return TableGraphContent( c, b, (int64_t) n, kind, depth );
        }
    }
    c.damaged = true;
    return false;
}

inline bool TableGraphBody( TableGraphCollect & c, const uint8_t * b, int64_t n, bool root, bool exact, int depth )
{
    if ( depth > kTableGraphMaxDepth ) { c.damaged = true; return false; }
    TableReport ignored;
    TableReader r( b, n, &ignored, c.ids );
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.getleb( ref ) ) { c.damaged = true; return false; }
        if ( ref == 0 ) { if ( exact && r.offset != n ) { c.damaged = true; return false; } return true; }
        if ( c.ids == NULL || ref > (uint64_t) c.ids->count || !r.has( 1 ) ) { c.damaged = true; return false; }
        const uint64_t id = c.ids->at( ref );
        const uint8_t kind = r.get8();
        if ( id == kTableNodeTableFieldId )
        {
            if ( !root || kind != 12 ) { c.damaged = true; return false; }
            uint64_t len = 0;
            if ( !r.getleb( len ) || !r.room( len ) ) { c.damaged = true; return false; }
            r.offset += (int64_t) len;
            continue;
        }
        if ( !TableGraphPayload( c, r, kind, depth ) ) { return false; }
    }
}

inline bool TableGraphContent( TableGraphCollect & c, const uint8_t * b, int64_t n, uint8_t kind, int depth )
{
    const int32_t width = TableGraphWidth( kind );
    if ( width > 0 )
    {
        if ( n != width ) { c.damaged = true; return false; }
        return true;
    }
    switch ( kind )
    {
        case 12:
            if ( !TableUtf8Valid( b, (uint64_t) n ) ) { c.damaged = true; return false; }
            return true;
        case 31:
            return true; // opaque: the one kind whose meaning a reader may not know (§3)
        case 32:
            if ( n != 0 ) { c.damaged = true; return false; }
            return true;
        case 13:
            if ( depth >= kTableGraphMaxDepth ) { c.damaged = true; return false; }
            return TableGraphBody( c, b, n, false, true, depth + 1 );
        case 14:
        {
            if ( depth >= kTableGraphMaxDepth ) { c.damaged = true; return false; }
            if ( n < 2 ) { c.damaged = true; return false; }
            TableReport ignored;
            TableReader r( b + 1, n - 1, &ignored, c.ids );
            const uint8_t elem = b[0];
            // THE ELEMENT KIND IS CLOSED, judged before any element and under
            // a count of 0 (docs/SPEC-TABLES.md §3, §4).
            if ( !TableArrayElementKind( elem ) ) { c.damaged = true; c.closed_kind = true; return false; }
            uint64_t count = 0;
            if ( !r.getleb( count ) ) { c.damaged = true; return false; }
            if ( count > (uint64_t) ( r.size - r.offset ) ) { c.damaged = true; return false; }
            for ( uint64_t i = 0; i < count; i++ ) { if ( !TableGraphPayload( c, r, elem, depth + 1 ) ) { return false; } }
            return true;
        }
        case 16:
        {
            if ( depth >= kTableGraphMaxDepth ) { c.damaged = true; return false; }
            if ( n < 2 ) { c.damaged = true; return false; }
            TableReport ignored;
            TableReader r( b + 1, n - 1, &ignored, c.ids );
            const uint8_t elem = b[0];
            // THE KEYED BODY'S ELEMENT KIND IS ITS OWN CLOSED LIST (§3, §4).
            if ( !TableKeyedElementKind( elem ) ) { c.damaged = true; c.closed_kind = true; return false; }
            uint64_t count = 0;
            if ( !r.getleb( count ) ) { c.damaged = true; return false; }
            if ( count > (uint64_t) ( r.size - r.offset ) ) { c.damaged = true; return false; }
            for ( uint64_t i = 0; i < count; i++ )
            {
                uint64_t key = 0;
                if ( !r.getleb( key ) || key == 0 || c.ids == NULL || key > (uint64_t) c.ids->count ) { c.damaged = true; return false; }
                uint64_t len = 0;
                if ( !r.getleb( len ) || !r.room( len ) ) { c.damaged = true; return false; }
                const uint8_t * eb = r.buffer + r.offset;
                r.offset += (int64_t) len;
                if ( !TableGraphContent( c, eb, (int64_t) len, elem, depth + 1 ) ) { return false; }
            }
            return true;
        }
        case 15:
        {
            TableReport ignored;
            TableReader r( b, n, &ignored, c.ids );
            return TableGraphUnion( c, r, depth );
        }
        case 17: case 30:
        {
            TableReport ignored;
            TableReader r( b, n, &ignored, c.ids );
            if ( !TableGraphPayload( c, r, kind, depth ) ) { return false; }
            if ( r.offset != n ) { c.damaged = true; return false; }
            return true;
        }
    }
    c.damaged = true;
    return false;
}

inline bool TableGraphUnion( TableGraphCollect & c, TableReader & r, int depth )
{
    uint64_t ref = 0;
    if ( !r.getleb( ref ) ) { c.damaged = true; return false; }
    if ( ref == 0 ) { return true; }
    if ( c.ids == NULL || ref > (uint64_t) c.ids->count || !r.has( 1 ) ) { c.damaged = true; return false; }
    const uint8_t kind = r.get8();
    uint64_t len = 0;
    if ( !r.getleb( len ) || !r.room( len ) ) { c.damaged = true; return false; }
    const uint8_t * b = r.buffer + r.offset;
    r.offset += (int64_t) len;
    const bool structural = kind == 13 || kind == 14 || kind == 15 || kind == 16;
    if ( structural )
    {
        if ( depth >= kTableGraphMaxDepth ) { c.damaged = true; return false; }
        if ( !TableGraphContent( c, b, (int64_t) len, kind, depth + 1 ) ) { return false; }
    }
    else if ( !TableGraphContent( c, b, (int64_t) len, kind, depth ) ) { return false; }
    return true;
}

struct TableGraphCount
{
    TableNodeDirEntry * nodes;
    int64_t count;
    bool bad;
};

inline bool TableGraphCountEdge( void * ctx, uint64_t index )
{
    TableGraphCount * c = (TableGraphCount *) ctx;
    if ( index == 0 ) { return true; }
    if ( index - 1 >= (uint64_t) c->count ) { c->bad = true; return false; }
    c->nodes[index - 1].type_id += 1;
    return true;
}

struct TableGraphKahn
{
    TableNodeDirEntry * nodes;
    int64_t count;
    int64_t head;
    uint64_t processed;
    bool bad;
};

inline void TableGraphKahnPush( TableGraphKahn & k, int64_t p )
{
    k.nodes[p].type_id = kTableGraphQueued | (uint64_t) ( k.head + 1 );
    k.head = p;
}

inline bool TableGraphKahnEdge( void * ctx, uint64_t index )
{
    TableGraphKahn * k = (TableGraphKahn *) ctx;
    if ( index == 0 ) { return true; }
    if ( index - 1 >= (uint64_t) k->count ) { k->bad = true; return false; }
    TableNodeDirEntry & e = k->nodes[index - 1];
    if ( ( e.type_id & kTableGraphQueued ) != 0 || e.type_id == 0 ) { k->bad = true; return false; }
    e.type_id -= 1;
    if ( e.type_id == 0 ) { TableGraphKahnPush( *k, (int64_t) index - 1 ); }
    return true;
}

struct TableGraphReach
{
    TableNodeDirEntry * nodes;
    int64_t count;
    int64_t head;
    int64_t tail;
    bool bad;
};

inline bool TableGraphReachEdge( void * ctx, uint64_t index )
{
    TableGraphReach * r = (TableGraphReach *) ctx;
    if ( index == 0 ) { return true; }
    if ( index - 1 >= (uint64_t) r->count ) { r->bad = true; return false; }
    const int64_t t = (int64_t) index - 1;
    if ( ( r->nodes[t].type_id & kTableGraphVisited ) != 0 ) { return true; }
    r->nodes[t].type_id = kTableGraphVisited;
    if ( r->tail < 0 ) { r->head = t; }
    else { r->nodes[r->tail].type_id = kTableGraphVisited | (uint64_t) ( t + 1 ); }
    r->tail = t;
    return true;
}

inline bool TableGraphParseNode( TableGraphCollect & c, const uint8_t * wire, int64_t wire_bytes, uint64_t start, bool root )
{
    if ( start > (uint64_t) wire_bytes ) { c.damaged = true; return false; }
    return TableGraphBody( c, wire + start, wire_bytes - (int64_t) start, root, root, 0 );
}

inline bool TableGraphFail( TableReport * report )
{
    report->damaged = true;
    report->refuse_reason = wire_damaged;
    return false;
}

// TableGraphCheck judges the pointer graph over one form-1 file's root body.
// The scratch holds count entries (the root plus every record) and may be the
// caller's node directory before it is filled; a true answer leaves it
// clobbered and a false answer is a refused load, so neither is observed.
inline bool TableGraphCheck( const uint8_t * wire, int64_t wire_bytes, const TableIdTable * ids, TableNodeDirEntry * scratch, int64_t count, TableReport * report )
{
    uint64_t type_id = 0;
    const uint8_t * body = NULL;
    int64_t length = 0;
    int64_t records = 0;
    {
        TableReport ignored;
        TableNodeScan scan = TableNodeScanBegin( wire, wire_bytes, &ignored, ids );
        while ( TableNodeScanNext( scan, type_id, body, length ) )
        {
            const int64_t k = records + 1;
            if ( k >= count ) { return true; } // a scan this check cannot frame is the typed read's to judge
            scratch[k].offset = (uint64_t) ( body - wire );
            if ( type_id == kTableStringTypeId || type_id == kTableBytesTypeId ) { scratch[k].offset |= kTableGraphBlob; }
            if ( type_id == kTableStringTypeId && !TableUtf8Valid( body, (uint64_t) length ) ) { return TableGraphFail( report ); }
            records++;
        }
        if ( !TableNodeScanWhole( scan ) || records + 1 != count ) { return true; } // the typed read names the framing
    }
    scratch[0].offset = 0;
    scratch[0].type_id = 0;
    const int64_t n = count;

    for ( int64_t i = 0; i < n; i++ ) { scratch[i].type_id = 0; }
    {
        TableGraphCount counts = { scratch, n, false };
        TableGraphCollect collect = { ids, &counts, &TableGraphCountEdge, false, false };
        if ( !TableGraphParseNode( collect, wire, wire_bytes, scratch[0].offset, true ) ) { return ( counts.bad || collect.closed_kind ) ? TableGraphFail( report ) : true; }
        for ( int64_t i = 1; i < n; i++ )
        {
            if ( ( scratch[i].offset & kTableGraphBlob ) != 0 ) { continue; }
            if ( !TableGraphParseNode( collect, wire, wire_bytes, scratch[i].offset & kTableGraphBodyMask, false ) ) { return ( counts.bad || collect.closed_kind ) ? TableGraphFail( report ) : true; }
        }
        for ( int64_t i = 1; i < n; i++ )
        {
            if ( ( scratch[i].offset & kTableGraphBlob ) != 0 && scratch[i].type_id > 1 )
            {
                report->refused = true;
                report->refuse_reason = blob_shared;
                return false;
            }
        }
    }

    TableGraphKahn kahn = { scratch, n, -1, 0, false };
    for ( int64_t i = 0; i < n; i++ ) { if ( scratch[i].type_id == 0 ) { TableGraphKahnPush( kahn, i ); } }
    while ( kahn.head >= 0 )
    {
        const int64_t p = kahn.head;
        kahn.head = (int64_t) ( scratch[p].type_id & kTableGraphLinkMask ) - 1;
        kahn.processed++;
        if ( ( scratch[p].offset & kTableGraphBlob ) != 0 ) { continue; }
        TableGraphCollect collect = { ids, &kahn, &TableGraphKahnEdge, false, false };
        if ( !TableGraphParseNode( collect, wire, wire_bytes, scratch[p].offset & kTableGraphBodyMask, p == 0 ) ) { return ( kahn.bad || collect.closed_kind ) ? TableGraphFail( report ) : true; }
    }
    if ( kahn.bad || kahn.processed != (uint64_t) n ) { return TableGraphFail( report ); }

    for ( int64_t i = 0; i < n; i++ ) { scratch[i].type_id = 0; }
    TableGraphReach reach = { scratch, n, 0, 0, false };
    scratch[0].type_id = kTableGraphVisited;
    while ( reach.head >= 0 )
    {
        const int64_t p = reach.head;
        reach.head = (int64_t) ( scratch[p].type_id & kTableGraphLinkMask ) - 1;
        if ( reach.head < 0 ) { reach.tail = -1; }
        if ( ( scratch[p].offset & kTableGraphBlob ) != 0 ) { continue; }
        TableGraphCollect collect = { ids, &reach, &TableGraphReachEdge, false, false };
        if ( !TableGraphParseNode( collect, wire, wire_bytes, scratch[p].offset & kTableGraphBodyMask, p == 0 ) ) { return ( reach.bad || collect.closed_kind ) ? TableGraphFail( report ) : true; }
    }
    if ( reach.bad ) { return TableGraphFail( report ); }
    for ( int64_t i = 1; i < n; i++ )
    {
        if ( ( scratch[i].type_id & kTableGraphVisited ) == 0 ) { return TableGraphFail( report ); }
    }
    return true;
}

` + tableStrictPointerEntry() + `} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// unitDeclaresBlob reports whether the unit declares a byte buffer pointer
// anywhere — a field, an element, a map's half or a union arm — which is the
// question the arena runtime's one package-scoped definition is gated on. See
// cppBlobThunks for why a DECLARATION scan and not the numbering walk's
// reachability answer.
func unitDeclaresBlob(u *ir.Unit) bool {
	return len(ir.BlobPointerFields(u)) > 0
}
