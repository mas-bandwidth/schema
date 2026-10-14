// RESULT cell-java-f7 -- "malformed, under 20 bytes" (schema#876 F7)
//
// Law (docs/FIXED-FORM-ALGORITHM.md:909): a file under 20 bytes answers
// malformed=true, refused=false, n=-1, all counters zero.
//
// The production entrypoint is TableFixed.readHeader (fixedruntime.go:225),
// called by <Table>Fixed.load() (fixedform.go:1583). This test calls
// readHeader directly on short arrays.
//
// The binding constraint: fileHeaderBytes (16) + the layout length prefix (4)
// = 20 bytes. Any input shorter than 20 bytes hits the length guard at
// fixedruntime.go:238 before any layout or hash logic.
//
// RUN: java -cp build/conformance-java test/conformance/java/rows/F7.java

import tabledemo.TableFixed;

public class F7 {
    public static void main(String[] args) {
        int passed = 0;
        int failed = 0;

        // k from 0 to 19: every byte count under the 20-byte threshold
        for (int k = 0; k < 20; k++) {
            byte[] data = new byte[k];
            if (k >= 1) {
                data[0] = 3; // form byte 3 (fixed form)
            }
            TableFixed.Header head = new TableFixed.Header();
            TableFixed.Report rep = new TableFixed.Report();
            rep.reset();

            boolean ret = TableFixed.readHeader(data, head, rep);

            if (ret) {
                System.out.println("FAIL k=" + k + ": readHeader returned true, want false (malformed)");
                failed++;
                continue;
            }
            if (!rep.malformed) {
                System.out.println("FAIL k=" + k + ": report.malformed=false, want true");
                failed++;
                continue;
            }
            if (rep.refused) {
                System.out.println("FAIL k=" + k + ": report.refused=true, want false (malformed is the other answer)");
                failed++;
                continue;
            }
            passed++;
        }

        // A 20-byte file with form byte 3 must NOT trigger the under-20 check
        byte[] full = new byte[20];
        full[0] = 3;
        TableFixed.Header head = new TableFixed.Header();
        TableFixed.Report rep = new TableFixed.Report();
        rep.reset();
        boolean ret = TableFixed.readHeader(full, head, rep);
        if (rep.malformed) {
            System.out.println("FAIL k=20: report.malformed=true from a 20-byte file (incorrect)");
            failed++;
        } else {
            System.out.println("INFO k=20: malformed=" + rep.malformed + " refused=" + rep.refused + " (not under 20; may fail on layout)");
            passed++;
        }

        System.out.println("RESULT: " + passed + " passed, " + failed + " failed");
        System.exit(failed > 0 ? 1 : 0);
    }
}