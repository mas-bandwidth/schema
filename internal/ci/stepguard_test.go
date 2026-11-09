package ci

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// stepGuardExempt is the closed list of ci-full.yml per-commit fleet jobs
// whose steps are measured in minutes by design and so clear the 110s
// per-step wall with a job-level empty PGROUP_WALL: pins (the re-pin
// rewrite), leg-tagged (the tagged builds) and negative-controls (rows cut
// to 120s of control time, past the wall). A job may join this list only by
// naming itself here and in the workflow in the same change.
var stepGuardExempt = map[string]bool{
	"leg-tagged":        true,
	"negative-controls": true,
	"pins":              true,
}

var (
	stepGuardEventRe = regexp.MustCompile(`github\.event_name == '([a-z_]+)'`)
	stepGuardWallRe  = regexp.MustCompile(`(?m)^  PGROUP_WALL: (.+)$`)
	stepGuardClearRe = regexp.MustCompile(`(?m)^\s+PGROUP_WALL: (''|"")\s*$`)
)

// checkStepGuard holds ci-full.yml to the per-commit step wall: the
// workflow-level PGROUP_WALL gives push and pull_request runs 110s per step
// and every other event (schedule, workflow_dispatch) no wall; every fleet
// job runs its steps under the pgroup shell that reads it; and exactly the
// exempt jobs clear it at the job level.
func checkStepGuard(wf string) []string {
	var errs []string

	m := stepGuardWallRe.FindStringSubmatch(wf)
	if m == nil {
		return []string{"ci-full.yml: no workflow-level PGROUP_WALL: per-commit steps have no 110s wall, so the two-minute rule is invisible until the runner kills the job"}
	}
	expr := m[1]
	events := map[string]bool{}
	for _, e := range stepGuardEventRe.FindAllStringSubmatch(expr, -1) {
		events[e[1]] = true
	}
	if !events["push"] || !events["pull_request"] || len(events) != 2 {
		errs = append(errs, "ci-full.yml: PGROUP_WALL applies to events "+strings.Join(sortedKeys(events), ", ")+"; it must apply to exactly push and pull_request, so schedule and workflow_dispatch runs keep the long budget")
	}
	if !strings.Contains(expr, "'110s'") || !strings.Contains(expr, "|| ''") {
		errs = append(errs, "ci-full.yml: PGROUP_WALL "+expr+" must be the per-commit 110s wall with an empty (long-budget) value for other events")
	}

	jobs := yamlJobs(wf)
	if len(jobs) == 0 {
		return append(errs, "ci-full.yml: no jobs parsed: this gate would pass over an empty set")
	}
	fleet, cleared := 0, map[string]bool{}
	for name, body := range jobs {
		if !strings.Contains(jobRunsOn(body), "self-hosted") {
			continue
		}
		fleet++
		shell := ""
		for raw := range strings.SplitSeq(body, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(raw), "shell:"); ok {
				shell = strings.TrimSpace(v)
				break
			}
		}
		if shell != pgroupShell {
			errs = append(errs, "ci-full.yml:"+name+" runs its steps under "+shell+", not "+pgroupShell+": its steps never see PGROUP_WALL")
		}
		if stepGuardClearRe.MatchString(body) {
			cleared[name] = true
			if !stepGuardExempt[name] {
				errs = append(errs, "ci-full.yml:"+name+" clears PGROUP_WALL but is not in the step-guard exemption list: a per-commit fleet job would run past 110s per step with nothing naming the rule")
			}
		}
	}
	if fleet == 0 {
		errs = append(errs, "ci-full.yml: no fleet job found: this gate would pass over an empty set")
	}
	for name := range stepGuardExempt {
		if !cleared[name] {
			errs = append(errs, "ci-full.yml:"+name+" is exempt from the 110s wall in the test but does not clear PGROUP_WALL in the workflow: the exemption list and the workflow disagree")
		}
	}
	sort.Strings(errs)
	return errs
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestPerCommitStepWall(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	wf, ok := wfs["ci-full.yml"]
	if !ok {
		t.Fatal("ci-full.yml is the workflow this gate reads")
	}
	for _, e := range checkStepGuard(wf) {
		t.Error(e)
	}
}

// TestPerCommitStepWallNegativeControls mutates a copy of ci-full.yml each
// way the guard can be lost and requires the check to name it.
func TestPerCommitStepWallNegativeControls(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	base, ok := wfs["ci-full.yml"]
	if !ok {
		t.Fatal("ci-full.yml is the workflow these controls mutate")
	}
	cases := []struct {
		name, old, new, want string
	}{
		{
			"wall removed",
			"  PGROUP_WALL: ${{ (github.event_name == 'push' || github.event_name == 'pull_request') && '110s' || '' }}\n",
			"",
			"no workflow-level PGROUP_WALL",
		},
		{
			"wall given to the nightly instead",
			"github.event_name == 'push'",
			"github.event_name == 'schedule'",
			"must apply to exactly push and pull_request",
		},
		{
			"wall loosened past two minutes",
			"&& '110s' || ''",
			"&& '900s' || ''",
			"must be the per-commit 110s wall",
		},
		{
			"exempt job stops clearing the wall",
			"    env:\n      PGROUP_WALL: ''\n    defaults:\n      run:\n        shell: go run ./tools/pgroup -- bash -e {0}\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n\n      # the siblings go-test's compiler rows clone",
			"    defaults:\n      run:\n        shell: go run ./tools/pgroup -- bash -e {0}\n    steps:\n      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n\n      # the siblings go-test's compiler rows clone",
			"leg-tagged is exempt",
		},
	}
	for _, c := range cases {
		if !strings.Contains(base, c.old) {
			t.Fatalf("%s: ci-full.yml no longer carries %q", c.name, c.old)
		}
		errs := strings.Join(checkStepGuard(strings.Replace(base, c.old, c.new, 1)), "\n")
		if !strings.Contains(errs, c.want) {
			t.Errorf("%s: the check did not report %q; it reported:\n%s", c.name, c.want, errs)
		}
	}
}
