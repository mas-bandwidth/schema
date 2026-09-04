// The cross-target gate on MAPS (docs/SPEC-TABLES.md §2.8, §11): the language
// takes `map[K]V` and holds every rule the page states, and EVERY target
// refuses a table closure that declares one BY NAME until its codec lands
// (schema#380). In its own file so the construct's gate adds a file and edits
// no shared one.
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// mapSrc holds the three shapes §2.8's own example does — a lookup by name, a
// lookup by number whose value is a shared node, and a map of maps — so the
// derived facts below are read off the construct at every depth it admits.
const mapSrc = `package probe

table ShipConfig
{
    name   string(64)
    health int32 = 0
}

table Item
{
    count int32 = 0
}

table Fleet
{
    ships    map[string(32)]ShipConfig
    by_id    map[uint32]*ShipConfig
    loadouts map[string(16)]map[uint8]Item
}
`

// TestMapsAreLegal: the spelling resolves to an ARRAY OF TABLES on the wire —
// kind 14 over element kind 13, no new kind and no new framing (§2.8, §3) —
// the generated entry is a real table of the closure with the key first and the
// value second under two constant ids, the record slot is sixteen bytes, and
// the holder is VARIABLE-LENGTH by the fact of declaring one (§2.2).
func TestMapsAreLegal(t *testing.T) {
	u := unitFromSource(t, mapSrc)
	fleet := u.Tables["Fleet"]
	if fleet == nil {
		t.Fatalf("table Fleet missing")
	}
	if !ir.VariableTables(u)["Fleet"] {
		t.Fatalf("Fleet derived FIXED — a map is a variable edge whatever its key and value are (§2.2, §2.8)")
	}
	byName := map[string]*ir.Field{}
	for _, f := range fleet.Fields {
		byName[f.Name] = f
	}
	for name, entryName := range map[string]string{
		"ships":    "FleetShipsEntry",
		"by_id":    "FleetByIdEntry",
		"loadouts": "FleetLoadoutsEntry",
	} {
		f := byName[name]
		if f == nil {
			t.Fatalf("field %s missing", name)
		}
		if !f.IsMap() {
			t.Fatalf("%s: not a map in the IR", name)
		}
		if f.Type.Kind != ir.TMap {
			t.Errorf("%s: field kind %v, want ir.TMap", name, f.Type.Kind)
		}
		if got := f.MapEntry.Name; got != entryName {
			t.Errorf("%s: entry named %q, want %q — the claimed <Table><Field>Entry name (§2.8)", name, got, entryName)
		}
		if u.Tables[entryName] == nil {
			t.Errorf("%s: the generated entry %s is not a table of the closure (§2.8)", name, entryName)
		}
		if got := ir.TableFieldKind(f); got != ir.TableKindArray {
			t.Errorf("%s: field kind %d, want %d — a map rides as an array and spends no kind (§2.8, §3)", name, got, ir.TableKindArray)
		}
		if got := ir.TableElemKind(f); got != ir.TableKindTable {
			t.Errorf("%s: element kind %d, want %d — the entry body", name, got, ir.TableKindTable)
		}
		pieces := ir.FieldPieces(u, f, 0)
		if len(pieces) != 1 || pieces[0].Size != 16 || pieces[0].Offset%8 != 0 {
			t.Errorf("%s: storage pieces %+v, want one sixteen-byte piece at eight (§2.8, §7.2)", name, pieces)
		}
		key, value := ir.MapKeyField(f), ir.MapValueField(f)
		if key.Name != "key" || value.Name != "value" {
			t.Errorf("%s: entry fields are %q and %q, want key and value", name, key.Name, value.Name)
		}
		if ir.TableFieldId(key) != ir.MapKeyId || ir.TableFieldId(value) != ir.MapValueId {
			t.Errorf("%s: the entry's ids are not the two constants (§2.8, §5)", name)
		}
	}

	// the two constants are the ORDINARY hash of two ordinary names, which is
	// what makes a user's own `table Pair { key K  value V }` under [..N]Pair
	// the map's own bytes (§2.8)
	if ir.MapKeyId != 0xA079 || ir.MapValueId != 0x9194 {
		t.Errorf("the entry's constant ids are 0x%04X / 0x%04X, want 0xA079 / 0x9194 (§2.8, §5)", ir.MapKeyId, ir.MapValueId)
	}

	// a map of maps nests the claim rather than escaping it
	inner := ir.MapValueField(byName["loadouts"])
	if !inner.IsMap() || inner.MapEntry.Name != "FleetLoadoutsEntryValueEntry" {
		t.Errorf("the nested map's entry is %v, want FleetLoadoutsEntryValueEntry (§2.8)", inner.MapEntry)
	}

	// a *T value is a POINTER inside the entry, so the entry is variable and
	// the shared node is the pointer's ordinary sharing (§2.8, §3.1)
	if !ir.VariableTables(u)["FleetByIdEntry"] {
		t.Errorf("FleetByIdEntry derived FIXED — an entry holding *T is variable-length")
	}
	if !ir.PointerTargets(u)["ShipConfig"] {
		t.Errorf("ShipConfig is not a pointer target — a map[K]*T names one (§2.8)")
	}

	want := []string{"Fleet.by_id", "Fleet.loadouts", "Fleet.ships", "FleetLoadoutsEntry.value"}
	got := ir.MapFields(u)
	if len(got) != len(want) {
		t.Fatalf("MapFields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MapFields[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if !ir.HasMap(u) {
		t.Errorf("HasMap = false for a unit that declares three")
	}
}

// TestMapsAreRefusedByEveryTarget: no backend carries the codec yet
// (schema#380), so every registered target refuses the UNIT, naming the fields
// and saying what is missing. A codec that never met the entry, its sort or its
// ascending check must not be emitted — including the C++ reference's, whose
// own half is the next PR.
func TestMapsAreRefusedByEveryTarget(t *testing.T) {
	u := unitFromSource(t, mapSrc)
	c := New()
	for _, target := range c.Targets() {
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				t.Fatalf("--lang %s accepted a unit with a map in a table closure — it must refuse by name", target)
			}
			for _, want := range []string{"map", "Fleet.by_id", "Fleet.loadouts", "Fleet.ships", "schema#380"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("--lang %s: the refusal does not name %q: %v", target, want, err)
				}
			}
		})
	}
}

// TestMapFreeUnitIsUntouched: a unit that declares no map generates exactly
// what it generated before the construct existed — the zero-cost rule, at the
// compiler's own surface (§2.2, §2.8).
func TestMapFreeUnitIsUntouched(t *testing.T) {
	u := unitFromSource(t, optionalArraySrc)
	if ir.HasMap(u) {
		t.Fatalf("HasMap = true for a map-free unit")
	}
	c := New()
	for _, target := range c.Targets() {
		if _, err := c.Generate(u, target, Options{}); err != nil && strings.Contains(err.Error(), "map") {
			t.Errorf("--lang %s refused a map-free unit over maps: %v", target, err)
		}
	}
}

// TestMapRefusalNamesTheCarrierOnceThereIsOne: the refusal has two shapes, and
// the second one is what every other construct's says. This exercises the
// carrier registry directly rather than waiting for the C++ codec to land, so
// the day target_cpp.go registers, the message it produces is already held.
func TestMapRefusalNamesTheCarrierOnceThereIsOne(t *testing.T) {
	if len(mapTargets) != 0 {
		t.Fatalf("mapTargets = %v, want empty until a backend carries the codec (schema#380)", mapTargets)
	}
	defer func() { mapTargets = nil }()
	registerMapCarrier("cpp")

	u := unitFromSource(t, mapSrc)
	err := refuseMaps(u, "go")
	if err == nil {
		t.Fatalf("refuseMaps accepted a map-bearing unit for a non-carrier")
	}
	for _, want := range []string{"a map is cpp only today", "Fleet.ships", "--lang cpp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the carrier-form refusal does not name %q: %v", want, err)
		}
	}
	// a CARRIER never calls refuseMaps at all — it registers instead, exactly as
	// target_cpp.go does for the optional array — so there is no third shape.
}
