// write.go is the type wire's WRITE side (docs/PORT-LOBSTER.md, the
// type-wire-write card): for every packet struct and union it emits
// Write<Name>(s, v) against the VENDORED serialize port's write stream
// (internal/codegen/lobster/runtime/, schema-lobster-serialize-vendor).
// There is no private bit writer here; every bit on the wire is the port's.
// The bytes are docs/SPEC.md §4.3 and §6.1, the same ones the C++ target pins
// in testdata/wire/*.bin.
package lobster

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("write", writePart) }

// maxUint64 is 2^64-1 as a big integer.
var maxUint64 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))

// writeCtx numbers the temporaries of one generated writer, so every port
// call's (value, ok) pair has its own names (Lobster refuses a redefined
// identifier, and all modules of a unit share one namespace).
type writeCtx struct{ n int }

// writePart emits the write function for every packet struct and union of
// the file (docs/PORT-LOBSTER.md, THE TYPE MAPPING; SPEC §4.3).
func writePart(g *Gen) error {
	var any bool
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if d.IsTable {
				continue
			}
			any = true
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] != nil {
				continue
			}
			any = true
		}
	}
	if !any {
		return nil
	}
	needs := writeNeeds(g)
	for _, r := range []string{"serialize_stream", "serialize_bitpacker", "serialize_float", "serialize_fixed", "serialize_int128"} {
		if !needs[r] {
			continue
		}
		var has bool
		for _, have := range g.Runtimes {
			if have == r {
				has = true
			}
		}
		if !has {
			g.Runtimes = append(g.Runtimes, r)
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if d.IsTable {
				continue
			}
			emitWriteStruct(g, d)
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] != nil {
				continue
			}
			emitWriteUnion(g, d)
		}
	}
	return nil
}

// writeNeeds picks the vendored families a module's writers call. The
// stream and the bit packer are the bottom layer; float, fixed and int128
// join only where a field of that kind rides. Compressed floats and byte
// buffers are written with the bit packer directly, so their family modules
// (whose pinned text carries compiler warnings) are not imported here.
func writeNeeds(g *Gen) map[string]bool {
	n := map[string]bool{"serialize_stream": true, "serialize_bitpacker": true}
	var scan func([]ir.Item)
	scan = func(items []ir.Item) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				scanField(n, item.F)
			case *ir.Branch:
				scan(item.Then)
				scan(item.Else)
			}
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !d.IsTable {
				scan(d.Items)
			}
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] == nil {
				for _, v := range d.Variants {
					if v.F != nil {
						scanField(n, v.F)
					}
				}
			}
		}
	}
	return n
}

func scanField(n map[string]bool, f *ir.Field) {
	switch f.Type.Kind {
	case ir.TFloat32, ir.TFloat64:
		n["serialize_float"] = true
	case ir.TFixed:
		n["serialize_fixed"] = true
	}
	if f.Type.Kind == ir.TInt && f.Type.Width == 128 {
		n["serialize_int128"] = true
	}
}

// writeRef names a declaration's emitted writer, requiring its module when
// the declaration lives in another file (docs/PORT-LOBSTER.md, THE RUNTIME
// LAYOUT).
func (g *Gen) writeRef(name string) string {
	g.Import(g.moduleOf(name, false))
	return "Write" + name
}

// emitWriteStruct emits Write<Name>(s, v) for one struct, walking the wire
// tree so branches, const, reserved and align keep their places (SPEC §4.3,
// §4.5, §6.1: the wire order is the declared order).
func emitWriteStruct(g *Gen, st *ir.Struct) {
	g.Pf("// type %s (SPEC §6.1: the wire order is the declared order)\n", st.Name)
	g.Pf("def Write%s(s:serialize_write_stream, v:%s) -> bool:\n", st.Name, st.Name)
	c := &writeCtx{}
	emitWriteItems(g, c, st.Items, "    ")
	g.Pf("    return true\n\n")
}

func emitWriteItems(g *Gen, c *writeCtx, items []ir.Item, indent string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitWriteField(g, c, "v."+ir.GoExportName(item.F.Name), item.F, indent)
		case *ir.ConstItem:
			emitConstBits(g, c, item.Value, item.Bits, indent)
		case *ir.ReservedItem:
			emitZeroBits(g, c, item.Bits, indent)
		case *ir.AlignItem:
			g.Pf("%sif not s.serialize_align(): return false // align (SPEC §4.3)\n", indent)
		case *ir.Branch:
			cond := "v." + ir.GoExportName(item.Cond)
			if item.Neg {
				cond = "not " + cond
			}
			g.Pf("%sif %s: // the if/else branch rides only its taken side (SPEC §4.5)\n", indent, cond)
			emitWriteItems(g, c, item.Then, indent+"    ")
			if item.Else != nil {
				g.Pf("%selse:\n", indent)
				emitWriteItems(g, c, item.Else, indent+"    ")
			}
		}
	}
}

// emitCall emits one port write and its ok check. call is the port method
// call, whose (value, ok) pair is bound to fresh names; comment is the SPEC
// citation (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT: the port is the only
// bit writer).
func emitCall(g *Gen, c *writeCtx, indent, call, comment string) {
	c.n++
	g.Pf("%slet _w%d, _ok%d = %s\n", indent, c.n, c.n, call)
	g.Pf("%sif not _ok%d: return false%s\n", indent, c.n, comment)
}

// emitConstBits writes a const(value, bits) (SPEC §4.3).
func emitConstBits(g *Gen, c *writeCtx, v *big.Int, bits int64, indent string) {
	if bits == 0 {
		return
	}
	if bits > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(%s, %d)", IntLit(v), bits),
			fmt.Sprintf(" // const(%s, %d) (SPEC §4.3)", v.String(), bits))
		return
	}
	emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(%s, %d)", IntLit(v), bits),
		fmt.Sprintf(" // const(%s, %d) (SPEC §4.3)", v.String(), bits))
}

// emitZeroBits writes reserved(bits) zeros (SPEC §4.3).
func emitZeroBits(g *Gen, c *writeCtx, bits int64, indent string) {
	if bits == 0 {
		return
	}
	if bits > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(0, %d)", bits),
			fmt.Sprintf(" // reserved(%d) (SPEC §4.3)", bits))
		return
	}
	emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(0, %d)", bits),
		fmt.Sprintf(" // reserved(%d) (SPEC §4.3)", bits))
}

// emitWriteField writes one field, including its array framing (SPEC §4.6,
// §4.7): a counted array's count first, in [min, bound], then its elements.
func emitWriteField(g *Gen, c *writeCtx, val string, f *ir.Field, indent string) {
	if f.Array == ir.ArrayNone {
		emitWriteScalar(g, c, val, "", f, indent)
		return
	}
	if f.Array == ir.ArrayCounted {
		count := val + "Count"
		emitRangedCount(g, c, count, f.ArrayMin, f.ArrayBound, indent)
		g.Pf("%sfor(%s) _i: // the count guards the loop (SPEC §6.3)\n", indent, count)
		emitWriteElem(g, c, val, f, indent+"    ")
		return
	}
	if isByteElem(f.Type) && !f.HasIntRange {
		g.Pf("%sif not s.serialize_align(): return false // [N]uint8 bulk copy, aligned (SPEC §4.7)\n", indent)
		g.Pf("%ss.pack_bytes(%s, 0, %d)\n", indent, val, f.ArrayBound)
		g.Pf("%sif not s.ok(): return false\n", indent)
		return
	}
	g.Pf("%sfor(%d) _i:\n", indent, f.ArrayBound)
	emitWriteElem(g, c, val, f, indent+"    ")
}

// emitRangedCount writes a counted array's count in [lo, hi] (SPEC §4.6).
func emitRangedCount(g *Gen, c *writeCtx, val string, lo, hi int64, indent string) {
	g.Pf("%sif %s < %d or %s > %d: return false // the count is in [%d, %d] (SPEC §4.6)\n", indent, val, lo, val, hi, lo, hi)
	bits := ir.BitsRequired(big.NewInt(lo), big.NewInt(hi))
	if bits == 0 {
		return
	}
	expr := val
	if lo != 0 {
		expr = fmt.Sprintf("%s - (%d)", val, lo)
	}
	if bits > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(%s, %d)", expr, bits), " // the count (SPEC §4.6)")
		return
	}
	emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(%s, %d)", expr, bits), " // the count (SPEC §4.6)")
}

// emitWriteElem writes one array element, keeping a text element's
// used-length slot beside it (SPEC §4.7).
func emitWriteElem(g *Gen, c *writeCtx, val string, f *ir.Field, indent string) {
	elem := *f
	elem.Array = ir.ArrayNone
	if isByteElem(f.Type) {
		emitWriteScalar(g, c, fmt.Sprintf("read_uint8_le(%s, _i)", val), "", &elem, indent)
		return
	}
	length := ""
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		length = val + "Length[_i]"
	}
	emitWriteScalar(g, c, val+"[_i]", length, &elem, indent)
}

// emitWriteScalar writes one value of a field's kind (SPEC §4.3). length is
// the used-length expression for a buffer field; empty means the field's own
// <Name>Length slot.
func emitWriteScalar(g *Gen, c *writeCtx, val, length string, f *ir.Field, indent string) {
	t := f.Type
	call := func(method, comment string) { emitCall(g, c, indent, method, comment) }
	switch t.Kind {
	case ir.TBool:
		call("s.serialize_bool("+val+")", " // SPEC §4.3")
	case ir.TInt:
		switch {
		case t.Width == 128 && f.HasIntRange:
			if f.IntMin.Cmp(f.IntMax) == 0 {
				emitInt128Equal(g, val, f.IntMin, indent)
				return
			}
			minLo, minHi := Int128LitLanes(f.IntMin)
			maxLo, maxHi := Int128LitLanes(f.IntMax)
			call(fmt.Sprintf("s.serialize_int128(serialize_int128 { lo: %s.lo, hi: %s.hi }, serialize_int128 { lo: %s, hi: %s }, serialize_int128 { lo: %s, hi: %s })",
				val, val, minLo, minHi, maxLo, maxHi), " // int128, offset from min (SPEC §4.3)")
		case t.Width == 128:
			call(fmt.Sprintf("s.serialize_uint128(serialize_uint128 { lo: %s.lo, hi: %s.hi })", val, val), " // uint128 raw (SPEC §4.3)")
		case f.HasIntRange:
			emitWriteRangedInt(g, c, val, f, indent)
		default:
			emitWriteBareInt(g, c, val, t, indent)
		}
	case ir.TBits:
		if int64(t.Width) > 32 {
			call(fmt.Sprintf("s.serialize_bits64(%s, %d)", val, t.Width), " // the declared bit width (SPEC §4.3)")
		} else {
			call(fmt.Sprintf("s.serialize_bits(%s, %d)", val, t.Width), " // the declared bit width (SPEC §4.3)")
		}
	case ir.TFixed:
		emitWriteFixed(g, c, val, f, indent)
	case ir.TFloat32:
		if f.HasFloatRange {
			emitCompressedFloat(g, c, val, f, indent)
		} else {
			call(fmt.Sprintf("s.serialize_float(%s)", val), "")
		}
	case ir.TFloat64:
		call(fmt.Sprintf("s.serialize_double(%s)", val), "")
	case ir.TString, ir.TBytes:
		size := t.Size
		if length == "" {
			length = val + "Length"
		}
		g.Pf("%sif %s < 0 or %s > %d: return false // the length is in [0, %d] (SPEC §4.7)\n", indent, length, length, size, size)
		emitLength(g, c, length, big.NewInt(size), indent)
		g.Pf("%sif not s.serialize_align(): return false // bytes align first (SPEC §4.7)\n", indent)
		g.Pf("%ss.pack_bytes(%s, 0, %s) // the used bytes, the port's byte copier (SPEC §4.7)\n", indent, val, length)
		g.Pf("%sif not s.ok(): return false\n", indent)
	case ir.TWString:
		size := t.Size
		if length == "" {
			length = val + "Length"
		}
		g.Pf("%sif %s < 0 or %s > %d: return false // the code unit count is in [0, %d] (SPEC §4.12)\n", indent, length, length, size, size)
		emitLength(g, c, length, big.NewInt(size), indent)
		g.Pf("%sfor(%s) _u: // UTF-16 code unit (SPEC §4.12)\n", indent, length)
		emitCall(g, c, indent+"    ", fmt.Sprintf("s.serialize_bits(read_uint16_le(%s, _u * 2), 32)", val), "")
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			g.Pf("%sif %s < 0 or %s > %d: return false // the enum value (SPEC §4.2)\n", indent, val, val, ref.Max)
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			if bits == 0 {
				return
			}
			if bits > 32 {
				call(fmt.Sprintf("s.serialize_bits64(%s, %d)", val, bits), " // the enum value (SPEC §4.2)")
			} else {
				call(fmt.Sprintf("s.serialize_bits(%s, %d)", val, bits), " // the enum value (SPEC §4.2)")
			}
		case *ir.Flags:
			emitWriteFlags(g, c, val, ref.WireBits, indent)
		case *ir.Struct, *ir.Union:
			g.Pf("%sif not %s(s, %s): return false\n", indent, g.writeRef(t.Name), val)
		}
	}
}

// emitLength writes a buffer's used length in [0, max] (SPEC §4.7).
func emitLength(g *Gen, c *writeCtx, val string, max *big.Int, indent string) {
	bits := ir.BitsRequired(big.NewInt(0), max)
	if bits == 0 {
		return
	}
	if bits > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(%s, %d)", val, bits), " // the used length (SPEC §4.7)")
		return
	}
	emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(%s, %d)", val, bits), " // the used length (SPEC §4.7)")
}

// emitCompressedFloat writes a float32 with a declared [min, max, resolution]
// as its quantized step index (SPEC §4.3): the same clamped, float32-rounded
// arithmetic the vendored port's compressed-float family performs, over the
// port's bit writer. The parameters are folded at generation time exactly as
// ir.CompressedFloatParams derives them.
func emitCompressedFloat(g *Gen, c *writeCtx, val string, f *ir.Field, indent string) {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	c.n++
	n := c.n
	g.Pf("%slet _cf%d = schema_rt.fround(%s) // the float32 value (SPEC §4.3)\n", indent, n, val)
	if min32 == 0 {
		g.Pf("%svar _cn%d = schema_rt.fround(_cf%d / %s)\n", indent, n, n, FormatFloat32(float64(delta)))
	} else {
		g.Pf("%svar _cn%d = schema_rt.fround(schema_rt.fround(_cf%d - %s) / %s)\n",
			indent, n, n, FormatFloat32(float64(min32)), FormatFloat32(float64(delta)))
	}
	g.Pf("%sif not (_cn%d >= 0.0): _cn%d = 0.0 // the runtime's clamp form — NaN lands in range too (SPEC §4.3)\n", indent, n, n)
	g.Pf("%selif not (_cn%d <= 1.0): _cn%d = 1.0\n", indent, n, n)
	g.Pf("%svar _ci%d = floor(schema_rt.fround(schema_rt.fround(_cn%d * %s) + 0.5)) // every step is float32 (SPEC §4.3)\n", indent, n, n, FormatFloat(float64(steps)))
	g.Pf("%sif _ci%d > %d: _ci%d = %d // the integer clamp is normative (SPEC §4.3)\n", indent, n, steps, n, steps)
	if bits > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(_ci%d, %d)", n, bits), " // the step index (SPEC §4.3)")
	} else {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(_ci%d, %d)", n, bits), " // the step index (SPEC §4.3)")
	}
}

// emitWriteBareInt writes a bare integer at its storage width (SPEC §4.3):
// a signed narrow value narrows through its same-width mask, the bit pattern
// the Go/C++ targets write.
func emitWriteBareInt(g *Gen, c *writeCtx, val string, t ir.FieldType, indent string) {
	width := int64(t.Width)
	expr := val
	if t.Signed && width < 64 {
		expr = fmt.Sprintf("(%s & 0x%x)", val, uint64(1)<<uint(width)-1)
	}
	if width > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(%s, %d)", expr, width), " // the storage width (SPEC §4.3)")
		return
	}
	emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(%s, %d)", expr, width), " // the storage width (SPEC §4.3)")
}

// emitWriteRangedInt writes a ranged integer as its offset from min in its
// generation-time bit count (SPEC §4.3); a degenerate range is zero bits and
// only its value refusal (SPEC §4.6).
func emitWriteRangedInt(g *Gen, c *writeCtx, val string, f *ir.Field, indent string) {
	lo, hi := f.IntMin, f.IntMax
	bits := ir.BitsRequired(lo, hi)
	if bits == 0 {
		g.Pf("%sif %s != %s: return false // the degenerate range's one value (SPEC §4.6)\n", indent, val, IntLit(lo))
		return
	}
	// The write-side refusal, in the domain the bounds can be compared in.
	switch {
	case lo.Sign() == 0 && hi.Cmp(maxUint64) == 0:
		// full uint64: every bit pattern is a value
	case hi.Cmp(big.NewInt(math.MaxInt64)) <= 0:
		g.Pf("%sif %s < %s or %s > %s: return false // the value is in [min, max] (SPEC §4.3)\n", indent, val, IntLit(lo), val, IntLit(hi))
	default:
		g.Pf("%sif %s < %s or schema_rt.ult(%s, %s): return false // the unsigned upper bound (SPEC §4.3)\n", indent, val, IntLit(lo), val, IntLit(hi))
	}
	expr := val
	if lo.Sign() != 0 {
		expr = fmt.Sprintf("%s - (%s)", val, IntLit(lo))
	}
	if bits > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(%s, %d)", expr, bits), " // the offset from min (SPEC §4.3)")
		return
	}
	emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(%s, %d)", expr, bits), " // the offset from min (SPEC §4.3)")
}

// emitWriteFixed writes a fixed point value through the vendored port's Q
// format path (SPEC §4.3); a degenerate range is its one raw value and zero
// bits (SPEC §4.6).
func emitWriteFixed(g *Gen, c *writeCtx, val string, f *ir.Field, indent string) {
	t := f.Type
	if f.IntMin.Cmp(f.IntMax) == 0 {
		raw := new(big.Int).Lsh(f.IntMin, uint(t.FracBits))
		if t.Width > 64 {
			emitInt128Equal(g, val, raw, indent)
			return
		}
		g.Pf("%sif %s != %s: return false // the degenerate range's one raw value (SPEC §4.6)\n", indent, val, IntLit(raw))
		return
	}
	switch {
	case t.Width > 64:
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_fixed128(serialize_int128 { lo: %s.lo, hi: %s.hi }, %d, %d, %s, %s)",
			val, val, t.IntBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax)), " // the Q format's raw offset (SPEC §4.3)")
	case t.Width > 32:
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_fixed64(%s, %d, %d, %s, %s)",
			val, t.IntBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax)), " // the Q format's raw offset (SPEC §4.3)")
	default:
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_fixed32(%s, %d, %d, %s, %s)",
			val, t.IntBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax)), " // the Q format's raw offset (SPEC §4.3)")
	}
}

// emitInt128Equal refuses a value that is not the degenerate range's one
// 128-bit raw value; zero bits ride (SPEC §4.6).
func emitInt128Equal(g *Gen, val string, v *big.Int, indent string) {
	lo, hi := Int128LitLanes(v)
	g.Pf("%sif %s.lo != %s or %s.hi != %s: return false // the degenerate range's one value (SPEC §4.6)\n",
		indent, val, lo, val, hi)
}

// emitWriteFlags writes a flags mask at its wire width (SPEC §4.2).
func emitWriteFlags(g *Gen, c *writeCtx, val string, wireBits int, indent string) {
	if wireBits < 64 {
		g.Pf("%sif %s < 0 or %s >= 0x%x: return false // the mask fits %d bits (SPEC §4.2)\n",
			indent, val, val, uint64(1)<<uint(wireBits), wireBits)
	}
	if wireBits > 32 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits64(%s, %d)", val, wireBits), " // the flags wire width (SPEC §4.2)")
	} else if wireBits > 0 {
		emitCall(g, c, indent, fmt.Sprintf("s.serialize_bits(%s, %d)", val, wireBits), " // the flags wire width (SPEC §4.2)")
	}
}

// emitWriteUnion emits Write<Name>(s, v): the tag in minimal bits for
// [0, Max], validated BEFORE it rides, then the selected arm only (SPEC §4.8).
func emitWriteUnion(g *Gen, un *ir.Union) {
	tag := un.Name + "Type"
	c := &writeCtx{}
	g.Pf("// union %s (SPEC §4.8: None = 0, then each declared arm)\n", un.Name)
	g.Pf("def Write%s(s:serialize_write_stream, v:%s) -> bool:\n", un.Name, un.Name)
	g.Pf("    if v.Type < 0 or v.Type > %d: return false // the tag validates BEFORE it rides (SPEC §4.8)\n", un.Max)
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(un.Max))
	if bits > 32 {
		emitCall(g, c, "    ", fmt.Sprintf("s.serialize_bits64(v.Type, %d)", bits), " // the union tag (SPEC §4.8)")
	} else if bits > 0 {
		emitCall(g, c, "    ", fmt.Sprintf("s.serialize_bits(v.Type, %d)", bits), " // the union tag (SPEC §4.8)")
	}
	for _, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		key := ir.GoExportName(arm.Name)
		g.Pf("    if v.Type == %s: // the selected arm only (SPEC §4.8)\n", VariantName(tag, arm.Name))
		emitWriteField(g, c, "v."+key, arm.F, "        ")
	}
	g.Pf("    return true\n\n")
}

// Int128LitLanes renders a 128-bit value as the two lanes the runtime's
// Int128 struct stores, low first (docs/PORT-LOBSTER.md, THE TYPE MAPPING).
func Int128LitLanes(v *big.Int) (string, string) {
	m128 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	bits := new(big.Int).And(v, m128)
	lo := new(big.Int).And(bits, mask64)
	hi := new(big.Int).Rsh(bits, 64)
	return fmt.Sprintf("0x%016x", lo), fmt.Sprintf("0x%016x", hi)
}
