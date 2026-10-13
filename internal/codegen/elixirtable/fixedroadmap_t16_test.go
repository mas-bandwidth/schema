package elixirtable

// TestFixedRoadmapElixirT16OrdinalLanes is the ELIXIR leg's R31 proof:
// full-width enum and union ordinals ride in full-width lanes
// (docs/FIXED-FORM-ALGORITHM.md §4.1 "arg IS FULL WIDTH AND SO IS arg2: no byte
// lane anywhere", §4.5 the ordinal read at its own width, §5.2 "the remap table
// is as long as the WRITER's variant count" and §5.8 row 14 "past the uint16_t
// length word the plan refuses by name"). One subtest, elixir/R31, t.Parallel()
// first.
//
// The remap table now carries its LENGTH WORD in slot 0 — the writer's variant
// count — and ordinal/3 reads it, so a writer whose variant count passes the
// uint16 length word refuses by name, :plan_too_large, rather than silently
// dropping the 65536th variant on. The runtime-text clauses (the guard chain's
// outer link, the unbounded ordinal temporary) are read off the emitted runtime
// body, and the length word and its refusal are the NEW text this card adds.

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

	// §4.5: the ordinal reads at its own width through an unbounded integer.
	t16Has(t, rt, "raw::little-unsigned-size(^size)-unit(8)",
		"R31 the ordinal op reads through an unbounded (64-bit or wider) temporary")

	// §5.2: the remap carries its LENGTH WORD in slot 0, and ordinal/3 reads it.
	t16Has(t, rt, "List.to_tuple([their_variants | remap])",
		"R31 the remap table's length word is the writer's variant count")
	t16Has(t, rt, "elem(remap, 0)",
		"R31 the ordinal op reads the length word from slot 0")

	// §5.8 row 14: a writer whose variant count passes the uint16 length word
	// refuses by name, :plan_too_large, never layout_record_too_large.
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
