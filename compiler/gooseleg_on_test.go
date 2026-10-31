//go:build schema_leg_goose

package compiler

const gooseLegBuilt = true

func init() {
	// the goose table wire's carried constructs (compiler/carries_goose_tablewire.go)
	nonGoCarriers["goose"] = []string{carryTimestamp, carryRequired, carryUnboundedText, carryTextListTotal, carryTextList, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap}
}
