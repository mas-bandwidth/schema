package cstable

const tableRegionMessageReadSource = `
    static bool NativeMessageReadBody(ref NativeState state, ref BitReader r, MessageRead d, NativeValue value, TableTypeInfo type)
    {
        if(value.Base!=null) { NativeReset(value,type); }
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
            if (!NativeMessageReadField(ref state, ref r, d, value, field, entry.Kind, entry.Shape)) { return false; }
            if (widened && !field.MapKey) { d.Report.Widened++; }
            if (field.Optional && value.Base!=null) { value.SetPresent(field, true); }
        }
    }
    static bool NativeMessageReadField(ref NativeState state, ref BitReader r, MessageRead d, NativeValue value, TableFieldInfo f, byte kind, TableMessageShape shape)
    {
        if (f.Map) { return NativeMessageReadMap(ref state, ref r, d, value, f, shape); }
        if (f.IsArray && f.GetBuffer != null)
        {
            if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || !r.Align()) { return false; }
            ulong n = (ulong)raw;
            if (n > (ulong)((r.End - r.At) / 8)) { return false; }
            int kept = (int)Math.Min(n, (ulong)f.ArrayBound);
            if (n > (ulong)f.ArrayBound) { d.Report.Clamped++; }
            if(value.Base!=null) { r.Bytes.Slice((int)(r.At / 8), kept).CopyTo(value.Buffer(f)); value.SetCount(f,kept); } r.At += (long)n * 8; return true;
        }
        if (kind == 12 || kind == 33)
        {
            if (!r.Get(BitCount(shape.Max), out UInt128 length) || kind == 12 && !r.Align()) { return false; }
            long bits = (long)length * (kind == 12 ? 8 : 16);
            if (bits > r.End - r.At) { return false; }
            if (kind == 12)
            {
                ReadOnlySpan<byte> text = r.Bytes.Slice((int)(r.At / 8), (int)length); r.At += bits;
                return NativeReadText(text,value,f,d.Report);
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
            for (int i = 0; i < keep; i++) { r.Get(16, out UInt128 raw); last=(char)(uint)raw; if(value.Base!=null) { value.Chars(f)[i]=last; } }
            if (keep > 0 && keep < (int)length && char.IsHighSurrogate(last)) { keep--; }
            if(value.Base!=null) { value.SetCount(f,keep); } r = check; return true;
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
                    if (!NativeMessageReadElement(ref state, ref r, d, value, f, slot - 1, shape.Elem, shape.Inner, slot != 0)) { return false; }
                }
                return true;
            }
            int run = Width(shape.Elem) != 0 ? ValueBits(shape.Elem, shape.Inner) : -1;
            if (f.Dynamic && (n > int.MaxValue || run!=0 && n > (ulong)((r.End - r.At) / Math.Max(1, run)))) { return false; }
            int kept = (int)Math.Min(n, (ulong)f.ArrayBound);
            if (n > (ulong)f.ArrayBound) { d.Report.Clamped++; }
            if(f.Dynamic && !NativeReserve(ref state,value,f,n,d.Report)) { return false; }
            ulong walk = run >= 0 ? (ulong)kept : n;
            for (ulong i = 0; i < walk; i++)
            {

                if (!NativeMessageReadElement(ref state, ref r, d, value, f, (int)i, shape.Elem, shape.Inner, i < (ulong)kept)) { return false; }
            }
            if (walk < n && !r.Skip((long)(n - walk) * run)) { return false; }
            if (f.Counted && value.Base!=null) { value.SetCount(f, kept); }
            if (f.Optional && value.Base!=null) { value.SetPresent(f, true); }
            return true;
        }
        return NativeMessageReadElement(ref state, ref r, d, value, f, 0, kind, shape, true);
    }
    static bool NativeMessageReadElement(ref NativeState state, ref BitReader r, MessageRead d, NativeValue value, TableFieldInfo f, int index, byte kind, TableMessageShape shape, bool keep)
    {
        keep = keep && value.Base!=null;
        if (kind == 13)
        { return NativeMessageReadBody(ref state, ref r, d, keep ? value.Child(f,index) : default, f.Table); }
        if (kind == 17)
        {
            if (d.IndexBits == 0 || !r.Get(d.IndexBits, out UInt128 raw)) { return false; }
            NativePointer(ref state,keep?value:default,f,index,(ulong)raw,d.Report);
            return true;
        }
        if (kind == 30)
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry) || reference != 0 && (entry.Kind != 0 || Reserved(entry.Id))) { return false; }
            int variant = reference == 0 ? 0 : FindVariant(f, entry.Id, false);
            if (reference != 0 && variant == 0) { d.Report.Unknown++; }
            if (keep) { value.SetRaw(f,index,(ulong)variant); } return true;
        }
        if (kind == 15)
        {
            NativeValue union = keep ? value.Child(f,index) : default;
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
            if (reference == 0) { if(union.Base!=null) { union.SetTag(f,0); } return true; }
            if (entry.Kind == 0 || Reserved(entry.Id)) { return false; }
            int tag = FindVariant(f, entry.Id, false);
            if (tag == 0)
            { if(union.Base!=null) { union.SetTag(f,0); } d.Report.Unknown++; return MessageSkip(ref r, d, entry.Kind, entry.Shape); }
            TableFieldInfo arm = f.Arms.Arms[tag].Field;
            bool widened = false;
            if (arm == null ? entry.Kind != 32 : !MessageCompatible(entry, arm, out widened))
            { if(union.Base!=null) { union.SetTag(f,0); } d.Report.KindMismatch++; return MessageSkip(ref r, d, entry.Kind, entry.Shape); }
            if (widened) { d.Report.Widened++; }
            if(union.Base!=null) { union.SetTag(f,(ulong)tag); }
            if (arm == null) { return true; }
            if(union.Base!=null) { NativeResetArm(union.Arm(f),arm); }
            return NativeMessageReadField(ref state, ref r, d, union.Arm(f), arm, entry.Kind, entry.Shape);
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
            if (keep) { value.SetRaw(f,index,raw); } return true;
        }
        if (kind == 1 || kind == 11)
        { if (keep) { value.SetRaw(f,index,(ulong)bits); } return true; }
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
            if (f.SetWide != null) { value.SetWide(f,index,stored); }
            else if (f.GetBuffer != null) { value.Buffer(f)[index] = (byte)stored; }
            else { value.SetRaw(f,index,(ulong)stored); }
        }
        return true;
    }
    static bool NativeMessageMapKey(BitReader r, MessageRead d, TableFieldInfo keyField, out NativeMapKey key, out bool mismatch, out bool widened, out long end)
    {
        key = default; mismatch = false; widened = false; end=r.At;
        for (;;)
        {
            if (!MessageReference(ref r,d,out ulong reference,out TableMessageEntry entry)) { return false; }
            if (reference == 0) { end=r.At; return true; }
            if (Reserved(entry.Id)) { return false; }
            if (entry.Id != keyField.Id) { if (!MessageSkip(ref r,d,entry.Kind,entry.Shape)) { return false; } continue; }
            mismatch = entry.Kind != keyField.Kind && !Widen(entry.Kind,keyField.Kind);
            widened = keyField.Kind != 12 && entry.Kind != keyField.Kind;
            if (mismatch) { if (!MessageSkip(ref r,d,entry.Kind,entry.Shape)) { return false; } continue; }
            if (keyField.Kind == 12)
            {
                if (!r.Get(BitCount(entry.Shape.Max),out UInt128 n) || !r.Align() || n > (UInt128)((r.End-r.At)/8)) { return false; }
                key.Text = r.Bytes.Slice((int)(r.At/8),(int)n); r.At += (long)n*8;
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
    static bool NativeMessageReadMap(ref NativeState state, ref BitReader r, MessageRead d, NativeValue value, TableFieldInfo f, TableMessageShape shape)
    {
        if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || raw + shape.Min > int.MaxValue) { return false; }
        ulong count = (ulong)raw + shape.Min; if(value.Base!=null) { NativeResetField(value,f,true); }
        if(!NativeReserve(ref state,value,f,count,d.Report)) { return false; }
        int landed = 0; bool widened = false; NativeMapKey last = default; TableFieldInfo keyField = f.Table.Fields[0];
        for (ulong i = 0; i < count; i++)
        {
            BitReader scan = r;
            if (!NativeMessageMapKey(scan,d,keyField,out NativeMapKey key,out bool mismatch,out bool entryWidened,out long end)) { return false; }
            scan.At=end;
            if (entryWidened && !widened) { widened = true; d.Report.Widened++; }
            if (mismatch)
            {
                d.Report.KindMismatch++; if(value.Base!=null) { NativeResetField(value,f,true); } r = scan;
                for (ulong j=i+1;j<count;j++) { if (!MessageSkipBody(ref r,d)) { return false; } }
                return true;
            }
            if (keyField.Kind == 12 && key.Text.Length > keyField.ArrayBound) { d.Report.Clamped++; r = scan; continue; }
            int order = landed == 0 ? -1 : keyField.Kind==12 ? last.Text.SequenceCompareTo(key.Text) : keyField.Kind>=2 && keyField.Kind<=5 ? unchecked((long)last.Raw).CompareTo(unchecked((long)key.Raw)) : last.Raw.CompareTo(key.Raw);
            if (order > 0) { return false; }
            int slot = order == 0 ? landed-1 : landed;
            if (order == 0) { d.Report.Duplicate++; } else { landed++; }
            NativeValue entry=value.Child(f,slot); value.SetCount(f,landed);
            if (!NativeMessageReadBody(ref state, ref r,d,entry,f.Table) || r.At != scan.At) { return false; }
            last = key;
        }
        return true;
    }
`
