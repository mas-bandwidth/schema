package cstable

// tableFixedRuntimeTypes defines the namespace-level types for the fixed-table
// wire form (docs/SPEC-TABLES.md §3.4): TableFixedEntry, TableFixedDst,
// TableFixedLayoutEntry, TableFixedLayoutView, TableFixedPlan, TableFixedFill,
// and TableFixedSlot.
const tableFixedRuntimeTypes = `
// ---------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ---------------------------------------------------------------------------

// A PLAN ENTRY. Src and Dst are byte offsets or slot indices. Guard is the
// byte offset of a union tag when the entry belongs to an arm, or NoGuard.
// Arg is THE GUARD'S TAG and nothing else. Meta is THE OP'S OWN ARGUMENT —
// a text entry's flavour. ArgW is THE GUARD'S WIDTH IN BYTES: a union tag
// is one, two, four or eight, and comparing only the FIRST of them fires
// arm 1 on a foreign tag of 0x0101. Zero is read as one. Flavour stays in
// Meta; they are two lanes because they are two facts.
[System.Runtime.InteropServices.StructLayout(System.Runtime.InteropServices.LayoutKind.Sequential)]
public struct TableFixedEntry
{
    public uint Src;
    public uint Dst;
    public uint Size;
    public uint Aux;
    public uint Guard;
    public byte Op;
    public ulong Arg; // Arm ordinal compared by Guard, at ArgW bytes
    public byte DstSize;
    public byte Sign;
    public byte Meta; // Op metadata (e.g. text flavour: 1=utf8, 2=wide, 3=bytes)
    public byte ArgW; // THE GUARD'S WIDTH IN BYTES. Zero is read as one.
    // Guard2 / Arg2 / ArgW2 ARE THE OUTER TAG AN ARM INSIDE AN ARM ALSO ANSWERS
    // TO, CONJOINED with Guard/Arg: the entry runs only when BOTH tags hold
    // their ordinal (docs/FIXED-FORM-ALGORITHM.md §4.1). Arg2 carries the outer
    // arm ordinal and its own lane carries NO BYTE: it is full width like Arg.
    // NoGuard on Guard2 is "no second condition", which is every entry that is
    // not nested.
    public uint Guard2;
    public ulong Arg2; // The OUTER arm ordinal compared by Guard2, at ArgW2 bytes
    public byte ArgW2; // THE OUTER GUARD'S WIDTH IN BYTES. Zero is read as one.
    // GuardIndex / GuardCount ARE THE GUARD CHAIN (docs/FIXED-FORM-ALGORITHM.md
    // §4.1): offset+count into a pool of (guard, arg, argw) links, OUTERMOST
    // FIRST, the only bound the layout's 64. GuardIndex is a BYTE OFFSET into
    // the plan's bytes (the identity plan's guard pool, or the compiled plan's
    // own storage behind its entries), a link GuardLinkBytes long. GuardCount 0
    // is no chain: a chain of one or two is the two-lane special case above.
    public uint GuardIndex;
    public uint GuardCount;

    public TableFixedEntry(uint src, uint dst, uint size, uint aux, uint guard, byte op, ulong arg, byte dstSize, byte sign = 0, byte meta = 0, byte argW = 1, uint guard2 = 0xFFFFFFFFu, ulong arg2 = 0, byte argW2 = 1, uint guardIndex = 0, uint guardCount = 0)
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
        ArgW = argW;
        Guard2 = guard2;
        Arg2 = arg2;
        ArgW2 = argW2;
        GuardIndex = guardIndex;
        GuardCount = guardCount;
    }
}

// TableFixedGuardLink is ONE LINK of a guard chain in the compiler's hand
// (docs/FIXED-FORM-ALGORITHM.md §4.1): the tag's offset, the ordinal it must
// hold and its width. The pool lays it as TableFixedWire.GuardLinkBytes bytes.
public struct TableFixedGuardLink
{
    public uint Guard;
    public ulong Arg;
    public byte ArgW;
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

// ONE SLOT RANGE THE PLAN DOES NOT LAND. Dst is a slot index, Size is how
// many consecutive slots. The identity plan's list is empty: its destinations
// ARE the value slots, so there is nothing left to subtract.
[System.Runtime.InteropServices.StructLayout(System.Runtime.InteropServices.LayoutKind.Sequential)]
public struct TableFixedFill
{
    public uint Dst;
    public uint Size;

    public TableFixedFill(uint dst, uint size)
    {
        Dst = dst;
        Size = size;
    }
}

// ---- THE LINEAGE, AS STATIC DATA (docs/FIXED-FORM-ALGORITHM.md §5) ---------
//
// A fixed table reads BACKWARD and never forward. A file is matched on the eight
// bytes of its header's hash against the lineage the BUILD laid down, and the
// layout it carries is held to a BYTE COMPARISON against the bytes the lock
// recorded — never walked. A hash the lineage does not hold is layout_newer
// (ship the reader); a hash it holds below the floor is layout_unsupported
// (upgrade the client). Those are the operator's two distinct answers.

// TableFixedKnownLayout is one locked layout: the wire hash a file is matched
// on, the layout bytes verbatim, the byte length riding BESIDE the pointer, and
// the RECORD SIZE taken from the lock and never from the file.
//
// §5.9 #19 names the four members in this order — hash, layout, layout_bytes,
// record_bytes — the byte length beside the pointer rather than inside it, the
// way §4.1 names the plan entry's lanes; tools/fixedtwin holds the C and C++
// pair to the same text, and §5.2 makes the names the contract. A C# array
// carries its own length, so LayoutBytes is the length of Layout rather than a
// second fact, but the member rides so the four names and their order are the
// page's on every leg.
public readonly struct TableFixedKnownLayout
{
    public readonly ulong Hash;
    public readonly byte[] Layout;
    public readonly int LayoutBytes;
    public readonly long RecordBytes;

    public TableFixedKnownLayout(ulong hash, byte[] layout, long record)
    {
        Hash = hash;
        Layout = layout;
        LayoutBytes = layout.Length;
        RecordBytes = record;
    }
}

// TableFixedLineagePlan is ONE older entry's plan, laid down once from THE
// LOCK'S bytes. Unknown and KindMismatch are the COMPILE CENSUS: a writer field
// no reader field names is counted ONCE PER ENDPOINT and never per record (§5.4),
// and LOAD carries them onto the report after the record loop, on a read that
// returns (§5.9 #6). Why is the name a load refuses by when the entry itself
// could not be compiled — a bug in the lock, which at BUILD time fails the
// build and at run time owes a name rather than a crash (§5.9 #8).
public sealed class TableFixedLineagePlan
{
    public TableFixedEntry[] Entries = Array.Empty<TableFixedEntry>();
    public int Count;
    public int Split; // THE SPLIT (§5.9 #42): the number of UNGUARDED entries, the index the guarded half starts at
    public int Unknown;
    public int KindMismatch;
    public string Why;
}

public readonly struct TableFixedSlot<T>
{
    public readonly Action<T, ulong> SetRaw;
    public readonly Action<T, double> SetDouble;
    public readonly Action<T, UInt128> SetWide;
    public readonly TableFixedSetBytes<T> SetBytes;
    public readonly TableFixedSetBytes<T> SetChars;
    public readonly Action<T, ulong, TableReport> SetRawReport;
    public readonly Action<T> Reset; // declared default into this slot, and nowhere else

    public TableFixedSlot(
        Action<T, ulong> setRaw = null,
        Action<T, double> setDouble = null,
        Action<T, UInt128> setWide = null,
        TableFixedSetBytes<T> setBytes = null,
        TableFixedSetBytes<T> setChars = null,
        Action<T, ulong, TableReport> setRawReport = null,
        Action<T> reset = null)
    {
        SetRaw = setRaw;
        SetDouble = setDouble;
        SetWide = setWide;
        SetBytes = setBytes;
        SetChars = setChars;
        SetRawReport = setRawReport;
        Reset = reset;
    }
}
`

// tableFixedWireSource defines TableFixedWire inside Schema: layout parsing,
// plan compilation, and the ONE execution loop.
const tableFixedWireSource = `
public static class TableFixedWire
{
    // FLAT ELEMENT ARRAYS IN THE WIRE'S BYTE ORDER (docs/FIXED-FORM-ALGORITHM.md:
    // every integer is little-endian; docs/PORTING.md M12). On a little-endian
    // host the bulk copy IS the wire image, so it stays one copy; on a
    // big-endian host every element's bytes are reversed after it. The two
    // copies below are the only host-order reinterpretations of wire bytes in
    // the fixed form, and each is followed by that swap.
    public static void ReadLittle<T>(ReadOnlySpan<byte> src, Span<T> dst) where T : unmanaged
    {
        int width = System.Runtime.CompilerServices.Unsafe.SizeOf<T>();
        int n = Math.Min(src.Length / width, dst.Length);
        MemoryMarshal.Cast<byte, T>(src.Slice(0, n * width)).CopyTo(dst); /* byte order: swapped below on a big-endian host */
        if (!BitConverter.IsLittleEndian) { ReverseEach(MemoryMarshal.AsBytes(dst.Slice(0, n)), width); }
    }
    public static void WriteLittle<T>(ReadOnlySpan<T> src, Span<byte> dst) where T : unmanaged
    {
        int width = System.Runtime.CompilerServices.Unsafe.SizeOf<T>();
        MemoryMarshal.AsBytes(src).CopyTo(dst); /* byte order: swapped below on a big-endian host */
        if (!BitConverter.IsLittleEndian) { ReverseEach(dst.Slice(0, src.Length * width), width); }
    }
    // Reverses each width-byte group of bytes in place: one element's host image
    // to its little-endian image, and back.
    public static void ReverseEach(Span<byte> bytes, int width)
    {
        if (width == 1) { return; }
        for (int i = 0; i + width <= bytes.Length; i += width) { bytes.Slice(i, width).Reverse(); }
    }

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
    // A FOLDED RUN WHOSE TWO IMAGES DIFFER IN WIDTH (§5.2 EMIT kind 14, and
    // §6's flat-element fold). The fold's premise is that the element's
    // STORAGE image IS its WIRE image, and a widening breaks that premise: the
    // writer's run is narrower than the reader's storage, element for element.
    // §5.2's EMIT has no fold at all — it emits the element min(their_n, my_n)
    // times — and a FOLDED destination has no element-wise slot to aim those
    // entries at, so the widening happens inside ONE entry here. Sign is 0 for
    // a zero-extend, 1 for a sign-extend and 2 for f32 into f64; Meta is the
    // WRITER's element width and DstSize the READER's.
    public const byte FlatWiden = 8;

    // Arg on a Text entry
    public const byte TextUtf8 = 1;
    public const byte TextWide = 2;
    public const byte TextBytes = 3;

    public const int EntryBytes = 17;

    public static bool Widens(byte from, byte to)
    {
        if (from >= 6 && from <= 9 && to >= 6 && to <= 9) { return to > from; }
        if (from >= 2 && from <= 5 && to >= 2 && to <= 5) { return to > from; }
        // THE FIXED-POINT RUNGS, signed 20..24 and unsigned 25..29: a
        // fixed(I,F) into a wider I at equal F is a LADDER widen and not a
        // kind that moved (docs/FIXED-FORM-ALGORITHM.md §1, §5.1).
        if (from >= 20 && from <= 24 && to >= 20 && to <= 24) { return to > from; }
        if (from >= 25 && from <= 29 && to >= 25 && to <= 29) { return to > from; }
        return from == 10 && to == 11;
    }

    // A WIDEN'S SIGN IS THE LADDER'S: the source sign-extends when the
    // WRITER's kind is signed — i8..i64 and a SIGNED fixed(I,F), 20..24 —
    // and zero-extends otherwise (§5.2). The SAME-KIND widen has no sign at
    // all and the grown enum ordinal none either: taken as stated, not
    // inferred from the kind's signedness.
    public static bool SignedKind(byte kind)
    {
        return (kind >= 2 && kind <= 5) || (kind >= 20 && kind <= 24);
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
            case 1: return size == 1;                 // bool
            case 2: return size == 1;                 // i8
            case 3: return size == 2;                 // i16
            case 4: return size == 4;                 // i32
            case 5: return size == 8;                 // i64
            case 6: return size == 1 || size == 4;    // u8, and bits(1..8)
            case 7: return size == 2 || size == 4;    // u16, and bits(9..16)
            case 8: return size == 4;                 // u32, and bits(17..32)
            case 9: return size == 8;                 // u64, flags, and bits(33..64)
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

    private ref struct Compiler
    {
        public Span<TableFixedEntry> Plan;
        public int Capacity;
        public int Count;
        public int Pool;
        public bool Overflow;
        public TableReport Report;
        public byte ArgW; // the CURRENT union's tag width, stamped onto every guarded push
        public bool WantGuarded; // THE WALK RUNS TWICE, unguarded first (§5.9 #42)
        public bool TooLarge; // ONE ENTRY PAST THE WRITER'S RECORD, which refuses the plan WHOLE (§5.2)
        public uint Record;  // the WRITER's declared root size, the bound every entry's source extent is held to
        // THE GUARD CHAIN of the arm being compiled, OUTERMOST FIRST
        // (docs/FIXED-FORM-ALGORITHM.md §4.1): one link per enclosing union,
        // pushed on the way into an arm and popped on the way out. A chain
        // past the two lanes is laid into the pool once per arm, lazily.
        public TableFixedGuardLink[] Chain;
        public int ChainLen;
        public uint ChainAt;
        public bool ChainLaid;

        public void Push(in TableFixedEntry e)
        {
            // THE WALK RUNS TWICE and each pass keeps its own half, which is
            // how the plan comes out partitioned without a second array to
            // partition it in (§5.9 #42).
            if ((e.Guard != NoGuard) != WantGuarded) { return; }
            TableFixedEntry stamped = e;
            if (stamped.Guard != NoGuard)
            {
                stamped.ArgW = ArgW == 0 ? (byte)1 : ArgW;
            }
            // ONE ENTRY PAST THE WRITER'S RECORD REFUSES THE PLAN WHOLE
            // (docs/FIXED-FORM-ALGORITHM.md §5.2). The bound is the writer's
            // declared root size and not this reader's: an entry that reached
            // past it would read the NEXT record's bytes.
            if (Record != 0 && SrcEnd(stamped) > Record) { TooLarge = true; return; }
            // THE GUARD CHAIN (§4.1): an entry beneath a union answers to
            // EVERY enclosing tag. Its two lanes are the chain's innermost
            // two, and a chain past two rides the pool as offset+count.
            if (stamped.Guard != NoGuard && ChainLen > 0)
            {
                TableFixedGuardLink inner = Chain[ChainLen - 1];
                stamped.Guard = inner.Guard; stamped.Arg = inner.Arg; stamped.ArgW = inner.ArgW;
                if (ChainLen >= 2)
                {
                    TableFixedGuardLink outer = Chain[ChainLen - 2];
                    stamped.Guard2 = outer.Guard; stamped.Arg2 = outer.Arg; stamped.ArgW2 = outer.ArgW;
                }
                if (ChainLen > 2)
                {
                    if (!ChainLaid)
                    {
                        ChainAt = LayGuards();
                        ChainLaid = true;
                        if (Overflow) { return; }
                    }
                    stamped.GuardIndex = ChainAt;
                    stamped.GuardCount = (uint)ChainLen;
                }
            }
            int entrySize = System.Runtime.CompilerServices.Unsafe.SizeOf<TableFixedEntry>();
            int room = Capacity - (Pool + entrySize - 1) / entrySize;
            if (Count >= room) { Overflow = true; return; }
            Plan[Count++] = stamped;
        }

        // LayGuards lays the current chain into the pool behind the entries,
        // OUTERMOST FIRST, GuardLinkBytes a link, and answers its byte offset
        // (§4.1). Pool storage that meets the entries is plan_too_large.
        public uint LayGuards()
        {
            int bytes = ChainLen * GuardLinkBytes;
            int entrySize = System.Runtime.CompilerServices.Unsafe.SizeOf<TableFixedEntry>();
            int total = entrySize * Capacity;
            Pool += bytes;
            if (total - Pool < Count * entrySize) { Overflow = true; return 0; }
            uint at = (uint)(total - Pool);
            Span<byte> planBytes = MemoryMarshal.AsBytes(Plan);
            for (int j = 0; j < ChainLen; ++j)
            {
                Span<byte> l = planBytes.Slice((int)at + j * GuardLinkBytes, GuardLinkBytes);
                BinaryPrimitives.WriteUInt32LittleEndian(l, Chain[j].Guard);
                BinaryPrimitives.WriteUInt64LittleEndian(l.Slice(4), Chain[j].Arg);
                l[12] = Chain[j].ArgW;
            }
            return at;
        }

        public uint LayTable(ReadOnlySpan<ushort> values, int n)
        {
            // THE LENGTH WORD IS uint16, so a table that does not fit one
            // refuses the plan BY NAME rather than truncating its own length
            // (§5.8 row 14): the remap is one slot per writer variant, and a
            // count past 65535 is a plan that does not fit.
            if (n < 0 || n > 65535) { Overflow = true; return 0; }
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

    // 'census' is whether THIS entry is the FIRST time the writer's field is
    // seen. An array compiles its element ONCE PER SLOT, and the unknown count
    // is ONCE PER FIELD PER ENDPOINT (ALG §5.4), so only element 0 censuses.
    // Without it a writer's field dropped from a [4]Item is counted FOUR times and
    // the report says 4 where the law says 1 (docs/FIXED-FORM-VERSIONING-TESTS.md
    // §5.8 row 11). rust #1162 and cpp #1163 carry the same flag for the same
    // reason; this leg had no row 11 at all, so nothing here ever said so.
    private static void MatchChildren(
        ref Compiler c,
        TableFixedLayoutView theirs, int ti, uint their_at,
        TableFixedLayoutView mine, int mi, ReadOnlySpan<TableFixedDst> dst, uint my_at,
        uint guard, ulong arg, bool census)
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
                    CompileEntry(ref c, theirs, their_child, their_off, mine, my_child, dst, my_at, guard, arg, census);
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
            if (!named && census && c.Report != null) { c.Report.Unknown++; }
            tc_at += Subtree(theirs, tc_at);
        }
    }

    private static void CompileEntry(
        ref Compiler c,
        TableFixedLayoutView theirs, int ti, uint their_at,
        TableFixedLayoutView mine, int mi, ReadOnlySpan<TableFixedDst> dst, uint my_at,
        uint guard, ulong arg, bool census)
    {
        TableFixedLayoutEntry te = EntryAt(theirs, ti);
        TableFixedLayoutEntry me = EntryAt(mine, mi);
        TableFixedDst d = dst[mi];
        uint at = my_at + d.Dst;
        uint aux_at = my_at + d.Aux;
        if (te.Kind != me.Kind && me.Kind != 35)
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
        // T INTO ?T (§5.2 EMIT, bill §12.8): the reader wrapped a value the
        // writer carried bare. The PRESENT byte is a CONSTANT 1 at the
        // wrapper's AUX lane, constant in its VALUE and still carrying the
        // row's own guard and ordinal (§5.9 #13) — guarded under a union
        // arm, unguarded at top level, which is one rule and not two — and
        // the payload is compiled against the writer's own entry under that
        // same guard. It is not a kind that moved.
        if (me.Kind == 35 && te.Kind != 35)
        {
            c.Push(new TableFixedEntry(0, aux_at, 1, 1, guard, Const, arg, 0));
            CompileEntry(ref c, theirs, ti, their_at, mine, mi + 1, dst, my_at, guard, arg, census);
            return;
        }
        switch (me.Kind)
        {
            case 35: // Optional wrapper
                {
                    TableFixedEntry e = new TableFixedEntry(their_at, aux_at, 1, 0, guard, Copy, arg, 0);
                    c.Push(e);
                    CompileEntry(ref c, theirs, ti + 1, their_at + 1, mine, mi + 1, dst, my_at, guard, arg, census);
                    break;
                }
            case 13: // Nested table
                {
                    MatchChildren(ref c, theirs, ti, their_at, mine, mi, dst, at, guard, arg, census);
                    break;
                }
            case 14: // Array
                {
                    if (d.Arg == TextBytes)
                    {
                        uint units = Math.Min(me.Size - 4u, te.Size - 4u);
                        TableFixedEntry e = new TableFixedEntry(
                            their_at, aux_at, units, at, guard, Text, arg, 0, 0, d.Arg);
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
                        // THE COUNT'S BOUND IS THE WRITER'S their_n (§5.2 EMIT kind 14,
                        // §5.3 step 11: BOUNDS runs against the PLAN'S bounds). The
                        // reader's own my_n is the wrong number -- a forged 7 from a
                        // writer bounded at 4 must land 4 and COUNT, not be admitted
                        // because this reader grew to 8. their_n held to this reader's
                        // storage is their_n itself on every legal widening, which is
                        // what go spells 'n' and rust spells 'their_n.min(my_n)'.
                        TableFixedEntry e = new TableFixedEntry(their_at, aux_at, Math.Min(their_n, my_n), 0, guard, Count, arg, 0);
                        c.Push(e);
                    }
                    uint n = Math.Min(their_n, my_n);
                    if (d.Stride == 0)
                    {
                        if (tel.Kind == mel.Kind && tel.Size == mel.Size)
                        {
                            TableFixedEntry e = new TableFixedEntry(their_base, at, n * mel.Size, 0, guard, Flat, arg, 0);
                            c.Push(e);
                            break;
                        }
                        // THE FOLD IS NOT A FOLD ACROSS A WIDENING, and dropping the
                        // run would land the reader's defaults over values the writer
                        // sent (§5.2 EMIT kind 14). One entry, widened element by
                        // element, because the folded destination is one span setter
                        // and has no element-wise slot.
                        bool sameFamily = tel.Kind == mel.Kind || Widens(tel.Kind, mel.Kind);
                        if (sameFamily && tel.Size < mel.Size && mel.Size <= 8)
                        {
                            byte sign = (byte)(tel.Kind == 10 && mel.Kind == 11 ? 2 : (SignedKind(tel.Kind) && tel.Kind != mel.Kind ? 1 : 0));
                            c.Push(new TableFixedEntry(their_base, at, n * tel.Size, 0, guard, FlatWiden,
                                                       arg, (byte)mel.Size, sign, (byte)tel.Size));
                            break;
                        }
                        // A KIND THAT MOVED IS REPORTED, NEVER REINTERPRETED (§5.2).
                        if (c.Report != null) { c.Report.KindMismatch++; }
                        break;
                    }
                    for (uint i = 0; i < n; ++i)
                    {
                        CompileEntry(ref c, theirs, ti + 1, their_base + i * tel.Size,
                                     mine, mi + 1, dst, at + i * d.Stride, guard, arg, census && i == 0);
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
                                         mine, mel, dst, at + k * d.Stride, guard, arg, census && k == 0);
                            break;
                        }
                    }
                    break;
                }
            case 15: // Union
                {
                    uint their_tag = TagBytes(theirs, ti);
                    uint my_tag = TagBytes(mine, mi);
                    byte saved_argw = c.ArgW;
                    c.ArgW = (their_tag >= 1u && their_tag <= 8u) ? (byte)their_tag : (byte)1;
                    // FIRST, UNGUARDED: a const 0 of my_tag bytes into the
                    // reader's tag (§5.2 EMIT kind 15). It is None, and it
                    // STANDS WHEN NO ARM MATCHES: without it a tag naming no arm
                    // this reader holds leaves the PREVIOUS record's arm in the
                    // slot, because the arms' consts are guarded and every
                    // guarded entry's destination counts as landed, so the
                    // prefill does not cover the tag either. The row carries its
                    // OWN guard and ordinal — unguarded at top level, guarded
                    // under an outer arm — which is one rule and not two
                    // (§5.9 #13).
                    c.Push(new TableFixedEntry(their_at, aux_at, my_tag, 0, guard, Const, arg, 0));
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
                                // THE ARM ANSWERS TO THIS TAG AND EVERY TAG OUTSIDE
                                // IT (§4.1): one link pushed on the way in, the
                                // arm's own tag const included, popped on the way
                                // out. The layout's 64 bounds the depth.
                                uint savedAt = c.ChainAt;
                                bool savedLaid = c.ChainLaid;
                                if (c.Chain == null) { c.Chain = new TableFixedGuardLink[MaxDepth + 1]; }
                                if (c.ChainLen >= c.Chain.Length) { c.Overflow = true; return; }
                                c.Chain[c.ChainLen++] = new TableFixedGuardLink { Guard = their_at, Arg = (ulong)(j + 1), ArgW = c.ArgW == 0 ? (byte)1 : c.ArgW };
                                c.ChainLaid = false;
                                TableFixedEntry tag = new TableFixedEntry(
                                    their_at, aux_at, my_tag, k + 1, their_at, Const, (ulong)(j + 1), 0);
                                c.Push(tag);
                                CompileEntry(ref c, theirs, their_arm, their_at + their_tag,
                                             mine, my_arm, dst, at, their_at, (ulong)(j + 1), census);
                                c.ChainLen--;
                                c.ChainAt = savedAt;
                                c.ChainLaid = savedLaid;
                                break;
                            }
                            their_arm += Subtree(theirs, their_arm);
                        }
                        my_arm += Subtree(mine, my_arm);
                    }
                    c.ArgW = saved_argw;
                    break;
                }
            case 30: // Enum
                {
                    // THE HALF TEST PRECEDES THE ALLOCATION (docs/FIXED-FORM-ALGORITHM.md
                    // §5.9 #42): a kind that allocates before it pushes must
                    // test its half first, or the discarded pass spends the pool
                    // and a plan that fits comes back plan_too_large. The remap
                    // table is laid in the pool BEFORE the Push that would
                    // reject the other half, so the enum tests its half here and
                    // the discarded pass never calls LayTable.
                    if ((guard != NoGuard) != c.WantGuarded) { break; }
                    // A GROWN ORDINAL WIDTH IS A WIDEN, unsigned, and it counts
                    // widened (§5.2 EMIT kind 30, §5.4). Only a SAME-WIDTH
                    // ordinal takes the remap table.
                    if (te.Size < me.Size)
                    {
                        c.Push(new TableFixedEntry(their_at, at, te.Size, 0, guard, Widen, arg, (byte)me.Size));
                        break;
                    }
                    // FULL WIDTH (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.8 row 14):
                    // the remap is one slot per writer variant, never a
                    // 255-deep stack array. A count past the uint16 length word
                    // does not fit the length word: the table refuses the plan
                    // by name rather than answering a wrong ordinal.
                    uint n = te.Children;
                    if (n > 65535u) { c.Overflow = true; break; }
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
            case 12:
            case 33: // Text / WString
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

    // The split is metadata beside the plan, never consulted by the run
    // loop (FIXED-FORM-ALGORITHM §5.9 #42), so a caller that only runs the
    // plan compiles without it.
    public static int Compile(
        TableFixedLayoutView theirs,
        ReadOnlySpan<byte> my_layout,
        ReadOnlySpan<TableFixedDst> dst,
        Span<TableFixedEntry> plan,
        TableReport report)
    {
        return Compile(theirs, my_layout, dst, plan, report, out _);
    }

    public static int Compile(
        TableFixedLayoutView theirs,
        ReadOnlySpan<byte> my_layout,
        ReadOnlySpan<TableFixedDst> dst,
        Span<TableFixedEntry> plan,
        TableReport report,
        out int split)
    {
        split = 0;
        if (plan.IsEmpty) { return -1; }
        if (!ParseLayout(my_layout, out TableFixedLayoutView mine)) { return -1; }
        Compiler c = new Compiler
        {
            Plan = plan,
            Capacity = plan.Length,
            Report = report,
            ArgW = 1,
            WantGuarded = false,
            // THE WRITER'S RECORD IS THE BOUND ON EVERY ENTRY (§5.2): the
            // root's declared size, taken from the layout this plan is
            // compiled against.
            Record = EntryAt(theirs, 0).Size
        };
        // PASS 1: UNGUARDED entries only, counting the census. PASS 2:
        // guarded entries only, COUNTING NOTHING (§5.9 #42) — the unknown
        // census is the first pass's, and counting it twice would lie.
        MatchChildren(ref c, theirs, 0, 0, mine, 0, dst, 0, NoGuard, 0, true);
        int plain = c.Count;
        c.WantGuarded = true;
        c.Report = null;
        MatchChildren(ref c, theirs, 0, 0, mine, 0, dst, 0, NoGuard, 0, true);
        // ONE ENTRY PAST THE WRITER'S RECORD REFUSES THE PLAN WHOLE, by its
        // own name: -3 is layout_record_too_large, -1 the plan storage.
        if (c.TooLarge) { return -3; }
        if (c.Overflow) { return -1; }
        // COALESCE inside each half, never across the split (§5.9 #42). The
        // destination is a slot index, so a merge demands the SAME slot and
        // a contiguous source — the span-setter run that is already one
        // Flat or Text entry — and adjacent scalar copies never merge.
        int out_ = 0;
        int splitOut = 0;
        for (int i = 0; i < c.Count; ++i)
        {
            if (i == plain) { splitOut = out_; }
            if (out_ > splitOut && c.Plan[out_ - 1].Op == c.Plan[i].Op &&
                c.Plan[i].Op == Copy &&
                c.Plan[out_ - 1].Guard == c.Plan[i].Guard && c.Plan[out_ - 1].Arg == c.Plan[i].Arg &&
                c.Plan[out_ - 1].ArgW == c.Plan[i].ArgW && c.Plan[out_ - 1].Meta == c.Plan[i].Meta &&
                c.Plan[out_ - 1].Guard2 == c.Plan[i].Guard2 && c.Plan[out_ - 1].Arg2 == c.Plan[i].Arg2 &&
                c.Plan[out_ - 1].ArgW2 == c.Plan[i].ArgW2 &&
                c.Plan[out_ - 1].GuardIndex == c.Plan[i].GuardIndex && c.Plan[out_ - 1].GuardCount == c.Plan[i].GuardCount &&
                c.Plan[out_ - 1].Src + c.Plan[out_ - 1].Size == c.Plan[i].Src &&
                c.Plan[out_ - 1].Dst == c.Plan[i].Dst)
            {
                c.Plan[out_ - 1].Size += c.Plan[i].Size;
                continue;
            }
            c.Plan[out_++] = c.Plan[i];
        }
        if (plain == c.Count) { splitOut = out_; }
        split = splitOut;
        return out_;
    }

    // THE PREFILL IS THE SLOTS THE PLAN DOES NOT LAND. Cover is the
    // identity plan's destinations; Fills is that set minus everything
    // this plan writes. A GUARDED ENTRY COUNTS AS LANDING: it is a union
    // arm, and an arm's storage is the arm's. Identity's list is empty —
    // its destinations ARE the cover — and that is the rule's limit case,
    // not an exception: the record loop runs the list it was handed.
    // THE HEADER'S HASH SELECTS A KNOWN LAYOUT, and nothing else does
    // (§5.3 step 5). The FIRST index whose hash matches; -1 for a hash the
    // lineage does not hold, which is layout_newer.
    public static int Select(ReadOnlySpan<TableFixedKnownLayout> known, ulong hash)
    {
        for (int i = 0; i < known.Length; ++i)
        {
            if (known[i].Hash == hash) { return i; }
        }
        return -1;
    }

    // ONE PLAN PER LINEAGE ENTRY, compiled from THE LOCK'S layout bytes in
    // the static initializer of the generated type — C#'s own build-time
    // lane, and §5.9 #3's first conforming shape: the bytes came from the
    // lock, the walk happens once per type load, OFF EVERY LOAD PATH, and a
    // plan that will not build is recorded as a refusal NAME on that entry
    // rather than discovered at the first file that needs it. Nothing on
    // the load path compiles, nothing on the load path parses a layout, and
    // the load path cannot fail for want of a plan.
    //
    // The pool grows by construction (§5.9 #4): the buffer is retried at
    // four times the size until the plan fits or the declared cap is
    // reached, and the entry that exceeds it carries plan_too_large BY NAME.
    //
    // AND THIS FUNCTION MUST NOT THROW, WHICH IS THE COST OF RUNNING IN A
    // STATIC INITIALIZER AND IS PARTICULAR TO THIS LEG. A static readonly
    // field initializer runs in the type's static constructor, and an
    // exception out of a static constructor is not the caller's to catch: the
    // CLR wraps it in a TypeInitializationException and POISONS THE TYPE —
    // every later touch of ANY member of the generated table class rethrows
    // the same wrapped exception for the life of the process, including the
    // refusal paths, including Select, including a load of a file whose
    // layout is the reader's own. One bad entry in a lock would take the
    // whole table down, and it would take it down with a name no operator
    // can act on.
    //
    // So EVERY ENTRY IS A LANE WITH ITS OWN REFUSAL and there is no throw on
    // this walk to reach. A lane is constructed and stored BEFORE anything
    // can fail on it; a layout that does not parse sets Why to
    // layout_malformed and the loop continues; a plan that outgrows the cap
    // sets plan_too_large and the loop continues; no array is indexed out of
    // its own length and no input is trusted for a size. The refusal a bad
    // entry earns is read at LOAD time, by the one endpoint that selects it
    // (§5.9 #8) — which is also why a lock bug here costs exactly the
    // version it broke rather than the type.
    public static TableFixedLineagePlan[] LineagePlans(
        TableFixedKnownLayout[] known,
        byte[] my_layout,
        TableFixedDst[] dst,
        ulong own)
    {
        TableFixedLineagePlan[] out_ = new TableFixedLineagePlan[known.Length];
        for (int i = 0; i < known.Length; ++i)
        {
            TableFixedLineagePlan lane = new TableFixedLineagePlan();
            out_[i] = lane;
            if (known[i].Hash == own) { continue; }
            if (!ParseLayout(known[i].Layout, out TableFixedLayoutView parsed, out string why))
            {
                // A LINEAGE ENTRY THAT IS NOT A LAYOUT is a bug in the lock
                // and not a wire event (§5.9 #8): the reader still owes a
                // name rather than a crash.
                lane.Why = "layout_malformed";
                continue;
            }
            for (int room = 256; ; room *= 4)
            {
                TableFixedEntry[] buffer = new TableFixedEntry[room];
                TableReport census = new TableReport();
                int made = Compile(parsed, my_layout, dst, buffer, census, out int split);
                if (made == -3)
                {
                    // AN ENTRY PAST THE WRITER'S DECLARED RECORD is a bug in
                    // the lock and not a wire event (§5.8 row 6): the reader
                    // owes the name and not a crash.
                    lane.Why = "layout_record_too_large";
                    break;
                }
                if (made < 0)
                {
                    if (room >= (1 << 18)) { lane.Why = "plan_too_large"; break; }
                    continue;
                }
                lane.Entries = buffer;
                lane.Count = made;
                lane.Split = split;
                lane.Unknown = census.Unknown;
                lane.KindMismatch = census.KindMismatch;
                break;
            }
        }
        return out_;
    }

    public static int Fills(
        ReadOnlySpan<TableFixedEntry> plan,
        ReadOnlySpan<TableFixedFill> cover,
        Span<byte> landed,
        Span<TableFixedFill> dest)
    {
        landed.Clear();
        for (int i = 0; i < plan.Length; ++i)
        {
            ref readonly TableFixedEntry p = ref plan[i];
            if ((uint)p.Dst < (uint)landed.Length) { landed[(int)p.Dst] = 1; }
            if (p.Op == Text && (uint)p.Aux < (uint)landed.Length) { landed[(int)p.Aux] = 1; }
        }
        int n = 0;
        for (int r = 0; r < cover.Length; ++r)
        {
            uint pos = cover[r].Dst;
            uint hi = pos + cover[r].Size;
            while (pos < hi)
            {
                while (pos < hi && (uint)pos < (uint)landed.Length && landed[(int)pos] != 0) { pos++; }
                if (pos >= hi) { break; }
                uint start = pos;
                while (pos < hi && ((uint)pos >= (uint)landed.Length || landed[(int)pos] == 0)) { pos++; }
                if (n < dest.Length)
                {
                    dest[n] = new TableFixedFill(start, pos - start);
                }
                n++;
            }
        }
        return n;
    }

    public static void FillRun<T>(
        ReadOnlySpan<TableFixedFill> fill,
        ReadOnlySpan<TableFixedSlot<T>> slots,
        T dst)
    {
        for (int i = 0; i < fill.Length; ++i)
        {
            uint off = fill[i].Dst;
            uint n = fill[i].Size;
            for (uint s = 0; s < n; ++s)
            {
                uint idx = off + s;
                if (idx < (uint)slots.Length)
                {
                    slots[(int)idx].Reset?.Invoke(dst);
                }
            }
        }
    }

    // THE TAG IS COMPARED AT ITS OWN WIDTH. A two- or four-byte tag whose
    // low byte happens to be 1 is not arm 1: it is an ordinal this build
    // has no arm for, and reading only the first byte let 0x0101 run arm
    // 1's entries over a stranger's record. ArgW 0 means 1, >8 clamps to
    // 8. Little-endian, the same widths C#'s packet codec reads a tag at.
    // ONE LINK OF A GUARD CHAIN as a pool lays it (docs/FIXED-FORM-ALGORITHM.md
    // §4.1): guard u32, arg u64, argw u8, little-endian.
    public const int GuardLinkBytes = 13;

    // ChainHolds is whether EVERY link of an entry's guard chain holds: an arm
    // inside an arm inside an arm answers to every enclosing tag, outermost
    // first (§4.1). A chain the plan's bytes do not hold whole does not run.
    public static bool ChainHolds(ReadOnlySpan<byte> src, in TableFixedEntry p, ReadOnlySpan<byte> planBytes)
    {
        long end = (long)p.GuardIndex + (long)p.GuardCount * GuardLinkBytes;
        if (end > planBytes.Length) { return false; }
        for (int j = 0; j < (int)p.GuardCount; ++j)
        {
            ReadOnlySpan<byte> l = planBytes.Slice((int)p.GuardIndex + j * GuardLinkBytes, GuardLinkBytes);
            uint guard = BinaryPrimitives.ReadUInt32LittleEndian(l);
            ulong arg = BinaryPrimitives.ReadUInt64LittleEndian(l.Slice(4));
            if (TagAt(src, guard, l[12]) != arg) { return false; }
        }
        return true;
    }

    public static ulong TagAt(ReadOnlySpan<byte> src, uint guard, byte argw)
    {
        int w = argw == 0 ? 1 : (argw > 8 ? 8 : argw);
        int at = (int)guard;
        switch (w)
        {
            case 1: return src[at];
            case 2: return BinaryPrimitives.ReadUInt16LittleEndian(src.Slice(at));
            case 4: return BinaryPrimitives.ReadUInt32LittleEndian(src.Slice(at));
            case 8: return BinaryPrimitives.ReadUInt64LittleEndian(src.Slice(at));
            default:
                {
                    ulong v = 0;
                    for (int i = 0; i < w; ++i)
                    {
                        v |= ((ulong)src[at + i]) << (8 * i);
                    }
                    return v;
                }
        }
    }

    // THE GUARD'S WIDTH, clamped (§5.2): 0 reads as 1, and nothing above 8
    // is a width an ordinal is stored at.
    private static uint ArgWidth(byte w)
    {
        return w == 0 ? 1u : (w > 8 ? 8u : (uint)w);
    }

    // SrcEnd is the last source byte, EXCLUSIVE, an entry reads — the guard
    // included, because a guarded entry loads the tag before anything else.
    // It is the bound the plan is held to against the writer's declared
    // record size (docs/FIXED-FORM-ALGORITHM.md §5.2, the reference's
    // TableFixedSrcEnd).
    public static uint SrcEnd(in TableFixedEntry e)
    {
        uint end;
        if (e.Op == Const)
        {
            // a constant reads no source at all, EXCEPT the union's None
            // const, which reads the writer's raw tag to count a clamp
            // past DstSize arms — it has a source reach.
            end = (e.Aux == 0 && e.DstSize != 0) ? e.Src + e.Size : 0u;
        }
        else if (e.Op == Count)
        {
            end = e.Src + 4; // Size is the BOUND, not a byte count
        }
        else if (e.Op == Text)
        {
            end = e.Src + 4 + e.Size;
        }
        else
        {
            end = e.Src + e.Size;
        }
        if (e.Guard != NoGuard)
        {
            uint g = e.Guard + ArgWidth(e.ArgW);
            if (g > end) { end = g; }
        }
        // THE OUTER TAG IS READ BEFORE THE ENTRY TOO (§4.1), so its bytes are
        // part of the entry's source reach.
        if (e.ArgW2 != 0)
        {
            uint g = e.Guard2 + ArgWidth(e.ArgW2);
            if (g > end) { end = g; }
        }
        return end;
    }

    // THE WIDEN SCRATCH IS THE CALLER'S, AND IT IS HOISTED OUT OF THE RECORD
    // LOOP. Run is called ONCE PER RECORD, so a new byte[runs * dw] taken
    // inside the FlatWiden case allocated once per widened run per record —
    // a batch of ten thousand records paid ten thousand arrays for the same
    // run. The buffer is handed in ref and grown only when a run needs
    // more than it holds, so a whole batch pays one allocation per run
    // width. It is written in full before it is read: every byte of
    // runs * dw is stored by this case, so nothing from the previous
    // record survives into this one.
    public static string Run<T>(
        ReadOnlySpan<TableFixedEntry> plan,
        ReadOnlySpan<TableFixedSlot<T>> slots,
        ReadOnlySpan<byte> src,
        T dst,
        TableReport report,
        ReadOnlySpan<byte> planBytes,
        ref byte[] widenScratch,
        ref int forgedOrdinal)
    {
        for (int i = 0; i < plan.Length; ++i)
        {
            ref readonly TableFixedEntry p = ref plan[i];
            // an entry belonging to an ARM runs only under its own tag,
            // compared at ArgW bytes, never as a prefix
            if (p.Guard != NoGuard && TagAt(src, p.Guard, p.ArgW) != p.Arg) { continue; }
            // AN ARM INSIDE AN ARM ANSWERS TO THE OUTER TAG TOO (§4.1): the
            // entry runs only when the second lane's tag holds its ordinal.
            if (p.Guard2 != NoGuard && TagAt(src, p.Guard2, p.ArgW2) != p.Arg2) { continue; }
            // AND EVERY TAG FURTHER OUT: the guard chain past the two lanes (§4.1).
            if (p.GuardCount > 2 && !ChainHolds(src, p, planBytes)) { continue; }
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
                case FlatWiden:
                    {
                        int sw = p.Meta == 0 ? 1 : p.Meta;
                        int dw = p.DstSize == 0 ? 1 : p.DstSize;
                        int runs = (int)p.Size / sw;
                        int need = runs * dw;
                        if (widenScratch == null || widenScratch.Length < need)
                        {
                            widenScratch = new byte[need];
                        }
                        System.Span<byte> wide = widenScratch.AsSpan(0, need);
                        for (int e = 0; e < runs; ++e)
                        {
                            int at = (int)p.Src + e * sw;
                            if (p.Sign == 2)
                            {
                                double d2 = (double)BinaryPrimitives.ReadSingleLittleEndian(src.Slice(at));
                                BinaryPrimitives.WriteDoubleLittleEndian(wide.Slice(e * dw), d2);
                                continue;
                            }
                            ulong raw = 0;
                            if (sw == 1) raw = src[at];
                            else if (sw == 2) raw = BinaryPrimitives.ReadUInt16LittleEndian(src.Slice(at));
                            else if (sw == 4) raw = BinaryPrimitives.ReadUInt32LittleEndian(src.Slice(at));
                            else if (sw == 8) raw = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice(at));
                            if (p.Sign == 1)
                            {
                                int bits = sw * 8;
                                ulong top = 1UL << (bits - 1);
                                if ((raw & top) != 0) { raw |= ~((top << 1) - 1UL); }
                            }
                            System.Span<byte> cell = wide.Slice(e * dw, dw);
                            if (dw == 1) cell[0] = (byte)raw;
                            else if (dw == 2) BinaryPrimitives.WriteUInt16LittleEndian(cell, (ushort)raw);
                            else if (dw == 4) BinaryPrimitives.WriteUInt32LittleEndian(cell, (uint)raw);
                            else if (dw == 8) BinaryPrimitives.WriteUInt64LittleEndian(cell, raw);
                        }
                        slots[(int)p.Dst].SetBytes?.Invoke(dst, wide, need);
                        // WIDENED COUNTS ONE PER ELEMENT WIDENED, NOT ONE PER ENTRY.
                        // The C++ reference's EMIT has NO FOLD ACROSS A WIDEN: it
                        // emits one entry per element of a widened array, so its
                        // widened for array_elem_widen is n — 4 on that row's
                        // corpus. §5.4's "once per entry" is therefore once per
                        // ELEMENT there, and the ruling is that a fold across a
                        // widen is not allowed to change the number. This leg DOES
                        // fold the run into one entry, for the one copy loop, so it
                        // adds the run's element count and reports the reference's
                        // figure. A scalar widen is runs == 1 and still counts one.
                        if (report != null) { report.Widened += runs; }
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
                        ReadOnlySpan<byte> textBytes = src.Slice((int)p.Src + 4, (int)p.Size);
                        // THE CONTENT RULES APPLY TO THE USED UNITS AND NOTHING
                        // ELSE, and a violation REFUSES BY NAME, the verdict the
                        // packet reader gives (docs/FIXED-FORM-ALGORITHM.md §4.5,
                        // §5.4 fix 11, §5.8 row 8): the length above CLAMPS and
                        // counts; the bytes it kept are checked now, over v units
                        // and never over the span.
                        if (TextIllFormed(p.Meta, (uint)v, textBytes))
                        {
                            return "text_ill_formed";
                        }
                        slots[(int)p.Dst].SetRaw?.Invoke(dst, (ulong)(uint)v);
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
                        // A 64-BIT TEMPORARY AND AN EIGHT-BYTE CASE (§5.8 row 7,
                        // §4.5): an ordinal width of 8 is admissible, and read
                        // through a 32-bit temporary with no case for it the raw
                        // value stayed 0 — a silent None where the writer named a
                        // variant.
                        ulong raw = 0;
                        if (p.Size == 1) raw = src[(int)p.Src];
                        else if (p.Size == 2) raw = BinaryPrimitives.ReadUInt16LittleEndian(src.Slice((int)p.Src));
                        else if (p.Size == 4) raw = BinaryPrimitives.ReadUInt32LittleEndian(src.Slice((int)p.Src));
                        else if (p.Size == 8) raw = BinaryPrimitives.ReadUInt64LittleEndian(src.Slice((int)p.Src));
                        uint v = 0;
                        if (!planBytes.IsEmpty && p.Aux < (uint)planBytes.Length)
                        {
                            ReadOnlySpan<byte> tableBytes = planBytes.Slice((int)p.Aux);
                            ushort count = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes);
                            if (raw != 0 && raw <= count)
                            {
                                v = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes.Slice(2 * (int)raw));
                            }
                            // A FORGED ORDINAL — one past the WRITER's own variant
                            // count — lands 0 (None) and moves NOTHING (§5.4: the
                            // ordinal op lands its value and moves nothing). The
                            // remap to None is the BOUNDS PASS's to count, on BOTH
                            // plans, so the op records the forge in the caller's
                            // forgedOrdinal lane and the bounds step, not this op,
                            // turns it into Clamped (§5.4, §5.8 row 12).
                            if (raw > count) { forgedOrdinal++; }
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
                        // THE FLOAT RUNG IS THE ORDINARY CONVERSION (docs/SPEC-TABLES.md
                        // §3.3, ALG §4.2): exact for every finite float32; a NaN or an
                        // infinity stays one, and the finite pass after the run refuses it.
                        float f = BinaryPrimitives.ReadSingleLittleEndian(src.Slice((int)p.Src));
                        slots[(int)p.Dst].SetDouble?.Invoke(dst, (double)f);
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
        return null;
    }

    // TextIllFormed reports whether a text payload's USED units are the text
    // its kind says (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.4 fix 11): UTF-8
    // with no zero byte for string, paired non-zero UTF-16 code units for
    // wstring. bytes(N) carries no text rule at all, and slack past the used
    // units is unspecified and never a refusal (docs/SPEC-TABLES.md §3.4).
    // The used argument is the clamped length in the flavour's own units.
    public static bool TextIllFormed(byte meta, uint used, ReadOnlySpan<byte> p)
    {
        switch (meta)
        {
            case TextUtf8:
                {
                    uint i = 0;
                    while (i < used)
                    {
                        byte lead = p[(int)i];
                        uint continuations;
                        uint code_point;
                        if (lead == 0) { return true; }
                        if (lead < 0x80) { i++; continue; }
                        else if ((lead & 0xE0) == 0xC0) { continuations = 1; code_point = (uint)(lead & 0x1F); }
                        else if ((lead & 0xF0) == 0xE0) { continuations = 2; code_point = (uint)(lead & 0x0F); }
                        else if ((lead & 0xF8) == 0xF0) { continuations = 3; code_point = (uint)(lead & 0x07); }
                        else { return true; }
                        if (i + continuations >= used) { return true; }
                        for (uint k = 1; k <= continuations; k++)
                        {
                            if ((p[(int)(i + k)] & 0xC0) != 0x80) { return true; }
                            code_point = (code_point << 6) | (uint)(p[(int)(i + k)] & 0x3F);
                        }
                        if (continuations == 1 && code_point < 0x80) { return true; }
                        if (continuations == 2 && (code_point < 0x800 || (code_point >= 0xD800 && code_point <= 0xDFFF))) { return true; }
                        if (continuations == 3 && (code_point < 0x10000 || code_point > 0x10FFFF)) { return true; }
                        i += 1 + continuations;
                    }
                    break;
                }
            case TextWide:
                {
                    for (uint i = 0; i < used; i++)
                    {
                        uint u = (uint)(p[(int)i * 2] | (p[(int)i * 2 + 1] << 8));
                        if (u == 0) { return true; }
                        if (u >= 0xD800 && u <= 0xDBFF)
                        {
                            if (i + 1 >= used) { return true; }
                            uint lo = (uint)(p[(int)(i + 1) * 2] | (p[(int)(i + 1) * 2 + 1] << 8));
                            if (lo < 0xDC00 || lo > 0xDFFF) { return true; }
                            i++;
                        }
                        else if (u >= 0xDC00 && u <= 0xDFFF) { return true; }
                    }
                    break;
                }
        }
        return false;
    }
}
`
