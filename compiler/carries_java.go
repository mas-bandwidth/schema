package compiler

func init() {
	registerCarries("java",
		carryOptionalVariableClosure,
		carryOneSidedRange,
		carryForms,
		carryArrayRoot,
		carryTimestamp,
		carryOptionalMap,
		carryRequired,
		carryIntegrity,
		carryTextList,
		carryTextListTotal,
		carryUnboundedText,
	)
}
