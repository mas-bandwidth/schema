package compiler

// Rust carries ?T and the by-value ?[N]T over a variable closure
// (docs/SPEC-TABLES.md §2.3): an absent optional is None, and every walk that
// follows pointer edges (the numbering, the save, the text writer's count)
// descends only through Some; held by
// target_rust_carries_optional_variable_closure_test.go.
func init() {
	registerCarries("rust", carryOptionalVariableClosure)
}
