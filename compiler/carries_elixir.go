package compiler

// Elixir carries ?T and the by-value ?[N]T over a variable closure
// (docs/SPEC-TABLES.md §2.3): an absent optional is nil, and every walk that
// follows pointer edges (the node collection, the save, the measure and the
// retaining save) returns on nil before it descends; held by
// target_elixir_carries_optional_variable_closure_test.go and by conformance
// over tblg1 (optional_absent_no_edge, optional_present_edge).
func init() {
	registerCarries("elixir", carryOptionalVariableClosure)
}
