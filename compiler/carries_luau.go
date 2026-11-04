package compiler

// Luau carries every construct;
// each carried construct is held by its target_luau_carries_*_test.go.
func init() {
	registerCarries("luau", carryTimestamp, carryRequired, carryForms, carryIntegrity, carryUnboundedText, carryTextListTotal, carryTextList, carryArrayRoot, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap)
}
