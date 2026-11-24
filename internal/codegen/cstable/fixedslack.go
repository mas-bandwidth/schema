package cstable

// THE SLOTS PAST A COUNT AFTER ANY LOAD (docs/SPEC-TABLES.md §3.4, THE SLOTS
// PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED).
//
// The C# plan lands every slot the record carries, a counted array's dead
// slots included, so after a lawful load the slots past a count hold the
// WRITER'S ZEROS: SlackCell.Id lands 0 where a fresh value holds its declared
// 7. The refusal path already resets the whole value (TableReset). The lawful
// path runs this pass after the plan, on every plan (the identity plan and a
// lineage plan alike), over STORAGE: every element slot past a counted
// array's count is reset IN PLACE to the element's declared defaults, and the
// pass recurses through live elements, a live union arm and a present
// optional, so a counted array at any depth gets its own slack reset.
//
// ZERO COST: only a counted array of tables, types or unions whose element
// default image is NOT all zero needs the reset (an all-zero element's
// default is exactly what the writer's zeros land), and the pass walks only
// into types that (transitively) hold such an array. A unit with none emits
// no pass and no call, byte for byte the code it emitted before.

import (
	"fmt"
	"math"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// csSlackElemNonZero is whether one element of f (f's array shape aside) has a
// default image that is not all zero: what a fresh element holds differs from
// what a record's zero bytes land there.
func csSlackElemNonZero(f *ir.Field, seen map[string]bool) bool {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return csSlackStructNonZero(r, seen)
		case *ir.Union:
			for _, v := range r.Variants {
				if v.F != nil && csSlackFieldNonZero(v.F, seen) {
					return true
				}
			}
			return false
		case *ir.Enum:
			// a defaulted enum rides its variant's ordinal, from 1; 0 is None
			return f.HasDefault && f.DefVariant != ""
		}
	}
	if !f.HasDefault {
		return false
	}
	switch f.Type.Kind {
	case ir.TBool:
		return f.DefBool
	case ir.TFloat32:
		return math.Float32bits(float32(f.DefFloat)) != 0
	case ir.TFloat64:
		return math.Float64bits(f.DefFloat) != 0
	case ir.TString, ir.TBytes:
		return len(f.DefBytes) != 0
	}
	return f.DefInt != nil && f.DefInt.Sign() != 0
}

func csSlackFieldNonZero(f *ir.Field, seen map[string]bool) bool {
	switch {
	case f.Array == ir.ArrayCounted:
		return f.BornCount() > 0 || csSlackElemNonZero(f, seen)
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		return csSlackElemNonZero(f, seen)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		return len(f.DefBytes) != 0
	}
	// an optional's payload rides whole: a fresh one is absent and its
	// payload still holds its defaults
	return csSlackElemNonZero(f, seen)
}

func csSlackStructNonZero(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	defer delete(seen, st.Name)
	for _, f := range st.Fields {
		if csSlackFieldNonZero(f, seen) {
			return true
		}
	}
	return false
}

// csSlackObjectElem is whether f's elements are objects the value owns (a
// table, a type or a union held by value), the slots TableReset resets.
func csSlackObjectElem(f *ir.Field) bool {
	return isClassRef(f.Type) && !f.Type.Pointer
}

// csSlackResetsDead is whether f is a counted array whose slots past the
// count need the reset: object elements whose default image is not zero.
func csSlackResetsDead(f *ir.Field) bool {
	return f.Array == ir.ArrayCounted && csSlackObjectElem(f) && csSlackElemNonZero(f, map[string]bool{})
}

// csSlackNeedsStruct is whether st holds, anywhere a load lands it, a counted
// array csSlackResetsDead names.
func csSlackNeedsStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	defer delete(seen, st.Name)
	for _, f := range st.Fields {
		if csSlackNeedsField(f, seen) {
			return true
		}
	}
	return false
}

func csSlackNeedsField(f *ir.Field, seen map[string]bool) bool {
	if !csSlackObjectElem(f) {
		return false
	}
	if csSlackResetsDead(f) {
		return true
	}
	return csSlackNeedsElem(f, seen)
}

// csSlackNeedsElem is whether one element of f holds such an array inside it.
func csSlackNeedsElem(f *ir.Field, seen map[string]bool) bool {
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return csSlackNeedsStruct(r, seen)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.F != nil && csSlackNeedsField(v.F, seen) {
				return true
			}
		}
	}
	return false
}

// emitFixedSlackFn emits TableFixedSlack(T): the slack pass for one type. It is
// verb-first and overloaded on the type, as TableReset is, so it claims no
// name from a declaration (§11), and private: only the fixed load calls it.
func (g *tableGen) emitFixedSlackFn(st *ir.Struct) {
	if !csSlackNeedsStruct(st, map[string]bool{}) {
		return
	}
	saved := g.owner
	g.owner = st
	g.pf("// TableFixedSlack(%s): every element slot past a counted array's count\n", st.Name)
	g.pf("// back to its declared defaults, in place, at every depth a load lands\n")
	g.pf("// (docs/SPEC-TABLES.md §3.4, THE SLOTS PAST A COUNT AFTER ANY LOAD).\n")
	g.pf("private static void TableFixedSlack(%s value)\n{\n", st.Name)
	for _, f := range st.Fields {
		g.emitFixedSlackField(f, "value", 4)
	}
	g.pf("}\n\n")
	g.owner = saved
}

func (g *tableGen) emitFixedSlackField(f *ir.Field, val string, indent int) {
	if !csSlackNeedsField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// an absent optional's payload is not a value a load lands (§4.6)
		g.pf("%sif (%s.%sPresent)\n%s{\n", ind, val, member(f), ind)
		g.emitFixedSlackPayload(f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedSlackPayload(f, val, indent)
}

func (g *tableGen) emitFixedSlackPayload(f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	expr := val + "." + member(f)
	inner := csSlackNeedsElem(f, map[string]bool{})
	loop := func(slots, from, limit string, body func(elem string)) {
		i := g.enterLoop()
		g.pf("%sfor (int %s = %s; %s < %s; ++%s)\n%s{\n", ind, i, from, i, limit, i, ind)
		body(slots + "[" + i + "]")
		g.pf("%s}\n", ind)
		g.leaveLoop()
	}
	switch {
	case f.Array == ir.ArrayCounted:
		count := val + "." + member(f) + "Count"
		if inner {
			// the live elements: their own counted arrays past their own counts
			loop(expr, "0", fmt.Sprintf("%s && %s < %s.Length", count, g.fixedLoopVar(), expr), func(e string) {
				g.emitFixedSlackElement(f, e, indent+4)
			})
		}
		if csSlackResetsDead(f) {
			// the dead slots: what a fresh value holds there
			loop(expr, count, expr+".Length", func(e string) {
				g.pf("%s    TableReset(%s);\n", ind, e)
			})
		}
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		slots := g.keyedSlots(val+".", f)
		loop(slots, "0", slots+".Length", func(e string) {
			g.emitFixedSlackElement(f, e, indent+4)
		})
	default:
		g.emitFixedSlackElement(f, expr, indent)
	}
}

func (g *tableGen) emitFixedSlackElement(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if csSlackNeedsStruct(r, map[string]bool{}) {
			g.pf("%sTableFixedSlack(%s);\n", ind, expr)
		}
	case *ir.Union:
		// the live arm only: the other arms are no value a load lands
		tagType := f.Type.Name + "Type"
		saved := g.owner
		g.owner = unionOwner(r)
		g.pf("%sswitch (%s.Type)\n%s{\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !csSlackNeedsField(v.F, map[string]bool{}) {
				continue
			}
			g.pf("%s    case %s.%s:\n%s        {\n", ind, tagType, ir.GoExportName(v.Name), ind)
			g.emitFixedSlackField(v.F, expr, indent+12)
			g.pf("%s            break;\n%s        }\n", ind, ind)
		}
		g.pf("%s    default: break;\n%s}\n", ind, ind)
		g.owner = saved
	}
}
