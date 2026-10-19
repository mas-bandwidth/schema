//go:build schema_leg_squirrel

package compiler

// Squirrel carries `?T` and the by-value `?[N]T` over a variable closure: the
// table runtime gates every pointer walk on the presence companion, so an
// absent value owns no node (docs/SPEC-TABLES.md §2.3).
func init() {
	registerCarried("squirrel", carryOptionalVariableClosure)
}
