using System;
using System.Buffers.Binary;
using System.Runtime.InteropServices;

using Old = Vold_float_widen;
using New = Vnew_float_widen;
using NewEntry = Vnew_float_widen.TableFixedEntry;

// cell-cs-r21: widenf is the bit-exact widening - signalling NaNs kept, the
// quiet bit carried as the writer wrote it
//
// The law (docs/FIXED-FORM-ALGORITHM.md section 4.5, the scatter table's
// widen/widenf row): "widenf is the f32 at src as an f64 at dst ... exact by
// construction, NaN payloads included". The exact f64 image of an f32 is the
// sign, the exponent re-biased (all ones stays all ones) and the 23 mantissa
// bits moved up 29 places: the quiet bit (mantissa bit 22) lands at bit 51 AS
// WRITTEN, so a signalling NaN stays signalling.
//
// Vector: the writer is the generated VOLD_float_widen FixedSave (lead uint32,
// v float32, trail uint32; body 12 bytes, v at body 4). The reader is
// VNEW_float_widen (v float64) running the plan a lineage build compiles for
// this pair - copy, widenf, copy - through its own TableFixedWire.Run, the R29
// pattern (a bare pair of generated units has no lineage, so the plan is
// spelled here, one entry per field, slot indices the reader's).

class Program
{
    static int Main()
    {
        bool ok = true;
        ok &= Widen(0x7F800001u, "signalling NaN, payload 1 (quiet bit clear)");
        ok &= Widen(0x7FA00000u, "signalling NaN, high payload bit (quiet bit clear)");
        ok &= Widen(0xFF800123u, "negative signalling NaN, payload 0x123");
        ok &= Widen(0x7FC00000u, "the canonical quiet NaN");
        ok &= Widen(0xFFC00123u, "negative quiet NaN, payload 0x123");
        ok &= Widen(0x3F800000u, "1.0f (control: an ordinary value)");
        if (!ok)
        {
            return 1;
        }
        Console.WriteLine("widenf is the bit-exact widening");
        return 0;
    }

    // The exact f64 image of an f32 NaN or finite normal (the vectors are those).
    static ulong Exact(uint f)
    {
        ulong sign = (ulong)(f >> 31) << 63;
        uint exp = (f >> 23) & 0xFF;
        ulong mant = (ulong)(f & 0x7FFFFF) << 29;
        ulong e64 = exp == 0xFF ? 0x7FFul : (ulong)(exp - 127 + 1023);
        return sign | (e64 << 52) | mant;
    }

    static bool Widen(uint bits, string what)
    {
        var w = new Old.FloatWiden();
        Old.Schema.TableReset(w);
        w.Lead = 0xAAAAAAAAu;
        w.V = BitConverter.UInt32BitsToSingle(bits);
        w.Trail = 0xBBBBBBBBu;
        byte[] file = new byte[Old.Schema.FloatWidenFixedMeasure(1)];
        if (Old.Schema.FloatWidenFixedSave(w, file) != file.Length)
        {
            Console.WriteLine("FAIL: " + what + ": the writer did not fill its buffer");
            return false;
        }
        int at = Old.Schema.TableFixedWire.HeaderBytes + 4 + (int)Old.Schema.FloatWidenFixedLayoutBytes + 8;
        ReadOnlySpan<byte> body = file.AsSpan(at, (int)Old.Schema.FloatWidenFixedBodyBytes);
        if (BinaryPrimitives.ReadUInt32LittleEndian(body.Slice(4)) != bits)
        {
            // The writer itself must carry the f32 bits as given, or nothing below means anything.
            Console.WriteLine("FAIL: " + what + $": the writer stored 0x{BinaryPrimitives.ReadUInt32LittleEndian(body.Slice(4)):X8}, not 0x{bits:X8}");
            return false;
        }

        NewEntry[] plan = new NewEntry[] {
            new NewEntry(0u, 0u, 4u, 0u, New.Schema.TableFixedWire.NoGuard, New.Schema.TableFixedWire.Copy,   (byte)0, (byte)4),
            new NewEntry(4u, 1u, 4u, 0u, New.Schema.TableFixedWire.NoGuard, New.Schema.TableFixedWire.WidenF, (byte)0, (byte)8),
            new NewEntry(8u, 2u, 4u, 0u, New.Schema.TableFixedWire.NoGuard, New.Schema.TableFixedWire.Copy,   (byte)0, (byte)4),
        };
        var back = new New.FloatWiden();
        var r = new New.TableReport();
        byte[] scratch = Array.Empty<byte>();
        New.Schema.TableFixedWire.Run(plan, New.Schema.FloatWidenFixedSlots, body, back, r,
            MemoryMarshal.AsBytes<NewEntry>(plan), ref scratch);

        ulong got = BitConverter.DoubleToUInt64Bits(back.V);
        ulong want = Exact(bits);
        bool ok = true;
        if (got != want)
        {
            Console.WriteLine($"FAIL: {what}: f32 0x{bits:X8} widened to 0x{got:X16}, want 0x{want:X16}");
            ok = false;
        }
        else
        {
            Console.WriteLine($"OK: {what}: f32 0x{bits:X8} widens to 0x{got:X16}");
        }
        if (r.Widened != 1)
        {
            Console.WriteLine($"FAIL: {what}: widened={r.Widened}, want 1");
            ok = false;
        }
        if (back.Lead != 0xAAAAAAAAu || back.Trail != 0xBBBBBBBBu)
        {
            Console.WriteLine($"FAIL: {what}: lead/trail moved");
            ok = false;
        }
        return ok;
    }
}
