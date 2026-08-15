// Object-view function emission (SPEC §4.8, §6.1 item 7): the Deep and
// Shallow wire structs' split Write/Read pairs, the Quantize/Unquantize
// mapping between Interpolate and Shallow (the hand-written referent's shape:
// floor(c*K + 0.5) per component, straight copies for discrete and projected
// fields), and the ObjectType tag pair. State structs are simulation-only and
// get no wire functions.
package cpp

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// emitObjectMaxBits emits the Deep and Shallow view bounds — data-side,
// because buffer sizing needs no serialize dependency.
func (g *gen) emitObjectMaxBits(d *ir.Object) {
	var deep, interp []*ir.Field
	for _, f := range d.Fields {
		if !f.Local {
			deep = append(deep, f)
		}
		if f.Interpolate {
			interp = append(interp, f)
		}
	}
	deepName := d.Name + "Data_Deep"
	deepBits := g.maxBitsView(deep, viewDeep)
	g.pf("inline constexpr int64_t %sMaxBits = %d;\n", deepName, deepBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", deepName, ir.MaxBytes(deepBits))
	shName := d.Name + "Data_Shallow"
	shBits := g.maxBitsView(interp, viewShallow)
	g.pf("inline constexpr int64_t %sMaxBits = %d;\n", shName, shBits)
	g.pf("inline constexpr int64_t %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", shName, ir.MaxBytes(shBits))
}

// voidIfEmpty emits (void) casts for params when no statement has been
// emitted since mark — a view function over zero wire components must not
// strand its parameters, or every -Wall -Wextra -Werror consumer breaks
// (found by FuzzGeneratedCompiles, issue #22).
func (g *gen) voidIfEmpty(mark int, params ...string) {
	if g.body.Len() != mark {
		return
	}
	for _, p := range params {
		g.pf("    (void) %s;\n", p)
	}
}

func (g *gen) emitObjectWire(d *ir.Object) {
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
	g.pf("inline bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", deepName, deepName)
	mark := g.body.Len()
	for _, f := range deep {
		g.emitViewWriteField(f, viewDeep, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return true;\n}\n\n")
	g.pf("inline bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", deepName, deepName)
	mark = g.body.Len()
	for _, f := range deep {
		g.emitViewReadField(f, viewDeep, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return true;\n}\n\n")

	// ---- Shallow: the [interpolate] fields on the quantized wire
	shName := d.Name + "Data_Shallow"
	g.pf("inline bool Write%s( serialize::WriteStream & stream, const %s & value )\n{\n", shName, shName)
	mark = g.body.Len()
	for _, f := range interp {
		g.emitViewWriteField(f, viewShallow, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return true;\n}\n\n")
	g.pf("inline bool Read%s( serialize::ReadStream & stream, %s & value )\n{\n", shName, shName)
	mark = g.body.Len()
	for _, f := range interp {
		g.emitViewReadField(f, viewShallow, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return true;\n}\n\n")

}

// emitObjectQuantize emits the Quantize/Unquantize pair (the Interpolate <->
// Shallow mapping, SPEC §4.8's artifact table — the hand-written Quantize(),
// generated). Emitted into the DATA header, not the wire header: the pair is
// pure struct math with no serialize dependency, and consumers living in a
// core_serialize world (the game's snapshot path) must be able to include it
// without the wire header's serialize.h.
func (g *gen) emitObjectQuantize(d *ir.Object) {
	if !ir.ObjectNeedsQuantize(d) {
		// every [interpolate] field rides the wire domain verbatim — fixed
		// components are their own quantization (SPEC §4.8) — so the pair
		// would be a pure member copy and is NOT emitted.
		g.pf("// Quantize%s/Unquantize%s are not emitted: every [interpolate] field\n", d.Name, d.Name)
		g.pf("// is already wire-domain (fixed components are their own quantization,\n")
		g.pf("// SPEC §4.8) — Interpolate and Shallow are the same values.\n\n")
		return
	}
	var interp []*ir.Field
	for _, f := range d.Fields {
		if f.Interpolate {
			interp = append(interp, f)
		}
	}
	shName := d.Name + "Data_Shallow"
	inName := d.Name + "Data_Interpolate"
	g.pf("inline void Quantize%s( const %s & input, %s & output )\n{\n", d.Name, inName, shName)
	mark := g.body.Len()
	for _, f := range interp {
		g.emitQuantizeField(f, "    ")
	}
	if g.body.Len() == mark {
		// a quantized property over a type with no numeric components emits
		// no statements; keep -Werror consumers building (found by
		// FuzzGeneratedCompiles, issue #22)
		g.pf("    (void) input;\n    (void) output; // no numeric components to quantize\n")
	}
	g.pf("}\n\n")
	g.pf("inline void Unquantize%s( const %s & input, %s & output )\n{\n", d.Name, shName, inName)
	mark = g.body.Len()
	for _, f := range interp {
		g.emitUnquantizeField(f, "    ")
	}
	if g.body.Len() == mark {
		g.pf("    (void) input;\n    (void) output; // no numeric components to unquantize\n")
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
	case v == viewShallow && f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			lo, hi, bits, wide, _ := fixedShallowComp(f, comp)
			if wide {
				g.emitWriteRangedFold64(fmt.Sprintf("%s_%s", name, comp.Name),
					cppInt64Lit(lo), cppInt64Lit(hi), bits, false, ind)
			} else {
				g.emitWriteRangedFold32(fmt.Sprintf("%s_%s", name, comp.Name),
					lo.String(), hi.String(), bits, false, ind)
			}
		}
	case v == viewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		wide := f.QuantBound > 2147483647 // the int32 family truncates past this (same switch as intRangePath)
		quantBits := bitsRequired(big.NewInt(-f.QuantBound), big.NewInt(f.QuantBound))
		for _, comp := range st.Fields {
			if wide {
				g.emitWriteRangedFold64(fmt.Sprintf("%s_%s", name, comp.Name),
					fmt.Sprintf("-%dll", f.QuantBound), fmt.Sprintf("%dll", f.QuantBound), quantBits, false, ind)
			} else {
				g.emitWriteRangedFold32(fmt.Sprintf("%s_%s", name, comp.Name),
					fmt.Sprintf("-%d", f.QuantBound), fmt.Sprintf("%d", f.QuantBound), quantBits, false, ind)
			}
		}
	case v == viewShallow && f.HasFloatRange:
		stepBits := bitsRequired(big.NewInt(0), big.NewInt(f.Steps))
		if f.Steps > 2147483647 {
			g.emitWriteRangedFold64(name, "0", fmt.Sprintf("%dll", f.Steps), stepBits, true, ind)
		} else {
			g.emitWriteRangedFold32(name, "0", fmt.Sprintf("%d", f.Steps), stepBits, true, ind)
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
	case v == viewShallow && f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			lo, hi, bits, wide, compT := fixedShallowComp(f, comp)
			if bits == 0 {
				// a degenerate component narrows to zero bits — the value is
				// the range (SPEC §4.6, decided 2026-08-15)
				g.pf("%s%s_%s = %s( %s );\n", ind, name, comp.Name, compT, cppInt64Lit(lo))
				continue
			}
			if wide {
				g.pf("%s{\n%s    int64_t component_value = 0;\n", ind, ind)
				g.pf("%s    read_int64( stream, component_value, %s, %s );\n", ind, cppInt64Lit(lo), cppInt64Lit(hi))
			} else {
				g.pf("%s{\n%s    int32_t component_value = 0;\n", ind, ind)
				g.pf("%s    read_int( stream, component_value, %s, %s );\n", ind, lo, hi)
			}
			g.pf("%s    %s_%s = %s( component_value );\n%s}\n", ind, name, comp.Name, compT, ind)
		}
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
	case f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			drop := comp.Type.FracBits - f.QuantShift
			_, _, _, _, compT := fixedShallowComp(f, comp)
			if drop == 0 {
				g.pf("%soutput.%s_%s = %s( input.%s.%s );\n", ind, f.Name, comp.Name, compT, f.Name, comp.Name)
				continue
			}
			// round-to-nearest narrowing shift — arithmetic on int64, ties
			// AWAY FROM ZERO: the one fixed-point rounding rule (SPEC §4.8,
			// decided 2026-08-15; the data compiler's ratRoundHalfAway is the
			// same rule). Negative raws mirror through negation so the tie
			// leaves zero in both signs. In-bounds raws cannot overflow the
			// add or the negation (checker-enforced bounds leave 2^(F-1) of
			// headroom past any legal raw)
			half := int64(1) << (drop - 1)
			g.pf("%s{\n%s    int64_t raw = int64_t( input.%s.%s );\n", ind, ind, f.Name, comp.Name)
			g.pf("%s    output.%s_%s = %s( raw >= 0 ? ( raw + %dll ) >> %d : -( ( -raw + %dll ) >> %d ) );\n",
				ind, f.Name, comp.Name, compT, half, drop, half, drop)
			g.pf("%s}\n", ind)
		}
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
			// the product lands in a named local BEFORE the + 0.5, so
			// FP_CONTRACT cannot fuse the multiply into the add — the
			// compressed_float FMA hazard one level up, defended the way
			// serialize.c defends its writer (#26)
			g.pf("%s{\n%s    double scaled_value = double( input.%s.%s ) * double( %s );\n",
				ind, ind, f.Name, comp.Name, scale)
			g.pf("%s    double quantized_value = floor( scaled_value + 0.5 );\n", ind)
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
	case f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			drop := comp.Type.FracBits - f.QuantShift
			storT := cppInt(comp.Type.Width)
			if drop == 0 {
				g.pf("%soutput.%s.%s = %s( input.%s_%s );\n", ind, f.Name, comp.Name, storT, f.Name, comp.Name)
			} else {
				g.pf("%soutput.%s.%s = %s( int64_t( input.%s_%s ) << %d );\n", ind, f.Name, comp.Name, storT, f.Name, comp.Name, drop)
			}
		}
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

// emitObjectTagWire is the ObjectType twin of the message tag pair.
func (g *gen) emitObjectTagWire() {
	g.needsSerialize = true
	count := int64(len(g.unit.ObjNames))
	g.pf("// The object tag wire: ObjectType in [0, %d], minimal bits; None = 0 is the\n", count)
	g.pf("// null — the sentinel the surveyed baseline streams terminate with (SPEC §4.8).\n")
	g.pf("inline bool WriteObjectType( serialize::WriteStream & stream, ObjectType value )\n{\n")
	g.emitWriteRangedFold32("value", "0", fmt.Sprintf("%d", count),
		bitsRequired(big.NewInt(0), big.NewInt(count)), true, "    ")
	g.pf("    return true;\n}\n\n")
	g.pf("inline bool ReadObjectType( serialize::ReadStream & stream, ObjectType & value )\n{\n")
	g.pf("    int32_t tag_value = 0;\n")
	g.pf("    read_int( stream, tag_value, 0, %d );\n", count)
	g.pf("    value = ObjectType( tag_value );\n    return true;\n}\n\n")
}
