using System;
using System.Diagnostics;
using System.Runtime.InteropServices;
using RT=Tblrt1;

static partial class Program
{
    static byte[] RetainFrame(int id,byte kind,byte[] content) { return Join(Var((ulong)id),new byte[]{kind},Var((ulong)content.Length),content); }
    static byte[] RetainScalar(int id,uint value) { return Join(Var((ulong)id),new byte[]{4},U32(value)); }
    static unsafe void RetainProbe(byte[] wire,int captured,int remaining,int lost,int saveLost,Action<RT.Node> check=null,int idsCapacity=1024,bool audit=true)
    {
        long need=RT.Schema.NodeLoadMeasure(wire);
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
        byte* bytes=(byte*)NativeMemory.Alloc(16384);
        RT.TableRetain.Id* ids=(RT.TableRetain.Id*)NativeMemory.Alloc((nuint)(1024*sizeof(RT.TableRetain.Id)));
        try
        {
            RT.TableRetain store=new RT.TableRetain { Bytes=bytes,Capacity=16384,Ids=ids,IdCapacity=idsCapacity };
            var report=new RT.TableReport();
            Check(RT.Schema.NodeLoadRetain((IntPtr)region,need,wire,ref store,report)!=null && !report.Malformed && !report.Refused,"retention contract load");
            Check(report.Retained==captured && report.RetainLost==lost && store.Count==remaining,"retention record lifetime "+report.Retained+","+report.RetainLost+","+store.Count);
            check?.Invoke(RT.Schema.NodeLoadBuilder((IntPtr)region));
            byte[] saved=new byte[RT.Schema.NodeMeasureRetain((IntPtr)region,ref store)];
            var save=new RT.TableReport();
            Check(RT.Schema.NodeSaveRetain((IntPtr)region,ref store,saved,save)==saved.Length && save.RetainLost==saveLost,"retention contract save");
            if(idsCapacity==0)
            {
                byte[] plain=new byte[RT.Schema.NodeMeasure((IntPtr)region)]; RT.Schema.NodeSave((IntPtr)region,plain);
                Check(saved.AsSpan().SequenceEqual(plain),"unplaceable-only bodies elide with their defaults");
            }
            // Capture and resolved emission add no GC allocations to the native
            // walk. Numbering owns native scratch just as the ordinary save does.
            long allocations=audit?MeasureCalls(()=>
            {
                RT.Schema.NodeLoadRetain((IntPtr)region,need,wire,ref store,report);
                RT.Schema.NodeMeasureRetain((IntPtr)region,ref store);
                RT.Schema.NodeSaveRetain((IntPtr)region,ref store,saved,save);
            }):0;
            Check(allocations==0,"whole retention family GC allocation audit: "+allocations);
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.Free(bytes); NativeMemory.Free(ids); }
    }
    static unsafe void RetainDamagedArm()
    {
        // Minimized from the differential corpus: an arm captures a field,
        // then fails. Its disappearing path is a save loss, not a replacement.
        byte[] wire=Convert.FromHexString("01010803000000020f030d0a04030306010105110200060c1602070a040e03060102051103000707040e030601030000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000");
        long need=Streamdemo.Schema.FeedLoadMeasure(wire);
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
        byte* bytes=stackalloc byte[4096]; Streamdemo.TableRetain.Id* ids=stackalloc Streamdemo.TableRetain.Id[128];
        var retain=new Streamdemo.TableRetain { Bytes=bytes,Capacity=4096,Ids=ids,IdCapacity=128 };
        var report=new Streamdemo.TableReport();
        try
        {
            Check(Streamdemo.Schema.FeedLoadRetain((IntPtr)region,need,wire,ref retain,report)!=null && report.Malformed,"damaged retained union arm");
            Check(report.Retained==1 && report.RetainLost==0,"damaged arm retains its load event");
            byte[] saved=new byte[Streamdemo.Schema.FeedMeasureRetain((IntPtr)region,ref retain)];
            Check(Streamdemo.Schema.FeedSaveRetain((IntPtr)region,ref retain,saved,report)==saved.Length && report.RetainLost==1,"damaged arm's missing path counts at save");
        }
        finally { NativeMemory.AlignedFree(region); }
    }
    static unsafe void NativeNumberingGrowth()
    {
        var value=new Streamdemo.Feed(); value.Frame.Type=Streamdemo.FrameType.Chunk;
        var tail=value.Frame.Chunk;
        for(int i=0;i<600;i++)
        { var node=new Streamdemo.Chunk { DataLength=1 }; node.Data[0]=(byte)i; tail.Next=node; tail=node; }
        value.Pair[0]=value.Frame.Chunk.Next; value.Pair[1]=tail;
        byte[] wire=new byte[Streamdemo.Schema.FeedMeasure(value)]; Streamdemo.Schema.FeedSave(value,wire);
        long need=Streamdemo.Schema.FeedLoadMeasure(wire);
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
        try
        {
            var report=new Streamdemo.TableReport();
            Streamdemo.FeedRow* root=Streamdemo.Schema.FeedLoad((IntPtr)region,need,wire,report);
            Check(root!=null && !report.Malformed,"large native graph loads");
            byte[] saved=new byte[Streamdemo.Schema.FeedMeasure((IntPtr)region)];
            Check(Streamdemo.Schema.FeedSave((IntPtr)region,saved)==wire.Length && wire.AsSpan().SequenceEqual(saved),"native numbering grows and preserves sharing");
            Streamdemo.ChunkRow* last=&root->Frame.Chunk;
            while(last->Next!=0) { last=(Streamdemo.ChunkRow*)((byte*)&last->Next+last->Next); }
            last->Next=(byte*)last-(byte*)&last->Next;
            saved.AsSpan().Fill(0xa5);
            Check(Streamdemo.Schema.FeedMeasure((IntPtr)region)<0 && Streamdemo.Schema.FeedSave((IntPtr)region,saved)<0 && saved[0]==0xa5,"native cycle refuses before output");
        }
        finally { NativeMemory.AlignedFree(region); }
    }
    static void TestRetentionContracts()
    {
        byte[] future=Join(RetainScalar(2,7),new byte[]{0});
        byte[] known=Join(RetainScalar(3,2),new byte[]{0});
        RetainProbe(Fixture(Join(RetainFrame(1,13,future),RetainFrame(1,13,known),new byte[]{0}),"inner","future","hits"),1,0,0,0,n=>Check(n.Inner.Hits==2,"last table occurrence wins"));
        byte[] list=Join(new byte[]{13,1},Var((ulong)future.Length),future);
        RetainProbe(Fixture(Join(RetainFrame(1,14,list),RetainFrame(1,14,Array.Empty<byte>()),new byte[]{0}),"list","future"),1,1,0,0,n=>Check(n.ListCount==1,"inert list occurrence preserves its predecessor"));
        RetainProbe(Fixture(Join(RetainFrame(1,14,list),RetainFrame(1,14,new byte[]{13,0}),new byte[]{0}),"list","future"),1,0,0,0,n=>Check(n.ListCount==0,"empty list replaces its predecessor"));
        RetainProbe(Fixture(Join(RetainFrame(1,13,RetainScalar(2,7)),new byte[]{0}),"future","deeper"),0,0,1,0);
        // A by-value body containing only an unknown field must survive elision.
        RetainProbe(Fixture(Join(RetainFrame(1,13,future),new byte[]{0}),"inner","future"),1,1,0,0);
        RetainProbe(Fixture(Join(RetainFrame(1,13,future),new byte[]{0}),"inner","future"),1,1,0,1,idsCapacity:0);
        byte[] defaultSlot=Join(new byte[]{13,1,3},Var((ulong)future.Length),future);
        RetainProbe(Fixture(Join(RetainFrame(1,16,defaultSlot),new byte[]{0}),"banks","future","Low"),1,1,0,1,idsCapacity:0);
        // Disjoint enum-keyed occurrences overwrite only the slot they carry.
        byte[] low=Join(RetainScalar(2,41),RetainScalar(3,4),new byte[]{0});
        byte[] high=Join(RetainScalar(2,42),RetainScalar(3,5),new byte[]{0});
        byte[] bankLow=Join(new byte[]{13,1,4},Var((ulong)low.Length),low);
        byte[] bankHigh=Join(new byte[]{13,1,5},Var((ulong)high.Length),high);
        RetainProbe(Fixture(Join(RetainFrame(1,16,bankLow),RetainFrame(1,16,bankHigh),new byte[]{0}),"banks","future","hits","Low","High"),2,2,0,0,n=>Check(n.Banks[1].Hits==4 && n.Banks[2].Hits==5,"disjoint keyed bodies survive"));
        foreach(int depth in new int[]{8,20,40,64,65})
        {
            byte[] content=Join(RetainScalar(2,1),new byte[]{0});
            for(int i=1;i<depth;i++) { content=Join(RetainFrame(2,13,content),new byte[]{0}); }
            byte[] wire=Fixture(Join(RetainFrame(1,13,content),new byte[]{0}),"future","deeper");
            var time=Stopwatch.StartNew();
            RetainProbe(wire,depth<=64?1:0,depth<=64?1:0,depth<=64?0:1,0,audit:false);
            Check(time.Elapsed.TotalSeconds<10,"retention resolving walk bounded at depth "+depth);
        }
        RetainDamagedArm();
        NativeNumberingGrowth();
    }
}
