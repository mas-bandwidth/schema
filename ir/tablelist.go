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

// ListElementLayout is the storage one element of a `[]T` takes and the
// alignment its array is laid at (docs/SPEC-TABLES.md §2.9): a TableRef for a
// `[]*T`, and the element type's own size and alignment otherwise. The element
// array in a holder's node extent is `count × size` at `align`, and this is
// the one place both the C++ writer and the tool's cook-check take the two
// numbers from.
func ListElementLayout(u *Unit, f *Field) (size, align int64) {
	single := *f
	single.Array = ArrayNone
	single.ArrayBound = 0
	single.Type.Optional = false
	p := elementPiece(u, &single)
	return p.size, p.align
}

// CookExtentAlign is the greatest alignment any array a record's node extent
// CAN hold asks for, at any depth (docs/SPEC-TABLES.md §2.8, §2.9, §7.2): a
// list's element, a map's generated entry, and whatever those, a nested
// record held by value or a union arm held by value carry in turn. It is
// computed from the TYPES and never from which arrays one value holds, so every
// node of a type is laid out one way. 1 for a record whose extent can hold
// nothing.
func CookExtentAlign(u *Unit, st *Struct) int64 {
	return cookExtentAlign(u, st, map[*Struct]bool{}, map[*Union]bool{})
}

// CookNodeAlign is one node's alignment in a cooked region: the greater of its
// record's alignment and CookExtentAlign, so an array placed at its element's
// alignment RELATIVE TO THE EXTENT is at that alignment in memory too.
func CookNodeAlign(u *Unit, st *Struct) int64 {
	return max(RecordLayout(u, st).Align, CookExtentAlign(u, st))
}

// CookExtentStart is where a node's extent begins, relative to the node: the
// record's sizeof rounded up to the greater of the region's alignment floor and
// CookExtentAlign (docs/SPEC-TABLES.md §2.8). Rounding to the floor alone puts
// a sixteen-aligned array at eight modulo sixteen whenever sizeof is.
func CookExtentStart(u *Unit, st *Struct) int64 {
	a := max(RegionAlignFloor, CookExtentAlign(u, st))
	return (RecordLayout(u, st).Size + a - 1) / a * a
}

func cookExtentAlign(u *Unit, st *Struct, seenS map[*Struct]bool, seenU map[*Union]bool) int64 {
	if st == nil || seenS[st] {
		return 1
	}
	seenS[st] = true
	align := int64(1)
	for _, f := range st.Fields {
		align = max(align, cookFieldExtentAlign(u, f, seenS, seenU))
	}
	return align
}

func cookFieldExtentAlign(u *Unit, f *Field, seenS map[*Struct]bool, seenU map[*Union]bool) int64 {
	align := int64(1)
	switch {
	case f.IsMap():
		return max(RecordLayout(u, f.MapEntry).Align, cookExtentAlign(u, f.MapEntry, seenS, seenU))
	case f.IsList():
		_, align = ListElementLayout(u, f)
	}
	if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != TNamed {
		return align
	}
	switch ref := f.Type.Ref.(type) {
	case *Struct:
		return max(align, cookExtentAlign(u, ref, seenS, seenU))
	case *Union:
		if seenU[ref] {
			return align
		}
		seenU[ref] = true
		for _, v := range ref.Variants {
			if v.F != nil {
				align = max(align, cookFieldExtentAlign(u, v.F, seenS, seenU))
			}
		}
	}
	return align
}
