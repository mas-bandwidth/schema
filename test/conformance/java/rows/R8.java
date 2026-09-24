// R8 — A HASH IN NO LINEAGE ENTRY → layout_newer, REPORTING THE FILE'S HASH
// AND NOTHING ELSE (docs/roadmap.sexp java/R8). The law is
// docs/FIXED-FORM-ALGORITHM.md:887: "the hash is in no lineage entry |
// layout_newer | the file's hash, and nothing else". The refusal table (:906)
// pins the answer's shape: `layout_newer` ALSO sets `layout_hash` to THE
// FILE'S hash and nothing else — not this build's constant, not a hash of the
// layout bytes behind it, which no runtime computes (:867).
//
// THE VECTOR is built from the law through the PRODUCTION WRITER, CfgFixed.save,
// whose every byte the law fixes. The header hash is then XORed to a value no
// lineage entry holds — a hash no reader's compile-time constants list. The
// reader must refuse layout_newer and carry THE FILE'S HASH, exactly as given,
// with no counter moving and nothing decoded.
//
// Run from the repository root against the conformance classpath:
//   java -cp build/conformance-java test/conformance/java/rows/R8.java
// exit 0 green, exit 1 red, one printed line per assertion.

import java.nio.charset.StandardCharsets;
import tblv1.CfgFixed;
import tblv1.TableFixed;

public final class R8 {
    private static int failures = 0;

    private static void check(boolean ok, String line) {
        System.out.println((ok ? "pass" : "FAIL") + ": " + line);
        if (!ok) {
            failures++;
        }
    }

    public static void main(String[] args) {
        CfgFixed.Value written = new CfgFixed.Value();
        written.a = 1;
        written.b = 1.0f;
        byte[] name = "r8".getBytes(StandardCharsets.US_ASCII);
        System.arraycopy(name, 0, written.name, 0, name.length);
        written.nameLength = 2;
        written.itemsCount = 0;

        byte[] file = new byte[CfgFixed.measure(1)];
        int saved = CfgFixed.save(new CfgFixed.Value[] { written }, 1, file);
        check(saved == file.length,
                "the production writer wrote the whole file: " + saved + " of " + file.length + " bytes");

        // The file's own hash was the compile-time constant — a known entry.
        // Flip every byte of the hash to a value NO lineage entry holds.
        long given = -1L;
        for (int i = 0; i < 8; i++) {
            file[TableFixed.hashAt + i] ^= (byte) 0xFF;
        }
        given = TableFixed.get64(file, TableFixed.hashAt);
        check(given != CfgFixed.hash,
                "the corrupted hash 0x" + Long.toHexString(given) + "L is not the build's own");

        CfgFixed.Value[] out = { new CfgFixed.Value() };
        int initialA = out[0].a;
        int initialB = Float.floatToRawIntBits(out[0].b);
        int initialNameLength = out[0].nameLength;
        int initialItemsCount = out[0].itemsCount;
        TableFixed.Report r = new TableFixed.Report();
        int n = CfgFixed.load(out, 1, file, TableFixed.plan(1024), new short[1024],
                CfgFixed.image(), r);

        check(n == -1, "a hash in no lineage entry is refused (load returned " + n + ")");
        check(r.refused, "the load refuses");
        check(r.reason == TableFixed.Reason.layoutNewer,
                "the refusal is by the name layoutNewer (got " + r.reason + ")");
        check(!r.malformed, "layoutNewer is a refusal, not malformed");
        check(r.layoutHash == given,
                "the report carries THE FILE'S HASH and nothing else: 0x"
                        + Long.toHexString(given) + "L as given (got 0x"
                        + Long.toHexString(r.layoutHash) + "L)");
        check(r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0 && r.clamped == 0,
                "REFUSE is total: no counter moves");
        check(out[0].a == initialA && Float.floatToRawIntBits(out[0].b) == initialB
                        && out[0].nameLength == initialNameLength
                        && out[0].itemsCount == initialItemsCount,
                "nothing decoded: the destination is untouched (a=" + out[0].a + " b=" + out[0].b + ")");
        check(r.layoutHash != CfgFixed.hash,
                "the report does NOT carry this build's own hash — it carries the file's");

        System.exit(failures == 0 ? 0 : 1);
    }
}