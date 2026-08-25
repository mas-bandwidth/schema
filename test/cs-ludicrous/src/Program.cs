// The C# cross-language wire test for the fixed-point + 128-bit unit
// (examples128/): the generated C# writes the SAME pinned instance
// test/ludicrous_main.cpp pins in testdata/wire/ludicrous_state*.bin and
// byte-compares against those files — cross-language wire identity for the
// serialize-phase-1 families (fixed(I, F), int128, uint128) is the §7.2 gate
// this binary carries. Plus round-trips through the C# reader, the §5
// branch-zeroing check over a 128-bit field, the specified-defaults checks
// (through the emulated Int128Value/UInt128Value pair — including one default
// no long literal can spell), and the hostile-read rejections the C++ test
// carries (reject, never clamp — STANDARD.md).
//
// Prints OK and exits 0, exactly like its C++, Go and Rust twins. Run from
// test/cs-ludicrous (the Makefile does): the wire goldens are at
// ../../testdata/wire; a fallback resolves them from the build output
// directory so `dotnet run` stays honest whatever the working directory.
//
// Mirrors test/ludicrous_main.cpp block for block.

using System;
using System.IO;
using Ludicrous;
using Serialize;
using static Ludicrous.Schema;

static class Program
{
    static bool failed;
    static string wireDir;

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed = true;
        }
    }

    // GoldenWire byte-compares written wire against the C++-pinned golden.
    static void GoldenWire(string name, ReadOnlySpan<byte> data)
    {
        byte[] golden;
        try
        {
            golden = File.ReadAllBytes(Path.Combine(wireDir, name + ".bin"));
        }
        catch (Exception e)
        {
            Console.WriteLine("FAILED: read wire golden " + name + ": " + e.Message);
            failed = true;
            return;
        }
        Check(data.SequenceEqual(golden), "wire golden " + name + " — C# bytes must equal the C++-pinned bytes");
    }

    static string FindTestDataDir(string kind)
    {
        string[] candidates =
        {
            Path.Combine("..", "..", "testdata", kind),
            Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "testdata", kind),
        };
        foreach (string candidate in candidates)
        {
            if (Directory.Exists(candidate))
            {
                return candidate;
            }
        }
        return candidates[0];
    }

    static WriteStream NewWriteStream()
    {
        return new WriteStream(new byte[256]);
    }

    // Data flushes the stream and copies the written wire out.
    static byte[] Data(WriteStream ws)
    {
        ws.Flush();
        return ws.Data.ToArray();
    }

    // SetBits forces bits [pos, pos + n) of the stream image to 1 — the
    // hostile reader's tool: smuggling an offset into the range's bit
    // headroom, which a read must REJECT, never clamp (STANDARD.md).
    static void SetBits(byte[] data, int pos, int n)
    {
        for (int i = pos; i < pos + n; i++)
        {
            data[i / 8] |= (byte)(1 << (i % 8));
        }
    }

    // MakeState is test/ludicrous_main.cpp's make_state — the values must
    // stay mirrored on both sides.
    static LudicrousState MakeState()
    {
        LudicrousState input = new LudicrousState();
        input.Mode = DriveMode.Ludicrous;
        input.Probe.Angle = 2981888;                                                 // +45.5 * 2^16
        input.Probe.Position = -809119744L;                                          // -12346.1875 * 2^16
        input.Probe.Reach = 65536000000L - 1;                                        // raw_max - 1
        input.Probe.Ticks = 777777;
        input.Probe.Samples[0] = -524288;                                            // raw_min
        input.Probe.Samples[1] = 524288;                                             // raw_max
        input.Wide.EntityId = new UInt128Value(0x0123456789ABCDEFul, 0xFEDCBA9876543210ul);
        input.Wide.Energy = 4999999999L;
        input.Wide.Flux = new Int128Value(0x800000000ul, 7ul); // 2^99 + 7
        // wide.bias and wide.seed stay at their SPECIFIED DEFAULTS (-250 and
        // 2^65) — construction installs them, and they ride the wire as written
        input.KeysCount = 2;
        input.Keys[0] = 1;
        input.Keys[1] = new UInt128Value(0x8000000000000000ul, 0ul); // 1 << 127
        input.HasTarget = true;
        input.TargetId = 42;
        return input;
    }

    static int Main()
    {
        wireDir = FindTestDataDir("wire");

        // worst-case bounds, hand-derived (SPEC §6.1 item 4) — the same
        // numbers test/ludicrous_main.cpp static_asserts
        Check(FixedProbeMaxBits == 156, "FixedProbe worst case");
        Check(WideProbeMaxBits == 403, "WideProbe worst case");
        Check(LudicrousStateMaxBits == 1205, "LudicrousState worst case");
        Check(ProtocolId != 0, "the unit has a protocol id");

        // zero initialization with specified defaults (SPEC §4.2),
        // sentinel-zero composition: a fresh state starts at DriveMode.None —
        // the null rides in-band — and the two defaulted 128-bit fields
        // construct to their declared values, one of which no long literal
        // can spell
        {
            LudicrousState zero = new LudicrousState();
            Check(zero.Mode == DriveMode.None, "a fresh state starts at DriveMode None");
            Check(zero.Probe.Reach == Int128Value.Zero, "reach starts zero");
            Check(zero.Wide.EntityId == UInt128Value.Zero, "entity_id starts zero");
            Check(zero.Wide.Bias == -250, "bias defaults -250");
            Check(zero.Wide.Seed == new UInt128Value(0x2ul, 0x0ul), "seed defaults 2^65");
            Check(zero.KeysCount == 0, "keys start empty");
            Check(zero.TargetId == UInt128Value.Zero, "target_id starts zero");
            // Zero* gives the §5 zero form — the defaults are construction's,
            // not the zero form's (SPEC §4.2, the C# column)
            ZeroLudicrousState(zero);
            Check(zero.Wide.Bias == Int128Value.Zero, "the §5 zero form zeroes the defaults");
        }

        byte[] takenWire = null;

        // ---- the taken-branch wire: generated bytes == the C++-pinned golden ----
        {
            LudicrousState input = MakeState();
            WriteStream ws = NewWriteStream();
            Check(WriteLudicrousState(ws, input), "write LudicrousState");
            takenWire = Data(ws);
            GoldenWire("ludicrous_state", takenWire);

            LudicrousState output = new LudicrousState();
            ReadStream rs = new ReadStream(takenWire);
            Check(ReadLudicrousState(rs, output), "read LudicrousState");
            Check(output.Mode == DriveMode.Ludicrous, "mode round-trips");
            Check(output.Probe.Angle == input.Probe.Angle, "angle round-trips");
            Check(output.Probe.Position == input.Probe.Position, "position round-trips");
            Check(output.Probe.Reach == input.Probe.Reach, "reach round-trips");
            Check(output.Probe.Ticks == input.Probe.Ticks, "ticks round-trips");
            Check(output.Probe.Samples[0] == input.Probe.Samples[0]
                && output.Probe.Samples[1] == input.Probe.Samples[1], "samples round-trip");
            Check(output.Wide.EntityId == input.Wide.EntityId, "entity_id round-trips");
            Check(output.Wide.Energy == input.Wide.Energy, "energy round-trips");
            Check(output.Wide.Flux == input.Wide.Flux, "flux round-trips");
            Check(output.Wide.Bias == -250, "the bias default rides the wire");
            Check(output.Wide.Seed == new UInt128Value(0x2ul, 0x0ul), "the seed default rides the wire");
            Check(output.KeysCount == 2, "keys_count round-trips");
            Check(output.Keys[0] == input.Keys[0] && output.Keys[1] == input.Keys[1], "keys round-trip");
            Check(output.HasTarget && output.TargetId == 42, "the taken branch round-trips");
        }

        // ---- the untaken branch: identical prefix, and the 128-bit field
        // under it reads back ZERO into a dirty object (SPEC §5) ----
        {
            LudicrousState input = MakeState();
            input.HasTarget = false;
            WriteStream ws = NewWriteStream();
            Check(WriteLudicrousState(ws, input), "write LudicrousState untargeted");
            byte[] wire = Data(ws);
            GoldenWire("ludicrous_state_untargeted", wire);

            LudicrousState output = new LudicrousState();
            output.TargetId = 0xDEAD; // dirty — the read must zero it
            ReadStream rs = new ReadStream(wire);
            Check(ReadLudicrousState(rs, output), "read LudicrousState untargeted");
            Check(!output.HasTarget, "has_target reads false");
            Check(output.TargetId == UInt128Value.Zero, "the untaken 128-bit field reads as zero (SPEC §5)");
        }

        // ---- hostile reads REJECT, never clamp (STANDARD.md, SPEC §5) ----
        {
            // fixed: angle's 25 offset bits start at bit 2; all-ones =
            // 33554431, above the raw range 360 * 2^16 = 23592960
            byte[] hostile = (byte[])takenWire.Clone();
            SetBits(hostile, 2, 25);
            LudicrousState output = new LudicrousState();
            ReadStream rs = new ReadStream(hostile);
            Check(!ReadLudicrousState(rs, output), "a smuggled fixed offset is REJECTED");
        }
        {
            // int128: energy's 34 offset bits start at bit 286 (2+156+128);
            // all-ones = 2^34 - 1 = 17179869183, above the range 10^10
            byte[] hostile = (byte[])takenWire.Clone();
            SetBits(hostile, 286, 34);
            LudicrousState output = new LudicrousState();
            ReadStream rs = new ReadStream(hostile);
            Check(!ReadLudicrousState(rs, output), "a smuggled int128 offset is REJECTED");
        }
        {
            // truncation: running out of input mid-read is a read failure (SPEC §5)
            LudicrousState output = new LudicrousState();
            ReadStream rs = new ReadStream(new ReadOnlySpan<byte>(takenWire, 0, 4).ToArray());
            Check(!ReadLudicrousState(rs, output), "a truncated stream is a read failure");
        }

        // ---- DegenerateProbe: min == max costs ZERO bits (SPEC §4.6) ----
        // The whole wire is the tail byte; a port that emits ANY bits for a
        // degenerate range shifts it and fails the golden compare.
        {
            Check(DegenerateProbeMaxBits == 8, "three degenerate fields cost zero bits");

            DegenerateProbe input = new DegenerateProbe();
            input.LockedFixed = -196608; // -3 * 2^16, the ONE legal raw
            input.LockedInt = 7;
            input.LockedWide = -12345678901234L;
            input.Tail = 0xA5;

            WriteStream ws = NewWriteStream();
            Check(WriteDegenerateProbe(ws, input), "write DegenerateProbe");
            byte[] wire = Data(ws);
            GoldenWire("degenerate_probe", wire);

            DegenerateProbe output = new DegenerateProbe();
            ReadStream rs = new ReadStream(wire);
            Check(ReadDegenerateProbe(rs, output), "read DegenerateProbe");
            Check(output.LockedFixed == -196608 && output.LockedInt == 7, "degenerate values materialize from the range alone");
            Check(output.LockedWide == (Int128Value)(-12345678901234L), "degenerate int128 materializes");
            Check(output.Tail == 0xA5, "the tail rides at bit 0");
        }

        // ---- UnsignedProbe: ufixed(I, F), the unsigned sibling (SPEC §4.3;
        //) ----
        // Span's raw value fills ulong's HIGH HALF (above 2^63) — the
        // unsigned SerializeFixed overloads carry it natively, and the
        // C++-pinned golden is the gate. Values mirror
        // test/ludicrous_main.cpp block for block.
        {
            Check(UnsignedProbeMaxBits == 196, "UnsignedProbe worst case");

            UnsignedProbe input = new UnsignedProbe();
            input.Angle = 2981888;                             // +45.5 * 2^16
            input.Span = 0xFFFFFFFFFFFF0000UL;                 // raw_max — the ulong HIGH HALF
            input.Reach = new UInt128Value(0UL, 131071999999UL); // raw_max - 1
            input.Ticks = 777777;
            input.Samples[0] = 0;                              // raw_min
            input.Samples[1] = 1048576;                        // raw_max
            input.Locked = 196608;                             // 3 * 2^16, the ONE legal raw
            input.Tail = 0xA5;

            WriteStream ws = NewWriteStream();
            Check(WriteUnsignedProbe(ws, input), "write UnsignedProbe");
            byte[] uWire = Data(ws);
            GoldenWire("unsigned_probe", uWire);

            UnsignedProbe output = new UnsignedProbe();
            ReadStream rs = new ReadStream(uWire);
            Check(ReadUnsignedProbe(rs, output), "read UnsignedProbe");
            Check(output.Angle == 2981888, "angle round-trips");
            Check(output.Span == 0xFFFFFFFFFFFF0000UL, "the ulong high half round-trips bit-exact");
            Check(output.Reach == new UInt128Value(0UL, 131071999999UL), "reach round-trips");
            Check(output.Ticks == 777777, "ticks round-trips");
            Check(output.Samples[0] == 0 && output.Samples[1] == 1048576, "samples round-trip");
            Check(output.Locked == 196608, "the degenerate ufixed materializes from the range alone");
            Check(output.Tail == 0xA5, "the tail rides after zero degenerate bits");

            // the write-side degenerate refusal: any raw but 3 * 2^16 is
            // refused before a single bit is written
            UnsignedProbe bad = new UnsignedProbe();
            bad.Angle = input.Angle; bad.Span = input.Span; bad.Reach = input.Reach;
            bad.Ticks = input.Ticks; bad.Samples[0] = input.Samples[0]; bad.Samples[1] = input.Samples[1];
            bad.Locked = 196609; bad.Tail = input.Tail;
            WriteStream badWs = NewWriteStream();
            Check(!WriteUnsignedProbe(badWs, bad), "a wrong degenerate ufixed raw is REFUSED on write");

            // hostile: span's 64 offset bits (starting at bit 25) all-ones =
            // 2^64 - 1, above the raw range 0xFFFFFFFFFFFF0000 — the
            // headroom is exactly the low 16 bits, and the reject must fire
            // in the UNSIGNED domain (a signed compare would call the
            // smuggled value negative)
            byte[] hostile = (byte[])uWire.Clone();
            SetBits(hostile, 25, 64);
            UnsignedProbe hOut = new UnsignedProbe();
            ReadStream hRs = new ReadStream(hostile);
            Check(!ReadUnsignedProbe(hRs, hOut), "a smuggled ufixed high-half offset is REJECTED");
        }

        if (failed)
        {
            return 1;
        }
        Console.WriteLine("OK");
        return 0;
    }
}
