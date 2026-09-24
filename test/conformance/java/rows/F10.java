// test/conformance/java/rows/F10.java
//
// cell java/F10 — "no_layout" (docs/FIXED-FORM-ALGORITHM.md §5.3 step 7; the
// schema matrix's plan-selection row). A fixed-form file whose per-record hash
// does not equal the file's own hash is refused BY NAME — no_layout — before
// any body byte is decoded and before any counter moves.
//
// The law (docs/FIXED-FORM-ALGORITHM.md:148):
//   | 7 | per record: if LE(8, at) != h, REFUSE no_layout; then §4.4 |
//
// The vector is built from the law, by the generated writer's own constants
// (build/tables-generated-java/v1, package tblv1, on the conformance
// classpath): the header is the form byte 3, seven reserved zero bytes, the
// layout hash at 8, the layout length at 16 and the layout itself; then one
// record of recordBytes = 8 + bodyBytes, whose first eight bytes are the
// per-record hash. Only that per-record hash is corrupted — a record hash a
// reader holds no layout for is exactly what step 7 refuses.
//
// Run from the repository root against the conformance classpath, the same one
// make/java.mk gives the conformance driver (build/conformance-java):
//   java -cp build/conformance-java test/conformance/java/rows/F10.java
// exit 0 green, exit 1 red, one printed line per assertion.

import tblv1.CellFixed;
import tblv1.TableFixed;

public final class F10 {
    private static int failures = 0;

    private static void check(boolean ok, String line) {
        System.out.println((ok ? "pass" : "FAIL") + ": " + line);
        if (!ok) {
            failures++;
        }
    }

    public static void main(String[] args) {
        // A valid one-record file, built with the generated writer's own
        // constants: header (form 3, reserved zeros, the layout hash at 8, the
        // layout length at 16, the layout) then one record. Its first eight
        // bytes, at `recordsAt`, are the per-record hash (LE(8, at)).
        byte[] file = new byte[CellFixed.measure(1)];
        int recordsAt = CellFixed.writeHeader(file);
        CellFixed.Value v = new CellFixed.Value();
        v.power = 7;
        CellFixed.writeBody(file, recordsAt + 8, v);
        TableFixed.put64(file, recordsAt, CellFixed.hash);

        // Step 7: if LE(8, at) != h, REFUSE no_layout. Flip one byte of the
        // record's hash so it names a layout this reader does not hold.
        file[recordsAt] ^= 0x01;

        CellFixed.Value[] values = new CellFixed.Value[1];
        values[0] = new CellFixed.Value();
        TableFixed.Report r = new TableFixed.Report();
        int n = CellFixed.load(values, 1, file,
                CellFixed.identityPlan(), new short[8], CellFixed.image(), r);

        check(n == -1, "a record whose hash names no layout is refused (load returned " + n + ")");
        check(r.refused, "the load refuses");
        check(r.reason == TableFixed.Reason.noLayout,
                "the refusal is by the name noLayout (got " + r.reason + ")");
        check(!r.malformed, "noLayout is a refusal, not malformed");
        check(r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0,
                "REFUSE is total: no counter moves");
        check(values[0].power == 0, "nothing decoded: the destination is untouched");

        System.exit(failures == 0 ? 0 : 1);
    }
}