package cstable

const tableMessageReadSource = `
    struct MessageRead
    {
        public TableVocabulary Vocabulary;
        public TableReport Report;
        public Graph Graph;
        public int IndexBits;
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
    static bool MessageSkipBody(ref BitReader r, MessageRead d)
    {
        for (;;)
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry e)) { return false; }
            if (reference == 0) { return true; }
            if (!MessageSkip(ref r, d, e.Kind, e.Shape)) { return false; }
        }
    }
    static bool MessageSkip(ref BitReader r, MessageRead d, byte kind, TableMessageShape shape)
    {
        switch (kind)
        {
            case 0: case 32: return true;
            case 30:
                return MessageReference(ref r, d, out ulong variant, out TableMessageEntry ve) &&
                    (variant == 0 || !Reserved(ve.Id) && ve.Kind == 0);
            case 15:
                if (!MessageReference(ref r, d, out ulong arm, out TableMessageEntry ae)) { return false; }
                return arm == 0 || !Reserved(ae.Id) && ae.Kind != 0 && MessageSkip(ref r, d, ae.Kind, ae.Shape);
            case 13: return MessageSkipBody(ref r, d);
            case 17: return d.IndexBits > 0 && r.Skip(d.IndexBits);
            case 12: case 33:
                if (!r.Get(BitCount(shape.Max), out UInt128 length) || kind == 12 && !r.Align()) { return false; }
                return r.Skip((long)length * (kind == 12 ? 8 : 16));
            case 31:
                return r.Align() && r.Get(32, out UInt128 opaque) && r.Skip((long)opaque * 8);
            case 14: case 16:
                if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || kind == 14 && shape.Elem == 6 && !r.Align()) { return false; }
                ulong n = (ulong)raw + shape.Min;
                if (kind == 14 && (Width(shape.Elem) != 0 || shape.Elem == 0 || shape.Elem == 32))
                { return r.Skip((long)n * ValueBits(shape.Elem, shape.Inner)); }
                for (ulong i = 0; i < n; i++)
                {
                    if (kind == 16 && !r.Get(d.Vocabulary.RefBits, out _)) { return false; }
                    if (shape.Elem == 13 || shape.Elem == 15 || shape.Elem == 17 || shape.Elem == 30)
                    { if (!MessageSkip(ref r, d, shape.Elem, shape.Inner)) { return false; } }
                    else { int bits=ValueBits(shape.Elem,shape.Inner); if(bits<0 || !r.Skip(bits)) { return false; } }
                }
                return true;
            default: return MessageKind(kind) && r.Skip(ValueBits(kind, shape));
        }
    }
    sealed class MessageRecord
    {
        public ulong Id;
        public long Start, End;
        public bool Blob;
    }
    static bool MessageNodes(ref BitReader r, ref MessageRead d, object value, TableTypeInfo type)
    {
        d.Graph = new Graph { Root = value, RootType = type }; d.IndexBits = 1;
        long before = r.At;
        if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
        if (reference == 0 || entry.Id != ulong.MaxValue) { r.At = before; return true; }
        if (!r.Get(32, out UInt128 rawCount)) { return false; }
        ulong count = (ulong)rawCount; d.IndexBits = BitCount(count + 1);
        var records = new System.Collections.Generic.List<MessageRecord>();
        for (ulong i = 0; i < count; i++)
        {
            if (!MessageName(ref r, d, out _, out TableMessageEntry named)) { return false; }
            var rec = new MessageRecord { Id = named.Id, Start = r.At, Blob = named.Id == BytesTypeId || named.Id == StringTypeId };
            if (rec.Blob)
            {
                if (!r.Get(32, out UInt128 length) || !r.Align()) { return false; }
                rec.Start = r.At;
                if (!r.Skip((long)length * 8)) { return false; }
            }
            else if (!MessageSkipBody(ref r, d)) { return false; }
            rec.End = r.At; records.Add(rec);
        }
        TableTypeInfo[] placeable = type.PointerTypes();
        foreach (MessageRecord rec in records)
        {
            Node node = new Node { TypeId = rec.Id }; d.Graph.Nodes.Add(node);
            if (rec.Blob)
            {
                if (rec.Id == BytesTypeId && type.BytesEdge || rec.Id == StringTypeId && type.StringEdge)
                {
                    ReadOnlySpan<byte> text = r.Bytes.Slice((int)(rec.Start / 8), (int)((rec.End - rec.Start) / 8));
                    if (rec.Id == StringTypeId && !TextValid(text)) { return false; }
                    node.BlobKind = rec.Id == StringTypeId ? (byte)12 : (byte)14; node.Value = new TableBlob(text.ToArray());
                }
            }
            else
            {
                foreach (TableTypeInfo candidate in placeable)
                { if (candidate.Id == rec.Id) { node.Type = candidate; node.Value = candidate.Create(); break; } }
            }
            if (node.Value == null) { d.Report.Unknown++; }
        }
        for (int i = 0; i < records.Count; i++)
        {
            Node node = d.Graph.Nodes[i]; if (node.Type == null) { continue; }
            BitReader body = r; body.At = records[i].Start; body.End = records[i].End;
            if (!MessageReadBody(ref body, d, node.Value, node.Type)) { return false; }
        }
        return true;
    }
    static bool MessageCompatible(TableMessageEntry entry, TableFieldInfo f, out bool widened)
    {
        byte mine = Kind(f), elem = f.IsArray ? f.Kind : (byte)0;
        widened = entry.Shape.Elem == elem ? Widen(entry.Kind, mine) : entry.Kind == mine && mine == 14 && Widen(entry.Shape.Elem, elem);
        return entry.Kind == mine && entry.Shape.Elem == elem || widened;
    }
    static bool MessageReadBody(ref BitReader r, MessageRead d, object value, TableTypeInfo type)
    {
        if(value!=null) { type.Reset(value); }
        for (;;)
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
            if (reference == 0) { return true; }
            if (Reserved(entry.Id)) { return false; }
            TableFieldInfo field = FindField(type, entry.Id);
            if (field == null)
            { d.Report.Unknown++; if (!MessageSkip(ref r, d, entry.Kind, entry.Shape)) { return false; } continue; }
            if (!MessageCompatible(entry, field, out bool widened))
            { d.Report.KindMismatch++; if (!MessageSkip(ref r, d, entry.Kind, entry.Shape)) { return false; } continue; }
            if (!MessageReadField(ref r, d, value, field, entry.Kind, entry.Shape)) { return false; }
            if (widened && !field.MapKey) { d.Report.Widened++; }
            if (field.Optional && value!=null) { field.SetPresent(value, true); }
        }
    }
    static bool MessageReadField(ref BitReader r, MessageRead d, object value, TableFieldInfo f, byte kind, TableMessageShape shape)
    {
        if (f.Map) { return MessageReadMap(ref r, d, value, f, shape); }
        if (f.IsArray && f.GetBuffer != null)
        {
            if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || !r.Align()) { return false; }
            ulong n = (ulong)raw;
            if (n > (ulong)((r.End - r.At) / 8)) { return false; }
            int kept = (int)Math.Min(n, (ulong)f.ArrayBound);
            if (n > (ulong)f.ArrayBound) { d.Report.Clamped++; }
            if(value!=null) { r.Bytes.Slice((int)(r.At / 8), kept).CopyTo(f.GetBuffer(value)); f.SetCount(value,kept); } r.At += (long)n * 8; return true;
        }
        if (kind == 12 || kind == 33)
        {
            if (!r.Get(BitCount(shape.Max), out UInt128 length) || kind == 12 && !r.Align()) { return false; }
            long bits = (long)length * (kind == 12 ? 8 : 16);
            if (bits > r.End - r.At) { return false; }
            if (kind == 12)
            {
                ReadOnlySpan<byte> text = r.Bytes.Slice((int)(r.At / 8), (int)length); r.At += bits;
                return ReadText(text, value, f, d.Report);
            }
            BitReader check = r;
            for (int i = 0; i < (int)length; i++)
            {
                check.Get(16, out UInt128 raw); uint c = (uint)raw;
                if (c == 0 || c >= 0xdc00 && c <= 0xdfff) { return false; }
                if (c >= 0xd800 && c <= 0xdbff)
                { if (++i == (int)length || !check.Get(16, out UInt128 low) || low < 0xdc00 || low > 0xdfff) { return false; } }
            }
            int keep = Math.Min((int)length, f.ArrayBound);
            if (length > (uint)f.ArrayBound) { d.Report.Clamped++; }
            char last=default;
            for (int i = 0; i < keep; i++) { r.Get(16, out UInt128 raw); last=(char)(uint)raw; if(value!=null) { f.GetChars(value)[i]=last; } }
            if (keep > 0 && keep < (int)length && char.IsHighSurrogate(last)) { keep--; }
            if(value!=null) { f.SetCount(value,keep); } r = check; return true;
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
                    if (!MessageReadElement(ref r, d, value, f, slot - 1, shape.Elem, shape.Inner, slot != 0)) { return false; }
                }
                return true;
            }
            int run = Width(shape.Elem) != 0 ? ValueBits(shape.Elem, shape.Inner) : -1;
            if (f.Dynamic && (n > int.MaxValue || run!=0 && n > (ulong)((r.End - r.At) / Math.Max(1, run)))) { return false; }
            int kept = (int)Math.Min(n, (ulong)f.ArrayBound);
            if (n > (ulong)f.ArrayBound) { d.Report.Clamped++; }
            int previous = f.Counted && value != null && f.GetCount != null ? f.GetCount(value) : 0;
            ulong walk = run >= 0 ? (ulong)kept : n;
            for (ulong i = 0; i < walk; i++)
            {
                if (f.Dynamic && value!=null) { f.EnsureCount(value, (int)i + 1); }
                if (!MessageReadElement(ref r, d, value, f, (int)i, shape.Elem, shape.Inner, i < (ulong)kept)) { return false; }
            }
            if (walk < n && !r.Skip((long)(n - walk) * run)) { return false; }
            if (f.Counted && value!=null) { ResetCountedTail(value, f, previous, kept); }
            if (f.Optional && value!=null) { f.SetPresent(value, true); }
            return true;
        }
        return MessageReadElement(ref r, d, value, f, 0, kind, shape, true);
    }
    static bool MessageReadElement(ref BitReader r, MessageRead d, object value, TableFieldInfo f, int index, byte kind, TableMessageShape shape, bool keep)
    {
        keep = keep && value!=null;
        if (kind == 13)
        { return MessageReadBody(ref r, d, keep ? f.GetChild(value, index) : null, f.Table); }
        if (kind == 17)
        {
            if (d.IndexBits == 0 || !r.Get(d.IndexBits, out UInt128 raw)) { return false; }
            object node = d.Graph.Resolve((ulong)raw, f, d.Report);
            if (keep) { f.SetChild(value, index, node); }
            return true;
        }
        if (kind == 30)
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry) || reference != 0 && (entry.Kind != 0 || Reserved(entry.Id))) { return false; }
            int variant = reference == 0 ? 0 : FindVariant(f, entry.Id, false);
            if (reference != 0 && variant == 0) { d.Report.Unknown++; }
            if (keep) { f.SetRaw(value, index, (ulong)variant); } return true;
        }
        if (kind == 15)
        {
            object union = keep ? f.GetChild(value, index) : null;
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
            if (reference == 0) { if(union!=null) { f.Arms.SetTag(union,0); } return true; }
            if (entry.Kind == 0 || Reserved(entry.Id)) { return false; }
            int tag = FindVariant(f, entry.Id, false);
            if (tag == 0)
            { if(union!=null) { f.Arms.SetTag(union,0); } d.Report.Unknown++; return MessageSkip(ref r, d, entry.Kind, entry.Shape); }
            TableFieldInfo arm = f.Arms.Arms[tag].Field;
            bool widened = false;
            if (arm == null ? entry.Kind != 32 : !MessageCompatible(entry, arm, out widened))
            { if(union!=null) { f.Arms.SetTag(union,0); } d.Report.KindMismatch++; return MessageSkip(ref r, d, entry.Kind, entry.Shape); }
            if (widened) { d.Report.Widened++; }
            if(union!=null) { f.Arms.SetTag(union,(ulong)tag); }
            if (arm == null) { return true; }
            if(union!=null) { ResetArm(union,arm); }
            return MessageReadField(ref r, d, union, arm, entry.Kind, entry.Shape);
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
            ulong raw = f.Kind == 11 ? WidenFloat((uint)bits) : (uint)bits;
            if (f.Kind != 11 && f.ClampRaw != null) { raw = f.ClampRaw(raw, d.Report); }
            if (keep) { f.SetRaw(value, index, raw); } return true;
        }
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
    static bool MessageMapKey(ref BitReader r, MessageRead d, TableFieldInfo keyField, out MapKey key, out bool mismatch, out bool widened)
    {
        key = new MapKey { Text = Array.Empty<byte>() }; mismatch = false; widened = false;
        for (;;)
        {
            if (!MessageReference(ref r,d,out ulong reference,out TableMessageEntry entry)) { return false; }
            if (reference == 0) { return true; }
            if (Reserved(entry.Id)) { return false; }
            if (entry.Id != keyField.Id) { if (!MessageSkip(ref r,d,entry.Kind,entry.Shape)) { return false; } continue; }
            mismatch = entry.Kind != keyField.Kind && !Widen(entry.Kind,keyField.Kind);
            widened = keyField.Kind != 12 && entry.Kind != keyField.Kind;
            if (mismatch) { if (!MessageSkip(ref r,d,entry.Kind,entry.Shape)) { return false; } continue; }
            if (keyField.Kind == 12)
            {
                if (!r.Get(BitCount(entry.Shape.Max),out UInt128 n) || !r.Align() || n > (UInt128)((r.End-r.At)/8)) { return false; }
                key.Text = r.Bytes.Slice((int)(r.At/8),(int)n).ToArray(); r.At += (long)n*8;
            }
            else
            {
                int width = ValueBits(entry.Kind,entry.Shape);
                if (!r.Get(width,out UInt128 raw)) { return false; }
                if (entry.Shape.Packing == 1) { raw = unchecked(raw + entry.Shape.Base); }
                else if (keyField.Kind >= 2 && keyField.Kind <= 5 && width > 0 && width < 128)
                { raw = unchecked((UInt128)((Int128)(raw << (128-width)) >> (128-width))); }
                int destination = Width(keyField.Kind)*8;
                key.Raw = keyField.Kind >= 2 && keyField.Kind <= 5 ? unchecked((ulong)(long)((Int128)(raw << (128-destination)) >> (128-destination))) : (ulong)raw & (destination == 64 ? ulong.MaxValue : (1ul << destination)-1);
            }
        }
    }
    static bool MessageReadMap(ref BitReader r, MessageRead d, object value, TableFieldInfo f, TableMessageShape shape)
    {
        if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || raw + shape.Min > int.MaxValue) { return false; }
        ulong count = (ulong)raw + shape.Min; if(value!=null) { f.ResetField(value); }
        int landed = 0; bool widened = false; MapKey last = default; TableFieldInfo keyField = f.Table.Fields[0];
        for (ulong i = 0; i < count; i++)
        {
            BitReader scan = r;
            if (!MessageMapKey(ref scan,d,keyField,out MapKey key,out bool mismatch,out bool entryWidened)) { return false; }
            if (entryWidened && !widened) { widened = true; d.Report.Widened++; }
            if (mismatch)
            {
                d.Report.KindMismatch++; if(value!=null) { f.ResetField(value); } r = scan;
                for (ulong j=i+1;j<count;j++) { if (!MessageSkipBody(ref r,d)) { return false; } }
                return true;
            }
            if (keyField.Kind == 12 && key.Text.Length > keyField.ArrayBound) { d.Report.Clamped++; r = scan; continue; }
            int order = landed == 0 ? -1 : KeyOrder(last,key,keyField);
            if (order > 0) { return false; }
            int slot = order == 0 ? landed-1 : landed;
            if (order == 0) { d.Report.Duplicate++; } else { landed++; }
            object entry=null;
            if(value!=null) { f.EnsureCount(value,landed); f.SetCount(value,landed); entry=f.Table.Create(); f.SetChild(value,slot,entry); }
            if (!MessageReadBody(ref r,d,entry,f.Table) || r.At != scan.At) { return false; }
            last = key;
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
        count = 0;
        int total = MessageCount(bytes, vocabulary, report);
        if (report.Refused) { return Finish(report, Verdict.Refused); }
        if (total == 0) { return Finish(report, Verdict.Damaged); }
        if (total > values.Length) { count = total; return Refuse(report, "batch_too_large"); }
        BitReader r = new BitReader(bytes); r.At = 16;
        for (; count < total; count++)
        {
            object value = values[count]; type.Reset(value);
            var d = new MessageRead { Vocabulary = vocabulary, Report = report };
            if (type.Variable && !MessageNodes(ref r, ref d, value, type) || !MessageReadBody(ref r, d, value, type))
            { Damage(report); return Finish(report, Verdict.Damaged); }
        }
        if (!r.Align() || r.At != r.End) { Damage(report); return Finish(report, Verdict.Damaged); }
        return Finish(report, Verdict.Ok);
    }
`
