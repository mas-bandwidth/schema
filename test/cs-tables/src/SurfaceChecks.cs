using System;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Threading.Tasks;

static partial class Program
{
    [System.Runtime.CompilerServices.MethodImpl(System.Runtime.CompilerServices.MethodImplOptions.NoInlining)]
    static long MeasureBlockAllocations(IntPtr pointer,long capacity,in Blockdemo.PaddedFrameCounts counts)
    {
        // Isolate the allocation instrument from Parallel.For's captured state.
        for(int i=0;i<2000;i++) { Blockdemo.PaddedFrameBlock.Begin(out var block,pointer,capacity,in counts,out _); Blockdemo.PaddedFrameBlock.Open(out _,pointer,block.Bytes); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for(int i=0;i<10000;i++) { Blockdemo.PaddedFrameBlock.Begin(out var block,pointer,capacity,in counts,out _); Blockdemo.PaddedFrameBlock.Open(out _,pointer,block.Bytes); }
        return GC.GetAllocatedBytesForCurrentThread()-before;
    }
    [System.Runtime.CompilerServices.MethodImpl(System.Runtime.CompilerServices.MethodImplOptions.NoInlining)]
    static long MeasureCalls(Action walk)
    {
        for(int i=0;i<3000;i++) { walk(); }
        long before=GC.GetAllocatedBytesForCurrentThread();
        for(int i=0;i<10000;i++) { walk(); }
        return GC.GetAllocatedBytesForCurrentThread()-before;
    }
    static unsafe void TestSurfaces()
    {
        // Compile every surface together, then exercise the layouts rather than
        // accepting a successful compile as evidence of native offsets.
        foreach (Type type in Assembly.GetExecutingAssembly().GetTypes())
        {
            if (type.Name == "TableCookLayout" || type.Name == "TableBlockLayout")
            { type.GetMethod("Verify", BindingFlags.Public | BindingFlags.NonPublic | BindingFlags.Static).Invoke(null, null); }
        }
        var orphanView = Csview.Schema.UnitView();
        Check(orphanView.NumTypes == 2 && orphanView.NumTables == 1 && orphanView.NumEnums == 1 && orphanView.NumFlags == 1,"view includes declarations outside the closure");
        var orphan = orphanView.Types.Span[0];
        Check(orphan.Name == "Orphan" && orphan.Doc == "A type no table reaches." && orphan.Tags.Span[0] == "inspected","view preserves outside annotations");
        foreach (var field in orphan.Type.Fields) { Check(field.Id == 0 && field.Json == null,"outside descriptors carry no unchecked wire identity"); }
        Check(orphan.Type.Fields[0].VariantId(1) == 0 && orphan.Type.Fields[3].KeyId(1) == 0,"outside vocabulary id functions return zero");
        var view = Messagedemo.Schema.UnitView();
        Check(ReferenceEquals(view,Messagedemo.Schema.UnitView()),"UnitView is cached");
        Check(view.NumTables > 0 && view.NumTypes == 2 && view.NumUnions == 3 && view.NumConstants == 1,"UnitView carries all six sets");
        string previous = "";
        foreach (var table in view.Tables.Span)
        {
            Check(string.CompareOrdinal(previous,table.Name) < 0 && table.Table && table.Type.Name == table.Name,"UnitView table order and factories");
            previous = table.Name;
        }
        foreach (var union in view.Unions.Span)
        {
            Check(union.Variants.Span[0].Value == 0,"UnitView includes the empty arm");
            foreach (var arm in union.Variants.Span)
            { Check(arm.PayloadName == null || arm.Payload != null || arm.Field != null,"UnitView exposes every arm payload"); }
        }
        Check(Mapdemo.Schema.UnitView().NumTables == 18,"UnitView excludes generated map entry tables");

        int allocated = 0, freed = 0;
        var allocator = new Blockdemo.TableBlockAllocator {
            Allocate = n => { allocated++; return Marshal.AllocHGlobal((nint)n); },
            Free = p => { freed++; Marshal.FreeHGlobal(p); }
        };
        using (var storage = new Blockdemo.PaddedFrameBlockStorage(allocator))
        {
            storage.Clear();
            var counts = new Blockdemo.PaddedFrameCounts { Rows = 64 };
            Check(Blockdemo.PaddedFrameBlock.Begin(out var block,storage,in counts,out _),"block Begin at maximum");
            Check(block.Bytes == Blockdemo.PaddedFrameBlock.BlockMaxBytes,"block max extent");
            block.WritableProjection.Marker = 42;
            Parallel.For(0,64,i => { block.RowsWritableSpan[i].Id = (uint)i; block.RowsWritableSpan[i].Value = i+0.5; });
            Check(Blockdemo.PaddedFrameBlock.Open(out var read,storage.Pointer,block.Bytes,out var reason) && reason == Blockdemo.TableRefuseReason.ok,"built block opens");
            for(int i=0;i<64;i++) { Check(read.RowsSpan[i].Id == (uint)i && read.RowsSpan[i].Value == i+0.5,"parallel block fill"); }
            counts.Rows = 65;
            Check(!Blockdemo.PaddedFrameBlock.Begin(out _,storage,in counts,out var refused) && refused.Array == "rows" && refused.Count == 65 && refused.Maximum == 64,"named block count refusal");
            Check(block.Projection.Marker == 42 && block.RowsSpan.Length == 64,"refused Begin leaves storage untouched");
            counts.Rows = 0;
            Blockdemo.PaddedFrameBlock.Begin(out _,storage,in counts,out _);
            long added = MeasureBlockAllocations(storage.Pointer,storage.Capacity,in counts);
            Check(added == 0,"block Begin and Open allocate zero (observed " + added + " bytes)");
        }
        Check(allocated == 1 && freed == 1,"block allocation hook pair owns one allocation");

        byte[] graphWire = ReadGolden("graph_shared");
        var graph = new Graphdemo.Scene(); var graphReport = new Graphdemo.TableReport();
        Graphdemo.Schema.SceneLoad(graph,graphWire,graphReport);
        var graphBatch = new[] {graph}; byte[] graphMessage = new byte[Graphdemo.Schema.SceneMeasureMessages(graphBatch)];
        Graphdemo.Schema.SceneSaveMessages(graphBatch,graphMessage);
        var graphVocabulary = new Graphdemo.TableVocabulary();
        Graphdemo.Schema.AnnounceRead(graphVocabulary,Graphdemo.Schema.Announce(),graphReport);
        Check(Graphdemo.Schema.SceneLoadMeasure(graphWire) == Graphdemo.Schema.SceneLoadMeasure(graphVocabulary,graphMessage),"file and message measure the same graph region");
        Check(MeasureCalls(() => { Graphdemo.Schema.SceneLoadMeasure(graphWire); }) == 0,"variable file LoadMeasure allocates zero");
        Check(MeasureCalls(() => { Graphdemo.Schema.SceneLoadMeasure(graphVocabulary,graphMessage); }) == 0,"variable message LoadMeasure allocates zero");

        var keyed=new Tabledemo.KeyedConfig(); keyed.Teams[(int)Tabledemo.Team.Red].SpawnCount=9;
        var keyedBatch=new[] {keyed}; byte[] keyedMessage=new byte[Tabledemo.Schema.KeyedConfigMeasureMessages(keyedBatch)];
        Tabledemo.Schema.KeyedConfigSaveMessages(keyedBatch,keyedMessage);
        var keyedVocabulary=new Tabledemo.TableVocabulary(); var keyedReport=new Tabledemo.TableReport();
        Tabledemo.Schema.AnnounceRead(keyedVocabulary,Tabledemo.Schema.Announce(),keyedReport);
        foreach(var entry in keyedVocabulary.Entries) { if(entry.Kind==0 && entry.Id==FieldId("Red")) { entry.Id=FieldId("UnknownTeam"); } }
        var keyedTarget=new[] {new Tabledemo.KeyedConfig()};
        Tabledemo.Schema.KeyedConfigLoadMessages(keyedTarget,keyedMessage,keyedVocabulary,keyedReport,out _);
        Check(!keyedReport.Malformed && keyedReport.Unknown==1 && keyedTarget[0].Teams[(int)Tabledemo.Team.Red].SpawnCount==4,"unknown keyed table body is discarded with its report");
        Check(MeasureCalls(()=> { Tabledemo.Schema.KeyedConfigLoadMessages(keyedTarget,keyedMessage,keyedVocabulary,keyedReport,out _); })==0,"discarding a fixed keyed table body allocates zero");

        var values = new Messagedemo.Cursor[256]; var loaded = new Messagedemo.Cursor[256];
        for(int i=0;i<256;i++) { values[i] = new Messagedemo.Cursor { Line=(uint)i,Column=(uint)(255-i) }; loaded[i] = new Messagedemo.Cursor(); }
        var vocabulary = new Messagedemo.TableVocabulary(); var report = new Messagedemo.TableReport();
        Check(Messagedemo.Schema.AnnounceRead(vocabulary,Messagedemo.Schema.Announce(),report) == Messagedemo.Schema.TableWire.Verdict.Ok,"announcement accepted");
        long n = Messagedemo.Schema.CursorMeasureMessages(values); byte[] bytes = new byte[n];
        Check(Messagedemo.Schema.CursorSaveMessages(values,bytes) == n && bytes[1] == 255,"256 body message framing");
        Check(Messagedemo.Schema.CursorLoadMessages(loaded,bytes,vocabulary,report,out int count) == Messagedemo.Schema.TableWire.Verdict.Ok && count == 256,"256 body batch load");
        for(int i=0;i<256;i++) { Check(loaded[i].Line == (uint)i && loaded[i].Column == (uint)(255-i),"batch body independence"); }
        var tooMany = new Messagedemo.Cursor[257]; var refusedReport = new Messagedemo.TableReport();
        Check(Messagedemo.Schema.CursorSaveMessages(tooMany,bytes,refusedReport) == -1 && refusedReport.Reason == "batch_too_large","named oversized batch refusal");
        var shortReport = new Messagedemo.TableReport(); loaded[0].Line = 777;
        Check(Messagedemo.Schema.CursorLoadMessages(new[] {loaded[0]},bytes,vocabulary,shortReport,out _) == Messagedemo.Schema.TableWire.Verdict.Refused && loaded[0].Line == 777,"batch capacity refused before reading");
        // Warm tiered JIT paths before measuring; callers own all input/output.
        var single = new[] {values[1]}; var target = new[] {loaded[1]}; byte[] small = new byte[Messagedemo.Schema.CursorMeasureMessages(single)];
        byte[] cook = new byte[Messagedemo.Schema.CursorCookMeasure(single[0])];
        Action[] walks = {
            () => { Messagedemo.Schema.CursorMeasureMessages(single); },
            () => { Messagedemo.Schema.CursorSaveMessages(single,small); },
            () => { Messagedemo.Schema.CursorLoadMessages(target,small,vocabulary,report,out _); },
            () => { Messagedemo.Schema.CursorCookMeasure(single[0]); },
            () => { Messagedemo.Schema.CursorCook(single[0],cook); }
        };
        string[] labels = {"message measure","message save","message load","cook measure","cook write"};
        for(int i=0;i<walks.Length;i++) {
            long added = MeasureCalls(walks[i]);
            Check(added == 0,"fixed " + labels[i] + " allocates zero (observed " + added + " bytes)");
        }
    }
}
