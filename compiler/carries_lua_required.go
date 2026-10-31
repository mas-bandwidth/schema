package compiler

// docs/SPEC-TABLES.md §2.3: the Lua target carries a table field's `| required`
// marker. The wire read and save and the text form count a missing required
// field and never elide one on write; every surface outside the surface list
// is unchanged.
var _ = carryLua(carryRequired)
