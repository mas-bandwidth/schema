// Maps in the C++ reference (docs/SPEC-TABLES.md §2.8): the runtime a map
// field's storage, its builder and its lookup are spelled in, and the
// per-entry helpers the generated code hangs off it.
//
// A map is a LOOKUP over an array of one generated `{ key, value }` table held
// in ascending key order, so nothing here is a new wire construct. What the
// runtime adds is the sorted array's binary search, the builder's segments and
// the optional side index.
package cpptable

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// unitHasMap reports whether any closure member declares a `map[K]V`. It is
// what gates the map runtime: not one symbol of it appears in a map-free
// unit's generated header (docs/SPEC-TABLES.md §2.2, §2.8).
func unitHasMap(u *ir.Unit, closure map[string]bool) bool {
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.IsMap() {
				return true
			}
		}
	}
	return false
}

// mapFieldsOf lists one member's map fields in declaration order.
func mapFieldsOf(st *ir.Struct) []*ir.Field {
	var out []*ir.Field
	for _, f := range st.Fields {
		if f.IsMap() {
			out = append(out, f)
		}
	}
	return out
}

// mapVerb spells one of a map field's claimed surface names on its holder:
// `<Table><Field>` followed by the verb (docs/SPEC-TABLES.md §2.8, §11).
func mapVerb(owner string, f *ir.Field, verb string) string {
	return owner + ir.GoExportName(f.Name) + verb
}

// mapEntryOf names the generated entry table of a map field.
func mapEntryOf(f *ir.Field) *ir.Struct { return f.MapEntry }

// mapKeyIsString reports a `string(N)` key; every other key is an integer.
func mapKeyIsString(f *ir.Field) bool { return ir.MapKeyField(f).Type.Kind == ir.TString }

// mapKeyCallType is the form a KEY takes at the call site and out of an
// iteration (docs/SPEC-TABLES.md §2.8): a `const char *` for a bounded string,
// NUL-terminated and exactly the bytes the storage holds, and the integer
// itself otherwise.
func (g *tableGen) mapKeyCallType(f *ir.Field) string {
	key := ir.MapKeyField(f)
	if key.Type.Kind == ir.TString {
		return "const char *"
	}
	typ, _ := g.cppFieldType(key.Type)
	return typ
}

// mapKeyOrderType is the width the ORDER compares an integer key at: signed
// for the signed kinds, unsigned for the unsigned (docs/SPEC-TABLES.md §2.8).
func mapKeyOrderType(f *ir.Field) string {
	key := ir.MapKeyField(f)
	if key.Type.Signed {
		return "int64_t"
	}
	return "uint64_t"
}

// mapValueIsPointer reports a `map[K]*T` — the value is a pointer SLOT, so the
// const form's Find answers the resolved `const T *` (docs/SPEC-TABLES.md §2.8).
func mapValueIsPointer(f *ir.Field) bool { return ir.MapValueField(f).Type.Pointer }

// ---- the runtime (docs/SPEC-TABLES.md §2.8) ----

// tableMapRuntime is the map half of the variable-length runtime: the storage
// type, the order, the builder's head and segments, the ordered cursor the
// four writing walks read, and the optional side index. It is emitted only
// into a unit that declares a map.
func tableMapRuntime(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_MAP"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// ---- a MAP: a sorted entry array, and the lookup over it (§2.8) ----
//
// On the wire, in a region and in a cook a map is an array of one generated
// ENTRY table held in ascending key order. What this adds is Find — a binary
// search over that array where it lies — and a builder that inserts, replaces
// and erases by key. Nothing here is stored: a region and a cook carry the
// array and the count, and not one byte about a hash or a probe.

// entries carved from ONE call to the allocator pair; a new segment is
// appended when the current one fills, and nothing ever moves (§6.4)
static const int32_t kTableMapSegmentEntries = 32;

// TableDeclRef names a type in an unevaluated context and is never defined —
// what <utility>'s declval is for, without the include the generated corpus
// refuses to pay for (the iterator_traits note, §13.9).
template <typename T> T & TableDeclRef();

// THE ORDER IS TOTAL, AND IT IS THE SAME IN NINE LANGUAGES (§2.8). Integers
// compare by VALUE, signed for the signed kinds and unsigned for the unsigned.
// Strings compare by BYTES, unsigned, a shorter string that is a prefix of a
// longer one first: memcmp over the common length, then the lengths. Never a
// locale, never a code point, never a case fold.
inline int TableKeyOrder( uint64_t a, uint64_t b ) { return a < b ? -1 : ( a > b ? 1 : 0 ); }
inline int TableKeyOrder( int64_t a, int64_t b ) { return a < b ? -1 : ( a > b ? 1 : 0 ); }
inline int TableKeyOrder( const char * a, int32_t a_length, const char * b, int32_t b_length )
{
    const int32_t common = a_length < b_length ? a_length : b_length;
    if ( common > 0 )
    {
        const int order = memcmp( (const void *) a, (const void *) b, (size_t) common );
        if ( order != 0 ) { return order < 0 ? -1 : 1; }
    }
    return a_length < b_length ? -1 : ( a_length > b_length ? 1 : 0 );
}

// the length of a NUL-terminated key at a call site, bounded by the storage it
// has to fit: a key one byte longer than the bound is refused, never truncated
inline int32_t TableKeyLength( const char * key, int32_t bound )
{
    if ( key == NULL ) { return 0; }
    for ( int32_t i = 0; i <= bound; i++ ) { if ( key[i] == 0 ) { return i; } }
    return bound + 1; // longer than the bound: the caller refuses it
}

// ---- the storage: SIXTEEN BYTES in the holder's record (§2.8, §7.2) ----
//
// An int64 self-relative reference to the entry array and an int32 count, then
// padding to eight. The reference is a TableRef like a pointer's: in the arena
// it names the builder's HEAD, in a region it is the delta from the slot to
// the first entry, and 0 is the empty map in both.
template <typename Entry> struct TableMap
{
    TableRef entries;
    int32_t count = 0;   // the LIVE count, in both forms
    int32_t padding = 0; // named, so the record has no unwritten byte in it

    // ---- the CONST form: a locked region, a loaded one, an opened cook ----
    //
    // One surface over one encoding (§6.3). A region reference resolves from
    // the slot's own address, so every one of these is a member and needs no
    // base and no context.
    const Entry * Entries() const
    {
        return entries.value != 0 ? (const Entry *) ( (const uint8_t *) &entries + entries.value ) : NULL;
    }
    int32_t size() const { return count; }

    // FIND: floor( log2 n ) + 1 key compares, in place, no allocation. NULL
    // when absent, and on a map[K]*T the RESOLVED pointer, which is what a
    // pointer field's accessor answers.
    template <typename Key> const Entry * FindEntry( Key key ) const
    {
        const Entry * base = Entries();
        int32_t low = 0, high = count;
        while ( low < high )
        {
            const int32_t mid = low + ( high - low ) / 2;
            const int order = TableEntryOrder( base[mid], key );
            if ( order == 0 ) { return base + mid; }
            if ( order < 0 ) { low = mid + 1; } else { high = mid; }
        }
        return NULL;
    }
    // the return type is DEDUCED, so it is worked out when a call site
    // instantiates Find and not when the holder's record declares the slot —
    // which is what lets the entry's own overloads be declared after it
    template <typename Key> auto Find( Key key ) const
    {
        return TableEntryFound( FindEntry( key ) );
    }

    // ---- iteration: ASCENDING key order, the key beside the value ----
    //
    // A proxy BY VALUE, the keyed array's shape (§2.4): for ( auto [ key,
    // value ] : map ). It carries no iterator_traits, for the reason
    // TableKeyed's does not (§13.9).
    struct ConstEntry
    {
        decltype( TableEntryKey( TableDeclRef<const Entry>() ) ) key;
        decltype( TableEntryFound( (const Entry *) NULL ) ) value;
    };

    struct ConstIterator
    {
        const Entry * at;
        ConstEntry operator*() const { return ConstEntry{ TableEntryKey( *at ), TableEntryFound( at ) }; }
        ConstIterator & operator++() { at++; return *this; }
        bool operator==( const ConstIterator & other ) const { return at == other.at; }
        bool operator!=( const ConstIterator & other ) const { return at != other.at; }
    };

    ConstIterator begin() const { return ConstIterator{ Entries() }; }
    ConstIterator end() const { return ConstIterator{ Entries() + count }; }
};

// ---- the BUILDER's side: a head, and segments that never move (§2.8, §6.4) ----
//
// The head is a small node in the arena holding the segment chain, the live
// count and the dead count, allocated when the first entry is inserted. Each
// segment is a fixed number of entries carved from one call to the allocator
// pair. An entry's address is stable for the arena's life, so a value handed
// back by an insert stays valid while other entries arrive.
struct TableMapHead
{
    TableRef first; // the arena offset of the first segment
    TableRef last;  // and of the one an insert appends into
    int32_t live;
    int32_t dead;
};

template <typename Entry> struct TableMapSegment
{
    TableRef next;
    int32_t used;                                        // entries carved from this segment
    int32_t padding;
    uint32_t dead[ ( kTableMapSegmentEntries + 31 ) / 32 ]; // Erase marks one bit, never the entry
    Entry entries[ kTableMapSegmentEntries ];
};

inline bool TableMapSegmentDead( const uint32_t * dead, int32_t index )
{
    return ( dead[ index / 32 ] & ( 1u << ( index % 32 ) ) ) != 0;
}

// ---- the ORDERED CURSOR the four writing walks read (§2.8) ----
//
// Measure, Save, Lock and Cook each write a map's entries in ascending key
// order with no key twice, deriving the order from the builder's entries as
// each walk derives the numbering (§3.1). Nothing passes between them, so
// measure == save over a map is a real check on two sorts agreeing.
//
// A REGION is already sorted, so its cursor is the array in place and
// allocates nothing. The BUILDER's is the sort: an array of entry pointers
// allocated through the pair and released before the walk returns, because
// sorting the segments themselves would move entries whose addresses a caller
// holds.
template <typename Entry> struct TableMapCursor
{
    const Entry * const * order = NULL; // the builder's form: sorted pointers
    const Entry * entries = NULL;       // the region's form: the array in place
    int32_t count = 0;
    TableAllocator allocator;
    bool ok = false;
    const Entry * operator[]( int32_t index ) const
    {
        return order != NULL ? order[index] : entries + index;
    }
};

// heapsort: O( n log n ) once per map, no recursion, no allocation past the
// pointer array the caller already paid for
template <typename Entry> inline void TableMapSort( const Entry ** order, int32_t count )
{
    for ( int32_t start = count / 2 - 1; start >= 0; start-- )
    {
        int32_t root = start;
        for ( ;; )
        {
            int32_t child = 2 * root + 1;
            if ( child >= count ) { break; }
            if ( child + 1 < count && TableEntryOrder( *order[child], *order[child + 1] ) < 0 ) { child++; }
            if ( TableEntryOrder( *order[root], *order[child] ) >= 0 ) { break; }
            const Entry * swap = order[root]; order[root] = order[child]; order[child] = swap;
            root = child;
        }
    }
    for ( int32_t end = count - 1; end > 0; end-- )
    {
        const Entry * swap = order[0]; order[0] = order[end]; order[end] = swap;
        int32_t root = 0;
        for ( ;; )
        {
            int32_t child = 2 * root + 1;
            if ( child >= end ) { break; }
            if ( child + 1 < end && TableEntryOrder( *order[child], *order[child + 1] ) < 0 ) { child++; }
            if ( TableEntryOrder( *order[root], *order[child] ) >= 0 ) { break; }
            const Entry * hold = order[root]; order[root] = order[child]; order[child] = hold;
            root = child;
        }
    }
}

// the REGION form: the array is already sorted, so the cursor is the array
template <typename Entry>
inline TableMapCursor<Entry> TableMapOrder( const TableRegionCtx &, const TableMap<Entry> & map )
{
    TableMapCursor<Entry> cursor;
    cursor.entries = map.Entries();
    cursor.count = map.count;
    cursor.ok = true;
    return cursor;
}

// the BUILDER's form: gather the LIVE entries out of the segment chain in
// insertion order, then sort. A dead entry costs nothing on any wire (§2.8).
template <typename Entry>
inline TableMapCursor<Entry> TableMapOrder( const TableArena & arena, const TableMap<Entry> & map )
{
    TableMapCursor<Entry> cursor;
    cursor.allocator = arena.allocator;
    cursor.count = map.count;
    if ( map.entries.value == 0 || map.count <= 0 ) { cursor.ok = map.count == 0; cursor.count = 0; return cursor; }
    const TableMapHead * head = (const TableMapHead *) TableArenaAt( arena, (uint32_t) map.entries.value );
    if ( head->live != map.count ) { return cursor; } // the slot and the head disagree: refused, never guessed
    const Entry ** order = (const Entry **) arena.allocator.alloc( arena.allocator.context, (int64_t) map.count * (int64_t) sizeof( const Entry * ) );
    if ( order == NULL ) { return cursor; }
    int32_t at = 0;
    TableRef segment_ref = head->first;
    while ( segment_ref.value != 0 && at < map.count )
    {
        const TableMapSegment<Entry> * segment = (const TableMapSegment<Entry> *) TableArenaAt( arena, (uint32_t) segment_ref.value );
        for ( int32_t i = 0; i < segment->used && at < map.count; i++ )
        {
            if ( TableMapSegmentDead( segment->dead, i ) ) { continue; }
            order[at++] = segment->entries + i;
        }
        segment_ref = segment->next;
    }
    if ( at != map.count )
    {
        arena.allocator.free( arena.allocator.context, order );
        return cursor;
    }
    TableMapSort( order, map.count );
    cursor.order = order;
    cursor.ok = true;
    return cursor;
}

template <typename Entry>
inline TableMapCursor<Entry> TableMapOrder( const TableArenaCtx & ctx, const TableMap<Entry> & map )
{
    return TableMapOrder( *ctx.arena, map );
}

template <typename Entry> inline void TableMapRelease( TableMapCursor<Entry> & cursor )
{
    if ( cursor.order != NULL ) { cursor.allocator.free( cursor.allocator.context, (void *) cursor.order ); }
    cursor.order = NULL;
}

// ---- the builder's five (§2.8) ----
//
// Insert APPENDS after one LINEAR SCAN of the live entries for the key it may
// replace, Find is that same scan, and Erase is the scan and one bit. The
// builder builds NO INDEX, and that is a rule: the sort happens once, at Lock,
// Save or Cook, and every lookup that matters runs over the sorted region.

// the head, allocated when the first entry is inserted
template <typename Entry>
inline TableMapHead * TableMapReach( TableWorker & worker, TableMap<Entry> & map )
{
    if ( worker.arena == NULL || worker.arena->locked ) { return NULL; }
    if ( map.entries.value != 0 ) { return (TableMapHead *) TableArenaAt( *worker.arena, (uint32_t) map.entries.value ); }
    uint32_t at = 0;
    TableMapHead * head = (TableMapHead *) worker.AllocRaw( (int64_t) sizeof( TableMapHead ), (int64_t) alignof( TableMapHead ), at );
    if ( head == NULL ) { return NULL; }
    head->first.value = 0;
    head->last.value = 0;
    head->live = 0;
    head->dead = 0;
    map.entries.value = (int64_t) at;
    return head;
}

// one entry's storage, appended: the current segment when it has room, a new
// one carved from one call to the pair when it does not
template <typename Entry>
inline Entry * TableMapAppend( TableWorker & worker, TableMapHead * head, TableMap<Entry> & map )
{
    TableMapSegment<Entry> * segment = NULL;
    if ( head->last.value != 0 )
    {
        segment = (TableMapSegment<Entry> *) TableArenaAt( *worker.arena, (uint32_t) head->last.value );
        if ( segment->used >= kTableMapSegmentEntries ) { segment = NULL; }
    }
    if ( segment == NULL )
    {
        uint32_t at = 0;
        segment = (TableMapSegment<Entry> *) worker.AllocRaw( (int64_t) sizeof( TableMapSegment<Entry> ), (int64_t) alignof( TableMapSegment<Entry> ), at );
        if ( segment == NULL ) { return NULL; } // the arena could not carve another segment
        segment->next.value = 0;
        segment->used = 0;
        segment->padding = 0;
        for ( int32_t i = 0; i < (int32_t) ( sizeof( segment->dead ) / sizeof( segment->dead[0] ) ); i++ ) { segment->dead[i] = 0; }
        if ( head->last.value != 0 )
        {
            TableMapSegment<Entry> * previous = (TableMapSegment<Entry> *) TableArenaAt( *worker.arena, (uint32_t) head->last.value );
            previous->next.value = (int64_t) at;
        }
        else
        {
            head->first.value = (int64_t) at;
        }
        head->last.value = (int64_t) at;
    }
    Entry * entry = segment->entries + segment->used;
    segment->used++;
    head->live++;
    map.count++;
    return entry;
}

// the LINEAR SCAN: the live entries in insertion order, O( n ) key compares
template <typename Entry, typename Key>
inline Entry * TableMapScan( const TableArena & arena, const TableMap<Entry> & map, Key key )
{
    if ( map.entries.value == 0 ) { return NULL; }
    const TableMapHead * head = (const TableMapHead *) TableArenaAt( arena, (uint32_t) map.entries.value );
    TableRef segment_ref = head->first;
    while ( segment_ref.value != 0 )
    {
        TableMapSegment<Entry> * segment = (TableMapSegment<Entry> *) TableArenaAt( arena, (uint32_t) segment_ref.value );
        for ( int32_t i = 0; i < segment->used; i++ )
        {
            if ( TableMapSegmentDead( segment->dead, i ) ) { continue; }
            if ( TableEntryOrder( segment->entries[i], key ) == 0 ) { return segment->entries + i; }
        }
        segment_ref = segment->next;
    }
    return NULL;
}

// ERASE marks the entry DEAD, one bit in the segment's slot and not in the
// entry table, and decrements the live count. Its storage is reclaimed at
// RESET and never reused mid-build, because reusing a slot would make "an
// entry's address is stable" false for exactly one case.
template <typename Entry, typename Key>
inline bool TableMapErase( TableArena & arena, TableMap<Entry> & map, Key key )
{
    if ( map.entries.value == 0 ) { return false; }
    TableMapHead * head = (TableMapHead *) TableArenaAt( arena, (uint32_t) map.entries.value );
    TableRef segment_ref = head->first;
    while ( segment_ref.value != 0 )
    {
        TableMapSegment<Entry> * segment = (TableMapSegment<Entry> *) TableArenaAt( arena, (uint32_t) segment_ref.value );
        for ( int32_t i = 0; i < segment->used; i++ )
        {
            if ( TableMapSegmentDead( segment->dead, i ) ) { continue; }
            if ( TableEntryOrder( segment->entries[i], key ) != 0 ) { continue; }
            segment->dead[ i / 32 ] |= 1u << ( i % 32 );
            head->live--;
            head->dead++;
            map.count--;
            return true;
        }
        segment_ref = segment->next;
    }
    return false;
}

// ---- iterate on the BUILDER: INSERTION order, live entries only (§2.8) ----
template <typename Entry> struct TableMapEach
{
    const TableArena * arena;
    TableRef first;

    struct Iterator
    {
        const TableArena * arena;
        TableMapSegment<Entry> * segment;
        int32_t index;

        void Skip()
        {
            for ( ;; )
            {
                if ( segment == NULL ) { return; }
                if ( index >= segment->used )
                {
                    segment = segment->next.value != 0 ? (TableMapSegment<Entry> *) TableArenaAt( *arena, (uint32_t) segment->next.value ) : NULL;
                    index = 0;
                    continue;
                }
                if ( TableMapSegmentDead( segment->dead, index ) ) { index++; continue; }
                return;
            }
        }
        auto operator*() const { return TableEntryEach( segment->entries + index ); }
        Iterator & operator++() { index++; Skip(); return *this; }
        bool operator==( const Iterator & other ) const { return segment == other.segment && index == other.index; }
        bool operator!=( const Iterator & other ) const { return !( *this == other ); }
    };

    Iterator begin() const
    {
        Iterator it = { arena, first.value != 0 ? (TableMapSegment<Entry> *) TableArenaAt( *arena, (uint32_t) first.value ) : NULL, 0 };
        it.Skip();
        return it;
    }
    Iterator end() const { Iterator it = { arena, NULL, 0 }; return it; }
};

template <typename Entry>
inline TableMapEach<Entry> TableMapEachOf( const TableArena & arena, const TableMap<Entry> & map )
{
    TableMapEach<Entry> each = { &arena, TableRef() };
    if ( map.entries.value != 0 )
    {
        const TableMapHead * head = (const TableMapHead *) TableArenaAt( arena, (uint32_t) map.entries.value );
        each.first = head->first;
    }
    return each;
}

// AN UNREACHED SLOT MUST HOLD NO MAP WITH ENTRIES IN IT (§2.8, §7.6). An empty
// map takes no bytes, so a record whose extent measures ZERO is a record whose
// every by-value map is empty; a measure that REFUSED answers non-zero here
// too, and refusing on it is the same answer one level up.
inline bool TableMapUnreachedEmpty( int64_t extent ) { return extent == 0; }

// ---- the LOAD side: where a decoded entry lands (§2.8) ----
//
// THE READER TRUSTS NOTHING and spends one compare per entry. Every load path
// applies the same rules and produces one report (§4), so the region load of
// §6.5 and LoadBuilder never disagree about a wire. These two shapes are what
// makes that true with one generated decoder: a REGION carves the entry array
// out of the holder node's own extent, and the TOOL's path appends into the
// builder's arena, and the decoder above them cannot tell which it has.

// TableMapCarve is a node's extent cursor, PRE-ORDER: a map's whole entry
// array first, then, entry by entry in key order, the arrays of any map an
// entry's value holds by value. The cursor is the node map's, because the
// generated decoder is threaded with that and not with a region.
struct TableMapCarve
{
    uint8_t * at = NULL;         // the region path: the node's extent, unspent
    int64_t left = 0;
    TableWorker * worker = NULL; // the TOOL's path: entries come from the arena
};

// TableMapFill is one map field being decoded: where the next entry lands, and
// the entry that last LANDED, which is what the ascending check compares
// against.
template <typename Entry> struct TableMapFill
{
    TableMap<Entry> * map = NULL;
    Entry * array = NULL;        // the region path: the carved array
    int32_t capacity = 0;
    TableWorker * worker = NULL; // the TOOL's path
    bool ok = false;
};

template <typename Entry>
inline TableMapFill<Entry> TableMapFillBegin( const TableNodeMap & nodes, TableMap<Entry> & map, uint32_t n )
{
    TableMapFill<Entry> fill;
    fill.map = &map;
    map.entries.value = 0;
    map.count = 0;
    if ( nodes.carve == NULL ) { return fill; }
    if ( nodes.carve->worker != NULL )
    {
        fill.worker = nodes.carve->worker; // the tool's path: the arena carves
        fill.ok = true;
        return fill;
    }
    const int64_t align = (int64_t) alignof( Entry );
    uint8_t * base = (uint8_t *) ( ( (uintptr_t) nodes.carve->at + (uintptr_t) ( align - 1 ) ) & ~( (uintptr_t) ( align - 1 ) ) );
    const int64_t bytes = (int64_t) n * (int64_t) sizeof( Entry );
    const int64_t pad = (int64_t) ( base - nodes.carve->at );
    if ( pad + bytes > nodes.carve->left ) { return fill; } // the measure and the load disagree: refused
    nodes.carve->at = base + bytes;
    nodes.carve->left -= pad + bytes;
    fill.array = (Entry *) base;
    fill.capacity = (int32_t) n;
    map.entries.value = (int64_t) ( base - (const uint8_t *) &map.entries );
    fill.ok = true;
    return fill;
}

// the entry that last LANDED — NULL before the first
template <typename Entry> inline Entry * TableMapFillLast( TableMapFill<Entry> & fill )
{
    if ( fill.map->count <= 0 ) { return NULL; }
    if ( fill.array != NULL ) { return fill.array + ( fill.map->count - 1 ); }
    return TableMapLive( *fill.worker->arena, *fill.map, fill.map->count - 1 );
}

// the next slot, at the entry type's declared defaults
template <typename Entry> inline Entry * TableMapFillNext( TableMapFill<Entry> & fill )
{
    if ( fill.array != NULL )
    {
        if ( fill.map->count >= fill.capacity ) { return NULL; }
        Entry * entry = fill.array + fill.map->count;
        TableReset( *entry );
        fill.map->count++;
        return entry;
    }
    TableMapHead * head = TableMapReach( *fill.worker, *fill.map );
    if ( head == NULL ) { return NULL; }
    Entry * entry = TableMapAppend( *fill.worker, head, *fill.map );
    if ( entry != NULL ) { TableReset( *entry ); }
    return entry;
}

// A MAP WITH HALF ITS KEYS IS NOT A MAP (§2.8): at the first entry whose key
// kind disagrees with the reader's declaration the map resets to EMPTY, one
// kind_mismatch is counted for the map, and its remaining bytes are skipped.
template <typename Entry> inline void TableMapFillReset( TableMapFill<Entry> & fill )
{
    if ( fill.array != NULL )
    {
        fill.map->entries.value = 0;
        fill.map->count = 0;
        return;
    }
    if ( fill.map->entries.value != 0 )
    {
        TableMapHead * head = (TableMapHead *) TableArenaAt( *fill.worker->arena, (uint32_t) fill.map->entries.value );
        head->first.value = 0;
        head->last.value = 0;
        head->live = 0;
        head->dead = 0;
    }
    fill.map->count = 0;
}

// an EMPTY map's reference is null in both encodings, so a load that placed
// nothing leaves the slot exactly as a Reset does
template <typename Entry> inline void TableMapFillEnd( TableMapFill<Entry> & fill )
{
    if ( fill.array != NULL && fill.map->count == 0 ) { fill.map->entries.value = 0; }
}

// the k-th LIVE entry of a builder map, in insertion order — what the tool
// path's ascending check compares against
template <typename Entry>
inline Entry * TableMapLive( const TableArena & arena, const TableMap<Entry> & map, int32_t index )
{
    if ( map.entries.value == 0 ) { return NULL; }
    const TableMapHead * head = (const TableMapHead *) TableArenaAt( arena, (uint32_t) map.entries.value );
    TableRef segment_ref = head->first;
    int32_t at = 0;
    while ( segment_ref.value != 0 )
    {
        TableMapSegment<Entry> * segment = (TableMapSegment<Entry> *) TableArenaAt( arena, (uint32_t) segment_ref.value );
        for ( int32_t i = 0; i < segment->used; i++ )
        {
            if ( TableMapSegmentDead( segment->dead, i ) ) { continue; }
            if ( at == index ) { return segment->entries + i; }
            at++;
        }
        segment_ref = segment->next;
    }
    return NULL;
}

// ---- LoadMeasure's term, from the FRAMING alone (§2.8, §6.5) ----
//
// LoadMeasure's term for a map is N x sizeof( Entry ) rounded to
// alignof( Entry ), AT EVERY DEPTH. N is framing and not a value, so this
// reads no field: it walks the map's own header and, where an entry's value
// holds a map of its own, the entries' headers under it. The caller owns the
// allocation precisely so it can refuse a number it did not expect.
typedef bool ( * TableMapWireExtentFn )( const uint8_t * body, int64_t length, int64_t & at, const TableIdTable * ids );

// A MAP ENTRY'S SMALLEST WIRE FOOTPRINT that commands one storage unit is its
// own L and the body's terminator, and under this form's variable lengths that
// footprint is TWO BYTES (docs/SPEC-TABLES.md §4.2). It is what bounds the N a
// map's L can carry, and therefore what a LoadMeasure may be asked for.
static const int64_t kTableMapEntryFloor = 2;

inline bool TableMapWireExtent( const uint8_t * body, int64_t length, int64_t & at,
                                int64_t entry_size, int64_t entry_align, TableMapWireExtentFn inner,
                                const TableIdTable * ids )
{
    TableReport scratch;
    TableReader r( body, length, &scratch, ids );
    if ( length < 2 ) { return true; }  // no array header: nothing rides
    if ( r.get8() != 13 ) { return true; } // not an array of tables: §4's ordinary kind mismatch
    uint64_t n = 0;
    if ( !r.getleb( n ) ) { return true; }
    const int64_t rest = length - r.offset;
    if ( n > (uint64_t) ( rest / kTableMapEntryFloor ) ) { return false; } // an N the map's L cannot carry
    at = ( at + entry_align - 1 ) & ~( entry_align - 1 );
    at += (int64_t) n * entry_size;
    if ( inner == NULL ) { return true; } // no map below an entry: one depth is the whole term
    for ( uint64_t i = 0; i < n; i++ )
    {
        uint64_t elem = 0;
        if ( !r.getleb( elem ) || !r.room( elem ) ) { return true; } // framing damage: the load reports it
        if ( !inner( r.buffer + r.offset, (int64_t) elem, at, ids ) ) { return false; }
        r.offset += (int64_t) elem;
    }
    return true;
}

// the same framing walk over an ARRAY OF TABLES that is not a map: its
// elements' own maps are part of this node's extent too
inline bool TableWireExtentElements( const uint8_t * body, int64_t length, int64_t & at, TableMapWireExtentFn inner, const TableIdTable * ids )
{
    TableReport scratch;
    TableReader r( body, length, &scratch, ids );
    if ( length < 2 ) { return true; }
    if ( r.get8() != 13 ) { return true; }
    uint64_t n = 0;
    if ( !r.getleb( n ) ) { return true; }
    for ( uint64_t i = 0; i < n; i++ )
    {
        uint64_t elem = 0;
        if ( !r.getleb( elem ) || !r.room( elem ) ) { return true; }
        if ( !inner( r.buffer + r.offset, (int64_t) elem, at, ids ) ) { return false; }
        r.offset += (int64_t) elem;
    }
    return true;
}

// and over an ENUM-KEYED array, whose triples carry a key REFERENCE before each
// length-prefixed element (docs/SPEC-TABLES.md §3.2)
inline bool TableWireExtentKeyed( const uint8_t * body, int64_t length, int64_t & at, TableMapWireExtentFn inner, const TableIdTable * ids )
{
    TableReport scratch;
    TableReader r( body, length, &scratch, ids );
    if ( length < 2 ) { return true; }
    if ( r.get8() != 13 ) { return true; }
    uint64_t n = 0;
    if ( !r.getleb( n ) ) { return true; }
    for ( uint64_t i = 0; i < n; i++ )
    {
        uint64_t key = 0;
        if ( !r.getleb( key ) ) { return true; }
        uint64_t elem = 0;
        if ( !r.getleb( elem ) || !r.room( elem ) ) { return true; }
        if ( !inner( r.buffer + r.offset, (int64_t) elem, at, ids ) ) { return false; }
        r.offset += (int64_t) elem;
    }
    return true;
}

// ---- the TEXT form's placement (docs/SPEC-TABLES.md §2.8, §16) ----
//
// The text is a plain JSON object keyed by the KEY, and the generic walk fills
// it through the ENTRY'S OWN descriptor — so all it needs from here is one
// entry at one key, handed back at its defaults. It is the builder's Insert
// with the ENTRY returned rather than its value, because the walk writes the
// value through a field row and not through a typed pointer.
template <typename Entry, typename Key>
inline Entry * TableMapPlace( TableWorker & worker, TableMap<Entry> & map, Key key )
{
    if ( worker.arena == NULL ) { return NULL; }
    Entry * found = TableMapScan( *worker.arena, map, key );
    if ( found != NULL )
    {
        TableResetMapValue( *found ); // a repeated key is LAST-WINS, whole
        return found;
    }
    TableMapHead * head = TableMapReach( worker, map );
    if ( head == NULL ) { return NULL; }
    Entry * entry = TableMapAppend( worker, head, map );
    if ( entry != NULL ) { TableReset( *entry ); }
    return entry;
}

// ---- the OPTIONAL RUNTIME INDEX (§2.8) ----
//
// Open addressing with LINEAR PROBING over the sorted array, built AT LOAD for
// a map large enough that log n compares over a cold array cost more than one
// hash and a probe. IT IS NEVER STORED: the caller measures it, owns its
// storage, builds it in one pass and releases it whenever.
//
// ITS HASH AND ITS LOAD FACTOR ARE NOT A CROSS-PORT CONTRACT, and that is a
// rule. What a port is held to is the CONTRACT of the lookup: the same value
// the sorted array's Find returns for the same key, and no allocation past the
// storage the caller handed in.
struct TableMapIndex
{
    int32_t * slots = NULL; // entry indices, +1; 0 is an empty slot
    int32_t capacity = 0;
    bool good = false;
};

// this runtime's own, and no port reproduces it: fnv1a64 over the key's bytes
inline uint64_t TableMapHash( const void * bytes, int32_t length )
{
    uint64_t hash = 0xCBF29CE484222325ull;
    const uint8_t * at = (const uint8_t *) bytes;
    for ( int32_t i = 0; i < length; i++ ) { hash ^= (uint64_t) at[i]; hash *= 0x100000001B3ull; }
    return hash;
}
inline uint64_t TableMapHash( uint64_t key ) { return TableMapHash( (const void *) &key, (int32_t) sizeof( key ) ); }

// this runtime's own load factor, and no port reproduces it either: the next
// power of two at or above twice the count, so a probe run stays short
inline int32_t TableMapIndexSlots( int32_t count )
{
    int32_t slots = 8;
    while ( slots < count * 2 ) { slots *= 2; }
    return slots;
}

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// ---- the per-entry helpers the runtime hangs off (docs/SPEC-TABLES.md §2.8) ----

// emitMapEntrySurface emits the four overloads one generated entry type
// contributes to the runtime: the ORDER against another entry and against a
// bare key, the key in the form a call site takes it, and what a Find answers.
//
// They are free functions in the unit's namespace, reached from the runtime's
// templates by argument-dependent lookup at instantiation — the same bridge
// TableReset takes, and the reason the runtime can be a template over an entry
// type whose header it does not name.
func (g *tableGen) emitMapEntrySurface(owner *ir.Struct, f *ir.Field) {
	entry := mapEntryOf(f)
	key := ir.MapKeyField(f)
	value := ir.MapValueField(f)
	n := entry.Name
	g.pf("// ---- %s: the order, the key and the value (docs/SPEC-TABLES.md §2.8) ----\n", n)
	g.pf("//\n")
	g.pf("// The four overloads the map runtime's templates reach by argument-dependent\n")
	g.pf("// lookup. Nothing outside this file names them.\n")
	g.pf("static_assert( alignof( %s ) <= kTableAlign, \"a map entry's alignment must fit the arena's\" );\n", n)
	if mapKeyIsString(f) {
		bound := key.Type.Size
		g.pf("static const int32_t k%sKeyBound = %d; // string(%d): the key's storage, and the length an insert refuses past\n\n", n, bound, bound)
		g.pf("inline int TableEntryOrder( const %s & a, const %s & b )\n{\n", n, n)
		g.pf("    return TableKeyOrder( a.key, a.key_length, b.key, b.key_length );\n}\n")
		g.pf("inline int TableEntryOrder( const %s & entry, const char * key )\n{\n", n)
		g.pf("    return TableKeyOrder( entry.key, entry.key_length, key, TableKeyLength( key, k%sKeyBound ) );\n}\n", n)
		g.pf("inline const char * TableEntryKey( const %s & entry ) { return entry.key; } // NUL-terminated, the storage's own bytes\n", n)
		g.pf("inline void TableEntrySetKey( %s & entry, const char * key, int32_t length )\n{\n", n)
		g.pf("    memcpy( (void *) entry.key, (const void *) key, (size_t) length );\n")
		g.pf("    entry.key[length] = 0;\n")
		g.pf("    entry.key_length = length;\n}\n")
	} else {
		order := mapKeyOrderType(f)
		typ, _ := g.cppFieldType(key.Type)
		g.pf("\ninline int TableEntryOrder( const %s & a, const %s & b )\n{\n", n, n)
		g.pf("    return TableKeyOrder( (%s) a.key, (%s) b.key ); // integers compare by VALUE, %s\n}\n", order, order, signedWord(key))
		g.pf("inline int TableEntryOrder( const %s & entry, %s key )\n{\n", n, typ)
		g.pf("    return TableKeyOrder( (%s) entry.key, (%s) key );\n}\n", order, order)
		g.pf("inline %s TableEntryKey( const %s & entry ) { return entry.key; }\n", typ, n)
		g.pf("inline void TableEntrySetKey( %s & entry, %s key ) { entry.key = key; }\n", n, typ)
	}
	// what a Find answers, and what an iteration yields beside the key
	if mapValueIsPointer(f) {
		t := value.Type.Name
		g.pf("// a map[K]*T: the const Find answers the RESOLVED pointer, one add on the\n")
		g.pf("// self-relative delta, exactly as <T>At answers it (§6.2, §6.3)\n")
		g.pf("inline const %s * TableEntryFound( const %s * entry ) { return entry != NULL ? %sAt( entry->value ) : NULL; }\n", t, n, t)
		g.pf("inline TableRef * TableEntryValue( %s * entry ) { return &entry->value; } // the builder hands back the SLOT\n", n)
	} else {
		typ := g.mapValueStorageType(f)
		g.pf("inline const %s * TableEntryFound( const %s * entry ) { return entry != NULL ? &entry->value : NULL; }\n", typ, n)
		g.pf("inline %s * TableEntryValue( %s * entry ) { return &entry->value; }\n", typ, n)
	}
	// the builder's iteration proxy: the key beside the value, by value
	g.pf("struct %sEach { %s key; decltype( TableEntryValue( (%s *) NULL ) ) value; };\n", n, g.mapKeyCallType(f), n)
	g.pf("inline %sEach TableEntryEach( %s * entry ) { return %sEach{ TableEntryKey( *entry ), TableEntryValue( entry ) }; }\n", n, n, n)
	// the value half an Insert resets before handing it back
	// the parameter is named `value` because the field it resets is the
	// entry's own `value`, which is what the reset emitter spells
	g.pf("inline void TableResetMapValue( %s & value )\n{\n", n)
	g.emitMapValueReset(f)
	g.pf("}\n\n")
	_ = owner
}

// signedWord names the compare an integer key takes, for the comment.
func signedWord(key *ir.Field) string {
	if key.Type.Signed {
		return "signed here"
	}
	return "unsigned here"
}

// mapValueStorageType is the C++ spelling of a map value's storage.
func (g *tableGen) mapValueStorageType(f *ir.Field) string {
	value := ir.MapValueField(f)
	if value.IsMap() {
		return fmt.Sprintf("TableMap<%s>", value.MapEntry.Name)
	}
	typ, _ := g.cppFieldType(value.Type)
	return typ
}

// emitMapValueReset gives an entry's VALUE half its declared defaults — what
// an Insert hands back, and what a DUPLICATE is reset to before the repeat
// fills it (docs/SPEC-TABLES.md §2.8).
func (g *tableGen) emitMapValueReset(f *ir.Field) {
	value := ir.MapValueField(f)
	saved := g.indent
	g.indent = ""
	g.emitTableResetField(value)
	g.indent = saved
}

// emitMapSurfaces emits every map field's entry surface, in closure order, so
// that each is declared before the first codec body that instantiates the
// runtime over it.
func (g *tableGen) emitMapSurfaces(members []*ir.Struct) {
	if !g.anyMap {
		return
	}
	for _, st := range members {
		for _, f := range mapFieldsOf(st) {
			g.emitMapEntrySurface(st, f)
			g.emitMapKeyReader(f)
		}
	}
}

// ---- the MAP's wire codecs (docs/SPEC-TABLES.md §2.8, §3) ----
//
// A map rides as a kind 14 ARRAY of kind 13 elements — an array of the
// generated entry, in ascending key order — so nothing below is a new framing
// rule. What is the map's own is the SORT on the write side and the key scan,
// the ascending check and the duplicate rule on the read side.

// emitMapMeasureField adds one map field to a body's measured size. The
// entries come off the ordered cursor, so measure and save derive the same
// order from the same builder and nothing passes between them.
func (g *tableGen) emitMapMeasureField(f *ir.Field) {
	entry := mapEntryOf(f)
	id := ir.TableFieldWireId(f)
	g.pf("    {\n")
	g.pf("        // %s: a kind %d array of kind %d elements, ASCENDING (§2.8)\n", f.Name, tkArray, tkTable)
	g.pf("        TableMapCursor<%s> order_%s = TableMapOrder( ctx, value.%s );\n", entry.Name, f.Name, f.Name)
	g.pf("        if ( !order_%s.ok ) { return -1; } // the sort could not run\n", f.Name)
	g.pf("        if ( order_%s.count > 0 )\n        {\n", f.Name)
	g.pf("            const uint64_t ref_%s = %s;\n", f.Name, wireRef(id))
	g.pf("            int64_t body_%s = 1 + TableLebBytes( (uint64_t) order_%s.count ); // the element kind byte and the count\n", f.Name, f.Name)
	g.pf("            for ( int32_t i = 0; i < order_%s.count; i++ )\n            {\n", f.Name)
	g.pf("                const int64_t elem_%s = %s;\n", f.Name, g.measureCall(entry.Name, "*order_"+f.Name+"[i]"))
	g.pf("                if ( elem_%s < 0 ) { TableMapRelease( order_%s ); return -1; }\n", f.Name, f.Name)
	g.pf("                body_%s += %s; // BUT THE ENTRY ALWAYS RIDES: identity here is the key\n", f.Name, framed("elem_"+f.Name))
	g.pf("            }\n")
	g.pf("            bytes += TableLebBytes( ref_%s ) + 1 + %s;\n", f.Name, framed("body_"+f.Name))
	g.pf("        }\n")
	g.pf("        TableMapRelease( order_%s );\n", f.Name)
	g.pf("    }\n")
}

// emitMapWriteField writes one map field: the array framing, then the entries
// in ASCENDING key order with no key twice, dead entries dropped.
func (g *tableGen) emitMapWriteField(f *ir.Field) {
	entry := mapEntryOf(f)
	id := ir.TableFieldWireId(f)
	g.pf("    {\n")
	g.pf("        TableMapCursor<%s> order_%s = TableMapOrder( ctx, value.%s ); // %s\n", entry.Name, f.Name, f.Name, f.Name)
	g.pf("        if ( !order_%s.ok ) { return false; }\n", f.Name)
	g.pf("        if ( order_%s.count > 0 ) // an EMPTY map elides, the by-value rule (§3)\n        {\n", f.Name)
	g.pf("            const uint64_t ref_%s = %s;\n", f.Name, wireRef(id))
	g.pf("            int64_t body_%s = 1 + TableLebBytes( (uint64_t) order_%s.count );\n", f.Name, f.Name)
	g.pf("            for ( int32_t i = 0; i < order_%s.count; i++ )\n            {\n", f.Name)
	g.pf("                const int64_t elem_%s = %s;\n", f.Name, g.measureCall(entry.Name, "*order_"+f.Name+"[i]"))
	g.pf("                if ( elem_%s < 0 ) { TableMapRelease( order_%s ); return false; }\n", f.Name, f.Name)
	g.pf("                body_%s += %s;\n", f.Name, framed("elem_"+f.Name))
	g.pf("            }\n")
	g.pf("            w.putleb( ref_%s ); w.put8( %d ); w.putleb( (uint64_t) body_%s );\n", f.Name, tkArray, f.Name)
	g.pf("            w.put8( %d ); w.putleb( (uint64_t) order_%s.count );\n", tkTable, f.Name)
	g.pf("            for ( int32_t i = 0; i < order_%s.count; i++ )\n            {\n", f.Name)
	g.pf("                const int64_t elem_len_%s = %s;\n", f.Name, g.measureCall(entry.Name, "*order_"+f.Name+"[i]"))
	g.pf("                if ( elem_len_%s < 0 ) { TableMapRelease( order_%s ); return false; }\n", f.Name, f.Name)
	g.pf("                w.putleb( (uint64_t) elem_len_%s );\n", f.Name)
	g.pf("                if ( !%s ) { TableMapRelease( order_%s ); return false; }\n", g.saveCall(entry.Name, "*order_"+f.Name+"[i]"), f.Name)
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("        TableMapRelease( order_%s );\n", f.Name)
	g.pf("    }\n")
}

// ---- the KEY SCAN (docs/SPEC-TABLES.md §2.8) ----

// emitMapKeyReader emits one entry's key scan: the reader does not assume
// where the key sits, so before an entry's body decodes it SCANS that body's
// field headers for the key's id and reads the key. Where the body carries the
// id more than once the scan reads the occurrence §3's repeated-field rule
// keeps, the last one; a body with no key field at all has the key's default.
func (g *tableGen) emitMapKeyReader(f *ir.Field) {
	entry := mapEntryOf(f)
	key := ir.MapKeyField(f)
	n := entry.Name
	kind := tableScalarKind(key)
	g.pf("// %sReadKey: the key, before the slot is chosen (docs/SPEC-TABLES.md §2.8).\n", n)
	g.pf("// Field order inside a body is not contractual (§3), so this scans rather\n")
	g.pf("// than assuming a position — and this implementation writes the key first,\n")
	g.pf("// so on any wire it wrote the scan ends at the first header.\n")
	g.pf("struct %sKeyRead\n{\n", n)
	if mapKeyIsString(f) {
		g.pf("    const char * key;   // INTO the entry body; nothing is copied\n")
		g.pf("    int32_t length;\n")
	} else {
		typ, _ := g.cppFieldType(key.Type)
		g.pf("    %s key;\n", typ)
	}
	g.pf("    bool found;         // the body carried the key's id\n")
	g.pf("    bool kind_bad;      // it carried it under another kind: the MAP's event\n")
	g.pf("    bool over;          // longer than this reader's bound: the ENTRY is dropped\n")
	g.pf("    bool malformed;     // the entry's framing gave out\n")
	g.pf("};\n\n")
	g.pf("inline %sKeyRead %sReadKey( const uint8_t * body, int64_t length, const TableIdTable * ids )\n{\n", n, n)
	if mapKeyIsString(f) {
		g.pf("    %sKeyRead out = { NULL, 0, false, false, false, false };\n", n)
	} else {
		g.pf("    %sKeyRead out = { 0, false, false, false, false };\n", n)
	}
	g.pf("    TableReport scratch; // the scan's own framing damage is the MAP's, raised by the caller\n")
	g.pf("    TableReader r( body, length, &scratch, ids );\n")
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t field_ref = 0;\n")
	g.pf("        if ( !r.getleb( field_ref ) ) { out.malformed = true; return out; }\n")
	g.pf("        if ( field_ref == 0 ) { return out; } // the terminator: no key field is the key's DEFAULT\n")
	g.pf("        if ( ids == NULL || field_ref > (uint64_t) ids->count ) { out.malformed = true; return out; }\n")
	g.pf("        const uint64_t field_id = ids->at( field_ref );\n")
	g.pf("        if ( !r.has( 1 ) ) { out.malformed = true; return out; }\n")
	g.pf("        uint8_t field_kind = r.get8();\n")
	g.pf("        if ( field_id == 0x%016xull ) // `key`, the ordinary hash of an ordinary name\n        {\n", ir.MapKeyWireId)
	g.pf("            out.kind_bad = field_kind != %d; // THE KEY KIND IS THE READER'S DECLARATION\n", kind)
	g.pf("            out.found = !out.kind_bad;\n")
	g.pf("            if ( !out.kind_bad )\n            {\n")
	if mapKeyIsString(f) {
		g.pf("                uint64_t key_len = 0;\n")
		g.pf("                if ( !r.getleb( key_len ) || !r.room( key_len ) ) { out.malformed = true; return out; }\n")
		g.pf("                out.key = (const char *) ( r.buffer + r.offset );\n")
		g.pf("                out.length = (int32_t) key_len;\n")
		g.pf("                out.over = key_len > %d; // KEYS NEVER CLAMP: the entry is dropped whole\n", key.Type.Size)
		g.pf("                r.offset += (int64_t) key_len;\n")
		g.pf("                continue; // the LAST occurrence is the one §3 keeps\n")
	} else {
		width := tableKindWidth(kind)
		typ, _ := g.cppFieldType(key.Type)
		g.pf("                if ( !r.has( %d ) ) { out.malformed = true; return out; }\n", width)
		g.pf("                out.key = (%s) r.%s();\n", typ, tableGet(width))
		g.pf("                continue; // the LAST occurrence is the one §3 keeps\n")
	}
	g.pf("            }\n        }\n")
	g.pf("        if ( !r.skip( field_kind ) ) { out.malformed = true; return out; }\n")
	g.pf("    }\n}\n\n")
}

// emitMapReadField decodes one map field, and it is where every reader rule
// §2.8 states lands: the key before the slot, ascending against the key of the
// last entry that LANDED, a duplicate that resets the slot it took, a
// descending key that stops the map and lets the parent read on, a key that
// does not fit skipped by its L and counted clamped, and a key kind the
// reader does not declare emptying the map for one kind_mismatch.
func (g *tableGen) emitMapReadField(f *ir.Field) {
	entry := mapEntryOf(f)
	key := ir.MapKeyField(f)
	n := entry.Name
	ind := "                "
	g.pf("%suint64_t body_len = 0;\n", ind)
	g.pf("%sif ( !r.getleb( body_len ) || !r.room( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
	g.pf("%sint64_t body_end = r.offset + (int64_t) body_len;\n", ind)
	g.pf("%sif ( body_len >= 2 )\n%s{\n", ind, ind)
	g.pf("%s    uint8_t elem_kind = r.get8();\n", ind)
	g.pf("%s    uint64_t count = 0;\n", ind)
	g.pf("%s    if ( !r.getleb( count ) ) { r.report->malformed = true; r.offset = body_end; break; }\n", ind)
	g.pf("%s    // A MAP HEADER WHOSE ELEMENT KIND IS NOT %d is the ordinary array\n", ind, tkTable)
	g.pf("%s    // kind mismatch of §4, and nothing about a map is special-cased\n", ind)
	g.pf("%s    if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, tkTable)
	g.pf("%s    TableMapFill<%s> fill = TableMapFillBegin( nodes, value.%s, (uint32_t) count );\n", ind, n, f.Name)
	g.pf("%s    if ( !fill.ok ) { r.report->malformed = true; r.offset = body_end; break; }\n", ind)
	g.pf("%s    TableReader sub( r.buffer + r.offset, body_end - r.offset, r.report, r.ids );\n", ind)
	if mapKeyIsString(f) {
		g.pf("%s    const char * last_key = NULL; int32_t last_length = 0;\n", ind)
	} else {
		typ, _ := g.cppFieldType(key.Type)
		g.pf("%s    %s last_key = 0;\n", ind, typ)
	}
	g.pf("%s    bool landed = false;\n", ind)
	g.pf("%s    for ( uint64_t i = 0; i < count; i++ )\n%s    {\n", ind, ind)
	g.pf("%s        uint64_t elem_len = 0;\n", ind)
	g.pf("%s        if ( !sub.getleb( elem_len ) || !sub.room( elem_len ) ) { r.report->malformed = true; break; }\n", ind)
	g.pf("%s        const uint8_t * elem_body = sub.buffer + sub.offset;\n", ind)
	g.pf("%s        sub.offset += (int64_t) elem_len;\n", ind)
	g.pf("%s        %sKeyRead read = %sReadKey( elem_body, (int64_t) elem_len, r.ids );\n", ind, n, n)
	g.pf("%s        // THE KEY KIND IS CHECKED FIRST: a key read under another kind\n", ind)
	g.pf("%s        // desynchronizes the rest of the scan, and the honest answer to a\n", ind)
	g.pf("%s        // body whose key is not this reader's kind is the KIND, not the\n", ind)
	g.pf("%s        // framing damage that follows from it.\n", ind)
	g.pf("%s        if ( read.kind_bad )\n%s        {\n", ind, ind)
	g.pf("%s            // A MAP WITH HALF ITS KEYS IS NOT A MAP (§2.8): the map resets\n", ind)
	g.pf("%s            // to EMPTY, ONE kind_mismatch is counted for it, and the rest\n", ind)
	g.pf("%s            // is skipped. Events counted inside earlier entries stand.\n", ind)
	g.pf("%s            r.report->kind_mismatch++;\n", ind)
	g.pf("%s            TableMapFillReset( fill );\n", ind)
	g.pf("%s            break;\n", ind)
	g.pf("%s        }\n", ind)
	g.pf("%s        if ( read.malformed ) { r.report->malformed = true; break; }\n", ind)
	g.pf("%s        if ( read.over ) { r.report->clamped++; continue; } // skipped by its L, one count per entry\n", ind)
	if mapKeyIsString(f) {
		g.pf("%s        const int order = landed ? TableKeyOrder( last_key, last_length, read.key, read.length ) : -1;\n", ind)
	} else {
		ord := mapKeyOrderType(f)
		g.pf("%s        const int order = landed ? TableKeyOrder( (%s) last_key, (%s) read.key ) : -1;\n", ind, ord, ord)
	}
	g.pf("%s        if ( order > 0 )\n%s        {\n", ind, ind)
	g.pf("%s            // DESCENDING: not a body any conforming writer produced. The map\n", ind)
	g.pf("%s            // keeps the ascending prefix it has, the rest skips by the map's\n", ind)
	g.pf("%s            // L, and the PARENT reads on past the field's length (§4).\n", ind)
	g.pf("%s            r.report->malformed = true;\n", ind)
	g.pf("%s            break;\n", ind)
	g.pf("%s        }\n", ind)
	g.pf("%s        %s * slot = NULL;\n", ind, n)
	g.pf("%s        if ( order == 0 )\n%s        {\n", ind, ind)
	g.pf("%s            // EQUAL: a DUPLICATE. The slot that entry took is reset to the\n", ind)
	g.pf("%s            // entry's defaults by the decode below, so LAST WINS WHOLE and an\n", ind)
	g.pf("%s            // elided field of the repeat reads as its default. The map's\n", ind)
	g.pf("%s            // count excludes it.\n", ind)
	g.pf("%s            slot = TableMapFillLast( fill );\n", ind)
	g.pf("%s            r.report->duplicate++;\n", ind)
	g.pf("%s        }\n%s        else\n%s        {\n", ind, ind, ind)
	g.pf("%s            slot = TableMapFillNext( fill ); // ASCENDING: the next slot\n", ind)
	g.pf("%s        }\n", ind)
	g.pf("%s        if ( slot == NULL ) { r.report->malformed = true; break; }\n", ind)
	g.pf("%s        {\n", ind)
	g.pf("%s            TableReader elem( elem_body, (int64_t) elem_len, r.report, r.ids );\n", ind)
	g.pf("%s            %s;\n", ind, g.loadCall(n, "elem", "*slot"))
	g.pf("%s        }\n", ind)
	if mapKeyIsString(f) {
		g.pf("%s        last_key = read.key; last_length = read.length; // the WIRE keys of the entries that LAND\n", ind)
	} else {
		g.pf("%s        last_key = read.key; // the WIRE keys of the entries that LAND\n", ind)
	}
	g.pf("%s        landed = true;\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s    TableMapFillEnd( fill );\n", ind)
	g.pf("%s}\n", ind)
	g.pf("%sr.offset = body_end; // the remaining entries skip by the map's L\n", ind)
}

// ---- the NODE EXTENT (docs/SPEC-TABLES.md §2.8, §6.3) ----
//
// A map's entries are BY-VALUE RECORDS INSIDE THE HOLDER'S NODE EXTENT, laid
// after the record's own storage: count x sizeof( Entry ) at alignof( Entry ),
// zero slack, one array per map reachable BY VALUE from the record — which
// includes a map inside a nested table and a map inside an entry — in
// depth-first field order. The placement is PRE-ORDER: a map's whole entry
// array first, then, entry by entry in key order, the arrays of any map an
// entry's value holds by value.
//
// Two emitters walk that layout and they are ONE walk: the measure advances a
// running offset, and the pack advances the same one and copies. Nothing
// passes between them, which is what makes `used == total` a real check.

// hasMapExtent reports a member with any map reachable by value — the members
// that carry an extent, and the ones whose two extent walks are emitted.
func (g *tableGen) hasMapExtent(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.IsMap() {
			return true
		}
		// A MAP REACHABLE BY VALUE IS THIS RECORD'S EXTENT (docs/SPEC-TABLES.md
		// §2.8), whichever by-value edge reaches it: a nested table, an array
		// of them, an enum-keyed array of them, or a union arm.
		switch g.edgeOf(f) {
		case edgeNested:
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && g.hasMapExtent(ref) {
				return true
			}
		case edgeArm:
			for _, v := range f.Type.Ref.(*ir.Union).Variants {
				if ref := memberOf(g.unit, v.Type); ref != nil && g.hasMapExtent(ref) {
					return true
				}
			}
		}
		// a nested table this walk does not call an edge can still hold a map,
		// because a map makes its holder VARIABLE and every variable nesting is
		// an edge — so there is nothing else to look at here
	}
	return false
}

// memberOf resolves one closure member by name.
func memberOf(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// emitMapExtent emits `<T>MapExtentAt`: the running offset every map array
// reachable by value from one record takes, in the order the pack lays them.
func (g *tableGen) emitMapExtent(st *ir.Struct) {
	g.pf("// %sMapExtentAt: the node extent %s's maps take, PRE-ORDER, advancing the\n", st.Name, st.Name)
	g.pf("// running offset exactly as %sMapPack advances it (docs/SPEC-TABLES.md §2.8).\n", st.Name)
	g.pf("template <typename Ctx>\ninline bool %sMapExtentAt( const Ctx & ctx, const %s & value, int64_t & at )\n{\n", st.Name, st.Name)
	if !g.hasMapExtent(st) {
		g.pf("    (void) ctx; (void) value; (void) at; // no map below this record\n")
		g.pf("    return true;\n}\n\n")
		return
	}
	g.emitMapExtentWalk(st, "value", func(f *ir.Field, expr, ind string) {
		entry := mapEntryOf(f)
		g.pf("%s{\n", ind)
		g.pf("%s    TableMapCursor<%s> cursor = TableMapOrder( ctx, %s );\n", ind, entry.Name, expr)
		g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
		g.pf("%s    at = ( at + %d ) & ~(int64_t) %d; // at alignof( %s )\n", ind, alignOfEntry(g.unit, entry)-1, alignOfEntry(g.unit, entry)-1, entry.Name)
		g.pf("%s    at += (int64_t) cursor.count * (int64_t) sizeof( %s ); // the whole array FIRST\n", ind, entry.Name)
		if g.isVar(entry.Name) {
			g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ ) // then, entry by entry in key order\n%s    {\n", ind, ind)
			g.pf("%s        if ( !%sMapExtentAt( ctx, *cursor[i], at ) ) { TableMapRelease( cursor ); return false; }\n", ind, entry.Name)
			g.pf("%s    }\n", ind)
		}
		g.pf("%s    TableMapRelease( cursor );\n", ind)
		g.pf("%s}\n", ind)
	}, func(table, expr, ind string) {
		g.pf("%sif ( !%sMapExtentAt( ctx, %s, at ) ) { return false; }\n", ind, table, expr)
	})
	g.pf("    return true;\n}\n\n")
	g.pf("// the whole extent of one node, from a fresh offset: what a pack reserves\n")
	g.pf("// for it beside the record's own storage.\n")
	g.pf("template <typename Ctx>\ninline int64_t %sMapExtent( const Ctx & ctx, const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    int64_t at = 0;\n")
	g.pf("    if ( !%sMapExtentAt( ctx, value, at ) ) { return -1; }\n", st.Name)
	g.pf("    return at;\n}\n\n")
}

// emitMapPack emits `<T>MapPack`: the same walk, copying each map's entries in
// key order into the node's extent and pointing the record's slot at them.
func (g *tableGen) emitMapPack(st *ir.Struct) {
	g.pf("// %sMapPack: carve %s's map arrays out of the node's extent and copy the\n", st.Name, st.Name)
	g.pf("// entries in ASCENDING key order, PRE-ORDER, advancing the same running\n")
	g.pf("// offset %sMapExtentAt advances (docs/SPEC-TABLES.md §2.8).\n", st.Name)
	g.pf("template <typename Ctx>\ninline bool %sMapPack( const Ctx & ctx, const %s & src, %s & dst, uint8_t * extent, int64_t & at, int64_t capacity )\n{\n", st.Name, st.Name, st.Name)
	if !g.hasMapExtent(st) {
		g.pf("    (void) ctx; (void) src; (void) dst; (void) extent; (void) at; (void) capacity; // no map below this record\n")
		g.pf("    return true;\n}\n\n")
		return
	}
	dstOf := func(expr string) string { return "dst" + expr[len("src"):] }
	g.emitMapExtentWalk(st, "src", func(f *ir.Field, expr, ind string) {
		entry := mapEntryOf(f)
		slot := dstOf(expr)
		g.pf("%s{\n", ind)
		g.pf("%s    TableMapCursor<%s> cursor = TableMapOrder( ctx, %s );\n", ind, entry.Name, expr)
		g.pf("%s    if ( !cursor.ok ) { return false; }\n", ind)
		g.pf("%s    at = ( at + %d ) & ~(int64_t) %d;\n", ind, alignOfEntry(g.unit, entry)-1, alignOfEntry(g.unit, entry)-1)
		g.pf("%s    const int64_t bytes = (int64_t) cursor.count * (int64_t) sizeof( %s );\n", ind, entry.Name)
		g.pf("%s    if ( at + bytes > capacity ) { TableMapRelease( cursor ); return false; }\n", ind)
		g.pf("%s    %s * placed = (%s *) ( extent + at );\n", ind, entry.Name, entry.Name)
		g.pf("%s    at += bytes;\n", ind)
		g.pf("%s    %s.count = cursor.count;\n", ind, slot)
		g.pf("%s    %s.padding = 0;\n", ind, slot)
		g.pf("%s    %s.entries.value = cursor.count > 0 ? (int64_t) ( (uint8_t *) placed - (const uint8_t *) &%s.entries ) : 0;\n", ind, slot, slot)
		g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ )\n%s    {\n", ind, ind)
		g.pf("%s        memcpy( (void *) ( placed + i ), (const void *) cursor[i], sizeof( %s ) ); // trivially copyable, by construction\n", ind, entry.Name)
		g.pf("%s    }\n", ind)
		if g.isVar(entry.Name) {
			g.pf("%s    for ( int32_t i = 0; i < cursor.count; i++ )\n%s    {\n", ind, ind)
			g.pf("%s        if ( !%sMapPack( ctx, *cursor[i], placed[i], extent, at, capacity ) ) { TableMapRelease( cursor ); return false; }\n", ind, entry.Name)
			g.pf("%s    }\n", ind)
		}
		g.pf("%s    TableMapRelease( cursor );\n", ind)
		g.pf("%s}\n", ind)
	}, func(table, expr, ind string) {
		g.pf("%sif ( !%sMapPack( ctx, %s, %s, extent, at, capacity ) ) { return false; }\n", ind, table, expr, dstOf(expr))
	})
	g.pf("    return true;\n}\n\n")
}

// emitMapExtentWalk is the ONE walk both extent emitters take: the record's
// fields in declaration order, every map at its own position, descending each
// by-value edge in place. A pointer is NOT an edge here — a pointee is its own
// node with its own extent — and neither is a union arm that is one, for the
// same reason.
func (g *tableGen) emitMapExtentWalk(st *ir.Struct, subject string, mapField func(f *ir.Field, expr, ind string), descend func(table, expr, ind string)) {
	v := edgeVisitor{read: subject}
	for _, f := range st.Fields {
		if f.IsMap() {
			mapField(f, subject+"."+f.Name, "    ")
			continue
		}
		switch g.edgeOf(f) {
		case edgeNested:
			ref, _ := f.Type.Ref.(*ir.Struct)
			if ref == nil || !g.hasMapExtent(ref) {
				continue
			}
			g.emitVariableByValueWalk(f, v, func(expr edgeExpr) { descend(f.Type.Name, expr.Src, "        ") })
			g.emitUnreachedMapRefusal(f, ref, subject)
		case edgeArm:
			un := f.Type.Ref.(*ir.Union)
			any := false
			for _, arm := range un.Variants {
				if ref := memberOf(g.unit, arm.Type); ref != nil && g.hasMapExtent(ref) {
					any = true
				}
			}
			if !any {
				continue
			}
			// only the ARMS that hold a map are descended: a pointer arm and a
			// byte buffer arm reach nodes, not this node's extent
			armed := edgeVisitor{read: subject,
				pointer: func(*ir.Field, edgeExpr) {},
				blob:    func(*ir.Field, edgeExpr) {},
				descend: func(table string, expr edgeExpr, indent string) {
					if ref := memberOf(g.unit, table); ref != nil && g.hasMapExtent(ref) {
						descend(table, expr.Src, indent)
					}
				},
			}
			g.emitVariableUnionWalk(f, armed)
		}
	}
}

// alignOfEntry is the C ABI alignment of one generated entry record — the
// alignment its array is laid at, and the same model §20.3 commits the
// compiler to for every record in the closure.
func alignOfEntry(u *ir.Unit, entry *ir.Struct) int64 {
	if ml := ir.RecordLayout(u, entry); ml != nil && ml.Align > 0 {
		return ml.Align
	}
	return 8
}

// ---- the three walks at a map (docs/SPEC-TABLES.md §2.8, §3.1) ----
//
// A map is a BY-VALUE EDGE of the ONE declaration-order walk: it is reached at
// its field's position, its entries are visited in ASCENDING KEY ORDER, and
// each entry's value is descended for the pointer slots inside it before the
// next entry is reached. A map declared before a pointer field therefore
// reaches a shared node FIRST and numbers it first, exactly as a union arm or
// a nested table declared there does. The rule is the walk's, not the map's.

// mapNumberEdge descends one map's entries for the NUMBERING walk.
func (g *tableGen) mapNumberEdge(f *ir.Field) {
	g.emitMapEntryLoop(f, "value", "return false;", func(entry, elem, ind string) {
		g.pf("%sif ( !%sNumber( ctx, numbering, %s ) ) { TableMapRelease( cursor_%s ); return false; }\n", ind, entry, elem, f.Name)
	})
}

// mapPackMeasureEdge descends one map's entries for the PACK MEASURE.
func (g *tableGen) mapPackMeasureEdge(f *ir.Field) {
	g.emitMapEntryLoop(f, "value", "return -1;", func(entry, elem, ind string) {
		g.pf("%sint64_t inner = %sPackMeasure( ctx, seen, %s );\n", ind, entry, elem)
		g.pf("%sif ( inner < 0 ) { TableMapRelease( cursor_%s ); return -1; }\n", ind, f.Name)
		g.pf("%sbytes += inner;\n", ind)
	})
}

// mapPackEdge descends one map's entries for the PACK, against the array
// <T>MapPack already placed in the node's extent.
func (g *tableGen) mapPackEdge(f *ir.Field) {
	entry := mapEntryOf(f)
	g.emitMapEntryLoopHead(f, "src", "return false;")
	g.pf("        %s * placed_%s = (%s *) ( dst.%s.entries.value != 0 ? ( (uint8_t *) &dst.%s.entries + dst.%s.entries.value ) : NULL );\n",
		entry.Name, f.Name, entry.Name, f.Name, f.Name, f.Name)
	g.pf("        for ( int32_t i = 0; i < cursor_%s.count; i++ )\n        {\n", f.Name)
	g.pf("            if ( !%sPackEdges( ctx, seen, *cursor_%s[i], placed_%s[i], base, capacity, used ) ) { TableMapRelease( cursor_%s ); return false; }\n",
		entry.Name, f.Name, f.Name, f.Name)
	g.pf("        }\n")
	g.pf("        TableMapRelease( cursor_%s );\n    }\n", f.Name)
}

// emitMapEntryLoopHead opens one map's sorted cursor over the given subject.
func (g *tableGen) emitMapEntryLoopHead(f *ir.Field, subject, onBad string) {
	entry := mapEntryOf(f)
	g.pf("    { // %s: a by-value edge, entries in ASCENDING key order (§2.8, §3.1)\n", f.Name)
	g.pf("        TableMapCursor<%s> cursor_%s = TableMapOrder( ctx, %s.%s );\n", entry.Name, f.Name, subject, f.Name)
	g.pf("        if ( !cursor_%s.ok ) { %s }\n", f.Name, onBad)
}

// emitMapEntryLoop is the whole shape: the cursor, the loop, the release.
func (g *tableGen) emitMapEntryLoop(f *ir.Field, subject, onBad string, body func(entry, elem, ind string)) {
	entry := mapEntryOf(f)
	g.emitMapEntryLoopHead(f, subject, onBad)
	g.pf("        for ( int32_t i = 0; i < cursor_%s.count; i++ )\n        {\n", f.Name)
	body(entry.Name, fmt.Sprintf("*cursor_%s[i]", f.Name), "            ")
	g.pf("        }\n")
	g.pf("        TableMapRelease( cursor_%s );\n    }\n", f.Name)
}

// emitMapWalkSurface emits the two extent walks for every variable member of a
// map-bearing unit. They are emitted for EVERY such member, because a walk
// that descends a by-value nesting has to be able to name the nested one's.
func (g *tableGen) emitMapWalkSurface(members []*ir.Struct) {
	if !g.anyMap {
		return
	}
	for _, st := range g.varMembers(members) {
		g.emitMapWireExtent(st)
		g.emitMapExtent(st)
		g.emitMapPack(st)
	}
}

// emitNodeBytes emits the bytes ONE NODE takes in a packed region: the
// record's own storage rounded to the arena's alignment, plus the extent its
// maps take (docs/SPEC-TABLES.md §2.8, §6.3), the sum rounded again so the
// next node starts aligned. A unit with no map emits exactly the term it
// always emitted.
func (g *tableGen) emitNodeBytes(table, expr, ind, onBad string, plain func(term string), extent func(term string)) {
	target := memberOf(g.unit, table)
	if !g.anyMap || target == nil || !g.hasMapExtent(target) {
		// a node with no map below it takes exactly the term it always took,
		// so a map-free unit emits what it emitted before the construct existed
		plain(fmt.Sprintf("TableAlignUp64( (int64_t) sizeof( %s ) )", table))
		return
	}
	g.pf("%sint64_t node_extent = %sMapExtent( ctx, %s );\n", ind, table, expr)
	g.pf("%sif ( node_extent < 0 ) { %s }\n", ind, onBad)
	extent(fmt.Sprintf("TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + node_extent )", table))
}

// emitMapWireExtent emits `<T>WireExtent`: the region bytes one record's maps
// command, read from the wire FRAMING alone at every depth
// (docs/SPEC-TABLES.md §2.8, §6.5). False is the refusal — an N the map's L
// cannot carry — and it is what makes LoadMeasure answer -1.
func (g *tableGen) emitMapWireExtent(st *ir.Struct) {
	g.pf("// %sWireExtent: the extent %s's maps command, from the FRAMING alone.\n", st.Name, st.Name)
	g.pf("// It reads no field value, so a caller can refuse a number it did not\n")
	g.pf("// expect before one byte is allocated (docs/SPEC-TABLES.md §6.5).\n")
	g.pf("inline bool %sWireExtent( const uint8_t * body, int64_t length, int64_t & at, const TableIdTable * ids )\n{\n", st.Name)
	if !g.hasMapExtent(st) {
		g.pf("    (void) body; (void) length; (void) at; (void) ids; // no map below this record\n")
		g.pf("    return true;\n}\n\n")
		return
	}
	g.pf("    TableReport scratch; // the scan's framing damage is the LOAD's to report\n")
	g.pf("    TableReader r( body, length, &scratch, ids );\n")
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t field_ref = 0;\n")
	g.pf("        if ( !r.getleb( field_ref ) ) { return true; }\n")
	g.pf("        if ( field_ref == 0 ) { return true; }\n")
	g.pf("        if ( ids == NULL || field_ref > (uint64_t) ids->count ) { return true; }\n")
	g.pf("        const uint64_t field_id = ids->at( field_ref );\n")
	g.pf("        if ( !r.has( 1 ) ) { return true; }\n")
	g.pf("        uint8_t field_kind = r.get8();\n")
	g.emitMapExtentWireCases(st)
	g.pf("        if ( !r.skip( field_kind ) ) { return true; }\n")
	g.pf("    }\n}\n\n")
}

// emitMapExtentWireCases emits one arm per map field and one per by-value
// nesting that holds a map, in DECLARATION ORDER, so the framing scan advances
// the running offset in the same order the pack and the load carve it.
func (g *tableGen) emitMapExtentWireCases(st *ir.Struct) {
	for _, f := range st.Fields {
		if f.IsMap() {
			entry := mapEntryOf(f)
			inner := "NULL"
			if g.hasMapExtent(entry) {
				inner = "&" + entry.Name + "WireExtent"
			}
			g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s\n        {\n", ir.TableFieldWireId(f), tkArray, f.Name)
			g.pf("            uint64_t map_len = 0;\n")
			g.pf("            if ( !r.getleb( map_len ) || !r.room( map_len ) ) { return true; }\n")
			g.pf("            const uint8_t * map_body = r.buffer + r.offset;\n")
			g.pf("            r.offset += (int64_t) map_len;\n")
			g.pf("            if ( !TableMapWireExtent( map_body, (int64_t) map_len, at, (int64_t) sizeof( %s ), (int64_t) alignof( %s ), %s, ids ) ) { return false; }\n",
				entry.Name, entry.Name, inner)
			g.pf("            continue;\n        }\n")
			continue
		}
		switch g.edgeOf(f) {
		case edgeNested:
			ref, _ := f.Type.Ref.(*ir.Struct)
			if ref == nil || !g.hasMapExtent(ref) {
				continue
			}
			// a nested table's maps are part of THIS node's extent, so its own
			// scan runs over the nested body at the running offset
			kind, walk := tkTable, ""
			switch {
			case f.KeyEnum != "":
				kind, walk = tkKeyed, "TableWireExtentKeyed"
			case f.Array != ir.ArrayNone:
				kind, walk = tkArray, "TableWireExtentElements"
			}
			g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: a nesting that holds a map\n        {\n", ir.TableFieldWireId(f), kind, f.Name)
			g.pf("            uint64_t nested_len = 0;\n")
			g.pf("            if ( !r.getleb( nested_len ) || !r.room( nested_len ) ) { return true; }\n")
			g.pf("            const uint8_t * nested_body = r.buffer + r.offset;\n")
			g.pf("            r.offset += (int64_t) nested_len;\n")
			if walk == "" {
				g.pf("            if ( !%sWireExtent( nested_body, (int64_t) nested_len, at, ids ) ) { return false; }\n", f.Type.Name)
			} else {
				g.pf("            if ( !%s( nested_body, (int64_t) nested_len, at, &%sWireExtent, ids ) ) { return false; }\n", walk, f.Type.Name)
			}
			g.pf("            continue;\n        }\n")
		case edgeArm:
			un := f.Type.Ref.(*ir.Union)
			any := false
			for _, v := range un.Variants {
				if ref := memberOf(g.unit, v.Type); ref != nil && g.hasMapExtent(ref) {
					any = true
				}
			}
			if !any {
				continue
			}
			g.pf("        if ( field_id == 0x%016xull && field_kind == %d ) // %s: a union arm that holds a map\n        {\n", ir.TableFieldWireId(f), tkUnion, f.Name)
			g.pf("            uint64_t arm_ref = 0;\n")
			g.pf("            if ( !r.getleb( arm_ref ) ) { return true; }\n")
			g.pf("            if ( arm_ref == 0 ) { continue; } // None: the reference is the whole payload\n")
			g.pf("            if ( arm_ref > (uint64_t) ids->count ) { return true; }\n")
			g.pf("            const uint64_t arm_id = ids->at( arm_ref );\n")
			g.pf("            if ( !r.has( 1 ) ) { return true; }\n")
			g.pf("            r.offset += 1; // the arm's kind byte\n")
			g.pf("            uint64_t arm_len = 0;\n")
			g.pf("            if ( !r.getleb( arm_len ) || !r.room( arm_len ) ) { return true; }\n")
			g.pf("            const uint8_t * arm_body = r.buffer + r.offset;\n")
			g.pf("            r.offset += (int64_t) arm_len;\n")
			g.pf("            switch ( arm_id )\n            {\n")
			for _, v := range un.Variants {
				ref := memberOf(g.unit, v.Type)
				if ref == nil || !g.hasMapExtent(ref) {
					continue
				}
				g.pf("                case 0x%016xull: if ( !%sWireExtent( arm_body, (int64_t) arm_len, at, ids ) ) { return false; } break; // %s\n",
					ir.TableWireId(v.Name), v.Type, v.Name)
			}
			g.pf("                default: break; // an arm this reader cannot name reads None\n")
			g.pf("            }\n")
			g.pf("            continue;\n        }\n")
		}
	}
}

// emitRootDataBytes emits a load's DATA term for the root itself: its record,
// plus the extent its own maps take, read from the wire framing (§2.8, §6.5).
func (g *tableGen) emitRootDataBytes(st *ir.Struct, ind, onBad string) {
	if !g.anyMap {
		g.pf("%sint64_t data = TableAlignUp64( (int64_t) sizeof( %s ) );\n", ind, st.Name)
		return
	}
	g.pf("%sint64_t root_extent = 0;\n", ind)
	g.pf("%sif ( !%sWireExtent( wire, wire_bytes, root_extent, &ids_table ) ) { %s }\n", ind, st.Name, onBad)
	g.pf("%sint64_t data = TableAlignUp64( TableAlignUp64( (int64_t) sizeof( %s ) ) + root_extent );\n", ind, st.Name)
}

// ---- the builder's five, and the optional index (docs/SPEC-TABLES.md §2.8) ----
//
// FREE FUNCTIONS taking the worker or the arena, as Emplace and the arena At
// overloads are (§6.2), because an arena reference resolves THROUGH the arena
// and the builder's surface has to say which one. Insert takes the WORKER
// because it may allocate a segment; Find, Erase and Each take the arena
// because they never do.
func (g *tableGen) emitMapBuilderSurface(owner *ir.Struct, f *ir.Field) {
	entry := mapEntryOf(f)
	key := g.mapKeyCallType(f)
	n := entry.Name
	hold := fmt.Sprintf("TableMap<%s> & map", n)
	g.pf("// ---- %s.%s: the builder's five and the side index (§2.8) ----\n\n", owner.Name, f.Name)

	// INSERT
	g.pf("// INSERT: the key is copied, the value is handed back at its defaults to\n")
	g.pf("// fill. A DUPLICATE key REPLACES — the value is reset and the same entry\n")
	g.pf("// handed back, key and address unchanged — so a caller that wants to know\n")
	g.pf("// writes Find first. NULL is NOT INSERTED: a key longer than the bound,\n")
	g.pf("// because a truncated key would be a merged entry, and an arena that\n")
	g.pf("// cannot carve another segment, alike.\n")
	g.pf("inline %s %s( TableWorker & worker, %s, %s key )\n{\n", g.mapInsertReturn(f), mapVerb(owner.Name, f, "Insert"), hold, key)
	if mapKeyIsString(f) {
		g.pf("    const int32_t length = TableKeyLength( key, k%sKeyBound );\n", n)
		g.pf("    if ( key == NULL || length > k%sKeyBound ) { return NULL; } // KEYS NEVER CLAMP\n", n)
	}
	g.pf("    if ( worker.arena == NULL ) { return NULL; }\n")
	g.pf("    %s * found = TableMapScan( *worker.arena, map, key ); // one LINEAR SCAN of the live entries\n", n)
	g.pf("    if ( found != NULL )\n    {\n")
	g.pf("        TableResetMapValue( *found ); // a duplicate REPLACES: the value goes back to its defaults\n")
	g.pf("        return TableEntryValue( found );\n    }\n")
	g.pf("    TableMapHead * head = TableMapReach( worker, map );\n")
	g.pf("    if ( head == NULL ) { return NULL; }\n")
	g.pf("    %s * entry = TableMapAppend( worker, head, map ); // APPENDS; nothing ever moves (§6.4)\n", n)
	g.pf("    if ( entry == NULL ) { return NULL; }\n")
	g.pf("    TableReset( *entry );\n")
	if mapKeyIsString(f) {
		g.pf("    TableEntrySetKey( *entry, key, length );\n")
	} else {
		g.pf("    TableEntrySetKey( *entry, key );\n")
	}
	g.pf("    return TableEntryValue( entry );\n}\n\n")

	// FIND on the builder
	g.pf("// FIND on the builder: the same linear scan, O( n ) key compares over the\n")
	g.pf("// segments in insertion order. NULL when absent. The builder builds NO\n")
	g.pf("// INDEX, and that is a rule — the sort happens once, at Lock, Save or\n")
	g.pf("// Cook, and every lookup that matters runs over the sorted region.\n")
	g.pf("inline %s %s( TableArena & arena, %s, %s key )\n{\n", g.mapInsertReturn(f), mapVerb(owner.Name, f, "Find"), hold, key)
	g.pf("    %s * found = TableMapScan( arena, map, key );\n", n)
	g.pf("    return found != NULL ? TableEntryValue( found ) : NULL;\n}\n\n")

	// ERASE
	g.pf("// ERASE: marks the entry DEAD, one bit in the segment's slot and not in the\n")
	g.pf("// entry table. False when absent. Its storage is held until the builder\n")
	g.pf("// resets and never reused mid-build, because reusing a slot would make \"an\n")
	g.pf("// entry's address is stable\" false for exactly one case.\n")
	g.pf("inline bool %s( TableArena & arena, %s, %s key )\n{\n", mapVerb(owner.Name, f, "Erase"), hold, key)
	g.pf("    return TableMapErase( arena, map, key );\n}\n\n")

	// EACH
	g.pf("// EACH on the builder: INSERTION order, live entries only.\n")
	g.pf("inline TableMapEach<%s> %s( const TableArena & arena, const TableMap<%s> & map )\n{\n", n, mapVerb(owner.Name, f, "Each"), n)
	g.pf("    return TableMapEachOf( arena, map );\n}\n\n")

	// the optional index
	g.pf("// ---- the OPTIONAL INDEX: caller-owned, built at load, never stored ----\n")
	g.pf("//\n")
	g.pf("// Open addressing with linear probing over the sorted array, for a map large\n")
	g.pf("// enough that log n compares over a cold array cost more than one hash and a\n")
	g.pf("// probe. ITS HASH AND ITS LOAD FACTOR ARE NOT A CROSS-PORT CONTRACT: the\n")
	g.pf("// index is never stored, so no golden, no cook-check rule and no\n")
	g.pf("// build-version line ever names either. What a port is held to is the\n")
	g.pf("// CONTRACT of the lookup — the same value the sorted array's Find returns\n")
	g.pf("// for the same key, and no allocation past the storage the caller handed in.\n")
	g.pf("inline int64_t %s( const TableMap<%s> & map )\n{\n", mapVerb(owner.Name, f, "IndexMeasure"), n)
	g.pf("    return (int64_t) TableMapIndexSlots( map.count ) * (int64_t) sizeof( int32_t );\n}\n\n")
	g.pf("inline TableMapIndex %s( const TableMap<%s> & map, void * storage, int64_t bytes )\n{\n", mapVerb(owner.Name, f, "Index"), n)
	g.pf("    TableMapIndex index;\n")
	g.pf("    const int32_t slots = TableMapIndexSlots( map.count );\n")
	g.pf("    if ( storage == NULL || bytes < (int64_t) slots * (int64_t) sizeof( int32_t ) ) { return index; }\n")
	g.pf("    index.slots = (int32_t *) storage;\n")
	g.pf("    index.capacity = slots;\n")
	g.pf("    for ( int32_t i = 0; i < slots; i++ ) { index.slots[i] = 0; }\n")
	g.pf("    const %s * entries = map.Entries();\n", n)
	g.pf("    for ( int32_t i = 0; i < map.count; i++ ) // ONE PASS over the sorted array\n    {\n")
	g.pf("        int32_t at = (int32_t) ( %s & (uint64_t) ( slots - 1 ) );\n", g.mapIndexHash(f, "entries[i]"))
	g.pf("        while ( index.slots[at] != 0 ) { at = ( at + 1 ) & ( slots - 1 ); }\n")
	g.pf("        index.slots[at] = i + 1; // slots are ENTRY INDICES; 0 is an empty slot\n")
	g.pf("    }\n")
	g.pf("    index.good = true;\n")
	g.pf("    return index;\n}\n\n")
	g.pf("inline %s %s( const TableMapIndex & index, const TableMap<%s> & map, %s key )\n{\n",
		g.mapFindReturn(f), mapVerb(owner.Name, f, "IndexFind"), n, key)
	g.pf("    if ( !index.good ) { return map.Find( key ); } // an index that did not build is not a wrong answer\n")
	g.pf("    const %s * entries = map.Entries();\n", n)
	g.pf("    int32_t at = (int32_t) ( %s & (uint64_t) ( index.capacity - 1 ) );\n", g.mapIndexHashOfKey(f, "key"))
	g.pf("    for ( int32_t probe = 0; probe < index.capacity; probe++ )\n    {\n")
	g.pf("        const int32_t slot = index.slots[at];\n")
	g.pf("        if ( slot == 0 ) { return NULL; }\n")
	g.pf("        if ( TableEntryOrder( entries[slot - 1], key ) == 0 ) { return TableEntryFound( entries + slot - 1 ); }\n")
	g.pf("        at = ( at + 1 ) & ( index.capacity - 1 );\n")
	g.pf("    }\n")
	g.pf("    return NULL;\n}\n\n")
}

// mapInsertReturn is what an Insert and a builder Find hand back: the VALUE at
// its defaults, and on a map[K]*T the pointer SLOT at null, which Emplace
// fills as it fills any pointer slot (docs/SPEC-TABLES.md §2.8).
func (g *tableGen) mapInsertReturn(f *ir.Field) string {
	if mapValueIsPointer(f) {
		return "TableRef *"
	}
	return g.mapValueStorageType(f) + " *"
}

// mapFindReturn is what the CONST form answers: the resolved pointer on a
// map[K]*T, and a pointer to the value otherwise.
func (g *tableGen) mapFindReturn(f *ir.Field) string {
	if mapValueIsPointer(f) {
		return "const " + ir.MapValueField(f).Type.Name + " *"
	}
	return "const " + g.mapValueStorageType(f) + " *"
}

// mapIndexHash hashes one ENTRY's key, and mapIndexHashOfKey a bare one. Both
// are this runtime's own and no port reproduces either.
func (g *tableGen) mapIndexHash(f *ir.Field, entry string) string {
	if mapKeyIsString(f) {
		return fmt.Sprintf("TableMapHash( (const void *) %s.key, %s.key_length )", entry, entry)
	}
	return fmt.Sprintf("TableMapHash( (uint64_t) %s.key )", entry)
}

func (g *tableGen) mapIndexHashOfKey(f *ir.Field, key string) string {
	if mapKeyIsString(f) {
		return fmt.Sprintf("TableMapHash( (const void *) %s, TableKeyLength( %s, k%sKeyBound ) )", key, key, mapEntryOf(f).Name)
	}
	return fmt.Sprintf("TableMapHash( (uint64_t) %s )", key)
}

// emitMapBuilderSurfaces emits every map field's builder five and side index,
// after the arena runtime and the entry surface they are spelled in terms of.
func (g *tableGen) emitMapBuilderSurfaces(members []*ir.Struct) {
	if !g.anyMap {
		return
	}
	for _, st := range members {
		for _, f := range mapFieldsOf(st) {
			g.emitMapBuilderSurface(st, f)
		}
	}
}

// ---- the COOK's write side at a map (docs/SPEC-TABLES.md §2.8, §7.6) ----
//
// A cook is a region written verbatim, so a cooked map is its SORTED entry
// array where the cook put it: the node's extent, laid after the record's own
// storage by the same PRE-ORDER rule the pack lays it by. Find is then a
// binary search over the mapped bytes, in place, with nothing to parse.

// cookMapsSignature is one record's extent writer.
func (g *tableGen) cookMapsSignature(st *ir.Struct) string {
	return fmt.Sprintf("template <typename Ctx> inline bool %sCookMaps( const Ctx & ctx, const TableCookRegion & region, uint8_t * extent, int64_t & at, uint8_t * record, const %s & value, TableByteOrder order )", st.Name, st.Name)
}

// emitCookMaps emits one record's extent writer: every map reachable by value,
// PRE-ORDER, each entry through its own cook body.
func (g *tableGen) emitCookMaps(st *ir.Struct) {
	g.pf("// %sCookMaps: %s's map arrays into the node's extent, PRE-ORDER, the entries\n", st.Name, st.Name)
	g.pf("// in ASCENDING key order, each through its own cook body (§2.8, §7.6).\n")
	g.pf("%s\n{\n", g.cookMapsSignature(st))
	if !g.hasMapExtent(st) {
		g.pf("    (void) ctx; (void) region; (void) extent; (void) at; (void) record; (void) value; (void) order;\n")
		g.pf("    return true; // no map below this record\n}\n\n")
		return
	}
	g.pf("    (void) region; // a map's entries carry their own references through their own bodies\n")
	ml := ir.RecordLayout(g.unit, st)
	offsetOf := func(name string) int64 {
		for i := range ml.Fields {
			if ml.Fields[i].Field.Name == name {
				return ml.Fields[i].Offset
			}
		}
		return 0
	}
	for _, f := range st.Fields {
		if f.IsMap() {
			entry := mapEntryOf(f)
			el := ir.RecordLayout(g.unit, entry)
			slot := offsetOf(f.Name)
			g.pf("    { // %s\n", f.Name)
			g.pf("        TableMapCursor<%s> cursor = TableMapOrder( ctx, value.%s );\n", entry.Name, f.Name)
			g.pf("        if ( !cursor.ok ) { return false; }\n")
			g.pf("        at = ( at + %d ) & ~(int64_t) %d; // at alignof( %s )\n", el.Align-1, el.Align-1, entry.Name)
			g.pf("        uint8_t * array = extent + at;\n")
			g.pf("        at += (int64_t) cursor.count * %d; // the whole array FIRST\n", el.Size)
			g.pf("        // the SIXTEEN BYTES of the slot: the self-relative delta, then the count\n")
			g.pf("        table_cook_put( record + %d, cursor.count > 0 ? (uint64_t) (int64_t) ( array - ( record + %d ) ) : 0, 8, order );\n", slot, slot)
			g.pf("        table_cook_put( record + %d, (uint64_t) (uint32_t) cursor.count, 4, order );\n", slot+8)
			g.pf("        for ( int32_t i = 0; i < cursor.count; i++ )\n        {\n")
			g.pf("            %s\n", g.cookBodyCall(entry, fmt.Sprintf("array + i * %d", el.Size), "*cursor[i]"))
			g.pf("        }\n")
			if g.hasMapExtent(entry) {
				g.pf("        for ( int32_t i = 0; i < cursor.count; i++ ) // then, entry by entry in key order\n        {\n")
				g.pf("            if ( !%sCookMaps( ctx, region, extent, at, array + i * %d, *cursor[i], order ) ) { TableMapRelease( cursor ); return false; }\n", entry.Name, el.Size)
				g.pf("        }\n")
			}
			g.pf("        TableMapRelease( cursor );\n    }\n")
			continue
		}
		if g.edgeOf(f) != edgeNested {
			continue
		}
		ref, _ := f.Type.Ref.(*ir.Struct)
		if ref == nil || !g.hasMapExtent(ref) {
			continue
		}
		nested := offsetOf(f.Name)
		if f.Array == ir.ArrayNone {
			g.pf("    if ( !%sCookMaps( ctx, region, extent, at, record + %d, value.%s, order ) ) { return false; } // %s\n", ref.Name, nested, f.Name, f.Name)
			continue
		}
		stride := cookElementBytes(g.unit, f)
		base := "value." + f.Name
		bound := fmt.Sprintf("%d", f.ArrayBound)
		if f.KeyEnum != "" && st.IsTable {
			base += ".slots"
		}
		if f.Array == ir.ArrayCounted {
			// THE LIVE COUNT, as the extent walk counts it: a slot past the
			// count is storage the walk does not reach, and a non-empty map in
			// one was already refused there (§7.6)
			bound = fmt.Sprintf("( value.%s_count < %d ? value.%s_count : %d )", f.Name, f.ArrayBound, f.Name, f.ArrayBound)
		}
		g.pf("    for ( int32_t i = 0; i < %s; i++ ) // %s\n    {\n", bound, f.Name)
		g.pf("        if ( !%sCookMaps( ctx, region, extent, at, record + %d + i * %d, %s[i], order ) ) { return false; }\n", ref.Name, nested, stride, base)
		g.pf("    }\n")
	}
	g.pf("    return true;\n}\n\n")
}

// emitCookNode emits `<T>CookNode`: one NODE's record and then its own extent.
// A nested record's writer is the body alone, because a nesting's maps are
// part of the HOLDER's extent and this walk already reached them.
func (g *tableGen) emitCookNode(st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	record := cookAlignUp(ml.Size, ir.RegionAlignFloor)
	g.pf("// %sCookNode: one node — the record, then the extent its maps take (§2.8).\n", st.Name)
	g.pf("template <typename Ctx> inline bool %sCookNode( const Ctx & ctx, const TableCookRegion & region, uint8_t * at, const %s & value, TableByteOrder order )\n{\n", st.Name, st.Name)
	if g.isVar(st.Name) {
		g.pf("    if ( !%sCookBody( ctx, region, at, value, order ) ) { return false; }\n", st.Name)
	} else {
		g.pf("    %sCookBody( at, value, order );\n", st.Name)
	}
	g.pf("    int64_t extent_at = 0;\n")
	g.pf("    return %sCookMaps( ctx, region, at + %d, extent_at, at, value, order );\n}\n\n", st.Name, record)
}

// emitCookMapSurface emits the extent writer and the node writer for every
// closure member of a map-bearing unit.
func (g *tableGen) emitCookMapSurface(members []*ir.Struct) {
	if !g.anyMap {
		return
	}
	var bodies []*ir.Struct
	for _, st := range members {
		if ir.RecordLayout(g.unit, st) != nil {
			bodies = append(bodies, st)
		}
	}
	for _, st := range bodies {
		g.pf("%s;\n", g.cookMapsSignature(st))
	}
	g.pf("\n")
	for _, st := range bodies {
		g.emitCookMaps(st)
	}
	for _, st := range bodies {
		g.emitCookNode(st)
	}
}

// cookNodeBytes is one node's whole span in a cooked region: its record at the
// region's alignment floor, plus the extent its maps take (docs/SPEC-TABLES.md
// §2.8, §7.2).
func (g *tableGen) emitCookNodeBytes(st *ir.Struct, ind, expr, onBad string) {
	ml := ir.RecordLayout(g.unit, st)
	if !g.anyMap || !g.hasMapExtent(st) {
		g.pf("%ssize = %d; node_align = %d;\n", ind, ml.Size, ml.Align)
		return
	}
	g.pf("%s{\n", ind)
	g.pf("%s    const int64_t extent = %sMapExtent( ctx, %s );\n", ind, st.Name, expr)
	g.pf("%s    if ( extent < 0 ) { %s }\n", ind, onBad)
	g.pf("%s    size = %d + extent; node_align = %d;\n", ind, cookAlignUp(ml.Size, ir.RegionAlignFloor), ml.Align)
	g.pf("%s}\n", ind)
}

// onlyMapFields reports a record whose every field is a map — a cook body that
// writes the empty slots and reads nothing off the value, because the extent
// writer fills them.
func onlyMapFields(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if !f.IsMap() {
			return false
		}
	}
	return len(st.Fields) > 0
}

// mapColumn is the four descriptor columns a MAP field carries
// (docs/SPEC-TABLES.md §2.8, §16): the generated entry's descriptor, and the
// three thunks the ONE text walk cannot spell for itself. Empty in a unit that
// declares no map, so a map-free unit's descriptors are what they always were.
func (g *tableGen) mapColumn(f *ir.Field) string {
	if !g.anyMap {
		return ""
	}
	if !f.IsMap() {
		return "NULL, NULL, NULL, NULL, "
	}
	entry := mapEntryOf(f)
	n := entry.Name
	hold := fmt.Sprintf("TableMap<%s>", n)
	count := fmt.Sprintf("[]( const void * slot ) -> int32_t { return ( (const %s *) slot )->count; }", hold)
	at := fmt.Sprintf("[]( const void * slot, int32_t index ) -> const void * { return (const void *) ( ( (const %s *) slot )->Entries() + index ); }", hold)
	var insert string
	if mapKeyIsString(f) {
		insert = fmt.Sprintf("[]( TableWorker & worker, void * slot, const char * key, int32_t key_length, int64_t ) -> void * "+
			"{ if ( key == NULL || key_length > k%sKeyBound ) { return NULL; } "+ // KEYS NEVER CLAMP
			"%s * placed = TableMapPlace( worker, *(%s *) slot, key ); "+
			"if ( placed != NULL ) { TableEntrySetKey( *placed, key, key_length ); } return (void *) placed; }", n, n, hold)
	} else {
		typ, _ := g.cppFieldType(ir.MapKeyField(f).Type)
		insert = fmt.Sprintf("[]( TableWorker & worker, void * slot, const char *, int32_t, int64_t key_value ) -> void * "+
			"{ %s * placed = TableMapPlace( worker, *(%s *) slot, (%s) key_value ); "+
			"if ( placed != NULL ) { TableEntrySetKey( *placed, (%s) key_value ); } return (void *) placed; }", n, hold, typ, typ)
	}
	return fmt.Sprintf("&%sTableInfo, %s, %s, %s, ", n, count, at, insert)
}

// nodeStorageBody, nodeStorageArg and nodeStorageReader are the ONE extra
// parameter a node's storage takes where a map rides in an extent
// (docs/SPEC-TABLES.md §2.8): the record's body, from which the framing scan
// sums the entry arrays. A root that can name no such record does not take it,
// so a map-free unit's dispatch is the one it always emitted.
func (g *tableGen) nodeStorageBody(anyExtent bool) string {
	if anyExtent {
		return "const uint8_t * body, "
	}
	return ""
}

func (g *tableGen) nodeStorageArg(root *ir.Struct) string {
	if g.rootHasExtent(root) {
		return "body, "
	}
	return ""
}

func (g *tableGen) nodeStorageReader(root *ir.Struct) string {
	if g.rootHasExtent(root) {
		return "r.buffer, "
	}
	return ""
}

// rootHasExtent reports whether any record one root's numbering can name holds
// a map by value — which is what decides both halves of the signature above.
func (g *tableGen) rootHasExtent(root *ir.Struct) bool {
	if !g.anyMap {
		return false
	}
	return slices.ContainsFunc(g.pointerReachable(root), g.hasMapExtent)
}

// emitUnreachedMapRefusal refuses an UNREACHED NON-EMPTY MAP SLOT, the same
// refusal §7.6 gives a pointer in that position (docs/SPEC-TABLES.md §2.8): a
// COUNTED array's slots past its live count are storage the walk does not
// reach, so a non-empty map in one names entries the region will not hold, and
// the write answers false with nothing partial written.
//
// The test is the extent itself: an empty map takes no bytes and advances the
// running offset by none, so a record whose extent measures ZERO is a record
// whose every by-value map is empty. A measure that refuses answers non-zero
// here too, and refusing on it is the same answer one level up.
func (g *tableGen) emitUnreachedMapRefusal(f *ir.Field, ref *ir.Struct, subject string) {
	if f.Array != ir.ArrayCounted {
		return // every other array shape is reached whole
	}
	g.pf("    for ( int32_t i = %s.%s_count; i < %d; i++ ) // %s: the slots the walk does not reach (§7.6)\n    {\n",
		subject, f.Name, f.ArrayBound, f.Name)
	g.pf("        if ( !TableMapUnreachedEmpty( %sMapExtent( ctx, %s.%s[i] ) ) ) { return false; }\n", ref.Name, subject, f.Name)
	g.pf("    }\n")
}
