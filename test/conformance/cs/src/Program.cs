// THE C# CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp, and it is deliberately the same
// shape: one process per surface, every expectation in the data, nothing
// literal here. What it does NOT list is as load-bearing as what it does — this
// backend has no text form (SPEC-TABLES.md §16's backend status), so it lists
// neither json-read nor json-write and the matrix prints them ABSENT. A missing
// feature and a failing test are different facts and the matrix keeps them apart.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>

using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;

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
        Demo<Tabledemo.RootConfig>("RootConfig", Tabledemo.Schema.RootConfigLoad, Tabledemo.Schema.RootConfigMeasure, Tabledemo.Schema.RootConfigSave),
        Demo<Tabledemo.ProfileConfig>("ProfileConfig", Tabledemo.Schema.ProfileConfigLoad, Tabledemo.Schema.ProfileConfigMeasure, Tabledemo.Schema.ProfileConfigSave),
        Demo<Tabledemo.LoadoutConfig>("LoadoutConfig", Tabledemo.Schema.LoadoutConfigLoad, Tabledemo.Schema.LoadoutConfigMeasure, Tabledemo.Schema.LoadoutConfigSave),
        Demo<Tabledemo.WideBlob>("WideBlob", Tabledemo.Schema.WideBlobLoad, Tabledemo.Schema.WideBlobMeasure, Tabledemo.Schema.WideBlobSave),
        Demo<Tabledemo.ArchiveConfig>("ArchiveConfig", Tabledemo.Schema.ArchiveConfigLoad, Tabledemo.Schema.ArchiveConfigMeasure, Tabledemo.Schema.ArchiveConfigSave),
        Demo<Tabledemo.KeyedConfig>("KeyedConfig", Tabledemo.Schema.KeyedConfigLoad, Tabledemo.Schema.KeyedConfigMeasure, Tabledemo.Schema.KeyedConfigSave),
        V1(), V2(), P1(), P3(),
    };

    delegate bool LoadOf<TValue, TReport>(TValue value, ReadOnlySpan<byte> bytes, TReport report);
    delegate long SaveOf<TValue>(TValue value, Span<byte> buffer);

    static Codec Demo<T>(string root, LoadOf<T, Tabledemo.TableReport> load, Func<T, long> measure, SaveOf<T> save)
        where T : new()
    {
        return new Codec
        {
            Unit = "tabledemo",
            Root = root,
            Load = (bytes, report) =>
            {
                T value = new T();
                Tabledemo.TableReport inner = new Tabledemo.TableReport();
                bool ok = load(value, bytes, inner);
                Fill(report, Copy(inner));
                return ok ? (object)value : null;
            },
            Measure = v => measure((T)v),
            Save = (v, buffer) => save((T)v, buffer),
        };
    }

    static Codec V1()
    {
        return new Codec
        {
            Unit = "tblv1",
            Root = "Cfg",
            Load = (bytes, report) =>
            {
                Tblv1.Cfg value = new Tblv1.Cfg();
                Tblv1.TableReport inner = new Tblv1.TableReport();
                bool ok = Tblv1.Schema.CfgLoad(value, bytes, inner);
                Fill(report, Copy(inner));
                return ok ? (object)value : null;
            },
            Measure = v => Tblv1.Schema.CfgMeasure((Tblv1.Cfg)v),
            Save = (v, buffer) => Tblv1.Schema.CfgSave((Tblv1.Cfg)v, buffer),
        };
    }

    static Codec V2()
    {
        return new Codec
        {
            Unit = "tblv2",
            Root = "Cfg",
            Load = (bytes, report) =>
            {
                Tblv2.Cfg value = new Tblv2.Cfg();
                Tblv2.TableReport inner = new Tblv2.TableReport();
                bool ok = Tblv2.Schema.CfgLoad(value, bytes, inner);
                Fill(report, Copy(inner));
                return ok ? (object)value : null;
            },
            Measure = v => Tblv2.Schema.CfgMeasure((Tblv2.Cfg)v),
            Save = (v, buffer) => Tblv2.Schema.CfgSave((Tblv2.Cfg)v, buffer),
        };
    }

    static Codec P1()
    {
        return new Codec
        {
            Unit = "tblp1",
            Root = "Chain",
            Load = (bytes, report) =>
            {
                Tblp1.Chain value = new Tblp1.Chain();
                Tblp1.TableReport inner = new Tblp1.TableReport();
                bool ok = Tblp1.Schema.ChainLoad(value, bytes, inner);
                Fill(report, Copy(inner));
                return ok ? (object)value : null;
            },
            Measure = v => Tblp1.Schema.ChainMeasure((Tblp1.Chain)v),
            Save = (v, buffer) => Tblp1.Schema.ChainSave((Tblp1.Chain)v, buffer),
        };
    }

    static Codec P3()
    {
        return new Codec
        {
            Unit = "tblp3",
            Root = "Chain",
            Load = (bytes, report) =>
            {
                Tblp3.Chain value = new Tblp3.Chain();
                Tblp3.TableReport inner = new Tblp3.TableReport();
                bool ok = Tblp3.Schema.ChainLoad(value, bytes, inner);
                Fill(report, Copy(inner));
                return ok ? (object)value : null;
            },
            Measure = v => Tblp3.Schema.ChainMeasure((Tblp3.Chain)v),
            Save = (v, buffer) => Tblp3.Schema.ChainSave((Tblp3.Chain)v, buffer),
        };
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
                Console.Error.WriteLine("driver: no codec for " + f[2] + "." + f[3]);
                return 1;
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

    static int SurfaceReport(string outDir)
    {
        foreach (string[] f in Kind("report"))
        {
            Codec codec = Find(f[2], f[3]);
            if (codec == null)
            {
                Console.Error.WriteLine("driver: no codec for " + f[2] + "." + f[3]);
                return 1;
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
            // no text form in this backend (SPEC-TABLES.md §16.1's status), so
            // json-read and json-write are absent rather than failing
            Console.Out.Write("wire\nreport\nblock\nforgery\n");
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
            case "block": return SurfaceBlock(outDir);
            case "forgery": return SurfaceForgery(outDir);
            default: return 2;
        }
    }
}
