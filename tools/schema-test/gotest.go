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
//
// It batches by package: `go test -json -run '^(A|B|...)$'` per package, cut
// into calls of at most callBudget measured seconds (so no call outweighs
// its shard), several packages at once and a package's calls one after
// another, each given what is left of the driver's deadline as -timeout,
// and maps every event to its row by the test's full name. A go test that
// exits non-zero with every test passed fails its rows (packageVerdict).
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
	// Subtest is one subtest of Test, or empty to run the whole test. One
	// row per subtest is how a case fails by its own name. The call is
	// go test -run with ^Test$ and ^sub$.
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
	if strings.Contains(in.Subtest, "/") || strings.ContainsAny(in.Subtest, "\r\n") {
		return in, fmt.Errorf("inputs: subtest wants one subtest name, got %q", in.Subtest)
	}
	if in.Make != "" && !makeTarget.MatchString(in.Make) {
		return in, fmt.Errorf("inputs: make wants one make target, got %q", in.Make)
	}
	return in, nil
}

// The runner calls Prepare only on an adapter that is a Preparer.
var _ testreg.Preparer = (*goTestAdapter)(nil)

// Prepare makes, once each, the targets the instances' rows name in
// "make" (testreg.Preparer). A row whose inputs do not read fails in Run,
// by name; a target that does not make fails the group here, naming it and
// the row that asked for it.
func (a *goTestAdapter) Prepare(ctx context.Context, _ string, instances []testreg.Instance) error {
	seen := map[string]bool{}
	for _, inst := range instances {
		in, err := goTestInputsOf(inst.Test)
		if err != nil || in.Make == "" || seen[in.Make] {
			continue
		}
		seen[in.Make] = true
		if a.build == nil {
			return fmt.Errorf("%s: needs make %s, and the gotest adapter has no builder", inst.Full, in.Make)
		}
		if err := a.build(ctx, []string{"make", in.Make}); err != nil {
			return fmt.Errorf("%s: %w", inst.Full, err)
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
	calls := goTestCalls{}
	var absent []testreg.Result // rows naming a data file the tree does not hold
	for _, inst := range instances {
		in, err := goTestInputsOf(inst.Test)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inst.Full, err)
		}
		if why := missingFiles(a.root, in.Files); why != "" {
			absent = append(absent, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: why})
			continue
		}
		name := in.Test
		if in.Subtest != "" {
			name += "/" + in.Subtest
		}
		calls.add(goTestPackage{pkg: in.Package}, name, inst, a.callBudget())
	}
	res, err := a.runCalls(ctx, calls)
	return append(res, absent...), err
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

// runPackage runs one package's asked tests in one go test.
func (a *goTestAdapter) runPackage(ctx context.Context, p goTestPackage, tests map[string]string) ([]testreg.Result, error) {
	pkg := p.pkg
	if ctx.Err() != nil {
		return nil, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	dir, rel, err := a.moduleOf(pkg)
	if err != nil {
		return nil, err
	}
	args := goTestArgs(p, rel, tests, goTestTimeout(ctx, time.Now()))
	// a package's tests were written to run in one process at a time (some
	// write fixed paths under build/), so the gotest and go-union harnesses
	// never run go test on one package at once
	unlock := lockGoTestPackage(filepath.Join(dir, rel))
	defer unlock()
	run := a.goTest
	if run == nil {
		run = execGoTest
	}
	var stdout, stderr bytes.Buffer
	runErr := run(ctx, dir, args, &stdout, &stderr)
	res, err := goTestResults(&stdout, tests)
	if err != nil {
		return res, fmt.Errorf("go test %s: %w", pkg, err)
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	return packageVerdict(pkg, res, runErr, stdout.String(), stderr.String())
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
	run := "^(" + strings.Join(names, "|") + ")$"
	if strings.Contains(names[0], "/") {
		parent, _, _ := strings.Cut(names[0], "/")
		subs := make([]string, 0, len(names))
		for _, n := range names {
			_, sub, ok := strings.Cut(n, "/")
			if !ok || parent+"/"+sub != n {
				return append(args, "-run", run, rel)
			}
			subs = append(subs, regexp.QuoteMeta(sub))
		}
		run = "^" + regexp.QuoteMeta(parent) + "$/^(" + strings.Join(subs, "|") + ")$"
	}
	return append(args, "-run", run, rel)
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

// subtestCall is true when every asked name is Parent/sub. A call is one
// or the other (addToCall); a mix is a bug in the cut.
func subtestCall(tests map[string]string) bool {
	if len(tests) == 0 {
		return false
	}
	for name := range tests {
		if !strings.Contains(name, "/") {
			return false
		}
	}
	return true
}

// goTestResults reads go test -json events into one result per asked test.
// A subtest's events count against its top-level test: its output joins the
// reason, and a subtest that skips fails its parent. A call that asked for
// subtests by name answers each one on its own (goTestSubResults).
func goTestResults(r io.Reader, tests map[string]string) ([]testreg.Result, error) {
	if subtestCall(tests) {
		return goTestSubResults(r, tests)
	}
	type event struct {
		Action, Test, Output string
		Elapsed              float64
	}
	output := map[string][]string{}
	skipped := map[string][]string{}
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
		case "output":
			output[top] = append(output[top], strings.TrimRight(ev.Output, "\n"))
		case "skip":
			if isSub {
				skipped[top] = append(skipped[top], sub)
				continue
			}
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "the Go test skipped, and nothing is ever skipped: " + tail(strings.Join(output[top], "\n"), 5)})
		case "pass":
			if isSub {
				continue
			}
			if s := skipped[top]; len(s) > 0 {
				out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: fmt.Sprintf("subtests skipped, and nothing is ever skipped: %s\n%s", strings.Join(s, ", "), tail(strings.Join(output[top], "\n"), 5))})
				continue
			}
			// go test's clock reads to the hundredth: a pass it timed at
			// 0.00 s was measured, at under goTestTick, and is recorded so
			// (`--write-seconds`, `schema-test seconds`), never left to
			// weigh DefaultSeconds as if it had never run
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Pass, Seconds: max(ev.Elapsed, goTestTick)})
		case "fail":
			if isSub {
				continue
			}
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: tail(strings.Join(output[top], "\n"), 20), Seconds: ev.Elapsed})
		}
	}
	return out, nil
}

// goTestSubResults answers each asked Parent/sub from its own pass, fail
// or skip event. A subtest the run did not execute fails by that name. The
// parent's own output is the reason when the test died before the subtest.
func goTestSubResults(r io.Reader, tests map[string]string) ([]testreg.Result, error) {
	type event struct {
		Action, Test, Output string
		Elapsed              float64
	}
	output := map[string][]string{}
	var other []string
	verdict := map[string]testreg.Result{}
	var order []string
	dec := json.NewDecoder(r)
	for {
		var ev event
		if err := dec.Decode(&ev); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go test -json: %w", err)
		}
		inst, asked := tests[ev.Test]
		switch ev.Action {
		case "output":
			if asked {
				output[ev.Test] = append(output[ev.Test], strings.TrimRight(ev.Output, "\n"))
			} else if ev.Output != "" {
				other = append(other, strings.TrimRight(ev.Output, "\n"))
			}
		case "pass":
			if !asked {
				continue
			}
			order = append(order, ev.Test)
			verdict[ev.Test] = testreg.Result{Instance: inst, Verdict: testreg.Pass, Seconds: max(ev.Elapsed, goTestTick)}
		case "fail":
			if !asked {
				continue
			}
			order = append(order, ev.Test)
			verdict[ev.Test] = testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: tail(strings.Join(output[ev.Test], "\n"), 20), Seconds: ev.Elapsed}
		case "skip":
			if !asked {
				continue
			}
			order = append(order, ev.Test)
			verdict[ev.Test] = testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "the Go test skipped, and nothing is ever skipped: " + tail(strings.Join(output[ev.Test], "\n"), 5)}
		}
	}
	out := make([]testreg.Result, 0, len(order))
	for _, name := range order {
		out = append(out, verdict[name])
	}
	why := tail(strings.Join(other, "\n"), 20)
	missing := make([]string, 0)
	for name := range tests {
		if _, ok := verdict[name]; !ok {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	for _, name := range missing {
		reason := "the Go test did not run the subtest " + name
		if why != "" {
			reason += "\n" + why
		}
		out = append(out, testreg.Result{Instance: tests[name], Verdict: testreg.Fail, Reason: reason})
	}
	return out, nil
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
// weigh together (testreg.Weight, their measured seconds). group is "" for
// top-level tests, or the parent test when the rows are its subtests. A call
// never mixes the two, and never mixes two parents: -run is one pattern.
type goTestCall struct {
	group   string
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
// callGroup is the batch a row belongs in: "" for a top-level test, or the
// parent test's name for a subtest row.
func callGroup(name string) string {
	parent, _, ok := strings.Cut(name, "/")
	if !ok {
		return ""
	}
	return parent
}

func addToCall(calls []goTestCall, name string, inst testreg.Instance, budget float64) []goTestCall {
	w := testreg.Weight(inst)
	g := callGroup(name)
	if n := len(calls); n > 0 && calls[n-1].group == g && calls[n-1].seconds+w <= budget {
		calls[n-1].tests[name] = inst.Full
		calls[n-1].seconds += w
		return calls
	}
	return append(calls, goTestCall{group: g, tests: map[string]string{name: inst.Full}, seconds: w})
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
