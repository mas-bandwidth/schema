// THE C# PER-PATH ALLOCATION GATE (docs/SPEC-TABLES.md, "What allocates, and
// what never does"; docs/PORTING.md I1). C# has a garbage collector, so a grep
// over the emitted source proves nothing and the "read path allocates nothing"
// claim has to be MEASURED — with an instrument the generated code cannot
// influence: the runtime's own per-thread allocation counter,
// GC.GetAllocatedBytesForCurrentThread.
//
// SETUP is everything the gate licenses once and the caller owns: the golden
// bytes, the value, the save buffer and the caller-supplied TableReport.
// STEADY WORK is the replay loop over that same storage. A path that allocates
// only on its first call (JIT, a cached descriptor, a lazily built table) is
// therefore not charged to the steady region, which is the claim: the read
// path owns no memory once it is warm.
//
// Its own sensitivity check is not optional: a counter that reports zero for a
// planted allocation is a gate watching nothing (test/go-tables/alloc_test.go
// says the same and makes the same demand). TestAllocationGateCanGoRed plants
// two escapes and requires the installed rows to move — and LOCALIZES: a plant
// on the read row leaves the save row green.
using System;
using System.Runtime.InteropServices;
using Demo = Tabledemo;

static partial class Program
{
    // THE RUNTIME PIN (docs/PORTING.md I14). make passes the pin in as
    // SCHEMA_CS_ALLOC_PIN, read from .github/dotnet-version, so C# holds no
    // second copy. One major's JIT inlines a generated body that another leaves
    // over its threshold, where a double store boxes: a floor measured on
    // whatever dotnet a PATH lookup found says nothing about the runtime the
    // claim is for. Returns true when the runtime is the pin (the gate may
    // certify). On any other runtime it REFUSES (refused = true, measures
    // nothing) unless SCHEMA_CS_ALLOC_ANY_RUNTIME=1, which measures and reports
    // NOT CERTIFIED and returns false.
    static bool AllocRuntimeGate(out bool refused)
    {
        refused = false;
        string pin = (Environment.GetEnvironmentVariable("SCHEMA_CS_ALLOC_PIN") ?? "").Trim();
        string anyRuntime = Environment.GetEnvironmentVariable("SCHEMA_CS_ALLOC_ANY_RUNTIME");
        int major = Environment.Version.Major;
        string running = RuntimeInformation.FrameworkDescription + " (major " + major + ")";
        int dot = pin.IndexOf('.');
        bool pinned = int.TryParse(dot < 0 ? pin : pin.Substring(0, dot), out int pinMajor);
        if (pinned && pinMajor == major)
        {
            Console.WriteLine("cs alloc gate: runtime " + running + " is the pin " + pin);
            return true;
        }
        string have = pinned ? "pin " + pin : "usable pin (SCHEMA_CS_ALLOC_PIN='" + pin + "')";
        if (anyRuntime == "1")
        {
            Console.WriteLine("cs alloc gate NOT CERTIFIED: runtime " + running + " is not the " + have + "; SCHEMA_CS_ALLOC_ANY_RUNTIME=1 measures anyway and certifies nothing");
            return false;
        }
        Console.WriteLine("cs alloc gate: REFUSED to measure: runtime " + running + " is not the " + have + "; the floor is certified on the pinned runtime only (SCHEMA_CS_ALLOC_ANY_RUNTIME=1 observes without certifying)");
        refused = true;
        failed = true;
        return false;
    }

    // where a control's allocation escapes to: nothing else reads it, so the
    // compiler cannot prove the value does not reach it and elide the work.
    static object allocSink;

    // Load, with the caller's report supplied. Reused value and report: only
    // the read itself is charged.
    static long AllocLoad(byte[] golden, Demo.RootConfig value, Demo.TableReport report, bool plant)
    {
        for (int i = 0; i < 2000; i++) { Demo.Schema.RootConfigLoad(value, golden, report); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 10000; i++)
        {
            Demo.Schema.RootConfigLoad(value, golden, report);
            if (plant) { allocSink = new byte[64]; }
        }
        return GC.GetAllocatedBytesForCurrentThread() - before;
    }

    // The NULL-report read: the runtime must not construct a TableReport on
    // the caller's behalf (it caches one ignored ledger instead).
    static long AllocNullReportLoad(byte[] golden, Demo.RootConfig value, bool plant)
    {
        for (int i = 0; i < 2000; i++) { Demo.Schema.RootConfigLoad(value, golden, null); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 10000; i++)
        {
            Demo.Schema.RootConfigLoad(value, golden, null);
            if (plant) { allocSink = new byte[64]; }
        }
        return GC.GetAllocatedBytesForCurrentThread() - before;
    }

    static long AllocMeasure(Demo.RootConfig value, bool plant)
    {
        for (int i = 0; i < 2000; i++) { Demo.Schema.RootConfigMeasure(value); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 10000; i++)
        {
            Demo.Schema.RootConfigMeasure(value);
            if (plant) { allocSink = new byte[64]; }
        }
        return GC.GetAllocatedBytesForCurrentThread() - before;
    }

    static long AllocSave(Demo.RootConfig value, byte[] buffer, bool plant)
    {
        for (int i = 0; i < 2000; i++) { Demo.Schema.RootConfigSave(value, buffer); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 10000; i++)
        {
            Demo.Schema.RootConfigSave(value, buffer);
            if (plant) { allocSink = new byte[64]; }
        }
        return GC.GetAllocatedBytesForCurrentThread() - before;
    }

    static long AllocRoundTrip(byte[] golden, Demo.RootConfig value, Demo.TableReport report, byte[] buffer, bool plant)
    {
        void Body()
        {
            Demo.Schema.RootConfigLoad(value, golden, report);
            long need = Demo.Schema.RootConfigMeasure(value);
            Demo.Schema.RootConfigSave(value, buffer.AsSpan(0, (int)need));
        }
        for (int i = 0; i < 2000; i++) { Body(); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 10000; i++)
        {
            Body();
            if (plant) { allocSink = new byte[64]; }
        }
        return GC.GetAllocatedBytesForCurrentThread() - before;
    }

    static void TestAllocationGate()
    {
        // SETUP, named as such: every allocation the gate licenses, made once.
        byte[] golden = ReadGolden("root_full");
        Demo.RootConfig value = new Demo.RootConfig();
        Demo.TableReport report = new Demo.TableReport();
        byte[] buffer = new byte[1 << 16];
        Check(Demo.Schema.RootConfigLoad(value, golden, report), "alloc gate: the golden loads in setup");
        long need = Demo.Schema.RootConfigMeasure(value);
        Check(need > 0 && need <= buffer.Length, "alloc gate: the golden measures inside the setup buffer");

        // STEADY WORK: exact zero byte per path once warm.
        Check(AllocLoad(golden, value, report, false) == 0,
            "alloc gate: wire Load allocates nothing with caller-supplied reporting storage");
        Check(AllocNullReportLoad(golden, value, false) == 0,
            "alloc gate: wire Load allocates nothing with no report supplied");
        Check(AllocMeasure(value, false) == 0, "alloc gate: wire Measure allocates nothing");
        Check(AllocSave(value, buffer, false) == 0, "alloc gate: wire Save allocates nothing");
        Check(AllocRoundTrip(golden, value, report, buffer, false) == 0,
            "alloc gate: the load/measure/save round trip allocates nothing");
    }

    static void TestAllocationGateCanGoRed()
    {
        byte[] golden = ReadGolden("root_full");
        Demo.RootConfig value = new Demo.RootConfig();
        Demo.TableReport report = new Demo.TableReport();
        byte[] buffer = new byte[1 << 16];
        Demo.Schema.RootConfigLoad(value, golden, report);

        // The instrument must SEE an allocation that escapes.
        allocSink = null;
        Check(AllocLoad(golden, value, report, true) > 0,
            "alloc gate control: an escaping allocation on the read row measured as zero — the gate is watching nothing");
        Check(allocSink != null, "alloc gate control: the planted allocation escaped to the sink");

        // LOCALIZATION: a plant on the read row moves the read row and leaves
        // the save row alone; a plant on the save row does the reverse.
        Check(AllocSave(value, buffer, false) == 0,
            "alloc gate control: the save row stayed green beside the planted read row");
        Check(AllocSave(value, buffer, true) > 0,
            "alloc gate control: an escaping allocation on the save row measured as zero");
        Check(AllocLoad(golden, value, report, false) == 0,
            "alloc gate control: the read row stayed green beside the planted save row");
    }
}
