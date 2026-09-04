// The two body grammars (SPEC §4.2): an `enum` or `flags` body is
// COMMA-separated variant names, a `type` or `table` body is line-separated
// fields. Writing the first the way the second is written is the most likely
// early mistake in the language, and this pins what the parser says about it.
package parser

import (
	"strings"
	"testing"
)

func errorTexts(src string) []string {
	_, errs := Parse("Bad.schema", []byte(src))
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

// One mistake, one diagnostic, and it names the rule. Reverting the
// missing-comma arm of parseVariantList turns this red twice over: the
// diagnostic becomes `expected }, found "newline"`, which does not name
// comma separation, and a second error follows placing the next variant at
// file scope.
func TestLineSeparatedVariantsNameTheRuleAndRecover(t *testing.T) {
	for _, what := range []string{"enum", "flags"} {
		src := "package bad\n\n" + what + " Big\n{\n    A\n    B\n    C\n}\n"
		got := errorTexts(src)
		if len(got) != 1 {
			t.Errorf("%s: want 1 diagnostic, got %d: %v", what, len(got), got)
			continue
		}
		for _, want := range []string{"COMMA-separated", "needs a comma after it", "SPEC §4.2"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("%s: the diagnostic does not carry %q: %s", what, want, got[0])
			}
		}
		if strings.Contains(got[0], "file scope") {
			t.Errorf("%s: the diagnostic still sends the reader to file scope: %s", what, got[0])
		}
	}
}

// The recovery reads the WHOLE body, so the declaration that follows is parsed
// as a declaration and nothing is reported at file scope.
func TestLineSeparatedVariantsStillYieldTheirNames(t *testing.T) {
	f, _ := Parse("Bad.schema", []byte("package bad\n\nenum Big\n{\n    A\n    B\n    C\n}\n\ntype T { b Big }\n"))
	if f == nil {
		t.Fatal("no file returned")
	}
	if len(f.Decls) != 2 {
		t.Fatalf("want the enum and the type, got %d declarations", len(f.Decls))
	}
}

// The correct spelling stays silent, both on one line and one variant per line
// with commas.
func TestCommaSeparatedVariantsParseClean(t *testing.T) {
	for _, src := range []string{
		"package ok\n\nenum Big { A, B, C }\n",
		"package ok\n\nenum Big\n{\n    A,\n    B,\n    C\n}\n",
		"package ok\n\nenum Big\n{\n    A,\n    B,\n    C,\n}\n",
		"package ok\n\nenum Empty { }\n",
	} {
		if got := errorTexts(src); len(got) != 0 {
			t.Errorf("%q: want no diagnostic, got %v", src, got)
		}
	}
}
