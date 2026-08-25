// Object-view function emission (SPEC §4.8, §6.1 item 7): the Deep and
// Shallow wire structs' split Write/Read pairs, the Quantize/Unquantize
// mapping between Interpolate and Shallow (floor(c*K + 0.5) per component in
// double math, straight copies for discrete and projected fields), and the
// ObjectType tag pair. State structs are simulation-only and get no wire
// functions. The wire is byte-identical to the C++ target's.
package golang

import (
	"math/big"

	"github.com/mas-bandwidth/schema/ir"
)

func (g *gen) emitObjectFunctions(d *ir.Object) {
	g.needsSerialize = true

	deep, interp := splitObjectFields(d)

	deepName := d.Name + "Data_Deep"
	deepBits := ir.MaxBitsView(deep, ir.ViewDeep)
	g.pf("const %sMaxBits = %d\n", deepName, deepBits)
	g.pf("const %sMaxBytes = %d // rounded up to the 8-byte write-buffer granularity\n\n", deepName, ir.MaxBytes(deepBits))

	g.pf("func Write%s(stream *serialize.WriteStream, value *%s) error {\n", deepName, deepName)
	for _, f := range deep {
		g.emitViewWriteField(f, ir.ViewDeep, "\t")
	}
	g.pf("\treturn stream.Err()\n}\n\n")
	g.pf("func Read%s(stream *serialize.ReadStream, value *%s) error {\n", deepName, deepName)
	for _, f := range deep {
		g.emitViewReadField(f, ir.ViewDeep, "\t")
	}
	g.pf("\treturn stream.Err()\n}\n\n")

	shName := d.Name + "Data_Shallow"
	shBits := ir.MaxBitsView(interp, ir.ViewShallow)
	g.pf("const %sMaxBits = %d\n", shName, shBits)
	g.pf("const %sMaxBytes = %d // rounded up to the 8-byte write-buffer granularity\n\n", shName, ir.MaxBytes(shBits))

	g.pf("func Write%s(stream *serialize.WriteStream, value *%s) error {\n", shName, shName)
	for _, f := range interp {
		g.emitViewWriteField(f, ir.ViewShallow, "\t")
	}
	g.pf("\treturn stream.Err()\n}\n\n")
	g.pf("func Read%s(stream *serialize.ReadStream, value *%s) error {\n", shName, shName)
	for _, f := range interp {
		g.emitViewReadField(f, ir.ViewShallow, "\t")
	}
	g.pf("\treturn stream.Err()\n}\n\n")

	// ---- Quantize / Unquantize: the Interpolate <-> Shallow mapping pair
	// (SPEC §4.8's artifact table — the hand-written Quantize(), generated).
	// NOT emitted when every | interpolate field is already wire-domain —
	// fixed components are their own quantization (SPEC §4.8).
	if ir.ObjectNeedsQuantize(d) {
		inName := d.Name + "Data_Interpolate"
		g.pf("func Quantize%s(input *%s, output *%s) {\n", d.Name, inName, shName)
		for _, f := range interp {
			g.emitQuantizeField(f, "\t")
		}
		g.pf("}\n\n")
		g.pf("func Unquantize%s(input *%s, output *%s) {\n", d.Name, shName, inName)
		for _, f := range interp {
			g.emitUnquantizeField(f, "\t")
		}
		g.pf("}\n\n")
	}
}

// emitViewWriteField emits one field of a view wire function.
func (g *gen) emitViewWriteField(f *ir.Field, v ir.View, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	switch {
	case v == ir.ViewShallow && f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			compName := name + ir.GoExportName(comp.Name)
			lo, hi, bits, wide, _, width := fixedShallowComp(f, comp)
			switch {
			case wide:
				g.pf("%s{\n%s\tcomponentValue := int64(%s)\n", ind, ind, compName)
				g.emitWriteRangedFold64("componentValue", lo.String(), hi.String(), bits, false, ind+"\t")
				g.pf("%s}\n", ind)
			case width == 32:
				g.emitWriteRangedFold32(compName, lo.String(), hi.String(), bits, false, ind)
			default:
				g.pf("%s{\n%s\tcomponentValue := int32(%s)\n", ind, ind, compName)
				g.emitWriteRangedFold32("componentValue", lo.String(), hi.String(), bits, false, ind+"\t")
				g.pf("%s}\n", ind)
			}
		}
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		wide := f.QuantBound > 2147483647 // the int32 family truncates past this
		quantBits := ir.BitsRequired(big.NewInt(-f.QuantBound), big.NewInt(f.QuantBound))
		lo := big.NewInt(-f.QuantBound).String()
		hi := big.NewInt(f.QuantBound).String()
		for _, comp := range st.Fields {
			compName := name + ir.GoExportName(comp.Name)
			switch {
			case wide:
				g.pf("%s{\n%s\tcomponentValue := int64(%s)\n", ind, ind, compName)
				g.emitWriteRangedFold64("componentValue", lo, hi, quantBits, false, ind+"\t")
				g.pf("%s}\n", ind)
			case smallestSigned(f.QuantBound) == 32:
				g.emitWriteRangedFold32(compName, lo, hi, quantBits, false, ind)
			default:
				g.pf("%s{\n%s\tcomponentValue := int32(%s)\n", ind, ind, compName)
				g.emitWriteRangedFold32("componentValue", lo, hi, quantBits, false, ind+"\t")
				g.pf("%s}\n", ind)
			}
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		stepBits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Steps))
		if f.Steps > 2147483647 {
			g.pf("%s{\n%s\tprojectedValue := int64(%s)\n", ind, ind, name)
			g.emitWriteRangedFold64("projectedValue", "0", big.NewInt(f.Steps).String(), stepBits, true, ind+"\t")
			g.pf("%s}\n", ind)
		} else {
			g.pf("%s{\n%s\tprojectedValue := int32(%s)\n", ind, ind, name)
			g.emitWriteRangedFold32("projectedValue", "0", big.NewInt(f.Steps).String(), stepBits, true, ind+"\t")
			g.pf("%s}\n", ind)
		}
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		// the triple describes the shallow wire only — deep is the bare float
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%sstream.SerializeFloat64(&%s)\n", ind, name)
		} else {
			g.pf("%sstream.SerializeFloat32(&%s)\n", ind, name)
		}
	default:
		g.emitWriteField(f, ind)
	}
}

func (g *gen) emitViewReadField(f *ir.Field, v ir.View, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	switch {
	case v == ir.ViewShallow && f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			compName := name + ir.GoExportName(comp.Name)
			lo, hi, bits, wide, compT, width := fixedShallowComp(f, comp)
			if bits == 0 {
				// a degenerate component narrows to zero bits — the value is
				// the range (SPEC §4.6)
				g.pf("%s%s = %s(%s)\n", ind, compName, compT, lo.String())
				continue
			}
			switch {
			case wide:
				g.pf("%s{\n%s\tcomponentValue := int64(0)\n", ind, ind)
				g.pf("%s\tstream.SerializeInt64(&componentValue, %s, %s)\n", ind, lo, hi)
				g.pf("%s\t%s = %s(componentValue)\n%s}\n", ind, compName, compT, ind)
			case width == 32:
				g.pf("%sstream.SerializeInt(&%s, %s, %s)\n", ind, compName, lo, hi)
			default:
				g.pf("%s{\n%s\tcomponentValue := int32(0)\n", ind, ind)
				g.pf("%s\tstream.SerializeInt(&componentValue, %s, %s)\n", ind, lo, hi)
				g.pf("%s\t%s = %s(componentValue)\n%s}\n", ind, compName, compT, ind)
			}
		}
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		compT := goInt(smallestSigned(f.QuantBound))
		wide := f.QuantBound > 2147483647
		for _, comp := range st.Fields {
			compName := name + ir.GoExportName(comp.Name)
			switch {
			case wide:
				g.pf("%s{\n%s\tcomponentValue := int64(0)\n", ind, ind)
				g.pf("%s\tstream.SerializeInt64(&componentValue, -%d, %d)\n", ind, f.QuantBound, f.QuantBound)
				g.pf("%s\t%s = %s(componentValue)\n%s}\n", ind, compName, compT, ind)
			case smallestSigned(f.QuantBound) == 32:
				g.pf("%sstream.SerializeInt(&%s, -%d, %d)\n", ind, compName, f.QuantBound, f.QuantBound)
			default:
				g.pf("%s{\n%s\tcomponentValue := int32(0)\n", ind, ind)
				g.pf("%s\tstream.SerializeInt(&componentValue, -%d, %d)\n", ind, f.QuantBound, f.QuantBound)
				g.pf("%s\t%s = %s(componentValue)\n%s}\n", ind, compName, compT, ind)
			}
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		storT := goUint(ir.StorageBitsFor(f.Steps))
		if f.Steps > 2147483647 {
			g.pf("%s{\n%s\tprojectedValue := int64(0)\n", ind, ind)
			g.pf("%s\tstream.SerializeInt64(&projectedValue, 0, %d)\n", ind, f.Steps)
			g.pf("%s\t%s = %s(projectedValue)\n%s}\n", ind, name, storT, ind)
		} else {
			g.pf("%s{\n%s\tprojectedValue := int32(0)\n", ind, ind)
			g.pf("%s\tstream.SerializeInt(&projectedValue, 0, %d)\n", ind, f.Steps)
			g.pf("%s\t%s = %s(projectedValue)\n%s}\n", ind, name, storT, ind)
		}
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%sstream.SerializeFloat64(&%s)\n", ind, name)
		} else {
			g.pf("%sstream.SerializeFloat32(&%s)\n", ind, name)
		}
	default:
		g.emitReadField(f, ind)
	}
}

// emitQuantizeField maps one Interpolate field into its Shallow twin:
// composites quantize per component — floor(c * K + 0.5), clamped to the wire
// range (SPEC §4.8 rule 2); projected fields are already wire-domain ints
// (rule 5) and copy; discrete fields copy.
func (g *gen) emitQuantizeField(f *ir.Field, ind string) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			drop := comp.Type.FracBits - f.QuantShift
			_, _, _, _, compT, _ := fixedShallowComp(f, comp)
			if drop == 0 {
				g.pf("%soutput.%s%s = %s(input.%s.%s)\n", ind, name, compName, compT, name, compName)
				continue
			}
			// round-to-nearest narrowing shift — arithmetic on int64, ties
			// AWAY FROM ZERO: the one fixed-point rounding rule (SPEC §4.8). Negative raws mirror through negation so the tie
			// leaves zero in both signs. In-bounds raws cannot overflow the
			// add or the negation (checker-enforced bounds leave 2^(F-1) of
			// headroom past any legal raw)
			half := int64(1) << (drop - 1)
			g.pf("%s{\n%s\traw := int64(input.%s.%s)\n", ind, ind, name, compName)
			g.pf("%s\tif raw >= 0 {\n%s\t\toutput.%s%s = %s((raw + %d) >> %d)\n", ind, ind, name, compName, compT, half, drop)
			g.pf("%s\t} else {\n%s\t\toutput.%s%s = %s(-((-raw + %d) >> %d))\n%s\t}\n%s}\n",
				ind, ind, name, compName, compT, half, drop, ind, ind)
		}
	case f.HasQuantize:
		g.needsMath = true
		st := f.Type.Ref.(*ir.Struct)
		compT := goInt(smallestSigned(f.QuantBound))
		scale := g.renderInt(f.QuantScaleExpr, big.NewInt(f.QuantScale))
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			// double math regardless of component width — the referent's own
			// arithmetic, and a float product would pre-round before the + 0.5.
			// The clamp happens in the FLOAT64 domain before the int conversion:
			// float->int of out-of-range or NaN input is target- and
			// arch-dependent, so this shape quantizes even garbage input
			// identically in every target (NaN clamps low)
			// the product lands in a local and re-enters through an EXPLICIT
			// float64 conversion — the Go spec permits fusing ACROSS
			// statements, and only an explicit conversion forces the rounding
			// that bars the FMA (the compressed_float hazard one level up,
			// the same remedy serialize.go's writer uses)
			g.pf("%s{\n%s\tscaledValue := float64(input.%s.%s) * float64(%s)\n",
				ind, ind, name, compName, scale)
			g.pf("%s\tquantizedValue := math.Floor(float64(scaledValue) + 0.5)\n", ind)
			g.pf("%s\tcomponentValue := int64(-%d)\n", ind, f.QuantBound)
			g.pf("%s\tif quantizedValue > %d.0 {\n%s\t\tcomponentValue = %d\n", ind, f.QuantBound, ind, f.QuantBound)
			g.pf("%s\t} else if quantizedValue >= -%d.0 {\n%s\t\tcomponentValue = int64(quantizedValue)\n%s\t}\n", ind, f.QuantBound, ind, ind)
			g.pf("%s\toutput.%s%s = %s(componentValue)\n%s}\n", ind, name, compName, compT, ind)
		}
	default:
		g.pf("%soutput.%s = input.%s\n", ind, name, name)
	}
}

// emitUnquantizeField maps one Shallow field back into Interpolate:
// composites divide by the scale; everything else copies.
func (g *gen) emitUnquantizeField(f *ir.Field, ind string) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			drop := comp.Type.FracBits - f.QuantShift
			storT := goInt(comp.Type.Width)
			if drop == 0 {
				g.pf("%soutput.%s.%s = %s(input.%s%s)\n", ind, name, compName, storT, name, compName)
			} else {
				g.pf("%soutput.%s.%s = %s(int64(input.%s%s) << %d)\n",
					ind, name, compName, storT, name, compName, drop)
			}
		}
	case f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		scale := g.renderInt(f.QuantScaleExpr, big.NewInt(f.QuantScale))
		for _, comp := range st.Fields {
			cast := "float64"
			if comp.Type.Kind == ir.TFloat32 {
				cast = "float32"
			}
			g.pf("%soutput.%s.%s = %s(input.%s%s) / %s(%s)\n",
				ind, name, ir.GoExportName(comp.Name), cast, name, ir.GoExportName(comp.Name), cast, scale)
		}
	default:
		g.pf("%soutput.%s = input.%s\n", ind, name, name)
	}
}

// emitObjectTagFunctions is the ObjectType twin of the message tag pair.
func (g *gen) emitObjectTagFunctions() {
	g.needsSerialize = true
	count := int64(len(g.unit.ObjNames))
	g.pf("// The object tag wire: ObjectType in [0, %d], minimal bits; None = 0 is the\n", count)
	g.pf("// null — the sentinel the surveyed baseline streams terminate with (SPEC §4.8).\n")
	g.pf("func WriteObjectType(stream *serialize.WriteStream, value ObjectType) error {\n")
	g.pf("\ttagValue := int32(value)\n")
	g.emitWriteRangedFold32("tagValue", "0", big.NewInt(count).String(),
		ir.BitsRequired(big.NewInt(0), big.NewInt(count)), true, "\t")
	g.pf("\treturn stream.Err()\n}\n\n")
	g.pf("func ReadObjectType(stream *serialize.ReadStream, value *ObjectType) error {\n")
	g.pf("\ttagValue := int32(0)\n")
	g.pf("\tstream.SerializeInt(&tagValue, 0, %d)\n", count)
	g.pf("\t*value = ObjectType(tagValue)\n")
	g.pf("\treturn stream.Err()\n}\n\n")
}
