package cstable

// THE READ-SIDE BOUNDS PASS (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.3 step 11).
//
// A record is a positional image and the one read loop moves bytes: it asks
// nothing about what they mean. Two things a declaration bounds are held by
// nobody in §4.5 — a RANGED SCALAR's min and max, and an ORDINAL's set: a union
// tag past the arm count, an enum ordinal past the enum's top value. They are
// held HERE, as straight-line code AFTER the plan run, and NEVER as plan
// entries, which would be a test per bounded field on every read of every
// record and is the cost the identity plan exists to avoid.
//
// THE PASS RUNS OVER STORAGE, which is what makes ONE pass cover BOTH plans:
// the identity plan and a plan compiled from a stranger's layout land values in
// the same places, so a compiled plan needs no op of its own. It walks only what
// a read can have written — a counted array's LIVE elements and never its slack,
// an optional's payload only when the present byte says so — because the
// prefill's declared defaults are in range by construction, and clamping storage
// nobody wrote would count a clamp on every clean read.
//
// A type that bounds nothing emits no pass at all (§2.2's zero-cost rule), and a
// clamp that cannot fire is not emitted: an end sitting on its width's limit
// (tableClampEnds) and an ordinal whose extent FILLS its storage width never
// clamped, so the counter they would have moved never moved either (§5.4).

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// csOrdinalFillsStorage reports whether an ordinal's extent is the LARGEST value
// its storage width holds — 255 variants in a byte, 65535 in two. The read-side
// clamp for such an ordinal is `> extent` against a value of that very width,
// which no value satisfies, so the emitter drops it: the same "this check cannot
// fire" test tableClampEnds applies to a ranged scalar's end.
func csOrdinalFillsStorage(max int64, storageBits int) bool {
	if storageBits <= 0 || storageBits >= 64 {
		return false
	}
	return uint64(max) >= uint64(1)<<uint(storageBits)-1
}

// emitFixedClampBodyFn is ONE type's bounds, the twin of its write body: a
// nested type is a call and not an inlining, so a type that appears twice in a
// closure is spelled once.
func (g *tableGen) emitFixedClampBodyFn(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("// %s's read-side bounds (§4.6).\n", st.Name)
	g.pf("public static void %sFixedClampBody(%s value, ref int clamped)\n{\n", st.Name, st.Name)
	g.pf("    if (value == null) return;\n")
	for _, f := range st.Fields {
		g.emitFixedClampField(st, f, "value", 4)
	}
	g.pf("}\n\n")
}

// emitFixedClamp is the ROOT's entry point: the one call a read makes, and the
// one place the count reaches the report.
func (g *tableGen) emitFixedClamp(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("// THE READ-SIDE BOUNDS (§4.6): a ranged scalar's declared min and max,\n")
	g.pf("// and an ORDINAL's set — a union tag past the arm count, an enum ordinal\n")
	g.pf("// past the enum's top value. Straight-line, after the run, over STORAGE,\n")
	g.pf("// so the identity plan and a plan compiled from a stranger's layout are\n")
	g.pf("// held to the same numbers by the same pass. Every clamp COUNTS.\n")
	g.pf("public static void %sFixedClamp(%s value, TableReport report)\n{\n", st.Name, st.Name)
	g.pf("    int clamped = 0;\n")
	g.pf("    %sFixedClampBody(value, ref clamped);\n", st.Name)
	g.pf("    if (report != null) { report.Clamped += clamped; }\n")
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedClampField(owner *ir.Struct, f *ir.Field, val string, indent int) {
	if !ir.TableFixedClampNeededField(f) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS IGNORED ON READ (§4.6), so it is not
		// held to a bound either: what rides there is zero from the template
		// and nobody wrote it.
		g.pf("%sif (%s.%sPresent)\n%s{\n", ind, val, member(f), ind)
		g.emitFixedClampPayload(owner, f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedClampPayload(owner, f, val, indent)
}

func (g *tableGen) emitFixedClampPayload(owner *ir.Struct, f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	prop := member(f)
	expr := val + "." + prop
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4).
		slots := expr
		if owner != nil && owner.IsTable {
			slots = expr + ".Slots"
		}
		g.pf("%sif (%s != null)\n%s{\n", ind, slots, ind)
		g.pf("%s    for (int i = 0; i < %d && i < %s.Length; ++i)\n%s    {\n", ind, f.KeyEnumRef.Max, slots, ind)
		g.emitFixedClampElement(f, slots+"[i]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayFixed:
		g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
		g.pf("%s    for (int i = 0; i < %d && i < %s.Length; ++i)\n%s    {\n", ind, f.ArrayBound, expr, ind)
		g.emitFixedClampElement(f, expr+"[i]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Array == ir.ArrayCounted:
		// THE LIVE COUNT AND NOT THE BOUND: the slack is the template's zeros
		// and a zero nobody wrote is not a value to hold to a range.
		g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
		g.pf("%s    for (int i = 0; i < %s.%sCount && i < %s.Length; ++i)\n%s    {\n", ind, val, prop, expr, ind)
		g.emitFixedClampElement(f, expr+"[i]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		// THE CONTENT RULE IS NOT THIS LEG'S YET. §5.8 row 8's ruling — a
		// content violation REFUSES BY NAME — is owed by the reference first;
		// the bounds pass holds the bounds a declaration states and nothing
		// else.
	default:
		g.emitFixedClampElement(f, expr, indent)
	}
}

func (g *tableGen) emitFixedClampElement(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if ir.TableFixedClampNeeded(r) {
				g.pf("%s%sFixedClampBody(%s, ref clamped);\n", ind, r.Name, expr)
			}
			return
		case *ir.Union:
			// A UNION TAG PAST THE ARM COUNT NAMES NO ARM. On the compiled path
			// the ordinal op has already remapped it; on the identity path it is
			// a raw copy out of a stranger's bytes, and this is what holds it.
			// It lands None — the same nothing an unset union holds — and
			// counts, on BOTH plans (§5.4, §5.8 row 12).
			tagType := f.Type.Name + "Type"
			g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
			if !csOrdinalFillsStorage(r.Max, ir.StorageBitsFor(r.Max)) {
				g.pf("%s    if ((ulong)%s.Type > %d) { %s.Type = (%s)0; clamped++; }\n",
					ind, expr, r.Max, expr, tagType)
			}
			if ir.TableFixedClampNeededUnionArm(r) {
				g.pf("%s    switch (%s.Type)\n%s    {\n", ind, expr, ind)
				for _, v := range r.Variants {
					if v.F == nil || !ir.TableFixedClampNeededField(v.F) {
						continue
					}
					g.pf("%s        case %s.%s:\n%s        {\n", ind, tagType, ir.GoExportName(v.Name), ind)
					g.emitFixedClampElement(v.F, fmt.Sprintf("%s.%s", expr, ir.GoExportName(v.Name)), indent+12)
					g.pf("%s            break;\n%s        }\n", ind, ind)
				}
				g.pf("%s        default: break;\n%s    }\n", ind, ind)
			}
			g.pf("%s}\n", ind)
			return
		case *ir.Enum:
			// AN ORDINAL PAST THE ENUM'S TOP VALUE is not a variant this
			// generation cannot name — it is a number the enum cannot hold at
			// all. It lands None and counts. A FORGED ordinal — one past the
			// WRITER's own variant count, which the `ordinal` op lands as 0 —
			// is this pass's to count too, on the COMPILED plan exactly as on
			// the identity one (§5.4, §5.9 #27).
			if csOrdinalFillsStorage(r.Max, r.StorageBits) {
				return
			}
			g.pf("%sif ((ulong)%s > %d) { %s = (%s)0; clamped++; }\n",
				ind, expr, r.Max, expr, f.Type.Name)
			return
		}
		return
	}
	width := int(fixedStorageBytes(f.Type))
	switch {
	case f.Type.Kind == ir.TFloat32 && f.HasFloatRange:
		g.csClampBoth(expr, formatFloat32(f.FMin), formatFloat32(f.FMax), ind)
	case f.Type.Kind == ir.TFloat64 && f.HasFloatRange:
		g.csClampBoth(expr, formatFloat64(f.FMin), formatFloat64(f.FMax), ind)
	default:
		signed := ir.TableKindSigned(ir.TableScalarKind(f))
		// A FIXED-POINT FIELD'S BOUNDS ARE IN VALUE UNITS and its storage is
		// raw, so ir.TableRawRange shifts both ends by F before either is
		// spelled — the same numbers every other form's codec clamps at.
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			low, high := tableClampEnds(f, width)
			switch {
			case low && high:
				g.csClampBoth(expr, csIntLit(rlo, signed, width), csIntLit(rhi, signed, width), ind)
			case low:
				g.csClampEnd(expr, "<", csIntLit(rlo, signed, width), ind)
			case high:
				g.csClampEnd(expr, ">", csIntLit(rhi, signed, width), ind)
			}
		}
		if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*int64(width) {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%s// bits(%d) width clamp\n", ind, f.Type.Width)
			g.csClampEnd(expr, ">", fmt.Sprintf("%d", maxv), ind)
		}
	}
}

// csClampBoth is a two-ended clamp: the count first, then the clamp, so the
// count reads the value the wire carried and not the one the clamp just wrote.
func (g *tableGen) csClampBoth(expr, lo, hi, ind string) {
	g.pf("%sif (%s < %s) { %s = %s; clamped++; }\n", ind, expr, lo, expr, lo)
	g.pf("%selse if (%s > %s) { %s = %s; clamped++; }\n", ind, expr, hi, expr, hi)
}

// csClampEnd is the one-ended twin: an end the storage's own width already holds
// is an end the emitter drops (tableClampEnds), so a great many bounded fields
// spend one compare and not two.
func (g *tableGen) csClampEnd(expr, op, bound, ind string) {
	g.pf("%sif (%s %s %s) { %s = %s; clamped++; }\n", ind, expr, op, bound, expr, bound)
}
