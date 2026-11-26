// R14 — EVERY COMPILED ENTRY'S REACH IS IN THE WRITER'S COORDINATES: a union
// tag grown 1 -> 8 reads the writer's tag at the WRITER's width and compiles
// whole; the run of the plan it lays lands the tag in 8 bytes and the arm's
// payload whole past it, never the payload read into the tag.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15): the union's None
// `const 0` LANDS my_tag bytes into the reader's tag, and a leg that reads the
// writer's raw tag beside it (to hold it to the writer's arm count, §4.6)
// reads it at THE WRITER'S TAG WIDTH, their_tag bytes, never my_tag: its
// source reach is src + their_tag, inside the writer's record, so a grown tag
// compiles whole and the tag never swallows the arm's payload.
//
// THE JAVA LEG (build/tables-generated-java/v1/TableFixed.java, emitted from
// internal/codegen/javatable/fixedruntime.go): compileEntry's `case 15:`
// pushes the None const with src = the writer's tag offset, size = MY tag
// width (the bytes it lands), dstsize = the writer's arm count and sign = THE
// WRITER'S TAG WIDTH; Compiler.opReach answers 0 for opConst (a const reads
// no record byte at compile), and run's opConst case reads the writer's raw
// tag at `sign` bytes, bounded by the record length there.
//
// THE VECTOR (one 17-byte layout entry: id u64LE, kind u8, size u32LE,
// children u32LE, after a u32LE entry count; kind 13 a table, 15 a union whose
// size is tag + widest arm, 8 a u32):
//   THEIRS, the writer's lock layout: a record of 5 bytes — one union whose
//   tag is 1 byte, one arm a u32.
//   MINE(tag), the reader: the same ids with the tag grown to `tag` (a lawful
//   §5.2 ordinal-width evolution); my union's tag at image 0, the u32 arm at
//   image `tag`.
//
// Expected: THEIRS vs MINE(8) compiles WHOLE (never layout_record_too_large);
// the None const reads src 0, lands 8 bytes, reads the writer's tag at 1
// byte (sign), reach 0 + 1 of 5; the run over [1, 44 33 22 11] lands my tag
// exactly 1 in 8 bytes (high bytes zero) and the u32 0x11223344; tag 0 is
// None; a forged tag 2 refuses outOfRange. The twins MINE(1) and MINE(2)
// compile whole, and the lineage lane the generated unit lays at class
// initialization is ready for MINE(8) with the lock's record size.
//
// RUN (from the repository root, after make build/tables-generated-java/.stamp;
// tools/conformancerows compiles the v1 unit this row names):
//   java -cp <v1 classes> test/conformance/java/rows/R14.java
// exit 0 green, exit 1 red, one printed line per assertion.

import tblv1.TableFixed;

public final class R14 {
    private static int failures = 0;

    private static void check(boolean ok, String line) {
        System.out.println((ok ? "pass" : "FAIL") + ": " + line);
        if (!ok) {
            failures++;
        }
    }

    // ---- the vectors --------------------------------------------------------

    private static byte[] block(long[][] entries) {
        final byte[] b = new byte[TableFixed.layoutHeaderBytes + entries.length * TableFixed.entryBytes];
        TableFixed.put32(b, 0, entries.length);
        for (int i = 0; i < entries.length; i++) {
            final int at = TableFixed.layoutHeaderBytes + i * TableFixed.entryBytes;
            TableFixed.put64(b, at, entries[i][0]);
            b[at + 8] = (byte) entries[i][1];
            TableFixed.put32(b, at + 9, (int) entries[i][2]);
            TableFixed.put32(b, at + 13, (int) entries[i][3]);
        }
        return b;
    }

    // THEIRS — the writer's lock layout: record 5, a union of tag 1 over one u32.
    private static final byte[] THEIRS = block(new long[][] {
        {1, 13, 5, 1},
        {7, 15, 5, 1},
        {9, 8, 4, 0},
    });

    // MINE — the same ids, the union's tag grown to `tag`, its one arm unchanged.
    private static byte[] mine(int tag) {
        return block(new long[][] {
            {1, 13, tag + 4, 1},
            {7, 15, tag + 4, 1},
            {9, 8, 4, 0},
        });
    }

    // MY SIDE, five lanes per entry (dest, stride, aux, counted, arg), the way
    // the generated <Root>Fixed.dest rows lay it: the union's tag at image 0
    // (its aux lane), its arm's u32 past my tag.
    private static int[] dest(int tag) {
        return new int[] {
            0, 0, 0, 0, 0, // 0 the root
            0, 0, 0, 0, 0, // 1 the union: my tag at aux 0
            tag, 0, 0, 0, 0, // 2 the u32 arm, past my tag
        };
    }

    private static TableFixed.Layout parse(byte[] bytes, String what) {
        final TableFixed.Report r = new TableFixed.Report();
        final TableFixed.Layout l = TableFixed.parse(bytes, 0, bytes.length, r);
        check(l != null && !r.refused, what + " is a lawful layout (parse answers it, no refusal: " + r.reason + ")");
        return l;
    }

    private static final class Compiled {
        int made;
        TableFixed.Entry[] plan;
        short[] remap;
        TableFixed.Report report;
    }

    private static Compiled compile(int tag) {
        final Compiled c = new Compiled();
        c.plan = TableFixed.plan(256);
        c.remap = new short[1024];
        c.report = new TableFixed.Report();
        final TableFixed.Layout t = parse(THEIRS, "THEIRS");
        final TableFixed.Layout m = parse(mine(tag), "MINE(" + tag + ")");
        c.made = (t == null || m == null) ? -2 : TableFixed.compile(t, m, dest(tag), c.plan, c.remap, c.report);
        return c;
    }

    // THE UNION'S None CONST: opConst, aux 0 (None), the writer's arm count in dstsize.
    private static TableFixed.Entry noneConst(Compiled c) {
        for (int i = 0; i < c.made; i++) {
            final TableFixed.Entry e = c.plan[i];
            if (e.op == TableFixed.opConst && e.aux == 0 && e.dstsize != 0) {
                return e;
            }
        }
        return null;
    }

    private static TableFixed.Report run(Compiled c, byte[] record, byte[] image) {
        final TableFixed.Report r = new TableFixed.Report();
        TableFixed.run(c.plan, c.made, c.remap, record, 0, record.length, image, r);
        return r;
    }

    public static void main(String[] args) {
        // ---- 1. THE CELL: the tag grown 1 -> 8 compiles WHOLE ---------------
        final Compiled c = compile(8);
        check(c.made > 0, "R14: THEIRS vs MINE(8) compiles whole: compile answers " + c.made + " entries");
        check(!c.report.refused && c.report.reason == TableFixed.Reason.none,
                "R14: no refusal on the report (not layoutRecordTooLarge): got " + c.report.reason);
        check(c.report.reason != TableFixed.Reason.layoutRecordTooLarge,
                "R14: the grown tag is never layoutRecordTooLarge");

        // ---- 2. the None const reads the WRITER's 1-byte tag ----------------
        final TableFixed.Entry none = noneConst(c);
        check(none != null, "R14: the union's None const is in the plan");
        if (none != null) {
            check(none.src == 0, "R14: the None const reads the writer's tag at its offset 0, got " + none.src);
            check(none.size == 8, "R14: the None const LANDS my 8-byte tag, got " + none.size);
            check(none.sign == 1, "R14: the None const reads the WRITER's 1-byte tag, never 8: got " + none.sign);
            check(none.dstsize == 1, "R14: it carries the writer's arm count, 1: got " + none.dstsize);
            check(none.guard == TableFixed.noGuard, "R14: the None const is unguarded, got " + none.guard);
            check(none.src + none.sign <= 5, "R14: its reach, 0 + 1, is inside the writer's record of 5");
        }
        boolean armCopy = false;
        for (int i = 0; i < c.made; i++) {
            final TableFixed.Entry e = c.plan[i];
            if (e.op == TableFixed.opCopy && e.src == 1 && e.dst == 8 && e.size == 4) {
                armCopy = true;
            }
        }
        check(armCopy, "R14: the u32 arm is a copy of 4 bytes from src 1 (past the WRITER's tag) to image 8 (past MY tag)");

        // ---- 3. RUN the plan it lays ----------------------------------------
        if (c.made > 0) {
            // the writer's arm 1 carrying 0x11223344: my tag lands exactly 1 in
            // 8 bytes, the u32 whole past it; the payload is never the tag.
            {
                final byte[] image = new byte[12];
                final TableFixed.Report r = run(c, new byte[] {1, 0x44, 0x33, 0x22, 0x11}, image);
                check(!r.refused, "R14: arm 1 reads with no refusal, got " + r.reason);
                final long tag = TableFixed.get64(image, 0);
                check(tag == 1L, "R14: my 8-byte tag lands exactly 1 (high bytes zero), got 0x" + Long.toHexString(tag));
                final int u = TableFixed.get32(image, 8);
                check(u == 0x11223344, "R14: the u32 lands whole past my tag, got 0x" + Integer.toHexString(u));
            }
            // tag 0 is None: my whole tag lands 0.
            {
                final byte[] image = new byte[12];
                java.util.Arrays.fill(image, 0, 8, (byte) 0x5A);
                final TableFixed.Report r = run(c, new byte[] {0, 0, 0, 0, 0}, image);
                check(!r.refused, "R14: tag 0 reads with no refusal, got " + r.reason);
                check(TableFixed.get64(image, 0) == 0L,
                        "R14: tag 0 lands None in my 8-byte tag, got 0x" + Long.toHexString(TableFixed.get64(image, 0)));
            }
            // a forged tag past the writer's one arm, read at the writer's width,
            // refuses outOfRange (§4.6).
            {
                final byte[] image = new byte[12];
                final TableFixed.Report r = run(c, new byte[] {2, 0, 0, 0, 0}, image);
                check(r.refused && r.reason == TableFixed.Reason.outOfRange,
                        "R14: a forged writer tag 2 refuses outOfRange, got refused=" + r.refused + " " + r.reason);
            }
        }

        // ---- 4. THE TWINS: MINE(1) and MINE(2) compile whole ----------------
        for (int tag : new int[] {1, 2}) {
            final Compiled t = compile(tag);
            check(t.made > 0 && !t.report.refused,
                    "R14: the twin THEIRS vs MINE(" + tag + ") compiles whole: " + t.made + " entries, " + t.report.reason);
        }

        // ---- 5. THE LINEAGE LANE the generated unit lays at class init -----
        for (int tag : new int[] {8, 2}) {
            final TableFixed.KnownLayout[] known = {
                new TableFixed.KnownLayout(0x4444444433333333L, THEIRS, THEIRS.length, 8 + 5),
            };
            final TableFixed.Plan[] lanes =
                    TableFixed.lineagePlans(known, mine(tag), dest(tag), 0x2222222211111111L, tag + 4);
            final TableFixed.Plan lane = lanes[0];
            check(lane != null && lane.why == TableFixed.Reason.none && lane.entries != null && lane.count > 0,
                    "R14: the lineage lane for THEIRS under MINE(" + tag + ") is ready, got "
                            + (lane == null ? "null" : lane.why + " count " + lane.count));
        }

        System.out.println(failures == 0 ? "PASS: R14" : "FAIL: R14 (" + failures + " failed)");
        if (failures != 0) {
            System.exit(1);
        }
    }
}
