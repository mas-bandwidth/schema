package compiler

// Rust carries ?T and the by-value ?[N]T over a variable closure
// (docs/SPEC-TABLES.md §2.3): an absent optional is None, and every walk that
// follows pointer edges (the numbering, the save, the text writer's count)
// descends only through Some; held by
// target_rust_carries_optional_variable_closure_test.go. One-sided integer
// ranges use the normalized endpoints, held across JSON, variable/fixed wire,
// and cook by rusttable.TestRustOneSidedRangeNative.
func init() {
	registerCarries("rust", carryOptionalVariableClosure, carryOneSidedRange)
}
