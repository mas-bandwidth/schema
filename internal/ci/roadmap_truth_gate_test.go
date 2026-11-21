package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The roadmap truth gate (tools/roadmap/truthgate_test.go) runs in ci-fast on
// every pull request and push, in a job of its own whatever the diff touches,
// with no build tags, no skip and no environment switch: the tags are what
// hid moo from the default binary in v3.0.1, and a diff filter would let a
// compiler change break a ticked cell unseen.
func TestCIFastRunsTheRoadmapTruthGate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	job := yamlJobs(wf)["roadmap-truth"]
	if job == "" {
		t.Fatal("ci-fast.yml has no roadmap-truth job")
	}
	const run = "        run: go test -count=1 -run '^(TestDefaultBuildTargetsAreTheDocumentedLanguages|TestEveryTickedRoadmapCellGenerates)$' ./tools/roadmap/\n"
	if strings.Count(job, run) != 1 {
		t.Errorf("roadmap-truth must run the two truth-gate tests exactly once:\n%s", run)
	}
	for _, banned := range []string{"-tags", "GOFLAGS", "-skip", "-short", "continue-on-error", "needs:", "SCHEMA_"} {
		if strings.Contains(job, banned) {
			t.Errorf("roadmap-truth carries %q: the gate runs untagged, unfiltered and unconditionally", banned)
		}
	}
	if jobIfCondition(wf, "roadmap-truth") != jobIfCondition(wf, "vet") {
		t.Error("roadmap-truth must have the same admission as vet: every pull request and push ci-fast runs")
	}
	if !strings.Contains(job, "    timeout-minutes: 2\n") {
		t.Error("roadmap-truth must keep the fast lane's two-minute cap")
	}
}
