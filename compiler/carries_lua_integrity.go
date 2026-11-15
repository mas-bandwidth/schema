package compiler

// docs/SPEC-TABLES.md §2.14: the Lua target carries a table's `integrity`
// trailer: save writes the CRC-32C its declaration promises, load checks it.
var _ = carryLua(carryIntegrity)
