// The Java cross-language wire test for the fixed-point + 128-bit unit
// (examples128/): the generated Java classes write the SAME pinned instance
// test/ludicrous_main.cpp pins in testdata/wire/ludicrous_state*.bin and
// byte-compares against those files — cross-language wire identity for the
// serialize-phase-1 families (fixed(I, F), ufixed(I, F), int128, uint128) is
// the §7.2 gate this leg carries. Plus round-trips, the §5 branch-zeroing
// check over a 128-bit field, the specified-defaults checks (one default no
// 64-bit literal can spell — the emitted pair composition), and the hostile
// reads (reject, never clamp — STANDARD.md).
//
// Prints OK and exits 0. Run from test/java-ludicrous (the Makefile does).
// Both modes run in CI: -ea and default; the wire must be identical in
// both. Mirrors test/ludicrous_main.cpp block for block; 128-bit values
// ride the generated Int128/UInt128 pair.

import ludicrous.Int128;
import ludicrous.Ludicrous;
import ludicrous.UInt128;

public final class Main {
    static boolean failed = false;

    static void check(boolean ok, String what) {
        if (!ok) {
            System.out.println("FAILED: " + what);
            failed = true;
        }
    }

    static final boolean assertsEnabled = detectAsserts();

    static boolean detectAsserts() {
        boolean on = false;
        assert on = true;
        return on;
    }

    static void expectAssert(Runnable fn, String what) {
        if (!assertsEnabled) {
            return;
        }
        try {
            fn.run();
            check(false, what);
        } catch (AssertionError e) {
            // the contract fired — expected
        }
    }

    static byte[] golden(String name) {
        try {
            return java.nio.file.Files.readAllBytes(
                    java.nio.file.Path.of("../../testdata/wire/" + name + ".bin"));
        } catch (java.io.IOException e) {
            throw new RuntimeException(e);
        }
    }

    static boolean bytesEqual(byte[] a, int aLen, byte[] b) {
        if (aLen != b.length) {
            return false;
        }
        return java.util.Arrays.equals(a, 0, aLen, b, 0, b.length);
    }

    static final byte[] writeBuf = new byte[256];

    // setBits forces bits [pos, pos + n) of the stream image to 1 — the
    // hostile reader's tool: smuggling an offset into the range's bit
    // headroom, which a read must REJECT, never clamp (STANDARD.md).
    static void setBits(byte[] data, int pos, int n) {
        for (int i = pos; i < pos + n; i++) {
            data[i >>> 3] |= (byte) (1 << (i & 7));
        }
    }

    // makeState is test/ludicrous_main.cpp's make_state — the values must
    // stay mirrored on both sides.
    static Ludicrous.LudicrousState makeState() {
        final Ludicrous.LudicrousState inp = new Ludicrous.LudicrousState();
        inp.mode = Ludicrous.DriveMode.ludicrous;
        inp.probe.angle = 2981888; // +45.5 * 2^16
        inp.probe.position = -809119744; // -12346.1875 * 2^16
        inp.probe.reach = Int128.fromLong(65536000000L - 1); // raw_max - 1
        inp.probe.ticks = 777777;
        inp.probe.samples[0] = -524288; // raw_min
        inp.probe.samples[1] = 524288; // raw_max
        inp.wide.entityId = new UInt128(0x0123456789abcdefL, 0xfedcba9876543210L);
        inp.wide.energy = Int128.fromLong(4999999999L);
        inp.wide.flux = new Int128(1L << 35, 7); // 2^99 + 7
        // wide.bias and wide.seed stay at their SPECIFIED DEFAULTS (-250 and
        // 2^65) — construction installs them, and they ride the wire as written
        inp.keysCount = 2;
        inp.keys[0] = new UInt128(0, 1);
        inp.keys[1] = new UInt128(0x8000000000000000L, 0); // 1 << 127
        inp.hasTarget = true;
        inp.targetId = new UInt128(0, 42);
        return inp;
    }

    public static void main(String[] args) {
        // worst-case bounds, hand-derived (SPEC §6.1 item 4) — the same
        // numbers test/ludicrous_main.cpp static_asserts
        check(Ludicrous.fixedProbeMaxBits == 156, "FixedProbe worst case");
        check(Ludicrous.wideProbeMaxBits == 403, "WideProbe worst case");
        check(Ludicrous.ludicrousStateMaxBits == 1205, "LudicrousState worst case");
        check(Ludicrous.protocolId != 0, "the unit has a protocol id");

        // zero initialization with specified defaults (SPEC §4.2), sentinel-
        // zero composition: construction starts at DriveMode None — the null
        // rides in-band — and the two defaulted 128-bit fields construct to
        // their declared values, one of which no 64-bit literal can spell
        {
            final Ludicrous.LudicrousState zero = new Ludicrous.LudicrousState();
            check(zero.mode == Ludicrous.DriveMode.none, "a fresh state starts at DriveMode None");
            check(zero.probe.reach.equals(Int128.zero), "reach starts zero");
            check(zero.wide.entityId.equals(UInt128.zero), "entity_id starts zero");
            check(zero.wide.bias.equals(Int128.fromLong(-250)), "bias defaults -250");
            check(zero.wide.seed.equals(new UInt128(2, 0)), "seed defaults 2^65");
            check(zero.keysCount == 0, "keys start empty");
            check(zero.targetId.equals(UInt128.zero), "target_id starts zero");
            // zero* is the §5 zero form; construction alone installs the defaults
            Ludicrous.zeroLudicrousState(zero);
            check(zero.wide.bias.equals(Int128.zero), "the §5 zero form stays zero");
        }

        // ---- the taken-branch wire: generated bytes == the C++-pinned golden ----
        final byte[] takenWire;
        {
            final Ludicrous.LudicrousState inp = makeState();
            final int n = Ludicrous.writeLudicrousState(inp, writeBuf);
            takenWire = java.util.Arrays.copyOf(writeBuf, n);
            final byte[] g = golden("ludicrous_state");
            check(bytesEqual(takenWire, takenWire.length, g),
                    "wire golden ludicrous_state — Java bytes must equal the C++-pinned bytes");
            check((Ludicrous.measureLudicrousState(inp) + 7) >>> 3 == n, "measure vs written bytes");

            final Ludicrous.LudicrousState out = new Ludicrous.LudicrousState();
            check(Ludicrous.readLudicrousState(out, takenWire, n * 8), "read LudicrousState");
            check(out.mode == Ludicrous.DriveMode.ludicrous, "mode round-trips");
            check(out.probe.angle == inp.probe.angle, "angle round-trips");
            check(out.probe.position == inp.probe.position, "position round-trips");
            check(out.probe.reach.equals(inp.probe.reach), "reach round-trips");
            check(out.probe.ticks == inp.probe.ticks, "ticks round-trips");
            check(out.probe.samples[0] == -524288 && out.probe.samples[1] == 524288, "samples round-trip");
            check(out.wide.entityId.equals(inp.wide.entityId), "entity_id round-trips");
            check(out.wide.energy.equals(inp.wide.energy), "energy round-trips");
            check(out.wide.flux.equals(inp.wide.flux), "flux round-trips");
            check(out.wide.bias.equals(Int128.fromLong(-250)), "the bias default rides");
            check(out.wide.seed.equals(new UInt128(2, 0)), "the seed default rides");
            check(out.keysCount == 2, "keys_count round-trips");
            check(out.keys[0].equals(inp.keys[0]) && out.keys[1].equals(inp.keys[1]), "keys round-trip");
            check(out.hasTarget && out.targetId.equals(new UInt128(0, 42)), "the taken branch round-trips");

            final int n2 = Ludicrous.writeLudicrousState(out, writeBuf);
            check(n2 == n && bytesEqual(writeBuf, n, takenWire),
                    "LudicrousState round-trips to identical bytes");
        }

        // ---- the untaken branch: identical prefix, and the 128-bit field under
        // it reads back ZERO into a dirty object (SPEC §5) ----
        {
            final Ludicrous.LudicrousState inp = makeState();
            inp.hasTarget = false;
            final int n = Ludicrous.writeLudicrousState(inp, writeBuf);
            check(bytesEqual(writeBuf, n, golden("ludicrous_state_untargeted")),
                    "wire golden ludicrous_state_untargeted");

            final Ludicrous.LudicrousState out = new Ludicrous.LudicrousState();
            out.targetId = new UInt128(0, 0xdead); // dirty — the read must zero it
            check(Ludicrous.readLudicrousState(out, java.util.Arrays.copyOf(writeBuf, n), n * 8),
                    "read LudicrousState untargeted");
            check(!out.hasTarget, "has_target reads false");
            check(out.targetId.equals(UInt128.zero), "the untaken 128-bit field reads as zero (SPEC §5)");
        }

        // ---- hostile reads REJECT, never clamp (STANDARD.md, SPEC §5) ----
        {
            // fixed: angle's 25 offset bits start at bit 2; all-ones = 33554431,
            // above the raw range 360 * 2^16 = 23592960
            final byte[] hostile = takenWire.clone();
            setBits(hostile, 2, 25);
            final Ludicrous.LudicrousState out = new Ludicrous.LudicrousState();
            check(!Ludicrous.readLudicrousState(out, hostile, hostile.length * 8),
                    "a smuggled fixed offset is REJECTED");
        }
        {
            // int128: energy's 34 offset bits start at bit 286 (2+156+128);
            // all-ones = 2^34 - 1 = 17179869183, above the range 10^10
            final byte[] hostile = takenWire.clone();
            setBits(hostile, 286, 34);
            final Ludicrous.LudicrousState out = new Ludicrous.LudicrousState();
            check(!Ludicrous.readLudicrousState(out, hostile, hostile.length * 8),
                    "a smuggled int128 offset is REJECTED");
        }
        {
            // truncation: running out of input mid-read is a read failure (SPEC §5)
            final Ludicrous.LudicrousState out = new Ludicrous.LudicrousState();
            final byte[] t = java.util.Arrays.copyOf(takenWire, 4);
            check(!Ludicrous.readLudicrousState(out, t, t.length * 8),
                    "a truncated stream is a read failure");
        }

        // ---- DegenerateProbe: min == max costs ZERO bits (SPEC §4.6) ----
        // The whole wire is the tail byte; a port that emits ANY bits for a
        // degenerate range shifts it and fails the golden compare.
        {
            check(Ludicrous.degenerateProbeMaxBits == 8, "three degenerate fields cost zero bits");

            final Ludicrous.DegenerateProbe inp = new Ludicrous.DegenerateProbe();
            inp.lockedFixed = -196608; // -3 * 2^16, the ONE legal raw
            inp.lockedInt = 7;
            inp.lockedWide = Int128.fromLong(-12345678901234L);
            inp.tail = (byte) 0xa5;

            final int n = Ludicrous.writeDegenerateProbe(inp, writeBuf);
            check(bytesEqual(writeBuf, n, golden("degenerate_probe")), "wire golden degenerate_probe");
            check(Ludicrous.measureDegenerateProbe(inp) == 8, "the degenerate wire is one byte");

            final Ludicrous.DegenerateProbe out = new Ludicrous.DegenerateProbe();
            check(Ludicrous.readDegenerateProbe(out, java.util.Arrays.copyOf(writeBuf, n), n * 8),
                    "read DegenerateProbe");
            check(out.lockedFixed == -196608 && out.lockedInt == 7
                    && out.lockedWide.equals(Int128.fromLong(-12345678901234L))
                    && (out.tail & 0xff) == 0xa5,
                    "DegenerateProbe round-trips — every value materialized from its range");
        }

        // ---- UnsignedProbe: ufixed(I, F), the unsigned sibling (SPEC §4.3) ----
        // span's raw value fills uint64's HIGH HALF (above 2^63) — it rides
        // bit-transparently in long, and the C++-pinned golden is the gate.
        {
            check(Ludicrous.unsignedProbeMaxBits == 196, "UnsignedProbe worst case");

            final Ludicrous.UnsignedProbe inp = new Ludicrous.UnsignedProbe();
            inp.angle = 2981888; // +45.5 * 2^16
            inp.span = 0xffffffffffff0000L; // raw_max — the uint64 HIGH HALF
            inp.reach = UInt128.fromLong(131071999999L); // raw_max - 1
            inp.ticks = 777777;
            inp.samples[0] = 0; // raw_min
            inp.samples[1] = 1048576; // raw_max
            inp.locked = 196608; // 3 * 2^16, the ONE legal raw
            inp.tail = (byte) 0xa5;

            final int n = Ludicrous.writeUnsignedProbe(inp, writeBuf);
            final byte[] uWire = java.util.Arrays.copyOf(writeBuf, n);
            check(bytesEqual(uWire, uWire.length, golden("unsigned_probe")), "wire golden unsigned_probe");

            final Ludicrous.UnsignedProbe out = new Ludicrous.UnsignedProbe();
            check(Ludicrous.readUnsignedProbe(out, uWire, n * 8), "read UnsignedProbe");
            check(out.span == 0xffffffffffff0000L
                    && out.reach.equals(UInt128.fromLong(131071999999L))
                    && out.locked == 196608,
                    "UnsignedProbe round-trips — the uint64 high half bit-exact");

            // the write-side degenerate contract: any raw but 3 * 2^16 trips
            // the writer assert (checked twin)
            expectAssert(() -> {
                final Ludicrous.UnsignedProbe bad = new Ludicrous.UnsignedProbe();
                bad.angle = inp.angle;
                bad.span = inp.span;
                bad.reach = inp.reach;
                bad.ticks = inp.ticks;
                bad.samples[0] = 0;
                bad.samples[1] = 1048576;
                bad.locked = 196609;
                bad.tail = (byte) 0xa5;
                Ludicrous.writeUnsignedProbe(bad, writeBuf);
            }, "a wrong degenerate ufixed raw must trip the writer contract");

            // hostile: span's 64 offset bits (starting at bit 25) all-ones =
            // 2^64 - 1, above the raw range 0xFFFFFFFFFFFF0000 — the headroom
            // is exactly the low 16 bits, and the reject must fire in the
            // UNSIGNED domain (a signed compare would call the smuggled value
            // negative)
            final byte[] hostile = uWire.clone();
            setBits(hostile, 25, 64);
            final Ludicrous.UnsignedProbe hOut = new Ludicrous.UnsignedProbe();
            check(!Ludicrous.readUnsignedProbe(hOut, hostile, hostile.length * 8),
                    "a smuggled ufixed high-half offset is REJECTED");
        }

        if (failed) {
            System.exit(1);
        }
        System.out.println("OK");
    }
}
