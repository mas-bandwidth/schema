package compiler

// registerCarries names the constructs this target carries. The C++ reference
// now gates every pointer walk on the presence companion of a `?T` over a
// variable closure (docs/SPEC-TABLES.md §2.3), so it declares that construct
// here rather than being refused centrally in compiler/generate.go. The Go
// oracle is internal/codegen/gotable/optionalvariable_test.go's
// TestGoOptionalVariableClosureTable.
func init() {
	registerCarries("cpp", carryArrayRoot, carryOptionalVariableClosure)
}
