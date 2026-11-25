package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaCsRunsHostileCorpusWiring holds the card's blades without
// needing a .NET SDK in the Go unit tier: the make target exists and is a
// prerequisite of the port's normal unit target, its runner reads the committed
// manifest and every FIX-CARD names a case the manifest carries. The corpus
// runs as the registry's per-case hostile rows (tools/schema-test/hostile.go).
func TestSchemaCsRunsHostileCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile, err := os.ReadFile(filepath.Join(root, "make", "schema-cs-runs-hostile-corpus.mk"))
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	makeText := string(makeFile)
	for _, want := range []string{
		".PHONY: schema-cs-runs-hostile-corpus",
		"test-cs: schema-cs-runs-hostile-corpus",
		"schema-cs-runs-hostile-corpus-negative-control",
		"test/cs-hostile/Runner.cs",
		"internal/ci/hostilecorpus/corpus",
	} {
		if !strings.Contains(makeText, want) {
			t.Errorf("make/schema-cs-runs-hostile-corpus.mk does not carry %q", want)
		}
	}

	runner, err := os.ReadFile(filepath.Join(root, "test", "cs-hostile", "Runner.cs"))
	if err != nil {
		t.Fatalf("the port runner is missing: %v", err)
	}
	// The runner isolates every case in a worker process on a 512 KiB stack
	// thread: a stack overflow cannot be caught in .NET, so the pool is the
	// blade that keeps one crash from hiding the rest.
	for _, want := range []string{"manifest.tsv", "FIX-CARD", "known-findings.tsv", "--worker", "512 * 1024", "HOSTILE_CASE_BUDGET_MS"} {
		if !strings.Contains(string(runner), want) {
			t.Errorf("test/cs-hostile/Runner.cs does not carry %q", want)
		}
	}
	gen, err := os.ReadFile(filepath.Join(root, "test", "cs-hostile", "gen", "main.go"))
	if err != nil {
		t.Fatalf("the dispatch generator is missing: %v", err)
	}
	for _, want := range []string{"FromJson(", "LoadMessages(", "LoadRetain(", "AnnounceRead(", "Row* \" + t + \"Load(System.IntPtr region", "\"public static bool Read\" + t"} {
		if !strings.Contains(string(gen), want) {
			t.Errorf("test/cs-hostile/gen/main.go does not carry %q", want)
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

	findingsBytes, err := os.ReadFile(filepath.Join(root, "test", "cs-hostile", "known-findings.tsv"))
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
		if len(f) != 5 {
			t.Fatalf("carried finding has %d fields, want 5 (name, slug, input, answer, one line): %q", len(f), line)
		}
		input, ok := cases[f[0]]
		if !ok {
			t.Errorf("carried finding %q names a case the manifest does not carry", f[0])
			continue
		}
		if f[2] != input {
			t.Errorf("carried finding %q names input %q, manifest says %q", f[0], f[2], input)
		}
		if !strings.HasPrefix(f[1], "cs-hostile-") || strings.TrimSpace(f[3]) == "" {
			t.Errorf("carried finding %q has no FIX-CARD slug and one line: %q", f[0], line)
		}
	}
}

// nightlyOnlyTriggerProblems names every way a heavy workflow's top-level on:
// block breaks the nightly-and-dispatch-only rule (the owner, 2026-11-15: "I want
// everything that is not light moved to nightly test now." "no exceptions."):
// it must carry schedule and workflow_dispatch, and must not carry push,
// pull_request or pull_request_target.
func nightlyOnlyTriggerProblems(wf string) []string {
	triggers := workflowTriggerBranches(wf)
	var problems []string
	for _, need := range []string{"schedule", "workflow_dispatch"} {
		if _, ok := triggers[need]; !ok {
			problems = append(problems, "the on: block has no "+need+": trigger")
		}
	}
	for _, banned := range []string{"push", "pull_request", "pull_request_target"} {
		if _, ok := triggers[banned]; ok {
			problems = append(problems, "the on: block carries a "+banned+": trigger; heavy suites run nightly and on dispatch only")
		}
	}
	return problems
}
