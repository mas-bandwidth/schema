// THE C# TABLES SOAK (docs/SPEC-TABLES.md, "What allocates, and what never
// does"; docs/PORTING.md I9, issue #416).
//
// The per-path allocation gate (AllocGateChecks.cs) runs a ten-thousand
// iteration replay over each wire path and requires zero allocated bytes once
// warm. A soak that runs for SOAK_SECONDS of real time (20 s in `make test`,
// 3600 s at release) loops all five wire paths — Load with a caller-supplied
// TableReport, Load with no report, Measure, Save, and the Load/Measure/Save
// round trip — over the same `tabledemo.RootConfig` golden, reads
// GC.GetAllocatedBytesForCurrentThread at every sample, and FAILS if the
// allocated-bytes count moves after warmup.
//
// Managed-heap drift (GC.GetTotalMemory) and working-set drift
// (Environment.WorkingSet) are reported beside the count as the weaker
// instrument that sees retention. A short-lived allocation per iteration (a
// matched allocate-and-drop that the GC reclaims) leaves managed-heap drift
// flat while turning the per-thread allocated-bytes count red — which is why
// the count is the gate and what `tables-cs-soak-negative-control` proves.
using System;
using System.Diagnostics;
using System.Globalization;
using System.Runtime.CompilerServices;
using System.Threading;
using Demo = Tabledemo;

static partial class Program
{
    static object soakSink;

    [MethodImpl(MethodImplOptions.NoInlining)]
    static void PlantSoakShortLivedAllocation()
    {
        Volatile.Write(ref soakSink, new byte[64]);
        Volatile.Write(ref soakSink, null);
    }

    [MethodImpl(MethodImplOptions.AggressiveOptimization)]
    static bool SoakIteration(
        byte[] golden,
        Demo.RootConfig value,
        Demo.TableReport report,
        byte[] buffer,
        long expectedBytes,
        bool plant)
    {
        if (!Demo.Schema.RootConfigLoad(value, golden, report))
        {
            Console.WriteLine("SOAK FAILED: Load with caller-supplied TableReport returned false");
            return false;
        }
        if (!Demo.Schema.RootConfigLoad(value, golden, null))
        {
            Console.WriteLine("SOAK FAILED: Load with null TableReport returned false");
            return false;
        }
        long measured = Demo.Schema.RootConfigMeasure(value);
        if (measured != expectedBytes)
        {
            Console.WriteLine("SOAK FAILED: Measure returned " + measured + ", expected " + expectedBytes);
            return false;
        }
        long written = Demo.Schema.RootConfigSave(value, buffer);
        if (written != expectedBytes)
        {
            Console.WriteLine("SOAK FAILED: Save wrote " + written + " bytes, expected " + expectedBytes);
            return false;
        }
        if (!Demo.Schema.RootConfigLoad(value, golden, report))
        {
            Console.WriteLine("SOAK FAILED: round-trip Load returned false");
            return false;
        }
        long rtNeed = Demo.Schema.RootConfigMeasure(value);
        long rtWritten = Demo.Schema.RootConfigSave(value, buffer.AsSpan(0, (int)rtNeed));
        if (rtNeed != expectedBytes || rtWritten != expectedBytes)
        {
            Console.WriteLine("SOAK FAILED: round-trip Measure/Save returned " + rtNeed + "/" + rtWritten + ", expected " + expectedBytes);
            return false;
        }
        if (plant)
        {
            PlantSoakShortLivedAllocation();
        }
        return true;
    }

    static double ParseSoakSeconds(string[] args)
    {
        for (int i = 1; i < args.Length; i++)
        {
            if (args[i] == "--plant")
            {
                continue;
            }
            if (double.TryParse(args[i], NumberStyles.Float, CultureInfo.InvariantCulture, out double parsed) && parsed >= 0.0)
            {
                return parsed;
            }
        }
        string env = (Environment.GetEnvironmentVariable("SOAK_SECONDS") ?? "").Trim();
        if (double.TryParse(env, NumberStyles.Float, CultureInfo.InvariantCulture, out double fromEnv) && fromEnv >= 0.0)
        {
            return fromEnv;
        }
        return 20.0;
    }

    static bool ParseSoakPlant(string[] args)
    {
        if (args.Length > 0 && args[0] == "soak-negative-control")
        {
            return true;
        }
        for (int i = 1; i < args.Length; i++)
        {
            if (args[i] == "--plant")
            {
                return true;
            }
        }
        return Environment.GetEnvironmentVariable("SCHEMA_CS_SOAK_PLANT") == "1";
    }

    static int RunSoak(string[] args)
    {
        bool certified = AllocRuntimeGate(out bool refused);
        if (refused)
        {
            return 1;
        }

        double seconds = ParseSoakSeconds(args);
        bool plant = ParseSoakPlant(args);

        // SETUP: allocate the golden wire, target value, report, and save
        // buffer once before warmup and before the clock.
        byte[] golden = ReadGolden("root_full");
        Demo.RootConfig value = new Demo.RootConfig();
        Demo.TableReport report = new Demo.TableReport();
        byte[] buffer = new byte[1 << 16];
        if (!Demo.Schema.RootConfigLoad(value, golden, report))
        {
            Console.WriteLine("SOAK FAILED: root_full did not load in setup");
            return 1;
        }
        long need = Demo.Schema.RootConfigMeasure(value);
        if (need <= 0 || need > buffer.Length)
        {
            Console.WriteLine("SOAK FAILED: root_full measured " + need + " bytes outside setup buffer");
            return 1;
        }
        if (Demo.Schema.RootConfigSave(value, buffer.AsSpan(0, (int)need)) != need ||
            !buffer.AsSpan(0, (int)need).SequenceEqual(golden))
        {
            Console.WriteLine("SOAK FAILED: root_full does not re-save to its own bytes — refusing to soak a codec that does not reproduce the corpus");
            return 1;
        }

        // WARMUP: always unplanted so JIT compilation, tiered optimization, and
        // the cached null-report ledger settle before the count gate starts.
        const int WarmupIterations = 4000;
        for (int i = 0; i < WarmupIterations; i++)
        {
            if (!SoakIteration(golden, value, report, buffer, need, false))
            {
                return 1;
            }
        }

        GC.Collect();
        GC.WaitForPendingFinalizers();
        GC.Collect();
        long heapBefore = GC.GetTotalMemory(true);
        long workingSetBefore = Environment.WorkingSet;
        Console.WriteLine("cs tables soak: counting per-thread allocated bytes over Load, null-report Load, Measure, Save and round trip");
        Console.Out.Flush();

        long settledAllocated = GC.GetAllocatedBytesForCurrentThread();
        long prevSampleAllocated = settledAllocated;
        long startTicks = Stopwatch.GetTimestamp();
        long deadlineTicks = startTicks + (long)(seconds * Stopwatch.Frequency);
        long iterations = 0;
        long samples = 0;
        long movedSamples = 0;
        long firstMoveSample = 0;
        long firstMoveDelta = 0;

        for (;;)
        {
            if (!SoakIteration(golden, value, report, buffer, need, plant))
            {
                return 1;
            }
            iterations++;
            if ((iterations & 0xff) == 0)
            {
                samples++;
                long nowAllocated = GC.GetAllocatedBytesForCurrentThread();
                if (nowAllocated != prevSampleAllocated)
                {
                    movedSamples++;
                    if (firstMoveSample == 0)
                    {
                        firstMoveSample = samples;
                        firstMoveDelta = nowAllocated - settledAllocated;
                    }
                }
                prevSampleAllocated = nowAllocated;
                if (Stopwatch.GetTimestamp() >= deadlineTicks)
                {
                    break;
                }
            }
        }

        long afterAllocated = GC.GetAllocatedBytesForCurrentThread();
        long allocatedDelta = afterAllocated - settledAllocated;
        if (allocatedDelta != 0 && movedSamples == 0)
        {
            movedSamples = 1;
            firstMoveSample = samples;
            firstMoveDelta = allocatedDelta;
        }
        double elapsedSeconds = (double)(Stopwatch.GetTimestamp() - startTicks) / Stopwatch.Frequency;

        GC.Collect();
        GC.WaitForPendingFinalizers();
        GC.Collect();
        long heapAfter = GC.GetTotalMemory(true);
        long workingSetAfter = Environment.WorkingSet;
        long heapDrift = heapAfter - heapBefore;
        long workingSetDrift = workingSetAfter - workingSetBefore;

        // Managed-heap drift after a full blocking GC sees retained objects;
        // short-lived garbage is reclaimed and leaves heap drift flat within a
        // small runtime-bookkeeping band.
        const long ManagedHeapDriftCeiling = 64 * 1024;
        bool driftOk = heapDrift <= ManagedHeapDriftCeiling;
        string driftVerdict = driftOk ? "PASS" : "FAIL";

        Console.WriteLine(string.Format(
            CultureInfo.InvariantCulture,
            "cs tables soak: {0} iterations ({1} samples) over tabledemo.RootConfig in {2:F1} s",
            iterations,
            samples,
            elapsedSeconds));
        Console.WriteLine(string.Format(
            CultureInfo.InvariantCulture,
            "cs tables soak: allocated bytes after warmup {0}, at the end {1} (delta {2:+#;-#;0} bytes across {3} samples, moved in {4} sample(s))",
            settledAllocated,
            afterAllocated,
            allocatedDelta,
            samples,
            movedSamples));
        Console.WriteLine(string.Format(
            CultureInfo.InvariantCulture,
            "cs tables soak: managed-heap drift {0:+#;-#;0} bytes ({1} -> {2}), working-set drift {3:+#;-#;0} bytes ({4} -> {5}) — drift gate {6} (the weaker instrument that sees retention)",
            heapDrift,
            heapBefore,
            heapAfter,
            workingSetDrift,
            workingSetBefore,
            workingSetAfter,
            driftVerdict));

        if (!driftOk)
        {
            Console.WriteLine("SOAK FAILED: managed-heap drift " + heapDrift + " bytes exceeded ceiling " + ManagedHeapDriftCeiling + " bytes");
            return 1;
        }
        if (allocatedDelta != 0 || movedSamples != 0)
        {
            Console.WriteLine(string.Format(
                CultureInfo.InvariantCulture,
                "SOAK FAILED: allocated-bytes count moved by {0} bytes after warmup ({1}/{2} samples moved; first at sample {3}, +{4} bytes) — the read and write paths allocate nothing, and a short-lived allocation that the GC reclaims is invisible to drift, which is why the count is the gate",
                allocatedDelta,
                movedSamples,
                samples,
                firstMoveSample,
                firstMoveDelta));
            return 1;
        }
        if (!certified)
        {
            Console.WriteLine("cs tables soak NOT CERTIFIED: allocated-bytes count flat (0 bytes after warmup) and drift gate PASS on a runtime that is not the pin");
            return 0;
        }
        Console.WriteLine("cs tables soak passed: allocated-bytes count flat (0 bytes after warmup) and managed-heap drift flat");
        return 0;
    }
}
