package ci

// NO JOB RUNS ON A BRANCH NAME (the owner, 2026-11-24: "please fix this properly
// now. I have zero patience for tests not running when they are supposed to.
// split out into multiple groups of tests if you need to properly vs. hacks
// based on branches and so on.").
//
// Until that day every pull-request job opened its `if:` with
// `startsWith(github.head_ref, 'sprint/') == false`, and pull requests #4999,
// #5003 and #5004 merged with all 29 checks SKIPPED. A skipped job is not red,
// so nobody saw it.
//
// The rule, and the table these tests hold the workflows to, is
// docs/CI-TIERS.md "Every job and its tier":
//
//   - no `if:`, job or step, reads a branch name, a ref or a label; no
//     pull-request trigger filters its base branches. The fork guard
//     (head.repo.full_name == github.repository) is keyed on the repository and
//     stays: a fork's code never runs on the fleet;
//   - every job of a workflow that runs on a pull request, a push or a comment
//     has a row naming its tiers, and the tiers are what the workflow gives it;
//   - every per-PR group is a ci-fast.yml job that a pull request from any
//     branch runs, within two minutes.
//
// All of it reads the workflow text and evaluates `if:` with the model in
// ci_test.go (evalCIExpr); nothing runs.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const tierRepo = "mas-bandwidth/schema"

// refRead matches every expression that names a branch, a ref or a label.
var refRead = regexp.MustCompile(`github\.(head_ref|base_ref|ref|ref_name|ref_type)\b|github\.event\.(ref|base_ref)\b|pull_request\.(head|base)\.(ref|label)\b|\.labels\b`)

var ifLine = regexp.MustCompile(`^(\s*)(?:- )?if:\s*(.*)$`)

// workflowIfs returns every `if:` of a workflow, job or step, with a folded
// (`>-`, `|`) value's continuation lines joined on.
func workflowIfs(wf string) []string {
	lines := strings.Split(wf, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			continue
		}
		m := ifLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		expr := m[2]
		if strings.HasPrefix(expr, ">") || strings.HasPrefix(expr, "|") {
			indent := len(m[1])
			var parts []string
			for i+1 < len(lines) {
				next := lines[i+1]
				if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " ")) <= indent {
					break
				}
				parts = append(parts, strings.TrimSpace(next))
				i++
			}
			expr = strings.Join(parts, " ")
		}
		out = append(out, expr)
	}
	return out
}

// pullRequestFilters names each branches: or branches-ignore: filter under a
// pull_request or pull_request_target trigger.
func pullRequestFilters(wf string) []string {
	var out []string
	inOn, event := false, ""
	for raw := range strings.SplitSeq(wf, "\n") {
		if before, _, found := strings.Cut(raw, "#"); found {
			raw = before
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if !inOn {
			inOn = strings.HasPrefix(raw, "on:")
			continue
		}
		if !strings.HasPrefix(raw, " ") {
			break
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent == 2 {
			event = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if event != "pull_request" && event != "pull_request_target" {
			continue
		}
		if strings.HasPrefix(trimmed, "branches:") || strings.HasPrefix(trimmed, "branches-ignore:") {
			out = append(out, event+": "+trimmed)
		}
	}
	return out
}

// fullPlan sets every plan output a ci-fast.yml job can be selected by, so a
// job's answer is decided by its event and fork conditions alone.
func fullPlan(ev ciEvent) ciEvent {
	legs := `["c","cpp","cs","dart","elixir","go","goose","java","js","lobster","lua","luau","moo","python","ruby","rust","squirrel","unity"]`
	ev.PlanPkgs = "./..."
	ev.PlanLegs = legs
	ev.PlanVerifyLegs = legs
	ev.PlanDartCorpus = "true"
	ev.PlanCppCorpus = "true"
	ev.PRPins = "yes"
	ev.PRCS = "yes"
	return ev
}

func tierKeys[V any](m map[string]V) []string {
	keys := slices.Collect(maps.Keys(m))
	sort.Strings(keys)
	return keys
}

// branchTokenErrors is the textual half: an `if:` that reads a branch, a ref
// or a label, and a pull-request trigger that filters its base.
func branchTokenErrors(wfs map[string]string) []string {
	var errs []string
	for _, name := range tierKeys(wfs) {
		for _, expr := range workflowIfs(wfs[name]) {
			if m := refRead.FindString(expr); m != "" {
				errs = append(errs, fmt.Sprintf("%s: an if: reads %q, so a branch name, ref or label decides whether a check runs (if: %s)", name, m, expr))
			}
		}
		for _, f := range pullRequestFilters(wfs[name]) {
			errs = append(errs, fmt.Sprintf("%s: the %s trigger filters branches (%s): every pull request runs the per-PR tier, whatever its base", name, strings.SplitN(f, ":", 2)[0], f))
		}
	}
	return errs
}

// branchBehaviourErrors is the evaluated half: every job of a pull-request
// workflow gives the same answer for every head name, base and label set,
// from this repository and from a fork.
//
// Each variant changes one thing from the baseline (a pull request from
// `feature` into main with no label): the head name, the base, or a label.
// only, when set, limits the check to one workflow (the planted controls).
func branchBehaviourErrors(wfs map[string]string, only string) []string {
	type variant struct {
		head, base string
		labels     []string
	}
	variants := []variant{{"feature", "main", nil}}
	for _, h := range []string{"sprint/schema-main/a-card", "sprint-group/schema-main", "rowan/a-fix"} {
		variants = append(variants, variant{h, "main", nil})
	}
	for _, b := range []string{"sprint/schema-main", "dev"} {
		variants = append(variants, variant{"feature", b, nil})
	}
	variants = append(variants, variant{"feature", "main", []string{"full-ci"}})
	var errs []string
	for _, name := range tierKeys(wfs) {
		if only != "" && name != only {
			continue
		}
		triggers := workflowTriggerBranches(wfs[name])
		for _, evName := range []string{"pull_request", "pull_request_target"} {
			if _, on := triggers[evName]; !on {
				continue
			}
			for _, job := range tierKeys(yamlJobs(wfs[name])) {
				for _, headRepo := range []string{tierRepo, "someone/schema"} {
					answers := map[bool][]string{}
					for _, v := range variants {
						ev := fullPlan(ciEvent{Name: evName, Branch: v.base, HeadRef: v.head, Repo: tierRepo, HeadRepo: headRepo, Labels: v.labels})
						runs, err := jobRunsInWorkflow(wfs[name], job, ev)
						if err != nil {
							errs = append(errs, fmt.Sprintf("%s:%s: its if: cannot be evaluated for a %s: %v", name, job, evName, err))
							continue
						}
						answers[runs] = append(answers[runs], fmt.Sprintf("%s into %s labels=%v", v.head, v.base, v.labels))
					}
					if len(answers) > 1 {
						errs = append(errs, fmt.Sprintf("%s:%s: a %s from %s runs for %v and skips for %v: a branch name, base or label decides whether it runs", name, job, evName, headRepo, answers[true][0], answers[false][0]))
					}
				}
			}
		}
	}
	return errs
}

func TestNoJobRunsOnABranchName(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	if n := len(yamlJobs(wfs["ci-fast.yml"])); n < 10 {
		t.Fatalf("ci-fast.yml has %d jobs: this gate would pass over an empty fast lane", n)
	}
	for _, e := range branchTokenErrors(wfs) {
		t.Error(e)
	}
	for _, e := range branchBehaviourErrors(wfs, "") {
		t.Error(e)
	}

	// The fork guard stays, keyed on the repository: a fork's pull request is
	// refused whatever its branch is called.
	for _, head := range []string{"sprint/schema-main/a-card", "feature"} {
		fork := fullPlan(ciEvent{Name: "pull_request", Branch: "main", HeadRef: head, Repo: tierRepo, HeadRepo: "someone/schema"})
		if runs, err := jobRunsInWorkflow(wfs["ci-fast.yml"], "plan", fork); err != nil || runs {
			t.Errorf("a fork's pull request from %q runs ci-fast.yml plan on the fleet (runs=%v err=%v)", head, runs, err)
		}
		if runs, err := jobRunsInWorkflow(wfs["ci-fast.yml"], "fork-pull-request", fork); err != nil || !runs {
			t.Errorf("a fork's pull request from %q does not get the red fork-pull-request job (runs=%v err=%v)", head, runs, err)
		}
	}

	plant := func(name, from, to string) map[string]string {
		t.Helper()
		m := maps.Clone(wfs)
		if !strings.Contains(m[name], from) {
			t.Fatalf("control cannot plant: %s does not contain %q", name, from)
		}
		m[name] = strings.Replace(m[name], from, to, 1)
		return m
	}
	planIf := "  plan:\n    if: ("
	for _, c := range []struct {
		name      string
		wfs       map[string]string
		behaviour string // the workflow the evaluated half must catch it in; "" when only the text gate can
	}{
		{"the sprint/ skip put back on plan", plant("ci-fast.yml", planIf, "  plan:\n    if: startsWith(github.head_ref, 'sprint/') == false && ("), "ci-fast.yml"},
		{"a skip on the pull request's head ref", plant("ci-fast.yml", planIf, "  plan:\n    if: !startsWith(github.event.pull_request.head.ref, 'sprint/') && ("), "ci-fast.yml"},
		{"a base-branch gate", plant("ci-fast.yml", planIf, "  plan:\n    if: github.base_ref == 'main' && ("), "ci-fast.yml"},
		{"a label gate", plant("caa-self-test.yml", "    if: (", "    if: contains(github.event.pull_request.labels.*.name, 'full-ci') && ("), ""},
		{"a ref gate on a step", plant("ci-fast.yml", "        if: startsWith(matrix.lang, 'elixir-') && runner.environment == 'github-hosted'", "        if: github.ref_name == 'main'"), ""},
		{"a base filter on the pull_request trigger", plant("roadmap.yml", "  pull_request:\n", "  pull_request:\n    branches: [ main ]\n"), "roadmap.yml"},
	} {
		if len(branchTokenErrors(c.wfs)) == 0 {
			t.Errorf("PLANTED %q left the textual gate green", c.name)
		}
		if c.behaviour != "" && len(branchBehaviourErrors(c.wfs, c.behaviour)) == 0 {
			t.Errorf("PLANTED %q left the evaluated gate green", c.name)
		}
	}
}

// tierRow is a row of the "Every job and its tier" table. It never matches a
// row of the nightly table, whose first cell is a workflow.
var tierRow = regexp.MustCompile("^\\| `([a-z0-9_-]+)` \\| `([a-z0-9._-]+\\.yml)` \\| ([A-Za-z, -]+) \\| (.+) \\|$")

var tierOrder = []string{"per-PR", "fork-PR", "main", "nightly", "dispatch", "comment"}

type tierEntry struct {
	line          int
	job, workflow string
	tiers         string
}

func readTierTable(t *testing.T) string {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "CI-TIERS.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(doc)
}

func parseTierTable(doc string) ([]tierEntry, []string) {
	var rows []tierEntry
	var errs []string
	for i, line := range strings.Split(doc, "\n") {
		m := tierRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var got []string
		for p := range strings.SplitSeq(m[3], ",") {
			p = strings.TrimSpace(p)
			if !slices.Contains(tierOrder, p) {
				errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md:%d: %s names tier %q; the tiers are %v", i+1, m[1], p, tierOrder))
			}
			got = append(got, p)
		}
		rows = append(rows, tierEntry{line: i + 1, job: m[1], workflow: m[2], tiers: strings.Join(got, ", ")})
	}
	return rows, errs
}

// jobTiers evaluates the tiers a workflow gives a job, every plan output set.
func jobTiers(wf, job string) (string, error) {
	triggers := workflowTriggerBranches(wf)
	var tiers []string
	add := func(tier string, ev ciEvent) error {
		runs, err := jobRunsInWorkflow(wf, job, fullPlan(ev))
		if err != nil {
			return err
		}
		if runs && !slices.Contains(tiers, tier) {
			tiers = append(tiers, tier)
		}
		return nil
	}
	for _, evName := range []string{"pull_request", "pull_request_target"} {
		if _, on := triggers[evName]; !on {
			continue
		}
		if err := add("per-PR", ciEvent{Name: evName, Branch: "main", HeadRef: "feature", Repo: tierRepo, HeadRepo: tierRepo}); err != nil {
			return "", err
		}
		if err := add("fork-PR", ciEvent{Name: evName, Branch: "main", HeadRef: "feature", Repo: tierRepo, HeadRepo: "someone/schema"}); err != nil {
			return "", err
		}
	}
	for _, c := range []struct {
		tier string
		ev   ciEvent
	}{
		{"main", ciEvent{Name: "push", Branch: "main", Repo: tierRepo}},
		{"nightly", ciEvent{Name: "schedule", Branch: "main", Repo: tierRepo}},
		{"dispatch", ciEvent{Name: "workflow_dispatch", Branch: "main", Repo: tierRepo}},
		{"comment", ciEvent{Name: "issue_comment", Repo: tierRepo, IssueIsPR: true, IssueUser: "someone", CommentUser: "someone", CommentBody: "recheck"}},
	} {
		if err := add(c.tier, c.ev); err != nil {
			return "", err
		}
	}
	slices.SortFunc(tiers, func(a, b string) int { return slices.Index(tierOrder, a) - slices.Index(tierOrder, b) })
	return strings.Join(tiers, ", "), nil
}

// tieredWorkflows are the workflows whose every job needs a row: those a pull
// request, a push or a comment starts. A schedule- or dispatch-only workflow
// is decided whole in the nightly table (TestEveryNightlyOnlyGateHasAPRTierForm).
func tieredWorkflows(wfs map[string]string) []string {
	var out []string
	for _, name := range tierKeys(wfs) {
		triggers := workflowTriggerBranches(wfs[name])
		for _, ev := range []string{"pull_request", "pull_request_target", "push", "issue_comment"} {
			if _, on := triggers[ev]; on {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

func tierTableErrors(wfs map[string]string, doc string) []string {
	rows, errs := parseTierTable(doc)
	if len(rows) == 0 {
		return append(errs, "docs/CI-TIERS.md has no row in \"Every job and its tier\": the table holds nothing")
	}
	seen := map[string]int{}
	for _, r := range rows {
		key := r.workflow + ":" + r.job
		if prev, dup := seen[key]; dup {
			errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md:%d: %s has a row already at line %d", r.line, key, prev))
			continue
		}
		seen[key] = r.line
		wf, ok := wfs[r.workflow]
		if !ok {
			errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md:%d: %s names workflow %s, which does not exist", r.line, r.job, r.workflow))
			continue
		}
		if _, ok := yamlJobs(wf)[r.job]; !ok {
			errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md:%d: %s names job %s, which %s does not have: a group the table promises runs nowhere", r.line, r.workflow, r.job, r.workflow))
			continue
		}
		got, err := jobTiers(wf, r.job)
		if err != nil {
			errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md:%d: %s cannot be evaluated: %v", r.line, key, err))
			continue
		}
		if got != r.tiers {
			errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md:%d: %s says tiers %q, but the workflow runs it on %q", r.line, key, r.tiers, got))
		}
	}
	for _, name := range tieredWorkflows(wfs) {
		for _, job := range tierKeys(yamlJobs(wfs[name])) {
			if _, ok := seen[name+":"+job]; !ok {
				errs = append(errs, fmt.Sprintf("%s:%s has no row in docs/CI-TIERS.md \"Every job and its tier\": name its tiers there", name, job))
			}
		}
	}
	return errs
}

func TestEveryJobHasItsTierInCITiers(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	doc := readTierTable(t)
	for _, e := range tierTableErrors(wfs, doc) {
		t.Error(e)
	}

	plantDoc := func(from, to string) string {
		t.Helper()
		if !strings.Contains(doc, from) {
			t.Fatalf("control cannot plant: docs/CI-TIERS.md does not contain %q", from)
		}
		return strings.Replace(doc, from, to, 1)
	}
	dropJob := func(name, job string) map[string]string {
		t.Helper()
		m := maps.Clone(wfs)
		body, ok := yamlJobs(m[name])[job]
		if !ok {
			t.Fatalf("control cannot plant: %s has no job %s", name, job)
		}
		m[name] = strings.Replace(m[name], body, "", 1)
		return m
	}
	verifyRow := "| `verify-leg` | `ci-fast.yml` | per-PR, main, dispatch |"
	for _, c := range []struct {
		name string
		wfs  map[string]string
		doc  string
	}{
		{"a group dropped from ci-fast.yml", dropJob("ci-fast.yml", "verify-leg"), doc},
		{"a job with no row", wfs, plantDoc(verifyRow, "| `verify-leg-gone` | `ci-fast.yml` | per-PR, main, dispatch |")},
		{"a row with the wrong tiers", wfs, plantDoc(verifyRow, "| `verify-leg` | `ci-fast.yml` | main |")},
		{"a dispatch-only gate called per-PR", wfs, plantDoc("| `leg-gate` | `ci-fast.yml` | dispatch |", "| `leg-gate` | `ci-fast.yml` | per-PR, main, dispatch |")},
	} {
		if len(tierTableErrors(c.wfs, c.doc)) == 0 {
			t.Errorf("PLANTED %q left the gate green", c.name)
		}
	}
}

var timeoutLine = regexp.MustCompile(`(?m)^    timeout-minutes: (\d+)\s*$`)

func perPRGroupErrors(wfs map[string]string, doc string) []string {
	rows, errs := parseTierTable(doc)
	fast := wfs["ci-fast.yml"]
	jobs := yamlJobs(fast)
	groups := 0
	for _, r := range rows {
		if !slices.Contains(strings.Split(r.tiers, ", "), "per-PR") || r.workflow != "ci-fast.yml" {
			continue
		}
		groups++
		body, ok := jobs[r.job]
		if !ok {
			errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md:%d: per-PR group %s is not a ci-fast.yml job: it runs on no pull request", r.line, r.job))
			continue
		}
		m := timeoutLine.FindStringSubmatch(body)
		if m == nil {
			errs = append(errs, fmt.Sprintf("ci-fast.yml:%s has no job-level timeout-minutes: a per-PR group finishes in two minutes", r.job))
		} else if n, _ := strconv.Atoi(m[1]); n > 2 {
			errs = append(errs, fmt.Sprintf("ci-fast.yml:%s has timeout-minutes: %d: a per-PR group finishes in two minutes; split it into more rows or shards", r.job, n))
		}
		for _, head := range []string{"sprint/schema-main/a-card", "sprint-group/schema-main", "feature"} {
			for _, base := range []string{"main", "sprint/schema-main"} {
				ev := fullPlan(ciEvent{Name: "pull_request", Branch: base, HeadRef: head, Repo: tierRepo, HeadRepo: tierRepo})
				runs, err := jobRunsInWorkflow(fast, r.job, ev)
				if err != nil || !runs {
					errs = append(errs, fmt.Sprintf("ci-fast.yml:%s: a pull request from %s into %s does not run it (runs=%v err=%v): every pull request runs every per-PR group", r.job, head, base, runs, err))
				}
			}
		}
	}
	if groups < 10 {
		errs = append(errs, fmt.Sprintf("docs/CI-TIERS.md names %d per-PR groups of ci-fast.yml: this gate would pass over an empty tier", groups))
	}
	return errs
}

func TestEveryPerPRGroupRunsOnEveryPullRequest(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	doc := readTierTable(t)
	for _, e := range perPRGroupErrors(wfs, doc) {
		t.Error(e)
	}

	plant := func(from, to string) map[string]string {
		t.Helper()
		m := maps.Clone(wfs)
		if !strings.Contains(m["ci-fast.yml"], from) {
			t.Fatalf("control cannot plant: ci-fast.yml does not contain %q", from)
		}
		m["ci-fast.yml"] = strings.Replace(m["ci-fast.yml"], from, to, 1)
		return m
	}
	m := maps.Clone(wfs)
	m["ci-fast.yml"] = strings.Replace(m["ci-fast.yml"], yamlJobs(m["ci-fast.yml"])["verify-leg"], "", 1)
	verifyIf := "    if: ((needs.plan.outputs.all == 'yes' || needs.plan.outputs.verify_legs != '[]')"
	for _, c := range []struct {
		name string
		wfs  map[string]string
	}{
		{"a group dropped from ci-fast.yml", m},
		{"the sprint/ skip put back on a group", plant(verifyIf, "    if: startsWith(github.head_ref, 'sprint/') == false && ((needs.plan.outputs.all == 'yes' || needs.plan.outputs.verify_legs != '[]')")},
		{"a group given three minutes", plant("  vet:\n", "  vet:\n    timeout-minutes: 3\n")},
	} {
		if len(perPRGroupErrors(c.wfs, doc)) == 0 {
			t.Errorf("PLANTED %q left the gate green", c.name)
		}
	}
}
