// the tables bench — the Java runner, THE FIXED FORM's half (docs/SPEC-TABLES.md
// §3.4), a port of the C# leg's BenchFixed (bench/tables/cs/src/Program.cs).
//
// Measures ONE thing: the paired bench's sixty-four logical records written
// and read on the FIXED TABLE wire, form byte 3, through the generated fixed
// codec. The corpus is the C++ reference's (bench/paired/corpus/bench_fixed.bin
// beside its layout): ONE form-3 file of sixty-four records, so a save is one
// call over sixty-four values and a load is one call that lands sixty-four, and
// an op is ONE RECORD — the C# leg's own arithmetic, iterations stepping by
// sixty-four.
//
// CONTRACT (BENCH-STANDARD.md): fixed iteration counts, 1 discarded warmup run
// then 7 measured runs per (bench, path) — or exactly one measured run under
// --round K — median/min/max/spread over the measured runs; CSV v2 rows on
// stdout under --csv, a human table on stderr. --gate runs the goldens and
// stops. --indexed is accepted for the driver's sake and changes nothing: the
// fixed corpus is one file and carries no index.
//
// GOLDEN GATED (§1.5): before any clock the reference's file must load whole,
// clean, under this build's own hash (the identity path IS what is measured),
// its layout must be this build's layout byte for byte, and a save of what
// loaded must be the file again byte for byte. A runner that mismatches
// REFUSES to bench.
//
// JVM discipline is the packet runner's (bench/java/Main.java): one timed loop
// per path in its own method, the discarded warmup run carrying each to full
// C2 compilation, every loop's work draining into a static sink the bench
// publishes at exit under an env var the JIT cannot rule out. No -ea: the
// number a user gets is the number reported.
//
// THIS FILE IS SHAPE-BLIND. It names the generated ROOT type and nothing else:
// no field, no pinned value, no wire size. `make shape-gate` holds that.

public final class TableMain {
    private TableMain() {}

    static final int NUM_RECORDS = 64;
    static final int PLAN_CAPACITY = 8192;
    static final long FIXED_ITERS = 400000;

    static boolean csv = false;
    static boolean quick = false;
    static boolean gateOnly = false;
    static int numRuns = 7;
    static long iterations = 0;
    static String wireDir = "testdata/wire";
    static String variantDir = "bench/corpus/variants";

    static long sink = 0;

    // the CSV's own columns (BENCH-STANDARD.md §5.1): family `table` — the
    // table wire is a DIFFERENT wire over a different corpus; linkage class —
    // codec classfiles compiled beside the caller into one JVM; checks
    // contract — caller-error asserts dormant without -ea, wire-contract
    // validation unconditional in the reader; opt default (JIT); inline unknown.
    static final String CSV_SUFFIX = "table,class,contract,default,unknown";
    static final java.util.List<String> csvRows = new java.util.ArrayList<>();
    static final java.util.TreeMap<String, byte[]> goldensLoaded = new java.util.TreeMap<>();

    static double now() { return System.nanoTime() * 1e-9; }

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

    static void fail(String bench, String what) {
        System.err.println("GOLDEN GATE FAILED: " + bench + " " + what);
        System.err.println("reporting nothing.");
        System.exit(1);
    }

    static byte[] slurp(String path, String what) {
        try {
            final byte[] bytes = java.nio.file.Files.readAllBytes(java.nio.file.Path.of(path));
            goldensLoaded.put(what, bytes);
            return bytes;
        } catch (java.io.IOException e) {
            System.err.println("missing fixed corpus " + path
                    + " — run from the schema repo root (or pass --variant-dir)");
            System.exit(1);
            throw new IllegalStateException("unreachable");
        }
    }

    static double lastWriteMedian;
    static double lastRoundTripMedian;

    static void report(String bench, String path, long iters, double bytesPerOp, double[] rates) {
        final double[] sorted = rates.clone();
        java.util.Arrays.sort(sorted);
        final double median = sorted[sorted.length / 2];
        final double min = sorted[0];
        final double max = sorted[sorted.length - 1];
        final double spread = (max - min) / median * 100.0;
        final double mbps = median * bytesPerOp / (1024.0 * 1024.0);
        if ("write".equals(path)) {
            lastWriteMedian = median;
        } else if ("round_trip".equals(path)) {
            lastRoundTripMedian = median;
        }
        System.err.printf("%-18s %-11s %10.3f M msg/s %10.1f MB/s   (min %.3f, max %.3f, spread %.1f%%)%n",
                bench, path, median / 1e6, mbps, min / 1e6, max / 1e6, spread);
        if (csv) {
            csvRows.add(String.format("java,%s,%s,%d,%.2f,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
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

    // ---- the fixed form's leg -------------------------------------------------

    static final bench.FixedTableFixed.Value[] fixedValues = new bench.FixedTableFixed.Value[NUM_RECORDS];
    static final bench.FixedTableFixed.Value[] fixedOut = new bench.FixedTableFixed.Value[NUM_RECORDS];
    static final bench.TableFixed.Entry[] fixedPlan = bench.TableFixed.plan(PLAN_CAPACITY);
    static final short[] fixedRemap = new short[PLAN_CAPACITY];
    static final byte[] fixedImage = bench.FixedTableFixed.image();
    static final bench.TableFixed.Report fixedReport = new bench.TableFixed.Report();
    static byte[] fixedFile;
    static byte[] fixedTwin;

    static int fixedLoadAll(bench.FixedTableFixed.Value[] values, byte[] file) {
        return bench.FixedTableFixed.load(values, NUM_RECORDS, file, fixedPlan, fixedRemap, fixedImage, fixedReport);
    }

    static boolean fixedClean() {
        return !fixedReport.refused && !fixedReport.malformed && fixedReport.unknown == 0
                && fixedReport.kindMismatch == 0 && fixedReport.widened == 0 && fixedReport.clamped == 0;
    }

    static void gateFixed(String name) {
        for (int k = 0; k < NUM_RECORDS; k++) {
            fixedValues[k] = new bench.FixedTableFixed.Value();
            fixedOut[k] = new bench.FixedTableFixed.Value();
        }
        fixedFile = slurp(variantDir + "/bench_fixed.bin", "bench_fixed.bin");
        final byte[] layout = slurp(variantDir + "/bench_fixed.layout", "bench_fixed.layout");
        if (fixedFile.length == 0) {
            fail(name, "empty fixed corpus in " + variantDir);
        }
        // gate 1: the reference's layout IS this build's layout, byte for byte,
        // so the file reads under this build's own hash — the identity path.
        if (!java.util.Arrays.equals(layout, bench.FixedTableFixed.layout)) {
            fail(name, "the reference's layout is not this build's layout — the corpus would read as a stranger's");
        }
        // gate 2: the whole corpus loads, clean, and saves back byte-identical.
        if (fixedLoadAll(fixedValues, fixedFile) != NUM_RECORDS || !fixedClean()) {
            fail(name, "the fixed corpus did not load clean");
        }
        fixedTwin = new byte[bench.FixedTableFixed.measure(NUM_RECORDS)];
        if (bench.FixedTableFixed.save(fixedValues, NUM_RECORDS, fixedTwin) != fixedFile.length
                || !java.util.Arrays.equals(fixedTwin, fixedFile)) {
            fail(name, "round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
        }
        // gate 3: reused storage round-trips too, twice over.
        for (int pass = 0; pass < 2; pass++) {
            if (fixedLoadAll(fixedOut, fixedFile) != NUM_RECORDS || !fixedClean()
                    || bench.FixedTableFixed.save(fixedOut, NUM_RECORDS, fixedTwin) != fixedFile.length
                    || !java.util.Arrays.equals(fixedTwin, fixedFile)) {
                fail(name, "reused target round-trip bytes differ");
            }
        }
    }

    // WRITE: save the sixty-four loaded values, one file per step. The sink is
    // the byte fold: every step's result is a value the loop cannot drop.
    static double timeFixedWrite(long iters) {
        final double start = now();
        for (long i = 0; i < iters; i += NUM_RECORDS) {
            sink += bench.FixedTableFixed.save(fixedValues, NUM_RECORDS, fixedTwin);
        }
        return now() - start;
    }

    // ROUND-TRIP: load the file, then re-save what came out. The load needs no
    // sink of its own — its output IS the save's input.
    static double timeFixedRoundTrip(long iters) {
        final double start = now();
        for (long i = 0; i < iters; i += NUM_RECORDS) {
            if (fixedLoadAll(fixedOut, fixedFile) != NUM_RECORDS) {
                System.exit(1);
            }
            sink += bench.FixedTableFixed.save(fixedOut, NUM_RECORDS, fixedTwin);
        }
        return now() - start;
    }

    interface TimedLeg {
        double run(long iters);
    }

    static void benchLeg(String bench, String path, long iters, double bytesPerOp, TimedLeg leg) {
        final double[] rates = new double[numRuns];
        for (int run = -1; run < numRuns; run++) {
            final double elapsed = leg.run(iters);
            if (run >= 0) {
                rates[run] = iters / elapsed;
            }
        }
        report(bench, path, iters, bytesPerOp, rates);
    }

    static void reportDerivedRead(String bench) {
        if (lastWriteMedian <= 0 || lastRoundTripMedian <= 0) {
            return;
        }
        final double readTime = 1.0 / lastRoundTripMedian - 1.0 / lastWriteMedian;
        if (readTime > 0) {
            System.err.printf(
                    "%-18s %-11s %10.3f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)%n",
                    bench, "read", 1e-6 / readTime);
        }
    }

    public static void main(String[] args) {
        for (int i = 0; i < args.length; i++) {
            switch (args[i]) {
                case "--csv" -> csv = true;
                case "--quick" -> quick = true;
                case "--gate" -> gateOnly = true;
                case "--indexed" -> { }
                case "--wire-dir" -> {
                    if (i + 1 >= args.length) { usage(); }
                    wireDir = args[++i];
                }
                case "--variant-dir" -> {
                    if (i + 1 >= args.length) { usage(); }
                    variantDir = args[++i];
                }
                case "--iterations" -> {
                    if (i + 1 >= args.length) { usage(); }
                    try {
                        iterations = Long.parseLong(args[++i]);
                    } catch (NumberFormatException e) {
                        iterations = 0;
                    }
                    if (iterations <= 0 || iterations % NUM_RECORDS != 0 || iterations > 2147483584L) {
                        System.err.println("--iterations requires a positive multiple of 64 up to 2147483584");
                        System.exit(1);
                    }
                }
                case "--round" -> {
                    if (i + 1 >= args.length) { usage(); }
                    i++; // K only identifies the round to the driver
                    numRuns = 1;
                }
                default -> usage();
            }
        }
        if (quick && numRuns == 7) {
            numRuns = 3;
        }
        final long iters = iterations > 0 ? iterations : FIXED_ITERS;

        System.err.println("schema tables bench (java, the fixed form"
                + (quick ? ", --quick: iteration instrument, not certification" : "") + ")");
        gateFixed("bench_fixed");
        if (gateOnly) {
            System.err.println("OK (gate only, corpus_id " + corpusId() + ")");
            return;
        }
        final double bytesPerOp = (double) fixedFile.length / NUM_RECORDS;
        benchLeg("bench_fixed", "write", iters, bytesPerOp, TableMain::timeFixedWrite);
        benchLeg("bench_fixed", "round_trip", iters, bytesPerOp, TableMain::timeFixedRoundTrip);
        reportDerivedRead("bench_fixed");
        flushCsv();
        System.err.println("OK (corpus_id " + corpusId() + ")");
        if (System.getenv("SERIALIZE_BENCH_SINK") != null) {
            System.err.println("sink: " + sink);
        }
    }

    static void usage() {
        System.err.println("usage: TableMain [--indexed] [--gate] [--csv] [--quick] [--round K] [--iterations N] [--wire-dir <dir>] [--variant-dir <dir>]");
        System.exit(1);
    }
}
