package cstable

// THE FIXED FORM'S FINITE PASS (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27,
// "tables go finite-only too, same rule as packets").
//
// Every fixed read refuses a NaN or an infinity as non_finite_float, naming the
// field, and the test runs FIRST, before the bounds pass, so a compressed
// float's clamp never sees one. Like the bounds pass it runs over STORAGE after
// the record lands, so ONE pass covers both plans: the identity copy and a
// lineage plan's widenf, whose ordinary conversion keeps a NaN a NaN and an
// infinity an infinity. It walks only what a read can have written — a counted
// array's live elements, an optional's payload when present, a union's set arm.
// The writer runs the same pass over every value before its first output byte,
// so FixedSave refuses what FixedLoad refuses.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedFiniteNeededStruct is whether a fixed type holds a float anywhere a read
// lands one. A type that holds none gets no pass (§2.2's zero-cost rule).
func fixedFiniteNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedFiniteNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedFiniteNeededField(f *ir.Field, seen map[string]bool) bool {
	switch f.Type.Kind {
	case ir.TFloat32, ir.TFloat64:
		return true
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedFiniteNeededStruct(r, seen)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && fixedFiniteNeededField(v.F, seen) {
					return true
				}
			}
		}
	}
	return false
}

// emitFixedFiniteBodyFn emits <T>FixedFiniteBody: the first non-finite float's
// <Owner>.<field> name, or null for a record whose floats are all finite.
func (g *tableGen) emitFixedFiniteBodyFn(st *ir.Struct) {
	if !fixedFiniteNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("// %s's finite pass (docs/SPEC-TABLES.md §3.3): the first NaN or infinity's\n", st.Name)
	g.pf("// <Owner>.<field>, or null.\n")
	g.pf("public static string %sFixedFiniteBody(%s value)\n{\n", st.Name, st.Name)
	g.pf("    if (value == null) return null;\n")
	for _, f := range st.Fields {
		g.emitFixedFiniteField(st, f, "value", 4)
	}
	g.pf("    return null;\n}\n\n")
}

func (g *tableGen) emitFixedFiniteField(owner *ir.Struct, f *ir.Field, val string, indent int) {
	if !fixedFiniteNeededField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// an absent optional's payload is ignored on read (§4.6)
		g.pf("%sif (%s.%sPresent)\n%s{\n", ind, val, member(f), ind)
		g.emitFixedFinitePayload(owner, f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedFinitePayload(owner, f, val, indent)
}

func (g *tableGen) emitFixedFinitePayload(owner *ir.Struct, f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	expr := val + "." + member(f)
	loop := func(slots, limit string) {
		i := g.enterLoop()
		g.pf("%sif (%s != null)\n%s{\n", ind, slots, ind)
		g.pf("%s    for (int %s = 0; %s < %s && %s < %s.Length; ++%s)\n%s    {\n", ind, i, i, limit, i, slots, i, ind)
		g.emitFixedFiniteElement(owner, f, slots+"["+i+"]", indent+8)
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
		// the live count and not the bound: the slack is nobody's value
		loop(expr, val+"."+member(f)+"Count")
	default:
		g.emitFixedFiniteElement(owner, f, expr, indent)
	}
}

func (g *tableGen) emitFixedFiniteElement(owner *ir.Struct, f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch f.Type.Kind {
	case ir.TFloat32:
		g.pf("%sif (!float.IsFinite(%s)) return %q;\n", ind, expr, owner.DeclaredName()+"."+f.Name)
		return
	case ir.TFloat64:
		g.pf("%sif (!double.IsFinite(%s)) return %q;\n", ind, expr, owner.DeclaredName()+"."+f.Name)
		return
	case ir.TNamed:
	default:
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedFiniteNeededStruct(r, map[string]bool{}) {
			g.pf("%s{\n%s    string why = %sFixedFiniteBody(%s);\n%s    if (why != null) return why;\n%s}\n", ind, ind, r.Name, expr, ind, ind)
		}
	case *ir.Union:
		tagType := f.Type.Name + "Type"
		arms := unionOwner(r)
		g.pf("%sif (%s != null)\n%s{\n", ind, expr, ind)
		g.pf("%s    switch (%s.Type)\n%s    {\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !fixedFiniteNeededField(v.F, map[string]bool{}) {
				continue
			}
			g.pf("%s        case %s.%s:\n%s            {\n", ind, tagType, ir.GoExportName(v.Name), ind)
			g.emitFixedFiniteField(arms, v.F, expr, indent+16)
			g.pf("%s                break;\n%s            }\n", ind, ind)
		}
		g.pf("%s        default: break;\n%s    }\n", ind, ind)
		g.pf("%s}\n", ind)
	}
}
