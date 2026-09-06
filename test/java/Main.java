// The Java cross-language wire test: the generated Java classes write the
// SAME pinned instances the C++ test pins in testdata/wire/*.bin and
// byte-compare against those files — cross-language wire identity is the
// §7.2 gate this leg carries. Plus round-trips through the Java reader, the
// §5 branch-zeroing checks, the specified-defaults checks, the measure
// functions held to the written wire, the refusal vectors (reject, never
// clamp — and never throw on hostile bytes), and the bench-corpus pins
// (bench_*, real_packet) the C++ bench pinner authored.
//
// Prints OK and exits 0, exactly like its C++/Go/JS/Dart twins. Run from
// test/java (the Makefile does): the wire goldens are at
// ../../testdata/wire. Both modes run in CI: -ea (the checked twin —
// writer-contract asserts fire) and default (the production twin, issue
// #156's target — asserts dormant). The wire must be identical in both,
// and the goldens prove it.

import example.ArmDefaults;
import example.Clauses;
import example.Constants;
import example.Degenerate;
import example.Enums;
import example.Joins;
import example.Types;
import example.Wire;

public final class Main {
    static boolean failed = false;

    static void check(boolean ok, String what) {
        if (!ok) {
            System.out.println("FAILED: " + what);
            failed = true;
        }
    }

    // asserts on = the checked twin (-ea); off = the release shape.
    static final boolean assertsEnabled = detectAsserts();

    static boolean detectAsserts() {
        boolean on = false;
        assert on = true;
        return on;
    }

    // expectAssert runs a writer-contract violation and demands the assert
    // fires (checked twin only — the release writer trusts by design).
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

    // The shared write buffer — a multiple of 8, larger than any pinned shape.
    static final byte[] writeBuf = new byte[4096];

    interface Writer<T> {
        int run(T value, byte[] data);
    }

    interface Reader<T> {
        boolean run(T value, byte[] data, int numBits);
    }

    interface Measurer<T> {
        int run(T value);
    }

    // pin writes value, byte-compares against the C++-pinned golden, holds
    // measure to the written wire, reads it back, and re-writes to prove
    // byte-identical round-trip — the leg's core instrument.
    static <T> void pin(String name, T value, T out, Writer<T> write, Reader<T> read, Measurer<T> measure) {
        final int n = write.run(value, writeBuf);
        final byte[] g = golden(name);
        check(n == g.length, name + ": wrote " + n + " bytes, golden has " + g.length);
        check(bytesEqual(writeBuf, n, g), name + ": Java bytes == the C++-pinned bytes");
        final int bits = measure.run(value);
        check((bits + 7) >>> 3 == n, name + ": measure " + bits + " bits vs " + n + " bytes written");
        check(read.run(out, g, g.length * 8), name + ": read");
        final int n2 = write.run(out, writeBuf);
        check(n2 == n && bytesEqual(writeBuf, n, g), name + ": round-trips to identical bytes");
    }

    static byte[] textBytes(String s) {
        final byte[] b = new byte[s.length()];
        for (int i = 0; i < s.length(); i++) {
            b[i] = (byte) s.charAt(i);
        }
        return b;
    }

    static Types.ShipCreate makeShipCreate() {
        final Types.ShipCreate inp = new Types.ShipCreate();
        inp.shipType = Enums.ShipType.bomber;
        inp.position.x = 1000;
        inp.position.y = -2000;
        inp.position.z = 3000;
        inp.hasFlags = true;
        inp.flags = Enums.shipFlagsBoosting | Enums.shipFlagsAiming;
        inp.team = Enums.Team.blue;
        inp.health = 750;
        inp.thrust = 55;
        return inp;
    }

    static Types.RigidBody makeRigidBody() {
        final Types.RigidBody inp = new Types.RigidBody();
        inp.position.x = 1.5;
        inp.position.y = -2.5;
        inp.position.z = 3.25;
        inp.orientation.x = 0.1;
        inp.orientation.y = 0.2;
        inp.orientation.z = 0.3;
        inp.orientation.w = 0.9;
        inp.atRest = false;
        inp.linearVelocity.x = 10.0;
        inp.linearVelocity.y = 20.0;
        inp.linearVelocity.z = -3.0;
        inp.angularVelocity.x = 0.25;
        inp.angularVelocity.y = 0.5;
        inp.angularVelocity.z = 0.75;
        return inp;
    }

    static Types.InputPacket makeInputPacket() {
        final Types.InputPacket p = new Types.InputPacket();
        p.synchronizeSequence = 7;
        p.currentFrame = 123456789;
        p.startFrame = 123456780;
        p.inputsCount = 2;
        p.inputs[0].throttle = 0.5f;
        p.inputs[0].fire = true;
        p.inputs[1].stickX = -0.25f;
        p.inputs[1].boost = true;
        return p;
    }

    static Wire.TestData testDataInstance() {
        final Wire.TestData inp = new Wire.TestData();
        inp.a = -100;
        inp.b = 100;
        inp.c = 149;
        inp.d = 0x11;
        inp.e = 0x22;
        inp.f = 0x33;
        inp.g = true;
        inp.itemsCount = 3;
        inp.items[0] = 0;
        inp.items[1] = 128;
        inp.items[2] = 255;
        inp.floatValue = 3.1415926f;
        inp.compressedFloatValue = 2.5f;
        inp.doubleValue = 1.0 / 3.0;
        inp.int8Value = (byte) -128;
        inp.int16Value = (short) -32768;
        inp.uint8Value = (byte) 255;
        inp.uint16Value = (short) 65535;
        inp.uint32Value = 0xffffffff;
        inp.uint64Value = 0xffffffffffffffffL;
        inp.int64Full = 0x8000000000000000L; // int64 min, the hex spelling
        inp.int64Range = -999999999999L;
        for (int i = 0; i < inp.fixedBytes.length; i++) {
            inp.fixedBytes[i] = (byte) ((i * 3) & 0xff);
        }
        final byte[] text = textBytes("the quick brown fox");
        System.arraycopy(text, 0, inp.text, 0, text.length);
        inp.textLength = 19;
        return inp;
    }

    static void testShipCreate() {
        final Types.ShipCreate inp = makeShipCreate();
        final Types.ShipCreate out = new Types.ShipCreate();
        pin("shipcreate_flags", inp, out, Types::writeShipCreate, Types::readShipCreate, Types::measureShipCreate);
        check(out.hasFlags && out.flags == (Enums.shipFlagsBoosting | Enums.shipFlagsAiming),
                "ShipCreate flags round-trip");

        // untaken branch: flags must read back ZERO (SPEC §5) — into the same
        // out value, so stale flags would be caught
        inp.hasFlags = false;
        final int n = Types.writeShipCreate(inp, writeBuf);
        final byte[] wire = java.util.Arrays.copyOf(writeBuf, n);
        check(Types.readShipCreate(out, wire, n * 8), "read ShipCreate no-flags");
        check(!out.hasFlags && out.flags == 0, "untaken branch reads as zero (SPEC §5)");
        check((Types.measureShipCreate(inp) + 7) >>> 3 == n, "measure tracks the untaken branch");
    }

    static void testRigidBody() {
        final Types.RigidBody inp = makeRigidBody();
        pin("rigidbody_moving", inp, new Types.RigidBody(),
                Types::writeRigidBody, Types::readRigidBody, Types::measureRigidBody);

        inp.atRest = true;
        final Types.RigidBody out = new Types.RigidBody();
        pin("rigidbody_at_rest", inp, out,
                Types::writeRigidBody, Types::readRigidBody, Types::measureRigidBody);
        // the at-rest read must ZERO both velocities (SPEC §5) — out was read
        // from the at-rest wire after the writer's value had them set
        check(out.atRest, "at_rest reads true");
        check(out.linearVelocity.x == 0.0 && out.linearVelocity.y == 0.0 && out.linearVelocity.z == 0.0
                && out.angularVelocity.x == 0.0 && out.angularVelocity.y == 0.0 && out.angularVelocity.z == 0.0,
                "velocities read as zero under the taken at-rest branch (SPEC §5)");
    }

    static void testChat() {
        final Wire.Chat inp = new Wire.Chat();
        final byte[] text = textBytes("wire parity");
        System.arraycopy(text, 0, inp.text, 0, text.length);
        inp.textLength = 11;
        final Wire.Chat out = new Wire.Chat();
        pin("chat", inp, out, Wire::writeChat, Wire::readChat, Wire::measureChat);
        check(out.textLength == 11
                && java.util.Arrays.equals(out.text, 0, 11, textBytes("wire parity"), 0, 11),
                "Chat round-trips");
    }

    static void testProbeHeader() {
        final Wire.ProbeHeader inp = new Wire.ProbeHeader();
        inp.version = 5;
        inp.probeId = 0x1122334455667788L;
        final int n = Wire.writeProbeHeader(inp, writeBuf);
        check((writeBuf[0] & 0xff) == 0xab, "const(0xAB, 8) leads the wire");
        final Wire.ProbeHeader out = new Wire.ProbeHeader();
        pin("probe_header", inp, out, Wire::writeProbeHeader, Wire::readProbeHeader, Wire::measureProbeHeader);
        check(out.version == 5 && out.probeId == 0x1122334455667788L, "ProbeHeader round-trips");

        final byte[] corrupt = golden("probe_header");
        corrupt[0] = (byte) 0xac;
        check(!Wire.readProbeHeader(out, corrupt, corrupt.length * 8),
                "a corrupted wire constant is REJECTED (SPEC §4.3)");
        check(n == corrupt.length, "probe_header length agrees");
    }

    static void testCompressedProbe() {
        // the FMA-boundary vectors (SPEC §7.2 gate 7): 0.005 quantizes to 1
        // under the float32 two-rounding law; -4.8585 over the non-zero-min
        // range quantizes to 142. Same pinned instance as the C++ leg.
        final Wire.CompressedProbe inp = new Wire.CompressedProbe();
        inp.boundary = 0.005f;
        inp.offset = -4.8585f;
        final Wire.CompressedProbe out = new Wire.CompressedProbe();
        pin("compressed_probe", inp, out,
                Wire::writeCompressedProbe, Wire::readCompressedProbe, Wire::measureCompressedProbe);
        check(out.boundary == 1.0f / 1000.0f * 10.0f, "boundary reconstructs integer 1");
        check(out.offset == 142.0f / 10000.0f * 10.0f + -5.0f, "offset reconstructs integer 142");
    }

    static void testDefaults() {
        final Wire.ProbeSample sample = new Wire.ProbeSample();
        check(sample.active, "ProbeSample.active defaults true");
        Wire.zeroProbeSample(sample);
        check(!sample.active, "the §5 zero form stays zero — zero* does not reapply defaults");
        final Wire.ProbeConfig config = new Wire.ProbeConfig();
        check(config.retries == -1, "ProbeConfig.retries defaults -1");
        check(config.preferred == Wire.Weapon.railgun, "ProbeConfig.preferred defaults Railgun");
    }

    static void testProbeBits() {
        final Wire.ProbeBits inp = new Wire.ProbeBits();
        inp.small = 0x1ff;
        inp.boundary = 0x1ffffffffL;
        inp.wide = 0xfedcba9876543210L;
        inp.sensor = 0xffffffff;
        inp.nonce = 0xffffffffffffffffL;
        final Wire.ProbeBits out = new Wire.ProbeBits();
        pin("probebits", inp, out, Wire::writeProbeBits, Wire::readProbeBits, Wire::measureProbeBits);
        check(out.wide == 0xfedcba9876543210L && out.nonce == 0xffffffffffffffffL,
                "ProbeBits round-trips — 9/33/64-bit and full-range paths");
    }

    static void testProbeCollider() {
        // first-class one-of (SPEC §4.8) — C++-pinned wire, round trip, the
        // None arm, an array of unions, and the refusal negative controls
        final Wire.ProbeCollider inp = new Wire.ProbeCollider();
        check(inp.shape.type == Wire.ProbeShapeType.none, "construction is the empty union");
        check(Wire.probeShapeMaxBits == 2 + 16, "MaxBits is tag + the largest arm");

        inp.armor = 7;
        inp.shape.type = Wire.ProbeShapeType.slab;
        inp.shape.slab.width = 42;
        inp.shape.slab.height = 9;
        // inp.backup stays None — the empty arm costs the tag bits only
        inp.extrasCount = 1;
        inp.extras[0].type = Wire.ProbeShapeType.ring;
        inp.extras[0].ring.radius = 777;

        final Wire.ProbeCollider out = new Wire.ProbeCollider();
        out.backup.type = Wire.ProbeShapeType.ring; // dirty — the read must restore None
        pin("probecollider", inp, out,
                Wire::writeProbeCollider, Wire::readProbeCollider, Wire::measureProbeCollider);
        check(out.armor == 7 && out.shape.type == Wire.ProbeShapeType.slab
                && out.shape.slab.width == 42 && out.shape.slab.height == 9,
                "the selected arm round-trips");
        check(out.backup.type == Wire.ProbeShapeType.none, "the None arm reads back empty");
        check(out.extrasCount == 1 && out.extras[0].type == Wire.ProbeShapeType.ring
                && out.extras[0].ring.radius == 777,
                "the union array round-trips");

        // the all-None shape — the wire is far shorter than MaxBits; a reader
        // whose fused bounds counted MaxBitsUnion would refuse this valid wire
        final Wire.ProbeCollider none = new Wire.ProbeCollider();
        none.armor = 7;
        final int n = Wire.writeProbeCollider(none, writeBuf);
        final byte[] noneWire = java.util.Arrays.copyOf(writeBuf, n);
        check(Wire.readProbeCollider(out, noneWire, n * 8),
                "the all-None union wire reads (no MaxBits over-bounding)");
        check((Wire.measureProbeCollider(none) + 7) >>> 3 == n,
                "measure prices the selected arm, not MaxBits");

        // NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8, range
        // [0, 2]; forcing both bits makes 3 and the read must refuse
        final byte[] g = golden("probecollider");
        final byte[] corrupt = g.clone();
        corrupt[1] |= 0x03;
        check(!Wire.readProbeCollider(out, corrupt, g.length * 8),
                "an out-of-range union tag is refused (SPEC §4.8)");

        // NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits at bit
        // offset 10 with range [0, 100]; all seven bits decode 127
        final byte[] corrupt2 = g.clone();
        corrupt2[1] |= (byte) 0xfc;
        corrupt2[2] |= 0x01;
        check(!Wire.readProbeCollider(out, corrupt2, g.length * 8),
                "a corrupt union arm payload is refused (SPEC §4.8)");

        // the write side validates the tag BEFORE it rides (checked twin)
        expectAssert(() -> {
            final Wire.ProbeShape rogue = new Wire.ProbeShape();
            rogue.type = 3;
            Wire.writeProbeShape(rogue, writeBuf);
        }, "an out-of-set union tag must trip the writer contract");
    }

    static void testProbeSample() {
        // the nested if/else wire, both ways, and §5 zeroing
        final Wire.ProbeSample inp = new Wire.ProbeSample(); // active = true
        inp.orientation = 90.0f;
        inp.rawDelta = -5;
        inp.bigDelta = -1234567890123L;
        inp.weapon = Wire.Weapon.laser;
        inp.hasTarget = true;
        inp.targetId = 777;
        inp.idleTicks = 12345; // untaken side on the wire — must read back ZERO
        inp.samplesCount = 1;
        inp.samples[0] = 42;

        final int n = Wire.writeProbeSample(inp, writeBuf);
        final byte[] wire = java.util.Arrays.copyOf(writeBuf, n);
        final Wire.ProbeSample out = new Wire.ProbeSample();
        check(Wire.readProbeSample(out, wire, n * 8), "read ProbeSample active");
        check(out.active && out.weapon == Wire.Weapon.laser && out.hasTarget && out.targetId == 777,
                "the taken branch round-trips, nested branch included");
        check(out.idleTicks == 0, "the untaken else side reads as zero (SPEC §5)");
        check(out.orientation == 90.0f, "compressed float round-trips exactly at its resolution");
        check((Wire.measureProbeSample(inp) + 7) >>> 3 == n, "ProbeSample measure vs written bytes");

        inp.active = false;
        inp.hasTarget = false;
        final int n2 = Wire.writeProbeSample(inp, writeBuf);
        final byte[] wire2 = java.util.Arrays.copyOf(writeBuf, n2);
        check(Wire.readProbeSample(out, wire2, n2 * 8), "read ProbeSample idle");
        check(!out.active && out.idleTicks == 12345, "the else branch round-trips");
        check(out.weapon == Wire.Weapon.none && !out.hasTarget && out.targetId == 0,
                "the whole untaken then side reads as zero, nested branch included");
    }

    static void testProbeArray() {
        // transitive defaults and its C++ pin
        final Wire.ProbeArray fresh = new Wire.ProbeArray();
        check(fresh.samples[0].active && fresh.samples[1].active, "defaults reach through a fixed array");
        check(fresh.config.retries == -1 && fresh.config.preferred == Wire.Weapon.railgun,
                "defaults reach through a plain member");

        final Wire.ProbeArray inp = new Wire.ProbeArray();
        inp.samples[0].orientation = 90.0f;
        inp.samples[0].rawDelta = -5;
        inp.samples[0].bigDelta = -1234567890123L;
        inp.samples[0].weapon = Wire.Weapon.laser;
        inp.samples[0].hasTarget = true;
        inp.samples[0].targetId = 777;
        inp.samples[0].samplesCount = 1;
        inp.samples[0].samples[0] = 42;
        inp.samples[1].active = false;
        inp.samples[1].orientation = -45.5f;
        inp.samples[1].rawDelta = 7;
        inp.samples[1].bigDelta = 99;
        inp.samples[1].idleTicks = 1000;
        inp.samples[1].samplesCount = 2;
        inp.samples[1].samples[0] = 7;
        inp.samples[1].samples[1] = 8;
        inp.config.retries = 3;
        inp.config.preferred = Wire.Weapon.missile;

        final Wire.ProbeArray out = new Wire.ProbeArray();
        pin("probearray", inp, out, Wire::writeProbeArray, Wire::readProbeArray, Wire::measureProbeArray);
        check(!out.samples[1].active && out.samples[1].idleTicks == 1000, "nested else branch round-trips");
        check(out.samples[1].weapon == Wire.Weapon.none && !out.samples[1].hasTarget,
                "nested untaken side reads as zero (SPEC §5)");
        check(out.config.retries == 3 && out.config.preferred == Wire.Weapon.missile, "config round-trips");
    }

    static void testProbeReport() {
        // nested composition, and the widened flags wire
        final Wire.ProbeReport inp = new Wire.ProbeReport();
        inp.header.version = 3;
        inp.header.probeId = 0xcafebabeL;
        inp.flags = Wire.probeFlagsArmed | Wire.probeFlagsDamaged;
        inp.echo.testA = 555;
        inp.echo.testB = 1000;

        final int n = Wire.writeProbeReport(inp, writeBuf);
        final byte[] wire = java.util.Arrays.copyOf(writeBuf, n);
        final Wire.ProbeReport out = new Wire.ProbeReport();
        check(Wire.readProbeReport(out, wire, n * 8), "read ProbeReport");
        check(out.header.probeId == 0xcafebabeL
                && out.flags == (Wire.probeFlagsArmed | Wire.probeFlagsDamaged)
                && out.echo.testA == 555 && out.echo.testB == 1000,
                "ProbeReport round-trips — a named type as an ordinary field");

        // a mask bit above the widened 8-bit wire is refused, not truncated
        expectAssert(() -> {
            inp.flags = 1L << 9;
            Wire.writeProbeReport(inp, writeBuf);
        }, "a mask bit above the flags wire width must trip the writer contract");
    }

    static void testBlockAndHeartbeat() {
        final Wire.Block inp = new Wire.Block();
        for (int i = 0; i < 100; i++) {
            inp.data[i] = (byte) i;
        }
        inp.dataLength = 100;
        final int n = Wire.writeBlock(inp, writeBuf);
        final byte[] wire = java.util.Arrays.copyOf(writeBuf, n);
        final Wire.Block out = new Wire.Block();
        check(Wire.readBlock(out, wire, n * 8), "read Block");
        check(out.dataLength == 100 && java.util.Arrays.equals(out.data, 0, 100, inp.data, 0, 100),
                "Block round-trips — bytes(N) framing");
        check(Wire.measureBlock(inp) % 8 == 0, "Block wire ends byte-aligned");

        final Wire.Heartbeat hb = new Wire.Heartbeat();
        check(Wire.writeHeartbeat(hb, writeBuf) == 0
                && Wire.readHeartbeat(hb, writeBuf, 0)
                && Wire.measureHeartbeat(hb) == 0,
                "Heartbeat — presence is the payload (SPEC §4.6)");
    }

    static void testRejections() {
        // the readers agree on what they REJECT, and never throw

        // an interior null in a string is content the read refuses
        final byte[] chatGolden = golden("chat");
        final byte[] corrupt = chatGolden.clone();
        corrupt[4] = 0; // inside the text bytes (length rides bytes 0-1)
        final Wire.Chat out = new Wire.Chat();
        check(!Wire.readChat(out, corrupt, corrupt.length * 8),
                "an interior null is rejected (SPEC §4.7)");

        // a truncated stream is refused by the fused bounds checks
        final byte[] truncated = java.util.Arrays.copyOf(chatGolden, 3);
        check(!Wire.readChat(out, truncated, truncated.length * 8),
                "truncation is refused, not thrown");

        // a numBits larger than the buffer is refused up front
        check(!Wire.readChat(out, truncated, 4096), "an oversized numBits is refused up front");

        // a nonzero reserved bit is rejected
        final byte[] probeGolden = golden("probe_header");
        final byte[] corrupt2 = probeGolden.clone();
        corrupt2[1] |= 0x08; // the first reserved bit above version's 3
        final Wire.ProbeHeader out3 = new Wire.ProbeHeader();
        check(!Wire.readProbeHeader(out3, corrupt2, corrupt2.length * 8),
                "a nonzero reserved bit is rejected (SPEC §4.3)");

        // an out-of-range array count is refused before any element rides —
        // corrupt the count bits INSIDE a complete valid wire (the preamble is
        // 16+64+64 = 144 bits, so the 5-bit count sits at byte 18 bits 0-4)
        final byte[] packetGolden = golden("inputpacket");
        final byte[] corrupt3 = packetGolden.clone();
        corrupt3[18] = (byte) ((corrupt3[18] & ~0x1f) | 17); // count 2 -> 17, over [0, 16]
        final Types.InputPacket out4 = new Types.InputPacket();
        check(!Types.readInputPacket(out4, corrupt3, corrupt3.length * 8),
                "an out-of-range count is refused before the loop");
    }

    static void testWriterContracts() {
        // checked-twin writer contracts (release trusts by design)
        expectAssert(() -> {
            final Types.ShipCreate bad = new Types.ShipCreate();
            bad.health = 5000; // above [0, MaxHealth]
            Types.writeShipCreate(bad, writeBuf);
        }, "an out-of-range ranged write must trip the writer contract");

        expectAssert(() -> {
            final Wire.Chat bad = new Wire.Chat();
            bad.textLength = 999; // above string(MaxChatLength)
            Wire.writeChat(bad, writeBuf);
        }, "an out-of-range string length must trip the writer contract");

        // A COUNT IS NOT A CHECKED-TWIN CONTRACT (SPEC §4.6): the count guards
        // the element loop and the pack subtracts the low bound, so a count
        // outside its wire range is refused in EVERY build — with -ea and
        // without — rather than left to a predicate the JVM can disable.
        {
            final Types.InputPacket bad = new Types.InputPacket();
            bad.inputsCount = 17; // above [0, MaxInputsPerPacket]
            check(Types.writeInputPacket(bad, writeBuf) == -1,
                "a count above its wire range is refused in every build");
        }

        expectAssert(() -> {
            final Types.ShipCreate bad = new Types.ShipCreate();
            bad.shipType = 99; // enum headroom above the wire range
            Types.writeShipCreate(bad, writeBuf);
        }, "enum headroom above the wire range must trip the writer contract");
    }

    static void testNames() {
        // flagName / flagNames: per-bit names and the set renderer
        check(Enums.flagNameShipFlags(0).equals("FiringLaser"), "flagName names bit 0");
        check(Enums.flagNameShipFlags(9).equals("???"), "flagName is out-of-range safe");
        check(Enums.flagNamesShipFlags(0).equals("0"), "flagNames renders the empty set as 0");
        check(Enums.flagNamesShipFlags(Enums.shipFlagsFiringLaser | Enums.shipFlagsBraking)
                .equals("FiringLaser|Braking"), "flagNames renders the set bits");
        check(Enums.flagNamesShipFlags(Enums.shipFlagsAiming | (1L << 63))
                .equals("Aiming|0x8000000000000000"), "flagNames renders unknown high bits as hex");
        check(Wire.enumNameWeapon(Wire.Weapon.railgun).equals("Railgun"), "enumName names a variant");
        check(Wire.enumNameWeapon(15).equals("???"), "enumName is headroom-safe");
    }

    static void testBenchCorpus() {
        // ============ THE BENCH CORPUS (BENCH-STANDARD.md §1.5) ============
        // The same pinned instances test/bench/main.cpp authored into
        // testdata/wire/{bench_*,real_packet}.bin — the oracle gate the Java
        // bench runner is held to; this leg carries it because the runner
        // imports these exact classes.
        final bench.Bench.BenchPacket packet = new bench.Bench.BenchPacket();
        packet.a = -37;
        packet.b = 12345;
        packet.c = 987654;
        packet.bits7 = 97;
        packet.bits13 = 5000;
        packet.bits23 = 1234567;
        packet.flag = true;
        packet.x = 1.5f;
        packet.y = -3.25f;
        packet.z = 100.125f;
        packet.big = 0x123456789abcdef0L;
        for (int i = 0; i < 17; i++) {
            packet.blob[i] = (byte) ((i * 31) & 0xff);
        }
        pin("bench_packet", packet, new bench.Bench.BenchPacket(),
                bench.Bench::writeBenchPacket, bench.Bench::readBenchPacket, bench.Bench::measureBenchPacket);
        check(bench.Bench.measureBenchPacket(packet) == 392, "BenchPacket is 392 bits");

        final bench.Bench.BenchInts ints = new bench.Bench.BenchInts();
        ints.f0 = -37;
        ints.f1 = 12345;
        ints.f2 = 987654;
        ints.f3 = 2;
        ints.f4 = -15;
        ints.f5 = 777;
        ints.f6 = -2048;
        ints.f7 = 200;
        ints.f8 = -543210;
        ints.f9 = 99;
        pin("bench_ints", ints, new bench.Bench.BenchInts(),
                bench.Bench::writeBenchInts, bench.Bench::readBenchInts, bench.Bench::measureBenchInts);

        final bench.Bench.BenchBits bits = new bench.Bench.BenchBits();
        bits.b7 = 97;
        bits.b13 = 5000;
        bits.b23 = 1234567;
        bits.b3 = 5;
        bits.b32 = 0xdeadbeef;
        bits.b11 = 1024;
        bits.b19 = 333333;
        bits.b48 = 0xfedcba987654L;
        pin("bench_bits", bits, new bench.Bench.BenchBits(),
                bench.Bench::writeBenchBits, bench.Bench::readBenchBits, bench.Bench::measureBenchBits);

        // BenchMixed — THE canonical benchmark shape (#184); the pin is
        // test/bench/main.cpp's, transcribed exactly
        final bench.Bench.BenchMixed mixed = new bench.Bench.BenchMixed();
        mixed.sequence = 52428;
        mixed.ackSequence = 12345;
        mixed.ackBits = 0xa5a5a5a5;
        mixed.sessionId = 0x123456789abcdef0L;
        mixed.clientId = 0xdeadbeef;
        mixed.nonce = 0xfedcba9876543210L;
        mixed.worldTime = -987654321000L;
        mixed.frameTick = 0x123456789abcL;
        mixed.serverTime = 12345678;
        mixed.entitiesCount = 8;
        for (int i = 0; i < 8; i++) {
            final bench.Bench.MixedEntity e = mixed.entities[i];
            e.entityId = 2049 + i * 17;
            e.posX = -16383 + i * 4096;
            e.posY = 16383 - i * 4096;
            e.posZ = -1 + i * 2048;
            e.yaw = 511 - i * 64;
            e.pitch = i * 73;
            e.velX = -2048 + i * 512;
            e.velY = 2047 - i * 512;
            e.velZ = -1024 + i * 256;
            e.health = 1000 - i * 100;
            e.weapon = (byte) (1 + i);
            e.damage = 0x5a + i;
            e.moving = (i % 2) == 0;
            e.firing = (i % 3) == 0;
        }
        mixed.statsCount = 80;
        for (int i = 0; i < 80; i++) {
            mixed.stats[i].statId = (i * 3) % 256;
            mixed.stats[i].delta = -512 + (i * 13) % 1024;
        }
        mixed.gameEvent.type = bench.Bench.MixedEventType.hit;
        mixed.gameEvent.hit.targetId = 4095;
        mixed.gameEvent.hit.damage = 4095;
        mixed.gameEvent.hit.hitKind = 7;
        mixed.gameEvent.hit.crit = true;
        mixed.loadout[0] = 0x11;
        mixed.loadout[1] = 0x22;
        mixed.loadout[2] = 0x33;
        mixed.loadout[3] = 0x44;
        System.arraycopy("Rowan_01".getBytes(java.nio.charset.StandardCharsets.UTF_8), 0,
                mixed.playerName, 0, 8);
        final byte[] payloadBytes = { (byte) 0xde, (byte) 0xad, (byte) 0xbe, (byte) 0xef, 1, 2, 3, 4 };
        System.arraycopy(payloadBytes, 0, mixed.payload, 0, 8);
        mixed.playerNameLength = 8;
        mixed.payloadLength = 8;
        mixed.aimX = 0.5f;
        mixed.aimY = -0.25f;
        mixed.aimZ = 0.75f;
        mixed.recoil = 1.5f;
        mixed.drift = -3.25;
        mixed.wideKey = new bench.UInt128(0x0123456789abcdefL, 0xfedcba9876543210L);
        mixed.flux = new bench.Int128(0x800000000L, 7L);  // 2^99 + 7
        mixed.ping = 12345;
        mixed.crcHint = 0xabcdef;
        mixed.hasExtra = true;
        mixed.extra = 200;
        pin("bench_mixed", mixed, new bench.Bench.BenchMixed(),
                bench.Bench::writeBenchMixed, bench.Bench::readBenchMixed, bench.Bench::measureBenchMixed);

        // RealPacket pins the ALL-DEFAULTS instance: constructed and serialized
        // unmodified — 1629 bits = 204 bytes
        final realworld.RealWorld.RealPacket real = new realworld.RealWorld.RealPacket();
        pin("real_packet", real, new realworld.RealWorld.RealPacket(),
                realworld.RealWorld::writeRealPacket, realworld.RealWorld::readRealPacket,
                realworld.RealWorld::measureRealPacket);
        check(realworld.RealWorld.measureRealPacket(real) == 1629, "RealPacket is 1629 bits");
    }

    static void testSelectedArmDefaults() {
        // tag First=1 (2 bits), count=0 (2 bits), marker=5 (3 bits): 0x51.
        final ArmDefaults.DefaultChoice input = new ArmDefaults.DefaultChoice();
        input.type = ArmDefaults.DefaultChoiceType.first;
        for (int phase = 0; phase < 2; phase++) {
            if (phase == 1) {
                input.first.entriesCount = 2;
                input.first.entries[0].retries = 99;
                ArmDefaults.initDefaultArm(input.first);
                check(input.first.entriesCount == 0 && input.first.marker == 0
                        && input.first.entries[0].retries == -1 && input.first.entries[1].retries == -1,
                        "initDefaultArm restores construction defaults");
            }
            input.first.marker = 5;
            final int count = ArmDefaults.writeDefaultChoice(input, writeBuf);
            check(count == 1 && (writeBuf[0] & 255) == 0x51, "DefaultChoice independent wire is 0x51");
            check(ArmDefaults.measureDefaultChoice(input) == 7, "DefaultChoice uses seven independent bits");
        }

        final ArmDefaults.DefaultChoice output = new ArmDefaults.DefaultChoice();
        final ArmDefaults.DefaultArm first = output.first, second = output.second;
        final Wire.ProbeConfig[] firstEntries = first.entries, secondEntries = second.entries;
        final Wire.ProbeConfig first0 = first.entries[0], first1 = first.entries[1];
        final Wire.ProbeConfig second0 = second.entries[0], second1 = second.entries[1];
        second.marker = 7;
        second.entriesCount = 2;
        for (int i = 0; i < 2; i++) {
            second.entries[i].retries = 41 + i;
            second.entries[i].preferred = Wire.Weapon.missile;
        }
        output.type = ArmDefaults.DefaultChoiceType.second;
        for (int pass = 0; pass < 2; pass++) {
            first.entriesCount = 2;
            first.marker = 1;
            for (int i = 0; i < 2; i++) {
                first.entries[i].retries = 99;
                first.entries[i].preferred = Wire.Weapon.missile;
            }
            final boolean readOK = ArmDefaults.readDefaultChoice(output, new byte[] { 0x51 }, 7);
            check(readOK, "decode DefaultChoice, same tag included");
            final boolean selectedOK = output.type == ArmDefaults.DefaultChoiceType.first
                    && first.entriesCount == 0 && first.marker == 5;
            check(selectedOK, "DefaultChoice selected fields");
            // The control's backing-default marker requires a successful oracle decode.
            if (!readOK || !selectedOK) {
                return;
            }
            for (int i = 0; i < 2; i++) {
                check(first.entries[i].retries == -1 && first.entries[i].preferred == Wire.Weapon.railgun,
                        "selected unused backing entry receives its defaults");
                check(second.entries[i].retries == 41 + i && second.entries[i].preferred == Wire.Weapon.missile,
                        "unselected backing entry retains its value");
            }
            check(output.first == first && output.second == second
                    && first.entries == firstEntries && second.entries == secondEntries
                    && first.entries[0] == first0 && first.entries[1] == first1
                    && second.entries[0] == second0 && second.entries[1] == second1,
                    "selection preserves all preallocated identities");
            check(second.marker == 7 && second.entriesCount == 2, "unselected payload remains untouched");
        }
    }

    public static void main(String[] args) {
        testSelectedArmDefaults();
        check(Constants.maxHealth == 1000, "constants fold");
        testShipCreate();
        testRigidBody();
        testChat();
        testProbeHeader();
        pin("inputpacket", makeInputPacket(), new Types.InputPacket(),
                Types::writeInputPacket, Types::readInputPacket, Types::measureInputPacket);
        {
            final Wire.TestData out = new Wire.TestData();
            pin("testdata", testDataInstance(), out,
                    Wire::writeTestData, Wire::readTestData, Wire::measureTestData);
            check(out.int64Full == 0x8000000000000000L
                    && out.uint64Value == 0xffffffffffffffffL
                    && out.int8Value == (byte) -128,
                    "TestData extremes round-trip — signed narrows and full-range ints");
        }
        testCompressedProbe();
        testDefaults();
        testProbeBits();
        testProbeCollider();
        testProbeSample();
        testProbeArray();
        testProbeReport();
        testBlockAndHeartbeat();
        testRejections();
        testWriterContracts();
        testNames();
        testBenchCorpus();
        testDegenerate();
        testClauses();
        testJoins();

        if (failed) {
            System.exit(1);
        }
        System.out.println("OK");
    }

    // ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
    //
    // Twelve shapes in the C++ test's order against the one C++-pinned
    // golden. This emitter writes a whole message into a buffer rather than
    // appending to a bit stream, so the leg CONCATENATES its twelve buffers
    // — which equals what the stream legs write because every type in that
    // file is a whole number of bytes wide. A fixed scalar array whose
    // elements an emitter places TWICE is invisible to a same-language round
    // trip; only this compare against another language's bytes names it.
    static void testDegenerate() {
        final Degenerate.Vec2 vec2 = new Degenerate.Vec2();
        vec2.x = 1.5;
        vec2.y = -2.25;

        final Degenerate.SpanF64 spanF64 = new Degenerate.SpanF64();
        spanF64.values[0] = 3.5;
        spanF64.values[1] = -4.75;

        final Degenerate.SpanU64 spanU64 = new Degenerate.SpanU64();
        spanU64.values[0] = 0xDEADBEEFCAFEBABEL;
        spanU64.values[1] = 1;

        final Degenerate.SpanI64 spanI64 = new Degenerate.SpanI64();
        spanI64.values[0] = -1234567890123L;
        spanI64.values[1] = 42;

        final Degenerate.SpanOne spanOne = new Degenerate.SpanOne();
        spanOne.values[0] = 0x0123456789ABCDEFL;

        final Degenerate.SpanChunk spanChunk = new Degenerate.SpanChunk();
        spanChunk.values[0] = 0x1111;
        spanChunk.values[1] = 0x2222;
        spanChunk.values[2] = 0x3333;
        spanChunk.values[3] = 0x4444;

        final Degenerate.SpanTail spanTail = new Degenerate.SpanTail();
        spanTail.values[0] = 6.125;
        spanTail.values[1] = -7.0;
        spanTail.tail = 0xFEEDFACE;

        final Degenerate.SpanTwice spanTwice = new Degenerate.SpanTwice();
        spanTwice.a[0] = 8.5;
        spanTwice.a[1] = 9.5;
        spanTwice.b[0] = -10.5;
        spanTwice.b[1] = -11.5;

        final Degenerate.Trio trio = new Degenerate.Trio();
        trio.a = 0xABCDE;
        trio.b = 0x12345;
        trio.c = 0xFFFFF;

        final Degenerate.TrioSole trioSole = new Degenerate.TrioSole();
        trioSole.inner.a = 1;
        trioSole.inner.b = 2;
        trioSole.inner.c = 3;

        final Degenerate.TrioFirst trioFirst = new Degenerate.TrioFirst();
        trioFirst.inner.a = 0xAAAAA;
        trioFirst.inner.b = 0x55555;
        trioFirst.inner.c = 0xF0F0F;
        trioFirst.trailer = 0xBEEF;

        final Degenerate.TrioStraddle straddle = new Degenerate.TrioStraddle();
        straddle.pad0 = 0x0011223344556677L;
        straddle.pad1 = 0x8899AABBCCDDEEFFL;
        straddle.pad2 = 0xFFFFFFFFFFFFFFFFL;
        straddle.pad3 = 0;
        straddle.pad4 = 0x123456789ABCDEF0L;
        straddle.pad5 = 0xABCDEF;
        straddle.inner.a = 0x11111;
        straddle.inner.b = 0x22222;
        straddle.inner.c = 0x33333;

        final byte[] g = golden("degenerate");
        final byte[] joined = new byte[g.length];
        int at = 0;
        at = emitDegenerate(joined, at, vec2, Degenerate::writeVec2, Degenerate::measureVec2, "Vec2");
        at = emitDegenerate(joined, at, spanF64, Degenerate::writeSpanF64, Degenerate::measureSpanF64, "SpanF64");
        at = emitDegenerate(joined, at, spanU64, Degenerate::writeSpanU64, Degenerate::measureSpanU64, "SpanU64");
        at = emitDegenerate(joined, at, spanI64, Degenerate::writeSpanI64, Degenerate::measureSpanI64, "SpanI64");
        at = emitDegenerate(joined, at, spanOne, Degenerate::writeSpanOne, Degenerate::measureSpanOne, "SpanOne");
        at = emitDegenerate(joined, at, spanChunk, Degenerate::writeSpanChunk, Degenerate::measureSpanChunk, "SpanChunk");
        at = emitDegenerate(joined, at, spanTail, Degenerate::writeSpanTail, Degenerate::measureSpanTail, "SpanTail");
        at = emitDegenerate(joined, at, spanTwice, Degenerate::writeSpanTwice, Degenerate::measureSpanTwice, "SpanTwice");
        at = emitDegenerate(joined, at, trio, Degenerate::writeTrio, Degenerate::measureTrio, "Trio");
        at = emitDegenerate(joined, at, trioSole, Degenerate::writeTrioSole, Degenerate::measureTrioSole, "TrioSole");
        at = emitDegenerate(joined, at, trioFirst, Degenerate::writeTrioFirst, Degenerate::measureTrioFirst, "TrioFirst");
        at = emitDegenerate(joined, at, straddle, Degenerate::writeTrioStraddle, Degenerate::measureTrioStraddle,
                "TrioStraddle");

        check(at * 8 == 128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408,
                "the twelve degenerate shapes ride their declared widths and nothing more");
        check(at == g.length, "degenerate: wrote " + at + " bytes, golden has " + g.length);
        check(java.util.Arrays.equals(joined, g), "degenerate: Java bytes == the C++-pinned bytes");

        // and each shape reads back out of its own slice of the golden
        final Degenerate.Vec2 rVec2 = new Degenerate.Vec2();
        final Degenerate.SpanF64 rSpanF64 = new Degenerate.SpanF64();
        final Degenerate.SpanU64 rSpanU64 = new Degenerate.SpanU64();
        final Degenerate.SpanI64 rSpanI64 = new Degenerate.SpanI64();
        final Degenerate.SpanOne rSpanOne = new Degenerate.SpanOne();
        final Degenerate.SpanChunk rSpanChunk = new Degenerate.SpanChunk();
        final Degenerate.SpanTail rSpanTail = new Degenerate.SpanTail();
        final Degenerate.SpanTwice rSpanTwice = new Degenerate.SpanTwice();
        final Degenerate.Trio rTrio = new Degenerate.Trio();
        final Degenerate.TrioSole rTrioSole = new Degenerate.TrioSole();
        final Degenerate.TrioFirst rTrioFirst = new Degenerate.TrioFirst();
        final Degenerate.TrioStraddle rStraddle = new Degenerate.TrioStraddle();

        int off = 0;
        off = readDegenerate(g, off, 16, rVec2, Degenerate::readVec2, "Vec2");
        off = readDegenerate(g, off, 16, rSpanF64, Degenerate::readSpanF64, "SpanF64");
        off = readDegenerate(g, off, 16, rSpanU64, Degenerate::readSpanU64, "SpanU64");
        off = readDegenerate(g, off, 16, rSpanI64, Degenerate::readSpanI64, "SpanI64");
        off = readDegenerate(g, off, 8, rSpanOne, Degenerate::readSpanOne, "SpanOne");
        off = readDegenerate(g, off, 8, rSpanChunk, Degenerate::readSpanChunk, "SpanChunk");
        off = readDegenerate(g, off, 20, rSpanTail, Degenerate::readSpanTail, "SpanTail");
        off = readDegenerate(g, off, 32, rSpanTwice, Degenerate::readSpanTwice, "SpanTwice");
        off = readDegenerate(g, off, 8, rTrio, Degenerate::readTrio, "Trio");
        off = readDegenerate(g, off, 8, rTrioSole, Degenerate::readTrioSole, "TrioSole");
        off = readDegenerate(g, off, 10, rTrioFirst, Degenerate::readTrioFirst, "TrioFirst");
        off = readDegenerate(g, off, 51, rStraddle, Degenerate::readTrioStraddle, "TrioStraddle");
        check(off == g.length, "the twelve reads consume the whole golden");

        check(rVec2.x == 1.5 && rVec2.y == -2.25, "Vec2 round-trips");
        check(rSpanF64.values[0] == 3.5 && rSpanF64.values[1] == -4.75, "SpanF64 round-trips");
        check(rSpanU64.values[0] == 0xDEADBEEFCAFEBABEL && rSpanU64.values[1] == 1, "SpanU64 round-trips");
        check(rSpanI64.values[0] == -1234567890123L && rSpanI64.values[1] == 42, "SpanI64 round-trips");
        check(rSpanOne.values[0] == 0x0123456789ABCDEFL, "SpanOne round-trips");
        check(rSpanChunk.values[0] == 0x1111 && rSpanChunk.values[3] == 0x4444, "SpanChunk round-trips");
        check(rSpanTail.values[0] == 6.125 && rSpanTail.values[1] == -7.0 && rSpanTail.tail == 0xFEEDFACE,
                "SpanTail round-trips");
        check(rSpanTwice.a[0] == 8.5 && rSpanTwice.b[1] == -11.5, "SpanTwice round-trips");
        check(rTrio.a == 0xABCDE && rTrio.b == 0x12345 && rTrio.c == 0xFFFFF, "Trio round-trips");
        check(rTrioSole.inner.a == 1 && rTrioSole.inner.c == 3, "TrioSole round-trips");
        check(rTrioFirst.inner.a == 0xAAAAA && rTrioFirst.trailer == 0xBEEF, "TrioFirst round-trips");
        check(rStraddle.pad0 == 0x0011223344556677L && rStraddle.pad4 == 0x123456789ABCDEF0L,
                "TrioStraddle pads round-trip");
        check(rStraddle.pad5 == 0xABCDEF && rStraddle.inner.a == 0x11111 && rStraddle.inner.c == 0x33333,
                "TrioStraddle's nested fields round-trip across the boundary");
    }

    static <T> int emitDegenerate(byte[] joined, int at, T value, Writer<T> write, Measurer<T> measure, String name) {
        final int n = write.run(value, writeBuf);
        check((measure.run(value) + 7) >>> 3 == n, name + ": measure vs bytes written");
        System.arraycopy(writeBuf, 0, joined, at, n);
        return at + n;
    }

    static <T> int readDegenerate(byte[] g, int off, int bytes, T out, Reader<T> read, String name) {
        final byte[] slice = new byte[bytes + 8]; // read slack
        System.arraycopy(g, off, slice, 0, bytes);
        check(read.run(out, slice, bytes * 8), "read " + name);
        return off + bytes;
    }

    // Clauses.schema and Joins.schema are NOT byte-aligned, so a shape's own
    // width in BITS is the unit here, not its byte count.
    static <T> int emitShape(byte[] joined, int at, int bits, T value, Writer<T> write, Measurer<T> measure,
            String name) {
        final int n = write.run(value, writeBuf);
        check(measure.run(value) == bits, name + ": measure " + measure.run(value) + ", expected " + bits + " bits");
        check(n == (bits + 7) >>> 3, name + ": wrote " + n + " bytes, expected " + ((bits + 7) >>> 3));
        System.arraycopy(writeBuf, 0, joined, at, n);
        return at + n;
    }

    static <T> int readShape(byte[] g, int off, int bits, T out, Reader<T> read, String name) {
        final int bytes = (bits + 7) >>> 3;
        final byte[] slice = new byte[bytes + 8]; // read slack
        System.arraycopy(g, off, slice, 0, bytes);
        check(read.run(out, slice, bits), "read " + name);
        return off + bytes;
    }

    // ---- Clauses.schema: element widths whose clauses disagree ----
    //
    // Degenerate.schema is every-type-a-whole-number-of-bytes by
    // construction, so no clause boundary in it lands mid-byte. These widths
    // are chosen so they do: at 13 bits a write clause takes four elements
    // and a read clause three. Each shape is written alone and the golden is
    // those concatenated — the shapes are not byte-aligned, so a shared
    // stream would not equal the concatenation every emitter can produce.
    static final int[] W13_COUNTS = { 0, 1, 3, 4, 5, 7, 12 };
    static final int[] W17_COUNTS = { 0, 1, 2, 3, 4, 9 };
    static final int[] W26_COUNTS = { 0, 1, 2, 3, 6 };
    static final int[] W1_COUNTS = { 0, 1, 3, 4, 5, 20 };
    static final int[] TRI_COUNTS = { 0, 1, 3, 4, 5, 10 };
    static final int[] STRS_BITS = { 27, 155, 75 };
    static final int[] UNEVEN_ITEM_BITS = { 0, 5, 44, 49 };
    static final int[] UNEVEN_BITS = { 18, 21, 55 };
    static final byte[][] STRS_S = { {}, "abcdefgh".getBytes(java.nio.charset.StandardCharsets.US_ASCII),
            "xyz".getBytes(java.nio.charset.StandardCharsets.US_ASCII) };
    static final byte[][] STRS_B = { {}, { (byte) 0xF0, (byte) 0xF1, (byte) 0xF2, (byte) 0xF3, (byte) 0xF4,
            (byte) 0xF5, (byte) 0xF6, (byte) 0xF7 }, { 1, 2, 3 } };

    static void testClauses() {
        final byte[] g = golden("clauses");
        final byte[] joined = new byte[g.length];
        int at = 0;

        for (final int c : W13_COUNTS) {
            final Clauses.W13 v = new Clauses.W13();
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i] = (short) (8191 - i * 733);
            }
            at = emitShape(joined, at, 4 + 13 * c, v, Clauses::writeW13, Clauses::measureW13, "W13/" + c);
        }
        for (final int c : W17_COUNTS) {
            final Clauses.W17 v = new Clauses.W17();
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i] = 131071 - i * 11117;
            }
            at = emitShape(joined, at, 4 + 17 * c, v, Clauses::writeW17, Clauses::measureW17, "W17/" + c);
        }
        for (final int c : W26_COUNTS) {
            final Clauses.W26 v = new Clauses.W26();
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i] = 67108863 - i * 5555555;
            }
            at = emitShape(joined, at, 3 + 26 * c, v, Clauses::writeW26, Clauses::measureW26, "W26/" + c);
        }
        for (final int c : W1_COUNTS) {
            final Clauses.W1 v = new Clauses.W1();
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i] = (byte) (i % 2);
            }
            at = emitShape(joined, at, 5 + c, v, Clauses::writeW1, Clauses::measureW1, "W1/" + c);
        }
        for (int c = 0; c <= 3; c++) {
            final Clauses.W52 v = new Clauses.W52();
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i] = 4503599627370495L - (long) i * 123456789L;
            }
            at = emitShape(joined, at, 2 + 52 * c, v, Clauses::writeW52, Clauses::measureW52, "W52/" + c);
        }
        for (int c = 0; c <= 3; c++) {
            final Clauses.W50 v = new Clauses.W50();
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i] = 1125899906842623L - (long) i * 987654321L;
            }
            at = emitShape(joined, at, 2 + 50 * c, v, Clauses::writeW50, Clauses::measureW50, "W50/" + c);
        }
        final Clauses.F13 f13 = new Clauses.F13();
        for (int i = 0; i < 7; i++) {
            f13.items[i] = (short) (8191 - i * 911);
        }
        at = emitShape(joined, at, 91, f13, Clauses::writeF13, Clauses::measureF13, "F13");

        for (final int c : TRI_COUNTS) {
            final Clauses.ArrTri3 v = new Clauses.ArrTri3();
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i].a = i % 2;
                v.items[i].b = i % 4;
            }
            at = emitShape(joined, at, 4 + 3 * c, v, Clauses::writeArrTri3, Clauses::measureArrTri3, "ArrTri3/" + c);
        }
        final Clauses.ArrEleven arrEleven = new Clauses.ArrEleven();
        for (int i = 0; i < 9; i++) {
            arrEleven.items[i].a = i % 8;
            arrEleven.items[i].b = 255 - i * 17;
        }
        at = emitShape(joined, at, 99, arrEleven, Clauses::writeArrEleven, Clauses::measureArrEleven, "ArrEleven");

        // lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag
        for (byte tag = 0; tag <= 2; tag++) {
            final Clauses.HoldsEmptyUnion v = new Clauses.HoldsEmptyUnion();
            v.lead = 21;
            v.tail = 99;
            v.u.type = tag;
            at = emitShape(joined, at, 14, v, Clauses::writeHoldsEmptyUnion, Clauses::measureHoldsEmptyUnion,
                    "HoldsEmptyUnion/" + tag);
        }

        // lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s bytes,
        // b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail. The
        // 5-bit lead is what puts the align at a non-zero offset.
        for (int k = 0; k < 3; k++) {
            final Clauses.Strs v = new Clauses.Strs();
            v.lead = 21;
            v.tail = 5;
            v.sLength = STRS_S[k].length;
            v.bLength = STRS_B[k].length;
            System.arraycopy(STRS_S[k], 0, v.s, 0, STRS_S[k].length);
            System.arraycopy(STRS_B[k], 0, v.b, 0, STRS_B[k].length);
            at = emitShape(joined, at, STRS_BITS[k], v, Clauses::writeStrs, Clauses::measureStrs, "Strs/" + k);
        }

        for (int c = 0; c <= 4; c++) {
            final Clauses.ArrNested v = new Clauses.ArrNested();
            v.lead = 21;
            v.tail = 5;
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                v.items[i].a = i % 8;
                v.items[i].b = 200 - i * 7;
            }
            at = emitShape(joined, at, 11 + 11 * c, v, Clauses::writeArrNested, Clauses::measureArrNested,
                    "ArrNested/" + c);
        }
        final Clauses.Sole sole = new Clauses.Sole();
        sole.only = 5555;
        at = emitShape(joined, at, 13, sole, Clauses::writeSole, Clauses::measureSole, "Sole");

        check(at == g.length, "clauses: wrote " + at + " bytes, golden has " + g.length);
        check(java.util.Arrays.equals(joined, g), "clauses: Java bytes == the C++-pinned bytes");

        // Read each shape back out of its own slice. A clause that decodes a
        // different number of elements than the writer encoded shows up here
        // even where the byte compare above happens to pass.
        int off = 0;
        for (final int c : W13_COUNTS) {
            final Clauses.W13 r = new Clauses.W13();
            off = readShape(g, off, 4 + 13 * c, r, Clauses::readW13, "W13/" + c);
            check(r.itemsCount == c, "W13/" + c + " count round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i] == (short) (8191 - i * 733), "W13/" + c + " element round-trips");
            }
        }
        for (final int c : W17_COUNTS) {
            final Clauses.W17 r = new Clauses.W17();
            off = readShape(g, off, 4 + 17 * c, r, Clauses::readW17, "W17/" + c);
            check(r.itemsCount == c, "W17/" + c + " count round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i] == 131071 - i * 11117, "W17/" + c + " element round-trips");
            }
        }
        for (final int c : W26_COUNTS) {
            final Clauses.W26 r = new Clauses.W26();
            off = readShape(g, off, 3 + 26 * c, r, Clauses::readW26, "W26/" + c);
            check(r.itemsCount == c, "W26/" + c + " count round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i] == 67108863 - i * 5555555, "W26/" + c + " element round-trips");
            }
        }
        for (final int c : W1_COUNTS) {
            final Clauses.W1 r = new Clauses.W1();
            off = readShape(g, off, 5 + c, r, Clauses::readW1, "W1/" + c);
            check(r.itemsCount == c, "W1/" + c + " count round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i] == (byte) (i % 2), "W1/" + c + " element round-trips");
            }
        }
        for (int c = 0; c <= 3; c++) {
            final Clauses.W52 r = new Clauses.W52();
            off = readShape(g, off, 2 + 52 * c, r, Clauses::readW52, "W52/" + c);
            check(r.itemsCount == c, "W52/" + c + " count round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i] == 4503599627370495L - (long) i * 123456789L, "W52/" + c + " element round-trips");
            }
        }
        for (int c = 0; c <= 3; c++) {
            final Clauses.W50 r = new Clauses.W50();
            off = readShape(g, off, 2 + 50 * c, r, Clauses::readW50, "W50/" + c);
            check(r.itemsCount == c, "W50/" + c + " count round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i] == 1125899906842623L - (long) i * 987654321L, "W50/" + c + " element round-trips");
            }
        }
        final Clauses.F13 rF13 = new Clauses.F13();
        off = readShape(g, off, 91, rF13, Clauses::readF13, "F13");
        for (int i = 0; i < 7; i++) {
            check(rF13.items[i] == (short) (8191 - i * 911), "F13 element round-trips");
        }
        for (final int c : TRI_COUNTS) {
            final Clauses.ArrTri3 r = new Clauses.ArrTri3();
            off = readShape(g, off, 4 + 3 * c, r, Clauses::readArrTri3, "ArrTri3/" + c);
            check(r.itemsCount == c, "ArrTri3/" + c + " count round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i].a == i % 2 && r.items[i].b == i % 4, "ArrTri3/" + c + " element round-trips");
            }
        }
        final Clauses.ArrEleven rEleven = new Clauses.ArrEleven();
        off = readShape(g, off, 99, rEleven, Clauses::readArrEleven, "ArrEleven");
        for (int i = 0; i < 9; i++) {
            check(rEleven.items[i].a == i % 8 && rEleven.items[i].b == 255 - i * 17, "ArrEleven element round-trips");
        }
        for (byte tag = 0; tag <= 2; tag++) {
            final Clauses.HoldsEmptyUnion r = new Clauses.HoldsEmptyUnion();
            off = readShape(g, off, 14, r, Clauses::readHoldsEmptyUnion, "HoldsEmptyUnion/" + tag);
            check(r.lead == 21 && r.tail == 99 && r.u.type == tag, "HoldsEmptyUnion/" + tag + " round-trips");
        }
        for (int k = 0; k < 3; k++) {
            final Clauses.Strs r = new Clauses.Strs();
            off = readShape(g, off, STRS_BITS[k], r, Clauses::readStrs, "Strs/" + k);
            check(r.lead == 21 && r.tail == 5, "Strs/" + k + " lead and tail round-trip");
            check(r.sLength == STRS_S[k].length && r.bLength == STRS_B[k].length, "Strs/" + k + " lengths round-trip");
            for (int i = 0; i < STRS_S[k].length; i++) {
                check(r.s[i] == STRS_S[k][i], "Strs/" + k + " string byte round-trips");
            }
            for (int i = 0; i < STRS_B[k].length; i++) {
                check(r.b[i] == STRS_B[k][i], "Strs/" + k + " bytes byte round-trips");
            }
        }
        for (int c = 0; c <= 4; c++) {
            final Clauses.ArrNested r = new Clauses.ArrNested();
            off = readShape(g, off, 11 + 11 * c, r, Clauses::readArrNested, "ArrNested/" + c);
            check(r.itemsCount == c && r.lead == 21 && r.tail == 5, "ArrNested/" + c + " round-trips");
            for (int i = 0; i < c; i++) {
                check(r.items[i].a == i % 8 && r.items[i].b == 200 - i * 7, "ArrNested/" + c + " element round-trips");
            }
        }
        final Clauses.Sole rSole = new Clauses.Sole();
        off = readShape(g, off, 13, rSole, Clauses::readSole, "Sole");
        check(rSole.only == 5555, "Sole round-trips");
        check(off == g.length, "the clauses reads consume the whole golden");
    }

    // ---- Joins.schema: where a static offset is given up and regained ----
    //
    // Every branch is written on BOTH arms, so no path is pinned by omission.
    // The expected value after a round trip is not the value written: the
    // untaken side reads back as zero (SPEC §5).
    static void testJoins() {
        final byte[] g = golden("joins");
        final byte[] joined = new byte[g.length];
        int at = 0;

        for (int fi = 0; fi <= 1; fi++) {
            final boolean f = fi != 0;
            // the arms agree on WIDTH but not on value, so a join that keeps
            // the wrong arm is a value mismatch and not just a width one
            final Joins.ArmsAgree agree = new Joins.ArmsAgree();
            agree.lead = 21;
            agree.flag = f;
            agree.a = 1234;
            agree.b = 1500;
            agree.tail = 99;
            at = emitShape(joined, at, 24, agree, Joins::writeArmsAgree, Joins::measureArmsAgree, "ArmsAgree/" + f);

            final Joins.ArmsDisagree disagree = new Joins.ArmsDisagree();
            disagree.lead = 21;
            disagree.flag = f;
            disagree.a = 1234;
            disagree.b = 5;
            disagree.tail = 99;
            at = emitShape(joined, at, f ? 24 : 16, disagree, Joins::writeArmsDisagree, Joins::measureArmsDisagree,
                    "ArmsDisagree/" + f);

            final Joins.ArmEmpty armEmpty = new Joins.ArmEmpty();
            armEmpty.lead = 21;
            armEmpty.flag = f;
            armEmpty.a = 456789;
            armEmpty.tail = 99;
            at = emitShape(joined, at, f ? 32 : 13, armEmpty, Joins::writeArmEmpty, Joins::measureArmEmpty,
                    "ArmEmpty/" + f);

            final Joins.ArmAlign alignStr = new Joins.ArmAlign();
            alignStr.lead = 21;
            alignStr.flag = f;
            System.arraycopy("abcd".getBytes(java.nio.charset.StandardCharsets.US_ASCII), 0, alignStr.s, 0, 4);
            alignStr.sLength = 4;
            alignStr.b = 1000;
            alignStr.tail = 99;
            at = emitShape(joined, at, f ? 55 : 23, alignStr, Joins::writeArmAlign, Joins::measureArmAlign,
                    "ArmAlign/" + f);

            final Joins.ArmAlign alignEmpty = new Joins.ArmAlign();
            alignEmpty.lead = 21;
            alignEmpty.flag = f;
            alignEmpty.b = 1000;
            alignEmpty.tail = 99;
            at = emitShape(joined, at, 23, alignEmpty, Joins::writeArmAlign, Joins::measureArmAlign,
                    "ArmAlignEmptyStr/" + f);
        }

        for (int oi = 0; oi <= 1; oi++) {
            for (int ii = 0; ii <= 1; ii++) {
                final boolean o = oi != 0;
                final boolean in = ii != 0;
                final Joins.ArmsNested v = new Joins.ArmsNested();
                v.lead = 5;
                v.outer = o;
                v.inner = in;
                v.x = 500000000;
                v.y = 17;
                v.z = 4000;
                v.tail = 33;
                at = emitShape(joined, at, o ? (in ? 40 : 16) : 23, v, Joins::writeArmsNested, Joins::measureArmsNested,
                        "ArmsNested/" + oi + ii);
            }
        }

        for (int fi = 0; fi <= 1; fi++) {
            for (int c = 0; c <= 3; c++) {
                final boolean f = fi != 0;
                final Joins.ArmArray v = new Joins.ArmArray();
                v.lead = 21;
                v.flag = f;
                v.itemsCount = c;
                v.b = 300;
                v.tail = 99;
                for (int i = 0; i < c; i++) {
                    v.items[i] = (short) (8191 - i * 777);
                }
                at = emitShape(joined, at, f ? 15 + 13 * c : 22, v, Joins::writeArmArray, Joins::measureArmArray,
                        "ArmArray/" + f + "/" + c);
            }
        }

        // lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits
        for (byte tag = 0; tag <= 2; tag++) {
            final Joins.HoldsUneven v = new Joins.HoldsUneven();
            v.lead = 21;
            v.tail = 1500;
            v.u.type = tag;
            if (tag == Joins.UnevenType.narrow) {
                v.u.narrow.n = 5;
            }
            if (tag == Joins.UnevenType.wide) {
                v.u.wide.w = 123456789012L;
            }
            at = emitShape(joined, at, UNEVEN_BITS[tag], v, Joins::writeHoldsUneven, Joins::measureHoldsUneven,
                    "HoldsUneven/" + tag);
        }

        // alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37)
        for (int c = 0; c <= 3; c++) {
            final Joins.ArrUneven v = new Joins.ArrUneven();
            v.lead = 21;
            v.tail = 5;
            v.itemsCount = c;
            for (int i = 0; i < c; i++) {
                if (i % 2 == 0) {
                    v.items[i].type = Joins.UnevenType.narrow;
                    v.items[i].narrow.n = i % 8;
                } else {
                    v.items[i].type = Joins.UnevenType.wide;
                    v.items[i].wide.w = 99887766554L + i;
                }
            }
            at = emitShape(joined, at, 10 + UNEVEN_ITEM_BITS[c], v, Joins::writeArrUneven, Joins::measureArrUneven,
                    "ArrUneven/" + c);
        }

        // lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s,
        // then a 32 + 29 + 19 + 4 static run after the align regains it
        for (int c = 0; c <= 3; c++) {
            for (int sl = 0; sl <= 4; sl += 4) {
                final Joins.RegainAfterAlign v = new Joins.RegainAfterAlign();
                v.lead = 21;
                v.itemsCount = c;
                v.sLength = sl;
                if (sl != 0) {
                    System.arraycopy("wxyz".getBytes(java.nio.charset.StandardCharsets.US_ASCII), 0, v.s, 0, 4);
                }
                for (int i = 0; i < c; i++) {
                    v.items[i] = (short) (8191 - i * 999);
                }
                v.p = 0xDEADBEEF;
                v.q = (1 << 29) - 7;
                v.r = (1 << 19) - 3;
                v.tail = 9;
                final int afterAlign = ((5 + 2 + 13 * c + 3) + 7) / 8 * 8;
                at = emitShape(joined, at, afterAlign + 8 * sl + 84, v, Joins::writeRegainAfterAlign,
                        Joins::measureRegainAfterAlign, "Regain/" + c + "/" + sl);
            }
        }

        check(at == g.length, "joins: wrote " + at + " bytes, golden has " + g.length);
        check(java.util.Arrays.equals(joined, g), "joins: Java bytes == the C++-pinned bytes");

        int off = 0;
        for (int fi = 0; fi <= 1; fi++) {
            final boolean f = fi != 0;
            final Joins.ArmsAgree agree = new Joins.ArmsAgree();
            off = readShape(g, off, 24, agree, Joins::readArmsAgree, "ArmsAgree/" + f);
            check(agree.lead == 21 && agree.flag == f && agree.tail == 99, "ArmsAgree/" + f + " round-trips");
            check(f ? (agree.a == 1234 && agree.b == 0) : (agree.b == 1500 && agree.a == 0),
                    "ArmsAgree/" + f + "'s untaken side reads as zero (SPEC §5)");

            final Joins.ArmsDisagree disagree = new Joins.ArmsDisagree();
            off = readShape(g, off, f ? 24 : 16, disagree, Joins::readArmsDisagree, "ArmsDisagree/" + f);
            check(disagree.lead == 21 && disagree.tail == 99, "ArmsDisagree/" + f + " round-trips");
            check(f ? (disagree.a == 1234 && disagree.b == 0) : (disagree.b == 5 && disagree.a == 0),
                    "ArmsDisagree/" + f + "'s untaken side reads as zero");

            final Joins.ArmEmpty armEmpty = new Joins.ArmEmpty();
            off = readShape(g, off, f ? 32 : 13, armEmpty, Joins::readArmEmpty, "ArmEmpty/" + f);
            check(armEmpty.lead == 21 && armEmpty.tail == 99, "ArmEmpty/" + f + " round-trips");
            check(armEmpty.a == (f ? 456789 : 0), "ArmEmpty/" + f + "'s absent arm reads as zero");

            final Joins.ArmAlign alignStr = new Joins.ArmAlign();
            off = readShape(g, off, f ? 55 : 23, alignStr, Joins::readArmAlign, "ArmAlign/" + f);
            check(alignStr.lead == 21 && alignStr.tail == 99, "ArmAlign/" + f + " round-trips");
            check(f ? (alignStr.sLength == 4 && alignStr.s[0] == 'a' && alignStr.s[3] == 'd' && alignStr.b == 0)
                    : (alignStr.b == 1000 && alignStr.sLength == 0),
                    "ArmAlign/" + f + "'s untaken side reads as zero");

            final Joins.ArmAlign alignEmpty = new Joins.ArmAlign();
            off = readShape(g, off, 23, alignEmpty, Joins::readArmAlign, "ArmAlignEmptyStr/" + f);
            check(alignEmpty.lead == 21 && alignEmpty.tail == 99, "ArmAlignEmptyStr/" + f + " round-trips");
            check(f ? (alignEmpty.sLength == 0 && alignEmpty.b == 0) : (alignEmpty.b == 1000),
                    "ArmAlign/" + f + "'s empty string round-trips");
        }

        for (int oi = 0; oi <= 1; oi++) {
            for (int ii = 0; ii <= 1; ii++) {
                final boolean o = oi != 0;
                final boolean in = ii != 0;
                final Joins.ArmsNested r = new Joins.ArmsNested();
                off = readShape(g, off, o ? (in ? 40 : 16) : 23, r, Joins::readArmsNested, "ArmsNested/" + oi + ii);
                check(r.lead == 5 && r.tail == 33 && r.outer == o, "ArmsNested/" + oi + ii + " round-trips");
                if (o) {
                    check(r.inner == in && r.z == 0, "ArmsNested/" + oi + ii + "'s outer arm round-trips");
                    check(in ? (r.x == 500000000 && r.y == 0) : (r.y == 17 && r.x == 0),
                            "ArmsNested/" + oi + ii + "'s inner arm round-trips");
                } else {
                    check(r.z == 4000 && r.x == 0 && r.y == 0, "ArmsNested/" + oi + ii + "'s else arm round-trips");
                }
            }
        }

        for (int fi = 0; fi <= 1; fi++) {
            for (int c = 0; c <= 3; c++) {
                final boolean f = fi != 0;
                final Joins.ArmArray r = new Joins.ArmArray();
                off = readShape(g, off, f ? 15 + 13 * c : 22, r, Joins::readArmArray, "ArmArray/" + f + "/" + c);
                check(r.lead == 21 && r.tail == 99, "ArmArray/" + f + "/" + c + " round-trips");
                if (f) {
                    check(r.itemsCount == c && r.b == 0, "ArmArray/" + f + "/" + c + "'s array arm round-trips");
                    for (int i = 0; i < c; i++) {
                        check(r.items[i] == (short) (8191 - i * 777),
                                "ArmArray/" + f + "/" + c + " element round-trips");
                    }
                } else {
                    check(r.b == 300 && r.itemsCount == 0, "ArmArray/" + f + "/" + c + "'s scalar arm round-trips");
                }
            }
        }

        for (byte tag = 0; tag <= 2; tag++) {
            final Joins.HoldsUneven r = new Joins.HoldsUneven();
            off = readShape(g, off, UNEVEN_BITS[tag], r, Joins::readHoldsUneven, "HoldsUneven/" + tag);
            check(r.lead == 21 && r.tail == 1500 && r.u.type == tag, "HoldsUneven/" + tag + " round-trips");
            if (tag == Joins.UnevenType.narrow) {
                check(r.u.narrow.n == 5, "HoldsUneven's narrow arm round-trips");
            }
            if (tag == Joins.UnevenType.wide) {
                check(r.u.wide.w == 123456789012L, "HoldsUneven's wide arm round-trips");
            }
        }

        for (int c = 0; c <= 3; c++) {
            final Joins.ArrUneven r = new Joins.ArrUneven();
            off = readShape(g, off, 10 + UNEVEN_ITEM_BITS[c], r, Joins::readArrUneven, "ArrUneven/" + c);
            check(r.itemsCount == c && r.lead == 21 && r.tail == 5, "ArrUneven/" + c + " round-trips");
            for (int i = 0; i < c; i++) {
                if (i % 2 == 0) {
                    check(r.items[i].type == Joins.UnevenType.narrow && r.items[i].narrow.n == i % 8,
                            "ArrUneven narrow element round-trips");
                } else {
                    check(r.items[i].type == Joins.UnevenType.wide && r.items[i].wide.w == 99887766554L + i,
                            "ArrUneven wide element round-trips");
                }
            }
        }

        for (int c = 0; c <= 3; c++) {
            for (int sl = 0; sl <= 4; sl += 4) {
                final Joins.RegainAfterAlign r = new Joins.RegainAfterAlign();
                final int afterAlign = ((5 + 2 + 13 * c + 3) + 7) / 8 * 8;
                off = readShape(g, off, afterAlign + 8 * sl + 84, r, Joins::readRegainAfterAlign,
                        "Regain/" + c + "/" + sl);
                check(r.lead == 21 && r.itemsCount == c && r.sLength == sl, "Regain/" + c + "/" + sl + " round-trips");
                check(r.p == 0xDEADBEEF && r.q == (1 << 29) - 7 && r.r == (1 << 19) - 3 && r.tail == 9,
                        "Regain/" + c + "/" + sl + "'s static run after the align round-trips");
                for (int i = 0; i < c; i++) {
                    check(r.items[i] == (short) (8191 - i * 999), "Regain/" + c + "/" + sl + " element round-trips");
                }
            }
        }
        check(off == g.length, "the joins reads consume the whole golden");
    }

}
