package ci

// The C# hostile-corpus runner's planted-bad-pattern control
// (docs/HOSTILE-CORPUS.md). The runner itself is the generated C# reader; this
// test plants a wrong verdict in a scratch copy of the manifest and requires
// the runner to name it, then requires the unplanted corpus to answer.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

func runCSHostile(t *testing.T, runner, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("dotnet", runner, "corpus", dir)
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
	dir := t.TempDir()
	manifest, err := os.ReadFile(filepath.Join(hostileCorpus(t), "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(hostileCorpus(t), "inputs"), filepath.Join(dir, "inputs")); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(manifest), "\n")
	planted := false
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		f[6], f[7], f[8] = "refuse", "damaged", "-"
		lines[i] = strings.Join(f, "\t")
		planted = true
		break
	}
	if !planted {
		t.Fatal("no manifest row to plant")
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.tsv"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCSHostile(t, runner, dir)
	if err == nil {
		t.Fatalf("a planted wrong verdict was NOT caught:\n%s", out)
	}
	if !strings.Contains(out, "FIX-CARD: schema-cs-hostile-corpus:") {
		t.Fatalf("the runner failed without naming the case:\n%s", out)
	}
}
