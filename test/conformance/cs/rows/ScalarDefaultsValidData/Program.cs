using System;
using System.Runtime.InteropServices;

namespace Serialize
{
    public class WriteStream
    {
        public bool SerializeFixed(ref long x, int bits, int frac, long min, long max) { return true; }
        public bool SerializeFixed(ref uint x, int bits, int frac, long min, long max) { return true; }
    }

    public class ReadStream
    {
        public bool SerializeFixed(ref long x, int bits, int frac, long min, long max) { return true; }
        public bool SerializeFixed(ref uint x, int bits, int frac, long min, long max) { return true; }
    }
}

// cell-cs-scalar-defaults-valid-data — Scalar and enum defaults: valid-data write/read acceptance
//
// The law (docs/FIXED-FORM-ALGORITHM.md:1682): the assertion that
// scalar and enum defaults survive a fixed-form write/read roundtrip.
//
// This test saves a SimState with declared defaults, reads it back, and
// verifies that the non-zero defaults (Energy=-250, Scale=1.0, Pose.X=0.5)
// are preserved through the wire.

unsafe class Program
{
    static int Main()
    {
        bool ok = true;

        var src = new Scalardemo.SimState();
        long need = Scalardemo.Schema.SimStateFixedMeasure(1);
        var buf = new byte[need];
        long saved = Scalardemo.Schema.SimStateFixedSave(src, buf);
        if (saved != need)
        {
            Console.Error.WriteLine("FAIL: SimStateFixedSave returned {0} want {1}", saved, need);
            return 1;
        }

        var dst = new Scalardemo.SimState();
        dst.Energy = 0;
        dst.Scale = 0;
        dst.Pose.X = 0;

        var plan = new Scalardemo.TableFixedEntry[Scalardemo.Schema.SimStateFixedPlan.Count];
        Scalardemo.Schema.SimStateFixedPlan.Entries.CopyTo(plan.AsSpan());
        long loaded = Scalardemo.Schema.SimStateFixedLoad(dst, buf, plan);
        if (loaded != 1)
        {
            Console.Error.WriteLine("FAIL: SimStateFixedLoad returned {0} want 1", loaded);
            return 1;
        }

        System.Int128 wantEnergy = unchecked((System.Int128)(((UInt128)0xfffffffffffffffful << 64) | 0xffffffffffffff06ul));
        if (dst.Energy != wantEnergy)
        {
            Console.Error.WriteLine("FAIL: Energy={0} want {1} (-250)", dst.Energy, wantEnergy);
            ok = false;
        }
        else
        {
            Console.WriteLine("OK: Energy={0} (default -250 survives write/read)", dst.Energy);
        }

        if (dst.Scale != 65536)
        {
            Console.Error.WriteLine("FAIL: Scale={0} want 65536 (1.0 Q16.16)", dst.Scale);
            ok = false;
        }
        else
        {
            Console.WriteLine("OK: Scale={0} (default 1.0 survives write/read)", dst.Scale);
        }

        if (dst.Pose == null || dst.Pose.X != 32768L)
        {
            Console.Error.WriteLine("FAIL: Pose.X={0} want 32768 (0.5 Q48.16)", dst.Pose?.X);
            ok = false;
        }
        else
        {
            Console.WriteLine("OK: Pose.X={0} (default 0.5 survives write/read)", dst.Pose.X);
        }

        if (dst.Axes == null)
        {
            Console.Error.WriteLine("FAIL: Axes is null");
            ok = false;
        }
        else
        {
            for (int i = 0; i < dst.Axes.Slots.Length; ++i)
            {
                if (dst.Axes.Slots[i] != 0)
                {
                    Console.Error.WriteLine("FAIL: Axes slot {0}={1} want 0", i, dst.Axes.Slots[i]);
                    ok = false;
                }
            }
            if (ok)
            {
                Console.WriteLine("OK: Axes slots all zero (enum-keyed defaults survive write/read)");
            }
        }

        // wire-level: Energy at body byte 82, -250 encoded as int128 LE
        // file header (16) + layout-len (4) + layout (616) + record-hash (8) = 644
        int bodyBase = 644;
        byte energyB0 = buf[bodyBase + 82];
        if (energyB0 != 0x06)
        {
            Console.Error.WriteLine("FAIL: wire Energy byte[82]={0:X2} want 0x06", energyB0);
            ok = false;
        }
        else
        {
            Console.WriteLine("OK: wire Energy LSB=0x06 (-250 encoded)");
        }

        // wire-level: Scale at body byte 114, 1.0 = 65536 = [00 00 01 00] LE
        if (buf[bodyBase + 114] != 0x00 || buf[bodyBase + 116] != 0x01)
        {
            Console.Error.WriteLine("FAIL: wire Scale bytes");
            ok = false;
        }
        else
        {
            Console.WriteLine("OK: wire Scale bytes (1.0 Q16.16)");
        }

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("Scalar and enum defaults: valid-data write/read acceptance");
        return 0;
    }
}