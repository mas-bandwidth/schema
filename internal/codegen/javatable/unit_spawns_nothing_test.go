package javatable

// TestUnitTierSpawnsNoProgram is the unit-tier guard: the default `go test`
// for this package must spawn no process. Every untagged test file and every
// non-test .go file is parsed with go/parser (no process, no network) and any
// reference to os/exec in a call position fails by file and line — unless the
// offending call sits inside a function that is gated, directly or through a
// gated helper, by slowtest.Gate. Gated tests only run in the slow half.

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

	var files []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		// The gated toolchain tests live in *_functional_test.go; everything else
		// in the package is unit tier.
		if strings.HasSuffix(name, "_functional_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}

	gated := computeGatedFunctions(files)

	var violations []string
	for _, path := range files {
		violations = append(violations, scanFileForExec(path, gated)...)
	}

	if len(violations) > 0 {
		t.Fatalf("unit tier must not spawn programs:\n%s", strings.Join(violations, "\n"))
	}
}

// computeGatedFunctions returns the set of package-function names that are
// gated: those that call slowtest.Gate directly, transitively closed over the
// package's own call graph (a helper that calls a gated helper is itself
// gated).
func computeGatedFunctions(files []string) map[string]bool {
	direct := map[string]bool{}
	calls := map[string]map[string]bool{}
	for _, path := range files {
		fset := token.NewFileSet()
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, path, src, parser.AllErrors)
		if err != nil {
			continue
		}
		for _, fd := range file.Decls {
			decl, ok := fd.(*ast.FuncDecl)
			if !ok || decl.Name == nil || decl.Body == nil {
				continue
			}
			name := decl.Name.Name
			if directGateCall(decl.Body) {
				direct[name] = true
			}
			for called := range packageCalls(decl.Body) {
				if calls[name] == nil {
					calls[name] = map[string]bool{}
				}
				calls[name][called] = true
			}
		}
	}
	gated := map[string]bool{}
	for n := range direct {
		gated[n] = true
	}
	for changed := true; changed; {
		changed = false
		for fn, cset := range calls {
			if gated[fn] {
				continue
			}
			for called := range cset {
				if gated[called] {
					gated[fn] = true
					changed = true
					break
				}
			}
		}
	}
	return gated
}

// directGateCall reports whether the body directly calls slowtest.Gate.
func directGateCall(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
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

// packageCalls reports the set of package-level function names called in body.
func packageCalls(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if id.Name != "" {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// scanFileForExec returns one message per exec call whose function is not gated.
func scanFileForExec(path string, gated map[string]bool) []string {
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
	ast.Inspect(file, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Body == nil {
			return true
		}
		if gated[fd.Name.Name] {
			return false
		}
		ast.Inspect(fd.Body, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !isExecSelector(call) {
				return true
			}
			pos := fset.Position(call.Pos())
			out = append(out, fmt.Sprintf("%s:%d: %s is not under slowtest.Gate", path, pos.Line, callName(call)))
			return true
		})
		return false
	})
	return out
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
