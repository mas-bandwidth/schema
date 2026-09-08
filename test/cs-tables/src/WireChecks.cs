using System;
using System.IO;
using V1 = Tblv1;
using Demo = Tabledemo;

static partial class Program
{
    static void TestWireContracts()
    {
        Csids.WideIds wide = new Csids.WideIds();
        string[] names = new string[141]; names[0] = "words";
        using MemoryStream elements = new MemoryStream();
        for (int i = 0; i < 140; i++)
        {
            wide.Words[i] = (Csids.Word)(i + 1);
            names[i + 1] = "V" + (i + 1).ToString("D3");
            elements.Write(Var((ulong)i + 2));
        }
        byte[] payload = Join(new byte[] { 30 }, Var(140), elements.ToArray());
        byte[] expected = Fixture(Join(new byte[] { 1,14 }, Var((ulong)payload.Length), payload, new byte[] { 0 }), names);
        Check(Csids.Schema.WideIdsMeasure(wide) == expected.Length, "reference boundary: exact measure");
        byte[] actual = new byte[expected.Length];
        Check(Csids.Schema.WideIdsSave(wide, actual) == expected.Length && actual.AsSpan().SequenceEqual(expected),
            "reference boundary: independent first-use bytes, including 128");
        Check(Csids.Schema.WideIdsSave(wide, new byte[actual.Length - 1]) == -1, "reference boundary: short output refused");
        // Caller vocabulary capacity must not change first-use wire order.
        // Exercise exact capacity and the bounded-stack fallback against the
        // independent fixture, including references crossing 127/128.
        foreach (int capacity in new int[] { names.Length, 1024, 1025 })
        {
            Array.Fill(actual, (byte)0xa5);
            Check(Csids.Schema.TableWire.Save(wide, Csids.Schema.WideIdsTableType(), actual, new ulong[capacity], false) == expected.Length &&
                actual.AsSpan().SequenceEqual(expected), "reference storage capacity preserves canonical bytes: " + capacity);
        }
        Array.Fill(actual, (byte)0xa5);
        Check(Csids.Schema.TableWire.Save(wide, Csids.Schema.WideIdsTableType(), actual, new ulong[names.Length - 1], false) == -1 &&
            Array.TrueForAll(actual, b => b == 0xa5), "reference capacity refusal leaves output untouched");
        Csids.WideIds decoded = new Csids.WideIds(); Csids.TableReport wideReport = new Csids.TableReport();
        Check(Csids.Schema.WideIdsLoad(decoded, expected, wideReport) && decoded.Words[139] == (Csids.Word)140,
            "reference boundary: decode independent bytes");

        V1.Cfg cfg = new V1.Cfg(); V1.TableReport report = new V1.TableReport();
        Check(V1.Schema.CfgLoad(cfg, Fixture(new byte[] { 1,3,42,0,0 }, "a"), report) &&
            cfg.A == 42 && report.Widened == 1 && report.KindMismatch == 0, "signed widening");
        Demo.ProfileConfig profile = new Demo.ProfileConfig(); Demo.TableReport floatReport = new Demo.TableReport();
        uint raw = 0x7f812345u; // signalling NaN, whose payload must not be quieted
        Check(Demo.Schema.ProfileConfigLoad(profile, Fixture(Join(new byte[] { 1,10 }, U32(raw), new byte[] { 0 }), "precision"), floatReport),
            "float widening: load");
        Check(Demo.Schema.TableDoubleToBits(profile.Precision) == (0x7ff0000000000000ul | ((ulong)(raw & 0x7fffff) << 29)) &&
            floatReport.Widened == 1, "float widening: sign and signalling payload preserved");

        // Ref zero terminates a body; identity zero in the vocabulary is just
        // an unknown name. Escape, no-payload and enum values remain skippable.
        byte[] zeroId = Fixture(new byte[] { 1,31,2,99,100,2,32,0,3,30,127,4,4,42,0,0,0,0 },
            "zero", "future-void", "future-enum", "a");
        zeroId.AsSpan(zeroId.Length - 40, 8).Clear();
        report = new V1.TableReport();
        Check(V1.Schema.CfgLoad(cfg, zeroId, report) && cfg.A == 42 && report.Unknown == 3 && !report.Malformed,
            "zero identity and unknown future kinds skip without swallowing the next field");

        // C++ reads a fixed array's count at the enclosing cursor before
        // bounding elements by L. The next field reference completes count 128;
        // its bool kind mismatches the same array field. No element may be
        // fabricated from that following field.
        report = new V1.TableReport();
        byte[] headerSpill = Fixture(new byte[] { 1,14,2,4,128,1,1,1,0 }, "items");
        Check(V1.Schema.CfgLoadVerdict(cfg, headerSpill, report) == V1.Schema.TableWire.Verdict.Ok &&
            report.Malformed && report.Clamped == 1 && report.KindMismatch == 1 && cfg.ItemsCount == 0,
            "array header spill matches reference events while elements stay bounded");

        // The report and all typed storage belong to the caller. Warm metadata
        // and the JIT, then measure only repeated loads and saves.
        for (int i = 0; i < 1000; i++) { Csids.Schema.WideIdsLoad(decoded, expected, wideReport); Csids.Schema.WideIdsSave(wide, actual); }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 1000; i++) { Csids.Schema.WideIdsLoad(decoded, expected, wideReport); Csids.Schema.WideIdsSave(wide, actual); }
        long allocated = GC.GetAllocatedBytesForCurrentThread() - before;
        Check(allocated == 0, "wire load/save allocate zero bytes after warmup: " + allocated);
    }
}
