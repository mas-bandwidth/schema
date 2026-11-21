package unitytable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

func retainDepth(u *ir.Unit) int {
	memo := map[string]int{}
	var table func(*ir.Struct) int
	var field func(*ir.Field) int
	field = func(f *ir.Field) int {
		if f == nil || f.Type.Pointer {
			return 0
		}
		if f.IsMap() {
			return 1 + table(f.MapEntry)
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return 1 + table(ref)
		case *ir.Union:
			n := 1
			for _, v := range ref.Variants {
				if v.F != nil {
					n = max(n, 1+field(v.F))
				}
			}
			return n + 1
		}
		return 0
	}
	table = func(st *ir.Struct) int {
		if st == nil {
			return 0
		}
		if n, ok := memo[st.Name]; ok {
			return n
		}
		memo[st.Name] = 0
		n := 0
		for _, f := range st.Fields {
			n = max(n, field(f))
		}
		memo[st.Name] = n
		return n
	}
	depth := 64
	for _, st := range u.Tables {
		depth = max(depth, table(st)+8)
	}
	return depth
}

func emitRetainRuntime(b *strings.Builder, u *ir.Unit) {
	depth := retainDepth(u)
	var known []string
	for _, id := range ir.TableWireIds(u) {
		known = append(known, fmt.Sprintf("0x%016xUL", id))
	}
	b.WriteString(unityRetainTypesSource)
	b.WriteString("internal static partial class TableWire\n{\n")
	b.WriteString(strings.NewReplacer(
		"@DEPTH@", fmt.Sprint(depth),
		"@KNOWN@", strings.Join(known, ", "),
	).Replace(unityRetainWireSource))
	b.WriteString("}\n\n")
}

const unityRetainTypesSource = `public sealed class TableRetain
{
    public struct Id
    {
        public ulong Value;
        public int Slot;
    }

    public byte[] Bytes;
    public long Capacity;
    public long Used;
    public Id[] Ids;
    public int IdCapacity;
    public int IdUsed;
    public int Count;
    internal object[] NodeOwners;

    public TableRetain() { }

    public TableRetain(int byteCapacity, int idCapacity)
    {
        Bytes = byteCapacity > 0 ? new byte[byteCapacity] : Array.Empty<byte>();
        Capacity = byteCapacity;
        Ids = idCapacity > 0 ? new Id[idCapacity] : Array.Empty<Id>();
        IdCapacity = idCapacity;
    }

    public void Reset()
    {
        Used = 0;
        IdUsed = 0;
        Count = 0;
        NodeOwners = null;
    }
}

internal struct RetainPath
{
    public uint Node;
    public int Depth;
    public uint[] Steps;

    public static RetainPath ForNode(uint node)
    {
        return new RetainPath { Node = node, Depth = 0, Steps = null };
    }

    public RetainPath Step(int ordinal, int index)
    {
        if (Depth >= TableWire.RetainMaxDepth)
        {
            throw new InvalidOperationException("Schema retention path depth");
        }
        uint[] next = new uint[(Depth + 1) * 2];
        if (Steps != null && Depth > 0)
        {
            Array.Copy(Steps, 0, next, 0, Depth * 2);
        }
        next[Depth * 2] = (uint)ordinal;
        next[Depth * 2 + 1] = (uint)index;
        return new RetainPath { Node = Node, Depth = Depth + 1, Steps = next };
    }
}

`

const unityRetainWireSource = `    internal const int RetainMaxDepth = @DEPTH@;
    const int RetainHeader = 26;
    static readonly ulong[] RetainKnown = new ulong[] { @KNOWN@ };

    static uint Retain32(ReadOnlySpan<byte> p, int off)
    {
        return BinaryPrimitives.ReadUInt32LittleEndian(p.Slice(off, 4));
    }

    static ulong Retain64(ReadOnlySpan<byte> p, int off)
    {
        return BinaryPrimitives.ReadUInt64LittleEndian(p.Slice(off, 8));
    }

    static void RetainPut32(Span<byte> p, int off, uint v)
    {
        BinaryPrimitives.WriteUInt32LittleEndian(p.Slice(off, 4), v);
    }

    static void RetainPut64(Span<byte> p, int off, ulong v)
    {
        BinaryPrimitives.WriteUInt64LittleEndian(p.Slice(off, 8), v);
    }

    static int RetainPayloadOffset(ReadOnlySpan<byte> bytes, int recOffset)
    {
        return recOffset + RetainHeader + checked((int)(8 * Retain32(bytes, recOffset + 8)));
    }

    static bool RetainReserved(ulong id)
    {
        return id >= ulong.MaxValue - 2;
    }

    static bool RetainHere(TableRetain retain, int recOffset, RetainPath path, bool prefix = false, int field = -1)
    {
        if (retain == null || retain.Bytes == null || path.Node == 0) { return false; }
        if (Retain32(retain.Bytes, recOffset + 4) != path.Node) { return false; }
        uint depth = Retain32(retain.Bytes, recOffset + 8);
        if (prefix ? depth < (uint)path.Depth : depth != (uint)path.Depth) { return false; }
        if (field >= 0 && depth <= (uint)path.Depth) { return false; }
        int steps = recOffset + RetainHeader;
        for (int i = 0; i < path.Depth * 2; i++)
        {
            if (Retain32(retain.Bytes, steps + 4 * i) != path.Steps[i]) { return false; }
        }
        return field < 0 || Retain32(retain.Bytes, steps + 8 * path.Depth) == (uint)field;
    }

    internal static void RetainDiscard(TableRetain retain, RetainPath path, int field = -1)
    {
        if (retain == null || retain.Bytes == null || retain.Count == 0) { return; }
        int from = 0, to = 0, kept = 0;
        for (int i = 0; i < retain.Count; i++)
        {
            int n = checked((int)Retain32(retain.Bytes, from));
            if (!RetainHere(retain, from, path, true, field))
            {
                if (from != to) { Buffer.BlockCopy(retain.Bytes, from, retain.Bytes, to, n); }
                to += n;
                kept++;
            }
            from += n;
        }
        retain.Count = kept;
        retain.Used = to;
    }

    ref struct RetainIn
    {
        public Reader R;
        public Span<byte> Output;
        public bool HasOutput;
        public long At, Limit;

        public void Raw(ReadOnlySpan<byte> data)
        {
            if (HasOutput) { data.CopyTo(Output.Slice(checked((int)At), data.Length)); }
            At += data.Length;
        }

        public void Fixed(ulong value, int bytes)
        {
            if (HasOutput)
            {
                for (int i = 0; i < bytes; i++)
                {
                    Output[checked((int)(At + i))] = unchecked((byte)(value >> (8 * i)));
                }
            }
            At += bytes;
        }

        public void Var(ulong value)
        {
            while (value >= 128)
            {
                Fixed(unchecked((byte)(value | 128)), 1);
                value >>= 7;
            }
            Fixed(value, 1);
        }
    }

    static bool RetainInRef(ref RetainIn s, bool zero)
    {
        if (!s.R.Ref(out ulong reference, out ulong id) || reference == 0 && !zero || reference != 0 && RetainReserved(id))
        {
            return false;
        }
        if (reference == 0)
        {
            s.Fixed(0, 1);
        }
        else
        {
            s.Fixed(1, 1);
            s.Fixed(id, 8);
        }
        return true;
    }

    static bool RetainInFrame(ref RetainIn s, byte kind, int bytes, int depth)
    {
        long slot = s.At;
        s.Fixed(0, 8);
        long began = s.At;
        if (!RetainInContent(ref s, kind, bytes, depth) || s.At - began > uint.MaxValue) { return false; }
        if (s.HasOutput) { RetainPut32(s.Output, checked((int)slot), (uint)(s.At - began)); }
        return true;
    }

    static bool RetainInContent(ref RetainIn s, byte kind, int bytes, int depth)
    {
        if (depth > 64 || s.At > s.Limit || !s.R.Has(bytes)) { return false; }
        int end = s.R.Offset + bytes;
        switch (kind)
        {
            case 13:
                for (;;)
                {
                    int mark = s.R.Offset;
                    if (!s.R.Var(out ulong field)) { return false; }
                    if (field == 0) { s.Fixed(0, 1); break; }
                    s.R.Offset = mark;
                    if (!RetainInRef(ref s, false) || s.R.Offset >= end) { return false; }
                    byte fieldKind = s.R.Byte();
                    s.Fixed(fieldKind, 1);
                    if (!RetainInPayload(ref s, fieldKind, depth) || s.R.Offset > end) { return false; }
                }
                break;
            case 14:
            case 16:
                if (s.R.Offset >= end) { return false; }
                byte element = s.R.Byte();
                s.Fixed(element, 1);
                if (!s.R.Var(out ulong count)) { return false; }
                s.Var(count);
                for (ulong i = 0; i < count; i++)
                {
                    if (kind == 16)
                    {
                        if (!RetainInRef(ref s, false) || !s.R.Var(out ulong size) || size > (ulong)(end - s.R.Offset) || !RetainInFrame(ref s, element, (int)size, depth + 1))
                        {
                            return false;
                        }
                    }
                    else if (!RetainInPayload(ref s, element, depth)) { return false; }
                    if (s.R.Offset > end) { return false; }
                }
                break;
            case 15:
            case 30:
                if (!RetainInPayload(ref s, kind, depth)) { return false; }
                break;
            case 17:
                return false;
            default:
                s.Raw(s.R.Buffer.Slice(s.R.Offset, bytes));
                s.R.Offset += bytes;
                break;
        }
        return s.R.Offset == end;
    }

    static bool RetainInPayload(ref RetainIn s, byte kind, int depth)
    {
        if (s.At > s.Limit) { return false; }
        int width = Width(kind);
        if (width != 0)
        {
            if (!s.R.Has(width)) { return false; }
            s.Raw(s.R.Buffer.Slice(s.R.Offset, width));
            s.R.Offset += width;
            return true;
        }
        switch (kind)
        {
            case 12:
            case 31:
            case 32:
                if (!s.R.Var(out ulong size) || size > (ulong)(s.R.Buffer.Length - s.R.Offset)) { return false; }
                s.Var(size);
                s.Raw(s.R.Buffer.Slice(s.R.Offset, (int)size));
                s.R.Offset += (int)size;
                return true;
            case 13:
            case 14:
            case 16:
                if (!s.R.Var(out ulong bytes) || bytes > (ulong)(s.R.Buffer.Length - s.R.Offset)) { return false; }
                return RetainInFrame(ref s, kind, (int)bytes, depth + 1);
            case 15:
                int mark = s.R.Offset;
                if (!s.R.Var(out ulong arm)) { return false; }
                s.R.Offset = mark;
                if (!RetainInRef(ref s, true)) { return false; }
                if (arm == 0) { return true; }
                if (!s.R.Has(1)) { return false; }
                byte armKind = s.R.Byte();
                s.Fixed(armKind, 1);
                if (!s.R.Var(out ulong armBytes) || armBytes > (ulong)(s.R.Buffer.Length - s.R.Offset)) { return false; }
                return RetainInFrame(ref s, armKind, (int)armBytes, depth + 1);
            case 30:
                return RetainInRef(ref s, true);
            default:
                return false;
        }
    }

    static bool RetainCapture(TableRetain retain, RetainPath path, ref Reader r, ulong id, byte kind, _R report)
    {
        int mark = r.Offset;
        if (!r.Skip(kind)) { return false; }
        if ((kind == 10 || kind == 11) && r.Offset - mark == Width(kind) &&
            NonFiniteBits(kind, kind == 10 ? BinaryPrimitives.ReadUInt32LittleEndian(r.Buffer.Slice(mark, 4)) : BinaryPrimitives.ReadUInt64LittleEndian(r.Buffer.Slice(mark, 8))))
        {
            NonFiniteRetained(id, report);
            return true;
        }
        if (retain == null) { return true; }
        long cap = retain.Bytes == null ? 0 : Math.Min(retain.Capacity, retain.Bytes.LongLength);
        Reader payload = new Reader(r.Buffer.Slice(mark, r.Offset - mark), r.Vocabulary);
        RetainIn probe = new RetainIn { R = payload, Limit = cap - retain.Used - RetainHeader - 8L * path.Depth };
        if (!RetainInPayload(ref probe, kind, 0) || probe.R.Offset != payload.Buffer.Length)
        {
            report.RetainLost++;
            return true;
        }
        long need = RetainHeader + 8L * path.Depth + probe.At;
        if (need > int.MaxValue || retain.Bytes == null || retain.Used > cap - need)
        {
            report.RetainLost++;
            return true;
        }
        int recOff = checked((int)retain.Used);
        Span<byte> rec = retain.Bytes.AsSpan(recOff, (int)need);
        RetainPut32(rec, 0, (uint)need);
        RetainPut32(rec, 4, path.Node);
        RetainPut32(rec, 8, (uint)path.Depth);
        RetainPut32(rec, 12, (uint)probe.At);
        RetainPut64(rec, 16, id);
        rec[24] = kind;
        rec[25] = 0;
        for (int i = 0; i < path.Depth * 2; i++)
        {
            RetainPut32(rec, RetainHeader + 4 * i, path.Steps[i]);
        }
        int payOff = RetainHeader + 8 * path.Depth;
        RetainIn write = new RetainIn { R = payload, Output = rec.Slice(payOff, (int)probe.At), HasOutput = true, Limit = probe.At };
        if (!RetainInPayload(ref write, kind, 0))
        {
            report.RetainLost++;
            return true;
        }
        retain.Used += need;
        retain.Count++;
        report.Retained++;
        return true;
    }

    ref struct RetainIds
    {
        public Span<ulong> Values;
        public Span<int> Slots;
        public int KnownCount, Count;
        public TableRetain Store;
        public Graph Graph;
        public System.Collections.Generic.Dictionary<object, uint> OldNodes;
        public TableTypeInfo RootType;
        public bool Overflow, Lost;

        public RetainIds(Span<ulong> values, Span<int> slots, TableRetain store, Graph graph, System.Collections.Generic.Dictionary<object, uint> oldNodes, TableTypeInfo type)
        {
            Values = values;
            Slots = slots;
            KnownCount = 0;
            Count = 0;
            Store = store;
            Graph = graph;
            OldNodes = oldNodes;
            RootType = type;
            Overflow = false;
            Lost = false;
            if (store != null) { store.IdUsed = 0; }
        }

        public ulong Reference(ulong id)
        {
            for (int i = 0; i < KnownCount; i++)
            {
                if (Values[i] == id) { return (ulong)Slots[i]; }
            }
            if (KnownCount == Values.Length) { Overflow = true; return 0; }
            Values[KnownCount] = id;
            Slots[KnownCount++] = ++Count;
            return (ulong)Count;
        }

        public ulong Record(ulong id)
        {
            if (Array.BinarySearch(RetainKnown, id) >= 0) { return Reference(id); }
            if (Store == null) { Lost = true; return 0; }
            int idCap = Store.Ids == null ? 0 : Math.Min(Store.IdCapacity, Store.Ids.Length);
            for (int i = 0; i < Store.IdUsed; i++)
            {
                if (Store.Ids[i].Value == id) { return (ulong)Store.Ids[i].Slot; }
            }
            if (Store.Ids == null || Store.IdUsed >= idCap) { Lost = true; return 0; }
            Store.Ids[Store.IdUsed++] = new TableRetain.Id { Value = id, Slot = ++Count };
            return (ulong)Count;
        }

        public void Truncate(int mark)
        {
            while (KnownCount > 0 && Slots[KnownCount - 1] > mark) { KnownCount--; }
            if (Store != null && Store.Ids != null)
            {
                while (Store.IdUsed > 0 && Store.Ids[Store.IdUsed - 1].Slot > mark) { Store.IdUsed--; }
            }
            Count = mark;
            Lost = false;
        }

        public void Write(ref Writer w)
        {
            int i = 0, j = 0;
            int idUsed = Store == null || Store.Ids == null ? 0 : Store.IdUsed;
            while (i < KnownCount || j < idUsed)
            {
                if (j == idUsed || (i < KnownCount && Slots[i] < Store.Ids[j].Slot))
                {
                    w.Fixed(Values[i++], 8);
                }
                else
                {
                    w.Fixed(Store.Ids[j++].Value, 8);
                }
            }
            w.Fixed((ulong)Count, 8);
        }
    }

    ref struct RetainOut
    {
        public Reader R;
        public Writer W;
        public Span<byte> Input;
        public bool Measure;
        public long Bytes;

        public void Raw(ReadOnlySpan<byte> data) { if (!Measure) { W.Raw(data); } Bytes += data.Length; }
        public void Var(ulong value) { if (!Measure) { W.Var(value); } Bytes += VarSize(value); }
        public void Byte(byte value) { if (!Measure) { W.Byte(value); } Bytes++; }
    }

    static bool RetainOutRef(ref RetainOut s, ref RetainIds ids)
    {
        if (!s.R.Has(1)) { return false; }
        if (s.R.Byte() == 0)
        {
            s.Var(0);
            return true;
        }
        if (!s.R.Has(8)) { return false; }
        ulong id = s.R.Fixed(8);
        s.Var(ids.Record(id));
        return !ids.Lost && !ids.Overflow;
    }

    static bool RetainOutFrame(ref RetainOut s, ref RetainIds ids, byte kind, int depth)
    {
        if (!s.R.Has(8)) { return false; }
        int slot = s.R.Offset;
        uint size = Retain32(s.Input, slot);
        s.R.Offset += 8;
        if (size > int.MaxValue) { return false; }
        if (s.Measure)
        {
            long began = s.Bytes;
            if (!RetainOutContent(ref s, ref ids, kind, (int)size, depth) || s.Bytes - began > uint.MaxValue) { return false; }
            uint wire = (uint)(s.Bytes - began);
            RetainPut32(s.Input, slot + 4, wire);
            s.Bytes += VarSize(wire);
            return true;
        }
        uint cached = Retain32(s.Input, slot + 4);
        s.Var(cached);
        long start = s.Bytes;
        return RetainOutContent(ref s, ref ids, kind, (int)size, depth) && s.Bytes - start == cached;
    }

    static bool RetainOutContent(ref RetainOut s, ref RetainIds ids, byte kind, int bytes, int depth)
    {
        if (depth > 64 || !s.R.Has(bytes)) { return false; }
        int end = s.R.Offset + bytes;
        switch (kind)
        {
            case 13:
                for (;;)
                {
                    if (!s.R.Has(1)) { return false; }
                    if (s.R.Buffer[s.R.Offset] == 0) { s.R.Offset++; s.Var(0); break; }
                    if (!RetainOutRef(ref s, ref ids) || !s.R.Has(1)) { return false; }
                    byte field = s.R.Byte();
                    s.Byte(field);
                    if (!RetainOutPayload(ref s, ref ids, field, depth)) { return false; }
                }
                break;
            case 14:
            case 16:
                if (!s.R.Has(1)) { return false; }
                byte element = s.R.Byte();
                s.Byte(element);
                if (!s.R.Var(out ulong count)) { return false; }
                s.Var(count);
                for (ulong i = 0; i < count; i++)
                {
                    if (kind == 16)
                    {
                        if (!RetainOutRef(ref s, ref ids) || !RetainOutFrame(ref s, ref ids, element, depth + 1)) { return false; }
                    }
                    else if (!RetainOutPayload(ref s, ref ids, element, depth)) { return false; }
                }
                break;
            case 15:
            case 30:
                if (!RetainOutPayload(ref s, ref ids, kind, depth)) { return false; }
                break;
            case 17:
                return false;
            default:
                s.Raw(s.R.Buffer.Slice(s.R.Offset, bytes));
                s.R.Offset += bytes;
                break;
        }
        return s.R.Offset == end;
    }

    static bool RetainOutPayload(ref RetainOut s, ref RetainIds ids, byte kind, int depth)
    {
        int width = Width(kind);
        if (width != 0)
        {
            if (!s.R.Has(width)) { return false; }
            s.Raw(s.R.Buffer.Slice(s.R.Offset, width));
            s.R.Offset += width;
            return true;
        }
        switch (kind)
        {
            case 12:
            case 31:
            case 32:
                if (!s.R.Var(out ulong size) || size > (ulong)(s.R.Buffer.Length - s.R.Offset)) { return false; }
                s.Var(size);
                s.Raw(s.R.Buffer.Slice(s.R.Offset, (int)size));
                s.R.Offset += (int)size;
                return true;
            case 13:
            case 14:
            case 16:
                return RetainOutFrame(ref s, ref ids, kind, depth + 1);
            case 15:
                if (!s.R.Has(1)) { return false; }
                bool armZero = s.R.Buffer[s.R.Offset] == 0;
                if (!RetainOutRef(ref s, ref ids)) { return false; }
                if (armZero) { return true; }
                if (!s.R.Has(1)) { return false; }
                byte armKind = s.R.Byte();
                s.Byte(armKind);
                return RetainOutFrame(ref s, ref ids, armKind, depth + 1);
            case 30:
                return RetainOutRef(ref s, ref ids);
            default:
                return false;
        }
    }

    static long RetainRecordWire(TableRetain store, int recOffset, ref RetainIds ids, out ulong reference)
    {
        int mark = ids.Count;
        ids.Lost = false;
        reference = ids.Record(Retain64(store.Bytes, recOffset + 16));
        int payOff = RetainPayloadOffset(store.Bytes, recOffset);
        int payLen = checked((int)Retain32(store.Bytes, recOffset + 12));
        Span<byte> paySpan = store.Bytes.AsSpan(payOff, payLen);
        RetainOut s = new RetainOut { R = new Reader(paySpan, default), Input = paySpan, Measure = true };
        if (!ids.Lost && !ids.Overflow && RetainOutPayload(ref s, ref ids, store.Bytes[recOffset + 24], 0) && s.R.Offset == s.R.Buffer.Length)
        {
            return VarSize(reference) + 1 + s.Bytes;
        }
        ids.Truncate(mark);
        return -1;
    }

    static bool RetainHas(TableRetain retain, RetainPath path)
    {
        if (retain == null || retain.Bytes == null || path.Node == 0) { return false; }
        int at = 0;
        for (int i = 0; i < retain.Count; i++)
        {
            int cur = at;
            at += checked((int)Retain32(retain.Bytes, cur));
            if (RetainHere(retain, cur, path)) { return true; }
        }
        return false;
    }

    static long RetainTailMeasure(ref RetainIds ids, RetainPath path)
    {
        if (ids.Store == null || ids.Store.Bytes == null || path.Node == 0) { return 0; }
        int at = 0;
        long bytes = 0;
        for (int i = 0; i < ids.Store.Count; i++)
        {
            int cur = at;
            at += checked((int)Retain32(ids.Store.Bytes, cur));
            if (!RetainHere(ids.Store, cur, path)) { continue; }
            long size = RetainRecordWire(ids.Store, cur, ref ids, out _);
            if (size >= 0) { bytes += size; }
        }
        return bytes;
    }

    static void RetainTailSave(ref Writer w, ref RetainIds ids, RetainPath path)
    {
        if (ids.Store == null || ids.Store.Bytes == null || path.Node == 0) { return; }
        int at = 0;
        for (int i = 0; i < ids.Store.Count; i++)
        {
            int cur = at;
            at += checked((int)Retain32(ids.Store.Bytes, cur));
            if (!RetainHere(ids.Store, cur, path) || RetainRecordWire(ids.Store, cur, ref ids, out ulong reference) < 0)
            {
                continue;
            }
            byte kind = ids.Store.Bytes[cur + 24];
            w.Var(reference);
            w.Byte(kind);
            int payOff = RetainPayloadOffset(ids.Store.Bytes, cur);
            int payLen = checked((int)Retain32(ids.Store.Bytes, cur + 12));
            Span<byte> paySpan = ids.Store.Bytes.AsSpan(payOff, payLen);
            RetainOut s = new RetainOut { R = new Reader(paySpan, default), Input = paySpan, W = w };
            if (!RetainOutPayload(ref s, ref ids, kind, 0))
            {
                throw new InvalidOperationException("Retention measure/write disagreement");
            }
            w = s.W;
            ids.Store.Bytes[cur + 25] = 1;
        }
    }

    struct RetainValue
    {
        public object Obj;
        public TableRetain Store;
        public RetainPath Path;
        public bool IsArm;

        public RetainValue(object obj, TableRetain store, RetainPath path, bool isArm = false)
        {
            Obj = obj;
            Store = store;
            Path = path;
            IsArm = isArm;
        }

        public RetainValue Child(TableFieldInfo f, int index)
        {
            object child = f.GetChild(Obj, index);
            RetainPath next = Path;
            if (f.Kind == 13)
            {
                if (!(IsArm && !f.IsArray))
                {
                    next = Path.Step(f.Ordinal, index);
                }
            }
            return new RetainValue(child, Store, next, false);
        }

        public RetainValue UnionArm(TableFieldInfo f, int index, ulong tag)
        {
            object union = f.GetChild(Obj, index);
            RetainPath uPath = f.IsArray ? Path.Step(f.Ordinal, index) : Path;
            RetainPath armPath = uPath.Step(f.Ordinal, (int)tag - 1);
            return new RetainValue(union, Store, armPath, true);
        }
    }

    static bool RetainDefaultElement(RetainValue value, TableFieldInfo f, int i)
    {
        if (f.Kind == 17) { return f.GetChild(value.Obj, i) == null; }
        if (f.Kind == 13) { return RetainEmpty(value.Child(f, i), f.Table); }
        if (f.Kind == 15) { return f.Arms.GetTag(f.GetChild(value.Obj, i)) == 0; }
        if (f.GetWide != null) { return f.GetWide(value.Obj, i) == f.DefaultWide; }
        if (f.GetBuffer != null) { return f.GetBuffer(value.Obj)[i] == 0; }
        return DefaultRaw(f.GetRaw(value.Obj, i), f);
    }

    static bool RetainEmpty(RetainValue value, TableTypeInfo type)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (RetainRides(value, f)) { return false; }
        }
        return !RetainHas(value.Store, value.Path);
    }

    static bool RetainRides(RetainValue value, TableFieldInfo f)
    {
        if (f.WireGuard != null && !f.WireGuard(value.Obj)) { return false; }
        if (f.Optional) { return f.GetPresent(value.Obj); }
        if (f.Required) { return true; }
        if (f.Counted)
        {
            int n = f.GetCount(value.Obj);
            if (f.DefaultBytes != null) { return n != f.DefaultBytes.Length || !f.GetBuffer(value.Obj).AsSpan(0, n).SequenceEqual(f.DefaultBytes); }
            if (f.ArrayMin > 0)
            {
                if (n != f.ArrayMin) { return true; }
                if (f.Kind == 13 && f.KeyId == null) { return true; }
                for (int i = 0; i < n; i++) { if (!RetainDefaultElement(value, f, i)) { return true; } }
                return false;
            }
            return n != 0;
        }
        if (!f.IsArray) { return !RetainDefaultElement(value, f, 0); }
        if (f.Kind == 13 && f.KeyId == null) { return true; }
        for (int i = 0; i < f.ArrayBound; i++) { if (!RetainDefaultElement(value, f, i)) { return true; } }
        return false;
    }

    static bool RetainSlotRides(RetainValue value, TableFieldInfo f, int i, ref RetainIds ids)
    {
        return !RetainDefaultElement(value, f, i) && (f.Kind != 13 || RetainBodySize(value.Child(f, i), f.Table, ref ids) > 1);
    }

    static long RetainPayloadSize(RetainValue value, TableFieldInfo f, ref RetainIds ids)
    {
        if (f == null) { return 0; }
        if (f.IsArray) { return RetainArraySize(value, f, ref ids); }
        if (f.Kind == 12) { return Count(value.Obj, f); }
        return RetainElementSize(value, f, 0, ref ids, false);
    }

    static long RetainElementSize(RetainValue value, TableFieldInfo f, int i, ref RetainIds ids, bool framed)
    {
        if (f.TextList && f.Kind == 12)
        {
            long n = Count(f.GetChild(value.Obj, i), f.Table.Fields[0]);
            return n + (framed ? VarSize((ulong)n) : 0);
        }
        if (f.Kind == 13)
        {
            long n = RetainBodySize(value.Child(f, i), f.Table, ref ids);
            return n + (framed ? VarSize((ulong)n) : 0);
        }
        if (f.Kind == 15)
        {
            object union = f.GetChild(value.Obj, i);
            ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { return 1; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            ulong reference = ids.Reference(f.VariantId(tag));
            long n = RetainPayloadSize(value.UnionArm(f, i, tag), arm.Field, ref ids);
            return VarSize(reference) + 1 + VarSize((ulong)n) + n;
        }
        if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value.Obj, i);
            return VarSize(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        if (f.Kind == 17)
        {
            return VarSize(ids.Graph.Index(f.GetChild(value.Obj, i)));
        }
        return Width(f.Kind);
    }

    static long RetainArraySize(RetainValue value, TableFieldInfo f, ref RetainIds ids)
    {
        long n = 0;
        int count = 0;
        int total = Count(value.Obj, f);
        for (int i = 0; i < total; i++)
        {
            if (f.KeyId != null)
            {
                if (RetainDefaultElement(value, f, i)) { continue; }
                int mark = ids.Count;
                ulong reference = ids.Reference(f.KeyId((ulong)i + 1));
                long elem = RetainElementSize(value, f, i, ref ids, false);
                if (f.Kind == 13 && elem == 1) { ids.Truncate(mark); continue; }
                n += VarSize(reference) + VarSize((ulong)elem) + elem;
            }
            else if (f.Kind == 13)
            {
                long childBody = RetainBodySize(value.Child(f, i), f.Table, ref ids);
                n += VarSize((ulong)childBody) + childBody;
            }
            else
            {
                n += RetainElementSize(value, f, i, ref ids, true);
            }
            count++;
        }
        return 1 + VarSize((ulong)count) + n;
    }

    static long RetainBodySize(RetainValue value, TableTypeInfo type, ref RetainIds ids)
    {
        long n = 1;
        for (int i = 0; i < type.Fields.Length; i++)
        {
            TableFieldInfo f = type.Fields[i];
            if (!RetainRides(value, f)) { continue; }
            int mark = ids.Count;
            ulong reference = ids.Reference(f.Id);
            long payload = RetainPayloadSize(value, f, ref ids);
            if (!f.Optional && !f.Required && (f.Kind == 13 && !f.IsArray && payload == 1 || f.KeyId != null && payload == 2))
            {
                ids.Truncate(mark);
                continue;
            }
            n += VarSize(reference) + 1 + payload;
            if (f.IsArray || f.Kind == 12 || f.Kind == 13)
            {
                n += VarSize((ulong)payload);
            }
        }
        return n + RetainTailMeasure(ref ids, value.Path);
    }

    static RetainValue RetainNodeValue(Node node, ref RetainIds ids)
    {
        uint oldNode = 0;
        if (ids.OldNodes != null && node.Value != null)
        {
            ids.OldNodes.TryGetValue(node.Value, out oldNode);
        }
        return new RetainValue(node.Value, ids.Store, oldNode == 0 ? default : RetainPath.ForNode(oldNode), false);
    }

    static long RetainNodesSize(ref RetainIds ids)
    {
        if (ids.Graph == null || ids.Graph.Nodes.Count == 0) { return 0; }
        long size = VarSize((ulong)ids.Graph.Nodes.Count);
        foreach (Node node in ids.Graph.Nodes)
        {
            ulong reference = ids.Reference(node.TypeId);
            long n = node.BlobKind == 0 ? RetainBodySize(RetainNodeValue(node, ref ids), node.Type, ref ids) : ((TableBlob)node.Value).Data.Length;
            size += VarSize(reference) + VarSize((ulong)n) + n;
        }
        return size;
    }

    static void RetainWriteNodes(ref Writer w, ref RetainIds ids)
    {
        long size = RetainNodesSize(ref ids);
        if (size == 0) { return; }
        w.Var(ids.Reference(ulong.MaxValue));
        w.Byte(12);
        w.Var((ulong)size);
        w.Var((ulong)ids.Graph.Nodes.Count);
        foreach (Node node in ids.Graph.Nodes)
        {
            w.Var(ids.Reference(node.TypeId));
            if (node.BlobKind != 0)
            {
                byte[] data = ((TableBlob)node.Value).Data;
                w.Var((ulong)data.Length);
                w.Raw(data);
            }
            else
            {
                RetainValue nv = RetainNodeValue(node, ref ids);
                w.Var((ulong)RetainBodySize(nv, node.Type, ref ids));
                RetainWriteBody(ref w, nv, node.Type, ref ids, false);
            }
        }
    }

    static void RetainWriteElement(ref Writer w, RetainValue value, TableFieldInfo f, int i, ref RetainIds ids, bool framed)
    {
        if (f.TextList && f.Kind == 12)
        {
            object child = f.GetChild(value.Obj, i);
            TableFieldInfo text = f.Table.Fields[0];
            int count = Count(child, text);
            if (framed) { w.Var((ulong)count); }
            w.Raw(text.GetBuffer(child).AsSpan(0, count));
            return;
        }
        if (f.Kind == 13)
        {
            RetainValue child = value.Child(f, i);
            if (framed) { w.Var((ulong)RetainBodySize(child, f.Table, ref ids)); }
            RetainWriteBody(ref w, child, f.Table, ref ids, false);
        }
        else if (f.Kind == 15)
        {
            object union = f.GetChild(value.Obj, i);
            ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { w.Var(0); return; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            RetainValue armVal = value.UnionArm(f, i, tag);
            w.Var(ids.Reference(f.VariantId(tag)));
            w.Byte(arm.Field == null ? (byte)32 : Kind(arm.Field));
            w.Var((ulong)RetainPayloadSize(armVal, arm.Field, ref ids));
            RetainWritePayload(ref w, armVal, arm.Field, ref ids);
        }
        else if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value.Obj, i);
            w.Var(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        else if (f.Kind == 17)
        {
            w.Var(ids.Graph.Index(f.GetChild(value.Obj, i)));
        }
        else if (f.GetWide != null)
        {
            UInt128Value raw = f.GetWide(value.Obj, i);
            int width = Width(f.Kind);
            w.Fixed((ulong)raw, Math.Min(width, 8));
            if (width == 16) { w.Fixed((ulong)(raw >> 64), 8); }
        }
        else
        {
            w.Fixed(f.GetBuffer != null ? f.GetBuffer(value.Obj)[i] : f.GetRaw(value.Obj, i), Width(f.Kind));
        }
    }

    static void RetainWritePayload(ref Writer w, RetainValue value, TableFieldInfo f, ref RetainIds ids)
    {
        if (f == null) { return; }
        if (f.IsArray)
        {
            w.Byte(f.Kind);
            int total = Count(value.Obj, f);
            int count = total;
            if (f.KeyId != null)
            {
                count = 0;
                for (int i = 0; i < f.ArrayBound; i++)
                {
                    if (RetainSlotRides(value, f, i, ref ids)) { count++; }
                }
            }
            w.Var((ulong)count);
            for (int i = 0; i < total; i++)
            {
                if (f.KeyId != null)
                {
                    if (!RetainSlotRides(value, f, i, ref ids)) { continue; }
                    w.Var(ids.Reference(f.KeyId((ulong)i + 1)));
                    w.Var((ulong)RetainElementSize(value, f, i, ref ids, false));
                    RetainWriteElement(ref w, value, f, i, ref ids, false);
                }
                else if (f.Kind == 13)
                {
                    RetainValue child = value.Child(f, i);
                    long childBody = RetainBodySize(child, f.Table, ref ids);
                    w.Var((ulong)childBody);
                    RetainWriteBody(ref w, child, f.Table, ref ids, false);
                }
                else
                {
                    RetainWriteElement(ref w, value, f, i, ref ids, true);
                }
            }
        }
        else if (f.Kind == 12)
        {
            w.Raw(f.GetBuffer(value.Obj).AsSpan(0, Count(value.Obj, f)));
        }
        else
        {
            RetainWriteElement(ref w, value, f, 0, ref ids, false);
        }
    }

    static void RetainWriteBody(ref Writer w, RetainValue value, TableTypeInfo type, ref RetainIds ids, bool root)
    {
        for (int i = 0; i < type.Fields.Length; i++)
        {
            TableFieldInfo f = type.Fields[i];
            if (!RetainRides(value, f)) { continue; }
            if (!f.Optional && !f.Required && (f.Kind == 13 && !f.IsArray && RetainPayloadSize(value, f, ref ids) == 1 || f.KeyId != null && RetainArraySize(value, f, ref ids) == 2))
            {
                continue;
            }
            w.Var(ids.Reference(f.Id));
            w.Byte(Kind(f));
            if (f.IsArray || f.Kind == 12 || f.Kind == 13)
            {
                w.Var((ulong)RetainPayloadSize(value, f, ref ids));
            }
            RetainWritePayload(ref w, value, f, ref ids);
        }
        RetainTailSave(ref w, ref ids, value.Path);
        if (root) { RetainWriteNodes(ref w, ref ids); }
        w.Var(0);
    }

    internal static long SaveRetainCarrier(object carrierRoot, TableTypeInfo type, Span<byte> buffer, Span<ulong> vocabulary, Span<int> slots, TableRetain store, System.Collections.Generic.Dictionary<object, uint> oldNodes, _R report, bool measure)
    {
        if (carrierRoot == null || (!measure && report == null)) { return -1; }
        Graph graph = null;
        if (type.Variable)
        {
            graph = Number(carrierRoot, type);
            if (graph == null) { return -1; }
        }
        Ids validate = new Ids(vocabulary) { Graph = graph };
        if (!Collect(carrierRoot, type, ref validate) || !CollectNodes(ref validate)) { return -1; }
        uint rootOldNode = 0;
        if (oldNodes != null) { oldNodes.TryGetValue(carrierRoot, out rootOldNode); }
        RetainValue rootVal = new RetainValue(carrierRoot, store, rootOldNode == 0 ? default : RetainPath.ForNode(rootOldNode), false);
        RetainIds ids = new RetainIds(vocabulary, slots, store, graph, oldNodes, type);
        long n = 1 + RetainBodySize(rootVal, type, ref ids);
        if (graph != null && graph.Nodes.Count != 0)
        {
            ulong reference = ids.Reference(ulong.MaxValue);
            long nodes = RetainNodesSize(ref ids);
            n += VarSize(reference) + 1 + VarSize((ulong)nodes) + nodes;
        }
        n += 8L * ids.Count + 8;
        if (ids.Overflow) { return -1; }
        if (type.Integrity) { n += 4; }
        if (measure) { return n; }
        if (n > buffer.Length) { return -1; }
        if (store != null && store.Bytes != null)
        {
            int at = 0;
            for (int i = 0; i < store.Count; i++)
            {
                store.Bytes[at + 25] = 0;
                at += checked((int)Retain32(store.Bytes, at));
            }
        }
        Writer w = new Writer(buffer);
        w.Byte(1);
        RetainWriteBody(ref w, rootVal, type, ref ids, true);
        ids.Write(ref w);
        if (store != null && store.Bytes != null)
        {
            int at = 0;
            for (int i = 0; i < store.Count; i++)
            {
                if (store.Bytes[at + 25] == 0) { report.RetainLost++; }
                at += checked((int)Retain32(store.Bytes, at));
            }
        }
        if (type.Integrity) { w.Fixed(IntegrityCrc(buffer.Slice(0, (int)w.Offset)), 4); }
        return w.Offset;
    }
`
