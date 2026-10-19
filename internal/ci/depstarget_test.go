package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestDepsTargetProvisionsTheSerializeGoSibling(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	goMake, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	all := string(makefile) + "\n" + string(goMake)
	for _, want := range []string{".PHONY: deps", "SERIALIZE_GO_PIN", "SERIALIZE_GO_COMMIT", "fetch-runtime"} {
		if !strings.Contains(all, want) {
			t.Errorf("Makefile and make/go.mk do not contain %q", want)
		}
	}
	for _, target := range []string{"test", "test-go", "slow-gate-coverage", "tables-go-sibling-runtime"} {
		re := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(target) + ":\\s+deps(?:\\s|$)")
		if !re.MatchString(all) {
			t.Errorf("target %s does not list deps as a prerequisite", target)
		}
	}
}

func TestNoGateTestHardCodesTheSerializeGoPath(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var failures []string
	cacheNeedle := "serialize.go" + "@" + "v"
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/slowtest/") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		flag := func(pos token.Pos, why string) {
			p := fset.Position(pos)
			failures = append(failures, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), p.Line, why))
		}
		stringValue := func(expr ast.Expr) (string, bool) {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return "", false
			}
			value, err := strconv.Unquote(lit.Value)
			return value, err == nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				value, ok := stringValue(x)
				if ok {
					parts := strings.Split(strings.ReplaceAll(value, "\\", "/"), "/")
					if len(parts) > 1 && parts[len(parts)-1] == "serialize.go" && slicesContains(parts, "..") {
						flag(x.Pos(), "hard-coded parent-relative serialize.go path")
					}
					if strings.Contains(value, cacheNeedle) {
						flag(x.Pos(), "hard-coded versioned serialize.go module-cache path")
					}
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Join" {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "filepath" {
					return true
				}
				hasParent, hasRuntime := false, false
				for _, arg := range x.Args {
					value, ok := stringValue(arg)
					if !ok {
						continue
					}
					hasParent = hasParent || value == ".." || strings.HasPrefix(value, "../")
					hasRuntime = hasRuntime || value == "serialize.go"
				}
				if hasParent && hasRuntime {
					flag(x.Pos(), "filepath.Join hard-codes a parent-relative serialize.go path")
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		sort.Strings(failures)
		t.Fatalf("gate tests hard-code the serialize.go path:\n%s", strings.Join(failures, "\n"))
	}
}

func slicesContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
