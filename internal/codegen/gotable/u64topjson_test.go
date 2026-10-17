package gotable

import "testing"

// An in-range uint64 past MaxInt64 reads from JSON unclamped: the reader
// compares an unsigned field on the unsigned scale against its exact range
// (docs/SPEC-TABLES.md §8.1), so 2^64-1 under max = 2^64-1 is inside it, and
// a value under min still clamps to min. Found by the emitted round-trip
// tests of §16.11 (RangesTable u64_span at its high bound).
func TestGoJsonReadsInRangeU64TopUnclamped(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Top {
	u uint64 = 1 | min = 1, max = 18446744073709551615
	h uint64 = 9223372036854775808 | min = 9223372036854775808, max = 18446744073709551615
}
`, `package probe
import "testing"
func TestTop(t *testing.T) {
	var v Top
	var r TableReport
	if !TopFromJson(&v, []byte("{\"u\":18446744073709551615,\"h\":9223372036854775809}"), &r) {
		t.Fatalf("read refused: %+v", r)
	}
	if v.U != 18446744073709551615 || v.H != 9223372036854775809 || r.Clamped != 0 {
		t.Fatalf("in-range u64 read %+v report %+v", v, r)
	}
	v, r = Top{}, TableReport{}
	if !TopFromJson(&v, []byte("{\"u\":0,\"h\":5}"), &r) || v.U != 1 || v.H != 9223372036854775808 || r.Clamped != 2 {
		t.Fatalf("under-min u64 read %+v report %+v", v, r)
	}
}
`)
}
