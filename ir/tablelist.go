// Unbounded arrays in the IR (docs/SPEC-TABLES.md §2.9): the derived facts
// every backend and the tool's engines read off a `[]T` field. An unbounded
// array is a COUNTED ARRAY whose count the DATA decides — §2.8's map with the
// KEY and the SORT taken out — so nothing here describes a new wire construct.
// It names the surface the construct claims, the fields a backend without it
// refuses, and the one question every walk asks: does this field carry a LIVE
// COUNT the value decides.
package ir

import "sort"

// IsList reports a `[]T` field — an UNBOUNDED ARRAY
// (docs/SPEC-TABLES.md §2.9).
func (f *Field) IsList() bool { return f != nil && f.Array == ArrayList }

// ListFieldVerbs is the SURFACE an unbounded array claims on the table that
// declares it (docs/SPEC-TABLES.md §2.9, §11): `<Table><Field>` followed by
// each of these. There is no entry type and no lookup, so the list is the
// three calls a builder needs and nothing beside them.
//
// The list is claimed with the CONSTRUCT and not with the codec, on the rule
// §11 already follows for the map's surface and the block form's row
// accessors: a name free today must not become a collision the day a backend
// emits it.
var ListFieldVerbs = []string{"Add", "Each", "Erase"}

// ListFields lists the unbounded arrays an author WROTE, as `Table.field`,
// sorted — the names a backend that does not carry the construct puts in its
// refusal (docs/SPEC-TABLES.md §2.9, §11).
func ListFields(u *Unit) []string {
	var out []string
	for name := range TableClosure(u) {
		st := memberStruct(u, name)
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.IsList() {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// CountedOnWire reports a field whose array body carries a LIVE COUNT the
// value decides rather than one the declaration fixes: the bounded spellings
// `[..N]T` and `[Min..N]T`, and the unbounded `[]T` (docs/SPEC-TABLES.md §2.9,
// §3). The two write the same bytes, so every walk that asks "how many
// elements ride" asks this and not the spelling.
func (f *Field) CountedOnWire() bool {
	return f != nil && (f.Array == ArrayCounted || f.Array == ArrayList)
}
