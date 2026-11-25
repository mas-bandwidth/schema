package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHostileExpectationsCarryNoExemption: hostileExpectations returns the
// expectations alone, so no caller can receive, print or honour an
// exemption beside them (Glenn, 2026-11-25: no skips and no exemption list
// of any kind). A second result, of any type, is the slot an exemption came
// back in, and is red by name; so is any line of the harness that prints
// "EXEMPTS", the word a run printed an exemption under.
func TestHostileExpectationsCarryNoExemption(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "EXEMPTS") {
			t.Errorf("%s prints an exemption (\"EXEMPTS\"): there is none to print", name)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "hostileExpectations" {
				continue
			}
			found = true
			res := fn.Type.Results
			if res == nil || res.NumFields() != 1 {
				n := 0
				if res != nil {
					n = res.NumFields()
				}
				t.Errorf("%s: hostileExpectations returns %d results; it returns the expectations alone, with no slot for an exemption", fset.Position(fn.Pos()), n)
				continue
			}
			if arr, ok := res.List[0].Type.(*ast.ArrayType); !ok || arr.Len != nil || types(arr.Elt) != "expectation" {
				t.Errorf("%s: hostileExpectations returns something other than []expectation", fset.Position(fn.Pos()))
			}
		}
	}
	if !found {
		t.Fatal("the harness has no hostileExpectations: this test guards nothing")
	}
}

func types(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
