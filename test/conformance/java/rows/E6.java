// java/E6 — "Renaming uses the declared identity"
// (docs/FIXED-FORM-ALGORITHM.md §5.9 #41; docs/SPEC-TABLES.md §5).
//
// THE LAW. A field renamed through `was = "old"` keeps its wire identity:
// the id is fnv1a64 of the WIRE name, and `was` keeps the OLD name, so the
// layout bytes do not move (§5.1) and the rename is not a version at all.
// The harness pairs two generations' fields BY THE WIRE ID, never by name —
// §5.9 #41's sentence, at docs/FIXED-FORM-ALGORITHM.md:1549-1550.
//
// THE PAIR ON THIS CLASSPATH. The conformance driver's classpath
// (build/conformance-java, make/java.mk) carries the V1/V2 fixed-form pair
// (test/tables/V1.schema, test/tables/V2.schema), and V1 -> V2 is exactly
// this edit: V1 declares `name`, V2 renames it `title` with `was = "name"`
// (test/tables/V2.schema:65). This test asserts that the generated V2 reader
// matches `title` by the DECLARED identity — fnv1a64("name") — and not by
// fnv1a64("title"), and that the value lands through the production read
// path in both generations.
//
// THE VECTOR IS CONSTRUCTED HERE, with the derivation in fnv1a64() below:
// the wire id is the fnv1a64 of the wire name (docs/SPEC-TABLES.md §5,
// docs/FIXED-FORM-ALGORITHM.md §1: "id is fnv1a64(wire name)"). The tree
// has no committed fixed-form (form 3) bytes for the rename — the fixed-form
// corpus is generated at build time into build/fixedform-corpus — so the
// record bytes are written by the generated writers in this test, which is
// the same production path the driver reaches.
//
// Run: java -cp build/conformance-java test/conformance/java/rows/E6.java
// exit 0 green, exit 1 red, one printed line per assertion.

public final class TestRowE6 {
    private static int failures = 0;

    static void check(boolean ok, String what) {
        System.out.println(ok ? "ok   " + what : "FAILED " + what);
        if (!ok) { failures++; }
    }

    // THE WIRE ID IS fnv1a64 OF THE WIRE NAME (docs/FIXED-FORM-ALGORITHM.md
    // §1, "id is fnv1a64(wire name)", and §5: `was` keeps the old name's id).
    // Derived here, not read out of the generated code, so a generated
    // constant that silently drifted still fails this test.
    static long fnv1a64(String s) {
        long h = 0xcbf29ce484222325L;
        for (byte b : s.getBytes(java.nio.charset.StandardCharsets.UTF_8)) {
            h ^= (b & 0xff);
            h *= 0x100000001b3L;
        }
        return h;
    }

    static int countIds(tblv1.TableFixed.Layout l, long id) {
        int n = 0;
        for (int i = 0; i < l.count; i++) { if (l.id(i) == id) { n++; } }
        return n;
    }

    static int countIds2(tblv2.TableFixed.Layout l, long id) {
        int n = 0;
        for (int i = 0; i < l.count; i++) { if (l.id(i) == id) { n++; } }
        return n;
    }

    public static void main(String[] args) {
        final long idName = fnv1a64("name");
        final long idTitle = fnv1a64("title");
        check(idName != idTitle, "the two spellings hash to two ids (sanity)");

        // THE DECLARED IDENTITY, in the generated layouts the readers hold.
        final tblv1.TableFixed.Report r1 = new tblv1.TableFixed.Report();
        final tblv1.TableFixed.Layout l1 = tblv1.TableFixed.parse(
                tblv1.CfgFixed.layout, 0, tblv1.CfgFixed.layout.length, r1);
        check(l1 != null, "v1: the generated layout parses");
        check(countIds(l1, idName) == 1, "v1: `name` carries the wire id fnv1a64(\"name\")");

        final tblv2.TableFixed.Report r2 = new tblv2.TableFixed.Report();
        final tblv2.TableFixed.Layout l2 = tblv2.TableFixed.parse(
                tblv2.CfgFixed.layout, 0, tblv2.CfgFixed.layout.length, r2);
        check(l2 != null, "v2: the generated layout parses");
        check(countIds2(l2, idName) == 1,
                "v2: `title` (was = \"name\") carries the DECLARED identity fnv1a64(\"name\")");
        check(countIds2(l2, idTitle) == 0,
                "v2: no field carries fnv1a64(\"title\") — a bare rename would have");

        // THE READ PATH, both generations, through the production entrypoint.
        // V1 writes `name` and reads it back; V2 writes `title` (the same
        // logical field, spelled with the NEW name) and reads it back.
        final tblv1.CfgFixed.Value one = new tblv1.CfgFixed.Value();
        setText(one.name, "hello");
        one.nameLength = 5;
        final byte[] v1wire = new byte[tblv1.CfgFixed.measure(1)];
        check(tblv1.CfgFixed.save(new tblv1.CfgFixed.Value[] { one }, 1, v1wire) == v1wire.length,
                "v1: the writer emits a record");
        final tblv1.CfgFixed.Value[] back1 = { new tblv1.CfgFixed.Value() };
        final tblv1.TableFixed.Report rr1 = new tblv1.TableFixed.Report();
        final int n1 = tblv1.CfgFixed.load(back1, 1, v1wire,
                tblv1.TableFixed.plan(8192), new short[8192], tblv1.CfgFixed.image(), rr1);
        check(n1 == 1 && !rr1.refused && textOf(back1[0].name, back1[0].nameLength).equals("hello"),
                "v1: `name` reads back through the production load path");

        final tblv2.CfgFixed.Value two = new tblv2.CfgFixed.Value();
        setText(two.title, "hello");
        two.titleLength = 5;
        final byte[] v2wire = new byte[tblv2.CfgFixed.measure(1)];
        check(tblv2.CfgFixed.save(new tblv2.CfgFixed.Value[] { two }, 1, v2wire) == v2wire.length,
                "v2: the writer emits a record");
        final tblv2.CfgFixed.Value[] back2 = { new tblv2.CfgFixed.Value() };
        final tblv2.TableFixed.Report rr2 = new tblv2.TableFixed.Report();
        final int n2 = tblv2.CfgFixed.load(back2, 1, v2wire,
                tblv2.TableFixed.plan(8192), new short[8192], tblv2.CfgFixed.image(), rr2);
        check(n2 == 1 && !rr2.refused && textOf(back2[0].title, back2[0].titleLength).equals("hello"),
                "v2: `title` reads back through the production load path");

        if (failures != 0) {
            System.out.println(failures + " failure(s)");
            System.exit(1);
        }
        System.out.println("E6 green: renaming uses the declared identity on the Java leg");
    }

    static void setText(byte[] buf, String s) {
        final byte[] raw = s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(raw, 0, buf, 0, raw.length);
    }

    static String textOf(byte[] buf, int length) {
        return new String(buf, 0, length, java.nio.charset.StandardCharsets.UTF_8);
    }
}
