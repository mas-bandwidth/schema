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
    static final int MIXED_ITERS = 4000000;

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

    // BenchMixed — THE canonical benchmark shape (issue #184). The pin is
    // test/bench/main.cpp's, transcribed exactly; STRUCTURE fields (the two
    // array counts, the two used lengths, the union tag, the `if` gate) are
    // set here and never touched by varyBenchMixed, so bytes/op is constant.
    static void initBenchMixed(Bench.BenchMixed p) {
        p.sequence = 52428;
        p.ackSequence = 12345;
        p.ackBits = 0xa5a5a5a5;
        p.sessionId = 0x123456789abcdef0L;
        p.clientId = 0xdeadbeef;
        p.nonce = 0xfedcba9876543210L;
        p.worldTime = -987654321000L;
        p.frameTick = 0x123456789abcL;
        p.serverTime = 12345678;
        p.entitiesCount = 8;
        for (int i = 0; i < 8; i++) {
            final Bench.MixedEntity e = p.entities[i];
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
        p.statsCount = 80;
        for (int i = 0; i < 80; i++) {
            p.stats[i].statId = (i * 3) % 256;
            p.stats[i].delta = -512 + (i * 13) % 1024;
        }
        p.gameEvent.type = Bench.MixedEventType.hit;
        p.gameEvent.hit.targetId = 4095;
        p.gameEvent.hit.damage = 4095;
        p.gameEvent.hit.hitKind = 7;
        p.gameEvent.hit.crit = true;
        p.loadout[0] = 0x11;
        p.loadout[1] = 0x22;
        p.loadout[2] = 0x33;
        p.loadout[3] = 0x44;
        System.arraycopy(PLAYER_NAME_PIN, 0, p.playerName, 0, 8);
        p.playerNameLength = 8;
        System.arraycopy(PAYLOAD_PIN, 0, p.payload, 0, 8);
        p.payloadLength = 8;
        p.aimX = 0.5f;
        p.aimY = -0.25f;
        p.aimZ = 0.75f;
        p.recoil = 1.5f;
        p.drift = -3.25;
        p.wideKey = new bench.UInt128(0x0123456789abcdefL, 0xfedcba9876543210L);
        p.flux = new bench.Int128(0x800000000L, 7L); // 2^99 + 7
        p.ping = 12345;
        p.crcHint = 0xabcdef;
        p.hasExtra = true;
        p.extra = 200;
    }

    static final byte[] PLAYER_NAME_PIN =
            "Rowan_01".getBytes(java.nio.charset.StandardCharsets.UTF_8);
    static final byte[] PAYLOAD_PIN = { (byte) 0xde, (byte) 0xad, (byte) 0xbe, (byte) 0xef, 1, 2, 3, 4 };

    // The LCG field mapping, identical in every runner. VALUE fields only:
    // every count, used length, union tag and branch gate is STRUCTURE (§2.7).
    // All 8 entities vary; the 80 stats vary delta (statId stays pinned).
    static void varyBenchMixed(Bench.BenchMixed f) {
        lcgStep();
        f.sequence = (int) ((rng >>> 8) & 65535);
        f.ackSequence = (int) ((rng >>> 24) & 65535);
        f.ackBits = (int) (rng >>> 16);
        f.sessionId = rng;
        f.clientId = (int) (rng >>> 32);
        f.nonce = rng ^ 0xa5a5a5a5a5a5a5a5L;
        f.worldTime = ((rng >>> 12) & 0xfffffffffL) - 34359738368L;
        f.frameTick = rng & 0xffffffffffffL;
        f.serverTime = (int) ((rng >>> 20) & 0x7fffff);
        for (int i = 0; i < 8; i++) {
            final Bench.MixedEntity e = f.entities[i];
            e.entityId = (int) ((rng >>> i) & 4095);
            e.posX = (int) ((rng >>> (i + 4)) & 16383) - 8192;
            e.posY = (int) ((rng >>> (i + 12)) & 16383) - 8192;
            e.health = (int) ((rng >>> (i + 20)) & 511);
            e.weapon = (byte) ((rng >>> (i + 40)) & 15);
            e.damage = (rng >>> (i + 28)) & 255;
            e.moving = ((rng >>> i) & 1) != 0;
        }
        for (int i = 0; i < 80; i++) {
            f.stats[i].delta = (int) ((rng >>> (i & 31)) & 1023) - 512;
        }
        f.gameEvent.hit.targetId = (int) ((rng >>> 6) & 4095);
        f.gameEvent.hit.damage = (int) ((rng >>> 18) & 4095);
        f.gameEvent.hit.hitKind = (int) ((rng >>> 30) & 7);
        f.gameEvent.hit.crit = (rng & 4) != 0;
        f.loadout[0] = (byte) (rng >>> 56);
        f.playerName[7] = (byte) (65 + ((rng >>> 50) & 15));
        f.payload[0] = (byte) (rng >>> 48);
        f.aimX = ((rng >>> 2) & 255) * (1.0f / 256.0f) - 0.5f;
        f.aimY = ((rng >>> 10) & 255) * (1.0f / 256.0f) - 0.5f;
        f.aimZ = ((rng >>> 18) & 255) * (1.0f / 256.0f) - 0.5f;
        f.recoil = rng & 0xffffL;
        f.drift = ((rng >>> 8) & 0xffffffL) * 0.5;
        f.wideKey = new bench.UInt128(rng >>> 1, rng);
        f.flux = new bench.Int128(0L, rng >>> 16);
        f.ping = (short) ((rng >>> 40) & 0x7fff);
        f.crcHint = (int) ((rng >>> 24) & 0xffffff);
        f.extra = (int) ((rng >>> 52) & 255);
    }

    static boolean checkBenchMixed(Bench.BenchMixed e, Bench.BenchMixed d) {
        if (e.sequence != d.sequence || e.ackSequence != d.ackSequence || e.ackBits != d.ackBits
                || e.sessionId != d.sessionId || e.clientId != d.clientId || e.nonce != d.nonce
                || e.worldTime != d.worldTime || e.frameTick != d.frameTick
                || e.serverTime != d.serverTime || e.entitiesCount != d.entitiesCount
                || e.statsCount != d.statsCount || e.gameEvent.type != d.gameEvent.type
                || e.playerNameLength != d.playerNameLength || e.payloadLength != d.payloadLength
                || e.recoil != d.recoil || e.drift != d.drift
                || e.wideKey.hi != d.wideKey.hi || e.wideKey.lo != d.wideKey.lo
                || e.flux.hi != d.flux.hi || e.flux.lo != d.flux.lo
                || e.ping != d.ping || e.crcHint != d.crcHint
                || e.hasExtra != d.hasExtra || e.extra != d.extra) {
            return false;
        }
        for (int i = 0; i < e.entitiesCount; i++) {
            final Bench.MixedEntity a = e.entities[i];
            final Bench.MixedEntity b = d.entities[i];
            if (a.entityId != b.entityId || a.posX != b.posX || a.posY != b.posY || a.posZ != b.posZ
                    || a.yaw != b.yaw || a.pitch != b.pitch || a.velX != b.velX || a.velY != b.velY
                    || a.velZ != b.velZ || a.health != b.health || a.weapon != b.weapon
                    || a.damage != b.damage || a.moving != b.moving || a.firing != b.firing) {
                return false;
            }
        }
        for (int i = 0; i < e.statsCount; i++) {
            if (e.stats[i].statId != d.stats[i].statId || e.stats[i].delta != d.stats[i].delta) {
                return false;
            }
        }
        if (e.gameEvent.hit.targetId != d.gameEvent.hit.targetId
                || e.gameEvent.hit.damage != d.gameEvent.hit.damage
                || e.gameEvent.hit.hitKind != d.gameEvent.hit.hitKind
                || e.gameEvent.hit.crit != d.gameEvent.hit.crit) {
            return false;
        }
        for (int i = 0; i < 4; i++) {
            if (e.loadout[i] != d.loadout[i]) {
                return false;
            }
        }
        for (int i = 0; i < e.playerNameLength; i++) {
            if (e.playerName[i] != d.playerName[i]) {
                return false;
            }
        }
        for (int i = 0; i < e.payloadLength; i++) {
            if (e.payload[i] != d.payload[i]) {
                return false;
            }
        }
        // aimX/Y/Z are COMPRESSED floats: the wire carries a quantized step,
        // so the decoded value is not the value that was written and no
        // equality check applies here. Their bytes are pinned by the golden
        // and their width is fixed, which is what the gate needs.
        return true;
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

    static final byte[] writeBuf = new byte[512]; // >= benchMixedMaxBytes (456), multiple of 8

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

       Read-side sink discipline (#175, equalized to the cpp/c reference):
       every read loop observes the FULL decoded struct per iteration. The
       C/C++ legs get this for free from an empty-asm memory clobber over
       the whole struct; Java has no zero-cost clobber, so the leg's idiom
       is a per-iteration sum of every decoded field into the static sink —
       floats via floatToRawIntBits (a bitcast, not a conversion), booleans
       as 0/1, byte arrays element-by-element. The sink adds are real work
       the clobber languages do not pay; the published number is therefore
       an upper bound on the observation cost (the PR #178 review measured
       the widening at -23% read on bench_mixed for exactly this loop).
       ---------------------------------------------------------------------- */

    static long sinkOfBenchPacket(Bench.BenchPacket d) {
        long s = d.a + d.b + d.c + d.bits7 + d.bits13 + d.bits23
                + (d.flag ? 1 : 0)
                + Float.floatToRawIntBits(d.x) + Float.floatToRawIntBits(d.y)
                + Float.floatToRawIntBits(d.z) + d.big;
        for (int i = 0; i < 17; i++) {
            s += d.blob[i];
        }
        return s;
    }

    static long sinkOfBenchInts(Bench.BenchInts d) {
        return (long) d.f0 + d.f1 + d.f2 + d.f3 + d.f4 + d.f5 + d.f6 + d.f7 + d.f8 + d.f9;
    }

    static long sinkOfBenchBits(Bench.BenchBits d) {
        return (long) d.b7 + d.b13 + d.b23 + d.b3 + d.b32 + d.b11 + d.b19 + d.b48;
    }

    // §2.7 full-struct observation: every decoded field of the canonical shape
    // folds into the sink each iteration — array elements one by one over the
    // decoded extent, booleans as 0/1, floats bitcast, the 128-bit values as
    // both halves, the string and byte block byte-summed over their used
    // lengths. Java has no free memory barrier here (the decode call inlines),
    // so this fold IS the observation, and the read row carries its cost.
    static long sinkOfBenchMixed(Bench.BenchMixed d) {
        long s = (long) d.sequence + d.ackSequence + d.ackBits + d.sessionId + d.clientId
                + d.nonce + d.worldTime + d.frameTick + d.serverTime
                + d.entitiesCount + d.statsCount + d.gameEvent.type
                + d.playerNameLength + d.payloadLength
                + Float.floatToRawIntBits(d.aimX) + Float.floatToRawIntBits(d.aimY)
                + Float.floatToRawIntBits(d.aimZ) + Float.floatToRawIntBits(d.recoil)
                + Double.doubleToRawLongBits(d.drift)
                + d.wideKey.hi + d.wideKey.lo + d.flux.hi + d.flux.lo
                + d.ping + d.crcHint + (d.hasExtra ? 1 : 0) + d.extra + d.idleTicks;
        for (int i = 0; i < d.entitiesCount; i++) {
            final Bench.MixedEntity e = d.entities[i];
            s += (long) e.entityId + e.posX + e.posY + e.posZ + e.yaw + e.pitch
                    + e.velX + e.velY + e.velZ + e.health + e.weapon + e.damage
                    + (e.moving ? 1 : 0) + (e.firing ? 1 : 0);
        }
        for (int i = 0; i < d.statsCount; i++) {
            s += (long) d.stats[i].statId + d.stats[i].delta;
        }
        final Bench.MixedHitEvent h = d.gameEvent.hit;
        s += (long) h.targetId + h.damage + h.hitKind + (h.crit ? 1 : 0);
        for (int i = 0; i < 4; i++) {
            s += d.loadout[i];
        }
        for (int i = 0; i < d.playerNameLength; i++) {
            s += d.playerName[i];
        }
        for (int i = 0; i < d.payloadLength; i++) {
            s += d.payload[i];
        }
        return s;
    }

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
            sink += sinkOfBenchPacket(packetDecoded); // full-struct observation (#175)
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
            sink += sinkOfBenchInts(intsDecoded); // full-struct observation (#175)
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
            sink += sinkOfBenchBits(bitsDecoded); // full-struct observation (#175)
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
            sink += sinkOfBenchMixed(mixedDecoded); // full-struct observation (#175)
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
