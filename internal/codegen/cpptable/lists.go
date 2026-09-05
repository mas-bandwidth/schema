// Unbounded arrays in the C++ reference (docs/SPEC-TABLES.md §2.9): the
// runtime a `[]T` field's storage, its builder and its const surface are
// spelled in, and the per-field codecs the generated code hangs off it.
//
// An unbounded array is §2.8's map with the KEY and the SORT taken out: a
// counted array whose count the data decides, its elements by-value records
// inside the holder's node extent. So nothing here is a new wire construct,
// and most of what a map needed is not needed: no entry table, no order, no
// key compare, no ascending check and no duplicate rule. What the runtime
// adds is the reference-and-count slot, a builder that appends into segments
// that never move, and a cursor the four writing walks read in INDEX order
// without allocating.
package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// unitHasList reports whether any closure member declares a `[]T`. It gates
// the list runtime: not one symbol of it appears in a list-free unit's
// generated header (docs/SPEC-TABLES.md §2.2, §2.9).
func unitHasList(u *ir.Unit, closure map[string]bool) bool {
	for name := range closure {
		st := memberOf(u, name)
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.IsList() {
				return true
			}
		}
	}
	return false
}

// listFieldsOf lists one member's unbounded arrays in declaration order.
func listFieldsOf(st *ir.Struct) []*ir.Field {
	var out []*ir.Field
	for _, f := range st.Fields {
		if f.IsList() {
			out = append(out, f)
		}
	}
	return out
}

// listVerb spells one of a list field's claimed surface names on its holder:
// `<Table><Field>` followed by the verb (docs/SPEC-TABLES.md §2.9, §11).
func listVerb(owner string, f *ir.Field, verb string) string {
	return owner + ir.GoExportName(f.Name) + verb
}

// listElementIsPointer reports a `[]*T`: the elements are pointer SLOTS, so
// the builder's Add hands back the slot at null and the const form answers the
// resolved `const T *` (docs/SPEC-TABLES.md §2.9).
func listElementIsPointer(f *ir.Field) bool { return f.Type.Pointer }

// listTypeArg is the type argument the storage names: the element type for a
// `[]T`, and `T *` for a `[]*T`, which selects the pointer specialization.
func (g *tableGen) listTypeArg(f *ir.Field) string {
	if listElementIsPointer(f) {
		return f.Type.Name + " *"
	}
	typ, _ := g.cppFieldType(f.Type)
	return typ
}

// listStorageType is the C++ spelling of a list field's storage.
func (g *tableGen) listStorageType(f *ir.Field) string {
	return fmt.Sprintf("TableList<%s>", g.listTypeArg(f))
}

// listElementType is the C++ type ONE ELEMENT is stored as: a TableRef for a
// `[]*T`, the element's own type otherwise.
func (g *tableGen) listElementType(f *ir.Field) string {
	if listElementIsPointer(f) {
		return "TableRef"
	}
	typ, _ := g.cppFieldType(f.Type)
	return typ
}

// listElementStruct is the element's table when the element is one, else nil.
func listElementStruct(f *ir.Field) *ir.Struct {
	if listElementIsPointer(f) || f.Type.Kind != ir.TNamed {
		return nil
	}
	ref, _ := f.Type.Ref.(*ir.Struct)
	return ref
}

// listElementWireKind is the element kind the list's kind 14 body carries:
// kind 17 for a pointer element, the element's own kind otherwise (§2.9, §3).
func listElementWireKind(f *ir.Field) int {
	if listElementIsPointer(f) {
		return tkNodeIndex
	}
	return ir.TableWireElemKind(f)
}

// listElementFloor is the smallest wire footprint ONE element commands
// (docs/SPEC-TABLES.md §4.2, §6.5): a scalar its own width, a reference-shaped
// element (a pointer's node index, an enum's variant reference, a union's arm
// reference) one byte, a table element its own `L` and its terminator. It is
// what bounds the N a list's `L` can carry, and therefore what a LoadMeasure
// may be asked for.
func listElementFloor(f *ir.Field) int {
	if listElementIsPointer(f) {
		return 1
	}
	switch kind := ir.TableWireElemKind(f); kind {
	case tkTable:
		return 2
	case tkEnum, tkUnion:
		return 1
	default:
		return tableKindWidth(kind)
	}
}

// ---- the runtime (docs/SPEC-TABLES.md §2.9) ----

// tableListRuntime is the list half of the variable-length runtime: the
// storage type and its const surface, the builder's head and segments, the
// index-order cursor the four writing walks read, and the load side's fill.
// It is emitted only into a unit that declares an unbounded array.
func tableListRuntime(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_LIST"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// ---- an UNBOUNDED ARRAY: a counted array whose count the data decides (§2.9) ----
//
// On the wire, in a region and in a cook a list is the kind 14 body a [..N]T
// writes, its elements by-value records inside the holder's node extent. What
// this adds is the slot, a builder that appends into segments that never
// move, and a const surface that indexes and iterates in place. There is no
// sort, no key and no lookup: the order is INSERTION order, and it is
// identity the way position is identity in a fixed array.

// elements carved from ONE call to the allocator pair. A new segment is
// appended when the current one fills, and nothing ever moves (§6.4)
static const int32_t kTableListSegmentElements = 32;

// THE ELEMENT STORAGE: T itself, and a TableRef slot for a []*T, whose
// elements are references exactly as a pointer field's slot is (§2.1)
template <typename T> struct TableListStorage { typedef T Element; };
template <typename T> struct TableListStorage<T *> { typedef TableRef Element; };

// WHAT THE CONST FORM ANSWERS: the element by reference, and on a []*T the
// RESOLVED pointer, one add on the self-relative delta, NULL for a null slot,
// exactly as <T>At answers it (§6.2, §6.3)
template <typename T> struct TableListConst
{
    typedef const T & Result;
    static Result At( const T * element ) { return *element; }
};
template <typename T> struct TableListConst<T *>
{
    typedef const T * Result;
    static Result At( const TableRef * element )
    {
        return element->value != 0 ? (const T *) ( (const uint8_t *) element + element->value ) : NULL;
    }
};

// ---- the storage: SIXTEEN BYTES in the holder's record (§2.9, §7.2) ----
//
// An int64 self-relative reference to the element array and an int32 count,
// then padding to eight. The reference is a TableRef like a pointer's: in the
// arena it names the builder's HEAD, in a region it is the delta from the slot
// to the first element, and 0 is the empty list in both. It is the map's slot
// exactly, because it is the same two facts.
template <typename T> struct TableList
{
    typedef typename TableListStorage<T>::Element Element;

    TableRef elements;
    int32_t count = 0;   // the LIVE count, in both forms
    int32_t padding = 0; // named, so the record has no unwritten byte in it

    // ---- the CONST form: a locked region, a loaded one, an opened cook ----
    //
    // One surface over one encoding (§6.3). A region reference resolves from
    // the slot's own address, so every one of these is a member and needs no
    // base and no context.
    const Element * Elements() const
    {
        return elements.value != 0 ? (const Element *) ( (const uint8_t *) &elements + elements.value ) : NULL;
    }
    int32_t size() const { return count; }

    // INDEXING IS BOUNDS-CHECKED IN EVERY BUILD (§2.4, §2.9): the extent is a
    // number that CAME FROM A FILE, so an index past it is not a mistake a
    // release build gets to make cheaply. There is no undefined-behavior path
    // here in any configuration. The assert carries the message where a
    // debugger can read it and NDEBUG removes that. The fatal is what stands
    // after it. Both go through the hooks: define schema_assert and
    // schema_fatal and this refusal lands in your own handler.
    void RefuseIndex( int32_t index ) const
    {
        if ( (uint32_t) index >= (uint32_t) count )
        {
            schema_assert( false && "an unbounded array is indexed inside its count, which came from a file" );
            schema_fatal();
        }
    }
    typename TableListConst<T>::Result operator[]( int32_t index ) const
    {
        RefuseIndex( index );
        return TableListConst<T>::At( Elements() + index );
    }

    // ---- iteration: INDEX order, the element and no key ----
    //
    // It carries no iterator_traits, for the reason TableKeyed's does not
    // (§13.9).
    struct ConstIterator
    {
        const Element * at;
        typename TableListConst<T>::Result operator*() const { return TableListConst<T>::At( at ); }
        ConstIterator & operator++() { at++; return *this; }
        bool operator==( const ConstIterator & other ) const { return at == other.at; }
        bool operator!=( const ConstIterator & other ) const { return at != other.at; }
    };

    ConstIterator begin() const { return ConstIterator{ Elements() }; }
    ConstIterator end() const { return ConstIterator{ Elements() + count }; }
};

// ---- the BUILDER's side: a head, and segments that never move (§2.9, §6.4) ----
//
// The head is a small node in the arena holding the segment chain, the live
// count and the dead count, allocated when the first element is added. Each
// segment is a fixed number of elements carved from one call to the allocator
// pair. An element's address is stable for the arena's life, so a T * handed
// back by Add stays valid while other elements arrive.
struct TableListHead
{
    TableRef first; // the arena offset of the first segment
    TableRef last;  // and of the one an Add appends into
    int32_t live;
    int32_t dead;
};

template <typename Element> struct TableListSegment
{
    TableRef next;
    int32_t used;                                          // elements carved from this segment
    int32_t padding;
    uint32_t dead[ ( kTableListSegmentElements + 31 ) / 32 ]; // Erase marks one bit, never the element
    Element elements[ kTableListSegmentElements ];
};

inline bool TableListSegmentDead( const uint32_t * dead, int32_t index )
{
    return ( dead[ index / 32 ] & ( 1u << ( index % 32 ) ) ) != 0;
}

// the head, allocated when the first element is added
template <typename T>
inline TableListHead * TableListReach( TableWorker & worker, TableList<T> & list )
{
    if ( worker.arena == NULL || worker.arena->locked ) { return NULL; }
    if ( list.elements.value != 0 ) { return (TableListHead *) TableArenaAt( *worker.arena, (uint32_t) list.elements.value ); }
    uint32_t at = 0;
    TableListHead * head = (TableListHead *) worker.AllocRaw( (int64_t) sizeof( TableListHead ), (int64_t) alignof( TableListHead ), at );
    if ( head == NULL ) { return NULL; }
    head->first.value = 0;
    head->last.value = 0;
    head->live = 0;
    head->dead = 0;
    list.elements.value = (int64_t) at;
    return head;
}

// one element's storage, appended: the current segment when it has room, a
// new one carved from one call to the pair when it does not. NULL means NOT
// ADDED: an arena that cannot carve another segment, or a count at the int32
// cap (§2.2, §2.9).
template <typename T>
inline typename TableList<T>::Element * TableListAppend( TableWorker & worker, TableListHead * head, TableList<T> & list )
{
    typedef typename TableList<T>::Element Element;
    if ( list.count >= INT32_MAX ) { return NULL; } // the int32 storage cap
    TableListSegment<Element> * segment = NULL;
    if ( head->last.value != 0 )
    {
        segment = (TableListSegment<Element> *) TableArenaAt( *worker.arena, (uint32_t) head->last.value );
        if ( segment->used >= kTableListSegmentElements ) { segment = NULL; }
    }
    if ( segment == NULL )
    {
        uint32_t at = 0;
        segment = (TableListSegment<Element> *) worker.AllocRaw( (int64_t) sizeof( TableListSegment<Element> ), (int64_t) alignof( TableListSegment<Element> ), at );
        if ( segment == NULL ) { return NULL; } // the arena could not carve another segment
        segment->next.value = 0;
        segment->used = 0;
        segment->padding = 0;
        for ( int32_t i = 0; i < (int32_t) ( sizeof( segment->dead ) / sizeof( segment->dead[0] ) ); i++ ) { segment->dead[i] = 0; }
        if ( head->last.value != 0 )
        {
            TableListSegment<Element> * previous = (TableListSegment<Element> *) TableArenaAt( *worker.arena, (uint32_t) head->last.value );
            previous->next.value = (int64_t) at;
        }
        else
        {
            head->first.value = (int64_t) at;
        }
        head->last.value = (int64_t) at;
    }
    Element * element = segment->elements + segment->used;
    segment->used++;
    head->live++;
    list.count++;
    return element;
}

// ADD, whole: the head, the append, and the element at its declared defaults
// (§2.9). The text form's placement is this same call, because a list has no
// key to place under (§16).
template <typename T>
inline typename TableList<T>::Element * TableListPlace( TableWorker & worker, TableList<T> & list )
{
    typedef typename TableList<T>::Element Element;
    TableListHead * head = TableListReach( worker, list );
    if ( head == NULL ) { return NULL; }
    Element * element = TableListAppend( worker, head, list );
    if ( element == NULL ) { return NULL; }
    new ( element ) Element(); // value-init: the declared defaults, and null for a slot
    return element;
}

// ERASE, ADDRESSED BY THE POINTER (§2.9): the element Add handed back is the
// handle, because a list has no key and the address is the one thing the
// builder promises never moves (§6.4). It marks the element DEAD, one bit in
// the segment's slot and not in the element storage, and decrements the live
// count. False when the pointer is not this list's. Its storage is reclaimed
// at RESET and never reused mid-build, the map's rule for the map's reason.
template <typename T>
inline bool TableListErase( TableArena & arena, TableList<T> & list, const typename TableList<T>::Element * element )
{
    typedef typename TableList<T>::Element Element;
    if ( list.elements.value == 0 || element == NULL ) { return false; }
    TableListHead * head = (TableListHead *) TableArenaAt( arena, (uint32_t) list.elements.value );
    TableRef segment_ref = head->first;
    while ( segment_ref.value != 0 )
    {
        TableListSegment<Element> * segment = (TableListSegment<Element> *) TableArenaAt( arena, (uint32_t) segment_ref.value );
        if ( element >= segment->elements && element < segment->elements + segment->used )
        {
            const int32_t i = (int32_t) ( element - segment->elements );
            if ( TableListSegmentDead( segment->dead, i ) ) { return false; } // already erased
            segment->dead[ i / 32 ] |= 1u << ( i % 32 );
            head->live--;
            head->dead++;
            list.count--;
            return true;
        }
        segment_ref = segment->next;
    }
    return false;
}

// ---- iterate on the BUILDER: INDEX order, live elements only (§2.9) ----
template <typename T> struct TableListEach
{
    typedef typename TableList<T>::Element Element;
    const TableArena * arena;
    TableRef first;

    struct Iterator
    {
        const TableArena * arena;
        TableListSegment<Element> * segment;
        int32_t index;

        void Skip()
        {
            for ( ;; )
            {
                if ( segment == NULL ) { return; }
                if ( index >= segment->used )
                {
                    segment = segment->next.value != 0 ? (TableListSegment<Element> *) TableArenaAt( *arena, (uint32_t) segment->next.value ) : NULL;
                    index = 0;
                    continue;
                }
                if ( TableListSegmentDead( segment->dead, index ) ) { index++; continue; }
                return;
            }
        }
        Element * operator*() const { return segment->elements + index; }
        Iterator & operator++() { index++; Skip(); return *this; }
        bool operator==( const Iterator & other ) const { return segment == other.segment && index == other.index; }
        bool operator!=( const Iterator & other ) const { return !( *this == other ); }
    };

    Iterator begin() const
    {
        Iterator it = { arena, first.value != 0 ? (TableListSegment<Element> *) TableArenaAt( *arena, (uint32_t) first.value ) : NULL, 0 };
        it.Skip();
        return it;
    }
    Iterator end() const { Iterator it = { arena, NULL, 0 }; return it; }
};

template <typename T>
inline TableListEach<T> TableListEachOf( const TableArena & arena, const TableList<T> & list )
{
    TableListEach<T> each = { &arena, TableRef() };
    if ( list.elements.value != 0 )
    {
        const TableListHead * head = (const TableListHead *) TableArenaAt( arena, (uint32_t) list.elements.value );
        each.first = head->first;
    }
    return each;
}

// ---- the INDEX-ORDER CURSOR the four writing walks read (§2.9) ----
//
// Measure, Save, Lock and Cook each visit a list's live elements in the order
// they were added, and they allocate nothing to do it: a region's cursor is
// the array in place, and the builder's walks the segment chain. Indexing the
// builder's form is SEQUENTIAL by construction, every walk steps i, i + 1,
// i + 2, so the cursor remembers where the last access landed and moves one
// live slot per step. An access behind the memo restarts from the first
// segment, which no walk here does.
template <typename Element> struct TableListCursor
{
    const Element * elements = NULL; // the region's form: the array in place
    const TableArena * arena = NULL; // the builder's form: the segments
    TableRef first;
    int32_t count = 0;
    bool ok = false;
    // the memo: the segment and slot the last access landed on, and the live
    // index that slot holds
    mutable const TableListSegment<Element> * segment = NULL;
    mutable int32_t within = -1;
    mutable int32_t logical = -1;

    const Element * At( int32_t index ) const
    {
        if ( elements != NULL ) { return elements + index; }
        if ( segment == NULL || index < logical )
        {
            segment = first.value != 0 ? (const TableListSegment<Element> *) TableArenaAt( *arena, (uint32_t) first.value ) : NULL;
            within = -1;
            logical = -1;
        }
        while ( logical < index )
        {
            for ( ;; )
            {
                within++;
                while ( segment != NULL && within >= segment->used )
                {
                    segment = segment->next.value != 0 ? (const TableListSegment<Element> *) TableArenaAt( *arena, (uint32_t) segment->next.value ) : NULL;
                    within = 0;
                }
                if ( segment == NULL ) { return NULL; } // the slot and the head disagree
                if ( !TableListSegmentDead( segment->dead, within ) ) { break; }
            }
            logical++;
        }
        return segment->elements + within;
    }
    const Element & operator[]( int32_t index ) const { return *At( index ); }
};

// the REGION form: the array is the cursor
template <typename T>
inline TableListCursor<typename TableList<T>::Element> TableListElements( const TableRegionCtx &, const TableList<T> & list )
{
    TableListCursor<typename TableList<T>::Element> cursor;
    cursor.elements = list.Elements();
    cursor.count = list.count;
    cursor.ok = true;
    return cursor;
}

// the BUILDER's form: the live elements out of the segment chain, in the
// order they were added. A dead element costs nothing on any wire (§2.9).
template <typename T>
inline TableListCursor<typename TableList<T>::Element> TableListElements( const TableArena & arena, const TableList<T> & list )
{
    TableListCursor<typename TableList<T>::Element> cursor;
    cursor.arena = &arena;
    cursor.count = list.count;
    if ( list.elements.value == 0 || list.count <= 0 ) { cursor.ok = list.count == 0; cursor.count = 0; return cursor; }
    const TableListHead * head = (const TableListHead *) TableArenaAt( arena, (uint32_t) list.elements.value );
    if ( head->live != list.count ) { return cursor; } // the slot and the head disagree: refused, never guessed
    cursor.first = head->first;
    cursor.ok = true;
    return cursor;
}

template <typename T>
inline TableListCursor<typename TableList<T>::Element> TableListElements( const TableArenaCtx & ctx, const TableList<T> & list )
{
    return TableListElements( *ctx.arena, list );
}

// ---- the LOAD side: where a decoded element lands (§2.9) ----
//
// The same two shapes the map's fill takes, because the decoder above them
// cannot tell which it has: a REGION carves the element array out of the
// holder node's own extent, PRE-ORDER, and the TOOL's path appends into the
// builder's arena.
template <typename T> struct TableListFill
{
    typedef typename TableList<T>::Element Element;
    TableList<T> * list = NULL;
    Element * array = NULL;      // the region path: the carved array
    int32_t capacity = 0;
    TableWorker * worker = NULL; // the TOOL's path
    bool ok = false;
    bool refused = false;        // a count above the int32 cap on the tool's path: LoadBuilder answers NULL
};

template <typename T>
inline TableListFill<T> TableListFillBegin( const TableNodeMap & nodes, TableList<T> & list, uint64_t n )
{
    typedef typename TableList<T>::Element Element;
    TableListFill<T> fill;
    fill.list = &list;
    list.elements.value = 0;
    list.count = 0;
    if ( nodes.carve == NULL ) { return fill; }
    if ( n > (uint64_t) INT32_MAX )
    {
        // A COUNT ABOVE THE int32 STORAGE CAP (§2.2, §2.9): into a region it was
        // refused by LoadMeasure before this ran, and into a builder it is the
        // refusal LoadBuilder answers NULL for, moving no counter
        fill.refused = nodes.carve->worker != NULL;
        return fill;
    }
    if ( nodes.carve->worker != NULL )
    {
        fill.worker = nodes.carve->worker; // the tool's path: the arena carves
        fill.ok = true;
        return fill;
    }
    const int64_t align = (int64_t) alignof( Element );
    uint8_t * base = (uint8_t *) ( ( (uintptr_t) nodes.carve->at + (uintptr_t) ( align - 1 ) ) & ~( (uintptr_t) ( align - 1 ) ) );
    const int64_t bytes = (int64_t) n * (int64_t) sizeof( Element );
    const int64_t pad = (int64_t) ( base - nodes.carve->at );
    if ( pad + bytes > nodes.carve->left ) { return fill; } // the measure and the load disagree: refused
    nodes.carve->at = base + bytes;
    nodes.carve->left -= pad + bytes;
    fill.array = (Element *) base;
    fill.capacity = (int32_t) n;
    list.elements.value = (int64_t) ( base - (const uint8_t *) &list.elements );
    fill.ok = true;
    return fill;
}

// the next slot, at the element's declared defaults. NULL when the arena
// could not carve, which the decoder reports as framing damage
template <typename T> inline typename TableList<T>::Element * TableListFillNext( TableListFill<T> & fill )
{
    typedef typename TableList<T>::Element Element;
    if ( fill.array != NULL )
    {
        if ( fill.list->count >= fill.capacity ) { return NULL; }
        Element * element = fill.array + fill.list->count;
        new ( element ) Element();
        fill.list->count++;
        return element;
    }
    return TableListPlace( *fill.worker, *fill.list );
}

// A SLOT WHOSE ELEMENT NEVER LANDED is given back (§2.9, §4): the array keeps
// what it decoded, and an element whose own framing gave out before one byte
// of it decoded was not decoded. The region's form uncounts it, and the builder's
// marks it dead, which is what the storage rule allows mid-build.
template <typename T> inline void TableListFillDrop( TableListFill<T> & fill )
{
    typedef typename TableList<T>::Element Element;
    if ( fill.array != NULL )
    {
        if ( fill.list->count > 0 ) { fill.list->count--; }
        return;
    }
    if ( fill.list->elements.value == 0 ) { return; }
    TableListHead * head = (TableListHead *) TableArenaAt( *fill.worker->arena, (uint32_t) fill.list->elements.value );
    if ( head->last.value == 0 ) { return; }
    TableListSegment<Element> * segment = (TableListSegment<Element> *) TableArenaAt( *fill.worker->arena, (uint32_t) head->last.value );
    if ( segment->used <= 0 ) { return; }
    const int32_t i = segment->used - 1;
    if ( TableListSegmentDead( segment->dead, i ) ) { return; }
    segment->dead[ i / 32 ] |= 1u << ( i % 32 );
    head->live--;
    head->dead++;
    fill.list->count--;
}

// an EMPTY list's reference is null in both encodings, so a load that placed
// nothing leaves the slot exactly as a Reset does
template <typename T> inline void TableListFillEnd( TableListFill<T> & fill )
{
    if ( fill.array != NULL && fill.list->count == 0 ) { fill.list->elements.value = 0; }
}

// ---- LoadMeasure's term, from the FRAMING alone (§2.9, §6.5) ----
//
// N x sizeof( T ) rounded to alignof( T ), AT EVERY DEPTH. N is framing and
// not a value, so this reads no field: it walks the list's own header and,
// where a table element holds a list or a map of its own, the elements'
// headers under it. Every -1 carries its REASON (§6.5): the int32 cap first,
// because a count past it cannot fit any body, and then the body's own L.
inline bool TableListWireExtent( const uint8_t * body, int64_t length, int64_t & at,
                                 int64_t elem_size, int64_t elem_align, uint8_t elem_kind, int64_t elem_floor,
                                 TableWireExtentFn inner, const TableIdTable * ids, TableRefuseReason & reason )
{
    TableReport scratch;
    TableReader r( body, length, &scratch, ids );
    if ( length < 2 ) { return true; }              // no array header: nothing rides
    if ( r.get8() != elem_kind ) { return true; }  // another element kind: §4's ordinary kind mismatch, the field reads empty
    uint64_t n = 0;
    if ( !r.getleb( n ) ) { return true; }
    if ( n > (uint64_t) INT32_MAX ) { reason = count_over_extent_cap; return false; }
    const int64_t rest = length - r.offset;
    if ( n > (uint64_t) ( rest / elem_floor ) ) { reason = count_over_length; return false; } // an N the list's L cannot carry
    at = ( at + elem_align - 1 ) & ~( elem_align - 1 );
    at += (int64_t) n * elem_size;
    if ( inner == NULL ) { return true; } // nothing below an element: one depth is the whole term
    for ( uint64_t i = 0; i < n; i++ )
    {
        uint64_t elem = 0;
        if ( !r.getleb( elem ) || !r.room( elem ) ) { return true; } // framing damage: the load reports it
        if ( !inner( r.buffer + r.offset, (int64_t) elem, at, ids, reason ) ) { return false; }
        r.offset += (int64_t) elem;
    }
    return true;
}

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// ---- the list's wire codecs (docs/SPEC-TABLES.md §2.9, §3) ----
//
// A list rides as the kind 14 ARRAY a bounded array rides as, over the
// element's own kind, so the element measure, write and read are the bounded
// array's own emitters over a cursor rather than over inline storage. What
// is the list's own is the cursor and the fill.

// emitListMeasureField adds one list field to a body's measured size.
func (g *tableGen) emitListMeasureField(f *ir.Field) {
	id := ir.TableFieldWireId(f)
	cursor := "cursor_" + f.Name
	g.pf("    {\n")
	g.pf("        // %s: a kind %d array of kind %d elements, INDEX order (§2.9)\n", f.Name, tkArray, listElementWireKind(f))
	g.pf("        TableListCursor<%s> %s = TableListElements( ctx, value.%s );\n", g.listElementType(f), cursor, f.Name)
	g.pf("        if ( !%s.ok ) { return -1; } // the slot and the head disagree\n", cursor)
	g.pf("        if ( %s.count > 0 ) // an EMPTY list elides, the by-value rule (§3)\n        {\n", cursor)
	g.pf("            const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
	g.pf("            int64_t body_%s = 0;\n", f.Name)
	g.emitArrayBodyMeasure(f, listElementWireKind(f), "body_"+f.Name, cursor+".count", cursor+"[%s]", "            ", "return -1;", "_"+f.Name)
	g.pf("            bytes += TableLebBytes( ref_%s ) + 1 + %s;\n", f.Name, framed("body_"+f.Name))
	g.pf("        }\n")
	g.pf("    }\n")
}

// emitListWriteField writes one list field: the array framing, then the live
// elements in INDEX order, dead elements dropped.
func (g *tableGen) emitListWriteField(f *ir.Field) {
	id := ir.TableFieldWireId(f)
	cursor := "cursor_" + f.Name
	g.pf("    {\n")
	g.pf("        TableListCursor<%s> %s = TableListElements( ctx, value.%s ); // %s\n", g.listElementType(f), cursor, f.Name, f.Name)
	g.pf("        if ( !%s.ok ) { return false; }\n", cursor)
	g.pf("        if ( %s.count > 0 ) // an EMPTY list elides, the by-value rule (§3)\n        {\n", cursor)
	g.pf("            const uint64_t ref_%s = %s;\n", f.Name, g.wireRef(id))
	g.pf("            int64_t body_%s = 0;\n", f.Name)
	g.emitArrayBodyMeasure(f, listElementWireKind(f), "body_"+f.Name, cursor+".count", cursor+"[%s]", "            ", "return false;", "_"+f.Name)
	g.pf("            w.putleb( ref_%s ); w.put8( %d ); w.putleb( (uint64_t) body_%s ); // %s\n", f.Name, tkArray, f.Name, f.Name)
	g.emitArrayBodyWrite(f, listElementWireKind(f), cursor+".count", cursor+"[%s]", "            ", "_"+f.Name)
	g.pf("        }\n")
	g.pf("    }\n")
}

// emitListReadField decodes one list field, and every reader rule §2.9
// states lands here: the element kind checked against the reader's
// declaration, the count taken as the data's with nothing to clamp it
// against, the elements bounded by the body's own L, and a slot whose element
// never landed given back.
func (g *tableGen) emitListReadField(f *ir.Field) {
	ind := "                "
	elemKind := listElementWireKind(f)
	g.pf("%suint64_t body_len = 0;\n", ind)
	g.pf("%sif ( !r.getleb( body_len ) || !r.room( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
	g.pf("%sint64_t body_end = r.offset + (int64_t) body_len;\n", ind)
	g.pf("%s// A BODY TOO SHORT FOR ITS OWN HEADER is INERT (§4): the field keeps\n", ind)
	g.pf("%s// the value it has, no counter is raised, and the walk continues past L.\n", ind)
	g.pf("%sif ( body_len >= 2 )\n%s{\n", ind, ind)
	g.pf("%s    uint8_t elem_kind = r.get8();\n", ind)
	g.pf("%s    uint64_t count = 0;\n", ind)
	g.pf("%s    const bool counted_ok = r.getleb( count );\n", ind)
	g.pf("%s    if ( !counted_ok ) { r.report->malformed = true; }\n", ind)
	g.pf("%s    // AN ELEMENT KIND THAT DISAGREES with the reader's declaration is §3's\n", ind)
	g.pf("%s    // element-kind rule: the field reads EMPTY and one kind_mismatch counts\n", ind)
	g.pf("%s    else if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, elemKind)
	g.pf("%s    else\n%s    {\n", ind, ind)
	g.pf("%s        // THE COUNT IS THE DATA'S (§2.9): there is no bound, so clamped\n", ind)
	g.pf("%s        // cannot fire on it. A count above the int32 storage cap is the\n", ind)
	g.pf("%s        // fill's refusal, and it moves no counter.\n", ind)
	g.pf("%s        TableListFill<%s> fill = TableListFillBegin( nodes, value.%s, count );\n", ind, g.listTypeArg(f), f.Name)
	g.pf("%s        if ( fill.refused ) { nodes.refused = true; return false; }\n", ind)
	g.pf("%s        if ( !fill.ok ) { r.report->malformed = true; r.offset = body_end; break; }\n", ind)
	g.pf("%s        // elements are BOUNDED by the field body: a count the length cannot\n", ind)
	g.pf("%s        // cover keeps the decoded prefix, flags malformed, and the parent\n", ind)
	g.pf("%s        // continues at the next field\n", ind)
	g.pf("%s        TableReader sub( r.buffer + r.offset, body_end - r.offset, r.report, r.ids );\n", ind)
	g.pf("%s        for ( uint64_t i = 0; i < count; i++ )\n%s        {\n", ind, ind)
	g.pf("%s            %s * slot = TableListFillNext( fill );\n", ind, g.listElementType(f))
	g.pf("%s            if ( slot == NULL ) { r.report->malformed = true; break; } // the arena could not carve\n", ind)
	g.pf("%s            bool landed = false;\n", ind)
	g.pf("%s            do\n%s            {\n", ind, ind)
	g.emitTableReadElementInto(f, elemKind, "( *slot )", ind+"                ", "sub", "_"+f.Name)
	g.pf("%s                landed = true;\n", ind)
	g.pf("%s            } while ( 0 );\n", ind)
	g.pf("%s            if ( !landed ) { TableListFillDrop( fill ); break; } // the element's own framing gave out before it decoded\n", ind)
	g.pf("%s        }\n", ind)
	g.pf("%s        TableListFillEnd( fill );\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)
	g.pf("%sr.offset = body_end; // excess bytes and slack skip via the length\n", ind)
}

// ---- the three walks at a list (docs/SPEC-TABLES.md §2.9, §3.1) ----
//
// A list is a BY-VALUE EDGE of the ONE declaration-order walk: it is reached
// at its field's position, its elements are visited in INDEX ORDER, and each
// element is descended for the pointer slots inside it before the next
// element is reached. A []*T declared before a pointer field therefore
// reaches a shared node FIRST and numbers it first. The rule is the walk's,
// not the list's.

// emitListEdge opens one list's cursor under the walk's subjects and visits
// each element through the emitter's own pointer or descend visitor: the
// element IS the pointer slot on a []*T, and a variable table to descend on a
// []T. The pack's twin is the array <T>ExtentPack already placed in the node's
// extent, reached from the write subject's slot.
func (g *tableGen) emitListEdge(f *ir.Field, v edgeVisitor, onBad string) {
	elem := g.listElementType(f)
	cursor := "cursor_" + f.Name
	g.pf("    { // %s: a by-value edge, elements in INDEX order (§2.9, §3.1)\n", f.Name)
	g.pf("        TableListCursor<%s> %s = TableListElements( ctx, %s.%s );\n", elem, cursor, v.read, f.Name)
	g.pf("        if ( !%s.ok ) { %s }\n", cursor, onBad)
	placed := ""
	if v.write != "" {
		placed = "placed_" + f.Name
		slot := v.write + "." + f.Name
		g.pf("        %s * %s = (%s *) ( %s.elements.value != 0 ? ( (uint8_t *) &%s.elements + %s.elements.value ) : NULL );\n",
			elem, placed, elem, slot, slot, slot)
	}
	g.pf("        for ( int32_t i = 0; i < %s.count; i++ )\n        {\n", cursor)
	expr := edgeExpr{Src: cursor + "[i]"}
	if placed != "" {
		expr.Dst = placed + "[i]"
	}
	saved := g.indent
	g.indent = saved + "        "
	if listElementIsPointer(f) {
		v.pointer(f, expr)
	} else {
		v.descend(f.Type.Name, expr, "    ")
	}
	g.indent = saved
	g.pf("        }\n    }\n")
}

// ---- the builder's three (docs/SPEC-TABLES.md §2.9) ----
//
// FREE FUNCTIONS taking the worker or the arena, as Emplace and the map's
// five are. Add takes the WORKER because it may allocate a segment. Each and
// Erase take the arena because neither ever does.
func (g *tableGen) emitListBuilderSurface(owner *ir.Struct, f *ir.Field) {
	hold := fmt.Sprintf("TableList<%s> & list", g.listTypeArg(f))
	elem := g.listElementType(f)
	g.pf("// ---- %s.%s: the builder's three (§2.9) ----\n\n", owner.Name, f.Name)
	if listElementIsPointer(f) {
		g.pf("// ADD: the element is appended and handed back to fill. On a []*T that is\n")
		g.pf("// the SLOT at null, which %sEmplace fills as it fills any pointer slot,\n", f.Type.Name)
		g.pf("// and a second slot may hold the same reference: two slots, one node.\n")
	} else {
		g.pf("// ADD: the element is appended at its declared defaults and handed back to\n")
		g.pf("// fill. Nothing ever moves (§6.4), so the pointer stays valid while other\n")
		g.pf("// elements arrive.\n")
	}
	g.pf("// NULL means NOT ADDED: an arena that cannot carve another segment, or a\n")
	g.pf("// count at the int32 cap. A caller that needs the reason checks size().\n")
	g.pf("inline %s * %s( TableWorker & worker, %s )\n{\n", elem, listVerb(owner.Name, f, "Add"), hold)
	g.pf("    return TableListPlace( worker, list );\n}\n\n")
	g.pf("// ERASE, by the element's own pointer: marks it DEAD, one bit in the\n")
	g.pf("// segment's slot and not in the element storage. False when the pointer is\n")
	g.pf("// not this list's. Storage is held until the builder resets. INDICES ARE\n")
	g.pf("// NOT STABLE ACROSS AN ERASE: what was index 3 is index 2 in the next Save.\n")
	g.pf("inline bool %s( TableArena & arena, %s, const %s * element )\n{\n", listVerb(owner.Name, f, "Erase"), hold, elem)
	g.pf("    return TableListErase( arena, list, element );\n}\n\n")
	g.pf("// EACH on the builder: INDEX order, live elements only, yielding the\n")
	g.pf("// element Add handed back.\n")
	g.pf("inline TableListEach<%s> %s( const TableArena & arena, const TableList<%s> & list )\n{\n", g.listTypeArg(f), listVerb(owner.Name, f, "Each"), g.listTypeArg(f))
	g.pf("    return TableListEachOf( arena, list );\n}\n\n")
}

// emitListBuilderSurfaces emits every list field's builder three, after the
// arena and list runtimes they are spelled in terms of.
func (g *tableGen) emitListBuilderSurfaces(members []*ir.Struct) {
	if !g.anyList {
		return
	}
	for _, st := range members {
		for _, f := range listFieldsOf(st) {
			g.emitListBuilderSurface(st, f)
		}
	}
}

// emitListAlignAsserts holds every list element to the arena's alignment
// (docs/SPEC-TABLES.md §2.9): a node's extent begins at the arena's alignment
// and nothing inside it can ask for more.
func (g *tableGen) emitListAlignAsserts(members []*ir.Struct) {
	if !g.anyList {
		return
	}
	for _, st := range members {
		for _, f := range listFieldsOf(st) {
			g.pf("static_assert( alignof( %s ) <= kTableAlign, \"%s.%s: an unbounded array's element alignment must fit the arena's\" );\n", g.listElementType(f), st.Name, f.Name)
		}
	}
	g.pf("\n")
}

// listPlaceThunk is the descriptor's PLACE column for a list field
// (docs/SPEC-TABLES.md §8.1, §16): the one resolver the text walk cannot spell
// for itself, because TableList<T> is a type it has no name for. The key
// arguments are the map's and a list ignores them: it appends.
func (g *tableGen) listPlaceThunk(f *ir.Field) string {
	return fmt.Sprintf("[]( TableWorker & worker, void * slot, const char *, int32_t, int64_t ) -> void * { return (void *) TableListPlace( worker, *(%s *) slot ); }", g.listStorageType(f))
}
