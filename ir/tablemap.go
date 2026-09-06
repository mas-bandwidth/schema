// Maps in the IR (docs/SPEC-TABLES.md §2.8): the derived facts every backend
// and the tool's engines read off a `map[K]V` field. A map is a LOOKUP the
// runtime provides over ENTRIES the wire carries — an array of one generated
// `{ key, value }` table in ascending key order — so nothing here describes a
// new wire construct; it names the entry, its two constant ids, and the places
// a walk has to descend.
package ir

import (
	"fmt"
	"sort"
	"strings"
)

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

// IsMapEntry reports a table the compiler GENERATED for a map field.
//
// It is the ONE EXCEPTION to §7's "a root is any table" (docs/SPEC-TABLES.md
// §2.8): an entry is reached only through the map that generates it, so it
// gets no `Open`, no `Cook`, no `Save` and no `Load` of its own. Its walk, its
// layout and its cook body are the whole of what it carries, and every
// backend's root surface asks this before emitting one.
func (s *Struct) IsMapEntry() bool { return s != nil && s.MapEntryOf != "" }

// MapKeyField, MapValueField are the entry's two fields. Both panic on a field
// that is not a map, because every caller has already asked [Field.IsMap].
func MapKeyField(f *Field) *Field   { return f.MapEntry.Fields[0] }
func MapValueField(f *Field) *Field { return f.MapEntry.Fields[1] }

// MapFieldVerbs is the SURFACE a map claims on the table that declares it
// (docs/SPEC-TABLES.md §2.8, §11): `<Table><Field>` followed by each of these.
// The entry's name is the first of them, and the rest are the lookup a map is
// — so a unit that declares anything under one of these spellings is refused
// at the source rather than at the user's build.
//
// The list is claimed with the CONSTRUCT and not with the codec, on the rule
// §11 already follows for the block form's row accessors: a name free today
// must not become a collision the day a backend emits it.
var MapFieldVerbs = []string{
	"Entry", "Insert", "Find", "Erase", "Each",
	// the SIDE INDEX (§2.8): its measure, the build, and the lookup through it
	"IndexMeasure", "Index", "IndexFind",
}

// MapFields lists the map fields an author WROTE, as `Table.field`, sorted —
// the names a backend that does not carry the construct puts in its refusal
// (docs/SPEC-TABLES.md §11). A generated entry's own `value` is not one of
// them: the entry is in the closure and holds the nested map, but no source
// file names it, and a refusal that named it would send its reader looking
// for a declaration that is not there.
func MapFields(u *Unit) []string {
	var out []string
	for name := range TableClosure(u) {
		st := memberStruct(u, name)
		if st == nil || st.MapEntryOf != "" {
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

// MapEntryProjectionName is a generated entry's identity in the two
// PROJECTIONS — the cook projection the build version digests
// (docs/SPEC-TABLES.md §20.2) and the tables baseline (§18.1). It is
// `<holder id>.<field id>`: the HOLDER'S WIRE ID and the MAP FIELD'S WIRE ID
// joined by a dot, and never the generated name, for the reason §20.2 gives —
// the generated name is derived from the field's SOURCE spelling, so a `was`
// rename of the holder or of the map field would otherwise move a line and
// invalidate a cooked file that no byte had moved under.
//
// A nested map's entry is held by an entry, so the key CHAINS: the outermost
// holder is a declared table and contributes its type id, and each map field
// on the way in contributes its own.
func MapEntryProjectionName(u *Unit, st *Struct) string {
	if st == nil || st.MapEntryOf == "" {
		return ""
	}
	owner, field := splitMapEntryOf(st.MapEntryOf)
	id := TableWireId(field)
	if holder := memberStruct(u, owner); holder != nil {
		for _, f := range holder.Fields {
			if f.Name == field {
				id = TableFieldWireId(f)
				break
			}
		}
		if holder.MapEntryOf != "" {
			return MapEntryProjectionName(u, holder) + fmt.Sprintf(".%016x", id)
		}
	}
	return fmt.Sprintf("%016x.%016x", TableWireId(owner), id)
}

// ProjectionMemberName is the name one closure member takes in the two
// projections: its own, and a generated entry's anonymous key
// (see [MapEntryProjectionName]). Both projections sort their members by THIS
// name and print it, so a `was` rename — which moves the generated name and
// no byte — moves neither the order nor a line.
func ProjectionMemberName(u *Unit, name string) string {
	if st := memberStruct(u, name); st != nil {
		if st.MapEntryOf != "" {
			return MapEntryProjectionName(u, st)
		}
		// a table renamed under `was` keeps its line and its place: the name
		// here is the WIRE name, so the rename moves nothing (§20.4)
		return st.WireName()
	}
	return name
}

// splitMapEntryOf splits `Fleet.ships` into its holder and its field. A field
// name carries no dot, so the LAST one separates them.
func splitMapEntryOf(of string) (owner, field string) {
	i := strings.LastIndex(of, ".")
	if i < 0 {
		return of, ""
	}
	return of[:i], of[i+1:]
}
