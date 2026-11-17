package gotable

import "testing"

// TestGoTextFormFloat32BoundIsExact holds a float32 field's declared range to
// its own width (docs/SPEC-TABLES.md §16.9.2): the token is converted once to
// float32, so the comparison against the bound is the float32 comparison. A
// value between the bound's float64 and float32 roundings therefore reads as
// the bound rather than as a value past it, while a value past the float32
// rounding is still refused.
func TestGoTextFormFloat32BoundIsExact(t *testing.T) {
	t.Parallel()
	runTextFormUnit(t, textFormFloat32BoundSchema, map[string]string{
		"textformfloat32bound_test.go": textFormFloat32BoundProbe,
	})
}

// textFormFloat32BoundSchema is a fixed root whose one field has a range whose
// ends are not exactly representable in float32.
const textFormFloat32BoundSchema = `package probe
 fixed table Probe {
 x float32 | min = -0.1, max = 0.1, resolution = 0.001
}
`

// textFormFloat32BoundProbe runs inside the generated unit.
const textFormFloat32BoundProbe = `package probe

import (
	"math"
	"testing"
)

func TestProbeFloat32Bound(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		text string
		ok   bool
		bits uint32
	}{
		{"min", "(:x -0.1)", true, 0xbdcccccd},
		{"max", "(:x 0.1)", true, 0x3dcccccd},
		// each sits between the bound's float64 spelling and its float32
		// rounding, so only a comparison at float32 reads it as the bound
		{"between-min", "(:x -0.1000000005)", true, 0xbdcccccd},
		{"between-max", "(:x 0.1000000005)", true, 0x3dcccccd},
		{"past-min", "(:x -0.10000001)", false, 0},
		{"past-max", "(:x 0.10000001)", false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, v, err := ProbeLoadTextForm([]byte(c.text), TableTextFormDefaultLimits())
			if !c.ok {
				if err == nil || v != nil {
					t.Fatalf("%s: %q was accepted, want text_bound", c.name, c.text)
				}
				return
			}
			if err != nil || v == nil {
				t.Fatalf("%s: %q: %v", c.name, c.text, err)
			}
			if got := math.Float32bits(v.X); got != c.bits {
				t.Fatalf("%s: %q landed %#08x, want %#08x", c.name, c.text, got, c.bits)
			}
		})
	}
}
`
