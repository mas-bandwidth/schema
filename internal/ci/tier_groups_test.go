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
// The rule this test holds the workflows to: no `if:`, job or step, reads a
// branch name, a ref or a label; no pull-request trigger filters its base
// branches. The fork guard (head.repo.full_name == github.repository) is
// keyed on the repository and stays: a fork's code never runs on the fleet.
// (ci-fast.yml's two-minute cap is twoMinuteErrors' in nightly_test.go.)
//
// All of it reads the workflow text and evaluates `if:` with the model in
// ci_test.go (evalCIExpr); nothing runs.

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
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
	ev.PlanElixirCorpus = "true"
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
	fastJobs := yamlJobs(wfs["ci-fast.yml"])
	for _, key := range append([]string{"ci-fast.yml:fork-pull-request"}, ciFastJobKeys(wfs)...) {
		if _, job, _ := strings.Cut(key, ":"); fastJobs[job] == "" {
			t.Fatalf("ci-fast.yml has no %s job: this gate would pass over an empty fast lane", job)
		}
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
		for _, key := range ciFastJobKeys(wfs) {
			_, job, _ := strings.Cut(key, ":")
			if runs, err := jobRunsInWorkflow(wfs["ci-fast.yml"], job, fork); err != nil || runs {
				t.Errorf("a fork's pull request from %q runs ci-fast.yml %s on the fleet (runs=%v err=%v)", head, job, runs, err)
			}
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
	shardIf := "    needs: [fixed-corpus, produce]\n    if: ("
	for _, c := range []struct {
		name      string
		wfs       map[string]string
		behaviour string // the workflow the evaluated half must catch it in; "" when only the text gate can
	}{
		{"the sprint/ skip put back on the shards", plant("ci-fast.yml", shardIf, "    needs: [fixed-corpus, produce]\n    if: startsWith(github.head_ref, 'sprint/') == false && ("), "ci-fast.yml"},
		{"a skip on the pull request's head ref", plant("ci-fast.yml", shardIf, "    needs: [fixed-corpus, produce]\n    if: !startsWith(github.event.pull_request.head.ref, 'sprint/') && ("), "ci-fast.yml"},
		{"a base-branch gate", plant("ci-fast.yml", shardIf, "    needs: [fixed-corpus, produce]\n    if: github.base_ref == 'main' && ("), "ci-fast.yml"},
		{"a label gate", plant("roadmap.yml", "    if: (", "    if: contains(github.event.pull_request.labels.*.name, 'full-ci') && ("), ""},
		{"a ref gate on a step", plant("ci-fast.yml", "        if: matrix.lang == 'none'", "        if: github.ref_name == 'main'"), ""},
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
