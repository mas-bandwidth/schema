// OptionalArraysValidData — cell java/optional-arrays/valid-data, schema matrix
// row "Optional arrays": "Optional arrays: valid-data write/read acceptance"
// (schema docs/roadmap.sexp, task optional-arrays/java/valid-data).
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md:1682 is §7's proof list, whose corpus
// carries `p1`/`p3` for `?T` present and absent; the contract this cell pins is
// docs/SPEC-TABLES.md §2.3 — `?` on a bounded array, `?[..N]T` and `?[N]T`, "the
// same construct one shape up", where PRESENCE decides whether the field rides
// and "present and empty" and "absent" are two values no count alone can spell —
// and §3.4's optional wrapper, the ONE kind this form's layout adds:
//
//   * §3's C-table row `?T`: C(?T) = 1 + C(T) — the present flag FIRST, then
//     the payload, which rides WHOLE. For an array payload, C is §3's own:
//     `4 + Max*C(T)` for a counted array, `N*C(T)` for a fixed one.
//   * §3.1's write: an ABSENT optional writes flag 0 and skips the payload
//     store — what rides is the template's zeros; a PRESENT one writes the
//     array framing — the live count, ZERO INCLUDED, then count elements and
//     stop, slack zero (fix 1); a fixed array whole.
//   * §5.2's EMIT row `35 optional: copy 1 byte (the present flag), then EMIT
//     the payload` — and EMIT's `T into ?T` branch: a writer that sent the
//     array bare lands `present`, a CONSTANT 1, then the payload (§2.3's
//     "one framing with two spellings", byte for byte for any array that
//     rides under both).
//
// THE VECTOR IS CONSTRUCTED HERE FROM THE LAW, with the derivation below. No
// schema on the Java conformance driver's classpath (build/conformance-java:
// tables/examples, tables/pointers, tables/block, test/tables/{V1,V2,P1,P3})
// declares an optional array — the optionals there are nested tables and
// scalars — and the optional-array corpus lives in tables/messages, which the
// Java leg does not generate, so testdata/conformance/tables holds no data for
// this task. The writer's record bytes are therefore derived byte by byte from
// §3's C table and §3.1's write law, and the two layouts from §3.4's layout
// rules: kind 35 holds ONE child and its size is the child's + 1 ("one present
// byte in front of it"), and kind 14 a whole number of elements, behind a
// count or not.
//
// THE PRODUCTION PATH UNDER TEST is tblp3.TableFixed — the FIXED FORM's runtime,
// the reader's own engine, emitted by internal/codegen/javatable into every
// package on the classpath. THE TEST CALLS THE SAME FUNCTIONS THE GENERATED
// READERS CALL, nothing else:
//
//   * TableFixed.parse — the seven layout rules every layout a reader trusts
//     runs before a single record byte (§3.4; the wrapper's and the array's
//     rules are the optional array's own);
//   * TableFixed.compile — §5.2's EMIT, the plan a generated reader builds for
//     every lineage peer at class initialization (ChainFixed's Lineage runs
//     lineagePlans(), which parses both layouts and calls compile());
//   * TableFixed.run — the ONE read loop that load() runs over every record
//     (ChainFixed.load calls run() per record into the record image, then
//     scatters the image). The image is the read's landing domain: a byte[]
//     laid out exactly as this reader's own body, which is what this test
//     asserts where a generated scatter does not exist for the shape.
//
// The dest lanes below are the ones internal/codegen/javatable's walk lays down
// (the wrapper's aux is the present byte and it adds no dst; a counted array's
// dst is the element run, its aux the count word, its lane `counted` set — the
// same lanes ChainFixed.java and CfgFixed.java carry in `dest`).
//
// THE SHAPE, derived from §3's C table (sizes are the declaration's own):
//
//   fixed table OptArrays
//   {
//       picks ?[..2]int32   // counted optional array: 1 + (4 + 2*4) = 13
//       marks ?[2]uint16    // fixed optional array:   1 + 2*2     =  5
//   }                       // the body: 13 + 5 = 18 bytes
//
//   fixed table PlainArrays   // §2.3's "one framing with two spellings", the bare side
//   {
//       picks [..2]int32      // C = 12
//       marks [2]uint16       // C =  4
//   }                       // the body: 16 bytes
//
// One record body, byte for byte (every number little-endian):
//
//   offset  0      picks present flag   (1 byte, §3's `?T` row: 1 + C(T))
//   offset  1      picks count          (i32 LE, §3's `[Min..Max]T` row)
//   offset  5      picks[0]             (int32)
//   offset  9      picks[1]             (int32)
//   offset 13      marks present flag  (1 byte)
//   offset 14      marks[0]             (uint16)
//   offset 16      marks[1]             (uint16)
//
// RUN from the repo root, the way the driver is run (exit 0 = GREEN, the law
// asserted; exit 1 = RED, an assertion failed):
//
//   java -cp build/conformance-java test/conformance/java/rows/OptionalArraysValidData.java

import tblp3.TableFixed;

public final class OptionalArraysValidData {
    private OptionalArraysValidData() {}

    private static int failures = 0;

    private static void check(boolean ok, String what) {
        System.out.println((ok ? "ok   " : "FAILED ") + what);
        if (!ok) {
            failures++;
        }
    }

    // a report nobody moved: valid data reads clean, every counter zero, no
    // refusal — the "acceptance" half of the cell's title.
    private static boolean clean(TableFixed.Report r) {
        return r.clamped == 0 && r.widened == 0 && r.unknown == 0 && r.kindMismatch == 0
                && !r.malformed && !r.refused;
    }

    // THE WIRE ID IS fnv1a64 OF THE WIRE NAME (docs/SPEC-TABLES.md §5), and
    // TableFixed.hash is that same function over bytes — the production one,
    // not a copy, so the ids this test writes into the layouts are the ids the
    // compiler would lay down for these fields.
    private static long wireId(String name) {
        final byte[] b = name.getBytes(java.nio.charset.StandardCharsets.ISO_8859_1);
        return TableFixed.hash(b, 0, b.length);
    }

    private static void put64(byte[] b, int at, long v) {
        for (int i = 0; i < 8; i++) { b[at + i] = (byte) (v >>> (8 * i)); }
    }

    private static void put32(byte[] b, int at, int v) {
        for (int i = 0; i < 4; i++) { b[at + i] = (byte) (v >>> (8 * i)); }
    }

    // ONE LAYOUT ENTRY, seventeen bytes (§3.4): id u64, kind u8, size u32,
    // children u32, every number little-endian.
    private static byte[] entry(long id, int kind, long size, int children) {
        final byte[] b = new byte[TableFixed.entryBytes];
        put64(b, 0, id);
        b[8] = (byte) kind;
        put32(b, 9, (int) size);
        put32(b, 13, children);
        return b;
    }

    // a layout: the u32 entry count, then the entries pre-order.
    private static byte[] layout(byte[]... entries) {
        final byte[] out = new byte[TableFixed.layoutHeaderBytes + entries.length * TableFixed.entryBytes];
        put32(out, 0, entries.length);
        int at = TableFixed.layoutHeaderBytes;
        for (byte[] e : entries) {
            System.arraycopy(e, 0, out, at, TableFixed.entryBytes);
            at += TableFixed.entryBytes;
        }
        return out;
    }

    // the record bytes, spelled as hex so the derivation is reviewable in place
    private static byte[] hex(String s) {
        final String h = s.replaceAll("\\s", "");
        final byte[] out = new byte[h.length() / 2];
        for (int i = 0; i < out.length; i++) {
            out[i] = (byte) Integer.parseInt(h.substring(i * 2, i * 2 + 2), 16);
        }
        return out;
    }

    // the case helper: run one record body under the plan, then assert the
    // landed image byte-for-byte and a report nobody moved.
    private static void readCase(String what, TableFixed.Entry[] plan, int count,
                                String vectorHex, String expectedHex) {
        final byte[] record = hex(vectorHex);
        final byte[] image = new byte[18];
        java.util.Arrays.fill(image, (byte) 0xFF); // a byte the plan does not land stays 0xFF
        final TableFixed.Report r = new TableFixed.Report();
        TableFixed.run(plan, count, new short[256], record, 0, record.length, image, r);
        final boolean landed = java.util.Arrays.equals(image, hex(expectedHex));
        check(landed && clean(r), what);
        if (!landed) {
            System.out.println("       image  = " + java.util.HexFormat.of().formatHex(image));
            System.out.println("       expect = " + expectedHex);
        }
        if (!clean(r)) {
            System.out.println("       report: clamped=" + r.clamped + " widened=" + r.widened
                    + " unknown=" + r.unknown + " kindMismatch=" + r.kindMismatch
                    + " malformed=" + r.malformed + " refused=" + r.refused + " " + r.reason);
        }
    }

    public static void main(String[] args) {
        final long idPicks = wireId("picks");
        final long idMarks = wireId("marks");

        // THE OPT LAYOUT, seven entries, the walk internal/codegen/javatable
        // makes of the shape above: the root table, each field's wrapper (kind
        // 35, ONE child, size = the child's + 1 — §3.4's "one present byte in
        // front of it"), the array (kind 14: 4 + 2*4 counted, 2*2 bare), and
        // the element leaf at id 0, as fixedWalkElement lays it down.
        final byte[] optLayout = layout(
                entry(wireId("OptArrays"), 13, 18, 2),
                entry(idPicks, 35, 13, 1),
                entry(idPicks, 14, 12, 1),
                entry(0, 4, 4, 0),
                entry(idMarks, 35, 5, 1),
                entry(idMarks, 14, 4, 1),
                entry(0, 7, 2, 0));

        // THE PLAIN TWIN: the same two fields bare, no wrappers.
        final byte[] plainLayout = layout(
                entry(wireId("PlainArrays"), 13, 16, 2),
                entry(idPicks, 14, 12, 1),
                entry(0, 4, 4, 0),
                entry(idMarks, 14, 4, 1),
                entry(0, 7, 2, 0));

        // THE READER'S DEST LANES, five per entry, the ones the generated walk
        // lays down (ChainFixed.java and CfgFixed.java carry the same for their
        // own shapes): the wrapper names the PRESENT byte at aux and adds no
        // dst; a counted array's dst is the ELEMENT RUN (count word first), its
        // aux the count word, its `counted` lane set; a bare array's dst is its
        // elements.
        final int[] optDest = {
            0, 0, 0, 0, 0,   // 0 OptArrays
            0, 0, 0, 0, 0,   // 1 picks ?  — aux 0: the present byte at body offset 0
            5, 4, 1, 1, 0,   // 2 picks — dst 5: elements (count at 1); stride 4; aux 1: the count; counted
            0, 0, 0, 0, 0,   // 3 int32
            0, 0, 13, 0, 0,  // 4 marks ? — aux 13: the present byte at body offset 13
            14, 2, 0, 0, 0,  // 5 marks — dst 14: the elements; stride 2; no count word
            0, 0, 0, 0, 0,   // 6 uint16
        };

        // THE LAYOUT RULES ACCEPT BOTH SHAPES (§3.4, before a single record
        // byte): this is the layout half of the valid-data acceptance — the
        // wrapper's 13 is 12 + 1 and 5 is 4 + 1, and each array is a whole
        // number of elements, behind a count or not.
        final TableFixed.Report pr = new TableFixed.Report();
        final TableFixed.Layout opt = TableFixed.parse(optLayout, 0, optLayout.length, pr);
        check(opt != null && !pr.refused,
                "the layout rules accept the optional-array layout (kind 35 over a counted and a fixed array)");

        final TableFixed.Report pp = new TableFixed.Report();
        final TableFixed.Layout plain = TableFixed.parse(plainLayout, 0, plainLayout.length, pp);
        check(plain != null && !pp.refused,
                "the layout rules accept the plain twin's layout");

        // THE PLANS, the same ones a generated reader's class initializer
        // builds through lineagePlans() for each lineage peer: this reader
        // under its own shape (?T into ?T), and the plain twin under it
        // (T into ?T). The compile census is part of the acceptance: neither
        // peer moved a counter.
        final TableFixed.Entry[] ownPlan = TableFixed.plan(64);
        final TableFixed.Report cr = new TableFixed.Report();
        final int ownCount = TableFixed.compile(opt, opt, optDest, ownPlan, new short[256], cr);
        check(ownCount > 0 && clean(cr),
                "compile: a same-shape optional-array peer builds a clean plan (?T into ?T)");

        final TableFixed.Entry[] wrapPlan = TableFixed.plan(64);
        final TableFixed.Report wr = new TableFixed.Report();
        final int wrapCount = TableFixed.compile(plain, opt, optDest, wrapPlan, new short[256], wr);
        check(wrapCount > 0 && clean(wr),
                "compile: the bare twin builds a clean plan against the optional reader (T into ?T)");

        // ---- CASE 1: present, live data and slack. picks is present with ONE
        // live element (a non-default value) and the count's slack behind it;
        // marks present with both elements live. Everything lands
        // byte-for-byte and no counter moves.
        readCase("present: a live element lands and the count's slack stays the writer's zeros",
                ownPlan, ownCount,
                "01 01000000 2a2a2a2a 00000000" + " 01 beef 1234",
                "01 01000000 2a2a2a2a 00000000" + " 01 beef 1234");

        // ---- CASE 2: the boundary §2.3 buys — PRESENT AND EMPTY. picks rides
        // present with count 0, and marks is ABSENT behind it: flag 1 over a
        // count 0 and flag 0 are two values, which no count alone can spell.
        readCase("present and empty: the flag stays 1 over the count 0 — two values no count alone can spell",
                ownPlan, ownCount,
                "01 00000000 00000000 00000000" + " 00 0000 0000",
                "01 00000000 00000000 00000000" + " 00 0000 0000");

        // ---- CASE 3: ABSENT, and the fixed twin at all defaults. picks is
        // absent — flag 0, the payload the writer's zeros (§3.1: an absent
        // optional writes flag 0 and skips the payload store) — and marks is a
        // PRESENT FIXED array at all defaults, which rides whole anyway
        // (§2.3: presence decides, never content).
        readCase("absent: the flag lands 0 and the payload the writer's zeros; a present fixed array at all defaults rides whole",
                ownPlan, ownCount,
                "00 00000000 00000000 00000000" + " 01 0000 0000",
                "00 00000000 00000000 00000000" + " 01 0000 0000");

        // ---- CASE 4: count AT THE BOUND — the full array, both elements live,
        // no slack, no clamp (§4.5's count bound is the writer's own 2).
        readCase("count at the bound: the full array lands both elements and no clamp fires",
                ownPlan, ownCount,
                "01 02000000 11111111 22222222" + " 00 0000 0000",
                "01 02000000 11111111 22222222" + " 00 0000 0000");

        // ---- CASE 5: T INTO ?T (§5.2 EMIT's first branch; §2.3's "one
        // framing with two spellings"). The plain twin's record carries NO
        // flags — count, elements, marks — and the optional reader wraps what
        // the writer sent bare: both present bytes land the CONSTANT 1
        // (EMIT's `present`, never a copied byte), and the payload lands
        // byte-for-byte where it rode.
        readCase("T into ?T: the bare writer's arrays land present — the constant 1 — and the payload lands whole",
                wrapPlan, wrapCount,
                "01000000 33333333 00000000" + " beef 1234",
                "01 01000000 33333333 00000000" + " 01 beef 1234");

        if (failures != 0) {
            System.out.println("RED: " + failures + " assertion(s) failed");
            System.exit(1);
        }
        System.out.println("green: optional-arrays/java/valid-data —"
                + " Optional arrays: valid-data write/read acceptance");
    }
}
