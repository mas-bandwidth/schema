package cstable

const tableRegionMessageWriteSource = `
    static bool RegionMessageRides(NativeValue value, TableFieldInfo f)
    {
        if (!value.Guard(f)) { return false; }
        if (!f.Optional && f.Counted) { return value.Count(f) != 0; }
        return RegionRides(value, f);
    }
    static void RegionMessageBody(ref BitWriter w, NativeValue value, TableTypeInfo type, RegionGraph graph, int indexBits)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (!RegionMessageRides(value, f)) { continue; }
            w.Put((uint)f.MessageSlot, tableOwnVocabulary.RefBits);
            RegionMessageField(ref w, value, f, tableOwnVocabulary.Entries[f.MessageSlot - 1].Shape, graph, indexBits);
        }
        w.Put(0, tableOwnVocabulary.RefBits);
    }
    static void RegionMessageField(ref BitWriter w, NativeValue value, TableFieldInfo f, TableMessageShape shape, RegionGraph graph, int indexBits)
    {
        if (f.IsArray)
        {
            int count = RegionCount(value, f), present = count;
            if (f.KeyId != null) { present = 0; for (int i = 0; i < count; i++) { if (!RegionDefaultElement(value, f, i)) { present++; } } }
            w.Put(unchecked((ulong)present - shape.Min), BitCount(shape.Max - shape.Min));
            if (f.KeyId == null && f.Kind == 6) { w.Align(); }
            for (int i = 0; i < count; i++)
            {
                if (f.KeyId != null) { if (RegionDefaultElement(value, f, i)) { continue; } w.Put(NameSlot(f.KeyId((ulong)i + 1)), tableOwnVocabulary.RefBits); }
                RegionMessageElement(ref w, value, f, i, shape.Inner, graph, indexBits);
            }
        }
        else if (f.Kind == 12 || f.Kind == 33)
        {
            int count = RegionCount(value, f); w.Put((uint)count, BitCount(shape.Max));
            if (f.Kind == 12) { w.Align(); w.Raw(value.Buffer(f).Slice(0, count)); }
            else { for (int i = 0; i < count; i++) { w.Put(value.Chars(f)[i], 16); } }
        }
        else { RegionMessageElement(ref w, value, f, 0, shape, graph, indexBits); }
    }
    static void RegionMessageElement(ref BitWriter w, NativeValue value, TableFieldInfo f, int i, TableMessageShape shape, RegionGraph graph, int indexBits)
    {
        if (f.Kind == 13) { RegionMessageBody(ref w, value.Child(f,i), f.Table, graph, indexBits); return; }
        if (f.Kind == 17) { w.Put(graph.Index(value.Pointer(f,i)), indexBits); return; }
        if (f.Kind == 30) { ulong raw = value.Raw(f,i); w.Put(raw == 0 ? 0 : NameSlot(f.VariantId(raw)), tableOwnVocabulary.RefBits); return; }
        if (f.Kind == 15)
        {
            NativeValue union = value.Child(f,i); ulong tag = union.Tag(f);
            if (tag == 0) { w.Put(0, tableOwnVocabulary.RefBits); return; }
            var arm = f.Arms.Arms[(int)tag]; w.Put((uint)arm.MessageSlot, tableOwnVocabulary.RefBits);
            if (arm.Field != null) { RegionMessageField(ref w, union.Arm(f), arm.Field, tableOwnVocabulary.Entries[arm.MessageSlot - 1].Shape, graph, indexBits); }
            return;
        }
        UInt128 bits = f.GetWide != null ? value.Wide(f,i) : f.GetBuffer != null ? value.Buffer(f)[i] : value.Raw(f,i);
        if (f.Kind == 10 && shape.Packing == 2)
        {
            float normalized = (TableBitsToFloat((uint)bits) - shape.QMin) / shape.Delta;
            if (!(normalized >= 0)) { normalized = 0; } else if (!(normalized <= 1)) { normalized = 1; }
            float scaled = normalized * shape.Steps;
            bits = Math.Min((uint)Math.Floor((double)(scaled + 0.5f)), shape.Steps);
        }
        else if (shape.Packing == 1) { bits = unchecked(bits - shape.Base); }
        w.Put(bits, ValueBits(f.Kind, shape));
    }
    public static unsafe long SaveMessageRegion(ReadOnlySpan<IntPtr> values, TableTypeInfo type, Span<byte> bytes, bool measure,TableAllocator allocator=default)
    {
        if (values.Length < 1 || values.Length > 256) { return -1; }
        // Validate the graph and every enum/tag before touching the output.
        Span<RegionGraph> graphs=stackalloc RegionGraph[values.Length]; graphs.Clear();
        try
        {
        Span<ulong> storage = stackalloc ulong[tableOwnVocabulary.Count];
        for (int i = 0; i < values.Length; i++)
        {
            if (values[i] == IntPtr.Zero) { return -1; }
            if (type.Variable) { graphs[i] = RegionNumber(new NativeValue((byte*)values[i],0), type,allocator); if (!graphs[i].Valid) { return -1; } }
            RegionIds ids = new RegionIds(storage) { Graph = graphs[i],RootType=type };
            if (!RegionCollect(new NativeValue((byte*)values[i],0), type, ref ids) || !RegionCollectNodes(ref ids)) { return -1; }
        }
        BitWriter probe = new BitWriter(Span<byte>.Empty, true);
        RegionMessageBatch(ref probe, values, type, graphs);
        long size = (probe.At + 7) / 8;
        if (measure) { return size; }
        if (size > bytes.Length) { return -1; }
        bytes.Slice(0, (int)size).Clear(); BitWriter writer = new BitWriter(bytes, false);
        RegionMessageBatch(ref writer, values, type, graphs); return size;
        }
        finally { for(int i=0;i<graphs.Length;i++) { graphs[i].Dispose(); } }
    }
    static unsafe void RegionMessageBatch(ref BitWriter w, ReadOnlySpan<IntPtr> values, TableTypeInfo type, scoped ReadOnlySpan<RegionGraph> graphs)
    {
        w.Put(2, 8); w.Put((uint)(values.Length - 1), 8);
        for (int i = 0; i < values.Length; i++)
        {
            RegionGraph graph=graphs[i]; int indices=BitCount((ulong)(graph.Count+1));
            if (graph.Valid && graph.Count > 0)
            {
                w.Put(NameSlot(ulong.MaxValue), tableOwnVocabulary.RefBits); w.Put((uint)graph.Count, 32);
                foreach (RegionNode node in graph.Nodes)
                {
                    w.Put(NameSlot(node.TypeId), tableOwnVocabulary.RefBits);
                    if (node.BlobKind != 0) { ReadOnlySpan<byte> data = node.Blob; w.Put((uint)data.Length, 32); w.Align(); w.Raw(data); }
                    else { RegionMessageBody(ref w, node.Value, type.PointerType(node.TypeId), graph, indices); }
                }
            }
            RegionMessageBody(ref w, new NativeValue((byte*)values[i],0), type, graph, indices);
        }
        w.Align();
    }
`
