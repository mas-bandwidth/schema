using System;

static partial class Program
{
    delegate bool CollectionLoad<T, R>(T value, ReadOnlySpan<byte> bytes, R report);
    delegate long CollectionSave<T>(T value, Span<byte> bytes);
    static void Collection<T, R>(string name, Func<R> report, Func<R, bool> silent, CollectionLoad<T, R> load, Func<T, long> measure, CollectionSave<T> save,
        CollectionLoad<T, R> fromJson, Func<T, long> jsonMeasure, CollectionSave<T> toJson) where T : new()
    {
        byte[] golden = ReadGolden(name); T value = new T(); R r = report();
        Check(load(value, golden, r) && silent(r), name + " reference wire loads silently");
        long n = measure(value); Check(n == golden.Length, name + " measured wire length");
        if (n < 0 || n > int.MaxValue) { return; }
        byte[] bytes = new byte[(int)n]; Check(save(value, bytes) == n && bytes.AsSpan().SequenceEqual(golden), name + " re-encodes reference bytes");
        n = jsonMeasure(value); Check(n >= 0, name + " text is representable");
        if (n < 0 || n > int.MaxValue) { return; }
        byte[] json = new byte[(int)n]; Check(toJson(value, json) == n, name + " text measure agrees");
        T copy = new T(); r = report(); Check(fromJson(copy, json, r) && silent(r), name + " text loads silently");
        n = measure(copy); Check(n == golden.Length, name + " text roundtrip measure");
        if (n != golden.Length) { return; }
        Check(save(copy, bytes) == n && bytes.AsSpan().SequenceEqual(golden), name + " text preserves wire values and sharing");
    }
    static void TestCollections()
    {
        var damagedDefault = new Tblw1.Fleet();
        var damage = new Tblw1.TableReport();
        byte[] invalidTitle = Fixture(new byte[] { 1, 12, 1, 0xff, 0 }, "title");
        Check(Tblw1.Schema.FleetLoad(damagedDefault, invalidTitle, damage) && damage.Malformed && damagedDefault.TitleLength == 0,
            "an invalid text field clears its storage even when its declared default is nonempty");
        Collection<Listdemo.Save, Listdemo.TableReport>("list_tables", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave,
            Listdemo.Schema.SaveFromJson, Listdemo.Schema.SaveToJsonMeasure, Listdemo.Schema.SaveToJson);
        Collection<Listdemo.Save, Listdemo.TableReport>("list_scalars", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave,
            Listdemo.Schema.SaveFromJson, Listdemo.Schema.SaveToJsonMeasure, Listdemo.Schema.SaveToJson);
        Collection<Listdemo.Save, Listdemo.TableReport>("list_empty", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave,
            Listdemo.Schema.SaveFromJson, Listdemo.Schema.SaveToJsonMeasure, Listdemo.Schema.SaveToJson);
        Collection<Listdemo.Save, Listdemo.TableReport>("list_erased", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave,
            Listdemo.Schema.SaveFromJson, Listdemo.Schema.SaveToJsonMeasure, Listdemo.Schema.SaveToJson);
        Collection<Listdemo.Album, Listdemo.TableReport>("list_shared", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.AlbumLoad, Listdemo.Schema.AlbumMeasure, Listdemo.Schema.AlbumSave,
            Listdemo.Schema.AlbumFromJson, Listdemo.Schema.AlbumToJsonMeasure, Listdemo.Schema.AlbumToJson);
        Collection<Listdemo.Mixed, Listdemo.TableReport>("list_mixed", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.MixedLoad, Listdemo.Schema.MixedMeasure, Listdemo.Schema.MixedSave,
            Listdemo.Schema.MixedFromJson, Listdemo.Schema.MixedToJsonMeasure, Listdemo.Schema.MixedToJson);
        Collection<Listdemo.Album, Listdemo.TableReport>("list_before_pointer", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.AlbumLoad, Listdemo.Schema.AlbumMeasure, Listdemo.Schema.AlbumSave,
            Listdemo.Schema.AlbumFromJson, Listdemo.Schema.AlbumToJsonMeasure, Listdemo.Schema.AlbumToJson);
        Collection<Listdemo.Sheet, Listdemo.TableReport>("list_nested", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.SheetLoad, Listdemo.Schema.SheetMeasure, Listdemo.Schema.SheetSave,
            Listdemo.Schema.SheetFromJson, Listdemo.Schema.SheetToJsonMeasure, Listdemo.Schema.SheetToJson);
        Collection<Listdemo.Army, Listdemo.TableReport>("list_of_maps", () => new Listdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Listdemo.Schema.ArmyLoad, Listdemo.Schema.ArmyMeasure, Listdemo.Schema.ArmySave,
            Listdemo.Schema.ArmyFromJson, Listdemo.Schema.ArmyToJsonMeasure, Listdemo.Schema.ArmyToJson);
        Collection<Mapdemo.Fleet, Mapdemo.TableReport>("map_full", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.FleetLoad, Mapdemo.Schema.FleetMeasure, Mapdemo.Schema.FleetSave,
            Mapdemo.Schema.FleetFromJson, Mapdemo.Schema.FleetToJsonMeasure, Mapdemo.Schema.FleetToJson);
        Collection<Mapdemo.Fleet, Mapdemo.TableReport>("map_empty", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.FleetLoad, Mapdemo.Schema.FleetMeasure, Mapdemo.Schema.FleetSave,
            Mapdemo.Schema.FleetFromJson, Mapdemo.Schema.FleetToJsonMeasure, Mapdemo.Schema.FleetToJson);
        Collection<Mapdemo.Depth, Mapdemo.TableReport>("map_depth", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.DepthLoad, Mapdemo.Schema.DepthMeasure, Mapdemo.Schema.DepthSave,
            Mapdemo.Schema.DepthFromJson, Mapdemo.Schema.DepthToJsonMeasure, Mapdemo.Schema.DepthToJson);
        Collection<Mapdemo.Text, Mapdemo.TableReport>("map_text", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.TextLoad, Mapdemo.Schema.TextMeasure, Mapdemo.Schema.TextSave,
            Mapdemo.Schema.TextFromJson, Mapdemo.Schema.TextToJsonMeasure, Mapdemo.Schema.TextToJson);
        Collection<Mapdemo.Cells, Mapdemo.TableReport>("map_cells", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.CellsLoad, Mapdemo.Schema.CellsMeasure, Mapdemo.Schema.CellsSave,
            Mapdemo.Schema.CellsFromJson, Mapdemo.Schema.CellsToJsonMeasure, Mapdemo.Schema.CellsToJson);
        Collection<Mapdemo.Runs, Mapdemo.TableReport>("map_runs", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.RunsLoad, Mapdemo.Schema.RunsMeasure, Mapdemo.Schema.RunsSave,
            Mapdemo.Schema.RunsFromJson, Mapdemo.Schema.RunsToJsonMeasure, Mapdemo.Schema.RunsToJson);
        Collection<Mapdemo.Slots, Mapdemo.TableReport>("map_slots", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.SlotsLoad, Mapdemo.Schema.SlotsMeasure, Mapdemo.Schema.SlotsSave,
            Mapdemo.Schema.SlotsFromJson, Mapdemo.Schema.SlotsToJsonMeasure, Mapdemo.Schema.SlotsToJson);
        Collection<Mapdemo.Spans, Mapdemo.TableReport>("map_spans", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.SpansLoad, Mapdemo.Schema.SpansMeasure, Mapdemo.Schema.SpansSave,
            Mapdemo.Schema.SpansFromJson, Mapdemo.Schema.SpansToJsonMeasure, Mapdemo.Schema.SpansToJson);
        Collection<Mapdemo.Docs, Mapdemo.TableReport>("map_docs", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.DocsLoad, Mapdemo.Schema.DocsMeasure, Mapdemo.Schema.DocsSave,
            Mapdemo.Schema.DocsFromJson, Mapdemo.Schema.DocsToJsonMeasure, Mapdemo.Schema.DocsToJson);
        Collection<Mapdemo.Chunks, Mapdemo.TableReport>("map_chunks", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.ChunksLoad, Mapdemo.Schema.ChunksMeasure, Mapdemo.Schema.ChunksSave,
            Mapdemo.Schema.ChunksFromJson, Mapdemo.Schema.ChunksToJsonMeasure, Mapdemo.Schema.ChunksToJson);
        Collection<Mapdemo.Pairs, Mapdemo.TableReport>("map_pairs", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.PairsLoad, Mapdemo.Schema.PairsMeasure, Mapdemo.Schema.PairsSave,
            Mapdemo.Schema.PairsFromJson, Mapdemo.Schema.PairsToJsonMeasure, Mapdemo.Schema.PairsToJson);
        Collection<Mapdemo.Crews, Mapdemo.TableReport>("map_crews", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.CrewsLoad, Mapdemo.Schema.CrewsMeasure, Mapdemo.Schema.CrewsSave,
            Mapdemo.Schema.CrewsFromJson, Mapdemo.Schema.CrewsToJsonMeasure, Mapdemo.Schema.CrewsToJson);
        Collection<Mapdemo.Trails, Mapdemo.TableReport>("map_trails", () => new Mapdemo.TableReport(), r => !r.Malformed && !r.Refused && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Widened == 0 && r.Duplicate == 0,
            Mapdemo.Schema.TrailsLoad, Mapdemo.Schema.TrailsMeasure, Mapdemo.Schema.TrailsSave,
            Mapdemo.Schema.TrailsFromJson, Mapdemo.Schema.TrailsToJsonMeasure, Mapdemo.Schema.TrailsToJson);
    }
}
