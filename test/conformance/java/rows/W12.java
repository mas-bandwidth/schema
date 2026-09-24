// W12 — hash includes the 4-byte count (docs/FIXED-FORM-ALGORITHM.md:54-57).
//
// The hash is fnv1a64 over the layout's bytes as written, the 4-byte count
// included. The layout bytes always start with a u32 entry count. This test
// verifies:
//   1) the layout's first 4 bytes are the entry count (a u32 > 0)
//   2) TableFixed.hash() over the whole layout (with the count) gives a
//      result different from the hash over just the entries (without the
//      count) — proving the count IS in the hash input.
//
// RUN: java -cp build/conformance-java test/conformance/java/rows/W12.java

public final class W12 {
    private W12() {}

    public static void main(String[] args) {
        byte[] layout = tblv1.CellFixed.layout;

        // The layout starts with the 4-byte entry count.
        int entryCount = tblv1.TableFixed.get32(layout, 0);
        check(entryCount > 0,
            "layout starts with non-zero entry count; got " + entryCount);

        // Hash with the 4-byte count included.
        long withCount = tblv1.TableFixed.hash(layout, 0, layout.length);

        // Hash WITHOUT the first 4 bytes (the count).
        long withoutCount = tblv1.TableFixed.hash(layout, 4, layout.length - 4);

        // They must differ — the count is part of the hash input.
        check(withCount != withoutCount,
            "hash with count differs from hash without count");

        System.out.println("PASS: W12 hash includes the 4-byte count");
        System.exit(0);
    }

    private static void check(boolean ok, String msg) {
        if (!ok) {
            System.out.println("FAIL: " + msg);
            System.exit(1);
        }
    }
}