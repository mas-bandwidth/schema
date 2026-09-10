package cstable

// tableFixedRuntimeTypes defines the namespace-level types for the fixed-table
// wire form (docs/SPEC-TABLES.md §3.4): TableFixedEntry, TableFixedDst,
// TableFixedLayoutEntry, TableFixedLayoutView, TableFixedPlan, and TableFixedSlot.
const tableFixedRuntimeTypes = `
// ---------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ---------------------------------------------------------------------------

// A PLAN ENTRY. Src and Dst are byte offsets or slot indices. Guard is the
// byte offset of a union tag when the entry belongs to an arm, or NoGuard.
[System.Runtime.InteropServices.StructLayout(System.Runtime.InteropServices.LayoutKind.Sequential)]
public struct TableFixedEntry
{
    public uint Src;
    public uint Dst;
    public uint Size;
    public uint Aux;
    public uint Guard;
    public byte Op;
    public byte Arg; // Arm ordinal compared by Guard
    public byte DstSize;
    public byte Sign;
    public byte Meta; // Op metadata (e.g. text flavour: 1=utf8, 2=wide, 3=bytes)

    public TableFixedEntry(uint src, uint dst, uint size, uint aux, uint guard, byte op, byte arg, byte dstSize, byte sign = 0, byte meta = 0)
    {
        Src = src;
        Dst = dst;
        Size = size;
        Aux = aux;
        Guard = guard;
        Op = op;
        Arg = arg;
        DstSize = dstSize;
        Sign = sign;
        Meta = meta;
    }
}

// TableFixedDst is MY side of the layout walk: one row per entry of my own layout,
// carrying the storage facts a layout entry cannot: destination slot index, array
// stride in slots, auxiliary slot index, counted flag, and text flavour.
[System.Runtime.InteropServices.StructLayout(System.Runtime.InteropServices.LayoutKind.Sequential)]
public struct TableFixedDst
{
    public uint Dst;
    public uint Stride;
    public uint Aux;
    public byte Counted;
    public byte Arg;

    public TableFixedDst(uint dst, uint stride, uint aux, byte counted, byte arg)
    {
        Dst = dst;
        Stride = stride;
        Aux = aux;
        Counted = counted;
        Arg = arg;
    }
}

// TableFixedLayoutEntry is one seventeen-byte entry: an id, a constant size,
// a child count, and a kind, in the writer's declared order.
[System.Runtime.InteropServices.StructLayout(System.Runtime.InteropServices.LayoutKind.Sequential)]
public struct TableFixedLayoutEntry
{
    public ulong Id;
    public uint Size;
    public uint Children;
    public byte Kind;
}

public ref struct TableFixedLayoutView
{
    public ReadOnlySpan<byte> Bytes;
    public int Count;
}

public readonly struct TableFixedPlan
{
    public readonly TableFixedEntry[] Entries;
    public readonly int Count;

    public TableFixedPlan(TableFixedEntry[] entries)
    {
        Entries = entries;
        Count = entries != null ? entries.Length : 0;
    }

    public static implicit operator ReadOnlySpan<TableFixedEntry>(TableFixedPlan p) => p.Entries != null ? p.Entries.AsSpan(0, p.Count) : ReadOnlySpan<TableFixedEntry>.Empty;
    public static implicit operator TableFixedEntry[](TableFixedPlan p) => p.Entries;
}

public delegate void TableFixedSetBytes<T>(T target, ReadOnlySpan<byte> span, int len);

public readonly struct TableFixedSlot<T>
{
    public readonly Action<T, ulong> SetRaw;
    public readonly Action<T, double> SetDouble;
    public readonly Action<T, UInt128> SetWide;
    public readonly TableFixedSetBytes<T> SetBytes;
    public readonly TableFixedSetBytes<T> SetChars;
    public readonly Action<T, ulong, TableReport> SetRawReport;

    public TableFixedSlot(
        Action<T, ulong> setRaw = null,
        Action<T, double> setDouble = null,
        Action<T, UInt128> setWide = null,
        TableFixedSetBytes<T> setBytes = null,
        TableFixedSetBytes<T> setChars = null,
        Action<T, ulong, TableReport> setRawReport = null)
    {
        SetRaw = setRaw;
        SetDouble = setDouble;
        SetWide = setWide;
        SetBytes = setBytes;
        SetChars = setChars;
        SetRawReport = setRawReport;
    }
}
`

// tableFixedWireSource defines TableFixedWire inside Schema: layout parsing,
// plan compilation, and the ONE execution loop.
const tableFixedWireSource = `
    public static class TableFixedWire
    {
        public const byte Form = 3;
        public const int HeaderBytes = 16;
        public const int HashAt = 8;
        public const uint NoGuard = 0xFFFFFFFFu;
        public const int MaxDepth = 32;
        public const uint RecordMaxBytes = 65536;

        // The opcodes for plan entries
        public const byte Copy = 0;    // move Size bytes
        public const byte Count = 1;   // a count: clamp to reader's bound
        public const byte Text = 2;    // a length, then units, then copy
        public const byte Ordinal = 3; // a variant ordinal, remapped through plan table
        public const byte Widen = 4;   // narrower source into wider destination
        public const byte Const = 5;   // constant value: e.g. remapped union tag
        public const byte WidenF = 6;  // float32 into float64
        public const byte Flat = 7;    // folded flat array copy

        // Arg on a Text entry
        public const byte TextUtf8 = 1;
        public const byte TextWide = 2;
        public const byte TextBytes = 3;

        public const int EntryBytes = 17;

        public static bool Widens(byte from, byte to)
        {
            if (from >= 6 && from <= 9 && to >= 6 && to <= 9) { return to > from; }
            if (from >= 2 && from <= 5 && to >= 2 && to <= 5) { return to > from; }
            return from == 10 && to == 11;
        }

        public static bool SignedKind(byte kind)
        {
            return kind >= 2 && kind <= 5;
        }

        public static ulong HashOf(ReadOnlySpan<byte> layout)
        {
            ulong h = 0xcbf29ce484222325ul;
            for (int i = 0; i < layout.Length; ++i)
            {
                h ^= (ulong)layout[i];
                h *= 0x100000001b3ul;
            }
            return h;
        }

        public static TableFixedLayoutEntry EntryAt(TableFixedLayoutView b, int i)
        {
            ReadOnlySpan<byte> e = b.Bytes.Slice(4 + i * EntryBytes, EntryBytes);
            TableFixedLayoutEntry outEntry = default;
            outEntry.Id = BinaryPrimitives.ReadUInt64LittleEndian(e);
            outEntry.Kind = e[8];
            outEntry.Size = BinaryPrimitives.ReadUInt32LittleEndian(e.Slice(9));
            outEntry.Children = BinaryPrimitives.ReadUInt32LittleEndian(e.Slice(13));
            return outEntry;
        }

        public static int Subtree(TableFixedLayoutView b, int i)
        {
            if (i < 0 || i >= b.Count) { return 1; }
            TableFixedLayoutEntry e = EntryAt(b, i);
            int n = 1;
            int at = i + 1;
            for (uint c = 0; c < e.Children; ++c)
            {
                if (at >= b.Count) { break; }
                int sub = Subtree(b, at);
                at += sub;
                n += sub;
            }
            return n;
        }

        public static uint UnionArmBytes(TableFixedLayoutView b, int i)
        {
            TableFixedLayoutEntry e = EntryAt(b, i);
            uint widest = 0;
            int at = i + 1;
            for (uint k = 0; k < e.Children; ++k)
            {
                TableFixedLayoutEntry a = EntryAt(b, at);
                if (a.Size > widest) { widest = a.Size; }
                at += Subtree(b, at);
            }
            return widest;
        }

        public static uint TagBytes(TableFixedLayoutView b, int i)
        {
            return EntryAt(b, i).Size - UnionArmBytes(b, i);
        }

        private static bool LeafSize(byte kind, uint size, out bool isLeaf)
        {
            isLeaf = true;
            switch (kind)
            {
                case 1:  return size == 1;                 // bool
                case 2:  return size == 1;                 // i8
                case 3:  return size == 2;                 // i16
                case 4:  return size == 4;                 // i32
                case 5:  return size == 8;                 // i64
                case 6:  return size == 1 || size == 4;    // u8, and bits(1..8)
                case 7:  return size == 2 || size == 4;    // u16, and bits(9..16)
                case 8:  return size == 4;                 // u32, and bits(17..32)
                case 9:  return size == 8;                 // u64, flags, and bits(33..64)
                case 10: return size == 4;                 // f32
                case 11: return size == 8;                 // f64
                case 17: return size == 4;                 // pointer index
                case 18: case 19: return size == 16;       // i128, u128
                case 20: case 25: return size == 1;        // fixed8, ufixed8
                case 21: case 26: return size == 2;
                case 22: case 27: return size == 4;
                case 23: case 28: return size == 8;
                case 24: case 29: return size == 16;
                case 32: return size == 0;                 // variant arm that holds nothing
            }
            isLeaf = false;
            return true;
        }

        private static bool OrdinalWidth(uint n) => n == 1 || n == 2 || n == 4 || n == 8;

        private static bool KnownKind(byte kind)
        {
            if (kind >= 1 && kind <= 30) return true;
            return kind == 32 || kind == 33 || kind == 35;
        }

        private ref struct Checker
        {
            public TableFixedLayoutView Layout;
            public string Why;
            public bool Bad;

            public void Fail(string why)
            {
                if (!Bad)
                {
                    Bad = true;
                    Why = why;
                }
            }
        }

        private static int CheckEntry(ref Checker c, int i, int depth)
        {
            if (c.Bad) return 0;
            if (i < 0 || i >= c.Layout.Count) { c.Fail("layout_tree_unclosed"); return 0; }
            if (depth > MaxDepth) { c.Fail("layout_too_deep"); return 0; }
            TableFixedLayoutEntry e = EntryAt(c.Layout, i);
            if (!KnownKind(e.Kind)) { c.Fail("layout_kind_unknown"); return 0; }
            if (e.Size > RecordMaxBytes) { c.Fail("layout_record_too_large"); return 0; }

            int at = i + 1;
            ulong sum = 0;
            uint widest = 0;
            uint firstSize = 0, secondSize = 0;
            byte firstKind = 0;
            uint firstChildren = 0;
            bool kidsAreVariants = true;
            for (uint k = 0; k < e.Children; ++k)
            {
                int child = at;
                int used = CheckEntry(ref c, child, depth + 1);
                if (c.Bad) return 0;
                TableFixedLayoutEntry ce = EntryAt(c.Layout, child);
                if (k == 0) { firstSize = ce.Size; firstKind = ce.Kind; firstChildren = ce.Children; }
                if (k == 1) { secondSize = ce.Size; }
                if (ce.Kind != 32) { kidsAreVariants = false; }
                sum += ce.Size;
                if (ce.Size > widest) { widest = ce.Size; }
                if (sum > RecordMaxBytes) { c.Fail("layout_record_too_large"); return 0; }
                at += used;
            }

            if (!LeafSize(e.Kind, e.Size, out bool isLeaf)) { c.Fail("layout_size_mismatch"); return 0; }
            if (isLeaf)
            {
                if (e.Children != 0) { c.Fail("layout_kind_invalid"); return 0; }
                return at - i;
            }
            switch (e.Kind)
            {
                case 13: // table
                    if ((ulong)e.Size != sum) { c.Fail("layout_size_mismatch"); return 0; }
                    break;
                case 35: // optional
                    if (e.Children != 1) { c.Fail("layout_kind_invalid"); return 0; }
                    if ((ulong)e.Size != sum + 1) { c.Fail("layout_size_mismatch"); return 0; }
                    break;
                case 14: // array
                    if (e.Children != 1) { c.Fail("layout_kind_invalid"); return 0; }
                    if (firstSize == 0) { c.Fail("layout_size_mismatch"); return 0; }
                    bool bare = (e.Size % firstSize) == 0;
                    bool counted = e.Size >= 4 && ((e.Size - 4) % firstSize) == 0;
                    if (!bare && !counted) { c.Fail("layout_size_mismatch"); return 0; }
                    break;
                case 16: // enum-keyed array
                    if (e.Children != 2) { c.Fail("layout_kind_invalid"); return 0; }
                    if (firstKind != 30) { c.Fail("layout_kind_invalid"); return 0; }
                    if (secondSize == 0 || (e.Size % secondSize) != 0) { c.Fail("layout_size_mismatch"); return 0; }
                    if (e.Size / secondSize < firstChildren) { c.Fail("layout_size_mismatch"); return 0; }
                    break;
                case 15: // union
                    if (e.Children == 0) { c.Fail("layout_kind_invalid"); return 0; }
                    if (e.Size <= widest) { c.Fail("layout_size_mismatch"); return 0; }
                    if (!OrdinalWidth(e.Size - widest)) { c.Fail("layout_size_mismatch"); return 0; }
                    break;
                case 30: // enum
                    if (!OrdinalWidth(e.Size)) { c.Fail("layout_size_mismatch"); return 0; }
                    if (e.Children != 0 && !kidsAreVariants) { c.Fail("layout_kind_invalid"); return 0; }
                    break;
                case 12: // string
                    if (e.Children != 0) { c.Fail("layout_kind_invalid"); return 0; }
                    if (e.Size < 4) { c.Fail("layout_size_mismatch"); return 0; }
                    break;
                case 33: // wstring
                    if (e.Children != 0) { c.Fail("layout_kind_invalid"); return 0; }
                    if (e.Size < 4 || ((e.Size - 4) % 2) != 0) { c.Fail("layout_size_mismatch"); return 0; }
                    break;
                default:
                    c.Fail("layout_kind_unknown");
                    return 0;
            }
            return at - i;
        }

        public static bool ParseLayout(ReadOnlySpan<byte> bytes, out TableFixedLayoutView view, out string why)
        {
            view = default;
            why = null;
            if (bytes.Length < 4) { why = "layout_malformed"; return false; }
            uint count = BinaryPrimitives.ReadUInt32LittleEndian(bytes);
            if (count == 0 || (long)count * EntryBytes + 4 != bytes.Length)
            {
                why = "layout_count_mismatch";
                return false;
            }
            view.Bytes = bytes;
            view.Count = (int)count;
            TableFixedLayoutEntry root = EntryAt(view, 0);
            if (root.Kind != 13)
            {
                why = "layout_kind_invalid";
                view = default;
                return false;
            }
            if (root.Size == 0 || root.Size > RecordMaxBytes)
            {
                why = "layout_record_too_large";
                view = default;
                return false;
            }
            Checker c = new Checker { Layout = view, Why = "layout_malformed", Bad = false };
            int used = CheckEntry(ref c, 0, 0);
            if (c.Bad)
            {
                why = c.Why;
                view = default;
                return false;
            }
            if (used != view.Count)
            {
                why = "layout_tree_unclosed";
                view = default;
                return false;
            }
            return true;
        }

        public static bool ParseLayout(ReadOnlySpan<byte> bytes, out TableFixedLayoutView view) => ParseLayout(bytes, out view, out _);

        public static bool ParseBlock(ReadOnlySpan<byte> bytes, out TableFixedLayoutView view) => ParseLayout(bytes, out view);

        private ref struct Compiler
        {
            public Span<TableFixedEntry> Plan;
            public int Capacity;
            public int Count;
            public int Pool;
            public bool Overflow;
            public TableReport Report;

            public void Push(in TableFixedEntry e)
            {
                int entrySize = System.Runtime.CompilerServices.Unsafe.SizeOf<TableFixedEntry>();
                int room = Capacity - (Pool + entrySize - 1) / entrySize;
                if (Count >= room) { Overflow = true; return; }
                Plan[Count++] = e;
            }

            public uint LayTable(ReadOnlySpan<ushort> values, int n)
            {
                int bytes = (n + 1) * 2;
                int entrySize = System.Runtime.CompilerServices.Unsafe.SizeOf<TableFixedEntry>();
                int total = entrySize * Capacity;
                Pool += bytes;
                if (total - Pool < Count * entrySize) { Overflow = true; return 0; }
                uint at = (uint)(total - Pool);
                Span<byte> planBytes = MemoryMarshal.AsBytes(Plan);
                BinaryPrimitives.WriteUInt16LittleEndian(planBytes.Slice((int)at), (ushort)n);
                for (int i = 0; i < n; ++i)
                {
                    BinaryPrimitives.WriteUInt16LittleEndian(planBytes.Slice((int)at + 2 + 2 * i), values[i]);
                }
                return at;
            }
        }

        private static void MatchChildren(
            ref Compiler c,
            TableFixedLayoutView theirs, int ti, uint their_at,
            TableFixedLayoutView mine, int mi, ReadOnlySpan<TableFixedDst> dst, uint my_at,
            uint guard, byte arg)
        {
            TableFixedLayoutEntry te = EntryAt(theirs, ti);
            TableFixedLayoutEntry me = EntryAt(mine, mi);
            int my_child = mi + 1;
            for (uint k = 0; k < me.Children; ++k)
            {
                TableFixedLayoutEntry mc = EntryAt(mine, my_child);
                int their_child = ti + 1;
                uint their_off = their_at;
                for (uint j = 0; j < te.Children; ++j)
                {
                    TableFixedLayoutEntry tc = EntryAt(theirs, their_child);
                    if (tc.Id == mc.Id)
                    {
                        CompileEntry(ref c, theirs, their_child, their_off, mine, my_child, dst, my_at, guard, arg);
                        break;
                    }
                    their_off += tc.Size;
                    their_child += Subtree(theirs, their_child);
                }
                my_child += Subtree(mine, my_child);
            }
            int tc_at = ti + 1;
            for (uint j = 0; j < te.Children; ++j)
            {
                TableFixedLayoutEntry tc = EntryAt(theirs, tc_at);
                bool named = false;
                int mc_at = mi + 1;
                for (uint k = 0; k < me.Children; ++k)
                {
                    if (EntryAt(mine, mc_at).Id == tc.Id) { named = true; break; }
                    mc_at += Subtree(mine, mc_at);
                }
                if (!named && c.Report != null) { c.Report.Unknown++; }
                tc_at += Subtree(theirs, tc_at);
            }
        }

        private static void CompileEntry(
            ref Compiler c,
            TableFixedLayoutView theirs, int ti, uint their_at,
            TableFixedLayoutView mine, int mi, ReadOnlySpan<TableFixedDst> dst, uint my_at,
            uint guard, byte arg)
        {
            TableFixedLayoutEntry te = EntryAt(theirs, ti);
            TableFixedLayoutEntry me = EntryAt(mine, mi);
            TableFixedDst d = dst[mi];
            uint at = my_at + d.Dst;
            uint aux_at = my_at + d.Aux;
            if (te.Kind != me.Kind)
            {
                if (Widens(te.Kind, me.Kind))
                {
                    TableFixedEntry e = new TableFixedEntry(
                        their_at, at, te.Size, 0, guard,
                        (byte)(te.Kind == 10 ? WidenF : Widen),
                        arg, (byte)me.Size, (byte)(SignedKind(te.Kind) ? 1 : 0));
                    c.Push(e);
                    return;
                }
                if (c.Report != null) { c.Report.KindMismatch++; }
                return;
            }
            switch (me.Kind)
            {
                case 35: // Optional wrapper
                {
                    TableFixedEntry e = new TableFixedEntry(their_at, aux_at, 1, 0, guard, Copy, arg, 0);
                    c.Push(e);
                    CompileEntry(ref c, theirs, ti + 1, their_at + 1, mine, mi + 1, dst, my_at, guard, arg);
                    break;
                }
                case 13: // Nested table
                {
                    MatchChildren(ref c, theirs, ti, their_at, mine, mi, dst, at, guard, arg);
                    break;
                }
                case 14: // Array
                {
                    if (d.Arg == TextBytes)
                    {
                        uint units = Math.Min(me.Size - 4u, te.Size - 4u);
                        TableFixedEntry e = new TableFixedEntry(
                            their_at, at, units, aux_at, guard, Text, arg, 0, 0, d.Arg);
                        c.Push(e);
                        break;
                    }
                    TableFixedLayoutEntry tel = EntryAt(theirs, ti + 1);
                    TableFixedLayoutEntry mel = EntryAt(mine, mi + 1);
                    uint head = d.Counted != 0 ? 4u : 0u;
                    uint their_n = tel.Size != 0 ? (te.Size - head) / tel.Size : 0u;
                    uint my_n = mel.Size != 0 ? (me.Size - head) / mel.Size : 0u;
                    uint their_base = their_at + head;
                    if (d.Counted != 0)
                    {
                        TableFixedEntry e = new TableFixedEntry(their_at, aux_at, my_n, 0, guard, Count, arg, 0);
                        c.Push(e);
                    }
                    uint n = Math.Min(their_n, my_n);
                    if (d.Stride == 0)
                    {
                        if (tel.Kind == mel.Kind && tel.Size == mel.Size)
                        {
                            TableFixedEntry e = new TableFixedEntry(their_base, at, n * mel.Size, 0, guard, Flat, arg, 0);
                            c.Push(e);
                        }
                        break;
                    }
                    for (uint i = 0; i < n; ++i)
                    {
                        CompileEntry(ref c, theirs, ti + 1, their_base + i * tel.Size,
                                     mine, mi + 1, dst, at + i * d.Stride, guard, arg);
                    }
                    break;
                }
                case 16: // Enum-keyed array
                {
                    TableFixedLayoutEntry tkey = EntryAt(theirs, ti + 1);
                    TableFixedLayoutEntry mkey = EntryAt(mine, mi + 1);
                    int tel = ti + 1 + Subtree(theirs, ti + 1);
                    int mel = mi + 1 + Subtree(mine, mi + 1);
                    TableFixedLayoutEntry tee = EntryAt(theirs, tel);
                    for (uint k = 0; k < mkey.Children; ++k)
                    {
                        ulong key_id = EntryAt(mine, mi + 2 + (int)k).Id;
                        for (uint j = 0; j < tkey.Children; ++j)
                        {
                            if (EntryAt(theirs, ti + 2 + (int)j).Id != key_id) { continue; }
                            CompileEntry(ref c, theirs, tel, their_at + j * tee.Size,
                                         mine, mel, dst, at + k * d.Stride, guard, arg);
                            break;
                        }
                    }
                    break;
                }
                case 15: // Union
                {
                    uint their_tag = TagBytes(theirs, ti);
                    uint my_tag = TagBytes(mine, mi);
                    int my_arm = mi + 1;
                    for (uint k = 0; k < me.Children; ++k)
                    {
                        TableFixedLayoutEntry ma = EntryAt(mine, my_arm);
                        int their_arm = ti + 1;
                        for (uint j = 0; j < te.Children; ++j)
                        {
                            TableFixedLayoutEntry ta = EntryAt(theirs, their_arm);
                            if (ta.Id == ma.Id)
                            {
                                TableFixedEntry tag = new TableFixedEntry(
                                    their_at, aux_at, my_tag, k + 1, their_at, Const, (byte)(j + 1), 0);
                                c.Push(tag);
                                CompileEntry(ref c, theirs, their_arm, their_at + their_tag,
                                             mine, my_arm, dst, at, their_at, (byte)(j + 1));
                                break;
                            }
                            their_arm += Subtree(theirs, their_arm);
                        }
                        my_arm += Subtree(mine, my_arm);
                    }
                    break;
                }
                case 30: // Enum
                {
                    uint n = Math.Min(te.Children, 255u);
                    ushort[] map = new ushort[n];
                    for (uint j = 0; j < n; ++j)
                    {
                        ulong vid = EntryAt(theirs, ti + 1 + (int)j).Id;
                        ushort landed = 0;
                        for (uint k = 0; k < me.Children; ++k)
                        {
                            if (EntryAt(mine, mi + 1 + (int)k).Id == vid) { landed = (ushort)(k + 1); break; }
                        }
                        map[(int)j] = landed;
                    }
                    uint aux = c.LayTable(map, (int)n);
                    TableFixedEntry e = new TableFixedEntry(
                        their_at, at, te.Size, aux, guard, Ordinal, arg, (byte)me.Size);
                    c.Push(e);
                    break;
                }
                case 12: case 33: // Text / WString
                {
                    uint units = Math.Min(me.Size - 4u, te.Size - 4u);
                    TableFixedEntry e = new TableFixedEntry(
                        their_at, at, units, aux_at, guard, Text, arg, 0, 0, d.Arg);
                    c.Push(e);
                    break;
                }
                default:
                {
                    if (te.Size == me.Size)
                    {
                        TableFixedEntry e = new TableFixedEntry(their_at, at, me.Size, 0, guard, Copy, arg, 0);
                        c.Push(e);
                    }
                    else if (te.Size < me.Size && me.Size <= 8)
                    {
                        TableFixedEntry e = new TableFixedEntry(their_at, at, te.Size, 0, guard, Widen, arg, (byte)me.Size);
                        c.Push(e);
                    }
                    else if (c.Report != null)
                    {
                        c.Report.KindMismatch++;
                    }
                    break;
                }
            }
        }

        public static int Compile(
            TableFixedLayoutView theirs,
            ReadOnlySpan<byte> my_layout,
            ReadOnlySpan<TableFixedDst> dst,
            Span<TableFixedEntry> plan,
            TableReport report)
        {
            if (plan.IsEmpty) { return -1; }
            if (!ParseLayout(my_layout, out TableFixedLayoutView mine)) { return -1; }
            Compiler c = new Compiler
            {
                Plan = plan,
                Capacity = plan.Length,
                Report = report
            };
            MatchChildren(ref c, theirs, 0, 0, mine, 0, dst, 0, NoGuard, 0);
            if (c.Overflow) { return -1; }
            return c.Count;
        }

        public static void Run<T>(
            ReadOnlySpan<TableFixedEntry> plan,
            ReadOnlySpan<TableFixedSlot<T>> slots,
            ReadOnlySpan<byte> src,
            T dst,
            TableReport report,
            ReadOnlySpan<byte> planBytes)
        {
            for (int i = 0; i < plan.Length; ++i)
            {
                ref readonly TableFixedEntry p = ref plan[i];
                if (p.Guard != NoGuard && src[(int)p.Guard] != p.Arg) { continue; }
                switch (p.Op)
                {
                    case Copy:
                    {
                        if (slots[(int)p.Dst].SetBytes != null)
                        {
                            slots[(int)p.Dst].SetBytes(dst, src.Slice((int)p.Src, (int)p.Size), (int)p.Size);
                        }
                        else if (slots[(int)p.Dst].SetRawReport != null)
                        {
                            ulong raw = p.Size switch
                            {
                                1 => src[(int)p.Src],
                                2 => BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src)),
                                4 => BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src)),
                                8 => BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src)),
                                _ => 0UL,
                            };
                            slots[(int)p.Dst].SetRawReport(dst, raw, report);
                        }
                        else if (p.Size == 1)
                        {
                            slots[(int)p.Dst].SetRaw?.Invoke(dst, src[(int)p.Src]);
                        }
                        else if (p.Size == 2)
                        {
                            slots[(int)p.Dst].SetRaw?.Invoke(dst, BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src)));
                        }
                        else if (p.Size == 4)
                        {
                            slots[(int)p.Dst].SetRaw?.Invoke(dst, BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src)));
                        }
                        else if (p.Size == 8)
                        {
                            slots[(int)p.Dst].SetRaw?.Invoke(dst, BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src)));
                        }
                        else if (p.Size == 16)
                        {
                            ulong lo = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src));
                            ulong hi = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src + 8));
                            slots[(int)p.Dst].SetWide?.Invoke(dst, new UInt128(hi, lo));
                        }
                        else
                        {
                            slots[(int)p.Dst].SetBytes?.Invoke(dst, src.Slice((int)p.Src, (int)p.Size), (int)p.Size);
                        }
                        break;
                    }
                    case Flat:
                    {
                        slots[(int)p.Dst].SetBytes?.Invoke(dst, src.Slice((int)p.Src, (int)p.Size), (int)p.Size);
                        break;
                    }
                    case Count:
                    {
                        int v = BinaryPrimitives.ReadInt32LittleEndian(src.Slice((int)p.Src));
                        if (v < 0) { v = 0; if (report != null) report.Clamped++; }
                        else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }
                        slots[(int)p.Dst].SetRaw?.Invoke(dst, (ulong)(uint)v);
                        break;
                    }
                    case Text:
                    {
                        uint unit = (p.Meta == TextWide) ? 2u : 1u;
                        uint cap = p.Size / unit;
                        int v = BinaryPrimitives.ReadInt32LittleEndian(src.Slice((int)p.Src));
                        if (v < 0) { v = 0; if (report != null) report.Clamped++; }
                        else if ((uint)v > cap) { v = (int)cap; if (report != null) report.Clamped++; }
                        slots[(int)p.Dst].SetRaw?.Invoke(dst, (ulong)(uint)v);
                        ReadOnlySpan<byte> textBytes = src.Slice((int)p.Src + 4, (int)p.Size);
                        if (p.Meta == TextWide)
                        {
                            slots[(int)p.Aux].SetChars?.Invoke(dst, textBytes, v);
                        }
                        else
                        {
                            slots[(int)p.Aux].SetBytes?.Invoke(dst, textBytes, v);
                        }
                        break;
                    }
                    case Ordinal:
                    {
                        uint raw = 0;
                        if (p.Size == 1) raw = src[(int)p.Src];
                        else if (p.Size == 2) raw = BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src));
                        else if (p.Size == 4) raw = BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src));
                        uint v = 0;
                        if (!planBytes.IsEmpty && p.Aux < (uint)planBytes.Length)
                        {
                            ReadOnlySpan<byte> tableBytes = planBytes.Slice((int)p.Aux);
                            ushort count = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes);
                            if (raw != 0 && raw <= count)
                            {
                                v = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes.Slice(2 * (int)raw));
                            }
                            if (raw > count && report != null) { report.Clamped++; }
                        }
                        if (slots[(int)p.Dst].SetRaw != null)
                        {
                            slots[(int)p.Dst].SetRaw(dst, v);
                        }
                        else
                        {
                            slots[(int)p.Dst].SetRawReport?.Invoke(dst, v, report);
                        }
                        break;
                    }
                    case Widen:
                    {
                        ulong raw = 0;
                        if (p.Size == 1) raw = src[(int)p.Src];
                        else if (p.Size == 2) raw = BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src));
                        else if (p.Size == 4) raw = BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src));
                        else if (p.Size == 8) raw = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src));
                        if (p.Sign != 0)
                        {
                            int bits = (int)p.Size * 8;
                            ulong top = 1UL << (bits - 1);
                            if ((raw & top) != 0)
                            {
                                raw |= ~((top << 1) - 1UL);
                            }
                        }
                        if (p.DstSize == 16)
                        {
                            UInt128 w = (p.Sign != 0 && (long)raw < 0) ? unchecked((UInt128)(Int128)(long)raw) : (UInt128)raw;
                            slots[(int)p.Dst].SetWide?.Invoke(dst, w);
                        }
                        else
                        {
                            slots[(int)p.Dst].SetRaw?.Invoke(dst, raw);
                        }
                        if (report != null) { report.Widened++; }
                        break;
                    }
                    case WidenF:
                    {
                        float f = BinaryPrimitives.ReadSingleLittleEndian(src.Slice((int)p.Src));
                        double d = (double)f;
                        slots[(int)p.Dst].SetDouble?.Invoke(dst, d);
                        if (report != null) { report.Widened++; }
                        break;
                    }
                    case Const:
                    {
                        if (slots[(int)p.Dst].SetRaw != null)
                        {
                            slots[(int)p.Dst].SetRaw(dst, p.Aux);
                        }
                        else
                        {
                            slots[(int)p.Dst].SetRawReport?.Invoke(dst, p.Aux, report);
                        }
                        break;
                    }
                    default: break;
                }
            }
        }
    }
`
