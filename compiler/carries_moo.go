//go:build schema_leg_moo

package compiler

// The constructs compiler/generate.go gates that the moo table surface
// carries, each round-tripped from a wire the Go reference engine wrote
// (internal/codegen/mootable/construct_test.go, the case of each name).
//
// NOT registered, and so refused by name: required (its oracle is the
// generated Go codec, not the engine's Encode, which writes no required
// field at its default), forms (the block, cook and json runtimes must honour
// it per table first), unbounded-text (the Go engine's tabletext places an
// unbounded text list at capacity 0, so the engine is no oracle for it yet),
// array-root (a json form fact, the text forms' layer).
func init() {
	registerCarries("moo", carryTimestamp, carryIntegrity, carryTextList, carryTextListTotal,
		carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap)
}
