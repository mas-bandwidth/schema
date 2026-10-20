//go:build schema_leg_lua

package compiler

func init() {
	// docs/SPEC-TABLES.md §2.12: the Lua target carries a table's `forms`
	// declaration; luatable applies it to the wire, json, block and cook
	// surfaces, and the fixed, view, message and retain surfaces stay
	// governed by the surface list alone.
	// docs/SPEC-TABLES.md §4.3 and SPEC §4.6: a one-sided integer range is the
	// two-sided declaration, and the Lua descriptor clamps at the filled bound
	// on the wire and JSON surfaces exactly as the generated Go does.
	registerCarries("lua", carryForms, carryOptionalVariableClosure, carryOneSidedRange)
}
