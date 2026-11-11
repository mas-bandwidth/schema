package ctable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

/*
emitFixedWriteCheck is THE FIXED WRITER'S CONTRACT (docs/SPEC.md §5.1): 0 when

	a record holds a value its declaration does not admit — a ranged integer off
	its min or max, a bits(N) value past N, an enum ordinal or a union tag
	outside its set, a ranged float off its range, a string that is not
	well-formed UTF-8 or holds a zero byte. It asks the READ side's own bounds
	body, which only counts, so the writer never writes a record its own reader
	refuses and the two cannot drift. The save asks it debug-only, after the
	storage bound, so the walk stays inside the storage it reads.
*/
func (g *tableGen) emitFixedWriteCheck(st *ir.Struct) {
	if !ir.TableFixedClampNeeded(st) {
		return
	}
	g.pf("/* %s's write contract (docs/SPEC.md §5.1): 0 when the record holds a value\n", st.Name)
	g.pf("   its declaration does not admit, which the fixed save refuses debug-only. */\n")
	g.pf("static SCHEMA_UNUSED int %s( const %s * value )\n{\n", g.sym(st.Name, "fixed_write_check"), st.Name)
	g.pf("    int32_t out_of_range = 0;\n    int32_t damaged = 0;\n")
	g.pf("    %s( value, &out_of_range, &damaged );\n", g.sym(st.Name, "fixed_clamp_body"))
	g.pf("    return out_of_range == 0 && damaged == 0;\n}\n\n")
}

/*
emitFixedFitsFn is THE STORAGE BOUND (docs/SPEC.md §5.1): a counted array's

	live count and a text's used length say how far the store reads into the
	record, so one past its storage is refused in EVERY build, where the
	contract on values is debug-only. <t>_fixed_fits is 0 when a record holds
	one; the fixed save asks it of every record before any other pass and
	before the first byte. It walks what the store walks, asks each count
	BEFORE the loop it bounds, and asks nothing else.
*/
func (g *tableGen) emitFixedFitsFn(st *ir.Struct) {
	if !fixedFitsNeeded(st, map[string]bool{}) {
		return
	}
	g.pf("/* %s's storage bound (docs/SPEC.md §5.1): 0 when a count or a used length\n", st.Name)
	g.pf("   is past the storage that holds it, which the fixed save refuses in every build. */\n")
	g.pf("static SCHEMA_UNUSED int %s( const %s * value )\n{\n", g.sym(st.Name, "fixed_fits"), st.Name)
	g.fixedLoop = 0
	for _, f := range st.Fields {
		g.emitFixedFitsField(f, "value->", 4)
	}
	g.pf("    return 1;\n}\n\n")
}

func (g *tableGen) emitFixedFitsField(f *ir.Field, val string, indent int) {
	if !fixedFitsNeededField(f, map[string]bool{}) {
		return
	}
	if f.Type.Optional {
		/* an absent optional's payload is not written, so it is not read */
		ind := strings.Repeat(" ", indent)
		g.pf("%sif ( %s%s_present )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedFitsPayload(f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedFitsPayload(f, val, indent)
}

func (g *tableGen) emitFixedFitsPayload(f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case f.KeyEnum != "":
		g.emitFixedFitsLoop(f, strconv.FormatInt(f.KeyEnumRef.Max, 10), val+f.Name, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedFitsLoop(f, strconv.FormatInt(f.ArrayBound, 10), val+f.Name, indent)
	case f.Array == ir.ArrayCounted:
		count := fmt.Sprintf("%s%s_count", val, f.Name)
		g.pf("%sif ( %s < 0 || %s > %d ) { return 0; } /* the count indexes the storage (§3.4) */\n", ind, count, count, f.ArrayBound)
		g.emitFixedFitsLoop(f, "(int64_t) "+count, val+f.Name, indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		length := fmt.Sprintf("%s%s_length", val, f.Name)
		g.pf("%sif ( %s < 0 || %s > %d ) { return 0; } /* the length indexes the storage (§3.4) */\n", ind, length, length, f.Type.Size)
	default:
		g.emitFixedFitsElement(f, val+f.Name, indent)
	}
}

func (g *tableGen) emitFixedFitsLoop(f *ir.Field, count, expr string, indent int) {
	if !fixedFitsNeededElement(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	i := fixedLoopVar(g.fixedLoop)
	g.fixedLoop++
	g.pf("%s{\n%s    int64_t %s;\n%s    for ( %s = 0; %s < %s; ++%s )\n%s    {\n", ind, ind, i, ind, i, i, count, i, ind)
	g.emitFixedFitsElement(f, expr+"["+i+"]", indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
	g.fixedLoop--
}

func (g *tableGen) emitFixedFitsElement(f *ir.Field, expr string, indent int) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	ind := strings.Repeat(" ", indent)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedFitsNeeded(r, map[string]bool{}) {
			g.pf("%sif ( !%s( &%s ) ) { return 0; }\n", ind, g.sym(r.Name, "fixed_fits"), expr)
		}
	case *ir.Union:
		/* the store writes the set arm only, so only the set arm is asked */
		if !fixedFitsNeededElement(f, map[string]bool{}) {
			return
		}
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !fixedFitsNeededField(v.F, map[string]bool{}) {
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
			g.emitFixedFitsPayload(&arm, val, indent+8)
			g.pf("%s        break;\n%s    }\n", ind, ind)
		}
		g.pf("%s    default: break;\n%s}\n", ind, ind)
	}
}

// fixedFitsNeeded is whether a record holds a count or a used length the store
// indexes by, anywhere in its fixed closure: the fields fixedWriteBound
// asserts on, and the structs and union arms that reach one.
func fixedFitsNeeded(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedFitsNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedFitsNeededField(f *ir.Field, seen map[string]bool) bool {
	if f.Array == ir.ArrayCounted || f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return true
	}
	return fixedFitsNeededElement(f, seen)
}

func fixedFitsNeededElement(f *ir.Field, seen map[string]bool) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return fixedFitsNeeded(r, seen)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.F != nil && fixedFitsNeededField(v.F, seen) {
				return true
			}
		}
	}
	return false
}
