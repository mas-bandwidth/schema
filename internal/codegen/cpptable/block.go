// The BLOCK FORM in C++ (SPEC-TABLES.md §19): the builder half, emitted ON
// THE SIDE.
//
// NOTHING DECLARES IT. Every FIXED table has a block form — one projection of
// the same declaration beside its wire (§3) and its cook (§7), in which the
// table's own bounded arrays of structs are laid out of line at a fixed pitch
// so a consumer in another language points at their rows. It is emitted into
// <Base>Block.h and <Base>Block.cpp, which a consumer includes and compiles
// only if it uses the form; <Base>Table.h carries not one symbol of it, which
// is what the zero-cost gate (§2.2) asks.
//
// A table with NO block form says so in the header rather than going missing:
// a variable-length table has no fixed pitch anywhere in it, and a table whose
// closure carries a union has no blittable C# spelling (§19.3 pins that side
// to Sequential with generated padding, which cannot overlay arms).
//
// Everything is PRE-COOKED AT BUILD: every layout fact is settled by the
// compiler (ir.Blocks) and asserted into this side by generated static_asserts,
// so nothing is decided, discovered or checked at frame time.
//
// The fill path — Begin, the array accessors and the row storage they hand
// back — contains NO ALLOCATION, NO LOCK AND NO ATOMIC. That is an obligation
// on this backend rather than a permission to the caller (§19.1), and the
// Makefile's block-fill-refuser gate greps the marked region for one.
package cpptable

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

namespace ` + pkg + ` {

// ---- the block form's runtime (SPEC-TABLES.md §19) ----

// What a table knows about ONE of its out-of-line arrays: where the rows
// start, how many there are, and how far apart they sit. Sixteen bytes with no
// interior padding, sitting at the array field's own position in the
// projection (§2.7). A consumer reads all three FROM THE INSTANCE, never from
// its own constants — that is the difference between a generated pair of
// structs and an ABI (§19.2).
struct TableBlockTriple
{
    uint64_t offset_of; // block-relative: the block relocates by plain memcpy
    uint32_t count;     // rows the producer filled; rows past it are not part of the block
    uint32_t stride;    // the pitch the consumer indexes with, from the data
};

// THE CALLER'S ALLOCATOR, with malloc semantics. A block's storage is one
// extent sized from the declared maxima and allocated ONCE, at build time,
// through this pair — never at fill time, never per row, and never grown. The
// FILL path allocates nothing and takes no lock, which is the obligation the
// refuser holds (§19.1); "the block form never allocates" would be a claim
// about who owns the extent, and the answer is that the caller does.
struct TableBlockAllocator
{
    void * ( *alloc )( void * context, int64_t bytes );
    void ( *free )( void * context, void * pointer );
    void * context;
};

// The default pair, for a caller that has no allocator of its own to hand in.
// Nothing in the generated surface reaches for it: a caller names it.
inline void * table_block_default_alloc( void * context, int64_t bytes ) { (void) context; return malloc( (size_t) bytes ); }
inline void table_block_default_free( void * context, void * pointer ) { (void) context; ::free( pointer ); }

inline TableBlockAllocator TableBlockDefaultAllocator()
{
    TableBlockAllocator allocator;
    allocator.alloc = table_block_default_alloc;
    allocator.free = table_block_default_free;
    allocator.context = NULL;
    return allocator;
}

// THIS BUILD's byte order, as the prologue carries it (SPEC-TABLES.md §20.3).
// A block written by a build of the other order is REFUSED by BlockOpen: a
// big-endian fix-up path is a named obligation, not something a consumer
// improvises row by row.
#if defined( __BYTE_ORDER__ ) && defined( __ORDER_BIG_ENDIAN__ ) && __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__
inline constexpr uint64_t TableBlockByteOrder = 2; // big
#else
inline constexpr uint64_t TableBlockByteOrder = 1; // little
#endif

// Why Begin refused: the array, its count and its maximum (§19.1). Clamping a
// count to its maximum before Begin is the PRODUCER's job — Begin is a
// contract check, not a policy — and a producer at sixty hertz that silently
// drops a frame is worse than one that does not.
struct TableBlockRefusal
{
    const char * array = NULL;
    int64_t count = 0;
    int64_t maximum = 0;
};

// One array's rows, ITERATED at the pitch the instance gives (§19.2). A call
// site never spells the pitch arithmetic itself, for the same reason a keyed
// array's call sites should not re-derive their own slot rule. The implicit
// conversion to T* is the PRODUCER's typed base (§19.1): a worker holds it and
// indexes, and in a block this build just laid out the pitch IS sizeof.
template <typename T>
struct TableBlockRows
{
    uint8_t * base = NULL;
    int32_t count = 0;
    int32_t stride = 0;

    struct iterator
    {
        uint8_t * p;
        int32_t stride;
        T & operator*() const { return *(T *) p; }
        iterator & operator++() { p += stride; return *this; }
        bool operator!=( const iterator & other ) const { return p != other.p; }
    };

    iterator begin() const { return iterator{ base, stride }; }
    iterator end() const { return iterator{ base + (ptrdiff_t) count * stride, stride }; }
    int32_t size() const { return count; }
    operator T *() const { return (T *) base; }
};

// A CONTIGUOUS view of one array, available because the pitch IS sizeof
// rounded to the element's alignment — derived, always, with no declaration
// that adjusts it (§2.7). The stride still RIDES in the triple, because it is
// the pitch the consumer indexes with and it must come from the data.
template <typename T>
struct TableBlockSpan
{
    T * rows = NULL;
    int32_t count = 0;

    T * begin() const { return rows; }
    T * end() const { return rows + count; }
    int32_t size() const { return count; }
    T & operator[]( int32_t i ) const { return rows[i]; }
};

// ---- reflection over a block (SPEC-TABLES.md §8, §19.2) ----
//
// The descriptors are the mechanism, and they are what retires a hand-kept
// mirror: a consumer holding them reads the triples out of an instance and
// points at rows, with no hand-written struct per table and no knowledge of
// the spelling that produced any of it. They are constant data, so this costs
// a lookup, not a parse — and they are immutable, so any thread may read them.
struct TableBlockInfo;

struct TableBlockFieldInfo
{
    const char * name;
    uint32_t offset;  // the field's offset in the record this descriptor describes
    uint32_t size;    // its size there
    uint8_t kind;     // the table-wire kind, as TableFieldInfo carries it
    bool out_of_line; // an out-of-line array: the three members below are live
    uint32_t offset_of_offset; // the triple's offset_of member, or 0xffffffff
    uint32_t count_offset;     // its count member, or 0xffffffff
    uint32_t stride_offset;    // its stride member, or 0xffffffff
    uint32_t stride;           // THIS BUILD's pitch, to assert against — never to index with (§19.2)
    // the ELEMENT's or the nested record's own layout, behind a function so the
    // whole table stays constant-initialised. NULL when the field is a scalar.
    // Following it is how a walker DESCENDS: an out-of-line array's rows, and a
    // nested record's fields, are both reached through this one column.
    const TableBlockInfo * (*element)();
};

// One record's layout as DATA — the whole mechanism behind the block form's
// read side. A block-form table's own descriptor describes its PROJECTION; the
// element descriptor of each out-of-line array describes that array's ROW, and
// so on down. Nothing here is named at file scope: a walker reaches every
// record through the graph, which is what keeps the block form's reflection
// free of a name per row type.
struct TableBlockInfo
{
    const char * name;
    uint64_t build_version; // the unit's (SPEC-TABLES.md §20)
    uint32_t size;          // the record's own sizeof: a projection's, or a row's
    uint32_t align;
    int32_t num_fields;
    const TableBlockFieldInfo * fields;
};

// The block's magic, and the byte-order check with it (§19.1). It is stored in
// the producer's NATIVE order; a consumer that reads back the byte-swapped
// value has found a foreign byte order, and one that reads back anything else
// has not found a block at all.
inline constexpr uint64_t TableBlockMagic = 0x4b4c42414d484353ull;

inline uint64_t table_block_byteswap64( uint64_t v )
{
    return ( v >> 56 ) | ( ( v >> 40 ) & 0xff00ull ) | ( ( v >> 24 ) & 0xff0000ull ) | ( ( v >> 8 ) & 0xff000000ull )
         | ( ( v << 8 ) & 0xff00000000ull ) | ( ( v << 24 ) & 0xff0000000000ull ) | ( ( v << 40 ) & 0xff000000000000ull )
         | ( v << 56 );
}

// the prologue read BYTEWISE: the magic is the one field read without assuming
// the order the rest of the block is in
inline uint64_t table_block_read64( const uint8_t * p )
{
    uint64_t v = 0;
    memcpy( &v, p, 8 );
    return v;
}

inline int64_t table_block_align( int64_t offset, int64_t alignment )
{
    return ( offset + alignment - 1 ) / alignment * alignment;
}

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// generateBlockFiles emits <Base>Block.h and <Base>Block.cpp for every file of
// a unit that declares a table. A file whose tables all lack a block form
// still gets a header, saying which table and why — a form that goes missing
// without a word is the thing this avoids.
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
			g.pf("// table %s has NO block form: %s (SPEC-TABLES.md §19).\n", st.Name, blocks.SkippedReason(st.Name))
			g.pf("// Its wire (§3) and its cook (§7) are unaffected — only this projection\n")
			g.pf("// is absent, and it is absent by construction rather than by refusal.\n\n")
		}
		for _, bl := range formed {
			g.owner = bl.Table
			g.emitBlockSurface(bl)
			c.owner = bl.Table
			c.emitBlockDefinitions(bl)
		}
		out[f.Base+"Block.h"] = blockHeader(u, f, g, len(formed))
		if len(formed) > 0 {
			out[f.Base+"Block.cpp"] = blockSource(u, f, c)
		}
	}
	return out
}

func blockBanner(u *ir.Unit, f *ir.File) string {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the BLOCK FORM (SPEC-TABLES.md §19).\n", u.Package)
	return h.String()
}

func blockHeader(u *ir.Unit, f *ir.File, g *tableGen, formed int) []byte {
	var h strings.Builder
	h.WriteString(blockBanner(u, f))
	h.WriteString("//\n")
	h.WriteString("// NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on\n")
	h.WriteString("// the side: include this header and compile the .cpp beside it only if you use\n")
	h.WriteString("// the block form. The unit's <Base>Table.h carries not one symbol of it.\n")
	h.WriteString("//\n")
	h.WriteString("// A block is one flat extent: the table's own instance at the front — the\n")
	h.WriteString("// PROJECTION, carrying per bounded array of structs where its rows start, how\n")
	h.WriteString("// many there are and how far apart they sit — and then those rows, each array\n")
	h.WriteString("// at a fixed pitch. The other side reads those three facts and points.\n\n")
	h.WriteString("#pragma once\n\n")
	h.WriteString("#include <cstdint>\n#include <cstring>\n#include <cstddef> // offsetof, for the layout contract\n#include <cstdlib> // the DEFAULT allocator pair, for a caller with none of its own\n#include <type_traits> // the layout contract's standard-layout asserts\n")
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
	h.WriteString("\n")
	if formed > 0 {
		h.WriteString(buildVersionConstant(u.Package, ir.BuildVersion(u)))
		h.WriteString("\n")
		h.WriteString(blockRuntime(u.Package))
	}
	fmt.Fprintf(&h, "\nnamespace %s {\n\n", u.Package)
	h.WriteString(g.body.String())
	fmt.Fprintf(&h, "} // namespace %s\n", u.Package)
	return []byte(h.String())
}

func blockSource(u *ir.Unit, f *ir.File, c *tableGen) []byte {
	var s strings.Builder
	s.WriteString(blockBanner(u, f))
	s.WriteString("//\n")
	s.WriteString("// The block form's COLD half, in its own translation unit: the open path, run\n")
	s.WriteString("// once per handoff, and the reflection descriptors, which are constant data.\n")
	s.WriteString("// The per-frame FILL path stays inline in the header, because a call per row\n")
	s.WriteString("// accessor is exactly the cost this form exists to avoid.\n\n")
	fmt.Fprintf(&s, "#include \"%sBlock.h\"\n\n", f.Base)
	fmt.Fprintf(&s, "namespace %s {\n\n", u.Package)
	s.WriteString(c.body.String())
	fmt.Fprintf(&s, "} // namespace %s\n", u.Package)
	return []byte(s.String())
}

// emitBlockProjectionField renders one projection field. Every field keeps its
// by-value storage at its natural offset; a bounded array of structs — `T[N]`
// and its count companion — is replaced AT THAT FIELD'S POSITION by its
// sixteen-byte triple (SPEC-TABLES.md §2.7).
func (g *tableGen) emitBlockProjectionField(f *ir.Field) {
	if ir.BlockOutOfLine(f) {
		g.noteRef(f.Type.Name)
		g.pf("        TableBlockTriple %s = {}; // [..%d]%s laid out of line: (offset_of, count, stride)\n",
			f.Name, f.ArrayBound, f.Type.Name)
		return
	}
	saved := g.indent
	g.indent = saved + "    "
	g.emitTableStorageField(f)
	g.indent = saved
}

// emitBlockSurface emits one table's whole block form into the header.
func (g *tableGen) emitBlockSurface(bl *ir.BlockLayout) {
	name := bl.Table.Name

	g.pf("// ---- the block form of table %s (SPEC-TABLES.md §19): begin ----\n\n", name)

	g.pf("// The counts, gathered before Begin — nothing about counting is concurrent\n")
	g.pf("// (SPEC-TABLES.md §19.1). Clamping a count to its maximum is the PRODUCER's job.\n")
	g.pf("struct %sCounts\n{\n", name)
	for _, a := range bl.Arrays {
		g.pf("    int32_t %s = 0; // [0, %d]\n", a.Field.Name, a.Max)
	}
	if len(bl.Arrays) == 0 {
		g.pf("    int32_t unused = 0; // the table declares no out-of-line array\n")
	}
	g.pf("};\n\n")

	g.pf("// The block's STORAGE, sized from the declared maxima: one extent, allocated\n")
	g.pf("// once, never grown, never pooled (SPEC-TABLES.md §19.1). The sum is loose by\n")
	g.pf("// construction — arrays commonly draw from one shared pool, so their maxima\n")
	g.pf("// can add to more than can ever be occupied at once.\n")
	g.pf("// It is allocated ONCE, at build time, through the CALLER'S allocator, and\n")
	g.pf("// released through the same pair. The fill path allocates nothing.\n")
	g.pf("inline constexpr int64_t %sBlockMaxBytes = %d;\n\n", name, bl.MaxBytes)
	g.pf("struct %sBlockStorage\n{\n", name)
	g.pf("    uint8_t * base = NULL;             // the extent, 64-byte aligned\n")
	g.pf("    void * allocation = NULL;          // what the allocator handed back\n")
	g.pf("    TableBlockAllocator allocator = {};\n\n")
	g.pf("    // Create allocates %sBlockMaxBytes + %d bytes through the caller's pair\n", name, ir.BlockAlign-1)
	g.pf("    // and aligns the base inside them: malloc's guarantee is not a cache\n")
	g.pf("    // line's, and the 64-byte base is what keeps two workers filling\n")
	g.pf("    // different arrays off one line (§19.1). One call, at build time.\n")
	g.pf("    bool Create( const TableBlockAllocator & from )\n    {\n")
	g.pf("        allocator = from;\n")
	g.pf("        allocation = allocator.alloc( allocator.context, %sBlockMaxBytes + %d );\n", name, ir.BlockAlign-1)
	g.pf("        if ( allocation == NULL ) { base = NULL; return false; }\n")
	g.pf("        const uintptr_t raw = (uintptr_t) allocation;\n")
	g.pf("        base = (uint8_t *) ( ( raw + %d ) & ~(uintptr_t) %d );\n", ir.BlockAlign-1, ir.BlockAlign-1)
	g.pf("        return true;\n    }\n\n")
	g.pf("    void Destroy()\n    {\n")
	g.pf("        if ( allocation != NULL ) { allocator.free( allocator.context, allocation ); }\n")
	g.pf("        allocation = NULL;\n        base = NULL;\n    }\n")
	g.pf("};\n\n")

	g.pf("// The block: one extent, 64-byte aligned at its base, the PROJECTION at\n")
	g.pf("// offset 0 and then each out-of-line array in declaration order\n")
	g.pf("// (SPEC-TABLES.md §19.1). Sixty-four is a cache line, and the guarantee is PER\n")
	g.pf("// ARRAY: two workers filling different arrays never share one. Inside one\n")
	g.pf("// array the pitch is the element's sizeof, so two workers meeting at a range\n")
	g.pf("// boundary share the one line that straddles it — bounded at one line per\n")
	g.pf("// boundary, not one per row.\n")
	g.pf("//\n")
	g.pf("// A block is valid until the next Begin on the SAME storage, which invalidates\n")
	g.pf("// every block over it and every row pointer taken from one. Double-buffering\n")
	g.pf("// is therefore two storages, and it is the caller's.\n")
	g.pf("struct %sBlock\n{\n", name)
	g.pf("    // The PROJECTION: a record like any other, following the same C ABI rule\n")
	g.pf("    // (§19.3) — its own offsets are part of the contract, not scaffolding\n")
	g.pf("    // around it. It opens with the generated PROLOGUE of three uint64s.\n")
	g.pf("    struct Projection\n    {\n")
	g.pf("        uint64_t magic = 0;         // generated: identifies a schema block\n")
	g.pf("        uint64_t build_version = 0; // generated: the unit's build version (SPEC-TABLES.md §20)\n")
	g.pf("        uint64_t byte_order = 0;    // generated: 1 little, 2 big — the producer stamps its own\n")
	for _, f := range bl.Table.Fields {
		g.emitBlockProjectionField(f)
	}
	g.pf("    };\n\n")
	g.pf("    uint8_t * base = NULL;          // the extent's base, 64-byte aligned\n")
	g.pf("    Projection * projection = NULL; // the projection, at offset 0\n")
	g.pf("    int64_t bytes = 0;              // the extent in use\n")
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.pf("\n    // %s: the constants this build asserts against. A consumer INDEXES with\n", a.Field.Name)
		g.pf("    // what it read from the instance, never with these (§19.2).\n")
		g.pf("    static constexpr int64_t %sStride = %d;\n", field, a.Stride)
		g.pf("    static constexpr int64_t %sMax = %d;\n", field, a.Max)
		g.pf("    static constexpr int64_t %sProjectionOffset = %d;\n", field, a.TripleOffset)
	}
	g.pf("\n    // this table's block descriptors (SPEC-TABLES.md §8, §19.2): constant\n")
	g.pf("    // data, defined in the .cpp beside this header.\n")
	g.pf("    static const TableBlockInfo * Type();\n")
	g.pf("};\n\n")

	g.emitBlockLayoutAsserts(bl)
	g.emitBlockFillPath(bl)

	g.pf("// BlockOpen checks once and points, and this is the WHOLE check (§19.2): the\n")
	g.pf("// magic read bytewise, the BYTE ORDER the prologue carries against this\n")
	g.pf("// build's own, the BUILD VERSION against this build's own, each array's\n")
	g.pf("// pitch, its offset_of, its COUNT against the declared maximum and its\n")
	g.pf("// extent inside the block, the used extent against the bytes the caller\n")
	g.pf("// passed, and the base's alignment.\n")
	g.pf("// On a match the bytes are what a build with this layout wrote, so there is\n")
	g.pf("// nothing to validate and nothing to fix up. On any failure it returns false\n")
	g.pf("// and points at nothing.\n")
	g.pf("//\n")
	g.pf("// There is ONE entry point, and no tolerant twin: the block form is same-build\n")
	g.pf("// by construction — both sides are generated from one declaration at one build\n")
	g.pf("// and ship together — so a consumer older than its producer is not a case. A\n")
	g.pf("// mismatch is a refusal; regenerate both sides. Data that must outlive the\n")
	g.pf("// build that wrote it takes the wire (§3), which this same table still has.\n")
	g.pf("bool %sBlockOpen( %sBlock & block, void * base, int64_t bytes );\n\n", name, name)

	g.pf("// ---- the block form of table %s: end ----\n\n", name)
}

// emitBlockLayoutAsserts is this backend's half of the LAYOUT CONTRACT
// (SPEC-TABLES.md §19.3): the compiler computes the layout and each backend
// emits code asserting that ITS OWN compiler agrees. A disagreement is a build
// error on the side that disagrees, naming the type, the field and the offset.
func (g *tableGen) emitBlockLayoutAsserts(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("// The LAYOUT CONTRACT (SPEC-TABLES.md §19.3). The compiler derived every\n")
	g.pf("// number below from the declaration; these asserts are this compiler saying\n")
	g.pf("// whether it agrees. Neither side's layout is inferred from the other's —\n")
	g.pf("// both are checked against their own compiler's model, which is the only way\n")
	g.pf("// a two-language contract can be held by a compiler that generates both.\n")
	g.pf("static_assert( sizeof( bool ) == 1, \"a bool in a block row is ONE byte: the standard leaves sizeof(bool) implementation-defined, and this two-language layout contract does not (SPEC-TABLES.md §19.3)\" );\n")
	g.pf("static_assert( sizeof( TableBlockTriple ) == 16, \"a triple is sixteen bytes with no interior padding (SPEC-TABLES.md §2.7)\" );\n")
	g.pf("static_assert( offsetof( TableBlockTriple, offset_of ) == 0 && offsetof( TableBlockTriple, count ) == 8 && offsetof( TableBlockTriple, stride ) == 12, \"a triple's members sit at 0/8/12 (SPEC-TABLES.md §2.7)\" );\n")
	g.pf("static_assert( std::is_standard_layout<%sBlock::Projection>::value, \"%s's block projection must stay standard-layout for offsetof\" );\n", name, name)
	g.pf("static_assert( std::is_trivially_copyable<%sBlock::Projection>::value, \"%s's block projection must stay relocatable\" );\n", name, name)
	g.pf("static_assert( sizeof( %sBlock::Projection ) == %d, \"%s's block projection sizeof moved: the C# side asserts %d for the same declaration (SPEC-TABLES.md §19.3)\" );\n",
		name, bl.Projection.Size, name, bl.Projection.Size)
	g.pf("static_assert( alignof( %sBlock::Projection ) == %d, \"%s's block projection alignof moved (SPEC-TABLES.md §19.3)\" );\n",
		name, bl.Projection.Align, name)
	g.pf("static_assert( offsetof( %sBlock::Projection, magic ) == 0, \"the block prologue's magic sits at offset 0 (SPEC-TABLES.md §19.1)\" );\n", name)
	g.pf("static_assert( offsetof( %sBlock::Projection, build_version ) == 8, \"the block prologue's build_version sits at offset 8 (SPEC-TABLES.md §19.1, §20)\" );\n", name)
	g.pf("static_assert( offsetof( %sBlock::Projection, byte_order ) == 16, \"the block prologue's byte_order sits at offset 16 (SPEC-TABLES.md §19.1)\" );\n", name)
	for _, fl := range bl.Projection.Fields {
		g.pf("static_assert( offsetof( %sBlock::Projection, %s ) == %d, \"%s's projection field %s moved: the C# side asserts %d (SPEC-TABLES.md §19.3)\" );\n",
			name, fl.Field.Name, fl.Offset, name, fl.Field.Name, fl.Offset)
	}
	seen := map[string]bool{}
	for _, a := range bl.Arrays {
		g.emitBlockRowAsserts(a.ElemName, seen)
		field := ir.GoExportName(a.Field.Name)
		g.pf("static_assert( %sBlock::%sStride == (int64_t) sizeof( %s ), \"%s's pitch is its element's sizeof, always (SPEC-TABLES.md §2.7)\" );\n",
			name, field, a.ElemName, a.Field.Name)
	}
	g.pf("\n")
}

// emitBlockRowAsserts pins one row type's own layout — the same facts the C#
// side asserts under the managed model, from the same compiler-derived numbers.
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
	g.pf("static_assert( sizeof( %s ) == %d, \"block row %s's sizeof moved: the C# side asserts %d for the same declaration (SPEC-TABLES.md §19.3)\" );\n",
		name, ml.Size, name, ml.Size)
	g.pf("static_assert( alignof( %s ) == %d, \"block row %s's alignof moved (SPEC-TABLES.md §19.3)\" );\n", name, ml.Align, name)
	for _, fl := range ml.Fields {
		g.pf("static_assert( offsetof( %s, %s ) == %d, \"block row %s's field %s moved: the C# side asserts %d (SPEC-TABLES.md §19.3)\" );\n",
			name, fl.Field.Name, fl.Offset, name, fl.Field.Name, fl.Offset)
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
// PATH, and the whole of what the conformance refuser (SPEC-TABLES.md §19.1,
// §19.5) claims. Between the markers below there is no allocation, no lock and
// no atomic, and the Makefile's block-fill-refuser gate fails the build if one
// appears. It stays INLINE in the header: a call per row accessor is exactly
// the cost this form exists to avoid.
func (g *tableGen) emitBlockFillPath(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("// ---- block fill path: begin ----\n")
	g.pf("// THE MULTI-THREADED FILL IS AN OBLIGATION ON THIS BACKEND, not a permission\n")
	g.pf("// to the caller (SPEC-TABLES.md §19.1). Nothing between these markers\n")
	g.pf("// allocates, locks or takes an atomic; the Makefile's block-fill-refuser gate\n")
	g.pf("// fails the build if one appears. The parallelism itself lives in the\n")
	g.pf("// caller's loop — N workers, disjoint index ranges, no synchronisation of any\n")
	g.pf("// kind — and keeping this surface free of those three is what MAKES it\n")
	g.pf("// possible.\n\n")

	g.pf("// The LAYOUT is settled once per block, from the counts, before any worker\n")
	g.pf("// starts. Begin refuses counts past the declared maxima NAMING the array, its\n")
	g.pf("// count and its maximum; stamps the prologue; writes every array's offset_of,\n")
	g.pf("// count and stride; and hands back the block. It touches no row, and it is\n")
	g.pf("// O( out-of-line arrays ) — a handful, not thousands. The projection's own\n")
	g.pf("// fields are the producer's to write, exactly as the rows are: a caller that\n")
	g.pf("// needs a byte-stable artifact zeroes the storage once (§19.1).\n")
	g.pf("//\n")
	g.pf("// Begin and BlockBytes are SINGLE-THREADED: call Begin before the workers and\n")
	g.pf("// BlockBytes after they join (§19.1).\n")
	g.pf("inline bool %sBlockBegin( %sBlock & block, %sBlockStorage & storage, const %sCounts & counts, TableBlockRefusal * refusal = NULL )\n{\n",
		name, name, name, name)
	for _, a := range bl.Arrays {
		g.pf("    if ( counts.%s < 0 || counts.%s > %d )\n    {\n", a.Field.Name, a.Field.Name, a.Max)
		g.pf("        if ( refusal ) { refusal->array = \"%s\"; refusal->count = counts.%s; refusal->maximum = %d; }\n",
			a.Field.Name, a.Field.Name, a.Max)
		g.pf("        return false;\n    }\n")
	}
	if len(bl.Arrays) == 0 {
		g.pf("    (void) counts; (void) refusal; // no out-of-line array: no count to refuse\n")
	}
	g.pf("    if ( storage.base == NULL )\n        return false; // Create was not called, or the allocator refused\n")
	g.pf("    block.base = storage.base;\n")
	g.pf("    block.projection = (%sBlock::Projection *) storage.base;\n", name)
	g.pf("    block.projection->magic = TableBlockMagic;\n")
	g.pf("    block.projection->build_version = BuildVersion;\n")
	g.pf("    block.projection->byte_order = TableBlockByteOrder;\n")
	g.pf("    int64_t offset = %d; // sizeof the projection\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		g.pf("    offset = table_block_align( offset, %d ); // max( 64, alignof( %s ) )\n", blockStartAlign(a), a.ElemName)
		g.pf("    block.projection->%s.offset_of = (uint64_t) offset;\n", a.Field.Name)
		g.pf("    block.projection->%s.count = (uint32_t) counts.%s;\n", a.Field.Name, a.Field.Name)
		g.pf("    block.projection->%s.stride = (uint32_t) sizeof( %s );\n", a.Field.Name, a.ElemName)
		g.pf("    offset += (int64_t) counts.%s * (int64_t) sizeof( %s );\n", a.Field.Name, a.ElemName)
	}
	g.pf("    block.bytes = table_block_align( offset, %d );\n", ir.BlockAlign)
	g.pf("    return true;\n}\n\n")

	g.pf("// The USED extent: the greatest offset_of + count * stride, rounded up to 64,\n")
	g.pf("// never less than the projection's own size (SPEC-TABLES.md §19.1). Because\n")
	g.pf("// the layout follows the counts it is proportional to the frame rather than\n")
	g.pf("// to the maxima. The tail — the bytes between the last row and the rounding —\n")
	g.pf("// is UNSPECIFIED, because zeroing megabytes per frame is the cost this form\n")
	g.pf("// exists to avoid.\n")
	g.pf("inline int64_t %sBlockBytes( const %sBlock & block )\n{\n", name, name)
	if len(bl.Arrays) == 0 {
		g.pf("    (void) block; // no out-of-line array: the extent is the projection's own\n")
	}
	g.pf("    int64_t used = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		g.pf("    {\n")
		g.pf("        const int64_t end = (int64_t) block.projection->%s.offset_of + (int64_t) block.projection->%s.count * (int64_t) block.projection->%s.stride;\n",
			a.Field.Name, a.Field.Name, a.Field.Name)
		g.pf("        if ( end > used ) used = end;\n")
		g.pf("    }\n")
	}
	g.pf("    return table_block_align( used, %d );\n}\n\n", ir.BlockAlign)

	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.pf("// %s: an accessor is ONE ADD — block base + offset_of — typed as the element.\n", a.Field.Name)
		g.pf("// A worker holds it and indexes; disjoint index ranges into one array are\n")
		g.pf("// safe concurrently, and two workers writing one row is the caller's problem\n")
		g.pf("// (SPEC-TABLES.md §19.1). Iterating steps at the pitch the INSTANCE gives.\n")
		g.pf("inline TableBlockRows<%s> %s%s( const %sBlock & block )\n{\n", a.ElemName, name, field, name)
		g.pf("    TableBlockRows<%s> rows;\n", a.ElemName)
		g.pf("    rows.base = block.base + block.projection->%s.offset_of;\n", a.Field.Name)
		g.pf("    rows.count = (int32_t) block.projection->%s.count;\n", a.Field.Name)
		g.pf("    rows.stride = (int32_t) block.projection->%s.stride;\n", a.Field.Name)
		g.pf("    return rows;\n}\n\n")

		g.pf("// %s, as a CONTIGUOUS view — available because the pitch IS sizeof (§2.7),\n", a.Field.Name)
		g.pf("// which is how the fast path is actually written.\n")
		g.pf("inline TableBlockSpan<%s> %s%sSpan( const %sBlock & block )\n{\n", a.ElemName, name, field, name)
		g.pf("    TableBlockSpan<%s> span;\n", a.ElemName)
		g.pf("    span.rows = (%s *) ( block.base + block.projection->%s.offset_of );\n", a.ElemName, a.Field.Name)
		g.pf("    span.count = (int32_t) block.projection->%s.count;\n", a.Field.Name)
		g.pf("    return span;\n}\n\n")
	}
	g.pf("// ---- block fill path: end ----\n\n")
}

// blockStartAlign is where one out-of-line array begins: aligned to
// max( 64, alignof( element ) ) (SPEC-TABLES.md §19.1).
func blockStartAlign(a ir.BlockArray) int64 {
	if a.ElemAlign() > int64(ir.BlockAlign) {
		return a.ElemAlign()
	}
	return int64(ir.BlockAlign)
}

// emitBlockDefinitions emits the COLD half into <Base>Block.cpp: the open path
// and the reflection descriptors.
func (g *tableGen) emitBlockDefinitions(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("// ---- the block form of table %s: the open path and the descriptors ----\n\n", name)
	g.emitBlockOpenBody(bl)
	g.emitBlockDescriptors(bl)
}

func (g *tableGen) emitBlockOpenBody(bl *ir.BlockLayout) {
	name := bl.Table.Name
	g.pf("bool %sBlockOpen( %sBlock & block, void * base, int64_t bytes )\n{\n", name, name)
	g.pf("    block.base = NULL;\n    block.projection = NULL;\n    block.bytes = 0;\n")
	g.pf("    if ( base == NULL || bytes < %d )\n        return false;\n", bl.Projection.Size)
	g.pf("    if ( ( (uintptr_t) base %% %d ) != 0 )\n        return false; // the base's alignment\n", ir.BlockAlign)
	g.pf("    const uint8_t * raw = (const uint8_t *) base;\n")
	g.pf("    const uint64_t magic = table_block_read64( raw );\n")
	g.pf("    if ( magic != TableBlockMagic )\n")
	g.pf("    {\n")
	g.pf("        // a byte-swapped magic is a FOREIGN BYTE ORDER, and anything else is\n")
	g.pf("        // not a block at all. Both refuse; the distinction is here so a\n")
	g.pf("        // reader of this code knows the check covers the order too.\n")
	g.pf("        (void) table_block_byteswap64( magic );\n")
	g.pf("        return false;\n")
	g.pf("    }\n")
	g.pf("    if ( table_block_read64( raw + 8 ) != BuildVersion )\n        return false;\n")
	g.pf("    if ( table_block_read64( raw + 16 ) != TableBlockByteOrder )\n")
	g.pf("        return false; // a block of the other byte order: the fix-up path is a named obligation\n")
	g.pf("    %sBlock::Projection * projection = (%sBlock::Projection *) base;\n", name, name)
	g.pf("    int64_t used = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		g.pf("    {\n")
		g.pf("        // EVERY NUMBER BELOW COMES FROM THE INSTANCE, so the arithmetic is\n")
		g.pf("        // unsigned and each term is BOUNDED BEFORE IT IS ADDED. A forged\n")
		g.pf("        // offset_of near 2^63 must refuse, and an addition that carried past\n")
		g.pf("        // the top of the type would be the undefined behaviour the check\n")
		g.pf("        // after it was supposed to catch.\n")
		g.pf("        const uint64_t offset_of = projection->%s.offset_of;\n", a.Field.Name)
		g.pf("        const uint64_t count = projection->%s.count;\n", a.Field.Name)
		g.pf("        const uint64_t stride = projection->%s.stride;\n", a.Field.Name)
		g.pf("        if ( stride != (uint64_t) sizeof( %s ) )\n            return false;\n", a.ElemName)
		g.pf("        if ( count > (uint64_t) %sBlock::%sMax )\n", name, ir.GoExportName(a.Field.Name))
		g.pf("            return false; // past the DECLARED MAXIMUM: Begin refuses this on the\n")
		g.pf("                          // producer side and Open refuses it here, because a\n")
		g.pf("                          // consumer that sizes anything by the maximum would\n")
		g.pf("                          // overflow on a count the maximum does not bound\n")
		g.pf("        if ( offset_of < %d || ( offset_of %% %d ) != 0 )\n            return false;\n", bl.Projection.Size, blockStartAlign(a))
		g.pf("        if ( offset_of > (uint64_t) bytes )\n            return false;\n")
		g.pf("        const uint64_t rows = count * stride; // both bounded above: this cannot carry\n")
		g.pf("        if ( rows > (uint64_t) bytes - offset_of )\n            return false;\n")
		g.pf("        const int64_t end = (int64_t) ( offset_of + rows );\n")
		g.pf("        if ( end > used ) used = end;\n")
		g.pf("    }\n")
	}
	g.pf("    // the used extent, rounded to %d WITHOUT the rounding itself carrying past\n", ir.BlockAlign)
	g.pf("    // the top of the type: used is already inside bytes, and the padding is\n")
	g.pf("    // paid out of the slack that is left rather than added and compared after.\n")
	g.pf("    const int64_t padding = ( %d - ( used %% %d ) ) %% %d;\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	g.pf("    if ( padding > bytes - used )\n        return false;\n")
	g.pf("    used += padding;\n")
	g.pf("    block.base = (uint8_t *) base;\n")
	g.pf("    block.projection = projection;\n")
	g.pf("    block.bytes = used;\n")
	g.pf("    return true;\n}\n\n")
}

// emitBlockDescriptors emits one table's block reflection: the projection
// offset of every field, the offsets of the three members inside each triple,
// and the ELEMENT's own descriptor beside them (SPEC-TABLES.md §8, §19.2).
// A consumer holding these reads the facts out of an instance and points at
// rows, with no hand-written struct per table.
//
// The data sits in an ANONYMOUS NAMESPACE and is reached through the block
// type's own static member, so the block form claims no file-scope name beyond
// §11's set.
func (g *tableGen) emitBlockDescriptors(bl *ir.BlockLayout) {
	name := bl.Table.Name
	// every record this block's descriptors reach, in a stable order
	records := blockDescriptorRecords(g.blocks, bl)
	g.pf("namespace {\n\n")
	g.pf("// forward declarations, so the element column can be a constant\n")
	g.pf("// expression whatever order the records fall in\n")
	for _, r := range records {
		g.pf("extern const TableBlockInfo %s;\n", blockInfoSymbol(name, r))
	}
	g.pf("extern const TableBlockInfo %s;\n\n", blockInfoSymbol(name, ""))
	for _, r := range records {
		g.emitBlockRecordDescriptor(name, r, g.blocks.Layout(r), nil)
	}
	g.emitBlockRecordDescriptor(name, "", &bl.Projection, bl)
	g.pf("} // namespace\n\n")
	g.pf("const TableBlockInfo * %sBlock::Type() { return &%s; }\n\n", name, blockInfoSymbol(name, ""))
}

// emitBlockRecordDescriptor emits one record's field table and its info. When
// bl is non-nil the record is that block's PROJECTION; otherwise it is a row
// or something a row nests by value.
func (g *tableGen) emitBlockRecordDescriptor(owner, record string, ml *ir.MemberLayout, bl *ir.BlockLayout) {
	if ml == nil {
		return
	}
	symbol := blockInfoSymbol(owner, record)
	name := record
	if bl != nil {
		name = bl.Table.Name
	}
	g.pf("const TableBlockFieldInfo %s_fields[] = {\n", symbol)
	for _, fl := range ml.Fields {
		f := fl.Field
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		if bl != nil {
			if a := bl.ArrayByName(f.Name); a != nil {
				g.pf("    { \"%s\", %du, %du, %d, true, %du, %du, %du, %du, +[]() { return &%s; } },\n",
					f.Name, fl.Offset, fl.Size, kind, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride,
					blockInfoSymbol(owner, a.ElemName))
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
				element = fmt.Sprintf("+[]() { return &%s; }", blockInfoSymbol(owner, ref.Name))
			}
		}
		g.pf("    { \"%s\", %du, %du, %d, false, 0xffffffffu, 0xffffffffu, 0xffffffffu, 0u, %s },\n",
			f.Name, fl.Offset, fl.Size, kind, element)
	}
	g.pf("};\n\n")
	g.pf("const TableBlockInfo %s = { \"%s\", BuildVersion, %du, %du, %d, %s_fields };\n\n",
		symbol, name, ml.Size, ml.Align, len(ml.Fields), symbol)
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

// blockInfoSymbol names one record's descriptor inside its owner's anonymous
// namespace. The empty record is the owner's own projection.
func blockInfoSymbol(owner, record string) string {
	if record == "" {
		return strings.ToLower(owner) + "_block_projection"
	}
	return strings.ToLower(owner) + "_block_row_" + strings.ToLower(record)
}
