// Nesting depth is bounded (SPEC §4.2, §4.5): parentheses, unary minus and
// blocks all nest, and the parser refuses a form deeper than it accepts with a
// positioned diagnostic.
package parser

import (
	"strings"
	"testing"
)

func nestedBlockSource(depth int) string {
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

func TestNestingDepthLimit(t *testing.T) {
	t.Parallel()

	t.Run("parentheses", func(t *testing.T) {
		t.Parallel()
		src := "package t\nconst A = " + strings.Repeat("(", 100) + "1" + strings.Repeat(")", 100) + "\n"
		_, errs := Parse("T.schema", []byte(src))
		if len(errs) == 0 {
			t.Fatal("deeply nested parentheses were accepted")
		}
		got := errs[0].Error()
		if !strings.Contains(got, "nesting exceeds the 64-level limit") {
			t.Fatalf("want the nesting refusal, got %q", got)
		}
		if !strings.HasPrefix(got, "T.schema:2:") {
			t.Fatalf("want a positioned diagnostic, got %q", got)
		}
	})

	t.Run("unary minus", func(t *testing.T) {
		t.Parallel()
		src := "package t\nconst A = " + strings.Repeat("-", 100) + "1\n"
		_, errs := Parse("T.schema", []byte(src))
		if len(errs) == 0 {
			t.Fatal("deeply nested unary minus was accepted")
		}
		got := errs[0].Error()
		if !strings.Contains(got, "nesting exceeds the 64-level limit") {
			t.Fatalf("want the nesting refusal, got %q", got)
		}
		if !strings.HasPrefix(got, "T.schema:2:") {
			t.Fatalf("want a positioned diagnostic, got %q", got)
		}
	})

	t.Run("if blocks", func(t *testing.T) {
		t.Parallel()
		_, errs := Parse("T.schema", []byte(nestedBlockSource(300)))
		if len(errs) == 0 {
			t.Fatal("300 nested if blocks were accepted")
		}
		got := errs[0].Error()
		if !strings.Contains(got, "nesting exceeds the 64-level limit") {
			t.Fatalf("want the nesting refusal, got %q", got)
		}
		if !strings.HasPrefix(got, "T.schema:") {
			t.Fatalf("want a positioned diagnostic, got %q", got)
		}
	})

	t.Run("accepted below the limit", func(t *testing.T) {
		t.Parallel()
		src := "package t\nconst A = " + strings.Repeat("(", 8) + "1" + strings.Repeat(")", 8) + "\n"
		if _, errs := Parse("T.schema", []byte(src)); len(errs) > 0 {
			t.Fatalf("eight levels were refused: %v", errs)
		}
		if _, errs := Parse("T.schema", []byte(nestedBlockSource(8))); len(errs) > 0 {
			t.Fatalf("eight if blocks were refused: %v", errs)
		}
	})
}
