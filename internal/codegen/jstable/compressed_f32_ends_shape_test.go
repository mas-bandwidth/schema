package jstable

import (
	"strings"
	"testing"
)

// f32EndsShapeSchema is one compressed float32 whose ends float32 cannot spell
// (max = 0.1, min = -0.3) beside a plain float32, on a variable root and a
// fixed one.
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

// TestJavaScriptCompressedF32EndsShape pins, in the generated source and with
// no toolchain, that A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32
// (docs/SPEC-TABLES.md §3.3) on the JavaScript leg. The variable runtime holds
// every range through the descriptor's lo and hi — the read, the writer's
// valid test, the required test and the text read — so the descriptor carries
// the float32 nearest each end (-0.30000001192092896, 0.10000000149011612),
// where it carried the doubles -0.3 and 0.1 and refused 0.1f, its own end, on
// the read and the write. The text read narrows before its range test, the
// required test narrows a float32, and the fixed writer's preflight holds the
// narrowed value. A plain float32 carries no range. The cross-port corpus runs
// under node in TestJavaScriptCompressedF32EndsCorpus.
func TestJavaScriptCompressedF32EndsShape(t *testing.T) {
	t.Parallel()
	files := generatedTree(unitFrom(t, f32EndsShapeSchema))
	variable, fixed := string(files["ProbeVariable.js"]), string(files["ProbeTable.js"])
	if variable == "" || fixed == "" {
		t.Fatal("the generator wrote no ProbeVariable.js or ProbeTable.js")
	}
	const lo, hi = "-0.30000001192092896", "0.10000000149011612"
	ends := "f32: true, lo: " + lo + ", hi: " + hi + ", nf: "
	for _, want := range []string{
		ends + `"V.r"`, ends + `"V.xs"`, ends + `"F.r"`, ends + `"F.xs"`,
		// the control: a plain float32 has no range
		`f32: true, lo: null, hi: null, nf: "V.p"`,
		// the required test narrows a float32 first
		`const x = d.s === "float" && d.f32 ? globalThis.Math.fround(value) : value;`,
	} {
		if !strings.Contains(variable, want) {
			t.Errorf("ProbeVariable.js lacks %s", want)
		}
	}
	for _, never := range []string{"lo: -0.3,", "hi: 0.1,"} {
		if strings.Contains(variable, never) {
			t.Errorf("ProbeVariable.js carries a double end: %s", never)
		}
	}
	// the text read narrows to the field's width BEFORE the range test
	_, after, narrowed := strings.Cut(variable, "const y = TableVariableF32OfToken(n.tok, x);")
	place := strings.Index(after, "holder[key] = x;")
	ranged := strings.Index(after, "if (x < d.lo) { x = d.lo; report.oor++; }")
	if !narrowed || place < 0 || ranged < 0 || ranged > place {
		t.Error("the text read does not narrow a float32 before its range test")
	}
	for _, want := range []string{
		"if (globalThis.Math.fround(value.r) < " + lo + " || globalThis.Math.fround(value.r) > " + hi + ") { return false; }",
		"if (globalThis.Math.fround(value.xs[i1]) < " + lo + " || globalThis.Math.fround(value.xs[i1]) > " + hi + ") { return false; }",
	} {
		if !strings.Contains(fixed, want) {
			t.Errorf("ProbeTable.js lacks %s", want)
		}
	}
	if strings.Contains(fixed, "globalThis.Math.fround(value.p) <") || strings.Contains(fixed, "value.p < ") {
		t.Error("ProbeTable.js holds the plain float to a range")
	}
}
