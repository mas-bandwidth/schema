// The cross-target gate on MAPS (docs/SPEC-TABLES.md §2.8, §11): the language
// takes `map[K]V` and holds every rule the page states, and EVERY target
// refuses a table closure that declares one BY NAME until its codec lands
// (schema#380). In its own file so the construct's gate adds a file and edits
// no shared one.
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
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

	// only the fields an author WROTE: the nested map lives on the generated
	// entry, which no source file names, so a refusal that listed it would
	// send its reader looking for a declaration that is not there (§11)
	want := []string{"Fleet.by_id", "Fleet.loadouts", "Fleet.ships"}
	got := ir.MapFields(u)
	if len(got) != len(want) {
		t.Fatalf("MapFields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MapFields[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestMapsAreRefusedByEveryPort: the C++ REFERENCE carries the codec and the
// eight ports do not (docs/SPEC-TABLES.md §2.8, §15) — a map is a
// variable-class construct and the variable class is the reference's alone.
// Every port refuses the UNIT, naming the fields, naming the carrier and
// naming the flag that generates. A codec that never met the entry, its sort
// or its ascending check must not be emitted anywhere.
func TestMapsAreRefusedByEveryPort(t *testing.T) {
	u := unitFromSource(t, mapSrc)
	c := New()
	for _, target := range c.Targets() {
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if target == "cpp" {
				if err != nil {
					t.Fatalf("--lang cpp refused a map: the reference carries the codec (schema#380): %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("--lang %s accepted a unit with a map in a table closure — it must refuse by name", target)
			}
			for _, want := range []string{"map", "Fleet.by_id", "Fleet.loadouts", "Fleet.ships", "cpp"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("--lang %s: the refusal does not name %q: %v", target, want, err)
				}
			}
		})
	}
}

// TestMapCarrierIsTheReferenceAlone: exactly one target carries the construct,
// and it is the C++ reference (docs/SPEC-TABLES.md §2.8, §15). A port that
// registers here without its codec would turn every refusal below into a
// silent acceptance.
func TestMapCarrierIsTheReferenceAlone(t *testing.T) {
	if len(mapTargets) != 1 || mapTargets[0] != "cpp" {
		t.Fatalf("mapTargets = %v, want exactly [cpp] — the variable class is the reference's (docs/SPEC-TABLES.md §2.8, §15)", mapTargets)
	}
}

// TestMapFreeUnitIsUntouched: a unit that declares no map generates exactly
// what it generated before the construct existed — the zero-cost rule, at the
// compiler's own surface (§2.2, §2.8).
func TestMapFreeUnitIsUntouched(t *testing.T) {
	u := unitFromSource(t, optionalArraySrc)
	if got := ir.MapFields(u); len(got) != 0 {
		t.Fatalf("MapFields = %v for a map-free unit", got)
	}
	c := New()
	for _, target := range c.Targets() {
		if _, err := c.Generate(u, target, Options{}); err != nil && strings.Contains(err.Error(), "map") {
			t.Errorf("--lang %s refused a map-free unit over maps: %v", target, err)
		}
	}
}

// TestMapRefusalNamesTheCarrier: what a port's refusal says now that the
// reference carries the construct — the carrier, the flag that generates, and
// the fields an author wrote.
func TestMapRefusalNamesTheCarrier(t *testing.T) {
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

// TestMapTypeSpelling: a diagnostic and a descriptor name the declaration the
// author wrote, star and bound included — map[K]T and map[K]*T are two
// declarations (§2.8).
func TestMapTypeSpelling(t *testing.T) {
	u := unitFromSource(t, mapSrc)
	byName := map[string]*ir.Field{}
	for _, f := range u.Tables["Fleet"].Fields {
		byName[f.Name] = f
	}
	for name, want := range map[string]string{
		"ships":    "map[string(32)]ShipConfig",
		"by_id":    "map[uint32]*ShipConfig",
		"loadouts": "map[string(16)]map[uint8]Item",
	} {
		if got := ir.TableTypeSpelling(byName[name]); got != want {
			t.Errorf("%s spells %q, want %q", name, got, want)
		}
	}
}

// TestMapEntryIsNotARoot: §2.8 states the entry is the ONE EXCEPTION to §7's
// "a root is any table" — it is reached only through the map that generates
// it, so it gets no Open, no Cook, no Save and no Load of its own, and its
// walk, its layout and its cook body are the whole of what it carries.
//
// The gate runs the C++ REFERENCE's table emitter directly, because every
// registered target refuses a map-bearing unit until its codec lands
// (schema#380) and this is a fact of the emitter rather than of the target's
// gate. It is also where the checker and the emitter are held to ONE entry
// name: the header declares the struct the checker claimed, `by_id` in
// PascalCase and all.
func TestMapEntryIsNotARoot(t *testing.T) {
	files, err := cpptable.Generate(unitFromSource(t, mapSrc))
	if err != nil {
		t.Fatalf("generate the table header: %v", err)
	}
	var header string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			header = string(data)
		}
	}
	if header == "" {
		t.Fatal("no Table header generated")
	}
	for _, entry := range []string{"FleetShipsEntry", "FleetByIdEntry", "FleetLoadoutsEntry", "FleetLoadoutsEntryValueEntry"} {
		if !strings.Contains(header, "struct "+entry) {
			t.Errorf("the header declares no %s — the checker claims that name, so the emitter must produce it (§2.8, §11)", entry)
		}
		for _, verb := range []string{"Open", "Cook", "CookMeasure", "Save", "Load"} {
			if strings.Contains(header, entry+verb+"(") {
				t.Errorf("the header emits %s%s — an entry is reached only through its map and is not a root (§2.8, §7)", entry, verb)
			}
		}
	}
	// and the ROOT that declares the maps keeps its whole surface, so the
	// test above is about the entry and not about a header that emits nothing
	for _, verb := range []string{"Open", "Cook", "Load"} {
		if !strings.Contains(header, "Fleet"+verb+"(") {
			t.Errorf("the header emits no Fleet%s — the holder is an ordinary root", verb)
		}
	}
}

// TestToolRefusesMapsByName: the tool's WIRE and TEXT halves carry maps now
// (docs/SPEC-TABLES.md §2.8), and its COOK half does not. So the cook and
// uncook surfaces refuse a map-bearing unit BY NAME rather than laying out an
// entry array they have no placement for. Without the refusal a caller gets a
// cook whose region is short of the entries, which is worse than a diagnostic.
// `cook-check` refuses at the SLOT instead, where its scan meets one, and
// internal/tablecook's TestCookCheckMapSlotRefusedByName holds that.
func TestToolRefusesMapsByName(t *testing.T) {
	u := unitFromSource(t, mapSrc)
	c := New()
	surfaces := map[string]func() error{
		"Cook":   func() error { _, _, _, err := c.Cook(u, "Fleet", nil, CookOptions{}); return err },
		"Uncook": func() error { _, err := c.Uncook(u, "Fleet", nil); return err },
	}
	for name, call := range surfaces {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatalf("%s accepted a map-bearing unit — it must refuse by name", name)
			}
			for _, want := range []string{"Fleet.ships", "schema#380", "cpp"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s's refusal does not name %q: %v", name, want, err)
				}
			}
			if strings.Contains(err.Error(), "framing") && !strings.Contains(err.Error(), "framing damage rather than") {
				t.Errorf("%s blamed the framing: %v", name, err)
			}
		})
	}
}

// TestToolTakesAMapFreeUnit: the refusal above is the CONSTRUCT's and not a
// tax on every unit — a map-free one reaches the engines exactly as it did.
func TestToolTakesAMapFreeUnit(t *testing.T) {
	u := unitFromSource(t, optionalArraySrc)
	if err := refuseToolMaps(u); err != nil {
		t.Fatalf("the tool refused a map-free unit: %v", err)
	}
}
