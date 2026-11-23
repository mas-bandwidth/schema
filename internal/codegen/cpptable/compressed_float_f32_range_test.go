package cpptable

import (
	"sort"
	"strings"
	"testing"
)

// TestCppCompressedFloatRangeHeldAtFloat32 pins A COMPRESSED FLOAT'S RANGE IS
// HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3) in the generated C++, without a
// compiler: the descriptor the JSON and text reads and the JSON write compare
// against carries the float32 nearest each declared end, spelled exactly as a
// double (under max = 0.1 that is 0.10000000149011612, not 0.1, which refused
// the 0.1f the C++ had just saved), the text form narrows a float32 token
// before its bound test, and the variable and fixed writers hold row 10 of the
// write contract against the float32 literals as a debug assert (docs/SPEC.md
// §5.1). The control is a plain float32 beside it: no range in its descriptor
// and no range test on its write. TestCppCompressedF32EndsCorpus runs the same
// rule through the compiled C++.
func TestCppCompressedFloatRangeHeldAtFloat32(t *testing.T) {
	t.Parallel()
	u := cppUnitFrom(t, `package probe

fixed table Cell
{
    c float32 | min = -0.3, max = 0.1, resolution = 0.01
    p float32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	src := all.String()
	descriptor := func(field string) string {
		t.Helper()
		for line := range strings.SplitSeq(src, "\n") {
			if strings.Contains(line, `{ "`+field+`", "`+field+`", "float32"`) {
				return line
			}
		}
		t.Fatalf("no descriptor row for %s", field)
		return ""
	}
	if c := descriptor("c"); !strings.Contains(c, "NULL, true, -0.30000001192092896, 0.10000000149011612,") {
		t.Errorf("the compressed float's descriptor does not carry its float32 ends:\n%s", c)
	}
	if p := descriptor("p"); !strings.Contains(p, "NULL, false, 0.0, 0.0,") {
		t.Errorf("the plain float's descriptor carries a range:\n%s", p)
	}
	// the variable Save's row 10, a debug assert
	if !strings.Contains(src, "schema_assert( !( value.c < -0.3f ) && !( value.c > 0.1f ) );") {
		t.Error("the variable Save does not hold the compressed float's float32 range")
	}
	// the fixed write check FixedSave and SaveMessages assert, on a finite
	// value (a NaN or an infinity is row 7's)
	if !strings.Contains(src, "bad |= (int) ( TableFloatFinite( value.c ) && ( value.c < -0.3f || value.c > 0.1f ) );") {
		t.Error("the fixed write check does not hold the compressed float's float32 range")
	}
	// the fixed read's in-range pass
	if !strings.Contains(src, "bad |= (int) ( value.c < -0.3f ) | (int) ( value.c > 0.1f );") {
		t.Error("the fixed read does not hold the compressed float's float32 range")
	}
	// the text form's bound test sees the value the JSON read lands
	if !strings.Contains(src, "bool single = f->kind == 10;\n        double value = TableJsonTokenDouble( token, length, single );") ||
		!strings.Contains(src, "if ( single ) { return (double) TableJsonStrtof( work ); }") {
		t.Error("the text form does not narrow a float32 token before its bound test")
	}
	if strings.Contains(src, "value.p < ") || strings.Contains(src, "value.p > ") {
		t.Error("the plain float has a range test")
	}
}
