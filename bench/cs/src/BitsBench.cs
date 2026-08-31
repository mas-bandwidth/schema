// schema bench — family bits for the C# runner (the gen half lives in
// Program.cs).
//
// Family bits (BENCH-STANDARD.md §1.4): the raw BitWriter/BitReader with the
// 16-width table (227 bits/group) over a 65536-byte buffer. Values vary per
// pass through the LCG (widths are the structure and stay fixed; bytes/pass
// asserted constant); reads rotate 64 pre-written variant buffers, each
// verified to read back exactly what was written before any number is
// produced. Internal types per §3.1; the timed loops carry
// [MethodImpl(NoInlining)] so the §4.1 JitDisasm verdict has a loop body to
// count.

using System;
using System.Diagnostics;
using System.Runtime.CompilerServices;
using Serialize;

static partial class Program
{
    const int BitsNumWidths = 16;
    const int BitsBufferSize = 65536;

    static readonly int[] BitsWidths = { 1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22 }; // 227 bits/group

    static readonly byte[] gBitsBuffer = new byte[BitsBufferSize];
    static readonly byte[][] gBitsVariants = new byte[NumVariants][];

    static uint BitsMask(int width)
    {
        return width == 32 ? 0xFFFFFFFF : (1u << width) - 1;
    }

    // the per-pass value variation: one LCG step per pass, values from its bits
    static void VaryBitsValues(uint[] values, ulong rng)
    {
        for (int i = 0; i < BitsNumWidths; i++)
        {
            values[i] = (uint)(rng >> i) & BitsMask(BitsWidths[i]);
        }
    }

    // the single untimed WriteBits call site (§3.2)
    static long BitsWritePass(byte[] buffer, uint[] values)
    {
        BitWriter w = new BitWriter(buffer);
        while (w.BitsAvailable >= 256)
        {
            for (int i = 0; i < BitsNumWidths; i++)
            {
                w.WriteBits(values[i], BitsWidths[i]);
            }
        }
        w.FlushBits();
        return w.BytesWritten;
    }

    // the single untimed ReadBits call site (§3.2): the buffer must read back
    // exactly the values written — the bits family's refusal gate
    static bool BitsReadVerify(byte[] buffer, uint[] values)
    {
        BitReader r = new BitReader(buffer, BitsBufferSize);
        while (r.BitsRemaining >= 256)
        {
            for (int i = 0; i < BitsNumWidths; i++)
            {
                if (r.ReadBits(BitsWidths[i]) != values[i])
                {
                    return false;
                }
            }
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool BitpackerWriteLoop(long passes, long bytesPerPass, ref ulong rng, uint[] values)
    {
        BitWriter w = new BitWriter(gBitsBuffer);
        for (long pass = 0; pass < passes; pass++)
        {
            rng = BenchRng(rng);
            VaryBitsValues(values, rng);
            w.Reset(gBitsBuffer);
            while (w.BitsAvailable >= 256)
            {
                for (int i = 0; i < BitsNumWidths; i++)
                {
                    w.WriteBits(values[i], BitsWidths[i]);
                }
            }
            w.FlushBits();
            if (w.BytesWritten != bytesPerPass)
            {
                return false; // the bytes_per_op assertion (§2.7)
            }
            gSink = gSink + (ulong)w.BytesWritten;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool BitpackerReadLoop(long passes)
    {
        BitReader r = new BitReader(gBitsVariants[0], BitsBufferSize);
        for (long pass = 0; pass < passes; pass++)
        {
            r.Reset(gBitsVariants[pass & (NumVariants - 1)], BitsBufferSize);
            ulong sum = 0;
            while (r.BitsRemaining >= 256)
            {
                for (int i = 0; i < BitsNumWidths; i++)
                {
                    sum += r.ReadBits(BitsWidths[i]);
                }
            }
            gSink = gSink + sum;
        }
        return true;
    }

    static void BenchBitpacker(long passes)
    {
        uint[] values = new uint[BitsNumWidths];
        for (int k = 0; k < NumVariants; k++)
        {
            gBitsVariants[k] = new byte[BitsBufferSize];
        }

        ulong rng = 1;
        long bytesPerPass = -1;
        for (int k = 0; k < NumVariants; k++)
        {
            rng = BenchRng(rng);
            VaryBitsValues(values, rng);
            long wrote = BitsWritePass(gBitsVariants[k], values);
            if (bytesPerPass < 0)
            {
                bytesPerPass = wrote;
            }
            if (wrote != bytesPerPass)
            {
                Fail("bitpacker", "variation changed bytes/pass — widths are the structure and must stay fixed");
                return;
            }
            if (!BitsReadVerify(gBitsVariants[k], values))
            {
                Fail("bitpacker", "read-back disagrees with written values — refusing to bench");
                return;
            }
        }

        double[] writeRates = new double[gNumRuns];
        double[] readRates = new double[gNumRuns];

        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            if (!BitpackerWriteLoop(passes, bytesPerPass, ref rng, values))
            {
                Fail("bitpacker", "bytes/pass changed in the timed loop (§2.7 assertion)");
                return;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                writeRates[run] = passes / time;
            }
        }

        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            if (!BitpackerReadLoop(passes))
            {
                Fail("bitpacker", "read loop failed");
                return;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                readRates[run] = passes / time;
            }
        }

        Report("bitpacker", "write", passes, bytesPerPass, Stats(writeRates), "bits");
        Report("bitpacker", "read", passes, bytesPerPass, Stats(readRates), "bits");
    }
}
