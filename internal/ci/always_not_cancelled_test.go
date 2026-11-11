package ci

// A job whose `if:` is always() runs even after its run is cancelled. On a
// fleet runner that means a superseded run queues the job for a runner,
// cannot finish, and holds the next run of its concurrency group pending
// behind it (ci-full.yml's `proof` did this). A job that must run after a
// failed need says `always() && !cancelled()`; this gate refuses a job-level
// always() without !cancelled() in every workflow.

import (
	"sort"
	"strings"
	"testing"
)

func alwaysWithoutNotCancelled(cond string) bool {
	return strings.Contains(cond, "always()") && !strings.Contains(cond, "!cancelled()")
}

func TestNoJobRunsAfterItsRunIsCancelled(t *testing.T) {
	t.Parallel()

	if !alwaysWithoutNotCancelled("always() && (github.event_name != 'pull_request')") {
		t.Fatal("PLANTED PATTERN: a bare always() left the gate green")
	}
	if alwaysWithoutNotCancelled("always() && !cancelled() && (github.event_name != 'pull_request')") {
		t.Fatal("PLANTED PATTERN: always() && !cancelled() was refused")
	}

	wfs := workflows(t, repoRoot(t))
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for job := range yamlJobs(wfs[name]) {
			if cond := jobIfCondition(wfs[name], job); alwaysWithoutNotCancelled(cond) {
				t.Errorf("%s:%s runs on always() without !cancelled() (%q): a cancelled run would still queue it for a runner; write always() && !cancelled()", name, job, cond)
			}
		}
	}
}
