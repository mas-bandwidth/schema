package gotable

import "testing"

func TestGoExactIntegerRangeBounds(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Big {
	n int64 | min = 0, max = 9007199254740993
	m int64 | min = -9223372036854775807, max = 7
	u uint64 | min = 0, max = 18446744073709551614
	w int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
}
`, `package probe
import("testing";"bytes")
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
	if BigTableFields[3].RangeIntMin != 0 || BigTableFields[3].RangeIntMax != 0 {
		t.Fatalf("w int128 must carry no int64 pair: %d %d", BigTableFields[3].RangeIntMin, BigTableFields[3].RangeIntMax)
	}
	var v Big
	var r TableReport
	text := []byte("{\"n\":9007199254740994,\"m\":-9223372036854775808,\"u\":18446744073709551615}")
	if !BigFromJsonTolerant(&v, text, &r) || v.N != 9007199254740993 || v.M != -9223372036854775807 || v.U != 18446744073709551614 {
		t.Fatalf("clamp read %+v report %+v", v, r)
	}
	v = Big{}
	v.N = 9007199254740993
	v.M = -9223372036854775807
	v.U = 18446744073709551614
	if n := BigMeasure(&v); n < 0 {
		t.Fatalf("measure: %d", n)
	} else {
		b := make([]byte, n)
		if BigSave(&v, b) != n {
			t.Fatal("save")
		}
		var got Big
		if !BigLoad(&got, b, &r) || got.N != v.N || got.M != v.M || got.U != v.U {
			t.Fatalf("binary roundtrip %+v", got)
		}
	}
	a, c := make([]byte, 4096), make([]byte, 4096)
	na, nc := BigToJson(&v, a), BigToJson(&v, c)
	if na < 0 || nc != na || !bytes.Equal(a[:na], c[:nc]) {
		t.Fatalf("text encode")
	}
	var got Big
	if !BigFromJson(&got, a[:na], &r) || got.N != v.N || got.M != v.M || got.U != v.U {
		t.Fatalf("text roundtrip %+v", got)
	}
}
`)
}
