// the tables bench — the Unity C# runner (docs/UNITY.md, bench/tables/README.md).
//
// Drives the generated Unity C# 9 / netstandard2.1 codec assembly (unit/benchunit.csproj)
// over the 64-record Form-1 tolerant wire corpus: golden gate, per-variant
// round-trip gate, reused-target rotation gate, then 1 warmup + 7 measured runs.
//
// THIS FILE IS SHAPE-BLIND: it names the generated root type at its call sites
// and nothing else — no field, no pinned value, no wire size.

using System;
using System.Buffers.Binary;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using BenchValue = Benchtable.TableMixed;
using BenchSchema = Benchtable.Schema;
using BenchReport = Benchtable.Schema.TableReport;

internal static class Program
{
    private const int MaxNumRuns = 7;
    private const int NumVariants = 64;
    private const int BufferSize = 65536;
    private const int VariantStride = BufferSize + 64;

    private static bool gIndexed;
    private static readonly int[] gLengths = new int[NumVariants];
    private static long gIterations;
    private static int gNumRuns = MaxNumRuns;
    private static readonly byte[] gBuffer = new byte[BufferSize];
    private static readonly byte[] gTwin = new byte[BufferSize];
    private static readonly byte[][] gVariants = new byte[NumVariants][];

    private static ulong gSink;
    private static bool gCsv;
    private static bool gGate;
    private static string gWireDir = Path.Combine("testdata", "wire");
    private static string gVariantDir = Path.Combine("bench", "corpus", "variants");
    private static bool gFailed;

    private static readonly List<string> gCsvRows = new List<string>();
    private static readonly SortedDictionary<string, byte[]> gGoldensLoaded =
        new SortedDictionary<string, byte[]>(StringComparer.Ordinal);

    private static ulong Fnv1a64(ulong h, ReadOnlySpan<byte> data)
    {
        foreach (byte b in data)
        {
            h ^= b;
            h *= 0x100000001b3UL;
        }
        return h;
    }

    private static string CorpusId()
    {
        ulong h = 0xcbf29ce484222325UL;
        Span<byte> zero = stackalloc byte[1];
        foreach (KeyValuePair<string, byte[]> golden in gGoldensLoaded)
        {
            foreach (char c in golden.Key)
            {
                h ^= (byte)c;
                h *= 0x100000001b3UL;
            }
            h = Fnv1a64(h, zero);
            h = Fnv1a64(h, golden.Value);
        }
        return h.ToString("x16");
    }

    private static void Fail(string name, string what)
    {
        Console.Error.WriteLine("FAILED: " + name + ": " + what);
        gFailed = true;
    }

    private struct RunStats
    {
        public double Median, Min, Max, SpreadPct;
    }

    private static RunStats Stats(double[] rates)
    {
        double[] sorted = (double[])rates.Clone();
        Array.Sort(sorted);
        return new RunStats
        {
            Median = sorted[sorted.Length / 2],
            Min = sorted[0],
            Max = sorted[sorted.Length - 1],
            SpreadPct = (sorted[sorted.Length - 1] - sorted[0]) / sorted[sorted.Length / 2] * 100.0,
        };
    }

    private static void Report(string bench, string path, long iters, double bytesPerOp, RunStats s)
    {
        double mbps = s.Median * bytesPerOp / (1024.0 * 1024.0);
        Console.Error.WriteLine(string.Format(CultureInfo.InvariantCulture,
            "{0,-18} {1,-11} {2,10:F3} M msg/s {3,10:F1} MB/s   (min {4:F3}, max {5:F3}, spread {6:F1}%)",
            bench, path, s.Median / 1e6, mbps, s.Min / 1e6, s.Max / 1e6, s.SpreadPct));
        if (gCsv)
        {
            gCsvRows.Add(string.Format(CultureInfo.InvariantCulture,
                "unity,{0},{1},{2},{3},{4},{5:F0},{6:F0},{7:F0},{8:F2},{9:F2}",
                bench, path, iters, bytesPerOp, gNumRuns, s.Median, s.Min, s.Max, mbps, s.SpreadPct));
        }
    }

    private static bool CheckGolden(string name, byte[] data, long bytes)
    {
        string path = Path.Combine(gWireDir, name + ".bin");
        if (!File.Exists(path))
        {
            Console.Error.WriteLine("missing wire golden " + path);
            return false;
        }
        byte[] expected = File.ReadAllBytes(path);
        gGoldensLoaded[name + ".bin"] = expected;
        if (expected.Length != bytes)
        {
            Console.Error.WriteLine("WIRE GOLDEN MISMATCH: " + path);
            return false;
        }
        for (int i = 0; i < bytes; i++)
        {
            if (expected[i] != data[i])
            {
                Console.Error.WriteLine("WIRE GOLDEN MISMATCH: " + path + " differs at byte " + i);
                return false;
            }
        }
        return true;
    }

    private static double LoadVariants(string name)
    {
        string path = Path.Combine(gVariantDir, name + ".variants.bin");
        if (!File.Exists(path)) return -1;
        byte[] packed = File.ReadAllBytes(path);
        if (gIndexed)
        {
            string index = Path.Combine(gVariantDir, name + ".lengths");
            if (!File.Exists(index)) return -1;
            byte[] lengths = File.ReadAllBytes(index);
            if (lengths.Length != NumVariants * 4) return -1;
            gGoldensLoaded[name + ".lengths"] = lengths;
            for (int k = 0; k < NumVariants; k++)
            {
                uint n = BinaryPrimitives.ReadUInt32LittleEndian(lengths.AsSpan(k * 4, 4));
                if (n == 0 || n > BufferSize) return -1;
                gLengths[k] = (int)n;
            }
        }
        else
        {
            if (packed.Length == 0 || packed.Length % NumVariants != 0) return -1;
            Array.Fill(gLengths, packed.Length / NumVariants);
        }
        int offset = 0;
        for (int k = 0; k < NumVariants; k++)
        {
            int n = gLengths[k];
            if (n <= 0 || n > BufferSize || n > packed.Length - offset) return -1;
            gVariants[k] = new byte[VariantStride];
            Array.Copy(packed, offset, gVariants[k], 0, n);
            offset += n;
        }
        if (offset != packed.Length) return -1;
        gGoldensLoaded[Path.GetFileName(path)] = packed;
        return (double)packed.Length / NumVariants;
    }

    private delegate void ResetOf<T>(ref T value);
    private delegate long SaveOf<T>(ref T value, Span<byte> buffer);
    private delegate bool LoadOf<T>(ref T value, ReadOnlySpan<byte> bytes);

    private static void BenchTable<T>(string name, string golden, long baseIters,
        Func<T> make, ResetOf<T> reset, SaveOf<T> save, LoadOf<T> load)
    {
        long iters = gIterations > 0 ? gIterations : baseIters;
        double bytesPerOp = LoadVariants(name);
        if (bytesPerOp < 0)
        {
            gFailed = true;
            return;
        }
        if (!CheckGolden(golden, gVariants[0], gLengths[0]))
        {
            gFailed = true;
            return;
        }
        T[] instances = new T[NumVariants];
        for (int k = 0; k < NumVariants; k++)
        {
            instances[k] = make();
            reset(ref instances[k]);
            if (!load(ref instances[k], new ReadOnlySpan<byte>(gVariants[k], 0, gLengths[k])))
            {
                Fail(name, "load of a variant failed");
                return;
            }
            long wrote = save(ref instances[k], new Span<byte>(gTwin));
            if (wrote != gLengths[k])
            {
                Fail(name, "variant round-trip length differs");
                return;
            }
            for (int i = 0; i < gLengths[k]; i++)
            {
                if (gTwin[i] != gVariants[k][i])
                {
                    Fail(name, "variant round-trip bytes differ");
                    return;
                }
            }
        }

        T outValue = make();
        for (int k = 0; k < 2 * NumVariants; k++)
        {
            int index = k & (NumVariants - 1);
            if (!load(ref outValue, new ReadOnlySpan<byte>(gVariants[index], 0, gLengths[index])))
            {
                Fail(name, "load into reused target failed");
                return;
            }
            long wrote = save(ref outValue, new Span<byte>(gTwin));
            if (wrote != gLengths[index])
            {
                Fail(name, "reused target round-trip length differs");
                return;
            }
            for (int i = 0; i < gLengths[index]; i++)
            {
                if (gTwin[i] != gVariants[index][i])
                {
                    Fail(name, "reused target round-trip bytes differ");
                    return;
                }
            }
        }

        if (gGate) return;

        double[] writeRates = new double[gNumRuns];
        double[] roundtripRates = new double[gNumRuns];

        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                int idx = (int)(i & (NumVariants - 1));
                long wrote = save(ref instances[idx], new Span<byte>(gBuffer));
                if (wrote != gLengths[idx])
                {
                    Fail(name, "save failed in loop");
                    return;
                }
                gSink += (ulong)wrote;
            }
            double elapsed = sw.Elapsed.TotalSeconds;
            if (run >= 0) writeRates[run] = iters / elapsed;
        }

        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                int idx = (int)(i & (NumVariants - 1));
                if (!load(ref outValue, new ReadOnlySpan<byte>(gVariants[idx], 0, gLengths[idx])))
                {
                    Fail(name, "load failed in loop");
                    return;
                }
                long wrote = save(ref outValue, new Span<byte>(gBuffer));
                if (wrote != gLengths[idx])
                {
                    Fail(name, "re-save failed in loop");
                    return;
                }
                GC.KeepAlive(gBuffer);
                gSink += (ulong)wrote;
            }
            double elapsed = sw.Elapsed.TotalSeconds;
            if (run >= 0) roundtripRates[run] = iters / elapsed;
        }

        RunStats w = Stats(writeRates);
        RunStats rt = Stats(roundtripRates);
        Report(name, "write", iters, bytesPerOp, w);
        Report(name, "round_trip", iters, bytesPerOp, rt);
    }

    private static int Main(string[] args)
    {
        for (int i = 0; i < args.Length; i++)
        {
            if (args[i] == "--indexed") gIndexed = true;
            else if (args[i] == "--gate") gGate = true;
            else if (args[i] == "--csv") gCsv = true;
            else if (args[i] == "--wire-dir" && i + 1 < args.Length) gWireDir = args[++i];
            else if (args[i] == "--variant-dir" && i + 1 < args.Length) gVariantDir = args[++i];
            else if (args[i] == "--iterations" && i + 1 < args.Length)
            {
                if (!long.TryParse(args[++i], NumberStyles.Integer, CultureInfo.InvariantCulture, out long n) || n <= 0 || n % NumVariants != 0)
                    return 1;
                gIterations = n;
            }
            else if (args[i] == "--round" && i + 1 < args.Length)
            {
                if (!int.TryParse(args[++i], NumberStyles.Integer, CultureInfo.InvariantCulture, out int k) || k < 0)
                    return 1;
                gNumRuns = 1;
            }
            else return 1;
        }

        Console.Error.WriteLine("schema tables bench (unity)");
        if (gCsv)
        {
            Console.WriteLine("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline");
        }

        BenchTable<BenchValue>(
            "bench_table", "bench_table", 200000L,
            () => new BenchValue(),
            (ref BenchValue v) => BenchSchema.InitTableMixed(ref v),
            (ref BenchValue v, Span<byte> b) => BenchSchema.TableMixedSave(ref v, b),
            (ref BenchValue v, ReadOnlySpan<byte> b) =>
            {
                BenchReport rep = default;
                return BenchSchema.TableMixedLoad(ref v, b, ref rep) && rep.Unknown == 0 && rep.Widened == 0 && !rep.Damaged && !rep.Refused;
            });

        if (gCsv && !gFailed)
        {
            string id = CorpusId();
            foreach (string row in gCsvRows)
            {
                Console.WriteLine(row + "," + id + ",table,asm,contract,default,unknown");
            }
        }
        if (gFailed)
        {
            Console.Error.WriteLine("TABLES BENCH FAILED (corpus_id " + CorpusId() + ")");
            return 1;
        }
        Console.Error.WriteLine("OK (corpus_id " + CorpusId() + ")");
        GC.KeepAlive(gSink);
        return 0;
    }
}
