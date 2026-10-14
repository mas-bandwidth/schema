// WHAT THE LOCK SAYS TODAY ABOUT A COMPRESSED FLOAT'S RANGE AND RESOLUTION.
//
// The rule under red team is Rowan's: under a FIXED table a compressed float
// rides as the float32 itself (docs/SPEC-TABLES.md §3.4's row), so `min`, `max`
// and `res` are DEFINITIONS and not bytes, the RANGE may widen, the RESOLUTION
// may refine, and an old file reads exactly under the new definition.
//
// These five cases pin THE TIP's answer, which is NOT that rule: `monotone.go`'s
// rangeRule tests the resolution FIRST and refuses ANY move of it, refined or
// coarsened, before it ever looks at the bounds. So the tip is the strictest
// candidate — "no compressed-float resolution may change at all" — while the
// bounds follow the ordinary outward/inward law.
//
// Read these beside internal/codegen/gotable/fixedcfloat_test.go's
// TestFixedCFloatResolutionIsInTheLayoutHash: since #956 the fixed layout
// hash carries `res` under 'Q', so the lock's refusal and the wire's identity
// say the same thing.
package lockfile_test

import "testing"

// cfRow is `Row` with one compressed float, the triple spelled by the caller.
func cfRow(triple string) string {
	return rowTable("    v float32 | " + triple)
}

const cfBase = "min = 0, max = 1, resolution = 0.1"

// ---- the bounds: the ordinary law ----

// THE RANGE WIDENS AT THE MIN, by a non-multiple of `res`. The lock takes it
// and the layout hash moves, which is the lineage's one new entry. The read is
// proved exact by the Go leg's TestFixedCFloatRangeWidensMinOffGrid.
func TestLockCFloatRangeWidensAllows(t *testing.T) {
	rowAllows(t, cfRow(cfBase), cfRow("min = -0.05, max = 1, resolution = 0.1"), "Row")
}

// THE RANGE NARROWS: every record already written whose value is above the new
// max now clamps, so the lock refuses by name.
func TestLockCFloatRangeNarrowsRefuses(t *testing.T) {
	rowRefuses(t, cfRow(cfBase), cfRow("min = 0, max = 0.5, resolution = 0.1"),
		"range narrowed", "keeps its range")
}

// THE MIN MOVES INWARD while the max moves OUTWARD: one end outward is not a
// widening, and the refusal is the same one. The triple straddles zero on both
// sides, because a range that excludes zero needs a declared default and moving
// a default is its own refusal — one finding per case.
func TestLockCFloatMinMovesInwardRefuses(t *testing.T) {
	rowRefuses(t, cfRow("min = -1, max = 1, resolution = 0.1"),
		cfRow("min = -0.5, max = 2, resolution = 0.1"),
		"range narrowed", "keeps its range")
}

// ---- the resolution: finer is a widening, coarser is refused ----
//
// The red team (#942) found the lock refusing a resolution change in BOTH
// directions with the variable form's sentence, while the fixed layout hash
// did not carry the resolution at all. The ruling (#956): the resolution is a
// definition that only widens, FINER is lawful and COARSER is refused by name,
// and the fixed layout hash carries the step under 'Q'. These pin the ruling.

// THE RESOLUTION REFINES, 0.1 -> 0.01, with the bounds held. Every value on the
// old, coarser grid is a value the new reader holds exactly (§2): a widening,
// carried by `schema lock`.
func TestLockCFloatResolutionRefinedAllowed(t *testing.T) {
	rowAllows(t, cfRow(cfBase), cfRow("min = 0, max = 1, resolution = 0.01"), "Row")
}

// THE RESOLUTION COARSENS, 0.1 -> 0.5: a coarser grid is a narrower set, and
// the refusal names the two steps.
func TestLockCFloatResolutionCoarsenedRefuses(t *testing.T) {
	rowRefuses(t, cfRow(cfBase), cfRow("min = 0, max = 1, resolution = 0.5"),
		"resolution coarsened", "0.1 -> 0.5")
}

// THE WHOLE TRIPLE IS DROPPED and the field becomes a plain `float32`. In the
// fixed form that moves not one byte (§3.4): a `float32` and a compressed
// `float32` are the same four bytes, and every old value is inside an
// unbounded field and lands exactly (§2's "or REMOVED"). The red team found
// this refused by the resolution check standing ahead of the removal rule; the
// removal rule now comes first, and this is the widening it always was.
func TestLockCFloatRangeDroppedAllowed(t *testing.T) {
	rowAllows(t, cfRow(cfBase), rowTable("    v float32"), "Row")
}
