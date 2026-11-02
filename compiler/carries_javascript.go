package compiler

// JavaScript carries ?T and the by-value ?[N]T over a variable closure
// (docs/SPEC-TABLES.md §2.3): an absent optional is null, and every walk that
// follows pointer edges returns on it; held by
// target_javascript_carries_optional_variable_closure_test.go.
// One-sided ranges and optional maps are executed against the neutral oracle
// by jstable/carried_contracts_test.go, including present-empty maps and exact
// uint64 boundaries on both variable and fixed wire forms.
func init() {
	registerCarries("js", carryOptionalVariableClosure, carryOneSidedRange, carryOptionalMap, carryArrayRoot, carryTimestamp, carryRequired, carryForms, carryIntegrity, carryTextList, carryTextListTotal, carryUnboundedText)
}
