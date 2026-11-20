package ci

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var fsPathOps = map[string]bool{
	"os.Stat":      true,
	"os.Lstat":     true,
	"os.ReadFile":  true,
	"os.WriteFile": true,
	"os.Open":      true,
	"os.OpenFile":  true,
	"os.Create":    true,
	"os.Remove":    true,
	"os.Mkdir":     true,
	"os.MkdirAll":  true,
}

var fsWriteOps = map[string]bool{
	"os.WriteFile": true,
	"os.OpenFile":  true,
	"os.Create":    true,
}

func selectorCallName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + sel.Sel.Name
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return val, true
}

func exprText(fset *token.FileSet, expr ast.Expr) string {
	var b bytes.Buffer
	if err := format.Node(&b, fset, expr); err != nil {
		return ""
	}
	return b.String()
}

// extractBaseAndRel extracts a (baseExprKey, relLiteralPath) pair from a path
// argument passed to an `os.*` call:
//   - `"Foo.x"` -> ("<cwd>", "Foo.x")
//   - `filepath.Join(out, "Foo.luau")` -> ("out", "Foo.luau")
//   - `filepath.Join(out, "sub", "Foo.luau")` -> ("out", "sub/Foo.luau")
//   - `dir + "/Foo.x"` -> ("dir", "Foo.x")
func extractBaseAndRel(fset *token.FileSet, arg ast.Expr) (string, string, bool) {
	if s, ok := stringLiteralValue(arg); ok {
		clean := path.Clean(filepath.ToSlash(s))
		if clean == "." || clean == "/" || clean == "" {
			return "", "", false
		}
		return "<cwd>", clean, true
	}
	if call, ok := arg.(*ast.CallExpr); ok {
		name := selectorCallName(call)
		if (name == "filepath.Join" || name == "path.Join") && len(call.Args) >= 2 {
			// Split leading non-literal prefix from trailing string-literal segments.
			litStart := len(call.Args)
			var segs []string
			for i, v := range slices.Backward(call.Args) {
				s, ok := stringLiteralValue(v)
				if !ok {
					break
				}
				segs = append([]string{filepath.ToSlash(s)}, segs...)
				litStart = i
			}
			if len(segs) == 0 {
				return "", "", false
			}
			var baseParts []string
			for i := range litStart {
				baseParts = append(baseParts, exprText(fset, call.Args[i]))
			}
			base := "<cwd>"
			if len(baseParts) > 0 {
				base = strings.Join(baseParts, ",")
			}
			rel := path.Clean(strings.Join(segs, "/"))
			if rel == "." || rel == "/" || rel == "" {
				return "", "", false
			}
			return base, rel, true
		}
	}
	if bin, ok := arg.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		if rhs, ok := stringLiteralValue(bin.Y); ok {
			trimmed := strings.TrimLeft(filepath.ToSlash(rhs), "/")
			if trimmed != "" {
				return exprText(fset, bin.X), path.Clean(trimmed), true
			}
		}
	}
	return "", "", false
}

func looksLikeFilename(s string) bool {
	base := path.Base(filepath.ToSlash(s))
	ext := path.Ext(base)
	return len(ext) >= 2 && len(base) > len(ext) && !strings.ContainsAny(s, " \t\n:=")
}

func fileWritesFlatTempMap(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !fsWriteOps[selectorCallName(call)] || len(call.Args) == 0 {
			return true
		}
		// Check whether the write path joins directly `filepath.Join(dir, name)`
		// (2 args, second arg an identifier) without an isolating subdirectory.
		joinCall, ok := call.Args[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		jname := selectorCallName(joinCall)
		if (jname == "filepath.Join" || jname == "path.Join") && len(joinCall.Args) == 2 {
			if _, isIdent := joinCall.Args[1].(*ast.Ident); isIdent {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func checkScopeFSCollisions(fset *token.FileSet, fileName string, body *ast.BlockStmt) []string {
	type site struct {
		op   string
		base string
		rel  string
		line int
	}
	var sites []site
	var errs []string

	walkStmt := func(n ast.Node) {
		if n == nil {
			return
		}
		ast.Inspect(n, func(cur ast.Node) bool {
			if cur == nil {
				return false
			}
			// Nested closures / t.Run subtests get their own lexical check so
			// independent subtests using `t.TempDir()` in separate `dir` scopes
			// never cross-collide, while collisions inside one subtest are caught.
			if lit, ok := cur.(*ast.FuncLit); ok && lit.Body != body {
				errs = append(errs, checkScopeFSCollisions(fset, fileName, lit.Body)...)
				return false
			}
			call, ok := cur.(*ast.CallExpr)
			if !ok {
				return true
			}
			op := selectorCallName(call)
			if !fsPathOps[op] || len(call.Args) == 0 {
				return true
			}
			base, rel, ok := extractBaseAndRel(fset, call.Args[0])
			if !ok {
				return true
			}
			sites = append(sites, site{
				op:   op,
				base: base,
				rel:  rel,
				line: fset.Position(call.Pos()).Line,
			})
			return true
		})
	}
	walkStmt(body)

	seen := map[string]site{}
	for _, s := range sites {
		key := s.base + "::" + strings.ToLower(s.rel)
		prev, exists := seen[key]
		if !exists {
			seen[key] = s
			continue
		}
		if prev.rel != s.rel {
			errs = append(errs, fmt.Sprintf(
				"%s:%d: %s(%q) collides case-insensitively with %s(%q) at line %d under %s (breaks on macOS APFS / Windows NTFS; use distinct subdirectories or inspect os.ReadDir entry names)",
				fileName, s.line, s.op, s.rel, prev.op, prev.rel, prev.line, s.base,
			))
		}
	}
	return errs
}

func checkCaseInsensitiveTestCollisions(testSources map[string]string) []string {
	var errs []string
	var names []string
	for name := range testSources {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, testSources[name], 0)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: parse error: %v", name, err))
			continue
		}

		// 1. Check per-function and per-closure `os.*` path call pairs.
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			errs = append(errs, checkScopeFSCollisions(fset, name, fn.Body)...)
		}

		// 2. Check `map[string]string` / `map[string][]byte` composite literals
		// in files that stage map keys flat into a directory via `os.WriteFile(filepath.Join(dir, name), ...)`.
		if fileWritesFlatTempMap(file) {
			ast.Inspect(file, func(n ast.Node) bool {
				comp, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				mt, isMap := comp.Type.(*ast.MapType)
				if !isMap {
					return true
				}
				keyIdent, ok := mt.Key.(*ast.Ident)
				if !ok || keyIdent.Name != "string" {
					return true
				}
				byFold := map[string]string{}
				for _, elt := range comp.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					k, ok := stringLiteralValue(kv.Key)
					if !ok || !looksLikeFilename(k) {
						continue
					}
					norm := path.Clean(filepath.ToSlash(k))
					fold := strings.ToLower(norm)
					if prev, exists := byFold[fold]; exists && prev != norm {
						errs = append(errs, fmt.Sprintf(
							"%s:%d: map literal stages %q and %q into a flat directory writer (overwrites on macOS APFS / Windows NTFS; parse in memory or isolate into distinct subdirectories)",
							name, fset.Position(kv.Pos()).Line, prev, norm,
						))
					} else {
						byFold[fold] = norm
					}
				}
				return true
			})
		}
	}

	slices.Sort(errs)
	return errs
}

func TestCaseInsensitiveTestCollisions(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	sources := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", "dist", "generated", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		sources[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo test files: %v", err)
	}
	if len(sources) < 400 {
		t.Fatalf("scanned only %d *_test.go files; expected >= 400", len(sources))
	}
	for _, e := range checkCaseInsensitiveTestCollisions(sources) {
		t.Error(e)
	}
}

func TestCaseInsensitiveTestCollisionsNegativeControls(t *testing.T) {
	t.Parallel()

	clean := map[string]string{
		"pkg/clean_test.go": `package pkg
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	out := t.TempDir()
	_ = os.WriteFile(filepath.Join(out, "a", "Foo.schema"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(out, "b", "foo.schema"), []byte("y"), 0o644)
	entries, _ := os.ReadDir(out)
	_ = entries
	for _, want := range []string{"Foo.luau", "foo.luau"} {
		_ = strings.Contains("Foo.luau foo.luau", want)
	}
	inMem := map[string]string{
		"Foo.schema": "a",
		"foo.schema": "b",
	}
	_ = inMem
}
`,
	}
	if errs := checkCaseInsensitiveTestCollisions(clean); len(errs) != 0 {
		t.Fatalf("expected clean baseline to pass, got: %v", errs)
	}

	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			name: "planted os.Stat Foo.x and foo.x",
			src: `package pkg
import (
	"os"
	"testing"
)
func TestBadStat(t *testing.T) {
	_, _ = os.Stat("Foo.x")
	_, _ = os.Stat("foo.x")
}
`,
			wantSub: `os.Stat("foo.x") collides case-insensitively with os.Stat("Foo.x")`,
		},
		{
			name: "planted filepath.Join os.Stat Foo.luau and foo.luau",
			src: `package pkg
import (
	"os"
	"path/filepath"
	"testing"
)
func TestBadJoinStat(t *testing.T) {
	out := t.TempDir()
	_, _ = os.Stat(filepath.Join(out, "Foo.luau"))
	_, _ = os.Stat(filepath.Join(out, "foo.luau"))
}
`,
			wantSub: `os.Stat("foo.luau") collides case-insensitively with os.Stat("Foo.luau")`,
		},
		{
			name: "planted pre-fix unitFromSchemas flat temp dir map",
			src: `package pkg
import (
	"os"
	"path/filepath"
	"testing"
)
func unitFromSchemas(t *testing.T, schemas map[string]string) {
	dir := t.TempDir()
	for name, body := range schemas {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
}
func TestBadMap(t *testing.T) {
	unitFromSchemas(t, map[string]string{
		"Foo.schema": "package p",
		"foo.schema": "package p",
	})
}
`,
			wantSub: `map literal stages "Foo.schema" and "foo.schema" into a flat directory writer`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := checkCaseInsensitiveTestCollisions(map[string]string{"pkg/bad_test.go": tc.src})
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tc.wantSub) {
				t.Fatalf("planted case %q did not report %q; got:\n%s", tc.name, tc.wantSub, joined)
			}
		})
	}
}
