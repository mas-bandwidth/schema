package cstable

const tableBuilderLoadSource = `
    static bool BuilderElement(TableWorker worker,NativeValue value,TableFieldInfo field,int index)
    {
        TableCookList* slot=(TableCookList*)(value.Base+value.At+field.NativeOffset);
        if(!TableListStorage.Reserve(worker,slot,field,index+1)) { return false; }
        ResetNativeElement((IntPtr)TableListStorage.At((byte*)slot,index,field.NativeElementSize),field);
        return true;
    }
    public static bool LoadBuilder(IntPtr pointer,TableTypeInfo type,TableWorker worker,ReadOnlySpan<byte> bytes,TableReport report)
    {
        if(report==null) { throw new ArgumentNullException(nameof(report)); }
        report.Refused=false; report.Reason=null;
        if(pointer==IntPtr.Zero || worker.Arena.Closed) { return false; }
        if(bytes.Length==0) { Damage(report); Finish(report,Verdict.Damaged); return false; }
        if(bytes[0]!=1)
        { report.Refused=true; report.Reason=bytes[0]==2?"message_form_as_file":"newer_form"; Finish(report,Verdict.Refused); return false; }
        if(bytes.Length<9) { Damage(report); Finish(report,Verdict.Damaged); return false; }
        ulong idCount=Read64(bytes,bytes.Length-8);
        if(idCount>(ulong)(bytes.Length-9)/8) { Damage(report); Finish(report,Verdict.Damaged); return false; }
        int end=bytes.Length-8-(int)idCount*8;
        ReadOnlySpan<byte> ids=bytes.Slice(end,(int)idCount*8);
        for(int i=0;i<ids.Length;i+=8)
        { for(int j=0;j<i;j+=8) { if(Read64(ids,i)==Read64(ids,j)) { Damage(report); Finish(report,Verdict.Damaged); return false; } } }
        Reader root=new Reader(bytes.Slice(1,end-1),ids);
        if(EndsEarly(root)) { Damage(report); Finish(report,Verdict.Damaged); return false; }
        bool good=NodeFrames(root,out Reader records,out int count);
        if(!good) { count=0; Damage(report); }
        byte* directory=(byte*)worker.Arena.Allocator.Get(((long)count+1)*16);
        if(directory==null) { return false; }
        try
        {
            byte* data=(byte*)pointer;
            NativePut(directory,0,8); NativePut(directory+8,type.Id,8);
            Reader scan=records;
            for(int i=0;i<count;i++)
            {
                scan.Ref(out _,out ulong id); scan.Slice(out Reader body);
                long offset=-1; TableTypeInfo node=type.PointerType(id);
                bool blob=(type.BytesEdge && id==BytesTypeId)||(type.StringEdge && id==StringTypeId);
                if(blob)
                {
                    if(id==StringTypeId && !TextValid(body.Buffer)) { Damage(report); }
                    else
                    {
                        byte* p=worker.Raw(8L+body.Buffer.Length+(id==StringTypeId?1:0)); if(p==null) { return false; }
                        offset=p-data; NativePut(p,(uint)body.Buffer.Length,4);
                        body.Buffer.CopyTo(new Span<byte>(p+8,body.Buffer.Length));
                    }
                }
                else if(node!=null)
                { byte* p=worker.Raw(node.StorageSize); if(p==null) { return false; } offset=p-data; }
                else { report.Unknown++; }
                NativePut(directory+(i+1L)*16,unchecked((ulong)offset),8); NativePut(directory+(i+1L)*16+8,id,8);
            }
            NativeState state=new NativeState { Directory=directory,Nodes=count,NodesGood=good,Root=type,Worker=worker };
            scan=records;
            for(int i=0;i<count && !state.Refused;i++)
            {
                scan.Ref(out _,out ulong id); scan.Slice(out Reader body);
                long offset=(long)NativeWord(directory+(i+1L)*16,8); TableTypeInfo node=type.PointerType(id);
                if(offset!=-1 && node!=null) { NativeReadBody(ref state,ref body,new NativeValue(data,offset) { Mutable=true },node,report,true); }
            }
            bool complete=!state.Refused && NativeReadBody(ref state,ref root,new NativeValue(data,0) { Mutable=true },type,report,false);
            if(state.Refused) { return false; }
            Finish(report,complete?Verdict.Ok:Verdict.BodyStopped); return complete;
        }
        finally { worker.Arena.Allocator.Release(directory); }
    }
`
