package cstable

const tableRegionMessageLoadSource = `
    public static unsafe Verdict LoadMessageRegion(TableTypeInfo type,IntPtr region,long capacity,Span<IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,TableReport report,out int count)
    { return NativeLoadMessageRegion(type,region,capacity,IntPtr.Zero,0,false,roots,bytes,vocabulary,report,out count); }
    public static unsafe Verdict LoadMessageRegion(TableTypeInfo type,IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,Span<IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,TableReport report,out int count)
    { return NativeLoadMessageRegion(type,region,capacity,attribution,attributionCapacity,true,roots,bytes,vocabulary,report,out count); }
    static unsafe Verdict NativeLoadMessageRegion(TableTypeInfo type,IntPtr region,long capacity,IntPtr attribution,long attributionCapacity,bool separate,Span<IntPtr> roots,ReadOnlySpan<byte> bytes,TableVocabulary vocabulary,TableReport report,out int count)
    {
        count=0; report.Refused=false; report.Reason=null;
        int total=MessageCount(bytes,vocabulary,report);
        if(report.Refused) { return Finish(report,Verdict.Refused); }
        if(total==0) { return Finish(report,Verdict.Damaged); }
        if(total>roots.Length) { count=total; return Refuse(report,"batch_too_large"); }
        long need=MessageLoadMeasureParts(type,bytes,vocabulary,out long dataBytes,out long attributionBytes);
        if(need<0) { Damage(report); return Finish(report,Verdict.Damaged); }
        if(region==IntPtr.Zero || capacity<(separate?dataBytes:need) || (ulong)region%(ulong)type.RegionAlign!=0) { return Finish(report,Verdict.Refused); }
        byte* data=(byte*)region; byte* directory=separate?(byte*)attribution:data+dataBytes;
        if(separate)
        {
            if(attribution==IntPtr.Zero || attributionCapacity<attributionBytes || ((ulong)attribution&7)!=0) { return Finish(report,Verdict.Refused); }
            ulong da=(ulong)data,aa=(ulong)directory;
            if(da<=aa?aa-da<(ulong)dataBytes:da-aa<(ulong)attributionBytes) { return Finish(report,Verdict.Refused); }
        }
        System.Runtime.InteropServices.NativeMemory.Clear(data,checked((nuint)dataBytes));
        System.Runtime.InteropServices.NativeMemory.Clear(directory,checked((nuint)attributionBytes));
        long dataAt=0,dirAt=0; BitReader cursor=new BitReader(bytes); cursor.At=16;
        for(;count<total;count++)
        {
            MessageRead d=new MessageRead { Vocabulary=vocabulary,Report=report };
            BitReader measured=cursor;
            if(!MessageFrame(ref measured,ref d,type,out long recordsAt,out long rootAt,out int nodes,out long rootExtent,out long size,out _))
            { Damage(report); return Finish(report,Verdict.Damaged); }
            BitReader records=cursor; records.At=recordsAt; BitReader root=cursor; root.At=rootAt;
            byte* body=data+dataAt; byte* dir=directory+dirAt;
            roots[count]=(IntPtr)body; NativeReset(new NativeValue(body,0),type);
            NativePut(dir,0,8); NativePut(dir+8,type.Id,8);
            long at=Align(Align(type.StorageSize,type.RegionAlign)+rootExtent,type.RegionAlign);
            BitReader scan=records;
            for(int i=0;i<nodes;i++)
            {
                MessageName(ref scan,d,out _,out TableMessageEntry named);
                long offset=-1;
                if(named.Id==BytesTypeId || named.Id==StringTypeId)
                {
                    scan.Get(32,out UInt128 length); scan.Align();
                    if(named.Id==BytesTypeId && type.BytesEdge || named.Id==StringTypeId && type.StringEdge)
                    {
                        ReadOnlySpan<byte> blob=bytes.Slice((int)(scan.At/8),(int)length);
                        if(named.Id==StringTypeId && !TextValid(blob)) { Damage(report); return Finish(report,Verdict.Damaged); }
                        offset=at; at+=Align(8+(long)length+(named.Id==StringTypeId?1:0),type.RegionAlign);
                        NativePut(body+offset,length,8); blob.CopyTo(new Span<byte>(body+offset+8,(int)length));
                    }
                    scan.Skip((long)length*8);
                }
                else
                {
                    TableTypeInfo node=type.PointerType(named.Id); long extent=0;
                    MessageExtentBody(ref scan,d,node,ref extent);
                    if(node!=null) { offset=at; at+=Align(Align(node.StorageSize,type.RegionAlign)+extent,type.RegionAlign); }
                }
                if(offset<0) { report.Unknown++; }
                NativePut(dir+(i+1L)*16,unchecked((ulong)offset),8); NativePut(dir+(i+1L)*16+8,named.Id,8);
            }
            NativeState state=new NativeState { Directory=dir,Nodes=nodes,NodesGood=true,Root=type };
            scan=records;
            for(int i=0;i<nodes;i++)
            {
                MessageName(ref scan,d,out _,out TableMessageEntry named);
                if(named.Id==BytesTypeId || named.Id==StringTypeId)
                { scan.Get(32,out UInt128 length); scan.Align(); scan.Skip((long)length*8); continue; }
                TableTypeInfo node=type.PointerType(named.Id); long extent=0; BitReader payload=scan;
                MessageExtentBody(ref scan,d,node,ref extent); payload.End=scan.At;
                if(node==null) { continue; }
                long offset=(long)NativeWord(dir+(i+1L)*16,8);
                state.Next=offset+Align(node.StorageSize,type.RegionAlign); state.End=state.Next+extent;
                if(!NativeMessageReadBody(ref state,ref payload,d,new NativeValue(body,offset),node))
                { Damage(report); return Finish(report,Verdict.Damaged); }
            }
            state.Next=Align(type.StorageSize,type.RegionAlign); state.End=state.Next+rootExtent;
            if(!NativeMessageReadBody(ref state,ref root,d,new NativeValue(body,0),type))
            { Damage(report); return Finish(report,Verdict.Damaged); }
            cursor=root; dataAt+=size; dirAt+=(nodes+1L)*16;
        }
        if(!cursor.Align() || cursor.At!=cursor.End) { Damage(report); return Finish(report,Verdict.Damaged); }
        return Finish(report,Verdict.Ok);
    }
`
