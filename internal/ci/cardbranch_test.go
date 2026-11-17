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

var startsWithLiteral = regexp.MustCompile(`^startsWith\(github\.head_ref, '([^']*)'\) == false`)

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
			prefix := strings.ToLower(m[1])
			if strings.HasPrefix("sprint-group/schema-main", prefix) {
				errs = append(errs, fmt.Sprintf("%s:%s skips on head_ref prefix %q, which also skips the group branch sprint-group/schema-main: the landing gate would run nothing", name, j, m[1]))
			}
			if !strings.HasPrefix("sprint/schema-main/a-card", prefix) {
				errs = append(errs, fmt.Sprintf("%s:%s skips on head_ref prefix %q, which does not skip the card branch sprint/schema-main/a-card", name, j, m[1]))
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
	for _, name := range []string{"ci-fast.yml", "python-leg.yml", "roadmap.yml", "caa-self-test.yml", "cla.yml"} {
		if !strings.Contains(wfs[name], "    if: "+cardBranchSkip) {
			t.Errorf("%s carries no card-branch skip: the gate below would pass over it", name)
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
		{"a job without the skip", plant("cla.yml", "    if: "+cardBranchSkip+"\n", "")},
		{"a skip dropped from a combined if", plant("ci-fast.yml", "    if: "+cardBranchSkip+" && (", "    if: (")},
		{"a prefix that also skips the group branch", plant("roadmap.yml", "'sprint/'", "'sprint'")},
		{"a prefix that skips no card branch", plant("python-leg.yml", "'sprint/'", "'sprint-group/'")},
		{"the skip joined by ||", plant("caa-self-test.yml", cardBranchSkip+" && (", cardBranchSkip+" || (")},
	} {
		if len(cardBranchSkipErrors(c.wfs)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", c.name)
		}
	}
}
