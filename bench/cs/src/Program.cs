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
//     (the MessageStorage discipline — the C# stand-in for C++'s free stack
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

static class Program
{
    const int MaxNumRuns = 7;   // median of 7 (N >= 5), after 1 warmup run
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
    // is a separate assembly the JIT inlines across), checks removed (the
    // Release build compiles range and bounds checks away — the
    // NDEBUG-equivalent semantics, §3.4), opt default (the JIT has no
    // operator-visible optimization levels), inline unknown until the
    // verdict pass (§4.2) backfills it.
    const string CsvSuffix = "gen,asm,removed,default,unknown";

    static readonly List<string> gCsvRows = new List<string>();
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
        string id = CorpusId();
        foreach (string row in gCsvRows)
        {
            Console.WriteLine(row + "," + id + "," + CsvSuffix);
        }
    }

    // buffers: write buffers must be a multiple of 8 bytes (qword-flush
    // contract); variant buffers keep slack past the packet for the reader's
    // window loads. 4096 covers MessageMaxBytes (2008) with slack.
    const int BufferSize = 4096;

    static readonly byte[] gBuffer = new byte[BufferSize];
    static readonly byte[] gTwin = new byte[BufferSize];
    static readonly byte[][] gVariants = new byte[NumVariants][];

    static ulong gSink; // defeats dead code elimination of computed values
    static bool gCsv;
    static string gWireDir = Path.Combine("..", "..", "testdata", "wire");
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

    static void Report(string bench, string path, long iters, long bytesPerOp, RunStats s)
    {
        double mbps = s.Median * bytesPerOp / (1024.0 * 1024.0);
        Console.Error.WriteLine(string.Format(
            "{0,-18} {1,-5} {2,10:F2} M msg/s {3,10:F1} MB/s   (min {4:F2}, max {5:F2}, spread {6:F1}%)",
            bench, path, s.Median / 1e6, mbps, s.Min / 1e6, s.Max / 1e6, s.Spread));
        if (gCsv)
        {
            gCsvRows.Add(string.Format(
                "cs,{0},{1},{2},{3},{4},{5:F0},{6:F0},{7:F0},{8:F2},{9:F2}",
                bench, path, iters, bytesPerOp, gNumRuns, s.Median, s.Min, s.Max, mbps, s.Spread));
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
        // (the MessageStorage discipline — see the file header)
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

        Report(name, "write", iters, bytesPerOp, Stats(writeRates));
        Report(name, "read", iters, bytesPerOp, Stats(readRates));

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

    static ShipData_Shallow PinShipShallow()
    {
        ShipData_Interpolate interp = new ShipData_Interpolate();
        interp.ShipType = ShipType.Corvette;
        interp.Position.X = 1.5;
        interp.Position.Y = -2.25;
        interp.Position.Z = 100.0;
        interp.Rotation.X = 0.0;
        interp.Rotation.Y = 0.0;
        interp.Rotation.Z = 0.0;
        interp.Rotation.W = 1.0;
        interp.LinearVelocity.X = 3.0;
        interp.LinearVelocity.Y = 0.0;
        interp.LinearVelocity.Z = -1.0;
        interp.Flags = ShipFlagsBoosting;
        interp.Team = Team.Red;
        interp.Health = 750;
        interp.Thrust = 55;
        ShipData_Shallow q = new ShipData_Shallow();
        QuantizeShip(interp, q);
        return q;
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

    static void VaryShipShallow(ShipData_Shallow m, ulong rng)
    {
        m.PositionX = (int)((rng >> 8) & 0xFFFFF) - 0x80000;
        m.PositionY = (int)((rng >> 16) & 0xFFFFF) - 0x80000;
        m.PositionZ = (int)((rng >> 24) & 0xFFFFF) - 0x80000;
        m.RotationX = (short)((int)(rng & 0x7FF) - 1024);
        m.RotationW = (short)((int)((rng >> 11) & 0x7FF) - 1024);
        m.LinearVelocityX = (int)((rng >> 32) & 0x3FFFFF) - 2097152;
        m.Flags = rng & 15;
        m.Health = (ushort)((rng >> 5) & 511);
        m.Thrust = (byte)((rng >> 14) & 63);
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

    // ------------------------------------------------------------------------------------------
    // the synthetic steady-state batch: NumBatchMessages messages through the
    // Message dispatch surface (WriteMessage/ReadMessage) plus the None
    // terminator, mixed types driven by the LCG — the C++ batch builder
    // exactly.
    // ------------------------------------------------------------------------------------------

    const int NumBatchMessages = 4096;
    const int BatchPasses = 800;

    static Message[] BuildBatch(byte[] batchBuffer, out long batchBytes)
    {
        batchBytes = 0;
        Message[] messages = new Message[NumBatchMessages];

        ulong rng = 12345;
        for (int k = 0; k < NumBatchMessages; k++)
        {
            rng = BenchRng(rng);
            int pick = (int)((rng >> 32) % 20);
            if (pick < 5) // 25% Chat
            {
                Chat m = new Chat();
                m.TextLength = 16 + (int)(rng & 15);
                for (int i = 0; i < m.TextLength; i++)
                {
                    m.Text[i] = (byte)('a' + (int)((rng >> (i & 7)) & 15));
                }
                messages[k] = m;
            }
            else if (pick < 10) // 25% Test
            {
                Test m = new Test();
                m.TestA = (ushort)rng;
                m.TestB = (short)((rng >> 16) & 511);
                m.TestC = (short)((rng >> 25) & 511);
                m.TestD = (short)((rng >> 34) & 511);
                messages[k] = m;
            }
            else if (pick < 13) // 15% Synchronize
            {
                Synchronize m = new Synchronize();
                m.SyncFrame = rng;
                m.SyncSequence = (ushort)(rng >> 8);
                messages[k] = m;
            }
            else if (pick < 16) // 15% Timescale
            {
                Timescale m = new Timescale();
                m.Scale = ((uint)rng & 0xFFFF) / 65536.0;
                m.FrameA = (uint)(rng >> 16);
                m.FrameB = (uint)(rng >> 24);
                messages[k] = m;
            }
            else if (pick < 18) // 10% Heartbeat
            {
                messages[k] = new Heartbeat();
            }
            else // 10% Block
            {
                Block m = new Block();
                m.DataLength = 64 + (int)(rng & 127);
                for (int i = 0; i < m.DataLength; i++)
                {
                    m.Data[i] = (byte)(rng >> (i & 31));
                }
                messages[k] = m;
            }
        }

        WriteStream ws = new WriteStream(batchBuffer);
        for (int k = 0; k < NumBatchMessages; k++)
        {
            if (!WriteMessage(ws, messages[k]))
            {
                Fail("message_batch", "batch write failed during setup");
                return null;
            }
        }
        if (!WriteMessage(ws, null)) // null is the None terminator
        {
            Fail("message_batch", "terminator write failed during setup");
            return null;
        }
        ws.Flush();
        batchBytes = ws.BytesProcessed;
        return messages;
    }

    static void BenchBatch()
    {
        // worst case is NumBatchMessages * MessageMaxBytes; actual is far
        // less. + 8 read slack, and the size is a multiple of 8 (write
        // contract).
        int batchBufferSize = (NumBatchMessages + 1) * (int)MessageMaxBytes + 8;
        byte[] batchBuffer = new byte[batchBufferSize];

        Message[] messages = BuildBatch(batchBuffer, out long batchBytes);
        if (messages == null)
        {
            return;
        }

        const long passes = BatchPasses;
        const long totalMsgs = passes * NumBatchMessages;

        double[] writeRates = new double[gNumRuns];
        double[] readRates = new double[gNumRuns];
        ulong rng = 999;

        // write path: whole batch per pass; one message mutates per pass so
        // the batch is never loop-invariant
        WriteStream ws = new WriteStream(batchBuffer);
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long pass = 0; pass < passes; pass++)
            {
                rng = BenchRng(rng);
                switch (messages[(rng >> 16) % NumBatchMessages])
                {
                    case Synchronize s:
                        s.SyncFrame = rng;
                        break;
                    case Test t:
                        t.TestA = (ushort)rng;
                        break;
                }
                ws.Reset(batchBuffer);
                for (int k = 0; k < NumBatchMessages; k++)
                {
                    if (!WriteMessage(ws, messages[k]))
                    {
                        Fail("message_batch", "write failed in loop");
                        return;
                    }
                }
                WriteMessage(ws, null);
                ws.Flush();
                gSink = gSink + (ulong)ws.BytesProcessed;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                writeRates[run] = totalMsgs / time;
            }
        }

        // the read buffer: rebuild once from the deterministic batch state so bytes match
        if (BuildBatch(batchBuffer, out batchBytes) == null)
        {
            return;
        }

        // read path: read messages until the None terminator, whole batch per
        // pass; reads land in pre-allocated storage — no heap per message
        MessageStorage storage = new MessageStorage();
        ReadStream rs = new ReadStream(batchBuffer, (int)batchBytes);
        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            for (long pass = 0; pass < passes; pass++)
            {
                rs.Reset(batchBuffer, (int)batchBytes);
                long count = 0;
                for (;;)
                {
                    if (!ReadMessage(rs, storage, out Message m))
                    {
                        Fail("message_batch", "read failed in loop");
                        return;
                    }
                    if (m == null)
                    {
                        break;
                    }
                    count++;
                }
                if (count != NumBatchMessages)
                {
                    Fail("message_batch", "batch message count mismatch on read");
                    return;
                }
                gSink = gSink + (ulong)count;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                readRates[run] = totalMsgs / time;
            }
        }
        GC.KeepAlive(storage);

        long bytesPerMsg = batchBytes / NumBatchMessages;
        Report("message_batch", "write", totalMsgs, bytesPerMsg, Stats(writeRates));
        Report("message_batch", "read", totalMsgs, bytesPerMsg, Stats(readRates));

        // allocation note (optimization seed, not a benchmark): bytes
        // allocated and Gen0 collections during one extra untimed pass of
        // each path
        long allocBefore = GC.GetAllocatedBytesForCurrentThread();
        int gen0Before = GC.CollectionCount(0);
        ws.Reset(batchBuffer);
        for (int k = 0; k < NumBatchMessages; k++)
        {
            WriteMessage(ws, messages[k]);
        }
        WriteMessage(ws, null);
        ws.Flush();
        long writeAlloc = GC.GetAllocatedBytesForCurrentThread() - allocBefore;
        int writeGen0 = GC.CollectionCount(0) - gen0Before;

        allocBefore = GC.GetAllocatedBytesForCurrentThread();
        gen0Before = GC.CollectionCount(0);
        rs.Reset(batchBuffer, (int)batchBytes);
        for (;;)
        {
            if (!ReadMessage(rs, storage, out Message m) || m == null)
            {
                break;
            }
        }
        long readAlloc = GC.GetAllocatedBytesForCurrentThread() - allocBefore;
        int readGen0 = GC.CollectionCount(0) - gen0Before;
        Console.Error.WriteLine(
            $"alloc note: message_batch one pass ({NumBatchMessages} msgs): write {writeAlloc} bytes ({writeGen0} gen0), read {readAlloc} bytes ({readGen0} gen0)");
    }

    // ------------------------------------------------------------------------------------------

    // the message_stream golden: dispatch wire self-check (not a benchmark)
    static void CheckMessageStreamGolden()
    {
        Chat chat = new Chat();
        SetText(chat.Text, "dispatch");
        chat.TextLength = 8;
        Test test = new Test();
        test.TestB = 42;

        WriteStream ws = new WriteStream(gBuffer);
        if (!WriteMessage(ws, chat) || !WriteMessage(ws, test))
        {
            Fail("message_stream", "dispatch write failed");
            return;
        }
        ws.Flush();
        if (!CheckGolden("message_stream", gBuffer.AsSpan(0, (int)ws.BytesProcessed)))
        {
            failed = true;
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
            else
            {
                Console.Error.WriteLine("usage: schemabench [--csv] [--round K] [--wire-dir <dir>]");
                return 1;
            }
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

        for (int k = 0; k < NumVariants; k++)
        {
            gVariants[k] = new byte[BufferSize];
        }

        Console.Error.WriteLine(
            $"schema bench (cs, {(GCSettings.IsServerGC ? "server" : "workstation")} GC)");

        CheckMessageStreamGolden();

        // rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
        RigidBody atRest = PinRigidBodyMoving();
        atRest.AtRest = true;

        BenchMessage("rigidbody_moving", "rigidbody_moving", 2000000, PinRigidBodyMoving(), WriteRigidBody, ReadRigidBody, VaryRigidBody);
        BenchMessage("rigidbody_at_rest", "rigidbody_at_rest", 4000000, atRest, WriteRigidBody, ReadRigidBody, VaryRigidBodyAtRest);
        BenchMessage("chat", "chat", 4000000, PinChat(), WriteChat, ReadChat, VaryChat);
        BenchMessage("test", null, 16000000, new Test(), WriteTest, ReadTest, VaryTest);
        BenchMessage("inputpacket", "inputpacket", 2000000, PinInputPacket(), WriteInputPacket, ReadInputPacket, VaryInputPacket);
        BenchMessage("shipcreate", "shipcreate_flags", 4000000, PinShipCreate(), WriteShipCreate, ReadShipCreate, VaryShipCreate);
        BenchMessage("ship_shallow", "ship_shallow", 4000000, PinShipShallow(), WriteShipData_Shallow, ReadShipData_Shallow, VaryShipShallow);
        BenchMessage("probe_header", "probe_header", 16000000, PinProbeHeader(), WriteProbeHeader, ReadProbeHeader, VaryProbeHeader);
        BenchMessage("probebits", "probebits", 4000000, PinProbeBits(), WriteProbeBits, ReadProbeBits, VaryProbeBits);
        BenchMessage("probearray", "probearray", 2000000, PinProbeArray(), WriteProbeArray, ReadProbeArray, VaryProbeArray);
        BenchMessage("testdata", "testdata", 1000000, PinTestData(), WriteTestData, ReadTestData, VaryTestData);

        BenchBatch();

        FlushCsv(); // rows carry the corpus_id of the goldens this run loaded

        if (failed)
        {
            Console.Error.WriteLine("BENCH FAILED");
            return 1;
        }

        Console.Error.WriteLine("OK");
        GC.KeepAlive(gSink);
        return 0;
    }
}
