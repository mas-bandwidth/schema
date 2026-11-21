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
// 3323 _test.go files, 57 of them with no lane this gate can prove:
//
//   - tools/treelock/treelock_windows_test.go (1 test) is `//go:build
//     windows`. No Linux row compiles it, and ci-full.yml's windows job ran
//     `go vet` and `go build` and no test. It had never run in CI.
//   - test/conformance/go/rows: TestRowR20FloatWidensExactly was in neither
//     half of conformance-go-rows-a / -b, whose -run lists are written out
//     by hand. It ran only as a subtest of TestRowR20.
//   - 55 tests in the nested modules tables/cards (36), tables/cards/example
//     (1) and test/go-tables (18) had one lane: certify.yml's leg step, which
//     chooses its make goals in shell at run time (`make "test-${SHARD}"`
//     inside an if on a variable it computes). Nothing in the files says
//     that step runs test-go, and `go list ./...` from the root stops at a
//     nested go.mod, so no other gate counted these tests at all. ci-full's
//     go-test rows module-cards-go and module-go-tables run them by name now.
//
// HOW IT DECIDES. The rule everywhere is: credit only what is certain.
//
//   - Workflows (lanecover_workflow_test.go). A workflow counts for its
//     automatic triggers: a schedule (nightly), a push with no path filter
//     (per-commit). A job's matrix is expanded to the rows GitHub builds
//     (lists multiplied, exclude applied, include merged or appended), each
//     `if:` is evaluated per trigger and per row, and a step runs only where
//     its job's and its own `if:` are certainly true. `${{ }}` in a script is
//     replaced by its value for that row. Whatever cannot be resolved (a
//     matrix another job computes, runner.*, needs.*, env.*) is unknown, and
//     unknown is not a run. So a nightly lane counts exactly when its
//     workflow file exists, is scheduled, and certainly runs the test.
//   - Scripts (lanecover_engine_test.go). Only a command the script certainly
//     reaches counts: not one inside an if, a loop, a function or after ||.
//     A case over a literal word is read through its one matching branch. A
//     make goal must be spelled out; a shell variable or an unresolved
//     expression names no goal. Make rules are followed through prerequisites
//     and recursive make, and a rule inside a make conditional earns nothing.
//   - Commands. `go test` is read into directory, packages, -tags, -run and
//     -skip. -run and -skip are matched by the testing package's own rule
//     (internal/gotestmatch). -count=0, -list, -c, -overlay, -args with
//     arguments, a flag this reader does not know, GOFLAGS or GOOS set for
//     the command, a package list in a variable: credited with nothing.
//   - Files. `go list` under the job's GOOS and the command's -tags says which
//     test files a command compiles in the main module; go/build's own file
//     matching says it for a nested module.
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
// The fix for a red is always a lane a reader can see: a step or a make rule
// that names the goal. There is no list of exceptions here to add to.
//
// IT DOES NOT ASK whether a selected test then skips itself (the SCHEMA_SLOW
// accounting in tools/slowgatescan and the skip ledgers own that), and it
// does not require exactly once: the per-row exactly-once rule for ci-full's
// go-test job stays in tools/slowgatescan. It logs how many tests more than
// one command selects (6537 of 6648 on the day it was written).
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
	// GitHub runs every .yml and .yaml file in the directory; a tree with
	// none is a gate that would read no lane, so it is refused here.
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	wfs := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml")) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, err
		}
		wfs[e.Name()] = string(body)
	}
	if len(wfs) == 0 {
		return nil, nil, fmt.Errorf("%s holds no .yml or .yaml workflow: the gate would read no lane", dir)
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
	plain, err := compiled(laneContext{platform: "linux/amd64"})
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
		v, _ := realCompiledCache.LoadOrStore(root+"\x00"+ctx.platform+"\x00"+ctx.tags, &entry{})
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
	t.Run("F-023 an unreferenced make rule earns nothing on the real certify.yml", func(t *testing.T) {
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
		// her rule: named like certify.yml's `test-${{ matrix.shard }}` goals, on no row
		if err := mk2.read(root, "planted.mk", "test-stella-never-on-any-matrix:\n\tcd test/conformance/go && go test ./rows/ -count=1 -run '^TestRowR20FloatWidensExactly$$'\n"); err != nil {
			t.Fatal(err)
		}
		has(t, nowhere(t, mk2, wfs), "test/conformance/go/rows/R20_test.go: TestRowR20FloatWidensExactly is run by no CI lane")
	})
	t.Run("F-022 ci-full's smoke steps are off on its only automatic event", func(t *testing.T) {
		t.Parallel()
		_, set, err := laneVerdict(root, mk, wfs, realCompiled(root))
		if err != nil {
			t.Fatal(err)
		}
		full := 0
		for _, c := range set.cmds {
			switch c.target {
			case "test-moo-tagged-compiler-smoke", "test-moo-tagged-table-smoke", "test-goose-tagged-harness-smoke":
				t.Errorf("%s (%s) is credited through %s: its steps run on every event but schedule, and schedule is ci-full.yml's one automatic trigger", c.src, c.target, c.workflow)
			case "test-moo-tagged-compiler", "test-moo-tagged-table", "test-goose-tagged-harness":
				full++
			}
		}
		if full == 0 {
			t.Error("the scheduled moo and goose tagged targets are credited to nobody: this control is reading the wrong names")
		}
		// and with the scheduled step moved to the other events, the tests it alone ran are red
		errs := nowhere(t, mk, edit("ci-full.yml", "        if: matrix.group == 'moo-compiler' && github.event_name == 'schedule'", "        if: matrix.group == 'moo-compiler' && github.event_name != 'schedule'"))
		has(t, errs, "compiler/tablesmoo_test.go: ")
	})
	t.Run("a nested module's lane row leaves the matrix", func(t *testing.T) {
		t.Parallel()
		errs := nowhere(t, mk, edit("ci-full.yml", "module-cards-go, module-go-tables, ", ""))
		has(t, errs, "tables/cards/cards_test.go: ")
		has(t, errs, "tables/cards/example/example_test.go: ")
		has(t, errs, "test/go-tables/usage_test.go: TestUsage is run by no CI lane")
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
func laneFixture(t *testing.T, makefile string, workflows, files map[string]string) string {
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
	for name, body := range files {
		if body == "" {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				t.Fatal(err)
			}
			continue
		}
		write(name, body)
	}
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
    runs-on: [self-hosted, Linux, X64]
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

func laneFixtureErrors(t *testing.T, makefile string, workflows map[string]string, files ...map[string]string) []string {
	t.Helper()
	var extra map[string]string
	if len(files) > 0 {
		extra = files[0]
	}
	root := laneFixture(t, makefile, workflows, extra)
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
		files    map[string]string
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
			name: "the command sits in a shell if", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "run: go test ./a -run '^TestA$'", "run: if [ -n \"$X\" ]; then go test ./a -run '^TestA$'; fi"),
			want:    []string{"a/a_test.go: TestA" + none},
		},
		{
			name: "the make goal sits in a shell if", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "make -j4 test-all V=1", "if [ -z \"$IDX\" ]; then make -j4 test-all V=1; fi"),
			want:    []string{"b/tagged_test.go (1 tests): no CI lane's build compiles this file", "b/tagged_test.go: TestTagged" + none, "nested/n_test.go: TestNested" + none},
		},
		{
			name: "the command runs only when another fails", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "run: go test ./a -run '^TestA$'", "run: true || go test ./a -run '^TestA$'"),
			want:    []string{"a/a_test.go: TestA" + none},
		},
		{
			name: "GOFLAGS is set over the step", makefile: laneGoodMakefile,
			nightly: edit(laneGoodNightly, "        run: go test ./a -run '^TestA$'", "        env:\n          GOFLAGS: -run=TestNope\n        run: go test ./a -run '^TestA$'"),
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
			errs := laneFixtureErrors(t, c.makefile, map[string]string{"nightly.yml": c.nightly}, c.files)
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
		{"go test ./a -count=0", "a", "TestA", false},
		{"go test ./a -count 0", "a", "TestA", false},
		{"go test ./a -count \"$N\"", "a", "TestA", false},
		{"go test ./a -count=2", "a", "TestA", true},
		{"go test ./a -args -test.run TestNotFound", "a", "TestA", false},
		{"go test ./a -args", "a", "TestA", true},
		{"go test ./a -run 'Test('", "a", "TestA", false},
		{"go test ./a -skip 'TestMissing/child|TestA'", "a", "TestA", false},
		{"go test ./a -skip 'TestA/child|TestMissing'", "a", "TestA", true},
		{"go test ./a -run 'TestMissing/child|TestA'", "a", "TestA", true},
		{"GOFLAGS=-run=TestB go test ./a", "a", "TestA", false},
		{"GOOS=windows go test ./a", "a", "TestA", false},
		{"if [ -n \"$x\" ]; then go test ./a; fi", "a", "TestA", false},
		{"true || go test ./a", "a", "TestA", false},
		{"for p in a; do go test ./a; done", "a", "TestA", false},
		{"run() { go test ./a; }", "a", "TestA", false},
		{"case y in y) go test ./a ;; esac", "a", "TestA", true},
		{"case z in y) go test ./a ;; *) go test ./b ;; esac", "b", "TestA", true},
		{"go test ./a -unknown-flag", "a", "TestA", false},
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
		"make -j4 a b V=1":                           "a b",
		"make -o skipped a":                          "a",
		"make -C sub a":                              "",
		"make -pq a":                                 "",
		"make \"verify-${{ matrix.leg }}-x\"":        "",
		"case y in y) make a ;; z) make b ;; esac":   "a",
		"case \"$v\" in y) make a ;; esac":           "",
		"if [ -n \"$x\" ]; then make a; fi; make b":  "b",
		"for g in a b; do make $g c; done; make d":   "d",
		"run() { make a; }; make b":                  "b",
		"true || make a":                             "",
		"cat <<EOF\nmake a\nEOF\nmake b":             "b",
		"awk '\n  END { make a }\n' x\nmake b":       "b",
		"VARS=(\n  make a\n)\nmake b \"${VARS[@]}\"": "b",
		"( cd sub && make a ); make b":               "b",
		"make $goals a":                              "a",
		"case x in y) make a ;; z) make b ;; esac":   "",
		"case x in y) make a ;; *) make b ;; esac":   "b",
	} {
		_, makes, _ := laneScript("x:1", script, ".")
		if got := strings.Join(makes, " "); got != want {
			t.Errorf("%q: goals %q, want %q", script, got, want)
		}
	}
}

// TestLaneExpressions holds the expression reader to GitHub's rules where it
// answers, and to "unknown" everywhere else: an `if:` is a run only when it
// is certainly true.
func TestLaneExpressions(t *testing.T) {
	t.Parallel()
	row := map[string]laneVal{"group": laneS("unit-a"), "os": laneS("fleet"), "flag": laneB(true), "runner": {kind: laneList, raw: "[self-hosted, macOS, ARM64]"}}
	sched := laneEnv{event: "schedule", row: row, rowKnown: true}
	push := laneEnv{event: "push", row: row, rowKnown: true}
	noRow := laneEnv{event: "schedule"}
	for _, c := range []struct {
		cond string
		env  laneEnv
		want bool
	}{
		{"", sched, true},
		{"true", sched, true},
		{"false", sched, false},
		{"${{ false }}", sched, false},
		{"github.event_name == 'schedule'", sched, true},
		{"github.event_name == 'SCHEDULE'", sched, true}, // == ignores case
		{"github.event_name == 'schedule'", push, false},
		{"github.event_name == 'pull_request'", sched, false},
		{"github.event_name == 'workflow_dispatch'", sched, false},
		{"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'", sched, true},
		{"github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository", sched, true},
		{"github.event_name != 'pull_request' || (github.event.pull_request.head.repo.full_name == github.repository && contains(github.event.pull_request.labels.*.name, 'full-ci'))", sched, true},
		{"github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name == github.repository", sched, false},
		{"startsWith(github.head_ref, 'sprint/') == false && (github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository)", push, true},
		{"startsWith(github.head_ref, 'sprint/') == false && (needs.plan.outputs.legs != '[]' && github.event_name == 'workflow_dispatch')", push, false},
		{"needs.plan.outputs.legs != '[]'", push, false}, // unknown is not a run
		{"runner.environment == 'github-hosted'", sched, false},
		{"!(runner.environment == 'github-hosted')", sched, false},
		{"matrix.group == 'unit-a'", sched, true},
		{"matrix.group == 'unit-b'", sched, false},
		{"matrix.group != 'unit-b'", sched, true},
		{"matrix.group == 'unit-a' && runner.environment == 'github-hosted'", sched, false},
		{"matrix.group == 'unit-b' && runner.environment == 'github-hosted'", sched, false},
		{"matrix.group == 'unit-a' || runner.environment == 'github-hosted'", sched, true},
		{"matrix.group == 'unit-a' && github.event_name != 'schedule'", sched, false},
		{"matrix.group == 'unit-a' && github.event_name != 'schedule'", push, true},
		{"startsWith(matrix.group, 'unit-')", sched, true},
		{"startsWith(matrix.group, 'core-')", sched, false},
		{"'!startsWith(matrix.group, ''core-'') && matrix.group != ''generated'''", sched, true},
		{"matrix.absent != ''", sched, false}, // a key the row lacks is empty
		{"matrix.absent == ''", sched, true},
		{"matrix.flag", sched, true},
		{"matrix.runner", sched, true},
		{"matrix.group == 'unit-a'", noRow, false}, // an unresolved matrix answers nothing
		{"matrix.group != 'unit-b'", noRow, false},
		{"always()", sched, true},
		{"always() && !cancelled()", sched, true},
		{"failure()", sched, false},
		{"hashFiles('x') != ''", sched, false},
		{"github.event_name == ", sched, false}, // does not parse
		{"github.event_name === 'schedule'", sched, false},
	} {
		if got := laneCertain(c.cond, c.env); got != c.want {
			t.Errorf("if: %s under %s = %v, want %v", c.cond, c.env.event, got, c.want)
		}
	}
	if got := laneSubst("make \"test-${{ matrix.group }}\" ${{ matrix.runner }} ${{ needs.x.outputs.y }} ${{ github.event_name == 'schedule' && 'full' || 'smoke' }}", sched); got != "make \"test-unit-a\" ${{ matrix.runner }} ${{ needs.x.outputs.y }} full" {
		t.Errorf("substitution = %q", got)
	}
	if got := laneRunsOn("${{ matrix.runner }}", sched); got != "darwin/arm64" {
		t.Errorf("runs-on from a row's list = %q, want darwin/arm64", got)
	}
	for runsOn, want := range map[string]string{
		"[self-hosted, Linux, X64, fleet, schema]":         "linux/amd64",
		"[ self-hosted, linux, x64, schema ]":              "linux/amd64",
		"[\"self-hosted\", \"Linux\", \"X64\", \"fleet\"]": "linux/amd64",
		"[self-hosted, macOS, ARM64, fleet, schema]":       "darwin/arm64",
		"[self-hosted, macOS, fleet]":                      "", // F-026: no architecture named
		"[self-hosted, Linux, X64, ARM64]":                 "", // two
		"ubuntu-24.04":                                     "linux/amd64",
		"ubuntu-latest":                                    "linux/amd64",
		"ubuntu-24.04-arm":                                 "linux/arm64",
		"windows-latest":                                   "windows/amd64",
		"windows-2025":                                     "windows/amd64",
		"windows-11-arm":                                   "windows/arm64",
		"macos-latest":                                     "darwin/arm64",
		"macos-15":                                         "darwin/arm64",
		"macos-13":                                         "darwin/amd64",
		"macos-15-intel":                                   "darwin/amd64",
		"macos-15-large":                                   "darwin/amd64",
		"macos-15-xlarge":                                  "darwin/arm64",
		"self-hosted":                                      "",
	} {
		if got := laneRunsOn(runsOn, sched); got != want {
			t.Errorf("runs-on %s = %q, want %q", runsOn, got, want)
		}
	}
	if got := laneRunsOn("${{ needs.x.outputs.runner }}", sched); got != "" {
		t.Errorf("an unresolved runs-on = %q, want none", got)
	}
}

// TestLaneMatrixRows holds the matrix to the rows GitHub builds: the product
// of the lists, less exclude, with include merged into the rows it does not
// contradict or appended; and to "not known" when any part is computed.
func TestLaneMatrixRows(t *testing.T) {
	t.Parallel()
	read := func(body string) laneMatrix {
		t.Helper()
		wf := laneReadWorkflow("x.yml", "on:\n  schedule:\n    - cron: '1 2 * * *'\njobs:\n  j:\n    runs-on: ubuntu-24.04\n    strategy:\n      fail-fast: false\n      matrix:"+body+"\n    steps:\n      - run: true\n")
		if len(wf.jobs) != 1 {
			t.Fatalf("read %d jobs", len(wf.jobs))
		}
		return wf.jobs[0].matrix
	}
	show := func(rows []map[string]laneVal) string {
		var out []string
		for _, r := range rows {
			var keys []string
			for k := range r {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			var parts []string
			for _, k := range keys {
				v, _ := r[k].text()
				if r[k].kind == laneList {
					v = r[k].raw
				}
				parts = append(parts, k+"="+v)
			}
			out = append(out, strings.Join(parts, ","))
		}
		slices.Sort(out)
		return strings.Join(out, " | ")
	}
	for _, c := range []struct {
		name, body, event string
		want              string
		unknown           bool
	}{
		{name: "one list", body: "\n        g: [ a, b ]", want: "g=a | g=b"},
		{name: "a block list", body: "\n        g:\n          - a\n          - b # the second", want: "g=a | g=b"},
		{name: "a product", body: "\n        g: [a, b]\n        os: [x, y]", want: "g=a,os=x | g=a,os=y | g=b,os=x | g=b,os=y"},
		{name: "exclude", body: "\n        g: [a, b]\n        os: [x, y]\n        exclude:\n          - g: a\n            os: y", want: "g=a,os=x | g=b,os=x | g=b,os=y"},
		{name: "include alone is the rows", body: "\n        include:\n          - shard: s1\n            select: -run '^(TestA|TestB)'\n          - shard: s2\n            select: -skip '^(TestA|TestB)'", want: "select=-run '^(TestA|TestB)',shard=s1 | select=-skip '^(TestA|TestB)',shard=s2"},
		{name: "include as flow maps", body: "\n        include:\n          - { os: fleet, shard: go, runner: [self-hosted, Linux] }\n          - { os: mac, shard: go, runner: [self-hosted, macOS] }", want: "os=fleet,runner=[self-hosted, Linux],shard=go | os=mac,runner=[self-hosted, macOS],shard=go"},
		{name: "include merges into the rows it fits", body: "\n        g: [a, b]\n        include:\n          - g: a\n            extra: 1\n          - g: c", want: "extra=1,g=a | g=b | g=c"},
		{name: "include with only new keys joins every row", body: "\n        g: [a, b]\n        include:\n          - extra: 1", want: "extra=1,g=a | extra=1,g=b"},
		{name: "exclude by an expression the event decides (schedule)", event: "schedule", body: "\n        g: [a, b]\n        exclude: ${{ fromJSON((github.event_name == 'push' || github.event_name == 'pull_request') && '[{\"g\":\"a\"}]' || '[]') }}", want: "g=a | g=b"},
		{name: "exclude by an expression the event decides (push)", event: "push", body: "\n        g: [a, b]\n        exclude: ${{ fromJSON((github.event_name == 'push' || github.event_name == 'pull_request') && '[{\"g\":\"a\"}]' || '[]') }}", want: "g=b"},
		{name: "a list another job computes", body: "\n        g: ${{ fromJSON(needs.plan.outputs.legs) }}\n        gate: [x, y]", unknown: true},
		{name: "an include another job computes", body: "\n        include: ${{ fromJSON(needs.plan.outputs.rows) }}", unknown: true},
		{name: "a whole matrix another job computes", body: " ${{ fromJSON(needs.m.outputs.matrix) }}", unknown: true},
		{name: "an exclude another job computes", body: "\n        g: [a, b]\n        exclude: ${{ fromJSON(needs.m.outputs.x) }}", unknown: true},
	} {
		event := c.event
		if event == "" {
			event = "schedule"
		}
		rows, known := read(c.body).rows(event)
		if known == c.unknown {
			t.Errorf("%s: known = %v, want %v", c.name, known, !c.unknown)
			continue
		}
		if got := show(rows); known && got != c.want {
			t.Errorf("%s: rows %s, want %s", c.name, got, c.want)
		}
	}
	if rows, known := (laneMatrix{}).rows("schedule"); !known || len(rows) != 1 {
		t.Errorf("a job with no matrix: %d rows, known %v; want its one run", len(rows), known)
	}
}

// laneCorpus is the committed fixture corpus: testdata/lanecover (its README
// says how a case is laid out).
const laneCorpus = "testdata/lanecover"

// laneCorpusTree builds one corpus case in a temporary directory: the
// committed tree with the case's files laid over it.
func laneCorpusTree(t *testing.T, id string) string {
	t.Helper()
	root := t.TempDir()
	lay := func(src string) {
		t.Helper()
		err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			if first, rest, ok := strings.Cut(filepath.ToSlash(rel), "/"); ok && first == "workflows" {
				rel = filepath.Join(".github", "workflows", filepath.FromSlash(rest))
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			dst := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dst, body, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	lay(filepath.Join(laneCorpus, "tree"))
	dir := filepath.Join(laneCorpus, "cases", id)
	for _, part := range []string{"workflows", "files"} {
		if _, err := os.Stat(filepath.Join(dir, part)); err == nil {
			if part == "workflows" {
				// laid as workflows/..., which lay() installs under .github
				tmp := t.TempDir()
				if err := os.CopyFS(filepath.Join(tmp, "workflows"), os.DirFS(filepath.Join(dir, part))); err != nil {
					t.Fatal(err)
				}
				lay(tmp)
			} else {
				lay(filepath.Join(dir, part))
			}
		}
	}
	if body, err := os.ReadFile(filepath.Join(dir, "Makefile")); err == nil {
		if err := os.WriteFile(filepath.Join(root, "Makefile"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(dir, "remove.txt")); err == nil {
		for name := range strings.FieldsSeq(string(body)) {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// TestLaneCoverageFindingsCorpus runs the gate over every committed case in
// testdata/lanecover/cases and holds it to the case's want.txt: each line an
// error the gate must give, no other error, and none at all for a control.
// The reviewer's findings on this gate (F-020 to F-026) live there, each her
// repro beside a control; a finding whose case is gone fails here by id, and
// so does a corpus with no control or no case.
func TestLaneCoverageFindingsCorpus(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(filepath.Join(laneCorpus, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	controls, faults := 0, 0
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("%s: a file among the case directories", e.Name())
			continue
		}
		id := e.Name()
		have[id] = true
		wantBody, err := os.ReadFile(filepath.Join(laneCorpus, "cases", id, "want.txt"))
		if err != nil {
			t.Errorf("%s: no want.txt, so the case asserts nothing: %v", id, err)
			continue
		}
		if head, err := os.ReadFile(filepath.Join(laneCorpus, "cases", id, "CASE.txt")); err != nil || strings.SplitN(string(head), "\n", 2)[0] != id {
			t.Errorf("%s: CASE.txt must open with the case's id", id)
		}
		var want []string
		for line := range strings.SplitSeq(string(wantBody), "\n") {
			if strings.TrimSpace(line) != "" {
				want = append(want, line)
			}
		}
		if len(want) == 0 {
			controls++
		} else {
			faults++
		}
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			root := laneCorpusTree(t, id)
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
			errs := res.errors()
			if len(errs) != len(want) {
				t.Errorf("got %d errors, want %d:\n%s", len(errs), len(want), strings.Join(errs, "\n"))
			}
			for _, w := range want {
				if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, w) }) {
					t.Errorf("no error holds %q; got:\n%s", w, strings.Join(errs, "\n"))
				}
			}
		})
	}
	for _, id := range []string{"control", "F-020", "F-021", "F-021-control", "F-022", "F-022-control", "F-023", "F-023-control", "F-024", "F-025", "F-026", "F-026-arm64", "F-026-arm64-amd64", "F-026-arm64-neutral", "F-219", "F-219-underscore", "F-219-control"} {
		if !have[id] {
			t.Errorf("case %s is not in %s/cases: a finding's repro or its control was removed", id, laneCorpus)
		}
	}
	if controls < 4 || faults < 7 {
		t.Errorf("the corpus holds %d controls and %d faults: fewer than the findings it was committed with", controls, faults)
	}
}

// TestLaneCorpusTreeIsTheInlineFixture keeps the two fixtures one: the tree
// the corpus cases are laid over is the tree the inline controls edit.
func TestLaneCorpusTreeIsTheInlineFixture(t *testing.T) {
	t.Parallel()
	for file, want := range map[string]string{"Makefile": laneGoodMakefile, "workflows/nightly.yml": laneGoodNightly} {
		got, err := os.ReadFile(filepath.Join(laneCorpus, "tree", filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s/tree/%s differs from the inline fixture the controls edit", laneCorpus, file)
		}
	}
}

// TestLaneMakeReadsWhatMakeIncludes holds the make reader to make's own list
// of files: the include directives, where they stand, and nothing else. It
// replaced a fixed list of globs, one of which (make/negative-controls.d/*.mk)
// matched nothing, and which read make/checks files whether or not the
// Makefile included them.
func TestLaneMakeReadsWhatMakeIncludes(t *testing.T) {
	t.Parallel()
	tree := func(t *testing.T, files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for name, body := range files {
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	targets := func(mk *laneMake) []string {
		var got []string
		for _, r := range mk.rules {
			got = append(got, r.targets...)
		}
		slices.Sort(got)
		return got
	}
	root := tree(t, map[string]string{
		"Makefile":            "all: ; true\ninclude $(wildcard make/*.mk)\n-include absent.mk\nsinclude also-absent.mk\ninclude make/checks/x.mk\n",
		"make/a.mk":           "a: ; true\ninclude make/inner/b.mk\n",
		"make/c.mk":           "c: ; true\n",
		"make/notes.txt":      "n: ; true\n",
		"make/inner/b.mk":     "b: ; true\n",
		"make/checks/x.mk":    "x: ; true\n",
		"make/checks/y.mk":    "y: ; true\n", // not included: make never reads it
		"make/inner/twice.mk": "t: ; true\n",
	})
	mk, err := laneReadMake(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := targets(mk), []string{"a", "all", "b", "c", "x"}; !slices.Equal(got, want) {
		t.Errorf("rules read = %v, want %v", got, want)
	}
	for name, files := range map[string]map[string]string{
		"a missing include":             {"Makefile": "all: ; true\ninclude make/absent.mk\n"},
		"an include it cannot expand":   {"Makefile": "all: ; true\ninclude $(MKS)\n"},
		"a wildcard over directories":   {"Makefile": "all: ; true\ninclude $(wildcard */*.mk)\n"},
		"a wildcard it cannot read":     {"Makefile": "all: ; true\ninclude $(wildcard $(D)/*.mk)\n"},
		"a Makefile that declares none": {"Makefile": "# nothing\n"},
	} {
		if _, err := laneReadMake(tree(t, files)); err == nil {
			t.Errorf("%s: read without an error", name)
		}
	}
	repo, err := laneReadMake(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"Makefile", "make/go.mk", "make/goose-blockcook.mk", "make/checks/generated-tree.mk", "tools/roadmap/roadmap.mk"} {
		if !repo.files[f] {
			t.Errorf("the real Makefile's includes did not reach %s", f)
		}
	}
	if len(repo.files) < 20 {
		t.Errorf("the real Makefile's includes reached %d files, which is not this repository's make", len(repo.files))
	}
}

// TestLaneLoadRefusesATreeWithNoWorkflow: a workflows directory with nothing
// GitHub runs is a gate over no lane, not a pass.
func TestLaneLoadRefusesATreeWithNoWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for name, body := range map[string]string{
		"Makefile":                 "all: ; true\n",
		".github/workflows/README": "not a workflow\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := laneLoad(root); err == nil {
		t.Error("a tree with no .yml or .yaml workflow loaded")
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows", "w.yaml"), []byte("on: push\njobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, wfs, err := laneLoad(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := wfs["w.yaml"]; !ok || len(wfs) != 1 {
		t.Errorf("workflows read = %v, want the one .yaml file", slices.Collect(maps.Keys(wfs)))
	}
}
