using System;
using System.Buffers.Binary;
using System.IO;
using V1 = Tblv1;

static partial class Program
{
    static void TestLebWireContracts()
    {
        V1.Cfg target = new V1.Cfg();

        // 1. One-byte LEB field references and scalar values:
        // Canonical 1-byte LEB reference (1) with int32 value 42.
        byte[] oneByteWire = Fixture(new byte[] { 1, 4, 42, 0, 0, 0, 0 }, "a");
        DirtyResetTarget(target);
        V1.TableReport report = new V1.TableReport();
        var verdict = V1.Schema.CfgLoadVerdict(target, oneByteWire, report);
        Check(verdict == V1.Schema.TableWire.Verdict.Ok && !report.Malformed && target.A == 42,
            "leb fastpath: 1-byte reference and scalar value");

        // 2. Multi-byte LEB field reference (reference 128 = 0x80, 0x01):
        // Construct a vocabulary with 128 names, where name 128 is "a".
        string[] names128 = new string[128];
        for (int i = 0; i < 127; i++) { names128[i] = "unused" + i; }
        names128[127] = "a";
        byte[] multiByteRefWire = Fixture(Join(new byte[] { 0x80, 0x01, 4 }, U32(100), new byte[] { 0 }), names128);
        DirtyResetTarget(target);
        report = new V1.TableReport();
        verdict = V1.Schema.CfgLoadVerdict(target, multiByteRefWire, report);
        Check(verdict == V1.Schema.TableWire.Verdict.Ok && !report.Malformed && target.A == 100,
            "leb multi-byte fallback: reference 128");

        // 3. Multi-byte LEB array length and element counts:
        // Csids.WideIds has 140 elements, which crosses the 127/128 boundary on both write and read.
        Csids.WideIds wide = new Csids.WideIds();
        string[] wideNames = new string[141]; wideNames[0] = "words";
        using (MemoryStream elements = new MemoryStream())
        {
            for (int i = 0; i < 140; i++)
            {
                wide.Words[i] = (Csids.Word)(i + 1);
                wideNames[i + 1] = "V" + (i + 1).ToString("D3");
                elements.Write(Var((ulong)i + 2));
            }
            byte[] payload = Join(new byte[] { 30 }, Var(140), elements.ToArray());
            byte[] expected = Fixture(Join(new byte[] { 1, 14 }, Var((ulong)payload.Length), payload, new byte[] { 0 }), wideNames);
            byte[] actual = new byte[expected.Length];
            Check(Csids.Schema.WideIdsSave(wide, actual) == expected.Length && actual.AsSpan().SequenceEqual(expected),
                "leb save: 140 elements crossing 127/128 boundary");
            Csids.WideIds decoded = new Csids.WideIds();
            Csids.TableReport wideReport = new Csids.TableReport();
            Check(Csids.Schema.WideIdsLoad(decoded, expected, wideReport) && decoded.Words[139] == (Csids.Word)140,
                "leb load: 140 elements crossing 127/128 boundary");
        }

        // 4. Non-canonical multi-byte zero rejection:
        // [0x80, 0x00] is a 2-byte encoding of zero (redundant continuation bit).
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(target, Fixture(new byte[] { 0x80, 0x00 }), report) &&
            report.Malformed, "leb rejection: redundant 2-byte zero [0x80, 0x00]");

        // [0x80, 0x80, 0x00] is a 3-byte encoding of zero.
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(target, Fixture(new byte[] { 0x80, 0x80, 0x00 }), report) &&
            report.Malformed, "leb rejection: redundant 3-byte zero [0x80, 0x80, 0x00]");

        // 5. Truncated LEB rejection:
        // [0x80] at EOF has continuation set with no following byte.
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(target, Fixture(new byte[] { 0x80 }), report) &&
            report.Malformed, "leb rejection: truncated LEB [0x80] at EOF");

        // 6. 10th-byte overflow rejection:
        // 10 bytes with b > 1 on the 10th byte (overflows uint64):
        byte[] overflowLeb = new byte[] { 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02 };
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(target, Fixture(overflowLeb), report) &&
            report.Malformed, "leb rejection: 10th byte overflow");

        // 7. Allocation check across 1-byte fastpath and multi-byte paths:
        for (int i = 0; i < 1000; i++)
        {
            V1.Schema.CfgLoadVerdict(target, oneByteWire, report);
            V1.Schema.CfgLoadVerdict(target, multiByteRefWire, report);
        }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 5000; i++)
        {
            V1.Schema.CfgLoadVerdict(target, oneByteWire, report);
            V1.Schema.CfgLoadVerdict(target, multiByteRefWire, report);
        }
        long allocated = GC.GetAllocatedBytesForCurrentThread() - before;
        Check(allocated == 0, "leb load paths allocate zero bytes after warmup: " + allocated);
    }
}
