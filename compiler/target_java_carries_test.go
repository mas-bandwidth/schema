package compiler

func init() {
	nonGoCarriers["java"] = append(nonGoCarriers["java"],
		carryOneSidedRange,
		carryForms,
		carryArrayRoot,
	)
}
