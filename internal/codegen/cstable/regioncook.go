package cstable

func (g *tableGen) emitRegionCookSurface(names []string) {
	for _, n := range names {
		if g.unit.Tables[n].IsMapEntry() {
			continue
		}
		g.pf("public static long %sCookMeasure(IntPtr region,TableAllocator allocator=default) { return TableWire.CookRegion(region,%sTableType(),IntPtr.Zero,0,TableByteOrder.Little,true,allocator); }\n", n, n)
		g.pf("public static bool %sCook(IntPtr region,IntPtr bytes,long capacity,TableByteOrder order=TableByteOrder.Little,TableAllocator allocator=default) { return TableWire.CookRegion(region,%sTableType(),bytes,capacity,order,false,allocator)>=0; }\n", n, n)
		g.pf("public static unsafe bool %sCook(IntPtr region,Span<byte> bytes,TableByteOrder order=TableByteOrder.Little,TableAllocator allocator=default) { fixed(byte* p=bytes) { return %sCook(region,(IntPtr)p,bytes.Length,order,allocator); } }\n", n, n)
	}
}

const tableRegionCookSource = `
    // Native authoring walks keep values in place. Only the identity map
    // allocates, through the caller's hooks, and it dies before return.
    unsafe struct RegionPackWriter
    {
        public byte* Bytes;
        public bool Big;
        public long Base;
        public RegionGraph Graph;
        public void Put(long at,UInt128 raw,int width)
        {
            if(Bytes==null) { return; }
            for(int i=0;i<width;i++) { Bytes[Base+at+(Big?width-1-i:i)]=(byte)(raw>>(i*8)); }
        }
        public void Raw(long at,ReadOnlySpan<byte> bytes)
        { if(Bytes!=null) { bytes.CopyTo(new Span<byte>(Bytes+Base+at,bytes.Length)); } }
    }
    static bool RegionExtentSize(NativeValue value,TableTypeInfo type,ref long at)
    { foreach(TableFieldInfo f in type.Fields) { if(!RegionExtentSize(value,f,ref at)) { return false; } } return true; }
    static bool RegionExtentSize(NativeValue value,TableFieldInfo f,ref long at)
    {
        int n=f.IsArray?RegionCount(value,f):1;
        if(n<0 || f.IsArray && n>f.ArrayBound) { return false; }
        if(f.Dynamic) { at=checked(Align(at,f.StorageAlign)+(long)n*f.NativeElementSize); }
        for(int i=0;i<n;i++)
        {
            if(f.Kind==13) { if(!RegionExtentSize(value.Child(f,i),f.Table,ref at)) { return false; } }
            else if(f.Kind==15)
            {
                NativeValue union=value.Child(f,i); ulong tag=union.Tag(f);
                if(tag>(ulong)f.EnumMax) { return false; }
                TableFieldInfo arm=f.Arms.Arms[(int)tag].Field;
                if(arm!=null && !RegionExtentSize(union.Arm(f),arm,ref at)) { return false; }
            }
        }
        return true;
    }
    static bool RegionPackRecord(ref RegionPackWriter w,long at,NativeValue value,TableTypeInfo type,long extentAt,ref long extent)
    { foreach(TableFieldInfo f in type.Fields) { if(!RegionPackField(ref w,at,value,f,extentAt,ref extent)) { return false; } } return true; }
    static bool RegionPackField(ref RegionPackWriter w,long record,NativeValue value,TableFieldInfo f,long extentAt,ref long extent)
    {
        long at=record+f.NativeOffset;
        if(f.Optional) { w.Put(record+f.NativePresentOffset,value.Present(f)?1u:0u,1); }
        int live=f.Counted?value.Count(f):f.IsArray?f.ArrayBound:1;
        if(live<0 || f.Counted && live>f.ArrayBound) { return false; }
        if(f.NativeCountOffset>=0) { w.Put(record+f.NativeCountOffset,(uint)live,4); }
        if(f.Kind==12) { w.Raw(at,value.Buffer(f).Slice(0,live)); return true; }
        if(f.Kind==33) { for(int i=0;i<live;i++) { w.Put(at+i*2L,value.Chars(f)[i],2); } return true; }
        if(f.GetBuffer!=null) { w.Raw(at,value.Buffer(f).Slice(0,live)); return true; }
        int count=f.IsArray?f.Dynamic?live:f.ArrayBound:1;
        if(f.Dynamic)
        {
            extent=Align(extent,f.StorageAlign); long start=extentAt+extent;
            extent=checked(extent+(long)count*f.NativeElementSize);
            w.Put(at,count==0?0:unchecked((ulong)(start-at)),8); at=start;
        }
        for(int i=0;i<count;i++)
        {
            long element=at+(long)i*f.NativeElementSize,before=extent;
            if(!RegionPackElement(ref w,element,value,f,i,extentAt,ref extent)) { return false; }
            if(!f.Dynamic && f.Counted && i>=live && extent!=before) { return false; }
        }
        return true;
    }
    static bool RegionPackElement(ref RegionPackWriter w,long at,NativeValue value,TableFieldInfo f,int index,long extentAt,ref long extent)
    {
        if(f.Kind==17)
        {
            long target=value.Pointer(f,index),offset=0;
            if(target!=long.MinValue)
            {
                int node=w.Graph.Find(target); if(node==0) { return false; }
                offset=(node==1?0:w.Graph.Entries[node-2].NativeOffset)-at;
            }
            w.Put(at,unchecked((ulong)offset),8); return true;
        }
        if(f.Kind==13) { return RegionPackRecord(ref w,at,value.Child(f,index),f.Table,extentAt,ref extent); }
        if(f.Kind==15)
        {
            NativeValue union=value.Child(f,index); ulong tag=union.Tag(f);
            if(tag>(ulong)f.EnumMax) { return false; }
            w.Put(at,tag,f.Arms.NativeTagSize); TableFieldInfo arm=f.Arms.Arms[(int)tag].Field;
            return arm==null || RegionPackField(ref w,at+f.Arms.NativeArmOffset,union.Arm(f),arm,extentAt,ref extent);
        }
        w.Put(at,NativeWord(value.Slot(f,index),f.NativeElementSize),f.NativeElementSize); return true;
    }
    static long RegionPlace(NativeValue value,TableTypeInfo type,ref RegionGraph graph,out int alignment)
    {
        alignment=Math.Max(8,type.StorageAlign); long extent=0;
        if(!RegionExtentSize(value,type,ref extent)) { return -1; }
        long at=checked(NativeRecordBytes(type)+extent);
        for(int i=0;i<graph.Count;i++)
        {
            ref RegionNode node=ref graph.Entries[i]; TableTypeInfo child=node.BlobKind==0?type.PointerType(node.TypeId):null;
            int align=child==null?8:child.StorageAlign;
            alignment=Math.Max(alignment,align); at=Align(at,align); node.NativeOffset=at;
            if(child==null) { at=checked(at+8+node.Blob.Length+(node.BlobKind==12?1:node.BlobKind==33?2:0)); }
            else
            {
                extent=0; if(!RegionExtentSize(node.Value,child,ref extent)) { return -1; }
                at=checked(at+NativeRecordBytes(child)+extent);
            }
        }
        RegionPackWriter probe=new RegionPackWriter { Graph=graph }; extent=0;
        if(!RegionPackRecord(ref probe,0,value,type,NativeRecordBytes(type),ref extent)) { return -1; }
        foreach(RegionNode node in graph.Nodes)
        {
            if(node.BlobKind!=0) { continue; }
            TableTypeInfo child=type.PointerType(node.TypeId); extent=0;
            if(!RegionPackRecord(ref probe,node.NativeOffset,node.Value,child,node.NativeOffset+NativeRecordBytes(child),ref extent)) { return -1; }
        }
        return Align(at,alignment);
    }
    static void RegionPack(ref RegionPackWriter w,NativeValue root,TableTypeInfo type,long data,bool attribution)
    {
        long extent=0;
        RegionPackRecord(ref w,0,root,type,NativeRecordBytes(type),ref extent);
        if(attribution) { w.Put(data,0,8); w.Put(data+8,type.Id,8); }
        for(int i=0;i<w.Graph.Count;i++)
        {
            RegionNode node=w.Graph.Entries[i];
            if(node.BlobKind!=0) { w.Put(node.NativeOffset,(uint)node.Blob.Length,4); w.Raw(node.NativeOffset+8,node.Blob); }
            else
            {
                TableTypeInfo child=type.PointerType(node.TypeId); extent=0;
                RegionPackRecord(ref w,node.NativeOffset,node.Value,child,node.NativeOffset+NativeRecordBytes(child),ref extent);
            }
            if(attribution) { w.Put(data+(i+1)*16L,(ulong)node.NativeOffset,8); w.Put(data+(i+1)*16L+8,node.TypeId,8); }
        }
    }
    public static long CookRegion(IntPtr pointer,TableTypeInfo type,IntPtr bytes,long capacity,TableByteOrder order,bool measure,TableAllocator allocator=default,bool mutable=false)
    {
        if(pointer==IntPtr.Zero || order!=TableByteOrder.Little && order!=TableByteOrder.Big) { return -1; }
        NativeValue root=new NativeValue((byte*)pointer,0) { Mutable=mutable }; RegionGraph graph=RegionNumber(root,type,allocator);
        if(!graph.Valid) { return -1; }
        try
        {
            long data=RegionPlace(root,type,ref graph,out int alignment); if(data<0) { return -1; }
            long directory=((long)graph.Count+1)*16,size=checked(64+data+directory);
            if(measure) { return size; }
            if(bytes==IntPtr.Zero || capacity<size || (ulong)size>(ulong)nuint.MaxValue) { return -1; }
            System.Runtime.InteropServices.NativeMemory.Clear((void*)bytes,(nuint)size);
            RegionPackWriter w=new RegionPackWriter { Bytes=(byte*)bytes,Big=order==TableByteOrder.Big,Graph=graph };
            w.Put(0,0x4b4f4f434d484353ul,8); w.Put(8,tableOwnVocabulary.BuildVersion,8); w.Put(16,(uint)order,8);
            w.Put(24,(ulong)data,8); w.Put(32,(ulong)directory,8); w.Put(40,(uint)alignment,8);
            w.Base=64; RegionPack(ref w,root,type,data,true); return size;
        }
        catch(OverflowException) { return -1; }
        finally { graph.Dispose(); }
    }
`
