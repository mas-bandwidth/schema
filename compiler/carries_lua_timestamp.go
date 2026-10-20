//go:build schema_leg_lua

package compiler

// docs/SPEC-TABLES.md §2.11: the Lua target carries the `timestamp` kind
// (kind 36) on the variable, fixed and message forms; the descriptors and the
// runtime read and write the int64 nanosecond count.
var _ = carryLua(carryTimestamp)
