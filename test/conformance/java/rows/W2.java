// java/W2 — "absent optional skips store"
//
// docs/FIXED-FORM-ALGORITHM.md:203-206 — "An absent optional writes flag 0
// and skips the payload store: a declared default under a clear flag is
// meaning too (fix 13)."
//
// THE FIXTURE: P3.schema declares `link ?Link`. When the present flag is
// clear (false), the writer must set the flag byte to 0 and leave the 16
// payload bytes as template zeros — never the caller's leftovers. This test
// verifies that by writing with a pre-0xFF'd buffer and checking the bytes.
//
// Run: java -cp build/conformance-java test/conformance/java/rows/W2.java
// exit 0 green, exit 1 red, one printed line per assertion.

public final class W2 {
    private W2() {}

    private static int failures = 0;

    private static void check(String what, boolean ok) {
        System.out.println(ok ? "ok   " + what : "FAILED " + what);
        if (!ok) { failures++; }
    }

    private static boolean zeros(byte[] b, int from, int to) {
        for (int i = from; i < to; i++) {
            if (b[i] != 0) return false;
        }
        return true;
    }

    private static int u32(byte[] b, int at) {
        return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8)
                | ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
    }

    public static void main(String[] args) {
        // P3 Chain body layout:
        //   0-3:   name length (u32)
        //   4-19:  name buffer (16 bytes of text)
        //   20:    link present flag (u8)
        //   21-36: Link payload (value u32 + tag length u32 + tag buffer 8 bytes)
        // bodyBytes == 37

        final int nameOff = 0;
        final int presentOff = 20;
        final int payloadOff = 21;
        final int payloadLen = 16;

        // --- TEST 1: absent optional (linkPresent = false) ---
        // Fill buffer with 0xFF to detect any byte the writer misses.
        byte[] buf = new byte[tblp3.ChainFixed.bodyBytes];
        java.util.Arrays.fill(buf, (byte) 0xFF);

        tblp3.ChainFixed.Value absent = new tblp3.ChainFixed.Value();
        absent.nameLength = 4;
        absent.name[0] = 'T'; absent.name[1] = 'e'; absent.name[2] = 's'; absent.name[3] = 't';
        absent.linkPresent = false;
        // Leave link payload at defaults (value=0, tagLength=0, tag all zeros)

        tblp3.ChainFixed.writeBody(buf, 0, absent);

        // The name must be written.
        check("W2 absent: name length landed", u32(buf, nameOff) == 4);
        check("W2 absent: name byte landed", buf[4] == 'T');

        // The present flag must be 0.
        check("W2 absent: present flag is 0", buf[presentOff] == 0);

        // The 16 payload bytes must be zero (skipped payload store) —
        // never the 0xFF the caller left.
        check("W2 absent: payload is template zeros (skipped)",
                zeros(buf, payloadOff, payloadOff + payloadLen));
        check("W2 absent: no 0xFF leaked into payload",
                buf[payloadOff] == 0);

        // --- TEST 2: present optional (linkPresent = true) ---
        java.util.Arrays.fill(buf, (byte) 0xFF);

        tblp3.ChainFixed.Value present = new tblp3.ChainFixed.Value();
        present.nameLength = 4;
        present.name[0] = 'T'; present.name[1] = 'e'; present.name[2] = 's'; present.name[3] = 't';
        present.linkPresent = true;
        present.link.value = 42;
        present.link.tagLength = 3;
        present.link.tag[0] = 'a'; present.link.tag[1] = 'b'; present.link.tag[2] = 'c';

        tblp3.ChainFixed.writeBody(buf, 0, present);

        check("W2 present: name length landed", u32(buf, nameOff) == 4);
        check("W2 present: present flag is 1", buf[presentOff] == 1);
        // Payload must be written (non-zero value).
        check("W2 present: link value landed", u32(buf, payloadOff) == 42);
        check("W2 present: link tag length landed", u32(buf, payloadOff + 4) == 3);
        check("W2 present: link tag byte landed", buf[payloadOff + 8] == 'a');

        // --- TEST 3: save/load round-trip with absent optional ---
        int need = tblp3.ChainFixed.measure(1);
        byte[] file = new byte[need];
        java.util.Arrays.fill(file, (byte) 0xFF);
        tblp3.ChainFixed.Value av = new tblp3.ChainFixed.Value();
        av.nameLength = 4;
        av.name[0] = 'T'; av.name[1] = 'e'; av.name[2] = 's'; av.name[3] = 't';
        av.linkPresent = false;
        int wrote = tblp3.ChainFixed.save(new tblp3.ChainFixed.Value[] { av }, 1, file);
        check("W2 save returns measure", wrote == need);

        tblp3.ChainFixed.Value back = new tblp3.ChainFixed.Value();
        tblp3.TableFixed.Report r = new tblp3.TableFixed.Report();
        int n = tblp3.ChainFixed.load(
            new tblp3.ChainFixed.Value[] { back }, 1, file,
            tblp3.TableFixed.plan(8192), new short[8192], tblp3.ChainFixed.image(), r);
        check("W2 load returns 1", n == 1);
        check("W2 load no refusal", !r.refused);
        check("W2 load linkPresent false", !back.linkPresent);
        // Absent optional: link value is at defaults (0).
        check("W2 load link value == 0", back.link.value == 0);
        check("W2 load link tagLength == 0", back.link.tagLength == 0);

        // --- TEST 4: save/load round-trip with present optional ---
        java.util.Arrays.fill(file, (byte) 0xFF);
        tblp3.ChainFixed.Value pv = new tblp3.ChainFixed.Value();
        pv.nameLength = 4;
        pv.name[0] = 'T'; pv.name[1] = 'e'; pv.name[2] = 's'; pv.name[3] = 't';
        pv.linkPresent = true;
        pv.link.value = 42;
        pv.link.tagLength = 3;
        pv.link.tag[0] = 'a'; pv.link.tag[1] = 'b'; pv.link.tag[2] = 'c';
        wrote = tblp3.ChainFixed.save(new tblp3.ChainFixed.Value[] { pv }, 1, file);
        check("W2 present save returns measure", wrote == need);

        tblp3.ChainFixed.Value pb = new tblp3.ChainFixed.Value();
        tblp3.TableFixed.Report rp = new tblp3.TableFixed.Report();
        n = tblp3.ChainFixed.load(
            new tblp3.ChainFixed.Value[] { pb }, 1, file,
            tblp3.TableFixed.plan(8192), new short[8192], tblp3.ChainFixed.image(), rp);
        check("W2 present load returns 1", n == 1);
        check("W2 present load no refusal", !rp.refused);
        check("W2 present load linkPresent true", pb.linkPresent);
        check("W2 present load link value == 42", pb.link.value == 42);

        if (failures != 0) {
            System.out.println(failures + " failure(s)");
            System.exit(1);
        }
        System.out.println("W2 green: absent optional skips store on the Java leg");
    }
}