// The TABLE closure's KIND census (docs/SPEC-TABLES.md §2.2, §4): every wire
// kind the unit DECLARES, whatever shape test an emitter's own call sites sit
// behind.
package ir

// TableDeclaredKinds returns the set of wire kinds the unit's TABLE CLOSURE
// declares: for every field of every closure member, its own scalar kind
// ([TableWireScalarKind], which a plain field, a union arm and a map key are
// compared under) and its ELEMENT kind ([TableWireElemKind], which a bounded
// array, a keyed body and a list are compared under), plus the same two for
// every arm of every union a closure member reaches, transitively.
//
// IT IS A DECLARATION SCAN AND NOT A REACHABILITY WALK, for the reason
// [BlobPointerFields] is: a backend that gates a DEFINITION on a census must
// not rest on a second walk agreeing with the one its own call sites are
// emitted from, because a sabotage or a later edit can part the two and the
// backend then withholds a definition it has already written a call to. A
// declaration scan is a SUPERSET of every such walk over the same unit.
//
// IT IS A SUPERSET OF THE CALL SITES TOO, deliberately (§4: "the rule is
// decided by the KIND PAIR and by nothing else"). A widening helper's call
// sites are emitted from several places apiece, each behind its own shape test
// — a plain scalar at a field, a widenable element at an array, a keyed body
// or a list, a plain scalar again at an arm, a map key's own test — and no
// census matches that union without spelling those tests a second time and
// growing a second way to be wrong. A pointer field of kind 11, a map of
// int64 values, an f64 the message form never reaches: each answers yes and
// costs a unit a helper it does not call. The cost of the superset is dead
// text; the cost of a subset is a translation unit that does not compile.
//
// THE CENSUS IS UNIT-LEVEL AND NOT PER-FILE, for the reason [WideTextFields]
// is: a runtime that sits behind one guard is defined by whichever header a
// translation unit reaches first, so the question is asked of the unit and
// never of a file.
func TableDeclaredKinds(u *Unit) map[int]bool {
	kinds := map[int]bool{}
	seen := map[*Union]bool{}
	var note func(f *Field)
	var walkUnion func(un *Union)
	note = func(f *Field) {
		if f == nil {
			return
		}
		kinds[TableWireScalarKind(f)] = true
		if k := TableWireElemKind(f); k != 0 {
			kinds[k] = true
		}
		if f.Type.Kind == TNamed {
			if un, ok := f.Type.Ref.(*Union); ok {
				walkUnion(un)
			}
		}
	}
	walkUnion = func(un *Union) {
		if seen[un] {
			return
		}
		seen[un] = true
		for _, v := range un.Variants {
			note(v.F)
		}
	}
	for name := range TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			note(f)
		}
	}
	return kinds
}
