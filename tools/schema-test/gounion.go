package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// goUnion is the harness of every union row: a case the languages' own Go
// tests each answered in their own package (internal/codegen/<lang>table and
// the like), held once, whose inputs name each language's Go test
// (testreg.GoTestsOf). A language's instance runs that test by its exact
// name through the gotest adapter's go test -json calls, with the build tags
// the row names. A language the row names no test for is red, naming the
// languages that do answer the case; a named test its package's source does
// not declare is red as MISSING. Nothing is left out: every language row has
// an instance, and each one passes or fails.
var goUnion = harness{
	Name:  "go-union",
	Build: func(string) [][]string { return nil },
	Run: func(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
		return newGoTestAdapter(log, ".", goTestJobs()).runUnion(ctx, lang, instances)
	},
}

// isUnion reports whether a row is a union row, run by goUnion.
func isUnion(t testreg.Test) bool {
	_, ok, _ := testreg.GoTestsOf(t)
	return ok
}

// runUnion runs one language's union instances.
func (a *goTestAdapter) runUnion(ctx context.Context, lang string, instances []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	calls := goTestCalls{}
	for _, inst := range instances {
		tests, ok, err := testreg.GoTestsOf(inst.Test)
		if !ok || err != nil {
			return out, fmt.Errorf("%s: not a union row: %w", inst.Full, err)
		}
		g, has := tests[lang]
		if !has {
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: unansweredReason(lang, inst.Test.Key, tests)})
			continue
		}
		if !a.declared(g.Package)[g.Test] {
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: fmt.Sprintf("MISSING: %s answers %s with %s %s, which its package's source does not declare", lang, inst.Test.Key, g.Package, g.Test)})
			continue
		}
		calls.add(goTestPackage{pkg: g.Package, tags: g.Tags}, g.Test, inst, a.callBudget())
	}
	res, err := a.runCalls(ctx, calls)
	return append(out, res...), err
}

// unansweredReason is the red verdict's reason for a language with no Go test
// for a union case.
func unansweredReason(lang, key string, tests map[string]testreg.GoTest) string {
	return fmt.Sprintf("%s has no Go test for %s: the case is unanswered in %s (answered in %s)",
		lang, key, lang, strings.Join(slices.Sorted(maps.Keys(tests)), ", "))
}
