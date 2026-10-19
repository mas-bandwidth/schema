//go:build schema_leg_lua

package compiler

func init() {
	// docs/SPEC-TABLES.md §2.12: the Lua target carries a table's `forms`
	// declaration; luatable applies it to the wire, json, block and cook
	// surfaces, and the fixed, view, message and retain surfaces stay
	// governed by the surface list alone.
	//
	// docs/SPEC-TABLES.md §2.11: the Lua target carries the `timestamp` kind
	// (kind 36) on the variable, fixed and message forms; the descriptors and
	// the runtime read and write the int64 nanosecond count.
	registerCarries("lua", carryForms, carryTimestamp)
}
