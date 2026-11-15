package gotable

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/tables/goruntime"
)

// TestGoFixedWidenTo128IsExact drives the fixed plan interpreter's widen arm
// into a sixteen-byte destination at every lower fixed-point rung the Go plan
// admits. A signed source lands its sign through both destination words; an
// unsigned source leaves the high word zero.
func TestGoFixedWidenTo128IsExact(t *testing.T) {
	t.Parallel()
	const ones = ^uint64(0)
	cases := []struct {
		name   string
		size   uint32
		sign   uint8
		raw    uint64
		lo, hi uint64
	}{
		{"signed-8-minimum", 1, 1, 0x80, 0xFFFFFFFFFFFFFF80, ones},
		{"signed-8-minus-one", 1, 1, 0xFF, ones, ones},
		{"signed-8-positive", 1, 1, 0x7F, 0x7F, 0},
		{"signed-16-minimum", 2, 1, 0x8000, 0xFFFFFFFFFFFF8000, ones},
		{"signed-16-positive", 2, 1, 0x7FFF, 0x7FFF, 0},
		{"signed-32-minimum", 4, 1, 0x80000000, 0xFFFFFFFF80000000, ones},
		{"signed-32-minus-one", 4, 1, 0xFFFFFFFF, ones, ones},
		{"signed-64-minimum", 8, 1, 0x8000000000000000, 0x8000000000000000, ones},
		{"signed-64-minus-one", 8, 1, 0xFFFFFFFFFFFFFFFF, ones, ones},
		{"signed-64-positive", 8, 1, 0x7FFFFFFFFFFFFFFF, 0x7FFFFFFFFFFFFFFF, 0},
		{"unsigned-8-maximum", 1, 0, 0xFF, 0xFF, 0},
		{"unsigned-8-top-bit", 1, 0, 0x80, 0x80, 0},
		{"unsigned-16-maximum", 2, 0, 0xFFFF, 0xFFFF, 0},
		{"unsigned-32-maximum", 4, 0, 0xFFFFFFFF, 0xFFFFFFFF, 0},
		{"unsigned-32-top-bit", 4, 0, 0x80000000, 0x80000000, 0},
		{"unsigned-64-maximum", 8, 0, ones, ones, 0},
		{"unsigned-64-top-bit", 8, 0, 0x8000000000000000, 0x8000000000000000, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := make([]byte, 16)
			for i := range c.size {
				src[i] = byte(c.raw >> (8 * i))
			}
			dst := make([]byte, 16)
			for i := range dst {
				dst[i] = 0x5A
			}
			plan := []goruntime.TableFixedEntry{{
				Src: 0, Dst: 0, Size: c.size, Guard: goruntime.TableFixedNoGuard,
				Op: goruntime.TableFixedWiden, DstSize: 16, Sign: c.sign,
			}}
			var report goruntime.TableReport
			if why := goruntime.TableFixedRun(plan, 1, src, dst, &report); why != "" {
				t.Fatalf("the widen refused: %q", why)
			}
			if report.Widened != 1 {
				t.Fatalf("widened = %d, want 1", report.Widened)
			}
			lo := binary.NativeEndian.Uint64(dst[0:8])
			hi := binary.NativeEndian.Uint64(dst[8:16])
			if lo != c.lo || hi != c.hi {
				t.Fatalf("landed lo=%#016x hi=%#016x, want lo=%#016x hi=%#016x", lo, hi, c.lo, c.hi)
			}
		})
	}
}
