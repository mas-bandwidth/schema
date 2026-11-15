package ci

// ONLY THE FAST LANE RUNS ON A PUSH OR A PULL REQUEST, AND IN TWO MINUTES.
// The owner, 2026-11-15: "I want everything that is not light moved to
// nightly test now." "no exceptions." and "you have 2m max per-CI to test and
// verify code. that's it. never any more. never." On 2026-11-15 a landing pull
// request sat 45 minutes with 48 jobs queued while every schema runner ran
// push-on-main hostile corpora, fuzzers and resource bounds.
//
// So every workflow outside fastLaneWorkflows triggers on schedule and
// workflow_dispatch only, never on push, pull_request or pull_request_target;
// and every job of a workflow that does trigger on one of those carries
// `timeout-minutes:` of 2 or less. This reads the workflow text only.

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// fastLaneWorkflows are the light workflows a push or a pull request may
// start: the path-selected fast lane and the quick lints, plus the agreement
// check and the promotion tag, which test nothing. Every one of their jobs is
// held to two minutes.
var fastLaneWorkflows = map[string]bool{
	"ci-fast.yml":       true, // the path-selected fast lane
	"python-leg.yml":    true, // the Python leg's fast tests, by path
	"roadmap.yml":       true, // the roadmap lint, by path
	"caa-self-test.yml": true, // the agreement check's self-test
	"cla.yml":           true, // the contributor agreement check
	"promotion-tag.yml": true, // tags a promoted main commit
}

// pushOrPR are the events no heavy workflow may start on.
var pushOrPR = []string{"push", "pull_request", "pull_request_target"}

func nightlyOnlyErrors(wfs map[string]string) []string {
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		if fastLaneWorkflows[name] {
			continue
		}
		triggers := workflowTriggerBranches(wfs[name])
		for _, ev := range pushOrPR {
			if _, ok := triggers[ev]; ok {
				errs = append(errs, fmt.Sprintf("%s triggers on %s; only the fast lane runs on a push or a pull request, this file runs nightly and on dispatch", name, ev))
			}
		}
	}
	return errs
}

// jobTimeoutMinutes returns a job's `timeout-minutes:` (its own key, at the
// job's indentation) and whether it has one.
func jobTimeoutMinutes(jobBody string) (int, bool) {
	for raw := range strings.SplitSeq(jobBody, "\n") {
		rest, ok := strings.CutPrefix(raw, "    timeout-minutes:")
		if !ok {
			continue
		}
		if before, _, found := strings.Cut(rest, "#"); found {
			rest = before
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func twoMinuteErrors(wfs map[string]string) []string {
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		triggers := workflowTriggerBranches(wfs[name])
		onPushOrPR := false
		for _, ev := range pushOrPR {
			if _, ok := triggers[ev]; ok {
				onPushOrPR = true
			}
		}
		if !onPushOrPR {
			continue
		}
		jobs := yamlJobs(wfs[name])
		var jobNames []string
		for j := range jobs {
			jobNames = append(jobNames, j)
		}
		sort.Strings(jobNames)
		for _, j := range jobNames {
			n, ok := jobTimeoutMinutes(jobs[j])
			if !ok {
				errs = append(errs, fmt.Sprintf("%s:%s runs on a push or a pull request with no timeout-minutes; two minutes is the most a push or a pull request may take", name, j))
			} else if n > 2 {
				errs = append(errs, fmt.Sprintf("%s:%s runs on a push or a pull request with timeout-minutes: %d; two minutes is the most, a longer job runs nightly", name, j, n))
			}
		}
	}
	return errs
}

func TestOnlyTheFastLaneRunsOnPushOrPullRequest(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	if len(wfs) == 0 {
		t.Fatal("no workflows read: this gate would pass over an empty set")
	}
	for name := range fastLaneWorkflows {
		if _, ok := wfs[name]; !ok {
			t.Errorf("fastLaneWorkflows names %s, which is not in .github/workflows", name)
		}
	}
	for _, e := range nightlyOnlyErrors(wfs) {
		t.Error(e)
	}
	for _, e := range twoMinuteErrors(wfs) {
		t.Error(e)
	}

	plant := func(name, from, to string) map[string]string {
		m := maps.Clone(wfs)
		if !strings.Contains(m[name], from) {
			t.Fatalf("control cannot plant: %s does not contain %q", name, from)
		}
		m[name] = strings.Replace(m[name], from, to, 1)
		return m
	}
	for _, c := range []struct {
		name  string
		check func(map[string]string) []string
		wfs   map[string]string
	}{
		{"a hostile corpus back on push to main", nightlyOnlyErrors, plant("schema-c-runs-hostile-corpus.yml", "\non:\n", "\non:\n  push:\n    branches: [ main ]\n")},
		{"ci-full back on pull requests", nightlyOnlyErrors, plant("ci-full.yml", "\non:\n", "\non:\n  pull_request:\n")},
		{"a fast-lane job given five minutes", twoMinuteErrors, plant("ci-fast.yml", "    timeout-minutes: 2\n", "    timeout-minutes: 5\n")},
		{"a fast-lane job with no timeout", twoMinuteErrors, plant("roadmap.yml", "    timeout-minutes: 2\n", "")},
	} {
		if len(c.check(c.wfs)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", c.name)
		}
	}
}
