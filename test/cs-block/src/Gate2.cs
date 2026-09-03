// GATE 2's C# half (SPEC-TABLES.md §12.1): the per-frame READ, generated form
// against the hand-written mirror it replaces.
//
// THE BAR, in the owner's words: the generated form must be the SAME SPEED, or
// "not significantly slower", than the hand mirror. A regression is a defect to
// explain or close, not a trade to license.
//
// WHAT THE TWO ARMS ARE. Both read the SAME BYTES — one block a C++ producer
// wrote — and accumulate the same checksum over every row of every section.
// They differ in exactly one thing, which is the thing the form changes:
//
//   * the HAND arm declares its own [StructLayout(Sequential, Pack = 1,
//     Size = N)] mirror of every row and its own header of nine
//     (offset, count, stride) triples, and a person edits them in the same
//     commit as the C++ side — the shape this form replaces, transcribed;
//   * the GENERATED arm opens the block and takes each section as a
//     ReadOnlySpan<Row> over the generated blittable struct.
//
// BOTH READ THROUGH A CONTIGUOUS SPAN REINTERPRET at pitch == sizeof. That is
// the fast path the gate is about: blittable rows both generated sides index
// with, no marshalling and no copy AT THE BOUNDARY, and nothing per row.
//
// THE GOLDEN GATE COMES FIRST: the two arms' checksums must agree before any
// clock starts, and a mismatch REFUSES to bench.

using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using Blockdemo;

static class Gate2
{
    // ---- the hand-written mirror this form replaces ----
    //
    // Hand-declared, Pack = 1, Size pinned by hand against the C++ sizeof —
    // the wall a person keeps in step by editing it in the same commit. Only
    // the fields this read touches are spelled out; the rest is padding, which
    // is precisely the hand-kept discipline the generated form deletes.

    [StructLayout(LayoutKind.Sequential, Pack = 1, Size = 16)]
    struct HandSection
    {
        public ulong Offset;
        public uint Count;
        public uint Stride;
    }

    [StructLayout(LayoutKind.Sequential, Pack = 1, Size = 88)]
    struct HandShip
    {
        public double PositionX;
        public double PositionY;
        public double PositionZ;
        public double RotationX;
        public double RotationY;
        public double RotationZ;
        public double RotationW;
        public ulong Flags;
        public uint ObjectId;
        public uint TargetObjectId;
        public float Thrust;
        public byte ObjectSequence;
        public byte ShipType;
        public byte Team;
        public byte HasTargetLock;
        public byte PredictedExplode;
    }

    [StructLayout(LayoutKind.Sequential, Pack = 1, Size = 64)]
    struct HandTurret
    {
        public double RotationX;
        public double RotationY;
        public double RotationZ;
        public double RotationW;
        public ulong Flags;
        public uint ObjectId;
        public uint ParentObjectId;
        public uint TurretIndex;
        public uint TargetObjectId;
        public byte ObjectSequence;
        public byte Team;
        public byte HasTargetLock;
    }

    [StructLayout(LayoutKind.Sequential, Pack = 1, Size = 80)]
    struct HandStaticProp
    {
        public double PositionX;
        public double PositionY;
        public double PositionZ;
        public double RotationX;
        public double RotationY;
        public double RotationZ;
        public double RotationW;
        public double Scale;
        public ulong Flags;
        public uint StaticPropId;
        public byte PropType;
        public byte Team;
    }

    // The hand header's section order, by hand: the three sections this read
    // touches sit at these triple indices, and a person keeps THAT in step too.
    const int ShipSection = 1;
    const int TurretSection = 2;
    const int StaticPropSection = 5;

    static unsafe double HandRead(byte* at)
    {
        // The block's projection, mirrored BY HAND: the 24-byte prologue, the
        // table's own uint64, and then the nine triples. Every one of those
        // numbers is written down here by a person and kept in step with the
        // C++ side by hand, which is the discipline the generated form deletes.
        HandSection* sections = (HandSection*) (at + 32);
        double sum = 0.0;

        HandSection ships = sections[ShipSection];
        ReadOnlySpan<HandShip> shipRows = new ReadOnlySpan<HandShip>(at + ships.Offset, (int) ships.Count);
        for (int i = 0; i < shipRows.Length; i++)
        {
            sum += shipRows[i].PositionX + shipRows[i].RotationW + shipRows[i].ObjectId + shipRows[i].Thrust
                 + shipRows[i].Team + shipRows[i].HasTargetLock;
        }

        HandSection turrets = sections[TurretSection];
        ReadOnlySpan<HandTurret> turretRows = new ReadOnlySpan<HandTurret>(at + turrets.Offset, (int) turrets.Count);
        for (int i = 0; i < turretRows.Length; i++)
        {
            sum += turretRows[i].RotationW + turretRows[i].ObjectId + turretRows[i].TurretIndex + turretRows[i].Team;
        }

        HandSection props = sections[StaticPropSection];
        ReadOnlySpan<HandStaticProp> propRows = new ReadOnlySpan<HandStaticProp>(at + props.Offset, (int) props.Count);
        for (int i = 0; i < propRows.Length; i++)
        {
            sum += propRows[i].PositionX + propRows[i].Scale + propRows[i].StaticPropId + propRows[i].Team;
        }
        return sum;
    }

    // ---- the generated form ----

    static double GeneratedRead(RenderFrameBlock block)
    {
        double sum = 0.0;

        ReadOnlySpan<RenderShipRow> ships = block.ShipsSpan;
        for (int i = 0; i < ships.Length; i++)
        {
            sum += ships[i].Position.X + ships[i].Rotation.W + ships[i].ObjectId + ships[i].Thrust
                 + (byte) ships[i].Team + (ships[i].HasTargetLock ? 1 : 0);
        }

        ReadOnlySpan<RenderTurretRow> turrets = block.TurretsSpan;
        for (int i = 0; i < turrets.Length; i++)
        {
            sum += turrets[i].Rotation.W + turrets[i].ObjectId + turrets[i].TurretIndex + (byte) turrets[i].Team;
        }

        ReadOnlySpan<RenderStaticPropRow> props = block.StaticPropsSpan;
        for (int i = 0; i < props.Length; i++)
        {
            sum += props[i].Position.X + props[i].Scale + props[i].StaticPropId + (byte) props[i].Team;
        }
        return sum;
    }

    static double Median(double[] samples)
    {
        double[] sorted = (double[]) samples.Clone();
        Array.Sort(sorted);
        return sorted[sorted.Length / 2];
    }

    // Returns true when the gate passes. The caller owns the exit code.
    // smoke runs the CORRECTNESS half whole and does not enforce the timing
    // band: the band is a paired same-sitting measurement and a shared CI
    // runner has no quiet window, so a nightly leg enforcing it would report
    // the runner's mood. What a nightly leg CAN prove is that this gate still
    // builds and still reads the C++ half's frame through the generated form.
    internal static unsafe bool Run(string goldenPath, bool smoke = false)
    {
        if (!File.Exists(goldenPath))
        {
            Console.WriteLine("REFUSING TO BENCH: missing " + goldenPath);
            return false;
        }
        byte[] bytes = File.ReadAllBytes(goldenPath);
        IntPtr raw = Marshal.AllocHGlobal(bytes.Length + 64);
        try
        {
            long alignedAddress = ((long) raw + 63) & ~63L;
            IntPtr pointer = new IntPtr(alignedAddress);
            Marshal.Copy(bytes, 0, pointer, bytes.Length);
            byte* at = (byte*) pointer;

            RenderFrameBlock block;
            if (!RenderFrameBlock.Open(out block, pointer, bytes.Length))
            {
                Console.WriteLine("REFUSING TO BENCH: the block does not open");
                return false;
            }

            // THE GOLDEN GATE, FIRST: the two arms must agree before any clock
            // starts. Timing something that does not agree measures nothing.
            double hand = HandRead(at);
            double generated = GeneratedRead(block);
            if (hand != generated)
            {
                Console.WriteLine("REFUSING TO BENCH: the two arms disagree (" + hand + " hand, " + generated + " generated)");
                return false;
            }
            if (block.ShipsSpan.Length == 0)
            {
                Console.WriteLine("REFUSING TO BENCH: the frame is empty");
                return false;
            }

            int Warmup = smoke ? 50 : 2000;
            int Samples = smoke ? 3 : 15;
            int Reads = smoke ? 10 : 200;

            double sink = 0.0;
            for (int i = 0; i < Warmup; i++)
            {
                sink += HandRead(at);
                sink += GeneratedRead(block);
            }

            double[] handUs = new double[Samples];
            double[] generatedUs = new double[Samples];
            Stopwatch watch = new Stopwatch();
            for (int s = 0; s < Samples; s++)
            {
                watch.Restart();
                for (int r = 0; r < Reads; r++) { sink += HandRead(at); }
                watch.Stop();
                handUs[s] = watch.Elapsed.TotalMilliseconds * 1000.0 / Reads;

                watch.Restart();
                for (int r = 0; r < Reads; r++) { sink += GeneratedRead(block); }
                watch.Stop();
                generatedUs[s] = watch.Elapsed.TotalMilliseconds * 1000.0 / Reads;
            }

            double handMedian = Median(handUs);
            double generatedMedian = Median(generatedUs);
            double ratio = generatedMedian / handMedian;

            Console.WriteLine("gate 2, the per-frame C# READ (SPEC-TABLES.md §12.1)");
            Console.WriteLine("  hand-written mirror  : " + handMedian.ToString("F3") + " us/frame (median of " + Samples + ")");
            Console.WriteLine("  generated block form : " + generatedMedian.ToString("F3") + " us/frame (median of " + Samples + ")");
            Console.WriteLine("  ratio (generated/hand): " + ratio.ToString("F3"));
            Console.WriteLine("  (sink " + (sink != 0.0 ? 1 : 0) + ")");

            // THE BAR: the same speed, or not significantly slower.
            const double Band = 1.05;
            if (smoke)
            {
                Console.WriteLine("GATE 2 (C# read) SMOKE: correctness held, the band NOT enforced — a shared runner has no quiet window");
                return true;
            }
            if (ratio > Band)
            {
                Console.WriteLine("GATE 2 FAILED: the generated form is " + ((ratio - 1.0) * 100.0).ToString("F1") +
                    "% slower than the hand mirror, past the 5% band");
                return false;
            }
            Console.WriteLine("GATE 2 (C# read): the generated form is the same speed, or not significantly slower");
            return true;
        }
        finally
        {
            Marshal.FreeHGlobal(raw);
        }
    }
}
