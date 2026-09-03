// the tables bench — the Java runner.
//
// A port of bench/tables/cpp/table_main.cpp (the reference implementation)
// against the generated Java table codec: same corpus, same golden gate, same
// per-variant round-trip gate before any clock, same 1 warmup + 7 measured runs
// with median/min/max/spread, same CSV v2 rows with lang=java and family=table.
//
// The Java table codec names NO runtime — `make tables-java-standalone` gates
// that — so this leg compiles the generated sources beside itself and links
// nothing. That is the one contract difference from bench/java/Main.java, which
// compiles against the generated packet codecs of a different corpus.
//
// Language-specific discipline, the same choices the type leg made:
//   - escape barriers: a static sink accumulates observed byte counts and the
//     bench publishes it at exit under an env var the JIT cannot rule out, so no
//     loop can be proven unobservable
//   - the read path loads into ONE reused instance, reset first — the tolerant
//     wire elides a field at its default, so resetting is part of a correct read
//     into reused storage and stays inside the clock
//   - and the READER and the WRITER are reused too, which is this port's own
//     contract: a nested body moves the reader's limit instead of slicing a
//     sub-reader, so a read into reused storage allocates nothing at all
//     (`make tables-java-alloc` measures exactly that, at zero)
//   - the warmup run per path doubles as the JIT warmup, and each timed loop
//     lives in its own method so a shared loop cannot pool its type profile
//
// THIS FILE IS SHAPE-BLIND: it names the generated type at its call sites and
// nothing else — no field, no pinned value, no wire size.

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class Main {
    private Main() {}

    static final int MAX_NUM_RUNS = 7;      // median of 7 (N >= 5), after 1 warmup run
    static int numRuns = MAX_NUM_RUNS;      // --round K drops this to 1 (§2.4)
    static final int NUM_VARIANTS = 64;     // read-path variant buffers

    // CSV v2 (BENCH-STANDARD.md §5.1). family `table` (§1.9) per row. linkage
    // class — the generated codec's classfiles are compiled beside the caller
    // into one JVM with no library boundary, which is the recorded packaging
    // fact for this leg. checks contract — the reader's wire-contract validation
    // is unconditional while caller-error asserts are dormant (§3.4). opt
    // default (the JIT has no operator-visible optimization levels). inline
    // unknown: a JIT leg has no AOT artifact the §4 verdict pass could walk.
    static final String CSV_SUFFIX = "table,class,contract,default,unknown";

    static final java.util.List<String> csvRows = new java.util.ArrayList<>();
    static final java.util.TreeMap<String, byte[]> goldensLoaded = new java.util.TreeMap<>();

    static String corpusId() {
        long h = 0xcbf29ce484222325L;
        for (java.util.Map.Entry<String, byte[]> g : goldensLoaded.entrySet()) {
            for (byte b : g.getKey().getBytes(StandardCharsets.UTF_8)) {
                h = (h ^ (b & 0xff)) * 0x100000001b3L;
            }
            h = (h ^ 0) * 0x100000001b3L;
            for (byte b : g.getValue()) {
                h = (h ^ (b & 0xff)) * 0x100000001b3L;
            }
        }
        return String.format("%016x", h);
    }

    static void flushCsv() {
        if (!csv) {
            return;
        }
        if (failed) {
            // §1.5: a failing run emits NO rows.
            System.err.println("refusing to emit CSV rows from a failing run");
            return;
        }
        String id = corpusId();
        StringBuilder out = new StringBuilder();
        for (String row : csvRows) {
            out.append(row).append(',').append(id).append(',').append(CSV_SUFFIX).append('\n');
        }
        System.out.print(out);
    }

    // The tolerant wire spends bytes on ids, kinds and lengths, so a table
    // record is several times its equivalent type's. The record size comes from
    // the corpus at run time; this is only the ceiling the runner refuses past.
    static final int BUFFER_SIZE = 65536;
    static final int VARIANT_STRIDE = BUFFER_SIZE + 64;  // §2.7 stride policy, as in every leg

    static final byte[] buffer = new byte[BUFFER_SIZE];
    static final byte[] twin = new byte[BUFFER_SIZE];
    static final byte[][] variants = new byte[NUM_VARIANTS][];

    static long sink;  // defeats dead code elimination of computed values
    static boolean csv;
    static String wireDir = Paths.get("testdata", "wire").toString();
    static String variantDir = Paths.get("bench", "corpus", "variants").toString();
    static boolean failed;

    static void fail(String name, String what) {
        System.err.println("FAILED: " + name + ": " + what);
        failed = true;
    }

    static double now() {
        return System.nanoTime() * 1e-9;
    }

    static void report(String bench, String path, long iters, long bytesPerOp, double[] rates) {
        double[] sorted = rates.clone();
        java.util.Arrays.sort(sorted);
        double median = sorted[sorted.length / 2];
        double min = sorted[0];
        double max = sorted[sorted.length - 1];
        double spread = (max - min) / median * 100.0;
        double mbps = median * bytesPerOp / (1024.0 * 1024.0);
        if ("write".equals(path)) {
            lastWriteMedian = median;
        } else if ("round_trip".equals(path)) {
            lastRoundTripMedian = median;
        }
        System.err.printf("%-18s %-11s %10.3f M msg/s %10.1f MB/s   (min %.3f, max %.3f, spread %.1f%%)%n",
                bench, path, median / 1e6, mbps, min / 1e6, max / 1e6, spread);
        if (csv) {
            csvRows.add(String.format("java,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
                    bench, path, iters, bytesPerOp, rates.length, median, min, max, mbps, spread));
        }
    }

    static double lastWriteMedian;
    static double lastRoundTripMedian;

    static boolean checkGolden(String name, byte[] data, long bytes) throws IOException {
        Path path = Path.of(wireDir, name + ".bin");
        if (!Files.exists(path)) {
            System.err.println("missing wire golden " + path +
                    " — run from the schema repo root (or pass --wire-dir)");
            return false;
        }
        byte[] expected = Files.readAllBytes(path);
        goldensLoaded.put(name + ".bin", expected);
        if (expected.length != bytes) {
            System.err.println("WIRE GOLDEN MISMATCH: " + path + " (" + expected.length +
                    " golden vs " + bytes + " actual bytes)");
            return false;
        }
        for (int i = 0; i < bytes; i++) {
            if (expected[i] != data[i]) {
                System.err.println("WIRE GOLDEN MISMATCH: " + path + " differs at byte " + i +
                        " — refusing to bench code that does not match the corpus");
                return false;
            }
        }
        return true;
    }

    // Records are fixed-width by construction — test/bench/table_main.cpp
    // refuses to emit a corpus whose records differ — so the record size IS
    // file size / NUM_VARIANTS.
    static long loadVariants(String name) throws IOException {
        Path path = Path.of(variantDir, name + ".variants.bin");
        if (!Files.exists(path)) {
            System.err.println("missing variant data " + path +
                    " — run `make bench-table-corpus`, and run from the schema repo root (or pass --variant-dir)");
            return -1;
        }
        byte[] packed = Files.readAllBytes(path);
        if (packed.length == 0 || packed.length % NUM_VARIANTS != 0) {
            System.err.println("variant data " + path + " is " + packed.length +
                    " bytes, not a multiple of " + NUM_VARIANTS + " records");
            return -1;
        }
        int record = packed.length / NUM_VARIANTS;
        if (record > BUFFER_SIZE) {
            System.err.println("variant data " + path + " has " + record +
                    "-byte records, over the " + BUFFER_SIZE + "-byte buffer");
            return -1;
        }
        for (int k = 0; k < NUM_VARIANTS; k++) {
            variants[k] = new byte[VARIANT_STRIDE];
            System.arraycopy(packed, k * record, variants[k], 0, record);
        }
        goldensLoaded.put(path.getFileName().toString(), packed);
        return record;
    }

    // ------------------------------------------------------------------
    // the data-driven table driver
    // ------------------------------------------------------------------

    interface Make<T> { T make(); }
    interface Reset<T> { void reset(T value); }
    interface Save<T> { long save(T value, byte[] into); }
    interface Load<T> { boolean load(T value, byte[] bytes, int length); }

    // THE TWO TIMED LOOPS live in their own methods, one per direction: a shared
    // generic loop pools its type profile across call sites, goes megamorphic
    // and turns the timings bimodal — the same discipline bench/java/Main.java
    // states for the type leg (issue #156 item 5).

    static <T> double writeLoop(String name, T[] instances, Save<T> save, long iters, long bytesPerOp) {
        double start = now();
        for (long i = 0; i < iters; i++) {
            long wrote = save.save(instances[(int) (i & (NUM_VARIANTS - 1))], buffer);
            if (wrote != bytesPerOp) {
                fail(name, "save failed in loop");
                return -1;
            }
            sink += wrote;
        }
        return iters / (now() - start);
    }

    static <T> double roundTripLoop(String name, T out, Reset<T> reset, Load<T> load, Save<T> save,
                                    long iters, long bytesPerOp) {
        double start = now();
        for (long i = 0; i < iters; i++) {
            reset.reset(out);
            if (!load.load(out, variants[(int) (i & (NUM_VARIANTS - 1))], (int) bytesPerOp)) {
                fail(name, "load failed in loop");
                return -1;
            }
            long wrote = save.save(out, buffer);
            if (wrote != bytesPerOp) {
                fail(name, "re-save failed in loop");
                return -1;
            }
            sink += wrote;
        }
        return iters / (now() - start);
    }

    @SuppressWarnings("unchecked")
    static <T> void benchTable(String name, String golden, long iters,
                               Make<T> make, Reset<T> reset, Save<T> save, Load<T> load)
            throws IOException {
        long bytesPerOp = loadVariants(name);
        if (bytesPerOp < 0) {
            failed = true;
            return;
        }

        // gate 1 (§1.5): variant 0 IS the pinned instance.
        if (!checkGolden(golden, variants[0], bytesPerOp)) {
            failed = true;
            return;
        }

        // gate 2: every variant loads, re-saves, and comes back byte-identical
        // at the same length — before any clock starts.
        Object[] instances = new Object[NUM_VARIANTS];
        for (int k = 0; k < NUM_VARIANTS; k++) {
            T value = make.make();
            instances[k] = value;
            reset.reset(value);
            if (!load.load(value, variants[k], (int) bytesPerOp)) {
                fail(name, "load of a variant failed");
                return;
            }
            long wrote = save.save(value, twin);
            if (wrote != bytesPerOp) {
                fail(name, "variant round-trip length differs — refusing to bench a codec that does not reproduce the corpus");
                return;
            }
            for (int i = 0; i < bytesPerOp; i++) {
                if (twin[i] != variants[k][i]) {
                    fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
                    return;
                }
            }
        }

        double[] writeRates = new double[numRuns];
        double[] roundtripRates = new double[numRuns];

        // WRITE: save the 64 pre-loaded instances round-robin (§2.7 variation).
        for (int run = -1; run < numRuns; run++) {
            double rate = writeLoop(name, (T[]) instances, save, iters, bytesPerOp);
            if (rate < 0) {
                return;
            }
            if (run >= 0) {
                writeRates[run] = rate;
            }
        }

        // ROUND-TRIP: reset, load a variant buffer, re-save what came out. The
        // load's output IS the save's input, so every loaded field is observed
        // by construction (§2.7's read-side sink problem dissolved).
        T out = make.make();
        for (int run = -1; run < numRuns; run++) {
            double rate = roundTripLoop(name, out, reset, load, save, iters, bytesPerOp);
            if (rate < 0) {
                return;
            }
            if (run >= 0) {
                roundtripRates[run] = rate;
            }
        }

        report(name, "write", iters, bytesPerOp, writeRates);
        double writeMedian = lastWriteMedian;
        report(name, "round_trip", iters, bytesPerOp, roundtripRates);
        double roundTripMedian = lastRoundTripMedian;

        // READ is DERIVED, never measured (§2.9): stderr only, never a row.
        double readTime = 1.0 / roundTripMedian - 1.0 / writeMedian;
        if (readTime > 0) {
            System.err.printf("%-18s %-11s %10.3f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)%n",
                    name, "read", 1e-6 / readTime);
        }
    }

    public static void main(String[] args) throws IOException {
        for (int i = 0; i < args.length; i++) {
            if (args[i].equals("--csv")) {
                csv = true;
            } else if (args[i].equals("--wire-dir") && i + 1 < args.length) {
                wireDir = args[++i];
            } else if (args[i].equals("--variant-dir") && i + 1 < args.length) {
                variantDir = args[++i];
            } else if (args[i].equals("--round") && i + 1 < args.length) {
                String k = args[++i];
                try {
                    if (Integer.parseInt(k) < 0) {
                        throw new NumberFormatException(k);
                    }
                } catch (NumberFormatException e) {
                    System.err.println("--round takes a non-negative integer, got '" + k + "'");
                    System.exit(1);
                }
                numRuns = 1;
            } else {
                System.err.println("usage: Main [--csv] [--round K] [--wire-dir <dir>] [--variant-dir <dir>]");
                System.exit(1);
            }
        }

        System.err.println("schema tables bench (java)");

        if (csv) {
            System.out.println("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," +
                    "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline");
        }

        // The one measured shape, named once — the generated type at the call
        // site and nothing else about it (bench/SHAPE-GATE.allow). The reader,
        // the writer and the report are hoisted here because they are the
        // CALLER's under this port's contract, and a leg that allocated one per
        // record would be timing the allocator.
        final benchtable.TableReport report = new benchtable.TableReport();
        final benchtable.TableReader reader = new benchtable.TableReader();
        final benchtable.TableWriter writer = new benchtable.TableWriter();
        Main.<benchtable.BenchTableTable.TableMixed>benchTable(
                "bench_table", "bench_table", 400000L,
                benchtable.BenchTableTable.TableMixed::new,
                benchtable.BenchTableTable::tableMixedReset,
                (v, into) -> {
                    writer.reset(into, 0, into.length);
                    return benchtable.BenchTableTable.tableMixedSaveBody(writer, v) ? writer.offset : -1;
                },
                (v, bytes, length) -> {
                    reader.reset(bytes, 0, length, report.clear());
                    return benchtable.BenchTableTable.tableMixedLoadBody(reader, v) && !report.malformed;
                });

        flushCsv();

        if (failed) {
            System.err.println("TABLES BENCH FAILED (corpus_id " + corpusId() + ")");
            System.exit(1);
        }

        System.err.println("OK (corpus_id " + corpusId() + ")");
        if (System.getenv("SCHEMA_BENCH_PUBLISH_SINK") != null) {
            System.err.println("sink " + sink);
        }
    }
}
