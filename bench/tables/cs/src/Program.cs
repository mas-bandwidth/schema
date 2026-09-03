// the tables bench — the C# runner.
//
// A port of bench/tables/cpp/table_main.cpp (the reference implementation)
// against the generated C# table codec: same corpus, same golden gate, same
// per-variant round-trip gate before any clock, same 1 warmup + 7 measured
// runs with median/min/max/spread, same CSV v2 rows with lang=cs.
//
// The C# table codec names NO runtime — `make tables-cs-standalone` gates
// that — so this leg compiles the generated sources beside itself and links
// nothing. That is the one contract difference from bench/cs/src/Program.cs,
// which compiles against the serialize.cs assembly.
//
// Language-specific discipline, the same choices the type leg made:
//   - escape barriers: a static sink accumulates observed byte counts and
//     GC.KeepAlive holds the decoded object, so the JIT cannot delete the work
//   - the read path loads into ONE reused instance, reset first — the
//     tolerant wire elides a field at its default, so resetting is part of a
//     correct read into reused storage and stays inside the clock
//   - the warmup run per path doubles as the JIT warmup
//
// THIS FILE IS SHAPE-BLIND: it names the generated type at its call sites and
// nothing else — no field, no pinned value, no wire size.

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;

static class Program
{
    const int MaxNumRuns = 7;           // median of 7 (N >= 5), after 1 warmup run
    static int gNumRuns = MaxNumRuns;   // --round K drops this to 1 (§2.4)
    const int NumVariants = 64;         // read-path variant buffers

    // ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
    // family `table` (§1.9) per row. linkage asm — the generated codec is
    // compiled into this one assembly and there is no runtime assembly to
    // cross, which is the recorded packaging fact for this leg. checks
    // contract — the reader's wire-contract validation is unconditional while
    // caller-error asserts are dormant (§3.4). opt default (the JIT has no
    // operator-visible optimization levels). inline unknown: a JIT leg has no
    // AOT artifact the §4 verdict pass could walk.
    const string CsvSuffix = "asm,contract,default,unknown";

    static readonly List<(string Row, string Family)> gCsvRows = new List<(string, string)>();
    static readonly SortedDictionary<string, byte[]> gGoldensLoaded =
        new SortedDictionary<string, byte[]>(StringComparer.Ordinal);

    static ulong Fnv1a64(ulong h, ReadOnlySpan<byte> data)
    {
        foreach (byte b in data)
        {
            h ^= b;
            h *= 0x100000001b3ul;
        }
        return h;
    }

    static string CorpusId()
    {
        ulong h = 0xcbf29ce484222325ul;
        Span<byte> zero = stackalloc byte[1];
        foreach (var golden in gGoldensLoaded) // sorted basename order
        {
            foreach (char c in golden.Key)
            {
                h ^= (byte)c; // golden basenames are ASCII
                h *= 0x100000001b3ul;
            }
            h = Fnv1a64(h, zero);
            h = Fnv1a64(h, golden.Value);
        }
        return h.ToString("x16");
    }

    static void FlushCsv()
    {
        if (!gCsv)
        {
            return;
        }
        if (failed)
        {
            // §1.5: a failing run emits NO rows.
            Console.Error.WriteLine("refusing to emit CSV rows from a failing run");
            return;
        }
        string id = CorpusId();
        foreach ((string row, string family) in gCsvRows)
        {
            Console.WriteLine(row + "," + id + "," + family + "," + CsvSuffix);
        }
    }

    // The tolerant wire spends bytes on ids, kinds and lengths, so a table
    // record is several times its equivalent type's. The record size comes
    // from the corpus at run time; this is only the ceiling the runner
    // refuses past.
    const int BufferSize = 65536;
    const int VariantStride = BufferSize + 64;  // §2.7 stride policy, as in every leg

    static readonly byte[] gBuffer = new byte[BufferSize];
    static readonly byte[] gTwin = new byte[BufferSize];
    static readonly byte[][] gVariants = new byte[NumVariants][];

    static ulong gSink; // defeats dead code elimination of computed values
    static bool gCsv;
    static string gWireDir = Path.Combine("testdata", "wire");
    static string gVariantDir = Path.Combine("bench", "corpus", "variants");
    static bool failed;

    static void Fail(string name, string what)
    {
        Console.Error.WriteLine("FAILED: " + name + ": " + what);
        failed = true;
    }

    struct RunStats
    {
        public double Median, Min, Max, SpreadPct;
    }

    static RunStats Stats(double[] rates)
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

    static void Report(string bench, string path, long iters, long bytesPerOp, RunStats s)
    {
        double mbps = s.Median * bytesPerOp / (1024.0 * 1024.0);
        Console.Error.WriteLine(string.Format(CultureInfo.InvariantCulture,
            "{0,-18} {1,-11} {2,10:F3} M msg/s {3,10:F1} MB/s   (min {4:F3}, max {5:F3}, spread {6:F1}%)",
            bench, path, s.Median / 1e6, mbps, s.Min / 1e6, s.Max / 1e6, s.SpreadPct));
        if (gCsv)
        {
            gCsvRows.Add((string.Format(CultureInfo.InvariantCulture,
                "cs,{0},{1},{2},{3},{4},{5:F0},{6:F0},{7:F0},{8:F2},{9:F2}",
                bench, path, iters, bytesPerOp, gNumRuns, s.Median, s.Min, s.Max, mbps, s.SpreadPct), "table"));
        }
    }

    static bool CheckGolden(string name, byte[] data, long bytes)
    {
        string path = Path.Combine(gWireDir, name + ".bin");
        if (!File.Exists(path))
        {
            Console.Error.WriteLine("missing wire golden " + path + " — run from the schema repo root (or pass --wire-dir)");
            return false;
        }
        byte[] expected = File.ReadAllBytes(path);
        gGoldensLoaded[name + ".bin"] = expected;
        if (expected.Length != bytes)
        {
            Console.Error.WriteLine("WIRE GOLDEN MISMATCH: " + path + " (" + expected.Length + " golden vs " + bytes + " actual bytes)");
            return false;
        }
        for (int i = 0; i < bytes; i++)
        {
            if (expected[i] != data[i])
            {
                Console.Error.WriteLine("WIRE GOLDEN MISMATCH: " + path + " differs at byte " + i + " — refusing to bench code that does not match the corpus");
                return false;
            }
        }
        return true;
    }

    // Records are fixed-width by construction — test/bench/table_main.cpp
    // refuses to emit a corpus whose records differ — so the record size IS
    // file size / NumVariants.
    static long LoadVariants(string name)
    {
        string path = Path.Combine(gVariantDir, name + ".variants.bin");
        if (!File.Exists(path))
        {
            Console.Error.WriteLine("missing variant data " + path + " — run `make bench-table-corpus`, and run from the schema repo root (or pass --variant-dir)");
            return -1;
        }
        byte[] packed = File.ReadAllBytes(path);
        if (packed.Length == 0 || packed.Length % NumVariants != 0)
        {
            Console.Error.WriteLine("variant data " + path + " is " + packed.Length + " bytes, not a multiple of " + NumVariants + " records");
            return -1;
        }
        int record = packed.Length / NumVariants;
        if (record > BufferSize)
        {
            Console.Error.WriteLine("variant data " + path + " has " + record + "-byte records, over the " + BufferSize + "-byte buffer");
            return -1;
        }
        for (int k = 0; k < NumVariants; k++)
        {
            gVariants[k] = new byte[VariantStride];
            Array.Copy(packed, k * record, gVariants[k], 0, record);
        }
        gGoldensLoaded[Path.GetFileName(path)] = packed;
        return record;
    }

    // ------------------------------------------------------------------
    // the data-driven table driver
    // ------------------------------------------------------------------

    delegate void ResetOf<T>(T value);
    delegate long SaveOf<T>(T value, Span<byte> buffer);
    delegate bool LoadOf<T>(T value, ReadOnlySpan<byte> bytes);

    static void BenchTable<T>(string name, string golden, long baseIters,
                              Func<T> make, ResetOf<T> reset, SaveOf<T> save, LoadOf<T> load)
        where T : class
    {
        long iters = baseIters;

        long bytesPerOp = LoadVariants(name);
        if (bytesPerOp < 0)
        {
            failed = true;
            return;
        }

        // gate 1 (§1.5): variant 0 IS the pinned instance.
        if (!CheckGolden(golden, gVariants[0], bytesPerOp))
        {
            failed = true;
            return;
        }

        // gate 2: every variant loads, re-saves, and comes back byte-identical
        // at the same length — before any clock starts.
        T[] instances = new T[NumVariants];
        for (int k = 0; k < NumVariants; k++)
        {
            instances[k] = make();
            reset(instances[k]);
            if (!load(instances[k], new ReadOnlySpan<byte>(gVariants[k], 0, (int)bytesPerOp)))
            {
                Fail(name, "load of a variant failed");
                return;
            }
            long wrote = save(instances[k], new Span<byte>(gTwin));
            if (wrote != bytesPerOp)
            {
                Fail(name, "variant round-trip length differs — refusing to bench a codec that does not reproduce the corpus");
                return;
            }
            for (int i = 0; i < bytesPerOp; i++)
            {
                if (gTwin[i] != gVariants[k][i])
                {
                    Fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
                    return;
                }
            }
        }

        double[] writeRates = new double[gNumRuns];
        double[] roundtripRates = new double[gNumRuns];

        // WRITE: save the 64 pre-loaded instances round-robin (§2.7 variation).
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                long wrote = save(instances[(int)(i & (NumVariants - 1))], new Span<byte>(gBuffer));
                if (wrote != bytesPerOp)
                {
                    Fail(name, "save failed in loop");
                    return;
                }
                gSink = gSink + (ulong)wrote;
            }
            sw.Stop();
            if (run >= 0)
            {
                writeRates[run] = iters / sw.Elapsed.TotalSeconds;
            }
        }

        // ROUND-TRIP: reset, load a variant buffer, re-save what came out. The
        // load's output IS the save's input, so every loaded field is observed
        // by construction (§2.7's read-side sink problem dissolved).
        T outValue = make();
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                reset(outValue);
                if (!load(outValue, new ReadOnlySpan<byte>(gVariants[(int)(i & (NumVariants - 1))], 0, (int)bytesPerOp)))
                {
                    Fail(name, "load failed in loop");
                    return;
                }
                long wrote = save(outValue, new Span<byte>(gBuffer));
                if (wrote != bytesPerOp)
                {
                    Fail(name, "re-save failed in loop");
                    return;
                }
                gSink = gSink + (ulong)wrote;
            }
            sw.Stop();
            if (run >= 0)
            {
                roundtripRates[run] = iters / sw.Elapsed.TotalSeconds;
            }
        }
        GC.KeepAlive(outValue);

        RunStats w = Stats(writeRates);
        RunStats rt = Stats(roundtripRates);
        Report(name, "write", iters, bytesPerOp, w);
        Report(name, "round_trip", iters, bytesPerOp, rt);

        // READ is DERIVED, never measured (§2.9): stderr only, never a row.
        double readTime = 1.0 / rt.Median - 1.0 / w.Median;
        if (readTime > 0)
        {
            Console.Error.WriteLine(string.Format(CultureInfo.InvariantCulture,
                "{0,-18} {1,-11} {2,10:F3} M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)",
                name, "read", 1e-6 / readTime));
        }
    }

    static int Main(string[] args)
    {
        for (int i = 0; i < args.Length; i++)
        {
            if (args[i] == "--csv")
            {
                gCsv = true;
            }
            else if (args[i] == "--wire-dir" && i + 1 < args.Length)
            {
                gWireDir = args[++i];
            }
            else if (args[i] == "--variant-dir" && i + 1 < args.Length)
            {
                gVariantDir = args[++i];
            }
            else if (args[i] == "--round" && i + 1 < args.Length)
            {
                if (!int.TryParse(args[++i], NumberStyles.Integer, CultureInfo.InvariantCulture, out int k) || k < 0)
                {
                    Console.Error.WriteLine("--round takes a non-negative integer, got '" + args[i] + "'");
                    return 1;
                }
                gNumRuns = 1;
            }
            else
            {
                Console.Error.WriteLine("usage: schematablesbench [--csv] [--round K] [--wire-dir <dir>] [--variant-dir <dir>]");
                return 1;
            }
        }

        Console.Error.WriteLine("schema tables bench (cs)");

        if (gCsv)
        {
            Console.WriteLine("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline");
        }

        // The one measured shape, named once — the generated type at the call
        // site and nothing else about it (bench/SHAPE-GATE.allow).
        Benchtable.TableReport report = new Benchtable.TableReport();
        BenchTable<Benchtable.TableMixed>(
            "bench_table", "bench_table", 400000L,
            () => new Benchtable.TableMixed(),
            v => Benchtable.Schema.TableReset(v),
            (v, b) => Benchtable.Schema.TableMixedSave(v, b),
            (v, b) => Benchtable.Schema.TableMixedLoad(v, b, report) && !report.Malformed);

        FlushCsv();

        if (failed)
        {
            Console.Error.WriteLine("TABLES BENCH FAILED (corpus_id " + CorpusId() + ")");
            return 1;
        }

        Console.Error.WriteLine("OK (corpus_id " + CorpusId() + ")");
        GC.KeepAlive(gSink);
        return 0;
    }
}
