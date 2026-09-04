package ir_test

// The DERIVED FACTS of a map (docs/SPEC-TABLES.md §2.8): the entry's claimed
// name, the block form it never has, and its two projections — the cook
// projection the build version digests (§20.1) and, in internal/baseline, the
// tables baseline (§18.1). Both identify a generated entry by the holder's
// wire id and the field's, never by the name the field's spelling produced.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// mapUnitSrc is §2.8's own example plus a plain fixed table, so the same unit
// answers every question below: a map of tables by name, a map of pointers by
// number, a map of maps, and a table with no map at all.
const mapUnitSrc = `package fleet

table ShipConfig
{
    hull   int32
    shield int32
}

table Item { count int32 }

table Fleet
{
    ships    map[string(32)]ShipConfig
    by_id    map[uint32]*ShipConfig
    loadouts map[string(16)]map[uint8]Item
}
`

// TestMapEntryNameIsTheClaimedSpelling pins §2.8's rule: `<Table><Field>Entry`
// with the field's name in PascalCase by §11's rule, and `<OuterEntry>ValueEntry`
// for a map whose value is a map. `by_id` is the row that matters — a
// snake_case field must land on `ById` and not on `By_id`, because §11's
// spelling rule is what every other generated name already follows.
func TestMapEntryNameIsTheClaimedSpelling(t *testing.T) {
	u := unitFrom(t, mapUnitSrc)
	fleet := u.Tables["Fleet"]
	if fleet == nil {
		t.Fatal("table Fleet missing")
	}
	byName := map[string]*ir.Field{}
	for _, f := range fleet.Fields {
		byName[f.Name] = f
	}
	for field, want := range map[string]string{
		"ships":    "FleetShipsEntry",
		"by_id":    "FleetByIdEntry",
		"loadouts": "FleetLoadoutsEntry",
	} {
		f := byName[field]
		if f == nil || !f.IsMap() {
			t.Fatalf("field %s is not a map", field)
		}
		if got := f.MapEntry.Name; got != want {
			t.Errorf("%s: entry named %q, want %q (docs/SPEC-TABLES.md §2.8, §11)", field, got, want)
		}
		if got := ir.MapEntryName("Fleet", field); got != want {
			t.Errorf("MapEntryName(Fleet, %s) = %q, want %q", field, got, want)
		}
		if u.Tables[want] == nil {
			t.Errorf("%s: the entry %s is not a table of the closure", field, want)
		}
	}
	inner := ir.MapValueField(byName["loadouts"])
	if !inner.IsMap() || inner.MapEntry.Name != "FleetLoadoutsEntryValueEntry" {
		t.Errorf("the nested map's entry is %v, want FleetLoadoutsEntryValueEntry", inner.MapEntry)
	}
}

// TestAMapEntryHasNoBlockForm: §2.8 states it flatly — "a map's entries are
// never rows". The rule is CATEGORICAL and not derived, and FleetShipsEntry is
// why: a `string(32)` key and a by-value `ShipConfig` make an entry that looks
// perfectly fixed, so a derivation that asked only "is this table variable?"
// would hand it a block form, its counts, its storage and its projection
// static_asserts.
func TestAMapEntryHasNoBlockForm(t *testing.T) {
	u := unitFrom(t, mapUnitSrc)
	if ir.VariableTables(u)["FleetShipsEntry"] {
		t.Fatal("FleetShipsEntry derived VARIABLE — this test is no longer about a fixed-looking entry")
	}
	b := ir.Blocks(u)
	if b == nil {
		t.Fatal("the unit declares tables; ir.Blocks returned nil")
	}
	if b.IsBlock("FleetShipsEntry") {
		t.Error("FleetShipsEntry has a block form — a map's entries are never rows (§2.8)")
	}
	why := b.SkippedReason("FleetShipsEntry")
	if !strings.Contains(why, "Fleet.ships") || !strings.Contains(why, "never rows") {
		t.Errorf("the reason is %q, want one naming the map it is the entry of", why)
	}
}

// TestNoBlockFormReasonNamesTheEdgeItFound: a table has no block form because
// of something, and the reason says WHICH something. A map and a pointer are
// two different edges, and a sentence that says "a pointer" over a table whose
// only variable edge is a map sends its reader looking for a pointer that is
// not there.
func TestNoBlockFormReasonNamesTheEdgeItFound(t *testing.T) {
	const src = `package fleet

table ShipConfig { hull int32 }

table Item { count int32 }

table Mapped
{
    ships map[uint32]ShipConfig
}

table Pointered
{
    next *Item
}

table Nested
{
    inner Mapped
}
`
	b := ir.Blocks(unitFrom(t, src))
	for _, tc := range []struct{ table, want string }{
		{"Mapped", "Mapped.ships is a map"},
		{"Pointered", "Pointered.next is a pointer"},
		{"Nested", "Mapped.ships is a map"},
	} {
		if b.IsBlock(tc.table) {
			t.Errorf("%s has a block form — its closure holds a variable edge", tc.table)
			continue
		}
		if why := b.SkippedReason(tc.table); !strings.Contains(why, tc.want) {
			t.Errorf("%s: the reason is %q, want one naming %q", tc.table, why, tc.want)
		}
	}
}

// TestCookProjectionKeepsAMapAnonymous: §20.1's record line is keyed by wire
// id and not by source name, "because a `was` rename moves no byte and must
// not invalidate a cooked file" — and a generated entry's NAME is derived from
// the map field's source spelling, so the entry takes the anonymous key of its
// holder's wire id and its field's, at every depth.
func TestCookProjectionKeepsAMapAnonymous(t *testing.T) {
	u := unitFrom(t, mapUnitSrc)
	facts := ir.CookProjection(u)
	for _, generated := range []string{"FleetShipsEntry", "FleetByIdEntry", "FleetLoadoutsEntry", "FleetLoadoutsEntryValueEntry"} {
		if strings.Contains(facts, generated) {
			t.Errorf("the cook projection names the generated entry %s:\n%s", generated, facts)
		}
	}
	holder := ir.TableTypeId("Fleet")
	for _, want := range []string{
		// the MAP FIELD's own line is an ARRAY line: kind 14, `elem=` the
		// generated entry's own storage size — the pitch its entries lie at,
		// as on every other array line — `array=map`, and NO bound, because a
		// map declares no extent (§20.2)
		"field 294a5c4913e1ad44 kind=14 offset=0 size=16 elem=48 array=map\n",
		"field 7b024c46e98d3404 kind=14 offset=16 size=16 elem=16 array=map\n",
		// and the ENTRY takes a record line of its own, keyed by the holder's
		// wire id and the field's, the nested one chaining its holder's key
		"record " + hex16(holder) + ".294a5c4913e1ad44 sizeof=48 alignof=4\n",
		"record " + hex16(holder) + ".294fa1b3f0f5f070.7ce4fd9430e80cea sizeof=8 alignof=4\n",
		// the KEY's kind and capacity ride on the entry's own `key` line,
		// which is where a key edit moves the id (§20.1)
		"field 3dc94a19365b10ec kind=12 offset=0 size=40 bound=32\n",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("the cook projection has no line %q:\n%s", want, facts)
		}
	}
}

// TestAWasRenameMovesNeitherTheProjectionNorTheVersion is the reason the
// entry is anonymous, stated as the property: renaming a map field under `was`
// keeps its wire id, so it moves no byte — and it must therefore move no line
// of the projection and not the build version, entry lines and record ORDER
// included.
func TestAWasRenameMovesNeitherTheProjectionNorTheVersion(t *testing.T) {
	const before = `package fleet

table ShipConfig { hull int32 }

table Fleet
{
    ships map[string(32)]ShipConfig
}
`
	const after = `package fleet

table ShipConfig { hull int32 }

table Fleet
{
    vessels map[string(32)]ShipConfig | was = "ships"
}
`
	u1, u2 := unitFrom(t, before), unitFrom(t, after)
	if u2.Tables["Fleet"].Fields[0].MapEntry.Name != "FleetVesselsEntry" {
		t.Fatal("the rename did not move the GENERATED name — this test no longer proves anything")
	}
	if a, b := ir.CookProjection(u1), ir.CookProjection(u2); a != b {
		t.Errorf("a was rename moved the cook projection:\n--- before ---\n%s\n--- after ---\n%s", a, b)
	}
	if a, b := ir.BuildVersion(u1), ir.BuildVersion(u2); a != b {
		t.Errorf("a was rename moved the build version: %016x -> %016x", a, b)
	}
}

// TestAMapFieldIsOneSixteenBytePiece: an int64 self-relative reference to the
// entry array and an int32 count — the width every companion count takes
// (§2.8, §1.10 of the map rulings) — padded to eight. ONE piece and not two,
// because both backends spell it as one member and a port that walked two
// would account for twelve bytes where sixteen are written.
func TestAMapFieldIsOneSixteenBytePiece(t *testing.T) {
	u := unitFrom(t, mapUnitSrc)
	for _, f := range u.Tables["Fleet"].Fields {
		if !f.IsMap() {
			continue
		}
		pieces := ir.FieldPieces(u, f, 0)
		if len(pieces) != 1 || pieces[0].Size != 16 || pieces[0].Offset%8 != 0 {
			t.Errorf("%s: storage pieces %+v, want one sixteen-byte piece at eight", f.Name, pieces)
		}
	}
}

func hex16(v uint64) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = digits[v&0xf]
		v >>= 4
	}
	return string(out)
}
