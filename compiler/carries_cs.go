package compiler

func init() {
	// Native C# construct probes hold the JSON/wire contract; the array root
	// is represented in the descriptor so nested values use the same shape.
	registerCarries("cs", carryRequired, carryTimestamp, carryForms, carryOptionalVariableClosure, carryOneSidedRange, carryOptionalMap, carryArrayRoot)
}
