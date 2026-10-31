using System;
using System.Buffers.Binary;
using System.Runtime.InteropServices;

using Old = Vold_float_widen;
using New = Vnew_float_widen;
using NewEntry = Vnew_float_widen.TableFixedEntry;

// cell-cs-r21: widenf is the ordinary conversion under the finite rule - every
// finite f32 widens exactly, and a NaN or an infinity is refused
//
// The law (docs/SPEC-TABLES.md section 3.3, Glenn 2026-10-27, "tables go
// finite-only too, same rule as packets"; docs/FIXED-FORM-ALGORITHM.md section
// 4.5, the scatter table's widen/widenf row): widenf is the f32 at src as an
// f64 at dst by the ordinary conversion, exact for every finite f32, -0 and the
// subnormals included. A NaN or an infinity is refused by every writer, and the
// fixed read's finite pass (<T>FixedFiniteBody, run over storage after the plan)
// refuses one as non_finite_float.
//
// Vector: the writer is the generated VOLD_float_widen FixedSave (lead uint32,
// v float32, trail uint32; body 12 bytes, v at body 4). The reader is
// VNEW_float_widen (v float64) running the plan a lineage build compiles for
// this pair - copy, widenf, copy - through its own TableFixedWire.Run, the R29
// pattern (a bare pair of generated units has no lineage, so the plan is
// spelled here, one entry per field, slot indices the reader's). A non-finite
// vector is refused by the writer, and a body carrying one by hand is widened by
// the run into a value the reader's finite pass names.

class Program
{
    static int Main()
    {
        bool ok = true;
        ok &= Widen(0x3F800000u, "1.0f");
        ok &= Widen(0x80000000u, "-0");
        ok &= Widen(0x00000001u, "the smallest subnormal");
        ok &= Widen(0x807FFFFFu, "the largest negative subnormal");
        ok &= Widen(0x7F7FFFFFu, "the largest finite float32");
        ok &= Refused(0x7F800001u, "signalling NaN, payload 1");
        ok &= Refused(0xFFC00123u, "negative quiet NaN, payload 0x123");
        ok &= Refused(0x7F800000u, "+inf");
        ok &= Refused(0xFF800000u, "-inf");
        if (!ok)
        {
            return 1;
        }
        Console.WriteLine("widenf is the ordinary conversion, and a non-finite f32 is refused");
        return 0;
    }

    static byte[] Body(uint bits)
    {
        byte[] body = new byte[(int)Old.Schema.FloatWidenFixedBodyBytes];
        BinaryPrimitives.WriteUInt32LittleEndian(body, 0xAAAAAAAAu);
        BinaryPrimitives.WriteUInt32LittleEndian(body.AsSpan(4), bits);
        BinaryPrimitives.WriteUInt32LittleEndian(body.AsSpan(8), 0xBBBBBBBBu);
        return body;
    }

    static bool Refused(uint bits, string what)
    {
        bool ok = true;
        var w = new Old.FloatWiden();
        Old.Schema.TableReset(w);
        w.V = BitConverter.UInt32BitsToSingle(bits);
        byte[] file = new byte[Old.Schema.FloatWidenFixedMeasure(1)];
        Array.Fill(file, (byte)0xA5);
        if (Old.Schema.FloatWidenFixedSave(w, file) != -1 || !Array.TrueForAll(file, b => b == 0xA5))
        {
            Console.WriteLine("FAIL: " + what + ": the writer did not refuse the non-finite f32");
            ok = false;
        }
        var back = Run(Body(bits), out New.TableReport r);
        string why = New.Schema.FloatWidenFixedFiniteBody(back);
        if (why != "FloatWiden.v")
        {
            Console.WriteLine($"FAIL: {what}: the reader's finite pass answered {why ?? "null"} for f32 0x{bits:X8}, want FloatWiden.v");
            ok = false;
        }
        else
        {
            Console.WriteLine($"OK: {what}: f32 0x{bits:X8} is refused as non_finite_float: {why}");
        }
        return ok;
    }

    static New.FloatWiden Run(byte[] body, out New.TableReport r)
    {
        NewEntry[] plan = new NewEntry[] {
            new NewEntry(0u, 0u, 4u, 0u, New.Schema.TableFixedWire.NoGuard, New.Schema.TableFixedWire.Copy,   (byte)0, (byte)4),
            new NewEntry(4u, 1u, 4u, 0u, New.Schema.TableFixedWire.NoGuard, New.Schema.TableFixedWire.WidenF, (byte)0, (byte)8),
            new NewEntry(8u, 2u, 4u, 0u, New.Schema.TableFixedWire.NoGuard, New.Schema.TableFixedWire.Copy,   (byte)0, (byte)4),
        };
        var back = new New.FloatWiden();
        r = new New.TableReport();
        byte[] scratch = Array.Empty<byte>();
        int forgedOrdinal = 0;
        New.Schema.TableFixedWire.Run(plan, New.Schema.FloatWidenFixedSlots, body, back, r,
            MemoryMarshal.AsBytes<NewEntry>(plan), ref scratch, ref forgedOrdinal);
        return back;
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
        byte[] body = file.AsSpan(at, (int)Old.Schema.FloatWidenFixedBodyBytes).ToArray();
        if (BinaryPrimitives.ReadUInt32LittleEndian(body.AsSpan(4)) != bits)
        {
            // The writer itself must carry the f32 bits as given, or nothing below means anything.
            Console.WriteLine("FAIL: " + what + $": the writer stored 0x{BinaryPrimitives.ReadUInt32LittleEndian(body.AsSpan(4)):X8}, not 0x{bits:X8}");
            return false;
        }

        var back = Run(body, out New.TableReport r);
        ulong got = BitConverter.DoubleToUInt64Bits(back.V);
        ulong want = BitConverter.DoubleToUInt64Bits((double)BitConverter.UInt32BitsToSingle(bits));
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
        if (New.Schema.FloatWidenFixedFiniteBody(back) != null)
        {
            Console.WriteLine($"FAIL: {what}: the reader's finite pass refused a finite value");
            ok = false;
        }
        return ok;
    }
}
