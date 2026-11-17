package unitytable

import "github.com/mas-bandwidth/schema/v2/ir"

// fixedRuntimeNeeded reports whether any table of the unit has the fixed form
// on this leg, which is when the runtime carries the form's members.
func fixedRuntimeNeeded(u *ir.Unit) bool {
	for _, st := range u.Tables {
		if FixedCarried(u, st) {
			return true
		}
	}
	return false
}

// fixedRuntime is the fixed form's part of TableWire (docs/FIXED-FORM-
// ALGORITHM.md §2, §4, §5.3): the envelope and the plan selected by hash, the
// check pass over every record, and the plan run into a record image. Every
// table's static data — its lanes, plans and checks — is laid down by the
// generator; nothing here compiles a plan or hashes a layout.
const fixedRuntime = `
    // ---- the fixed form, form 3 (docs/SPEC-TABLES.md §3.4) ----

    internal const byte FixedForm = 3;
    // FixedHead is the header: the form byte, seven reserved zero bytes, the
    // layout hash and the layout's byte length; the layout follows it.
    internal const int FixedHead = 20;

    // The plan and check entry codes (docs/FIXED-FORM-ALGORITHM.md §4.1, §4.6).
    internal const int FixedCopy = 1, FixedWidenS = 2, FixedWidenU = 3, FixedBool = 4, FixedRangeS = 5, FixedRangeU = 6;

    // FixedOp is one plan entry (Src in the writer's body, Dst in the reader's
    // image, Size source bytes, Width destination bytes) or one check-pass
    // entry (Src, Size, and the writer's bounds Lo and Hi, two's complement
    // for a signed range).
    internal readonly struct FixedOp
    {
        public readonly int Code;
        public readonly int Src;
        public readonly int Dst;
        public readonly int Size;
        public readonly int Width;
        public readonly ulong Lo;
        public readonly ulong Hi;

        public FixedOp(int code, int src, int dst, int size, int width, ulong lo, ulong hi)
        {
            Code = code;
            Src = src;
            Dst = dst;
            Size = size;
            Width = width;
            Lo = lo;
            Hi = hi;
        }
    }

    // FixedLane is one lineage entry as the build laid it down: the hash a
    // file is matched on, the layout bytes it is held to, the whole record
    // size, the refusal an entry that is not a lawful widening carries (or 0),
    // whether it is this build's own entry, whether its plan prefills the
    // reader's defaults, the widenings one record lands, the plan's entry
    // count, the plan, and the check pass.
    internal sealed class FixedLane
    {
        public readonly ulong Hash;
        public readonly int RecordBytes;
        public readonly int Why;
        public readonly bool Identity;
        public readonly bool Prefill;
        public readonly int Widens;
        public readonly int PlanCount;
        public readonly byte[] Layout;
        public readonly FixedOp[] Ops;
        public readonly FixedOp[] Checks;

        public FixedLane(ulong hash, int recordBytes, int why, bool identity, bool prefill, int widens, int planCount, byte[] layout, FixedOp[] ops, FixedOp[] checks)
        {
            Hash = hash;
            RecordBytes = recordBytes;
            Why = why;
            Identity = identity;
            Prefill = prefill;
            Widens = widens;
            PlanCount = planCount;
            Layout = layout;
            Ops = ops;
            Checks = checks;
        }
    }

    // FixedGet is the unsigned little-endian value of the width bytes at at.
    internal static ulong FixedGet(ReadOnlySpan<byte> b, int at, int width)
    {
        ulong v = 0;
        for (int i = width - 1; i >= 0; i--)
        {
            v = (v << 8) | b[at + i];
        }
        return v;
    }

    // FixedPut stores v's low width bytes little-endian at at.
    internal static void FixedPut(Span<byte> b, int at, int width, ulong v)
    {
        for (int i = 0; i < width; i++)
        {
            b[at + i] = unchecked((byte)(v >> (8 * i)));
        }
    }

    // FixedSigned sign-extends the width-byte two's complement raw.
    internal static long FixedSigned(ulong raw, int width)
    {
        int shift = 64 - 8 * width;
        return unchecked((long)(raw << shift)) >> shift;
    }

    // FixedOpen is the envelope and the check pass (docs/FIXED-FORM-ALGORITHM.md
    // §5.3, steps 1 to 11, in that order): it answers Accepted with the
    // selected lane and the record count, or the refusal, having written
    // nothing. hash is the header's, for the two refusals that report it.
    internal static int FixedOpen(ReadOnlySpan<byte> data, FixedLane[] lanes, int floor, int capacity, int planCapacity, out int pick, out int count, out ulong hash)
    {
        pick = -1;
        count = 0;
        hash = 0;
        if (data.Length < 1) { return TableReport.Truncated; }
        if (data[0] != FixedForm)
        {
            return data[0] == 1 ? TableReport.PreviousForm : data[0] == 2 ? TableReport.MessageFormAsFile : TableReport.NewerForm;
        }
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
        // THE CHECK PASS: every record, before any record lands (step 11).
        ReadOnlySpan<byte> at = data.Slice(FixedHead + (int)layoutBytes);
        for (int k = 0; k < n; k++)
        {
            ReadOnlySpan<byte> one = at.Slice(k * record, record);
            if (FixedGet(one, 0, 8) != hash) { return TableReport.NoLayout; }
            int why = FixedCheck(lane.Checks, one.Slice(8));
            if (why != TableReport.Accepted) { return why; }
        }
        pick = selected;
        count = (int)n;
        return TableReport.Accepted;
    }

    // FixedCheck holds one record's live values: a bool byte is 0 or 1, and a
    // ranged value lies within the writer's own bounds (§4.5, §4.6).
    internal static int FixedCheck(FixedOp[] checks, ReadOnlySpan<byte> body)
    {
        for (int i = 0; i < checks.Length; i++)
        {
            FixedOp c = checks[i];
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
            }
        }
        return TableReport.Accepted;
    }

    // FixedApply runs a lineage plan from a writer's body into the reader's
    // image. It refuses nothing: the check pass already has.
    internal static void FixedApply(FixedOp[] ops, ReadOnlySpan<byte> body, Span<byte> image)
    {
        for (int i = 0; i < ops.Length; i++)
        {
            FixedOp o = ops[i];
            switch (o.Code)
            {
                case FixedCopy:
                    body.Slice(o.Src, o.Size).CopyTo(image.Slice(o.Dst));
                    break;
                case FixedWidenS:
                    FixedPut(image, o.Dst, o.Width, unchecked((ulong)FixedSigned(FixedGet(body, o.Src, o.Size), o.Size)));
                    break;
                case FixedWidenU:
                    FixedPut(image, o.Dst, o.Width, FixedGet(body, o.Src, o.Size));
                    break;
            }
        }
    }
`
