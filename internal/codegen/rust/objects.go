// Object-view function emission (SPEC §4.8, §6.1 item 7): the Deep and
// Shallow wire structs' split write/read pairs, the quantize/unquantize
// mapping between Interpolate and Shallow (floor(c*K + 0.5) per component in
// f64 math, straight copies for discrete and projected fields), and the
// ObjectType tag pair. State structs are simulation-only and get no wire
// functions. The wire is byte-identical to the C++ and Go targets',
// construct by construct.
package rust

import (
	"github.com/mas-bandwidth/schema/internal/ir"
)

func (g *gen) emitObjectFunctions(d *ir.Object) {
	g.needsStreams = true

	deep, interp := splitObjectFields(d)

	deepName := d.Name + "Data_Deep"
	deepSnake := ir.RustSnake(deepName)
	deepBits := ir.MaxBitsView(deep, ir.ViewDeep)
	g.pf("pub const %s: u64 = %d;\n", ir.RustConstName(deepName+"MaxBits"), deepBits)
	g.pf("pub const %s: usize = %d; // rounded up to the 8-byte write-buffer granularity\n\n", ir.RustConstName(deepName+"MaxBytes"), ir.MaxBytes(deepBits))

	g.pf("#[inline]\n")
	g.pf("pub fn write_%s(stream: &mut WriteStream<'_>, value: &%s) -> Result {\n", deepSnake, deepName)
	for _, f := range deep {
		g.emitViewWriteField(f, ir.ViewDeep, "    ")
	}
	g.pf("    Ok(())\n}\n\n")
	g.pf("#[inline]\n")
	g.pf("pub fn read_%s(stream: &mut ReadStream<'_>, value: &mut %s) -> Result {\n", deepSnake, deepName)
	for _, f := range deep {
		g.emitViewReadField(f, ir.ViewDeep, "    ")
	}
	g.pf("    Ok(())\n}\n\n")

	shName := d.Name + "Data_Shallow"
	shSnake := ir.RustSnake(shName)
	shBits := ir.MaxBitsView(interp, ir.ViewShallow)
	g.pf("pub const %s: u64 = %d;\n", ir.RustConstName(shName+"MaxBits"), shBits)
	g.pf("pub const %s: usize = %d; // rounded up to the 8-byte write-buffer granularity\n\n", ir.RustConstName(shName+"MaxBytes"), ir.MaxBytes(shBits))

	g.pf("#[inline]\n")
	g.pf("pub fn write_%s(stream: &mut WriteStream<'_>, value: &%s) -> Result {\n", shSnake, shName)
	for _, f := range interp {
		g.emitViewWriteField(f, ir.ViewShallow, "    ")
	}
	g.pf("    Ok(())\n}\n\n")
	g.pf("#[inline]\n")
	g.pf("pub fn read_%s(stream: &mut ReadStream<'_>, value: &mut %s) -> Result {\n", shSnake, shName)
	for _, f := range interp {
		g.emitViewReadField(f, ir.ViewShallow, "    ")
	}
	g.pf("    Ok(())\n}\n\n")

	// ---- quantize / unquantize: the Interpolate <-> Shallow mapping pair
	// (SPEC §4.8's artifact table — the hand-written Quantize(), generated)
	inName := d.Name + "Data_Interpolate"
	objSnake := ir.RustSnake(d.Name)
	g.pf("pub fn quantize_%s(input: &%s, output: &mut %s) {\n", objSnake, inName, shName)
	for _, f := range interp {
		g.emitQuantizeField(f, "    ")
	}
	g.pf("}\n\n")
	g.pf("pub fn unquantize_%s(input: &%s, output: &mut %s) {\n", objSnake, shName, inName)
	for _, f := range interp {
		g.emitUnquantizeField(f, "    ")
	}
	g.pf("}\n\n")
}

// emitViewWriteField emits one field of a view wire function.
func (g *gen) emitViewWriteField(f *ir.Field, v ir.View, ind string) {
	g.needsStreamTrait = true
	name := "value." + f.Name
	switch {
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		wide := f.QuantBound > 2147483647 // the i32 family truncates past this
		for _, comp := range st.Fields {
			compName := name + "_" + comp.Name
			if wide {
				g.pf("%s{\n%s    let mut component_value = %s as i64;\n", ind, ind, compName)
				g.pf("%s    stream.serialize_int64(&mut component_value, -%d, %d)?;\n%s}\n", ind, f.QuantBound, f.QuantBound, ind)
			} else if smallestSigned(f.QuantBound) == 32 {
				g.pf("%s{\n%s    let mut component_value = %s;\n", ind, ind, compName)
				g.pf("%s    stream.serialize_int(&mut component_value, -%d, %d)?;\n%s}\n", ind, f.QuantBound, f.QuantBound, ind)
			} else {
				g.pf("%s{\n%s    let mut component_value = %s as i32;\n", ind, ind, compName)
				g.pf("%s    stream.serialize_int(&mut component_value, -%d, %d)?;\n%s}\n", ind, f.QuantBound, f.QuantBound, ind)
			}
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		if f.Steps > 2147483647 {
			g.pf("%s{\n%s    let mut projected_value = %s as i64;\n", ind, ind, name)
			g.pf("%s    stream.serialize_int64(&mut projected_value, 0, %d)?;\n%s}\n", ind, f.Steps, ind)
		} else {
			g.pf("%s{\n%s    let mut projected_value = %s as i32;\n", ind, ind, name)
			g.pf("%s    stream.serialize_int(&mut projected_value, 0, %d)?;\n%s}\n", ind, f.Steps, ind)
		}
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		// the triple describes the shallow wire only — deep is the bare float
		g.pf("%s{\n%s    let mut float_value = %s;\n", ind, ind, name)
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%s    stream.serialize_f64(&mut float_value)?;\n%s}\n", ind, ind)
		} else {
			g.pf("%s    stream.serialize_f32(&mut float_value)?;\n%s}\n", ind, ind)
		}
	default:
		g.emitWriteField(f, ind)
	}
}

func (g *gen) emitViewReadField(f *ir.Field, v ir.View, ind string) {
	g.needsStreamTrait = true
	name := "value." + f.Name
	switch {
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		compT := rustInt(smallestSigned(f.QuantBound))
		wide := f.QuantBound > 2147483647
		for _, comp := range st.Fields {
			compName := name + "_" + comp.Name
			if wide {
				g.pf("%s{\n%s    let mut component_value: i64 = 0;\n", ind, ind)
				g.pf("%s    stream.serialize_int64(&mut component_value, -%d, %d)?;\n", ind, f.QuantBound, f.QuantBound)
				g.pf("%s    %s = component_value as %s;\n%s}\n", ind, compName, compT, ind)
			} else if smallestSigned(f.QuantBound) == 32 {
				g.pf("%sstream.serialize_int(&mut %s, -%d, %d)?;\n", ind, compName, f.QuantBound, f.QuantBound)
			} else {
				g.pf("%s{\n%s    let mut component_value: i32 = 0;\n", ind, ind)
				g.pf("%s    stream.serialize_int(&mut component_value, -%d, %d)?;\n", ind, f.QuantBound, f.QuantBound)
				g.pf("%s    %s = component_value as %s;\n%s}\n", ind, compName, compT, ind)
			}
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		storT := rustUint(ir.StorageBitsFor(f.Steps))
		if f.Steps > 2147483647 {
			g.pf("%s{\n%s    let mut projected_value: i64 = 0;\n", ind, ind)
			g.pf("%s    stream.serialize_int64(&mut projected_value, 0, %d)?;\n", ind, f.Steps)
			g.pf("%s    %s = projected_value as %s;\n%s}\n", ind, name, storT, ind)
		} else {
			g.pf("%s{\n%s    let mut projected_value: i32 = 0;\n", ind, ind)
			g.pf("%s    stream.serialize_int(&mut projected_value, 0, %d)?;\n", ind, f.Steps)
			g.pf("%s    %s = projected_value as %s;\n%s}\n", ind, name, storT, ind)
		}
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%sstream.serialize_f64(&mut %s)?;\n", ind, name)
		} else {
			g.pf("%sstream.serialize_f32(&mut %s)?;\n", ind, name)
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
	switch {
	case f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		compT := rustInt(smallestSigned(f.QuantBound))
		scale := g.renderScaleF64(f.QuantScaleExpr, f.QuantScale)
		for _, comp := range st.Fields {
			// f64 math regardless of component width — the referent's own
			// arithmetic, and an f32 product would pre-round before the + 0.5
			compIn := "input." + f.Name + "." + comp.Name
			if comp.Type.Kind == ir.TFloat32 {
				compIn = compIn + " as f64"
			}
			// the clamp happens in the F64 domain before the int conversion:
			// float->int of out-of-range or NaN input is target- and
			// arch-dependent in the other targets' languages, so this shape
			// quantizes even garbage input identically in every target
			// (NaN clamps low)
			g.pf("%s{\n%s    let quantized_value = (%s * %s + 0.5).floor();\n", ind, ind, compIn, scale)
			g.pf("%s    let component_value: i64 = if quantized_value > %d.0_f64 {\n%s        %d\n", ind, f.QuantBound, ind, f.QuantBound)
			g.pf("%s    } else if quantized_value >= -%d.0_f64 {\n%s        quantized_value as i64\n", ind, f.QuantBound, ind)
			g.pf("%s    } else {\n%s        -%d\n%s    };\n", ind, ind, f.QuantBound, ind)
			g.pf("%s    output.%s_%s = component_value as %s;\n%s}\n", ind, f.Name, comp.Name, compT, ind)
		}
	default:
		g.pf("%soutput.%s = input.%s;\n", ind, f.Name, f.Name)
	}
}

// emitUnquantizeField maps one Shallow field back into Interpolate:
// composites divide by the scale; everything else copies.
func (g *gen) emitUnquantizeField(f *ir.Field, ind string) {
	switch {
	case f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			cast := "f64"
			scale := g.renderScaleF64(f.QuantScaleExpr, f.QuantScale)
			if comp.Type.Kind == ir.TFloat32 {
				cast = "f32"
				scale = g.renderScaleF32(f.QuantScaleExpr, f.QuantScale)
			}
			g.pf("%soutput.%s.%s = input.%s_%s as %s / %s;\n",
				ind, f.Name, comp.Name, f.Name, comp.Name, cast, scale)
		}
	default:
		g.pf("%soutput.%s = input.%s;\n", ind, f.Name, f.Name)
	}
}

// emitObjectTagFunctions is the ObjectType twin of the message tag pair.
func (g *gen) emitObjectTagFunctions() {
	g.needsStreams = true
	g.needsStreamTrait = true
	count := int64(len(g.unit.ObjNames))
	g.pf("// The object tag wire: ObjectType in [0, %d], minimal bits; None = 0 is the\n", count)
	g.pf("// null — the sentinel the surveyed baseline streams terminate with (SPEC §4.8).\n")
	g.pf("#[inline]\n")
	g.pf("pub fn write_object_type(stream: &mut WriteStream<'_>, value: ObjectType) -> Result {\n")
	g.pf("    let mut tag_value = value.0 as i32;\n")
	g.pf("    stream.serialize_int(&mut tag_value, 0, %d)?;\n", count)
	g.pf("    Ok(())\n}\n\n")
	g.pf("#[inline]\n")
	g.pf("pub fn read_object_type(stream: &mut ReadStream<'_>, value: &mut ObjectType) -> Result {\n")
	g.pf("    let mut tag_value: i32 = 0;\n")
	g.pf("    stream.serialize_int(&mut tag_value, 0, %d)?;\n", count)
	g.pf("    *value = ObjectType(tag_value as %s);\n", rustUint(ir.StorageBitsFor(count)))
	g.pf("    Ok(())\n}\n\n")
}
