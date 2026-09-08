// THE C# CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp, and it is deliberately the same
// shape: one process per surface, every expectation in the data, nothing
// literal here. It answers every surface this backend has, the text form
// (docs/SPEC-TABLES.md §16) included — json-read and json-write moved from
// ABSENT to a registered leg when the C# walk landed.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>

using System;
using System.Collections.Generic;
using System.IO;
using System.Globalization;
using System.Runtime.InteropServices;
using System.Text;

static partial class Program
{
    // ---- the manifest, exactly as testdata/conformance/tables/FORMAT.md states it

    static readonly List<string[]> lines = new List<string[]>();

    static void ReadManifest(string path)
    {
        foreach (string raw in File.ReadAllLines(path))
        {
            string text = raw.Trim();
            if (text.Length == 0 || text[0] == '#')
            {
                continue;
            }
            lines.Add(text.Split((char[])null, StringSplitOptions.RemoveEmptyEntries));
        }
    }

    static IEnumerable<string[]> Kind(string kind)
    {
        foreach (string[] f in lines)
        {
            if (f[0] == kind)
            {
                yield return f;
            }
        }
    }

    // SpillAbsent says this backend cannot answer THIS CASE — a feature it
    // lacks, not a test it failed. The harness counts it and the matrix prints
    // it beside what the leg did answer (test/conformance/README.md).
    static void SpillAbsent(string outDir, string name)
    {
        File.WriteAllBytes(Path.Combine(outDir, name + ".absent"), new byte[0]);
    }

    // NoText marks an instance the corpus carries on the WIRE only — past the
    // text form's depth cap by the form's own rule (docs/SPEC-TABLES.md §16.7)
    // — so no leg is asked for its text.
    static bool NoText(string[] f) { return f.Length > 5 && f[5] == "no-text"; }

    // ---- the codec table: one row per (unit, root) the corpus names

    sealed class Report
    {
        public int Unknown, KindMismatch, Widened, Clamped, Duplicate;
        public bool Refused;
        public bool Malformed;
    }

    sealed class Codec
    {
        public Func<object, long> CookMeasure;
        public Func<object, byte[], bool, bool> Cook;
        public Func<byte[], byte[], Report, object> MessageLoad;
        public Func<object, long> MessageMeasure;
        public Func<object, byte[], long> MessageSave;
        public Func<byte[], long> MessageLoadMeasure;
        public byte[] Announcement;
        public Func<byte[],RetainAnswer> Retain;
        public string Unit;
        public string Root;
        public Func<byte[], Report, object> PartialLoad;
        public Func<byte[], Report, object> Load;   // null on refusal
        public Func<object, long> Measure;
        public LoadMeasureOf LoadMeasure;
        public Func<object, byte[], long> Save;
        // the TEXT form (docs/SPEC-TABLES.md §16), the same three per row
        public Func<byte[], Report, object> FromJson; // null on refusal
        public Func<object, long> ToJsonMeasure;
        public Func<object, byte[], long> ToJson;
    }

    // Each unit declares its own TableReport, so the driver carries one report
    // shape and every row copies into it — the report columns follow the common driver contract.
    static Report Copy(Tabledemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblv1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblv2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblp1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblp3.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }

    static Report Copy(Tblk1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblk2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Messagedemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblm1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblm2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tbla1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tbla2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Scalardemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Scalardemo2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Wide.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Graphdemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Blobdemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblp2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblw1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblw2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Listdemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Mapdemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Streamdemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblg1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Backenddemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Vocabdemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Vocab9demo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblr1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblr2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Widened = r.Widened, Refused = r.Refused, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static readonly List<Codec> codecs = new List<Codec>
    {
        Row<Tblr2.Cfg, Tblr2.TableReport>("tblr2", "Cfg", () => new Tblr2.TableReport(), Copy,
            Tblr2.Schema.CfgLoad, Tblr2.Schema.CfgMeasure, Tblr2.Schema.CfgSave,
            Tblr2.Schema.CfgFromJson, Tblr2.Schema.CfgToJsonMeasure, Tblr2.Schema.CfgToJson),
        Row<Tblr1.Cfg, Tblr1.TableReport>("tblr1", "Cfg", () => new Tblr1.TableReport(), Copy,
            Tblr1.Schema.CfgLoad, Tblr1.Schema.CfgMeasure, Tblr1.Schema.CfgSave,
            Tblr1.Schema.CfgFromJson, Tblr1.Schema.CfgToJsonMeasure, Tblr1.Schema.CfgToJson),
        Row<Vocab9demo.Wide19, Vocab9demo.TableReport>("vocab9demo", "Wide19", () => new Vocab9demo.TableReport(), Copy,
            Vocab9demo.Schema.Wide19Load, Vocab9demo.Schema.Wide19Measure, Vocab9demo.Schema.Wide19Save,
            Vocab9demo.Schema.Wide19FromJson, Vocab9demo.Schema.Wide19ToJsonMeasure, Vocab9demo.Schema.Wide19ToJson),
        Row<Vocab9demo.Wide00, Vocab9demo.TableReport>("vocab9demo", "Wide00", () => new Vocab9demo.TableReport(), Copy,
            Vocab9demo.Schema.Wide00Load, Vocab9demo.Schema.Wide00Measure, Vocab9demo.Schema.Wide00Save,
            Vocab9demo.Schema.Wide00FromJson, Vocab9demo.Schema.Wide00ToJsonMeasure, Vocab9demo.Schema.Wide00ToJson),
        Row<Vocabdemo.Wide09, Vocabdemo.TableReport>("vocabdemo", "Wide09", () => new Vocabdemo.TableReport(), Copy,
            Vocabdemo.Schema.Wide09Load, Vocabdemo.Schema.Wide09Measure, Vocabdemo.Schema.Wide09Save,
            Vocabdemo.Schema.Wide09FromJson, Vocabdemo.Schema.Wide09ToJsonMeasure, Vocabdemo.Schema.Wide09ToJson),
        Row<Vocabdemo.Wide00, Vocabdemo.TableReport>("vocabdemo", "Wide00", () => new Vocabdemo.TableReport(), Copy,
            Vocabdemo.Schema.Wide00Load, Vocabdemo.Schema.Wide00Measure, Vocabdemo.Schema.Wide00Save,
            Vocabdemo.Schema.Wide00FromJson, Vocabdemo.Schema.Wide00ToJsonMeasure, Vocabdemo.Schema.Wide00ToJson),
        Row<Backenddemo.Envelope, Backenddemo.TableReport>("backenddemo", "Envelope", () => new Backenddemo.TableReport(), Copy,
            Backenddemo.Schema.EnvelopeLoad, Backenddemo.Schema.EnvelopeMeasure, Backenddemo.Schema.EnvelopeSave,
            Backenddemo.Schema.EnvelopeFromJson, Backenddemo.Schema.EnvelopeToJsonMeasure, Backenddemo.Schema.EnvelopeToJson),
        Row<Backenddemo.StorePurchase, Backenddemo.TableReport>("backenddemo", "StorePurchase", () => new Backenddemo.TableReport(), Copy,
            Backenddemo.Schema.StorePurchaseLoad, Backenddemo.Schema.StorePurchaseMeasure, Backenddemo.Schema.StorePurchaseSave,
            Backenddemo.Schema.StorePurchaseFromJson, Backenddemo.Schema.StorePurchaseToJsonMeasure, Backenddemo.Schema.StorePurchaseToJson),
        Row<Backenddemo.MatchResult, Backenddemo.TableReport>("backenddemo", "MatchResult", () => new Backenddemo.TableReport(), Copy,
            Backenddemo.Schema.MatchResultLoad, Backenddemo.Schema.MatchResultMeasure, Backenddemo.Schema.MatchResultSave,
            Backenddemo.Schema.MatchResultFromJson, Backenddemo.Schema.MatchResultToJsonMeasure, Backenddemo.Schema.MatchResultToJson),
        Row<Backenddemo.LoginRequest, Backenddemo.TableReport>("backenddemo", "LoginRequest", () => new Backenddemo.TableReport(), Copy,
            Backenddemo.Schema.LoginRequestLoad, Backenddemo.Schema.LoginRequestMeasure, Backenddemo.Schema.LoginRequestSave,
            Backenddemo.Schema.LoginRequestFromJson, Backenddemo.Schema.LoginRequestToJsonMeasure, Backenddemo.Schema.LoginRequestToJson),
        Row<Tblg1.Guarded, Tblg1.TableReport>("tblg1", "Guarded", () => new Tblg1.TableReport(), Copy,
            Tblg1.Schema.GuardedLoad, Tblg1.Schema.GuardedMeasure, Tblg1.Schema.GuardedSave,
            Tblg1.Schema.GuardedFromJson, Tblg1.Schema.GuardedToJsonMeasure, Tblg1.Schema.GuardedToJson, Tblg1.Schema.GuardedLoadMeasure),
        Row<Streamdemo.Feed, Streamdemo.TableReport>("streamdemo", "Feed", () => new Streamdemo.TableReport(), Copy,
            Streamdemo.Schema.FeedLoad, Streamdemo.Schema.FeedMeasure, Streamdemo.Schema.FeedSave,
            Streamdemo.Schema.FeedFromJson, Streamdemo.Schema.FeedToJsonMeasure, Streamdemo.Schema.FeedToJson, Streamdemo.Schema.FeedLoadMeasure),
        Row<Mapdemo.EdgeRow, Mapdemo.TableReport>("mapdemo", "EdgeRow", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.EdgeRowLoad, Mapdemo.Schema.EdgeRowMeasure, Mapdemo.Schema.EdgeRowSave,
            Mapdemo.Schema.EdgeRowFromJson, Mapdemo.Schema.EdgeRowToJsonMeasure, Mapdemo.Schema.EdgeRowToJson, Mapdemo.Schema.EdgeRowLoadMeasure),
        Row<Mapdemo.WideRow, Mapdemo.TableReport>("mapdemo", "WideRow", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.WideRowLoad, Mapdemo.Schema.WideRowMeasure, Mapdemo.Schema.WideRowSave,
            Mapdemo.Schema.WideRowFromJson, Mapdemo.Schema.WideRowToJsonMeasure, Mapdemo.Schema.WideRowToJson, Mapdemo.Schema.WideRowLoadMeasure),
        Row<Mapdemo.Row, Mapdemo.TableReport>("mapdemo", "Row", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.RowLoad, Mapdemo.Schema.RowMeasure, Mapdemo.Schema.RowSave,
            Mapdemo.Schema.RowFromJson, Mapdemo.Schema.RowToJsonMeasure, Mapdemo.Schema.RowToJson, Mapdemo.Schema.RowLoadMeasure),
        Row<Mapdemo.Trails, Mapdemo.TableReport>("mapdemo", "Trails", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.TrailsLoad, Mapdemo.Schema.TrailsMeasure, Mapdemo.Schema.TrailsSave,
            Mapdemo.Schema.TrailsFromJson, Mapdemo.Schema.TrailsToJsonMeasure, Mapdemo.Schema.TrailsToJson, Mapdemo.Schema.TrailsLoadMeasure),
        Row<Mapdemo.Crews, Mapdemo.TableReport>("mapdemo", "Crews", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.CrewsLoad, Mapdemo.Schema.CrewsMeasure, Mapdemo.Schema.CrewsSave,
            Mapdemo.Schema.CrewsFromJson, Mapdemo.Schema.CrewsToJsonMeasure, Mapdemo.Schema.CrewsToJson, Mapdemo.Schema.CrewsLoadMeasure),
        Row<Mapdemo.Pairs, Mapdemo.TableReport>("mapdemo", "Pairs", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.PairsLoad, Mapdemo.Schema.PairsMeasure, Mapdemo.Schema.PairsSave,
            Mapdemo.Schema.PairsFromJson, Mapdemo.Schema.PairsToJsonMeasure, Mapdemo.Schema.PairsToJson, Mapdemo.Schema.PairsLoadMeasure),
        Row<Mapdemo.Chunks, Mapdemo.TableReport>("mapdemo", "Chunks", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.ChunksLoad, Mapdemo.Schema.ChunksMeasure, Mapdemo.Schema.ChunksSave,
            Mapdemo.Schema.ChunksFromJson, Mapdemo.Schema.ChunksToJsonMeasure, Mapdemo.Schema.ChunksToJson, Mapdemo.Schema.ChunksLoadMeasure),
        Row<Mapdemo.Docs, Mapdemo.TableReport>("mapdemo", "Docs", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.DocsLoad, Mapdemo.Schema.DocsMeasure, Mapdemo.Schema.DocsSave,
            Mapdemo.Schema.DocsFromJson, Mapdemo.Schema.DocsToJsonMeasure, Mapdemo.Schema.DocsToJson, Mapdemo.Schema.DocsLoadMeasure),
        Row<Mapdemo.Spans, Mapdemo.TableReport>("mapdemo", "Spans", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.SpansLoad, Mapdemo.Schema.SpansMeasure, Mapdemo.Schema.SpansSave,
            Mapdemo.Schema.SpansFromJson, Mapdemo.Schema.SpansToJsonMeasure, Mapdemo.Schema.SpansToJson, Mapdemo.Schema.SpansLoadMeasure),
        Row<Mapdemo.Slots, Mapdemo.TableReport>("mapdemo", "Slots", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.SlotsLoad, Mapdemo.Schema.SlotsMeasure, Mapdemo.Schema.SlotsSave,
            Mapdemo.Schema.SlotsFromJson, Mapdemo.Schema.SlotsToJsonMeasure, Mapdemo.Schema.SlotsToJson, Mapdemo.Schema.SlotsLoadMeasure),
        Row<Mapdemo.Runs, Mapdemo.TableReport>("mapdemo", "Runs", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.RunsLoad, Mapdemo.Schema.RunsMeasure, Mapdemo.Schema.RunsSave,
            Mapdemo.Schema.RunsFromJson, Mapdemo.Schema.RunsToJsonMeasure, Mapdemo.Schema.RunsToJson, Mapdemo.Schema.RunsLoadMeasure),
        Row<Mapdemo.Cells, Mapdemo.TableReport>("mapdemo", "Cells", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.CellsLoad, Mapdemo.Schema.CellsMeasure, Mapdemo.Schema.CellsSave,
            Mapdemo.Schema.CellsFromJson, Mapdemo.Schema.CellsToJsonMeasure, Mapdemo.Schema.CellsToJson, Mapdemo.Schema.CellsLoadMeasure),
        Row<Mapdemo.Text, Mapdemo.TableReport>("mapdemo", "Text", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.TextLoad, Mapdemo.Schema.TextMeasure, Mapdemo.Schema.TextSave,
            Mapdemo.Schema.TextFromJson, Mapdemo.Schema.TextToJsonMeasure, Mapdemo.Schema.TextToJson, Mapdemo.Schema.TextLoadMeasure),
        Row<Mapdemo.Depth, Mapdemo.TableReport>("mapdemo", "Depth", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.DepthLoad, Mapdemo.Schema.DepthMeasure, Mapdemo.Schema.DepthSave,
            Mapdemo.Schema.DepthFromJson, Mapdemo.Schema.DepthToJsonMeasure, Mapdemo.Schema.DepthToJson, Mapdemo.Schema.DepthLoadMeasure),
        Row<Mapdemo.Fleet, Mapdemo.TableReport>("mapdemo", "Fleet", () => new Mapdemo.TableReport(), Copy,
            Mapdemo.Schema.FleetLoad, Mapdemo.Schema.FleetMeasure, Mapdemo.Schema.FleetSave,
            Mapdemo.Schema.FleetFromJson, Mapdemo.Schema.FleetToJsonMeasure, Mapdemo.Schema.FleetToJson, Mapdemo.Schema.FleetLoadMeasure),
        Row<Listdemo.Album, Listdemo.TableReport>("listdemo", "Album", () => new Listdemo.TableReport(), Copy,
            Listdemo.Schema.AlbumLoad, Listdemo.Schema.AlbumMeasure, Listdemo.Schema.AlbumSave,
            Listdemo.Schema.AlbumFromJson, Listdemo.Schema.AlbumToJsonMeasure, Listdemo.Schema.AlbumToJson, Listdemo.Schema.AlbumLoadMeasure),
        Row<Listdemo.Army, Listdemo.TableReport>("listdemo", "Army", () => new Listdemo.TableReport(), Copy,
            Listdemo.Schema.ArmyLoad, Listdemo.Schema.ArmyMeasure, Listdemo.Schema.ArmySave,
            Listdemo.Schema.ArmyFromJson, Listdemo.Schema.ArmyToJsonMeasure, Listdemo.Schema.ArmyToJson, Listdemo.Schema.ArmyLoadMeasure),
        Row<Listdemo.Sheet, Listdemo.TableReport>("listdemo", "Sheet", () => new Listdemo.TableReport(), Copy,
            Listdemo.Schema.SheetLoad, Listdemo.Schema.SheetMeasure, Listdemo.Schema.SheetSave,
            Listdemo.Schema.SheetFromJson, Listdemo.Schema.SheetToJsonMeasure, Listdemo.Schema.SheetToJson, Listdemo.Schema.SheetLoadMeasure),
        Row<Listdemo.Mixed, Listdemo.TableReport>("listdemo", "Mixed", () => new Listdemo.TableReport(), Copy,
            Listdemo.Schema.MixedLoad, Listdemo.Schema.MixedMeasure, Listdemo.Schema.MixedSave,
            Listdemo.Schema.MixedFromJson, Listdemo.Schema.MixedToJsonMeasure, Listdemo.Schema.MixedToJson, Listdemo.Schema.MixedLoadMeasure),
        Row<Listdemo.Save, Listdemo.TableReport>("listdemo", "Save", () => new Listdemo.TableReport(), Copy,
            Listdemo.Schema.SaveLoad, Listdemo.Schema.SaveMeasure, Listdemo.Schema.SaveSave,
            Listdemo.Schema.SaveFromJson, Listdemo.Schema.SaveToJsonMeasure, Listdemo.Schema.SaveToJson, Listdemo.Schema.SaveLoadMeasure),
        Row<Tblw2.Ship, Tblw2.TableReport>("tblw2", "Ship", () => new Tblw2.TableReport(), Copy,
            Tblw2.Schema.ShipLoad, Tblw2.Schema.ShipMeasure, Tblw2.Schema.ShipSave,
            Tblw2.Schema.ShipFromJson, Tblw2.Schema.ShipToJsonMeasure, Tblw2.Schema.ShipToJson),
        Row<Tblw2.Fleet, Tblw2.TableReport>("tblw2", "Fleet", () => new Tblw2.TableReport(), Copy,
            Tblw2.Schema.FleetLoad, Tblw2.Schema.FleetMeasure, Tblw2.Schema.FleetSave,
            Tblw2.Schema.FleetFromJson, Tblw2.Schema.FleetToJsonMeasure, Tblw2.Schema.FleetToJson, Tblw2.Schema.FleetLoadMeasure),
        Row<Tblw1.Vessel, Tblw1.TableReport>("tblw1", "Vessel", () => new Tblw1.TableReport(), Copy,
            Tblw1.Schema.VesselLoad, Tblw1.Schema.VesselMeasure, Tblw1.Schema.VesselSave,
            Tblw1.Schema.VesselFromJson, Tblw1.Schema.VesselToJsonMeasure, Tblw1.Schema.VesselToJson),
        Row<Tblw1.Fleet, Tblw1.TableReport>("tblw1", "Fleet", () => new Tblw1.TableReport(), Copy,
            Tblw1.Schema.FleetLoad, Tblw1.Schema.FleetMeasure, Tblw1.Schema.FleetSave,
            Tblw1.Schema.FleetFromJson, Tblw1.Schema.FleetToJsonMeasure, Tblw1.Schema.FleetToJson, Tblw1.Schema.FleetLoadMeasure),
        Row<Tblp2.Chain, Tblp2.TableReport>("tblp2", "Chain", () => new Tblp2.TableReport(), Copy,
            Tblp2.Schema.ChainLoad, Tblp2.Schema.ChainMeasure, Tblp2.Schema.ChainSave,
            Tblp2.Schema.ChainFromJson, Tblp2.Schema.ChainToJsonMeasure, Tblp2.Schema.ChainToJson, Tblp2.Schema.ChainLoadMeasure),
        Row<Blobdemo.Catalog, Blobdemo.TableReport>("blobdemo", "Catalog", () => new Blobdemo.TableReport(), Copy,
            Blobdemo.Schema.CatalogLoad, Blobdemo.Schema.CatalogMeasure, Blobdemo.Schema.CatalogSave,
            Blobdemo.Schema.CatalogFromJson, Blobdemo.Schema.CatalogToJsonMeasure, Blobdemo.Schema.CatalogToJson, Blobdemo.Schema.CatalogLoadMeasure),
        Row<Graphdemo.Album, Graphdemo.TableReport>("graphdemo", "Album", () => new Graphdemo.TableReport(), Copy,
            Graphdemo.Schema.AlbumLoad, Graphdemo.Schema.AlbumMeasure, Graphdemo.Schema.AlbumSave,
            Graphdemo.Schema.AlbumFromJson, Graphdemo.Schema.AlbumToJsonMeasure, Graphdemo.Schema.AlbumToJson, Graphdemo.Schema.AlbumLoadMeasure),
        Row<Graphdemo.Depot, Graphdemo.TableReport>("graphdemo", "Depot", () => new Graphdemo.TableReport(), Copy,
            Graphdemo.Schema.DepotLoad, Graphdemo.Schema.DepotMeasure, Graphdemo.Schema.DepotSave,
            Graphdemo.Schema.DepotFromJson, Graphdemo.Schema.DepotToJsonMeasure, Graphdemo.Schema.DepotToJson, Graphdemo.Schema.DepotLoadMeasure),
        Row<Graphdemo.Scene, Graphdemo.TableReport>("graphdemo", "Scene", () => new Graphdemo.TableReport(), Copy,
            Graphdemo.Schema.SceneLoad, Graphdemo.Schema.SceneMeasure, Graphdemo.Schema.SceneSave,
            Graphdemo.Schema.SceneFromJson, Graphdemo.Schema.SceneToJsonMeasure, Graphdemo.Schema.SceneToJson, Graphdemo.Schema.SceneLoadMeasure),
        Row<Wide.Stamp, Wide.TableReport>("widedemo", "Stamp", () => new Wide.TableReport(), Copy,
            Wide.Schema.StampLoad, Wide.Schema.StampMeasure, Wide.Schema.StampSave,
            Wide.Schema.StampFromJson, Wide.Schema.StampToJsonMeasure, Wide.Schema.StampToJson),
        Row<Wide.Caption, Wide.TableReport>("widedemo", "Caption", () => new Wide.TableReport(), Copy,
            Wide.Schema.CaptionLoad, Wide.Schema.CaptionMeasure, Wide.Schema.CaptionSave,
            Wide.Schema.CaptionFromJson, Wide.Schema.CaptionToJsonMeasure, Wide.Schema.CaptionToJson),
        Row<Scalardemo2.SimState, Scalardemo2.TableReport>("tblscalars2", "SimState", () => new Scalardemo2.TableReport(), Copy,
            Scalardemo2.Schema.SimStateLoad, Scalardemo2.Schema.SimStateMeasure, Scalardemo2.Schema.SimStateSave,
            Scalardemo2.Schema.SimStateFromJson, Scalardemo2.Schema.SimStateToJsonMeasure, Scalardemo2.Schema.SimStateToJson),
        Row<Scalardemo.Pose, Scalardemo.TableReport>("scalars", "Pose", () => new Scalardemo.TableReport(), Copy,
            Scalardemo.Schema.PoseLoad, Scalardemo.Schema.PoseMeasure, Scalardemo.Schema.PoseSave,
            Scalardemo.Schema.PoseFromJson, Scalardemo.Schema.PoseToJsonMeasure, Scalardemo.Schema.PoseToJson),
        Row<Scalardemo.SimState, Scalardemo.TableReport>("scalars", "SimState", () => new Scalardemo.TableReport(), Copy,
            Scalardemo.Schema.SimStateLoad, Scalardemo.Schema.SimStateMeasure, Scalardemo.Schema.SimStateSave,
            Scalardemo.Schema.SimStateFromJson, Scalardemo.Schema.SimStateToJsonMeasure, Scalardemo.Schema.SimStateToJson),
        Row<Messagedemo.User, Messagedemo.TableReport>("messagedemo", "User", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.UserLoad, Messagedemo.Schema.UserMeasure, Messagedemo.Schema.UserSave,
            Messagedemo.Schema.UserFromJson, Messagedemo.Schema.UserToJsonMeasure, Messagedemo.Schema.UserToJson),
        Row<Messagedemo.Script, Messagedemo.TableReport>("messagedemo", "Script", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.ScriptLoad, Messagedemo.Schema.ScriptMeasure, Messagedemo.Schema.ScriptSave,
            Messagedemo.Schema.ScriptFromJson, Messagedemo.Schema.ScriptToJsonMeasure, Messagedemo.Schema.ScriptToJson),
        Row<Messagedemo.Selection, Messagedemo.TableReport>("messagedemo", "Selection", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.SelectionLoad, Messagedemo.Schema.SelectionMeasure, Messagedemo.Schema.SelectionSave,
            Messagedemo.Schema.SelectionFromJson, Messagedemo.Schema.SelectionToJsonMeasure, Messagedemo.Schema.SelectionToJson),
        Row<Messagedemo.InsertText, Messagedemo.TableReport>("messagedemo", "InsertText", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.InsertTextLoad, Messagedemo.Schema.InsertTextMeasure, Messagedemo.Schema.InsertTextSave,
            Messagedemo.Schema.InsertTextFromJson, Messagedemo.Schema.InsertTextToJsonMeasure, Messagedemo.Schema.InsertTextToJson),
        Row<Messagedemo.RemoveText, Messagedemo.TableReport>("messagedemo", "RemoveText", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.RemoveTextLoad, Messagedemo.Schema.RemoveTextMeasure, Messagedemo.Schema.RemoveTextSave,
            Messagedemo.Schema.RemoveTextFromJson, Messagedemo.Schema.RemoveTextToJsonMeasure, Messagedemo.Schema.RemoveTextToJson),
        Row<Messagedemo.Edit, Messagedemo.TableReport>("messagedemo", "Edit", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.EditLoad, Messagedemo.Schema.EditMeasure, Messagedemo.Schema.EditSave,
            Messagedemo.Schema.EditFromJson, Messagedemo.Schema.EditToJsonMeasure, Messagedemo.Schema.EditToJson),
        Row<Messagedemo.OpenDocument, Messagedemo.TableReport>("messagedemo", "OpenDocument", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.OpenDocumentLoad, Messagedemo.Schema.OpenDocumentMeasure, Messagedemo.Schema.OpenDocumentSave,
            Messagedemo.Schema.OpenDocumentFromJson, Messagedemo.Schema.OpenDocumentToJsonMeasure, Messagedemo.Schema.OpenDocumentToJson),
        Row<Messagedemo.SaveDocument, Messagedemo.TableReport>("messagedemo", "SaveDocument", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.SaveDocumentLoad, Messagedemo.Schema.SaveDocumentMeasure, Messagedemo.Schema.SaveDocumentSave,
            Messagedemo.Schema.SaveDocumentFromJson, Messagedemo.Schema.SaveDocumentToJsonMeasure, Messagedemo.Schema.SaveDocumentToJson),
        Row<Messagedemo.Transaction, Messagedemo.TableReport>("messagedemo", "Transaction", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.TransactionLoad, Messagedemo.Schema.TransactionMeasure, Messagedemo.Schema.TransactionSave,
            Messagedemo.Schema.TransactionFromJson, Messagedemo.Schema.TransactionToJsonMeasure, Messagedemo.Schema.TransactionToJson),
        Row<Messagedemo.ToolMessage, Messagedemo.TableReport>("messagedemo", "ToolMessage", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.ToolMessageLoad, Messagedemo.Schema.ToolMessageMeasure, Messagedemo.Schema.ToolMessageSave,
            Messagedemo.Schema.ToolMessageFromJson, Messagedemo.Schema.ToolMessageToJsonMeasure, Messagedemo.Schema.ToolMessageToJson),
        Row<Messagedemo.Cursor, Messagedemo.TableReport>("messagedemo", "Cursor", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.CursorLoad, Messagedemo.Schema.CursorMeasure, Messagedemo.Schema.CursorSave,
            Messagedemo.Schema.CursorFromJson, Messagedemo.Schema.CursorToJsonMeasure, Messagedemo.Schema.CursorToJson),
        Row<Messagedemo.Ping, Messagedemo.TableReport>("messagedemo", "Ping", () => new Messagedemo.TableReport(), Copy,
            Messagedemo.Schema.PingLoad, Messagedemo.Schema.PingMeasure, Messagedemo.Schema.PingSave,
            Messagedemo.Schema.PingFromJson, Messagedemo.Schema.PingToJsonMeasure, Messagedemo.Schema.PingToJson),
        Row<Tblm1.Open, Tblm1.TableReport>("tblm1", "Open", () => new Tblm1.TableReport(), Copy,
            Tblm1.Schema.OpenLoad, Tblm1.Schema.OpenMeasure, Tblm1.Schema.OpenSave,
            Tblm1.Schema.OpenFromJson, Tblm1.Schema.OpenToJsonMeasure, Tblm1.Schema.OpenToJson),
        Row<Tblm1.Save, Tblm1.TableReport>("tblm1", "Save", () => new Tblm1.TableReport(), Copy,
            Tblm1.Schema.SaveLoad, Tblm1.Schema.SaveMeasure, Tblm1.Schema.SaveSave,
            Tblm1.Schema.SaveFromJson, Tblm1.Schema.SaveToJsonMeasure, Tblm1.Schema.SaveToJson),
        Row<Tblm1.Quit, Tblm1.TableReport>("tblm1", "Quit", () => new Tblm1.TableReport(), Copy,
            Tblm1.Schema.QuitLoad, Tblm1.Schema.QuitMeasure, Tblm1.Schema.QuitSave,
            Tblm1.Schema.QuitFromJson, Tblm1.Schema.QuitToJsonMeasure, Tblm1.Schema.QuitToJson),
        Row<Tblm1.Msg, Tblm1.TableReport>("tblm1", "Msg", () => new Tblm1.TableReport(), Copy,
            Tblm1.Schema.MsgLoad, Tblm1.Schema.MsgMeasure, Tblm1.Schema.MsgSave,
            Tblm1.Schema.MsgFromJson, Tblm1.Schema.MsgToJsonMeasure, Tblm1.Schema.MsgToJson),
        Row<Tblm2.Open, Tblm2.TableReport>("tblm2", "Open", () => new Tblm2.TableReport(), Copy,
            Tblm2.Schema.OpenLoad, Tblm2.Schema.OpenMeasure, Tblm2.Schema.OpenSave,
            Tblm2.Schema.OpenFromJson, Tblm2.Schema.OpenToJsonMeasure, Tblm2.Schema.OpenToJson),
        Row<Tblm2.Close, Tblm2.TableReport>("tblm2", "Close", () => new Tblm2.TableReport(), Copy,
            Tblm2.Schema.CloseLoad, Tblm2.Schema.CloseMeasure, Tblm2.Schema.CloseSave,
            Tblm2.Schema.CloseFromJson, Tblm2.Schema.CloseToJsonMeasure, Tblm2.Schema.CloseToJson),
        Row<Tblm2.Quit, Tblm2.TableReport>("tblm2", "Quit", () => new Tblm2.TableReport(), Copy,
            Tblm2.Schema.QuitLoad, Tblm2.Schema.QuitMeasure, Tblm2.Schema.QuitSave,
            Tblm2.Schema.QuitFromJson, Tblm2.Schema.QuitToJsonMeasure, Tblm2.Schema.QuitToJson),
        Row<Tblm2.Msg, Tblm2.TableReport>("tblm2", "Msg", () => new Tblm2.TableReport(), Copy,
            Tblm2.Schema.MsgLoad, Tblm2.Schema.MsgMeasure, Tblm2.Schema.MsgSave,
            Tblm2.Schema.MsgFromJson, Tblm2.Schema.MsgToJsonMeasure, Tblm2.Schema.MsgToJson),
        Row<Tbla1.Body, Tbla1.TableReport>("tbla1", "Body", () => new Tbla1.TableReport(), Copy,
            Tbla1.Schema.BodyLoad, Tbla1.Schema.BodyMeasure, Tbla1.Schema.BodySave,
            Tbla1.Schema.BodyFromJson, Tbla1.Schema.BodyToJsonMeasure, Tbla1.Schema.BodyToJson),
        Row<Tbla1.Root, Tbla1.TableReport>("tbla1", "Root", () => new Tbla1.TableReport(), Copy,
            Tbla1.Schema.RootLoad, Tbla1.Schema.RootMeasure, Tbla1.Schema.RootSave,
            Tbla1.Schema.RootFromJson, Tbla1.Schema.RootToJsonMeasure, Tbla1.Schema.RootToJson),
        Row<Tbla2.Body, Tbla2.TableReport>("tbla2", "Body", () => new Tbla2.TableReport(), Copy,
            Tbla2.Schema.BodyLoad, Tbla2.Schema.BodyMeasure, Tbla2.Schema.BodySave,
            Tbla2.Schema.BodyFromJson, Tbla2.Schema.BodyToJsonMeasure, Tbla2.Schema.BodyToJson),
        Row<Tbla2.Root, Tbla2.TableReport>("tbla2", "Root", () => new Tbla2.TableReport(), Copy,
            Tbla2.Schema.RootLoad, Tbla2.Schema.RootMeasure, Tbla2.Schema.RootSave,
            Tbla2.Schema.RootFromJson, Tbla2.Schema.RootToJsonMeasure, Tbla2.Schema.RootToJson),
        Row<Tblk1.Root, Tblk1.TableReport>("tblk1", "Root", () => new Tblk1.TableReport(), Copy,
            Tblk1.Schema.RootLoad, Tblk1.Schema.RootMeasure, Tblk1.Schema.RootSave,
            Tblk1.Schema.RootFromJson, Tblk1.Schema.RootToJsonMeasure, Tblk1.Schema.RootToJson),
        Row<Tblk2.Root, Tblk2.TableReport>("tblk2", "Root", () => new Tblk2.TableReport(), Copy,
            Tblk2.Schema.RootLoad, Tblk2.Schema.RootMeasure, Tblk2.Schema.RootSave,
            Tblk2.Schema.RootFromJson, Tblk2.Schema.RootToJsonMeasure, Tblk2.Schema.RootToJson),
        Demo<Tabledemo.RootConfig>("RootConfig", Tabledemo.Schema.RootConfigLoad, Tabledemo.Schema.RootConfigMeasure, Tabledemo.Schema.RootConfigSave,
            Tabledemo.Schema.RootConfigFromJson, Tabledemo.Schema.RootConfigToJsonMeasure, Tabledemo.Schema.RootConfigToJson),
        Demo<Tabledemo.ProfileConfig>("ProfileConfig", Tabledemo.Schema.ProfileConfigLoad, Tabledemo.Schema.ProfileConfigMeasure, Tabledemo.Schema.ProfileConfigSave,
            Tabledemo.Schema.ProfileConfigFromJson, Tabledemo.Schema.ProfileConfigToJsonMeasure, Tabledemo.Schema.ProfileConfigToJson),
        Demo<Tabledemo.LoadoutConfig>("LoadoutConfig", Tabledemo.Schema.LoadoutConfigLoad, Tabledemo.Schema.LoadoutConfigMeasure, Tabledemo.Schema.LoadoutConfigSave,
            Tabledemo.Schema.LoadoutConfigFromJson, Tabledemo.Schema.LoadoutConfigToJsonMeasure, Tabledemo.Schema.LoadoutConfigToJson),
        Demo<Tabledemo.WideBlob>("WideBlob", Tabledemo.Schema.WideBlobLoad, Tabledemo.Schema.WideBlobMeasure, Tabledemo.Schema.WideBlobSave,
            Tabledemo.Schema.WideBlobFromJson, Tabledemo.Schema.WideBlobToJsonMeasure, Tabledemo.Schema.WideBlobToJson),
        Demo<Tabledemo.ArchiveConfig>("ArchiveConfig", Tabledemo.Schema.ArchiveConfigLoad, Tabledemo.Schema.ArchiveConfigMeasure, Tabledemo.Schema.ArchiveConfigSave,
            Tabledemo.Schema.ArchiveConfigFromJson, Tabledemo.Schema.ArchiveConfigToJsonMeasure, Tabledemo.Schema.ArchiveConfigToJson),
        Demo<Tabledemo.KeyedConfig>("KeyedConfig", Tabledemo.Schema.KeyedConfigLoad, Tabledemo.Schema.KeyedConfigMeasure, Tabledemo.Schema.KeyedConfigSave,
            Tabledemo.Schema.KeyedConfigFromJson, Tabledemo.Schema.KeyedConfigToJsonMeasure, Tabledemo.Schema.KeyedConfigToJson),
        Demo<Tabledemo.PackConfig>("PackConfig", Tabledemo.Schema.PackConfigLoad, Tabledemo.Schema.PackConfigMeasure, Tabledemo.Schema.PackConfigSave,
            Tabledemo.Schema.PackConfigFromJson, Tabledemo.Schema.PackConfigToJsonMeasure, Tabledemo.Schema.PackConfigToJson),
        Row<Tblv1.Cfg, Tblv1.TableReport>("tblv1", "Cfg", () => new Tblv1.TableReport(), Copy,
            Tblv1.Schema.CfgLoad, Tblv1.Schema.CfgMeasure, Tblv1.Schema.CfgSave,
            Tblv1.Schema.CfgFromJson, Tblv1.Schema.CfgToJsonMeasure, Tblv1.Schema.CfgToJson),
        Row<Tblv2.Cfg, Tblv2.TableReport>("tblv2", "Cfg", () => new Tblv2.TableReport(), Copy,
            Tblv2.Schema.CfgLoad, Tblv2.Schema.CfgMeasure, Tblv2.Schema.CfgSave,
            Tblv2.Schema.CfgFromJson, Tblv2.Schema.CfgToJsonMeasure, Tblv2.Schema.CfgToJson),
        Row<Tblp1.Chain, Tblp1.TableReport>("tblp1", "Chain", () => new Tblp1.TableReport(), Copy,
            Tblp1.Schema.ChainLoad, Tblp1.Schema.ChainMeasure, Tblp1.Schema.ChainSave,
            Tblp1.Schema.ChainFromJson, Tblp1.Schema.ChainToJsonMeasure, Tblp1.Schema.ChainToJson),
        Row<Tblp3.Chain, Tblp3.TableReport>("tblp3", "Chain", () => new Tblp3.TableReport(), Copy,
            Tblp3.Schema.ChainLoad, Tblp3.Schema.ChainMeasure, Tblp3.Schema.ChainSave,
            Tblp3.Schema.ChainFromJson, Tblp3.Schema.ChainToJsonMeasure, Tblp3.Schema.ChainToJson),
    };

    delegate long LoadMeasureOf(ReadOnlySpan<byte> bytes);
    delegate bool LoadOf<TValue, TReport>(TValue value, ReadOnlySpan<byte> bytes, TReport report);
    delegate bool FromJsonOf<TValue, TReport>(TValue value, ReadOnlySpan<byte> text, TReport report);
    delegate long SaveOf<TValue>(TValue value, Span<byte> buffer);

    // ONE row of the codec table, for any unit. The value type and the unit's
    // own TableReport are the two type parameters; `make` and `copy` are the
    // two things that cannot be generic, because each unit declares its own
    // report class and C# has no structural typing to unify five identical
    // shapes. Everything else — the wire pair and the text pair — is the same
    // three lines whatever the unit, which is why they are written once.
    static Codec Row<TValue, TReport>(
        string unit, string root,
        Func<TReport> make, Func<TReport, Report> copy,
        LoadOf<TValue, TReport> load, Func<TValue, long> measure, SaveOf<TValue> save,
        FromJsonOf<TValue, TReport> fromJson, Func<TValue, long> toJsonMeasure, SaveOf<TValue> toJson, LoadMeasureOf loadMeasure = null)
        where TValue : new()
    {
        return new Codec
        {
            Unit = unit,
            Root = root,
            LoadMeasure = loadMeasure,
            PartialLoad = (bytes, report) =>
            {
                TValue value = new TValue();
                TReport inner = make();
                load(value, bytes, inner);
                Fill(report, copy(inner));
                return value;
            },
            Load = (bytes, report) =>
            {
                TValue value = new TValue();
                TReport inner = make();
                bool ok = load(value, bytes, inner);
                Fill(report, copy(inner));
                return ok ? (object)value : null;
            },
            Measure = v => measure((TValue)v),
            Save = (v, buffer) => save((TValue)v, buffer),
            FromJson = (text, report) =>
            {
                TValue value = new TValue();
                TReport inner = make();
                bool ok = fromJson(value, text, inner);
                Fill(report, copy(inner));
                return ok ? (object)value : null;
            },
            ToJsonMeasure = v => toJsonMeasure((TValue)v),
            ToJson = (v, buffer) => toJson((TValue)v, buffer),
        };
    }

    static Codec Demo<T>(string root,
        LoadOf<T, Tabledemo.TableReport> load, Func<T, long> measure, SaveOf<T> save,
        FromJsonOf<T, Tabledemo.TableReport> fromJson, Func<T, long> toJsonMeasure, SaveOf<T> toJson)
        where T : new()
    {
        return Row<T, Tabledemo.TableReport>("tabledemo", root, () => new Tabledemo.TableReport(), Copy,
            load, measure, save, fromJson, toJsonMeasure, toJson);
    }

    static void Fill(Report to, Report from)
    {
        to.Unknown = from.Unknown;
        to.Widened = from.Widened;
        to.Refused = from.Refused;
        to.KindMismatch = from.KindMismatch;
        to.Clamped = from.Clamped;
        to.Duplicate = from.Duplicate;
        to.Malformed = from.Malformed;
    }

    static Codec Find(string unit, string root)
    {
        foreach (Codec c in codecs)
        {
            if (c.Unit == unit && c.Root == root)
            {
                return c;
            }
        }
        return null;
    }

    // ---- the surfaces

    static int SurfaceCookWrite(string outDir)
    {
        foreach (string[] f in Kind("cook-write"))
        {
            string[] instance = lines.Find(c => c[0] == "instance" && c[1] == f[1]);
            Codec codec = Find(instance[2], instance[3]);
            if (codec == null || codec.Cook == null) { SpillAbsent(outDir, f[1]); SpillAbsent(outDir, f[1] + "-be"); continue; }
            object value = codec.Load(File.ReadAllBytes(instance[4]), new Report());
            long size = codec.CookMeasure(value);
            if (size < 0) { Console.Error.WriteLine("cook measure failed: " + f[1]); return 1; }
            byte[] bytes = new byte[checked((int)size)];
            foreach (bool big in new bool[] { false, true })
            {
                if (!codec.Cook(value, bytes, big)) { Console.Error.WriteLine("cook failed: " + f[1]); return 1; }
                File.WriteAllBytes(Path.Combine(outDir, f[1] + (big ? "-be" : "")), bytes);
            }
        }
        return 0;
    }

    static int SurfaceMessage(string outDir)
    {
        foreach (string[] f in Kind("message"))
        {
            string[] connection = lines.Find(c => c[0] == "connection" && c[1] == f[2]);
            Codec codec = Find(connection[2], f[3]);
            if (codec == null) { SpillAbsent(outDir, f[1]); continue; }
            var report = new Report();
            object value = codec.MessageLoad(File.ReadAllBytes(connection[4]), File.ReadAllBytes(f[5]), report);
            if (report.Malformed || report.Refused) { Console.Error.WriteLine("message read failed: " + f[1]); return 1; }
            long size = codec.MessageMeasure(value);
            if (size < 0) { return 1; }
            byte[] bytes = new byte[checked((int)size)];
            if (codec.MessageSave(value, bytes) != size) { return 1; }
            File.WriteAllBytes(Path.Combine(outDir, f[1]), bytes);
        }
        return 0;
    }

    static int SurfaceWire(string outDir)
    {
        foreach (string[] f in Kind("instance"))
        {
            Codec codec = Find(f[2], f[3]);
            if (codec == null)
            {
                // C# refuses a pointered unit's wire by name (§11), so it has
                // no codec here and says so per case
                SpillAbsent(outDir, f[1]);
                continue;
            }
            byte[] wire = File.ReadAllBytes(f[4]);
            Report report = new Report();
            object value = codec.Load(wire, report);
            if (value == null)
            {
                Console.Error.WriteLine("driver: " + f[1] + " does not load");
                return 1;
            }
            long size = codec.Measure(value);
            byte[] buffer = new byte[size];
            if (codec.Save(value, buffer) != size)
            {
                Console.Error.WriteLine("driver: " + f[1] + " saves a size its measure did not name");
                return 1;
            }
            File.WriteAllBytes(Path.Combine(outDir, f[1]), buffer);
        }
        return 0;
    }

    // json-read: the text is the input and the WIRE is the answer, so the pass
    // proves the reader against bytes this driver did not write.
    static int SurfaceJsonRead(string outDir)
    {
        foreach (string[] f in Kind("instance"))
        {
            if (NoText(f)) { continue; }
            Codec codec = Find(f[2], f[3]);
            if (codec == null)
            {
                // C# refuses a pointered unit's wire by name (§11), so it has
                // no codec here and says so per case
                SpillAbsent(outDir, f[1]);
                continue;
            }
            string path = Path.Combine("testdata", "conformance", "tables", "json", f[1] + ".json");
            byte[] text = File.ReadAllBytes(path);
            Report report = new Report();
            object value = codec.FromJson(text, report);
            if (value == null)
            {
                Console.Error.WriteLine("driver: " + f[1] + " does not read as JSON");
                return 1;
            }
            long size = codec.Measure(value);
            if (size < 0)
            {
                Console.Error.WriteLine("driver: " + f[1] + " measures as unsaveable after a clean read");
                return 1;
            }
            byte[] buffer = new byte[size];
            if (codec.Save(value, buffer) != size)
            {
                Console.Error.WriteLine("driver: " + f[1] + " saves a size its measure did not name");
                return 1;
            }
            File.WriteAllBytes(Path.Combine(outDir, f[1]), buffer);
        }
        return 0;
    }

    // json-write: the wire is the input and the TEXT is the answer, compared
    // against a text a third implementation wrote.
    static int SurfaceJsonWrite(string outDir)
    {
        foreach (string[] f in Kind("instance"))
        {
            if (NoText(f)) { continue; }
            Codec codec = Find(f[2], f[3]);
            if (codec == null)
            {
                // C# refuses a pointered unit's wire by name (§11), so it has
                // no codec here and says so per case
                SpillAbsent(outDir, f[1] + ".json");
                continue;
            }
            byte[] wire = File.ReadAllBytes(f[4]);
            Report report = new Report();
            object value = codec.Load(wire, report);
            if (value == null)
            {
                Console.Error.WriteLine("driver: " + f[1] + " does not load");
                return 1;
            }
            long size = codec.ToJsonMeasure(value);
            if (size < 0)
            {
                Console.Error.WriteLine("driver: " + f[1] + " holds a value ToJson refuses");
                return 1;
            }
            byte[] text = new byte[size];
            if (codec.ToJson(value, text) != size)
            {
                Console.Error.WriteLine("driver: " + f[1] + " writes a text its measure did not name");
                return 1;
            }
            File.WriteAllBytes(Path.Combine(outDir, f[1] + ".json"), text);
        }
        return 0;
    }

    // json-hostile: one tree per rule the text form states (§16.2, §16.3,
    // §17.5). The answer is the REPORT the read produces, or `refused` — the
    // same two-valued verdict the engine's own gate holds, over the same data.
    static int SurfaceJsonHostile(string outDir)
    {
        foreach (string[] f in Kind("json-hostile"))
        {
            Codec codec = Find(f[2], f[3]);
            if (codec == null)
            {
                // C# refuses a pointered unit's wire by name (§11), so it has
                // no codec here and says so per case
                SpillAbsent(outDir, f[1]);
                continue;
            }
            // the tree is what `schema pack` reads, so the text is
            // <tree>/<root>.json (§17)
            byte[] text = File.ReadAllBytes(Path.Combine(f[4], f[3] + ".json"));
            Report report = new Report();
            object value = codec.FromJson(text, report);
            string verdict = value == null || report.Malformed
                ? "refused\n"
                : report.Unknown + "," + report.KindMismatch + "," + report.Widened + "," + report.Clamped + "," +
                  report.Duplicate + ",false,read\n";
            File.WriteAllText(Path.Combine(outDir, f[1]), verdict);
        }
        return 0;
    }

    static int SurfaceReport(string outDir)
    {
        foreach (string[] f in Kind("report"))
        {
            Codec codec = Find(f[2], f[3]);
            if (codec == null)
            {
                // C# refuses a pointered unit's wire by name (§11), so it has
                // no codec here and says so per case
                SpillAbsent(outDir, f[1]);
                continue;
            }
            byte[] wire = File.ReadAllBytes(f[4]);
            Report report = new Report();
            object value = codec.Load(wire, report);
            bool malformed = report.Malformed || (value == null && !report.Refused);
            string text = report.Unknown + "," + report.KindMismatch + "," + report.Widened + "," + report.Clamped + "," +
                          report.Duplicate + "," + (malformed ? "true" : "false") + "," + (report.Refused ? "refused" : "read") + "\n";
            File.WriteAllText(Path.Combine(outDir, f[1]), text);
        }
        return 0;
    }

    // A block's base is 64-byte aligned by construction (§19.1) and `extent` is
    // the length the CALLER claims, which a forgery may set past the bytes the
    // image carries. The allocation is the claim, so a reader that walks past
    // what it was given walks into memory this process owns and nothing else's.
    static unsafe string OpenBlock(string name, byte[] bytes, long extent, bool reasons = false, string pointerMode = "0")
    {
        // A CLAIM SHORTER THAN THE IMAGE IS A TRUNCATION, so the allocation is
        // the claim in that direction too and only what fits is copied.
        long claim = extent < 0 ? bytes.Length : extent;
        IntPtr raw = Marshal.AllocHGlobal(new IntPtr(claim + 128));
        try
        {
            long aligned = ((long)raw + 63) & ~63L;
            IntPtr pointer = new IntPtr(aligned + (pointerMode == "null" ? 0 : int.Parse(pointerMode)));
            new Span<byte>((void*)pointer, (int)Math.Min(claim, int.MaxValue)).Clear();
            Marshal.Copy(bytes, 0, pointer, (int)Math.Min(claim, bytes.Length));
            if (pointerMode == "null") { pointer = IntPtr.Zero; }
            Blockdemo.TableRefuseReason reason;
            bool opened;
            if (name.StartsWith("block_render", StringComparison.Ordinal))
            {
                Blockdemo.RenderFrameBlock block;
                opened = Blockdemo.RenderFrameBlock.Open(out block, pointer, claim, out reason);
            }
            else if (name.StartsWith("block_padded", StringComparison.Ordinal))
            {
                Blockdemo.PaddedFrameBlock block;
                opened = Blockdemo.PaddedFrameBlock.Open(out block, pointer, claim, out reason);
            }
            else
            {
                Console.Error.WriteLine("driver: no block named " + name);
                Environment.Exit(1);
                return "";
            }
            return reasons ? (opened ? "ok" : reason.ToString()) + "\n" : opened ? "open\n" : "refuse\n";
        }
        finally
        {
            Marshal.FreeHGlobal(raw);
        }
    }

    static int SurfaceBlock(string outDir)
    {
        foreach (string[] f in Kind("block"))
        {
            File.WriteAllText(Path.Combine(outDir, f[1]), OpenBlock(f[1], File.ReadAllBytes(f[3]), -1));
        }
        return 0;
    }

    // Foreign reverses the MAGIC word — the eight bytes at offset 0 — which is
    // what that word looks like to a reader of the OTHER byte order (§19.1,
    // §7.1). It makes the file foreign to WHOEVER READS IT rather than to a
    // particular host, so the refusal lands on the magic check every Open puts
    // first and the expectation is `refuse` for every leg on every machine.
    static byte[] Foreign(byte[] data)
    {
        byte[] out_ = (byte[])data.Clone();
        if (out_.Length >= 8)
        {
            for (int i = 0; i < 4; i++)
            {
                byte t = out_[i];
                out_[i] = out_[7 - i];
                out_[7 - i] = t;
            }
        }
        return out_;
    }

    // the cross-endian refusal over the block form: the same images with their
    // magic reversed, which every leg must refuse
    static int SurfaceBlockForeign(string outDir)
    {
        foreach (string[] f in Kind("block"))
        {
            File.WriteAllText(Path.Combine(outDir, f[1]),
                OpenBlock(f[1], Foreign(File.ReadAllBytes(f[3])), -1));
        }
        return 0;
    }

    // ---- the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md)
    //
    // The twin of the C++ leg's walk, and like it, written against §8's
    // descriptors and NOTHING ELSE: no generated row struct, no field named in
    // this file. That is the claim §19.2 makes for the descriptors, and a walk
    // that reached for a struct would be proving something else. A FLOAT is its
    // IEEE-754 bit pattern, because a block row is a byte-identical projection
    // and its bits are the fact.

    static unsafe void DumpScalar(StringBuilder into, byte* at, byte kind, int width)
    {
        switch (kind)
        {
            case 1:
                into.Append(*at != 0 ? "true" : "false");
                return;
            case 10:
                into.Append("0x").Append((*(uint*)at).ToString("x8", CultureInfo.InvariantCulture));
                return;
            case 11:
                into.Append("0x").Append((*(ulong*)at).ToString("x16", CultureInfo.InvariantCulture));
                return;
            case 2: case 3: case 4: case 5:
            {
                long v = width == 1 ? *(sbyte*)at : width == 2 ? *(short*)at : width == 4 ? *(int*)at : *(long*)at;
                into.Append(v.ToString(CultureInfo.InvariantCulture));
                return;
            }
            default:
            {
                ulong v = width == 1 ? *at : width == 2 ? *(ushort*)at : width == 4 ? *(uint*)at : *(ulong*)at;
                into.Append(v.ToString(CultureInfo.InvariantCulture));
                return;
            }
        }
    }

    static unsafe void DumpText(StringBuilder into, byte* at, int used)
    {
        if (used < 0)
        {
            used = 0;
        }
        into.Append('"');
        for (int i = 0; i < used; i++)
        {
            byte c = at[i];
            if (c >= 0x20 && c < 0x7f && c != (byte)'"' && c != (byte)'\\')
            {
                into.Append((char)c);
            }
            else
            {
                into.Append("\\x").Append(c.ToString("x2", CultureInfo.InvariantCulture));
            }
        }
        into.Append('"').Append(" len=").Append(used.ToString(CultureInfo.InvariantCulture));
    }

    static string DumpJoin(string prefix, string name)
    {
        return prefix.Length == 0 ? name : prefix + "." + name;
    }

    static unsafe bool DumpRecord(StringBuilder into, byte* storage, Blockdemo.TableBlockInfo info, string path)
    {
        if (info == null)
        {
            Console.Error.WriteLine("driver: a descriptor names no record");
            return false;
        }
        foreach (Blockdemo.TableBlockFieldInfo f in info.Fields)
        {
            if (f.OutOfLine)
            {
                continue;
            }
            string name = DumpJoin(path, f.Name);
            if (f.Counted)
            {
                int used = *(int*)(storage + f.CountOffset);
                if (used < 0 || used > f.ArrayBound)
                {
                    Console.Error.WriteLine("driver: " + info.Name + "." + f.Name +
                                            " carries a used length of " + used + ", outside [ 0, " + f.ArrayBound + " ]");
                    return false;
                }
                into.Append("  ").Append(name).Append(" = ");
                DumpText(into, storage + f.Offset, used);
                into.Append('\n');
            }
            else
            {
                int slots = f.IsArray ? f.ArrayBound : 1;
                for (int s = 0; s < slots; s++)
                {
                    string at = f.IsArray ? name + "[" + s.ToString(CultureInfo.InvariantCulture) + "]" : name;
                    byte* value = storage + f.Offset + (long)s * f.ElemSize;
                    if (f.Element != null)
                    {
                        if (!DumpRecord(into, value, f.Element, at))
                        {
                            return false;
                        }
                    }
                    else
                    {
                        into.Append("  ").Append(at).Append(" = ");
                        DumpScalar(into, value, f.Kind, f.ElemSize);
                        into.Append('\n');
                    }
                }
            }
            if (f.Optional)
            {
                into.Append("  ").Append(name).Append("#present = ")
                    .Append(storage[f.PresentOffset] != 0 ? "true" : "false").Append('\n');
            }
        }
        return true;
    }

    static unsafe bool DumpBlock(StringBuilder into, byte* baseAt, Blockdemo.TableBlockInfo info)
    {
        into.Append("projection ").Append(info.Name).Append(" @0\n");
        if (!DumpRecord(into, baseAt, info, ""))
        {
            return false;
        }
        foreach (Blockdemo.TableBlockFieldInfo f in info.Fields)
        {
            if (!f.OutOfLine)
            {
                continue;
            }
            ulong offsetOf = *(ulong*)(baseAt + f.OffsetOfOffset);
            uint count = *(uint*)(baseAt + f.CountOffset);
            uint stride = *(uint*)(baseAt + f.StrideOffset);
            Blockdemo.TableBlockInfo row = f.Element;
            if (row == null)
            {
                Console.Error.WriteLine("driver: " + f.Name + " names no element");
                return false;
            }
            into.Append("array ").Append(f.Name).Append(' ').Append(row.Name)
                .Append(" @").Append(offsetOf.ToString(CultureInfo.InvariantCulture))
                .Append(" count=").Append(count.ToString(CultureInfo.InvariantCulture))
                .Append(" stride=").Append(stride.ToString(CultureInfo.InvariantCulture)).Append('\n');
            for (uint r = 0; r < count; r++)
            {
                ulong at = offsetOf + (ulong)r * stride;
                into.Append("row ").Append(r.ToString(CultureInfo.InvariantCulture))
                    .Append(" @").Append(at.ToString(CultureInfo.InvariantCulture)).Append('\n');
                if (!DumpRecord(into, baseAt + (long)at, row, ""))
                {
                    return false;
                }
            }
        }
        return true;
    }

    static unsafe bool BlockDump(string name, byte[] bytes, StringBuilder into)
    {
        IntPtr raw = Marshal.AllocHGlobal(new IntPtr(bytes.Length + 64));
        try
        {
            long aligned = ((long)raw + 63) & ~63L;
            IntPtr pointer = new IntPtr(aligned);
            new Span<byte>((void*)pointer, bytes.Length).Clear();
            Marshal.Copy(bytes, 0, pointer, bytes.Length);
            if (name.StartsWith("block_render", StringComparison.Ordinal))
            {
                Blockdemo.RenderFrameBlock block;
                return Blockdemo.RenderFrameBlock.Open(out block, pointer, bytes.Length) &&
                       DumpBlock(into, block.Base, Blockdemo.RenderFrameBlock.Type);
            }
            if (name.StartsWith("block_padded", StringComparison.Ordinal))
            {
                Blockdemo.PaddedFrameBlock block;
                return Blockdemo.PaddedFrameBlock.Open(out block, pointer, bytes.Length) &&
                       DumpBlock(into, block.Base, Blockdemo.PaddedFrameBlock.Type);
            }
            Console.Error.WriteLine("driver: no block named " + name);
            return false;
        }
        finally
        {
            Marshal.FreeHGlobal(raw);
        }
    }

    static int SurfaceBlockDump(string outDir)
    {
        foreach (string[] f in Kind("block"))
        {
            StringBuilder text = new StringBuilder();
            if (!BlockDump(f[1], File.ReadAllBytes(f[3]), text))
            {
                return 1;
            }
            File.WriteAllBytes(Path.Combine(outDir, f[1]), Encoding.UTF8.GetBytes(text.ToString()));
        }
        return 0;
    }

    static int SurfaceBlockReason(string outDir)
    {
        foreach (string[] r in Kind("refusal"))
        {
            if (r[2] != "block") { continue; }
            string[] f = lines.Find(x => x[0] == "forgery" && x[1] == r[1]);
            File.WriteAllText(Path.Combine(outDir,r[1]), OpenBlock(f[3],File.ReadAllBytes(f[4]),long.Parse(f[5]),true,f.Length > 6 ? f[6] : "0"));
        }
        return 0;
    }

    static int SurfaceForgery(string outDir)
    {
        foreach (string[] f in Kind("forgery"))
        {
            if (f[2] != "block")
            {
                continue; // the cook's battery is its own binary's
            }
            File.WriteAllText(Path.Combine(outDir, f[1]),
                OpenBlock(f[3], File.ReadAllBytes(f[4]), long.Parse(f[5])));
        }
        return 0;
    }

    static void RegisterMessages()
    {
        {
            Codec c = Find("tabledemo", "RootConfig");
            c.CookMeasure = value => Tabledemo.Schema.RootConfigCookMeasure((Tabledemo.RootConfig)value);
            c.Cook = (value, bytes, big) => Tabledemo.Schema.RootConfigCook((Tabledemo.RootConfig)value, bytes, big ? Tabledemo.TableByteOrder.Big : Tabledemo.TableByteOrder.Little);
        }
        {
            Codec c = Find("tabledemo", "ProfileConfig");
            c.CookMeasure = value => Tabledemo.Schema.ProfileConfigCookMeasure((Tabledemo.ProfileConfig)value);
            c.Cook = (value, bytes, big) => Tabledemo.Schema.ProfileConfigCook((Tabledemo.ProfileConfig)value, bytes, big ? Tabledemo.TableByteOrder.Big : Tabledemo.TableByteOrder.Little);
        }
        {
            Codec c = Find("tabledemo", "LoadoutConfig");
            c.CookMeasure = value => Tabledemo.Schema.LoadoutConfigCookMeasure((Tabledemo.LoadoutConfig)value);
            c.Cook = (value, bytes, big) => Tabledemo.Schema.LoadoutConfigCook((Tabledemo.LoadoutConfig)value, bytes, big ? Tabledemo.TableByteOrder.Big : Tabledemo.TableByteOrder.Little);
        }
        {
            Codec c = Find("tabledemo", "WideBlob");
            c.CookMeasure = value => Tabledemo.Schema.WideBlobCookMeasure((Tabledemo.WideBlob)value);
            c.Cook = (value, bytes, big) => Tabledemo.Schema.WideBlobCook((Tabledemo.WideBlob)value, bytes, big ? Tabledemo.TableByteOrder.Big : Tabledemo.TableByteOrder.Little);
        }
        {
            Codec c = Find("tabledemo", "ArchiveConfig");
            c.CookMeasure = value => Tabledemo.Schema.ArchiveConfigCookMeasure((Tabledemo.ArchiveConfig)value);
            c.Cook = (value, bytes, big) => Tabledemo.Schema.ArchiveConfigCook((Tabledemo.ArchiveConfig)value, bytes, big ? Tabledemo.TableByteOrder.Big : Tabledemo.TableByteOrder.Little);
        }
        {
            Codec c = Find("tabledemo", "KeyedConfig");
            c.CookMeasure = value => Tabledemo.Schema.KeyedConfigCookMeasure((Tabledemo.KeyedConfig)value);
            c.Cook = (value, bytes, big) => Tabledemo.Schema.KeyedConfigCook((Tabledemo.KeyedConfig)value, bytes, big ? Tabledemo.TableByteOrder.Big : Tabledemo.TableByteOrder.Little);
        }
        {
            Codec c = Find("tabledemo", "PackConfig");
            c.CookMeasure = value => Tabledemo.Schema.PackConfigCookMeasure((Tabledemo.PackConfig)value);
            c.Cook = (value, bytes, big) => Tabledemo.Schema.PackConfigCook((Tabledemo.PackConfig)value, bytes, big ? Tabledemo.TableByteOrder.Big : Tabledemo.TableByteOrder.Little);
        }
        {
            Codec c = Find("tblr2", "Cfg");
            c.Announcement = Tblr2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblr2.Schema.CfgCookMeasure((Tblr2.Cfg)value);
            c.Cook = (value, bytes, big) => Tblr2.Schema.CfgCook((Tblr2.Cfg)value, bytes, big ? Tblr2.TableByteOrder.Big : Tblr2.TableByteOrder.Little);
            var ownVocabulary = new Tblr2.TableVocabulary(); Tblr2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblr2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblr2.TableVocabulary(); var inner = new Tblr2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblr2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblr2.Cfg[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblr2.Cfg(); }
                if (!inner.Refused && !inner.Malformed) { Tblr2.Schema.CfgLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblr2.Schema.CfgMeasureMessages(new Tblr2.Cfg[] { (Tblr2.Cfg)value });
            c.MessageSave = (value, bytes) => Tblr2.Schema.CfgSaveMessages(new Tblr2.Cfg[] { (Tblr2.Cfg)value }, bytes);
        }
        {
            Codec c = Find("tblr1", "Cfg");
            c.Announcement = Tblr1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblr1.Schema.CfgCookMeasure((Tblr1.Cfg)value);
            c.Cook = (value, bytes, big) => Tblr1.Schema.CfgCook((Tblr1.Cfg)value, bytes, big ? Tblr1.TableByteOrder.Big : Tblr1.TableByteOrder.Little);
            var ownVocabulary = new Tblr1.TableVocabulary(); Tblr1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblr1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblr1.TableVocabulary(); var inner = new Tblr1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblr1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblr1.Cfg[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblr1.Cfg(); }
                if (!inner.Refused && !inner.Malformed) { Tblr1.Schema.CfgLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblr1.Schema.CfgMeasureMessages(new Tblr1.Cfg[] { (Tblr1.Cfg)value });
            c.MessageSave = (value, bytes) => Tblr1.Schema.CfgSaveMessages(new Tblr1.Cfg[] { (Tblr1.Cfg)value }, bytes);
        }
        {
            Codec c = Find("vocab9demo", "Wide19");
            c.Announcement = Vocab9demo.Schema.Announce().ToArray();
            c.CookMeasure = value => Vocab9demo.Schema.Wide19CookMeasure((Vocab9demo.Wide19)value);
            c.Cook = (value, bytes, big) => Vocab9demo.Schema.Wide19Cook((Vocab9demo.Wide19)value, bytes, big ? Vocab9demo.TableByteOrder.Big : Vocab9demo.TableByteOrder.Little);
            var ownVocabulary = new Vocab9demo.TableVocabulary(); Vocab9demo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Vocab9demo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Vocab9demo.TableVocabulary(); var inner = new Vocab9demo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Vocab9demo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Vocab9demo.Wide19[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Vocab9demo.Wide19(); }
                if (!inner.Refused && !inner.Malformed) { Vocab9demo.Schema.Wide19LoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Vocab9demo.Schema.Wide19MeasureMessages(new Vocab9demo.Wide19[] { (Vocab9demo.Wide19)value });
            c.MessageSave = (value, bytes) => Vocab9demo.Schema.Wide19SaveMessages(new Vocab9demo.Wide19[] { (Vocab9demo.Wide19)value }, bytes);
        }
        {
            Codec c = Find("vocab9demo", "Wide00");
            c.Announcement = Vocab9demo.Schema.Announce().ToArray();
            c.CookMeasure = value => Vocab9demo.Schema.Wide00CookMeasure((Vocab9demo.Wide00)value);
            c.Cook = (value, bytes, big) => Vocab9demo.Schema.Wide00Cook((Vocab9demo.Wide00)value, bytes, big ? Vocab9demo.TableByteOrder.Big : Vocab9demo.TableByteOrder.Little);
            var ownVocabulary = new Vocab9demo.TableVocabulary(); Vocab9demo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Vocab9demo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Vocab9demo.TableVocabulary(); var inner = new Vocab9demo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Vocab9demo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Vocab9demo.Wide00[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Vocab9demo.Wide00(); }
                if (!inner.Refused && !inner.Malformed) { Vocab9demo.Schema.Wide00LoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Vocab9demo.Schema.Wide00MeasureMessages(new Vocab9demo.Wide00[] { (Vocab9demo.Wide00)value });
            c.MessageSave = (value, bytes) => Vocab9demo.Schema.Wide00SaveMessages(new Vocab9demo.Wide00[] { (Vocab9demo.Wide00)value }, bytes);
        }
        {
            Codec c = Find("vocabdemo", "Wide09");
            c.Announcement = Vocabdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Vocabdemo.Schema.Wide09CookMeasure((Vocabdemo.Wide09)value);
            c.Cook = (value, bytes, big) => Vocabdemo.Schema.Wide09Cook((Vocabdemo.Wide09)value, bytes, big ? Vocabdemo.TableByteOrder.Big : Vocabdemo.TableByteOrder.Little);
            var ownVocabulary = new Vocabdemo.TableVocabulary(); Vocabdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Vocabdemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Vocabdemo.TableVocabulary(); var inner = new Vocabdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Vocabdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Vocabdemo.Wide09[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Vocabdemo.Wide09(); }
                if (!inner.Refused && !inner.Malformed) { Vocabdemo.Schema.Wide09LoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Vocabdemo.Schema.Wide09MeasureMessages(new Vocabdemo.Wide09[] { (Vocabdemo.Wide09)value });
            c.MessageSave = (value, bytes) => Vocabdemo.Schema.Wide09SaveMessages(new Vocabdemo.Wide09[] { (Vocabdemo.Wide09)value }, bytes);
        }
        {
            Codec c = Find("vocabdemo", "Wide00");
            c.Announcement = Vocabdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Vocabdemo.Schema.Wide00CookMeasure((Vocabdemo.Wide00)value);
            c.Cook = (value, bytes, big) => Vocabdemo.Schema.Wide00Cook((Vocabdemo.Wide00)value, bytes, big ? Vocabdemo.TableByteOrder.Big : Vocabdemo.TableByteOrder.Little);
            var ownVocabulary = new Vocabdemo.TableVocabulary(); Vocabdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Vocabdemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Vocabdemo.TableVocabulary(); var inner = new Vocabdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Vocabdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Vocabdemo.Wide00[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Vocabdemo.Wide00(); }
                if (!inner.Refused && !inner.Malformed) { Vocabdemo.Schema.Wide00LoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Vocabdemo.Schema.Wide00MeasureMessages(new Vocabdemo.Wide00[] { (Vocabdemo.Wide00)value });
            c.MessageSave = (value, bytes) => Vocabdemo.Schema.Wide00SaveMessages(new Vocabdemo.Wide00[] { (Vocabdemo.Wide00)value }, bytes);
        }
        {
            Codec c = Find("backenddemo", "Envelope");
            c.Announcement = Backenddemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Backenddemo.Schema.EnvelopeCookMeasure((Backenddemo.Envelope)value);
            c.Cook = (value, bytes, big) => Backenddemo.Schema.EnvelopeCook((Backenddemo.Envelope)value, bytes, big ? Backenddemo.TableByteOrder.Big : Backenddemo.TableByteOrder.Little);
            var ownVocabulary = new Backenddemo.TableVocabulary(); Backenddemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Backenddemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Backenddemo.TableVocabulary(); var inner = new Backenddemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Backenddemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Backenddemo.Envelope[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Backenddemo.Envelope(); }
                if (!inner.Refused && !inner.Malformed) { Backenddemo.Schema.EnvelopeLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Backenddemo.Schema.EnvelopeMeasureMessages(new Backenddemo.Envelope[] { (Backenddemo.Envelope)value });
            c.MessageSave = (value, bytes) => Backenddemo.Schema.EnvelopeSaveMessages(new Backenddemo.Envelope[] { (Backenddemo.Envelope)value }, bytes);
        }
        {
            Codec c = Find("backenddemo", "StorePurchase");
            c.Announcement = Backenddemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Backenddemo.Schema.StorePurchaseCookMeasure((Backenddemo.StorePurchase)value);
            c.Cook = (value, bytes, big) => Backenddemo.Schema.StorePurchaseCook((Backenddemo.StorePurchase)value, bytes, big ? Backenddemo.TableByteOrder.Big : Backenddemo.TableByteOrder.Little);
            var ownVocabulary = new Backenddemo.TableVocabulary(); Backenddemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Backenddemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Backenddemo.TableVocabulary(); var inner = new Backenddemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Backenddemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Backenddemo.StorePurchase[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Backenddemo.StorePurchase(); }
                if (!inner.Refused && !inner.Malformed) { Backenddemo.Schema.StorePurchaseLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Backenddemo.Schema.StorePurchaseMeasureMessages(new Backenddemo.StorePurchase[] { (Backenddemo.StorePurchase)value });
            c.MessageSave = (value, bytes) => Backenddemo.Schema.StorePurchaseSaveMessages(new Backenddemo.StorePurchase[] { (Backenddemo.StorePurchase)value }, bytes);
        }
        {
            Codec c = Find("backenddemo", "MatchResult");
            c.Announcement = Backenddemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Backenddemo.Schema.MatchResultCookMeasure((Backenddemo.MatchResult)value);
            c.Cook = (value, bytes, big) => Backenddemo.Schema.MatchResultCook((Backenddemo.MatchResult)value, bytes, big ? Backenddemo.TableByteOrder.Big : Backenddemo.TableByteOrder.Little);
            var ownVocabulary = new Backenddemo.TableVocabulary(); Backenddemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Backenddemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Backenddemo.TableVocabulary(); var inner = new Backenddemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Backenddemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Backenddemo.MatchResult[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Backenddemo.MatchResult(); }
                if (!inner.Refused && !inner.Malformed) { Backenddemo.Schema.MatchResultLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Backenddemo.Schema.MatchResultMeasureMessages(new Backenddemo.MatchResult[] { (Backenddemo.MatchResult)value });
            c.MessageSave = (value, bytes) => Backenddemo.Schema.MatchResultSaveMessages(new Backenddemo.MatchResult[] { (Backenddemo.MatchResult)value }, bytes);
        }
        {
            Codec c = Find("backenddemo", "LoginRequest");
            c.Announcement = Backenddemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Backenddemo.Schema.LoginRequestCookMeasure((Backenddemo.LoginRequest)value);
            c.Cook = (value, bytes, big) => Backenddemo.Schema.LoginRequestCook((Backenddemo.LoginRequest)value, bytes, big ? Backenddemo.TableByteOrder.Big : Backenddemo.TableByteOrder.Little);
            var ownVocabulary = new Backenddemo.TableVocabulary(); Backenddemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Backenddemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Backenddemo.TableVocabulary(); var inner = new Backenddemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Backenddemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Backenddemo.LoginRequest[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Backenddemo.LoginRequest(); }
                if (!inner.Refused && !inner.Malformed) { Backenddemo.Schema.LoginRequestLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Backenddemo.Schema.LoginRequestMeasureMessages(new Backenddemo.LoginRequest[] { (Backenddemo.LoginRequest)value });
            c.MessageSave = (value, bytes) => Backenddemo.Schema.LoginRequestSaveMessages(new Backenddemo.LoginRequest[] { (Backenddemo.LoginRequest)value }, bytes);
        }
        {
            Codec c = Find("tblg1", "Guarded");
            c.Announcement = Tblg1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblg1.Schema.GuardedCookMeasure((Tblg1.Guarded)value);
            c.Cook = (value, bytes, big) => Tblg1.Schema.GuardedCook((Tblg1.Guarded)value, bytes, big ? Tblg1.TableByteOrder.Big : Tblg1.TableByteOrder.Little);
            var ownVocabulary = new Tblg1.TableVocabulary(); Tblg1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblg1.TableReport());
            c.MessageLoadMeasure = bytes => Tblg1.Schema.GuardedLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblg1.TableVocabulary(); var inner = new Tblg1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblg1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblg1.Guarded[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblg1.Guarded(); }
                if (!inner.Refused && !inner.Malformed) { Tblg1.Schema.GuardedLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblg1.Schema.GuardedMeasureMessages(new Tblg1.Guarded[] { (Tblg1.Guarded)value });
            c.MessageSave = (value, bytes) => Tblg1.Schema.GuardedSaveMessages(new Tblg1.Guarded[] { (Tblg1.Guarded)value }, bytes);
        }
        {
            Codec c = Find("streamdemo", "Feed");
            c.Announcement = Streamdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Streamdemo.Schema.FeedCookMeasure((Streamdemo.Feed)value);
            c.Cook = (value, bytes, big) => Streamdemo.Schema.FeedCook((Streamdemo.Feed)value, bytes, big ? Streamdemo.TableByteOrder.Big : Streamdemo.TableByteOrder.Little);
            var ownVocabulary = new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Streamdemo.TableReport());
            c.MessageLoadMeasure = bytes => Streamdemo.Schema.FeedLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Streamdemo.TableVocabulary(); var inner = new Streamdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Streamdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Streamdemo.Feed[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Streamdemo.Feed(); }
                if (!inner.Refused && !inner.Malformed) { Streamdemo.Schema.FeedLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Streamdemo.Schema.FeedMeasureMessages(new Streamdemo.Feed[] { (Streamdemo.Feed)value });
            c.MessageSave = (value, bytes) => Streamdemo.Schema.FeedSaveMessages(new Streamdemo.Feed[] { (Streamdemo.Feed)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "EdgeRow");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.EdgeRowCookMeasure((Mapdemo.EdgeRow)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.EdgeRowCook((Mapdemo.EdgeRow)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.EdgeRowLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.EdgeRow[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.EdgeRow(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.EdgeRowLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.EdgeRowMeasureMessages(new Mapdemo.EdgeRow[] { (Mapdemo.EdgeRow)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.EdgeRowSaveMessages(new Mapdemo.EdgeRow[] { (Mapdemo.EdgeRow)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "WideRow");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.WideRowCookMeasure((Mapdemo.WideRow)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.WideRowCook((Mapdemo.WideRow)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.WideRowLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.WideRow[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.WideRow(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.WideRowLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.WideRowMeasureMessages(new Mapdemo.WideRow[] { (Mapdemo.WideRow)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.WideRowSaveMessages(new Mapdemo.WideRow[] { (Mapdemo.WideRow)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Row");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.RowCookMeasure((Mapdemo.Row)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.RowCook((Mapdemo.Row)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.RowLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Row[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Row(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.RowLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.RowMeasureMessages(new Mapdemo.Row[] { (Mapdemo.Row)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.RowSaveMessages(new Mapdemo.Row[] { (Mapdemo.Row)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Trails");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.TrailsCookMeasure((Mapdemo.Trails)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.TrailsCook((Mapdemo.Trails)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.TrailsLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Trails[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Trails(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.TrailsLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.TrailsMeasureMessages(new Mapdemo.Trails[] { (Mapdemo.Trails)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.TrailsSaveMessages(new Mapdemo.Trails[] { (Mapdemo.Trails)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Crews");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.CrewsCookMeasure((Mapdemo.Crews)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.CrewsCook((Mapdemo.Crews)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.CrewsLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Crews[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Crews(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.CrewsLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.CrewsMeasureMessages(new Mapdemo.Crews[] { (Mapdemo.Crews)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.CrewsSaveMessages(new Mapdemo.Crews[] { (Mapdemo.Crews)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Pairs");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.PairsCookMeasure((Mapdemo.Pairs)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.PairsCook((Mapdemo.Pairs)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.PairsLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Pairs[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Pairs(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.PairsLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.PairsMeasureMessages(new Mapdemo.Pairs[] { (Mapdemo.Pairs)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.PairsSaveMessages(new Mapdemo.Pairs[] { (Mapdemo.Pairs)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Chunks");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.ChunksCookMeasure((Mapdemo.Chunks)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.ChunksCook((Mapdemo.Chunks)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.ChunksLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Chunks[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Chunks(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.ChunksLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.ChunksMeasureMessages(new Mapdemo.Chunks[] { (Mapdemo.Chunks)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.ChunksSaveMessages(new Mapdemo.Chunks[] { (Mapdemo.Chunks)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Docs");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.DocsCookMeasure((Mapdemo.Docs)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.DocsCook((Mapdemo.Docs)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.DocsLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Docs[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Docs(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.DocsLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.DocsMeasureMessages(new Mapdemo.Docs[] { (Mapdemo.Docs)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.DocsSaveMessages(new Mapdemo.Docs[] { (Mapdemo.Docs)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Spans");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.SpansCookMeasure((Mapdemo.Spans)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.SpansCook((Mapdemo.Spans)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.SpansLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Spans[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Spans(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.SpansLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.SpansMeasureMessages(new Mapdemo.Spans[] { (Mapdemo.Spans)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.SpansSaveMessages(new Mapdemo.Spans[] { (Mapdemo.Spans)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Slots");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.SlotsCookMeasure((Mapdemo.Slots)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.SlotsCook((Mapdemo.Slots)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.SlotsLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Slots[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Slots(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.SlotsLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.SlotsMeasureMessages(new Mapdemo.Slots[] { (Mapdemo.Slots)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.SlotsSaveMessages(new Mapdemo.Slots[] { (Mapdemo.Slots)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Runs");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.RunsCookMeasure((Mapdemo.Runs)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.RunsCook((Mapdemo.Runs)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.RunsLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Runs[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Runs(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.RunsLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.RunsMeasureMessages(new Mapdemo.Runs[] { (Mapdemo.Runs)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.RunsSaveMessages(new Mapdemo.Runs[] { (Mapdemo.Runs)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Cells");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.CellsCookMeasure((Mapdemo.Cells)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.CellsCook((Mapdemo.Cells)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.CellsLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Cells[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Cells(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.CellsLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.CellsMeasureMessages(new Mapdemo.Cells[] { (Mapdemo.Cells)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.CellsSaveMessages(new Mapdemo.Cells[] { (Mapdemo.Cells)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Text");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.TextCookMeasure((Mapdemo.Text)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.TextCook((Mapdemo.Text)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.TextLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Text[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Text(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.TextLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.TextMeasureMessages(new Mapdemo.Text[] { (Mapdemo.Text)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.TextSaveMessages(new Mapdemo.Text[] { (Mapdemo.Text)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Depth");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.DepthCookMeasure((Mapdemo.Depth)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.DepthCook((Mapdemo.Depth)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.DepthLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Depth[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Depth(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.DepthLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.DepthMeasureMessages(new Mapdemo.Depth[] { (Mapdemo.Depth)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.DepthSaveMessages(new Mapdemo.Depth[] { (Mapdemo.Depth)value }, bytes);
        }
        {
            Codec c = Find("mapdemo", "Fleet");
            c.Announcement = Mapdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Mapdemo.Schema.FleetCookMeasure((Mapdemo.Fleet)value);
            c.Cook = (value, bytes, big) => Mapdemo.Schema.FleetCook((Mapdemo.Fleet)value, bytes, big ? Mapdemo.TableByteOrder.Big : Mapdemo.TableByteOrder.Little);
            var ownVocabulary = new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Mapdemo.TableReport());
            c.MessageLoadMeasure = bytes => Mapdemo.Schema.FleetLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Mapdemo.TableVocabulary(); var inner = new Mapdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Mapdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Mapdemo.Fleet[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Mapdemo.Fleet(); }
                if (!inner.Refused && !inner.Malformed) { Mapdemo.Schema.FleetLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Mapdemo.Schema.FleetMeasureMessages(new Mapdemo.Fleet[] { (Mapdemo.Fleet)value });
            c.MessageSave = (value, bytes) => Mapdemo.Schema.FleetSaveMessages(new Mapdemo.Fleet[] { (Mapdemo.Fleet)value }, bytes);
        }
        {
            Codec c = Find("listdemo", "Album");
            c.Announcement = Listdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Listdemo.Schema.AlbumCookMeasure((Listdemo.Album)value);
            c.Cook = (value, bytes, big) => Listdemo.Schema.AlbumCook((Listdemo.Album)value, bytes, big ? Listdemo.TableByteOrder.Big : Listdemo.TableByteOrder.Little);
            var ownVocabulary = new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Listdemo.TableReport());
            c.MessageLoadMeasure = bytes => Listdemo.Schema.AlbumLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Listdemo.TableVocabulary(); var inner = new Listdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Listdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Listdemo.Album[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Listdemo.Album(); }
                if (!inner.Refused && !inner.Malformed) { Listdemo.Schema.AlbumLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Listdemo.Schema.AlbumMeasureMessages(new Listdemo.Album[] { (Listdemo.Album)value });
            c.MessageSave = (value, bytes) => Listdemo.Schema.AlbumSaveMessages(new Listdemo.Album[] { (Listdemo.Album)value }, bytes);
        }
        {
            Codec c = Find("listdemo", "Army");
            c.Announcement = Listdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Listdemo.Schema.ArmyCookMeasure((Listdemo.Army)value);
            c.Cook = (value, bytes, big) => Listdemo.Schema.ArmyCook((Listdemo.Army)value, bytes, big ? Listdemo.TableByteOrder.Big : Listdemo.TableByteOrder.Little);
            var ownVocabulary = new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Listdemo.TableReport());
            c.MessageLoadMeasure = bytes => Listdemo.Schema.ArmyLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Listdemo.TableVocabulary(); var inner = new Listdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Listdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Listdemo.Army[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Listdemo.Army(); }
                if (!inner.Refused && !inner.Malformed) { Listdemo.Schema.ArmyLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Listdemo.Schema.ArmyMeasureMessages(new Listdemo.Army[] { (Listdemo.Army)value });
            c.MessageSave = (value, bytes) => Listdemo.Schema.ArmySaveMessages(new Listdemo.Army[] { (Listdemo.Army)value }, bytes);
        }
        {
            Codec c = Find("listdemo", "Sheet");
            c.Announcement = Listdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Listdemo.Schema.SheetCookMeasure((Listdemo.Sheet)value);
            c.Cook = (value, bytes, big) => Listdemo.Schema.SheetCook((Listdemo.Sheet)value, bytes, big ? Listdemo.TableByteOrder.Big : Listdemo.TableByteOrder.Little);
            var ownVocabulary = new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Listdemo.TableReport());
            c.MessageLoadMeasure = bytes => Listdemo.Schema.SheetLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Listdemo.TableVocabulary(); var inner = new Listdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Listdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Listdemo.Sheet[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Listdemo.Sheet(); }
                if (!inner.Refused && !inner.Malformed) { Listdemo.Schema.SheetLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Listdemo.Schema.SheetMeasureMessages(new Listdemo.Sheet[] { (Listdemo.Sheet)value });
            c.MessageSave = (value, bytes) => Listdemo.Schema.SheetSaveMessages(new Listdemo.Sheet[] { (Listdemo.Sheet)value }, bytes);
        }
        {
            Codec c = Find("listdemo", "Mixed");
            c.Announcement = Listdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Listdemo.Schema.MixedCookMeasure((Listdemo.Mixed)value);
            c.Cook = (value, bytes, big) => Listdemo.Schema.MixedCook((Listdemo.Mixed)value, bytes, big ? Listdemo.TableByteOrder.Big : Listdemo.TableByteOrder.Little);
            var ownVocabulary = new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Listdemo.TableReport());
            c.MessageLoadMeasure = bytes => Listdemo.Schema.MixedLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Listdemo.TableVocabulary(); var inner = new Listdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Listdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Listdemo.Mixed[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Listdemo.Mixed(); }
                if (!inner.Refused && !inner.Malformed) { Listdemo.Schema.MixedLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Listdemo.Schema.MixedMeasureMessages(new Listdemo.Mixed[] { (Listdemo.Mixed)value });
            c.MessageSave = (value, bytes) => Listdemo.Schema.MixedSaveMessages(new Listdemo.Mixed[] { (Listdemo.Mixed)value }, bytes);
        }
        {
            Codec c = Find("listdemo", "Save");
            c.Announcement = Listdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Listdemo.Schema.SaveCookMeasure((Listdemo.Save)value);
            c.Cook = (value, bytes, big) => Listdemo.Schema.SaveCook((Listdemo.Save)value, bytes, big ? Listdemo.TableByteOrder.Big : Listdemo.TableByteOrder.Little);
            var ownVocabulary = new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Listdemo.TableReport());
            c.MessageLoadMeasure = bytes => Listdemo.Schema.SaveLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Listdemo.TableVocabulary(); var inner = new Listdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Listdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Listdemo.Save[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Listdemo.Save(); }
                if (!inner.Refused && !inner.Malformed) { Listdemo.Schema.SaveLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Listdemo.Schema.SaveMeasureMessages(new Listdemo.Save[] { (Listdemo.Save)value });
            c.MessageSave = (value, bytes) => Listdemo.Schema.SaveSaveMessages(new Listdemo.Save[] { (Listdemo.Save)value }, bytes);
        }
        {
            Codec c = Find("tblw2", "Ship");
            c.Announcement = Tblw2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblw2.Schema.ShipCookMeasure((Tblw2.Ship)value);
            c.Cook = (value, bytes, big) => Tblw2.Schema.ShipCook((Tblw2.Ship)value, bytes, big ? Tblw2.TableByteOrder.Big : Tblw2.TableByteOrder.Little);
            var ownVocabulary = new Tblw2.TableVocabulary(); Tblw2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblw2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblw2.TableVocabulary(); var inner = new Tblw2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblw2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblw2.Ship[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblw2.Ship(); }
                if (!inner.Refused && !inner.Malformed) { Tblw2.Schema.ShipLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblw2.Schema.ShipMeasureMessages(new Tblw2.Ship[] { (Tblw2.Ship)value });
            c.MessageSave = (value, bytes) => Tblw2.Schema.ShipSaveMessages(new Tblw2.Ship[] { (Tblw2.Ship)value }, bytes);
        }
        {
            Codec c = Find("tblw2", "Fleet");
            c.Announcement = Tblw2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblw2.Schema.FleetCookMeasure((Tblw2.Fleet)value);
            c.Cook = (value, bytes, big) => Tblw2.Schema.FleetCook((Tblw2.Fleet)value, bytes, big ? Tblw2.TableByteOrder.Big : Tblw2.TableByteOrder.Little);
            var ownVocabulary = new Tblw2.TableVocabulary(); Tblw2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblw2.TableReport());
            c.MessageLoadMeasure = bytes => Tblw2.Schema.FleetLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblw2.TableVocabulary(); var inner = new Tblw2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblw2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblw2.Fleet[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblw2.Fleet(); }
                if (!inner.Refused && !inner.Malformed) { Tblw2.Schema.FleetLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblw2.Schema.FleetMeasureMessages(new Tblw2.Fleet[] { (Tblw2.Fleet)value });
            c.MessageSave = (value, bytes) => Tblw2.Schema.FleetSaveMessages(new Tblw2.Fleet[] { (Tblw2.Fleet)value }, bytes);
        }
        {
            Codec c = Find("tblw1", "Vessel");
            c.Announcement = Tblw1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblw1.Schema.VesselCookMeasure((Tblw1.Vessel)value);
            c.Cook = (value, bytes, big) => Tblw1.Schema.VesselCook((Tblw1.Vessel)value, bytes, big ? Tblw1.TableByteOrder.Big : Tblw1.TableByteOrder.Little);
            var ownVocabulary = new Tblw1.TableVocabulary(); Tblw1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblw1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblw1.TableVocabulary(); var inner = new Tblw1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblw1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblw1.Vessel[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblw1.Vessel(); }
                if (!inner.Refused && !inner.Malformed) { Tblw1.Schema.VesselLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblw1.Schema.VesselMeasureMessages(new Tblw1.Vessel[] { (Tblw1.Vessel)value });
            c.MessageSave = (value, bytes) => Tblw1.Schema.VesselSaveMessages(new Tblw1.Vessel[] { (Tblw1.Vessel)value }, bytes);
        }
        {
            Codec c = Find("tblw1", "Fleet");
            c.Announcement = Tblw1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblw1.Schema.FleetCookMeasure((Tblw1.Fleet)value);
            c.Cook = (value, bytes, big) => Tblw1.Schema.FleetCook((Tblw1.Fleet)value, bytes, big ? Tblw1.TableByteOrder.Big : Tblw1.TableByteOrder.Little);
            var ownVocabulary = new Tblw1.TableVocabulary(); Tblw1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblw1.TableReport());
            c.MessageLoadMeasure = bytes => Tblw1.Schema.FleetLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblw1.TableVocabulary(); var inner = new Tblw1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblw1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblw1.Fleet[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblw1.Fleet(); }
                if (!inner.Refused && !inner.Malformed) { Tblw1.Schema.FleetLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblw1.Schema.FleetMeasureMessages(new Tblw1.Fleet[] { (Tblw1.Fleet)value });
            c.MessageSave = (value, bytes) => Tblw1.Schema.FleetSaveMessages(new Tblw1.Fleet[] { (Tblw1.Fleet)value }, bytes);
        }
        {
            Codec c = Find("tblp2", "Chain");
            c.Announcement = Tblp2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblp2.Schema.ChainCookMeasure((Tblp2.Chain)value);
            c.Cook = (value, bytes, big) => Tblp2.Schema.ChainCook((Tblp2.Chain)value, bytes, big ? Tblp2.TableByteOrder.Big : Tblp2.TableByteOrder.Little);
            var ownVocabulary = new Tblp2.TableVocabulary(); Tblp2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblp2.TableReport());
            c.MessageLoadMeasure = bytes => Tblp2.Schema.ChainLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblp2.TableVocabulary(); var inner = new Tblp2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblp2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblp2.Chain[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblp2.Chain(); }
                if (!inner.Refused && !inner.Malformed) { Tblp2.Schema.ChainLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblp2.Schema.ChainMeasureMessages(new Tblp2.Chain[] { (Tblp2.Chain)value });
            c.MessageSave = (value, bytes) => Tblp2.Schema.ChainSaveMessages(new Tblp2.Chain[] { (Tblp2.Chain)value }, bytes);
        }
        {
            Codec c = Find("blobdemo", "Catalog");
            c.Announcement = Blobdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Blobdemo.Schema.CatalogCookMeasure((Blobdemo.Catalog)value);
            c.Cook = (value, bytes, big) => Blobdemo.Schema.CatalogCook((Blobdemo.Catalog)value, bytes, big ? Blobdemo.TableByteOrder.Big : Blobdemo.TableByteOrder.Little);
            var ownVocabulary = new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Blobdemo.TableReport());
            c.MessageLoadMeasure = bytes => Blobdemo.Schema.CatalogLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Blobdemo.TableVocabulary(); var inner = new Blobdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Blobdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Blobdemo.Catalog[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Blobdemo.Catalog(); }
                if (!inner.Refused && !inner.Malformed) { Blobdemo.Schema.CatalogLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Blobdemo.Schema.CatalogMeasureMessages(new Blobdemo.Catalog[] { (Blobdemo.Catalog)value });
            c.MessageSave = (value, bytes) => Blobdemo.Schema.CatalogSaveMessages(new Blobdemo.Catalog[] { (Blobdemo.Catalog)value }, bytes);
        }
        {
            Codec c = Find("graphdemo", "Album");
            c.Announcement = Graphdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Graphdemo.Schema.AlbumCookMeasure((Graphdemo.Album)value);
            c.Cook = (value, bytes, big) => Graphdemo.Schema.AlbumCook((Graphdemo.Album)value, bytes, big ? Graphdemo.TableByteOrder.Big : Graphdemo.TableByteOrder.Little);
            var ownVocabulary = new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Graphdemo.TableReport());
            c.MessageLoadMeasure = bytes => Graphdemo.Schema.AlbumLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Graphdemo.TableVocabulary(); var inner = new Graphdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Graphdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Graphdemo.Album[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Graphdemo.Album(); }
                if (!inner.Refused && !inner.Malformed) { Graphdemo.Schema.AlbumLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Graphdemo.Schema.AlbumMeasureMessages(new Graphdemo.Album[] { (Graphdemo.Album)value });
            c.MessageSave = (value, bytes) => Graphdemo.Schema.AlbumSaveMessages(new Graphdemo.Album[] { (Graphdemo.Album)value }, bytes);
        }
        {
            Codec c = Find("graphdemo", "Depot");
            c.Announcement = Graphdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Graphdemo.Schema.DepotCookMeasure((Graphdemo.Depot)value);
            c.Cook = (value, bytes, big) => Graphdemo.Schema.DepotCook((Graphdemo.Depot)value, bytes, big ? Graphdemo.TableByteOrder.Big : Graphdemo.TableByteOrder.Little);
            var ownVocabulary = new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Graphdemo.TableReport());
            c.MessageLoadMeasure = bytes => Graphdemo.Schema.DepotLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Graphdemo.TableVocabulary(); var inner = new Graphdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Graphdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Graphdemo.Depot[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Graphdemo.Depot(); }
                if (!inner.Refused && !inner.Malformed) { Graphdemo.Schema.DepotLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Graphdemo.Schema.DepotMeasureMessages(new Graphdemo.Depot[] { (Graphdemo.Depot)value });
            c.MessageSave = (value, bytes) => Graphdemo.Schema.DepotSaveMessages(new Graphdemo.Depot[] { (Graphdemo.Depot)value }, bytes);
        }
        {
            Codec c = Find("graphdemo", "Scene");
            c.Announcement = Graphdemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Graphdemo.Schema.SceneCookMeasure((Graphdemo.Scene)value);
            c.Cook = (value, bytes, big) => Graphdemo.Schema.SceneCook((Graphdemo.Scene)value, bytes, big ? Graphdemo.TableByteOrder.Big : Graphdemo.TableByteOrder.Little);
            var ownVocabulary = new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Graphdemo.TableReport());
            c.MessageLoadMeasure = bytes => Graphdemo.Schema.SceneLoadMeasure(ownVocabulary, bytes);
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Graphdemo.TableVocabulary(); var inner = new Graphdemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Graphdemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Graphdemo.Scene[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Graphdemo.Scene(); }
                if (!inner.Refused && !inner.Malformed) { Graphdemo.Schema.SceneLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Graphdemo.Schema.SceneMeasureMessages(new Graphdemo.Scene[] { (Graphdemo.Scene)value });
            c.MessageSave = (value, bytes) => Graphdemo.Schema.SceneSaveMessages(new Graphdemo.Scene[] { (Graphdemo.Scene)value }, bytes);
        }
        {
            Codec c = Find("widedemo", "Stamp");
            c.Announcement = Wide.Schema.Announce().ToArray();
            c.CookMeasure = value => Wide.Schema.StampCookMeasure((Wide.Stamp)value);
            c.Cook = (value, bytes, big) => Wide.Schema.StampCook((Wide.Stamp)value, bytes, big ? Wide.TableByteOrder.Big : Wide.TableByteOrder.Little);
            var ownVocabulary = new Wide.TableVocabulary(); Wide.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Wide.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Wide.TableVocabulary(); var inner = new Wide.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Wide.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Wide.Stamp[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Wide.Stamp(); }
                if (!inner.Refused && !inner.Malformed) { Wide.Schema.StampLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Wide.Schema.StampMeasureMessages(new Wide.Stamp[] { (Wide.Stamp)value });
            c.MessageSave = (value, bytes) => Wide.Schema.StampSaveMessages(new Wide.Stamp[] { (Wide.Stamp)value }, bytes);
        }
        {
            Codec c = Find("widedemo", "Caption");
            c.Announcement = Wide.Schema.Announce().ToArray();
            c.CookMeasure = value => Wide.Schema.CaptionCookMeasure((Wide.Caption)value);
            c.Cook = (value, bytes, big) => Wide.Schema.CaptionCook((Wide.Caption)value, bytes, big ? Wide.TableByteOrder.Big : Wide.TableByteOrder.Little);
            var ownVocabulary = new Wide.TableVocabulary(); Wide.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Wide.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Wide.TableVocabulary(); var inner = new Wide.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Wide.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Wide.Caption[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Wide.Caption(); }
                if (!inner.Refused && !inner.Malformed) { Wide.Schema.CaptionLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Wide.Schema.CaptionMeasureMessages(new Wide.Caption[] { (Wide.Caption)value });
            c.MessageSave = (value, bytes) => Wide.Schema.CaptionSaveMessages(new Wide.Caption[] { (Wide.Caption)value }, bytes);
        }
        {
            Codec c = Find("tblscalars2", "SimState");
            c.Announcement = Scalardemo2.Schema.Announce().ToArray();
            c.CookMeasure = value => Scalardemo2.Schema.SimStateCookMeasure((Scalardemo2.SimState)value);
            c.Cook = (value, bytes, big) => Scalardemo2.Schema.SimStateCook((Scalardemo2.SimState)value, bytes, big ? Scalardemo2.TableByteOrder.Big : Scalardemo2.TableByteOrder.Little);
            var ownVocabulary = new Scalardemo2.TableVocabulary(); Scalardemo2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Scalardemo2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Scalardemo2.TableVocabulary(); var inner = new Scalardemo2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Scalardemo2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Scalardemo2.SimState[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Scalardemo2.SimState(); }
                if (!inner.Refused && !inner.Malformed) { Scalardemo2.Schema.SimStateLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Scalardemo2.Schema.SimStateMeasureMessages(new Scalardemo2.SimState[] { (Scalardemo2.SimState)value });
            c.MessageSave = (value, bytes) => Scalardemo2.Schema.SimStateSaveMessages(new Scalardemo2.SimState[] { (Scalardemo2.SimState)value }, bytes);
        }
        {
            Codec c = Find("scalars", "Pose");
            c.Announcement = Scalardemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Scalardemo.Schema.PoseCookMeasure((Scalardemo.Pose)value);
            c.Cook = (value, bytes, big) => Scalardemo.Schema.PoseCook((Scalardemo.Pose)value, bytes, big ? Scalardemo.TableByteOrder.Big : Scalardemo.TableByteOrder.Little);
            var ownVocabulary = new Scalardemo.TableVocabulary(); Scalardemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Scalardemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Scalardemo.TableVocabulary(); var inner = new Scalardemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Scalardemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Scalardemo.Pose[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Scalardemo.Pose(); }
                if (!inner.Refused && !inner.Malformed) { Scalardemo.Schema.PoseLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Scalardemo.Schema.PoseMeasureMessages(new Scalardemo.Pose[] { (Scalardemo.Pose)value });
            c.MessageSave = (value, bytes) => Scalardemo.Schema.PoseSaveMessages(new Scalardemo.Pose[] { (Scalardemo.Pose)value }, bytes);
        }
        {
            Codec c = Find("scalars", "SimState");
            c.Announcement = Scalardemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Scalardemo.Schema.SimStateCookMeasure((Scalardemo.SimState)value);
            c.Cook = (value, bytes, big) => Scalardemo.Schema.SimStateCook((Scalardemo.SimState)value, bytes, big ? Scalardemo.TableByteOrder.Big : Scalardemo.TableByteOrder.Little);
            var ownVocabulary = new Scalardemo.TableVocabulary(); Scalardemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Scalardemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Scalardemo.TableVocabulary(); var inner = new Scalardemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Scalardemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Scalardemo.SimState[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Scalardemo.SimState(); }
                if (!inner.Refused && !inner.Malformed) { Scalardemo.Schema.SimStateLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Scalardemo.Schema.SimStateMeasureMessages(new Scalardemo.SimState[] { (Scalardemo.SimState)value });
            c.MessageSave = (value, bytes) => Scalardemo.Schema.SimStateSaveMessages(new Scalardemo.SimState[] { (Scalardemo.SimState)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "User");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.UserCookMeasure((Messagedemo.User)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.UserCook((Messagedemo.User)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.User[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.User(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.UserLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.UserMeasureMessages(new Messagedemo.User[] { (Messagedemo.User)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.UserSaveMessages(new Messagedemo.User[] { (Messagedemo.User)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "Script");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.ScriptCookMeasure((Messagedemo.Script)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.ScriptCook((Messagedemo.Script)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.Script[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.Script(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.ScriptLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.ScriptMeasureMessages(new Messagedemo.Script[] { (Messagedemo.Script)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.ScriptSaveMessages(new Messagedemo.Script[] { (Messagedemo.Script)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "Selection");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.SelectionCookMeasure((Messagedemo.Selection)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.SelectionCook((Messagedemo.Selection)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.Selection[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.Selection(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.SelectionLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.SelectionMeasureMessages(new Messagedemo.Selection[] { (Messagedemo.Selection)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.SelectionSaveMessages(new Messagedemo.Selection[] { (Messagedemo.Selection)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "InsertText");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.InsertTextCookMeasure((Messagedemo.InsertText)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.InsertTextCook((Messagedemo.InsertText)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.InsertText[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.InsertText(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.InsertTextLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.InsertTextMeasureMessages(new Messagedemo.InsertText[] { (Messagedemo.InsertText)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.InsertTextSaveMessages(new Messagedemo.InsertText[] { (Messagedemo.InsertText)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "RemoveText");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.RemoveTextCookMeasure((Messagedemo.RemoveText)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.RemoveTextCook((Messagedemo.RemoveText)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.RemoveText[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.RemoveText(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.RemoveTextLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.RemoveTextMeasureMessages(new Messagedemo.RemoveText[] { (Messagedemo.RemoveText)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.RemoveTextSaveMessages(new Messagedemo.RemoveText[] { (Messagedemo.RemoveText)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "Edit");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.EditCookMeasure((Messagedemo.Edit)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.EditCook((Messagedemo.Edit)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.Edit[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.Edit(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.EditLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.EditMeasureMessages(new Messagedemo.Edit[] { (Messagedemo.Edit)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.EditSaveMessages(new Messagedemo.Edit[] { (Messagedemo.Edit)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "OpenDocument");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.OpenDocumentCookMeasure((Messagedemo.OpenDocument)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.OpenDocumentCook((Messagedemo.OpenDocument)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.OpenDocument[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.OpenDocument(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.OpenDocumentLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.OpenDocumentMeasureMessages(new Messagedemo.OpenDocument[] { (Messagedemo.OpenDocument)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.OpenDocumentSaveMessages(new Messagedemo.OpenDocument[] { (Messagedemo.OpenDocument)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "SaveDocument");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.SaveDocumentCookMeasure((Messagedemo.SaveDocument)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.SaveDocumentCook((Messagedemo.SaveDocument)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.SaveDocument[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.SaveDocument(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.SaveDocumentLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.SaveDocumentMeasureMessages(new Messagedemo.SaveDocument[] { (Messagedemo.SaveDocument)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.SaveDocumentSaveMessages(new Messagedemo.SaveDocument[] { (Messagedemo.SaveDocument)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "Transaction");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.TransactionCookMeasure((Messagedemo.Transaction)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.TransactionCook((Messagedemo.Transaction)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.Transaction[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.Transaction(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.TransactionLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.TransactionMeasureMessages(new Messagedemo.Transaction[] { (Messagedemo.Transaction)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.TransactionSaveMessages(new Messagedemo.Transaction[] { (Messagedemo.Transaction)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "ToolMessage");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.ToolMessageCookMeasure((Messagedemo.ToolMessage)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.ToolMessageCook((Messagedemo.ToolMessage)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.ToolMessage[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.ToolMessage(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.ToolMessageLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.ToolMessageMeasureMessages(new Messagedemo.ToolMessage[] { (Messagedemo.ToolMessage)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.ToolMessageSaveMessages(new Messagedemo.ToolMessage[] { (Messagedemo.ToolMessage)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "Cursor");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.CursorCookMeasure((Messagedemo.Cursor)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.CursorCook((Messagedemo.Cursor)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.Cursor[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.Cursor(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.CursorLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.CursorMeasureMessages(new Messagedemo.Cursor[] { (Messagedemo.Cursor)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.CursorSaveMessages(new Messagedemo.Cursor[] { (Messagedemo.Cursor)value }, bytes);
        }
        {
            Codec c = Find("messagedemo", "Ping");
            c.Announcement = Messagedemo.Schema.Announce().ToArray();
            c.CookMeasure = value => Messagedemo.Schema.PingCookMeasure((Messagedemo.Ping)value);
            c.Cook = (value, bytes, big) => Messagedemo.Schema.PingCook((Messagedemo.Ping)value, bytes, big ? Messagedemo.TableByteOrder.Big : Messagedemo.TableByteOrder.Little);
            var ownVocabulary = new Messagedemo.TableVocabulary(); Messagedemo.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Messagedemo.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Messagedemo.TableVocabulary(); var inner = new Messagedemo.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Messagedemo.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Messagedemo.Ping[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Messagedemo.Ping(); }
                if (!inner.Refused && !inner.Malformed) { Messagedemo.Schema.PingLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Messagedemo.Schema.PingMeasureMessages(new Messagedemo.Ping[] { (Messagedemo.Ping)value });
            c.MessageSave = (value, bytes) => Messagedemo.Schema.PingSaveMessages(new Messagedemo.Ping[] { (Messagedemo.Ping)value }, bytes);
        }
        {
            Codec c = Find("tblm1", "Open");
            c.Announcement = Tblm1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm1.Schema.OpenCookMeasure((Tblm1.Open)value);
            c.Cook = (value, bytes, big) => Tblm1.Schema.OpenCook((Tblm1.Open)value, bytes, big ? Tblm1.TableByteOrder.Big : Tblm1.TableByteOrder.Little);
            var ownVocabulary = new Tblm1.TableVocabulary(); Tblm1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm1.TableVocabulary(); var inner = new Tblm1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm1.Open[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm1.Open(); }
                if (!inner.Refused && !inner.Malformed) { Tblm1.Schema.OpenLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm1.Schema.OpenMeasureMessages(new Tblm1.Open[] { (Tblm1.Open)value });
            c.MessageSave = (value, bytes) => Tblm1.Schema.OpenSaveMessages(new Tblm1.Open[] { (Tblm1.Open)value }, bytes);
        }
        {
            Codec c = Find("tblm1", "Save");
            c.Announcement = Tblm1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm1.Schema.SaveCookMeasure((Tblm1.Save)value);
            c.Cook = (value, bytes, big) => Tblm1.Schema.SaveCook((Tblm1.Save)value, bytes, big ? Tblm1.TableByteOrder.Big : Tblm1.TableByteOrder.Little);
            var ownVocabulary = new Tblm1.TableVocabulary(); Tblm1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm1.TableVocabulary(); var inner = new Tblm1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm1.Save[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm1.Save(); }
                if (!inner.Refused && !inner.Malformed) { Tblm1.Schema.SaveLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm1.Schema.SaveMeasureMessages(new Tblm1.Save[] { (Tblm1.Save)value });
            c.MessageSave = (value, bytes) => Tblm1.Schema.SaveSaveMessages(new Tblm1.Save[] { (Tblm1.Save)value }, bytes);
        }
        {
            Codec c = Find("tblm1", "Quit");
            c.Announcement = Tblm1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm1.Schema.QuitCookMeasure((Tblm1.Quit)value);
            c.Cook = (value, bytes, big) => Tblm1.Schema.QuitCook((Tblm1.Quit)value, bytes, big ? Tblm1.TableByteOrder.Big : Tblm1.TableByteOrder.Little);
            var ownVocabulary = new Tblm1.TableVocabulary(); Tblm1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm1.TableVocabulary(); var inner = new Tblm1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm1.Quit[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm1.Quit(); }
                if (!inner.Refused && !inner.Malformed) { Tblm1.Schema.QuitLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm1.Schema.QuitMeasureMessages(new Tblm1.Quit[] { (Tblm1.Quit)value });
            c.MessageSave = (value, bytes) => Tblm1.Schema.QuitSaveMessages(new Tblm1.Quit[] { (Tblm1.Quit)value }, bytes);
        }
        {
            Codec c = Find("tblm1", "Msg");
            c.Announcement = Tblm1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm1.Schema.MsgCookMeasure((Tblm1.Msg)value);
            c.Cook = (value, bytes, big) => Tblm1.Schema.MsgCook((Tblm1.Msg)value, bytes, big ? Tblm1.TableByteOrder.Big : Tblm1.TableByteOrder.Little);
            var ownVocabulary = new Tblm1.TableVocabulary(); Tblm1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm1.TableVocabulary(); var inner = new Tblm1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm1.Msg[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm1.Msg(); }
                if (!inner.Refused && !inner.Malformed) { Tblm1.Schema.MsgLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm1.Schema.MsgMeasureMessages(new Tblm1.Msg[] { (Tblm1.Msg)value });
            c.MessageSave = (value, bytes) => Tblm1.Schema.MsgSaveMessages(new Tblm1.Msg[] { (Tblm1.Msg)value }, bytes);
        }
        {
            Codec c = Find("tblm2", "Open");
            c.Announcement = Tblm2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm2.Schema.OpenCookMeasure((Tblm2.Open)value);
            c.Cook = (value, bytes, big) => Tblm2.Schema.OpenCook((Tblm2.Open)value, bytes, big ? Tblm2.TableByteOrder.Big : Tblm2.TableByteOrder.Little);
            var ownVocabulary = new Tblm2.TableVocabulary(); Tblm2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm2.TableVocabulary(); var inner = new Tblm2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm2.Open[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm2.Open(); }
                if (!inner.Refused && !inner.Malformed) { Tblm2.Schema.OpenLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm2.Schema.OpenMeasureMessages(new Tblm2.Open[] { (Tblm2.Open)value });
            c.MessageSave = (value, bytes) => Tblm2.Schema.OpenSaveMessages(new Tblm2.Open[] { (Tblm2.Open)value }, bytes);
        }
        {
            Codec c = Find("tblm2", "Close");
            c.Announcement = Tblm2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm2.Schema.CloseCookMeasure((Tblm2.Close)value);
            c.Cook = (value, bytes, big) => Tblm2.Schema.CloseCook((Tblm2.Close)value, bytes, big ? Tblm2.TableByteOrder.Big : Tblm2.TableByteOrder.Little);
            var ownVocabulary = new Tblm2.TableVocabulary(); Tblm2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm2.TableVocabulary(); var inner = new Tblm2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm2.Close[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm2.Close(); }
                if (!inner.Refused && !inner.Malformed) { Tblm2.Schema.CloseLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm2.Schema.CloseMeasureMessages(new Tblm2.Close[] { (Tblm2.Close)value });
            c.MessageSave = (value, bytes) => Tblm2.Schema.CloseSaveMessages(new Tblm2.Close[] { (Tblm2.Close)value }, bytes);
        }
        {
            Codec c = Find("tblm2", "Quit");
            c.Announcement = Tblm2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm2.Schema.QuitCookMeasure((Tblm2.Quit)value);
            c.Cook = (value, bytes, big) => Tblm2.Schema.QuitCook((Tblm2.Quit)value, bytes, big ? Tblm2.TableByteOrder.Big : Tblm2.TableByteOrder.Little);
            var ownVocabulary = new Tblm2.TableVocabulary(); Tblm2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm2.TableVocabulary(); var inner = new Tblm2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm2.Quit[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm2.Quit(); }
                if (!inner.Refused && !inner.Malformed) { Tblm2.Schema.QuitLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm2.Schema.QuitMeasureMessages(new Tblm2.Quit[] { (Tblm2.Quit)value });
            c.MessageSave = (value, bytes) => Tblm2.Schema.QuitSaveMessages(new Tblm2.Quit[] { (Tblm2.Quit)value }, bytes);
        }
        {
            Codec c = Find("tblm2", "Msg");
            c.Announcement = Tblm2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblm2.Schema.MsgCookMeasure((Tblm2.Msg)value);
            c.Cook = (value, bytes, big) => Tblm2.Schema.MsgCook((Tblm2.Msg)value, bytes, big ? Tblm2.TableByteOrder.Big : Tblm2.TableByteOrder.Little);
            var ownVocabulary = new Tblm2.TableVocabulary(); Tblm2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblm2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblm2.TableVocabulary(); var inner = new Tblm2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblm2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblm2.Msg[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblm2.Msg(); }
                if (!inner.Refused && !inner.Malformed) { Tblm2.Schema.MsgLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblm2.Schema.MsgMeasureMessages(new Tblm2.Msg[] { (Tblm2.Msg)value });
            c.MessageSave = (value, bytes) => Tblm2.Schema.MsgSaveMessages(new Tblm2.Msg[] { (Tblm2.Msg)value }, bytes);
        }
        {
            Codec c = Find("tbla1", "Body");
            c.Announcement = Tbla1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tbla1.Schema.BodyCookMeasure((Tbla1.Body)value);
            c.Cook = (value, bytes, big) => Tbla1.Schema.BodyCook((Tbla1.Body)value, bytes, big ? Tbla1.TableByteOrder.Big : Tbla1.TableByteOrder.Little);
            var ownVocabulary = new Tbla1.TableVocabulary(); Tbla1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tbla1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tbla1.TableVocabulary(); var inner = new Tbla1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tbla1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tbla1.Body[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tbla1.Body(); }
                if (!inner.Refused && !inner.Malformed) { Tbla1.Schema.BodyLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tbla1.Schema.BodyMeasureMessages(new Tbla1.Body[] { (Tbla1.Body)value });
            c.MessageSave = (value, bytes) => Tbla1.Schema.BodySaveMessages(new Tbla1.Body[] { (Tbla1.Body)value }, bytes);
        }
        {
            Codec c = Find("tbla1", "Root");
            c.Announcement = Tbla1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tbla1.Schema.RootCookMeasure((Tbla1.Root)value);
            c.Cook = (value, bytes, big) => Tbla1.Schema.RootCook((Tbla1.Root)value, bytes, big ? Tbla1.TableByteOrder.Big : Tbla1.TableByteOrder.Little);
            var ownVocabulary = new Tbla1.TableVocabulary(); Tbla1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tbla1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tbla1.TableVocabulary(); var inner = new Tbla1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tbla1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tbla1.Root[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tbla1.Root(); }
                if (!inner.Refused && !inner.Malformed) { Tbla1.Schema.RootLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tbla1.Schema.RootMeasureMessages(new Tbla1.Root[] { (Tbla1.Root)value });
            c.MessageSave = (value, bytes) => Tbla1.Schema.RootSaveMessages(new Tbla1.Root[] { (Tbla1.Root)value }, bytes);
        }
        {
            Codec c = Find("tbla2", "Body");
            c.Announcement = Tbla2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tbla2.Schema.BodyCookMeasure((Tbla2.Body)value);
            c.Cook = (value, bytes, big) => Tbla2.Schema.BodyCook((Tbla2.Body)value, bytes, big ? Tbla2.TableByteOrder.Big : Tbla2.TableByteOrder.Little);
            var ownVocabulary = new Tbla2.TableVocabulary(); Tbla2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tbla2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tbla2.TableVocabulary(); var inner = new Tbla2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tbla2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tbla2.Body[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tbla2.Body(); }
                if (!inner.Refused && !inner.Malformed) { Tbla2.Schema.BodyLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tbla2.Schema.BodyMeasureMessages(new Tbla2.Body[] { (Tbla2.Body)value });
            c.MessageSave = (value, bytes) => Tbla2.Schema.BodySaveMessages(new Tbla2.Body[] { (Tbla2.Body)value }, bytes);
        }
        {
            Codec c = Find("tbla2", "Root");
            c.Announcement = Tbla2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tbla2.Schema.RootCookMeasure((Tbla2.Root)value);
            c.Cook = (value, bytes, big) => Tbla2.Schema.RootCook((Tbla2.Root)value, bytes, big ? Tbla2.TableByteOrder.Big : Tbla2.TableByteOrder.Little);
            var ownVocabulary = new Tbla2.TableVocabulary(); Tbla2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tbla2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tbla2.TableVocabulary(); var inner = new Tbla2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tbla2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tbla2.Root[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tbla2.Root(); }
                if (!inner.Refused && !inner.Malformed) { Tbla2.Schema.RootLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tbla2.Schema.RootMeasureMessages(new Tbla2.Root[] { (Tbla2.Root)value });
            c.MessageSave = (value, bytes) => Tbla2.Schema.RootSaveMessages(new Tbla2.Root[] { (Tbla2.Root)value }, bytes);
        }
        {
            Codec c = Find("tblk1", "Root");
            c.Announcement = Tblk1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblk1.Schema.RootCookMeasure((Tblk1.Root)value);
            c.Cook = (value, bytes, big) => Tblk1.Schema.RootCook((Tblk1.Root)value, bytes, big ? Tblk1.TableByteOrder.Big : Tblk1.TableByteOrder.Little);
            var ownVocabulary = new Tblk1.TableVocabulary(); Tblk1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblk1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblk1.TableVocabulary(); var inner = new Tblk1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblk1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblk1.Root[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblk1.Root(); }
                if (!inner.Refused && !inner.Malformed) { Tblk1.Schema.RootLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblk1.Schema.RootMeasureMessages(new Tblk1.Root[] { (Tblk1.Root)value });
            c.MessageSave = (value, bytes) => Tblk1.Schema.RootSaveMessages(new Tblk1.Root[] { (Tblk1.Root)value }, bytes);
        }
        {
            Codec c = Find("tblk2", "Root");
            c.Announcement = Tblk2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblk2.Schema.RootCookMeasure((Tblk2.Root)value);
            c.Cook = (value, bytes, big) => Tblk2.Schema.RootCook((Tblk2.Root)value, bytes, big ? Tblk2.TableByteOrder.Big : Tblk2.TableByteOrder.Little);
            var ownVocabulary = new Tblk2.TableVocabulary(); Tblk2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblk2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblk2.TableVocabulary(); var inner = new Tblk2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblk2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblk2.Root[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblk2.Root(); }
                if (!inner.Refused && !inner.Malformed) { Tblk2.Schema.RootLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblk2.Schema.RootMeasureMessages(new Tblk2.Root[] { (Tblk2.Root)value });
            c.MessageSave = (value, bytes) => Tblk2.Schema.RootSaveMessages(new Tblk2.Root[] { (Tblk2.Root)value }, bytes);
        }
        {
            Codec c = Find("tblv1", "Cfg");
            c.Announcement = Tblv1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblv1.Schema.CfgCookMeasure((Tblv1.Cfg)value);
            c.Cook = (value, bytes, big) => Tblv1.Schema.CfgCook((Tblv1.Cfg)value, bytes, big ? Tblv1.TableByteOrder.Big : Tblv1.TableByteOrder.Little);
            var ownVocabulary = new Tblv1.TableVocabulary(); Tblv1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblv1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblv1.TableVocabulary(); var inner = new Tblv1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblv1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblv1.Cfg[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblv1.Cfg(); }
                if (!inner.Refused && !inner.Malformed) { Tblv1.Schema.CfgLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblv1.Schema.CfgMeasureMessages(new Tblv1.Cfg[] { (Tblv1.Cfg)value });
            c.MessageSave = (value, bytes) => Tblv1.Schema.CfgSaveMessages(new Tblv1.Cfg[] { (Tblv1.Cfg)value }, bytes);
        }
        {
            Codec c = Find("tblv2", "Cfg");
            c.Announcement = Tblv2.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblv2.Schema.CfgCookMeasure((Tblv2.Cfg)value);
            c.Cook = (value, bytes, big) => Tblv2.Schema.CfgCook((Tblv2.Cfg)value, bytes, big ? Tblv2.TableByteOrder.Big : Tblv2.TableByteOrder.Little);
            var ownVocabulary = new Tblv2.TableVocabulary(); Tblv2.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblv2.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblv2.TableVocabulary(); var inner = new Tblv2.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblv2.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblv2.Cfg[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblv2.Cfg(); }
                if (!inner.Refused && !inner.Malformed) { Tblv2.Schema.CfgLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblv2.Schema.CfgMeasureMessages(new Tblv2.Cfg[] { (Tblv2.Cfg)value });
            c.MessageSave = (value, bytes) => Tblv2.Schema.CfgSaveMessages(new Tblv2.Cfg[] { (Tblv2.Cfg)value }, bytes);
        }
        {
            Codec c = Find("tblp1", "Chain");
            c.Announcement = Tblp1.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblp1.Schema.ChainCookMeasure((Tblp1.Chain)value);
            c.Cook = (value, bytes, big) => Tblp1.Schema.ChainCook((Tblp1.Chain)value, bytes, big ? Tblp1.TableByteOrder.Big : Tblp1.TableByteOrder.Little);
            var ownVocabulary = new Tblp1.TableVocabulary(); Tblp1.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblp1.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblp1.TableVocabulary(); var inner = new Tblp1.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblp1.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblp1.Chain[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblp1.Chain(); }
                if (!inner.Refused && !inner.Malformed) { Tblp1.Schema.ChainLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblp1.Schema.ChainMeasureMessages(new Tblp1.Chain[] { (Tblp1.Chain)value });
            c.MessageSave = (value, bytes) => Tblp1.Schema.ChainSaveMessages(new Tblp1.Chain[] { (Tblp1.Chain)value }, bytes);
        }
        {
            Codec c = Find("tblp3", "Chain");
            c.Announcement = Tblp3.Schema.Announce().ToArray();
            c.CookMeasure = value => Tblp3.Schema.ChainCookMeasure((Tblp3.Chain)value);
            c.Cook = (value, bytes, big) => Tblp3.Schema.ChainCook((Tblp3.Chain)value, bytes, big ? Tblp3.TableByteOrder.Big : Tblp3.TableByteOrder.Little);
            var ownVocabulary = new Tblp3.TableVocabulary(); Tblp3.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Tblp3.TableReport());
            c.MessageLoad = (announcement, bytes, report) =>
            {
                var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Tblp3.TableVocabulary(); var inner = new Tblp3.TableReport();
                if (!ReferenceEquals(announcement, c.Announcement)) { Tblp3.Schema.AnnounceRead(vocabulary, announcement, inner); }
                var values = new Tblp3.Chain[256]; for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new Tblp3.Chain(); }
                if (!inner.Refused && !inner.Malformed) { Tblp3.Schema.ChainLoadMessages(values, bytes, vocabulary, inner, out _); }
                Fill(report, Copy(inner)); return values[0];
            };
            c.MessageMeasure = value => Tblp3.Schema.ChainMeasureMessages(new Tblp3.Chain[] { (Tblp3.Chain)value });
            c.MessageSave = (value, bytes) => Tblp3.Schema.ChainSaveMessages(new Tblp3.Chain[] { (Tblp3.Chain)value }, bytes);
        }
    }

    static int WireFuzz()
    {
        using var scratch=new RetainScratch();
        RegisterRetains(scratch);
        using BinaryReader input = new BinaryReader(Console.OpenStandardInput());
        using BinaryWriter output = new BinaryWriter(Console.OpenStandardOutput());
        uint n = input.ReadUInt32();
        Codec[] roster = new Codec[n];
        byte[] forms = new byte[n];
        bool[] retaining=new bool[n];
        for (int i = 0; i < n; i++)
        {
            string unit = Encoding.UTF8.GetString(input.ReadBytes(input.ReadUInt16()));
            string root = Encoding.UTF8.GetString(input.ReadBytes(input.ReadUInt16()));
            byte form = input.ReadByte(), retain = input.ReadByte();
            Codec found=Find(unit,root);
            roster[i] = retain == 0 || found?.Retain!=null?found:null;
            retaining[i]=retain!=0;
            forms[i] = form;
            output.Write((byte)(roster[i] != null ? 1 : 0));
        }
        output.Flush();
        for (;;)
        {
            uint index;
            try { index = input.ReadUInt32(); } catch (EndOfStreamException) { return 0; }
            byte[] bytes = input.ReadBytes(checked((int)input.ReadUInt32()));
            Codec codec = roster[index];
            if(retaining[index])
            {
                RetainAnswer answer=codec.Retain(bytes); Report r=answer.Report;
                output.Write((byte)(answer.Loaded?1:0));
                output.Write(r.Unknown); output.Write(r.KindMismatch); output.Write(r.Widened); output.Write(r.Clamped); output.Write(r.Duplicate);
                output.Write((byte)(r.Malformed?1:0)); output.Write((byte)(r.Refused?1:0)); output.Write(answer.Need);
                output.Write(answer.Kept); output.Write(answer.Lost); output.Write(answer.Written);
                if(answer.Written>0) { output.Write(answer.Saved,0,(int)answer.Written); }
                output.Flush(); continue;
            }
            Report report = new Report();
            bool message = forms[index] == 2;
            long need = message ? (codec.MessageLoadMeasure == null ? -1 : codec.MessageLoadMeasure(bytes)) : codec.LoadMeasure == null ? -1 : codec.LoadMeasure(bytes);
            object value = message ? codec.MessageLoad(codec.Announcement, bytes, report) : codec.PartialLoad(bytes, report);
            bool loaded = (message ? codec.MessageLoadMeasure == null : codec.LoadMeasure == null) || need >= 0;
            output.Write((byte)(loaded ? 1 : 0));
            output.Write(report.Unknown); output.Write(report.KindMismatch); output.Write(report.Widened);
            output.Write(report.Clamped); output.Write(report.Duplicate);
            output.Write((byte)(report.Malformed ? 1 : 0)); output.Write((byte)(report.Refused ? 1 : 0));
            output.Write(need); output.Write(0); output.Write(0);
            long size = loaded ? (message ? codec.MessageMeasure(value) : codec.Measure(value)) : -1;
            byte[] saved = size < 0 ? Array.Empty<byte>() : new byte[checked((int)size)];
            long written = size < 0 ? -1 : (message ? codec.MessageSave(value, saved) : codec.Save(value, saved));
            output.Write(written);
            if (written > 0) { output.Write(saved, 0, checked((int)written)); }
            output.Flush();
        }
    }

    static int Main(string[] args)
    {
        RegisterMessages();
        if(args.Length==1 && args[0]=="wire-fuzz-region") { RegisterRegions(); return WireFuzz(); }
        if (args.Length == 1 && args[0] == "wire-fuzz") { return WireFuzz(); }
        if (args.Length < 2)
        {
            Console.Error.WriteLine("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>");
            return 2;
        }
        ReadManifest(args[0]);
        string surface = args[1];
        if (surface == "list")
        {
            // Form-1 wire and its five conformance surfaces. Unsupported constructs
            // remain explicit per-case absences, as in the other language drivers.
            Console.Out.Write("wire\nmessage\ncook-write\nreport\njson-read\njson-write\njson-hostile\nblock\nblock-foreign\nblock-dump\nforgery\nblock-reason\nretain\nretain-save\n");
            return 0;
        }
        if (args.Length < 3)
        {
            Console.Error.WriteLine("usage: driver <manifest> <surface> <outdir>");
            return 2;
        }
        string outDir = args[2];
        switch (surface)
        {
            case "retain": return SurfaceRetain(outDir,false);
            case "retain-save": return SurfaceRetain(outDir,true);
            case "wire": return SurfaceWire(outDir);
            case "message": return SurfaceMessage(outDir);
            case "cook-write": return SurfaceCookWrite(outDir);
            case "report": return SurfaceReport(outDir);
            case "json-read": return SurfaceJsonRead(outDir);
            case "json-write": return SurfaceJsonWrite(outDir);
            case "json-hostile": return SurfaceJsonHostile(outDir);
            case "block": return SurfaceBlock(outDir);
            case "block-foreign": return SurfaceBlockForeign(outDir);
            case "block-dump": return SurfaceBlockDump(outDir);
            case "forgery": return SurfaceForgery(outDir);
            case "block-reason": return SurfaceBlockReason(outDir);
            default: return 2;
        }
    }
}
