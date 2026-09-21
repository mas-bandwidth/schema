// W7.java — "arg and meta are two lanes" (cell java/W7).
//
// The spec: `arg` is the guard's ordinal and `meta` is the op's own
// argument (a text flavour). They are two facts that must never share
// one lane (docs/FIXED-FORM-ALGORITHM.md:245). A string(N) under a
// union arm needs both: the arm ordinal for the guard, and the flavour
// (1=utf8) for the text read.
//
// UT1.schema puts a string(8) under the SECOND arm (ordinal 2, flavour
// 1). The test writes a UT1 file with arm b's label = "hello", reads it
// through a compiled plan that guards b's fields on ordinal 2 while the
// reader's own tag is ordinal 3, and verifies the label survives as
// UTF-8.
//
// If arg and meta shared one lane, the flavour (1) would overwrite the
// guard ordinal (2) and the text entry would either not fire or read as
// WIDE.

package rows;

import tblut.*;
import tblut.TableFixed;

public final class W7 {
    private W7() {}

    // ---- the UT1 wire vector: one record, arm b, label "hello"
    //
    // Built using the generated tblut writers. The file is form 3 with
    // the UT1 layout hash and one record.

    private static byte[] buildUt1File() {
        byte[] body = new byte[UtRootFixed.bodyBytes];
        UtPickFixed.Value pick = new UtPickFixed.Value();
        pick.type = UtPickFixed.b; // arm b, ordinal 2
        byte[] hello = "hello".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(hello, 0, pick.b.label, 0, hello.length);
        pick.b.labelLength = hello.length;
        pick.b.grade = 0; // bronze
        UtPickFixed.writeBody(body, 0, pick);
        TableFixed.put32(body, 18, 3); // tail = 3

        int headerLen = UtRootFixed.headerBytes;
        int totalLen = headerLen + UtRootFixed.recordBytes;
        byte[] file = new byte[totalLen];
        UtRootFixed.writeHeader(file);
        TableFixed.put64(file, headerLen, UtRootFixed.hash);
        System.arraycopy(body, 0, file, headerLen + 8, UtRootFixed.bodyBytes);
        return file;
    }

    // ---- a compiled plan that reads UT1's body
    //
    // Record body layout (after the 8-byte hash):
    //   offset  0: pick tag (1 byte) — value 2 means arm b
    //   offset  1: pick body (17 bytes)
    //     offset  1: b.labelLength (4 bytes, int32 LE)
    //     offset  5: b.label (up to 8 bytes, UTF-8)
    //     offset 13: b.grade (1 byte)
    //     offset 14: b.m (4 bytes, int32 LE)
    //   offset 18: tail (4 bytes, int32 LE)
    //
    // Image layout (same as UtRootFixed):
    //   offset  0: pick tag (1 byte)
    //   offset  1: pick body (17 bytes)
    //     offset  1: b.labelLength (4 bytes)
    //     offset  5: b.label (8 bytes)
    //     offset 13: b.grade (1 byte)
    //     offset 14: b.m (4 bytes)
    //   offset 18: tail (4 bytes)
    //
    // The opText entry:
    //   src = offset of the LENGTH word in the source (5)
    //   dst = offset of the LENGTH word in the image (1)
    //   aux = offset of the PAYLOAD in the image (5)
    //   size = payload byte span (8)

    private static TableFixed.Entry[] buildCompiledPlan() {
        TableFixed.Entry[] plan = TableFixed.plan(5);
        int guardOffset = 0; // tag is at offset 0 in the union body
        long writerBOrdinal = 2; // UT1's ordinal for b

        // Entry 0: copy the tag (1 byte at src=0 to dst=0) — unguarded
        plan[0].src = 0; plan[0].dst = 0; plan[0].size = 1;
        plan[0].op = TableFixed.opCopy;
        plan[0].guard = TableFixed.noGuard;

        // Entry 1: label — text op, guarded on writer's ordinal 2
        // src=1 (length word in writer's record, right after pick tag),
        // dst=1 (length word in image, right after pick tag),
        // size=8 (payload byte span), aux=5 (payload destination in image)
        // meta=textUtf8 (1), the flavour — NOT the arm ordinal
        plan[1].src = 1; plan[1].dst = 1; plan[1].size = 8; plan[1].aux = 5;
        plan[1].op = TableFixed.opText;
        plan[1].guard = guardOffset;
        plan[1].arg = writerBOrdinal;
        plan[1].meta = TableFixed.textUtf8;

        // Entry 2: grade (1 byte, src=13, dst=13) — guarded
        plan[2].src = 13; plan[2].dst = 13; plan[2].size = 1;
        plan[2].op = TableFixed.opCopy;
        plan[2].guard = guardOffset;
        plan[2].arg = writerBOrdinal;

        // Entry 3: m (4 bytes, src=14, dst=14) — guarded
        plan[3].src = 14; plan[3].dst = 14; plan[3].size = 4;
        plan[3].op = TableFixed.opCopy;
        plan[3].guard = guardOffset;
        plan[3].arg = writerBOrdinal;

        // Entry 4: tail (4 bytes, src=18, dst=18) — unguarded
        plan[4].src = 18; plan[4].dst = 18; plan[4].size = 4;
        plan[4].op = TableFixed.opCopy;
        plan[4].guard = TableFixed.noGuard;

        return plan;
    }

    private static String readLabel(UtArmBFixed.Value b) {
        return new String(b.label, 0, b.labelLength,
                java.nio.charset.StandardCharsets.UTF_8);
    }

    public static void main(String[] args) {
        int pass = 0;
        int fail = 0;

        // ---- build the UT1 file
        byte[] file = buildUt1File();

        // ---- identity plan read (should always work)
        byte[] image = UtRootFixed.image();
        UtRootFixed.Value result = new UtRootFixed.Value();
        TableFixed.Report report = new TableFixed.Report();
        short[] remap = new short[256];

        int n = UtRootFixed.load(new UtRootFixed.Value[]{result}, 1, file,
                UtRootFixed.identityPlan(), remap, image, report);
        if (n != 1) {
            System.out.println("FAIL identity load returned " + n);
            fail++;
        } else if (report.malformed || report.refused) {
            System.out.println("FAIL identity load: malformed=" + report.malformed
                    + " refused=" + report.refused);
            fail++;
        } else {
            String label = readLabel(result.pick.b);
            if ("hello".equals(label) && result.tail == 3) {
                System.out.println("PASS identity read: label=hello, tail=3");
                pass++;
            } else {
                System.out.println("FAIL identity read: label=" + label + " tail=" + result.tail);
                fail++;
            }
        }

        // ---- compiled plan read: the critical test
        //
        // The plan guards b's fields on the writer's ordinal (2), and the
        // text entry uses meta=textUtf8 (1). The two lanes must be separate.
        TableFixed.Entry[] plan = buildCompiledPlan();
        byte[] image2 = UtRootFixed.image();
        TableFixed.Report report2 = new TableFixed.Report();

        int headerLen = UtRootFixed.headerBytes;
        byte[] recordBody = new byte[UtRootFixed.bodyBytes];
        System.arraycopy(file, headerLen + 8, recordBody, 0, UtRootFixed.bodyBytes);

        TableFixed.run(plan, 5, remap, recordBody, 0, recordBody.length, image2, report2);

        if (report2.malformed) {
            System.out.println("FAIL compiled plan: malformed");
            fail++;
        } else if (report2.refused) {
            System.out.println("FAIL compiled plan: refused " + report2.reason);
            fail++;
        } else {
            UtRootFixed.Value result2 = new UtRootFixed.Value();
            UtRootFixed.scatter(image2, 0, result2, report2);

            String label2 = readLabel(result2.pick.b);
            if ("hello".equals(label2)) {
                System.out.println("PASS compiled plan read: label=hello (arg and meta are two lanes)");
                pass++;
            } else {
                System.out.println("FAIL compiled plan read: label=" + label2
                        + " (expected hello; arg/meta likely shared a lane)");
                fail++;
            }
        }

        // ---- negative control: break the text flavour
        //
        // Set meta to textWide instead of textUtf8. A wide read halves the
        // bound (4 units instead of 8 bytes) and reads UTF-16 code units,
        // which will not match "hello" encoded as UTF-8 bytes.
        TableFixed.Entry[] badPlan = buildCompiledPlan();
        badPlan[1].meta = TableFixed.textWide;
        byte[] image3 = UtRootFixed.image();
        TableFixed.Report report3 = new TableFixed.Report();

        TableFixed.run(badPlan, 5, remap, recordBody, 0, recordBody.length, image3, report3);
        UtRootFixed.Value result3 = new UtRootFixed.Value();
        UtRootFixed.scatter(image3, 0, result3, report3);

        String label3 = readLabel(result3.pick.b);
        if (!"hello".equals(label3)) {
            System.out.println("PASS negative control: wrong flavour misreads label (" + label3 + ")");
            pass++;
        } else {
            System.out.println("FAIL negative control: wrong flavour still reads correctly");
            fail++;
        }

        System.out.println(pass + " passed, " + fail + " failed");
        System.exit(fail > 0 ? 1 : 0);
    }
}
