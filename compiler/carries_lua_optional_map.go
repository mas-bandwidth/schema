package compiler

// docs/SPEC-TABLES.md §2.8: the Lua target carries an optional map, `?map`,
// whose absence is its own state on every surface.
var _ = carryLua(carryOptionalMap)
