//go:build schema_leg_moo

package compiler

// The constructs compiler/generate.go gates that the moo table surface
// carries: every construct key of compiler/carries.go. Each is round-tripped
// from a wire the Go reference engine wrote, or, for required, from the wire
// the generated Go codec wrote (internal/codegen/mootable/construct_test.go,
// the case of each name).
func init() {
	registerCarries("moo", carryTimestamp, carryRequired, carryForms, carryIntegrity,
		carryUnboundedText, carryTextListTotal, carryTextList, carryArrayRoot,
		carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap)
}
