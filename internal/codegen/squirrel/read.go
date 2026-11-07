// The type wire's READ side (docs/PORT-SQUIRREL.md, THE TYPE MAPPING; SPEC
// §4.3, §5, §6.1). Every type, union and array gets a Read<Name>(stream,
// value) function that decodes the C++-pinned wire through the serialize
// port's ReadStream (internal/codegen/squirrel/serialize/), mirroring the
// JavaScript runtime tier (internal/codegen/js/functions.go) and the C++
// reference. Reads stay on the runtime's ranged calls (the family rule), so a
// latched stream error is the port's, and generated validation refusals (a
// wrong const, nonzero reserved bits or align padding, an out-of-range
// offset, malformed UTF-8, an interior NUL, a tag past its arms) return false
// without latching.
//
// Storage is the §6.1 storage principle: a record is a class instance, and a
// read fills its pre-allocated buffers in place. The untaken side of a wire
// branch reads as the §5 ZERO form, never the specified defaults.
package squirrel

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// readRef is the expression that names the Read<Name> function of declaration
// typ: _M.Read<Name> here, or _<Base>_.Read<Name> through the file that
// declares it.
func readRef(m *Module, typ string) string { return m.ref(typ, "Read"+typ) }

func init() {
	RegisterSurface(Surface{Name: "read", Decl: readDecl})
}

// readDecl emits the read function for a struct or union declaration.
func readDecl(m *Module, d ir.Decl) {
	switch d := d.(type) {
	case *ir.Struct:
		emitReadStruct(m, d)
	case *ir.Union:
		emitReadUnion(m, d)
	}
}

// emitReadStruct is one type's reader: the wire tree of st.Items in order,
// with the untaken side of each branch zeroed (SPEC §5).
func emitReadStruct(m *Module, st *ir.Struct) {
	m.Printf("// Read%s reads into value's storage in place; false on a refused read.\n", st.Name)
	m.Printf("_M.Read%s <- function(stream, value) {\n", st.Name)
	emitReadItems(m, st.Items, "  ")
	m.Printf("  return true;\n};\n\n")
}

// emitReadUnion is a one-of reader (SPEC §4.8): the tag in [0, Max] via the
// runtime ranged call (which rejects a tag past the arms), then the selected
// arm's payload read in place. A payload-free arm and None are the tag alone;
// the other arms' storage is left as it was. The cases are literals, each
// commented with its arm: a table lookup per case would cost a get each.
func emitReadUnion(m *Module, d *ir.Union) {
	m.Printf("// Read%s reads the tag, then the selected arm in place; false on a refused read.\n", d.Name)
	m.Printf("_M.Read%s <- function(stream, value) {\n", d.Name)
	if d.Max == 0 {
		m.Printf("  value.Type = 0; // zero wire bits: only None exists\n")
		m.Printf("  return true;\n};\n\n")
		return
	}
	h := m.Helper(holder)
	m.Printf("  if (!stream.serializeInt(%s, 0, %d)) return false;\n", h, d.Max)
	m.Printf("  value.Type = %s.value;\n", h)
	payload := false
	for _, v := range d.Variants {
		payload = payload || !v.Void()
	}
	if payload {
		m.Printf("  switch (value.Type) {\n")
		for i, v := range d.Variants {
			if v.Void() {
				continue
			}
			armName := ir.GoExportName(v.Name)
			armType := v.Type
			if armType == "" && v.F != nil && v.F.Array == ir.ArrayNone && v.F.Type.Kind == ir.TNamed {
				armType = v.F.Type.Name
			}
			m.Printf("    case %d: // %s\n", i+1, armName)
			if armType != "" {
				m.Printf("      if (!%s(stream, value.%s)) return false;\n", readRef(m, armType), armName)
			} else {
				emitReadField(m, v.F, "value."+armName, "      ")
			}
			m.Printf("      break;\n")
		}
		m.Printf("  }\n")
	}
	m.Printf("  return true;\n};\n\n")
}

// intRangePath picks the runtime call family for a ranged integer — the same
// switch as the other targets, so all six emit identical wire.
func intRangePath(min, max *big.Int) string {
	i32 := big.NewInt(math.MaxInt32)
	i32lo := big.NewInt(math.MinInt32)
	if min.Cmp(i32lo) >= 0 && max.Cmp(i32) <= 0 {
		return "int32"
	}
	i64 := big.NewInt(math.MaxInt64)
	i64lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	if min.Cmp(i64lo) >= 0 && max.Cmp(i64) <= 0 {
		return "int64"
	}
	return "bits64" // full-range unsigned: width-computed raw bits over value - min
}

// emitReadItems is one wire tree in order.
func emitReadItems(m *Module, items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitReadField(m, item.F, "value."+Member(item.F), ind)
		case *ir.ConstItem:
			emitReadConst(m, item, ind)
		case *ir.ReservedItem:
			emitReadReserved(m, item, ind)
		case *ir.AlignItem:
			m.Printf("%sif (!stream.serializeAlign()) return false;\n", ind)
		case *ir.Branch:
			m.Printf("%sif (%svalue.%s) {\n", ind, branchNeg(item.Neg), ir.GoExportName(item.Cond))
			emitReadItems(m, item.Then, ind+"  ")
			if item.Else != nil {
				emitZeroItems(m, item.Else, ind+"  ")
			}
			m.Printf("%s} else {\n", ind)
			if item.Else != nil {
				emitReadItems(m, item.Else, ind+"  ")
			}
			emitZeroItems(m, item.Then, ind+"  ")
			m.Printf("%s}\n", ind)
		}
	}
}

func branchNeg(neg bool) string {
	if neg {
		return "!"
	}
	return ""
}

// emitReadConst reads a const group and refuses any other value (SPEC §4.3).
func emitReadConst(m *Module, item *ir.ConstItem, ind string) {
	emitReadExpect(m, item.Bits, IntLit(item.Value), fmt.Sprintf("const(%s, %d)", item.Value.String(), item.Bits), ind)
}

// emitReadReserved reads a reserved group and refuses a nonzero one (SPEC §4.3).
func emitReadReserved(m *Module, item *ir.ReservedItem, ind string) {
	emitReadExpect(m, item.Bits, "0", fmt.Sprintf("reserved(%d)", item.Bits), ind)
}

func emitReadExpect(m *Module, bits int64, want, what, ind string) {
	h := m.Helper(holder)
	fn := "serializeBits"
	if bits > 32 {
		fn = "serializeBits64"
	}
	m.Printf("%sif (!stream.%s(%s, %d) || %s.value != %s) return false; // %s\n", ind, fn, h, bits, h, want, what)
}

// emitReadField reads one field into target (a storage path expression).
func emitReadField(m *Module, f *ir.Field, target, ind string) {
	if f.Array != ir.ArrayNone {
		emitReadArray(m, f, target, ind)
		return
	}
	emitReadScalar(m, f, target, ind)
}

func emitReadArray(m *Module, f *ir.Field, target, ind string) {
	scratch := m.Helper(holder)
	if f.Array == ir.ArrayCounted {
		m.Printf("%sif (!stream.serializeInt(%s, %d, %d)) return false;\n", ind, scratch, f.ArrayMin, f.ArrayBound)
		m.Printf("%s%sCount = %s.value;\n", ind, target, scratch)
		m.Printf("%sfor (local i = 0; i < %sCount; i++) {\n", ind, target)
	} else {
		m.Printf("%sfor (local i = 0; i < %d; i++) {\n", ind, f.ArrayBound)
	}
	elem := target + "[i]"
	if isByteElem(f.Type) && !f.HasIntRange {
		// a [N]uint8 element is an 8-bit read; the per-element loop is
		// byte-identical to the bulk path at any alignment. A ranged uint8
		// element (Clauses W1's [..20]uint8 in [0, 1]) rides the ranged
		// call instead, in its minimal bits (SPEC §4.3)
		m.Printf("%s  if (!stream.serializeBits(%s, 8)) return false;\n", ind, scratch)
		m.Printf("%s  %s = %s.value;\n", ind, elem, scratch)
	} else {
		emitReadScalar(m, f, elem, ind+"  ")
	}
	m.Printf("%s}\n", ind)
}

func emitReadScalar(m *Module, f *ir.Field, target, ind string) {
	scratch := m.Helper(holder)
	switch f.Type.Kind {
	case ir.TBool:
		m.Printf("%sif (!stream.serializeBool(%s)) return false;\n", ind, scratch)
		m.Printf("%s%s = %s.value;\n", ind, target, scratch)
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					m.Printf("%s%s = %s;\n", ind, target, Int128Lit(f.IntMin))
					return
				}
				m.Printf("%sif (!stream.serializeInt128(%s, %s, %s)) return false;\n",
					ind, scratch, Int128Lit(f.IntMin), Int128Lit(f.IntMax))
			} else {
				m.Printf("%sif (!stream.serializeUint128(%s)) return false;\n", ind, scratch)
			}
			m.Printf("%s%s = %s.value;\n", ind, target, scratch)
			return
		}
		m.emitReadInt(f, target, ind, scratch)
	case ir.TBits:
		fn := "serializeBits"
		if f.Type.Width > 32 {
			fn = "serializeBits64"
		}
		m.Printf("%sif (!stream.%s(%s, %d)) return false;\n", ind, fn, scratch, f.Type.Width)
		m.Printf("%s%s = %s.value;\n", ind, target, scratch)
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			m.Printf("%s%s = %s;\n", ind, target, IntLit(new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))))
			return
		}
		if f.Type.Width > 64 {
			m.Printf("%sif (!stream.serializeFixed128(%s, %d, %d, %s, %s)) return false;\n",
				ind, scratch, f.Type.IntBits, f.Type.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
		} else {
			m.Printf("%sif (!stream.serializeFixed64(%s, %d, %d, %s, %s)) return false;\n",
				ind, scratch, f.Type.IntBits, f.Type.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
		}
		m.Printf("%s%s = %s.value;\n", ind, target, scratch)
	case ir.TFloat32:
		if f.HasFloatRange {
			m.Printf("%sif (!stream.serializeCompressedFloat(%s, %s, %s, %s)) return false;\n",
				ind, scratch, Float64Lit(f.FMin), Float64Lit(f.FMax), Float64Lit(f.Resolution))
		} else {
			m.Printf("%sif (!stream.serializeFloat(%s)) return false;\n", ind, scratch)
		}
		m.Printf("%s%s = %s.value;\n", ind, target, scratch)
	case ir.TFloat64:
		m.Printf("%sif (!stream.serializeDouble(%s)) return false;\n", ind, scratch)
		m.Printf("%s%s = %s.value;\n", ind, target, scratch)
	case ir.TWString:
		emitReadWString(m, f, target, ind)
	case ir.TString, ir.TBytes:
		emitReadString(m, f, target, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			m.Printf("%sif (!stream.serializeInt(%s, 0, %d)) return false;\n", ind, scratch, ref.Max)
			m.Printf("%s%s = %s.value;\n", ind, target, scratch)
		case *ir.Flags:
			fn := "serializeBits"
			if ref.WireBits > 32 {
				fn = "serializeBits64"
			}
			m.Printf("%sif (!stream.%s(%s, %d)) return false;\n", ind, fn, scratch, ref.WireBits)
			m.Printf("%s%s = %s.value;\n", ind, target, scratch)
		case *ir.Struct, *ir.Union:
			m.Printf("%sif (!%s(stream, %s)) return false;\n", ind, readRef(m, f.Type.Name), target)
		}
	}
}

// emitReadInt is the ranged-int read, mirroring the JS intRangePath selection
// of the runtime ranged calls (reads stay on the runtime family).
func (m *Module) emitReadInt(f *ir.Field, target, ind, scratch string) {
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			if f.Type.Width > 32 {
				m.Printf("%s%s = %s;\n", ind, target, IntLit(f.IntMin))
			} else {
				m.Printf("%s%s = %s;\n", ind, target, f.IntMin.String())
			}
			return
		}
		switch intRangePath(f.IntMin, f.IntMax) {
		case "int32":
			m.Printf("%sif (!stream.serializeInt(%s, %s, %s)) return false;\n",
				ind, scratch, f.IntMin.String(), f.IntMax.String())
		case "int64":
			m.Printf("%sif (!stream.serializeInt64(%s, %s, %s)) return false;\n",
				ind, scratch, IntLit(f.IntMin), IntLit(f.IntMax))
		default:
			diff := new(big.Int).Sub(f.IntMax, f.IntMin)
			m.Printf("%sif (!stream.serializeBits64(%s, %d)) return false;\n",
				ind, scratch, ir.BitsRequired(f.IntMin, f.IntMax))
			if diff.Cmp(maxUint64) != 0 {
				m.Printf("%sif ((%s.value ^ %s) > %s) return false; // past the range: refused, not latched\n",
					ind, scratch, min64, IntLit(xorMin64(diff)))
			}
			if f.IntMin.Sign() == 0 {
				m.Printf("%s%s = %s.value;\n", ind, target, scratch)
			} else {
				m.Printf("%s%s = %s.value + %s;\n", ind, target, scratch, IntLit(f.IntMin))
			}
			return
		}
		m.Printf("%s%s = %s.value;\n", ind, target, scratch)
		return
	}
	fn := "serializeBits"
	if f.Type.Width > 32 {
		fn = "serializeBits64"
	}
	m.Printf("%sif (!stream.%s(%s, %d)) return false;\n", ind, fn, scratch, f.Type.Width)
	if f.Type.Signed && f.Type.Width < 64 {
		// sign-extend the same-width unsigned read (SPEC §5: an int8/int16/
		// int32 reads back signed at its own width)
		m.Printf("%s%s = ((%s.value << %d) >> %d);\n", ind, target, scratch, 64-f.Type.Width, 64-f.Type.Width)
		return
	}
	m.Printf("%s%s = %s.value;\n", ind, target, scratch)
}

// emitReadString reads a string/bytes field: the length via the runtime ranged
// call (guarding the copy), then the used bytes into a blob of exactly that
// length (the serialize port fills whole blobs), validated as text for a
// string (SPEC §4.7) and copied natively into the pre-allocated buffer.
func emitReadString(m *Module, f *ir.Field, target, ind string) {
	scratch := m.Helper(holder)
	m.Printf("%sif (!stream.serializeInt(%s, 0, %d)) return false;\n", ind, scratch, f.Type.Size)
	m.Printf("%s%sLength = %s.value;\n", ind, target, scratch)
	m.Printf("%s{\n", ind)
	m.Printf("%s  local _tmp = blob(%s.value);\n", ind, scratch)
	m.Printf("%s  if (!stream.serializeBytes(_tmp)) return false;\n", ind)
	if f.Type.Kind == ir.TString {
		m.Printf("%s  if (!%s(_tmp, _tmp.len())) return false;\n", ind, m.Helper(validText))
	}
	m.Printf("%s  %s.seek(0);\n", ind, target)
	m.Printf("%s  %s.writeblob(_tmp);\n", ind, target)
	m.Printf("%s}\n", ind)
}

// emitReadWString reads a wstring field: the length in UTF-16 code units via
// the runtime ranged call, then each unit as a 32-bit group into the
// pre-allocated integer array (SPEC §4.12), with the read refusals every
// target owes, in the spec's order: the length is bounded by the ranged call
// before any group is read, a group above 0xFFFF is not a code unit, a zero
// group is an interior null, a high surrogate must be followed at once by a
// low one (and is never the final group), and a low surrogate must follow a
// high one. Exhaustion fails the group's own read.
func emitReadWString(m *Module, f *ir.Field, target, ind string) {
	scratch := m.Helper(holder)
	m.Printf("%sif (!stream.serializeInt(%s, 0, %d)) return false;\n", ind, scratch, f.Type.Size)
	m.Printf("%s%sLength = %s.value;\n", ind, target, scratch)
	m.Printf("%s{\n", ind)
	m.Printf("%s  local high = false;\n", ind)
	m.Printf("%s  for (local i = 0; i < %sLength; i++) {\n", ind, target)
	m.Printf("%s    if (!stream.serializeBits(%s, 32)) return false;\n", ind, scratch)
	m.Printf("%s    local u = %s.value;\n", ind, scratch)
	m.Printf("%s    if (u < 0 || u > 0xFFFF || u == 0) return false;\n", ind)
	m.Printf("%s    local lo = u >= 0xDC00 && u <= 0xDFFF;\n", ind)
	m.Printf("%s    if (high != lo) return false;\n", ind)
	m.Printf("%s    high = u >= 0xD800 && u <= 0xDBFF;\n", ind)
	m.Printf("%s    %s[i] = u;\n", ind, target)
	m.Printf("%s  }\n", ind)
	m.Printf("%s  if (high) return false;\n", ind)
	m.Printf("%s}\n", ind)
}

// emitZeroItems zero-initializes every field under an untaken branch side
// (SPEC §5: ZERO values, not specified defaults).
func emitZeroItems(m *Module, items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitZeroField(m, item.F, "value."+Member(item.F), ind)
		case *ir.Branch:
			emitZeroItems(m, item.Then, ind)
			emitZeroItems(m, item.Else, ind)
		}
	}
}

func emitZeroField(m *Module, f *ir.Field, target, ind string) {
	switch {
	case f.Array == ir.ArrayNone && f.Type.Kind == ir.TWString:
		m.Printf("%sfor (local i = 0; i < %d; i++) %s[i] = 0;\n", ind, f.Type.Size, target)
		m.Printf("%s%sLength = 0;\n", ind, target)
	case f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes):
		m.Printf("%sfor (local i = 0; i < %d; i++) %s[i] = 0;\n", ind, f.Type.Size, target)
		m.Printf("%s%sLength = 0;\n", ind, target)
	case f.Array != ir.ArrayNone:
		if f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref) {
			m.Printf("%sfor (local i = 0; i < %d; i++) {\n", ind, f.ArrayBound)
			emitZeroField(m, &ir.Field{Type: f.Type, Name: f.Name}, target+"[i]", ind+"  ")
			m.Printf("%s}\n", ind)
		} else {
			m.Printf("%sfor (local i = 0; i < %d; i++) %s[i] = %s;\n", ind, f.ArrayBound, target, zeroValue(f.Type))
		}
		if f.Array == ir.ArrayCounted {
			m.Printf("%s%sCount = 0;\n", ind, target)
		}
	case f.Type.Kind == ir.TNamed && isClassRef(f.Type.Ref):
		if _, union := f.Type.Ref.(*ir.Union); union {
			m.Printf("%s%s.Type = 0; // the empty union (SPEC §4.8)\n", ind, target)
			return
		}
		st := f.Type.Ref.(*ir.Struct)
		for _, sf := range st.Fields {
			emitZeroField(m, sf, target+"."+Member(sf), ind)
		}
	default:
		m.Printf("%s%s = %s;\n", ind, target, zeroValue(f.Type))
	}
}
