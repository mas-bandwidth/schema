package ci

// A CARD BRANCH'S PULL REQUEST RUNS THE FAST LANE. The rule: we merge on
// CI against current main. From 2026-11-15 every job of a pull-request
// workflow opened its if: with startsWith(github.head_ref, 'sprint/') ==
// false, so a card branch's pull request (sprint/<stream>/<card>) ran
// nothing. The sprint's automatic landing gates on its group branch
// (sprint-group/<stream>), which kept the lane, but a hand landing
// (nova-sprint land, or judge land.<card> land) opens the card's own pull
// request against main and merges it at once. All 41 sprint/ pull requests
// merged into main from 2026-11-16 to 2026-11-17 merged that way, and on
// every one each fast-lane job ended SKIPPED (#4364: 15 checks, all
// skipped), which the sprint's forge counts as green. The first verdict on
// their code was main's own, after they had landed.
//
// So no job of a workflow that a pull request starts reads the head branch's
// name: a card branch's pull request runs exactly what any other branch's
// would. This reads the workflow text, and evaluates each job's if: where
// the expression model can.
//
// THE ONE EXCEPTION is cla.yml's caa, the Contributor Assignment Agreement
// check (pull_request_target, the org token). It keeps its card-branch skip,
// and only in the fork-aware form: a fork names its own branch, so only a
// same-repository card branch may skip it (CI-TOOLS-2). Whether card commits
// should face the CAA check is a policy call, not a CI-load one, and is
// outside this gate.
//
// FORK SAFETY IS UNCHANGED: a fork's pull request starts only the red,
// hosted fork-pull-request job, whatever its branch is named.

import (
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// cardBranchSkipForkAware is cla.yml's caa skip: only a same-repository card
// branch skips it.
const cardBranchSkipForkAware = "(startsWith(github.head_ref, 'sprint/') == false || github.event.pull_request.head.repo.full_name != github.repository)"

// cardBranchSkipAllowed is the one job that may still read the branch name.
var cardBranchSkipAllowed = map[string]string{"cla.yml:caa": cardBranchSkipForkAware}

// headRefRead is any way an if: can read the head branch's name.
var headRefRead = regexp.MustCompile(`(?i)github\.head_ref|github\.event\.pull_request\.head\.ref|github\.event\.pull_request\.head\.label`)

// cardBranchEvent is a pull request from head in headRepo, with a plan that
// selects every gate, so a job's own condition is not what skips it.
func cardBranchEvent(head, headRepo string) ciEvent {
	return ciEvent{
		Name: "pull_request", Branch: "main", HeadRef: head,
		Repo: "mas-bandwidth/schema", HeadRepo: headRepo,
		PlanPkgs: "./...", PlanLegs: `["cpp","rust","lobster","squirrel"]`,
		PlanVerifyLegs: `["elixir","dart"]`, PlanDartCorpus: "true", PlanCppCorpus: "true",
		PlanRows: `[{"make":"tables-go-fixedform"}]`,
	}
}

// cardBranchSkipErrors names every pull-request job whose if: reads the head
// branch's name (bar the allowed exception, exactly as written), every job
// that a same-repository card branch's pull request would run differently
// from any other branch's, and every job a fork's pull request would run
// differently for a branch named sprint/... It also returns how many jobs it
// evaluated, so the caller can refuse a gate that evaluated none.
func cardBranchSkipErrors(wfs map[string]string) ([]string, int) {
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	evaluated := 0
	for _, name := range names {
		triggers := workflowTriggerBranches(wfs[name])
		_, pr := triggers["pull_request"]
		_, prt := triggers["pull_request_target"]
		if !pr && !prt {
			continue
		}
		jobs := yamlJobs(wfs[name])
		var jobNames []string
		for j := range jobs {
			jobNames = append(jobNames, j)
		}
		sort.Strings(jobNames)
		for _, j := range jobNames {
			cond := jobIfCondition(wfs[name], j)
			if want, ok := cardBranchSkipAllowed[name+":"+j]; ok {
				if cond != want {
					errs = append(errs, fmt.Sprintf("%s:%s: its if: is %q; the one allowed card-branch skip is exactly %q", name, j, cond, want))
				}
			} else if headRefRead.MatchString(cond) {
				errs = append(errs, fmt.Sprintf("%s:%s reads the head branch's name in its if: (%s): a card branch's pull request must run what any other branch's does, so the sprint merges on CI against main", name, j, cond))
			}
			for _, ev := range []string{"pull_request", "pull_request_target"} {
				if _, on := triggers[ev]; !on {
					continue
				}
				at := func(head, repo string) ciEvent {
					e := cardBranchEvent(head, repo)
					e.Name = ev
					return e
				}
				// a fork's branch name never decides whether a job runs
				named, err1 := jobRunsInWorkflow(wfs[name], j, at("sprint/x", "someone/schema"))
				other, err2 := jobRunsInWorkflow(wfs[name], j, at("feature-x", "someone/schema"))
				if err1 == nil && err2 == nil && named != other {
					errs = append(errs, fmt.Sprintf("%s:%s: a fork's %s from a branch named sprint/... runs=%v, from any other name runs=%v: a fork chooses its branch name", name, j, ev, named, other))
				}
				if _, ok := cardBranchSkipAllowed[name+":"+j]; ok {
					continue
				}
				// a same-repository card branch runs what any other branch runs
				card, err1 := jobRunsInWorkflow(wfs[name], j, at("sprint/schema-main/a-card", "mas-bandwidth/schema"))
				plain, err2 := jobRunsInWorkflow(wfs[name], j, at("feature-x", "mas-bandwidth/schema"))
				if err1 != nil || err2 != nil {
					continue // the expression model cannot read it; the text check above still holds it
				}
				evaluated++
				if card != plain {
					errs = append(errs, fmt.Sprintf("%s:%s: a card branch's %s runs=%v, any other branch's runs=%v: the card's code would land with no verdict from this job", name, j, ev, card, plain))
				}
			}
		}
	}
	return errs, evaluated
}

func TestCardBranchPullRequestsRunCI(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	if len(wfs) == 0 {
		t.Fatal("no workflows read: this gate would pass over an empty set")
	}
	for _, name := range []string{"ci-fast.yml", "caa-self-test.yml", "roadmap.yml", "cla.yml"} {
		if wfs[name] == "" {
			t.Errorf("%s was not read: the gate below would pass over it", name)
		}
	}
	errs, evaluated := cardBranchSkipErrors(wfs)
	for _, e := range errs {
		t.Error(e)
	}
	// ci-fast.yml alone has more than twenty pull-request jobs; a gate that
	// evaluated a handful has lost its expression model
	t.Logf("%d pull-request jobs evaluated for a card branch", evaluated)
	if evaluated < 20 {
		t.Errorf("only %d pull-request jobs were evaluated for a card branch: the behaviour check is hollow", evaluated)
	}
	// the gate as Actions evaluates it
	inRepo, fork := "mas-bandwidth/schema", "someone/schema"
	for _, c := range []struct {
		wf, job string
		ev      ciEvent
		want    bool
	}{
		// a card branch's pull request runs the fast lane, as the group
		// branch's and any other's do
		{"ci-fast.yml", "plan", cardBranchEvent("sprint/schema-main/a-card", inRepo), true},
		{"ci-fast.yml", "go-test-touched", cardBranchEvent("sprint/schema-main/a-card", inRepo), true},
		{"ci-fast.yml", "verdict", cardBranchEvent("sprint/schema-main/a-card", inRepo), true},
		{"ci-fast.yml", "plan", cardBranchEvent("sprint-group/schema-main", inRepo), true},
		{"caa-self-test.yml", "caa-self-test", cardBranchEvent("sprint/schema-main/a-card", inRepo), true},
		{"roadmap.yml", "roadmap", cardBranchEvent("sprint/schema-main/a-card", inRepo), true},
		{"caa-self-test.yml", "caa-self-test", ciEvent{Name: "push", Branch: "main", Repo: inRepo}, true},
		// fork safety: a fork's pull request reaches no fleet job, whatever
		// its branch is named, and gets the red hosted fork job
		{"ci-fast.yml", "plan", cardBranchEvent("sprint/schema-main/a-card", fork), false},
		{"ci-fast.yml", "plan", cardBranchEvent("feature", fork), false},
		{"ci-fast.yml", "verdict", cardBranchEvent("sprint/anything", fork), false},
		{"caa-self-test.yml", "caa-self-test", cardBranchEvent("sprint/anything", fork), false},
		{"ci-fast.yml", "fork-pull-request", cardBranchEvent("sprint/anything", fork), true},
		{"ci-fast.yml", "fork-pull-request", cardBranchEvent("feature", fork), true},
		{"ci-fast.yml", "fork-pull-request", cardBranchEvent("feature", inRepo), false},
		{"ci-fast.yml", "fork-pull-request", cardBranchEvent("sprint/schema-main/a-card", inRepo), false},
		// CI-TOOLS-2: a fork's branch named sprint/... is still checked for
		// the CAA; the sprint's own card branch is not
		{"cla.yml", "caa", ciEvent{Name: "pull_request_target", Branch: "main", HeadRef: "sprint/anything", Repo: inRepo, HeadRepo: fork}, true},
		{"cla.yml", "caa", ciEvent{Name: "pull_request_target", Branch: "main", HeadRef: "sprint/schema-main/a-card", Repo: inRepo, HeadRepo: inRepo}, false},
	} {
		got, err := jobRunsInWorkflow(wfs[c.wf], c.job, c.ev)
		if err != nil || got != c.want {
			t.Errorf("%s:%s on %s from %s %q: runs=%v err=%v, want runs=%v", c.wf, c.job, c.ev.Name, c.ev.HeadRepo, c.ev.HeadRef, got, err, c.want)
		}
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
		name string
		wfs  map[string]string
	}{
		{"the card-branch skip back on ci-fast's plan", plant("ci-fast.yml", "  plan:\n    if: ", "  plan:\n    if: startsWith(github.head_ref, 'sprint/') == false && ")},
		{"the fork-aware skip on ci-fast's fork job", plant("ci-fast.yml", "  fork-pull-request:\n    if: ", "  fork-pull-request:\n    if: "+cardBranchSkipForkAware+" && ")},
		{"a skip spelled in another case on roadmap", plant("roadmap.yml", "  roadmap:\n    if: ", "  roadmap:\n    if: !startsWith(github.HEAD_REF, 'Sprint/') && ")},
		{"a skip on the pull request's head.ref", plant("caa-self-test.yml", "  caa-self-test:\n    if: ", "  caa-self-test:\n    if: !startsWith(github.event.pull_request.head.ref, 'sprint/') && ")},
		{"a fork's branch name skips the CAA", plant("cla.yml", "    if: "+cardBranchSkipForkAware+"\n", "    if: startsWith(github.head_ref, 'sprint/') == false\n")},
	} {
		if errs, _ := cardBranchSkipErrors(c.wfs); len(errs) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", c.name)
		}
	}
}
