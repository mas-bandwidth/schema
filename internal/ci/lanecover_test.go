package ci

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// TestEveryGoTestIsAssignedToALane is the gate for one rule: a Go test that
// exists in this repository is run by some CI lane that starts without a
// person, per-commit or nightly, and this test proves it for each one by name.
//
// WHAT IT CLOSED (measured on main at f397f5f77, 2026-11-21): 6638 tests in
// 3323 _test.go files. Two were run by no lane, and 83 more were run by lanes
// that no gate watched:
//
//   - tools/treelock/treelock_windows_test.go (1 test) is `//go:build
//     windows`. No Linux row compiles it, and ci-full.yml's windows job ran
//     `go vet` and `go build` and no test. It had never run in CI.
//   - test/conformance/go/rows: TestRowR20FloatWidensExactly was in neither
//     half of conformance-go-rows-a / -b, whose -run lists are written out
//     by hand. It ran only as a subtest of TestRowR20.
//   - 83 tests live in the nested modules under tables/cards and
//     test/conformance/go. They do run (cards-test and conformance-go-rows),
//     and nothing checked that: `go list ./...` from the root stops at a
//     nested go.mod, so every existing gate (tools/slowgatescan's
//     TestCIFullGoTestRunsEveryTestExactlyOnce, tools/gotestrows) counts only
//     the main module and would stay green if those recipes were deleted.
//
// HOW IT DECIDES. lanecover_engine_test.go reads every workflow and every make
// rule. A workflow counts when it has a schedule (nightly) or runs on a push
// with no path filter (per-commit); one only a person can dispatch does not,
// and neither does a job or a step under `if: false` or under a condition on
// the event being a dispatch. A nightly lane therefore counts exactly when
// its workflow file exists, is scheduled, and runs the test. A make recipe
// counts when a counted step runs its target, directly or through
// prerequisites. For each `go test` it finds, the toolchain (`go list` under
// the job's GOOS and the command's -tags) says which test files the command
// compiles; -run and -skip say which tests in them it keeps.
//
// WHAT IS RED:
//   - a test no command selects, by file and name;
//   - a _test.go file no lane's build compiles at all (a tag or a GOOS that
//     nothing sets), with its test count;
//   - a make goal a counted step names that no rule declares: a lane that
//     runs nothing;
//   - a test file the toolchain reports that the inventory did not walk, or
//     an empty inventory, or no command at all: the gate looked at less than
//     it should.
//
// A command the reader cannot read (a package list in a shell variable, an
// -overlay sabotage build, a command under `if`) is credited with nothing.
// So is the per-commit `go test $PACKAGES` of go-test-touched, by design: it
// runs what a diff touches, which is no promise about any one test. The
// count of such commands is logged on every run.
//
// IT DOES NOT ASK whether a selected test then skips itself (the SCHEMA_SLOW
// accounting in tools/slowgatescan and the skip ledgers own that), and it
// does not require exactly once: the per-row exactly-once rule for ci-full's
// go-test job stays in tools/slowgatescan. It logs how many tests more than
// one command selects (6546 of them on the day it was written: certify.yml's
// plain `go test ./...` and the per-target make gates select again what the
// ci-full rows already ran).
func TestEveryGoTestIsAssignedToALane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, wfs, err := laneLoad(root)
	if err != nil {
		t.Fatal(err)
	}
	res, set, err := laneVerdict(root, mk, wfs, realCompiled(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d tests in %d _test.go files (%d fixture files under testdata set aside); %d build contexts",
		res.tests, res.files, res.fixtures, res.contexts)
	t.Logf("%d go test commands reached from %d counted workflows: %d readable, %d credited with nothing",
		len(set.cmds), set.workflows, len(set.cmds)-len(set.unread), len(set.unread))
	t.Logf("assigned: %d per-commit, %d nightly only, %d nowhere; %d selected by more than one command",
		res.perCommit, res.nightly, len(res.nowhere), res.twice)
	if dump := os.Getenv("LANECOVER_DUMP"); dump != "" {
		var b strings.Builder
		for _, c := range set.cmds {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", c.src, c.workflow, c.tier, c.target, c.hits, c.unread, c.text)
		}
		for name, n := range res.selectedBy {
			fmt.Fprintf(&b, "selected\t%d\t%s\n", n, name)
		}
		if err := os.WriteFile(dump, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range res.errors() {
		t.Error(e)
	}
}

// errors is the verdict as lines: empty when every test has a lane.
func (res *laneResult) errors() []string {
	var out []string
	out = append(out, res.blind...)
	for _, s := range res.stranded {
		out = append(out, s+": no CI lane's build compiles this file, so its tests run nowhere. A lane must run `go test` with the build tag or on the GOOS its //go:build line names.")
	}
	for _, n := range res.nowhere {
		out = append(out, n+" is run by no CI lane: no `go test` reached from a scheduled or per-commit workflow compiles its file and selects it. Add it to the lane that should run it; do not add a list here.")
	}
	for _, m := range res.makeMiss {
		out = append(out, m+": a counted workflow step runs a make goal no rule declares, so that step runs nothing or fails")
	}
	return out
}

// laneLoad reads the make rules and the workflow files of a tree.
func laneLoad(root string) (*laneMake, map[string]string, error) {
	mk, err := laneReadMake(root)
	if err != nil {
		return nil, nil, err
	}
	files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		return nil, nil, err
	}
	wfs := map[string]string{}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		wfs[filepath.Base(f)] = string(body)
	}
	return mk, wfs, nil
}

// laneVerdict runs the whole gate over a tree: the workflows as given (so a
// control can change one), the make rules, the files on disk.
func laneVerdict(root string, mk *laneMake, workflows map[string]string, compiled func(laneContext) (map[string]map[string]bool, error)) (*laneResult, *laneSet, error) {
	wfs := map[string]laneWorkflow{}
	for name, body := range workflows {
		wfs[name] = laneReadWorkflow(name, body)
	}
	set := laneBuild(mk, wfs)
	files, err := laneInventory(root)
	if err != nil {
		return nil, nil, err
	}
	res, err := laneAssignAll(files, set, compiled)
	if err != nil {
		return nil, nil, err
	}
	res.makeMiss = set.makeMiss
	// the gate proves it looked: at some workflow, some command, some test,
	// and at every test file the toolchain itself reports
	if set.workflows == 0 {
		res.blind = append(res.blind, "no workflow under .github/workflows is scheduled or runs on push: this gate read no lane")
	}
	if len(set.cmds) == len(set.unread) {
		res.blind = append(res.blind, "no readable `go test` command is reached from a counted workflow: this gate read no lane")
	}
	if res.tests == 0 {
		res.blind = append(res.blind, "the inventory holds no test: this gate would pass over an empty set")
	}
	walked := map[string]bool{}
	for _, f := range files {
		walked[f.dir+"/"+f.name] = true
	}
	plain, err := compiled(laneContext{goos: "linux"})
	if err != nil {
		return nil, nil, err
	}
	for dir, names := range plain {
		for name := range names {
			if !walked[dir+"/"+name] {
				res.blind = append(res.blind, dir+"/"+name+": the toolchain compiles this test file and the inventory did not walk it")
			}
		}
	}
	slices.Sort(res.blind)
	return res, set, nil
}

var realCompiledCache sync.Map

// realCompiled asks the toolchain, once per build context for the whole
// package's tests.
func realCompiled(root string) func(laneContext) (map[string]map[string]bool, error) {
	type entry struct {
		once sync.Once
		got  map[string]map[string]bool
		err  error
	}
	return func(ctx laneContext) (map[string]map[string]bool, error) {
		v, _ := realCompiledCache.LoadOrStore(root+"\x00"+ctx.goos+"\x00"+ctx.tags, &entry{})
		e := v.(*entry)
		e.once.Do(func() { e.got, e.err = laneCompiled(root, ctx) })
		return e.got, e.err
	}
}

// ------------------------------------------------- controls on the real tree

// TestLaneCoverageGoesRedOnTheRealLanes is the reversed witness over this
// repository's own files: each case takes one lane away in memory and the
// gate must name the tests that lane was the only one to run.
func TestLaneCoverageGoesRedOnTheRealLanes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, wfs, err := laneLoad(root)
	if err != nil {
		t.Fatal(err)
	}
	nowhere := func(t *testing.T, mk *laneMake, wfs map[string]string) []string {
		t.Helper()
		res, _, err := laneVerdict(root, mk, wfs, realCompiled(root))
		if err != nil {
			t.Fatal(err)
		}
		return res.errors()
	}
	edit := func(file, old, new string) map[string]string {
		t.Helper()
		if !strings.Contains(wfs[file], old) {
			t.Fatalf("%s no longer holds %q: this control edits a line that is gone", file, old)
		}
		out := map[string]string{}
		maps.Copy(out, wfs)
		out[file] = strings.Replace(wfs[file], old, new, 1)
		return out
	}
	has := func(t *testing.T, errs []string, want string) {
		t.Helper()
		for _, e := range errs {
			if strings.Contains(e, want) {
				return
			}
		}
		t.Errorf("the gate stayed green for %s (%d errors)", want, len(errs))
	}

	t.Run("the windows job loses its test step", func(t *testing.T) {
		t.Parallel()
		errs := nowhere(t, mk, edit("ci-full.yml", "      - run: go test ./tools/treelock/ -count=1 -run '^TestTreelockWindowsRefusal$'\n", ""))
		has(t, errs, "tools/treelock/treelock_windows_test.go: TestTreelockWindowsRefusal is run by no CI lane")
		has(t, errs, "tools/treelock/treelock_windows_test.go (1 tests): no CI lane's build compiles this file")
	})
	t.Run("a nightly that is no longer scheduled", func(t *testing.T) {
		t.Parallel()
		out := map[string]string{}
		for k, v := range wfs {
			out[k] = strings.Replace(v, "\n  schedule:\n", "\n  workflow_call:\n", 1)
		}
		errs := nowhere(t, mk, out)
		// every test only a nightly ran is now run by nothing
		if len(errs) < 1000 {
			t.Errorf("with no workflow scheduled the gate named %d tests, want the thousands only nightlies run", len(errs))
		}
		has(t, errs, "compiler/")
	})
	t.Run("a nightly workflow file that does not exist", func(t *testing.T) {
		t.Parallel()
		out := map[string]string{}
		for k, v := range wfs {
			if k != "ci-full.yml" {
				out[k] = v
			}
		}
		has(t, nowhere(t, mk, out), "tools/treelock/treelock_windows_test.go: TestTreelockWindowsRefusal is run by no CI lane")
	})
	t.Run("a conformance row dropped from its hand-written list", func(t *testing.T) {
		t.Parallel()
		mk2, err := laneReadMake(root)
		if err != nil {
			t.Fatal(err)
		}
		edited := 0
		for _, r := range mk2.rules {
			for i := range r.recipe {
				if strings.Contains(r.recipe[i].text, "proof conformance-go-rows-b") {
					r.recipe[i].text = strings.ReplaceAll(r.recipe[i].text, "|TestRowR20FloatWidensExactly|", "|")
					edited++
				}
			}
		}
		if edited != 1 {
			t.Fatalf("edited %d conformance-go-rows-b recipes, want 1", edited)
		}
		has(t, nowhere(t, mk2, wfs), "test/conformance/go/rows/R20_test.go: TestRowR20FloatWidensExactly is run by no CI lane")
	})
	t.Run("the cards module's recipe gone", func(t *testing.T) {
		t.Parallel()
		mk2, err := laneReadMake(root)
		if err != nil {
			t.Fatal(err)
		}
		edited := 0
		for _, r := range mk2.rules {
			for i := range r.recipe {
				if strings.HasPrefix(r.recipe[i].text, "cd build/tables-generated-go/cards && go vet ./... && ") {
					r.recipe[i].text = "cd build/tables-generated-go/cards && go vet ./..."
					edited++
				}
			}
		}
		if edited != 1 {
			t.Fatalf("edited %d cards-test recipes, want 1", edited)
		}
		has(t, nowhere(t, mk2, wfs), "tables/cards/cards_test.go: ")
	})
}

// ------------------------------------------------- controls on a planted tree

// laneFixture writes a small repository into a temporary directory: two
// packages, a tagged test file, a Windows-only one, a nested module, a
// Makefile and the workflows a case names.
func laneFixture(t *testing.T, makefile string, workflows map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+laneModulePath+"\n\ngo 1.26\n")
	write("a/a.go", "package a\n")
	write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\nfunc TestB(t *testing.T) {}\nfunc FuzzC(f *testing.F) {}\nfunc TestMain(m *testing.M) {}\nfunc Testhelper(t *testing.T) {}\nconst src = `\nfunc TestInAString(t *testing.T) {}\n`\n")
	write("b/b.go", "package b\n")
	write("b/b_test.go", "package b\n\nimport \"testing\"\n\nfunc TestPlain(t *testing.T) {}\n")
	write("b/tagged_test.go", "//go:build special\n\npackage b\n\nimport \"testing\"\n\nfunc TestTagged(t *testing.T) {}\n")
	write("w/w.go", "package w\n")
	write("w/only_windows_test.go", "//go:build windows\n\npackage w\n\nimport \"testing\"\n\nfunc TestWin(t *testing.T) {}\n")
	write("nested/go.mod", "module nested\n\ngo 1.26\n")
	write("nested/n_test.go", "package nested\n\nimport \"testing\"\n\nfunc TestNested(t *testing.T) {}\n")
	write("a/testdata/fixture_test.go", "this is a fixture, not Go\n")
	write("Makefile", makefile)
	for name, body := range workflows {
		write(".github/workflows/"+name, body)
	}
	return root
}

const laneGoodMakefile = `SPECIAL := special
test-tagged:
	go test -count=1 -tags $(SPECIAL) ./b/ \
		-run '^TestTagged$$'
test-nested:
	cd nested && go test -count=1 .
test-all: test-tagged test-nested
orphan:
	go test ./a/
`

const laneGoodNightly = `name: nightly
on:
  schedule:
    - cron: '1 2 * * *'
  workflow_dispatch:
jobs:
  unit:
    runs-on: [self-hosted, Linux]
    strategy:
      matrix:
        group: [ one, two, rest ]
    steps:
      - name: row one
        if: matrix.group == 'one'
        run: go test ./a -run '^TestA$'
      - name: row two
        if: matrix.group == 'two'
        run: go test ./a -skip '^TestA$'
      - name: the rest
        if: matrix.group == 'rest'
        run: go test $(go list ./... | grep -v '/a$')
      - name: tagged and nested
        run: |
          # go test ./never
          make -j4 test-all V=1
  windows:
    runs-on: windows-latest
    steps:
      - run: go test ./w/
`

func laneFixtureErrors(t *testing.T, makefile string, workflows map[string]string) []string {
	t.Helper()
	root := laneFixture(t, makefile, workflows)
	mk, wfs, err := laneLoad(root)
	if err != nil {
		t.Fatal(err)
	}
	res, _, err := laneVerdict(root, mk, wfs, func(ctx laneContext) (map[string]map[string]bool, error) {
		return laneCompiled(root, ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.errors()
}

// TestLaneCoverageControls plants one fault per case in a small tree and
// requires the gate to name exactly the tests the fault strands. The first
// case is the same tree with no fault: green, and every later case differs
// from it by the one edit its name says.
func TestLaneCoverageControls(t *testing.T) {
	t.Parallel()
	const none = " is run by no CI lane"
	edit := func(s, old, new string) string {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("the fixture no longer holds %q", old)
		}
		return strings.Replace(s, old, new, 1)
	}
	for _, c := range []struct {
		name     string
		makefile string
		nightly  string
		want     []string // each must appear in one error; the count must match
	}{
		{name: "every test has a lane", makefile: laneGoodMakefile, nightly: laneGoodNightly},
		{
			name: "a -skip twin drifts from its -run row", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "-skip '^TestA$'", "-skip '^Test[AB]$'"),
			want:    []string{"a/a_test.go: TestB" + none},
		},
		{
			name: "a complement that drops a package nobody else runs", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "grep -v '/a$'", "grep -v '/a$' | grep -v '/b$'"),
			want:    []string{"b/b_test.go: TestPlain" + none},
		},
		{
			name: "no lane sets the build tag", makefile: edit(laneGoodMakefile, "-tags $(SPECIAL) ", ""),
			nightly: laneGoodNightly,
			want:    []string{"b/tagged_test.go (1 tests): no CI lane's build compiles this file", "b/tagged_test.go: TestTagged" + none},
		},
		{
			name: "the tag lane's -run names another test", makefile: edit(laneGoodMakefile, "'^TestTagged$$'", "'^TestOther$$'"),
			nightly: laneGoodNightly,
			want:    []string{"b/tagged_test.go: TestTagged" + none},
		},
		{
			name: "the make target is run by no workflow", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "make -j4 test-all V=1", "make -j4 test-nested V=1"),
			want:    []string{"b/tagged_test.go (1 tests): no CI lane's build compiles this file", "b/tagged_test.go: TestTagged" + none},
		},
		{
			name: "the workflow names a make goal that does not exist", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "make -j4 test-all V=1", "make -j4 test-all test-gone V=1"),
			want:    []string{"nightly.yml:23: make test-gone: a counted workflow step runs a make goal no rule declares"},
		},
		{
			name: "the nested module is run from the root", makefile: edit(laneGoodMakefile, "cd nested && go test -count=1 .", "go test -count=1 ./nested/"),
			nightly: laneGoodNightly,
			want:    []string{"nested/n_test.go: TestNested" + none},
		},
		{
			name: "the windows test is run on linux only", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "runs-on: windows-latest", "runs-on: ubuntu-24.04"),
			want:    []string{"w/only_windows_test.go (1 tests): no CI lane's build compiles this file", "w/only_windows_test.go: TestWin" + none},
		},
		{
			name: "the step is under if: false", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "        if: matrix.group == 'two'", "        if: false"),
			want:    []string{"a/a_test.go: FuzzC" + none, "a/a_test.go: TestB" + none},
		},
		{
			name: "the job runs only when a person dispatches it", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "  windows:\n", "  windows:\n    if: github.event_name == 'workflow_dispatch'\n"),
			want:    []string{"w/only_windows_test.go (1 tests): no CI lane's build compiles this file", "w/only_windows_test.go: TestWin" + none},
		},
		{
			name: "the package list is a shell variable", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "go test ./a -skip '^TestA$'", "go test $PACKAGES -skip '^TestA$'"),
			want:    []string{"a/a_test.go: FuzzC" + none, "a/a_test.go: TestB" + none},
		},
		{
			name: "the command is a negative control's condition", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "go test ./a -skip '^TestA$'", "if go test ./a -skip '^TestA$'; then exit 1; fi"),
			want:    []string{"a/a_test.go: FuzzC" + none, "a/a_test.go: TestB" + none},
		},
		{
			name: "the command builds over an overlay", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "go test ./a -skip '^TestA$'", "go test -overlay o.json ./a -skip '^TestA$'"),
			want:    []string{"a/a_test.go: FuzzC" + none, "a/a_test.go: TestB" + none},
		},
		{
			name: "the command only lists", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "go test ./a -skip '^TestA$'", "go test ./a -list . -skip '^TestA$'"),
			want:    []string{"a/a_test.go: FuzzC" + none, "a/a_test.go: TestB" + none},
		},
		{
			name: "the command is echoed", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "run: go test ./a -run '^TestA$'", "run: echo go test ./a -run '^TestA$'"),
			want:    []string{"a/a_test.go: TestA" + none},
		},
		{
			name: "the command runs in another directory", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "run: go test ./a -run '^TestA$'", "run: cd b && go test ./a -run '^TestA$'"),
			want:    []string{"a/a_test.go: TestA" + none},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			errs := laneFixtureErrors(t, c.makefile, map[string]string{"nightly.yml": c.nightly})
			if len(errs) != len(c.want) {
				t.Errorf("got %d errors, want %d:\n%s", len(errs), len(c.want), strings.Join(errs, "\n"))
			}
			for _, w := range c.want {
				if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, w) }) {
					t.Errorf("no error holds %q; got:\n%s", w, strings.Join(errs, "\n"))
				}
			}
		})
	}
}

// TestLaneCoverageCountsOnlyWorkflowsThatStartThemselves: the same lanes in a
// workflow only a person can dispatch, or one filtered by path, run nothing as
// far as this gate is concerned; a reusable workflow counts when a counted
// one calls it; and a tree with no lane at all says so instead of passing.
func TestLaneCoverageCountsOnlyWorkflowsThatStartThemselves(t *testing.T) {
	t.Parallel()
	dispatch := strings.Replace(laneGoodNightly, "  schedule:\n    - cron: '1 2 * * *'\n", "", 1)
	if errs := laneFixtureErrors(t, laneGoodMakefile, map[string]string{"manual.yml": dispatch}); len(errs) != 11 ||
		!slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, "this gate read no lane") }) {
		t.Errorf("a dispatch-only workflow: got %d errors, want the 7 tests, 2 stranded files and the two no-lane lines:\n%s", len(errs), strings.Join(errs, "\n"))
	}
	paths := strings.Replace(laneGoodNightly, "  schedule:\n    - cron: '1 2 * * *'\n", "  push:\n    paths: [ 'a/**' ]\n", 1)
	if errs := laneFixtureErrors(t, laneGoodMakefile, map[string]string{"paths.yml": paths}); len(errs) != 11 {
		t.Errorf("a path-filtered push workflow: got %d errors, want 11:\n%s", len(errs), strings.Join(errs, "\n"))
	}
	push := strings.Replace(laneGoodNightly, "  schedule:\n    - cron: '1 2 * * *'\n", "  push:\n    branches: [main]\n", 1)
	if errs := laneFixtureErrors(t, laneGoodMakefile, map[string]string{"push.yml": push}); len(errs) != 0 {
		t.Errorf("a push workflow: got errors, want none:\n%s", strings.Join(errs, "\n"))
	}
	callee := strings.Replace(laneGoodNightly, "  schedule:\n    - cron: '1 2 * * *'\n  workflow_dispatch:\n", "  workflow_call:\n", 1)
	caller := "name: caller\non:\n  schedule:\n    - cron: '1 2 * * *'\njobs:\n  call:\n    uses: ./.github/workflows/callee.yml\n"
	if errs := laneFixtureErrors(t, laneGoodMakefile, map[string]string{"callee.yml": callee, "caller.yml": caller}); len(errs) != 0 {
		t.Errorf("a reusable workflow a nightly calls: got errors, want none:\n%s", strings.Join(errs, "\n"))
	}
	if errs := laneFixtureErrors(t, laneGoodMakefile, map[string]string{"callee.yml": callee}); len(errs) != 11 {
		t.Errorf("a reusable workflow nobody calls: got %d errors, want 11:\n%s", len(errs), strings.Join(errs, "\n"))
	}
}

// TestLaneInventoryReadsWhatGoTestLists holds the inventory to the
// toolchain's own notion of a test: the right name and signature, not a
// function in a string literal, and an example only with an output comment.
func TestLaneInventoryReadsWhatGoTestLists(t *testing.T) {
	t.Parallel()
	src := "package p\n\nimport \"testing\"\n\n" +
		"func TestA(t *testing.T) {}\n" +
		"func Test(t *testing.T) {}\n" +
		"func Test_under(\n\tt *testing.T,\n) {\n}\n" +
		"func Testlower(t *testing.T) {}\n" +
		"func TestMain(m *testing.M) {}\n" +
		"func TestTwo(t *testing.T, n int) {}\n" +
		"func (s S) TestMethod(t *testing.T) {}\n" +
		"func FuzzF(f *testing.F) {}\n" +
		"func BenchmarkB(b *testing.B) {}\n" +
		"func ExampleRuns() {\n\t// Output: x\n}\n" +
		"func ExampleCompiledOnly() {}\n" +
		"const s = `\nfunc TestInAString(t *testing.T) {}\n`\n"
	got, err := laneTestsIn("p_test.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestA", "Test", "Test_under", "FuzzF", "ExampleRuns"}
	if !slices.Equal(got, want) {
		t.Errorf("tests = %v, want %v", got, want)
	}
}

// TestLaneCommandReader holds the `go test` reader to the shapes this
// repository's lanes use, and to crediting nothing for a shape it cannot read.
func TestLaneCommandReader(t *testing.T) {
	t.Parallel()
	one := func(t *testing.T, script string) laneCmd {
		t.Helper()
		cmds, _, _ := laneScript("x:1", script, ".")
		if len(cmds) != 1 {
			t.Fatalf("%q: read %d go test commands, want 1", script, len(cmds))
		}
		return cmds[0]
	}
	for _, c := range []struct {
		script   string
		pkg      string
		name     string
		selected bool
	}{
		{"go test ./compiler", "compiler", "TestA", true},
		{"go test ./compiler", "compiler/sub", "TestA", false},
		{"go test ./compiler/...", "compiler/sub", "TestA", true},
		{"go test ./...", "x/y", "TestA", true},
		{"go test", ".", "TestA", true},
		{"go test -v -count=1 -timeout 600s ./a/ ./b/", "b", "TestA", true},
		{"SCHEMA_SLOW=1 env X=1 go test ./a -run '^(TestA|TestB)$'", "a", "TestB", true},
		{"go test ./a -run '^(TestA|TestB)$'", "a", "TestC", false},
		{"go test ./a -run=TestA/sub", "a", "TestA", true},
		{"go test ./a -skip '^TestA$'", "a", "TestA", false},
		{"go test ./a -skip '^TestA$/sub'", "a", "TestA", true},
		{"go test ./a -run 'Test[A-C]' -skip 'TestB'", "a", "TestB", false},
		{"go test $(go list ./... | grep -v '/a$' | grep -vE '/(c|d)$')", "b", "TestA", true},
		{"go test $(go list ./... | grep -v '/a$' | grep -vE '/(c|d)$')", "a", "TestA", false},
		{"go test $(go list ./... | grep -v '/a$' | grep -vE '/(c|d)$')", "d", "TestA", false},
		{"go test $(go list ./x/... | grep -v '/a$')", "b", "TestA", false},
		{"go test " + laneModulePath + "/a", "a", "TestA", true},
		{"cd a && go test ./b", "a/b", "TestA", true},
		{"cd a && go test ./b", "b", "TestA", false},
		{"sh ./test/slowgate/proof label 'TestA TestB' ./a/ -count=1 -run '^(TestA|TestB)$'", "a", "TestB", true},
		{"go test ./a -soak-iterations 5 ./b", "b", "TestA", false},
		{"go test ./a 2>&1 | tee log", "a", "TestA", true},
		// credited with nothing
		{"go test $PACKAGES", "a", "TestA", false},
		{"go test ./a -run \"$RUN\"", "a", "TestA", false},
		{"go test ./a -tags \"$TAGS\"", "a", "TestA", false},
		{"go test ${{ matrix.select }} ./a", "a", "TestA", false},
		{"go test -c ./a", "a", "TestA", false},
		{"go test ./a -list .", "a", "TestA", false},
		{"go test -overlay=o.json ./a", "a", "TestA", false},
		{"if go test ./a; then exit 1; fi", "a", "TestA", false},
		{"! go test ./a", "a", "TestA", false},
		{"cd \"$dir\" && go test ./a", "a", "TestA", false},
		{"go test $(go list ./... | sed s/x/y/)", "a", "TestA", false},
		{"go test $(go list -f '{{.Dir}}' ./...)", "a", "TestA", false},
	} {
		cmd := one(t, c.script)
		got := cmd.unread == "" && cmd.selectsPackage("", c.pkg) && cmd.selectsName(c.name)
		if got != c.selected {
			t.Errorf("%q selects %s.%s = %v, want %v (unread: %q)", c.script, c.pkg, c.name, got, c.selected, cmd.unread)
		}
	}
	if cmds, _, _ := laneScript("x:1", "echo go test ./a\n# go test ./b\ngo vet ./...", "."); len(cmds) != 0 {
		t.Errorf("an echoed, a commented and a vet line read as %d go test commands", len(cmds))
	}
	if c := one(t, "go test -tags b,a ./x"); strings.Join(c.tags, ",") != "a,b" {
		t.Errorf("tags = %v, want [a b]", c.tags)
	}
	// make goals: flags, variables and a dry run
	for script, want := range map[string]string{
		"make -j4 a b V=1":                         "a b",
		"make -o skipped a":                        "a",
		"make -C sub a":                            "",
		"make -pq a":                               "",
		"make \"verify-${{ matrix.leg }}-x\"":      "verify-*-x",
		"make $goals a":                            "a",
		"case x in y) make a ;; z) make b ;; esac": "a b",
	} {
		_, makes, _ := laneScript("x:1", script, ".")
		if got := strings.Join(makes, " "); got != want {
			t.Errorf("%q: goals %q, want %q", script, got, want)
		}
	}
}
