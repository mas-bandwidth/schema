package compiler

// JavaScript carries ?T and the by-value ?[N]T over a variable closure
// (docs/SPEC-TABLES.md §2.3): an absent optional is null, and every walk that
// follows pointer edges returns on it; held by
// target_javascript_carries_optional_variable_closure_test.go.
func init() {
	registerCarries("js", carryOptionalVariableClosure)
}
