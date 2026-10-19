package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaElixirRunsHostileCorpusWiring holds the card's blades without
// needing the BEAM in the Go unit tier: the make target exists and is a
// prerequisite of the port's normal unit target, its runner reads the committed
// manifest and every FIX-CARD names a case the manifest carries, and the
// workflow has both a push/pull-request fast gate and a nightly control.
func TestSchemaElixirRunsHostileCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile, err := os.ReadFile(filepath.Join(root, "make", "schema-elixir-runs-hostile-corpus.mk"))
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	makeText := string(makeFile)
	for _, want := range []string{
		".PHONY: schema-elixir-runs-hostile-corpus",
		"test-elixir: schema-elixir-runs-hostile-corpus",
		"schema-elixir-runs-hostile-corpus-negative-control",
		"test/elixir-hostile/runner.exs",
		"internal/ci/hostilecorpus/corpus",
	} {
		if !strings.Contains(makeText, want) {
			t.Errorf("make/schema-elixir-runs-hostile-corpus.mk does not carry %q", want)
		}
	}

	runner, err := os.ReadFile(filepath.Join(root, "test", "elixir-hostile", "runner.exs"))
	if err != nil {
		t.Fatalf("the port runner is missing: %v", err)
	}
	for _, want := range []string{"manifest.tsv", "FIX-CARD", "known-findings.tsv", "from_json", "load_messages"} {
		if !strings.Contains(string(runner), want) {
			t.Errorf("test/elixir-hostile/runner.exs does not carry %q", want)
		}
	}

	manifestBytes, err := os.ReadFile(filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus", "manifest.tsv"))
	if err != nil {
		t.Fatalf("the committed manifest is missing: %v", err)
	}
	cases := map[string]string{}
	for _, line := range strings.Split(string(manifestBytes), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line has %d fields, want 9: %q", len(f), line)
		}
		cases[f[0]] = f[4]
	}
	if len(cases) == 0 {
		t.Fatal("the manifest names no case — this gate would pass over an empty corpus")
	}

	findingsBytes, err := os.ReadFile(filepath.Join(root, "test", "elixir-hostile", "known-findings.tsv"))
	if err != nil {
		t.Fatalf("the carried-findings list is missing: %v", err)
	}
	carried := 0
	for _, line := range strings.Split(string(findingsBytes), "\n") {
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
		if !strings.HasPrefix(f[1], "elixir-hostile-") || strings.TrimSpace(f[3]) == "" {
			t.Errorf("carried finding %q has no FIX-CARD slug and one line: %q", f[0], line)
		}
		carried++
	}
	if carried == 0 {
		t.Fatal("the carried-findings list is empty — a corpus with unexplained disagreements needs no list, and a list with no teeth needs a case")
	}

	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-elixir-runs-hostile-corpus.yml"))
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
	} {
		if !strings.Contains(workflowText, want) {
			t.Errorf("the workflow does not carry %q", want)
		}
	}
	if strings.Contains(workflowText, "runs-on: ubuntu-latest") {
		t.Error("the workflow runs on a GitHub-hosted runner; every CI job is on the fleet")
	}
	if !strings.Contains(workflowText, "schema-elixir-runs-hostile-corpus-negative-control") {
		t.Error("the workflow does not run the planted-bad-pattern control")
	}
}
