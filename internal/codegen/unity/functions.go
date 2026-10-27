// Write/Read emission (SPEC §6.1 items 2-4, §6.2): split static functions per
// type and union against serialize.cs's sealed WriteStream/ReadStream, every
// call's bool checked with an early-out, counts and lengths validated before
// the loops and slices they guard. The wire is byte-identical to the C++
// reference, construct by construct; test/unity holds it to the pinned bytes.
//
// Write-side contracts are DEBUG ONLY (docs/CONTRIBUTING.md rule 4): every
// caller-error check on the write path is a Debug.Assert, which is
// [Conditional("DEBUG")] and gone from a release build with its arguments.
// The read path keeps its whole validation set in every build.
package unity

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.needsSerialize = true
	maxBits := ir.MaxBitsStruct(st)
	g.emitBounds(st.Name, maxBits, "the longest wire path; align pads at worst case (SPEC §6.1)")

	g.sf("// Zero%s: the §5 zero form — all-zero storage, which is default.\n", st.Name)
	g.sf("public static void Zero%s(ref %s value)\n{\n    value = default;\n}\n\n", st.Name, st.Name)
	if hasDefaults(st) {
		g.sf("// Init%s restores the construction form in place: zero, then the specified\n", st.Name)
		g.sf("// defaults and born counts.\n")
		g.sf("public static void Init%s(ref %s value)\n{\n    value = default;\n", st.Name, st.Name)
		for _, f := range st.Fields {
			g.emitInitField(f, "    ")
		}
		g.sf("}\n\n")
		g.sf("// New%s returns a %s in its construction form.\n", st.Name, st.Name)
		g.sf("public static %s New%s()\n{\n    %s value = default;\n    Init%s(ref value);\n    return value;\n}\n\n", st.Name, st.Name, st.Name, st.Name)
	}

	aligned := ir.AlignedFixedByteArrays(st)
	g.sf("public static bool Write%s(WriteStream stream, ref %s value)\n{\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.sf("    // an empty body: presence is the payload (SPEC §4.6)\n")
	}
	g.emitItems(st.Items, true, aligned, "    ")
	g.sf("    return true;\n}\n\n")

	g.sf("public static bool Read%s(ReadStream stream, ref %s value)\n{\n", st.Name, st.Name)
	g.emitItems(st.Items, false, aligned, "    ")
	g.sf("    return true;\n}\n\n")
}

func (g *gen) emitBounds(name string, maxBits int64, what string) {
	g.sf("// %sMaxBits is %s;\n", name, what)
	g.sf("// %sMaxBytes rounds it up to the 8-byte write-buffer granularity.\n", name)
	g.sf("public const %s %sMaxBits = %d;\n", sizeType(maxBits), name, maxBits)
	g.sf("public const %s %sMaxBytes = %d;\n\n", sizeType(ir.MaxBytes(maxBits)), name, ir.MaxBytes(maxBits))
}

func sizeType(v int64) string {
	if v <= math.MaxInt32 {
		return "int"
	}
	return "long"
}

// emitInitField applies one field's construction value over zeroed storage.
func (g *gen) emitInitField(f *ir.Field, ind string) {
	name := "value." + member(f)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		if f.HasDefault && len(f.DefBytes) > 0 {
			for i, b := range f.DefBytes {
				g.sf("%s%s.E%d = 0x%02x;\n", ind, name, i, b)
			}
			g.sf("%s%sLength = %d;\n", ind, name, len(f.DefBytes))
		}
	case f.Array != ir.ArrayNone:
		if inner, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed && hasDefaults(inner) {
			span := elemSpan(f)
			g.sf("%s{\n%s    Span<%s> %s = %s.AsSpan();\n", ind, ind, f.Type.Name, span, name)
			g.sf("%s    for (int i = 0; i < %d; i++)\n%s    {\n", ind, f.ArrayBound, ind)
			g.sf("%s        Init%s(ref %s[i]);\n%s    }\n%s}\n", ind, f.Type.Name, span, ind, ind)
		}
		if n := f.BornCount(); n > 0 {
			g.sf("%s%sCount = %d;\n", ind, name, n)
		}
	case f.HasDefault:
		g.sf("%s%s = %s;\n", ind, name, g.defaultValue(f))
	case f.Type.Kind == ir.TNamed:
		if inner, ok := f.Type.Ref.(*ir.Struct); ok && hasDefaults(inner) {
			g.sf("%sInit%s(ref %s);\n", ind, f.Type.Name, name)
		}
	}
}

// emitItems walks a body in wire order.
func (g *gen) emitItems(items []ir.Item, writing bool, aligned map[*ir.Field]bool, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if writing {
				g.emitWriteField(item.F, aligned[item.F], ind)
			} else {
				g.emitReadField(item.F, aligned[item.F], ind)
			}
		case *ir.ConstItem:
			g.emitConstItem(item, writing, ind)
		case *ir.ReservedItem:
			g.emitReservedItem(item, writing, ind)
		case *ir.AlignItem:
			comment := ""
			if !writing {
				comment = " // rejects nonzero padding (SPEC §4.3)"
			}
			g.call(ind, "stream.SerializeAlign()", comment)
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.sf("%sif (%svalue.%s)\n%s{\n", ind, neg, ir.GoExportName(item.Cond), ind)
			g.emitItems(item.Then, writing, aligned, ind+"    ")
			if !writing {
				g.emitZeroItems(item.Else, ind+"    ") // the untaken side reads as zero (SPEC §5)
			}
			g.sf("%s}\n", ind)
			if item.Else != nil || !writing {
				g.sf("%selse\n%s{\n", ind, ind)
				g.emitItems(item.Else, writing, aligned, ind+"    ")
				if !writing {
					g.emitZeroItems(item.Then, ind+"    ")
				}
				g.sf("%s}\n", ind)
			}
		}
	}
}

// emitZeroItems zeroes every field under an untaken branch side (SPEC §5).
func (g *gen) emitZeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			f := item.F
			g.sf("%svalue.%s = default;\n", ind, member(f))
			switch {
			case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
				g.sf("%svalue.%sLength = 0;\n", ind, member(f))
			case f.Array == ir.ArrayCounted:
				g.sf("%svalue.%sCount = 0;\n", ind, member(f))
			}
		case *ir.Branch:
			g.emitZeroItems(item.Then, ind)
			g.emitZeroItems(item.Else, ind)
		}
	}
}

// call emits the bool early-out around one serialize call.
func (g *gen) call(ind, expr, comment string) {
	g.sf("%sif (!%s)%s\n%s{\n%s    return false;\n%s}\n", ind, expr, comment, ind, ind, ind)
}

func (g *gen) reject(ind, cond, comment string) {
	g.sf("%sif (%s)%s\n%s{\n%s    return false;\n%s}\n", ind, cond, comment, ind, ind, ind)
}

// writeAssert emits one write-side contract; cond is the positive form.
func (g *gen) writeAssert(ind, cond, message string) {
	g.needsDiag = true
	g.sf("%sDebug.Assert(%s, %q);\n", ind, cond, message)
}

func bitsCall(bits int64) (typ, fn string) {
	if bits > 32 {
		return "ulong", "SerializeBits64"
	}
	return "uint", "SerializeBits"
}

func (g *gen) emitConstItem(item *ir.ConstItem, writing bool, ind string) {
	typ, fn := bitsCall(item.Bits)
	if writing {
		g.sf("%s{\n%s    %s constValue = %s; // const(%s, %d) (SPEC §4.3)\n", ind, ind, typ, item.Value, item.Value, item.Bits)
		g.call(ind+"    ", fmt.Sprintf("stream.%s(ref constValue, %d)", fn, item.Bits), "")
		g.sf("%s}\n", ind)
		return
	}
	g.sf("%s{\n%s    %s constValue = 0;\n", ind, ind, typ)
	g.call(ind+"    ", fmt.Sprintf("stream.%s(ref constValue, %d)", fn, item.Bits), "")
	g.reject(ind+"    ", fmt.Sprintf("constValue != %s", item.Value), fmt.Sprintf(" // const(%s, %d): any other value is refused (SPEC §4.3)", item.Value, item.Bits))
	g.sf("%s}\n", ind)
}

func (g *gen) emitReservedItem(item *ir.ReservedItem, writing bool, ind string) {
	typ, fn := bitsCall(item.Bits)
	g.sf("%s{\n%s    %s reservedValue = 0;\n", ind, ind, typ)
	g.call(ind+"    ", fmt.Sprintf("stream.%s(ref reservedValue, %d)", fn, item.Bits), "")
	if !writing {
		g.reject(ind+"    ", "reservedValue != 0", fmt.Sprintf(" // reserved(%d): nonzero is refused (SPEC §4.3)", item.Bits))
	}
	g.sf("%s}\n", ind)
}

// ---- integers on the write path: the range folded at generation time ----

// storageBounds is the representable range of an integer storage type.
func storageBounds(t ir.FieldType) (*big.Int, *big.Int) {
	w := uint(t.Width)
	if t.Signed {
		half := new(big.Int).Lsh(big.NewInt(1), w-1)
		return new(big.Int).Neg(half), new(big.Int).Sub(half, big.NewInt(1))
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), w), big.NewInt(1))
}

// intPath picks the runtime call family for a ranged integer, the switch every
// target shares so the wire is one: int32, int64, or raw offset bits for a
// range only unsigned 64-bit storage can hold.
func intPath(min, max *big.Int) string {
	if min.Cmp(big.NewInt(math.MinInt32)) >= 0 && max.Cmp(big.NewInt(math.MaxInt32)) <= 0 {
		return "int32"
	}
	if min.Cmp(minInt64) >= 0 && max.Cmp(maxInt64) <= 0 {
		return "int64"
	}
	return "bits64"
}

// emitWriteOffset writes expr in [min, max] as its offset from min in
// BitsRequired(min, max) bits — byte-identical to the runtime's ranged
// SerializeInt/SerializeInt64 — with the range contract as a debug assertion
// (each half elided where the storage cannot escape it).
func (g *gen) emitWriteOffset(expr string, min, max *big.Int, assertLo, assertHi, wide bool, ind string) {
	msg := fmt.Sprintf("%s out of range [%s, %s]", expr, min, max)
	switch {
	case assertLo && assertHi:
		g.writeAssert(ind, fmt.Sprintf("%s >= %s && %s <= %s", expr, min, expr, max), msg)
	case assertLo:
		g.writeAssert(ind, fmt.Sprintf("%s >= %s", expr, min), msg)
	case assertHi:
		g.writeAssert(ind, fmt.Sprintf("%s <= %s", expr, max), msg)
	}
	bits := ir.BitsRequired(min, max)
	if bits == 0 {
		return // a degenerate range costs zero bits: the value is the range (SPEC §4.6)
	}
	typ, fn := "uint", "SerializeBits"
	if wide {
		typ, fn = "ulong", "SerializeBits64"
	}
	switch {
	case min.Sign() == 0:
		g.sf("%s{\n%s    %s offsetValue = (%s)%s;\n", ind, ind, typ, typ, expr)
	case min.Sign() < 0:
		g.sf("%s{\n%s    %s offsetValue = unchecked((%s)%s - (%s)(%s));\n", ind, ind, typ, typ, expr, typ, min)
	default:
		g.sf("%s{\n%s    %s offsetValue = (%s)%s - %s;\n", ind, ind, typ, typ, expr, min)
	}
	g.call(ind+"    ", fmt.Sprintf("stream.%s(ref offsetValue, %d)", fn, bits), "")
	g.sf("%s}\n", ind)
}

// countWrite writes a count or length in [min, max] through emitWriteOffset.
func (g *gen) countWrite(expr string, min, max int64, ind string) {
	g.emitWriteOffset(expr, big.NewInt(min), big.NewInt(max), true, true, false, ind)
}

// ---- fields ----

// elem is how a loop body names element i of an array field: through a span
// hoisted ahead of the loop, so a struct element passes by ref.
func elemSpan(f *ir.Field) string { return lowerFirst(member(f)) + "Span" }

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]|0x20) + s[1:]
}

func (g *gen) emitWriteField(f *ir.Field, aligned bool, ind string) {
	name := "value." + member(f)
	if f.Array == ir.ArrayNone {
		g.emitWriteScalar(f, name, ind)
		return
	}
	if aligned {
		// a statically byte-aligned [N]uint8: zero align bits, then one bulk copy
		// — wire-identical to the per-byte loop (SPEC §6.2)
		g.call(ind, fmt.Sprintf("stream.SerializeBytes(%s.AsSpan())", name), " // byte-aligned [N]uint8: one bulk copy")
		return
	}
	count := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		count = name + "Count"
		g.countWrite(count, f.ArrayMin, f.ArrayBound, ind)
	}
	span := elemSpan(f)
	g.sf("%s{\n%s    Span<%s> %s = %s.AsSpan();\n", ind, ind, g.storage(f.Type), span, name)
	g.sf("%s    for (int i = 0; i < %s; i++)\n%s    {\n", ind, count, ind)
	g.loopBody(ind+"        ", func(body string) { g.emitWriteScalar(f, span+"[i]", body) })
	g.sf("%s    }\n%s}\n", ind, ind)
}

func (g *gen) emitReadField(f *ir.Field, aligned bool, ind string) {
	name := "value." + member(f)
	if f.Array == ir.ArrayNone {
		g.emitReadScalar(f, name, ind)
		return
	}
	if aligned {
		g.call(ind, fmt.Sprintf("stream.SerializeBytes(%s.AsSpan())", name), " // byte-aligned [N]uint8: one bulk copy")
		return
	}
	count := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		count = name + "Count"
		g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, %d, %d)", count, f.ArrayMin, f.ArrayBound), " // the count guards the loop (§6.3)")
	}
	span := elemSpan(f)
	g.sf("%s{\n%s    Span<%s> %s = %s.AsSpan();\n", ind, ind, g.storage(f.Type), span, name)
	g.sf("%s    for (int i = 0; i < %s; i++)\n%s    {\n", ind, count, ind)
	g.loopBody(ind+"        ", func(body string) { g.emitReadScalar(f, span+"[i]", body) })
	g.sf("%s    }\n%s}\n", ind, ind)
}

// loopBody emits one element's code as a loop body. An element emitter that
// scopes its temporaries in a block of its own would read as a doubled brace
// there, so a body that is exactly one such block is emitted without it.
func (g *gen) loopBody(ind string, emit func(ind string)) {
	saved := g.schema
	g.schema = strings.Builder{}
	emit(ind)
	body := g.schema.String()
	g.schema = saved
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	block := len(lines) >= 2 && lines[0] == ind+"{" && lines[len(lines)-1] == ind+"}"
	for _, line := range lines[1 : len(lines)-1] {
		if block && !strings.HasPrefix(line, ind+"    ") {
			block = false
		}
	}
	if !block {
		g.schema.WriteString(body)
		return
	}
	for _, line := range lines[1 : len(lines)-1] {
		g.schema.WriteString(ind + strings.TrimPrefix(line, ind+"    ") + "\n")
	}
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		g.emitWriteFixed(f, name, ind)
	case ir.TInt:
		g.emitWriteInt(f, name, ind)
	case ir.TBits:
		_, fn := bitsCall(int64(f.Type.Width))
		g.call(ind, fmt.Sprintf("stream.%s(ref %s, %d)", fn, name, f.Type.Width), "")
	case ir.TBool:
		g.call(ind, fmt.Sprintf("stream.SerializeBool(ref %s)", name), "")
	case ir.TFloat32:
		if f.HasFloatRange {
			// a temp, so the quantization cannot write back into the caller's value
			g.sf("%s{\n%s    float compressedValue = %s;\n", ind, ind, name)
			g.call(ind+"    ", g.compressedCall(f, "compressedValue"), "")
			g.sf("%s}\n", ind)
			return
		}
		g.call(ind, fmt.Sprintf("stream.SerializeFloat(ref %s)", name), "")
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("stream.SerializeDouble(ref %s)", name), "")
	case ir.TString, ir.TBytes:
		// length in [0, N], align, then the used bytes (SPEC §4.7)
		length := name + "Length"
		g.countWrite(length, 0, f.Type.Size, ind)
		g.call(ind, fmt.Sprintf("stream.SerializeBytes(%s.AsSpan().Slice(0, %s))", name, length), "")
	case ir.TWString:
		// length in [0, N], then one 32-bit group per UTF-16 code unit, no
		// alignment anywhere (SPEC §4.12)
		length := name + "Length"
		g.countWrite(length, 0, f.Type.Size, ind)
		g.sf("%s{\n%s    Span<char> wide = %s.AsSpan();\n", ind, ind, name)
		g.sf("%s    for (int i = 0; i < %s; i++)\n%s    {\n", ind, length, ind)
		g.sf("%s        uint wideUnit = wide[i];\n", ind)
		g.writeAssert(ind+"        ", "wideUnit != 0", "a wide string's used units carry an interior null")
		g.call(ind+"        ", "stream.SerializeBits(ref wideUnit, 32)", "")
		g.sf("%s    }\n%s}\n", ind, ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max))
			typ, fn := "uint", "SerializeBits"
			if ref.StorageBits > 32 {
				typ, fn = "ulong", "SerializeBits64"
			}
			g.sf("%s{\n%s    %s enumValue = (%s)%s;\n", ind, ind, typ, typ, name)
			if big.NewInt(ref.Max).Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(ref.StorageBits)), big.NewInt(1))) < 0 {
				g.writeAssert(ind+"    ", fmt.Sprintf("enumValue <= %d", ref.Max), fmt.Sprintf("%s above the enum wire range [0, %d]", name, ref.Max))
			}
			if bits > 0 {
				g.call(ind+"    ", fmt.Sprintf("stream.%s(ref enumValue, %d)", fn, bits), "")
			}
			g.sf("%s}\n", ind)
		case *ir.Flags:
			if ref.WireBits < 64 {
				g.writeAssert(ind, fmt.Sprintf("(ulong)%s < 1UL << %d", name, ref.WireBits), fmt.Sprintf("%s has a mask bit above the %d-bit wire width", name, ref.WireBits))
			}
			typ, fn := bitsCall(int64(ref.WireBits))
			g.sf("%s{\n%s    %s flagsValue = (%s)%s;\n", ind, ind, typ, typ, name)
			g.call(ind+"    ", fmt.Sprintf("stream.%s(ref flagsValue, %d)", fn, ref.WireBits), "")
			g.sf("%s}\n", ind)
		case *ir.Struct, *ir.Union:
			g.call(ind, fmt.Sprintf("Write%s(stream, ref %s)", f.Type.Name, name), "")
		}
	}
}

func (g *gen) compressedCall(f *ir.Field, name string) string {
	steps, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	return fmt.Sprintf("stream.SerializeCompressedFloatPrecomputed(ref %s, %du, %d, %s, %s)",
		name, steps, wireBits, formatFloat32(float64(delta)), formatFloat32(float64(min32)))
}

func (g *gen) emitWriteInt(f *ir.Field, name, ind string) {
	t := f.Type
	if t.Width == 128 {
		switch {
		case !f.HasIntRange:
			g.call(ind, fmt.Sprintf("stream.SerializeUInt128(ref %s)", name), "") // uint128: raw, low half first
		case f.IntMin.Cmp(f.IntMax) == 0:
			g.writeAssert(ind, fmt.Sprintf("%s == (Int128Value)(%s)", name, lit128(f.IntMin, true)), name+" is not the one legal value of its degenerate range")
		default:
			g.call(ind, fmt.Sprintf("stream.SerializeInt128(ref %s, %s, %s)", name, lit128(f.IntMin, true), lit128(f.IntMax, true)), "")
		}
		return
	}
	if !f.HasIntRange {
		g.emitWriteBareInt(f, name, ind)
		return
	}
	sMin, sMax := storageBounds(t)
	switch intPath(f.IntMin, f.IntMax) {
	case "int32":
		g.emitWriteOffset(name, f.IntMin, f.IntMax, f.IntMin.Cmp(sMin) > 0, f.IntMax.Cmp(sMax) < 0, false, ind)
	case "int64":
		g.emitWriteOffset(name, f.IntMin, f.IntMax, f.IntMin.Cmp(sMin) > 0, f.IntMax.Cmp(sMax) < 0, true, ind)
	default:
		// a range past long: only ulong storage holds it, the offset is raw
		// bits in the unsigned domain
		g.emitWriteOffset(name, f.IntMin, f.IntMax, f.IntMin.Sign() > 0, f.IntMax.Cmp(maxUint64) < 0, true, ind)
	}
}

// emitWriteBareInt writes a bare integer at its storage width; a signed value
// goes through the same-width unsigned so no sign extension reaches the wire.
func (g *gen) emitWriteBareInt(f *ir.Field, name, ind string) {
	w := f.Type.Width
	switch {
	case w == 64 && !f.Type.Signed:
		g.call(ind, fmt.Sprintf("stream.SerializeBits64(ref %s, 64)", name), "")
	case w == 64:
		g.sf("%s{\n%s    ulong rawValue = unchecked((ulong)%s);\n", ind, ind, name)
		g.call(ind+"    ", "stream.SerializeBits64(ref rawValue, 64)", "")
		g.sf("%s}\n", ind)
	case w == 32 && !f.Type.Signed:
		g.call(ind, fmt.Sprintf("stream.SerializeBits(ref %s, 32)", name), "")
	default:
		conv := name // byte and ushort widen implicitly
		if f.Type.Signed {
			conv = fmt.Sprintf("unchecked((%s)%s)", csUint(w), name)
		}
		g.sf("%s{\n%s    uint rawValue = %s;\n", ind, ind, conv)
		g.call(ind+"    ", fmt.Sprintf("stream.SerializeBits(ref rawValue, %d)", w), "")
		g.sf("%s}\n", ind)
	}
}

// fixedTemp is the 16-bit temp an 8-bit fixed value rides through:
// serialize.cs's narrowest SerializeFixed overloads are short/ushort.
func fixedTemp(f *ir.Field) string {
	if f.Type.Signed {
		return "short"
	}
	return "ushort"
}

func (g *gen) emitWriteFixed(f *ir.Field, name, ind string) {
	if f.IntMin.Cmp(f.IntMax) == 0 {
		// a degenerate range: zero bits; the one legal raw is min << F
		raw := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
		msg := name + " is not " + raw.String() + ", the one legal raw of its degenerate fixed range"
		switch {
		case f.Type.Width == 128:
			g.writeAssert(ind, fmt.Sprintf("%s == (%s)(%s)", name, g.storage(f.Type), lit128(raw, f.Type.Signed)), msg)
		case f.Type.Signed:
			g.writeAssert(ind, fmt.Sprintf("(long)%s == %sL", name, raw), msg)
		default:
			g.writeAssert(ind, fmt.Sprintf("(ulong)%s == %sUL", name, raw), msg)
		}
		return
	}
	args := fmt.Sprintf("%d, %d, %sL, %sL", f.Type.IntBits, f.Type.FracBits, f.IntMin, f.IntMax)
	if f.Type.Width == 8 {
		g.sf("%s{\n%s    %s fixedValue = %s;\n", ind, ind, fixedTemp(f), name)
		g.call(ind+"    ", "stream.SerializeFixed(ref fixedValue, "+args+")", "")
		g.sf("%s}\n", ind)
		return
	}
	g.call(ind, fmt.Sprintf("stream.SerializeFixed(ref %s, %s)", name, args), "")
}

func (g *gen) emitReadScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		g.emitReadFixed(f, name, ind)
	case ir.TInt:
		g.emitReadInt(f, name, ind)
	case ir.TBits:
		_, fn := bitsCall(int64(f.Type.Width))
		g.call(ind, fmt.Sprintf("stream.%s(ref %s, %d)", fn, name, f.Type.Width), "")
	case ir.TBool:
		g.call(ind, fmt.Sprintf("stream.SerializeBool(ref %s)", name), "")
	case ir.TFloat32:
		if f.HasFloatRange {
			g.call(ind, g.compressedCall(f, name), "")
			return
		}
		g.call(ind, fmt.Sprintf("stream.SerializeFloat(ref %s)", name), "")
	case ir.TFloat64:
		g.call(ind, fmt.Sprintf("stream.SerializeDouble(ref %s)", name), "")
	case ir.TString, ir.TBytes:
		length := name + "Length"
		g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, 0, %d)", length, f.Type.Size), " // the length guards the slice (§6.3)")
		g.sf("%s{\n%s    Span<byte> bytes = %s.AsSpan().Slice(0, %s);\n", ind, ind, name, length)
		g.call(ind+"    ", "stream.SerializeBytes(bytes)", "")
		if f.Type.Kind == ir.TString {
			g.reject(ind+"    ", "!ValidUtf8Text(bytes)", " // malformed UTF-8 or an interior null is refused (SPEC §4.7)")
		}
		g.sf("%s}\n", ind)
	case ir.TWString:
		length := name + "Length"
		g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, 0, %d)", length, f.Type.Size), "")
		g.sf("%s{\n%s    Span<char> wide = %s.AsSpan();\n%s    bool expectLow = false;\n", ind, ind, name, ind)
		g.sf("%s    for (int i = 0; i < %s; i++)\n%s    {\n", ind, length, ind)
		g.sf("%s        uint wideUnit = 0;\n", ind)
		g.call(ind+"        ", "stream.SerializeBits(ref wideUnit, 32)", "")
		g.reject(ind+"        ", "wideUnit == 0 || wideUnit > 0xFFFF", " // a null or a group past one code unit (SPEC §4.12)")
		g.sf("%s        bool high = wideUnit >= 0xD800 && wideUnit <= 0xDBFF;\n", ind)
		g.sf("%s        bool low = wideUnit >= 0xDC00 && wideUnit <= 0xDFFF;\n", ind)
		g.reject(ind+"        ", "low != expectLow", " // an unpaired surrogate (SPEC §4.12)")
		g.sf("%s        expectLow = high;\n%s        wide[i] = (char)wideUnit;\n%s    }\n", ind, ind, ind)
		g.reject(ind+"    ", "expectLow", " // a high surrogate at the end")
		g.sf("%s}\n", ind)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.sf("%s{\n%s    int enumValue = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt(ref enumValue, 0, %d)", ref.Max), "")
			g.sf("%s    %s = (%s)enumValue;\n%s}\n", ind, name, f.Type.Name, ind)
		case *ir.Flags:
			typ, fn := bitsCall(int64(ref.WireBits))
			g.sf("%s{\n%s    %s flagsValue = 0;\n", ind, ind, typ)
			g.call(ind+"    ", fmt.Sprintf("stream.%s(ref flagsValue, %d)", fn, ref.WireBits), "")
			g.sf("%s    %s = (%s)flagsValue;\n%s}\n", ind, name, f.Type.Name, ind)
		case *ir.Struct, *ir.Union:
			g.call(ind, fmt.Sprintf("Read%s(stream, ref %s)", f.Type.Name, name), "")
		}
	}
}

func (g *gen) emitReadInt(f *ir.Field, name, ind string) {
	t := f.Type
	if t.Width == 128 {
		switch {
		case !f.HasIntRange:
			g.call(ind, fmt.Sprintf("stream.SerializeUInt128(ref %s)", name), "")
		case f.IntMin.Cmp(f.IntMax) == 0:
			g.sf("%s%s = %s; // a degenerate range: zero bits (SPEC §4.6)\n", ind, name, lit128(f.IntMin, true))
		default:
			g.call(ind, fmt.Sprintf("stream.SerializeInt128(ref %s, %s, %s)", name, lit128(f.IntMin, true), lit128(f.IntMax, true)), "")
		}
		return
	}
	if !f.HasIntRange {
		g.emitReadBareInt(f, name, ind)
		return
	}
	storage := g.storage(t)
	if f.IntMin.Cmp(f.IntMax) == 0 {
		g.sf("%s%s = unchecked((%s)%s); // a degenerate range: zero bits (SPEC §4.6)\n", ind, name, storage, f.IntMin)
		return
	}
	switch intPath(f.IntMin, f.IntMax) {
	case "int32":
		if t.Signed && t.Width == 32 {
			g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, %s, %s)", name, f.IntMin, f.IntMax), "")
			return
		}
		g.sf("%s{\n%s    int rangeValue = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt(ref rangeValue, %s, %s)", f.IntMin, f.IntMax), "")
		g.sf("%s    %s = (%s)rangeValue;\n%s}\n", ind, name, storage, ind)
	case "int64":
		if t.Signed && t.Width == 64 {
			g.call(ind, fmt.Sprintf("stream.SerializeInt64(ref %s, %sL, %sL)", name, f.IntMin, f.IntMax), "")
			return
		}
		g.sf("%s{\n%s    long rangeValue = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt64(ref rangeValue, %sL, %sL)", f.IntMin, f.IntMax), "")
		g.sf("%s    %s = (%s)rangeValue;\n%s}\n", ind, name, storage, ind)
	default:
		// raw offset bits; the decode masks to the encoded width, so only a span
		// that leaves headroom in that width needs the explicit refusal
		bits := ir.BitsRequired(f.IntMin, f.IntMax)
		span := new(big.Int).Sub(f.IntMax, f.IntMin)
		g.sf("%s{\n%s    ulong offsetValue = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("stream.SerializeBits64(ref offsetValue, %d)", bits), "")
		if span.Cmp(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))) < 0 {
			g.reject(ind+"    ", fmt.Sprintf("offsetValue > %sUL", span), " // out of range: refused, not latched (SPEC §5)")
		}
		if f.IntMin.Sign() == 0 {
			g.sf("%s    %s = offsetValue;\n%s}\n", ind, name, ind)
		} else {
			g.sf("%s    %s = offsetValue + %sUL;\n%s}\n", ind, name, f.IntMin, ind)
		}
	}
}

func (g *gen) emitReadBareInt(f *ir.Field, name, ind string) {
	w := f.Type.Width
	switch {
	case w == 64 && !f.Type.Signed:
		g.call(ind, fmt.Sprintf("stream.SerializeBits64(ref %s, 64)", name), "")
	case w == 64:
		g.sf("%s{\n%s    ulong rawValue = 0;\n", ind, ind)
		g.call(ind+"    ", "stream.SerializeBits64(ref rawValue, 64)", "")
		g.sf("%s    %s = unchecked((long)rawValue);\n%s}\n", ind, name, ind)
	case w == 32 && !f.Type.Signed:
		g.call(ind, fmt.Sprintf("stream.SerializeBits(ref %s, 32)", name), "")
	default:
		g.sf("%s{\n%s    uint rawValue = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("stream.SerializeBits(ref rawValue, %d)", w), "")
		if f.Type.Signed {
			// back through the same-width unsigned so the sign bit lands right
			g.sf("%s    %s = unchecked((%s)(%s)rawValue);\n%s}\n", ind, name, csInt(w), csUint(w), ind)
		} else {
			g.sf("%s    %s = (%s)rawValue;\n%s}\n", ind, name, csUint(w), ind)
		}
	}
}

func (g *gen) emitReadFixed(f *ir.Field, name, ind string) {
	if f.IntMin.Cmp(f.IntMax) == 0 {
		raw := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
		switch {
		case f.Type.Width == 128:
			g.sf("%s%s = %s; // a degenerate range: zero bits (SPEC §4.6)\n", ind, name, lit128(raw, f.Type.Signed))
		case f.Type.Signed:
			g.sf("%s%s = unchecked((%s)%sL); // a degenerate range: zero bits (SPEC §4.6)\n", ind, name, g.storage(f.Type), raw)
		default:
			g.sf("%s%s = unchecked((%s)%sUL); // a degenerate range: zero bits (SPEC §4.6)\n", ind, name, g.storage(f.Type), raw)
		}
		return
	}
	args := fmt.Sprintf("%d, %d, %sL, %sL", f.Type.IntBits, f.Type.FracBits, f.IntMin, f.IntMax)
	if f.Type.Width == 8 {
		g.sf("%s{\n%s    %s fixedValue = 0;\n", ind, ind, fixedTemp(f))
		g.call(ind+"    ", "stream.SerializeFixed(ref fixedValue, "+args+")", "")
		g.sf("%s    %s = (%s)fixedValue;\n%s}\n", ind, name, g.storage(f.Type), ind)
		return
	}
	g.call(ind, fmt.Sprintf("stream.SerializeFixed(ref %s, %s)", name, args), "")
}

// ---- unions ----

func (g *gen) emitUnionFunctions(d *ir.Union) {
	g.needsSerialize = true
	g.emitBounds(d.Name, ir.MaxBitsUnion(d), "the tag plus the largest arm (SPEC §4.8)")
	g.sf("// Zero%s: the §5 zero form — the empty union.\n", d.Name)
	g.sf("public static void Zero%s(ref %s value)\n{\n    value = default;\n}\n\n", d.Name, d.Name)

	tagBits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max))
	g.sf("public static bool Write%s(WriteStream stream, ref %s value)\n{\n", d.Name, d.Name)
	if d.Max == 0 {
		g.writeAssert("    ", fmt.Sprintf("value.Type == %sType.None", d.Name), "an empty union can only hold None")
		g.sf("    return true; // the tag range [0, 0] costs zero bits (SPEC §4.8)\n}\n\n")
	} else {
		g.sf("    uint tagValue = (uint)value.Type;\n")
		g.writeAssert("    ", fmt.Sprintf("tagValue <= %d", d.Max), fmt.Sprintf("the union tag is outside [0, %d]", d.Max))
		g.call("    ", fmt.Sprintf("stream.SerializeBits(ref tagValue, %d)", tagBits), "")
		g.sf("    switch (value.Type)\n    {\n")
		for _, v := range d.Variants {
			arm := ir.GoExportName(v.Name)
			if v.Void() {
				g.sf("        case %sType.%s:\n            return true; // a payload-free arm: the tag is the wire\n", d.Name, arm)
				continue
			}
			g.sf("        case %sType.%s:\n            return Write%s(stream, ref value.%s);\n", d.Name, arm, v.Type, arm)
		}
		g.sf("        default:\n            return true; // None: the tag is the wire\n    }\n}\n\n")
	}

	g.sf("public static bool Read%s(ReadStream stream, ref %s value)\n{\n", d.Name, d.Name)
	if d.Max == 0 {
		g.sf("    value.Type = %sType.None; // zero wire bits: only None exists (SPEC §4.8)\n    return true;\n}\n\n", d.Name)
		return
	}
	g.sf("    int tagValue = 0;\n")
	g.call("    ", fmt.Sprintf("stream.SerializeInt(ref tagValue, 0, %d)", d.Max), " // a tag above the count is refused (SPEC §4.8)")
	g.sf("    value.Type = (%sType)tagValue;\n", d.Name)
	g.sf("    switch (value.Type)\n    {\n")
	for _, v := range d.Variants {
		arm := ir.GoExportName(v.Name)
		g.sf("        case %sType.%s:\n", d.Name, arm)
		if v.Void() {
			g.sf("            return true;\n")
			continue
		}
		if hasDefaults(v.Ref) {
			g.sf("            Init%s(ref value.%s); // a selection starts from the construction form\n", v.Type, arm)
		} else {
			g.sf("            value.%s = default; // a selection starts from the construction form\n", arm)
		}
		g.sf("            return Read%s(stream, ref value.%s);\n", v.Type, arm)
	}
	g.sf("        default:\n            return true; // None\n    }\n}\n\n")
}
