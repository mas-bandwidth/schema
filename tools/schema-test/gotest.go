package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// goTestAdapter runs language-free Go tests: a test whose inputs are
// {"package": "./internal/x", "test": "TestY"} is one top-level Go test.
// A row that also names "subtest" is that subtest of the test.
//
// It batches by package: `go test -json -run '^(A|B|...)$'` per package, cut
// into calls of at most callBudget measured seconds (so no call outweighs
// its shard), several packages at once and a package's calls one after
// another, each given what is left of the driver's deadline as -timeout,
// and maps every event to its row by the test's full name. A row with no
// subtest is that top-level test, and a subtest's events count against it
// (goTestResults). A row that names a subtest runs as `-run '^T$/^(sub)$'`
// and takes that subtest's own pass or fail (goTestSubtestResults); a parent
// event does not flip it. A go test that exits non-zero with every top-level
// test passed fails those rows (packageVerdict).
// A package in a module of its own (a go.mod below the root) runs
// from that module's directory. A row whose test is not in its package's
// source is MISSING (Answers); a row whose test is in the source but never
// reports in the event stream fails (the runner's "did not report it"). A
// test that skips, or holds a subtest that skips, fails: there is no
// skipped verdict.
//
// A row whose test reads something make builds (a generated tree) names
// that make target in its inputs, {"make": "build/x/.stamp"}: Prepare makes
// each named target once, before any call's deadline starts
// (testreg.Preparer), and a target that fails to make fails the group by
// name, so the test never runs against a tree that was not built.
type goTestAdapter struct {
	log  io.Writer
	root string // the repository root, which packages are relative to
	jobs int    // packages run at once

	// build runs one build step (a make command line) once per process;
	// adapters hands in the shared builder's, a unit test its own
	build func(ctx context.Context, argv []string) error

	mu    sync.Mutex
	funcs map[string]map[string]bool // package -> the top-level test functions its source declares

	// goTest runs go test; nil is execGoTest (a test stands in for it)
	goTest func(ctx context.Context, dir string, args []string, stdout, stderr io.Writer) error

	// the builds the tests read (goneeds.go), declared in testreg.GoNeedsPath
	// and run through build (one of the adapter's own when build is nil)
	needsOnce  sync.Once
	needsTable []testreg.GoNeed
	needsErr   error
}

func newGoTestAdapter(log io.Writer, root string, jobs int) *goTestAdapter {
	return &goTestAdapter{log: log, root: root, jobs: max(jobs, 1), funcs: map[string]map[string]bool{}}
}

// newGoTestAdapterBuilding is newGoTestAdapter whose Prepare makes a row's
// "make" target with build.
func newGoTestAdapterBuilding(log io.Writer, root string, jobs int, build func(context.Context, []string) error) *goTestAdapter {
	a := newGoTestAdapter(log, root, jobs)
	a.build = build
	return a
}

type goTestInputs struct {
	Package string `json:"package"`
	Test    string `json:"test"`
	// Reads names the data the test reads (a file, or a directory with a
	// trailing /): the extract credits it to the row and holds the test's
	// source to naming it (extract.CoveredIn); the run itself needs nothing
	// of it.
	Reads []string `json:"reads,omitempty"`
	// ReadPlant is the key of the read plant that proves what Reads
	// credits (extract.CreditPlant): a read with no plant credits nothing.
	ReadPlant string `json:"read_plant,omitempty"`
	// Subtest is one subtest of Test, as go test names it. A space in the
	// t.Run argument is an underscore in that name. Empty means the top-level test.
	// Set, the row's verdict is that subtest's own (gotest_case.go).
	Subtest string `json:"subtest,omitempty"`
	// Files are the committed data files the test reads (a case folder it
	// replays): the registry indexes them where they are
	// (internal/testreg/extract, Indexed).
	Files []string `json:"files,omitempty"`
	// Make is the make target the test needs built first, or empty
	Make string `json:"make,omitempty"`
}

var (
	goTestName = regexp.MustCompile(`^(Test|Fuzz)[A-Za-z0-9_]*$`)
	makeTarget = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)
)

// goTestInputsOf reads and checks a row's inputs.
func goTestInputsOf(t testreg.Test) (goTestInputs, error) {
	var in goTestInputs
	d := json.NewDecoder(bytes.NewReader(t.Inputs))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return in, fmt.Errorf("inputs: %w", err)
	}
	if !strings.HasPrefix(in.Package, "./") || !goTestName.MatchString(in.Test) {
		return in, fmt.Errorf("inputs want a ./package and a Go test name, got %q %q", in.Package, in.Test)
	}
	if in.Make != "" && !makeTarget.MatchString(in.Make) {
		return in, fmt.Errorf("inputs: make wants one make target, got %q", in.Make)
	}
	if in.Subtest != "" && (strings.TrimSpace(in.Subtest) != in.Subtest || strings.ContainsAny(in.Subtest, "/\r\n")) {
		return in, fmt.Errorf("inputs: subtest wants one Go subtest name, got %q", in.Subtest)
	}
	return in, nil
}

// The runner calls Prepare only on an adapter that is a Preparer.
var _ testreg.Preparer = (*goTestAdapter)(nil)
var _ testreg.StepPreparer = (*goTestAdapter)(nil)

// PrepareSteps is a step for each target the instances' rows name in "make"
// and each build their Go tests read (testreg.StepPreparer): the runner runs
// each under its own call limit, named by its target. A row whose inputs do
// not read fails in Run, by name; a target that does not make fails the group
// here, naming it and the row that asked for it.
func (a *goTestAdapter) PrepareSteps(_ string, instances []testreg.Instance) []testreg.PrepareStep {
	var steps []testreg.PrepareStep
	seen := map[string]bool{}
	var refs []goTestRef
	for _, inst := range instances {
		in, err := goTestInputsOf(inst.Test)
		if err != nil {
			continue // Run fails the row, naming its inputs
		}
		refs = append(refs, goTestRef{in.Package, in.Test})
		if in.Make == "" || seen[in.Make] {
			continue
		}
		seen[in.Make] = true
		full, target := inst.Full, in.Make
		steps = append(steps, testreg.PrepareStep{Name: target, Run: func(ctx context.Context) error {
			if a.build == nil {
				return fmt.Errorf("%s: needs make %s, and the gotest adapter has no builder", full, target)
			}
			if err := a.build(ctx, []string{"make", target}); err != nil {
				return fmt.Errorf("%s: %w", full, err)
			}
			return nil
		}})
	}
	return append(steps, a.needsSteps(refs)...)
}

// Prepare runs every step under the one ctx it is given: the steps' own
// limits are the runner's (prepareSteps), so a direct caller holds them all to
// its single deadline.
func (a *goTestAdapter) Prepare(ctx context.Context, lang string, instances []testreg.Instance) error {
	for _, step := range a.PrepareSteps(lang, instances) {
		if err := step.Run(ctx); err != nil {
			return err
		}
	}
	return nil
}

// declared is the set of top-level functions a package's _test.go files
// declare, read once per package. The files are parsed, so a test written
// inside a fixture string is not one, and a file that does not parse
// declares nothing (its package's rows are MISSING, by name).
func (a *goTestAdapter) declared(pkg string) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if got, ok := a.funcs[pkg]; ok {
		return got
	}
	got := map[string]bool{}
	// errdiscard: Glob fails only on a malformed pattern, and *_test.go is not one
	files, _ := filepath.Glob(filepath.Join(a.root, filepath.FromSlash(pkg), "*_test.go"))
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
				got[fn.Name.Name] = true
			}
		}
	}
	a.funcs[pkg] = got
	return got
}

// Answers is true for a language-free row whose Go test exists: a row whose
// test was deleted, renamed or never written is MISSING, by name.
func (a *goTestAdapter) Answers(t testreg.Test) bool {
	if t.Scope != testreg.LangFree {
		return false
	}
	in, err := goTestInputsOf(t)
	if err != nil {
		return false
	}
	return a.declared(in.Package)[in.Test]
}

// moduleOf is the directory go test runs a package from, and the package's
// path there: the nearest directory at or above it holding a go.mod.
func (a *goTestAdapter) moduleOf(pkg string) (string, string, error) {
	dir := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(pkg, "./")))
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(a.root, d, "go.mod")); err == nil {
			rel, err := filepath.Rel(d, dir)
			if err != nil {
				return "", "", err
			}
			return filepath.Join(a.root, d), "./" + filepath.ToSlash(rel), nil
		}
		if d == "." || d == string(filepath.Separator) {
			return "", "", fmt.Errorf("package %s is in no Go module under %s", pkg, a.root)
		}
	}
}

func (a *goTestAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	ins := make([]goTestInputs, len(instances))
	refs := make([]goTestRef, len(instances))
	for i, inst := range instances {
		in, err := goTestInputsOf(inst.Test)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inst.Full, err)
		}
		ins[i], refs[i] = in, goTestRef{in.Package, in.Test}
	}
	// what the tests read is built first (goneeds.go); Prepare has built it,
	// and the builder answers a step it ran at once
	failed, err := a.buildNeeds(ctx, refs)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err // a build the deadline cut short is the deadline's
	}
	calls := goTestCalls{}
	var cases []goTestCase     // rows that name a subtest (gotest_case.go)
	var unrun []testreg.Result // rows reading a build that failed, or naming a data file the tree does not hold
	for i, inst := range instances {
		// the failed build first: a data file it would have made is not
		// the cause
		if why := a.needsFailure(failed, refs[i]); why != "" {
			unrun = append(unrun, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: why})
			continue
		}
		if why := missingFiles(a.root, ins[i].Files); why != "" {
			unrun = append(unrun, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: why})
			continue
		}
		if ins[i].Subtest != "" {
			cases = append(cases, goTestCase{in: ins[i], inst: inst})
			continue
		}
		calls.add(goTestPackage{pkg: ins[i].Package}, ins[i].Test, inst, a.callBudget())
	}
	res, err := a.runCalls(ctx, calls)
	cres, cerr := a.runGoTestCases(ctx, cases)
	return append(append(res, cres...), unrun...), errors.Join(err, cerr)
}

// goTestPackage is one go test invocation's package and its build tags.
type goTestPackage struct {
	pkg, tags string
}

// goTestCalls is every package's calls, cut by weight, in first-asked order.
type goTestCalls struct {
	order []goTestPackage
	calls map[goTestPackage][]goTestCall
}

func (c *goTestCalls) add(p goTestPackage, test string, inst testreg.Instance, budget float64) {
	if c.calls == nil {
		c.calls = map[goTestPackage][]goTestCall{}
	}
	if c.calls[p] == nil {
		c.order = append(c.order, p)
	}
	c.calls[p] = addToCall(c.calls[p], test, inst, budget)
}

// runCalls runs the calls: packages at once, a package's calls one after
// another, since its tests were written to share one process at a time (a
// test that is not t.Parallel may hold a path its siblings use).
func (a *goTestAdapter) runCalls(ctx context.Context, calls goTestCalls) ([]testreg.Result, error) {
	type pkgRun struct {
		res []testreg.Result
		err error
	}
	runs := make([]pkgRun, len(calls.order))
	sem := make(chan struct{}, a.jobs)
	var wg sync.WaitGroup
	for i, p := range calls.order {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			var errs []error
			for _, c := range calls.calls[p] {
				res, err := a.runPackage(ctx, p, c.tests)
				runs[i].res = append(runs[i].res, res...)
				if err != nil {
					errs = append(errs, err)
				}
			}
			runs[i].err = errors.Join(errs...)
		})
	}
	wg.Wait()
	var out []testreg.Result
	var errs []error
	for _, r := range runs {
		out = append(out, r.res...)
		if r.err != nil {
			errs = append(errs, r.err)
		}
	}
	return out, errors.Join(errs...)
}

// runPackage runs one package's asked tests. Rows that name a top-level test
// share one go test. Rows that name a subtest share one go test per parent
// test, because -run splits on / and a mixed pattern would select the wrong
// tests. A package's tests were written to run in one process at a time
// (some write fixed paths under build/), so the gotest and go-union harnesses
// never run go test on one package at once.
func (a *goTestAdapter) runPackage(ctx context.Context, p goTestPackage, tests map[string]string) ([]testreg.Result, error) {
	pkg := p.pkg
	if ctx.Err() != nil {
		return nil, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	dir, rel, err := a.moduleOf(pkg)
	if err != nil {
		return nil, err
	}
	plain, byParent := splitGoTests(tests)
	unlock := lockGoTestPackage(filepath.Join(dir, rel))
	defer unlock()
	var out []testreg.Result
	var errs []error
	if len(plain) > 0 {
		res, err := a.finishGoTest(ctx, pkg, dir, goTestArgs(p, rel, plain, goTestTimeout(ctx, time.Now())), func(stdout, stderr string, runErr error) ([]testreg.Result, error) {
			res, err := goTestResults(strings.NewReader(stdout), plain)
			if err != nil {
				return res, fmt.Errorf("go test %s: %w", pkg, err)
			}
			// go test's own -timeout fires goTestTimeoutMargin before the
			// driver's deadline, so its words come first: the limit, and the
			// test running
			if to, ok := goTestTimedOut(stdout); ok {
				return to.verdicts(pkg, plain, res), nil
			}
			if ctx.Err() != nil {
				return res, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
			}
			return packageVerdict(pkg, res, runErr, stdout, stderr)
		})
		out = append(out, res...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	var parents []string
	for parent := range byParent {
		parents = append(parents, parent)
	}
	slices.Sort(parents)
	for _, parent := range parents {
		if ctx.Err() != nil {
			errs = append(errs, fmt.Errorf("go test %s: %w", pkg, ctx.Err()))
			break
		}
		want := byParent[parent]
		args := goTestSubtestArgs(p, rel, parent, subtestNames(parent, want), goTestTimeout(ctx, time.Now()))
		res, err := a.finishGoTest(ctx, pkg, dir, args, func(stdout, stderr string, runErr error) ([]testreg.Result, error) {
			res, other, err := goTestSubtestResults(strings.NewReader(stdout), want)
			if err != nil {
				return res, fmt.Errorf("go test %s: %w", pkg, err)
			}
			if to, ok := goTestTimedOut(stdout); ok {
				return to.verdicts(pkg, want, res), nil
			}
			if ctx.Err() != nil {
				return res, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
			}
			// a failing subtest exits non-zero, and its event says so. A
			// parent check after the subtests exits non-zero too; that does
			// not flip a subtest that passed. A run that answered nothing
			// failed before its tests.
			if runErr != nil && len(res) == 0 {
				return res, fmt.Errorf("go test %s: %w\n%s%s", pkg, runErr, tail(stderr, 40), tail(strings.Join(other, "\n"), 40))
			}
			return res, nil
		})
		out = append(out, res...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return out, errors.Join(errs...)
}

// finishGoTest runs one go test in dir and maps its output. The package lock
// is the caller's.
func (a *goTestAdapter) finishGoTest(ctx context.Context, pkg, dir string, args []string, verdict func(stdout, stderr string, runErr error) ([]testreg.Result, error)) ([]testreg.Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	run := a.goTest
	if run == nil {
		run = execGoTest
	}
	var stdout, stderr bytes.Buffer
	runErr := run(ctx, dir, args, &stdout, &stderr)
	return verdict(stdout.String(), stderr.String(), runErr)
}

// goTestTimeoutRun is what go test printed when its own -timeout fired:
// "panic: test timed out after 1s", then "running tests:" and one line per
// test still running, with how long it had run ("TestBlocks (1s)").
type goTestTimeoutRun struct {
	limit   string
	running []goTestRunning // in go test's order
}

type goTestRunning struct {
	test string // as go test names it: a subtest is "TestA/sub"
	ran  string // how long it had run, in go test's words
}

// wasRunning is whether test, a top-level test, or a subtest of it, was
// running when the limit fired: test2json may close such a test as failed,
// and the timeout is why.
func (to goTestTimeoutRun) wasRunning(test string) bool {
	for _, r := range to.running {
		if top, _, _ := strings.Cut(r.test, "/"); top == test {
			return true
		}
	}
	return false
}

// goTestTimeoutPanic is go test's own timeout panic, and its limit.
var goTestTimeoutPanic = regexp.MustCompile(`^panic: test timed out after (\S+)$`)

// goTestRunningLine is one test of the panic's "running tests:" list.
var goTestRunningLine = regexp.MustCompile(`^\t\t(\S+) \(([^)]*)\)$`)

// goTestTimedOut reads go test's own timeout panic from a call's -json
// output (its output events, in order, and any line that is not an event),
// or ok false when the run did not time out.
func goTestTimedOut(stdout string) (goTestTimeoutRun, bool) {
	var lines []string
	for line := range strings.SplitSeq(stdout, "\n") {
		var ev struct{ Action, Output string }
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &ev) == nil && ev.Action != "" {
			if ev.Action == "output" {
				lines = append(lines, strings.TrimRight(ev.Output, "\n"))
			}
			continue
		}
		lines = append(lines, line)
	}
	for i, l := range lines {
		m := goTestTimeoutPanic.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		to := goTestTimeoutRun{limit: m[1]}
		j := i + 1
		if j < len(lines) && strings.TrimSpace(lines[j]) == "running tests:" {
			for j++; j < len(lines); j++ {
				r := goTestRunningLine.FindStringSubmatch(lines[j])
				if r == nil {
					break
				}
				to.running = append(to.running, goTestRunning{test: r[1], ran: r[2]})
			}
		}
		return to, true
	}
	return goTestTimeoutRun{}, false
}

// verdicts gives every asked row of a call whose go test timed out its own
// verdict, never a crash: a row whose test was running when the limit fired
// fails naming the limit and how long it had run; a row whose test never
// started fails naming the limit and the test that was running; a row that
// reported before the limit keeps its verdict.
func (to goTestTimeoutRun) verdicts(pkg string, tests map[string]string, res []testreg.Result) []testreg.Result {
	// a running test by its own name (a subtest row's "TestA/sub") and by
	// its top-level test's: how long it had run
	ran := map[string]string{}
	var names []string
	for _, r := range to.running {
		top, _, _ := strings.Cut(r.test, "/")
		if _, seen := ran[top]; !seen {
			ran[top] = r.ran
			names = append(names, fmt.Sprintf("%s (it had run %s)", r.test, r.ran))
		}
		ran[r.test] = r.ran
	}
	runningWords := "go test named no test running"
	if len(names) > 0 {
		runningWords = "the test running was " + strings.Join(names, ", ")
	}
	inst := map[string]string{} // instance -> its Go test
	for test, full := range tests {
		inst[full] = test
	}
	reported := map[string]bool{}
	var out []testreg.Result
	for _, r := range res {
		test := inst[r.Instance]
		reported[r.Instance] = true
		if d, running := ran[test]; running {
			// test2json may close a running test as failed: the timeout is why
			out = append(out, testreg.Result{Instance: r.Instance, Verdict: testreg.Fail, Seconds: r.Seconds,
				Reason: fmt.Sprintf("TIMED OUT: row %s: go test %s hit its -timeout of %s while %s was running (it had run %s)", r.Instance, pkg, to.limit, test, d)})
			continue
		}
		out = append(out, r)
	}
	for _, test := range slices.Sorted(maps.Keys(tests)) {
		full := tests[test]
		if reported[full] {
			continue
		}
		why := fmt.Sprintf("TIMED OUT: row %s: go test %s hit its -timeout of %s before %s finished; %s", full, pkg, to.limit, test, runningWords)
		if d, running := ran[test]; running {
			why = fmt.Sprintf("TIMED OUT: row %s: go test %s hit its -timeout of %s while %s was running (it had run %s)", full, pkg, to.limit, test, d)
		}
		out = append(out, testreg.Result{Instance: full, Verdict: testreg.Fail, Reason: why})
	}
	return out
}

// splitGoTests separates top-level test names from Test/subtest names. The
// subtest map is keyed by the full Go name, grouped by the top-level test.
func splitGoTests(tests map[string]string) (plain map[string]string, byParent map[string]map[string]string) {
	plain = map[string]string{}
	byParent = map[string]map[string]string{}
	for name, inst := range tests {
		parent, _, ok := strings.Cut(name, "/")
		if !ok {
			plain[name] = inst
			continue
		}
		if byParent[parent] == nil {
			byParent[parent] = map[string]string{}
		}
		byParent[parent][name] = inst
	}
	return plain, byParent
}

// subtestNames is the subtest half of each full Go name under parent.
func subtestNames(parent string, want map[string]string) []string {
	subs := make([]string, 0, len(want))
	prefix := parent + "/"
	for name := range want {
		subs = append(subs, strings.TrimPrefix(name, prefix))
	}
	return subs
}

// execGoTest runs go with args in dir.
func execGoTest(ctx context.Context, dir string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	bound(cmd)
	// nothing in a test decides whether it runs: which tests run is the
	// query, and a test's tier says what it needs
	cmd.Env = os.Environ()
	for k, v := range testreg.EnvFrom(ctx) {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// goTestArgs is the go test command line for one call: the named tests of
// one package, under the package's build tags.
func goTestArgs(p goTestPackage, rel string, tests map[string]string, timeout string) []string {
	names := make([]string, 0, len(tests))
	for n := range tests {
		names = append(names, n)
	}
	slices.Sort(names)
	args := []string{"test", "-count=1", "-json", "-timeout", timeout}
	if p.tags != "" {
		args = append(args, "-tags", p.tags)
	}
	return append(args, "-run", "^("+strings.Join(names, "|")+")$", rel)
}

// goTestSubtestArgs is the go test command line for one parent test's
// subtests. -run splits on unbracketed slashes, so the pattern is
// ^Parent$/^(sub|...)$ and not a mix with the top-level names.
func goTestSubtestArgs(p goTestPackage, rel, parent string, subs []string, timeout string) []string {
	slices.Sort(subs)
	quoted := make([]string, len(subs))
	for i, s := range subs {
		quoted[i] = regexp.QuoteMeta(s)
	}
	pattern := "^" + regexp.QuoteMeta(parent) + "$/^(" + strings.Join(quoted, "|") + ")$"
	args := []string{"test", "-count=1", "-json", "-timeout", timeout}
	if p.tags != "" {
		args = append(args, "-tags", p.tags)
	}
	return append(args, "-run", pattern, rel)
}

var goTestPackageLocks sync.Map // package directory -> *sync.Mutex

// lockGoTestPackage holds one package directory for one go test call.
func lockGoTestPackage(dir string) (unlock func()) {
	m, _ := goTestPackageLocks.LoadOrStore(dir, new(sync.Mutex))
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// packageVerdict holds a call's results to how go test exited. A failing
// test exits 1 and is a fail verdict already. A build failure reports no
// test: every row it was asked for fails as unreported, with the build's
// words in the error. A run whose tests all passed and that still exits
// non-zero failed outside them (a TestMain's own check after m.Run, such as
// the harness's tree-unchanged check, or a panic between tests): every row
// fails with go test's words, never passes.
func packageVerdict(pkg string, res []testreg.Result, runErr error, stdout, stderr string) ([]testreg.Result, error) {
	if runErr == nil {
		return res, nil
	}
	if len(res) == 0 {
		return res, fmt.Errorf("go test %s: %w\n%s%s", pkg, runErr, tail(stderr, 40), tail(stdout, 40))
	}
	if slices.ContainsFunc(res, func(r testreg.Result) bool { return r.Verdict == testreg.Fail }) {
		return res, nil
	}
	why := fmt.Sprintf("go test %s exited %v though every test it ran passed: the package failed outside them (its TestMain, or between tests):\n%s%s", pkg, runErr, tail(stderr, 20), tail(stdout, 20))
	out := make([]testreg.Result, len(res))
	for i, r := range res {
		out[i] = testreg.Result{Instance: r.Instance, Verdict: testreg.Fail, Reason: why, Seconds: r.Seconds}
	}
	return out, nil
}

// goTestResults reads go test -json events into one result per asked
// top-level test. A subtest's events count against its top-level test: its
// output joins the reason, and a subtest that skips fails its parent. A fuzz
// target that ran no seed input fails. A row that names a subtest is mapped
// by goTestSubtestResults instead, so this folding stays the verdict of
// every row that does not.
//
// A result's seconds are what the test costs run alone, which the sharder
// sums: its own Elapsed plus the Elapsed of every parallel subtest under it
// (one that paused for t.Parallel) whose every ancestor below the top-level
// test paused too. go test's Elapsed for a test leaves out its parallel
// subtests, which run after its body returns, so a test made of them would
// weigh its body alone (the Ruby strict gate: 4.24 s of a 612 s run). A
// subtest that did not pause ran inside its parent's body, and its whole
// run, parallel children and all, is in the parent's Elapsed already. Never the wall time between events:
// tests of one call run side by side, and the wall of each holds the
// others' work.
func goTestResults(r io.Reader, tests map[string]string) ([]testreg.Result, error) {
	type event struct {
		Action, Test, Output string
		Elapsed              float64
	}
	output := map[string][]string{}
	subOutput := map[string][]string{}
	failed := map[string][]string{}
	skipped := map[string][]string{}
	subRuns := map[string]int{}      // a top-level test's subtests that ran
	paused := map[string]bool{}      // the subtests that paused for t.Parallel
	parallel := map[string]float64{} // a top-level test's parallel subtests' Elapsed
	seconds := func(top string, ev event) float64 { return ev.Elapsed + parallel[top] }
	// outside is whether a paused subtest's time is in no Elapsed counted
	// yet: every test between it and the top-level one paused as well
	outside := func(name string) bool {
		for n := name; strings.Contains(n, "/"); n = n[:strings.LastIndex(n, "/")] {
			if !paused[n] {
				return false
			}
		}
		return true
	}
	var out []testreg.Result
	dec := json.NewDecoder(r)
	for {
		var ev event
		if err := dec.Decode(&ev); err == io.EOF {
			break
		} else if err != nil {
			return out, fmt.Errorf("go test -json: %w", err)
		}
		top, sub, isSub := strings.Cut(ev.Test, "/")
		inst, asked := tests[top]
		if !asked {
			continue // the package's own lines
		}
		switch ev.Action {
		case "run":
			if isSub {
				subRuns[top]++
			}
		case "pause":
			if isSub {
				paused[ev.Test] = true
			}
		case "output":
			output[top] = append(output[top], strings.TrimRight(ev.Output, "\n"))
			if isSub {
				subOutput[ev.Test] = append(subOutput[ev.Test], strings.TrimRight(ev.Output, "\n"))
			}
		case "skip":
			if isSub {
				skipped[top] = append(skipped[top], sub)
				continue
			}
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "the Go test skipped, and nothing is ever skipped: " + tail(strings.Join(output[top], "\n"), 5)})
		case "pass":
			if isSub {
				if outside(ev.Test) {
					parallel[top] += ev.Elapsed
				}
				continue
			}
			if s := skipped[top]; len(s) > 0 {
				out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: fmt.Sprintf("subtests skipped, and nothing is ever skipped: %s\n%s", strings.Join(s, ", "), tail(strings.Join(output[top], "\n"), 5))})
				continue
			}
			// a fuzz target outside -fuzz runs its seed inputs, each a
			// subtest: one that ran none passed having tested nothing
			if strings.HasPrefix(top, "Fuzz") && subRuns[top] == 0 {
				out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "the fuzz target ran no seed input (no f.Add and no testdata/fuzz corpus), so it tested nothing"})
				continue
			}
			// go test's clock reads to the hundredth: a pass it timed at
			// 0.00 s was measured, at under goTestTick, and is recorded so
			// (`--write-seconds`, `schema-test seconds`), never left to
			// weigh DefaultSeconds as if it had never run
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Pass, Seconds: max(seconds(top, ev), goTestTick)})
		case "fail":
			if isSub {
				failed[top] = append(failed[top], ev.Test)
				if outside(ev.Test) {
					parallel[top] += ev.Elapsed
				}
				continue
			}
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: failedFirst(failed[top], subOutput, output[top]) + tail(strings.Join(output[top], "\n"), 20), Seconds: seconds(top, ev)})
		}
	}
	return out, nil
}

// goTestSubtestResults reads go test -json events and answers each asked
// subtest by its own pass or fail event, the same way nameBuildResults does.
// A skip is a failure. A parent event and any subtest that was not asked are
// ignored, so a check the parent runs after its subtests does not flip a
// subtest that passed. An asked subtest with no verdict is left out, and the
// runner reports it as not reported. other is what the rest of the run said.
func goTestSubtestResults(r io.Reader, want map[string]string) (out []testreg.Result, other []string, err error) {
	type event struct {
		Action, Test, Output string
		Elapsed              float64
	}
	output := map[string][]string{}
	dec := json.NewDecoder(r)
	for {
		var ev event
		if err := dec.Decode(&ev); err == io.EOF {
			break
		} else if err != nil {
			return out, other, fmt.Errorf("go test -json: %w", err)
		}
		inst, asked := want[ev.Test]
		if !asked {
			if (ev.Action == "output" || ev.Action == "build-output") && ev.Output != "" {
				other = append(other, strings.TrimRight(ev.Output, "\n"))
			}
			continue
		}
		switch ev.Action {
		case "output":
			output[ev.Test] = append(output[ev.Test], strings.TrimRight(ev.Output, "\n"))
		case "pass":
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Pass, Seconds: max(ev.Elapsed, goTestTick)})
		case "fail":
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: tail(strings.Join(output[ev.Test], "\n"), 20), Seconds: ev.Elapsed})
		case "skip":
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "the Go test skipped, and nothing is ever skipped: " + tail(strings.Join(output[ev.Test], "\n"), 5)})
		}
	}
	return out, other, nil
}

// goTestTimeout is go test's -timeout for one package's run: what is left of
// the adapter call's deadline less goTestTimeoutMargin (at least 1 s), or 0
// (none) when the call has none. go test's
// own default is ten minutes, which would cut a package's run short of the
// limit the driver set (--timeout) and leave its tests unreported; the
// tests' probes read this deadline too (slowtest.ProbeContext).
func goTestTimeout(ctx context.Context, now time.Time) string {
	d, ok := ctx.Deadline()
	if !ok {
		return "0"
	}
	return max(d.Sub(now)-goTestTimeoutMargin, time.Second).Round(time.Second).String()
}

// goTestTimeoutMargin is how far before the driver's deadline go test's own
// -timeout fires: its panic prints the stack naming the test that hung, and
// it must land before the driver kills the call, not race it.
const goTestTimeoutMargin = 10 * time.Second

// goTestCall is one go test's rows: Go test name -> instance, and what they
// weigh together (testreg.Weight, their measured seconds).
type goTestCall struct {
	tests   map[string]string
	seconds float64
}

// callBudget is the most one go test call carries: one shard's budget
// (testreg.ShardBudget), so no call is heavier than the shard it runs in,
// whatever list the driver hands the adapter.
func (a *goTestAdapter) callBudget() float64 {
	return testreg.ShardBudget
}

// addToCall puts a row in its package's last call, or in a new call when
// the last would weigh more than budget with it. A call holds at least one
// row, so a row heavier than the budget is a call of its own (the sharder
// names it: TestThePRShardsFitTheirJobs).
func addToCall(calls []goTestCall, name string, inst testreg.Instance, budget float64) []goTestCall {
	w := testreg.Weight(inst)
	if n := len(calls); n > 0 && calls[n-1].seconds+w <= budget {
		calls[n-1].tests[name] = inst.Full
		calls[n-1].seconds += w
		return calls
	}
	return append(calls, goTestCall{tests: map[string]string{name: inst.Full}, seconds: w})
}

// failedFirst is the line a red test's reason starts with, ahead of its
// output's tail: when subtests failed, each failed leaf subtest (one no
// other failed subtest is under) named with its first own line of output;
// when none did, the test's own first line of output that is not go test's
// "=== RUN" or "--- FAIL" bookkeeping. Either way the first line of the
// reason is the failure's own words; "" when there are none.
func failedFirst(failed []string, subOutput map[string][]string, own []string) string {
	if len(failed) == 0 {
		if l := ownLine(own); l != "" {
			return "failed: " + l + "\n"
		}
		return ""
	}
	var lines []string
	for _, f := range failed {
		if slices.ContainsFunc(failed, func(g string) bool { return strings.HasPrefix(g, f+"/") }) {
			continue // a parent of a failed subtest: its leaf says why
		}
		if l := ownLine(subOutput[f]); l != "" {
			lines = append(lines, f+": "+l)
		} else {
			lines = append(lines, f)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "failed: " + strings.Join(lines, "; ") + "\n"
}

// ownLine is the first line of output that is the test's own words, not go
// test's "=== RUN", "=== PAUSE", "=== CONT" or "--- FAIL" bookkeeping.
func ownLine(output []string) string {
	for _, l := range output {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "=== ") && !strings.HasPrefix(l, "--- ") {
			return l
		}
	}
	return ""
}

// missingFiles names the files a row's inputs name that root does not hold
// as regular files, or is "" when it holds them all: a row whose data is
// gone fails by name and its test does not run.
func missingFiles(root string, files []string) string {
	var gone []string
	for _, f := range files {
		st, err := os.Stat(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil || !st.Mode().IsRegular() {
			gone = append(gone, f)
		}
	}
	if len(gone) == 0 {
		return ""
	}
	return fmt.Sprintf("the row names data the tree does not hold: %s", strings.Join(gone, ", "))
}

// goTestTick is go test's clock resolution: Elapsed is in hundredths.
const goTestTick = 0.01
