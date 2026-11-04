package rusttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

/* ---- the write contract over a Row ------------------------------------------

THE WRITE CONTRACT (docs/SPEC.md §5.1) for the two writers that take the unit's
`<Name>Row`: the fixed form's `<root>_fixed_save` and the message form's
`<root>_save_messages` and `<root>_measure_messages`. `<t>_check_row` answers
the first value the declaration does not admit, named <Owner>.<field>, or ""
when every value is admitted: a ranged integer off its min/max, a bits(N) value
wider than N, an enum ordinal outside its set, a union tag outside its arms, a
counted array's count outside its [min, max] (a count of zero is the empty
array, which both forms carry), a string(N), bytes(N) or wstring(N) length
outside 0..=N, a string(N) whose used bytes are not UTF-8 and a wstring(N)
whose used units hold an unpaired surrogate.

The writers ask it under `debug_assert!`, Rust's debug-only idiom, over the
WHOLE batch before the first byte is stored, so a refused batch leaves the
buffer untouched; a release build compiles the walk out and pays nothing. The
refusals those writers make in every build (a NaN or an infinity, a length or
count past the storage, the message form's enum and tag) are theirs and stay.

The walk is the read-side clamp pass's (emitFixedClampBody): the live count of
a counted array and never its slack, an optional's payload only when present,
a union's set arm only. A type with nothing to check gets no function and its
callers no call (§2.2's zero-cost rule). */

// rowCheckNeededStruct is whether a type's closure holds anything the write
// contract checks.
func rowCheckNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if rowCheckNeededField(f, seen) {
			return true
		}
	}
	return false
}

func rowCheckNeededField(f *ir.Field, seen map[string]bool) bool {
	if f == nil {
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
			return rowCheckNeededStruct(r, seen)
		case *ir.Union:
			return rowCheckNeededUnion(r, seen)
		case *ir.Enum:
			_, ranged := fixedEnumExtent(f, ir.TableFixedElementBytes(f))
			return ranged
		}
		return false
	}
	if low, high := fixedClampEnds(f); low || high {
		return true
	}
	return f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*ir.TableFixedStorageBytes(f.Type)
}

func rowCheckNeededUnion(un *ir.Union, seen map[string]bool) bool {
	if int64(len(un.Variants)) < tagMax(ir.TableFixedUnionTagBytes(un)) {
		return true
	}
	for _, v := range un.Variants {
		if rowCheckNeededField(v.F, seen) {
			return true
		}
	}
	return false
}

// rowCheck is the call that answers one value's contract, or "" when the type
// checks nothing.
func rowCheck(name string, needed bool) string {
	if !needed {
		return ""
	}
	return fn(name, "check_row")
}

// emitRowCheckBody emits <t>_check_row for one struct.
func (g *gen) emitRowCheckBody(st *ir.Struct) {
	if !rowCheckNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("/// %s's write contract (docs/SPEC.md §5.1): the first value its declaration\n", st.Name)
	g.pf("/// does not admit, named <Owner>.<field>, or \"\" when every value is admitted.\n")
	g.pf("#[allow(clippy::manual_range_contains, clippy::collapsible_if)]\n")
	g.pf("pub fn %s(value: &%sRow) -> &'static str {\n", fn(st.Name, "check_row"), st.Name)
	for _, f := range st.Fields {
		g.emitRowCheckField(f, "value", st.Name+"."+f.Name, 4)
	}
	g.pf("    \"\"\n}\n\n")
}

// emitRowCheckUnion emits <u>_check_row: the tag, then the arm THE TAG NAMES.
func (g *gen) emitRowCheckUnion(un *ir.Union) {
	if !rowCheckNeededUnion(un, map[string]bool{}) {
		return
	}
	g.pf("/// %s's write contract (docs/SPEC.md §5.1): a tag naming one of its arms,\n", un.Name)
	g.pf("/// then the arm THE TAG NAMES.\n")
	g.pf("#[allow(clippy::manual_range_contains, clippy::collapsible_if)]\n")
	g.pf("pub fn %s(value: &%sRow) -> &'static str {\n", fn(un.Name, "check_row"), un.Name)
	if int64(len(un.Variants)) < tagMax(ir.TableFixedUnionTagBytes(un)) {
		g.pf("    if value.tag > %d {\n        return %q;\n    }\n", len(un.Variants), un.Name)
	}
	var checked []int
	for i, v := range un.Variants {
		if rowCheckNeededField(v.F, map[string]bool{}) {
			checked = append(checked, i)
		}
	}
	arm := func(i, indent int) {
		v := un.Variants[i]
		ind := strings.Repeat(" ", indent)
		g.pf("%s// SAFETY: the tag was just matched against `%s`.\n", ind, v.Name)
		g.pf("%slet arm = unsafe { value.arms.%s };\n", ind, v.Name)
		g.emitRowCheckArm(v.F, un.Name+"."+v.Name, indent)
	}
	switch len(checked) {
	case 0:
	case 1:
		g.pf("    if value.tag == %d {\n", checked[0]+1)
		arm(checked[0], 8)
		g.pf("    }\n")
	default:
		g.pf("    match value.tag {\n")
		for _, i := range checked {
			g.pf("        %d => {\n", i+1)
			arm(i, 12)
			g.pf("        }\n")
		}
		g.pf("        _ => {} // None, and any arm with nothing to check\n")
		g.pf("    }\n")
	}
	g.pf("    \"\"\n}\n\n")
}

// emitRowCheckArm checks one union arm's `arm` binding, in the clamp pass's
// shape (emitFixedClampArm).
func (g *gen) emitRowCheckArm(f *ir.Field, owner string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case f.Array == ir.ArrayFixed:
		g.rowCheckLoop(f, fmt.Sprint(f.ArrayBound), "arm[i]", owner, indent)
	case f.Array == ir.ArrayCounted:
		g.rowCheckCount(f, "arm.value_count", owner, ind)
		g.rowCheckLoop(f, "arm.value_count as usize", "arm.value[i]", owner, indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		g.rowCheckText(f, "arm.value", "arm.value_length", owner, ind)
	default:
		g.emitRowCheckElement(f, "arm", owner, indent)
	}
}

// emitRowCheckField checks ONE field's live storage.
func (g *gen) emitRowCheckField(f *ir.Field, val, owner string, indent int) {
	if !rowCheckNeededField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	expr := val + "." + f.Name
	if f.Type.Optional {
		// an absent optional's payload is the template's zeros, never written
		g.pf("%sif %s_present {\n", ind, expr)
		g.emitRowCheckPayload(f, expr, owner, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitRowCheckPayload(f, expr, owner, indent)
}

func (g *gen) emitRowCheckPayload(f *ir.Field, expr, owner string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4)
		g.rowCheckLoop(f, fmt.Sprint(f.KeyEnumRef.Max), expr+"[i]", owner, indent)
	case f.Array == ir.ArrayFixed:
		g.rowCheckLoop(f, fmt.Sprint(f.ArrayBound), expr+"[i]", owner, indent)
	case f.Array == ir.ArrayCounted:
		g.rowCheckCount(f, expr+"_count", owner, ind)
		g.rowCheckLoop(f, expr+"_count as usize", expr+"[i]", owner, indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		g.rowCheckText(f, expr, expr+"_length", owner, ind)
	default:
		g.emitRowCheckElement(f, expr, owner, indent)
	}
}

// rowCheckCount refuses a live count outside the array's [min, max]; zero is
// the empty array and is admitted.
func (g *gen) rowCheckCount(f *ir.Field, count, owner, ind string) {
	cond := fmt.Sprintf("%s < 0 || %s > %d", count, count, f.ArrayBound)
	if f.ArrayMin > 1 {
		cond += fmt.Sprintf(" || (%s > 0 && %s < %d)", count, count, f.ArrayMin)
	}
	g.pf("%sif %s {\n%s    return %q;\n%s}\n", ind, cond, ind, owner, ind)
}

// rowCheckLoop checks every element of an array whose element has anything to
// check; the count it runs to is already held to the bound.
func (g *gen) rowCheckLoop(f *ir.Field, count, elem, owner string, indent int) {
	e := *f
	e.Array, e.KeyEnum, e.KeyEnumRef = ir.ArrayNone, "", nil
	e.Type.Optional = false
	if !rowCheckNeededField(&e, map[string]bool{}) || count == "0" {
		return
	}
	ind := strings.Repeat(" ", indent)
	g.pf("%sfor i in 0..%s {\n", ind, count)
	g.emitRowCheckElement(f, elem, owner, indent+4)
	g.pf("%s}\n", ind)
}

// rowCheckText refuses a used length outside 0..=N, then a string(N) whose
// used bytes are not UTF-8 or a wstring(N) whose used units hold an unpaired
// surrogate. bytes(N) has no content rule. A zero byte or unit is not this
// check's: the fixed and message forms carry one as the reference does.
func (g *gen) rowCheckText(f *ir.Field, buf, length, owner, ind string) {
	g.pf("%sif %s < 0 || %s > %d {\n%s    return %q;\n%s}\n", ind, length, length, f.Type.Size, ind, owner, ind)
	switch f.Type.Kind {
	case ir.TString:
		g.pf("%sif core::str::from_utf8(&%s[..%s as usize]).is_err() {\n%s    return %q;\n%s}\n",
			ind, buf, length, ind, owner, ind)
	case ir.TWString:
		g.pf("%sif core::char::decode_utf16(%s[..%s as usize].iter().copied()).any(|u| u.is_err()) {\n%s    return %q;\n%s}\n",
			ind, buf, length, ind, owner, ind)
	}
}

// emitRowCheckElement is ONE element: a call for a declaration, a comparison
// for a bounded scalar or an enum ordinal.
func (g *gen) emitRowCheckElement(f *ir.Field, expr, owner string, indent int) {
	ind := strings.Repeat(" ", indent)
	if f.Type.Kind == ir.TNamed {
		call := ""
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			call = rowCheck(r.Name, rowCheckNeededStruct(r, map[string]bool{}))
		case *ir.Union:
			call = rowCheck(r.Name, rowCheckNeededUnion(r, map[string]bool{}))
		case *ir.Enum:
			if extent, ranged := fixedEnumExtent(f, ir.TableFixedElementBytes(f)); ranged {
				g.pf("%sif %s > %d {\n%s    return %q;\n%s}\n", ind, expr, extent, ind, owner, ind)
			}
		}
		if call != "" {
			g.pf("%slet why = %s(&%s);\n%sif !why.is_empty() {\n%s    return why;\n%s}\n", ind, call, expr, ind, ind, ind)
		}
		return
	}
	lhs, wrap := expr, ""
	if w, wide := fixedWide128(f); wide {
		lhs, wrap = expr+".0", w
	}
	if low, high := fixedClampEnds(f); low || high {
		rlo, rhi, _ := ir.TableRawRange(f)
		slot := cookElemType(f)
		var conds []string
		if low {
			conds = append(conds, fmt.Sprintf("%s < %s", lhs, fixedClampLit(rlo, slot, wrap)))
		}
		if high {
			conds = append(conds, fmt.Sprintf("%s > %s", lhs, fixedClampLit(rhi, slot, wrap)))
		}
		g.pf("%sif %s {\n%s    return %q;\n%s}\n", ind, strings.Join(conds, " || "), ind, owner, ind)
	}
	if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*ir.TableFixedStorageBytes(f.Type) {
		g.pf("%sif %s > %d {\n%s    return %q; // bits(%d)'s own width\n%s}\n",
			ind, lhs, (uint64(1)<<f.Type.Width)-1, ind, owner, f.Type.Width, ind)
	}
}

// writeRowCheckBatch is the call a Row writer makes before its first byte:
// the whole batch held to the contract under debug_assert!, so a refusal
// leaves the buffer untouched and a release build compiles the walk out.
func writeRowCheckBatch(pf func(string, ...any), st *ir.Struct, ind string) {
	if !rowCheckNeededStruct(st, map[string]bool{}) {
		return
	}
	call := fn(st.Name, "check_row")
	pf("%s// THE WRITE CONTRACT (docs/SPEC.md §5.1), debug builds only: the whole\n", ind)
	pf("%s// batch before the first byte, so a refusal writes nothing.\n", ind)
	pf("%sif cfg!(debug_assertions) {\n", ind)
	pf("%s    for value in values {\n", ind)
	pf("%s        let why = %s(value);\n", ind, call)
	pf("%s        debug_assert!(why.is_empty(), \"{why}: a value its declaration does not admit, refused on write (docs/SPEC.md section 5.1)\");\n", ind)
	pf("%s    }\n", ind)
	pf("%s}\n", ind)
}
