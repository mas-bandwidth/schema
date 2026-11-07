// Package fixedclosure is the Elixir FIXED FORM backend's roots and closure
// (docs/SPEC-TABLES.md §3.4), in a package of its own so that the two Elixir
// emitters read ONE answer: the table backend (internal/codegen/elixirtable)
// emits a value module <Ns>.<Name> for every table in this closure, and the
// packet emitter (internal/codegen/elixir) steps its file module aside for
// exactly those names. It lives apart from elixirtable because that package's
// own tests import the packet emitter, so the packet emitter cannot import
// elixirtable without an import cycle.
package fixedclosure

import "github.com/mas-bandwidth/schema/v2/ir"

// Roots is every table of the unit the fixed form is emitted for, in
// declaration order: ir.TableFixedFormRoots (which applies the record ceiling
// and the depth bound) narrowed to the tables ir.TableFixedSupported carries.
func Roots(u *ir.Unit) []*ir.Struct {
	var out []*ir.Struct
	for _, st := range ir.TableFixedFormRoots(u) {
		if !ir.TableFixedSupported(st, 0) {
			continue
		}
		out = append(out, st)
	}
	return out
}

// Closure is every TYPE the roots reach, with a nested type ordered before the
// type that holds it.
func Closure(roots []*ir.Struct) []*ir.Struct {
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		collectTypes(st, seen, &order)
	}
	return order
}

func collectTypes(st *ir.Struct, seen map[string]bool, order *[]*ir.Struct) {
	if seen[st.Name] {
		return
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			collectTypes(r, seen, order)
		case *ir.Union:
			collectUnion(r, map[string]bool{}, seen, order)
		}
	}
	*order = append(*order, st)
}

// collectUnion descends through a union's arms for the structs the closure
// reaches. An arm that names a `type` descends into it; an arm that is ITSELF A
// UNION descends into THAT union's arms in turn, so a nested union's own arm
// structs are part of the closure and its helper is emitted where its caller's
// dispatch names it (docs/FIXED-FORM-ALGORITHM.md §4.1 "an arm inside an arm
// answers to the OUTER tag"). seenUnion is fresh per union field, so a union
// cycle cannot loop this walk.
func collectUnion(u *ir.Union, seenUnion, seen map[string]bool, order *[]*ir.Struct) {
	if seenUnion[u.Name] {
		return
	}
	seenUnion[u.Name] = true
	for _, v := range u.Variants {
		if v.F == nil {
			continue
		}
		switch r := v.F.Type.Ref.(type) {
		case *ir.Struct:
			if v.F.Type.Kind == ir.TNamed {
				collectTypes(r, seen, order)
			}
		case *ir.Union:
			collectUnion(r, seenUnion, seen, order)
		}
	}
}
