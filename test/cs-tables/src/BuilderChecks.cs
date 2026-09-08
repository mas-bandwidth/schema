using System;
using System.Runtime.InteropServices;
using System.Threading.Tasks;
using G = Graphdemo;
using L = Listdemo;
using M = Mapdemo;

static partial class Program
{
    unsafe struct DescendingStorage { public byte* Base; public long Next; }
    static unsafe IntPtr AllocateDescending(IntPtr context,long bytes)
    {
        var storage=(DescendingStorage*)context; long n=(bytes+15)&~15L;
        if(n>storage->Next) { return IntPtr.Zero; }
        storage->Next-=n; return (IntPtr)(storage->Base+storage->Next);
    }
    static void FreeDescending(IntPtr context,IntPtr pointer) { }
    static unsafe void TestBuilders()
    {
        DescendingStorage descending=new DescendingStorage { Base=(byte*)NativeMemory.AlignedAlloc(32*1024*1024,16),Next=32*1024*1024 };
        NativeMemory.Clear(descending.Base,32*1024*1024);
        try
        {
            using var builder=new Blobdemo.CatalogBuilder(new Blobdemo.TableAllocator { Allocate=&AllocateDescending,Free=&FreeDescending,Context=(IntPtr)(&descending) });
            byte[] large=ReadGolden("blob_large");
            Check(builder.Load(large,new Blobdemo.TableReport()) && builder.GetRoot()->Thumb<0,"later blob slab may precede its root address");
            byte[] saved=new byte[builder.Measure()]; Check(builder.Save(saved)==large.Length && saved.AsSpan().SequenceEqual(large),"negative slab displacement preserves exact wire");
            byte* blob=Blobdemo.TableArena.At<byte>(ref builder.GetRoot()->Thumb);
            *(uint*)(blob+4)=0xdeadbeef;
            Check(builder.Measure()==large.Length,"native blob length ignores its reserved word");
            byte[] cooked=new byte[builder.CookMeasure()];
            Check(builder.Cook(cooked) && builder.Lock(),"negative slab displacement cooks and locks without checked-pointer overflow");
            byte[] locked=new byte[builder.CookMeasure()];
            Check(builder.Cook(locked) && locked.AsSpan().SequenceEqual(cooked),"negative slab displacement preserves canonical cook after lock");
        }
        finally { NativeMemory.AlignedFree(descending.Base); }
        AllocationCounts counts=default;
        G.TableAllocator allocator=new G.TableAllocator { Allocate=&CountAllocate,Free=&CountFree,Context=(IntPtr)(&counts) };
        using(var builder=new G.SceneBuilder(allocator))
        {
            G.SceneRow* root=builder.GetRoot(); Check(root!=null && root->Version==1 && root->Meta.Build==1,"builder root resets declared defaults");
            G.ListNodeRow* first=builder.Alloc<G.ListNodeRow>(),tail=first;
            first->Value=7; G.TableArena.SetReference(ref root->Head,first); G.TableArena.SetReference(ref root->Alias,first);
            for(int i=0;i<600;i++)
            {
                G.ListNodeRow* next=builder.Alloc<G.ListNodeRow>(); next->Value=i;
                G.TableArena.SetReference(ref tail->Next,next); tail=next;
            }
            G.TableBlobSlot blob=builder.AllocString(100000); Check(blob.Pointer!=IntPtr.Zero && blob.Length==100000 && blob.Data[100000]==0,"large blob spans slabs and terminates");
            G.TableBlobSlot wide=builder.AllocWString(35000); Check(wide.Pointer!=IntPtr.Zero && wide.Length==35000 && wide.Bytes.Length==70000 && wide.Data[70000]==0 && wide.Data[70001]==0,"wide blob allocation uses code units and terminates");
            var workers=new G.TableWorker[4]; var held=new IntPtr[4];
            for(int i=0;i<workers.Length;i++) { workers[i]=builder.CreateWorker(); }
            Parallel.For(0,4,i=>
            {
                G.SettingsRow* start=workers[i].Alloc<G.SettingsRow>(); start->Quality=i; held[i]=(IntPtr)start;
                for(int j=0;j<150000;j++) { workers[i].Alloc<G.SettingsRow>()->Quality=j; }
                Check(start->Quality==i,"worker growth preserves node address and content");
            });
            Check(MeasureCalls(()=>workers[0].Alloc<G.SettingsRow>())==0,"warmed worker fill allocates zero GC bytes");
            Check(first->Value==7 && G.TableArena.At<G.ListNodeRow>(ref root->Alias)==first,"arena segment growth preserves shared references");
            long wireSize=builder.Measure(); byte[] wire=new byte[wireSize];
            Check(builder.Save(wire)==wireSize,"mutable builder saves");
            Check(builder.Lock() && builder.Lock() && builder.IsLocked && builder.GetRoot()==null,"builder lock is one way and idempotent");
            G.SceneRow* packed=builder.AsConst();
            Check(G.TableArena.At<G.ListNodeRow>(ref packed->Head)==G.TableArena.At<G.ListNodeRow>(ref packed->Alias),"packing preserves shared node identity");
            Check(builder.RegionBytes==builder.DataBytes+builder.AttributionBytes && builder.AttributionBytes==602*16,"packing attributes exactly the reachable root and nodes");
            byte[] locked=new byte[builder.Measure()];
            Check(builder.Save(locked)==wireSize && locked.AsSpan().SequenceEqual(wire),"locking preserves exact wire");
            Check(workers[0].Alloc<G.SettingsRow>()==null && builder.Alloc<G.SettingsRow>()==null,"all workers stop allocating after lock");
            byte* relocated=(byte*)NativeMemory.AlignedAlloc((nuint)builder.RegionBytes,64);
            try
            {
                Buffer.MemoryCopy((void*)builder.Region,relocated,builder.RegionBytes,builder.RegionBytes);
                byte[] copy=new byte[wireSize];
                Check(G.Schema.SceneSave((IntPtr)relocated,copy)==wireSize && copy.AsSpan().SequenceEqual(wire),"locked regions relocate without fixup");
            }
            finally { NativeMemory.AlignedFree(relocated); }
        }
        Check(counts.Live==0 && counts.Calls==counts.Frees,"disposing the builder releases every arena, map and packed allocation");
        using(var cycle=new G.SceneBuilder())
        {
            var node=cycle.Alloc<G.ListNodeRow>(); G.TableArena.SetReference(ref cycle.GetRoot()->Head,node);
            G.TableArena.SetReference(ref node->Next,node);
            Check(cycle.Measure()==-1 && !cycle.Lock() && !cycle.IsLocked,"cycles refuse save and lock without consuming the builder");
            node->Next=0; Check(cycle.Lock(),"cycle can be corrected before locking");
        }
        using(var builder=new L.SaveBuilder())
        {
            var worker=builder.CreateWorker(); var root=builder.GetRoot();
            var scores=worker.List<int>(ref root->Scores,L.Schema.SaveTableType().Fields[2]);
            int* first=scores.Add(); *first=73;
            for(int i=0;i<2000;i++) { *scores.Add()=i; }
            Check(scores.Count==2001 && scores.At(0)==first && *first==73,"list growth preserves element addresses");
            int count=scores.Count; Check(scores.Reserve(5000) && scores.Count==count && scores.Capacity>=5000,"list reserve changes capacity only");
            int* second=scores.At(1); Check(scores.Erase(0) && scores.At(0)==second && *second==0,"list erase shifts identities without moving elements");
            byte[] before=new byte[builder.Measure()]; builder.Save(before);
            Check(builder.Lock(),"list builder locks"); byte[] after=new byte[builder.Measure()]; builder.Save(after);
            Check(before.AsSpan().SequenceEqual(after),"list lock preserves insertion order");
            Check(scores.Add()==null && !scores.Erase(0) && !scores.Reserve(1) && scores.At(0)==null && scores.Count==0,"old list handle refuses every access after lock");
            builder.Dispose(); Check(!scores.Erase(0),"old list erase refuses after disposal");
        }
        using(var builder=new M.FleetBuilder())
        {
            var worker=builder.CreateWorker(); var root=builder.GetRoot();
            var map=worker.Map<M.FleetTiersEntryRow>(ref root->Tiers,M.Schema.FleetTableType().Fields[4]);
            var held=map.Insert(50L); held->Value.Count=91;
            for(long key=1000;key>=-1000;key--) { map.Insert(key)->Value.Count=(int)key; }
            Check(map.Count==2001 && map.Find(50L)==held,"map insertion preserves entry identity and replaces no existing value");
            for(int i=1;i<map.Count;i++) { Check(map.At(i-1)->Key<map.At(i)->Key,"map iteration is ordered by signed key"); }
            Check(map.Insert(40000L)==null,"map key outside declared storage refuses");
            Check(map.Erase(0L) && map.Find(0L)==null && map.Find(50L)==held,"map erase preserves other entry addresses");
            byte[] before=new byte[builder.Measure()]; builder.Save(before);
            Check(builder.Lock(),"map builder locks"); byte[] after=new byte[builder.Measure()]; builder.Save(after);
            Check(before.AsSpan().SequenceEqual(after),"map lock preserves canonical key order");
        }
        byte[] shortList=Fixture(new byte[] {1,14,6,4,3,42,0,0,0,0},"scores");
        Check(L.Schema.SaveLoadMeasure(shortList,out var shortReason)==-1 && shortReason==L.TableRefuseReason.count_over_length,"region measure refuses impossible list count");
        using(var builder=new L.SaveBuilder())
        {
            var report=new L.TableReport();
            Check(L.Schema.SaveLoadBuilder(builder,shortList,report) && report.Malformed && !report.Refused && report.Clamped==0,"builder decodes a short list prefix as damage");
            var list=builder.CreateWorker().List<int>(ref builder.GetRoot()->Scores,L.Schema.SaveTableType().Fields[2]);
            Check(list.Count==1 && *list.At(0)==42,"builder retains the decoded prefix");
        }
        byte[] cappedList=Fixture(Join(new byte[] {1,6,5,2,14,6,4},Var((ulong)int.MaxValue+1),new byte[] {3,6,7,0}),"before","scores","after");
        using(var builder=new L.SaveBuilder())
        {
            var report=new L.TableReport();
            Check(!L.Schema.SaveLoadBuilder(builder,cappedList,report) && report.Unknown==1 && report.KindMismatch==0 && report.Widened==0 && report.Duplicate==0 && !report.Malformed && !report.Refused && report.Clamped==0,"builder count-cap refusal preserves exactly the preceding report");
        }
        byte[] cappedMap=Fixture(Join(new byte[] {1,6,5,2,14,6,13},Var((ulong)int.MaxValue+1),new byte[] {3,6,7,0}),"before","tiers","after");
        using(var builder=new M.FleetBuilder())
        {
            var report=new M.TableReport();
            Check(!M.Schema.FleetLoadBuilder(builder,cappedMap,report) && report.Unknown==1 && report.KindMismatch==0 && report.Widened==0 && report.Duplicate==0 && !report.Malformed && !report.Refused && report.Clamped==0,"builder map count-cap refusal preserves exactly the preceding report");
        }
        byte[] shortMap=Fixture(Join(new byte[] {1,14,6,13},Var(int.MaxValue),new byte[] {0}),"tiers");
        using(var builder=new M.FleetBuilder())
        {
            var report=new M.TableReport();
            Check(M.Schema.FleetLoadBuilder(builder,shortMap,report) && report.Malformed && !report.Refused && report.Clamped==0 && builder.GetRoot()->Tiers.Count==0,"builder map at the count cap decodes only the available prefix");
        }
        byte[] loaded=ReadGolden("list_scalars"); long size=L.Schema.SaveLoadMeasure(loaded);
        byte* source=(byte*)NativeMemory.AlignedAlloc((nuint)(size+64),64);
        try
        {
            L.Schema.SaveLoad((IntPtr)source,size,loaded,new L.TableReport());
            // Region padding is never a mutable capacity, including when
            // adopting a region supplied by another runtime.
            ((L.SaveRow*)source)->Scores.Capacity=0x12345678;
            byte[] padded=new byte[L.Schema.SaveMeasure((IntPtr)source)];
            Check(L.Schema.SaveSave((IntPtr)source,padded)==loaded.Length && padded.AsSpan().SequenceEqual(loaded),"region read ignores collection padding");
            byte[] paddedCook=new byte[L.Schema.SaveCookMeasure((IntPtr)source)];
            Check(L.Schema.SaveCook((IntPtr)source,paddedCook),"native cook ignores collection padding");
            ((L.SaveRow*)source)->Scores.Capacity=0; byte[] cleanCook=new byte[L.Schema.SaveCookMeasure((IntPtr)source)];
            Check(L.Schema.SaveCook((IntPtr)source,cleanCook) && cleanCook.AsSpan().SequenceEqual(paddedCook),"native cook canonical bytes exclude collection padding");
            ((L.SaveRow*)source)->Scores.Capacity=0x12345678;
            L.TableRetain emptyRetain=default;
            byte[] retained=new byte[L.Schema.SaveMeasureRetain((IntPtr)source,ref emptyRetain)];
            Check(L.Schema.SaveSaveRetain((IntPtr)source,ref emptyRetain,retained,new L.TableReport())==loaded.Length && retained.AsSpan().SequenceEqual(loaded),"retained save ignores collection padding");
            using(var builder=new L.SaveBuilder())
            {
                Check(builder.CopyFrom((IntPtr)source),"builder copies loaded collections");
                var root=builder.GetRoot(); var worker=builder.CreateWorker();
                var scores=worker.List<int>(ref root->Scores,L.Schema.SaveTableType().Fields[2]);
                int* original=scores.At(0); int value=*original;
                for(int i=0;i<1000;i++) { *scores.Add()=i; }
                Check(scores.At(0)==original && *original==value,"loaded elements stay put when adopted by mutable storage");
                Check(builder.Lock(),"edited loaded list locks");
            }
        }
        finally { NativeMemory.AlignedFree(source); }
    }
}
