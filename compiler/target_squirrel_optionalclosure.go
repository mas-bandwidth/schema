package compiler

// Squirrel carries the scalar `?T` over a variable closure: the table runtime
// gates every pointer walk on the presence companion, so an absent value owns
// no node (docs/SPEC-TABLES.md §2.3). The by-value optional array `?[N]T` is
// the optional-array construct, whose follow-on Squirrel has not reached; this
// registration does not claim it.
func init() {
	registerCarried("squirrel", carryOptionalVariableClosure)
}
