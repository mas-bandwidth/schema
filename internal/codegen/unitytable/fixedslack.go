package unitytable

// THE SLOTS PAST A COUNT AFTER ANY LOAD (docs/SPEC-TABLES.md §3.4, THE SLOTS
// PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED): every element slot past a
// counted array's count holds what a fresh value holds there, the element's
// declared defaults.
//
// The Unity leg's fresh value is Init<T>, and the refusal path resets every
// row with it, so two places carry the rule. Init<T> builds every slot of a
// counted array of structs (the whole bound, not only the born count), and
// the fixed decode, after its live loop, puts each slot past the count back
// with Init<Elem>: the decode is the one function both the identity read and a
// lineage plan's image pass through, at every depth (a live element's, a live
// arm's, a present optional's own counted array).
//
// ZERO COST: only an array whose element struct's declared defaults are NOT
// the zero form (unitySlackStructNonZero) gets either change. Every other
// array, and every unit without one, emits what it emitted before.

import (
	"math"

	"github.com/mas-bandwidth/schema/v3/ir"
)

// unitySlackElem is the struct a counted array's slots past the count are
// reset to, or nil when its elements' fresh form is the zero form.
func unitySlackElem(f *ir.Field) *ir.Struct {
	if f.Array != ir.ArrayCounted || f.Type.Kind != ir.TNamed || f.Type.Pointer {
		return nil
	}
	inner, ok := f.Type.Ref.(*ir.Struct)
	if !ok || !structHasInitFn(inner) || !unitySlackStructNonZero(inner, map[string]bool{}) {
		return nil
	}
	return inner
}

// unitySlackStructNonZero is whether Init<T> writes anything over the zero
// form: the same walk emitInitFixedField makes, with the counted arrays of
// such structs taken at their whole bound.
func unitySlackStructNonZero(st *ir.Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	defer delete(seen, st.Name)
	for _, f := range st.Fields {
		if unitySlackFieldNonZero(f, seen) {
			return true
		}
	}
	return false
}

func unitySlackFieldNonZero(f *ir.Field, seen map[string]bool) bool {
	if f.Type.Optional {
		// Init leaves an optional absent and its payload at the zero form
		return false
	}
	inner, isStruct := f.Type.Ref.(*ir.Struct)
	isStruct = isStruct && f.Type.Kind == ir.TNamed
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return f.HasDefault && len(f.DefBytes) > 0
	case f.Array != ir.ArrayNone:
		if f.Array == ir.ArrayCounted && f.BornCount() > 0 {
			return true
		}
		return isStruct && structHasInitFn(inner) && unitySlackStructNonZero(inner, seen)
	case f.HasDefault:
		if _, ok := f.Type.Ref.(*ir.Enum); ok && f.Type.Kind == ir.TNamed {
			return f.DefVariant != ""
		}
		switch f.Type.Kind {
		case ir.TBool:
			return f.DefBool
		case ir.TFloat32:
			return math.Float32bits(float32(f.DefFloat)) != 0
		case ir.TFloat64:
			return math.Float64bits(f.DefFloat) != 0
		}
		return f.DefInt != nil && f.DefInt.Sign() != 0
	case isStruct:
		return structHasInitFn(inner) && unitySlackStructNonZero(inner, seen)
	}
	return false
}
