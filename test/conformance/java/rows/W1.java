// W1 — write slack is template zeros (schema matrix cell java/W1).
//
// docs/FIXED-FORM-ALGORITHM.md:1724, fix 1: "Text and array slack — the writer
// writes `length` units and `count` elements onto the zeroed template and
// stops, never the caller's leftovers or an element's default image."
// docs/SPEC-TABLES.md:6745, "ZERO ON WRITE: A writer zero-fills every byte of
// slack — the bytes past a string's length, past an array's count, behind a
// union's narrower arm, and under an absent optional's present flag."
//
// PRODUCTION PATH UNDER TEST (the actual call site):
//   blockdemo.PaddedFrameFixed.save(Value[], count, byte[])   <- the entrypoint
//     -> PaddedFrameFixed.writeHeader                       (the file header)
//     -> PaddedFrameFixed.writeBody                         (per record)
//          -> PaddedRowFixed.writeBody                      (live array elements)
// The buffer is pre-filled with 0xFF so any byte the writer does NOT land keeps
// a caller leftover and this test can see it. Every byte of declared slack —
// the array slots past `rowsCount`, the bytes past a string's `labelLength`,
// the region behind a clear present flag, and the bytes past `blobLength` —
// must come out zero.
//
// Runnable as a single source file, the way the driver is run:
//   java -cp build/conformance-java test/conformance/java/rows/W1.java
import blockdemo.PaddedFrameFixed;
import blockdemo.PaddedRowFixed;

public final class W1 {
    private W1() {}

    private static int failures = 0;

    private static void check(String what, boolean ok) {
        System.out.println("TestRowW1 " + what + ": " + (ok ? "PASS" : "FAIL"));
        if (!ok) {
            failures++;
        }
    }

    private static int get32(byte[] b, int at) {
        return (b[at] & 0xFF)
                | ((b[at + 1] & 0xFF) << 8)
                | ((b[at + 2] & 0xFF) << 16)
                | ((b[at + 3] & 0xFF) << 24);
    }

    private static long get64(byte[] b, int at) {
        long v = 0;
        for (int i = 7; i >= 0; i--) {
            v = (v << 8) | (b[at + i] & 0xFFL);
        }
        return v;
    }

    // a run of bytes all zero?
    private static boolean zeros(byte[] b, int from, int to) {
        for (int i = from; i < to; i++) {
            if (b[i] != 0) {
                return false;
            }
        }
        return true;
    }

    public static void main(String[] args) {
        final long stamp = 0x1122334455667788L;

        final PaddedFrameFixed.Value v = new PaddedFrameFixed.Value();
        v.marker = (byte) 0x11;
        v.stamp = stamp;
        // ONE live row. Everything past it is slack that the writer must zero.
        v.rowsCount = 1;
        v.rows[0].tag = (byte) 0x22;
        v.rows[0].value = 1.5;
        v.rows[0].flag = true;
        v.rows[0].id = 0xCAFEBABE;
        v.rows[0].label[0] = 'A';
        v.rows[0].label[1] = 'B';
        v.rows[0].labelLength = 2;
        java.util.Arrays.fill(v.rows[0].label, 2, 15, (byte) 0xFF); // caller leftover
        v.rows[0].slots[0] = 1;
        v.rows[0].slots[1] = 2;
        v.rows[0].slots[2] = 3;
        v.rows[0].slots[3] = 4;
        v.rows[0].counterPresent = false;
        v.rows[0].counter = 0x0BADF00D; // must NOT be written under a clear flag
        // The UNUSED element carries a fully-populated "default image"; the
        // writer must stop at `rowsCount` and never put this storage on the wire.
        PaddedRowFixed.Value unused = v.rows[1];
        unused.tag = (byte) 0x7F;
        unused.value = 9.25;
        unused.flag = true;
        unused.id = 0x7F7F7F7F;
        unused.labelLength = 4;
        java.util.Arrays.fill(unused.label, (byte) 0x7F);
        java.util.Arrays.fill(unused.slots, (short) 0x7F7F);
        java.util.Arrays.fill(unused.teams, (byte) 0x7F);
        unused.counterPresent = true;
        unused.counter = 0x7F7F7F7F;
        v.blob[0] = (byte) 0xAA;
        v.blob[1] = (byte) 0xBB;
        v.blobLength = 2;
        java.util.Arrays.fill(v.blob, 2, 12, (byte) 0xFF); // caller leftover

        final int need = PaddedFrameFixed.measure(1);
        final byte[] buf = new byte[need];
        java.util.Arrays.fill(buf, (byte) 0xFF); // caller leftovers everywhere

        check("save returns measure", PaddedFrameFixed.save(new PaddedFrameFixed.Value[] { v }, 1, buf) == need);

        final int body = PaddedFrameFixed.headerBytes + 8;
        final int rows = body + 13;

        // The live extent is landed.
        check("marker landed", buf[body + 0] == (byte) 0x11);
        check("stamp landed", get64(buf, body + 1) == stamp);
        check("rowsCount landed", get32(buf, body + 9) == 1);
        check("row0 tag landed", buf[rows + 0] == (byte) 0x22);
        check("row0 flag landed as 1", buf[rows + 9] == 1);
        check("row0 id landed", get32(buf, rows + 10) == 0xCAFEBABE);
        check("row0 labelLength landed", get32(buf, rows + 14) == 2);
        check("row0 label live", buf[rows + 18] == 'A' && buf[rows + 19] == 'B');

        // ARRAY slack: the slots past `rowsCount`, and the unused element's
        // whole default image, are zeros.
        check("array slack is template zeros", zeros(buf, rows + 50, body + 3213));

        // TEXT slack: the bytes past the string's length are zeros.
        check("string slack is template zeros", zeros(buf, rows + 18 + 2, rows + 33));

        // An ABSENT optional lands a zero flag and a zero payload, never the
        // storage the caller left.
        check("absent optional flag is zero", buf[rows + 45] == 0);
        check("absent optional payload is zero", zeros(buf, rows + 46, rows + 50));

        // `bytes(12)` slack: live length copied, the rest zeros.
        check("blobLength landed", get32(buf, body + 3213) == 2);
        check("blob live", buf[body + 3217] == (byte) 0xAA && buf[body + 3218] == (byte) 0xBB);
        check("bytes slack is template zeros", zeros(buf, body + 3217 + 2, body + 3229));

        System.out.println("TestRowW1 result: " + (failures == 0 ? "GREEN" : "RED"));
        if (failures != 0) {
            System.exit(1);
        }
    }
}