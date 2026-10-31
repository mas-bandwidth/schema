// read.go is the type wire's READ side (docs/PORT-LOBSTER.md, THE TYPE
// MAPPING and THE RUNTIME LAYOUT; SPEC §4.3, §4.8, §5, §6.1): for every packet
// struct and union it emits Read<Name>(stream, value) over the VENDORED
// serialize.lobster v2.1.1 read stream, through its public surface only. It
// mirrors write.go call for call, so the two are one wire.
//
// THE REFUSAL MODEL (serialize STANDARD.md, Refusal reasons; Reader
// Obligations). A read never traps. Every refusal LATCHES the stream with its
// reason, so the stream is terminal and stream.refusal_name() names the first
// rule broken: the port's calls latch their own (past_end,
// value_out_of_range, nonzero_padding, non_finite_float), and the generated
// rules latch through stream.fail: a const that is not its value and set
// reserved bits are value_out_of_range, a ranged offset in the bit headroom
// is value_out_of_range, a string(N) payload is embedded_nul or invalid_utf8.
// Every Read<Name> answers false on any refusal. A packet reader calls
// stream.serialize_end() after the top-level object; it refuses trailing
// bytes (trailing_data) and set padding (nonzero_padding).
//
// Storage is the §6.1 storage principle: a record is a class instance and a
// read fills its pre-allocated buffers in place, so the unused tail of a
// buffer keeps what it held. Selecting a union arm constructs a fresh payload
// with its declared defaults (SPEC §4.8); the untaken side of a branch reads
// as the §5 ZERO form, never the specified defaults.
package lobster

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("read", readPart)
}

// intRangePath picks the serialize call family for a ranged integer — the
// same switch as the other targets: int32 bounds ride serialize_int, int64
// bounds serialize_int64, and an unsigned range past int64 the bits64
// primitive with the headroom check. Every family writes the offset from min
// in bits_required(min, max) bits, which is what write.go writes.
func intRangePath(min, max *big.Int) string {
	if min.Cmp(big.NewInt(math.MinInt32)) >= 0 && max.Cmp(big.NewInt(math.MaxInt32)) <= 0 {
		return "int32"
	}
	if min.Cmp(big.NewInt(math.MinInt64)) >= 0 && max.Cmp(big.NewInt(math.MaxInt64)) <= 0 {
		return "int64"
	}
	return "bits64"
}

// readPart emits the read function for every packet struct and union of the
// file, after write.go's writers.
func readPart(g *Gen) error {
	if !packetDecls(g) {
		return nil
	}
	needWireRuntimes(g)
	for _, d := range ir.EmissionOrder(g.File) {
		switch d := d.(type) {
		case *ir.Struct:
			if d.IsTable {
				continue
			}
			emitReadStruct(g, d)
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] != nil {
				continue
			}
			emitReadUnion(g, d)
		}
	}
	return nil
}

// readRef names a declaration's emitted reader, requiring its module when
// the declaration lives in another file.
func (g *Gen) readRef(name string) string {
	g.Import(g.moduleOf(name, false))
	return "Read" + name
}

// newRef names a declaration's constructor, requiring its module.
func (g *Gen) newRef(name string) string {
	g.Import(g.moduleOf(name, false))
	return "New" + name
}

// readBody accumulates one read function: its statements, and the scratch
// declarations those statements need, emitted once at the top of the body
// (Lobster warns on an unused variable, and the tests compare stdout whole).
type readBody struct {
	b                          strings.Builder
	needInt, needOK, needFloat bool
	needBool, needText         bool
	needBytes, needWide        bool
	needInt128, needUint128    bool
	bulk                       map[*ir.Field]bool
}

func (r *readBody) int() string {
	r.needInt, r.needOK = true, true
	return "rv"
}

func (r *readBody) ok() string {
	r.needOK = true
	return "rok"
}

func (r *readBody) float() string {
	r.needFloat, r.needOK = true, true
	return "rf"
}

func (r *readBody) boolean() string {
	r.needBool, r.needOK = true, true
	return "rb"
}

func (r *readBody) bytes() string {
	r.needBytes, r.needOK = true, true
	return "rs"
}

func (r *readBody) text() string {
	r.needText = true
	return "rt"
}

func (r *readBody) int128() string {
	r.needInt128, r.needOK = true, true
	return "rw"
}

func (r *readBody) uint128() string {
	r.needUint128, r.needOK = true, true
	return "ru"
}

func (r *readBody) pf(ind, format string, args ...any) {
	r.b.WriteString(ind)
	fmt.Fprintf(&r.b, format, args...)
	r.b.WriteByte('\n')
}

// call emits one port read into v and its refusal check: a false answer is
// already latched by the port, so it only returns.
func (r *readBody) call(ind, v, call string) {
	r.pf(ind, "%s, %s = stream.%s", v, r.ok(), call)
	r.pf(ind, "if not %s: return false", r.ok())
}

// refuse emits a generated rule's refusal: latch the reason, answer false.
func (r *readBody) refuse(ind, cond, reason, comment string) {
	r.pf(ind, "if %s: return stream.fail(%s) // %s", cond, reason, comment)
}

// decls renders the scratch declarations the body used.
func (r *readBody) decls() string {
	var b strings.Builder
	if r.needInt {
		b.WriteString("    var rv = 0\n")
	}
	if r.needOK {
		b.WriteString("    var rok = true\n")
	}
	if r.needFloat {
		b.WriteString("    var rf = 0.0\n")
	}
	if r.needBool {
		b.WriteString("    var rb = false\n")
	}
	if r.needBytes {
		b.WriteString("    var rs = \"\"\n")
	}
	if r.needWide {
		b.WriteString("    var runit = 0\n    var rlow = false\n")
	}
	if r.needText {
		b.WriteString("    var rt = serialize_refusal_none\n")
	}
	if r.needInt128 {
		b.WriteString("    var rw = serialize_int128 { lo: 0, hi: 0 }\n")
	}
	if r.needUint128 {
		b.WriteString("    var ru = serialize_uint128 { lo: 0, hi: 0 }\n")
	}
	return b.String()
}

// emitReadStruct is one type's reader: the wire tree of st.Items in order,
// with the untaken side of each branch zeroed (SPEC §5).
func emitReadStruct(g *Gen, st *ir.Struct) {
	r := &readBody{bulk: ir.AlignedFixedByteArrays(st)}
	emitReadItems(g, r, st.Items, "    ")
	g.Pf("// Read%s decodes the wire into value, filling its pre-allocated storage\n", st.Name)
	g.Pf("// (SPEC §6.1); the untaken side of a branch reads as zero (SPEC §5). A\n")
	g.Pf("// refusal latches the stream with its reason and answers false.\n")
	g.Pf("def Read%s(stream:serialize_read_stream, value:%s) -> bool:\n", st.Name, st.Name)
	if r.b.Len() == 0 {
		g.Pf("    // no wire bits: an empty body, or every field a zero-bit degenerate\n")
		g.Pf("    // range; a failed stream still refuses (Reader Obligations)\n")
		g.Pf("    return stream.ok()\n\n")
		return
	}
	g.Pf("%s", r.decls())
	g.Pf("%s", r.b.String())
	g.Pf("    return true\n\n")
}

// emitReadUnion is a one-of reader (SPEC §4.8): the tag in [0, Max], whose
// ranged read refuses a tag past the arms, then the selected arm's payload,
// constructed fresh with its declared defaults on every selection. A
// payload-free arm and None are the tag alone.
func emitReadUnion(g *Gen, d *ir.Union) {
	r := &readBody{}
	tag := d.Name + "Type"
	g.Pf("// Read%s decodes the %s tag and its selected arm (SPEC §4.8); a tag\n", d.Name, tag)
	g.Pf("// past the arms is refused, and the selected arm is constructed fresh.\n")
	g.Pf("def Read%s(stream:serialize_read_stream, value:%s) -> bool:\n", d.Name, d.Name)
	if d.Max == 0 {
		g.Pf("    value.Type = %s // zero wire bits — only None exists (SPEC §4.8)\n", VariantName(tag, "None"))
		g.Pf("    return stream.ok()\n\n")
		return
	}
	v := r.int()
	r.call("    ", v, fmt.Sprintf("serialize_int(%s, 0, %d) // refuses a tag above the arms (SPEC §4.8)", v, d.Max))
	r.pf("    ", "value.Type = %s(%s)", tag, v)
	for _, arm := range d.Variants {
		if arm.Void() {
			continue
		}
		name := ir.GoExportName(arm.Name)
		r.pf("    ", "if value.Type == %s:", VariantName(tag, arm.Name))
		emitFreshArm(g, r, arm, "value."+name, "        ")
		emitReadField(g, r, arm.F, "value."+name, "        ")
	}
	r.pf("    ", "return true")
	g.Pf("%s", r.decls())
	g.Pf("%s", r.b.String())
	g.Pf("\n")
}

// emitFreshArm constructs a selected arm's payload at its declared defaults
// before the read overlays it (SPEC §4.8: a fresh payload on every
// selection, a repeated tag included).
func emitFreshArm(g *Gen, r *readBody, arm ir.UnionVariant, target, ind string) {
	f := arm.F
	if f.Array == ir.ArrayNone && f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			r.pf(ind, "%s = %s() // a fresh payload on every selection (SPEC §4.8)", target, g.newRef(f.Type.Name))
			return
		}
	}
	// packet arms are declared types (SPEC §4.8); any other shape is
	// overlaid by its read as a field is
}

// emitReadItems is one wire tree in order.
func emitReadItems(g *Gen, r *readBody, items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitReadField(g, r, item.F, "value."+ir.GoExportName(item.F.Name), ind)
		case *ir.ConstItem:
			if item.Bits == 0 {
				continue
			}
			v := r.int()
			r.call(ind, v, fmt.Sprintf("%s(%s, %d)", bitsFn(item.Bits), v, item.Bits))
			r.refuse(ind, fmt.Sprintf("%s != %s", v, IntLit(item.Value)), "serialize_refusal_value_out_of_range",
				fmt.Sprintf("const(%s, %d): any other value is refused (SPEC §4.3)", item.Value.String(), item.Bits))
		case *ir.ReservedItem:
			for left := item.Bits; left > 0; left -= 64 {
				n := min(left, 64)
				v := r.int()
				r.call(ind, v, fmt.Sprintf("%s(%s, %d)", bitsFn(n), v, n))
				r.refuse(ind, v+" != 0", "serialize_refusal_value_out_of_range",
					fmt.Sprintf("reserved(%d): a set bit is refused (SPEC §4.3)", item.Bits))
			}
		case *ir.AlignItem:
			r.pf(ind, "if not stream.serialize_align(): return false // refuses nonzero padding (SPEC §4.3)")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "not "
			}
			r.pf(ind, "if %svalue.%s: // the taken side rides; the other reads as zero (SPEC §4.5, §5)", neg, ir.GoExportName(item.Cond))
			n := r.b.Len()
			emitReadItems(g, r, item.Then, ind+"    ")
			emitZeroItems(g, r, item.Else, ind+"    ")
			if r.b.Len() == n {
				r.pf(ind+"    ", "pass()")
			}
			r.pf(ind, "else:")
			n = r.b.Len()
			emitReadItems(g, r, item.Else, ind+"    ")
			emitZeroItems(g, r, item.Then, ind+"    ")
			if r.b.Len() == n {
				r.pf(ind+"    ", "pass()")
			}
		}
	}
}

func bitsFn(bits int64) string {
	if bits > 32 {
		return "serialize_bits64"
	}
	return "serialize_bits"
}

// emitReadField reads one field into target (a storage path expression).
func emitReadField(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	if f.Array == ir.ArrayNone {
		emitReadScalar(g, r, f, target, "", ind)
		return
	}
	limit := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		v := r.int()
		r.call(ind, v, fmt.Sprintf("serialize_int(%s, %d, %d) // the count, in [%d, %d], guards the loop (SPEC §4.6, §6.3)", v, f.ArrayMin, f.ArrayBound, f.ArrayMin, f.ArrayBound))
		r.pf(ind, "%sCount = %s", target, v)
		limit = target + "Count"
	} else if isByteElem(f.Type) && !f.HasIntRange && r.bulk[f] {
		r.call(ind, r.bytes(), fmt.Sprintf("serialize_bytes(%s, %d) // [N]uint8 at a byte-aligned position: one bulk copy, in place (SPEC §4.7)", target, f.ArrayBound))
		return
	}
	elem := *f
	elem.Array = ir.ArrayNone
	r.pf(ind, "for(%s) i:", limit)
	switch {
	case isByteElem(f.Type):
		v := r.int()
		emitReadScalar(g, r, &elem, v, "", ind+"    ")
		r.pf(ind+"    ", "write_int8_le(%s, i, %s)", target, v)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		emitReadScalar(g, r, &elem, target+"[i]", target+"Length[i]", ind+"    ")
	default:
		emitReadScalar(g, r, &elem, target+"[i]", "", ind+"    ")
	}
}

// emitReadScalar reads one value of a field's kind into target. length is
// the used-length slot of a buffer field; empty means target's own
// <Name>Length.
func emitReadScalar(g *Gen, r *readBody, f *ir.Field, target, length, ind string) {
	switch f.Type.Kind {
	case ir.TBool:
		v := r.boolean()
		r.call(ind, v, fmt.Sprintf("serialize_bool(%s)", v))
		r.pf(ind, "%s = %s", target, v)
	case ir.TInt:
		if f.Type.Width == 128 {
			emitReadInt128(r, f, target, ind)
			return
		}
		emitReadInt(r, f, target, ind)
	case ir.TBits:
		v := r.int()
		r.call(ind, v, fmt.Sprintf("%s(%s, %d)", bitsFn(int64(f.Type.Width)), v, f.Type.Width))
		r.pf(ind, "%s = %s", target, v)
	case ir.TFixed:
		emitReadFixed(r, f, target, ind)
	case ir.TFloat32:
		v := r.float()
		if f.HasFloatRange {
			steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			min32 := float32(f.FMin)
			delta := float32(f.FMax) - min32
			r.call(ind, v, fmt.Sprintf("serialize_compressed_float_precomputed(%s, %d, %d, %s, %s) // a step past the top, or a non-finite decode, is refused (SPEC §4.3)",
				v, steps, bits, FormatFloat32(float64(delta)), FormatFloat32(float64(min32))))
		} else {
			r.call(ind, v, fmt.Sprintf("serialize_float(%s) // NaN and ±inf refuse as non_finite_float on the raw bits (SPEC §4.3)", v))
		}
		r.pf(ind, "%s = %s", target, v)
	case ir.TFloat64:
		v := r.float()
		r.call(ind, v, fmt.Sprintf("serialize_double(%s) // NaN and ±inf refuse as non_finite_float on the raw bits (SPEC §4.3)", v))
		r.pf(ind, "%s = %s", target, v)
	case ir.TString, ir.TBytes:
		if length == "" {
			length = target + "Length"
		}
		v := r.int()
		r.call(ind, v, fmt.Sprintf("serialize_int(%s, 0, %d) // the used length guards the copy (SPEC §4.7, §6.3)", v, f.Type.Size))
		r.call(ind, r.bytes(), fmt.Sprintf("serialize_bytes(%s, %s) // aligns (padding zero), then the used bytes in place (SPEC §4.7)", target, v))
		r.pf(ind, "%s = %s", length, v)
		if f.Type.Kind == ir.TString {
			rt := r.text()
			r.pf(ind, "%s = %s.text_refusal(%s, %s)", rt, RuntimeNamespace, target, v)
			r.refuse(ind, rt+" != serialize_refusal_none", rt, "an interior NUL or malformed UTF-8 is refused (SPEC §4.7)")
		}
	case ir.TWString:
		emitReadWString(r, f, target, length, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			v := r.int()
			r.call(ind, v, fmt.Sprintf("serialize_int(%s, 0, %d) // refuses a value past %sMax (SPEC §4.2)", v, ref.Max, f.Type.Name))
			r.pf(ind, "%s = %s(%s)", target, f.Type.Name, v)
		case *ir.Flags:
			v := r.int()
			r.call(ind, v, fmt.Sprintf("%s(%s, %d) // the flags wire width (SPEC §4.2)", bitsFn(int64(ref.WireBits)), v, ref.WireBits))
			r.pf(ind, "%s = %s", target, v)
		case *ir.Struct, *ir.Union:
			r.pf(ind, "if not %s(stream, %s): return false", g.readRef(f.Type.Name), target)
		}
	}
}

// emitReadWString reads a wstring(N): the count in [0, N] code units, then
// one 32-bit group per unit into the UTF-16LE buffer, with every content
// refusal SPEC §4.12 states in one pass: a zero unit is embedded_nul, a group
// above 0xFFFF and an unpaired surrogate (a low without a high, a high not
// followed by a low, a high as the final unit) are value_out_of_range.
func emitReadWString(r *readBody, f *ir.Field, target, length, ind string) {
	if length == "" {
		length = target + "Length"
	}
	v := r.int()
	r.needWide = true
	r.call(ind, v, fmt.Sprintf("serialize_int(%s, 0, %d) // the code unit count guards the loop (SPEC §4.12)", v, f.Type.Size))
	r.pf(ind, "%s = %s", length, v)
	r.pf(ind, "rlow = false")
	r.pf(ind, "for(%s) u:", v)
	in := ind + "    "
	r.call(in, "runit", "serialize_bits(runit, 32) // one 32-bit group per code unit (SPEC §4.12)")
	r.refuse(in, "runit == 0", "serialize_refusal_embedded_nul", "a zero code unit (SPEC §4.12)")
	r.refuse(in, "runit > 0xFFFF", "serialize_refusal_value_out_of_range", "a group past 16 bits (SPEC §4.12)")
	r.refuse(in, "(runit >= 0xDC00 and runit <= 0xDFFF) != rlow", "serialize_refusal_value_out_of_range", "an unpaired surrogate (SPEC §4.12)")
	r.pf(in, "rlow = runit >= 0xD800 and runit <= 0xDBFF")
	r.pf(in, "write_int16_le(%s, u * 2, runit)", target)
	r.refuse(ind, "rlow", "serialize_refusal_value_out_of_range", "a high surrogate as the final unit (SPEC §4.12)")
}

// emitReadInt reads a bare or ranged integer of 64 bits or fewer.
func emitReadInt(r *readBody, f *ir.Field, target, ind string) {
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			r.pf(ind, "%s = %s // a degenerate range: zero wire bits (SPEC §4.6)", target, IntLit(f.IntMin))
			return
		}
		v := r.int()
		switch intRangePath(f.IntMin, f.IntMax) {
		case "int32":
			r.call(ind, v, fmt.Sprintf("serialize_int(%s, %s, %s) // the offset from min; past max is refused (SPEC §4.3)", v, f.IntMin.String(), f.IntMax.String()))
			r.pf(ind, "%s = %s", target, v)
		case "int64":
			r.call(ind, v, fmt.Sprintf("serialize_int64(%s, %s, %s) // the offset from min; past max is refused (SPEC §4.3)", v, IntLit(f.IntMin), IntLit(f.IntMax)))
			r.pf(ind, "%s = %s", target, v)
		default:
			bits := ir.BitsRequired(f.IntMin, f.IntMax)
			diff := new(big.Int).Sub(f.IntMax, f.IntMin)
			r.call(ind, v, fmt.Sprintf("serialize_bits64(%s, %d) // the offset from min (SPEC §4.3)", v, bits))
			headroom := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
			if diff.Cmp(headroom) < 0 {
				r.refuse(ind, fmt.Sprintf("%s.ult(%s, %s)", RuntimeNamespace, IntLit(diff), v), "serialize_refusal_value_out_of_range",
					"an offset in the bit headroom is past max (SPEC §4.3)")
			}
			if f.IntMin.Sign() == 0 {
				r.pf(ind, "%s = %s", target, v)
			} else {
				r.pf(ind, "%s = %s + %s", target, v, IntLit(f.IntMin))
			}
		}
		return
	}
	v := r.int()
	r.call(ind, v, fmt.Sprintf("%s(%s, %d) // the storage width (SPEC §4.3)", bitsFn(int64(f.Type.Width)), v, f.Type.Width))
	if f.Type.Signed && f.Type.Width < 64 {
		r.pf(ind, "%s = (%s << %d) >> %d // sign-extend the narrow pattern", target, v, 64-f.Type.Width, 64-f.Type.Width)
		return
	}
	r.pf(ind, "%s = %s", target, v)
}

func emitReadInt128(r *readBody, f *ir.Field, target, ind string) {
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			r.pf(ind, "%s = %s // a degenerate range: zero wire bits (SPEC §4.6)", target, Int128Lit(f.IntMin))
			return
		}
		v := r.int128()
		r.call(ind, v, fmt.Sprintf("serialize_int128(%s, %s, %s) // the offset from min; past max is refused (SPEC §4.3)",
			v, serializeInt128Lit(f.IntMin), serializeInt128Lit(f.IntMax)))
		r.pf(ind, "%s = %s.Int128 { lo: %s.lo, hi: %s.hi }", target, RuntimeNamespace, v, v)
		return
	}
	v := r.uint128()
	r.call(ind, v, fmt.Sprintf("serialize_uint128(%s) // uint128 raw (SPEC §4.3)", v))
	r.pf(ind, "%s = %s.Int128 { lo: %s.lo, hi: %s.hi }", target, RuntimeNamespace, v, v)
}

// emitReadFixed reads a fixed point value through the port's Q format path,
// the same storage arm write.go writes with; a degenerate range is its one
// raw value and no wire bits (SPEC §4.6).
func emitReadFixed(r *readBody, f *ir.Field, target, ind string) {
	t := f.Type
	if f.IntMin.Cmp(f.IntMax) == 0 {
		raw := new(big.Int).Lsh(f.IntMin, uint(t.FracBits))
		if t.Width > 64 {
			r.pf(ind, "%s = %s // a degenerate range: zero wire bits (SPEC §4.6)", target, Int128Lit(raw))
		} else {
			r.pf(ind, "%s = %s // a degenerate range: zero wire bits (SPEC §4.6)", target, IntLit(raw))
		}
		return
	}
	fn, intBits, widened := fixedDeclaration(f)
	if t.Width > 64 {
		v := r.int128()
		r.call(ind, v, fmt.Sprintf("serialize_fixed128(%s, %d, %d, %s, %s) // the Q format's raw offset (SPEC §4.3)",
			v, intBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax)))
		r.pf(ind, "%s = %s.Int128 { lo: %s.lo, hi: %s.hi }", target, RuntimeNamespace, v, v)
		return
	}
	if widened {
		// a ufixed whose raw values reach 2^63 (fixedDeclaration): the
		// 128-bit operation, and the low lane is the 64-bit pattern
		v := r.int128()
		r.call(ind, v, fmt.Sprintf("serialize_fixed128(%s, %d, %d, %s, %s) // the Q format's raw offset, unsigned (SPEC §4.3)",
			v, intBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax)))
		r.pf(ind, "%s = %s.lo", target, v)
		return
	}
	v := r.int()
	r.call(ind, v, fmt.Sprintf("%s(%s, %d, %d, %s, %s) // the Q format's raw offset (SPEC §4.3)",
		fn, v, intBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax)))
	r.pf(ind, "%s = %s", target, v)
}

// serializeInt128Lit is Int128Lit in the serialize port's own struct type.
func serializeInt128Lit(v *big.Int) string {
	lo, hi := Int128LitLanes(v)
	return fmt.Sprintf("serialize_int128 { lo: %s, hi: %s }", lo, hi)
}

// emitZeroItems sets every field under an untaken branch side to its ZERO
// form (SPEC §5: zero values, never the specified defaults).
func emitZeroItems(g *Gen, r *readBody, items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitZeroField(g, r, item.F, "value."+ir.GoExportName(item.F.Name), ind)
		case *ir.Branch:
			emitZeroItems(g, r, item.Then, ind)
			emitZeroItems(g, r, item.Else, ind)
		}
	}
}

func emitZeroField(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	t := f.Type
	buffer := t.Kind == ir.TString || t.Kind == ir.TBytes || t.Kind == ir.TWString
	size := t.Size
	if t.Kind == ir.TWString {
		size = 2 * t.Size // two bytes a code unit
	}
	switch {
	case f.Array != ir.ArrayNone && isByteElem(t):
		r.pf(ind, "%s = %s.zeros(%d)", target, RuntimeNamespace, f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			r.pf(ind, "%sCount = 0", target)
		}
	case f.Array != ir.ArrayNone:
		if f.Array == ir.ArrayCounted {
			r.pf(ind, "%sCount = 0", target)
		}
		r.pf(ind, "for(%d) i:", f.ArrayBound)
		e := *f
		e.Array = ir.ArrayNone
		if buffer {
			r.pf(ind+"    ", "%s[i] = %s.zeros(%d)", target, RuntimeNamespace, size)
			r.pf(ind+"    ", "%sLength[i] = 0", target)
			return
		}
		emitZeroField(g, r, &e, target+"[i]", ind+"    ")
	case buffer:
		r.pf(ind, "%s = %s.zeros(%d)", target, RuntimeNamespace, size)
		r.pf(ind, "%sLength = 0", target)
	case t.Kind == ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Union:
			r.pf(ind, "%s.Type = %s // the empty union (SPEC §4.8)", target, VariantName(t.Name+"Type", "None"))
		case *ir.Struct:
			for _, sf := range ref.Fields {
				emitZeroField(g, r, sf, target+"."+ir.GoExportName(sf.Name), ind)
			}
		case *ir.Enum:
			r.pf(ind, "%s = %s", target, VariantName(t.Name, "None"))
		default:
			r.pf(ind, "%s = 0", target)
		}
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		r.pf(ind, "%s = 0.0", target)
	case t.Kind == ir.TBool:
		r.pf(ind, "%s = false", target)
	case t.Width > 64:
		r.pf(ind, "%s = %s.Int128 { lo: 0, hi: 0 }", target, RuntimeNamespace)
	default:
		r.pf(ind, "%s = 0", target)
	}
}
