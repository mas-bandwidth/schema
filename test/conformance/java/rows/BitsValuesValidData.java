// BitsValuesValidData — bits(N) fields: valid-data write/read acceptance
// (schema#876 schema matrix row java/BitsValuesValidData, the cell for the
// roadmap task bits-values/java/valid-data, contract §3.4 record, bits(N)).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:945; docs/SPEC-TABLES.md:10635,
// 16003). A `bits(N)` field's IMPLIED RANGE is `[0, 2^N - 1]` (the elided
// `min=0, max=2^N-1`). A value INSIDE that range, on both write and read,
// lands identically through the production encoder and decoder, with no
// counter moving: `clamped == 0`, `widened == 0`, `unknown == 0`,
// `kindMismatch == 0`, no refusal, no malformed. ACCEPTANCE means the
// round trip carries the value EXACTLY across the form-3 fixed wire.
//
// THE FIXED-FORM UNIT UNDER TEST. tables/examples/Ranges.schema declares
// `RangedWidths` — six `bits(N)` fields, two declared ranges that fit full
// storage (b8, b16, b32, b64) and two widths the column does NOT span
// (b12, b48). The unit is generated into build/tables-generated-java/examples
// (make/java.mk:88) and the conformance classpath (make/java.mk:585) carries
// `tabledemo.RangedWidthsFixed`, the form-3 read/write halves the contract
// points at.
//
// PRODUCTION PATH UNDER TEST (the actual call sites):
//   tabledemo.RangedWidthsFixed.save(values, count, buffer)  <- writer
//   tabledemo.RangedWidthsFixed.load(values, count, file,    <- reader
//                                    plan, remap, image, report)
// Both call sites reach the production runtime (`TableFixed.put32`,
// `TableFixed.get32`, `TableFixed.get64`, `RangedWidthsFixed.scatter`); no
// helper is the witness. The reader is driven end to end, through every
// reading check the form holds: `readHeader` (the framing), `select` (the
// hash taken as given against R.own_hash), the layout byte comparison, the
// prefill, and the scatter over the body.
//
// THE VECTOR. testdata/conformance/tables has no form-3 data for RangedWidths,
// so the bytes are constructed here from the law through the PRODUCTION WRITER
// — six fields, each filled with a UNIQUE VALID value, distinct from the
// declared default of 0 and from one another, so a misrouted field change is
// visible.
//
// RUN, FROM THE REPO ROOT:
//   java -cp build/conformance-java test/conformance/java/rows/BitsValuesValidData.java
// Exit 0 green, exit 1 red, one printed line per assertion.

import tabledemo.RangedWidthsFixed;
import tabledemo.TableFixed;

public final class BitsValuesValidData {
    private BitsValuesValidData() {}

    private static int failures = 0;

    private static void check(boolean ok, String what) {
        System.out.println((ok ? "ok   " : "RED  ") + what);
        if (!ok) { failures++; }
    }

    public static void main(String[] args) {
        // ---- the writer side ----------------------------------------------
        //
        // Six fields, six UNIQUE values, every one inside bits(N)'s implied
        // range [0, 2^N - 1]:
        //   b8  = 0xAB                (max 0xFF)
        //   b16 = 0xCAFE              (max 0xFFFF)
        //   b32 = 0xDEADBEEF          (a max-storable 32-bit pattern)
        //   b64 = 0xDECAFBADDEADBEEF  (max 0xFFFFFFFFFFFFFFFF)
        //   b12 = 0xABC               (= 2748, max 0xFFF)
        //   b48 = 0xC0FFEEDEADBEEF    (max 0xFFFFFFFFFFFF)
        // Each value is also different from the declared default of 0, so a
        // field that failed to write leaves a wrong read-back below.
        final RangedWidthsFixed.Value written = new RangedWidthsFixed.Value();
        written.b8  = 0xAB;
        written.b16 = 0xCAFE;
        written.b32 = 0xDEADBEEF;
        written.b64 = 0xDECAFBADDEADBEEFL;
        written.b12 = 0xABC;
        written.b48 = 0xC0FFEEDEADBEEFL;

        final int need = RangedWidthsFixed.measure(1);
        final byte[] file = new byte[need];
        final int saved = RangedWidthsFixed.save(new RangedWidthsFixed.Value[] { written }, 1, file);
        check(saved == need,
                "the writer fills exactly `measure(N)` bytes: " + saved + " of " + need
                + " bytes (header " + RangedWidthsFixed.headerBytes + " + 1 record of "
                + RangedWidthsFixed.recordBytes + ", with bodyBytes=" + RangedWidthsFixed.bodyBytes + ")");

        // ---- the writer's frame (every byte the form's own writeHeader set) ---
        //
        // file[0] = form byte 3; file[1..7] reserved, zero;
        // file[8..15] the layout hash (R.own_hash, the compile-time constant);
        // file[16..19] the u32 layout length; file[20..] the layout.
        final int recordsAt = RangedWidthsFixed.headerBytes;
        check(file[0] == 3,
                "the writer's frame: form byte 3 at file[0], got 0x" + (file[0] & 0xFF));
        for (int i = 1; i <= 7; i++) {
            if (file[i] != 0) {
                System.out.println("RED   reserved byte file[" + i + "] is not zero (0x"
                        + (file[i] & 0xFF) + ")");
                failures++;
                break;
            }
        }
        if (failures == 0) {
            check(true, "the writer's frame: the seven reserved bytes file[1..7] are zero");
        }
        check(TableFixed.get64(file, TableFixed.hashAt) == RangedWidthsFixed.hash,
                "the hash at 8 IS the compile-time constant R.own_hash (0x"
                        + Long.toHexString(RangedWidthsFixed.hash) + "L), the reader's own");
        check(TableFixed.get32(file, TableFixed.fileHeaderBytes) == RangedWidthsFixed.layout.length,
                "the u32 layout length at 16 is the layout's own length: "
                + TableFixed.get32(file, TableFixed.fileHeaderBytes) + " == "
                + RangedWidthsFixed.layout.length);

        // ---- the reader side ----------------------------------------------
        //
        // The production load path reads through every documented gate; the
        // assertions below state it must come back as one record, refuse
        // nothing, malform nothing and move no counter.
        final RangedWidthsFixed.Value[] readBack = { new RangedWidthsFixed.Value() };
        final TableFixed.Report report = new TableFixed.Report();
        final int n = RangedWidthsFixed.load(readBack, 1, file, RangedWidthsFixed.identityPlan(),
                new short[1024], RangedWidthsFixed.image(), report);

        // The READ itself completed: n == 1 (one record back), no refusal,
        // no malformed data, no layout refusal name carried.
        check(n == 1, "the production load reads back exactly one record (n=" + n + ")");
        check(!report.refused,
                "no refusal, and the file's own hash IS the reader's own (refused=" + report.refused + ")");
        check(!report.malformed,
                "no malformed framing on the writer's bytes (malformed=" + report.malformed + ")");

        // The ACCEPTANCE counters, all of them: §4's ledger, each zero on
        // valid input (a value in [0, 2^N - 1] isAccepted by `clamped` is N's
        // own ceiling, and no widening fires because every bits(N) carries its
        // own storage width).
        check(report.clamped == 0,
                "no `clamped` counter moves: every value is inside bits(N)'s implied [0, 2^N - 1] "
                + "(clamped=" + report.clamped + ")");
        check(report.widened == 0,
                "no `widened` counter moves: every bits(N) carries its own storage width "
                + "(widened=" + report.widened + ")");
        check(report.unknown == 0 && report.kindMismatch == 0,
                "the layout is the reader's own: no `unknown` and no `kind_mismatch` "
                + "(unknown=" + report.unknown + ", kindMismatch=" + report.kindMismatch + ")");

        // ---- the VALUES round-trip ----------------------------------------
        //
        // Every one of the six `bits(N)` fields lands EXACTLY the value the
        // writer set, the byte equality the wire is supposed to carry across
        // save -> load. Each pair is tested at its declared storage width:
        // b8/b16/b32/b12 are u32 lanes, b64/b48 are u64 lanes (see
        // RangedWidthsFixed.writeBody at offsets 0, 4, 8, 20 and 12, 24).
        check(readBack[0].b8  == written.b8,
                "b8 lands what was written: 0x" + Integer.toHexString(written.b8)
                + " (got 0x" + Integer.toHexString(readBack[0].b8) + ")");
        check(readBack[0].b16 == written.b16,
                "b16 lands what was written: 0x" + Integer.toHexString(written.b16)
                + " (got 0x" + Integer.toHexString(readBack[0].b16) + ")");
        check(readBack[0].b32 == written.b32,
                "b32 lands what was written: 0x" + Integer.toHexString(written.b32)
                + " (got 0x" + Integer.toHexString(readBack[0].b32) + ")");
        check(readBack[0].b64 == written.b64,
                "b64 lands what was written: 0x" + Long.toHexString(written.b64)
                + " (got 0x" + Long.toHexString(readBack[0].b64) + ")");
        check(readBack[0].b12 == written.b12,
                "b12 lands what was written: 0x" + Integer.toHexString(written.b12)
                + " (got 0x" + Integer.toHexString(readBack[0].b12) + ")");
        check(readBack[0].b48 == written.b48,
                "b48 lands what was written: 0x" + Long.toHexString(written.b48)
                + " (got 0x" + Long.toHexString(readBack[0].b48) + ")");

        System.out.println(failures == 0
                ? "BitsValuesValidData green: bits(N) fields accept valid data on the Java leg "
                        + "(write/read round-trip, no counter moved)"
                : "BitsValuesValidData RED: " + failures + " assertion(s) failed");
        if (failures != 0) {
            System.exit(1);
        }
    }
}
