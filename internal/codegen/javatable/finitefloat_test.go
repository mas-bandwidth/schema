package javatable

// THE FINITE FLOAT RULE on the Java leg (docs/SPEC-TABLES.md §3.3; Glenn,
// 2026-10-27: "Tables go finite-only too, same rule as packets. NaN/inf
// refused on write and read, -0 and subnormals kept"). One generated unit and
// one Java driver hold every table surface this leg emits to the rule, each
// check on a line of its own:
//
//   - every read refuses a NaN or an infinity from the raw bits, the tolerant
//     load included, naming <Owner>.<field>, and lands nothing: the variable
//     form (scalars, the f32-into-f64 rung, list and keyed elements, a nested
//     type, a union arm, the retaining read), the message batch (count 0, the
//     bodies reset) and the fixed form (a compressed float refused before its
//     bounds, never clamped);
//   - a retained unknown kind 10/11 entry is tested like a known one (ruling
//     3), and the plain read, which retains nothing, skips it;
//   - every writer refuses one: variable and retaining save, message save and
//     measure, fixed save, and the cook's plan, whose measure reason names it;
//   - a block or cook record's float accessor refuses one by name and never
//     returns it (ruling 2): Java's accessors are static over (data, at) with
//     no view object to latch on, so the refusal is the thrown
//     TableBytes.NonFiniteFloat naming the field;
//   - -0 and a subnormal ride bit for bit, a -0 under a +0 default included.
//
// TestJavaFiniteFloatNegativeControl removes each check in turn from the
// generated code and requires the probe to go red on exactly that check's
// lines and nowhere else.

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const finiteSchema = `package probe

type Inner { g float32 }

enum Axis { X, Y }

union Pick
{
    a float32
    b float64
}

table Root
{
    f     float32
    d     float64
    w     float64
    list  [..4]float32
    keyed [Axis]float64
    inner Inner
    pick  Pick
    tail  int32
}

fixed table Fx
{
    f     float32
    d     float64
    c     float32 | min = -2, max = 2, resolution = 0.01
    arr   [2]float32
    inner Inner
    pick  Pick
    tail  int32
}

fixed table Acc
{
    f   float32
    d   float64
    arr [2]float32
}
`

// finiteDriver is the probe. ANNOUNCE is the unit's message announcement.
const finiteDriver = `package probe;

import java.util.Arrays;

public final class Driver {
    static void say(String label, boolean ok, String detail) {
        System.out.println("CHECK " + label + (ok ? " ok" : " FAIL " + detail));
    }

    interface Check { String run() throws Exception; }

    // one check: "" is a pass, anything else is the failure's detail, and an
    // exception is a failure of that check alone
    static void check(String label, Check c) {
        try {
            String why = c.run();
            say(label, why.isEmpty(), why);
        } catch (Throwable e) {
            say(label, false, "threw " + e);
        }
    }

    static byte[] fromHex(String h) {
        byte[] b = new byte[h.length() / 2];
        for (int i = 0; i < b.length; i++) { b[i] = (byte) Integer.parseInt(h.substring(2 * i, 2 * i + 2), 16); }
        return b;
    }

    // ---- the variable form: wires built by hand, raw bits where they lie ----

    static TableVariable.Writer begin() {
        TableVariable.Writer w = new TableVariable.Writer(new byte[4096], new TableVariable.Ids(64));
        w.put8(1);
        return w;
    }

    static byte[] end(TableVariable.Writer w) {
        w.putLeb(w.ref(RootVariable.F_tail)); w.put8(4); w.put32(7);
        w.put8(0);
        w.trailer();
        return Arrays.copyOf(w.buf, w.off);
    }

    static byte[] scalar(long id, int kind, long bits) {
        TableVariable.Writer w = begin();
        w.putLeb(w.ref(id)); w.put8(kind);
        if (kind == 10) { w.put32((int) bits); } else { w.put64(bits); }
        return end(w);
    }

    static byte[] body(long id, int kind, TableVariable.Writer body) {
        TableVariable.Writer w = begin();
        w.putLeb(w.ref(id)); w.put8(kind);
        w.putLeb(body.off); w.putBytes(body.buf, 0, body.off);
        return end(w);
    }

    static String refusesVariable(byte[] wire, String field) {
        TableVariable.Report r = new TableVariable.Report();
        RootVariable.Value v = RootVariable.load(wire, r);
        if (v == null && r.refused && ("non_finite_float: " + field).equals(r.reason)) { return ""; }
        return "loaded=" + (v != null) + " refused=" + r.refused + " reason=" + r.reason;
    }

    static String refusesRetaining(byte[] wire, String field) {
        TableVariable.Report r = new TableVariable.Report();
        RootVariable.Value v = RootRetain.loadRetain(wire, new TableRetain(1 << 16, 1 << 13), r);
        if (v == null && r.refused && ("non_finite_float: " + field).equals(r.reason) && r.retained == 0) { return ""; }
        return "loaded=" + (v != null) + " refused=" + r.refused + " reason=" + r.reason + " retained=" + r.retained;
    }

    // a SENTINEL is a finite value with a pattern found once in the saved
    // wire, overwritten with a non-finite one where it lies
    static final int S32 = 0x3f9e0419;
    static final long S64 = 0x3ff3c0c9539b8887L;

    static byte[] plant(byte[] wire, long sentinel, int width, long bits) {
        int found = -1;
        for (int i = 0; i + width <= wire.length; i++) {
            boolean same = true;
            for (int k = 0; k < width && same; k++) { same = wire[i + k] == (byte) (sentinel >>> (8 * k)); }
            if (same) {
                if (found >= 0) { throw new IllegalStateException("the sentinel lies twice"); }
                found = i;
            }
        }
        if (found < 0) { throw new IllegalStateException("the sentinel is not in the wire"); }
        byte[] out = wire.clone();
        for (int k = 0; k < width; k++) { out[found + k] = (byte) (bits >>> (8 * k)); }
        return out;
    }

    static String ridesVariable(RootVariable.Value v, java.util.function.Function<RootVariable.Value, Long> bits, long want) {
        byte[] wire = RootVariable.save(v);
        if (wire == null) { return "the save refused a finite value"; }
        TableVariable.Report r = new TableVariable.Report();
        RootVariable.Value back = RootVariable.load(wire, r);
        if (back == null || r.refused || r.malformed) { return "the load refused: " + r.reason; }
        long got = bits.apply(back);
        return got == want ? "" : String.format("landed 0x%x, want 0x%x", got, want);
    }

    // ---- the message and fixed forms: the plant is three finite saves ----
    //
    // base ^ a ^ b, where the three values differ only in the field under test
    // and a and b differ in one exponent bit, is base with that bit flipped:
    // 1.0 with the top exponent bit set is +infinity. It needs no knowledge of
    // where or in what bit order the field lies.

    interface Set { void set(FxFixed.Value v, long bits); }

    static final long[] F32 = { 0x3f800000L, 0x00800000L, 0x40800000L };
    static final long[] F64 = { 0x3ff0000000000000L, 0x0010000000000000L, 0x4010000000000000L };

    static FxFixed.Value fx(Set s, long bits) {
        FxFixed.Value v = new FxFixed.Value();
        v.tail = 7;
        if (s != null) { s.set(v, bits); }
        return v;
    }

    static byte[] xor3(byte[] x, byte[] y, byte[] z) {
        if (x == null || y == null || z == null || x.length != y.length || x.length != z.length) {
            throw new IllegalStateException("the three saves are not one layout");
        }
        byte[] out = new byte[x.length];
        for (int i = 0; i < out.length; i++) { out[i] = (byte) (x[i] ^ y[i] ^ z[i]); }
        return out;
    }

    static byte[] message(FxFixed.Value v) {
        byte[] buf = new byte[4096];
        int n = FxMessage.saveMessages(new FxFixed.Value[] { v }, 1, buf, new TableVocabulary.Report());
        return n < 0 ? null : Arrays.copyOf(buf, n);
    }

    static byte[] fixed(FxFixed.Value v) {
        byte[] buf = new byte[FxFixed.measure(1)];
        return FxFixed.save(new FxFixed.Value[] { v }, 1, buf) < 0 ? null : buf;
    }

    static TableVocabulary vocab() {
        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(4096);
        if (!TableVocabulary.announceRead(vocab, fromHex("ANNOUNCE"), new TableVocabulary.Report())) {
            throw new IllegalStateException("the announcement did not read");
        }
        return vocab;
    }

    static String refusesMessage(Set s, long[] bits, String field) {
        byte[] wire = xor3(message(fx(s, bits[0])), message(fx(s, bits[1])), message(fx(s, bits[2])));
        FxFixed.Value[] back = { new FxFixed.Value(), new FxFixed.Value() };
        TableVocabulary.Report r = new TableVocabulary.Report();
        int n = FxMessage.loadMessages(back, 2, vocab(), wire, r);
        if (n == 0 && r.refused && ("non_finite_float: " + field).equals(r.reason) && back[0].tail == 0) { return ""; }
        return "n=" + n + " refused=" + r.refused + " reason=" + r.reason + " tail=" + back[0].tail;
    }

    static String refusesFixed(Set s, long[] bits, String field) {
        byte[] file = xor3(fixed(fx(s, bits[0])), fixed(fx(s, bits[1])), fixed(fx(s, bits[2])));
        FxFixed.Value[] back = { new FxFixed.Value() };
        TableFixed.Report r = new TableFixed.Report();
        int n = FxFixed.load(back, 1, file, TableFixed.plan(1 << 14), new short[1 << 14], FxFixed.image(), r);
        if (n == -1 && r.refused && r.reason == TableFixed.Reason.nonFiniteFloat && field.equals(r.nonFiniteFloat) && r.clamped == 0) { return ""; }
        return "n=" + n + " refused=" + r.refused + " reason=" + r.reason + " field=" + r.nonFiniteFloat + " clamped=" + r.clamped;
    }

    static String refusesWrite(Set s, long bits) {
        FxFixed.Value v = fx(s, bits);
        FxFixed.Value[] one = { v };
        int measured = FxMessage.measureMessages(one, 1, new TableVocabulary.Report());
        int saved = FxMessage.saveMessages(one, 1, new byte[4096], new TableVocabulary.Report());
        int fixedSaved = FxFixed.save(one, 1, new byte[FxFixed.measure(1)]);
        if (measured == -1 && saved == -1 && fixedSaved == -1) { return ""; }
        return "message measure=" + measured + " save=" + saved + " fixed save=" + fixedSaved;
    }

    // ---- the accessors: a record image with the bits where the field lies ----

    static String accessorRefuses(String field, Check read) {
        try {
            read.run();
            return "the accessor answered a value";
        } catch (TableBytes.NonFiniteFloat e) {
            return e.field.equals(field) && e.getMessage().equals("non_finite_float: " + field) ? "" : "named " + e.field;
        } catch (Exception e) {
            return "threw " + e;
        }
    }

    static byte[] image(int size, int at, long bits, int width) {
        byte[] b = new byte[size];
        TableBytes.put(b, at, bits, width, false);
        return b;
    }

    public static void main(String[] args) {
        long[] nans32 = { 0x7fc00000L, 0x7f800001L, 0xffa5a5a5L, 0x7f800000L, 0xff800000L };
        String[] names32 = { "quiet-nan", "signalling-nan", "negative-nan", "plus-inf", "minus-inf" };
        for (int i = 0; i < nans32.length; i++) {
            final long bits = nans32[i];
            check("variable-read:f32-" + names32[i], () -> refusesVariable(scalar(RootVariable.F_f, 10, bits), "Root.f"));
        }
        check("variable-read:f64-nan", () -> refusesVariable(scalar(RootVariable.F_d, 11, 0x7ff8000000000000L), "Root.d"));
        check("variable-read:f64-minus-inf", () -> refusesVariable(scalar(RootVariable.F_d, 11, 0xfff0000000000000L), "Root.d"));
        check("variable-read:f32-into-f64", () -> refusesVariable(scalar(RootVariable.F_w, 10, 0x7fa12345L), "Root.w"));
        check("variable-read:list-element", () -> {
            TableVariable.Writer b = begin().sub(new byte[256]);
            b.put8(10); b.putLeb(2); b.put32(0x3f800000); b.put32(0x7f800000);
            return refusesVariable(body(RootVariable.F_list, 14, b), "Root.list");
        });
        check("variable-read:nested-type", () -> {
            RootVariable.Value v = new RootVariable.Value();
            v.inner.g = Float.intBitsToFloat(S32);
            return refusesVariable(plant(RootVariable.save(v), S32, 4, 0x7fc00000L), "Inner.g");
        });
        check("variable-read:union-arm", () -> {
            RootVariable.Value v = new RootVariable.Value();
            v.pick.tag = 2;
            v.pick.b = Double.longBitsToDouble(S64);
            return refusesVariable(plant(RootVariable.save(v), S64, 8, 0x7ff0000000000000L), "Pick.b");
        });
        check("variable-read:keyed-element", () -> {
            RootVariable.Value v = new RootVariable.Value();
            v.keyed.set(1, Double.longBitsToDouble(S64));
            return refusesVariable(plant(RootVariable.save(v), S64, 8, 0xfff8000000000000L), "Root.keyed");
        });
        check("variable-read:retaining-known-field", () -> refusesRetaining(scalar(RootVariable.F_f, 10, 0x7fc00000L), "Root.f"));

        // RULING 3: a retained unknown float is tested like a known one; the
        // plain read retains nothing and skips it as it skips any unknown
        check("variable-retain-unknown:kind-10", () -> refusesRetaining(scalar(0x1234L, 10, 0x7fc00000L), "Root.0x0000000000001234"));
        check("variable-retain-unknown:kind-11", () -> refusesRetaining(scalar(0xfeedfacecafebeefL, 11, 0xfff0000000000000L), "Root.0xfeedfacecafebeef"));
        check("variable-retain-exact:plain-read-skips", () -> {
            TableVariable.Report r = new TableVariable.Report();
            RootVariable.Value v = RootVariable.load(scalar(0x1234L, 10, 0x7fc00000L), r);
            return v != null && !r.refused && r.unknown == 1 && v.tail == 7 ? "" : "loaded=" + (v != null) + " refused=" + r.refused + " unknown=" + r.unknown;
        });
        check("variable-retain-exact:finite-retained", () -> {
            TableVariable.Report r = new TableVariable.Report();
            RootVariable.Value v = RootRetain.loadRetain(scalar(0x1234L, 10, 0x3f800000L), new TableRetain(1 << 16, 1 << 13), r);
            return v != null && !r.refused && r.retained == 1 ? "" : "loaded=" + (v != null) + " refused=" + r.refused + " retained=" + r.retained;
        });

        // EVERY VARIABLE WRITER REFUSES ONE
        check("variable-write:scalar", () -> { RootVariable.Value v = new RootVariable.Value(); v.f = Float.NaN; return RootVariable.save(v) == null ? "" : "saved"; });
        check("variable-write:f64", () -> { RootVariable.Value v = new RootVariable.Value(); v.d = Double.NEGATIVE_INFINITY; return RootVariable.save(v) == null ? "" : "saved"; });
        check("variable-write:list-element", () -> { RootVariable.Value v = new RootVariable.Value(); v.list.add(1.0f); v.list.add(Float.POSITIVE_INFINITY); return RootVariable.save(v) == null ? "" : "saved"; });
        check("variable-write:keyed-element", () -> { RootVariable.Value v = new RootVariable.Value(); v.keyed.set(0, Double.NaN); return RootVariable.save(v) == null ? "" : "saved"; });
        check("variable-write:nested-type", () -> { RootVariable.Value v = new RootVariable.Value(); v.inner.g = Float.NaN; return RootVariable.save(v) == null ? "" : "saved"; });
        check("variable-write:union-arm", () -> { RootVariable.Value v = new RootVariable.Value(); v.pick.tag = 1; v.pick.a = Float.NaN; return RootVariable.save(v) == null ? "" : "saved"; });
        check("variable-write:retaining-save", () -> {
            RootVariable.Value v = new RootVariable.Value();
            v.d = Double.POSITIVE_INFINITY;
            TableRetain store = new TableRetain(1 << 16, 1 << 13);
            byte[] saved = RootRetain.saveRetain(v, store, new TableVariable.Report());
            long measured = RootRetain.measureRetain(v, store);
            return saved == null && measured == -1 ? "" : "saved=" + (saved != null) + " measured=" + measured;
        });

        // -0 AND A SUBNORMAL RIDE BIT FOR BIT (the 10-15 rule): a -0 under a
        // +0 default is not elided
        check("variable-exact:minus-zero", () -> { RootVariable.Value v = new RootVariable.Value(); v.f = -0.0f; return ridesVariable(v, b -> Float.floatToRawIntBits(b.f) & 0xffffffffL, 0x80000000L); });
        check("variable-exact:minus-zero-f64", () -> { RootVariable.Value v = new RootVariable.Value(); v.d = -0.0; return ridesVariable(v, b -> Double.doubleToRawLongBits(b.d), 0x8000000000000000L); });
        check("variable-exact:subnormal", () -> { RootVariable.Value v = new RootVariable.Value(); v.f = Float.intBitsToFloat(1); return ridesVariable(v, b -> Float.floatToRawIntBits(b.f) & 0xffffffffL, 1L); });
        check("variable-exact:keyed-minus-zero", () -> { RootVariable.Value v = new RootVariable.Value(); v.keyed.set(1, -0.0); return ridesVariable(v, b -> Double.doubleToRawLongBits(b.keyed.get(1)), 0x8000000000000000L); });
        check("variable-exact:retaining-minus-zero", () -> {
            RootVariable.Value v = new RootVariable.Value();
            v.f = -0.0f;
            TableRetain store = new TableRetain(1 << 16, 1 << 13);
            byte[] wire = RootRetain.saveRetain(v, store, new TableVariable.Report());
            TableVariable.Report r = new TableVariable.Report();
            RootVariable.Value back = wire == null ? null : RootRetain.loadRetain(wire, store, r);
            return back != null && Float.floatToRawIntBits(back.f) == 0x80000000 ? "" : "the -0 did not ride the retaining pair";
        });

        // THE MESSAGE FORM: a batch holding one is refused whole
        check("message-read:f32", () -> refusesMessage((v, b) -> v.f = Float.intBitsToFloat((int) b), F32, "Fx.f"));
        check("message-read:f64", () -> refusesMessage((v, b) -> v.d = Double.longBitsToDouble(b), F64, "Fx.d"));
        check("message-read:array-element", () -> refusesMessage((v, b) -> v.arr[1] = Float.intBitsToFloat((int) b), F32, "Fx.arr"));
        check("message-read:nested-type", () -> refusesMessage((v, b) -> v.inner.g = Float.intBitsToFloat((int) b), F32, "Inner.g"));
        check("message-read:union-arm", () -> refusesMessage((v, b) -> { v.pick.type = PickFixed.b; v.pick.b = Double.longBitsToDouble(b); }, F64, "Pick.b"));
        check("message-exact:minus-zero", () -> {
            FxFixed.Value v = fx((x, b) -> x.f = -0.0f, 0);
            FxFixed.Value[] back = { new FxFixed.Value() };
            TableVocabulary.Report r = new TableVocabulary.Report();
            int n = FxMessage.loadMessages(back, 1, vocab(), message(v), r);
            return n == 1 && !r.refused && Float.floatToRawIntBits(back[0].f) == 0x80000000 ? "" : "n=" + n + " bits=" + Integer.toHexString(Float.floatToRawIntBits(back[0].f));
        });

        // THE FIXED FORM: a compressed float is refused before its bounds
        check("fixed-read:f32", () -> refusesFixed((v, b) -> v.f = Float.intBitsToFloat((int) b), F32, "Fx.f"));
        check("fixed-read:f64", () -> refusesFixed((v, b) -> v.d = Double.longBitsToDouble(b), F64, "Fx.d"));
        check("fixed-read:compressed", () -> refusesFixed((v, b) -> v.c = Float.intBitsToFloat((int) b), F32, "Fx.c"));
        check("fixed-read:array-element", () -> refusesFixed((v, b) -> v.arr[1] = Float.intBitsToFloat((int) b), F32, "Fx.arr"));
        check("fixed-read:nested-type", () -> refusesFixed((v, b) -> v.inner.g = Float.intBitsToFloat((int) b), F32, "Inner.g"));
        check("fixed-read:union-arm", () -> refusesFixed((v, b) -> { v.pick.type = PickFixed.b; v.pick.b = Double.longBitsToDouble(b); }, F64, "Pick.b"));
        check("fixed-exact:minus-zero", () -> {
            FxFixed.Value[] back = { new FxFixed.Value() };
            TableFixed.Report r = new TableFixed.Report();
            int n = FxFixed.load(back, 1, fixed(fx((x, b) -> x.f = -0.0f, 0)), TableFixed.plan(1 << 14), new short[1 << 14], FxFixed.image(), r);
            return n == 1 && !r.refused && Float.floatToRawIntBits(back[0].f) == 0x80000000 ? "" : "n=" + n;
        });

        // THE MESSAGE AND FIXED WRITERS REFUSE ONE, the quantized float too
        check("form-write:f32", () -> refusesWrite((v, b) -> v.f = Float.NaN, 0));
        check("form-write:f64", () -> refusesWrite((v, b) -> v.d = Double.POSITIVE_INFINITY, 0));
        check("form-write:compressed", () -> refusesWrite((v, b) -> v.c = Float.NEGATIVE_INFINITY, 0));
        check("form-write:array-element", () -> refusesWrite((v, b) -> v.arr[0] = Float.NaN, 0));
        check("form-write:nested-type", () -> refusesWrite((v, b) -> v.inner.g = Float.NaN, 0));
        check("form-write:union-arm", () -> refusesWrite((v, b) -> { v.pick.type = PickFixed.a; v.pick.a = Float.NaN; }, 0));

        // RULING 2: the record accessors refuse by name and never answer one
        check("accessor:row-f32", () -> { byte[] b = image(AccRow.size, AccRow.offsets[0], 0x7fc00000L, 4); return accessorRefuses("Acc.f", () -> String.valueOf(AccRow.f(b, 0))); });
        check("accessor:row-f64", () -> { byte[] b = image(AccRow.size, AccRow.offsets[1], 0xfff0000000000000L, 8); return accessorRefuses("Acc.d", () -> String.valueOf(AccRow.d(b, 0))); });
        check("accessor:row-element", () -> { byte[] b = image(AccRow.size, AccRow.offsets[2] + 4, 0x7f800000L, 4); return accessorRefuses("Acc.arr", () -> String.valueOf(AccRow.arr(b, 0, 1))); });
        check("accessor:block-projection", () -> { byte[] b = image(AccBlock.projectionSize, AccBlock.projectionOffsets[0], 0xff800001L, 4); return accessorRefuses("Acc.f", () -> String.valueOf(AccBlock.f(b, 0))); });
        check("accessor-exact:minus-zero", () -> {
            byte[] b = image(AccRow.size, AccRow.offsets[0], 0x80000000L, 4);
            return Float.floatToRawIntBits(AccRow.f(b, 0)) == 0x80000000 ? "" : "the -0 did not come back";
        });

        // THE COOK'S WRITER refuses one, and its measure reason names it
        check("cook-write:refused", () -> {
            AccVariable.Value v = new AccVariable.Value();
            v.arr.add(0.0f);
            v.arr.add(Float.NaN);
            byte[] buf = new byte[4096];
            AccCook.MeasureReason why = AccCook.cookMeasureReason(v);
            return AccCook.cookMeasure(v) == -1 && !AccCook.cookFrom(v, buf, TableCookWrite.LITTLE) && why.bytes() == -1 && why.reason().equals("non_finite_float") ? "" : "reason=" + why.reason();
        });
        check("cook-write:minus-zero", () -> {
            AccVariable.Value v = new AccVariable.Value();
            v.d = -0.0;
            v.arr.add(0.0f);
            v.arr.add(0.0f);
            long size = AccCook.cookMeasure(v);
            byte[] buf = new byte[(int) Math.max(size, 1)];
            if (size < 0 || !AccCook.cookFrom(v, buf, TableCookWrite.LITTLE)) { return "the finite cook refused"; }
            AccCook cook = AccCook.open(buf);
            return cook != null && Double.doubleToRawLongBits(AccRow.d(cook.data(), cook.root())) == 0x8000000000000000L ? "" : "the -0 did not ride the cook";
        });
    }
}
`

// finiteLabels is every check the probe makes, each under its group: the
// negative control removes one group's check and wants exactly that group red.
var finiteLabels = []string{
	"variable-read:f32-quiet-nan", "variable-read:f32-signalling-nan", "variable-read:f32-negative-nan",
	"variable-read:f32-plus-inf", "variable-read:f32-minus-inf", "variable-read:f64-nan", "variable-read:f64-minus-inf",
	"variable-read:f32-into-f64", "variable-read:list-element", "variable-read:nested-type", "variable-read:union-arm",
	"variable-read:keyed-element", "variable-read:retaining-known-field",
	"variable-retain-unknown:kind-10", "variable-retain-unknown:kind-11", "variable-retain-exact:plain-read-skips",
	"variable-retain-exact:finite-retained",
	"variable-write:scalar", "variable-write:f64", "variable-write:list-element", "variable-write:keyed-element",
	"variable-write:nested-type", "variable-write:union-arm", "variable-write:retaining-save",
	"variable-exact:minus-zero", "variable-exact:minus-zero-f64", "variable-exact:subnormal",
	"variable-exact:keyed-minus-zero", "variable-exact:retaining-minus-zero",
	"message-read:f32", "message-read:f64", "message-read:array-element", "message-read:nested-type",
	"message-read:union-arm", "message-exact:minus-zero",
	"fixed-read:f32", "fixed-read:f64", "fixed-read:compressed", "fixed-read:array-element", "fixed-read:nested-type",
	"fixed-read:union-arm", "fixed-exact:minus-zero",
	"form-write:f32", "form-write:f64", "form-write:compressed", "form-write:array-element", "form-write:nested-type",
	"form-write:union-arm",
	"accessor:row-f32", "accessor:row-f64", "accessor:row-element", "accessor:block-projection", "accessor-exact:minus-zero",
	"cook-write:refused", "cook-write:minus-zero",
}

// finiteSabotage removes one check from the generated code: in every file
// whose name ends with one of the suffixes, each old text becomes the new.
type finiteSabotage struct {
	name     string
	suffixes []string
	edits    [][2]string
	red      string // the one group that must go red
}

var finiteSabotages = []finiteSabotage{
	// the variable form's read test: a mask no bits can equal
	{"variable read", []string{"Variable.java", "Retain.java"}, [][2]string{
		{"(fbits & 0x7f800000) == 0x7f800000", "(fbits & 0x7f800000) == 0x7f800001 /* SABOTAGED */"},
		{"(fbits & 0x7ff0000000000000L) == 0x7ff0000000000000L", "(fbits & 0x7ff0000000000000L) == 0x7ff0000000000001L /* SABOTAGED */"},
	}, "variable-read"},
	// the retaining read's test on an unknown entry's payload
	{"retained unknown", []string{"Retain.java"}, [][2]string{
		{"(get32(r.buf, pStart) & 0x7f800000) == 0x7f800000", "(get32(r.buf, pStart) & 0x7f800000) == 0x7f800001 /* SABOTAGED */"},
		{"(get64(r.buf, pStart) & 0x7ff0000000000000L) == 0x7ff0000000000000L", "(get64(r.buf, pStart) & 0x7ff0000000000000L) == 0x7ff0000000000001L /* SABOTAGED */"},
	}, "variable-retain-unknown"},
	// the message batch's read test
	{"message read", []string{"TableMessage.java"}, [][2]string{
		{"(raw & 0x7f800000) == 0x7f800000", "(raw & 0x7f800000) == 0x7f800001 /* SABOTAGED */"},
		{"(raw & 0x7ff0000000000000L) == 0x7ff0000000000000L", "(raw & 0x7ff0000000000000L) == 0x7ff0000000000001L /* SABOTAGED */"},
	}, "message-read"},
	// the fixed form's scatter test, before the bounds
	{"fixed read", []string{"Fixed.java"}, [][2]string{
		{"(fbits & 0x7f800000) == 0x7f800000", "(fbits & 0x7f800000) == 0x7f800001 /* SABOTAGED */"},
		{"(fbits & 0x7ff0000000000000L) == 0x7ff0000000000000L", "(fbits & 0x7ff0000000000000L) == 0x7ff0000000000001L /* SABOTAGED */"},
	}, "fixed-read"},
	// the variable writer's mark
	{"variable write", []string{"TableVariable.java"}, [][2]string{
		{"ids.nonFinite = true;", "ids.nonFinite = false; // SABOTAGED"},
	}, "variable-write"},
	// the record accessor's refusal
	{"accessor", []string{"TableBytes.java"}, [][2]string{
		{"throw new NonFiniteFloat(field);", "field = null; // SABOTAGED"},
	}, "accessor"},
}

// runFiniteProbe generates the unit, applies the sabotage (none for the gate),
// compiles it with the driver and runs it, answering each check's verdict:
// "" for a pass and the failure's detail otherwise.
func runFiniteProbe(t *testing.T, sabotage *finiteSabotage) map[string]string {
	t.Helper()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	javac, java := javaTools(t)
	u := unitFrom(t, finiteSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if _, ok := files[name]; !ok {
			files[name] = data
		}
	}
	if sabotage != nil {
		for _, e := range sabotage.edits {
			patched := 0
			for name, data := range files {
				for _, suffix := range sabotage.suffixes {
					if strings.HasSuffix(name, suffix) && strings.Contains(string(data), e[0]) {
						files[name] = []byte(strings.ReplaceAll(string(data), e[0], e[1]))
						patched++
						break
					}
				}
			}
			if patched == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage patched nothing: %q is in no %v file", sabotage.name, e[0], sabotage.suffixes)
			}
		}
	}
	files["Driver.java"] = []byte(strings.ReplaceAll(finiteDriver, "ANNOUNCE", hex.EncodeToString(ir.TableAnnouncement(u))))
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-Xlint:all", "-Werror", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-ea", "-cp", dir, u.Package+".Driver").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(line, "CHECK ")
		if !ok {
			continue
		}
		label, verdict, _ := strings.Cut(rest, " ")
		if verdict == "ok" {
			got[label] = ""
		} else {
			got[label] = strings.TrimPrefix(verdict, "FAIL ")
		}
	}
	for _, label := range finiteLabels {
		if _, ok := got[label]; !ok {
			t.Fatalf("the probe made no %s check\n%s", label, out)
		}
	}
	if len(got) != len(finiteLabels) {
		t.Fatalf("the probe made %d checks, finiteLabels names %d\n%s", len(got), len(finiteLabels), out)
	}
	return got
}

// TestJavaFiniteFloats is the gate: every check passes.
func TestJavaFiniteFloats(t *testing.T) {
	t.Parallel()
	got := runFiniteProbe(t, nil)
	for _, label := range finiteLabels {
		if why := got[label]; why != "" {
			t.Errorf("finite %s: %s", label, why)
		}
	}
}

// TestJavaFiniteFloatNegativeControl removes each check in turn and wants the
// probe red on exactly that check's group: a control that reds nothing is not
// a gate, and one that reds another group cannot say which check it watches.
func TestJavaFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	for i := range finiteSabotages {
		s := &finiteSabotages[i]
		t.Run(strings.ReplaceAll(s.name, " ", "-"), func(t *testing.T) {
			t.Parallel()
			got := runFiniteProbe(t, s)
			var red, stray []string
			for _, label := range finiteLabels {
				group, _, _ := strings.Cut(label, ":")
				switch {
				case group == s.red && got[label] == "":
					t.Errorf("NEGATIVE CONTROL FAILED: with the %s check gone, %s stayed green", s.name, label)
				case group == s.red:
					red = append(red, label)
				case got[label] != "":
					stray = append(stray, label+" ("+got[label]+")")
				}
			}
			if len(stray) > 0 {
				t.Errorf("NEGATIVE CONTROL FAILED: the %s sabotage also redded checks outside %s: %s", s.name, s.red, strings.Join(stray, "; "))
			}
			t.Logf("negative control: with the %s check gone, exactly %d %s checks go red", s.name, len(red), s.red)
		})
	}
}
