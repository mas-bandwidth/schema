// schema bench — the C# runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated C# and the serialize.cs runtime: same benchmark set, same
// variant corpus, same golden + per-variant round-trip self-checks (a
// mismatch REFUSES to bench), same warmup + 7 measured runs +
// median/min/max/spread, same CSV row format with lang=cs. See
// bench/README.md for the runner contract.
//
// Language-specific discipline:
//   - escape barriers: a static sink field accumulates observed bytes/counts
//     and GC.KeepAlive holds the decoded object — the JIT cannot eliminate
//     the serialized work
//   - streams are reused via Reset (the runtime's documented no-allocation
//     reuse path); the read path decodes into ONE reused instance per bench
//     (the reused-storage discipline — the C# stand-in for C++'s free stack
//     temporary; §5 zeroing makes reuse equivalent on every field that rides)
//   - the driver passes write/read as delegates (one indirect call per op;
//     Rust and C++ get this inlined via generics — noted in the results)
//   - the warmup run per path doubles as the JIT warmup

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Runtime;
using Serialize;

static partial class Program
{
    const int MaxNumRuns = 7;   // median of 7 (N >= 5), after 1 warmup run
    static bool gQuick = false; // --quick: bench_mixed only, 3 measured runs
    static int gNumRuns = MaxNumRuns; // --round K drops this to 1 (§2.4: one warmup +
                                      // one measured run per round; the driver
                                      // aggregates across rounds)
    const int NumVariants = 64; // read-path variant buffers

    // ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
    // Rows are buffered and emitted at exit so every row carries the
    // corpus_id (§1.6): FNV-1a-64 over the goldens THIS RUN actually loaded —
    // for each file in sorted basename order, the basename bytes, a 0x00
    // byte, the contents. The per-runner constants: family gen (these are
    // the generated-code benchmarks), linkage asm (the serialize.cs runtime
    // is a separate assembly the JIT inlines across), checks ALWAYS —
    // serialize.cs has no Debug.Assert and no conditional compilation of
    // its checks: bounds checks, range validation and the sticky error
    // latch are unconditional in every build, §3.4's definition of
    // `always` word for word (this row previously claimed `removed`, which
    // was wrong — there is no NDEBUG-equivalent build of serialize.cs) —
    // opt default (the JIT has no operator-visible optimization levels),
    // inline unknown until the verdict pass (§4.2) backfills it.
    // family is per ROW now (gen | bits — §5.1); linkage/checks/opt/
    // inline stay per-runner constants
    const string CsvSuffix = "asm,always,default,unknown";

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
            // §1.5: a failing run emits NO rows — the exit code and stderr
            // are the whole output. Numbers from a run whose gate refused
            // are not numbers.
            Console.Error.WriteLine("refusing to emit CSV rows from a failing run");
            return;
        }
        string id = CorpusId();
        foreach ((string row, string family) in gCsvRows)
        {
            Console.WriteLine(row + "," + id + "," + family + "," + CsvSuffix);
        }
    }

    // buffers: write buffers must be a multiple of 8 bytes (qword-flush
    // contract); variant buffers keep slack past the packet for the reader's
    // window loads. 4096 covers the largest pinned shape (2008 bytes) with slack.
    const int BufferSize = 4096;

    // §2.7 variant-buffer stride: the 64 rotating read buffers are allocated
    // at BufferSize + 64 per slot, NOT packed at exact 4096. At stride 4096
    // every head line maps into one of 4 L1 set-groups on the M2 (set bits
    // [13:6]: 4096 >> 6 = 64 sets per step, 64k mod 256 cycles
    // {0,64,128,192}); at 4160 the step is 65 and gcd(65,256) = 1: 64 head
    // lines, 64 distinct sets. Identical policy in all five runners. The CLR
    // already inserts object headers between sequentially allocated arrays,
    // so this runner's stride was never exactly 4096 — the pad is applied
    // for uniformity of the §2.7 policy, not because the arithmetic needs
    // it here. Reads bound themselves with bytesPerOp; the pad is address
    // spacing only.
    const int VariantStride = BufferSize + 64;

    static readonly byte[] gBuffer = new byte[BufferSize];
    static readonly byte[] gTwin = new byte[BufferSize];
    static readonly byte[][] gVariants = new byte[NumVariants][];

    static ulong gSink; // defeats dead code elimination of computed values
    static bool gCsv;
    static string gWireDir = Path.Combine("..", "..", "testdata", "wire");
    static string gVariantDir = Path.Combine("..", "..", "bench", "corpus", "variants");
    static bool failed;

    // the LCG every runner must use (Knuth MMIX, as in serialize bench.cpp)
    static ulong BenchRng(ulong rng)
    {
        return rng * 6364136223846793005ul + 1442695040888963407ul;
    }

    static void Fail(string name, string what)
    {
        Console.Error.WriteLine("FAILED: " + name + ": " + what);
        failed = true;
    }

    static bool CheckGolden(string name, ReadOnlySpan<byte> data)
    {
        string path = Path.Combine(gWireDir, name + ".bin");
        byte[] expected;
        try
        {
            expected = File.ReadAllBytes(path);
        }
        catch (Exception)
        {
            Console.Error.WriteLine("missing wire golden " + path + " — run from bench/cs (or pass --wire-dir)");
            return false;
        }
        gGoldensLoaded[name + ".bin"] = expected;
        if (!data.SequenceEqual(expected))
        {
            Console.Error.WriteLine(
                $"WIRE GOLDEN MISMATCH: {name} ({expected.Length} golden vs {data.Length} actual bytes) — refusing to bench code that does not match the corpus");
            return false;
        }
        return true;
    }

    struct RunStats
    {
        public double Median; // ops/sec
        public double Min;
        public double Max;
        public double Spread; // (max - min) / median * 100
    }

    static RunStats Stats(double[] rates)
    {
        Array.Sort(rates);
        int n = rates.Length;
        return new RunStats
        {
            Median = rates[n / 2],
            Min = rates[0],
            Max = rates[n - 1],
            Spread = (rates[n - 1] - rates[0]) / rates[n / 2] * 100.0,
        };
    }

    static void Report(string bench, string path, long iters, long bytesPerOp, RunStats s, string family)
    {
        double mbps = s.Median * bytesPerOp / (1024.0 * 1024.0);
        Console.Error.WriteLine(string.Format(
            "{0,-18} {1,-5} {2,10:F2} M msg/s {3,10:F1} MB/s   (min {4:F2}, max {5:F2}, spread {6:F1}%)",
            bench, path, s.Median / 1e6, mbps, s.Min / 1e6, s.Max / 1e6, s.Spread));
        if (gCsv)
        {
            gCsvRows.Add((string.Format(
                "cs,{0},{1},{2},{3},{4},{5:F0},{6:F0},{7:F0},{8:F2},{9:F2}",
                bench, path, iters, bytesPerOp, gNumRuns, s.Median, s.Min, s.Max, mbps, s.Spread), family));
        }
    }

    // ------------------------------------------------------------------------------------------
    // the DATA-DRIVEN benchmark driver (issue #191)
    // ------------------------------------------------------------------------------------------
    //
    // THE PROPERTY: nothing below names a field of the shape it measures.
    // Shape knowledge lives in the committed variant DATA
    // (bench/corpus/variants, emitted by bench/tools/variantgen) and in the
    // generated codec, and nowhere else — so this driver cannot drift from
    // another language's driver in what it measures, which is the whole
    // reason the design exists. If a change here ever needs a field name, the
    // design has failed and that is the finding.

    // Loads <variant-dir>/<name>.variants.bin into the NumVariants
    // §2.7-staggered slots and returns the record size, or -1. The records are
    // fixed-width by construction (§2.7 pins every structure field), so the
    // file needs no index: the record size IS file size / NumVariants, and a
    // file that does not divide evenly is a refusal.
    static long LoadVariants(string name)
    {
        string path = Path.Combine(gVariantDir, name + ".variants.bin");
        byte[] packed;
        try
        {
            packed = File.ReadAllBytes(path);
        }
        catch (IOException)
        {
            Console.Error.WriteLine("missing variant data " + path
                + " — run `make bench-variants`, and run the bench from bench/cs (or pass --variant-dir)");
            return -1;
        }
        if (packed.Length == 0 || packed.Length % NumVariants != 0)
        {
            Console.Error.WriteLine("variant data " + path + " is " + packed.Length
                + " bytes, not a multiple of " + NumVariants
                + " records — refusing to bench data whose stride is not the record size");
            return -1;
        }
        int record = packed.Length / NumVariants;
        if (record > BufferSize)
        {
            Console.Error.WriteLine("variant data " + path + " has " + record
                + "-byte records, over the " + BufferSize + "-byte buffer");
            return -1;
        }
        for (int k = 0; k < NumVariants; k++)
        {
            Array.Copy(packed, k * record, gVariants[k], 0, record);
        }
        // The variant data is corpus (§1.6): it defines the work inside the
        // timed loops, so it rides in corpus_id exactly as the wire goldens
        // do. A run against drifted variant data reports a different id and
        // the tools refuse the ratio, instead of publishing a number for
        // different work.
        gGoldensLoaded[name + ".variants.bin"] = packed;
        return record;
    }

    // T — the generated message type — is named explicitly at the call site,
    // as in the C++ reference. A TYPE name is not a field name; the driver
    // still knows nothing about the shape's contents.
    static void BenchDataDriven<T>(string name, string golden, long iters,
        Func<WriteStream, T, bool> writeFn, Func<ReadStream, T, bool> readFn)
        where T : class, new()
    {
        long bytesPerOp = LoadVariants(name);
        if (bytesPerOp < 0)
        {
            failed = true;
            return;
        }

        // gate 1 (§1.5): variant 0 IS the pinned instance, so the whole
        // variant file is bound to the wire golden by one byte-compare.
        if (!CheckGolden(golden, gVariants[0].AsSpan(0, (int)bytesPerOp)))
        {
            failed = true;
            return;
        }

        // gate 2: every variant decodes, re-encodes, and comes back
        // byte-identical at the same length. This is stronger than a
        // pinned-instance-only gate — §1.5's named residual (the 64 varied
        // buffers length-checked but never value-checked) closes here, for
        // every variant.
        T[] instances = new T[NumVariants];
        for (int k = 0; k < NumVariants; k++)
        {
            instances[k] = new T();
            ReadStream gateRs = new ReadStream(gVariants[k], (int)bytesPerOp);
            if (!readFn(gateRs, instances[k]))
            {
                Fail(name, "decode of a variant failed");
                return;
            }
            WriteStream gateWs = new WriteStream(gTwin);
            if (!writeFn(gateWs, instances[k]))
            {
                Fail(name, "re-encode of a decoded variant failed");
                return;
            }
            gateWs.Flush();
            if (gateWs.BytesProcessed != bytesPerOp
                || !gTwin.AsSpan(0, (int)bytesPerOp).SequenceEqual(gVariants[k].AsSpan(0, (int)bytesPerOp)))
            {
                Fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
                return;
            }
        }

        double[] writeRates = new double[gNumRuns];
        double[] roundtripRates = new double[gNumRuns];

        // WRITE: encode the 64 pre-decoded instances round-robin. Rotating the
        // instances is what §2.7's per-iteration LCG mutation bought — the
        // encoder never sees the same input twice in a row and cannot
        // precompute scratch words — with none of the per-language mutation
        // code, and with bytes/op constant by construction rather than by
        // assertion. The sink is the byte fold: every iteration's result is a
        // value the loop cannot drop.
        WriteStream ws = new WriteStream(gBuffer);
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                ws.Reset(gBuffer);
                if (!writeFn(ws, instances[i & (NumVariants - 1)]))
                {
                    Fail(name, "write failed in loop");
                    return;
                }
                ws.Flush();
                gSink = gSink + (ulong)ws.BytesProcessed;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                writeRates[run] = iters / time;
            }
        }

        // ROUND-TRIP: decode a variant buffer, then re-encode what came out.
        // The decode needs no sink discipline of its own — its output IS the
        // encode's input, so every decoded field is observed by construction,
        // in every language, with no per-language fold to audit (§2.7's
        // read-side sink problem dissolved rather than equalized). The decode
        // target is hoisted and reused, as everywhere else.
        T outValue = new T();
        ReadStream rs = new ReadStream(gVariants[0], (int)bytesPerOp);
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                rs.Reset(gVariants[i & (NumVariants - 1)], (int)bytesPerOp);
                if (!readFn(rs, outValue))
                {
                    Fail(name, "read failed in loop");
                    return;
                }
                ws.Reset(gBuffer);
                if (!writeFn(ws, outValue))
                {
                    Fail(name, "re-write failed in loop");
                    return;
                }
                ws.Flush();
                gSink = gSink + (ulong)ws.BytesProcessed;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                roundtripRates[run] = iters / time;
            }
        }
        GC.KeepAlive(outValue);

        RunStats w = Stats(writeRates);
        RunStats rt = Stats(roundtripRates);
        Report(name, "write", iters, bytesPerOp, w, "gen");
        Report(name, "round_trip", iters, bytesPerOp, rt, "gen");

        // READ is DERIVED, never measured: round-trip time minus write time.
        // It prints for continuity with the read rows the rest of the corpus
        // still reports and is NOT a CSV row — a derived number in the CSV
        // would be divided as if it had been measured.
        double readTime = 1.0 / rt.Median - 1.0 / w.Median;
        if (readTime > 0)
        {
            Console.Error.WriteLine(string.Format(CultureInfo.InvariantCulture,
                "{0,-18} {1,-5} {2,10:F2} M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)",
                name, "read", 1e-6 / readTime));
        }

        // alloc gate (proof of the reuse discipline, not a benchmark): bytes
        // allocated during one extra untimed pass of each path — nonzero
        // FAILS the leg, so a future allocation goes red in the sweep
        const int allocOps = 4 * NumVariants;
        long allocBefore = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < allocOps; i++)
        {
            ws.Reset(gBuffer);
            if (!writeFn(ws, instances[i & (NumVariants - 1)]))
            {
                Fail(name, "write failed in alloc pass");
                return;
            }
            ws.Flush();
            gSink = gSink + (ulong)ws.BytesProcessed;
        }
        long writeAlloc = GC.GetAllocatedBytesForCurrentThread() - allocBefore;
        allocBefore = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < allocOps; i++)
        {
            rs.Reset(gVariants[i & (NumVariants - 1)], (int)bytesPerOp);
            if (!readFn(rs, outValue))
            {
                Fail(name, "read failed in alloc pass");
                return;
            }
            ws.Reset(gBuffer);
            if (!writeFn(ws, outValue))
            {
                Fail(name, "re-write failed in alloc pass");
                return;
            }
            ws.Flush();
            gSink = gSink + (ulong)ws.BytesProcessed;
        }
        long roundtripAlloc = GC.GetAllocatedBytesForCurrentThread() - allocBefore;
        Console.Error.WriteLine(
            $"alloc note: {name} one pass ({allocOps} ops/path): write {writeAlloc} bytes, round_trip {roundtripAlloc} bytes");
        if (writeAlloc != 0 || roundtripAlloc != 0)
        {
            Fail(name, $"allocation in generated code: write {writeAlloc} bytes, round_trip {roundtripAlloc} bytes (the contract is 0)");
        }
    }

    static int Main(string[] args)
    {
        CultureInfo.DefaultThreadCurrentCulture = CultureInfo.InvariantCulture;

        for (int i = 0; i < args.Length; i++)
        {
            if (args[i] == "--csv")
            {
                gCsv = true;
            }
            else if (args[i] == "--variant-dir" && i + 1 < args.Length)
            {
                gVariantDir = args[++i];
            }
            else if (args[i] == "--wire-dir" && i + 1 < args.Length)
            {
                gWireDir = args[++i];
            }
            else if (args[i] == "--round" && i + 1 < args.Length)
            {
                // §2.4: one warmup + one measured run of every benchmark,
                // then exit. K only identifies the round to the interleaved
                // driver, which aggregates across rounds itself.
                if (!int.TryParse(args[++i], out int k) || k < 0)
                {
                    Console.Error.WriteLine($"--round takes a non-negative integer, got '{args[i]}'");
                    return 1;
                }
                gNumRuns = 1;
            }
            else if (args[i] == "--quick")
            {
                gQuick = true;
            }
            else
            {
                Console.Error.WriteLine("usage: schemabench [--csv] [--round K] [--quick] [--wire-dir <dir>] [--variant-dir <dir>]");
                return 1;
            }
        }
        if (gQuick && gNumRuns == MaxNumRuns)
        {
            gNumRuns = 3;
        }

        // dotnet run leaves the working directory at the project dir
        // (bench/cs); a fallback resolves the goldens from the build output
        // directory so the binary stays honest whatever the working directory.
        if (!Directory.Exists(gWireDir))
        {
            string fallback = Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "testdata", "wire");
            if (Directory.Exists(fallback))
            {
                gWireDir = fallback;
            }
        }
        if (!Directory.Exists(gVariantDir))
        {
            string fallback = Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "bench", "corpus", "variants");
            if (Directory.Exists(fallback))
            {
                gVariantDir = fallback;
            }
        }

        for (int k = 0; k < NumVariants; k++)
        {
            gVariants[k] = new byte[VariantStride];   // §2.7 stride stagger
        }

        Console.Error.WriteLine(
            $"schema bench (cs, {(GCSettings.IsServerGC ? "server" : "workstation")} GC)");

        if (gQuick)
        {
            // --quick: the iteration instrument, never the certification
            // instrument — bench_mixed only, 3 measured runs.
            Console.Error.WriteLine("--quick: iteration instrument, not certification");
        }

        // family gen over the Bench corpus: BenchMixed through the generated
        // code, fed by the committed variant corpus — same goldens, same
        // iteration count in every runner (§2.1). No hand-written pin, vary
        // or sink code participates in this leg.
        BenchDataDriven<Bench.BenchMixed>("bench_mixed", "bench_mixed", 4000000, Bench.Schema.WriteBenchMixed, Bench.Schema.ReadBenchMixed);

        // family bits (§1.4): the one bitpacker workload in the estate
        if (!gQuick)
        {
            BenchBitpacker(24576);
        }

        FlushCsv(); // rows carry the corpus_id of the goldens this run loaded

        if (failed)
        {
            Console.Error.WriteLine($"BENCH FAILED (corpus_id {CorpusId()})");
            return 1;
        }

        Console.Error.WriteLine($"OK (corpus_id {CorpusId()})");
        GC.KeepAlive(gSink);
        return 0;
    }
}
