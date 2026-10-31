package cstable

const tableMessageReadSource = `
    struct MessageRead
    {
        public TableVocabulary Vocabulary;
        public TableReport Report;
    }
    static bool MessageReference(ref BitReader r, MessageRead d, out ulong reference, out TableMessageEntry entry)
    {
        entry = null; reference = 0;
        if (!r.Get(d.Vocabulary.RefBits, out UInt128 raw) || raw > (uint)d.Vocabulary.Count) { return false; }
        reference = (ulong)raw;
        if (reference != 0) { entry = d.Vocabulary.Entries[(int)reference - 1]; }
        return true;
    }
    static bool MessageName(ref BitReader r, MessageRead d, out ulong reference, out TableMessageEntry entry)
    { return MessageReference(ref r, d, out reference, out entry) && reference != 0 && !Reserved(entry.Id) && entry.Kind == 0; }
    // depth is the unknown-skip walk (docs/SPEC-TABLES.md message_skip_depth).
    // Callers that omit it start at 0. A nested body, a union arm and a walked
    // element pass depth + 1. depth > 128 is malformed; 128 itself still skips.
    static bool MessageSkipBody(ref BitReader r, MessageRead d, int depth = 0)
    {
        if (depth > 128) { return false; }
        for (; ; )
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry e)) { return false; }
            if (reference == 0) { return true; }
            if (!MessageSkip(ref r, d, e.Kind, e.Shape, depth)) { return false; }
        }
    }
    static bool MessageSkip(ref BitReader r, MessageRead d, byte kind, TableMessageShape shape, int depth = 0)
    {
        if (depth > 128) { return false; }
        switch (kind)
        {
            case 0: case 32: return true;
            case 30:
                return MessageReference(ref r, d, out ulong variant, out TableMessageEntry ve) &&
                    (variant == 0 || !Reserved(ve.Id) && ve.Kind == 0);
            case 15:
                if (!MessageReference(ref r, d, out ulong arm, out TableMessageEntry ae)) { return false; }
                return arm == 0 || !Reserved(ae.Id) && ae.Kind != 0 && MessageSkip(ref r, d, ae.Kind, ae.Shape, depth + 1);
            case 13: return MessageSkipBody(ref r, d, depth + 1);
            case 17: return false; // a node reference: no fixed body holds one
            case 12:
                if (!r.Get(BitCount(shape.Max), out UInt128 length) || kind == 12 && !r.Align()) { return false; }
                return r.Skip((long)length * (kind == 12 ? 8 : 16));
            case 31:
                return r.Align() && r.Get(32, out UInt128 opaque) && r.Skip((long)opaque * 8);
            case 14:
            case 16:
                ulong min = kind == 16 ? 0 : shape.Min;
                if (!r.Get(BitCount(shape.Max - min), out UInt128 raw) || kind == 14 && shape.Elem == 6 && !r.Align()) { return false; }
                ulong n = (ulong)raw + min;
                if (Width(shape.Elem) != 0 || shape.Elem == 0 || shape.Elem == 32)
                { return r.Skip((long)n * (ValueBits(shape.Elem, shape.Inner) + (kind == 16 ? d.Vocabulary.RefBits : 0))); }
                for (ulong i = 0; i < n; i++)
                {
                    if (kind == 16 && !r.Get(d.Vocabulary.RefBits, out _)) { return false; }
                    if (shape.Elem == 13 || shape.Elem == 15 || shape.Elem == 17 || shape.Elem == 30)
                    { if (!MessageSkip(ref r, d, shape.Elem, shape.Inner, depth + 1)) { return false; } }
                    else { int bits = ValueBits(shape.Elem, shape.Inner); if (bits < 0 || !r.Skip(bits)) { return false; } }
                }
                return true;
            default: return MessageKind(kind) && r.Skip(ValueBits(kind, shape));
        }
    }
    static bool MessageCompatible(TableMessageEntry entry, TableFieldInfo f, out bool widened)
    {
        byte mine = Kind(f), elem = f.IsArray ? f.Kind : (byte)0;
        widened = entry.Shape.Elem == elem ? Widen(entry.Kind, mine) : entry.Kind == mine && mine == 14 && Widen(entry.Shape.Elem, elem);
        return entry.Kind == mine && entry.Shape.Elem == elem || widened;
    }
    static bool MessageReadBody(ref BitReader r, MessageRead d, object value, TableTypeInfo type, int depth = 0)
    {
        if (depth > 128) { return false; }
        if (value != null) { type.Reset(value); }
        for (; ; )
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
            if (reference == 0) { return true; }
            if (Reserved(entry.Id)) { return false; }
            TableFieldInfo field = FindField(type, entry.Id);
            if (field == null)
            { d.Report.Unknown++; if (!MessageSkip(ref r, d, entry.Kind, entry.Shape, depth)) { return false; } continue; }
            if (!MessageCompatible(entry, field, out bool widened))
            { d.Report.KindMismatch++; if (!MessageSkip(ref r, d, entry.Kind, entry.Shape, depth)) { return false; } continue; }
            if (!MessageReadField(ref r, d, value, field, entry.Kind, entry.Shape, depth)) { return false; }
            if (widened && !field.MapKey) { d.Report.Widened++; }
            if (field.Optional && value != null) { field.SetPresent(value, true); }
        }
    }
    static bool MessageReadField(ref BitReader r, MessageRead d, object value, TableFieldInfo f, byte kind, TableMessageShape shape, int depth = 0)
    {
        if (f.IsArray && f.GetBuffer != null)
        {
            if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || !r.Align()) { return false; }
            ulong n = (ulong)raw;
            if (n > (ulong)((r.End - r.At) / 8)) { return false; }
            int kept = (int)Math.Min(n, (ulong)f.ArrayBound);
            if (n > (ulong)f.ArrayBound) { d.Report.Clamped++; }
            if (value != null) { r.Bytes.Slice((int)(r.At / 8), kept).CopyTo(f.GetBuffer(value)); f.SetCount(value, kept); }
            r.At += (long)n * 8; return true;
        }
        if (kind == 12)
        {
            if (!r.Get(BitCount(shape.Max), out UInt128 length) || !r.Align()) { return false; }
            long bits = (long)length * 8;
            if (bits > r.End - r.At) { return false; }
            ReadOnlySpan<byte> text = r.Bytes.Slice((int)(r.At / 8), (int)length); r.At += bits;
            return ReadText(text, value, f, d.Report);
        }
        if (f.IsArray)
        {
            if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || kind == 14 && shape.Elem == 6 && !r.Align()) { return false; }
            ulong n = (ulong)raw + shape.Min;
            if (f.KeyId != null)
            {
                for (ulong i = 0; i < n; i++)
                {
                    if (!MessageName(ref r, d, out _, out TableMessageEntry key)) { return false; }
                    int slot = FindVariant(f, key.Id, true);
                    if (slot == 0) { d.Report.Unknown++; }
                    if (!MessageReadElement(ref r, d, value, f, slot - 1, shape.Elem, shape.Inner, slot != 0, depth)) { return false; }
                }
                return true;
            }
            int run = Width(shape.Elem) != 0 ? ValueBits(shape.Elem, shape.Inner) : -1;
            int kept = (int)Math.Min(n, (ulong)f.ArrayBound);
            if (n > (ulong)f.ArrayBound) { d.Report.Clamped++; }
            int previous = f.Counted && value != null && f.GetCount != null ? f.GetCount(value) : 0;
            // A FLOAT PAST THE BOUND IS STILL TESTED (docs/SPEC-TABLES.md §3.3):
            // the stream walks every element, the dropped tail into no slot, so
            // its NaN or infinity refuses the batch as a kept one does.
            bool tested = shape.Elem == 11 || shape.Elem == 10 && shape.Inner.Packing != 2;
            ulong walk = run >= 0 && !tested ? (ulong)kept : n;
            for (ulong i = 0; i < walk; i++)
            {
                if (!MessageReadElement(ref r, d, value, f, (int)i, shape.Elem, shape.Inner, i < (ulong)kept, depth)) { return false; }
            }
            if (walk < n && !r.Skip((long)(n - walk) * run)) { return false; }
            if (f.Counted && value != null) { ResetCountedTail(value, f, previous, kept); }
            if (f.Optional && value != null) { f.SetPresent(value, true); }
            return true;
        }
        return MessageReadElement(ref r, d, value, f, 0, kind, shape, true, depth);
    }
    static bool MessageReadElement(ref BitReader r, MessageRead d, object value, TableFieldInfo f, int index, byte kind, TableMessageShape shape, bool keep, int depth = 0)
    {
        if (depth > 128) { return false; }
        keep = keep && value != null;
        if (kind == 13)
        { return MessageReadBody(ref r, d, keep ? f.GetChild(value, index) : null, f.Table, depth + 1); }
        if (kind == 30)
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry) || reference != 0 && (entry.Kind != 0 || Reserved(entry.Id))) { return false; }
            int variant = reference == 0 ? 0 : FindVariant(f, entry.Id, false);
            if (reference != 0 && variant == 0) { d.Report.Unknown++; }
            if (keep) { f.SetRaw(value, index, (ulong)variant); }
            return true;
        }
        if (kind == 15)
        {
            if (depth + 1 > 128) { return false; }
            object union = keep ? f.GetChild(value, index) : null;
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
            if (reference == 0) { if (union != null) { f.Arms.SetTag(union, 0); } return true; }
            if (entry.Kind == 0 || Reserved(entry.Id)) { return false; }
            int tag = FindVariant(f, entry.Id, false);
            if (tag == 0)
            { if (union != null) { f.Arms.SetTag(union, 0); } d.Report.Unknown++; return MessageSkip(ref r, d, entry.Kind, entry.Shape, depth + 1); }
            TableFieldInfo arm = f.Arms.Arms[tag].Field;
            bool widened = false;
            if (arm == null ? entry.Kind != 32 : !MessageCompatible(entry, arm, out widened))
            { if (union != null) { f.Arms.SetTag(union, 0); } d.Report.KindMismatch++; return MessageSkip(ref r, d, entry.Kind, entry.Shape, depth + 1); }
            if (widened) { d.Report.Widened++; }
            if (union != null) { f.Arms.SetTag(union, (ulong)tag); }
            if (arm == null) { return true; }
            if (union != null) { ResetArm(union, arm); }
            return MessageReadField(ref r, d, union, arm, entry.Kind, entry.Shape, depth + 1);
        }
        int width = ValueBits(kind, shape);
        if (!r.Get(width, out UInt128 bits)) { return false; }
        if (kind == 10)
        {
            if (shape.Packing == 2)
            {
                if (bits > shape.Steps) { return false; }
                float normalized = (float)(uint)bits / shape.Steps;
                float scaled = normalized * shape.Delta;
                bits = TableFloatToBits(scaled + shape.QMin);
            }
            // refused on its raw bits, kept or dropped (docs/SPEC-TABLES.md §3.3)
            else if (NonFiniteBits(10, (ulong)bits)) { NonFinite(f, d.Report); return true; }
            ulong raw = f.Kind == 11 ? WidenFloat((uint)bits) : (uint)bits;
            if (f.Kind != 11 && f.ClampRaw != null) { raw = f.ClampRaw(raw, d.Report); }
            if (keep) { f.SetRaw(value, index, raw); }
            return true;
        }
        if (kind == 11 && NonFiniteBits(11, (ulong)bits)) { NonFinite(f, d.Report); return true; }
        if (kind == 1 || kind == 11)
        { if (keep) { f.SetRaw(value, index, (ulong)bits); } return true; }
        UInt128 stored = bits; bool negative = false, above = false;
        if (shape.Packing == 1)
        {
            stored = unchecked(bits + shape.Base);
            bool baseNegative = (kind >= 18 || Signed(kind)) && unchecked((Int128)shape.Base) < 0;
            negative = baseNegative && bits < unchecked(0 - shape.Base);
            above = !baseNegative && stored < bits;
            if (kind < 18)
            {
                ulong raw = (ulong)stored;
                negative = Signed(kind) && unchecked((long)raw) < 0; above = false;
                stored = negative ? unchecked((UInt128)(Int128)(long)raw) : raw;
            }
        }
        else if (Signed(kind) && width > 0 && ((bits >> (width - 1)) & 1) != 0)
        { negative = true; if (width < 128) { stored |= UInt128.MaxValue << width; } }
        if (f.MessageBounded)
        {
            bool low, high;
            if (f.MessageSigned)
            {
                low = negative && unchecked((Int128)stored) < unchecked((Int128)f.MessageMin);
                high = above || !negative && stored > (UInt128)Int128.MaxValue || !low && unchecked((Int128)stored) > unchecked((Int128)f.MessageMax);
                if (!negative && unchecked((Int128)f.MessageMin) > 0 && stored < f.MessageMin) { low = true; }
            }
            else { low = negative || stored < f.MessageMin; high = above || !negative && stored > f.MessageMax; }
            if (low) { stored = f.MessageMin; d.Report.Clamped++; }
            else if (high) { stored = f.MessageMax; d.Report.Clamped++; }
        }
        if (keep)
        {
            if (f.SetWide != null) { f.SetWide(value, index, stored); }
            else if (f.GetBuffer != null) { f.GetBuffer(value)[index] = (byte)stored; }
            else { f.SetRaw(value, index, (ulong)stored); }
        }
        return true;
    }
    public static int MessageCount(ReadOnlySpan<byte> bytes, TableVocabulary vocabulary, TableReport report)
    {
        if (bytes.Length == 0) { Damage(report); return 0; }
        if (bytes[0] != 2) { Refuse(report, "newer_form"); return 0; }
        if (vocabulary == null || !vocabulary.Announced) { Refuse(report, "no_vocabulary"); return 0; }
        if (bytes.Length < 2) { Damage(report); return 0; }
        return bytes[1] + 1;
    }
    public static Verdict MessageLoad(object[] values, TableTypeInfo type, ReadOnlySpan<byte> bytes, TableVocabulary vocabulary, TableReport report, out int count)
    {
        count = 0; report.NonFiniteFloat = null;
        int total = MessageCount(bytes, vocabulary, report);
        if (report.Refused) { return Finish(report, Verdict.Refused); }
        if (total == 0) { return Finish(report, Verdict.Damaged); }
        if (total > values.Length) { count = total; return Refuse(report, "batch_too_large"); }
        BitReader r = new BitReader(bytes); r.At = 16;
        for (; count < total; count++)
        {
            object value = values[count]; type.Reset(value);
            var d = new MessageRead { Vocabulary = vocabulary, Report = report };
            bool good = MessageReadBody(ref r, d, value, type);
            // A NON-FINITE FLOAT IN ANY BODY REFUSES THE BATCH (docs/SPEC-TABLES.md
            // §3.3), outranking damage met after it: no body is delivered.
            if (NonFiniteRefused(report))
            { for (int j = 0; j <= count; j++) { type.Reset(values[j]); } count = 0; return Finish(report, Verdict.Refused); }
            if (!good) { Damage(report); return Finish(report, Verdict.Damaged); }
        }
        if (!r.Align() || r.At != r.End) { Damage(report); return Finish(report, Verdict.Damaged); }
        return Finish(report, Verdict.Ok);
    }
`
