package c

// Objects (SPEC §4.8): one declaration, a generated family per target.
//
// An object produces a State struct (the full simulation type, generated once
// per context where the object carries [local, context = ...] fields), plus
// three views: Data_Deep (every non-[local] field, declared encodings),
// Data_Shallow (the [interpolate] fields on the quantized wire) and
// Data_Interpolate (the same fields in their interpolation domain). The
// Quantize/Unquantize pair moves between the last two.

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// view selects the storage rules a field is emitted under.
type view int

const (
	storageDeep    view = iota // declared storage — State and Data_Deep
	storageShallow             // quantized wire storage
	storageInterp              // the interpolation domain
)

func hasContextFields(d *ir.Object) bool {
	for _, f := range d.Fields {
		if f.Context != "" {
			return true
		}
	}
	return false
}

// splitObjectFields separates the deep view (every non-[local] field) from the
// interpolate view (the [interpolate] fields).
func splitObjectFields(d *ir.Object) (deep, interp []*ir.Field) {
	for _, f := range d.Fields {
		if !f.Local {
			deep = append(deep, f)
		}
		if f.Interpolate {
			interp = append(interp, f)
		}
	}
	return deep, interp
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}

// ---- the data header ----

func (g *gen) emitObject(d *ir.Object) {
	g.pf("\n/* ---- object %s — one definition, a generated family per target (SPEC §4.8) ---- */\n\n", d.Name)

	if hasContextFields(d) {
		for _, ctx := range g.unit.Contexts {
			var fields []*ir.Field
			for _, f := range d.Fields {
				if f.Context == "" || f.Context == ctx {
					fields = append(fields, f)
				}
			}
			name := capitalize(ctx) + d.Name + "State"
			g.pf("/* %s — the full simulation struct for the %s context: every `all`\n", name, ctx)
			g.pf("   field plus the fields scoped [local, context = %s] */\n", ctx)
			g.emitViewStruct(name, fields, storageDeep)
		}
	} else {
		g.pf("/* %sState — the full simulation struct: every field */\n", d.Name)
		g.emitViewStruct(d.Name+"State", d.Fields, storageDeep)
	}

	deep, interp := splitObjectFields(d)

	g.pf("/* %sData_Deep — every non-[local] field, deep encodings: full state for\n", d.Name)
	g.pf("   client-side prediction */\n")
	g.emitViewStruct(d.Name+"Data_Deep", deep, storageDeep)

	g.pf("/* %sData_Shallow — the [interpolate] fields on the quantized wire */\n", d.Name)
	g.emitViewStruct(d.Name+"Data_Shallow", interp, storageShallow)

	g.pf("/* %sData_Interpolate — the same fields in their interpolation domain */\n", d.Name)
	g.emitViewStruct(d.Name+"Data_Interpolate", interp, storageInterp)
}

func (g *gen) emitViewStruct(name string, fields []*ir.Field, v view) {
	if len(fields) == 0 {
		g.pf("typedef struct %s {\n    char unused_; /* C has no empty struct */\n} %s;\n\n", name, name)
		return
	}
	g.pf("typedef struct %s {\n", name)
	for _, f := range fields {
		g.emitViewField(f, v)
	}
	g.pf("} %s;\n\n", name)
}

// emitViewField emits one field under a view's storage rules.
func (g *gen) emitViewField(f *ir.Field, v view) {
	// A composite quantized into the shallow view becomes per-component
	// integers, one per member of the referenced type.
	if v == storageShallow && f.HasQuantize {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		lo, hi := quantBounds(f)
		g.pf("    /* %s: %s quantized — per-component int in [%s, %s] */\n", f.Name, f.Type.Name, lo, hi)
		for _, comp := range st.Fields {
			g.pf("    %s %s_%s;\n", quantStorage(f), f.Name, comp.Name)
		}
		return
	}
	// SPEC §4.8 rule 5: a compressed float is PROJECTED to its wire-integer
	// domain in the shallow and interpolate views — snap-interpolated, not
	// carried as a float. Only the deep view keeps the float, and the deep view
	// writes it at full precision.
	if (v == storageShallow || v == storageInterp) && f.HasFloatRange {
		g.pf("    %s %s; /* float [%s, %s] @ %s -> wire int [0, %d] */\n",
			projStorage(f.Steps), f.Name, formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution), f.Steps)
		return
	}
	g.emitField(f)
}

// quantStorage is the per-component integer width a quantized composite uses.
func quantStorage(f *ir.Field) string {
	bound := f.QuantBound
	switch {
	case bound <= 0x7F:
		return "int8_t"
	case bound <= 0x7FFF:
		return "int16_t"
	case bound <= 0x7FFFFFFF:
		return "int32_t"
	}
	return "int64_t"
}

// projStorage is the smallest unsigned type holding a projected float's wire
// integer domain [0, steps].
func projStorage(steps int64) string {
	switch {
	case steps <= 0xFF:
		return "uint8_t"
	case steps <= 0xFFFF:
		return "uint16_t"
	case steps <= 0xFFFFFFFF:
		return "uint32_t"
	}
	return "uint64_t"
}

func quantBounds(f *ir.Field) (string, string) {
	b := big.NewInt(f.QuantBound)
	return new(big.Int).Neg(b).String(), b.String()
}

// ---- the wire header ----

func (g *gen) emitObjectFunctions(d *ir.Object) {
	deep, interp := splitObjectFields(d)

	g.pf("/* Writes %s's DEEP view — the declared encodings. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED int write_%s_deep( serialize_write_stream_t * stream, const %sData_Deep * value )\n{\n",
		snake(d.Name), d.Name)
	for _, f := range deep {
		g.emitDeepWriteField(f, "    ")
	}
	g.pf("    return 1;\n}\n\n")

	g.pf("/* Reads %s's DEEP view. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED int read_%s_deep( serialize_read_stream_t * stream, %sData_Deep * value )\n{\n",
		snake(d.Name), d.Name)
	for _, f := range deep {
		g.emitDeepReadField(f, "    ")
	}
	g.pf("    return 1;\n}\n\n")

	g.pf("/* Writes %s's SHALLOW view — the quantized wire. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED int write_%s_shallow( serialize_write_stream_t * stream, const %sData_Shallow * value )\n{\n",
		snake(d.Name), d.Name)
	for _, f := range interp {
		g.emitShallowWriteField(f, "    ")
	}
	g.pf("    return 1;\n}\n\n")

	g.pf("/* Reads %s's SHALLOW view. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED int read_%s_shallow( serialize_read_stream_t * stream, %sData_Shallow * value )\n{\n",
		snake(d.Name), d.Name)
	for _, f := range interp {
		g.emitShallowReadField(f, "    ")
	}
	g.pf("    return 1;\n}\n\n")
}

// emitShallowWriteField writes one field of the shallow (quantized) view.
func (g *gen) emitShallowWriteField(f *ir.Field, ind string) {
	if f.HasQuantize {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		lo, hi := quantBounds(f)
		bits := ir.BitsRequired(mustBig(lo), mustBig(hi))
		for _, comp := range st.Fields {
			member := fmt.Sprintf("value->%s_%s", f.Name, comp.Name)
			g.pf("%sif ( (serialize_int64_t) %s < %sLL || (serialize_int64_t) %s > %sLL )\n%s{\n%s    return 0;\n%s}\n",
				ind, member, lo, member, hi, ind, ind, ind)
			g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) ( %s - (%s) ), %d )", member, lo, bits))
		}
		return
	}
	if f.HasFloatRange {
		// already projected into the wire-int domain by Quantize; it rides as
		// the plain ranged integer it now is
		bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Steps))
		g.pf("%sif ( (serialize_int64_t) value->%s > %dLL )\n%s{\n%s    return 0;\n%s}\n",
			ind, f.Name, f.Steps, ind, ind, ind)
		g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) value->%s, %d )", f.Name, bits))
		return
	}
	g.emitWriteField(f, ind)
}

func (g *gen) emitShallowReadField(f *ir.Field, ind string) {
	if f.HasQuantize {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		lo, hi := quantBounds(f)
		bits := ir.BitsRequired(mustBig(lo), mustBig(hi))
		span := new(big.Int).Sub(mustBig(hi), mustBig(lo))
		for _, comp := range st.Fields {
			member := fmt.Sprintf("value->%s_%s", f.Name, comp.Name)
			g.pf("%s{\n%s    serialize_uint32_t raw = 0;\n", ind, ind)
			g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &raw, %d )", bits))
			g.pf("%s    if ( raw > %sU )\n%s    {\n%s        return 0;\n%s    }\n", ind, span.String(), ind, ind, ind)
			g.pf("%s    %s = (%s) ( (serialize_int64_t) raw + (%s) );\n%s}\n", ind, member, quantStorage(f), lo, ind)
		}
		return
	}
	if f.HasFloatRange {
		bits := ir.BitsRequired(big.NewInt(0), big.NewInt(f.Steps))
		g.pf("%s{\n%s    serialize_uint32_t raw = 0;\n", ind, ind)
		g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &raw, %d )", bits))
		g.pf("%s    if ( raw > %dU )\n%s    {\n%s        return 0;\n%s    }\n", ind, f.Steps, ind, ind, ind)
		g.pf("%s    value->%s = (%s) raw;\n%s}\n", ind, f.Name, projStorage(f.Steps), ind)
		return
	}
	g.emitReadField(f, ind)
}

func mustBig(s string) *big.Int {
	v, _ := new(big.Int).SetString(s, 10)
	if v == nil {
		return big.NewInt(0)
	}
	return v
}


// emitDeepWriteField writes one field of the DEEP view. A ranged float rides at
// FULL precision here: the deep view is full state for client-side prediction,
// so the compression that the shallow wire applies would be a lossy step with
// nothing to gain.
func (g *gen) emitDeepWriteField(f *ir.Field, ind string) {
	if f.HasFloatRange && f.Type.Kind == ir.TFloat32 {
		g.call(ind, fmt.Sprintf("serialize_write_float( stream, value->%s )", f.Name))
		return
	}
	g.emitWriteField(f, ind)
}

func (g *gen) emitDeepReadField(f *ir.Field, ind string) {
	if f.HasFloatRange && f.Type.Kind == ir.TFloat32 {
		g.call(ind, fmt.Sprintf("serialize_read_float( stream, &value->%s )", f.Name))
		return
	}
	g.emitReadField(f, ind)
}
