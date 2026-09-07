using System;
using System.Runtime.InteropServices;

static partial class Program
{
    delegate long RegionSave(IntPtr region,Span<byte> bytes);
    delegate long RegionMeasure(ReadOnlySpan<byte> bytes);
    delegate IntPtr RegionLoad<R>(IntPtr data,long capacity,ReadOnlySpan<byte> bytes,R report);
    static unsafe void RegionCase<T,R>(string name,Func<R> makeReport,Func<R,bool> silent,RegionMeasure measure,
        RegionLoad<R> load,Func<IntPtr,T> copy,Func<T,long> fileMeasure,CollectionSave<T> fileSave,Func<IntPtr,long> regionMeasure,RegionSave regionSave)
    {
        byte[] bytes=ReadGolden(name); long n=measure(bytes);
        Check(n>=0,name+" native region is measurable"); if(n<0) { return; }
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(n+64),64);
        byte* relocated=(byte*)NativeMemory.AlignedAlloc((nuint)(n+64),64);
        try
        {
            new Span<byte>(region,(int)n+64).Fill(0xa5);
            R report=makeReport(); IntPtr p=(IntPtr)region;
            Check(load(p,n-1,bytes,report)==IntPtr.Zero && region[0]==0xa5,name+" capacity refusal precedes writes");
            Check(load(p+1,n,bytes,report)==IntPtr.Zero && region[1]==0xa5,name+" alignment refusal precedes writes");
            Check(load(p,n,bytes,report)==p && silent(report),name+" native region loads silently");
            for(long i=n;i<n+64;i++) { Check(region[i]==0xa5,name+" exact-size load stays inside allocation"); }
            new ReadOnlySpan<byte>(region,(int)n).CopyTo(new Span<byte>(relocated,(int)n));
            NativeMemory.Clear(region,(nuint)n);
            long nativeSize=regionMeasure((IntPtr)relocated); Check(nativeSize==bytes.Length,name+" native save measure");
            if(nativeSize>=0) { byte[] native=new byte[nativeSize]; Check(regionSave((IntPtr)relocated,native)==nativeSize && native.AsSpan().SequenceEqual(bytes),name+" native writer reproduces C++ wire"); }
            T value=copy((IntPtr)relocated); long wireSize=fileMeasure(value);
            Check(wireSize==bytes.Length,name+" relocated region preserves wire length");
            if(wireSize<0) { return; }
            byte[] back=new byte[wireSize];
            Check(fileSave(value,back)==wireSize && back.AsSpan().SequenceEqual(bytes),name+" relocated region reproduces C++ wire");
            long allocated=MeasureCalls(() => { load(p,n,bytes,report); });
            Check(allocated==0,name+" native load allocates zero (observed "+allocated+")");
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.AlignedFree(relocated); }
    }
    static unsafe void TestRegions()
    {
        byte[] shared=ReadGolden("graph_shared");
        long total=Graphdemo.Schema.SceneLoadMeasure(shared,out long dataBytes,out long attributionBytes,out var reason);
        Check(total==dataBytes+attributionBytes && reason==Graphdemo.TableRefuseReason.ok,"region measure exposes both parts");
        byte* data=(byte*)NativeMemory.AlignedAlloc((nuint)dataBytes,64);
        byte* attribution=(byte*)NativeMemory.AlignedAlloc((nuint)attributionBytes,64);
        try
        {
            var report=new Graphdemo.TableReport();
            Check(Graphdemo.Schema.SceneLoad((IntPtr)data,dataBytes,(IntPtr)attribution,attributionBytes-1,shared,report)==null,"short attribution refuses");
            Check(Graphdemo.Schema.SceneLoad((IntPtr)data,dataBytes,(IntPtr)data,attributionBytes,shared,report)==null,"overlapping parts refuse");
            Check(Graphdemo.Schema.SceneLoad((IntPtr)data,dataBytes,(IntPtr)attribution,attributionBytes,shared,report)!=null && !report.Malformed,"separate parts load");
            NativeMemory.Clear(attribution,(nuint)attributionBytes);
            var copied=Graphdemo.Schema.SceneLoadBuilder((IntPtr)data);
            byte[] back=new byte[Graphdemo.Schema.SceneMeasure(copied)];
            Check(Graphdemo.Schema.SceneSave(copied,back)==shared.Length && back.AsSpan().SequenceEqual(shared),"attribution can be released before reads");
        }
        finally { NativeMemory.AlignedFree(data); NativeMemory.AlignedFree(attribution); }
        RegionCase<Listdemo.Save,Listdemo.TableReport>("list_tables",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.SaveLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.SaveLoad(p,n,b,r),Listdemo.Schema.SaveLoadBuilder,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave);
        RegionCase<Listdemo.Save,Listdemo.TableReport>("list_scalars",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.SaveLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.SaveLoad(p,n,b,r),Listdemo.Schema.SaveLoadBuilder,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave);
        RegionCase<Listdemo.Save,Listdemo.TableReport>("list_empty",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.SaveLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.SaveLoad(p,n,b,r),Listdemo.Schema.SaveLoadBuilder,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave);
        RegionCase<Listdemo.Save,Listdemo.TableReport>("list_erased",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.SaveLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.SaveLoad(p,n,b,r),Listdemo.Schema.SaveLoadBuilder,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasure,Listdemo.Schema.SaveSave);
        RegionCase<Listdemo.Album,Listdemo.TableReport>("list_shared",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.AlbumLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.AlbumLoad(p,n,b,r),Listdemo.Schema.AlbumLoadBuilder,Listdemo.Schema.AlbumMeasure,Listdemo.Schema.AlbumSave,Listdemo.Schema.AlbumMeasure,Listdemo.Schema.AlbumSave);
        RegionCase<Listdemo.Mixed,Listdemo.TableReport>("list_mixed",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.MixedLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.MixedLoad(p,n,b,r),Listdemo.Schema.MixedLoadBuilder,Listdemo.Schema.MixedMeasure,Listdemo.Schema.MixedSave,Listdemo.Schema.MixedMeasure,Listdemo.Schema.MixedSave);
        RegionCase<Listdemo.Album,Listdemo.TableReport>("list_before_pointer",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.AlbumLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.AlbumLoad(p,n,b,r),Listdemo.Schema.AlbumLoadBuilder,Listdemo.Schema.AlbumMeasure,Listdemo.Schema.AlbumSave,Listdemo.Schema.AlbumMeasure,Listdemo.Schema.AlbumSave);
        RegionCase<Listdemo.Sheet,Listdemo.TableReport>("list_nested",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.SheetLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.SheetLoad(p,n,b,r),Listdemo.Schema.SheetLoadBuilder,Listdemo.Schema.SheetMeasure,Listdemo.Schema.SheetSave,Listdemo.Schema.SheetMeasure,Listdemo.Schema.SheetSave);
        RegionCase<Listdemo.Army,Listdemo.TableReport>("list_of_maps",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Listdemo.Schema.ArmyLoadMeasure,(p,n,b,r)=>(IntPtr)Listdemo.Schema.ArmyLoad(p,n,b,r),Listdemo.Schema.ArmyLoadBuilder,Listdemo.Schema.ArmyMeasure,Listdemo.Schema.ArmySave,Listdemo.Schema.ArmyMeasure,Listdemo.Schema.ArmySave);
        RegionCase<Mapdemo.Fleet,Mapdemo.TableReport>("map_full",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.FleetLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.FleetLoad(p,n,b,r),Mapdemo.Schema.FleetLoadBuilder,Mapdemo.Schema.FleetMeasure,Mapdemo.Schema.FleetSave,Mapdemo.Schema.FleetMeasure,Mapdemo.Schema.FleetSave);
        RegionCase<Mapdemo.Fleet,Mapdemo.TableReport>("map_empty",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.FleetLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.FleetLoad(p,n,b,r),Mapdemo.Schema.FleetLoadBuilder,Mapdemo.Schema.FleetMeasure,Mapdemo.Schema.FleetSave,Mapdemo.Schema.FleetMeasure,Mapdemo.Schema.FleetSave);
        RegionCase<Mapdemo.Depth,Mapdemo.TableReport>("map_depth",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.DepthLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.DepthLoad(p,n,b,r),Mapdemo.Schema.DepthLoadBuilder,Mapdemo.Schema.DepthMeasure,Mapdemo.Schema.DepthSave,Mapdemo.Schema.DepthMeasure,Mapdemo.Schema.DepthSave);
        RegionCase<Mapdemo.Text,Mapdemo.TableReport>("map_text",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.TextLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.TextLoad(p,n,b,r),Mapdemo.Schema.TextLoadBuilder,Mapdemo.Schema.TextMeasure,Mapdemo.Schema.TextSave,Mapdemo.Schema.TextMeasure,Mapdemo.Schema.TextSave);
        RegionCase<Mapdemo.Cells,Mapdemo.TableReport>("map_cells",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.CellsLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.CellsLoad(p,n,b,r),Mapdemo.Schema.CellsLoadBuilder,Mapdemo.Schema.CellsMeasure,Mapdemo.Schema.CellsSave,Mapdemo.Schema.CellsMeasure,Mapdemo.Schema.CellsSave);
        RegionCase<Mapdemo.Runs,Mapdemo.TableReport>("map_runs",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.RunsLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.RunsLoad(p,n,b,r),Mapdemo.Schema.RunsLoadBuilder,Mapdemo.Schema.RunsMeasure,Mapdemo.Schema.RunsSave,Mapdemo.Schema.RunsMeasure,Mapdemo.Schema.RunsSave);
        RegionCase<Mapdemo.Slots,Mapdemo.TableReport>("map_slots",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.SlotsLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.SlotsLoad(p,n,b,r),Mapdemo.Schema.SlotsLoadBuilder,Mapdemo.Schema.SlotsMeasure,Mapdemo.Schema.SlotsSave,Mapdemo.Schema.SlotsMeasure,Mapdemo.Schema.SlotsSave);
        RegionCase<Mapdemo.Spans,Mapdemo.TableReport>("map_spans",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.SpansLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.SpansLoad(p,n,b,r),Mapdemo.Schema.SpansLoadBuilder,Mapdemo.Schema.SpansMeasure,Mapdemo.Schema.SpansSave,Mapdemo.Schema.SpansMeasure,Mapdemo.Schema.SpansSave);
        RegionCase<Mapdemo.Docs,Mapdemo.TableReport>("map_docs",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.DocsLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.DocsLoad(p,n,b,r),Mapdemo.Schema.DocsLoadBuilder,Mapdemo.Schema.DocsMeasure,Mapdemo.Schema.DocsSave,Mapdemo.Schema.DocsMeasure,Mapdemo.Schema.DocsSave);
        RegionCase<Mapdemo.Chunks,Mapdemo.TableReport>("map_chunks",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.ChunksLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.ChunksLoad(p,n,b,r),Mapdemo.Schema.ChunksLoadBuilder,Mapdemo.Schema.ChunksMeasure,Mapdemo.Schema.ChunksSave,Mapdemo.Schema.ChunksMeasure,Mapdemo.Schema.ChunksSave);
        RegionCase<Mapdemo.Pairs,Mapdemo.TableReport>("map_pairs",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.PairsLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.PairsLoad(p,n,b,r),Mapdemo.Schema.PairsLoadBuilder,Mapdemo.Schema.PairsMeasure,Mapdemo.Schema.PairsSave,Mapdemo.Schema.PairsMeasure,Mapdemo.Schema.PairsSave);
        RegionCase<Mapdemo.Crews,Mapdemo.TableReport>("map_crews",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.CrewsLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.CrewsLoad(p,n,b,r),Mapdemo.Schema.CrewsLoadBuilder,Mapdemo.Schema.CrewsMeasure,Mapdemo.Schema.CrewsSave,Mapdemo.Schema.CrewsMeasure,Mapdemo.Schema.CrewsSave);
        RegionCase<Mapdemo.Trails,Mapdemo.TableReport>("map_trails",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Mapdemo.Schema.TrailsLoadMeasure,(p,n,b,r)=>(IntPtr)Mapdemo.Schema.TrailsLoad(p,n,b,r),Mapdemo.Schema.TrailsLoadBuilder,Mapdemo.Schema.TrailsMeasure,Mapdemo.Schema.TrailsSave,Mapdemo.Schema.TrailsMeasure,Mapdemo.Schema.TrailsSave);
        RegionCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_shared",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Graphdemo.Schema.SceneLoadMeasure,(p,n,b,r)=>(IntPtr)Graphdemo.Schema.SceneLoad(p,n,b,r),Graphdemo.Schema.SceneLoadBuilder,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave);
        RegionCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_tree",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Graphdemo.Schema.SceneLoadMeasure,(p,n,b,r)=>(IntPtr)Graphdemo.Schema.SceneLoad(p,n,b,r),Graphdemo.Schema.SceneLoadBuilder,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave);
        RegionCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_empty",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Graphdemo.Schema.SceneLoadMeasure,(p,n,b,r)=>(IntPtr)Graphdemo.Schema.SceneLoad(p,n,b,r),Graphdemo.Schema.SceneLoadBuilder,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave);
        RegionCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_deep",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Graphdemo.Schema.SceneLoadMeasure,(p,n,b,r)=>(IntPtr)Graphdemo.Schema.SceneLoad(p,n,b,r),Graphdemo.Schema.SceneLoadBuilder,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasure,Graphdemo.Schema.SceneSave);
        RegionCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_chain",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Streamdemo.Schema.FeedLoadMeasure,(p,n,b,r)=>(IntPtr)Streamdemo.Schema.FeedLoad(p,n,b,r),Streamdemo.Schema.FeedLoadBuilder,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave);
        RegionCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_header",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Streamdemo.Schema.FeedLoadMeasure,(p,n,b,r)=>(IntPtr)Streamdemo.Schema.FeedLoad(p,n,b,r),Streamdemo.Schema.FeedLoadBuilder,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave);
        RegionCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_parts",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Streamdemo.Schema.FeedLoadMeasure,(p,n,b,r)=>(IntPtr)Streamdemo.Schema.FeedLoad(p,n,b,r),Streamdemo.Schema.FeedLoadBuilder,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave);
        RegionCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_arm_first",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Streamdemo.Schema.FeedLoadMeasure,(p,n,b,r)=>(IntPtr)Streamdemo.Schema.FeedLoad(p,n,b,r),Streamdemo.Schema.FeedLoadBuilder,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave);
        RegionCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_arm_pointer",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Streamdemo.Schema.FeedLoadMeasure,(p,n,b,r)=>(IntPtr)Streamdemo.Schema.FeedLoad(p,n,b,r),Streamdemo.Schema.FeedLoadBuilder,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasure,Streamdemo.Schema.FeedSave);
        RegionCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_small",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Blobdemo.Schema.CatalogLoadMeasure,(p,n,b,r)=>(IntPtr)Blobdemo.Schema.CatalogLoad(p,n,b,r),Blobdemo.Schema.CatalogLoadBuilder,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave);
        RegionCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_empty",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Blobdemo.Schema.CatalogLoadMeasure,(p,n,b,r)=>(IntPtr)Blobdemo.Schema.CatalogLoad(p,n,b,r),Blobdemo.Schema.CatalogLoadBuilder,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave);
        RegionCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_large",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Blobdemo.Schema.CatalogLoadMeasure,(p,n,b,r)=>(IntPtr)Blobdemo.Schema.CatalogLoad(p,n,b,r),Blobdemo.Schema.CatalogLoadBuilder,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave);
        RegionCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_shared",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Blobdemo.Schema.CatalogLoadMeasure,(p,n,b,r)=>(IntPtr)Blobdemo.Schema.CatalogLoad(p,n,b,r),Blobdemo.Schema.CatalogLoadBuilder,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave);
        RegionCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_str8",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Blobdemo.Schema.CatalogLoadMeasure,(p,n,b,r)=>(IntPtr)Blobdemo.Schema.CatalogLoad(p,n,b,r),Blobdemo.Schema.CatalogLoadBuilder,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave);
        RegionCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_str16",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Blobdemo.Schema.CatalogLoadMeasure,(p,n,b,r)=>(IntPtr)Blobdemo.Schema.CatalogLoad(p,n,b,r),Blobdemo.Schema.CatalogLoadBuilder,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasure,Blobdemo.Schema.CatalogSave);
        RegionCase<Tblg1.Guarded,Tblg1.TableReport>("guard_false_no_edge",()=>new Tblg1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Tblg1.Schema.GuardedLoadMeasure,(p,n,b,r)=>(IntPtr)Tblg1.Schema.GuardedLoad(p,n,b,r),Tblg1.Schema.GuardedLoadBuilder,Tblg1.Schema.GuardedMeasure,Tblg1.Schema.GuardedSave,Tblg1.Schema.GuardedMeasure,Tblg1.Schema.GuardedSave);
        RegionCase<Tblg1.Guarded,Tblg1.TableReport>("guard_true_both_edges",()=>new Tblg1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Tblg1.Schema.GuardedLoadMeasure,(p,n,b,r)=>(IntPtr)Tblg1.Schema.GuardedLoad(p,n,b,r),Tblg1.Schema.GuardedLoadBuilder,Tblg1.Schema.GuardedMeasure,Tblg1.Schema.GuardedSave,Tblg1.Schema.GuardedMeasure,Tblg1.Schema.GuardedSave);
        RegionCase<Tblw1.Fleet,Tblw1.TableReport>("w1_fleet",()=>new Tblw1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Tblw1.Schema.FleetLoadMeasure,(p,n,b,r)=>(IntPtr)Tblw1.Schema.FleetLoad(p,n,b,r),Tblw1.Schema.FleetLoadBuilder,Tblw1.Schema.FleetMeasure,Tblw1.Schema.FleetSave,Tblw1.Schema.FleetMeasure,Tblw1.Schema.FleetSave);
        RegionCase<Tblw1.Fleet,Tblw1.TableReport>("w1_fleet_default",()=>new Tblw1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Tblw1.Schema.FleetLoadMeasure,(p,n,b,r)=>(IntPtr)Tblw1.Schema.FleetLoad(p,n,b,r),Tblw1.Schema.FleetLoadBuilder,Tblw1.Schema.FleetMeasure,Tblw1.Schema.FleetSave,Tblw1.Schema.FleetMeasure,Tblw1.Schema.FleetSave);
        RegionCase<Tblw2.Fleet,Tblw2.TableReport>("w2_fleet",()=>new Tblw2.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
            Tblw2.Schema.FleetLoadMeasure,(p,n,b,r)=>(IntPtr)Tblw2.Schema.FleetLoad(p,n,b,r),Tblw2.Schema.FleetLoadBuilder,Tblw2.Schema.FleetMeasure,Tblw2.Schema.FleetSave,Tblw2.Schema.FleetMeasure,Tblw2.Schema.FleetSave);
    }
}
