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

static class Program
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
        public int Unknown, KindMismatch, Clamped, Duplicate;
        public bool Malformed;
    }

    sealed class Codec
    {
        public string Unit;
        public string Root;
        public Func<byte[], Report, object> Load;   // null on refusal
        public Func<object, long> Measure;
        public Func<object, byte[], long> Save;
        // the TEXT form (docs/SPEC-TABLES.md §16), the same three per row
        public Func<byte[], Report, object> FromJson; // null on refusal
        public Func<object, long> ToJsonMeasure;
        public Func<object, byte[], long> ToJson;
    }

    // Each unit declares its own TableReport, so the driver carries one report
    // shape and every row copies into it — five counters is the whole of §4.
    static Report Copy(Tabledemo.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblv1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblv2.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblp1.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }
    static Report Copy(Tblp3.TableReport r)
    {
        return new Report { Unknown = r.Unknown, KindMismatch = r.KindMismatch, Clamped = r.Clamped, Duplicate = r.Duplicate, Malformed = r.Malformed };
    }

    static readonly List<Codec> codecs = new List<Codec>
    {
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
        FromJsonOf<TValue, TReport> fromJson, Func<TValue, long> toJsonMeasure, SaveOf<TValue> toJson)
        where TValue : new()
    {
        return new Codec
        {
            Unit = unit,
            Root = root,
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
                : report.Unknown + "," + report.KindMismatch + "," + report.Clamped + "," +
                  report.Duplicate + ",false\n";
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
            bool malformed = report.Malformed || value == null;
            string text = report.Unknown + "," + report.KindMismatch + "," + report.Clamped + "," +
                          report.Duplicate + "," + (malformed ? "true" : "false") + "\n";
            File.WriteAllText(Path.Combine(outDir, f[1]), text);
        }
        return 0;
    }

    // A block's base is 64-byte aligned by construction (§19.1) and `extent` is
    // the length the CALLER claims, which a forgery may set past the bytes the
    // image carries. The allocation is the claim, so a reader that walks past
    // what it was given walks into memory this process owns and nothing else's.
    static unsafe string OpenBlock(string name, byte[] bytes, long extent)
    {
        long claim = extent < 0 || extent < bytes.Length ? bytes.Length : extent;
        IntPtr raw = Marshal.AllocHGlobal(new IntPtr(claim + 64));
        try
        {
            long aligned = ((long)raw + 63) & ~63L;
            IntPtr pointer = new IntPtr(aligned);
            new Span<byte>((void*)pointer, (int)Math.Min(claim, int.MaxValue)).Clear();
            Marshal.Copy(bytes, 0, pointer, bytes.Length);
            bool opened;
            if (name.StartsWith("block_render", StringComparison.Ordinal))
            {
                Blockdemo.RenderFrameBlock block;
                opened = Blockdemo.RenderFrameBlock.Open(out block, pointer, claim);
            }
            else if (name.StartsWith("block_padded", StringComparison.Ordinal))
            {
                Blockdemo.PaddedFrameBlock block;
                opened = Blockdemo.PaddedFrameBlock.Open(out block, pointer, claim);
            }
            else
            {
                Console.Error.WriteLine("driver: no block named " + name);
                Environment.Exit(1);
                return "";
            }
            return opened ? "open\n" : "refuse\n";
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

    static int Main(string[] args)
    {
        if (args.Length < 2)
        {
            Console.Error.WriteLine("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>");
            return 2;
        }
        ReadManifest(args[0]);
        string surface = args[1];
        if (surface == "list")
        {
            Console.Out.Write("wire\nreport\njson-read\njson-write\njson-hostile\nblock\nblock-foreign\nblock-dump\nforgery\n");
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
            case "wire": return SurfaceWire(outDir);
            case "report": return SurfaceReport(outDir);
            case "json-read": return SurfaceJsonRead(outDir);
            case "json-write": return SurfaceJsonWrite(outDir);
            case "json-hostile": return SurfaceJsonHostile(outDir);
            case "block": return SurfaceBlock(outDir);
            case "block-foreign": return SurfaceBlockForeign(outDir);
            case "block-dump": return SurfaceBlockDump(outDir);
            case "forgery": return SurfaceForgery(outDir);
            default: return 2;
        }
    }
}
