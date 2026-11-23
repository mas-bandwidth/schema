package ir

// ---------------------------------------------------------------------------
// THE COUNT MINIMUM IS A BOUND OF EVERY RECORD THAT HOLDS IT (docs/SPEC.md
// §5.1 row 3)
//
// TableFixedClampNeeded asks about the bounds a read holds a VALUE to: a
// range, a float range, an ordinal, a union tag, text's content rule, a bits
// width. A counted array's declared minimum is a bound on its stored COUNT,
// which a fixed record carries over storage like any other number, and the
// bounds pass holds it in the same straight-line walk. A walk that skips a
// child because the child declares no value bound never reaches the child's
// minimum: a record holding `nest [1..4]Child` with `Child.vals [2..4]uint8`
// passed a child with one value (F-184 for C++, F-187 for C).
//
// These are the questions with the minimum in, at any depth: a record in a
// record, an array of records, a union arm's payload, a record inside an arm.
// Every generator that walks a fixed record asks them, not the clamp's.
// ---------------------------------------------------------------------------

// TableFixedBoundsNeeded answers whether a fixed record's bounds pass has
// anything to hold anywhere in the type's closure: what TableFixedClampNeeded
// answers, or a counted array with a positive minimum at any depth.
func TableFixedBoundsNeeded(st *Struct) bool {
	return TableFixedClampNeeded(st) || tableFixedCountMinIn(st, map[string]bool{})
}

// TableFixedBoundsNeededField is the same question about one field: a bound
// on its value, its own count minimum, or either anywhere beneath it.
func TableFixedBoundsNeededField(f *Field) bool {
	return TableFixedClampNeededField(f) || tableFixedCountMinField(f, map[string]bool{})
}

// TableFixedBoundsNeededUnionArm is whether ANY arm's payload holds a bound,
// the count minimum included. The tag is a bound of its own and is held
// whatever this answers; a switch over arms none of which holds anything is
// not emitted.
func TableFixedBoundsNeededUnionArm(r *Union) bool {
	for _, v := range r.Variants {
		if v.F != nil && TableFixedBoundsNeededField(v.F) {
			return true
		}
	}
	return false
}

// TableFixedElementBoundsNeeded answers whether the ELEMENTS of an array
// field are walked: a bound on the element's value, or a counted minimum
// anywhere beneath the element. The array's OWN minimum is not an element's
// bound: the emitter holds it on the stored count before the walk, so a field
// whose one bound is its own minimum walks nothing. Whatever the array's own
// minimum, a child's minimum is reached.
func TableFixedElementBoundsNeeded(f *Field) bool {
	return TableFixedClampNeededField(f) || tableFixedCountMinElement(f, map[string]bool{})
}

func tableFixedCountMinIn(st *Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if tableFixedCountMinField(f, seen) {
			return true
		}
	}
	return false
}

func tableFixedCountMinField(f *Field, seen map[string]bool) bool {
	if f.Array == ArrayCounted && f.ArrayMin > 0 {
		return true
	}
	return tableFixedCountMinElement(f, seen)
}

// tableFixedCountMinElement is whether f's element type, a record or a union
// arm's payload at any depth, holds a counted array with a positive minimum.
// A pointer's referent is its own record and is walked from there.
func tableFixedCountMinElement(f *Field, seen map[string]bool) bool {
	if f.Type.Kind != TNamed || f.Type.Pointer {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *Struct:
		return tableFixedCountMinIn(r, seen)
	case *Union:
		for _, v := range r.Variants {
			if v.F != nil && tableFixedCountMinField(v.F, seen) {
				return true
			}
		}
	}
	return false
}
