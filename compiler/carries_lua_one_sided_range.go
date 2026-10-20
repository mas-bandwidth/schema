//go:build schema_leg_lua

package compiler

// docs/SPEC-TABLES.md §4.3 and SPEC §4.6: an integer range with one side
// declared takes the other side from the type's limit, and the Lua descriptor
// clamps at the filled bound on the wire and JSON surfaces exactly as the
// generated Go does.
var _ = carryLua(carryOneSidedRange)
