package testreg

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// driverPackages are the driver's own packages, from this directory. The
// repo-wide skip policy is a later layer (docs/TEST-REGISTRY.md).
var driverPackages = []string{".", "./extract", "../../tools/schema-test"}

// skipCalls returns file:line for every skip mechanism in Go source: a call
// of Skip, SkipNow or Skipf on anything, and testing.Short().
func skipCalls(fset *token.FileSet, f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		pkg, _ := sel.X.(*ast.Ident)
		if name == "Skip" || name == "SkipNow" || name == "Skipf" || (name == "Short" && pkg != nil && pkg.Name == "testing") {
			out = append(out, fmt.Sprintf("%s: %s", fset.Position(call.Pos()), name))
		}
		return true
	})
	return out
}

func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files in %s (%v)", dir, err)
	}
	return files
}

// TestTheDriverHasNoSkips: no test or code in the driver's packages skips.
// Its plant is the source below, which must be found.
func TestTheDriverHasNoSkips(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	plant, err := parser.ParseFile(fset, "plant.go", "package p\nimport \"testing\"\nfunc TestP(t *testing.T) { if testing.Short() { t.Skip(\"slow\") } }\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := skipCalls(fset, plant); len(got) != 2 {
		t.Fatalf("the planted Skip and testing.Short were not both found: %v", got)
	}
	var found []string
	for _, dir := range driverPackages {
		for _, file := range goFiles(t, dir) {
			f, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			found = append(found, skipCalls(fset, f)...)
		}
	}
	if len(found) > 0 {
		t.Errorf("the driver's packages skip, and nothing is ever skipped:\n  %s", strings.Join(found, "\n  "))
	}
}

// TestEveryDriverTestIsRegistered: every Go test in the driver's packages is
// a language-free registry test the gotest adapter runs by name, and every
// such registry row names a test that exists. No test lives outside the
// registry.
func TestEveryDriverTestIsRegistered(t *testing.T) {
	t.Parallel()
	reg, err := Load("../../" + DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{} // "./pkg TestX"
	pkgs := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range driverPackages {
		pkg := "./" + filepath.ToSlash(filepath.Clean(filepath.Join("internal/testreg", dir)))
		pkgs[pkg] = true
		for _, file := range goFiles(t, dir) {
			if !strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") && fn.Name.Name != "TestMain" {
					have[pkg+" "+fn.Name.Name] = true
				}
			}
		}
	}
	registered := map[string]bool{}
	for _, tt := range reg.Tests {
		if tt.Adapter != "gotest" {
			continue
		}
		var in struct{ Package, Test string }
		if err := json.Unmarshal(tt.Inputs, &in); err != nil {
			t.Fatalf("%s: %v", tt.Key, err)
		}
		k := in.Package + " " + in.Test
		if !pkgs[in.Package] {
			continue
		}
		registered[k] = true
		if !have[k] {
			t.Errorf("%s:%d: %s names %s, which does not exist", reg.Path, tt.Line, tt.Key, k)
		}
	}
	for k := range have {
		if !registered[k] {
			t.Errorf("%s is a Go test with no registry row (rec test, adapter gotest)", k)
		}
	}
	if len(have) == 0 {
		t.Fatal("found no Go tests in the driver's packages")
	}
}
