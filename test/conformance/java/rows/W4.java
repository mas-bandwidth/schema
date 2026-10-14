// READ SLACK UNSPECIFIED (docs/FIXED-FORM-ALGORITHM.md:341, cell java/W4).
//
// The bounds pass walks only WHAT A READ CAN HAVE WRITTEN — a counted
// array's LIVE elements and never its slack. This test verifies that the
// Java fixed-form reader (PaddedFrameFixed) reads only the live elements
// of a counted array, leaving slack untouched even when the image contains
// non-default bytes there.
//
// The test constructs a fixed-form file byte-by-byte with KNOWN GARBAGE in
// the counted array's slack positions (rows past rowsCount). The reader's
// scatter MUST only read `rowsCount` rows, so the garbage must never reach
// the output value. The test asserts rows 3..63 remain at prefill defaults.
//
// Production call site: PaddedFrameFixed.scatter() at lines 81-83
// iterates `for (int i = 0; i < v.rowsCount; i++)` — live elements only.
//
// Negative control: change that loop bound to `64` (all elements) and the
// garbage bytes in slack positions land in rows[3..63], making the test RED.
//
// Law: docs/FIXED-FORM-ALGORITHM.md:341-349

import blockdemo.*;

public final class W4 {
    private W4() {}

    private static int passed;
    private static int failed;

    private static void check(String what, boolean ok) {
        if (ok) {
            passed++;
            System.out.println("PASS " + what);
        } else {
            failed++;
            System.out.println("FAIL " + what);
        }
    }

    public static void main(String[] args) throws Exception {
        // ---- construct a PaddedFrame file byte-by-byte
        //
        // The file layout (PaddedFrameFixed):
        //   offset 0:       form byte (3), 7 reserved zero bytes
        //   offset 8:       8-byte hash
        //   offset 16:      4-byte layout length
        //   offset 20:      layout bytes (395 bytes)
        //   offset 415:     1st record: 8-byte hash
        //   offset 423:     body: marker(1), stamp(8), rowsCount(4),
        //                   rows[0..63] (50 bytes each == 3200 bytes),
        //                   blobLength(4), blob(12) = 3229 bytes total

        int fileLen = PaddedFrameFixed.measure(1);
        byte[] file = new byte[fileLen];

        // write the header
        PaddedFrameFixed.writeHeader(file);

        // compute record start
        int recAt = PaddedFrameFixed.headerBytes;

        // write the record hash
        TableFixed.put64(file, recAt, PaddedFrameFixed.hash);

        // write the body manually, with KNOWN GARBAGE in slack positions
        int bodyAt = recAt + 8;

        // marker
        TableFixed.put8(file, bodyAt + 0, (byte) 0xAB);
        // stamp
        TableFixed.put64(file, bodyAt + 1, 42L);
        // rowsCount = 3
        TableFixed.put32(file, bodyAt + 9, 3);

        // rows[0]: write known values
        {
            int rowAt = bodyAt + 13;
            TableFixed.put8(file, rowAt, (byte) 100);
            TableFixed.put64(file, rowAt + 1, Double.doubleToRawLongBits(3.14));
            TableFixed.put8(file, rowAt + 9, (byte) 1); // flag = true
            TableFixed.put32(file, rowAt + 10, 1000);
            TableFixed.put32(file, rowAt + 14, 0); // labelLength = 0
            java.util.Arrays.fill(file, rowAt + 18, rowAt + 33, (byte) 0); // label
            for (int j = 0; j < 4; j++) TableFixed.put16(file, rowAt + 33 + j*2, (short)(j));
            for (int j = 0; j < 4; j++) TableFixed.put8(file, rowAt + 41 + j, (byte)(j));
            TableFixed.put8(file, rowAt + 45, (byte) 0); // counterPresent = false
        }
        // rows[1]: write known values
        {
            int rowAt = bodyAt + 13 + 1 * 50;
            TableFixed.put8(file, rowAt, (byte) 101);
            TableFixed.put64(file, rowAt + 1, Double.doubleToRawLongBits(4.14));
            TableFixed.put8(file, rowAt + 9, (byte) 0); // flag = false
            TableFixed.put32(file, rowAt + 10, 1001);
            TableFixed.put32(file, rowAt + 14, 0);
            java.util.Arrays.fill(file, rowAt + 18, rowAt + 33, (byte) 0);
            for (int j = 0; j < 4; j++) TableFixed.put16(file, rowAt + 33 + j*2, (short)(4+j));
            for (int j = 0; j < 4; j++) TableFixed.put8(file, rowAt + 41 + j, (byte)(4+j));
            TableFixed.put8(file, rowAt + 45, (byte) 1); // counterPresent = true
            TableFixed.put32(file, rowAt + 46, 999);
        }
        // rows[2]: write known values
        {
            int rowAt = bodyAt + 13 + 2 * 50;
            TableFixed.put8(file, rowAt, (byte) 102);
            TableFixed.put64(file, rowAt + 1, Double.doubleToRawLongBits(5.14));
            TableFixed.put8(file, rowAt + 9, (byte) 1); // flag = true
            TableFixed.put32(file, rowAt + 10, 1002);
            TableFixed.put32(file, rowAt + 14, 0);
            java.util.Arrays.fill(file, rowAt + 18, rowAt + 33, (byte) 0);
            for (int j = 0; j < 4; j++) TableFixed.put16(file, rowAt + 33 + j*2, (short)(8+j));
            for (int j = 0; j < 4; j++) TableFixed.put8(file, rowAt + 41 + j, (byte)(8+j));
            TableFixed.put8(file, rowAt + 45, (byte) 0); // counterPresent = false
        }

        // SLACK GARBAGE: fill rows 3..63 with known non-zero bytes (0xFF)
        // The reader MUST NOT read these — if it iterates all 64 rows,
        // rows[3..63] would get 0xFF data from the image, not defaults.
        int slackStart = bodyAt + 13 + 3 * 50; // after row 2
        int slackEnd = bodyAt + 13 + 64 * 50;  // where rows array ends (3200 bytes from offset 13)
        // but body has 3204 bytes from offset 13: (13 + 64*50) = 3213,
        // and bodyBytes = 3229, so 3213..3228 is blobLength and blob
        // rows end at 13 + 64*50 = 3213
        java.util.Arrays.fill(file, slackStart, bodyAt + 3213, (byte) 0xFF);

        // blob fields: leave zero
        TableFixed.put32(file, bodyAt + 3213, 0); // blobLength
        java.util.Arrays.fill(file, bodyAt + 3217, bodyAt + 3229, (byte) 0); // blob

        // ---- load the file

        TableFixed.Report report = new TableFixed.Report();
        PaddedFrameFixed.Value[] out = new PaddedFrameFixed.Value[1];
        out[0] = new PaddedFrameFixed.Value();

        int n = PaddedFrameFixed.load(out, 1, file,
                PaddedFrameFixed.identityPlan(), null,
                PaddedFrameFixed.image(), report);

        check("load returns 1 record", n == 1);
        check("no refusal", !report.refused);
        check("no malformed", !report.malformed);
        check("no clamped", report.clamped == 0);
        check("no unknown", report.unknown == 0);

        PaddedFrameFixed.Value loaded = out[0];

        // ---- the projection fields

        check("marker equals " + (byte)0xAB, loaded.marker == (byte) 0xAB);
        check("stamp equals 42", loaded.stamp == 42L);

        // ---- COUNTED ARRAY: only rowsCount = 3 live elements were read

        check("rowsCount equals 3", loaded.rowsCount == 3);

        // rows[0] verification
        check("rows[0].tag equals 100", loaded.rows[0].tag == (byte) 100);
        check("rows[0].value approx 3.14", Math.abs(loaded.rows[0].value - 3.14) < 0.001);
        check("rows[0].flag is true", loaded.rows[0].flag);
        check("rows[0].id equals 1000", loaded.rows[0].id == 1000);
        check("rows[0].counterPresent is false", !loaded.rows[0].counterPresent);

        // rows[1] verification
        check("rows[1].tag equals 101", loaded.rows[1].tag == (byte) 101);
        check("rows[1].value approx 4.14", Math.abs(loaded.rows[1].value - 4.14) < 0.001);
        check("rows[1].flag is false", !loaded.rows[1].flag);
        check("rows[1].id equals 1001", loaded.rows[1].id == 1001);
        check("rows[1].counterPresent is true", loaded.rows[1].counterPresent);
        check("rows[1].counter equals 999", loaded.rows[1].counter == 999);

        // rows[2] verification
        check("rows[2].tag equals 102", loaded.rows[2].tag == (byte) 102);
        check("rows[2].value approx 5.14", Math.abs(loaded.rows[2].value - 5.14) < 0.001);
        check("rows[2].flag is true", loaded.rows[2].flag);
        check("rows[2].id equals 1002", loaded.rows[2].id == 1002);
        check("rows[2].counterPresent is false", !loaded.rows[2].counterPresent);

        // ---- SLACK: rows 3..63 must be UNTOUCHED (prefill defaults),
        //      despite 0xFF garbage sitting in the image at those positions.

        for (int i = 3; i < 64; i++) {
            check("rows[" + i + "].tag is default (0)",
                    loaded.rows[i].tag == (byte) 0);
            check("rows[" + i + "].value is default (0.0)",
                    loaded.rows[i].value == 0.0);
            check("rows[" + i + "].flag is default (false)",
                    !loaded.rows[i].flag);
            check("rows[" + i + "].id is default (0)",
                    loaded.rows[i].id == 0);
            check("rows[" + i + "].labelLength is default (0)",
                    loaded.rows[i].labelLength == 0);
            check("rows[" + i + "].counterPresent is default (false)",
                    !loaded.rows[i].counterPresent);
        }

        // ---- NEGATIVE CONTROL: a forged count past max should clamp

        TableFixed.Report report2 = new TableFixed.Report();
        PaddedFrameFixed.Value[] out2 = new PaddedFrameFixed.Value[1];
        out2[0] = new PaddedFrameFixed.Value();
        byte[] forged = file.clone();
        int rowsCountOffset = bodyAt + 9;
        TableFixed.put32(forged, rowsCountOffset, 99); // > rowsMax=64

        PaddedFrameFixed.load(out2, 1, forged,
                PaddedFrameFixed.identityPlan(), null,
                PaddedFrameFixed.image(), report2);

        check("negative control: count > max clamps", report2.clamped >= 1);
        check("negative control: rowsCount clamped to 64", out2[0].rowsCount == 64);

        System.out.println("\n" + passed + " passed, " + failed + " failed");
        System.exit(failed > 0 ? 1 : 0);
    }
}