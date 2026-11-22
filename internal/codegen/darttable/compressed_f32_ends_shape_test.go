package darttable

import (
	"maps"
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

// TestDartCompressedF32EndsShape pins, in the generated source and with no
// toolchain, that A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32
// (docs/SPEC-TABLES.md §3.3) on the variable read of a leaf and of every array
// element, on the JSON read and on the JSON writer's check, each against the
// float32 nearest each end (-0.30000001192092896, 0.10000000149011612), and
// that a plain float32 has no range test. Before, the variable read held the
// leaf to the doubles -0.3 and 0.1 (refusing the 0.1f its own writer saves),
// and neither the element read nor the JSON read held a range at all.
// TestDartCompressedF32EndsCorpus runs the cross-port corpus under the VM.
func TestDartCompressedF32EndsShape(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, f32EndsShapeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	wire := string(files["ProbeWire.dart"])
	if wire == "" {
		t.Fatal("the generator wrote no ProbeWire.dart")
	}
	const lo, hi = "-0.30000001192092896", "0.10000000149011612"
	for _, want := range []string{
		// the variable read: a leaf, and each element of the array
		"if (d < " + lo + ") { r.report.refuse('out_of_range'); return; }",
		"if (d > " + hi + ") { r.report.refuse('out_of_range'); return; }",
		"{ final cf = p.readF32(p.pos, 'V.xs'); p.pos += 4; if (cf < " + lo + " || cf > " + hi + ") p.report.refuse('out_of_range'); out.add(cf == 0 ? 0.0 : cf); }",
		// the JSON read: a leaf, and each element of the array
		"v.r = _jsonRangedFloat(_jsonFloatField(val, r, true, v.r), " + lo + ", " + hi + ", r);",
		"(i1, p1) => _jsonRangedFloat(_jsonFloatField(i1, r, true, p1), " + lo + ", " + hi + ", r)",
		// the JSON writer's check
		"if (!(v.r >= " + lo + " && v.r <= " + hi + ")) {",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("ProbeWire.dart lacks %s", want)
		}
	}
	// the control: a plain float32 is read and written with no range test
	if !strings.Contains(wire, "v.p = _jsonFloatField(val, r, true, v.p);") {
		t.Error("ProbeWire.dart's JSON read of the plain float is not the bare read")
	}
	for _, never := range []string{
		"_jsonFloatField(val, r, true, v.p),",
		"v.p >=",
		// the doubles a hair inside the float32 ends
		"d < -0.3)", "d > 0.1)", ">= -0.3 &&", "<= 0.1)",
	} {
		if strings.Contains(wire, never) {
			t.Errorf("ProbeWire.dart holds %q", never)
		}
	}
}
