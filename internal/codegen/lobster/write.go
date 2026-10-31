// write.go is the type wire's WRITE side (docs/PORT-LOBSTER.md, the
// type-wire-write card): for every packet struct and union it emits
// Write<Name>(s, v) against the VENDORED serialize.lobster v2.1.1 write stream
// (internal/codegen/lobster/runtime/). There is no private bit writer here;
// every bit on the wire is the port's, through its public surface (its
// DESIGN.md, "The public surface"). The bytes are docs/SPEC.md §4.3 and §6.1,
// the same ones the C++ target pins in testdata/wire/*.bin.
//
// THE CHECK MODEL (serialize.lobster IMPLEMENTING.md U9; SPEC §5). Write misuse
// the runtime can see (a value wider than its bits, a NaN or an infinity, a
// compressed float that is not finite, a fixed or int128 value outside its
// range, a payload longer than its buffer, a write that does not fit) TRAPS in
// every run: serialize_abort prints the rule and exits 70, so a write call
// never answers false and none is checked. What only the schema knows (a
// counted array's count, a union tag, an enum value, a ranged integer, a
// string's length and text, a flags mask) is a write-side CONTRACT, held by
// Lobster's build-removable assert (--runtime-no-asserts drops it), as C++
// holds it with serialize_assert and C# with Debug.Assert.
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

// wireRuntimes are the modules a module with a packet wire imports: the
// vendored port's entry (every family; v2.1.1 compiles with no warning) and
// this target's wire helpers (runtime/schema_wire.lobster).
var wireRuntimes = []string{"serialize", "schema_wire"}

// packetDecls reports whether the file declares a packet struct or union,
// the declarations the type wire reads and writes.
func packetDecls(g *Gen) bool {
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !d.IsTable {
				return true
			}
		case *ir.Union:
			if g.Unit.TableUnions[d.Name] == nil {
				return true
			}
		}
	}
	return false
}

// needWireRuntimes adds the wire's runtime modules to the module's imports.
func needWireRuntimes(g *Gen) {
	for _, r := range wireRuntimes {
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
}

// writeCtx carries what one generated writer needs beside the field: the
// statically byte-aligned [N]uint8 arrays of the struct being written
// (ir.AlignedFixedByteArrays), which take the bulk copy.
type writeCtx struct {
	bulk map[*ir.Field]bool
}

// writePart emits the write function for every packet struct and union of
// the file (docs/PORT-LOBSTER.md, THE TYPE MAPPING; SPEC §4.3).
func writePart(g *Gen) error {
	if !packetDecls(g) {
		return nil
	}
	needWireRuntimes(g)
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
	g.Pf("// Write%s writes v in wire order (SPEC §6.1). Write misuse the runtime\n", st.Name)
	g.Pf("// sees traps in every run; the schema's own contracts are asserts.\n")
	g.Pf("def Write%s(s:serialize_write_stream, v:%s) -> bool:\n", st.Name, st.Name)
	c := &writeCtx{bulk: ir.AlignedFixedByteArrays(st)}
	emitWriteItems(g, c, st.Items, "    ")
	g.Pf("    return true\n\n")
}

func emitWriteItems(g *Gen, c *writeCtx, items []ir.Item, indent string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitWriteField(g, c, "v."+ir.GoExportName(item.F.Name), item.F, indent)
		case *ir.ConstItem:
			emitBits(g, IntLit(item.Value), item.Bits, indent, fmt.Sprintf("const(%s, %d) (SPEC §4.3)", item.Value.String(), item.Bits))
		case *ir.ReservedItem:
			for left := item.Bits; left > 0; left -= 64 {
				emitBits(g, "0", min(left, 64), indent, fmt.Sprintf("reserved(%d) (SPEC §4.3)", item.Bits))
			}
		case *ir.AlignItem:
			g.Pf("%ss.serialize_align() // align (SPEC §4.3)\n", indent)
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

// emitBits writes expr in bits wire bits through the port's bits primitive
// (serialize_bits to 32, serialize_bits64 above); zero bits write nothing.
func emitBits(g *Gen, expr string, bits int64, indent, comment string) {
	switch {
	case bits <= 0:
		return
	case bits > 32:
		g.Pf("%ss.serialize_bits64(%s, %d) // %s\n", indent, expr, bits, comment)
	default:
		g.Pf("%ss.serialize_bits(%s, %d) // %s\n", indent, expr, bits, comment)
	}
}

// emitWriteField writes one field, including its array framing (SPEC §4.6,
// §4.7): a counted array's count first, in [min, bound], then its elements.
func emitWriteField(g *Gen, c *writeCtx, val string, f *ir.Field, indent string) {
	if f.Array == ir.ArrayNone {
		emitWriteScalar(g, val, "", f, indent)
		return
	}
	if f.Array == ir.ArrayCounted {
		count := val + "Count"
		emitRangedCount(g, count, f.ArrayMin, f.ArrayBound, indent)
		g.Pf("%sfor(%s) _i: // the count guards the loop (SPEC §6.3)\n", indent, count)
		emitWriteElem(g, val, f, indent+"    ")
		return
	}
	if isByteElem(f.Type) && !f.HasIntRange && c.bulk[f] {
		g.Pf("%ss.serialize_bytes(%s, %d) // [N]uint8 at a byte-aligned position: one bulk copy, wire-identical to the per-byte loop (SPEC §4.7)\n", indent, val, f.ArrayBound)
		return
	}
	g.Pf("%sfor(%d) _i:\n", indent, f.ArrayBound)
	emitWriteElem(g, val, f, indent+"    ")
}

// emitRangedCount writes a counted array's count in [lo, hi] (SPEC §4.6).
func emitRangedCount(g *Gen, val string, lo, hi int64, indent string) {
	// a write-side contract: Lobster's assert is the build-removable idiom
	// (--runtime-no-asserts drops it), as C++'s serialize_assert is
	g.Pf("%sassert %s >= %d and %s <= %d // the count is in [%d, %d] (SPEC §4.6)\n", indent, val, lo, val, hi, lo, hi)
	expr := val
	if lo != 0 {
		expr = fmt.Sprintf("%s - (%d)", val, lo)
	}
	emitBits(g, expr, ir.BitsRequired(big.NewInt(lo), big.NewInt(hi)), indent, "the count (SPEC §4.6)")
}

// emitWriteElem writes one array element, keeping a text element's
// used-length slot beside it (SPEC §4.7).
func emitWriteElem(g *Gen, val string, f *ir.Field, indent string) {
	elem := *f
	elem.Array = ir.ArrayNone
	if isByteElem(f.Type) {
		emitWriteScalar(g, fmt.Sprintf("read_uint8_le(%s, _i)", val), "", &elem, indent)
		return
	}
	length := ""
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		length = val + "Length[_i]"
	}
	emitWriteScalar(g, val+"[_i]", length, &elem, indent)
}

// emitWriteScalar writes one value of a field's kind (SPEC §4.3). length is
// the used-length expression for a buffer field; empty means the field's own
// <Name>Length slot.
func emitWriteScalar(g *Gen, val, length string, f *ir.Field, indent string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		g.Pf("%ss.serialize_bool(%s) // SPEC §4.3\n", indent, val)
	case ir.TInt:
		switch {
		case t.Width == 128 && f.HasIntRange:
			if f.IntMin.Cmp(f.IntMax) == 0 {
				emitInt128Equal(g, val, f.IntMin, indent)
				return
			}
			minLo, minHi := Int128LitLanes(f.IntMin)
			maxLo, maxHi := Int128LitLanes(f.IntMax)
			g.Pf("%ss.serialize_int128(serialize_int128 { lo: %s.lo, hi: %s.hi }, serialize_int128 { lo: %s, hi: %s }, serialize_int128 { lo: %s, hi: %s }) // int128, offset from min (SPEC §4.3)\n",
				indent, val, val, minLo, minHi, maxLo, maxHi)
		case t.Width == 128:
			g.Pf("%ss.serialize_uint128(serialize_uint128 { lo: %s.lo, hi: %s.hi }) // uint128 raw (SPEC §4.3)\n", indent, val, val)
		case f.HasIntRange:
			emitWriteRangedInt(g, val, f, indent)
		default:
			emitWriteBareInt(g, val, t, indent)
		}
	case ir.TBits:
		emitBits(g, val, int64(t.Width), indent, "the declared bit width (SPEC §4.3)")
	case ir.TFixed:
		emitWriteFixed(g, val, f, indent)
	case ir.TFloat32:
		if f.HasFloatRange {
			emitCompressedFloat(g, val, f, indent)
			return
		}
		g.Pf("%ss.serialize_float(%s) // finite only: NaN and ±inf trap (SPEC §4.3)\n", indent, val)
	case ir.TFloat64:
		g.Pf("%ss.serialize_double(%s) // finite only: NaN and ±inf trap (SPEC §4.3)\n", indent, val)
	case ir.TString, ir.TBytes:
		size := t.Size
		if length == "" {
			length = val + "Length"
		}
		g.Pf("%sassert %s >= 0 and %s <= %d // the used length is in [0, %d] (SPEC §4.7)\n", indent, length, length, size, size)
		if t.Kind == ir.TString {
			g.Pf("%sassert %s.text_refusal(%s, %s) == serialize_refusal_none // no interior NUL, well-formed UTF-8 (SPEC §4.7)\n", indent, RuntimeNamespace, val, length)
		}
		emitBits(g, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), indent, "the used length (SPEC §4.7)")
		g.Pf("%ss.serialize_bytes(%s, %s) // aligns, then the used bytes (SPEC §4.7)\n", indent, val, length)
	case ir.TWString:
		// length in [0, N] code units, then one 32-bit group per unit and NO
		// align anywhere: the classic wstring framing, carried by this
		// target over the port's bits primitive (serialize.lobster v2.1.1,
		// format 2.0, has no wstring; SPEC §4.12). A zero unit is writer
		// misuse, asserted; surrogate pairing is the reader's to refuse.
		size := t.Size
		if length == "" {
			length = val + "Length"
		}
		g.Pf("%sassert %s >= 0 and %s <= %d // the code unit count is in [0, %d] (SPEC §4.12)\n", indent, length, length, size, size)
		g.Pf("%sassert %s.units_nonzero(%s, %s) // no zero code unit (SPEC §4.12)\n", indent, RuntimeNamespace, val, length)
		emitBits(g, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), indent, "the code unit count (SPEC §4.12)")
		g.Pf("%sfor(%s) _u: // one 32-bit group per UTF-16 code unit (SPEC §4.12)\n", indent, length)
		g.Pf("%s    s.serialize_bits(read_uint16_le(%s, _u * 2), 32)\n", indent, val)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			g.Pf("%sassert %s >= 0 and %s <= %d // the enum value (SPEC §4.2)\n", indent, val, val, ref.Max)
			emitBits(g, val, ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), indent, "the enum value (SPEC §4.2)")
		case *ir.Flags:
			if ref.WireBits < 64 {
				g.Pf("%sassert %s >= 0 and %s < 0x%x // the mask fits %d bits (SPEC §4.2)\n",
					indent, val, val, uint64(1)<<uint(ref.WireBits), ref.WireBits)
			}
			emitBits(g, val, int64(ref.WireBits), indent, "the flags wire width (SPEC §4.2)")
		case *ir.Struct, *ir.Union:
			g.Pf("%s%s(s, %s)\n", indent, g.writeRef(t.Name), val)
		}
	}
}

// emitCompressedFloat writes a float32 with a declared [min, max, resolution]
// through the port's precomputed compressed float (SPEC §4.3), the parameters
// folded at generation time exactly as ir.CompressedFloatParams derives them,
// as the C++ target folds them. NaN and ±inf trap: never clamped (#2203).
func emitCompressedFloat(g *Gen, val string, f *ir.Field, indent string) {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	g.Pf("%ss.serialize_compressed_float_precomputed(%s, %d, %d, %s, %s) // the step index; a non-finite value traps (SPEC §4.3)\n",
		indent, val, steps, bits, FormatFloat32(float64(delta)), FormatFloat32(float64(min32)))
}

// emitWriteBareInt writes a bare integer at its storage width (SPEC §4.3):
// a signed narrow value narrows through its same-width mask, the bit pattern
// the Go/C++ targets write.
func emitWriteBareInt(g *Gen, val string, t ir.FieldType, indent string) {
	width := int64(t.Width)
	expr := val
	if t.Signed && width < 64 {
		expr = fmt.Sprintf("(%s & 0x%x)", val, uint64(1)<<uint(width)-1)
	}
	emitBits(g, expr, width, indent, "the storage width (SPEC §4.3)")
}

// emitWriteRangedInt writes a ranged integer as its offset from min in its
// generation-time bit count (SPEC §4.3); a degenerate range is zero bits and
// only its contract (SPEC §4.6).
func emitWriteRangedInt(g *Gen, val string, f *ir.Field, indent string) {
	lo, hi := f.IntMin, f.IntMax
	bits := ir.BitsRequired(lo, hi)
	if bits == 0 {
		g.Pf("%sassert %s == %s // the degenerate range's one value (SPEC §4.6)\n", indent, val, IntLit(lo))
		return
	}
	// The write-side contract, in the domain the bounds can be compared in.
	switch {
	case lo.Sign() == 0 && hi.Cmp(maxUint64) == 0:
		// full uint64: every bit pattern is a value
	case hi.Cmp(big.NewInt(math.MaxInt64)) <= 0:
		g.Pf("%sassert %s >= %s and %s <= %s // the value is in [min, max] (SPEC §4.3)\n", indent, val, IntLit(lo), val, IntLit(hi))
	default:
		g.Pf("%sassert not %s.ult(%s, %s) and not %s.ult(%s, %s) // the value is in [min, max], unsigned (SPEC §4.3)\n",
			indent, RuntimeNamespace, val, IntLit(lo), RuntimeNamespace, IntLit(hi), val)
	}
	expr := val
	if lo.Sign() != 0 {
		expr = fmt.Sprintf("%s - (%s)", val, IntLit(lo))
	}
	emitBits(g, expr, bits, indent, "the offset from min (SPEC §4.3)")
}

// emitWriteFixed writes a fixed point value through the vendored port's Q
// format path (SPEC §4.3), which traps on a value outside its range; a
// degenerate range is its one raw value and zero bits (SPEC §4.6).
func emitWriteFixed(g *Gen, val string, f *ir.Field, indent string) {
	t := f.Type
	if f.IntMin.Cmp(f.IntMax) == 0 {
		raw := new(big.Int).Lsh(f.IntMin, uint(t.FracBits))
		if t.Width > 64 {
			emitInt128Equal(g, val, raw, indent)
			return
		}
		g.Pf("%sassert %s == %s // the degenerate range's one raw value (SPEC §4.6)\n", indent, val, IntLit(raw))
		return
	}
	fn, intBits, widened := fixedDeclaration(f)
	switch {
	case t.Width > 64:
		g.Pf("%ss.serialize_fixed128(serialize_int128 { lo: %s.lo, hi: %s.hi }, %d, %d, %s, %s) // the Q format's raw offset (SPEC §4.3)\n",
			indent, val, val, intBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
	case widened:
		// a ufixed whose raw values reach 2^63: the 64-bit pattern
		// zero-extended into the 128-bit signed declaration
		g.Pf("%ss.serialize_fixed128(serialize_int128 { lo: %s, hi: 0 }, %d, %d, %s, %s) // the Q format's raw offset, unsigned (SPEC §4.3)\n",
			indent, val, intBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
	default:
		g.Pf("%ss.%s(%s, %d, %d, %s, %s) // the Q format's raw offset (SPEC §4.3)\n",
			indent, fn, val, intBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
	}
}

// fixedDeclaration is the serialize.lobster declaration a fixed field is
// written and read through: the operation, the integer_bits it passes, and
// whether a 64-bit-or-narrower value rides the 128-bit operation.
//
// serialize's fixed operations take a SIGNED Q format (STANDARD.md, fixed: the
// sign bit counts toward integer_bits, and the runtime refuses a declaration
// whose bounds the signed storage cannot hold). The wire depends only on the
// fraction bits and the raw bounds (min << F, max << F), never on
// integer_bits, so a ufixed(I, F) (SPEC §4.3) is passed as the widest signed
// declaration of the same F that holds its raw values, and the bytes are the
// same: storage 32 (integer_bits 32 - F) while its raw maximum stays below
// 2^31, so the common narrow ufixed keeps the 32-bit operation; storage 64
// (integer_bits 64 - F) while it stays below 2^63; else storage 128
// (integer_bits 128 - F) with the 64-bit pattern zero-extended. A raw value
// from 2^31 up never takes the 32-bit operation, because it answers the raw
// value sign-extended from bit 31. A signed fixed passes its own declaration.
func fixedDeclaration(f *ir.Field) (fn string, intBits int, widened bool) {
	t := f.Type
	switch {
	case t.Width > 64:
		return "serialize_fixed128", t.IntBits, false
	case t.Signed && t.Width > 32:
		return "serialize_fixed64", t.IntBits, false
	case t.Signed:
		return "serialize_fixed32", t.IntBits, false
	}
	rawMax := new(big.Int).Lsh(f.IntMax, uint(t.FracBits))
	if rawMax.Cmp(big.NewInt(math.MaxInt32)) <= 0 && t.FracBits < 32 {
		return "serialize_fixed32", 32 - t.FracBits, false
	}
	if rawMax.Cmp(big.NewInt(math.MaxInt64)) <= 0 {
		return "serialize_fixed64", 64 - t.FracBits, false
	}
	return "serialize_fixed128", 128 - t.FracBits, true
}

// emitInt128Equal asserts the degenerate range's one 128-bit raw value; zero
// bits ride (SPEC §4.6).
func emitInt128Equal(g *Gen, val string, v *big.Int, indent string) {
	lo, hi := Int128LitLanes(v)
	g.Pf("%sassert %s.lo == %s and %s.hi == %s // the degenerate range's one value (SPEC §4.6)\n",
		indent, val, lo, val, hi)
}

// emitWriteUnion emits Write<Name>(s, v): the tag in minimal bits for
// [0, Max], a write-side contract asserted BEFORE it rides, then the
// selected arm only (SPEC §4.8).
func emitWriteUnion(g *Gen, un *ir.Union) {
	tag := un.Name + "Type"
	c := &writeCtx{}
	g.Pf("// Write%s writes the tag, then the selected arm only (SPEC §4.8: None = 0,\n", un.Name)
	g.Pf("// then each declared arm).\n")
	g.Pf("def Write%s(s:serialize_write_stream, v:%s) -> bool:\n", un.Name, un.Name)
	g.Pf("    assert v.Type >= 0 and v.Type <= %d // the tag is a write-side contract (SPEC §4.8)\n", un.Max)
	emitBits(g, "v.Type", ir.BitsRequired(big.NewInt(0), big.NewInt(un.Max)), "    ", "the union tag (SPEC §4.8)")
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
