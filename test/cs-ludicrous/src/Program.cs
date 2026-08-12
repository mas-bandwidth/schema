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
        input.Probe.Position = -809119744L;                                          // -12345.25 * 2^16
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
        Check(MessageMaxBits == 1206, "message-level bound");
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

        // ---- the message dispatch surface over the new unit ----
        {
            LudicrousState input = MakeState();
            WriteStream ws = NewWriteStream();
            Check(WriteMessage(ws, input), "write Message LudicrousState");
            byte[] wire = Data(ws);

            MessageStorage storage = new MessageStorage();
            ReadStream rs = new ReadStream(wire);
            Check(ReadMessage(rs, storage, out Message m), "read Message LudicrousState");
            LudicrousState output = m as LudicrousState;
            Check(output != null, "the message is the LudicrousState");
            if (output != null)
            {
                Check(output.Wide.Flux == input.Wide.Flux, "flux rides the dispatch surface");
                Check(output.Probe.Angle == 2981888, "angle rides the dispatch surface");
            }
        }

        if (failed)
        {
            return 1;
        }
        Console.WriteLine("OK");
        return 0;
    }
}
