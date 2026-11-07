using System;
using System.Runtime.InteropServices;
using Demo = Tabledemo;
using L = Listdemo;

// TABLES ARE FINITE ONLY (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27,
// "tables go finite-only too, same rule as packets"): every read refuses a NaN
// or an infinity on its raw bits as non_finite_float, the tolerant read
// included, and lands nothing; every writer refuses one before its first
// byte; -0 and the subnormals ride bit-exact, and elision compares bits.
static partial class Program
{
    const string PrecisionName = "ProfileConfig.precision";
    const string ValuesName = "Floats.values";

    static byte[] U64(ulong v) { return Join(U32((uint)v), U32((uint)(v >> 32))); }

    // Replaces the first 32-bit run equal to find, at any bit offset, LSB first
    // (the message form's bit order), with put.
    static bool PatchBits(byte[] bytes, uint find, uint put)
    {
        for (long at = 0; at + 32 <= (long)bytes.Length * 8; at++)
        {
            uint v = 0;
            for (int i = 0; i < 32; i++) { long b = at + i; v |= (uint)((bytes[b >> 3] >> (int)(b & 7)) & 1) << i; }
            if (v != find) { continue; }
            for (int i = 0; i < 32; i++)
            {
                long b = at + i; byte mask = (byte)(1 << (int)(b & 7));
                if (((put >> i) & 1) != 0) { bytes[b >> 3] |= mask; } else { bytes[b >> 3] &= (byte)~mask; }
            }
            return true;
        }
        return false;
    }

    static void TestFiniteFloats()
    {
        // ---- form 1, the descriptor read: a plain float64 and the float rung ----
        foreach ((byte kind, ulong raw, string what) in new (byte, ulong, string)[] {
            (10, 0x7f812345u, "a signalling NaN on the float rung"),
            (10, 0xffc00000u, "a negative quiet NaN on the float rung"),
            (10, 0x7f800000u, "+inf on the float rung"),
            (11, 0x7ff8000000000001ul, "a quiet NaN"),
            (11, 0xfff0000000000000ul, "-inf"),
        })
        {
            Demo.ProfileConfig profile = new Demo.ProfileConfig(); Demo.TableReport report = new Demo.TableReport();
            // a finite field BEFORE the float: nothing of the read may land
            byte[] wire = Fixture(Join(new byte[] { 1, 2, 7, 2, kind }, kind == 10 ? U32((uint)raw) : U64(raw), new byte[] { 0 }), "tilt", "precision");
            Check(!Demo.Schema.ProfileConfigLoad(profile, wire, report) && report.Refused &&
                report.Verdict == Demo.Schema.TableWire.Verdict.Refused && report.Reason == "non_finite_float: " + PrecisionName &&
                report.NonFiniteFloat == PrecisionName, "form 1 refuses " + what + " as non_finite_float");
            Check(profile.Tilt == 0 && BitConverter.DoubleToUInt64Bits(profile.Precision) == 0, "form 1 lands nothing of a refused read: " + what);
            // a report reused for a clean read carries no stale refusal
            Check(Demo.Schema.ProfileConfigLoad(profile, Fixture(new byte[] { 1, 2, 7, 0 }, "tilt"), report) && !report.Refused &&
                report.NonFiniteFloat == null && report.Reason == null && profile.Tilt == 7, "a reused report forgets the refusal: " + what);
            // and with no report at all
            Check(!Demo.Schema.ProfileConfigLoad(profile, wire, null), "form 1 refuses with a null report: " + what);
        }
        {
            // every finite float32 widens exactly through the ordinary conversion
            Demo.ProfileConfig profile = new Demo.ProfileConfig(); Demo.TableReport report = new Demo.TableReport();
            foreach (uint raw in new uint[] { 0xc0490fdbu, 0x00000001u, 0x80000000u, 0x7f7fffffu })
            {
                report = new Demo.TableReport();
                Check(Demo.Schema.ProfileConfigLoad(profile, Fixture(Join(new byte[] { 1, 10 }, U32(raw), new byte[] { 0 }), "precision"), report) &&
                    report.Widened == 1 && BitConverter.DoubleToUInt64Bits(profile.Precision) == BitConverter.DoubleToUInt64Bits((double)BitConverter.UInt32BitsToSingle(raw)),
                    $"the float rung widens 0x{raw:x8} exactly");
            }
        }

        // ---- -0 and the subnormals ride by their bits, under a +0 default ----
        foreach (ulong bits in new ulong[] { 0x8000000000000000ul, 0x0000000000000001ul, 0x800fffffffffffff })
        {
            Demo.ProfileConfig profile = new Demo.ProfileConfig();
            profile.Precision = BitConverter.UInt64BitsToDouble(bits);
            profile.Ratings[2] = -0.0f; profile.Ratings[3] = BitConverter.UInt32BitsToSingle(0x80000001u);
            long n = Demo.Schema.ProfileConfigMeasure(profile);
            byte[] file = new byte[Math.Max(n, 0)];
            Check(n > 0 && Demo.Schema.ProfileConfigSave(profile, file) == n, $"form 1 saves 0x{bits:x16}");
            Demo.ProfileConfig back = new Demo.ProfileConfig(); Demo.TableReport report = new Demo.TableReport();
            Check(Demo.Schema.ProfileConfigLoad(back, file, report) && !report.Refused &&
                BitConverter.DoubleToUInt64Bits(back.Precision) == bits &&
                BitConverter.SingleToUInt32Bits(back.Ratings[2]) == 0x80000000u && BitConverter.SingleToUInt32Bits(back.Ratings[3]) == 0x80000001u,
                $"form 1: 0x{bits:x16} and a -0 element ride under a +0 default, bit-exact");
            Demo.ProfileConfig[] batch = { profile };
            long m = Demo.Schema.ProfileConfigMeasureMessages(batch);
            byte[] message = new byte[Math.Max(m, 0)];
            Check(m > 0 && Demo.Schema.ProfileConfigSaveMessages(batch, message) == m, $"message saves 0x{bits:x16}");
            Demo.TableVocabulary vocabulary = new Demo.TableVocabulary();
            Demo.Schema.AnnounceRead(vocabulary, Demo.Schema.Announce(), new Demo.TableReport());
            Demo.ProfileConfig[] into = { new Demo.ProfileConfig() };
            Check(Demo.Schema.ProfileConfigLoadMessages(into, message, vocabulary, report, out int count) == Demo.Schema.TableWire.Verdict.Ok && count == 1 &&
                BitConverter.DoubleToUInt64Bits(into[0].Precision) == bits && BitConverter.SingleToUInt32Bits(into[0].Ratings[2]) == 0x80000000u,
                $"message: 0x{bits:x16} and a -0 element ride under a +0 default, bit-exact");
            byte[] fixedFile = new byte[Demo.Schema.ProfileConfigFixedMeasure(1)];
            Check(Demo.Schema.ProfileConfigFixedSave(batch, fixedFile) == fixedFile.Length, $"fixed saves 0x{bits:x16}");
            Demo.ProfileConfig[] fixedBack = { new Demo.ProfileConfig() };
            Check(Demo.Schema.ProfileConfigFixedLoad(fixedBack, fixedFile, new Demo.TableFixedEntry[1024], new Demo.TableReport()) == 1 &&
                BitConverter.DoubleToUInt64Bits(fixedBack[0].Precision) == bits && BitConverter.SingleToUInt32Bits(fixedBack[0].Ratings[2]) == 0x80000000u,
                $"fixed: 0x{bits:x16} and a -0 element round-trip bit-exact");
        }

        // ---- every writer refuses a NaN or an infinity ----
        foreach (double bad in new double[] { double.NaN, double.PositiveInfinity, double.NegativeInfinity, BitConverter.UInt64BitsToDouble(0x7ff0000000000001ul) })
        {
            Demo.ProfileConfig profile = new Demo.ProfileConfig(); profile.Precision = bad;
            byte[] buffer = new byte[4096]; Array.Fill(buffer, (byte)0xa5);
            Check(Demo.Schema.ProfileConfigMeasure(profile) == -1 && Demo.Schema.ProfileConfigSave(profile, buffer) == -1 &&
                Array.TrueForAll(buffer, b => b == 0xa5), "form 1 refuses to write a non-finite float64 and writes no byte");
            Demo.ProfileConfig[] batch = { profile };
            Check(Demo.Schema.ProfileConfigMeasureMessages(batch) == -1 && Demo.Schema.ProfileConfigSaveMessages(batch, buffer) == -1,
                "the message writer refuses a non-finite float64");
            byte[] fixedFile = new byte[Demo.Schema.ProfileConfigFixedMeasure(1)]; Array.Fill(fixedFile, (byte)0xa5);
            Check(Demo.Schema.ProfileConfigFixedSave(batch, fixedFile) == -1 && Array.TrueForAll(fixedFile, b => b == 0xa5),
                "the fixed writer refuses a non-finite float64 and writes no byte");
            Check(Demo.Schema.ProfileConfigCookMeasure(profile) == -1, "the cook refuses a non-finite float64");
            profile.Precision = 0; profile.Ratings[1] = (float)bad;
            Check(Demo.Schema.ProfileConfigMeasure(profile) == -1 && Demo.Schema.ProfileConfigFixedSave(batch, fixedFile) == -1 &&
                Demo.Schema.ProfileConfigMeasureMessages(batch) == -1 && Demo.Schema.ProfileConfigCookMeasure(profile) == -1,
                "every writer refuses a non-finite float32 array element");
        }

        // ---- the fixed read refuses on storage, before the bounds pass ----
        {
            Demo.ProfileConfig profile = new Demo.ProfileConfig(); profile.Tilt = 7; profile.Precision = 1234.5678;
            Demo.ProfileConfig[] batch = { new Demo.ProfileConfig(), profile };
            byte[] file = new byte[Demo.Schema.ProfileConfigFixedMeasure(2)];
            Check(Demo.Schema.ProfileConfigFixedSave(batch, file) == file.Length, "fixed saves a finite batch");
            byte[] want = BitConverter.GetBytes(1234.5678);
            int at = file.AsSpan().IndexOf(want);
            Check(at > 0, "the fixed record holds the float64's bytes");
            if (at > 0)
            {
                BitConverter.GetBytes(0x7ff4000000000000ul).CopyTo(file, at);
                Demo.ProfileConfig[] back = { new Demo.ProfileConfig(), new Demo.ProfileConfig() };
                Demo.TableReport report = new Demo.TableReport();
                Check(Demo.Schema.ProfileConfigFixedLoad(back, file, new Demo.TableFixedEntry[1024], report) == -1 && report.Refused &&
                    report.Reason == "non_finite_float: " + PrecisionName && report.NonFiniteFloat == PrecisionName,
                    "the fixed read refuses a signalling NaN as non_finite_float");
                Check(back[1].Tilt == 0 && BitConverter.DoubleToUInt64Bits(back[1].Precision) == 0, "the refused fixed batch lands nothing");
            }
        }

        // ---- the variable class: form 1 and the region ----
        {
            const uint pi = 0x40490fdbu;
            L.Floats floats = new L.Floats { Values = new float[] { 1.5f, BitConverter.UInt32BitsToSingle(pi) }, ValuesCount = 2, Past = 3 };
            byte[] file = new byte[L.Schema.FloatsMeasure(floats)];
            Check(L.Schema.FloatsSave(floats, file) == file.Length, "Floats saves");
            foreach (uint bad in new uint[] { 0x7fc00001u, 0xff800000u })
            {
                byte[] wire = (byte[])file.Clone();
                Check(PatchBits(wire, pi, bad), "the file holds the element's bits");
                L.Floats back = new L.Floats(); L.TableReport report = new L.TableReport();
                Check(!L.Schema.FloatsLoad(back, wire, report) && report.Refused && report.Reason == "non_finite_float: " + ValuesName &&
                    back.ValuesCount == 0 && back.Past == 0, $"form 1 refuses list element 0x{bad:x8} and lands nothing");
                RegionRefuses(wire, $"the region read refuses list element 0x{bad:x8}");
            }
            floats.Values[0] = float.NaN;
            Check(L.Schema.FloatsMeasure(floats) == -1 && L.Schema.FloatsCookMeasure(floats) == -1,
                "the variable writers refuse a NaN list element");
        }
    }

    static unsafe void RegionRefuses(byte[] wire, string what)
    {
        long need = L.Schema.FloatsLoadMeasure(wire, out long dataBytes, out long attributionBytes, out _);
        if (need < 0) { Check(false, what + ": the region measure refused the framing"); return; }
        byte* data = (byte*)NativeMemory.AlignedAlloc(checked((nuint)(need + 64)), 64);
        try
        {
            L.TableReport report = new L.TableReport();
            Check(L.Schema.FloatsLoad((IntPtr)data, need, wire, report) == null && report.Refused &&
                report.Reason == "non_finite_float: " + ValuesName, what);
        }
        finally { NativeMemory.AlignedFree(data); }
    }
}
