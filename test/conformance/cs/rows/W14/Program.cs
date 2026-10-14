using System;
using System.Runtime.InteropServices;
using System.Runtime.CompilerServices;
using Tblk1;

// cell-cs-w14 — plan dst == offsetof/sizeof
//
// The law (docs/FIXED-FORM-ALGORITHM.md:1672): the build-time assertion that
// the plan's destinations equal its own offsetof and sizeof.
//
// In C# the plan uses slot indices, and the slots write to class fields via
// setRaw lambdas. This test defines probe structs that mirror the fixed-form
// wire layout and verifies that each slot's field in the probe lands at the
// offset the plan entry's source position names.

unsafe class Program
{
    [StructLayout(LayoutKind.Sequential, Pack = 1)]
    struct RootProbe
    {
        public byte Grade;
        public ushort Raw;
    }

    static int Main()
    {
        bool ok = true;

        ok &= CheckRootProbe();
        ok &= CheckRootPlan();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("plan dst == offsetof/sizeof");
        return 0;
    }

    static bool CheckRootProbe()
    {
        RootProbe probe = default;
        byte* at = (byte*)&probe;

        int sizeofProbe = Unsafe.SizeOf<RootProbe>();
        long wantBody = Schema.RootFixedBodyBytes;
        if (sizeofProbe != wantBody)
        {
            Console.Error.WriteLine(
                "FAIL: sizeof(RootProbe)={0}  want fixed-body-bytes={1}", sizeofProbe, wantBody);
            return false;
        }
        Console.WriteLine("OK: sizeof(RootProbe)={0} == RootFixedBodyBytes", sizeofProbe);

        long gradeOff = (byte*)&probe.Grade - at;
        if (gradeOff != 0)
        {
            Console.Error.WriteLine("FAIL: Grade offset={0}  want plan src=0", gradeOff);
            return false;
        }
        Console.WriteLine("OK: Root.Grade offset={0} == plan src 0", gradeOff);

        int gradeSize = sizeof(byte);
        // Entry 0: src=0 size=1
        if (gradeSize != 1)
        {
            Console.Error.WriteLine("FAIL: Grade storage size={0} want 1", gradeSize);
            return false;
        }

        long rawOff = (byte*)&probe.Raw - at;
        if (rawOff != 1)
        {
            Console.Error.WriteLine("FAIL: Raw offset={0}  want plan src=1", rawOff);
            return false;
        }
        Console.WriteLine("OK: Root.Raw  offset={0} == plan src 1", rawOff);

        int rawSize = Unsafe.SizeOf<ushort>();
        // Entry 1: src=1 size=2
        if (rawSize != 2)
        {
            Console.Error.WriteLine("FAIL: Raw storage size={0} want 2", rawSize);
            return false;
        }

        long end = rawOff + rawSize;
        if (end != wantBody)
        {
            Console.Error.WriteLine(
                "FAIL: probe end={0}  want fixed-body-bytes={1}", end, wantBody);
            return false;
        }

        return true;
    }

    static bool CheckRootPlan()
    {
        var plan = Schema.RootFixedPlan;
        if (plan.Count != 2)
        {
            Console.Error.WriteLine("FAIL: plan entry count={0}  want 2", plan.Count);
            return false;
        }

        // Entry 0: grade — 1 byte at src 0, dst slot 0
        {
            var e = plan.Entries[0];
            if (e.Src != 0 || e.Dst != 0 || e.Size != 1)
            {
                Console.Error.WriteLine(
                    "FAIL: plan[0] src={0} dst={1} size={2}  want src=0 dst=0 size=1",
                    e.Src, e.Dst, e.Size);
                return false;
            }
            Console.WriteLine("OK: plan[0] src={0} dst={1} size={2} (Grade)", e.Src, e.Dst, e.Size);
        }

        // Entry 1: raw — 2 bytes at src 1, dst slot 1
        {
            var e = plan.Entries[1];
            if (e.Src != 1 || e.Dst != 1 || e.Size != 2)
            {
                Console.Error.WriteLine(
                    "FAIL: plan[1] src={0} dst={1} size={2}  want src=1 dst=1 size=2",
                    e.Src, e.Dst, e.Size);
                return false;
            }
            Console.WriteLine("OK: plan[1] src={0} dst={1} size={2} (Raw)", e.Src, e.Dst, e.Size);
        }

        // the src ranges tile the body without gaps
        long body = Schema.RootFixedBodyBytes;
        long covered = 0;
        for (int i = 0; i < plan.Count; ++i)
        {
            long s = plan.Entries[i].Src;
            long n = plan.Entries[i].Size;
            if (s != covered)
            {
                Console.Error.WriteLine(
                    "FAIL: plan[{0}] src gap: expect cover={1} got src={2}", i, covered, s);
                return false;
            }
            covered += n;
        }
        if (covered != body)
        {
            Console.Error.WriteLine(
                "FAIL: plan covers {0} bytes, body is {1}", covered, body);
            return false;
        }
        Console.WriteLine("OK: plan tiles the {0} body bytes", body);

        return true;
    }
}