package compiler

// Squirrel carries the ONE-SIDED INTEGER RANGE (docs/SPEC-TABLES.md §4.3,
// SPEC §4.6): the checker fills the missing side from the integer kind's
// limit, so the descriptor carries the two-sided clamp the runtime reads.
func init() {
	registerCarried("squirrel", carryOneSidedRange)
}
