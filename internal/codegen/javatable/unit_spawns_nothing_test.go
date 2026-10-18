package javatable

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

// TestUnitTierSpawnsNoProgram holds the owner rule (Glenn, 2026-10-12): a unit
// test is logic only; anything that runs a program is a functional test. It
// reads the package's own untagged sources with go/parser — no process, no
// network — and fails by file and line on a process-spawning reference. The
// functional-tagged files are where javac and java are allowed to live.
func TestUnitTierSpawnsNoProgram(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var findings []string
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(src), "//go:build functional") {
			continue
		}
		f, err := parser.ParseFile(fset, name, src, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ImportSpec:
				if x.Path != nil {
					if p, err := strconv.Unquote(x.Path.Value); err == nil && p == "os/exec" {
						findings = append(findings, fset.Position(x.Pos()).String()+": imports os/exec")
					}
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" {
						switch sel.Sel.Name {
						case "Command", "CommandContext", "LookPath":
							findings = append(findings, fset.Position(x.Pos()).String()+": calls exec."+sel.Sel.Name)
						}
					}
				}
				for _, arg := range x.Args {
					if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, "javac") {
							findings = append(findings, fset.Position(lit.Pos()).String()+": call mentions javac")
						}
					}
				}
			}
			return true
		})
	}
	if len(findings) > 0 {
		t.Fatalf("unit tier spawns programs (%d finding(s)); move these to a //go:build functional file:\n%s", len(findings), strings.Join(findings, "\n"))
	}
}
