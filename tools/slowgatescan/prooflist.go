// prooflist.go is the name audit behind schema#988 (G5): a positive gate
// target that goes through test/slowgate/proof must name the tests it proves,
// and a name that no declaration in the package satisfies proves NOTHING — the
// proof script requires a literal `--- PASS: <name> (` and fails on a missing
// one, but only when that target actually runs. A stale name is visible here,
// in a scan that costs milliseconds, instead of at the gate.
//
// It checks two kinds of required name:
//
//   - the quoted names list of every `sh test/slowgate/proof <label> '<names>'
//     <package> ...` recipe line that scanMakefiles already collects from
//     Makefile, make/*.mk and make/checks/*.mk;
//   - the `test=` values of test/conformance/go/ownership-negative-control,
//     whose package is ./internal/codegen/gotable.
//
// The declarations are read with go/parser, so only a TOP-LEVEL
// `func TestX(t *testing.T)` counts: a `func TestRetain(t *testing.T)` printed
// inside a probe's string literal (retain_test.go:58) is not a declaration and
// can never satisfy a name. A `Parent/sub` name checks Parent, because Go's -run
// selects the subtest through its parent.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// proofRef is one required name, and where it is written. file is relative to
// the repository root; label is the proof label (or the negative control's
// mode), so a failure reads where and why the name was demanded.
type proofRef struct {
	file  string
	line  int
	label string
	pkg   string
	name  string
}

// proofListFindings reports every required name with no matching top-level
// declaration as `<file>:<line> <label>: <name> names no test in <package>`.
func proofListFindings(root string, recipes []recipe) ([]string, error) {
	refs := proofRefsFromRecipes(recipes)
	own, err := proofRefsFromOwnership(filepath.Join(root, "test", "conformance", "go", "ownership-negative-control"))
	if err != nil {
		return nil, err
	}
	refs = append(refs, own...)
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].file != refs[j].file {
			return refs[i].file < refs[j].file
		}
		if refs[i].line != refs[j].line {
			return refs[i].line < refs[j].line
		}
		return refs[i].name < refs[j].name
	})
	decls := map[string]map[string]bool{}
	var findings []string
	for _, r := range refs {
		set, ok := decls[r.pkg]
		if !ok {
			dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(r.pkg, "./")))
			set, err = declaredTests(dir)
			if err != nil {
				return nil, fmt.Errorf("reading the test declarations of %s: %w", r.pkg, err)
			}
			decls[r.pkg] = set
		}
		name := r.name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[:i]
		}
		if !set[name] {
			findings = append(findings, fmt.Sprintf("%s:%d %s: %s names no test in %s", r.file, r.line, r.label, r.name, r.pkg))
		}
	}
	return findings, nil
}

// proofRefsFromRecipes reads the `sh test/slowgate/proof <label> '<names>'
// <package> ...` shape out of the recipe lines scanMakefiles collected.
func proofRefsFromRecipes(recipes []recipe) []proofRef {
	var out []proofRef
	for _, r := range recipes {
		words := shellWords(r.cmd)
		at := -1
		for i, w := range words {
			if w == "test/slowgate/proof" {
				at = i
				break
			}
		}
		if at < 0 || at+3 > len(words) {
			continue
		}
		label := words[at+1]
		names := strings.Fields(words[at+2])
		pkg := ""
		for i := at + 3; i < len(words); i++ {
			if words[i] == "." || strings.HasPrefix(words[i], "./") {
				pkg = words[i]
				break
			}
		}
		if pkg == "" {
			continue
		}
		for _, n := range names {
			out = append(out, proofRef{file: r.file, line: r.line, label: label, pkg: pkg, name: n})
		}
	}
	return out
}

var (
	proofControlMode = regexp.MustCompile(`^([a-z][a-z0-9-]*)\)`)
	proofControlTest = regexp.MustCompile(`\btest=([A-Za-z_][A-Za-z0-9_]*)`)
)

// proofRefsFromOwnership reads the `test=` names of the Go ownership negative
// control, which all name ./internal/codegen/gotable.
func proofRefsFromOwnership(path string) ([]proofRef, error) {
	fh, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }() // Read-only input; Scanner reports read errors.
	const rel = "test/conformance/go/ownership-negative-control"
	mode := "default"
	var out []proofRef
	sc := bufio.NewScanner(fh)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if m := proofControlMode.FindStringSubmatch(text); m != nil {
			mode = m[1]
		}
		if m := proofControlTest.FindStringSubmatch(text); m != nil {
			out = append(out, proofRef{file: rel, line: line, label: mode, pkg: "./internal/codegen/gotable", name: m[1]})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// declaredTests collects the top-level Test declarations of a package's
// *_test.go files.
func declaredTests(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	set := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if isTestFunc(fn) {
				set[fn.Name.Name] = true
			}
		}
	}
	return set, nil
}

// isTestFunc is the `func TestX(t *testing.T)` shape. A func Test inside a
// string literal is not an *ast.FuncDecl and never reaches here.
func isTestFunc(fn *ast.FuncDecl) bool {
	if !strings.HasPrefix(fn.Name.Name, "Test") {
		return false
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	p := fn.Type.Params.List[0]
	if len(p.Names) != 1 {
		return false
	}
	star, ok := p.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing" && sel.Sel.Name == "T"
}

// shellWords splits a recipe into shell words, honoring the single and double
// quotes the proof lists use. The recipe is already a single joined line.
func shellWords(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	single, double, esc := false, false, false
	for _, r := range s {
		switch {
		case esc:
			b.WriteRune(r)
			esc = false
		case r == '\\' && !single:
			esc = true
		case r == '\'' && !double:
			single = !single
		case r == '"' && !single:
			double = !double
		case (r == ' ' || r == '\t') && !single && !double:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}
