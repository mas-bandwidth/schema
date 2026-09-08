package cstable

const tableRegionGraphSource = `
    unsafe struct RegionNode
    {
        public NativeValue Value;
        public ulong TypeId;
        public byte BlobKind;
        public bool Open;
        public ReadOnlySpan<byte> Blob { get { return new ReadOnlySpan<byte>(Value.Base+Value.At+8,checked((int)NativeWord(Value.Base+Value.At,8))); } }
    }
    // Numbering scratch is proportional to the reachable nodes. It owns
    // native metadata only; neither values nor retained payloads enter it.
    unsafe struct RegionGraph
    {
        public RegionNode* Entries;
        public int* Hash;
        public int Count,Capacity;
        public bool Valid;
        public ReadOnlySpan<RegionNode> Nodes { get { return new ReadOnlySpan<RegionNode>(Entries,Count); } }
        static uint Mix(long at)
        { unchecked { ulong x=(ulong)at; x^=x>>33; x*=0xff51afd7ed558ccdUL; x^=x>>33; return (uint)x; } }
        public int Find(long at)
        {
            if(at<0) { return 0; } if(at==0) { return 1; }
            if(Capacity==0) { return 0; }
            int mask=Capacity*2-1,slot=(int)(Mix(at)&(uint)mask);
            for(;;)
            { int index=Hash[slot]; if(index==0 || Entries[index-2].Value.At==at) { return index; } slot=(slot+1)&mask; }
        }
        public ulong Index(long at) { return (ulong)Find(at); }
        void Place(int index)
        {
            int mask=Capacity*2-1,slot=(int)(Mix(Entries[index-2].Value.At)&(uint)mask);
            while(Hash[slot]!=0) { slot=(slot+1)&mask; } Hash[slot]=index;
        }
        public bool Add(RegionNode node)
        {
            if(Count==Capacity)
            {
                if(Capacity>int.MaxValue/8) { return false; }
                int capacity=Capacity==0?256:Capacity*4;
                RegionNode* entries=(RegionNode*)System.Runtime.InteropServices.NativeMemory.Alloc((nuint)capacity,(nuint)sizeof(RegionNode));
                int* hash=(int*)System.Runtime.InteropServices.NativeMemory.AllocZeroed((nuint)capacity*2,(nuint)sizeof(int));
                if(entries==null || hash==null)
                { System.Runtime.InteropServices.NativeMemory.Free(entries); System.Runtime.InteropServices.NativeMemory.Free(hash); return false; }
                Nodes.CopyTo(new Span<RegionNode>(entries,Count));
                System.Runtime.InteropServices.NativeMemory.Free(Entries); System.Runtime.InteropServices.NativeMemory.Free(Hash);
                Entries=entries; Hash=hash; Capacity=capacity;
                for(int i=0;i<Count;i++) { Place(i+2); }
            }
            Entries[Count++]=node; Place(Count+1); return true;
        }
        public void Dispose()
        { System.Runtime.InteropServices.NativeMemory.Free(Entries); System.Runtime.InteropServices.NativeMemory.Free(Hash); this=default; }
    }
    ref struct RegionIds
    {
        public Span<ulong> Values;
        public int Count;
        public RegionGraph Graph;
        public TableTypeInfo RootType;
        public RegionIds(Span<ulong> values) { Values=values; Count=0; Graph=default; RootType=null; }
        public ulong Reference(ulong id)
        { for(int i=0;i<Count;i++) { if(Values[i]==id) { return (ulong)i+1; } } return 0; }
        public bool Add(ulong id)
        { if(Reference(id)!=0) { return true; } if(Count==Values.Length) { return false; } Values[Count++]=id; return true; }
    }
    static RegionGraph RegionNumber(NativeValue root,TableTypeInfo type)
    {
        RegionGraph graph=new RegionGraph { Valid=true };
        if(!RegionVisit(root,type,ref graph)) { graph.Dispose(); return default; } return graph;
    }
    static bool RegionVisit(NativeValue value,TableTypeInfo type,ref RegionGraph graph)
    {
        foreach(TableFieldInfo f in type.Fields)
        { if(RegionRides(value,f) && !RegionVisitField(value,f,ref graph)) { return false; } }
        return true;
    }
    static int RegionKeyOrder(NativeValue a,NativeValue b,TableFieldInfo key)
    {
        if(key.Kind==12) { return a.Buffer(key).Slice(0,a.Count(key)).SequenceCompareTo(b.Buffer(key).Slice(0,b.Count(key))); }
        ulong x=a.Raw(key,0),y=b.Raw(key,0);
        return key.Kind>=2 && key.Kind<=5?unchecked((long)x).CompareTo(unchecked((long)y)):x.CompareTo(y);
    }
    static unsafe bool RegionVisitField(NativeValue value,TableFieldInfo f,ref RegionGraph graph)
    {
        if(f==null) { return true; }
        int n=f.IsArray?RegionCount(value,f):1;
        if(f.Counted && (RegionCount(value,f)<0 || RegionCount(value,f)>f.ArrayBound)) { return false; }
        if(f.Map) { for(int i=1;i<n;i++) { if(RegionKeyOrder(value.Child(f,i-1),value.Child(f,i),f.Table.Fields[0])>=0) { return false; } } }
        for(int i=0;i<n;i++)
        {
            if(f.Kind==13) { if(!RegionVisit(value.Child(f,i),f.Table,ref graph)) { return false; } }
            else if(f.Kind==15)
            {
                NativeValue union=value.Child(f,i); ulong tag=union.Tag(f);
                if(tag>(ulong)f.EnumMax || !RegionVisitField(union.Arm(f),f.Arms.Arms[(int)tag].Field,ref graph)) { return false; }
            }
            else if(f.Kind==17)
            {
                long at=value.Pointer(f,i); if(at<0) { continue; }
                int index=graph.Find(at);
                if(index==1 || index>=2 && graph.Entries[index-2].Open) { return false; }
                if(index>=2)
                { if(graph.Entries[index-2].TypeId!=f.PointerTypeId) { return false; } continue; }
                var child=new NativeValue(value.Base,at);
                if(f.BlobKind!=0 && NativeWord(child.Base+at,8)>int.MaxValue) { return false; }
                int entry=graph.Count;
                if(!graph.Add(new RegionNode { Value=child,TypeId=f.PointerTypeId,BlobKind=f.BlobKind,Open=true })) { return false; }
                if(f.BlobKind==0 && !RegionVisit(child,f.Table,ref graph)) { return false; }
                graph.Entries[entry].Open=false;
            }
        }
        return true;
    }
    static bool RegionCollectNodes(ref RegionIds ids)
    {
        if(!ids.Graph.Valid || ids.Graph.Count==0) { return true; }
        if(!ids.Add(ulong.MaxValue)) { return false; }
        foreach(RegionNode node in ids.Graph.Nodes)
        { if(!ids.Add(node.TypeId) || node.BlobKind==0 && !RegionCollect(node.Value,ids.RootType.PointerType(node.TypeId),ref ids)) { return false; } }
        return true;
    }
    static long RegionNodesSize(ref RegionIds ids)
    {
        if(!ids.Graph.Valid || ids.Graph.Count==0) { return 0; }
        long size=VarSize((ulong)ids.Graph.Count);
        foreach(RegionNode node in ids.Graph.Nodes)
        {
            long n=node.BlobKind==0?RegionBodySize(node.Value,ids.RootType.PointerType(node.TypeId),ref ids):node.Blob.Length;
            size+=VarSize(ids.Reference(node.TypeId))+VarSize((ulong)n)+n;
        }
        return size;
    }
    static void RegionWriteNodes(ref Writer w,ref RegionIds ids)
    {
        long n=RegionNodesSize(ref ids); if(n==0) { return; }
        w.Var(ids.Reference(ulong.MaxValue)); w.Byte(12); w.Var((ulong)n); w.Var((ulong)ids.Graph.Count);
        foreach(RegionNode node in ids.Graph.Nodes)
        {
            long size=node.BlobKind==0?RegionBodySize(node.Value,ids.RootType.PointerType(node.TypeId),ref ids):node.Blob.Length;
            w.Var(ids.Reference(node.TypeId)); w.Var((ulong)size);
            if(node.BlobKind==0) { RegionWriteBody(ref w,node.Value,ids.RootType.PointerType(node.TypeId),ref ids); } else { w.Raw(node.Blob); }
        }
    }
`
