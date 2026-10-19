//go:build schema_leg_luau

// The Luau target carries UNBOUNDED TEXT (docs/SPEC-TABLES.md §2.13.1) in
// text lists and as map keys: the construct is registered from this file,
// beside the target's own registration, so every other target keeps refusing
// it by name and no shared refusal line is edited.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	registerCarried("luau", carryUnboundedText)
	carryLuauBesideUnboundedText()
}

// carryLuauBesideUnboundedText keeps the constructs that share the central
// text-forms gate with unbounded text refused by name. The gate is one
// condition in compiler/generate.go, so naming luau as its carrier would
// otherwise open the rest of the group; the text list and its total are
// already carried here, so only the array root still refuses, and the
// emitters are untouched.
func carryLuauBesideUnboundedText() {
	for i, g := range builtinTargets {
		if len(g.Names()) > 0 && g.Names()[0] == "luau" {
			builtinTargets[i] = luauCarriesUnboundedTextTarget{}
			return
		}
	}
	panic("compiler: the luau target must register before it carries unbounded text")
}

// luauCarriesUnboundedTextTarget is the Luau target with the neighbour
// refusals in front of it. It wraps the target itself, not the text-list
// decorator: once unbounded text is carried the decorator's unbounded refusal
// is stale, and the text list it already carried stays carried.
type luauCarriesUnboundedTextTarget struct{ luauTarget }

func (luauCarriesUnboundedTextTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	if err := refuseLuauOtherTextForms(u); err != nil {
		return nil, err
	}
	return luauTarget{}.Generate(u, opts)
}

// refuseLuauOtherTextForms is the part of compiler/tablesstringarrays.go's
// refuseTextForms for the constructs Luau still does not carry (docs/
// SPEC-TABLES.md §2.13, §16.10). An unbounded text-list element or key, the
// text list and its total are carried, so they are skipped; an array root
// still refuses by the same name.
func refuseLuauOtherTextForms(u *ir.Unit) error {
	if tables := ir.TableArrayRoots(u); len(tables) > 0 {
		return fmt.Errorf("target %q refuses the construct json = \"array\" on table %s — Go carries the array root first; no other target honours it until its turn (docs/SPEC-TABLES.md §16.10)", "luau", tables[0])
	}
	return nil
}
