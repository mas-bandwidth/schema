//go:build schema_leg_luau

// The Luau target carries the ARRAY ROOT, `json = "array"` (docs/SPEC-TABLES.md
// §16.10): the construct is registered from this file, beside the target's own
// registration, so every other target keeps refusing it by name and no shared
// refusal line is edited.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauArrayRootTarget is the Luau target with the array root now carried. It
// keeps the text-list carry's one remaining neighbour refusal — the unbounded
// element (§2.13.1) — by name. The text-list decorators install only over the
// plain target, so replacing it here keeps the target that already carries the
// text list and its total (§2.13) while adding §16.10.
type luauArrayRootTarget struct{ luauTarget }

func (g luauArrayRootTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	if fields := ir.TableUnboundedTexts(u); len(fields) > 0 {
		field := fields[0]
		what, spelling := "unbounded text-list element", "a []string, []?string, [..N]string or [..N]?string list"
		if ir.UnitMapKeyUnbounded(u, field) {
			what, spelling = "unbounded map key", "a map[string]V key"
		}
		return nil, fmt.Errorf("target %q refuses the construct %s on field %s — %s is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13.1)", "luau", what, field, spelling)
	}
	return g.luauTarget.Generate(u, opts)
}

func init() {
	registerCarried("luau", carryArrayRoot)
	// The text-list decorators run after this file and wrap only their own
	// concrete types; replacing the target here installs the wider carry once
	// (docs/SPEC-TABLES.md §2.13, §16.10).
	for i, g := range builtinTargets {
		if _, ok := g.(luauTarget); ok {
			builtinTargets[i] = luauArrayRootTarget{}
		}
	}
}
