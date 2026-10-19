//go:build schema_leg_luau

package compiler

// Luau carries the ONE-SIDED INTEGER RANGE (docs/SPEC-TABLES.md §4.3): the
// checker fills the missing side from the integer kind's limit, so the
// descriptor carries the two-sided clamp the runtime reads.
func init() {
	registerCarried("luau", carryOneSidedRange)
}
