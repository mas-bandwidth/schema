package cstable

const tableMessageMeasureSource = `
    static bool MessageExtentBody(ref BitReader r, MessageRead d, TableTypeInfo type, ref long extent)
    {
        for (;;)
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
            if (reference == 0) { return true; }
            TableFieldInfo field = null;
            if (type != null)
            { foreach (TableFieldInfo f in type.Fields) { if (f.Id == entry.Id && entry.Kind == Kind(f)) { field = f; break; } } }
            if (field == null) { if (!MessageSkip(ref r, d, entry.Kind, entry.Shape)) { return false; } }
            else if (!MessageExtentField(ref r, d, field, entry.Kind, entry.Shape, ref extent)) { return false; }
        }
    }
    static bool MessageExtentField(ref BitReader r, MessageRead d, TableFieldInfo field, byte kind, TableMessageShape shape, ref long extent)
    {
        if (kind == 13) { return MessageExtentBody(ref r, d, field.Table, ref extent); }
        if (kind == 15)
        {
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return false; }
            if (reference == 0) { return true; }
            if (entry.Kind == 0 || Reserved(entry.Id)) { return false; }
            int tag = FindVariant(field, entry.Id, false);
            TableFieldInfo arm = tag == 0 ? null : field.Arms.Arms[tag].Field;
            if (arm == null || entry.Kind != Kind(arm)) { return MessageSkip(ref r, d, entry.Kind, entry.Shape); }
            return MessageExtentField(ref r, d, arm, entry.Kind, entry.Shape, ref extent);
        }
        if ((kind == 14 || kind == 16) && shape.Elem == field.Kind)
        {
            if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || kind == 14 && shape.Elem == 6 && !r.Align()) { return false; }
            ulong n = (ulong)raw + shape.Min;
            if (field.Dynamic)
            {
                int floor = Math.Max(1, ValueBits(shape.Elem, shape.Inner));
                if (n > int.MaxValue || n > (ulong)((r.End - r.At) / floor)) { return false; }
                extent = Align(extent, field.StorageAlign) + (long)n * field.StorageSize;
            }
            if (kind == 14 && Width(shape.Elem) != 0) { return r.Skip((long)n * ValueBits(shape.Elem, shape.Inner)); }
            for (ulong i = 0; i < n; i++)
            {
                if (kind == 16 && !r.Get(d.Vocabulary.RefBits, out _)) { return false; }
                if (shape.Elem == 13) { if (!MessageExtentBody(ref r, d, field.Table, ref extent)) { return false; } }
                else if (shape.Elem == 15) { if (!MessageExtentField(ref r, d, field, 15, shape.Inner, ref extent)) { return false; } }
                else if (!MessageSkip(ref r, d, shape.Elem, shape.Inner)) { return false; }
            }
            return true;
        }
        return MessageSkip(ref r, d, kind, shape);
    }
    public static long MessageLoadMeasure(TableTypeInfo type, ReadOnlySpan<byte> bytes, TableVocabulary vocabulary)
    {
        if (vocabulary == null || !vocabulary.Announced || bytes.Length < 2 || bytes[0] != 2) { return -1; }
        int bodies = bytes[1] + 1; BitReader r = new BitReader(bytes); r.At = 16;
        long data = 0, attribution = 0;
        var d = new MessageRead { Vocabulary = vocabulary };
        for (int b = 0; b < bodies; b++)
        {
            long before = r.At;
            if (!MessageReference(ref r, d, out ulong reference, out TableMessageEntry entry)) { return -1; }
            ulong records = 0;
            if (reference != 0 && entry.Id == ulong.MaxValue)
            { if (!r.Get(32, out UInt128 raw)) { return -1; } records = (ulong)raw; }
            else { r.At = before; }
            d.IndexBits = BitCount(records + 1);
            for (ulong i = 0; i < records; i++)
            {
                if (!MessageName(ref r, d, out _, out TableMessageEntry named)) { return -1; }
                if (named.Id == BytesTypeId || named.Id == StringTypeId)
                {
                    if (!r.Get(32, out UInt128 length) || !r.Align() || !r.Skip((long)length * 8)) { return -1; }
                    if (named.Id == BytesTypeId && type.BytesEdge || named.Id == StringTypeId && type.StringEdge)
                    { data += Align(8 + (long)length + (named.Id == StringTypeId ? 1 : 0), type.RegionAlign); }
                }
                else
                {
                    TableTypeInfo node = type.PointerType(named.Id);
                    long extent = 0;
                    if (!MessageExtentBody(ref r, d, node, ref extent)) { return -1; }
                    if (node != null) { data += Align(Align(node.StorageSize, type.RegionAlign) + extent, type.RegionAlign); }
                }
            }
            long rootExtent = 0;
            bool complete = MessageExtentBody(ref r, d, type, ref rootExtent);
            data += Align(Align(type.StorageSize, type.RegionAlign) + rootExtent, type.RegionAlign);
            attribution += ((long)records + 1) * 16;
            if (!complete) { break; }
        }
        return data + attribution;
    }
`
