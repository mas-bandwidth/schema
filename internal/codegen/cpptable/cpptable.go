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
// Plain byte code with NO serialize dependency, so a Table header is
// includable from any translation unit; the encode surface is a
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
)

func tableScalarKind(f *ir.Field) int { return ir.TableScalarKind(f) }

func tableKindWidth(kind int) int {
	switch kind {
	case tkBool, tkI8, tkU8:
		return 1
	case tkI16, tkU16:
		return 2
	case tkI32, tkU32, tkF32:
		return 4
	case tkI64, tkU64, tkF64:
		return 8
	}
	return 0
}

func tablePut(width int) string { return fmt.Sprintf("put%d", width*8) }
func tableGet(width int) string { return fmt.Sprintf("get%d", width*8) }

type tableGen struct {
	unit        *ir.Unit
	file        *ir.File
	anyVariable bool // the unit declares at least one variable-length table
	anyKeyed    bool // the unit declares at least one enum-keyed array
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

func tablePrimitives(pkg string, anyVariable bool, anyKeyed bool) string {
	keyedStorage := ""
	if anyKeyed {
		keyedStorage = tableKeyedStorage
	}
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_PRIMITIVES"
	forceInline := tableInlineMacro(pkg)
	// the two pointer-era descriptor members exist only in a unit that HAS
	// pointers: a unit of value-only tables emits the descriptor surface it
	// always emitted, to the byte (docs/SPEC-TABLES.md §2, the zero-cost gate)
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

// The table-wire read report — the permissive contract's ledger. Silence
// (all zero) means the data matched this reader's schema exactly.
struct TableReport
{
    int32_t unknown = 0;       // unknown field ids skipped (newer data)
    int32_t kind_mismatch = 0; // known id, changed type — skipped, never misdecoded
    int32_t clamped = 0;       // out-of-range values clamped to declared bounds
    // a key the TEXT form saw twice: last wins, and the repeat is counted
    // (docs/SPEC-TABLES.md §16.2). The wire never raises it — a body carrying an
    // id twice is legal input whose last occurrence wins, silently (§3).
    int32_t duplicate = 0;
    bool malformed = false;    // framing damage; decode stopped, partial result kept
};

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
struct TableUnionArmInfo
{
    uint32_t offset;             // offsetof the arm's payload within the union storage
    const TableTypeInfo * table; // the arm payload's descriptor
};

// A union field's shape: the tag, and the arms indexed by it. Arms run
// [0, enum_max]; index 0 is the EMPTY arm and carries no payload.
struct TableUnionInfo
{
    uint32_t tag_offset; // offsetof the tag within the union storage
    uint32_t tag_size;   // sizeof the tag
    const TableUnionArmInfo * arms;
};

` + pointerForward + `struct TableFieldInfo
{
    const char * name;      // schema field name, e.g. "health"
    const char * json;      // the TEXT form's key: the json = "key" attribute, else name (§16.3)
    const char * type_name; // schema type name, e.g. "float32", "Grade"
    uint16_t id;            // table-wire field id (name hash; the was alias's hash after a rename)
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
    uint16_t (*variant_id)( uint64_t value );
    // an ENUM-KEYED array (docs/SPEC-TABLES.md §2.4): the array has one slot per
    // variant of key_type_name, indexed by the variant's value, and its slots
    // ride under variant ids rather than positions. key_name and key_id are
    // the key's vocabulary — walk [0, array_bound) to print slots by name.
    // NULL on every other field.
    const char * key_type_name;
    const char * (*key_name)( uint64_t value );
    uint16_t (*key_id)( uint64_t value );
    // union fields: the tag and its arms, behind a function so the whole
    // descriptor stays CONSTANT-INITIALISED (a captureless lambda converts to
    // a function pointer at compile time; the arms themselves are a static
    // inside it). NULL for every other kind.
    const TableUnionInfo * (*arms)();
    const char * guard;     // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded
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
    ` + forceInline + ` void patch32( int64_t at, uint32_t v )
    {
        if ( at + 4 > capacity ) { overflow = true; return; }
        buffer[at] = uint8_t( v ); buffer[at+1] = uint8_t( v >> 8 );
        buffer[at+2] = uint8_t( v >> 16 ); buffer[at+3] = uint8_t( v >> 24 );
    }
};

struct TableReader
{
    const uint8_t * buffer;
    int64_t size;
    int64_t offset = 0;
    TableReport * report;

    TableReader( const uint8_t * from_buffer, int64_t from_size, TableReport * to_report )
        : buffer( from_buffer ), size( from_size ), report( to_report ) {}

    ` + forceInline + ` bool has( int64_t bytes ) const { return offset + bytes <= size; }
    ` + forceInline + ` uint8_t get8()   { return buffer[offset++]; }
    ` + forceInline + ` uint16_t get16() { uint16_t v = uint16_t( buffer[offset] ) | uint16_t( buffer[offset+1] ) << 8; offset += 2; return v; }
    ` + forceInline + ` uint32_t get32() { uint32_t v = uint32_t( buffer[offset] ) | uint32_t( buffer[offset+1] ) << 8 | uint32_t( buffer[offset+2] ) << 16 | uint32_t( buffer[offset+3] ) << 24; offset += 4; return v; }
    ` + forceInline + ` uint64_t get64() { uint64_t lo = get32(); uint64_t hi = get32(); return lo | ( hi << 32 ); }

    // skip one payload by kind; false = framing damage
    bool skip( uint8_t kind )
    {
        switch ( kind )
        {
            case 1: case 2: case 6: return has( 1 ) ? ( offset += 1, true ) : false;
            case 3: case 7:         return has( 2 ) ? ( offset += 2, true ) : false;
            case 4: case 8: case 10: case 17: return has( 4 ) ? ( offset += 4, true ) : false; // 17 is a NODE INDEX (docs/SPEC-TABLES.md §3.1)
            case 5: case 9: case 11: return has( 8 ) ? ( offset += 8, true ) : false;
            case 12: case 13: case 14: case 16:
            {
                if ( !has( 4 ) ) return false;
                uint32_t n = get32();
                return has( n ) ? ( offset += n, true ) : false;
            }
            case 15: // union: u16 arm id, then the arm length-prefixed (id 0 = empty, no body)
            {
                if ( !has( 2 ) ) return false;
                if ( get16() == 0 ) return true;
                if ( !has( 4 ) ) return false;
                uint32_t n = get32();
                return has( n ) ? ( offset += n, true ) : false;
            }
        }
        return false;
    }
};

` + keyedStorage + `inline float table_bits_to_float( uint32_t bits ) { float f; memcpy( &f, &bits, 4 ); return f; }
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
	blocks := ir.Blocks(u)

	// The BLOCK FORM (docs/SPEC-TABLES.md §19) is emitted ON THE SIDE, into
	// <Base>Block.h and <Base>Block.cpp: nothing declares it, every fixed
	// table has one, and a consumer includes and compiles it only if it uses
	// the form. The Table header below carries not one symbol of it.
	out := generateBlockFiles(u, blocks, variable, targets)
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, anyVariable: anyVariable, anyKeyed: anyKeyed, blocks: blocks, variable: variable, targets: targets,
			includes: map[string]bool{}, nativeIncludes: map[string]bool{}}
		var members []*ir.Struct
		members = append(members, orderTables(f.Tables)...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if len(members) > 0 {
			for _, st := range members {
				if st.IsTable {
					g.emitTableStruct(st)
				}
			}
			for _, e := range tableEnums(members) {
				g.emitEnumIdentity(e)
			}
			g.emitTableResetDeclarations(members)
			for _, st := range members {
				g.emitTableReset(st)
			}
			g.emitCodecDeclarations(members)
			for _, st := range members {
				g.owner = st
				g.emitTableMeasure(st)
				g.emitTableWrite(st)
				g.emitTableSave(st)
				g.emitTableRead(st)
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
			g.pf("// no tables declared or referenced in this file — codecs are emitted\n")
			g.pf("// for the table closure only (`table` declarations and what they reach)\n")
		}
		var h strings.Builder
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n// your choice. See the LICENSE exception in the schema compiler; the compiler is\n// AGPL-3.0, its output is not.\n", f.Base)
		fmt.Fprintf(&h, "// package %s — protocol id 0x%016x (packets only: tables version by field id, not by protocol id)\n", u.Package, u.ProtocolId)
		h.WriteString("// The TABLE wire (evolution-tolerant, docs/SPEC-TABLES.md): no serialize\n")
		h.WriteString("// dependency — includable from any TU.\n\n")
		// The C spellings, as serialize.h uses them: <stdint.h>, not <cstdint>.
		// The relocatability and standard-layout asserts read the COMPILER
		// INTRINSICS, so <type_traits> — 124 headers on its own — is not here.
		h.WriteString("#pragma once\n\n#include <stdint.h>\n#include <string.h> // the prefill's scalar-array fills\n#include <stddef.h> // offsetof, for the reflection descriptors\n")
		if anyKeyed {
			// ENUM-KEYED arrays only: indexing one by None is a program error in
			// EVERY configuration, and the accessor is where a runtime key can
			// first be caught (docs/SPEC-TABLES.md §2.4). It is the runtime's
			// only refusal, so it is the only reason these two hooks are here.
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
		h.WriteString(tablePrimitives(u.Package, anyVariable, anyKeyed))
		if anyVariable {
			h.WriteString("\n")
			h.WriteString(tableArenaRuntime(u.Package))
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
			c.WriteString(tableJsonWalk(u.Package, anyVariable))
			fmt.Fprintf(&c, "\nnamespace %s {\n\n", u.Package)
			cg := &tableGen{unit: u, file: f, anyVariable: anyVariable, blocks: blocks, variable: variable, targets: targets,
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
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
				if j, ok := byName[ref.Name]; ok && j != i {
					adj[j] = append(adj[j], i)
					indeg[i]++
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
