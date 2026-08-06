// Object-view function emission (SPEC §4.8, §6.1 item 7): the Deep and
// Shallow wire structs' split Write/Read pairs, the Quantize/Unquantize
// mapping between Interpolate and Shallow (the hand-written referent's shape:
// floor(c*K + 0.5) per component, straight copies for discrete and projected
// fields), and the ObjectType tag pair. State structs are simulation-only and
// get no wire functions.
package cpp

import (
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

func (g *gen) emitObjectFunctions(d *ir.Object) {
	g.needsSerialize = true

	var deep, interp []*ir.Field
	for _, f := range d.Fields {
		if !f.Local {
			deep = append(deep, f)
		}
		if f.Interpolate {
			interp = append(interp, f)
		}
	}

	// ---- Deep: every non-local field, deep encodings — the view-encoding
	// attributes describe the SHALLOW wire only, so an [interpolate] float
	// triple serializes as a bare float here (SPEC §4.8)
	deepName := d.Name + "Data_Deep"
	deepBits := g.maxBitsView(deep, viewDeep)
	g.pf("inline constexpr int64_t %sMaxBits = %d;\n", deepName, deepBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", deepName, ir.MaxBytes(deepBits))

	g.pf("inline bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", deepName, deepName)
	for _, f := range deep {
		g.emitViewWriteField(f, viewDeep, "    ")
	}
	g.pf("    return true;\n}\n\n")
	g.pf("inline bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", deepName, deepName)
	for _, f := range deep {
		g.emitViewReadField(f, viewDeep, "    ")
	}
	g.pf("    return true;\n}\n\n")

	// ---- Shallow: the [interpolate] fields on the quantized wire
	shName := d.Name + "Data_Shallow"
	shBits := g.maxBitsView(interp, viewShallow)
	g.pf("inline constexpr int64_t %sMaxBits = %d;\n", shName, shBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", shName, ir.MaxBytes(shBits))

	g.pf("inline bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", shName, shName)
	for _, f := range interp {
		g.emitViewWriteField(f, viewShallow, "    ")
	}
	g.pf("    return true;\n}\n\n")
	g.pf("inline bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", shName, shName)
	for _, f := range interp {
		g.emitViewReadField(f, viewShallow, "    ")
	}
	g.pf("    return true;\n}\n\n")

	// ---- Quantize / Unquantize: the Interpolate <-> Shallow mapping pair
	// (SPEC §4.8's artifact table — the hand-written Quantize(), generated)
	inName := d.Name + "Data_Interpolate"
	g.pf("inline void Quantize%s( const %s & input, %s & output )\n{\n", d.Name, inName, shName)
	for _, f := range interp {
		g.emitQuantizeField(f, "    ")
	}
	g.pf("}\n\n")
	g.pf("inline void Unquantize%s( const %s & input, %s & output )\n{\n", d.Name, shName, inName)
	for _, f := range interp {
		g.emitUnquantizeField(f, "    ")
	}
	g.pf("}\n\n")
}

type objView = ir.View

const (
	viewDeep    = ir.ViewDeep
	viewShallow = ir.ViewShallow
)

func (g *gen) maxBitsView(fields []*ir.Field, v objView) int64 { return ir.MaxBitsView(fields, v) }

// emitViewWriteField emits one field of a view wire function.
func (g *gen) emitViewWriteField(f *ir.Field, v objView, ind string) {
	name := "value." + f.Name
	switch {
	case v == viewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		wide := f.QuantBound > 2147483647 // the int32 family truncates past this (same switch as intRangePath)
		for _, comp := range st.Fields {
			if wide {
				g.pf("%swrite_int64( stream, %s_%s, -%dll, %dll );\n", ind, name, comp.Name, f.QuantBound, f.QuantBound)
			} else {
				g.pf("%swrite_int( stream, %s_%s, -%d, %d );\n", ind, name, comp.Name, f.QuantBound, f.QuantBound)
			}
		}
	case v == viewShallow && f.HasFloatRange:
		if f.Steps > 2147483647 {
			g.pf("%swrite_int64( stream, %s, 0, %dll );\n", ind, name, f.Steps)
		} else {
			g.pf("%swrite_int( stream, %s, 0, %d );\n", ind, name, f.Steps)
		}
	case v == viewDeep && f.HasFloatRange && f.Interpolate:
		// the triple describes the shallow wire only — deep is the bare float
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%swrite_double( stream, %s );\n", ind, name)
		} else {
			g.pf("%swrite_float( stream, %s );\n", ind, name)
		}
	default:
		g.emitWriteField(f, ind)
	}
}

func (g *gen) emitViewReadField(f *ir.Field, v objView, ind string) {
	name := "value." + f.Name
	switch {
	case v == viewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		compT := cppInt(smallestSigned(f.QuantBound))
		wide := f.QuantBound > 2147483647
		for _, comp := range st.Fields {
			if wide {
				g.pf("%s{\n%s    int64_t component_value = 0;\n", ind, ind)
				g.pf("%s    read_int64( stream, component_value, -%dll, %dll );\n", ind, f.QuantBound, f.QuantBound)
			} else {
				g.pf("%s{\n%s    int32_t component_value = 0;\n", ind, ind)
				g.pf("%s    read_int( stream, component_value, -%d, %d );\n", ind, f.QuantBound, f.QuantBound)
			}
			g.pf("%s    %s_%s = %s( component_value );\n%s}\n", ind, name, comp.Name, compT, ind)
		}
	case v == viewShallow && f.HasFloatRange:
		storT := cppUint(smallestUnsigned(f.Steps))
		if f.Steps > 2147483647 {
			g.pf("%s{\n%s    int64_t projected_value = 0;\n", ind, ind)
			g.pf("%s    read_int64( stream, projected_value, 0, %dll );\n", ind, f.Steps)
		} else {
			g.pf("%s{\n%s    int32_t projected_value = 0;\n", ind, ind)
			g.pf("%s    read_int( stream, projected_value, 0, %d );\n", ind, f.Steps)
		}
		g.pf("%s    %s = %s( projected_value );\n%s}\n", ind, name, storT, ind)
	case v == viewDeep && f.HasFloatRange && f.Interpolate:
		if f.Type.Kind == ir.TFloat64 {
			g.pf("%sread_double( stream, %s );\n", ind, name)
		} else {
			g.pf("%sread_float( stream, %s );\n", ind, name)
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
		g.needsCmath = true
		st := f.Type.Ref.(*ir.Struct)
		compT := cppInt(smallestSigned(f.QuantBound))
		scale := g.renderInt(f.QuantScaleExpr, big.NewInt(f.QuantScale))
		for _, comp := range st.Fields {
			// double math regardless of component width — the referent's own
			// arithmetic, and a float product would pre-round before the + 0.5.
			// The clamp happens in the DOUBLE domain before the int conversion:
			// float->int of out-of-range or NaN input is target- and
			// arch-dependent, so this shape quantizes even garbage input
			// identically in every target (NaN clamps low)
			g.pf("%s{\n%s    double quantized_value = floor( double( input.%s.%s ) * double( %s ) + 0.5 );\n",
				ind, ind, f.Name, comp.Name, scale)
			g.pf("%s    int64_t component_value = -%dll;\n", ind, f.QuantBound)
			g.pf("%s    if ( quantized_value > %d.0 )\n%s    {\n%s        component_value = %dll;\n%s    }\n",
				ind, f.QuantBound, ind, ind, f.QuantBound, ind)
			g.pf("%s    else if ( quantized_value >= -%d.0 )\n%s    {\n%s        component_value = int64_t( quantized_value );\n%s    }\n",
				ind, f.QuantBound, ind, ind, ind)
			g.pf("%s    output.%s_%s = %s( component_value );\n%s}\n", ind, f.Name, comp.Name, compT, ind)
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
		scale := g.renderInt(f.QuantScaleExpr, big.NewInt(f.QuantScale))
		for _, comp := range st.Fields {
			cast := ""
			if comp.Type.Kind == ir.TFloat32 {
				cast = "float"
			} else {
				cast = "double"
			}
			g.pf("%soutput.%s.%s = %s( input.%s_%s ) / %s( %s );\n",
				ind, f.Name, comp.Name, cast, f.Name, comp.Name, cast, scale)
		}
	default:
		g.pf("%soutput.%s = input.%s;\n", ind, f.Name, f.Name)
	}
}

// emitObjectTagFunctions is the ObjectType twin of the message tag pair.
func (g *gen) emitObjectTagFunctions() {
	g.needsSerialize = true
	count := int64(len(g.unit.ObjNames))
	g.pf("// The object tag wire: ObjectType in [0, %d], minimal bits; None = 0 is the\n", count)
	g.pf("// null — the sentinel the surveyed baseline streams terminate with (SPEC §4.8).\n")
	g.pf("inline bool WriteObjectType( serialize::WriteStream & stream, ObjectType value )\n{\n")
	g.pf("    write_int( stream, int32_t( value ), 0, %d );\n    return true;\n}\n\n", count)
	g.pf("inline bool ReadObjectType( serialize::ReadStream & stream, ObjectType & value )\n{\n")
	g.pf("    int32_t tag_value = 0;\n")
	g.pf("    read_int( stream, tag_value, 0, %d );\n", count)
	g.pf("    value = ObjectType( tag_value );\n    return true;\n}\n\n")
}
