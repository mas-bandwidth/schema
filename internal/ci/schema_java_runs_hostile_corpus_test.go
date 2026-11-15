package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaJavaRunsHostileCorpusWiring holds the card's blades without
// needing a JDK in the Go unit tier: the make target exists and is a
// prerequisite of the port's normal unit target, its runner reads the committed
// manifest and every FIX-CARD names a case the manifest carries, and the
// workflow runs nightly and on dispatch only (the owner, 2026-11-15).
func TestSchemaJavaRunsHostileCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile, err := os.ReadFile(filepath.Join(root, "make", "schema-java-runs-hostile-corpus.mk"))
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	makeText := string(makeFile)
	for _, want := range []string{
		".PHONY: schema-java-runs-hostile-corpus",
		"test-java: schema-java-runs-hostile-corpus",
		"schema-java-runs-hostile-corpus-negative-control",
		"test/java-hostile/Runner.java",
		"internal/ci/hostilecorpus/corpus",
	} {
		if !strings.Contains(makeText, want) {
			t.Errorf("make/schema-java-runs-hostile-corpus.mk does not carry %q", want)
		}
	}

	runner, err := os.ReadFile(filepath.Join(root, "test", "java-hostile", "Runner.java"))
	if err != nil {
		t.Fatalf("the port runner is missing: %v", err)
	}
	for _, want := range []string{"manifest.tsv", "FIX-CARD", "known-findings.tsv", "fromJson", "loadMessages", "loadRetain", "\"packet\"", "512 * 1024", "HOSTILE_CASE_BUDGET_MS"} {
		if !strings.Contains(string(runner), want) {
			t.Errorf("test/java-hostile/Runner.java does not carry %q", want)
		}
	}

	manifestBytes, err := os.ReadFile(filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus", "manifest.tsv"))
	if err != nil {
		t.Fatalf("the committed manifest is missing: %v", err)
	}
	cases := map[string]string{}
	for line := range strings.SplitSeq(string(manifestBytes), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line has %d fields, want 9: %q", len(f), line)
		}
		input := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus", f[4])
		if info, err := os.Stat(input); err != nil || info.IsDir() {
			t.Errorf("manifest case %q names input %q, which is not a committed file: %v", f[0], f[4], err)
		}
		cases[f[0]] = f[4]
	}
	if len(cases) == 0 {
		t.Fatal("the manifest names no case — this gate would pass over an empty corpus")
	}

	findingsBytes, err := os.ReadFile(filepath.Join(root, "test", "java-hostile", "known-findings.tsv"))
	if err != nil {
		t.Fatalf("the carried-findings list is missing: %v", err)
	}
	// An empty list is valid once every corpus case agrees. The native runner
	// still rejects unexplained disagreements and stale carried findings.
	for line := range strings.SplitSeq(string(findingsBytes), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("carried finding has %d fields, want 4: %q", len(f), line)
		}
		input, ok := cases[f[0]]
		if !ok {
			t.Errorf("carried finding %q names a case the manifest does not carry", f[0])
			continue
		}
		if f[2] != input {
			t.Errorf("carried finding %q names input %q, manifest says %q", f[0], f[2], input)
		}
		if !strings.HasPrefix(f[1], "java-hostile-") || strings.TrimSpace(f[3]) == "" {
			t.Errorf("carried finding %q has no FIX-CARD slug and one line: %q", f[0], line)
		}
	}

	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-java-runs-hostile-corpus.yml"))
	if err != nil {
		t.Fatalf("the card's workflow is missing: %v", err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"runs-on: [self-hosted, linux, x64, schema]",
		"if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository",
	} {
		if !strings.Contains(workflowText, want) {
			t.Errorf("the workflow does not carry %q", want)
		}
	}
	// NIGHTLY AND DISPATCH ONLY (the owner, 2026-11-15: "I want everything that is
	// not light moved to nightly test now." "no exceptions."): the on: block
	// carries no push and no pull_request trigger.
	for _, p := range nightlyOnlyTriggerProblems(workflowText) {
		t.Errorf("schema-java-runs-hostile-corpus.yml: %s", p)
	}
	if strings.Contains(workflowText, "runs-on: ubuntu-latest") {
		t.Error("the workflow runs on a GitHub-hosted runner; every CI job is on the fleet")
	}
	if !strings.Contains(workflowText, "schema-java-runs-hostile-corpus-negative-control") {
		t.Error("the workflow does not run the planted-bad-pattern control")
	}

	// Self-hosted admission for corpus and negative-control.
	for _, ev := range []ciEvent{
		{Name: "pull_request", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "fork/schema"},
		{Name: "pull_request", Branch: "dev", Repo: "mas-bandwidth/schema", HeadRepo: "fork/schema", Labels: []string{"full-ci"}},
	} {
		if ok, err := jobSelectsSelfHosted(workflowText, "corpus", ev); err != nil || ok {
			t.Errorf("Java corpus job admitted fork PR (%+v): ok=%v err=%v", ev, ok, err)
		}
		if ok, err := jobSelectsSelfHosted(workflowText, "negative-control", ev); err != nil || ok {
			t.Errorf("Java negative-control job admitted fork PR (%+v): ok=%v err=%v", ev, ok, err)
		}
	}
	for _, ev := range []ciEvent{
		{Name: "schedule", Branch: "main", Repo: "mas-bandwidth/schema"},
		{Name: "workflow_dispatch", Branch: "main", Repo: "mas-bandwidth/schema"},
	} {
		if ok, err := jobSelectsSelfHosted(workflowText, "corpus", ev); err != nil || !ok {
			t.Errorf("Java corpus job did not run for allowed event (%+v): ok=%v err=%v", ev, ok, err)
		}
		if ok, err := jobSelectsSelfHosted(workflowText, "negative-control", ev); err != nil || !ok {
			t.Errorf("Java negative-control did not run for allowed event (%+v): ok=%v err=%v", ev, ok, err)
		}
	}
	// No pull request and no push, to main, dev or a card branch, starts this
	// workflow: a push or pull request runs only the fast lane, ci-fast.yml.
	for _, ev := range []ciEvent{
		{Name: "pull_request", Branch: "dev", HeadRef: "sprint/schema-java", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"},
		{Name: "pull_request", Branch: "main", HeadRef: "dev", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "dev", Repo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "sprint/schema-java", Repo: "mas-bandwidth/schema"},
	} {
		if ok, err := jobSelectsSelfHosted(workflowText, "corpus", ev); err != nil || ok {
			t.Errorf("Java corpus job ran on (%+v): ok=%v err=%v; the workflow is nightly and dispatch only", ev, ok, err)
		}
	}
	// Mutation checks: a restored pull_request or push trigger is seen, both by
	// the job evaluator and by the nightly-only check.
	withPR := strings.Replace(workflowText, "\non:\n", "\non:\n  pull_request:\n", 1)
	if ok, err := jobSelectsSelfHosted(withPR, "corpus", ciEvent{Name: "pull_request", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}); err != nil || !ok {
		t.Errorf("trigger mutation control: a restored pull_request trigger was not seen: ok=%v err=%v", ok, err)
	}
	if len(nightlyOnlyTriggerProblems(withPR)) == 0 {
		t.Error("nightly-only control: a restored pull_request trigger was not refused")
	}
	withPush := strings.Replace(workflowText, "\non:\n", "\non:\n  push:\n    branches: [ main, dev ]\n", 1)
	if ok, err := jobSelectsSelfHosted(withPush, "corpus", ciEvent{Name: "push", Branch: "dev", Repo: "mas-bandwidth/schema"}); err != nil || !ok {
		t.Errorf("trigger mutation control: a restored push trigger was not seen: ok=%v err=%v", ok, err)
	}
	if len(nightlyOnlyTriggerProblems(withPush)) == 0 {
		t.Error("nightly-only control: a restored push trigger was not refused")
	}
	noSchedule := strings.Replace(workflowText, "\n  schedule:\n", "\n  # schedule removed\n", 1)
	if len(nightlyOnlyTriggerProblems(noSchedule)) == 0 {
		t.Error("nightly-only control: a workflow without schedule: was not refused")
	}
}
