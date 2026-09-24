// VD_signed_integers.java — cell signed-integers/java/valid-data:
// "Signed integers: 8, 16, 32 and 64 bits: valid-data write/read acceptance"
// (docs/roadmap.sexp:4293, item signed-integers/java/valid-data, contract
// "§3.4 record, int8..int64").
//
// THE LAW. The card's mechanical law pointer docs/FIXED-FORM-ALGORITHM.md:1081
// is the PORTING step "The fixtures FIRST, and RED" — about the §5.7 versioning
// corpus — verified there and set aside; it is not this item's law. The
// governing sentences for the title words are:
//
//   docs/FIXED-FORM-ALGORITHM.md:163 — "Every field rides as its declared
//     storage image, little-endian, at its declared storage width, nothing
//     padded between fields."
//   docs/FIXED-FORM-ALGORITHM.md:171 — "| `int8`..`uint64`, and a RANGED
//     integer | the declared storage width | the bounds do not ride |"
//   docs/SPEC-TABLES.md:3726 — "| `2`–`5` i8/i16/i32/i64 | 1/2/4/8 bytes,
//     two's complement |"
//   docs/SPEC-TABLES.md:6721 — "| `int8`/`uint8` … `int64`/`uint64` | 1, 2,
//     4, 8 — the declared storage width |"
//   docs/SPEC-TABLES.md:6411-6412 (§3.4) — "A FIXED-TABLE RECORD IS AN
//     EIGHT-BYTE HASH OF THE WRITER'S LAYOUT, AND THEN THE VALUES IN
//     DECLARED ORDER, EVERY FIELD AT ITS BOUND."
//
// So valid-data acceptance for the four signed widths is: values set at each
// width's declared edges write as two's-complement little-endian at the
// field's declared offset, read back EXACTLY (clamped == 0 on values inside
// the declared range), the record body is exactly the declared widths — the
// BOUNDS never ride — and a save-load-save round trip reproduces the same
// bytes.
//
// TWO SPELLINGS, ONE STORAGE IMAGE (ALGORITHM:171 groups them in one row):
//   * RANGED — tabledemo.RangedSigned (tables/examples/Ranges.schema:27-52)
//     carries int8, int16, int32 and int64, each four ways: both bounds at
//     the storage limits, each limit alone, one value off each end. Every
//     field below is set to an edge of ITS OWN declared range.
//   * BARE — tabledemo.ProfileConfig carries bare `tilt int8`,
//     `heading int16`, `timestamp int64` (Tables.schema:96-98), asserted at
//     the storage limits. No bare (unranged) int32 exists on this classpath's
//     corpus; bare-vs-ranged is the BOUNDS, which never ride, so the
//     four-width law is RangedSigned's and the bare spelling rides along for
//     8/16/64.
//
// THE VECTOR IS CONSTRUCTED HERE, derivation inline below: the tree carries no
// committed form-3 bytes for either root (build/fixedform-corpus is written at
// build time and is absent on this checkout; testdata/wire/tables and the
// conformance MANIFEST carry neither root), so the record bytes are
// hand-derived from the law — declared order, declared widths, two's
// complement little-endian, nothing padded — and held against the PRODUCTION
// writer's output byte for byte. The hand readers/writers below never call
// TableFixed.get*/put*: a broken production codec cannot agree with itself
// into a green here.
//
// PRODUCTION PATH UNDER TEST: tabledemo.RangedSignedFixed.save/load and
// tabledemo.ProfileConfigFixed.save/load — the generated fixed-form writer and
// reader on the conformance driver's classpath (make/java.mk's
// build-conformance-java compiles build/tables-generated-java/examples into
// build/conformance-java beside test/conformance/java/src/Driver.java).
//
// RUN from the repository root:
//   java -cp build/conformance-java test/conformance/java/rows/VD_signed_integers.java
// exit 0 green, exit 1 red, one printed line per assertion.

import tabledemo.ProfileConfigFixed;
import tabledemo.RangedSignedFixed;
import tabledemo.TableFixed;

public final class VD_signed_integers {
    private VD_signed_integers() {}

    private static int failures = 0;

    private static void check(String label, boolean cond) {
        System.out.println((cond ? "ok " : "FAIL ") + label);
        if (!cond) {
            failures++;
        }
    }

    // ---- hand little-endian, the LAW's encoding and never TableFixed's ----
    // (SPEC-TABLES.md:3726: 1/2/4/8 bytes, two's complement, little-endian
    // per ALGORITHM:163.)

    private static void le16(byte[] b, int at, int v) {
        b[at] = (byte) (v & 0xff);
        b[at + 1] = (byte) ((v >> 8) & 0xff);
    }

    private static void le32(byte[] b, int at, long v) {
        b[at] = (byte) (v & 0xff);
        b[at + 1] = (byte) ((v >> 8) & 0xff);
        b[at + 2] = (byte) ((v >> 16) & 0xff);
        b[at + 3] = (byte) ((v >> 24) & 0xff);
    }

    private static void le64(byte[] b, int at, long v) {
        for (int i = 0; i < 8; i++) {
            b[at + i] = (byte) ((v >> (8 * i)) & 0xff);
        }
    }

    private static long u16(byte[] b, int at) {
        return (b[at] & 0xffL) | ((b[at + 1] & 0xffL) << 8);
    }

    private static long u32(byte[] b, int at) {
        return (b[at] & 0xffL) | ((b[at + 1] & 0xffL) << 8)
                | ((b[at + 2] & 0xffL) << 16) | ((b[at + 3] & 0xffL) << 24);
    }

    private static long u64(byte[] b, int at) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v |= (b[at + i] & 0xffL) << (8 * i);
        }
        return v;
    }

    private static boolean zeros(byte[] b, int from, int to) {
        for (int i = from; i < to; i++) {
            if (b[i] != 0) {
                return false;
            }
        }
        return true;
    }

    // =========================================================================
    // PART A — RangedSigned: int8..int64, ranged spelling, edge values.
    // =========================================================================

    // The value set: every field at an edge of ITS OWN declared range
    // (Ranges.schema:29-51), so a conforming read moves clamped exactly 0.
    private static RangedSignedFixed.Value edgeValue() {
        RangedSignedFixed.Value v = new RangedSignedFixed.Value();
        v.i8Span = -128;            // [-128, 127]  storage minimum
        v.i8Low = -128;             // [-128, 126]  range minimum
        v.i8High = 127;             // [-127, 127]  range maximum = storage maximum
        v.i8Inside = -127;          // [-127, 126]  range minimum
        v.i16Span = -32768;         // [-32768, 32767] storage minimum
        v.i16Low = 32766;           // [-32768, 32766] range maximum
        v.i16High = 32767;          // [-32767, 32767] range maximum = storage maximum
        v.i16Inside = -32767;       // [-32767, 32766] range minimum
        v.i32Span = Integer.MIN_VALUE;
        v.i32Low = Integer.MIN_VALUE;       // [MIN, MAX-1]
        v.i32High = Integer.MAX_VALUE;      // [MIN+1, MAX]
        v.i32Inside = Integer.MIN_VALUE + 1; // [MIN+1, MAX-1] range minimum
        v.i64Span = Long.MIN_VALUE;
        v.i64Low = Long.MIN_VALUE;          // [MIN, MAX-1]
        v.i64High = Long.MAX_VALUE;         // [MIN+1, MAX]
        v.i64Inside = Long.MIN_VALUE + 1;   // [MIN+1, MAX-1] range minimum
        v.edges[0] = Short.MIN_VALUE;
        v.edges[1] = -1;
        v.edges[2] = 0;
        v.edges[3] = Short.MAX_VALUE;
        v.edgesCount = 4;
        return v;
    }

    // The body, hand-derived from the law: declared order (Ranges.schema
    // field order), declared widths (1/2/4/8 bytes), two's-complement
    // little-endian, the array's i32 count then its four int16 elements,
    // NOTHING padded and NO BOUNDS riding (ALGORITHM:163, 171; SPEC-TABLES
    // 3726). Size check is an assertion, not an assumption: 4*1 + 4*2 +
    // 4*4 + 4*8 + 4 + 4*2 = 72.
    private static byte[] expectedRangedSignedBody() {
        byte[] b = new byte[72];
        b[0] = (byte) 0x80;              // i8_span   = -128
        b[1] = (byte) 0x80;              // i8_low    = -128
        b[2] = 0x7f;                     // i8_high   = 127
        b[3] = (byte) 0x81;              // i8_inside = -127
        le16(b, 4, -32768);              // i16_span
        le16(b, 6, 32766);               // i16_low
        le16(b, 8, 32767);               // i16_high
        le16(b, 10, -32767);             // i16_inside
        le32(b, 12, Integer.MIN_VALUE);  // i32_span
        le32(b, 16, Integer.MIN_VALUE);  // i32_low
        le32(b, 20, Integer.MAX_VALUE);  // i32_high
        le32(b, 24, Integer.MIN_VALUE + 1L); // i32_inside
        le64(b, 28, Long.MIN_VALUE);     // i64_span
        le64(b, 36, Long.MIN_VALUE);     // i64_low
        le64(b, 44, Long.MAX_VALUE);     // i64_high
        le64(b, 52, Long.MIN_VALUE + 1); // i64_inside
        le32(b, 60, 4);                  // edges count (i32 LE)
        le16(b, 64, -32768);             // edges[0]
        le16(b, 66, -1);                 // edges[1]
        le16(b, 68, 0);                  // edges[2]
        le16(b, 70, 32767);              // edges[3]
        return b;
    }

    private static void checkFrame(String tag, byte[] file, long wantHash,
            byte[] wantLayout, int wantHeaderBytes) {
        check(tag + ": form byte 3 at offset 0 (SPEC-TABLES 6417)",
                file.length >= 20 && file[0] == 3);
        check(tag + ": the seven reserved bytes 1..7 are zero", zeros(file, 1, 8));
        check(tag + ": the u64 LE hash at offset 8 is the handed constant",
                u64(file, 8) == wantHash);
        check(tag + ": the u32 LE layout length at offset 16 matches the layout",
                u32(file, 16) == wantLayout.length);
        boolean layoutOk = 20 + wantLayout.length == wantHeaderBytes;
        for (int i = 0; layoutOk && i < wantLayout.length; i++) {
            layoutOk = file[20 + i] == wantLayout[i];
        }
        check(tag + ": 16 + 4 + layout == headerBytes and the layout bytes ride", layoutOk);
    }

    private static void rangedSigned() {
        final RangedSignedFixed.Value v = edgeValue();

        // C(T) is 72: four int8 + four int16 + four int32 + four int64 + the
        // array's i32 count + four int16 elements — the widths and nothing
        // else, so the declared BOUNDS demonstrably do not ride (ALGORITHM:171).
        check("ranged: bodyBytes == 72 — widths only, bounds never ride",
                RangedSignedFixed.bodyBytes == 72);
        check("ranged: recordBytes == 8 + bodyBytes (the hash then the body)",
                RangedSignedFixed.recordBytes == 8 + RangedSignedFixed.bodyBytes);

        // ---- THE WRITE ----
        final byte[] file = new byte[RangedSignedFixed.measure(1)];
        final int wrote = RangedSignedFixed.save(new RangedSignedFixed.Value[] {v}, 1, file);
        check("ranged: save answers measure(1)", wrote == RangedSignedFixed.measure(1));

        // Frame per §3.4.
        checkFrame("ranged", file, RangedSignedFixed.hash, RangedSignedFixed.layout,
                RangedSignedFixed.headerBytes);

        // The per-record eight-byte hash, then the body at its declared offsets.
        final int bodyAt = RangedSignedFixed.headerBytes + 8;
        check("ranged: the record opens with the same eight-byte hash",
                u64(file, RangedSignedFixed.headerBytes) == RangedSignedFixed.hash);

        // THE WRITE'S BYTES against the hand-derived law vector.
        final byte[] want = expectedRangedSignedBody();
        boolean same = file.length == bodyAt + 72;
        int firstDiff = -1;
        for (int i = 0; same && i < 72; i++) {
            if (file[bodyAt + i] != want[i]) {
                same = false;
                firstDiff = i;
            }
        }
        if (!same) {
            StringBuilder got = new StringBuilder();
            for (int i = Math.max(0, firstDiff); i < Math.min(72, firstDiff + 8); i++) {
                got.append(String.format(" %02x", file[bodyAt + i]));
            }
            StringBuilder exp = new StringBuilder();
            for (int i = Math.max(0, firstDiff); i < Math.min(72, firstDiff + 8); i++) {
                exp.append(String.format(" %02x", want[i]));
            }
            check("ranged: the body is the law's bytes at offset " + firstDiff
                    + " (got:" + got + " want:" + exp + ")", false);
        } else {
            check("ranged: the body is exactly the law's 72 bytes (LE two's complement"
                    + " at declared offsets, bounds absent)", true);
        }

        // ---- THE READ ----
        final RangedSignedFixed.Value[] out = { new RangedSignedFixed.Value() };
        final TableFixed.Report rep = new TableFixed.Report();
        rep.reset();
        final int n = RangedSignedFixed.load(out, 1, file,
                TableFixed.plan(1024), new short[1024], RangedSignedFixed.image(), rep);
        check("ranged: load answers 1", n == 1);
        check("ranged: a clean read is not refused and not malformed",
                !rep.refused && !rep.malformed);
        check("ranged: a value inside every declared range moves no counter"
                        + " (clamped/unknown/kindMismatch/widened all 0)",
                rep.clamped == 0 && rep.unknown == 0
                        && rep.kindMismatch == 0 && rep.widened == 0);

        final RangedSignedFixed.Value got = out[0];
        check("ranged: int8 edges round-trip exactly (span/low/high/inside)",
                got.i8Span == -128 && got.i8Low == -128
                        && got.i8High == 127 && got.i8Inside == -127);
        check("ranged: int16 edges round-trip exactly (span/low/high/inside)",
                got.i16Span == (short) -32768 && got.i16Low == (short) 32766
                        && got.i16High == (short) 32767 && got.i16Inside == (short) -32767);
        check("ranged: int32 edges round-trip exactly (span/low/high/inside)",
                got.i32Span == Integer.MIN_VALUE && got.i32Low == Integer.MIN_VALUE
                        && got.i32High == Integer.MAX_VALUE
                        && got.i32Inside == Integer.MIN_VALUE + 1);
        check("ranged: int64 edges round-trip exactly (span/low/high/inside)",
                got.i64Span == Long.MIN_VALUE && got.i64Low == Long.MIN_VALUE
                        && got.i64High == Long.MAX_VALUE
                        && got.i64Inside == Long.MIN_VALUE + 1);
        check("ranged: the [..4]int16 elements round-trip exactly",
                got.edgesCount == 4 && got.edges[0] == Short.MIN_VALUE
                        && got.edges[1] == -1 && got.edges[2] == 0
                        && got.edges[3] == Short.MAX_VALUE);

        // ---- SAVE AGAIN: the round trip is byte-exact ----
        final byte[] again = new byte[RangedSignedFixed.measure(1)];
        final int wrote2 = RangedSignedFixed.save(out, 1, again);
        check("ranged: the second save fills measure(1) too",
                wrote2 == RangedSignedFixed.measure(1));
        boolean equal = again.length == file.length;
        int diff = -1;
        for (int i = 0; equal && i < file.length; i++) {
            if (again[i] != file[i]) {
                equal = false;
                diff = i;
            }
        }
        check("ranged: save -> load -> save reproduces the file byte for byte"
                + (equal ? "" : " (first differing byte " + diff + ")"), equal);
    }

    // =========================================================================
    // PART B — ProfileConfig: bare (unranged) int8/int16/int64 at the
    // storage limits, two records, one per side.
    // =========================================================================

    private static void bareSigned() {
        final ProfileConfigFixed.Value lo = new ProfileConfigFixed.Value();
        lo.tilt = Byte.MIN_VALUE;           // bare int8  = -128
        lo.heading = Short.MIN_VALUE;       // bare int16 = -32768
        lo.timestamp = Long.MIN_VALUE;      // bare int64 = Long.MIN_VALUE

        final ProfileConfigFixed.Value hi = new ProfileConfigFixed.Value();
        hi.tilt = Byte.MAX_VALUE;           // 127
        hi.heading = Short.MAX_VALUE;       // 32767
        hi.timestamp = Long.MAX_VALUE;

        final ProfileConfigFixed.Value[] vals = { lo, hi };
        final byte[] buf = new byte[ProfileConfigFixed.measure(2)];
        final int wrote = ProfileConfigFixed.save(vals, 2, buf);
        check("bare: save answers measure(2)", wrote == ProfileConfigFixed.measure(2));

        checkFrame("bare", buf, ProfileConfigFixed.hash, ProfileConfigFixed.layout,
                ProfileConfigFixed.headerBytes);

        // Declared offsets inside the body (Tables.schema:96-98 order after
        // name/icon/experience): tilt at 60 (1 byte), heading at 61 (2),
        // timestamp at 63 (8) — LE two's complement (ALGORITHM:163).
        final int r0 = ProfileConfigFixed.headerBytes + 8;
        final int r1 = r0 + ProfileConfigFixed.recordBytes;
        check("bare: record 0 int8 at offset 60 is 0x80 (-128)",
                buf[r0 + 60] == (byte) 0x80);
        check("bare: record 0 int16 at offset 61 is 00 80 LE (-32768)",
                buf[r0 + 61] == 0x00 && buf[r0 + 62] == (byte) 0x80);
        check("bare: record 0 int64 at offset 63 is 00..00 80 LE (Long.MIN_VALUE)",
                u64(buf, r0 + 63) == Long.MIN_VALUE);
        check("bare: record 1 int8 at offset 60 is 0x7f (127)",
                buf[r1 + 60] == 0x7f);
        check("bare: record 1 int16 at offset 61 is ff 7f LE (32767)",
                buf[r1 + 61] == (byte) 0xff && buf[r1 + 62] == 0x7f);
        check("bare: record 1 int64 at offset 63 is ff..ff 7f LE (Long.MAX_VALUE)",
                u64(buf, r1 + 63) == Long.MAX_VALUE);

        // ---- THE READ ----
        final ProfileConfigFixed.Value[] out = {
            new ProfileConfigFixed.Value(), new ProfileConfigFixed.Value() };
        final TableFixed.Report rep = new TableFixed.Report();
        rep.reset();
        final int n = ProfileConfigFixed.load(out, 2, buf,
                TableFixed.plan(1024), new short[1024], ProfileConfigFixed.image(), rep);
        check("bare: load answers 2", n == 2);
        check("bare: a clean read is not refused and not malformed",
                !rep.refused && !rep.malformed);
        check("bare: bare fields carry no range to clamp — every counter 0",
                rep.clamped == 0 && rep.unknown == 0
                        && rep.kindMismatch == 0 && rep.widened == 0);
        check("bare: int8/int16/int64 minimums round-trip exactly",
                out[0].tilt == Byte.MIN_VALUE
                        && out[0].heading == Short.MIN_VALUE
                        && out[0].timestamp == Long.MIN_VALUE);
        check("bare: int8/int16/int64 maximums round-trip exactly",
                out[1].tilt == Byte.MAX_VALUE
                        && out[1].heading == Short.MAX_VALUE
                        && out[1].timestamp == Long.MAX_VALUE);
    }

    public static void main(String[] args) {
        rangedSigned();
        bareSigned();
        if (failures > 0) {
            System.err.println("VD_signed_integers FAIL: " + failures
                    + " assertion(s) failed");
            System.exit(1);
        }
        System.out.println("VD_signed_integers PASS");
    }
}
