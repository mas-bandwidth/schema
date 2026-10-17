// Text lists and the array root in the IR (docs/SPEC-TABLES.md §2.13, §16.10).
//
// A TEXT LIST is `[..N]string(M)`, `[..N]?string(M)`, `[]string(M)` or
// `[]?string(M)` in a variable table. Like a map (§2.8) it hangs off a
// GENERATED element record, `<Table><Field>Text`, one field named `value`
// holding the `string(M)` (or `?string(M)`), so its storage is an array of that
// record and every walk that already carries an array of records carries it.
// What it adds is the wire's element kind for the plain spelling — `12`, each
// element its own `L` and bytes (§3) — and the text form's plain array of
// strings (§16.2).
//
// The ARRAY ROOT is a table line's `json = "array"` (§16.10): a text-form fact
// that moves no wire byte, the projection, the build version, the lock or the
// tables baseline.
package ir

import (
	"sort"
)

// TextListValueFieldName is the element record's one field. It is the map
// entry's value name (§2.8), so a `?string(M)` element's body is the bytes a
// `{ value ?string(M) }` record writes.
const TextListValueFieldName = "value"

// TextListElemName is the name a text list's generated element record CLAIMS:
// `<Table><Field>Text` — `HandoverRulesText` for `Handover.rules` (§2.13).
func TextListElemName(owner, field string) string {
	return owner + GoExportName(field) + "Text"
}

// TextListVerbs is the surface a text list claims on its table, `<Table><Field>`
// followed by each of these (§2.13, §11). An unbounded list claims §2.9's
// four as well.
var TextListVerbs = []string{"Text", "TextAt", "Append", "AppendAbsent"}

// IsTextListElem reports a record the compiler GENERATED for a text list. It is
// a generated entry as a map's is (MapEntryOf names its holder), reached only
// through the list that generates it.
func (s *Struct) IsTextListElem() bool { return s != nil && s.TextElem }

// TextListElem is a text list field's element record, and nil for every other
// field.
func (f *Field) TextListElem() *Struct {
	if f == nil || f.Array == ArrayNone || f.Type.Pointer || f.Type.Kind != TNamed {
		return nil
	}
	if st, ok := f.Type.Ref.(*Struct); ok && st.TextElem {
		return st
	}
	return nil
}

// IsTextListField is [Field.IsTextList] as a function.
func IsTextListField(f *Field) bool { return f.IsTextList() }

// IsTextList reports a text list field (§2.13).
func (f *Field) IsTextList() bool { return f.TextListElem() != nil }

// TextListOptional reports a `[..N]?string(M)` / `[]?string(M)` list: its
// elements ride as bodies of the element record under element kind 13.
func TextListOptional(f *Field) bool {
	st := f.TextListElem()
	return st != nil && st.Fields[0].Type.Optional
}

// TextListPlain reports a `[..N]string(M)` / `[]string(M)` list: its elements
// ride as kind 12 payloads under element kind 12 (§3).
func TextListPlain(f *Field) bool {
	st := f.TextListElem()
	return st != nil && !st.Fields[0].Type.Optional
}

// TextListValue is the element record's one field: the `string(M)`.
func TextListValue(f *Field) *Field { return f.TextListElem().Fields[0] }

// TableTextLists lists the text list fields an author WROTE, as `Table.field`,
// sorted — the names a target that does not carry the construct puts in its
// refusal.
func TableTextLists(u *Unit) []string {
	var out []string
	for name, st := range u.Tables {
		if st == nil || st.MapEntryOf != "" {
			continue
		}
		for _, f := range st.Fields {
			if f.IsTextList() {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// TableArrayRoots lists the tables declared `json = "array"` (§16.10), sorted.
func TableArrayRoots(u *Unit) []string {
	var out []string
	for name, st := range u.Tables {
		if st != nil && st.JsonArray {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// UnitHasTextLists reports whether any table of the unit holds a text list.
func UnitHasTextLists(u *Unit) bool { return len(TableTextLists(u)) > 0 }
