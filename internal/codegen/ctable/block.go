// The BLOCK FORM in C (docs/SPEC-TABLES.md §19): the builder half, emitted ON
// THE SIDE.
//
// NOTHING DECLARES IT. Every FIXED table has a block form — one projection of
// the same declaration beside its wire (§3) and its cook (§7), in which the
// table's own bounded arrays of structs are laid out of line at a fixed pitch
// so a consumer in another language points at their rows. It is emitted into
// <Base>Block.h and <Base>Block.c, which a consumer includes and compiles
// only if it uses the form; <Base>Table.h carries not one symbol of it, which
// is what the zero-cost gate (§2.2) asks.
//
// A table with NO block form says so in the header rather than going missing:
// a variable-length table has no fixed pitch anywhere in it, and a table whose
// closure carries a union has no blittable C# spelling (§19.3 pins that side
// to Sequential with generated padding, which cannot overlay arms).
//
// Everything is PRE-COOKED AT BUILD: every layout fact is settled by the
// compiler (ir.Blocks) and asserted into this side by generated compile-time
// assertions, so nothing is decided, discovered or checked at frame time.
//
// The fill path — Begin, the array accessors and the row storage they hand
// back — contains NO ALLOCATION, NO LOCK AND NO ATOMIC. That is an obligation
// on this backend rather than a permission to the caller (§19.1).
package ctable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockRuntime is the shared block runtime: emitted into every <Base>Block.h
// behind a per-package guard, so one definition survives per translation unit
// whatever the include order and a lone Block.h works standalone.
//
// The unit's BUILD VERSION is NOT here: both accelerators carry it (§20.6) and
// a Block header includes the Table header beside it, so it has a guard of its
// own and one text — buildVersionConstant, emitted by both.
func blockRuntime(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_BLOCK_PRIMITIVES"
	return `#ifndef ` + guard + `
#define ` + guard + `

/* ---- the block form's runtime (docs/SPEC-TABLES.md §19) ---- */

/* What a table knows about ONE of its out-of-line arrays: where the rows
   start, how many there are, and how far apart they sit. Sixteen bytes with no
   interior padding, sitting at the array field's own position in the
   projection (§2.7). A consumer reads all three FROM THE INSTANCE, never from
   its own constants — that is the difference between a generated pair of
   structs and an ABI (§19.2). */
typedef struct TableBlockTriple
{
    uint64_t offset_of; /* block-relative: the block relocates by plain memcpy */
    uint32_t count;     /* rows the producer filled; rows past it are not part of the block */
    uint32_t stride;    /* the pitch the consumer indexes with, from the data */
} TableBlockTriple;

/* THE CALLER'S ALLOCATOR, with malloc semantics. A block's storage is one
   extent sized from the declared maxima and allocated ONCE, at build time,
   through this triple — never at fill time, never per row, and never grown.
   The FILL path allocates nothing and takes no lock, which is the obligation
   §19.1 states; "the block form never allocates" would be a claim about who
   owns the extent, and the answer is that the caller does. */
typedef struct TableBlockAllocator
{
    void * ( *alloc )( void * context, int64_t bytes );
    void ( *free )( void * context, void * pointer );
    void * context;
} TableBlockAllocator;

/* The default triple, for a caller that has no allocator of its own to hand
   in. Nothing in the generated surface reaches for it: a caller names it. */
static SCHEMA_UNUSED void * table_block_default_alloc( void * context, int64_t bytes ) { (void) context; return malloc( (size_t) bytes ); }
static SCHEMA_UNUSED void table_block_default_free( void * context, void * pointer ) { (void) context; free( pointer ); }

static SCHEMA_UNUSED TableBlockAllocator TableBlockDefaultAllocator( void )
{
    TableBlockAllocator allocator;
    allocator.alloc = table_block_default_alloc;
    allocator.free = table_block_default_free;
    allocator.context = NULL;
    return allocator;
}

/* THIS BUILD's byte order, as the prologue carries it (docs/SPEC-TABLES.md §20.3).
   A block written by a build of the other order is REFUSED by BlockOpen: a
   big-endian fix-up path is a named obligation, not something a consumer
   improvises row by row. */
#if defined( __BYTE_ORDER__ ) && defined( __ORDER_BIG_ENDIAN__ ) && __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__
#define TableBlockByteOrder 2ull /* big */
#else
#define TableBlockByteOrder 1ull /* little */
#endif

/* Why Begin refused: the array, its count and its maximum (§19.1). Clamping a
   count to its maximum before Begin is the PRODUCER's job — Begin is a
   contract check, not a policy — and a producer at sixty hertz that silently
   drops a frame is worse than one that does not. */
typedef struct TableBlockRefusal
{
    const char * array;
    int64_t count;
    int64_t maximum;
} TableBlockRefusal;

/* One array's rows, at the pitch the instance gives (§19.2). A call site never
   spells the pitch arithmetic itself, for the same reason a keyed array's call
   sites should not re-derive their own slot rule. In a block this build just
   laid out, the pitch IS sizeof, which is what makes the Span accessor beside
   each of these a contiguous typed pointer. */
typedef struct TableBlockRows
{
    uint8_t * base;
    int32_t count;
    int32_t stride;
} TableBlockRows;

static SCHEMA_UNUSED void * TableBlockRowAt( const TableBlockRows * rows, int32_t index )
{
    return (void *) ( rows->base + (ptrdiff_t) index * rows->stride );
}

/* ---- reflection over a block (docs/SPEC-TABLES.md §8, §19.2) ----

   The descriptors are the mechanism, and they are what retires a hand-kept
   mirror: a consumer holding them reads the triples out of an instance and
   points at rows, with no hand-written struct per table and no knowledge of
   the spelling that produced any of it. They are constant data, so this costs
   a lookup, not a parse — and they are immutable, so any thread may read
   them. */
struct TableBlockInfo;

typedef struct TableBlockFieldInfo
{
    const char * name;
    uint32_t offset;  /* the field's offset in the record this descriptor describes */
    uint32_t size;    /* its size there */
    uint8_t kind;     /* the table-wire kind, as TableFieldInfo carries it */
    int out_of_line;  /* an out-of-line array: the triple's three members are live */
    uint32_t offset_of_offset; /* the triple's offset_of member, or 0xffffffff */
    /* The COUNT COMPANION, and it is one column doing one job in both
       spellings: the triple's count member for an out-of-line array, the int32
       used length of a string or a bytes inline, 0xffffffff when the field has
       none. */
    uint32_t count_offset;
    uint32_t stride_offset;    /* the triple's stride member, or 0xffffffff */
    uint32_t stride;           /* THIS BUILD's pitch, to assert against — never to index with (§19.2) */
    /* ---- what a GENERIC ROW WALK needs, in the vocabulary TableFieldInfo
       already uses (docs/SPEC-TABLES.md §8.1), so ONE walker reads a cooked node
       and a block row without learning a second one. Where the field starts is
       the pair above; this is everything after it. */
    int is_array;            /* inline storage of array_bound slots at elem_size (bytes included) */
    int counted;             /* count_offset names a used-length companion */
    int optional;            /* present_offset names a presence companion */
    int32_t array_bound;     /* inline slots, or a string's declared maximum; 0 for a plain scalar */
    uint32_t elem_size;      /* ONE slot's size; the field's own when it holds one value */
    uint32_t present_offset; /* the presence companion, or 0xffffffff */
    /* the ELEMENT's or the nested record's own layout. NULL when the field is a
       scalar. Following it is how a walker DESCENDS: an out-of-line array's
       rows, and a nested record's fields, are both reached through this one
       column. C needs no lazy factory here — the descriptors are constant data
       in one translation unit, so a forward declaration makes the address a
       constant expression whatever order the records fall in. */
    const struct TableBlockInfo * element;
} TableBlockFieldInfo;

/* One record's layout as DATA — the whole mechanism behind the block form's
   read side. A block-form table's own descriptor describes its PROJECTION; the
   element descriptor of each out-of-line array describes that array's ROW, and
   so on down. */
typedef struct TableBlockInfo
{
    const char * name;
    uint64_t build_version; /* the unit's (docs/SPEC-TABLES.md §20) */
    uint32_t size;          /* the record's own sizeof: a projection's, or a row's */
    uint32_t align;
    int32_t num_fields;
    const TableBlockFieldInfo * fields;
} TableBlockInfo;

/* The block's magic, and the byte-order check with it (§19.1). It is stored in
   the producer's NATIVE order; a consumer that reads back the byte-swapped
   value has found a foreign byte order, and one that reads back anything else
   has not found a block at all. */
#define TableBlockMagic 0x4b4c42414d484353ull

static SCHEMA_UNUSED uint64_t table_block_byteswap64( uint64_t v )
{
    return ( v >> 56 ) | ( ( v >> 40 ) & 0xff00ull ) | ( ( v >> 24 ) & 0xff0000ull ) | ( ( v >> 8 ) & 0xff000000ull )
         | ( ( v << 8 ) & 0xff00000000ull ) | ( ( v << 24 ) & 0xff0000000000ull ) | ( ( v << 40 ) & 0xff000000000000ull )
         | ( v << 56 );
}

/* the prologue read BYTEWISE: the magic is the one field read without assuming
   the order the rest of the block is in */
static SCHEMA_UNUSED uint64_t table_block_read64( const uint8_t * p )
{
    uint64_t v = 0;
    memcpy( &v, p, 8 );
    return v;
}

static SCHEMA_UNUSED int64_t table_block_align( int64_t offset, int64_t alignment )
{
    return ( offset + alignment - 1 ) / alignment * alignment;
}

#endif /* ` + guard + ` */
`
}

// generateBlockFiles emits <Base>Block.h and <Base>Block.c for every file of a
// unit that declares a table. A file whose tables all lack a block form still
// gets a header, saying which table and why — a form that goes missing without
// a word is the thing this avoids.
func generateBlockFiles(u *ir.Unit, blocks *ir.BlockUnit, variable, targets map[string]bool) map[string][]byte {
	out := map[string][]byte{}
	if blocks == nil {
		return out
	}
	for _, f := range u.Files {
		if len(f.Tables) == 0 {
			continue
		}
		g := &tableGen{unit: u, file: f, blocks: blocks, variable: variable, targets: targets,
			includes: map[string]bool{}, nativeIncludes: map[string]bool{}}
		c := &tableGen{unit: u, file: f, blocks: blocks, variable: variable, targets: targets,
			includes: map[string]bool{}, nativeIncludes: map[string]bool{}}

		var formed []*ir.BlockLayout
		for _, st := range f.Tables {
			if bl := blocks.Block(st.Name); bl != nil {
				formed = append(formed, bl)
				continue
			}
			g.pf("/* table %s has NO block form: %s (docs/SPEC-TABLES.md §19).\n", st.Name, blocks.SkippedReason(st.Name))
			g.pf("   Its wire (§3) and its cook (§7) are unaffected — only this projection\n")
			g.pf("   is absent, and it is absent by construction rather than by refusal. */\n\n")
		}
		for _, bl := range formed {
			g.owner = bl.Table
			g.emitBlockSurface(bl)
			c.owner = bl.Table
			c.emitBlockDefinitions(bl)
		}
		out[f.Base+"Block.h"] = blockHeader(u, f, g, len(formed))
		if len(formed) > 0 {
			out[f.Base+"Block.c"] = blockSource(u, f, c)
		}
	}
	return out
}

func blockBanner(u *ir.Unit, f *ir.File) string {
	var h strings.Builder
	fmt.Fprintf(&h, "/* Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
	h.WriteString("   SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("   your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("   AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "   package %s — the BLOCK FORM (docs/SPEC-TABLES.md §19).\n", u.Package)
	return h.String()
}

func blockHeader(u *ir.Unit, f *ir.File, g *tableGen, formed int) []byte {
	var h strings.Builder
	guard := "SCHEMA_" + strings.ToUpper(u.Package) + "_" + strings.ToUpper(f.Base) + "BLOCK_H"
	h.WriteString(blockBanner(u, f))
	h.WriteString("\n")
	h.WriteString("   NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on\n")
	h.WriteString("   the side: include this header and compile the .c beside it only if you use\n")
	h.WriteString("   the block form. The unit's <Base>Table.h carries not one symbol of it.\n")
	h.WriteString("\n")
	h.WriteString("   A block is one flat extent: the table's own instance at the front — the\n")
	h.WriteString("   PROJECTION, carrying per bounded array of structs where its rows start, how\n")
	h.WriteString("   many there are and how far apart they sit — and then those rows, each array\n")
	h.WriteString("   at a fixed pitch. The other side reads those three facts and points. */\n\n")
	fmt.Fprintf(&h, "#ifndef %s\n#define %s\n\n", guard, guard)
	h.WriteString("#include <stdint.h>\n#include <stddef.h> /* offsetof, and ptrdiff_t for the row pitch */\n#include <string.h>\n#include <stdlib.h> /* the DEFAULT allocator pair, for a caller with none of its own */\n")
	fmt.Fprintf(&h, "\n#include \"%sTable.h\"\n", f.Base)
	names := make([]string, 0, len(g.includes))
	for n := range g.includes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if n == f.Base {
			continue
		}
		fmt.Fprintf(&h, "#include \"%sTable.h\"\n", n)
	}
	h.WriteString("\n#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	if formed > 0 {
		h.WriteString(buildVersionConstant(u.Package, ir.BuildVersion(u)))
		h.WriteString("\n")
		h.WriteString(blockRuntime(u.Package))
		h.WriteString("\n")
	}
	h.WriteString(g.body.String())
	fmt.Fprintf(&h, "\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* %s */\n", guard)
	return []byte(h.String())
}

func blockSource(u *ir.Unit, f *ir.File, c *tableGen) []byte {
	var s strings.Builder
	s.WriteString(blockBanner(u, f))
	s.WriteString("\n")
	s.WriteString("   The block form's COLD half, in its own translation unit: the open path, run\n")
	s.WriteString("   once per handoff, and the reflection descriptors, which are constant data.\n")
	s.WriteString("   The per-frame FILL path stays inline in the header, because a call per row\n")
	s.WriteString("   accessor is exactly the cost this form exists to avoid. */\n\n")
	fmt.Fprintf(&s, "#include \"%sBlock.h\"\n\n", f.Base)
	s.WriteString(c.body.String())
	return []byte(s.String())
}

// emitBlockProjectionField renders one projection field. Every field keeps its
// by-value storage at its natural offset; a bounded array of structs — `T[N]`
// and its count companion — is replaced AT THAT FIELD'S POSITION by its
// sixteen-byte triple (docs/SPEC-TABLES.md §2.7).
func (g *tableGen) emitBlockProjectionField(f *ir.Field) {
	if ir.BlockOutOfLine(f) {
		g.noteRef(f.Type.Name)
		g.pf("    TableBlockTriple %s; /* [..%d]%s laid out of line: (offset_of, count, stride) */\n",
			f.Name, f.ArrayBound, f.Type.Name)
		return
	}
	g.emitTableStorageField(f)
}

// emitBlockSurface emits one table's whole block form into the header.
func (g *tableGen) emitBlockSurface(bl *ir.BlockLayout) {
	name := bl.Table.Name

	g.pf("/* ---- the block form of table %s (docs/SPEC-TABLES.md §19): begin ---- */\n\n", name)

	g.pf("/* The counts, gathered before Begin — nothing about counting is concurrent\n")
	g.pf("   (docs/SPEC-TABLES.md §19.1). Clamping a count to its maximum is the PRODUCER's job. */\n")
	g.pf("typedef struct %sCounts\n{\n", name)
	for _, a := range bl.Arrays {
		g.pf("    int32_t %s; /* [0, %d] */\n", a.Field.Name, a.Max)
	}
	if len(bl.Arrays) == 0 {
		g.pf("    int32_t unused; /* the table declares no out-of-line array */\n")
	}
	g.pf("} %sCounts;\n\n", name)

	g.pf("/* The block's STORAGE, sized from the declared maxima: one extent, allocated\n")
	g.pf("   once, never grown, never pooled (docs/SPEC-TABLES.md §19.1). The sum is loose by\n")
	g.pf("   construction — arrays commonly draw from one shared pool, so their maxima\n")
	g.pf("   can add to more than can ever be occupied at once.\n")
	g.pf("   It is allocated ONCE, at build time, through the CALLER'S allocator, and\n")
	g.pf("   released through the same triple. The fill path allocates nothing. */\n")
	g.pf("#define %sBlockMaxBytes %d\n\n", name, bl.MaxBytes)
	g.pf("typedef struct %sBlockStorage\n{\n", name)
	g.pf("    uint8_t * base;      /* the extent, 64-byte aligned */\n")
	g.pf("    void * allocation;   /* what the allocator handed back */\n")
	g.pf("    TableBlockAllocator allocator;\n")
	g.pf("} %sBlockStorage;\n\n", name)

	g.pf("/* Create allocates %sBlockMaxBytes + %d bytes through the caller's triple\n", name, ir.BlockAlign-1)
	g.pf("   and aligns the base inside them: malloc's guarantee is not a cache\n")
	g.pf("   line's, and the 64-byte base is what keeps two workers filling\n")
	g.pf("   different arrays off one line (§19.1). One call, at build time. */\n")
	g.pf("static SCHEMA_UNUSED int %sBlockStorageCreate( %sBlockStorage * storage, const TableBlockAllocator * from )\n{\n", name, name)
	g.pf("    uintptr_t raw;\n")
	g.pf("    storage->allocator = *from;\n")
	g.pf("    storage->allocation = storage->allocator.alloc( storage->allocator.context, %sBlockMaxBytes + %d );\n", name, ir.BlockAlign-1)
	g.pf("    if ( storage->allocation == NULL ) { storage->base = NULL; return 0; }\n")
	g.pf("    raw = (uintptr_t) storage->allocation;\n")
	g.pf("    storage->base = (uint8_t *) ( ( raw + %d ) & ~(uintptr_t) %d );\n", ir.BlockAlign-1, ir.BlockAlign-1)
	g.pf("    return 1;\n}\n\n")
	g.pf("static SCHEMA_UNUSED void %sBlockStorageDestroy( %sBlockStorage * storage )\n{\n", name, name)
	g.pf("    if ( storage->allocation != NULL ) { storage->allocator.free( storage->allocator.context, storage->allocation ); }\n")
	g.pf("    storage->allocation = NULL;\n    storage->base = NULL;\n}\n\n")

	g.pf("/* The PROJECTION: a record like any other, following the same C ABI rule\n")
	g.pf("   (§19.3) — its own offsets are part of the contract, not scaffolding\n")
	g.pf("   around it. It opens with the generated PROLOGUE of three uint64s. */\n")
	g.pf("typedef struct %sBlockProjection\n{\n", name)
	g.pf("    uint64_t magic;         /* generated: identifies a schema block */\n")
	g.pf("    uint64_t build_version; /* generated: the unit's build version (docs/SPEC-TABLES.md §20) */\n")
	g.pf("    uint64_t byte_order;    /* generated: 1 little, 2 big — the producer stamps its own */\n")
	for _, f := range bl.Table.Fields {
		g.emitBlockProjectionField(f)
	}
	g.pf("} %sBlockProjection;\n\n", name)

	g.pf("/* The block: one extent, 64-byte aligned at its base, the PROJECTION at\n")
	g.pf("   offset 0 and then each out-of-line array in declaration order\n")
	g.pf("   (docs/SPEC-TABLES.md §19.1). Sixty-four is a cache line, and the guarantee is PER\n")
	g.pf("   ARRAY: two workers filling different arrays never share one.\n")
	g.pf("\n")
	g.pf("   A block is valid until the next Begin on the SAME storage, which invalidates\n")
	g.pf("   every block over it and every row pointer taken from one. Double-buffering\n")
	g.pf("   is therefore two storages, and it is the caller's. */\n")
	g.pf("typedef struct %sBlock\n{\n", name)
	g.pf("    uint8_t * base;                    /* the extent's base, 64-byte aligned */\n")
	g.pf("    %sBlockProjection * projection;    /* the projection, at offset 0 */\n", name)
	g.pf("    int64_t bytes;                     /* the extent in use */\n")
	g.pf("} %sBlock;\n\n", name)

	g.pf("/* this table's block descriptors (docs/SPEC-TABLES.md §8, §19.2): constant\n")
	g.pf("   data, defined in the .c beside this header. A consumer holding this reads\n")
	g.pf("   the triples out of an instance and points at rows, with no hand-written\n")
	g.pf("   struct per table. */\n")
	g.pf("extern const TableBlockInfo %s;\n", g.sym(name, "block_info"))
	g.pf("static SCHEMA_UNUSED const TableBlockInfo * %sBlockType( void ) { return &%s; }\n\n", name, g.sym(name, "block_info"))

	g.emitBlockLayoutAsserts(bl)
	g.emitBlockFillPath(bl)

	g.pf("/* BlockOpen checks once and points, and this is the WHOLE check (§19.2): the\n")
	g.pf("   magic read bytewise, the BYTE ORDER the prologue carries against this\n")
	g.pf("   build's own, the BUILD VERSION against this build's own, each array's\n")
	g.pf("   pitch, its offset_of, its COUNT against the declared maximum and its\n")
	g.pf("   extent inside the block, the used extent against the bytes the caller\n")
	g.pf("   passed, and the base's alignment.\n")
	g.pf("   On a match the bytes are what a build with this layout wrote, so there is\n")
	g.pf("   nothing to validate and nothing to fix up. On any failure it returns 0\n")
	g.pf("   and points at nothing.\n")
	g.pf("\n")
	g.pf("   There is ONE entry point, and no tolerant twin: the block form is same-build\n")
	g.pf("   by construction — both sides are generated from one declaration at one build\n")
	g.pf("   and ship together — so a consumer older than its producer is not a case. A\n")
	g.pf("   mismatch is a refusal; regenerate both sides. Data that must outlive the\n")
	g.pf("   build that wrote it takes the wire (§3), which this same table still has. */\n")
	g.pf("int %s( %sBlock * block, void * base, int64_t bytes );\n", g.sym(name, "block_open"), name)
	g.pf("static SCHEMA_UNUSED int %sBlockOpen( %sBlock * block, void * base, int64_t bytes )\n{\n", name, name)
	g.pf("    return %s( block, base, bytes );\n}\n\n", g.sym(name, "block_open"))

	g.pf("/* ---- the block form of table %s: end ---- */\n\n", name)
}

// emitBlockLayoutAsserts is this backend's half of the LAYOUT CONTRACT
// (docs/SPEC-TABLES.md §19.3): the compiler computes the layout and each backend
// emits code asserting that ITS OWN compiler agrees. A disagreement is a build
// error on the side that disagrees, naming the type, the field and the offset.
func (g *tableGen) emitBlockLayoutAsserts(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("/* The LAYOUT CONTRACT (docs/SPEC-TABLES.md §19.3). The compiler derived every\n")
	g.pf("   number below from the declaration; these assertions are this compiler saying\n")
	g.pf("   whether it agrees. Neither side's layout is inferred from the other's —\n")
	g.pf("   both are checked against their own compiler's model, which is the only way\n")
	g.pf("   a two-language contract can be held by a compiler that generates both. */\n")
	g.emitStaticAssert(name+"_triple_size", "sizeof( TableBlockTriple ) == 16",
		"a triple is sixteen bytes with no interior padding (docs/SPEC-TABLES.md §2.7)")
	g.emitStaticAssert(name+"_triple_members",
		"offsetof( TableBlockTriple, offset_of ) == 0 && offsetof( TableBlockTriple, count ) == 8 && offsetof( TableBlockTriple, stride ) == 12",
		"a triple's members sit at 0/8/12 (docs/SPEC-TABLES.md §2.7)")
	g.emitStaticAssert(name+"_projection_size",
		fmt.Sprintf("sizeof( %sBlockProjection ) == %d", name, bl.Projection.Size),
		fmt.Sprintf("%s's block projection sizeof moved: the other backends assert %d for the same declaration (docs/SPEC-TABLES.md §19.3)", name, bl.Projection.Size))
	g.emitStaticAssert(name+"_projection_align",
		fmt.Sprintf("SCHEMA_TABLE_ALIGNOF( %sBlockProjection ) == %d", name, bl.Projection.Align),
		fmt.Sprintf("%s's block projection alignof moved (docs/SPEC-TABLES.md §19.3)", name))
	g.emitStaticAssert(name+"_prologue",
		fmt.Sprintf("offsetof( %sBlockProjection, magic ) == 0 && offsetof( %sBlockProjection, build_version ) == 8 && offsetof( %sBlockProjection, byte_order ) == 16", name, name, name),
		"the block prologue is magic at 0, build_version at 8, byte_order at 16 (docs/SPEC-TABLES.md §19.1, §20)")
	for _, fl := range bl.Projection.Fields {
		g.emitStaticAssert(fmt.Sprintf("%s_projection_%s", name, fl.Field.Name),
			fmt.Sprintf("offsetof( %sBlockProjection, %s ) == %d", name, fl.Field.Name, fl.Offset),
			fmt.Sprintf("%s's projection field %s moved: the other backends assert %d (docs/SPEC-TABLES.md §19.3)", name, fl.Field.Name, fl.Offset))
	}
	seen := map[string]bool{}
	for _, a := range bl.Arrays {
		g.emitBlockRowAsserts(a.ElemName, seen)
		g.emitStaticAssert(fmt.Sprintf("%s_%s_stride", name, a.Field.Name),
			fmt.Sprintf("%d == (int64_t) sizeof( %s )", a.Stride, a.ElemName),
			fmt.Sprintf("%s's pitch is its element's sizeof, always (docs/SPEC-TABLES.md §2.7)", a.Field.Name))
	}
	g.pf("\n")
}

// emitBlockRowAsserts pins one row type's own layout — the same facts the other
// backends assert under their own models, from the same compiler-derived
// numbers. They are emitted per BLOCK rather than per record, so the tag names
// the owner too: one row type reached by two blocks is asserted twice, and two
// identical assertions are cheaper than a rule about who owns one.
func (g *tableGen) emitBlockRowAsserts(name string, seen map[string]bool) {
	if seen[name] {
		return
	}
	seen[name] = true
	ml := g.blocks.Layout(name)
	if ml == nil {
		return
	}
	g.noteRef(name)
	owner := ""
	if g.owner != nil {
		owner = g.owner.Name + "_"
	}
	g.emitStaticAssert(fmt.Sprintf("%srow_%s_size", owner, name),
		fmt.Sprintf("sizeof( %s ) == %d", name, ml.Size),
		fmt.Sprintf("block row %s's sizeof moved: the other backends assert %d for the same declaration (docs/SPEC-TABLES.md §19.3)", name, ml.Size))
	g.emitStaticAssert(fmt.Sprintf("%srow_%s_align", owner, name),
		fmt.Sprintf("SCHEMA_TABLE_ALIGNOF( %s ) == %d", name, ml.Align),
		fmt.Sprintf("block row %s's alignof moved (docs/SPEC-TABLES.md §19.3)", name))
	for _, fl := range ml.Fields {
		g.emitStaticAssert(fmt.Sprintf("%srow_%s_%s", owner, name, fl.Field.Name),
			fmt.Sprintf("offsetof( %s, %s ) == %d", name, fl.Field.Name, fl.Offset),
			fmt.Sprintf("block row %s's field %s moved: the other backends assert %d (docs/SPEC-TABLES.md §19.3)", name, fl.Field.Name, fl.Offset))
	}
	for _, fl := range ml.Fields {
		if fl.Field.Type.Kind == ir.TNamed {
			if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
				g.emitBlockRowAsserts(ref.Name, seen)
			}
		}
	}
}

// emitBlockFillPath emits Begin, BlockBytes and the row accessors — THE FILL
// PATH, and the whole of what the conformance refuser (docs/SPEC-TABLES.md §19.1,
// §19.5) claims. Between the markers below there is no allocation, no lock and
// no atomic. It stays INLINE in the header: a call per row accessor is exactly
// the cost this form exists to avoid.
func (g *tableGen) emitBlockFillPath(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("/* ---- block fill path: begin ----\n")
	g.pf("   THE MULTI-THREADED FILL IS AN OBLIGATION ON THIS BACKEND, not a permission\n")
	g.pf("   to the caller (docs/SPEC-TABLES.md §19.1). Nothing between these markers\n")
	g.pf("   allocates, locks or takes an atomic. The parallelism itself lives in the\n")
	g.pf("   caller's loop — N workers, disjoint index ranges, no synchronisation of any\n")
	g.pf("   kind — and keeping this surface free of those three is what MAKES it\n")
	g.pf("   possible. */\n\n")

	g.pf("/* The LAYOUT is settled once per block, from the counts, before any worker\n")
	g.pf("   starts. Begin refuses counts past the declared maxima NAMING the array, its\n")
	g.pf("   count and its maximum; stamps the prologue; writes every array's offset_of,\n")
	g.pf("   count and stride; and hands back the block. It touches no row, and it is\n")
	g.pf("   O( out-of-line arrays ) — a handful, not thousands. The projection's own\n")
	g.pf("   fields are the producer's to write, exactly as the rows are: a caller that\n")
	g.pf("   needs a byte-stable artifact zeroes the storage once (§19.1).\n")
	g.pf("\n")
	g.pf("   Begin and BlockBytes are SINGLE-THREADED: call Begin before the workers and\n")
	g.pf("   BlockBytes after they join (§19.1). `refusal` may be NULL. */\n")
	g.pf("static SCHEMA_UNUSED int %sBlockBegin( %sBlock * block, %sBlockStorage * storage, const %sCounts * counts, TableBlockRefusal * refusal )\n{\n",
		name, name, name, name)
	g.pf("    int64_t offset;\n")
	for _, a := range bl.Arrays {
		g.pf("    if ( counts->%s < 0 || counts->%s > %d )\n    {\n", a.Field.Name, a.Field.Name, a.Max)
		g.pf("        if ( refusal != NULL ) { refusal->array = \"%s\"; refusal->count = counts->%s; refusal->maximum = %d; }\n",
			a.Field.Name, a.Field.Name, a.Max)
		g.pf("        return 0;\n    }\n")
	}
	if len(bl.Arrays) == 0 {
		g.pf("    (void) counts; (void) refusal; /* no out-of-line array: no count to refuse */\n")
	}
	g.pf("    if ( storage->base == NULL ) { return 0; } /* Create was not called, or the allocator refused */\n")
	g.pf("    block->base = storage->base;\n")
	g.pf("    block->projection = (%sBlockProjection *) (void *) storage->base;\n", name)
	g.pf("    block->projection->magic = TableBlockMagic;\n")
	g.pf("    block->projection->build_version = BuildVersion;\n")
	g.pf("    block->projection->byte_order = TableBlockByteOrder;\n")
	g.pf("    offset = %d; /* sizeof the projection */\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		g.pf("    offset = table_block_align( offset, %d ); /* max( 64, alignof( %s ) ) */\n", blockStartAlign(a), a.ElemName)
		g.pf("    block->projection->%s.offset_of = (uint64_t) offset;\n", a.Field.Name)
		g.pf("    block->projection->%s.count = (uint32_t) counts->%s;\n", a.Field.Name, a.Field.Name)
		g.pf("    block->projection->%s.stride = (uint32_t) sizeof( %s );\n", a.Field.Name, a.ElemName)
		g.pf("    offset += (int64_t) counts->%s * (int64_t) sizeof( %s );\n", a.Field.Name, a.ElemName)
	}
	g.pf("    block->bytes = table_block_align( offset, %d );\n", ir.BlockAlign)
	g.pf("    return 1;\n}\n\n")

	g.pf("/* The USED extent: the greatest offset_of + count * stride, rounded up to 64,\n")
	g.pf("   never less than the projection's own size (docs/SPEC-TABLES.md §19.1). Because\n")
	g.pf("   the layout follows the counts it is proportional to the frame rather than\n")
	g.pf("   to the maxima. The tail — the bytes between the last row and the rounding —\n")
	g.pf("   is UNSPECIFIED, because zeroing megabytes per frame is the cost this form\n")
	g.pf("   exists to avoid. */\n")
	g.pf("static SCHEMA_UNUSED int64_t %sBlockBytes( const %sBlock * block )\n{\n", name, name)
	g.pf("    int64_t used = %d;\n", bl.Projection.Size)
	if len(bl.Arrays) == 0 {
		g.pf("    (void) block; /* no out-of-line array: the extent is the projection's own */\n")
	}
	for _, a := range bl.Arrays {
		g.pf("    {\n")
		g.pf("        const int64_t end = (int64_t) block->projection->%s.offset_of + (int64_t) block->projection->%s.count * (int64_t) block->projection->%s.stride;\n",
			a.Field.Name, a.Field.Name, a.Field.Name)
		g.pf("        if ( end > used ) { used = end; }\n")
		g.pf("    }\n")
	}
	g.pf("    return table_block_align( used, %d );\n}\n\n", ir.BlockAlign)

	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.pf("/* %s: an accessor is ONE ADD — block base + offset_of — at the pitch the\n", a.Field.Name)
		g.pf("   INSTANCE gives. A worker holds it and indexes; disjoint index ranges into\n")
		g.pf("   one array are safe concurrently, and two workers writing one row is the\n")
		g.pf("   caller's problem (docs/SPEC-TABLES.md §19.1). */\n")
		g.pf("static SCHEMA_UNUSED TableBlockRows %s%s( const %sBlock * block )\n{\n", name, field, name)
		g.pf("    TableBlockRows rows;\n")
		g.pf("    rows.base = block->base + block->projection->%s.offset_of;\n", a.Field.Name)
		g.pf("    rows.count = (int32_t) block->projection->%s.count;\n", a.Field.Name)
		g.pf("    rows.stride = (int32_t) block->projection->%s.stride;\n", a.Field.Name)
		g.pf("    return rows;\n}\n\n")

		g.pf("/* %s, as a CONTIGUOUS TYPED base — available because the pitch IS sizeof\n", a.Field.Name)
		g.pf("   (§2.7), which is how the fast path is actually written. The COUNT comes\n")
		g.pf("   from the accessor above, out of the instance, never from a constant. */\n")
		g.pf("static SCHEMA_UNUSED %s * %s%sSpan( const %sBlock * block )\n{\n", a.ElemName, name, field, name)
		g.pf("    return (%s *) (void *) ( block->base + block->projection->%s.offset_of );\n}\n\n", a.ElemName, a.Field.Name)
	}
	g.pf("/* ---- block fill path: end ---- */\n\n")
}

// blockStartAlign is where one out-of-line array begins: aligned to
// max( 64, alignof( element ) ) (docs/SPEC-TABLES.md §19.1).
func blockStartAlign(a ir.BlockArray) int64 {
	if a.ElemAlign() > int64(ir.BlockAlign) {
		return a.ElemAlign()
	}
	return int64(ir.BlockAlign)
}

// emitBlockDefinitions emits the COLD half into <Base>Block.c: the open path
// and the reflection descriptors.
func (g *tableGen) emitBlockDefinitions(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("/* ---- the block form of table %s: the open path and the descriptors ---- */\n\n", name)
	g.emitBlockOpenBody(bl)
	g.emitBlockDescriptors(bl)
}

func (g *tableGen) emitBlockOpenBody(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("int %s( %sBlock * block, void * base, int64_t bytes )\n{\n", g.sym(name, "block_open"), name)
	g.pf("    const uint8_t * raw;\n    uint64_t magic;\n    %sBlockProjection * projection;\n    int64_t used, padding;\n", name)
	g.pf("    block->base = NULL;\n    block->projection = NULL;\n    block->bytes = 0;\n")
	g.pf("    if ( base == NULL || bytes < %d ) { return 0; }\n", bl.Projection.Size)
	g.pf("    if ( ( (uintptr_t) base %% %d ) != 0 ) { return 0; } /* the base's alignment */\n", ir.BlockAlign)
	g.pf("    raw = (const uint8_t *) base;\n")
	g.pf("    magic = table_block_read64( raw );\n")
	g.pf("    if ( magic != TableBlockMagic )\n")
	g.pf("    {\n")
	g.pf("        /* a byte-swapped magic is a FOREIGN BYTE ORDER, and anything else is\n")
	g.pf("           not a block at all. Both refuse; the distinction is here so a\n")
	g.pf("           reader of this code knows the check covers the order too. */\n")
	g.pf("        (void) table_block_byteswap64( magic );\n")
	g.pf("        return 0;\n")
	g.pf("    }\n")
	g.pf("    if ( table_block_read64( raw + 8 ) != BuildVersion ) { return 0; }\n")
	g.pf("    if ( table_block_read64( raw + 16 ) != TableBlockByteOrder )\n")
	g.pf("    {\n        return 0; /* a block of the other byte order: the fix-up path is a named obligation */\n    }\n")
	g.pf("    projection = (%sBlockProjection *) base;\n", name)
	g.pf("    used = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		g.pf("    {\n")
		g.pf("        /* EVERY NUMBER BELOW COMES FROM THE INSTANCE, so the arithmetic is\n")
		g.pf("           unsigned and each term is BOUNDED BEFORE IT IS ADDED. A forged\n")
		g.pf("           offset_of near 2^63 must refuse, and an addition that carried past\n")
		g.pf("           the top of the type would be the undefined behaviour the check\n")
		g.pf("           after it was supposed to catch. */\n")
		g.pf("        const uint64_t offset_of = projection->%s.offset_of;\n", a.Field.Name)
		g.pf("        const uint64_t count = projection->%s.count;\n", a.Field.Name)
		g.pf("        const uint64_t stride = projection->%s.stride;\n", a.Field.Name)
		g.pf("        uint64_t rows;\n        int64_t end;\n")
		g.pf("        if ( stride != (uint64_t) sizeof( %s ) ) { return 0; }\n", a.ElemName)
		g.pf("        if ( count > (uint64_t) %d )\n", a.Max)
		g.pf("        {\n")
		g.pf("            return 0; /* past the DECLARED MAXIMUM: Begin refuses this on the\n")
		g.pf("                         producer side and Open refuses it here, because a\n")
		g.pf("                         consumer that sizes anything by the maximum would\n")
		g.pf("                         overflow on a count the maximum does not bound */\n")
		g.pf("        }\n")
		g.pf("        if ( offset_of < %d || ( offset_of %% %d ) != 0 ) { return 0; }\n", bl.Projection.Size, blockStartAlign(a))
		g.pf("        if ( offset_of > (uint64_t) bytes ) { return 0; }\n")
		g.pf("        rows = count * stride; /* both bounded above: this cannot carry */\n")
		g.pf("        if ( rows > (uint64_t) bytes - offset_of ) { return 0; }\n")
		g.pf("        end = (int64_t) ( offset_of + rows );\n")
		g.pf("        if ( end > used ) { used = end; }\n")
		g.pf("    }\n")
	}
	g.pf("    /* the used extent, rounded to %d WITHOUT the rounding itself carrying past\n", ir.BlockAlign)
	g.pf("       the top of the type: used is already inside bytes, and the padding is\n")
	g.pf("       paid out of the slack that is left rather than added and compared after. */\n")
	g.pf("    padding = ( %d - ( used %% %d ) ) %% %d;\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	g.pf("    if ( padding > bytes - used ) { return 0; }\n")
	g.pf("    used += padding;\n")
	g.pf("    block->base = (uint8_t *) base;\n")
	g.pf("    block->projection = projection;\n")
	g.pf("    block->bytes = used;\n")
	g.pf("    return 1;\n}\n\n")
}

// emitBlockDescriptors emits one table's block reflection: the projection
// offset of every field, the offsets of the three members inside each triple,
// and the ELEMENT's own descriptor beside them (docs/SPEC-TABLES.md §8, §19.2).
//
// The row records' descriptors are one ARRAY — <Name>BlockRows — and every
// field table is one array — <Name>BlockFields — with each record pointing into
// it at its own base. That is what keeps the block form's reflection to THREE
// names per table, all of them claimed by §11's suffix set, rather than one
// file-scope name per row type a schema could collide with.
func (g *tableGen) emitBlockDescriptors(bl *ir.BlockLayout) {
	name := bl.Table.Name
	records := blockDescriptorRecords(g.blocks, bl)

	// every field table, concatenated, so one name covers the whole graph
	type span struct {
		start, count int
	}
	spans := map[string]span{}
	at := 0
	rowCount := 0
	for _, r := range records {
		if g.blocks.Layout(r) != nil {
			rowCount++
		}
	}
	if rowCount > 0 {
		g.pf("/* The row descriptors, DECLARED before the field table that names them: the\n")
		g.pf("   descriptors are constant data in one translation unit, so a field's element\n")
		g.pf("   column is the address of one of these whatever order the records fall in —\n")
		g.pf("   which is how a self- or mutually-referential record graph stays constant\n")
		g.pf("   data instead of a lazy link. */\n")
		g.pf("static const TableBlockInfo %s[%d];\n\n", g.sym(name, "block_rows"), rowCount)
	}
	g.pf("/* Every record's field table, concatenated: the projection's, then each\n")
	g.pf("   row's. One name for the whole graph (docs/SPEC-TABLES.md §11). */\n")
	g.pf("static const TableBlockFieldInfo %s[] = {\n", g.sym(name, "block_fields"))
	g.pf("    /* %s (the projection) */\n", name)
	g.emitBlockRecordFields(name, &bl.Projection, bl, records)
	spans[""] = span{0, len(bl.Projection.Fields)}
	at = len(bl.Projection.Fields)
	for _, r := range records {
		ml := g.blocks.Layout(r)
		if ml == nil {
			continue
		}
		g.pf("    /* %s (a row, or a record one nests) */\n", r)
		g.emitBlockRecordFields(name, ml, nil, records)
		spans[r] = span{at, len(ml.Fields)}
		at += len(ml.Fields)
	}
	g.pf("};\n\n")

	if rowCount > 0 {
		g.pf("static const TableBlockInfo %s[%d] = {\n", g.sym(name, "block_rows"), rowCount)
		for _, r := range records {
			ml := g.blocks.Layout(r)
			if ml == nil {
				continue
			}
			s := spans[r]
			g.pf("    { \"%s\", BuildVersion, %du, %du, %d, %s + %d },\n",
				r, ml.Size, ml.Align, len(ml.Fields), g.sym(name, "block_fields"), s.start)
		}
		g.pf("};\n\n")
	}
	g.pf("const TableBlockInfo %s = { \"%s\", BuildVersion, %du, %du, %d, %s };\n\n",
		g.sym(name, "block_info"), name, bl.Projection.Size, bl.Projection.Align, len(bl.Projection.Fields), g.sym(name, "block_fields"))
}

// emitBlockRecordFields emits one record's rows of the concatenated field
// table. When bl is non-nil the record is that block's PROJECTION; otherwise it
// is a row or something a row nests by value.
func (g *tableGen) emitBlockRecordFields(owner string, ml *ir.MemberLayout, bl *ir.BlockLayout, records []string) {
	rowIndex := func(record string) string {
		for i, r := range records {
			if r == record {
				return fmt.Sprintf("&%s[%d]", g.sym(owner, "block_rows"), i)
			}
		}
		return "NULL"
	}
	for _, fl := range ml.Fields {
		f := fl.Field
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		facts := ir.BlockFieldOf(g.unit, f, fl.Offset, bl != nil)
		if bl != nil {
			if a := bl.ArrayByName(f.Name); a != nil {
				g.pf("    { \"%s\", %du, %du, %d, 1, %du, %du, %du, %du, 1, 1, 0, %d, 0u, 0xffffffffu, %s },\n",
					f.Name, fl.Offset, fl.Size, kind, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride,
					a.Max, rowIndex(a.ElemName))
				continue
			}
		}
		element := "NULL"
		// A field that NAMES a record carries that record's layout, whether it
		// holds one or an array of them: an INLINE array of records is part of
		// a row, and a walker descending one reaches its element through this
		// same column. Only the pointer class has no layout to name.
		if f.Type.Kind == ir.TNamed && !f.Type.Pointer {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				element = rowIndex(ref.Name)
			}
		}
		g.pf("    { \"%s\", %du, %du, %d, 0, 0xffffffffu, %s, 0xffffffffu, 0u, %s, %s, %s, %d, %du, %s, %s },\n",
			f.Name, fl.Offset, fl.Size, kind, blockCOffset(facts.CountOffset),
			boolC(facts.IsArray), boolC(facts.Counted), boolC(facts.Optional), facts.ArrayBound, facts.ElemSize,
			blockCOffset(facts.PresentOffset), element)
	}
}

// blockCOffset spells a companion's offset, or the absent marker every other
// 32-bit offset column in the descriptors uses.
func blockCOffset(offset int64) string {
	if offset < 0 {
		return "0xffffffffu"
	}
	return fmt.Sprintf("%du", offset)
}

// blockDescriptorRecords is every record one block's descriptors reach, sorted.
func blockDescriptorRecords(b *ir.BlockUnit, bl *ir.BlockLayout) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		ml := b.Layout(name)
		if ml == nil {
			return
		}
		seen[name] = true
		out = append(out, name)
		for _, fl := range ml.Fields {
			if fl.Field.Type.Kind == ir.TNamed && !fl.Field.Type.Pointer {
				if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
					walk(ref.Name)
				}
			}
		}
	}
	for _, a := range bl.Arrays {
		walk(a.ElemName)
	}
	for _, fl := range bl.Projection.Fields {
		if ir.BlockOutOfLine(fl.Field) {
			continue
		}
		if fl.Field.Type.Kind == ir.TNamed && !fl.Field.Type.Pointer {
			if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
				walk(ref.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}
