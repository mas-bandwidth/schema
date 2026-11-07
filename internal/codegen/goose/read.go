// The packet wire's READ path (docs/PORT-GOOSE.md, THE CARD MAP,
// schema-goose-type-wire-read): every field in wire order, refused — never
// clamped — when the wire carries what the schema does not allow (a value in
// a range's bit headroom, a const or reserved mismatch, malformed text), with
// the reason latched on the stream through serialize_fail. A failed runtime
// read has latched its own reason already, and the read returns false.
package goose

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const failRange = "return serialize_fail(s, SERIALIZE_ERROR_VALUE_OUT_OF_RANGE);"

func (w *wgen) emitReadField(f *ir.Field, ind string) error {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array == ir.ArrayNone {
		return w.emitReadScalar(f, name, name+"Length", ind)
	}
	limit := fmt.Sprint(f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		// the count is refused above its bound before any element rides
		lo, hi := big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)
		w.emitReadRanged(lo, hi, "i32", name+"Count", ind)
		limit = name + "Count"
	}
	i := w.tmp("ix")
	w.Pf("%sfor %s in %s {\n", ind, i, limit)
	if err := w.emitReadScalar(f, name+"["+i+"]", name+"Length["+i+"]", ind+"    "); err != nil {
		return err
	}
	w.Pf("%s}\n", ind)
	return nil
}

// emitReadRanged reads a value of [lo, hi] (64 bits or fewer) as its offset
// from lo, refuses an offset in the bit headroom, and stores lo + offset in
// the storage type typ. A degenerate range reads nothing: the value is the
// range (SPEC §4.6).
func (w *wgen) emitReadRanged(lo, hi *big.Int, typ, expr, ind string) {
	bits := ir.BitsRequired(lo, hi)
	if bits == 0 {
		w.Pf("%sguard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }\n", ind)
		w.Pf("%s%s = %s; // degenerate: zero bits, the value is the range (SPEC §4.6)\n", ind, expr, IntLit(lo))
		return
	}
	raw := w.readRaw(bits, ind)
	span := new(big.Int).Sub(hi, lo)
	if !fills(span, bits) {
		w.Pf("%sguard %s <= %s else { %s } // a value smuggled into the bit headroom is refused\n", ind, raw, span, failRange)
	}
	val := raw
	if lo.Sign() != 0 {
		val = fmt.Sprintf("%s + %s", raw, u64Lit(lo))
	}
	if typ == "u64" {
		w.Pf("%s%s = %s;\n", ind, expr, val)
		return
	}
	w.Pf("%s%s = (%s) as! %s;\n", ind, expr, val, typ)
}

func (w *wgen) emitReadScalar(f *ir.Field, expr, length, ind string) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		w.Pf("%sguard schema_rt::wire_bool(s, %s) else { return false; }\n", ind, expr)
	case ir.TFloat32:
		if f.HasFloatRange {
			steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			w.Pf("%sguard serialize_compressed_float_precomputed(s, %s, %d, %d, %s, %s) else { return false; }\n",
				ind, expr, steps, bits, floatLit32(delta), floatLit32(min32))
			return nil
		}
		// a NaN or an infinity pattern is refused on its raw bits, as
		// non_finite_float, before the field is written (SPEC §4.3)
		w.Pf("%sguard serialize_float(s, %s) else { return false; }\n", ind, expr)
	case ir.TFloat64:
		w.Pf("%sguard serialize_double(s, %s) else { return false; }\n", ind, expr)
	case ir.TString, ir.TBytes:
		w.emitReadRanged(big.NewInt(0), big.NewInt(t.Size), "i32", length, ind)
		w.Pf("%sguard serialize_bytes(s, %s[0..%s]) else { return false; }\n", ind, expr, length)
		if t.Kind == ir.TString {
			// content the read refuses (SPEC §4.7): malformed UTF-8, then an
			// interior null, each by its own reason
			w.Pf("%sguard serialize_valid_utf8(%s[0..%s]) else { return serialize_fail(s, SERIALIZE_ERROR_INVALID_UTF8); } // malformed UTF-8 is content the read refuses\n", ind, expr, length)
			c := w.tmp("c")
			w.Pf("%sfor %s in %s[0..%s] { guard %s != 0 else { return serialize_fail(s, SERIALIZE_ERROR_EMBEDDED_NUL); } } // an interior null is content the read refuses\n", ind, c, expr, length, c)
		}
	case ir.TWString:
		w.emitReadWString(f, expr, length, ind)
	case ir.TFixed:
		return w.emitReadFixed(f, expr, ind)
	case ir.TInt:
		if t.Width > 64 {
			return w.emitRead128(f, expr, ind)
		}
		typ := intGoose(t.Signed, t.Width)
		if !f.HasIntRange {
			w.emitReadBareInt(f, expr, typ, ind)
			return nil
		}
		w.emitReadRanged(f.IntMin, f.IntMax, typ, expr, ind)
	case ir.TBits:
		if t.Width <= 32 {
			w.Pf("%sguard schema_rt::wire_bits(s, %s, %d) else { return false; }\n", ind, expr, t.Width)
		} else {
			w.Pf("%sguard schema_rt::wire_bits64(s, %s, %d) else { return false; }\n", ind, expr, t.Width)
		}
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			w.emitReadRanged(big.NewInt(0), big.NewInt(ref.Max), w.TypeRef(t.Name, false), expr, ind)
		case *ir.Flags:
			raw := w.readRaw(int64(ref.WireBits), ind)
			w.Pf("%s%s = %s;\n", ind, expr, raw)
		case *ir.Struct, *ir.Union:
			w.Pf("%sguard Read%s(s, %s) else { return false; }\n", ind, w.TypeRef(t.Name, false), expr)
		default:
			return fmt.Errorf("field %s names %s, which has no packet wire", f.Name, t.Name)
		}
	default:
		return fmt.Errorf("field %s has a type kind with no packet wire", f.Name)
	}
	return nil
}

func (w *wgen) emitReadBareInt(f *ir.Field, expr, typ, ind string) {
	t := f.Type
	switch {
	case t.Width == 64 && !t.Signed:
		w.Pf("%sguard schema_rt::wire_bits64(s, %s, 64) else { return false; }\n", ind, expr)
	case t.Width == 32 && !t.Signed:
		w.Pf("%sguard schema_rt::wire_bits(s, %s, 32) else { return false; }\n", ind, expr)
	default:
		raw := w.readRaw(int64(t.Width), ind)
		if t.Signed && t.Width < 64 {
			// back through the same-width unsigned, so the sign bit lands
			w.Pf("%s%s = (%s as! u%d) as! %s;\n", ind, expr, raw, t.Width, typ)
			return
		}
		w.Pf("%s%s = %s as! %s;\n", ind, expr, raw, typ)
	}
}

// emitReadWString reads wide text (SPEC §4.12): each 32-bit group is one
// UTF-16 code unit, never zero, never above 0xFFFF, and surrogates only in
// high-then-low pairs; anything else is refused.
func (w *wgen) emitReadWString(f *ir.Field, expr, length, ind string) {
	w.emitReadRanged(big.NewInt(0), big.NewInt(f.Type.Size), "i32", length, ind)
	i, g, el := w.tmp("unit"), w.tmp("group"), w.tmp("expectLow")
	w.Pf("%svar %s = false;\n", ind, el)
	w.Pf("%sfor %s in %s {\n", ind, i, length)
	w.Pf("%s    var %s: u32 = 0;\n", ind, g)
	w.Pf("%s    guard schema_rt::wire_bits(s, %s, 32) else { return false; }\n", ind, g)
	w.Pf("%s    guard %s != 0 && %s <= 0xFFFF else { %s }\n", ind, g, g, failRange)
	w.Pf("%s    let high%s = %s >= 0xD800 && %s <= 0xDBFF;\n", ind, g, g, g)
	w.Pf("%s    let low%s = %s >= 0xDC00 && %s <= 0xDFFF;\n", ind, g, g, g)
	w.Pf("%s    guard low%s == %s else { %s } // an unpaired surrogate (SPEC §4.12)\n", ind, g, el, failRange)
	w.Pf("%s    %s = high%s;\n", ind, el, g)
	w.Pf("%s    %s[%s] = %s as! u16;\n", ind, expr, i, g)
	w.Pf("%s}\n", ind)
	w.Pf("%sguard !%s else { %s } // a final high surrogate\n", ind, el, failRange)
}

func (w *wgen) emitReadFixed(f *ir.Field, expr, ind string) error {
	t := f.Type
	if f.IntMin == nil || f.IntMax == nil {
		return fmt.Errorf("fixed field %s has no resolved whole-unit bounds", f.Name)
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// degenerate: zero bits, the value is the range, raw min << F (SPEC §4.6)
		rawMin := new(big.Int).Lsh(f.IntMin, uint(t.FracBits))
		w.Pf("%sguard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }\n", ind)
		if t.Width > 64 {
			w.Pf("%s%s = %s;\n", ind, expr, Int128Lit(rawMin))
		} else {
			w.Pf("%s%s = %s;\n", ind, expr, IntLit(rawMin))
		}
		return nil
	}
	entry, ib := fixedRoute(t)
	lo, hi := IntLit(f.IntMin), IntLit(f.IntMax)
	switch {
	case t.Signed && entry == "32" && t.Width == 32, t.Signed && entry == "64":
		w.Pf("%sguard serialize_fixed%s(s, %s, %d, %d, %s, %s) else { return false; }\n", ind, entry, expr, ib, t.FracBits, lo, hi)
	case entry == "32":
		v := w.tmp("fixed")
		w.Pf("%svar %s: i32 = 0;\n", ind, v)
		w.Pf("%sguard serialize_fixed32(s, %s, %d, %d, %s, %s) else { return false; }\n", ind, v, ib, t.FracBits, lo, hi)
		w.Pf("%s%s = %s as! %s; // inside the raw bounds, or the read failed\n", ind, expr, v, intGoose(true, t.Width))
	case entry == "64": // unsigned storage of 32 bits or fewer, zero extended
		v := w.tmp("fixed")
		w.Pf("%svar %s: i64 = 0;\n", ind, v)
		w.Pf("%sguard serialize_fixed64(s, %s, %d, %d, %s, %s) else { return false; }\n", ind, v, ib, t.FracBits, lo, hi)
		w.Pf("%s%s = %s as! %s; // inside the raw bounds, or the read failed\n", ind, expr, v, intGoose(false, t.Width))
	default:
		v := w.tmp("fixed")
		w.Pf("%svar %s = Int128 { lo: 0, hi: 0 };\n", ind, v)
		w.Pf("%sguard serialize_fixed128(s, %s, %d, %d, %s, %s) else { return false; }\n", ind, v, ib, t.FracBits, lo, hi)
		if t.Width > 64 {
			w.Pf("%s%s = %s::Int128 { lo: %s.lo, hi: %s.hi };\n", ind, expr, RuntimeNamespace, v, v)
		} else {
			w.Pf("%s%s = %s.lo; // unsigned 64-bit storage: the low lane, zero extended\n", ind, expr, v)
		}
	}
	return nil
}

func (w *wgen) emitRead128(f *ir.Field, expr, ind string) error {
	v := w.tmp("wide")
	if !f.HasIntRange {
		w.Pf("%svar %s = Uint128 { lo: 0, hi: 0 };\n", ind, v)
		w.Pf("%sguard serialize_uint128(s, %s) else { return false; }\n", ind, v)
		w.Pf("%s%s = %s::Int128 { lo: %s.lo, hi: %s.hi };\n", ind, expr, RuntimeNamespace, v, v)
		return nil
	}
	if f.IntMin == nil || f.IntMax == nil {
		return fmt.Errorf("int128 field %s has no resolved range (SPEC §4.3)", f.Name)
	}
	if f.IntMin.Cmp(f.IntMax) == 0 {
		w.Pf("%sguard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }\n", ind)
		w.Pf("%s%s = %s; // degenerate: zero bits, the value is the range (SPEC §4.6)\n", ind, expr, Int128Lit(f.IntMin))
		return nil
	}
	w.Pf("%svar %s = Int128 { lo: 0, hi: 0 };\n", ind, v)
	w.Pf("%sguard serialize_int128(s, %s, %s, %s) else { return false; }\n", ind, v, int128Lit(f.IntMin), int128Lit(f.IntMax))
	w.Pf("%s%s = %s::Int128 { lo: %s.lo, hi: %s.hi };\n", ind, expr, RuntimeNamespace, v, v)
	return nil
}
