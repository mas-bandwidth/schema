// schema bench — families rt and bits for the C# runner (the rt half of
// Program; the gen half lives in Program.cs).
//
// Family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize.cs runtime API
// called BY HAND — the four Bench.schema shapes as hand-written packets over
// the plain WriteStream/ReadStream Serialize* surface, the way a game would
// write them. (The generated code's WriteBatch surface is a generated-code
// optimization; family rt measures the plain stream API.) The §1.5 oracle
// gate byte-compares the hand-written wire against the goldens the GENERATED
// code pinned (testdata/wire/bench_*.bin) and round-trips before any number.
// Internal types per §3.1. Per §3.2 every benched op has EXACTLY two call
// sites: its untimed once-helper and its timed loop ([MethodImpl(NoInlining)],
// so the §4.1 JitDisasm verdict has a loop body to count).
//
// Family bits (§1.4): the raw BitWriter/BitReader with the 16-width table
// (227 bits/group) over a 65536-byte buffer. Values vary per pass through
// the LCG (widths are the structure and stay fixed; bytes/pass asserted
// constant); reads rotate 64 pre-written variant buffers, each verified to
// read back exactly what was written before any number is produced.

using System;
using System.Diagnostics;
using System.Runtime.CompilerServices;
using Serialize;

static partial class Program
{
    // ---- the four shapes, hand-written (Bench.schema §1.3) ----

    internal sealed class RtBenchPacket
    {
        public int A, B, C;
        public uint Bits7, Bits13, Bits23;
        public bool Flag;
        public float X, Y, Z;
        public ulong Big;
        public byte[] Blob = new byte[17];
    }

    static bool WriteRtPacket(WriteStream s, RtBenchPacket p)
    {
        if (!s.SerializeInt(ref p.A, -100, 100)) { return false; }
        if (!s.SerializeInt(ref p.B, 0, 65535)) { return false; }
        if (!s.SerializeInt(ref p.C, -1000000, 1000000)) { return false; }
        if (!s.SerializeBits(ref p.Bits7, 7)) { return false; }
        if (!s.SerializeBits(ref p.Bits13, 13)) { return false; }
        if (!s.SerializeBits(ref p.Bits23, 23)) { return false; }
        if (!s.SerializeBool(ref p.Flag)) { return false; }
        if (!s.SerializeFloat(ref p.X)) { return false; }
        if (!s.SerializeFloat(ref p.Y)) { return false; }
        if (!s.SerializeFloat(ref p.Z)) { return false; }
        if (!s.SerializeUInt64(ref p.Big)) { return false; }
        if (!s.SerializeBytes(p.Blob)) { return false; }    // aligns internally — the schema says `align` out loud
        return true;
    }

    static bool ReadRtPacket(ReadStream s, RtBenchPacket p)
    {
        if (!s.SerializeInt(ref p.A, -100, 100)) { return false; }
        if (!s.SerializeInt(ref p.B, 0, 65535)) { return false; }
        if (!s.SerializeInt(ref p.C, -1000000, 1000000)) { return false; }
        if (!s.SerializeBits(ref p.Bits7, 7)) { return false; }
        if (!s.SerializeBits(ref p.Bits13, 13)) { return false; }
        if (!s.SerializeBits(ref p.Bits23, 23)) { return false; }
        if (!s.SerializeBool(ref p.Flag)) { return false; }
        if (!s.SerializeFloat(ref p.X)) { return false; }
        if (!s.SerializeFloat(ref p.Y)) { return false; }
        if (!s.SerializeFloat(ref p.Z)) { return false; }
        if (!s.SerializeUInt64(ref p.Big)) { return false; }
        if (!s.SerializeBytes(p.Blob)) { return false; }
        return true;
    }

    internal sealed class RtBenchInts
    {
        public int F0, F1, F2, F3, F4, F5, F6, F7, F8, F9;
    }

    static bool WriteRtInts(WriteStream s, RtBenchInts f)
    {
        if (!s.SerializeInt(ref f.F0, -100, 100)) { return false; }
        if (!s.SerializeInt(ref f.F1, 0, 65535)) { return false; }
        if (!s.SerializeInt(ref f.F2, -1000000, 1000000)) { return false; }
        if (!s.SerializeInt(ref f.F3, 0, 3)) { return false; }
        if (!s.SerializeInt(ref f.F4, -15, 15)) { return false; }
        if (!s.SerializeInt(ref f.F5, 0, 1000)) { return false; }
        if (!s.SerializeInt(ref f.F6, -2048, 2047)) { return false; }
        if (!s.SerializeInt(ref f.F7, 0, 255)) { return false; }
        if (!s.SerializeInt(ref f.F8, -600000, 600000)) { return false; }
        if (!s.SerializeInt(ref f.F9, 0, 100)) { return false; }
        return true;
    }

    static bool ReadRtInts(ReadStream s, RtBenchInts f)
    {
        if (!s.SerializeInt(ref f.F0, -100, 100)) { return false; }
        if (!s.SerializeInt(ref f.F1, 0, 65535)) { return false; }
        if (!s.SerializeInt(ref f.F2, -1000000, 1000000)) { return false; }
        if (!s.SerializeInt(ref f.F3, 0, 3)) { return false; }
        if (!s.SerializeInt(ref f.F4, -15, 15)) { return false; }
        if (!s.SerializeInt(ref f.F5, 0, 1000)) { return false; }
        if (!s.SerializeInt(ref f.F6, -2048, 2047)) { return false; }
        if (!s.SerializeInt(ref f.F7, 0, 255)) { return false; }
        if (!s.SerializeInt(ref f.F8, -600000, 600000)) { return false; }
        if (!s.SerializeInt(ref f.F9, 0, 100)) { return false; }
        return true;
    }

    internal sealed class RtBenchBits
    {
        public uint B7, B13, B23, B3, B32, B11, B19;
        public ulong B48;
    }

    static bool WriteRtBits(WriteStream s, RtBenchBits f)
    {
        if (!s.SerializeBits(ref f.B7, 7)) { return false; }
        if (!s.SerializeBits(ref f.B13, 13)) { return false; }
        if (!s.SerializeBits(ref f.B23, 23)) { return false; }
        if (!s.SerializeBits(ref f.B3, 3)) { return false; }
        if (!s.SerializeBits(ref f.B32, 32)) { return false; }
        if (!s.SerializeBits(ref f.B11, 11)) { return false; }
        if (!s.SerializeBits(ref f.B19, 19)) { return false; }
        if (!s.SerializeBits64(ref f.B48, 48)) { return false; }
        return true;
    }

    static bool ReadRtBits(ReadStream s, RtBenchBits f)
    {
        if (!s.SerializeBits(ref f.B7, 7)) { return false; }
        if (!s.SerializeBits(ref f.B13, 13)) { return false; }
        if (!s.SerializeBits(ref f.B23, 23)) { return false; }
        if (!s.SerializeBits(ref f.B3, 3)) { return false; }
        if (!s.SerializeBits(ref f.B32, 32)) { return false; }
        if (!s.SerializeBits(ref f.B11, 11)) { return false; }
        if (!s.SerializeBits(ref f.B19, 19)) { return false; }
        if (!s.SerializeBits64(ref f.B48, 48)) { return false; }
        return true;
    }

    internal sealed class RtBenchMixed
    {
        public int Sequence;
        public uint AckBits, EntityId;
        public int PosX, PosY, PosZ;
        public uint Yaw;
        public bool Moving, Firing;
        public ulong Timestamp;
        public int Weapon;
    }

    static bool WriteRtMixed(WriteStream s, RtBenchMixed f)
    {
        if (!s.SerializeInt(ref f.Sequence, 0, 65535)) { return false; }
        if (!s.SerializeBits(ref f.AckBits, 32)) { return false; }
        if (!s.SerializeBits(ref f.EntityId, 12)) { return false; }
        if (!s.SerializeInt(ref f.PosX, -16384, 16383)) { return false; }
        if (!s.SerializeInt(ref f.PosY, -16384, 16383)) { return false; }
        if (!s.SerializeInt(ref f.PosZ, -16384, 16383)) { return false; }
        if (!s.SerializeBits(ref f.Yaw, 9)) { return false; }
        if (!s.SerializeBool(ref f.Moving)) { return false; }
        if (!s.SerializeBool(ref f.Firing)) { return false; }
        if (!s.SerializeBits64(ref f.Timestamp, 48)) { return false; }
        if (!s.SerializeInt(ref f.Weapon, 0, 15)) { return false; }
        return true;
    }

    static bool ReadRtMixed(ReadStream s, RtBenchMixed f)
    {
        if (!s.SerializeInt(ref f.Sequence, 0, 65535)) { return false; }
        if (!s.SerializeBits(ref f.AckBits, 32)) { return false; }
        if (!s.SerializeBits(ref f.EntityId, 12)) { return false; }
        if (!s.SerializeInt(ref f.PosX, -16384, 16383)) { return false; }
        if (!s.SerializeInt(ref f.PosY, -16384, 16383)) { return false; }
        if (!s.SerializeInt(ref f.PosZ, -16384, 16383)) { return false; }
        if (!s.SerializeBits(ref f.Yaw, 9)) { return false; }
        if (!s.SerializeBool(ref f.Moving)) { return false; }
        if (!s.SerializeBool(ref f.Firing)) { return false; }
        if (!s.SerializeBits64(ref f.Timestamp, 48)) { return false; }
        if (!s.SerializeInt(ref f.Weapon, 0, 15)) { return false; }
        return true;
    }

    // ---- pinned instances: test/bench/main.cpp (the golden producer), verbatim ----

    static RtBenchPacket PinRtPacket()
    {
        RtBenchPacket p = new RtBenchPacket
        {
            A = -37, B = 12345, C = 987654,
            Bits7 = 97, Bits13 = 5000, Bits23 = 1234567,
            Flag = true,
            X = 1.5f, Y = -3.25f, Z = 100.125f,
            Big = 0x123456789ABCDEF0,
        };
        for (int i = 0; i < 17; i++)
        {
            p.Blob[i] = (byte)(i * 31);
        }
        return p;
    }

    static RtBenchInts PinRtInts() => new RtBenchInts
    {
        F0 = -37, F1 = 12345, F2 = 987654, F3 = 2, F4 = -15,
        F5 = 777, F6 = -2048, F7 = 200, F8 = -543210, F9 = 99,
    };

    static RtBenchBits PinRtBits() => new RtBenchBits
    {
        B7 = 97, B13 = 5000, B23 = 1234567, B3 = 5,
        B32 = 0xDEADBEEF, B11 = 1024, B19 = 333333, B48 = 0xFEDCBA987654,
    };

    static RtBenchMixed PinRtMixed() => new RtBenchMixed
    {
        Sequence = 52428, AckBits = 0xA5A5A5A5, EntityId = 2049,
        PosX = -16384, PosY = 16383, PosZ = -1,
        Yaw = 511, Moving = true, Firing = false,
        Timestamp = 0x123456789ABC, Weapon = 15,
    };

    // ---- vary functions: bench/cpp/bench_main.cpp's rt mappings exactly ----

    static void VaryRtPacket(RtBenchPacket p, ulong rng)
    {
        p.A = (int)((rng >> 8) & 63) - 32;
        p.B = (int)((uint)(rng >> 16) & 65535);
        p.C = (int)((rng >> 24) & 0xFFFFF) - 500000;
        p.Bits7 = (uint)rng & 127;
        p.Bits13 = (uint)(rng >> 3) & 8191;
        p.Bits23 = (uint)(rng >> 5) & 8388607;
        p.Flag = (rng & 1) != 0;
        p.X = (uint)rng & 0xFFFF;
        p.Big = rng;
        p.Blob[0] = (byte)(rng >> 32);
    }

    static void VaryRtInts(RtBenchInts f, ulong rng)
    {
        f.F0 = (int)((rng >> 8) & 63) - 32;
        f.F1 = (int)((uint)(rng >> 16) & 65535);
        f.F2 = (int)((rng >> 24) & 0xFFFFF) - 500000;
        f.F3 = (int)((uint)(rng >> 2) & 3);
        f.F4 = (int)((rng >> 11) & 15) - 8;
        f.F5 = (int)((uint)(rng >> 22) & 511);
        f.F6 = (int)((rng >> 33) & 2047) - 1024;
        f.F7 = (int)((uint)(rng >> 40) & 255);
        f.F8 = (int)((rng >> 30) & 0xFFFFF) - 500000;
        f.F9 = (int)((uint)(rng >> 57) & 63);
    }

    static void VaryRtBits(RtBenchBits f, ulong rng)
    {
        f.B7 = (uint)rng & 127;
        f.B13 = (uint)(rng >> 3) & 8191;
        f.B23 = (uint)(rng >> 5) & 8388607;
        f.B3 = (uint)(rng >> 29) & 7;
        f.B32 = (uint)(rng >> 16);
        f.B11 = (uint)(rng >> 37) & 2047;
        f.B19 = (uint)(rng >> 44) & 524287;
        f.B48 = rng & 0xFFFFFFFFFFFF;
    }

    static void VaryRtMixed(RtBenchMixed f, ulong rng)
    {
        f.Sequence = (int)((uint)(rng >> 8) & 65535);
        f.AckBits = (uint)(rng >> 16);
        f.EntityId = (uint)rng & 4095;
        f.PosX = (int)((rng >> 20) & 32767) - 16384;
        f.PosY = (int)((rng >> 25) & 32767) - 16384;
        f.PosZ = (int)((rng >> 30) & 32767) - 16384;
        f.Yaw = (uint)(rng >> 3) & 511;
        f.Moving = (rng & 1) != 0;
        f.Firing = (rng & 2) != 0;
        f.Timestamp = rng & 0xFFFFFFFFFFFF;
        f.Weapon = (int)((uint)(rng >> 60) & 15);
    }

    // ---- the single untimed call sites (§3.2), one pair per shape ----

    static long RtOnceWritePacket(RtBenchPacket p, byte[] buf)
    {
        WriteStream ws = new WriteStream(buf);
        if (!WriteRtPacket(ws, p)) { return -1; }
        ws.Flush();
        return ws.BytesProcessed;
    }

    static bool RtOnceReadPacket(RtBenchPacket p, byte[] buf, int bytes)
    {
        ReadStream rs = new ReadStream(buf, bytes);
        return ReadRtPacket(rs, p);
    }

    static long RtOnceWriteInts(RtBenchInts f, byte[] buf)
    {
        WriteStream ws = new WriteStream(buf);
        if (!WriteRtInts(ws, f)) { return -1; }
        ws.Flush();
        return ws.BytesProcessed;
    }

    static bool RtOnceReadInts(RtBenchInts f, byte[] buf, int bytes)
    {
        ReadStream rs = new ReadStream(buf, bytes);
        return ReadRtInts(rs, f);
    }

    static long RtOnceWriteBits(RtBenchBits f, byte[] buf)
    {
        WriteStream ws = new WriteStream(buf);
        if (!WriteRtBits(ws, f)) { return -1; }
        ws.Flush();
        return ws.BytesProcessed;
    }

    static bool RtOnceReadBits(RtBenchBits f, byte[] buf, int bytes)
    {
        ReadStream rs = new ReadStream(buf, bytes);
        return ReadRtBits(rs, f);
    }

    static long RtOnceWriteMixed(RtBenchMixed f, byte[] buf)
    {
        WriteStream ws = new WriteStream(buf);
        if (!WriteRtMixed(ws, f)) { return -1; }
        ws.Flush();
        return ws.BytesProcessed;
    }

    static bool RtOnceReadMixed(RtBenchMixed f, byte[] buf, int bytes)
    {
        ReadStream rs = new ReadStream(buf, bytes);
        return ReadRtMixed(rs, f);
    }

    // ---- the timed loops, one JIT'd symbol per (shape, path) so the §4.1
    // JitDisasm verdict has a loop body to count. Streams reuse via Reset —
    // the runtime's documented no-allocation path, as the gen benches do. ----

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchPacketWriteLoop(RtBenchPacket baseValue, long iters, ref ulong rng)
    {
        WriteStream ws = new WriteStream(gBuffer);
        for (long i = 0; i < iters; i++)
        {
            rng = BenchRng(rng);
            VaryRtPacket(baseValue, rng);
            ws.Reset(gBuffer);
            if (!WriteRtPacket(ws, baseValue)) { return false; }
            ws.Flush();
            gSink = gSink + (ulong)ws.BytesProcessed;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchPacketReadLoop(RtBenchPacket outValue, long iters, int bytesPerOp)
    {
        ReadStream rs = new ReadStream(gVariants[0], bytesPerOp);
        for (long i = 0; i < iters; i++)
        {
            rs.Reset(gVariants[i & (NumVariants - 1)], bytesPerOp);
            if (!ReadRtPacket(rs, outValue)) { return false; }
            gSink = gSink + 1;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchIntsWriteLoop(RtBenchInts baseValue, long iters, ref ulong rng)
    {
        WriteStream ws = new WriteStream(gBuffer);
        for (long i = 0; i < iters; i++)
        {
            rng = BenchRng(rng);
            VaryRtInts(baseValue, rng);
            ws.Reset(gBuffer);
            if (!WriteRtInts(ws, baseValue)) { return false; }
            ws.Flush();
            gSink = gSink + (ulong)ws.BytesProcessed;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchIntsReadLoop(RtBenchInts outValue, long iters, int bytesPerOp)
    {
        ReadStream rs = new ReadStream(gVariants[0], bytesPerOp);
        for (long i = 0; i < iters; i++)
        {
            rs.Reset(gVariants[i & (NumVariants - 1)], bytesPerOp);
            if (!ReadRtInts(rs, outValue)) { return false; }
            gSink = gSink + 1;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchBitsWriteLoop(RtBenchBits baseValue, long iters, ref ulong rng)
    {
        WriteStream ws = new WriteStream(gBuffer);
        for (long i = 0; i < iters; i++)
        {
            rng = BenchRng(rng);
            VaryRtBits(baseValue, rng);
            ws.Reset(gBuffer);
            if (!WriteRtBits(ws, baseValue)) { return false; }
            ws.Flush();
            gSink = gSink + (ulong)ws.BytesProcessed;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchBitsReadLoop(RtBenchBits outValue, long iters, int bytesPerOp)
    {
        ReadStream rs = new ReadStream(gVariants[0], bytesPerOp);
        for (long i = 0; i < iters; i++)
        {
            rs.Reset(gVariants[i & (NumVariants - 1)], bytesPerOp);
            if (!ReadRtBits(rs, outValue)) { return false; }
            gSink = gSink + 1;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchMixedWriteLoop(RtBenchMixed baseValue, long iters, ref ulong rng)
    {
        WriteStream ws = new WriteStream(gBuffer);
        for (long i = 0; i < iters; i++)
        {
            rng = BenchRng(rng);
            VaryRtMixed(baseValue, rng);
            ws.Reset(gBuffer);
            if (!WriteRtMixed(ws, baseValue)) { return false; }
            ws.Flush();
            gSink = gSink + (ulong)ws.BytesProcessed;
        }
        return true;
    }

    [MethodImpl(MethodImplOptions.NoInlining)]
    static bool RtBenchMixedReadLoop(RtBenchMixed outValue, long iters, int bytesPerOp)
    {
        ReadStream rs = new ReadStream(gVariants[0], bytesPerOp);
        for (long i = 0; i < iters; i++)
        {
            rs.Reset(gVariants[i & (NumVariants - 1)], bytesPerOp);
            if (!ReadRtMixed(rs, outValue)) { return false; }
            gSink = gSink + 1;
        }
        return true;
    }

    // ---- the family rt driver: §1.5 oracle gate, then the timed loops ----

    delegate long RtOnceWrite<T>(T msg, byte[] buf);
    delegate bool RtOnceRead<T>(T msg, byte[] buf, int bytes);
    delegate bool RtWriteLoop<T>(T baseValue, long iters, ref ulong rng);
    delegate bool RtReadLoop<T>(T outValue, long iters, int bytesPerOp);

    static void BenchRt<T>(string name, long iters, T pinned,
        RtOnceWrite<T> onceWrite, RtOnceRead<T> onceRead,
        RtWriteLoop<T> writeLoop, RtReadLoop<T> readLoop,
        Action<T, ulong> vary)
        where T : class, new()
    {
        // oracle 1: the hand-written wire must equal the generated-code golden
        T baseValue = pinned;
        long bytesPerOp = onceWrite(baseValue, gBuffer);
        if (bytesPerOp < 0)
        {
            Fail(name, "write of pinned instance failed");
            return;
        }
        if (!CheckGolden(name, gBuffer.AsSpan(0, (int)bytesPerOp)))
        {
            failed = true;
            return;
        }

        // oracle 2: round-trip write -> read -> re-write -> identical bytes
        T outValue = new T();
        if (!onceRead(outValue, gBuffer, (int)bytesPerOp))
        {
            Fail(name, "read of pinned instance failed");
            return;
        }
        if (onceWrite(outValue, gTwin) != bytesPerOp
            || !gTwin.AsSpan(0, (int)bytesPerOp).SequenceEqual(gBuffer.AsSpan(0, (int)bytesPerOp)))
        {
            Fail(name, "round-trip bytes differ");
            return;
        }

        // variant buffers (and proof that variation keeps bytes/op constant)
        ulong rng = 1;
        for (int k = 0; k < NumVariants; k++)
        {
            rng = BenchRng(rng);
            vary(baseValue, rng);
            if (onceWrite(baseValue, gVariants[k]) != bytesPerOp)
            {
                Fail(name, "variation changed bytes/op — vary must keep structure fields fixed");
                return;
            }
        }

        double[] writeRates = new double[gNumRuns];
        double[] readRates = new double[gNumRuns];

        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            if (!writeLoop(baseValue, iters, ref rng))
            {
                Fail(name, "write failed in loop");
                return;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                writeRates[run] = iters / time;
            }
        }

        for (int run = -1; run < gNumRuns; run++)
        {
            Stopwatch sw = Stopwatch.StartNew();
            if (!readLoop(outValue, iters, (int)bytesPerOp))
            {
                Fail(name, "read failed in loop");
                return;
            }
            double time = sw.Elapsed.TotalSeconds;
            if (run >= 0)
            {
                readRates[run] = iters / time;
            }
        }
        GC.KeepAlive(outValue); // every decoded field is observed

        Report(name, "write", iters, bytesPerOp, Stats(writeRates), "rt");
        Report(name, "read", iters, bytesPerOp, Stats(readRates), "rt");
    }

    // iteration counts fixed and identical across all five runners (§2.1;
    // sized in the C++ reference)
    static void BenchRtAll()
    {
        BenchRt("bench_packet", 32000000, PinRtPacket(), RtOnceWritePacket, RtOnceReadPacket, RtBenchPacketWriteLoop, RtBenchPacketReadLoop, VaryRtPacket);
        BenchRt("bench_ints", 40000000, PinRtInts(), RtOnceWriteInts, RtOnceReadInts, RtBenchIntsWriteLoop, RtBenchIntsReadLoop, VaryRtInts);
        BenchRt("bench_bits", 48000000, PinRtBits(), RtOnceWriteBits, RtOnceReadBits, RtBenchBitsWriteLoop, RtBenchBitsReadLoop, VaryRtBits);
        BenchRtMixed();
    }

    // the --quick leg: bench_mixed alone (golden-gated by BenchRt like every leg)
    static void BenchRtMixed()
    {
        BenchRt("bench_mixed", 40000000, PinRtMixed(), RtOnceWriteMixed, RtOnceReadMixed, RtBenchMixedWriteLoop, RtBenchMixedReadLoop, VaryRtMixed);
    }

    // ------------------------------------------------------------------------------------------
    // family bits (§1.4)
    // ------------------------------------------------------------------------------------------

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
