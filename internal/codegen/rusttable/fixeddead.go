package rusttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// EVERY DEAD BYTE IS ZERO (docs/SPEC-TABLES.md §3.4, THE SLACK RULE;
// docs/FIXED-FORM-ALGORITHM.md §3.1; docs/rulings.json fixed-dead-bytes-zero).
//
// A conforming writer leaves zeros in every byte of slack — an absent
// optional's payload, a counted array's tail past its live count, a text's or
// a byte buffer's bytes past the used length, and a union's arm bytes behind
// the taken arm (all of them under tag 0) — so a record with anything else
// there is one no conforming writer produced, and the read refuses it whole.
//
// THE IDENTITY READ CHECKS THE RECORD AS IT ARRIVED, before the plan moves a
// byte: a straight line per type, emitted beside the scatter, over this
// build's own layout. A read through the lock checks the WRITER's slack
// instead, which is the plan's Absent, Tail and Arm entries (fixedruntime.go).
//
// Slack is checked for zero and for nothing else: no bool, range, ordinal or
// text rule runs on it. Slack nested in live data is that value's own slack
// and is checked as such; slack inside slack is zero because the outer span
// was checked whole, so the inner checks run unconditionally and pass over
// it. A `| deprecated` field's slot is not read at all, as on every read.

// emitFixedDeadBody is one table's dead-byte check over its record image.
func (g *gen) emitFixedDeadBody(st *ir.Struct) {
	g.pf("/// %s: true when every dead byte of one record image is zero (§3.4,\n", st.Name)
	g.pf("/// THE SLACK RULE). A read that sees false refuses the whole load.\n")
	g.pf("pub fn %s(b: &[u8]) -> bool {\n", fn(st.Name, "fixed_dead"))
	if !fixedStructHasSlack(st) {
		g.pf("    let _ = b; // nothing in this table is ever slack\n")
	}
	off := int64(0)
	for _, f := range st.Fields {
		if !f.Deprecated && fixedFieldHasSlack(f) {
			g.emitFixedDeadField(f, off)
		}
		off += ir.TableFixedFieldBytes(f)
	}
	g.pf("    true\n}\n\n")
}

func (g *gen) emitFixedDeadField(f *ir.Field, off int64) {
	base := off
	if f.Type.Optional {
		n := ir.TableFixedFieldBytes(f)
		g.pf("    // %s: under an absent flag the payload is slack\n", f.Name)
		g.pf("    if b[%d] == 0 && !table_fixed_dead_zero(&b[%d..%d]) {\n        return false;\n    }\n",
			off, off+ir.TableFixedPresentBytes, off+n)
		base += ir.TableFixedPresentBytes
	}
	g.emitFixedDeadPayload(f, base, 4)
}

// emitFixedDeadPayload checks one value's own slack (an optional's payload, or
// a union arm) laid at base.
func (g *gen) emitFixedDeadPayload(f *ir.Field, base int64, indent int) {
	ind := strings.Repeat(" ", indent)
	elem := ir.TableFixedElementBytes(f)
	switch {
	case f.KeyEnum != "":
		g.emitFixedDeadSlots(f, base, f.KeyEnumRef.Max, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedDeadSlots(f, base, f.ArrayBound, indent)
	case f.Array == ir.ArrayCounted:
		g.pf("%s// %s: the tail past the live count is slack\n", ind, f.Name)
		g.emitFixedDeadTail(base, f.ArrayBound, elem, ind)
		// every slot, dead ones too: a dead slot is zero, so its own checks pass
		g.emitFixedDeadSlots(f, base+ir.TableFixedCountBytes, f.ArrayBound, indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("%s// %s: the bytes past the used length are slack\n", ind, f.Name)
		g.emitFixedDeadTail(base, f.Type.Size, 1, ind)
	default:
		if name, n, nested := fixedNestedBody(f); nested && fixedElementHasSlack(f) {
			g.pf("%sif !%s(&b[%d..%d]) {\n%s    return false;\n%s}\n", ind, fn(name, "fixed_dead"), base, base+n, ind, ind)
		}
	}
}

// emitFixedDeadTail checks the units past a live count (or a used length)
// held to its bound: a count past the bound is out of range, and the scatter
// refuses it by name, so the tail is measured from the clamped value.
func (g *gen) emitFixedDeadTail(at, bound, unit int64, ind string) {
	if bound == 0 {
		return
	}
	g.pf("%s{\n", ind)
	g.pf("%s    let raw = i32::from_le_bytes(b[%d..%d].try_into().expect(\"four bytes\"));\n", ind, at, at+4)
	g.pf("%s    let live = raw.clamp(0, %d) as usize;\n", ind, bound)
	start := fmt.Sprintf("%d + live", at+ir.TableFixedCountBytes)
	if unit != 1 {
		start = fmt.Sprintf("%d + live * %d", at+ir.TableFixedCountBytes, unit)
	}
	g.pf("%s    if !table_fixed_dead_zero(&b[%s..%d]) {\n%s        return false;\n%s    }\n", ind, start, at+ir.TableFixedCountBytes+bound*unit, ind, ind)
	g.pf("%s}\n", ind)
}

// emitFixedDeadSlots runs a nested element's own check over every slot.
func (g *gen) emitFixedDeadSlots(f *ir.Field, base, count int64, indent int) {
	name, n, nested := fixedNestedBody(f)
	if !nested || count == 0 || !fixedElementHasSlack(f) {
		return
	}
	ind := strings.Repeat(" ", indent)
	g.pf("%sfor i in 0..%d {\n", ind, count)
	g.pf("%s    let at = %s;\n", ind, fixedSlotOffset(base, n))
	g.pf("%s    if !%s(&b[at..at + %d]) {\n%s        return false;\n%s    }\n", ind, fn(name, "fixed_dead"), n, ind, ind)
	g.pf("%s}\n", ind)
}

// emitFixedDeadUnion is one union's dead-byte check: every arm byte behind the
// taken arm (all of them under tag 0) is slack, and then the taken arm's own.
func (g *gen) emitFixedDeadUnion(un *ir.Union) {
	tagw := ir.TableFixedUnionTagBytes(un)
	size := ir.TableFixedUnionBytes(un)
	g.pf("/// %s: true when every dead byte of one union image is zero (§3.4,\n", un.Name)
	g.pf("/// THE SLACK RULE): the arm bytes behind the taken arm, all of them under\n")
	g.pf("/// tag 0, and the taken arm's own slack.\n")
	g.pf("pub fn %s(b: &[u8]) -> bool {\n", fn(un.Name, "fixed_dead"))
	if size == tagw {
		g.pf("    let _ = b; // no arm carries a payload, so no arm byte exists\n    true\n}\n\n")
		return
	}
	if tagw == 1 {
		g.pf("    let tag = u64::from(b[0]);\n")
	} else {
		g.pf("    let tag = u64::from(%s::from_le_bytes(b[0..%d].try_into().expect(\"the tag's width\")));\n", fixedRawUint(tagw), tagw)
	}
	g.pf("    let taken: usize = match tag {\n        0 => 0,\n")
	for i, v := range un.Variants {
		taken := int64(0)
		if v.F != nil {
			taken = ir.TableFixedFieldBytes(v.F)
		}
		g.pf("        %d => %d,\n", i+1, taken)
	}
	g.pf("        // a forged tag: the scatter refuses it out of range, and no arm\n")
	g.pf("        // was taken to measure behind\n")
	g.pf("        _ => return true,\n")
	g.pf("    };\n")
	g.pf("    if !table_fixed_dead_zero(&b[%d + taken..]) {\n        return false;\n    }\n", tagw)
	var arms []int
	for i, v := range un.Variants {
		if v.F != nil && fixedFieldHasSlack(v.F) {
			arms = append(arms, i)
		}
	}
	if len(arms) > 0 {
		g.pf("    match tag {\n")
		for _, i := range arms {
			g.pf("        %d => {\n", i+1)
			g.emitFixedDeadPayload(un.Variants[i].F, tagw, 12)
			g.pf("        }\n")
		}
		g.pf("        _ => {}\n    }\n")
	}
	g.pf("    true\n}\n\n")
}

// fixedFieldHasSlack: whether a field can carry slack anywhere in its bytes.
func fixedFieldHasSlack(f *ir.Field) bool {
	if f.Type.Optional || f.Array == ir.ArrayCounted || f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return true
	}
	return fixedElementHasSlack(f)
}

// fixedElementHasSlack: whether ONE element of a field (a nested table or a
// union) can carry slack.
func fixedElementHasSlack(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return fixedStructHasSlack(r)
	case *ir.Union:
		return ir.TableFixedUnionBytes(r) > ir.TableFixedUnionTagBytes(r)
	}
	return false
}

func fixedStructHasSlack(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if !f.Deprecated && fixedFieldHasSlack(f) {
			return true
		}
	}
	return false
}
