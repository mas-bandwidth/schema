//go:build schema_leg_luau

package compiler

// Luau carries the ONE-SIDED INTEGER RANGE (docs/SPEC-TABLES.md §4.3,
// SPEC §4.6): the checker has already filled the missing side from the
// kind's own limit, so the descriptor carries the two-sided clamp the
// runtime walks read and the rest of the construct needs no second rule.
//
// The base's registerCarries refuses a second registration for a target,
// and its init already registers luau with no constructs; this adds the one
// construct to that set. A registration belongs in that target's own
// carries_<target>.go file.
func init() {
	set := targetCarries["luau"]
	if set == nil {
		set = map[string]struct{}{}
		targetCarries["luau"] = set
	}
	set[carryOneSidedRange] = struct{}{}
}
