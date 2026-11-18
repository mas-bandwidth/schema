package compiler

func init() {
	registerCarries("unity", carryUnboundedText, carryRequired, carryTimestamp, carryForms, carryIntegrity, carryTextList, carryTextListTotal, carryOptionalVariableClosure, carryOneSidedRange, carryOptionalMap, carryArrayRoot)
}
