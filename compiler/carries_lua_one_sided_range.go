package compiler

// docs/SPEC-TABLES.md §4, SPEC §4.6: the Lua target carries a one-sided
// integer range, the missing bound being the type's own limit.
var _ = carryLua(carryOneSidedRange)
