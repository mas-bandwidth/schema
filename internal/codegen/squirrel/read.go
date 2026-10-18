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
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// readRef is the expression that names the Read<Name> function of declaration
// typ: _M.Read<Name> here, or _<Base>.Read<Name> through the file that
// declares it.
func readRef(m *Module, typ string) string {
	base, ok := m.Unit.DeclFile[typ]
	if !ok || base == m.File.Base {
		return "_M.Read" + typ
	}
	m.deps[base] = true
	return "_" + base + ".Read" + typ
}

func init() {
	RegisterSurface(Surface{Name: "read", Decl: readDecl, File: readFile})
}

// readFile runs once per module after its declarations: nothing is needed
// here today beyond the module-scoped scratch holder, which the read functions
// declare lazily themselves.
func readFile(m *Module) {}

// readDecl emits the read function for a struct or union declaration.
func readDecl(m *Module, d ir.Decl) {
	switch d := d.(type) {
	case *ir.Struct:
		emitReadStruct(m, d)
	case *ir.Union:
		emitReadUnion(m, d)
	}
}

// readScratch is the { value = ... } holder every read call in one function
// shares, declared once at the top of the function body (never lazily inside
// a branch or loop, whose scope would end before a later use). A holder is
// always consumed in the same call that fills it — the design's twin of the
// runtime's own shared FLOAT_SCRATCH.
type readScratch struct {
	used bool
}

// declare emits the holder at the top of a read function's body.
func (r *readScratch) declare(m *Module) {
	m.Printf("  local _rs = { value = 0 };\n")
	r.used = true
}

func (r *readScratch) decl(m *Module) string {
	if !r.used {
		r.declare(m)
	}
	return "_rs"
}

// emitReadStruct is one type's reader: the wire tree of st.Items in order,
// with the untaken side of each branch zeroed (SPEC §5).
func emitReadStruct(m *Module, st *ir.Struct) {
	m.Printf("// Read%s decodes the wire into value, filling its pre-allocated storage\n", st.Name)
	m.Printf("// (SPEC §6.1); the untaken side of a branch reads as zero (SPEC §5).\n")
	m.Printf("_M.Read%s <- function(stream, value) {\n", st.Name)
	if len(st.Items) == 0 {
		m.Printf("  // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		rs := &readScratch{}
		rs.declare(m)
		emitReadItems(m, st.Items, "  ", rs)
	}
	m.Printf("  return true;\n};\n\n")
}

// emitReadUnion is a one-of reader (SPEC §4.8): the tag in [0, Max] via the
// runtime ranged call (which rejects a tag past the arms), then the selected
// arm's payload read. A payload-free arm is the tag alone; None is the tag
// alone. The selected arm is re-initialized as a fresh value before decoding.
func emitReadUnion(m *Module, d *ir.Union) {
	m.Printf("_M.Read%s <- function(stream, value) {\n", d.Name)
	rs := &readScratch{}
	scratch := rs.decl(m)
	if d.Max == 0 {
		m.Printf("  value.Type = 0; // zero wire bits — only None exists (SPEC §4.8)\n")
		m.Printf("  return true;\n};\n\n")
		return
	}
	m.Printf("  if (!stream.serializeInt(%s, 0, %d)) return false; // rejects a tag above the arms (SPEC §4.8)\n", scratch, d.Max)
	m.Printf("  value.Type = %s.value;\n", scratch)
	m.Printf("  switch (value.Type) {\n")
	for i, v := range d.Variants {
		tag := i + 1
		armName := ir.GoExportName(v.Name)
		if v.Void() {
			m.Printf("    case %d: return true; // a payload-free arm: the tag is the whole wire (SPEC §4.8)\n", tag)
			continue
		}
		armType := v.Type
		if armType == "" && v.F != nil && v.F.Array == ir.ArrayNone && v.F.Type.Kind == ir.TNamed {
			armType = v.F.Type.Name
		}
		m.Printf("    case %d: {\n", tag)
		if armType != "" {
			m.Printf("      if (!%s(stream, value.%s)) return false;\n", readRef(m, armType), armName)
		} else {
			// a scalar arm field: read it in place
			emitReadField(m, v.F, "value."+armName, "      ", rs)
		}
		m.Printf("      return true;\n    }\n")
	}
	m.Printf("    default: return true; // None\n  }\n};\n\n")
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
func emitReadItems(m *Module, items []ir.Item, ind string, rs *readScratch) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitReadField(m, item.F, "value."+Member(item.F), ind, rs)
		case *ir.ConstItem:
			emitReadConst(m, item, ind, rs)
		case *ir.ReservedItem:
			emitReadReserved(m, item, ind, rs)
		case *ir.AlignItem:
			m.Printf("%sif (!stream.serializeAlign()) return false; // rejects nonzero padding (SPEC §4.3)\n", ind)
		case *ir.Branch:
			m.Printf("%sif (%svalue.%s) {\n", ind, branchNeg(item.Neg), ir.GoExportName(item.Cond))
			emitReadItems(m, item.Then, ind+"  ", rs)
			if item.Else != nil {
				emitZeroItems(m, item.Else, ind+"  ")
			}
			m.Printf("%s} else {\n", ind)
			if item.Else != nil {
				emitReadItems(m, item.Else, ind+"  ", rs)
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

func emitReadConst(m *Module, item *ir.ConstItem, ind string, rs *readScratch) {
	scratch := rs.decl(m)
	fn, lit := "serializeBits", IntLit(item.Value)
	if item.Bits > 32 {
		fn = "serializeBits64"
	}
	m.Printf("%sif (!stream.%s(%s, %d)) return false;\n", ind, fn, scratch, item.Bits)
	m.Printf("%sif (%s.value != %s) { // const(%s, %d): a read rejects any other value (SPEC §4.3)\n", ind, scratch, lit, item.Value.String(), item.Bits)
	m.Printf("%s  return false;\n%s}\n", ind, ind)
}

func emitReadReserved(m *Module, item *ir.ReservedItem, ind string, rs *readScratch) {
	scratch := rs.decl(m)
	fn := "serializeBits"
	if item.Bits > 32 {
		fn = "serializeBits64"
	}
	m.Printf("%sif (!stream.%s(%s, %d)) return false;\n", ind, fn, scratch, item.Bits)
	m.Printf("%sif (%s.value != 0) { // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, scratch, item.Bits)
	m.Printf("%s  return false;\n%s}\n", ind, ind)
}

// emitReadField reads one field into target (a storage path expression).
func emitReadField(m *Module, f *ir.Field, target, ind string, rs *readScratch) {
	if f.Array != ir.ArrayNone {
		emitReadArray(m, f, target, ind, rs)
		return
	}
	emitReadScalar(m, f, target, ind, rs)
}

func emitReadArray(m *Module, f *ir.Field, target, ind string, rs *readScratch) {
	scratch := rs.decl(m)
	if f.Array == ir.ArrayCounted {
		m.Printf("%sif (!stream.serializeInt(%s, %d, %d)) return false; // the count guards the loop (§6.3)\n", ind, scratch, f.ArrayMin, f.ArrayBound)
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
		emitReadScalar(m, f, elem, ind+"  ", rs)
	}
	m.Printf("%s}\n", ind)
}

func emitReadScalar(m *Module, f *ir.Field, target, ind string, rs *readScratch) {
	scratch := rs.decl(m)
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
		emitReadWString(m, f, target, ind, rs)
	case ir.TString, ir.TBytes:
		emitReadString(m, f, target, ind, rs)
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
				m.Printf("%sif (%s.value > %s) { // a read rejects out-of-range (SPEC §5) — not latched\n",
					ind, scratch, IntLit(diff))
				m.Printf("%s  return false;\n%s}\n", ind, ind)
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
// call (guarding the loop), then the used bytes into a temporary blob of
// exactly that length (the serialize port fills whole blobs), copied into the
// pre-allocated field buffer. For a string, UTF-8 and interior-NUL validation
// ride the byte copy (SPEC §4.7).
func emitReadString(m *Module, f *ir.Field, target, ind string, rs *readScratch) {
	scratch := rs.decl(m)
	m.Printf("%sif (!stream.serializeInt(%s, 0, %d)) return false; // the length guards the copy (§6.3)\n", ind, scratch, f.Type.Size)
	m.Printf("%s%sLength = %s.value;\n", ind, target, scratch)
	m.Printf("%slocal _n = %s.value;\n", ind, scratch)
	m.Printf("%slocal _tmp = blob(_n);\n", ind)
	m.Printf("%sif (!stream.serializeBytes(_tmp)) return false;\n", ind)
	m.Printf("%sfor (local i = 0; i < _n; i++) %s[i] = _tmp[i];\n", ind, target)
	if f.Type.Kind == ir.TString {
		m.Printf("%sfor (local i = 0; i < _n; i++) { if (%s[i] == 0) return false; } // interior NUL (SPEC §4.7)\n", ind, target)
		emitReadUTF8Squirrel(m, target, "_n", ind)
	}
}

// emitReadUTF8Squirrel validates the used bytes in place (Unicode Table 3-7).
func emitReadUTF8Squirrel(m *Module, name, n, ind string) {
	m.Printf("%sfor (local i = 0; i < %s; ) {\n", ind, n)
	m.Printf("%s  local c = %s[i]; i++;\n", ind, name)
	m.Printf("%s  if (c < 0x80) continue;\n", ind)
	m.Printf("%s  local rem, point;\n", ind)
	m.Printf("%s  if ((c & 0xE0) == 0xC0) { rem = 1; point = c & 0x1F; }\n", ind)
	m.Printf("%s  else if ((c & 0xF0) == 0xE0) { rem = 2; point = c & 0x0F; }\n", ind)
	m.Printf("%s  else if ((c & 0xF8) == 0xF0) { rem = 3; point = c & 0x07; }\n", ind)
	m.Printf("%s  else return false; // malformed UTF-8 lead (SPEC §4.7)\n", ind)
	m.Printf("%s  if (rem > %s - i) return false;\n", ind, n)
	m.Printf("%s  for (local j = 0; j < rem; j++) {\n", ind)
	m.Printf("%s    local b = %s[i]; i++;\n", ind, name)
	m.Printf("%s    if ((b & 0xC0) != 0x80) return false;\n", ind)
	m.Printf("%s    point = (point << 6) | (b & 0x3F);\n", ind)
	m.Printf("%s  }\n", ind)
	m.Printf("%s  if (rem == 1 && point < 0x80) return false;\n", ind)
	m.Printf("%s  if (rem == 2 && (point < 0x800 || (point >= 0xD800 && point <= 0xDFFF))) return false;\n", ind)
	m.Printf("%s  if (rem == 3 && (point < 0x10000 || point > 0x10FFFF)) return false;\n", ind)
	m.Printf("%s}\n", ind)
}

// emitReadWString reads a wstring field: the length in UTF-16 code units via
// the runtime ranged call, then each unit as a 32-bit group into the
// pre-allocated integer array (SPEC §4.12).
func emitReadWString(m *Module, f *ir.Field, target, ind string, rs *readScratch) {
	scratch := rs.decl(m)
	m.Printf("%sif (!stream.serializeInt(%s, 0, %d)) return false;\n", ind, scratch, f.Type.Size)
	m.Printf("%s%sLength = %s.value;\n", ind, target, scratch)
	m.Printf("%sfor (local i = 0; i < %sLength; i++) {\n", ind, target)
	m.Printf("%s  if (!stream.serializeBits(%s, 32)) return false;\n", ind, scratch)
	m.Printf("%s  %s[i] = %s.value;\n", ind, target, scratch)
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
