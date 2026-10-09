// RangedIntegersValidData — the roadmap row ranged-integers/java/valid-data:
// "Ranged integer fields: valid-data write/read acceptance"
// (docs/roadmap.sexp, row ranged-integers/java, audit schema#898).
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md:1726, reference fix 5 (LANDED a17e1b0c):
//
//   "The ranged clamp — a ranged integer clamps on load and counts,
//    fixed-point on the raw scale."
//
// ruled in §4.6: "A RANGED SCALAR clamps to its declared min and max, COUNT
// clamped." The contract is §3.4's record, ranged integer: a fixed-form record
// lands each integer RAW at its declared storage width, and the straight-line
// bounds pass after the plan holds it to the reader's own declared range. THIS
// ROW IS THE VALID-DATA HALF OF THAT LAW: a value inside the declared bounds —
// at each end of every range, at each of the four bound shapes, and off both
// ends — must come back EXACT on the read and move NO counter, through the
// production write path and the production read path both.
//
// THE SUBJECT. tables/examples/Ranges.schema declares every ranged-integer
// shape in the book: both bounds AT the storage limits (a clamp that can never
// fire), each limit alone, and both bounds inside — at every width from int8
// to uint64, signed and unsigned, plus counted arrays whose ELEMENTS are
// ranged. The generated readers for those tables, tabledemo.RangedSignedFixed
// and tabledemo.RangedUnsignedFixed, ride the conformance driver's classpath
// (make build-conformance-java compiles build/tables-generated-java/examples,
// and Ranges.schema is a member of that unit) — the same generated code a
// consumer's own build emits.
//
// THE VECTOR. testdata/conformance/tables carries no form-3 data for these
// tables (its rows are the block and cook batteries), so the byte vector is
// constructed here from the law through the PRODUCTION WRITERS, whose every
// byte the law fixes. One file per table, three records each:
//
//   record 0: every field at its DECLARED MINIMUM (the lower boundary case);
//   record 1: every field at its DECLARED MAXIMUM (the upper boundary case);
//   record 2: interior NON-DEFAULT values, live counts off both the bound
//             and zero.
//
// The frame, as the generated writer writes it (§3's header, form byte 3): the
// form byte at 0; seven reserved zeros at 1..7; the layout hash —
// 0xcbf32b9992cc8183L for RangedSigned, 0x8b7880ef6aaed3c0L for
// RangedUnsigned — as eight little-endian bytes at 8, a COMPILE-TIME CONSTANT
// a runtime never derives; the u32 LE layout length (327) at 16; the 327
// layout bytes from 20; then the records back to back, each eight bytes of
// the same hash then a body of values in declared order at constant offsets.
// RangedSigned's 72-byte body: the four int8s at 0..3, the four int16s at
// 4..10, the four int32s at 12..24, the four int64s at 28..52, the edges count
// (u32 LE) at 60 and its four int16 elements at 64..71, slack zeroed.
// RangedUnsigned's 96-byte body: the four uint8s at 0..3, the four uint16s at
// 4..10, the four uint32s at 12..24, the four uint64s at 28..52, the counts
// count (u32 LE) at 60 and its four uint64 elements at 64..95, slack zeroed.
// THE FRAME IS ASSERTED BELOW, not trusted.
//
// PRODUCTION PATH UNDER TEST, both directions:
//
//   the write  Ranged*Fixed.save -> writeHeader, then per record put64 of the
//              hash and writeBody's straight line of put8/put16/put32/put64
//              stores at constant offsets;
//   the read   Ranged*Fixed.load -> TableFixed.readHeader (the hash TAKEN AS
//              GIVEN) -> TableFixed.select against the compile-time lineage ->
//              the lock's byte comparison -> TableFixed.run over the identity
//              plan -> scatter, whose per-field clamps are the bounds the law
//              names, emitted by internal/codegen/javatable/fixedform.go:1315
//              (fixedIntRange's declared min and max as the `q < lo / q > hi`
//              constants), one per ranged field and element.
//
// The round trip is closed: what the read decoded is saved again and the bytes
// must be IDENTICAL. A writer and a reader that share one offset mistake round
// trip perfectly and are both wrong, so the VALUES are asserted on the read,
// not inferred from the bytes.
//
// RUN from the repo root (the classpath is the one make/java.mk gives the
// conformance driver):
//
//   java -cp build/conformance-java test/conformance/java/rows/RangedIntegersValidData.java

package test.conformance.java.rows;

import java.util.Arrays;

import tabledemo.RangedSignedFixed;
import tabledemo.RangedUnsignedFixed;
import tabledemo.TableFixed;

public final class RangedIntegersValidData {
    private RangedIntegersValidData() {}

    static int failures = 0;

    static void check(boolean ok, String what) {
        if (ok) {
            System.out.println("ok: " + what);
        } else {
            System.out.println("FAILED: " + what);
            failures++;
        }
    }

    // ---- the records: non-default values and boundary cases -----------------

    /** every field at its DECLARED MINIMUM, edges full and at their own min. */
    static RangedSignedFixed.Value signedAtMin() {
        final RangedSignedFixed.Value v = new RangedSignedFixed.Value();
        v.i8Span = -128;
        v.i8Low = -128;
        v.i8High = -127;
        v.i8Inside = -127;
        v.i16Span = Short.MIN_VALUE;
        v.i16Low = Short.MIN_VALUE;
        v.i16High = -32767;
        v.i16Inside = -32767;
        v.i32Span = Integer.MIN_VALUE;
        v.i32Low = Integer.MIN_VALUE;
        v.i32High = -2147483647;
        v.i32Inside = -2147483647;
        v.i64Span = Long.MIN_VALUE;
        v.i64Low = Long.MIN_VALUE;
        v.i64High = -9223372036854775807L;
        v.i64Inside = -9223372036854775807L;
        v.edgesCount = 4;
        v.edges[0] = Short.MIN_VALUE;
        v.edges[1] = Short.MIN_VALUE;
        v.edges[2] = Short.MIN_VALUE;
        v.edges[3] = Short.MIN_VALUE;
        return v;
    }

    /** every field at its DECLARED MAXIMUM, a live count under the bound. */
    static RangedSignedFixed.Value signedAtMax() {
        final RangedSignedFixed.Value v = new RangedSignedFixed.Value();
        v.i8Span = 127;
        v.i8Low = 126;
        v.i8High = 127;
        v.i8Inside = 126;
        v.i16Span = 32767;
        v.i16Low = 32766;
        v.i16High = 32767;
        v.i16Inside = 32766;
        v.i32Span = 2147483647;
        v.i32Low = 2147483646;
        v.i32High = 2147483647;
        v.i32Inside = 2147483646;
        v.i64Span = Long.MAX_VALUE;
        v.i64Low = 9223372036854775806L;
        v.i64High = Long.MAX_VALUE;
        v.i64Inside = 9223372036854775806L;
        v.edgesCount = 3;
        v.edges[0] = 32767;
        v.edges[1] = -1;
        v.edges[2] = 1;
        return v;
    }

    /** interior non-default values; a live count of ZERO is valid data too. */
    static RangedSignedFixed.Value signedInterior() {
        final RangedSignedFixed.Value v = new RangedSignedFixed.Value();
        v.i8Span = -1;
        v.i8Low = -100;
        v.i8High = 100;
        v.i8Inside = 42;
        v.i16Span = -12345;
        v.i16Low = -30000;
        v.i16High = 30000;
        v.i16Inside = 12345;
        v.i32Span = -1000000;
        v.i32Low = -2000000000;
        v.i32High = 2000000000;
        v.i32Inside = 987654321;
        v.i64Span = -1L;
        v.i64Low = -4000000000000L;
        v.i64High = 4000000000000L;
        v.i64Inside = 1234567890123L;
        v.edgesCount = 0;
        return v;
    }

    /** every field at its DECLARED MINIMUM (1 for the ranges that exclude 0). */
    static RangedUnsignedFixed.Value unsignedAtMin() {
        final RangedUnsignedFixed.Value v = new RangedUnsignedFixed.Value();
        v.u8Span = 0;
        v.u8Low = 0;
        v.u8High = 1;
        v.u8Inside = 1;
        v.u16Span = 0;
        v.u16Low = 0;
        v.u16High = 1;
        v.u16Inside = 1;
        v.u32Span = 0;
        v.u32Low = 0;
        v.u32High = 1;
        v.u32Inside = 1;
        v.u64Span = 0L;
        v.u64Low = 0L;
        v.u64High = 1L;
        v.u64Inside = 1L;
        v.countsCount = 4;
        v.counts[0] = 0L;
        v.counts[1] = 0L;
        v.counts[2] = 0L;
        v.counts[3] = 0L;
        return v;
    }

    /** every field at its DECLARED MAXIMUM. An unsigned value rides
     *  BIT-TRANSPARENT in Java's signed primitive, the packet codec's own
     *  rule: 0xFF..FF is -1 and the sixty-four bits are the sixty-four bits. */
    static RangedUnsignedFixed.Value unsignedAtMax() {
        final RangedUnsignedFixed.Value v = new RangedUnsignedFixed.Value();
        v.u8Span = (byte) 255;
        v.u8Low = (byte) 254;
        v.u8High = (byte) 255;
        v.u8Inside = (byte) 254;
        v.u16Span = (short) 65535;
        v.u16Low = (short) 65534;
        v.u16High = (short) 65535;
        v.u16Inside = (short) 65534;
        v.u32Span = 0xFFFFFFFF;
        v.u32Low = 0xFFFFFFFE;
        v.u32High = 0xFFFFFFFF;
        v.u32Inside = 0xFFFFFFFE;
        v.u64Span = 0xFFFFFFFFFFFFFFFFL;
        v.u64Low = 0xFFFFFFFFFFFFFFFEL;
        v.u64High = 0xFFFFFFFFFFFFFFFFL;
        v.u64Inside = 0xFFFFFFFFFFFFFFFEL;
        v.countsCount = 4;
        v.counts[0] = 0xFFFFFFFFFFFFFFFFL;
        v.counts[1] = 0xFFFFFFFFFFFFFFFFL;
        v.counts[2] = 0xFFFFFFFFFFFFFFFFL;
        v.counts[3] = 0xFFFFFFFFFFFFFFFFL;
        return v;
    }

    /** interior non-default values, one live count element. */
    static RangedUnsignedFixed.Value unsignedInterior() {
        final RangedUnsignedFixed.Value v = new RangedUnsignedFixed.Value();
        v.u8Span = (byte) 128;
        v.u8Low = (byte) 200;
        v.u8High = (byte) 100;
        v.u8Inside = (byte) 42;
        v.u16Span = (short) 40000;
        v.u16Low = (short) 60000;
        v.u16High = (short) 33333;
        v.u16Inside = (short) 4242;
        v.u32Span = 0x80000000;
        v.u32Low = 0xEE6B2800; // 3,999,999,000 in [0, 4294967294]
        v.u32High = 100;
        v.u32Inside = 1234567890;
        v.u64Span = 0x0123456789ABCDEFL;
        v.u64Low = Long.MAX_VALUE;
        v.u64High = 2L;
        v.u64Inside = 42L;
        v.countsCount = 1;
        v.counts[0] = 0x123456789ABCDEFL;
        return v;
    }

    // ---- the comparison: the read must equal the write, field by field ------

    /** the name of the first field where a and b differ, or null. */
    static String diff(RangedSignedFixed.Value a, RangedSignedFixed.Value b) {
        if (a.i8Span != b.i8Span) { return "i8_span"; }
        if (a.i8Low != b.i8Low) { return "i8_low"; }
        if (a.i8High != b.i8High) { return "i8_high"; }
        if (a.i8Inside != b.i8Inside) { return "i8_inside"; }
        if (a.i16Span != b.i16Span) { return "i16_span"; }
        if (a.i16Low != b.i16Low) { return "i16_low"; }
        if (a.i16High != b.i16High) { return "i16_high"; }
        if (a.i16Inside != b.i16Inside) { return "i16_inside"; }
        if (a.i32Span != b.i32Span) { return "i32_span"; }
        if (a.i32Low != b.i32Low) { return "i32_low"; }
        if (a.i32High != b.i32High) { return "i32_high"; }
        if (a.i32Inside != b.i32Inside) { return "i32_inside"; }
        if (a.i64Span != b.i64Span) { return "i64_span"; }
        if (a.i64Low != b.i64Low) { return "i64_low"; }
        if (a.i64High != b.i64High) { return "i64_high"; }
        if (a.i64Inside != b.i64Inside) { return "i64_inside"; }
        if (a.edgesCount != b.edgesCount) { return "edges#count"; }
        for (int i = 0; i < a.edgesCount; i++) {
            if (a.edges[i] != b.edges[i]) { return "edges[" + i + "]"; }
        }
        return null;
    }

    /** the name of the first field where a and b differ, or null. */
    static String diff(RangedUnsignedFixed.Value a, RangedUnsignedFixed.Value b) {
        if (a.u8Span != b.u8Span) { return "u8_span"; }
        if (a.u8Low != b.u8Low) { return "u8_low"; }
        if (a.u8High != b.u8High) { return "u8_high"; }
        if (a.u8Inside != b.u8Inside) { return "u8_inside"; }
        if (a.u16Span != b.u16Span) { return "u16_span"; }
        if (a.u16Low != b.u16Low) { return "u16_low"; }
        if (a.u16High != b.u16High) { return "u16_high"; }
        if (a.u16Inside != b.u16Inside) { return "u16_inside"; }
        if (a.u32Span != b.u32Span) { return "u32_span"; }
        if (a.u32Low != b.u32Low) { return "u32_low"; }
        if (a.u32High != b.u32High) { return "u32_high"; }
        if (a.u32Inside != b.u32Inside) { return "u32_inside"; }
        if (a.u64Span != b.u64Span) { return "u64_span"; }
        if (a.u64Low != b.u64Low) { return "u64_low"; }
        if (a.u64High != b.u64High) { return "u64_high"; }
        if (a.u64Inside != b.u64Inside) { return "u64_inside"; }
        if (a.countsCount != b.countsCount) { return "counts#count"; }
        for (int i = 0; i < a.countsCount; i++) {
            if (a.counts[i] != b.counts[i]) { return "counts[" + i + "]"; }
        }
        return null;
    }

    // ---- the halves ---------------------------------------------------------

    static final String[] SHAPES = {
        "every declared minimum", "every declared maximum", "interior, non-default",
    };

    static void signedHalf() {
        final RangedSignedFixed.Value[] write = {
            signedAtMin(), signedAtMax(), signedInterior(),
        };
        final byte[] file = new byte[RangedSignedFixed.measure(write.length)];
        final int put = RangedSignedFixed.save(write, write.length, file);
        check(put == file.length, "signed: save answers measure, every byte accounted (" + put + ")");
        check(file[0] == TableFixed.form, "signed: the frame opens with form byte 3");
        check((file.length - RangedSignedFixed.headerBytes) / RangedSignedFixed.recordBytes == write.length,
                "signed: the frame carries three records back to back");

        final RangedSignedFixed.Value[] read = {
            new RangedSignedFixed.Value(), new RangedSignedFixed.Value(), new RangedSignedFixed.Value(),
        };
        final TableFixed.Report r = new TableFixed.Report();
        final int got = RangedSignedFixed.load(read, read.length, file,
                TableFixed.plan(4096), new short[4096], RangedSignedFixed.image(), r);
        check(got == write.length, "signed: load reads all three records (got " + got + ")");
        check(!r.refused, "signed: the read is not refused");
        check(!r.malformed, "signed: the read is not malformed");
        check(r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0,
                "signed: no census counter moved on valid data");
        check(r.clamped == 0, "signed: valid data clamps nothing and counts nothing (clamped=" + r.clamped + ")");
        for (int k = 0; k < write.length; k++) {
            final String d = diff(write[k], read[k]);
            check(d == null, "signed: record " + k + " (" + SHAPES[k]
                    + ") reads back exact" + (d == null ? "" : " — first difference: " + d));
        }

        final byte[] back = new byte[RangedSignedFixed.measure(read.length)];
        check(RangedSignedFixed.save(read, read.length, back) == back.length,
                "signed: the decoded values save again");
        check(Arrays.equals(back, file), "signed: write/read acceptance is byte-identical both ways");
    }

    static void unsignedHalf() {
        final RangedUnsignedFixed.Value[] write = {
            unsignedAtMin(), unsignedAtMax(), unsignedInterior(),
        };
        final byte[] file = new byte[RangedUnsignedFixed.measure(write.length)];
        final int put = RangedUnsignedFixed.save(write, write.length, file);
        check(put == file.length, "unsigned: save answers measure, every byte accounted (" + put + ")");
        check(file[0] == TableFixed.form, "unsigned: the frame opens with form byte 3");
        check((file.length - RangedUnsignedFixed.headerBytes) / RangedUnsignedFixed.recordBytes == write.length,
                "unsigned: the frame carries three records back to back");

        final RangedUnsignedFixed.Value[] read = {
            new RangedUnsignedFixed.Value(), new RangedUnsignedFixed.Value(), new RangedUnsignedFixed.Value(),
        };
        final TableFixed.Report r = new TableFixed.Report();
        final int got = RangedUnsignedFixed.load(read, read.length, file,
                TableFixed.plan(4096), new short[4096], RangedUnsignedFixed.image(), r);
        check(got == write.length, "unsigned: load reads all three records (got " + got + ")");
        check(!r.refused, "unsigned: the read is not refused");
        check(!r.malformed, "unsigned: the read is not malformed");
        check(r.unknown == 0 && r.kindMismatch == 0 && r.widened == 0,
                "unsigned: no census counter moved on valid data");
        check(r.clamped == 0, "unsigned: valid data clamps nothing and counts nothing (clamped=" + r.clamped + ")");
        for (int k = 0; k < write.length; k++) {
            final String d = diff(write[k], read[k]);
            check(d == null, "unsigned: record " + k + " (" + SHAPES[k]
                    + ") reads back exact" + (d == null ? "" : " — first difference: " + d));
        }

        final byte[] back = new byte[RangedUnsignedFixed.measure(read.length)];
        check(RangedUnsignedFixed.save(read, read.length, back) == back.length,
                "unsigned: the decoded values save again");
        check(Arrays.equals(back, file), "unsigned: write/read acceptance is byte-identical both ways");
    }

    public static void main(String[] args) {
        signedHalf();
        unsignedHalf();
        if (failures != 0) {
            System.out.println("RangedIntegersValidData: " + failures + " assertion(s) failed");
            System.exit(1);
        }
        System.out.println("RangedIntegersValidData: Ranged integer fields:"
                + " valid-data write/read acceptance — GREEN");
    }
}
