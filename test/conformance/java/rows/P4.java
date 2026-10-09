// P4.java — "wrong plan goes red" (docs/FIXED-FORM-ALGORITHM.md:1685)
//
// Test: a wire written under schema K1 (grade=Grade, raw=uint16) read under
// schema K2 (grade=uint16, raw=Grade) MUST produce exactly 2 kind_mismatches.
// The reverse (K2 written, K1 read) must also produce exactly 2.
//
// Run from repo root as:
//   java -cp build/conformance-java test/conformance/java/rows/P4.java
// or:
//   java -cp build/conformance-java P4
//
// Exit 0 = GREEN (law asserted), exit 1 = RED (law fails)

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Paths;

public final class P4 {
    private P4() {}
    static int failed = 0;

    static void check(boolean condition, String msg) {
        if (!condition) {
            System.err.println("FAIL: " + msg);
            failed++;
        } else {
            System.out.println("OK: " + msg);
        }
    }

    static byte[] readWire(String path) throws IOException {
        return Files.readAllBytes(Paths.get(path));
    }

    public static void main(String[] args) throws IOException {
        // -------------------------------------------------------------------
        // 1. Wire inspections (Form 1 wire reference pins)
        // -------------------------------------------------------------------
        byte[] k1_as_k2 = readWire("testdata/wire/tables/k1_as_k2.bin");
        check(k1_as_k2.length >= 6, "k1_as_k2.bin has header (len=" + k1_as_k2.length + ")");
        check(k1_as_k2[0] == 0x01, "magic[0] == 0x01");
        check(k1_as_k2[1] == 0x01, "magic[1] == 0x01 (version)");
        check(k1_as_k2[2] == (byte) 0x1e, "k1_as_k2 field1 kind=0x1e (Grade)");
        check(k1_as_k2[5] == (byte) 0x07, "k1_as_k2 field2 kind=0x07 (uint16)");
        check(k1_as_k2.length == 41, "k1_as_k2.bin has expected length (41 bytes)");

        byte[] k2_as_k1 = readWire("testdata/wire/tables/k2_as_k1.bin");
        check(k2_as_k1.length >= 6, "k2_as_k1.bin has header (len=" + k2_as_k1.length + ")");
        check(k2_as_k1[0] == 0x01, "magic[0] == 0x01");
        check(k2_as_k1[1] == 0x01, "magic[1] == 0x01 (version)");
        check(k2_as_k1[2] == (byte) 0x07, "k2_as_k1 field1 kind=0x07 (uint16)");
        check(k2_as_k1[6] == (byte) 0x1e, "k2_as_k1 field2 kind=0x1e (Grade)");
        check(k2_as_k1.length == 41, "k2_as_k1.bin has expected length (41 bytes)");

        // -------------------------------------------------------------------
        // 2. Fixed Form Plan Compilation & Census Verification (§5.2, §5.4)
        // -------------------------------------------------------------------
        // (a) K1 wire read under K2 reader:
        // Reader is K2; K2 compiles writer K1's layout against K2's own layout.
        tblk2.TableFixed.Layout theirsK1 = tblk2.TableFixed.parse(tblk1.RootFixed.layout, 0, tblk1.RootFixed.layout.length, null);
        tblk2.TableFixed.Layout mineK2 = tblk2.TableFixed.parse(tblk2.RootFixed.layout, 0, tblk2.RootFixed.layout.length, null);
        check(theirsK1 != null && theirsK1.count == 6, "parsed K1 layout has 6 entries");
        check(mineK2 != null && mineK2.count == 6, "parsed K2 layout has 6 entries");

        tblk2.TableFixed.Entry[] planK1asK2 = new tblk2.TableFixed.Entry[64];
        for (int i = 0; i < planK1asK2.length; i++) { planK1asK2[i] = new tblk2.TableFixed.Entry(); }
        short[] remapK1asK2 = new short[64];
        tblk2.TableFixed.Report repK1asK2 = new tblk2.TableFixed.Report();
        int nK1asK2 = tblk2.TableFixed.compile(theirsK1, mineK2, tblk2.RootFixed.dest, planK1asK2, remapK1asK2, repK1asK2);

        check(repK1asK2.kindMismatch == 2, "K1 read under K2 produces kindMismatch == 2");
        check(repK1asK2.unknown == 0, "K1 read under K2 produces unknown == 0");
        check(nK1asK2 == 0, "K1 read under K2 produces 0 plan copy entries");

        // (b) K2 wire read under K1 reader:
        // Reader is K1; K1 compiles writer K2's layout against K1's own layout.
        tblk1.TableFixed.Layout theirsK2 = tblk1.TableFixed.parse(tblk2.RootFixed.layout, 0, tblk2.RootFixed.layout.length, null);
        tblk1.TableFixed.Layout mineK1 = tblk1.TableFixed.parse(tblk1.RootFixed.layout, 0, tblk1.RootFixed.layout.length, null);
        check(theirsK2 != null && theirsK2.count == 6, "parsed K2 layout has 6 entries");
        check(mineK1 != null && mineK1.count == 6, "parsed K1 layout has 6 entries");

        tblk1.TableFixed.Entry[] planK2asK1 = new tblk1.TableFixed.Entry[64];
        for (int i = 0; i < planK2asK1.length; i++) { planK2asK1[i] = new tblk1.TableFixed.Entry(); }
        short[] remapK2asK1 = new short[64];
        tblk1.TableFixed.Report repK2asK1 = new tblk1.TableFixed.Report();
        int nK2asK1 = tblk1.TableFixed.compile(theirsK2, mineK1, tblk1.RootFixed.dest, planK2asK1, remapK2asK1, repK2asK1);

        check(repK2asK1.kindMismatch == 2, "K2 read under K1 produces kindMismatch == 2");
        check(repK2asK1.unknown == 0, "K2 read under K1 produces unknown == 0");
        check(nK2asK1 == 0, "K2 read under K1 produces 0 plan copy entries");

        // (c) Lawful K1 compile: 0 mismatches, plan entries produced
        tblk1.TableFixed.Entry[] planK1asK1 = new tblk1.TableFixed.Entry[64];
        for (int i = 0; i < planK1asK1.length; i++) { planK1asK1[i] = new tblk1.TableFixed.Entry(); }
        short[] remapK1asK1 = new short[64];
        tblk1.TableFixed.Report repK1asK1 = new tblk1.TableFixed.Report();
        int nK1asK1 = tblk1.TableFixed.compile(mineK1, mineK1, tblk1.RootFixed.dest, planK1asK1, remapK1asK1, repK1asK1);

        check(repK1asK1.kindMismatch == 0, "Lawful K1 compile produces kindMismatch == 0");
        check(repK1asK1.unknown == 0, "Lawful K1 compile produces unknown == 0");
        check(nK1asK1 > 0, "Lawful K1 compile produces " + nK1asK1 + " plan entries");

        // (d) Lawful K2 compile: 0 mismatches, plan entries produced
        tblk2.TableFixed.Entry[] planK2asK2 = new tblk2.TableFixed.Entry[64];
        for (int i = 0; i < planK2asK2.length; i++) { planK2asK2[i] = new tblk2.TableFixed.Entry(); }
        short[] remapK2asK2 = new short[64];
        tblk2.TableFixed.Report repK2asK2 = new tblk2.TableFixed.Report();
        int nK2asK2 = tblk2.TableFixed.compile(mineK2, mineK2, tblk2.RootFixed.dest, planK2asK2, remapK2asK2, repK2asK2);

        check(repK2asK2.kindMismatch == 0, "Lawful K2 compile produces kindMismatch == 0");
        check(repK2asK2.unknown == 0, "Lawful K2 compile produces unknown == 0");
        check(nK2asK2 > 0, "Lawful K2 compile produces " + nK2asK2 + " plan entries");

        // -------------------------------------------------------------------
        // 3. Execution via TableFixed.run & RootFixed.scatter
        // -------------------------------------------------------------------
        tblk1.RootFixed.Value k1Src = new tblk1.RootFixed.Value();
        k1Src.grade = tblk1.K1.Grade.gold; // 3
        k1Src.raw = (short) 500;
        byte[] k1WireBody = new byte[tblk1.RootFixed.bodyBytes];
        tblk1.RootFixed.writeBody(k1WireBody, 0, k1Src);

        // Lawful read:
        byte[] lawfulImage = new byte[tblk1.RootFixed.bodyBytes];
        System.arraycopy(tblk1.RootFixed.defaults, 0, lawfulImage, 0, tblk1.RootFixed.bodyBytes);
        tblk1.TableFixed.run(planK1asK1, nK1asK1, remapK1asK1, k1WireBody, 0, k1WireBody.length, lawfulImage, repK1asK1);
        tblk1.RootFixed.Value k1Decoded = new tblk1.RootFixed.Value();
        tblk1.RootFixed.scatter(lawfulImage, 0, k1Decoded, repK1asK1);
        check(k1Decoded.grade == tblk1.K1.Grade.gold, "Lawful K1 read lands grade=Gold");
        check(k1Decoded.raw == 500, "Lawful K1 read lands raw=500");

        // Cross-schema read through compiled plan:
        // Mismatches are caught and skipped, fields safely take defaults
        byte[] k2Image = new byte[tblk2.RootFixed.bodyBytes];
        System.arraycopy(tblk2.RootFixed.defaults, 0, k2Image, 0, tblk2.RootFixed.bodyBytes);
        tblk2.TableFixed.run(planK1asK2, nK1asK2, remapK1asK2, k1WireBody, 0, k1WireBody.length, k2Image, repK1asK2);
        tblk2.RootFixed.Value k2Decoded = new tblk2.RootFixed.Value();
        tblk2.RootFixed.scatter(k2Image, 0, k2Decoded, repK1asK2);
        check(k2Decoded.grade == 0, "Cross-schema read leaves grade at default (0)");
        check(k2Decoded.raw == tblk2.K2.Grade.none, "Cross-schema read leaves raw enum at default (None=0)");

        // -------------------------------------------------------------------
        // 4. "The wrong plan goes red" (docs/FIXED-FORM-ALGORITHM.md:1685)
        // -------------------------------------------------------------------
        // Applying this build's identity plan (which blindly copies the body)
        // over the other schema's record bytes produces mangled/wrong values.
        byte[] wrongImage = new byte[tblk2.RootFixed.bodyBytes];
        tblk2.TableFixed.Entry[] wrongPlan = tblk2.RootFixed.identityPlan();
        tblk2.TableFixed.run(wrongPlan, wrongPlan.length, null, k1WireBody, 0, k1WireBody.length, wrongImage, new tblk2.TableFixed.Report());
        tblk2.RootFixed.Value k2Wrong = new tblk2.RootFixed.Value();
        tblk2.RootFixed.scatter(wrongImage, 0, k2Wrong, new tblk2.TableFixed.Report());

        check(k2Wrong.grade != 0 && k2Wrong.grade != 500,
            "Wrong plan mangles grade value (read " + (k2Wrong.grade & 0xFFFF) + ")");
        check(k2Wrong.raw != tblk1.K1.Grade.gold,
            "Wrong plan mangles raw enum value (read " + k2Wrong.raw + ")");

        // -------------------------------------------------------------------
        // 5. Negative controls
        // -------------------------------------------------------------------
        boolean negControlCaught = false;
        // Simulating incorrect report expectations
        if (repK1asK2.kindMismatch != 0) {
            negControlCaught = true;
        }
        check(negControlCaught, "Negative control: non-zero kindMismatch correctly detected");

        if (failed > 0) {
            System.err.println("P4 TEST FAILED (" + failed + " failures)");
            System.exit(1);
        }
        System.out.println("P4 TEST PASSED");
        System.exit(0);
    }
}
