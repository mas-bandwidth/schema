package javatable

import (
	"strings"
	"testing"
)

// f32EndsShapeSchema is one compressed float32 whose ends float32 cannot spell
// (max = 0.1, min = -0.3) beside a plain float32, on a fixed root and a
// variable one.
const f32EndsShapeSchema = `package probe

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
`

// TestJavaCompressedF32EndsShape pins, in the generated source and with no
// toolchain, that A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32
// (docs/SPEC-TABLES.md §3.3) on every Java path: the variable read and write,
// the fixed read's scatter and the fixed writer's debug contract walk, each
// against the float32 nearest each end (bits be99999a and 3dcccccd), and that a
// plain float32 has no range test. TestJavaCompressedF32EndsCorpus runs the
// cross-port corpus under the JDK.
func TestJavaCompressedF32EndsShape(t *testing.T) {
	t.Parallel()
	files := generate(t, f32EndsShapeSchema)
	squeeze := func(s string) string { return strings.Join(strings.Fields(s), "") }
	var all strings.Builder
	for _, name := range sortedNames(files) {
		all.Write(files[name])
	}
	src := squeeze(all.String())
	lo, hi := fixedFloatLit(-0.3), fixedFloatLit(0.1)
	for _, want := range []string{
		// the variable read and write
		`TableVariable.compressedF32(java.lang.Float.intBitsToFloat(fbits), ` + lo + `, ` + hi,
		`putF32In(v.r, ` + lo + `, ` + hi + `)`,
		// the fixed read
		`if (q < ` + lo + `) {`,
		`} else if (q > ` + hi + `) {`,
		// the fixed writer's contract walk, a leaf and every array slot
		`assert !java.lang.Float.isFinite(v.r) || (v.r >= ` + lo + ` && v.r <= ` + hi + `);`,
		`assert !java.lang.Float.isFinite(v.xs[i]) || (v.xs[i] >= ` + lo + ` && v.xs[i] <= ` + hi + `);`,
	} {
		if !strings.Contains(src, squeeze(want)) {
			t.Errorf("the generated Java lacks %s", want)
		}
	}
	// the control: a plain float32 is held to no range, on either form
	for _, never := range []string{`v.p >=`, `v.p <=`, `putF32In(v.p`, `compressedF32(v.p`} {
		if strings.Contains(src, squeeze(never)) {
			t.Errorf("the generated Java holds the plain float to a range: %s", never)
		}
	}
}
