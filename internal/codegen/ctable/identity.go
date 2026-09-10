// THE IDENTITY READ (docs/SPEC-TABLES.md §3.4), C shape.
//
// Three facts on the identity path:
//
//	(a) no default prefill on the identity path — this hash wrote every field
//	(b) count and text clamps run straight-line AFTER the copy, so the identity
//	    plan is copies only and adjacent runs coalesce without those ops in
//	    the way
//	(c) where the C ABI is the wire (ir.TableFixedFlatType), the read is one
//	    memcpy and the scatter is empty
//
// Packed layout is not forced. A record with padding, a count sitting after
// its array, or a string's extra terminator byte still scatters through the
// copy plan. The wire bytes do not move.
package ctable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableFixedIdentityCopies rewrites one identity plan so count and text are
// COPY ops (the clamp bound and the text flavour live in the straight-line
// that runs after). The coalescer then runs again: neighbours that those ops
// used to split can become one run.
func tableFixedIdentityCopies(plan []ir.TableFixedLeaf) (out []ir.TableFixedLeaf, guarded int) {
	raw := make([]ir.TableFixedLeaf, 0, len(plan)+4)
	for _, e := range plan {
		switch e.Op {
		case ir.TableFixedOpCount:
			e.Op = ir.TableFixedOpCopy
			e.Size = ir.TableFixedCountBytes
			e.Arg = 0
			raw = append(raw, e)
		case ir.TableFixedOpText:
			length := e
			length.Op = ir.TableFixedOpCopy
			length.Size = ir.TableFixedCountBytes
			length.Aux = 0
			length.Arg = 0
			length.Note = e.Note + " length"
			content := e
			content.Op = ir.TableFixedOpCopy
			content.Src = e.Src + ir.TableFixedCountBytes
			content.Dst = e.Aux
			content.Aux = 0
			content.Arg = 0
			raw = append(raw, length, content)
		default:
			raw = append(raw, e)
		}
	}
	return tableFixedCoalesceCopies(raw)
}

func tableFixedCoalesceCopies(raw []ir.TableFixedLeaf) (plan []ir.TableFixedLeaf, guarded int) {
	for pass := range 2 {
		for _, e := range raw {
			if (e.Guard != ir.TableFixedNoGuard) != (pass == 1) {
				continue
			}
			if len(plan) > guarded {
				last := &plan[len(plan)-1]
				if last.Op == ir.TableFixedOpCopy && e.Op == ir.TableFixedOpCopy &&
					last.Guard == e.Guard && last.Arg == e.Arg &&
					last.Src+last.Size == e.Src && last.Dst+last.Size == e.Dst {
					last.Size += e.Size
					continue
				}
			}
			plan = append(plan, e)
		}
		if pass == 0 {
			guarded = len(plan)
		}
	}
	return plan, guarded
}

func fixedTypeNeedsIdentityClamps(u *ir.Unit, st *ir.Struct) bool {
	for _, f := range st.Fields {
		if fixedFieldNeedsIdentityClamps(u, f) {
			return true
		}
	}
	return false
}

func fixedFieldNeedsIdentityClamps(u *ir.Unit, f *ir.Field) bool {
	if f.Array == ir.ArrayCounted {
		return true
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes:
		return true
	}
	if f.Type.Kind != ir.TNamed {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return fixedTypeNeedsIdentityClamps(u, r)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.F != nil && fixedFieldNeedsIdentityClamps(u, v.F) {
				return true
			}
		}
	}
	return false
}

func (g *tableGen) emitFixedIdentityClamps(st *ir.Struct) {
	g.pf("/* COUNT AND TEXT CLAMPS, straight-line after the identity copy. A record\n")
	g.pf("   under this build's own hash can still carry a count this build does not\n")
	g.pf("   admit; the plan no longer names those ops, so this is what holds them. */\n")
	g.pf("static SCHEMA_UNUSED %s void %s( %s * value, TableReport * report )\n{\n",
		tableInlineMacro(g.unit.Package), g.api(st.Name, "fixed_identity_clamps"), st.Name)
	g.pf("    (void) report;\n")
	for _, f := range st.Fields {
		g.emitFixedIdentityClampField(f, "value->", 4)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedIdentityClampField(f *ir.Field, val string, indent int) {
	if !fixedFieldNeedsIdentityClamps(g.unit, f) {
		return
	}
	ind := strings.Repeat(" ", indent)
	switch {
	case f.KeyEnum != "":
		g.emitFixedIdentityClampElems(f, val+f.Name, f.KeyEnumRef.Max, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedIdentityClampElems(f, val+f.Name, f.ArrayBound, indent)
	case f.Array == ir.ArrayCounted:
		g.emitFixedIdentityClampInt(fmt.Sprintf("%s%s_count", val, f.Name), f.ArrayBound, ind)
		g.emitFixedIdentityClampElems(f, val+f.Name, f.ArrayBound, indent)
	case f.Type.Kind == ir.TString:
		g.emitFixedIdentityClampInt(fmt.Sprintf("%s%s_length", val, f.Name), f.Type.Size, ind)
		g.pf("%s%s%s[%s%s_length] = 0;\n", ind, val, f.Name, val, f.Name)
	case f.Type.Kind == ir.TWString:
		g.emitFixedIdentityClampInt(fmt.Sprintf("%s%s_length", val, f.Name), f.Type.Size, ind)
		g.pf("%s%s%s[%s%s_length] = 0;\n", ind, val, f.Name, val, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.emitFixedIdentityClampInt(fmt.Sprintf("%s%s_length", val, f.Name), f.Type.Size, ind)
	default:
		if f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if fixedTypeNeedsIdentityClamps(g.unit, r) {
				g.pf("%s%s( &%s%s, report );\n", ind, g.api(r.Name, "fixed_identity_clamps"), val, f.Name)
			}
		case *ir.Union:
			g.emitFixedIdentityClampUnion(f, r, val, indent)
		}
	}
}

func (g *tableGen) emitFixedIdentityClampInt(expr string, bound int64, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s    int32_t n = %s;\n", ind, expr)
	g.pf("%s    if ( n < 0 ) { n = 0; report->clamped++; } else if ( n > %d ) { n = %d; report->clamped++; }\n",
		ind, bound, bound)
	g.pf("%s    %s = n;\n", ind, expr)
	g.pf("%s}\n", ind)
}

func (g *tableGen) emitFixedIdentityClampElems(f *ir.Field, expr string, count int64, indent int) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if !fixedTypeNeedsIdentityClamps(g.unit, r) {
			return
		}
		ind := strings.Repeat(" ", indent)
		g.pf("%s{\n%s    int64_t i;\n%s    for ( i = 0; i < %s; ++i )\n%s    {\n",
			ind, ind, ind, strconv.FormatInt(count, 10), ind)
		g.pf("%s        %s( &%s[i], report );\n", ind, g.api(r.Name, "fixed_identity_clamps"), expr)
		g.pf("%s    }\n%s}\n", ind, ind)
	case *ir.Union:
		if !fixedUnionNeedsIdentityClamps(g.unit, r) {
			return
		}
		ind := strings.Repeat(" ", indent)
		g.pf("%s{\n%s    int64_t i;\n%s    for ( i = 0; i < %s; ++i )\n%s    {\n",
			ind, ind, ind, strconv.FormatInt(count, 10), ind)
		elem := *f
		elem.Array = ir.ArrayNone
		elem.KeyEnum = ""
		elem.Name = ""
		g.emitFixedIdentityClampUnion(&elem, r, expr+"[i].", indent+8)
		g.pf("%s    }\n%s}\n", ind, ind)
	}
}

func fixedUnionNeedsIdentityClamps(u *ir.Unit, un *ir.Union) bool {
	for _, v := range un.Variants {
		if v.F != nil && fixedFieldNeedsIdentityClamps(u, v.F) {
			return true
		}
	}
	return false
}

func (g *tableGen) emitFixedIdentityClampUnion(f *ir.Field, un *ir.Union, val string, indent int) {
	if !fixedUnionNeedsIdentityClamps(g.unit, un) {
		return
	}
	ind := strings.Repeat(" ", indent)
	g.pf("%sswitch ( %s%s.type )\n%s{\n", ind, val, f.Name, ind)
	for _, v := range un.Variants {
		if v.F == nil || !fixedFieldNeedsIdentityClamps(g.unit, v.F) {
			continue
		}
		g.pf("%s    case %s:\n%s    {\n", ind, enumConst(f.Type.Name+"Type", v.Name), ind)
		g.emitFixedIdentityClampField(v.F, fmt.Sprintf("%s%s.as.", val, f.Name), indent+8)
		g.pf("%s        break;\n%s    }\n", ind, ind)
	}
	g.pf("%s    default: break;\n%s}\n", ind, ind)
}
