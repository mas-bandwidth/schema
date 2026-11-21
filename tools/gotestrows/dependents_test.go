package main

import (
	"slices"
	"strings"
	"testing"
)

// listFixture is `go list -test -f '{{.ImportPath}}|{{join .Deps " "}}' ./...`
// in miniature: tablewire imports the parser through check, ir imports
// nothing of the module, and tools/x imports the parser from its external
// test package alone.
const listFixture = `github.com/mas-bandwidth/schema/v2/internal/parser|bytes fmt
github.com/mas-bandwidth/schema/v2/internal/parser.test|bytes fmt github.com/mas-bandwidth/schema/v2/internal/parser testing
github.com/mas-bandwidth/schema/v2/internal/check|fmt github.com/mas-bandwidth/schema/v2/internal/parser
github.com/mas-bandwidth/schema/v2/internal/tablewire|fmt github.com/mas-bandwidth/schema/v2/internal/check github.com/mas-bandwidth/schema/v2/internal/parser
github.com/mas-bandwidth/schema/v2/internal/tablewire [github.com/mas-bandwidth/schema/v2/internal/tablewire.test]|fmt github.com/mas-bandwidth/schema/v2/internal/check github.com/mas-bandwidth/schema/v2/internal/parser testing
github.com/mas-bandwidth/schema/v2/ir|fmt
github.com/mas-bandwidth/schema/v2/ir.test|fmt github.com/mas-bandwidth/schema/v2/ir testing
github.com/mas-bandwidth/schema/v2/tools/x|fmt
github.com/mas-bandwidth/schema/v2/tools/x_test [github.com/mas-bandwidth/schema/v2/tools/x.test]|fmt github.com/mas-bandwidth/schema/v2/internal/parser testing
github.com/mas-bandwidth/schema/v2/tools/x.test|fmt github.com/mas-bandwidth/schema/v2/tools/x github.com/mas-bandwidth/schema/v2/internal/parser testing
`

// TestDependentsSelectsEveryPackageThatBuildsOnAChange: a change to the
// parser selects every package whose build or tests depend on it, directly
// (check), through another package (tablewire, the PR #4437 case), or from an
// external test package alone (tools/x); a package that does not depend on it
// (ir) is not selected.
func TestDependentsSelectsEveryPackageThatBuildsOnAChange(t *testing.T) {
	t.Parallel()
	got := dependents(listFixture, []string{"./internal/parser"})
	want := []string{"./internal/check", "./internal/parser", "./internal/tablewire", "./tools/x"}
	if !slices.Equal(got, want) {
		t.Fatalf("dependents of ./internal/parser = %v, want %v", got, want)
	}
	if got := dependents(listFixture, []string{"./ir"}); !slices.Equal(got, []string{"./ir"}) {
		t.Fatalf("dependents of ./ir = %v, want only ./ir's own test binary", got)
	}
	if got := dependents(listFixture, nil); len(got) != 0 {
		t.Fatalf("no change selected %v", got)
	}
}

// TestDependentsControlDirectoriesAloneMissTablewire is the reversed witness:
// the diff's directories alone (what the plan selected before) do not name
// internal/tablewire for a parser change, so without dependents its broken
// tests run in no row.
func TestDependentsControlDirectoriesAloneMissTablewire(t *testing.T) {
	t.Parallel()
	tb := table{short: map[string]float64{}, plain: map[string]float64{}}
	for _, pkgs := range [][]string{{"./internal/parser"}, append([]string{"./internal/parser"}, dependents(listFixture, []string{"./internal/parser"})...)} {
		found := false
		for _, r := range plan(tb, pkgs, false, 60) {
			found = found || slices.Contains(strings.Fields(r.Packages), "./internal/tablewire")
		}
		if want := len(pkgs) > 1; found != want {
			t.Fatalf("packages %v: tablewire in a row = %v, want %v", pkgs, found, want)
		}
	}
}
