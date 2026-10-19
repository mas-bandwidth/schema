//go:build schema_leg_squirrel

package compiler

// Squirrel carries `?T` over a variable closure: the table runtime gates every
// pointer walk on the presence companion, so an absent value owns no node
// (docs/SPEC-TABLES.md §2.3). The by-value `?[N]T` array shape over a closure
// remains a follow-on refusal (the optional-array gate, compiler/tablesoptionalarrays.go).
func init() {
	registerCarried("squirrel", carryOptionalVariableClosure)
}
