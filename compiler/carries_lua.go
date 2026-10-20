//go:build schema_leg_lua

package compiler

func init() {
	// docs/SPEC-TABLES.md §2.12: the Lua target carries a table's `forms`
	// declaration; luatable applies it to the wire, json, block and cook
	// surfaces, and the fixed, view, message and retain surfaces stay
	// governed by the surface list alone. §16.10: it carries the JSON array
	// root too, writing and reading the one field's bare array.
	registerCarries("lua", carryForms, carryOptionalVariableClosure, carryArrayRoot)
}
