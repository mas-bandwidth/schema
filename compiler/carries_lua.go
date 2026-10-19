//go:build schema_leg_lua

package compiler

func init() {
	// docs/SPEC-TABLES.md §2.12: the Lua target carries a table's `forms`
	// declaration; luatable applies it to the wire, json, block and cook
	// surfaces, and the fixed, view, message and retain surfaces stay
	// governed by the surface list alone.
	//
	// docs/SPEC-TABLES.md §2.14: the Lua target carries a table's `integrity`
	// declaration; luatable writes and checks the fixed and message forms'
	// trailers.
	registerCarries("lua", carryForms, carryIntegrity)
}
