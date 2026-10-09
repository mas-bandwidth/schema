// R7 — THE IDENTITY LANE IS AN INDEX COMPARISON, NEVER A RECOMPUTED HASH
// (docs/roadmap.sexp java/R7, audit schema#898). The law is
// docs/FIXED-FORM-ALGORITHM.md:867: "the compiler hands every reader its own
// wire hash and every known hash as CONSTANTS — `R.own_hash` and
// `R.lineage[i]`, laid down by COMPILE — and a runtime NEVER computes a hash
// from layout bytes it holds, not for the IDENTITY LANE (step 8, where the
// selected entry being the reader's own is an INDEX COMPARISON and never a
// recomputation)". The refusal table (:884) pins the answer's shape: a hash in
// no lineage entry is `layout_newer` carrying THE FILE'S HASH, and nothing
// else.
//
// The hurt the law exists for is the Elixir leg (#928): it hashed its own
// layout bytes to find itself in the lineage, matched NOTHING, and took a
// compiled plan on every read of its own files — layout_newer for its own
// files, and a compiled lane where the identity lane was owed. Both faces are
// asserted below, on the production read path.
//
// PRODUCTION PATH UNDER TEST. tblv1.CfgFixed.load, the generated fixed-form
// reader on the conformance driver's classpath (make/java.mk builds it into
// build/conformance-java beside test/conformance/java/src/Driver.java): load
// -> TableFixed.readHeader, which takes the header's hash AS GIVEN (step 4) ->
// TableFixed.select(known, fileHash), the FIRST index with lineage[i] == h
// against the constants COMPILE laid down (step 5) -> the floor (step 6) ->
// the lock's byte comparison (step 7) -> step 8, `if (fileHash != hash)`:
// the selected entry being the reader's own is the comparison against the
// R.own_hash constant, and the identity plan — one move, no parse, no
// compile — is taken; any other hash takes the compiled lane. Nothing on that
// path computes a hash from layout bytes. The reader is driven end to end;
// no helper is the witness.
//
// THE VECTOR. testdata/conformance/tables carries no form-3 data for tblv1
// (the fixed-form corpus is build/fixedform-corpus, written by the C++
// reference), so the vector is constructed here from the law through the
// PRODUCTION WRITER, CfgFixed.save, whose every byte the law fixes: the form
// byte 3 at 0; the seven reserved zeros at 1..7; the hash — the compile-time
// constant itself, since a runtime never derives one — at 8; the u32 layout
// length at 16; the layout bytes from 20; then one record per
// 8 + 250 = 258 bytes, each record opening with the same eight hash bytes.
// The frame those bytes carry is asserted below, not trusted.

import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import tblv1.CfgFixed;
import tblv1.TableFixed;

final class TestRowR7 {
    private static int red = 0;

    private static void check(boolean ok, String what) {
        System.out.println((ok ? "ok: " : "RED: ") + what);
        if (!ok) { red++; }
    }

    /** one Cfg record, values inside every writer-side bound, defaults left
     *  standing everywhere the test does not name. */
    private static CfgFixed.Value one() {
        final CfgFixed.Value v = new CfgFixed.Value();
        v.a = 777;
        v.b = 2.25f;
        final byte[] name = "cfg r7".getBytes(StandardCharsets.US_ASCII);
        System.arraycopy(name, 0, v.name, 0, name.length);
        v.nameLength = name.length;
        v.itemsCount = 2;
        v.items[0] = 7;
        v.items[1] = 99;
        v.grade = 2;
        v.extraPresent = true;
        v.extra.factor = 0.5f;
        v.tierPresent = true;
        v.tier = 42;
        return v;
    }

    /** the read-back must be the value that was written, first difference
     *  named. */
    private static String drift(CfgFixed.Value v) {
        if (v.a != 777) { return "a"; }
        if (Float.floatToRawIntBits(v.b) != Float.floatToRawIntBits(2.25f)) { return "b"; }
        if (v.nameLength != 6) { return "nameLength"; }
        if (!Arrays.equals(Arrays.copyOf(v.name, 6), "cfg r7".getBytes(StandardCharsets.US_ASCII))) {
            return "name";
        }
        if (v.itemsCount != 2 || v.items[0] != 7 || v.items[1] != 99) { return "items"; }
        if (v.grade != 2) { return "grade"; }
        if (!v.extraPresent || Float.floatToRawIntBits(v.extra.factor) != Float.floatToRawIntBits(0.5f)) {
            return "extra";
        }
        if (!v.tierPresent || v.tier != 42) { return "tier"; }
        return "";
    }

    /** the report a read that answers owes: every counter zero, no damage,
     *  no refusal, no layout hash carried. */
    private static boolean clean(tblv1.TableFixed.Report r) {
        return !r.refused && !r.malformed && r.reason == tblv1.TableFixed.Reason.none
                && r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0
                && r.layoutHash == 0;
    }

    private static int load(CfgFixed.Value[] out, byte[] f, int planRoom) {
        final tblv1.TableFixed.Report r = new tblv1.TableFixed.Report();
        return CfgFixed.load(out, out.length, f, TableFixed.plan(planRoom), new short[1024],
                CfgFixed.image(), r);
    }

    private static tblv1.TableFixed.Report report(CfgFixed.Value[] out, byte[] f, int planRoom) {
        final tblv1.TableFixed.Report r = new tblv1.TableFixed.Report();
        CfgFixed.load(out, out.length, f, TableFixed.plan(planRoom), new short[1024],
                CfgFixed.image(), r);
        return r;
    }

    public static void main(String[] args) {
        // THE VECTOR, from the law through the production writer.
        final CfgFixed.Value written = one();
        final byte[] f = new byte[CfgFixed.measure(1)];
        final int saved = CfgFixed.save(new CfgFixed.Value[] { written }, 1, f);
        check(saved == f.length, "the production writer wrote the whole file: " + saved + " of "
                + f.length + " bytes (header " + CfgFixed.headerBytes + " + 1 record of "
                + CfgFixed.recordBytes + ")");

        // THE FRAME THE LAW FIXES, and the constants it is held to. The hash in
        // the file is R.own_hash itself — HANDED to this reader by COMPILE —
        // and the lineage's last entry is the build's own, so the identity lane
        // is reachable (§5.9 #2).
        final int last = CfgFixed.known.length - 1;
        check(f[0] == 3 && f[1] == 0 && f[2] == 0 && f[3] == 0 && f[4] == 0 && f[5] == 0
                && f[6] == 0 && f[7] == 0,
                "the file's frame is the law's: form byte 3 at 0, the seven reserved zeros at 1..7");
        check(TableFixed.get64(f, TableFixed.hashAt) == CfgFixed.hash,
                "the hash at 8 IS the compile-time constant R.own_hash (0x"
                        + Long.toHexString(CfgFixed.hash) + "L), handed to the reader, never derived");
        check(CfgFixed.known[last].hash == CfgFixed.hash,
                "the lineage COMPILE laid down ends on the build's own entry: R.lineage[" + last
                        + "].hash == R.own_hash");
        check(TableFixed.get32(f, 16) == CfgFixed.layout.length
                && Arrays.equals(Arrays.copyOfRange(f, 20, 20 + CfgFixed.layout.length),
                        CfgFixed.known[last].layout),
                "the u32 at 16 is the layout's length and the layout behind it is byte for byte the "
                        + "lock's own bytes, R.lineage[" + last + "]");

        // THE R7 BITE, first face: the build's own file READS, on the identity
        // lane. A runtime that derived its own hash from the layout bytes it
        // holds would fold in no digest, match NOTHING in the lineage, and
        // answer layout_newer for its own files — the Elixir hurt (#928). The
        // census lands zero because the identity plan is the law's own shape,
        // not a compiled one (§5.9 #30).
        final CfgFixed.Value[] out = { new CfgFixed.Value() };
        final int n = load(out, f, 1024);
        final String d = drift(out[0]);
        final tblv1.TableFixed.Report r = report(out, f, 1024);
        check(n == 1 && clean(r) && d.isEmpty(),
                "the build's own file reads on the identity lane: n=" + n + ", the census lands zero, "
                        + "every value round-trips"
                        + (d.isEmpty() && n == 1 && clean(r) ? "" : " — RED: report=" + r + " drift=" + d));

        // THE R7 BITE, second face: the identity lane is ONE entry — one move,
        // no compile — so the same read answers at a caller plan capacity of
        // exactly the identity plan's own. A reader that took a COMPILED plan
        // on every read of its own files would hold lane.count over one entry
        // and refuse plan_too_large here.
        final CfgFixed.Value[] out1 = { new CfgFixed.Value() };
        final int n1 = load(out1, f, CfgFixed.identityPlan().length);
        check(n1 == 1 && !report(out1, f, CfgFixed.identityPlan().length).refused
                && drift(out1[0]).isEmpty(),
                "the own hash resolves to a plan of exactly the identity plan's size ("
                        + CfgFixed.identityPlan().length + " entr"
                        + (CfgFixed.identityPlan().length == 1 ? "y" : "ies")
                        + "): the same file reads at that caller plan capacity, so the lane the own "
                        + "hash takes is the identity lane and never a compiled one");

        // THE HASH WAS TAKEN AS GIVEN, NEVER RECOMPUTED. A header hash no
        // lineage entry holds is layout_newer, and the report carries THE
        // FILE'S HASH — the value the header held, not this build's constant
        // and not a hash of the layout bytes behind it, which no runtime
        // computes.
        final byte[] lying = f.clone();
        for (int i = 0; i < 8; i++) { lying[TableFixed.hashAt + i] ^= (byte) 0xFF; }
        final long given = TableFixed.get64(lying, TableFixed.hashAt);
        final tblv1.TableFixed.Report lr = report(out, lying, 1024);
        check(TableFixed.get64(f, TableFixed.hashAt) == CfgFixed.hash
                && given == (CfgFixed.hash ^ 0xFFFFFFFFFFFFFFFFL) && lr.refused
                && lr.reason == TableFixed.Reason.layoutNewer && lr.layoutHash == given
                && !lr.malformed && lr.unknown == 0 && lr.kindMismatch == 0
                && lr.widened == 0 && lr.clamped == 0,
                "a header hash no lineage entry holds is layout_newer carrying THE FILE'S HASH and "
                        + "nothing else (0x" + Long.toHexString(given)
                        + "L, as given): no counter moves, nothing decodes");

        System.out.println(red == 0 ? "PASS test/conformance/java/rows TestRowR7"
                : "FAIL test/conformance/java/rows TestRowR7 (" + red + " red)");
        if (red != 0) { System.exit(1); }
    }
}
