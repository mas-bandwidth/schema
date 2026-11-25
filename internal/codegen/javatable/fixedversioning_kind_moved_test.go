package javatable

// kind_moved, the second half of the UNLAWFUL lineage entry
// (docs/FIXED-FORM-VERSIONING-TESTS.md row `kind_moved`): the OLD writer
// carries `v int32`, the NEW reader declares `v float32`, a kind off every
// ladder (§5.1 "ladder"). EMIT refuses a kind that moved rather than
// reinterpreting it or counting it and reading on (docs/FIXED-FORM-ALGORITHM.md
// §5.2 EMIT, §5.4, §5.9 #30): no plan is laid, the load refuses
// `layoutMalformed` with every counter zero, and every row handed in, poisoned
// first, is back at its declared defaults (lead 1, v 0, trail 2; SPEC-TABLES
// §6.5), on every load. Both kinds are four bytes, so a reinterpreting leg
// lands the bits of int32 7 as a float and a counting leg lands 0.0f with the
// brackets read: either loads, and this probe is red. The OLD entry is handed
// in exactly as the unknown_census row hands in its own.
import "testing"

func TestFixedVersioningKindMoved(t *testing.T) {
	file := javaUnlawfulRequired(t, "old_kind_moved.bin")
	javaUnlawfulRun(t, "kind_moved", file,
		`v.lead = 99; v.v = 5.5f; v.trail = 88;`,
		`v.lead == 1 && v.v == 0.0f && v.trail == 2`)
}
