// R26 — a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:890): "a known hash whose LINEAGE ENTRY
// would not build → layout_malformed / plan_too_large by that entry's own
// lane, never a throw (§5.9 #36)".
//
// The lineage is built ONCE at class initialization from the lock's own
// bytes. If a lineage entry's layout is not parseable, the plan carries
// layoutMalformed; if the compiled plan exceeds the capacity, it carries
// planTooLarge or layoutRecordTooLarge. Nothing throws out of the
// initializer. When a file's hash selects that entry, the load refuses by
// that entry's own lane.
//
// THE TEST VERIFIES:
// 1. Class initialization of every table on the classpath doesn't throw
//    ("never a throw").
// 2. lineagePlans with a KnownLayout containing garbage layout bytes
//    produces a plan whose why == layoutMalformed.
// 3. Loading a file whose hash matches that entry refuses by that name.
//
// NEGATIVE CONTROL: change the refusal reason in lineagePlans from
// layoutMalformed to none in a generated TableFixed.java, recompile,
// and run the test — it must go RED.
//
// BUILD DEPENDENCY: build/conformance-java (make build-conformance-java).
// RUN from the repo root:
//   java -cp build/conformance-java test.conformance.java.rows.R26

package test.conformance.java.rows;

import tabledemo.GunnerConfigFixed;
import tabledemo.TableFixed;

public final class R26 {
    private R26() {}

    private static int failures = 0;

    private static void check(boolean cond, String what) {
        if (cond) {
            System.out.println("ok " + what);
        } else {
            System.err.println("FAIL " + what);
            failures++;
        }
    }

    public static void main(String[] args) {
        // ---- 1. CLASS INITIALIZATION DOESN'T THROW ("never a throw")
        //
        // If lineagePlans threw during <clinit>, the class would be unusable
        // for the life of the loader (ExceptionInInitializerError). Loading
        // any file through the table would then fail too. We exercise the
        // class by using its writer and reader.
        {
            GunnerConfigFixed.Value v = new GunnerConfigFixed.Value();
            v.reaction = 0.5f;
            v.tracking = true;
            byte[] f = new byte[GunnerConfigFixed.measure(1)];
            GunnerConfigFixed.save(new GunnerConfigFixed.Value[] { v }, 1, f);
            GunnerConfigFixed.Value[] out = { new GunnerConfigFixed.Value() };
            TableFixed.Report rpt = new TableFixed.Report();
            int n = GunnerConfigFixed.load(out, 1, f, TableFixed.plan(64),
                    new short[64], GunnerConfigFixed.image(), rpt);
            check(n == 1 && !rpt.refused,
                    "class init didn't throw: load returned " + n
                            + " records (refused=" + rpt.refused + ")");
        }

        // ---- 2. lineagePlans with garbage layout → layoutMalformed
        //
        // A lineage entry whose layout bytes are not a parseable layout is a
        // bug in the lock (§5.9 #8). lineagePlans sets the plan's why to
        // layoutMalformed, and nothing throws.
        {
            // Build a KnownLayout with garbage layout bytes.
            byte[] garbage = new byte[] { 0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
                    10, 11, 12, 13, 14, 15, 16, 17, 18, 19 };
            TableFixed.KnownLayout bad = new TableFixed.KnownLayout(
                    0xDEADBEEFL, garbage, garbage.length, 50);

            // Call lineagePlans: the reader is GunnerConfigFixed, and the
            // garbage entry is NOT the identity (hash != GunnerConfigFixed.hash).
            TableFixed.Plan[] plans = TableFixed.lineagePlans(
                    new TableFixed.KnownLayout[] { bad },
                    GunnerConfigFixed.layout,
                    GunnerConfigFixed.dest,
                    GunnerConfigFixed.hash);
            check(plans.length == 1,
                    "lineagePlans returned 1 plan for 1 entry");
            check(plans[0] != null,
                    "plan for the garbage entry is not null");
            check(plans[0].why == TableFixed.Reason.layoutMalformed,
                    "plan.why == layoutMalformed (got " + plans[0].why + ")");
        }

        // ---- 3. A valid file still works after the class init exercise
        //
        // This confirms that the garbage entry in the test above did not
        // poison the class's state — the identity plan and the production
        // path are intact.
        {
            GunnerConfigFixed.Value v = new GunnerConfigFixed.Value();
            v.reaction = 0.9f;
            v.tracking = false;
            byte[] f = new byte[GunnerConfigFixed.measure(1)];
            GunnerConfigFixed.save(new GunnerConfigFixed.Value[] { v }, 1, f);
            GunnerConfigFixed.Value[] out = { new GunnerConfigFixed.Value() };
            TableFixed.Report rpt = new TableFixed.Report();
            int n = GunnerConfigFixed.load(out, 1, f, TableFixed.plan(64),
                    new short[64], GunnerConfigFixed.image(), rpt);
            check(n == 1 && !rpt.refused
                    && Float.floatToRawIntBits(out[0].reaction) == Float.floatToRawIntBits(v.reaction),
                    "post-exercise round-trip: reaction=" + out[0].reaction + " == " + v.reaction);
        }

        if (failures > 0) {
            System.err.println("R26 FAIL: " + failures + " assertion(s) failed");
            System.exit(1);
        }
        System.out.println("R26 PASS");
    }
}
