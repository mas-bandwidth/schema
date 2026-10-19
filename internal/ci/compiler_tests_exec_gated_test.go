package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// execGateFinding is one compiler test that can reach an exec without passing
// slowtest.Gate on the way.
type execGateFinding struct {
	test   string
	chain  []string
	file   string
	line   int
	callee string
}

func (f execGateFinding) String() string {
	where := fmt.Sprintf("%s:%d", f.file, f.line)
	return fmt.Sprintf("%s: %s reaches %s without slowtest.Gate first (%s)",
		where, strings.Join(f.chain, " -> "), f.callee, f.test)
}

// TestCompilerTestsExecOnlyBehindTheSlowGate is the static guard behind the
// one-minute rule: a compiler test that starts a program — a foreign compiler,
// the Go toolchain, or a generated binary — must reach slowtest.Gate first on
// that path. The gate is what keeps `go test ./compiler/` pure Go; an ungated
// exec puts a toolchain on the default run and makes the package's wall time
// somebody else's problem.
//
// It follows calls by name through the package's own test helpers, cutting
// cycles, and reads source order: a Gate before an exec on the same path
// counts, a Gate in a sibling subtest does not.
func TestCompilerTestsExecOnlyBehindTheSlowGate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "compiler", "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no compiler/*_test.go found — this guard would pass over an empty set")
	}
	sort.Strings(paths)
	fset := token.NewFileSet()
	files := make(map[string]*ast.File, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		file, err := parser.ParseFile(fset, path, data, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[path] = file
	}
	findings := ungatedExecs(fset, files)
	for _, f := range findings {
		t.Error(f.String())
	}
}

// ungatedExecs walks every Test function in the parsed files. A helper found
// by name is walked with the caller's gate state, so `Gate(t, ...); run()`
// covers run's exec while `run(); exec.Command(...)` does not.
func ungatedExecs(fset *token.FileSet, files map[string]*ast.File) []execGateFinding {
	funcs := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || fn.Body == nil {
				continue
			}
			if _, exists := funcs[fn.Name.Name]; !exists {
				funcs[fn.Name.Name] = fn
			}
		}
	}
	var findings []execGateFinding
	seen := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			visiting := map[string]bool{fn.Name.Name: true}
			walkCalls(fset, fn.Body, false, []string{fn.Name.Name}, visiting, funcs, func(f execGateFinding) {
				key := fmt.Sprintf("%s:%d:%s", f.file, f.line, strings.Join(f.chain, ">"))
				if seen[key] {
					return
				}
				seen[key] = true
				findings = append(findings, f)
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		if findings[i].line != findings[j].line {
			return findings[i].line < findings[j].line
		}
		return findings[i].test < findings[j].test
	})
	return findings
}

func walkCalls(fset *token.FileSet, node ast.Node, gated bool, chain []string, visiting map[string]bool, funcs map[string]*ast.FuncDecl, report func(execGateFinding)) {
	state := gated
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			walkCalls(fset, x.Body, state, chain, visiting, funcs, report)
			return false
		case *ast.CallExpr:
			if isSlowGate(x) {
				state = true
				return true
			}
			if callee, ok := execCall(x); ok {
				if !state {
					pos := fset.Position(x.Pos())
					report(execGateFinding{
						test:   chain[0],
						chain:  append([]string(nil), chain...),
						file:   pos.Filename,
						line:   pos.Line,
						callee: callee,
					})
				}
				return true
			}
			name, ok := localCallName(x)
			if !ok {
				return true
			}
			fn := funcs[name]
			if fn == nil || visiting[name] {
				return true
			}
			visiting[name] = true
			walkCalls(fset, fn.Body, state, append(append([]string(nil), chain...), name), visiting, funcs, report)
			delete(visiting, name)
		}
		return true
	})
}

func isSlowGate(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "slowtest" && sel.Sel.Name == "Gate"
}

func execCall(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	qualified := pkg.Name + "." + sel.Sel.Name
	switch qualified {
	case "exec.Command", "exec.CommandContext", "os.StartProcess", "syscall.Exec":
		return qualified, true
	}
	return "", false
}

func localCallName(call *ast.CallExpr) (string, bool) {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// TestCompilerExecGuardReversedWitness is the reversed witness for the guard:
// a helper that execs with no gate is caught and names both the test and the
// helper; the same path with slowtest.Gate at the test's top passes; and a
// pure-Go test passes. Fixtures are sources, so this proves the rule without
// touching compiler/.
func TestCompilerExecGuardReversedWitness(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		want    []string
		wantAny bool
	}{
		{
			name: "exec through an ungated helper fails naming both",
			src: `package compiler
import (
	"os/exec"
	"testing"
)
func TestExecThroughHelper(t *testing.T) { helper() }
func helper() { exec.Command("cc", "x") }
`,
			want:    []string{"TestExecThroughHelper -> helper", "exec.Command"},
			wantAny: true,
		},
		{
			name: "Gate at the test's top covers the helper",
			src: `package compiler
import (
	"os/exec"
	"testing"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)
func TestGatedThroughHelper(t *testing.T) { slowtest.Gate(t, "cc"); helper() }
func helper() { exec.Command("cc", "x") }
`,
			wantAny: false,
		},
		{
			name: "a pure-Go test passes",
			src: `package compiler
import "testing"
func TestPureGo(t *testing.T) { _ = 1 + 1 }
`,
			wantAny: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.name+".go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			findings := ungatedExecs(fset, map[string]*ast.File{tc.name + ".go": file})
			if !tc.wantAny {
				if len(findings) != 0 {
					t.Fatalf("want no finding, got %v", findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("want a finding naming %v, got none", tc.want)
			}
			got := findings[0].String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("finding does not name %q: %s", want, got)
				}
			}
		})
	}
}
