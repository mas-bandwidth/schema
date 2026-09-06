// RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6): the opt-in that keeps what this
// build cannot name, across a REGION round trip and only that.
//
// It is a SECOND, PARALLEL FAMILY of body functions — <T>LoadBodyRetain,
// <T>MeasureBodyRetain and <T>SaveBodyRetainFields — beside the three the
// wire already had. Load, Measure and Save are not touched and not read here:
// a caller that does not ask keeps the codec it always had, to the byte, which
// is what makes the whole feature free where it is not used.
//
// The runtime below is schema-independent and the emitter threads the PATH
// through it. Nothing here allocates: the record buffer and the retained-id
// list are the caller's, declared with their capacities, and a record that
// does not fit whole is dropped with one retain_lost.
package cpptable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableRetainDepth is the unit's own BY-VALUE nesting depth, which is the
// number of PATH STEPS a retained record can ever need after the first
// (docs/SPEC-TABLES.md §6.6). By-value cycles are refused (§2) and a pointer
// edge is not a step, so this walk terminates and the answer is a
// compile-time constant of the unit: a hostile file cannot make a path long.
func tableRetainDepth(u *ir.Unit, closure map[string]bool) int {
	memo := map[string]int{}
	var depthOf func(name string, open map[string]bool) int
	var fieldDepth func(f *ir.Field, open map[string]bool) int

	unionDepth := func(un *ir.Union, open map[string]bool) int {
		best := 0
		for _, v := range un.Variants {
			if v.F == nil {
				continue
			}
			// an arm is a step of its own: the union field descends into the
			// arm's body, and the arm's own contents descend again
			if d := fieldDepth(v.F, open) + 1; d > best {
				best = d
			}
		}
		return best
	}

	fieldDepth = func(f *ir.Field, open map[string]bool) int {
		if f.Type.Pointer {
			return 0 // a pointer target is a NODE and takes a first step of its own
		}
		if f.IsMap() {
			// the entry body is the step, and the value's own body is another
			if entry := f.MapEntry; entry != nil {
				return depthOf(entry.Name, open) + 1
			}
			return 1
		}
		if f.Type.Kind == ir.TNamed {
			switch ref := f.Type.Ref.(type) {
			case *ir.Union:
				return unionDepth(ref, open) + 1
			case *ir.Struct:
				return depthOf(ref.Name, open) + 1
			}
		}
		return 0
	}

	depthOf = func(name string, open map[string]bool) int {
		if d, ok := memo[name]; ok {
			return d
		}
		if open[name] {
			return 0 // defensive: by-value cycles are refused before this runs
		}
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			return 0
		}
		open[name] = true
		best := 0
		for _, f := range st.Fields {
			if d := fieldDepth(f, open); d > best {
				best = d
			}
		}
		delete(open, name)
		memo[name] = best
		return best
	}

	best := 0
	for name := range closure {
		if d := depthOf(name, map[string]bool{}); d > best {
			best = d
		}
	}
	if best < 1 {
		best = 1 // a zero-length array is not a type; one step is the floor
	}
	return best
}

// tableRetainKnownIds renders the unit's own id set as a sorted constant, and
// the count beside it. An id inside a retained record takes its trailer entry
// from the GENERATED table when this build can name it and from the CALLER's
// list otherwise (docs/SPEC-TABLES.md §6.6), and this array is what decides
// which — the same set TableIds's capacity is derived from.
func tableRetainKnownIds(u *ir.Unit) (string, int) {
	ids := ir.TableWireIds(u)
	var b strings.Builder
	for i, id := range ids {
		if i%4 == 0 {
			b.WriteString("\n    ")
		}
		fmt.Fprintf(&b, "0x%016xull,", id)
		if i%4 != 3 && i != len(ids)-1 {
			b.WriteString(" ")
		}
	}
	b.WriteString("\n")
	return b.String(), len(ids)
}

// tableRetainRuntime is §6.6's runtime, emitted into a unit that declares a
// VARIABLE-LENGTH table. A fixed-only unit carries none of it: retention is
// the variable class's, and a fixed-class root's three verbs are refused by
// name in the source that unit does emit.
func tableRetainRuntime(pkg string, u *ir.Unit, depth int) string {
	known, count := tableRetainKnownIds(u)
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_RETAIN"
	body := strings.NewReplacer(
		"@DEPTH@", strconv.Itoa(depth),
		"@KNOWN@", known,
		"@KNOWNCOUNT@", strconv.Itoa(count),
	).Replace(cppRetainRuntime)
	return "#ifndef " + guard + "\n#define " + guard + "\n\nnamespace " + pkg + " {\n\n" +
		body + "} // namespace " + pkg + "\n\n#endif // " + guard + "\n"
}

const cppRetainRuntime = `// ---- RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6) ----
//
// A REGION ROUND TRIP AND ONLY THAT: LoadRetain is Load's path into a region
// and SaveRetain saves from that same region. The builder path carries no
// retention, because a builder has no node directory to anchor a record on and
// re-derives its numbering from the reader's declaration order.
//
// Nothing here allocates. The record bytes and the retained-id list are the
// caller's storage, declared with their capacities, and a record that does not
// fit whole is dropped with one retain_lost.

// THE PATH NAMES THE BODY, and it is the REGION's own address (§6.6). Step one
// is the node's index in the region's node directory, 1 for the root body and
// k for the node at directory position k - 1. Every further step is the PAIR:
// the field ordinal in the body the step descends from, in the READER's own
// declaration order, and the element index inside that field — zero for a
// scalar body, the element's index for an array of any of the four kinds, the
// ARM's OWN ORDINAL for a union, and the key's slot for a map.
static const int32_t kTableRetainDepthMax = @DEPTH@;

struct TableRetainStep
{
    uint32_t ordinal;
    uint32_t index;
};

// at is the node's own address, which is what the SAVE side matches on: a
// record carries the directory INDEX and the directory answers the address in
// one add, so neither side ever searches a numbering.
struct TableRetainPath
{
    const void * at;
    uint32_t node;
    int32_t depth;
    TableRetainStep steps[ kTableRetainDepthMax ];
};

inline TableRetainPath TableRetainPathRoot( const void * at, uint32_t node )
{
    TableRetainPath path;
    path.at = at;
    path.node = node;
    path.depth = 0;
    return path;
}

// A STEP IS COMPUTED LOCALLY, at the moment the walk descends (§6.6), and it
// is taken by VALUE so that a descent is an expression: both sides walk the
// same declaration order, so neither numbers a tree and neither pops.
inline TableRetainPath TableRetainStepInto( const TableRetainPath & path, uint32_t ordinal, uint32_t index )
{
    TableRetainPath out = path;
    if ( out.depth < kTableRetainDepthMax )
    {
        out.steps[ out.depth ].ordinal = ordinal;
        out.steps[ out.depth ].index = index;
    }
    out.depth++;
    return out;
}

// THE CALLER'S TWO STORES (§6.6): the record bytes and the retained ids, each
// a pointer, a capacity and what has been used of it. A retention buffer
// belongs to ONE loaded region, and the next LoadRetain into it resets both.
struct TableRetain
{
    // AN ENTRY IS THE ID AND ITS SLOT IN THE TRAILER BEING WRITTEN. The two
    // stores are numbered into ONE trailer in merged first-use order, so an
    // index into this list is not the number a second reference wants and the
    // slot rides beside the id. The layout is this port's own.
    struct Id
    {
        uint64_t id;
        int32_t slot;
    };

    uint8_t * bytes = NULL;
    int64_t capacity = 0;
    int64_t used = 0;
    Id * ids = NULL;
    int32_t id_capacity = 0;
    int32_t id_used = 0;
    int32_t count = 0; // records held

    // the REGION this buffer belongs to: a record carries a directory index
    // and the save resolves it here, so nothing searches and nothing allocates
    const uint8_t * base = NULL;
    const TableNodeDirEntry * directory = NULL;
    int64_t directory_count = 0;
};

// A RETAINED RECORD IS READER-PRIVATE (§6.6). It is not a wire form: no form
// byte, no version, no declared byte order, and nothing ever writes one to
// disk or hands one to another process. What it must CARRY is the body it
// belongs to, and the field's identity and bytes with every reference
// resolved. This layout is one sound way to carry them and nothing compares
// two ports' buffers.
//
//   u32 record bytes, this header included
//   u32 node          the path's first step
//   u32 depth         the step pairs that follow
//   u32 payload bytes
//   u64 field id
//   u8  kind
//   u8  placed        the save's own mark, cleared before every save
//   depth x { u32 ordinal, u32 index }
//   payload           the field's payload with every reference resolved
static const int64_t kTableRetainRecordHeader = 26;

inline uint32_t TableRetainRead32( const uint8_t * p )
{
    return uint32_t( p[0] ) | uint32_t( p[1] ) << 8 | uint32_t( p[2] ) << 16 | uint32_t( p[3] ) << 24;
}

inline uint64_t TableRetainRead64( const uint8_t * p )
{
    return uint64_t( TableRetainRead32( p ) ) | ( uint64_t( TableRetainRead32( p + 4 ) ) << 32 );
}

inline void TableRetainWrite32( uint8_t * p, uint32_t v )
{
    p[0] = uint8_t( v ); p[1] = uint8_t( v >> 8 ); p[2] = uint8_t( v >> 16 ); p[3] = uint8_t( v >> 24 );
}

inline void TableRetainWrite64( uint8_t * p, uint64_t v )
{
    TableRetainWrite32( p, uint32_t( v ) );
    TableRetainWrite32( p + 4, uint32_t( v >> 32 ) );
}

inline int64_t TableRetainRecordBytes( const uint8_t * record ) { return (int64_t) TableRetainRead32( record ); }
inline uint32_t TableRetainRecordNode( const uint8_t * record ) { return TableRetainRead32( record + 4 ); }
inline int32_t TableRetainRecordDepth( const uint8_t * record ) { return (int32_t) TableRetainRead32( record + 8 ); }
inline int64_t TableRetainRecordPayloadBytes( const uint8_t * record ) { return (int64_t) TableRetainRead32( record + 12 ); }
inline uint64_t TableRetainRecordId( const uint8_t * record ) { return TableRetainRead64( record + 16 ); }
inline uint8_t TableRetainRecordKind( const uint8_t * record ) { return record[24]; }
inline bool TableRetainRecordPlaced( const uint8_t * record ) { return record[25] != 0; }
inline const uint8_t * TableRetainRecordSteps( const uint8_t * record ) { return record + kTableRetainRecordHeader; }
inline const uint8_t * TableRetainRecordPayload( const uint8_t * record )
{
    return record + kTableRetainRecordHeader + 8 * (int64_t) TableRetainRecordDepth( record );
}

// THE RECORD'S OWN BODY, resolved through the directory the buffer holds. A
// node index names one node for the life of the region, so this is one add.
inline const void * TableRetainRecordAt( const TableRetain & retain, const uint8_t * record )
{
    const uint32_t node = TableRetainRecordNode( record );
    if ( retain.directory == NULL || node == 0 || (int64_t) node > retain.directory_count ) { return NULL; }
    return (const void *) ( retain.base + retain.directory[ node - 1 ].offset );
}

// Does this record belong to the body the walk is standing in? The node first,
// which rejects almost everything in one compare, then the step pairs.
inline bool TableRetainRecordHere( const TableRetain & retain, const uint8_t * record, const TableRetainPath & path )
{
    if ( TableRetainRecordDepth( record ) != path.depth ) { return false; }
    if ( TableRetainRecordAt( retain, record ) != path.at ) { return false; }
    const uint8_t * steps = TableRetainRecordSteps( record );
    for ( int32_t i = 0; i < path.depth; i++ )
    {
        if ( TableRetainRead32( steps + 8 * i ) != path.steps[i].ordinal ) { return false; }
        if ( TableRetainRead32( steps + 8 * i + 4 ) != path.steps[i].index ) { return false; }
    }
    return true;
}

// EVERY ID THIS BUILD CAN NAME, ascending: the set TableIds's capacity is
// derived from. An id inside a retained record takes its trailer entry from
// the GENERATED table when it is here and from the CALLER's list otherwise, so
// no retained id ever enters the generated table and no id is written twice.
static const int32_t kTableRetainKnownIds = @KNOWNCOUNT@;
static const uint64_t kTableRetainKnown[ kTableRetainKnownIds ] = {@KNOWN@};

inline bool TableRetainNameable( uint64_t id )
{
    int32_t low = 0, high = kTableRetainKnownIds - 1;
    while ( low <= high )
    {
        const int32_t mid = low + ( high - low ) / 2;
        if ( kTableRetainKnown[mid] == id ) { return true; }
        if ( kTableRetainKnown[mid] < id ) { low = mid + 1; } else { high = mid - 1; }
    }
    return false;
}

// THE TWO STORES, NUMBERED INTO ONE TRAILER in merged first-use order (§6.6).
// It answers the surface TableIds answers — ref, count, truncate, overflow —
// so the retain family's codec is the plain one with its names changed, and
// the GENERATED TABLE IS UNTOUCHED: its capacity, its overflow rule and its
// -1 stand exactly as they are for every save.
struct TableRetainIds
{
    TableIds known;
    int32_t known_slot[ TableIds::kCapacity ];
    TableRetain * retain;
    int32_t count;
    bool overflow;
    bool lost; // a retained id past the caller's capacity: the record is dropped

    TableRetainIds( TableRetain * to_retain ) : retain( to_retain ), count( 0 ), overflow( false ), lost( false ) {}

    // an id this build CAN name, which is every id the generated codec writes
    uint64_t ref( uint64_t id )
    {
        const int32_t before = known.count;
        const uint64_t k = known.ref( id );
        if ( known.overflow ) { overflow = true; return 1; }
        if ( known.count != before ) { known_slot[ (int32_t) k - 1 ] = ++count; }
        return (uint64_t) known_slot[ (int32_t) k - 1 ];
    }

    // an id from INSIDE a retained record. A retained id takes its entry from
    // the caller's list, and one past the capacity sets lost: the record is
    // dropped, nothing else about the save changes, and the save is never
    // refused (§6.6).
    uint64_t record_ref( uint64_t id )
    {
        if ( TableRetainNameable( id ) ) { return ref( id ); }
        if ( retain == NULL ) { lost = true; return 0; }
        for ( int32_t i = 0; i < retain->id_used; i++ )
        {
            if ( retain->ids[i].id == id ) { return (uint64_t) retain->ids[i].slot; }
        }
        if ( retain->id_used >= retain->id_capacity ) { lost = true; return 0; }
        retain->ids[ retain->id_used ].id = id;
        retain->ids[ retain->id_used ].slot = ++count;
        retain->id_used++;
        return (uint64_t) count;
    }

    // undo every entry taken since mark, in either store. Both are appended in
    // slot order, so an entry removed is the last one of its store.
    void truncate( int32_t mark )
    {
        while ( known.count > 0 && known_slot[ known.count - 1 ] > mark ) { known.truncate( known.count - 1 ); }
        while ( retain != NULL && retain->id_used > 0 && retain->ids[ retain->id_used - 1 ].slot > mark ) { retain->id_used--; }
        count = mark;
    }
};

// THE FILE STILL CARRIES ONE ID TABLE (§3): the split is the writer's storage
// rather than the wire's, and the trailer is one merge of two slot-ordered
// stores.
inline int64_t TableRetainIdsBytes( const TableRetainIds & ids ) { return int64_t( ids.count ) * 8 + 8; }

inline void TableRetainIdsWrite( TableWriter & w, const TableRetainIds & ids )
{
    int32_t i = 0, j = 0;
    for ( int32_t slot = 1; slot <= ids.count; slot++ )
    {
        if ( i < ids.known.count && ids.known_slot[i] == slot ) { w.put64( ids.known.ids[i] ); i++; continue; }
        if ( ids.retain != NULL && j < ids.retain->id_used && ids.retain->ids[j].slot == slot ) { w.put64( ids.retain->ids[j].id ); j++; continue; }
        w.put64( 0 ); // unreachable: every slot was taken by one store or the other
    }
    w.put64( uint64_t( ids.count ) );
}

// ---- THE RESOLVING WALK (§6.6) ----
//
// A reference names a SLOT of the file's id table, so a verbatim copy
// re-emitted into a file whose table is ordered differently would point at
// other names in silence. A retained record therefore holds the field with
// every reference replaced by the sixty-four-bit id it names, and every length
// that frames a rewritten reference recomputed.
//
// THE WALK IS AN INTERPRETATION, AND ITS VERDICT IS STATED: it reads kind
// bytes, lengths and references and nothing else. No value is decoded, no
// bound is checked, no branch is taken on a payload byte, and anything it
// cannot frame DROPS THE RECORD, counts one retain_lost, and never raises
// malformed on the plain read.
//
// A retained record's inner nesting is the WRITER's and not this build's, so
// it is the one depth on this path a file can drive. The walk caps it and a
// record that nests past the cap is dropped, on the same rule as any other
// shape the walk cannot take.
static const int32_t kTableRetainWalkDepthMax = 64;

// the three RESERVED ids (§3.1, §3.3). One inside a retained record's payload
// would be re-emitted into a nested body, where it is malformed, so meeting
// one drops the record.
inline bool TableRetainReservedId( uint64_t id )
{
    return id == kTableNodeTableFieldId || id == kTableBuildVersionFieldId || id == kTableMessageVocabularyFieldId;
}

struct TableRetainIn
{
    const uint8_t * in;
    int64_t size;
    int64_t at;
    const TableIdTable * ids;
    uint8_t * out;   // NULL: measuring, and nothing is written
    int64_t out_at;
};

inline void TableRetainInRaw( TableRetainIn & s, const uint8_t * from, int64_t bytes )
{
    if ( s.out != NULL ) { memcpy( s.out + s.out_at, from, (size_t) bytes ); }
    s.out_at += bytes;
}

inline void TableRetainInLeb( TableRetainIn & s, uint64_t v )
{
    uint8_t b[10];
    int64_t n = 0;
    while ( v >= 0x80 ) { b[n++] = uint8_t( v ) | 0x80; v >>= 7; }
    b[n++] = uint8_t( v );
    TableRetainInRaw( s, b, n );
}

inline void TableRetainInId( TableRetainIn & s, uint64_t id )
{
    uint8_t b[8];
    TableRetainWrite64( b, id );
    TableRetainInRaw( s, b, 8 );
}

inline bool TableRetainInLebRead( TableRetainIn & s, uint64_t & value )
{
    value = 0;
    uint32_t shift = 0;
    for ( int32_t i = 0; i < 10; i++ )
    {
        if ( s.at >= s.size ) { return false; }
        const uint8_t b = s.in[ s.at++ ];
        if ( i == 9 && b > 1 ) { return false; }
        value |= uint64_t( b & 0x7F ) << shift;
        if ( ( b & 0x80 ) == 0 ) { return i == 0 || b != 0; }
        shift += 7;
    }
    return false;
}

// one REFERENCE resolved to the id it names. A zero reference is the wire's
// own "no id" — the enum's None and the union's empty arm — and rides as the
// id zero. A reference above the entry count, a reference at an id-table entry
// of zero, and a reference at a reserved id are each damage the plain read
// never looked at, and each drops the record.
inline bool TableRetainInRef( TableRetainIn & s, bool zero_allowed )
{
    uint64_t ref = 0;
    if ( !TableRetainInLebRead( s, ref ) ) { return false; }
    if ( ref == 0 )
    {
        if ( !zero_allowed ) { return false; }
        TableRetainInId( s, 0 );
        return true;
    }
    if ( s.ids == NULL || ref > (uint64_t) s.ids->count ) { return false; }
    const uint64_t id = s.ids->at( ref );
    if ( id == 0 || TableRetainReservedId( id ) ) { return false; }
    TableRetainInId( s, id );
    return true;
}

inline int64_t TableRetainInPayload( TableRetainIn & s, uint8_t kind, int32_t depth );
inline int64_t TableRetainInContent( TableRetainIn & s, uint8_t kind, int64_t length, int32_t depth );

// the resolved byte count a framed CONTENT of kind takes, walked without
// writing. It is what a length in the resolved form has to say before the
// content it frames is written.
inline int64_t TableRetainInContentBytes( const TableRetainIn & s, uint8_t kind, int64_t length, int32_t depth )
{
    TableRetainIn probe = s;
    probe.out = NULL;
    probe.out_at = 0;
    return TableRetainInContent( probe, kind, length, depth );
}

inline int64_t TableRetainInContent( TableRetainIn & s, uint8_t kind, int64_t length, int32_t depth )
{
    if ( depth > kTableRetainWalkDepthMax ) { return -1; }
    if ( length < 0 || s.at + length > s.size ) { return -1; }
    const int64_t end = s.at + length;
    const int64_t began = s.out_at;
    switch ( kind )
    {
        case 13: // a table BODY: fields, then the zero reference
        {
            for ( ;; )
            {
                uint64_t ref = 0;
                const int64_t mark = s.at;
                if ( !TableRetainInLebRead( s, ref ) ) { return -1; }
                if ( ref == 0 ) { TableRetainInLeb( s, 0 ); break; }
                s.at = mark;
                if ( !TableRetainInRef( s, false ) ) { return -1; }
                if ( s.at >= end ) { return -1; }
                const uint8_t field_kind = s.in[ s.at++ ];
                TableRetainInRaw( s, &field_kind, 1 );
                if ( TableRetainInPayload( s, field_kind, depth + 1 ) < 0 ) { return -1; }
                if ( s.at > end ) { return -1; }
            }
            break;
        }
        case 14: // an ARRAY body: the element kind, the count, then the elements
        {
            if ( s.at >= end ) { return -1; }
            const uint8_t elem_kind = s.in[ s.at++ ];
            TableRetainInRaw( s, &elem_kind, 1 );
            uint64_t n = 0;
            if ( !TableRetainInLebRead( s, n ) ) { return -1; }
            TableRetainInLeb( s, n );
            for ( uint64_t i = 0; i < n; i++ )
            {
                if ( TableRetainInPayload( s, elem_kind, depth + 1 ) < 0 ) { return -1; }
                if ( s.at > end ) { return -1; }
            }
            break;
        }
        case 16: // an ENUM-KEYED body: N triples of a KEY REFERENCE, an L and the element
        {
            if ( s.at >= end ) { return -1; }
            const uint8_t elem_kind = s.in[ s.at++ ];
            TableRetainInRaw( s, &elem_kind, 1 );
            uint64_t n = 0;
            if ( !TableRetainInLebRead( s, n ) ) { return -1; }
            TableRetainInLeb( s, n );
            for ( uint64_t i = 0; i < n; i++ )
            {
                // A KEYED BODY'S KEYS RESOLVE AT EVERY ELEMENT KIND (§6.6, §3.2)
                if ( !TableRetainInRef( s, false ) ) { return -1; }
                uint64_t slot_bytes = 0;
                if ( !TableRetainInLebRead( s, slot_bytes ) ) { return -1; }
                if ( slot_bytes > (uint64_t) ( end - s.at ) ) { return -1; }
                const int64_t resolved = TableRetainInContentBytes( s, elem_kind, (int64_t) slot_bytes, depth + 1 );
                if ( resolved < 0 ) { return -1; }
                TableRetainInLeb( s, (uint64_t) resolved );
                if ( TableRetainInContent( s, elem_kind, (int64_t) slot_bytes, depth + 1 ) < 0 ) { return -1; }
            }
            break;
        }
        case 17: return -1; // A NODE INDEX ANYWHERE DROPS THE WHOLE RECORD (§6.6)
        default:
            // every other content is bytes: a string, wide text, an escape, a
            // payload-free kind, a scalar under a keyed slot's own length
            TableRetainInRaw( s, s.in + s.at, length );
            s.at += length;
            break;
    }
    if ( s.at != end ) { return -1; }
    return s.out_at - began;
}

inline int64_t TableRetainInPayload( TableRetainIn & s, uint8_t kind, int32_t depth )
{
    if ( depth > kTableRetainWalkDepthMax ) { return -1; }
    const int64_t began = s.out_at;
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: // the fixed-width kinds, by width
        case 3: case 7: case 21: case 26:
        case 4: case 8: case 10: case 22: case 27:
        case 5: case 9: case 11: case 23: case 28:
        case 18: case 19: case 24: case 29:
        {
            int64_t width = 1;
            switch ( kind )
            {
                case 3: case 7: case 21: case 26: width = 2; break;
                case 4: case 8: case 10: case 22: case 27: width = 4; break;
                case 5: case 9: case 11: case 23: case 28: width = 8; break;
                case 18: case 19: case 24: case 29: width = 16; break;
                default: width = 1; break;
            }
            if ( s.at + width > s.size ) { return -1; }
            TableRetainInRaw( s, s.in + s.at, width );
            s.at += width;
            break;
        }
        case 12: case 31: case 32: case 33: // L, then L bytes, nothing framed inside
        {
            uint64_t length = 0;
            if ( !TableRetainInLebRead( s, length ) ) { return -1; }
            if ( length > (uint64_t) ( s.size - s.at ) ) { return -1; }
            TableRetainInLeb( s, length );
            TableRetainInRaw( s, s.in + s.at, (int64_t) length );
            s.at += (int64_t) length;
            break;
        }
        case 13: case 14: case 16: // L, then a body the walk resolves
        {
            uint64_t length = 0;
            if ( !TableRetainInLebRead( s, length ) ) { return -1; }
            if ( length > (uint64_t) ( s.size - s.at ) ) { return -1; }
            const int64_t resolved = TableRetainInContentBytes( s, kind, (int64_t) length, depth + 1 );
            if ( resolved < 0 ) { return -1; }
            TableRetainInLeb( s, (uint64_t) resolved );
            if ( TableRetainInContent( s, kind, (int64_t) length, depth + 1 ) < 0 ) { return -1; }
            break;
        }
        case 15: // a UNION: the arm id reference, and when it is not zero its kind, L and payload
        {
            const int64_t mark = s.at;
            uint64_t arm = 0;
            if ( !TableRetainInLebRead( s, arm ) ) { return -1; }
            s.at = mark;
            if ( !TableRetainInRef( s, true ) ) { return -1; }
            if ( arm == 0 ) { break; }
            if ( s.at >= s.size ) { return -1; }
            const uint8_t arm_kind = s.in[ s.at++ ];
            TableRetainInRaw( s, &arm_kind, 1 );
            uint64_t length = 0;
            if ( !TableRetainInLebRead( s, length ) ) { return -1; }
            if ( length > (uint64_t) ( s.size - s.at ) ) { return -1; }
            const int64_t resolved = TableRetainInContentBytes( s, arm_kind, (int64_t) length, depth + 1 );
            if ( resolved < 0 ) { return -1; }
            TableRetainInLeb( s, (uint64_t) resolved );
            if ( TableRetainInContent( s, arm_kind, (int64_t) length, depth + 1 ) < 0 ) { return -1; }
            break;
        }
        case 30: // an ENUM's variant reference, zero for None
        {
            if ( !TableRetainInRef( s, true ) ) { return -1; }
            break;
        }
        case 17: return -1; // A NODE INDEX (§3.1): the whole record goes with it
        default: return -1; // a kind this walk cannot frame
    }
    return s.out_at - began;
}

// ---- CAPTURE: the load side (§6.6) ----
//
// The field is skipped by its framing exactly as it always was and counted
// unknown exactly as it always was, so a full buffer degrades to the default
// behavior one field at a time. False is what r.skip( kind ) answers false
// for, and nothing else: retention can lose a field, it can never turn a good
// read into a bad one.
inline bool TableRetainCapture( TableRetain * retain, TableReader & r, const TableRetainPath & path,
                                uint64_t field_id, uint8_t kind )
{
    const int64_t start = r.offset;
    if ( !r.skip( kind ) ) { return false; }
    if ( retain == NULL ) { return true; }
    const int64_t wire_bytes = r.offset - start;

    TableRetainIn probe;
    probe.in = r.buffer + start;
    probe.size = wire_bytes;
    probe.at = 0;
    probe.ids = r.ids;
    probe.out = NULL;
    probe.out_at = 0;
    const int64_t payload = TableRetainInPayload( probe, kind, 0 );
    if ( payload < 0 || probe.at != wire_bytes ) { r.report->retain_lost++; return true; }

    const int64_t need = kTableRetainRecordHeader + 8 * (int64_t) path.depth + payload;
    if ( need > 0xFFFFFFFFll || retain->used + need > retain->capacity )
    {
        // REFUSAL IS PER RECORD AND NEVER PARTIAL: the buffer never holds a
        // truncated field, and the read continues (§6.6)
        r.report->retain_lost++;
        return true;
    }
    uint8_t * record = retain->bytes + retain->used;
    TableRetainWrite32( record, (uint32_t) need );
    TableRetainWrite32( record + 4, path.node );
    TableRetainWrite32( record + 8, (uint32_t) path.depth );
    TableRetainWrite32( record + 12, (uint32_t) payload );
    TableRetainWrite64( record + 16, field_id );
    record[24] = kind;
    record[25] = 0;
    for ( int32_t i = 0; i < path.depth; i++ )
    {
        TableRetainWrite32( record + kTableRetainRecordHeader + 8 * i, path.steps[i].ordinal );
        TableRetainWrite32( record + kTableRetainRecordHeader + 8 * i + 4, path.steps[i].index );
    }
    TableRetainIn write;
    write.in = r.buffer + start;
    write.size = wire_bytes;
    write.at = 0;
    write.ids = r.ids;
    write.out = record + kTableRetainRecordHeader + 8 * (int64_t) path.depth;
    write.out_at = 0;
    if ( TableRetainInPayload( write, kind, 0 ) < 0 ) { r.report->retain_lost++; return true; }
    retain->used += need;
    retain->count++;
    r.report->retained++;
    return true;
}

// LoadRetain RESETS BOTH STORES and writes into neither list (§6.6): a
// retained record carries its field's identity in the record itself, with
// every reference resolved.
inline void TableRetainReset( TableRetain * retain, const TableNodeMap & nodes, const uint8_t * region )
{
    if ( retain == NULL ) { return; }
    retain->used = 0;
    retain->id_used = 0;
    retain->count = 0;
    retain->base = region;
    retain->directory = nodes.entries;
    retain->directory_count = nodes.count;
}

// ---- EMIT: the save side (§6.6) ----
//
// The record read back the other way: every resolved id becomes the reference
// the trailer being written gives it, and every length is recomputed against
// the references' new widths. The walk is the capture's mirror and the same
// damage rules apply, except that damage cannot be met — these bytes are the
// reader's own.
struct TableRetainOut
{
    const uint8_t * in;
    int64_t size;
    int64_t at;
    TableRetainIds * ids;
    TableWriter * w; // NULL: measuring
    int64_t bytes;
};

inline void TableRetainOutRaw( TableRetainOut & s, const uint8_t * from, int64_t bytes )
{
    if ( s.w != NULL ) { s.w->raw( from, bytes ); }
    s.bytes += bytes;
}

inline void TableRetainOutLeb( TableRetainOut & s, uint64_t v )
{
    if ( s.w != NULL ) { s.w->putleb( v ); }
    s.bytes += TableLebBytes( v );
}

inline bool TableRetainOutLebRead( TableRetainOut & s, uint64_t & value )
{
    value = 0;
    uint32_t shift = 0;
    for ( int32_t i = 0; i < 10; i++ )
    {
        if ( s.at >= s.size ) { return false; }
        const uint8_t b = s.in[ s.at++ ];
        value |= uint64_t( b & 0x7F ) << shift;
        if ( ( b & 0x80 ) == 0 ) { return true; }
        shift += 7;
    }
    return false;
}

inline bool TableRetainOutRef( TableRetainOut & s )
{
    if ( s.at + 8 > s.size ) { return false; }
    const uint64_t id = TableRetainRead64( s.in + s.at );
    s.at += 8;
    if ( id == 0 ) { TableRetainOutLeb( s, 0 ); return true; } // the wire's own no-id
    const uint64_t ref = s.ids->record_ref( id );
    if ( s.ids->lost || s.ids->overflow ) { return false; }
    TableRetainOutLeb( s, ref );
    return true;
}

inline bool TableRetainOutPayload( TableRetainOut & s, uint8_t kind, int32_t depth );
inline bool TableRetainOutContent( TableRetainOut & s, uint8_t kind, int64_t length, int32_t depth );

// the WIRE byte count a resolved CONTENT takes. The ids it names are interned
// on the way past, which is what makes measure and save one walk in two
// readings, exactly as every other body on this wire is.
inline int64_t TableRetainOutContentBytes( const TableRetainOut & s, uint8_t kind, int64_t length, int32_t depth )
{
    TableRetainOut probe = s;
    probe.w = NULL;
    probe.bytes = 0;
    if ( !TableRetainOutContent( probe, kind, length, depth ) ) { return -1; }
    return probe.bytes;
}

inline bool TableRetainOutContent( TableRetainOut & s, uint8_t kind, int64_t length, int32_t depth )
{
    if ( depth > kTableRetainWalkDepthMax ) { return false; }
    if ( length < 0 || s.at + length > s.size ) { return false; }
    const int64_t end = s.at + length;
    switch ( kind )
    {
        case 13:
        {
            for ( ;; )
            {
                if ( s.at + 8 > end ) { return false; }
                const uint64_t id = TableRetainRead64( s.in + s.at );
                if ( id == 0 ) { s.at += 8; TableRetainOutLeb( s, 0 ); break; }
                if ( !TableRetainOutRef( s ) ) { return false; }
                if ( s.at >= end ) { return false; }
                const uint8_t field_kind = s.in[ s.at++ ];
                TableRetainOutRaw( s, &field_kind, 1 );
                if ( !TableRetainOutPayload( s, field_kind, depth + 1 ) ) { return false; }
            }
            break;
        }
        case 14:
        {
            if ( s.at >= end ) { return false; }
            const uint8_t elem_kind = s.in[ s.at++ ];
            TableRetainOutRaw( s, &elem_kind, 1 );
            uint64_t n = 0;
            if ( !TableRetainOutLebRead( s, n ) ) { return false; }
            TableRetainOutLeb( s, n );
            for ( uint64_t i = 0; i < n; i++ )
            {
                if ( !TableRetainOutPayload( s, elem_kind, depth + 1 ) ) { return false; }
            }
            break;
        }
        case 16:
        {
            if ( s.at >= end ) { return false; }
            const uint8_t elem_kind = s.in[ s.at++ ];
            TableRetainOutRaw( s, &elem_kind, 1 );
            uint64_t n = 0;
            if ( !TableRetainOutLebRead( s, n ) ) { return false; }
            TableRetainOutLeb( s, n );
            for ( uint64_t i = 0; i < n; i++ )
            {
                if ( !TableRetainOutRef( s ) ) { return false; }
                uint64_t slot_bytes = 0;
                if ( !TableRetainOutLebRead( s, slot_bytes ) ) { return false; }
                const int64_t wire = TableRetainOutContentBytes( s, elem_kind, (int64_t) slot_bytes, depth + 1 );
                if ( wire < 0 ) { return false; }
                TableRetainOutLeb( s, (uint64_t) wire );
                if ( !TableRetainOutContent( s, elem_kind, (int64_t) slot_bytes, depth + 1 ) ) { return false; }
            }
            break;
        }
        default:
            TableRetainOutRaw( s, s.in + s.at, length );
            s.at += length;
            break;
    }
    return s.at == end;
}

inline bool TableRetainOutPayload( TableRetainOut & s, uint8_t kind, int32_t depth )
{
    if ( depth > kTableRetainWalkDepthMax ) { return false; }
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25:
        case 3: case 7: case 21: case 26:
        case 4: case 8: case 10: case 22: case 27:
        case 5: case 9: case 11: case 23: case 28:
        case 18: case 19: case 24: case 29:
        {
            int64_t width = 1;
            switch ( kind )
            {
                case 3: case 7: case 21: case 26: width = 2; break;
                case 4: case 8: case 10: case 22: case 27: width = 4; break;
                case 5: case 9: case 11: case 23: case 28: width = 8; break;
                case 18: case 19: case 24: case 29: width = 16; break;
                default: width = 1; break;
            }
            if ( s.at + width > s.size ) { return false; }
            TableRetainOutRaw( s, s.in + s.at, width );
            s.at += width;
            break;
        }
        case 12: case 31: case 32: case 33:
        {
            uint64_t length = 0;
            if ( !TableRetainOutLebRead( s, length ) ) { return false; }
            if ( length > (uint64_t) ( s.size - s.at ) ) { return false; }
            TableRetainOutLeb( s, length );
            TableRetainOutRaw( s, s.in + s.at, (int64_t) length );
            s.at += (int64_t) length;
            break;
        }
        case 13: case 14: case 16:
        {
            uint64_t length = 0;
            if ( !TableRetainOutLebRead( s, length ) ) { return false; }
            if ( length > (uint64_t) ( s.size - s.at ) ) { return false; }
            const int64_t wire = TableRetainOutContentBytes( s, kind, (int64_t) length, depth + 1 );
            if ( wire < 0 ) { return false; }
            TableRetainOutLeb( s, (uint64_t) wire );
            if ( !TableRetainOutContent( s, kind, (int64_t) length, depth + 1 ) ) { return false; }
            break;
        }
        case 15:
        {
            if ( s.at + 8 > s.size ) { return false; }
            const uint64_t arm = TableRetainRead64( s.in + s.at );
            if ( !TableRetainOutRef( s ) ) { return false; }
            if ( arm == 0 ) { break; }
            if ( s.at >= s.size ) { return false; }
            const uint8_t arm_kind = s.in[ s.at++ ];
            TableRetainOutRaw( s, &arm_kind, 1 );
            uint64_t length = 0;
            if ( !TableRetainOutLebRead( s, length ) ) { return false; }
            if ( length > (uint64_t) ( s.size - s.at ) ) { return false; }
            const int64_t wire = TableRetainOutContentBytes( s, arm_kind, (int64_t) length, depth + 1 );
            if ( wire < 0 ) { return false; }
            TableRetainOutLeb( s, (uint64_t) wire );
            if ( !TableRetainOutContent( s, arm_kind, (int64_t) length, depth + 1 ) ) { return false; }
            break;
        }
        case 30:
        {
            if ( !TableRetainOutRef( s ) ) { return false; }
            break;
        }
        default: return false;
    }
    return true;
}

// ---- THE RETAINED TAIL: where the records go back (§6.6) ----
//
// AT THE END OF THEIR OWN BODY, IN THE ORDER RETAINED. Position carries
// nothing on this wire, so appending is chosen for three properties: it is a
// write with no splice, the retained order is preserved, and the result is
// IDEMPOTENT after the first save.
//
// A RETAINED ID PAST THE CAPACITY COUNTS ONE retain_lost AND ITS RECORD IS
// DROPPED, and the save is never refused. MeasureRetain and SaveRetain drop
// the same records under the same walk, so the measure sees the same overflow
// and its answer is the size the save writes.

// one record's WIRE bytes under the trailer being written, and -1 for a record
// this save cannot place: an id the caller's list had no room for, or a
// resolved form the walk cannot read back. The ids it names are interned on
// the way past, which is what makes measure and save one rule read twice.
inline int64_t TableRetainRecordWire( const uint8_t * record, TableRetainIds & ids, uint64_t & ref )
{
    const int32_t mark = ids.count;
    ids.lost = false;
    ref = ids.record_ref( TableRetainRecordId( record ) );
    if ( !ids.lost && !ids.overflow )
    {
        TableRetainOut s;
        s.in = TableRetainRecordPayload( record );
        s.size = TableRetainRecordPayloadBytes( record );
        s.at = 0;
        s.ids = &ids;
        s.w = NULL;
        s.bytes = 0;
        if ( TableRetainOutPayload( s, TableRetainRecordKind( record ), 0 ) && s.at == s.size )
        {
            return TableLebBytes( ref ) + 1 + s.bytes;
        }
    }
    // the record is not written at all, and nothing else about the save
    // changes: a full id list degrades to the default behavior one record at a
    // time, and the entries this attempt took are given back
    ids.truncate( mark );
    ids.lost = false;
    return -1;
}

inline int64_t TableRetainTailMeasure( const TableRetain * retain, TableRetainIds & ids, const TableRetainPath & path )
{
    if ( retain == NULL || retain->bytes == NULL ) { return 0; }
    int64_t bytes = 0;
    int64_t at = 0;
    for ( int32_t k = 0; k < retain->count; k++ )
    {
        const uint8_t * record = retain->bytes + at;
        at += TableRetainRecordBytes( record );
        if ( !TableRetainRecordHere( *retain, record, path ) ) { continue; }
        uint64_t ref = 0;
        const int64_t wire = TableRetainRecordWire( record, ids, ref );
        if ( wire < 0 ) { continue; }
        bytes += wire;
    }
    return bytes;
}

inline bool TableRetainTailSave( TableRetain * retain, TableRetainIds & ids, TableWriter & w, const TableRetainPath & path )
{
    if ( retain == NULL || retain->bytes == NULL ) { return true; }
    int64_t at = 0;
    for ( int32_t k = 0; k < retain->count; k++ )
    {
        uint8_t * record = retain->bytes + at;
        at += TableRetainRecordBytes( record );
        if ( !TableRetainRecordHere( *retain, record, path ) ) { continue; }
        uint64_t ref = 0;
        if ( TableRetainRecordWire( record, ids, ref ) < 0 ) { continue; }
        w.putleb( ref );
        w.put8( TableRetainRecordKind( record ) );
        TableRetainOut s;
        s.in = TableRetainRecordPayload( record );
        s.size = TableRetainRecordPayloadBytes( record );
        s.at = 0;
        s.ids = &ids;
        s.w = &w;
        s.bytes = 0;
        if ( !TableRetainOutPayload( s, TableRetainRecordKind( record ), 0 ) ) { return false; }
        record[25] = 1; // PLACED: the one mark the save leaves on the buffer
    }
    return !w.overflow;
}

// THE SAVE'S OWN SHARE OF retain_lost, counted ONCE and read after the save
// (§6.6): every record the walk did not place. A record whose path no longer
// names a body, one the caller's id list had no room for, and one the walk
// could not read back are one number here, because the check a caller reads is
// one number. A record is marked as it is written, so this cannot double-count
// a body measured twice.
inline void TableRetainClearPlaced( TableRetain * retain )
{
    if ( retain == NULL || retain->bytes == NULL ) { return; }
    int64_t at = 0;
    for ( int32_t k = 0; k < retain->count; k++ )
    {
        retain->bytes[ at + 25 ] = 0;
        at += TableRetainRecordBytes( retain->bytes + at );
    }
}

inline void TableRetainCountLost( const TableRetain * retain, TableReport * report )
{
    if ( retain == NULL || retain->bytes == NULL || report == NULL ) { return; }
    int64_t at = 0;
    for ( int32_t k = 0; k < retain->count; k++ )
    {
        const uint8_t * record = retain->bytes + at;
        at += TableRetainRecordBytes( record );
        if ( !TableRetainRecordPlaced( record ) ) { report->retain_lost++; }
    }
}

// THE NODE TABLE under retention (§3.1, §6.6): the same fill rule the plain
// pair derives, with the retain family's ids and each record's own body
// reached through a dispatch the CALL supplies rather than a second pair of
// thunks on the numbering — a store per node on the PLAIN save path would be a
// cost this feature is not allowed to have.
template <typename Ctx, typename Measure>
inline int64_t TableNodeTablePayloadRetain( const Ctx & ctx, TableRetainIds & ids, const TableNumbering & n,
                                            TableRetain * retain, Measure measure )
{
    int64_t payload = TableLebBytes( (uint64_t) n.count );
    for ( int64_t k = 0; k < n.count; k++ )
    {
        payload += TableLebBytes( ids.ref( n.entries[k].type_id ) );
        const int64_t body = measure( ctx, n, ids, n.entries[k].type_id, n.entries[k].node, retain );
        if ( body < 0 ) { return -1; }
        payload += TableLebBytes( (uint64_t) body ) + body;
    }
    return payload;
}

template <typename Ctx, typename Measure>
inline int64_t TableNodeTableMeasureRetain( const Ctx & ctx, TableRetainIds & ids, const TableNumbering & n,
                                            TableRetain * retain, Measure measure )
{
    if ( n.count == 0 ) { return 0; }
    const uint64_t ref = ids.ref( kTableNodeTableFieldId );
    const int64_t payload = TableNodeTablePayloadRetain( ctx, ids, n, retain, measure );
    if ( payload < 0 ) { return -1; }
    return TableLebBytes( ref ) + 1 + TableLebBytes( (uint64_t) payload ) + payload;
}

template <typename Ctx, typename Measure, typename Save>
inline bool TableNodeTableSaveRetain( const Ctx & ctx, TableWriter & w, TableRetainIds & ids, const TableNumbering & n,
                                      TableRetain * retain, Measure measure, Save save )
{
    if ( n.count == 0 ) { return true; }
    const uint64_t ref = ids.ref( kTableNodeTableFieldId );
    const int64_t payload = TableNodeTablePayloadRetain( ctx, ids, n, retain, measure );
    if ( payload < 0 ) { return false; }
    w.putleb( ref );
    w.put8( 12 ); // kind 12 is the opaque byte payload, exactly as the plain save writes it
    w.putleb( (uint64_t) payload );
    w.putleb( (uint64_t) n.count );
    for ( int64_t k = 0; k < n.count; k++ )
    {
        w.putleb( ids.ref( n.entries[k].type_id ) );
        const int64_t body = measure( ctx, n, ids, n.entries[k].type_id, n.entries[k].node, retain );
        if ( body < 0 ) { return false; }
        w.putleb( (uint64_t) body );
        if ( !save( ctx, n, w, ids, n.entries[k].type_id, n.entries[k].node, retain ) ) { return false; }
    }
    return true;
}
`

// ---- the emitted surface (docs/SPEC-TABLES.md §6.6) ----

// setOwner fixes the closure member whose codec is being emitted, and with it
// the FIELD ORDINALS a path step names: the field's position in its body's own
// DECLARATION ORDER, which is the reader's order and not the wire's.
func (g *tableGen) setOwner(st *ir.Struct) {
	g.owner = st
	g.ordinal = map[*ir.Field]int{}
	if st != nil {
		for i, f := range st.Fields {
			g.ordinal[f] = i
		}
	}
	g.pathExpr = "path"
	g.elemIndex = "0"
	g.unionField = nil
}

// emitRetainDeclarations forward-declares the retain family, which is mutually
// recursive across a unit's files exactly as the plain one is.
func (g *tableGen) emitRetainDeclarations(members []*ir.Struct) {
	g.pf("// ---- retain-unknown: the second family (docs/SPEC-TABLES.md §6.6) ----\n")
	g.pf("//\n")
	g.pf("// The same walks, with the PATH threaded and the unknown arm capturing. The\n")
	g.pf("// three above are untouched and cost nothing for these being here: a caller\n")
	g.pf("// that does not ask instantiates none of them.\n\n")
	for _, st := range members {
		if g.isVar(st.Name) {
			g.pf("template <typename Ctx> inline int64_t %sMeasureBodyRetain( const Ctx & ctx, const TableNumbering & numbering, TableRetainIds & ids, const %s & value, TableRetain * retain, const TableRetainPath & path );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveBodyFieldsRetain( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, TableRetainIds & ids, const %s & value, TableRetain * retain, const TableRetainPath & path );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveBodyRetain( const Ctx & ctx, const TableNumbering & numbering, TableWriter & w, TableRetainIds & ids, const %s & value, TableRetain * retain, const TableRetainPath & path );\n", st.Name, st.Name)
			g.pf("inline bool %sLoadBodyRetain( TableReader & r, const TableNodeMap & nodes, %s & value, TableRetain * retain, const TableRetainPath & path );\n", st.Name, st.Name)
			continue
		}
		g.pf("inline int64_t %sMeasureBodyRetain( TableRetainIds & ids, const %s & value, TableRetain * retain, const TableRetainPath & path );\n", st.Name, st.Name)
		g.pf("%s bool %sSaveBodyRetain( TableWriter & w, TableRetainIds & ids, const %s & value, TableRetain * retain, const TableRetainPath & path );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
		g.pf("%s bool %sLoadBodyRetain( TableReader & r, %s & value, TableRetain * retain, const TableRetainPath & path );\n", tableInlineMacro(g.unit.Package), st.Name, st.Name)
	}
	g.pf("\n")
}

// emitRetainBodies writes the family itself: the same field emitters, under
// the retain names.
func (g *tableGen) emitRetainBodies(members []*ir.Struct) {
	g.retain = true
	for _, st := range members {
		g.setOwner(st)
		g.emitTableMeasure(st)
		g.emitTableWrite(st)
		g.emitTableRead(st)
	}
	g.retain = false
	g.setOwner(nil)
}

// emitRetainRoot writes one variable-class root's three verbs: LoadRetain,
// MeasureRetain and SaveRetain (docs/SPEC-TABLES.md §6.6).
func (g *tableGen) emitRetainRoot(st *ir.Struct) {
	n := st.Name
	reachable := ir.PointerReachable(st)
	blobs := reachableBlobs(st)

	// the node dispatch is a LOCAL of each call rather than a second pair of
	// thunks on the numbering: a store per node on the plain save path would
	// be a cost this feature is not allowed to have (§6.6, the owner's law)
	dispatch := func(ind string) {
		g.pf("%sauto retain_measure = []( const Ctx & c, const TableNumbering & nn, TableRetainIds & ii, uint64_t type_id, const void * node, TableRetain * rt ) -> int64_t\n%s{\n", ind, ind)
		g.pf("%s    const TableRetainPath at = TableRetainPathRoot( node, 0 );\n", ind)
		g.pf("%s    (void) c; (void) nn; (void) ii; (void) rt; (void) at;\n", ind)
		g.pf("%s    switch ( type_id )\n%s    {\n", ind, ind)
		for _, t := range reachable {
			if g.isVar(t.Name) {
				g.pf("%s        case 0x%016xull: return %sMeasureBodyRetain( c, nn, ii, *(const %s *) node, rt, at ); // %s\n", ind, ir.TableWireId(t.WireName()), t.Name, t.Name, t.Name)
				continue
			}
			g.pf("%s        case 0x%016xull: return %sMeasureBodyRetain( ii, *(const %s *) node, rt, at ); // %s\n", ind, ir.TableWireId(t.WireName()), t.Name, t.Name, t.Name)
		}
		for _, b := range blobs {
			g.pf("%s        case %s: return (int64_t) ( (const TableBlob *) node )->length;\n", ind, b.constant)
		}
		g.pf("%s        default: break;\n%s    }\n%s    return -1;\n%s};\n", ind, ind, ind, ind)
	}
	saveDispatch := func(ind string) {
		g.pf("%sauto retain_save = []( const Ctx & c, const TableNumbering & nn, TableWriter & ww, TableRetainIds & ii, uint64_t type_id, const void * node, TableRetain * rt ) -> bool\n%s{\n", ind, ind)
		g.pf("%s    const TableRetainPath at = TableRetainPathRoot( node, 0 );\n", ind)
		g.pf("%s    (void) c; (void) nn; (void) ww; (void) ii; (void) rt; (void) at;\n", ind)
		g.pf("%s    switch ( type_id )\n%s    {\n", ind, ind)
		for _, t := range reachable {
			if g.isVar(t.Name) {
				g.pf("%s        case 0x%016xull: return %sSaveBodyRetain( c, nn, ww, ii, *(const %s *) node, rt, at ); // %s\n", ind, ir.TableWireId(t.WireName()), t.Name, t.Name, t.Name)
				continue
			}
			g.pf("%s        case 0x%016xull: return %sSaveBodyRetain( ww, ii, *(const %s *) node, rt, at ); // %s\n", ind, ir.TableWireId(t.WireName()), t.Name, t.Name, t.Name)
		}
		for _, b := range blobs {
			g.pf("%s        case %s:\n%s        {\n", ind, b.constant, ind)
			g.pf("%s            const TableBlob * blob = (const TableBlob *) node;\n", ind)
			g.pf("%s            ww.raw( (const void *) ( blob + 1 ), (int64_t) blob->length );\n%s            return true;\n%s        }\n", ind, ind, ind)
		}
		g.pf("%s        default: break;\n%s    }\n%s    return false;\n%s};\n", ind, ind, ind, ind)
	}

	g.retain = true
	g.emitRootNodeBody(st, reachable, blobs)
	g.emitVariableLoad(st)
	g.retain = false

	g.pf("// %sMeasureRetain and %sSaveRetain: the pair, with the retained tail in\n", n, n)
	g.pf("// every body it belongs to (docs/SPEC-TABLES.md §6.6). They drop the same\n")
	g.pf("// records under the same walk, so Measure's answer is the size the save\n")
	g.pf("// writes even where a record could not be placed.\n")
	g.pf("template <typename Ctx>\ninline int64_t %sMeasureWireRetain( const Ctx & ctx, const %s & root, TableRetain * retain, TableAllocator allocator )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    int64_t bytes = -1;\n")
	dispatch("    ")
	g.pf("    if ( %sNumberFrom( ctx, numbering, root ) )\n    {\n", n)
	g.pf("        TableRetainIds ids( retain );\n")
	g.pf("        if ( retain != NULL ) { retain->id_used = 0; } // one walk fills the list, and the save's own walk refills it\n")
	g.pf("        bytes = %sMeasureBodyRetain( ctx, numbering, ids, root, retain, TableRetainPathRoot( (const void *) &root, 1 ) );\n", n)
	g.pf("        if ( bytes >= 0 )\n        {\n")
	g.pf("            const int64_t table = TableNodeTableMeasureRetain( ctx, ids, numbering, retain, retain_measure );\n")
	g.pf("            bytes = table < 0 || ids.overflow ? -1 : 1 + bytes + table + TableRetainIdsBytes( ids );\n")
	g.pf("        }\n    }\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    return bytes;\n}\n\n")

	g.pf("template <typename Ctx>\ninline int64_t %sSaveWireRetain( const Ctx & ctx, const %s & root, TableRetain * retain, uint8_t * buffer, int64_t capacity, TableReport * report )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, TableDefaultAllocator() );\n")
	dispatch("    ")
	saveDispatch("    ")
	g.pf("    if ( !%sNumberFrom( ctx, numbering, root ) ) { TableNumberingShutdown( numbering ); return -1; }\n", n)
	g.pf("    TableWriter w( buffer, capacity );\n")
	g.pf("    TableRetainIds ids( retain );\n")
	g.pf("    if ( retain != NULL ) { retain->id_used = 0; }\n")
	g.pf("    TableRetainClearPlaced( retain );\n")
	g.pf("    w.put8( kTableWireForm ); // the FORM BYTE is the whole header (§3)\n")
	g.pf("    // the root's own fields, then the RETAINED TAIL, then the node table's\n")
	g.pf("    // field: a retained field is one of the root's own values, and the tail\n")
	g.pf("    // is pinned before the large and damage-prone part (§6.6, §3.1)\n")
	g.pf("    bool ok = %sSaveBodyFieldsRetain( ctx, numbering, w, ids, root, retain, TableRetainPathRoot( (const void *) &root, 1 ) ) &&\n", n)
	g.pf("              TableNodeTableSaveRetain( ctx, w, ids, numbering, retain, retain_measure, retain_save );\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    if ( !ok || ids.overflow ) { return -1; }\n")
	g.pf("    w.put8( 0 ); // the ZERO REFERENCE that ends the root body\n")
	g.pf("    TableRetainIdsWrite( w, ids );\n")
	g.pf("    if ( w.overflow ) { return -1; } // the caller's buffer was too small\n")
	g.pf("    // THE SAVE'S OWN SHARE OF retain_lost, read after the save (§6.6): every\n")
	g.pf("    // record the walk did not place, counted once.\n")
	g.pf("    TableRetainCountLost( retain, report );\n")
	g.pf("    return w.offset;\n}\n\n")

	g.pf("inline int64_t %sMeasureRetain( const %s * root, TableRetain * retain, TableAllocator allocator = TableDefaultAllocator() )\n{\n", n, n)
	g.pf("    if ( root == NULL ) { return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    return %sMeasureWireRetain( ctx, *root, retain, allocator );\n}\n\n", n)

	g.pf("// SaveRetain REFUSES A NULL REPORT and returns -1 (docs/SPEC-TABLES.md\n")
	g.pf("// §6.6): the save is the only place a caller learns that a record was\n")
	g.pf("// dropped, so the report is required here where it is optional everywhere\n")
	g.pf("// else. A surface that let a caller retain, save and never find out would\n")
	g.pf("// be a promise it could not check.\n")
	g.pf("inline int64_t %sSaveRetain( const %s * root, TableRetain * retain, uint8_t * buffer, int64_t capacity, TableReport * report )\n{\n", n, n)
	g.pf("    if ( root == NULL || report == NULL ) { return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    return %sSaveWireRetain( ctx, *root, retain, buffer, capacity, report );\n}\n\n", n)
}

// emitRetainRefusals is RETENTION IS THE VARIABLE CLASS'S, AND A FIXED-CLASS
// ROOT GETS NONE (docs/SPEC-TABLES.md §6.6). A fixed-class root is a value: it
// has no region and no node directory, so the path's first step names nothing
// and the anchor the round trip rests on does not exist.
//
// The refusal is IN THE SOURCE THE UNIT DOES EMIT rather than a missing symbol
// (§11's rule for a surface a class does not carry): the three names are
// declared, and naming one is a compile error that says why.
func (g *tableGen) emitRetainRefusals(members []*ir.Struct) {
	first := true
	for _, st := range members {
		if g.isVar(st.Name) || st.IsMapEntry() {
			continue
		}
		if first {
			g.pf("// ---- retain-unknown on a FIXED-class root: refused by name (§6.6) ----\n")
			g.pf("//\n")
			g.pf("// A fixed-class root is a VALUE: no region, no node directory, and so no\n")
			g.pf("// anchor for a retained record's path. The three names are declared here so\n")
			g.pf("// that naming one is a refusal that says why, rather than a symbol a linker\n")
			g.pf("// could not find. The suffixes stay claimed on every closure member all the\n")
			g.pf("// same (§11): a table gains or loses pointers as an edit, and a name that is\n")
			g.pf("// free today must not become a collision tomorrow.\n\n")
			first = false
		}
		why := fmt.Sprintf("%s is a FIXED-class root (docs/SPEC-TABLES.md §6.1): it is a value with no region and no node directory, so retain-unknown has no anchor for a path's first step. LoadRetain, MeasureRetain and SaveRetain are refused by name on a fixed-class root (§6.6). Retention is a REGION round trip: load a variable-class root, or save without it.", st.Name)
		for _, verb := range []string{"LoadRetain", "MeasureRetain", "SaveRetain"} {
			g.pf("template <typename... Args>\ninline void %s%s( Args &&... )\n{\n", st.Name, verb)
			g.pf("    static_assert( sizeof...( Args ) == (size_t) -1,\n        \"%s\" );\n}\n\n", why)
		}
	}
}
