package unitytable

import "github.com/mas-bandwidth/schema/v3/ir"

func fixedRuntimeNeeded(u *ir.Unit) bool {
	for _, st := range u.Tables {
		if FixedCarried(u, st) {
			return true
		}
	}
	return false
}

const fixedRuntime = `
    // ---- the fixed form, form 3 (docs/SPEC-TABLES.md §3.4) ----

    internal const byte FixedForm = 3;
    internal const int FixedHead = 20;

    // Plan and check operation codes.
    internal const int FixedCopy = 1, FixedWidenS = 2, FixedWidenU = 3, FixedBool = 4, FixedRangeS = 5, FixedRangeU = 6,
        FixedWidenF = 7, FixedConst = 8, FixedCountOp = 9, FixedTextOp = 10, FixedOrdinal = 11,
        FixedDead = 12, FixedCountCheck = 13, FixedBytesCheck = 14, FixedStringCheck = 15,
        FixedF32Finite = 16, FixedF32Range = 17, FixedF64Finite = 18, FixedF64Range = 19,
        FixedRangeS128 = 20, FixedRangeU128 = 21, FixedLoopFixed = 22, FixedLoopCounted = 23;

    internal const int FixedGuardEq = 1, FixedGuardGt = 2;

    internal readonly struct FixedGuard
    {
        public readonly int At;
        public readonly int Width;
        public readonly ulong Value;
        public readonly int Mode;

        public FixedGuard(int at, int width, ulong value, int mode)
        {
            At = at;
            Width = width;
            Value = value;
            Mode = mode;
        }
    }

    internal sealed class FixedFill
    {
        public readonly int Dst;
        public readonly int GuardOff;
        public readonly int GuardLen;
        public readonly byte[] Image;

        public FixedFill(int dst, int guardOff, int guardLen, byte[] image)
        {
            Dst = dst;
            GuardOff = guardOff;
            GuardLen = guardLen;
            Image = image;
        }
    }

    internal readonly struct FixedOp
    {
        public readonly int Code;
        public readonly int Src;
        public readonly int Dst;
        public readonly int Size;
        public readonly int Width;
        public readonly ulong Lo;
        public readonly ulong Hi;
        public readonly int GuardOff;
        public readonly int GuardLen;
        public readonly ulong Lo2;
        public readonly ulong Hi2;

        public FixedOp(int code, int src, int dst, int size, int width, ulong lo, ulong hi)
        {
            Code = code;
            Src = src;
            Dst = dst;
            Size = size;
            Width = width;
            Lo = lo;
            Hi = hi;
            GuardOff = 0;
            GuardLen = 0;
            Lo2 = 0;
            Hi2 = 0;
        }

        public FixedOp(int code, int src, int dst, int size, int width, ulong lo, ulong hi, int guardOff, int guardLen, ulong lo2 = 0, ulong hi2 = 0)
        {
            Code = code;
            Src = src;
            Dst = dst;
            Size = size;
            Width = width;
            Lo = lo;
            Hi = hi;
            GuardOff = guardOff;
            GuardLen = guardLen;
            Lo2 = lo2;
            Hi2 = hi2;
        }
    }

    internal sealed class FixedLane
    {
        public readonly ulong Hash;
        public readonly int RecordBytes;
        public readonly int Why;
        public readonly bool Identity;
        public readonly bool Prefill;
        public readonly int Widens;
        public readonly int Unknown;
        public readonly int PlanCount;
        public readonly bool HasGuards;
        public readonly byte[] Layout;
        public readonly FixedOp[] Ops;
        public readonly FixedOp[] Checks;
        public readonly FixedGuard[] Guards;
        public readonly FixedGuard[] CheckGuards;
        public readonly FixedFill[] Fills;
        public readonly ushort[] Remap;

        public FixedLane(ulong hash, int recordBytes, int why, bool identity, bool prefill, int widens, int planCount, byte[] layout, FixedOp[] ops, FixedOp[] checks)
            : this(hash, recordBytes, why, identity, prefill, widens, 0, planCount, false, layout, ops, checks, Array.Empty<FixedGuard>(), Array.Empty<FixedGuard>(), Array.Empty<FixedFill>(), Array.Empty<ushort>())
        {
        }

        public FixedLane(ulong hash, int recordBytes, int why, bool identity, bool prefill, int widens, int unknown, int planCount, bool hasGuards, byte[] layout, FixedOp[] ops, FixedOp[] checks, FixedGuard[] guards, FixedGuard[] checkGuards, FixedFill[] fills, ushort[] remap)
        {
            Hash = hash;
            RecordBytes = recordBytes;
            Why = why;
            Identity = identity;
            Prefill = prefill;
            Widens = widens;
            Unknown = unknown;
            PlanCount = planCount;
            HasGuards = hasGuards;
            Layout = layout;
            Ops = ops;
            Checks = checks;
            Guards = guards;
            CheckGuards = checkGuards;
            Fills = fills;
            Remap = remap;
        }
    }

    internal static ulong FixedGet(ReadOnlySpan<byte> b, int at, int width)
    {
        ulong v = 0;
        for (int i = width - 1; i >= 0; i--)
        {
            v = (v << 8) | b[at + i];
        }
        return v;
    }

    internal static void FixedPut(Span<byte> b, int at, int width, ulong v)
    {
        for (int i = 0; i < width; i++)
        {
            b[at + i] = unchecked((byte)(v >> (8 * i)));
        }
    }

    internal static long FixedSigned(ulong raw, int width)
    {
        int shift = 64 - 8 * width;
        return unchecked((long)(raw << shift)) >> shift;
    }

    internal static float FixedGetF32(ReadOnlySpan<byte> b, int at)
    {
        return BitConverter.Int32BitsToSingle(BinaryPrimitives.ReadInt32LittleEndian(b.Slice(at, 4)));
    }

    internal static double FixedGetF64(ReadOnlySpan<byte> b, int at)
    {
        return BitConverter.Int64BitsToDouble(BinaryPrimitives.ReadInt64LittleEndian(b.Slice(at, 8)));
    }

    internal static void FixedPutF32(Span<byte> b, int at, float v)
    {
        BinaryPrimitives.WriteInt32LittleEndian(b.Slice(at, 4), BitConverter.SingleToInt32Bits(v));
    }

    internal static void FixedPutF64(Span<byte> b, int at, double v)
    {
        BinaryPrimitives.WriteInt64LittleEndian(b.Slice(at, 8), BitConverter.DoubleToInt64Bits(v));
    }

    internal static bool FixedDeadZero(ReadOnlySpan<byte> b, int from, int to)
    {
        for (int i = from; i < to; i++)
        {
            if (b[i] != 0) { return false; }
        }
        return true;
    }

    internal static bool FixedGuardsHold(FixedGuard[] guards, int off, int len, ReadOnlySpan<byte> body)
    {
        for (int i = 0; i < len; i++)
        {
            FixedGuard g = guards[off + i];
            ulong actual = FixedGet(body, g.At, g.Width);
            if (g.Mode == FixedGuardEq && actual != g.Value) { return false; }
            if (g.Mode == FixedGuardGt && actual <= g.Value) { return false; }
        }
        return true;
    }

    internal static int FixedOpen(ReadOnlySpan<byte> data, FixedLane[] lanes, int floor, int capacity, int planCapacity, out int pick, out int count, out ulong hash, bool integrity = false)
    {
        pick = -1;
        count = 0;
        hash = 0;
        if (data.Length < 1) { return TableReport.Truncated; }
        if (data[0] != FixedForm)
        {
            return data[0] == 1 ? TableReport.PreviousForm : data[0] == 2 ? TableReport.MessageFormAsFile : TableReport.NewerForm;
        }
        if (integrity && !IntegrityRead(ref data)) { return TableReport.IntegrityMismatch; }
        if (data.Length < FixedHead) { return TableReport.Truncated; }
        for (int i = 1; i < 8; i++)
        {
            if (data[i] != 0) { return TableReport.ReservedNotZero; }
        }
        ulong layoutBytes = FixedGet(data, 16, 4);
        if ((ulong)FixedHead + layoutBytes > (ulong)data.Length) { return TableReport.LayoutMalformed; }
        hash = FixedGet(data, 8, 8);
        int selected = -1;
        for (int i = 0; i < lanes.Length; i++)
        {
            if (lanes[i].Hash == hash) { selected = i; break; }
        }
        if (selected < 0) { return TableReport.LayoutUnknown; }
        if (selected < floor) { return TableReport.LayoutUnsupported; }
        FixedLane lane = lanes[selected];
        if (!data.Slice(FixedHead, (int)layoutBytes).SequenceEqual(new ReadOnlySpan<byte>(lane.Layout))) { return TableReport.LayoutMalformed; }
        if (lane.Why != 0) { return lane.Why; }
        if (lane.PlanCount > planCapacity) { return TableReport.PlanTooLarge; }
        long rest = data.Length - FixedHead - (long)layoutBytes;
        int record = lane.RecordBytes;
        if (rest == 0 || record <= 8 || rest % record != 0) { return TableReport.Truncated; }
        long n = rest / record;
        if (n > capacity) { return TableReport.BatchTooLarge; }
        ReadOnlySpan<byte> at = data.Slice(FixedHead + (int)layoutBytes);
        for (int k = 0; k < n; k++)
        {
            ReadOnlySpan<byte> one = at.Slice(k * record, record);
            if (FixedGet(one, 0, 8) != hash) { return TableReport.NoLayout; }
            int why = FixedCheck(lane.Checks, lane.CheckGuards, 0, lane.Checks.Length, one.Slice(8));
            if (why != TableReport.Accepted) { return why; }
        }
        pick = selected;
        count = (int)n;
        return TableReport.Accepted;
    }

    internal static int FixedCheck(FixedOp[] checks, FixedGuard[] guards, int from, int to, ReadOnlySpan<byte> body)
    {
        for (int i = from; i < to; i++)
        {
            FixedOp c = checks[i];
            if (c.GuardLen > 0 && !FixedGuardsHold(guards, c.GuardOff, c.GuardLen, body))
            {
                if (c.Code == FixedLoopFixed || c.Code == FixedLoopCounted) { i += c.Dst; }
                continue;
            }
            switch (c.Code)
            {
                case FixedBool:
                    if (body[c.Src] > 1) { return TableReport.WireDamaged; }
                    break;
                case FixedRangeS:
                    {
                        long v = FixedSigned(FixedGet(body, c.Src, c.Size), c.Size);
                        if (v < unchecked((long)c.Lo) || v > unchecked((long)c.Hi)) { return TableReport.OutOfRange; }
                        break;
                    }
                case FixedRangeU:
                    {
                        ulong v = FixedGet(body, c.Src, c.Size);
                        if (v < c.Lo || v > c.Hi) { return TableReport.OutOfRange; }
                        break;
                    }
                case FixedRangeS128:
                    {
                        ulong lo = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(c.Src, 8));
                        ulong hi = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(c.Src + 8, 8));
                        Int128Value v = new Int128Value(hi, lo);
                        Int128Value minV = new Int128Value(c.Lo2, c.Lo);
                        Int128Value maxV = new Int128Value(c.Hi2, c.Hi);
                        if (v < minV || v > maxV) { return TableReport.OutOfRange; }
                        break;
                    }
                case FixedRangeU128:
                    {
                        ulong lo = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(c.Src, 8));
                        ulong hi = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(c.Src + 8, 8));
                        UInt128Value v = new UInt128Value(hi, lo);
                        UInt128Value minV = new UInt128Value(c.Lo2, c.Lo);
                        UInt128Value maxV = new UInt128Value(c.Hi2, c.Hi);
                        if (v < minV || v > maxV) { return TableReport.OutOfRange; }
                        break;
                    }
                case FixedF32Finite:
                    if ((BinaryPrimitives.ReadUInt32LittleEndian(body.Slice(c.Src, 4)) & 0x7F800000u) == 0x7F800000u) { return TableReport.NonFiniteFloat; }
                    break;
                case FixedF32Range:
                    {
                        uint bits = BinaryPrimitives.ReadUInt32LittleEndian(body.Slice(c.Src, 4));
                        if ((bits & 0x7F800000u) == 0x7F800000u) { return TableReport.NonFiniteFloat; }
                        float v = BitConverter.Int32BitsToSingle(unchecked((int)bits));
                        float minV = BitConverter.Int32BitsToSingle(unchecked((int)(uint)c.Lo));
                        float maxV = BitConverter.Int32BitsToSingle(unchecked((int)(uint)c.Hi));
                        if (v < minV || v > maxV) { return TableReport.OutOfRange; }
                        break;
                    }
                case FixedF64Finite:
                    if ((BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(c.Src, 8)) & 0x7FF0000000000000UL) == 0x7FF0000000000000UL) { return TableReport.NonFiniteFloat; }
                    break;
                case FixedF64Range:
                    {
                        ulong bits = BinaryPrimitives.ReadUInt64LittleEndian(body.Slice(c.Src, 8));
                        if ((bits & 0x7FF0000000000000UL) == 0x7FF0000000000000UL) { return TableReport.NonFiniteFloat; }
                        double v = BitConverter.Int64BitsToDouble(unchecked((long)bits));
                        double minV = BitConverter.Int64BitsToDouble(unchecked((long)c.Lo));
                        double maxV = BitConverter.Int64BitsToDouble(unchecked((long)c.Hi));
                        if (v < minV || v > maxV) { return TableReport.OutOfRange; }
                        break;
                    }
                case FixedDead:
                    if (!FixedDeadZero(body, c.Src, c.Src + c.Size)) { return TableReport.WireDamaged; }
                    break;
                case FixedCountCheck:
                    {
                        long cnt = FixedSigned(FixedGet(body, c.Src, 4), 4);
                        if (cnt < (long)c.Lo || cnt > (long)c.Hi) { return TableReport.CountOverBound; }
                        int liveEnd = c.Src + 4 + (int)cnt * c.Width;
                        if (!FixedDeadZero(body, liveEnd, c.Dst)) { return TableReport.WireDamaged; }
                        break;
                    }
                case FixedBytesCheck:
                case FixedStringCheck:
                    {
                        long len = FixedSigned(FixedGet(body, c.Src, 4), 4);
                        if (len < 0 || len > (long)c.Hi) { return TableReport.LengthOverBound; }
                        int liveEnd = c.Src + 4 + (int)len;
                        if (!FixedDeadZero(body, liveEnd, c.Src + 4 + (int)c.Hi)) { return TableReport.WireDamaged; }
                        if (c.Code == FixedStringCheck && !TextValid(body.Slice(c.Src + 4, (int)len))) { return TableReport.TextIllFormed; }
                        break;
                    }
                case FixedLoopFixed:
                    {
                        for (int iter = 0; iter < c.Size; iter++)
                        {
                            int why = FixedCheck(checks, guards, i + 1, i + 1 + c.Dst, body.Slice(c.Src + iter * c.Width, c.Width));
                            if (why != TableReport.Accepted) { return why; }
                        }
                        i += c.Dst;
                        break;
                    }
                case FixedLoopCounted:
                    {
                        int cnt = (int)FixedGet(body, c.Src, 4);
                        for (int iter = 0; iter < cnt; iter++)
                        {
                            int why = FixedCheck(checks, guards, i + 1, i + 1 + c.Dst, body.Slice(c.Src + 4 + iter * c.Width, c.Width));
                            if (why != TableReport.Accepted) { return why; }
                        }
                        i += c.Dst;
                        break;
                    }
            }
        }
        return TableReport.Accepted;
    }

    internal static int FixedApply(FixedLane lane, ReadOnlySpan<byte> body, Span<byte> image)
    {
        FixedFill[] fills = lane.Fills;
        FixedGuard[] guards = lane.Guards;
        for (int i = 0; i < fills.Length; i++)
        {
            FixedFill fill = fills[i];
            if (fill.GuardLen == 0 || FixedGuardsHold(guards, fill.GuardOff, fill.GuardLen, body))
            {
                fill.Image.CopyTo(image.Slice(fill.Dst));
            }
        }
        int widened = 0;
        FixedOp[] ops = lane.Ops;
        for (int i = 0; i < ops.Length; i++)
        {
            FixedOp o = ops[i];
            if (o.GuardLen > 0 && !FixedGuardsHold(guards, o.GuardOff, o.GuardLen, body)) { continue; }
            switch (o.Code)
            {
                case FixedCopy:
                    body.Slice(o.Src, o.Size).CopyTo(image.Slice(o.Dst));
                    break;
                case FixedWidenS:
                    if (o.Width == 16)
                    {
                        long sx = FixedSigned(FixedGet(body, o.Src, o.Size), o.Size);
                        FixedPut(image, o.Dst, 8, unchecked((ulong)sx));
                        FixedPut(image, o.Dst + 8, 8, sx < 0 ? ulong.MaxValue : 0UL);
                    }
                    else
                    {
                        FixedPut(image, o.Dst, o.Width, unchecked((ulong)FixedSigned(FixedGet(body, o.Src, o.Size), o.Size)));
                    }
                    widened++;
                    break;
                case FixedWidenU:
                    if (o.Width == 16)
                    {
                        image.Slice(o.Dst, 16).Clear();
                        FixedPut(image, o.Dst, o.Size, FixedGet(body, o.Src, o.Size));
                    }
                    else
                    {
                        FixedPut(image, o.Dst, o.Width, FixedGet(body, o.Src, o.Size));
                    }
                    widened++;
                    break;
                case FixedWidenF:
                    FixedPutF64(image, o.Dst, (double)FixedGetF32(body, o.Src));
                    widened++;
                    break;
                case FixedConst:
                    FixedPut(image, o.Dst, o.Size, o.Lo);
                    break;
                case FixedCountOp:
                    body.Slice(o.Src, 4).CopyTo(image.Slice(o.Dst));
                    break;
                case FixedTextOp:
                    body.Slice(o.Src, 4 + o.Size).CopyTo(image.Slice(o.Dst));
                    break;
                case FixedOrdinal:
                    {
                        ulong raw = FixedGet(body, o.Src, o.Size);
                        if (o.Hi > 0)
                        {
                            raw = (raw >= 1UL && raw <= o.Hi) ? (ulong)lane.Remap[(int)o.Lo + (int)raw - 1] : 0UL;
                        }
                        FixedPut(image, o.Dst, o.Width, raw);
                        if (o.Size < o.Width) { widened++; }
                        break;
                    }
            }
        }
        return widened;
    }
`
