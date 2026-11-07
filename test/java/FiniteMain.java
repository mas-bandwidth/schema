// The Java packet harness's finite-float leg (SPEC §4.3, serialize format
// 2.0): a bare float32 or float64 is finite, so a NaN (any sign, any payload)
// and both infinities are refused on write (the checkWrite contract, -ea) and
// refused on read, on the raw bits, as non_finite_float. -0 and the
// subnormals ride bit exact. Compressed floats keep their clamp and refuse a
// non-finite write the same way.
//
// test/java/Finite.schema is the shape; make/java.mk generates it and runs
// this twice, with -ea and without — the read refusals and the bit-exact
// round trips hold in both, the write refusals in the checked twin only (the
// release writer trusts by design, as it does for an out-of-range integer).
// No time, no sockets, no files: crafted bytes in memory only.

import packetfinite.Finite;

public final class FiniteMain {
    static boolean failed = false;

    static void check(boolean ok, String what) {
        if (!ok) {
            System.out.println("FAILED: " + what);
            failed = true;
        }
    }

    static final boolean assertsEnabled = detectAsserts();

    static boolean detectAsserts() {
        boolean on = false;
        assert on = true;
        return on;
    }

    static void expectAssert(Runnable fn, String what) {
        if (!assertsEnabled) {
            return;
        }
        try {
            fn.run();
            check(false, what);
        } catch (AssertionError e) {
            // the contract fired — expected
        }
    }

    // the non-finite patterns: quiet NaN, -NaN, a payload (signalling) NaN,
    // and both infinities
    static final int[] F32_BAD = {0x7fc00000, 0xffc00000, 0x7f800001, 0x7fa5a5a5, 0x7f800000, 0xff800000};
    static final String[] F32_BAD_NAMES = {"NaN", "-NaN", "sNaN payload 1", "NaN payload", "+inf", "-inf"};
    static final long[] F64_BAD = {
        0x7ff8000000000000L, 0xfff8000000000000L, 0x7ff0000000000001L, 0x7ff5a5a5a5a5a5a5L,
        0x7ff0000000000000L, 0xfff0000000000000L,
    };
    static final String[] F64_BAD_NAMES = F32_BAD_NAMES;

    // the finite edges that must ride bit exact: -0, the smallest and largest
    // subnormals, and the largest finite magnitude (the exponent one below
    // all-ones, so a too-wide mask would refuse it)
    static final int[] F32_EXACT = {0x80000000, 0x00000001, 0x807fffff, 0x7f7fffff, 0xff7fffff};
    static final long[] F64_EXACT = {
        0x8000000000000000L, 0x0000000000000001L, 0x800fffffffffffffL, 0x7fefffffffffffffL, 0xffefffffffffffffL,
    };

    static final byte[] buf = new byte[64];

    // setBits overwrites n bits at bit position pos with value — the packet
    // wire's bit order: bit k of the stream is bit k % 8 of byte k / 8
    static void setBits(byte[] data, int pos, int n, long value) {
        for (int i = 0; i < n; i++) {
            final int bit = pos + i;
            final int mask = 1 << (bit & 7);
            if (((value >>> i) & 1) != 0) {
                data[bit >>> 3] |= (byte) mask;
            } else {
                data[bit >>> 3] &= (byte) ~mask;
            }
        }
    }

    static long getBits(byte[] data, int pos, int n) {
        long value = 0;
        for (int i = 0; i < n; i++) {
            final int bit = pos + i;
            if ((data[bit >>> 3] & (1 << (bit & 7))) != 0) {
                value |= 1L << i;
            }
        }
        return value;
    }

    // Scalars: f32 at bit 0 (32 bits), f64 at bit 32 (64 bits)
    static Finite.Scalars scalars(float f32, double f64) {
        final Finite.Scalars s = new Finite.Scalars();
        s.f32 = f32;
        s.f64 = f64;
        return s;
    }

    // Elements: a32 at bits 0 and 32, the 2-bit count at 64, a64 at 66 and 130
    static Finite.Elements elements(float a32, double a64) {
        final Finite.Elements e = new Finite.Elements();
        e.a32[0] = 1.5f;
        e.a32[1] = a32;
        e.a64Count = 2;
        e.a64[0] = -2.25;
        e.a64[1] = a64;
        return e;
    }

    static void testWriteRefusals() {
        for (int i = 0; i < F32_BAD.length; i++) {
            final float bad = Float.intBitsToFloat(F32_BAD[i]);
            final String name = F32_BAD_NAMES[i];
            expectAssert(() -> Finite.writeScalars(scalars(bad, 1.0), buf),
                    "a float32 field holding " + name + " must trip the writer contract");
            expectAssert(() -> Finite.writeElements(elements(bad, 1.0), buf),
                    "a float32 array element holding " + name + " must trip the writer contract");
            final Finite.Quantized q = new Finite.Quantized();
            q.q = bad;
            expectAssert(() -> Finite.writeQuantized(q, buf),
                    "a compressed float32 holding " + name + " must trip the writer contract");
        }
        for (int i = 0; i < F64_BAD.length; i++) {
            final double bad = Double.longBitsToDouble(F64_BAD[i]);
            final String name = F64_BAD_NAMES[i];
            expectAssert(() -> Finite.writeScalars(scalars(1.0f, bad), buf),
                    "a float64 field holding " + name + " must trip the writer contract");
            expectAssert(() -> Finite.writeElements(elements(1.0f, bad), buf),
                    "a float64 array element holding " + name + " must trip the writer contract");
        }
    }

    // crafted bytes: a valid wire with one float's bits replaced by a
    // non-finite pattern; the read must refuse it as non_finite_float
    static void testReadRefusals() {
        final Finite.Scalars outS = new Finite.Scalars();
        final Finite.Elements outE = new Finite.Elements();
        final int sBits = Finite.writeScalarsBits(scalars(0.5f, 0.25), buf, 0);
        final byte[] sGood = java.util.Arrays.copyOf(buf, buf.length);
        check(Finite.readScalars(outS, sGood, sBits), "the untouched Scalars wire reads");
        final int eBits = Finite.writeElementsBits(elements(0.5f, 0.25), buf, 0);
        final byte[] eGood = java.util.Arrays.copyOf(buf, buf.length);
        check(Finite.readElements(outE, eGood, eBits), "the untouched Elements wire reads");

        for (int i = 0; i < F32_BAD.length; i++) {
            final String name = F32_BAD_NAMES[i];
            final byte[] s = sGood.clone();
            setBits(s, 0, 32, F32_BAD[i] & 0xffffffffL);
            check(!Finite.readScalars(outS, s, sBits), "non_finite_float read: a float32 field of " + name);
            final byte[] e = eGood.clone();
            setBits(e, 32, 32, F32_BAD[i] & 0xffffffffL);
            check(!Finite.readElements(outE, e, eBits), "non_finite_float read: a float32 element of " + name);
        }
        for (int i = 0; i < F64_BAD.length; i++) {
            final String name = F64_BAD_NAMES[i];
            final byte[] s = sGood.clone();
            setBits(s, 32, 64, F64_BAD[i]);
            check(!Finite.readScalars(outS, s, sBits), "non_finite_float read: a float64 field of " + name);
            final byte[] e = eGood.clone();
            setBits(e, 130, 64, F64_BAD[i]);
            check(!Finite.readElements(outE, e, eBits), "non_finite_float read: a float64 element of " + name);
        }
    }

    // -0, the subnormals and the largest finite magnitudes: written as their
    // raw bits, read back as the same raw bits, in both widths, field and element
    static void testBitExact() {
        for (int i = 0; i < F32_EXACT.length; i++) {
            final long f64Bits = F64_EXACT[i];
            final float f32 = Float.intBitsToFloat(F32_EXACT[i]);
            final double f64 = Double.longBitsToDouble(f64Bits);
            final String what = String.format("0x%08x / 0x%016x", F32_EXACT[i], f64Bits);

            final int sBits = Finite.writeScalarsBits(scalars(f32, f64), buf, 0);
            check(getBits(buf, 0, 32) == (F32_EXACT[i] & 0xffffffffL), "float32 field rides bit exact: " + what);
            check(getBits(buf, 32, 64) == f64Bits, "float64 field rides bit exact: " + what);
            final Finite.Scalars s = new Finite.Scalars();
            check(Finite.readScalars(s, buf, sBits), "a finite edge reads: " + what);
            check(Float.floatToRawIntBits(s.f32) == F32_EXACT[i]
                    && Double.doubleToRawLongBits(s.f64) == f64Bits,
                    "a finite edge round-trips bit exact as a field: " + what);

            final int eBits = Finite.writeElementsBits(elements(f32, f64), buf, 0);
            check(getBits(buf, 32, 32) == (F32_EXACT[i] & 0xffffffffL), "float32 element rides bit exact: " + what);
            check(getBits(buf, 130, 64) == f64Bits, "float64 element rides bit exact: " + what);
            final Finite.Elements e = new Finite.Elements();
            check(Finite.readElements(e, buf, eBits), "a finite edge reads as an element: " + what);
            check(Float.floatToRawIntBits(e.a32[1]) == F32_EXACT[i]
                    && Double.doubleToRawLongBits(e.a64[1]) == f64Bits,
                    "a finite edge round-trips bit exact as an element: " + what);
        }
    }

    // the compressed float's finite domain is unchanged: out-of-range values
    // still clamp to the ends
    static void testQuantizedClamp() {
        final Finite.Quantized q = new Finite.Quantized();
        final Finite.Quantized out = new Finite.Quantized();
        q.q = 5.0f;
        int bits = Finite.writeQuantizedBits(q, buf, 0);
        check(Finite.readQuantized(out, buf, bits) && out.q == 1.0f, "a compressed float above max clamps to max");
        q.q = -5.0f;
        bits = Finite.writeQuantizedBits(q, buf, 0);
        check(Finite.readQuantized(out, buf, bits) && out.q == -1.0f, "a compressed float below min clamps to min");
    }

    public static void main(String[] args) {
        testWriteRefusals();
        testReadRefusals();
        testBitExact();
        testQuantizedClamp();
        if (failed) {
            System.exit(1);
        }
        System.out.println("OK");
    }
}
