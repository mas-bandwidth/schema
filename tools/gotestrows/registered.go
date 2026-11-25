package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// THE REGISTRY'S GO TESTS RUN THROUGH THE DRIVER, NOT HERE
// (docs/TEST-REGISTRY.md). A language-free Go test with a gotest row in the
// test registry runs in ci-fast's schema-test-pr shards (`schema-test run
// --group pr`), so go-test-touched does not run it a second time: a package
// whose every test is a registry row leaves the selection, and a package with
// tests the registry does not run yet gets one row that runs only those.

// registered is the gotest rows of the registry's pr group: for each
// ./package, the top-level tests the schema-test-pr shards run. A row of a
// tier the pr group leaves out (nightly) is not one: go-test-touched keeps
// running its test on the pull request that touches its package.
func registered(path string) (map[string]map[string]bool, error) {
	reg, err := testreg.Load(path)
	if err != nil {
		return nil, err
	}
	g, ok := reg.Group("pr")
	if !ok {
		return nil, fmt.Errorf("%s has no group pr", path)
	}
	sel, err := reg.Select(g.Query())
	if err != nil {
		return nil, fmt.Errorf("%s: group pr: %w", path, err)
	}
	out := map[string]map[string]bool{}
	for _, inst := range sel.Instances {
		t := inst.Test
		if t.Adapter != "gotest" {
			continue
		}
		var in struct{ Package, Test string }
		if err := json.Unmarshal(t.Inputs, &in); err != nil || !strings.HasPrefix(in.Package, "./") || in.Test == "" {
			return nil, fmt.Errorf("%s:%d: %s: a gotest row's inputs name a ./package and a test", path, t.Line, t.Key)
		}
		if out[in.Package] == nil {
			out[in.Package] = map[string]bool{}
		}
		out[in.Package][in.Test] = true
	}
	return out, nil
}

// declared is the top-level tests and fuzz targets a package's _test.go
// files declare, as go test names them: functions, not methods, named Test
// or Fuzz and then a name not starting lower case, TestMain aside. The files
// are parsed, so a test written inside a fixture string is not one.
func declared(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []string
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			name := fn.Name.Name
			rest, isTest := strings.CutPrefix(name, "Test")
			if !isTest {
				if rest, isTest = strings.CutPrefix(name, "Fuzz"); !isTest {
					continue
				}
			}
			if name == "TestMain" {
				continue
			}
			if r, _ := utf8.DecodeRuneInString(rest); rest != "" && unicode.IsLower(r) {
				continue
			}
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// unregistered splits pkgs (relative to root) by the registry: kept are the packages
// go-test-touched still runs, and left maps each kept package that has
// registry rows to the tests the registry does not run, which are all its
// row runs. A package whose every test is a row is in neither.
func unregistered(root string, pkgs []string, reg map[string]map[string]bool) (kept []string, left map[string][]string, err error) {
	left = map[string][]string{}
	for _, p := range pkgs {
		rows := reg[p]
		if len(rows) == 0 {
			kept = append(kept, p)
			continue
		}
		names, err := declared(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return nil, nil, err
		}
		var rest []string
		for _, n := range names {
			if !rows[n] {
				rest = append(rest, n)
			}
		}
		if len(rest) == 0 {
			continue
		}
		kept = append(kept, p)
		left[p] = rest
	}
	return kept, left, nil
}
