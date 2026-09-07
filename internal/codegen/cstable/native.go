package cstable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) nativeColumns(f *ir.Field) string {
	at := int64(0)
	if !g.arm {
		if field := ir.RecordLayout(g.unit, g.owner).FieldByName(f.Name); field != nil {
			at = field.Offset
		}
	}
	pieces := ir.FieldPieces(g.unit, f, at)
	count, present := int64(-1), int64(-1)
	if f.Type.Optional {
		present = pieces[len(pieces)-1].Offset
	}
	if !f.IsList() && !f.IsMap() && (f.CountedOnWire() || !f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes)) {
		count = pieces[1].Offset
	}
	element := pieces[0].Size
	if f.IsList() || f.IsMap() {
		count = at + 8
		if f.IsMap() {
			element = ir.RecordLayout(g.unit, f.MapEntry).Size
		} else {
			element, _ = ir.ListElementLayout(g.unit, f)
		}
	} else if f.Array != ir.ArrayNone && f.ArrayBound > 0 {
		element /= f.ArrayBound
	}
	return fmt.Sprintf(", NativeOffset = %d, NativeElementSize = %d, NativeCountOffset = %d, NativePresentOffset = %d", at, element, count, present)
}

func (g *tableGen) emitNativeSurface(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	n := st.Name
	g.pf("public static long %sCookMeasure(%s value) { return TableWire.Cook(value, %sTableType(), Span<byte>.Empty, TableByteOrder.Little, true); }\n", n, n, n)
	g.pf("public static bool %sCook(%s value, Span<byte> bytes, TableByteOrder order = TableByteOrder.Little) { return TableWire.Cook(value, %sTableType(), bytes, order, false) >= 0; }\n\n", n, n, n)
}

const tableNativeSource = `
    static bool NativeExtent(TableTypeInfo type)
    { foreach (TableFieldInfo f in type.Fields) { if (NativeExtent(f)) { return true; } } return false; }
    static bool NativeExtent(TableFieldInfo f)
    {
        if (f.Dynamic) { return true; }
        if (f.Kind == 13) { return NativeExtent(f.Table); }
        if (f.Kind == 15)
        { foreach (TableUnionArmInfo arm in f.Arms.Arms) { if (arm.Field != null && NativeExtent(arm.Field)) { return true; } } }
        return false;
    }
    ref struct NativeWriter
    {
        public Span<byte> Bytes;
        public bool Measure, Big;
        public Graph Graph;
        public long Base;
        public NativeWriter(Span<byte> bytes, bool measure, bool big, Graph graph)
        { Bytes = bytes; Measure = measure; Big = big; Graph = graph; Base = 0; }
        public void Put(long at, UInt128 raw, int width)
        {
            if (Measure) { return; }
            for (int i = 0; i < width; i++) { Bytes[checked((int)(Base + at + (Big ? width - 1 - i : i)))] = (byte)(raw >> (i * 8)); }
        }
        public void Raw(long at, ReadOnlySpan<byte> bytes)
        { if (!Measure) { bytes.CopyTo(Bytes.Slice(checked((int)(Base + at)))); } }
    }
    static bool NativeRecord(ref NativeWriter w, long at, object value, TableTypeInfo type, long extentAt, ref long extent)
    {
        foreach (TableFieldInfo f in type.Fields)
        { if (!NativeField(ref w, at, value, f, extentAt, ref extent)) { return false; } }
        return true;
    }
    static bool NativeField(ref NativeWriter w, long record, object value, TableFieldInfo f, long extentAt, ref long extent)
    {
        long at = record + f.NativeOffset;
        if (f.Optional) { w.Put(record + f.NativePresentOffset, f.GetPresent(value) ? 1u : 0u, 1); }
        int live = f.Counted ? f.GetCount(value) : f.IsArray ? f.ArrayBound : 1;
        if (live < 0 || f.Counted && live > f.ArrayBound) { return false; }
        if (f.NativeCountOffset >= 0) { w.Put(record + f.NativeCountOffset, (uint)live, 4); }
        if (f.Kind == 12)
        { w.Raw(at, f.GetBuffer(value).AsSpan(0, live)); return true; }
        if (f.Kind == 33)
        { for (int i = 0; i < live; i++) { w.Put(at + i * 2L, f.GetChars(value)[i], 2); } return true; }
        int count = f.IsArray ? f.Dynamic ? live : f.ArrayBound : 1;
        if (f.GetBuffer != null)
        { w.Raw(at, f.GetBuffer(value).AsSpan(0, live)); return true; }
        if (f.Dynamic)
        {
            extent = Align(extent, f.StorageAlign);
            long start = extentAt + extent;
            extent += (long)count * f.NativeElementSize;
            w.Put(at, count == 0 ? 0 : unchecked((ulong)(start - at)), 8); at = start;
        }
        for (int i = 0; i < count; i++)
        {
            long element = at + (long)i * f.NativeElementSize, before = extent;
            if (!NativeElement(ref w, element, value, f, i, extentAt, ref extent)) { return false; }
            if (!f.Dynamic && f.Counted && i >= live && extent != before) { return false; }
        }
        return true;
    }
    static bool NativeElement(ref NativeWriter w, long at, object value, TableFieldInfo f, int index, long extentAt, ref long extent)
    {
        if (f.Kind == 17)
        {
            object target = f.GetChild(value, index); long offset = 0;
            if (target != null)
            {
                if (w.Graph == null || !w.Graph.Indices.TryGetValue(target, out ulong node)) { return false; }
                offset = (node == 1 ? 0 : w.Graph.Nodes[(int)node - 2].NativeOffset) - at;
            }
            w.Put(at, unchecked((ulong)offset), 8); return true;
        }
        if (f.Kind == 13) { return NativeRecord(ref w, at, f.GetChild(value, index), f.Table, extentAt, ref extent); }
        if (f.Kind == 15)
        {
            object union = f.GetChild(value, index); ulong tag = f.Arms.GetTag(union);
            if (tag > (ulong)f.EnumMax) { return false; }
            w.Put(at, tag, f.Arms.NativeTagSize);
            TableFieldInfo arm = f.Arms.Arms[(int)tag].Field;
            return arm == null || NativeField(ref w, at + f.Arms.NativeArmOffset, union, arm, extentAt, ref extent);
        }
        UInt128 raw = f.GetWide != null ? f.GetWide(value, index) : f.GetRaw(value, index);
        w.Put(at, raw, f.NativeElementSize); return true;
    }
    static long NativeRecordBytes(TableTypeInfo type)
    { return NativeExtent(type) ? Align(type.StorageSize, 8) : type.StorageSize; }
    static long NativePlace(object value, TableTypeInfo type, Graph graph, out int alignment)
    {
        alignment = Math.Max(8, type.StorageAlign);
        NativeWriter probe = new NativeWriter(default, true, false, graph);
        long extent = 0, rootBytes = NativeRecordBytes(type);
        // Placement precedes validation of references: later nodes have no
        // addresses yet. This pass sizes only each holder's container extent.
        if (!NativeExtentSize(value, type, ref extent)) { return -1; }
        long at = rootBytes + extent;
        if (graph != null)
        {
            foreach (Node node in graph.Nodes)
            {
                int align = node.BlobKind != 0 ? 8 : node.Type.StorageAlign;
                alignment = Math.Max(alignment, align); at = Align(at, align); node.NativeOffset = at;
                if (node.BlobKind != 0) { at += 8 + ((TableBlob)node.Value).Data.Length + (node.BlobKind == 12 ? 1 : 0); }
                else
                {
                    extent = 0;
                    if (!NativeExtentSize(node.Value, node.Type, ref extent)) { return -1; }
                    at += NativeRecordBytes(node.Type) + extent;
                }
            }
        }
        // Check every stored pointer, including slots outside a live count,
        // before any caller output is touched.
        extent = 0;
        if (!NativeRecord(ref probe, 0, value, type, rootBytes, ref extent)) { return -1; }
        if (graph != null)
        {
            foreach (Node node in graph.Nodes)
            {
                if (node.BlobKind != 0) { continue; }
                extent = 0;
                if (!NativeRecord(ref probe, node.NativeOffset, node.Value, node.Type, node.NativeOffset + NativeRecordBytes(node.Type), ref extent)) { return -1; }
            }
        }
        return Align(at, alignment);
    }
    static bool NativeExtentSize(object value, TableTypeInfo type, ref long at)
    { foreach (TableFieldInfo f in type.Fields) { if (!NativeExtentSize(value, f, ref at)) { return false; } } return true; }
    static bool NativeExtentSize(object value, TableFieldInfo f, ref long at)
    {
        if (f.Dynamic)
        {
            int count = f.GetCount(value); if (count < 0) { return false; }
            at = Align(at, f.StorageAlign) + (long)count * f.StorageSize;
        }
        int n = f.IsArray ? Count(value, f) : 1;
        if (n < 0 || n > f.ArrayBound && f.IsArray) { return false; }
        for (int i = 0; i < n; i++)
        {
            if (f.Kind == 13) { if (!NativeExtentSize(f.GetChild(value, i), f.Table, ref at)) { return false; } }
            else if (f.Kind == 15)
            {
                object union = f.GetChild(value, i); ulong tag = f.Arms.GetTag(union);
                if (tag > (ulong)f.EnumMax) { return false; }
                TableFieldInfo arm = f.Arms.Arms[(int)tag].Field;
                if (arm != null && !NativeExtentSize(union, arm, ref at)) { return false; }
            }
        }
        return true;
    }
    public static long Cook(object value, TableTypeInfo type, Span<byte> bytes, TableByteOrder order, bool measure)
    {
        if (value == null || order != TableByteOrder.Little && order != TableByteOrder.Big) { return -1; }
        Graph graph = type.Variable ? Number(value, type) : null;
        if (type.Variable && graph == null) { return -1; }
        long data = NativePlace(value, type, graph, out int alignment);
        if (data < 0) { return -1; }
        long nodes = graph == null ? 1 : graph.Nodes.Count + 1;
        long size = 64 + data + nodes * 16;
        if (measure) { return size; }
        if (size > bytes.Length) { return -1; }
        bytes.Slice(0, (int)size).Clear();
        NativeWriter w = new NativeWriter(bytes, false, order == TableByteOrder.Big, graph);
        w.Put(0, 0x4b4f4f434d484353ul, 8); w.Put(8, tableOwnVocabulary.BuildVersion, 8);
        w.Put(16, (uint)order, 8); w.Put(24, (ulong)data, 8); w.Put(32, (ulong)nodes * 16, 8); w.Put(40, (uint)alignment, 8);
        w.Base = 64; long extent = 0;
        if (!NativeRecord(ref w, 0, value, type, NativeRecordBytes(type), ref extent)) { return -1; }
        w.Put(data, 0, 8); w.Put(data + 8, type.Id, 8);
        if (graph != null)
        {
            for (int i = 0; i < graph.Nodes.Count; i++)
            {
                Node node = graph.Nodes[i];
                if (node.BlobKind != 0)
                { byte[] raw = ((TableBlob)node.Value).Data; w.Put(node.NativeOffset, (uint)raw.Length, 4); w.Raw(node.NativeOffset + 8, raw); }
                else
                { extent = 0; if (!NativeRecord(ref w, node.NativeOffset, node.Value, node.Type, node.NativeOffset + NativeRecordBytes(node.Type), ref extent)) { return -1; } }
                w.Put(data + (i + 1) * 16L, (ulong)node.NativeOffset, 8); w.Put(data + (i + 1) * 16L + 8, node.TypeId, 8);
            }
        }
        return size;
    }
`
