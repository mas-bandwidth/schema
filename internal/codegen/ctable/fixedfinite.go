package ctable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

/* ---- the fixed form's finite pass ------------------------------------------

A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables go
finite-only too, same rule as packets"). The fixed form has no walk to put a
raw-bits test in: a record lands by a copy plan, so the test runs over STORAGE,
straight-line, after the record lands and BEFORE the bounds pass, and ONE pass
covers both plans: the identity copy and a lineage plan's widenF, whose
ordinary conversion keeps a NaN a NaN and an infinity an infinity. The same
pass is the save's pre-pass, so a writer refuses what a reader refuses. It
walks only what a read can have written: a counted array's live elements, an
optional's payload when present, a union's set arm. The Go oracle is
internal/codegen/gotable's <T>FixedFiniteBody. */

// fixedFiniteNeededStruct is whether a fixed type holds a float anywhere a
// read lands one. A type that holds none gets no pass (§2.2's zero-cost rule).
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

// emitFixedFiniteBodyFn emits <T>'s finite pass: 1 when every float a read
// lands is finite, 0 at the first NaN or infinity.
func (g *tableGen) emitFixedFiniteBodyFn(st *ir.Struct) {
	if !fixedFiniteNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("/* %s's finite pass (docs/SPEC-TABLES.md §3.3): 0 at the first NaN or infinity. */\n", st.Name)
	g.pf("static SCHEMA_UNUSED int %s( const %s * value )\n{\n", g.sym(st.Name, "fixed_finite_body"), st.Name)
	g.fixedLoop = 0
	for _, f := range st.Fields {
		g.emitFixedFiniteField(f, "value->", 4)
	}
	g.pf("    return 1;\n}\n\n")
}

func (g *tableGen) emitFixedFiniteField(f *ir.Field, val string, indent int) {
	if !fixedFiniteNeededField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// AN ABSENT OPTIONAL'S PAYLOAD IS IGNORED ON READ (§3.4)
		g.pf("%sif ( %s%s_present )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedFinitePayload(f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedFinitePayload(f, val, indent)
}

func (g *tableGen) emitFixedFinitePayload(f *ir.Field, val string, indent int) {
	switch {
	case f.KeyEnum != "":
		g.emitFixedFiniteLoop(f, strconv.FormatInt(f.KeyEnumRef.Max, 10), val+f.Name, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedFiniteLoop(f, strconv.FormatInt(f.ArrayBound, 10), val+f.Name, indent)
	case f.Array == ir.ArrayCounted:
		// the bounds pass clamps the count AFTER this one, so the walk keeps
		// inside storage on its own
		g.emitFixedFiniteLoop(f, fmt.Sprintf("( %s%s_count < %d ? (int64_t) %s%s_count : %d )", val, f.Name, f.ArrayBound, val, f.Name, f.ArrayBound), val+f.Name, indent)
	default:
		g.emitFixedFiniteElement(f, val+f.Name, indent)
	}
}

func (g *tableGen) emitFixedFiniteLoop(f *ir.Field, count, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	i := fixedLoopVar(g.fixedLoop)
	g.fixedLoop++
	g.pf("%s{\n%s    int64_t %s;\n%s    for ( %s = 0; %s < %s; ++%s )\n%s    {\n", ind, ind, i, ind, i, i, count, i, ind)
	g.emitFixedFiniteElement(f, expr+"["+i+"]", indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
	g.fixedLoop--
}

func (g *tableGen) emitFixedFiniteElement(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch f.Type.Kind {
	case ir.TFloat32:
		g.pf("%sif ( !table_float_finite( %s ) ) { return 0; }\n", ind, expr)
		return
	case ir.TFloat64:
		g.pf("%sif ( !table_double_finite( %s ) ) { return 0; }\n", ind, expr)
		return
	case ir.TNamed:
	default:
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedFiniteNeededStruct(r, map[string]bool{}) {
			g.pf("%sif ( !%s( &%s ) ) { return 0; }\n", ind, g.sym(r.Name, "fixed_finite_body"), expr)
		}
	case *ir.Union:
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !fixedFiniteNeededField(v.F, map[string]bool{}) {
				continue
			}
			arm := *v.F
			val := expr + ".as."
			if armCompanioned(v) {
				val = expr + ".as." + v.Name + "."
				arm.Name = "value"
			} else {
				arm.Name = v.Name
			}
			g.pf("%s    case %s:\n%s    {\n", ind, enumConst(f.Type.Name+"Type", v.Name), ind)
			g.emitFixedFinitePayload(&arm, val, indent+8)
			g.pf("%s        break;\n%s    }\n", ind, ind)
		}
		g.pf("%s    default: break;\n%s}\n", ind, ind)
	}
}
