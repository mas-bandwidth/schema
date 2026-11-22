package rusttable

import (
	"strings"
	"testing"
)

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3), on
// the generated Rust's own text and no toolchain: every write check of a
// compressed float32 is a debug assert against the float32 nearest each
// declared end, as bits (-0.3f is be99999a, 0.1f is 3dcccccd), for the leaf
// and for each element of a fixed array alike, in the variable and the fixed
// writers. The control is a plain float32 array beside it, which carries no
// range assert.
func TestCompressedF32EndsRustForm(t *testing.T) {
	t.Parallel()
	modules, err := Generate(unitFrom(t, `package f32ends
table V
{
    r  float32 | min = -0.3, max = 0.1, resolution = 0.01
    xs [2]float32 | min = -0.3, max = 0.1, resolution = 0.01
    ps [2]float32
}
fixed table F
{
    r  float32 | min = -0.3, max = 0.1, resolution = 0.01
    xs [2]float32 | min = -0.3, max = 0.1, resolution = 0.01
    ps [2]float32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	table, fixed := string(modules["probe_table.rs"]), string(modules["probe_fixed.rs"])
	if table == "" || fixed == "" {
		t.Fatalf("no probe_table.rs or probe_fixed.rs among %d modules", len(modules))
	}
	const ends = `>= f32::from_bits(0xbe99999a) && %s <= f32::from_bits(0x3dcccccd)), "%s out of range");`
	leaf := `debug_assert!(!v.r.is_finite() || (v.r ` + strings.Replace(strings.Replace(ends, "%s", "v.r", 1), "%s", "r", 1)
	elem := `debug_assert!(!(*e).is_finite() || ((*e) ` + strings.Replace(strings.Replace(ends, "%s", "(*e)", 1), "%s", "xs", 1)
	row := `debug_assert!(!value.xs[i].is_finite() || (value.xs[i] ` + strings.Replace(strings.Replace(ends, "%s", "value.xs[i]", 1), "%s", "xs", 1)
	for _, pin := range []struct {
		what, src, text string
		want            int
	}{
		{"the variable writers' leaf (V and F save bodies)", table, leaf, 2},
		{"the variable writers' array element (V and F save bodies)", table, elem, 2},
		{"the fixed writer's array element", fixed, row, 1},
	} {
		if got := strings.Count(pin.src, pin.text); got != pin.want {
			t.Errorf("%s: %d of %s, want %d", pin.what, got, pin.text, pin.want)
		}
	}
	if strings.Contains(table+fixed, `"ps out of range"`) {
		t.Error("a plain float32 array carries a range assert")
	}
	if strings.Contains(table+fixed, "f64::from_bits(") {
		t.Error("a float32 range is held at float64")
	}
}
