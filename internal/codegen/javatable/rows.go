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
	"strconv"
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
	// record is the declared name of the record being emitted, the <Record>
	// of a float accessor's non_finite_float refusal (docs/SPEC-TABLES.md §3.3)
	record string
}

// fieldName names one of the record's fields for a float accessor's refusal.
func (g *rowGen) fieldName(f *ir.Field) string { return g.record + "." + f.Name }

// declaredRecordName is a record's schema spelling (ir.Struct.DeclaredName),
// found by its generated name among the unit's tables and types.
func declaredRecordName(u *ir.Unit, name string) string {
	if st := u.Tables[name]; st != nil {
		return st.DeclaredName()
	}
	if st := u.Structs[name]; st != nil {
		return st.DeclaredName()
	}
	return name
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
	case ir.TInt, ir.TFixed:
		// a fixed is its RAW integer at its storage width (§7.2)
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

// readCall is the TableBytes read one Java type takes. A float's read names
// its field, <Record>.<field>, because it refuses a NaN or an infinity by
// that name (docs/SPEC-TABLES.md §3.3, ruling 2).
func readCall(typ, at, field string) string {
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
		return "TableBytes.f32(data, " + at + ", " + strconv.Quote(field) + ")"
	case "double":
		return "TableBytes.f64(data, " + at + ", " + strconv.Quote(field) + ")"
	}
	return "TableBytes.i8(data, " + at + ")"
}

// emitRowFiles emits one <Name>Row.java per record either accelerator reaches,
// and one per UNION the cook closure reaches.
func emitRowFiles(u *ir.Unit, set *records, blocks *ir.BlockUnit, ck *cookUnit) map[string][]byte {
	out := map[string][]byte{}
	for _, name := range set.order {
		g := &rowGen{unit: u, set: set}
		g.emitRow(name, set.layout[name], blocks, ck)
		out[name+"Row.java"] = javaFile(u, "one record of the two accelerators, read where it lies (docs/SPEC-TABLES.md §7, §19).", g.b.String())
	}
	if ck != nil {
		for _, un := range ck.unions {
			g := &rowGen{unit: u, set: set}
			g.emitUnionRow(un)
			out[un.Name+"Row.java"] = javaFile(u, "one cooked union, read where it lies (docs/SPEC-TABLES.md §7.2).", g.b.String())
		}
	}
	return out
}

// emitUnionRow is a cooked UNION (docs/SPEC-TABLES.md §7.2): the TAG at the
// union's own base at its storage width, the arms overlaid at align_up(tag
// width, greatest arm alignment), the whole rounded to the union's alignment.
// Only the SET arm's bytes are meaningful; every other byte is zero. Each arm
// is a field line, so its accessors are a record field's, at the arm offset.
func (g *rowGen) emitUnionRow(un *ir.Union) {
	size, align, tag, armAt := ir.UnionLayout(g.unit, un)
	// an arm is named as the variable form's Value names it (unionMembers):
	// the writer reads the set arm off that Value, and an arm whose own name
	// is the reserved "tag" is spelled tagArm there and here alike
	mem := unionMembers(un)
	var arms []ir.FieldLayout
	var tags []int
	for i, v := range un.Variants {
		if v.Void() {
			continue
		}
		armSize, armAlign := ir.ArmLayout(g.unit, v)
		one := *v.F
		one.Name = mem[i]
		arms = append(arms, ir.FieldLayout{Field: &one, Offset: armAt, Size: armSize, Align: armAlign})
		tags = append(tags, i+1)
	}
	name := un.Name
	g.f("// %s — a cooked UNION: a tag beside its overlaid arms (docs/SPEC-TABLES.md\n", name)
	g.f("// §7.2). `Row` is a CLAIMED suffix (§11), so no declaration can take it.\n")
	g.f("//\n")
	g.f("// The accessors are STATIC over (byte[] data, int at), `at` being where the\n")
	g.f("// union starts; an arm's accessors read the arm in place, and only the arm\n")
	g.f("// the tag names holds anything but zeros.\n")
	g.f("public final class %sRow {\n", name)
	g.f("    private %sRow() {}\n\n", name)
	g.f("    /** the union's own size and alignment, from the ONE layout model (§19.3). */\n")
	g.f("    public static final int size = %d;\n", size)
	g.f("    public static final int align = %d;\n", align)
	g.f("    /** the tag's width in bytes; the arms start at armOffset. */\n")
	g.f("    public static final int tagWidth = %d;\n", tag)
	g.f("    public static final int armOffset = %d;\n\n", armAt)
	g.f("    /** where each ARM starts, in declaration order — checked by\n")
	g.f("     *  TableCookLayout against the descriptors below (§19.3). */\n")
	g.f("    static final int[] offsets = {")
	for i, fl := range arms {
		if i > 0 {
			g.f(",")
		}
		g.f(" %d", fl.Offset)
	}
	g.f(" };\n\n")
	g.f("    /** the TAG: 0 is None, k is the k-th declared arm. */\n")
	g.f("    public static long tag(byte[] data, int at) { return %s; }\n\n", unsignedReadCall(tag, "at"))
	for _, fl := range arms {
		g.emitRowField(fl)
	}
	g.f("    // this union's COOK descriptor (docs/SPEC-TABLES.md §7.2): its fields are\n")
	g.f("    // its ARMS, each carrying the tag that selects it, at the arm offset.\n")
	g.f("    // published by CLASS INITIALIZATION, for the reason a record's states.\n")
	g.f("    private static final class CookInfoHolder {\n")
	g.f("        static final TableCookInfo INFO = build();\n\n")
	g.f("        private static TableCookInfo build() {\n")
	g.f("            TableCookInfo info = new TableCookInfo();\n")
	g.f("            info.name = %q; info.size = %d; info.align = %d; info.numFields = %d;\n",
		name, size, align, len(arms))
	g.f("            info.tagWidth = %d; info.armCount = %d;\n", tag, un.Max)
	if cookUnionHasExtent(g.unit, un) {
		g.f("            info.hasExtent = true;\n")
	}
	g.f("            TableCookFieldInfo[] fields = new TableCookFieldInfo[%d];\n", len(arms))
	for i, fl := range arms {
		g.f("            fields[%d] = %s.arm(%d);\n", i, g.cookFieldInfo(fl), tags[i])
	}
	g.f("            info.fields = fields;\n")
	g.f("            return info;\n        }\n    }\n\n")
	g.f("    public static TableCookInfo cookInfo() { return CookInfoHolder.INFO; }\n")
	g.f("}\n")
}

// isWide answers whether a scalar is 128 bits: an int128, a uint128, or a
// fixed or ufixed whose storage is 128 bits (§7.2).
func isWide(t ir.FieldType) bool {
	return !t.Pointer && (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Width == 128
}

// unsignedReadCall reads an unsigned integer of `width` bytes, widened to long.
func unsignedReadCall(width int64, at string) string {
	switch width {
	case 1:
		return "TableBytes.u8(data, " + at + ")"
	case 2:
		return "TableBytes.u16(data, " + at + ")"
	case 4:
		return "TableBytes.u32(data, " + at + ")"
	}
	return "TableBytes.i64(data, " + at + ")"
}

func (g *rowGen) emitRow(name string, ml *ir.MemberLayout, blocks *ir.BlockUnit, ck *cookUnit) {
	g.record = declaredRecordName(g.unit, name)
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
	case f.IsList() || f.IsMap():
		g.emitRowHolder(f, name, pieces[0].Offset)
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
		switch {
		case isClassRef(f.Type):
			g.f("    /** [%d]%s: the element's own offset — read it with %sRow's accessors. */\n",
				f.ArrayBound, f.Type.Name, f.Type.Name)
			g.f("    public static int %sAt(int at, int i) { return at + %d + i * %d; }\n\n", name, pieces[0].Offset, elemSize)
		case isWide(f.Type):
			g.f("    /** element i's low and high 64-bit halves (§7.2). */\n")
			g.f("    public static long %sLo(byte[] data, int at, int i) { return TableBytes.i64(data, at + %d + i * %d); }\n",
				name, pieces[0].Offset, elemSize)
			g.f("    public static long %sHi(byte[] data, int at, int i) { return TableBytes.i64(data, at + %d + i * %d); }\n",
				name, pieces[0].Offset+8, elemSize)
		default:
			g.f("    public static %s %s(byte[] data, int at, int i) { return %s; }\n",
				elem, name, readCall(elem, fmt.Sprintf("at + %d + i * %d", pieces[0].Offset, elemSize), g.fieldName(f)))
		}
		if f.KeyEnum != "" {
			g.emitRowKeyForm(f, name, elem, pieces[0].Offset, elemSize)
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
	case isWide(f.Type):
		// SIXTEEN BYTES AT SIXTEEN (§7.2): the low half first in this reader's
		// little-endian order, each half one eight-byte lane
		g.f("    /** a 128-bit slot's low and high 64-bit halves (§7.2). */\n")
		g.f("    public static long %sLo(byte[] data, int at) { return TableBytes.i64(data, at + %d); }\n", name, pieces[0].Offset)
		g.f("    public static long %sHi(byte[] data, int at) { return TableBytes.i64(data, at + %d); }\n\n", name, pieces[0].Offset+8)
	default:
		typ := javaRecordType(f.Type)
		g.f("    public static %s %s(byte[] data, int at) { return %s; }\n\n",
			typ, name, readCall(typ, fmt.Sprintf("at + %d", pieces[0].Offset), g.fieldName(f)))
	}
	if f.Type.Optional {
		g.f("    public static boolean %sPresent(byte[] data, int at) { return TableBytes.bool(data, at + %d); }\n\n",
			name, pieces[len(pieces)-1].Offset)
	}
}

// emitRowKeyForm is a row's KEY FORM (docs/SPEC-TABLES.md §2.4, docs/PORTING.md
// M5), beside the slot form and additive to it: the slot accessor above still
// takes a storage index, and this one takes the KEY, refuses None and a key past
// Max by name in every build (javaKeyRefusal's one unsigned compare), and
// writes the shift — key k at slot k-1 — so no caller does. A row is read over
// a caller's byte[] that is usually wider than the row, which is where the
// JVM's own bound gives no help: slot -1 or slot Max is a silent read of a
// neighbour, so the refusal is the accessor's or it is nobody's. `ByKey` and
// `AtKey` are the key form's spellings of the slot form's `<name>` and
// `<name>At`.
func (g *rowGen) emitRowKeyForm(f *ir.Field, name, elem string, base, elemSize int64) {
	if isClassRef(f.Type) {
		g.f("    /** [%s]%s %s: the element's own offset, refusing None and past Max. */\n",
			f.KeyEnum, f.Type.Name, keyedFormMarker)
		g.f("    public static int %sAtKey(int at, int key) {\n", name)
		g.f("%s", javaKeyRefusal("        ", "key", f))
		g.f("        return at + %d + (key - 1) * %d;\n", base, elemSize)
		g.f("    }\n")
		return
	}
	g.f("    /** %s %s: key k reads slot k-1, refusing None and past Max. */\n",
		ir.FieldTypeSpelling(f), keyedFormMarker)
	g.f("    public static %s %sByKey(byte[] data, int at, int key) {\n", elem, name)
	g.f("%s", javaKeyRefusal("        ", "key", f))
	g.f("        return %s;\n", readCall(elem, fmt.Sprintf("at + %d + (key - 1) * %d", base, elemSize), g.fieldName(f)))
	g.f("    }\n")
}

// emitBlockRecordInfo is §19.2's descriptor for one ROW record: the offsets a
// reflective walk reads at, derived from the layout model independently of the
// accessors above — which is what TableBlockLayout's check compares.
func (g *rowGen) emitBlockRecordInfo(name string, ml *ir.MemberLayout) {
	g.f("    // this record's BLOCK descriptor (docs/SPEC-TABLES.md §8, §19.2): constant\n")
	g.f("    // data, so a reflective read costs a lookup and not a parse — and it is\n")
	g.f("    // published by CLASS INITIALIZATION rather than by a plain cached write,\n")
	g.f("    // because a descriptor's fields are not final and a racing reader could\n")
	g.f("    // otherwise see a non-null descriptor with a null field array (JLS §17.4).\n")
	g.f("    private static final class BlockInfoHolder {\n")
	g.f("        static final TableBlockInfo INFO = build();\n\n")
	g.f("        private static TableBlockInfo build() {\n")
	g.f("            TableBlockInfo info = new TableBlockInfo();\n")
	g.f("            info.name = %q; info.buildVersion = BuildVersion.value; info.size = %d; info.align = %d; info.numFields = %d;\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	g.f("            TableBlockFieldInfo[] fields = new TableBlockFieldInfo[%d];\n", len(ml.Fields))
	for i, fl := range ml.Fields {
		g.f("            fields[%d] = %s;\n", i, g.blockFieldInfo(fl, false, nil))
	}
	g.f("            info.fields = fields;\n")
	g.f("            return info;\n        }\n    }\n\n")
	g.f("    public static TableBlockInfo blockInfo() { return BlockInfoHolder.INFO; }\n\n")
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
	g.f("    // published by CLASS INITIALIZATION, for the reason the block half states.\n")
	g.f("    private static final class CookInfoHolder {\n")
	g.f("        static final TableCookInfo INFO = build();\n\n")
	g.f("        private static TableCookInfo build() {\n")
	g.f("            TableCookInfo info = new TableCookInfo();\n")
	g.f("            info.name = %q; info.size = %d; info.align = %d; info.numFields = %d;\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	if st := g.unit.Tables[name]; st != nil {
		g.f("            info.typeId = 0x%016xL;\n", ir.TableWireId(st.WireName()))
	}
	g.f("            info.defaults = %sVariable.Value::new;\n", name)
	if st := cookMember(g.unit, name); st != nil && cookHasExtent(g.unit, st) {
		g.f("            info.hasExtent = true;\n")
	}
	g.f("            TableCookFieldInfo[] fields = new TableCookFieldInfo[%d];\n", len(ml.Fields))
	for i, fl := range ml.Fields {
		g.f("            fields[%d] = %s;\n", i, g.cookFieldInfo(fl))
	}
	g.f("            info.fields = fields;\n")
	g.f("            return info;\n        }\n    }\n\n")
	g.f("    public static TableCookInfo cookInfo() { return CookInfoHolder.INFO; }\n\n")
}

func (g *rowGen) cookFieldInfo(fl ir.FieldLayout) string {
	f := fl.Field
	if f.IsList() || f.IsMap() {
		return g.cookHolderInfo(fl)
	}
	names := "null"
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			names = fmt.Sprintf("() -> %sRow.cookInfo()", ref.Name)
		case *ir.Union:
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
		// Only an INLINE string(N)/bytes(N) carries a count companion beside its
		// bytes; a *string/*bytes BLOB is a pointer slot (REFERENCE, §2.5, §6.3)
		// and has no companion, so its countOffset stays -1.
		if !f.Type.Pointer {
			countOffset = pieces[1].Offset
		}
	}
	isArray := f.KeyEnum != "" || f.Array != ir.ArrayNone
	bound := int64(1)
	// one slot's width is the VALUE piece's: an optional's presence bool is
	// its own piece after it (§7.2), and a store at the field's whole size
	// would overwrite it
	elemSize := pieces[0].Size
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
	return fmt.Sprintf("TableCookFieldInfo.of(%q, %q, %d, %d, %d, %v, %d, %v, %d, %d, TableCookStorage.%s, %s)",
		f.Name, varMember(f), fl.Offset, fl.Size, elemSize, isArray, bound, f.Type.Pointer, countOffset, presentOffset,
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
	case ir.TInt, ir.TFixed:
		if isWide(f.Type) {
			return "WIDE"
		}
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
		case *ir.Union:
			return "UNION"
		}
	}
	return "RECORD"
}

// cookHolderInfo is a LIST's or a MAP's cooked descriptor (docs/SPEC-TABLES.md
// §2.8, §2.9, §7.2): the sixteen-byte holder at the field's offset, a
// self-relative reference and an int32 count, and the element the node extent
// lays at its own size and alignment — a list's element as a field at offset
// zero, a map's entry record.
func (g *rowGen) cookHolderInfo(fl ir.FieldLayout) string {
	f := fl.Field
	pieces := ir.FieldPieces(g.unit, f, fl.Offset)
	var size, align int64
	var elem, kind string
	if f.IsMap() {
		ml := ir.RecordLayout(g.unit, f.MapEntry)
		size, align = ml.Size, max(ml.Align, 1)
		elem = fmt.Sprintf("TableCookFieldInfo.of(%q, %q, 0, %d, %d, false, 1, false, -1, -1, TableCookStorage.RECORD, () -> %sRow.cookInfo())",
			f.Name, varMember(f), size, size, f.MapEntry.Name)
		kind = "MAP"
	} else {
		size, align = ir.ListElementLayout(g.unit, f)
		elem = g.cookFieldInfo(ir.FieldLayout{Field: variableElement(f), Offset: 0, Size: size, Align: align})
		kind = "LIST"
	}
	return fmt.Sprintf("TableCookFieldInfo.of(%q, %q, %d, %d, %d, false, 1, false, -1, -1, TableCookStorage.%s, null).extent(%d, %d, %s)",
		f.Name, varMember(f), fl.Offset, fl.Size, pieces[0].Size, kind, size, align, elem)
}

// emitRowHolder is a LIST's or a MAP's sixteen-byte holder (docs/SPEC-TABLES.md
// §2.8, §2.9, §7.2): the slot, holding the SIGNED SELF-RELATIVE delta to the
// element array in the holder's node extent (zero for an empty one), and the
// int32 count beside it. <Root>Cook.at(slot, count * stride) resolves the array,
// bounded by the region. A MAP adds FIND: a binary search over the ascending
// entries where they lie, floor(log2 n) + 1 key compares and no allocation.
func (g *rowGen) emitRowHolder(f *ir.Field, name string, off int64) {
	var stride int64
	if f.IsMap() {
		stride = ir.RecordLayout(g.unit, f.MapEntry).Size
	} else {
		stride, _ = ir.ListElementLayout(g.unit, f)
	}
	g.f("    /** %s: the holder's slot; <Root>Cook.at(%sSlot(at), %sCount(data, at) * %sStride)\n",
		ir.FieldTypeSpelling(f), name, name, name)
	g.f("     *  resolves the element array (§2.8, §2.9, §6.3). */\n")
	g.f("    public static int %sSlot(int at) { return at + %d; }\n", name, off)
	g.f("    /** the raw SIGNED SELF-RELATIVE delta to the element array; zero is empty. */\n")
	g.f("    public static long %sDelta(byte[] data, int at) { return TableBytes.i64(data, at + %d); }\n", name, off)
	g.f("    /** one element's size in the array: a list's element or a map's entry. */\n")
	g.f("    public static final int %sStride = %d;\n", name, stride)
	g.f("    public static int %sCount(byte[] data, int at) {\n", name)
	g.f("        int n = TableBytes.i32(data, at + %d);\n", off+8)
	g.f("        return n < 0 ? 0 : n;\n")
	g.f("    }\n")
	if f.IsMap() {
		g.emitRowFind(f, name, stride)
	}
	g.f("\n")
}

// emitRowFind is a cooked map's FIND (docs/PORTING.md M19): the entries lie in
// ascending key order (§2.8), integers by VALUE (signed for the signed kinds,
// unsigned for the unsigned) and strings by BYTES, unsigned, a prefix first. It
// answers the found entry's offset, read with <Entry>Row, or -1.
func (g *rowGen) emitRowFind(f *ir.Field, name string, stride int64) {
	key := ir.MapKeyField(f)
	entry := f.MapEntry.Name + "Row"
	kname := member(key)
	if key.Type.Kind == ir.TString {
		g.f("    /** the entry whose key is these UTF-8 bytes, in the array at entries, or -1. */\n")
		g.f("    public static int %sFind(byte[] data, int entries, int count, byte[] key) {\n", name)
		g.f("        int low = 0, high = count;\n")
		g.f("        while (low < high) {\n")
		g.f("            int mid = (low + high) >>> 1;\n")
		g.f("            int at = entries + mid * %d;\n", stride)
		g.f("            int k = %s.%sAt(at), n = %s.%sLength(data, at);\n", entry, kname, entry, kname)
		g.f("            int order = java.util.Arrays.compareUnsigned(data, k, k + n, key, 0, key.length);\n")
		g.f("            if (order == 0) { return at; }\n")
		g.f("            if (order < 0) { low = mid + 1; } else { high = mid; }\n")
		g.f("        }\n        return -1;\n    }\n")
		return
	}
	typ := javaRecordType(key.Type)
	width := key.Type.Width
	widen := "(long) %s"
	cmp := "Long.compare"
	if !key.Type.Signed {
		cmp = "Long.compareUnsigned"
		if width < 64 {
			widen = fmt.Sprintf("((long) %%s & 0x%xL)", uint64(1)<<width-1)
		}
	}
	g.f("    /** the entry whose key is key, in the array at entries, or -1. */\n")
	g.f("    public static int %sFind(byte[] data, int entries, int count, %s key) {\n", name, typ)
	g.f("        long want = %s;\n", fmt.Sprintf(widen, "key"))
	g.f("        int low = 0, high = count;\n")
	g.f("        while (low < high) {\n")
	g.f("            int mid = (low + high) >>> 1;\n")
	g.f("            int at = entries + mid * %d;\n", stride)
	g.f("            int order = %s(%s, want);\n", cmp, fmt.Sprintf(widen, entry+"."+kname+"(data, at)"))
	g.f("            if (order == 0) { return at; }\n")
	g.f("            if (order < 0) { low = mid + 1; } else { high = mid; }\n")
	g.f("        }\n        return -1;\n    }\n")
}
