// R23 — the static data's member names and order — TableFixedKnownLayout =
// hash, layout, layout_bytes, record_bytes; the report's layout_hash last and
// zero on every other path (docs/roadmap.sexp java/R23, audit schema#898).
//
// The law (docs/FIXED-FORM-ALGORITHM.md §5.9 #19, #15):
//
//   §5.9 #19: One entry of R.known is TableFixedKnownLayout and it carries FOUR
//   members in this order — hash, layout, layout_bytes, record_bytes — the byte
//   length riding BESIDE the pointer rather than inside it, the way §4.1 names
//   the plan entry's lanes.
//
//   §5.9 #15: THE FILE'S HASH LANDS ON THE REPORT. layout_hash, LAST on the
//   report, zero on every other path — a new member at the end rather than a
//   field inserted among the counters, because the report is a struct callers
//   already hold. §5.3 requires both layout refusals to report the file's hash
//   and never said where it went, so the pilot and the C leg each invented the
//   name and happened to agree.
//
// Java cannot static_assert on field offsets the way C does (§5.9 #34 admits
// the leg's spelling), so the member names and order are asserted through the
// java.lang.reflect API: getDeclaredFields() returns the fields in declaration
// order on every JVM that runs this test (§8.4 of the JLS does not require it,
// but the leg's only JDK — HotSpot — preserves it, and this test's CI runs are
// pinned to one JDK across every OS). A compile that lands a different name or
// order goes RED here.
//
// The vector is built from the law and the PRODUCTION WRITER — CellFixed, the
// simplest fixed-form unit — whose every byte the law fixes. The production
// READER (CellFixed.load) drives the report.
//
// RUN: java -cp build/conformance-java test/conformance/java/rows/R23.java

import java.lang.reflect.Field;
import java.util.Arrays;
import tblv1.CellFixed;
import tblv1.TableFixed;

public final class R23 {
    private static int red = 0;

    private static void check(boolean ok, String what) {
        System.out.println((ok ? "ok: " : "RED: ") + what);
        if (!ok) { red++; }
    }

    public static void main(String[] args) {
        // ---- PART ONE: the static data's member names and order (§5.9 #19) -----

        final Field[] klFields = TableFixed.KnownLayout.class.getDeclaredFields();
        final String[] names = new String[klFields.length];
        for (int i = 0; i < klFields.length; i++) { names[i] = klFields[i].getName(); }

        check(names.length == 4,
                "KnownLayout carries exactly FOUR members: " + Arrays.toString(names));

        check("hash".equals(names[0]),
                "the first member is 'hash' (a long): the identity");
        check(long.class.equals(klFields[0].getType()),
                "'hash' is a long");

        check("layout".equals(names[1]),
                "the second member is 'layout' (a byte[]): the lock's layout bytes verbatim");
        check(byte[].class.equals(klFields[1].getType()),
                "'layout' is a byte[]");

        check("layoutBytes".equals(names[2]),
                "the third member is 'layoutBytes' (an int): the byte length beside the array");
        check(int.class.equals(klFields[2].getType()),
                "'layoutBytes' is an int");

        check("recordBytes".equals(names[3]),
                "the fourth member is 'recordBytes' (an int): the record's whole size from the lock");
        check(int.class.equals(klFields[3].getType()),
                "'recordBytes' is an int");

        // Verify the KnownLayout CONSTRUCTOR takes exactly the same four types in order.
        final Class<?>[] ctorParams = TableFixed.KnownLayout.class.getDeclaredConstructors()[0].getParameterTypes();
        check(ctorParams.length == 4
                && ctorParams[0].equals(long.class) && ctorParams[1].equals(byte[].class)
                && ctorParams[2].equals(int.class) && ctorParams[3].equals(int.class),
                "the KnownLayout constructor takes (long, byte[], int, int): the four members in order");

        // A production known entry from the generated code: build a real one and
        // prove every member is reachable.
        final TableFixed.KnownLayout entry = CellFixed.known[0];
        check(entry.hash == CellFixed.hash,
                "a production entry's hash equals its own build's own_hash");
        check(Arrays.equals(entry.layout, CellFixed.layout),
                "a production entry's layout is the lock's own bytes verbatim");
        check(entry.layoutBytes == CellFixed.layout.length,
                "a production entry's layoutBytes equals its layout array's length");
        check(entry.recordBytes == CellFixed.recordBytes,
                "a production entry's recordBytes equals the build's recordBytes");

        // ---- PART TWO: layout_hash LAST on the report (§5.9 #15) ----------------

        final Field[] rptFields = TableFixed.Report.class.getDeclaredFields();
        final String lastName = rptFields[rptFields.length - 1].getName();
        check("layoutHash".equals(lastName),
                "'layoutHash' is the LAST member of the Report, after "
                        + (rptFields.length - 1) + " others: " + lastName);
        check(long.class.equals(rptFields[rptFields.length - 1].getType()),
                "'layoutHash' is a long");

        // ---- PART THREE: zero on every other path (§5.9 #15) --------------------

        // Build a valid one-record file using the PRODUCTION WRITER.
        final byte[] file = new byte[CellFixed.measure(1)];
        final int recordsAt = CellFixed.writeHeader(file);
        final CellFixed.Value v = new CellFixed.Value();
        v.power = 7;
        CellFixed.writeBody(file, recordsAt + 8, v);
        TableFixed.put64(file, recordsAt, CellFixed.hash);

        final CellFixed.Value[] out = { new CellFixed.Value() };

        // A CLEAN LOAD: every counter zero, no refusal, layoutHash zero.
        {
            final TableFixed.Report r = new TableFixed.Report();
            final int n = CellFixed.load(out, 1, file, CellFixed.identityPlan(),
                    new short[8], CellFixed.image(), r);
            check(n == 1 && !r.refused && !r.malformed && r.reason == TableFixed.Reason.none
                    && r.layoutHash == 0,
                    "a clean load: n=" + n + " refused=" + r.refused + " reason=" + r.reason
                            + " layoutHash=" + r.layoutHash + " (want n=1, clean, layoutHash=0)");
        }

        // A LAYOUT REFUSAL (layoutNewer): the file's hash not in the lineage.
        // The report carries THE FILE'S HASH in layoutHash.
        {
            final byte[] lying = file.clone();
            for (int i = 0; i < 8; i++) { lying[TableFixed.hashAt + i] ^= (byte) 0xFF; }
            final long given = TableFixed.get64(lying, TableFixed.hashAt);
            final TableFixed.Report r = new TableFixed.Report();
            CellFixed.load(out, 1, lying, CellFixed.identityPlan(), new short[8],
                    CellFixed.image(), r);
            check(r.refused && r.reason == TableFixed.Reason.layoutNewer
                    && r.layoutHash == given && !r.malformed,
                    "layoutNewer carries THE FILE'S HASH (0x" + Long.toHexString(given)
                            + "L): refused=" + r.refused + " reason=" + r.reason
                            + " layoutHash=0x" + Long.toHexString(r.layoutHash)
                            + " malformed=" + r.malformed);
        }

        // A REFUSAL BY NAME THAT IS NOT A LAYOUT REFUSAL: noLayout, triggered by
        // corrupting the per-record hash. layoutHash must be ZERO.
        {
            final byte[] lying = file.clone();
            lying[recordsAt] ^= 0x01; // corrupt one byte of the record hash
            final TableFixed.Report r = new TableFixed.Report();
            CellFixed.load(out, 1, lying, CellFixed.identityPlan(), new short[8],
                    CellFixed.image(), r);
            check(r.refused && r.reason == TableFixed.Reason.noLayout
                    && r.layoutHash == 0,
                    "a noLayout refusal keeps layoutHash ZERO: refused=" + r.refused
                            + " reason=" + r.reason + " layoutHash=" + r.layoutHash);
        }

        // batchTooLarge: a refusal BY NAME that is not a layout refusal.
        // layoutHash must be ZERO.
        {
            final TableFixed.Report r = new TableFixed.Report();
            CellFixed.load(new CellFixed.Value[0], 1, file, CellFixed.identityPlan(),
                    new short[8], CellFixed.image(), r);
            check(r.refused && r.reason == TableFixed.Reason.batchTooLarge
                    && r.layoutHash == 0,
                    "batchTooLarge keeps layoutHash ZERO: refused=" + r.refused
                            + " reason=" + r.reason + " layoutHash=" + r.layoutHash);
        }

        System.out.println(red == 0 ? "PASS test/conformance/java/rows R23"
                : "FAIL test/conformance/java/rows R23 (" + red + " red)");
        if (red != 0) { System.exit(1); }
    }
}