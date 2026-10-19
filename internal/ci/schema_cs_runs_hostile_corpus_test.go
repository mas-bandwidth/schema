package ci

// The C# hostile-corpus runner's planted-bad-pattern control
// (docs/HOSTILE-CORPUS.md). The runner itself is the generated C# reader; this
// test switches on a test-only reader bug (a corrupted decoded value for the
// surrogate_pair case) and requires the runner to name it, then requires the
// unplanted corpus to answer. No manifest or fixture is changed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const csHostilePlantCase = "fuzz-text-surrogate_pair"

func csHostileRunner(t *testing.T) string {
	t.Helper()
	runner := os.Getenv("SCHEMA_CS_HOSTILE_RUNNER")
	if runner == "" {
		t.Skip("SCHEMA_CS_HOSTILE_RUNNER is not set; run make schema-cs-runs-hostile-corpus")
	}
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("runner %s does not exist: %v", runner, err)
	}
	return runner
}

func runCSHostile(t *testing.T, runner, dir string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("dotnet", runner, "corpus", dir)
	if len(env) != 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func hostileCorpus(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "internal", "ci", "hostilecorpus", "corpus")
}

func TestSchemaCSRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	runner := csHostileRunner(t)
	out, err := runCSHostile(t, runner, hostileCorpus(t))
	if err != nil {
		t.Fatalf("the C# reader disagrees with the manifest:\n%s", out)
	}
	if !strings.Contains(out, "cases agree with the manifest") {
		t.Fatalf("the runner printed no agreement line:\n%s", out)
	}
}

func TestSchemaCSRunsHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	runner := csHostileRunner(t)
	out, err := runCSHostile(t, runner, hostileCorpus(t), "SCHEMA_CS_HOSTILE_SABOTAGE=surrogate_pair")
	if err == nil {
		t.Fatalf("a planted reader bug was NOT caught:\n%s", out)
	}
	want := "FIX-CARD: schema-cs-hostile-corpus: " + csHostilePlantCase
	if !strings.Contains(out, want) {
		t.Fatalf("the runner failed without naming the case %q:\n%s", csHostilePlantCase, out)
	}
	clean, err := runCSHostile(t, runner, hostileCorpus(t))
	if err != nil {
		t.Fatalf("the unplanted corpus is red:\n%s", clean)
	}
}
