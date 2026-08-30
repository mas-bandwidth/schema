// schema bench — the Java runner: a conforming run.sh leg per
// bench/README.md's runner contract, measuring the generated monomorphic
// Java codecs over the four bench/corpus/Bench.schema shapes.
//
// CONTRACT (BENCH-STANDARD.md): fixed iteration counts identical to every
// other runner's rows for these benches; 1 discarded warmup run then 7
// measured runs per (bench, path) — or exactly one measured run under
// --round K, where the interleaved driver aggregates across rounds (§2.4);
// per-iteration LCG variation on the write path; 64 rotating variant read
// buffers; median/min/max/spread over the measured runs; CSV v2 rows on
// stdout under --csv, human table on stderr.
//
// WHAT THE ROWS MEASURE: the generated codec — schema's Java backend emits
// self-contained monomorphic write/read functions with the bitpacker
// inlined and zero runtime dependencies, so the generated code IS the Java
// serialize path. The rows carry family=gen: a ratio against another
// language's family=rt row (the serialize runtime API called by hand) is a
// subject difference, not a language difference, and the tools refuse it.
// Peak-style numbers from this runner's earlier serialize-family form
// (tight per-shape loops, best-of-five) are NOT comparable to these rows —
// different measurement contract, and the statistic alone moves the number.
//
// GOLDEN GATED (§1.5): before any timing, every measured shape's pinned
// instance is written and byte-compared against the C++-pinned
// testdata/wire golden, and all 64 LCG variant buffers are decoded back
// with every field verified. A runner that mismatches REFUSES to bench.
// corpus_id is FNV-1a-64 over the goldens this run actually loaded (§1.6).
//
// JVM discipline, by hand (no JMH — zero dependencies): every timed loop
// lives in its own method, one per shape and direction — a shared generic
// loop pools its type profile across shapes, goes megamorphic, and turns
// the timings bimodal (issue #156 item 5); the discarded warmup run (tens
// of millions of iterations) carries every loop to full C2/OSR compilation
// before the measured runs; and every loop's work drains into a static
// sink the bench publishes at exit under an env var the JIT cannot rule
// out. Default JVM flags, no -ea: asserts are dormant — the number a user
// gets is the number reported. Known artifact on this hardware: the packet
// WRITE row is bimodal ACROSS PROCESSES (~76 vs ~129 M msgs/s on the M2
// Air), keyed by process-environment-block size — an alignment effect, not
// warmup; under --round it surfaces as cross-round spread.
//
//   make build/java-bench/.stamp
//   cd bench/java && ../../dist/jdk-21.0.12.1/Contents/Home/bin/java \
//       -cp ../../build/java-bench Main [--csv] [--round K] [--quick]
//
// --quick: bench_mixed only, 3 measured runs — run.sh's iteration
// instrument, never the certification instrument.

import bench.Bench;

public final class Main {
    static final int NUM_VARIANTS = 64;

    // §1.2/§2.1: fixed per-benchmark iteration counts, identical across
    // every language's rows for these benches, recorded in the iters column
    static final int PACKET_ITERS = 32000000;
    static final int INTS_ITERS = 40000000;
    static final int BITS_ITERS = 48000000;
    static final int MIXED_ITERS = 40000000;

    static boolean csv = false;
    static boolean quick = false;
    static int numRuns = 7;

    static double now() {
        return System.nanoTime() * 1e-9;
    }

    // the g_sink of the C bench: computed values flow here, and the bench
    // publishes it at exit under an env var the JIT cannot rule out, so no
    // loop can be proven unobservable
    static long sink = 0;

    static void gateFail(String row, String what) {
        System.err.println("GOLDEN GATE FAILED: " + row + " " + what);
        System.err.println("reporting nothing.");
        System.exit(1);
    }

    /* ----------------------------------------------------------------------
       CSV v2 (§5.1): rows collected and flushed with the corpus_id of the
       goldens this run loaded; the per-runner constants — family gen (the
       rows measure generated code), linkage class (codec classfiles compiled
       beside the caller into one JVM, no library boundary), checks contract
       (caller-error asserts dormant without -ea, wire-contract validation
       unconditional in the reader), opt default (JIT, no level), inline
       unknown (no AOT artifact for the §4 verdict pass to walk).
       ---------------------------------------------------------------------- */

    static final String CSV_SUFFIX = "gen,class,contract,default,unknown";
    static final java.util.List<String> csvRows = new java.util.ArrayList<>();
    static final java.util.TreeMap<String, byte[]> goldensLoaded = new java.util.TreeMap<>();

    static String corpusId() {
        long h = 0xcbf29ce484222325L;
        for (java.util.Map.Entry<String, byte[]> g : goldensLoaded.entrySet()) {
            for (byte b : g.getKey().getBytes(java.nio.charset.StandardCharsets.UTF_8)) {
                h = (h ^ (b & 0xff)) * 0x100000001b3L;
            }
            h = (h ^ 0) * 0x100000001b3L;
            for (byte b : g.getValue()) {
                h = (h ^ (b & 0xff)) * 0x100000001b3L;
            }
        }
        return String.format("%016x", h);
    }

    static void report(String bench, String path, int iters, int bytesPerOp, double[] rates) {
        final double[] sorted = rates.clone();
        java.util.Arrays.sort(sorted);
        final double median = sorted[sorted.length / 2];
        final double min = sorted[0];
        final double max = sorted[sorted.length - 1];
        final double spread = (max - min) / median * 100.0;
        final double mbps = median * bytesPerOp / (1024.0 * 1024.0);
        System.err.printf("%-18s %-5s %10.2f M msg/s %10.1f MB/s   (min %.2f, max %.2f, spread %.1f%%)%n",
                bench, path, median / 1e6, mbps, min / 1e6, max / 1e6, spread);
        if (csv) {
            csvRows.add(String.format("java,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
                    bench, path, iters, bytesPerOp, rates.length, median, min, max, mbps, spread));
        }
    }

    static void flushCsv() {
        if (!csv) {
            return;
        }
        final String id = corpusId();
        final StringBuilder out = new StringBuilder();
        out.append("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,")
           .append("max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
        for (String row : csvRows) {
            out.append(row).append(',').append(id).append(',').append(CSV_SUFFIX).append('\n');
        }
        System.out.print(out);
    }

    /* ----------------------------------------------------------------------
       the C bench's uint64 LCG, direct: Java's long is 64 bits and wraps
       two's complement, which IS arithmetic mod 2^64
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
            final byte[] bytes = java.nio.file.Files.readAllBytes(
                    java.nio.file.Path.of("../../testdata/wire/" + name + ".bin"));
            goldensLoaded.put(name + ".bin", bytes);
            return bytes;
        } catch (java.io.IOException e) {
            System.err.println("missing wire golden testdata/wire/" + name
                    + ".bin — run from bench/java");
            System.exit(1);
            throw new IllegalStateException("unreachable");
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

    static double timePacketWrite(int iters) {
        final double start = now();
        for (int i = 0; i < iters; i++) {
            varyBenchPacket(packet);
            sink += Bench.writeBenchPacket(packet, writeBuf);
        }
        return now() - start;
    }

    static double timePacketRead(int iters) {
        final int numBits = packetBytes * 8;
        final double start = now();
        for (int i = 0; i < iters; i++) {
            if (!Bench.readBenchPacket(packetDecoded, packetVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += packetDecoded.b;
        }
        return now() - start;
    }

    static double timeIntsWrite(int iters) {
        final double start = now();
        for (int i = 0; i < iters; i++) {
            varyBenchInts(ints);
            sink += Bench.writeBenchInts(ints, writeBuf);
        }
        return now() - start;
    }

    static double timeIntsRead(int iters) {
        final int numBits = intsBytes * 8;
        final double start = now();
        for (int i = 0; i < iters; i++) {
            if (!Bench.readBenchInts(intsDecoded, intsVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += intsDecoded.f0;
        }
        return now() - start;
    }

    static double timeBitsWrite(int iters) {
        final double start = now();
        for (int i = 0; i < iters; i++) {
            varyBenchBits(bits);
            sink += Bench.writeBenchBits(bits, writeBuf);
        }
        return now() - start;
    }

    static double timeBitsRead(int iters) {
        final int numBits = bitsBytes * 8;
        final double start = now();
        for (int i = 0; i < iters; i++) {
            if (!Bench.readBenchBits(bitsDecoded, bitsVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += bitsDecoded.b7;
        }
        return now() - start;
    }

    static double timeMixedWrite(int iters) {
        final double start = now();
        for (int i = 0; i < iters; i++) {
            varyBenchMixed(mixed);
            sink += Bench.writeBenchMixed(mixed, writeBuf);
        }
        return now() - start;
    }

    static double timeMixedRead(int iters) {
        final int numBits = mixedBytes * 8;
        final double start = now();
        for (int i = 0; i < iters; i++) {
            if (!Bench.readBenchMixed(mixedDecoded, mixedVariants[i & (NUM_VARIANTS - 1)], numBits)) {
                System.exit(1);
            }
            sink += mixedDecoded.sequence;
        }
        return now() - start;
    }

    /* ----------------------------------------------------------------------
       the run scaffold: per (bench, path), 1 discarded warmup run — the C2
       warmup, tens of millions of iterations — then numRuns measured runs.
       The loops themselves are the per-shape methods above, handed in as
       monomorphic wrappers.
       ---------------------------------------------------------------------- */

    interface TimedLeg {
        double run(int iters);
    }

    static void benchLeg(String bench, String path, int iters, int bytesPerOp, TimedLeg leg) {
        final double[] rates = new double[numRuns];
        for (int run = -1; run < numRuns; run++) {
            final double elapsed = leg.run(iters);
            if (run >= 0) {
                rates[run] = iters / elapsed;
            }
        }
        report(bench, path, iters, bytesPerOp, rates);
    }

    public static void main(String[] args) {
        for (int i = 0; i < args.length; i++) {
            switch (args[i]) {
                case "--csv" -> csv = true;
                case "--quick" -> quick = true;
                case "--round" -> {
                    if (i + 1 >= args.length) {
                        System.err.println("--round takes a round number");
                        System.exit(1);
                    }
                    i++; // K only identifies the round to the driver
                    numRuns = 1;
                }
                default -> {
                    System.err.println("usage: Main [--csv] [--round K] [--quick]");
                    System.exit(1);
                }
            }
        }
        if (quick && numRuns == 7) {
            numRuns = 3;
        }

        System.err.println("schema bench (java, generated codecs"
                + (quick ? ", --quick: iteration instrument, not certification" : "") + ")");

        // every measured row's golden gate runs before any row is timed: a
        // runner that fails its goldens reports nothing at all (§1.5)
        if (quick) {
            gateMixed();
            benchLeg("bench_mixed", "write", MIXED_ITERS, mixedBytes, Main::timeMixedWrite);
            benchLeg("bench_mixed", "read", MIXED_ITERS, mixedBytes, Main::timeMixedRead);
        } else {
            gatePacket();
            gateInts();
            gateBits();
            gateMixed();
            benchLeg("bench_packet", "write", PACKET_ITERS, packetBytes, Main::timePacketWrite);
            benchLeg("bench_packet", "read", PACKET_ITERS, packetBytes, Main::timePacketRead);
            benchLeg("bench_ints", "write", INTS_ITERS, intsBytes, Main::timeIntsWrite);
            benchLeg("bench_ints", "read", INTS_ITERS, intsBytes, Main::timeIntsRead);
            benchLeg("bench_bits", "write", BITS_ITERS, bitsBytes, Main::timeBitsWrite);
            benchLeg("bench_bits", "read", BITS_ITERS, bitsBytes, Main::timeBitsRead);
            benchLeg("bench_mixed", "write", MIXED_ITERS, mixedBytes, Main::timeMixedWrite);
            benchLeg("bench_mixed", "read", MIXED_ITERS, mixedBytes, Main::timeMixedRead);
        }

        flushCsv();
        System.err.println("OK (corpus_id " + corpusId() + ")");

        // the g_sink escape: the JIT cannot prove the env var absent, so the
        // accumulated sink is observable and no loop's work can be deleted
        if (System.getenv("SERIALIZE_BENCH_SINK") != null) {
            System.err.println("sink: " + sink);
        }
    }
}
