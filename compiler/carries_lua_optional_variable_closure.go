package compiler

// docs/SPEC-TABLES.md §2.3: the Lua target carries `?T` over a variable
// closure.
var _ = carryLua(carryOptionalVariableClosure)
