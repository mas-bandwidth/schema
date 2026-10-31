// write.go is the type wire's WRITE side (docs/PORT-LUA.md, the type-wire
// write card): for every struct and union it emits Write<Name>(stream, v)
// against the vendored serialize port's WriteStream
// (internal/codegen/lua/runtime/, schema-lua-serialize-vendor). There is no
// private bit writer here; every bit on the wire is the port's. The bytes are
// SPEC.md §4.3 and §6.1, the same ones the C++ target pins in
// testdata/wire/*.bin. A value outside its declared range, or of the wrong
// Lua type, is refused before it rides.
package lua

import (
	"embed"
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

//go:embed runtime/*.lua
var runtimeFS embed.FS

func init() {
	RegisterRuntime(runtimeFS)
	RegisterPass(writePass)
}

func writePass(m *Module) error {
	var b strings.Builder
	for _, d := range m.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			emitWriteStruct(m, &b, d)
		case *ir.Union:
			emitWriteUnion(m, &b, d)
		}
	}
	if b.Len() > 0 {
		m.Body.WriteString(b.String())
	}
	return nil
}

// writeFn names a declaration's emitted writer, requiring its module when the
// declaration lives in another file (docs/PORT-LUA.md, THE RUNTIME LAYOUT).
func writeFn(m *Module, name string) string {
	ref := m.Require(name)
	return ref[:strings.LastIndexByte(ref, '.')+1] + "Write" + name
}

// emitWriteStruct emits Write<Name>(stream, v) for one struct, walking the
// wire tree so branches, const, reserved and align keep their places
// (ir.Struct.Items; SPEC §4.3, §4.5).
func emitWriteStruct(m *Module, b *strings.Builder, st *ir.Struct) {
	fmt.Fprintf(b, "-- type %s (SPEC §6.1: the wire order is the declared order)\n", st.Name)
	fmt.Fprintf(b, "function _M.Write%s(%s, %s)\n", st.Name, unusedIf(len(st.Items) == 0, "stream"), unusedIf(!itemsTouchValue(st.Items), "v"))
	emitWriteItems(m, b, st.Items, "  ")
	b.WriteString("  return true\nend\n\n")
}

func emitWriteItems(m *Module, b *strings.Builder, items []ir.Item, indent string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitWriteField(m, b, "v."+ir.GoExportName(item.F.Name), item.F, indent)
		case *ir.ConstItem:
			fmt.Fprintf(b, "%sif not %s then return false end -- const(%s, %d) — SPEC §4.3\n",
				indent, callBits(item.Value, item.Bits), item.Value.String(), item.Bits)
		case *ir.ReservedItem:
			fmt.Fprintf(b, "%sif not %s then return false end -- reserved(%d): zeros on the wire (SPEC §4.3)\n",
				indent, callBits(big.NewInt(0), item.Bits), item.Bits)
		case *ir.AlignItem:
			fmt.Fprintf(b, "%sif not stream:serializeAlign() then return false end -- SPEC §4.3\n", indent)
		case *ir.Branch:
			cond := "v." + ir.GoExportName(item.Cond)
			if item.Neg {
				cond = "not " + cond
			}
			fmt.Fprintf(b, "%sif %s then\n", indent, cond)
			emitWriteItems(m, b, item.Then, indent+"  ")
			if item.Else != nil {
				fmt.Fprintf(b, "%selse\n", indent)
				emitWriteItems(m, b, item.Else, indent+"  ")
			}
			fmt.Fprintf(b, "%send\n", indent)
		}
	}
}

// emitWriteField writes one field, including its array framing (SPEC §4.6,
// §4.7): a counted array's count first, in [min, bound], then the elements.
func emitWriteField(m *Module, b *strings.Builder, val string, f *ir.Field, indent string) {
	if f.Array == ir.ArrayNone {
		emitWriteScalar(m, b, val, f, indent)
		return
	}
	bound := IntLiteral(big.NewInt(f.ArrayBound))
	if f.Array == ir.ArrayCounted {
		count := val + "Count"
		min := IntLiteral(big.NewInt(f.ArrayMin))
		fmt.Fprintf(b, "%sif type(%s) ~= \"number\" or %s < %s or %s > %s then return false end -- the count guards the loop (SPEC §6.3)\n",
			indent, count, count, min, count, bound)
		countExpr := count
		if f.ArrayMin != 0 {
			// a range form's count rides offset from its declared minimum
			// (SPEC §4.6), exactly as the ranged integer it is
			countExpr = count + " - " + subtrahend(min)
		}
		emitBitsOrSkip(b, indent, countExpr, ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)), "the count")
		fmt.Fprintf(b, "%sfor _i = 1, %s do\n", indent, count)
		elem := *f
		elem.Array = ir.ArrayNone
		emitWriteScalar(m, b, val+"[_i]", &elem, indent+"  ")
		fmt.Fprintf(b, "%send\n", indent)
		return
	}
	fmt.Fprintf(b, "%sfor _i = 1, %s do\n", indent, bound)
	elem := *f
	elem.Array = ir.ArrayNone
	emitWriteScalar(m, b, val+"[_i]", &elem, indent+"  ")
	fmt.Fprintf(b, "%send\n", indent)
}

// emitWriteScalar writes one value of a field's kind (SPEC §4.3's table).
func emitWriteScalar(m *Module, b *strings.Builder, val string, f *ir.Field, indent string) {
	t := f.Type
	call := func(expr, comment string) {
		fmt.Fprintf(b, "%sif not %s then return false end%s\n", indent, expr, comment)
	}
	switch t.Kind {
	case ir.TBool:
		call(fmt.Sprintf("stream:serializeBool({ value = %s })", val), " -- SPEC §4.3")
	case ir.TInt:
		switch {
		case t.Width == 128 && f.HasIntRange:
			call(fmt.Sprintf("stream:serializeInt128({ value = %s }, %s, %s)", val,
				Int128Literal(f.IntMin), Int128Literal(f.IntMax)), " -- int128, offset from min (SPEC §4.3)")
		case t.Width == 128:
			call(fmt.Sprintf("stream:serializeUint128({ value = %s })", val), " -- uint128 raw (SPEC §4.3)")
		case f.HasIntRange:
			lo, hi := IntLiteral(f.IntMin), IntLiteral(f.IntMax)
			if f.IntMax.Cmp(maxInt64) > 0 {
				// a uint64 range past the signed ceiling: compare in the
				// unsigned 64-bit domain (docs/PORT-LUA.md, 64-BIT AND
				// 128-BIT INTEGERS: a uint64 is the same bits read unsigned)
				fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" or math.ult(%s, %s) or math.ult(%s, %s) then return false end\n",
					indent, val, val, lo, hi, val)
			} else {
				fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" or %s < %s or %s > %s then return false end\n",
					indent, val, val, lo, val, hi)
			}
			emitBitsOrSkip(b, indent, val+" - "+subtrahend(lo), ir.BitsRequired(f.IntMin, f.IntMax), "the offset from min")
		default:
			fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" then return false end\n", indent, val)
			expr := val
			if t.Signed && t.Width < 64 {
				// a signed storage's same-width unsigned pattern (the C++
				// target's cast; SPEC §4.3)
				expr = fmt.Sprintf("%s & 0x%x", val, uint64(1)<<uint(t.Width)-1)
			}
			emitBitsOrSkip(b, indent, expr, int64(t.Width), "the storage width")
		}
	case ir.TBits:
		fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" then return false end\n", indent, val)
		emitBitsOrSkip(b, indent, val, int64(t.Width), "the declared bit width")
	case ir.TFixed:
		call(fmt.Sprintf("stream:serializeFixed({ value = %s }, %d, %d, %s, %s)", val,
			t.IntBits, t.FracBits, IntLiteral(f.IntMin), IntLiteral(f.IntMax)), " -- the Q format's raw offset (SPEC §4.3)")
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
		fmt.Fprintf(b, "%sif type(%s) ~= \"string\" or #%s > %d then return false end\n", indent, val, val, size)
		length := "#" + val
		emitBitsOrSkip(b, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "the length")
		call(fmt.Sprintf("stream:serializeBytes({ value = string.sub(%s, 1, %s) }, %s)", val, length, length),
			" -- align, then the used bytes (SPEC §4.7)")
	case ir.TWString:
		emitWriteWString(b, val, t.Size, indent)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" or %s < 0 or %s > %d then return false end\n",
				indent, val, val, val, ref.Max)
			emitBitsOrSkip(b, indent, val, ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), "the enum value")
		case *ir.Flags:
			if ref.WireBits < 64 {
				fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" or %s < 0 or %s >= %s then return false end\n",
					indent, val, val, val, IntLiteral(new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits))))
			} else {
				fmt.Fprintf(b, "%sif math.type(%s) ~= \"integer\" then return false end\n", indent, val)
			}
			emitBitsOrSkip(b, indent, val, int64(ref.WireBits), "the flags wire width")
		case *ir.Struct:
			call(fmt.Sprintf("%s(stream, %s)", writeFn(m, t.Name), val), "")
		case *ir.Union:
			call(fmt.Sprintf("%s(stream, %s)", writeFn(m, t.Name), val), "")
		}
	}
}

// emitWriteWString composes wstring(N) itself (SPEC §4.12: serialize format
// 2.0 removed wstring from every runtime, and no runtime call carries it). The
// storage is the reader's: a sequence of UTF-16 code units, each an integer.
// The bytes are classic serialize_wstring's over a buffer of N + 1: the length
// as a ranged integer over [0, N] with no alignment, then each code unit as one
// 32-bit group. A unit outside [1, 0xFFFF] is not a code unit the wire can
// carry (zero is §4.7's interior null) and refuses, as the Go target does.
func emitWriteWString(b *strings.Builder, val string, size int64, indent string) {
	fmt.Fprintf(b, "%sif type(%s) ~= \"table\" or #%s > %d then return false end -- the length in [0, %d] (SPEC §4.12)\n",
		indent, val, val, size, size)
	emitBitsOrSkip(b, indent, "#"+val, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "the length, no alignment (SPEC §4.12)")
	fmt.Fprintf(b, "%sfor _u = 1, #%s do\n", indent, val)
	fmt.Fprintf(b, "%s  local unit = %s[_u]\n", indent, val)
	fmt.Fprintf(b, "%s  if math.type(unit) ~= \"integer\" or unit < 1 or unit > 0xFFFF then return false end\n", indent)
	fmt.Fprintf(b, "%s  if not stream:serializeBits({ value = unit }, 32) then return false end -- one 32-bit group per code unit\n", indent)
	fmt.Fprintf(b, "%send\n", indent)
}

// emitBitsOrSkip writes expr as bits raw bits, or nothing when the range is
// degenerate (zero bits, SPEC §4.6); up to 32 bits rides serializeBits, above
// it serializeBits64 (SPEC §4.3, the 64-bit scratch).
func emitBitsOrSkip(b *strings.Builder, indent, expr string, bits int64, what string) {
	if bits == 0 {
		return
	}
	fn := "serializeBits"
	if bits > 32 {
		fn = "serializeBits64"
	}
	fmt.Fprintf(b, "%sif not stream:%s({ value = %s }, %d) then return false end -- %s\n",
		indent, fn, expr, bits, what)
}

// callBits is the const/reserved primitive at its declared width.
func callBits(v *big.Int, bits int64) string {
	fn := "serializeBits"
	if bits > 32 {
		fn = "serializeBits64"
	}
	return fmt.Sprintf("stream:%s({ value = %s }, %d)", fn, IntLiteral(v), bits)
}

// emitWriteUnion emits Write<Name>(stream, v): the tag in minimal bits for
// [0, Max], validated BEFORE it rides, then the selected arm only (SPEC §4.8).
func emitWriteUnion(m *Module, b *strings.Builder, un *ir.Union) {
	fmt.Fprintf(b, "-- union %s (SPEC §4.8: None = 0, then each declared arm)\n", un.Name)
	fmt.Fprintf(b, "function _M.Write%s(%s, v)\n", un.Name, unusedIf(un.Max == 0, "stream"))
	fmt.Fprintf(b, "  if math.type(v.Type) ~= \"integer\" or v.Type < 0 or v.Type > %d then return false end\n", un.Max)
	if un.Max > 0 {
		bits := ir.BitsRequired(big.NewInt(0), big.NewInt(un.Max))
		emitBitsOrSkip(b, "  ", "v.Type", bits, "the union tag")
	}
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		key := ir.GoExportName(arm.Name)
		fmt.Fprintf(b, "  if v.Type == %d then\n", i+1)
		emitWriteField(m, b, "v."+key, arm.F, "    ")
		b.WriteString("  end\n")
	}
	b.WriteString("  return true\nend\n\n")
}

// subtrahend spells the right operand of a subtraction as StyLua does: a
// single token (a literal, a negated literal, a name) bare, and anything with
// an operator of its own in parentheses.
func subtrahend(x string) string {
	body := strings.TrimPrefix(x, "-")
	for _, c := range body {
		if c != '.' && c != '_' && (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return "(" + x + ")"
		}
	}
	return x
}
