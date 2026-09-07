package cstable

const tableRegionGraphSource = `
    unsafe struct RegionNode
    {
        public NativeValue Value;
        public TableTypeInfo Type;
        public ulong TypeId;
        public byte BlobKind;
        public ReadOnlySpan<byte> Blob { get { return new ReadOnlySpan<byte>(Value.Base+Value.At+8,checked((int)NativeWord(Value.Base+Value.At,8))); } }
    }
    sealed class RegionGraph
    {
        public System.Collections.Generic.Dictionary<long,int> Indices=new System.Collections.Generic.Dictionary<long,int>();
        public System.Collections.Generic.HashSet<long> Open=new System.Collections.Generic.HashSet<long>();
        public System.Collections.Generic.List<RegionNode> Nodes=new System.Collections.Generic.List<RegionNode>();
        public ulong Index(long at) { return at<0?0:(ulong)Indices[at]; }
    }
    ref struct RegionIds
    {
        public Span<ulong> Values;
        public int Count;
        public RegionGraph Graph;
        public RegionIds(Span<ulong> values) { Values=values; Count=0; Graph=null; }
        public ulong Reference(ulong id)
        { for(int i=0;i<Count;i++) { if(Values[i]==id) { return (ulong)i+1; } } return 0; }
        public bool Add(ulong id)
        { if(Reference(id)!=0) { return true; } if(Count==Values.Length) { return false; } Values[Count++]=id; return true; }
    }
    static RegionGraph RegionNumber(NativeValue root,TableTypeInfo type)
    {
        RegionGraph graph=new RegionGraph(); graph.Indices.Add(0,1); graph.Open.Add(0);
        if(!RegionVisit(root,type,graph)) { return null; } graph.Open.Clear(); return graph;
    }
    static bool RegionVisit(NativeValue value,TableTypeInfo type,RegionGraph graph)
    {
        foreach(TableFieldInfo f in type.Fields)
        { if(RegionRides(value,f) && !RegionVisitField(value,f,graph)) { return false; } }
        return true;
    }
    static int RegionKeyOrder(NativeValue a,NativeValue b,TableFieldInfo key)
    {
        if(key.Kind==12) { return a.Buffer(key).Slice(0,a.Count(key)).SequenceCompareTo(b.Buffer(key).Slice(0,b.Count(key))); }
        ulong x=a.Raw(key,0),y=b.Raw(key,0);
        return key.Kind>=2 && key.Kind<=5?unchecked((long)x).CompareTo(unchecked((long)y)):x.CompareTo(y);
    }
    static unsafe bool RegionVisitField(NativeValue value,TableFieldInfo f,RegionGraph graph)
    {
        if(f==null) { return true; }
        int n=f.IsArray?RegionCount(value,f):1;
        if(f.Counted && (RegionCount(value,f)<0 || RegionCount(value,f)>f.ArrayBound)) { return false; }
        if(f.Map) { for(int i=1;i<n;i++) { if(RegionKeyOrder(value.Child(f,i-1),value.Child(f,i),f.Table.Fields[0])>=0) { return false; } } }
        for(int i=0;i<n;i++)
        {
            if(f.Kind==13) { if(!RegionVisit(value.Child(f,i),f.Table,graph)) { return false; } }
            else if(f.Kind==15)
            {
                NativeValue union=value.Child(f,i); ulong tag=union.Tag(f);
                if(tag>(ulong)f.EnumMax || !RegionVisitField(union.Arm(f),f.Arms.Arms[(int)tag].Field,graph)) { return false; }
            }
            else if(f.Kind==17)
            {
                long at=value.Pointer(f,i); if(at<0) { continue; }
                if(graph.Open.Contains(at)) { return false; }
                if(graph.Indices.TryGetValue(at,out int index))
                { if(index>=2 && graph.Nodes[index-2].TypeId!=f.PointerTypeId) { return false; } continue; }
                var child=new NativeValue(value.Base,at);
                if(f.BlobKind!=0 && NativeWord(child.Base+at,8)>int.MaxValue) { return false; }
                graph.Indices.Add(at,graph.Nodes.Count+2);
                graph.Nodes.Add(new RegionNode { Value=child,Type=f.Table,TypeId=f.PointerTypeId,BlobKind=f.BlobKind });
                if(f.BlobKind==0) { graph.Open.Add(at); if(!RegionVisit(child,f.Table,graph)) { return false; } graph.Open.Remove(at); }
            }
        }
        return true;
    }
    static bool RegionCollectNodes(ref RegionIds ids)
    {
        if(ids.Graph==null || ids.Graph.Nodes.Count==0) { return true; }
        if(!ids.Add(ulong.MaxValue)) { return false; }
        foreach(RegionNode node in ids.Graph.Nodes)
        { if(!ids.Add(node.TypeId) || node.BlobKind==0 && !RegionCollect(node.Value,node.Type,ref ids)) { return false; } }
        return true;
    }
    static long RegionNodesSize(ref RegionIds ids)
    {
        if(ids.Graph==null || ids.Graph.Nodes.Count==0) { return 0; }
        long size=VarSize((ulong)ids.Graph.Nodes.Count);
        foreach(RegionNode node in ids.Graph.Nodes)
        {
            long n=node.BlobKind==0?RegionBodySize(node.Value,node.Type,ref ids):node.Blob.Length;
            size+=VarSize(ids.Reference(node.TypeId))+VarSize((ulong)n)+n;
        }
        return size;
    }
    static void RegionWriteNodes(ref Writer w,ref RegionIds ids)
    {
        long n=RegionNodesSize(ref ids); if(n==0) { return; }
        w.Var(ids.Reference(ulong.MaxValue)); w.Byte(12); w.Var((ulong)n); w.Var((ulong)ids.Graph.Nodes.Count);
        foreach(RegionNode node in ids.Graph.Nodes)
        {
            long size=node.BlobKind==0?RegionBodySize(node.Value,node.Type,ref ids):node.Blob.Length;
            w.Var(ids.Reference(node.TypeId)); w.Var((ulong)size);
            if(node.BlobKind==0) { RegionWriteBody(ref w,node.Value,node.Type,ref ids); } else { w.Raw(node.Blob); }
        }
    }
`
