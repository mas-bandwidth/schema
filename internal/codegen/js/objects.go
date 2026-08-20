// Object-view emission (SPEC §4.8, §6.1 item 7): the State classes (one per
// context where context-scoped [local] fields exist), the Deep and Shallow
// wire classes' split Write/Read pairs, the Interpolate class, and the
// Quantize/Unquantize mapping between Interpolate and Shallow —
// floor(c*K + 0.5) per component in double math for float composites,
// round-to-nearest narrowing shifts with ties away from zero for fixed
// composites, straight copies for discrete and projected fields. State
// classes are simulation-only and get no wire functions. The wire is
// byte-identical to the other targets', construct by construct.
//
// The value-domain seam applies per VIEW storage, not per declared field: a
// BigInt deep component (a wide fixed) can narrow to Number shallow storage
// when the scaled bounds fit 32 bits, so the quantize pair converts domains
// exactly where C# casts between long and the small component types. The
// fixed-composite narrowing shift runs in Number arithmetic through
// Math.floor division below 2^53 (JavaScript's >> is 32-bit and would wrap
// where C#'s long arithmetic does not) and in BigInt shifts above.
package js

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/ir"
)

// view selects which storage a field emission derives (SPEC §4.8).
type view int

const (
	storageDeep    view = iota // declared storage — State and Data_Deep
	storageShallow             // quantized wire storage
	storageInterp              // interpolate storage: projected fields wire-int, composites continuous
)

func (g *gen) emitObject(d *ir.Object) {
	g.pf("// ---- object %s — one definition, a generated family per target (SPEC §4.8) ----\n\n", d.Name)

	if hasContextFields(d) {
		for _, ctx := range g.unit.Contexts {
			var fields []*ir.Field
			for _, f := range d.Fields {
				if f.Context == "" || f.Context == ctx {
					fields = append(fields, f)
				}
			}
			name := capitalize(ctx) + d.Name + "State"
			g.pf("// %s — the full simulation class for the %s context: every `all`\n", name, ctx)
			g.pf("// field plus the fields scoped [local, context = %s]\n", ctx)
			g.emitViewClass(name, fields, storageDeep)
		}
	} else {
		g.pf("// %sState — the full simulation class: every field\n", d.Name)
		g.emitViewClass(d.Name+"State", d.Fields, storageDeep)
	}

	deep, interp := splitObjectFields(d)

	g.pf("// %sData_Deep — every non-[local] field, deep encodings: full state for\n", d.Name)
	g.pf("// client-side prediction\n")
	g.emitViewClass(d.Name+"Data_Deep", deep, storageDeep)

	g.pf("// %sData_Shallow — the [interpolate] fields on the quantized wire: the\n", d.Name)
	g.pf("// implementation detail on the way to interpolation on the client\n")
	g.emitViewClass(d.Name+"Data_Shallow", interp, storageShallow)

	g.pf("// %sData_Interpolate — the same fields in interpolate storage: projected\n", d.Name)
	g.pf("// fields stay in the wire integer domain and snap-interpolate; quantized\n")
	g.pf("// composites store continuous (SPEC §4.8 rule 5)\n")
	g.emitViewClass(d.Name+"Data_Interpolate", interp, storageInterp)
}

func (g *gen) emitViewClass(name string, fields []*ir.Field, v view) {
	g.pf("export class %s {\n  constructor() {\n", name)
	if len(fields) == 0 {
		g.pf("    // empty body — no fields under this view\n")
	}
	for _, f := range fields {
		g.emitViewStorageField(f, v)
	}
	g.pf("  }\n}\n\n")
}

func splitObjectFields(d *ir.Object) (deep, interp []*ir.Field) {
	for _, f := range d.Fields {
		if !f.Local {
			deep = append(deep, f)
		}
		if f.Interpolate {
			interp = append(interp, f)
		}
	}
	return
}

func hasContextFields(d *ir.Object) bool {
	for _, f := range d.Fields {
		if f.Context != "" {
			return true
		}
	}
	return false
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

// emitViewStorageField emits one field's storage under a view: quantized
// composites and projected floats replace their declared storage on the
// shallow (and, for projected floats, interpolate) views; everything else
// keeps it.
func (g *gen) emitViewStorageField(f *ir.Field, v view) {
	name := ir.GoExportName(f.Name)
	if v == storageShallow && f.HasQuantize {
		st := f.Type.Ref.(*ir.Struct)
		if f.FixedShallow {
			g.pf("    // %s: %s narrowed to %d fractional bits (quantize = %s) — per-component\n",
				f.Name, f.Type.Name, f.QuantShift, ir.RenderExpr(f.QuantScaleExpr))
			g.pf("    // quantized units; bounds are the component's whole-unit [min, max] scaled\n")
			for _, comp := range st.Fields {
				lo, hi, width := fixedShallowComp(f, comp)
				g.pf("    this.%s%s = %s; // in [%s, %s]\n",
					name, ir.GoExportName(comp.Name), zeroForWidth(width), lo, hi)
			}
			return
		}
		g.pf("    // %s: %s quantized by %s, max %s — per-component int in [-%d, %d]\n",
			f.Name, f.Type.Name, ir.RenderExpr(f.QuantScaleExpr), ir.RenderExpr(f.QuantMaxExpr), f.QuantBound, f.QuantBound)
		for _, comp := range st.Fields {
			g.pf("    this.%s%s = %s;\n", name, ir.GoExportName(comp.Name), zeroForWidth(smallestSigned(f.QuantBound)))
		}
		return
	}
	if (v == storageShallow || v == storageInterp) && f.HasFloatRange {
		note := ""
		if f.Round != "nearest" {
			note = ", round " + f.Round
		}
		tail := ""
		if v == storageInterp {
			tail = " — wire-int domain, snap-interpolated (SPEC §4.8 rule 5)"
		}
		g.pf("    this.%s = %s; // float [%s, %s] @ resolution %s -> wire int [0, %d]%s%s\n",
			name, zeroForWidth(ir.StorageBitsFor(f.Steps)), formatFloat(f.FMin), formatFloat(f.FMax),
			formatFloat(f.Resolution), f.Steps, note, tail)
		return
	}
	g.emitStorageField(f)
}

// zeroForWidth is the zero literal of a view component's storage width — the
// Number/BigInt seam applied to view storage.
func zeroForWidth(width int) string {
	if width > 32 {
		return "0n"
	}
	return "0"
}

func (g *gen) emitObjectFunctions(d *ir.Object) {
	// view field lists embed at unknown alignment — no bulk-bytes marks here
	// (ir.AlignedFixedByteArrays is a per-struct proof; unknown never optimizes)
	g.bulkBytes = nil

	deep, interp := splitObjectFields(d)

	deepName := d.Name + "Data_Deep"
	deepBits := ir.MaxBitsView(deep, ir.ViewDeep)
	g.pf("export const %sMaxBits = %d;\n", deepName, deepBits)
	g.pf("export const %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", deepName, ir.MaxBytes(deepBits))

	g.pf("export function Write%s(stream, value) {\n", deepName)
	for _, f := range deep {
		g.emitViewWriteField(f, ir.ViewDeep, "  ")
	}
	g.pf("  return true;\n}\n\n")
	g.pf("export function Read%s(stream, value) {\n", deepName)
	for _, f := range deep {
		g.emitViewReadField(f, ir.ViewDeep, "  ")
	}
	g.pf("  return true;\n}\n\n")

	shName := d.Name + "Data_Shallow"
	shBits := ir.MaxBitsView(interp, ir.ViewShallow)
	g.pf("export const %sMaxBits = %d;\n", shName, shBits)
	g.pf("export const %sMaxBytes = %d; // rounded up to the 8-byte write-buffer granularity\n\n", shName, ir.MaxBytes(shBits))

	g.pf("export function Write%s(stream, value) {\n", shName)
	for _, f := range interp {
		g.emitViewWriteField(f, ir.ViewShallow, "  ")
	}
	g.pf("  return true;\n}\n\n")
	g.pf("export function Read%s(stream, value) {\n", shName)
	for _, f := range interp {
		g.emitViewReadField(f, ir.ViewShallow, "  ")
	}
	g.pf("  return true;\n}\n\n")

	// ---- Quantize / Unquantize: the Interpolate <-> Shallow mapping pair
	// (SPEC §4.8's artifact table — the hand-written Quantize(), generated).
	// NOT emitted when every [interpolate] field is already wire-domain —
	// fixed components are their own quantization (SPEC §4.8).
	if ir.ObjectNeedsQuantize(d) {
		inName := d.Name + "Data_Interpolate"
		g.pf("// Quantize%s maps %s into %s — the\n", d.Name, inName, shName)
		g.pf("// hand-written Quantize(), generated (SPEC §4.8).\n")
		g.pf("export function Quantize%s(input, output) {\n", d.Name)
		for _, f := range interp {
			g.emitQuantizeField(f, "  ")
		}
		g.pf("}\n\n")
		g.pf("// Unquantize%s maps %s back into %s.\n", d.Name, shName, inName)
		g.pf("export function Unquantize%s(input, output) {\n", d.Name)
		for _, f := range interp {
			g.emitUnquantizeField(f, "  ")
		}
		g.pf("}\n\n")
	}
}

// fixedShallowComp resolves one component of a narrowed fixed composite
// (SPEC §4.8 rule 2b) to its JS shallow shape: wire bounds and the storage
// width (Number at 32 bits or fewer, BigInt at 64). The bounds mirror
// ir.FixedShallowBounds so all six backends agree on the wire.
func fixedShallowComp(f, cf *ir.Field) (lo, hi *big.Int, width int) {
	lo, hi = ir.FixedShallowBounds(f, cf)
	abs := new(big.Int).Neg(lo)
	if abs.Cmp(hi) < 0 {
		abs = hi
	}
	bound := int64(9223372036854775807)
	if abs.IsInt64() {
		bound = abs.Int64()
	}
	width = smallestSigned(bound)
	return
}

func smallestSigned(bound int64) int {
	switch {
	case bound <= 127:
		return 8
	case bound <= 32767:
		return 16
	case bound <= 2147483647:
		return 32
	default:
		return 64
	}
}

// emitViewRangedWrite is the shallow write fold for a view component: the
// generated guard plus offset bits, in the storage's value domain.
func (g *gen) emitViewRangedWrite(name string, lo, hi *big.Int, storageBig bool, ind string) {
	const refuse = " // out-of-contract writes are refused, not wrapped"
	if storageBig {
		sMin, sMax := storageBoundsBig(ir.FieldType{Kind: ir.TInt, Signed: true, Width: 64})
		g.emitWriteFoldedBig(name, bigLit(lo), bigLit(hi), lo, hi,
			lo.Cmp(sMin) > 0, hi.Cmp(sMax) < 0, refuse, ind)
		return
	}
	g.emitWriteFoldedNum(name, lo.String(), hi.String(), lo, hi, true, refuse, ind)
}

// emitViewRangedRead is the shallow read for a view component: the runtime
// ranged call in the family the bounds pick, then the storage-domain assign.
func (g *gen) emitViewRangedRead(name string, lo, hi *big.Int, storageBig bool, ind string) {
	if lo.Cmp(hi) == 0 {
		// a degenerate component narrows to zero bits — the value is the
		// range (SPEC §4.6, decided 2026-08-15)
		if storageBig {
			g.pf("%s%s = %s;\n", ind, name, bigLit(lo))
		} else {
			g.pf("%s%s = %s;\n", ind, name, lo.String())
		}
		return
	}
	if storageBig {
		scratch := g.bigScratch()
		g.call(ind, fmt.Sprintf("stream.serializeInt64(%s, %s, %s)", scratch, bigLit(lo), bigLit(hi)), "")
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
		return
	}
	if intRangePath(lo, hi) == "int32" {
		scratch := g.numScratch()
		g.call(ind, fmt.Sprintf("stream.serializeInt(%s, %s, %s)", scratch, lo.String(), hi.String()), "")
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
		return
	}
	// Number storage whose bounds escape int32 (a projected float with more
	// than 2^31 steps): the int64 call family with BigInt bounds, narrowed
	// back — the decoded value is inside [lo, hi], so the narrowing is exact
	scratch := g.bigScratch()
	g.call(ind, fmt.Sprintf("stream.serializeInt64(%s, %s, %s)", scratch, bigLit(lo), bigLit(hi)), "")
	g.pf("%s%s = Number(%s.value);\n", ind, name, scratch)
}

// emitViewWriteField emits one field of a view wire function. Quantized
// components and projected floats fold their bounds at generation time — the
// same lever as ranged integers (see functions.go), byte-identical wire.
func (g *gen) emitViewWriteField(f *ir.Field, v ir.View, ind string) {
	name := "value." + ir.GoExportName(f.Name)
	switch {
	case v == ir.ViewShallow && f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			lo, hi, width := fixedShallowComp(f, comp)
			g.emitViewRangedWrite(name+ir.GoExportName(comp.Name), lo, hi, width > 32, ind)
		}
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		qb := big.NewInt(f.QuantBound)
		nqb := new(big.Int).Neg(qb)
		wide := smallestSigned(f.QuantBound) > 32
		for _, comp := range st.Fields {
			g.emitViewRangedWrite(name+ir.GoExportName(comp.Name), nqb, qb, wide, ind)
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		g.emitViewRangedWrite(name, big.NewInt(0), big.NewInt(f.Steps),
			ir.StorageBitsFor(f.Steps) > 32, ind)
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		// the triple describes the shallow wire only — deep is the bare float
		scratch := g.numScratch()
		g.pf("%s%s.value = %s;\n", ind, scratch, name)
		if f.Type.Kind == ir.TFloat64 {
			g.call(ind, fmt.Sprintf("stream.serializeDouble(%s)", scratch), "")
		} else {
			g.call(ind, fmt.Sprintf("stream.serializeFloat(%s)", scratch), "")
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
			lo, hi, width := fixedShallowComp(f, comp)
			g.emitViewRangedRead(name+ir.GoExportName(comp.Name), lo, hi, width > 32, ind)
		}
	case v == ir.ViewShallow && f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		qb := big.NewInt(f.QuantBound)
		nqb := new(big.Int).Neg(qb)
		wide := smallestSigned(f.QuantBound) > 32
		for _, comp := range st.Fields {
			g.emitViewRangedRead(name+ir.GoExportName(comp.Name), nqb, qb, wide, ind)
		}
	case v == ir.ViewShallow && f.HasFloatRange:
		g.emitViewRangedRead(name, big.NewInt(0), big.NewInt(f.Steps),
			ir.StorageBitsFor(f.Steps) > 32, ind)
	case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
		scratch := g.numScratch()
		if f.Type.Kind == ir.TFloat64 {
			g.call(ind, fmt.Sprintf("stream.serializeDouble(%s)", scratch), "")
		} else {
			g.call(ind, fmt.Sprintf("stream.serializeFloat(%s)", scratch), "")
		}
		g.pf("%s%s = %s.value;\n", ind, name, scratch)
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
			_, _, width := fixedShallowComp(f, comp)
			deepBig := comp.Type.Width > 32
			shallowBig := width > 32
			src := fmt.Sprintf("input.%s.%s", name, compName)
			if drop == 0 {
				g.pf("%soutput.%s%s = %s;\n", ind, name, compName, convertDomain(src, deepBig, shallowBig))
				continue
			}
			// round-to-nearest narrowing shift — ties AWAY FROM ZERO: the one
			// fixed-point rounding rule (SPEC §4.8, decided 2026-08-15).
			// Negative raws mirror through negation so the tie leaves zero in
			// both signs. In-bounds raws cannot overflow the add (checker-
			// enforced bounds leave 2^(F-1) of headroom past any legal raw).
			// Number-domain raws shift via Math.floor division — JS >> is
			// 32-bit and would wrap where the siblings' 64-bit arithmetic
			// does not; below 2^53 the division is exact and identical.
			if deepBig {
				half := new(big.Int).Lsh(big.NewInt(1), uint(drop-1))
				expr := fmt.Sprintf("raw >= 0n ? (raw + %s) >> %dn : -((-raw + %s) >> %dn)",
					bigLit(half), drop, bigLit(half), drop)
				g.pf("%s{\n%s  const raw = %s;\n", ind, ind, src)
				g.pf("%s  output.%s%s = %s;\n%s}\n", ind, name, compName, convertDomain(expr, true, shallowBig), ind)
			} else {
				half := int64(1) << (drop - 1)
				pow := int64(1) << drop
				expr := fmt.Sprintf("raw >= 0 ? Math.floor((raw + %d) / %d) : -Math.floor((-raw + %d) / %d)",
					half, pow, half, pow)
				g.pf("%s{\n%s  const raw = %s;\n", ind, ind, src)
				g.pf("%s  output.%s%s = %s;\n%s}\n", ind, name, compName, convertDomain(expr, false, shallowBig), ind)
			}
		}
	case f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		wide := smallestSigned(f.QuantBound) > 32
		scale := g.renderNum(f.QuantScaleExpr, big.NewInt(f.QuantScale))
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			// double math regardless of component width — JS numbers ARE
			// doubles and per-op IEEE rounding is the language semantics (no
			// FMA can fuse the product into the + 0.5). The clamp happens in
			// the DOUBLE domain before any integer conversion, so garbage
			// input quantizes identically in every target (NaN clamps low).
			g.pf("%s{\n%s  const quantizedValue = Math.floor(input.%s.%s * %s + 0.5);\n",
				ind, ind, name, compName, scale)
			if wide {
				g.pf("%s  let componentValue = -%dn;\n", ind, f.QuantBound)
				g.pf("%s  if (quantizedValue > %d.0) {\n%s    componentValue = %dn;\n", ind, f.QuantBound, ind, f.QuantBound)
				g.pf("%s  } else if (quantizedValue >= -%d.0) {\n%s    componentValue = BigInt(quantizedValue);\n%s  }\n",
					ind, f.QuantBound, ind, ind)
			} else {
				g.pf("%s  let componentValue = -%d;\n", ind, f.QuantBound)
				g.pf("%s  if (quantizedValue > %d.0) {\n%s    componentValue = %d;\n", ind, f.QuantBound, ind, f.QuantBound)
				g.pf("%s  } else if (quantizedValue >= -%d.0) {\n%s    componentValue = quantizedValue;\n%s  }\n",
					ind, f.QuantBound, ind, ind)
			}
			g.pf("%s  output.%s%s = componentValue;\n%s}\n", ind, name, compName, ind)
		}
	default:
		g.emitCopyField("output", "input", f, ind, 0)
	}
}

// emitUnquantizeField maps one Shallow field back into Interpolate:
// composites divide by the scale (or shift back, for narrowed fixed);
// everything else copies.
func (g *gen) emitUnquantizeField(f *ir.Field, ind string) {
	name := ir.GoExportName(f.Name)
	switch {
	case f.HasQuantize && f.FixedShallow:
		st := f.Type.Ref.(*ir.Struct)
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			drop := comp.Type.FracBits - f.QuantShift
			_, _, width := fixedShallowComp(f, comp)
			deepBig := comp.Type.Width > 32
			shallowBig := width > 32
			src := fmt.Sprintf("input.%s%s", name, compName)
			switch {
			case drop == 0:
				g.pf("%soutput.%s.%s = %s;\n", ind, name, compName, convertDomain(src, shallowBig, deepBig))
			case deepBig:
				// the left shift back, in BigInt (the deep raw domain)
				g.pf("%soutput.%s.%s = %s << %dn;\n", ind, name, compName, convertDomain(src, shallowBig, true), drop)
			default:
				// Number domain: multiply instead of << (JS shifts are 32-bit);
				// in-contract raws stay exact below 2^53
				g.pf("%soutput.%s.%s = %s * %d;\n", ind, name, compName, convertDomain(src, shallowBig, false), int64(1)<<drop)
			}
		}
	case f.HasQuantize:
		st := f.Type.Ref.(*ir.Struct)
		wide := smallestSigned(f.QuantBound) > 32
		for _, comp := range st.Fields {
			compName := ir.GoExportName(comp.Name)
			src := convertDomain(fmt.Sprintf("input.%s%s", name, compName), wide, false)
			if comp.Type.Kind == ir.TFloat32 {
				// float32 division exactly: fround the operands, divide in
				// double, fround the quotient — double rounding is innocuous
				// at this precision split, so this IS f32 division
				g.pf("%soutput.%s.%s = Math.fround(%s / %s);\n",
					ind, name, compName, src, formatFloat32(float64(f.QuantScale)))
			} else {
				g.pf("%soutput.%s.%s = %s / %s;\n",
					ind, name, compName, src, g.renderNum(f.QuantScaleExpr, big.NewInt(f.QuantScale)))
			}
		}
	default:
		g.emitCopyField("output", "input", f, ind, 0)
	}
}

// convertDomain wraps expr with the Number/BigInt conversion the source and
// destination storage domains require; in-contract values convert exactly.
func convertDomain(expr string, srcBig, dstBig bool) string {
	switch {
	case srcBig && !dstBig:
		return "Number(" + expr + ")"
	case !srcBig && dstBig:
		return "BigInt(" + expr + ")"
	}
	return expr
}

// emitCopyField copies one non-quantized field between view instances. JS
// classes and arrays are references, so buffers copy element-wise into the
// destination's pre-allocated storage and composed classes copy member-wise
// (recursion ends because composition cycles are compile errors); depth
// uniquifies nested loop variables.
func (g *gen) emitCopyField(dstPrefix, srcPrefix string, f *ir.Field, ind string, depth int) {
	base := ir.GoExportName(f.Name)
	dst := dstPrefix + "." + base
	src := srcPrefix + "." + base
	iv := loopVar(depth)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("%s%s.set(%s);\n", ind, dst, src)
		g.pf("%s%sLength = %sLength;\n", ind, dst, src)
	case f.Array != ir.ArrayNone:
		if st, isStruct := f.Type.Ref.(*ir.Struct); isStruct && f.Type.Kind == ir.TNamed {
			g.pf("%sfor (let %s = 0; %s < %s.length; %s++) {\n", ind, iv, iv, src, iv)
			g.emitCopyStruct(dst+"["+iv+"]", src+"["+iv+"]", st, ind+"  ", depth+1)
			g.pf("%s}\n", ind)
		} else if isByteElem(f.Type) {
			g.pf("%s%s.set(%s);\n", ind, dst, src)
		} else {
			g.pf("%sfor (let %s = 0; %s < %s.length; %s++) {\n", ind, iv, iv, src, iv)
			g.pf("%s  %s[%s] = %s[%s];\n%s}\n", ind, dst, iv, src, iv, ind)
		}
		if f.Array == ir.ArrayCounted {
			g.pf("%s%sCount = %sCount;\n", ind, dst, src)
		}
	default:
		if st, isStruct := f.Type.Ref.(*ir.Struct); isStruct && f.Type.Kind == ir.TNamed {
			g.emitCopyStruct(dst, src, st, ind, depth)
			return
		}
		g.pf("%s%s = %s;\n", ind, dst, src)
	}
}

func (g *gen) emitCopyStruct(dst, src string, st *ir.Struct, ind string, depth int) {
	for _, f := range st.Fields {
		g.emitCopyField(dst, src, f, ind, depth)
	}
}

func loopVar(depth int) string {
	if depth == 0 {
		return "i"
	}
	return fmt.Sprintf("i%d", depth+1)
}
