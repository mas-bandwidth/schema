// The packet wire's WRITE path (docs/PORT-GOOSE.md, THE CARD MAP,
// schema-goose-type-wire-write): every field in wire order, its value checked
// against what the wire can carry BEFORE the runtime sees it (serialize.goose
// aborts on write misuse, U9), then moved through the runtime's bit
// primitives at a bit count folded here, at generation time.
package goose

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const refuseRange = "return SERIALIZE_ERROR_VALUE_OUT_OF_RANGE;"

func (w *wgen) emitWriteField(f *ir.Field, ind string) error {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array == ir.ArrayNone {
		return w.emitWriteScalar(f, name, name+"Length", ind)
	}
	limit := fmt.Sprint(f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		// the count is a writer contract: Goose has no build-removable
		// assert, so it is the every-build refusal (SPEC §4.6, §5)
		w.Pf("%sif %sCount < %d || %sCount > %d { %s }\n", ind, name, f.ArrayMin, name, f.ArrayBound, refuseRange)
		if bits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)); bits > 0 {
			off := name + "Count"
			if f.ArrayMin != 0 {
				off = fmt.Sprintf("%sCount - %d", name, f.ArrayMin)
			}
			w.Pf("%sschema_rt::wire_bits(s, l, (%s) as! u32, %d); // the count, offset from %d\n", ind, off, bits, f.ArrayMin)
		}
		limit = name + "Count"
	}
	i := w.tmp("ix")
	w.Pf("%sfor %s in %s {\n", ind, i, limit)
	if err := w.emitWriteScalar(f, name+"["+i+"]", name+"Length["+i+"]", ind+"    "); err != nil {
		return err
	}
	w.Pf("%s}\n", ind)
	return nil
}

// emitWriteScalar writes one value: a field, or one element of an array.
// length is the used-length slot of a string, bytes or wstring value.
func (w *wgen) emitWriteScalar(f *ir.Field, expr, length, ind string) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		w.Pf("%sschema_rt::wire_bool(s, l, %s);\n", ind, expr)
	case ir.TFloat32:
		if f.HasFloatRange {
			w.emitWriteCompressed(f, expr, ind)
			return nil
		}
		b := w.tmp("bits")
		w.Pf("%slet %s = serialize_f32_bits(%s);\n", ind, b, expr)
		w.Pf("%sif !serialize_float_bits_are_finite(%s) { return SERIALIZE_ERROR_NON_FINITE_FLOAT; } // NaN and the infinities are not values of the type (SPEC §4.3)\n", ind, b)
		w.Pf("%sschema_rt::wire_bits(s, l, %s, 32);\n", ind, b)
	case ir.TFloat64:
		b := w.tmp("bits")
		w.Pf("%slet %s = serialize_f64_bits(%s);\n", ind, b, expr)
		w.Pf("%sif !serialize_double_bits_are_finite(%s) { return SERIALIZE_ERROR_NON_FINITE_FLOAT; } // NaN and the infinities are not values of the type (SPEC §4.3)\n", ind, b)
		w.Pf("%sschema_rt::wire_bits64(s, l, %s, 64);\n", ind, b)
	case ir.TString, ir.TBytes:
		bits := ir.BitsRequired(big.NewInt(0), big.NewInt(t.Size))
		// length in [0, N], align, then the used bytes: the classic
		// serialize_string framing over a buffer of N + 1 (SPEC §4.7)
		w.Pf("%sif %s < 0 || %s > %d { %s }\n", ind, length, length, t.Size, refuseRange)
		w.Pf("%sschema_rt::wire_bits(s, l, %s as! u32, %d);\n", ind, length, bits)
		w.streamCall(ind, "%sserialize_bytes(s, %s[0..%s]);\n", ind, expr, length)
	case ir.TWString:
		w.emitWriteWString(f, expr, length, ind)
	case ir.TFixed:
		return w.emitWriteFixed(f, expr, ind)
	case ir.TInt:
		if t.Width > 64 {
			return w.emitWrite128(f, expr, ind)
		}
		if !f.HasIntRange {
			w.emitWriteBareInt(f, expr, ind)
			return nil
		}
		w.emitWriteRanged(t.Signed, t.Width, f.IntMin, f.IntMax, expr, ind)
	case ir.TBits:
		storage := 32
		if t.Width > 32 {
			storage = 64
		}
		if t.Width < storage {
			mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1))
			w.Pf("%sif %s > %s { %s } // a value wider than bits(%d) cannot ride\n", ind, expr, mask, refuseRange, t.Width)
		}
		if t.Width <= 32 {
			w.Pf("%sschema_rt::wire_bits(s, l, %s, %d);\n", ind, expr, t.Width)
		} else {
			w.Pf("%sschema_rt::wire_bits64(s, l, %s, %d);\n", ind, expr, t.Width)
		}
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			storage := ir.StorageBitsFor(ref.Max)
			if !fills(big.NewInt(ref.Max), int64(storage)) {
				w.Pf("%sif %s > %d { %s } // not a %s value\n", ind, expr, ref.Max, refuseRange, ref.Name)
			}
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			switch {
			case bits == 0:
			case storage <= 32:
				w.Pf("%sschema_rt::wire_bits(s, l, %s, %d);\n", ind, expr, bits)
			default:
				w.Pf("%sschema_rt::wire_bits64(s, l, %s, %d);\n", ind, expr, bits)
			}
		case *ir.Flags:
			if ref.WireBits < 64 {
				mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits)), big.NewInt(1))
				w.Pf("%sif %s > %s { %s } // a mask bit above the flags wire width cannot ride\n", ind, expr, mask, refuseRange)
			}
			if ref.WireBits <= 32 {
				w.Pf("%sschema_rt::wire_bits(s, l, %s as! u32, %d);\n", ind, expr, ref.WireBits)
			} else {
				w.Pf("%sschema_rt::wire_bits64(s, l, %s, %d);\n", ind, expr, ref.WireBits)
			}
		case *ir.Struct, *ir.Union:
			e := w.tmp("refusal")
			w.Pf("%slet %s = %s;\n", ind, e, w.nestedWrite(ref, w.TypeRef(t.Name, false), expr, ind))
			w.Pf("%sif %s != SERIALIZE_ERROR_NONE { return %s; }\n", ind, e, e)
		default:
			return fmt.Errorf("field %s names %s, which has no packet wire", f.Name, t.Name)
		}
	default:
		return fmt.Errorf("field %s has a type kind with no packet wire", f.Name)
	}
	return nil
}

// emitRangeRefusal refuses a value outside [lo, hi], compared in its own
// storage type; a half whose bound is the storage's own extreme can never be
// true and is left out.
func (w *wgen) emitRangeRefusal(signed bool, width int, lo, hi *big.Int, expr, ind string) {
	smin, smax := storageExtremes(signed, storageWidth(width))
	var cond string
	if lo.Cmp(smin) > 0 {
		cond = fmt.Sprintf("%s < %s", expr, IntLit(lo))
	}
	if hi.Cmp(smax) < 0 {
		if cond != "" {
			cond += " || "
		}
		cond += fmt.Sprintf("%s > %s", expr, IntLit(hi))
	}
	if cond != "" {
		w.Pf("%sif %s { %s }\n", ind, cond, refuseRange)
	}
}

// emitWriteRanged writes a ranged integer of 64 bits or fewer as its offset
// from lo in BitsRequired(lo, hi) bits. The offset is taken in u64, which
// wraps by definition, so a span past the signed type's reach is exact.
func (w *wgen) emitWriteRanged(signed bool, width int, lo, hi *big.Int, expr, ind string) {
	w.emitRangeRefusal(signed, width, lo, hi, expr, ind)
	bits := ir.BitsRequired(lo, hi)
	if bits == 0 {
		return // degenerate: the refusal is the whole write (SPEC §4.6)
	}
	o := w.tmp("offset")
	if signed {
		w.Pf("%slet %s: u64 = %s as! u64;\n", ind, o, expr)
	} else {
		w.Pf("%slet %s: u64 = %s;\n", ind, o, expr)
	}
	off := o
	if lo.Sign() != 0 {
		off = fmt.Sprintf("%s - %s", o, u64Lit(lo))
	}
	if bits <= 32 {
		w.Pf("%sschema_rt::wire_bits(s, l, (%s) as! u32, %d);\n", ind, off, bits)
		return
	}
	w.Pf("%sschema_rt::wire_bits64(s, l, %s, %d);\n", ind, off, bits)
}

// emitWriteBareInt writes a bare integer at its declared width; a signed
// value goes through its same-width unsigned, so no sign extension reaches
// the wire.
func (w *wgen) emitWriteBareInt(f *ir.Field, expr, ind string) {
	t := f.Type
	switch {
	case t.Width == 64 && t.Signed:
		w.Pf("%sschema_rt::wire_bits64(s, l, %s as! u64, 64);\n", ind, expr)
	case t.Width == 64:
		w.Pf("%sschema_rt::wire_bits64(s, l, %s, 64);\n", ind, expr)
	case t.Signed:
		w.Pf("%sschema_rt::wire_bits(s, l, %s as! u%d, %d);\n", ind, expr, t.Width, t.Width)
	default:
		w.Pf("%sschema_rt::wire_bits(s, l, %s, %d);\n", ind, expr, t.Width)
	}
}

// emitWriteCompressed is a ranged float32 through the runtime's precomputed
// entry: the step count, wire width and float32 range width are folded here
// (ir.CompressedFloatParams) and the quantization arithmetic keeps its one
// audited home in serialize.goose. A non-finite value is refused before the
// runtime sees it (SPEC §4.3).
func (w *wgen) emitWriteCompressed(f *ir.Field, expr, ind string) {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	w.Pf("%sif !serialize_float_bits_are_finite(serialize_f32_bits(%s)) { return SERIALIZE_ERROR_NON_FINITE_FLOAT; } // NaN and the infinities are not values of the type (SPEC §4.3)\n", ind, expr)
	w.streamCall(ind, "%sserialize_compressed_float_precomputed(s, %s, %d, %d, %s, %s);\n", ind, expr, steps, bits, floatLit32(delta), floatLit32(min32))
}

// emitWriteWString writes wide text (SPEC §4.12): the length in [0, N], then
// each UTF-16 code unit in its own 32-bit group. A zero unit is refused;
// surrogate pairing is the reader's refusal.
func (w *wgen) emitWriteWString(f *ir.Field, expr, length, ind string) {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size))
	w.Pf("%sif %s < 0 || %s > %d { %s }\n", ind, length, length, f.Type.Size, refuseRange)
	w.Pf("%sschema_rt::wire_bits(s, l, %s as! u32, %d);\n", ind, length, bits)
	i := w.tmp("unit")
	w.Pf("%sfor %s in %s {\n", ind, i, length)
	w.Pf("%s    if %s[%s] == 0 { %s } // an interior null is not text (SPEC §4.12)\n", ind, expr, i, refuseRange)
	w.Pf("%s    schema_rt::wire_bits(s, l, %s[%s], 32);\n", ind, expr, i)
	w.Pf("%s}\n", ind)
}

// ---- fixed point and 128-bit integers ---------------------------------------

// int128Lit is a 128-bit constant as the runtime's lanes, hi first as
// serialize_int128_make takes them.
func int128Lit(v *big.Int) string {
	m128 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	bits := new(big.Int).And(v, m128)
	lo := new(big.Int).And(bits, mask64)
	hi := new(big.Int).Rsh(bits, 64)
	return fmt.Sprintf("serialize_int128_make(0x%016x, 0x%016x)", hi, lo)
}

// lanes is a stored 128-bit value as the runtime's Int128 (its lanes are the
// storage's, named alike).
func lanes(expr string) string {
	return fmt.Sprintf("Int128 { lo: %s.lo, hi: %s.hi }", expr, expr)
}

// fixedRoute is the runtime entry a fixed field rides and the Q format it
// declares there. The wire depends only on the raw span — the bounds scaled
// by F — so an entry wider than the storage writes the same bytes; unsigned
// storage rides a wider signed entry, zero extended, so no raw value above
// the signed half is ever read as negative (STANDARD.md, fixed).
func fixedRoute(t ir.FieldType) (entry string, intBits int) {
	switch {
	case t.Signed && t.Width <= 32:
		return "32", t.IntBits
	case t.Signed && t.Width == 64:
		return "64", t.IntBits
	case t.Signed:
		return "128", t.IntBits
	case t.Width <= 32:
		return "64", 64 - t.FracBits
	default:
		return "128", 128 - t.FracBits
	}
}

func (w *wgen) emitWriteFixed(f *ir.Field, expr, ind string) error {
	t := f.Type
	if f.IntMin == nil || f.IntMax == nil {
		return fmt.Errorf("fixed field %s has no resolved whole-unit bounds", f.Name)
	}
	rawMin := new(big.Int).Lsh(f.IntMin, uint(t.FracBits))
	rawMax := new(big.Int).Lsh(f.IntMax, uint(t.FracBits))
	if t.Width > 64 {
		cmp := "serialize_int128_compare"
		if !t.Signed {
			cmp = "serialize_uint128_compare"
		}
		v := w.tmp("fixed")
		if t.Signed {
			w.Pf("%slet %s = %s;\n", ind, v, lanes(expr))
		} else {
			w.Pf("%slet %s = Uint128 { lo: %s.lo, hi: %s.hi };\n", ind, v, expr, expr)
		}
		lit := func(x *big.Int) string {
			if t.Signed {
				return int128Lit(x)
			}
			return "serialize_uint128_make" + int128Lit(x)[len("serialize_int128_make"):]
		}
		if f.IntMin.Cmp(f.IntMax) == 0 {
			w.Pf("%sif %s(%s, %s) != 0 { %s } // degenerate: zero bits, the one legal raw (SPEC §4.6)\n", ind, cmp, v, lit(rawMin), refuseRange)
			return nil
		}
		w.Pf("%sif %s(%s, %s) < 0 || %s(%s, %s) > 0 { %s }\n", ind, cmp, v, lit(rawMin), cmp, v, lit(rawMax), refuseRange)
		_, ib := fixedRoute(t)
		arg := v
		if !t.Signed {
			arg = lanes(v)
		}
		w.streamCall(ind, "%sserialize_fixed128(s, %s, %d, %d, %s, %s);\n", ind, arg, ib, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
		return nil
	}
	w.emitRangeRefusal(t.Signed, t.Width, rawMin, rawMax, expr, ind)
	if f.IntMin.Cmp(f.IntMax) == 0 {
		return nil // degenerate: zero bits, the refusal is the whole write (SPEC §4.6)
	}
	entry, ib := fixedRoute(t)
	arg := expr
	if entry == "128" { // unsigned 64-bit storage, zero extended
		arg = fmt.Sprintf("Int128 { lo: %s, hi: 0 }", expr)
	}
	w.streamCall(ind, "%sserialize_fixed%s(s, %s, %d, %d, %s, %s);\n", ind, entry, arg, ib, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
	return nil
}

func (w *wgen) emitWrite128(f *ir.Field, expr, ind string) error {
	if !f.HasIntRange {
		// uint128 is the raw field: 128 bits, the low half first
		w.streamCall(ind, "%sserialize_uint128(s, Uint128 { lo: %s.lo, hi: %s.hi });\n", ind, expr, expr)
		return nil
	}
	if f.IntMin == nil || f.IntMax == nil {
		return fmt.Errorf("int128 field %s has no resolved range (SPEC §4.3)", f.Name)
	}
	v := w.tmp("wide")
	w.Pf("%slet %s = %s;\n", ind, v, lanes(expr))
	if f.IntMin.Cmp(f.IntMax) == 0 {
		w.Pf("%sif serialize_int128_compare(%s, %s) != 0 { %s } // degenerate: zero bits (SPEC §4.6)\n", ind, v, int128Lit(f.IntMin), refuseRange)
		return nil
	}
	w.Pf("%sif serialize_int128_compare(%s, %s) < 0 || serialize_int128_compare(%s, %s) > 0 { %s }\n",
		ind, v, int128Lit(f.IntMin), v, int128Lit(f.IntMax), refuseRange)
	w.streamCall(ind, "%sserialize_int128(s, %s, %s, %s);\n", ind, v, int128Lit(f.IntMin), int128Lit(f.IntMax))
	return nil
}
