package javatable

// THE UNIT TIER SPAWNS NO PROGRAM. This package's default `go test` used to
// start about 53 javac processes at once (one `go test ./...` on a 64-core
// bench, 2026-10-18), because sixteen test files reached javaTools/exec.Command
// with no slowtest.Gate and no bound. The slow tier is the repo's one mechanism
// (slowtest.Gate under SCHEMA_SLOW=1), so there is no build tag here. This test
// holds two rules, SITE BY SITE, over every .go file of the package, parsed
// with go/parser (no process, no network):
//
//  1. Every exec site is gated. An exec site is a reference to exec.Command,
//     exec.CommandContext, exec.LookPath, os.StartProcess, syscall.Exec or
//     syscall.ForkExec, or a call that passes the literal "javac" or "java".
//     It is gated when an earlier statement of a block that encloses it, inside
//     its own function, is a gate: a call to slowtest.Gate, or to a package
//     function whose body opens (before any return) with a gate. A gate
//     anywhere else in the file, in a sibling function, or after the site does
//     not count: that was the hole a file-level check left open.
//  2. exec.Command, exec.CommandContext and os.StartProcess appear in
//     boundedCommand alone, so every program run takes a slot of javaDoor's
//     semaphore (functional_java_test.go).
//
// TestUnitTierScanFlagsEachSite feeds the scan planted sources so the rules
// themselves are held, not just today's files.

import (
	"fmt"
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

const slowtestPath = "github.com/mas-bandwidth/schema/v2/internal/slowtest"

// programDoorFuncs are the only functions that may build a program.
var programDoorFuncs = map[string]bool{"boundedCommand": true}

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
	var files []*ast.File
	tests := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			tests++
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	if tests == 0 {
		t.Fatal("scanned no test files; the package directory is not what this test thinks it is")
	}
	for _, v := range scanForPrograms(fset, files) {
		t.Error(v)
	}
}

// TestUnitTierScanFlagsEachSite plants each way an exec site can slip through
// and requires the scan to name exactly the planted lines.
func TestUnitTierScanFlagsEachSite(t *testing.T) {
	t.Parallel()
	const head = `package p

import (
	"os/exec"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func javaTools(t *testing.T) (string, string) {
	slowtest.Gate(t, "javac")
	p, _ := exec.LookPath("javac")
	return p, p
}

func boundedCommand(t *testing.T, name string, args ...string) *exec.Cmd {
	slowtest.Gate(t, "javac")
	return exec.Command(name, args...)
}

func TestGated(t *testing.T) {
	javac, _ := javaTools(t)
	_ = boundedCommand(t, javac, "-version")
	t.Run("sub", func(t *testing.T) {
		_, _ = exec.LookPath("java")
	})
}
`
	cases := []struct {
		name  string
		plant string
		want  []string // "line: rule" for each violation the plant must raise
	}{
		{"clean", ``, nil},
		{"ungated helper beside a gated one", `
func plantedSpawn() { _ = exec.Command("javac", "-version").Run() }
`, []string{"29: not under slowtest.Gate", "29: outside boundedCommand"}},
		{"ungated LookPath beside a gated one", `
func plantedLook() { _, _ = exec.LookPath("javac") }
`, []string{"29: not under slowtest.Gate"}},
		{"gate after the site", `
func TestLate(t *testing.T) {
	_, _ = exec.LookPath("javac")
	slowtest.Gate(t, "javac")
}
`, []string{"30: not under slowtest.Gate"}},
		{"gate inside an earlier if", `
func TestIf(t *testing.T, slow bool) {
	if slow {
		slowtest.Gate(t, "javac")
	}
	_, _ = exec.LookPath("javac")
}
`, []string{"33: not under slowtest.Gate"}},
		{"helper whose gate follows a return", `
func maybeGate(t *testing.T, fast bool) {
	if fast {
		return
	}
	slowtest.Gate(t, "javac")
}

func TestMaybe(t *testing.T) {
	maybeGate(t, true)
	_, _ = exec.LookPath("javac")
}
`, []string{"38: not under slowtest.Gate"}},
		{"exec.Command as a value", `
var spawn = exec.Command
`, []string{"29: not under slowtest.Gate", "29: outside boundedCommand"}},
		{"gated spawn outside the door", `
func TestDirect(t *testing.T) {
	javac, _ := javaTools(t)
	_ = exec.Command(javac).Run()
}
`, []string{"31: outside boundedCommand"}},
		{"javac literal in an ungated call", `
func run(name string) {}

func TestLiteral(t *testing.T) { run("javac") }
`, []string{"31: not under slowtest.Gate"}},
		{"gated closure", `
func TestClosure(t *testing.T) {
	t.Run("x", func(t *testing.T) {
		javac, _ := javaTools(t)
		_ = boundedCommand(t, javac)
		_, _ = exec.LookPath("java")
	})
}
`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "planted.go", head+c.plant, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, v := range scanForPrograms(fset, []*ast.File{f}) {
				// "planted.go:29: exec.Command is not under slowtest.Gate ..."
				rest := strings.TrimPrefix(v, "planted.go:")
				line, msg, _ := strings.Cut(rest, ":")
				switch {
				case strings.Contains(msg, "not under slowtest.Gate"):
					got = append(got, line+": not under slowtest.Gate")
				case strings.Contains(msg, "outside boundedCommand"):
					got = append(got, line+": outside boundedCommand")
				default:
					got = append(got, v)
				}
			}
			sort.Strings(got)
			want := append([]string(nil), c.want...)
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("scan of the plant:\n got %q\nwant %q", got, want)
			}
		})
	}
}

// scanForPrograms returns one line per exec site that breaks a rule, as
// "file:line: message".
func scanForPrograms(fset *token.FileSet, files []*ast.File) []string {
	gating := gatingFuncs(files)
	var out []string
	for _, f := range files {
		names := importNames(f)
		file := filepath.Base(fset.Position(f.Package).Filename)
		report := func(n ast.Node, what string, gated bool, fn string) {
			line := fset.Position(n.Pos()).Line
			if !gated {
				out = append(out, fmt.Sprintf("%s:%d: %s is not under slowtest.Gate: a program run belongs in the slow tier, gated in its own function before the site", file, line, what))
			}
			if spawns(what) && !programDoorFuncs[fn] {
				out = append(out, fmt.Sprintf("%s:%d: %s is outside boundedCommand: build every program there, so it takes a slot of javaDoor", file, line, what))
			}
		}
		for _, decl := range f.Decls {
			fd, isFunc := decl.(*ast.FuncDecl)
			fn := ""
			if isFunc && fd.Recv == nil {
				fn = fd.Name.Name
			}
			var stack []ast.Node
			ast.Inspect(decl, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, n)
				if what := execSite(n, names); what != "" {
					report(n, what, isFunc && dominatedByGate(stack, names, gating), fn)
				}
				return true
			})
		}
	}
	return out
}

// spawns reports whether an exec site starts a program (as opposed to finding one).
func spawns(what string) bool {
	switch what {
	case "exec.Command", "exec.CommandContext", "os.StartProcess", "syscall.Exec", "syscall.ForkExec":
		return true
	}
	return false
}

// importNames maps an import path to its local name in f.
func importNames(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		m[p] = name
	}
	return m
}

// execSite names the exec site n is, or returns "".
func execSite(n ast.Node, names map[string]string) string {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return ""
		}
		for path, sels := range map[string][]string{
			"os/exec": {"Command", "CommandContext", "LookPath"},
			"os":      {"StartProcess"},
			"syscall": {"Exec", "ForkExec"},
		} {
			local, imported := names[path]
			if !imported || id.Name != local {
				continue
			}
			for _, s := range sels {
				if x.Sel.Name == s {
					return path[strings.LastIndex(path, "/")+1:] + "." + s
				}
			}
		}
	case *ast.CallExpr:
		if execSite(x.Fun, names) != "" {
			return "" // exec.LookPath("javac") is one site, named by its selector
		}
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == names[slowtestPath] && sel.Sel.Name == "Gate" {
				return "" // slowtest.Gate(t, "javac") names the cost; it runs nothing
			}
		}
		for _, a := range x.Args {
			if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, _ := strconv.Unquote(lit.Value); s == "javac" || s == "java" {
					return "a call naming " + lit.Value
				}
			}
		}
	}
	return ""
}

// isGateStmt reports whether s is itself a gate: a statement whose own call is
// slowtest.Gate or a gating package function.
func isGateStmt(s ast.Stmt, names map[string]string, gating map[string]bool) bool {
	var calls []ast.Expr
	switch x := s.(type) {
	case *ast.ExprStmt:
		calls = append(calls, x.X)
	case *ast.AssignStmt:
		calls = append(calls, x.Rhs...)
	case *ast.DeclStmt:
		if g, ok := x.Decl.(*ast.GenDecl); ok {
			for _, sp := range g.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok {
					calls = append(calls, vs.Values...)
				}
			}
		}
	}
	slow, haveSlow := names[slowtestPath]
	for _, e := range calls {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			continue
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := fun.X.(*ast.Ident); ok && haveSlow && id.Name == slow && fun.Sel.Name == "Gate" {
				return true
			}
		case *ast.Ident:
			if gating[fun.Name] {
				return true
			}
		}
	}
	return false
}

// dominatedByGate reports whether the node at the top of stack runs only after
// a gate: some block on the path to it, inside its function, holds a gate
// statement before the statement that leads to the node.
func dominatedByGate(stack []ast.Node, names map[string]string, gating map[string]bool) bool {
	for i := len(stack) - 2; i >= 0; i-- {
		var list []ast.Stmt
		switch b := stack[i].(type) {
		case *ast.BlockStmt:
			list = b.List
		case *ast.CaseClause:
			list = b.Body
		case *ast.CommClause:
			list = b.Body
		default:
			continue
		}
		child := stack[i+1]
		for _, s := range list {
			if s == child {
				break
			}
			if isGateStmt(s, names, gating) {
				return true
			}
		}
	}
	return false
}

// gatingFuncs returns the package functions whose body opens with a gate: a
// gate statement at the top level of the body before any statement that can
// return. Closed over the package's call graph: a function that opens by
// calling a gating function gates too.
func gatingFuncs(files []*ast.File) map[string]bool {
	gating := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, f := range files {
			names := importNames(f)
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || fd.Body == nil || gating[fd.Name.Name] {
					continue
				}
				for _, s := range fd.Body.List {
					if isGateStmt(s, names, gating) {
						gating[fd.Name.Name] = true
						changed = true
						break
					}
					if canReturn(s) {
						break
					}
				}
			}
		}
	}
	return gating
}

// canReturn reports whether s holds a return outside any function literal.
func canReturn(s ast.Stmt) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			found = true
		}
		return !found
	})
	return found
}
