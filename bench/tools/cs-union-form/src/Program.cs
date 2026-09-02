// The C# union-form decision probe (schema#262). README.md beside this file
// carries the question, the sitting behind the decision, and the reason this
// is NOT a bench-standard leg: it emits no CSV row, run.sh does not know about
// it, and nothing here may be divided against a bench CSV.
//
// A fixed table with a union has no native C# spelling, so the owner licensed
// two candidates. Both are measured here on a REPRESENTATIVE SHAPE — the space
// per-frame render table: a bounded array of records, each carrying a union of
// blittable arms, decoded from the table wire every frame. The shape is this
// probe's own, hand-written on purpose: the question is which C# STORAGE FORM
// wins, so the two forms must differ in nothing else.
//
//   Form A — all arms INLINE, max-of-arms via [StructLayout(LayoutKind.Explicit)]
//            (the arms are blittable here, so the overlap is legal).
//   Form B — a tag beside ONE REFERENCE PER ARM, allocated at construction and
//            reused on every read (the C# packet emitter's existing union form,
//            and what the table backend ships).
//   Form C — one reference per arm, allocated ON READ: the literal reading of
//            the owner's option (b).
//
// Measured: steady-state Load of one 256-item frame, and the allocation the
// read path performs.

using System;
using System.Diagnostics;
using System.Runtime.InteropServices;

// ---------------------------------------------------------------- form A ----

struct SolidA { public uint Color; }
struct TexturedA { public uint Tex; public float U; public float V; }
struct SkinnedA { public int Bone; public float Weight; public float Bias; public float Scale; }

[StructLayout(LayoutKind.Explicit)]
struct EffectArmsA
{
    [FieldOffset(0)] public SolidA Solid;
    [FieldOffset(0)] public TexturedA Textured;
    [FieldOffset(0)] public SkinnedA Skinned;
}

struct EffectA
{
    public byte Tag;
    public EffectArmsA Arms;
}

sealed class ItemA
{
    public uint Id;
    public float X, Y, Z;
    public EffectA Effect;
}

sealed class FrameA
{
    public ItemA[] Items = new ItemA[256];
    public int ItemsCount;
    public FrameA()
    {
        for (int i = 0; i < Items.Length; i++) { Items[i] = new ItemA(); }
    }
}

// ---------------------------------------------------------------- form B ----

sealed class SolidB { public uint Color; }
sealed class TexturedB { public uint Tex; public float U; public float V; }
sealed class SkinnedB { public int Bone; public float Weight; public float Bias; public float Scale; }

sealed class EffectB
{
    public byte Tag;
    public SolidB Solid = new SolidB();
    public TexturedB Textured = new TexturedB();
    public SkinnedB Skinned = new SkinnedB();
}

sealed class ItemB
{
    public uint Id;
    public float X, Y, Z;
    public EffectB Effect = new EffectB();
}

sealed class FrameB
{
    public ItemB[] Items = new ItemB[256];
    public int ItemsCount;
    public FrameB()
    {
        for (int i = 0; i < Items.Length; i++) { Items[i] = new ItemB(); }
    }
}

// ---------------------------------------------------------------- the wire --

static class Program
{
    const int Items = 256;
    const int Iterations = 20000;

    static byte[] BuildWire()
    {
        // one record: id (u32), x/y/z (f32), tag (u8), then the selected arm's own bytes
        byte[] wire = new byte[Items * 40];
        int n = 0;
        for (int i = 0; i < Items; i++)
        {
            BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), (uint)i); n += 4;
            BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), (float)i); n += 4;
            BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), (float)(i * 2)); n += 4;
            BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), (float)(i * 3)); n += 4;
            byte tag = (byte)(1 + (i % 3));
            wire[n++] = tag;
            switch (tag)
            {
                case 1:
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), (uint)(0xFF00 + i)); n += 4;
                    break;
                case 2:
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), (uint)i); n += 4;
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), 0.5f); n += 4;
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), 0.25f); n += 4;
                    break;
                default:
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), i); n += 4;
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), 1.0f); n += 4;
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), 2.0f); n += 4;
                    BitConverter.TryWriteBytes(new Span<byte>(wire, n, 4), 3.0f); n += 4;
                    break;
            }
        }
        byte[] exact = new byte[n];
        Array.Copy(wire, exact, n);
        return exact;
    }

    static void LoadA(FrameA frame, ReadOnlySpan<byte> wire)
    {
        int n = 0;
        int count = 0;
        while (n + 17 <= wire.Length && count < frame.Items.Length)
        {
            ItemA item = frame.Items[count];
            item.Id = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
            item.X = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            item.Y = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            item.Z = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            byte tag = wire[n++];
            item.Effect.Tag = tag;
            switch (tag)
            {
                case 1:
                    item.Effect.Arms.Solid = default;
                    item.Effect.Arms.Solid.Color = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
                    break;
                case 2:
                    item.Effect.Arms.Textured = default;
                    item.Effect.Arms.Textured.Tex = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
                    item.Effect.Arms.Textured.U = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Arms.Textured.V = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    break;
                default:
                    item.Effect.Arms.Skinned = default;
                    item.Effect.Arms.Skinned.Bone = BitConverter.ToInt32(wire.Slice(n, 4)); n += 4;
                    item.Effect.Arms.Skinned.Weight = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Arms.Skinned.Bias = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Arms.Skinned.Scale = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    break;
            }
            count++;
        }
        frame.ItemsCount = count;
    }

    static void LoadB(FrameB frame, ReadOnlySpan<byte> wire)
    {
        int n = 0;
        int count = 0;
        while (n + 17 <= wire.Length && count < frame.Items.Length)
        {
            ItemB item = frame.Items[count];
            item.Id = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
            item.X = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            item.Y = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            item.Z = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            byte tag = wire[n++];
            item.Effect.Tag = tag;
            switch (tag)
            {
                case 1:
                    item.Effect.Solid.Color = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
                    break;
                case 2:
                    item.Effect.Textured.Tex = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
                    item.Effect.Textured.U = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Textured.V = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    break;
                default:
                    item.Effect.Skinned.Bone = BitConverter.ToInt32(wire.Slice(n, 4)); n += 4;
                    item.Effect.Skinned.Weight = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Skinned.Bias = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Skinned.Scale = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    break;
            }
            count++;
        }
        frame.ItemsCount = count;
    }

    // C — one reference per arm allocated ON READ, the literal reading of the
    // owner's option (b). Same storage as B, but the selected arm is a fresh
    // object every frame.
    static void LoadC(FrameB frame, ReadOnlySpan<byte> wire)
    {
        int n = 0;
        int count = 0;
        while (n + 17 <= wire.Length && count < frame.Items.Length)
        {
            ItemB item = frame.Items[count];
            item.Id = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
            item.X = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            item.Y = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            item.Z = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
            byte tag = wire[n++];
            item.Effect.Tag = tag;
            switch (tag)
            {
                case 1:
                    item.Effect.Solid = new SolidB();
                    item.Effect.Solid.Color = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
                    break;
                case 2:
                    item.Effect.Textured = new TexturedB();
                    item.Effect.Textured.Tex = BitConverter.ToUInt32(wire.Slice(n, 4)); n += 4;
                    item.Effect.Textured.U = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Textured.V = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    break;
                default:
                    item.Effect.Skinned = new SkinnedB();
                    item.Effect.Skinned.Bone = BitConverter.ToInt32(wire.Slice(n, 4)); n += 4;
                    item.Effect.Skinned.Weight = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Skinned.Bias = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    item.Effect.Skinned.Scale = BitConverter.ToSingle(wire.Slice(n, 4)); n += 4;
                    break;
            }
            count++;
        }
        frame.ItemsCount = count;
    }

    static double Median(double[] v)
    {
        Array.Sort(v);
        return v[v.Length / 2];
    }

    static void Main()
    {
        byte[] wire = BuildWire();
        FrameA frameA = new FrameA();
        FrameB frameB = new FrameB();

        // warm up the tiers
        for (int i = 0; i < 200000; i++) { LoadA(frameA, wire); LoadB(frameB, wire); LoadC(frameB, wire); }

        double[] a = new double[7], b = new double[7], c = new double[7];
        long allocA = 0, allocB = 0, allocC = 0;
        for (int run = 0; run < 7; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            long before = GC.GetAllocatedBytesForCurrentThread();
            for (int i = 0; i < Iterations; i++) { LoadA(frameA, wire); }
            allocA = GC.GetAllocatedBytesForCurrentThread() - before;
            sw.Stop();
            a[run] = sw.Elapsed.TotalMilliseconds * 1e6 / Iterations; // ns per frame

            sw = Stopwatch.StartNew();
            before = GC.GetAllocatedBytesForCurrentThread();
            for (int i = 0; i < Iterations; i++) { LoadB(frameB, wire); }
            allocB = GC.GetAllocatedBytesForCurrentThread() - before;
            sw.Stop();
            b[run] = sw.Elapsed.TotalMilliseconds * 1e6 / Iterations;

            sw = Stopwatch.StartNew();
            before = GC.GetAllocatedBytesForCurrentThread();
            for (int i = 0; i < Iterations; i++) { LoadC(frameB, wire); }
            allocC = GC.GetAllocatedBytesForCurrentThread() - before;
            sw.Stop();
            c[run] = sw.Elapsed.TotalMilliseconds * 1e6 / Iterations;
        }

        Console.WriteLine("shape: 256-record frame, union of 3 blittable arms, " + wire.Length + " wire bytes");
        Console.WriteLine("iterations per run: " + Iterations + ", median of 7");
        Console.WriteLine();
        Console.WriteLine("A  arms inline (explicit layout)      " + Median(a).ToString("F0") +
                          " ns/frame   alloc " + (allocA / Iterations) + " B/frame");
        Console.WriteLine("B  tag + arms pre-allocated (shipped) " + Median(b).ToString("F0") +
                          " ns/frame   alloc " + (allocB / Iterations) + " B/frame");
        Console.WriteLine("C  one reference per arm, on read     " + Median(c).ToString("F0") +
                          " ns/frame   alloc " + (allocC / Iterations) + " B/frame");
        Console.WriteLine();
        Console.WriteLine("B/A ratio " + (Median(b) / Median(a)).ToString("F3") +
                          "   C/A ratio " + (Median(c) / Median(a)).ToString("F3"));

        // resident footprint per record, measured by construction
        GC.Collect(); GC.WaitForPendingFinalizers(); GC.Collect();
        long baseline = GC.GetAllocatedBytesForCurrentThread();
        FrameA[] holdA = new FrameA[64];
        for (int i = 0; i < holdA.Length; i++) { holdA[i] = new FrameA(); }
        long madeA = GC.GetAllocatedBytesForCurrentThread() - baseline;
        baseline = GC.GetAllocatedBytesForCurrentThread();
        FrameB[] holdB = new FrameB[64];
        for (int i = 0; i < holdB.Length; i++) { holdB[i] = new FrameB(); }
        long madeB = GC.GetAllocatedBytesForCurrentThread() - baseline;
        Console.WriteLine();
        Console.WriteLine("construction footprint, per 256-record frame:  A " + (madeA / 64) +
                          " B      B " + (madeB / 64) + " B");
        Console.WriteLine("(A and B are held live to defeat dead-store elimination: " +
                          (holdA[0].Items.Length + holdB[0].Items.Length) + ")");
    }
}
