package compiler

func init() {
	registerCarries("java",
		carryOptionalVariableClosure,
		carryOneSidedRange,
		carryForms,
		carryArrayRoot,
	)
}
