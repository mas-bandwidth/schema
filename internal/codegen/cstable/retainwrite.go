package cstable

import "strings"

func tableRetainWriteSource() string {
	s := tableRegionWriteSource[:tableSourceIndex(tableRegionWriteSource, "    public static unsafe long SaveRegion")]
	// Numbering is the base writer's graph, independently re-derived from the
	// region. Retained paths resolve the old directory only when placing tails.
	s += tableRegionGraphSource[tableSourceIndex(tableRegionGraphSource, "    static long RegionNodesSize"):]
	s = strings.NewReplacer("RegionIds", "RetainIds", "NativeValue", "RetainValue", "RegionDefaultElement", "RetainDefaultElement", "RegionEmpty", "RetainEmpty", "RegionRides", "RetainRides", "RegionCount", "RetainCount", "RegionCollect", "RetainCollect", "RegionPayloadSize", "RetainPayloadSize", "RegionElementSize", "RetainElementSize", "RegionArraySize", "RetainArraySize", "RegionBodySize", "RetainBodySize", "RegionWrite", "RetainWrite", "RegionNodesSize", "RetainNodesSize").Replace(s)
	// Validation is performed by the plain collector before the retained walk.
	begin := tableSourceIndex(s, "    static bool RetainCollectElement")
	end := tableSourceIndex(s, "    static long RetainPayloadSize")
	s = s[:begin] + s[end:]
	s = strings.ReplaceAll(s, "        return true;\n    }\n    static bool RetainRides", "        return !RetainHas(value.Store,value.Path);\n    }\n    static bool RetainRides")
	s = strings.ReplaceAll(s, "            long n = RetainPayloadSize(union.Arm(f), arm.Field, ref ids);\n            return VarSize(ids.Reference(f.VariantId(tag)))", "            ulong reference=ids.Reference(f.VariantId(tag));\n            long n = RetainPayloadSize(union.Arm(f), arm.Field, ref ids);\n            return VarSize(reference)")
	s = strings.ReplaceAll(s, "                long elem = RetainElementSize", "                ulong reference=ids.Reference(f.KeyId((ulong)i+1));\n                long elem = RetainElementSize")
	s = strings.ReplaceAll(s, "n += VarSize(ids.Reference(f.KeyId((ulong)i + 1)))", "n += VarSize(reference)")
	s = strings.ReplaceAll(s, "        return n;\n    }\n    static void RetainWriteElement", "        return n+RetainTailMeasure(ref ids,value.Path);\n    }\n    static void RetainWriteElement")
	s = strings.ReplaceAll(s, "        if (root) { RetainWriteNodes", "        RetainTailSave(ref w,ref ids,value.Path);\n        if (root) { RetainWriteNodes")
	s = strings.ReplaceAll(s, "node.Value,ids.RootType.PointerType(node.TypeId)", "new RetainValue(node.Value.Base,node.Value.At,ids.Store,0),ids.RootType.PointerType(node.TypeId)")
	s = strings.ReplaceAll(s, "            long n=node.BlobKind", "            ulong reference=ids.Reference(node.TypeId);\n            long n=node.BlobKind")
	s = strings.ReplaceAll(s, "size+=VarSize(ids.Reference(node.TypeId))", "size+=VarSize(reference)")
	s = strings.ReplaceAll(s, "                ulong reference=ids.Reference(f.KeyId((ulong)i+1));", "                int mark=ids.Count; ulong reference=ids.Reference(f.KeyId((ulong)i+1));")
	s = strings.ReplaceAll(s, "                n += VarSize(reference) + VarSize((ulong)elem) + elem;", "                if(f.Kind==13 && elem==1) { ids.Truncate(mark); continue; }\n                n += VarSize(reference) + VarSize((ulong)elem) + elem;")
	s = strings.ReplaceAll(s, `            n += VarSize(ids.Reference(f.Id)) + 1;
            if (f.IsArray) { long a = RetainArraySize(value, f, ref ids); n += VarSize((ulong)a) + a; }
            else if (f.Kind == 12 || f.Kind == 33) { long s = RetainPayloadSize(value, f, ref ids); n += VarSize((ulong)s) + s; }
            else { n += RetainElementSize(value, f, 0, ref ids, true); }`,
		`            int mark=ids.Count; ulong reference=ids.Reference(f.Id);
            long payload=RetainPayloadSize(value,f,ref ids);
            if(!f.Optional && (f.Kind==13 && !f.IsArray && payload==1 || f.KeyId!=null && payload==2))
            { ids.Truncate(mark); continue; }
            n+=VarSize(reference)+1+payload;
            if(f.IsArray || f.Kind==12 || f.Kind==33 || f.Kind==13) { n+=VarSize((ulong)payload); }`)
	s = strings.ReplaceAll(s, "            w.Var(ids.Reference(f.Id)); w.Byte(Kind(f));", `            if(!f.Optional && (f.Kind==13 && !f.IsArray && RetainPayloadSize(value,f,ref ids)==1 || f.KeyId!=null && RetainArraySize(value,f,ref ids)==2)) { continue; }
            w.Var(ids.Reference(f.Id)); w.Byte(Kind(f));`)
	// On the emit pass the trailer is already settled. A slot whose only
	// retained content could not be placed is its default again.
	start := tableSourceIndex(s, "    static void RetainWritePayload")
	payloadEnd := start + tableSourceIndex(s[start:], "    static void RetainWriteBody")
	payload := s[start:payloadEnd]
	payload = strings.ReplaceAll(payload, "!RetainDefaultElement(value, f, i)", "RetainSlotRides(value,f,i,ref ids)")
	payload = strings.ReplaceAll(payload, "RetainDefaultElement(value, f, i)", "!RetainSlotRides(value,f,i,ref ids)")
	s = s[:start] + payload + s[payloadEnd:]
	s += `    static bool RetainSlotRides(RetainValue value,TableFieldInfo f,int i,ref RetainIds ids)
    { return !RetainDefaultElement(value,f,i) && (f.Kind!=13 || RetainBodySize(value.Child(f,i),f.Table,ref ids)>1); }
`
	return s + tableRetainSaveSource
}

const tableRetainSaveSource = `
    public static long SaveRetainRegion(IntPtr pointer,TableTypeInfo type,Span<byte> buffer,Span<ulong> vocabulary,Span<int> slots,ref TableRetain store,TableReport report,bool measure)
    {
        if(pointer==IntPtr.Zero || !measure && report==null) { return -1; }
        NativeValue native=new NativeValue((byte*)pointer,0);
        RegionGraph graph=RegionNumber(native,type); if(!graph.Valid) { return -1; }
        try
        {
        RegionIds validate=new RegionIds(vocabulary) { Graph=graph,RootType=type };
        if(!RegionCollect(native,type,ref validate) || !RegionCollectNodes(ref validate)) { return -1; }
        fixed(TableRetain* retain=&store)
        {
            RetainValue value=new RetainValue((byte*)pointer,0,retain,1);
            RetainIds ids=new RetainIds(vocabulary,slots,retain,graph,type);
            long n=1+RetainBodySize(value,type,ref ids);
            if(graph.Count!=0)
            {
                ulong reference=ids.Reference(ulong.MaxValue);
                long nodes=RetainNodesSize(ref ids);
                n+=VarSize(reference)+1+VarSize((ulong)nodes)+nodes;
            }
            n+=8L*ids.Count+8;
            if(ids.Overflow) { return -1; }
            if(measure) { return n; }
            if(n>buffer.Length) { return -1; }
            long at=0;
            for(int i=0;i<retain->Count;i++) { retain->Bytes[at+25]=0; at+=Retain32(retain->Bytes+at); }
            Writer w=new Writer(buffer); w.Byte(1); RetainWriteBody(ref w,value,type,ref ids,true); ids.Write(ref w);
            at=0;
            for(int i=0;i<retain->Count;i++) { if(retain->Bytes[at+25]==0) { report.RetainLost++; } at+=Retain32(retain->Bytes+at); }
            return w.Offset;
        }
        }
        finally { graph.Dispose(); }
    }
    public static IntPtr LoadRetainRegion(TableTypeInfo type,IntPtr region,long capacity,ReadOnlySpan<byte> bytes,ref TableRetain store,TableReport report)
    {
        long total=LoadMeasureParts(type,bytes,out long data,out long attribution,out string reason);
        if(total>=0 && capacity<total) { return IntPtr.Zero; }
        fixed(TableRetain* retain=&store)
        { return RetainLoadRegion(type,region,data,(IntPtr)((byte*)region+data),attribution,bytes,report,total,data,reason,retain); }
    }
    public static IntPtr LoadRetainRegion(TableTypeInfo type,IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,ReadOnlySpan<byte> bytes,ref TableRetain store,TableReport report)
    {
        long total=LoadMeasureParts(type,bytes,out long data,out _,out string reason);
        fixed(TableRetain* retain=&store)
        { return RetainLoadRegion(type,region,capacity,attribution,attributionCapacity,bytes,report,total,data,reason,retain); }
    }
`
