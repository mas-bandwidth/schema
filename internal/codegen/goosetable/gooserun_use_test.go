package goosetable_test

// EVERY PROCESS A GOOSE TEST STARTS GOES THROUGH gooserun.Command
// (internal/codegen/goose/gooserun). gooserun.Command bounds the wall time and,
// on Linux, kills the process when the test binary dies, so no Goose process
// outlives the test that started it. This test reads every _test.go file of the
// package and refuses a bare exec.Command or exec.CommandContext outside the one
// deliberate re-exec that proves the kill: the helper in diewithparent_test.go,
// allowed by its enclosing function rather than by line. Its negative control
// holds a bare exec.Command in in-memory source and requires it refused by name.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
)

// gooserunUseExempt names the calls a bare exec.Command is right for, by the
// file and enclosing function that hold them. TestGooseDieWithParent re-executes
// this test binary on purpose to show its driver dies with it.
var gooserunUseExempt = map[string]map[string]bool{
	"diewithparent_test.go": {"TestGooseDieWithParent": true},
}

// checkGooserunUse returns one line per bare exec.Command or exec.CommandContext
// call in files that is not exempt, naming file, line and function.
func checkGooserunUse(files map[string][]byte) []string {
	var out []string
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, files[name], 0)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: parse: %v", name, err))
			continue
		}
		local := ""
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) != "os/exec" {
				continue
			}
			local = "exec"
			if imp.Name != nil {
				local = imp.Name.Name
			}
			if local == "_" || local == "." {
				local = ""
			}
		}
		if local == "" {
			continue
		}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || x.Name != local {
				return true
			}
			if sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" {
				return true
			}
			fn := enclosingFunction(stack)
			if gooserunUseExempt[name][fn] {
				return true
			}
			pos := fset.Position(call.Pos())
			out = append(out, fmt.Sprintf("%s:%d: exec.%s in %s must use gooserun.Command", name, pos.Line, sel.Sel.Name, fn))
			return true
		})
	}
	slices.Sort(out)
	return out
}

// enclosingFunction names the innermost function declaration holding the
// inspected node, or the file's package when the call sits at package level.
func enclosingFunction(stack []ast.Node) string {
	for i := len(stack) - 1; i >= 0; i-- {
		if fn, ok := stack[i].(*ast.FuncDecl); ok {
			return fn.Name.Name
		}
	}
	return "(package level)"
}

func TestGooseTestsStartProcessesThroughGooserun(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = b
	}
	if len(files) == 0 {
		t.Fatal("no _test.go files to check")
	}
	if bad := checkGooserunUse(files); len(bad) > 0 {
		t.Fatalf("every process a Goose test starts must go through gooserun.Command:\n%s", strings.Join(bad, "\n"))
	}
}

func TestGooseTestsStartProcessesThroughGooserunNegativeControl(t *testing.T) {
	t.Parallel()
	const src = `package goosetable_test

import "os/exec"

func startIt() {
	_ = exec.Command("goose", "run", "main.goose")
}
`
	bad := checkGooserunUse(map[string][]byte{"control_test.go": []byte(src)})
	if len(bad) != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the check refused %d call(s), want 1: %v", len(bad), bad)
	}
	if !strings.Contains(bad[0], "control_test.go") || !strings.Contains(bad[0], "exec.Command") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the check did not refuse the bare exec.Command by name: %v", bad[0])
	}
}
