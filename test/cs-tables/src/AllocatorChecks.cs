using System;
using System.Runtime.InteropServices;
using G = Graphdemo;

static partial class Program
{
    struct AllocationCounts { public int Calls, Frees, Live, FailAt; }
    static unsafe IntPtr CountAllocate(IntPtr context,long size)
    {
        AllocationCounts* c=(AllocationCounts*)context;
        int call=System.Threading.Interlocked.Increment(ref c->Calls);
        if(c->FailAt==call) { return IntPtr.Zero; }
        void* p=NativeMemory.AllocZeroed((nuint)size);
        if(p!=null) { System.Threading.Interlocked.Increment(ref c->Live); }
        return (IntPtr)p;
    }
    static unsafe void CountFree(IntPtr context,IntPtr pointer)
    {
        AllocationCounts* c=(AllocationCounts*)context;
        Check(pointer!=IntPtr.Zero,"allocator never frees null");
        System.Threading.Interlocked.Increment(ref c->Frees); System.Threading.Interlocked.Decrement(ref c->Live); NativeMemory.Free((void*)pointer);
    }
    static unsafe IntPtr DirtyAllocate(IntPtr context,long size)
    {
        IntPtr p=CountAllocate(context,size);
        if(p!=IntPtr.Zero) { new Span<byte>((void*)p,checked((int)size)).Fill(0xa5); }
        return p;
    }
    static unsafe void TestNumberingGrowthFailures()
    {
        using var builder=new G.SceneBuilder();
        G.ListNodeRow* tail=null;
        for(int i=0;i<600;i++)
        {
            var node=builder.Alloc<G.ListNodeRow>(); node->Value=i;
            if(tail==null) { G.TableArena.SetReference(ref builder.GetRoot()->Head,node); }
            else { G.TableArena.SetReference(ref tail->Next,node); }
            tail=node;
        }
        AllocationCounts counts=default;
        var allocator=new G.TableAllocator { Allocate=&CountAllocate,Free=&CountFree,Context=(IntPtr)(&counts) };
        long size=G.Schema.SceneMeasure((IntPtr)builder.GetRoot(),allocator);
        Check(size>0 && counts.Calls==4 && counts.Live==0,"large graph grows and releases both numbering allocations");
        byte[] bytes=new byte[size];
        for(int fail=1;fail<=4;fail++)
        {
            counts=default; counts.FailAt=fail; Array.Fill(bytes,(byte)0xa5);
            Check(G.Schema.SceneSave((IntPtr)builder.GetRoot(),bytes,allocator)==-1 && counts.Live==0,"failed numbering growth frees old and partial new allocations");
            foreach(byte b in bytes) { Check(b==0xa5,"failed numbering growth preserves output"); }
        }
        // The hash defensively initializes itself even if a callback violates
        // the documented zeroing contract; other arena storage still requires it.
        counts=default; allocator.Allocate=&DirtyAllocate;
        Check(G.Schema.SceneSave((IntPtr)builder.GetRoot(),bytes,allocator)==size && counts.Live==0,"numbering hash initializes dirty scratch at every growth");
    }
    static unsafe void TestAllocators()
    {
        TestNumberingGrowthFailures();
        byte[] wire=ReadGolden("graph_shared"); long size=G.Schema.SceneLoadMeasure(wire);
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(size+64),64);
        try
        {
            Check(G.Schema.SceneLoad((IntPtr)region,size,wire,new G.TableReport())!=null,"allocator fixture loads");
            AllocationCounts counts=default;
            G.TableAllocator allocator=new G.TableAllocator { Allocate=&CountAllocate,Free=&CountFree,Context=(IntPtr)(&counts) };
            Check(G.Schema.SceneMeasure((IntPtr)region,allocator)==wire.Length && counts.Calls>0 && counts.Live==0 && counts.Calls==counts.Frees,"native file numbering uses balanced caller hooks");
            byte[] saved=new byte[wire.Length];
            Check(G.Schema.SceneSave((IntPtr)region,saved,allocator)==wire.Length && saved.AsSpan().SequenceEqual(wire) && counts.Live==0,"native save hook path preserves bytes");
            IntPtr[] roots={(IntPtr)region}; long messageSize=G.Schema.SceneMeasureMessages(roots,allocator);
            byte[] message=new byte[messageSize];
            Check(G.Schema.SceneSaveMessages(roots,message,allocator)==messageSize && counts.Live==0,"native message numbering uses caller hooks");
            long cookSize=G.Schema.SceneCookMeasure((IntPtr)region,allocator); byte[] cook=new byte[cookSize];
            Check(G.Schema.SceneCook((IntPtr)region,cook,G.TableByteOrder.Little,allocator) && counts.Live==0,"native cooking releases scratch");
            for(int fail=1;fail<=2;fail++)
            {
                counts=default; counts.FailAt=fail;
                using(var failed=new G.SceneBuilder(allocator))
                {
                    var loadReport=new G.TableReport { Verdict=G.Schema.TableWire.Verdict.Ok };
                    Check(!failed.Load(wire,loadReport) && loadReport.Verdict==G.Schema.TableWire.Verdict.BodyStopped && !loadReport.Malformed && !loadReport.Refused,"builder allocation failure replaces a stale verdict without inventing damage");
                }
                Check(counts.Live==0,"failed builder releases owned allocations");
                counts=default; counts.FailAt=fail;
                Array.Fill(saved,(byte)0xa5);
                Check(G.Schema.SceneSave((IntPtr)region,saved,allocator)==-1 && counts.Live==0,"allocation failure releases partial numbering");
                foreach(byte b in saved) { Check(b==0xa5,"allocation refusal preserves output"); }
                counts=default; counts.FailAt=fail; Array.Fill(cook,(byte)0xa5);
                Check(!G.Schema.SceneCook((IntPtr)region,cook,G.TableByteOrder.Little,allocator) && counts.Live==0,"cook allocation failure releases scratch");
                foreach(byte b in cook) { Check(b==0xa5,"cook allocation refusal preserves output"); }
            }
            counts=default; counts.FailAt=3;
            using(var failed=new Blobdemo.CatalogBuilder(new Blobdemo.TableAllocator { Allocate=&CountAllocate,Free=&CountFree,Context=(IntPtr)(&counts) }))
            {
                var loadReport=new Blobdemo.TableReport();
                Check(!failed.Load(ReadGolden("blob_large"),loadReport) && counts.Calls==3 && loadReport.Verdict==Blobdemo.Schema.TableWire.Verdict.BodyStopped,"mid-build blob allocation failure refuses with a current verdict");
            }
            Check(counts.Live==0,"mid-build failure releases directory and arena storage");
            counts=default; G.TableAllocator incomplete=new G.TableAllocator { Allocate=&CountAllocate,Context=(IntPtr)(&counts) };
            Check(G.Schema.SceneMeasure((IntPtr)region,incomplete)==-1 && counts.Calls==0,"incomplete allocator pair refuses before callback");
            Array.Fill(cook,(byte)0xa5);
            Check(!G.Schema.SceneCook((IntPtr)region,cook.AsSpan(0,cook.Length-1),G.TableByteOrder.Little,allocator),"short native cook refuses");
            foreach(byte b in cook) { Check(b==0xa5,"short native cook preserves output"); }
            Check(MeasureCalls(()=>G.Schema.SceneCookMeasure((IntPtr)region))==0,"native cook measure has zero warmed GC allocation");
            Check(MeasureCalls(()=>G.Schema.SceneCook((IntPtr)region,cook))==0,"native cook write has zero warmed GC allocation");
        }
        finally { NativeMemory.AlignedFree(region); }
    }
}
