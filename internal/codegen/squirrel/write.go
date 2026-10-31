// write.go is the type wire's WRITE side (docs/PORT-SQUIRREL.md, the
// type-wire write card): for every struct and union it emits
// Write<Name>(stream, v) against the vendored serialize port's WriteStream
// (internal/codegen/squirrel/serialize/, schema-squirrel-serialize-vendor).
// There is no private bit writer here; every bit on the wire is the port's.
// The bytes are SPEC.md §4.3 and §6.1, the same ones the C++ target pins in
// testdata/wire/*.bin. A value the runtime is handed outside its declared
// range, bit width or finite domain is write misuse under serialize.nut
// v2.1.1 (IMPLEMENTING.md U9): the runtime TRAPS, throwing a string that
// starts "serialize: write misuse:" at the call, before any bit moves, and
// latches nothing on the stream. The emitted guards that run before a runtime
// call (a value that is not an integer, a uint64 outside its range, an enum
// or flags value past its wire width) return false instead (SPEC §4.3).
package squirrel

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// min64 and mask64U are the unsigned-compare constants of the number model
// (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS): a uint64 above
// 2^63 - 1 is the same 64 bits read signed, and the unsigned comparison is
// (a ^ MIN64) < (b ^ MIN64).
const min64 = "0x8000000000000000"

func init() {
	RegisterSurface(Surface{
		Name: "write",
		Decl: writeDecl,
	})
}

// writeDecl is the write surface's per-declaration step: each struct and each
// union gets a Write<Name>(stream, v) function. The class and tag table were
// emitted before us (emitClass, emitUnion), so this surface only adds the
// codec.
func writeDecl(m *Module, d ir.Decl) {
	switch d := d.(type) {
	case *ir.Struct:
		emitWriteStruct(m, d)
	case *ir.Union:
		emitWriteUnion(m, d)
	}
}

// writeRef names a declaration's emitted writer: _M.WriteX here, or
// _<Base>_.WriteX through the file that declares it (docs/PORT-SQUIRREL.md,
// THE RUNTIME LAYOUT).
func writeRef(m *Module, name string) string { return m.ref(name, "Write"+name) }

// emitWriteStruct emits Write<Name>(stream, v) for one struct, walking the
// wire tree so branches, const, reserved and align keep their places
// (ir.Struct.Items; SPEC §4.3, §4.5).
func emitWriteStruct(m *Module, st *ir.Struct) {
	m.Printf("// Write%s writes value in declared order; false on a refused value.\n", st.Name)
	m.Printf("_M.Write%s <- function(stream, value) {\n", st.Name)
	emitWriteItems(m, st.Items, "  ")
	m.Printf("  return true;\n};\n\n")
}

func emitWriteItems(m *Module, items []ir.Item, indent string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitWriteField(m, "value."+Member(item.F), item.F, indent)
		case *ir.ConstItem:
			emitBits(m, indent, IntLit(item.Value), item.Bits, fmt.Sprintf("const(%s, %d)", item.Value.String(), item.Bits))
		case *ir.ReservedItem:
			emitBits(m, indent, "0", item.Bits, fmt.Sprintf("reserved(%d)", item.Bits))
		case *ir.AlignItem:
			m.Printf("%sif (!stream.serializeAlign()) return false;\n", indent)
		case *ir.Branch:
			cond := "value." + ir.GoExportName(item.Cond)
			if item.Neg {
				cond = "!" + cond
			}
			m.Printf("%sif (%s) {\n", indent, cond)
			emitWriteItems(m, item.Then, indent+"  ")
			if item.Else != nil {
				m.Printf("%s} else {\n", indent)
				emitWriteItems(m, item.Else, indent+"  ")
			}
			m.Printf("%s}\n", indent)
		}
	}
}

// emitWriteField writes one field, including its array framing (SPEC §4.6,
// §4.7): a counted array's count first, in [min, bound], then the elements.
func emitWriteField(m *Module, val string, f *ir.Field, indent string) {
	if f.Array == ir.ArrayNone {
		emitWriteScalar(m, val, f, indent)
		return
	}
	bound := f.ArrayBound
	if f.Array == ir.ArrayCounted {
		count := val + "Count"
		min := f.ArrayMin
		m.Printf("%sif (typeof %s != \"integer\" || %s < %d || %s > %d) return false;\n",
			indent, count, count, min, count, bound)
		countExpr := count
		if min != 0 {
			countExpr = fmt.Sprintf("%s - %d", count, min)
		}
		emitBits(m, indent, countExpr, ir.BitsRequired(big.NewInt(min), big.NewInt(bound)), "")
		m.Printf("%sfor (local _i = 0; _i < %s; _i++) {\n", indent, count)
		emitArrayElement(m, val, f, indent+"  ")
		m.Printf("%s}\n", indent)
		return
	}
	m.Printf("%sfor (local _i = 0; _i < %d; _i++) {\n", indent, bound)
	emitArrayElement(m, val, f, indent+"  ")
	m.Printf("%s}\n", indent)
}

// emitArrayElement writes one element of a field's array: the field with its
// array framing removed, at index _i.
func emitArrayElement(m *Module, val string, f *ir.Field, indent string) {
	elem := *f
	elem.Array = ir.ArrayNone
	emitWriteScalar(m, fmt.Sprintf("%s[_i]", val), &elem, indent)
}

// emitWriteScalar writes one value of a field's kind (SPEC §4.3's table).
// Every refusal returns false, so a caller's `if (!...) return false` ends the
// write with the stream's latched error (SPEC §4.3).
func emitWriteScalar(m *Module, val string, f *ir.Field, indent string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		emitCall(m, indent, val, "serializeBool")
	case ir.TInt:
		switch {
		case t.Width == 128 && f.HasIntRange:
			emitCall(m, indent, val, "serializeInt128", Int128Lit(f.IntMin), Int128Lit(f.IntMax))
		case t.Width == 128:
			emitCall(m, indent, val, "serializeUint128")
		case f.HasIntRange:
			emitWriteRangedInt(m, val, f, indent)
		default:
			m.Printf("%sif (typeof %s != \"integer\") return false;\n", indent, val)
			expr := val
			if t.Signed && t.Width < 64 {
				expr = fmt.Sprintf("%s & 0x%x", val, uint64(1)<<uint(t.Width)-1)
			}
			emitBits(m, indent, expr, int64(t.Width), "")
		}
	case ir.TBits:
		m.Printf("%sif (typeof %s != \"integer\") return false;\n", indent, val)
		emitBits(m, indent, val, int64(t.Width), "")
	case ir.TFixed:
		fn := "serializeFixed64"
		if t.Width > 64 {
			fn = "serializeFixed128"
		}
		emitCall(m, indent, val, fn, strconv.Itoa(t.IntBits), strconv.Itoa(t.FracBits), IntLit(f.IntMin), IntLit(f.IntMax))
	case ir.TFloat32:
		if f.HasFloatRange {
			emitCall(m, indent, val, "serializeCompressedFloat", Float32Lit(f.FMin), Float32Lit(f.FMax), Float32Lit(f.Resolution))
		} else {
			emitCall(m, indent, val, "serializeFloat")
		}
	case ir.TFloat64:
		emitCall(m, indent, val, "serializeDouble")
	case ir.TString, ir.TBytes:
		// the used bytes ride whole: the buffer itself when the value fills
		// it, else one native copy of the used prefix (SPEC §4.7)
		size := t.Size
		length := val + "Length"
		m.Printf("%sif (typeof %s != \"blob\" || typeof %s != \"integer\" || %s < 0 || %s > %d || %s > %s.len()) return false;\n",
			indent, val, length, length, length, size, length, val)
		emitBits(m, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "")
		m.Printf("%sif (!stream.serializeBytes(%s(%s, %s))) return false;\n", indent, m.Helper(usedBytes), val, length)
	case ir.TWString:
		size := t.Size
		length := val + "Length"
		// the used length is refused outside [0, N] and past the array's own
		// length, exactly as the narrow string's (SPEC §4.12)
		m.Printf("%sif (typeof %s != \"array\" || typeof %s != \"integer\" || %s < 0 || %s > %d || %s > %s.len()) return false;\n",
			indent, val, length, length, length, size, length, val)
		emitBits(m, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "")
		m.Printf("%sfor (local _j = 0; _j < %s; _j++) {\n", indent, length)
		// a zero unit among the used units is refused, as §4.7 refuses the
		// interior null, and a slot that is not a code unit has no group
		// (SPEC §4.12)
		m.Printf("%s  local _u = %s[_j];\n", indent, val)
		m.Printf("%s  if (typeof _u != \"integer\" || _u <= 0 || _u > 0xFFFF) return false;\n", indent)
		emitCall(m, indent+"  ", "_u", "serializeUint32")
		m.Printf("%s}\n", indent)
	case ir.TNamed:
		emitWriteNamed(m, val, f, indent)
	}
}

// emitCall writes expr through one serialize call: the module's shared holder
// takes the value, and the call consumes it (SPEC §6.1, no allocation per
// field).
func emitCall(m *Module, indent, expr, fn string, args ...string) {
	h := m.Helper(holder)
	m.Printf("%s%s.value = %s;\n", indent, h, expr)
	m.Printf("%sif (!stream.%s(%s)) return false;\n", indent, fn, strings.Join(append([]string{h}, args...), ", "))
}

// emitWriteRangedInt writes an integer with a declared range on the same
// runtime call the read takes (intRangePath); a range past the signed ceiling
// is checked in the unsigned domain, where a bound at the domain's edge is
// vacuous and is not emitted (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT
// INTEGERS).
func emitWriteRangedInt(m *Module, val string, f *ir.Field, indent string) {
	switch intRangePath(f.IntMin, f.IntMax) {
	case "int32":
		emitCall(m, indent, val, "serializeInt", IntLit(f.IntMin), IntLit(f.IntMax))
		return
	case "int64":
		emitCall(m, indent, val, "serializeInt64", IntLit(f.IntMin), IntLit(f.IntMax))
		return
	}
	cond := []string{fmt.Sprintf("typeof %s != \"integer\"", val)}
	if f.IntMin.Sign() != 0 {
		cond = append(cond, fmt.Sprintf("(%s ^ %s) < %s", val, min64, IntLit(xorMin64(f.IntMin))))
	}
	if f.IntMax.Cmp(maxUint64) != 0 {
		cond = append(cond, fmt.Sprintf("(%s ^ %s) > %s", val, min64, IntLit(xorMin64(f.IntMax))))
	}
	m.Printf("%sif (%s) return false;\n", indent, strings.Join(cond, " || "))
	expr := val
	if f.IntMin.Sign() != 0 {
		expr = fmt.Sprintf("%s - %s", val, IntLit(f.IntMin))
	}
	emitBits(m, indent, expr, ir.BitsRequired(f.IntMin, f.IntMax), "")
}

// xorMin64 is bound ^ MIN64 folded at generation time: the bound's 64 bits with
// the sign bit flipped, as the signed integer the unsigned compare reads.
func xorMin64(bound *big.Int) *big.Int {
	bits := new(big.Int).And(bound, mask64).Uint64() ^ (1 << 63)
	return big.NewInt(int64(bits))
}

// emitWriteNamed writes a named reference: an enum's or flags' integer bits, or
// a record's write call.
func emitWriteNamed(m *Module, val string, f *ir.Field, indent string) {
	ref := f.Type.Ref
	switch ref := ref.(type) {
	case *ir.Enum:
		m.Printf("%sif (typeof %s != \"integer\" || %s < 0 || %s > %d) return false;\n",
			indent, val, val, val, ref.Max)
		emitBits(m, indent, val, ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), "")
	case *ir.Flags:
		if ref.WireBits < 64 {
			m.Printf("%sif (typeof %s != \"integer\" || %s < 0 || %s >= %s) return false;\n",
				indent, val, val, val, IntLit(new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits))))
		} else {
			m.Printf("%sif (typeof %s != \"integer\") return false;\n", indent, val)
		}
		emitBits(m, indent, val, int64(ref.WireBits), "")
	case *ir.Struct, *ir.Union:
		m.Printf("%sif (!%s(stream, %s)) return false;\n", indent, writeRef(m, f.Type.Name), val)
	}
}

// emitBits writes expr as raw bits, or nothing when the range is degenerate
// (zero bits, SPEC §4.6); up to 32 bits rides serializeBits, above it
// serializeBits64 (SPEC §4.3, the 64-bit scratch). what, when given, names a
// const or reserved group in a trailing comment.
func emitBits(m *Module, indent, expr string, bits int64, what string) {
	if bits == 0 {
		return
	}
	fn := "serializeBits"
	if bits > 32 {
		fn = "serializeBits64"
	}
	h := m.Helper(holder)
	m.Printf("%s%s.value = %s;\n", indent, h, expr)
	comment := ""
	if what != "" {
		comment = " // " + what
	}
	m.Printf("%sif (!stream.%s(%s, %d)) return false;%s\n", indent, fn, h, bits, comment)
}

// emitWriteUnion emits Write<Name>(stream, value): the tag in minimal bits for
// [0, Max], validated BEFORE it rides, then the selected arm only (SPEC §4.8).
func emitWriteUnion(m *Module, un *ir.Union) {
	m.Printf("// Write%s writes the tag, then the selected arm; false on a refused value.\n", un.Name)
	m.Printf("_M.Write%s <- function(stream, value) {\n", un.Name)
	m.Printf("  if (typeof value.Type != \"integer\" || value.Type < 0 || value.Type > %d) return false;\n", un.Max)
	if un.Max > 0 {
		emitBits(m, "  ", "value.Type", ir.BitsRequired(big.NewInt(0), big.NewInt(un.Max)), "")
	}
	payload := false
	for _, arm := range un.Variants {
		payload = payload || !arm.Void()
	}
	if payload {
		m.Printf("  switch (value.Type) {\n")
		for i, arm := range un.Variants {
			if arm.Void() {
				continue
			}
			m.Printf("    case %d: // %s\n", i+1, ir.GoExportName(arm.Name))
			emitWriteField(m, "value."+Member(arm.F), arm.F, "      ")
			m.Printf("      break;\n")
		}
		m.Printf("  }\n")
	}
	m.Printf("  return true;\n};\n\n")
}
