package ci

import (
	"fmt"
	"strings"
	"testing"
)

// releaseProofRunPrefix opens the one line that picks the ci-full run whose
// proof report the full lane's release lock reads.
const releaseProofRunPrefix = `run=$(gh run list --workflow ci-full.yml --commit "$GITHUB_SHA" `

// releaseProofRunErrors holds the full lane's lock to a GREEN ci-full run at
// the commit (the finding CI-TOOLS-5): ci-full's `proof` job needs only the
// conformance rows, so a run red on go-test, generated, the negative controls
// or vuln can still carry a green proof report. Only the run's conclusion,
// which includes its `verdict` job, says the whole suite passed.
func releaseProofRunErrors(content string) []string {
	var lines []string
	for line := range strings.SplitSeq(content, "\n") {
		if strings.Contains(line, releaseProofRunPrefix) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	if len(lines) != 1 {
		return []string{fmt.Sprintf("release.yml picks the ci-full run for the proof report on %d lines, want exactly one starting %s", len(lines), releaseProofRunPrefix)}
	}
	var errs []string
	if !strings.Contains(lines[0], " --status success ") {
		errs = append(errs, "release.yml reads the proof report from a ci-full run of any conclusion: a run red outside the conformance rows would release (want --status success): "+lines[0])
	}
	_, after, _ := strings.Cut(content, lines[0])
	next, _, _ := strings.Cut(strings.TrimLeft(after, "\n"), "\n")
	if !strings.Contains(next, `if [ -z "$run" ]`) || !strings.Contains(next, "exit 1") {
		errs = append(errs, "release.yml does not refuse when there is no green ci-full run at the commit: the line after the run pick must exit 1 on an empty run")
	}
	return errs
}

func TestReleaseLockReadsOnlyAGreenCIFullRun(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["release.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/release.yml")
	}
	for _, e := range releaseProofRunErrors(wf) {
		t.Error(e)
	}
	for _, c := range []struct{ name, from, to, want string }{
		{"any conclusion", "--status success --limit 1 --json databaseId --jq '.[0].databaseId // empty')\n          if [ -z \"$run\" ]; then echo \"release REFUSES", "--status completed --limit 1 --json databaseId --jq '.[0].databaseId // empty')\n          if [ -z \"$run\" ]; then echo \"release REFUSES", "of any conclusion"},
		{"no run goes on", "at $GITHUB_SHA (a red, cancelled or missing run carries no proof the release can take)\"; exit 1; fi", "at $GITHUB_SHA (a red, cancelled or missing run carries no proof the release can take)\"; exit 0; fi", "does not refuse"},
	} {
		mutated := strings.Replace(wf, c.from, c.to, 1)
		if mutated == wf {
			t.Fatalf("negative control %q did not mutate release.yml", c.name)
		}
		if errs := releaseProofRunErrors(mutated); !strings.Contains(strings.Join(errs, "\n"), c.want) {
			t.Errorf("NEGATIVE CONTROL FAILED: %q: got %q, want an error containing %q", c.name, errs, c.want)
		}
	}
}
