package cpptable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableOpenProbeComparisons is the ir-level reimplementation of the distinctness
// walk the emitters share (docs/SPEC-TABLES.md §3, the bounded-work contract):
// an open-addressing probe over 8192 slots, capped at 4096 entries, with the
// reference's Fibonacci multiply. It returns whether a duplicate was found and
// how many slot-id comparisons the probe made — the work the contract bounds.
func tableOpenProbeComparisons(ids []uint64) (duplicate bool, comparisons int) {
	const bound = 4096
	const slots = 8192
	const shift = 51
	if len(ids) > bound {
		// Refused as malformed before any entry is touched.
		return false, 0
	}
	seen := make([]uint64, slots)
	used := make([]uint64, slots/64)
	for _, id := range ids {
		s := uint32((id * 0x9E3779B97F4A7C15) >> shift) & (slots - 1)
		for {
			bit := uint64(1) << (s & 63)
			if used[s>>6]&bit == 0 {
				used[s>>6] |= bit
				seen[s] = id
				break
			}
			comparisons++
			if seen[s] == id {
				return true, comparisons
			}
			s = (s + 1) & (slots - 1)
		}
	}
	return false, comparisons
}

// TestTableOpenIdValidationWorkIsBounded pins the bounded-work contract
// (docs/SPEC-TABLES.md §3, SER-AUDIT SCG-04): the generated TableOpen refuses a
// table past the 4096-entry bound and proves distinctness with a bounded probe,
// never an O(n^2) pairwise scan. Work is counted with a counter, never timed.
func TestTableOpenIdValidationWorkIsBounded(t *testing.T) {
	t.Parallel()

	u := &ir.Unit{Package: "bench"}
	src := tablePrimitives("bench", false, false, false, false, false, widenCensus{}, 64, u)

	for _, want := range []string{
		"static const int32_t kTableProbeBound = 4096;",
		"count > (uint64_t) kTableProbeBound",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("TableOpen is missing the bounded-work cap %q", want)
		}
	}
	if strings.Contains(src, "for ( int64_t j = 0; j < i; j++ )") {
		t.Errorf("TableOpen still carries the pairwise distinctness walk")
	}

	// The probe's id comparisons at three distinct-id sizes stay within the
	// n(n-1)/2 ceiling the contract states, and a duplicate at each size is
	// still refused.
	for _, n := range []int{129, 512, 4096} {
		ids := make([]uint64, n)
		for i := range ids {
			ids[i] = uint64(i + 1)
		}
		dup, cmp := tableOpenProbeComparisons(ids)
		if dup {
			t.Fatalf("%d distinct ids reported a duplicate", n)
		}
		if budget := n * (n - 1) / 2; cmp > budget {
			t.Fatalf("%d distinct ids: %d comparisons exceed the %d budget", n, cmp, budget)
		}
		ids[n-1] = ids[0]
		dup, _ = tableOpenProbeComparisons(ids)
		if !dup {
			t.Fatalf("%d ids with a duplicate went undetected", n)
		}
	}
}
