// write.go is the type wire's WRITE side (docs/PORT-LUAU.md, the type-wire
// write card): for every packet struct and union it emits
// Write<Name>(stream, v) against the vendored serialize port's WriteStream
// (internal/codegen/luau/runtime/, schema-luau-serialize-vendor). There is no
// private bit writer here; every bit on the wire is the port's. The bytes are
// SPEC.md §4.3 and §6.1, the same ones the C++ target pins in
// testdata/wire/*.bin.
//
// THE WRITER CONTRACT (SPEC §5). Luau has no debug-only assert idiom, so the
// contract is checked in every build, as Go and Elixir check it: a value
// outside its declared range, or of the wrong Luau storage type, makes the
// write return false before any of it rides. The guard runs BEFORE the
// runtime call because serialize.luau 2.x TRAPS on write misuse (it raises,
// IMPLEMENTING.md U9) rather than latching a reason: a generated writer
// never hands the runtime a value it would raise on, so a caller's bad value
// is a false, never an error.
//
// Every value rides through the module's holders (BITS, BIG, BOOL, WIDE,
// declared by read.go): a holder is filled and consumed by the same call, so
// a write allocates no table per field.
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
	maxBits := ir.MaxBitsStruct(st)
	g.Pf("-- %sMaxBits is the longest wire path, align padding at its worst case;\n", st.Name)
	g.Pf("-- %sMaxBytes sizes a write buffer, rounded up to 8 bytes (SPEC §6.1).\n", st.Name)
	g.Pf("%s.%sMaxBits = %d\n", g.Self, st.Name, maxBits)
	g.Pf("%s.%sMaxBytes = %d\n\n", g.Self, st.Name, ir.MaxBytes(maxBits))
	g.BulkBytes = ir.AlignedFixedByteArrays(st)
	defer func() { g.BulkBytes = nil }()
	g.Pf("-- Write%s writes the type wire (SPEC §6.1: the wire order is the declared\n", st.Name)
	g.Pf("-- order); false refuses a value that breaks the writer contract (SPEC §5).\n")
	g.Pf("function %s.Write%s(stream: any, v: any): boolean\n", g.Self, st.Name)
	emitWriteItems(g, st.Items, "  ")
	g.Pf("  return true\nend\n\n")
}

func emitWriteItems(g *Gen, items []ir.Item, indent string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitWriteField(g, "v."+ir.GoExportName(item.F.Name), item.F, indent)
		case *ir.ConstItem:
			emitHeld(g, indent, item.Value, item.Bits, fmt.Sprintf("const(%s, %d) — SPEC §4.3", item.Value.String(), item.Bits))
		case *ir.ReservedItem:
			emitHeld(g, indent, big.NewInt(0), item.Bits, fmt.Sprintf("reserved(%d): zeros on the wire (SPEC §4.3)", item.Bits))
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

// emitHeld writes a constant (const or reserved) at its declared width.
func emitHeld(g *Gen, indent string, v *big.Int, bits int64, comment string) {
	if bits > 32 {
		g.Pf("%sBIG.value = %s\n", indent, IntegerLit(v))
		g.Pf("%sif not stream:serializeBits64(BIG, %d) then return false end -- %s\n", indent, bits, comment)
		return
	}
	g.Pf("%sBITS.value = %s\n", indent, IntLiteral(v))
	g.Pf("%sif not stream:serializeBits(BITS, %d) then return false end -- %s\n", indent, bits, comment)
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
		guardNum(g, indent, count, big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound), "the count guards the loop (SPEC §6.3)")
		emitBitsOrSkip(g, indent, offsetExpr(count, big.NewInt(f.ArrayMin)),
			ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)), "the count (SPEC §4.6)")
		g.Pf("%sfor _i = 1, %s do\n", indent, count)
		emitWriteElem(g, val, f, indent+"  ")
		g.Pf("%send\n", indent)
		return
	}
	if isByteElem(f.Type) && !f.HasIntRange && g.BulkBytes[f] {
		// byte aligned by construction (ir.AlignedFixedByteArrays), so the
		// bulk copy's align adds no bits: the wire is the per-byte loop's
		g.Pf("%sif type(%s) ~= \"buffer\" or buffer.len(%s) ~= %s then return false end\n", indent, val, val, bound)
		g.Pf("%sif not stream:serializeBytes(%s) then return false end -- [N]uint8 on a byte boundary: one bulk copy\n",
			indent, val)
		return
	}
	if isByteElem(f.Type) {
		g.Pf("%sif type(%s) ~= \"buffer\" or buffer.len(%s) ~= %s then return false end\n", indent, val, val, bound)
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

// guardNum refuses a number-domain value that is not an integer in [lo, hi]:
// a non-number, a fraction, a NaN or an infinity included (x % 1 is NaN for
// both, never 0).
func guardNum(g *Gen, indent, val string, lo, hi *big.Int, comment string) {
	if comment != "" {
		comment = " -- " + comment
	}
	g.Pf("%sif type(%s) ~= \"number\" or %s %% 1 ~= 0 or %s < %s or %s > %s then return false end%s\n",
		indent, val, val, val, lo.String(), val, hi.String(), comment)
}

// guardInteger refuses a value that is not a Luau integer in [lo, hi]: signed
// compares while hi fits int64, unsigned past it (the integer holds the
// uint64 bit pattern, docs/PORT-LUAU.md 64-BIT AND 128-BIT INTEGERS). A
// bound equal to the storage's own edge is not tested.
func guardInteger(g *Gen, indent, val string, lo, hi *big.Int, signed bool) {
	cond := fmt.Sprintf("typeof(%s) ~= \"integer\"", val)
	unsigned := hi.Cmp(maxInt64) > 0
	switch {
	case unsigned:
		if lo.Sign() != 0 {
			cond += fmt.Sprintf(" or integer.ult(%s, %s)", val, IntegerLit(lo))
		}
		if hi.Cmp(maxUint64) != 0 {
			cond += fmt.Sprintf(" or integer.ugt(%s, %s)", val, IntegerLit(hi))
		}
	default:
		if !signed || lo.Cmp(minInt64) != 0 {
			cond += fmt.Sprintf(" or integer.lt(%s, %s)", val, IntegerLit(lo))
		}
		cond += fmt.Sprintf(" or integer.gt(%s, %s)", val, IntegerLit(hi))
	}
	g.Pf("%sif %s then return false end\n", indent, cond)
}

var minInt64 = big.NewInt(math.MinInt64)

// offsetExpr is val's offset from lo in the number domain.
func offsetExpr(val string, lo *big.Int) string {
	switch lo.Sign() {
	case 0:
		return val
	case -1:
		return val + " + " + new(big.Int).Neg(lo).String()
	}
	return val + " - " + lo.String()
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
		g.Pf("%sif type(%s) ~= \"boolean\" then return false end\n", indent, val)
		g.Pf("%sBOOL.value = %s\n", indent, val)
		call("stream:serializeBool(BOOL)", " -- SPEC §4.3")
	case ir.TInt:
		switch {
		case t.Width == 128 && f.HasIntRange:
			g.Pf("%sif not _Rt.int128Within(%s, %s, %s, %t) then return false end\n",
				indent, val, Int128Lit(f.IntMin), Int128Lit(f.IntMax), !t.Signed)
			if f.IntMin.Cmp(f.IntMax) == 0 {
				return // a degenerate range: zero bits (SPEC §4.6)
			}
			g.Pf("%sWIDE.value = %s\n", indent, val)
			call(fmt.Sprintf("stream:serializeInt128(WIDE, %s, %s)", Int128Lit(f.IntMin), Int128Lit(f.IntMax)),
				" -- int128, offset from min (SPEC §4.3)")
		case t.Width == 128:
			g.Pf("%sif not _Rt.isInt128(%s) then return false end\n", indent, val)
			g.Pf("%sWIDE.value = %s\n", indent, val)
			call("stream:serializeUint128(WIDE)", " -- uint128 raw (SPEC §4.3)")
		case t.Width == 64 && f.HasIntRange:
			guardInteger(g, indent, val, f.IntMin, f.IntMax, t.Signed)
			emitBits64OrSkip(g, indent, offset64(val, f.IntMin), ir.BitsRequired(f.IntMin, f.IntMax), "the offset from min (SPEC §4.3)")
		case t.Width == 64:
			g.Pf("%sif typeof(%s) ~= \"integer\" then return false end\n", indent, val)
			emitBits64OrSkip(g, indent, val, 64, "the storage width (SPEC §4.3)")
		case f.HasIntRange:
			guardNum(g, indent, val, f.IntMin, f.IntMax, "")
			emitBitsOrSkip(g, indent, offsetExpr(val, f.IntMin), ir.BitsRequired(f.IntMin, f.IntMax), "the offset from min (SPEC §4.3)")
		case t.Signed:
			half := new(big.Int).Lsh(big.NewInt(1), uint(t.Width-1))
			guardNum(g, indent, val, new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1)), "")
			// the two's complement at the storage width: Luau's % floors, so
			// a negative value maps to value + 2^width (SPEC §4.3)
			emitBitsOrSkip(g, indent, fmt.Sprintf("%s %% %s", val, new(big.Int).Lsh(half, 1).String()), int64(t.Width), "the storage width, two's complement")
		default:
			guardNum(g, indent, val, big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(t.Width)), big.NewInt(1)), "")
			emitBitsOrSkip(g, indent, val, int64(t.Width), "the storage width")
		}
	case ir.TBits:
		top := new(big.Int).Lsh(big.NewInt(1), uint(t.Width))
		if t.Width > 32 {
			g.Pf("%sif typeof(%s) ~= \"integer\"", indent, val)
			if t.Width < 64 {
				g.Pf(" or integer.uge(%s, %s)", val, IntegerLit(top))
			}
			g.Pf(" then return false end\n")
			emitBits64OrSkip(g, indent, val, int64(t.Width), "the declared bit width")
		} else {
			guardNum(g, indent, val, big.NewInt(0), new(big.Int).Sub(top, big.NewInt(1)), "")
			emitBitsOrSkip(g, indent, val, int64(t.Width), "the declared bit width")
		}
	case ir.TFixed:
		// the RAW scaled value rides as its offset from the raw minimum, in
		// bits_required(rawMin, rawMax) bits: byte identical to
		// serializeFixed64/128 over the whole-unit bounds (SPEC §4.3)
		rawMin := new(big.Int).Lsh(f.IntMin, uint(t.FracBits))
		rawMax := new(big.Int).Lsh(f.IntMax, uint(t.FracBits))
		bits := ir.BitsRequired(rawMin, rawMax)
		switch {
		case t.Width > 64:
			g.Pf("%sif not _Rt.int128Within(%s, %s, %s, %t) then return false end\n",
				indent, val, Int128Lit(rawMin), Int128Lit(rawMax), !t.Signed)
			if bits == 0 {
				return
			}
			g.Pf("%sWIDE.value = %s\n", indent, val)
			call(fmt.Sprintf("stream:serializeFixed128(WIDE, %d, %d, %s, %s)",
				t.IntBits, t.FracBits, IntegerLit(f.IntMin), IntegerLit(f.IntMax)), " -- the Q format's raw offset (SPEC §4.3)")
		case t.Width > 32:
			guardInteger(g, indent, val, rawMin, rawMax, t.Signed)
			emitBits64OrSkip(g, indent, offset64(val, rawMin), bits, "the Q format's raw offset (SPEC §4.3)")
		default:
			guardNum(g, indent, val, rawMin, rawMax, "")
			emitBitsOrSkip(g, indent, offsetExpr(val, rawMin), bits, "the Q format's raw offset (SPEC §4.3)")
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
		guardNum(g, indent, length, big.NewInt(0), big.NewInt(size), "the length guards the copy (SPEC §6.3)")
		g.Pf("%sif type(%s) ~= \"buffer\" or buffer.len(%s) < %s then return false end\n", indent, val, val, length)
		emitBitsOrSkip(g, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "the length")
		call(fmt.Sprintf("stream:serializeBytes(buffer.fromstring(buffer.readstring(%s, 0, %s)))", val, length),
			" -- align, then the used bytes (SPEC §4.7)")
	case ir.TWString:
		emitWriteWString(g, val, length, t.Size, indent)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			guardNum(g, indent, val, big.NewInt(0), big.NewInt(ref.Max), "headroom above the wire range cannot ride")
			emitBitsOrSkip(g, indent, val, ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), "the enum value")
		case *ir.Flags:
			g.Pf("%sif typeof(%s) ~= \"integer\"", indent, val)
			if ref.WireBits < 64 {
				g.Pf(" or integer.uge(%s, %s)", val, IntegerLit(new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits))))
			}
			g.Pf(" then return false end\n")
			emitBits64OrSkip(g, indent, val, int64(ref.WireBits), "the flags wire width")
		case *ir.Struct, *ir.Union:
			call(fmt.Sprintf("%s(stream, %s)", g.writeRef(t.Name), val), "")
		}
	}
}

// emitWriteWString writes a wstring(N) field (SPEC §4.12): the used length in
// [0, N] over bits_required(0, N) bits, then each used code unit as a 32-bit
// group, no alignment anywhere. The write checks the length and refuses a
// zero code unit among the used units; surrogate pairing is the reader's.
func emitWriteWString(g *Gen, val, length string, size int64, indent string) {
	if length == "" {
		length = val + "Length"
	}
	guardNum(g, indent, length, big.NewInt(0), big.NewInt(size), "the length guards the copy (SPEC §6.3)")
	g.Pf("%sif type(%s) ~= \"buffer\" or buffer.len(%s) < %s * 2 then return false end\n", indent, val, val, length)
	emitBitsOrSkip(g, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "the code unit count")
	g.Pf("%sfor _u = 0, %s - 1 do\n", indent, length)
	g.Pf("%s  local unit = buffer.readu16(%s, _u * 2)\n", indent, val)
	g.Pf("%s  if unit == 0 then return false end -- a zero code unit is refused (SPEC §4.12)\n", indent)
	g.Pf("%s  BITS.value = unit\n", indent)
	g.Pf("%s  if not stream:serializeBits(BITS, 32) then return false end -- one UTF-16 code unit as a 32-bit group\n", indent)
	g.Pf("%send\n", indent)
}

// offset64 is val's offset from lo in the integer domain.
func offset64(val string, lo *big.Int) string {
	if lo.Sign() == 0 {
		return val
	}
	return fmt.Sprintf("integer.sub(%s, %s)", val, IntegerLit(lo))
}

// emitBitsOrSkip writes expr as bits raw bits through the BITS holder, or
// nothing when the range is degenerate (zero bits, SPEC §4.6); up to 32 bits
// rides serializeBits (SPEC §4.3).
func emitBitsOrSkip(g *Gen, indent, expr string, bits int64, what string) {
	if bits == 0 {
		return
	}
	g.Pf("%sBITS.value = %s\n", indent, expr)
	g.Pf("%sif not stream:serializeBits(BITS, %d) then return false end -- %s\n", indent, bits, what)
}

// emitBits64OrSkip is the integer-domain twin of emitBitsOrSkip (SPEC §4.3,
// the 64-bit scratch), through the BIG holder.
func emitBits64OrSkip(g *Gen, indent, expr string, bits int64, what string) {
	if bits == 0 {
		return
	}
	g.Pf("%sBIG.value = %s\n", indent, expr)
	g.Pf("%sif not stream:serializeBits64(BIG, %d) then return false end -- %s\n", indent, bits, what)
}

// emitWriteUnion emits Write<Name>(stream, v): the tag in minimal bits for
// [0, Max], validated BEFORE it rides, then the selected arm only (SPEC §4.8).
func emitWriteUnion(g *Gen, un *ir.Union) {
	maxBits := ir.MaxBitsUnion(un)
	g.Pf("-- %sMaxBits is the tag plus the largest arm, None the tag alone (SPEC §4.8);\n", un.Name)
	g.Pf("-- %sMaxBytes sizes a write buffer, rounded up to 8 bytes (SPEC §6.1).\n", un.Name)
	g.Pf("%s.%sMaxBits = %d\n", g.Self, un.Name, maxBits)
	g.Pf("%s.%sMaxBytes = %d\n\n", g.Self, un.Name, ir.MaxBytes(maxBits))
	g.Pf("-- Write%s writes union %s (SPEC §4.8: None = 0, then each declared arm)\n", un.Name, un.Name)
	g.Pf("function %s.Write%s(stream: any, v: any): boolean\n", g.Self, un.Name)
	guardNum(g, "  ", "v.Type", big.NewInt(0), big.NewInt(un.Max), "a tag that names no arm cannot ride")
	emitBitsOrSkip(g, "  ", "v.Type", ir.BitsRequired(big.NewInt(0), big.NewInt(un.Max)), "the union tag")
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
