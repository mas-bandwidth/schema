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
// length is the used-length slot of a string or bytes value.
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

// ---- the measure pass -------------------------------------------------------

// The generated writer checks the value's real size against the stream's room
// left before the runtime sees a write that would not fit: serialize.goose
// aborts on a write past the buffer (U9), so the short stream is refused with
// SERIALIZE_ERROR_PAST_END instead. The fast path is <T>MaxBits, the longest
// path the type can take; below it the measure pass sums the value's exact
// wire bits, alignment folded at its real position, so a stream that holds the
// value still succeeds byte-identically.

// emitMeasureStruct writes _Measure<T>, the value's exact end bit position
// starting from bits.
func (w *wgen) emitMeasureStruct(st *ir.Struct) error {
	w.Declare("_Measure" + st.Name)
	w.Pf("fn _Measure%s(value: %s&, bits: i64) -> i64 {\n", st.Name, st.Name)
	w.Pf("    var total = bits;\n")
	w.n = 0
	if err := w.emitMeasureItems(st.Items, "    "); err != nil {
		return fmt.Errorf("%s: %w", st.Name, err)
	}
	w.Pf("    return total;\n}\n\n")
	return nil
}

// emitMeasureUnion writes _Measure<U>: the tag, then the selected arm.
func (w *wgen) emitMeasureUnion(d *ir.Union) error {
	w.Declare("_Measure" + d.Name)
	tag := d.Name + "Type"
	w.Pf("fn _Measure%s(value: %s&, bits: i64) -> i64 {\n", d.Name, d.Name)
	w.Pf("    var total = bits;\n")
	if bits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max)); bits > 0 {
		w.Pf("    total += %d; // the tag\n", bits)
	}
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		if v.Ref == nil {
			return fmt.Errorf("union %s arm %s carries no type payload, which the packet wire has no encoding for", d.Name, v.Name)
		}
		arm := ir.GoExportName(v.Name)
		w.Pf("    if value.Type == %s%s { total = _Measure%s(value.%s, total); }\n", tag, arm, w.TypeRef(v.Type, false), arm)
	}
	w.Pf("    return total;\n}\n\n")
	return nil
}

func (w *wgen) emitMeasureItems(items []ir.Item, ind string) error {
	for _, item := range items {
		switch it := item.(type) {
		case *ir.FieldItem:
			if err := w.emitMeasureField(it.F, ind); err != nil {
				return err
			}
		case *ir.ConstItem:
			if it.Bits > 0 {
				w.Pf("%stotal += %d; // const(%s, %d)\n", ind, it.Bits, it.Value.String(), it.Bits)
			}
		case *ir.ReservedItem:
			if it.Bits > 0 {
				w.Pf("%stotal += %d; // reserved(%d)\n", ind, it.Bits, it.Bits)
			}
		case *ir.AlignItem:
			w.Pf("%stotal += (8 - total %% 8) %% 8;\n", ind)
		case *ir.Branch:
			w.Pf("%sif %s {\n", ind, condExpr(it))
			if err := w.emitMeasureItems(it.Then, ind+"    "); err != nil {
				return err
			}
			if it.Else != nil {
				w.Pf("%s} else {\n", ind)
				if err := w.emitMeasureItems(it.Else, ind+"    "); err != nil {
					return err
				}
			}
			w.Pf("%s}\n", ind)
		}
	}
	return nil
}

func (w *wgen) emitMeasureField(f *ir.Field, ind string) error {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array == ir.ArrayNone {
		return w.emitMeasureScalar(f, name, name+"Length", ind)
	}
	limit := fmt.Sprint(f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		bits := ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound))
		if bits > 0 {
			w.Pf("%stotal += %d; // the count\n", ind, bits)
		}
		n := w.tmp("n")
		w.Pf("%slet %s = if %s > %d { %d } else if %s < 0 { 0 } else { %s };\n", ind, n, name+"Count", f.ArrayBound, f.ArrayBound, name+"Count", name+"Count")
		limit = n
	}
	i := w.tmp("ix")
	if f.Array == ir.ArrayCounted {
		w.Pf("%sif %s > 0 {\n", ind, limit)
		w.Pf("%s    for %s in %s {\n", ind, i, limit)
		if err := w.emitMeasureScalar(f, name+"["+i+"]", name+"Length["+i+"]", ind+"        "); err != nil {
			return err
		}
		w.Pf("%s    }\n%s}\n", ind, ind)
		return nil
	}
	w.Pf("%sfor %s in %s {\n", ind, i, limit)
	if err := w.emitMeasureScalar(f, name+"["+i+"]", name+"Length["+i+"]", ind+"    "); err != nil {
		return err
	}
	w.Pf("%s}\n", ind)
	return nil
}

func (w *wgen) emitMeasureScalar(f *ir.Field, expr, length, ind string) error {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		w.Pf("%stotal += 1;\n", ind)
	case ir.TFloat32:
		if f.HasFloatRange {
			w.Pf("%stotal += %d;\n", ind, ir.CompressedFloatBits(f.FMin, f.FMax, f.Resolution))
			return nil
		}
		w.Pf("%stotal += 32;\n", ind)
	case ir.TFloat64:
		w.Pf("%stotal += 64;\n", ind)
	case ir.TString, ir.TBytes:
		if bits := ir.BitsRequired(big.NewInt(0), big.NewInt(t.Size)); bits > 0 {
			w.Pf("%stotal += %d; // the length\n", ind, bits)
		}
		w.Pf("%stotal += (8 - total %% 8) %% 8; // the payload aligns\n", ind)
		// length is an i32 slot: clamp to [0, N] in i64 before scaling to
		// bits so a large bound cannot wrap i32 and an out-of-range length
		// cannot mask VALUE_OUT_OF_RANGE when every legal value fits
		n := w.tmp("len")
		w.Pf("%slet %s: i64 = if %s > %d { %d } else if %s < 0 { 0 } else { %s };\n", ind, n, length, t.Size, t.Size, length, length)
		w.Pf("%stotal += %s * 8;\n", ind, n)
	case ir.TFixed:
		if f.IntMin == nil || f.IntMax == nil {
			return fmt.Errorf("fixed field %s has no resolved whole-unit bounds", f.Name)
		}
		if f.IntMin.Cmp(f.IntMax) != 0 {
			w.Pf("%stotal += %d;\n", ind, ir.BitsRequired(f.IntMin, f.IntMax)+int64(t.FracBits))
		}
	case ir.TInt:
		if t.Width > 64 {
			if !f.HasIntRange {
				w.Pf("%stotal += 128;\n", ind)
				return nil
			}
			if f.IntMin == nil || f.IntMax == nil {
				return fmt.Errorf("int128 field %s has no resolved range (SPEC §4.3)", f.Name)
			}
			if f.IntMin.Cmp(f.IntMax) != 0 {
				w.Pf("%stotal += %d;\n", ind, ir.BitsRequired(f.IntMin, f.IntMax))
			}
			return nil
		}
		if f.HasIntRange {
			if bits := ir.BitsRequired(f.IntMin, f.IntMax); bits > 0 {
				w.Pf("%stotal += %d;\n", ind, bits)
			}
			return nil
		}
		w.Pf("%stotal += %d;\n", ind, t.Width)
	case ir.TBits:
		w.Pf("%stotal += %d;\n", ind, t.Width)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			if bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)); bits > 0 {
				w.Pf("%stotal += %d;\n", ind, bits)
			}
		case *ir.Flags:
			w.Pf("%stotal += %d;\n", ind, ref.WireBits)
		case *ir.Struct, *ir.Union:
			w.Pf("%stotal = _Measure%s(%s, total);\n", ind, w.TypeRef(t.Name, false), expr)
		default:
			return fmt.Errorf("field %s names %s, which has no packet wire", f.Name, t.Name)
		}
	default:
		return fmt.Errorf("field %s has a type kind with no packet wire", f.Name)
	}
	return nil
}
