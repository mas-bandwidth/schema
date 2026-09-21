// THE JAVA FIXED-FORM WIDE-TEXT CODE UNITS TEST.
//
// Cell java/C5: schema matrix row "wide text code units".
// The law (docs/FIXED-FORM-ALGORITHM.md:329, the `text` row of §4.5's table):
//
//   unit := (meta == wide) ? 2 : 1;
//   cap  := size / unit;
//   v    := SLE(4, record+src) clamped into [0, cap], COUNT clamped if it fired.
//
// `v` is the USED LENGTH IN CODE UNITS, and the clamp uses CODE UNITS, not
// BYTES. An astral pair is TWO code units (surrogate high + surrogate low)
// counted as two, never as one byte pair, never as 2N bytes (fix 7).
//
// THE TEST READS A PLAN OVER HAND-BUILT BYTES, calling the production
// runtime directly: the same TableFixed.run() the fixed-form corpus calls,
// not a second copy of the code. The classpath make/java.mk gives the
// conformance driver carries this class.
//
// A FIELD DECLARED wstring(4) has size = 4 (the declaration's units), a
// buffer of 2 * 4 = 8 bytes, and cap = size / unit = 4 / 2 = 2 code units.
//
// Three cases:
//   1. used = 2 (astral pair written as 4 bytes): clamp does not fire,
//      the length lands as 2 in the image's length word.
//   2. used = 3 (writer said three code units, beyond the bound of 2):
//      clamp fires, the length lands clamped to 2, report.clamped == 1.
//      THIS IS THE BITE: a reader that divided cap by 2N instead of unit
//      would land 3 unchanged. The fix the cell is for (7) is exactly that
//      distinction.
//   3. used = -1 (the four-byte length is negative): clamp fires as the
//      count side of the spec's `v < 0` branch, length lands as 0,
//      report.clamped == 1.
//
// If any case fails the test prints the failing case to stderr and returns 1;
// on success each case prints a single line to stdout.
//
// RUN from the repo root:
//
//   java -cp <build/conformance-java:build/tables-generated-java/examples>
//        test.conformance.java.rows.C5
//
// The classpath is exactly the one make/java.mk names for the Java
// conformance driver; build/tables-generated-java/examples is on it so
// TableFixed is reachable.

package test.conformance.java.rows;

import tabledemo.TableFixed;

public final class C5 {
    private C5() {}

    private static int failures = 0;

    private static void check(String label, boolean cond) {
        if (cond) {
            System.out.println("ok " + label);
        } else {
            System.err.println("FAIL " + label);
            failures++;
        }
    }

    // Build one record body whose single field is a wstring(4):
    //   [length: u32 LE] [2N bytes: wstring payload]
    // The plan has ONE entry, an opText with meta = textWide.
    //
    // size = N = 4 (the wstring's DECLARED size, in CODE UNITS, the same
    // number the C++ reference stores in the layout entry and the same
    // number the runtime uses as `size` in opText).
    private static byte[] record(int used) {
        byte[] b = new byte[4 + 2 * 4];
        // length as 4-byte little-endian; used may be any int (incl. negative)
        b[0] = (byte) (used & 0xff);
        b[1] = (byte) ((used >> 8) & 0xff);
        b[2] = (byte) ((used >> 16) & 0xff);
        b[3] = (byte) ((used >> 24) & 0xff);
        // payload: every code unit as 0x0041 ("A") so an astral pair written
        // as 0xD83D 0xDE40 (four bytes) reads as two code units, never as
        // one. Fill with 0x00 0x41 everywhere -- the byte values do not
        // matter for opText; the count is what the test is about.
        for (int i = 0; i < 2 * 4; i += 2) {
            b[4 + i] = 0x00;
            b[4 + i + 1] = 0x41;
        }
        return b;
    }

    // Run the runtime over the source bytes and return the resulting image's
    // length word plus the report counters. The image has 8 bytes (4 for the
    // length, 4 for a single int placeholder); the wstring's LENGTH lands
    // at offset 0 of the image and its BUFFER at offset 4.
    private static int runOnce(int used) {
        byte[] src = record(used);
        // Build a one-entry plan.
        TableFixed.Entry[] plan = TableFixed.plan(1);
        TableFixed.Entry e = plan[0];
        e.op = TableFixed.opText;
        e.meta = TableFixed.textWide; // the wide flavour
        e.src = 0;       // length word at offset 0 in the source
        e.size = 4;      // declared size in code units (= N for wstring(N))
        e.dst = 0;       // length word lands at offset 0 in the image
        e.aux = 4;       // payload lands at offset 4 in the image
        e.guard = TableFixed.noGuard;
        e.arg = 0;
        e.argw = 0;
        e.dstsize = 0;
        e.sign = 0;
        // remap scratch is required even though we never use opOrdinal.
        short[] remap = new short[16];
        byte[] image = new byte[4 + 2 * 4];
        TableFixed.Report report = new TableFixed.Report();
        TableFixed.run(plan, 1, remap, src, 0, src.length, image, report);
        // Return the length the runtime landed.
        return (image[0] & 0xff) | ((image[1] & 0xff) << 8)
                | ((image[2] & 0xff) << 16) | ((image[3] & 0xff) << 24);
    }

    public static void main(String[] args) {
        // CASE 1: used = 2, an astral pair (high+low surrogate).
        // 4 bytes, counted as 2 code units. The length lands as 2 and no
        // clamp fires.
        {
            TableFixed.Entry[] plan = TableFixed.plan(1);
            TableFixed.Entry e = plan[0];
            e.op = TableFixed.opText;
            e.meta = TableFixed.textWide;
            e.src = 0;
            e.size = 4;
            e.dst = 0;
            e.aux = 4;
            short[] remap = new short[16];
            byte[] src = record(2);
            byte[] image = new byte[8];
            TableFixed.Report report = new TableFixed.Report();
            TableFixed.run(plan, 1, remap, src, 0, src.length, image, report);
            int v = (image[0] & 0xff) | ((image[1] & 0xff) << 8)
                    | ((image[2] & 0xff) << 16) | ((image[3] & 0xff) << 24);
            check("C5 wide text: used=2 (astral pair) lands as 2 code units", v == 2);
            check("C5 wide text: used=2 does not fire the clamp", report.clamped == 0);
        }

        // CASE 2: used = 3, beyond the bound cap = size / unit = 4 / 2 = 2.
        // The clamp fires (report.clamped == 1) and the length lands as 2,
        // NOT 3 and NOT 6 (the byte count). THIS IS THE CELL'S OWN BITE: a
        // reader that took cap = size (and unit = 1) would clamp to 3 (the
        // byte bound), and a reader that doubled on the way IN would land 6.
        // The wide-text code-units rule says cap = size / unit, which gives
        // exactly 2.
        {
            TableFixed.Entry[] plan = TableFixed.plan(1);
            TableFixed.Entry e = plan[0];
            e.op = TableFixed.opText;
            e.meta = TableFixed.textWide;
            e.src = 0;
            e.size = 4;
            e.dst = 0;
            e.aux = 4;
            short[] remap = new short[16];
            byte[] src = record(3);
            byte[] image = new byte[8];
            TableFixed.Report report = new TableFixed.Report();
            TableFixed.run(plan, 1, remap, src, 0, src.length, image, report);
            int v = (image[0] & 0xff) | ((image[1] & 0xff) << 8)
                    | ((image[2] & 0xff) << 16) | ((image[3] & 0xff) << 24);
            check("C5 wide text: used=3 clamps to cap=2 (code units, not bytes)", v == 2);
            check("C5 wide text: used=3 fires the clamp exactly once", report.clamped == 1);
        }

        // CASE 3: used = -1 (the count-side `v < 0` branch).
        // The runtime clamps to 0 and increments clamped. A reader that
        // interpreted the u32 as signed would already do this; a reader that
        // interpreted it as unsigned would land v = 0xffffffff == 4 billion
        // and clamp to cap = 2 from the OTHER branch.
        {
            TableFixed.Entry[] plan = TableFixed.plan(1);
            TableFixed.Entry e = plan[0];
            e.op = TableFixed.opText;
            e.meta = TableFixed.textWide;
            e.src = 0;
            e.size = 4;
            e.dst = 0;
            e.aux = 4;
            short[] remap = new short[16];
            byte[] src = record(-1);
            byte[] image = new byte[8];
            TableFixed.Report report = new TableFixed.Report();
            TableFixed.run(plan, 1, remap, src, 0, src.length, image, report);
            int v = (image[0] & 0xff) | ((image[1] & 0xff) << 8)
                    | ((image[2] & 0xff) << 16) | ((image[3] & 0xff) << 24);
            check("C5 wide text: used=-1 lands as 0", v == 0);
            check("C5 wide text: used=-1 fires the clamp exactly once", report.clamped == 1);
        }

        if (failures > 0) {
            System.err.println("C5 FAIL: " + failures + " assertion(s) failed");
            System.exit(1);
        }
        System.out.println("C5 PASS");
    }
}
