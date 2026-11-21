package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestCreditFollowsTheLinesBuildContext is the crediting half of the
// reviewer's F-030: an armed line that builds under a context other than the
// lane's is credited only with the tests that context builds. `go test -race`
// does not build a test behind `//go:build !race`, so a race recipe selecting
// it runs nothing of it; the control, TestBoth, is built either way and stays
// credited.
func TestCreditFollowsTheLinesBuildContext(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pkg/both_test.go":   "package pkg\n\nimport \"testing\"\n\nfunc TestBoth(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
		"pkg/norace_test.go": "//go:build !race\n\npackage pkg\n\nimport \"testing\"\n\nfunc TestNoRace(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
		"Makefile":           "race:\n\tSCHEMA_SLOW=1 go test -race -count=1 -run '^Test(NoRace|Both)$$' ./pkg/\n",
	})
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	recipes, err := scanMakefiles(root, gated)
	if err != nil {
		t.Fatal(err)
	}
	decls := newDeclCache(root)
	for name, want := range map[string]string{"TestNoRace": "", "TestBoth": "race"} {
		if got := coveredBy(decls, recipes, gatedTest{pkg: "./pkg", name: name}); got != want {
			t.Errorf("%s is credited to %q, want %q (-race builds under the race tag)", name, got, want)
		}
	}
}

// TestCreditReadsSelectorsAsGoRewritesThem is the reviewer's F-219 on this
// scan: Go rewrites a space in a selector to `_`, so a recipe with
// `-skip 'TestWin Codec'` does not run TestWin_Codec and must not be credited
// with it. The control, TestWin_Other, is run by the same line and stays
// credited. A workflow step's grammar admits no space in a selector (fields
// split at blanks, a bare value is one word), so a step with that -skip is
// not read at all and credits neither test.
func TestCreditReadsSelectorsAsGoRewritesThem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pkg/win_test.go": "package pkg\n\nimport \"testing\"\n\nfunc TestWin_Codec(t *testing.T) { slowtest.Gate(t, \"x\") }\nfunc TestWin_Other(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
		"Makefile":        "core:\n\tSCHEMA_SLOW=1 go test -count=1 -run '^TestWin' -skip 'TestWin Codec' ./pkg/\n",
	})
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	recipes, err := scanMakefiles(root, gated)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := parseWorkflowContent("w.yml", "name: w\njobs:\n  slow:\n    runs-on: ubuntu-latest\n    steps:\n      - name: core\n        env:\n          SCHEMA_SLOW: '1'\n        run: go test ./pkg -run '^TestWin' -skip 'TestWin Codec'\n")
	if err != nil {
		t.Fatal(err)
	}
	decls := newDeclCache(root)
	for name, want := range map[string]string{"TestWin_Codec": "", "TestWin_Other": "core"} {
		test := gatedTest{pkg: "./pkg", name: name}
		if got := coveredBy(decls, recipes, test); got != want {
			t.Errorf("recipe: %s is credited to %q, want %q (go test rewrites the space to _)", name, got, want)
		}
		if got := coveredByCI(steps, test); got != "" {
			t.Errorf("workflow step: %s is credited to %q by a step whose -skip holds a space, which the step grammar cannot read", name, got)
		}
	}
}

// TestSlowGateScanCorpus runs the scan over the committed corpus
// (testdata/corpus): each row names a fixture tree, what is judged there, and
// the verdict. A `static` row is the unarmed-line audit's exit code; a
// `credit` row is the make target credited with running a gated test, `-` for
// none. Every reported verdict sits beside a clean control in the same file.
func TestSlowGateScanCorpus(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "corpus")
	data, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	red, green := 0, 0
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv line %d: %d fields, want 5 (id, tree, check, subject, want)", n+1, len(f))
		}
		id, tree, check, subject, want := f[0], filepath.Join(dir, f[1]), f[2], f[3], f[4]
		gatedTests, err := gatedTestPackages(tree)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		gated, err := gatedPackagesWith(tree, gatedTests)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		recipes, err := scanMakefiles(tree, gated)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		switch check {
		case "static":
			// subject is the one recipe target the tree holds
			if len(recipes) != 1 || recipes[0].target != subject || recipes[0].armed {
				t.Fatalf("%s: want one unarmed recipe %q, got %+v", id, subject, recipes)
			}
			got := strconv.Itoa(runStatic(tree, recipes, nil, gated, gatedTests))
			if got != want {
				t.Errorf("%s: the static scan of %s exits %s, want %s", id, f[1], got, want)
			}
			if want == "0" {
				green++
			} else {
				red++
			}
		case "credit":
			pkg, name, ok := strings.Cut(subject, ".")
			if !ok {
				t.Fatalf("%s: subject %q is not pkg.Test", id, subject)
			}
			got := coveredBy(newDeclCache(tree), recipes, gatedTest{pkg: "./" + pkg, name: name})
			if got == "" {
				got = "-"
			}
			if got != want {
				t.Errorf("%s: %s in %s is credited to %q, want %q", id, subject, f[1], got, want)
			}
			if want == "-" {
				red++
			} else {
				green++
			}
		default:
			t.Fatalf("%s: unknown check %q", id, check)
		}
	}
	if red == 0 || green == 0 {
		t.Errorf("the corpus holds %d refused and %d clean rows: a refusal is only proven beside a clean control", red, green)
	}
}
