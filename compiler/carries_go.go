package compiler

func init() {
	registerCarries("go", carryTimestamp, carryRequired, carryForms, carryIntegrity, carryUnboundedText, carryTextListTotal, carryTextList, carryArrayRoot, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap)
}
