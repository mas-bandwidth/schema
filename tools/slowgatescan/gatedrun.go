package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// gatedRunPattern returns an anchored -run pattern selecting every
// top-level test in a gated package that reaches slowtest.Gate, directly
// or through a helper in its own package or an imported runner package
// (such as sqrun). The call graph is package-scoped, so a helper named
// like a helper in another package cannot pull unrelated tests in. The
// coverage transcript only needs the gated tests to attempt and skip;
// every other test in these packages already runs in ci-full.yml's
// SCHEMA_SLOW=1 unit rows.
func gatedRunPattern(root string) (string, error) {
	gated, err := gatedPackages(root)
	if err != nil {
		return "", err
	}
	funcs, err := moduleFuncs(root)
	if err != nil {
		return "", err
	}
	reaching := map[funcKey]bool{}
	for key, body := range funcs {
		if body.gate {
			reaching[key] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for key, body := range funcs {
			if reaching[key] {
				continue
			}
			for callee := range body.callees {
				if reaching[callee] {
					reaching[key] = true
					changed = true
					break
				}
			}
		}
	}
	var names []string
	seen := map[string]bool{}
	dirs := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", "generated", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err == nil {
				dirs[filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	_ = gated
	for rel := range dirs {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		tests, err := declaredTests(dir)
		if err != nil {
			continue
		}
		for name := range tests {
			if reaching[funcKey{dir: rel, name: name}] && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no gated test reaches slowtest.Gate")
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("^(")
	for i, n := range names {
		if i > 0 {
			b.WriteString("|")
		}
		b.WriteString(regexp.QuoteMeta(n))
	}
	b.WriteString(")$")
	return b.String(), nil
}

type funcKey struct {
	dir, name string
}

type funcBody struct {
	gate    bool
	callees map[funcKey]bool
}

// moduleFuncs indexes every function declaration in the module by
// package directory and name, recording whether its body calls the
// gate and which package-scoped functions it calls.
func moduleFuncs(root string) (map[funcKey]funcBody, error) {
	module := ""
	if b, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		for line := range strings.SplitSeq(string(b), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				module = strings.TrimSpace(rest)
				break
			}
		}
	}
	out := map[funcKey]funcBody{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", "generated", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		imports := map[string]string{}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil || module == "" {
				continue
			}
			rest, ok := strings.CutPrefix(p, module)
			if !ok {
				continue
			}
			dir := strings.TrimPrefix(rest, "/")
			alias := ""
			if imp.Name != nil {
				alias = imp.Name.Name
			} else {
				alias = filepath.Base(dir)
				if alias == "." || alias == "/" {
					alias = filepath.Base(p)
				}
			}
			imports[alias] = dir
		}
		full, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return nil
		}
		dir := filepath.ToSlash(rel)
		for _, decl := range full.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			body := funcBody{callees: map[funcKey]bool{}}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && strings.Contains(lit.Value, gateSkipMarker) {
					body.gate = true
				}
				// A helper passed as a value (a table of check funcs) is a
				// call this test makes, though no CallExpr names it here.
				if id, ok := n.(*ast.Ident); ok {
					body.callees[funcKey{dir: dir, name: id.Name}] = true
				}
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok {
						if impDir, ok := imports[id.Name]; ok {
							body.callees[funcKey{dir: impDir, name: sel.Sel.Name}] = true
						} else {
							body.callees[funcKey{dir: dir, name: sel.Sel.Name}] = true
						}
					}
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Enabled" {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "slowtest" {
						body.gate = true
					}
				}
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					body.callees[funcKey{dir: dir, name: fun.Name}] = true
				case *ast.SelectorExpr:
					if id, ok := fun.X.(*ast.Ident); ok {
						if id.Name == "slowtest" && fun.Sel.Name == "Gate" {
							body.gate = true
						}
						if impDir, ok := imports[id.Name]; ok {
							body.callees[funcKey{dir: impDir, name: fun.Sel.Name}] = true
							if fun.Sel.Name == "Gate" {
								body.gate = true
							}
						} else {
							body.callees[funcKey{dir: dir, name: fun.Sel.Name}] = true
						}
					}
				}
				return true
			})
			key := funcKey{dir: dir, name: fn.Name.Name}
			prev := out[key]
			if prev.callees != nil {
				for c := range prev.callees {
					body.callees[c] = true
				}
				body.gate = body.gate || prev.gate
			}
			out[key] = body
		}
		return nil
	})
	return out, err
}
