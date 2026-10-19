//go:build schema_leg_luau

// The Luau target carries the TEXT LIST construct (docs/SPEC-TABLES.md
// §2.13): [..N]string(M), [..N]?string(M), []string(M) and []?string(M).
// Only this construct is registered here — the array root (§16.10) and a
// text list's total still have their own cards — so every other target keeps
// refusing it by the same name.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauTextListTarget is the Luau target with the text-list carry's neighbour
// refusals in front of it: carrying the text list makes the central gate step
// over the whole text-form group, so the target itself keeps refusing the
// constructs it does not carry yet (docs/SPEC-TABLES.md §2.13.1, §2.13,
// §16.10) by the same name the central refusal uses.
type luauTextListTarget struct{ luauTarget }

func (g luauTextListTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	if err := refuseLuauTextFormNeighbours(u); err != nil {
		return nil, err
	}
	return g.luauTarget.Generate(u, opts)
}

func refuseLuauTextFormNeighbours(u *ir.Unit) error {
	if fields := ir.TableUnboundedTexts(u); len(fields) > 0 {
		field := fields[0]
		what, spelling := "unbounded text-list element", "a []string, []?string, [..N]string or [..N]?string list"
		if ir.UnitMapKeyUnbounded(u, field) {
			what, spelling = "unbounded map key", "a map[string]V key"
		}
		return fmt.Errorf("target %q refuses the construct %s on field %s — %s is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13.1)", "luau", what, field, spelling)
	}
	if fields := ir.TableTextTotals(u); len(fields) > 0 {
		return fmt.Errorf("target %q refuses the construct text list total on field %s — | total = N on a text list is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13)", "luau", fields[0])
	}
	if tables := ir.TableArrayRoots(u); !carries("luau", carryArrayRoot) && len(tables) > 0 {
		return fmt.Errorf("target %q refuses the construct json = \"array\" on table %s — Go carries the array root first; no other target honours it until its turn (docs/SPEC-TABLES.md §16.10)", "luau", tables[0])
	}
	return nil
}

func init() {
	registerCarried("luau", carryTextList)
	// The target's own registration ran first (target_luau.go); replace it
	// with the decorator so a direct luautable call keeps the surface's older,
	// narrower refusal while the target refuses its neighbours by name.
	for i, g := range builtinTargets {
		if _, ok := g.(luauTarget); ok {
			builtinTargets[i] = luauTextListTarget{}
		}
	}
}
