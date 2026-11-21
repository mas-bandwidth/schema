package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

// embedFixture: the Squirrel generator embeds runtime/module.nut, compiler
// imports the generator, internal/goldens imports compiler, and
// internal/codegen/lua's tests alone embed a fixture.
const embedFixture = `github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel|fmt|runtime/module.nut runtime/sq.nut||runtime/*.nut|
github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel [github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel.test]|fmt testing|runtime/module.nut runtime/sq.nut|
github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable|fmt github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel||
github.com/mas-bandwidth/schema/v2/compiler|fmt github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel||
github.com/mas-bandwidth/schema/v2/internal/goldens.test|fmt github.com/mas-bandwidth/schema/v2/compiler github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel testing||
github.com/mas-bandwidth/schema/v2/internal/codegen/lua|fmt||testdata/x.lua 
github.com/mas-bandwidth/schema/v2/ir|fmt||
github.com/mas-bandwidth/schema/v2/cmd/schema|fmt|help/ai.md help/usage.md||help|
github.com/mas-bandwidth/schema/v2/internal/ci.test|fmt github.com/mas-bandwidth/schema/v2/cmd/schema testing||
`

// TestSelectPackagesSeedsDependentsFromAnEmbeddedChange: a runtime-only
// change (a .nut file the Squirrel generator embeds, no .go file) selects
// every package that builds on the generator, compiler and internal/goldens
// among them; a changed file only a package's tests embed selects that
// package alone; a file nothing embeds selects nothing more.
func TestSelectPackagesSeedsDependentsFromAnEmbeddedChange(t *testing.T) {
	t.Parallel()
	got, err := expand(selectPackages(embedFixture, []string{"./internal/codegen/squirrel", "./internal/codegen/squirreltable"}, nil, []string{"internal/codegen/squirrel/runtime/module.nut"}, allExist))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"./compiler", "./internal/goldens"} {
		if !slices.Contains(got, want) {
			t.Errorf("a change to the embedded runtime/module.nut selected %v, without %s", got, want)
		}
	}
	if slices.Contains(got, "./ir") {
		t.Errorf("selected ./ir, which does not depend on the generator: %v", got)
	}
	got = selectPackages(embedFixture, nil, nil, []string{"internal/codegen/lua/testdata/x.lua", "docs/README.md"}, allExist)
	if !slices.Equal(got, []string{"./internal/codegen/lua"}) {
		t.Errorf("a test-embedded change and an unembedded one selected %v, want only ./internal/codegen/lua", got)
	}
}

func allExist(string) bool { return true }

// TestRunSelectsTheOwnerOfAnEmbedOnlyDiff drives the command's one entry
// point, as ci-fast's rows step calls it, with a diff that changes only a
// file cmd/schema embeds and names no package: the finalized selection
// (-packages-out, what go-test-touched is gated on) holds cmd/schema and its
// dependent internal/ci, and the rows test them. The workflow once gated on
// the shell's own package list, empty for this diff, and skipped every row.
func TestRunSelectsTheOwnerOfAnEmbedOnlyDiff(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	diff := filepath.Join(dir, "diff-files.txt")
	if err := os.WriteFile(diff, []byte("cmd/schema/help/ai.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(dir, "selected.txt")
	var out bytes.Buffer
	golist := func() (string, error) { return embedFixture, nil }
	if err := run([]string{"-table", filepath.Join("..", "..", tablePath), "-dependents-of", "", "-changed-files", diff, "-packages-out", selected}, golist, allExist, &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(selected)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(b))
	for _, want := range []string{"./cmd/schema", "./internal/ci"} {
		if !slices.Contains(got, want) {
			t.Errorf("the finalized selection for an embed-only diff is %q, without %s", got, want)
		}
	}
	var rows []Row
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("rows %q: %v", out.String(), err)
	}
	tested := false
	for _, r := range rows {
		tested = tested || slices.Contains(strings.Fields(r.Packages), "./cmd/schema")
	}
	if !tested {
		t.Errorf("no row tests ./cmd/schema: %s", out.String())
	}

	// nothing selected: an empty selection and an empty matrix, never null
	out.Reset()
	if err := os.WriteFile(diff, []byte("docs/README.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-table", filepath.Join("..", "..", tablePath), "-changed-files", diff, "-packages-out", selected}, golist, allExist, &out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(selected); strings.TrimSpace(string(b)) != "" || strings.TrimSpace(out.String()) != "[]" {
		t.Errorf("an unembedded docs change selected %q with rows %s, want nothing and []", b, out.String())
	}
}

// TestDeletedEmbeddedFileSelectsItsOwner: a deleted file is in no list go
// list makes on HEAD, so it is matched against the embed patterns: one a
// glob covers (runtime/*.nut) and one under an embedded directory (help)
// each seed their owner's dependents; a deleted file no pattern covers seeds
// nothing.
func TestDeletedEmbeddedFileSelectsItsOwner(t *testing.T) {
	t.Parallel()
	gone := func(string) bool { return false }
	got := selectPackages(embedFixture, nil, nil, []string{"internal/codegen/squirrel/runtime/gone.nut"}, gone)
	for _, want := range []string{"./compiler", "./internal/goldens"} {
		if !slices.Contains(got, want) {
			t.Errorf("deleting an embedded runtime file selected %v, without %s", got, want)
		}
	}
	got = selectPackages(embedFixture, nil, nil, []string{"cmd/schema/help/old/page.md"}, gone)
	if !slices.Contains(got, "./internal/ci") {
		t.Errorf("deleting a file under the embedded help directory selected %v, without ./internal/ci", got)
	}
	if got := selectPackages(embedFixture, nil, nil, []string{"internal/codegen/squirrel/README.md"}, gone); len(got) != 0 {
		t.Errorf("deleting a file no pattern covers selected %v", got)
	}
}
