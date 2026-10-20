//go:build schema_leg_lobster

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg lobster`; docs/PORT-LOBSTER.md is the port's
// design. The schema_leg_lobster tag keeps this target out of the default
// nine until the packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_lobster` is `schema generate --lang lobster`.
// Drop the tag when the port joins the builtins.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type lobsterTarget struct{}

func (lobsterTarget) Names() []string { return []string{"lobster"} }

// Generate is the type surface (one module per schema file) and, for a unit
// that declares tables, the table surface (one <Base>Table.lobster per file
// with tables) — the descriptors and constructors, no codec yet
// (docs/PORT-LOBSTER.md, THE CARD MAP). The constructs the lobster leg does
// not carry are refused centrally, by name (compiler/carries_lobster.go); a
// construct the table surface does not describe is refused by lobstertable.
func (lobsterTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// THE LEG HAS NO WIRE CODEC YET (docs/PORT-LOBSTER.md, THE CARD MAP), so
	// it carries none of the constructs the shared refusals hold back from a
	// port that has not met them: a string, bytes or flags default, a was row,
	// a union with table arms or a payload-free table arm, an array of
	// unions, a byte blob, an optional array, a map and an unbounded list —
	// each refused here by name, as every non-carrier refuses it. The
	// descriptor surface itself describes them all
	// (TestLobsterTableDescriptorsLoad generates every manifest unit through
	// lobster and lobstertable directly).
	refusals := []func(*ir.Unit, string) error{refuseUnported, refuseOptionalArrays}
	// A text list and an unbounded map key ARE the unbounded list and the map
	// this target otherwise refuses; the cards that carry them register it in
	// carries_lobster_text_lists.go, so a unit whose lists are ALL text lists
	// and whose maps ALL have unbounded text keys is theirs to emit. Every
	// other list or map is still refused by name.
	if !carries("lobster", carryUnboundedText) || !lobsterOnlyTextLists(u) {
		refusals = append(refusals, refuseLists)
	}
	if !carries("lobster", carryUnboundedText) || !lobsterOnlyUnboundedMapKeys(u) {
		refusals = append(refusals, refuseMaps)
	}
	for _, refuse := range refusals {
		if err := refuse(u, "lobster"); err != nil {
			return nil, err
		}
	}
	files, err := lobster.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice by the lobster target; rename the schema file (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT)", name)
		}
		files[name] = data
	}
	return files, nil
}

// lobsterOnlyTextLists reports that every unbounded list the unit declares is
// a text list (docs/SPEC-TABLES.md §2.13, §2.13.1), the construct carried by
// carries_lobster_text_lists.go.
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

func init() {
	registerBuiltin(lobsterTarget{}, false, false, false, false)
}
