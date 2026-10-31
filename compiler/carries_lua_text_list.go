package compiler

// docs/SPEC-TABLES.md §2.13, §2.13.1, §16.2: the Lua target carries a text
// list, its byte total and unbounded text (a list element and a map key) in
// every surface the generator gives them.
var _ = carryLua(carryTextList)
var _ = carryLua(carryTextListTotal)
var _ = carryLua(carryUnboundedText)
