//go:build schema_leg_lua

package compiler

// docs/SPEC-TABLES.md §2.8: the Lua target carries the optional map `?map[K]V`
// and its presence companion.
var _ = carryLua(carryOptionalMap)
