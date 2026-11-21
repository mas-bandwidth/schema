// Scanner regressions from the 2026-11-21 review: a doc line refuses U+0085
// (NEL, a C# line end) beside CR, LS and PS; a trailing backslash is found
// past a form feed or vertical tab (GCC and Clang still splice the line); doc
// text and string literals are UTF-8 (SPEC §4.1); and one number's spelling
// is bounded, since a multi-megabyte digit run costs quadratic work.
package scanner

import (
	"strings"
	"testing"
)

func TestDocLineRefusesNextLine(t *testing.T) {
	t.Parallel()
	src := "package ok\n/// a\u0085public const int X = 1;\ntype T { x int32 }\n"
	if !hasSubstrs(errorTexts(src), "U+0085") {
		t.Fatal("a NEL inside a /// doc line was not refused")
	}
}

func TestDocLineRefusesBackslashBeforeFormFeedOrVerticalTab(t *testing.T) {
	t.Parallel()
	for _, ws := range []string{"\f", "\v", " \f\t"} {
		src := "package ok\n/// C:\\" + ws + "\ntype T { x int32 }\n"
		if !hasSubstrs(errorTexts(src), "backslash") {
			t.Errorf("a backslash before %q at the end of a doc line was not refused", ws)
		}
	}
}

func TestDocLineAndStringMustBeUTF8(t *testing.T) {
	t.Parallel()
	if !hasSubstrs(errorTexts("package ok\n/// caf\xe9\ntype T { x int32 }\n"), "UTF-8") {
		t.Error("a doc line that is not UTF-8 was not refused")
	}
	if !hasSubstrs(errorTexts("package ok\ntable T | cpp_include = \"caf\xe9.h\"\n{\n    x int32\n}\n"), "UTF-8") {
		t.Error("a string literal that is not UTF-8 was not refused")
	}
	if errs := errorTexts("package ok\n/// café \u00e9\ntype T { x int32 }\n"); len(errs) > 0 {
		t.Errorf("a UTF-8 doc line was refused: %v", errs)
	}
}

func TestNumberSpellingIsBounded(t *testing.T) {
	t.Parallel()
	long := "package ok\nconst X = 1" + strings.Repeat("0", maxNumberLength) + "\n"
	if !hasSubstrs(errorTexts(long), "longer than") {
		t.Error("a number past the length bound was not refused")
	}
	bin := "package ok\nconst X = 0b" + strings.Repeat("1", maxNumberLength) + "\n"
	if !hasSubstrs(errorTexts(bin), "longer than") {
		t.Error("a binary number past the length bound was not refused")
	}
	ok := "package ok\nconst X = 340282366920938463463374607431768211455\n"
	if errs := errorTexts(ok); len(errs) > 0 {
		t.Errorf("uint128's largest value was refused: %v", errs)
	}
}
