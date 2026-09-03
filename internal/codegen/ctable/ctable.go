// Package ctable emits <Base>Table.h and <Base>Table.c — the TABLE-wire C
// codecs (docs/SPEC-TABLES.md). One header/source pair per unit file, emitted
// only when the unit declares tables: storage structs for the `table`
// declarations, then measure/save/load codecs and reflection descriptors for
// the whole TABLE CLOSURE (every table plus everything one references,
// transitively).
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
//
// # WHAT IS DIFFERENT ABOUT THIS TARGET
//
// The reference is internal/codegen/cpptable and this port mirrors it: the
// same wire, the same report, the same descriptor CONTENT, the same refusals.
// Five spellings differ, because C has no C++ to spell them with, and each is
// the C form of one C++ mechanism rather than a new idea.
//
//   - NO OVERLOAD SET FOR ENUM IDENTITY. C++ emits one TableEnumId/
//     TableEnumValue pair per enum and finds it by overload resolution. C has
//     no overloading, so the switch is emitted AT THE USE SITE — the same
//     switch, inline, resolving no call. That claims not one name per enum,
//     which is the property §11 asks of a port.
//
//   - THE DESCRIPTOR'S VOCABULARY COLUMNS ARE DATA, NOT FUNCTIONS. C++ spells
//     an enum's names and wire ids as captureless lambdas; C has none, and a
//     named function per enum would claim a name per enum. The same facts ride
//     as a static table of (name, id) indexed the way enum_max bounds — one
//     array per owning record, reached through the field descriptor. Every
//     question §8 asks of a descriptor still has an answer, and the walk asks
//     it of an array instead of a call.
//
//   - THE KEYED ARRAY IS A PLAIN ARRAY. C++ wraps the slots in TableKeyed<T,E>
//     for operator[] and its None refusal. C's storage is `T slots[E.Max]`
//     directly and the refusal lives in SCHEMA_TABLE_KEYED_AT, a unit-level macro over
//     table_keyed_slot — assert plus abort, in EVERY build, exactly as C++'s
//     accessor refuses (docs/SPEC-TABLES.md §2.4).
//
//   - THE HEADER/SOURCE SPLIT IS WIDER. C++17 has inline variables, so its
//     descriptors live in the header. C has none: the descriptors and the text
//     form's walk are DEFINED in <Base>Table.c and only declared in the
//     header, so a translation unit that includes the header for the codecs
//     pays for neither. Compiling <Base>Table.c is what a consumer of the
//     descriptors or the text form does.
//
//   - THE LINKER AND THE PREPROCESSOR ARE TWO MORE NAMESPACES, and C++ has
//     neither problem. Every external this backend emits carries the package —
//     schema_<package>_<type>_<what>_ — because two units whose type names
//     collide have to LINK together, which is what the conformance driver does
//     with two generations of one schema; the name-first surface §11 states is
//     `static` in the header and forwards to them. And every MACRO it defines
//     leads with SCHEMA_, because a schema's constants, enum variants and flag
//     masks are #defines in this target and a collision there is a silent
//     rewrite rather than a redeclaration error (internal/check's
//     cReservedMacros refuses a declaration that spells one).
package ctable

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

func tablePut(width int) string { return fmt.Sprintf("table_writer_put%d", width*8) }
func tableGet(width int) string { return fmt.Sprintf("table_reader_get%d", width*8) }

type tableGen struct {
	unit        *ir.Unit
	file        *ir.File
	anyVariable bool // the unit declares at least one variable-length table
	anyKeyed    bool // the unit declares at least one enum-keyed array
	// blocks is the unit's BLOCK FORM surface (docs/SPEC-TABLES.md §19), nil when
	// no table is marked `| block`. Nil is what makes the zero-cost gate
	// answerable by asking one question (§2.2).
	blocks   *ir.BlockUnit
	owner    *ir.Struct      // the closure member whose codec is being emitted
	variable map[string]bool // the derived VARIABLE-LENGTH members (ir.VariableTables)
	targets  map[string]bool // tables some pointer targets (ir.PointerTargets)
	body     strings.Builder
	includes map[string]bool // referenced files -> #include "<base>Table.h"
	indent   string          // extra per-line indent while emitting inside a branch guard
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

// unitHasKeyedArray reports whether any closure member declares an enum-keyed
// array, which is what decides whether the unit's Table.h carries the keyed
// accessor and its <assert.h>.
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

// tableKeyedAccessor is the ENUM-KEYED array's accessor, emitted only into a
// unit that declares one (docs/SPEC-TABLES.md §2.4).
//
// C's storage IS the array — `T slots[E_MAX]`, the key k at index k-1 — so
// there is no wrapper type to emit and no size parameter to spell: the extent
// comes from the key enum's own _MAX and from nowhere else. What C++ puts in
// operator[] lives here instead.
//
// NONE IS THE NULL KEY: it names no slot, it never rides on the wire, a stored
// key of 0 is malformed, and INDEXING BY IT IS A PROGRAM ERROR IN EVERY
// CONFIGURATION — REFUSED UNCONDITIONALLY. NDEBUG does not remove the compare:
// there is NO UB PATH here in any build. The assert carries the message where a
// debugger can read it and NDEBUG removes that; the abort is what stands after
// it. The cost is one perfectly-predicted compare, on a path that reads config.
const tableKeyedAccessor = `
/* The storage index a key names, with the None refusal that stands in EVERY
   build. The storage shifts left and holds no slot for None, so a build that
   skipped this compare would index one element BEFORE the array — undefined
   behaviour in the configuration a game ships. */
static SCHEMA_UNUSED int32_t table_keyed_slot( int32_t key )
{
    if ( key == 0 )
    {
        assert( 0 && "None is the null key of an enum-keyed array: it keys no slot" );
        abort();
    }
    return key - 1;
}

/* keyed[key] — the slot a variant owns, as an LVALUE. The key is evaluated
   once. ITERATION is the surface a consumer of the whole array wants: walk
   1..E_MAX and index with the key, so a call site writes no shift. */
#define SCHEMA_TABLE_KEYED_AT( array, key ) ( (array)[ table_keyed_slot( (int32_t) ( key ) ) ] )

`

// tablePrimitives is the shared runtime, emitted into every Table.h behind a
// per-package guard — one definition per TU whatever the include order, and a
// lone Table.h works standalone.
func tablePrimitives(pkg string, anyVariable bool, anyKeyed bool) string {
	keyed := ""
	if anyKeyed {
		keyed = tableKeyedAccessor
	}
	guard := "SCHEMA_" + strings.ToUpper(pkg) + "_TABLE_PRIMITIVES"
	forceInline := tableInlineMacro(pkg)
	// the two pointer-era descriptor members exist only in a unit that HAS
	// pointers: a unit of value-only tables emits the descriptor surface it
	// always emitted, to the byte (docs/SPEC-TABLES.md §2, the zero-cost gate)
	pointerFieldMember, pointerTypeMember := "", ""
	if anyVariable {
		pointerFieldMember = "\n    int is_pointer;         /* a *T pointer field: storage is an 8-byte TableRef; the target is a table */"
		pointerTypeMember = "\n    /* the DERIVED mode (docs/SPEC-TABLES.md): 0 = fixed-size, a plain\n" +
			"       relocatable struct; 1 = variable-length, built through a builder\n" +
			"       and read through a region root. Nobody declares it; the compiler\n" +
			"       works it out. */\n    int variable;"
	}
	return `#ifndef ` + guard + `
#define ` + guard + `

/* THE CODEC DOES NOT DEPEND ON THE COMPILER'S INLINING BUDGET. A table of a
   realistic field count emits one large body per type, and the cursor a body
   writes through lives in the caller's TableWriter: across a call boundary that
   cursor round-trips through memory, and a uint8_t * store may alias the writer
   itself, so every put reloads it. When a budget runs out mid-body the codec
   silently degrades to that shape. Forcing the primitives and the fixed-class
   bodies inline is what keeps the cursor in registers and lets adjacent
   constant framing bytes merge into one store.

   The spelling is FEATURE TESTED rather than assumed, exactly as the packet
   emitter's own inlining demand is (SPEC §6.1's C column).

   IT CARRIES THE PACKAGE for the reason every other name here does, and that
   reason is the LINKER rather than the preprocessor: two units of the same
   schema family link into one program — two generations of one schema is the
   case the conformance driver itself is — so nothing this backend emits may
   be spelled the same way by two packages. They do NOT share a translation
   unit and cannot: C has no namespace, so two units' headers in one TU
   redefine TableReport, TableWriter and every other runtime name, which is the
   packet emitter's standing limit (SPEC §6.1) and not something tables
   changed. A per-package macro is what keeps the LINK legal. */
#ifndef ` + forceInline + `
#if defined( _MSC_VER )
#define ` + forceInline + ` __forceinline
#elif defined( __GNUC__ ) || defined( __clang__ )
#define ` + forceInline + ` inline __attribute__(( always_inline ))
#else
#define ` + forceInline + ` inline
#endif
#endif

/* A COMPILE-TIME assertion under -std=c99, which has none of its own. C11's
   _Static_assert carries the message into the build log verbatim and is used
   wherever the compiler has it; everywhere else the negative array bound is
   the same refusal with a tag a reader can find. The tag is unique per record
   and field, and each record is asserted by the ONE header that declares it,
   so no two of these ever meet. */
#ifndef SCHEMA_TABLE_STATIC_ASSERT
#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L
#define SCHEMA_TABLE_STATIC_ASSERT( tag, cond, message ) _Static_assert( cond, message )
#elif defined( __cplusplus ) && __cplusplus >= 201103L
#define SCHEMA_TABLE_STATIC_ASSERT( tag, cond, message ) static_assert( cond, message )
#else
#define SCHEMA_TABLE_STATIC_ASSERT( tag, cond, message ) typedef char schema_table_assert_##tag[ ( cond ) ? 1 : -1 ]
#endif
#endif

/* A record's ALIGNMENT, which the cook's layout contract asserts and which the
   cook's Open compares the header's alignment word against (§7.1, §20.3).
   C99 has no alignof either; C11's _Alignof, the two GNU spellings and MSVC's
   cover every compiler this repo builds under, and the last form is the
   portable definition of the same number. */
#ifndef SCHEMA_TABLE_ALIGNOF
#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L
#define SCHEMA_TABLE_ALIGNOF( T ) _Alignof( T )
#elif defined( __GNUC__ ) || defined( __clang__ )
#define SCHEMA_TABLE_ALIGNOF( T ) __alignof__( T )
#elif defined( _MSC_VER )
#define SCHEMA_TABLE_ALIGNOF( T ) __alignof( T )
#else
#define SCHEMA_TABLE_ALIGNOF( T ) ( (size_t) offsetof( struct { char schema_pad_; T schema_member_; }, schema_member_ ) )
#endif
#endif

/* The table-wire read report — the permissive contract's ledger. Silence
   (all zero) means the data matched this reader's schema exactly. */
typedef struct TableReport
{
    int32_t unknown;       /* unknown field ids skipped (newer data) */
    int32_t kind_mismatch; /* known id, changed type — skipped, never misdecoded */
    int32_t clamped;       /* out-of-range values clamped to declared bounds */
    /* a key the TEXT form saw twice: last wins, and the repeat is counted
       (docs/SPEC-TABLES.md §16.2). The wire never raises it — a body carrying an
       id twice is legal input whose last occurrence wins, silently (§3). */
    int32_t duplicate;
    int malformed;         /* framing damage; decode stopped, partial result kept */
} TableReport;

/* ---- reflection (tables only, docs/SPEC-TABLES.md) ----

   Static field descriptors for every type in the table closure: name, wire
   id/kind, storage offset, bounds, ranges, enum names and branch guards —
   enough to walk, print, diff, edit or bind any table value at runtime with
   no schema files. <name>_table_type() returns <Name>'s descriptor. */

struct TableTypeInfo;

/* One arm of a union field: where its payload sits inside the union's storage
   and what its payload looks like. The arm's NAME and its table-wire id ride
   in the field's own vocabulary table at the same tag, so nothing is spelled
   twice (docs/SPEC-TABLES.md §8). */
typedef struct TableUnionArmInfo
{
    uint32_t offset;                    /* offsetof the arm's payload within the union storage */
    const struct TableTypeInfo * table; /* the arm payload's descriptor */
} TableUnionArmInfo;

/* A union field's shape: the tag, and the arms indexed by it. Arms run
   [0, enum_max]; index 0 is the EMPTY arm and carries no payload. */
typedef struct TableUnionInfo
{
    uint32_t tag_offset; /* offsetof the tag within the union storage */
    uint32_t tag_size;   /* sizeof the tag */
    const TableUnionArmInfo * arms;
} TableUnionInfo;

/* ONE ENTRY OF A VOCABULARY, and the whole of what C++ spells as a pair of
   captureless lambdas. An enum's values, a union's arms and a flags field's
   BITS are each a named set indexed by [0, enum_max], so the set rides as an
   ARRAY indexed the same way: no function per enum, and therefore no name per
   enum for a schema to collide with (docs/SPEC-TABLES.md §11).

   name is the variant's spelling, or NULL for a value the declared set does
   not name. id is the TABLE-WIRE id it rides under (§5) — the hash of the
   variant's name, 0 for the reserved None and for the empty union arm. A
   FLAGS field's variants are BIT POSITIONS and have no per-variant wire id
   (§4), so has_ids is 0 there and 1 for an enum and a union: a vocabulary
   with names and no ids is what says "flags". */
typedef struct TableVariantInfo
{
    const char * name;
    uint16_t id;
} TableVariantInfo;

typedef struct TableFieldInfo
{
    const char * name;      /* schema field name, e.g. "health" */
    const char * json;      /* the TEXT form's key: the json = "key" attribute, else name (§16.3) */
    const char * type_name; /* schema type name, e.g. "float32", "Grade" */
    uint16_t id;            /* table-wire field id (name hash; the was alias's hash after a rename) */
    uint8_t kind;           /* table-wire kind; for arrays/strings/bytes, the ELEMENT kind */
    int is_array;           /* fixed or counted array (bytes included) */` + pointerFieldMember + `
    int counted;            /* a _count/_length int32 companion exists (counted arrays, strings, bytes) */
    int optional;           /* a ?T field: a _present companion decides whether it rides */
    int32_t array_bound;    /* array capacity / string max length; 0 for plain scalars */
    uint32_t offset;        /* offsetof the storage member */
    uint32_t elem_size;     /* sizeof the member (element size for arrays) */
    uint32_t count_offset;  /* offsetof the _count/_length companion, or 0xffffffff */
    uint32_t present_offset; /* offsetof the _present companion, or 0xffffffff */
    const struct TableTypeInfo * table; /* nested table's descriptor, or NULL */
    int has_range;          /* a declared [min, max] (int or float) */
    double range_min;       /* NOTE: int64 ranges beyond 2^53 lose precision here */
    double range_max;
    int64_t enum_max;       /* enums: highest valid value (None = 0 always valid);
                               unions: the arm count (tag range [0, enum_max]);
                               flags: the highest declared BIT INDEX; else -1 */
    /* the vocabulary, indexed the same way enum_max bounds: an enum's value ->
       (name, id), a union's tag -> (arm name, arm id), a FLAGS field's bit
       index -> (variant name, 0). NULL for every other kind. */
    const TableVariantInfo * variants;
    int has_variant_ids;    /* 1 for an enum or a union, 0 for flags (§4) */
    /* an ENUM-KEYED array (docs/SPEC-TABLES.md §2.4): the array has one slot per
       variant of key_type_name, the key k at index k-1, and its slots ride
       under variant ids rather than positions. keys is the key's vocabulary,
       indexed by the KEY — walk [0, array_bound) and ask about index + 1 to
       print slots by name. NULL on every other field. */
    const char * key_type_name;
    const TableVariantInfo * keys;
    int64_t key_max;        /* the key enum's highest value, bounding keys */
    /* union fields: the tag and its arms. NULL for every other kind. */
    const TableUnionInfo * arms;
    const char * guard;     /* branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded */
} TableFieldInfo;

typedef struct TableTypeInfo
{
    const char * name;   /* schema type name */
    uint32_t size;       /* sizeof the storage struct */
    int32_t num_fields;
    const TableFieldInfo * fields;
    /* put one instance back at its declared defaults, in place. A generic
       walker that fills a value has to be able to establish the defaults an
       absent field takes, and it holds no type to spell — this is the one
       thing the descriptors could not express without it. It is the void *
       form of <name>_reset and the same code. */
    void (*reset)( void * storage );` + pointerTypeMember + `
} TableTypeInfo;

typedef struct TableWriter
{
    uint8_t * buffer;
    int64_t capacity;
    int64_t offset;
    int overflow;
} TableWriter;

static SCHEMA_UNUSED TableWriter table_writer_make( uint8_t * buffer, int64_t capacity )
{
    TableWriter w;
    w.buffer = buffer;
    w.capacity = capacity;
    w.offset = 0;
    w.overflow = 0;
    return w;
}

static SCHEMA_UNUSED ` + forceInline + ` void table_writer_raw( TableWriter * w, const void * data, int64_t bytes )
{
    if ( w->offset + bytes > w->capacity ) { w->overflow = 1; return; }
    memcpy( w->buffer + w->offset, data, (size_t) bytes );
    w->offset += bytes;
}
static SCHEMA_UNUSED ` + forceInline + ` void table_writer_put8( TableWriter * w, uint8_t v ) { table_writer_raw( w, &v, 1 ); }
static SCHEMA_UNUSED ` + forceInline + ` void table_writer_put16( TableWriter * w, uint16_t v )
{
    uint8_t b[2];
    b[0] = (uint8_t) v; b[1] = (uint8_t) ( v >> 8 );
    table_writer_raw( w, b, 2 );
}
static SCHEMA_UNUSED ` + forceInline + ` void table_writer_put32( TableWriter * w, uint32_t v )
{
    uint8_t b[4];
    b[0] = (uint8_t) v; b[1] = (uint8_t) ( v >> 8 ); b[2] = (uint8_t) ( v >> 16 ); b[3] = (uint8_t) ( v >> 24 );
    table_writer_raw( w, b, 4 );
}
static SCHEMA_UNUSED ` + forceInline + ` void table_writer_put64( TableWriter * w, uint64_t v )
{
    table_writer_put32( w, (uint32_t) v );
    table_writer_put32( w, (uint32_t) ( v >> 32 ) );
}
static SCHEMA_UNUSED ` + forceInline + ` void table_writer_patch32( TableWriter * w, int64_t at, uint32_t v )
{
    if ( at + 4 > w->capacity ) { w->overflow = 1; return; }
    w->buffer[at] = (uint8_t) v; w->buffer[at+1] = (uint8_t) ( v >> 8 );
    w->buffer[at+2] = (uint8_t) ( v >> 16 ); w->buffer[at+3] = (uint8_t) ( v >> 24 );
}

typedef struct TableReader
{
    const uint8_t * buffer;
    int64_t size;
    int64_t offset;
    TableReport * report;
} TableReader;

static SCHEMA_UNUSED TableReader table_reader_make( const uint8_t * buffer, int64_t size, TableReport * report )
{
    TableReader r;
    r.buffer = buffer;
    r.size = size;
    r.offset = 0;
    r.report = report;
    return r;
}

static SCHEMA_UNUSED ` + forceInline + ` int table_reader_has( const TableReader * r, int64_t bytes ) { return r->offset + bytes <= r->size; }
static SCHEMA_UNUSED ` + forceInline + ` uint8_t table_reader_get8( TableReader * r ) { return r->buffer[r->offset++]; }
static SCHEMA_UNUSED ` + forceInline + ` uint16_t table_reader_get16( TableReader * r )
{
    uint16_t v = (uint16_t) ( (uint16_t) r->buffer[r->offset] | ( (uint16_t) r->buffer[r->offset+1] << 8 ) );
    r->offset += 2;
    return v;
}
static SCHEMA_UNUSED ` + forceInline + ` uint32_t table_reader_get32( TableReader * r )
{
    uint32_t v = (uint32_t) r->buffer[r->offset] | ( (uint32_t) r->buffer[r->offset+1] << 8 )
               | ( (uint32_t) r->buffer[r->offset+2] << 16 ) | ( (uint32_t) r->buffer[r->offset+3] << 24 );
    r->offset += 4;
    return v;
}
static SCHEMA_UNUSED ` + forceInline + ` uint64_t table_reader_get64( TableReader * r )
{
    uint64_t lo = table_reader_get32( r );
    uint64_t hi = table_reader_get32( r );
    return lo | ( hi << 32 );
}

/* skip one payload by kind; 0 = framing damage */
static SCHEMA_UNUSED int table_reader_skip( TableReader * r, uint8_t kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6:
            if ( !table_reader_has( r, 1 ) ) { return 0; }
            r->offset += 1; return 1;
        case 3: case 7:
            if ( !table_reader_has( r, 2 ) ) { return 0; }
            r->offset += 2; return 1;
        /* 17 is a NODE INDEX (docs/SPEC-TABLES.md 3.1): four bytes, so it costs
           one row here and a reader without the kind still skips a pointer field */
        case 4: case 8: case 10: case 17:
            if ( !table_reader_has( r, 4 ) ) { return 0; }
            r->offset += 4; return 1;
        case 5: case 9: case 11:
            if ( !table_reader_has( r, 8 ) ) { return 0; }
            r->offset += 8; return 1;
        case 12: case 13: case 14: case 16:
        {
            uint32_t n;
            if ( !table_reader_has( r, 4 ) ) { return 0; }
            n = table_reader_get32( r );
            if ( !table_reader_has( r, n ) ) { return 0; }
            r->offset += n;
            return 1;
        }
        case 15: /* union: u16 arm id, then the arm length-prefixed (id 0 = empty, no body) */
        {
            uint32_t n;
            if ( !table_reader_has( r, 2 ) ) { return 0; }
            if ( table_reader_get16( r ) == 0 ) { return 1; }
            if ( !table_reader_has( r, 4 ) ) { return 0; }
            n = table_reader_get32( r );
            if ( !table_reader_has( r, n ) ) { return 0; }
            r->offset += n;
            return 1;
        }
        default: break;
    }
    return 0;
}
` + keyed + `
static SCHEMA_UNUSED float table_bits_to_float( uint32_t bits ) { float f; memcpy( &f, &bits, 4 ); return f; }
static SCHEMA_UNUSED uint32_t table_float_to_bits( float f ) { uint32_t b; memcpy( &b, &f, 4 ); return b; }
static SCHEMA_UNUSED double table_bits_to_double( uint64_t bits ) { double d; memcpy( &d, &bits, 8 ); return d; }
static SCHEMA_UNUSED uint64_t table_double_to_bits( double d ) { uint64_t b; memcpy( &b, &d, 8 ); return b; }

#endif /* ` + guard + ` */
`
}

// Generate emits <Base>Table.h and <Base>Table.c for every unit file when the
// unit declares tables, and nothing when it does not — a table-free unit's
// generated tree is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[f.Base] = true
	}
	for _, f := range u.Files {
		if bases[f.Base+"Table"] {
			return nil, fmt.Errorf("schema files %s and %sTable collide — the C table emitter writes %sTable.h as %s's table header; rename one file (docs/SPEC-TABLES.md)", f.Base, f.Base, f.Base, f.Base)
		}
	}
	closure := ir.TableClosure(u)
	variable := ir.VariableTables(u)
	targets := ir.PointerTargets(u)
	anyVariable := len(variable) > 0
	anyKeyed := unitHasKeyedArray(u, closure)
	blocks := ir.Blocks(u)

	// The BLOCK FORM (docs/SPEC-TABLES.md §19) is emitted ON THE SIDE, into
	// <Base>Block.h and <Base>Block.c: nothing declares it, every fixed
	// table has one, and a consumer includes and compiles it only if it uses
	// the form. The Table header below carries not one symbol of it.
	out := generateBlockFiles(u, blocks, variable, targets)
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, anyVariable: anyVariable, anyKeyed: anyKeyed, blocks: blocks, variable: variable, targets: targets,
			includes: map[string]bool{}}
		var members []*ir.Struct
		members = append(members, orderTables(f.Tables)...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		cg := &tableGen{unit: u, file: f, anyVariable: anyVariable, anyKeyed: anyKeyed, blocks: blocks, variable: variable, targets: targets,
			includes: map[string]bool{}}
		if len(members) > 0 {
			for _, st := range members {
				if st.IsTable {
					g.emitTableStruct(st)
				}
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
			g.emitRelocatabilityPreamble()
			g.emitCookLayoutAsserts(members)
			g.pf("/* ---- reflection descriptors (tables only, docs/SPEC-TABLES.md) ----\n\n")
			g.pf("   The descriptors are CONSTANT data DEFINED in %sTable.c: a translation\n", f.Base)
			g.pf("   unit that includes this header for the wire codecs pays nothing for\n")
			g.pf("   them, and one definition per program means one address per type. The\n")
			g.pf("   whole reflection surface is immutable — read it from any thread, any\n")
			g.pf("   time. */\n\n")
			for _, st := range members {
				g.pf("extern const TableTypeInfo %s;\n", g.sym(st.Name, "info"))
			}
			g.pf("\n")
			for _, st := range members {
				g.pf("static SCHEMA_UNUSED const TableTypeInfo * %s( void ) { return &%s; }\n", g.api(st.Name, "table_type"), g.sym(st.Name, "info"))
			}
			g.pf("\n")
			// the TEXT form's surface (docs/SPEC-TABLES.md §16), DECLARED here
			// and defined in <Base>Table.c beside the one generic walk it
			// calls.
			g.pf("/* ---- the text form (docs/SPEC-TABLES.md §16) ---- */\n\n")
			for _, st := range members {
				g.emitJsonDeclarations(st)
			}
			for _, st := range members {
				cg.owner = st
				cg.emitTableDescriptor(st)
			}
			for _, st := range members {
				cg.emitJsonDefinitions(st)
			}
		} else {
			g.pf("/* no tables declared or referenced in this file — codecs are emitted\n")
			g.pf("   for the table closure only (`table` declarations and what they reach) */\n")
		}
		out[f.Base+"Table.h"] = g.header(u, f, members)
		if len(members) > 0 {
			out[f.Base+"Table.c"] = tableSource(u, f, g, cg, members)
		}
	}
	return out, nil
}

// header assembles <Base>Table.h.
func (g *tableGen) header(u *ir.Unit, f *ir.File, members []*ir.Struct) []byte {
	var h strings.Builder
	guard := "SCHEMA_" + strings.ToUpper(u.Package) + "_" + strings.ToUpper(f.Base) + "TABLE_H"
	fmt.Fprintf(&h, "/* Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n   SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n   your choice. See the LICENSE exception in the schema compiler; the compiler is\n   AGPL-3.0, its output is not.\n", f.Base)
	fmt.Fprintf(&h, "   package %s — protocol id 0x%016x (packets only: tables version by field id, not by protocol id)\n", u.Package, u.ProtocolId)
	h.WriteString("   The TABLE wire (evolution-tolerant, docs/SPEC-TABLES.md): no serialize\n")
	h.WriteString("   dependency — includable from any TU. Compile the .c beside this header\n")
	h.WriteString("   to use the reflection descriptors or the text form. */\n\n")
	fmt.Fprintf(&h, "#ifndef %s\n#define %s\n\n", guard, guard)
	h.WriteString("#include <stdint.h>\n#include <stddef.h> /* offsetof, for the reflection descriptors */\n#include <string.h> /* memcpy, memset — the prefill and the wire's raw moves */\n")
	if g.anyVariable {
		// VARIABLE-LENGTH tables only: a unit of pointer-free tables pays
		// for none of these headers (docs/SPEC-TABLES.md §2, the zero-cost gate)
		h.WriteString("#include <stdlib.h> /* the arena's segments (the AUTHORING path may allocate) */\n")
	}
	if g.anyKeyed {
		// ENUM-KEYED arrays only: indexing one by None is a program error in
		// EVERY configuration, and the accessor is where a runtime key can
		// first be caught (docs/SPEC-TABLES.md §2.4). The refusal aborts, so it
		// needs <stdlib.h> whether or not NDEBUG keeps the assert.
		h.WriteString("#include <assert.h> /* the keyed accessor's None refusal, in a debug build */\n")
		if !g.anyVariable {
			h.WriteString("#include <stdlib.h> /* and its abort, which NDEBUG does not remove */\n")
		}
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
	// SCHEMA_UNUSED with the packet emitter's own guard: every function in
	// this header is `static`, so a translation unit that uses one and not
	// another must not be warned about the rest. The definition is the packet
	// header's, guarded identically, so whichever header a TU sees first
	// defines it and the other agrees.
	h.WriteString("\n#ifndef SCHEMA_UNUSED\n#if defined(__GNUC__) || defined(__clang__)\n#define SCHEMA_UNUSED __attribute__((unused))\n#else\n#define SCHEMA_UNUSED\n#endif\n#endif\n")
	h.WriteString("\n#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	h.WriteString(tablePrimitives(u.Package, g.anyVariable, g.anyKeyed))
	if g.anyVariable {
		h.WriteString("\n")
		h.WriteString(tableArenaRuntime(u.Package))
	}
	// the COOKED FORM's read side (docs/SPEC-TABLES.md §7) and the BUILD VERSION
	// it matches against, in EVERY unit that declares a table: every table
	// cooks and any table may be a cook's root, so there is no unit with
	// tables and no cook reader.
	h.WriteString("\n")
	h.WriteString(buildVersionConstant(u.Package, ir.BuildVersion(u)))
	h.WriteString("\n")
	h.WriteString(tableCookRuntime(u.Package))
	h.WriteString("\n")
	h.WriteString(g.body.String())
	fmt.Fprintf(&h, "\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* %s */\n", guard)
	return []byte(h.String())
}

// tableSource assembles <Base>Table.c: the descriptors and the text form's
// definitions, plus the one generic walk they call.
func tableSource(u *ir.Unit, f *ir.File, g *tableGen, cg *tableGen, members []*ir.Struct) []byte {
	var c strings.Builder
	fmt.Fprintf(&c, "/* Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n   SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n   your choice. See the LICENSE exception in the schema compiler; the compiler is\n   AGPL-3.0, its output is not.\n", f.Base)
	fmt.Fprintf(&c, "   package %s — the TABLE wire's reflection descriptors and text form\n", u.Package)
	c.WriteString("   (docs/SPEC-TABLES.md §8, §16). Compile this file to use <name>_table_type,\n")
	c.WriteString("   <name>_from_json or <name>_to_json; a project that reads neither the\n")
	c.WriteString("   descriptors nor a text still gets the whole wire from the header. */\n\n")
	fmt.Fprintf(&c, "#include \"%sTable.h\"\n\n", f.Base)
	c.WriteString("#include <stdio.h>  /* the text form: number formatting */\n")
	c.WriteString("#include <stdlib.h> /* the text form: exact number conversion */\n")
	c.WriteString("#include <locale.h> /* the text form: the runtime's decimal point */\n\n")
	fixed := 0
	for _, st := range members {
		if !g.isVar(st.Name) {
			fixed++
		}
	}
	if fixed > 0 {
		c.WriteString(tableJsonWalk(u.Package))
		c.WriteString("\n")
	}
	c.WriteString(cg.body.String())
	return []byte(c.String())
}

// emitStaticAssert writes a C89-compatible compile-time assertion. C11's
// _Static_assert is used where the compiler has it — the message reaches the
// build log verbatim — and the negative-array-size form stands in everywhere
// else, which is what keeps this header compilable under -std=c99.
func (g *tableGen) emitStaticAssert(tag, cond, message string) {
	g.pf("SCHEMA_TABLE_STATIC_ASSERT( %s, %s, \"%s\" );\n", tag, cond, message)
}

// orderTables returns a file's tables with every same-file table preceding
// its by-value users — schema references are order-free, C is not. Stable:
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

// api is the CALLABLE spelling of a name-first generated function: the §11
// suffix set, spelled in C.
//
// THE RULE, AND IT IS THE PACKET EMITTER'S: a generated identifier is spelled
// in the convention this backend's PACKET half already uses in this language,
// because the two halves land in one project and often in one file's include
// set, and a reader should not be able to tell which emitter wrote a line. For
// C that convention is four rules, all of them read off `generated/c/`:
//
//   - TYPES are PascalCase                — ArmAlign, TableReader, <Name>Block
//   - FUNCTIONS are snake_case            — write_arms_agree, schema_utf8_valid_
//   - FILE-SCOPE CONSTANTS are snake_case — enum_name_ship_type
//   - MACROS are SCREAMING_SNAKE under SCHEMA_ — SCHEMA_UNUSED
//
// So §11's <Name>Load is `<name>_load` here, exactly as it is `<name>_load` in
// Rust and `<Name>Load` in Go, C# and C++: §11 names the SUFFIX SET that a
// declaration may not collide with, and each backend spells that set in its own
// language. A port that spelled the reference's C++ casing in C would be the
// only backend whose table half and packet half disagree.
func (g *tableGen) api(name, verb string) string {
	return ir.RustSnake(name) + "_" + verb
}

// sym is the LINKER spelling of a generated symbol with external linkage.
//
// C has no namespaces, and the reference's separation of two generations of
// one schema — tblv1::Cfg beside tblv2::Cfg — is a namespace. Two such units
// linked into one program would collide on every external name derived from a
// type, so every external this backend emits carries the PACKAGE and a
// trailing underscore, the same "reserved by the generator" marker the packet
// emitter's own helpers use (schema_utf8_valid_).
//
// Nothing a consumer types looks like this. The name-first surface §11 states
// — spelled by `api` above as <name>_load, <name>_from_json, <name>_table_type,
// <name>_block_open — is emitted as `static` in the header and forwards to
// these, so a call site reads as C while the LINKER sees one symbol per
// (package, type).
// Two units still cannot meet in ONE TRANSLATION UNIT, which is the packet
// emitter's stated limit and unchanged by any of this.
func (g *tableGen) sym(name, what string) string {
	return "schema_" + g.unit.Package + "_" + ir.RustSnake(name) + "_" + what + "_"
}

// tableInlineMacro names the unit's force-inline macro. It carries the package
// for the reason every other emitted name does, and the reason is the LINKER:
// two units of one schema family link into one program. Two units' headers
// cannot share a translation unit at all — C has no namespace, so that is
// dozens of redefinitions and the packet emitter's standing limit (SPEC §6.1).
//
// IT LEADS WITH SCHEMA_, and so does every other macro this backend defines.
// C's preprocessor namespace is the one place a generated name and a DECLARED
// name meet with no compiler between them — a schema's constants, enum variants
// and flag masks are all `#define`s in this target — so the generator reserves
// that prefix and the checker refuses a declaration that would spell it
// (internal/check's reservedCMacroPrefix). The reference puts the package first
// because C++ has no schema-generated macros to collide with.
func tableInlineMacro(pkg string) string { return "SCHEMA_" + strings.ToUpper(pkg) + "_TABLE_INLINE" }
