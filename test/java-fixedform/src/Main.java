// THE JAVA LEG OF THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3.
//
// It proves two things and they are different things:
//
//   THE WRITE   the C++ REFERENCE's own byte oracle — build/fixedform-corpus,
//               written by test/tables/fixedform_dump.cpp — is read back and
//               saved again, and the bytes must be IDENTICAL. That reaches
//               every field of every shape in the corpus: a byte this port
//               encodes differently is a byte that does not come back.
//   THE READ    a record written under ANOTHER schema's layout is read through
//               a plan compiled from that layout, which is the whole of what
//               §3.4's versioning invariant is worth — "we must not ever break
//               versioning in fixed tables", the project owner.
//
// And then the NEGATIVE CONTROLS, because a validation nobody watched fail is
// a validation nobody has: the wrong plan, a corrupted layout under each of
// §3.4's seven named rules, a form byte this reader does not carry, a plan
// that does not fit, a ragged tail, and a batch past the caller's capacity.

import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class Main {
    private Main() {}

    private static int failures = 0;

    static void check(boolean ok, String what) {
        if (!ok) {
            System.out.println("FAILED: " + what);
            failures++;
        }
    }

    static byte[] slurp(String dir, String name) {
        try {
            return Files.readAllBytes(Paths.get(dir, name));
        } catch (java.io.IOException e) {
            System.out.println("FAILED: cannot read " + dir + "/" + name + ": " + e);
            failures++;
            return new byte[0];
        }
    }

    static byte[] bytesOf(String s) {
        return s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
    }

    static void setText(byte[] buf, String s) {
        final byte[] raw = bytesOf(s);
        System.arraycopy(raw, 0, buf, 0, raw.length);
    }

    static String textOf(byte[] buf, int length) {
        return new String(buf, 0, length, java.nio.charset.StandardCharsets.UTF_8);
    }

    // ------------------------------------------------------------------
    // THE WRITE: the six oracle files, byte for byte
    // ------------------------------------------------------------------

    static void fx1Write(String dir) {
        final byte[] golden = slurp(dir, "fx1.bin");
        final tblfx1.FxRootFixed.Value[] v = new tblfx1.FxRootFixed.Value[8];
        for (int i = 0; i < v.length; i++) { v[i] = new tblfx1.FxRootFixed.Value(); }
        final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
        final int n = tblfx1.FxRootFixed.load(v, v.length, golden,
                tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
        check(n == 2, "fx1.bin: two records");
        check(!r.refused && !r.malformed && r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0,
                "fx1.bin: its own layout is the identity plan, and a clean read moves no counter");
        check(v[0].keep == 4242 && (v[0].narrow & 0xffff) == 40000 && v[0].renamed == 321 && v[0].gone == 654,
                "fx1.bin: the scalars are the reference's");
        check(v[0].nested.a == 111 && v[0].nested.b == 222, "fx1.bin: the nesting");
        final byte[] back = new byte[tblfx1.FxRootFixed.measure(n)];
        check(tblfx1.FxRootFixed.save(v, n, back) == back.length, "fx1.bin: save fills what measure says");
        check(Arrays.equals(back, golden), "fx1.bin: the bytes are the C++ reference's, exactly");
    }

    static void fx2Write(String dir) {
        final byte[] golden = slurp(dir, "fx2.bin");
        final tblfx2.FxRootFixed.Value[] v = new tblfx2.FxRootFixed.Value[8];
        for (int i = 0; i < v.length; i++) { v[i] = new tblfx2.FxRootFixed.Value(); }
        final tblfx2.TableFixed.Report r = new tblfx2.TableFixed.Report();
        final int n = tblfx2.FxRootFixed.load(v, v.length, golden,
                tblfx2.TableFixed.plan(1024), new short[1024], tblfx2.FxRootFixed.image(), r);
        check(n == 1, "fx2.bin: one record");
        check(!r.refused && !r.malformed && r.unknown == 0 && r.kindMismatch == 0,
                "fx2.bin: a clean read moves no counter");
        check(v[0].keep == 5150 && v[0].narrow == 70000 && v[0].renamedTo == 808 && v[0].added == 909,
                "fx2.bin: the scalars are the reference's");
        check(v[0].extra.x == 55 && v[0].extra.y == 66, "fx2.bin: the nested type FX1 has no name for");
        final byte[] back = new byte[tblfx2.FxRootFixed.measure(n)];
        check(tblfx2.FxRootFixed.save(v, n, back) == back.length, "fx2.bin: save fills what measure says");
        check(Arrays.equals(back, golden), "fx2.bin: the bytes are the C++ reference's, exactly");
    }

    static void p1Write(String dir) {
        final byte[] golden = slurp(dir, "p1.bin");
        final tblp1.ChainFixed.Value[] v = new tblp1.ChainFixed.Value[4];
        for (int i = 0; i < v.length; i++) { v[i] = new tblp1.ChainFixed.Value(); }
        final tblp1.TableFixed.Report r = new tblp1.TableFixed.Report();
        final int n = tblp1.ChainFixed.load(v, v.length, golden,
                tblp1.TableFixed.plan(1024), new short[1024], tblp1.ChainFixed.image(), r);
        check(n == 1, "p1.bin: one record");
        check(!r.refused && !r.malformed && r.clamped == 0, "p1.bin: a clean read moves no counter");
        check(textOf(v[0].name, v[0].nameLength).equals("chain-one"), "p1.bin: the text lands at its used length");
        check(v[0].link.value == 77 && textOf(v[0].link.tag, v[0].link.tagLength).equals("tagged"),
                "p1.bin: the nested record");
        final byte[] back = new byte[tblp1.ChainFixed.measure(n)];
        check(tblp1.ChainFixed.save(v, n, back) == back.length, "p1.bin: save fills what measure says");
        check(Arrays.equals(back, golden), "p1.bin: the bytes are the C++ reference's, exactly");
    }

    static void p3Write(String dir) {
        final byte[] golden = slurp(dir, "p3.bin");
        final tblp3.ChainFixed.Value[] v = new tblp3.ChainFixed.Value[4];
        for (int i = 0; i < v.length; i++) { v[i] = new tblp3.ChainFixed.Value(); }
        final tblp3.TableFixed.Report r = new tblp3.TableFixed.Report();
        final int n = tblp3.ChainFixed.load(v, v.length, golden,
                tblp3.TableFixed.plan(1024), new short[1024], tblp3.ChainFixed.image(), r);
        check(n == 2, "p3.bin: two records");
        check(v[0].linkPresent && v[0].link.value == 88, "p3.bin: an optional that is PRESENT");
        check(!v[1].linkPresent, "p3.bin: an optional that is ABSENT");
        // THE PAYLOAD RIDES WHOLE WHETHER OR NOT IT IS PRESENT (§3.4), which is
        // exactly why `?T` and a plain `T` are ONE BYTE APART on this form.
        check(v[1].link.value == 99 && textOf(v[1].link.tag, v[1].link.tagLength).equals("still"),
                "p3.bin: an ABSENT optional's payload rides WHOLE all the same (§3.4)");
        final byte[] back = new byte[tblp3.ChainFixed.measure(n)];
        check(tblp3.ChainFixed.save(v, n, back) == back.length, "p3.bin: save fills what measure says");
        check(Arrays.equals(back, golden), "p3.bin: the bytes are the C++ reference's, exactly");
    }

    static void keyedWrite(String dir) {
        final byte[] golden = slurp(dir, "keyed.bin");
        final tabledemo.KeyedConfigFixed.Value[] v = new tabledemo.KeyedConfigFixed.Value[4];
        for (int i = 0; i < v.length; i++) { v[i] = new tabledemo.KeyedConfigFixed.Value(); }
        final tabledemo.TableFixed.Report r = new tabledemo.TableFixed.Report();
        final int n = tabledemo.KeyedConfigFixed.load(v, v.length, golden,
                tabledemo.TableFixed.plan(4096), new short[4096], tabledemo.KeyedConfigFixed.image(), r);
        check(n == 2, "keyed.bin: two records");
        check(!r.refused && !r.malformed && r.clamped == 0, "keyed.bin: a clean read moves no counter");
        // record 1: k = 1
        check(v[1].teams[0].spawnCount == 14, "keyed: a keyed array's slot");
        check(textOf(v[1].teams[2].banner, v[1].teams[2].bannerLength).equals("green"), "keyed: text in a keyed slot");
        check(v[1].scores.perTeam[1] == 2001, "keyed: a `type` nested by value");
        check(v[1].hulls[2].turrets[0].damage == 16.0f, "keyed: a keyed array nested in a keyed array");
        check(v[1].hulls[0].turrets[0].gunnerPresent, "keyed: an optional section that is PRESENT");
        check(!v[1].hulls[0].turrets[1].gunnerPresent, "keyed: an optional section that is ABSENT");
        check(v[1].hulls[0].turrets[1].gunner.reaction == 0.3f,
                "keyed: an ABSENT optional's payload rides WHOLE all the same (§3.4)");
        final byte[] back = new byte[tabledemo.KeyedConfigFixed.measure(n)];
        check(tabledemo.KeyedConfigFixed.save(v, n, back) == back.length, "keyed.bin: save fills what measure says");
        check(Arrays.equals(back, golden), "keyed.bin: the bytes are the C++ reference's, exactly");
    }

    static void packWrite(String dir) {
        final byte[] golden = slurp(dir, "pack.bin");
        final tabledemo.PackConfigFixed.Value[] v = new tabledemo.PackConfigFixed.Value[4];
        for (int i = 0; i < v.length; i++) { v[i] = new tabledemo.PackConfigFixed.Value(); }
        final tabledemo.TableFixed.Report r = new tabledemo.TableFixed.Report();
        final int n = tabledemo.PackConfigFixed.load(v, v.length, golden,
                tabledemo.TableFixed.plan(4096), new short[4096], tabledemo.PackConfigFixed.image(), r);
        check(n == 2, "pack.bin: two records");
        check(!r.refused && !r.malformed && r.clamped == 0, "pack.bin: a clean read moves no counter");
        check(v[0].version == 7 && v[0].global.tickRate == 120, "pack: the scalars");
        // AN ENUM RIDES ITS ORDINAL, FROM 1: Hard is the third variant.
        check(v[0].global.difficulty == tabledemo.Pack.Difficulty.hard, "pack: an enum rides its ORDINAL, from 1");
        check(textOf(v[0].global.buildNote, v[0].global.buildNoteLength).equals("first build"), "pack: text at its length");
        check(v[0].global.spawnDelays[2] == 1.5f, "pack: a fixed array of floats");
        check(v[0].ships[1].hardpointsCount == 2, "pack: a COUNTED array's used count");
        check(v[0].ships[1].hardpoints[1] == 2, "pack: a counted array's element");
        check(v[0].reservesCount == 2, "pack: a counted array OF TABLES");
        check(textOf(v[0].reserves[1].displayName, v[0].reserves[1].displayNameLength).equals("spare-b"),
                "pack: a counted array of tables carries text");
        check(v[0].thresholds[2] == 300, "pack: a keyed array of scalars");
        final byte[] back = new byte[tabledemo.PackConfigFixed.measure(n)];
        check(tabledemo.PackConfigFixed.save(v, n, back) == back.length, "pack.bin: save fills what measure says");
        check(Arrays.equals(back, golden), "pack.bin: the bytes are the C++ reference's, exactly");
    }

    // ------------------------------------------------------------------
    // THE COMPILED PLAN OVER MY OWN LAYOUT
    // ------------------------------------------------------------------
    //
    // The cross-schema cases below never reach the count, text, ordinal and
    // keyed ops all at once, so this compiles a plan from a layout that IS
    // this reader's own and requires it to land exactly what the identity plan
    // lands. If the two ever disagree, one of the two reader paths is wrong —
    // and §3.4 says there is only one.

    static void compiledSelfPlan(String dir) {
        final byte[] golden = slurp(dir, "pack.bin");
        final tabledemo.TableFixed.Report r = new tabledemo.TableFixed.Report();
        final tabledemo.TableFixed.Header head = new tabledemo.TableFixed.Header();
        check(tabledemo.TableFixed.readHeader(golden, head, r), "self plan: the file's header reads");
        final tabledemo.TableFixed.Layout theirs =
                tabledemo.TableFixed.parse(golden, head.layoutAt, head.layoutLength, r);
        final tabledemo.TableFixed.Layout mine = tabledemo.TableFixed.parse(
                tabledemo.PackConfigFixed.layout, 0, tabledemo.PackConfigFixed.layout.length, r);
        check(theirs != null && mine != null, "self plan: the layout parses on both sides");
        if (theirs == null || mine == null) { return; }
        final tabledemo.TableFixed.Entry[] plan = tabledemo.TableFixed.plan(4096);
        final short[] remap = new short[4096];
        final int made = tabledemo.TableFixed.compile(theirs, mine, tabledemo.PackConfigFixed.dest, plan, remap, r);
        check(made > 0, "self plan: a plan compiles from my own layout");
        check(r.unknown == 0 && r.kindMismatch == 0, "self plan: a plan over MY OWN layout names nothing unknown");
        // A REAL PLAN, not one run: the text, the counts, the ordinals and the
        // keyed walk all break the coalescer's runs.
        check(made > 1, "self plan: text, count and the keyed walk break the runs");

        final byte[] image = tabledemo.PackConfigFixed.image();
        final tabledemo.PackConfigFixed.Value viaPlan = new tabledemo.PackConfigFixed.Value();
        final int at = head.recordsAt;
        System.arraycopy(tabledemo.PackConfigFixed.defaults, 0, image, 0, tabledemo.PackConfigFixed.bodyBytes);
        tabledemo.TableFixed.run(plan, made, remap, golden, at + 8, tabledemo.PackConfigFixed.bodyBytes, image, r);
        tabledemo.PackConfigFixed.scatter(image, 0, viaPlan, r);

        final tabledemo.PackConfigFixed.Value[] viaIdentity = { new tabledemo.PackConfigFixed.Value() };
        final tabledemo.TableFixed.Report r2 = new tabledemo.TableFixed.Report();
        check(tabledemo.PackConfigFixed.load(viaIdentity, 1, Arrays.copyOfRange(golden, 0,
                        head.recordsAt + tabledemo.PackConfigFixed.recordBytes),
                tabledemo.TableFixed.plan(4096), new short[4096], tabledemo.PackConfigFixed.image(), r2) == 1,
                "self plan: the identity path reads the same record");
        // A Value has no equality of its own, so the two are compared by the
        // bytes they save back — which is the only comparison that reaches
        // every field anyway.
        final byte[] a = new byte[tabledemo.PackConfigFixed.measure(1)];
        final byte[] b = new byte[tabledemo.PackConfigFixed.measure(1)];
        tabledemo.PackConfigFixed.save(new tabledemo.PackConfigFixed.Value[] { viaPlan }, 1, a);
        tabledemo.PackConfigFixed.save(viaIdentity, 1, b);
        check(Arrays.equals(a, b), "self plan: a COMPILED plan lands exactly what the identity plan lands");
    }

    // ------------------------------------------------------------------
    // TEXT AND AN ENUM UNDER A UNION'S SECOND ARM (test/tables/UT.schema)
    // ------------------------------------------------------------------
    //
    // Every field inside a union arm rides a plan entry GUARDED on the
    // WRITER's own tag, and the guard compares that entry's ARM ORDINAL. So an
    // op that borrows the same byte for something of its own — a text entry's
    // flavour — loses every text field under an arm past the FIRST: the guard
    // then compares a flavour against a tag, the entry never fires, the field
    // never lands and no counter moves. No corpus root carries text under a
    // second arm, so the compiled path was never asked this question. This
    // fixture asks it, and the only admissible answer is the identity path's.
    //
    // The enum beside it is the OTHER half, tonight's ruling: an ordinal past
    // the last variant lands None (0) and counts one `clamped`, and a TAG past
    // the last arm does the same.

    static byte[] utRecord(String label, byte grade, int m, int tail) {
        final tblut.UtRootFixed.Value[] v = { new tblut.UtRootFixed.Value() };
        v[0].pick.type = tblut.UtPickFixed.b;
        setText(v[0].pick.b.label, label);
        v[0].pick.b.labelLength = bytesOf(label).length;
        v[0].pick.b.grade = grade;
        v[0].pick.b.m = m;
        v[0].tail = tail;
        final byte[] wire = new byte[tblut.UtRootFixed.measure(1)];
        check(tblut.UtRootFixed.save(v, 1, wire) == wire.length, "arm text: save fills what measure says");
        return wire;
    }

    /** one record read through a plan COMPILED from a layout that is my own. */
    static tblut.UtRootFixed.Value utCompiled(byte[] wire, tblut.TableFixed.Report r) {
        final tblut.UtRootFixed.Value out = new tblut.UtRootFixed.Value();
        final tblut.TableFixed.Header head = new tblut.TableFixed.Header();
        check(tblut.TableFixed.readHeader(wire, head, r), "arm text: the file's header reads");
        final tblut.TableFixed.Layout theirs =
                tblut.TableFixed.parse(wire, head.layoutAt, head.layoutLength, r);
        final tblut.TableFixed.Layout mine = tblut.TableFixed.parse(
                tblut.UtRootFixed.layout, 0, tblut.UtRootFixed.layout.length, r);
        check(theirs != null && mine != null, "arm text: the layout parses on both sides");
        if (theirs == null || mine == null) { return out; }
        final tblut.TableFixed.Entry[] plan = tblut.TableFixed.plan(512);
        final short[] remap = new short[512];
        final int made = tblut.TableFixed.compile(theirs, mine, tblut.UtRootFixed.dest, plan, remap, r);
        check(made > 0, "arm text: a plan compiles from my own layout");
        final byte[] image = tblut.UtRootFixed.image();
        System.arraycopy(tblut.UtRootFixed.defaults, 0, image, 0, tblut.UtRootFixed.bodyBytes);
        tblut.TableFixed.run(plan, made, remap, wire, head.recordsAt + 8, (int) theirs.size(0), image, r);
        tblut.UtRootFixed.scatter(image, 0, out, r);
        return out;
    }

    /** two values compared by the bytes they save back, which is the only
     *  comparison that reaches every field. */
    static boolean utSame(tblut.UtRootFixed.Value x, tblut.UtRootFixed.Value y) {
        final byte[] a = new byte[tblut.UtRootFixed.measure(1)];
        final byte[] b = new byte[tblut.UtRootFixed.measure(1)];
        tblut.UtRootFixed.save(new tblut.UtRootFixed.Value[] { x }, 1, a);
        tblut.UtRootFixed.save(new tblut.UtRootFixed.Value[] { y }, 1, b);
        return Arrays.equals(a, b);
    }

    /** the offset of one record's BODY inside the file. */
    static int utBodyAt(byte[] wire) {
        final tblut.TableFixed.Header head = new tblut.TableFixed.Header();
        final tblut.TableFixed.Report r = new tblut.TableFixed.Report();
        check(tblut.TableFixed.readHeader(wire, head, r), "arm text: the header reads for the plant");
        return head.recordsAt + 8;
    }

    static void armTextUnderASecondArm() {
        final byte[] wire = utRecord("hello", tblut.UT.UtGrade.gold, 42, 11);

        // THE IDENTITY PATH
        final tblut.UtRootFixed.Value[] id = { new tblut.UtRootFixed.Value() };
        final tblut.TableFixed.Report r = new tblut.TableFixed.Report();
        check(tblut.UtRootFixed.load(id, 1, wire, tblut.TableFixed.plan(512), new short[512],
                tblut.UtRootFixed.image(), r) == 1, "arm text: the identity path reads the record");
        check(!r.refused && !r.malformed && r.clamped == 0 && r.unknown == 0 && r.kindMismatch == 0,
                "arm text: a clean read moves no counter");
        check(id[0].pick.type == tblut.UtPickFixed.b, "arm text: the SECOND arm is the selected one");
        check(textOf(id[0].pick.b.label, id[0].pick.b.labelLength).equals("hello"),
                "arm text: the identity path lands the arm's text");

        // THE COMPILED PATH, and it must land the same record
        final tblut.TableFixed.Report rc = new tblut.TableFixed.Report();
        final tblut.UtRootFixed.Value viaPlan = utCompiled(wire, rc);
        check(textOf(viaPlan.pick.b.label, viaPlan.pick.b.labelLength).equals("hello"),
                "arm text: a COMPILED plan lands a text field under the SECOND arm");
        check(viaPlan.pick.b.grade == tblut.UT.UtGrade.gold && viaPlan.pick.b.m == 42 && viaPlan.tail == 11,
                "arm text: and the rest of that arm with it");
        check(rc.clamped == 0 && !rc.malformed && !rc.refused,
                "arm text: the compiled read moves no counter either");
        check(utSame(viaPlan, id[0]),
                "arm text: a COMPILED plan lands exactly what the identity plan lands");

        // A TAG PAST THE LAST ARM: None on both paths, and the identity path
        // counts one clamped. THE COMPILED PLAN COUNTS NOTHING HERE and that
        // is not an oversight: a plan says what a SELECTED arm does, so
        // "no arm fired" is not an event any entry of it can see. The tag
        // still lands None, because the prefill put None there and no entry
        // wrote over it.
        {
            final byte[] bad = wire.clone();
            final int body = utBodyAt(bad);
            check(bad[body] == 2, "tag past the last arm: the tag sits where the layout says");
            bad[body] = 7;

            final tblut.UtRootFixed.Value[] v = { new tblut.UtRootFixed.Value() };
            final tblut.TableFixed.Report r1 = new tblut.TableFixed.Report();
            check(tblut.UtRootFixed.load(v, 1, bad, tblut.TableFixed.plan(512), new short[512],
                    tblut.UtRootFixed.image(), r1) == 1, "tag past the last arm: the record still reads");
            check(v[0].pick.type == tblut.UtPickFixed.none, "tag past the last arm: the union lands None");
            check(r1.clamped == 1, "tag past the last arm: the identity path counts ONE clamped");
            check(!r1.malformed && !r1.refused, "tag past the last arm: damage in a VALUE is not framing damage");

            final tblut.TableFixed.Report r2 = new tblut.TableFixed.Report();
            final tblut.UtRootFixed.Value bent = utCompiled(bad, r2);
            check(bent.pick.type == tblut.UtPickFixed.none, "tag past the last arm: the compiled plan lands None too");
            check(r2.clamped == 0, "tag past the last arm: no entry of a plan can see an arm that did not fire");
        }

        // AN ORDINAL PAST THE LAST VARIANT: None (0) on both paths, and BOTH
        // count it — the identity scatter on its own read, and the plan's
        // ordinal op on the compiled one.
        {
            final byte[] bad = wire.clone();
            final int body = utBodyAt(bad);
            final int at = body + 13; // the tag, then the arm's text, then the ordinal
            check(bad[at] == tblut.UT.UtGrade.gold, "ordinal past the last variant: the ordinal is where the layout says");
            bad[at] = 9;

            final tblut.UtRootFixed.Value[] v = { new tblut.UtRootFixed.Value() };
            final tblut.TableFixed.Report r1 = new tblut.TableFixed.Report();
            check(tblut.UtRootFixed.load(v, 1, bad, tblut.TableFixed.plan(512), new short[512],
                    tblut.UtRootFixed.image(), r1) == 1, "ordinal past the last variant: the record still reads");
            check(v[0].pick.b.grade == tblut.UT.UtGrade.none, "ordinal past the last variant: it lands None");
            check(r1.clamped == 1, "ordinal past the last variant: the identity path counts ONE clamped");

            final tblut.TableFixed.Report r2 = new tblut.TableFixed.Report();
            final tblut.UtRootFixed.Value bent = utCompiled(bad, r2);
            check(bent.pick.b.grade == tblut.UT.UtGrade.none,
                    "ordinal past the last variant: the compiled plan lands None too");
            check(r2.clamped == 1, "ordinal past the last variant: and the plan's ordinal op counts it");
            check(utSame(bent, v[0]), "ordinal past the last variant: the two paths land ONE record");
        }
    }

    // ------------------------------------------------------------------
    // THE VERSIONING CONFORMANCE
    // ------------------------------------------------------------------

    static void anOlderWriter(String dir) {
        // FX2 reads FX1's records: `narrow` WIDENS uint16 -> uint32, `renamed`
        // arrives under `was =`, `added` is a field the writer does not carry
        // and `gone` is one this reader cannot name.
        final byte[] golden = slurp(dir, "fx1.bin");
        final tblfx2.FxRootFixed.Value[] v = new tblfx2.FxRootFixed.Value[8];
        for (int i = 0; i < v.length; i++) { v[i] = new tblfx2.FxRootFixed.Value(); }
        final tblfx2.TableFixed.Report r = new tblfx2.TableFixed.Report();
        final int n = tblfx2.FxRootFixed.load(v, v.length, golden,
                tblfx2.TableFixed.plan(1024), new short[1024], tblfx2.FxRootFixed.image(), r);
        check(n == 2, "older writer: both records");
        check(v[0].keep == 4242, "older writer: an unmoved field");
        check(v[0].narrow == 40000, "WIDENED: uint16 into uint32, exactly");
        check(r.widened == 2, "WIDENED: one widened counts, per record");
        check(v[0].renamedTo == 321, "RENAMED: `was =` keeps the wire id");
        check(v[0].added == 11, "MISSING: a field the writer does not carry takes its declared default");
        check(v[0].extra.x == 0 && v[0].extra.y == 0, "MISSING: a whole nested type takes its defaults");
        check(v[0].nested.a == 111 && v[0].nested.b == 222, "older writer: the nesting");
        check(r.unknown == 1, "older writer: `gone` is the one field this reader cannot name");
        check(r.kindMismatch == 0 && !r.malformed && !r.refused, "older writer: nothing else fired");
        check(v[1].keep == 1 && v[1].narrow == 2 && v[1].renamedTo == 3 && v[1].nested.b == 6,
                "older writer: the second record too");
    }

    static void aNewerWriter(String dir) {
        // FX1 reads FX2's record: `added` and `extra` are names it cannot
        // name, and `narrow` came back DOWN the ladder, which is a kind that
        // MOVED and never a narrowing decode.
        final byte[] golden = slurp(dir, "fx2.bin");
        final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value() };
        final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
        final int n = tblfx1.FxRootFixed.load(v, 1, golden,
                tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
        check(n == 1, "newer writer: one record");
        check(v[0].keep == 5150, "newer writer: an unmoved field lands past the unknowns");
        check(v[0].renamed == 808, "newer writer: `was =` reads the other way too");
        check(v[0].gone == 9, "newer writer: a field the writer dropped takes its declared default");
        check(v[0].nested.a == 33 && v[0].nested.b == 44, "newer writer: the nesting lands past the unknown type");
        check(r.unknown == 2, "newer writer: two names this reader does not have");
        check(r.kindMismatch == 1, "newer writer: uint32 into uint16 is a kind that MOVED, not a widening");
        check(v[0].narrow == 3, "newer writer: a narrowing leaves the declared default");
        check(!r.malformed && !r.refused, "newer writer: no damage and no refusal");
    }

    static void anOptional(String dir) {
        // P1 nests Link BY VALUE and P3 marks the same field `?Link`. On FORM 1
        // those two are wire-identical (§2.3). ON THIS FORM THEY ARE ONE BYTE
        // APART, so the edit is REPORTED rather than silent — the departure
        // §3.4 states rather than leaves to be discovered.
        final byte[] golden = slurp(dir, "p1.bin");
        final tblp3.ChainFixed.Value[] v = { new tblp3.ChainFixed.Value() };
        final tblp3.TableFixed.Report r = new tblp3.TableFixed.Report();
        final int n = tblp3.ChainFixed.load(v, 1, golden,
                tblp3.TableFixed.plan(1024), new short[1024], tblp3.ChainFixed.image(), r);
        check(n == 1, "optional: P3 reads P1's record");
        check(textOf(v[0].name, v[0].nameLength).equals("chain-one"), "optional: the field before the edit lands");
        check(r.kindMismatch == 1, "optional: `?T` against `T` is a kind that MOVED and is SEEN");
        check(!v[0].linkPresent, "optional: the edited field takes its declared default");
        check(!r.malformed && !r.refused, "optional: no damage and no refusal");
    }

    static void theSlide() {
        // AN ENUM VARIANT, A UNION ARM AND A KEYED SLOT INSERTED IN THE MIDDLE.
        // Under a positional encoding every stored Gold would read back as
        // Silver and every Beta as Omega. They ride by NAME, so they do not.
        final tblv1.CfgFixed.Value one = new tblv1.CfgFixed.Value();
        one.a = 42;
        setText(one.name, "hello");
        one.nameLength = 5;
        one.grade = tblv1.V1.Grade.gold;              // V2 inserts Silver BEFORE Gold
        one.effect.type = tblv1.EffectFixed.ward;
        one.effect.ward.charge = 0.75f;         // V2 inserts hex BEFORE ward
        one.tokens[tblv1.V1.Slot.alpha - 1] = 21;
        one.tokens[tblv1.V1.Slot.delta - 1] = 24;     // V2 slides Beta and keeps Delta
        one.tierPresent = true;
        one.tier = 77;

        final byte[] w = new byte[tblv1.CfgFixed.measure(1)];
        check(tblv1.CfgFixed.save(new tblv1.CfgFixed.Value[] { one }, 1, w) == w.length, "V1 save");

        final tblv2.CfgFixed.Value[] back = { new tblv2.CfgFixed.Value() };
        final tblv2.TableFixed.Report r = new tblv2.TableFixed.Report();
        final int n = tblv2.CfgFixed.load(back, 1, w,
                tblv2.TableFixed.plan(8192), new short[8192], tblv2.CfgFixed.image(), r);
        check(n == 1, "V2 reads V1: one record");
        check(back[0].grade == tblv2.V2.Grade.gold, "ENUM: a variant inserted in the middle is remapped by NAME");
        check(back[0].effect.type == tblv2.EffectFixed.ward, "UNION: an arm inserted in the middle is remapped by NAME");
        check(back[0].effect.ward.charge == 0.75f, "UNION: the arm's payload lands");
        check(textOf(back[0].title, back[0].titleLength).equals("hello"), "RENAMED: title reads name's bytes");
        check(back[0].tokens[tblv2.V2.Slot.alpha - 1] == 21, "KEYED: a slot whose key did not move");
        check(back[0].tokens[tblv2.V2.Slot.delta - 1] == 24, "KEYED: a slot whose key SLID keeps its value");
        check(back[0].tokens[tblv2.V2.Slot.sigma - 1] == 0, "KEYED: a key the writer has no name for takes its default");
        check(back[0].tierPresent && back[0].tier == 77, "OPTIONAL: the present flag and the payload");
        check(back[0].c, "MISSING: V2's own `c` takes its declared default");
        check(back[0].a == 5.0f, "KIND MOVED: int32 -> float32 leaves the declared default");
        check(!r.malformed && !r.refused, "V2 reads V1: no damage and no refusal");
    }

    // ------------------------------------------------------------------
    // THE NEGATIVE CONTROLS
    // ------------------------------------------------------------------

    static void checkForm(byte[] file, byte formByte, tblfx1.TableFixed.Reason want, String what) {
        final byte[] other = file.clone();
        other[0] = formByte;
        final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value() };
        final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
        final int n = tblfx1.FxRootFixed.load(v, 1, other,
                tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
        check(n < 0 && r.refused && r.reason == want, what + " (got " + r.reason + ")");
        check(!r.malformed, what + ": never damage");
    }

    static void negativeControls(String dir) {
        final byte[] fx2 = slurp(dir, "fx2.bin");
        final byte[] fx1 = slurp(dir, "fx1.bin");

        // 1. THE WRONG PLAN. FX1's identity plan is correct for an FX1 record
        //    and wrong for an FX2 one — FX2 inserts `added` between
        //    `renamed_to` and `nested`, so every offset past it has moved.
        //    Running it over an FX2 body must come out WRONG. If it ever comes
        //    out right, the hash is not carrying anything.
        {
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final tblfx1.TableFixed.Header head = new tblfx1.TableFixed.Header();
            tblfx1.TableFixed.readHeader(fx2, head, r);
            final byte[] image = tblfx1.FxRootFixed.image();
            System.arraycopy(tblfx1.FxRootFixed.defaults, 0, image, 0, tblfx1.FxRootFixed.bodyBytes);
            tblfx1.TableFixed.run(tblfx1.FxRootFixed.identityPlan(), 1, new short[4],
                    fx2, head.recordsAt + 8, tblfx1.FxRootFixed.bodyBytes, image, r);
            final tblfx1.FxRootFixed.Value wrong = new tblfx1.FxRootFixed.Value();
            tblfx1.FxRootFixed.scatter(image, 0, wrong, r);
            final boolean intact = wrong.nested.a == 33 && wrong.nested.b == 44 && wrong.renamed == 808;
            check(!intact, "NEGATIVE CONTROL: FX1's identity plan over an FX2 record must NOT reproduce it");
        }

        // and the loader never takes that path: the HASH is what selects the plan
        {
            final tblfx1.FxRootFixed.Value[] right = { new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final int n = tblfx1.FxRootFixed.load(right, 1, fx2,
                    tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
            check(n == 1 && right[0].nested.a == 33 && right[0].nested.b == 44,
                    "NEGATIVE CONTROL: the loader compiles a plan from the layout and gets it right");
        }

        // 2. A FORM BYTE THIS READER DOES NOT CARRY IS A REFUSAL AND NEVER
        //    DAMAGE — AND THE REFUSAL SAYS WHICH DIRECTION (§3). The registry
        //    is ordered, so one word for both directions is one word too few:
        //    the variable form is OLDER, a batch handed to a file root is
        //    somewhere else entirely, and only a byte no form defines is newer.
        //    6 is the first byte that is neither defined nor reserved.
        {
            checkForm(fx2, (byte) 6, tblfx1.TableFixed.Reason.newerForm,
                    "REFUSED BY NAME: newer_form for a byte no form defines");
            checkForm(fx2, (byte) 1, tblfx1.TableFixed.Reason.previousForm,
                    "REFUSED BY NAME: previous_form for the VARIABLE form, which is older");
            checkForm(fx2, (byte) 2, tblfx1.TableFixed.Reason.messageFormAsFile,
                    "REFUSED BY NAME: message_form_as_file for a batch handed to a file root");
        }

        // 2b. A HEADER WHOSE HASH IS NOT THE HASH OF THE LAYOUT BEHIND IT is
        //     refused (§3), and it is checked LAST so a broken layout is never
        //     reported as a lying header.
        {
            final byte[] lying = fx2.clone();
            lying[tblfx1.TableFixed.hashAt] ^= (byte) 0xFF;
            final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final int n = tblfx1.FxRootFixed.load(v, 1, lying,
                    tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
            check(n < 0 && r.refused && r.reason == tblfx1.TableFixed.Reason.layoutMalformed,
                    "REFUSED BY NAME: a header that names a layout it does not carry");
            check(!r.malformed, "a lying header is a refusal and never damage");
        }

        // 3. A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE IS A REFUSAL BY
        //    NAME, and the codec allocates nothing to get around it.
        {
            final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final int n = tblfx1.FxRootFixed.load(v, 1, fx2,
                    tblfx1.TableFixed.plan(1), new short[4], tblfx1.FxRootFixed.image(), r);
            check(n < 0 && r.refused && r.reason == tblfx1.TableFixed.Reason.planTooLarge,
                    "REFUSED BY NAME: plan_too_large");
        }

        // 4. A RAGGED TAIL: the two ends of the file have met, which is framing
        //    damage and not a refusal.
        {
            final byte[] ragged = Arrays.copyOf(fx1, fx1.length + 1);
            final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value(), new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final int n = tblfx1.FxRootFixed.load(v, 2, ragged,
                    tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
            check(n < 0 && r.malformed, "a ragged tail: malformed, because the two ends of the file met");
        }

        // 5. MORE RECORDS THAN THE CALLER'S OWN ARRAY: a refusal by name, and
        //    nothing is written into an array that cannot hold it.
        {
            final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final int n = tblfx1.FxRootFixed.load(v, 1, fx1,
                    tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
            check(n < 0 && r.refused && r.reason == tblfx1.TableFixed.Reason.batchTooLarge,
                    "REFUSED BY NAME: batch_too_large");
        }
    }

    // ------------------------------------------------------------------
    // THE LAYOUT'S OWN VALIDATION, ONE CASE PER NAMED RULE
    // ------------------------------------------------------------------
    //
    // A LAYOUT ARRIVES FROM AN UNTRUSTED PEER (docs/SPEC-TABLES.md §3.4). Every
    // rule below runs BEFORE a single record byte is touched, each refuses
    // under ITS OWN NAME, and a layout that fails any of them SETS NOTHING.
    //
    // THE FILE HEADER IS §3's, ONE RULE FOR ALL FIVE FORMS: the form byte at 0,
    // seven reserved zero bytes, the LAYOUT HASH at 8, and the body at 16. So
    // the layout length is the four bytes at 16, the layout starts at 20, the
    // entry count is the four bytes there, and entry k is the seventeen bytes
    // at 20 + 4 + 17k.

    private static final int LAYOUT_AT = 20;
    private static final int ENTRY_0 = LAYOUT_AT + 4;

    static int entryAt(int k) { return ENTRY_0 + k * 17; }

    static void refuses(byte[] broken, tblfx1.TableFixed.Reason want, String what) {
        final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value() };
        final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
        final int n = tblfx1.FxRootFixed.load(v, 1, broken,
                tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
        check(n < 0 && r.refused && r.reason == want, what + " (got " + r.reason + ")");
        // NOTHING WAS DECODED AND NOTHING WAS COUNTED. A refusal that half-read
        // a record would be the damage the refusal exists to prevent.
        check(r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed,
                "a layout refusal sets nothing and counts nothing: " + what);
    }

    static void putEntry(java.io.ByteArrayOutputStream out, long id, int kind, int size, int children) {
        final byte[] e = new byte[17];
        tblfx1.TableFixed.put64(e, 0, id);
        e[8] = (byte) kind;
        tblfx1.TableFixed.put32(e, 9, size);
        tblfx1.TableFixed.put32(e, 13, children);
        out.write(e, 0, e.length);
    }

    // a FILE around a hand-built layout: §3's header with an HONEST hash, the
    // layout's length, the layout, and no records — every rule below refuses
    // before a record is reached, and the honest hash is what keeps each case
    // to the ONE break it makes.
    static byte[] fileOf(byte[] layout) {
        final byte[] out = new byte[20 + layout.length];
        out[0] = 3;
        tblfx1.TableFixed.put64(out, tblfx1.TableFixed.hashAt,
                tblfx1.TableFixed.hash(layout, 0, layout.length));
        tblfx1.TableFixed.put32(out, tblfx1.TableFixed.fileHeaderBytes, layout.length);
        System.arraycopy(layout, 0, out, 20, layout.length);
        return out;
    }

    static void layoutValidation(String dir) {
        // the layout this reader ACCEPTS, which every case below breaks once
        final byte[] good = slurp(dir, "fx2.bin");
        {
            final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            check(tblfx1.FxRootFixed.load(v, 1, good, tblfx1.TableFixed.plan(1024), new short[1024],
                    tblfx1.FxRootFixed.image(), r) == 1 && !r.refused,
                    "layout validation: the unbroken file reads, so the breaks below are the breaks");
        }

        // 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY
        {
            final byte[] f = good.clone();
            tblfx1.TableFixed.put32(f, LAYOUT_AT, tblfx1.TableFixed.get32(f, LAYOUT_AT) + 1);
            refuses(f, tblfx1.TableFixed.Reason.layoutCountMismatch,
                    "RULE: the entry count fits the layout length exactly");
        }
        {
            final byte[] f = good.clone();
            tblfx1.TableFixed.put32(f, LAYOUT_AT, 0);
            refuses(f, tblfx1.TableFixed.Reason.layoutCountMismatch,
                    "RULE: an entry count of zero is not a layout");
        }

        // 2. EVERY KIND IS IN THE CLOSED SET. A fixed form's kind set is
        //    CLOSED, so a kind outside it means a NEWER FORM BYTE — a different
        //    form — and not a newer layout of this one. It is REFUSED, never
        //    stepped over.
        {
            final byte[] f = good.clone();
            f[entryAt(1) + 8] = (byte) 200;
            refuses(f, tblfx1.TableFixed.Reason.layoutKindUnknown,
                    "RULE: a kind outside the closed set is REFUSED, not skipped");
        }

        // 3. A KIND IS USED AS ITS DEFINITION ALLOWS — here, the ROOT is a table
        {
            final byte[] f = good.clone();
            f[entryAt(0) + 8] = 14;
            refuses(f, tblfx1.TableFixed.Reason.layoutKindInvalid, "RULE: the root entry is a TABLE");
        }

        // 4. A CONSTANT SIZE MATCHES ITS KIND
        {
            final byte[] f = good.clone();
            tblfx1.TableFixed.put32(f, entryAt(1) + 9, 5); // a uint32 leaf in five bytes
            refuses(f, tblfx1.TableFixed.Reason.layoutSizeMismatch,
                    "RULE: a constant size its kind does not admit");
        }
        {
            final byte[] f = good.clone();
            tblfx1.TableFixed.put32(f, entryAt(0) + 9, tblfx1.TableFixed.get32(f, entryAt(0) + 9) + 4);
            refuses(f, tblfx1.TableFixed.Reason.layoutSizeMismatch,
                    "RULE: a table's size is the sum of its fields'");
        }

        // 5. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES
        {
            final byte[] f = good.clone();
            tblfx1.TableFixed.put32(f, entryAt(0) + 13, tblfx1.TableFixed.get32(f, entryAt(0) + 13) + 1);
            refuses(f, tblfx1.TableFixed.Reason.layoutTreeUnclosed, "RULE: the tree runs out of layout");
        }
        {
            // THE OTHER DIRECTION: a tree that closes EARLY leaves entries no
            // walk reaches. It takes a hand-built layout to reach, and that is
            // itself worth stating: dropping a child of a TABLE is caught one
            // rule sooner, by the size that no longer sums, so the only subtree
            // whose loss the size rule cannot see is one that contributes NO
            // size — an enum's variants, at kind 32 and size 0.
            final java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
            final byte[] count = new byte[4];
            tblfx1.TableFixed.put32(count, 0, 4);
            out.write(count, 0, 4);
            putEntry(out, 1, 13, 4, 1);  // a table of one field
            putEntry(out, 2, 30, 4, 0);  // an enum, its TWO variants unreached
            putEntry(out, 3, 32, 0, 0);
            putEntry(out, 4, 32, 0, 0);
            refuses(fileOf(out.toByteArray()), tblfx1.TableFixed.Reason.layoutTreeUnclosed,
                    "RULE: the layout outlasts the tree");
        }

        // 6. THE TOTAL RECORD SIZE IS WITHIN 65536 AND DOES NOT OVERFLOW
        {
            final byte[] f = good.clone();
            tblfx1.TableFixed.put32(f, entryAt(0) + 9, 65537);
            refuses(f, tblfx1.TableFixed.Reason.layoutRecordTooLarge, "RULE: a record size past 65536");
        }
        {
            // A SIZE THAT WOULD WRAP. The children's sizes are summed in 64
            // bits precisely so a u32 that overflows is CAUGHT rather than
            // wrapped into a small number that then agrees with a parent.
            final byte[] f = good.clone();
            tblfx1.TableFixed.put32(f, entryAt(1) + 9, 0xFFFFFFFF);
            refuses(f, tblfx1.TableFixed.Reason.layoutRecordTooLarge,
                    "RULE: a size that would overflow the sum");
        }

        // 7. NOTHING NESTED PAST THE READER'S WALK BOUND. A BOUND ON THE WALK
        //    AND NOT ON THE WIRE: the validation recurses, so a layout of a
        //    thousand entries each claiming one child would spend a reader's
        //    stack before any other rule could fire.
        {
            final int depth = 4096; // far past any reader's own bound
            final java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
            final byte[] count = new byte[4];
            tblfx1.TableFixed.put32(count, 0, depth + 1);
            out.write(count, 0, 4);
            for (int i = 0; i < depth; i++) {
                // a table, then optional wrappers all the way down
                putEntry(out, 1, i == 0 ? 13 : 35, depth - i, 1);
            }
            putEntry(out, 2, 1, 1, 0); // a bool at the bottom
            refuses(fileOf(out.toByteArray()), tblfx1.TableFixed.Reason.layoutTooDeep,
                    "RULE: a nesting depth past the walk's own bound");
        }

        // AND THE RESIDUE: bytes that are not a layout at all, which is the one
        // case the seven named rules never reach.
        {
            refuses(fileOf(new byte[2]), tblfx1.TableFixed.Reason.layoutMalformed,
                    "RULE: fewer bytes than a header is layout_malformed");
        }
    }

    public static void main(String[] args) {
        if (args.length != 1) {
            System.out.println("usage: Main <corpus-dir>");
            System.exit(2);
        }
        final String dir = args[0];
        fx1Write(dir);
        fx2Write(dir);
        p1Write(dir);
        p3Write(dir);
        keyedWrite(dir);
        packWrite(dir);
        compiledSelfPlan(dir);
        armTextUnderASecondArm();
        anOlderWriter(dir);
        aNewerWriter(dir);
        anOptional(dir);
        theSlide();
        negativeControls(dir);
        layoutValidation(dir);
        if (failures != 0) {
            System.out.println(failures + " failure(s)");
            System.exit(1);
        }
        System.out.println("java fixed form: the bytes are the C++ reference's, and the versioned path is the path");
    }
}
