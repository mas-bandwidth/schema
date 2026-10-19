//go:build schema_leg_luau

package compiler

// SPEC-TABLES.md §2.12: the Luau target accepts the forms declaration and
// its table emitter applies it to the wire, JSON and block-read surfaces.
func init() { registerCarried("luau", carryForms) }
