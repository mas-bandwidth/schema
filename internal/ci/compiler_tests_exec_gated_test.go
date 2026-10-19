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
	walkStmt(fset, node, gated, chain, visiting, funcs, report)
}

// walkStmt walks statements in source order and returns whether slowtest.Gate
// has definitely run on every path to the end of the statement. Branches join
// with AND: a Gate in one arm of an if does not gate the code after it unless
// every arm reaches a Gate, and a loop body may run zero times, so neither
// gates what follows. Expressions still follow local calls by name.
func walkStmt(fset *token.FileSet, node ast.Node, gated bool, chain []string, visiting map[string]bool, funcs map[string]*ast.FuncDecl, report func(execGateFinding)) bool {
	switch s := node.(type) {
	case *ast.BlockStmt:
		for _, st := range s.List {
			gated = walkStmt(fset, st, gated, chain, visiting, funcs, report)
		}
		return gated
	case *ast.IfStmt:
		if s.Init != nil {
			gated = walkStmt(fset, s.Init, gated, chain, visiting, funcs, report)
		}
		gated = walkExpr(fset, s.Cond, gated, chain, visiting, funcs, report)
		body := walkStmt(fset, s.Body, gated, chain, visiting, funcs, report)
		otherwise := gated
		if s.Else != nil {
			otherwise = walkStmt(fset, s.Else, gated, chain, visiting, funcs, report)
		}
		return body && otherwise
	case *ast.ForStmt:
		if s.Init != nil {
			gated = walkStmt(fset, s.Init, gated, chain, visiting, funcs, report)
		}
		if s.Cond != nil {
			gated = walkExpr(fset, s.Cond, gated, chain, visiting, funcs, report)
		}
		if s.Post != nil {
			gated = walkStmt(fset, s.Post, gated, chain, visiting, funcs, report)
		}
		walkStmt(fset, s.Body, gated, chain, visiting, funcs, report)
		return gated
	case *ast.RangeStmt:
		if s.Key != nil {
			walkExpr(fset, s.Key, gated, chain, visiting, funcs, report)
		}
		if s.Value != nil {
			walkExpr(fset, s.Value, gated, chain, visiting, funcs, report)
		}
		gated = walkExpr(fset, s.X, gated, chain, visiting, funcs, report)
		walkStmt(fset, s.Body, gated, chain, visiting, funcs, report)
		return gated
	case *ast.SwitchStmt:
		if s.Init != nil {
			gated = walkStmt(fset, s.Init, gated, chain, visiting, funcs, report)
		}
		if s.Tag != nil {
			gated = walkExpr(fset, s.Tag, gated, chain, visiting, funcs, report)
		}
		all := true
		hasDefault := false
		for _, clause := range s.Body.List {
			cc := clause.(*ast.CaseClause)
			if len(cc.List) == 0 {
				hasDefault = true
			}
			if walkCase(fset, cc, gated, chain, visiting, funcs, report) != true {
				all = false
			}
		}
		if !hasDefault || !all {
			return gated
		}
		return true
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			gated = walkStmt(fset, s.Init, gated, chain, visiting, funcs, report)
		}
		gated = walkStmt(fset, s.Assign, gated, chain, visiting, funcs, report)
		all := true
		hasDefault := false
		for _, clause := range s.Body.List {
			cc := clause.(*ast.CaseClause)
			if len(cc.List) == 0 {
				hasDefault = true
			}
			if walkCase(fset, cc, gated, chain, visiting, funcs, report) != true {
				all = false
			}
		}
		if !hasDefault || !all {
			return gated
		}
		return true
	case *ast.SelectStmt:
		for _, clause := range s.Body.List {
			cc := clause.(*ast.CommClause)
			state := gated
			if cc.Comm != nil {
				state = walkStmt(fset, cc.Comm, state, chain, visiting, funcs, report)
			}
			for _, st := range cc.Body {
				state = walkStmt(fset, st, state, chain, visiting, funcs, report)
			}
			_ = state
		}
		return gated
	case *ast.LabeledStmt:
		return walkStmt(fset, s.Stmt, gated, chain, visiting, funcs, report)
	case *ast.ExprStmt:
		return walkExpr(fset, s.X, gated, chain, visiting, funcs, report)
	case *ast.AssignStmt:
		for _, e := range s.Rhs {
			gated = walkExpr(fset, e, gated, chain, visiting, funcs, report)
		}
		return gated
	case *ast.ReturnStmt:
		for _, e := range s.Results {
			gated = walkExpr(fset, e, gated, chain, visiting, funcs, report)
		}
		return gated
	case *ast.IncDecStmt:
		return walkExpr(fset, s.X, gated, chain, visiting, funcs, report)
	case *ast.SendStmt:
		gated = walkExpr(fset, s.Chan, gated, chain, visiting, funcs, report)
		return walkExpr(fset, s.Value, gated, chain, visiting, funcs, report)
	case *ast.GoStmt:
		return walkExpr(fset, s.Call, gated, chain, visiting, funcs, report)
	case *ast.DeferStmt:
		return walkExpr(fset, s.Call, gated, chain, visiting, funcs, report)
	case *ast.DeclStmt:
		ast.Inspect(s, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				gated = walkCall(fset, call, gated, chain, visiting, funcs, report)
			}
			return true
		})
		return gated
	}
	return gated
}

func walkCase(fset *token.FileSet, cc *ast.CaseClause, gated bool, chain []string, visiting map[string]bool, funcs map[string]*ast.FuncDecl, report func(execGateFinding)) bool {
	state := gated
	for _, st := range cc.Body {
		state = walkStmt(fset, st, state, chain, visiting, funcs, report)
	}
	return state
}

func walkExpr(fset *token.FileSet, node ast.Node, gated bool, chain []string, visiting map[string]bool, funcs map[string]*ast.FuncDecl, report func(execGateFinding)) bool {
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			walkStmt(fset, x.Body, gated, chain, visiting, funcs, report)
			return false
		case *ast.CallExpr:
			gated = walkCall(fset, x, gated, chain, visiting, funcs, report)
		}
		return true
	})
	return gated
}

func walkCall(fset *token.FileSet, call *ast.CallExpr, gated bool, chain []string, visiting map[string]bool, funcs map[string]*ast.FuncDecl, report func(execGateFinding)) bool {
	if isSlowGate(call) {
		return true
	}
	if callee, ok := execCall(call); ok {
		if !gated {
			pos := fset.Position(call.Pos())
			report(execGateFinding{
				test:   chain[0],
				chain:  append([]string(nil), chain...),
				file:   pos.Filename,
				line:   pos.Line,
				callee: callee,
			})
		}
		return gated
	}
	name, ok := localCallName(call)
	if !ok {
		return gated
	}
	fn := funcs[name]
	if fn == nil || visiting[name] {
		return gated
	}
	visiting[name] = true
	walkStmt(fset, fn.Body, gated, append(append([]string(nil), chain...), name), visiting, funcs, report)
	delete(visiting, name)
	return gated
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
			name: "a Gate in a branch that may not run does not cover the exec after it",
			src: `package compiler
import (
	"os/exec"
	"testing"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)
func TestConditionalGateBypass(t *testing.T) {
	if false {
		slowtest.Gate(t, "cc")
	}
	exec.Command("cc", "x")
}
`,
			want:    []string{"TestConditionalGateBypass", "exec.Command"},
			wantAny: true,
		},
		{
			name: "Gate in every arm of an if covers the exec after it",
			src: `package compiler
import (
	"os/exec"
	"testing"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)
func TestGateInEveryArm(t *testing.T) {
	if flag {
		slowtest.Gate(t, "cc")
	} else {
		slowtest.Gate(t, "cc")
	}
	exec.Command("cc", "x")
}
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
