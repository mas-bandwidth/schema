package cpptable

import (
	"fmt"
	"math"
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

/* ---- the fixed form's slack pass -------------------------------------------

THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
§3.4): after every fixed-form load, by the identity plan or a lineage plan,
every element slot past a counted array's live count holds its declared
default — what a fresh value (its member initializers, <T>Reset) holds there.
This leg's value IS the record body, so the run lands the writer's dead bytes,
which are zeros, into those slots; zero is the declared default only where the
element's reset image is all zero. So the pass is emitted ONLY for a counted
array whose element reset image holds a non-zero byte, and it descends only
into types that (transitively) hold such an array: through every slot of a
fixed or keyed array, the live elements of a counted array, a union's set arm
and a present optional. A unit with no such array generates exactly what it did
before (the zero-cost rule), and where the pass is emitted it is one tight loop
of in-place resets per array. It runs after the bounds pass, over counts the
run has already clamped to [0, N]; a refusal resets the whole value anyway. */

// cppResetNonZeroStruct is whether <T>Reset leaves a non-zero byte anywhere
// in the value. It mirrors emitTableResetAt field for field.
func cppResetNonZeroStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	defer delete(seen, st.Name)
	for _, f := range st.Fields {
		if cppResetNonZeroField(f, seen) {
			return true
		}
	}
	return false
}

func cppResetNonZeroField(f *ir.Field, seen map[string]bool) bool {
	if f.IsList() || f.IsMap() {
		return false
	}
	if f.Type.Pointer {
		return f.Array == ir.ArrayCounted && f.BornCount() != 0
	}
	switch {
	case f.Type.TextUnbounded():
		return false
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		return hasByteDefault(f)
	case f.KeyEnum != "", f.Array == ir.ArrayFixed:
		return cppResetNonZeroElement(f, seen)
	case f.Array == ir.ArrayCounted:
		return f.BornCount() != 0 || cppResetNonZeroElement(f, seen)
	case cppSelfInit(f.Type):
		return cppResetNonZeroElement(f, seen)
	}
	return cppScalarDefaultNonZero(f)
}

// cppSelfInit is cppFieldType's second answer without its side effects: a
// struct (its own or a native mapping of the same layout) or a union.
func cppSelfInit(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch t.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// cppResetNonZeroElement is whether ONE element of f (or f itself, when it is
// a single struct or union) resets to a non-zero byte. A scalar array's reset
// is a memset (emitTableResetArray), so only a struct or a union element can.
func cppResetNonZeroElement(f *ir.Field, seen map[string]bool) bool {
	if !cppSelfInit(f.Type) {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return cppResetNonZeroStruct(r, seen)
	case *ir.Union:
		// emitTableResetOne: each arm reset at its overlay storage
		// (emitTableResetArm), then the tag at None, which is 0
		for _, v := range r.Variants {
			if v.F == nil || v.F.Type.Pointer && v.F.Array == ir.ArrayNone {
				continue
			}
			if armCompanioned(v) {
				if v.F.Array == ir.ArrayCounted && !v.F.Type.Pointer && cppResetNonZeroElement(v.F, seen) {
					return true
				}
				continue
			}
			if v.F.Array == ir.ArrayFixed {
				if !v.F.Type.Pointer && cppResetNonZeroElement(v.F, seen) {
					return true
				}
				continue
			}
			if cppResetNonZeroField(v.F, seen) {
				return true
			}
		}
	}
	return false
}

// cppScalarDefaultNonZero is whether a scalar's declared default
// (fieldDefaultExpr) is a non-zero bit pattern. A float's -0 is non-zero bits.
func cppScalarDefaultNonZero(f *ir.Field) bool {
	if !f.HasDefault {
		return false
	}
	switch f.Type.Kind {
	case ir.TBool:
		return f.DefBool
	case ir.TFloat32, ir.TFloat64:
		return f.DefFloat != 0 || math.Signbit(f.DefFloat)
	case ir.TInt, ir.TBits, ir.TFixed:
		return f.DefInt != nil && f.DefInt.Sign() != 0
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return f.DefVariant != "" // None is 0; every variant packs from 1
		case *ir.Flags:
			return f.DefInt != nil && f.DefInt.Sign() != 0
		}
	}
	return false
}

// fixedSlackOwn is whether f is a counted array whose slots past the count
// the pass writes: its element resets to a non-zero byte.
func fixedSlackOwn(f *ir.Field) bool {
	return f.Array == ir.ArrayCounted && !f.Type.Pointer && !f.IsList() && !f.IsMap() &&
		cppResetNonZeroElement(f, map[string]bool{})
}

// fixedSlackNeededStruct is whether a fixed type holds, anywhere a load lands
// a live value, a counted array the pass writes. A type that holds none gets
// no pass and no call (§2.2's zero-cost rule).
func fixedSlackNeededStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if fixedSlackNeededField(f, seen) {
			return true
		}
	}
	return false
}

func fixedSlackNeededField(f *ir.Field, seen map[string]bool) bool {
	if f.Type.Pointer || f.IsList() || f.IsMap() {
		return false
	}
	if fixedSlackOwn(f) {
		return true
	}
	return fixedSlackNeededElement(f, seen)
}

// fixedSlackNeededElement is whether one element of f (or f itself) needs the
// pass inside it.
func fixedSlackNeededElement(f *ir.Field, seen map[string]bool) bool {
	if f.Type.Kind != ir.TNamed || f.Type.Pointer {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return fixedSlackNeededStruct(r, seen)
	case *ir.Union:
		for _, v := range r.Variants {
			if v.F != nil && fixedSlackNeededField(v.F, seen) {
				return true
			}
		}
	}
	return false
}

// emitFixedSlackBodyFn emits <T>FixedSlack, when <T> needs one.
func (g *tableGen) emitFixedSlackBodyFn(st *ir.Struct) {
	if !fixedSlackNeededStruct(st, map[string]bool{}) {
		return
	}
	g.fixedOwner = st
	owner := g.owner
	g.owner = st
	defer func() { g.owner = owner }()
	g.pf("// %s's slack pass (docs/SPEC-TABLES.md §3.4, THE SLOTS PAST A COUNT AFTER\n", st.Name)
	g.pf("// ANY LOAD): every slot past a counted array's live count holds what a fresh\n")
	g.pf("// value holds there, never the writer's dead zeros.\n")
	g.pf("TABLE_FIXED_INLINE void %sFixedSlack( %s & value )\n{\n", st.Name, st.Name)
	for _, f := range st.Fields {
		if !fixedSlackNeededField(f, map[string]bool{}) {
			continue
		}
		if f.Type.Optional {
			// an absent optional's payload is its defaults image already (§3.4)
			g.pf("    if ( value.%s_present )\n    {\n", f.Name)
			g.emitFixedSlackField(f, "value", 0, 8)
			g.pf("    }\n")
			continue
		}
		g.emitFixedSlackField(f, "value", 0, 4)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedSlackField(f *ir.Field, val string, depth, indent int) {
	switch {
	case f.KeyEnum != "":
		// EVERY SLOT OF A KEYED ARRAY IS LIVE (§2.4)
		g.emitFixedSlackArray(f, val+"."+ir.TableFixedKeyedSlots(g.fixedOwner, f), "", f.KeyEnumRef.Max, depth, indent)
	case f.Array == ir.ArrayFixed:
		g.emitFixedSlackArray(f, val+"."+f.Name, "", f.ArrayBound, depth, indent)
	case f.Array == ir.ArrayCounted:
		g.emitFixedSlackArray(f, val+"."+f.Name, val+"."+f.Name+"_count", f.ArrayBound, depth, indent)
	default:
		g.emitFixedSlackElement(f, val+"."+f.Name, depth, indent)
	}
}

// emitFixedSlackArray walks one array: into every live element that needs the
// pass, and, for a counted array the pass writes, over every slot past the
// count, each reset in place (emitTableResetArray's form, for the same
// -Wstringop-overflow reason). count is "" for a fixed or keyed array.
func (g *tableGen) emitFixedSlackArray(f *ir.Field, elems, count string, bound int64, depth, indent int) {
	ind := strings.Repeat(" ", indent)
	i := fixedLoopVar(depth)
	if fixedSlackNeededElement(f, map[string]bool{}) {
		limit := fmt.Sprintf("%d", bound)
		if count != "" {
			limit = fmt.Sprintf("(int64_t) %s && %s < %d", count, i, bound)
		}
		g.pf("%sfor ( int64_t %s = 0; %s < %s; ++%s )\n%s{\n", ind, i, i, limit, i, ind)
		g.emitFixedSlackElement(f, elems+"["+i+"]", depth+1, indent+4)
		g.pf("%s}\n", ind)
	}
	if count == "" || !fixedSlackOwn(f) {
		return
	}
	// the run clamped the count to [0, N]; a negative one walks nothing
	g.pf("%sfor ( int64_t %s = (int64_t) %s; %s >= 0 && %s < %d; ++%s )\n%s{\n", ind, i, count, i, i, bound, i, ind)
	typ, _ := g.cppFieldType(f.Type)
	saved := g.indent
	g.indent = saved + strings.Repeat(" ", indent)
	g.emitTableResetOne(elems+"["+i+"]", typ, f)
	g.indent = saved
	g.pf("%s}\n", ind)
}

// emitFixedSlackElement descends into ONE live element: a struct's own pass,
// or a union's set arm.
func (g *tableGen) emitFixedSlackElement(f *ir.Field, expr string, depth, indent int) {
	if f.Type.Kind != ir.TNamed || f.Type.Pointer {
		return
	}
	ind := strings.Repeat(" ", indent)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedSlackNeededStruct(r, map[string]bool{}) {
			g.pf("%s%sFixedSlack( %s );\n", ind, r.Name, expr)
		}
	case *ir.Union:
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !fixedSlackNeededField(v.F, map[string]bool{}) {
				continue
			}
			g.pf("%s    case %sType::%s:\n%s    {\n", ind, f.Type.Name, ir.GoExportName(v.Name), ind)
			switch v.F.Array {
			case ir.ArrayFixed:
				g.emitFixedSlackArray(v.F, armValue(expr, v), "", v.F.ArrayBound, depth, indent+8)
			case ir.ArrayCounted:
				g.emitFixedSlackArray(v.F, armValue(expr, v), armCount(expr, v), v.F.ArrayBound, depth, indent+8)
			default:
				g.emitFixedSlackElement(v.F, armValue(expr, v), depth, indent+8)
			}
			g.pf("%s        break;\n%s    }\n", ind, ind)
		}
		g.pf("%s    default: break;\n%s}\n", ind, ind)
	}
}
