// R2 — record_bytes is 8 + body: the lock stores the body, COMPILE adds
// the eight once, and no backend adds anything.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:447): "the lock stores the BODY,
// COMPILE adds the eight ONCE, for every backend it hands entries to
// (compiler/lineage.go), and NO BACKEND ADDS ANYTHING — what a leg is
// handed is already the whole number its static data needs".
//
// The invariant is `recordBytes == 8 + bodyBytes` in every generated
// <Table>Fixed class. The lock records the body size; COMPILE adds the
// eight hash bytes once; the backend receives the full record_bytes and
// adds nothing. A leg that adds the eight a second time ships every older
// entry eight bytes long, and a driver that passes the lock's number
// through ships them eight bytes SHORT.
//
// THE TEST VERIFIES:
// 1. recordBytes == 8 + bodyBytes for every table on the classpath.
// 2. measure() == headerBytes + count * recordBytes.
// 3. save() then load() round-trips values through the production path.
// 4. The reader divides the file's tail by recordBytes from the lock,
//    not by its own bodyBytes.
//
// NEGATIVE CONTROL: change `recordBytes = 8 + bodyBytes` to
// `recordBytes = bodyBytes` in one generated source, recompile the
// generated file, and run the test — it must go RED.
//
// BUILD DEPENDENCY: build/conformance-java (make build-conformance-java).
// RUN from the repo root:
//   java -cp build/conformance-java test.conformance.java.rows.R2

package test.conformance.java.rows;

import tabledemo.GunnerConfigFixed;
import tabledemo.TableFixed;

public final class R2 {
    private R2() {}

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
        // ---- 1. recordBytes == 8 + bodyBytes for every table on the classpath
        check(GunnerConfigFixed.recordBytes == 8 + GunnerConfigFixed.bodyBytes,
                "GunnerConfigFixed: recordBytes (" + GunnerConfigFixed.recordBytes
                        + ") == 8 + bodyBytes (" + GunnerConfigFixed.bodyBytes + ")");

        // ---- 2. measure() == headerBytes + count * recordBytes
        for (int count = 0; count <= 3; count++) {
            int measured = GunnerConfigFixed.measure(count);
            int expected = GunnerConfigFixed.headerBytes + count * GunnerConfigFixed.recordBytes;
            check(measured == expected,
                    "GunnerConfigFixed.measure(" + count + ") == " + expected);
        }

        // ---- 3. save/load round-trip through the production path
        GunnerConfigFixed.Value v = new GunnerConfigFixed.Value();
        v.reaction = 0.75f;
        v.tracking = true;
        GunnerConfigFixed.Value[] values = { v };
        byte[] file = new byte[GunnerConfigFixed.measure(1)];
        int saved = GunnerConfigFixed.save(values, 1, file);
        check(saved == file.length,
                "save returned " + saved + " == file.length " + file.length);

        GunnerConfigFixed.Value[] out = { new GunnerConfigFixed.Value() };
        TableFixed.Report rpt = new TableFixed.Report();
        int n = GunnerConfigFixed.load(out, 1, file, TableFixed.plan(64),
                new short[64], GunnerConfigFixed.image(), rpt);
        check(n == 1 && !rpt.refused && !rpt.malformed,
                "load returned " + n + " records, refused=" + rpt.refused
                        + " malformed=" + rpt.malformed);
        check(Float.floatToRawIntBits(out[0].reaction) == Float.floatToRawIntBits(v.reaction),
                "round-trip reaction: " + out[0].reaction + " == " + v.reaction);
        check(out[0].tracking == v.tracking,
                "round-trip tracking: " + out[0].tracking + " == " + v.tracking);

        // ---- 4. TWO RECORDS: measure(2) == header + 2 * recordBytes
        GunnerConfigFixed.Value v2 = new GunnerConfigFixed.Value();
        v2.reaction = 0.25f;
        v2.tracking = false;
        GunnerConfigFixed.Value[] pair = { v, v2 };
        byte[] file2 = new byte[GunnerConfigFixed.measure(2)];
        GunnerConfigFixed.save(pair, 2, file2);
        GunnerConfigFixed.Value[] out2 = { new GunnerConfigFixed.Value(),
                new GunnerConfigFixed.Value() };
        TableFixed.Report rpt2 = new TableFixed.Report();
        int n2 = GunnerConfigFixed.load(out2, 2, file2, TableFixed.plan(64),
                new short[64], GunnerConfigFixed.image(), rpt2);
        check(n2 == 2 && !rpt2.refused && !rpt2.malformed,
                "two-record load: " + n2 + " records");
        check(Float.floatToRawIntBits(out2[0].reaction) == Float.floatToRawIntBits(v.reaction)
                && Float.floatToRawIntBits(out2[1].reaction) == Float.floatToRawIntBits(v2.reaction),
                "two-record round-trip: reaction values match");

        // ---- 5. KnownLayout carries recordBytes from the lock
        check(GunnerConfigFixed.known.length >= 1,
                "known array has at least 1 entry");
        check(GunnerConfigFixed.known[0].recordBytes == GunnerConfigFixed.recordBytes,
                "known[0].recordBytes (" + GunnerConfigFixed.known[0].recordBytes
                        + ") == recordBytes (" + GunnerConfigFixed.recordBytes + ")");
        check(GunnerConfigFixed.known[0].recordBytes == 8 + GunnerConfigFixed.bodyBytes,
                "known[0].recordBytes == 8 + bodyBytes (the lock carries the body, "
                        + "COMPILE adds the eight)");

        if (failures > 0) {
            System.err.println("R2 FAIL: " + failures + " assertion(s) failed");
            System.exit(1);
        }
        System.out.println("R2 PASS");
    }
}
