package cstable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessages() {
	g.tf("%s", tableMessageTypes)
	g.pf("static readonly byte[] tableAnnouncement = new byte[] { %s };\n", byteLiterals(ir.TableAnnouncement(g.unit)))
	g.pf("public static int AnnounceMeasure() { return tableAnnouncement.Length; }\npublic static ReadOnlySpan<byte> Announce() { return tableAnnouncement; }\n")
	g.pf("public static TableWire.Verdict AnnounceRead(TableVocabulary vocabulary, ReadOnlySpan<byte> bytes, TableReport report) { return TableWire.AnnounceRead(vocabulary, bytes, report); }\n")
	g.pf("static readonly TableVocabulary tableOwnVocabulary = TableWire.OwnVocabulary(tableAnnouncement);\n\n")
}

func (g *tableGen) emitMessageSurface(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	n := st.Name
	if ir.VariableTables(g.unit)[n] {
		g.pf("public static long %sLoadMeasure(TableVocabulary vocabulary, ReadOnlySpan<byte> bytes) { return TableWire.MessageLoadMeasure(%sTableType(), bytes, vocabulary); }\n", n, n)
	}
	g.pf("public static long %sMeasureMessages(%s[] values) { return TableWire.MessageSave(values, %sTableType(), Span<byte>.Empty, true); }\n", n, n, n)
	g.pf("public static long %sSaveMessages(%s[] values, Span<byte> bytes, TableReport report = null) { if (values.Length > 256 && report != null) { report.Refused = true; report.Reason = \"batch_too_large\"; report.Verdict = TableWire.Verdict.Refused; } return TableWire.MessageSave(values, %sTableType(), bytes, false); }\n", n, n, n)
	g.pf("public static TableWire.Verdict %sLoadMessages(%s[] values, ReadOnlySpan<byte> bytes, TableVocabulary vocabulary, TableReport report, out int count) { return TableWire.MessageLoad(values, %sTableType(), bytes, vocabulary, report, out count); }\n\n", n, n, n)
}

func messageSlot(u *ir.Unit, f *ir.Field) string {
	columns := fmt.Sprintf(", MessageSlot = %d", ir.TableVocabularySlots(u)[ir.TableFieldEntry(f).Key()])
	lo, hi, ok := ir.TableRawRange(f)
	if f.Type.Kind == ir.TBits {
		lo, hi, ok = big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)), true
	} else if !ok && ir.TableKindWide(tableScalarKind(f)) {
		lo, hi = tableStorageRange(f.Type.Signed, f.Type.Width)
		ok = true
	}
	if ok {
		columns += fmt.Sprintf(", MessageBounded = true, MessageSigned = %t, MessageMin = %s, MessageMax = %s", f.Type.Signed, wideLiteral(lo, false), wideLiteral(hi, false))
	}
	return columns
}

const tableMessageTypes = `
// One received announcement per direction. A failed first announcement is
// terminal too; a second never changes its vocabulary or build version.
public sealed class TableVocabulary
{
    public int MaxEntries = 4096, MaxBytes = 65536;
    public bool Announced { get; internal set; }
    public ulong BuildVersion { get; internal set; }
    internal bool Attempted;
    internal TableMessageEntry[] Entries = Array.Empty<TableMessageEntry>();
    public int Count { get { return Entries.Length; } }
    public int RefBits { get { return Math.Max(1, System.Numerics.BitOperations.Log2((uint)Math.Max(1, Count)) + 1); } }
}
internal sealed class TableMessageShape
{
    public byte Packing, Elem;
    public int Bits;
    public ulong Min, Max;
    public UInt128 Base;
    public float QMin, QMax, QRes, Delta;
    public uint Steps;
    public TableMessageShape Inner;
}
internal sealed class TableMessageEntry
{
    public ulong Id;
    public byte Kind;
    public TableMessageShape Shape;
}
`

const tableMessageSource = `
    static int BitCount(ulong n) { return n == 0 ? 0 : System.Numerics.BitOperations.Log2(n) + 1; }
    static bool Reserved(ulong id) { return id >= ulong.MaxValue - 2; }
    static bool MessageKind(byte kind) { return kind <= 33; }
    static bool MessageNumber(byte kind) { return kind >= 2 && kind <= 9 || kind >= 18 && kind <= 29; }
    static bool Signed(byte kind) { return kind >= 2 && kind <= 5 || kind == 18 || kind >= 20 && kind <= 24; }
    static int ValueBits(byte kind, TableMessageShape s)
    { return kind == 0 || kind == 32 ? 0 : kind == 1 ? 1 : kind == 10 && s.Packing == 2 || MessageNumber(kind) && s.Packing == 1 ? s.Bits : Width(kind) == 0 ? -1 : Width(kind) * 8; }
    static Verdict Refuse(TableReport report, string reason)
    { report.Refused = true; report.Reason = reason; return Finish(report, Verdict.Refused); }
    static bool Shape(ref Reader r, byte kind, out TableMessageShape shape, bool element = false)
    {
        shape = new TableMessageShape();
        if (MessageNumber(kind) || kind == 10)
        {
            if (!r.Has(1)) { return false; }
            shape.Packing = r.Byte();
            if (shape.Packing == 1 && kind != 10)
            {
                if (!r.Var(out ulong bits) || bits > (ulong)Width(kind) * 8) { return false; }
                shape.Bits = (int)bits;
                if (kind >= 18 && kind <= 29)
                { if (!r.Has(16)) { return false; } shape.Base = r.Fixed(8); shape.Base |= (UInt128)r.Fixed(8) << 64; }
                else
                {
                    if (!r.Var(out ulong raw)) { return false; }
                    shape.Base = Signed(kind) ? unchecked((UInt128)(Int128)((long)(raw >> 1) ^ -(long)(raw & 1))) : raw;
                }
            }
            else if (shape.Packing == 2 && kind == 10)
            {
                if (!r.Has(12)) { return false; }
                shape.QMin = TableBitsToFloat((uint)r.Fixed(4)); shape.QMax = TableBitsToFloat((uint)r.Fixed(4)); shape.QRes = TableBitsToFloat((uint)r.Fixed(4));
                if (!(shape.QMin < shape.QMax) || !(shape.QRes > 0)) { return false; }
                shape.Delta = shape.QMax - shape.QMin;
                float steps = shape.Delta / shape.QRes;
                if (!float.IsFinite(shape.Delta) || !float.IsFinite(steps)) { return false; }
                steps = Math.Clamp(steps, 1f, 4294967040f);
                shape.Steps = (uint)Math.Ceiling(steps); shape.Bits = BitCount(shape.Steps);
            }
            else if (shape.Packing != 0) { return false; }
        }
        else if (kind == 12 || kind == 33)
        { if (!r.Var(out shape.Max) || shape.Max > int.MaxValue) { return false; } }
        else if (kind == 14 || kind == 16)
        {
            if (kind == 14 && (!r.Var(out shape.Min) || shape.Min > uint.MaxValue)) { return false; }
            if (!r.Var(out shape.Max) || shape.Max > uint.MaxValue || shape.Max < shape.Min || !r.Has(1)) { return false; }
            shape.Elem = r.Byte();
            if (!MessageKind(shape.Elem) || shape.Elem == 12 || shape.Elem == 33) { return false; }
            // An entry carries its own shape and one element shape. The C++
            // announcement reader does not recursively parse nested arrays.
            if (!element && !Shape(ref r, shape.Elem, out shape.Inner, true)) { return false; }
        }
        return true;
    }
    public static TableVocabulary OwnVocabulary(ReadOnlySpan<byte> bytes)
    {
        var vocabulary = new TableVocabulary();
        if (AnnounceRead(vocabulary, bytes, new TableReport()) != Verdict.Ok) { throw new InvalidOperationException("invalid generated vocabulary"); }
        return vocabulary;
    }
    public static Verdict AnnounceRead(TableVocabulary v, ReadOnlySpan<byte> bytes, TableReport report)
    {
        if (v.Attempted) { return Refuse(report, "second_announcement"); }
        v.Attempted = true;
        if (bytes.Length == 0) { Damage(report); return Finish(report, Verdict.Damaged); }
        if (bytes[0] != 1) { return Refuse(report, bytes[0] == 2 ? "message_form_as_file" : "newer_form"); }
        if (bytes.Length < 9) { Damage(report); return Finish(report, Verdict.Damaged); }
        ulong count = Read64(bytes, bytes.Length - 8);
        if (count > (ulong)(bytes.Length - 9) / 8) { Damage(report); return Finish(report, Verdict.Damaged); }
        int end = bytes.Length - 8 - (int)count * 8;
        ReadOnlySpan<byte> ids = bytes.Slice(end, (int)count * 8);
        for (int i = 0; i < ids.Length; i += 8)
        { for (int j = 0; j < i; j += 8) { if (Read64(ids, i) == Read64(ids, j)) { Damage(report); return Finish(report, Verdict.Damaged); } } }
        Reader r = new Reader(bytes.Slice(1, end - 1), ids);
        if (EndsEarly(r)) { Damage(report); return Finish(report, Verdict.Damaged); }
        Reader payload = default; int versions = 0, vocabularies = 0; bool terminated = false;
        while (r.Ref(out ulong reference, out ulong id))
        {
            if (reference == 0) { terminated = true; break; }
            if (!r.Has(1)) { break; }
            byte kind = r.Byte();
            if (id == ulong.MaxValue - 1)
            { if (kind != 9 || !r.Has(8)) { break; } v.BuildVersion = r.Fixed(8); versions++; }
            else if (id == ulong.MaxValue - 2)
            {
                if (kind != 14 || !r.Slice(out Reader a) || !a.Has(1) || a.Byte() != 6 || !a.Var(out ulong n) || n != (ulong)(a.Buffer.Length - a.Offset)) { break; }
                if (n > (ulong)(v.MaxBytes > 0 ? v.MaxBytes : 65536)) { return Refuse(report, "vocabulary_too_large"); }
                payload = new Reader(a.Buffer.Slice(a.Offset), default); vocabularies++;
            }
            else { report.Unknown++; if (!r.Skip(kind)) { break; } }
        }
        if (!terminated || versions != 1 || vocabularies != 1) { Damage(report); return Finish(report, Verdict.Damaged); }
        var entries = new System.Collections.Generic.List<TableMessageEntry>();
        var keys = new System.Collections.Generic.HashSet<string>(); bool node = false;
        while (payload.Has(1))
        {
            int start = payload.Offset;
            if (!payload.Has(9)) { Damage(report); return Finish(report, Verdict.Damaged); }
            ulong id = payload.Fixed(8); byte kind = payload.Byte();
            if (id == ulong.MaxValue - 1 || id == ulong.MaxValue - 2 || id == ulong.MaxValue && node || !MessageKind(kind) || !Shape(ref payload, kind, out TableMessageShape shape))
            { Damage(report); return Finish(report, Verdict.Damaged); }
            if (id == ulong.MaxValue) { node = true; }
            if (!keys.Add(Convert.ToHexString(payload.Buffer.Slice(start, payload.Offset - start)))) { Damage(report); return Finish(report, Verdict.Damaged); }
            entries.Add(new TableMessageEntry { Id = id, Kind = kind, Shape = shape });
            if (entries.Count > (v.MaxEntries > 0 ? v.MaxEntries : 4096)) { return Refuse(report, "vocabulary_too_large"); }
        }
        v.Entries = entries.ToArray(); v.Announced = true; return Finish(report, Verdict.Ok);
    }
    ref struct BitWriter
    {
        public Span<byte> Bytes;
        public long At;
        public bool Measure;
        public BitWriter(Span<byte> bytes, bool measure) { Bytes = bytes; Measure = measure; At = 0; }
        public void Put(UInt128 value, int bits)
        {
            if (!Measure) { for (int i = 0; i < bits; i++) { int at = checked((int)(At + i)); if (((value >> i) & 1) != 0) { Bytes[at >> 3] |= (byte)(1 << (at & 7)); } } }
            At += bits;
        }
        public void Align() { At = (At + 7) & ~7L; }
        public void Raw(ReadOnlySpan<byte> bytes) { foreach (byte b in bytes) { Put(b, 8); } }
    }
    ref struct BitReader
    {
        public ReadOnlySpan<byte> Bytes;
        public long At, End;
        public BitReader(ReadOnlySpan<byte> bytes) { Bytes = bytes; At = 0; End = (long)bytes.Length * 8; }
        public bool Get(int bits, out UInt128 value)
        {
            value = 0; if (bits < 0 || bits > 128 || bits > End - At) { return false; }
            for (int i = 0; i < bits; i++) { long at = At + i; value |= (UInt128)((Bytes[(int)(at >> 3)] >> (int)(at & 7)) & 1) << i; }
            At += bits; return true;
        }
        public bool Align() { return Get((int)((8 - (At & 7)) & 7), out UInt128 padding) && padding == 0; }
        public bool Skip(long bits) { if (bits < 0 || bits > End - At) { return false; } At += bits; return true; }
    }
    static ulong NameSlot(ulong id)
    {
        for (int i = 0; i < tableOwnVocabulary.Count; i++)
        { var entry = tableOwnVocabulary.Entries[i]; if (entry.Id == id && (entry.Kind == 0 || id == ulong.MaxValue)) { return (ulong)i + 1; } }
        return 0;
    }
    static bool MessageRides(object value, TableFieldInfo f)
    {
        if (f.WireGuard != null && !f.WireGuard(value)) { return false; }
        if (!f.Optional && f.Counted) { return f.GetCount(value) != 0; }
        return Rides(value, f);
    }
    static void MessageBody(ref BitWriter w, object value, TableTypeInfo type, Graph graph, int indexBits)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (!MessageRides(value, f)) { continue; }
            w.Put((uint)f.MessageSlot, tableOwnVocabulary.RefBits);
            MessageField(ref w, value, f, tableOwnVocabulary.Entries[f.MessageSlot - 1].Shape, graph, indexBits);
        }
        w.Put(0, tableOwnVocabulary.RefBits);
    }
    static void MessageField(ref BitWriter w, object value, TableFieldInfo f, TableMessageShape shape, Graph graph, int indexBits)
    {
        if (f.IsArray)
        {
            int count = Count(value, f), present = count;
            if (f.KeyId != null) { present = 0; for (int i = 0; i < count; i++) { if (!DefaultElement(value, f, i)) { present++; } } }
            w.Put(unchecked((ulong)present - shape.Min), BitCount(shape.Max - shape.Min));
            if (f.KeyId == null && f.Kind == 6) { w.Align(); }
            for (int i = 0; i < count; i++)
            {
                if (f.KeyId != null) { if (DefaultElement(value, f, i)) { continue; } w.Put(NameSlot(f.KeyId((ulong)i + 1)), tableOwnVocabulary.RefBits); }
                MessageElement(ref w, value, f, i, shape.Inner, graph, indexBits);
            }
        }
        else if (f.Kind == 12 || f.Kind == 33)
        {
            int count = Count(value, f); w.Put((uint)count, BitCount(shape.Max));
            if (f.Kind == 12) { w.Align(); w.Raw(f.GetBuffer(value).AsSpan(0, count)); }
            else { for (int i = 0; i < count; i++) { w.Put(f.GetChars(value)[i], 16); } }
        }
        else { MessageElement(ref w, value, f, 0, shape, graph, indexBits); }
    }
    static void MessageElement(ref BitWriter w, object value, TableFieldInfo f, int i, TableMessageShape shape, Graph graph, int indexBits)
    {
        if (f.Kind == 13) { MessageBody(ref w, f.GetChild(value, i), f.Table, graph, indexBits); return; }
        if (f.Kind == 17) { w.Put(graph.Index(f.GetChild(value, i)), indexBits); return; }
        if (f.Kind == 30) { ulong raw = f.GetRaw(value, i); w.Put(raw == 0 ? 0 : NameSlot(f.VariantId(raw)), tableOwnVocabulary.RefBits); return; }
        if (f.Kind == 15)
        {
            object union = f.GetChild(value, i); ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { w.Put(0, tableOwnVocabulary.RefBits); return; }
            var arm = f.Arms.Arms[(int)tag]; w.Put((uint)arm.MessageSlot, tableOwnVocabulary.RefBits);
            if (arm.Field != null) { MessageField(ref w, union, arm.Field, tableOwnVocabulary.Entries[arm.MessageSlot - 1].Shape, graph, indexBits); }
            return;
        }
        UInt128 bits = f.GetWide != null ? f.GetWide(value, i) : f.GetBuffer != null ? f.GetBuffer(value)[i] : f.GetRaw(value, i);
        if (f.Kind == 10 && shape.Packing == 2)
        {
            float normalized = (TableBitsToFloat((uint)bits) - shape.QMin) / shape.Delta;
            if (!(normalized >= 0)) { normalized = 0; } else if (!(normalized <= 1)) { normalized = 1; }
            float scaled = normalized * shape.Steps;
            bits = Math.Min((uint)Math.Floor((double)(scaled + 0.5f)), shape.Steps);
        }
        else if (shape.Packing == 1) { bits = unchecked(bits - shape.Base); }
        w.Put(bits, ValueBits(f.Kind, shape));
    }
    public static long MessageSave(object[] values, TableTypeInfo type, Span<byte> bytes, bool measure)
    {
        if (values.Length < 1 || values.Length > 256) { return -1; }
        // Validate the graph and every enum/tag before touching the output.
        Graph[] graphs = type.Variable ? new Graph[values.Length] : null;
        Span<ulong> storage = stackalloc ulong[tableOwnVocabulary.Count];
        for (int i = 0; i < values.Length; i++)
        {
            if (values[i] == null) { return -1; }
            if (type.Variable) { graphs[i] = Number(values[i], type); if (graphs[i] == null) { return -1; } }
            Ids ids = new Ids(storage) { Graph = graphs == null ? null : graphs[i] };
            if (!Collect(values[i], type, ref ids) || !CollectNodes(ref ids)) { return -1; }
        }
        BitWriter probe = new BitWriter(Span<byte>.Empty, true);
        MessageBatch(ref probe, values, type, graphs);
        long size = (probe.At + 7) / 8;
        if (measure) { return size; }
        if (size > bytes.Length) { return -1; }
        bytes.Slice(0, (int)size).Clear(); BitWriter writer = new BitWriter(bytes, false);
        MessageBatch(ref writer, values, type, graphs); return size;
    }
    static void MessageBatch(ref BitWriter w, object[] values, TableTypeInfo type, Graph[] graphs)
    {
        w.Put(2, 8); w.Put((uint)(values.Length - 1), 8);
        for (int i = 0; i < values.Length; i++)
        {
            Graph graph = graphs == null ? null : graphs[i]; int indices = BitCount((ulong)(graph == null ? 1 : graph.Nodes.Count + 1));
            if (graph != null && graph.Nodes.Count > 0)
            {
                w.Put(NameSlot(ulong.MaxValue), tableOwnVocabulary.RefBits); w.Put((uint)graph.Nodes.Count, 32);
                foreach (Node node in graph.Nodes)
                {
                    w.Put(NameSlot(node.TypeId), tableOwnVocabulary.RefBits);
                    if (node.BlobKind != 0) { byte[] data = ((TableBlob)node.Value).Data; w.Put((uint)data.Length, 32); w.Align(); w.Raw(data); }
                    else { MessageBody(ref w, node.Value, node.Type, graph, indices); }
                }
            }
            MessageBody(ref w, values[i], type, graph, indices);
        }
        w.Align();
    }
`
