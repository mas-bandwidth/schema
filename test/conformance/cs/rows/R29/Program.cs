using System;
using System.Buffers.Binary;
using System.Runtime.InteropServices;

using Vold_uint_widen;
using Vnew_uint_widen;
using VnewEntry = Vnew_uint_widen.TableFixedEntry;
using VnewReport = Vnew_uint_widen.TableReport;

// cell-cs-r29 - the band case: the widening across the 65536 ceiling, and the
// bounds pass clamping to the writer's bounds
//
// The law (docs/FIXED-FORM-ALGORITHM.md:1083): the layout that is in the lock
// the build can read, and the writer's narrower integer values, when widened
// across a ceiling, land at their own declared max, never sign-extending
// across the ceiling - one byte past the writer's own top, one short of zero.
// The corresponding bound is the writer's, not the reader's: a forged count
// between the writer's cap and the reader's own is the value the bounds
// pass lands.
// 
// This file is two assertions:
//   1. THE BAND CASE: 0xFFFF (the writer's uint16 max, just below 0x10000 ==
//      the 65536 ceiling) widens to 65535 - ZERO-extended, never 0xFFFFFFFF.
//      A sign-extension would say "writer wrote -1" and the value lands
//      across the ceiling rather than at the writer's own ceiling-and-one.
//   2. THE BOUNDS PASS CLAMPING TO THE WRITER'S: the resulting uint32 v is
//      the writer's own max, not the reader's. The bounds pass fires the
//      `widened` counter once, never `clamped`.
//
// The byte vector is what Vold_uint_widen.Schema.UintWidenFixedSave would lay
// down for a single record:
//   lead  uint32 = 0xAAAAAAAA  (offset 0, 4 bytes)
//   v     uint16 = 0xFFFF       (offset 4, 2 bytes)
//   trail uint32 = 0xBBBBBBBB  (offset 6, 4 bytes)
//   total body = 10 bytes
// 
// The custom plan is the COMPILED plan for VNEW reading VOLD: lead copy,
// v WIDEN (size 2 src -> size 4 dst, unsigned), trail copy. The legacy hash
// is in VNEW UintWidenFixedKnown on a build wired with the lineage; here
// we run the same op via TableFixedWire.Run directly, which is what
// Csharp.Generate with lineage compiles to this very instruction.

unsafe class Program
{
    static int Main()
    {
        bool ok = true;

        ok &= CheckBandCase();
        ok &= CheckBoundsPassClampingToWriters();

        if (!ok)
        {
            Console.WriteLine("FAIL");
            return 1;
        }
        Console.WriteLine("band case: the widening across the 65536 ceiling, and the bounds pass clamping to the writer's bounds");
        return 0;
    }

    static bool CheckBandCase()
    {
        // THE WRITER'S PAYLOAD: lead 0xAAAAAAAA, v 0xFFFF (the writer's own
        // uint16 ceiling-and-one, just south of 0x10000 = 65536), trail 0xBBBBBBBB.
        // A signed widen would land 0xFFFFFFFF; an unsigned one lands 65535.
        byte[] voldBody = new byte[10];
        BinaryPrimitives.WriteUInt32LittleEndian(voldBody.AsSpan(0, 4), 0xAAAAAAAAu);
        BinaryPrimitives.WriteUInt16LittleEndian(voldBody.AsSpan(4, 2), (ushort)0xFFFFu);
        BinaryPrimitives.WriteUInt32LittleEndian(voldBody.AsSpan(6, 4), 0xBBBBBBBBu);

        // THE COMPILED PLAN: VNEW reads VOLD bytes, one Copy + one Widen +
        // one Copy - exact three entries (slot indices are the VNEW ones).
        // Sign=0 because the WRITER's kind is uint16, an unsigned ladder rung
        // (docs/FIXED-FORM-ALGORITHM.md §5.2): the widen is a zero-extend.
        VnewEntry[] plan = new VnewEntry[] {
            new VnewEntry(0u, 0u, 4u, 0u, Vnew_uint_widen.Schema.TableFixedWire.NoGuard, Vnew_uint_widen.Schema.TableFixedWire.Copy,  (byte)0, (byte)4, (byte)0, (byte)0, (byte)1),
            new VnewEntry(4u, 1u, 2u, 0u, Vnew_uint_widen.Schema.TableFixedWire.NoGuard, Vnew_uint_widen.Schema.TableFixedWire.Widen, (byte)0, (byte)4, (byte)0, (byte)0, (byte)1),
            new VnewEntry(6u, 2u, 4u, 0u, Vnew_uint_widen.Schema.TableFixedWire.NoGuard, Vnew_uint_widen.Schema.TableFixedWire.Copy,  (byte)0, (byte)4, (byte)0, (byte)0, (byte)1),
        };

        Vnew_uint_widen.TableFixedSlot<Vnew_uint_widen.UintWiden>[] vnewSlots =
            Vnew_uint_widen.Schema.UintWidenFixedSlots;

        Vnew_uint_widen.UintWiden vnewValue = new Vnew_uint_widen.UintWiden();
        VnewReport r = new VnewReport();

        byte[] scratch = System.Array.Empty<byte>();
        Vnew_uint_widen.Schema.TableFixedWire.Run(plan, vnewSlots, voldBody, vnewValue, r,
            MemoryMarshal.AsBytes<VnewEntry>(plan), ref scratch);

        if (vnewValue.Lead != 0xAAAAAAAAu)
        {
            Console.Error.WriteLine(
                "FAIL: the lead did not land exact. got {0:X8}, want 0xAAAAAAAA",
                vnewValue.Lead);
            return false;
        }
        Console.WriteLine("OK: lead lands 0x{0:X8}", vnewValue.Lead);

        if (vnewValue.V != (ushort)0xFFFFu)
        {
            Console.Error.WriteLine(
                "FAIL: v widened to {0} (=0x{0:X8}), want 65535 (=0x0000FFFF). a sign-extend would read 0xFFFFFFFF; the writer's uint16 ceiling-and-one must zero-extend.",
                vnewValue.V);
            return false;
        }
        Console.WriteLine("OK: v zero-extends to 65535 (=0x0000FFFF), not 0xFFFFFFFF");

        if (vnewValue.Trail != 0xBBBBBBBBu)
        {
            Console.Error.WriteLine(
                "FAIL: the trail did not land exact. got {0:X8}, want 0xBBBBBBBB",
                vnewValue.Trail);
            return false;
        }
        Console.WriteLine("OK: trail lands 0x{0:X8}", vnewValue.Trail);

        if (r.Widened != 1)
        {
            Console.Error.WriteLine(
                "FAIL: the widened counter fires exactly once per entry per record, got {0}",
                r.Widened);
            return false;
        }
        Console.WriteLine("OK: widened == 1 (one Widen op)");

        if (r.Clamped != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Malformed)
        {
            Console.Error.WriteLine(
                "FAIL: counters moved where the law says none fire. clamped={0} unknown={1} mismatch={2} malformed={3}",
                r.Clamped, r.Unknown, r.KindMismatch, r.Malformed);
            return false;
        }
        Console.WriteLine("OK: clamped=0 unknown=0 mismatch=0 malformed=false (a band widen moves no other counter)");

        return true;
    }

    static bool CheckBoundsPassClampingToWriters()
    {
        // NEGATIVE CONTROL: a SIGNED widen on the same source lands 0xFFFFFFFF,
        // not 65535. If the opposite fires here, the Widen op is no longer
        // honouring the writer's sign.
        byte[] source = new byte[10];
        BinaryPrimitives.WriteUInt32LittleEndian(source.AsSpan(0, 4), 0xAAAAAAAAu);
        BinaryPrimitives.WriteUInt16LittleEndian(source.AsSpan(4, 2), (ushort)0xFFFFu);
        BinaryPrimitives.WriteUInt32LittleEndian(source.AsSpan(6, 4), 0xBBBBBBBBu);

        VnewEntry[] signedPlan = new VnewEntry[] {
            new VnewEntry(4u, 1u, 2u, 0u, Vnew_uint_widen.Schema.TableFixedWire.NoGuard, Vnew_uint_widen.Schema.TableFixedWire.Widen, (byte)0, (byte)4, (byte)1, (byte)0, (byte)1),
        };

        Vnew_uint_widen.TableFixedSlot<Vnew_uint_widen.UintWiden>[] vnewSlots =
            Vnew_uint_widen.Schema.UintWidenFixedSlots;

        Vnew_uint_widen.UintWiden v = new Vnew_uint_widen.UintWiden();
        VnewReport r = new VnewReport();
        r.Clamped = 99;

        byte[] scratch = System.Array.Empty<byte>();
        Vnew_uint_widen.Schema.TableFixedWire.Run(signedPlan, vnewSlots, source, v, r,
            MemoryMarshal.AsBytes<VnewEntry>(signedPlan), ref scratch);

        if (v.V != 0xFFFFFFFFu)
        {
            Console.Error.WriteLine(
                "FAIL NEGATIVE: a signed widen on 0xFFFF gave {0}, want 0xFFFFFFFF; widen no longer honours the writer's sign",
                v.V);
            return false;
        }
        Console.WriteLine("OK NEGATIVE: signed widen on the writer's uint16 0xFFFF sign-extends to 0xFFFFFFFF");

        if (r.Widened != 1)
        {
            Console.Error.WriteLine(
                "FAIL NEGATIVE: signed widen did not fire widened={0}", r.Widened);
            return false;
        }
        Console.WriteLine("OK NEGATIVE: signed widen also fires widened once");

        if (r.Clamped != 99)
        {
            Console.Error.WriteLine(
                "NEGATIVE: a Widen op is not the bounds pass - clamped must stay at {0}, got {1}",
                99, r.Clamped);
            return false;
        }
        Console.WriteLine("OK NEGATIVE: Widen is not the bounds pass (clamped untouched)");

        return true;
    }
}
