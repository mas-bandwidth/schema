// The variant list's separators (SPEC §4.1, §4.2): a comma or a newline, a
// trailing separator allowed, and the newline earning its place at the one
// line kind that needs it, a qualified variant, whose section claims the
// rest of the line.
package parser

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
)

func errorTexts(src string) []string {
	_, errs := Parse("Bad.schema", []byte(src))
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

func names(t *testing.T, src string) []string {
	t.Helper()
	f, errs := Parse("Ok.schema", []byte(src))
	if len(errs) > 0 {
		t.Fatalf("%q: %v", src, errs)
	}
	return declVariantNames(f)
}

// Every separator spelling parses to the same list.
func TestVariantSeparatorsParseAlike(t *testing.T) {
	for _, src := range []string{
		"package ok\n\nenum Big { A, B, C }\n",
		"package ok\n\nenum Big\n{\n    A,\n    B,\n    C\n}\n",
		"package ok\n\nenum Big\n{\n    A,\n    B,\n    C,\n}\n",
		"package ok\n\nenum Big\n{\n    A\n    B\n    C\n}\n",
		"package ok\n\nenum Big\n{\n    A, B\n    C\n}\n",
		"package ok\n\nenum Big\n{\n    A\n    , B\n    , C\n}\n",
		"package ok\n\nenum Big\n{\n    A | x\n    B\n    C | y, z\n}\n",
		"package ok\n\nenum Big\n{\n    /// the first\n    A\n    B | x\n    /// the last\n    C\n}\n",
	} {
		got := names(t, src)
		if strings.Join(got, ",") != "A,B,C" {
			t.Errorf("%q: want A,B,C, got %v", src, got)
		}
	}
	if got := names(t, "package ok\n\nenum Empty { }\n"); len(got) != 0 {
		t.Errorf("empty list: got %v", got)
	}
}

// A qualified variant ends its own line: the pipe claims the rest of it, so
// a closing brace there is refused by name, and a comma at the head of the
// next line is a separator after a separator.
func TestQualifiedVariantEndsItsLine(t *testing.T) {
	for src, want := range map[string]string{
		"package bad\n\nenum E { Laser | beam }\n":                   "a qualified variant ends its own line",
		"package bad\n\nenum E\n{\n    Laser | beam\n    , Gun\n}\n": "expected enum variant name",
		"package bad\n\nenum E\n{\n    A B\n}\n":                     "separated by a comma or a newline",
	} {
		got := errorTexts(src)
		if len(got) == 0 {
			t.Errorf("%q: parsed clean, want %q", src, want)
			continue
		}
		if !strings.Contains(got[0], want) {
			t.Errorf("%q: want %q, got %s", src, want, got[0])
		}
	}
}

// A reserved word is not an identifier, so it cannot be a tag (SPEC §4.1).
func TestReservedWordIsNotATag(t *testing.T) {
	got := errorTexts("package bad\n\ntype T { x int32 | table }\n")
	if len(got) != 1 || !strings.Contains(got[0], "table is a reserved word and cannot be a tag") {
		t.Errorf("want the reserved-word refusal, got %v", got)
	}
}

// | is never an operator (SPEC §4.2).
func TestPipeIsNeverAnOperator(t *testing.T) {
	got := errorTexts("package bad\n\nconst X = 1 | 2\n")
	if len(got) != 1 || !strings.Contains(got[0], "| is never an operator") {
		t.Errorf("want the operator refusal, got %v", got)
	}
}

// checkErrTexts parses and checks one file, returning the checker's errors.
func checkErrTexts(src string) []string {
	f, perrs := Parse("Bad.schema", []byte(src))
	if len(perrs) > 0 {
		out := make([]string, 0, len(perrs))
		for _, e := range perrs {
			out = append(out, e.Error())
		}
		return out
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Bad.schema", Name: "Bad.schema", Base: "Bad", Bytes: []byte(src), AST: f,
	}})
	out := make([]string, 0, len(cerrs))
	for _, e := range cerrs {
		out = append(out, e.Error())
	}
	return out
}

// An enum or flags variant may be named a reserved word (SPEC §4.1): a variant
// name is read only inside its own body or after E., so it never meets a type
// position. A variant named None or Max is still refused by name, and a field
// named int is still refused.
func TestEnumVariantMayBeATypeWord(t *testing.T) {
	t.Parallel()
	if got := names(t, "package ok\n\nenum FieldType { text, int, enum, list, bool }\n"); strings.Join(got, ",") != "text,int,enum,list,bool" {
		t.Errorf("enum: want text,int,enum,list,bool, got %v", got)
	}
	if got := names(t, "package ok\n\nflags F { string, bytes }\n"); strings.Join(got, ",") != "string,bytes" {
		t.Errorf("flags: want string,bytes, got %v", got)
	}
	if got := errorTexts("package bad\n\ntype T { int int32 }\n"); len(got) == 0 || !strings.Contains(got[0], "expected") {
		t.Errorf("field named int: want a parse refusal, got %v", got)
	}
	for _, v := range []string{"None", "Max"} {
		if got := checkErrTexts("package bad\n\nenum E { " + v + " }\n"); len(got) == 0 || !strings.Contains(got[0], v+" is a compile error") {
			t.Errorf("variant %s: want the compile-error refusal, got %v", v, got)
		}
	}
}
