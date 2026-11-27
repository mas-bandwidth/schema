package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoTestFunctionIsASkippingHelper holds this package's tests to one rule
// the nightly readiness gate depends on: a subprocess helper is entered from
// TestMain, never written as a Test that skips outside its subprocess. Such a
// Test skips in every ordinary run, and readiness refuses every unclassified
// native skip, so nightly 45119714773's native-harness phase went red on
// TestJSONFuzzSyntheticDriverHelper ("helper for
// TestJSONFuzzPlantedNegativeControls").
func TestNoTestFunctionIsASkippingHelper(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no test files found in the harness package")
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Body == nil {
				continue
			}
			scanned++
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Skip" && sel.Sel.Name != "Skipf") {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err == nil && strings.HasPrefix(strings.ToLower(text), "helper") {
					t.Errorf("%s: %s is a helper written as a Test that skips (%q): enter it from TestMain instead",
						fset.Position(call.Pos()), fn.Name.Name, text)
				}
				return true
			})
		}
	}
	if scanned == 0 {
		t.Fatal("no Test functions scanned")
	}
}
