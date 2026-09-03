// The BLOCK FORM in Java (docs/SPEC-TABLES.md §19): the READ half, emitted ON THE
// SIDE into <Table>Block.java.
//
// NOTHING DECLARES IT. Every fixed table has a block form; a consumer compiles
// this file only if it uses one, and <Base>Table.java carries not one symbol of
// it. The C++ side is the producer (§19.1's builder) and this side is the
// consumer: it reads bytes another language wrote, in place, out of the array
// the caller owns.
//
// Two ways to read one block, and both come from one declaration (§19.2): the
// DESCRIPTORS, which carry the projection's own layout and retire a hand-kept
// mirror, and the generated ACCESSORS beside them, which are the typed fast
// path a per-frame job uses. A consumer picks by what it is doing — reflection
// to walk anything, the accessors to read one thing fast.
//
// THE LAYOUT MODEL IS NAMED (§19.3), and Java's half of the contract is not the
// same half. C++ and C# each have a runtime layout of their own that can
// DISAGREE with the compiler's model, and they static_assert or throw on the
// disagreement. Java has no record layout at all — the offsets are constants
// this emitter wrote — so there is no second model to check against, and saying
// otherwise would be theatre. What TableBlockLayout does check is real and is
// the only disagreement this language can have: the ACCESSORS' offsets against
// the DESCRIPTORS' offsets, two derivations the emitter makes separately
// (ir.BlockFieldPieceOffsets and ir.BlockFieldOf), plus each array's pitch
// constant against the row size it must equal. What refuses a FOREIGN block is
// Open, which checks every number the instance carries against this build's.
//
// THE BYTE ORDER IS SETTLED BY THE READER, not by the host: every read here is
// explicitly little-endian, so a block written by a big-endian producer is
// refused twice — its magic reads back byte-swapped, and its order word is not
// this reader's.
//
// ALLOCATION: none of it. The bytes belong to the consumer — a byte[] it read,
// mapped or received — and this side takes the array and an offset and reads.
package javatable

import (
	"fmt"
	"maps"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockRuntimeFiles is the shared block runtime, one file per public type.
func blockRuntimeFiles(u *ir.Unit) map[string][]byte {
	return map[string][]byte{
		"TableBlockRows.java": javaFile(u, "TableBlockRows",
			"one out-of-line array's rows, at the pitch the instance gives (docs/SPEC-TABLES.md §19.2).", tableBlockRowsSource),
		"TableBlockInfo.java": javaFile(u, "TableBlockInfo",
			"a block record's reflection descriptor (docs/SPEC-TABLES.md §8, §19.2).", tableBlockInfoSource()),
		"TableBlockFieldInfo.java": javaFile(u, "TableBlockFieldInfo",
			"a block field's reflection descriptor (docs/SPEC-TABLES.md §8.1, §19.2).", tableBlockFieldInfoSource),
	}
}

// tableBytesFile is the little-endian byte access every accelerator reads
// through — the one primitive Java needs and C++ gets from its type system.
func tableBytesFile(u *ir.Unit) []byte {
	return javaFile(u, "TableBytes", "explicit little-endian reads out of a byte[] (docs/SPEC-TABLES.md §7, §19).", tableBytesSource)
}

// buildVersionFile is the unit's BUILD VERSION (docs/SPEC-TABLES.md §20).
func buildVersionFile(u *ir.Unit) []byte {
	var b strings.Builder
	b.WriteString("// THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact the\n")
	b.WriteString("// bytes this build produces depend on — the type wire's protocol id, every\n")
	b.WriteString("// table's layout keyed by wire id, every table's meaning (defaults, ranges,\n")
	b.WriteString("// enum and union vocabularies, keyed the same way), and the build's byte order.\n")
	b.WriteString("// It is the number a block and a cook carry and the number Open compares.\n")
	b.WriteString("//\n")
	b.WriteString("// There are TWO ids in the design and they are not interchangeable: the\n")
	b.WriteString("// PROTOCOL ID is the type wire's and nothing else, and the BUILD VERSION is\n")
	b.WriteString("// what everything cooked or blocked is keyed by. A table edit moves this and\n")
	b.WriteString("// never the protocol id; a type edit moves both.\n")
	b.WriteString("public final class BuildVersion {\n")
	b.WriteString("    private BuildVersion() {}\n\n")
	fmt.Fprintf(&b, "    public static final long value = 0x%016xL;\n", ir.BuildVersion(u))
	b.WriteString("}\n")
	return javaFile(u, "BuildVersion", "the unit's build version (docs/SPEC-TABLES.md §20).", b.String())
}

const tableBytesSource = `// Explicit little-endian reads out of a byte[]. Every multi-byte read a block
// or a cook makes goes through here, so the BYTE ORDER of the two accelerators
// is this reader's own and never the host's — which is what lets a foreign-order
// file be refused by its magic rather than silently misread.
//
// Nothing here bounds-checks: the callers do, and the array's own bounds are
// the floor under them — an index past the end is an ArrayIndexOutOfBounds,
// which is why every reader above checks BEFORE it reads rather than catching
// afterwards.
public final class TableBytes {
    private TableBytes() {}

    public static boolean bool(byte[] data, int at) { return data[at] != 0; }

    public static byte i8(byte[] data, int at) { return data[at]; }

    public static int u8(byte[] data, int at) { return data[at] & 0xff; }

    public static short i16(byte[] data, int at) {
        return (short) ((data[at] & 0xff) | ((data[at + 1] & 0xff) << 8));
    }

    public static int u16(byte[] data, int at) { return i16(data, at) & 0xffff; }

    public static int i32(byte[] data, int at) {
        return (data[at] & 0xff) | ((data[at + 1] & 0xff) << 8) |
               ((data[at + 2] & 0xff) << 16) | ((data[at + 3] & 0xff) << 24);
    }

    public static long u32(byte[] data, int at) { return i32(data, at) & 0xffffffffL; }

    public static long i64(byte[] data, int at) {
        return (i32(data, at) & 0xffffffffL) | ((long) i32(data, at + 4) << 32);
    }

    public static float f32(byte[] data, int at) { return Float.intBitsToFloat(i32(data, at)); }

    public static double f64(byte[] data, int at) { return Double.longBitsToDouble(i64(data, at)); }
}
`

const tableBlockRowsSource = `// One out-of-line array's rows, at the pitch the INSTANCE gives
// (docs/SPEC-TABLES.md §19.2). A call site never spells the pitch arithmetic
// itself, for the same reason a keyed array's call sites should not re-derive
// their own slot rule: the idiom written at every call site is the one written
// wrong somewhere.
//
// A row is an OFFSET into the block's own array — Java has no pointer and no
// row struct — so reading one is <Name>Row's accessors at rows.at(i).
public final class TableBlockRows {
    public final byte[] data;
    public final int base;    // the first row's offset in data
    public final int count;   // rows the producer filled
    public final int stride;  // the pitch the consumer indexes with, FROM THE DATA

    public TableBlockRows(byte[] data, int base, int count, int stride) {
        this.data = data;
        this.base = base;
        this.count = count;
        this.stride = stride;
    }

    /** the offset of the row at index, at the pitch the instance gave. */
    public int at(int index) {
        if (index < 0 || index >= count) {
            throw new IndexOutOfBoundsException("row " + index + " of " + count);
        }
        return base + index * stride;
    }
}
`

// tableBlockInfoSource is the block record's descriptor, and it takes the
// prologue's own constants from ir rather than repeating them: a magic with two
// spellings is a magic that can disagree with itself.
func tableBlockInfoSource() string {
	return `// One record's layout as DATA — the whole mechanism behind the block form's
// read side, and what retires a hand-kept mirror. A block-form table's own
// descriptor describes its PROJECTION; the element descriptor of each
// out-of-line array describes that array's ROW, and so on down.
public final class TableBlockInfo {
    /** The block's magic (docs/SPEC-TABLES.md §19.1), read BYTEWISE: it is the one
     *  field read without assuming the order the rest of the block is in. */
    public static final long magic = ` + fmt.Sprintf("0x%016xL", ir.BlockMagic) + `;

    /** THIS READER's byte order, as the prologue carries it (§20.3). It is a
     *  CONSTANT here and not the host's: every read goes through TableBytes,
     *  which is explicitly little-endian, so a block written by a build of the
     *  other order is refused — by its magic, which reads back byte-swapped, and
     *  by this word. A big-endian fix-up path is a named obligation, not
     *  something a consumer improvises row by row. */
    public static final long byteOrder = ` + fmt.Sprintf("%dL", ir.BlockByteOrderLittle) + `;

    /** every block base and every out-of-line array start is 64-aligned (§19.1). */
    public static final int alignment = ` + fmt.Sprintf("%d", ir.BlockAlign) + `;

    public String name;
    public long buildVersion; // the unit's (docs/SPEC-TABLES.md §20)
    public int size;          // the record's own size: a projection's, or a row's
    public int align;
    public int numFields;
    public TableBlockFieldInfo[] fields;
}
`
}

const tableBlockFieldInfoSource = `// One block field's reflection descriptor. The descriptors are the mechanism,
// and they are what retires a hand-kept mirror: a consumer holding them reads
// the triples out of an instance and reaches rows, with no hand-written
// accessor per table and no knowledge of the spelling that produced any of it.
public final class TableBlockFieldInfo {
    /** the ELEMENT's or the nested record's own layout, behind a supplier so the
     *  table stays constructible in any order. null when the field is a scalar.
     *  Following it is how a walker DESCENDS: an out-of-line array's rows, and a
     *  nested record's fields, are both reached through this one column. */
    public interface InfoRef { TableBlockInfo get(); }

    public String name;
    public int offset;         // the field's offset in the record this descriptor describes
    public int size;           // its size there
    public int kind;           // the table-wire kind, as TableFieldInfo carries it
    public boolean outOfLine;  // an out-of-line array: the triple's three members are live
    public int offsetOfOffset; // the triple's offset_of member, or -1
    // The COUNT COMPANION, and it is one column doing one job in both spellings:
    // the triple's count member for an out-of-line array, the int32 used length
    // of a string or a bytes inline, -1 when the field has none.
    public int countOffset;
    public int strideOffset;   // the triple's stride member, or -1
    public int stride;         // THIS BUILD's pitch, to assert against — never to index with (§19.2)
    // ---- what a GENERIC ROW WALK needs, in the vocabulary TableFieldInfo
    // already uses (docs/SPEC-TABLES.md §8.1), so ONE walker reads a cooked node and
    // a block row without learning a second one.
    public boolean isArray;    // inline storage of arrayBound slots at elemSize (bytes included)
    public boolean counted;    // countOffset names a used-length companion
    public boolean optional;   // presentOffset names a bool presence companion
    public int arrayBound;     // inline slots, or a string's declared maximum; 0 for a plain scalar
    public int elemSize;       // ONE slot's size; the field's own when it holds one value
    public int presentOffset;  // the presence companion, or -1
    public InfoRef elementRef;

    public TableBlockInfo element() { return elementRef == null ? null : elementRef.get(); }

    /** one INLINE field. */
    public static TableBlockFieldInfo of(String name, int offset, int size, int kind, int countOffset,
                                         boolean isArray, boolean counted, boolean optional,
                                         int arrayBound, int elemSize, int presentOffset, InfoRef element) {
        TableBlockFieldInfo f = new TableBlockFieldInfo();
        f.name = name;
        f.offset = offset;
        f.size = size;
        f.kind = kind;
        f.outOfLine = false;
        f.offsetOfOffset = -1;
        f.countOffset = countOffset;
        f.strideOffset = -1;
        f.stride = 0;
        f.isArray = isArray;
        f.counted = counted;
        f.optional = optional;
        f.arrayBound = arrayBound;
        f.elemSize = elemSize;
        f.presentOffset = presentOffset;
        f.elementRef = element;
        return f;
    }

    /** one OUT-OF-LINE array: its triple, and the row it names. */
    public static TableBlockFieldInfo array(String name, int offset, int size, int kind,
                                            int offsetOfOffset, int countOffset, int strideOffset,
                                            int stride, int max, InfoRef element) {
        TableBlockFieldInfo f = new TableBlockFieldInfo();
        f.name = name;
        f.offset = offset;
        f.size = size;
        f.kind = kind;
        f.outOfLine = true;
        f.offsetOfOffset = offsetOfOffset;
        f.countOffset = countOffset;
        f.strideOffset = strideOffset;
        f.stride = stride;
        f.isArray = true;
        f.counted = true;
        f.optional = false;
        f.arrayBound = max;
        f.elemSize = 0;
        f.presentOffset = -1;
        f.elementRef = element;
        return f;
    }
}
`

// anyBlockForm reports whether the unit has a block form at all.
func anyBlockForm(u *ir.Unit, blocks *ir.BlockUnit) bool {
	if blocks == nil {
		return false
	}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if blocks.Block(st.Name) != nil {
				return true
			}
		}
	}
	return false
}

// generateBlockFiles emits <Table>Block.java for every table with a block form,
// plus the shared block runtime.
func generateBlockFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if !anyBlockForm(u, blocks) {
		return out, nil
	}
	maps.Copy(out, blockRuntimeFiles(u))
	for _, bl := range blocks.Tables {
		g := &blockGen{unit: u, blocks: blocks, bl: bl}
		g.emit()
		out[bl.Table.Name+"Block.java"] = javaFile(u, bl.Table.Name+"Block",
			"the BLOCK FORM's read half (docs/SPEC-TABLES.md §19).", g.b.String())
	}
	return out, nil
}

type blockGen struct {
	unit   *ir.Unit
	blocks *ir.BlockUnit
	bl     *ir.BlockLayout
	b      strings.Builder
}

func (g *blockGen) f(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

func (g *blockGen) emit() {
	bl := g.bl
	name := bl.Table.Name
	g.f("// %s's block: an array and an offset, and then rows in place. Opening one\n", name)
	g.f("// is ONE check and no copy; reading a row is one add (docs/SPEC-TABLES.md §19.2).\n")
	g.f("//\n")
	g.f("// The bytes belong to the CONSUMER — an array it read, mapped or received.\n")
	g.f("// Nothing here allocates beyond the handle itself.\n")
	g.f("//\n")
	g.f("// THE BASE'S ALIGNMENT is its OFFSET's: C++ and C# measure the address a\n")
	g.f("// caller holds, and a Java caller holds an array and an index, so the residue\n")
	g.f("// that decides the check is the index's. The arithmetic is the same, so the\n")
	g.f("// refusals are.\n")
	g.f("public final class %sBlock {\n", name)
	g.f("    private final byte[] data;\n")
	g.f("    private final int base;\n")
	g.f("    private final long bytes;\n\n")
	g.f("    private %sBlock(byte[] data, int base, long bytes) {\n", name)
	g.f("        this.data = data;\n        this.base = base;\n        this.bytes = bytes;\n    }\n\n")
	g.f("    // The storage a PRODUCER of this block allocates, sized from the declared\n")
	g.f("    // maxima (docs/SPEC-TABLES.md §19.1). A Java consumer does not allocate a block —\n")
	g.f("    // the bytes are handed to it — but it caps by this: a playback buffer, a\n")
	g.f("    // recording, a scratch copy all size from the generated constant rather\n")
	g.f("    // than from a number a person wrote down beside it.\n")
	g.f("    public static final long blockMaxBytes = %dL;\n\n", bl.MaxBytes)
	g.f("    /** the PROJECTION's own size, prologue included, and the offsets this\n")
	g.f("     *  class's accessors read at (§19.3). */\n")
	g.f("    public static final int projectionSize = %d;\n", bl.Projection.Size)
	g.f("    static final int[] projectionOffsets = {")
	for i, fl := range bl.Projection.Fields {
		if i > 0 {
			g.f(",")
		}
		g.f(" %d", fl.Offset)
	}
	g.f(" };\n\n")
	g.f("    public byte[] data() { return data; }\n")
	g.f("    public int base() { return base; }\n")
	g.f("    public long bytes() { return bytes; }\n\n")
	g.f("    // the generated PROLOGUE (§19.1), read where it lies\n")
	g.f("    public static long magic(byte[] data, int at) { return TableBytes.i64(data, at); }\n")
	g.f("    public static long buildVersion(byte[] data, int at) { return TableBytes.i64(data, at + 8); }\n")
	g.f("    public static long byteOrder(byte[] data, int at) { return TableBytes.i64(data, at + 16); }\n\n")
	g.emitProjectionAccessors()
	g.emitArrays()
	g.emitOpen()
	g.emitType()
	g.f("}\n")
}

// emitProjectionAccessors spells the table's own inline fields where they lie
// in the projection.
func (g *blockGen) emitProjectionAccessors() {
	for _, fl := range g.bl.Projection.Fields {
		f := fl.Field
		if ir.BlockOutOfLine(f) {
			continue // the triple's accessor is the rows() below
		}
		name := member(f)
		pieces := ir.BlockFieldPieceOffsets(g.unit, f, fl.Offset, true)
		switch {
		case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
			g.f("    public static int %sAt(int at) { return at + %d; }\n", name, pieces[0].Offset)
			g.f("    public static int %sLength(byte[] data, int at) {\n", name)
			g.f("        int used = TableBytes.i32(data, at + %d);\n", pieces[1].Offset)
			g.f("        return used < 0 || used > %d ? 0 : used;\n", f.Type.Size)
			g.f("    }\n\n")
		case f.KeyEnum != "" || f.Array != ir.ArrayNone:
			elem := javaRecordType(f.Type)
			elemSize := pieces[0].Size / f.ArrayBound
			if isClassRef(f.Type) {
				g.f("    public static int %sAt(int at, int i) { return at + %d + i * %d; }\n\n", name, pieces[0].Offset, elemSize)
			} else {
				g.f("    public static %s %s(byte[] data, int at, int i) { return %s; }\n\n",
					elem, name, readCall(elem, fmt.Sprintf("at + %d + i * %d", pieces[0].Offset, elemSize)))
			}
			if f.Array == ir.ArrayCounted {
				g.f("    public static int %sCount(byte[] data, int at) { return TableBytes.i32(data, at + %d); }\n\n",
					name, pieces[1].Offset)
			}
		case isClassRef(f.Type):
			g.f("    public static int %sAt(int at) { return at + %d; }\n\n", name, pieces[0].Offset)
		default:
			typ := javaRecordType(f.Type)
			g.f("    public static %s %s(byte[] data, int at) { return %s; }\n\n",
				typ, name, readCall(typ, fmt.Sprintf("at + %d", pieces[0].Offset)))
		}
		if f.Type.Optional {
			g.f("    public static boolean %sPresent(byte[] data, int at) { return TableBytes.bool(data, at + %d); }\n\n",
				name, pieces[len(pieces)-1].Offset)
		}
	}
}

func (g *blockGen) emitArrays() {
	for _, a := range g.bl.Arrays {
		field := member(a.Field)
		g.f("    // %s: the constants this build asserts against. A consumer INDEXES with\n", a.Field.Name)
		g.f("    // what it read from the instance, never with these (docs/SPEC-TABLES.md §19.2).\n")
		g.f("    public static final long %sStride = %dL;\n", field, a.Stride)
		g.f("    public static final long %sMax = %dL;\n", field, a.Max)
		g.f("    public static final long %sProjectionOffset = %dL;\n\n", field, a.TripleOffset)
		g.f("    // ITERATED, not indexed by hand: the accessor answers each row's OFFSET,\n")
		g.f("    // at the pitch the INSTANCE gives, for count rows — read one with\n")
		g.f("    // %sRow's accessors.\n", a.ElemName)
		g.f("    public TableBlockRows %s() {\n", field)
		g.f("        long offsetOf = TableBytes.i64(data, base + %d);\n", a.OffsetOfOffset)
		g.f("        int count = TableBytes.i32(data, base + %d);\n", a.CountOffset)
		g.f("        int stride = TableBytes.i32(data, base + %d);\n", a.StrideOffset)
		g.f("        return new TableBlockRows(data, base + (int) offsetOf, count, stride);\n")
		g.f("    }\n\n")
	}
}

func (g *blockGen) emitOpen() {
	bl := g.bl
	name := bl.Table.Name
	g.f("    // Open checks once and points, and this is the WHOLE check (docs/SPEC-TABLES.md\n")
	g.f("    // §19.2): the magic, the BYTE ORDER the prologue carries against this\n")
	g.f("    // reader's own, the BUILD VERSION against this build's own, each array's\n")
	g.f("    // pitch, its offset_of, its COUNT against the declared maximum and its\n")
	g.f("    // extent inside the block, the used extent against the bytes the caller\n")
	g.f("    // passed, and the base's alignment. On a match the bytes are what a build\n")
	g.f("    // with this layout wrote, so there is nothing to validate and nothing to\n")
	g.f("    // fix up. On any failure it answers null and points at nothing.\n")
	g.f("    //\n")
	g.f("    // There is ONE entry point and no tolerant twin: the block form is\n")
	g.f("    // same-build by construction — both sides generated from one declaration\n")
	g.f("    // at one build — so a consumer older than its producer is not a case. A\n")
	g.f("    // mismatch is a refusal; regenerate both sides. Data that must outlive the\n")
	g.f("    // build that wrote it takes the wire, which this same table still has.\n")
	g.f("    //\n")
	g.f("    // EVERY NUMBER THE INSTANCE CARRIES IS COMPARED UNSIGNED and each term is\n")
	g.f("    // BOUNDED BEFORE IT IS ADDED. A forged offset_of near 2^63 must refuse, and\n")
	g.f("    // an addition that wrapped past the top of the type would be what the check\n")
	g.f("    // after it was supposed to catch. The C++ side holds the same shape for the\n")
	g.f("    // same reason.\n")
	g.f("    public static %sBlock open(byte[] data, int offset, long bytes) {\n", name)
	g.f("        TableBlockLayout.verify();\n")
	g.f("        if (data == null || offset < 0 || bytes < %d) { return null; }\n", bl.Projection.Size)
	g.f("        // the caller's CLAIM must be storage the caller actually has: a claim\n")
	g.f("        // past the array is a caller-side error, and refusing is the only answer\n")
	g.f("        // that cannot read memory it was not given\n")
	g.f("        if (bytes > data.length - offset) { return null; }\n")
	g.f("        if ((offset %% TableBlockInfo.alignment) != 0) { return null; } // the base's alignment\n")
	g.f("        if (TableBytes.i64(data, offset) != TableBlockInfo.magic) { return null; }\n")
	g.f("        if (TableBytes.i64(data, offset + 8) != BuildVersion.value) { return null; }\n")
	g.f("        if (TableBytes.i64(data, offset + 16) != TableBlockInfo.byteOrder) { return null; }\n")
	g.f("        long used = %d;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		field := member(a.Field)
		alignment := int64(ir.BlockAlign)
		if a.ElemAlign() > alignment {
			alignment = a.ElemAlign()
		}
		g.f("        {\n")
		g.f("            long offsetOf = TableBytes.i64(data, offset + %d);\n", a.OffsetOfOffset)
		g.f("            long count = TableBytes.u32(data, offset + %d);\n", a.CountOffset)
		g.f("            long stride = TableBytes.u32(data, offset + %d);\n", a.StrideOffset)
		g.f("            if (stride != %sStride) { return null; }\n", field)
		g.f("            // past the DECLARED MAXIMUM: Begin refuses this on the producer\n")
		g.f("            // side and Open refuses it here, because a consumer that sizes\n")
		g.f("            // anything by the maximum would overflow on a count the maximum\n")
		g.f("            // does not bound\n")
		g.f("            if (count > %sMax) { return null; }\n", field)
		g.f("            if (Long.compareUnsigned(offsetOf, %d) < 0 || (offsetOf & %d) != 0) { return null; }\n",
			bl.Projection.Size, alignment-1)
		g.f("            if (Long.compareUnsigned(offsetOf, bytes) > 0) { return null; }\n")
		g.f("            long rows = count * stride; // both bounded above: this cannot carry\n")
		g.f("            if (rows > bytes - offsetOf) { return null; }\n")
		g.f("            long end = offsetOf + rows;\n")
		g.f("            if (end > used) { used = end; }\n")
		g.f("        }\n")
	}
	g.f("        // the used extent, rounded to %d WITHOUT the rounding itself wrapping:\n", ir.BlockAlign)
	g.f("        // used is already inside bytes, and the padding is paid out of the slack\n")
	g.f("        // that is left rather than added and compared after.\n")
	g.f("        long padding = (%d - (used %% %d)) %% %d;\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	g.f("        if (padding > bytes - used) { return null; }\n")
	g.f("        used += padding;\n")
	g.f("        return new %sBlock(data, offset, used);\n    }\n\n", name)
	g.f("    /** the whole array as the block, which is how a consumer that read a file\n")
	g.f("     *  spells it. */\n")
	g.f("    public static %sBlock open(byte[] data) { return open(data, 0, data == null ? 0 : data.length); }\n\n", name)
}

// emitType is the REFLECTIVE half (docs/SPEC-TABLES.md §8, §19.2): the projection
// offset of every field, the offsets of the three members inside each triple,
// and the element's own descriptor beside them. A consumer holding these reads
// the facts out of an instance and reaches rows, with no hand-written accessor
// per table and nothing to maintain when a field is added.
func (g *blockGen) emitType() {
	bl := g.bl
	name := bl.Table.Name
	rg := &rowGen{unit: g.unit}
	g.f("    // this table's block descriptors: constant data, so a reflective read costs\n")
	g.f("    // a lookup and not a parse. The row layouts hang off the element column of\n")
	g.f("    // each field, so a walker reaches every record through the graph.\n")
	g.f("    private static TableBlockInfo projection;\n\n")
	g.f("    public static TableBlockInfo type() {\n")
	g.f("        TableBlockInfo info = projection;\n")
	g.f("        if (info != null) { return info; }\n")
	g.f("        info = new TableBlockInfo();\n")
	g.f("        info.name = %q; info.buildVersion = BuildVersion.value; info.size = %d; info.align = %d; info.numFields = %d;\n",
		name, bl.Projection.Size, bl.Projection.Align, len(bl.Projection.Fields))
	g.f("        TableBlockFieldInfo[] fields = new TableBlockFieldInfo[%d];\n", len(bl.Projection.Fields))
	for i, fl := range bl.Projection.Fields {
		if a := bl.ArrayByName(fl.Field.Name); a != nil {
			g.f("        fields[%d] = %s;\n", i, rg.blockFieldInfo(fl, true, a))
			continue
		}
		g.f("        fields[%d] = %s;\n", i, rg.blockFieldInfo(fl, true, nil))
	}
	g.f("        info.fields = fields;\n")
	g.f("        projection = info;\n")
	g.f("        return info;\n    }\n")
}

// emitBlockLayoutFile is §19.3's Java half, and it says exactly what Java can
// disagree about (see this file's header): the ACCESSORS' offsets against the
// DESCRIPTORS', and each array's pitch constant against the row size it must
// equal. Run once, before any block or cook is opened, and it THROWS naming the
// record, the field, and the two numbers.
func emitBlockLayoutFile(u *ir.Unit, blocks *ir.BlockUnit, set *records) []byte {
	var b strings.Builder
	b.WriteString("// THE LAYOUT CONTRACT's Java half (docs/SPEC-TABLES.md §19.3), run ONCE.\n")
	b.WriteString("//\n")
	b.WriteString("// C++ and C# each have a RUNTIME layout that can disagree with the compiler's\n")
	b.WriteString("// model, and they static_assert or throw on the disagreement. Java has no\n")
	b.WriteString("// record layout at all — the offsets are constants the generator wrote — so\n")
	b.WriteString("// there is no second model to check against, and a check that pretended\n")
	b.WriteString("// otherwise would be theatre.\n")
	b.WriteString("//\n")
	b.WriteString("// What IS checked is the one disagreement this language can have, and it is a\n")
	b.WriteString("// real one: the ACCESSORS' offsets against the DESCRIPTORS' offsets. The two\n")
	b.WriteString("// come from separate derivations in the generator, and a walker that read a\n")
	b.WriteString("// row through the descriptors while a consumer read it through the accessors\n")
	b.WriteString("// would otherwise be reading two different records. Each array's pitch\n")
	b.WriteString("// constant is checked against the row size it must equal for the same reason.\n")
	b.WriteString("//\n")
	b.WriteString("// What refuses a FOREIGN block is <Table>Block.open, which checks every number\n")
	b.WriteString("// the instance carries against this build's.\n")
	b.WriteString("public final class TableBlockLayout {\n")
	b.WriteString("    private TableBlockLayout() {}\n\n")
	b.WriteString("    private static boolean checked;\n\n")
	b.WriteString("    public static void verify() {\n")
	b.WriteString("        if (checked) { return; }\n")
	b.WriteString("        checked = true;\n")
	for _, name := range set.order {
		if !set.inBlock[name] {
			continue
		}
		fmt.Fprintf(&b, "        record(\"%sRow\", %sRow.size, %sRow.align, %sRow.offsets, %sRow.blockInfo());\n",
			name, name, name, name, name)
	}
	for _, bl := range blocks.Tables {
		t := bl.Table.Name
		fmt.Fprintf(&b, "        record(\"%sBlock projection\", %sBlock.projectionSize, %d, %sBlock.projectionOffsets, %sBlock.type());\n",
			t, t, bl.Projection.Align, t, t)
		for _, a := range bl.Arrays {
			fmt.Fprintf(&b, "        number(\"%sBlock.%sStride\", (int) %sBlock.%sStride, %sRow.size);\n",
				t, member(a.Field), t, member(a.Field), a.ElemName)
		}
	}
	b.WriteString("    }\n\n")
	b.WriteString(`    private static void record(String what, int size, int align, int[] offsets, TableBlockInfo info) {
        number(what + " size", size, info.size);
        number(what + " align", align, info.align);
        number(what + " field count", offsets.length, info.numFields);
        for (int i = 0; i < offsets.length; i++) {
            number(what + "." + info.fields[i].name, offsets[i], info.fields[i].offset);
        }
    }

    private static void number(String what, int accessors, int descriptors) {
        if (accessors != descriptors) {
            throw new IllegalStateException(
                    "schema block layout: " + what + " is " + accessors + " through the accessors and " +
                    descriptors + " through the descriptors — the two halves of one generated layout " +
                    "disagree about the bytes (docs/SPEC-TABLES.md §19.3)");
        }
    }
}
`)
	return javaFile(u, "TableBlockLayout", "the block form's layout contract, run once (docs/SPEC-TABLES.md §19.3).", b.String())
}
