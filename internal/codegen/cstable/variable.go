package cstable

// The managed graph is a numbering, not recursive load scratch. A record is
// allocated only after the entire node table has passed its framing scan.
const tableVariableWireSource = `
    internal sealed class Node
    {
        public object Value;
        public TableTypeInfo Type;
        public ulong TypeId;
        public byte BlobKind;
        public int Offset, Length;
        public long NativeOffset;
    }
    internal sealed class Graph
    {
        public System.Collections.Generic.Dictionary<object, int> Uses = new System.Collections.Generic.Dictionary<object, int>(System.Collections.Generic.ReferenceEqualityComparer.Instance);
        public System.Collections.Generic.Dictionary<object, ulong> Labels = new System.Collections.Generic.Dictionary<object, ulong>(System.Collections.Generic.ReferenceEqualityComparer.Instance);
        public object Root;
        public TableTypeInfo RootType;
        public bool Good = true;
        public int PayloadOffset, PayloadLength;
        public System.Collections.Generic.List<Node> Nodes = new System.Collections.Generic.List<Node>();
        public System.Collections.Generic.Dictionary<object, ulong> Indices = new System.Collections.Generic.Dictionary<object, ulong>(System.Collections.Generic.ReferenceEqualityComparer.Instance);
        public System.Collections.Generic.HashSet<object> Open = new System.Collections.Generic.HashSet<object>(System.Collections.Generic.ReferenceEqualityComparer.Instance);
        public ulong Index(object o) { return o == null ? 0 : Indices[o]; }
        public object Resolve(ulong index, TableFieldInfo f, TableReport report)
        {
            if (!Good || index == 0) { return null; }
            if (index == 1)
            {
                if (f.PointerTypeId == RootType.Id && f.BlobKind == 0) { return Root; }
                report.KindMismatch++; return null;
            }
            if (index - 2 >= (ulong)Nodes.Count) { Damage(report); return null; }
            Node node = Nodes[(int)(index - 2)];
            if (node.Value == null) { return null; }
            if (node.TypeId != f.PointerTypeId) { report.KindMismatch++; return null; }
            return node.Value;
        }
        public bool Visit(object value, TableTypeInfo type)
        {
            foreach (TableFieldInfo f in type.Fields)
            { if (Rides(value, f) && !VisitField(value, f)) { return false; } }
            return true;
        }
        bool VisitField(object value, TableFieldInfo f)
        {
            if (f == null) { return true; }
            int n = f.IsArray ? Count(value, f) : 1;
            if (f.Counted && (Count(value, f) < 0 || Count(value, f) > f.ArrayBound)) { return false; }
            if (f.Map && !MapOrdered(value, f)) { return false; }
            for (int i = 0; i < n; i++)
            {
                if (f.Kind == 13) { if (!Visit(f.GetChild(value, i), f.Table)) { return false; } }
                else if (f.Kind == 15)
                {
                    object union = f.GetChild(value, i); ulong tag = f.Arms.GetTag(union);
                    if (tag > (ulong)f.EnumMax) { return false; }
                    if (tag != 0 && !VisitField(union, f.Arms.Arms[(int)tag].Field)) { return false; }
                }
                else if (f.Kind == 17)
                {
                    object child = f.GetChild(value, i);
                    if (child == null) { continue; }
                    Uses.TryGetValue(child, out int uses); Uses[child] = uses + 1;
                    if (Open.Contains(child)) { return false; }
                    if (Indices.TryGetValue(child, out ulong old))
                    { if (old >= 2 && Nodes[(int)old - 2].TypeId != f.PointerTypeId) { return false; } continue; }
                    Indices.Add(child, (ulong)Nodes.Count + 2);
                    Nodes.Add(new Node { Value = child, Type = f.Table, TypeId = f.PointerTypeId, BlobKind = f.BlobKind });
                    if (f.BlobKind == 0)
                    {
                        Open.Add(child);
                        if (!Visit(child, f.Table)) { return false; }
                        Open.Remove(child);
                    }
                }
            }
            return true;
        }
    }
    internal static Graph Number(object value, TableTypeInfo type)
    {
        Graph graph = new Graph { Root = value, RootType = type };
        graph.Indices.Add(value, 1); graph.Open.Add(value);
        if (!graph.Visit(value, type)) { return null; }
        graph.Open.Clear(); return graph;
    }
    static bool CollectNodes(ref Ids ids)
    {
        if (ids.Graph == null || ids.Graph.Nodes.Count == 0) { return true; }
        if (!ids.Add(ulong.MaxValue)) { return false; }
        foreach (Node node in ids.Graph.Nodes)
        {
            if (!ids.Add(node.TypeId)) { return false; }
            if (node.BlobKind == 0 && !Collect(node.Value, node.Type, ref ids)) { return false; }
        }
        return true;
    }
    static long NodesSize(ref Ids ids)
    {
        if (ids.Graph == null || ids.Graph.Nodes.Count == 0) { return 0; }
        long size = VarSize((ulong)ids.Graph.Nodes.Count);
        foreach (Node node in ids.Graph.Nodes)
        {
            long n = node.BlobKind == 0 ? BodySize(node.Value, node.Type, ref ids) : ((TableBlob)node.Value).Data.Length;
            size += VarSize(ids.Reference(node.TypeId)) + VarSize((ulong)n) + n;
        }
        return size;
    }
    static void WriteNodes(ref Writer w, ref Ids ids)
    {
        long size = NodesSize(ref ids);
        if (size == 0) { return; }
        w.Var(ids.Reference(ulong.MaxValue)); w.Byte(12); w.Var((ulong)size);
        w.Var((ulong)ids.Graph.Nodes.Count);
        foreach (Node node in ids.Graph.Nodes)
        {
            w.Var(ids.Reference(node.TypeId));
            if (node.BlobKind != 0)
            { byte[] data = ((TableBlob)node.Value).Data; w.Var((ulong)data.Length); w.Raw(data); }
            else { w.Var((ulong)BodySize(node.Value, node.Type, ref ids)); WriteBody(ref w, node.Value, node.Type, ref ids); }
        }
    }
    static Graph ScanGraph(Reader root, object value, TableTypeInfo type, TableReport report)
    {
        Graph graph = new Graph { Root = value, RootType = type };
        Reader payload = default;
        bool present = false;
        Reader scan = root;
        while (scan.Ref(out ulong reference, out ulong id) && reference != 0 && scan.Has(1))
        {
            byte kind = scan.Byte();
            if (id == ulong.MaxValue)
            {
                present = true;
                if (kind != 12 || !scan.Slice(out payload)) { graph.Good = false; break; }
                graph.PayloadOffset = scan.Offset - payload.Buffer.Length; graph.PayloadLength = payload.Buffer.Length;
            }
            else if (!scan.Skip(kind)) { break; }
        }
        if (present && graph.Good)
        {
            scan = payload;
            if (!scan.Var(out ulong declared)) { graph.Good = false; }
            else
            {
                while (scan.Offset < scan.Buffer.Length)
                {
                    if (!scan.Ref(out ulong reference, out ulong id) || reference == 0 || !scan.Slice(out Reader body))
                    { graph.Good = false; break; }
                    graph.Nodes.Add(new Node { TypeId = id, Offset = scan.Offset - body.Buffer.Length, Length = body.Buffer.Length });
                }
                if (declared != (ulong)graph.Nodes.Count) { graph.Good = false; }
            }
        }
        if (!graph.Good) { graph.Nodes.Clear(); Damage(report); return graph; }
        return graph;
    }
    const ulong BytesTypeId = 0x2f2ec0474f1c4fe4ul, StringTypeId = 0x704be0d8faaffc58ul;
    static Graph ReadGraph(Reader root, object value, TableTypeInfo type, TableReport report)
    {
        Graph graph = ScanGraph(root, value, type, report);
        Reader payload = new Reader(root.Buffer.Slice(graph.PayloadOffset, graph.PayloadLength), root.Vocabulary);
        TableTypeInfo[] placeable = type.PointerTypes();
        foreach (Node node in graph.Nodes)
        {
            if (type.BytesEdge && node.TypeId == 0x2f2ec0474f1c4fe4ul) { node.BlobKind = 14; }
            if (type.StringEdge && node.TypeId == 0x704be0d8faaffc58ul) { node.BlobKind = 12; }
            if (node.BlobKind != 0)
            {
                ReadOnlySpan<byte> data = payload.Buffer.Slice(node.Offset, node.Length);
                if (node.BlobKind == 12 && !TextValid(data)) { Damage(report); continue; }
                node.Value = new TableBlob(data.ToArray()); continue;
            }
            foreach (TableTypeInfo candidate in placeable)
            { if (candidate.Id == node.TypeId) { node.Type = candidate; break; } }
            if (node.Type == null) { report.Unknown++; continue; }
            node.Value = node.Type.Create();
        }
        foreach (Node node in graph.Nodes)
        {
            if (node.Type == null || node.Value == null) { continue; }
            Reader body = new Reader(payload.Buffer.Slice(node.Offset, node.Length), root.Vocabulary) { Graph = graph };
            ReadBody(ref body, node.Value, node.Type, report, true);
        }
        return graph;
    }
    internal static int CompareKey(object a, object b, TableFieldInfo key)
    {
        if (key.Kind == 12) { return key.GetBuffer(a).AsSpan(0, key.GetCount(a)).SequenceCompareTo(key.GetBuffer(b).AsSpan(0, key.GetCount(b))); }
        if (key.GetWide != null)
        { return key.WideSigned ? unchecked((Int128)key.GetWide(a, 0)).CompareTo(unchecked((Int128)key.GetWide(b, 0))) : key.GetWide(a, 0).CompareTo(key.GetWide(b, 0)); }
        ulong x = key.GetRaw(a, 0), y = key.GetRaw(b, 0);
        return key.Kind >= 2 && key.Kind <= 5 ? unchecked((long)x).CompareTo(unchecked((long)y)) : x.CompareTo(y);
    }
    internal static bool MapOrdered(object value, TableFieldInfo f)
    {
        for (int i = 1; i < Count(value, f); i++)
        { if (CompareKey(f.GetChild(value, i - 1), f.GetChild(value, i), f.Table.Fields[0]) >= 0) { return false; } }
        return true;
    }
`
