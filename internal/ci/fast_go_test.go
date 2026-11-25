package ci

import (
	"regexp"
	"strings"
	"testing"
)

// TestCIFastAndCIFullRunOnPullRequestWhateverItsBase is the sprint-base gate:
// CI fast runs on a pull request into any base; CI full runs on none (it is
// nightly). A fork pull request still never reaches a self-hosted job.
func TestCIFastAndCIFullRunOnPullRequestWhateverItsBase(t *testing.T) {
	wfs := workflows(t, repoRoot(t))
	ev := ciEvent{
		Name:     "pull_request",
		Branch:   "feature/not-a-named-base",
		HeadRef:  "topic",
		Repo:     "mas-bandwidth/schema",
		HeadRepo: "mas-bandwidth/schema",
		PlanPkgs: "./internal/codegen/gotable",
		PlanLegs: `["go"]`,
	}
	for _, key := range ciFastJobKeys(wfs) {
		_, job, _ := strings.Cut(key, ":")
		if ok, err := jobRunsInWorkflow(wfs["ci-fast.yml"], job, ev); err != nil || !ok {
			t.Fatalf("ci-fast.yml %s did not run on a pull request into %s: ok=%v err=%v", job, ev.Branch, ok, err)
		}
	}
	// ci-full.yml runs nightly and on dispatch only (the owner, 2026-11-15: "I
	// want everything that is not light moved to nightly test now." "no
	// exceptions."): no pull request, whatever its base, starts it.
	if workflowTriggersOn(wfs["ci-full.yml"], ev) {
		t.Fatalf("ci-full.yml triggers on a pull request into %s; it runs nightly and on dispatch only", ev.Branch)
	}
	fork := ev
	fork.HeadRepo = "fork/schema"
	fork.PlanVerifyLegs = `["elixir"]`
	fork.Labels = []string{"full-ci"}
	for _, name := range []string{"ci-fast.yml"} {
		if branches := workflowTriggerBranches(wfs[name])["pull_request"]; len(branches) != 0 {
			t.Fatalf("%s pull_request filters bases %v; it must run whatever the base is", name, branches)
		}
		allYamlJobs := yamlJobs(wfs[name])
		if len(allYamlJobs) == 0 {
			t.Fatal("yamlJobs(wfs[name]) is empty: the loop below would check nothing")
		}
		for nameJob, body := range allYamlJobs {
			if !strings.Contains(jobRunsOn(body), "self-hosted") {
				continue
			}
			runs, err := jobRunsInWorkflow(wfs[name], nameJob, fork)
			if err != nil {
				t.Fatalf("%s %s eval error on a fork pull request: %v", name, nameJob, err)
			}
			if runs {
				t.Fatalf("%s %s admits a fork pull request onto a self-hosted runner", name, nameJob)
			}
		}
	}
	if _, ok := workflowTriggerBranches(wfs["ci-full.yml"])["push"]; ok {
		t.Fatal("ci-full.yml gained a push trigger; it runs nightly and on dispatch only")
	}
	// the fork job runs for a fork's pull request and for nothing else
	for _, c := range []struct {
		ev   ciEvent
		want bool
	}{
		{fork, true},
		{ciEvent{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"}, false},
		{ciEvent{Name: "pull_request", Branch: "main", HeadRef: "feature", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}, false},
		{ciEvent{Name: "workflow_dispatch", Branch: "main", Repo: "mas-bandwidth/schema"}, false},
	} {
		if runs, err := jobRunsInWorkflow(wfs["ci-fast.yml"], "fork-pull-request", c.ev); err != nil || runs != c.want {
			t.Fatalf("ci-fast.yml fork-pull-request on %s from %q: runs=%v err=%v, want %v", c.ev.Name, c.ev.HeadRepo, runs, err, c.want)
		}
	}
	// ci-fast.yml runs on a push to main and nowhere else: the sprint reads
	// this lane's verdict on every base tip to tell a card's red from main's.
	if push := workflowTriggerBranches(wfs["ci-fast.yml"])["push"]; len(push) != 1 || push[0] != "main" {
		t.Fatalf("ci-fast.yml push branches %v; it runs on a push to main only", push)
	}
	if !workflowTriggersOn(wfs["ci-fast.yml"], ciEvent{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"}) {
		t.Fatal("ci-fast.yml does not trigger on a push to main")
	}
	mainPush := ciEvent{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema", PlanPkgs: "./internal/codegen/gotable", PlanLegs: `["go"]`, PlanVerifyLegs: `["cpp","dart","elixir","go"]`, PlanDartCorpus: "true", PlanCppCorpus: "true"}
	fastJobs := yamlJobs(wfs["ci-fast.yml"])
	if len(fastJobs) == 0 {
		t.Fatal("ci-fast.yml has no jobs: the push-to-main check below would check nothing")
	}
	checkedOnPush := 0
	for nameJob, body := range fastJobs {
		if strings.Contains(body, "github.event_name == 'workflow_dispatch'") {
			continue // the leg rows run on dispatch only
		}
		if nameJob == "fork-pull-request" {
			continue // a fork's pull request only: the red verdict that no CI ran
		}
		runs, err := jobRunsInWorkflow(wfs["ci-fast.yml"], nameJob, mainPush)
		if err != nil {
			t.Fatalf("ci-fast.yml %s eval error on a push to main: %v", nameJob, err)
		}
		if !runs {
			t.Fatalf("ci-fast.yml %s does not run on a push to main", nameJob)
		}
		checkedOnPush++
	}
	if checkedOnPush == 0 {
		t.Fatal("every ci-fast.yml job was skipped: no job was checked to run on a push to main")
	}
}

func yamlJobs(wf string) map[string]string {
	_, after, ok := strings.Cut(wf, "\njobs:\n")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`(?m)^  ([a-zA-Z][a-zA-Z0-9_-]*):`)
	idxs := re.FindAllStringSubmatchIndex(after, -1)
	out := map[string]string{}
	for i, loc := range idxs {
		name := after[loc[2]:loc[3]]
		end := len(after)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		out[name] = after[loc[0]:end]
	}
	return out
}
