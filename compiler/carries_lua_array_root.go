package compiler

// docs/SPEC-TABLES.md §16.10: the Lua target carries the JSON array root,
// writing and reading the one field's bare array.
var _ = carryLua(carryArrayRoot)
