package unitytable_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

// TestUnityCompressedF32RangeHeldAtFloat32Source pins THE RANGE IS HELD AT
// FLOAT32 (docs/SPEC-TABLES.md §3.3) in the generated C# (unity), without a
// toolchain: every range test on a float32 field compares against the float32
// nearest each end (the `f` literal -0.3f, 0.1f), on the fixed writer's assert
// (row 10 of docs/SPEC.md §5.1), the descriptor's write and read delegates and
// the JSON read; none compares against the declared decimal as a double. The
// control `p` is a plain float32 and has no range test anywhere. The runtime
// twin is TestUnityCompressedF32EndsCorpus.
func TestUnityCompressedF32RangeHeldAtFloat32Source(t *testing.T) {
	t.Parallel()
	files, err := unitytable.Generate(fixedproof.ParseCheckUnit(t, `package f32src

fixed table F {
    r float32 | min = -0.3, max = 0.1, resolution = 0.01
    p float32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	src := all.String()
	if !strings.Contains(src, "Debug.Assert(value.R >= -0.3f && value.R <= 0.1f,") {
		t.Error("the fixed writer does not assert r at float32 (row 10 of the write contract)")
	}
	descriptor := func(name string) string {
		t.Helper()
		for line := range strings.SplitSeq(src, "\n") {
			if strings.Contains(line, "new TableFieldInfo { Name = \""+name+"\"") {
				return line
			}
		}
		t.Fatalf("no descriptor for %s", name)
		return ""
	}
	if r := descriptor("r"); !strings.Contains(r, "return v >= -0.3f && v <= 0.1f;") || !strings.Contains(r, "if (v < -0.3f || v > 0.1f)") {
		t.Errorf("r's descriptor delegates do not hold it at float32:\n%s", r)
	}
	if !strings.Contains(src, "if (single ? (float)value < (float)f.RangeMin || (float)value > (float)f.RangeMax :") {
		t.Error("the JSON read does not narrow a float32's value and ends to float32 before the range test")
	}
	for _, double := range []string{"< -0.3)", "< -0.3 ", "> 0.1)", "> 0.1 ", ">= -0.3 ", "<= 0.1)", "<= 0.1 ", "<= 0.1,"} {
		if strings.Contains(src, double) {
			t.Errorf("a range test compares against the declared decimal as a double: %q", double)
		}
	}
	// THE CONTROL: a plain float has no range test, on either side.
	if strings.Contains(src, "value.P >=") || strings.Contains(src, "value.P <") {
		t.Error("the plain float p has a range test")
	}
	if p := descriptor("p"); strings.Contains(p, "v < ") || strings.Contains(p, "v >= ") {
		t.Errorf("the plain float p's descriptor carries a range test:\n%s", p)
	}
}
