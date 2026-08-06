// Object-view function emission (SPEC §4.8, §6.1 item 7): the Deep and
// Shallow wire classes' split Write/Read pairs, the Quantize/Unquantize
// mapping between Interpolate and Shallow (floor(c*K + 0.5) per component in
// double math, straight copies for discrete and projected fields), and the
// ObjectType tag pair. State classes are simulation-only and get no wire
// functions. The wire is byte-identical to the other targets', construct by
// construct.
package csharp

import (
	"fmt"

	"github.com/mas-bandwidth/schema/internal/ir"
)

func (g *gen) emitObjectFunctions(d *ir.Object) {
	g.needsSerialize = true

	deep, interp := splitObjectFields(d)

	deepName := d.Name + "Data_Deep"
	deepBits := ir.MaxBitsView(deep, ir.ViewDeep)
	g.owner = deepName // member references escape exactly as the class emitted them
	g.sf("public const long %sMaxBits = %d;\n", deepName, deepBits)
	g.sf("public const long %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", deepName, ir.MaxBytes(deepBits))

	g.sf("public static bool Write%s(WriteStream stream, %s value)\n{\n", deepName, deepName)
	for _, f := range deep {
		g.emitViewWriteField(f, ir.ViewDeep, "    ")
	}
	g.sf("    return true;\n}\n\n")
	g.sf("public static bool Read%s(ReadStream stream, %s value)\n{\n", deepName, deepName)
	for _, f := range deep {
		g.emitViewReadField(f, ir.ViewDeep, "    ")
	}
	g.sf("    return true;\n}\n\n")

	shName := d.Name + "Data_Shallow"
	shBits := ir.MaxBitsView(interp, ir.ViewShallow)
	g.owner = shName
	g.sf("public const long %sMaxBits = %d;\n", shName, shBits)
	g.sf("public const long %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", shName, ir.MaxBytes(shBits))

	g.sf("public static bool Write%s(WriteStream stream, %s value)\n{\n", shName, shName)
	for _, f := range interp {
		g.emitViewWriteField(f, ir.ViewShallow, "    ")
	}
	g.sf("    return true;\n}\n\n")
	g.sf("public static bool Read%s(ReadStream stream, %s value)\n{\n", shName, shName)
	for _, f := range interp {
		g.emitViewReadField(f, ir.ViewShallow, "    ")
	}
	g.sf("    return true;\n}\n\n")

	// ---- Quantize / Unquantize: the Interpolate <-> Shallow mapping pair
	// (SPEC §4.8's artifact table — the hand-written Quantize(), generated).
	// Top-level members of the view classes never need the CS0542 escape
	// (view class names carry an underscore ir.GoExportName cannot produce).
	inName := d.Name + "Data_Interpolate"
	g.owner = ""
	g.sf("public static void Quantize%s(%s input, %s output)\n{\n", d.Name, inName, shName)
	for _, f := range interp {
		g.emitQuantizeField(f, "    ")
	}
	g.sf("}\n\n")
	g.sf("public static void Unquantize%s(%s input, %s output)\n{\n", d.Name, shName, inName)
	for _, f := range interp {
		g.emitUnquantizeField(f, "    ")
	}
	g.sf("}\n\n")
}

// emitViewWriteField emits one field of a view wire function.
func (g *gen) emitViewWriteField(f *ir.Field, v ir.View, ind string) {
	name := "value." + g.fieldBase(f)
	switch {
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		wide := f.QuantBound > 2147483647 // the int32 family truncates past this
		for _, comp := range st.Fields {
			compName := name + ir.GoExportName(comp.Name)
			if wide {
				g.sf("%s{\n%s    long componentValue = (long)%s;\n", ind, ind, compName)
				g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt64(ref componentValue, -%d, %d)", f.QuantBound, f.QuantBound), "")
				g.sf("%s}\n", ind)
			} else if smallestSigned(f.QuantBound) == 32 {
				g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, -%d, %d)", compName, f.QuantBound, f.QuantBound), "")
			} else {
				g.sf("%s{\n%s    int componentValue = (int)%s;\n", ind, ind, compName)
				g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt(ref componentValue, -%d, %d)", f.QuantBound, f.QuantBound), "")
				g.sf("%s}\n", ind)
			}
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		if f.Steps > 2147483647 {
			g.sf("%s{\n%s    long projectedValue = (long)%s;\n", ind, ind, name)
			g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt64(ref projectedValue, 0, %d)", f.Steps), "")
			g.sf("%s}\n", ind)
		} else {
			g.sf("%s{\n%s    int projectedValue = (int)%s;\n", ind, ind, name)
			g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt(ref projectedValue, 0, %d)", f.Steps), "")
			g.sf("%s}\n", ind)
		}
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		// the triple describes the shallow wire only — deep is the bare float
		if f.Type.Kind == ir.TFloat64 {
			g.call(ind, fmt.Sprintf("stream.SerializeDouble(ref %s)", name), "")
		} else {
			g.call(ind, fmt.Sprintf("stream.SerializeFloat(ref %s)", name), "")
		}
	default:
		g.emitWriteField(f, ind)
	}
}

func (g *gen) emitViewReadField(f *ir.Field, v ir.View, ind string) {
	name := "value." + g.fieldBase(f)
	switch {
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		compT := csInt(smallestSigned(f.QuantBound))
		wide := f.QuantBound > 2147483647
		for _, comp := range st.Fields {
			compName := name + ir.GoExportName(comp.Name)
			if wide {
				g.sf("%s{\n%s    long componentValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt64(ref componentValue, -%d, %d)", f.QuantBound, f.QuantBound), "")
				g.sf("%s    %s = (%s)componentValue;\n%s}\n", ind, compName, compT, ind)
			} else if smallestSigned(f.QuantBound) == 32 {
				g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, -%d, %d)", compName, f.QuantBound, f.QuantBound), "")
			} else {
				g.sf("%s{\n%s    int componentValue = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt(ref componentValue, -%d, %d)", f.QuantBound, f.QuantBound), "")
				g.sf("%s    %s = (%s)componentValue;\n%s}\n", ind, compName, compT, ind)
			}
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		storT := csUint(ir.StorageBitsFor(f.Steps))
		if f.Steps > 2147483647 {
			g.sf("%s{\n%s    long projectedValue = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt64(ref projectedValue, 0, %d)", f.Steps), "")
			g.sf("%s    %s = (%s)projectedValue;\n%s}\n", ind, name, storT, ind)
		} else {
			g.sf("%s{\n%s    int projectedValue = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("stream.SerializeInt(ref projectedValue, 0, %d)", f.Steps), "")
			g.sf("%s    %s = (%s)projectedValue;\n%s}\n", ind, name, storT, ind)
		}
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		if f.Type.Kind == ir.TFloat64 {
			g.call(ind, fmt.Sprintf("stream.SerializeDouble(ref %s)", name), "")
		} else {
			g.call(ind, fmt.Sprintf("stream.SerializeFloat(ref %s)", name), "")
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
	case f.HasQuantize:
		g.needsSystem = true
		st := f.Type.Ref.(*ir.Struct)
		compT := csInt(smallestSigned(f.QuantBound))
		scale := g.renderScaleF64(f.QuantScaleExpr, f.QuantScale)
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			// double math regardless of component width — the referent's own
			// arithmetic, and a float product would pre-round before the + 0.5
			compIn := "input." + name + "." + compName
			if comp.Type.Kind == ir.TFloat32 {
				compIn = "(double)" + compIn
			}
			// the clamp happens in the DOUBLE domain before the int conversion:
			// float->int of out-of-range or NaN input is target- and
			// arch-dependent across the family's languages, so this shape
			// quantizes even garbage input identically in every target
			// (NaN clamps low)
			g.sf("%s{\n%s    double quantizedValue = Math.Floor(%s * %s + 0.5);\n", ind, ind, compIn, scale)
			g.sf("%s    long componentValue = -%d;\n", ind, f.QuantBound)
			g.sf("%s    if (quantizedValue > %d.0)\n%s    {\n%s        componentValue = %d;\n%s    }\n",
				ind, f.QuantBound, ind, ind, f.QuantBound, ind)
			g.sf("%s    else if (quantizedValue >= -%d.0)\n%s    {\n%s        componentValue = (long)quantizedValue;\n%s    }\n",
				ind, f.QuantBound, ind, ind, ind)
			g.sf("%s    output.%s%s = (%s)componentValue;\n%s}\n", ind, name, compName, compT, ind)
		}
	default:
		g.emitCopyField("output", "input", "", f, ind, 0)
	}
}

// emitUnquantizeField maps one Shallow field back into Interpolate:
// composites divide by the scale; everything else copies.
func (g *gen) emitUnquantizeField(f *ir.Field, ind string) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			cast := "double"
			scale := g.renderScaleF64(f.QuantScaleExpr, f.QuantScale)
			if comp.Type.Kind == ir.TFloat32 {
				cast = "float"
				scale = g.renderScaleF32(f.QuantScaleExpr, f.QuantScale)
			}
			g.sf("%soutput.%s.%s = (%s)input.%s%s / %s;\n",
				ind, name, compName, cast, name, compName, scale)
		}
	default:
		g.emitCopyField("output", "input", "", f, ind, 0)
	}
}

// emitCopyField copies one non-quantized field between view instances. The
// other targets copy with one value assignment; C# classes and arrays are
// references, so buffers copy element-wise into the destination's
// pre-allocated storage and composed classes copy member-wise (recursion ends
// because composition cycles are compile errors). owner is the class the
// field belongs to, for the CS0542 member escape ("" for the view classes,
// whose underscored names the mapping can never produce); depth uniquifies
// nested loop variables.
func (g *gen) emitCopyField(dstPrefix, srcPrefix, owner string, f *ir.Field, ind string, depth int) {
	base := ir.GoExportName(f.Name)
	dst := dstPrefix + "." + base
	src := srcPrefix + "." + base
	iv := loopVar(depth)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.needsSystem = true
		length := base + "Length"
		g.sf("%sArray.Copy(%s, %s, %s.Length);\n", ind, src, dst, src)
		g.sf("%s%s.%s = %s.%s;\n", ind, dstPrefix, length, srcPrefix, length)
	case f.Array != ir.ArrayNone:
		if st, isStruct := f.Type.Ref.(*ir.Struct); isStruct && f.Type.Kind == ir.TNamed {
			g.sf("%sfor (int %s = 0; %s < %s.Length; %s++)\n%s{\n", ind, iv, iv, src, iv, ind)
			g.emitCopyStruct(dst+"["+iv+"]", src+"["+iv+"]", st, ind+"    ", depth+1)
			g.sf("%s}\n", ind)
		} else {
			g.needsSystem = true
			g.sf("%sArray.Copy(%s, %s, %s.Length);\n", ind, src, dst, src)
		}
		if f.Array == ir.ArrayCounted {
			count := base + "Count"
			g.sf("%s%s.%s = %s.%s;\n", ind, dstPrefix, count, srcPrefix, count)
		}
	default:
		if st, isStruct := f.Type.Ref.(*ir.Struct); isStruct && f.Type.Kind == ir.TNamed {
			g.emitCopyStruct(dst, src, st, ind, depth)
			return
		}
		g.sf("%s%s = %s;\n", ind, dst, src)
	}
}

func (g *gen) emitCopyStruct(dst, src string, st *ir.Struct, ind string, depth int) {
	for _, f := range st.Fields {
		g.emitCopyField(dst, src, st.Name, f, ind, depth)
	}
}

func loopVar(depth int) string {
	if depth == 0 {
		return "i"
	}
	return fmt.Sprintf("i%d", depth+1)
}

// emitObjectTagFunctions is the ObjectType twin of the message tag pair.
func (g *gen) emitObjectTagFunctions() {
	g.needsSerialize = true
	count := int64(len(g.unit.ObjNames))
	g.sf("// The object tag wire: ObjectType in [0, %d], minimal bits; None = 0 is the\n", count)
	g.sf("// null — the sentinel the surveyed baseline streams terminate with (SPEC §4.8).\n")
	g.sf("public static bool WriteObjectType(WriteStream stream, ObjectType value)\n{\n")
	g.sf("    int tagValue = (int)value;\n")
	g.sf("    return stream.SerializeInt(ref tagValue, 0, %d);\n}\n\n", count)
	g.sf("public static bool ReadObjectType(ReadStream stream, ref ObjectType value)\n{\n")
	g.sf("    int tagValue = 0;\n")
	g.sf("    if (!stream.SerializeInt(ref tagValue, 0, %d))\n    {\n        return false;\n    }\n", count)
	g.sf("    value = (ObjectType)tagValue;\n    return true;\n}\n\n")
}
