package compiler

// docs/SPEC-TABLES.md §2.12: the Lua target carries a table's `forms`
// declaration; luatable applies it to the wire, json, block and cook
// surfaces, and the fixed, view, message and retain surfaces stay
// governed by the surface list alone.
var _ = carryLua(carryForms)
