// Write/Read function emission for types and messages (SPEC §6.1 items 2-4,
// §6.2): straight-line split functions against the serialize.go pointer API —
// sticky stream errors, counts error-checked before every loop and slice (the
// untrusted-data rule), `return stream.Err()` at the end. The wire is
// byte-identical to the C++ target's, construct by construct.
package golang

import (
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/ir"
)

// emitUnionFunctions emits the union's bounds and wire pair (SPEC §4.8): the
// write validates the tag BEFORE it rides (WriteMessage's rule — an
// out-of-set tag writes nothing), the read rejects a tag above the count and
// zero-establishes exactly the selected arm before decoding it.
func (g *gen) emitUnionFunctions(d *ir.Union) {
	g.needsSerialize = true
	maxBits := ir.MaxBitsUnion(d)
	g.pf("// %sMaxBits is the tag plus the largest arm; None costs the tag only (SPEC §4.8).\n", d.Name)
	g.pf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", d.Name)
	g.pf("const %sMaxBits = %d\n", d.Name, maxBits)
	g.pf("const %sMaxBytes = %d\n\n", d.Name, ir.MaxBytes(maxBits))

	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(d.Max))
	g.pf("func Write%s(stream *serialize.WriteStream, value *%s) error {\n", d.Name, d.Name)
	if d.Max == 0 {
		g.pf("\t// an empty union holds only None; its degenerate tag range [0, 0] costs\n")
		g.pf("\t// zero bits (SPEC §4.8)\n")
		g.pf("\tif value.Type != %sTypeNone {\n\t\treturn serialize.ErrValueOutOfRange\n\t}\n", d.Name)
		g.pf("\treturn stream.Err()\n}\n\n")
	} else {
		g.pf("\ttagValue := int32(value.Type)\n")
		g.pf("\tif tagValue < 0 || tagValue > %d { // the tag validates BEFORE it rides (SPEC §4.8)\n", d.Max)
		g.pf("\t\treturn serialize.ErrValueOutOfRange\n\t}\n")
		g.pf("\t{\n\t\toffsetValue := uint32(tagValue)\n\t\tstream.SerializeBits(&offsetValue, %d)\n\t}\n", bits)
		g.pf("\tswitch value.Type {\n")
		for _, v := range d.Variants {
			g.pf("\tcase %sType%s:\n\t\treturn Write%s(stream, &value.%s)\n",
				d.Name, ir.GoExportName(v.Name), v.Type, ir.GoExportName(v.Name))
		}
		g.pf("\t}\n\treturn stream.Err() // None — the tag is the whole wire (SPEC §4.8)\n}\n\n")
	}

	g.pf("func Read%s(stream *serialize.ReadStream, value *%s) error {\n", d.Name, d.Name)
	if d.Max == 0 {
		g.pf("\tvalue.Type = %sTypeNone // zero wire bits — only None exists (SPEC §4.8)\n", d.Name)
		g.pf("\treturn stream.Err()\n}\n\n")
		return
	}
	g.pf("\ttagValue := int32(0)\n")
	g.pf("\tstream.SerializeInt(&tagValue, 0, %d) // rejects a tag above the count (SPEC §4.8)\n", d.Max)
	g.pf("\tif stream.Err() != nil {\n\t\treturn stream.Err()\n\t}\n")
	g.pf("\tvalue.Type = %sType(tagValue)\n", d.Name)
	g.pf("\tswitch value.Type {\n")
	for _, v := range d.Variants {
		g.pf("\tcase %sType%s:\n", d.Name, ir.GoExportName(v.Name))
		g.pf("\t\tvalue.%s = %s{} // the selected arm starts from the zero form (SPEC §5)\n", ir.GoExportName(v.Name), v.Type)
		g.pf("\t\treturn Read%s(stream, &value.%s)\n", v.Type, ir.GoExportName(v.Name))
	}
	g.pf("\t}\n\treturn stream.Err() // None\n}\n\n")
}

// emitStructFunctions emits MaxBits/MaxBytes and the split Write/Read pair
// for a type or message.
func (g *gen) emitStructFunctions(st *ir.Struct) {
	g.needsSerialize = true
	g.bulkBytes = ir.AlignedFixedByteArrays(st)
	maxBits := ir.MaxBitsStruct(st)
	g.pf("// %sMaxBits is the longest wire path; align pads at worst case (SPEC §6.1).\n", st.Name)
	g.pf("// %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", st.Name)
	g.pf("const %sMaxBits = %d\n", st.Name, maxBits)
	g.pf("const %sMaxBytes = %d\n\n", st.Name, ir.MaxBytes(maxBits))

	g.pf("func Write%s(stream *serialize.WriteStream, value *%s) error {\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.pf("\t_ = value // empty body — presence is the payload (SPEC §4.6)\n")
	} else {
		g.emitWriteItems(st.Items, "\t")
	}
	g.pf("\treturn stream.Err()\n}\n\n")

	g.pf("func Read%s(stream *serialize.ReadStream, value *%s) error {\n", st.Name, st.Name)
	if len(st.Items) == 0 {
		g.pf("\t_ = value\n")
	} else {
		g.emitReadItems(st.Items, "\t")
	}
	g.pf("\treturn stream.Err()\n}\n\n")
}

func (g *gen) emitWriteItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitWriteField(item.F, ind)
		case *ir.ConstItem:
			g.emitConstItem(item, ind, true)
		case *ir.ReservedItem:
			g.emitReservedItem(item, ind, true)
		case *ir.AlignItem:
			g.pf("%sstream.SerializeAlign()\n", ind)
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif %svalue.%s {\n", ind, neg, ir.GoExportName(item.Cond))
			g.emitWriteItems(item.Then, ind+"\t")
			if item.Else != nil {
				g.pf("%s} else {\n", ind)
				g.emitWriteItems(item.Else, ind+"\t")
			}
			g.pf("%s}\n", ind)
		}
	}
}

func (g *gen) emitReadItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitReadField(item.F, ind)
		case *ir.ConstItem:
			g.emitConstItem(item, ind, false)
		case *ir.ReservedItem:
			g.emitReservedItem(item, ind, false)
		case *ir.AlignItem:
			g.pf("%sstream.SerializeAlign() // rejects nonzero padding (SPEC §4.3)\n", ind)
		case *ir.Branch:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			g.pf("%sif %svalue.%s {\n", ind, neg, ir.GoExportName(item.Cond))
			g.emitReadItems(item.Then, ind+"\t")
			// the untaken side reads as zero values (SPEC §5)
			g.emitZeroItems(item.Else, ind+"\t")
			g.pf("%s} else {\n", ind)
			if item.Else != nil {
				g.emitReadItems(item.Else, ind+"\t")
			}
			g.emitZeroItems(item.Then, ind+"\t")
			g.pf("%s}\n", ind)
		}
	}
}

// emitConstItem writes const(value, bits) on the wire; a read rejects any
// other value (SPEC §4.3).
func (g *gen) emitConstItem(item *ir.ConstItem, ind string, writing bool) {
	if writing {
		if item.Bits <= 32 {
			g.pf("%s{\n%s\tconstValue := uint32(%s)\n", ind, ind, item.Value.String())
			g.pf("%s\tstream.SerializeBits(&constValue, %d) // const(%s, %d) — SPEC §4.3\n", ind, item.Bits, item.Value.String(), item.Bits)
		} else {
			g.pf("%s{\n%s\tconstValue := uint64(%s)\n", ind, ind, item.Value.String())
			g.pf("%s\tstream.SerializeBits64(&constValue, %d) // const(%s, %d) — SPEC §4.3\n", ind, item.Bits, item.Value.String(), item.Bits)
		}
		g.pf("%s}\n", ind)
		return
	}
	if item.Bits <= 32 {
		g.pf("%s{\n%s\tconstValue := uint32(0)\n", ind, ind)
		g.pf("%s\tstream.SerializeBits(&constValue, %d)\n", ind, item.Bits)
	} else {
		g.pf("%s{\n%s\tconstValue := uint64(0)\n", ind, ind)
		g.pf("%s\tstream.SerializeBits64(&constValue, %d)\n", ind, item.Bits)
	}
	g.pf("%s\tif stream.Err() != nil {\n%s\t\treturn stream.Err()\n%s\t}\n", ind, ind, ind)
	g.pf("%s\tif constValue != %s { // const(%s, %d): a read rejects any other value (SPEC §4.3)\n",
		ind, item.Value.String(), item.Value.String(), item.Bits)
	g.pf("%s\t\treturn ErrValidation\n%s\t}\n%s}\n", ind, ind, ind)
}

// emitReservedItem writes reserved(bits) as zeros; a read rejects nonzero.
func (g *gen) emitReservedItem(item *ir.ReservedItem, ind string, writing bool) {
	if writing {
		if item.Bits <= 32 {
			g.pf("%s{\n%s\treservedValue := uint32(0)\n", ind, ind)
			g.pf("%s\tstream.SerializeBits(&reservedValue, %d) // reserved(%d) — zeros on the wire\n", ind, item.Bits, item.Bits)
		} else {
			g.pf("%s{\n%s\treservedValue := uint64(0)\n", ind, ind)
			g.pf("%s\tstream.SerializeBits64(&reservedValue, %d) // reserved(%d) — zeros on the wire\n", ind, item.Bits, item.Bits)
		}
		g.pf("%s}\n", ind)
		return
	}
	if item.Bits <= 32 {
		g.pf("%s{\n%s\treservedValue := uint32(0)\n", ind, ind)
		g.pf("%s\tstream.SerializeBits(&reservedValue, %d)\n", ind, item.Bits)
	} else {
		g.pf("%s{\n%s\treservedValue := uint64(0)\n", ind, ind)
		g.pf("%s\tstream.SerializeBits64(&reservedValue, %d)\n", ind, item.Bits)
	}
	g.pf("%s\tif stream.Err() != nil {\n%s\t\treturn stream.Err()\n%s\t}\n", ind, ind, ind)
	g.pf("%s\tif reservedValue != 0 { // reserved(%d): a read rejects nonzero (SPEC §4.3)\n", ind, item.Bits)
	g.pf("%s\t\treturn ErrValidation\n%s\t}\n%s}\n", ind, ind, ind)
}

// emitZeroItems zero-initializes every field under an untaken branch side
// (SPEC §5: ZERO values, not specified defaults — Go's zero value is exactly
// that, so plain zero assignments are the memset twin).
func (g *gen) emitZeroItems(items []ir.Item, ind string) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			g.emitZeroField(item.F, ind)
		case *ir.Branch:
			g.emitZeroItems(item.Then, ind)
			g.emitZeroItems(item.Else, ind)
		}
	}
}

func (g *gen) emitZeroField(f *ir.Field, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("%s%s = [%s]byte{}\n%s%sLength = 0\n", ind, name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)), ind, name)
	case f.Array != ir.ArrayNone:
		g.pf("%s%s = [%s]%s{}\n", ind, name, g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)), g.goFieldType(f.Type))
		if f.Array == ir.ArrayCounted {
			g.pf("%s%sCount = 0\n", ind, name)
		}
	default:
		switch f.Type.Kind {
		case ir.TBool:
			g.pf("%s%s = false\n", ind, name)
		case ir.TInt, ir.TFixed:
			if f.Type.Width == 128 {
				// the pair's zero value is zero, but it is a struct — the 0
				// literal does not assign to it
				g.pf("%s%s = %s{}\n", ind, name, g.goFieldType(f.Type))
				return
			}
			g.pf("%s%s = 0\n", ind, name)
		case ir.TNamed:
			switch f.Type.Ref.(type) {
			case *ir.Enum:
				g.pf("%s%s = %sNone\n", ind, name, f.Type.Name)
			case *ir.Flags:
				g.pf("%s%s = 0\n", ind, name)
			case *ir.Struct:
				// the zero value is §5's ZERO form; specified defaults live
				// only in New*, so this is the memset twin exactly
				g.pf("%s%s = %s{}\n", ind, name, f.Type.Name)
			case *ir.Union:
				// zero IS None (sentinel-zero tag), §5's zero form
				g.pf("%s%s = %s{}\n", ind, name, f.Type.Name)
			}
		default:
			g.pf("%s%s = 0\n", ind, name)
		}
	}
}

// rangeArgs renders the min/max arguments symbolically where possible.
func (g *gen) rangeArgs(f *ir.Field) (string, string) {
	return g.renderInt(f.IntMinExpr, f.IntMin), g.renderInt(f.IntMaxExpr, f.IntMax)
}

// ---- compile-time bound emission (the const-emit lever, C++ schema#8) ----
//
// A ranged integer's min/max/bit count are schema constants, so the GENERATOR
// folds them: the write emits the offset from min in a bit count computed at
// generation time — no runtime BitsRequired call, no min/max parameter
// traffic, and the 32/64 split resolves here, not at run time. The wire bytes
// are identical to the runtime SerializeInt/SerializeInt64 forms (same offset,
// same bit count — re-proven by the wire golden gate). This path bypasses the
// runtime's ranged calls, so it supplies their write-side range refusal, the
// discipline the full-range unsigned path below already carries: a misuse
// value must not wrap into valid-looking wire. The offset subtraction runs in
// the signed domain — Go defines signed overflow as wrapping, so the result
// is exactly the unsigned-domain offset for ranges wider than 2^31/2^63.
// Reads stay on the runtime calls: the branchless reader already folds, and
// the C++ pass measured nothing to gain there.

// emitWriteRangedFold32 writes an int in [lo,hi] as offset-from-lo in a
// generation-time bit count (the int32 family: SerializeInt's encoding).
// expr must be an int32-typed expression.
func (g *gen) emitWriteRangedFold32(expr, lo, hi string, bits int64, loZero bool, ind string) {
	g.pf("%sif %s < %s || %s > %s { // the runtime range refusal, folded (SPEC §5)\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n",
		ind, expr, lo, expr, hi, ind, ind)
	// A degenerate range costs ZERO BITS -- the value is known from the range
	// alone, so nothing rides. The bit packer requires at least one bit, so
	// this must not fall through to it.
	if bits == 0 {
		return
	}
	if loZero {
		g.pf("%s{\n%s\toffsetValue := uint32(%s)\n", ind, ind, expr)
	} else {
		g.pf("%s{\n%s\toffsetValue := uint32(%s - (%s))\n", ind, ind, expr, lo)
	}
	g.pf("%s\tstream.SerializeBits(&offsetValue, %d)\n%s}\n", ind, bits, ind)
}

// emitWriteRangedFold64 is the int64 family twin (SerializeInt64's encoding:
// where the count fits 32 bits a single dword, otherwise the low dword first
// then the high remainder — SerializeBits64's own split, byte-identical).
// expr must be an int64-typed expression.
func (g *gen) emitWriteRangedFold64(expr, lo, hi string, bits int64, loZero bool, ind string) {
	g.pf("%sif %s < %s || %s > %s { // the runtime range refusal, folded (SPEC §5)\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n",
		ind, expr, lo, expr, hi, ind, ind)
	offset := "uint64(" + expr + ")"
	if !loZero {
		offset = "uint64(" + expr + " - (" + lo + "))"
	}
	if bits <= 32 {
		g.pf("%s{\n%s\toffsetValue := uint32(%s)\n", ind, ind, offset)
		g.pf("%s\tstream.SerializeBits(&offsetValue, %d)\n%s}\n", ind, bits, ind)
		return
	}
	g.pf("%s{\n%s\toffsetValue := %s\n", ind, ind, offset)
	g.pf("%s\tstream.SerializeBits64(&offsetValue, %d)\n%s}\n", ind, bits, ind)
}

// emitWriteCompressedFold quantizes a ranged float onto its step count in a
// generation-time bit count — serialize_compressed_float's own arithmetic with
// the declaration folded, the way the ranged-integer folds carry their bounds.
// The step count, wire width and delta depend only on (min, max, resolution),
// which are compile-time constants of the call site, so the runtime's per-field
// derivation (a float32 divide, a clamp, a Ceil and a BitsRequired) is paid once
// here instead of on every field of every message.
//
// The float32() around the product is LOAD BEARING, exactly as it is in the
// runtime and in internal/pack: STANDARD.md pins this arithmetic to float32 with
// TWO roundings, and Go permits fusing a multiply into an add unless a
// conversion forces the intermediate rounding. arm64 takes that permission, so a
// fused line writes different bytes for 0.005 over [0, 10] at resolution 0.01.
// Do not "simplify" it.
//
// uint32() truncates toward zero, which is the runtime's Floor: the clamp leaves
// normalizedValue in [0, 1] (its !>= form sends NaN to 0), so the product plus
// 0.5 is always positive, where truncation and floor agree.
func (g *gen) emitWriteCompressedFold(f *ir.Field, name, ind string) {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	g.pf("%s{\n", ind)
	if min32 == 0 {
		g.pf("%s\tnormalizedValue := %s / %s\n", ind, name, f32lit(delta))
	} else {
		g.pf("%s\tnormalizedValue := (%s - (%s)) / %s\n", ind, name, f32lit(min32), f32lit(delta))
	}
	g.pf("%s\tif !(normalizedValue >= 0) { // the runtime's clamp form — it forces NaN into range too\n", ind)
	g.pf("%s\t\tnormalizedValue = 0\n%s\t} else if !(normalizedValue <= 1) {\n%s\t\tnormalizedValue = 1\n%s\t}\n", ind, ind, ind, ind)
	g.pf("%s\tintegerValue := uint32(float32(normalizedValue*%s) + 0.5)\n", ind, f32lit(float32(steps)))
	g.pf("%s\tstream.SerializeBits(&integerValue, %d)\n%s}\n", ind, bits, ind)
}

// emitReadCompressedFold is the read twin: the quantized value at its
// generation-time width, the runtime's headroom refusal folded, then the
// dequantization. The refusal is elided where the step count fills its bit width
// exactly — there is no headroom to smuggle a value into.
//
// The float32() around the product is LOAD BEARING for the reader's own reason:
// fused, integer 384 over [-100, 100] at resolution 0.01 decodes to -96.159996
// where two roundings give -96.160004, so an arm64 reader would disagree with
// every other runtime about what it just read.
func (g *gen) emitReadCompressedFold(f *ir.Field, name, ind string) {
	steps, bits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
	min32 := float32(f.FMin)
	delta := float32(f.FMax) - min32
	g.pf("%s{\n%s\tintegerValue := uint32(0)\n", ind, ind)
	g.pf("%s\tstream.SerializeBits(&integerValue, %d)\n", ind, bits)
	if steps != uint64(1)<<uint(bits)-1 {
		g.pf("%s\tif stream.Err() != nil {\n%s\t\treturn stream.Err()\n%s\t}\n", ind, ind, ind)
		g.pf("%s\tif integerValue > %d { // a value smuggled into the bit headroom is refused (SPEC §4.3)\n", ind, steps)
		g.pf("%s\t\treturn ErrValidation\n%s\t}\n", ind, ind)
	}
	g.pf("%s\tnormalizedValue := float32(integerValue) / %s\n", ind, f32lit(float32(steps)))
	if min32 == 0 {
		g.pf("%s\t%s = float32(normalizedValue * %s)\n%s}\n", ind, name, f32lit(delta), ind)
	} else {
		g.pf("%s\t%s = float32(normalizedValue*%s) + (%s)\n%s}\n", ind, name, f32lit(delta), f32lit(min32), ind)
	}
}

// f32lit prints a float32 as a Go literal that converts back to exactly that
// value — the folded constants are float32 quantities, and the wire depends on
// their exact bits.
func f32lit(v float32) string {
	return formatFloat32(float64(v))
}

// maxUint64 is 2^64 - 1, the top of unsigned-64 storage — the bound against
// which a range guard becomes vacuous.
var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

// intRangePath picks the runtime call family for a ranged integer — the same
// switch as the C++ target, so the two emit identical wire.
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

func (g *gen) emitWriteField(f *ir.Field, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array != ir.ArrayNone {
		bound := g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))
		if g.bulkBytes[f] {
			// statically byte-aligned [N]uint8: the bulk path is
			// byte-identical to the per-byte loop (its internal align is
			// zero bits here) and block copies instead of 8-bit packing
			g.pf("%sstream.SerializeBytes(%s[:]) // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop\n", ind, name)
			return
		}
		if f.Array == ir.ArrayCounted {
			g.emitWriteRangedFold32(name+"Count", big.NewInt(f.ArrayMin).String(), bound,
				ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)), f.ArrayMin == 0, ind)
			g.pf("%sif stream.Err() != nil { // the count guards the loop (§6.3)\n%s\treturn stream.Err()\n%s}\n", ind, ind, ind)
			g.pf("%sfor i := int32(0); i < %sCount; i++ {\n", ind, name)
		} else {
			g.pf("%sfor i := 0; i < %s; i++ {\n", ind, bound)
		}
		g.emitWriteScalar(f, name+"[i]", ind+"\t")
		g.pf("%s}\n", ind)
		return
	}
	g.emitWriteScalar(f, name, ind)
}

func (g *gen) emitWriteScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: ZERO bits — the folded range refusal and no
			// wire call at all, so no runtime degenerate support is needed
			// (SPEC §4.6, decided 2026-08-15). The one legal raw is min << F,
			// compared in the storage's own signedness (a wide ufixed raw can
			// live above int64).
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			switch {
			case f.Type.Width == 128 && f.Type.Signed:
				g.pf("%sif %s != %s { // the runtime range refusal, folded (SPEC §5)\n", ind, name, g.render128(nil, rawMin))
			case f.Type.Width == 128:
				g.pf("%sif %s != %s { // the runtime range refusal, folded (SPEC §5)\n", ind, name, g.renderU128(nil, rawMin))
			case f.Type.Signed:
				g.pf("%sif int64(%s) != %s { // the runtime range refusal, folded (SPEC §5)\n", ind, name, rawMin.String())
			default:
				g.pf("%sif uint64(%s) != %s { // the runtime range refusal, folded (SPEC §5)\n", ind, name, rawMin.String())
			}
			g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
			return
		}
		// the Q format and the whole-unit bounds are compile-time constants of
		// the call site — part of the wire format, exactly like a ranged
		// integer's bounds (STANDARD.md, fixed). The runtime carries the
		// write-side range refusal natively (ErrValueOutOfRange, sticky) and
		// derives the unit capacity from min < 0, so unsigned storage rides
		// the same int64/Int128-shaped calls through bit-exact casts.
		lo, hi := g.rangeArgs(f)
		switch {
		case f.Type.Width == 128 && f.Type.Signed:
			g.pf("%sstream.SerializeFixed128(&%s, %d, %d, %s, %s)\n", ind, name, f.Type.IntBits, f.Type.FracBits, lo, hi)
		case f.Type.Width == 128:
			g.pf("%s{\n%s\tfixedValue := %s.Int128()\n", ind, ind, name)
			g.pf("%s\tstream.SerializeFixed128(&fixedValue, %d, %d, %s, %s)\n%s}\n", ind, f.Type.IntBits, f.Type.FracBits, lo, hi, ind)
		case f.Type.Width == 64 && f.Type.Signed:
			g.pf("%sstream.SerializeFixed64(&%s, %d, %d, %s, %s)\n", ind, name, f.Type.IntBits, f.Type.FracBits, lo, hi)
		default:
			// storage narrower than the library's int64 form, or unsigned:
			// widen through a temp — lossless, the raw value fits I+F bits by
			// construction, and uint64 -> int64 is a bit-exact cast the
			// runtime's unsigned-domain offset math recovers
			g.pf("%s{\n%s\tfixedValue := int64(%s)\n", ind, ind, name)
			g.pf("%s\tstream.SerializeFixed64(&fixedValue, %d, %d, %s, %s)\n%s}\n", ind, f.Type.IntBits, f.Type.FracBits, lo, hi, ind)
		}
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: ZERO bits — refusal only (SPEC §4.6)
					g.pf("%sif %s != %s { // the runtime range refusal, folded (SPEC §5)\n", ind, name, g.render128(f.IntMinExpr, f.IntMin))
					g.pf("%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, ind)
					return
				}
				// int128 is ALWAYS ranged (SPEC §4.3): offset from min —
				// identical bytes to SerializeInt64 wherever the range fits
				g.pf("%sstream.SerializeInt128(&%s, %s, %s)\n", ind, name,
					g.render128(f.IntMinExpr, f.IntMin), g.render128(f.IntMaxExpr, f.IntMax))
			} else {
				// uint128 is the raw field: 128 bits, low 64-bit half first
				g.pf("%sstream.SerializeUint128(&%s)\n", ind, name)
			}
			return
		}
		if f.HasIntRange {
			lo, hi := g.rangeArgs(f)
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				if f.Type.Signed && f.Type.Width == 32 {
					g.emitWriteRangedFold32(name, lo, hi, ir.BitsRequired(f.IntMin, f.IntMax), f.IntMin.Sign() == 0, ind)
					return
				}
				g.pf("%s{\n%s\trangeValue := int32(%s)\n", ind, ind, name)
				g.emitWriteRangedFold32("rangeValue", lo, hi, ir.BitsRequired(f.IntMin, f.IntMax), f.IntMin.Sign() == 0, ind+"\t")
				g.pf("%s}\n", ind)
			case "int64":
				if f.Type.Signed && f.Type.Width == 64 {
					g.emitWriteRangedFold64(name, lo, hi, ir.BitsRequired(f.IntMin, f.IntMax), f.IntMin.Sign() == 0, ind)
					return
				}
				g.pf("%s{\n%s\trangeValue := int64(%s)\n", ind, ind, name)
				g.emitWriteRangedFold64("rangeValue", lo, hi, ir.BitsRequired(f.IntMin, f.IntMax), f.IntMin.Sign() == 0, ind+"\t")
				g.pf("%s}\n", ind)
			default:
				// full-range unsigned: raw offset bits (uint64 storage only —
				// no narrower storage can hold a range past int64). This path
				// bypasses the runtime's ranged calls, so it supplies their
				// write-side range refusal (a misuse value must not wrap into
				// valid-looking wire); vacuous halves are elided
				loVacuous := f.IntMin.Sign() == 0
				hiVacuous := f.IntMax.Cmp(maxUint64) == 0
				switch {
				case !loVacuous && !hiVacuous:
					g.pf("%sif %s < %s || %s > %s {\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, name, lo, name, f.IntMax.String(), ind, ind)
				case !loVacuous:
					g.pf("%sif %s < %s {\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, name, lo, ind, ind)
				case !hiVacuous:
					g.pf("%sif %s > %s {\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, name, f.IntMax.String(), ind, ind)
				}
				if ir.BitsRequired(f.IntMin, f.IntMax) == 0 {
					// degenerate range: zero bits — the refusal above is the
					// whole write (SPEC §4.6, decided 2026-08-15)
					return
				}
				if loVacuous {
					g.pf("%s{\n%s\toffsetValue := %s\n", ind, ind, name)
				} else {
					g.pf("%s{\n%s\toffsetValue := %s - %s\n", ind, ind, name, lo)
				}
				g.pf("%s\tstream.SerializeBits64(&offsetValue, %d)\n%s}\n", ind, ir.BitsRequired(f.IntMin, f.IntMax), ind)
			}
			return
		}
		g.emitWriteBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.pf("%sstream.SerializeBits(&%s, %d)\n", ind, name, f.Type.Width)
		} else {
			g.pf("%sstream.SerializeBits64(&%s, %d)\n", ind, name, f.Type.Width)
		}
	case ir.TBool:
		g.pf("%sstream.SerializeBool(&%s)\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitWriteCompressedFold(f, name, ind)
			return
		}
		g.pf("%sstream.SerializeFloat32(&%s)\n", ind, name)
	case ir.TFloat64:
		g.pf("%sstream.SerializeFloat64(&%s)\n", ind, name)
	case ir.TString, ir.TBytes:
		// length in [0, N], align, then the used bytes — the classic
		// serialize_string framing over a buffer of N + 1 (SPEC §4.7).
		// Interior nulls are writer misuse; the read side rejects them (§4.7).
		g.emitWriteRangedFold32(name+"Length", "0", g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)),
			ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)), true, ind)
		g.pf("%sif stream.Err() != nil { // the length guards the slice (§6.3)\n%s\treturn stream.Err()\n%s}\n", ind, ind, ind)
		g.pf("%sstream.SerializeBytes(%s[:%sLength])\n", ind, name, name)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.pf("%s{\n%s\tenumValue := int32(%s)\n", ind, ind, name)
			g.emitWriteRangedFold32("enumValue", "0", big.NewInt(ref.Max).String(),
				ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)), true, ind+"\t")
			g.pf("%s}\n", ind)
		case *ir.Flags:
			g.emitWriteFlagsValue(name, ref.WireBits, ind)
		case *ir.Struct:
			g.pf("%sif err := Write%s(stream, &%s); err != nil {\n%s\treturn err\n%s}\n", ind, f.Type.Name, name, ind, ind)
		case *ir.Union:
			g.pf("%sif err := Write%s(stream, &%s); err != nil {\n%s\treturn err\n%s}\n", ind, f.Type.Name, name, ind, ind)
		}
	}
}

// emitWriteBareInt writes a bare integer at its storage width. Signed values
// cast through the same-width unsigned first — the sign extension into a
// wider temp would corrupt neighboring wire data, exactly as in C++.
func (g *gen) emitWriteBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.pf("%s{\n%s\trawValue := uint64(%s)\n", ind, ind, name)
			g.pf("%s\tstream.SerializeBits64(&rawValue, 64)\n%s}\n", ind, ind)
			return
		}
		g.pf("%sstream.SerializeBits64(&%s, 64)\n", ind, name)
		return
	}
	if !f.Type.Signed && f.Type.Width == 32 {
		g.pf("%sstream.SerializeBits(&%s, 32)\n", ind, name)
		return
	}
	cast := fmt32Cast(f, name)
	g.pf("%s{\n%s\trawValue := %s\n", ind, ind, cast)
	g.pf("%s\tstream.SerializeBits(&rawValue, %d)\n%s}\n", ind, f.Type.Width, ind)
}

// fmt32Cast renders the value-to-uint32 conversion for a sub-32 (or signed
// 32-bit) bare integer: signed narrows go through the same-width unsigned.
func fmt32Cast(f *ir.Field, name string) string {
	if f.Type.Signed && f.Type.Width < 32 {
		return "uint32(uint" + itoa(f.Type.Width) + "(" + name + "))"
	}
	return "uint32(" + name + ")"
}

func itoa(v int) string {
	return map[int]string{8: "8", 16: "16", 32: "32", 64: "64"}[v]
}

func (g *gen) emitReadField(f *ir.Field, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	if f.Array != ir.ArrayNone {
		bound := g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound))
		if g.bulkBytes[f] {
			g.pf("%sstream.SerializeBytes(%s[:]) // byte-aligned [N]uint8 — bulk copy, wire-identical to the per-byte loop\n", ind, name)
			return
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%sstream.SerializeInt(&%sCount, %d, %s)\n", ind, name, f.ArrayMin, bound)
			g.pf("%sif stream.Err() != nil { // the count guards the loop (§6.3)\n%s\treturn stream.Err()\n%s}\n", ind, ind, ind)
			g.pf("%sfor i := int32(0); i < %sCount; i++ {\n", ind, name)
		} else {
			g.pf("%sfor i := 0; i < %s; i++ {\n", ind, bound)
		}
		g.emitReadScalar(f, name+"[i]", ind+"\t")
		g.pf("%s}\n", ind)
		return
	}
	g.emitReadScalar(f, name, ind)
}

func (g *gen) emitReadScalar(f *ir.Field, name, ind string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if f.IntMin.Cmp(f.IntMax) == 0 {
			// degenerate range: zero bits — the value is the range, raw
			// min << F, materialized with no wire call (SPEC §4.6), in the
			// storage's own signedness
			rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
			switch {
			case f.Type.Width == 128 && f.Type.Signed:
				g.pf("%s%s = %s\n", ind, name, g.render128(nil, rawMin))
			case f.Type.Width == 128:
				g.pf("%s%s = %s\n", ind, name, g.renderU128(nil, rawMin))
			case f.Type.Signed:
				g.pf("%s%s = %s(%s)\n", ind, name, goInt(f.Type.Width), rawMin.String())
			default:
				g.pf("%s%s = %s(%s)\n", ind, name, goUint(f.Type.Width), rawMin.String())
			}
			return
		}
		// the runtime validates the raw offset against the raw bounds and
		// rejects — never clamps — surfacing ErrValueOutOfRange on a hostile
		// stream; unsigned storage narrows back through a bit-exact cast
		lo, hi := g.rangeArgs(f)
		switch {
		case f.Type.Width == 128 && f.Type.Signed:
			g.pf("%sstream.SerializeFixed128(&%s, %d, %d, %s, %s)\n", ind, name, f.Type.IntBits, f.Type.FracBits, lo, hi)
		case f.Type.Width == 128:
			g.pf("%s{\n%s\tfixedValue := serialize.Int128{}\n", ind, ind)
			g.pf("%s\tstream.SerializeFixed128(&fixedValue, %d, %d, %s, %s)\n", ind, f.Type.IntBits, f.Type.FracBits, lo, hi)
			g.pf("%s\t%s = fixedValue.Uint128()\n%s}\n", ind, name, ind)
		case f.Type.Width == 64 && f.Type.Signed:
			g.pf("%sstream.SerializeFixed64(&%s, %d, %d, %s, %s)\n", ind, name, f.Type.IntBits, f.Type.FracBits, lo, hi)
		default:
			// narrow (or re-sign) on the member assignment — lossless, a
			// decoded raw value is inside the raw bounds or the read already
			// failed
			g.pf("%s{\n%s\tfixedValue := int64(0)\n", ind, ind)
			g.pf("%s\tstream.SerializeFixed64(&fixedValue, %d, %d, %s, %s)\n", ind, f.Type.IntBits, f.Type.FracBits, lo, hi)
			g.pf("%s\t%s = %s(fixedValue)\n%s}\n", ind, name, goInt2(f.Type.Signed, f.Type.Width), ind)
		}
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.HasIntRange {
				if f.IntMin.Cmp(f.IntMax) == 0 {
					// degenerate range: zero bits — materialize (SPEC §4.6)
					g.pf("%s%s = %s\n", ind, name, g.render128(f.IntMinExpr, f.IntMin))
					return
				}
				// rejects a decoded offset beyond max - min (reject, never clamp)
				g.pf("%sstream.SerializeInt128(&%s, %s, %s)\n", ind, name,
					g.render128(f.IntMinExpr, f.IntMin), g.render128(f.IntMaxExpr, f.IntMax))
			} else {
				g.pf("%sstream.SerializeUint128(&%s)\n", ind, name)
			}
			return
		}
		if f.HasIntRange {
			if f.IntMin.Cmp(f.IntMax) == 0 {
				// degenerate range: zero bits — the value is the range,
				// materialized with no wire call (SPEC §4.6)
				g.pf("%s%s = %s(%s)\n", ind, name, goInt2(f.Type.Signed, f.Type.Width), g.renderInt(f.IntMinExpr, f.IntMin))
				return
			}
			lo, hi := g.rangeArgs(f)
			switch intRangePath(f.IntMin, f.IntMax) {
			case "int32":
				if f.Type.Signed && f.Type.Width == 32 {
					g.pf("%sstream.SerializeInt(&%s, %s, %s)\n", ind, name, lo, hi)
					return
				}
				g.pf("%s{\n%s\trangeValue := int32(0)\n", ind, ind)
				g.pf("%s\tstream.SerializeInt(&rangeValue, %s, %s)\n", ind, lo, hi)
				g.pf("%s\t%s = %s(rangeValue)\n%s}\n", ind, name, goInt2(f.Type.Signed, f.Type.Width), ind)
			case "int64":
				if f.Type.Signed && f.Type.Width == 64 {
					g.pf("%sstream.SerializeInt64(&%s, %s, %s)\n", ind, name, lo, hi)
					return
				}
				g.pf("%s{\n%s\trangeValue := int64(0)\n", ind, ind)
				g.pf("%s\tstream.SerializeInt64(&rangeValue, %s, %s)\n", ind, lo, hi)
				g.pf("%s\t%s = %s(rangeValue)\n%s}\n", ind, name, goInt2(f.Type.Signed, f.Type.Width), ind)
			default:
				diff := new(big.Int).Sub(f.IntMax, f.IntMin)
				g.pf("%s{\n%s\toffsetValue := uint64(0)\n", ind, ind)
				g.pf("%s\tstream.SerializeBits64(&offsetValue, %d)\n", ind, ir.BitsRequired(f.IntMin, f.IntMax))
				if diff.Cmp(maxUint64) != 0 {
					// a full-width diff cannot overflow its own read — elided
					g.pf("%s\tif stream.Err() != nil {\n%s\t\treturn stream.Err()\n%s\t}\n", ind, ind, ind)
					g.pf("%s\tif offsetValue > %s { // a read rejects out-of-range (SPEC §5)\n", ind, diff.String())
					g.pf("%s\t\treturn ErrValidation\n%s\t}\n", ind, ind)
				}
				if f.IntMin.Sign() == 0 {
					g.pf("%s\t%s = offsetValue\n%s}\n", ind, name, ind)
				} else {
					g.pf("%s\t%s = offsetValue + %s\n%s}\n", ind, name, lo, ind)
				}
			}
			return
		}
		g.emitReadBareInt(f, name, ind)
	case ir.TBits:
		if f.Type.Width <= 32 {
			g.pf("%sstream.SerializeBits(&%s, %d)\n", ind, name, f.Type.Width)
		} else {
			g.pf("%sstream.SerializeBits64(&%s, %d)\n", ind, name, f.Type.Width)
		}
	case ir.TBool:
		g.pf("%sstream.SerializeBool(&%s)\n", ind, name)
	case ir.TFloat32:
		if f.HasFloatRange {
			g.emitReadCompressedFold(f, name, ind)
			return
		}
		g.pf("%sstream.SerializeFloat32(&%s)\n", ind, name)
	case ir.TFloat64:
		g.pf("%sstream.SerializeFloat64(&%s)\n", ind, name)
	case ir.TString, ir.TBytes:
		g.pf("%sstream.SerializeInt(&%sLength, 0, %s)\n", ind, name, g.renderInt(f.Type.SizeExpr, big.NewInt(f.Type.Size)))
		g.pf("%sif stream.Err() != nil { // the length guards the slice (§6.3)\n%s\treturn stream.Err()\n%s}\n", ind, ind, ind)
		g.pf("%sstream.SerializeBytes(%s[:%sLength])\n", ind, name, name)
		if f.Type.Kind == ir.TString {
			// the interior-null rule is generated-code validation (SPEC §4.7);
			// a truncated stream must surface as the stream's own error, not
			// as a content verdict over bytes that never arrived
			g.pf("%sif stream.Err() != nil {\n%s\treturn stream.Err()\n%s}\n", ind, ind, ind)
			g.pf("%sfor i := int32(0); i < %sLength; i++ {\n", ind, name)
			g.pf("%s\tif %s[i] == 0 {\n%s\t\treturn ErrValidation\n%s\t}\n%s}\n", ind, name, ind, ind, ind)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			g.pf("%s{\n%s\tenumValue := int32(0)\n", ind, ind)
			g.pf("%s\tstream.SerializeInt(&enumValue, 0, %d)\n", ind, ref.Max)
			g.pf("%s\t%s = %s(enumValue)\n%s}\n", ind, name, f.Type.Name, ind)
		case *ir.Flags:
			g.emitReadFlags(name, f.Type.Name, ref.WireBits, ind)
		case *ir.Struct:
			g.pf("%sif err := Read%s(stream, &%s); err != nil {\n%s\treturn err\n%s}\n", ind, f.Type.Name, name, ind, ind)
		case *ir.Union:
			g.pf("%sif err := Read%s(stream, &%s); err != nil {\n%s\treturn err\n%s}\n", ind, f.Type.Name, name, ind, ind)
		}
	}
}

func (g *gen) emitReadBareInt(f *ir.Field, name, ind string) {
	if f.Type.Width == 64 {
		if f.Type.Signed {
			g.pf("%s{\n%s\trawValue := uint64(0)\n", ind, ind)
			g.pf("%s\tstream.SerializeBits64(&rawValue, 64)\n", ind)
			g.pf("%s\t%s = int64(rawValue)\n%s}\n", ind, name, ind)
			return
		}
		g.pf("%sstream.SerializeBits64(&%s, 64)\n", ind, name)
		return
	}
	if !f.Type.Signed && f.Type.Width == 32 {
		g.pf("%sstream.SerializeBits(&%s, 32)\n", ind, name)
		return
	}
	g.pf("%s{\n%s\trawValue := uint32(0)\n", ind, ind)
	g.pf("%s\tstream.SerializeBits(&rawValue, %d)\n", ind, f.Type.Width)
	if f.Type.Signed && f.Type.Width < 32 {
		// back through the same-width unsigned so the sign bit lands right
		g.pf("%s\t%s = int%d(uint%d(rawValue))\n%s}\n", ind, name, f.Type.Width, f.Type.Width, ind)
		return
	}
	g.pf("%s\t%s = %s(rawValue)\n%s}\n", ind, name, goInt2(f.Type.Signed, f.Type.Width), ind)
}

// emitReadFlags reads a flags value through an unsigned temp and casts to the
// named type.
func (g *gen) emitReadFlags(name, typeName string, wireBits int, ind string) {
	if wireBits <= 32 {
		g.pf("%s{\n%s\tflagsValue := uint32(0)\n", ind, ind)
		g.pf("%s\tstream.SerializeBits(&flagsValue, %d)\n", ind, wireBits)
		g.pf("%s\t%s = %s(flagsValue)\n%s}\n", ind, name, typeName, ind)
		return
	}
	g.pf("%s{\n%s\tflagsValue := uint64(0)\n", ind, ind)
	g.pf("%s\tstream.SerializeBits64(&flagsValue, %d)\n", ind, wireBits)
	g.pf("%s\t%s = %s(flagsValue)\n%s}\n", ind, name, typeName, ind)
}

// emitWriteFlagsValue is the write half used by emitWriteScalar. Storage is
// wider than the wire wherever WireBits < 64, so a mask bit above the wire
// width is refused rather than silently truncated.
func (g *gen) emitWriteFlagsValue(name string, wireBits int, ind string) {
	if wireBits < 64 {
		g.pf("%sif %s >= 1<<%d { // a mask bit above the wire width cannot ride\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n",
			ind, name, wireBits, ind, ind)
	}
	if wireBits <= 32 {
		g.pf("%s{\n%s\tflagsValue := uint32(%s)\n", ind, ind, name)
		g.pf("%s\tstream.SerializeBits(&flagsValue, %d)\n%s}\n", ind, wireBits, ind)
		return
	}
	g.pf("%s{\n%s\tflagsValue := uint64(%s)\n", ind, ind, name)
	g.pf("%s\tstream.SerializeBits64(&flagsValue, %d)\n%s}\n", ind, wireBits, ind)
}

// emitMessageTagFunctions emits the tag wire pair, the message-level bound,
// and the Go dispatch surface: an interface over the message set with a type
// switch — the language's own idiom (SPEC §6.1 item 6). nil is None, the
// stream terminator.
func (g *gen) emitMessageTagFunctions() {
	g.needsSerialize = true
	count := int64(len(g.unit.Messages))
	g.pf("// The message tag wire: MessageType in [0, %d], minimal bits; None = 0 is a\n", count)
	g.pf("// valid wire value meaning *no message* — the stream terminator (SPEC §4.8).\n")
	g.pf("func WriteMessageType(stream *serialize.WriteStream, value MessageType) error {\n")
	g.pf("\ttagValue := int32(value)\n")
	g.emitWriteRangedFold32("tagValue", "0", big.NewInt(count).String(),
		ir.BitsRequired(big.NewInt(0), big.NewInt(count)), true, "\t")
	g.pf("\treturn stream.Err()\n}\n\n")
	g.pf("func ReadMessageType(stream *serialize.ReadStream, value *MessageType) error {\n")
	g.pf("\ttagValue := int32(0)\n")
	g.pf("\tstream.SerializeInt(&tagValue, 0, %d)\n", count)
	g.pf("\t*value = MessageType(tagValue)\n")
	g.pf("\treturn stream.Err()\n}\n\n")

	tagBits := ir.BitsRequired(big.NewInt(0), big.NewInt(count))
	largest := int64(0)
	for _, m := range g.unit.Messages {
		if b := ir.MaxBitsStruct(g.unit.Structs[m]); b > largest {
			largest = b
		}
	}
	g.pf("// The message-level bound: the tag plus the largest message (SPEC §6.1);\n")
	g.pf("// MessageMaxBytes is rounded up to the 8-byte write-buffer granularity.\n")
	g.pf("const MessageMaxBits = %d\n", tagBits+largest)
	g.pf("const MessageMaxBytes = %d\n\n", ir.MaxBytes(tagBits+largest))

	msgs := g.unit.Messages
	g.pf("// Message is the dispatch surface: the interface every message satisfies.\n")
	g.pf("// nil is None — the stream terminator (SPEC §4.8). The concrete types are\n")
	g.pf("// the message structs themselves; dispatch is a type switch, the Go idiom.\n")
	g.pf("type Message interface {\n\tMessageType() MessageType\n}\n\n")
	for _, m := range msgs {
		g.pf("func (*%s) MessageType() MessageType { return MessageType%s }\n", m, m)
	}
	g.pf("\n")

	g.pf("// MessageStorage is the pre-allocated home a read message lands in — the Go\n")
	g.pf("// stand-in for the C++ tagged union (SPEC §6.1: storage never heap-allocates\n")
	g.pf("// per message; fixed buffers on sender and receiver). Reuse it across reads:\n")
	g.pf("// the Message a read returns points into it and stays valid until the next\n")
	g.pf("// read against the same storage — the union's own discipline, exactly.\n")
	g.pf("type MessageStorage struct {\n")
	for _, m := range msgs {
		g.pf("\t%s %s\n", m, m)
	}
	g.pf("}\n\n")

	// dispatch validates BEFORE the tag rides the wire: a Message
	// implementation from outside the generated set writes nothing (a tag
	// with no payload would desynchronize the stream), and the tag framing
	// is the tag pair's — one source
	g.pf("func WriteMessage(stream *serialize.WriteStream, message Message) error {\n")
	g.pf("\tswitch m := message.(type) {\n")
	g.pf("\tcase nil:\n\t\treturn WriteMessageType(stream, MessageTypeNone) // the stream terminator (SPEC §4.8)\n")
	for _, m := range msgs {
		g.pf("\tcase *%s:\n", m)
		g.pf("\t\tif m == nil { // a typed nil is not a writable message; nothing rides\n\t\t\treturn ErrValidation\n\t\t}\n")
		g.pf("\t\tif err := WriteMessageType(stream, MessageType%s); err != nil {\n\t\t\treturn err\n\t\t}\n", m)
		g.pf("\t\treturn Write%s(stream, m)\n", m)
	}
	g.pf("\t}\n\treturn ErrValidation // not a generated message type; nothing was written\n}\n\n")

	g.pf("func ReadMessage(stream *serialize.ReadStream, storage *MessageStorage) (Message, error) {\n")
	g.pf("\ttagValue := MessageTypeNone\n")
	g.pf("\tif err := ReadMessageType(stream, &tagValue); err != nil {\n\t\treturn nil, err\n\t}\n")
	g.pf("\tswitch tagValue {\n")
	g.pf("\tcase MessageTypeNone:\n\t\treturn nil, nil // the stream terminator (SPEC §4.8)\n")
	for _, m := range msgs {
		g.pf("\tcase MessageType%s:\n", m)
		g.pf("\t\tstorage.%s = %s{} // reused storage starts from the zero form, as the union does\n", m, m)
		g.pf("\t\tif err := Read%s(stream, &storage.%s); err != nil {\n\t\t\treturn nil, err\n\t\t}\n", m, m)
		g.pf("\t\treturn &storage.%s, nil\n", m)
	}
	g.pf("\t}\n\treturn nil, ErrValidation\n}\n\n")
}
