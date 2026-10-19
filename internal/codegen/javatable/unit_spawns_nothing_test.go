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
// and requires the scan to name exactly the planted lines. The head is a small
// copy of the package's real shape; each want is "<line in the plant>: <rule>",
// line 1 being the plant's first line.
func TestUnitTierScanFlagsEachSite(t *testing.T) {
	t.Parallel()
	const head = `package p

import (
	"os/exec"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type programDoor struct{ run func(*exec.Cmd) error }

type boundedCmd struct {
	unboundedCmd *exec.Cmd
	door         *programDoor
}

func runProgram(c *exec.Cmd) error { return c.Run() }

var javaDoor = &programDoor{run: runProgram}

func (d *programDoor) wrap(c *exec.Cmd) *boundedCmd { return &boundedCmd{unboundedCmd: c, door: d} }

func (c *boundedCmd) Run() error { return c.door.run(c.unboundedCmd) }

func javaTools(t *testing.T) (string, string) {
	slowtest.Gate(t, "javac")
	p, _ := exec.LookPath("javac")
	return p, p
}

func boundedCommand(t *testing.T, name string, args ...string) *boundedCmd {
	slowtest.Gate(t, "javac")
	return javaDoor.wrap(exec.Command(name, args...))
}

func TestGated(t *testing.T) {
	javac, _ := javaTools(t)
	_ = boundedCommand(t, javac, "-version").Run()
	t.Run("sub", func(t *testing.T) {
		_, _ = exec.LookPath("java")
	})
}
`
	cases := []struct {
		name  string
		plant string
		want  []string
	}{
		{"clean", ``, nil},
		{"ungated helper beside a gated one", `
func plantedSpawn() { _ = exec.Command("javac", "-version").Run() }
`, []string{"1: not under slowtest.Gate", "1: outside boundedCommand"}},
		{"ungated LookPath beside a gated one", `
func plantedLook() { _, _ = exec.LookPath("javac") }
`, []string{"1: not under slowtest.Gate"}},
		{"gate after the site", `
func TestLate(t *testing.T) {
	_, _ = exec.LookPath("javac")
	slowtest.Gate(t, "javac")
}
`, []string{"2: not under slowtest.Gate"}},
		{"gate inside an earlier if", `
func TestIf(t *testing.T, slow bool) {
	if slow {
		slowtest.Gate(t, "javac")
	}
	_, _ = exec.LookPath("javac")
}
`, []string{"5: not under slowtest.Gate"}},
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
`, []string{"10: not under slowtest.Gate"}},
		{"exec.Command as a value", `
var spawn = exec.Command
`, []string{"1: not under slowtest.Gate", "1: outside boundedCommand"}},
		{"gated spawn outside the door", `
func TestDirect(t *testing.T) {
	javac, _ := javaTools(t)
	_ = exec.Command(javac).Run()
}
`, []string{"3: outside boundedCommand"}},
		{"javac literal in an ungated call", `
func run(name string) {}

func TestLiteral(t *testing.T) { run("javac") }
`, []string{"3: not under slowtest.Gate"}},
		{"exec.Cmd literal", `
func TestLit(t *testing.T) { _ = (&exec.Cmd{Path: "/usr/bin/javac"}).Run() }
`, []string{"1: not under slowtest.Gate", "1: outside boundedCommand"}},
		{"new(exec.Cmd)", `
func TestNew(t *testing.T) { c := new(exec.Cmd); c.Path = "/usr/bin/javac"; _ = c.Run() }
`, []string{"1: not under slowtest.Gate", "1: outside boundedCommand"}},
		{"raw command pulled out of the door", `
func TestRaw(t *testing.T) {
	javac, _ := javaTools(t)
	_ = boundedCommand(t, javac).unboundedCmd.Run()
}
`, []string{"3: unboundedCmd outside boundedCmd"}},
		{"a second real door", `
var wideDoor = &programDoor{run: runProgram}
`, []string{"1: runProgram outside javaDoor"}},
		{"dot import", `
func TestDot(t *testing.T) {}
`, []string{"dot import"}},
		{"gated closure", `
func TestClosure(t *testing.T) {
	t.Run("x", func(t *testing.T) {
		javac, _ := javaTools(t)
		_ = boundedCommand(t, javac).Run()
		_, _ = exec.LookPath("java")
	})
}
`, nil},
	}
	base := strings.Count(head, "\n") + 1 // the plant's leading newline ends head's last line
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := head + c.plant
			if c.name == "dot import" {
				src = strings.Replace(src, "import (\n", "import (\n\t. \"os\"\n", 1)
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "planted.go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, v := range scanForPrograms(fset, []*ast.File{f}) {
				// "planted.go:29: exec.Command is not under slowtest.Gate ..."
				lineText, msg, _ := strings.Cut(strings.TrimPrefix(v, "planted.go:"), ":")
				n, _ := strconv.Atoi(lineText)
				line := strconv.Itoa(n - base)
				switch {
				case strings.Contains(msg, "dot import"):
					got = append(got, "dot import")
				case strings.Contains(msg, "not under slowtest.Gate"):
					got = append(got, line+": not under slowtest.Gate")
				case strings.Contains(msg, "outside boundedCommand"):
					got = append(got, line+": outside boundedCommand")
				case strings.Contains(msg, ".unboundedCmd is read outside"):
					got = append(got, line+": unboundedCmd outside boundedCmd")
				case strings.Contains(msg, "runProgram is used outside"):
					got = append(got, line+": runProgram outside javaDoor")
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
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if imp.Name != nil && imp.Name.Name == "." && (p == "os/exec" || p == "os" || p == "syscall") {
				out = append(out, fmt.Sprintf("%s:%d: dot import of %s hides its exec sites from this scan", file, fset.Position(imp.Pos()).Line, p))
			}
		}
		for _, decl := range f.Decls {
			fd, isFunc := decl.(*ast.FuncDecl)
			fn, recv := "", ""
			if isFunc && fd.Recv == nil {
				fn = fd.Name.Name
			}
			if isFunc && fd.Recv != nil && len(fd.Recv.List) == 1 {
				rt := fd.Recv.List[0].Type
				if st, ok := rt.(*ast.StarExpr); ok {
					rt = st.X
				}
				if id, ok := rt.(*ast.Ident); ok {
					recv = id.Name
				}
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
				switch x := n.(type) {
				case *ast.SelectorExpr:
					// The raw command inside a boundedCmd runs only through its door.
					if x.Sel.Name == "unboundedCmd" && recv != "boundedCmd" {
						out = append(out, fmt.Sprintf("%s:%d: .unboundedCmd is read outside boundedCmd's methods: running it skips javaDoor's semaphore", file, fset.Position(x.Pos()).Line))
					}
				case *ast.Ident:
					// The real executor belongs to javaDoor alone: a second door
					// built on it would run programs past the pinned bound.
					if x.Name == "runProgram" && !(isFunc && fd.Name == x) && !isJavaDoorSpec(decl) {
						out = append(out, fmt.Sprintf("%s:%d: runProgram is used outside javaDoor's declaration: a second real door is not held to max(1, GOMAXPROCS/4)", file, fset.Position(x.Pos()).Line))
					}
				}
				return true
			})
		}
	}
	return out
}

// isJavaDoorSpec reports whether decl is the `var javaDoor = ...` declaration.
func isJavaDoorSpec(decl ast.Decl) bool {
	g, ok := decl.(*ast.GenDecl)
	if !ok || g.Tok != token.VAR || len(g.Specs) != 1 {
		return false
	}
	vs, ok := g.Specs[0].(*ast.ValueSpec)
	return ok && len(vs.Names) == 1 && vs.Names[0].Name == "javaDoor"
}

// spawns reports whether an exec site starts a program (as opposed to finding one).
func spawns(what string) bool {
	switch what {
	case "exec.Command", "exec.CommandContext", "an exec.Cmd literal", "new(exec.Cmd)",
		"os.StartProcess", "syscall.Exec", "syscall.ForkExec", "syscall.StartProcess":
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
			"syscall": {"Exec", "ForkExec", "StartProcess"},
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
	case *ast.CompositeLit:
		if isExecCmdType(x.Type, names) {
			return "an exec.Cmd literal"
		}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 && isExecCmdType(x.Args[0], names) {
			return "new(exec.Cmd)"
		}
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

// isExecCmdType reports whether e names os/exec's Cmd type.
func isExecCmdType(e ast.Expr, names map[string]string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	local, imported := names["os/exec"]
	return ok && imported && id.Name == local && sel.Sel.Name == "Cmd"
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
