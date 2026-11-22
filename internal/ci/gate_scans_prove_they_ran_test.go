package ci

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// TestGateScansProveTheyRan holds the gate tests of this package and of
// internal/slowtest to the zero-match rule (internal/provesran): none of them
// calls path/filepath.Glob. A gate that globs for the files it judges and gets
// nothing back runs its loop zero times and passes, which is the defect the
// rule is about; measured on 2026-11-21 by pointing each of this package's
// globs at nothing in turn, sixteen of them left their test green.
// provesran.Glob refuses an empty match, and a scan that looks in several
// places and needs a match in one counts them in a provesran.Ledger.
func TestGateScansProveTheyRan(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	read := 0
	for _, dir := range []string{"internal/ci", "internal/slowtest"} {
		for _, path := range provesran.MustGlob(t, filepath.Join(root, dir, "*_test.go")) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			at, err := rawGlobCalls(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			read++
			for _, line := range at {
				t.Errorf("%s:%d calls filepath.Glob: a pattern that matches nothing is then a scan that passes having judged nothing — use provesran.Glob, or a provesran.Ledger's Glob where an empty place is sound", filepath.ToSlash(rel), line)
			}
		}
	}
	if read < 100 {
		t.Fatalf("read %d gate test sources, want at least 100: this gate is looking at the wrong tree", read)
	}
}

// TestGateScansProveTheyRanNegativeControl plants the call under each name a
// source can give it, and the calls that are not it.
func TestGateScansProveTheyRanNegativeControl(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src  string
		want []int
	}{
		"the plain call":      {"package p\n\nimport \"path/filepath\"\n\nfunc f() { filepath.Glob(\"*.mk\") }\n", []int{5}},
		"a renamed import":    {"package p\n\nimport fp \"path/filepath\"\n\nfunc f() {\n\tfp.Glob(\"*.mk\")\n}\n", []int{6}},
		"taken as a value":    {"package p\n\nimport \"path/filepath\"\n\nvar glob = filepath.Glob\n", []int{5}},
		"the refusing helper": {"package p\n\nimport \"example.com/m/internal/provesran\"\n\nfunc f() { provesran.Glob(\"*.mk\") }\n", nil},
		"another package":     {"package p\n\nimport \"io/fs\"\n\nfunc f(s fs.FS) { fs.Glob(s, \"*.mk\") }\n", nil},
		"a method named Glob": {"package p\n\nimport \"path/filepath\"\n\nfunc f(l ledger) { l.Glob(filepath.Join(\"a\", \"*\")) }\n", nil},
	} {
		got, err := rawGlobCalls(name+".go", []byte(tc.src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: filepath.Glob at lines %v, want %v", name, got, tc.want)
		}
	}
}

// rawGlobCalls returns the lines of a Go source that name path/filepath.Glob,
// under whatever name the source imports the package. src nil reads path.
func rawGlobCalls(path string, src []byte) ([]int, error) {
	fset := token.NewFileSet()
	var source any
	if src != nil {
		source = src
	}
	file, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != "path/filepath" {
			continue
		}
		name := "filepath"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = true
	}
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Glob" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && names[pkg.Name] {
			lines = append(lines, fset.Position(sel.Pos()).Line)
		}
		return true
	})
	return lines, nil
}
