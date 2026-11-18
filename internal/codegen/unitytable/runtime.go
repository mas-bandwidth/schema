package unitytable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func emitRuntime(u *ir.Unit, orphans []*ir.Struct, unionOrphans []*ir.Union) []byte {
	sort.Slice(unionOrphans, func(i, j int) bool { return unionOrphans[i].Name < unionOrphans[j].Name })
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Name < orphans[j].Name })
	var types, schema strings.Builder
	saves := false
	for _, un := range unionOrphans {
		emitTableUnion(&types, &schema, u, un)
	}
	for _, st := range orphans {
		emitStruct(&types, u, st)
		emitDescriptor(&schema, u, st)
		emitConstruct(&schema, u, st)
		if Carried(st) {
			saves = true
			emitMeasure(&schema, st)
			emitSave(&schema, st)
			emitLoad(&schema, st)
		}
	}
	if fixedRuntimeNeeded(u) {
		saves = true
		emitFixedUnitHelpers(&schema, u)
	}
	schema.WriteString(runtimeTypes)
	fmt.Fprintf(&schema, wireHead, ir.BytesWireTypeId, ir.StringWireTypeId, ir.TableWireForm, ir.TableWireMessageForm,
		ir.TableWireIdTableBound, ir.TableBinaryMaxDepth)
	for _, k := range registry {
		schema.WriteString(k.runtime())
	}
	schema.WriteString(wireCheck)
	if fixedRuntimeNeeded(u) {
		schema.WriteString(fixedRuntime)
	}
	schema.WriteString("}\n")
	usings := []string{"System", "System.Buffers", "System.Buffers.Binary"}
	if saves {
		usings = append(usings, "System.Diagnostics")
	}
	usings = append(usings, "Serialize")
	var h strings.Builder
	h.WriteString(header(u, "the unit's tables", usings...))
	h.WriteString("// The Unity table runtime (docs/UNITY.md): C# 9 over netstandard2.1, no\n")
	h.WriteString("// `unsafe` code, no reflection, no runtime code generation.\n\n")
	return []byte(h.String() + wrap(u, types.String(), schema.String()))
}

const runtimeTypes = `// TableReport is one read's report (docs/SPEC-TABLES.md §6.5).
public struct TableReport
{
    public int Unknown;
    public int Widened;
    public bool Refused;
    public int Reason;
    public ulong LayoutHash;

    public const int Accepted = 0, WireDamaged = 1, KindMismatch = 2, OutOfRange = 3, MissingRequired = 4,
        NewerForm = 5, MessageFormAsFile = 6, CountOverExtentCap = 7, BlobShared = 8, NonFiniteFloat = 9,
        Truncated = 10, PreviousForm = 11, ReservedNotZero = 12, LayoutMalformed = 13, LayoutUnknown = 14,
        LayoutUnsupported = 15, BatchTooLarge = 16, NoLayout = 17, PlanTooLarge = 18,
        CountOverBound = 19, LengthOverBound = 20, TextIllFormed = 21, IntegrityMismatch = 22;

    public static string ReasonName(int reason)
    {
        switch (reason)
        {
            case Accepted: return "accepted";
            case WireDamaged: return "wire_damaged";
            case KindMismatch: return "kind_mismatch";
            case OutOfRange: return "out_of_range";
            case MissingRequired: return "missing_required";
            case NewerForm: return "newer_form";
            case MessageFormAsFile: return "message_form_as_file";
            case CountOverExtentCap: return "count_over_extent_cap";
            case BlobShared: return "blob_shared";
            case NonFiniteFloat: return "non_finite_float";
            case Truncated: return "truncated";
            case PreviousForm: return "previous_form";
            case ReservedNotZero: return "reserved_not_zero";
            case LayoutMalformed: return "layout_malformed";
            case LayoutUnknown: return "layout_unknown";
            case LayoutUnsupported: return "layout_unsupported";
            case BatchTooLarge: return "batch_too_large";
            case NoLayout: return "no_layout";
            case PlanTooLarge: return "plan_too_large";
            case CountOverBound: return "count_over_bound";
            case LengthOverBound: return "length_over_bound";
            case TextIllFormed: return "text_ill_formed";
            case IntegrityMismatch: return "integrity_mismatch";
        }
        return "unknown_reason";
    }
}

public readonly struct TableFieldInfo
{
    public readonly ulong Id;
    public readonly byte Kind;
    public readonly byte Codec;
    public readonly ushort Ordinal;
    public readonly long Lo;
    public readonly ulong Hi;
    public readonly ulong Default;
    public readonly long Bound;
    public readonly byte Flags;

    public const byte Pointer = 1, Optional = 2, Array = 4, Required = 8, Guarded = 16, Deprecated = 32, Map = 64;

    public TableFieldInfo(ulong id, byte kind, byte codec, ushort ordinal, long lo, ulong hi, ulong defaultBits, long bound, byte flags)
    {
        Id = id;
        Kind = kind;
        Codec = codec;
        Ordinal = ordinal;
        Lo = lo;
        Hi = hi;
        Default = defaultBits;
        Bound = bound;
        Flags = flags;
    }
}

public sealed class TableTypeInfo
{
    public readonly string Name;
    public readonly ulong TypeId;
    public readonly bool Variable;
    public readonly bool Carried;
    public readonly string[] FieldNames;
    public readonly TableFieldInfo[] Fields;

    public TableTypeInfo(string name, ulong typeId, bool variable, bool carried, string[] fieldNames, TableFieldInfo[] fields)
    {
        Name = name;
        TypeId = typeId;
        Variable = variable;
        Carried = carried;
        FieldNames = fieldNames;
        Fields = fields;
    }
}

`

const wireHead = `internal static class TableWire
{
    internal const ulong NodeTableId = 0xFFFFFFFFFFFFFFFFUL, BuildVersionId = 0xFFFFFFFFFFFFFFFEUL, VocabularyId = 0xFFFFFFFFFFFFFFFDUL;
    internal const ulong BytesTypeId = 0x%016xUL, StringTypeId = 0x%016xUL;
    internal const byte FileForm = %d, MessageForm = %d;
    internal const int IdTableBound = %d, MaxDepth = %d;

    private static readonly uint[] IntegrityTable = MakeIntegrityTable();

    private static uint[] MakeIntegrityTable()
    {
        uint[] table = new uint[256];
        for (uint i = 0; i < 256; i++)
        {
            uint crc = i;
            for (int bit = 0; bit < 8; bit++) { crc = (crc >> 1) ^ ((crc & 1) != 0 ? 0x82f63b78u : 0u); }
            table[i] = crc;
        }
        return table;
    }

    internal static uint IntegrityCrc(ReadOnlySpan<byte> bytes)
    {
        uint crc = 0xffffffffu;
        for (int i = 0; i < bytes.Length; i++) { crc = (crc >> 8) ^ IntegrityTable[(crc ^ bytes[i]) & 255]; }
        return crc ^ 0xffffffffu;
    }

    internal static bool IntegrityRead(ref ReadOnlySpan<byte> bytes)
    {
        if (bytes.Length < 5) { return false; }
        int end = bytes.Length - 4;
        uint stored = BinaryPrimitives.ReadUInt32LittleEndian(bytes.Slice(end, 4));
        if (stored != IntegrityCrc(bytes.Slice(0, end))) { return false; }
        bytes = bytes.Slice(0, end);
        return true;
    }

    internal static int Width(byte kind)
    {
        switch (kind)
        {
            case 1: case 2: case 6: case 20: case 25: return 1;
            case 3: case 7: case 21: case 26: return 2;
            case 4: case 8: case 10: case 22: case 27: return 4;
            case 5: case 9: case 11: case 23: case 28: case 36: return 8;
            case 18: case 19: case 24: case 29: return 16;
        }
        return 0;
    }

    internal static bool Signed(byte kind)
    {
        return (kind >= 2 && kind <= 5) || kind == 18 || (kind >= 20 && kind <= 24);
    }

    internal static bool Widens(byte kind, byte declared)
    {
        switch (declared)
        {
            case 3: case 4: case 5: return kind >= 2 && kind < declared;
            case 18: return kind >= 2 && kind <= 5;
            case 7: case 8: case 9: return kind >= 6 && kind < declared;
            case 19: return kind >= 6 && kind <= 9;
            case 11: return kind == 10;
        }
        return false;
    }

    internal static bool Leb(ReadOnlySpan<byte> b, ref int off, out ulong v)
    {
        v = 0;
        for (int i = 0; i < 10; i++)
        {
            if (off + i >= b.Length) { return false; }
            byte c = b[off + i];
            if (i == 9 && c > 1) { return false; }
            v |= (ulong)(c & 127) << (7 * i);
            if (c < 128)
            {
                if (i > 0 && c == 0) { return false; }
                off += i + 1;
                return true;
            }
        }
        return false;
    }

    internal static ulong Ref(ReadOnlySpan<byte> b, ref int off)
    {
        Leb(b, ref off, out ulong v);
        return v;
    }

    internal static int LebSize(ulong v)
    {
        int n = 1;
        while (v >= 128) { v >>= 7; n++; }
        return n;
    }

    internal static void PutLeb(Span<byte> b, ref int off, ulong v)
    {
        while (v >= 128) { b[off++] = (byte)(v | 128); v >>= 7; }
        b[off++] = (byte)v;
    }

    internal static void PutWidth(Span<byte> b, ref int off, int w, ulong v)
    {
        for (int i = 0; i < w; i++) { b[off++] = (byte)(v >> (8 * i)); }
    }

    internal static ulong Id(ReadOnlySpan<byte> ids, ulong reference)
    {
        return BinaryPrimitives.ReadUInt64LittleEndian(ids.Slice((int)(reference - 1) * 8, 8));
    }

    internal static bool Int(ReadOnlySpan<byte> body, ref int off, byte kind, byte declared, ref int widened, out long value)
    {
        value = 0;
        if (kind != declared)
        {
            if (!Widens(kind, declared)) { return false; }
            widened++;
        }
        int w = Width(kind);
        ulong raw = 0;
        for (int i = 0; i < w; i++) { raw |= (ulong)body[off + i] << (8 * i); }
        off += w;
        if (Signed(kind) && w < 8)
        {
            int shift = 64 - w * 8;
            value = (long)(raw << shift) >> shift;
        }
        else
        {
            value = unchecked((long)raw);
        }
        return true;
    }

    internal static void Skip(ReadOnlySpan<byte> b, ref int off, byte kind)
    {
        int w = Width(kind);
        if (w != 0) { off += w; return; }
        ulong n;
        switch (kind)
        {
            case 17:
            case 30:
                Leb(b, ref off, out _);
                return;
            case 15:
                Leb(b, ref off, out ulong arm);
                if (arm == 0) { return; }
                off++;
                Leb(b, ref off, out n);
                off += (int)n;
                return;
        }
        Leb(b, ref off, out n);
        off += (int)n;
    }

    internal static int Open(ReadOnlySpan<byte> bytes, out ReadOnlySpan<byte> body, out ReadOnlySpan<byte> ids, out int records, out bool nonFinite)
    {
        body = default;
        ids = default;
        records = 0;
        nonFinite = false;
        if (bytes.Length < 1) { return TableReport.WireDamaged; }
        if (bytes[0] != FileForm)
        {
            return bytes[0] == MessageForm ? TableReport.MessageFormAsFile : TableReport.NewerForm;
        }
        if (bytes.Length < 9) { return TableReport.WireDamaged; }
        ulong count = BinaryPrimitives.ReadUInt64LittleEndian(bytes.Slice(bytes.Length - 8));
        if (count > (ulong)bytes.Length / 8 || count > IdTableBound) { return TableReport.WireDamaged; }
        int span = (int)count * 8 + 8;
        if (span + 1 > bytes.Length) { return TableReport.WireDamaged; }
        int first = bytes.Length - span;
        ids = bytes.Slice(first, (int)count * 8);
        body = bytes.Slice(1, first - 1);
        if (count > 1)
        {
            ulong[] sorted = ArrayPool<ulong>.Shared.Rent((int)count);
            try
            {
                for (int i = 0; i < (int)count; i++) { sorted[i] = Id(ids, (ulong)i + 1); }
                Array.Sort(sorted, 0, (int)count);
                for (int i = 1; i < (int)count; i++) { if (sorted[i] == sorted[i - 1]) { return TableReport.WireDamaged; } }
            }
            finally { ArrayPool<ulong>.Shared.Return(sorted); }
        }
        WireWalk w = new WireWalk { Ids = ids, Count = (int)count, Words = (int)count / 64 + 1 };
        try
        {
            int fail = w.Run(body);
            records = w.Records;
            nonFinite = w.NonFinite;
            return fail;
        }
        finally { w.Release(); }
    }
`

const wireCheck = `    ref struct WireWalk
    {
        public ReadOnlySpan<byte> Ids, Table;
        public int Count, Words, Depth, Fail, Records, EdgeCount, Next;
        public bool NonFinite, Present, OrderBad;
        public ulong[] Used, Edges;
        public int[] Starts;

        public void Release()
        {
            if (Used != null) { ArrayPool<ulong>.Shared.Return(Used); Used = null; }
            if (Edges != null) { ArrayPool<ulong>.Shared.Return(Edges); Edges = null; }
            if (Starts != null) { ArrayPool<int>.Shared.Return(Starts); Starts = null; }
        }

        bool Damage()
        {
            if (Fail == TableReport.Accepted) { Fail = TableReport.WireDamaged; }
            return false;
        }

        ulong IdAt(ulong reference) { return Id(Ids, reference); }

        void Use(ulong reference)
        {
            int r = (int)reference;
            ulong bit = 1UL << (r & 63);
            if ((Used[r >> 6] & bit) != 0) { return; }
            Used[r >> 6] |= bit;
            if (r - 1 != Next) { OrderBad = true; }
            Next++;
        }

        void Edge(ulong index)
        {
            if (Edges == null) { Edges = ArrayPool<ulong>.Shared.Rent(16); }
            else if (EdgeCount == Edges.Length)
            {
                ulong[] grown = ArrayPool<ulong>.Shared.Rent(Edges.Length * 2);
                Array.Copy(Edges, grown, EdgeCount);
                ArrayPool<ulong>.Shared.Return(Edges);
                Edges = grown;
            }
            Edges[EdgeCount++] = index;
        }

        public int Run(ReadOnlySpan<byte> body)
        {
            Used = ArrayPool<ulong>.Shared.Rent(Words);
            Array.Clear(Used, 0, Words);
            if (!Body(body, true)) { return Fail; }
            int rootEdges = EdgeCount;
            if (Present)
            {
                ReadOnlySpan<byte> t = Table;
                int off = 0;
                if (!Leb(t, ref off, out ulong declared)) { Damage(); return Fail; }
                int first = off;
                int records = 0;
                while (off < t.Length)
                {
                    if (!Leb(t, ref off, out ulong reference) || reference == 0 || reference > (ulong)Count) { Damage(); return Fail; }
                    if (!Leb(t, ref off, out ulong length) || length > (ulong)(t.Length - off)) { Damage(); return Fail; }
                    if (IdAt(reference) >= BuildVersionId - 1) { Damage(); return Fail; }
                    off += (int)length;
                    records++;
                }
                if (declared != (ulong)records) { Damage(); return Fail; }
                if (records == 0) { Damage(); return Fail; }
                Starts = ArrayPool<int>.Shared.Rent(records + 2);
                Starts[0] = 0;
                Starts[1] = rootEdges;
                off = first;
                for (int k = 0; k < records; k++)
                {
                    Leb(t, ref off, out ulong reference);
                    Leb(t, ref off, out ulong length);
                    ReadOnlySpan<byte> rec = t.Slice(off, (int)length);
                    off += (int)length;
                    Use(reference);
                    ulong id = IdAt(reference);
                    if (id == StringTypeId) { if (!TextValid(rec)) { Damage(); return Fail; } }
                    else if (id != BytesTypeId) { if (!Body(rec, false)) { return Fail; } }
                    Starts[k + 2] = EdgeCount;
                }
                Records = records;
            }
            if (!Graph()) { return Fail; }
            if (OrderBad || Next != Count) { Damage(); }
            return Fail;
        }

        bool Graph()
        {
            int n = Records + 1;
            int[] marks = ArrayPool<int>.Shared.Rent(n * 4);
            try
            {
                Span<int> name = marks.AsSpan(0, n), mark = marks.AsSpan(n, n), next = marks.AsSpan(2 * n, n), path = marks.AsSpan(3 * n, n);
                name.Clear(); mark.Clear(); next.Clear();
                for (int e = 0; e < EdgeCount; e++)
                {
                    if (Edges[e] == 0) { continue; }
                    if (Edges[e] - 1 >= (ulong)n) { return Damage(); }
                    name[(int)(Edges[e] - 1)]++;
                }
                if (n > 1)
                {
                    ReadOnlySpan<byte> t = Table;
                    int off = 0;
                    Leb(t, ref off, out _);
                    for (int k = 1; k < n; k++)
                    {
                        Leb(t, ref off, out ulong reference);
                        Leb(t, ref off, out ulong length);
                        off += (int)length;
                        ulong id = IdAt(reference);
                        if ((id == StringTypeId || id == BytesTypeId) && name[k] > 1)
                        {
                            if (Fail == TableReport.Accepted) { Fail = TableReport.BlobShared; }
                            return false;
                        }
                    }
                }
                int top = 0;
                path[0] = 0;
                mark[0] = 1;
                while (top >= 0)
                {
                    int node = path[top];
                    int from = Starts == null ? 0 : Starts[node];
                    int to = Starts == null ? EdgeCount : Starts[node + 1];
                    if (from + next[node] == to) { mark[node] = 2; top--; continue; }
                    ulong index = Edges[from + next[node]];
                    next[node]++;
                    if (index == 0) { continue; }
                    int target = (int)(index - 1);
                    if (mark[target] == 1) { return Damage(); }
                    if (mark[target] == 2) { continue; }
                    mark[target] = 1;
                    path[++top] = target;
                }
                for (int k = 1; k < n; k++) { if (mark[k] == 0) { return Damage(); } }
                return true;
            }
            finally { ArrayPool<int>.Shared.Return(marks); }
        }

        bool Body(ReadOnlySpan<byte> buf, bool root)
        {
            Span<ulong> seen = stackalloc ulong[Words];
            seen.Clear();
            int off = 0;
            bool nodeTable = false;
            while (true)
            {
                if (!Leb(buf, ref off, out ulong reference)) { return Damage(); }
                if (reference == 0) { return off == buf.Length || Damage(); }
                if (nodeTable) { return Damage(); }
                if (reference > (ulong)Count) { return Damage(); }
                Use(reference);
                int r = (int)reference;
                if ((seen[r >> 6] & (1UL << (r & 63))) != 0) { return Damage(); }
                seen[r >> 6] |= 1UL << (r & 63);
                if (off >= buf.Length) { return Damage(); }
                byte kind = buf[off++];
                ulong id = IdAt(reference);
                if (id == BuildVersionId || id == VocabularyId) { return Damage(); }
                if (id == NodeTableId)
                {
                    if (!root || kind != 12) { return Damage(); }
                    if (!Leb(buf, ref off, out ulong n) || n > (ulong)(buf.Length - off)) { return Damage(); }
                    Table = buf.Slice(off, (int)n);
                    Present = true;
                    off += (int)n;
                    nodeTable = true;
                    continue;
                }
                if (!Payload(buf, ref off, kind)) { return false; }
            }
        }

        bool Payload(ReadOnlySpan<byte> buf, ref int off, byte kind)
        {
            int w = Width(kind);
            if (w != 0)
            {
                if (w > buf.Length - off) { return Damage(); }
                off += w;
                return Scalar(buf.Slice(off - w, w), kind);
            }
            switch (kind)
            {
                case 17:
                    {
                        if (!Leb(buf, ref off, out ulong index)) { return Damage(); }
                        Edge(index);
                        return true;
                    }
                case 30:
                    {
                        if (!Leb(buf, ref off, out ulong variant) || variant > (ulong)Count) { return Damage(); }
                        if (variant != 0) { Use(variant); }
                        return true;
                    }
                case 15:
                    return Union(buf, ref off);
                case 12:
                case 31:
                case 32:
                case 13:
                case 14:
                case 16:
                    {
                        if (!Leb(buf, ref off, out ulong n) || n > (ulong)(buf.Length - off)) { return Damage(); }
                        off += (int)n;
                        return Content(buf.Slice(off - (int)n, (int)n), kind);
                    }
            }
            return Damage();
        }

        bool Content(ReadOnlySpan<byte> b, byte kind)
        {
            int w = Width(kind);
            if (w != 0) { return b.Length == w ? Scalar(b, kind) : Damage(); }
            switch (kind)
            {
                case 12: return TextValid(b) || Damage();
                case 31: return true;
                case 32: return b.Length == 0 || Damage();
                case 13:
                case 14:
                case 16:
                    {
                        if (Depth >= MaxDepth) { return Damage(); }
                        Depth++;
                        bool good = kind == 13 ? Body(b, false) : kind == 14 ? ArrayBody(b) : KeyedBody(b);
                        Depth--;
                        return good;
                    }
                case 17:
                case 30:
                case 15:
                    {
                        int end = 0;
                        if (!Payload(b, ref end, kind)) { return false; }
                        return end == b.Length || Damage();
                    }
            }
            return Damage();
        }

        bool Scalar(ReadOnlySpan<byte> b, byte kind)
        {
            if (kind == 1 && b[0] > 1) { return Damage(); }
            if (kind == 10 && (BinaryPrimitives.ReadUInt32LittleEndian(b) & 0x7F800000u) == 0x7F800000u) { NonFinite = true; }
            if (kind == 11 && (BinaryPrimitives.ReadUInt64LittleEndian(b) & 0x7FF0000000000000UL) == 0x7FF0000000000000UL) { NonFinite = true; }
            return true;
        }

        bool Header(ReadOnlySpan<byte> b, out byte kind, out ulong count, out int off)
        {
            kind = 0;
            count = 0;
            off = 1;
            if (b.Length < 2) { return Damage(); }
            kind = b[0];
            if (!Leb(b, ref off, out count)) { return Damage(); }
            if (count > int.MaxValue)
            {
                if (Fail == TableReport.Accepted) { Fail = TableReport.CountOverExtentCap; }
                return false;
            }
            return true;
        }

        static bool ArrayKind(byte k) { return (k >= 1 && k <= 13) || k == 15 || k == 17 || (k >= 18 && k <= 29) || k == 30; }
        static bool KeyedKind(byte k) { return (k >= 1 && k <= 11) || k == 13 || (k >= 18 && k <= 29) || k == 30; }

        bool ArrayBody(ReadOnlySpan<byte> b)
        {
            if (!Header(b, out byte kind, out ulong count, out int off)) { return false; }
            if (!ArrayKind(kind)) { return Damage(); }
            for (ulong i = 0; i < count; i++) { if (!Payload(b, ref off, kind)) { return false; } }
            return off == b.Length || Damage();
        }

        bool KeyedBody(ReadOnlySpan<byte> b)
        {
            if (!Header(b, out byte kind, out ulong count, out int off)) { return false; }
            if (!KeyedKind(kind)) { return Damage(); }
            Span<ulong> seen = stackalloc ulong[Words];
            seen.Clear();
            for (ulong i = 0; i < count; i++)
            {
                if (!Leb(b, ref off, out ulong key) || key == 0 || key > (ulong)Count) { return Damage(); }
                Use(key);
                int k = (int)key;
                if ((seen[k >> 6] & (1UL << (k & 63))) != 0) { return Damage(); }
                seen[k >> 6] |= 1UL << (k & 63);
                if (!Leb(b, ref off, out ulong n) || n > (ulong)(b.Length - off)) { return Damage(); }
                off += (int)n;
                if (!Content(b.Slice(off - (int)n, (int)n), kind)) { return false; }
            }
            return off == b.Length || Damage();
        }

        bool Union(ReadOnlySpan<byte> buf, ref int off)
        {
            if (!Leb(buf, ref off, out ulong arm)) { return Damage(); }
            if (arm == 0) { return true; }
            if (arm > (ulong)Count) { return Damage(); }
            Use(arm);
            if (off >= buf.Length) { return Damage(); }
            byte kind = buf[off++];
            if (!Leb(buf, ref off, out ulong n) || n > (ulong)(buf.Length - off)) { return Damage(); }
            off += (int)n;
            ReadOnlySpan<byte> content = buf.Slice(off - (int)n, (int)n);
            if (kind != 13 && kind != 14 && kind != 16 && kind != 15) { return Content(content, kind); }
            if (Depth >= MaxDepth) { return Damage(); }
            Depth++;
            bool good = Content(content, kind);
            Depth--;
            return good;
        }
    }

    internal static bool TextValid(ReadOnlySpan<byte> b)
    {
        int i = 0;
        while (i < b.Length)
        {
            byte c = b[i];
            if (c == 0) { return false; }
            if (c < 0x80) { i++; continue; }
            int n;
            uint cp, min;
            if (c >= 0xC2 && c <= 0xDF) { n = 1; cp = c & 0x1Fu; min = 0x80; }
            else if (c >= 0xE0 && c <= 0xEF) { n = 2; cp = c & 0x0Fu; min = 0x800; }
            else if (c >= 0xF0 && c <= 0xF4) { n = 3; cp = c & 0x07u; min = 0x10000; }
            else { return false; }
            if (i + n >= b.Length) { return false; }
            for (int k = 1; k <= n; k++)
            {
                byte d = b[i + k];
                if ((d & 0xC0) != 0x80) { return false; }
                cp = (cp << 6) | (d & 0x3Fu);
            }
            if (cp < min || cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF)) { return false; }
            i += n + 1;
        }
        return true;
    }
`
