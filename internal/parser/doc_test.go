// The `///` doc comment (SPEC §4.1): its text rule, its binding, and every
// misplacement refused by name.
package parser

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
)

func declVariantNames(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.EnumDecl:
			for _, v := range d.Variants {
				out = append(out, v.Text)
			}
		case *ast.FlagsDecl:
			for _, v := range d.Variants {
				out = append(out, v.Text)
			}
		}
	}
	return out
}

// The text is the block verbatim with the marker removed: at most one
// leading space dropped, trailing whitespace dropped, lines joined by one
// newline, nothing interpreted.
func TestDocTextIsVerbatim(t *testing.T) {
	src := "package ok\n\n///  two leading spaces keep one\n///\n/// trailing whitespace goes   \t\n/// a \"quote\" and a \\ backslash and <angles> and */\ntype T { x int32 }\n"
	f, errs := Parse("Ok.schema", []byte(src))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	want := " two leading spaces keep one\n\ntrailing whitespace goes\na \"quote\" and a \\ backslash and <angles> and */"
	if got := f.Decls[0].(*ast.TypeDecl).Doc; got != want {
		t.Errorf("doc text:\n got %q\nwant %q", got, want)
	}
}

// The block binds to the declaration, field, variant or arm directly under
// it, and a plain // block binds to nothing.
func TestDocBindsToEveryDocumentableItem(t *testing.T) {
	src := `package ok

/// a constant
const X = 1

/// an enum
enum E
{
    /// a variant
    A
    B
}

/// a flags
flags F { A, B }

/// a type
type T
{
    // a working note, and it reaches nothing
    x int32
    /// a field
    y int32 | min = 0, max = 1
}

/// a table
table R
{
    /// a table field
    z int32
}

/// a union
union U
{
    /// an arm with a payload
    t T
    /// a payload-free arm
    none
}
`
	f, errs := Parse("Ok.schema", []byte(src))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	got := map[string]string{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.ConstDecl:
			got["const "+d.Name] = d.Doc
		case *ast.EnumDecl:
			got["enum "+d.Name] = d.Doc
			for _, v := range d.Variants {
				got["variant "+v.Text] = v.Doc
			}
		case *ast.FlagsDecl:
			got["flags "+d.Name] = d.Doc
		case *ast.TypeDecl:
			got["type "+d.Name] = d.Doc
			for _, it := range d.Body.Items {
				fl := it.(*ast.Field)
				got["field "+fl.Name] = fl.Doc
			}
		case *ast.TableDecl:
			got["table "+d.Name] = d.Doc
			for _, it := range d.Body.Items {
				fl := it.(*ast.Field)
				got["field "+fl.Name] = fl.Doc
			}
		case *ast.UnionDecl:
			got["union "+d.Name] = d.Doc
			for _, v := range d.Variants {
				got["arm "+v.Name] = v.Doc
				if v.Arm != nil {
					got["armfield "+v.Name] = v.Arm.Doc
				}
			}
		}
	}
	want := map[string]string{
		"const X": "a constant", "enum E": "an enum", "variant A": "a variant", "variant B": "",
		"flags F": "a flags", "type T": "a type", "field x": "", "field y": "a field",
		"table R": "a table", "field z": "a table field", "union U": "a union",
		"arm t": "an arm with a payload", "armfield t": "an arm with a payload", "arm none": "a payload-free arm",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %q, want %q", k, got[k], w)
		}
	}
}

// Every /// line is part of a doc comment or is refused by name.
func TestDocMisplacementsAreRefusedByName(t *testing.T) {
	for src, want := range map[string]string{
		"/// above package\npackage bad\n":                                                     "a /// block above package reaches no descriptor",
		"package bad\ntype T\n{\n    /// above a const item\n    const(1, 8)\n}\n":             "above a const( ) item",
		"package bad\ntype T\n{\n    /// above reserved\n    reserved(8)\n}\n":                 "above a reserved( ) item",
		"package bad\ntype T\n{\n    /// above align\n    align\n}\n":                          "above an align item",
		"package bad\ntype T\n{\n    a bool\n    /// above if\n    if a { x int32 }\n}\n":      "above an if branch",
		"package bad\ntype T\n{\n    x int32\n    /// above a closing brace\n}\n":              "above a closing brace",
		"package bad\nenum E\n{\n    A\n    /// above a closing brace\n}\n":                    "above a closing brace",
		"package bad\nunion U\n{\n    a bool\n    /// above a closing brace\n}\n":              "above a closing brace",
		"package bad\n/// at the end of the file\n":                                            "has nothing under it",
		"package bad\n/// at the end of the file, no newline":                                  "has nothing under it",
		"package bad\n/// held off by a blank line\n\ntype T { x int32 }\n":                    "separated from it by a blank line",
		"package bad\n/// held off by a comment\n// a note\ntype T { x int32 }\n":              "separated from it by a comment line",
		"package bad\n/// held off by a comment\n/* a note */\ntype T { x int32 }\n":           "separated from it by a comment line",
		"package bad\ntype T { x int32 } /// trailing\n":                                       "trails code on the item's own line",
		"package bad\ntype T\n{\n    x int32 | min = 0, max = 1 /// past a qualification\n}\n": "trails code on the item's own line",
	} {
		got := errorTexts(src)
		if len(got) == 0 {
			t.Errorf("%q: parsed clean, want %q", src, want)
			continue
		}
		found := false
		for _, g := range got {
			if strings.Contains(g, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want %q, got %v", src, want, got)
		}
	}
}

// A /* */ block is never a doc comment, and a //// divider is a /// line.
func TestBlockCommentIsNeverADoc(t *testing.T) {
	f, errs := Parse("Ok.schema", []byte("package ok\n/* not a doc */\ntype T { x int32 }\n"))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if d := f.Decls[0].(*ast.TypeDecl).Doc; d != "" {
		t.Errorf("a block comment became a doc: %q", d)
	}
}
