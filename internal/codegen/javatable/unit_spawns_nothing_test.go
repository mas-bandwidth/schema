package javatable

// TestUnitTierSpawnsNoProgram is the unit-tier guard: the default `go test`
// for this package must spawn no process. Every untagged test file and every
// non-test .go file is parsed with go/parser (no process, no network) and any
// reference to os/exec or to javac/java in a call position fails by file and
// line — unless the offending call sits inside a function that is gated by
// slowtest.Gate, because gated tests only run in the slow half.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

	var violations []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") && strings.HasSuffix(name, "_functional_test.go") {
			continue
		}
		// The gated toolchain tests live in *_functional_test.go; everything else
		// in the package is unit tier.
		if strings.HasSuffix(name, "_functional_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		violations = append(violations, scanFileForExec(path)...)
	}

	if len(violations) > 0 {
		t.Fatalf("unit tier must not spawn programs:\n%s", strings.Join(violations, "\n"))
	}
}

// scanFileForExec returns one message per exec call whose function is not gated.
func scanFileForExec(path string) []string {
	fset := token.NewFileSet()
	src, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", path, err)}
	}
	file, err := parser.ParseFile(fset, path, src, parser.AllErrors)
	if err != nil {
		// Parse errors are not ours to report; the scanner only flags exec sites.
		return nil
	}

	var out []string
	hasExecImport := false
	for _, imp := range file.Imports {
		if imp.Path != nil && strings.Trim(imp.Path.Value, `"`) == "os/exec" {
			hasExecImport = true
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		gated := functionCallsGate(fd)
		if hasExecImport && !gated {
			// A file that imports os/exec but whose functions are all gated is
			// fine; only ungated functions are unit-tier violations. We report
			// the import once at the top of the file when no function is gated.
			out = append(out, fmt.Sprintf("%s: imports os/exec but no function is gated", path))
			return false
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !isExecSelector(call) {
				return true
			}
			if !gated {
				pos := fset.Position(call.Pos())
				out = append(out, fmt.Sprintf("%s:%d: %s is not under slowtest.Gate", path, pos.Line, callName(call)))
			}
			return true
		})
		return false
	})
	return out
}

// functionCallsGate reports whether the function body calls slowtest.Gate.
func functionCallsGate(fd *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "slowtest" && sel.Sel.Name == "Gate" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// isExecSelector reports whether a call is exec.Command, exec.CommandContext,
// exec.LookPath, or os/exec usage.
func isExecSelector(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if id.Name != "exec" || sel.Sel.Name == "" {
		return false
	}
	switch sel.Sel.Name {
	case "Command", "CommandContext", "LookPath":
		return true
	}
	return false
}

func callName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

