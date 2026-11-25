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
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// goTestCase is one row that names a subtest of a top-level Go test.
type goTestCase struct {
	in   goTestInputs
	inst testreg.Instance
}

// runGoTestCases runs rows that name a subtest. One call is one parent
// test's subtests, cut by callBudget, selected with -run '^Test$/^(a|b)$'.
// The verdict is that subtest's own pass or fail, as nameBuildResults maps
// it: a parent failure does not flip a sibling that passed, so packageVerdict
// is not applied. Rows that do not name a subtest never reach here.
func (a *goTestAdapter) runGoTestCases(ctx context.Context, rows []goTestCase) ([]testreg.Result, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	type key struct{ pkg, test string }
	var order []key
	grouped := map[key][]goTestCase{}
	for _, r := range rows {
		k := key{r.in.Package, r.in.Test}
		if grouped[k] == nil {
			order = append(order, k)
		}
		grouped[k] = append(grouped[k], r)
	}
	var out []testreg.Result
	var errs []error
	budget := a.callBudget()
	for _, k := range order {
		seen := map[string]string{}
		var calls []goTestCall
		for _, r := range grouped[k] {
			full := r.in.Test + "/" + r.in.Subtest
			if prev, dup := seen[full]; dup {
				return out, fmt.Errorf("%s and %s both name %s", prev, r.inst.Full, full)
			}
			seen[full] = r.inst.Full
			calls = addToCall(calls, r.in.Subtest, r.inst, budget)
		}
		for _, c := range calls {
			res, err := a.runGoTestCaseCall(ctx, k.pkg, k.test, c.tests)
			out = append(out, res...)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	return out, errors.Join(errs...)
}

// runGoTestCaseCall runs one parent test's asked subtests.
func (a *goTestAdapter) runGoTestCaseCall(ctx context.Context, pkg, test string, tests map[string]string) ([]testreg.Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	dir, rel, err := a.moduleOf(pkg)
	if err != nil {
		return nil, err
	}
	subs := make([]string, 0, len(tests))
	for sub := range tests {
		subs = append(subs, sub)
	}
	slices.Sort(subs)
	want := map[string]string{}
	quoted := make([]string, len(subs))
	for i, sub := range subs {
		quoted[i] = regexp.QuoteMeta(sub)
		want[test+"/"+sub] = tests[sub]
	}
	pattern := "^" + regexp.QuoteMeta(test) + "$/^(" + strings.Join(quoted, "|") + ")$"
	args := []string{"test", "-count=1", "-json", "-timeout", goTestTimeout(ctx, time.Now()), "-run", pattern, rel}
	unlock := lockGoTestPackage(filepath.Join(dir, rel))
	defer unlock()
	run := a.goTest
	if run == nil {
		run = execGoTest
	}
	var stdout, stderr bytes.Buffer
	runErr := run(ctx, dir, args, &stdout, &stderr)
	res, other, err := goTestCaseResults(&stdout, want)
	if err != nil {
		return res, fmt.Errorf("go test %s: %w", pkg, err)
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
	}
	if runErr != nil && len(res) == 0 {
		return res, fmt.Errorf("go test %s: %w\n%s%s", pkg, runErr, tail(stderr.String(), 40), tail(stdout.String(), 40))
	}
	if len(res) == 0 {
		return res, fmt.Errorf("go test %s answered none of the %d asked: %s matched no test\n%s", pkg, len(want), pattern, tail(strings.Join(other, "\n"), 20))
	}
	return res, nil
}

// goTestCaseResults answers each asked subtest by its own pass, fail or
// skip. A skip is a failure. An asked subtest with no verdict is left out,
// so the run reports it as not reported. The parent test's events are not
// verdicts: its failure must not turn a sibling that passed into a failure.
func goTestCaseResults(r io.Reader, want map[string]string) (out []testreg.Result, other []string, err error) {
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
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: ev.Test + ": " + tail(strings.Join(output[ev.Test], "\n"), 20), Seconds: ev.Elapsed})
		case "skip":
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: ev.Test + ": the Go test skipped, and nothing is ever skipped: " + tail(strings.Join(output[ev.Test], "\n"), 5)})
		}
	}
	return out, other, nil
}
