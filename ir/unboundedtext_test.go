package ir_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUnboundedTextProjectsItsOwnLine holds docs/SPEC-TABLES.md §2.13.1 and
// §20.2: an unbounded element's value and an unbounded key render
// `text=unbounded` with no `bound=` over the eight-byte slot, so respelling
// string(M) as string moves the build version as moving M does.
func TestUnboundedTextProjectsItsOwnLine(t *testing.T) {
	t.Parallel()
	const src = "package t\ntable Where {\n    rules []string\n    byname map[string]int32\n}\n"
	text := ir.CookProjection(unitFrom(t, src))
	for _, want := range []string{"size=8 text=unbounded"} {
		if strings.Count(text, want) != 2 {
			t.Errorf("the projection does not carry %q on the value and the key:\n%s", want, text)
		}
	}
	if strings.Contains(text, "text=unbounded bound=") {
		t.Errorf("unbounded text projects a bound=:\n%s", text)
	}
	unbounded := ir.BuildVersion(unitFrom(t, src))
	for _, respelled := range []string{
		strings.Replace(src, "[]string", "[]string(8)", 1),
		strings.Replace(src, "map[string]", "map[string(8)]", 1),
	} {
		if ir.BuildVersion(unitFrom(t, respelled)) == unbounded {
			t.Errorf("respelling string as string(8) did not move the build version:\n%s", respelled)
		}
	}
}
