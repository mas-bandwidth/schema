package ci

// A CARD BRANCH'S PULL REQUEST RUNS NO CI. The coordinator, 2026-11-15: card
// branches (sprint/<stream>/<card>) skip CI. The sprint lands every card
// through its stream's group branch, sprint-group/<stream>, whose pull request
// keeps the whole fast lane; a card's own pull request gates nothing (a hand
// `nova-sprint land` has no CI gate, and the landing policy never uses rung 1),
// and its card already ran its DONE-WHEN on a bench. On 2026-11-15 card-branch
// runs were 461 of 588 active jobs on the schema runners, and the group's
// verdict queued behind them.
//
// So every job of a workflow that triggers on pull_request or
// pull_request_target opens its job-level `if:` with cardBranchSkip, combined
// with the job's own condition by `&& (...)`. github.head_ref is empty on a
// push, a dispatch and a comment, so only a pull request from a card branch
// skips; a skipped job ends skipped, never failed or pending. This reads the
// workflow text only.
//
// A FORK NAMES ITS OWN BRANCH (CI-TOOLS-2). head_ref cannot tell an in-repo
// sprint/ branch from a fork's branch of the same name, so a job a fork's pull
// request reaches (cla.yml's caa on pull_request_target, ci-fast.yml's
// fork-pull-request) opens with cardBranchSkipForkAware instead, which skips
// only a same-repository card branch. The gate below holds it by behaviour:
// for every pull-request job, a fork's pull request from a branch named
// sprint/... runs it exactly when the same pull request from any other branch
// name would.

import (
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// cardBranchSkip is the gate every pull-request job opens its if: with.
const cardBranchSkip = "startsWith(github.head_ref, 'sprint/') == false"

// cardBranchSkipForkAware is the same gate for a job a fork's pull request
// reaches: only a same-repository card branch skips.
const cardBranchSkipForkAware = "(" + cardBranchSkip + " || github.event.pull_request.head.repo.full_name != github.repository)"

var startsWithLiteral = regexp.MustCompile(`^(?:startsWith\(github\.head_ref, '([^']*)'\) == false|\(startsWith\(github\.head_ref, '([^']*)'\) == false \|\| github\.event\.pull_request\.head\.repo\.full_name != github\.repository\))`)

// cardBranchSkipErrors names every pull-request job that does not open its
// if: with the card-branch skip, and a skip whose prefix would also skip the
// group branch or would not skip a card branch.
func cardBranchSkipErrors(wfs map[string]string) []string {
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
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
			cond, ok := "", false
			for line := range strings.SplitSeq(jobs[j], "\n") {
				if c, found := strings.CutPrefix(line, "    if: "); found {
					cond, ok = c, true
					break
				}
			}
			if !ok {
				errs = append(errs, fmt.Sprintf("%s:%s runs on a pull request with no job-level if:; open it with %q so a card branch's pull request skips it", name, j, cardBranchSkip))
				continue
			}
			m := startsWithLiteral.FindStringSubmatch(cond)
			if m == nil {
				errs = append(errs, fmt.Sprintf("%s:%s does not open its if: with %q: a card branch's pull request would run it (if: %s)", name, j, cardBranchSkip, cond))
				continue
			}
			// startsWith is case-insensitive in Actions
			lit := m[1] + m[2]
			prefix := strings.ToLower(lit)
			groupRef := "sprint-group/schema-main"
			cardRef := "sprint/schema-main/a-card"
			if strings.HasPrefix(groupRef, prefix) {
				errs = append(errs, fmt.Sprintf("%s:%s skips on head_ref prefix %q, which also skips the group branch sprint-group/schema-main: the landing gate would run nothing", name, j, lit))
			}
			if !strings.HasPrefix(cardRef, prefix) {
				errs = append(errs, fmt.Sprintf("%s:%s skips on head_ref prefix %q, which does not skip the card branch sprint/schema-main/a-card", name, j, lit))
			}
			// a fork's branch name must not decide whether the job runs
			for _, ev := range []string{"pull_request", "pull_request_target"} {
				if _, on := triggers[ev]; !on {
					continue
				}
				fork := func(head string) ciEvent {
					return ciEvent{Name: ev, Branch: "main", HeadRef: head, Repo: "mas-bandwidth/schema", HeadRepo: "someone/schema"}
				}
				named, err1 := jobRunsInWorkflow(wfs[name], j, fork("sprint/x"))
				other, err2 := jobRunsInWorkflow(wfs[name], j, fork("feature-x"))
				if err1 != nil || err2 != nil {
					errs = append(errs, fmt.Sprintf("%s:%s: its if: cannot be evaluated for a fork's %s: %v %v", name, j, ev, err1, err2))
				} else if named != other {
					errs = append(errs, fmt.Sprintf("%s:%s: a fork's %s from a branch named sprint/... runs=%v, from any other name runs=%v: a fork chooses its branch name, so open the if: with %s", name, j, ev, named, other, cardBranchSkipForkAware))
				}
			}
			if rest := cond[len(m[0]):]; rest != "" && (!strings.HasPrefix(rest, " && (") || !strings.HasSuffix(rest, ")")) {
				errs = append(errs, fmt.Sprintf("%s:%s combines the card-branch skip with its own condition other than by `&& (...)`: %s", name, j, cond))
			}
		}
	}
	return errs
}

func TestCardBranchPullRequestsSkipCI(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	if len(wfs) == 0 {
		t.Fatal("no workflows read: this gate would pass over an empty set")
	}
	for _, name := range []string{"ci-fast.yml", "roadmap.yml", "caa-self-test.yml"} {
		if !strings.Contains(wfs[name], "    if: "+cardBranchSkip) {
			t.Errorf("%s carries no card-branch skip: the gate below would pass over it", name)
		}
	}
	for _, name := range []string{"cla.yml", "ci-fast.yml"} {
		if !strings.Contains(wfs[name], "    if: "+cardBranchSkipForkAware) {
			t.Errorf("%s carries no fork-aware card-branch skip: the fork rule below would pass over it", name)
		}
	}
	for _, e := range cardBranchSkipErrors(wfs) {
		t.Error(e)
	}
	// the gate as Actions evaluates it: a card branch's pull request skips
	// plan, the group branch's runs it, and a push to main still runs CAA's
	// self-test
	for _, c := range []struct {
		wf, job string
		ev      ciEvent
		want    bool
	}{
		{"ci-fast.yml", "plan", ciEvent{Name: "pull_request", Branch: "main", HeadRef: "sprint/schema-main/a-card", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}, false},
		{"ci-fast.yml", "plan", ciEvent{Name: "pull_request", Branch: "main", HeadRef: "sprint-group/schema-main", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}, true},
		{"caa-self-test.yml", "caa-self-test", ciEvent{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"}, true},
		// CI-TOOLS-2: a fork's branch named sprint/... is still checked for
		// the CAA; the table's own card branch is not
		{"cla.yml", "caa", ciEvent{Name: "pull_request_target", Branch: "main", HeadRef: "sprint/anything", Repo: "mas-bandwidth/schema", HeadRepo: "someone/schema"}, true},
		{"cla.yml", "caa", ciEvent{Name: "pull_request_target", Branch: "main", HeadRef: "sprint/schema-main/a-card", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}, false},
		// CI-TOOLS-1: a fork's pull request gets the red fork job, whatever
		// its branch is named; the table's own pull request does not
		{"ci-fast.yml", "fork-pull-request", ciEvent{Name: "pull_request", Branch: "main", HeadRef: "sprint/anything", Repo: "mas-bandwidth/schema", HeadRepo: "someone/schema"}, true},
		{"ci-fast.yml", "fork-pull-request", ciEvent{Name: "pull_request", Branch: "main", HeadRef: "feature", Repo: "mas-bandwidth/schema", HeadRepo: "someone/schema"}, true},
		{"ci-fast.yml", "fork-pull-request", ciEvent{Name: "pull_request", Branch: "main", HeadRef: "feature", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}, false},
	} {
		got, err := jobRunsInWorkflow(wfs[c.wf], c.job, c.ev)
		if err != nil || got != c.want {
			t.Errorf("%s:%s on %s from %q: runs=%v err=%v, want runs=%v", c.wf, c.job, c.ev.Name, c.ev.HeadRef, got, err, c.want)
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
		{"a job without the skip", plant("cla.yml", "    if: "+cardBranchSkipForkAware+"\n", "")},
		{"a fork's branch name skips the CAA", plant("cla.yml", "    if: "+cardBranchSkipForkAware+"\n", "    if: "+cardBranchSkip+"\n")},
		{"a skip dropped from a combined if", plant("ci-fast.yml", "    if: "+cardBranchSkip+" && (", "    if: (")},
		{"a prefix that also skips the group branch", plant("roadmap.yml", "'sprint/'", "'sprint'")},
		{"a prefix that skips no card branch", plant("ci-fast.yml", "'sprint/'", "'sprint-group/'")},
		{"the skip joined by ||", plant("caa-self-test.yml", cardBranchSkip+" && (", cardBranchSkip+" || (")},
	} {
		if len(cardBranchSkipErrors(c.wfs)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", c.name)
		}
	}
}
