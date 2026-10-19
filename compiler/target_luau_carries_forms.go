//go:build schema_leg_luau

package compiler

import "github.com/mas-bandwidth/schema/v2/ir"

// SPEC-TABLES.md §2.12: the Luau target accepts the forms declaration and
// its table emitter applies it to the wire, json, block and cook surfaces.
var _ = func() struct{} {
	registerCarries("luau", carryForms)
	return struct{}{}
}()

// GenerateLineage routes the Luau target directly through luauTarget.Generate
// once all text-form constructs (including json = "array") are carried.
func (luauTarget) GenerateLineage(u *ir.Unit, opts Options, _ *FixedLineage) (map[string][]byte, error) {
	return luauTarget{}.Generate(u, opts)
}
