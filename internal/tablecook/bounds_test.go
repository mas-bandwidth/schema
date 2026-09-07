package tablecook

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestArrayExtentArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		at, delta, count, size, base, extent int64
		fits                                 bool
	}{
		{"exact", 16, 16, 4, 8, 0, 64, true},
		{"backwards inside holder", 32, -16, 2, 8, 8, 64, true},
		{"past end", 16, 16, 5, 8, 0, 64, false},
		{"before holder", 32, -32, 1, 8, 8, 64, false},
		{"offset overflow", 16, math.MaxInt64, 1, 8, 0, 64, false},
		{"offset underflow", 16, math.MinInt64, 1, 8, 0, 64, false},
		{"product overflow", 16, 16, math.MaxInt32, 1 << 34, 0, 64, false},
		{"end overflow", 16, 16, 1, math.MaxInt64, 0, 64, false},
		{"zero size", 16, 16, 1, 0, 0, 64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end, ok := arrayExtent(tc.at, tc.delta, tc.count, tc.size, tc.base, tc.extent)
			if ok != tc.fits {
				t.Fatalf("[%d,%d), fits=%v", start, end, ok)
			}
		})
	}
}

func TestPlacedArrayRejectsOverflow(t *testing.T) {
	for _, tc := range []struct{ delta, size int64 }{{math.MaxInt64, 8}, {16, 1 << 34}} {
		buf := make([]byte, 64)
		binary.LittleEndian.PutUint64(buf[16:], uint64(tc.delta))
		binary.LittleEndian.PutUint32(buf[24:], math.MaxInt32)
		s := &scan{buf: buf, ord: binary.LittleEndian, base: 0, extent: 64}
		if _, _, err := s.placedArray(16, tc.size, 8, "element", "list"); err == nil {
			t.Fatal("overflow accepted")
		}
	}
}
