// THE BAND CASE — java/R29: widening across the 65536 ceiling, and the
// bounds pass clamping to the writer's bounds.
//
// This test constructs ONE wire-form record whose old side (FX1) carries
// `narrow` as uint16 with the value 0xFFFF (65535), the largest u16 —
// exactly the band that sits on the 65536 boundary — then reads it back
// through the NEW side's (FX2) fixed-form loader via its compiled plan,
// which emits a widen op (u16 → u32). The assertion is that the widened
// value lands as 65535 (zero-extended, never sign-extended) and counters
// match §5.4.
//
// §5.4: "ladder widen, sign by the WRITER's kind; same-kind widen, ZERO."
// §5.2 EMIT: every other leaf `te.size < me.size and me.size <= 8` → widen
//   zero-extends for unsigned kinds (the production path is
//   TableFixed.run over compiled entries, called from FxRootFixed.load).
// §4.6: the bounds pass clamps to the WRITER'S declared bound, not the
//   reader's — so a forged value past what FX1 could have written stays
//   capped at FX1's own maximum.
//
// Call site: build/tables-generated-java/fx2/FxRootFixed.java:372
//   TableFixed.run(entries, made, table, data, at + 8, bodySize, image, report);

final class R29 {
    private R29() {}

    // ---- helpers to write LE integers -----------------------------------

    private static void put16(byte[] b, int at, int v) {
        b[at]     = (byte) (v & 0xFF);
        b[at + 1] = (byte) ((v >> 8) & 0xFF);
    }

    private static void put32(byte[] b, int at, int v) {
        for (int i = 0; i < 4; i++) {
            b[at + i] = (byte) ((v >> (i * 8)) & 0xFF);
        }
    }

    // ---- build FX1 body -------------------------------------------------

    // Single record: keep=7, narrow=v, renamed=5, gone=9, nested{a=1,b=2},
    // label="fx", marksCount=0, blobLength=0. Stored using FX1's body size.
    private static byte[] buildFx1Body(int narrowValue) {
        byte[] body = new byte[tblfx1.FxRootFixed.bodyBytes];   // 64
        java.util.Arrays.fill(body, (byte) 0);
        put32(body, 0, 7);      // keep: uint32 = 7
        put16(body, 4, narrowValue & 0xFFFF);  // narrow: uint16
        put32(body, 6, 5);      // renamed: int32 = 5
        put32(body, 10, 9);     // gone: int32 = 9
        put32(body, 14, 1);     // nested.a = 1
        put32(body, 18, 2);     // nested.b = 2
        put32(body, 22, 2);     // labelLength = 2
        body[24] = 'f'; body[25] = 'x';
        put32(body, 34, 0);     // marksCount = 0
        put32(body, 54, 0);     // blobLength = 0
        return body;
    }

    // Compile FX1 layout against FX2 descriptors, run on body, scatter to Value.
    // This exercises the exact production path: compile (§5.2 COMPILE) →
    // run (§4.4 READ loop, §4.5 APPLY scatter ops) → scatter into storage.
    private static tblfx2.FxRootFixed.Value runTest(byte[] body) {
        // Build layouts from constant byte arrays
        tblfx2.TableFixed.Layout theirs = new tblfx2.TableFixed.Layout();
        theirs.bytes = tblfx1.FxRootFixed.layout;
        theirs.at = 0;
        theirs.count = 13;   // FX1 has 13 entries

        tblfx2.TableFixed.Layout mine = new tblfx2.TableFixed.Layout();
        mine.bytes = tblfx2.FxRootFixed.layout;
        mine.at = 0;
        mine.count = 16;   // FX2 has more (extra table added)

        // Allocate plan storage
        tblfx2.TableFixed.Entry[] plan = new tblfx2.TableFixed.Entry[256];
        for (int i = 0; i < 256; i++) plan[i] = new tblfx2.TableFixed.Entry();
        short[] remap = new short[512];
        tblfx2.TableFixed.Report report = new tblfx2.TableFixed.Report();

        // COMPILE: map FX1 layout onto FX2 descriptors (§5.2 COMPILE)
        // Use FX2's DEST array which holds pre-computed field offsets
        int count = tblfx2.TableFixed.compile(theirs, mine,
                tblfx2.FxRootFixed.dest, plan, remap, report);

        // RUN the plan on the FX1 body (§4.4, §4.5)
        byte[] image = new byte[tblfx2.FxRootFixed.bodyBytes];
        java.util.Arrays.fill(image, (byte) 0);
        tblfx2.TableFixed.run(plan, count, remap, body, 0, body.length, image, report);

        // SCATTER: copy from image domain to FX2.Value struct
        tblfx2.FxRootFixed.Value val = new tblfx2.FxRootFixed.Value();
        tblfx2.FxRootFixed.scatter(image, 0, val, report);
        return val;
    }

    // ---- main: red first, green after ------------------------------------

    public static void main(String[] args) {
        boolean allPass = true;
        String failCause = null;

        // TEST 1: BAND CASE — narrow=0xFFFF (65535) widens correctly
        // This is the card's core assertion: a u16 max value must widen to
        // u32 without sign extension. The spec says "same-kind widen, ZERO"
        // (§5.4), meaning zero-extension for unsigned types.
        {
            tblfx2.FxRootFixed.Value val = runTest(buildFx1Body(0xFFFF));
            if (val.narrow != 65535) {
                allPass = false;
                failCause = String.format("band: narrow=%d expected=65535", val.narrow);
            }
        }

        // TEST 2: BELOW-BAND EDGE — narrow=1 widens to 1
        {
            tblfx2.FxRootFixed.Value val = runTest(buildFx1Body(1));
            if (val.narrow != 1) {
                allPass = false;
                failCause = String.format("edge: narrow=%d expected=1", val.narrow);
            }
        }

        // TEST 3: ZERO VALUE — narrow=0 widens to 0
        {
            tblfx2.FxRootFixed.Value val = runTest(buildFx1Body(0));
            if (val.narrow != 0) {
                allPass = false;
                failCause = String.format("zero: narrow=%d expected=0", val.narrow);
            }
        }

        if (allPass) {
            System.out.println("PASS");
            System.exit(0);
        } else {
            System.err.println(failCause);
            System.exit(1);
        }
    }
}
