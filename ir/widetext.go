// Wide text's unit-level census (SPEC §4.12): the fields a target must carry
// before it can be handed a unit that declares one.
package ir

import "sort"

// WideTextFields returns the qualified name of every `wstring(N)` field the
// unit declares — `Type.field` for a field of a declared type, and
// `Union.arm` for a union arm whose payload is wide text. The compiler's
// per-target refusal names these, so a backend that has not met the construct
// refuses the unit loudly instead of emitting a member it never laid out.
//
// Only the PACKET wire is walked, because a `wstring(N)` inside a table
// closure is refused at the source by the checker until the table wire's
// kind 33 lands (docs/SPEC-TABLES.md §3, §11).
func WideTextFields(u *Unit) []string {
	var out []string
	for name, st := range u.Structs {
		for _, f := range st.Fields {
			if f.Type.Kind == TWString {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	for name, un := range u.Unions {
		for _, v := range un.Variants {
			if v.F != nil && v.F.Type.Kind == TWString {
				out = append(out, name+"."+v.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}
