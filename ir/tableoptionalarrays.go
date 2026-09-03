// The OPTIONAL ARRAY census (docs/SPEC-TABLES.md §2.3): its own file, per the
// registry split — a construct's cross-target surface adds files rather than
// growing a shared one.
package ir

import "sort"

// TableOptionalArrays names every OPTIONAL ARRAY a table closure holds —
// `?[..N]T` and `?[N]T` (docs/SPEC-TABLES.md §2.3) — as `Member.field`,
// sorted: the fields a backend without the form refuses a unit over, by name.
func TableOptionalArrays(u *Unit) []string {
	closure := TableClosure(u)
	var out []string
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Optional && f.Array != ArrayNone {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}
