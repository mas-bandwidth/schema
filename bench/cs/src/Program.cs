// schema bench — the C# runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated C# and the serialize.cs runtime: same benchmark set, same
// pinned corpus instances, same LCG (unchecked ulong arithmetic) and
// vary-function field mappings, same golden + round-trip self-checks (a
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
//   - the driver passes write/read/vary as delegates (one indirect call per
//     op; Rust and C++ get this inlined via generics — noted in the results)
//   - the warmup run per path doubles as the JIT warmup

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Runtime;
using Example;
using Serialize;
using static Example.Schema;

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
    // family is per ROW now (gen | rt | bits — §5.1); linkage/checks/opt/
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
    // the per-message benchmark driver
    // ------------------------------------------------------------------------------------------

    static void BenchMessage<T>(string name, string golden, long iters, T pinned,
        Func<WriteStream, T, bool> writeFn, Func<ReadStream, T, bool> readFn, Action<T, ulong> varyFn)
        where T : class, new()
    {
        // self-check 1: the pinned instance matches its wire golden byte-for-byte
        T baseValue = pinned;
        WriteStream ws = new WriteStream(gBuffer);
        if (!writeFn(ws, baseValue))
        {
            Fail(name, "write of pinned instance failed");
            return;
        }
        ws.Flush();
        long bytesPerOp = ws.BytesProcessed;
        if (golden != null && !CheckGolden(golden, gBuffer.AsSpan(0, (int)bytesPerOp)))
        {
            failed = true;
            return;
        }

        // self-check 2: round-trip write -> read -> re-write -> identical bytes
        {
            T output = new T();
            ReadStream checkRs = new ReadStream(gBuffer, (int)bytesPerOp);
            if (!readFn(checkRs, output))
            {
                Fail(name, "read of pinned instance failed");
                return;
            }
            WriteStream tws = new WriteStream(gTwin);
            if (!writeFn(tws, output))
            {
                Fail(name, "re-write of decoded instance failed");
                return;
            }
            tws.Flush();
            if (tws.BytesProcessed != bytesPerOp
                || !gTwin.AsSpan(0, (int)bytesPerOp).SequenceEqual(gBuffer.AsSpan(0, (int)bytesPerOp)))
            {
                Fail(name, "round-trip bytes differ");
                return;
            }
        }

        // variant buffers for the read path (and proof that variation keeps bytes/op constant)
        ulong rng = 1;
        for (int k = 0; k < NumVariants; k++)
        {
            rng = BenchRng(rng);
            varyFn(baseValue, rng);
            WriteStream vs = new WriteStream(gVariants[k]);
            if (!writeFn(vs, baseValue))
            {
                Fail(name, "write of varied instance failed");
                return;
            }
            vs.Flush();
            if (vs.BytesProcessed != bytesPerOp)
            {
                Fail(name, "variation changed bytes/op — vary must keep structure fields fixed");
                return;
            }
        }

        double[] writeRates = new double[gNumRuns];
        double[] readRates = new double[gNumRuns];

        // write path: 1 warmup (also the JIT warmup) + NumRuns measured
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long i = 0; i < iters; i++)
            {
                rng = BenchRng(rng);
                varyFn(baseValue, rng);
                ws.Reset(gBuffer);
                if (!writeFn(ws, baseValue))
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

        // read path: 1 warmup + NumRuns measured; ONE reused decode instance
        // (the reused-storage discipline — see the file header)
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
                gSink = gSink + 1;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                readRates[run] = iters / time;
            }
        }
        GC.KeepAlive(outValue); // every decoded field is observed

        Report(name, "write", iters, bytesPerOp, Stats(writeRates), "gen");
        Report(name, "read", iters, bytesPerOp, Stats(readRates), "gen");

        // alloc note (proof of the reuse discipline, not a benchmark): bytes
        // allocated during one extra untimed pass of each path — must be 0
        const int allocOps = 4 * NumVariants;
        long allocBefore = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < allocOps; i++)
        {
            rng = BenchRng(rng);
            varyFn(baseValue, rng);
            ws.Reset(gBuffer);
            if (!writeFn(ws, baseValue))
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
            gSink = gSink + 1;
        }
        long readAlloc = GC.GetAllocatedBytesForCurrentThread() - allocBefore;
        Console.Error.WriteLine(
            $"alloc note: {name} one pass ({allocOps} ops/path): write {writeAlloc} bytes, read {readAlloc} bytes");
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
    //
    // It replaces BenchMessage for bench_mixed only. BenchMessage still
    // drives every shape whose harness code is not yet data-driven.

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
        // byte-identical at the same length. This is stronger than the
        // pinned-instance-only gate BenchMessage applies — §1.5's named
        // residual (the 64 varied buffers length-checked but never
        // value-checked) closes here, for every variant.
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

        // alloc note (proof of the reuse discipline, not a benchmark): bytes
        // allocated during one extra untimed pass of each path — must be 0
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
    }

    // ------------------------------------------------------------------------------------------
    // pinned corpus instances — the C++ reference's pin_* functions exactly
    // (the same instances test/cs/src/Program.cs pins to the wire goldens)
    // ------------------------------------------------------------------------------------------

    static RigidBody PinRigidBodyMoving()
    {
        RigidBody input = new RigidBody();
        input.Position.X = 1.5;
        input.Position.Y = -2.5;
        input.Position.Z = 3.25;
        input.Orientation.X = 0.1;
        input.Orientation.Y = 0.2;
        input.Orientation.Z = 0.3;
        input.Orientation.W = 0.9;
        input.AtRest = false;
        input.LinearVelocity.X = 10.0;
        input.LinearVelocity.Y = 20.0;
        input.LinearVelocity.Z = -3.0;
        input.AngularVelocity.X = 0.25;
        input.AngularVelocity.Y = 0.5;
        input.AngularVelocity.Z = 0.75;
        return input;
    }

    static void SetText(byte[] buffer, string text)
    {
        for (int i = 0; i < text.Length; i++)
        {
            buffer[i] = (byte)text[i];
        }
    }

    static Chat PinChat()
    {
        Chat input = new Chat();
        SetText(input.Text, "wire parity");
        input.TextLength = 11;
        return input;
    }

    static InputPacket PinInputPacket()
    {
        InputPacket input = new InputPacket();
        input.SynchronizeSequence = 7;
        input.CurrentFrame = 123456789;
        input.StartFrame = 123456780;
        input.InputsCount = 2;
        input.Inputs[0].Throttle = 0.5f;
        input.Inputs[0].Fire = true;
        input.Inputs[1].StickX = -0.25f;
        input.Inputs[1].Boost = true;
        return input;
    }

    static ShipCreate PinShipCreate()
    {
        ShipCreate input = new ShipCreate();
        input.ShipType = ShipType.Bomber;
        input.Position.X = 1000;
        input.Position.Y = -2000;
        input.Position.Z = 3000;
        input.HasFlags = true;
        input.Flags = ShipFlagsBoosting | ShipFlagsAiming;
        input.Team = Team.Blue;
        input.Health = 750;
        input.Thrust = 55;
        return input;
    }



    static ProbeHeader PinProbeHeader()
    {
        ProbeHeader h = new ProbeHeader();
        h.Version = 5;
        h.ProbeId = 0x1122334455667788;
        return h;
    }

    static ProbeBits PinProbeBits()
    {
        ProbeBits input = new ProbeBits();
        input.Small = 0x1FF;
        input.Boundary = 0x1FFFFFFFF;
        input.Wide = 0xFEDCBA9876543210;
        input.Sensor = 4294967295;
        input.Nonce = 18446744073709551615;
        return input;
    }

    static ProbeArray PinProbeArray()
    {
        ProbeArray input = new ProbeArray(); // construction carries the defaults — as the C++ default ctor
        input.Samples[0].Orientation = 90.0f;
        input.Samples[0].RawDelta = -5;
        input.Samples[0].BigDelta = -1234567890123;
        input.Samples[0].Weapon = Weapon.Laser;
        input.Samples[0].HasTarget = true;
        input.Samples[0].TargetId = 777;
        input.Samples[0].SamplesCount = 1;
        input.Samples[0].Samples[0] = 42;
        input.Samples[1].Active = false;
        input.Samples[1].Orientation = -45.5f;
        input.Samples[1].RawDelta = 7;
        input.Samples[1].BigDelta = 99;
        input.Samples[1].IdleTicks = 1000;
        input.Samples[1].SamplesCount = 2;
        input.Samples[1].Samples[0] = 7;
        input.Samples[1].Samples[1] = 8;
        input.Config.Retries = 3;
        input.Config.Preferred = Weapon.Missile;
        return input;
    }

    static TestData PinTestData()
    {
        TestData input = new TestData();
        input.A = -100;
        input.B = 100;
        input.C = 149;
        input.D = 0x11;
        input.E = 0x22;
        input.F = 0x33;
        input.G = true;
        input.ItemsCount = 3;
        input.Items[0] = 0;
        input.Items[1] = 128;
        input.Items[2] = 255;
        input.FloatValue = 3.1415926f;
        input.CompressedFloatValue = 2.5f;
        input.DoubleValue = 1.0 / 3.0;
        input.Int8Value = -128;
        input.Int16Value = -32768;
        input.Uint8Value = 255;
        input.Uint16Value = 65535;
        input.Uint32Value = 4294967295;
        input.Uint64Value = 18446744073709551615;
        input.Int64Full = long.MinValue;
        input.Int64Range = -999999999999;
        for (int i = 0; i < 17; i++)
        {
            input.FixedBytes[i] = (byte)(i * 3);
        }
        SetText(input.Text, "the quick brown fox");
        input.TextLength = 19;
        return input;
    }

    // ------------------------------------------------------------------------------------------
    // vary functions — the C++ reference's vary_* field mappings exactly:
    // VALUE fields mutate within wire ranges through the LCG; structure
    // fields (counts, lengths, branch bools) stay fixed so bytes/op is
    // constant.
    // ------------------------------------------------------------------------------------------

    static void VaryRigidBody(RigidBody m, ulong rng)
    {
        m.Position.X = (double)((long)(rng >> 8) & 0xFFFF) * 0.25;
        m.Position.Y = (double)((long)(rng >> 16) & 0xFFFF) * 0.5;
        m.Position.Z = (double)((long)(rng >> 24) & 0xFFFF) * 0.125;
        m.Orientation.X = (double)((long)rng & 0xFF) * 0.001;
        m.LinearVelocity.X = (double)((long)(rng >> 32) & 0xFFF) * 0.25;
        m.AngularVelocity.Z = (double)((long)(rng >> 40) & 0xFFF) * 0.125;
    }

    static void VaryRigidBodyAtRest(RigidBody m, ulong rng)
    {
        m.Position.X = (double)((long)(rng >> 8) & 0xFFFF) * 0.25;
        m.Position.Y = (double)((long)(rng >> 16) & 0xFFFF) * 0.5;
        m.Orientation.X = (double)((long)rng & 0xFF) * 0.001;
    }

    static void VaryChat(Chat m, ulong rng)
    {
        for (int i = 0; i < m.TextLength; i++)
        {
            m.Text[i] = (byte)('a' + (int)((rng >> (i & 7)) & 15)); // never zero
        }
    }

    static void VaryTest(Test m, ulong rng)
    {
        m.TestA = (ushort)rng;
        m.TestB = (short)((rng >> 16) & 511); // within [0, 1000]
        m.TestC = (short)((rng >> 25) & 511);
        m.TestD = (short)((rng >> 34) & 511);
    }

    static void VaryInputPacket(InputPacket m, ulong rng)
    {
        m.SynchronizeSequence = (ushort)rng;
        m.CurrentFrame = rng;
        m.StartFrame = rng >> 1;
        m.Inputs[0].Throttle = ((uint)rng & 0xFF) / 256.0f;
        m.Inputs[0].Fire = (rng & 1) != 0;
        m.Inputs[1].StickX = ((uint)(rng >> 8) & 0xFF) / 256.0f - 0.5f;
        m.Inputs[1].Boost = (rng & 2) != 0;
    }

    static void VaryShipCreate(ShipCreate m, ulong rng)
    {
        m.Position.X = (int)((rng >> 8) & 0xFFFFF) - 0x80000; // within [-8388608, 8388608]
        m.Position.Y = (int)((rng >> 16) & 0xFFFFF) - 0x80000;
        m.Position.Z = (int)((rng >> 24) & 0xFFFFF) - 0x80000;
        m.Rotation.X = (short)((int)(rng & 0x7FF) - 1024); // within [-1024, 1024]
        m.LinearVelocity.X = (int)((rng >> 32) & 0x3FFFFF) - 2097152;
        m.Flags = rng & 15; // 4 wire bits, has_flags stays true
        m.Health = (short)((rng >> 5) & 511); // within [0, 1000]
        m.Thrust = (sbyte)((rng >> 14) & 63); // within [0, 100]
    }



    static void VaryProbeHeader(ProbeHeader m, ulong rng)
    {
        m.Version = (uint)rng & 7; // 3 wire bits
        m.ProbeId = rng;
    }

    static void VaryProbeBits(ProbeBits m, ulong rng)
    {
        m.Small = (uint)rng & 511; // 9 bits
        m.Boundary = rng & ((1ul << 33) - 1); // 33 bits
        m.Wide = rng * 3;
        m.Sensor = (uint)(rng >> 16);
        m.Nonce = rng ^ 0x5555555555555555ul;
    }

    static void VaryProbeArray(ProbeArray m, ulong rng)
    {
        m.Samples[0].Orientation = -180.0f + ((uint)rng & 0x3FFF) * 0.02f;
        m.Samples[0].RawDelta = (int)(uint)(rng >> 8);
        m.Samples[0].BigDelta = (long)(rng * 5);
        m.Samples[0].TargetId = (ushort)(rng >> 24);
        m.Samples[0].Samples[0] = (ushort)(rng >> 40);
        m.Samples[1].Orientation = -180.0f + ((uint)(rng >> 3) & 0x3FFF) * 0.02f;
        m.Samples[1].IdleTicks = (uint)(rng >> 32);
        m.Samples[1].Samples[0] = (ushort)(rng >> 4);
        m.Samples[1].Samples[1] = (ushort)(rng >> 12);
        m.Config.Retries = (int)(uint)(rng >> 20);
    }

    static void VaryTestData(TestData m, ulong rng)
    {
        m.A = (int)(rng & 127) - 64; // within [-100, 100]
        m.B = (int)((rng >> 7) & 127) - 64;
        m.C = (int)((rng >> 14) & 127) - 64; // within [-100, 150]
        m.D = (uint)rng & 255;
        m.E = (uint)(rng >> 8) & 255;
        m.F = (uint)(rng >> 16) & 255;
        m.Items[0] = (int)(rng & 255); // items_count stays 3
        m.Items[1] = (int)((rng >> 8) & 255);
        m.Items[2] = (int)((rng >> 16) & 255);
        m.FloatValue = (uint)rng & 0xFFFF;
        m.CompressedFloatValue = ((uint)rng & 1023) * 0.005f; // within [0, 10] (max 5.115)
        m.DoubleValue = (double)((long)(rng >> 16) & 0xFFFFFF) * 0.5;
        m.Int8Value = (sbyte)rng;
        m.Int16Value = (short)(rng >> 8);
        m.Uint8Value = (byte)(rng >> 16);
        m.Uint16Value = (ushort)(rng >> 24);
        m.Uint32Value = (uint)(rng >> 32);
        m.Uint64Value = rng * 7;
        m.Int64Full = (long)(rng * 11);
        m.Int64Range = (long)((rng >> 24) & ((1ul << 37) - 1)) - (1L << 36); // within +/- 1e12
        m.FixedBytes[0] = (byte)rng;
        m.FixedBytes[16] = (byte)(rng >> 8);
        for (int i = 0; i < m.TextLength; i++)
        {
            m.Text[i] = (byte)('a' + (int)((rng >> (i & 7)) & 15)); // never zero
        }
    }

    // real_packet — BENCH-STANDARD.md §1.7's realistic snapshot, measured
    // through the GENERATED code (bench/corpus/RealWorld.schema ->
    // generated/bench/cs/realworld, namespace Realworld — referenced
    // qualified so the two units' same-named table types never collide).
    // The pinned instance is the ALL-DEFAULTS instance: new RealPacket()
    // serialized unmodified (field initializers carry the schema defaults),
    // 1629 bits = 204 wire bytes, pinned to testdata/wire/real_packet.bin by
    // test/bench/main.cpp. The four branch gates (f012 true, f043 false,
    // f050 true, f074 false) are STRUCTURE (§2.7): they keep their schema
    // defaults here, so the same branch bodies ride every iteration and
    // bytes/op is constant. The field mappings are bench/cpp/bench_main.cpp's
    // vary_real_packet exactly — fields under the false gates do not ride
    // and are not varied; every mapping keeps its field inside its declared
    // wire range (comments give the bound it stays within).
    static void VaryRealPacket(Realworld.RealPacket m, ulong rng)
    {
        // ranged ints, assorted widths, signed and unsigned
        m.F001Int = (int)((rng >> 8) & 0xFFFFF) - 0x80000; // +/-2^19 within +/-805495
        m.F003Int = (int)((rng >> 12) & 0xFFFFF) - 0x80000; // within +/-835897
        m.F005Uint = (ushort)((rng >> 20) & 0xFFF); // <=4095 within [0, 7316]
        m.F006Int = (short)((int)((rng >> 26) & 0x7FF) - 1024); // +/-1024 within +/-1513
        m.F009Int = (sbyte)((int)((rng >> 33) & 31) - 16); // +/-16 within +/-22
        m.F033Uint = (uint)((rng >> 37) & 0x1FFFF); // <=131071 within [0, 142780]
        m.F041Int = (sbyte)((int)((rng >> 42) & 63) - 32); // +/-32 within +/-55
        m.F062Uint = (ushort)((rng >> 47) & 255); // <=255 within [0, 503]
        m.F088Int = (short)((int)((rng >> 52) & 0x3FF) - 512); // +/-512 within +/-694
        m.F090Uint = (byte)((rng >> 57) & 127); // <=127 within [0, 214]
        // bits(N), narrow and wide
        m.F011Bits = (uint)rng & 0x3FF; // 10 bits
        m.F023Bits = (uint)(rng >> 5) & 0x1FFFFFF; // 25 bits
        m.F042Bits = (uint)(rng >> 3) & 0x3FFFFFFF; // 30 bits
        m.F081Bits = (uint)(rng >> 7) & 0x1FFFFFFF; // 29 bits
        m.F089Bits = rng & 0xFFFFFFFFFFFFul; // 48 bits
        m.F093Bits = rng ^ 0x5555555555555555ul; // 64 bits
        m.F097Bits = (uint)(rng >> 11) & 0xFFF; // 12 bits
        // bools (NEVER the four branch gates — those are structure, §2.7)
        m.F037Bool = (rng & 1) != 0;
        m.F055Bool = (rng & 2) != 0;
        m.F092Bool = (rng & 4) != 0;
        // float32 / float64
        m.F007F32 = (uint)rng & 0xFFFF;
        m.F020F32 = ((uint)(rng >> 16) & 0xFFFF) * 0.5f;
        m.F058F32 = ((uint)(rng >> 24) & 0xFFFF) * 0.25f;
        m.F002F64 = (double)((long)(rng >> 8) & 0xFFFFFF) * 0.5;
        m.F059F64 = (double)((long)(rng >> 16) & 0xFFFFFF) * 0.25;
        m.F087F64 = (double)((long)(rng >> 24) & 0xFFFFFF) * 0.125;
        // compressed floats (in range by construction)
        m.F004Cf32 = ((uint)rng & 0x3FFF) * 0.1f; // <=1638.3 within [0, 2000]
        m.F061Cf32 = -90.0f + ((uint)(rng >> 9) & 255) * 0.5f; // within [-90, 90] (max 37.5)
        m.F067Cf32 = -100.0f + ((uint)(rng >> 18) & 511) * 0.25f; // within [-100, 100] (max 27.75)
        m.F072Cf32 = ((uint)(rng >> 27) & 8191) * 0.01f; // <=81.91 within [0, 100]
        // fixed / ufixed (raw storage scaled by 2^F; bounds are whole units)
        m.F016Fixed = (int)((rng >> 10) & 0x3FFFFFF) - 0x2000000; // +/-2^25 within +/-36*2^20
        m.F025Fixed = (short)((int)((rng >> 18) & 0x7FFF) - 0x4000); // +/-2^14 within +/-119*2^8
        m.F095Fixed = (int)((rng >> 22) & 0x7FFFFFF) - 0x4000000; // +/-2^26 within +/-1577*2^16
        m.F021Ufixed = (uint)(rng >> 30) & 0x3FFFFFF; // <=2^26-1 within 25141*2^12
        m.F049Ufixed = (ushort)((rng >> 36) & 0x7FFF); // <=32767 within 3*2^14
        m.F084Ufixed = (byte)((rng >> 44) & 0x7F); // <=127 within 1*2^7
        // enum / flags (wire-valid by construction)
        m.F036Enum = (Realworld.PacketMode)((uint)(rng >> 30) & 3); // within wire range [0, 5]
        m.F083Enum = (Realworld.PacketMode)((uint)(rng >> 34) & 3);
        m.F091Flags = rng & 31; // 5 wire bits
        // full-width 64-bit
        m.F008U64 = rng;
        m.F029I64 = (long)(rng * 3);
        m.F063I64 = (long)(rng * 5);
        // fields riding inside the TAKEN branches (f012 true, f050 true)
        m.F013F32 = (uint)(rng >> 4) & 0xFFFF;
        m.F014Uint = (ushort)((rng >> 21) & 511); // <=511 within [0, 775]
        m.F015Int = (sbyte)((int)((rng >> 40) & 31) - 16); // +/-16 within +/-21
        m.F017Uint = (ushort)((rng >> 29) & 0xFFF); // <=4095 within [0, 4606]
        m.F051Bool = (rng & 8) != 0;
        m.F052Int = (sbyte)((int)((rng >> 38) & 63) - 32); // +/-32 within +/-57
        m.F053F32 = ((uint)(rng >> 40) & 0xFFFF) * 0.125f;
        m.F054Int = (sbyte)((int)((rng >> 45) & 63) - 32); // +/-32 within +/-35
    }



    // ------------------------------------------------------------------------------------------
    // family gen over the Bench corpus (issue #177): the four Bench.schema
    // shapes measured through the GENERATED code (generated/bench/cs,
    // namespace Bench) — the gen twins of the rt rows (RtBench.cs), which
    // serialize the same shapes BY HAND against the runtime API. Same golden
    // files, same pinned values, same LCG field mappings, same BenchMessage
    // discipline as every gen row above; the family column carries the
    // subject, and relative.go refuses gen-vs-rt ratios. Generated best case
    // per the profiling doctrine (#170): the plain Release build, no PGO
    // beyond the JIT's own defaults.
    // ------------------------------------------------------------------------------------------

    static Bench.BenchPacket PinGenPacket()
    {
        Bench.BenchPacket input = new Bench.BenchPacket();
        input.A = -37;
        input.B = 12345;
        input.C = 987654;
        input.Bits7 = 97;
        input.Bits13 = 5000;
        input.Bits23 = 1234567;
        input.Flag = true;
        input.X = 1.5f;
        input.Y = -3.25f;
        input.Z = 100.125f;
        input.Big = 0x123456789ABCDEF0;
        for (int i = 0; i < 17; i++)
        {
            input.Blob[i] = (byte)(i * 31);
        }
        return input;
    }

    static Bench.BenchInts PinGenInts()
    {
        Bench.BenchInts input = new Bench.BenchInts();
        input.F0 = -37;
        input.F1 = 12345;
        input.F2 = 987654;
        input.F3 = 2;
        input.F4 = -15;
        input.F5 = 777;
        input.F6 = -2048;
        input.F7 = 200;
        input.F8 = -543210;
        input.F9 = 99;
        return input;
    }

    static Bench.BenchBits PinGenBits()
    {
        Bench.BenchBits input = new Bench.BenchBits();
        input.B7 = 97;
        input.B13 = 5000;
        input.B23 = 1234567;
        input.B3 = 5;
        input.B32 = 0xDEADBEEF;
        input.B11 = 1024;
        input.B19 = 333333;
        input.B48 = 0xFEDCBA987654;
        return input;
    }

    static void VaryGenPacket(Bench.BenchPacket p, ulong rng)
    {
        p.A = (int)((rng >> 8) & 63) - 32;
        p.B = (int)((uint)(rng >> 16) & 65535);
        p.C = (int)((rng >> 24) & 0xFFFFF) - 500000;
        p.Bits7 = (uint)rng & 127;
        p.Bits13 = (uint)(rng >> 3) & 8191;
        p.Bits23 = (uint)(rng >> 5) & 8388607;
        p.Flag = (rng & 1) != 0;
        p.X = (uint)rng & 0xFFFF;
        p.Big = rng;
        p.Blob[0] = (byte)(rng >> 32);
    }

    static void VaryGenInts(Bench.BenchInts f, ulong rng)
    {
        f.F0 = (int)((rng >> 8) & 63) - 32;
        f.F1 = (int)((uint)(rng >> 16) & 65535);
        f.F2 = (int)((rng >> 24) & 0xFFFFF) - 500000;
        f.F3 = (int)((uint)(rng >> 2) & 3);
        f.F4 = (int)((rng >> 11) & 15) - 8;
        f.F5 = (int)((uint)(rng >> 22) & 511);
        f.F6 = (int)((rng >> 33) & 2047) - 1024;
        f.F7 = (int)((uint)(rng >> 40) & 255);
        f.F8 = (int)((rng >> 30) & 0xFFFFF) - 500000;
        f.F9 = (int)((uint)(rng >> 57) & 63);
    }

    static void VaryGenBits(Bench.BenchBits f, ulong rng)
    {
        f.B7 = (uint)rng & 127;
        f.B13 = (uint)(rng >> 3) & 8191;
        f.B23 = (uint)(rng >> 5) & 8388607;
        f.B3 = (uint)(rng >> 29) & 7;
        f.B32 = (uint)(rng >> 16);
        f.B11 = (uint)(rng >> 37) & 2047;
        f.B19 = (uint)(rng >> 44) & 524287;
        f.B48 = rng & 0xFFFFFFFFFFFFul;
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
            // --quick: bench_mixed only, 3 measured runs — the iteration
            // instrument, never the certification instrument. Golden gate
            // unconditional (BenchRt gates before timing).
            // The gen row is the schema subject (the blended table's row);
            // the rt row rides beside it as the hand-written-usage subject.
            Console.Error.WriteLine("--quick: iteration instrument, not certification");
            BenchDataDriven<Bench.BenchMixed>("bench_mixed", "bench_mixed", 4000000, Bench.Schema.WriteBenchMixed, Bench.Schema.ReadBenchMixed);
            BenchRtMixed();
            FlushCsv();
            if (failed)
            {
                Console.Error.WriteLine($"BENCH FAILED (corpus_id {CorpusId()})");
                return 1;
            }
            Console.Error.WriteLine($"OK (corpus_id {CorpusId()})");
            GC.KeepAlive(gSink);
            return 0;
        }

        // rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
        RigidBody atRest = PinRigidBodyMoving();
        atRest.AtRest = true;

        BenchMessage("rigidbody_moving", "rigidbody_moving", 24000000, PinRigidBodyMoving(), WriteRigidBody, ReadRigidBody, VaryRigidBody);
        BenchMessage("rigidbody_at_rest", "rigidbody_at_rest", 32000000, atRest, WriteRigidBody, ReadRigidBody, VaryRigidBodyAtRest);
        BenchMessage("chat", "chat", 48000000, PinChat(), WriteChat, ReadChat, VaryChat);
        BenchMessage("test", null, 192000000, new Test(), WriteTest, ReadTest, VaryTest);
        BenchMessage("inputpacket", "inputpacket", 16000000, PinInputPacket(), WriteInputPacket, ReadInputPacket, VaryInputPacket);
        BenchMessage("shipcreate", "shipcreate_flags", 32000000, PinShipCreate(), WriteShipCreate, ReadShipCreate, VaryShipCreate);
        BenchMessage("probe_header", "probe_header", 256000000, PinProbeHeader(), WriteProbeHeader, ReadProbeHeader, VaryProbeHeader);
        BenchMessage("probebits", "probebits", 128000000, PinProbeBits(), WriteProbeBits, ReadProbeBits, VaryProbeBits);
        BenchMessage("probearray", "probearray", 20000000, PinProbeArray(), WriteProbeArray, ReadProbeArray, VaryProbeArray);
        BenchMessage("testdata", "testdata", 8000000, PinTestData(), WriteTestData, ReadTestData, VaryTestData);

        // real_packet (§1.7): the realistic snapshot — ~93 riding individually
        // serialized small fields, 204 wire bytes, 0% bulk share by bits. The
        // pin is the ALL-DEFAULTS instance (new RealPacket() — the C++
        // RealPacket{}), golden-gated like every row above. Iteration count
        // sized in the C++ reference (§2.1).
        BenchMessage("real_packet", "real_packet", 8000000, new Realworld.RealPacket(), Realworld.Schema.WriteRealPacket, Realworld.Schema.ReadRealPacket, VaryRealPacket);

        // family gen over the Bench corpus (issue #177): the generated twins
        // of the rt rows below — same shapes, same goldens, same pins, same
        // vary mappings, same iteration counts (fixed and identical across
        // all five runners, §2.1); only the subject differs, and the family
        // column says so.
        BenchMessage("bench_packet", "bench_packet", 32000000, PinGenPacket(), Bench.Schema.WriteBenchPacket, Bench.Schema.ReadBenchPacket, VaryGenPacket);
        BenchMessage("bench_ints", "bench_ints", 40000000, PinGenInts(), Bench.Schema.WriteBenchInts, Bench.Schema.ReadBenchInts, VaryGenInts);
        BenchMessage("bench_bits", "bench_bits", 48000000, PinGenBits(), Bench.Schema.WriteBenchBits, Bench.Schema.ReadBenchBits, VaryGenBits);
        BenchDataDriven<Bench.BenchMixed>("bench_mixed", "bench_mixed", 4000000, Bench.Schema.WriteBenchMixed, Bench.Schema.ReadBenchMixed);

        // family rt (§1.3/§1.5): the runtime API by hand, oracle-gated
        // against the goldens the generated code pinned. Iteration counts
        // are fixed and identical across all five runners (§2.1; sized in
        // the C++ reference).
        BenchRtAll();

        // family bits (§1.4): the one bitpacker workload in the estate
        BenchBitpacker(24576);

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
