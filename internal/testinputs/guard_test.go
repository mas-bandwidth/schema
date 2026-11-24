package testinputs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/execref"
)

// ImportPath is this package's import path.
const ImportPath = modulePath + "/internal/testinputs"

// testprocPath is the bounded child starter most tests use in place of os/exec.
const testprocPath = modulePath + "/internal/testproc"

// Interpreters are the commands whose child reads repository files go test
// cannot see: a shell, make, or a script interpreter, named by the command or
// by an argument of a wrapper (`pgroup -- sh -c ...`).
var Interpreters = map[string]bool{
	"sh": true, "bash": true, "dash": true, "zsh": true,
	"make": true, "gmake": true,
	"python": true, "python3": true,
	"node": true, "perl": true,
}

// NoneDirective, on the line of a child's start or the line above it,
// followed by a reason, says the child reads no repository file (a shell
// running `exit 0`, a script the test wrote into its own temporary
// directory).
const NoneDirective = "testinputs:none"

// Unregistered is one start of an interpreter child, in a function that never
// calls this package and carries no NoneDirective.
type Unregistered struct {
	Pos     token.Position
	Func    string
	Command string // the interpreter named
}

func (u Unregistered) String() string {
	return u.Pos.Filename + ":" + strconv.Itoa(u.Pos.Line) + ": " + u.Func + " starts " + strconv.Quote(u.Command)
}

// Check returns every start of an interpreter child in one _test.go file that
// is not registered. A start is os/exec's or internal/testproc's Command or
// CommandContext, found by what the name binds to (internal/execref), whose
// command, or one of whose arguments, is a string literal naming one of the
// Interpreters. It is registered when the top-level function holding it
// calls this package, or when a NoneDirective with a reason sits on its line
// or the line above. A start whose command and arguments are all computed
// (a variable holding "sh") is not seen.
func Check(filename string, src []byte) ([]Unregistered, error) {
	var refs []execref.Ref
	for _, pkg := range []string{"os/exec", testprocPath} {
		r, err := execref.Find(token.NewFileSet(), filename, src, pkg, "Command", "CommandContext")
		if err != nil {
			return nil, err
		}
		for i := range r {
			r[i].Name = pkg + "." + r[i].Name
		}
		refs = append(refs, r...)
	}
	if len(refs) == 0 {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	calls := map[int]*ast.CallExpr{} // by the offset of the callee's name
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			switch fun := ast.Unparen(call.Fun).(type) {
			case *ast.Ident:
				calls[fset.Position(fun.Pos()).Offset] = call
			case *ast.SelectorExpr:
				calls[fset.Position(fun.Sel.Pos()).Offset] = call
			}
		}
		return true
	})
	none := map[int]bool{} // lines carrying a NoneDirective with a reason
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
			if rest, ok := strings.CutPrefix(text, NoneDirective); ok && strings.TrimSpace(strings.TrimSuffix(rest, "*/")) != "" {
				none[fset.Position(c.Pos()).Line] = true
			}
		}
	}
	local := localName(f)
	var bad []Unregistered
	for _, r := range refs {
		call := calls[r.Pos.Offset]
		if call == nil {
			continue // taken as a value, not called here
		}
		skip := 0
		if strings.HasPrefix(r.Name, testprocPath) {
			skip++ // the test
		}
		if strings.HasSuffix(r.Name, "CommandContext") {
			skip++ // the context
		}
		command := interpreter(call.Args, skip)
		if command == "" {
			continue
		}
		line := fset.Position(call.Pos()).Line
		if none[line] || none[line-1] {
			continue
		}
		fn := enclosing(f, call.Pos())
		if fn != nil && local != "" && callsPackage(fn, local) {
			continue
		}
		name := "(package level)"
		if fn != nil {
			name = fn.Name.Name
		}
		pos := fset.Position(call.Pos())
		pos.Filename = filename
		bad = append(bad, Unregistered{Pos: pos, Func: name, Command: command})
	}
	return bad, nil
}

// interpreter returns the first string literal among args[skip:] that names
// one of the Interpreters, the command or a wrapper's argument, or "".
func interpreter(args []ast.Expr, skip int) string {
	for i, a := range args {
		if i < skip {
			continue
		}
		lit, ok := ast.Unparen(a).(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		s, err := strconv.Unquote(lit.Value)
		if err == nil && Interpreters[s] {
			return s
		}
	}
	return ""
}

// localName is the name the file imports this package under, or "".
func localName(f *ast.File) string {
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err != nil || p != ImportPath {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				return ""
			}
			return imp.Name.Name
		}
		return "testinputs"
	}
	return ""
}

// enclosing is the top-level function declaration holding pos, or nil.
func enclosing(f *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Pos() <= pos && pos < fn.End() {
			return fn
		}
	}
	return nil
}

// callsPackage reports whether fn calls a function of the package imported
// under local.
func callsPackage(fn *ast.FuncDecl, local string) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == local {
				found = true
			}
		}
		return !found
	})
	return found
}

// goTestFiles returns every _test.go file of this module under root, relative
// to it with forward slashes: nested modules (a directory with its own
// go.mod), testdata, and hidden and build directories are left out, since
// they cannot import this package.
func goTestFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			n := d.Name()
			if n == "testdata" || n == "build" || n == "node_modules" || n == "vendor" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out, err
}

// TestEveryInterpreterChildRegistersItsInputs holds every _test.go file of
// the module to this package: a test that starts sh, bash, make, python or
// node (or a wrapper running one) calls testinputs in the same function, or
// says on the start's line or the line above why the child reads no
// repository file. Without it, go test replays a cached PASS after only the
// script changed.
func TestEveryInterpreterChildRegistersItsInputs(t *testing.T) {
	t.Parallel()
	root := Root(t)
	files, err := goTestFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 100 {
		t.Fatalf("read %d _test.go files under %s; the walk is broken, and judging none proves nothing", len(files), root)
	}
	starts := 0
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		bad, err := Check(rel, src)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for _, b := range bad {
			starts++
			t.Errorf("%s: its inputs are not registered; call testinputs.Files (or testinputs.Make) in %s on every repository file the child reads, or put %q and the reason on the line above when it reads none", b, b.Func, "// "+NoneDirective)
		}
	}
	if starts > 0 {
		t.Logf("%d unregistered interpreter start(s) in %d files", starts, len(files))
	}
}

// TestEveryInterpreterChildRegistersItsInputsPlants plants each form the
// guard must refuse and each it must accept, and checks the reader's verdict
// on every one.
func TestEveryInterpreterChildRegistersItsInputsPlants(t *testing.T) {
	t.Parallel()
	const head = "package p\nimport (\n\"context\"\n\"os/exec\"\n\"testing\"\n\"github.com/mas-bandwidth/schema/v2/internal/testproc\"\n\"github.com/mas-bandwidth/schema/v2/internal/testinputs\"\n)\nvar _ = context.Background\nvar _ = testinputs.Files\nvar _ = testproc.Command\n"
	cases := []struct {
		name, body string
		want       int
	}{
		{"sh through os/exec", "func TestA(t *testing.T) { exec.Command(\"sh\", \"make/x.sh\").Run() }", 1},
		{"bash through CommandContext", "func TestA(t *testing.T) { exec.CommandContext(context.Background(), \"bash\", \"x.sh\").Run() }", 1},
		{"make through testproc", "func TestA(t *testing.T) { testproc.Command(t, \"make\", \"-n\", \"x\").Run() }", 1},
		{"python3 through testproc CommandContext", "func TestA(t *testing.T) { testproc.CommandContext(t, context.Background(), \"python3\", \"tools/x.py\").Run() }", 1},
		{"node", "func TestA(t *testing.T) { exec.Command(\"node\", \"x.js\").Run() }", 1},
		{"wrapper running sh", "func TestA(t *testing.T, bin string) { exec.Command(bin, \"--\", \"sh\", \"x.sh\").Run() }", 1},
		{"registered in another function only", "func reg(t *testing.T) { testinputs.Files(t, \"x.sh\") }\nfunc TestA(t *testing.T) { exec.Command(\"sh\", \"x.sh\").Run() }", 1},
		{"directive with no reason", "func TestA(t *testing.T) {\n// testinputs:none\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 1},
		{"registered", "func TestA(t *testing.T) { testinputs.Files(t, \"make/x.sh\"); exec.Command(\"sh\", \"make/x.sh\").Run() }", 0},
		{"registered through Make", "func TestA(t *testing.T) { testinputs.Make(t); testproc.Command(t, \"make\", \"x\").Run() }", 0},
		{"directive with a reason", "func TestA(t *testing.T) {\n// testinputs:none — runs `exit 0`, reads no file\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() }", 0},
		{"directive on the line", "func TestA(t *testing.T) {\nexec.Command(\"sh\", \"-c\", \"exit 0\").Run() // testinputs:none — reads no file\n}", 0},
		{"a compiled binary", "func TestA(t *testing.T, bin string) { exec.Command(bin, \"-x\").Run() }", 0},
		{"go, not an interpreter", "func TestA(t *testing.T) { exec.Command(\"go\", \"vet\").Run() }", 0},
	}
	refused := 0
	for _, c := range cases {
		bad, err := Check(c.name+"_test.go", []byte(head+c.body+"\n"))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(bad) != c.want {
			t.Errorf("%s: the guard refused %d start(s) %v, want %d", c.name, len(bad), bad, c.want)
		}
		if c.want > 0 {
			refused++
		}
	}
	if refused != 8 {
		t.Fatalf("planted %d refused forms, want 8: a guard that refuses nothing would pass", refused)
	}
	// a dot import of os/exec is resolved by binding, not spelling
	dot := "package p\nimport (\n. \"os/exec\"\n\"testing\"\n)\nfunc TestA(t *testing.T) { Command(\"sh\", \"x.sh\").Run() }\n"
	if bad, err := Check("dot_test.go", []byte(dot)); err != nil || len(bad) != 1 {
		t.Errorf("dot import: the guard refused %v (err %v), want 1", bad, err)
	}
}
