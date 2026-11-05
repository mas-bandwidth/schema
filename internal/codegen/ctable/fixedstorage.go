package ctable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

/*
THE STORAGE BOUND, the C twin of the reference's <T>FixedFits (docs/SPEC.md

	§5.1): a counted array's live count and a text's used length say how far
	the store reads into the record, so one past its storage is refused in
	EVERY build, where the writer's contract on values is debug-only.
	<t>_fixed_fits is 0 when a record holds one; the fixed save asks it of every
	record before any other question and before the first byte. It walks what
	the store walks, asks each count BEFORE the loop it bounds, and asks
	nothing else.
*/
func (g *tableGen) emitFixedFitsFn(st *ir.Struct) {
	if !ir.TableFixedStorageBoundNeeded(st) {
		return
	}
	g.pf("/* %s's storage bound (docs/SPEC.md §5.1): 0 when a count or a used length\n", st.Name)
	g.pf("   is past the storage that holds it, which the fixed save refuses in every build. */\n")
	g.pf("static SCHEMA_UNUSED int %s( const %s * value )\n{\n", g.sym(st.Name, "fixed_fits"), st.Name)
	g.pf("    (void) value;\n")
	g.fixedLoop = 0
	for _, f := range st.Fields {
		g.emitFixedFitsField(f, "value->", 4)
	}
	g.pf("    return 1;\n}\n\n")
}

func (g *tableGen) emitFixedFitsField(f *ir.Field, val string, indent int) {
	if !ir.TableFixedStorageBoundNeededField(f) {
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
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		length := fmt.Sprintf("%s%s_length", val, f.Name)
		g.pf("%sif ( %s < 0 || %s > %d ) { return 0; } /* the length indexes the storage (§3.4) */\n", ind, length, length, f.Type.Size)
	default:
		g.emitFixedFitsElement(f, val+f.Name, indent)
	}
}

func (g *tableGen) emitFixedFitsLoop(f *ir.Field, count, expr string, indent int) {
	if !ir.TableFixedStorageBoundNeededElement(f) {
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
		if ir.TableFixedStorageBoundNeeded(r) {
			g.pf("%sif ( !%s( &%s ) ) { return 0; }\n", ind, g.sym(r.Name, "fixed_fits"), expr)
		}
	case *ir.Union:
		/* the store writes the set arm only, so only the set arm is asked */
		if !ir.TableFixedStorageBoundNeededElement(f) {
			return
		}
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !ir.TableFixedStorageBoundNeededField(v.F) {
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
