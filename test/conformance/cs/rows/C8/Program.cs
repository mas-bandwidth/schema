using System;
using System.Buffers.Binary;

using Bits = Vnew_bits_grow;
using Fx = Vnew_fixed_i_grow;

// cell-cs-c8: fixed-point F-shift / bits(N)
//
// The law (docs/FIXED-FORM-ALGORITHM.md section 4.6, the bounds pass): "A
// fixed-point field's bounds are in VALUE UNITS and its storage is raw, so both
// ends are shifted by F first; a bits(N) clamps to 2^N - 1." The pass runs over
// STORAGE after the plan run, so the identity plan is held to it exactly as a
// compiled one is: this row needs one generation, no lineage.
//
// Vectors: the writer's own FixedSave of one record, then the body's raw field
// planted past the bound, then the generated FixedLoad.
//   - test/tables/VNEW_bits_grow.schema: v bits(12) at body 4, a uint32 word.
//     Raw 0xFFFF lands 4095 (2^12 - 1) and clamped counts once; raw 4095 lands
//     untouched.
//   - test/tables/VNEW_fixed_I_grow.schema: v fixed(12, 4) | min = -8, max = 7
//     at body 4, an int16 word. In raw units the bounds are -8 * 16 = -128 and
//     7 * 16 = 112. Raw 0x7FFF lands 112, raw -2000 lands -128, each counting
//     once; raw 112 (7.0 exactly) lands untouched. An UNSHIFTED clamp (bounds
//     taken as raw -8 and 7) would land 7 for the in-range 112: that is the
//     control this row exists for.
// The lead and trail words either side prove the planted bytes moved nothing
// else.

class Program
{
    static int Main()
    {
        bool ok = true;
        ok &= BitsClamp(0xFFFFu, 4095u, 1, "bits(12): raw 0xFFFF clamps to 2^12 - 1");
        ok &= BitsClamp(4095u, 4095u, 0, "bits(12): raw 4095 is the top and lands as written");
        ok &= FixedClamp(0x7FFF, 112, 1, "fixed(12,4): raw 0x7FFF clamps to max 7 shifted by F (raw 112)");
        ok &= FixedClamp(-2000, -128, 1, "fixed(12,4): raw -2000 clamps to min -8 shifted by F (raw -128)");
        ok &= FixedClamp(112, 112, 0, "fixed(12,4): raw 112 is 7.0 exactly and lands as written (an unshifted clamp lands 7)");
        ok &= FixedClamp(-128, -128, 0, "fixed(12,4): raw -128 is -8.0 exactly and lands as written");
        if (!ok)
        {
            return 1;
        }
        Console.WriteLine("fixed-point F-shift / bits(N)");
        return 0;
    }

    static bool Check(bool cond, string line)
    {
        Console.WriteLine((cond ? "OK: " : "FAIL: ") + line);
        return cond;
    }

    static bool BitsClamp(uint planted, uint want, int wantClamped, string what)
    {
        var w = new Bits.BitsGrow();
        Bits.Schema.TableReset(w);
        w.Lead = 0xAAAAAAAAu;
        w.V = 5;
        w.Trail = 0xBBBBBBBBu;
        byte[] file = new byte[Bits.Schema.BitsGrowFixedMeasure(1)];
        if (Bits.Schema.BitsGrowFixedSave(w, file) != file.Length)
        {
            return Check(false, what + ": the writer did not fill its buffer");
        }
        int body = Bits.Schema.TableFixedWire.HeaderBytes + 4 + (int)Bits.Schema.BitsGrowFixedLayoutBytes + 8;
        if (BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(body + 4)) != 5u)
        {
            return Check(false, what + ": v is not at body offset 4");
        }
        BinaryPrimitives.WriteUInt32LittleEndian(file.AsSpan(body + 4), planted);

        var back = new Bits.BitsGrow();
        var r = new Bits.TableReport();
        var plan = new Bits.TableFixedEntry[64];
        long n = Bits.Schema.BitsGrowFixedLoad(back, file, plan, r);
        bool ok = true;
        ok &= Check(n == 1, what + ": one record");
        ok &= Check(back.V == want, $"{what}: v={back.V}, want {want}");
        ok &= Check(r.Clamped == wantClamped, $"{what}: clamped={r.Clamped}, want {wantClamped}");
        ok &= Check(back.Lead == 0xAAAAAAAAu && back.Trail == 0xBBBBBBBBu, what + ": lead and trail intact");
        ok &= Check(!r.Refused && !r.Malformed && r.Unknown == 0 && r.Widened == 0, what + ": no other counter moved");
        return ok;
    }

    static bool FixedClamp(short planted, short want, int wantClamped, string what)
    {
        var w = new Fx.FixedIGrow();
        Fx.Schema.TableReset(w);
        w.Lead = 0xAAAAAAAAu;
        w.V = 5;
        w.Trail = 0xBBBBBBBBu;
        byte[] file = new byte[Fx.Schema.FixedIGrowFixedMeasure(1)];
        if (Fx.Schema.FixedIGrowFixedSave(w, file) != file.Length)
        {
            return Check(false, what + ": the writer did not fill its buffer");
        }
        int body = Fx.Schema.TableFixedWire.HeaderBytes + 4 + (int)Fx.Schema.FixedIGrowFixedLayoutBytes + 8;
        if (BinaryPrimitives.ReadInt16LittleEndian(file.AsSpan(body + 4)) != 5)
        {
            return Check(false, what + ": v is not at body offset 4");
        }
        BinaryPrimitives.WriteInt16LittleEndian(file.AsSpan(body + 4), planted);

        var back = new Fx.FixedIGrow();
        var r = new Fx.TableReport();
        var plan = new Fx.TableFixedEntry[64];
        long n = Fx.Schema.FixedIGrowFixedLoad(back, file, plan, r);
        bool ok = true;
        ok &= Check(n == 1, what + ": one record");
        ok &= Check(back.V == want, $"{what}: raw v={back.V}, want {want}");
        ok &= Check(r.Clamped == wantClamped, $"{what}: clamped={r.Clamped}, want {wantClamped}");
        ok &= Check(back.Lead == 0xAAAAAAAAu && back.Trail == 0xBBBBBBBBu, what + ": lead and trail intact");
        ok &= Check(!r.Refused && !r.Malformed && r.Unknown == 0 && r.Widened == 0, what + ": no other counter moved");
        return ok;
    }
}
