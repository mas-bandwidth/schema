package compiler

func init() {
	registerCarries(
		"c",
		carryOptionalVariableClosure,
		carryOneSidedRange,
		carryOptionalMap,
		carryArrayRoot,
		carryTimestamp,
		carryForms,
		carryIntegrity,
		carryRequired,
		carryTextList,
		carryTextListTotal,
		carryUnboundedText,
	)
}
