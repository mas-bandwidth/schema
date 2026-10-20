// The type wire's READ side (docs/PORT-LOBSTER.md, THE TYPE MAPPING and THE
// RUNTIME LAYOUT; SPEC §4.3, §5, §6.1). Every type and union gets a
// Read<Name>(stream, value) function that decodes the C++-pinned wire through
// the vendored serialize port (runtime/serialize*.lobster), mirroring the Go
// reference's Read<Name> (internal/codegen/golang/functions.go) and the
// JavaScript runtime tier. Reads stay on the serialize port's ranged calls, so
// a latched stream error is the port's; generated validation refusals (a wrong
// const, nonzero reserved bits or align padding, an out-of-range value, an
// out-of-range union tag, malformed UTF-8, an interior NUL, a malformed
// wstring unit) return false without latching.
//
// Storage is the §6.1 storage principle: a record is a class instance, and a
// read fills its pre-allocated buffers in place. The untaken side of a wire
// branch reads as the §5 ZERO form, never the specified defaults.
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
// same switch as the other targets, so every emitter writes identical wire.
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
	return "bits64"
}

// readPart is called once per module after its type surface.
func readPart(g *Gen) error {
	order := ir.EmissionOrder(g.File)
	hasRead := false
	for _, d := range order {
		switch d.(type) {
		case *ir.Struct, *ir.Union:
			hasRead = true
		}
	}
	if hasRead {
		// The read functions name the vendored serialize port's stream type
		// and its leaf families. Import the modules they use directly rather
		// than the `serialize` entry: the entry also compiles serialize_bytes
		// and serialize_compressed_float, whose compiler warnings would
		// precede every generated program's stdout. This file emits the two
		// pieces it needs from those families (emitReadStringValid,
		// emitReadCompressedFloat) over the safe leaf modules below.
		g.Runtimes = append(g.Runtimes,
			"serialize_stream",
			"serialize_bitpacker",
			"serialize_int",
			"serialize_int128",
			"serialize_float",
			"serialize_fixed",
		)
	}
	for _, d := range order {
		switch d := d.(type) {
		case *ir.Struct:
			emitReadStruct(g, d)
		case *ir.Union:
			emitReadUnion(g, d)
		}
	}
	if hasRead {
		emitReadStringValid(g)
	}
	return nil
}

// emitReadStringValid emits this module's own string checker: the string(N)
// read refuses an interior NUL and malformed UTF-8 (SPEC §4.7, §4.3). It is
// generated here rather than taken from serialize_bytes so the generated
// modules never compile that family (its wstring writer emits a warning that
// would precede every program's stdout); the rules and the byte-wise table are
// the vendored port's, kept identical so the two agree.
func emitReadStringValid(g *Gen) {
	g.Pf("// %sReadStringValid is 's[0:n] has no NUL and is well-formed UTF-8' (SPEC §4.7).\n", g.File.Base)
	g.Pf("def %sReadStringValid(s:string, n:int) -> bool:\n", g.File.Base)
	g.Pf("    var i = 0\n")
	g.Pf("    while i < n:\n")
	g.Pf("        let lead = read_uint8_le(s, i)\n")
	g.Pf("        if lead == 0:\n")
	g.Pf("            return false\n")
	g.Pf("        if lead < 0x80:\n")
	g.Pf("            i += 1\n")
	g.Pf("        elif (lead & 0xE0) == 0xC0:\n")
	g.Pf("            if lead < 0xC2:\n")
	g.Pf("                return false\n")
	g.Pf("            if i + 1 >= n:\n")
	g.Pf("                return false\n")
	g.Pf("            if (read_uint8_le(s, i + 1) & 0xC0) != 0x80:\n")
	g.Pf("                return false\n")
	g.Pf("            i += 2\n")
	g.Pf("        elif (lead & 0xF0) == 0xE0:\n")
	g.Pf("            if i + 2 >= n:\n")
	g.Pf("                return false\n")
	g.Pf("            let b1 = read_uint8_le(s, i + 1)\n")
	g.Pf("            let b2 = read_uint8_le(s, i + 2)\n")
	g.Pf("            if (b1 & 0xC0) != 0x80 or (b2 & 0xC0) != 0x80:\n")
	g.Pf("                return false\n")
	g.Pf("            if lead == 0xE0 and b1 < 0xA0:\n")
	g.Pf("                return false\n")
	g.Pf("            if lead == 0xED and b1 >= 0xA0:\n")
	g.Pf("                return false\n")
	g.Pf("            i += 3\n")
	g.Pf("        elif (lead & 0xF8) == 0xF0:\n")
	g.Pf("            if lead > 0xF4:\n")
	g.Pf("                return false\n")
	g.Pf("            if i + 3 >= n:\n")
	g.Pf("                return false\n")
	g.Pf("            let b1 = read_uint8_le(s, i + 1)\n")
	g.Pf("            let b2 = read_uint8_le(s, i + 2)\n")
	g.Pf("            let b3 = read_uint8_le(s, i + 3)\n")
	g.Pf("            if (b1 & 0xC0) != 0x80 or (b2 & 0xC0) != 0x80 or (b3 & 0xC0) != 0x80:\n")
	g.Pf("                return false\n")
	g.Pf("            if lead == 0xF0 and b1 < 0x90:\n")
	g.Pf("                return false\n")
	g.Pf("            if lead == 0xF4 and b1 >= 0x90:\n")
	g.Pf("                return false\n")
	g.Pf("            i += 4\n")
	g.Pf("        else:\n")
	g.Pf("            return false\n")
	g.Pf("    return true\n\n")
}

// readBody accumulates one read function: its statements, and the scratch
// declarations those statements need, emitted once at the top of the body
// (Lobster warns on an unused variable, and the tests compare stdout whole).
type readBody struct {
	b                          strings.Builder
	needInt, needOK, needFloat bool
	needBool, needString       bool
	needInt128, needUint128    bool
	needAlign, needBits        bool
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
	r.needFloat = true
	return "rf"
}

func (r *readBody) boolean() string {
	r.needBool, r.needOK = true, true
	return "rb"
}

func (r *readBody) str() string {
	r.needString = true
	return "rs"
}

func (r *readBody) int128() string {
	r.needInt128, r.needOK = true, true
	return "rw"
}

func (r *readBody) uint128() string {
	r.needUint128, r.needOK = true, true
	return "ru"
}

func (r *readBody) align() string {
	r.needAlign = true
	return "ralign"
}

func (r *readBody) bits() string {
	r.needBits = true
	return "rbits"
}

func (r *readBody) pf(ind, format string, args ...any) {
	r.b.WriteString(ind)
	fmt.Fprintf(&r.b, format, args...)
	r.b.WriteByte('\n')
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
	if r.needString {
		b.WriteString("    var rs = \"\"\n")
	}
	if r.needInt128 {
		b.WriteString("    var rw = serialize_int128 { lo: 0, hi: 0 }\n")
	}
	if r.needUint128 {
		b.WriteString("    var ru = serialize_uint128 { lo: 0, hi: 0 }\n")
	}
	if r.needAlign {
		b.WriteString("    var ralign = 0\n")
	}
	if r.needBits {
		b.WriteString("    var rbits = 0\n")
	}
	return b.String()
}

// emitReadStruct is one type's reader: the wire tree of st.Items in order,
// with the untaken side of each branch zeroed (SPEC §5).
func emitReadStruct(g *Gen, st *ir.Struct) {
	r := &readBody{}
	g.Pf("// Read%s decodes the wire into value, filling its pre-allocated storage\n", st.Name)
	g.Pf("// (SPEC §6.1); the untaken side of a branch reads as zero (SPEC §5).\n")
	if len(st.Items) == 0 {
		g.Pf("def Read%s(stream:serialize_read_stream, value:%s) -> bool:\n", st.Name, st.Name)
		g.Pf("    // empty body — presence is the payload (SPEC §4.6)\n")
		g.Pf("    return true\n\n")
		return
	}
	emitReadItems(g, r, st.Items, "    ")
	g.Pf("def Read%s(stream:serialize_read_stream, value:%s) -> bool:\n", st.Name, st.Name)
	g.Pf("%s", r.decls())
	g.Pf("%s", r.b.String())
	g.Pf("    return true\n\n")
}

// emitReadUnion is a one-of reader (SPEC §4.8): the tag in [0, Max] via the
// ranged call (which rejects a tag past the arms), then the selected arm's
// payload. A payload-free arm is the tag alone; None is the tag alone.
func emitReadUnion(g *Gen, d *ir.Union) {
	r := &readBody{}
	tag := d.Name + "Type"
	g.Pf("// Read%s decodes the %s tag and its selected arm (SPEC §4.8); a tag\n", d.Name, tag)
	g.Pf("// past the arms is refused, and None costs the tag bits only.\n")
	g.Pf("def Read%s(stream:serialize_read_stream, value:%s) -> bool:\n", d.Name, d.Name)
	if d.Max == 0 {
		g.Pf("    value.Type = %s // zero wire bits — only None exists (SPEC §4.8)\n", VariantName(tag, "None"))
		g.Pf("    return true\n\n")
		return
	}
	v := r.int()
	r.pf("    ", "%s, %s = stream.serialize_int(%s, 0, %d) // rejects a tag above the arms (SPEC §4.8)", v, r.ok(), v, d.Max)
	r.pf("    ", "if not %s: return false", r.ok())
	r.pf("    ", "value.Type = %s(%s)", tag, v)
	for _, arm := range d.Variants {
		if arm.Void() {
			continue
		}
		name := ir.GoExportName(arm.Name)
		r.pf("    ", "if value.Type == %s:", VariantName(tag, arm.Name))
		if arm.Type != "" {
			r.pf("        ", "if not Read%s(stream, value.%s): return false", arm.Type, name)
		} else {
			emitReadField(g, r, arm.F, "value."+name, "        ")
		}
		r.pf("        ", "return true")
	}
	r.pf("    ", "return true // None")
	g.Pf("%s", r.decls())
	g.Pf("%s", r.b.String())
	g.Pf("\n")
}

// emitReadItems is one wire tree in order.
func emitReadItems(g *Gen, r *readBody, items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			emitReadField(g, r, item.F, "value."+ir.GoExportName(item.F.Name), ind)
		case *ir.ConstItem:
			v := r.int()
			fn := "serialize_bits"
			if item.Bits > 32 {
				fn = "serialize_bits64"
			}
			r.pf(ind, "%s, %s = stream.%s(%s, %d)", v, r.ok(), fn, v, item.Bits)
			r.pf(ind, "if not %s: return false", r.ok())
			r.pf(ind, "if %s != %s: return false // const(%s, %d): a read rejects any other value (SPEC §4.3)",
				v, IntLit(item.Value), item.Value.String(), item.Bits)
		case *ir.ReservedItem:
			v := r.int()
			fn := "serialize_bits"
			if item.Bits > 32 {
				fn = "serialize_bits64"
			}
			r.pf(ind, "%s, %s = stream.%s(%s, %d)", v, r.ok(), fn, v, item.Bits)
			r.pf(ind, "if not %s: return false", r.ok())
			r.pf(ind, "if %s != 0: return false // reserved(%d): a read rejects nonzero (SPEC §4.3)", v, item.Bits)
		case *ir.AlignItem:
			r.pf(ind, "if not stream.serialize_align(): return false // rejects nonzero padding (SPEC §4.3)")
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "not "
			}
			r.pf(ind, "if %svalue.%s:", neg, ir.GoExportName(item.Cond))
			emitReadItems(g, r, item.Then, ind+"    ")
			emitZeroItems(g, r, item.Else, ind+"    ")
			r.pf(ind, "else:")
			if item.Else != nil {
				emitReadItems(g, r, item.Else, ind+"    ")
			}
			emitZeroItems(g, r, item.Then, ind+"    ")
		}
	}
}

// emitReadField reads one field into target (a storage path expression).
func emitReadField(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	if f.Array != ir.ArrayNone {
		emitReadArray(g, r, f, target, ind)
		return
	}
	emitReadScalar(g, r, f, target, ind)
}

func emitReadArray(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	counted := f.Array == ir.ArrayCounted
	limit := fmt.Sprintf("%d", f.ArrayBound)
	if counted {
		v := r.int()
		r.pf(ind, "%s, %s = stream.serialize_int(%s, %d, %d) // the count guards the loop (SPEC §6.3)", v, r.ok(), v, f.ArrayMin, f.ArrayBound)
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%sCount = %s", target, v)
		limit = target + "Count"
	}
	r.pf(ind, "for(%s) i:", limit)
	switch {
	case f.Type.Kind == ir.TNamed && classRef(f.Type.Ref):
		r.pf(ind+"    ", "if not Read%s(stream, %s[i]): return false", f.Type.Name, target)
	case isByteElem(f.Type):
		v := r.int()
		if f.HasIntRange {
			r.pf(ind+"    ", "%s, %s = stream.serialize_int(%s, %s, %s)", v, r.ok(), v, f.IntMin.String(), f.IntMax.String())
		} else {
			r.pf(ind+"    ", "%s, %s = stream.serialize_bits(%s, 8)", v, r.ok(), v)
		}
		r.pf(ind+"    ", "if not %s: return false", r.ok())
		r.pf(ind+"    ", "%s = write_int8_le(%s, i, %s)", target, target, v)
	default:
		emitReadScalar(g, r, f, target+"[i]", ind+"    ")
	}
}

func classRef(ref any) bool {
	switch ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// emitReadScalar reads one scalar (or a whole-string/byte) field.
func emitReadScalar(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	switch f.Type.Kind {
	case ir.TBool:
		v := r.boolean()
		r.pf(ind, "%s, %s = stream.serialize_bool(%s)", v, r.ok(), v)
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%s = %s", target, v)
	case ir.TInt:
		if f.Type.Width == 128 {
			emitReadInt128(g, r, f, target, ind)
			return
		}
		emitReadInt(g, r, f, target, ind)
	case ir.TBits:
		v := r.int()
		fn := "serialize_bits"
		if f.Type.Width > 32 {
			fn = "serialize_bits64"
		}
		r.pf(ind, "%s, %s = stream.%s(%s, %d)", v, r.ok(), fn, v, f.Type.Width)
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%s = %s", target, v)
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			r.pf(ind, "%s = %s", target, IntLit(new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))))
			return
		}
		if f.Type.Width > 64 {
			v := r.int128()
			r.pf(ind, "%s, %s = stream.serialize_fixed128(%s, %d, %d, %s, %s)",
				v, r.ok(), v, f.Type.IntBits, f.Type.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
			r.pf(ind, "if not %s: return false", r.ok())
			r.pf(ind, "%s = %s.Int128 { lo: %s.lo, hi: %s.hi }", target, RuntimeNamespace, v, v)
			return
		}
		v := r.int()
		r.pf(ind, "%s, %s = stream.serialize_fixed64(%s, %d, %d, %s, %s)",
			v, r.ok(), v, f.Type.IntBits, f.Type.FracBits, IntLit(f.IntMin), IntLit(f.IntMax))
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%s = %s", target, v)
	case ir.TFloat32:
		v := r.float()
		if f.HasFloatRange {
			emitReadCompressedFloat(g, r, f, target, ind)
			return
		}
		r.pf(ind, "%s, %s = stream.serialize_float(%s)", v, r.ok(), v)
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%s = %s", target, v)
	case ir.TFloat64:
		v := r.float()
		r.pf(ind, "%s, %s = stream.serialize_double(%s)", v, r.ok(), v)
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%s = %s", target, v)
	case ir.TWString:
		emitReadWString(g, r, f, target, ind)
	case ir.TString, ir.TBytes:
		emitReadString(g, r, f, target, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			v := r.int()
			r.pf(ind, "%s, %s = stream.serialize_int(%s, 0, %d)", v, r.ok(), v, ref.Max)
			r.pf(ind, "if not %s: return false", r.ok())
			r.pf(ind, "%s = %s(%s)", target, f.Type.Name, v)
		case *ir.Flags:
			v := r.int()
			fn := "serialize_bits"
			if ref.WireBits > 32 {
				fn = "serialize_bits64"
			}
			r.pf(ind, "%s, %s = stream.%s(%s, %d)", v, r.ok(), fn, v, ref.WireBits)
			r.pf(ind, "if not %s: return false", r.ok())
			r.pf(ind, "%s = %s", target, v)
		case *ir.Struct, *ir.Union:
			r.pf(ind, "if not Read%s(stream, %s): return false", f.Type.Name, target)
		}
	}
}

// emitReadCompressedFloat folds the declaration's parameters at generation
// time (ir.CompressedFloatParams, the same derivation the port makes per call)
// and reads the quantized integer directly, so no module from the
// serialize_compressed_float family is compiled (SPEC §4.3; STANDARD.md,
// compressed_float). The dequantization keeps the port's two float32
// roundings: a fused multiply-add would decode differently (docs/PORT-LOBSTER.md,
// FLOAT32).
func emitReadCompressedFloat(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	v := r.float()
	b := r.bits()
	r.pf(ind, "if stream.failed: return false")
	r.pf(ind, "if not stream.read_gate(%d): return false", bits)
	r.pf(ind, "%s = stream.unpack_bits(%d)", b, bits)
	if steps != (uint64(1)<<uint(bits))-1 {
		r.pf(ind, "if %s > %d: return false // a value in the bit headroom is refused (SPEC §4.3)", b, steps)
	}
	normalized := fmt.Sprintf("serialize_float32_round(float(%s) / float(%d))", b, steps)
	scaled := fmt.Sprintf("serialize_float32_round(%s * %s)", normalized, FormatFloat32(float64(delta)))
	if min32 == 0 {
		r.pf(ind, "%s = %s", v, scaled)
	} else {
		r.pf(ind, "%s = serialize_float32_round(%s + %s)", v, scaled, FormatFloat32(float64(min32)))
	}
	r.pf(ind, "%s = %s", target, v)
}

// emitReadInt reads a bare or ranged integer of 64 bits or fewer.
func emitReadInt(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	v := r.int()
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			r.pf(ind, "%s = %s", target, IntLit(f.IntMin))
			return
		}
		switch intRangePath(f.IntMin, f.IntMax) {
		case "int32":
			r.pf(ind, "%s, %s = stream.serialize_int(%s, %s, %s)", v, r.ok(), v, f.IntMin.String(), f.IntMax.String())
		case "int64":
			r.pf(ind, "%s, %s = stream.serialize_int64(%s, %s, %s)", v, r.ok(), v, IntLit(f.IntMin), IntLit(f.IntMax))
		default:
			diff := new(big.Int).Sub(f.IntMax, f.IntMin)
			r.pf(ind, "%s, %s = stream.serialize_bits64(%s, %d)", v, r.ok(), v, ir.BitsRequired(f.IntMin, f.IntMax))
			r.pf(ind, "if not %s: return false", r.ok())
			if diff.Cmp(maxUint64) != 0 {
				r.pf(ind, "if %s > %s: return false // a read rejects out-of-range (SPEC §5)", v, IntLit(diff))
			}
			if f.IntMin.Sign() == 0 {
				r.pf(ind, "%s = %s", target, v)
			} else {
				r.pf(ind, "%s = %s + %s", target, v, IntLit(f.IntMin))
			}
			return
		}
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%s = %s", target, v)
		return
	}
	fn := "serialize_bits"
	if f.Type.Width > 32 {
		fn = "serialize_bits64"
	}
	r.pf(ind, "%s, %s = stream.%s(%s, %d)", v, r.ok(), fn, v, f.Type.Width)
	r.pf(ind, "if not %s: return false", r.ok())
	if f.Type.Signed && f.Type.Width < 64 {
		r.pf(ind, "%s = (%s << %d) >> %d", target, v, 64-f.Type.Width, 64-f.Type.Width)
		return
	}
	r.pf(ind, "%s = %s", target, v)
}

func emitReadInt128(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	if f.HasIntRange {
		if f.IntMin.Cmp(f.IntMax) == 0 {
			r.pf(ind, "%s = %s", target, Int128Lit(f.IntMin))
			return
		}
		v := r.int128()
		r.pf(ind, "%s, %s = stream.serialize_int128(%s, %s, %s)",
			v, r.ok(), v, serializeInt128Lit(f.IntMin), serializeInt128Lit(f.IntMax))
		r.pf(ind, "if not %s: return false", r.ok())
		r.pf(ind, "%s = %s.Int128 { lo: %s.lo, hi: %s.hi }", target, RuntimeNamespace, v, v)
		return
	}
	v := r.uint128()
	r.pf(ind, "%s, %s = stream.serialize_uint128(%s)", v, r.ok(), v)
	r.pf(ind, "if not %s: return false", r.ok())
	r.pf(ind, "%s = %s.Int128 { lo: %s.lo, hi: %s.hi }", target, RuntimeNamespace, v, v)
}

// serializeInt128Lit is Int128Lit in the serialize port's own struct type.
func serializeInt128Lit(v *big.Int) string {
	m128 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	bits := new(big.Int).And(v, m128)
	lo := new(big.Int).And(bits, mask64)
	hi := new(big.Int).Rsh(bits, 64)
	return fmt.Sprintf("serialize_int128 { lo: 0x%016x, hi: 0x%016x }", lo, hi)
}

// emitReadString reads a string/bytes field: the length in [0, N], then the
// used bytes; a string refuses an interior NUL and malformed UTF-8 (SPEC §4.7).
func emitReadString(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	v := r.int()
	s := r.str()
	r.pf(ind, "%s, %s = stream.serialize_int(%s, 0, %d) // the length guards the copy (§6.3)", v, r.ok(), v, f.Type.Size)
	r.pf(ind, "if not %s: return false", r.ok())
	r.pf(ind, "%sLength = %s", target, v)
	r.pf(ind, "%s = ensure_size(\"\", %s, 0)", s, v)
	align := r.align()
	r.pf(ind, "%s = stream.align_bits()", align)
	r.pf(ind, "if not stream.read_gate(%s + %s * 8): return false", align, v)
	r.pf(ind, "if %s > 0 and stream.unpack_bits(%s) != 0: return false // the align padding must be zero (SPEC §4.3)", align, align)
	r.pf(ind, "if %s > 0:", v)
	r.pf(ind+"    ", "stream.unpack_bytes(%s, 0, %s)", s, v)
	if strings.Contains(target, "[") {
		r.pf(ind, "write_substring(%s, 0, %s, false)", target, s)
	} else {
		r.pf(ind, "%s = write_substring(%s, 0, %s, false)", target, target, s)
	}
	if f.Type.Kind == ir.TString {
		r.pf(ind, "if not %sReadStringValid(%s, %s): return false // an interior NUL or malformed UTF-8 refuses (SPEC §4.7)", g.File.Base, s, v)
	}
}

// emitReadWString reads a wstring: the length in code units, then each unit as
// a 32-bit group into the UTF-16LE byte buffer (SPEC §4.12).
func emitReadWString(g *Gen, r *readBody, f *ir.Field, target, ind string) {
	v := r.int()
	unit := r.int()
	r.pf(ind, "%s, %s = stream.serialize_int(%s, 0, %d)", v, r.ok(), v, f.Type.Size)
	r.pf(ind, "if not %s: return false", r.ok())
	r.pf(ind, "%sLength = %s", target, v)
	r.pf(ind, "var expect_low = false")
	r.pf(ind, "for(%s) i:", target+"Length")
	r.pf(ind+"    ", "%s, %s = stream.serialize_bits(%s, 32)", unit, r.ok(), unit)
	r.pf(ind+"    ", "if not %s: return false", r.ok())
	r.pf(ind+"    ", "if %s == 0 or %s > 0xFFFF: return false // a zero or non-BMP unit is refused (SPEC §4.12)", unit, unit)
	r.pf(ind+"    ", "let high = %s >= 0xD800 and %s <= 0xDBFF", unit, unit)
	r.pf(ind+"    ", "let low = %s >= 0xDC00 and %s <= 0xDFFF", unit, unit)
	r.pf(ind+"    ", "if low != expect_low: return false // a lone surrogate is refused (SPEC §4.12)")
	r.pf(ind+"    ", "expect_low = high")
	r.pf(ind+"    ", "%s = write_int16_le(%s, i * 2, %s)", target, target, unit)
	r.pf(ind, "if expect_low: return false // a truncated surrogate pair is refused (SPEC §4.12)")
}

// emitZeroItems zero-initializes every field under an untaken branch side
// (SPEC §5: ZERO values, not specified defaults).
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
	switch {
	case f.Array != ir.ArrayNone:
		limit := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			r.pf(ind, "%sCount = 0", target)
			limit = target + "Count"
		}
		r.pf(ind, "for(%s) i:", limit)
		if f.Type.Kind == ir.TNamed && classRef(f.Type.Ref) {
			e := *f
			e.Array = ir.ArrayNone
			emitZeroField(g, r, &e, target+"[i]", ind+"    ")
		} else {
			emitZeroScalar(r, f, target+"[i]", ind+"    ")
		}
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		r.pf(ind, "%s = %s.zeros(%d)", target, RuntimeNamespace, f.Type.Size)
		r.pf(ind, "%sLength = 0", target)
	case f.Type.Kind == ir.TWString:
		r.pf(ind, "%s = %s.zeros(%d)", target, RuntimeNamespace, 2*f.Type.Size)
		r.pf(ind, "%sLength = 0", target)
	case f.Type.Kind == ir.TNamed && classRef(f.Type.Ref):
		if _, union := f.Type.Ref.(*ir.Union); union {
			r.pf(ind, "%s.Type = %s // the empty union (SPEC §4.8)", target, VariantName(f.Type.Name+"Type", "None"))
			return
		}
		st := f.Type.Ref.(*ir.Struct)
		for _, sf := range st.Fields {
			emitZeroField(g, r, sf, target+"."+ir.GoExportName(sf.Name), ind)
		}
	default:
		emitZeroScalar(r, f, target, ind)
	}
}

func emitZeroScalar(r *readBody, f *ir.Field, target, ind string) {
	switch f.Type.Kind {
	case ir.TFloat32, ir.TFloat64:
		r.pf(ind, "%s = 0.0", target)
	case ir.TBool:
		r.pf(ind, "%s = false", target)
	case ir.TInt, ir.TBits, ir.TFixed:
		if f.Type.Width > 64 {
			r.pf(ind, "%s = %s.Int128 { lo: 0, hi: 0 }", target, RuntimeNamespace)
			return
		}
		r.pf(ind, "%s = 0", target)
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			r.pf(ind, "%s = %s", target, VariantName(f.Type.Name, "None"))
		default:
			r.pf(ind, "%s = 0", target)
		}
	default:
		r.pf(ind, "%s = 0", target)
	}
}
