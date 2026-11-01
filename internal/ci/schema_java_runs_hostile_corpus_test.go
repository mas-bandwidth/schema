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
// workflow has both a push/pull-request fast gate and a nightly control.
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
	carried := 0
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
		carried++
	}
	if carried == 0 {
		t.Fatal("the carried-findings list is empty — a corpus with unexplained disagreements needs no list, and a list with no teeth needs a case")
	}

	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-java-runs-hostile-corpus.yml"))
	if err != nil {
		t.Fatalf("the card's workflow is missing: %v", err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		"push:",
		"pull_request:",
		"schedule:",
		"workflow_dispatch:",
		"runs-on: [self-hosted, linux, x64, schema]",
		"branches: [ main, dev, fixed-table-form, 'sprint/**' ]",
		"if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository",
	} {
		if !strings.Contains(workflowText, want) {
			t.Errorf("the workflow does not carry %q", want)
		}
	}
	if strings.Contains(workflowText, "runs-on: ubuntu-latest") {
		t.Error("the workflow runs on a GitHub-hosted runner; every CI job is on the fleet")
	}
	if !strings.Contains(workflowText, "schema-java-runs-hostile-corpus-negative-control") {
		t.Error("the workflow does not run the planted-bad-pattern control")
	}

	// Self-hosted admission and dev/main trigger assertions for corpus and negative-control.
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
		{Name: "pull_request", Branch: "dev", HeadRef: "sprint/schema-java", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"},
		{Name: "pull_request", Branch: "main", HeadRef: "dev", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "dev", Repo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "main", Repo: "mas-bandwidth/schema"},
		{Name: "push", Branch: "sprint/schema-java", Repo: "mas-bandwidth/schema"},
		{Name: "schedule", Branch: "main", Repo: "mas-bandwidth/schema"},
		{Name: "workflow_dispatch", Branch: "main", Repo: "mas-bandwidth/schema"},
	} {
		if ok, err := jobSelectsSelfHosted(workflowText, "corpus", ev); err != nil || !ok {
			t.Errorf("Java corpus job did not run for allowed event (%+v): ok=%v err=%v", ev, ok, err)
		}
		wantNeg := ev.Name == "schedule" || ev.Name == "workflow_dispatch"
		if ok, err := jobSelectsSelfHosted(workflowText, "negative-control", ev); err != nil || ok != wantNeg {
			t.Errorf("Java negative-control under (%+v) = %v (err=%v), want %v", ev, ok, err, wantNeg)
		}
	}
	// Mutation checks: dropping same-repo guard admits fork PR; dropping dev branch skips stream-to-dev PR.
	noGuard := strings.Replace(workflowText, "    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository\n", "", 1)
	if ok, err := jobSelectsSelfHosted(noGuard, "corpus", ciEvent{Name: "pull_request", Branch: "main", Repo: "mas-bandwidth/schema", HeadRepo: "fork/schema"}); err != nil || !ok {
		t.Errorf("guard mutation control did not admit fork PR as expected: ok=%v err=%v", ok, err)
	}
	noDev := strings.ReplaceAll(workflowText, "branches: [ main, dev, fixed-table-form, 'sprint/**' ]", "branches: [ main, 'sprint/**' ]")
	if ok, err := jobSelectsSelfHosted(noDev, "corpus", ciEvent{Name: "pull_request", Branch: "dev", HeadRef: "sprint/schema-java", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}); err != nil || ok {
		t.Errorf("trigger mutation control still ran stream-to-dev PR after removing dev: ok=%v err=%v", ok, err)
	}
}
