//go:build schema_leg_lua

package compiler

// docs/SPEC-TABLES.md §2.14: the Lua target carries a table's `integrity`
// declaration; luatable writes and checks the fixed and message forms'
// trailers.
var _ = carryLua(carryIntegrity)
