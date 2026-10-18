package ci

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// generatedResultHelpers names Go test helpers that build or run generated code
// and return output and error that each caller must check. Each entry names its
// defining test file.
var generatedResultHelpers = map[string]string{
	"runGeneratedResult":     "internal/codegen/gotable/values_test.go",
	"runGeneratedEdited":     "internal/codegen/gotable/values_test.go",
	"runGeneratedUnit":       "internal/codegen/gotable/values_test.go",
	"runGeneratedTableUnit":  "internal/fuzz/tablediff_test.go",
	"runFixLoadProbe":        "internal/codegen/gotable/fixedloadallocs_test.go",
	"runVersionProbe":        "internal/codegen/gotable/fixedversioning_test.go",
	"runVersionProbeRetired": "internal/codegen/gotable/fixedversioning_test.go",
	"runTextDifferential":    "internal/codegen/gotable/textdifferential_test.go",
	"subsetGo":               "internal/codegen/gotable/subsets_test.go",
	"t05RunProbe":            "internal/codegen/gotable/fixedroadmap_t05_test.go",
}

type generatedResultFinding struct {
	file   string
	line   int
	helper string
}

func (f generatedResultFinding) String() string {
	return f.file + ":" + strconv.Itoa(f.line) + ": " + f.helper + " result is not checked"
}

func TestGeneratedHelperResultsAreChecked(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fset := token.NewFileSet()
	var paths []string
	for _, scanRoot := range []string{"internal", "test"} {
		base := filepath.Join(root, scanRoot)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_test.go") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", scanRoot, err)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no Go test files under internal/ and test/; generated-helper gate scanned nothing")
	}
	t.Logf("scanned %d Go test files", len(paths))
	var findings []generatedResultFinding
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			continue
		}
		findings = append(findings, uncheckedGeneratedResults(fset, file)...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		if findings[i].line != findings[j].line {
			return findings[i].line < findings[j].line
		}
		return findings[i].helper < findings[j].helper
	})
	for _, finding := range findings {
		t.Error(finding.String())
	}
}

func uncheckedGeneratedResults(fset *token.FileSet, file *ast.File) []generatedResultFinding {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	var findings []generatedResultFinding
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if _, listed := generatedResultHelpers[name.Name]; !listed {
			return true
		}
		if callResultIsChecked(call, parents) {
			return true
		}
		pos := fset.Position(call.Pos())
		findings = append(findings, generatedResultFinding{file: pos.Filename, line: pos.Line, helper: name.Name})
		return true
	})
	return findings
}

func callResultIsChecked(call *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	parent := parents[call]
ancestors:
	for node := parent; node != nil; node = parents[node] {
		switch node.(type) {
		case *ast.GoStmt, *ast.DeferStmt:
			return false
		case *ast.FuncDecl, *ast.FuncLit:
			break ancestors
		}
	}
	switch p := parent.(type) {
	case *ast.CallExpr:
		for _, arg := range p.Args {
			if arg == call {
				return true
			}
		}
	case *ast.ReturnStmt:
		for _, result := range p.Results {
			if result == call {
				return true
			}
		}
	case *ast.AssignStmt:
		if len(p.Rhs) == 1 && p.Rhs[0] == call && len(p.Lhs) >= 2 {
			errorTarget := p.Lhs[len(p.Lhs)-1]
			if id, ok := errorTarget.(*ast.Ident); ok {
				if id.Name == "_" {
					return false
				}
				return identifierHasRead(id.Name, enclosingFunction(call, parents), parents)
			}
		}
	case *ast.ValueSpec:
		if len(p.Values) == 1 && p.Values[0] == call && len(p.Names) >= 2 {
			id := p.Names[len(p.Names)-1]
			if id.Name == "_" {
				return false
			}
			return identifierHasRead(id.Name, enclosingFunction(call, parents), parents)
		}
	case *ast.ExprStmt, *ast.GoStmt, *ast.DeferStmt:
		return false
	}
	return false
}

func enclosingFunction(node ast.Node, parents map[ast.Node]ast.Node) ast.Node {
	for parent := parents[node]; parent != nil; parent = parents[parent] {
		switch parent.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return parent
		}
	}
	return nil
}

func identifierHasRead(name string, function ast.Node, parents map[ast.Node]ast.Node) bool {
	if function == nil {
		return false
	}
	read := false
	ast.Inspect(function, func(node ast.Node) bool {
		if node == nil || read {
			return !read
		}
		id, ok := node.(*ast.Ident)
		if !ok || id.Name != name || isIdentifierWrite(id, parents) || isSelectorName(id, parents) {
			return true
		}
		if isDiscardedError(id, parents) {
			return true
		}
		read = true
		return false
	})
	return read
}

func isIdentifierWrite(id *ast.Ident, parents map[ast.Node]ast.Node) bool {
	switch p := parents[id].(type) {
	case *ast.AssignStmt:
		for _, lhs := range p.Lhs {
			if lhs == id {
				return true
			}
		}
	case *ast.ValueSpec:
		for _, name := range p.Names {
			if name == id {
				return true
			}
		}
	case *ast.RangeStmt:
		return p.Key == id || p.Value == id
	case *ast.IncDecStmt:
		return p.X == id
	case *ast.Field:
		for _, name := range p.Names {
			if name == id {
				return true
			}
		}
	case *ast.FuncDecl:
		return p.Name == id
	case *ast.LabeledStmt:
		return p.Label == id
	}
	return false
}

func isSelectorName(id *ast.Ident, parents map[ast.Node]ast.Node) bool {
	p, ok := parents[id].(*ast.SelectorExpr)
	return ok && p.Sel == id
}

func isDiscardedError(id *ast.Ident, parents map[ast.Node]ast.Node) bool {
	assign, ok := parents[id].(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 || assign.Rhs[0] != id || len(assign.Lhs) != 1 {
		return false
	}
	blank, ok := assign.Lhs[0].(*ast.Ident)
	return ok && blank.Name == "_"
}

func TestGeneratedHelperResultsCheckerForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"expression", "package p; func f(){ runGeneratedUnit() }", 1},
		{"go statement", "package p; func f(){ go runGeneratedUnit() }", 1},
		{"nested go statement", "package p; func f(){ go consume(runGeneratedUnit()) }", 1},
		{"defer statement", "package p; func f(){ defer runGeneratedUnit() }", 1},
		{"nested defer statement", "package p; func f(){ defer consume(runGeneratedUnit()) }", 1},
		{"blank short assignment", "package p; func f(){ out, _ := runGeneratedUnit(); _ = out }", 1},
		{"blank assignment", "package p; func f(){ _, _ = runGeneratedUnit() }", 1},
		{"unused error", "package p; func f(){ out, err := runGeneratedUnit(); _ = out }", 1},
		{"only discarded error", "package p; func f(){ out, err := runGeneratedUnit(); _ = out; _ = err }", 1},
		{"checked error", "package p; func f(){ out, err := runGeneratedUnit(); _ = out; if err != nil {} }", 0},
		{"asserted error", "package p; func f(t T){ _, err := runGeneratedUnit(); assertNoError(t, err) }", 0},
		{"returned error", "package p; func wrap()([]byte,error){ return runGeneratedUnit() }", 0},
		{"passed error tuple", "package p; func f(){ consume(runGeneratedUnit()) }", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.name+".go", tc.src, 0)
			if err != nil {
				t.Fatal(err)
			}
			got := uncheckedGeneratedResults(fset, file)
			if len(got) != tc.want {
				t.Fatalf("findings = %v, want %d", got, tc.want)
			}
		})
	}
}
