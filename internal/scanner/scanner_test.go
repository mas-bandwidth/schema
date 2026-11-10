// The `///` doc comment's text rule (SPEC §4.1): a line runs to the next \n,
// and a carriage return or a Unicode line/paragraph separator inside the line
// is refused by name because some target compilers treat it as a line end.
package scanner

import (
	"strings"
	"testing"
)

func errorTexts(src string) []string {
	_, errs := Scan("Scanner.schema", []byte(src))
	var out []string
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

func hasSubstrs(got []string, want string) bool {
	for _, g := range got {
		if strings.Contains(g, want) {
			return true
		}
	}
	return false
}

func TestDocLineRefusesBareCarriageReturn(t *testing.T) {
	t.Parallel()
	src := "package ok\n/// a\rint x;\ntype T { x int32 }\n"
	if !hasSubstrs(errorTexts(src), "carriage return") {
		t.Fatal("a bare CR inside a /// doc line was not refused")
	}
}

func TestDocLineRefusesUnicodeLineSeparator(t *testing.T) {
	t.Parallel()
	src := "package ok\n/// a\u2028int x;\ntype T { x int32 }\n"
	if !hasSubstrs(errorTexts(src), "line separator") {
		t.Fatal("a U+2028 inside a /// doc line was not refused")
	}
}

func TestDocLineRefusesUnicodeParagraphSeparator(t *testing.T) {
	t.Parallel()
	src := "package ok\n/// a\u2029int x;\ntype T { x int32 }\n"
	if !hasSubstrs(errorTexts(src), "paragraph separator") {
		t.Fatal("a U+2029 inside a /// doc line was not refused")
	}
}

func TestDocLineScansCRLFFileAsBefore(t *testing.T) {
	t.Parallel()
	src := "package ok\r\n/// a doc line\r\n/// second line\r\ntype T { x int32 }\r\n"
	if errs := errorTexts(src); len(errs) > 0 {
		t.Fatalf("a CRLF schema was refused: %v", errs)
	}
	toks, _ := Scan("Crlf.schema", []byte(src))
	var doc string
	for _, t := range toks {
		if t.Kind == Doc {
			doc = t.Text
		}
	}
	if want := "a doc line\nsecond line"; doc != want {
		t.Errorf("doc text:\n got %q\nwant %q", doc, want)
	}
}
