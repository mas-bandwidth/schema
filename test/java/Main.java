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

import example.Constants;
import example.Degenerate;
import example.Enums;
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

        expectAssert(() -> {
            final Types.InputPacket bad = new Types.InputPacket();
            bad.inputsCount = 17; // above [0, MaxInputsPerPacket]
            Types.writeInputPacket(bad, writeBuf);
        }, "an out-of-range array count must trip the writer contract");

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

    public static void main(String[] args) {
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

}
