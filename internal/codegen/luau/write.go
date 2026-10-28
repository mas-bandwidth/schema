// write.go is the type wire's WRITE side (docs/PORT-LUAU.md, the type-wire
// write card): for every packet struct and union it emits
// Write<Name>(stream, v) against the vendored serialize port's WriteStream
// (internal/codegen/luau/runtime/, schema-luau-serialize-vendor). There is no
// private bit writer here; every bit on the wire is the port's. The bytes are
// SPEC.md §4.3 and §6.1, the same ones the C++ target pins in
// testdata/wire/*.bin. A value outside its declared range, or of the wrong
// Luau storage type, is refused before it rides.
package luau

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var maxInt64 = big.NewInt(math.MaxInt64)

func IntLiteral(v *big.Int) string { return v.String() }

func FloatLiteral(v float64) string { return FormatFloat(v) }

func init() {
	Register("write", writePart)
}

func writePart(g *Gen) error {
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

// writeRef names a declaration's emitted writer, requiring its module when the
// declaration lives in another file (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
func (g *Gen) writeRef(name string) string {
	if b, ok := g.Unit.DeclFile[name]; ok && b != g.File.Base {
		return g.Require(b) + ".Write" + name
	}
	return g.Self + ".Write" + name
}

// emitWriteStruct emits Write<Name>(stream, v) for one struct, walking the
// wire tree so branches, const, reserved and align keep their places
// (ir.Struct.Items; SPEC §4.3, §4.5).
func emitWriteStruct(g *Gen, st *ir.Struct) {
	g.Pf("-- type %s (SPEC §6.1: the wire order is the declared order)\n", st.Name)
	g.Pf("function %s.Write%s(stream: any, v: any)\n", g.Self, st.Name)
	emitWriteItems(g, st.Items, "  ")
	g.Pf("  return true\nend\n\n")
}

func emitWriteItems(g *Gen, items []ir.Item, indent string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitWriteField(g, "v."+ir.GoExportName(item.F.Name), item.F, indent)
		case *ir.ConstItem:
			g.Pf("%sif not %s then return false end -- const(%s, %d) — SPEC §4.3\n",
				indent, callBits(item.Value, item.Bits), item.Value.String(), item.Bits)
		case *ir.ReservedItem:
			g.Pf("%sif not %s then return false end -- reserved(%d): zeros on the wire (SPEC §4.3)\n",
				indent, callBits(big.NewInt(0), item.Bits), item.Bits)
		case *ir.AlignItem:
			g.Pf("%sif not stream:serializeAlign() then return false end -- SPEC §4.3\n", indent)
		case *ir.Branch:
			cond := "v." + ir.GoExportName(item.Cond)
			if item.Neg {
				cond = "not " + cond
			}
			g.Pf("%sif %s then\n", indent, cond)
			emitWriteItems(g, item.Then, indent+"  ")
			if item.Else != nil {
				g.Pf("%selse\n", indent)
				emitWriteItems(g, item.Else, indent+"  ")
			}
			g.Pf("%send\n", indent)
		}
	}
}

// emitWriteField writes one field, including its array framing (SPEC §4.6,
// §4.7): a counted array's count first, in [min, bound], then the elements.
func emitWriteField(g *Gen, val string, f *ir.Field, indent string) {
	if f.Array == ir.ArrayNone {
		emitWriteScalar(g, val, "", f, indent)
		return
	}
	bound := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		count := val + "Count"
		g.Pf("%sif type(%s) ~= \"number\" or %s < %d or %s > %s then return false end -- the count guards the loop (SPEC §6.3)\n",
			indent, count, count, f.ArrayMin, count, bound)
		g.Pf("%sif not stream:serializeInt({ value = %s }, %d, %s) then return false end -- the count (SPEC §4.6)\n",
			indent, count, f.ArrayMin, bound)
		g.Pf("%sfor _i = 1, %s do\n", indent, count)
		emitWriteElem(g, val, f, indent+"  ")
		g.Pf("%send\n", indent)
		return
	}
	if isByteElem(f.Type) && !f.HasIntRange {
		g.Pf("%sif not stream:serializeBytes(%s) then return false end -- [N]uint8 bulk copy (SPEC §4.7)\n",
			indent, val)
		return
	}
	g.Pf("%sfor _i = 1, %s do\n", indent, bound)
	emitWriteElem(g, val, f, indent+"  ")
	g.Pf("%send\n", indent)
}

// emitWriteElem writes one array element, keeping a text element's used-length
// slot beside it (SPEC §4.7).
func emitWriteElem(g *Gen, val string, f *ir.Field, indent string) {
	elem := *f
	elem.Array = ir.ArrayNone
	if isByteElem(f.Type) {
		emitWriteScalar(g, "buffer.readu8("+val+", _i - 1)", "", &elem, indent)
		return
	}
	length := ""
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		length = val + "Length[_i]"
	}
	emitWriteScalar(g, val+"[_i]", length, &elem, indent)
}

// emitWriteScalar writes one value of a field's kind (SPEC §4.3's table).
// length is the used-length expression for a buffer field; empty means the
// field's own <Name>Length slot.
func emitWriteScalar(g *Gen, val, length string, f *ir.Field, indent string) {
	t := f.Type
	call := func(expr, comment string) {
		g.Pf("%sif not %s then return false end%s\n", indent, expr, comment)
	}
	switch t.Kind {
	case ir.TBool:
		call(fmt.Sprintf("stream:serializeBool({ value = %s })", val), " -- SPEC §4.3")
	case ir.TInt:
		switch {
		case t.Width == 128 && f.HasIntRange:
			call(fmt.Sprintf("stream:serializeInt128({ value = %s }, %s, %s)", val,
				Int128Lit(f.IntMin), Int128Lit(f.IntMax)), " -- int128, offset from min (SPEC §4.3)")
		case t.Width == 128:
			call(fmt.Sprintf("stream:serializeUint128({ value = %s })", val), " -- uint128 raw (SPEC §4.3)")
		case t.Width == 64 && f.HasIntRange:
			lo, hi := IntegerLit(f.IntMin), IntegerLit(f.IntMax)
			if f.IntMax.Cmp(maxInt64) > 0 {
				g.Pf("%sif typeof(%s) ~= \"integer\" or integer.ult(%s, %s) or integer.ugt(%s, %s) then return false end\n",
					indent, val, val, lo, val, hi)
			} else {
				g.Pf("%sif typeof(%s) ~= \"integer\" or integer.lt(%s, %s) or integer.gt(%s, %s) then return false end\n",
					indent, val, val, lo, val, hi)
			}
			bits := ir.BitsRequired(f.IntMin, f.IntMax)
			if bits > 0 {
				g.Pf("%sif not stream:serializeBits64({ value = integer.sub(%s, %s) }, %d) then return false end -- the offset from min (SPEC §4.3)\n",
					indent, val, lo, bits)
			}
		case t.Width == 64:
			g.Pf("%sif typeof(%s) ~= \"integer\" then return false end\n", indent, val)
			g.Pf("%sif not stream:serializeBits64({ value = %s }, 64) then return false end -- the storage width (SPEC §4.3)\n", indent, val)
		case f.HasIntRange && f.IntMin.Cmp(big.NewInt(math.MinInt32)) >= 0 && f.IntMax.Cmp(big.NewInt(math.MaxInt32)) <= 0:
			call(fmt.Sprintf("stream:serializeInt({ value = %s }, %s, %s)", val,
				IntLiteral(f.IntMin), IntLiteral(f.IntMax)), " -- int, offset from min (SPEC §4.3)")
		case f.HasIntRange:
			lo, hi := IntLiteral(f.IntMin), IntLiteral(f.IntMax)
			g.Pf("%sif type(%s) ~= \"number\" or %s %% 1 ~= 0 or %s < %s or %s > %s then return false end\n",
				indent, val, val, val, lo, val, hi)
			emitBitsOrSkip(g, indent, val+" - ("+lo+")", ir.BitsRequired(f.IntMin, f.IntMax), "the offset from min")
		default:
			// The storage's domain is checked before the bits ride: the
			// runtime traps a value that does not fit its bits (serialize
			// format 2.0, IMPLEMENTING.md U9), so a value the storage cannot
			// hold refuses here instead. A signed storage rides its
			// same-width unsigned pattern (the C++ target's cast; SPEC §4.3).
			lo, hi := intStorageBounds(t.Signed, t.Width)
			g.Pf("%sif type(%s) ~= \"number\" or %s %% 1 ~= 0 or %s < %s or %s > %s then return false end\n",
				indent, val, val, val, lo, val, hi)
			expr := val
			if t.Signed {
				expr = fmt.Sprintf("%s %% %s", val, IntLiteral(new(big.Int).Lsh(big.NewInt(1), uint(t.Width))))
			}
			emitBitsOrSkip(g, indent, expr, int64(t.Width), "the storage width")
		}
	case ir.TBits:
		if t.Width > 32 {
			g.Pf("%sif typeof(%s) ~= \"integer\" then return false end\n", indent, val)
			emitBits64OrSkip(g, indent, val, int64(t.Width), "the declared bit width")
		} else {
			g.Pf("%sif type(%s) ~= \"number\" or %s %% 1 ~= 0 or %s < 0 or %s > %s then return false end\n",
				indent, val, val, val, val, IntLiteral(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1))))
			emitBitsOrSkip(g, indent, val, int64(t.Width), "the declared bit width")
		}
	case ir.TFixed:
		switch {
		case t.Width > 64:
			call(fmt.Sprintf("stream:serializeFixed128({ value = %s }, %d, %d, %s, %s)", val,
				t.IntBits, t.FracBits, Int128Lit(f.IntMin), Int128Lit(f.IntMax)), " -- the Q format's raw offset (SPEC §4.3)")
		case t.Width > 32:
			call(fmt.Sprintf("stream:serializeFixed64({ value = %s }, %d, %d, %s, %s)", val,
				t.IntBits, t.FracBits, IntegerLit(f.IntMin), IntegerLit(f.IntMax)), " -- the Q format's raw offset (SPEC §4.3)")
		default:
			call(fmt.Sprintf("stream:serializeInt({ value = %s }, %s, %s)", val,
				IntLiteral(f.IntMin), IntLiteral(f.IntMax)), " -- the Q format's raw offset (SPEC §4.3)")
		}
	case ir.TFloat32:
		if f.HasFloatRange {
			call(fmt.Sprintf("stream:serializeCompressedFloat({ value = %s }, %s, %s, %s)", val,
				FloatLiteral(f.FMin), FloatLiteral(f.FMax), FloatLiteral(f.Resolution)), " -- the step index (SPEC §4.3)")
		} else {
			call(fmt.Sprintf("stream:serializeFloat({ value = %s })", val), "")
		}
	case ir.TFloat64:
		call(fmt.Sprintf("stream:serializeDouble({ value = %s })", val), "")
	case ir.TString, ir.TBytes:
		size := t.Size
		if length == "" {
			length = val + "Length"
		}
		g.Pf("%sif type(%s) ~= \"buffer\" or type(%s) ~= \"number\" or %s < 0 or %s > %d then return false end\n",
			indent, val, length, length, length, size)
		emitBitsOrSkip(g, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "the length")
		call(fmt.Sprintf("stream:serializeBytes(buffer.fromstring(buffer.readstring(%s, 0, %s)))", val, length),
			" -- align, then the used bytes (SPEC §4.7)")
	case ir.TWString:
		size := t.Size
		if length == "" {
			length = val + "Length"
		}
		g.Pf("%sif type(%s) ~= \"buffer\" or type(%s) ~= \"number\" or %s < 0 or %s > %d then return false end\n",
			indent, val, length, length, length, size)
		emitBitsOrSkip(g, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size-1)), "the code unit count")
		g.Pf("%sfor _u = 1, %s do\n", indent, length)
		g.Pf("%s  if not stream:serializeBits({ value = buffer.readu16(%s, (_u - 1) * 2) }, 32) then return false end -- UTF-16 code unit (SPEC §4.12)\n",
			indent, val)
		g.Pf("%send\n", indent)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			g.Pf("%sif type(%s) ~= \"number\" or %s %% 1 ~= 0 or %s < 0 or %s > %d then return false end\n",
				indent, val, val, val, val, ref.Max)
			emitBitsOrSkip(g, indent, val, ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), "the enum value")
		case *ir.Flags:
			g.Pf("%sif typeof(%s) ~= \"integer\" then return false end\n", indent, val)
			if ref.WireBits < 64 {
				g.Pf("%sif integer.ult(%s, 0i) or integer.uge(%s, %s) then return false end\n",
					indent, val, val, IntegerLit(new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits))))
			}
			emitBits64OrSkip(g, indent, val, int64(ref.WireBits), "the flags wire width")
		case *ir.Struct, *ir.Union:
			call(fmt.Sprintf("%s(stream, %s)", g.writeRef(t.Name), val), "")
		}
	}
}

// intStorageBounds is a bare integer storage's domain, [-2^(w-1), 2^(w-1))
// or [0, 2^w), as Luau literals.
func intStorageBounds(signed bool, width int) (string, string) {
	one := big.NewInt(1)
	if signed {
		half := new(big.Int).Lsh(one, uint(width-1))
		return IntLiteral(new(big.Int).Neg(half)), IntLiteral(new(big.Int).Sub(half, one))
	}
	return "0", IntLiteral(new(big.Int).Sub(new(big.Int).Lsh(one, uint(width)), one))
}

// emitBitsOrSkip writes expr as bits raw bits, or nothing when the range is
// degenerate (zero bits, SPEC §4.6); up to 32 bits rides serializeBits (SPEC
// §4.3).
func emitBitsOrSkip(g *Gen, indent, expr string, bits int64, what string) {
	if bits == 0 {
		return
	}
	g.Pf("%sif not stream:serializeBits({ value = %s }, %d) then return false end -- %s\n",
		indent, expr, bits, what)
}

// emitBits64OrSkip is the integer-domain twin of emitBitsOrSkip (SPEC §4.3,
// the 64-bit scratch).
func emitBits64OrSkip(g *Gen, indent, expr string, bits int64, what string) {
	if bits == 0 {
		return
	}
	g.Pf("%sif not stream:serializeBits64({ value = %s }, %d) then return false end -- %s\n",
		indent, expr, bits, what)
}

// callBits is the const/reserved primitive at its declared width.
func callBits(v *big.Int, bits int64) string {
	if bits > 32 {
		return fmt.Sprintf("stream:serializeBits64({ value = %s }, %d)", IntegerLit(v), bits)
	}
	return fmt.Sprintf("stream:serializeBits({ value = %s }, %d)", IntLiteral(v), bits)
}

// emitWriteUnion emits Write<Name>(stream, v): the tag in minimal bits for
// [0, Max], validated BEFORE it rides, then the selected arm only (SPEC §4.8).
func emitWriteUnion(g *Gen, un *ir.Union) {
	g.Pf("-- union %s (SPEC §4.8: None = 0, then each declared arm)\n", un.Name)
	g.Pf("function %s.Write%s(stream: any, v: any)\n", g.Self, un.Name)
	g.Pf("  if type(v.Type) ~= \"number\" or v.Type %% 1 ~= 0 or v.Type < 0 or v.Type > %d then return false end\n", un.Max)
	if un.Max > 0 {
		bits := ir.BitsRequired(big.NewInt(0), big.NewInt(un.Max))
		emitBitsOrSkip(g, "  ", "v.Type", bits, "the union tag")
	}
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		key := ir.GoExportName(arm.Name)
		g.Pf("  if v.Type == %d then\n", i+1)
		emitWriteField(g, "v."+key, arm.F, "    ")
		g.Pf("  end\n")
	}
	g.Pf("  return true\nend\n\n")
}
