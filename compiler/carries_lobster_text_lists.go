//go:build schema_leg_lobster

// Text lists, their total and unbounded text, carried by the Lobster table
// surface (docs/SPEC-TABLES.md §2.13, §2.13.1; docs/PORT-LOBSTER.md, THE CARD
// MAP). Every other target keeps refusing them by name in
// compiler/tablesstringarrays.go.
package compiler

import "github.com/mas-bandwidth/schema/v2/ir"

var (
	_ = carryLobster(carryTextList)
	_ = carryLobster(carryTextListTotal)
	_ = carryLobster(carryUnboundedText)
)

// lobsterListRefusalApplies reports that a unit's unbounded lists are not all
// text lists; the target then keeps refusing them by name, as every other
// non-carrier does (docs/SPEC-TABLES.md §2.13, §2.13.1).
func lobsterListRefusalApplies(u *ir.Unit) bool {
	return !carries("lobster", carryUnboundedText) || !lobsterOnlyTextLists(u)
}

// lobsterMapRefusalApplies reports that a unit declares a map whose key is not
// the unbounded text this card carries, so the map refusal stays
// (docs/SPEC-TABLES.md §2.13.1).
func lobsterMapRefusalApplies(u *ir.Unit) bool {
	return !carries("lobster", carryUnboundedText) || !lobsterOnlyUnboundedMapKeys(u)
}

// lobsterOnlyTextLists reports that every unbounded list the unit declares is
// a text list (docs/SPEC-TABLES.md §2.13, §2.13.1).
func lobsterOnlyTextLists(u *ir.Unit) bool {
	for _, st := range append(unitTables(u), unitStructs(u)...) {
		for _, f := range st.Fields {
			if f.IsList() && !f.IsTextList() {
				return false
			}
		}
	}
	return true
}

// lobsterOnlyUnboundedMapKeys reports that every map the unit declares has an
// unbounded text key (docs/SPEC-TABLES.md §2.13.1).
func lobsterOnlyUnboundedMapKeys(u *ir.Unit) bool {
	for _, st := range append(unitTables(u), unitStructs(u)...) {
		for _, f := range st.Fields {
			if f.IsMap() && !mapHasUnboundedKey(f) {
				return false
			}
		}
	}
	return true
}

// mapHasUnboundedKey reports an unbounded text key anywhere in a map's value
// chain: `map[uint8]map[string]V` carries one at the inner map.
func mapHasUnboundedKey(f *ir.Field) bool {
	if ir.MapKeyUnbounded(f) {
		return true
	}
	if f.IsMap() && f.MapEntry != nil && len(f.MapEntry.Fields) > 1 {
		value := f.MapEntry.Fields[1]
		return value.IsMap() && mapHasUnboundedKey(value)
	}
	return false
}

func unitTables(u *ir.Unit) []*ir.Struct {
	out := make([]*ir.Struct, 0, len(u.Tables))
	for _, st := range u.Tables {
		out = append(out, st)
	}
	return out
}

func unitStructs(u *ir.Unit) []*ir.Struct {
	out := make([]*ir.Struct, 0, len(u.Structs))
	for _, st := range u.Structs {
		out = append(out, st)
	}
	return out
}
