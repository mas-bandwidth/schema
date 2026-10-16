package ir

// THE JAVASCRIPT FIXED FORM'S CLOSURE (docs/SPEC-TABLES.md §2.6, §3.4): which
// tables carry the form in JavaScript, which types and unions their closure
// reaches, and so which generated module DEFINES a table arm's class.
//
// It lives here and not in internal/codegen/jstable because two emitters ask
// it. jstable emits the fixed home's classes for the closure, and
// internal/codegen/js emits a union's storage class, which constructs one
// instance per arm: a TABLE arm's class is jstable's, so the union module has
// to import it from the module that defines it — the fixed home when the union
// is in the fixed closure, the variable home otherwise (a union no fixed root
// reaches is the variable form's, and its arm may exist only there, as Stream's
// Chunk does). internal/codegen/js cannot import jstable (jstable's tests
// import js), so the predicate is ir's and both emitters call it.

import (
	"fmt"
	"strings"
)

// JSTableFixedRefusal answers why a table cannot carry the fixed form in JavaScript,
// or "" when it can. NAMED, NEVER SILENT is the property: a table this backend
// will not emit the form for gets a line in the generated module saying which
// table, which field and why, so a consumer reaching for Save gets a missing
// name from its own tooling beside a file that says why.
func JSTableFixedRefusal(st *Struct) string {
	// THE WALK'S DEPTH BOUND GOES FIRST, AND IT IS ITS OWN SENTENCE. This leg
	// held a private closure test with `16` written into it, so a table nested 17
	// deep lost the form here while C++ emitted it — and the line this function
	// wrote blamed the CLOSURE, naming a pointer or a map in a schema that has
	// neither. The bound is read through ir now, and the reason it gives is the
	// bound's own: past TableFixedMaxDepth no conforming reader takes the walk
	// (docs/FIXED-FORM-ALGORITHM.md §5.2's `layout_malformed`).
	if !TableFixedWithinDepth(st) {
		return fmt.Sprintf("its layout nests %d deep, past the form's %d-entry depth bound, so no conforming reader takes the walk (`layout_malformed`) — it keeps form 1 (docs/FIXED-FORM-ALGORITHM.md §5.2, docs/SPEC-TABLES.md §3.4)",
			TableFixedTypeDepth(st), TableFixedMaxDepth)
	}
	if !TableFixedSupported(st, 0) {
		return "its closure carries a construct the fixed class refuses — a pointer, a map, an unbounded array or a guarded branch (docs/SPEC-TABLES.md §3.4)"
	}
	// AN OPTIONAL IS NOT A REFUSAL. §3.4's kind table CARRIES `?T` — kind 35,
	// a present byte in front of a payload that rides WHOLE — so the leg that
	// refused it was refusing a wire the form already spells.
	//
	// WHAT THE OLD REFUSAL SAID was that the JavaScript packet classes had
	// nowhere to land the present byte, and that inventing a member in one
	// would be this backend inventing storage for a class another emitter
	// owns. THE PREMISE WAS WRONG, in the port's own favour: `?T` is a TABLE
	// construct — a `type`'s wire is positional and every field of one always
	// rides, so the compiler refuses the `?` there outright (§2.3) — and
	// internal/codegen/js emits a class for every `type` and NONE for a
	// `table`. So the only class that can ever hold a present flag is the one
	// THIS FILE emits (emitTableClass), and there was never another emitter's
	// storage to invent.
	//
	// §3.4's 65536-BYTE CEILING IS A REFUSAL OF THE FORM, and it belongs here
	// rather than in the roots loop, because the refusal IS the filter
	// (JSTableFixedRoots): a ceiling the filter knew and this function did not would be
	// a module that carries a 280020-byte record and says nothing about it. The
	// compiler already warns, naming the table and the size — *"THE FIXED FORM
	// IS NOT EMITTED FOR IT, because no conforming reader decodes a record that
	// size"* (TableFixedRecordBounds) — and the class is untouched: the table
	// keeps form 1, its by-value storage, its cook and its block form. So this
	// is not a refusal of the TABLE and never of the unit.
	if TableFixedTypeBytes(st) > TableFixedRecordMaxBytes {
		return "its record body is past §3.4's 65536-byte ceiling, so no conforming reader would decode a record that size — it keeps form 1 (docs/SPEC-TABLES.md §3.4)"
	}
	return ""
}

// JSTableFixedRoots is every table of a file the JavaScript fixed form is
// emitted for. Unlike the C++ reference there is no leaf cap here: JavaScript
// builds no plan at compile time, so there is no constexpr array for a type to
// overflow — the identity plan is a constant the emitter writes and any other
// plan is compiled at load time, which is the follow-on §3.4 names for the
// reference's own bound.
func JSTableFixedRoots(tables []*Struct) []*Struct {
	var out []*Struct
	for _, st := range tables {
		// THE FILTER IS THE REFUSAL ITSELF, and it has to be: a form emitted
		// for a table the refusal names is a module that says in a comment it
		// carries nothing and then carries it anyway. That is how the optional
		// hole shipped — the refusal named `?T` while this loop asked only
		// whether the closure was supported, so the writer emitted a body with
		// no present byte in it and the comment above said the form was not
		// there.
		if !st.IsTable || st.IsMapEntry() || JSTableFixedRefusal(st) != "" {
			continue
		}
		out = append(out, st)
	}
	return out
}

// JSTableFixedCollectTypes orders a root's closure so that a nested type's
// helpers are emitted before the type that uses them: every struct a field
// reaches by value or by element, and every struct a union field's arm holds.
func JSTableFixedCollectTypes(st *Struct, seen map[string]bool, order *[]*Struct) {
	if seen[st.Name] {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if f.Type.Kind != TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *Struct:
			JSTableFixedCollectTypes(r, seen, order)
		case *Union:
			for _, v := range r.Variants {
				if v.F == nil {
					continue // a payload-free arm reaches no nested type
				}
				if s, ok := v.F.Type.Ref.(*Struct); ok && v.F.Type.Kind == TNamed {
					JSTableFixedCollectTypes(s, seen, order)
				}
			}
		}
	}
	*order = append(*order, st)
}

// JSTableFixedClosure is the closure of every JavaScript fixed root of the
// unit, dependency-first — the structs whose classes the fixed home defines —
// and the set of unions a closure struct holds as a field. A union in that set
// is a fixed-closure union: its table arms' classes are the fixed home's.
func JSTableFixedClosure(u *Unit) (structs []*Struct, unions map[string]bool) {
	seen := map[string]bool{}
	for _, f := range u.Files {
		for _, st := range JSTableFixedRoots(f.Tables) {
			JSTableFixedCollectTypes(st, seen, &structs)
		}
	}
	unions = map[string]bool{}
	for _, st := range structs {
		for _, f := range st.Fields {
			if r, ok := f.Type.Ref.(*Union); ok && f.Type.Kind == TNamed {
				unions[r.Name] = true
			}
		}
	}
	return structs, unions
}

// JSTableRuntimeHome is the base name of the unit's ONE table runtime home in
// JavaScript: the PACKAGE, capitalized, so a file that sorts earlier relocates
// nothing — spelled as the unit's own file when one already has that name in
// another case. The fixed home is <Home>Table.js and the variable home
// <Home>Variable.js. It is internal/codegen/jstable's runtimeHome, answered
// here for internal/codegen/js; the jstable copy keeps its own line because
// make/js.mk's runtime-home negative control patches that line by text, and
// TestTableUnionsConstructAndRoundTripJavaScript holds the two to one answer.
func JSTableRuntimeHome(u *Unit) string {
	home := u.Package
	if home != "" {
		home = strings.ToUpper(home[:1]) + home[1:]
	}
	for _, f := range u.Files {
		if strings.EqualFold(f.Base, home) {
			return f.Base
		}
	}
	return home
}

// JSTableArmModule answers the module base name (no ".js") that DEFINES the
// class a union's TABLE arm constructs: the fixed home when the union is in the
// fixed closure (JSTableFixedClosure), the variable home otherwise. A union's
// storage class imports each table arm from here and from nowhere else.
func JSTableArmModule(u *Unit, un *Union) string {
	_, fixed := JSTableFixedClosure(u)
	if fixed[un.Name] {
		return JSTableRuntimeHome(u) + "Table"
	}
	return JSTableRuntimeHome(u) + "Variable"
}
