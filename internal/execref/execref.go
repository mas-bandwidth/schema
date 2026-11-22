// Package execref finds, in one Go source file, every reference to named
// functions of an imported package, with each name resolved to what it binds
// to the way the compiler resolves it: not by its spelling.
//
// The child-process policies (internal/ci TestEveryTestChildIsBounded, and
// internal/codegen/goosetable TestGooseTestsStartProcessesThroughGooserun)
// refuse a test that starts a process with os/exec's Command or
// CommandContext. Read by spelling, both missed `import . "os/exec"` then a
// bare Command(...), both refused `exec.Command()` where exec is a local value
// with a method of that name, and the Goose reader missed
// `start := exec.Command` (the reviewer's F-230). Here the file is
// type-checked on its own: the imported package is given the named functions,
// every other import an empty package, and each identifier's binding is read
// from the checker. A reference through a dot import is found, one through a
// local declaration that shadows the import is not, and one taken as a value
// is found like a call.
package execref

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strconv"
)

// Ref is one reference to a named function.
type Ref struct {
	Pos  token.Position
	Name string // the function referred to, e.g. "Command"
	Func string // the top-level function holding the reference, or "(package level)"
	Call bool   // the reference is the callee of a call (else it is taken as a value)
}

// Find returns every reference in the file to one of the functions names of
// the package importPath, in source order. A file that does not parse is an
// error; type errors (the rest of the package and every other import are
// absent) are expected and ignored, since only the bindings are read.
func Find(fset *token.FileSet, filename string, src []byte, importPath string, names ...string) ([]Ref, error) {
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	// a file that does not import the package cannot refer to it
	imported := false
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == importPath {
			imported = true
		}
	}
	if !imported {
		return nil, nil
	}
	target := types.NewPackage(importPath, path.Base(importPath))
	wanted := map[types.Object]string{}
	for _, name := range names {
		params := types.NewTuple(types.NewParam(token.NoPos, target, "args", types.NewSlice(types.NewInterfaceType(nil, nil))))
		fn := types.NewFunc(token.NoPos, target, name, types.NewSignatureType(nil, nil, nil, params, nil, true))
		target.Scope().Insert(fn)
		wanted[fn] = name
	}
	target.MarkComplete()
	others := map[string]*types.Package{}
	conf := types.Config{
		Importer: importerFunc(func(p string) (*types.Package, error) {
			if p == importPath {
				return target, nil
			}
			if pkg, ok := others[p]; ok {
				return pkg, nil
			}
			pkg := types.NewPackage(p, path.Base(p))
			pkg.MarkComplete()
			others[p] = pkg
			return pkg, nil
		}),
		Error: func(error) {}, // the file is checked alone: its errors are expected
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	_, _ = conf.Check(f.Name.Name, fset, []*ast.File{f}, info)

	callees := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			switch fun := ast.Unparen(call.Fun).(type) {
			case *ast.Ident:
				callees[fun] = true
			case *ast.SelectorExpr:
				callees[fun.Sel] = true
			}
		}
		return true
	})
	var refs []Ref
	for id, obj := range info.Uses {
		name, ok := wanted[obj]
		if !ok {
			continue
		}
		refs = append(refs, Ref{Pos: fset.Position(id.Pos()), Name: name, Func: enclosing(f, id.Pos()), Call: callees[id]})
	}
	slices.SortFunc(refs, func(a, b Ref) int { return a.Pos.Offset - b.Pos.Offset })
	return refs, nil
}

// enclosing names the top-level function declaration holding pos.
func enclosing(f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Pos() <= pos && pos < fn.End() {
			return fn.Name.Name
		}
	}
	return "(package level)"
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
