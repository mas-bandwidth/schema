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
	"math/big"
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
		i := g.enterLoop()
		g.pf("%sif (%s != null)\n%s{\n", ind, slots, ind)
		g.pf("%s    for (int %s = 0; %s < %d && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, f.KeyEnumRef.Max, i, slots, i, ind)
		g.emitFixedClampElement(f, slots+"["+i+"]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.leaveLoop()
	case f.Array == ir.ArrayFixed:
		i := g.enterLoop()
		g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
		g.pf("%s    for (int %s = 0; %s < %d && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, f.ArrayBound, i, expr, i, ind)
		g.emitFixedClampElement(f, expr+"["+i+"]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.leaveLoop()
	case f.Array == ir.ArrayCounted:
		// THE LIVE COUNT AND NOT THE BOUND: the slack is the template's zeros
		// and a zero nobody wrote is not a value to hold to a range.
		i := g.enterLoop()
		g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
		g.pf("%s    for (int %s = 0; %s < %s.%sCount && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, val, prop, i, expr, i, ind)
		g.emitFixedClampElement(f, expr+"["+i+"]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.leaveLoop()
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
					g.pf("%s        case %s.%s:\n%s            {\n", ind, tagType, ir.GoExportName(v.Name), ind)
					// AN ARM IS A FIELD LINE (§2.6): an array arm holds each
					// element to its bound over the arm's live count, the way
					// the same line in a struct is held.
					g.emitFixedClampPayload(unionOwner(r), v.F, expr, indent+16)
					g.pf("%s                break;\n%s            }\n", ind, ind)
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
			// A 128-BIT END HAS NO LITERAL FORM IN C#: a value past ulong is
			// spelled as two halves shifted together, which is the shape the
			// wide codec's own clamp already uses (wideLiteral).
			lit := func(v *big.Int) string {
				if width == 16 {
					return wideLiteral(v, signed)
				}
				return csIntLit(v, signed, width)
			}
			cmp := csWideCompare(f, width, expr)
			switch {
			case low && high:
				g.csClampBothAs(cmp, expr, lit(rlo), lit(rhi), ind)
			case low:
				g.csClampEndAs(cmp, expr, "<", lit(rlo), ind)
			case high:
				g.csClampEndAs(cmp, expr, ">", lit(rhi), ind)
			}
		}
		if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*int64(width) {
			maxv := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
			g.pf("%s// bits(%d) width clamp\n", ind, f.Type.Width)
			if width == 16 {
				g.csClampEndAs(csWideCompare(f, width, expr), expr, ">", wideLiteral(maxv, false), ind)
			} else {
				g.csClampEnd(expr, ">", csIntLit(maxv, false, width), ind)
			}
		}
	}
}

// fixedWriterBound is one bounded leaf of a lineage entry's WRITER, resolved
// onto this reader's value: the reader's field (same storage width and kind —
// a ranged field narrows its width only by the ladder, and the reference's
// collectKnownRanges skips a pair whose widths differ) and the writer's raw
// bounds.
type fixedWriterBound struct {
	field  *ir.Field
	lo, hi *big.Int
}

// fixedWriterBounds resolves every lineage entry's ranged leaves against this
// reader's own fields BY NAME — the match the reference's collectKnownRanges
// makes — so the compiled lane's bounds pass holds a peer to what IT declared
// and never to this reader's wider own (§4.6, bill §12.5). The identity entry
// needs none: its writer IS this reader, and the reader's own pass holds it. A
// field this reader cannot name bounds nothing. The answer is nil when no
// entry bounds anything, so a table without ranged writers emits no clamp.
func (g *tableGen) fixedWriterBounds(st *ir.Struct, entries []FixedLineageEntry, hash uint64) [][]fixedWriterBound {
	var all [][]fixedWriterBound
	any := false
	for _, e := range entries {
		var rows []fixedWriterBound
		if e.Wire != hash {
			for _, spec := range e.Ranges {
				for _, f := range st.Fields {
					if f.Name != spec.Name {
						continue
					}
					rows = append(rows, fixedWriterBoundsOfField(f, spec.Lo, spec.Hi)...)
					break
				}
			}
		}
		all = append(all, rows)
		if len(rows) > 0 {
			any = true
		}
	}
	if !any {
		return nil
	}
	return all
}

// fixedWriterBoundsOfField spells one writer-bound spec at this reader's
// field: a fixed array is one row per slot; a counted array's live count is a
// run-time fact a static clamp does not carry, so it bounds nothing (the
// reader's own §4.6 pass walks a counted array's live elements).
func fixedWriterBoundsOfField(f *ir.Field, lo, hi int64) []fixedWriterBound {
	blo, bhi := big.NewInt(lo), big.NewInt(hi)
	if f.Array == ir.ArrayFixed {
		out := make([]fixedWriterBound, 0, f.ArrayBound)
		for i := int64(0); i < f.ArrayBound; i++ {
			out = append(out, fixedWriterBound{field: f, lo: blo, hi: bhi})
		}
		return out
	}
	return []fixedWriterBound{{field: f, lo: blo, hi: bhi}}
}

// emitFixedWriterClamp writes ONE lineage entry's writer-bounds pass: a
// straight-line clamp over this reader's value, to the peer's raw bounds —
// the same shape as the reader's own §4.6 pass, but to the WRITER's numbers.
func (g *tableGen) emitFixedWriterClamp(name string, idx int, rows []fixedWriterBound) {
	g.pf("// LINEAGE ENTRY %d's writer bounds (§4.6, bill §12.5): the peer's own\n", idx)
	g.pf("// ranged leaves, raw scale, at this reader's value — what the compiled\n")
	g.pf("// lane's bounds pass holds this peer to. The reader's range is the wider\n")
	g.pf("// one, so clamping to it would land a value the old writer could not have\n")
	g.pf("// written.\n")
	g.pf("public static void %sFixedClampKnown%d(%s value, TableReport report)\n{\n", name, idx, name)
	g.pf("    int clamped = 0;\n")
	for _, r := range rows {
		g.emitFixedWriterClampField(r, 4)
	}
	g.pf("    if (report != null) { report.Clamped += clamped; }\n")
	g.pf("}\n\n")
}

// emitFixedWriterClampField clamps one writer-bound leaf: a fixed array's
// every slot, or the one scalar field.
func (g *tableGen) emitFixedWriterClampField(b fixedWriterBound, indent int) {
	f := b.field
	expr := "value." + member(f)
	ind := strings.Repeat(" ", indent)
	if f.Array == ir.ArrayFixed {
		i := g.enterLoop()
		g.pf("%sfor (int %s = 0; %s < %d; ++%s)\n%s{\n", ind, i, i, f.ArrayBound, i, ind)
		g.emitFixedWriterClampScalar(f, expr+"["+i+"]", b.lo, b.hi, indent+4)
		g.pf("%s}\n", ind)
		g.leaveLoop()
		return
	}
	g.emitFixedWriterClampScalar(f, expr, b.lo, b.hi, indent)
}

// emitFixedWriterClampScalar spells the two-ended clamp for one scalar to the
// writer's raw bounds, dropping an end that sits on its storage's limit the
// way the reader's own pass does (tableClampEnds).
func (g *tableGen) emitFixedWriterClampScalar(f *ir.Field, expr string, lo, hi *big.Int, indent int) {
	ind := strings.Repeat(" ", indent)
	width := int(fixedStorageBytes(f.Type))
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lit := func(v *big.Int) string {
		if width == 16 {
			return wideLiteral(v, signed)
		}
		return csIntLit(v, signed, width)
	}
	lowEnd, highEnd := fixedWriterClampEnds(f, width, lo, hi)
	cmp := csWideCompare(f, width, expr)
	switch {
	case lowEnd && highEnd:
		g.csClampBothAs(cmp, expr, lit(lo), lit(hi), ind)
	case lowEnd:
		g.csClampEndAs(cmp, expr, "<", lit(lo), ind)
	case highEnd:
		g.csClampEndAs(cmp, expr, ">", lit(hi), ind)
	}
}

// fixedWriterClampEnds is tableClampEnds for a writer-carried range: which ends
// can fire against the storage width, so an end the storage already holds is
// dropped and never counted.
func fixedWriterClampEnds(f *ir.Field, widthBytes int, lo, hi *big.Int) (low, high bool) {
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	slo, shi := tableStorageRange(signed, widthBytes*8)
	return lo.Cmp(slo) > 0, hi.Cmp(shi) < 0
}

// csClampBoth is a two-ended clamp: the count first, then the clamp, so the
// count reads the value the wire carried and not the one the clamp just wrote.
func (g *tableGen) csClampBoth(expr, lo, hi, ind string) {
	g.csClampBothAs(expr, expr, lo, hi, ind)
}

// csClampBothAs is csClampBoth with the COMPARED operand spelled apart from the
// ASSIGNED one: a 128-bit leaf compares through its BCL type (csWideCompare).
func (g *tableGen) csClampBothAs(cmp, expr, lo, hi, ind string) {
	g.pf("%sif (%s < %s) { %s = %s; clamped++; }\n", ind, cmp, lo, expr, lo)
	g.pf("%selse if (%s > %s) { %s = %s; clamped++; }\n", ind, cmp, hi, expr, hi)
}

// csClampEnd is the one-ended twin: an end the storage's own width already holds
// is an end the emitter drops (tableClampEnds), so a great many bounded fields
// spend one compare and not two.
func (g *tableGen) csClampEnd(expr, op, bound, ind string) {
	g.csClampEndAs(expr, expr, op, bound, ind)
}

// csClampEndAs is csClampEnd with the compared operand spelled apart.
func (g *tableGen) csClampEndAs(cmp, expr, op, bound, ind string) {
	g.pf("%sif (%s %s %s) { %s = %s; clamped++; }\n", ind, cmp, op, bound, expr, bound)
}

// csWideCompare spells a 128-bit leaf's COMPARED operand through its BCL type.
// A table class stores System.Int128/UInt128 (csFieldType), but a PACKET
// closure class from <Base>.cs stores the serialize runtime's Int128Value /
// UInt128Value, which converts implicitly both ways: comparing that against a
// BCL literal finds two user-defined operator< candidates and is CS9342
// (ambiguous). The cast picks the BCL operator, is the identity on a table
// class, and the assignment beside it still lands through the implicit
// conversion. Narrower widths have built-in operators and are left bare.
func csWideCompare(f *ir.Field, width int, expr string) string {
	if width != 16 {
		return expr
	}
	return "((" + csFieldType(f.Type) + ")" + expr + ")"
}
