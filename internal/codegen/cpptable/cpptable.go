// Package cpptable emits <Base>Table.h — the TABLE-wire C++ codecs
// (docs/SPEC-TABLES.md). One header per unit file, emitted only when the unit
// declares tables: storage structs for the `table` declarations, then
// measure/save/load codecs and reflection descriptors for the whole
// TABLE CLOSURE (every table plus everything one references, transitively).
//
// The wire is neutral, evolution-tolerant TLV: field identity is the name
// hash, unknown fields skip, absent fields default, changed kinds skip
// (never misdecode), out-of-range values clamp, framing damage stops the
// decode with a partial result — and every event lands in the TableReport.
// Plain byte code with no serialize dependency — a unit whose closure
// declares a 128-bit field takes exactly one thing from serialize.h, the
// 128-bit storage type — so a Table header is includable from any
// translation unit; the encode surface is a
// measure/save split, so a caller can measure nested tables in parallel,
// prefix-sum offsets, and scatter-write disjoint ranges from N workers.
// Generated codecs allocate nothing: the caller owns every buffer.
package cpptable

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (docs/SPEC-TABLES.md §3), named locally over the one
// target-independent definition in ir — the vocabulary is wire law, and the
// baseline projection reads the same mapping this emitter writes.
const (
	tkBool   = ir.TableKindBool
	tkI8     = ir.TableKindI8
	tkI16    = ir.TableKindI16
	tkI32    = ir.TableKindI32
	tkI64    = ir.TableKindI64
	tkU8     = ir.TableKindU8
	tkU16    = ir.TableKindU16
	tkU32    = ir.TableKindU32
	tkU64    = ir.TableKindU64
	tkF32    = ir.TableKindF32
	tkF64    = ir.TableKindF64
	tkString = ir.TableKindString
	tkTable  = ir.TableKindTable
	tkArray  = ir.TableKindArray
	tkUnion  = ir.TableKindUnion
	// an ENUM-KEYED array body is its OWN kind (docs/SPEC-TABLES.md §3.2): the
	// positional array body and the keyed one are incompatible, so a reader
	// meeting the other must see a KIND MISMATCH and skip, never misdecode.
	tkKeyed = ir.TableKindKeyed
	// a POINTER field's kind: a u32 NODE INDEX into the flat node table
	// (docs/SPEC-TABLES.md §3.1), distinct from kind 13 so that an edit between
	// a by-value nesting and a pointer is an ordinary kind mismatch.
	tkNodeIndex = ir.TableKindPointer
	// the scalars the TYPE wire carries — the 128-bit integers and the
	// fixed-point family, kinds 18-29 (docs/SPEC-TABLES.md §3) — are reached
	// through ir.TableScalarKind and ir.TableKindWidth and never named here:
	// every path that handles them dispatches on the kind's WIDTH.
)

func tableScalarKind(f *ir.Field) int { return ir.TableWireScalarKind(f) }

// the three kinds this form added (docs/SPEC-TABLES.md §3): an ENUM rides
// under its own kind carrying the reference to its variant name's id, the
// ESCAPE is how a later major adds a kind without the addition reading as
// damage, and the PAYLOAD-FREE kind is what an arm that holds nothing rides
// under, because an arm header carries a kind.
const (
	tkEnum      = ir.TableKindEnum
	tkNoPayload = ir.TableKindNoPayload
)

// tableFieldWireId is a field's effective id on this wire: fnv1a64 of its
// `was` alias where one is declared, and of its own name otherwise (§5).
func tableFieldWireId(f *ir.Field) uint64 { return ir.TableFieldWireId(f) }

func tableKindWidth(kind int) int { return ir.TableKindWidth(kind) }

func tablePut(width int) string { return fmt.Sprintf("put%d", width*8) }
func tableGet(width int) string { return fmt.Sprintf("get%d", width*8) }

type tableGen struct {
	unit        *ir.Unit
	file        *ir.File
	anyVariable bool // the unit declares at least one variable-length table
	anyKeyed    bool // the unit declares at least one enum-keyed array
	// anyMap is the unit declaring at least one `map[K]V` (docs/SPEC-TABLES.md
	// §2.8). It gates the map runtime and every map-shaped walk, so not one
	// symbol of the machinery reaches a map-free unit's header (§2.2).
	anyMap bool
	// anyList is the unit declaring at least one `[]T` (docs/SPEC-TABLES.md
	// §2.9). It gates the list runtime and the builder's three the same way.
	anyList bool
	// anyExtent is either: the unit carries the NODE EXTENT machinery both
	// constructs share: the carve, the framing walk, the extent walks and the
	// refusal reason (§2.8, §2.9, §6.5). A unit with neither carries none of it.
	anyExtent bool
	// blocks is the unit's BLOCK FORM surface (docs/SPEC-TABLES.md §19), nil when
	// no table is marked `| block`. Nil is what makes the zero-cost gate
	// answerable by asking one question (§2.2).
	blocks         *ir.BlockUnit
	owner          *ir.Struct      // the closure member whose codec is being emitted
	variable       map[string]bool // the derived VARIABLE-LENGTH members (ir.VariableTables)
	targets        map[string]bool // tables some pointer targets (ir.PointerTargets)
	body           strings.Builder
	includes       map[string]bool // referenced files -> #include "<base>Table.h"
	nativeIncludes map[string]bool // cpp_include headers of mapped types
	indent         string          // extra per-line indent while emitting inside a branch guard
	// slots is the unit's MESSAGE-FORM vocabulary, id -> slot
	// (docs/SPEC-TABLES.md §3.3). Every id a generated field header can name
	// has one, and the slot rides at the header as a LITERAL beside the id, so
	// a form-`2` save does no lookup at all.
	// slots is the unit's MESSAGE-FORM vocabulary: the SLOT each announced
	// entry takes, keyed by the triple (§3.3), so a generated field header
	// carries its reference as a literal and a save does no lookup at all.
	slots map[string]uint64
	// msgDepth is the message codec's nesting depth while it emits: every
	// loop variable and local a nested payload declares carries the depth as
	// a suffix, so an element's decode inside an element's decode shadows
	// nothing (docs/SPEC-TABLES.md §13.9 holds the reference to -Wshadow).
	msgDepth int
}

// wireRef is a field header's id reference: the id and its MESSAGE-FORM SLOT,
// both literals. The FILE form interns the id and answers a first-use
// reference; the MESSAGE form answers the slot and never touches the table
// (docs/SPEC-TABLES.md §3, §3.3).
func (g *tableGen) wireRef(id uint64) string {
	return fmt.Sprintf("ids.ref( 0x%016xull )", id)
}

// hdrBytes is a field header's cost: the id reference and the kind byte, which
// is the whole of it (§3).
func (g *tableGen) hdrBytes(id uint64) string {
	return "TableLebBytes( " + g.wireRef(id) + " ) + 1"
}

// slotOf is one id's message-form slot as a C++ literal.
func (g *tableGen) slotOf(id uint64) string {
	return strconv.FormatUint(g.slots[ir.TableVocabularyEntry{Id: id}.Key()], 10)
}

func (g *tableGen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)
		if trailing {
			s += "\n"
		}
	}
	g.body.WriteString(s)
}

func (g *tableGen) noteRef(name string) {
	if base, ok := g.unit.DeclFile[name]; ok && base != g.file.Base {
		g.includes[base] = true
	}
}

// formatFloat renders a float literal; single-precision literals format at
// FLOAT32 precision, so the emitted clamp bounds and defaults are exactly the
// values the runtime compares against.
func formatFloat(v float64, single bool) string {
	bitSize := 64
	if single {
		bitSize = 32
	}
	s := strconv.FormatFloat(v, 'g', -1, bitSize)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	if single {
		s += "f"
	}
	return s
}

// tableHooks is the C library a generated table header reaches for, and the
// macros that replace it. It is spelled the way serialize.h spells
// serialize_assert: a plain #ifndef the caller wins, and the C header pulled in
// only when the caller supplied nothing.
//
// The names carry NO package scope. A consumer defines them once for the whole
// program, the way it defines serialize_assert once, and every generated header
// in every package picks the definition up.
const tableHooks = `
// ---- the hooks (docs/USAGE.md, "the C++ table runtime's hooks") ----
//
// schema_assert — the runtime's own assert, and the refusal a debugger reads.
// NDEBUG removes it, exactly as it removes assert. A caller who already routes
// serialize's asserts writes ` + "`#define schema_assert serialize_assert`" + ` before
// including this header and both halves land in one handler.
#ifndef schema_assert
#include <assert.h>
#define schema_assert assert
#endif // #ifndef schema_assert

// schema_fatal — what stands after the assert on a path that cannot continue.
// NDEBUG does not remove it. Supply it and <stdlib.h> is never included.
#ifndef schema_fatal
#include <stdlib.h> // abort
#define schema_fatal abort
#endif // #ifndef schema_fatal
`

// tableAllocatorHook is the DEFAULT allocator pair behind the same #ifndef,
// emitted only into a unit that has a variable-length table — the only unit
// whose runtime allocates at all.
const tableAllocatorHook = `
// schema_allocate / schema_release — what "no allocator handed in" means for
// this program. schema_allocate hands back ZEROED bytes and NULL on failure:
// an arena segment is copied whole, padding included, so anything left
// uninitialized here would reach a packed region. Supply both and <stdlib.h>
// is never included; hand a TableAllocator to a builder to route one
// structure's allocations somewhere else again.
#ifndef schema_allocate
#include <stdlib.h> // calloc, free
#define schema_allocate( bytes ) calloc( (size_t) 1, (size_t) ( bytes ) )
#define schema_release( pointer ) free( pointer )
#endif // #ifndef schema_allocate
`

// unitHasKeyedArray reports whether any closure member declares an enum-keyed
// array, which is what decides whether the unit's Table.h carries the keyed
// storage type and its refusal.
func unitHasKeyedArray(u *ir.Unit, closure map[string]bool) bool {
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.KeyEnum != "" {
				return true
			}
		}
	}
	return false
}

// tableKeyedStorage is the storage type behind `ships [ShipType]ShipConfig`
// (docs/SPEC-TABLES.md §2.4). Emitted only into a unit that declares a keyed array,
// so a unit without one is byte-identical to what it was.
//
// Its layout IS `T slots[E::Max]` — one non-static data member, no virtuals,
// everything public — so it stays trivially copyable and standard-layout and a
// table holding one stays relocatable (§9). The extent is a static constant
// derived from the key enum, which is not a data member and changes no layout;
// the iterator and the entry are NESTED TYPES holding no storage of their own,
// so the iteration surface costs the struct nothing and claims no unit-level
// name.
const tableKeyedStorage = `
// An ENUM-KEYED array's storage: E.Max slots, ONE PER NAMED VARIANT, with the
// key k at index k-1 — the storage SHIFTS LEFT and nothing is stored for None.
//
// NOTHING OUTSIDE THE ARRAY NAMES ITS SIZE: the extent is derived from E::Max
// here and nowhere else, so there is no size parameter to spell and no count a
// consumer could put one out of step with.
//
// NONE IS THE NULL KEY: it names no slot, it never rides on the wire, a stored
// key of 0 is malformed, and INDEXING BY IT IS A PROGRAM ERROR IN EVERY
// CONFIGURATION — caught by operator[], which cannot see a runtime key any
// earlier, and REFUSED UNCONDITIONALLY. A KEY PAST Max IS THE SAME ERROR for
// the same reason — it names a variant this enum does not have — so the
// accessor refuses BOTH ENDS. NDEBUG does not remove the compare:
// there is NO UB PATH here in any build. ITERATION is still the surface a
// consumer of the whole array wants: begin()/end() walk every stored slot and
// yield the KEY, 1..E.Max, so a call site writes no bound, no cast, no shift
// and no None question.
template <typename T, typename E>
struct TableKeyed
{
    // the extent is the enum's, derived here and named nowhere else
    static constexpr int32_t kSlots = (int32_t) E::Max;

    T slots[kSlots] = {};

    T & operator[]( E key )
    {
        RefuseKey( key );
        return slots[ (int32_t) key - 1 ];
    }
    const T & operator[]( E key ) const
    {
        RefuseKey( key );
        return slots[ (int32_t) key - 1 ];
    }

    // THE REFUSAL, and it stands in EVERY BUILD, AT BOTH ENDS. The storage
    // holds one slot per NAMED variant: nothing for None below it and nothing
    // above Max, so a build that skipped this compare would index one element
    // BEFORE the array or past its end — undefined behavior in the
    // configuration a game ships. Either key is a program error, so the
    // accessor ends the program rather than reading something. The assert
    // carries the message where a debugger can read it and NDEBUG removes
    // that; the fatal is what stands after it. BOTH GO THROUGH THE HOOKS —
    // define schema_assert and schema_fatal and this refusal lands in your
    // own handler.
    //
    // ONE UNSIGNED COMPARE COVERS BOTH ENDS: the storage index is key - 1, and
    // None's is -1, which wraps above kSlots unsigned. The cost is one
    // perfectly-predicted compare, on a path that reads config.
    static void RefuseKey( E key )
    {
        if ( (uint32_t) ( (int32_t) key - 1 ) >= (uint32_t) kSlots )
        {
            schema_assert( false && "an enum-keyed array holds one slot per named variant: None keys none, and neither does a key past Max" );
            schema_fatal();
        }
    }

    // ---- iteration: keys 1..E.Max over storage 0..E.Max-1, key beside element ----
    //
    // The entry is a key and a REFERENCE, handed out BY VALUE the way any
    // proxy is: for ( auto [ key, element ] : keyed ) binds element to the
    // reference member, so iterating fills the array as well as reads it.
    // auto & [ key, element ] does NOT compile, and that is by design — a
    // non-const lvalue reference cannot bind to the proxy. Write
    // auto [ ... ], or auto && [ ... ] if you prefer the reference form.
    //
    // THE ITERATORS CARRY NO iterator_traits TYPEDEFS. They bought std::distance
    // and the forward-pass algorithms for an audience that does not call them,
    // and the <iterator> they need is the single most expensive include the
    // generated corpus had: 536 headers and 986 KB, in a header whose whole
    // remaining set is 123. begin(), end() and size() need none of it.

    struct Entry { E key; T & element; };
    struct ConstEntry { E key; const T & element; };

    struct Iterator
    {
        T * slots;
        int32_t index; // the STORAGE index; the key it holds is index + 1
        Entry operator*() const { return Entry{ (E) ( index + 1 ), slots[index] }; }
        Iterator & operator++() { index++; return *this; }
        bool operator==( const Iterator & other ) const { return index == other.index; }
        bool operator!=( const Iterator & other ) const { return index != other.index; }
    };

    struct ConstIterator
    {
        const T * slots;
        int32_t index; // the STORAGE index; the key it holds is index + 1
        ConstEntry operator*() const { return ConstEntry{ (E) ( index + 1 ), slots[index] }; }
        ConstIterator & operator++() { index++; return *this; }
        bool operator==( const ConstIterator & other ) const { return index == other.index; }
        bool operator!=( const ConstIterator & other ) const { return index != other.index; }
    };

    Iterator begin() { return Iterator{ slots, 0 }; }
    Iterator end() { return Iterator{ slots, kSlots }; }
    ConstIterator begin() const { return ConstIterator{ slots, 0 }; }
    ConstIterator end() const { return ConstIterator{ slots, kSlots }; }
};

`

// tablePrimitives is the shared runtime, emitted into every Table.h behind a
// per-package guard — one definition per TU whatever the include order, and a
// lone Table.h works standalone.
// tableInlineMacro names the unit's force-inline macro. It is package-scoped
// for the same reason the primitives guard is: a consumer compiles several
// packages' Table.h files in one TU, and a macro every one of them spelled the
// same way would be a redefinition.
func tableInlineMacro(pkg string) string { return strings.ToUpper(pkg) + "_TABLE_INLINE" }

func tablePrimitives(pkg string, anyVariable bool, anyKeyed bool, anyExtent bool, idCap int, u *ir.Unit) string {
	// THE ID TABLE'S CAPACITY IS A COMPILE-TIME FACT of the unit (§3): the
	// distinct names its table closure can spell, so a save allocates nothing.
	// The bucket count is the next power of two at twice the capacity, so the
	// chains stay short, and the shift is what turns the mixed hash into one.
	buckets := 16
	bits := 4
	for buckets < 2*idCap {
		buckets *= 2
		bits++
	}
	idCapacity := strconv.Itoa(idCap)
	idBuckets := strconv.Itoa(buckets)
	idShift := strconv.Itoa(64 - bits)
	keyedStorage := ""
	if anyKeyed {
		keyedStorage = tableKeyedStorage
	}
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_PRIMITIVES"
	forceInline := tableInlineMacro(pkg)
	messageForm := tableMessageForm(u, anyVariable)
	// the two pointer-era descriptor members exist only in a unit that HAS
	// pointers: a unit of value-only tables emits the descriptor surface it
	// always emitted, to the byte (docs/SPEC-TABLES.md §2, the zero-cost gate)
	// the PLACE column (docs/SPEC-TABLES.md §8.1, §16), emitted only into a
	// unit that declares a map or an unbounded array: the one resolver the ONE
	// text walk cannot spell for itself, because TableMap<Entry> and
	// TableList<T> are types it has no name for. The SHAPE of either is read
	// from the array columns like every other array's, with array_bound = 0
	// the one tell that the offset names a reference and not the first element.
	placeMember := ""
	// WHY A FILE WAS REFUSED, by name (docs/SPEC-TABLES.md §6.5, §7, §11,
	// §19.2): ONE enum for a cook's Open, a block's BlockOpen and LoadMeasure's
	// -1, in every unit, because a caller asking why it cannot have this file
	// is asking one question whichever call refused it. Written on the
	// refusal path only.
	refuseReason := tableRefuseReasonEnum
	if anyExtent {
		placeMember = "\n    // an OUT-OF-LINE array (docs/SPEC-TABLES.md §8.1): place one element and\n" +
			"    // hand it back at its defaults. A MAP places BY KEY, a string key comes\n" +
			"    // in as the bytes and the length, an integer key as the value, and NULL\n" +
			"    // is NOT INSERTED: a key past the bound, or an arena that could not carve\n" +
			"    // another segment. A LIST ignores the key and APPENDS, NULL at the arena\n" +
			"    // or the int32 cap. NULL on every field that is neither.\n" +
			"    void * ( * place )( TableWorker & worker, void * slot, const char * key, int32_t key_length, int64_t key_value );"
	}
	pointerFieldMember, pointerTypeMember, pointerForward := "", "", ""
	if anyVariable {
		pointerForward = "// the arena's allocation front, defined with the variable-length runtime\n" +
			"// below; a descriptor names it only through a pointer parameter.\nstruct TableWorker;\n\n"
		pointerFieldMember = "\n    bool is_pointer;        // a *T pointer field: storage is an 8-byte TableRef; the target is a table" +
			"\n    // THE TWO THE TEXT FORM NEEDS (docs/SPEC-TABLES.md §16.7), and they\n" +
			"    // are here for the same reason is_pointer is: the walk is ONE walk\n" +
			"    // over descriptors and cannot spell a target's own <T>At or\n" +
			"    // <T>Emplace. `resolve` reads a slot in a REGION and answers the\n" +
			"    // node it names, or NULL; `emplace` allocates one in a BUILDER's\n" +
			"    // arena and points the slot at it. NULL on every field that is not\n" +
			"    // a pointer, and emitted only in a unit that declares one.\n" +
			"    const void * (*resolve)( const void * slot );\n" +
			"    void * (*emplace)( TableWorker & worker, void * slot );"
		pointerTypeMember = "\n    // the DERIVED mode (docs/SPEC-TABLES.md): false = fixed-size, a plain\n" +
			"    // relocatable struct; true = variable-length, built through a Builder\n" +
			"    // and read through a region root. Nobody declares it; the compiler\n" +
			"    // works it out.\n    bool variable;"
	}
	return `#ifndef ` + guard + `
#define ` + guard + `

// THE CODEC DOES NOT DEPEND ON THE COMPILER'S INLINING BUDGET. A table of a
// realistic field count emits one large body per type, and the cursor a body
// writes through lives in the caller's ` + "`TableWriter`" + `: across a call boundary
// that cursor round-trips through memory, and a ` + "`uint8_t *`" + ` store may alias the
// writer itself, so every put reloads it. When a budget runs out mid-body the
// codec silently degrades to that shape. Forcing the primitives and the
// fixed-class bodies inline is what keeps the cursor in registers and lets
// adjacent constant framing bytes merge into one store.
#if defined( _MSC_VER )
#define ` + forceInline + ` __forceinline
#elif defined( __GNUC__ ) || defined( __clang__ )
#define ` + forceInline + ` inline __attribute__(( always_inline ))
#else
#define ` + forceInline + ` inline
#endif

namespace ` + pkg + ` {

// WHY A READ WAS REFUSED, by name (docs/SPEC-TABLES.md §3.3, §11). A REFUSAL
// is not one of §4's events: nothing is decoded, no counter moves and no
// damage is reported, so five zero counters and a false flag are what a clean
// read prints too and only the verdict tells them apart. The reason says which
// refusal it was.
//
// This is the MESSAGE PATH's vocabulary and not the cooked form's (§7.4): a
// caller meeting one of these has been refused a MESSAGE on a connection,
// which is a different recovery with a different owner than a file a header
// match turned down.
enum TableMessageReason
{
    newer_form,           // a FORM BYTE this reader does not carry (§3)
    no_vocabulary,        // no table for this connection: the message arrived before the announcement, or after a refused one
    second_announcement,  // a second announcement on a connection: it sets nothing, amends nothing, and the connection closes
    vocabulary_too_large, // an announcement above the receiver's declared bound, refused before an entry is touched
    message_form_as_file, // a form 2 wire where a FILE was expected: its table is somewhere else
    batch_too_large       // a batch of more than 256 bodies on the write side, or of more than the caller has room for on the read side: nothing is written or decoded, and the count says what the wire carries
};

// The table-wire read report — the permissive contract's ledger. Silence
// (all zero) means the data matched this reader's schema exactly.
struct TableReport
{
    int32_t unknown = 0;       // unknown field ids skipped (newer data)
    int32_t kind_mismatch = 0; // known id, changed type — skipped, never misdecoded
    // a kind that GREW since the writer (docs/SPEC-TABLES.md §4): an integer
    // kind read into a wider one of the same signedness, or f32 into f64,
    // decoded EXACTLY. One count per field or per map. It is the one counter
    // that names no loss: the bytes were not the shape this reader declares,
    // and the number survived.
    int32_t widened = 0;
    int32_t clamped = 0;       // out-of-range values clamped to declared bounds
    // a key the TEXT form saw twice: last wins, and the repeat is counted
    // (docs/SPEC-TABLES.md §16.2). The wire never raises it — a body carrying an
    // id twice is legal input whose last occurrence wins, silently (§3).
    int32_t duplicate = 0;
    bool malformed = false;    // framing damage; decode stopped, partial result kept
    // THE REFUSAL VERDICT, which is not one of §4's events and moves no counter
    // (docs/SPEC-TABLES.md §3): a FORM BYTE this reader does not carry. Five
    // zero counters and a false flag are what a clean read prints too, so the
    // verdict is what tells the two apart.
    bool refused = false;
    // WHICH refusal, and it is read only when refused is set: a read that
    // was not refused has no reason, and this member is the one the caller
    // must not look at then (docs/SPEC-TABLES.md §3.3).
    TableMessageReason reason = newer_form;
};
` + refuseReason + `
// ---- reflection (tables only, docs/SPEC-TABLES.md) ----
//
// Static field descriptors for every type in the table closure: name, wire
// id/kind, storage offset, bounds, ranges, enum names and branch guards —
// enough to walk, print, diff, edit or bind any table value at runtime with
// no RTTI and no schema files. TableType<X>() returns X's descriptor.

struct TableTypeInfo;

// One arm of a union field: where its payload sits inside the union's storage
// and what its payload looks like. The arm's NAME and its table-wire id come
// from the field's enum_name/variant_id functions at the same tag, so nothing
// is spelled twice (docs/SPEC-TABLES.md §8).
struct TableFieldInfo;

struct TableUnionArmInfo
{
    uint32_t offset;             // offsetof the arm's payload within the union storage
    const TableTypeInfo * table; // the arm payload's descriptor, or NULL
    // AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6): an arm that names no
    // declared type or table carries the FIELD descriptor a field of that
    // type would carry instead — offsets taken within the union storage — so
    // a generic walk meets an arm's kind, width, bounds and companions where
    // it meets a field's. Exactly one of the two is non-NULL on a set arm.
    const TableFieldInfo * field;
    uint32_t size;               // the arm's whole storage, which selection zero-establishes
};

// A union field's shape: the tag, and the arms indexed by it. Arms run
// [0, enum_max]; index 0 is the EMPTY arm and carries no payload.
struct TableUnionInfo
{
    uint32_t tag_offset; // offsetof the tag within the union storage
    uint32_t tag_size;   // sizeof the tag
    const TableUnionArmInfo * arms;
};

// The exact raw range of a wide-kind field (docs/SPEC-TABLES.md §8.2): two 128-bit
// values as 64-bit lanes, low lane first, two's complement for the signed kinds.
struct TableWideRange
{
    uint64_t lo[2];
    uint64_t hi[2];
};

// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1): a declaration with no ///
// block carries a doc column pointing at this one object, so absence costs a
// unit no string data and a printer concatenates doc columns with no null
// test. One definition for the whole unit: every absent doc compares equal by
// address.
inline const char TableDocNone[1] = "";

` + pointerForward + `struct TableFieldInfo
{
    const char * name;      // schema field name, e.g. "health"
    const char * json;      // the TEXT form's key: the json = "key" attribute, else name (§16.3)
    const char * type_name; // schema type name, e.g. "float32", "Grade"
    uint64_t id;            // table-wire field id: fnv1a64 of the name, of the was alias after a rename (§5)
    uint8_t kind;           // table-wire kind; for arrays/strings/bytes, the ELEMENT kind
    bool is_array;          // fixed or counted array (bytes included)` + pointerFieldMember + `
    bool counted;           // a _count/_length int32 companion exists (counted arrays, strings, bytes)
    bool optional;          // a ?T field: a _present bool companion decides whether it rides
    int32_t array_bound;    // array capacity / string max length; 0 for plain scalars
    uint32_t offset;        // offsetof the storage member
    uint32_t elem_size;     // sizeof the member (element size for arrays)
    uint32_t count_offset;  // offsetof the _count/_length companion, or 0xffffffff
    uint32_t present_offset; // offsetof the _present companion, or 0xffffffff
    const TableTypeInfo * table; // nested table's descriptor, or NULL
    bool has_range;         // a declared [min, max] (int or float)
    double range_min;       // NOTE: int64 ranges beyond 2^53 lose precision here
    double range_max;
    // the WIDE kinds (18-29, docs/SPEC-TABLES.md §3, §8.2): frac_bits is a fixed
    // field's F — its storage holds units × 2^F — and wide is the declared
    // range on that RAW scale, exact, as two 128-bit two's-complement values
    // in 64-bit lanes (low lane first). NULL where the declaration bounds
    // nothing (a bare uint128) and for every other kind; frac_bits is 0 for
    // every kind that is not fixed-point. range_min/range_max still carry
    // the declared bounds as doubles — whole units for a fixed field — for
    // a walker that only shows them.
    uint8_t frac_bits;
    const TableWideRange * wide;
    int64_t enum_max;       // enums: highest valid value (None = 0 always valid);
                            // unions: the arm count (tag range [0, enum_max]);
                            // flags: the highest declared BIT INDEX; else -1
    // the vocabulary's names, indexed the same way enum_max bounds: an enum's
    // value -> name, a union's tag -> arm name, a FLAGS field's bit index ->
    // variant name. NULL for every other kind.
    const char * (*enum_name)( uint64_t value );
    // the TABLE-WIRE id of one variant (docs/SPEC-TABLES.md §5): for an enum, the
    // hash of the variant's name; for a union, the hash of the arm's name.
    // 0 is the reserved id — an enum's None, a union's empty. NULL for every
    // other kind — a FLAGS field's variants have no per-variant wire id (§4),
    // so a NULL here beside a non-NULL enum_name is what says "flags".
    // Walk [0, enum_max] to enumerate a vocabulary and its ids.
    uint64_t (*variant_id)( uint64_t value );
    // an ENUM-KEYED array (docs/SPEC-TABLES.md §2.4): the array has one slot per
    // variant of key_type_name, indexed by the variant's value, and its slots
    // ride under variant ids rather than positions. key_name and key_id are
    // the key's vocabulary — walk [0, array_bound) to print slots by name.
    // NULL on every other field.
    const char * key_type_name;
    const char * (*key_name)( uint64_t value );
    uint64_t (*key_id)( uint64_t value );
    // union fields: the tag and its arms, behind a function so the whole
    // descriptor stays CONSTANT-INITIALISED (a captureless lambda converts to
    // a function pointer at compile time; the arms themselves are a static
    // inside it). NULL for every other kind.
    const TableUnionInfo * (*arms)();` + placeMember + `
    const char * guard;     // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded
    // what a PERSON wrote about the field (docs/SPEC-TABLES.md §8.1): the ///
    // block above it, verbatim (SPEC §4.1). It is TableDocNone when there is
    // none, never NULL. Its tags (SPEC §4.2) follow in declared order, and an
    // untagged field is 0 beside NULL. Static, constant-initialized,
    // allocating nothing.
    const char * doc;
    int32_t num_tags;
    const char * const * tags;
};

struct TableTypeInfo
{
    const char * name;   // schema type name
    uint32_t size;       // sizeof the storage struct
    int32_t num_fields;
    const TableFieldInfo * fields;
    // put one instance back at its declared defaults, in place. A generic
    // walker that fills a value has to be able to establish the defaults an
    // absent field takes, and it holds no type to spell — this is the one
    // thing the descriptors could not express without it. Placement-new
    // value-init, exactly what the wire's read path does, and no temporary.
    void (*reset)( void * storage );` + pointerTypeMember + `
    // the declaration's own doc and tags, on the same terms as a field's
    // (docs/SPEC-TABLES.md §8.1)
    const char * doc;
    int32_t num_tags;
    const char * const * tags;
};

struct TableWriter
{
    uint8_t * buffer;
    int64_t capacity;
    int64_t offset = 0;
    bool overflow = false;

    // the parameters do not repeat the member names: a parameter that hides a
    // member is a warning the estate's compilers disagree about (gcc's
    // -Wshadow and cl's C4458 refuse it, clang's -Wshadow does not), and this
    // is a header a consumer compiles under its OWN flags
    TableWriter( uint8_t * to_buffer, int64_t to_capacity ) : buffer( to_buffer ), capacity( to_capacity ) {}

    ` + forceInline + ` void raw( const void * data, int64_t bytes )
    {
        if ( offset + bytes > capacity ) { overflow = true; return; }
        memcpy( buffer + offset, data, (size_t) bytes );
        offset += bytes;
    }
    ` + forceInline + ` void put8( uint8_t v )   { raw( &v, 1 ); }
    ` + forceInline + ` void put16( uint16_t v ) { uint8_t b[2] = { uint8_t( v ), uint8_t( v >> 8 ) }; raw( b, 2 ); }
    ` + forceInline + ` void put32( uint32_t v ) { uint8_t b[4] = { uint8_t( v ), uint8_t( v >> 8 ), uint8_t( v >> 16 ), uint8_t( v >> 24 ) }; raw( b, 4 ); }
    ` + forceInline + ` void put64( uint64_t v ) { put32( uint32_t( v ) ); put32( uint32_t( v >> 32 ) ); }
    // a 128-bit value as two lanes, the low half first (docs/SPEC-TABLES.md §3)
    ` + forceInline + ` void put128( uint64_t lo, uint64_t hi ) { put64( lo ); put64( hi ); }
    // EVERY LENGTH, COUNT, INDEX AND ID REFERENCE IS ONE CANONICAL UNSIGNED
    // LEB128 (docs/SPEC-TABLES.md §3): seven value bits a byte, the lowest
    // group first, the high bit set on every byte but the last. One value has
    // one spelling, so two conforming writers agree byte for byte.
    ` + forceInline + ` void putleb( uint64_t v )
    {
        while ( v >= 0x80 ) { put8( uint8_t( v ) | 0x80 ); v >>= 7; }
        put8( uint8_t( v ) );
    }
};

// TableLebBytes is one value's spelling length, which a MEASURE needs before
// the bytes exist — the length of a body has to be known before it is written,
// because a length whose own width moves cannot be patched in place.
inline int64_t TableLebBytes( uint64_t v )
{
    int64_t n = 1;
    while ( v >= 0x80 ) { v >>= 7; n++; }
    return n;
}

// THE ID TABLE, WRITER SIDE (docs/SPEC-TABLES.md §3). It holds every id the
// body used, once each, in FIRST-USE order over the whole wire, and the body
// names them by position: reference k is the kth entry, counted from 1, and
// reference 0 names NO ID.
//
// Its capacity is a COMPILE-TIME fact of the unit — the distinct names its
// table closure can spell — so a save allocates nothing: the table is a local
// of Measure and of Save. The bucket chain makes ref constant time and makes
// truncate constant time too, which is what an ELIDED field needs: a field
// that turns out not to ride costs nothing in the id table either, so the walk
// interns its id, builds the payload that decides, and undoes the entry when
// nothing rides.
struct TableIds
{
    static const int32_t kCapacity = ` + idCapacity + `;
    static const int32_t kBuckets = ` + idBuckets + `;

    uint64_t ids[ kCapacity ];
    int32_t chain[ kCapacity ];
    int32_t head[ kBuckets ];
    int32_t count;
    bool overflow;

    TableIds() : count( 0 ), overflow( false )
    {
        for ( int32_t i = 0; i < kBuckets; i++ ) { head[i] = -1; }
    }

    static ` + forceInline + ` uint32_t bucket_of( uint64_t id )
    {
        return uint32_t( ( id * 0x9E3779B97F4A7C15ull ) >> ` + idShift + ` ) & uint32_t( kBuckets - 1 );
    }

    // the reference an id takes: the file's own first-use entry, appended on
    // first use. The MESSAGE form names no id at all: its references are
    // compile-time slots of the announced vocabulary (docs/SPEC-TABLES.md §3.3).
    ` + forceInline + ` uint64_t ref( uint64_t id )
    {
        const uint32_t b = bucket_of( id );
        for ( int32_t i = head[b]; i >= 0; i = chain[i] )
        {
            if ( ids[i] == id ) { return uint64_t( i ) + 1; }
        }
        if ( count >= kCapacity ) { overflow = true; return 1; }
        ids[count] = id; chain[count] = head[b]; head[b] = count; count++;
        return uint64_t( count );
    }

    // undo every entry appended since mark. An entry removed is the most
    // recent one in its bucket, so it sits at that bucket's head.
    void truncate( int32_t mark )
    {
        while ( count > mark )
        {
            count--;
            head[ bucket_of( ids[count] ) ] = chain[count];
        }
    }
};

// TableIdsBytes is the trailer's own size: the entries, each a fixed
// little-endian u64, and the ENTRY COUNT, the one fixed-width number on the
// wire (docs/SPEC-TABLES.md §3).
inline int64_t TableIdsBytes( const TableIds & ids ) { return int64_t( ids.count ) * 8 + 8; }

// TableIdsWrite puts the trailer where the walk ended: a writer never patches,
// because first-use order is known only when the walk ends.
inline void TableIdsWrite( TableWriter & w, const TableIds & ids )
{
    for ( int32_t i = 0; i < ids.count; i++ ) { w.put64( ids.ids[i] ); }
    w.put64( uint64_t( ids.count ) );
}

// THE ID TABLE, READER SIDE (docs/SPEC-TABLES.md §3). A reader locates it from
// the END of the wire and resolves it ONCE, at open: the entries are eight
// bytes each and a body names them by position, so every field dispatches
// through an index rather than through a search over hashes.
struct TableIdTable
{
    const uint8_t * entries = NULL;
    int64_t count = 0;

    // the id a reference names. ref is 1-based and bounds-checked by the
    // caller: a reference ABOVE the entry count is framing damage on the body
    // that carries it, and 0 names no id at all.
    uint64_t at( uint64_t ref ) const
    {
        const uint8_t * e = entries + ( ref - 1 ) * 8;
        uint64_t lo = uint64_t( e[0] ) | uint64_t( e[1] ) << 8 | uint64_t( e[2] ) << 16 | uint64_t( e[3] ) << 24;
        uint64_t hi = uint64_t( e[4] ) | uint64_t( e[5] ) << 8 | uint64_t( e[6] ) << 16 | uint64_t( e[7] ) << 24;
        return lo | ( hi << 32 );
    }
};

struct TableReader
{
    const uint8_t * buffer;
    int64_t size;
    int64_t offset = 0;
    TableReport * report;
    const TableIdTable * ids = NULL;
    // ONLY THE ROOT BODY CARRIES THE NODE TABLE (docs/SPEC-TABLES.md §3.1), so
    // a body has to know which it is: the reserved id inside a NESTED body is
    // malformed, because a second numbering cannot exist. Every reader made
    // for a payload is nested; the two the wire surfaces make for a root say so.
    bool nested = true;

    TableReader( const uint8_t * from_buffer, int64_t from_size, TableReport * to_report )
        : buffer( from_buffer ), size( from_size ), report( to_report ) {}

    TableReader( const uint8_t * from_buffer, int64_t from_size, TableReport * to_report, const TableIdTable * to_ids )
        : buffer( from_buffer ), size( from_size ), report( to_report ), ids( to_ids ) {}

    ` + forceInline + ` bool has( int64_t bytes ) const { return offset + bytes <= size; }
    // A LENGTH IS A 64-BIT NUMBER AND A BUFFER IS NOT (docs/SPEC-TABLES.md
    // §3): every length, count and index on this wire has sixty-four bits of
    // capability, so one past what remains must be compared UNSIGNED. Casting
    // it to int64 first turns 0xFFFFFFFFFFFFFFFF into -1, and a negative
    // length looks like room.
    ` + forceInline + ` bool room( uint64_t bytes ) const { return bytes <= (uint64_t) ( size - offset ); }
    ` + forceInline + ` uint8_t get8()   { return buffer[offset++]; }
    ` + forceInline + ` uint16_t get16() { uint16_t v = uint16_t( buffer[offset] ) | uint16_t( buffer[offset+1] ) << 8; offset += 2; return v; }
    ` + forceInline + ` uint32_t get32() { uint32_t v = uint32_t( buffer[offset] ) | uint32_t( buffer[offset+1] ) << 8 | uint32_t( buffer[offset+2] ) << 16 | uint32_t( buffer[offset+3] ) << 24; offset += 4; return v; }
    ` + forceInline + ` uint64_t get64() { uint64_t lo = get32(); uint64_t hi = get32(); return lo | ( hi << 32 ); }
    ` + forceInline + ` void get128( uint64_t & lo, uint64_t & hi ) { lo = get64(); hi = get64(); }

    // ONE CANONICAL UNSIGNED LEB128 (docs/SPEC-TABLES.md §3), and a
    // non-minimal spelling is MALFORMED: 0x80 0x00 and 0x00 both spell zero,
    // and only the second is legal input. An encoding past ten bytes, or a
    // tenth byte with a bit above the 64th value bit, is malformed on the same
    // rule. false = framing damage on the body carrying it.
    bool getleb( uint64_t & value )
    {
        // A NUMBER THIS READER REFUSES LEAVES THE CURSOR WHERE IT WAS. The
        // caller's next question is often "did this body end exactly at its
        // L", and a rejected number that had moved the cursor would answer
        // that question with the damage already stepped over.
        const int64_t at = offset;
        value = 0;
        uint32_t shift = 0;
        for ( int32_t i = 0; i < 10; i++ )
        {
            if ( !has( 1 ) ) { offset = at; return false; }
            const uint8_t b = get8();
            if ( i == 9 && b > 1 ) { offset = at; return false; }
            value |= uint64_t( b & 0x7F ) << shift;
            if ( ( b & 0x80 ) == 0 )
            {
                if ( i > 0 && b == 0 ) { offset = at; return false; } // a redundant continuation
                return true;
            }
            shift += 7;
        }
        offset = at;
        return false;
    }

    // resolve one id reference against the file's table. false = a reference
    // ABOVE the entry count, or a 0 where an id is required, both of which
    // are framing damage on the body that carries it.
    bool getid( uint64_t & id )
    {
        uint64_t ref = 0;
        if ( !getleb( ref ) ) { return false; }
        if ( ref == 0 || ids == NULL || ref > (uint64_t) ids->count ) { return false; }
        id = ids->at( ref );
        return true;
    }

    // skip one payload by kind; false = framing damage. FOUR RULES COVER THE
    // SET (docs/SPEC-TABLES.md §3), and a kind outside it is not skippable —
    // which is why the set is closed and why kind 31 exists.
    bool skip( uint8_t kind )
    {
        switch ( kind )
        {
            // the fixed-width kinds, each by its width: 18-29 are the 128-bit integers and
            // the fixed-point family at every storage width (docs/SPEC-TABLES.md §3)
            case 1: case 2: case 6: case 20: case 25: return has( 1 ) ? ( offset += 1, true ) : false;
            case 3: case 7: case 21: case 26:         return has( 2 ) ? ( offset += 2, true ) : false;
            case 4: case 8: case 10: case 22: case 27: return has( 4 ) ? ( offset += 4, true ) : false;
            case 5: case 9: case 11: case 23: case 28: return has( 8 ) ? ( offset += 8, true ) : false;
            case 18: case 19: case 24: case 29: return has( 16 ) ? ( offset += 16, true ) : false;
            case 17: case 30: // a NODE INDEX (§3.1) and an ENUM's variant reference: one LEB128 and stop
            {
                uint64_t ignored = 0;
                return getleb( ignored );
            }
            case 12: case 13: case 14: case 16: case 31: case 32: // 31 is the ESCAPE, 32 the payload-free kind
            {
                uint64_t n = 0;
                if ( !getleb( n ) ) return false;
                return room( n ) ? ( offset += (int64_t) n, true ) : false;
            }
            case 15: // union: the arm id reference, then its kind, its L and its payload (reference 0 = empty)
            {
                uint64_t arm = 0;
                if ( !getleb( arm ) ) return false;
                if ( arm == 0 ) return true;
                if ( !has( 1 ) ) return false;
                offset += 1; // the arm's kind byte
                uint64_t n = 0;
                if ( !getleb( n ) ) return false;
                return room( n ) ? ( offset += (int64_t) n, true ) : false;
            }
            // KIND 34 IS RESERVED FOR float16 AND IS NOT PART OF THIS MAJOR (§3):
            // no writer emits it and no reader has a rule for it, so a reader
            // meets it only as DAMAGE, exactly as it meets 35 or 200. A bare 34
            // is a writer that ignored the escape kind 31.
            case 34: return false;
        }
        return false;
    }
};

` + tableWidenRuntime + tableTextRuntime + `
// The RESERVED node-table id, the one id the language holds back
// (docs/SPEC-TABLES.md §3.1, §5). It rides in every unit, pointered or not,
// because every body has to know that a NESTED body claiming one is damaged.
static const uint64_t kTableNodeTableFieldId = 0xFFFFFFFFFFFFFFFFull;

// TableWireForm is the FORM BYTE, and it is the whole header
// (docs/SPEC-TABLES.md §3). A reader that meets a byte it does not know
// refuses the wire by name and never reports damage.
const uint8_t kTableWireForm = 1;

// TableOpen reads the form byte and the trailer, in that order, and hands back
// the ROOT BODY. It answers one of three verdicts, because five zero counters
// and a false flag are what a clean read prints too:
//
//   TableOpenOk       the form is known and the table read whole
//   TableOpenRefused  a FORM BYTE this reader does not carry: nothing is
//                     decoded, nothing is counted, and no damage is reported
//   TableOpenDamaged  a table that cannot be read whole — fewer than eight
//                     bytes, a count whose entries run past the front of the
//                     file, a count that leaves no room for the form byte, or
//                     ONE ID IN TWO ENTRIES. The whole wire is malformed,
//                     nothing is decoded, and one event is counted.
//   TableOpenBodyStopped  the form and the table were good and the ROOT BODY
//                     could not be walked to its own terminator. What it
//                     decoded before that is kept, as everywhere on this wire.
enum TableOpenVerdict { TableOpenOk, TableOpenRefused, TableOpenDamaged, TableOpenBodyStopped };

inline TableOpenVerdict TableOpen( const uint8_t * buffer, int64_t bytes, TableIdTable & table, int64_t & body_bytes )
{
    if ( bytes < 1 ) { return TableOpenDamaged; }
    if ( buffer[0] != kTableWireForm ) { return TableOpenRefused; }
    if ( bytes < 9 ) { return TableOpenDamaged; }
    const uint8_t * tail = buffer + bytes - 8;
    uint64_t lo = uint64_t( tail[0] ) | uint64_t( tail[1] ) << 8 | uint64_t( tail[2] ) << 16 | uint64_t( tail[3] ) << 24;
    uint64_t hi = uint64_t( tail[4] ) | uint64_t( tail[5] ) << 8 | uint64_t( tail[6] ) << 16 | uint64_t( tail[7] ) << 24;
    uint64_t count = lo | ( hi << 32 );
    if ( count > (uint64_t) ( bytes / 8 ) ) { return TableOpenDamaged; }
    const int64_t span = (int64_t) count * 8 + 8;
    if ( span + 1 > bytes ) { return TableOpenDamaged; }
    table.entries = buffer + bytes - span;
    table.count = (int64_t) count;
    // THE ENTRIES ARE DISTINCT: a table that carries one id twice is malformed
    // for the whole wire, because no wire this schema writes carries a repeat
    // and it would leave one more shape of table for a hostile writer to aim
    // at (docs/SPEC-TABLES.md §3).
    for ( int64_t i = 1; i < table.count; i++ )
    {
        const uint64_t id = table.at( uint64_t( i ) + 1 );
        for ( int64_t j = 0; j < i; j++ )
        {
            if ( table.at( uint64_t( j ) + 1 ) == id ) { return TableOpenDamaged; }
        }
    }
    body_bytes = bytes - span - 1;
    return TableOpenOk;
}

// TableBodyExtent walks a body's framing to the zero reference that ends it,
// so a reader can tell a body that ENDED EARLY — leaving bytes no field claims
// — from one that is merely damaged. ANY BYTE BETWEEN THE ROOT'S TERMINATOR
// AND THE TABLE'S FIRST ENTRY IS MALFORMED, because no field claims it and the
// two ends of the file have met (docs/SPEC-TABLES.md §3).
inline bool TableBodyEndsEarly( const uint8_t * body, int64_t bytes, const TableIdTable & table )
{
    TableReport ignored;
    TableReader r( body, bytes, &ignored, &table );
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.getleb( ref ) ) { return false; }
        if ( ref == 0 ) { return r.offset != bytes; }
        if ( ref > (uint64_t) table.count ) { return false; }
        if ( !r.has( 1 ) ) { return false; }
        if ( !r.skip( r.get8() ) ) { return false; }
    }
}

` + messageForm + `` + keyedStorage + `inline float table_bits_to_float( uint32_t bits ) { float f; memcpy( &f, &bits, 4 ); return f; }
inline uint32_t table_float_to_bits( float f ) { uint32_t b; memcpy( &b, &f, 4 ); return b; }
inline double table_bits_to_double( uint64_t bits ) { double d; memcpy( &d, &bits, 8 ); return d; }
inline uint64_t table_double_to_bits( double d ) { uint64_t b; memcpy( &b, &d, 8 ); return b; }

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// Generate emits <Base>Table.h for every unit file when the unit declares
// tables, and nothing when it does not — a table-free unit's generated tree
// is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	closure := ir.TableClosure(u)
	variable := ir.VariableTables(u)
	targets := ir.PointerTargets(u)
	anyVariable := len(variable) > 0
	anyKeyed := unitHasKeyedArray(u, closure)
	anyMap := unitHasMap(u, closure)
	anyList := unitHasList(u, closure)
	anyExtent := anyMap || anyList
	blocks := ir.Blocks(u)

	// The BLOCK FORM (docs/SPEC-TABLES.md §19) is emitted ON THE SIDE, into
	// <Base>Block.h and <Base>Block.cpp: nothing declares it, every fixed
	// table has one, and a consumer includes and compiles it only if it uses
	// the form. The Table header below carries not one symbol of it.
	out := generateBlockFiles(u, blocks, variable, targets)
	// THE MESSAGE FORM'S VOCABULARY (docs/SPEC-TABLES.md §3.3), derived once
	// for the unit: every id the closure can put on a wire, and the slot the
	// compiler settled for it. A generated field header carries the slot as a
	// literal beside the id.
	slots := ir.TableVocabularySlots(u)
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, anyVariable: anyVariable, anyKeyed: anyKeyed, anyMap: anyMap, anyList: anyList, anyExtent: anyExtent, blocks: blocks, variable: variable, targets: targets,
			includes: map[string]bool{}, nativeIncludes: map[string]bool{}, slots: slots}
		var members []*ir.Struct
		members = append(members, orderTables(f.Tables)...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		unions := tableUnionsOf(f)
		if len(members) > 0 {
			// a table-armed union is emitted before the first table that holds
			// it and after its same-file arms, which orderTables placed ahead
			// of that holder; one no same-file table holds follows the structs
			emittedUnion := map[string]bool{}
			for _, st := range members {
				if !st.IsTable {
					continue
				}
				for _, un := range tableUnionsHeldBy(st, unions) {
					if !emittedUnion[un.Name] {
						emittedUnion[un.Name] = true
						g.emitTableUnion(un)
					}
				}
				g.emitTableStruct(st)
			}
			for _, un := range unions {
				if !emittedUnion[un.Name] {
					emittedUnion[un.Name] = true
					g.emitTableUnion(un)
				}
			}
			for _, e := range tableEnums(members) {
				g.emitEnumIdentity(e)
			}
			g.emitTableResetDeclarations(members)
			for _, st := range members {
				g.emitTableReset(st)
			}
			g.emitMessageBodyDeclarations(members)
			g.emitCodecDeclarations(members)
			g.emitMapSurfaces(members)
			for _, st := range members {
				g.owner = st
				g.emitTableMeasure(st)
				g.emitTableWrite(st)
				g.emitTableSave(st)
				g.emitTableRead(st)
				g.emitMessageCodec(st)
				g.emitMessageEntries(st)
			}
			g.emitVariableSurface(members)
			g.emitCookSurface(members)
			g.emitCookWriteSurface(members)
			g.emitRelocatabilityPreamble()
			for _, st := range members {
				g.pf("static_assert( __is_trivially_copyable( %s ), \"%s must stay relocatable\" );\n", st.Name, st.Name)
				g.pf("static_assert( __is_standard_layout( %s ), \"%s must stay standard-layout for offsetof\" );\n", st.Name, st.Name)
			}
			g.pf("\n")
			g.emitCookLayoutAsserts(members)
			g.emitListAlignAsserts(members)
			g.pf("// ---- reflection descriptors (tables only, docs/SPEC-TABLES.md) ----\n\n")
			for _, st := range members {
				g.pf("inline const TableTypeInfo * %sTableType();\n", st.Name)
			}
			if anyVariable {
				g.pf("// The descriptors are CONSTANT-INITIALISED data, and a field's target is\n")
				g.pf("// the ADDRESS of another descriptor. These declarations are what let a\n")
				g.pf("// self- or mutually-referential graph — Node naming itself through *Node —\n")
				g.pf("// be expressed as constant data instead of a lazy link, which could not\n")
				g.pf("// have been written race-free OR recursion-safe. The whole reflection\n")
				g.pf("// surface is therefore immutable: read it from any thread, any time.\n")
				for _, st := range members {
					g.pf("extern const TableTypeInfo %sTableInfo;\n", st.Name)
				}
			}
			g.pf("\n")
			for _, st := range members {
				g.owner = st
				g.emitTableDescriptor(st)
			}
			// the TEXT form's surface (docs/SPEC-TABLES.md §16), DECLARED after the
			// descriptors it names. The definitions and the one generic walk
			// they call live in <Base>Table.cpp, so a translation unit that
			// includes this header for the wire codecs or the descriptors pays
			// nothing for a form it never calls.
			g.pf("// ---- the text form (docs/SPEC-TABLES.md §16) ----\n\n")
			for _, st := range members {
				g.emitJsonDeclarations(st)
			}
		} else {
			for _, un := range unions {
				g.emitTableUnion(un) // its arms are all cross-file: nothing here precedes it
			}
			g.pf("// no tables declared or referenced in this file — codecs are emitted\n")
			g.pf("// for the table closure only (`table` declarations and what they reach)\n")
		}
		var h strings.Builder
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n// your choice. See the LICENSE exception in the schema compiler; the compiler is\n// AGPL-3.0, its output is not.\n", f.Base)
		fmt.Fprintf(&h, "// package %s — protocol id 0x%016x (packets only: tables version by field id, not by protocol id)\n", u.Package, u.ProtocolId)
		if unitHas128(u, closure) {
			h.WriteString("// The TABLE wire (evolution-tolerant, docs/SPEC-TABLES.md): includable from\n")
			h.WriteString("// any TU — the one thing taken from serialize.h is the 128-bit storage type.\n\n")
		} else {
			h.WriteString("// The TABLE wire (evolution-tolerant, docs/SPEC-TABLES.md): no serialize\n")
			h.WriteString("// dependency — includable from any TU.\n\n")
		}
		// The C spellings, as serialize.h uses them: <stdint.h>, not <cstdint>.
		// The relocatability and standard-layout asserts read the COMPILER
		// INTRINSICS, so <type_traits> — 124 headers on its own — is not here.
		h.WriteString("#pragma once\n\n#include <stdint.h>\n#include <string.h> // the prefill's scalar-array fills\n#include <stddef.h> // offsetof, for the reflection descriptors\n")
		if anyKeyed || anyList {
			// ENUM-KEYED arrays and UNBOUNDED arrays only: indexing a keyed array
			// by None, or a list past its count, is a program error in EVERY
			// configuration, and the accessor is where a runtime key or index can
			// first be caught (docs/SPEC-TABLES.md §2.4, §2.9). Those are the
			// runtime's only refusals, so they are the only reason these two
			// hooks are here.
			h.WriteString(tableHooks)
		}
		if anyVariable {
			// VARIABLE-LENGTH tables only: a unit of pointer-free tables pays
			// for none of these headers (docs/SPEC-TABLES.md §2, the zero-cost gate)
			h.WriteString(tableAllocatorHook)
			h.WriteString("#include <new> // a node's lifetime starts in arena storage (placement new)\n")
			h.WriteString("#include <atomic> // one atomic per slab: the arena is lock-free by ownership\n")
		}
		fmt.Fprintf(&h, "\n#include \"%s.h\"\n", f.Base)
		if unitHas128(u, closure) {
			// the 128-bit storage type is serialize's pair — native __int128
			// where the compiler has it, the emulated two-lane struct where
			// it does not (docs/SPEC-TABLES.md §3): the one thing a Table
			// header takes from serialize.h, and only a unit whose closure
			// declares a 128-bit field takes it
			h.WriteString("#include \"serialize.h\" // serialize::int128_t / uint128_t: the 128-bit storage\n")
		}
		names := make([]string, 0, len(g.includes))
		for n := range g.includes {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&h, "#include \"%sTable.h\"\n", n)
		}
		native := make([]string, 0, len(g.nativeIncludes))
		for n := range g.nativeIncludes {
			native = append(native, n)
		}
		sort.Strings(native)
		for _, n := range native {
			fmt.Fprintf(&h, "#include \"%s\"\n", n)
		}
		h.WriteString("\n")
		h.WriteString(tablePrimitives(u.Package, anyVariable, anyKeyed, anyExtent, ir.TableWireIdCapacity(u), u))
		if anyVariable {
			h.WriteString("\n")
			h.WriteString(tableArenaRuntime(u.Package, anyExtent))
			// the node table on the MESSAGE wire (docs/SPEC-TABLES.md §3.3),
			// spelled in terms of the numbering the arena runtime declares
			h.WriteString("\n")
			nodeGuard := strings.ToUpper(u.Package) + "_SCHEMA_TABLE_MESSAGE_NODES"
			h.WriteString("#ifndef " + nodeGuard + "\n#define " + nodeGuard + "\n\nnamespace " + u.Package + " {\n\n" + cppMessageNodeRuntime + "} // namespace " + u.Package + "\n\n#endif // " + nodeGuard + "\n")
		}
		if anyExtent {
			// the NODE EXTENT runtime (docs/SPEC-TABLES.md §2.8, §2.9): what a
			// map and an unbounded array share once the key and the sort are
			// taken out. Either makes its holder variable-length, so it always
			// follows the arena runtime it is spelled in terms of.
			h.WriteString("\n")
			h.WriteString(tableExtentRuntime(u.Package))
		}
		if anyMap {
			// the MAP runtime (docs/SPEC-TABLES.md §2.8): the storage type, the
			// order, the builder's head and segments, and the optional index.
			h.WriteString("\n")
			h.WriteString(tableMapRuntime(u.Package))
		}
		if anyList {
			// the LIST runtime (docs/SPEC-TABLES.md §2.9): the storage type and
			// its const surface, the builder's head and segments, the index-order
			// cursor and the load side's fill.
			h.WriteString("\n")
			h.WriteString(tableListRuntime(u.Package))
		}
		// the COOKED FORM's read side (docs/SPEC-TABLES.md §7) and the BUILD VERSION
		// it matches against, in EVERY unit that declares a table: every table
		// cooks and any table may be a cook's root, so there is no unit with
		// tables and no cook reader. It is not pointer machinery — no arena, no
		// builder, no reference slot — which is what keeps §2.2's question
		// answerable with the form still emitted.
		h.WriteString("\n")
		h.WriteString(buildVersionConstant(u.Package, ir.BuildVersion(u)))
		h.WriteString("\n")
		h.WriteString(tableCookRuntime(u.Package))
		if anyVariable {
			// the cook's WRITE side for a POINTERED root names the numbering,
			// so it follows both runtimes and only a pointered unit carries it
			h.WriteString("\n")
			h.WriteString(tableCookWriteVariableRuntime(u.Package))
		}
		fmt.Fprintf(&h, "\nnamespace %s {\n\n", u.Package)
		h.WriteString(g.body.String())
		fmt.Fprintf(&h, "} // namespace %s\n", u.Package)
		out[f.Base+"Table.h"] = []byte(h.String())

		// The TEXT form's runtime, in its own translation unit (owner's
		// ruling, docs/SPEC-TABLES.md §13.5): the generic walk, the pointer
		// adapters that say what the unit is (§16.7), and this file's
		// definitions, compiled ONCE by a project that uses them rather than
		// re-parsed by every translation unit that includes the header. A file
		// with no table member has nothing to define and emits no file, because
		// the generic-walk gate requires a walker in every generated .cpp.
		if len(members) > 0 {
			var c strings.Builder
			fmt.Fprintf(&c, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n// your choice. See the LICENSE exception in the schema compiler; the compiler is\n// AGPL-3.0, its output is not.\n", f.Base)
			fmt.Fprintf(&c, "// package %s — the TABLE wire's text form (docs/SPEC-TABLES.md §16).\n", u.Package)
			c.WriteString("// Compile this file to use <Name>FromJson / <Name>ToJson; a project that\n")
			c.WriteString("// never reads or writes a text does not compile it and pays nothing.\n\n")
			fmt.Fprintf(&c, "#include \"%sTable.h\"\n\n", f.Base)
			c.WriteString("#include <stdio.h> // the text form: number formatting\n")
			c.WriteString("#include <stdlib.h> // the text form: exact number conversion\n")
			c.WriteString("#include <locale.h> // the text form: the runtime's decimal point\n\n")
			c.WriteString(tableJsonWalk(u.Package, anyVariable, anyMap, anyList))
			fmt.Fprintf(&c, "\nnamespace %s {\n\n", u.Package)
			cg := &tableGen{unit: u, file: f, anyVariable: anyVariable, anyMap: anyMap, anyList: anyList, anyExtent: anyExtent, blocks: blocks, variable: variable, targets: targets,

				includes: map[string]bool{}, nativeIncludes: map[string]bool{}}
			for _, st := range members {
				cg.emitJsonDefinitions(st)
			}
			c.WriteString(cg.body.String())
			fmt.Fprintf(&c, "} // namespace %s\n", u.Package)
			out[f.Base+"Table.cpp"] = []byte(c.String())
		}
	}
	return out, nil
}

// orderTables returns a file's tables with every same-file table preceding
// its by-value users — schema references are order-free, C++ is not. Stable:
// declaration order survives wherever no dependency forces otherwise.
// (Cycles are refused by the checker; the fallback below is defensive.)
func orderTables(tables []*ir.Struct) []*ir.Struct {
	n := len(tables)
	byName := map[string]int{}
	for i, st := range tables {
		byName[st.Name] = i
	}
	adj := make([][]int, n)
	indeg := make([]int, n)
	for i, st := range tables {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			edge := func(name string) {
				if j, ok := byName[name]; ok && j != i {
					adj[j] = append(adj[j], i)
					indeg[i]++
				}
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				if ref.IsTable {
					edge(ref.Name)
				}
			case *ir.Union:
				// a table-armed union is emitted just before its holder, so the
				// holder follows every same-file table an arm names (§2.6)
				for _, v := range ref.Variants {
					if v.Ref != nil && v.Ref.IsTable {
						edge(v.Type)
					}
				}
			}
		}
	}
	order := make([]*ir.Struct, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		pick := -1
		for i := range n {
			if !done[i] && indeg[i] == 0 {
				pick = i
				break
			}
		}
		if pick == -1 {
			for i := range n {
				if !done[i] {
					pick = i
					break
				}
			}
		}
		done[pick] = true
		order = append(order, tables[pick])
		for _, t := range adj[pick] {
			indeg[t]--
		}
	}
	return order
}

// unitHas128 reports whether any closure member declares a 128-bit field —
// an int128, a uint128, or a fixed of 128 bits — which is what decides whether
// the Table header includes serialize.h for the storage type.
func unitHas128(u *ir.Unit, closure map[string]bool) bool {
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Width == 128 && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) {
				return true
			}
		}
	}
	return false
}
