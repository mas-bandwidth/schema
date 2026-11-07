// The tidy pass: the emitter writes each construct as a self-contained
// fragment — a bare block per construct so its locals cannot collide with
// the next one's, parentheses around every interpolated operand so no
// precedence can surprise it — and this pass rewrites the parsed file into
// the code a person would have written: no bare blocks and no redundant
// parentheses. It moves no semantics and no wire bit: a bare block's locals
// are hoisted into the enclosing statement list under names nothing else in
// the function uses, and a parenthesis is dropped only where the grammar or
// the operator precedence already binds the same way.
package golang

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"
)

// tidy parses one generated file, flattens the bare blocks of every function
// body, drops redundant parentheses and prints the result gofmt-clean.
func tidy(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	info := resolve(fset, file)
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		flattenFunc(fset, info, fn)
		simplifyStmts(fn.Body.List)
	}
	var out bytes.Buffer
	if err := format.Node(&out, fset, file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// resolve binds every identifier of file to the object it declares or uses,
// with go/types: the flatten needs to know which spellings are one local, and
// a type checker answers that exactly where go/parser's syntactic resolution
// only guessed. The file is one of several in its package and imports the
// runtime, so the check runs without the rest: imports resolve to empty
// packages, and the errors that leaves (names from sibling files and imported
// packages) are expected and ignored. Function-local objects, the only ones
// the flatten renames, never depend on them.
func resolve(fset *token.FileSet, file *ast.File) *types.Info {
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{
		Importer: emptyImporter{},
		Error:    func(error) {},
	}
	_, _ = conf.Check(file.Name.Name, fset, []*ast.File{file}, info)
	return info
}

// emptyImporter hands the checker an empty, complete package for every import.
type emptyImporter struct{}

func (emptyImporter) Import(p string) (*types.Package, error) {
	name := path.Base(p)
	if i := strings.IndexFunc(name, func(r rune) bool { return r == '.' || r == '-' }); i > 0 {
		name = name[:i]
	}
	pkg := types.NewPackage(p, name)
	pkg.MarkComplete()
	return pkg, nil
}

// localVar is the function-local variable id declares or uses, or nil: a
// blank identifier, a struct field and anything that is not a variable are
// never renamed.
func localVar(info *types.Info, id *ast.Ident) *types.Var {
	if id.Name == "_" {
		return nil
	}
	obj := info.Defs[id]
	if obj == nil {
		obj = info.Uses[id]
	}
	v, ok := obj.(*types.Var)
	if !ok || v.IsField() {
		return nil
	}
	return v
}

// ---- bare blocks -----------------------------------------------------------

// flattener hoists the bare blocks of one function body.
type flattener struct {
	fset *token.FileSet
	info *types.Info
	// taken holds every identifier name the function spells, and every name
	// handed out so far: a hoisted local takes a name in none of them, so it
	// can neither collide with nor shadow anything
	taken map[string]bool
	// fixed holds the names of objects that are NOT hoisted (parameters,
	// loop variables, `if err :=`), which a hoisted local never keeps
	fixed map[string]bool
	// kept holds the names a hoisted local already kept
	kept map[string]bool
	// names holds a hoisted local's name once it is renamed: a types.Object's
	// name is fixed, so the rename is recorded here
	names map[*types.Var]string
}

func flattenFunc(fset *token.FileSet, info *types.Info, fn *ast.FuncDecl) {
	f := &flattener{fset: fset, info: info, taken: map[string]bool{}, fixed: map[string]bool{}, kept: map[string]bool{}, names: map[*types.Var]string{}}
	ast.Inspect(fn, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			f.taken[id.Name] = true
		}
		return true
	})
	// every object declared outside a bare block is fixed
	hoisted := map[*types.Var]bool{}
	f.collectBare(fn.Body.List, hoisted)
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if v := localVar(info, n); v != nil && info.Defs[n] == v && !hoisted[v] {
				f.fixed[n.Name] = true
			}
		case *ast.TypeSwitchStmt:
			// the symbolic variable of `switch x := v.(type)` declares no
			// object of its own (each clause gets an implicit one)
			if a, ok := n.Assign.(*ast.AssignStmt); ok {
				for _, l := range a.Lhs {
					if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
						f.fixed[id.Name] = true
					}
				}
			}
		}
		return true
	})
	fn.Body.List = f.list(fn.Body.List)
}

// collectBare records every object declared directly in a bare block
// anywhere under list: those are the objects a flatten hoists. It walks the
// statement lists flattener.list and flattener.nested walk.
func (f *flattener) collectBare(list []ast.Stmt, hoisted map[*types.Var]bool) {
	for _, s := range list {
		switch s := s.(type) {
		case *ast.BlockStmt:
			for _, v := range f.directObjects(s.List) {
				hoisted[v] = true
			}
			f.collectBare(s.List, hoisted)
		case *ast.IfStmt:
			f.collectBare(s.Body.List, hoisted)
			switch e := s.Else.(type) {
			case *ast.BlockStmt: // an else body, not a bare block
				f.collectBare(e.List, hoisted)
			case *ast.IfStmt:
				f.collectBare([]ast.Stmt{e}, hoisted)
			}
		case *ast.ForStmt:
			f.collectBare(s.Body.List, hoisted)
		case *ast.RangeStmt:
			f.collectBare(s.Body.List, hoisted)
		case *ast.SwitchStmt:
			for _, c := range s.Body.List {
				f.collectBare(c.(*ast.CaseClause).Body, hoisted)
			}
		}
	}
}

// directObjects returns the objects declared by the statements of list
// themselves (`x := …`, `var x …`), not by anything nested in them.
func (f *flattener) directObjects(list []ast.Stmt) []*types.Var {
	var out []*types.Var
	for _, s := range list {
		switch s := s.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				continue
			}
			for _, l := range s.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					if v := localVar(f.info, id); v != nil && f.info.Defs[id] == v {
						out = append(out, v)
					}
				}
			}
		case *ast.DeclStmt:
			gd, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, id := range vs.Names {
						if v := localVar(f.info, id); v != nil {
							out = append(out, v)
						}
					}
				}
			}
		}
	}
	return out
}

// list flattens one statement list: each bare block's statements are spliced
// in place of the block, its locals renamed where they must be, and every
// nested body is flattened in turn.
func (f *flattener) list(in []ast.Stmt) []ast.Stmt {
	var out []ast.Stmt
	for _, s := range in {
		if b, ok := s.(*ast.BlockStmt); ok {
			for _, v := range f.directObjects(b.List) {
				f.hoist(b, v)
			}
			f.closeBraces(b)
			out = append(out, f.list(b.List)...)
			continue
		}
		f.nested(s)
		out = append(out, s)
	}
	return out
}

// closeBraces folds the lines a removed block's braces stood on into their
// neighbours, so the printer sees the spliced statements as adjacent and opens
// no blank line where a brace was. The emitter never writes anything after a
// brace on its line, so no comment is folded into code.
func (f *flattener) closeBraces(b *ast.BlockStmt) {
	file := f.fset.File(b.Lbrace)
	if len(b.List) == 0 {
		return
	}
	if r := file.Line(b.Rbrace); r > 1 {
		file.MergeLine(r - 1)
	}
	if l := file.Line(b.Lbrace); l < file.LineCount() {
		file.MergeLine(l)
	}
}

// nested flattens the bodies a statement owns.
func (f *flattener) nested(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.IfStmt:
		s.Body.List = f.list(s.Body.List)
		if s.Else != nil {
			f.nested(s.Else)
		}
	case *ast.BlockStmt: // an else block
		s.List = f.list(s.List)
	case *ast.ForStmt:
		s.Body.List = f.list(s.Body.List)
	case *ast.RangeStmt:
		s.Body.List = f.list(s.Body.List)
	case *ast.SwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			cc.Body = f.list(cc.Body)
		}
	case *ast.TypeSwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			cc.Body = f.list(cc.Body)
		}
	}
}

// hoist gives a hoisted object its final name: its own, unless a fixed object
// or an earlier hoisted one already spells it, and then the next free name in
// its family (f0 -> f5 for a numbered local, offsetValue -> offsetValue2).
func (f *flattener) hoist(b *ast.BlockStmt, obj *types.Var) {
	old := f.names[obj]
	if old == "" {
		old = obj.Name()
	}
	name := old
	if !f.fixed[name] && !f.kept[name] {
		f.kept[name] = true
		return
	}
	stem, n := splitNumber(name)
	for {
		n++
		candidate := stem + strconv.Itoa(n)
		if !f.taken[candidate] {
			name = candidate
			break
		}
	}
	f.taken[name] = true
	f.kept[name] = true
	ast.Inspect(b, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Name == old && localVar(f.info, id) == obj {
			id.Name = name
		}
		return true
	})
	f.names[obj] = name
}

// splitNumber splits a trailing decimal off a name: f0 -> (f, 0), and a name
// without one starts its family at 1 so the first rename is name2.
func splitNumber(name string) (string, int) {
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	if i == len(name) || i == 0 {
		return name, 1
	}
	n, err := strconv.Atoi(name[i:])
	if err != nil {
		return name, 1
	}
	return name[:i], n
}

// ---- parentheses -----------------------------------------------------------

// binding is the context an expression sits in: free (an operand of nothing),
// the operand of a binary operator of some precedence, or the operand of a
// unary operator or a selector, which binds tighter than any binary one.
type binding int

const (
	free    binding = 0
	primary binding = 100
)

func simplifyStmts(list []ast.Stmt) {
	for _, s := range list {
		simplifyStmt(s)
	}
}

func simplifyStmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.AssignStmt:
		for i := range s.Lhs {
			s.Lhs[i] = simplify(s.Lhs[i], free)
		}
		for i := range s.Rhs {
			s.Rhs[i] = simplify(s.Rhs[i], free)
		}
	case *ast.ExprStmt:
		s.X = simplify(s.X, free)
	case *ast.ReturnStmt:
		for i := range s.Results {
			s.Results[i] = simplify(s.Results[i], free)
		}
	case *ast.IncDecStmt:
		s.X = simplify(s.X, free)
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for i := range vs.Values {
						vs.Values[i] = simplify(vs.Values[i], free)
					}
				}
			}
		}
	case *ast.IfStmt:
		if s.Init != nil {
			simplifyStmt(s.Init)
		}
		s.Cond = simplify(s.Cond, free)
		simplifyStmts(s.Body.List)
		if s.Else != nil {
			simplifyStmt(s.Else)
		}
	case *ast.BlockStmt:
		simplifyStmts(s.List)
	case *ast.ForStmt:
		if s.Init != nil {
			simplifyStmt(s.Init)
		}
		if s.Cond != nil {
			s.Cond = simplify(s.Cond, free)
		}
		if s.Post != nil {
			simplifyStmt(s.Post)
		}
		simplifyStmts(s.Body.List)
	case *ast.RangeStmt:
		s.X = simplify(s.X, free)
		simplifyStmts(s.Body.List)
	case *ast.SwitchStmt:
		if s.Init != nil {
			simplifyStmt(s.Init)
		}
		if s.Tag != nil {
			s.Tag = simplify(s.Tag, free)
		}
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			for i := range cc.List {
				cc.List[i] = simplify(cc.List[i], free)
			}
			simplifyStmts(cc.Body)
		}
	}
}

// simplify drops the parentheses e does not need in context ctx, depth first.
func simplify(e ast.Expr, ctx binding) ast.Expr {
	switch x := e.(type) {
	case *ast.ParenExpr:
		inner := simplify(x.X, free)
		switch in := inner.(type) {
		case *ast.Ident, *ast.BasicLit, *ast.CallExpr, *ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr:
			return inner
		case *ast.BinaryExpr:
			if ctx == free || (ctx != primary && in.Op.Precedence() > int(ctx)) {
				return inner
			}
		case *ast.UnaryExpr:
			if ctx == free {
				return inner
			}
		}
		x.X = inner
		return x
	case *ast.BinaryExpr:
		p := binding(x.Op.Precedence())
		x.X = simplify(x.X, p)
		x.Y = simplify(x.Y, p)
	case *ast.UnaryExpr:
		x.X = simplify(x.X, primary)
	case *ast.StarExpr:
		x.X = simplify(x.X, primary)
	case *ast.CallExpr:
		// a parenthesized Fun is a conversion like (*T)(x): left alone
		for i := range x.Args {
			x.Args[i] = simplify(x.Args[i], free)
		}
	case *ast.IndexExpr:
		x.X = simplify(x.X, primary)
		x.Index = simplify(x.Index, free)
	case *ast.SliceExpr:
		x.X = simplify(x.X, primary)
		for _, p := range []*ast.Expr{&x.Low, &x.High, &x.Max} {
			if *p != nil {
				*p = simplify(*p, free)
			}
		}
	case *ast.SelectorExpr:
		x.X = simplify(x.X, primary)
	case *ast.KeyValueExpr:
		x.Value = simplify(x.Value, free)
	case *ast.CompositeLit:
		for i := range x.Elts {
			x.Elts[i] = simplify(x.Elts[i], free)
		}
	}
	return e
}
