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

    // WHAT §5.6 RETIRED HAS LEFT THIS PROGRAM (docs/FIXED-FORM-ALGORITHM.md
    // §5.6, §5.9 #23): textUnderAnArm, anOlderWriter, aNewerWriter, anOptional,
    // theSlide and layoutValidation read a file across two generations with no
    // lineage between them, or walked a stranger's layout at run time, and the
    // plan_too_large control could no longer be reached without a lineage. Their
    // coverage lives in the lineage harness
    // (internal/codegen/javatable/fixedversioning_test.go) and in the LOCK's
    // validation of what it records. A case not run is never a pass, so none is
    // kept here to be printed and passed over.

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
        check(!r.refused && !r.damaged && r.unknown == 0 && r.widened == 0,
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
        check(!r.refused && !r.damaged && r.unknown == 0,
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
        check(!r.refused && !r.damaged, "p1.bin: a clean read moves no counter");
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
        // THE PAYLOAD RIDES WHOLE WHETHER OR NOT IT IS PRESENT (§3.4) — which is
        // exactly why `?T` and a plain `T` are ONE BYTE APART on this form — AND
        // WHEN THE FLAG IS 0 WHAT RIDES IS ZERO. The reference's own storage for
        // this record carries 99 and "still" behind the false flag ON PURPOSE:
        // that is the stain the oracle plants, and the FILE carries none of it.
        // So an absent optional is a hole in the record and never a window into
        // the writer's memory.
        check(v[1].link.value == 0 && v[1].link.tagLength == 0,
                "p3.bin: an ABSENT optional's payload rides WHOLE, and behind a false flag it is the template's zeros (§3.4)");
        final byte[] back = new byte[tblp3.ChainFixed.measure(n)];
        check(tblp3.ChainFixed.save(v, n, back) == back.length, "p3.bin: save fills what measure says");
        check(Arrays.equals(back, golden), "p3.bin: the bytes are the C++ reference's, exactly");
        // AND THIS PORT'S OWN WRITER DOES THE SAME. The stain goes in the VALUE
        // this time, which the C++ oracle cannot reach from here, and the wire
        // must carry none of it — while the SAME payload PRESENT does put those
        // bytes on the wire, so the check is about the FLAG and not about a
        // payload never having been written.
        final tblp3.ChainFixed.Value[] stained = { new tblp3.ChainFixed.Value() };
        stained[0].nameLength = 6;
        setText(stained[0].name, "absent");
        stained[0].linkPresent = false;
        stained[0].link.value = 99;
        stained[0].link.tagLength = 5;
        setText(stained[0].link.tag, "still");
        final byte[] stainedBytes = new byte[tblp3.ChainFixed.measure(1)];
        tblp3.ChainFixed.save(stained, 1, stainedBytes);
        final tblp3.ChainFixed.Value[] clean = { new tblp3.ChainFixed.Value() };
        clean[0].nameLength = 6;
        setText(clean[0].name, "absent");
        clean[0].linkPresent = false;
        final byte[] cleanBytes = new byte[tblp3.ChainFixed.measure(1)];
        tblp3.ChainFixed.save(clean, 1, cleanBytes);
        check(Arrays.equals(stainedBytes, cleanBytes),
                "p3: an absent optional this port writes carries the template's zeros, not the caller's storage");
        stained[0].linkPresent = true;
        clean[0].linkPresent = true;
        final byte[] stainedPresent = new byte[tblp3.ChainFixed.measure(1)];
        tblp3.ChainFixed.save(stained, 1, stainedPresent);
        final byte[] cleanPresent = new byte[tblp3.ChainFixed.measure(1)];
        tblp3.ChainFixed.save(clean, 1, cleanPresent);
        check(!Arrays.equals(stainedPresent, cleanPresent),
                "p3: and the SAME payload PRESENT does put those bytes on the wire");
    }

    static void keyedWrite(String dir) {
        final byte[] golden = slurp(dir, "keyed.bin");
        final tabledemo.KeyedConfigFixed.Value[] v = new tabledemo.KeyedConfigFixed.Value[4];
        for (int i = 0; i < v.length; i++) { v[i] = new tabledemo.KeyedConfigFixed.Value(); }
        final tabledemo.TableFixed.Report r = new tabledemo.TableFixed.Report();
        final int n = tabledemo.KeyedConfigFixed.load(v, v.length, golden,
                tabledemo.TableFixed.plan(4096), new short[4096], tabledemo.KeyedConfigFixed.image(), r);
        check(n == 2, "keyed.bin: two records");
        check(!r.refused && !r.damaged, "keyed.bin: a clean read moves no counter");
        // record 1: k = 1
        check(v[1].teams[0].spawnCount == 14, "keyed: a keyed array's slot");
        check(textOf(v[1].teams[2].banner, v[1].teams[2].bannerLength).equals("green"), "keyed: text in a keyed slot");
        check(v[1].scores.perTeam[1] == 2001, "keyed: a `type` nested by value");
        check(v[1].hulls[2].turrets[0].damage == 16.0f, "keyed: a keyed array nested in a keyed array");
        check(v[1].hulls[0].turrets[0].gunnerPresent, "keyed: an optional section that is PRESENT");
        check(!v[1].hulls[0].turrets[1].gunnerPresent, "keyed: an optional section that is ABSENT");
        // ... and behind a false flag the payload that rides is the template's
        // zeros (§3.4), not the writer's storage — the bool included, which is
        // why `tracking` is asserted false beside the float.
        check(v[1].hulls[0].turrets[1].gunner.reaction == 0.2f && !v[1].hulls[0].turrets[1].gunner.tracking,
                "keyed: an ABSENT optional leaves its destination Value untouched while the wire carries zeros (§3.4)");
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
        check(!r.refused && !r.damaged, "pack.bin: a clean read moves no counter");
        check(v[0].version == 7 && v[0].settings.tickRate == 120, "pack: the scalars");
        // AN ENUM RIDES ITS ORDINAL, FROM 1: Hard is the third variant.
        check(v[0].settings.difficulty == tabledemo.Pack.Difficulty.HARD, "pack: an enum rides its ORDINAL, from 1");
        check(textOf(v[0].settings.buildNote, v[0].settings.buildNoteLength).equals("first build"), "pack: text at its length");
        check(v[0].settings.spawnDelays[2] == 1.5f, "pack: a fixed array of floats");
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
        check(r.unknown == 0, "self plan: a plan over MY OWN layout names nothing unknown");
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
    // The enum beside it is the OTHER half, under the strict read: an ordinal
    // past the last variant REFUSES the load as outOfRange, and a TAG past the
    // last arm does the same.

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
        final byte[] wire = utRecord("hello", tblut.UT.UtGrade.GOLD, 42, 11);

        // THE IDENTITY PATH
        final tblut.UtRootFixed.Value[] id = { new tblut.UtRootFixed.Value() };
        final tblut.TableFixed.Report r = new tblut.TableFixed.Report();
        check(tblut.UtRootFixed.load(id, 1, wire, tblut.TableFixed.plan(512), new short[512],
                tblut.UtRootFixed.image(), r) == 1, "arm text: the identity path reads the record");
        check(!r.refused && !r.damaged && r.unknown == 0,
                "arm text: a clean read moves no counter");
        check(id[0].pick.type == tblut.UtPickFixed.b, "arm text: the SECOND arm is the selected one");
        check(textOf(id[0].pick.b.label, id[0].pick.b.labelLength).equals("hello"),
                "arm text: the identity path lands the arm's text");

        // THE COMPILED PATH, and it must land the same record
        final tblut.TableFixed.Report rc = new tblut.TableFixed.Report();
        final tblut.UtRootFixed.Value viaPlan = utCompiled(wire, rc);
        check(textOf(viaPlan.pick.b.label, viaPlan.pick.b.labelLength).equals("hello"),
                "arm text: a COMPILED plan lands a text field under the SECOND arm");
        check(viaPlan.pick.b.grade == tblut.UT.UtGrade.GOLD && viaPlan.pick.b.m == 42 && viaPlan.tail == 11,
                "arm text: and the rest of that arm with it");
        check(!rc.damaged && !rc.refused,
                "arm text: the compiled read moves no counter either");
        check(utSame(viaPlan, id[0]),
                "arm text: a COMPILED plan lands exactly what the identity plan lands");

        // A TAG PAST THE LAST ARM REFUSES on both paths as outOfRange (the
        // strict read, docs/plans/strict-only.md; schema#1254, §5.8 row 12).
        // The compiled plan pushes the tag's None UNGUARDED with the writer's
        // arm count in its dstsize lane, so a tag past that set refuses
        // exactly as the identity plan's decode bound does (javatable, #1278).
        {
            final byte[] bad = wire.clone();
            final int body = utBodyAt(bad);
            check(bad[body] == 2, "tag past the last arm: the tag sits where the layout says");
            bad[body] = 7;

            final tblut.UtRootFixed.Value[] v = { new tblut.UtRootFixed.Value() };
            final tblut.TableFixed.Report r1 = new tblut.TableFixed.Report();
            check(tblut.UtRootFixed.load(v, 1, bad, tblut.TableFixed.plan(512), new short[512],
                    tblut.UtRootFixed.image(), r1) == -1, "tag past the last arm: the load refuses");
            check(r1.refused && r1.reason == tblut.TableFixed.Reason.outOfRange,
                    "tag past the last arm: the identity path refuses outOfRange");
            check(!r1.damaged, "tag past the last arm: a bad VALUE is not framing damage");

            final tblut.TableFixed.Report r2 = new tblut.TableFixed.Report();
            utCompiled(bad, r2);
            check(r2.refused && r2.reason == tblut.TableFixed.Reason.outOfRange && !r2.damaged,
                    "tag past the last arm: the compiled plan refuses outOfRange too");
        }

        // AN ORDINAL PAST THE LAST VARIANT REFUSES on both paths as
        // outOfRange — the identity scatter on its own read, and the plan's
        // ordinal op on the compiled one.
        {
            final byte[] bad = wire.clone();
            final int body = utBodyAt(bad);
            final int at = body + 13; // the tag, then the arm's text, then the ordinal
            check(bad[at] == tblut.UT.UtGrade.GOLD, "ordinal past the last variant: the ordinal is where the layout says");
            bad[at] = 9;

            final tblut.UtRootFixed.Value[] v = { new tblut.UtRootFixed.Value() };
            final tblut.TableFixed.Report r1 = new tblut.TableFixed.Report();
            check(tblut.UtRootFixed.load(v, 1, bad, tblut.TableFixed.plan(512), new short[512],
                    tblut.UtRootFixed.image(), r1) == -1, "ordinal past the last variant: the load refuses");
            check(r1.refused && r1.reason == tblut.TableFixed.Reason.outOfRange && !r1.damaged,
                    "ordinal past the last variant: the identity path refuses outOfRange");

            final tblut.TableFixed.Report r2 = new tblut.TableFixed.Report();
            utCompiled(bad, r2);
            check(r2.refused && r2.reason == tblut.TableFixed.Reason.outOfRange && !r2.damaged,
                    "ordinal past the last variant: the plan's ordinal op refuses outOfRange too");
        }
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
        check(!r.damaged, what + ": never damage");
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
        // — which under §5.3 it does by SELECTING OUT OF THE LINEAGE, so a
        // stranger's file is not compiled at all. An FX1 build that never locked
        // FX2's layout answers layout_newer, and it carries the FILE's hash.
        {
            final tblfx1.FxRootFixed.Value[] right = { new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final int n = tblfx1.FxRootFixed.load(right, 1, fx2,
                    tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
            check(n < 0 && r.refused && r.reason == tblfx1.TableFixed.Reason.layoutNewer,
                    "REFUSED BY NAME: a layout this build never locked is layout_newer");
            check(r.layoutHash == tblfx1.TableFixed.get64(fx2, tblfx1.TableFixed.hashAt),
                    "layout_newer reports THE FILE'S hash, and nothing else");
            check(!r.damaged, "a refusal by name never sets malformed too");
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
            // UNDER §5.3 THIS IS A HASH IN NO LINEAGE ENTRY, not a lie about a
            // known version: the header's hash is TAKEN AS GIVEN and never
            // recomputed from the layout behind it, so a flipped hash names a
            // layout this reader has never locked. The lie about a KNOWN version
            // — a hash the lineage holds whose layout bytes differ — is
            // layout_malformed, and the versioning harness's
            // hash_known_bytes_differ row is where it is asserted.
            check(n < 0 && r.refused && r.reason == tblfx1.TableFixed.Reason.layoutNewer,
                    "REFUSED BY NAME: a header hash no lineage entry holds");
            check(!r.damaged, "a lying header is a refusal and never damage");
        }

        // 3. A RAGGED TAIL: the two ends of the file have met, which is framing
        //    damage and not a refusal.
        {
            final byte[] ragged = Arrays.copyOf(fx1, fx1.length + 1);
            final tblfx1.FxRootFixed.Value[] v = { new tblfx1.FxRootFixed.Value(), new tblfx1.FxRootFixed.Value() };
            final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
            final int n = tblfx1.FxRootFixed.load(v, 2, ragged,
                    tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
            check(n < 0 && r.damaged, "a ragged tail: malformed, because the two ends of the file met");
        }

        // 4. MORE RECORDS THAN THE CALLER'S OWN ARRAY: a refusal by name, and
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
    // THE FIVE FORM-HEADER PROBES (schema#876, card 411411)
    // ------------------------------------------------------------------
    //
    // docs/FIXED-FORM-ALGORITHM.md §5.3 step 1 and docs/SPEC-TABLES.md §3.4:
    // THE FORM BYTE IS READ BEFORE THE FILE'S LENGTH. A fixed reader given `1`
    // answers previous_form, given `2` message_form_as_file, and given a byte
    // no form defines newer_form — WHATEVER THE FILE'S LENGTH. The lengths
    // below are the REAL lengths of the two forms this reader names: a
    // committed form-1 file is TEN bytes and a form-2 batch THREE. A reader
    // that measured first would answer malformed for both of the forms it is
    // supposed to name. THE MODEL IS test/elixir-fixedform/main.exs.
    //
    // Each probe runs the reader on the EXACT input, asserts the report's two
    // halves jointly (the named answer and nothing else claiming a read), and
    // asserts (ruling R4) the destination is back at its declared default: it
    // is pre-poisoned first and compared with a fresh value after the refusal.
    static void expectHeaderOutcome(byte[] file, tblfx1.TableFixed.Reason want,
                                    boolean wantMalformed, String what) {
        final tblfx1.FxRootFixed.Value v = new tblfx1.FxRootFixed.Value();
        v.keep = 0x5EED;
        v.nested.a = 0x1234;
        v.nested.b = 0x4321;
        final tblfx1.FxRootFixed.Value[] dest = { v };
        final tblfx1.TableFixed.Report r = new tblfx1.TableFixed.Report();
        final int n = tblfx1.FxRootFixed.load(dest, 1, file,
                tblfx1.TableFixed.plan(1024), new short[1024], tblfx1.FxRootFixed.image(), r);
        check(n < 0, what + ": no record was read (got n=" + n + ")");
        if (wantMalformed) {
            check(r.damaged && !r.refused && r.reason == tblfx1.TableFixed.Reason.none,
                    what + ": the report is damage and names no reason (refused=" + r.refused
                            + ", malformed=" + r.damaged + ", reason=" + r.reason + ")");
        } else {
            check(r.refused && r.reason == want && !r.damaged,
                    what + ": the report is the name " + want + " and never damage (refused="
                            + r.refused + ", malformed=" + r.damaged + ", reason=" + r.reason + ")");
        }
        check(r.unknown == 0 && r.widened == 0
                        && r.layoutHash == 0,
                what + ": no counter claims a read happened");
        final tblfx1.FxRootFixed.Value d = new tblfx1.FxRootFixed.Value();
        check(v.keep == d.keep && v.nested.a == d.nested.a && v.nested.b == d.nested.b,
                what + ": the destination record is back at its declared default (R4)");
    }

    static void formHeaderProbes(String dir) {
        final byte[] fx1 = slurp(dir, "fx1.bin");

        // 1. empty-file: there is no first byte to earn a name, so the residue.
        expectHeaderOutcome(new byte[0], null, true,
                "empty-file: zero bytes is malformed, with no reason");

        // 2. short-form1: a TEN-byte form-1 file, the form's own real length.
        expectHeaderOutcome(new byte[] { 1, 0, 0, 0, 0, 0, 0, 0, 0, 0 },
                tblfx1.TableFixed.Reason.previousForm, false,
                "short-form1: a TEN-byte form-1 file is previous_form, not malformed");

        // 3. short-form2: a THREE-byte form-2 batch, the form's own real length.
        expectHeaderOutcome(new byte[] { 2, 1, 0 },
                tblfx1.TableFixed.Reason.messageFormAsFile, false,
                "short-form2: a THREE-byte form-2 batch is message_form_as_file, not malformed");

        // 4. unassigned-form0: form byte 0 on the otherwise valid fixture tail.
        //    No form defines 0, and the registry is ordered, so it is newer.
        final byte[] form0 = fx1.clone();
        form0[0] = 0;
        expectHeaderOutcome(form0, tblfx1.TableFixed.Reason.newerForm, false,
                "unassigned-form0: form byte 0 on a valid fixture tail is newer_form, not malformed");

        // 5. reserved-byte3: a valid form-3 fixture with reserved byte 3 set
        //    nonzero. The seven reserved bytes are REFUSED, not ignored.
        final byte[] reserved3 = fx1.clone();
        reserved3[3] = 1;
        expectHeaderOutcome(reserved3, null, true,
                "reserved-byte3: a nonzero reserved byte 3 is malformed");
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
        negativeControls(dir);
        formHeaderProbes(dir);
        if (failures != 0) {
            System.out.println(failures + " failure(s)");
            System.exit(1);
        }
        System.out.println("java fixed form: the bytes are the C++ reference's, and the versioned path is the path");
    }
}
