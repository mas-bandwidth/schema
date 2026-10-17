package gotable

import "testing"

func TestGoExactIntegerRangeBounds(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Big {
	n int64 | min = 0, max = 9007199254740993
	m int64 | min = -9223372036854775807, max = 7
	u uint64 | min = 0, max = 18446744073709551614
}
`, `package probe
import("testing")
func TestExact(t *testing.T) {
	if BigTableFields[0].RangeIntMax != 9007199254740993 {
		t.Fatalf("n RangeIntMax = %d", BigTableFields[0].RangeIntMax)
	}
	if BigTableFields[1].RangeIntMin != -9223372036854775807 {
		t.Fatalf("m RangeIntMin = %d", BigTableFields[1].RangeIntMin)
	}
	if BigTableFields[2].RangeUintMax != 18446744073709551614 {
		t.Fatalf("u RangeUintMax = %d", BigTableFields[2].RangeUintMax)
	}
	var v Big
	var r TableReport
	text := []byte("{\"n\":9007199254740994,\"m\":-9223372036854775808,\"u\":18446744073709551615}")
	if !BigFromJson(&v, text, &r) || v.N != 9007199254740993 || v.M != -9223372036854775807 || v.U != 18446744073709551614 {
		t.Fatalf("clamp read %+v report %+v", v, r)
	}
}
`)
}
