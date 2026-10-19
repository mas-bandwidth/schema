//go:build schema_leg_luau

package compiler

// SPEC-TABLES.md §2.12: the Luau target accepts the forms declaration and
// its table emitter applies it to the supported wire and JSON surfaces.
func init() { registerCarried("luau", carryForms) }
