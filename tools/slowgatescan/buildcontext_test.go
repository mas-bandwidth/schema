package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestCreditFollowsTheLinesBuildContext is the crediting half of the
// reviewer's F-030: an armed line that builds under a context other than the
// lane's is credited only with the tests that context builds. `go test -race`
// does not build a test behind `//go:build !race`, so a race recipe selecting
// it runs nothing of it, and TestNoRace falls through to the later recipe that
// does build it (a pinned toolchain, whose release tags build both files). The
// control, TestBoth, is built either way and stays credited to the race line.
func TestCreditFollowsTheLinesBuildContext(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pkg/both_test.go":   "package pkg\n\nimport \"testing\"\n\nfunc TestBoth(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
		"pkg/norace_test.go": "//go:build !race\n\npackage pkg\n\nimport \"testing\"\n\nfunc TestNoRace(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
		"Makefile": "race:\n\tSCHEMA_SLOW=1 go test -race -count=1 -run '^Test(NoRace|Both)$$' ./pkg/\n" +
			// a pinned toolchain whose release tags build both: credited
			"pinned:\n\tGOTOOLCHAIN=go1.26.0 sh test/slowgate/proof pinned 'TestNoRace' ./pkg -run '^TestNoRace$$' -count=1\n",
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
	for name, want := range map[string]string{"TestNoRace": "pinned", "TestBoth": "race"} {
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
// none; a `coverage` row is the whole accounting's exit code over a committed
// transcript, run in a scratch copy of the tree; a `gated` row is the set of
// gated packages. Every reported verdict sits beside a clean control in the
// same file.
func TestSlowGateScanCorpus(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "corpus")
	data, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	red, green := 0, 0
	tally := func(refused bool) {
		if refused {
			red++
		} else {
			green++
		}
	}
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv line %d: %d fields, want 5 (id, tree, check, subject, want)", n+1, len(f))
		}
		id, tree, check, subject, want := f[0], filepath.Join(dir, f[1]), f[2], f[3], f[4]
		if check == "gated" {
			got := "-"
			if gated, err := gatedPackages(tree); err == nil {
				got = strings.Join(slices.Sorted(maps.Keys(gated)), " ")
			}
			if got != want {
				t.Errorf("%s: the gated packages of %s are %q, want %q", id, f[1], got, want)
			}
			tally(want == "-")
			continue
		}
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
			tally(want != "0")
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
			tally(want == "-")
		case "coverage":
			parts := strings.Split(subject, ":")
			if len(parts) < 2 {
				t.Fatalf("%s: subject %q is not transcript:go-exit[:blocked]", id, subject)
			}
			goExit, err := strconv.Atoi(parts[1])
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			scratch := t.TempDir()
			if err := os.CopyFS(scratch, os.DirFS(tree)); err != nil {
				t.Fatal(err)
			}
			table := filepath.Join(scratch, "build", "slowgate", "coverage.tsv")
			if len(parts) > 2 && parts[2] == "blocked" {
				if err := os.MkdirAll(table, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := strconv.Itoa(runCoverage(scratch, filepath.Join(scratch, parts[0]), goExit, recipes, nil, nil))
			if got != want {
				t.Errorf("%s: the accounting over %s exits %s, want %s", id, subject, got, want)
			}
			if want == "0" {
				if b, err := os.ReadFile(table); err != nil || strings.Count(string(b), "\n") < 2 {
					t.Errorf("%s: a green accounting left no table at %s (%v)", id, table, err)
				}
			}
			tally(want != "0")
		default:
			t.Fatalf("%s: unknown check %q", id, check)
		}
	}
	if red == 0 || green == 0 {
		t.Errorf("the corpus holds %d refused and %d clean rows: a refusal is only proven beside a clean control", red, green)
	}
}

// TestWorkflowRefusalIsScopedToTheStepThatSetsIt: a working directory or a Go
// environment variable this scanner does not resolve voids the step that sets
// it, not the whole file. One GOFLAGS on ci-full.yml's big-endian row used to
// void every step of the file, and the 1320 gated codegen tests its
// unit-codegen rows run were credited only through a recipe that ran another
// module's `./...`. Set at the workflow's or a job's level, every step
// inherits it, and the file still earns nothing. A job's own env is not the
// previous job's last step's.
func TestWorkflowRefusalIsScopedToTheStepThatSetsIt(t *testing.T) {
	t.Parallel()
	slowStep := "      - name: slow\n        env:\n          SCHEMA_SLOW: '1'\n        run: go test ./pkg -run '^TestSlow$'\n"
	for _, tc := range []struct {
		name, workflow string
		want           bool
	}{
		{"a step sets GOFLAGS, another step runs the test", "name: w\njobs:\n  a:\n    runs-on: x\n    steps:\n      - name: big-endian\n        env:\n          GOFLAGS: -gcflags=all=-l\n        run: make conformance\n" + slowStep, true},
		{"the step that runs the test sets GOFLAGS", "name: w\njobs:\n  a:\n    runs-on: x\n    steps:\n      - name: slow\n        env:\n          SCHEMA_SLOW: '1'\n          GOFLAGS: -count=0\n        run: go test ./pkg -run '^TestSlow$'\n", false},
		{"the step that runs the test sets a working directory", "name: w\njobs:\n  a:\n    runs-on: x\n    steps:\n" + slowStep + "        working-directory: sub\n", false},
		{"the step sets GOTOOLCHAIN after its run", "name: w\njobs:\n  a:\n    runs-on: x\n    steps:\n" + slowStep + "        env:\n          GOTOOLCHAIN: go1.20.0\n", false},
		{"the workflow sets GOFLAGS", "name: w\nenv:\n  GOFLAGS: -count=0\njobs:\n  a:\n    runs-on: x\n    steps:\n" + slowStep, false},
		{"the job sets GOFLAGS", "name: w\njobs:\n  a:\n    runs-on: x\n    env:\n      GOFLAGS: -count=0\n    steps:\n" + slowStep, false},
		{"the job's defaults set a working directory", "name: w\njobs:\n  a:\n    runs-on: x\n    defaults:\n      run:\n        working-directory: sub\n    steps:\n" + slowStep, false},
		{"a job's env after another job's last step", "name: w\njobs:\n  a:\n    runs-on: x\n    steps:\n      - name: plain\n        run: go test ./pkg -run '^TestSlow$'\n  b:\n    runs-on: x\n    env:\n      SCHEMA_SLOW: '1'\n    steps:\n      - run: echo b\n", false},
		{"GOFLAGS named inside a run script is the script's", "name: w\njobs:\n  a:\n    runs-on: x\n    steps:\n      - name: note\n        run: |\n          echo GOFLAGS: none\n" + slowStep, true},
	} {
		steps, err := parseWorkflowContent("w.yml", tc.workflow)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := coveredByCI(steps, gatedTest{pkg: "./pkg", name: "TestSlow"}) != ""; got != tc.want {
			t.Errorf("%s: TestSlow credited = %v, want %v", tc.name, got, tc.want)
		}
	}
}
