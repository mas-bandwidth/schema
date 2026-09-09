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
            { field = FindField(type, entry.Id); if (field != null && !MessageCompatible(entry,field,out _)) { field = null; } }
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
            if (arm == null || !MessageCompatible(entry,arm,out _)) { return MessageSkip(ref r, d, entry.Kind, entry.Shape); }
            return MessageExtentField(ref r, d, arm, entry.Kind, entry.Shape, ref extent);
        }
        if ((kind == 14 || kind == 16) && (shape.Elem == field.Kind || Widen(shape.Elem,field.Kind)))
        {
            if (!r.Get(BitCount(shape.Max - shape.Min), out UInt128 raw) || kind == 14 && shape.Elem == 6 && !r.Align()) { return false; }
            ulong n = (ulong)raw + shape.Min;
            if (field.Dynamic)
            {
                int floor = Width(shape.Elem)!=0?ValueBits(shape.Elem,shape.Inner):1;
                if (n > int.MaxValue || floor>0 && n > (ulong)((r.End - r.At) / floor)) { return false; }
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
    // Frame one body without constructing its values. The root follows the
    // node records on the wire but precedes them in the region.
    static bool MessageFrame(ref BitReader r,ref MessageRead d,TableTypeInfo type,out long records,out long root,out int count,out long rootExtent,out long data,out bool complete)
    {
        records=root=0; count=0; rootExtent=data=0; complete=false;
        long before=r.At;
        if(!MessageReference(ref r,d,out ulong reference,out TableMessageEntry entry)) { return false; }
        if(reference!=0 && entry.Id==ulong.MaxValue)
        { if(!r.Get(32,out UInt128 raw) || raw>int.MaxValue) { return false; } count=(int)raw; }
        else { r.At=before; }
        d.IndexBits=BitCount((ulong)count+1); records=r.At;
        for(int i=0;i<count;i++)
        {
            if(!MessageName(ref r,d,out _,out TableMessageEntry named)) { return false; }
            if(named.Id==BytesTypeId || named.Id==StringTypeId)
            {
                if(!r.Get(32,out UInt128 length) || !r.Align() || !r.Skip((long)length*8)) { return false; }
                if(named.Id==BytesTypeId && type.BytesEdge || named.Id==StringTypeId && type.StringEdge)
                { data+=Align(8+(long)length+(named.Id==StringTypeId?1:0),type.RegionAlign); }
            }
            else
            {
                TableTypeInfo node=type.PointerType(named.Id); long extent=0;
                if(!MessageExtentBody(ref r,d,node,ref extent)) { return false; }
                if(node!=null) { data+=Align(Align(node.StorageSize,type.RegionAlign)+extent,type.RegionAlign); }
            }
        }
        root=r.At; complete=MessageExtentBody(ref r,d,type,ref rootExtent);
        data+=Align(Align(type.StorageSize,type.RegionAlign)+rootExtent,type.RegionAlign);
        return true;
    }
    public static long MessageLoadMeasure(TableTypeInfo type, ReadOnlySpan<byte> bytes, TableVocabulary vocabulary)
    { return MessageLoadMeasureParts(type,bytes,vocabulary,out _,out _); }
    public static long MessageLoadMeasureParts(TableTypeInfo type,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,out long data,out long attribution)
    {
        data=attribution=0;
        if(vocabulary==null || !vocabulary.Announced || bytes.Length<2 || bytes[0]!=2) { return -1; }
        int bodies=bytes[1]+1; BitReader r=new BitReader(bytes); r.At=16;
        var d=new MessageRead { Vocabulary=vocabulary };
        for(int b=0;b<bodies;b++)
        {
            if(!MessageFrame(ref r,ref d,type,out _,out _,out int count,out _,out long size,out bool complete)) { return -1; }
            data+=size; attribution+=(count+1L)*16;
            if(!complete) { break; }
        }
        return data+attribution;
    }
`
