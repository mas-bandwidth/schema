// Wide text's unit-level census (SPEC §4.12): the fields a target must carry
// before it can be handed a unit that declares one.
package ir

import "sort"

// WideTextFields returns the qualified name of every `wstring(N)` field the
// unit declares — `Type.field` for a field of a declared type or a table, and
// `Union.arm` for a union arm whose payload is wide text. The compiler's
// per-target refusal names these, so a backend that has not met the construct
// refuses the unit loudly instead of emitting a member it never laid out.
//
// BOTH WIRES ARE WALKED, because wide text rides both: the packet wire's
// 32-bit groups (SPEC.md §4.12) and kind 33 on the table wire
// (docs/SPEC-TABLES.md §3). A target carries the construct or it does not, and
// a table declaration is one more place the construct is declared.
func WideTextFields(u *Unit) []string {
	var out []string
	collect := func(name string, st *Struct) {
		for _, f := range st.Fields {
			if f.Type.Kind == TWString {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	for name, st := range u.Structs {
		collect(name, st)
	}
	for name, st := range u.Tables {
		collect(name, st)
	}
	arms := func(name string, un *Union) {
		for _, v := range un.Variants {
			if v.F != nil && v.F.Type.Kind == TWString {
				out = append(out, name+"."+v.Name)
			}
		}
	}
	for name, un := range u.Unions {
		arms(name, un)
	}
	for name, un := range u.TableUnions {
		arms(name, un)
	}
	sort.Strings(out)
	return out
}
