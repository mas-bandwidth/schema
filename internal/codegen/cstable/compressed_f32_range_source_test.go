package cstable

import (
	"slices"
	"strings"
	"testing"
)

// TestCompressedF32RangeHeldAtFloat32Source pins THE RANGE IS HELD AT FLOAT32
// (docs/SPEC-TABLES.md §3.3) in the generated C#, without a toolchain: every
// range test on a float32 field compares against the float32 nearest each end
// (the `f` literal -0.3f, 0.1f), on the fixed read, the fixed write check (row
// 10 of docs/SPEC.md §5.1, a debug assert), the variable and region writers'
// descriptor delegate and the JSON read; none compares against the declared
// decimal as a double. The control `p` is a plain float32 and has no range test
// anywhere. The runtime twin is TestCompressedF32EndsCorpus.
func TestCompressedF32RangeHeldAtFloat32Source(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, `package f32src

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
	body := func(sig string) string {
		t.Helper()
		at := strings.Index(src, sig)
		if at < 0 {
			t.Fatalf("the generated C# has no %q", sig)
		}
		// The body ends at the closing brace at the signature's own indent.
		line := strings.LastIndex(src[:at], "\n") + 1
		end := strings.Index(src[at:], "\n"+src[line:at]+"}\n")
		if end < 0 {
			t.Fatalf("%q has no end", sig)
		}
		return src[at : at+end]
	}
	const held = "value.R < -0.3f || value.R > 0.1f"
	read := body("public static void FFixedClampBody(F value, ref int clamped)")
	write := body("public static void FFixedWriteCheckBody(F value, ref int clamped)")
	if !strings.Contains(read, held) {
		t.Errorf("the fixed read does not hold r at float32 (%q):\n%s", held, read)
	}
	if !strings.Contains(write, "if ("+held+") { clamped++; }") {
		t.Errorf("the fixed write check does not hold r at float32 (%q), row 10 of the write contract:\n%s", held, write)
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
	for _, double := range []string{"< -0.3)", "< -0.3 ", "> 0.1)", "> 0.1 ", ">= -0.3 ", "<= 0.1)", "<= 0.1 "} {
		if strings.Contains(src, double) {
			t.Errorf("a range test compares against the declared decimal as a double: %q", double)
		}
	}
	// THE CONTROL: a plain float has no range test, on either side.
	for _, side := range []string{read, write} {
		if strings.Contains(side, "value.P <") || strings.Contains(side, "value.P >") {
			t.Errorf("the plain float p has a range test:\n%s", side)
		}
	}
	if p := descriptor("p"); strings.Contains(p, "RequiredValidRaw") || strings.Contains(p, "v < ") || strings.Contains(p, "v > ") {
		t.Errorf("the plain float p's descriptor carries a range delegate:\n%s", p)
	}
}
