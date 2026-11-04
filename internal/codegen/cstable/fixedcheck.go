package cstable

// THE WRITE-SIDE CHECK PASS of the fixed form (docs/SPEC-TABLES.md §3.3, §3.4;
// docs/SPEC.md §5.1). It is the write-side twin of the read-side bounds pass
// (fixedbounds.go): the same walk over the same live extent, with every bound
// the read side would clamp, and every count, length and text rule, answered
// by `return false`. C# is an assert leg, so <T>FixedSave asks it through
// Debug.Assert over the whole batch BEFORE the first output byte: a debug build
// refuses the batch with the buffer untouched, and a release build compiles the
// call away.
//
// THE STORAGE PASS (<T>FixedStorageBody) is the every-build part: a count or a
// length the writer indexes its own storage with (an array's count, a text's
// or a bytes' used length) past its declared bound, negative, or past the
// array the value holds is refused by <T>FixedSave with -1 in EVERY build,
// before the first output byte, as are a NaN or an infinity and the buffer's
// own size. A memory bound is never a debug-only check (docs/SPEC.md §5.1).
//
// A type that checks nothing gets no function (§2.2's zero-cost rule).

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedCheckNeededStruct is whether a type's check body holds any test.
func fixedCheckNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedCheckNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedCheckNeededField(f *ir.Field, seen map[string]bool) bool {
	// a deprecated slot is never a value the caller writes
	if f.Deprecated {
		return false
	}
	if f.Array == ir.ArrayCounted {
		return true
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes:
		return true
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedCheckNeededStruct(r, seen)
		case *ir.Union:
			return true
		case *ir.Enum:
			return !csOrdinalFillsStorage(r.Max, r.StorageBits)
		}
		return false
	}
	return fixedCheckScalar(f, "v") != ""
}

// fixedCheckScalar is the refusal condition of one scalar leaf over the C#
// expression expr: a ranged integer off its min or max, a bits(N) value past
// N. "" when the storage holds nothing the field refuses (an end sitting on
// its width's limit is dropped, as the read side's clamp drops it).
func fixedCheckScalar(f *ir.Field, expr string) string {
	width := int(fixedStorageBytes(f.Type))
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lit := func(v *big.Int) string {
		if width == 16 {
			return wideLiteral(v, signed)
		}
		return csIntLit(v, signed, width)
	}
	cmp := csWideCompare(f, width, expr)
	var parts []string
	if f.Type.Kind == ir.TInt && f.HasIntRange {
		if rlo, rhi, ok := ir.TableRawRange(f); ok {
			low, high := tableClampEnds(f, width)
			if low {
				parts = append(parts, fmt.Sprintf("%s < %s", cmp, lit(rlo)))
			}
			if high {
				parts = append(parts, fmt.Sprintf("%s > %s", cmp, lit(rhi)))
			}
		}
	}
	if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*int64(width) {
		maxv := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		if width == 16 {
			parts = append(parts, fmt.Sprintf("%s > %s", cmp, wideLiteral(maxv, false)))
		} else {
			parts = append(parts, fmt.Sprintf("%s > %s", cmp, csIntLit(maxv, false, width)))
		}
	}
	return strings.Join(parts, " || ")
}

// emitFixedCheckBodyFn is ONE type's write-side check: true when every value
// it holds is one its declaration admits.
func (g *tableGen) emitFixedCheckBodyFn(st *ir.Struct) {
	if !fixedCheckNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("// %s's write-side contract (docs/SPEC-TABLES.md §3.3): false is a value\n", st.Name)
	g.pf("// its declaration refuses. FixedSave asks it through Debug.Assert.\n")
	g.pf("public static bool %sFixedCheckBody(%s value)\n{\n", st.Name, st.Name)
	g.pf("    if (value == null) return true;\n")
	for _, f := range st.Fields {
		g.emitFixedCheckField(st, f, "value", 4)
	}
	g.pf("    return true;\n}\n\n")
}

func (g *tableGen) emitFixedCheckField(owner *ir.Struct, f *ir.Field, val string, indent int) {
	if !fixedCheckNeededField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// an absent optional's payload is the template's zeros (§3.4)
		g.pf("%sif (%s.%sPresent)\n%s{\n", ind, val, member(f), ind)
		g.emitFixedCheckPayload(owner, f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedCheckPayload(owner, f, val, indent)
}

func (g *tableGen) emitFixedCheckPayload(owner *ir.Struct, f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	prop := member(f)
	expr := val + "." + prop
	elem := *f
	elem.Array, elem.KeyEnum = ir.ArrayNone, ""
	loop := func(slots, limit string) {
		if !fixedCheckNeededField(&elem, map[string]bool{}) {
			return
		}
		i := g.enterLoop()
		g.pf("%sif (%s != null)\n%s{\n", ind, slots, ind)
		g.pf("%s    for (int %s = 0; %s < %s && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, limit, i, slots, i, ind)
		g.emitFixedCheckElement(f, slots+"["+i+"]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.leaveLoop()
	}
	switch {
	case f.KeyEnum != "":
		// every slot of a keyed array is live (§2.4)
		slots := expr
		if owner != nil && owner.IsTable {
			slots = expr + ".Slots"
		}
		loop(slots, fmt.Sprint(f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		loop(expr, fmt.Sprint(f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		// the declared count is the bound, and the live count the walk
		g.pf("%sif (%sCount < 0 || %sCount > %d) return false;\n", ind, expr, expr, f.ArrayBound)
		loop(expr, expr+"Count")
	case f.Type.Kind == ir.TString:
		// the used bytes are well-formed UTF-8 holding no zero byte, which the
		// fixed reader refuses as text_ill_formed (docs/SPEC.md §5.1)
		g.pf("%sif (%sLength < 0 || %sLength > %d) return false;\n", ind, expr, expr, f.Type.Size)
		g.pf("%sif (%s != null && (!TableWire.TextWellFormed(%s.AsSpan(0, %sLength)) || %s.AsSpan(0, %sLength).IndexOf((byte)0) >= 0)) return false;\n", ind, expr, expr, expr, expr, expr)
	case f.Type.Kind == ir.TWString:
		// the used units hold no unpaired surrogate and no zero unit, which the
		// fixed reader refuses as text_ill_formed (docs/SPEC.md §5.1)
		g.pf("%sif (%sLength < 0 || %sLength > %d) return false;\n", ind, expr, expr, f.Type.Size)
		g.pf("%sif (%s != null && (!TableWire.UnitsWellFormed(%s.AsSpan(0, %sLength)) || %s.AsSpan(0, %sLength).IndexOf((char)0) >= 0)) return false;\n", ind, expr, expr, expr, expr, expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sif (%sLength < 0 || %sLength > %d) return false;\n", ind, expr, expr, f.Type.Size)
	default:
		g.emitFixedCheckElement(f, expr, indent)
	}
}

func (g *tableGen) emitFixedCheckElement(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedCheckNeededStruct(r, map[string]bool{}) {
				g.pf("%sif (!%sFixedCheckBody(%s)) return false;\n", ind, r.Name, expr)
			}
		case *ir.Union:
			tagType := f.Type.Name + "Type"
			g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
			if !csOrdinalFillsStorage(r.Max, ir.StorageBitsFor(r.Max)) {
				// a tag past the arm count names no arm
				g.pf("%s    if ((ulong)%s.Type > %d) return false;\n", ind, expr, r.Max)
			}
			arms := false
			for _, v := range r.Variants {
				if v.F != nil && fixedCheckNeededField(v.F, map[string]bool{}) {
					arms = true
				}
			}
			if arms {
				g.pf("%s    switch (%s.Type)\n%s    {\n", ind, expr, ind)
				for _, v := range r.Variants {
					if v.F == nil || !fixedCheckNeededField(v.F, map[string]bool{}) {
						continue
					}
					g.pf("%s        case %s.%s:\n%s            {\n", ind, tagType, ir.GoExportName(v.Name), ind)
					// an arm is a field line (§2.6)
					g.emitFixedCheckPayload(unionOwner(r), v.F, expr, indent+16)
					g.pf("%s                break;\n%s            }\n", ind, ind)
				}
				g.pf("%s        default: break;\n%s    }\n", ind, ind)
			}
			g.pf("%s}\n", ind)
		case *ir.Enum:
			// an ordinal past the enum's top value is a number it cannot hold
			if !csOrdinalFillsStorage(r.Max, r.StorageBits) {
				g.pf("%sif ((ulong)%s > %d) return false;\n", ind, expr, r.Max)
			}
		}
		return
	}
	if cond := fixedCheckScalar(f, expr); cond != "" {
		g.pf("%sif (%s) return false;\n", ind, cond)
	}
}

// fixedStorageNeededStruct is whether a type's storage body holds any test: a
// count or a length the writer indexes storage with, in the type or below it.
// A deprecated slot is written too, so it is walked too.
func fixedStorageNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedStorageNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedStorageNeededField(f *ir.Field, seen map[string]bool) bool {
	if f.Array == ir.ArrayCounted {
		return true
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes:
		return true
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedStorageNeededStruct(r, seen)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && fixedStorageNeededField(v.F, seen) {
					return true
				}
			}
		}
	}
	return false
}

// emitFixedStorageBodyFn is ONE type's storage pass: true when every count and
// length the writer indexes storage with is inside its declared bound and the
// array that holds it. <T>FixedSave asks it in every build.
func (g *tableGen) emitFixedStorageBodyFn(st *ir.Struct) {
	if !fixedStorageNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("// %s's storage bounds (docs/SPEC.md §5.1): false is a count or a length\n", st.Name)
	g.pf("// the writer would index storage past. FixedSave refuses it in every build.\n")
	g.pf("public static bool %sFixedStorageBody(%s value)\n{\n", st.Name, st.Name)
	g.pf("    if (value == null) return true;\n")
	for _, f := range st.Fields {
		g.emitFixedStorageField(st, f, "value", 4)
	}
	g.pf("    return true;\n}\n\n")
}

func (g *tableGen) emitFixedStorageField(owner *ir.Struct, f *ir.Field, val string, indent int) {
	if !fixedStorageNeededField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// an absent optional's payload is never read by the writer (§3.4)
		g.pf("%sif (%s.%sPresent)\n%s{\n", ind, val, member(f), ind)
		g.emitFixedStoragePayload(owner, f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedStoragePayload(owner, f, val, indent)
}

func (g *tableGen) emitFixedStoragePayload(owner *ir.Struct, f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	expr := val + "." + member(f)
	elem := *f
	elem.Array, elem.KeyEnum = ir.ArrayNone, ""
	loop := func(slots, limit string) {
		if !fixedStorageNeededField(&elem, map[string]bool{}) {
			return
		}
		i := g.enterLoop()
		g.pf("%sif (%s != null)\n%s{\n", ind, slots, ind)
		g.pf("%s    for (int %s = 0; %s < %s && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, limit, i, slots, i, ind)
		g.emitFixedStorageElement(f, slots+"["+i+"]", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.leaveLoop()
	}
	switch {
	case f.KeyEnum != "":
		slots := expr
		if owner != nil && owner.IsTable {
			slots = expr + ".Slots"
		}
		loop(slots, fmt.Sprint(f.KeyEnumRef.Max))
	case f.Array == ir.ArrayFixed:
		loop(expr, fmt.Sprint(f.ArrayBound))
	case f.Array == ir.ArrayCounted:
		g.pf("%sif (%sCount < 0 || %sCount > %d) return false;\n", ind, expr, expr, f.ArrayBound)
		loop(expr, expr+"Count")
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString || f.Type.Kind == ir.TBytes:
		// the used length indexes the value's own array as well as the record
		g.pf("%sif (%sLength < 0 || %sLength > %d || (%s != null && %sLength > %s.Length)) return false;\n", ind, expr, expr, f.Type.Size, expr, expr, expr)
	default:
		g.emitFixedStorageElement(f, expr, indent)
	}
}

func (g *tableGen) emitFixedStorageElement(f *ir.Field, expr string, indent int) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	ind := strings.Repeat(" ", indent)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedStorageNeededStruct(r, map[string]bool{}) {
			g.pf("%sif (!%sFixedStorageBody(%s)) return false;\n", ind, r.Name, expr)
		}
	case *ir.Union:
		tagType := f.Type.Name + "Type"
		arms := false
		for _, v := range r.Variants {
			if v.F != nil && fixedStorageNeededField(v.F, map[string]bool{}) {
				arms = true
			}
		}
		if !arms {
			return
		}
		g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
		g.pf("%s    switch (%s.Type)\n%s    {\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !fixedStorageNeededField(v.F, map[string]bool{}) {
				continue
			}
			g.pf("%s        case %s.%s:\n%s            {\n", ind, tagType, ir.GoExportName(v.Name), ind)
			// an arm is a field line (§2.6), written from the union's members
			g.emitFixedStoragePayload(unionOwner(r), v.F, expr, indent+16)
			g.pf("%s                break;\n%s            }\n", ind, ind)
		}
		g.pf("%s        default: break;\n%s    }\n", ind, ind)
		g.pf("%s}\n", ind)
	}
}
