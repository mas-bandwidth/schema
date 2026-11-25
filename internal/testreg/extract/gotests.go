package extract

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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
		slow, short, skips := p.reach(e.fn)
		if slow {
			gates = append(gates, "slow")
		}
		if short {
			gates = append(gates, "short")
		}
		for _, sk := range skips {
			gates = append(gates, "skip: "+sk)
		}
		tier := "unit"
		if slow {
			tier = "functional"
		}
		s.add(Item{Kind: KindGoTest, Dir: p.dir, Name: e.fn.Name.Name, Source: p.pos(e.fn.Pos()), Gates: gates, Tier: tier})
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

// reach returns whether fn, or a package function it calls (transitively),
// opens the slow gate, whether it reads testing.Short(), and every Skip
// reachable that way, each as "file:line" with the condition of the if
// around it.
func (p *goPkg) reach(fn *ast.FuncDecl) (bool, bool, []string) {
	seen := map[*ast.FuncDecl]bool{}
	var skips []string
	slow, short := false, false
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
			case *ast.BasicLit:
				if x.Kind == token.STRING && strings.Contains(x.Value, "SCHEMA_SLOW") {
					slow = true
				}
			case *ast.CallExpr:
				switch fun := x.Fun.(type) {
				case *ast.SelectorExpr:
					if identIs(fun.X, "slowtest") && (fun.Sel.Name == "Gate" || fun.Sel.Name == "Enabled") {
						slow = true
					}
					if identIs(fun.X, "testing") && fun.Sel.Name == "Short" {
						short = true
					}
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
	return slow, short, skips
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
