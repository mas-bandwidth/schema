package elixirtable

// TestFixedRoadmapElixirT16OrdinalLanes is the ELIXIR leg's R31 proof:
// full-width enum and union ordinals ride in full-width lanes
// (docs/FIXED-FORM-ALGORITHM.md §4.1 "arg IS FULL WIDTH AND SO IS arg2: no byte
// lane anywhere", §4.5 the ordinal read at its own width, §5.2 "the remap table
// is as long as the WRITER's variant count" and §5.8 row 14 "past the uint16_t
// length word the plan refuses by name"). One subtest, elixir/R31, t.Parallel()
// first.
//
// NOTHING HERE SHELLS OUT. Every clause is a sentence about the shared runtime
// body (fixedRuntimeBody) this leg emits beside every fixed module, so the
// harness is reachable on a tree that never built the C++ reference corpus.
//
// THE THREE CLAUSES THIS LEG'S t06 LEFT UNPROVED, now held:
//   - the OUTER TAG (arg2) rides the guard CHAIN — an arm inside an arm answers
//     to every tag above it, and the compile prepends the outer link
//     (`guard ++ [{their_at, argw}]`), so an inner arm's selection survives the
//     outer tag (§4.1, §5.8 row 6);
//   - the ordinal reads at its own width through an unbounded integer, never a
//     fixed 32-bit lane (§4.5: an ordinal width of 8 is admissible);
//   - a writer whose variant count does not fit the uint16 length word refuses
//     by name at compile, :plan_too_large, never layout_record_too_large (§5.8
//     row 14).

import (
	"strings"
	"testing"
)

func TestFixedRoadmapElixirT16OrdinalLanes(t *testing.T) {
	t.Parallel()
	t.Run("elixir/R31", func(t *testing.T) {
		t.Parallel()
		t16ElixirR31(t)
	})
}

func t16ElixirR31(t *testing.T) {
	rt := fixedRuntimeBody

	// §4.1 / §5.8 row 6: the OUTER tag is a second guard link, conjoined, never
	// overwriting the inner one.
	t16Has(t, rt, "guard ++ [{their_at, argw}]",
		"R31 an arm inside an arm answers to the outer tag too (arg2 is the guard chain's outer link)")

	// §4.5: the ordinal reads at its own width through an unbounded integer —
	// this leg has no fixed-width lane to truncate an eight-byte ordinal.
	t16Has(t, rt, "raw::little-unsigned-size(^size)-unit(8)",
		"R31 the ordinal op reads through an unbounded (64-bit or wider) temporary")

	// §5.2 / §5.8 row 14: a writer whose variant count does not fit the uint16
	// length word refuses by name at compile, plan_too_large.
	t16Has(t, rt, "their_variants > 65535",
		"R31 a writer past the uint16 length word is refused at compile")
	t16Has(t, rt, "Map.put(report, :plan_too_large, true)",
		"R31 the length-word overflow is plan_too_large, never layout_record_too_large")
	t16Has(t, rt, "report[:plan_too_large] ->",
		"R31 the compile answers the length-word overflow by name")
}

func t16Has(t *testing.T, src, want, rule string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted runtime does not carry %q", rule, want)
	}
}
