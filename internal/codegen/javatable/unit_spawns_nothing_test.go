package javatable

// THE UNIT TIER SPAWNS NO PROGRAM. This package's default `go test` used to
// start about 53 javac processes at once (one `go test ./...` on a 64-core
// bench, 2026-10-18), because sixteen test files reached javaTools/exec.Command
// with no slowtest.Gate and no bound. The slow tier is the repo's one mechanism
// (slowtest.Gate under SCHEMA_SLOW=1), so there is no build tag here; what this
// test enforces instead is that every file which can spawn a program carries
// the gate, and that no Test function calls exec directly. Program executions
// must go through boundedCommand, which gates and bounds them.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUnitTierSpawnsNoProgram(t *testing.T) {
	t.Parallel()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			checked++
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		var spawnRefs []token.Pos
		importsExec := false
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == "os/exec" {
				importsExec = true
			}
		}
		gated := fileCallsGate(f)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isExecCall(call) {
				spawnRefs = append(spawnRefs, call.Pos())
			}
			return true
		})
		if (len(spawnRefs) > 0 || importsExec) && !gated {
			pos := f.Package
			if len(spawnRefs) > 0 {
				pos = spawnRefs[0]
			}
			where := fset.Position(pos)
			t.Errorf("%s:%d: this file can spawn a program (os/exec or exec.*) but carries no slowtest.Gate: program runs belong in the slow tier", name, where.Line)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if ok && isExecCall(call) {
					where := fset.Position(call.Pos())
					t.Errorf("%s:%d: %s calls exec directly; route every javac/java execution through boundedCommand (gated and bounded)", name, where.Line, fn.Name.Name)
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("scanned no test files; the package directory is not what this test thinks it is")
	}
}

func isExecCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != "exec" {
		return false
	}
	switch sel.Sel.Name {
	case "Command", "CommandContext", "LookPath":
		return true
	}
	return false
}

func fileCallsGate(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if ok && ident.Name == "slowtest" && sel.Sel.Name == "Gate" {
			found = true
			return false
		}
		return true
	})
	return found
}
