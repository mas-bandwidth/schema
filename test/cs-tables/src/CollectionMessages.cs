using System;

static partial class Program
{
    delegate void CollectionMessageLoad<T,R>(T[] values, ReadOnlySpan<byte> bytes, R report);
    delegate long CollectionMessageSave<T>(T[] values, Span<byte> bytes);
    static void MessageCollection<T,R>(string name, Func<R> report, Func<R,bool> silent,
        CollectionLoad<T,R> fileLoad, Func<T,long> fileMeasure, CollectionSave<T> fileSave,
        Func<T[],long> measure, CollectionMessageSave<T> save, CollectionMessageLoad<T,R> load) where T:new()
    {
        byte[] golden = ReadGolden(name); var value = new T(); var r = report();
        Check(fileLoad(value,golden,r) && silent(r),name+" message fixture loads");
        T[] values = {value}; long n = measure(values); Check(n >= 0,name+" message measure");
        if(n<0) { return; }
        byte[] bytes = new byte[n]; Check(save(values,bytes)==n,name+" message size agrees");
        T[] target = {new T()}; r = report(); load(target,bytes,r);
        Check(silent(r),name+" message roundtrip report");
        n=fileMeasure(target[0]); Check(n==golden.Length,name+" message preserves file length");
        if(n!=golden.Length) { return; }
        byte[] back = new byte[n]; Check(fileSave(target[0],back)==n && back.AsSpan().SequenceEqual(golden),name+" message preserves reference file bytes");
    }
    static void TestCollectionMessages()
    {
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Save,Listdemo.TableReport>("list_tables", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave, Listdemo.Schema.SaveMeasureMessages,
                (values,bytes) => Listdemo.Schema.SaveSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.SaveLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Save,Listdemo.TableReport>("list_scalars", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave, Listdemo.Schema.SaveMeasureMessages,
                (values,bytes) => Listdemo.Schema.SaveSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.SaveLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Save,Listdemo.TableReport>("list_empty", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave, Listdemo.Schema.SaveMeasureMessages,
                (values,bytes) => Listdemo.Schema.SaveSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.SaveLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Save,Listdemo.TableReport>("list_erased", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave, Listdemo.Schema.SaveMeasureMessages,
                (values,bytes) => Listdemo.Schema.SaveSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.SaveLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Album,Listdemo.TableReport>("list_shared", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.AlbumLoad, Listdemo.Schema.AlbumMeasure, Listdemo.Schema.AlbumSave, Listdemo.Schema.AlbumMeasureMessages,
                (values,bytes) => Listdemo.Schema.AlbumSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.AlbumLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Mixed,Listdemo.TableReport>("list_mixed", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.MixedLoad, Listdemo.Schema.MixedMeasure, Listdemo.Schema.MixedSave, Listdemo.Schema.MixedMeasureMessages,
                (values,bytes) => Listdemo.Schema.MixedSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.MixedLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Album,Listdemo.TableReport>("list_before_pointer", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.AlbumLoad, Listdemo.Schema.AlbumMeasure, Listdemo.Schema.AlbumSave, Listdemo.Schema.AlbumMeasureMessages,
                (values,bytes) => Listdemo.Schema.AlbumSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.AlbumLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Sheet,Listdemo.TableReport>("list_nested", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.SheetLoad, Listdemo.Schema.SheetMeasure, Listdemo.Schema.SheetSave, Listdemo.Schema.SheetMeasureMessages,
                (values,bytes) => Listdemo.Schema.SheetSaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.SheetLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Listdemo.TableVocabulary(); var report = new Listdemo.TableReport();
            Listdemo.Schema.AnnounceRead(vocabulary,Listdemo.Schema.Announce(),report);
            MessageCollection<Listdemo.Army,Listdemo.TableReport>("list_of_maps", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Listdemo.Schema.ArmyLoad, Listdemo.Schema.ArmyMeasure, Listdemo.Schema.ArmySave, Listdemo.Schema.ArmyMeasureMessages,
                (values,bytes) => Listdemo.Schema.ArmySaveMessages(values,bytes), (values,bytes,r) => { Listdemo.Schema.ArmyLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Fleet,Mapdemo.TableReport>("map_full", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.FleetLoad, Mapdemo.Schema.FleetMeasure, Mapdemo.Schema.FleetSave, Mapdemo.Schema.FleetMeasureMessages,
                (values,bytes) => Mapdemo.Schema.FleetSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.FleetLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Fleet,Mapdemo.TableReport>("map_empty", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.FleetLoad, Mapdemo.Schema.FleetMeasure, Mapdemo.Schema.FleetSave, Mapdemo.Schema.FleetMeasureMessages,
                (values,bytes) => Mapdemo.Schema.FleetSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.FleetLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Depth,Mapdemo.TableReport>("map_depth", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.DepthLoad, Mapdemo.Schema.DepthMeasure, Mapdemo.Schema.DepthSave, Mapdemo.Schema.DepthMeasureMessages,
                (values,bytes) => Mapdemo.Schema.DepthSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.DepthLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Text,Mapdemo.TableReport>("map_text", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.TextLoad, Mapdemo.Schema.TextMeasure, Mapdemo.Schema.TextSave, Mapdemo.Schema.TextMeasureMessages,
                (values,bytes) => Mapdemo.Schema.TextSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.TextLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Cells,Mapdemo.TableReport>("map_cells", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.CellsLoad, Mapdemo.Schema.CellsMeasure, Mapdemo.Schema.CellsSave, Mapdemo.Schema.CellsMeasureMessages,
                (values,bytes) => Mapdemo.Schema.CellsSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.CellsLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Runs,Mapdemo.TableReport>("map_runs", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.RunsLoad, Mapdemo.Schema.RunsMeasure, Mapdemo.Schema.RunsSave, Mapdemo.Schema.RunsMeasureMessages,
                (values,bytes) => Mapdemo.Schema.RunsSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.RunsLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Slots,Mapdemo.TableReport>("map_slots", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.SlotsLoad, Mapdemo.Schema.SlotsMeasure, Mapdemo.Schema.SlotsSave, Mapdemo.Schema.SlotsMeasureMessages,
                (values,bytes) => Mapdemo.Schema.SlotsSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.SlotsLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Spans,Mapdemo.TableReport>("map_spans", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.SpansLoad, Mapdemo.Schema.SpansMeasure, Mapdemo.Schema.SpansSave, Mapdemo.Schema.SpansMeasureMessages,
                (values,bytes) => Mapdemo.Schema.SpansSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.SpansLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Docs,Mapdemo.TableReport>("map_docs", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.DocsLoad, Mapdemo.Schema.DocsMeasure, Mapdemo.Schema.DocsSave, Mapdemo.Schema.DocsMeasureMessages,
                (values,bytes) => Mapdemo.Schema.DocsSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.DocsLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Chunks,Mapdemo.TableReport>("map_chunks", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.ChunksLoad, Mapdemo.Schema.ChunksMeasure, Mapdemo.Schema.ChunksSave, Mapdemo.Schema.ChunksMeasureMessages,
                (values,bytes) => Mapdemo.Schema.ChunksSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.ChunksLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Pairs,Mapdemo.TableReport>("map_pairs", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.PairsLoad, Mapdemo.Schema.PairsMeasure, Mapdemo.Schema.PairsSave, Mapdemo.Schema.PairsMeasureMessages,
                (values,bytes) => Mapdemo.Schema.PairsSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.PairsLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Crews,Mapdemo.TableReport>("map_crews", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.CrewsLoad, Mapdemo.Schema.CrewsMeasure, Mapdemo.Schema.CrewsSave, Mapdemo.Schema.CrewsMeasureMessages,
                (values,bytes) => Mapdemo.Schema.CrewsSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.CrewsLoadMessages(values,bytes,vocabulary,r,out _); });
        }
        {
            var vocabulary = new Mapdemo.TableVocabulary(); var report = new Mapdemo.TableReport();
            Mapdemo.Schema.AnnounceRead(vocabulary,Mapdemo.Schema.Announce(),report);
            MessageCollection<Mapdemo.Trails,Mapdemo.TableReport>("map_trails", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
                Mapdemo.Schema.TrailsLoad, Mapdemo.Schema.TrailsMeasure, Mapdemo.Schema.TrailsSave, Mapdemo.Schema.TrailsMeasureMessages,
                (values,bytes) => Mapdemo.Schema.TrailsSaveMessages(values,bytes), (values,bytes,r) => { Mapdemo.Schema.TrailsLoadMessages(values,bytes,vocabulary,r,out _); });
        }
    }
}
