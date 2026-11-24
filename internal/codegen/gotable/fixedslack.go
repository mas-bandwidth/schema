package gotable

import (
	"fmt"
	"math"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

/* ---- the slots past a count ------------------------------------------------

THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
§3.4). After every fixed-form load every element slot past a counted array's
live count holds what a fresh value holds there. The run lands the record's
bytes, so a dead slot arrives as the writer's zeros; that is the fresh value
only where the element's fresh image is all zero. Where it is not (a table or
union element with a non-zero declared default somewhere inside), the load
resets each slot past the count in place, after the record lands, on the
identity plan and every lineage plan alike, and walks live elements, live arms
and present optionals for the counted arrays inside them.

A schema whose counted arrays all have an all-zero fresh element generates no
slack code at all (§2.2's zero-cost rule). A refusal needs nothing here: the
refusal exit resets every row handed in to its declared defaults. */

// fixedFreshNonZeroStruct is whether a fresh st (its Reset) holds a non-zero
// byte anywhere.
func fixedFreshNonZeroStruct(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if fixedFreshNonZeroField(f) {
			return true
		}
	}
	return false
}

// fixedFreshNonZeroField is whether Reset leaves a non-zero byte in f's
// storage. An optional's payload is reset to its defaults as well, so it
// counts; its present flag is false.
func fixedFreshNonZeroField(f *ir.Field) bool {
	switch {
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		if f.Array == ir.ArrayCounted && f.BornCount() != 0 {
			return true
		}
		return fixedFreshNonZeroElement(f)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		return len(f.DefBytes) > 0
	}
	return fixedFreshNonZeroElement(f) || fixedFreshNonZeroScalar(f)
}

// fixedFreshNonZeroElement is whether one fresh element of f (a table, or a
// union with no arm and every arm at its defaults) holds a non-zero byte. A
// scalar or enum element is zero: an array takes no specified default.
func fixedFreshNonZeroElement(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return fixedFreshNonZeroStruct(r)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.F != nil && fixedFreshNonZeroField(v.F) {
				return true
			}
		}
	}
	return false
}

func fixedFreshNonZeroScalar(f *ir.Field) bool {
	if !f.HasDefault && f.DefInt == nil {
		return false
	}
	switch f.Type.Kind {
	case ir.TBool:
		return f.DefBool
	case ir.TFloat32, ir.TFloat64:
		return f.HasDefault && math.Float64bits(f.DefFloat) != 0
	case ir.TNamed:
		if _, ok := f.Type.Ref.(*ir.Enum); ok {
			return f.HasDefault && f.DefVariant != ""
		}
	}
	return f.DefInt != nil && f.DefInt.Sign() != 0
}

// fixedSlackNeededStruct is whether st holds, at any depth, a counted array
// whose fresh element is not all zero.
func fixedSlackNeededStruct(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if fixedSlackNeededField(f) {
			return true
		}
	}
	return false
}

func fixedSlackNeededField(f *ir.Field) bool {
	if f.Array == ir.ArrayCounted && fixedFreshNonZeroElement(f) {
		return true
	}
	return fixedSlackNeededElement(f)
}

// fixedSlackNeededElement is whether one live element of f (or f itself, when
// it is no array) holds such an array.
func fixedSlackNeededElement(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return fixedSlackNeededStruct(r)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.F != nil && fixedSlackNeededField(v.F) {
				return true
			}
		}
	}
	return false
}

// emitFixedSlackBodyFn emits st's slack pass. A type that needs none gets no
// function.
func (g *tableGen) emitFixedSlackBodyFn(st *ir.Struct) {
	if !fixedSlackNeededStruct(st) {
		return
	}
	g.pf("// %sFixedSlackBody resets every slot past a counted array's live count to\n", st.Name)
	g.pf("// what a fresh value holds there, at every depth (docs/SPEC-TABLES.md §3.4,\n")
	g.pf("// THE SLOTS PAST A COUNT AFTER ANY LOAD): the record lands the writer's zeros.\n")
	g.pf("func %sFixedSlackBody(value *%s) {\n", st.Name, g.fixedStorage(st.Name))
	for _, f := range st.Fields {
		g.emitFixedSlackField(f, "value", 1, 0)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedSlackField(f *ir.Field, val string, tabs, depth int) {
	if !fixedSlackNeededField(f) {
		return
	}
	ind := strings.Repeat("\t", tabs)
	if f.Type.Optional {
		g.pf("%sif %s.%sPresent {\n", ind, val, member(f))
		g.emitFixedSlackPayload(f, val, tabs+1, depth)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedSlackPayload(f, val, tabs, depth)
}

func (g *tableGen) emitFixedSlackPayload(f *ir.Field, val string, tabs, depth int) {
	ind := strings.Repeat("\t", tabs)
	base := val + "." + member(f)
	i := fixedLoopVar(depth)
	elem := fmt.Sprintf("%s[%s]", base, i)
	switch {
	case f.Array == ir.ArrayCounted && fixedFreshNonZeroElement(f):
		// every slot: a live one is walked, a dead one is reset in place
		g.pf("%sfor %s := range %s {\n", ind, i, base)
		g.pf("%s\tif %s < int(%s.%sCount) {\n", ind, i, val, member(f))
		g.emitFixedSlackLive(f, elem, tabs+2, depth+1)
		g.pf("%s\t\tcontinue\n", ind)
		g.pf("%s\t}\n", ind)
		g.emitFixedSlackReset(f, elem, tabs+1)
		g.pf("%s}\n", ind)
	case f.Array == ir.ArrayCounted:
		g.pf("%sfor %s := range min(max(int(%s.%sCount), 0), len(%s)) {\n", ind, i, val, member(f), base)
		g.emitFixedSlackLive(f, elem, tabs+1, depth+1)
		g.pf("%s}\n", ind)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone:
		g.pf("%sfor %s := range %s {\n", ind, i, base)
		g.emitFixedSlackLive(f, elem, tabs+1, depth+1)
		g.pf("%s}\n", ind)
	default:
		g.emitFixedSlackLive(f, base, tabs, depth)
	}
}

// emitFixedSlackLive walks one live element (or a non-array field's value).
func (g *tableGen) emitFixedSlackLive(f *ir.Field, expr string, tabs, depth int) {
	if !fixedSlackNeededElement(f) {
		return
	}
	ind := strings.Repeat("\t", tabs)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		g.pf("%s%sFixedSlackBody(&%s)\n", ind, r.Name, expr)
	case *ir.Union:
		// the set arm only: an unset arm is dead, and the union reset or the
		// record's zeros already hold it
		g.pf("%sswitch %s.Type {\n", ind, expr)
		for _, v := range r.Variants {
			if v.F == nil || !fixedSlackNeededField(v.F) {
				continue
			}
			owner, arm := g.fixedArmVal(v, expr)
			g.pf("%scase %sType%s:\n", ind, f.Type.Name, ir.GoExportName(v.Name))
			g.emitFixedSlackField(&arm, owner, tabs+1, depth)
		}
		g.pf("%s}\n", ind)
	}
}

// emitFixedSlackReset resets one dead slot in place to a fresh element.
func (g *tableGen) emitFixedSlackReset(f *ir.Field, expr string, tabs int) {
	ind := strings.Repeat("\t", tabs)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		g.pf("%s%sReset(&%s)\n", ind, f.Type.Name, expr)
	case *ir.Union:
		g.emitUnionValueReset(expr, r)
	}
}
