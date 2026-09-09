// The form-1 fixed-table wire. Descriptor walks use the generated,
// typed storage accessors; no reflection, boxing, or per-read scratch allocation.
package cstable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func isCleanScalarLeaf(f *ir.Field) bool {
	if f.Array != ir.ArrayNone || f.Type.Optional || f.Guard != "" || f.IsMap() || f.IsList() || f.Type.Pointer {
		return false
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TFloat32, ir.TFloat64:
		return true
	case ir.TInt:
		return f.Type.Width <= 64
	case ir.TBits:
		return f.Type.Width <= 64
	}
	return false
}

func isScalarLeafStruct(st *ir.Struct) bool {
	if st == nil || len(st.Fields) == 0 {
		return false
	}
	for _, f := range st.Fields {
		if !isCleanScalarLeaf(f) {
			return false
		}
	}
	return true
}

func isChildScalarArray(f *ir.Field) (*ir.Struct, bool) {
	if !f.IsMap() && !f.IsList() && f.KeyEnum == "" && !f.Type.Pointer && f.Guard == "" && !f.Type.Optional && (f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted) && f.Type.Kind == ir.TNamed {
		if st, ok := f.Type.Ref.(*ir.Struct); ok && isScalarLeafStruct(st) {
			return st, true
		}
	}
	return nil, false
}

func needsElemOffset(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Array != ir.ArrayNone && f.KeyEnum == "" && tableScalarKind(f) == tkTable {
			return true
		}
	}
	return false
}

func needsFieldsArray(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if isCleanScalarLeaf(f) {
			continue
		}
		if _, ok := isChildScalarArray(f); ok {
			continue
		}
		return true
	}
	return false
}

func isPayloadPrefixed(f *ir.Field) bool {
	kind := tableScalarKind(f)
	return f.Array != ir.ArrayNone || kind == tkString || kind == 33 || kind == tkTable
}

func leafRideCondition(f *ir.Field) string {
	def := fieldDefaultExpr(f)
	prop := member(f)
	return fmt.Sprintf("v.%s != %s", prop, def)
}

func (g *tableGen) emitCollectTyped(st *ir.Struct) {
	name := st.Name
	g.pf("public static bool %sCollectTyped(%s v, ref TableWire.Ids ids)\n{\n", name, name)
	if needsFieldsArray(st) {
		g.pf("    TableFieldInfo[] fields = %sTableType().Fields;\n", name)
	}
	for i, f := range st.Fields {
		id := ir.TableFieldWireId(f)
		if isCleanScalarLeaf(f) {
			cond := leafRideCondition(f)
			g.pf("    if (%s && !ids.Add(0x%016xul)) return false;\n", cond, id)
		} else if childSt, ok := isChildScalarArray(f); ok {
			prop := member(f)
			childName := childSt.Name
			if f.CountedOnWire() {
				g.pf("    int count_%d = v.%sCount;\n", i, prop)
				g.pf("    if (count_%d < 0 || count_%d > %d) return false;\n", i, i, f.ArrayBound)
				g.pf("    if (count_%d > 0)\n    {\n", i)
				g.pf("        if (!ids.Add(0x%016xul)) return false;\n", id)
				g.pf("        for (int i_%d = 0; i_%d < count_%d; i_%d++)\n        {\n", i, i, i, i)
				g.pf("            if (!%sCollectTyped(v.%s[i_%d], ref ids)) return false;\n", childName, prop, i)
				g.pf("        }\n    }\n")
			} else {
				g.pf("    if (!ids.Add(0x%016xul)) return false;\n", id)
				g.pf("    for (int i_%d = 0; i_%d < %d; i_%d++)\n    {\n", i, i, f.ArrayBound, i)
				g.pf("        if (!%sCollectTyped(v.%s[i_%d], ref ids)) return false;\n", childName, prop, i)
				g.pf("    }\n")
			}
		} else {
			g.pf("    if (!TableWire.CollectField(v, fields[%d], ref ids)) return false;\n", i)
		}
	}
	g.pf("    return true;\n}\n\n")
}

func (g *tableGen) knownOrdinal(f *ir.Field, id uint64) int {
	if g.idOrdinal == nil {
		g.idOrdinal = make(map[uint64]int)
		for i, known := range ir.TableWireIds(g.unit) {
			g.idOrdinal[known] = i
		}
	}
	ord, ok := g.idOrdinal[id]
	if !ok {
		panic(fmt.Sprintf("table field %s (%d) missing from wire ID table; schema compiler invariant violated", f.Name, id))
	}
	return ord
}

func (g *tableGen) emitBodySizeTyped(st *ir.Struct) {
	name := st.Name
	g.pf("public static long %sBodySizeTyped(%s v, ref TableWire.Ids ids, scoped Span<long> rootPayloadSizes = default, scoped Span<long> rootElemSizes = default)\n{\n", name, name)
	g.pf("    long n = 1;\n")
	if needsFieldsArray(st) {
		g.pf("    TableFieldInfo[] fields = %sTableType().Fields;\n", name)
	}
	if needsElemOffset(st) {
		g.pf("    int elemOffset = 0;\n")
	}
	for i, f := range st.Fields {
		id := ir.TableFieldWireId(f)
		if isCleanScalarLeaf(f) {
			cond := leafRideCondition(f)
			kind := tableScalarKind(f)
			width := tableKindWidth(kind)
			g.pf("    if (%s) { n += TableWire.VarSize(ids.RefAt(%d, 0x%016xul)) + %d; }\n", cond, g.knownOrdinal(f, id), id, 1+width)
		} else if childSt, ok := isChildScalarArray(f); ok {
			prop := member(f)
			childName := childSt.Name
			if f.CountedOnWire() {
				g.pf("    int count_%d = v.%sCount;\n", i, prop)
				g.pf("    if (count_%d > 0)\n    {\n", i)
			} else {
				g.pf("    int count_%d = %d;\n    {\n", i, f.ArrayBound)
			}
			g.pf("        scoped Span<long> elemCache_%d = default;\n", i)
			g.pf("        if (!rootElemSizes.IsEmpty)\n        {\n")
			g.pf("            int take = System.Math.Min(count_%d, System.Math.Max(0, rootElemSizes.Length - elemOffset));\n", i)
			g.pf("            elemCache_%d = rootElemSizes.Slice(elemOffset, take);\n", i)
			g.pf("            elemOffset += take;\n        }\n")
			g.pf("        long nChild_%d = 0;\n", i)
			g.pf("        for (int i_%d = 0; i_%d < count_%d; i_%d++)\n        {\n", i, i, i, i)
			g.pf("            long childBody = %sBodySizeTyped(v.%s[i_%d], ref ids);\n", childName, prop, i)
			g.pf("            if (!elemCache_%d.IsEmpty && i_%d < elemCache_%d.Length) { elemCache_%d[i_%d] = childBody; }\n", i, i, i, i, i)
			g.pf("            nChild_%d += TableWire.VarSize((ulong)childBody) + childBody;\n", i)
			g.pf("        }\n")
			g.pf("        long payload_%d = 1 + TableWire.VarSize((ulong)count_%d) + nChild_%d;\n", i, i, i)
			g.pf("        n += TableWire.VarSize(ids.RefAt(%d, 0x%016xul)) + 1 + TableWire.VarSize((ulong)payload_%d) + payload_%d;\n", g.knownOrdinal(f, id), id, i, i)
			g.pf("        if (!rootPayloadSizes.IsEmpty) { rootPayloadSizes[%d] = payload_%d; }\n", i, i)
			g.pf("    }\n")
		} else {
			switch {
			case f.Array != ir.ArrayNone && f.KeyEnum == "" && tableScalarKind(f) == tkTable:
				g.pf("    scoped Span<long> elemCache_%d = default;\n", i)
				g.pf("    if (!rootElemSizes.IsEmpty)\n    {\n")
				g.pf("        int c = TableWire.Count(v, fields[%d]);\n", i)
				g.pf("        int take = System.Math.Min(c, System.Math.Max(0, rootElemSizes.Length - elemOffset));\n")
				g.pf("        elemCache_%d = rootElemSizes.Slice(elemOffset, take);\n", i)
				g.pf("        elemOffset += take;\n    }\n")
				g.pf("    n += TableWire.BodySizeField(v, fields[%d], ref ids, elemCache_%d, out long payload_%d);\n", i, i, i)
				g.pf("    if (!rootPayloadSizes.IsEmpty) { rootPayloadSizes[%d] = payload_%d; }\n", i, i)
			case isPayloadPrefixed(f):
				g.pf("    n += TableWire.BodySizeField(v, fields[%d], ref ids, default, out long payload_%d);\n", i, i)
				g.pf("    if (!rootPayloadSizes.IsEmpty) { rootPayloadSizes[%d] = payload_%d; }\n", i, i)
			default:
				g.pf("    n += TableWire.BodySizeField(v, fields[%d], ref ids);\n", i)
			}
		}
	}
	g.pf("    return n;\n}\n\n")
}

func (g *tableGen) emitWriteBodyTyped(st *ir.Struct) {
	name := st.Name
	g.pf("public static void %sWriteBodyTyped(ref TableWire.Writer w, %s v, ref TableWire.Ids ids, scoped ReadOnlySpan<long> rootPayloadSizes = default, scoped ReadOnlySpan<long> rootElemSizes = default)\n{\n", name, name)
	if needsFieldsArray(st) {
		g.pf("    TableFieldInfo[] fields = %sTableType().Fields;\n", name)
	}
	if needsElemOffset(st) {
		g.pf("    int elemOffset = 0;\n")
	}
	for i, f := range st.Fields {
		id := ir.TableFieldWireId(f)
		if isCleanScalarLeaf(f) {
			cond := leafRideCondition(f)
			kind := tableScalarKind(f)
			width := tableKindWidth(kind)
			raw := csRawGet("v."+member(f), f.Type)
			g.pf("    if (%s)\n    {\n", cond)
			g.pf("        w.HeaderAt(%d, 0x%016xul, %d, ref ids);\n", g.knownOrdinal(f, id), id, kind)
			g.pf("        w.Fixed(%s, %d);\n", raw, width)
			g.pf("    }\n")
		} else if childSt, ok := isChildScalarArray(f); ok {
			prop := member(f)
			childName := childSt.Name
			if f.CountedOnWire() {
				g.pf("    int count_%d = v.%sCount;\n", i, prop)
				g.pf("    if (count_%d > 0)\n    {\n", i)
			} else {
				g.pf("    int count_%d = %d;\n    {\n", i, f.ArrayBound)
			}
			g.pf("        w.HeaderAt(%d, 0x%016xul, 14, ref ids);\n", g.knownOrdinal(f, id), id)
			g.pf("        scoped ReadOnlySpan<long> elemCache_%d = default;\n", i)
			g.pf("        if (!rootElemSizes.IsEmpty)\n        {\n")
			g.pf("            int take = System.Math.Min(count_%d, System.Math.Max(0, rootElemSizes.Length - elemOffset));\n", i)
			g.pf("            elemCache_%d = rootElemSizes.Slice(elemOffset, take);\n", i)
			g.pf("            elemOffset += take;\n        }\n")
			g.pf("        long payload_%d = !rootPayloadSizes.IsEmpty ? rootPayloadSizes[%d] : -1;\n", i, i)
			g.pf("        if (payload_%d < 0)\n        {\n", i)
			g.pf("            long nChild_%d = 0;\n", i)
			g.pf("            for (int i_%d = 0; i_%d < count_%d; i_%d++)\n            {\n", i, i, i, i)
			g.pf("                long childBody = (!elemCache_%d.IsEmpty && i_%d < elemCache_%d.Length) ? elemCache_%d[i_%d] : %sBodySizeTyped(v.%s[i_%d], ref ids);\n", i, i, i, i, i, childName, prop, i)
			g.pf("                nChild_%d += TableWire.VarSize((ulong)childBody) + childBody;\n            }\n", i)
			g.pf("            payload_%d = 1 + TableWire.VarSize((ulong)count_%d) + nChild_%d;\n", i, i, i)
			g.pf("        }\n")
			g.pf("        w.Var((ulong)payload_%d);\n", i)
			g.pf("        w.Byte(13);\n")
			g.pf("        w.Var((ulong)count_%d);\n", i)
			g.pf("        for (int i_%d = 0; i_%d < count_%d; i_%d++)\n        {\n", i, i, i, i)
			g.pf("            long childBody = (!elemCache_%d.IsEmpty && i_%d < elemCache_%d.Length) ? elemCache_%d[i_%d] : %sBodySizeTyped(v.%s[i_%d], ref ids);\n", i, i, i, i, i, childName, prop, i)
			g.pf("            w.Var((ulong)childBody);\n")
			g.pf("            %sWriteBodyTyped(ref w, v.%s[i_%d], ref ids);\n", childName, prop, i)
			g.pf("        }\n    }\n")
		} else {
			switch {
			case f.Array != ir.ArrayNone && f.KeyEnum == "" && tableScalarKind(f) == tkTable:
				g.pf("    scoped ReadOnlySpan<long> elemCache_%d = default;\n", i)
				g.pf("    if (!rootElemSizes.IsEmpty)\n    {\n")
				g.pf("        int c = TableWire.Count(v, fields[%d]);\n", i)
				g.pf("        int take = System.Math.Min(c, System.Math.Max(0, rootElemSizes.Length - elemOffset));\n")
				g.pf("        elemCache_%d = rootElemSizes.Slice(elemOffset, take);\n", i)
				g.pf("        elemOffset += take;\n    }\n")
				g.pf("    long payload_%d = !rootPayloadSizes.IsEmpty ? rootPayloadSizes[%d] : -1;\n", i, i)
				g.pf("    TableWire.WriteBodyField(ref w, v, fields[%d], ref ids, elemCache_%d, payload_%d);\n", i, i, i)
			case isPayloadPrefixed(f):
				g.pf("    long payload_%d = !rootPayloadSizes.IsEmpty ? rootPayloadSizes[%d] : -1;\n", i, i)
				g.pf("    TableWire.WriteBodyField(ref w, v, fields[%d], ref ids, default, payload_%d);\n", i, i)
			default:
				g.pf("    TableWire.WriteBodyField(ref w, v, fields[%d], ref ids);\n", i)
			}
		}
	}
	g.pf("    w.Var(0);\n}\n\n")
}

func (g *tableGen) emitSaveTyped(st *ir.Struct) {
	name := st.Name
	g.pf("public static long %sSaveTyped(%s value, Span<byte> buffer, Span<ulong> vocabulary, bool measure)\n{\n", name, name)
	g.pf("    TableTypeInfo type = %sTableType();\n", name)
	g.pf("    int slots = 0;\n")
	g.pf("    if (vocabulary.Length <= 1024)\n    {\n")
	g.pf("        slots = 1;\n")
	g.pf("        while (slots < vocabulary.Length * 2) { slots <<= 1; }\n    }\n")
	g.pf("    Span<int> index = stackalloc int[slots];\n")
	g.pf("    Span<uint> ordinalSlots = vocabulary.Length <= 1024 ? stackalloc uint[vocabulary.Length] : default;\n")
	g.pf("    Span<int> ordinalOf = vocabulary.Length <= 1024 ? stackalloc int[vocabulary.Length] : default;\n")
	g.pf("    TableWire.Ids ids = new TableWire.Ids(vocabulary, index, ordinalSlots, ordinalOf);\n")
	g.pf("    if (!%sCollectTyped(value, ref ids)) { return -1; }\n", name)
	g.pf("    int cachedFields = !measure && type.Fields.Length <= 256 ? type.Fields.Length : 0;\n")
	g.pf("    Span<long> rootPayloadSizes = stackalloc long[cachedFields];\n")
	g.pf("    int cachedElemSlots = !measure ? type.RootElemSlots : 0;\n")
	g.pf("    Span<long> rootElemSizes = stackalloc long[cachedElemSlots];\n")
	g.pf("    long n = 1 + %sBodySizeTyped(value, ref ids, rootPayloadSizes, rootElemSizes) + 8L * ids.Count + 8;\n", name)
	g.pf("    if (measure) { return n; }\n")
	g.pf("    if (n > buffer.Length) { return -1; }\n")
	g.pf("    scoped TableWire.Writer w = new TableWire.Writer(buffer);\n")
	g.pf("    w.Byte(1);\n")
	g.pf("    %sWriteBodyTyped(ref w, value, ref ids, rootPayloadSizes, rootElemSizes);\n", name)
	g.pf("    for (int i = 0; i < ids.Count; i++) { w.Fixed(ids.Values[i], 8); }\n")
	g.pf("    w.Fixed((ulong)ids.Count, 8);\n")
	g.pf("    return w.Offset;\n}\n\n")
}

func (g *tableGen) emitTypedWireSurface(st *ir.Struct) {
	g.emitCollectTyped(st)
	g.emitBodySizeTyped(st)
	g.emitWriteBodyTyped(st)
	g.emitSaveTyped(st)
}

func (g *tableGen) emitWireSurface(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	if ir.VariableTables(g.unit)[st.Name] {
		g.pf("public static long %sLoadMeasure(ReadOnlySpan<byte> bytes) { return TableWire.LoadMeasure(%sTableType(), bytes, out _); }\n\n", st.Name, st.Name)
		g.pf("public static long %sLoadMeasure(ReadOnlySpan<byte> bytes, out TableRefuseReason reason) { long size = TableWire.LoadMeasure(%sTableType(), bytes, out string why); reason = why == null ? TableRefuseReason.ok : Enum.Parse<TableRefuseReason>(why); return size; }\n", st.Name, st.Name)
	}
	cap := ir.TableWireIdCapacity(g.unit)
	if !ir.VariableTables(g.unit)[st.Name] {
		g.emitTypedWireSurface(st)
		g.pf("public static long %sMeasure(%s value)\n{\n    Span<ulong> ids = stackalloc ulong[%d];\n    return %sSaveTyped(value, Span<byte>.Empty, ids, true);\n}\n\n", st.Name, st.Name, cap, st.Name)
		g.pf("public static long %sSave(%s value, Span<byte> buffer)\n{\n    Span<ulong> ids = stackalloc ulong[%d];\n    return %sSaveTyped(value, buffer, ids, false);\n}\n\n", st.Name, st.Name, cap, st.Name)
	} else {
		g.pf("public static long %sMeasure(%s value)\n{\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(value, %sTableType(), Span<byte>.Empty, ids, true);\n}\n\n", st.Name, st.Name, cap, st.Name)
		g.pf("public static long %sSave(%s value, Span<byte> buffer)\n{\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(value, %sTableType(), buffer, ids, false);\n}\n\n", st.Name, st.Name, cap, st.Name)
	}
	g.pf("public static TableWire.Verdict %sLoadVerdict(%s value, ReadOnlySpan<byte> bytes, TableReport report)\n{\n    return TableWire.Load(value, %sTableType(), bytes, report);\n}\n\n", st.Name, st.Name, st.Name)
	g.pf("public static bool %sLoad(%s value, ReadOnlySpan<byte> bytes, TableReport report)\n{\n    return %sLoadVerdict(value, bytes, report) == TableWire.Verdict.Ok;\n}\n\n", st.Name, st.Name, st.Name)
}

// The wire's clamps use integer literals at their actual width, never the
// descriptor's double range columns (which are for text/UI reflection).
func (g *tableGen) wireColumns(f *ir.Field) string {
	var b strings.Builder
	if g.owner.IsMapEntry() && f == g.owner.Fields[0] {
		b.WriteString(", MapKey = true")
	}
	b.WriteString(messageSlot(g.unit, f))
	b.WriteString(g.nativeColumns(f))
	if f.IsList() || f.IsMap() {
		size, align := int64(0), int64(0)
		if f.IsMap() {
			layout := ir.RecordLayout(g.unit, f.MapEntry)
			size, align = layout.Size, layout.Align
		} else {
			size, align = ir.ListElementLayout(g.unit, f)
		}
		fmt.Fprintf(&b, ", StorageSize = %d, StorageAlign = %d", size, align)
		fmt.Fprintf(&b, ", Dynamic = true, Map = %t", f.IsMap())
	}
	if f.Type.Pointer {
		id := ir.TableWireId(f.Type.Name)
		if st, ok := f.Type.Ref.(*ir.Struct); ok {
			id = ir.TableWireId(st.WireName())
		}
		blob := 0
		if f.Type.Blob() {
			id = ir.BlobWireTypeId(f)
			blob = 14
			if f.Type.Kind == ir.TString {
				blob = 12
			}
		}
		fmt.Fprintf(&b, ", PointerTypeId = 0x%016xul, BlobKind = %d", id, blob)
	}
	ownerType := "global::" + capitalize(g.unit.Package) + "." + g.owner.Name
	guards := tableGuardExprs(g.owner)
	if guard := guards[f.Name]; guard != "" {
		fmt.Fprintf(&b, ", WireGuard = delegate(object o) { var value = (%s)o; return %s; }", ownerType, guard)
	}
	reset := &tableGen{owner: g.owner, unit: g.unit}
	reset.emitTableResetField(f)
	fmt.Fprintf(&b, ", ResetField = delegate(object o) { var value = (%s)o; %s }", ownerType, strings.Join(strings.Fields(reset.schema.String()), " "))
	if len(f.DefBytes) > 0 {
		fmt.Fprintf(&b, ", DefaultBytes = new byte[] { %s }", byteLiterals(f.DefBytes))
	}
	if ir.TableKindWide(tableScalarKind(f)) {
		b.WriteString(wideColumns(f))
		return b.String()
	}
	if f.IsMap() || f.Type.Pointer || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes || isClassRef(f.Type) {
		return b.String()
	}
	defaultExpr := fieldDefaultExpr(f)
	if enumRef(f) != nil {
		defaultExpr = "global::" + capitalize(g.unit.Package) + "." + defaultExpr
	}
	fmt.Fprintf(&b, ", DefaultRaw = %s", csRawGet(defaultExpr, f.Type))
	kind := tableScalarKind(f)
	if kind == tkBool || enumRef(f) != nil {
		return b.String()
	}
	var c strings.Builder
	typ := csFieldType(f.Type)
	fmt.Fprintf(&c, "%s v = %s; ", typ, strings.TrimSuffix(strings.TrimPrefix(csRawSet("v", "raw", f.Type), "v = "), ";"))
	if f.HasIntRange {
		width := tableKindWidth(kind)
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		low, high := tableClampEnds(f, width)
		if low {
			fmt.Fprintf(&c, "if (v < %s) { r.Clamped++; v = %s; } ", csIntLit(f.IntMin, signed, width), csIntLit(f.IntMin, signed, width))
		}
		if high {
			fmt.Fprintf(&c, "if (v > %s) { r.Clamped++; v = %s; } ", csIntLit(f.IntMax, signed, width), csIntLit(f.IntMax, signed, width))
		}
	} else if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		fmt.Fprintf(&c, "if (v > %dul) { r.Clamped++; v = %d; } ", (uint64(1)<<f.Type.Width)-1, (uint64(1)<<f.Type.Width)-1)
	}
	if f.HasFloatRange {
		lit := formatFloat64
		if kind == tkF32 {
			lit = formatFloat32
		}
		fmt.Fprintf(&c, "if (v < %s) { r.Clamped++; v = %s; } else if (v > %s) { r.Clamped++; v = %s; } ", lit(f.FMin), lit(f.FMin), lit(f.FMax), lit(f.FMax))
	}
	fmt.Fprintf(&c, "return %s;", csRawGet("v", f.Type))
	fmt.Fprintf(&b, ", ClampRaw = delegate(ulong raw, TableReport r) { %s }", c.String())
	return b.String()
}

const tableWireSource = `// ---- form-1 table wire: begin ----
public static partial class TableWire
{
    public enum Verdict { Ok, Refused, Damaged, BodyStopped }

    // One schema-bounded vocabulary lives on the caller's stack for a save.
    // Collect follows the emitted fields in order. Measuring and writing then
    // look up stable references, so a nested length never needs patching.
    public ref struct Ids
    {
        public Span<ulong> Values;
        public Span<int> Slots;
        public Span<uint> OrdinalSlots;
        public Span<int> OrdinalOf;
        public int Count;
        internal Graph Graph;
        public Ids(Span<ulong> values)
        {
            Values = values;
            Slots = default;
            OrdinalSlots = default;
            OrdinalOf = default;
            Count = 0;
            Graph = null;
        }
        public Ids(Span<ulong> values, Span<int> slots, Span<uint> ordinalSlots, Span<int> ordinalOf)
        {
            Values = values;
            Slots = slots;
            if (!Slots.IsEmpty) { Slots.Clear(); }
            OrdinalSlots = ordinalSlots;
            if (!OrdinalSlots.IsEmpty) { OrdinalSlots.Clear(); }
            OrdinalOf = ordinalOf;
            if (!OrdinalOf.IsEmpty) { OrdinalOf.Fill(-1); }
            Count = 0;
            Graph = null;
        }
        int Slot(ulong id)
        {
            int mask = Slots.Length - 1;
            int slot = unchecked((int)(id ^ (id >> 32))) & mask;
            while (Slots[slot] != 0 && Values[Slots[slot] - 1] != id) { slot = (slot + 1) & mask; }
            return slot;
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public ulong Reference(ulong id)
        {
            if (!Slots.IsEmpty) { return (ulong)Slots[Slot(id)]; }
            for (int i = 0; i < Count; i++) { if (Values[i] == id) { return (ulong)i + 1; } }
            return 0;
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public ulong Ref(ulong id)
        {
            if (!Slots.IsEmpty)
            {
                int slot = Slot(id);
                if (Slots[slot] != 0) { return (ulong)Slots[slot]; }
                if (Count == Values.Length) { return 0; }
                Values[Count] = id;
                if (!OrdinalOf.IsEmpty && Count < OrdinalOf.Length) { OrdinalOf[Count] = -1; }
                Count++;
                Slots[slot] = Count;
                return (ulong)Count;
            }
            for (int i = 0; i < Count; i++) { if (Values[i] == id) { return (ulong)i + 1; } }
            if (Count == Values.Length) { return 0; }
            Values[Count] = id;
            if (!OrdinalOf.IsEmpty && Count < OrdinalOf.Length) { OrdinalOf[Count] = -1; }
            Count++;
            return (ulong)Count;
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public ulong RefAt(int ordinal, ulong id)
        {
            if (!OrdinalSlots.IsEmpty && (uint)ordinal < (uint)OrdinalSlots.Length)
            {
                uint s = OrdinalSlots[ordinal];
                if (s != 0) { return (ulong)s; }
            }
            return RefAtMiss(ordinal, id);
        }
        ulong RefAtMiss(int ordinal, ulong id)
        {
            ulong r = Ref(id);
            if (r != 0 && !OrdinalSlots.IsEmpty && (uint)ordinal < (uint)OrdinalSlots.Length)
            {
                OrdinalSlots[ordinal] = (uint)r;
                if (!OrdinalOf.IsEmpty && (int)(r - 1) < OrdinalOf.Length)
                {
                    OrdinalOf[(int)(r - 1)] = ordinal;
                }
            }
            return r;
        }
        public bool Add(ulong id)
        {
            if (!Slots.IsEmpty)
            {
                int slot = Slot(id);
                if (Slots[slot] != 0) { return true; }
                if (Count == Values.Length) { return false; }
                Values[Count] = id;
                if (!OrdinalOf.IsEmpty && Count < OrdinalOf.Length) { OrdinalOf[Count] = -1; }
                Count++;
                Slots[slot] = Count;
                return true;
            }
            if (Reference(id) != 0) { return true; }
            if (Count == Values.Length) { return false; }
            Values[Count] = id;
            if (!OrdinalOf.IsEmpty && Count < OrdinalOf.Length) { OrdinalOf[Count] = -1; }
            Count++;
            return true;
        }
        public void Truncate(int mark)
        {
            while (Count > 0 && Count > mark)
            {
                Count--;
                if (!Slots.IsEmpty)
                {
                    int slot = Slot(Values[Count]);
                    Slots[slot] = 0;
                }
                if (!OrdinalOf.IsEmpty && Count < OrdinalOf.Length)
                {
                    int o = OrdinalOf[Count];
                    if (o >= 0 && !OrdinalSlots.IsEmpty && (uint)o < (uint)OrdinalSlots.Length)
                    {
                        OrdinalSlots[o] = 0;
                    }
                    OrdinalOf[Count] = -1;
                }
            }
        }
    }

    public ref struct Writer
    {
        public Span<byte> Buffer;
        public int Offset;
        public Writer(Span<byte> buffer) { Buffer = buffer; Offset = 0; }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public void Byte(byte v) { Buffer[Offset++] = v; }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public void Raw(ReadOnlySpan<byte> v) { v.CopyTo(Buffer.Slice(Offset)); Offset += v.Length; }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public void Fixed(ulong v, int n)
        {
            switch (n)
            {
                case 1: Byte((byte)v); return;
                case 2: System.Buffers.Binary.BinaryPrimitives.WriteUInt16LittleEndian(Buffer.Slice(Offset), (ushort)v); break;
                case 4: System.Buffers.Binary.BinaryPrimitives.WriteUInt32LittleEndian(Buffer.Slice(Offset), (uint)v); break;
                case 8: System.Buffers.Binary.BinaryPrimitives.WriteUInt64LittleEndian(Buffer.Slice(Offset), v); break;
                default: for (int i = 0; i < n; i++) { Byte((byte)v); v >>= 8; } return;
            }
            Offset += n;
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public void Header(ulong reference, byte kind)
        {
            if (reference < 128)
            {
                System.Buffers.Binary.BinaryPrimitives.WriteUInt16LittleEndian(Buffer.Slice(Offset), (ushort)(reference | ((ulong)kind << 8)));
                Offset += 2;
                return;
            }
            Var(reference); Byte(kind);
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public void HeaderAt(int ordinal, ulong id, byte kind, ref Ids ids)
        {
            Header(ids.RefAt(ordinal, id), kind);
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public void Var(ulong v)
        {
            if (v < 128) { Byte((byte)v); return; }
            while (v >= 128) { Byte((byte)(v | 128)); v >>= 7; }
            Byte((byte)v);
        }
    }

    ref struct Reader
    {
        public ReadOnlySpan<byte> Buffer, Vocabulary;
        public Graph Graph;
        public int Offset;
        public Reader(ReadOnlySpan<byte> buffer, ReadOnlySpan<byte> vocabulary)
        { Buffer = buffer; Vocabulary = vocabulary; Offset = 0; Graph = null; }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public bool Has(int n) { return n >= 0 && n <= Buffer.Length - Offset; }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public byte Byte() { return Buffer[Offset++]; }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public ulong Fixed(int n)
        {
            ulong v;
            switch (n)
            {
                case 1: return Byte();
                case 2: v = System.Buffers.Binary.BinaryPrimitives.ReadUInt16LittleEndian(Buffer.Slice(Offset)); break;
                case 4: v = System.Buffers.Binary.BinaryPrimitives.ReadUInt32LittleEndian(Buffer.Slice(Offset)); break;
                case 8: v = System.Buffers.Binary.BinaryPrimitives.ReadUInt64LittleEndian(Buffer.Slice(Offset)); break;
                default:
                    v = 0;
                    for (int i = 0; i < n; i++) { v |= (ulong)Byte() << (8 * i); }
                    return v;
            }
            Offset += n;
            return v;
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public bool Var(out ulong v)
        {
            if ((uint)Offset < (uint)Buffer.Length)
            {
                byte b0 = Buffer[Offset];
                if (b0 < 128)
                {
                    Offset++;
                    v = b0;
                    return true;
                }
            }
            v = 0;
            int start = Offset;
            for (int i = 0; i < 10; i++)
            {
                if (!Has(1)) { Offset = start; return false; }
                byte b = Byte();
                if (i == 9 && b > 1) { Offset = start; return false; }
                v |= (ulong)(b & 127) << (7 * i);
                if (b < 128) { if (i == 0 || b != 0) { return true; } Offset = start; return false; }
            }
            Offset = start;
            return false;
        }
        [MethodImpl(MethodImplOptions.AggressiveInlining)]
        public bool Ref(out ulong reference, out ulong id)
        {
            id = 0;
            if (!Var(out reference) || reference > (ulong)(Vocabulary.Length / 8)) { return false; }
            if (reference != 0) { id = Read64(Vocabulary, (int)(reference - 1) * 8); }
            return true;
        }
        public bool Slice(out Reader sub)
        {
            sub = default;
            if (!Var(out ulong n) || n > (ulong)(Buffer.Length - Offset)) { return false; }
            sub = new Reader(Buffer.Slice(Offset, (int)n), Vocabulary) { Graph = Graph };
            Offset += (int)n;
            return true;
        }
        public bool Skip(byte kind)
        {
            int width = Width(kind);
            if (width != 0) { if (!Has(width)) { return false; } Offset += width; return true; }
            switch (kind)
            {
                case 17: return Var(out _);
                case 30: return Var(out _);
                case 12: case 13: case 14: case 16: case 31: case 32: case 33:
                    return Slice(out _);
                case 15:
                    if (!Var(out ulong arm)) { return false; }
                    if (arm == 0) { return true; }
                    if (!Has(1)) { return false; }
                    Byte();
                    return Slice(out _);
                default: return false;
            }
        }
    }

` + tableVariableWireSource + tableLoadMeasureSource + tableMapWireSource + tableMessageSource + tableMessageReadSource + tableMessageMeasureSource + tableNativeSource + `
    static ulong Read64(ReadOnlySpan<byte> bytes, int offset)
    {
        return System.Buffers.Binary.BinaryPrimitives.ReadUInt64LittleEndian(bytes.Slice(offset));
    }
    static int Width(byte k)
    {
        switch (k)
        {
            case 1: case 2: case 6: case 20: case 25: return 1;
            case 3: case 7: case 21: case 26: return 2;
            case 4: case 8: case 10: case 22: case 27: return 4;
            case 5: case 9: case 11: case 23: case 28: return 8;
            case 18: case 19: case 24: case 29: return 16;
            default: return 0;
        }
    }
    public static int VarSize(ulong v) { int n = 1; while (v >= 128) { n++; v >>= 7; } return n; }
    static byte Kind(TableFieldInfo f) { return f.KeyId != null ? (byte)16 : f.IsArray ? (byte)14 : f.Kind; }
    static bool Named(string name) { return name != null && name != "???"; }
    static bool DefaultRaw(ulong raw, TableFieldInfo f)
    {
        if (f.Kind == 10) { return TableBitsToFloat((uint)raw) == TableBitsToFloat((uint)f.DefaultRaw); }
        if (f.Kind == 11) { return TableBitsToDouble(raw) == TableBitsToDouble(f.DefaultRaw); }
        return raw == f.DefaultRaw;
    }
    static bool DefaultElement(object value, TableFieldInfo f, int i)
    {
        if (f.Kind == 17) { return f.GetChild(value, i) == null; }
        if (f.Kind == 13) { return Empty(f.GetChild(value, i), f.Table); }
        if (f.Kind == 15) { return f.Arms.GetTag(f.GetChild(value, i)) == 0; }
        if (f.GetWide != null) { return f.GetWide(value, i) == f.DefaultWide; }
        if (f.GetBuffer != null) { return f.GetBuffer(value)[i] == 0; }
        return DefaultRaw(f.GetRaw(value, i), f);
    }
    static bool Empty(object value, TableTypeInfo type)
    {
        foreach (TableFieldInfo f in type.Fields) { if (Rides(value, f)) { return false; } }
        return true;
    }
    static bool Rides(object value, TableFieldInfo f)
    {
        if (f.WireGuard != null && !f.WireGuard(value)) { return false; }
        if (f.Optional) { return f.GetPresent(value); }
        if (f.Counted) {
            int n = f.GetCount(value);
            if (f.DefaultBytes != null) { return n != f.DefaultBytes.Length || !f.GetBuffer(value).AsSpan(0, n).SequenceEqual(f.DefaultBytes); }
            return n != 0;
        }
        if (!f.IsArray) { return !DefaultElement(value, f, 0); }
        if (f.Kind == 13 && f.KeyId == null) { return true; }
        for (int i = 0; i < f.ArrayBound; i++) { if (!DefaultElement(value, f, i)) { return true; } }
        return false;
    }
    public static int Count(object value, TableFieldInfo f) { return f.Counted ? f.GetCount(value) : f.ArrayBound; }
    static bool CollectElement(object value, TableFieldInfo f, int i, ref Ids ids)
    {
        if (f.Kind == 13) { return Collect(f.GetChild(value, i), f.Table, ref ids); }
        if (f.Kind == 15)
        {
            object union = f.GetChild(value, i);
            ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { return true; }
            if (tag > (ulong)f.EnumMax || !Named(f.EnumName(tag))) { return false; }
            return ids.Add(f.VariantId(tag)) && CollectPayload(union, f.Arms.Arms[(int)tag].Field, ref ids);
        }
        if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value, i);
            if (v == 0) { return true; }
            return v <= (ulong)f.EnumMax && Named(f.EnumName(v)) && ids.Add(f.VariantId(v));
        }
        return true;
    }
    static bool CollectPayload(object value, TableFieldInfo f, ref Ids ids)
    {
        if (f == null) { return true; } // selected, payload-free arm
        int count = Count(value, f);
        if (f.Counted && (count < 0 || count > f.ArrayBound)) { return false; }
        if (!f.IsArray) { return CollectElement(value, f, 0, ref ids); }
        for (int i = 0; i < count; i++)
        {
            if (f.KeyId != null)
            {
                if (DefaultElement(value, f, i)) { continue; }
                if (!Named(f.KeyName((ulong)i + 1)) || !ids.Add(f.KeyId((ulong)i + 1))) { return false; }
            }
            if (!CollectElement(value, f, i, ref ids)) { return false; }
        }
        return true;
    }
    static bool Collect(object value, TableTypeInfo type, ref Ids ids)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (f.WireGuard != null && !f.WireGuard(value)) { continue; }
            if (f.Optional && !f.GetPresent(value)) { continue; }
            if (f.Counted && (Count(value, f) < 0 || Count(value, f) > f.ArrayBound)) { return false; }
            if (Rides(value, f) && (!ids.Add(f.Id) || !CollectPayload(value, f, ref ids))) { return false; }
        }
        return true;
    }
    static long PayloadSize(object value, TableFieldInfo f, ref Ids ids, scoped Span<long> elemCache = default)
    {
        if (f == null) { return 0; }
        if (f.IsArray) { return ArraySize(value, f, ref ids, elemCache); }
        if (f.Kind == 12 || f.Kind == 33) { return Count(value, f) * (f.Kind == 33 ? 2L : 1L); }
        return ElementSize(value, f, 0, ref ids, false);
    }
    static long ElementSize(object value, TableFieldInfo f, int i, ref Ids ids, bool framed)
    {
        if (f.Kind == 13)
        {
            long n = BodySize(f.GetChild(value, i), f.Table, ref ids);
            return n + (framed ? VarSize((ulong)n) : 0);
        }
        if (f.Kind == 15)
        {
            object union = f.GetChild(value, i);
            ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { return 1; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            long n = PayloadSize(union, arm.Field, ref ids);
            return VarSize(ids.Reference(f.VariantId(tag))) + 1 + VarSize((ulong)n) + n;
        }
        if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value, i);
            return VarSize(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        if (f.Kind == 17) { return VarSize(ids.Graph.Index(f.GetChild(value, i))); }
        return Width(f.Kind);
    }
    static long ArraySize(object value, TableFieldInfo f, ref Ids ids, scoped Span<long> elemCache = default)
    {
        long n = 0; int count = 0;
        int total = Count(value, f);
        for (int i = 0; i < total; i++)
        {
            if (f.KeyId != null)
            {
                if (DefaultElement(value, f, i)) { continue; }
                long elem = ElementSize(value, f, i, ref ids, false);
                n += VarSize(ids.Reference(f.KeyId((ulong)i + 1))) + VarSize((ulong)elem) + elem;
            }
            else if (f.Kind == 13)
            {
                long childBody = BodySize(f.GetChild(value, i), f.Table, ref ids);
                if (!elemCache.IsEmpty && i < elemCache.Length) { elemCache[i] = childBody; }
                n += VarSize((ulong)childBody) + childBody;
            }
            else { n += ElementSize(value, f, i, ref ids, true); }
            count++;
        }
        return 1 + VarSize((ulong)count) + n;
    }
    static long BodySize(object value, TableTypeInfo type, ref Ids ids, scoped Span<long> rootPayloadSizes = default, scoped Span<long> rootElemSizes = default)
    {
        long n = 1;
        int elemOffset = 0;
        for (int i = 0; i < type.Fields.Length; i++)
        {
            TableFieldInfo f = type.Fields[i];
            if (!Rides(value, f)) { continue; }
            n += VarSize(ids.Reference(f.Id)) + 1;
            if (f.IsArray || f.Kind == 12 || f.Kind == 33 || f.Kind == 13)
            {
                scoped Span<long> elemCache = default;
                if (f.IsArray && f.KeyId == null && f.Kind == 13 && !rootElemSizes.IsEmpty)
                {
                    int c = Count(value, f);
                    int take = Math.Min(c, Math.Max(0, rootElemSizes.Length - elemOffset));
                    elemCache = rootElemSizes.Slice(elemOffset, take);
                    elemOffset += take;
                }
                long payload = PayloadSize(value, f, ref ids, elemCache);
                n += VarSize((ulong)payload) + payload;
                if (!rootPayloadSizes.IsEmpty) { rootPayloadSizes[i] = payload; }
            }
            else { n += ElementSize(value, f, 0, ref ids, true); }
        }
        return n;
    }
    static void WriteElement(ref Writer w, object value, TableFieldInfo f, int i, ref Ids ids, bool framed)
    {
        if (f.Kind == 13)
        {
            object child = f.GetChild(value, i);
            if (framed) { w.Var((ulong)BodySize(child, f.Table, ref ids)); }
            WriteBody(ref w, child, f.Table, ref ids);
        }
        else if (f.Kind == 15)
        {
            object union = f.GetChild(value, i); ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { w.Var(0); return; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            w.Var(ids.Reference(f.VariantId(tag))); w.Byte(arm.Field == null ? (byte)32 : Kind(arm.Field));
            w.Var((ulong)PayloadSize(union, arm.Field, ref ids));
            WritePayload(ref w, union, arm.Field, ref ids);
        }
        else if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value, i);
            w.Var(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        else if (f.Kind == 17) { w.Var(ids.Graph.Index(f.GetChild(value, i))); }
        else if (f.GetWide != null) {
            UInt128 raw = f.GetWide(value, i); int width = Width(f.Kind);
            w.Fixed((ulong)raw, Math.Min(width, 8)); if (width == 16) { w.Fixed((ulong)(raw >> 64), 8); }
        }
        else { w.Fixed(f.GetBuffer != null ? f.GetBuffer(value)[i] : f.GetRaw(value, i), Width(f.Kind)); }
    }
    static void WritePayload(ref Writer w, object value, TableFieldInfo f, ref Ids ids, scoped ReadOnlySpan<long> elemCache = default)
    {
        if (f == null) { return; }
        if (f.IsArray)
        {
            w.Byte(f.Kind);
            int total = Count(value, f);
            int count = total;
            if (f.KeyId != null)
            {
                count = 0;
                for (int i = 0; i < f.ArrayBound; i++) { if (!DefaultElement(value, f, i)) { count++; } }
            }
            w.Var((ulong)count);
            for (int i = 0; i < total; i++)
            {
                if (f.KeyId != null)
                {
                    if (DefaultElement(value, f, i)) { continue; }
                    w.Var(ids.Reference(f.KeyId((ulong)i + 1)));
                    w.Var((ulong)ElementSize(value, f, i, ref ids, false));
                    WriteElement(ref w, value, f, i, ref ids, false);
                }
                else if (f.Kind == 13)
                {
                    object child = f.GetChild(value, i);
                    long childBody = (!elemCache.IsEmpty && i < elemCache.Length) ? elemCache[i] : BodySize(child, f.Table, ref ids);
                    w.Var((ulong)childBody);
                    WriteBody(ref w, child, f.Table, ref ids);
                }
                else
                {
                    WriteElement(ref w, value, f, i, ref ids, true);
                }
            }
        }
        else if (f.Kind == 33) { char[] chars = f.GetChars(value); int count = Count(value, f); for (int i = 0; i < count; i++) { w.Fixed(chars[i], 2); } }
        else if (f.Kind == 12) { w.Raw(f.GetBuffer(value).AsSpan(0, Count(value, f))); }
        else { WriteElement(ref w, value, f, 0, ref ids, false); }
    }
    static void WriteBody(ref Writer w, object value, TableTypeInfo type, ref Ids ids, bool root = false, scoped ReadOnlySpan<long> rootPayloadSizes = default, scoped ReadOnlySpan<long> rootElemSizes = default)
    {
        int elemOffset = 0;
        for (int i = 0; i < type.Fields.Length; i++)
        {
            TableFieldInfo f = type.Fields[i];
            if (!Rides(value, f)) { continue; }
            w.Header(ids.Reference(f.Id), Kind(f));
            scoped ReadOnlySpan<long> elemCache = default;
            if (f.IsArray && f.KeyId == null && f.Kind == 13 && !rootElemSizes.IsEmpty)
            {
                int c = Count(value, f);
                int take = Math.Min(c, Math.Max(0, rootElemSizes.Length - elemOffset));
                elemCache = rootElemSizes.Slice(elemOffset, take);
                elemOffset += take;
            }
            if (f.IsArray || f.Kind == 12 || f.Kind == 33 || f.Kind == 13)
            { w.Var((ulong)(rootPayloadSizes.IsEmpty ? PayloadSize(value, f, ref ids) : rootPayloadSizes[i])); }
            WritePayload(ref w, value, f, ref ids, elemCache);
        }
        if (root) { WriteNodes(ref w, ref ids); }
        w.Var(0);
    }
    public static bool CollectField(object value, TableFieldInfo f, ref Ids ids)
    {
        if (f.WireGuard != null && !f.WireGuard(value)) { return true; }
        if (f.Optional && !f.GetPresent(value)) { return true; }
        if (f.Counted && (Count(value, f) < 0 || Count(value, f) > f.ArrayBound)) { return false; }
        return !Rides(value, f) || (ids.Add(f.Id) && CollectPayload(value, f, ref ids));
    }
    public static long BodySizeField(object value, TableFieldInfo f, ref Ids ids, scoped Span<long> elemCache, out long payload)
    {
        payload = 0;
        if (!Rides(value, f)) { return 0; }
        long n = VarSize(ids.Reference(f.Id)) + 1;
        if (f.IsArray || f.Kind == 12 || f.Kind == 33 || f.Kind == 13)
        {
            payload = PayloadSize(value, f, ref ids, elemCache);
            n += VarSize((ulong)payload) + payload;
        }
        else
        {
            n += ElementSize(value, f, 0, ref ids, true);
        }
        return n;
    }
    public static long BodySizeField(object value, TableFieldInfo f, ref Ids ids, scoped Span<long> elemCache = default)
    {
        return BodySizeField(value, f, ref ids, elemCache, out _);
    }
    public static void WriteBodyField(ref Writer w, object value, TableFieldInfo f, ref Ids ids, scoped ReadOnlySpan<long> elemCache = default, long cachedPayload = -1)
    {
        if (!Rides(value, f)) { return; }
        w.Header(ids.Reference(f.Id), Kind(f));
        if (f.IsArray || f.Kind == 12 || f.Kind == 33 || f.Kind == 13)
        {
            long payload = cachedPayload >= 0 ? cachedPayload : PayloadSize(value, f, ref ids);
            w.Var((ulong)payload);
        }
        WritePayload(ref w, value, f, ref ids, elemCache);
    }
    public static long Save(object value, TableTypeInfo type, Span<byte> buffer, Span<ulong> vocabulary, bool measure)
    {
        // Preserve first-use vocabulary order; the index only accelerates
        // references during the repeated collect, measure and write walks.
        // Bound extra stack use for large schemas; their existing linear
        // path remains available without a per-save heap allocation.
        int slots = 0;
        if (vocabulary.Length <= 1024)
        {
            slots = 1;
            while (slots < vocabulary.Length * 2) { slots <<= 1; }
        }
        Span<int> index = stackalloc int[slots];
        Span<uint> ordinalSlots = vocabulary.Length <= 1024 ? stackalloc uint[vocabulary.Length] : default;
        Span<int> ordinalOf = vocabulary.Length <= 1024 ? stackalloc int[vocabulary.Length] : default;
        Ids ids = new Ids(vocabulary, index, ordinalSlots, ordinalOf);
        if (type.Variable) { ids.Graph = Number(value, type); if (ids.Graph == null) { return -1; } }
        if (!Collect(value, type, ref ids) || !CollectNodes(ref ids)) { return -1; }
        // Reuse the root's measured length prefixes while writing its fields,
        // and cache immediate child element body sizes for root unkeyed table arrays.
        // These ceilings bound additional stack storage to 2 KiB each (4 KiB total).
        // Roots with >256 fields bypass root payload sizing caching while element
        // caching remains active; element counts beyond 256 keep their cached prefix
        // with remaining elements falling back to on-demand sizing; variable graphs
        // and Measure retain the uncached path. Nested writes keep their own sizing
        // path, so no recursive cache or shared state lives across calls, and all
        // validation still precedes the first output byte.
        int cachedFields = !measure && !type.Variable && type.Fields.Length <= 256 ? type.Fields.Length : 0;
        Span<long> rootPayloadSizes = stackalloc long[cachedFields];
        int cachedElemSlots = !measure && !type.Variable ? type.RootElemSlots : 0;
        Span<long> rootElemSizes = stackalloc long[cachedElemSlots];
        long n = 1 + BodySize(value, type, ref ids, rootPayloadSizes, rootElemSizes) + 8L * ids.Count + 8;
        long nodes = NodesSize(ref ids);
        if (nodes != 0) { n += VarSize(ids.Reference(ulong.MaxValue)) + 1 + VarSize((ulong)nodes) + nodes; }
        if (measure) { return n; }
        if (n > buffer.Length) { return -1; }
        scoped Writer w = new Writer(buffer);
        w.Byte(1); WriteBody(ref w, value, type, ref ids, true, rootPayloadSizes, rootElemSizes);
        for (int i = 0; i < ids.Count; i++) { w.Fixed(ids.Values[i], 8); }
        w.Fixed((ulong)ids.Count, 8);
        return w.Offset;
    }

    static bool Widen(byte from, byte to)
    {
        return (from >= 2 && to <= 5 && from < to) || (from >= 6 && to <= 9 && from < to) || (from == 10 && to == 11) || (to == 18 && from >= 2 && from <= 5) || (to == 19 && from >= 6 && from <= 9);
    }
    // Numeric float conversion quiets signalling NaNs. Carry their sign and
    // payload by bits; ordinary finite values use the exact hardware widening.
    static ulong WidenFloat(uint raw)
    {
        if ((raw & 0x7f800000u) == 0x7f800000u)
        { return ((ulong)(raw & 0x80000000u) << 32) | 0x7ff0000000000000ul | ((ulong)(raw & 0x7fffffu) << 29); }
        return TableDoubleToBits((double)TableBitsToFloat(raw));
    }
    static bool Compatible(byte from, byte to, TableReport report)
    {
        if (from == to) { return true; }
        if (Widen(from, to)) { report.Widened++; return true; }
        report.KindMismatch++; return false;
    }
    static bool Damage(TableReport report) { report.Malformed = true; return false; }
    static bool EndsEarly(Reader r)
    {
        while (r.Ref(out ulong reference, out _))
        {
            if (reference == 0) { return r.Offset != r.Buffer.Length; }
            if (!r.Has(1) || !r.Skip(r.Byte())) { return false; }
        }
        return false;
    }
    static int FindVariant(TableFieldInfo f, ulong id, bool key)
    {
        int max = key ? f.ArrayBound : (int)f.EnumMax;
        for (int i = 1; i <= max; i++)
        {
            if (Named(key ? f.KeyName((ulong)i) : f.EnumName((ulong)i)) &&
                (key ? f.KeyId((ulong)i) : f.VariantId((ulong)i)) == id) { return i; }
        }
        return 0;
    }
    static bool ReadScalar(ref Reader r, object value, TableFieldInfo f, int index, byte kind, TableReport report)
    {
        if (f.GetWide != null)
        {
            int width = Width(kind);
            if (!r.Has(width)) { return Damage(report); }
            UInt128 wide = r.Fixed(Math.Min(width, 8));
            if (width == 16) { wide |= (UInt128)r.Fixed(8) << 64; }
            else if ((kind >= 2 && kind <= 5) || (kind >= 20 && kind <= 23))
            { int shift = 128 - width * 8; wide = unchecked((UInt128)((Int128)(wide << shift) >> shift)); }
            if (f.ClampWide != null) { wide = f.ClampWide(wide, report); }
            f.SetWide(value, index, wide); return true;
        }
        ulong raw;
        if (kind == 30)
        {
            if (!r.Ref(out ulong reference, out ulong id)) { return Damage(report); }
            int variant = reference == 0 ? 0 : FindVariant(f, id, false);
            if (reference != 0 && variant == 0) { report.Unknown++; }
            raw = (ulong)variant;
        }
        else
        {
            int width = Width(kind);
            if (!r.Has(width)) { return Damage(report); }
            raw = r.Fixed(width);
            if (kind >= 2 && kind <= 5 && width < 8)
            { int shift = 64 - width * 8; raw = unchecked((ulong)((long)(raw << shift) >> shift)); }
            if (kind == 1) { raw = raw == 0 ? 0ul : 1ul; }
            if (kind == 10 && f.Kind == 11) { raw = WidenFloat((uint)raw); }
            if (f.ClampRaw != null) { raw = f.ClampRaw(raw, report); }
        }
        if (f.GetBuffer != null) { f.GetBuffer(value)[index] = (byte)raw; }
        else { f.SetRaw(value, index, raw); }
        return true;
    }
    static bool ReadElement(ref Reader r, object value, TableFieldInfo f, int index, byte kind, TableReport report, bool framed)
    {
        if (kind == 17)
        {
            if (!r.Var(out ulong indexValue)) { return Damage(report); }
            f.SetChild(value, index, r.Graph == null ? null : r.Graph.Resolve(indexValue, f, report)); return true;
        }
        if (kind == 13)
        {
            Reader sub = r;
            if (framed && !r.Slice(out sub)) { return Damage(report); }
            object child = f.GetChild(value, index);
            ReadBody(ref sub, child, f.Table, report, true);
            if (sub.Offset != sub.Buffer.Length) { f.Table.Reset(child); Damage(report); }
            if (!framed) { r.Offset = r.Buffer.Length; }
            return true;
        }
        if (kind == 15)
        {
            if (!r.Var(out ulong reference)) { return Damage(report); }
            object union = f.GetChild(value, index);
            // Array elements establish None as soon as their reference is
            // present, even if its index or the following arm header is bad.
            // A scalar union field retains its old selection until the whole
            // header is bounded, matching the C++ reader under repeated fields.
            if (f.IsArray) { f.Arms.SetTag(union, 0); }
            if (reference == 0) { f.Arms.SetTag(union, 0); return true; }
            if (reference > (ulong)r.Vocabulary.Length / 8) { return Damage(report); }
            ulong id = Read64(r.Vocabulary, ((int)reference - 1) * 8);
            if (!r.Has(1)) { return Damage(report); }
            byte armKind = r.Byte();
            if (!r.Slice(out Reader arm)) { return Damage(report); }
            f.Arms.SetTag(union, 0);
            int tag = FindVariant(f, id, false);
            if (tag == 0) { report.Unknown++; return true; }
            TableUnionArmInfo info = f.Arms.Arms[tag];
            byte expected = info.Field == null ? (byte)32 : Kind(info.Field);
            // A widening arm must first have exactly the source kind's width.
            // Damaged framing never records a successful widening event.
            if (Widen(armKind, expected) && arm.Buffer.Length != Width(armKind))
            { Damage(report); return true; }
            if (!Compatible(armKind, expected, report)) { return true; }
            f.Arms.SetTag(union, (ulong)tag);
            if (!ReadArm(ref arm, union, info.Field, armKind, report)) { f.Arms.SetTag(union, 0); }
            return true;
        }
        return ReadScalar(ref r, value, f, index, kind, report);
    }
    // Selection establishes storage. Table bodies take their declared defaults;
    // general arms are zeroed, including unused tails of arrays of plain types.
    internal static void ResetArm(object value, TableFieldInfo f)
    {
        if (f.Kind == 13 && !f.IsArray) { f.Table.Reset(f.GetChild(value, 0)); }
        else { ZeroField(value, f); }
    }
    static void ZeroField(object value, TableFieldInfo f)
    {
        if (f.Dynamic) { f.ResetField(value); return; }
        if (f.GetChars != null) { Array.Clear(f.GetChars(value), 0, f.ArrayBound); }
        else if (f.GetBuffer != null) { Array.Clear(f.GetBuffer(value), 0, f.ArrayBound); }
        else
        {
            int n = f.IsArray ? f.ArrayBound : 1;
            for (int i = 0; i < n; i++)
            {
                if (f.Kind == 13)
                {
                    object child = f.GetChild(value, i);
                    foreach (TableFieldInfo field in f.Table.Fields) { ZeroField(child, field); }
                }
                else if (f.Kind == 15) { ResetUnion(f.GetChild(value, i), f.Arms); }
                else if (f.Kind == 17) { f.SetChild(value, i, null); }
                else if (f.SetWide != null) { f.SetWide(value, i, 0); }
                else { f.SetRaw(value, i, 0); }
            }
        }
        if (f.Counted) { f.SetCount(value, 0); }
        if (f.Optional) { f.SetPresent(value, false); }
    }
    // A shorter replacement, including a damaged one, must restore slots above
    // the new count to the value-initialized element (#725).
    static void ResetUnion(object union, TableUnionInfo arms)
    {
        if (union == null || arms == null) { return; }
        arms.SetTag(union, 0);
        if (arms.Arms == null) { return; }
        // C++ assigns a fresh element; C memsets. Tag-only leaves the previous
        // arm's storage (a list under None). Reset every payload arm.
        for (int a = 1; a < arms.Arms.Length; a++)
        {
            TableFieldInfo payload = arms.Arms[a].Field;
            if (payload != null) { ResetArm(union, payload); }
        }
    }
    static void ResetElement(object value, TableFieldInfo f, int i)
    {
        if (f.Kind == 13 && f.Table != null) { object child = f.GetChild(value, i); if (child != null) { f.Table.Reset(child); } }
        else if (f.Kind == 15 && f.Arms != null) { ResetUnion(f.GetChild(value, i), f.Arms); }
        else if (f.Kind == 17) { f.SetChild(value, i, null); }
        else if (f.SetWide != null) { f.SetWide(value, i, 0); }
        else if (f.SetRaw != null) { f.SetRaw(value, i, f.DefaultRaw); }
    }
    static void ResetCountedTail(object value, TableFieldInfo f, int previous, int decoded)
    {
        if (value == null || f.SetCount == null) { return; }
        int end = previous;
        if (end > f.ArrayBound) { end = f.ArrayBound; }
        for (int i = decoded; i < end; i++) { ResetElement(value, f, i); }
        f.SetCount(value, decoded);
    }
    static bool ReadArm(ref Reader r, object value, TableFieldInfo f, byte kind, TableReport report)
    {
        if (f == null) { return r.Buffer.Length == 0 || Damage(report); }
        if (f.Map) { return ReadMapBody(ref r, value, f, report); }
        int width = Width(kind);
        if (width != 0 && r.Buffer.Length != width) { return Damage(report); }
        if (kind == 13)
        {
            ReadBody(ref r, f.GetChild(value, 0), f.Table, report, true);
            return r.Offset == r.Buffer.Length || Damage(report);
        }
        if (kind == 15) { return ReadElement(ref r, value, f, 0, kind, report, true); }
        if (kind == 17)
        {
            if (!r.Var(out ulong indexValue) || r.Offset != r.Buffer.Length) { return Damage(report); }
            f.SetChild(value, 0, r.Graph == null ? null : r.Graph.Resolve(indexValue, f, report));
            return true;
        }
        if (kind == 12 || kind == 33)
        {
            ResetArm(value, f);
            return kind == 33 ? ReadChars(r.Buffer, value, f, report) : ReadText(r.Buffer, value, f, report);
        }
        if (kind == 14)
        {
            ResetArm(value, f);
            if (!r.Has(2)) { return true; } // an inert short arm still selects
            byte elementKind = r.Byte();
            if (!r.Var(out ulong count)) { return Damage(report); }
            if (!Compatible(elementKind, f.Kind, report)) { return false; }
            int keep = (int)Math.Min(count, (ulong)f.ArrayBound);
            if (!f.Dynamic && count > (ulong)f.ArrayBound) { report.Clamped++; }
            int decoded = 0;
            for (int i = 0; i < keep; i++)
            {
                if (f.Dynamic) { if (!r.Has(1)) { Damage(report); break; } f.EnsureCount(value, i + 1); }
                if (!ReadElement(ref r, value, f, i, elementKind, report, true)) { break; }
                decoded++;
            }
            if (f.Counted) { f.SetCount(value, decoded); }
            return true; // damaged elements preserve the selected arm's prefix
        }
        if (!ReadScalar(ref r, value, f, 0, kind, report)) { return false; }
        return r.Offset == r.Buffer.Length || Damage(report);
    }
    static bool ReadText(ReadOnlySpan<byte> text, object value, TableFieldInfo f, TableReport report)
    {
        if (!TextValid(text)) { return Damage(report); }
        int keep = text.Length;
        if (keep > f.ArrayBound)
        {
            keep = f.ArrayBound; report.Clamped++;
            while (keep > 0 && (text[keep] & 0xc0) == 0x80) { keep--; }
        }
        if(value!=null) { text.Slice(0,keep).CopyTo(f.GetBuffer(value)); f.SetCount(value,keep); }
        return true;
    }
    static bool ReadChars(ReadOnlySpan<byte> text, object value, TableFieldInfo f, TableReport report)
    {
        if ((text.Length & 1) != 0) { return Damage(report); }
        for (int i = 0; i < text.Length; i += 2)
        {
            int c = text[i] | text[i + 1] << 8;
            if (c == 0 || (c >= 0xdc00 && c <= 0xdfff)) { return Damage(report); }
            if (c >= 0xd800 && c <= 0xdbff)
            {
                if (i + 3 >= text.Length) { return Damage(report); }
                int low = text[i + 2] | text[i + 3] << 8;
                if (low < 0xdc00 || low > 0xdfff) { return Damage(report); }
                i += 2;
            }
        }
        int keep = text.Length / 2;
        if (keep > f.ArrayBound)
        {
            keep = f.ArrayBound; report.Clamped++;
            if (keep > 0 && (text[keep * 2 - 1] & 0xfc) == 0xd8) { keep--; }
        }
        char[] storage = f.GetChars(value);
        for (int i = 0; i < keep; i++) { storage[i] = (char)(text[i * 2] | text[i * 2 + 1] << 8); }
        f.SetCount(value, keep); return true;
    }
    internal static bool TextValid(ReadOnlySpan<byte> text)
    {
        for (int i = 0; i < text.Length;)
        {
            byte c = text[i++];
            if (c == 0) { return false; }
            if (c < 128) { continue; }
            int n; uint v, min;
            if (c >= 0xc2 && c <= 0xdf) { n = 1; v = (uint)(c & 31); min = 0x80; }
            else if (c >= 0xe0 && c <= 0xef) { n = 2; v = (uint)(c & 15); min = 0x800; }
            else if (c >= 0xf0 && c <= 0xf4) { n = 3; v = (uint)(c & 7); min = 0x10000; }
            else { return false; }
            if (n > text.Length - i) { return false; }
            for (int j = 0; j < n; j++)
            { byte b = text[i++]; if ((b & 0xc0) != 0x80) { return false; } v = (v << 6) | (uint)(b & 63); }
            if (v < min || v > 0x10ffff || (v >= 0xd800 && v <= 0xdfff)) { return false; }
        }
        return true;
    }
    static bool ReadField(ref Reader r, object value, TableFieldInfo f, byte kind, TableReport report, out bool placed)
    {
        placed = true;
        if (f.Map) { if (!r.Slice(out Reader map)) { return Damage(report); } return ReadMapBody(ref map, value, f, report, r.Buffer.Slice(r.Offset - map.Buffer.Length)); }
        if (kind == 12 || kind == 33)
        {
            if (!r.Slice(out Reader text)) { return Damage(report); }
            if (!(kind == 33 ? ReadChars(text.Buffer, value, f, report) : ReadText(text.Buffer, value, f, report)))
            { f.ResetField(value); }
            return true;
        }
        if (kind == 14 || kind == 16)
        {
            if (!r.Slice(out Reader array)) { return Damage(report); }
            if (array.Buffer.Length < 2) { return true; } // inert, including under a repeat
            byte elementKind = array.Byte();
            // Match the reference's header cursor: the count is read at the
            // enclosing cursor, then all elements are bounded by the array's L.
            // A count spilling past L can clamp, but cannot invent an element.
            Reader header = new Reader(r.Buffer.Slice(r.Offset - array.Buffer.Length + 1), r.Vocabulary);
            if (!header.Var(out ulong count)) { Damage(report); return true; }
            array.Offset = Math.Min(array.Buffer.Length, 1 + header.Offset);
            if (!Compatible(elementKind, f.Kind, report)) { placed = false; return true; }
            if (kind == 16)
            {
                for (ulong i = 0; i < count; i++)
                {
                    if (!array.Ref(out ulong reference, out ulong id) || reference == 0 || !array.Slice(out Reader element))
                    { Damage(report); break; }
                    int slot = FindVariant(f, id, true);
                    if (slot == 0) { report.Unknown++; continue; }
                    ReadElement(ref element, value, f, slot - 1, elementKind, report, false);
                }
            }
            else
            {
                int keep = (int)Math.Min(count, (ulong)f.ArrayBound);
                if (!f.Dynamic && count > (ulong)f.ArrayBound) { report.Clamped++; }
                int previous = f.Counted && f.GetCount != null ? f.GetCount(value) : 0;
                int decoded = 0;
                for (int i = 0; i < keep; i++)
                {
                    if (f.Dynamic) { if (!array.Has(1)) { Damage(report); break; } f.EnsureCount(value, i + 1); }
                    if (!ReadElement(ref array, value, f, i, elementKind, report, true)) { break; }
                    decoded++;
                }
                if (f.Counted) { ResetCountedTail(value, f, previous, decoded); }
            }
            return true;
        }
        return ReadElement(ref r, value, f, 0, kind, report, true);
    }
    // IndexFields builds a power-of-two open-addressing table over Fields using
    // the same mix as Ids.Slot. Nested static TableType() Build() calls it
    // before publishing Instance, so ARM64 sees the index with the descriptor.
    internal static void IndexFields(TableTypeInfo type)
    {
        TableFieldInfo[] fields = type.Fields;
        if (fields == null || fields.Length == 0)
        {
            type.IdIndex = Array.Empty<TableFieldInfo>();
            type.IdMask = 0;
            return;
        }
        int slots = 1;
        while (slots < fields.Length * 2) { slots <<= 1; }
        TableFieldInfo[] index = new TableFieldInfo[slots];
        int mask = slots - 1;
        for (int i = 0; i < fields.Length; i++)
        {
            TableFieldInfo f = fields[i];
            int slot = unchecked((int)(f.Id ^ (f.Id >> 32))) & mask;
            while (index[slot] != null) { slot = (slot + 1) & mask; }
            index[slot] = f;
        }
        type.IdIndex = index;
        type.IdMask = mask;
    }
    internal static TableFieldInfo FindField(TableTypeInfo type, ulong id)
    {
        TableFieldInfo[] index = type.IdIndex;
        if (index != null)
        {
            if (index.Length == 0) { return null; }
            int mask = type.IdMask;
            int slot = unchecked((int)(id ^ (id >> 32))) & mask;
            for (;;)
            {
                TableFieldInfo f = index[slot];
                if (f == null) { return null; }
                if (f.Id == id) { return f; }
                slot = (slot + 1) & mask;
            }
        }
        if (type.Fields == null) { return null; }
        foreach (TableFieldInfo f in type.Fields) { if (f.Id == id) { return f; } }
        return null;
    }
    static bool ReadBody(ref Reader r, object value, TableTypeInfo type, TableReport report, bool nested)
    {
        // Load resets the root before even the framing checks. Every nested
        // occurrence still replaces its target, including repeated fields.
        if (nested) { type.Reset(value); }
        for (;;)
        {
            if (!r.Ref(out ulong reference, out ulong id)) { return Damage(report); }
            if (reference == 0) { return true; }
            if ((nested && id == ulong.MaxValue) || id == ulong.MaxValue - 1 || id == ulong.MaxValue - 2) { return Damage(report); }
            if (!r.Has(1)) { return Damage(report); }
            byte kind = r.Byte();
            TableFieldInfo field = FindField(type, id);
            if (!nested && id == ulong.MaxValue && type.Variable)
            { if (!r.Skip(kind)) { return Damage(report); } continue; }
            if (field == null)
            {
                report.Unknown++;
                if (!r.Skip(kind)) { return Damage(report); }
                continue;
            }
            int beforeWidened = report.Widened;
            if (Widen(kind,Kind(field)) && !r.Has(Width(kind))) { return Damage(report); }
            if (!Compatible(kind, Kind(field), report))
            {
                if (!r.Skip(kind)) { return Damage(report); }
                continue;
            }
            if (field.MapKey) { report.Widened = beforeWidened; }
            if (!ReadField(ref r, value, field, kind, report, out bool placed)) { return false; }
            if (field.Optional && placed) { field.SetPresent(value, true); }
        }
    }
    static bool DistinctVocabulary(ReadOnlySpan<byte> vocabulary)
    {
        int count = vocabulary.Length / 8;
        if (count >= 16 && count <= 256)
        {
            // One KiB, independent of the input's claimed count. Slots store
            // ordinal+1, so the identity zero remains an ordinary identity.
            // At most 256 entries occupy 512 slots: even deliberately colliding
            // identities reach an empty slot after at most 256 occupied probes.
            Span<ushort> slots = stackalloc ushort[512];
            slots.Clear();
            for (int i = 0; i < count; i++)
            {
                ulong id = Read64(vocabulary, i * 8);
                int slot = (int)(unchecked(id * 0x9e3779b97f4a7c15ul) >> 55);
                while (slots[slot] != 0)
                {
                    if (id == Read64(vocabulary, (slots[slot] - 1) * 8)) { return false; }
                    slot = (slot + 1) & 511;
                }
                slots[slot] = (ushort)(i + 1);
            }
            return true;
        }
        // Tiny vocabularies avoid scratch initialization. Larger foreign
        // vocabularies keep the existing allocation-free validation path.
        for (int i = 0; i < vocabulary.Length; i += 8)
        {
            ulong id = Read64(vocabulary, i);
            for (int j = 0; j < i; j += 8)
            { if (id == Read64(vocabulary, j)) { return false; } }
        }
        return true;
    }
    static Verdict Finish(TableReport report, Verdict verdict) { report.Verdict = verdict; return verdict; }
    public static Verdict Load(object value, TableTypeInfo type, ReadOnlySpan<byte> bytes, TableReport report)
    {
        if (report == null) { report = new TableReport(); }
        type.Reset(value);
        report.Refused = false; report.Reason = null;
        if (bytes.Length == 0) { Damage(report); return Finish(report, Verdict.Damaged); }
        if (bytes[0] != 1)
        {
            report.Refused = true;
            report.Reason = bytes[0] == 2 ? "message_form_as_file" : "newer_form";
            return Finish(report, Verdict.Refused);
        }
        if (bytes.Length < 9) { Damage(report); return Finish(report, Verdict.Damaged); }
        ulong count = Read64(bytes, bytes.Length - 8);
        if (count > (ulong)(bytes.Length - 9) / 8) { Damage(report); return Finish(report, Verdict.Damaged); }
        int end = bytes.Length - 8 - (int)count * 8;
        ReadOnlySpan<byte> vocabulary = bytes.Slice(end, (int)count * 8);
        if (!DistinctVocabulary(vocabulary)) { Damage(report); return Finish(report, Verdict.Damaged); }
        Reader r = new Reader(bytes.Slice(1, end - 1), vocabulary);
        if (EndsEarly(r)) { Damage(report); return Finish(report, Verdict.Damaged); }
        if (type.Variable) { if (LoadMeasure(type, bytes, out _) < 0) { Damage(report); return Finish(report, Verdict.Damaged); } r.Graph = ReadGraph(r, value, type, report); }
        return Finish(report, ReadBody(ref r, value, type, report, false) ? Verdict.Ok : Verdict.BodyStopped);
    }
}
// ---- form-1 table wire: end ----
`
