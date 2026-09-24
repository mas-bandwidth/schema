// R9 — a known hash with a different layout length or bytes →
// layout_malformed; the seven §1.1 malformations under a known hash all come
// back as this one name (docs/roadmap.sexp java/R9, matrix schema#876, audit
// schema#898).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:862-864): "The order is load-bearing
// and it is not §1.1's order. A layout arriving on the wire is no longer
// walked, so the seven rules do not fire at run time: the hash is looked up,
// then the floor, then the bytes are compared. The seven §1.1 malformations
// under a KNOWN hash all come back as one name, layout_malformed — a lie
// about a known version." Step 7 (§5.3) is the comparison itself: "k :=
// R.known[i]; if L != k.layout_length or the L bytes at layout differ from
// k.layout: REFUSE layout_malformed" — the refusal table's row "a known
// hash, different layout length or bytes | layout_malformed | the name".
//
// PRODUCTION PATH UNDER TEST. tblv1.CfgFixed.load, the generated fixed-form
// reader on the conformance driver's classpath (make/java.mk builds it into
// build/conformance-java beside test/conformance/java/src/Driver.java):
// load -> TableFixed.readHeader (steps 1-3) -> TableFixed.select (step 5,
// the header's hash taken as given) -> the floor (step 6) -> STEP 7
// (build/tables-generated-java/v1/CfgFixed.java:511-514): the layout LENGTH
// held to k.layoutBytes and the L layout BYTES held to k.layout[i] — a BYTE
// COMPARISON and never §1.1's walk, which is why no probe below can come
// back under a rule's own name. That comparison is the production read path
// every fixed-table consumer goes through (emitted by
// internal/codegen/javatable/fixedform.go:1608-1611); the reader is driven
// end to end through load, and no helper is the witness.
//
// THE VECTOR. testdata/conformance/tables carries no form-3 data (the
// fixed-form corpus is build/fixedform-corpus, written by the C++ reference),
// so the vector is constructed here from the law through the PRODUCTION
// WRITER, CfgFixed.save, whose every byte the law fixes (§3.1): the form
// byte 3 at 0; the seven reserved zeros at 1..7; the hash — the compile-time
// constant R.own_hash itself, handed to the reader and never derived — at 8;
// the u32 layout length at 16; the layout bytes from 20; then one record of
// 8 + 250 = 258 bytes, opening with the same eight hash bytes. Every probe
// below is that file with EXACTLY ONE field moved (each move's derivation is
// its own comment) or, for the depth rule, the smallest layout the law's own
// walk bound refuses.
//
// BUILD DEPENDENCY: build/conformance-java (make build-conformance-java).
// RUN: java -cp build/conformance-java test/conformance/java/rows/R9.java

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import tblv1.CfgFixed;
import tblv1.TableFixed;

public final class R9 {
    private R9() {}

    private static int red = 0;

    /** the destination byte a refusal must never write. */
    private static final int MARKER = 4242;

    /** the layout's own four-byte entry count, at the head of the layout. */
    private static final int LAYOUT_AT = 20;

    /** entry 0 is seventeen bytes into the layout proper. */
    private static final int ENTRY_0 = LAYOUT_AT + 4;

    private static int entryAt(int k) { return ENTRY_0 + k * 17; }

    private static void check(boolean ok, String what) {
        System.out.println((ok ? "ok: " : "RED: ") + what);
        if (!ok) { red++; }
    }

    /** one Cfg record, three fields the identity lane must land whole. */
    private static CfgFixed.Value one() {
        final CfgFixed.Value v = new CfgFixed.Value();
        v.a = 777;
        final byte[] name = "r9".getBytes(StandardCharsets.US_ASCII);
        System.arraycopy(name, 0, v.name, 0, name.length);
        v.nameLength = name.length;
        v.tierPresent = true;
        v.tier = 42;
        return v;
    }

    /** first field that failed to round-trip, or "" when every one landed. */
    private static String landed(CfgFixed.Value v) {
        if (v.a != 777) { return "a"; }
        if (v.nameLength != 2 || v.name[0] != 'r' || v.name[1] != '9') { return "name"; }
        if (!v.tierPresent || v.tier != 42) { return "tier"; }
        return "";
    }

    /** the report a read that lands values owes (§5.3's joint table). */
    private static boolean clean(TableFixed.Report r) {
        return !r.refused && !r.malformed && r.reason == TableFixed.Reason.none
                && r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0
                && r.layoutHash == 0;
    }

    private static String shape(TableFixed.Report r) {
        return "n/a reason=" + r.reason + " refused=" + r.refused + " malformed=" + r.malformed
                + " layoutHash=0x" + Long.toHexString(r.layoutHash);
    }

    private static final class Read {
        int n;
        final TableFixed.Report r = new TableFixed.Report();
    }

    private static Read run(byte[] f, CfgFixed.Value[] v) {
        final Read out = new Read();
        out.n = CfgFixed.load(v, v.length, f, TableFixed.plan(64), new short[64],
                CfgFixed.image(), out.r);
        return out;
    }

    /** one seventeen-byte layout entry (id u64, kind u8, size u32, children
     *  u32), §1's struct Entry. */
    private static void putEntry(ByteArrayOutputStream out, long id, int kind, int size, int children) {
        final byte[] e = new byte[17];
        TableFixed.put64(e, 0, id);
        e[8] = (byte) kind;
        TableFixed.put32(e, 9, size);
        TableFixed.put32(e, 13, children);
        out.write(e, 0, e.length);
    }

    /** a FILE around a hand-built layout, under the KNOWN hash: §3's header
     *  with R.own_hash — the compile-time constant, never derived from these
     *  bytes (§5.3 step 4) — then the layout's length and the layout. Every
     *  probe refuses before a record is reached, so there are none. */
    private static byte[] fileWithKnownHash(byte[] layout) {
        final byte[] out = new byte[20 + layout.length];
        out[0] = 3;
        TableFixed.put64(out, TableFixed.hashAt, CfgFixed.hash);
        TableFixed.put32(out, TableFixed.fileHeaderBytes, layout.length);
        System.arraycopy(layout, 0, out, 20, layout.length);
        return out;
    }

    /** THE SMALLEST VECTOR §1.1 RULE 7 CAN REFUSE. The walk bound is
     *  TableFixed.maxDepth = 64 and entry k of a single-child chain sits at
     *  depth k, so the refused entry is the one at depth 65 — and the index
     *  rule names nothing unless that entry is inside the count, so the
     *  smallest such layout carries 66 entries (4 + 17 * 66 = 1126 bytes):
     *  a root table of one field (parse demands kind 13 and a nonzero size),
     *  a chain of optional wrappers, and the bottom entry. Walked at its own
     *  length the name is layout_too_deep; under the known hash it is not. */
    private static byte[] deepLayout() {
        final int entries = TableFixed.maxDepth + 2;
        final ByteArrayOutputStream out = new ByteArrayOutputStream();
        final byte[] count = new byte[4];
        TableFixed.put32(count, 0, entries);
        out.write(count, 0, 4);
        for (int i = 0; i < entries; i++) {
            final boolean root = i == 0;
            final boolean bottom = i == entries - 1;
            putEntry(out, i + 1, root ? 13 : (bottom ? 1 : 35), 1, bottom ? 0 : 1);
        }
        return out.toByteArray();
    }

    /** THE JOINT SHAPE, asserted on every named refusal (§5.3): the file's
     *  hash is still the KNOWN constant; refused plus reason is set and
     *  malformed is NOT (the two answers are never both set); the name is
     *  the ONE name and never the §1.1 rule a walk would have named; no
     *  counter moved; the name carries no layout hash; and REFUSE is total —
     *  not one destination byte written. */
    private static void refusesOneName(byte[] f, TableFixed.Reason ifWalked, String what) {
        final CfgFixed.Value[] v = { new CfgFixed.Value() };
        v[0].a = MARKER;
        final Read read = run(f, v);
        final TableFixed.Report r = read.r;
        final boolean underKnown = TableFixed.get64(f, TableFixed.hashAt) == CfgFixed.hash;
        final boolean oneName = read.n == -1 && r.refused
                && r.reason == TableFixed.Reason.layoutMalformed;
        final boolean joint = !r.malformed && r.unknown == 0 && r.kindMismatch == 0
                && r.widened == 0 && r.clamped == 0 && r.layoutHash == 0;
        final boolean total = v[0].a == MARKER;
        check(underKnown && oneName && joint && total,
                what + " — under the KNOWN hash the answer is the ONE name layout_malformed"
                + (ifWalked == null ? "" : ", never " + ifWalked)
                + (underKnown && oneName && joint && total
                        ? "" : " (got n=" + read.n + " " + shape(r) + " a=" + v[0].a + ")"));
    }

    public static void main(String[] args) {
        // THE VECTOR, from the law through the production writer.
        final CfgFixed.Value written = one();
        final byte[] f = new byte[CfgFixed.measure(1)];
        final int saved = CfgFixed.save(new CfgFixed.Value[] { written }, 1, f);
        check(saved == f.length, "the production writer wrote the whole file: " + saved + " of "
                + f.length + " bytes (header " + CfgFixed.headerBytes + " + 1 record of "
                + CfgFixed.recordBytes + ")");

        // THE FRAME THE LAW FIXES, and the constants every probe is held to.
        check(f[0] == 3 && f[1] == 0 && f[2] == 0 && f[3] == 0 && f[4] == 0 && f[5] == 0
                && f[6] == 0 && f[7] == 0,
                "the file's frame is the law's: form byte 3 at 0, the seven reserved zeros at 1..7");
        check(TableFixed.get64(f, TableFixed.hashAt) == CfgFixed.hash
                && CfgFixed.known[0].hash == CfgFixed.hash,
                "the hash at 8 IS the compile-time constant R.own_hash (0x"
                        + Long.toHexString(CfgFixed.hash)
                        + "L), handed to the reader and never derived — and R.known[0] holds the "
                        + "same hash, so every probe below runs under a KNOWN hash");
        check(TableFixed.get32(f, 16) == CfgFixed.layout.length
                && CfgFixed.known[0].layoutBytes == CfgFixed.layout.length
                && Arrays.equals(Arrays.copyOfRange(f, 20, 20 + CfgFixed.layout.length),
                        CfgFixed.known[0].layout),
                "the u32 at 16 is the layout's length (" + CfgFixed.layout.length
                        + ") and the layout behind it is byte for byte the lock's own bytes, R.known[0]");

        // THE ANCHOR: the unbroken file reads, so the breaks below are the
        // breaks (§5.3's third outcome row: no refusal, no damage, no counters,
        // every value landed).
        final CfgFixed.Value[] out = { new CfgFixed.Value() };
        final Read read = run(f, out);
        final String d = landed(out[0]);
        check(read.n == 1 && clean(read.r) && d.isEmpty(),
                "the unbroken file reads clean on the identity lane, so the breaks below are the breaks"
                + (read.n == 1 && clean(read.r) && d.isEmpty()
                        ? "" : " (got n=" + read.n + " " + shape(read.r) + " drift=" + d + ")"));

        // STEP 7's RULE, BOTH ARMS — the title's first clause. A known hash
        // with a different layout LENGTH: L one too large, and one too small,
        // both still 20 + L inside the file so step 3 passes and step 7's
        // length arm is what refuses.
        final byte[] tooLong = f.clone();
        TableFixed.put32(tooLong, 16, CfgFixed.layout.length + 1);
        refusesOneName(tooLong, null,
                "a known hash with the layout length one too large (L=" + (CfgFixed.layout.length + 1)
                        + " against the lock's " + CfgFixed.known[0].layoutBytes + ")");

        final byte[] tooShort = f.clone();
        TableFixed.put32(tooShort, 16, CfgFixed.layout.length - 1);
        refusesOneName(tooShort, null,
                "a known hash with the layout length one too small (L=" + (CfgFixed.layout.length - 1)
                        + " against the lock's " + CfgFixed.known[0].layoutBytes + ")");

        // ... and with different layout BYTES: entry 0's id's low byte moved.
        // THE ID IS NEVER CHECKED by §1.1 — the walk looks at kind, size and
        // children alone — so this layout WALKS CLEAN and is refused all the
        // same: the lock holds the bytes, not the rules.
        final byte[] moved = f.clone();
        moved[ENTRY_0] ^= 1;
        refusesOneName(moved, null,
                "a known hash with one layout byte moved (entry 0's id's low byte), a layout that "
                        + "would walk CLEAN under §1.1 since ids are never checked");

        // THE SEVEN §1.1 MALFORMATIONS UNDER A KNOWN HASH — the title's second
        // clause. Each file is the unbroken one with EXACTLY ONE field moved,
        // so each IS the malformation its rule names (the derivation is the
        // comment on the move) and would be refused under ITS OWN NAME by a
        // walk. The walk does not run at run time: every one comes back as
        // the ONE name.
        {
            // rule 1 (layout_count_mismatch): 4 + 17 * count must equal the
            // layout's stated length; count 67 -> 68 makes 1160 != 1143.
            final byte[] p = f.clone();
            TableFixed.put32(p, LAYOUT_AT, TableFixed.get32(p, LAYOUT_AT) + 1);
            refusesOneName(p, TableFixed.Reason.layoutCountMismatch,
                    "§1.1 rule 1 (layout_count_mismatch): the entry count no longer fits the "
                            + "layout length");
        }
        {
            // rule 2 (layout_kind_unknown): entry 1 is `a`, an i32 leaf; kind
            // 200 is outside the closed set 1..30, 32, 33, 35.
            final byte[] p = f.clone();
            p[entryAt(1) + 8] = (byte) 200;
            refusesOneName(p, TableFixed.Reason.layoutKindUnknown,
                    "§1.1 rule 2 (layout_kind_unknown): entry 1's kind 200 is outside the closed set");
        }
        {
            // rule 3 (layout_size_mismatch): a kind 4 leaf admits four bytes
            // and no other width (§1.2's C table).
            final byte[] p = f.clone();
            TableFixed.put32(p, entryAt(1) + 9, 5);
            refusesOneName(p, TableFixed.Reason.layoutSizeMismatch,
                    "§1.1 rule 3 (layout_size_mismatch): entry 1's i32 leaf in five bytes");
        }
        {
            // rule 4 (layout_kind_invalid): parse demands the ROOT be a table
            // — kind 13, here an array's kind 14.
            final byte[] p = f.clone();
            p[entryAt(0) + 8] = 14;
            refusesOneName(p, TableFixed.Reason.layoutKindInvalid,
                    "§1.1 rule 4 (layout_kind_invalid): the root entry an array (kind 14), not a table");
        }
        {
            // rule 5 (layout_tree_unclosed): the root's 19 fields cover the
            // 66 entries behind it exactly; one more child walks past the end.
            final byte[] p = f.clone();
            TableFixed.put32(p, entryAt(0) + 13, TableFixed.get32(p, entryAt(0) + 13) + 1);
            refusesOneName(p, TableFixed.Reason.layoutTreeUnclosed,
                    "§1.1 rule 5 (layout_tree_unclosed): the root claims one child past the entries");
        }
        {
            // rule 6 (layout_record_too_large): the root's size is the record
            // body and must sit inside 65536 (§1.1 rule 6, TableFixed.recordMaxBytes).
            final byte[] p = f.clone();
            TableFixed.put32(p, entryAt(0) + 9, 65537);
            refusesOneName(p, TableFixed.Reason.layoutRecordTooLarge,
                    "§1.1 rule 6 (layout_record_too_large): the root's size 65537 past 65536");
        }
        {
            // rule 7 (layout_too_deep): the smallest chain past the walk bound.
            refusesOneName(fileWithKnownHash(deepLayout()), TableFixed.Reason.layoutTooDeep,
                    "§1.1 rule 7 (layout_too_deep): a single-child chain one past the walk bound");
        }

        System.out.println(red == 0 ? "PASS test/conformance/java/rows R9"
                : "FAIL test/conformance/java/rows R9 (" + red + " red)");
        if (red != 0) { System.exit(1); }
    }
}
