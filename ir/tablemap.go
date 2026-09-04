// Maps in the IR (docs/SPEC-TABLES.md §2.8): the derived facts every backend
// and the tool's engines read off a `map[K]V` field. A map is a LOOKUP the
// runtime provides over ENTRIES the wire carries — an array of one generated
// `{ key, value }` table in ascending key order — so nothing here describes a
// new wire construct; it names the entry, its two constant ids, and the places
// a walk has to descend.
package ir

import "sort"

// MapKeyFieldName, MapValueFieldName are the entry's two field names. They are
// ordinary names taking the ordinary hash, which is what makes a user's own
// `table Pair { key K  value V }` under `[..N]Pair` the map's own bytes.
const (
	MapKeyFieldName   = "key"
	MapValueFieldName = "value"
)

// MapKeyId, MapValueId are the two CONSTANT field ids an entry rides under on
// the table wire (docs/SPEC-TABLES.md §2.8, §5) — the fold of `key` and
// `value`, fixed for every map in every unit. They are constants of the RULE
// rather than beside it: both are [FieldId] of an ordinary name, so they move
// with the id rule and never on their own.
var (
	MapKeyId   = FieldId(MapKeyFieldName)
	MapValueId = FieldId(MapValueFieldName)
)

// MapEntryName is the name a map field's generated entry table CLAIMS:
// `<Table><Field>Entry` — `FleetShipsEntry` for `Fleet.ships`
// (docs/SPEC-TABLES.md §2.8). A unit that declares a table under that spelling
// beside the map is refused, naming the map. A map inside a map nests the
// rule rather than escaping it: the inner entry's holder is the outer entry,
// so `Fleet.loadouts` of `map[string(16)]map[uint8]Item` claims
// `FleetLoadoutsEntry` and `FleetLoadoutsEntryValueEntry`.
func MapEntryName(owner, field string) string {
	return owner + GoExportName(field) + "Entry"
}

// IsMap reports a `map[K]V` field.
func (f *Field) IsMap() bool { return f.MapEntry != nil }

// MapKeyField, MapValueField are the entry's two fields. Both panic on a field
// that is not a map, because every caller has already asked [Field.IsMap].
func MapKeyField(f *Field) *Field   { return f.MapEntry.Fields[0] }
func MapValueField(f *Field) *Field { return f.MapEntry.Fields[1] }

// MapEntryStructs lists a unit's GENERATED entry tables in a deterministic
// order — the order they were synthesized, innermost first, so a header that
// emits them in list order declares an inner entry before the outer one that
// holds it.
func MapEntryStructs(u *Unit) []*Struct {
	var out []*Struct
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.MapEntryOf != "" {
				out = append(out, st)
			}
		}
	}
	return out
}

// MapFields lists the MAP fields of a unit's table closure as `Table.field`,
// sorted — the names a backend that does not carry the construct puts in its
// refusal (docs/SPEC-TABLES.md §11).
func MapFields(u *Unit) []string {
	var out []string
	for name := range TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.IsMap() {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// HasMap reports whether a unit's table closure declares one — the question
// the zero-cost gate and every backend's refusal ask.
func HasMap(u *Unit) bool { return len(MapFields(u)) > 0 }

// MapKeyIsString reports the key's family: a bounded string, or an integer.
// Nothing else is a key (docs/SPEC-TABLES.md §2.8), so the two questions are
// one.
func MapKeyIsString(f *Field) bool { return MapKeyField(f).Type.Kind == TString }
