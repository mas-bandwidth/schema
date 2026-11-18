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
	"regexp"
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

// nightlyWorkflow reports whether name is a nightly workflow: everything
// outside the fast lane except release.yml, which runs on dispatch only to
// cut a release and keeps its bounded timeout.
func nightlyWorkflow(name string) bool {
	return !fastLaneWorkflows[name] && name != "release.yml"
}

// NIGHTLY RUNS HAVE NO TIMEOUT. The owner, 2026-11-16: "Nightly runs don't
// have a timeout... they can run as long as they need." On 2026-11-13, 11-15
// and 11-16 ci-full's go-test (unit-codegen) was cancelled at its 15-minute
// cap with no verdict per test, and nine red tests hid behind it.
//
// So no nightly workflow carries timeout-minutes, none names a go test
// -timeout but 0, and each sets PGROUP_GO_TEST_TIMEOUT: '0' in its top-level
// env (tools/pgroup then lifts the -timeout every go test under a step names,
// make recipes included). GitHub's 360-minute job limit is the backstop.
var nightlyGoTestTimeout = regexp.MustCompile(`(?:^|\s)--?(?:test\.)?timeout[= ]+(\S+)`)

func nightlyNoTimeoutErrors(wfs map[string]string) []string {
	var names []string
	for name := range wfs {
		if nightlyWorkflow(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		wf := wfs[name]
		for i, raw := range strings.Split(wf, "\n") {
			line := raw
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if before, _, ok := strings.Cut(line, " #"); ok {
				line = before
			}
			if strings.HasPrefix(strings.TrimSpace(line), "timeout-minutes:") {
				errs = append(errs, fmt.Sprintf("%s:%d carries timeout-minutes; a nightly run has no timeout, it runs as long as it needs", name, i+1))
			}
			for _, m := range nightlyGoTestTimeout.FindAllStringSubmatch(line, -1) {
				if strings.Trim(m[1], `'"`) != "0" {
					errs = append(errs, fmt.Sprintf("%s:%d names -timeout %s; a nightly go test runs with -timeout 0", name, i+1, m[1]))
				}
			}
		}
		if !strings.Contains(wf, "\nenv:\n") || !strings.Contains(wf, "\n  PGROUP_GO_TEST_TIMEOUT: '0'\n") {
			errs = append(errs, fmt.Sprintf("%s does not set PGROUP_GO_TEST_TIMEOUT: '0' in its top-level env, so a go test a make recipe runs keeps the -timeout it names", name))
		}
	}
	return errs
}

// verdictWorkflows carry a verdict job: with no caps the one way a job stops
// short is GitHub's backstop, which reads cancelled, and a job whose need
// failed reads skipped. The verdict job runs always() and needs every other
// job, so neither reads green or vanishes from the run's result.
var verdictWorkflows = []string{"ci-full.yml", "certify.yml"}

func verdictErrors(wfs map[string]string) []string {
	var errs []string
	for _, name := range verdictWorkflows {
		jobs := yamlJobs(wfs[name])
		v, ok := jobs["verdict"]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s has no verdict job", name))
			continue
		}
		var needs string
		always := false
		for raw := range strings.SplitSeq(v, "\n") {
			if rest, ok := strings.CutPrefix(raw, "    needs:"); ok {
				needs = strings.TrimSpace(rest)
			}
			if rest, ok := strings.CutPrefix(raw, "    if:"); ok && strings.HasPrefix(strings.TrimSpace(rest), "always()") {
				always = true
			}
		}
		if !always {
			errs = append(errs, fmt.Sprintf("%s:verdict does not run if: always(), so a failed or cancelled job skips it and the run reads as if nothing were wrong", name))
		}
		needed := map[string]bool{}
		for part := range strings.SplitSeq(strings.Trim(needs, "[]"), ",") {
			if p := strings.TrimSpace(part); p != "" {
				needed[p] = true
			}
		}
		var jobNames []string
		for j := range jobs {
			jobNames = append(jobNames, j)
		}
		sort.Strings(jobNames)
		for _, j := range jobNames {
			if j != "verdict" && !needed[j] {
				errs = append(errs, fmt.Sprintf("%s:verdict does not need %s, so that job cancelled or skipped would not turn the run red", name, j))
			}
		}
		if !strings.Contains(v, `.value.result != "success"`) {
			errs = append(errs, fmt.Sprintf("%s:verdict does not fail on every result but success", name))
		}
	}
	return errs
}

func TestNightlyRunsHaveNoTimeout(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	n := 0
	for name := range wfs {
		if nightlyWorkflow(name) {
			n++
		}
	}
	if n == 0 {
		t.Fatal("no nightly workflow read: this gate would pass over an empty set")
	}
	for _, e := range nightlyNoTimeoutErrors(wfs) {
		t.Error(e)
	}
	for _, e := range verdictErrors(wfs) {
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
		{"unit-codegen given its fifteen minutes back", nightlyNoTimeoutErrors, plant("ci-full.yml", "\n  go-test:\n", "\n  go-test:\n    timeout-minutes: 15\n")},
		{"a nightly go test given -timeout 600s", nightlyNoTimeoutErrors, plant("schema-lua-nightly-fuzz.yml", "-count=1 -timeout 0", "-count=1 -timeout 600s")},
		{"a nightly go test given -timeout=10m", nightlyNoTimeoutErrors, plant("nightly-conformance.yml", "-json -timeout=0", "-json -timeout=10m")},
		{"the shim's lift dropped", nightlyNoTimeoutErrors, plant("certify.yml", "\n  PGROUP_GO_TEST_TIMEOUT: '0'\n", "\n")},
		{"a job the verdict does not need", verdictErrors, plant("certify.yml", ", schema-legs, big-endian]", ", big-endian]")},
		{"a verdict that is skipped when a job fails", verdictErrors, plant("ci-full.yml", "  verdict:\n    if: always() && ", "  verdict:\n    if: ")},
	} {
		if len(c.check(c.wfs)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", c.name)
		}
	}
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
