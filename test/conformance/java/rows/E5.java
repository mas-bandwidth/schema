// java/E5 — "?T vs plain nesting"
//
// docs/FIXED-FORM-ALGORITHM.md:182 — "?T: 1 + C(T) — the present flag THEN the
// payload, which rides WHOLE"
// docs/SPEC-TABLES.md:6556-6565 — "KIND 35 IS THE ONE KIND THIS LAYOUT ADDS
// TO §3'S CLOSED SET... its size is one present byte plus its child's... ?T
// and a plain T nesting are WIRE-IDENTICAL... ON THIS FORM THEY ARE ONE BYTE
// APART"
//
// THE FIXTURE: P1.schema nests Link by value (plain T), P3.schema marks the
// same field `?Link` (optional T). On form 3 the two layouts differ by the
// present flag, and the kind changes from 13 (struct) to 35 (optional wrapper).
// This test verifies both formats write and read correctly.
//
// Run: java -cp build/conformance-java test/conformance/java/rows/E5.java
// exit 0 green, exit 1 red, one printed line per assertion.

public final class E5 {
    private E5() {}

    private static int failures = 0;

    private static void check(String what, boolean ok) {
        System.out.println(ok ? "ok   " + what : "FAILED " + what);
        if (!ok) { failures++; }
    }

    // layout entry: struct Entry { id : u64 ; kind : u8 ; size : u32 ; children : u32 } (17 bytes)
    // docs/FIXED-FORM-ALGORITHM.md:33
    private static int u32(byte[] b, int at) {
        return (b[at] & 0xff) | ((b[at + 1] & 0xff) << 8)
                | ((b[at + 2] & 0xff) << 16) | ((b[at + 3] & 0xff) << 24);
    }

    private static int layoutKind(byte[] layout, int entryIndex) {
        return layout[4 + entryIndex * 17 + 8] & 0xff;
    }

    private static int layoutSize(byte[] layout, int entryIndex) {
        return u32(layout, 4 + entryIndex * 17 + 9);
    }

    public static void main(String[] args) {
        // --- P1: plain Link nesting ---
        check("P1 ChainFixed bodyBytes == 36", tblp1.ChainFixed.bodyBytes == 36);
        check("P1 ChainFixed link entry kind == 13 (struct)",
                layoutKind(tblp1.ChainFixed.layout, 2) == 13);
        check("P1 ChainFixed link entry size == 16",
                layoutSize(tblp1.ChainFixed.layout, 2) == 16);

        // --- P3: ?Link optional nesting ---
        check("P3 ChainFixed bodyBytes == 37", tblp3.ChainFixed.bodyBytes == 37);
        check("P3 ChainFixed optional wrapper kind == 35",
                layoutKind(tblp3.ChainFixed.layout, 2) == 35);
        check("P3 ChainFixed optional wrapper size == 17 (1 present + 16 payload)",
                layoutSize(tblp3.ChainFixed.layout, 2) == 17);
        check("P3 ChainFixed nested Link entry kind == 13",
                layoutKind(tblp3.ChainFixed.layout, 3) == 13);

        // --- P1 write-then-read round-trip ---
        tblp1.ChainFixed.Value p1v = new tblp1.ChainFixed.Value();
        p1v.nameLength = 4;
        p1v.name[0] = 'T'; p1v.name[1] = 'e'; p1v.name[2] = 's'; p1v.name[3] = 't';
        p1v.link.value = 42;
        p1v.link.tagLength = 3;
        p1v.link.tag[0] = 'a'; p1v.link.tag[1] = 'b'; p1v.link.tag[2] = 'c';
        int need = tblp1.ChainFixed.measure(1);
        byte[] p1buf = new byte[need];
        int wrote = tblp1.ChainFixed.save(new tblp1.ChainFixed.Value[] { p1v }, 1, p1buf);
        check("P1 save returns measure", wrote == need);
        tblp1.ChainFixed.Value p1back = new tblp1.ChainFixed.Value();
        tblp1.TableFixed.Report r1 = new tblp1.TableFixed.Report();
        int n1 = tblp1.ChainFixed.load(
            new tblp1.ChainFixed.Value[] { p1back }, 1, p1buf,
            tblp1.TableFixed.plan(8192), new short[8192], tblp1.ChainFixed.image(), r1);
        check("P1 load returns 1", n1 == 1);
        check("P1 load no refusal", !r1.refused);
        check("P1 load name", p1back.nameLength == 4
                && p1back.name[0] == 'T' && p1back.name[1] == 'e'
                && p1back.name[2] == 's' && p1back.name[3] == 't');
        check("P1 load link value", p1back.link.value == 42);
        check("P1 load link tag", p1back.link.tagLength == 3
                && p1back.link.tag[0] == 'a'
                && p1back.link.tag[1] == 'b'
                && p1back.link.tag[2] == 'c');

        // --- P3 write-then-read round-trip (present) ---
        tblp3.ChainFixed.Value p3v = new tblp3.ChainFixed.Value();
        p3v.nameLength = 4;
        p3v.name[0] = 'T'; p3v.name[1] = 'e'; p3v.name[2] = 's'; p3v.name[3] = 't';
        p3v.linkPresent = true;
        p3v.link.value = 42;
        p3v.link.tagLength = 3;
        p3v.link.tag[0] = 'a'; p3v.link.tag[1] = 'b'; p3v.link.tag[2] = 'c';
        need = tblp3.ChainFixed.measure(1);
        byte[] p3buf = new byte[need];
        wrote = tblp3.ChainFixed.save(new tblp3.ChainFixed.Value[] { p3v }, 1, p3buf);
        check("P3 present save returns measure", wrote == need);
        tblp3.ChainFixed.Value p3back = new tblp3.ChainFixed.Value();
        tblp3.TableFixed.Report r3 = new tblp3.TableFixed.Report();
        int n3 = tblp3.ChainFixed.load(
            new tblp3.ChainFixed.Value[] { p3back }, 1, p3buf,
            tblp3.TableFixed.plan(8192), new short[8192], tblp3.ChainFixed.image(), r3);
        check("P3 present load returns 1", n3 == 1);
        check("P3 present load no refusal", !r3.refused);
        check("P3 present load linkPresent", p3back.linkPresent);
        check("P3 present load link value", p3back.link.value == 42);

        // --- P3 write-then-read round-trip (absent) ---
        tblp3.ChainFixed.Value p3a = new tblp3.ChainFixed.Value();
        p3a.nameLength = 4;
        p3a.name[0] = 'T'; p3a.name[1] = 'e'; p3a.name[2] = 's'; p3a.name[3] = 't';
        p3a.linkPresent = false;
        need = tblp3.ChainFixed.measure(1);
        byte[] p3abuf = new byte[need];
        wrote = tblp3.ChainFixed.save(new tblp3.ChainFixed.Value[] { p3a }, 1, p3abuf);
        check("P3 absent save returns measure", wrote == need);
        tblp3.ChainFixed.Value p3aback = new tblp3.ChainFixed.Value();
        tblp3.TableFixed.Report r3a = new tblp3.TableFixed.Report();
        n3 = tblp3.ChainFixed.load(
            new tblp3.ChainFixed.Value[] { p3aback }, 1, p3abuf,
            tblp3.TableFixed.plan(8192), new short[8192], tblp3.ChainFixed.image(), r3a);
        check("P3 absent load returns 1", n3 == 1);
        check("P3 absent load no refusal", !r3a.refused);
        check("P3 absent load linkPresent false", !p3aback.linkPresent);
        check("P3 absent load link value == 0", p3aback.link.value == 0);
        check("P3 absent load link tagLength == 0", p3aback.link.tagLength == 0);

        // --- Byte-level check: P3 body is one byte larger than P1 ---
        check("P3 recordBytes == P1 recordBytes + 1",
                tblp3.ChainFixed.recordBytes == tblp1.ChainFixed.recordBytes + 1);

        // --- For a present optional, both formats carry the same name bytes ---
        // Both records have name at offsets 0-19 (4-byte length + 16 bytes text)
        int p1RecordAt = tblp1.ChainFixed.headerBytes + 8;
        int p3RecordAt = tblp3.ChainFixed.headerBytes + 8;
        boolean sameName = true;
        for (int i = 0; i < 20; i++) {
            if (p1buf[p1RecordAt + i] != p3buf[p3RecordAt + i]) {
                sameName = false;
                break;
            }
        }
        check("P1 and P3 have same name bytes", sameName);

        if (failures != 0) {
            System.out.println(failures + " failure(s)");
            System.exit(1);
        }
        System.out.println("E5 green: ?T vs plain nesting on the Java leg");
    }
}