package gotable

import (
	"regexp"
	"strings"
	"testing"
)

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3), on
// the generated Go's own text and no toolchain: every range test compares a
// float32 operand with an untyped end, which Go converts to the float32
// nearest it, and the JSON runtime narrows the declared float64 ends with
// float32(...). A float64 operand there would refuse 0.1f under max = 0.1.
// The control is a plain float32 beside it, which carries no range test.
func TestCompressedF32EndsGoForm(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, `package f32ends
table V
{
    r  float32 | min = -0.3, max = 0.1, resolution = 0.01
    xs [2]float32 | min = -0.3, max = 0.1, resolution = 0.01
    p  float32
}
fixed table F
{
    r  float32 | min = -0.3, max = 0.1, resolution = 0.01
    xs [2]float32 | min = -0.3, max = 0.1, resolution = 0.01
    p  float32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, b := range files {
		all.Write(b)
	}
	src := all.String()
	for _, pin := range []struct{ what, re string }{
		{"the value is a float32", `\bR\s+float32\n`},
		{"the array element is a float32", `\bXs\s+\[2\]float32\n`},
		{"the variable writer tests the float32 leaf", `value\.R != value\.R \|\| value\.R < -0\.3 \|\| value\.R > 0\.1`},
		{"the variable writer tests the float32 element", `value\.Xs\[i\] != value\.Xs\[i\] \|\| value\.Xs\[i\] < -0\.3 \|\| value\.Xs\[i\] > 0\.1`},
		{"the variable read narrows the bits to float32 before the test", `v := math\.Float32frombits\(b\)\s+if v < -0\.3 \|\| v > 0\.1`},
		{"the fixed read tests the float32 leaf", `if value\.R < -0\.3 \|\| value\.R > 0\.1 \{\s+\(\*over\)\+\+`},
		{"the JSON writer narrows the ends", `value < float32\(f\.RangeMin\) \|\| value > float32\(f\.RangeMax\)`},
		{"the JSON read narrows the value and the low end", `float32\(value\) < float32\(f\.RangeMin\)`},
		{"the JSON read narrows the value and the high end", `float32\(value\) > float32\(f\.RangeMax\)`},
	} {
		if !regexp.MustCompile(pin.re).MatchString(src) {
			t.Errorf("%s: no match for %s", pin.what, pin.re)
		}
	}
	for _, bad := range []string{`float64(value.R)`, `float64(value.Xs[i])`, `float64(v) <`, `value.P <`, `value.P >`} {
		if strings.Contains(src, bad) {
			t.Errorf("the generated Go holds %q", bad)
		}
	}
}
