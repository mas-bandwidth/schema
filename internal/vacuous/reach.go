package vacuous

import (
	"go/ast"
	"go/token"
	"go/types"
)

// failMethods are the testing.TB methods that mark a test failed.
var failMethods = map[string]bool{
	"Error": true, "Errorf": true, "Fatal": true, "Fatalf": true, "Fail": true, "FailNow": true,
}

// A scope is one top-level function seen as a unit: its file, the closures it
// binds to names, and the names of its testing parameters.
type scope struct {
	file     *srcFile
	decl     *ast.FuncDecl
	closures map[string]*ast.FuncLit
	tnames   map[string]bool
}

type analyzer struct {
	strict bool // count only t.Error and friends: no panic, no exit, no unseen callee
	t      *tree
	memo   map[*ast.FuncDecl]int // 0 unknown, 1 in progress, 2 cannot fail, 3 can fail
	scopes map[*ast.FuncDecl]*scope
	lits   map[*ast.FuncLit]int
}

func newAnalyzer(t *tree) *analyzer {
	return &analyzer{t: t, memo: map[*ast.FuncDecl]int{}, scopes: map[*ast.FuncDecl]*scope{}, lits: map[*ast.FuncLit]int{}}
}

func isTestingType(e ast.Expr) bool {
	if st, ok := e.(*ast.StarExpr); ok {
		e = st.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch x.Name + "." + sel.Sel.Name {
	case "testing.T", "testing.B", "testing.F", "testing.TB", "rapid.T", "rapid.TB":
		return true
	}
	return false
}

func (a *analyzer) scopeOf(d *ast.FuncDecl) *scope {
	if s, ok := a.scopes[d]; ok {
		return s
	}
	s := &scope{file: a.t.declAt[d], decl: d, closures: map[string]*ast.FuncLit{}, tnames: map[string]bool{"t": true, "tb": true}}
	a.scopes[d] = s
	addParams := func(ft *ast.FuncType) {
		if ft.Params == nil {
			return
		}
		for _, f := range ft.Params.List {
			if isTestingType(f.Type) {
				for _, n := range f.Names {
					s.tnames[n.Name] = true
				}
			}
		}
	}
	addParams(d.Type)
	if d.Body == nil {
		return s
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			addParams(n.Type)
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(n.Rhs) {
					continue
				}
				if lit, ok := n.Rhs[i].(*ast.FuncLit); ok {
					s.closures[id.Name] = lit
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				if i < len(n.Values) {
					if lit, ok := n.Values[i].(*ast.FuncLit); ok {
						s.closures[id.Name] = lit
					}
				}
			}
		}
		return true
	})
	return s
}

// declCanFail reports whether a call of d can mark a test failed, through any
// chain of helpers in this tree.
func (a *analyzer) declCanFail(d *ast.FuncDecl) bool {
	switch a.memo[d] {
	case 1, 2:
		return false
	case 3:
		return true
	}
	if d.Body == nil || a.t.declAt[d] == nil {
		a.memo[d] = 2
		return false
	}
	a.memo[d] = 1
	if a.reach(a.scopeOf(d), d.Body) {
		a.memo[d] = 3
		return true
	}
	a.memo[d] = 2
	return false
}

func (a *analyzer) litCanFail(s *scope, lit *ast.FuncLit) bool {
	switch a.lits[lit] {
	case 1, 2:
		return false
	case 3:
		return true
	}
	a.lits[lit] = 1
	if a.reach(s, lit.Body) {
		a.lits[lit] = 3
		return true
	}
	a.lits[lit] = 2
	return false
}

func (a *analyzer) hasTestingArg(s *scope, call *ast.CallExpr) bool {
	if a.strict {
		return false
	}
	for _, arg := range call.Args {
		if id, ok := arg.(*ast.Ident); ok && s.tnames[id.Name] {
			return true
		}
	}
	return false
}

func rootIdent(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			e = x.X
		case *ast.CallExpr:
			e = x.Fun
		case *ast.IndexExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.TypeAssertExpr:
			e = x.X
		default:
			return nil
		}
	}
}

// directFail reports whether call is itself a failure: t.Error and friends on
// anything that is not an imported package, panic, os.Exit, log.Fatal.
func (a *analyzer) directFail(s *scope, call *ast.CallExpr) bool {
	if a.strict {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if x, ok := sel.X.(*ast.Ident); ok {
			if _, isPkg := s.file.imports[x.Name]; isPkg {
				return false
			}
		}
		return failMethods[sel.Sel.Name]
	}
	return directFail(s, call)
}

func directFail(s *scope, call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == "panic"
	case *ast.SelectorExpr:
		name := fun.Sel.Name
		if x, ok := fun.X.(*ast.Ident); ok {
			if ip, isPkg := s.file.imports[x.Name]; isPkg {
				switch ip {
				case "os":
					return name == "Exit"
				case "log":
					return name == "Fatal" || name == "Fatalf" || name == "Fatalln" || name == "Panic" || name == "Panicf"
				}
				return false
			}
		}
		return failMethods[name]
	}
	return false
}

// reach reports whether any statement under n can fail the test: a direct
// failure, a helper of this tree that can fail, or a callee this tree cannot
// see that is handed the test.
func (a *analyzer) reach(s *scope, n ast.Node) bool {
	if n == nil {
		return false
	}
	found := false
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			if a.directFail(s, n) {
				found = true
				return false
			}
			switch fun := n.Fun.(type) {
			case *ast.Ident:
				if !a.identKnown(s, fun) && fun.Name != "append" && fun.Name != "len" && a.hasTestingArg(s, n) {
					found = true
					return false
				}
			case *ast.SelectorExpr:
				if a.selectorCanFail(s, fun, n) {
					found = true
					return false
				}
				for _, arg := range n.Args {
					ast.Inspect(arg, visit)
				}
				ast.Inspect(fun.X, visit)
				return false
			}
		case *ast.SelectorExpr:
			// a method value or a package function passed as a value
			if a.selectorCanFail(s, n, nil) {
				found = true
				return false
			}
			ast.Inspect(n.X, visit)
			return false
		case *ast.Ident:
			if lit, ok := s.closures[n.Name]; ok {
				if a.litCanFail(s, lit) {
					found = true
				}
				return false
			}
			for _, d := range s.file.pkg.funcs[n.Name] {
				if a.declCanFail(d) {
					found = true
					return false
				}
			}
		}
		return true
	}
	ast.Inspect(n, visit)
	return found
}

func (a *analyzer) identKnown(s *scope, id *ast.Ident) bool {
	if _, ok := s.closures[id.Name]; ok {
		return true
	}
	if _, ok := s.file.pkg.funcs[id.Name]; ok {
		return true
	}
	return types.Universe.Lookup(id.Name) != nil
}

// selectorCanFail handles x.M, as a call (call non-nil) or as a value.
func (a *analyzer) selectorCanFail(s *scope, sel *ast.SelectorExpr, call *ast.CallExpr) bool {
	name := sel.Sel.Name
	if x, ok := sel.X.(*ast.Ident); ok {
		if _, isPkg := s.file.imports[x.Name]; isPkg {
			if p := a.t.imported(s.file, x.Name); p != nil {
				for _, d := range p.funcs[name] {
					if a.declCanFail(d) {
						return true
					}
				}
				return false
			}
			// standard library or third party: it can fail the test only if it is given the test
			return call != nil && a.hasTestingArg(s, call)
		}
		if s.tnames[x.Name] {
			// t.Run, t.Helper, t.Log, f.Fuzz, b.Loop: a method of the test itself that is
			// not a failure. The closures passed to it are walked by the caller.
			return false
		}
	}
	if call == nil {
		// a field read looks the same as a method value; only a call is followed
		return false
	}
	known := false
	for _, d := range s.file.pkg.methods[name] {
		known = true
		if a.declCanFail(d) {
			return true
		}
	}
	for local := range s.file.imports {
		if p := a.t.imported(s.file, local); p != nil {
			for _, d := range p.methods[name] {
				known = true
				if a.declCanFail(d) {
					return true
				}
			}
		}
	}
	if known {
		return false
	}
	// a func-typed field or a method of a type this tree does not declare
	return a.hasTestingArg(s, call)
}

// isTestFunc reports whether d is a Test, Fuzz or Benchmark entry point, and
// which letter.
func isTestFunc(d *ast.FuncDecl) (kind string, ok bool) {
	if d.Recv != nil || d.Type.Params == nil || len(d.Type.Params.List) != 1 || d.Type.Results != nil {
		return "", false
	}
	st, isStar := d.Type.Params.List[0].Type.(*ast.StarExpr)
	if !isStar {
		return "", false
	}
	sel, isSel := st.X.(*ast.SelectorExpr)
	if !isSel {
		return "", false
	}
	x, isIdent := sel.X.(*ast.Ident)
	if !isIdent || x.Name != "testing" {
		return "", false
	}
	for _, c := range []struct{ prefix, typ, kind string }{{"Test", "T", "test"}, {"Fuzz", "F", "fuzz"}, {"Benchmark", "B", "benchmark"}} {
		if len(d.Name.Name) >= len(c.prefix) && d.Name.Name[:len(c.prefix)] == c.prefix && sel.Sel.Name == c.typ {
			rest := d.Name.Name[len(c.prefix):]
			if rest == "" || !(rest[0] >= 'a' && rest[0] <= 'z') {
				return c.kind, true
			}
		}
	}
	return "", false
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

func exprString(e ast.Expr) string { return types.ExprString(e) }

// evidence is a second transitive walk, used by the negative-control rule: it
// reports whether hit holds for any node under n or under any helper of this
// tree that n refers to.
func (a *analyzer) evidence(s *scope, n ast.Node, hit func(s *scope, n ast.Node) bool, memo map[*ast.FuncDecl]bool) bool {
	if n == nil {
		return false
	}
	found := false
	follow := func(decls []*ast.FuncDecl) {
		for _, d := range decls {
			if found {
				return
			}
			if _, seen := memo[d]; seen {
				found = found || memo[d]
				continue
			}
			memo[d] = false
			if d.Body != nil && a.t.declAt[d] != nil && a.evidence(a.scopeOf(d), d.Body, hit, memo) {
				memo[d] = true
				found = true
			}
		}
	}
	ast.Inspect(n, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if hit(s, n) {
			found = true
			return false
		}
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok {
				if _, isPkg := s.file.imports[x.Name]; isPkg {
					if p := a.t.imported(s.file, x.Name); p != nil {
						follow(p.funcs[n.Sel.Name])
					}
					return false
				}
				if s.tnames[x.Name] {
					return false
				}
			}
			follow(s.file.pkg.methods[n.Sel.Name])
		case *ast.Ident:
			follow(s.file.pkg.funcs[n.Name])
		}
		return true
	})
	return found
}

var _ = token.NoPos
