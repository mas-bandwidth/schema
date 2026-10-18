// write.go is the type wire's WRITE side (docs/PORT-SQUIRREL.md, the
// type-wire write card): for every struct and union it emits
// Write<Name>(stream, v) against the vendored serialize port's WriteStream
// (internal/codegen/squirrel/serialize/, schema-squirrel-serialize-vendor).
// There is no private bit writer here; every bit on the wire is the port's.
// The bytes are SPEC.md §4.3 and §6.1, the same ones the C++ target pins in
// testdata/wire/*.bin. A value past its declared bound is refused on write
// (docs/PORT-SQUIRREL.md, THE NAMED REFUSALS): the write functions return
// false, leaving the stream's latched error (SPEC §4.3).
package squirrel

import (
	"fmt"
	"math/big"

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
// _<Base>.WriteX through the file that declares it (docs/PORT-SQUIRREL.md,
// THE RUNTIME LAYOUT).
func writeRef(m *Module, name string) string {
	base, ok := m.Unit.DeclFile[name]
	if !ok || base == m.File.Base {
		return "_M.Write" + name
	}
	m.deps[base] = true
	return "_" + base + ".Write" + name
}

// emitWriteStruct emits Write<Name>(stream, v) for one struct, walking the
// wire tree so branches, const, reserved and align keep their places
// (ir.Struct.Items; SPEC §4.3, §4.5).
func emitWriteStruct(m *Module, st *ir.Struct) {
	m.Printf("// type %s (SPEC §6.1: the wire order is the declared order)\n", st.Name)
	m.Printf("_M.Write%s <- function(stream, v) {\n", st.Name)
	emitWriteItems(m, st.Items, "  ")
	m.Printf("  return true;\n};\n\n")
}

func emitWriteItems(m *Module, items []ir.Item, indent string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitWriteField(m, "v."+Member(item.F), item.F, indent)
		case *ir.ConstItem:
			m.Printf("%sif (!%s) return false; // const(%s, %d) — SPEC §4.3\n",
				indent, bitsCall(item.Value, item.Bits), item.Value.String(), item.Bits)
		case *ir.ReservedItem:
			m.Printf("%sif (!%s) return false; // reserved(%d): zeros on the wire (SPEC §4.3)\n",
				indent, bitsCall(big.NewInt(0), item.Bits), item.Bits)
		case *ir.AlignItem:
			m.Printf("%sif (!stream.serializeAlign()) return false; // SPEC §4.3\n", indent)
		case *ir.Branch:
			cond := "v." + ir.GoExportName(item.Cond)
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
		m.Printf("%sif (typeof %s != \"integer\" || %s < %d || %s > %d) return false; // the count guards the loop (SPEC §6.3)\n",
			indent, count, count, min, count, bound)
		countExpr := count
		if min != 0 {
			countExpr = fmt.Sprintf("(%s - %d)", count, min)
		}
		emitBits(m, indent, countExpr, ir.BitsRequired(big.NewInt(min), big.NewInt(bound)), "the count")
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
		m.Printf("%sif (!stream.serializeBool({ value = %s })) return false; // SPEC §4.3\n", indent, val)
	case ir.TInt:
		switch {
		case t.Width == 128 && f.HasIntRange:
			m.Printf("%sif (!stream.serializeInt128({ value = %s }, %s, %s)) return false; // int128, offset from min (SPEC §4.3)\n",
				indent, val, Int128Lit(f.IntMin), Int128Lit(f.IntMax))
		case t.Width == 128:
			m.Printf("%sif (!stream.serializeUint128({ value = %s })) return false; // uint128 raw (SPEC §4.3)\n",
				indent, val)
		case f.HasIntRange:
			emitWriteRangedInt(m, val, f, indent)
		default:
			m.Printf("%sif (typeof %s != \"integer\") return false;\n", indent, val)
			expr := val
			if t.Signed && t.Width < 64 {
				expr = fmt.Sprintf("(%s & 0x%x)", val, uint64(1)<<uint(t.Width)-1)
			}
			emitBits(m, indent, expr, int64(t.Width), "the storage width")
		}
	case ir.TBits:
		m.Printf("%sif (typeof %s != \"integer\") return false;\n", indent, val)
		emitBits(m, indent, val, int64(t.Width), "the declared bit width")
	case ir.TFixed:
		if t.Width > 64 {
			m.Printf("%sif (!stream.serializeFixed128({ value = %s }, %d, %d, %s, %s)) return false; // the Q format's raw offset (SPEC §4.3)\n",
				indent, val, t.IntBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
		} else {
			m.Printf("%sif (!stream.serializeFixed64({ value = %s }, %d, %d, %s, %s)) return false; // the Q format's raw offset (SPEC §4.3)\n",
				indent, val, t.IntBits, t.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
		}
	case ir.TFloat32:
		if f.HasFloatRange {
			m.Printf("%sif (!stream.serializeCompressedFloat({ value = %s }, %s, %s, %s)) return false; // the step index (SPEC §4.3)\n",
				indent, val, Float32Lit(f.FMin), Float32Lit(f.FMax), Float32Lit(f.Resolution))
		} else {
			m.Printf("%sif (!stream.serializeFloat({ value = %s })) return false;\n", indent, val)
		}
	case ir.TFloat64:
		m.Printf("%sif (!stream.serializeDouble({ value = %s })) return false;\n", indent, val)
	case ir.TString, ir.TBytes:
		size := t.Size
		length := val + "Length"
		m.Printf("%sif (typeof %s != \"blob\" || %s > %d) return false;\n", indent, val, length, size)
		emitBits(m, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "the length")
		m.Printf("%s{\n", indent)
		m.Printf("%s  local _b = blob(%s);\n", indent, length)
		m.Printf("%s  for (local _j = 0; _j < %s; _j++) _b[_j] = %s[_j];\n", indent, length, val)
		m.Printf("%s  if (!stream.serializeBytes(_b)) return false; // align, then the used bytes (SPEC §4.7)\n", indent)
		m.Printf("%s}\n", indent)
	case ir.TWString:
		size := t.Size
		length := val + "Length"
		m.Printf("%sif (typeof %s != \"array\" || %s > %d) return false;\n", indent, val, length, size)
		emitBits(m, indent, length, ir.BitsRequired(big.NewInt(0), big.NewInt(size)), "the UTF-16 length")
		m.Printf("%sfor (local _j = 0; _j < %s; _j++) {\n", indent, length)
		m.Printf("%s  if (!stream.serializeUint32({ value = %s[_j] })) return false;\n", indent, val)
		m.Printf("%s}\n", indent)
	case ir.TNamed:
		emitWriteNamed(m, val, f, indent)
	}
}

// emitWriteRangedInt writes a 64-bit integer with a declared range, the range
// outside the signed ceiling in the unsigned domain (docs/PORT-SQUIRREL.md,
// 64-BIT AND 128-BIT INTEGERS).
func emitWriteRangedInt(m *Module, val string, f *ir.Field, indent string) {
	bits := ir.BitsRequired(f.IntMin, f.IntMax)
	if f.IntMax.Cmp(maxInt64) <= 0 && f.IntMin.Cmp(minInt64) >= 0 {
		fn := "serializeInt"
		if bits > 32 {
			fn = "serializeInt64"
		}
		m.Printf("%sif (!stream.%s({ value = %s }, %s, %s)) return false; // the offset from min (SPEC §4.3)\n",
			indent, fn, val, IntLit(f.IntMin), IntLit(f.IntMax))
		return
	}
	m.Printf("%sif (typeof %s != \"integer\" || (%s ^ %s) < (%s ^ %s) || (%s ^ %s) < (%s ^ %s)) return false;\n",
		indent, val, val, min64, IntLit(f.IntMin), min64, IntLit(f.IntMax), min64, val, min64)
	emitBits(m, indent, fmt.Sprintf("(%s - %s)", val, IntLit(f.IntMin)), bits, "the offset from min")
}

// emitWriteNamed writes a named reference: an enum's or flags' integer bits, or
// a record's write call.
func emitWriteNamed(m *Module, val string, f *ir.Field, indent string) {
	ref := f.Type.Ref
	switch ref := ref.(type) {
	case *ir.Enum:
		m.Printf("%sif (typeof %s != \"integer\" || %s < 0 || %s > %d) return false;\n",
			indent, val, val, val, ref.Max)
		emitBits(m, indent, val, ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), "the enum value")
	case *ir.Flags:
		if ref.WireBits < 64 {
			m.Printf("%sif (typeof %s != \"integer\" || %s < 0 || %s >= %s) return false;\n",
				indent, val, val, val, IntLit(new(big.Int).Lsh(big.NewInt(1), uint(ref.WireBits))))
		} else {
			m.Printf("%sif (typeof %s != \"integer\") return false;\n", indent, val)
		}
		emitBits(m, indent, val, int64(ref.WireBits), "the flags wire width")
	case *ir.Struct:
		m.Printf("%sif (!%s(stream, %s)) return false;\n", indent, writeRef(m, ref.Name), val)
	case *ir.Union:
		m.Printf("%sif (!%s(stream, %s)) return false;\n", indent, writeRef(m, ref.Name), val)
	}
}

// emitBits writes expr as raw bits, or nothing when the range is degenerate
// (zero bits, SPEC §4.6); up to 32 bits rides serializeBits, above it
// serializeBits64 (SPEC §4.3, the 64-bit scratch).
func emitBits(m *Module, indent, expr string, bits int64, what string) {
	if bits == 0 {
		return
	}
	m.Printf("%sif (!%s) return false; // %s\n", indent, bitsCallExpr(expr, bits), what)
}

// bitsCall is the const/reserved primitive at its declared width.
func bitsCall(v *big.Int, bits int64) string {
	return bitsCallExpr(IntLit(v), bits)
}

func bitsCallExpr(expr string, bits int64) string {
	fn := "serializeBits"
	if bits > 32 {
		fn = "serializeBits64"
	}
	return fmt.Sprintf("stream.%s({ value = %s }, %d)", fn, expr, bits)
}

// emitWriteUnion emits Write<Name>(stream, v): the tag in minimal bits for
// [0, Max], validated BEFORE it rides, then the selected arm only (SPEC §4.8).
func emitWriteUnion(m *Module, un *ir.Union) {
	m.Printf("// union %s (SPEC §4.8: None = 0, then each declared arm)\n", un.Name)
	m.Printf("_M.Write%s <- function(stream, v) {\n", un.Name)
	m.Printf("  if (typeof v.Type != \"integer\" || v.Type < 0 || v.Type > %d) return false;\n", un.Max)
	if un.Max > 0 {
		bits := ir.BitsRequired(big.NewInt(0), big.NewInt(un.Max))
		emitBits(m, "  ", "v.Type", bits, "the union tag")
	}
	for i, arm := range un.Variants {
		if arm.Void() {
			continue
		}
		key := Member(arm.F)
		m.Printf("  if (v.Type == %d) {\n", i+1)
		emitWriteField(m, "v."+key, arm.F, "    ")
		m.Printf("  }\n")
	}
	m.Printf("  return true;\n};\n\n")
}
