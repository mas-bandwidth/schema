package golang

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"
)

// shapesUnit carries every construct the emitter writes a scoped fragment
// for: flat runs, ranged fields on both sides of zero and at the int32
// minimum, enums, flags, a union, strings, counted arrays, branches, const and
// reserved items, compressed floats and the bound arithmetic between them.
const shapesUnit = `package t

const Lo = 100
const Span = 1000

enum Kind { A, B, C }
flags Bits { X, Y }

type Arm { v uint8 }
type Other { w uint16 }

union Pick
{
    arm Arm
    other Other
}

type Shapes
{
    object_id int32 | min = 0, max = 9999
    target_id uint16
    http_url_count uint8
    low int32 | min = -Lo, max = Lo
    lowest int32 | min = -2147483648, max = 0
    narrow int16 | min = -Lo, max = Span
    wide int64 | min = -5000000000, max = 5000000000
    kind Kind
    mask Bits
    angle float32 | min = -180.0, max = 180.0, resolution = 0.01
    pick Pick
    name string(16)
    items [..4]uint8 | min = 0, max = 200
    more [..4]int8
    const(5, 3)
    reserved(2)
    has bool
    if has
    {
        extra int32 | min = -Lo, max = Lo
        other int32 | min = -Lo, max = Lo
    }
}
`

// TestTidyLeavesNoBareBlocks: every function body is a flat statement list;
// the only blocks are the bodies of if, for, switch and case.
func TestTidyLeavesNoBareBlocks(t *testing.T) {
	files := generateGo(t, "Shapes", shapesUnit)
	for name, src := range files {
		fset := token.NewFileSet()
		f, err := goparser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var list []ast.Stmt
			switch n := n.(type) {
			case *ast.BlockStmt:
				list = n.List
			case *ast.CaseClause:
				list = n.Body
			}
			for _, s := range list {
				if _, bare := s.(*ast.BlockStmt); bare {
					t.Errorf("%s:%s: a bare block survived the tidy pass", name, fset.Position(s.Pos()))
				}
			}
			return true
		})
	}
}

// TestTidyDropsTheNoise: no bound local, no parenthesized operand or bound,
// no double conversion, one spec citation per file.
func TestTidyDropsTheNoise(t *testing.T) {
	files := generateGo(t, "Shapes", shapesUnit)
	text := string(files["Shapes.go"])
	for _, gone := range []string{
		"lowValue",                     // the bound folds into the decode
		"- (Lo)", "- (-Lo)", "+ (-Lo)", // a bound is an operand, not a parenthesized one
		":= (uint64(",               // a parenthesized conversion
		"uint64(uint32(value.",      // a widening hop the mask already covers
		"PickType(int32(",           // a tag read converts once
		"Kind(int32(",               // an enum read converts once
		"(SPEC §", "— SPEC §", "(§", // the citations moved to the header
	} {
		if strings.Contains(text, gone) {
			t.Errorf("generated Go still carries %q", gone)
		}
	}
	if n := strings.Count(text, "SPEC.md §"); n != 1 {
		t.Errorf("generated Go carries %d spec citations, want the header's one", n)
	}
	for _, want := range []string{
		"value.Low = int32(v",                // the decode adds the bound in place
		"- Lo",                               // a negative bound subtracts its magnitude
		"(-2147483648)",                      // except at the type minimum, whose magnitude overflows
		"value.Kind = Kind(v",                // one conversion
		"value.Type = PickType(offsetValue)", // one conversion
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated Go does not carry %q", want)
		}
	}
}

// TestGoInitialismsOnMembers: members take Go's initialisms; top-level names
// keep the spelling every port shares.
func TestGoInitialismsOnMembers(t *testing.T) {
	files := generateGo(t, "Shapes", shapesUnit)
	text := string(files["Shapes.go"])
	for _, want := range []string{"ObjectID ", "TargetID ", "HTTPURLCount ", "value.ObjectID"} {
		if !strings.Contains(text, want) {
			t.Errorf("generated Go does not carry %q", want)
		}
	}
	for _, gone := range []string{"ObjectId", "TargetId", "HttpUrl"} {
		if strings.Contains(text, gone) {
			t.Errorf("generated Go still spells %q", gone)
		}
	}
}

// TestGodocOnEveryExport: every exported top-level identifier has a doc
// comment, and a lone declaration's begins with its own name.
func TestGodocOnEveryExport(t *testing.T) {
	files := generateGo(t, "Shapes", shapesUnit)
	for name, src := range files {
		fset := token.NewFileSet()
		f, err := goparser.ParseFile(fset, name, src, goparser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() && !strings.HasPrefix(d.Doc.Text(), d.Name.Name+" ") {
					t.Errorf("%s: func %s has no godoc beginning with its name: %q", name, d.Name.Name, d.Doc.Text())
				}
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				if d.Doc == nil {
					t.Errorf("%s: %s declaration at %s has no doc comment", name, d.Tok, fset.Position(d.Pos()))
					continue
				}
				if len(d.Specs) == 1 && !d.Lparen.IsValid() {
					var ident string
					switch s := d.Specs[0].(type) {
					case *ast.TypeSpec:
						ident = s.Name.Name
					case *ast.ValueSpec:
						ident = s.Names[0].Name
					}
					if !strings.HasPrefix(d.Doc.Text(), ident+" ") {
						t.Errorf("%s: %s's godoc does not begin with its name: %q", name, ident, d.Doc.Text())
					}
				}
			}
		}
	}
}

// TestEnumAndFlagsNameMethods: an enum and a union tag carry String, which
// EnumName<E> returns; a flags set's String is FlagNames<F>, which takes the
// flags type itself.
func TestEnumAndFlagsNameMethods(t *testing.T) {
	files := collapseColumns(generateGo(t, "Shapes", shapesUnit))
	text := string(files["Shapes.go"])
	for _, want := range []string{
		"func (v Kind) String() string {",
		"func EnumNameKind(value Kind) string {\nreturn value.String()\n}",
		"func (v PickType) String() string {",
		"func FlagNamesBits(value Bits) string {",
		"func (v Bits) String() string {\nreturn FlagNamesBits(v)\n}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated Go does not carry %q", want)
		}
	}
}

// TestHoistRenamesOnlyWhatCollides: a block local keeps its name unless the
// function already spells it, and then takes the next free one in its family.
func TestHoistRenamesOnlyWhatCollides(t *testing.T) {
	src := []byte(`package p

func f() int {
	{
		x := 1
		_ = x
	}
	{
		x := 2
		_ = x
	}
	{
		f0 := 3
		_ = f0
	}
	f0 := 4
	return f0
}
`)
	out, err := tidy(src)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"\tx := 1\n", "\tx2 := 2\n", "\tf1 := 3\n", "\tf0 := 4\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("tidy output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n\n\tx") || strings.Contains(got, "{\n\n") {
		t.Errorf("tidy left a blank line where a brace stood:\n%s", got)
	}
}
