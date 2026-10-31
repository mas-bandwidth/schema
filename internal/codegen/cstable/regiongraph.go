package cstable

const tableRegionGraphSource = `
    unsafe struct RegionNode
    {
        public NativeValue Value;
        public ulong TypeId;
        public long NativeOffset, NativeExtent;
        public byte BlobKind;
        public bool Open;
        public ReadOnlySpan<byte> Blob
        {
            get
            {
                // A later arena slab may have a lower address. Compute its
                // signed displacement outside the checked length conversion.
                byte* node = Value.Base + Value.At;
                int length = checked((int)NativeWord(node, 4));
                return new ReadOnlySpan<byte>(node + 8, length);
            }
        }
    }
    // Numbering scratch is proportional to the reachable nodes. It owns
    // native metadata only; neither values nor retained payloads enter it.
    unsafe struct RegionGraph
    {
        public RegionNode* Entries;
        public int* Hash;
        public int Count, Capacity;
        public bool Valid;
        // A blob is owned by ONE slot (docs/SPEC-TABLES.md §2.5): a writer
        // refuses a blob a second slot names (blob_shared), while Pack and
        // Cook place the graph as they find it.
        public bool RefuseSharedBlobs;
        public TableAllocator Allocator;
        public ReadOnlySpan<RegionNode> Nodes { get { return new ReadOnlySpan<RegionNode>(Entries, Count); } }
        static uint Mix(long at)
        { unchecked { ulong x = (ulong)at; x ^= x >> 33; x *= 0xff51afd7ed558ccdUL; x ^= x >> 33; return (uint)x; } }
        public int Find(long at)
        {
            if (at == long.MinValue) { return 0; }
            if (at == 0) { return 1; }
            if (Capacity == 0) { return 0; }
            int mask = Capacity * 2 - 1, slot = (int)(Mix(at) & (uint)mask);
            for (; ; )
            { int index = Hash[slot]; if (index == 0 || Entries[index - 2].Value.At == at) { return index; } slot = (slot + 1) & mask; }
        }
        public ulong Index(long at) { return (ulong)Find(at); }
        void Place(int index)
        {
            int mask = Capacity * 2 - 1, slot = (int)(Mix(Entries[index - 2].Value.At) & (uint)mask);
            while (Hash[slot] != 0) { slot = (slot + 1) & mask; }
            Hash[slot] = index;
        }
        public bool Add(RegionNode node)
        {
            if (Count == Capacity)
            {
                if (Capacity > int.MaxValue / 8) { return false; }
                int capacity = Capacity == 0 ? 256 : Capacity * 4;
                RegionNode* entries = (RegionNode*)Allocator.Get((long)capacity * sizeof(RegionNode));
                int* hash = (int*)Allocator.Get((long)capacity * 2 * sizeof(int));
                if (entries == null || hash == null)
                { Allocator.Release(entries); Allocator.Release(hash); return false; }
                System.Runtime.InteropServices.NativeMemory.Clear(hash, (nuint)((long)capacity * 2 * sizeof(int)));
                Nodes.CopyTo(new Span<RegionNode>(entries, Count));
                Allocator.Release(Entries); Allocator.Release(Hash);
                Entries = entries; Hash = hash; Capacity = capacity;
                for (int i = 0; i < Count; i++) { Place(i + 2); }
            }
            Entries[Count++] = node; Place(Count + 1); return true;
        }
        public void Dispose()
        { Allocator.Release(Entries); Allocator.Release(Hash); this = default; }
    }
    ref struct RegionIds
    {
        public Span<ulong> Values;
        public int Count;
        public RegionGraph Graph;
        public TableTypeInfo RootType;
        public RegionIds(Span<ulong> values) { Values = values; Count = 0; Graph = default; RootType = null; }
        public ulong Reference(ulong id)
        { for (int i = 0; i < Count; i++) { if (Values[i] == id) { return (ulong)i + 1; } } return 0; }
        public bool Add(ulong id)
        { if (Reference(id) != 0) { return true; } if (Count == Values.Length) { return false; } Values[Count++] = id; return true; }
    }
    static RegionGraph RegionNumber(NativeValue root, TableTypeInfo type, TableAllocator allocator = default, bool refuseSharedBlobs = false)
    {
        RegionGraph graph = new RegionGraph { Valid = true, Allocator = allocator, RefuseSharedBlobs = refuseSharedBlobs };
        if (!RegionVisit(root, type, ref graph)) { graph.Dispose(); return default; }
        return graph;
    }
    // A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4), for the region,
    // retaining and builder loads: KAHN'S ALGORITHM over the node directory the
    // load already holds. The vertices are the root (position 0) and every
    // directory entry this load placed as a table record; a byte buffer reaches
    // nothing and takes no part. The edges are every non-null table pointer slot
    // a node's by-value closure STORES, under a false guard too. Every node is
    // entered once and every slot visited twice, so the check is linear in nodes
    // and slots, nothing recurses over the pointer graph, and a shared node is
    // legal. A cycle anywhere, reached from the root or not, refuses the load.
    // The scratch is three ints per directory entry from the load's allocator:
    // the in-degree, the link of the zero-in-degree stack and the order by offset
    // that a pointer's target is found through. A scratch the allocator cannot
    // give is a load this check cannot carry, and it refuses.
    unsafe struct RegionCycle
    {
        public byte* Data, Directory;
        public bool Mutable, Releasing;
        public int* Indegree, Link, Order;
        public int Ordered, Head;
        public TableTypeInfo Root;
    }
    static unsafe long RegionCycleOffset(ref RegionCycle c, int p) { return unchecked((long)NativeWord(c.Directory + p * 16L, 8)); }
    static unsafe TableTypeInfo RegionCycleType(ref RegionCycle c, int p)
    {
        if (p == 0) { return c.Root; }
        if (unchecked((long)NativeWord(c.Directory + p * 16L, 8)) == -1) { return null; }
        ulong id = NativeWord(c.Directory + p * 16L + 8, 8);
        if ((c.Root.BytesEdge && id == BytesTypeId) || (c.Root.StringEdge && id == StringTypeId)) { return null; }
        return c.Root.PointerType(id);
    }
    static unsafe int RegionCycleFind(ref RegionCycle c, long at)
    {
        int lo = 0, hi = c.Ordered;
        while (lo < hi)
        {
            int mid = lo + (hi - lo) / 2; long o = RegionCycleOffset(ref c, c.Order[mid]);
            if (o == at) { return c.Order[mid]; }
            if (o < at) { lo = mid + 1; } else { hi = mid; }
        }
        return -1;
    }
    static unsafe bool RegionCyclic(byte* data, byte* directory, int nodes, TableTypeInfo type, TableAllocator allocator, bool mutable)
    {
        int count = nodes + 1;
        int* scratch = (int*)allocator.Get(count * 12L);
        if (scratch == null) { return true; }
        try
        {
            RegionCycle c = new RegionCycle { Data = data, Directory = directory, Mutable = mutable, Root = type, Indegree = scratch, Link = scratch + count, Order = scratch + 2L * count };
            new Span<int>(scratch, count * 2).Clear();
            for (int p = 0; p < count; p++) { if (RegionCycleType(ref c, p) != null) { c.Order[c.Ordered++] = p; } }
            // HEAPSORT the vertices by offset: no recursion, no allocation
            for (int start = c.Ordered / 2 - 1, end = c.Ordered; ;)
            {
                int root;
                if (start >= 0) { root = start--; }
                else { if (--end <= 0) { break; } int t = c.Order[0]; c.Order[0] = c.Order[end]; c.Order[end] = t; root = 0; }
                for (; ; )
                {
                    int child = 2 * root + 1; if (child >= end) { break; }
                    if (child + 1 < end && RegionCycleOffset(ref c, c.Order[child + 1]) > RegionCycleOffset(ref c, c.Order[child])) { child++; }
                    if (RegionCycleOffset(ref c, c.Order[root]) >= RegionCycleOffset(ref c, c.Order[child])) { break; }
                    int x = c.Order[root]; c.Order[root] = c.Order[child]; c.Order[child] = x; root = child;
                }
            }
            for (int i = 1; i < c.Ordered; i++) { if (RegionCycleOffset(ref c, c.Order[i - 1]) == RegionCycleOffset(ref c, c.Order[i])) { return true; } } // two nodes at one offset
            for (int i = 0; i < c.Ordered; i++) { int p = c.Order[i]; RegionCycleSlots(ref c, new NativeValue(data, RegionCycleOffset(ref c, p)) { Mutable = mutable }, RegionCycleType(ref c, p)); }
            for (int i = 0; i < c.Ordered; i++) { int p = c.Order[i]; if (c.Indegree[p] == 0) { c.Link[p] = c.Head; c.Head = p + 1; } }
            c.Releasing = true;
            int processed = 0;
            while (c.Head != 0)
            {
                int p = c.Head - 1; c.Head = c.Link[p]; processed++;
                RegionCycleSlots(ref c, new NativeValue(data, RegionCycleOffset(ref c, p)) { Mutable = mutable }, RegionCycleType(ref c, p));
            }
            return processed < c.Ordered;
        }
        finally { allocator.Release(scratch); }
    }
    static unsafe void RegionCycleSlots(ref RegionCycle c, NativeValue value, TableTypeInfo type)
    {
        foreach (TableFieldInfo f in type.Fields) { RegionCycleField(ref c, value, f); }
    }
    static unsafe void RegionCycleField(ref RegionCycle c, NativeValue value, TableFieldInfo f)
    {
        if (f == null) { return; }
        int n = f.IsArray ? Math.Max(0, RegionCount(value, f)) : 1;
        for (int i = 0; i < n; i++)
        {
            if (f.Kind == 13) { RegionCycleSlots(ref c, value.Child(f, i), f.Table); }
            else if (f.Kind == 15)
            {
                NativeValue union = value.Child(f, i); ulong tag = union.Tag(f);
                if (tag != 0 && tag <= (ulong)f.EnumMax) { RegionCycleField(ref c, union.Arm(f), f.Arms.Arms[(int)tag].Field); }
            }
            else if (f.Kind == 17 && f.BlobKind == 0)
            {
                long at = value.Pointer(f, i); if (at == long.MinValue) { continue; }
                int t = RegionCycleFind(ref c, at); if (t < 0) { continue; }
                if (!c.Releasing) { c.Indegree[t]++; }
                else if (--c.Indegree[t] == 0) { c.Link[t] = c.Head; c.Head = t + 1; }
            }
        }
    }
    static bool RegionVisit(NativeValue value, TableTypeInfo type, ref RegionGraph graph)
    {
        foreach (TableFieldInfo f in type.Fields)
        { if (RegionRides(value, f) && !RegionVisitField(value, f, ref graph)) { return false; } }
        return true;
    }
    static int RegionKeyOrder(NativeValue a, NativeValue b, TableFieldInfo key)
    {
        if (key.Kind == 12) { return a.Buffer(key).Slice(0, a.Count(key)).SequenceCompareTo(b.Buffer(key).Slice(0, b.Count(key))); }
        ulong x = a.Raw(key, 0), y = b.Raw(key, 0);
        return key.Kind >= 2 && key.Kind <= 5 ? unchecked((long)x).CompareTo(unchecked((long)y)) : x.CompareTo(y);
    }
    static unsafe bool RegionVisitField(NativeValue value, TableFieldInfo f, ref RegionGraph graph)
    {
        if (f == null) { return true; }
        int n = f.IsArray ? RegionCount(value, f) : 1;
        if (f.Counted && (RegionCount(value, f) < 0 || RegionCount(value, f) > f.ArrayBound)) { return false; }
        if (f.Map) { for (int i = 1; i < n; i++) { if (RegionKeyOrder(value.Child(f, i - 1), value.Child(f, i), f.Table.Fields[0]) >= 0) { return false; } } }
        for (int i = 0; i < n; i++)
        {
            if (f.Kind == 13) { if (!RegionVisit(value.Child(f, i), f.Table, ref graph)) { return false; } }
            else if (f.Kind == 15)
            {
                NativeValue union = value.Child(f, i); ulong tag = union.Tag(f);
                if (tag > (ulong)f.EnumMax || !RegionVisitField(union.Arm(f), f.Arms.Arms[(int)tag].Field, ref graph)) { return false; }
            }
            else if (f.Kind == 17)
            {
                long at = value.Pointer(f, i); if (at == long.MinValue) { continue; }
                int index = graph.Find(at);
                if (index == 1 || index >= 2 && graph.Entries[index - 2].Open) { return false; }
                if (index >= 2)
                {
                    if (graph.Entries[index - 2].TypeId != f.PointerTypeId) { return false; }
                    if (f.BlobKind != 0 && graph.RefuseSharedBlobs) { return false; }
                    continue;
                }
                var child = new NativeValue(value.Base, at) { Mutable = value.Mutable };
                if (f.BlobKind != 0 && NativeWord(child.Base + at, 4) > int.MaxValue) { return false; }
                int entry = graph.Count;
                if (!graph.Add(new RegionNode { Value = child, TypeId = f.PointerTypeId, BlobKind = f.BlobKind, Open = true })) { return false; }
                if (f.BlobKind == 0 && !RegionVisit(child, f.Table, ref graph)) { return false; }
                graph.Entries[entry].Open = false;
            }
        }
        return true;
    }
    static bool RegionCollectNodes(ref RegionIds ids)
    {
        if (!ids.Graph.Valid || ids.Graph.Count == 0) { return true; }
        if (!ids.Add(ulong.MaxValue)) { return false; }
        foreach (RegionNode node in ids.Graph.Nodes)
        { if (!ids.Add(node.TypeId) || node.BlobKind == 0 && !RegionCollect(node.Value, ids.RootType.PointerType(node.TypeId), ref ids)) { return false; } }
        return true;
    }
    static long RegionNodesSize(ref RegionIds ids)
    {
        if (!ids.Graph.Valid || ids.Graph.Count == 0) { return 0; }
        long size = VarSize((ulong)ids.Graph.Count);
        foreach (RegionNode node in ids.Graph.Nodes)
        {
            long n = node.BlobKind == 0 ? RegionBodySize(node.Value, ids.RootType.PointerType(node.TypeId), ref ids) : node.Blob.Length;
            size += VarSize(ids.Reference(node.TypeId)) + VarSize((ulong)n) + n;
        }
        return size;
    }
    static void RegionWriteNodes(ref Writer w, ref RegionIds ids)
    {
        long n = RegionNodesSize(ref ids); if (n == 0) { return; }
        w.Var(ids.Reference(ulong.MaxValue)); w.Byte(12); w.Var((ulong)n); w.Var((ulong)ids.Graph.Count);
        foreach (RegionNode node in ids.Graph.Nodes)
        {
            long size = node.BlobKind == 0 ? RegionBodySize(node.Value, ids.RootType.PointerType(node.TypeId), ref ids) : node.Blob.Length;
            w.Var(ids.Reference(node.TypeId)); w.Var((ulong)size);
            if (node.BlobKind == 0) { RegionWriteBody(ref w, node.Value, ids.RootType.PointerType(node.TypeId), ref ids); } else { w.Raw(node.Blob); }
        }
    }
`
