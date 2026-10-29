package compiler

// Luau carries every construct but integrity (docs/SPEC-TABLES.md §2.14);
// each carried construct is held by its target_luau_carries_*_test.go.
func init() {
	registerCarries("luau", carryTimestamp, carryRequired, carryForms, carryUnboundedText, carryTextListTotal, carryTextList, carryArrayRoot, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap)
}
