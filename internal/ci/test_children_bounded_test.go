package ci

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// boundedChildDirs are the packages whose tests start an interpreter or a
// compiled leg's toolchain and binaries, and the helper packages that start
// them for them. Every child process they start goes
// through internal/testproc, which kills it with its process group when the
// test ends, before the test binary's deadline, and (on Linux) when the binary
// dies (review-owed/ci-tools/test-children-bounded:
// TestLuauVariableSaveRefusesOffDeclaration once left a luau process spinning
// for six days on a bench).
var boundedChildDirs = []string{
	"internal/codegen/dart", "internal/codegen/darttable",
	"internal/codegen/elixir", "internal/codegen/elixirtable",
	"internal/codegen/js", "internal/codegen/jstable",
	"internal/codegen/lobster", "internal/codegen/lobstertable", "internal/codegen/lobster/lobsterrun",
	"internal/codegen/lua", "internal/codegen/luatable", "internal/codegen/lua/luarun",
	"internal/codegen/luau", "internal/codegen/luautable", "internal/codegen/luau/luaurun",
	"internal/codegen/moo", "internal/codegen/mootable", "internal/codegen/moo/moorun",
	"internal/codegen/python", "internal/codegen/pythontable", "internal/codegen/python/pyrun",
	"internal/codegen/ruby", "internal/codegen/rubytable", "internal/codegen/ruby/rubyrun",
	"internal/codegen/squirrel", "internal/codegen/squirreltable", "internal/codegen/squirrel/sqrun",
	"internal/codegen/c", "internal/codegen/ctable",
	"internal/codegen/cpp", "internal/codegen/cpptable",
	"internal/codegen/csharp", "internal/codegen/cstable", "internal/codegen/unitytable",
	"internal/codegen/golang", "internal/codegen/gotable", "internal/codegen/gotable/fuzzunit",
	"internal/codegen/java", "internal/codegen/javatable",
	"internal/codegen/rust", "internal/codegen/rusttable",
	"compiler",
	"tools/namebuild",
}

// helperDir reports whether dir is a run-helper package, whose every file
// (not only its tests) starts children for tests.
func helperDir(dir string) bool { return strings.HasSuffix(dir, "run") }

// unboundedChildren returns file:line for every call of os/exec's Command or
// CommandContext in src, under whatever name the file imports os/exec.
func unboundedChildren(fset *token.FileSet, name string, src []byte) ([]string, error) {
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	execName := ""
	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == "os/exec" {
			execName = "exec"
			if imp.Name != nil {
				execName = imp.Name.Name
			}
		}
	}
	if execName == "" || execName == "_" {
		return nil, nil
	}
	var at []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == execName && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
			p := fset.Position(sel.Pos())
			at = append(at, p.Filename+":"+strconv.Itoa(p.Line))
		}
		return true
	})
	return at, nil
}

// TestEveryTestChildIsBounded holds every child a covered test starts to
// internal/testproc: no _test.go file in a covered package, and no file in a
// run-helper package, calls exec.Command or exec.CommandContext.
func TestEveryTestChildIsBounded(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fset := token.NewFileSet()
	var bad []string
	for _, dir := range boundedChildDirs {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("covered package %s: %v", dir, err)
		}
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !judgedFile(dir, n) {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(dir, n))
			src, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			at, err := unboundedChildren(fset, rel, src)
			if err != nil {
				t.Fatal(err)
			}
			bad = append(bad, at...)
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("%s starts a child with os/exec directly; use testproc.Command(t, ...) so it cannot outlive its test", b)
	}
}

// TestEveryTestChildIsBoundedNegativeControl plants each way a file can start
// an unbounded child and checks the gate's reader sees it, and that the
// bounded form passes.
func TestEveryTestChildIsBoundedNegativeControl(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      int
	}{
		{"exec.Command", "package p\nimport \"os/exec\"\nfunc f() { exec.Command(\"luau\") }\n", 1},
		{"exec.CommandContext", "package p\nimport (\"context\"; \"os/exec\")\nfunc f() { exec.CommandContext(context.Background(), \"luau\") }\n", 1},
		{"renamed import", "package p\nimport osexec \"os/exec\"\nfunc f() { osexec.Command(\"luau\") }\n", 1},
		{"taken as a value", "package p\nimport \"os/exec\"\nvar start = exec.Command\n", 1},
		{"bounded", "package p\nimport \"github.com/mas-bandwidth/schema/v2/internal/testproc\"\nfunc f(t testing.TB) { testproc.Command(t, \"luau\") }\n", 0},
		{"exec for its types only", "package p\nimport \"os/exec\"\nfunc f(c *exec.Cmd) error { var e *exec.ExitError; _ = e; return c.Run() }\n", 0},
	}
	for _, c := range cases {
		at, err := unboundedChildren(token.NewFileSet(), c.name+".go", []byte(c.src))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(at) != c.want {
			t.Errorf("%s: the gate saw %d unbounded child(ren) %v, want %d", c.name, len(at), at, c.want)
		}
	}
}
