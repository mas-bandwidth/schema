package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testselect"
)

// THE REGISTRY'S GO TESTS RUN THROUGH THE DRIVER, NOT HERE
// (docs/TEST-REGISTRY.md). A language-free Go test with a gotest row in the
// test registry runs through the driver, in the nightly group (`schema-test run
// --group nightly`), so go-test-touched does not run it a second time: a package
// whose every test is a registry row (of any tier: a nightly row runs in the
// nightly lane) leaves the selection, and a package with tests the registry
// does not run yet gets one row that runs only those.

// registered is the registry's gotest rows, of every tier: for each
// ./package, the top-level tests the driver runs. Every row runs in the
// nightly group (the pr job runs only compile and happy-path rows), so
// go-test-touched runs neither: a per-change row never runs a nightly test.
func registered(path string) (map[string]map[string]bool, error) {
	reg, err := testreg.Load(path)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]bool{}
	for _, t := range reg.Tests {
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

// declared is the top-level tests, fuzz targets and examples go test runs in
// a package on this platform (internal/testselect.Names: its build
// constraints, functions not methods, TestMain aside, a test in a fixture
// string not one). A test built only for another platform is not declared
// here, so a row never names a test its runner cannot build.
func declared(dir string) ([]string, error) {
	return testselect.Names(build.Default, dir)
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
