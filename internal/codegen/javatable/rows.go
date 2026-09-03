// The RECORD accessors the two accelerators share (docs/SPEC-TABLES.md §7, §19).
//
// C++ and C# spell a block row and a cooked record as a STRUCT and point at it;
// Java has no struct and no pointer, so a record is spelled as a class of
// STATIC ACCESSORS over `(byte[] data, int at)` — one read at one offset,
// nothing copied and nothing allocated. That is the whole of the divergence:
// the offsets are the same layout model's, computed once in ir and never
// re-derived here, so a Java consumer reads the same bytes at the same places
// as the C++ producer wrote them.
//
// A cooked record IS the blittable row (§7), so one class serves both: a record
// the block form reaches carries a TableBlockInfo, a record a cook reaches
// carries a TableCookInfo, and a record in both carries both — over ONE set of
// accessors and ONE set of offset constants, which is what makes the layout
// check below a real check rather than a restatement.
package javatable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// records is every record the two accelerators touch, with the layout each one
// has and which accelerator reaches it.
type records struct {
	order   []string
	layout  map[string]*ir.MemberLayout
	inBlock map[string]bool
	inCook  map[string]bool
}

func collectRecords(u *ir.Unit, blocks *ir.BlockUnit, ck *cookUnit) *records {
	r := &records{
		layout:  map[string]*ir.MemberLayout{},
		inBlock: map[string]bool{},
		inCook:  map[string]bool{},
	}
	if blocks != nil {
		for _, name := range blocks.Order {
			if ml := blocks.Layout(name); ml != nil {
				r.layout[name] = ml
				r.inBlock[name] = true
			}
		}
	}
	if ck != nil {
		for _, name := range ck.order {
			if ml := ck.members[name]; ml != nil {
				if _, seen := r.layout[name]; !seen {
					r.layout[name] = ml
				}
				r.inCook[name] = true
			}
		}
	}
	for name := range r.layout {
		r.order = append(r.order, name)
	}
	sort.Strings(r.order)
	_ = u
	return r
}

// rowGen emits one <Name>Row.java.
type rowGen struct {
	unit *ir.Unit
	set  *records
	b    strings.Builder
}

func (g *rowGen) f(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

// javaRecordType is the Java spelling one record field's accessor answers in:
// the packet emitter's same-width signed type, bit-transparent, so a block row
// and a table's storage read the same way.
func javaRecordType(t ir.FieldType) string {
	if t.Pointer {
		return "long"
	}
	switch t.Kind {
	case ir.TBool:
		return "boolean"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TInt:
		return intJavaType(t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "int"
		}
		return "long"
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			return enumJavaType(ref.StorageBits)
		case *ir.Flags:
			return "long"
		}
	}
	return "byte"
}

// readCall is the TableBytes read one Java type takes.
func readCall(typ, at string) string {
	switch typ {
	case "boolean":
		return "TableBytes.bool(data, " + at + ")"
	case "byte":
		return "TableBytes.i8(data, " + at + ")"
	case "short":
		return "TableBytes.i16(data, " + at + ")"
	case "int":
		return "TableBytes.i32(data, " + at + ")"
	case "long":
		return "TableBytes.i64(data, " + at + ")"
	case "float":
		return "TableBytes.f32(data, " + at + ")"
	case "double":
		return "TableBytes.f64(data, " + at + ")"
	}
	return "TableBytes.i8(data, " + at + ")"
}

// emitRowFiles emits one <Name>Row.java per record either accelerator reaches.
func emitRowFiles(u *ir.Unit, set *records, blocks *ir.BlockUnit, ck *cookUnit) map[string][]byte {
	out := map[string][]byte{}
	for _, name := range set.order {
		g := &rowGen{unit: u, set: set}
		g.emitRow(name, set.layout[name], blocks, ck)
		out[name+"Row.java"] = javaFile(u, name+"Row",
			"one record of the two accelerators, read where it lies (docs/SPEC-TABLES.md §7, §19).", g.b.String())
	}
	return out
}

func (g *rowGen) emitRow(name string, ml *ir.MemberLayout, blocks *ir.BlockUnit, ck *cookUnit) {
	g.f("// %s — a block row, a cooked record, or a record one of them nests by\n", name)
	g.f("// value. `Row` is a CLAIMED suffix (docs/SPEC-TABLES.md §11), so no declaration\n")
	g.f("// in the unit can take it.\n")
	g.f("//\n")
	g.f("// The accessors are STATIC over (byte[] data, int at): `at` is where this\n")
	g.f("// record starts in the caller's array, and every read is one offset from it.\n")
	g.f("// Nothing here copies and nothing here allocates.\n")
	g.f("public final class %sRow {\n", name)
	g.f("    private %sRow() {}\n\n", name)
	g.f("    /** the record's own size and alignment, from the ONE layout model (§19.3). */\n")
	g.f("    public static final int size = %d;\n", ml.Size)
	g.f("    public static final int align = %d;\n\n", ml.Align)
	g.f("    /** where each field starts, in declaration order — the accessors' own\n")
	g.f("     *  constants, which TableBlockLayout / TableCookLayout check against the\n")
	g.f("     *  descriptors below (§19.3). */\n")
	g.f("    static final int[] offsets = {")
	for i, fl := range ml.Fields {
		if i > 0 {
			g.f(",")
		}
		g.f(" %d", fl.Offset)
	}
	g.f(" };\n\n")
	for _, fl := range ml.Fields {
		g.emitRowField(fl)
	}
	if g.set.inBlock[name] {
		g.emitBlockRecordInfo(name, ml)
	}
	if g.set.inCook[name] {
		g.emitCookRecordInfo(name, ml)
	}
	g.f("}\n")
	_ = blocks
	_ = ck
}

func (g *rowGen) emitRowField(fl ir.FieldLayout) {
	f := fl.Field
	name := member(f)
	pieces := ir.FieldPieces(g.unit, f, fl.Offset)
	switch {
	case f.Type.Pointer:
		// A *T SLOT IS EIGHT BYTES AT EIGHT (docs/SPEC-TABLES.md §6.3, §7.2), holding
		// the SIGNED SELF-RELATIVE delta from the slot's own address, and NULL IS
		// ZERO. The slot's own address is its byte offset here, so the target's
		// offset is the slot's plus the delta — which is what <T>Cook.at resolves,
		// bounded by the region.
		g.f("    /** *%s: the slot's own offset — <Root>Cook.at resolves it (§6.3). */\n", f.Type.Name)
		g.f("    public static int %sSlot(int at) { return at + %d; }\n", name, pieces[0].Offset)
		g.f("    /** the raw SIGNED SELF-RELATIVE delta; zero is null. */\n")
		g.f("    public static long %sDelta(byte[] data, int at) { return TableBytes.i64(data, at + %d); }\n\n", name, pieces[0].Offset)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		what := "string"
		if f.Type.Kind == ir.TBytes {
			what = "bytes"
		}
		g.f("    /** %s(%d): the buffer's own offset and the used length beside it (§7.2).\n", what, f.Type.Size)
		g.f("     *  The used bytes are data[%sAt(at) .. +%sLength(data, at)]. */\n", name, name)
		g.f("    public static int %sAt(int at) { return at + %d; }\n", name, pieces[0].Offset)
		g.f("    public static int %sLength(byte[] data, int at) {\n", name)
		g.f("        int used = TableBytes.i32(data, at + %d);\n", pieces[1].Offset)
		g.f("        // a companion outside its bound is cook-check's refusal, not a read\n")
		g.f("        return used < 0 || used > %d ? 0 : used;\n", f.Type.Size)
		g.f("    }\n\n")
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		elem := javaRecordType(f.Type)
		elemSize := pieces[0].Size / f.ArrayBound
		if isClassRef(f.Type) {
			g.f("    /** [%d]%s: the element's own offset — read it with %sRow's accessors. */\n",
				f.ArrayBound, f.Type.Name, f.Type.Name)
			g.f("    public static int %sAt(int at, int i) { return at + %d + i * %d; }\n\n", name, pieces[0].Offset, elemSize)
		} else {
			g.f("    public static %s %s(byte[] data, int at, int i) { return %s; }\n",
				elem, name, readCall(elem, fmt.Sprintf("at + %d + i * %d", pieces[0].Offset, elemSize)))
		}
		if f.Array == ir.ArrayCounted {
			at := pieces[1].Offset
			g.f("    public static int %sCount(byte[] data, int at) {\n", name)
			g.f("        int used = TableBytes.i32(data, at + %d);\n", at)
			g.f("        return used < 0 || used > %d ? 0 : used;\n", f.ArrayBound)
			g.f("    }\n")
		}
		g.f("\n")
	case isClassRef(f.Type):
		g.f("    /** %s by value: its own offset — read it with %sRow's accessors. */\n", f.Type.Name, f.Type.Name)
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

// emitBlockRecordInfo is §19.2's descriptor for one ROW record: the offsets a
// reflective walk reads at, derived from the layout model independently of the
// accessors above — which is what TableBlockLayout's check compares.
func (g *rowGen) emitBlockRecordInfo(name string, ml *ir.MemberLayout) {
	g.f("    // this record's BLOCK descriptor (docs/SPEC-TABLES.md §8, §19.2): constant\n")
	g.f("    // data, so a reflective read costs a lookup and not a parse.\n")
	g.f("    private static TableBlockInfo blockInfo;\n\n")
	g.f("    public static TableBlockInfo blockInfo() {\n")
	g.f("        TableBlockInfo info = blockInfo;\n")
	g.f("        if (info != null) { return info; }\n")
	g.f("        info = new TableBlockInfo();\n")
	g.f("        info.name = %q; info.buildVersion = BuildVersion.value; info.size = %d; info.align = %d; info.numFields = %d;\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	g.f("        TableBlockFieldInfo[] fields = new TableBlockFieldInfo[%d];\n", len(ml.Fields))
	for i, fl := range ml.Fields {
		g.f("        fields[%d] = %s;\n", i, g.blockFieldInfo(fl, false, nil))
	}
	g.f("        info.fields = fields;\n")
	g.f("        blockInfo = info;\n")
	g.f("        return info;\n    }\n\n")
}

// blockFieldInfo renders one TableBlockFieldInfo construction expression.
func (g *rowGen) blockFieldInfo(fl ir.FieldLayout, projection bool, array *ir.BlockArray) string {
	f := fl.Field
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		// the ELEMENT kind, exactly as TableFieldInfo carries it (§8.1)
		kind = tkU8
	}
	if array != nil {
		return fmt.Sprintf("TableBlockFieldInfo.array(%q, %d, %d, %d, %d, %d, %d, %d, %d, () -> %sRow.blockInfo())",
			f.Name, fl.Offset, fl.Size, kind, array.OffsetOfOffset, array.CountOffset, array.StrideOffset,
			array.Stride, array.Max, array.ElemName)
	}
	facts := ir.BlockFieldOf(g.unit, f, fl.Offset, projection)
	element := "null"
	// A field that NAMES a record carries that record's layout, whether it holds
	// one or an array of them: an INLINE array of records is part of a row, and a
	// walker descending one reaches its element through this same column.
	if f.Type.Kind == ir.TNamed && !f.Type.Pointer {
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
			element = fmt.Sprintf("() -> %sRow.blockInfo()", ref.Name)
		}
	}
	return fmt.Sprintf("TableBlockFieldInfo.of(%q, %d, %d, %d, %d, %v, %v, %v, %d, %d, %d, %s)",
		f.Name, fl.Offset, fl.Size, kind, facts.CountOffset,
		facts.IsArray, facts.Counted, facts.Optional, facts.ArrayBound, facts.ElemSize,
		facts.PresentOffset, element)
}

// emitCookRecordInfo is §7's descriptor for one cooked record.
func (g *rowGen) emitCookRecordInfo(name string, ml *ir.MemberLayout) {
	g.f("    // this record's COOK descriptor (docs/SPEC-TABLES.md §7): the facts a region\n")
	g.f("    // actually has — where a field sits, how big it is, whether it is a\n")
	g.f("    // POINTER EDGE, the bound its COUNT COMPANION is checked against, and the\n")
	g.f("    // record it names.\n")
	g.f("    private static TableCookInfo cookInfo;\n\n")
	g.f("    public static TableCookInfo cookInfo() {\n")
	g.f("        TableCookInfo info = cookInfo;\n")
	g.f("        if (info != null) { return info; }\n")
	g.f("        info = new TableCookInfo();\n")
	g.f("        info.name = %q; info.size = %d; info.align = %d; info.numFields = %d;\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	g.f("        TableCookFieldInfo[] fields = new TableCookFieldInfo[%d];\n", len(ml.Fields))
	for i, fl := range ml.Fields {
		g.f("        fields[%d] = %s;\n", i, g.cookFieldInfo(fl))
	}
	g.f("        info.fields = fields;\n")
	g.f("        cookInfo = info;\n")
	g.f("        return info;\n    }\n\n")
}

func (g *rowGen) cookFieldInfo(fl ir.FieldLayout) string {
	f := fl.Field
	names := "null"
	if f.Type.Kind == ir.TNamed {
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
			names = fmt.Sprintf("() -> %sRow.cookInfo()", ref.Name)
		}
	}
	pieces := ir.FieldPieces(g.unit, f, fl.Offset)
	countOffset := int64(-1)
	if f.Array == ir.ArrayCounted {
		countOffset = pieces[len(pieces)-1].Offset
		if f.Type.Optional {
			countOffset = pieces[len(pieces)-2].Offset
		}
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		countOffset = pieces[1].Offset
	}
	isArray := f.KeyEnum != "" || f.Array != ir.ArrayNone
	bound := int64(1)
	elemSize := fl.Size
	if isArray {
		bound = f.ArrayBound
		elemSize = pieces[0].Size / bound
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		// the COUNT COMPANION of a string or a `bytes` bounds a walker just as an
		// array's does (§7.4), and the bound is the declared length
		bound = f.Type.Size
		isArray = false
	}
	presentOffset := int64(-1)
	if f.Type.Optional {
		presentOffset = pieces[len(pieces)-1].Offset
	}
	return fmt.Sprintf("TableCookFieldInfo.of(%q, %d, %d, %d, %v, %d, %v, %d, %d, TableCookStorage.%s, %s)",
		f.Name, fl.Offset, fl.Size, elemSize, isArray, bound, f.Type.Pointer, countOffset, presentOffset,
		cookStorageKind(f), names)
}

// cookStorageKind is what a cooked SLOT holds, which is not always what the
// wire carries: an ENUM slot holds the ORDINAL at the enum's own derived
// storage width (§7.2), where the wire carries the variant-name hash.
func cookStorageKind(f *ir.Field) string {
	if f.Type.Pointer {
		return "REFERENCE"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "BOOL"
	case ir.TFloat32, ir.TFloat64:
		return "FLOAT"
	case ir.TString:
		return "STRING"
	case ir.TBytes:
		return "BYTES"
	case ir.TBits:
		return "UNSIGNED"
	case ir.TInt:
		if f.Type.Signed {
			return "SIGNED"
		}
		return "UNSIGNED"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "UNSIGNED"
		case *ir.Struct:
			return "RECORD"
		}
	}
	return "RECORD"
}
