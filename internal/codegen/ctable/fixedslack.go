package ctable

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

/* ---- the fixed form's slack pass -------------------------------------------

THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
§3.4): after every fixed-form load, by the identity plan or a lineage plan,
every element slot past a counted array's live count holds its declared
default — what a fresh value (<T>_reset) holds there. This leg's value IS the
record body, so the run lands the writer's dead bytes, which are zeros, into
those slots; zero is the declared default only where the element's reset image
is all zero. So the pass is emitted ONLY for a counted array whose element
reset image holds a non-zero byte, and it descends only into types that
(transitively) hold such an array: through every slot of a fixed or keyed
array, the live elements of a counted array, a union's set arm and a present
optional. A unit with no such array generates exactly what it did before (the
zero-cost rule), and where the pass is emitted it is one reset and a tight copy
loop per array. It runs after the bounds pass, once the run has clamped every
count to [0, N]; a refusal resets the whole value through <T>_reset anyway. */

// cResetNonZeroStruct is whether <T>_reset leaves a non-zero byte anywhere in
// the value. It mirrors emitTableResetField field for field.
func cResetNonZeroStruct(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	defer delete(seen, st.Name)
	for _, f := range st.Fields {
		if cResetNonZeroField(f, seen) {
			return true
		}
	}
	return false
}

func cResetNonZeroField(f *ir.Field, seen map[string]bool) bool {
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
		return f.HasDefault && len(f.DefBytes) != 0
	case f.KeyEnum != "", f.Array == ir.ArrayFixed:
		return cResetNonZeroElement(f, seen)
	case f.Array == ir.ArrayCounted:
		return f.BornCount() != 0 || cResetNonZeroElement(f, seen)
	case cSelfInit(f.Type):
		return cResetNonZeroElement(f, seen)
	}
	return cScalarDefaultNonZero(f)
}

// cResetNonZeroElement is whether ONE element of f (or f itself, when it is a
// single struct or union) resets to a non-zero byte. A scalar array's reset is
// a memset (emitTableResetArray), so only a struct or a union element can.
func cResetNonZeroElement(f *ir.Field, seen map[string]bool) bool {
	if !cSelfInit(f.Type) {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		return cResetNonZeroStruct(r, seen)
	case *ir.Union:
		// emitTableResetOne: a memset, each self-initialising arm's defaults
		// over it (emitTableResetArm), and the tag at None
		for _, v := range r.Variants {
			if v.Void() || v.F == nil || v.F.Type.Pointer || v.F.IsList() || v.F.IsMap() || !cSelfInit(v.F.Type) {
				continue
			}
			if cResetNonZeroElement(v.F, seen) {
				return true
			}
		}
	}
	return false
}

// cScalarDefaultNonZero is whether a scalar's declared default (fieldDefaultExpr)
// is a non-zero bit pattern. A float's -0 is non-zero bits.
func cScalarDefaultNonZero(f *ir.Field) bool {
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
		cResetNonZeroElement(f, map[string]bool{})
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
	if f.Type.Kind != ir.TNamed {
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

// emitFixedSlackBodyFn emits <T>'s slack pass, when <T> needs one.
func (g *tableGen) emitFixedSlackBodyFn(st *ir.Struct) {
	if !fixedSlackNeededStruct(st, map[string]bool{}) {
		return
	}
	g.pf("/* %s's slack pass (docs/SPEC-TABLES.md §3.4, THE SLOTS PAST A COUNT AFTER\n", st.Name)
	g.pf("   ANY LOAD): every slot past a counted array's live count holds what a\n")
	g.pf("   fresh value holds there, never the writer's dead zeros. */\n")
	g.pf("static SCHEMA_UNUSED void %s( %s * value )\n{\n", g.sym(st.Name, "fixed_slack"), st.Name)
	g.fixedLoop = 0
	for _, f := range st.Fields {
		g.emitFixedSlackField(f, "value->", 4)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitFixedSlackField(f *ir.Field, val string, indent int) {
	if !fixedSlackNeededField(f, map[string]bool{}) {
		return
	}
	ind := strings.Repeat(" ", indent)
	if f.Type.Optional {
		// an absent optional's payload is its defaults image already (§3.4)
		g.pf("%sif ( %s%s_present )\n%s{\n", ind, val, f.Name, ind)
		g.emitFixedSlackPayload(f, val, indent+4)
		g.pf("%s}\n", ind)
		return
	}
	g.emitFixedSlackPayload(f, val, indent)
}

func (g *tableGen) emitFixedSlackPayload(f *ir.Field, val string, indent int) {
	ind := strings.Repeat(" ", indent)
	inner := fixedSlackNeededElement(f, map[string]bool{})
	switch {
	case f.KeyEnum != "":
		if inner {
			g.emitFixedSlackLoop(f, "0", strconv.FormatInt(f.KeyEnumRef.Max, 10), val+f.Name, indent)
		}
	case f.Array == ir.ArrayFixed:
		if inner {
			g.emitFixedSlackLoop(f, "0", strconv.FormatInt(f.ArrayBound, 10), val+f.Name, indent)
		}
	case f.Array == ir.ArrayCounted:
		count := fmt.Sprintf("(int64_t) %s%s_count", val, f.Name)
		if inner {
			g.emitFixedSlackLoop(f, "0", count, val+f.Name, indent)
		}
		if fixedSlackOwn(f) {
			// one reset at the first dead slot and a copy across the rest; the
			// run clamped the count to [0, N], and the guard keeps the
			// subscript inside the array whatever it holds
			expr := val + f.Name
			i := fixedLoopVar(g.fixedLoop)
			g.pf("%sif ( %s >= 0 && %s < %d )\n%s{\n", ind, count, count, f.ArrayBound, ind)
			g.pf("%s    const int64_t dead = %s;\n", ind, count)
			g.pf("%s    int64_t %s;\n", ind, i)
			g.emitFixedSlackResetOne(f, expr+"[dead]", ind+"    ")
			g.pf("%s    for ( %s = dead + 1; %s < %d; ++%s ) { %s[%s] = %s[dead]; }\n", ind, i, i, f.ArrayBound, i, expr, i, expr)
			g.pf("%s}\n", ind)
		}
	default:
		g.emitFixedSlackElement(f, val+f.Name, indent)
	}
}

// emitFixedSlackResetOne gives one element slot what a fresh value holds there:
// the element type's own reset for a struct, and emitTableResetOne's union
// image (a memset, each arm's defaults, the tag at None) for a union.
func (g *tableGen) emitFixedSlackResetOne(f *ir.Field, expr, ind string) {
	if _, ok := f.Type.Ref.(*ir.Struct); ok {
		g.pf("%s%s( &%s );\n", ind, g.api(g.cFieldType(f.Type), "reset"), expr)
		return
	}
	un := f.Type.Ref.(*ir.Union)
	g.pf("%smemset( &%s, 0, sizeof( %s ) );\n", ind, expr, expr)
	for _, v := range un.Variants {
		if v.Void() || v.F == nil || v.F.Type.Pointer || v.F.IsList() || v.F.IsMap() || !cSelfInit(v.F.Type) {
			continue
		}
		arm := v.F

		at := armValue(expr, v)
		switch {
		case arm.KeyEnum != "", arm.Array == ir.ArrayFixed, arm.Array == ir.ArrayCounted:
			bound := strconv.FormatInt(arm.ArrayBound, 10)
			if arm.KeyEnum != "" {
				bound = enumMaxConst(arm.KeyEnum)
			}
			g.pf("%s{\n", ind)
			g.emitFixedSlackResetOne(arm, at+"[0]", ind+"    ")
			g.pf("%s    { int32_t r; for ( r = 1; r < (int32_t) ( %s ); r++ ) { %s[r] = %s[0]; } }\n", ind, bound, at, at)
			g.pf("%s}\n", ind)
		default:
			g.emitFixedSlackResetOne(arm, at, ind)
		}
	}
	g.pf("%s%s.type = 0;\n", ind, expr)
}

func (g *tableGen) emitFixedSlackLoop(f *ir.Field, from, count, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	i := fixedLoopVar(g.fixedLoop)
	g.fixedLoop++
	g.pf("%s{\n%s    int64_t %s;\n%s    for ( %s = %s; %s < %s; ++%s )\n%s    {\n", ind, ind, i, ind, i, from, i, count, i, ind)
	g.emitFixedSlackElement(f, expr+"["+i+"]", indent+8)
	g.pf("%s    }\n%s}\n", ind, ind)
	g.fixedLoop--
}

// emitFixedSlackElement descends into ONE live element: a struct's own pass,
// or a union's set arm.
func (g *tableGen) emitFixedSlackElement(f *ir.Field, expr string, indent int) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	ind := strings.Repeat(" ", indent)
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		if fixedSlackNeededStruct(r, map[string]bool{}) {
			g.pf("%s%s( &%s );\n", ind, g.sym(r.Name, "fixed_slack"), expr)
		}
	case *ir.Union:
		g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
		for _, v := range r.Variants {
			if v.F == nil || !fixedSlackNeededField(v.F, map[string]bool{}) {
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
			g.emitFixedSlackPayload(&arm, val, indent+8)
			g.pf("%s        break;\n%s    }\n", ind, ind)
		}
		g.pf("%s    default: break;\n%s}\n", ind, ind)
	}
}
