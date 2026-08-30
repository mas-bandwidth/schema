// schema bench — the Java runner (minimal form): the generated monomorphic
// codecs over the four family `rt` shapes (BENCH-STANDARD.md §1.3), measured
// with serialize.java's own bench methodology so the rows read side by side
// with that library's — same LCG, same vary-function field mappings, same
// iteration counts, same 64 read variants, same warmup-then-best-of-five
// discipline, same units and reporting format (serialize.java
// bench/serialize/bench/Bench.java is the reference for all of it). This is
// the issue #156 prediction instrument: generated constant-width monomorphic
// code vs the library's stream rows.
//
// GOLDEN GATED (§1.5): before any row is timed, every pinned corpus
// instance is written and byte-compared against the C++-pinned
// testdata/wire golden, and all 64 LCG variant buffers of every shape are
// decoded back with every field verified. A runner that mismatches REFUSES
// to bench.
//
// JVM discipline, by hand (no JMH — zero dependencies): every timed loop
// lives in its own method, one per shape and direction — a shared generic
// loop pools its type profile across shapes, goes megamorphic, and turns
// the timings bimodal (issue #156 item 5); warmup trials (default 2,
// BENCH_WARMUP_TRIALS overrides) run each leg to full C2 compilation before
// the five timed trials; and every loop's work drains into a static sink
// the bench publishes at exit, so no loop can be proven unobservable. The
// timed run uses default JVM flags and no -ea: asserts are dormant — the
// number a user gets is the number reported.
//
//   make build/java-bench/.stamp
//   cd bench/java && ../../dist/jdk-21.0.12.1/Contents/Home/bin/java \
//       -cp ../../build/java-bench Main
//
// Iteration counts are overridable, the library bench's own env names:
//   BENCH_STREAM_PACKETS=100000 ... java -cp ../../build/java-bench Main
//
// Full run.sh/CSV-preamble integration per bench/README.md is a named
// follow-on; this runner carries the golden gate, the methodology and the
// rows.

import bench.Bench;

public final class Main {
    static final int NUM_TRIALS = 5;
    static final int NUM_VARIANTS = 64;

    static int envInt(String name, int fallback) {
        final String raw = System.getenv(name);
        if (raw == null) {
            return fallback;
        }
        int value;
        try {
            value = Integer.parseInt(raw);
        } catch (NumberFormatException e) {
            value = 0;
        }
        if (value < 1) {
            System.err.println(name + " must be a positive integer");
            System.exit(1);
        }
        return value;
    }

    static final int STREAM_NUM_PACKETS = envInt("BENCH_STREAM_PACKETS", 1000000);
    static final int WARMUP_TRIALS = envInt("BENCH_WARMUP_TRIALS", 2);

    // MEASURED CAVEAT (this machine, 2026-08-30): the packet WRITE row is
    // bimodal ACROSS PROCESSES — ~76 Mpps vs ~129 Mpps — keyed by the size
    // of the process environment block (adding any env var flips it), an
    // alignment artifact, not warmup (extra warmup trials do not move it)
    // and not the harness (rates are stable within a process and linear in
    // BENCH_STREAM_PACKETS). Every other row is stable. Read packet rows as
    // the mode you measured; the family discipline of quiet-machine ratios
    // applies per process.

    static boolean csv = false;

    static double now() {
        return System.nanoTime() * 1e-9;
    }

    // the g_sink of the C bench: computed values flow here, and the bench
    // publishes it at exit under an env var the JIT cannot rule out, so no
    // loop's work can be proven unobservable
    static long sink = 0;

    record Result(String row, String op, String units, double value) {}

    static final java.util.List<Result> results = new java.util.ArrayList<>();

    static void report(String row, String op, String units, double value) {
        results.add(new Result(row, op, units, value));
    }

    static void print(String line) {
        if (!csv) {
            System.out.print(line);
        }
    }

    static void gateFail(String row, String what) {
        System.err.println("GOLDEN GATE FAILED: " + row + " " + what);
        System.err.println("reporting nothing.");
        System.exit(1);
    }

    /* ----------------------------------------------------------------------
       the C bench's uint64 LCG, direct: Java's long is 64 bits and wraps
       two's complement, which IS arithmetic mod 2^64 (serialize.java bench,
       verbatim)
       ---------------------------------------------------------------------- */

    static final long LCG_MUL = 0x5851F42D4C957F2DL;
    static final long LCG_ADD = 0x14057B7EF767814FL;

    static long rng = 1;

    static void lcgSeed() {
        rng = 1;
    }

    static void lcgStep() {
        rng = rng * LCG_MUL + LCG_ADD;
    }

    // the low 32 bits of (rng >> s), for s in [0,63]
    static int shr(int s) {
        return (int) (rng >>> s);
    }

    /* ----------------------------------------------------------------------
       pinned instances and vary functions — the §1.3 shapes, field mappings
       verbatim from the family benches
       ---------------------------------------------------------------------- */

    static void initBenchPacket(Bench.BenchPacket p) {
        p.a = -37;
        p.b = 12345;
        p.c = 987654;
        p.bits7 = 97;
        p.bits13 = 5000;
        p.bits23 = 1234567;
        p.flag = true;
        p.x = 1.5f;
        p.y = -3.25f;
        p.z = 100.125f;
        p.big = 0x123456789abcdef0L;
        for (int i = 0; i < 17; i++) {
            p.blob[i] = (byte) ((i * 31) & 0xff);
        }
    }

    static void varyBenchPacket(Bench.BenchPacket p) {
        lcgStep();
        p.a = (shr(8) & 63) - 32;
        p.b = shr(16) & 65535;
        p.c = (shr(24) & 0xfffff) - 500000;
        p.bits7 = shr(0) & 127;
        p.bits13 = shr(3) & 8191;
        p.bits23 = shr(5) & 8388607;
        p.flag = (rng & 1) != 0;
        p.x = (float) (rng & 0xffff); // exact in float32
        p.big = rng; // the full 64 bits, direct
        p.blob[0] = (byte) (shr(32) & 0xff);
    }

    static boolean checkBenchPacket(Bench.BenchPacket e, Bench.BenchPacket d) {
        if (e.a != d.a || e.b != d.b || e.c != d.c
                || e.bits7 != d.bits7 || e.bits13 != d.bits13 || e.bits23 != d.bits23
                || e.flag != d.flag || e.x != d.x || e.y != d.y || e.z != d.z
                || e.big != d.big) {
            return false;
        }
        for (int i = 0; i < 17; i++) {
            if (e.blob[i] != d.blob[i]) {
                return false;
            }
        }
        return true;
    }

    static void initBenchInts(Bench.BenchInts p) {
        p.f0 = -37;
        p.f1 = 12345;
        p.f2 = 987654;
        p.f3 = 2;
        p.f4 = -15;
        p.f5 = 777;
        p.f6 = -2048;
        p.f7 = 200;
        p.f8 = -543210;
        p.f9 = 99;
    }

    static void varyBenchInts(Bench.BenchInts f) {
        lcgStep();
        f.f0 = (shr(8) & 63) - 32;
        f.f1 = shr(16) & 65535;
        f.f2 = (shr(24) & 0xfffff) - 500000;
        f.f3 = shr(2) & 3;
        f.f4 = (shr(11) & 15) - 8;
        f.f5 = shr(22) & 511;
        f.f6 = (shr(33) & 2047) - 1024;
        f.f7 = shr(40) & 255;
        f.f8 = (shr(30) & 0xfffff) - 500000;
        f.f9 = shr(57) & 63;
    }

    static boolean checkBenchInts(Bench.BenchInts e, Bench.BenchInts d) {
        return e.f0 == d.f0 && e.f1 == d.f1 && e.f2 == d.f2 && e.f3 == d.f3
                && e.f4 == d.f4 && e.f5 == d.f5 && e.f6 == d.f6 && e.f7 == d.f7
                && e.f8 == d.f8 && e.f9 == d.f9;
    }

    static void initBenchBits(Bench.BenchBits p) {
        p.b7 = 97;
        p.b13 = 5000;
        p.b23 = 1234567;
        p.b3 = 5;
        p.b32 = 0xdeadbeef;
        p.b11 = 1024;
        p.b19 = 333333;
        p.b48 = 0xfedcba987654L;
    }

    static void varyBenchBits(Bench.BenchBits f) {
        lcgStep();
        f.b7 = shr(0) & 127;
        f.b13 = shr(3) & 8191;
        f.b23 = shr(5) & 8388607;
        f.b3 = shr(29) & 7;
        f.b32 = shr(16);
        f.b11 = shr(37) & 2047;
        f.b19 = shr(44) & 524287;
        // the 48-bit field: low dword + 16-bit remainder, composed — the same
        // bits the family benches send as two lanes
        f.b48 = (rng & 0xffffffffL) | (((rng >>> 32) & 0xffffL) << 32);
    }

    static boolean checkBenchBits(Bench.BenchBits e, Bench.BenchBits d) {
        return e.b7 == d.b7 && e.b13 == d.b13 && e.b23 == d.b23 && e.b3 == d.b3
                && e.b32 == d.b32 && e.b11 == d.b11 && e.b19 == d.b19 && e.b48 == d.b48;
    }

    static void initBenchMixed(Bench.BenchMixed p) {
        p.sequence = 52428;
        p.ackBits = 0xa5a5a5a5;
        p.entityId = 2049;
        p.posX = -16384;
        p.posY = 16383;
        p.posZ = -1;
        p.yaw = 511;
        p.moving = true;
        p.firing = false;
        p.timestamp = 0x123456789abcL;
        p.weapon = 15;
    }

    static void varyBenchMixed(Bench.BenchMixed f) {
        lcgStep();
        f.sequence = shr(8) & 65535;
        f.ackBits = shr(16);
        f.entityId = (int) (rng & 4095);
        f.posX = (shr(20) & 32767) - 16384;
        f.posY = (shr(25) & 32767) - 16384;
        f.posZ = (shr(30) & 32767) - 16384;
        f.yaw = shr(3) & 511;
        f.moving = (rng & 1) != 0;
        f.firing = (rng & 2) != 0;
        f.timestamp = (rng & 0xffffffffL) | (((rng >>> 32) & 0xffffL) << 32);
        f.weapon = shr(60) & 15;
    }

    static boolean checkBenchMixed(Bench.BenchMixed e, Bench.BenchMixed d) {
        return e.sequence == d.sequence && e.ackBits == d.ackBits && e.entityId == d.entityId
                && e.posX == d.posX && e.posY == d.posY && e.posZ == d.posZ
                && e.yaw == d.yaw && e.moving == d.moving && e.firing == d.firing
                && e.timestamp == d.timestamp && e.weapon == d.weapon;
    }

    /* ----------------------------------------------------------------------
       the golden gate (§1.5), per shape: the PINNED instance's bytes must
       equal the C++-pinned testdata/wire golden, and all 64 variant buffers
       must decode back field-perfect. A mismatch refuses to bench.
       ---------------------------------------------------------------------- */

    static byte[] golden(String name) {
        try {
            return java.nio.file.Files.readAllBytes(
                    java.nio.file.Path.of("../../testdata/wire/" + name + ".bin"));
        } catch (java.io.IOException e) {
            throw new RuntimeException(e);
        }
    }

    static void gatePinned(String row, byte[] buffer, int n, String goldenName) {
        final byte[] g = golden(goldenName);
        if (n != g.length || !java.util.Arrays.equals(buffer, 0, n, g, 0, g.length)) {
            gateFail(row, "pinned instance vs testdata/wire/" + goldenName + ".bin");
        }
    }

    // the four shapes' state: packets, decode targets, variant buffers —
    // static fields so the timed loop methods reference them directly
    static final Bench.BenchPacket packet = new Bench.BenchPacket();
    static final Bench.BenchPacket packetDecoded = new Bench.BenchPacket();
    static final byte[][] packetVariants = new byte[NUM_VARIANTS][];
    static int packetBytes;

    static final Bench.BenchInts ints = new Bench.BenchInts();
    static final Bench.BenchInts intsDecoded = new Bench.BenchInts();
    static final byte[][] intsVariants = new byte[NUM_VARIANTS][];
    static int intsBytes;

    static final Bench.BenchBits bits = new Bench.BenchBits();
    static final Bench.BenchBits bitsDecoded = new Bench.BenchBits();
    static final byte[][] bitsVariants = new byte[NUM_VARIANTS][];
    static int bitsBytes;

    static final Bench.BenchMixed mixed = new Bench.BenchMixed();
    static final Bench.BenchMixed mixedDecoded = new Bench.BenchMixed();
    static final byte[][] mixedVariants = new byte[NUM_VARIANTS][];
    static int mixedBytes;

    static final byte[] writeBuf = new byte[256];

    static void gatePacket() {
        initBenchPacket(packet);
        final int n = Bench.writeBenchPacket(packet, writeBuf);
        gatePinned("bench_packet", writeBuf, n, "bench_packet");
        initBenchPacket(packet);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchPacket(packet);
            final int nv = Bench.writeBenchPacket(packet, writeBuf);
            if (v == 0) {
                packetBytes = nv;
            } else if (nv != packetBytes) {
                gateFail("bench_packet", "variant " + v + " size " + nv + " != " + packetBytes);
            }
            packetVariants[v] = java.util.Arrays.copyOf(writeBuf, nv);
        }
        initBenchPacket(packet);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchPacket(packet);
            if (!Bench.readBenchPacket(packetDecoded, packetVariants[v], packetBytes * 8)) {
                gateFail("bench_packet", "variant " + v + " read verdict");
            }
            if (!checkBenchPacket(packet, packetDecoded)) {
                gateFail("bench_packet", "variant " + v + " field mismatch");
            }
        }
    }

    static void gateInts() {
        initBenchInts(ints);
        final int n = Bench.writeBenchInts(ints, writeBuf);
        gatePinned("bench_ints", writeBuf, n, "bench_ints");
        initBenchInts(ints);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchInts(ints);
            final int nv = Bench.writeBenchInts(ints, writeBuf);
            if (v == 0) {
                intsBytes = nv;
            } else if (nv != intsBytes) {
                gateFail("bench_ints", "variant " + v + " size " + nv + " != " + intsBytes);
            }
            intsVariants[v] = java.util.Arrays.copyOf(writeBuf, nv);
        }
        initBenchInts(ints);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchInts(ints);
            if (!Bench.readBenchInts(intsDecoded, intsVariants[v], intsBytes * 8)) {
                gateFail("bench_ints", "variant " + v + " read verdict");
            }
            if (!checkBenchInts(ints, intsDecoded)) {
                gateFail("bench_ints", "variant " + v + " field mismatch");
            }
        }
    }

    static void gateBits() {
        initBenchBits(bits);
        final int n = Bench.writeBenchBits(bits, writeBuf);
        gatePinned("bench_bits", writeBuf, n, "bench_bits");
        initBenchBits(bits);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchBits(bits);
            final int nv = Bench.writeBenchBits(bits, writeBuf);
            if (v == 0) {
                bitsBytes = nv;
            } else if (nv != bitsBytes) {
                gateFail("bench_bits", "variant " + v + " size " + nv + " != " + bitsBytes);
            }
            bitsVariants[v] = java.util.Arrays.copyOf(writeBuf, nv);
        }
        initBenchBits(bits);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchBits(bits);
            if (!Bench.readBenchBits(bitsDecoded, bitsVariants[v], bitsBytes * 8)) {
                gateFail("bench_bits", "variant " + v + " read verdict");
            }
            if (!checkBenchBits(bits, bitsDecoded)) {
                gateFail("bench_bits", "variant " + v + " field mismatch");
            }
        }
    }

    static void gateMixed() {
        initBenchMixed(mixed);
        final int n = Bench.writeBenchMixed(mixed, writeBuf);
        gatePinned("bench_mixed", writeBuf, n, "bench_mixed");
        initBenchMixed(mixed);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchMixed(mixed);
            final int nv = Bench.writeBenchMixed(mixed, writeBuf);
            if (v == 0) {
                mixedBytes = nv;
            } else if (nv != mixedBytes) {
                gateFail("bench_mixed", "variant " + v + " size " + nv + " != " + mixedBytes);
            }
            mixedVariants[v] = java.util.Arrays.copyOf(writeBuf, nv);
        }
        initBenchMixed(mixed);
        lcgSeed();
        for (int v = 0; v < NUM_VARIANTS; v++) {
            varyBenchMixed(mixed);
            if (!Bench.readBenchMixed(mixedDecoded, mixedVariants[v], mixedBytes * 8)) {
                gateFail("bench_mixed", "variant " + v + " read verdict");
            }
            if (!checkBenchMixed(mixed, mixedDecoded)) {
                gateFail("bench_mixed", "variant " + v + " field mismatch");
            }
        }
    }

    /* ----------------------------------------------------------------------
       the timed legs: one loop method per shape and direction (issue #156
       item 5 — a shared generic loop pools its type profile across shapes
       and turns the timings bimodal), each a normally-compiled method body
       calling the generated statics directly.
       ---------------------------------------------------------------------- */

    static double timePacketWrite() {
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            varyBenchPacket(packet);
            sink += Bench.writeBenchPacket(packet, writeBuf);
        }
        return now() - start;
    }

    static double timePacketRead() {
        final int numBits = packetBytes * 8;
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            if (!Bench.readBenchPacket(packetDecoded, packetVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += packetDecoded.b;
        }
        return now() - start;
    }

    static double timePacketMeasure() {
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            varyBenchPacket(packet);
            sink += Bench.measureBenchPacket(packet);
        }
        return now() - start;
    }

    static double timeIntsWrite() {
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            varyBenchInts(ints);
            sink += Bench.writeBenchInts(ints, writeBuf);
        }
        return now() - start;
    }

    static double timeIntsRead() {
        final int numBits = intsBytes * 8;
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            if (!Bench.readBenchInts(intsDecoded, intsVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += intsDecoded.f0;
        }
        return now() - start;
    }

    static double timeBitsWrite() {
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            varyBenchBits(bits);
            sink += Bench.writeBenchBits(bits, writeBuf);
        }
        return now() - start;
    }

    static double timeBitsRead() {
        final int numBits = bitsBytes * 8;
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            if (!Bench.readBenchBits(bitsDecoded, bitsVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += bitsDecoded.b7;
        }
        return now() - start;
    }

    static double timeMixedWrite() {
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            varyBenchMixed(mixed);
            sink += Bench.writeBenchMixed(mixed, writeBuf);
        }
        return now() - start;
    }

    static double timeMixedRead() {
        final int numBits = mixedBytes * 8;
        final double start = now();
        for (int i = 0; i < STREAM_NUM_PACKETS; i++) {
            if (!Bench.readBenchMixed(mixedDecoded, mixedVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += mixedDecoded.sequence;
        }
        return now() - start;
    }

    /* ----------------------------------------------------------------------
       the trial scaffold, written once: the loops themselves are the
       per-shape methods above, handed in as monomorphic wrappers (the
       serialize.java bench's own shape).
       ---------------------------------------------------------------------- */

    interface TimedLeg {
        double run();
    }

    interface Reseed {
        void run();
    }

    record Best(double write, double read, double measure) {}

    static Best trials(Reseed reseed, TimedLeg writeLeg, TimedLeg readLeg, TimedLeg measureLeg) {
        double bestWrite = Double.POSITIVE_INFINITY;
        double bestRead = Double.POSITIVE_INFINITY;
        double bestMeasure = Double.POSITIVE_INFINITY;
        for (int trial = 0; trial < WARMUP_TRIALS + NUM_TRIALS; trial++) {
            reseed.run();
            double elapsed = writeLeg.run();
            if (trial >= WARMUP_TRIALS && elapsed < bestWrite) {
                bestWrite = elapsed;
            }
            elapsed = readLeg.run();
            if (trial >= WARMUP_TRIALS && elapsed < bestRead) {
                bestRead = elapsed;
            }
            if (measureLeg != null) {
                elapsed = measureLeg.run();
                if (trial >= WARMUP_TRIALS && elapsed < bestMeasure) {
                    bestMeasure = elapsed;
                }
            }
        }
        return new Best(bestWrite, bestRead, bestMeasure);
    }

    public static void main(String[] args) {
        for (String arg : args) {
            if (arg.equals("--csv")) {
                csv = true;
            }
        }

        // every row's golden gate runs before any row is timed: a runner that
        // fails its goldens reports nothing at all (§1.5)
        gatePacket();
        gateInts();
        gateBits();
        gateMixed();

        print("\n[schema bench — generated Java]\n\n");

        final double packets = STREAM_NUM_PACKETS / 1000000.0;

        // the stream-comparable row: the same 12-op packet serialize.java's
        // stream rows measure, through the generated monomorphic codec
        {
            final Best b = trials(() -> {
                initBenchPacket(packet);
                lcgSeed();
            }, Main::timePacketWrite, Main::timePacketRead, Main::timePacketMeasure);
            final double totalMB = (double) packetBytes * STREAM_NUM_PACKETS / (1024 * 1024);
            report("bench_packet", "write", "MB/s", totalMB / b.write());
            report("bench_packet", "write", "Mpackets/s", packets / b.write());
            report("bench_packet", "read", "MB/s", totalMB / b.read());
            report("bench_packet", "read", "Mpackets/s", packets / b.read());
            report("bench_packet", "measure", "Mpackets/s", packets / b.measure());
            print(String.format("packet (generated): write: %8.1f MB/s  (%.1f M packets/s)%n",
                    totalMB / b.write(), packets / b.write()));
            print(String.format("packet (generated): read:  %8.1f MB/s  (%.1f M packets/s)%n",
                    totalMB / b.read(), packets / b.read()));
            print(String.format("packet (generated): measure: %14.1f M packets/s%n",
                    packets / b.measure()));
        }

        print("\n");

        {
            final Best b = trials(() -> {
                initBenchInts(ints);
                lcgSeed();
            }, Main::timeIntsWrite, Main::timeIntsRead, null);
            report("bench_ints", "write", "Mpackets/s", packets / b.write());
            report("bench_ints", "read", "Mpackets/s", packets / b.read());
            print(String.format("int packet   (generated):  write: %6.1f M packets/s   read: %6.1f M packets/s%n",
                    packets / b.write(), packets / b.read()));
        }
        {
            final Best b = trials(() -> {
                initBenchBits(bits);
                lcgSeed();
            }, Main::timeBitsWrite, Main::timeBitsRead, null);
            report("bench_bits", "write", "Mpackets/s", packets / b.write());
            report("bench_bits", "read", "Mpackets/s", packets / b.read());
            print(String.format("bits packet  (generated):  write: %6.1f M packets/s   read: %6.1f M packets/s%n",
                    packets / b.write(), packets / b.read()));
        }
        {
            final Best b = trials(() -> {
                initBenchMixed(mixed);
                lcgSeed();
            }, Main::timeMixedWrite, Main::timeMixedRead, null);
            report("bench_mixed", "write", "Mpackets/s", packets / b.write());
            report("bench_mixed", "read", "Mpackets/s", packets / b.read());
            print(String.format("mixed packet (generated):  write: %6.1f M packets/s   read: %6.1f M packets/s%n",
                    packets / b.write(), packets / b.read()));
        }

        print("\n");

        if (csv) {
            final StringBuilder out = new StringBuilder("row,op,units,value\n");
            for (Result r : results) {
                out.append(String.format("%s,%s,%s,%.4f%n", r.row(), r.op(), r.units(), r.value()));
            }
            System.out.print(out);
        }

        // the g_sink escape: the JIT cannot prove the env var absent, so the
        // accumulated sink is observable and no loop's work can be deleted
        if (System.getenv("SERIALIZE_BENCH_SINK") != null) {
            System.err.println("sink: " + sink);
        }
    }
}
