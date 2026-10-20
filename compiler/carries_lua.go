//go:build schema_leg_lua

package compiler

func init() {
	// docs/SPEC-TABLES.md §2.12: the Lua target carries a table's `forms`
	// declaration; luatable applies it to the wire, json, block and cook
	// surfaces, and the fixed, view, message and retain surfaces stay
	// governed by the surface list alone.
	//
	// docs/SPEC-TABLES.md §2.13, §2.13.1: Lua carries text lists and their
	// byte total, and unbounded text lists and map keys, in every form.
	registerCarries("lua", carryForms, carryTextList, carryTextListTotal, carryUnboundedText)
}
