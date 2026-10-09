// test/conformance/java/rows/P3.java — the java/P3 cell of the hostile-input
// work set (docs/roadmap.sexp:3938), asserted for THIS leg at the tip.
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:1686, §7 item 5:
//
//   "a byte-flip fuzz over the whole file, under a sanitizer, and it is not
//    optional — every offset is arithmetic over sizes a stranger wrote down,
//    so every byte, one bit at a time, is answered one of three ways and never
//    a fourth: a refusal by name, a `malformed` read, or a read that lands
//    values"
//
// THE VECTOR: the tree carries no fixed-form bytes in testdata, so the file is
// CONSTRUCTED here from the law — a lawful form-3 file of one GlobalSettings
// record, written by THIS build's own generated writer
// (build/tables-generated-java/examples/GlobalSettingsFixed.save), which is
// the same byte vector the reference's fuzz_case starts from
// (test/tables/fixedform_main.cpp:666-676). The reader under test is this
// build's IDENTITY plan over its own bytes, the JS leg's shape
// (test/js-tables/fixedform.mjs, hostileByteSweep).
//
// THE SANITIZER HALF is the language itself: an offset past the array this
// process owns throws IndexOutOfBoundsException, and an exception escaping a
// load that was asked a question IS the fourth answer — the same instrument
// the readers' oracle names (make/java.mk, tables-java-fuzz). Every byte of
// the file is flipped one bit at a time and the load must answer one of the
// three ways and never a fourth.
//
// Runs green with the generated code untouched and RED with one clamp removed
// from the generated reader (Step 3's control).
//
// RUN (from ./repo, the classpath make/java.mk gives the conformance driver):
//   java -cp build/conformance-java test/conformance/java/rows/P3.java

import java.nio.charset.StandardCharsets;
import tabledemo.GlobalSettingsFixed;
import tabledemo.TableFixed;

public final class P3 {
    private P3() {}

    private static int failures = 0;
    private static int assertions = 0;

    // One printed line per assertion.
    private static void check(boolean ok, String what) {
        assertions++;
        if (ok) {
            System.out.println("PASS: " + what);
        } else {
            System.out.println("FAILED: " + what);
            failures++;
        }
    }

    // A LANDED RECORD IS IN BOUND when every field the schema gives a bound to
    // sits inside it. GlobalSettings (tables/examples/Pack.schema) declares:
    //   tick_rate    uint32 = 60    min = 1, max = 240
    //   difficulty   Difficulty     (Easy/Normal/Hard, ordinals 0..3)
    //   build_note   string(48)
    //   spawn_delays [3]float32     (fixed three, no count on the wire)
    // The scatter's decode bounds are the pass the identity path owes (§7 item
    // 5); a hostile value that reaches the caller UNCLAMPED is the silently
    // wrong report this watches for.
    private static String issue(GlobalSettingsFixed.Value v) {
        if (v.tickRate < 1 || v.tickRate > 240) { return "tickRate " + v.tickRate; }
        if ((v.difficulty & 0xff) > 3) { return "difficulty " + (v.difficulty & 0xff); }
        if (v.buildNoteLength < 0 || v.buildNoteLength > 48) { return "buildNoteLength " + v.buildNoteLength; }
        return null;
    }

    public static void main(String[] args) {
        // THE LAWFUL VECTOR: one GlobalSettings record with every field set,
        // written by this build's own generated writer. The bytes are the
        // reader's own — form byte 3, the header, the layout, one record.
        final GlobalSettingsFixed.Value v = new GlobalSettingsFixed.Value();
        v.tickRate = 60;
        v.difficulty = 2;
        final byte[] note = "hostile".getBytes(StandardCharsets.UTF_8);
        System.arraycopy(note, 0, v.buildNote, 0, note.length);
        v.buildNoteLength = note.length;
        v.spawnDelays[0] = 1.5f;
        v.spawnDelays[1] = 2.5f;
        v.spawnDelays[2] = 3.5f;
        final byte[] file = new byte[GlobalSettingsFixed.measure(1)];
        check(GlobalSettingsFixed.save(new GlobalSettingsFixed.Value[] { v }, 1, file) == file.length,
                "P3 hostile bytes: the lawful record saves (" + file.length + " bytes)");

        // CONTROL 1's POSITIVE HALF, and it is the rule's own legitimate value:
        // the UNFLIPPED record must land, in bound, with no counter moved. A
        // sweep whose assertions fired on this would be testing something else.
        {
            final GlobalSettingsFixed.Value[] back = new GlobalSettingsFixed.Value[1];
            back[0] = new GlobalSettingsFixed.Value();
            final TableFixed.Report r = new TableFixed.Report();
            final int n = GlobalSettingsFixed.load(back, 1, file,
                    GlobalSettingsFixed.identityPlan(), new short[1024], GlobalSettingsFixed.image(), r);
            check(n == 1 && !r.malformed && !r.refused && r.clamped == 0,
                    "P3 hostile bytes: the lawful record reads clean (n=" + n
                            + ", refused=" + r.refused + ", malformed=" + r.malformed
                            + ", clamped=" + r.clamped + ")");
            check(issue(back[0]) == null,
                    "P3 hostile bytes: the lawful record lands every field in bound");
        }

        // THE SWEEP: every byte, one bit at a time. Each mutant is answered one
        // of three ways and never a fourth:
        //   refusal by name  n < 0 && refused && !malformed
        //   malformed        n < 0 && malformed && !refused
        //   lands values     n > 0, every landed field in bound, not refused
        // A throw escaping the load, a negative read that is neither refusal
        // nor malformed, or a landed field out of bound is the FOURTH.
        long refused = 0, malformed = 0, landed = 0, corrected = 0, divergences = 0;
        for (int at = 0; at < file.length; at++) {
            for (int bit = 0; bit < 8; bit++) {
                final byte[] hit = file.clone();
                hit[at] ^= (byte) (1 << bit);
                final GlobalSettingsFixed.Value[] back = new GlobalSettingsFixed.Value[1];
                back[0] = new GlobalSettingsFixed.Value();
                final TableFixed.Report r = new TableFixed.Report();
                int n;
                try {
                    n = GlobalSettingsFixed.load(back, 1, hit,
                            GlobalSettingsFixed.identityPlan(), new short[1024], GlobalSettingsFixed.image(), r);
                } catch (RuntimeException e) {
                    divergences++;
                    check(false, "P3 hostile bytes: byte " + at + " bit " + bit + " THREW " + e);
                    continue;
                }
                if (n < 0) {
                    if (r.refused && !r.malformed) {
                        refused++;
                    } else if (r.malformed && !r.refused) {
                        malformed++;
                    } else {
                        divergences++;
                        check(false, "P3 hostile bytes: byte " + at + " bit " + bit
                                + " answered a FOURTH way: n=" + n + ", refused=" + r.refused
                                + ", malformed=" + r.malformed);
                    }
                    continue;
                }
                landed++;
                if (r.clamped > 0) { corrected++; }
                if (r.refused) {
                    divergences++;
                    check(false, "P3 hostile bytes: byte " + at + " bit " + bit
                            + " returned records yet reported refused");
                }
                final String bad = issue(back[0]);
                if (bad != null) {
                    divergences++;
                    check(false, "P3 hostile bytes: byte " + at + " bit " + bit
                            + " landed OUT OF BOUND: " + bad);
                }
            }
        }

        // NOT VACUOUS: each of the three answers must actually have been SEEN,
        // and a correction must have moved a counter. A sweep that never
        // refused, never malformed, or never landed is a sweep over the wrong
        // file.
        check(refused > 0, "P3 hostile bytes: the sweep saw a named refusal (" + refused + ")");
        check(malformed > 0, "P3 hostile bytes: the sweep saw a malformed read (" + malformed + ")");
        check(landed > 0, "P3 hostile bytes: the sweep saw reads that landed values (" + landed + ")");
        check(corrected > 0, "P3 hostile bytes: the sweep exercised a correction that moved a counter (" + corrected + ")");
        check(divergences == 0, "P3 hostile bytes: " + divergences + " divergences, never a fourth");

        System.out.println("P3 hostile bytes: " + file.length + " bytes x 8 bits — " + refused
                + " refused by name, " + malformed + " malformed, " + landed
                + " records read (" + corrected + " with a correction), " + divergences + " divergences");
        if (failures == 0) {
            System.out.println("P3: " + assertions + " assertions, all green");
            System.exit(0);
        }
        System.out.println("P3: " + failures + " of " + assertions + " assertions FAILED");
        System.exit(1);
    }
}