using System;
using System.Runtime.InteropServices;

static partial class Program
{
    delegate long NativeMessageSave(ReadOnlySpan<IntPtr> roots,Span<byte> bytes);
    delegate long NativeMessageSize(ReadOnlySpan<IntPtr> roots);
    delegate long NativeMessageMeasure(ReadOnlySpan<byte> bytes);
    delegate bool NativeMessageLoad<R>(IntPtr region,long capacity,Span<IntPtr> roots,ReadOnlySpan<byte> bytes,R report,out int count);
    static unsafe void NativeMessageCase<T,R>(string name,Func<R> makeReport,Func<R,bool> silent,CollectionLoad<T,R> fileLoad,
        Func<T[],long> measure,CollectionMessageSave<T> save,NativeMessageMeasure regionMeasure,NativeMessageLoad<R> load,RegionSave fileSave,NativeMessageSize nativeMeasure,NativeMessageSave nativeSave) where T:new()
    {
        byte[] golden=ReadGolden(name); T value=new T(); R report=makeReport();
        Check(fileLoad(value,golden,report),name+" native message fixture loads");
        T[] batch={value,value}; byte[] wire=new byte[measure(batch)]; Check(save(batch,wire)==wire.Length,name+" message fixture writes");
        long need=regionMeasure(wire); Check(need>=0,name+" native message measured"); if(need<0) { return; }
        byte* data=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
        byte* moved=(byte*)NativeMemory.AlignedAlloc((nuint)need,64); IntPtr[] roots=new IntPtr[2]; report=makeReport();
        try
        {
            new Span<byte>(data,(int)need+64).Fill(0xa5);
            Check(load((IntPtr)data,need,roots,wire,report,out int count) && count==2 && silent(report),name+" native message batch loads silently");
            Check(roots[0]!=IntPtr.Zero && roots[1]!=IntPtr.Zero && roots[0]!=roots[1],name+" message bodies own separate regions");
            if(roots[0]==IntPtr.Zero || roots[1]==IntPtr.Zero) { return; }
            for(long i=need;i<need+64;i++) { Check(data[i]==0xa5,name+" message stays inside measured region"); }
            new ReadOnlySpan<byte>(data,(int)need).CopyTo(new Span<byte>(moved,(int)need)); NativeMemory.Clear(data,(nuint)need);
            foreach(IntPtr root in roots)
            {
                byte[] back=new byte[golden.Length]; IntPtr relocated=(IntPtr)(moved+((byte*)root-data));
                Check(fileSave(relocated,back)==back.Length && back.AsSpan().SequenceEqual(golden),name+" relocated message body reproduces C++ wire");
            }
            IntPtr[] relocatedRoots=new IntPtr[2];
            for(int i=0;i<2;i++) { relocatedRoots[i]=(IntPtr)(moved+((byte*)roots[i]-data)); }
            long messageBytes=nativeMeasure(relocatedRoots); Check(messageBytes==wire.Length,name+" native message writer measures the original batch");
            if(messageBytes>=0) { byte[] back=new byte[messageBytes]; Check(nativeSave(relocatedRoots,back)==back.Length && back.AsSpan().SequenceEqual(wire),name+" native message writer preserves batch bytes"); }
            long allocated=MeasureCalls(()=> { load((IntPtr)data,need,roots,wire,report,out _); });
            Check(allocated==0,name+" native message load allocates zero (observed "+allocated+")");
        }
        finally { NativeMemory.AlignedFree(data); NativeMemory.AlignedFree(moved); }
    }
    static unsafe void TestNativeMessages()
    {
        {
            byte[] file=ReadGolden("graph_shared"); var scene=new Graphdemo.Scene(); var report=new Graphdemo.TableReport();
            Graphdemo.Schema.SceneLoad(scene,file,report); var batch=new[] {scene,scene};
            byte[] bytes=new byte[Graphdemo.Schema.SceneMeasureMessages(batch)]; Graphdemo.Schema.SceneSaveMessages(batch,bytes);
            var vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,Graphdemo.Schema.Announce(),report);
            long total=Graphdemo.Schema.SceneLoadMeasure(vocabulary,bytes,out long dataBytes,out long attributionBytes);
            Check(total==dataBytes+attributionBytes,"message measure separates data and attribution");
            byte* data=(byte*)NativeMemory.AlignedAlloc((nuint)dataBytes,64);
            byte* attribution=(byte*)NativeMemory.AlignedAlloc((nuint)attributionBytes,64); IntPtr[] roots=new IntPtr[2];
            try
            {
                new Span<byte>(data,(int)dataBytes).Fill(0xa5);
                var verdict=Graphdemo.Schema.SceneLoadMessages((IntPtr)data,dataBytes,(IntPtr)attribution,attributionBytes,roots.AsSpan(0,1),bytes,vocabulary,report,out int count);
                Check(verdict==Graphdemo.Schema.TableWire.Verdict.Refused && report.Reason=="batch_too_large" && count==2 && data[0]==0xa5,"native message root capacity refuses before writes");
                verdict=Graphdemo.Schema.SceneLoadMessages((IntPtr)data,dataBytes,(IntPtr)attribution,attributionBytes,roots,bytes,vocabulary,report,out count);
                Check(verdict==Graphdemo.Schema.TableWire.Verdict.Ok && count==2,"native message accepts separate parts");
                NativeMemory.Clear(attribution,(nuint)attributionBytes);
                byte[] back=new byte[Graphdemo.Schema.SceneMeasureMessages(roots)];
                Check(Graphdemo.Schema.SceneSaveMessages(roots,back)==bytes.Length && back.AsSpan().SequenceEqual(bytes),"message reads survive released attribution");
                long allocated=MeasureCalls(()=> { Graphdemo.Schema.SceneLoadMessages((IntPtr)data,dataBytes,(IntPtr)attribution,attributionBytes,roots,bytes,vocabulary,report,out _); });
                Check(allocated==0,"separate message parts load without allocation");
            }
            finally { NativeMemory.AlignedFree(data); NativeMemory.AlignedFree(attribution); }
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Save,Listdemo.TableReport>("list_tables",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.SaveLoad,Listdemo.Schema.SaveMeasureMessages,(v,b)=>Listdemo.Schema.SaveSaveMessages(v,b),b=>Listdemo.Schema.SaveLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.SaveLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasureMessages,Listdemo.Schema.SaveSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Save,Listdemo.TableReport>("list_scalars",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.SaveLoad,Listdemo.Schema.SaveMeasureMessages,(v,b)=>Listdemo.Schema.SaveSaveMessages(v,b),b=>Listdemo.Schema.SaveLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.SaveLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasureMessages,Listdemo.Schema.SaveSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Save,Listdemo.TableReport>("list_empty",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.SaveLoad,Listdemo.Schema.SaveMeasureMessages,(v,b)=>Listdemo.Schema.SaveSaveMessages(v,b),b=>Listdemo.Schema.SaveLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.SaveLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasureMessages,Listdemo.Schema.SaveSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Save,Listdemo.TableReport>("list_erased",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.SaveLoad,Listdemo.Schema.SaveMeasureMessages,(v,b)=>Listdemo.Schema.SaveSaveMessages(v,b),b=>Listdemo.Schema.SaveLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.SaveLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.SaveSave,Listdemo.Schema.SaveMeasureMessages,Listdemo.Schema.SaveSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Album,Listdemo.TableReport>("list_shared",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.AlbumLoad,Listdemo.Schema.AlbumMeasureMessages,(v,b)=>Listdemo.Schema.AlbumSaveMessages(v,b),b=>Listdemo.Schema.AlbumLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.AlbumLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.AlbumSave,Listdemo.Schema.AlbumMeasureMessages,Listdemo.Schema.AlbumSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Mixed,Listdemo.TableReport>("list_mixed",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.MixedLoad,Listdemo.Schema.MixedMeasureMessages,(v,b)=>Listdemo.Schema.MixedSaveMessages(v,b),b=>Listdemo.Schema.MixedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.MixedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.MixedSave,Listdemo.Schema.MixedMeasureMessages,Listdemo.Schema.MixedSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Album,Listdemo.TableReport>("list_before_pointer",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.AlbumLoad,Listdemo.Schema.AlbumMeasureMessages,(v,b)=>Listdemo.Schema.AlbumSaveMessages(v,b),b=>Listdemo.Schema.AlbumLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.AlbumLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.AlbumSave,Listdemo.Schema.AlbumMeasureMessages,Listdemo.Schema.AlbumSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Sheet,Listdemo.TableReport>("list_nested",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.SheetLoad,Listdemo.Schema.SheetMeasureMessages,(v,b)=>Listdemo.Schema.SheetSaveMessages(v,b),b=>Listdemo.Schema.SheetLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.SheetLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.SheetSave,Listdemo.Schema.SheetMeasureMessages,Listdemo.Schema.SheetSaveMessages);
        }
        {
            var vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),new Listdemo.TableReport());
            NativeMessageCase<Listdemo.Army,Listdemo.TableReport>("list_of_maps",()=>new Listdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Listdemo.Schema.ArmyLoad,Listdemo.Schema.ArmyMeasureMessages,(v,b)=>Listdemo.Schema.ArmySaveMessages(v,b),b=>Listdemo.Schema.ArmyLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Listdemo.TableReport r,out int count)=>Listdemo.Schema.ArmyLoadMessages(p,n,roots,b,vocabulary,r,out count)==Listdemo.Schema.TableWire.Verdict.Ok,Listdemo.Schema.ArmySave,Listdemo.Schema.ArmyMeasureMessages,Listdemo.Schema.ArmySaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Fleet,Mapdemo.TableReport>("map_full",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.FleetLoad,Mapdemo.Schema.FleetMeasureMessages,(v,b)=>Mapdemo.Schema.FleetSaveMessages(v,b),b=>Mapdemo.Schema.FleetLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.FleetLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.FleetSave,Mapdemo.Schema.FleetMeasureMessages,Mapdemo.Schema.FleetSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Fleet,Mapdemo.TableReport>("map_empty",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.FleetLoad,Mapdemo.Schema.FleetMeasureMessages,(v,b)=>Mapdemo.Schema.FleetSaveMessages(v,b),b=>Mapdemo.Schema.FleetLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.FleetLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.FleetSave,Mapdemo.Schema.FleetMeasureMessages,Mapdemo.Schema.FleetSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Depth,Mapdemo.TableReport>("map_depth",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.DepthLoad,Mapdemo.Schema.DepthMeasureMessages,(v,b)=>Mapdemo.Schema.DepthSaveMessages(v,b),b=>Mapdemo.Schema.DepthLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.DepthLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.DepthSave,Mapdemo.Schema.DepthMeasureMessages,Mapdemo.Schema.DepthSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Text,Mapdemo.TableReport>("map_text",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.TextLoad,Mapdemo.Schema.TextMeasureMessages,(v,b)=>Mapdemo.Schema.TextSaveMessages(v,b),b=>Mapdemo.Schema.TextLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.TextLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.TextSave,Mapdemo.Schema.TextMeasureMessages,Mapdemo.Schema.TextSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Cells,Mapdemo.TableReport>("map_cells",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.CellsLoad,Mapdemo.Schema.CellsMeasureMessages,(v,b)=>Mapdemo.Schema.CellsSaveMessages(v,b),b=>Mapdemo.Schema.CellsLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.CellsLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.CellsSave,Mapdemo.Schema.CellsMeasureMessages,Mapdemo.Schema.CellsSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Runs,Mapdemo.TableReport>("map_runs",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.RunsLoad,Mapdemo.Schema.RunsMeasureMessages,(v,b)=>Mapdemo.Schema.RunsSaveMessages(v,b),b=>Mapdemo.Schema.RunsLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.RunsLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.RunsSave,Mapdemo.Schema.RunsMeasureMessages,Mapdemo.Schema.RunsSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Slots,Mapdemo.TableReport>("map_slots",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.SlotsLoad,Mapdemo.Schema.SlotsMeasureMessages,(v,b)=>Mapdemo.Schema.SlotsSaveMessages(v,b),b=>Mapdemo.Schema.SlotsLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.SlotsLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.SlotsSave,Mapdemo.Schema.SlotsMeasureMessages,Mapdemo.Schema.SlotsSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Spans,Mapdemo.TableReport>("map_spans",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.SpansLoad,Mapdemo.Schema.SpansMeasureMessages,(v,b)=>Mapdemo.Schema.SpansSaveMessages(v,b),b=>Mapdemo.Schema.SpansLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.SpansLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.SpansSave,Mapdemo.Schema.SpansMeasureMessages,Mapdemo.Schema.SpansSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Docs,Mapdemo.TableReport>("map_docs",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.DocsLoad,Mapdemo.Schema.DocsMeasureMessages,(v,b)=>Mapdemo.Schema.DocsSaveMessages(v,b),b=>Mapdemo.Schema.DocsLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.DocsLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.DocsSave,Mapdemo.Schema.DocsMeasureMessages,Mapdemo.Schema.DocsSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Chunks,Mapdemo.TableReport>("map_chunks",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.ChunksLoad,Mapdemo.Schema.ChunksMeasureMessages,(v,b)=>Mapdemo.Schema.ChunksSaveMessages(v,b),b=>Mapdemo.Schema.ChunksLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.ChunksLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.ChunksSave,Mapdemo.Schema.ChunksMeasureMessages,Mapdemo.Schema.ChunksSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Pairs,Mapdemo.TableReport>("map_pairs",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.PairsLoad,Mapdemo.Schema.PairsMeasureMessages,(v,b)=>Mapdemo.Schema.PairsSaveMessages(v,b),b=>Mapdemo.Schema.PairsLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.PairsLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.PairsSave,Mapdemo.Schema.PairsMeasureMessages,Mapdemo.Schema.PairsSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Crews,Mapdemo.TableReport>("map_crews",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.CrewsLoad,Mapdemo.Schema.CrewsMeasureMessages,(v,b)=>Mapdemo.Schema.CrewsSaveMessages(v,b),b=>Mapdemo.Schema.CrewsLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.CrewsLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.CrewsSave,Mapdemo.Schema.CrewsMeasureMessages,Mapdemo.Schema.CrewsSaveMessages);
        }
        {
            var vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),new Mapdemo.TableReport());
            NativeMessageCase<Mapdemo.Trails,Mapdemo.TableReport>("map_trails",()=>new Mapdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Mapdemo.Schema.TrailsLoad,Mapdemo.Schema.TrailsMeasureMessages,(v,b)=>Mapdemo.Schema.TrailsSaveMessages(v,b),b=>Mapdemo.Schema.TrailsLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Mapdemo.TableReport r,out int count)=>Mapdemo.Schema.TrailsLoadMessages(p,n,roots,b,vocabulary,r,out count)==Mapdemo.Schema.TableWire.Verdict.Ok,Mapdemo.Schema.TrailsSave,Mapdemo.Schema.TrailsMeasureMessages,Mapdemo.Schema.TrailsSaveMessages);
        }
        {
            var vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,Graphdemo.Schema.Announce(),new Graphdemo.TableReport());
            NativeMessageCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_shared",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Graphdemo.Schema.SceneLoad,Graphdemo.Schema.SceneMeasureMessages,(v,b)=>Graphdemo.Schema.SceneSaveMessages(v,b),b=>Graphdemo.Schema.SceneLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Graphdemo.TableReport r,out int count)=>Graphdemo.Schema.SceneLoadMessages(p,n,roots,b,vocabulary,r,out count)==Graphdemo.Schema.TableWire.Verdict.Ok,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasureMessages,Graphdemo.Schema.SceneSaveMessages);
        }
        {
            var vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,Graphdemo.Schema.Announce(),new Graphdemo.TableReport());
            NativeMessageCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_tree",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Graphdemo.Schema.SceneLoad,Graphdemo.Schema.SceneMeasureMessages,(v,b)=>Graphdemo.Schema.SceneSaveMessages(v,b),b=>Graphdemo.Schema.SceneLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Graphdemo.TableReport r,out int count)=>Graphdemo.Schema.SceneLoadMessages(p,n,roots,b,vocabulary,r,out count)==Graphdemo.Schema.TableWire.Verdict.Ok,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasureMessages,Graphdemo.Schema.SceneSaveMessages);
        }
        {
            var vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,Graphdemo.Schema.Announce(),new Graphdemo.TableReport());
            NativeMessageCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_empty",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Graphdemo.Schema.SceneLoad,Graphdemo.Schema.SceneMeasureMessages,(v,b)=>Graphdemo.Schema.SceneSaveMessages(v,b),b=>Graphdemo.Schema.SceneLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Graphdemo.TableReport r,out int count)=>Graphdemo.Schema.SceneLoadMessages(p,n,roots,b,vocabulary,r,out count)==Graphdemo.Schema.TableWire.Verdict.Ok,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasureMessages,Graphdemo.Schema.SceneSaveMessages);
        }
        {
            var vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,Graphdemo.Schema.Announce(),new Graphdemo.TableReport());
            NativeMessageCase<Graphdemo.Scene,Graphdemo.TableReport>("graph_deep",()=>new Graphdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Graphdemo.Schema.SceneLoad,Graphdemo.Schema.SceneMeasureMessages,(v,b)=>Graphdemo.Schema.SceneSaveMessages(v,b),b=>Graphdemo.Schema.SceneLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Graphdemo.TableReport r,out int count)=>Graphdemo.Schema.SceneLoadMessages(p,n,roots,b,vocabulary,r,out count)==Graphdemo.Schema.TableWire.Verdict.Ok,Graphdemo.Schema.SceneSave,Graphdemo.Schema.SceneMeasureMessages,Graphdemo.Schema.SceneSaveMessages);
        }
        {
            var vocabulary=new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(vocabulary,Streamdemo.Schema.Announce(),new Streamdemo.TableReport());
            NativeMessageCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_chain",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Streamdemo.Schema.FeedLoad,Streamdemo.Schema.FeedMeasureMessages,(v,b)=>Streamdemo.Schema.FeedSaveMessages(v,b),b=>Streamdemo.Schema.FeedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Streamdemo.TableReport r,out int count)=>Streamdemo.Schema.FeedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Streamdemo.Schema.TableWire.Verdict.Ok,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasureMessages,Streamdemo.Schema.FeedSaveMessages);
        }
        {
            var vocabulary=new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(vocabulary,Streamdemo.Schema.Announce(),new Streamdemo.TableReport());
            NativeMessageCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_header",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Streamdemo.Schema.FeedLoad,Streamdemo.Schema.FeedMeasureMessages,(v,b)=>Streamdemo.Schema.FeedSaveMessages(v,b),b=>Streamdemo.Schema.FeedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Streamdemo.TableReport r,out int count)=>Streamdemo.Schema.FeedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Streamdemo.Schema.TableWire.Verdict.Ok,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasureMessages,Streamdemo.Schema.FeedSaveMessages);
        }
        {
            var vocabulary=new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(vocabulary,Streamdemo.Schema.Announce(),new Streamdemo.TableReport());
            NativeMessageCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_parts",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Streamdemo.Schema.FeedLoad,Streamdemo.Schema.FeedMeasureMessages,(v,b)=>Streamdemo.Schema.FeedSaveMessages(v,b),b=>Streamdemo.Schema.FeedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Streamdemo.TableReport r,out int count)=>Streamdemo.Schema.FeedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Streamdemo.Schema.TableWire.Verdict.Ok,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasureMessages,Streamdemo.Schema.FeedSaveMessages);
        }
        {
            var vocabulary=new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(vocabulary,Streamdemo.Schema.Announce(),new Streamdemo.TableReport());
            NativeMessageCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_arm_first",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Streamdemo.Schema.FeedLoad,Streamdemo.Schema.FeedMeasureMessages,(v,b)=>Streamdemo.Schema.FeedSaveMessages(v,b),b=>Streamdemo.Schema.FeedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Streamdemo.TableReport r,out int count)=>Streamdemo.Schema.FeedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Streamdemo.Schema.TableWire.Verdict.Ok,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasureMessages,Streamdemo.Schema.FeedSaveMessages);
        }
        {
            var vocabulary=new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(vocabulary,Streamdemo.Schema.Announce(),new Streamdemo.TableReport());
            NativeMessageCase<Streamdemo.Feed,Streamdemo.TableReport>("stream_arm_pointer",()=>new Streamdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Streamdemo.Schema.FeedLoad,Streamdemo.Schema.FeedMeasureMessages,(v,b)=>Streamdemo.Schema.FeedSaveMessages(v,b),b=>Streamdemo.Schema.FeedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Streamdemo.TableReport r,out int count)=>Streamdemo.Schema.FeedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Streamdemo.Schema.TableWire.Verdict.Ok,Streamdemo.Schema.FeedSave,Streamdemo.Schema.FeedMeasureMessages,Streamdemo.Schema.FeedSaveMessages);
        }
        {
            var vocabulary=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(vocabulary,Blobdemo.Schema.Announce(),new Blobdemo.TableReport());
            NativeMessageCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_small",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Blobdemo.Schema.CatalogLoad,Blobdemo.Schema.CatalogMeasureMessages,(v,b)=>Blobdemo.Schema.CatalogSaveMessages(v,b),b=>Blobdemo.Schema.CatalogLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Blobdemo.TableReport r,out int count)=>Blobdemo.Schema.CatalogLoadMessages(p,n,roots,b,vocabulary,r,out count)==Blobdemo.Schema.TableWire.Verdict.Ok,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasureMessages,Blobdemo.Schema.CatalogSaveMessages);
        }
        {
            var vocabulary=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(vocabulary,Blobdemo.Schema.Announce(),new Blobdemo.TableReport());
            NativeMessageCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_empty",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Blobdemo.Schema.CatalogLoad,Blobdemo.Schema.CatalogMeasureMessages,(v,b)=>Blobdemo.Schema.CatalogSaveMessages(v,b),b=>Blobdemo.Schema.CatalogLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Blobdemo.TableReport r,out int count)=>Blobdemo.Schema.CatalogLoadMessages(p,n,roots,b,vocabulary,r,out count)==Blobdemo.Schema.TableWire.Verdict.Ok,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasureMessages,Blobdemo.Schema.CatalogSaveMessages);
        }
        {
            var vocabulary=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(vocabulary,Blobdemo.Schema.Announce(),new Blobdemo.TableReport());
            NativeMessageCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_large",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Blobdemo.Schema.CatalogLoad,Blobdemo.Schema.CatalogMeasureMessages,(v,b)=>Blobdemo.Schema.CatalogSaveMessages(v,b),b=>Blobdemo.Schema.CatalogLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Blobdemo.TableReport r,out int count)=>Blobdemo.Schema.CatalogLoadMessages(p,n,roots,b,vocabulary,r,out count)==Blobdemo.Schema.TableWire.Verdict.Ok,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasureMessages,Blobdemo.Schema.CatalogSaveMessages);
        }
        {
            var vocabulary=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(vocabulary,Blobdemo.Schema.Announce(),new Blobdemo.TableReport());
            NativeMessageCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_shared",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Blobdemo.Schema.CatalogLoad,Blobdemo.Schema.CatalogMeasureMessages,(v,b)=>Blobdemo.Schema.CatalogSaveMessages(v,b),b=>Blobdemo.Schema.CatalogLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Blobdemo.TableReport r,out int count)=>Blobdemo.Schema.CatalogLoadMessages(p,n,roots,b,vocabulary,r,out count)==Blobdemo.Schema.TableWire.Verdict.Ok,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasureMessages,Blobdemo.Schema.CatalogSaveMessages);
        }
        {
            var vocabulary=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(vocabulary,Blobdemo.Schema.Announce(),new Blobdemo.TableReport());
            NativeMessageCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_str8",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Blobdemo.Schema.CatalogLoad,Blobdemo.Schema.CatalogMeasureMessages,(v,b)=>Blobdemo.Schema.CatalogSaveMessages(v,b),b=>Blobdemo.Schema.CatalogLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Blobdemo.TableReport r,out int count)=>Blobdemo.Schema.CatalogLoadMessages(p,n,roots,b,vocabulary,r,out count)==Blobdemo.Schema.TableWire.Verdict.Ok,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasureMessages,Blobdemo.Schema.CatalogSaveMessages);
        }
        {
            var vocabulary=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(vocabulary,Blobdemo.Schema.Announce(),new Blobdemo.TableReport());
            NativeMessageCase<Blobdemo.Catalog,Blobdemo.TableReport>("blob_str16",()=>new Blobdemo.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Blobdemo.Schema.CatalogLoad,Blobdemo.Schema.CatalogMeasureMessages,(v,b)=>Blobdemo.Schema.CatalogSaveMessages(v,b),b=>Blobdemo.Schema.CatalogLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Blobdemo.TableReport r,out int count)=>Blobdemo.Schema.CatalogLoadMessages(p,n,roots,b,vocabulary,r,out count)==Blobdemo.Schema.TableWire.Verdict.Ok,Blobdemo.Schema.CatalogSave,Blobdemo.Schema.CatalogMeasureMessages,Blobdemo.Schema.CatalogSaveMessages);
        }
        {
            var vocabulary=new Tblg1.TableVocabulary(); Tblg1.Schema.AnnounceRead(vocabulary,Tblg1.Schema.Announce(),new Tblg1.TableReport());
            NativeMessageCase<Tblg1.Guarded,Tblg1.TableReport>("guard_false_no_edge",()=>new Tblg1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Tblg1.Schema.GuardedLoad,Tblg1.Schema.GuardedMeasureMessages,(v,b)=>Tblg1.Schema.GuardedSaveMessages(v,b),b=>Tblg1.Schema.GuardedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Tblg1.TableReport r,out int count)=>Tblg1.Schema.GuardedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Tblg1.Schema.TableWire.Verdict.Ok,Tblg1.Schema.GuardedSave,Tblg1.Schema.GuardedMeasureMessages,Tblg1.Schema.GuardedSaveMessages);
        }
        {
            var vocabulary=new Tblg1.TableVocabulary(); Tblg1.Schema.AnnounceRead(vocabulary,Tblg1.Schema.Announce(),new Tblg1.TableReport());
            NativeMessageCase<Tblg1.Guarded,Tblg1.TableReport>("guard_true_both_edges",()=>new Tblg1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Tblg1.Schema.GuardedLoad,Tblg1.Schema.GuardedMeasureMessages,(v,b)=>Tblg1.Schema.GuardedSaveMessages(v,b),b=>Tblg1.Schema.GuardedLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Tblg1.TableReport r,out int count)=>Tblg1.Schema.GuardedLoadMessages(p,n,roots,b,vocabulary,r,out count)==Tblg1.Schema.TableWire.Verdict.Ok,Tblg1.Schema.GuardedSave,Tblg1.Schema.GuardedMeasureMessages,Tblg1.Schema.GuardedSaveMessages);
        }
        {
            var vocabulary=new Tblw1.TableVocabulary(); Tblw1.Schema.AnnounceRead(vocabulary,Tblw1.Schema.Announce(),new Tblw1.TableReport());
            NativeMessageCase<Tblw1.Fleet,Tblw1.TableReport>("w1_fleet",()=>new Tblw1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Tblw1.Schema.FleetLoad,Tblw1.Schema.FleetMeasureMessages,(v,b)=>Tblw1.Schema.FleetSaveMessages(v,b),b=>Tblw1.Schema.FleetLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Tblw1.TableReport r,out int count)=>Tblw1.Schema.FleetLoadMessages(p,n,roots,b,vocabulary,r,out count)==Tblw1.Schema.TableWire.Verdict.Ok,Tblw1.Schema.FleetSave,Tblw1.Schema.FleetMeasureMessages,Tblw1.Schema.FleetSaveMessages);
        }
        {
            var vocabulary=new Tblw1.TableVocabulary(); Tblw1.Schema.AnnounceRead(vocabulary,Tblw1.Schema.Announce(),new Tblw1.TableReport());
            NativeMessageCase<Tblw1.Fleet,Tblw1.TableReport>("w1_fleet_default",()=>new Tblw1.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Tblw1.Schema.FleetLoad,Tblw1.Schema.FleetMeasureMessages,(v,b)=>Tblw1.Schema.FleetSaveMessages(v,b),b=>Tblw1.Schema.FleetLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Tblw1.TableReport r,out int count)=>Tblw1.Schema.FleetLoadMessages(p,n,roots,b,vocabulary,r,out count)==Tblw1.Schema.TableWire.Verdict.Ok,Tblw1.Schema.FleetSave,Tblw1.Schema.FleetMeasureMessages,Tblw1.Schema.FleetSaveMessages);
        }
        {
            var vocabulary=new Tblw2.TableVocabulary(); Tblw2.Schema.AnnounceRead(vocabulary,Tblw2.Schema.Announce(),new Tblw2.TableReport());
            NativeMessageCase<Tblw2.Fleet,Tblw2.TableReport>("w2_fleet",()=>new Tblw2.TableReport(),r=>!r.Malformed && !r.Refused && r.Unknown==0 && r.KindMismatch==0 && r.Clamped==0 && r.Widened==0 && r.Duplicate==0,
                Tblw2.Schema.FleetLoad,Tblw2.Schema.FleetMeasureMessages,(v,b)=>Tblw2.Schema.FleetSaveMessages(v,b),b=>Tblw2.Schema.FleetLoadMeasure(vocabulary,b),
                (IntPtr p,long n,Span<IntPtr> roots,ReadOnlySpan<byte> b,Tblw2.TableReport r,out int count)=>Tblw2.Schema.FleetLoadMessages(p,n,roots,b,vocabulary,r,out count)==Tblw2.Schema.TableWire.Verdict.Ok,Tblw2.Schema.FleetSave,Tblw2.Schema.FleetMeasureMessages,Tblw2.Schema.FleetSaveMessages);
        }
    }
}
