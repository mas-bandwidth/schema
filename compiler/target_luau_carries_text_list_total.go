//go:build schema_leg_luau

// The Luau target carries the TEXT LIST TOTAL, `| total = N` on a text list
// (docs/SPEC-TABLES.md §2.13): the reader clamps the list at the element that
// would cross it and the writer refuses a list past it. The construct is
// registered here, in the target's own file, so no shared refusal line is
// edited and every target that does not register it keeps refusing by name.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauTextListTotalTarget is the Luau target with the text-list total now
// carried: it keeps the text-list carry's neighbour refusals — the unbounded
// element (§2.13.1) and the array root (§16.10) still have their own cards —
// and steps over the total itself (§2.13).
type luauTextListTotalTarget struct{ luauTarget }

func (g luauTextListTotalTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	if err := refuseLuauTextFormNeighboursWithoutTotal(u); err != nil {
		return nil, err
	}
	return g.luauTarget.Generate(u, opts)
}

func refuseLuauTextFormNeighboursWithoutTotal(u *ir.Unit) error {
	if fields := ir.TableUnboundedTexts(u); len(fields) > 0 {
		field := fields[0]
		what, spelling := "unbounded text-list element", "a []string, []?string, [..N]string or [..N]?string list"
		if ir.UnitMapKeyUnbounded(u, field) {
			what, spelling = "unbounded map key", "a map[string]V key"
		}
		return fmt.Errorf("target %q refuses the construct %s on field %s — %s is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13.1)", "luau", what, field, spelling)
	}
	if tables := ir.TableArrayRoots(u); len(tables) > 0 {
		return fmt.Errorf("target %q refuses the construct json = \"array\" on table %s — Go carries the array root first; no other target honours it until its turn (docs/SPEC-TABLES.md §16.10)", "luau", tables[0])
	}
	return nil
}

func init() {
	// THE TEXT LIST is this construct's carrier dependency: a total bounds a
	// text list, so the target carries both here (docs/SPEC-TABLES.md §2.13).
	registerCarried("luau", carryTextList, carryTextListTotal)
	// The text list's own registration ran first and installed its decorator;
	// replace it so the total is carried while its neighbours stay refused.
	for i, g := range builtinTargets {
		if _, ok := g.(luauTextListTarget); ok {
			builtinTargets[i] = luauTextListTotalTarget{}
		}
	}
}
