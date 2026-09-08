package cstable

import "strings"

func tableRetainMessageSource() string {
	s := tableRegionMessageReadSource + tableRegionMessageLoadSource[tableSourceIndex(tableRegionMessageLoadSource, "    static unsafe Verdict NativeLoadMessageRegion"):]
	s = strings.ReplaceAll(s, "Native", "Retain")
	s = strings.ReplaceAll(s, "InteropServices.RetainMemory", "InteropServices.NativeMemory")
	s = strings.ReplaceAll(s, "if(value.Base!=null) { RetainReset(value,type); }", "if(value.Base!=null) { RetainDiscard(value.Store,value.Path); RetainReset(value,type); }")
	s = strings.ReplaceAll(s, "{ d.Report.Unknown++; if (!MessageSkip(ref r, d, entry.Kind, entry.Shape)) { return false; } continue; }", `{
                d.Report.Unknown++; long start=r.At;
                if (!MessageSkip(ref r, d, entry.Kind, entry.Shape)) { return false; }
                RetainMessageCapture(value.Store,value.Path,r,d,entry,start); continue;
            }`)
	s = strings.ReplaceAll(s, "{ d.Report.Unknown++; }", "{ d.Report.Unknown++; if(value.Store!=null) { d.Report.RetainLost++; } }")
	s = strings.ReplaceAll(s, "d.Report.Unknown++; return MessageSkip", "d.Report.Unknown++; if(value.Store!=null) { d.Report.RetainLost++; } return MessageSkip")
	s = strings.ReplaceAll(s, "if(union.Base!=null) { union.SetTag(f,(ulong)tag); }", "if(union.Base!=null) { union.DiscardUnion(); union.SetTag(f,0); union.SetTag(f,(ulong)tag); }")
	s = strings.ReplaceAll(s, "int kept = (int)Math.Min(n, (ulong)f.ArrayBound);", "RetainDiscard(value.Store,value.Path,f.Ordinal);\n            int kept = (int)Math.Min(n, (ulong)f.ArrayBound);")
	s = strings.ReplaceAll(s, "TableReport report,out int count)", "TableReport report,TableRetain* retains,int retainCapacity,out int count)")
	s = strings.ReplaceAll(s, "total>roots.Length", "total>roots.Length || total>retainCapacity")
	s = strings.ReplaceAll(s, "byte* body=data+dataAt; byte* dir=directory+dirAt;", `byte* body=data+dataAt; byte* dir=directory+dirAt;
            TableRetain* retain=retains+count;
            retain->Used=0; retain->IdUsed=0; retain->Count=0; retain->Base=body; retain->Directory=dir; retain->DirectoryCount=nodes+1L;`)
	s = strings.ReplaceAll(s, "if(offset<0) { report.Unknown++; }", "if(offset<0) { report.Unknown++; report.RetainLost++; }")
	s = strings.ReplaceAll(s, "d,new RetainValue(body,offset),node", "d,new RetainValue(body,offset,retain,(uint)i+2),node")
	s = strings.ReplaceAll(s, "d,new RetainValue(body,0),type", "d,new RetainValue(body,0,retain,1),type")
	s = strings.ReplaceAll(s, "if(union.Base!=null) { union.SetTag(f,0); }", "if(union.Base!=null) { union.DiscardUnion(); union.SetTag(f,0); }")
	s = strings.ReplaceAll(s, "RetainDiscard(value.Store,value.Path,f.Ordinal)", "value.DiscardField(f)")
	return s + tableRetainMessageCaptureSource
}

const tableRetainMessageCaptureSource = `
    static bool RetainMessageName(ref BitReader r,MessageRead d,out ulong id)
    {
        id=0;
        if(!MessageReference(ref r,d,out ulong reference,out TableMessageEntry entry) || reference==0 || entry.Id==0 || RetainReserved(entry.Id)) { return false; }
        id=entry.Id; return true;
    }
    static bool RetainMessageOpaque(ref BitReader r,ref RetainIn output,byte kind,TableMessageShape shape,bool framed)
    {
        UInt128 raw;
        if(kind==12 || kind==33)
        { if(!r.Get(BitCount(shape.Max),out raw) || kind==12 && !r.Align()) { return false; } if(kind==33) { raw*=2; } }
        else if(!r.Align() || !r.Get(32,out raw)) { return false; }
        if(raw>int.MaxValue || (long)raw>(r.End-r.At)/8) { return false; }
        if(framed) { output.Var((ulong)raw); }
        for(int i=0;i<(int)raw;i++) { if(!r.Get(8,out UInt128 b)) { return false; } output.Fixed((ulong)b,1); }
        return true;
    }
    static bool RetainMessageScalar(ref BitReader r,ref RetainIn output,byte kind,TableMessageShape shape)
    {
        int width=Width(kind),bits=ValueBits(kind,shape);
        if(width==0 || bits<0 || !r.Get(bits,out UInt128 raw)) { return false; }
        if(kind==10 && shape.Packing==2)
        {
            if(raw>shape.Steps) { return false; }
            float normalized=(float)(uint)raw/shape.Steps;
            float scaled=normalized*shape.Delta;
            raw=TableFloatToBits(scaled+shape.QMin);
        }
        else if(shape.Packing==1) { raw=unchecked(raw+shape.Base); }
        else if(Signed(kind) && bits>0 && bits<64)
        { int shift=64-bits; raw=unchecked((UInt128)(Int128)((long)((ulong)raw<<shift)>>shift)); }
        output.Fixed((ulong)raw,Math.Min(width,8)); if(width==16) { output.Fixed((ulong)(raw>>64),8); }
        return true;
    }
    static bool RetainMessageFrame(ref BitReader r,MessageRead d,ref RetainIn output,byte kind,TableMessageShape shape,int depth)
    {
        long slot=output.At; output.Fixed(0,8); long began=output.At;
        if(!RetainMessageContent(ref r,d,ref output,kind,shape,depth+1) || output.At-began>uint.MaxValue) { return false; }
        if(output.Output!=null) { RetainPut32(output.Output+slot,(uint)(output.At-began)); } return true;
    }
    static bool RetainMessageContent(ref BitReader r,MessageRead d,ref RetainIn output,byte kind,TableMessageShape shape,int depth)
    {
        if(depth>64 || output.At>output.Limit) { return false; }
        switch(kind)
        {
            case 17: return false;
            case 13:
                for(;;)
                {
                    if(!MessageReference(ref r,d,out ulong reference,out TableMessageEntry entry)) { return false; }
                    if(reference==0) { output.Fixed(0,8); return true; }
                    if(entry.Id==0 || RetainReserved(entry.Id)) { return false; }
                    output.Fixed(entry.Id,8); output.Fixed(entry.Kind,1);
                    if(!RetainMessagePayload(ref r,d,ref output,entry.Kind,entry.Shape,depth)) { return false; }
                }
            case 14: case 16:
                if(shape.Elem==17 || !r.Get(BitCount(shape.Max-(kind==16?0:shape.Min)),out UInt128 raw)) { return false; }
                ulong count=(ulong)raw+(kind==16?0:shape.Min);
                if(kind==14 && shape.Elem==6 && !r.Align()) { return false; }
                output.Fixed(shape.Elem,1); output.Var(count);
                // Zero-bit announced elements can describe billions of file
                // bytes in a few input bits. Refuse the whole record from its
                // minimum resolved size before walking that count.
                int minimum=Width(shape.Elem);
                if(shape.Elem==32) { minimum=kind==16?0:1; }
                else if(shape.Elem==30 || shape.Elem==15) { minimum=8; }
                else if(shape.Elem==13) { minimum=kind==16?8:16; }
                if(kind==16) { minimum+=16; }
                if(minimum>0 && (output.At>output.Limit || count>(ulong)((output.Limit-output.At)/minimum))) { return false; }
                for(ulong i=0;i<count;i++)
                {
                    if(kind==16)
                    {
                        if(!RetainMessageName(ref r,d,out ulong key)) { return false; }
                        output.Fixed(key,8);
                        if(!RetainMessageFrame(ref r,d,ref output,shape.Elem,shape.Inner,depth)) { return false; }
                    }
                    else if(!RetainMessagePayload(ref r,d,ref output,shape.Elem,shape.Inner,depth)) { return false; }
                }
                return true;
            case 15: case 30: return RetainMessagePayload(ref r,d,ref output,kind,shape,depth);
            case 32: return true;
            case 12: case 31: case 33: return RetainMessageOpaque(ref r,ref output,kind,shape,false);
            default: return RetainMessageScalar(ref r,ref output,kind,shape);
        }
    }
    static bool RetainMessagePayload(ref BitReader r,MessageRead d,ref RetainIn output,byte kind,TableMessageShape shape,int depth)
    {
        if(output.At>output.Limit) { return false; }
        switch(kind)
        {
            case 0: case 17: return false;
            case 32: output.Var(0); return true;
            case 30: case 15:
                if(!MessageReference(ref r,d,out ulong reference,out TableMessageEntry entry)) { return false; }
                if(reference==0) { output.Fixed(0,8); return true; }
                if(entry.Id==0 || RetainReserved(entry.Id) || kind==15 && entry.Kind==0) { return false; }
                output.Fixed(entry.Id,8);
                if(kind==30) { return true; }
                output.Fixed(entry.Kind,1);
                return RetainMessageFrame(ref r,d,ref output,entry.Kind,entry.Shape,depth);
            case 13: case 14: case 16: return RetainMessageFrame(ref r,d,ref output,kind,shape,depth);
            case 12: case 31: case 33: return RetainMessageOpaque(ref r,ref output,kind,shape,true);
            default: return RetainMessageScalar(ref r,ref output,kind,shape);
        }
    }
    static void RetainMessageCapture(TableRetain* retain,RetainPath path,BitReader r,MessageRead d,TableMessageEntry entry,long start)
    {
        if(retain==null) { return; }
        long end=r.At; r.At=start; BitReader walk=r; RetainIn probe=new RetainIn { Limit=retain->Capacity-retain->Used-RetainHeader-8L*path.Depth };
        if(!RetainMessagePayload(ref walk,d,ref probe,entry.Kind,entry.Shape,0) || walk.At!=end)
        { d.Report.RetainLost++; return; }
        long need=RetainHeader+8L*path.Depth+probe.At;
        if(need>int.MaxValue || retain->Bytes==null || retain->Used>retain->Capacity-need)
        { d.Report.RetainLost++; return; }
        byte* record=retain->Bytes+retain->Used;
        RetainPut32(record,(uint)need); RetainPut32(record+4,path.Node); RetainPut32(record+8,(uint)path.Depth);
        RetainPut32(record+12,(uint)probe.At); RetainPut64(record+16,entry.Id); record[24]=entry.Kind; record[25]=0;
        for(int i=0;i<path.Depth*2;i++) { RetainPut32(record+RetainHeader+4*i,path.Steps[i]); }
        RetainIn write=new RetainIn { Output=RetainPayload(record),Limit=probe.At };
        if(!RetainMessagePayload(ref r,d,ref write,entry.Kind,entry.Shape,0)) { d.Report.RetainLost++; return; }
        retain->Used+=need; retain->Count++; d.Report.Retained++;
    }
    public static Verdict LoadMessageRetainRegion(TableTypeInfo type,IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,bool separate,Span<IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,Span<TableRetain> stores,TableReport report,out int count)
    {
        fixed(TableRetain* retain=stores)
        { return RetainLoadMessageRegion(type,region,capacity,attribution,attributionCapacity,separate,roots,bytes,vocabulary,report,retain,stores.Length,out count); }
    }
`
