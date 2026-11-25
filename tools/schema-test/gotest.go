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
// It batches by package: one `go test -json -run '^(A|B|...)$'` per package,
// several packages at once, and maps every event to its row by the test's
// full name. A package in a module of its own (a go.mod below the root) runs
// from that module's directory. A row whose test is not in its package's
// source is MISSING (Answers); a row whose test is in the source but never
// reports in the event stream fails (the runner's "did not report it"). A
// test that skips, or holds a subtest that skips, fails: there is no
// skipped verdict.
type goTestAdapter struct {
	log  io.Writer
	root string // the repository root, which packages are relative to
	jobs int    // packages run at once

	mu    sync.Mutex
	funcs map[string]map[string]bool // package -> the top-level test functions its source declares
}

func newGoTestAdapter(log io.Writer, root string, jobs int) *goTestAdapter {
	return &goTestAdapter{log: log, root: root, jobs: max(jobs, 1), funcs: map[string]map[string]bool{}}
}

type goTestInputs struct {
	Package string `json:"package"`
	Test    string `json:"test"`
}

var goTestName = regexp.MustCompile(`^(Test|Fuzz)[A-Za-z0-9_]*$`)

// goTestEnv opens every gate the repository's Go tests still carry for the
// lanes that run them outside the driver (internal/slowtest): under the
// driver the slow half always runs, and a missing tool or corpus is a
// failure, never a skip. Which tests run is the query; a test's tier says
// what it needs.
var goTestEnv = []string{"SCHEMA_SLOW=1", "SCHEMA_REQUIRE_CORPUS=1"}

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
	return in, nil
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
	byPkg := map[string]map[string]string{} // package -> Go test -> instance
	var pkgs []string
	for _, inst := range instances {
		in, err := goTestInputsOf(inst.Test)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inst.Full, err)
		}
		if byPkg[in.Package] == nil {
			byPkg[in.Package] = map[string]string{}
			pkgs = append(pkgs, in.Package)
		}
		byPkg[in.Package][in.Test] = inst.Full
	}
	type pkgRun struct {
		res []testreg.Result
		err error
	}
	runs := make([]pkgRun, len(pkgs))
	sem := make(chan struct{}, a.jobs)
	var wg sync.WaitGroup
	for i, pkg := range pkgs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := a.runPackage(ctx, pkg, byPkg[pkg])
			runs[i] = pkgRun{res, err}
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
func (a *goTestAdapter) runPackage(ctx context.Context, pkg string, tests map[string]string) ([]testreg.Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	dir, rel, err := a.moduleOf(pkg)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tests))
	for n := range tests {
		names = append(names, n)
	}
	slices.Sort(names)
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-json", "-timeout", goTestTimeout(ctx, time.Now()), "-run", "^("+strings.Join(names, "|")+")$", rel)
	cmd.Dir = dir
	bound(cmd)
	cmd.Env = append(os.Environ(), goTestEnv...)
	for k, v := range testreg.EnvFrom(ctx) {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	res, err := goTestResults(&stdout, tests)
	if err != nil {
		return res, fmt.Errorf("go test %s: %w", pkg, err)
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	// a failing test exits 1 and is a fail verdict already; a build
	// failure reports no test, and every one it was asked for fails as
	// unreported, with the build's words here
	if runErr != nil && len(res) == 0 {
		return res, fmt.Errorf("go test %s: %w\n%s%s", pkg, runErr, tail(stderr.String(), 40), tail(stdout.String(), 40))
	}
	return res, nil
}

// goTestResults reads go test -json events into one result per asked test.
// A subtest's events count against its top-level test: its output joins the
// reason, and a subtest that skips fails its parent.
func goTestResults(r io.Reader, tests map[string]string) ([]testreg.Result, error) {
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
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Pass, Seconds: ev.Elapsed})
		case "fail":
			if isSub {
				continue
			}
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: tail(strings.Join(output[top], "\n"), 20), Seconds: ev.Elapsed})
		}
	}
	return out, nil
}

// goTestTimeout is go test's -timeout for one package's run: what is left of
// the adapter call's deadline, or 0 (none) when the call has none. go test's
// own default is ten minutes, which would cut a package's run short of the
// limit the driver set (--timeout) and leave its tests unreported; the
// tests' probes read this deadline too (slowtest.ProbeContext).
func goTestTimeout(ctx context.Context, now time.Time) string {
	d, ok := ctx.Deadline()
	if !ok {
		return "0"
	}
	return max(d.Sub(now), time.Second).Round(time.Second).String()
}
