package compiler

func init() {
	registerCarries("go", carryTimestamp, carryRequired, carryForms, carryIntegrity, carryUnboundedText, carryTextListTotal, carryBoundedTextList, carryTextList, carryArrayRoot, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap)
}
