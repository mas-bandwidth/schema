// The BLOCK FORM in C++ (SPEC-TABLES.md §2.7, §19): the builder half.
//
// A table marked `| block` gains a third projection of the same declaration
// beside its wire (§3) and its cook (§7). Everything here is PRE-COOKED AT
// BUILD: every layout fact is settled by the compiler (ir.Blocks) and asserted
// into this side by generated static_asserts, so nothing is decided,
// discovered or checked at frame time.
//
// The fill path — Begin, the array accessors and the row storage they hand
// back — contains NO ALLOCATION, NO LOCK AND NO ATOMIC. That is an obligation
// on this backend rather than a permission to the caller (§19.1), and the
// Makefile's block-fill-refuser gate greps the marked region below for one.
package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockRuntime is the shared block runtime, emitted into the per-package
// primitives guard of a unit that marks a table and into NO OTHER (the
// zero-cost gate, §2.2).
func blockRuntime() string {
	return `
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
        T & operator*() const { return *(T*) p; }
        iterator & operator++() { p += stride; return *this; }
        bool operator!=( const iterator & other ) const { return p != other.p; }
    };

    iterator begin() const { return iterator{ base, stride }; }
    iterator end() const { return iterator{ base + (ptrdiff_t) count * stride, stride }; }
    int32_t size() const { return count; }
    operator T *() const { return (T *) base; }
};

// A CONTIGUOUS view of one array, available because the pitch IS sizeof
// (§2.7). A version that let a declaration widen the pitch would cost this.
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

// the prologue read BYTEWISE: the magic is the one field read without
// assuming the order the rest of the block is in
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
`
}

// unitHasBlock reports whether the unit marks any table `| block` — the one
// question the zero-cost gate asks (SPEC-TABLES.md §2.2).
func unitHasBlock(b *ir.BlockUnit) bool { return b != nil && len(b.Tables) > 0 }

// blockMember renders a projection field's storage spelling. Every field keeps
// its by-value storage at its natural offset; an out-of-line array's storage —
// `T[N]` and its count companion — is replaced AT THAT FIELD'S POSITION by its
// sixteen-byte triple (SPEC-TABLES.md §2.7).
func (g *tableGen) emitBlockProjectionField(f *ir.Field) {
	if ir.BlockOutOfLine(f) {
		g.pf("        TableBlockTriple %s = {}; // [..%d]%s laid out of line: (offset_of, count, stride)\n",
			f.Name, f.ArrayBound, f.Type.Name)
		return
	}
	saved := g.indent
	g.indent = saved + "    "
	g.emitTableStorageField(f)
	g.indent = saved
}

// emitBlockSurface emits one marked table's whole block form.
func (g *tableGen) emitBlockSurface(bl *ir.BlockLayout) {
	name := bl.Table.Name

	g.pf("// ---- the block form of table %s (SPEC-TABLES.md §19): begin ----\n\n", name)

	// the layout digest, keyed as §19.3 states: the projection's own fields
	// and their offsets and sizes, each out-of-line array's element and pitch,
	// and every row field's offset, size and kind. A declared MAXIMUM is
	// deliberately excluded — a consumer takes every offset_of from the
	// instance, so raising one is absorbed on the default entry point (§19.4).
	g.pf("// The LAYOUT ID: a 64-bit digest over the facts §18.2 refuses to move.\n")
	g.pf("// A declared MAXIMUM is deliberately NOT one of them (§19.3), which is what\n")
	g.pf("// makes a raised maximum absorbable on the default entry point (§19.4).\n")
	g.pf("inline constexpr uint64_t %sBlockLayoutId = 0x%016xull;\n\n", name, bl.LayoutId)

	// the counts, gathered before Begin, which is single-threaded
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

	// the storage: the allocate-max law
	g.pf("// The block's STORAGE, sized from the declared maxima: one extent, allocated\n")
	g.pf("// once, never grown, never pooled (SPEC-TABLES.md §19.1). The sum is loose by\n")
	g.pf("// construction — arrays commonly draw from one shared pool, so their maxima\n")
	g.pf("// can add to more than can ever be occupied at once.\n")
	g.pf("inline constexpr int64_t %sBlockMaxBytes = %d;\n\n", name, bl.MaxBytes)
	g.pf("struct alignas( %d ) %sBlockStorage\n{\n", ir.BlockAlign, name)
	g.pf("    uint8_t bytes[%sBlockMaxBytes];\n", name)
	g.pf("};\n\n")

	// the block handle and its projection
	g.pf("// The block: one extent, 64-byte aligned at its base, the PROJECTION at\n")
	g.pf("// offset 0 and then each out-of-line array in declaration order\n")
	g.pf("// (SPEC-TABLES.md §19.1). A block is valid until the next Begin on the SAME\n")
	g.pf("// storage, which invalidates every block over it and every row pointer taken\n")
	g.pf("// from one — double-buffering is therefore two storages, and it is yours.\n")
	g.pf("struct %sBlock\n{\n", name)
	g.pf("    // The PROJECTION: a record like any other, following the same C ABI rule\n")
	g.pf("    // (§19.3) — its own offsets are part of the contract, not scaffolding\n")
	g.pf("    // around it. It opens with the generated PROLOGUE of two uint64s.\n")
	g.pf("    struct Projection\n    {\n")
	g.pf("        uint64_t magic = 0;     // generated: identifies a schema block, and the byte order with it\n")
	g.pf("        uint64_t layout_id = 0; // generated: the digest §19.3 defines\n")
	for _, f := range bl.Table.Fields {
		g.emitBlockProjectionField(f)
	}
	g.pf("    };\n\n")
	g.pf("    uint8_t * base = NULL;          // the extent's base, 64-byte aligned\n")
	g.pf("    Projection * projection = NULL; // the projection, at offset 0\n")
	g.pf("    int64_t bytes = 0;              // the extent the caller owns\n")
	for _, a := range bl.Arrays {
		field := ir.GoExportName(a.Field.Name)
		g.pf("\n    // %s: the constants this build asserts against — a consumer INDEXES with\n", a.Field.Name)
		g.pf("    // what it read from the instance, never with these (§19.2).\n")
		g.pf("    static constexpr int64_t %sStride = %d;\n", field, a.Stride)
		g.pf("    static constexpr int64_t %sMax = %d;\n", field, a.Max)
		g.pf("    static constexpr int64_t %sProjectionOffset = %d;\n", field, a.TripleOffset)
	}
	g.pf("};\n\n")

	g.emitBlockLayoutAsserts(bl)
	g.emitBlockFillPath(bl)
	g.emitBlockOpen(bl)

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
	g.pf("// whether it agrees. Neither side's layout is inferred from the other's.\n")
	g.pf("static_assert( sizeof( bool ) == 1, \"a bool in a block row is ONE byte: the standard leaves sizeof(bool) implementation-defined, and the block form's two-language layout contract does not (SPEC-TABLES.md §19.3)\" );\n")
	g.pf("static_assert( std::is_standard_layout<%sBlock::Projection>::value, \"%s's block projection must stay standard-layout for offsetof\" );\n", name, name)
	g.pf("static_assert( std::is_trivially_copyable<%sBlock::Projection>::value, \"%s's block projection must stay relocatable\" );\n", name, name)
	g.pf("static_assert( sizeof( %sBlock::Projection ) == %d, \"%s's block projection sizeof moved: the C# side asserts %d for the same declaration (SPEC-TABLES.md §19.3)\" );\n",
		name, bl.Projection.Size, name, bl.Projection.Size)
	g.pf("static_assert( alignof( %sBlock::Projection ) == %d, \"%s's block projection alignof moved (SPEC-TABLES.md §19.3)\" );\n",
		name, bl.Projection.Align, name)
	g.pf("static_assert( offsetof( %sBlock::Projection, magic ) == 0, \"the block prologue's magic sits at offset 0 (SPEC-TABLES.md §19.1)\" );\n", name)
	g.pf("static_assert( offsetof( %sBlock::Projection, layout_id ) == 8, \"the block prologue's layout_id sits at offset 8 (SPEC-TABLES.md §19.1)\" );\n", name)
	for _, fl := range bl.Projection.Fields {
		g.pf("static_assert( offsetof( %sBlock::Projection, %s ) == %d, \"%s's projection field %s moved: the C# side asserts %d (SPEC-TABLES.md §19.3)\" );\n",
			name, fl.Field.Name, fl.Offset, name, fl.Field.Name, fl.Offset)
	}
	g.pf("static_assert( sizeof( TableBlockTriple ) == 16, \"a triple is sixteen bytes with no interior padding (SPEC-TABLES.md §2.7)\" );\n")
	g.pf("static_assert( offsetof( TableBlockTriple, offset_of ) == 0 && offsetof( TableBlockTriple, count ) == 8 && offsetof( TableBlockTriple, stride ) == 12, \"a triple's members sit at 0/8/12 (SPEC-TABLES.md §2.7)\" );\n")

	// every row type, and every field of each
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
	// everything a row nests by value is a record the other side asserts too
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
// appears.
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

	// Begin
	g.pf("// The LAYOUT is settled once per block, from the counts, before any worker\n")
	g.pf("// starts. Begin refuses counts past the declared maxima NAMING the array, its\n")
	g.pf("// count and its maximum; stamps the prologue; writes every array's offset_of,\n")
	g.pf("// count and stride; and hands back the block. It touches no row, and it is\n")
	g.pf("// O( out-of-line arrays ) — a handful, not thousands.\n")
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
	g.pf("    block.base = storage.bytes;\n")
	g.pf("    block.projection = (%sBlock::Projection *) storage.bytes;\n", name)
	g.pf("    block.projection->magic = TableBlockMagic;\n")
	g.pf("    block.projection->layout_id = %sBlockLayoutId;\n", name)
	g.pf("    int64_t offset = %d; // sizeof the projection\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		start := ir.BlockAlign
		if a.ElemAlign() > int64(start) {
			start = int(a.ElemAlign())
		}
		g.pf("    offset = table_block_align( offset, %d ); // max( 64, alignof( %s ) )\n", start, a.ElemName)
		g.pf("    block.projection->%s.offset_of = (uint64_t) offset;\n", a.Field.Name)
		g.pf("    block.projection->%s.count = (uint32_t) counts.%s;\n", a.Field.Name, a.Field.Name)
		g.pf("    block.projection->%s.stride = (uint32_t) sizeof( %s );\n", a.Field.Name, a.ElemName)
		g.pf("    offset += (int64_t) counts.%s * (int64_t) sizeof( %s );\n", a.Field.Name, a.ElemName)
	}
	g.pf("    block.bytes = table_block_align( offset, %d );\n", ir.BlockAlign)
	g.pf("    return true;\n}\n\n")

	// BlockBytes
	g.pf("// The USED extent: the greatest offset_of + count * stride, rounded up to 64,\n")
	g.pf("// never less than the projection's own size (SPEC-TABLES.md §19.1). Because\n")
	g.pf("// the layout follows the counts it is proportional to the frame rather than\n")
	g.pf("// to the maxima. The tail — the bytes between the last row and the rounding —\n")
	g.pf("// is UNSPECIFIED: a caller that needs a byte-stable artifact zeroes the\n")
	g.pf("// storage once, because zeroing megabytes per frame is the cost this form\n")
	g.pf("// exists to avoid.\n")
	g.pf("inline int64_t %sBlockBytes( const %sBlock & block )\n{\n", name, name)
	g.pf("    int64_t used = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		g.pf("    {\n")
		g.pf("        int64_t end = (int64_t) block.projection->%s.offset_of + (int64_t) block.projection->%s.count * (int64_t) block.projection->%s.stride;\n",
			a.Field.Name, a.Field.Name, a.Field.Name)
		g.pf("        if ( end > used ) used = end;\n")
		g.pf("    }\n")
	}
	g.pf("    return table_block_align( used, %d );\n}\n\n", ir.BlockAlign)

	// the accessors
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

		g.pf("// %s, as a CONTIGUOUS view — available because the pitch IS sizeof (§2.7).\n", a.Field.Name)
		g.pf("// A block opened through BlockOpenCompatible may carry a LARGER pitch than\n")
		g.pf("// this build's row, and a cast would then garble every row after the first,\n")
		g.pf("// so the span is empty in that case and the rows accessor above is what a\n")
		g.pf("// tolerant consumer iterates.\n")
		g.pf("inline TableBlockSpan<%s> %s%sSpan( const %sBlock & block )\n{\n", a.ElemName, name, field, name)
		g.pf("    TableBlockSpan<%s> span;\n", a.ElemName)
		g.pf("    if ( block.projection->%s.stride != (uint32_t) sizeof( %s ) )\n", a.Field.Name, a.ElemName)
		g.pf("        return span;\n")
		g.pf("    span.rows = (%s *) ( block.base + block.projection->%s.offset_of );\n", a.ElemName, a.Field.Name)
		g.pf("    span.count = (int32_t) block.projection->%s.count;\n", a.Field.Name)
		g.pf("    return span;\n}\n\n")
	}
	g.pf("// ---- block fill path: end ----\n\n")
}

// emitBlockOpen emits the two entry points a consumer takes by name
// (SPEC-TABLES.md §19.2, §19.4).
func (g *tableGen) emitBlockOpen(bl *ir.BlockLayout) {
	g.pf("// BlockOpen checks once and points, and this is the WHOLE check (§19.2): the\n")
	g.pf("// magic read bytewise, the byte order it establishes, the layout id against\n")
	g.pf("// this build's own, the used extent against the bytes the caller passed, the\n")
	g.pf("// base's alignment, and each array's offset_of and extent inside the block. On\n")
	g.pf("// a match the bytes are what a build with this layout wrote, so there is\n")
	g.pf("// nothing to validate and nothing to fix up. On any failure it returns false\n")
	g.pf("// and points at nothing.\n")
	g.emitBlockOpenBody(bl, false)
	g.pf("// BlockOpenCompatible checks everything BlockOpen checks EXCEPT the layout id\n")
	g.pf("// (SPEC-TABLES.md §19.4), and then, per array it knows, this build's\n")
	g.pf("// sizeof( element ) <= the stride it READ from the instance. Never an\n")
	g.pf("// equality, and never against its own pitch constant: a producer whose rows\n")
	g.pf("// have grown writes a larger pitch, and it is precisely that case this entry\n")
	g.pf("// point exists to absorb. It drops the id check and NOTHING ELSE — there is\n")
	g.pf("// no silent bypass, and a caller either gets the layout id's guarantee from\n")
	g.pf("// BlockOpen or asks for the weaker one by name.\n")
	g.emitBlockOpenBody(bl, true)
}

func (g *tableGen) emitBlockOpenBody(bl *ir.BlockLayout, compatible bool) {
	name := bl.Table.Name
	fn := name + "BlockOpen"
	if compatible {
		fn = name + "BlockOpenCompatible"
	}
	g.pf("inline bool %s( %sBlock & block, void * base, int64_t bytes )\n{\n", fn, name)
	g.pf("    block.base = NULL;\n    block.projection = NULL;\n    block.bytes = 0;\n")
	g.pf("    if ( base == NULL || bytes < %d )\n        return false;\n", bl.Projection.Size)
	g.pf("    if ( ( (uintptr_t) base %% %d ) != 0 )\n        return false; // the base's alignment\n", ir.BlockAlign)
	g.pf("    const uint8_t * bytes_in = (const uint8_t *) base;\n")
	g.pf("    const uint64_t magic = table_block_read64( bytes_in );\n")
	g.pf("    if ( magic != TableBlockMagic )\n")
	g.pf("        return false; // not a block, or a foreign byte order (%s)\n", "table_block_byteswap64( magic ) == TableBlockMagic")
	if !compatible {
		g.pf("    const uint64_t layout_id = table_block_read64( bytes_in + 8 );\n")
		g.pf("    if ( layout_id != %sBlockLayoutId )\n        return false;\n", name)
	}
	g.pf("    %sBlock::Projection * projection = (%sBlock::Projection *) base;\n", name, name)
	g.pf("    int64_t used = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		alignment := ir.BlockAlign
		if a.ElemAlign() > int64(alignment) {
			alignment = int(a.ElemAlign())
		}
		g.pf("    {\n")
		g.pf("        const int64_t offset_of = (int64_t) projection->%s.offset_of;\n", a.Field.Name)
		g.pf("        const int64_t count = (int64_t) projection->%s.count;\n", a.Field.Name)
		g.pf("        const int64_t stride = (int64_t) projection->%s.stride;\n", a.Field.Name)
		if compatible {
			g.pf("        if ( (int64_t) sizeof( %s ) > stride )\n            return false; // this build's row is wider than the pitch the producer wrote\n", a.ElemName)
		} else {
			g.pf("        if ( stride != (int64_t) sizeof( %s ) )\n            return false;\n", a.ElemName)
		}
		g.pf("        if ( offset_of < %d || ( offset_of %% %d ) != 0 )\n            return false;\n", bl.Projection.Size, alignment)
		g.pf("        const int64_t end = offset_of + count * stride;\n")
		g.pf("        if ( count < 0 || end < offset_of || end > bytes )\n            return false;\n")
		g.pf("        if ( end > used ) used = end;\n")
		g.pf("    }\n")
	}
	g.pf("    used = table_block_align( used, %d );\n", ir.BlockAlign)
	g.pf("    if ( used > bytes )\n        return false;\n")
	g.pf("    block.base = (uint8_t *) base;\n")
	g.pf("    block.projection = projection;\n")
	g.pf("    block.bytes = used;\n")
	g.pf("    return true;\n}\n\n")
}

// blockColumns renders one closure member's BLOCK descriptor columns
// (SPEC-TABLES.md §8): the table's own `block` flag, and the projection's size
// beside it so a walker over a block can bound the record it is reading. The
// columns exist in every unit, whatever its mode — they describe the LANGUAGE.
func (g *tableGen) blockColumns(st *ir.Struct) string {
	if bl := g.blocks.Block(st.Name); bl != nil {
		return fmt.Sprintf(", true, %d", bl.Projection.Size)
	}
	return ", false, 0"
}

// blockFieldColumns renders one field's BLOCK descriptor columns: its
// PROJECTION offset, and — for an out-of-line array — the offsets of the three
// members inside its triple (SPEC-TABLES.md §8). Every field of a block-form
// table carries a projection offset, because the projection is a different
// struct from the by-value one and a walker over a block needs the positions
// that struct actually has.
func (g *tableGen) blockFieldColumns(st *ir.Struct, f *ir.Field) string {
	bl := g.blocks.Block(st.Name)
	if bl == nil {
		return ", 0xffffffffu, 0xffffffffu, 0xffffffffu, 0xffffffffu"
	}
	fl := bl.Projection.FieldByName(f.Name)
	if fl == nil {
		return ", 0xffffffffu, 0xffffffffu, 0xffffffffu, 0xffffffffu"
	}
	if a := bl.ArrayByName(f.Name); a != nil {
		return fmt.Sprintf(", %du, %du, %du, %du", fl.Offset, a.OffsetOfOffset, a.CountOffset, a.StrideOffset)
	}
	return fmt.Sprintf(", %du, 0xffffffffu, 0xffffffffu, 0xffffffffu", fl.Offset)
}
