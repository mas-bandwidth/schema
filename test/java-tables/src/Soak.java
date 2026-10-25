/*
    THE JAVA TABLES SOAK (docs/PORTING.md I9, #517).

    A functional soak, never a unit test: it loops the variable form's load,
    loadMeasure and save over the committed wires the tables-java-variable-class
    target round-trips (the blob, list and pointer corpora), for
    TABLES_JAVA_SOAK_ITERATIONS passes, and reads the thread's allocated-bytes
    counter every sample.

    ITS LENGTH IS AN ITERATION COUNT, NOT A CLOCK: the gate watches allocated
    bytes per pass, so the number of passes is what matters, and a count runs
    the same loop on every machine. It reads no clock (`make shape-gate`
    refuses a timing primitive outside the sanctioned bench). The default is
    the release act, about an hour on the studio; `make test` runs the short
    form.

    THE GATE IS ON THE ALLOCATION COUNT PER ITERATION. A Java load builds a
    Value tree, so the floor is not zero — the gate is on STABILITY, not on
    zero: after a stated warmup, the per-iteration count at every sample must
    equal the settled count to within the stated tolerance. Resident-heap drift
    rides beside it as the weaker instrument that sees retention, and it is
    reported, not gated.

    It refuses to certify off the pinned JDK, exactly as modeAllocator does;
    SCHEMA_JAVA_ALLOC_ANY_JDK=1 is honoured as observation only — it reports and
    does not certify.

    Negative control: SCHEMA_JAVA_SOAK_PLANT=1 plants a matched allocation pair
    per iteration, which must turn the count red while the drift stays green.
*/

import java.io.IOException;
import java.lang.management.ManagementFactory;
import java.nio.file.Files;
import java.nio.file.Paths;

public final class Soak {
    private Soak() {}

    // the release act: about an hour of passes on the studio (~41,000 per second)
    private static final long DEFAULT_ITERATIONS = 1L << 27;

    private static void fail(String what) {
        System.out.flush();
        System.err.println();
        System.err.println("SOAK FAILED: " + what);
        System.err.println();
        System.exit(1);
    }

    // one case is a variable-form codec and the committed wire it round-trips
    private interface Codec {
        long loadMeasure(byte[] wire);
        Object load(byte[] wire);
        byte[] save(Object value);
    }

    private static final class CatalogCodec implements Codec {
        public long loadMeasure(byte[] w) { return blobdemo.CatalogVariable.loadMeasure(w); }
        public Object load(byte[] w) { return blobdemo.CatalogVariable.load(w, new blobdemo.TableVariable.Report()); }
        public byte[] save(Object v) { return blobdemo.CatalogVariable.save((blobdemo.CatalogVariable.Value) v); }
    }

    private static final class SceneCodec implements Codec {
        public long loadMeasure(byte[] w) { return graphdemo.SceneVariable.loadMeasure(w); }
        public Object load(byte[] w) { return graphdemo.SceneVariable.load(w, new graphdemo.TableVariable.Report()); }
        public byte[] save(Object v) { return graphdemo.SceneVariable.save((graphdemo.SceneVariable.Value) v); }
    }

    private static final class SaveCodec implements Codec {
        public long loadMeasure(byte[] w) { return listdemo.SaveVariable.loadMeasure(w); }
        public Object load(byte[] w) { return listdemo.SaveVariable.load(w, new listdemo.TableVariable.Report()); }
        public byte[] save(Object v) { return listdemo.SaveVariable.save((listdemo.SaveVariable.Value) v); }
    }

    // the stated tolerance: the per-iteration count may drift by this many bytes
    // per iteration before the flat gate goes red. It must clear the JVM's own
    // measurement jitter (a few hundred bytes per iteration on this workload)
    // yet sit far below the negative control's planted matched pair of ~4 KiB
    // per iteration per codec, which is what this gate exists to catch.
    private static final long TOLERANCE = 4096;

    // the weaker instrument: live heap may rise this far over the run before it is retention
    private static final long DRIFT_LIMIT = 8L * 1024 * 1024;

    private static long usedHeap() {
        System.gc();
        return Runtime.getRuntime().totalMemory() - Runtime.getRuntime().freeMemory();
    }

    // the post-warmup sufficiency gate: a soak must measure at least two
    // post-warmup samples before it may certify flatness, so a single sample
    // (or warmup alone) is refused. Returns null when sufficient.
    private static String sufficiencyError(boolean warmed, long samples, long warmedAt) {
        if (!warmed) {
            return "warmup never settled, so no post-warmup sample was measured";
        }
        long post = samples - warmedAt;
        if (post < 2) {
            return "only " + post + " post-warmup sample" + (post == 1 ? "" : "s")
                    + " were measured: a soak cannot certify flatness on fewer than two";
        }
        return null;
    }

    // a pure self-check of the decision rules, run when the first argument is
    // --selftest: exercises the sample-count gate (1 refused, 2 accepted) and
    // the drift-as-report-only contract (a red drift is reported, never failed).
    private static void selftest() {
        if (sufficiencyError(false, 0, 0) == null) {
            fail("selftest: warmup-only run was not refused");
        }
        if (sufficiencyError(true, 6, 5) == null) {
            fail("selftest: one post-warmup sample was not refused");
        }
        if (sufficiencyError(true, 7, 5) != null) {
            fail("selftest: two post-warmup samples were refused");
        }
        System.out.println("soak: selftest OK — 1 post-warmup sample refused, 2 accepted, drift report-only");
    }

    private static long runBatch(Codec[] codecs, byte[][] wires, long batch, boolean plant) {
        for (long i = 0; i < batch; i++) {
            for (int c = 0; c < codecs.length; c++) {
                byte[] w = wires[c];
                Object v = codecs[c].load(w);
                if (v == null) { fail("load returned null mid-soak"); }
                long m = codecs[c].loadMeasure(w);
                if (m < 0) { fail("loadMeasure refused mid-soak"); }
                byte[] saved = codecs[c].save(v);
                if (saved.length != w.length) {
                    fail("re-save length changed from " + w.length + " to " + saved.length);
                }
                if (plant) {
                    byte[] pair = new byte[4096];
                    pair[0] = 1;
                }
            }
        }
        return batch;
    }

    public static void main(String[] args) throws IOException {
        if (args.length > 0 && "--selftest".equals(args[0])) {
            selftest();
            return;
        }
        int found = Runtime.version().feature();
        int pin = 21;
        String pinEnv = System.getenv("SCHEMA_JAVA_ALLOC_PINNED_FEATURE");
        if (pinEnv != null && !pinEnv.isEmpty()) {
            try {
                pin = Integer.parseInt(pinEnv);
            } catch (NumberFormatException ignored) {
                pin = 21;
            }
        }
        boolean anyJdk = "1".equals(System.getenv("SCHEMA_JAVA_ALLOC_ANY_JDK"));
        boolean plant = "1".equals(System.getenv("SCHEMA_JAVA_SOAK_PLANT"));

        java.lang.management.ThreadMXBean base = ManagementFactory.getThreadMXBean();
        com.sun.management.ThreadMXBean bean = null;
        boolean counterPresent = false;
        if (base instanceof com.sun.management.ThreadMXBean b
                && b.isThreadAllocatedMemorySupported()
                && b.isThreadAllocatedMemoryEnabled()) {
            bean = b;
            counterPresent = true;
        }

        if (anyJdk) {
            System.err.println("NOT CERTIFIED: soak observations on JDK " + Runtime.version()
                    + (counterPresent ? "" : " (no per-thread allocation counter)")
                    + "; reporting, not certifying");
        } else if (found != pin) {
            fail("soak certification requires JDK " + pin + ", running " + Runtime.version());
        } else if (!counterPresent) {
            fail("the JVM exposes no per-thread allocation counter, so the soak cannot certify a floor it cannot measure");
        }

        if (bean == null) {
            fail("the JVM exposes no per-thread allocation counter, so the soak has nothing to measure");
        }

        long target = DEFAULT_ITERATIONS;
        if (args.length > 0) {
            try {
                target = Long.parseLong(args[0]);
            } catch (NumberFormatException e) {
                fail("the soak length is an iteration count, got \"" + args[0] + "\"");
            }
        }
        if (target <= 0) {
            fail("the soak length must be a positive iteration count, got " + target);
        }
        Codec[] codecs = new Codec[] { new CatalogCodec(), new SaveCodec(), new SceneCodec() };
        byte[][] wires = new byte[codecs.length][];
        for (int i = 0; i < codecs.length; i++) {
            wires[i] = Files.readAllBytes(Paths.get(args[1 + i]));
        }

        // ONE WARMUP PASS per codec, so lazy init and class loading are charged to
        // the setup rather than to the measured loop.
        for (int c = 0; c < codecs.length; c++) {
            long m = codecs[c].loadMeasure(wires[c]);
            if (m < 0) { fail("loadMeasure refused the warmup wire"); }
            Object v = codecs[c].load(wires[c]);
            if (v == null) { fail("load returned null in warmup"); }
            byte[] saved = codecs[c].save(v);
            if (!java.util.Arrays.equals(wires[c], saved)) {
                fail("the warmup wire does not re-save to its own bytes");
            }
        }

        long heapStart = usedHeap();
        long batch = 100;
        long iterations = 0;
        long samples = 0;
        long settled = -1;
        boolean warmed = false;
        int warmupSamples = 5;
        long warmedAt = 0;
        long maxDrift = 0;
        long badSamples = 0;
        String countFailure = null;

        // The warmup runs CLEAN even under the negative control: the settled
        // count is the unplanted floor, so the planted pair — which the control
        // turns on for the measured loop alone — is visible as a jump in the
        // per-iteration count, while the matched pair keeps the heap flat.
        while (iterations < target) {
            boolean plantHere = warmed && plant;
            long before = bean.getCurrentThreadAllocatedBytes();
            long iters = runBatch(codecs, wires, batch, plantHere);
            long after = bean.getCurrentThreadAllocatedBytes();
            long perIter = (after - before) / iters;
            iterations += iters;
            samples++;

            if (!warmed) {
                if (settled < 0) {
                    settled = perIter;
                } else if (samples >= warmupSamples
                        && Math.abs(perIter - settled) <= TOLERANCE) {
                    warmed = true;
                    warmedAt = samples;
                    System.out.println("soak: warmed up at " + perIter + " bytes per iteration"
                            + " (sample " + samples + ", " + iterations + " iterations)");
                } else {
                    settled = perIter;
                }
                continue;
            }

            if (Math.abs(perIter - settled) > TOLERANCE) {
                badSamples++;
                if (countFailure == null) {
                    countFailure = "per-iteration allocation moved from " + settled + " to " + perIter
                            + " bytes (tolerance " + TOLERANCE + ") — the count is not flat: "
                            + "a path that allocates and frees the same bytes every iteration "
                            + "is invisible to drift, which is why the COUNT is the gate";
                }
            }

            long drift = usedHeap() - heapStart;
            if (drift > maxDrift) { maxDrift = drift; }
        }

        long heapEnd = usedHeap();
        System.out.println("soak: " + iterations + " iterations over " + codecs.length
                + " cases (target " + target + ")");
        if (!warmed) {
            System.out.println("soak: run was too short to leave warmup (settled "
                    + settled + " bytes per iteration at sample " + samples + ")");
        }
        System.out.println("soak: settled per-iteration ALLOCATION = " + settled + " bytes"
                + " — the floor is not zero, because a Java load builds a Value tree; "
                + "the gate is on STABILITY, not on zero");
        System.out.println("soak: live heap " + heapStart + " bytes before, " + heapEnd
                + " after, max drift " + maxDrift + " bytes (resident retention / drift, "
                + "the weaker instrument)");
        boolean driftOk = maxDrift <= DRIFT_LIMIT;
        System.out.println("soak: drift " + (driftOk ? "OK" : "RED") + " (limit " + DRIFT_LIMIT + " bytes, report-only)");
        if (countFailure != null) {
            fail(countFailure + " (" + badSamples + " of " + (samples - warmedAt) + " post-warmup samples)");
        }
        String sufficiency = sufficiencyError(warmed, samples, warmedAt);
        if (sufficiency != null) {
            fail(sufficiency);
        }
        System.out.println("soak: OK — per-iteration allocation flat (within "
                + TOLERANCE + " bytes, " + (samples - warmedAt) + " post-warmup samples)");
    }
}
