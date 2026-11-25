package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// subtest runs one named subtest of a language-free Go test. The parent
// gotest row still runs the top-level Test. A row that names a subtest gets
// that subtest's own pass or fail: the event is mapped the way
// nameBuildResults maps an asked subtest to the instance that named it.
type subtestAdapter struct {
	root  string
	jobs  int
	tests *goTestAdapter
	// goTest runs go test; nil is execGoTest.
	goTest func(ctx context.Context, dir string, args []string, stdout, stderr io.Writer) error
}

func newSubtestAdapter(root string, jobs int) *subtestAdapter {
	return &subtestAdapter{root: root, jobs: max(jobs, 1), tests: newGoTestAdapter(io.Discard, root, 1)}
}

type subtestInputs struct {
	Package string   `json:"package"`
	Test    string   `json:"test"`
	Subtest string   `json:"subtest"`
	Files   []string `json:"files,omitempty"`
}

func subtestInputsOf(t testreg.Test) (subtestInputs, error) {
	var in subtestInputs
	d := json.NewDecoder(bytes.NewReader(t.Inputs))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return in, fmt.Errorf("inputs: %w", err)
	}
	if !strings.HasPrefix(in.Package, "./") || !goTestName.MatchString(in.Test) || in.Subtest == "" {
		return in, fmt.Errorf("inputs want a ./package, a Go test name and a subtest, got %q %q %q", in.Package, in.Test, in.Subtest)
	}
	if strings.Contains(in.Subtest, "\n") || strings.HasPrefix(in.Subtest, "/") || strings.HasSuffix(in.Subtest, "/") || strings.Contains(in.Subtest, "//") {
		return in, fmt.Errorf("inputs: subtest %q is not a Go subtest name", in.Subtest)
	}
	return in, nil
}

func (a *subtestAdapter) Answers(t testreg.Test) bool {
	if t.Scope != testreg.LangFree {
		return false
	}
	in, err := subtestInputsOf(t)
	if err != nil {
		return false
	}
	return a.tests.declared(in.Package)[in.Test]
}

// subtestPlan maps each asked subtest's full Go name to its instance and
// returns the -run alternation that selects exactly those subtests. A
// subtest may contain slashes; each element is its own segment, and each
// asked subtest is its own alternative so the segments do not cross.
func subtestPlan(instances []testreg.Instance) (map[string]string, string, error) {
	want := map[string]string{}
	var alts []string
	for _, inst := range instances {
		in, err := subtestInputsOf(inst.Test)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", inst.Full, err)
		}
		full, pattern, err := subtestRun(in.Test, in.Subtest)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", inst.Full, err)
		}
		if prev, dup := want[full]; dup {
			return nil, "", fmt.Errorf("%s and %s both name %s", prev, inst.Full, full)
		}
		want[full] = inst.Full
		alts = append(alts, pattern)
	}
	if len(alts) == 0 {
		return nil, "", errors.New("subtest: no instance asked for")
	}
	return want, strings.Join(alts, "|"), nil
}

func subtestRun(test, sub string) (full, pattern string, err error) {
	// testing.rewrite runs on the whole t.Run name before the slash split,
	// and -json reports that name. A space is an underscore there.
	parts := strings.Split(goSubtestName(sub), "/")
	var b strings.Builder
	b.WriteString("^")
	b.WriteString(regexp.QuoteMeta(test))
	b.WriteString("$")
	full = test
	for _, p := range parts {
		if p == "" {
			return "", "", fmt.Errorf("subtest %q has an empty element", sub)
		}
		full += "/" + p
		b.WriteString("/^")
		b.WriteString(regexp.QuoteMeta(p))
		b.WriteString("$")
	}
	return full, b.String(), nil
}

// goSubtestName is testing.rewrite (testing/match.go): the name a t.Run
// shows in go test -json, which is also what -run matches after it rewrites
// each pattern element the same way.
func goSubtestName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case goSubtestSpace(r):
			b.WriteByte('_')
		case !strconv.IsPrint(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func goSubtestSpace(r rune) bool {
	if r < 0x2000 {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0, 0x1680:
			return true
		}
	} else {
		if r <= 0x200a {
			return true
		}
		switch r {
		case 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
			return true
		}
	}
	return false
}

// subtestResults answers each asked subtest by its own pass or fail event.
// A skip is a failure. An event for a test that was not asked is other:
// the parent's output, the package's, the build's.
func subtestResults(r io.Reader, want map[string]string) (out []testreg.Result, other []string, err error) {
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

func (a *subtestAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	type job struct {
		pkg   string
		insts []testreg.Instance
	}
	var order []string
	jobs := map[string]*job{}
	var absent []testreg.Result
	for _, inst := range instances {
		in, err := subtestInputsOf(inst.Test)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inst.Full, err)
		}
		if why := missingFiles(a.root, in.Files); why != "" {
			absent = append(absent, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: why})
			continue
		}
		j := jobs[in.Package]
		if j == nil {
			j = &job{pkg: in.Package}
			jobs[in.Package] = j
			order = append(order, in.Package)
		}
		j.insts = append(j.insts, inst)
	}
	type pkgRun struct {
		res []testreg.Result
		err error
	}
	runs := make([]pkgRun, len(order))
	sem := make(chan struct{}, a.jobs)
	var wg sync.WaitGroup
	for i, pkg := range order {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			runs[i].res, runs[i].err = a.runPackage(ctx, jobs[pkg].pkg, jobs[pkg].insts)
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
	return append(out, absent...), errors.Join(errs...)
}

func (a *subtestAdapter) runPackage(ctx context.Context, pkg string, instances []testreg.Instance) ([]testreg.Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	want, pattern, err := subtestPlan(instances)
	if err != nil {
		return nil, err
	}
	dir, rel, err := a.tests.moduleOf(pkg)
	if err != nil {
		return nil, err
	}
	args := []string{"test", "-count=1", "-json", "-timeout", goTestTimeout(ctx, time.Now()), "-run", pattern, rel}
	unlock := lockGoTestPackage(filepath.Join(dir, rel))
	defer unlock()
	run := a.goTest
	if run == nil {
		run = execGoTest
	}
	var stdout, stderr bytes.Buffer
	runErr := run(ctx, dir, args, &stdout, &stderr)
	res, other, parseErr := subtestResults(&stdout, want)
	if stderr.Len() > 0 {
		other = append(other, strings.TrimRight(stderr.String(), "\n"))
	}
	switch {
	case ctx.Err() != nil:
		return res, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	case parseErr != nil:
		return res, fmt.Errorf("go test %s: %w", pkg, parseErr)
	case runErr != nil && len(res) == 0:
		return res, fmt.Errorf("go test %s: %w\n%s", pkg, runErr, tail(strings.Join(other, "\n"), 20))
	case runErr != nil && !subtestFailed(res):
		why := fmt.Sprintf("go test %s exited %v though every subtest it ran passed: the package failed outside them:\n%s", pkg, runErr, tail(strings.Join(other, "\n"), 20))
		for i := range res {
			res[i].Verdict = testreg.Fail
			res[i].Reason = why
		}
		return res, nil
	case len(res) == 0:
		return res, fmt.Errorf("go test %s answered none of the %d asked: %s matched no test\n%s", pkg, len(want), pattern, tail(strings.Join(other, "\n"), 20))
	}
	return res, nil
}

func subtestFailed(res []testreg.Result) bool {
	for _, r := range res {
		if r.Verdict == testreg.Fail {
			return true
		}
	}
	return false
}
