// R4: "the hash is fnv1a64 over the layout bytes then DIGEST(T), the digest
// computed at the hash site from the schema; a runtime never derives a hash
// from layout bytes it holds"
//
// This test computes fnv1a64 LONGHAND in the fixture over the layout bytes
// as written (including the 4-byte entry count) and asserts the header's
// hash constant equals that computation. It then drops the count word and
// asserts the number moves. A driver that computed the hash with its own
// code "would agree with whatever that code happened to do"; here the
// fixture is the oracle, not an echo.
//
// Run: java -cp <classpath from make/java.mk> test/conformance/java/rows/R4.java

package test.conformance.java.rows;

// Standalone: import nothing from generated code.
public final class R4 {
    private R4() {}

    // fnv1a64 written out LONGHAND: the oracle, not borrowed from the code
    // under test. (§5's fnv1a64 over the layout's bytes as written.)
    private static long fnv1a64(byte[] b, int at, int length) {
        long h = 0xcbf29ce484222325L;
        for (int i = 0; i < length; i++) {
            h ^= (b[at + i] & 0xFFL);
            h *= 0x100000001b3L;
        }
        return h;
    }

    // The layout bytes as written from FuRootFixed (build/tables-generated-java/fu1/).
    // First four bytes are the 4-byte entry count (14 = 0x0000000e LE).
    private static final byte[] LAYOUT_AS_WRITTEN = {
        14, 0, 0, 0, // entry count
        -76, 61, -98, -63, 36, 59, -34, 18, 13, 37, 0, 0, 0, 6, 0, 0, 0,
        23, 11, -116, 8, 121, -48, -14, -43, 1, 1, 0, 0, 0, 0, 0, 0, 0,
        -35, 124, 88, -47, -70, -5, -8, 59, 35, 5, 0, 0, 0, 1, 0, 0, 0,
        -35, 124, 88, -47, -70, -5, -8, 59, 4, 4, 0, 0, 0, 0, 0, 0, 0,
        -40, -69, 19, -59, 13, -60, 11, -65, 15, 21, 0, 0, 0, 2, 0, 0, 0,
        7, -78, 82, 22, 78, 25, 77, -3, 13, 4, 0, 0, 0, 1, 0, 0, 0,
        113, -8, 1, -122, 76, -29, 99, -81, 4, 4, 0, 0, 0, 0, 0, 0, 0,
        98, 124, -30, -45, -66, 23, 122, -39, 13, 20, 0, 0, 0, 3, 0, 0, 0,
        111, 5, 2, -94, -83, -126, -83, 36, 4, 4, 0, 0, 0, 0, 0, 0, 0,
        61, 98, -53, -113, -20, -4, -9, 57, 12, 12, 0, 0, 0, 0, 0, 0, 0,
        -93, 110, 8, -59, -90, -109, -25, -34, 4, 4, 0, 0, 0, 0, 0, 0, 0,
        35, -41, -104, 7, -17, 86, 76, -39, 4, 4, 0, 0, 0, 0, 0, 0, 0,
        -64, -54, 126, -50, -94, 63, 59, 31, 3, 2, 0, 0, 0, 0, 0, 0, 0,
        -45, -16, -103, 95, -52, 2, -113, 10, 10, 4, 0, 0, 0, 0, 0, 0, 0,
    };

    // The hash constant from FuRootFixed.
    private static final long HASH_CONSTANT = 0x1152d0025fcf499dL;

    public static void main(String[] args) {
        // THE RULE, BY NAME: the layout hash is fnv1a64 over the layout's bytes
        // as written, the 4-byte entry count included.
        long computed = fnv1a64(LAYOUT_AS_WRITTEN, 0, LAYOUT_AS_WRITTEN.length);
        if (HASH_CONSTANT != computed) {
            System.err.println("R4 FAILED: layout hash != fnv1a64 over layout as written");
            System.err.println("  constant: " + Long.toHexString(HASH_CONSTANT));
            System.err.println("  computed: " + Long.toHexString(computed));
            System.exit(1);
        }

        // THE COUNT IS IN THE INPUT: dropping it must move the number.
        long withoutCount = fnv1a64(LAYOUT_AS_WRITTEN, 4, LAYOUT_AS_WRITTEN.length - 4);
        if (HASH_CONSTANT == withoutCount) {
            System.err.println("R4 FAILED: dropping 4-byte count leaves hash unchanged");
            System.err.println("  hash constant: " + Long.toHexString(HASH_CONSTANT));
            System.err.println("  without count: " + Long.toHexString(withoutCount));
            System.exit(1);
        }

        System.out.println("R4 PASS");
        System.exit(0);
    }
}
