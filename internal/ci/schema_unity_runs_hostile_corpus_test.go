package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaUnityRunsHostileCorpusWiring holds the card's blades without
// needing a .NET SDK in the Go unit tier: the make target exists and is a
// prerequisite of the port's normal unit target, its runner reads the committed
// manifest and every FIX-CARD names a case the manifest carries. The corpus
// runs as the registry's per-case hostile rows (tools/schema-test/hostile.go).
func TestSchemaUnityRunsHostileCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile, err := os.ReadFile(filepath.Join(root, "make", "schema-unity-runs-hostile-corpus.mk"))
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	makeText := string(makeFile)
	for _, want := range []string{
		".PHONY: schema-unity-runs-hostile-corpus",
		"test-unity: schema-unity-runs-hostile-corpus",
		"schema-unity-runs-hostile-corpus-negative-control",
		"test/unity-hostile/Runner.cs",
		"internal/ci/hostilecorpus/corpus",
	} {
		if !strings.Contains(makeText, want) {
			t.Errorf("make/schema-unity-runs-hostile-corpus.mk does not carry %q", want)
		}
	}

	runner, err := os.ReadFile(filepath.Join(root, "test", "unity-hostile", "Runner.cs"))
	if err != nil {
		t.Fatalf("the port runner is missing: %v", err)
	}
	// The runner isolates every case in a worker process on a 512 KiB stack
	// thread: a stack overflow cannot be caught in .NET, so the pool is the
	// blade that keeps one crash from hiding the rest.
	for _, want := range []string{"manifest.tsv", "FIX-CARD", "known-findings.tsv", "--worker", "512 * 1024", "HOSTILE_CASE_BUDGET_MS"} {
		if !strings.Contains(string(runner), want) {
			t.Errorf("test/unity-hostile/Runner.cs does not carry %q", want)
		}
	}
	gen, err := os.ReadFile(filepath.Join(root, "test", "unity-hostile", "gen", "main.go"))
	if err != nil {
		t.Fatalf("the dispatch generator is missing: %v", err)
	}
	for _, want := range []string{"FromJson(ref ", "LoadMessages(", "LoadRetain(ref ", "AnnounceRead(", "FixedLoad(", "\"public static bool Read\" + t"} {
		if !strings.Contains(string(gen), want) {
			t.Errorf("test/unity-hostile/gen/main.go does not carry %q", want)
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

	findingsBytes, err := os.ReadFile(filepath.Join(root, "test", "unity-hostile", "known-findings.tsv"))
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
		if !strings.HasPrefix(f[1], "unity-hostile-") || strings.TrimSpace(f[3]) == "" {
			t.Errorf("carried finding %q has no FIX-CARD slug and one line: %q", f[0], line)
		}
	}
}
