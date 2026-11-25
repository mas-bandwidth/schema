package rusttable

// kind_moved, the second half of the UNLAWFUL lineage entry: the OLD writer
// carries `v int32`, the NEW reader declares `v float32`, a kind off every
// ladder (§5.1 "ladder"). EMIT refuses a kind that moved rather than
// reinterpreting it or counting it and reading on (docs/FIXED-FORM-ALGORITHM.md
// §5.2 EMIT, §5.4, §5.9 #30): no plan is laid, the load refuses
// layout_malformed, and every row handed in is back at its declared defaults
// (lead = 1, v = 0, trail = 2; SPEC-TABLES §6.5), on two loads. Both kinds are
// four bytes, so a reinterpreting leg lands the bits of int32 7 as a float and
// a counting leg lands 0.0 with the brackets read: either loads, and this probe
// is red.

import "testing"

func TestFixedVersioningKindMoved(t *testing.T) {
	rustUnlawfulProbe(t, "kind_moved", "old_kind_moved.bin",
		"VNEW_kind_moved", "VOLD_kind_moved",
		`v.lead = 99; v.v = 5.5; v.trail = 88;`,
		`v.lead == 1 && v.v == 0.0 && v.trail == 2`)
}
