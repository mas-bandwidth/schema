/*
    THE JAVA TABLES SOAK (docs/PORTING.md I9, #517).

    A functional soak, never a unit test: it loops the variable form's load,
    loadMeasure and save over the committed wires the tables-java-variable-class
    target round-trips, for SOAK_SECONDS of real time, and reads the thread's
    allocated-bytes counter every sample.

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

        double seconds = args.length > 0 ? Double.parseDouble(args[0]) : 20.0;
        Codec[] codecs = new Codec[] { new CatalogCodec(), new SceneCodec() };
        byte[][] wires = new byte[][] {
            Files.readAllBytes(Paths.get(args[1])),
            Files.readAllBytes(Paths.get(args[2]))
        };

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
        long start = System.nanoTime();
        long deadline = start + (long) (seconds * 1e9);
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
        while (System.nanoTime() < deadline) {
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
        long elapsedMs = (System.nanoTime() - start) / 1_000_000;
        System.out.println("soak: " + iterations + " iterations over " + codecs.length
                + " cases in " + elapsedMs + " ms");
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
        System.out.println("soak: drift " + (driftOk ? "OK" : "RED") + " (limit " + DRIFT_LIMIT + " bytes)");
        if (countFailure != null) {
            fail(countFailure + " (" + badSamples + " of " + (samples - warmedAt) + " post-warmup samples)");
        }
        if (!driftOk) {
            fail("resident heap drifted " + maxDrift + " bytes, past the " + DRIFT_LIMIT + " limit: retention");
        }
        if (!warmed || samples <= warmedAt) {
            fail("no post-warmup samples were measured (" + (samples - warmedAt)
                    + " post-warmup samples): a soak cannot certify flatness on warmup alone");
        }
        System.out.println("soak: OK — per-iteration allocation flat (within "
                + TOLERANCE + " bytes, " + (samples - warmedAt) + " post-warmup samples)");
    }
}
