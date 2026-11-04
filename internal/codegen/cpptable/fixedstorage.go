package cpptable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

/* ---- the storage bound ------------------------------------------------------

A COUNT OR A LENGTH PAST ITS STORAGE IS REFUSED IN EVERY BUILD (docs/SPEC.md
§5.1). The writer's contract above is debug-only because it guards VALUES; a
counted array's live count and a text's used length are different in kind:
they say how far the store reads into the record, and one past its storage
reads memory the record does not own. <T>FixedFits is false when a record holds
one, and FixedSave asks it of every record before any other question and before
the first byte, so a refused batch leaves the buffer as the caller handed it
over. It walks what the store walks, asks each count BEFORE the loop it bounds,
and asks nothing else: a release save pays one compare per count and length. */

func (g *tableGen) emitFixedFitsFn(st *ir.Struct) {
	if !ir.TableFixedStorageBoundNeeded(st) {
		return
	}
	g.fixedOwner = st
	g.pf("// %s's storage bound (docs/SPEC.md §5.1): false when a count or a used\n", st.Name)
	g.pf("// length is past the storage that holds it, which FixedSave refuses in every build.\n")
	g.pf("TABLE_FIXED_INLINE bool %sFixedFits( const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    (void) value;\n")
	for _, f := range st.Fields {
		g.emitFixedFitsField(f, "value", 0, 4)
	}
	g.pf("    return true;\n}\n\n")
}

func (g *tableGen) emitFixedFitsField(f *ir.Field, val string, depth, indent int) {
	if !ir.TableFixedStorageBoundNeededField(f) {
		return
	}
	if f.Type.Optional {
		// an absent optional's payload is not written, so it is not read
		ind := strings.Repeat(" ", indent)
		g.pf("%sif ( %s.%s_present )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedFitsPayload(f, val, depth, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedFitsPayload(f, val, depth, indent)
}

func (g *tableGen) emitFixedFitsPayload(f *ir.Field, val string, depth, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case f.KeyEnum != "":
		g.emitFixedFitsLoop(f, strconv.FormatInt(f.KeyEnumRef.Max, 10), val+"."+ir.TableFixedKeyedSlots(g.fixedOwner, f), depth, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedFitsLoop(f, strconv.FormatInt(f.ArrayBound, 10), val+"."+f.Name, depth, indent)
	case f.Array == ir.ArrayCounted:
		count := fmt.Sprintf("%s.%s_count", val, f.Name)
		g.pf("%sif ( %s < 0 || %s > %d ) { return false; } // the count indexes the storage (§3.4)\n", ind, count, count, f.ArrayBound)
		g.emitFixedFitsLoop(f, count, val+"."+f.Name, depth, indent)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		length := fmt.Sprintf("%s.%s_length", val, f.Name)
		g.pf("%sif ( %s < 0 || %s > %d ) { return false; } // the length indexes the storage (§3.4)\n", ind, length, length, f.Type.Size)
	default:
		g.emitFixedFitsElement(f, val+"."+f.Name, depth, indent)
	}
}

func (g *tableGen) emitFixedFitsLoop(f *ir.Field, count, expr string, depth, indent int) {
	if !ir.TableFixedStorageBoundNeededElement(f) {
		return
	}
	ind := strings.Repeat(" ", indent)
	i := fixedLoopVar(depth)
	g.pf("%sfor ( int64_t %s = 0; %s < (int64_t) %s; ++%s )\n%s{\n", ind, i, i, count, i, ind)
	g.emitFixedFitsElement(f, expr+"["+i+"]", depth+1, indent+4)
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitFixedFitsElement(f *ir.Field, expr string, depth, indent int) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	ind := strings.Repeat(" ", indent)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if ir.TableFixedStorageBoundNeeded(r) {
			g.pf("%sif ( !%sFixedFits( %s ) ) { return false; }\n", ind, r.Name, expr)
		}
	case *ir.Union:
		// the store writes the set arm only, so only the set arm is asked
		if !ir.TableFixedStorageBoundNeededElement(f) {
			return
		}
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !ir.TableFixedStorageBoundNeededField(v.F) {
				continue
			}
			arm, val := *v.F, expr
			if armCompanioned(v) {
				arm.Name, val = "value", expr+"."+v.Name
			} else {
				arm.Name = v.Name
			}
			g.pf("%s    case %sType::%s:\n%s    {\n", ind, f.Type.Name, ir.GoExportName(v.Name), ind)
			g.emitFixedFitsPayload(&arm, val, depth, indent+8)
			g.pf("%s        break;\n%s    }\n", ind, ind)
		}
		g.pf("%s    default: break;\n%s}\n", ind, ind)
	}
}
