package gotable

import (
	"strings"
	"testing"
)

// A DEPRECATED SLOT CARRIES ITS DEFAULT IMAGE ON FORM 3, FOR EVERY SHAPE
// (docs/SPEC-TABLES.md §3.4, §22.6): a deprecated field copies its owner's
// default body, and a deprecated union arm of a table or type copies that
// type's own default body, so the arm's declared defaults ride and not zeros.
func TestFixedDeprecatedArmWritesItsDefaultImage(t *testing.T) {
	out := generate(t, deprecatedShapesSchema)
	var all strings.Builder
	for _, data := range out {
		all.Write(data)
	}
	src := all.String()
	for _, want := range []string{
		"var InnerFixedDefaultBody = func() []byte {",
		"copy(b[79:][1:], InnerFixedDefaultBody)",
		"ProbeFixedDefaultBody[4:16]",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the generated fixed writer lacks %q", want)
		}
	}
}

const deprecatedShapesSchema = `package dep

type Inner
{
    a int32 = 7
    s string(4) = "ab"
}

union U
{
    old Inner | deprecated
    level int32
}

fixed table Probe
{
    keep  int32
    gone  Inner | deprecated
    text  string(6) = "hi" | deprecated
    list  [..3]int32 | deprecated
    opt   ?Inner | deprecated
    arr   [2]Inner | deprecated
    u     U
}
`
