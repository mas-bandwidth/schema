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

	"github.com/mas-bandwidth/schema/ir"
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

	g.emitQuantizePair(d)
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
	// A narrowed fixed composite (SPEC §4.8 rule 2b) becomes per-component
	// signed integers keeping QuantShift fractional bits; each component's
	// storage holds its own whole-unit bounds scaled by QuantScale.
	if v == storageShallow && f.HasQuantize && f.FixedShallow {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		g.pf("    /* %s: %s narrowed — %d fractional bits kept (SPEC §4.8 rule 2b) */\n", f.Name, f.Type.Name, f.QuantShift)
		for _, comp := range st.Fields {
			lo, hi, _, typ := fixedShallowComp(f, comp)
			g.pf("    %s %s_%s; /* [%s, %s] */\n", typ, f.Name, comp.Name, lo, hi)
		}
		return
	}
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

// fixedShallowComp resolves one component of a narrowed fixed composite
// (SPEC §4.8 rule 2b) to its C shallow shape: wire bounds, wire bits and
// storage type. Bounds come from ir.FixedShallowBounds — the one derivation
// all five backends share, or the five wires disagree.
func fixedShallowComp(f, cf *ir.Field) (lo, hi *big.Int, bits int64, typ string) {
	lo, hi = ir.FixedShallowBounds(f, cf)
	bits = ir.BitsRequired(lo, hi)
	abs := new(big.Int).Neg(lo)
	if abs.Cmp(hi) < 0 {
		abs = hi
	}
	bound := int64(9223372036854775807)
	if abs.IsInt64() {
		bound = abs.Int64()
	}
	switch {
	case bound <= 0x7F:
		typ = "int8_t"
	case bound <= 0x7FFF:
		typ = "int16_t"
	case bound <= 0x7FFFFFFF:
		typ = "int32_t"
	default:
		typ = "int64_t"
	}
	return
}

// ---- the wire header ----

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

func (g *gen) emitObjectFunctions(d *ir.Object) {
	// no bulk-bytes marks for a view: the analysis walks a struct's ITEMS,
	// and a view's field set is assembled from the object declaration, so no
	// view field's position is statically proven. The per-byte loop is the
	// correct emission for every one of them — same as the C++ backend.
	g.bulkBytes = nil

	deep, interp := splitObjectFields(d)

	g.pf("/* Writes %s's DEEP view — the declared encodings. */\n", d.Name)
	// the function name derives from the VIEW STRUCT name, exactly like the
	// other four backends (WriteShipData_Deep -> write_ship_data_deep) — the
	// object-name form this used to emit (write_ship_deep) lived outside the
	// claimed-name registry and could collide with a user type (#23)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_WRITE_INLINE int write_%s( serialize_write_stream_t * stream, const %sData_Deep * value )\n{\n",
		snake(d.Name+"Data_Deep"), d.Name)
	mark := g.body.Len()
	for _, f := range deep {
		g.emitDeepWriteField(f, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return 1;\n}\n\n")

	g.pf("/* Reads %s's DEEP view. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_READ_INLINE int read_%s( serialize_read_stream_t * stream, %sData_Deep * value )\n{\n",
		snake(d.Name+"Data_Deep"), d.Name)
	mark = g.body.Len()
	for _, f := range deep {
		g.emitDeepReadField(f, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return 1;\n}\n\n")

	g.pf("/* Writes %s's SHALLOW view — the quantized wire. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_WRITE_INLINE int write_%s( serialize_write_stream_t * stream, const %sData_Shallow * value )\n{\n",
		snake(d.Name+"Data_Shallow"), d.Name)
	mark = g.body.Len()
	for _, f := range interp {
		g.emitShallowWriteField(f, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return 1;\n}\n\n")

	g.pf("/* Reads %s's SHALLOW view. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED SCHEMA_C_READ_INLINE int read_%s( serialize_read_stream_t * stream, %sData_Shallow * value )\n{\n",
		snake(d.Name+"Data_Shallow"), d.Name)
	mark = g.body.Len()
	for _, f := range interp {
		g.emitShallowReadField(f, "    ")
	}
	g.voidIfEmpty(mark, "stream", "value")
	g.pf("    return 1;\n}\n\n")
}

// emitShallowWriteField writes one field of the shallow (quantized) view.
func (g *gen) emitShallowWriteField(f *ir.Field, ind string) {
	if f.HasQuantize && f.FixedShallow {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		for _, comp := range st.Fields {
			member := fmt.Sprintf("value->%s_%s", f.Name, comp.Name)
			lo, hi, bits, _ := fixedShallowComp(f, comp)
			// refuse an out-of-range write, then the folded offset write —
			// the same shape the ranged-int path uses (>32-bit widths split
			// low 32 bits first, per the wire model)
			g.pf("%sif ( (serialize_int64_t) %s < %s || (serialize_int64_t) %s > %s )\n%s{\n%s    return 0;\n%s}\n",
				ind, member, intLit(lo, "LL"), member, intLit(hi, "LL"), ind, ind, ind)
			if bits == 0 {
				// a degenerate component narrows to zero bits — the refusal
				// above is the whole write (SPEC §4.6, decided 2026-08-15)
				continue
			}
			if bits <= 32 {
				g.call(ind, fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) ( (serialize_int64_t) %s - (%s) ), %d )", member, intLit(lo, "LL"), bits))
			} else {
				g.pf("%s{\n%s    serialize_uint64_t offset_value = (serialize_uint64_t) ( (serialize_int64_t) %s - (%s) );\n", ind, ind, member, intLit(lo, "LL"))
				g.call(ind+"    ", "serialize_write_bits( stream, (serialize_uint32_t) ( offset_value & 0xFFFFFFFFu ), 32 )")
				g.call(ind+"    ", fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) ( offset_value >> 32 ), %d )", bits-32))
				g.pf("%s}\n", ind)
			}
		}
		return
	}
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
	if f.HasQuantize && f.FixedShallow {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		for _, comp := range st.Fields {
			member := fmt.Sprintf("value->%s_%s", f.Name, comp.Name)
			lo, hi, bits, typ := fixedShallowComp(f, comp)
			span := new(big.Int).Sub(hi, lo)
			if bits == 0 {
				// a degenerate component narrows to zero bits — the value is
				// the range (SPEC §4.6, decided 2026-08-15)
				g.pf("%s%s = (%s) %s;\n", ind, member, typ, intLit(lo, "LL"))
				continue
			}
			if bits <= 32 {
				g.pf("%s{\n%s    serialize_uint32_t raw = 0;\n", ind, ind)
				g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &raw, %d )", bits))
				g.pf("%s    if ( raw > %sU )\n%s    {\n%s        return 0;\n%s    }\n", ind, span.String(), ind, ind, ind)
				g.pf("%s    %s = (%s) ( (serialize_int64_t) raw + (%s) );\n%s}\n", ind, member, typ, intLit(lo, "LL"), ind)
			} else {
				// low 32 first, then the remainder — the same split the
				// ranged 64-bit read uses; reject, never clamp
				g.pf("%s{\n%s    serialize_uint32_t lo = 0;\n%s    serialize_uint32_t hi = 0;\n%s    serialize_uint64_t raw;\n", ind, ind, ind, ind)
				g.call(ind+"    ", "serialize_read_bits( stream, &lo, 32 )")
				g.call(ind+"    ", fmt.Sprintf("serialize_read_bits( stream, &hi, %d )", bits-32))
				g.pf("%s    raw = (serialize_uint64_t) lo | ( ( (serialize_uint64_t) hi ) << 32 );\n", ind)
				g.pf("%s    if ( raw > %sULL )\n%s    {\n%s        return 0;\n%s    }\n", ind, span.String(), ind, ind, ind)
				g.pf("%s    %s = (%s) ( (serialize_int64_t) raw + (%s) );\n%s}\n", ind, member, typ, intLit(lo, "LL"), ind)
			}
		}
		return
	}
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

// ---- Quantize / Unquantize ----
//
// The pair that moves between the interpolate view (the domain the simulation
// works in) and the shallow view (the quantized wire). Both are pure struct
// transforms: no stream, no failure mode.

func (g *gen) emitQuantizePair(d *ir.Object) {
	if !ir.ObjectNeedsQuantize(d) {
		// every [interpolate] field rides the wire domain verbatim — fixed
		// components are their own quantization (SPEC §4.8) — so the pair
		// would be a pure member copy and is NOT emitted. The same rule,
		// the same words, as the other four backends.
		g.pf("/* Quantize%s/Unquantize%s are not emitted: every [interpolate] field\n", d.Name, d.Name)
		g.pf("   is already wire-domain (fixed components are their own quantization,\n")
		g.pf("   SPEC §4.8) — Interpolate and Shallow are the same values. */\n\n")
		return
	}
	_, interp := splitObjectFields(d)

	g.pf("/* Quantize%s — the interpolate domain to the quantized wire domain. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED void quantize_%s( const %sData_Interpolate * input, %sData_Shallow * output )\n{\n",
		snake(d.Name), d.Name, d.Name)
	mark := g.body.Len()
	for _, f := range interp {
		g.emitQuantizeField(f)
	}
	if g.body.Len() == mark {
		// a quantized property over a type with no numeric components emits
		// no statements; keep -Werror consumers building (found by
		// FuzzGeneratedCompiles, issue #22)
		g.pf("    (void) input; /* no numeric components to quantize */\n    (void) output;\n")
	}
	g.pf("}\n\n")

	g.pf("/* Unquantize%s — the quantized wire domain back to the interpolate domain. */\n", d.Name)
	g.pf("static SCHEMA_UNUSED void unquantize_%s( const %sData_Shallow * input, %sData_Interpolate * output )\n{\n",
		snake(d.Name), d.Name, d.Name)
	mark = g.body.Len()
	for _, f := range interp {
		g.emitUnquantizeField(f)
	}
	if g.body.Len() == mark {
		g.pf("    (void) input; /* no numeric components to unquantize */\n    (void) output;\n")
	}
	g.pf("}\n\n")
}

func (g *gen) emitQuantizeField(f *ir.Field) {
	if !f.HasQuantize {
		// projected floats and discrete fields copy across unchanged — the
		// projection already happened in the interpolate view's storage
		g.pf("    output->%s = input->%s;\n", f.Name, f.Name)
		return
	}
	if f.FixedShallow {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		for _, comp := range st.Fields {
			drop := comp.Type.FracBits - f.QuantShift
			_, _, _, typ := fixedShallowComp(f, comp)
			if drop == 0 {
				g.pf("    output->%s_%s = (%s) input->%s.%s;\n", f.Name, comp.Name, typ, f.Name, comp.Name)
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
			g.pf("    {\n        serialize_int64_t raw = (serialize_int64_t) input->%s.%s;\n", f.Name, comp.Name)
			g.pf("        output->%s_%s = (%s) ( raw >= 0 ? ( raw + %dLL ) >> %d : -( ( -raw + %dLL ) >> %d ) );\n",
				f.Name, comp.Name, typ, half, drop, half, drop)
			g.pf("    }\n")
		}
		return
	}
	st, ok := f.Type.Ref.(*ir.Struct)
	if !ok {
		g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
		return
	}
	bound := f.QuantBound
	for _, comp := range st.Fields {
		g.pf("    {\n")
		// the product lands in a named local BEFORE the + 0.5, so FP_CONTRACT
		// cannot fuse the multiply into the add — the compressed_float FMA
		// hazard one level up, defended the way serialize.c defends its
		// writer (#26)
		g.pf("        double scaled_value = (double) input->%s.%s * (double) ( %s );\n",
			f.Name, comp.Name, g.renderInt(f.QuantScaleExpr, big.NewInt(f.QuantScale)))
		g.pf("        double quantized_value = floor( scaled_value + 0.5 );\n")
		// clamp to the component range, in the same order C++ does so the
		// boundary cases land identically
		g.pf("        serialize_int64_t component_value = %dLL;\n", -bound)
		g.pf("        if ( quantized_value > %s )\n        {\n", formatFloat(float64(bound)))
		g.pf("            component_value = %dLL;\n        }\n", bound)
		g.pf("        else if ( quantized_value >= %s )\n        {\n", formatFloat(float64(-bound)))
		g.pf("            component_value = (serialize_int64_t) quantized_value;\n        }\n")
		g.pf("        output->%s_%s = (%s) component_value;\n", f.Name, comp.Name, quantStorage(f))
		g.pf("    }\n")
	}
}

func (g *gen) emitUnquantizeField(f *ir.Field) {
	if !f.HasQuantize {
		g.pf("    output->%s = input->%s;\n", f.Name, f.Name)
		return
	}
	if f.FixedShallow {
		st, ok := f.Type.Ref.(*ir.Struct)
		if !ok {
			g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
			return
		}
		for _, comp := range st.Fields {
			drop := comp.Type.FracBits - f.QuantShift
			if drop == 0 {
				g.pf("    output->%s.%s = (%s) input->%s_%s;\n", f.Name, comp.Name, g.storageType(comp), f.Name, comp.Name)
				continue
			}
			// the left shift back — dropped bits return as zeros
			g.pf("    output->%s.%s = (%s) ( (serialize_int64_t) input->%s_%s << %d );\n",
				f.Name, comp.Name, g.storageType(comp), f.Name, comp.Name, drop)
		}
		return
	}
	st, ok := f.Type.Ref.(*ir.Struct)
	if !ok {
		g.unsupported("field %s is [quantize]d but does not reference a composite type", f.Name)
		return
	}
	for _, comp := range st.Fields {
		g.pf("    output->%s.%s = (double) input->%s_%s / (double) ( %s );\n",
			f.Name, comp.Name, f.Name, comp.Name, g.renderInt(f.QuantScaleExpr, big.NewInt(f.QuantScale)))
	}
}
