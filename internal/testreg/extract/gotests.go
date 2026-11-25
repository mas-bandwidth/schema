package extract

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// goPkg is one directory's test files: an in-package and an external test
// package in one directory are one go test run, so one place.
type goPkg struct {
	dir   string
	fset  *token.FileSet
	files map[*ast.File]string // file -> path from the root
	funcs map[string]*ast.FuncDecl
	build map[*ast.File]string
	src   map[string][]byte // file -> its bytes
}

// goTests finds every Go test, with its gates.
func (s *scan) goTests(files []string) {
	byDir := map[string][]string{}
	for _, f := range files {
		d := filepath.ToSlash(filepath.Dir(f))
		byDir[d] = append(byDir[d], f)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		p := &goPkg{dir: d, fset: token.NewFileSet(), files: map[*ast.File]string{}, funcs: map[string]*ast.FuncDecl{}, build: map[*ast.File]string{}, src: map[string][]byte{}}
		for _, rel := range byDir[d] {
			src, err := os.ReadFile(filepath.Join(s.root, rel))
			if err != nil {
				s.fail(rel, "read: %v", err)
				continue
			}
			f, err := parser.ParseFile(p.fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				s.fail(rel, "parse: %v", err)
				continue
			}
			p.files[f] = rel
			p.src[rel] = src
			s.goStrings(d, f)
			p.build[f] = buildLine(src)
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
					// two files may define one helper under different build
					// constraints; either answers a call
					if _, dup := p.funcs[fn.Name.Name]; !dup {
						p.funcs[fn.Name.Name] = fn
					}
				}
			}
		}
		s.goPkgTests(p)
	}
	s.goSourceStrings()
}

func (s *scan) goPkgTests(p *goPkg) {
	type entry struct {
		fn   *ast.FuncDecl
		file *ast.File
	}
	var entries []entry
	for f := range p.files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && isTestEntry(fn) {
				entries = append(entries, entry{fn, f})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].fn.Name.Name < entries[j].fn.Name.Name })
	for _, e := range entries {
		var gates []string
		if b := p.build[e.file]; b != "" {
			gates = append(gates, "build: "+b)
		}
		skips := p.reach(e.fn)
		for _, sk := range skips {
			gates = append(gates, "skip: "+sk)
		}
		s.add(Item{Kind: KindGoTest, Dir: p.dir, Name: e.fn.Name.Name, Source: p.pos(e.fn.Pos()), Gates: gates})
	}
}

// isTestEntry is a function go test runs: TestX(*testing.T),
// FuzzX(*testing.F), BenchmarkX(*testing.B), where X does not start with a
// lower-case letter.
func isTestEntry(fn *ast.FuncDecl) bool {
	n := fn.Name.Name
	for prefix, param := range map[string]string{"Test": "T", "Fuzz": "F", "Benchmark": "B"} {
		if !strings.HasPrefix(n, prefix) || n == "TestMain" {
			continue
		}
		rest := n[len(prefix):]
		if r, _ := utf8.DecodeRuneInString(rest); rest != "" && unicode.IsLower(r) {
			continue
		}
		params := fn.Type.Params.List
		if len(params) != 1 || fn.Type.Results != nil {
			continue
		}
		star, ok := params[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if ok && identIs(sel.X, "testing") && sel.Sel.Name == param {
			return true
		}
	}
	return false
}

func identIs(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func (p *goPkg) pos(pos token.Pos) string {
	at := p.fset.Position(pos)
	return at.Filename + ":" + strconv.Itoa(at.Line)
}

// reach returns every Skip fn reaches, itself or through a package function
// it calls (transitively), each as
// "file:line" with the condition of the if around it.
func (p *goPkg) reach(fn *ast.FuncDecl) []string {
	seen := map[*ast.FuncDecl]bool{}
	var skips []string
	var visit func(*ast.FuncDecl)
	visit = func(f *ast.FuncDecl) {
		if seen[f] {
			return
		}
		seen[f] = true
		var stack []ast.Node
		ast.Inspect(f.Body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			switch x := n.(type) {
			case *ast.CallExpr:
				switch fun := x.Fun.(type) {
				case *ast.SelectorExpr:
					switch fun.Sel.Name {
					case "Skip", "SkipNow", "Skipf":
						skips = append(skips, p.pos(x.Pos())+p.cond(stack))
					}
				case *ast.Ident:
					if callee, ok := p.funcs[fun.Name]; ok {
						visit(callee)
					}
				}
			case *ast.Ident:
				// a helper passed as a value (t.Run(name, helper)) runs too
				if callee, ok := p.funcs[x.Name]; ok && callee != f {
					visit(callee)
				}
			}
			return true
		})
	}
	visit(fn)
	sort.Strings(skips)
	skips = slices.Compact(skips)
	return skips
}

// cond is " if <condition>" for the innermost if whose body holds the call.
func (p *goPkg) cond(stack []ast.Node) string {
	for i := len(stack) - 1; i > 0; i-- {
		if ifs, ok := stack[i-1].(*ast.IfStmt); ok && stack[i] == ifs.Body {
			start, end := p.fset.Position(ifs.Cond.Pos()), p.fset.Position(ifs.Cond.End())
			src := p.src[start.Filename]
			if start.Offset < end.Offset && end.Offset <= len(src) {
				return " if " + strings.Join(strings.Fields(string(src[start.Offset:end.Offset])), " ")
			}
		}
	}
	return ""
}

// buildLine is the file's //go:build constraint, or "".
func buildLine(src []byte) string {
	for line := range bytes.Lines(src) {
		t := strings.TrimSpace(string(line))
		if strings.HasPrefix(t, "package ") {
			break
		}
		if c, ok := strings.CutPrefix(t, "//go:build "); ok {
			return strings.TrimSpace(c)
		}
	}
	return ""
}

// goStrings records every string literal of a parsed _test.go file under its
// package's directory, and the packages of this module the file imports.
func (s *scan) goStrings(dir string, f *ast.File) {
	if s.res.GoStrings == nil {
		s.res.GoStrings = map[string]map[string]bool{}
	}
	set := s.res.GoStrings[dir]
	if set == nil {
		set = map[string]bool{}
		s.res.GoStrings[dir] = set
	}
	addLiterals(set, f)
	if s.imports == nil {
		s.imports = map[string]map[string]bool{}
	}
	if s.imports[dir] == nil {
		s.imports[dir] = map[string]bool{}
	}
	for _, im := range f.Imports {
		if v, err := strconv.Unquote(im.Path.Value); err == nil {
			s.imports[dir][v] = true
		}
	}
}

// addLiterals records f's string literals, and the path each run of
// string literals side by side in a filepath.Join or path.Join call makes,
// as one literal: filepath.Join("..", "testdata", "x") names ../testdata/x,
// and filepath.Join(root, "testdata", "x", name) names testdata/x.
func addLiterals(set map[string]bool, f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BasicLit:
			if v, ok := stringLit(n); ok {
				set[v] = true
			}
		case *ast.CallExpr:
			for _, v := range literalJoins(n) {
				set[v] = true
			}
		}
		return true
	})
}

// stringLit is a string literal's value.
func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(bl.Value)
	return v, err == nil
}

// literalJoins is the path each run of two or more string literals side
// by side makes in a filepath.Join or path.Join call.
func literalJoins(c *ast.CallExpr) []string {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Join" {
		return nil
	}
	if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "filepath" && x.Name != "path" {
		return nil
	}
	var out, run []string
	flush := func() {
		if len(run) > 1 {
			out = append(out, path.Join(run...))
		}
		run = nil
	}
	for _, a := range c.Args {
		if v, ok := stringLit(a); ok {
			run = append(run, v)
			continue
		}
		flush()
	}
	flush()
	return out
}

// goSourceStrings adds to each test package's strings those of its own
// non-test files and of the non-test files of every package of this module
// its tests import: a test that reads its corpus through a helper package
// (internal/nameemit's corpus directory) names it there.
func (s *scan) goSourceStrings() {
	mod := s.modulePath()
	cache := map[string]map[string]bool{}
	of := func(dir string) map[string]bool {
		if got, ok := cache[dir]; ok {
			return got
		}
		set := map[string]bool{}
		cache[dir] = set
		ents, err := os.ReadDir(filepath.Join(s.root, filepath.FromSlash(dir)))
		if err != nil {
			return set // an import outside this tree (a module of its own) names nothing here
		}
		fset := token.NewFileSet()
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			rel := path.Join(dir, n)
			src, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(rel)))
			if err != nil {
				s.fail(rel, "read: %v", err)
				continue
			}
			f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
			if err != nil {
				s.fail(rel, "parse: %v", err)
				continue
			}
			addLiterals(set, f)
		}
		return set
	}
	for dir, set := range s.res.GoStrings {
		dirs := []string{dir}
		for im := range s.imports[dir] {
			if rest, ok := strings.CutPrefix(im, mod+"/"); ok && mod != "" {
				dirs = append(dirs, rest)
			}
		}
		for _, d := range dirs {
			for l := range of(d) {
				set[l] = true
			}
		}
	}
}

// modulePath is go.mod's module path ("" when it names none).
func (s *scan) modulePath() string {
	data, err := os.ReadFile(filepath.Join(s.root, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(data)) {
		if m, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(m), `"`)
		}
	}
	return ""
}
