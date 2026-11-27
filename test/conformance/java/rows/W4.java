// W4 — EVERY DEAD BYTE IS ZERO, AND A NON-ZERO DEAD BYTE REFUSES THE WHOLE
// LOAD (docs/SPEC-TABLES.md, THE SLACK RULE: "EVERY SLACK BYTE IS DEAD, AND A
// NON-ZERO DEAD BYTE REFUSES THE WHOLE LOAD"; docs/FIXED-FORM-ALGORITHM.md
// §4.5, §5.3; cell java/W4). The Go row test/conformance/go/rows/W4_test.go
// holds the same law. This row once held the old slack rule (garbage in a
// counted array's slack read, and was never looked at); the spec replaced
// it, and the row with it.
//
// THE LAW, clause by clause, as this row checks it:
//   - ZERO ON WRITE: the writer zero-fills every byte of slack, so the record
//     this build saves reads clean and saves back byte identically;
//   - each kind of dead byte (a counted array's slots past its live count, a
//     string(N)'s and a bytes(N)'s bytes past the used length, and an absent
//     optional's payload) REFUSES `wireDamaged` when one byte of it is not
//     zero, and a refusal is never damage;
//   - the refusal lands nothing: every row the call was given holds its
//     declared default after it (ruling R4; PaddedFrame declares none, so
//     the default is a fresh Value), and no counter moves.
//
// THE VECTOR, blockdemo's PaddedFrame (tables/block/Padded.schema), the body
// positional (SPEC §3.4): marker +0, stamp +1, the rows count +9, then 64
// PaddedRows of 50 bytes from +13 (tag +0, value +1, flag +9, id +10, the
// label's length +14 and its 15 bytes +18, slots +33, teams +41, counter's
// present flag +45 and its payload +46), then blob's length +3213 and its 12
// bytes +3217. The record's 8-byte hash sits in front of the body.

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

    /**
     * the first part of the VALUE at which a and b differ, or null: the used
     * extent only (the live rows, the used label and blob bytes, a present
     * counter), which is what the value is; storage past a count or a length
     * is not part of it.
     */
    private static String diff(PaddedFrameFixed.Value a, PaddedFrameFixed.Value b) {
        if (a.marker != b.marker) return "marker";
        if (a.stamp != b.stamp) return "stamp";
        if (a.rowsCount != b.rowsCount) return "rowsCount";
        if (a.blobLength != b.blobLength) return "blobLength";
        if (!java.util.Arrays.equals(a.blob, 0, a.blobLength, b.blob, 0, b.blobLength)) return "blob";
        for (int i = 0; i < a.rowsCount; i++) {
            PaddedRowFixed.Value x = a.rows[i], y = b.rows[i];
            if (x.tag != y.tag || Double.compare(x.value, y.value) != 0 || x.flag != y.flag || x.id != y.id
                    || x.labelLength != y.labelLength
                    || !java.util.Arrays.equals(x.label, 0, x.labelLength, y.label, 0, y.labelLength)
                    || !java.util.Arrays.equals(x.slots, y.slots) || !java.util.Arrays.equals(x.teams, y.teams)
                    || x.counterPresent != y.counterPresent || (x.counterPresent && x.counter != y.counter)) {
                return "rows[" + i + "]";
            }
        }
        return null;
    }

    public static void main(String[] args) throws Exception {
        // ---- the record: three live rows, every dead byte zero
        PaddedFrameFixed.Value v = new PaddedFrameFixed.Value();
        v.marker = (byte) 0xAB;
        v.stamp = 42L;
        v.rowsCount = 3;
        for (int i = 0; i < 3; i++) {
            PaddedRowFixed.Value r = v.rows[i];
            r.tag = (byte) (100 + i);
            r.value = 3.25 + i;
            r.flag = i != 1;
            r.id = 1000 + i;
            for (int j = 0; j < 4; j++) {
                r.slots[j] = (short) (4 * i + j);
                r.teams[j] = (byte) (4 * i + j);
            }
        }
        v.rows[0].label[0] = 'o';
        v.rows[0].label[1] = 'k';
        v.rows[0].labelLength = 2; // 13 dead label bytes
        v.rows[1].counterPresent = true;
        v.rows[1].counter = 999; // row 0 and row 2: the 4 payload bytes are dead
        v.blob[0] = 1;
        v.blob[1] = 2;
        v.blob[2] = 3;
        v.blob[3] = 4;
        v.blobLength = 4; // 8 dead blob bytes

        byte[] file = new byte[PaddedFrameFixed.measure(1)];
        check("the record saves", PaddedFrameFixed.save(new PaddedFrameFixed.Value[] {v}, 1, file) == file.length);
        final int bodyAt = PaddedFrameFixed.headerBytes + 8;
        final int rowsAt = bodyAt + 13;

        // ---- zero on write
        int deadNonZero = 0;
        for (int k = rowsAt + 3 * 50; k < rowsAt + 64 * 50; k++) if (file[k] != 0) deadNonZero++;
        for (int k = rowsAt + 18 + 2; k < rowsAt + 18 + 15; k++) if (file[k] != 0) deadNonZero++;
        for (int k = rowsAt + 46; k < rowsAt + 50; k++) if (file[k] != 0) deadNonZero++;
        for (int k = bodyAt + 3217 + 4; k < bodyAt + 3229; k++) if (file[k] != 0) deadNonZero++;
        check("zero on write: every dead byte the writer emits is zero (" + deadNonZero + " non-zero)", deadNonZero == 0);

        // ---- the clean record reads, and saves back byte identically
        PaddedFrameFixed.Value[] back = {new PaddedFrameFixed.Value()};
        TableFixed.Report report = new TableFixed.Report();
        int n = PaddedFrameFixed.load(back, 1, file, PaddedFrameFixed.identityPlan(), null, PaddedFrameFixed.image(), report);
        check("baseline: the zero-slack record loads (n=" + n + ")", n == 1 && !report.refused && !report.damaged);
        check("baseline: every live value lands", diff(back[0], v) == null);
        byte[] again = new byte[file.length];
        check("baseline: the read record saves back byte identically",
                PaddedFrameFixed.save(back, 1, again) == again.length && java.util.Arrays.equals(again, file));

        // ---- each kind of dead byte, stained alone, refuses the whole load
        PaddedFrameFixed.Value defaults = new PaddedFrameFixed.Value();
        Object[][] stains = {
            {"a counted array's slot past its live count 3 (row 3's tag)", rowsAt + 3 * 50},
            {"the array's last dead byte (row 63's counter)", rowsAt + 64 * 50 - 1},
            {"a string(15)'s byte past its used length 2 (row 0's label)", rowsAt + 18 + 2},
            {"an absent optional's payload (row 0's counter)", rowsAt + 46},
            {"a bytes(12)'s byte past its used length 4 (blob)", bodyAt + 3217 + 4},
        };
        for (Object[] s : stains) {
            String what = (String) s[0];
            byte[] stained = file.clone();
            stained[(Integer) s[1]] = 1;
            PaddedFrameFixed.Value[] out = {new PaddedFrameFixed.Value()};
            out[0].marker = 7; // a stale destination, so a refusal that lands anything is visible
            out[0].rowsCount = 1;
            out[0].rows[0].id = 5;
            TableFixed.Report r = new TableFixed.Report();
            int got = PaddedFrameFixed.load(out, 1, stained, PaddedFrameFixed.identityPlan(), null, PaddedFrameFixed.image(), r);
            check(what + ": a non-zero dead byte refuses wireDamaged (n=" + got + ", reason=" + r.reason + ")",
                    got == -1 && r.refused && r.reason == TableFixed.Reason.wireDamaged && !r.damaged);
            String d = diff(out[0], defaults);
            check(what + ": the refusal leaves the destination at its declared defaults (R4)" + (d == null ? "" : ": " + d), d == null);
            check(what + ": a refused load moves no counter", r.unknown == 0 && r.widened == 0);
        }

        System.out.println("\n" + passed + " passed, " + failed + " failed");
        System.exit(failed > 0 ? 1 : 0);
    }
}
