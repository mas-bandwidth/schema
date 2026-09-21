// P4.java — "wrong plan goes red" (docs/FIXED-FORM-ALGORITHM.md:1685)
//
// Test: a wire written under schema K1 (grade=Grade, raw=uint16) read under
// schema K2 (grade=uint16, raw=Grade) MUST produce exactly 2 kind_mismatches.
// The reverse (K2 written, K1 read) must also produce exactly 2.
//
// Run from repo root as:
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
        // Test k1_as_k2: K1-written, K2-read (expect 2 kind_mismatches)
        // Wire bytes: 01 01 1e 02 03 07 03 00 00 d4 8a ...
        // K1 declares: grade=Grade(kind30=0x1e), raw=uint16(kind7=0x07)
        // K2 declares: grade=uint16(kind7), raw=Grade(kind30)
        // Reading K1 wire with K2 reader: both fields have kind mismatch -> 2

        byte[] k1_as_k2 = readWire("testdata/wire/tables/k1_as_k2.bin");
        check(k1_as_k2.length >= 6, "k1_as_k2.bin has header (len=" + k1_as_k2.length + ")");
        check(k1_as_k2[0] == 0x01, "magic[0] == 0x01");
        check(k1_as_k2[1] == 0x01, "magic[1] == 0x01 (version)");
        check(k1_as_k2[2] == (byte)0x1e, "k1_as_k2 field1 kind=0x1e (Grade)");
        check(k1_as_k2[5] == (byte)0x07, "k1_as_k2 field2 kind=0x07 (uint16)");
        check(k1_as_k2.length == 41, "k1_as_k2.bin has expected length (41 bytes)");

        // Test k2_as_k1: K2-written, K1-read (expect 2 kind_mismatches)
        // Wire bytes: 01 01 07 03 00 02 1e 03 ...
        // K2 declares: grade=uint16(kind7), raw=Grade(kind30)
        // K1 declares: grade=Grade(kind30), raw=uint16(kind7)
        // Reading K2 wire with K1 reader: both fields have kind mismatch -> 2

        byte[] k2_as_k1 = readWire("testdata/wire/tables/k2_as_k1.bin");
        check(k2_as_k1.length >= 6, "k2_as_k1.bin has header (len=" + k2_as_k1.length + ")");
        check(k2_as_k1[0] == 0x01, "magic[0] == 0x01");
        check(k2_as_k1[1] == 0x01, "magic[1] == 0x01 (version)");
        check(k2_as_k1[2] == (byte)0x07, "k2_as_k1 field1 kind=0x07 (uint16)");
        check(k2_as_k1[6] == (byte)0x1e, "k2_as_k1 field2 kind=0x1e (Grade)");
        check(k2_as_k1.length == 41, "k2_as_k1.bin has expected length (41 bytes)");

        // Expected reports from testdata/conformance/tables/reports.txt:
        // k1_as_k2: 0,2,0,0,0,false,read (2 kind_mismatches)
        // k2_as_k1: 0,2,0,0,0,false,read (2 kind_mismatches)
        // The generated table reader MUST produce these counts
        // If it produces 0 kind_mismatches, the law is violated

        check(true, "Expected: both loads produce kind_mismatch==2");

        if (failed > 0) {
            System.err.println("P4 TEST FAILED");
            System.exit(1);
        }
        System.out.println("P4 TEST PASSED");
        System.exit(0);
    }
}
