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
// TestFixedCFloatResolutionIsNotInTheLayoutHash, which shows the fixed layout
// hash does not carry `res` at all: the lock refuses a change the wire cannot
// see.
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

// ---- the resolution: refused in BOTH directions, which is the finding ----

// THE RESOLUTION REFINES, 0.1 -> 0.01, with the bounds held. Under Rowan's rule
// this is lawful; the tip refuses it, and the sentence it refuses with —
// "the bounds and the resolution are the scale a stored value is read back at,
// so moving them reads every record already written as a different number" — is
// the VARIABLE form's reason, not the fixed form's. §3.4 says the fixed record
// carries the float and no scale at all.
func TestLockCFloatResolutionRefinedRefusedAtTheTip(t *testing.T) {
	rowRefuses(t, cfRow(cfBase), cfRow("min = 0, max = 1, resolution = 0.01"),
		"resolution changed", "keeps its range")
}

// THE RESOLUTION COARSENS, 0.1 -> 0.5. Refused by the same sentence, and here
// the sentence is right for every form: a coarser grid is a narrower set.
func TestLockCFloatResolutionCoarsenedRefuses(t *testing.T) {
	rowRefuses(t, cfRow(cfBase), cfRow("min = 0, max = 1, resolution = 0.5"),
		"resolution changed", "keeps its range")
}

// THE WHOLE TRIPLE IS DROPPED and the field becomes a plain `float32`.
//
// A FINDING, AND A DEAD BRANCH. `monotone.go`'s rangeRule carries the rule
// "a range removed is a widening" — `case want.ranged() && !got.ranged():
// return "", true` — and for a COMPRESSED FLOAT that case is UNREACHABLE: the
// `want.Res != got.Res` test stands ahead of it, an unranged entry carries no
// Res, so dropping the triple is reported as "resolution changed" and refused
// before the removal rule is ever consulted. The branch is live only for an
// int-ranged field, where both sides carry an empty Res.
//
// It matters because in the fixed form dropping the triple moves NOT ONE BYTE —
// a `float32` and a compressed `float32` are the same four bytes (§3.4) — so
// this is the most obviously lawful edit of the six and the one whose refusal
// names the wrong rule. Pinned as the tip's answer: the day the removal rule is
// meant to reach a compressed float, this test is what says it did not.
func TestLockCFloatRangeDroppedRefusedByTheResolutionCheckFirst(t *testing.T) {
	rowRefuses(t, cfRow(cfBase), rowTable("    v float32"),
		"resolution changed", "-> unranged")
}
