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
//   - the read path loads into ONE reused instance; public Load restores
//     declared defaults inside the clock, with no separate runner reset
//   - the warmup run per path doubles as the JIT warmup
//
// THIS FILE IS SHAPE-BLIND: it names the generated type at its call sites and
// nothing else — no field, no pinned value, no wire size.

#if BENCH_MATCHED
using BenchValue = Bench.BenchMixed;
using BenchSchema = Bench.Schema;
using BenchReport = Bench.TableReport;
#else
using BenchValue = Benchtable.TableMixed;
using BenchSchema = Benchtable.Schema;
using BenchReport = Benchtable.TableReport;
#endif
using System;
using System.Buffers.Binary;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;

static class Program
{
    const int MaxNumRuns = 7;           // median of 7 (N >= 5), after 1 warmup run
    static bool gIndexed;
    static readonly int[] gLengths = new int[NumVariants];
    static long gIterations = 0; // explicit diagnostic count; defaults stay standard
    static int gNumRuns = MaxNumRuns;   // --round K drops this to 1 (§2.4)
    static int gRound = 0;
    const int NumVariants = 64;         // read-path variant buffers

    // THE READ ARM (BENCH-STANDARD.md §1.10): one process times the fixed
    // form's read, the plan lane's read (on the BENCH_PLAN build), and the
    // packet wire's read, rotating which runs first by --round K.
    static bool gReadArm;
    static long gPacketIterations = 0;
    static string gPacketWireDir = Path.Combine("testdata", "wire");
    static string gPacketVariantDir = Path.Combine("bench", "corpus", "variants");
#if BENCH_PLAN
    static string gLineageDir = Path.Combine("bench", "tables", "cs", "lineage");
#endif

    // ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
    // family `table` (§1.9) per row. linkage asm — the generated codec is
    // compiled into this one assembly and there is no runtime assembly to
    // cross, which is the recorded packaging fact for this leg. checks
    // contract — the reader's wire-contract validation is unconditional while
    // caller-error asserts are dormant (§3.4). opt default (the JIT has no
    // operator-visible optimization levels). inline unknown: a JIT leg has no
    // AOT artifact the §4 verdict pass could walk.
    static readonly List<(string Row, string Family, string Checks)> gCsvRows =
        new List<(string, string, string)>();
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
        foreach ((string row, string family, string checks) in gCsvRows)
        {
            Console.WriteLine(row + "," + id + "," + family + ",asm," + checks + ",default,unknown");
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
    static bool gGate;
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

    static void ReportWith(string bench, string path, string family, string checks, long iters, double bytesPerOp, RunStats s)
    {
        double mbps = s.Median * bytesPerOp / (1024.0 * 1024.0);
        Console.Error.WriteLine(string.Format(CultureInfo.InvariantCulture,
            "{0,-18} {1,-11} {2,10:F3} M msg/s {3,10:F1} MB/s   (min {4:F3}, max {5:F3}, spread {6:F1}%)",
            bench, path, s.Median / 1e6, mbps, s.Min / 1e6, s.Max / 1e6, s.SpreadPct));
        if (gCsv)
        {
            gCsvRows.Add((string.Format(CultureInfo.InvariantCulture,
                "cs,{0},{1},{2},{3},{4},{5:F0},{6:F0},{7:F0},{8:F2},{9:F2}",
                bench, path, iters, bytesPerOp, gNumRuns, s.Median, s.Min, s.Max, mbps, s.SpreadPct), family, checks));
        }
    }

    static void Report(string bench, string path, long iters, double bytesPerOp, RunStats s)
    {
        ReportWith(bench, path, "table", "contract", iters, bytesPerOp, s);
    }

    static bool CheckGoldenFrom(string dir, string name, byte[] data, long bytes)
    {
        string path = Path.Combine(dir, name + ".bin");
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

    static bool CheckGolden(string name, byte[] data, long bytes)
    {
        return CheckGoldenFrom(gWireDir, name, data, bytes);
    }

    // Load 64 concatenated records. --indexed requires a 64-entry uint32
    // length index; otherwise retain the historical fixed-stride format.
    // Return the exact mean encoded bytes; padding is never counted.
    static double LoadVariants(string name)
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
        long iters = gIterations > 0 ? gIterations : baseIters;

        double bytesPerOp = LoadVariants(name);
        if (bytesPerOp < 0)
        {
            failed = true;
            return;
        }

        // gate 1 (§1.5): variant 0 IS the pinned instance.
        if (!CheckGolden(golden, gVariants[0], gLengths[0]))
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
            if (!load(instances[k], new ReadOnlySpan<byte>(gVariants[k], 0, gLengths[k])))
            {
                Fail(name, "load of a variant failed");
                return;
            }
            long wrote = save(instances[k], new Span<byte>(gTwin));
            if (wrote != gLengths[k])
            {
                Fail(name, "variant round-trip length differs — refusing to bench a codec that does not reproduce the corpus");
                return;
            }
            for (int i = 0; i < gLengths[k]; i++)
            {
                if (gTwin[i] != gVariants[k][i])
                {
                    Fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
                    return;
                }
            }
        }

        // gate 3: reused storage must also round-trip, including the last-to-first
        // transition. Public Load owns default restoration; do not reset between loads.
        T outValue = make();
        for (int k = 0; k < 2 * NumVariants; k++)
        {
            int index = k & (NumVariants - 1);
            if (!load(outValue, new ReadOnlySpan<byte>(gVariants[index], 0, gLengths[index])))
            {
                Fail(name, "load into reused target failed");
                return;
            }
            long wrote = save(outValue, new Span<byte>(gTwin));
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

        // WRITE: save the 64 pre-loaded instances round-robin (§2.7 variation).
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                long wrote = save(instances[(int)(i & (NumVariants - 1))], new Span<byte>(gBuffer));
                if (wrote != gLengths[(int)(i & (NumVariants - 1))])
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

        // ROUND-TRIP: load a variant buffer, re-save what came out. The
        // load's output IS the save's input, so every loaded field is observed
        // by construction (§2.7's read-side sink problem dissolved).
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                if (!load(outValue, new ReadOnlySpan<byte>(gVariants[(int)(i & (NumVariants - 1))], 0, gLengths[(int)(i & (NumVariants - 1))])))
                {
                    Fail(name, "load failed in loop");
                    return;
                }
                long wrote = save(outValue, new Span<byte>(gBuffer));
                if (wrote != gLengths[(int)(i & (NumVariants - 1))])
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

#if BENCH_MATCHED
    // ------------------------------------------------------------------
    // the FIXED form, form byte 3 (docs/SPEC-TABLES.md §3.4)
    // ------------------------------------------------------------------
    //
    // The same 64 logical records, on the same table wire, in the form that spends
    // no byte on ids, kinds, lengths or terminators. A FILE is the unit here and
    // not a record: the vocabulary block rides once, the records follow it to the
    // end, and every one of them is the same size — so the write is one call for
    // all 64 and the read is one call back, and an OP is one RECORD of that call.
    //
    // The gates before the clock are the same three in a different spelling, plus
    // the one this form adds: THE BLOCK THIS BUILD EMITS IS THE CORPUS'S BLOCK,
    // byte for byte. That is the whole claim a port makes on this form — the record
    // layout, the ids, the kinds and the hash are all in those bytes — so it is
    // checked first and by itself.
    const int FixedCount = 64;
    const int FixedPlanCapacity = 4096;
    static readonly Bench.FixedTable[] gFixedValues = new Bench.FixedTable[FixedCount];
    static readonly Bench.FixedTable[] gFixedOut = new Bench.FixedTable[FixedCount];
    static readonly Bench.TableFixedEntry[] gFixedPlan = new Bench.TableFixedEntry[FixedPlanCapacity];
    static readonly BenchReport gFixedReport = new BenchReport();
    static readonly byte[][] gPacketVariants = new byte[NumVariants][];

    static long FixedLoadAll(Bench.FixedTable[] values, ReadOnlySpan<byte> bytes)
    {
        return BenchSchema.FixedTableFixedLoad(values, bytes, gFixedPlan, gFixedReport);
    }

    static bool LoadAndGateFixed(string name, out byte[] file)
    {
        file = null;
        string filePath = Path.Combine(gVariantDir, "bench_fixed.bin");
        if (!File.Exists(filePath))
        {
            Console.Error.WriteLine("missing fixed corpus " + filePath + " — run from the schema repo root (or pass --variant-dir)");
            failed = true;
            return false;
        }
        file = File.ReadAllBytes(filePath);

        string layoutPath = Path.Combine(gVariantDir, "bench_fixed.layout");
        if (!File.Exists(layoutPath))
        {
            Console.Error.WriteLine("missing fixed corpus " + layoutPath + " — run from the schema repo root (or pass --variant-dir)");
            failed = true;
            return false;
        }
        byte[] layout = File.ReadAllBytes(layoutPath);

        if (file.Length == 0 || layout.Length == 0)
        {
            Console.Error.WriteLine("empty fixed corpus in " + gVariantDir);
            failed = true;
            return false;
        }

        gGoldensLoaded["bench_fixed.bin"] = file;
        gGoldensLoaded["bench_fixed.layout"] = layout;
        byte[] twin = new byte[file.Length];

        // gate 1: THE LAYOUT IS THE CORPUS'S LAYOUT.
        if (layout.Length != BenchSchema.FixedTableFixedLayoutBytes ||
            !layout.AsSpan().SequenceEqual(BenchSchema.FixedTableFixedLayout))
        {
            Fail(name, "this build's layout block is not the corpus's, byte for byte");
            return false;
        }

        // gate 2: the whole file loads clean.
        long loaded = FixedLoadAll(gFixedValues, file);
        if (loaded != FixedCount)
        {
            Fail(name, "the fixed corpus did not load");
            return false;
        }

        // gate 3: writing them back reproduces the file, byte for byte.
        if (BenchSchema.FixedTableFixedSave(gFixedValues, twin) != file.Length ||
            !twin.AsSpan().SequenceEqual(file))
        {
            Fail(name, "round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
            return false;
        }

        // gate 4: reused storage round-trips too. The load owns the prefill, so
        // nothing is reset between the two passes.
        for (int k = 0; k < 2; k++)
        {
            if (FixedLoadAll(gFixedOut, file) != FixedCount ||
                BenchSchema.FixedTableFixedSave(gFixedOut, twin) != file.Length ||
                !twin.AsSpan().SequenceEqual(file))
            {
                Fail(name, "reused target round-trip bytes differ");
                return false;
            }
        }
        return true;
    }

    static void BenchFixed(string name, long baseIters)
    {
        long iters = gIterations > 0 ? gIterations : baseIters;
        if (!LoadAndGateFixed(name, out byte[] file))
        {
            return;
        }
        if (gGate) return;

        double bytesPerOp = (double)file.Length / FixedCount;
        byte[] twin = new byte[file.Length];
        double[] writeRates = new double[gNumRuns];
        double[] roundtripRates = new double[gNumRuns];

        // WRITE: one call lays down all 64 records; an op is one record.
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i += FixedCount)
            {
                long wrote = BenchSchema.FixedTableFixedSave(gFixedValues, twin);
                if (wrote != file.Length)
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

        // ROUND-TRIP: read the file back, then write what came out.
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i += FixedCount)
            {
                if (FixedLoadAll(gFixedOut, file) != FixedCount)
                {
                    Fail(name, "load failed in loop");
                    return;
                }
                long wrote = BenchSchema.FixedTableFixedSave(gFixedOut, twin);
                if (wrote != file.Length)
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

        GC.KeepAlive(gFixedValues);
        GC.KeepAlive(gFixedOut);
        GC.KeepAlive(twin);

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

    static bool GateMixedRead(out int packetSize)
    {
        packetSize = 0;
        string path = Path.Combine(gPacketVariantDir, "bench_mixed.variants.bin");
        if (!File.Exists(path))
        {
            Console.Error.WriteLine("missing packet variant data " + path + " — pass --packet-variant-dir");
            failed = true;
            return false;
        }
        byte[] packed = File.ReadAllBytes(path);
        if (packed.Length == 0 || packed.Length % NumVariants != 0)
        {
            Fail("bench_mixed_read", "packet variant data is not a multiple of 64 records");
            return false;
        }
        int record = packed.Length / NumVariants;
        if (record <= 0 || record > BufferSize)
        {
            Fail("bench_mixed_read", "packet variant record size out of range");
            return false;
        }
        for (int k = 0; k < NumVariants; k++)
        {
            gPacketVariants[k] = new byte[VariantStride];
            Array.Copy(packed, k * record, gPacketVariants[k], 0, record);
        }
        gGoldensLoaded["bench_mixed.variants.bin"] = packed;

        if (!CheckGoldenFrom(gPacketWireDir, "bench_mixed", gPacketVariants[0], record))
        {
            failed = true;
            return false;
        }

        BenchValue instance = new BenchValue();
        for (int k = 0; k < NumVariants; k++)
        {
            Serialize.ReadStream rs = new Serialize.ReadStream(gPacketVariants[k], record);
            if (!BenchSchema.ReadBenchMixed(rs, instance) || rs.BytesProcessed != record)
            {
                Fail("bench_mixed_read", "decode of a packet variant failed");
                return false;
            }
            Serialize.WriteStream ws = new Serialize.WriteStream(gTwin);
            if (!BenchSchema.WriteBenchMixed(ws, instance))
            {
                Fail("bench_mixed_read", "re-encode of a decoded packet variant failed");
                return false;
            }
            ws.Flush();
            if (ws.BytesProcessed != record ||
                !gTwin.AsSpan(0, record).SequenceEqual(gPacketVariants[k].AsSpan(0, record)))
            {
                Fail("bench_mixed_read", "packet variant round-trip bytes differ");
                return false;
            }
        }

        BenchValue reused = new BenchValue();
        for (int k = 0; k < 2 * NumVariants; k++)
        {
            int index = k & (NumVariants - 1);
            Serialize.ReadStream rs = new Serialize.ReadStream(gPacketVariants[index], record);
            if (!BenchSchema.ReadBenchMixed(rs, reused) || rs.BytesProcessed != record)
            {
                Fail("bench_mixed_read", "decode into reused packet target failed");
                return false;
            }
            Serialize.WriteStream ws = new Serialize.WriteStream(gTwin);
            if (!BenchSchema.WriteBenchMixed(ws, reused))
            {
                Fail("bench_mixed_read", "re-encode of reused packet target failed");
                return false;
            }
            ws.Flush();
            if (ws.BytesProcessed != record ||
                !gTwin.AsSpan(0, record).SequenceEqual(gPacketVariants[index].AsSpan(0, record)))
            {
                Fail("bench_mixed_read", "reused packet target round-trip bytes differ");
                return false;
            }
        }

        packetSize = record;
        return true;
    }

#if BENCH_PLAN
    static bool GatePlanRead(byte[] fixedGolden, out byte[] lineageFile)
    {
        lineageFile = null;
        string filePath = Path.Combine(gLineageDir, "bench_lineage.bin");
        if (!File.Exists(filePath))
        {
            Console.Error.WriteLine("missing lineage corpus " + filePath + " — pass --lineage-dir");
            failed = true;
            return false;
        }
        lineageFile = File.ReadAllBytes(filePath);

        string layoutPath = Path.Combine(gLineageDir, "bench_lineage.layout");
        if (!File.Exists(layoutPath))
        {
            Console.Error.WriteLine("missing lineage layout " + layoutPath + " — pass --lineage-dir");
            failed = true;
            return false;
        }
        byte[] lineageLayout = File.ReadAllBytes(layoutPath);

        if (lineageFile.Length == 0 || lineageLayout.Length == 0)
        {
            Console.Error.WriteLine("empty lineage corpus in " + gLineageDir);
            failed = true;
            return false;
        }
        gGoldensLoaded["bench_lineage.bin"] = lineageFile;
        gGoldensLoaded["bench_lineage.layout"] = lineageLayout;

        // gate 1: THE LINEAGE IS THE LOCK'S. This build knows exactly two layouts,
        // the older one first, and the lineage file carries that older layout's
        // hash and bytes — so the read below runs a plan, never the identity.
        if (lineageFile.Length < 20)
        {
            Fail("bench_plan_read", "bench_lineage.bin is shorter than 20 bytes");
            return false;
        }
        uint embedded = BinaryPrimitives.ReadUInt32LittleEndian(lineageFile.AsSpan(16, 4));
        ulong lineageHash = BinaryPrimitives.ReadUInt64LittleEndian(lineageFile.AsSpan(8, 8));
        if (BenchSchema.FixedTableFixedKnown.Length != 2 ||
            lineageHash != BenchSchema.FixedTableFixedKnown[0].Hash ||
            BenchSchema.FixedTableFixedKnown[0].Hash == BenchSchema.FixedTableFixedHash ||
            lineageLayout.Length != BenchSchema.FixedTableFixedKnown[0].Layout.Length ||
            !lineageLayout.AsSpan().SequenceEqual(BenchSchema.FixedTableFixedKnown[0].Layout) ||
            embedded != (uint)lineageLayout.Length ||
            20 + lineageLayout.Length > lineageFile.Length ||
            !lineageFile.AsSpan(20, lineageLayout.Length).SequenceEqual(lineageLayout))
        {
            Fail("bench_plan_read", "the lineage corpus is not the lock's older layout, or this build's lineage is not that lock's");
            return false;
        }

        // gate 2: the whole file reads, and nothing but a widening is reported.
        for (int k = 0; k < FixedCount; k++)
        {
            gFixedOut[k] = null;
        }
        BenchReport rep = new BenchReport();
        if (BenchSchema.FixedTableFixedLoad(gFixedOut, lineageFile, gFixedPlan, rep) != FixedCount ||
            rep.Malformed || rep.Refused ||
            rep.Unknown != 0 || rep.KindMismatch != 0 ||
            rep.Duplicate != 0 || rep.Clamped != 0)
        {
            Fail("bench_plan_read", "the lineage corpus did not read clean through its plan");
            return false;
        }
        Console.Error.WriteLine(string.Format(CultureInfo.InvariantCulture,
            "{0,-18} plan read of {1} records: widened {2}, clamped {3} (lineage 0x{4:x16} -> 0x{5:x16})",
            "bench_plan_read", FixedCount, rep.Widened, rep.Clamped,
            BenchSchema.FixedTableFixedKnown[0].Hash, BenchSchema.FixedTableFixedHash));

        // gate 3: a poisoned destination loads to the exact same bytes as a
        // clean one, and the answer is not the identity corpus's.
        if (FixedLoadAll(gFixedValues, lineageFile) != FixedCount)
        {
            Fail("bench_plan_read", "initial load into poison buffer failed");
            return false;
        }
        Span<byte> poisonBytes = stackalloc byte[512];
        poisonBytes.Fill(0xA5);
        BenchReport poisonRep = new BenchReport();
        for (int k = 0; k < FixedCount; k++)
        {
            foreach (var slot in BenchSchema.FixedTableFixedSlots)
            {
                slot.SetRaw?.Invoke(gFixedValues[k], 0xA5A5A5A5A5A5A5A5ul);
                slot.SetRawReport?.Invoke(gFixedValues[k], 0xA5A5A5A5A5A5A5A5ul, poisonRep);
                slot.SetDouble?.Invoke(gFixedValues[k], -12345.6789);
                slot.SetWide?.Invoke(gFixedValues[k], new UInt128(0xA5A5A5A5A5A5A5A5ul, 0xA5A5A5A5A5A5A5A5ul));
                slot.SetBytes?.Invoke(gFixedValues[k], poisonBytes, poisonBytes.Length);
                slot.SetChars?.Invoke(gFixedValues[k], poisonBytes, poisonBytes.Length);
            }
        }
        long wantBytes = BenchSchema.FixedTableFixedMeasure(FixedCount);
        byte[] a = new byte[wantBytes];
        byte[] b = new byte[wantBytes];
        if (FixedLoadAll(gFixedValues, lineageFile) != FixedCount ||
            BenchSchema.FixedTableFixedSave(gFixedOut, a) != wantBytes ||
            BenchSchema.FixedTableFixedSave(gFixedValues, b) != wantBytes ||
            !a.AsSpan().SequenceEqual(b))
        {
            Fail("bench_plan_read", "a poisoned destination did not load to the same bytes as a clean one");
            return false;
        }
        if (a.AsSpan().SequenceEqual(fixedGolden))
        {
            Fail("bench_plan_read", "the lineage corpus decoded to the identity corpus's bytes");
            return false;
        }
        return true;
    }

    static void BenchPlanRead(byte[] lineageFile, long iters)
    {
        double bytesPerOp = (double)lineageFile.Length / FixedCount;
        double[] readRates = new double[gNumRuns];
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i += FixedCount)
            {
                long loaded = FixedLoadAll(gFixedOut, lineageFile);
                if (loaded != FixedCount)
                {
                    Fail("bench_plan_read", "load failed in loop");
                    return;
                }
                gSink = gSink + (ulong)loaded;
            }
            sw.Stop();
            if (run >= 0)
            {
                readRates[run] = iters / sw.Elapsed.TotalSeconds;
            }
        }
        GC.KeepAlive(gFixedOut);
        ReportWith("bench_plan_read", "read", "table", "contract", iters, bytesPerOp, Stats(readRates));
    }
#endif

    static void BenchFixedRead(byte[] file, long iters)
    {
        double bytesPerOp = (double)file.Length / FixedCount;
        double[] readRates = new double[gNumRuns];
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i += FixedCount)
            {
                long loaded = FixedLoadAll(gFixedOut, file);
                if (loaded != FixedCount)
                {
                    Fail("bench_fixed_read", "load failed in loop");
                    return;
                }
                gSink = gSink + (ulong)loaded;
            }
            sw.Stop();
            if (run >= 0)
            {
                readRates[run] = iters / sw.Elapsed.TotalSeconds;
            }
        }
        GC.KeepAlive(gFixedOut);
        ReportWith("bench_fixed_read", "read", "table", "contract", iters, bytesPerOp, Stats(readRates));
    }

    static void BenchMixedRead(int packetSize, long iters)
    {
        BenchValue outValue = new BenchValue();
        Serialize.ReadStream rs = new Serialize.ReadStream(gPacketVariants[0], packetSize);
        double[] readRates = new double[gNumRuns];
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                rs.Reset(gPacketVariants[(int)(i & (NumVariants - 1))], packetSize);
                if (!BenchSchema.ReadBenchMixed(rs, outValue))
                {
                    Fail("bench_mixed_read", "read failed in loop");
                    return;
                }
                gSink = gSink + (ulong)rs.BytesProcessed;
            }
            sw.Stop();
            if (run >= 0)
            {
                readRates[run] = iters / sw.Elapsed.TotalSeconds;
            }
        }
        GC.KeepAlive(outValue);
        ReportWith("bench_mixed_read", "read", "gen", "removed", iters, packetSize, Stats(readRates));
    }

    static void ReadArm()
    {
        bool savedGate = gGate;
        gGate = true;
        BenchReport report = new BenchReport();
        BenchTable<BenchValue>(
            "bench_table", "bench_table", 400000L,
            () => new BenchValue(),
            v => BenchSchema.TableReset(v),
            (v, b) => BenchSchema.BenchMixedSave(v, b),
            (v, b) => BenchSchema.BenchMixedLoad(v, b, report) && !report.Malformed);
        gGate = savedGate;
        if (failed) return;

        if (!LoadAndGateFixed("bench_fixed_read", out byte[] fixedFile))
        {
            return;
        }
        if (!GateMixedRead(out int packetSize))
        {
            return;
        }
#if BENCH_PLAN
        if (!GatePlanRead(fixedFile, out byte[] lineageFile))
        {
            return;
        }
#endif
        if (failed || gGate) return;

        long fixedIters = gIterations > 0 ? gIterations : 4000000L;
        long packetIters = gPacketIterations > 0 ? gPacketIterations : fixedIters;
#if BENCH_PLAN
        for (int step = 0; step < 3; step++)
        {
            int slot = (gRound + step) % 3;
            if (slot == 0) BenchFixedRead(fixedFile, fixedIters);
            else if (slot == 1) BenchPlanRead(lineageFile, fixedIters);
            else BenchMixedRead(packetSize, packetIters);
            if (failed) return;
        }
#else
        if ((gRound & 1) != 0)
        {
            BenchMixedRead(packetSize, packetIters);
            if (!failed) BenchFixedRead(fixedFile, fixedIters);
        }
        else
        {
            BenchFixedRead(fixedFile, fixedIters);
            if (!failed) BenchMixedRead(packetSize, packetIters);
        }
#endif
    }
#endif

    static int Main(string[] args)
    {
        for (int i = 0; i < args.Length; i++)
        {
            if (args[i] == "--indexed") { gIndexed = true; }
            else if (args[i] == "--gate")
            {
                gGate = true;
            }
            else if (args[i] == "--csv")
            {
                gCsv = true;
            }
            else if (args[i] == "--read-arm")
            {
                gReadArm = true;
            }
            else if (args[i] == "--wire-dir" && i + 1 < args.Length)
            {
                gWireDir = args[++i];
            }
            else if (args[i] == "--variant-dir" && i + 1 < args.Length)
            {
                gVariantDir = args[++i];
            }
            else if (args[i] == "--packet-wire-dir" && i + 1 < args.Length)
            {
                gPacketWireDir = args[++i];
            }
            else if (args[i] == "--packet-variant-dir" && i + 1 < args.Length)
            {
                gPacketVariantDir = args[++i];
            }
#if BENCH_PLAN
            else if (args[i] == "--lineage-dir" && i + 1 < args.Length)
            {
                gLineageDir = args[++i];
            }
#endif
            else if (args[i] == "--iterations" && i + 1 < args.Length)
            {
                if (!long.TryParse(args[++i], NumberStyles.Integer, CultureInfo.InvariantCulture, out long n) || n <= 0 || n > 2147483584L || n % NumVariants != 0)
                {
                    Console.Error.WriteLine("--iterations requires a positive multiple of 64 up to 2147483584");
                    return 1;
                }
                gIterations = n;
            }
            else if (args[i] == "--packet-iterations" && i + 1 < args.Length)
            {
                if (!long.TryParse(args[++i], NumberStyles.Integer, CultureInfo.InvariantCulture, out long n) || n <= 0 || n > 2147483584L || n % NumVariants != 0)
                {
                    Console.Error.WriteLine("--packet-iterations requires a positive multiple of 64 up to 2147483584");
                    return 1;
                }
                gPacketIterations = n;
            }
            else if (args[i] == "--round" && i + 1 < args.Length)
            {
                if (!int.TryParse(args[++i], NumberStyles.Integer, CultureInfo.InvariantCulture, out int k) || k < 0)
                {
                    Console.Error.WriteLine("--round takes a non-negative integer, got '" + args[i] + "'");
                    return 1;
                }
                gRound = k;
                gNumRuns = 1;
            }
            else
            {
                Console.Error.WriteLine("usage: schematablesbench [--indexed] [--gate] [--csv] [--read-arm] [--round K] [--iterations N] [--packet-iterations N] [--wire-dir <dir>] [--variant-dir <dir>] [--packet-wire-dir <dir>] [--packet-variant-dir <dir>]");
                return 1;
            }
        }

        Console.Error.WriteLine("schema tables bench (cs)");

        if (gCsv)
        {
            Console.WriteLine("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline");
        }

        if (gReadArm)
        {
#if BENCH_MATCHED
            ReadArm();
            FlushCsv();
            if (failed)
            {
                Console.Error.WriteLine("TABLES BENCH FAILED (corpus_id " + CorpusId() + ")");
                return 1;
            }
            Console.Error.WriteLine("OK (corpus_id " + CorpusId() + ")");
            GC.KeepAlive(gSink);
            return 0;
#else
            Console.Error.WriteLine("--read-arm requires the paired BENCH_MATCHED build");
            return 1;
#endif
        }

        // The measured shape, named once — the generated type at the call site and
        // nothing else about it (bench/SHAPE-GATE.allow).
        //
        // IN THE MATCHED BUILD THE MEASURED WIRE IS THE FIXED FORM (§3.4), and the
        // tolerant form still runs every correctness gate it always ran, without a
        // clock: the corpus is one corpus, both forms carry the same 64 logical
        // records, and a run that stopped loading the tolerant half would report a
        // corpus id the paired driver could not match.
#if BENCH_MATCHED
        bool measured = gGate;
        gGate = true;
        BenchReport report = new BenchReport();
        BenchTable<BenchValue>(
            "bench_table", "bench_table", 400000L,
            () => new BenchValue(),
            v => BenchSchema.TableReset(v),
            (v, b) => BenchSchema.BenchMixedSave(v, b),
            (v, b) => BenchSchema.BenchMixedLoad(v, b, report) && !report.Malformed);
        gGate = measured;
        BenchFixed("bench_fixed", 400000L);
#else
        BenchReport report = new BenchReport();
        BenchTable<BenchValue>(
            "bench_table", "bench_table", 400000L,
            () => new BenchValue(),
            v => BenchSchema.TableReset(v),
            (v, b) => BenchSchema.TableMixedSave(v, b),
            (v, b) => BenchSchema.TableMixedLoad(v, b, report) && !report.Malformed);
#endif

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
