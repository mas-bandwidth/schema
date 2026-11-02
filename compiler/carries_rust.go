package compiler

// Rust carries ?T and the by-value ?[N]T over a variable closure
// (docs/SPEC-TABLES.md §2.3): an absent optional is None, and every walk that
// follows pointer edges (the numbering, the save, the text writer's count)
// descends only through Some; held by
// target_rust_carries_optional_variable_closure_test.go. One-sided integer
// ranges use the normalized endpoints, held across JSON, variable/fixed wire,
// and cook by rusttable.TestRustOneSidedRangeNative. Optional maps are held by
// the native presence, graph, ordering, text and cook probes in rusttable.
// Array-root JSON shape is held across every by-value position by the native
// array-root closure corpus, including named wrong-root diagnostics. Timestamp
// text, variable/fixed/message wire and cook use the shared boundary corpus and
// independent native Go bytes in rusttable's timestamp tests.
func init() {
	registerCarries("rust", carryOptionalVariableClosure, carryOneSidedRange, carryOptionalMap, carryArrayRoot, carryTimestamp)
}
