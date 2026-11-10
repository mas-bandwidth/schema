// Nesting depth is bounded (SPEC §4.5): a body deeper than the parser accepts
// is refused at its position, so the checker's branch walk never meets a form
// deeper than the bound.
package check

import (
	"strings"
	"testing"
)

func nestedBranchSource(depth int) string {
	var b strings.Builder
	b.WriteString("package t\ntype T\n{\n    on bool\n")
	for i := 0; i < depth; i++ {
		b.WriteString("    if on {\n")
	}
	b.WriteString("        x int32\n")
	for i := 0; i < depth; i++ {
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func TestDeeplyNestedBranchesAreRefused(t *testing.T) {
	t.Parallel()
	got := refuse(t, nestedBranchSource(300))
	if !strings.Contains(got, "nesting exceeds the 64-level limit") {
		t.Fatalf("want the nesting refusal, got %q", got)
	}
	if !strings.HasPrefix(got, "T.schema:") {
		t.Fatalf("want a positioned diagnostic, got %q", got)
	}
}
