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
	"unicode"

	"github.com/mas-bandwidth/schema/v3/internal/execref"
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

// Registrars are the functions of this package that register inputs: Files
// records the named paths, Make and Build record what a make run reads. Any
// other call into the package (Root only finds the repository root) records
// nothing, so it does not count.
var Registrars = map[string]bool{"Files": true, "Make": true, "Build": true}

// NoneDirective, on the line of a child's start or the line above it,
// followed by a separator and a reason of at least MinReasonWords words, says
// the child reads no repository file (a shell running `exit 0`, a script the
// test wrote into its own temporary directory).
const NoneDirective = "testinputs:none"

// MinReasonWords is the fewest words a NoneDirective's reason may have.
const MinReasonWords = 3

// hasReason reports whether rest, the text after NoneDirective, is a
// separator (a dash, a colon, spaces) and then a reason: at least
// MinReasonWords words that each hold a letter or a digit, so punctuation
// alone, or one word, is not one. A name that merely starts with the
// directive (`testinputs:nonexistent ...`) is not the directive.
func hasReason(rest string) bool {
	rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "*/"))
	if rest == "" {
		return false
	}
	if first := []rune(rest)[0]; unicode.IsLetter(first) || unicode.IsDigit(first) {
		return false
	}
	words := 0
	for w := range strings.FieldsSeq(rest) {
		if strings.IndexFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			words++
		}
	}
	return words >= MinReasonWords
}

// Unregistered is one start of an interpreter child, in a function that never
// registers inputs with this package and carries no NoneDirective.
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
// or the line above. A call counts as registering only when it is one of the
// Registrars; testinputs.Root alone does not. A start whose command and arguments are all computed
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
			if rest, ok := strings.CutPrefix(text, NoneDirective); ok && hasReason(rest) {
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

// callsPackage reports whether fn calls a Registrar of the package imported
// under local.
func callsPackage(fn *ast.FuncDecl, local string) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == local && Registrars[sel.Sel.Name] {
				found = true
			}
		}
		return !found
	})
	return found
}

// GoTestFiles returns every _test.go file of this module under root, relative
// to it with forward slashes: nested modules (a directory with its own
// go.mod), testdata, and hidden and build directories are left out, since
// they cannot import this package.
func GoTestFiles(root string) ([]string, error) {
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
