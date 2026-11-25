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
// manifest and no list carries a finding (every disagreement is red by name). The corpus
// runs as the registry's per-case hostile rows (tools/schema-test/hostile.go).
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
	for _, want := range []string{"manifest.tsv", "fromJson", "loadMessages", "loadRetain", "\"packet\"", "512 * 1024", "HOSTILE_CASE_BUDGET_MS"} {
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

	// NO LIST CARRIES A FINDING (docs/rulings.json no-skips): every
	// disagreement is red by its case's name, so neither a findings file nor
	// a runner that reads one is wired here
	if _, err := os.Stat(filepath.Join(root, "test", "java-hostile", "known-findings.tsv")); !os.IsNotExist(err) {
		t.Errorf("test/java-hostile/known-findings.tsv is present: a list that carries findings by name (%v)", err)
	}
	for _, carry := range []string{"known-findings", "FIX-CARD", "STALE"} {
		if strings.Contains(string(runner), carry) {
			t.Errorf("test/java-hostile/Runner.java carries %q: a finding is never carried", carry)
		}
	}
}
