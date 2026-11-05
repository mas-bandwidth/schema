package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStrictOnlyModelsAreRecorded(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	model, err := os.ReadFile(filepath.Join(root, "tla", "ReadOrder.tla"))
	if err != nil {
		t.Fatalf("read tla/ReadOrder.tla: %v", err)
	}
	if !regexp.MustCompile(`(?m)^OneVerdict\s*==`).Match(model) {
		t.Fatal("tla/ReadOrder.tla has no OneVerdict invariant")
	}

	configs := []struct {
		name string
		want []string
	}{
		{"MCReadOrder.cfg", []string{"INVARIANT OneVerdict"}},
		{"MCReadOrderPartialValue.cfg", []string{"INVARIANT OneVerdict", "CONSTANT PartialValue = TRUE"}},
	}
	for _, c := range configs {
		data, err := os.ReadFile(filepath.Join(root, "tla", c.name))
		if err != nil {
			t.Fatalf("read tla/%s: %v", c.name, err)
		}
		for _, want := range c.want {
			if !strings.Contains(string(data), want) {
				t.Errorf("tla/%s does not contain %q", c.name, want)
			}
		}
	}

	runs, err := os.ReadFile(filepath.Join(root, "tla", "RUNS.tsv"))
	if err != nil {
		t.Fatalf("read tla/RUNS.tsv: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(runs)), "\n")
	if len(lines) < 2 {
		t.Fatal("tla/RUNS.tsv has no data rows")
	}
	header := strings.Split(lines[0], "\t")
	col := make(map[string]int, len(header))
	for i, name := range header {
		col[name] = i
	}
	for _, name := range []string{"config", "module", "outcome", "invariant"} {
		if _, ok := col[name]; !ok {
			t.Fatalf("tla/RUNS.tsv header has no %q column", name)
		}
	}

	wants := []struct {
		config    string
		outcome   string
		invariant string
	}{
		{"MCReadOrder.cfg", "exhaustive", "-"},
		{"MCReadOrderPartialValue.cfg", "counterexample", "OneVerdict"},
	}
	found := make([]bool, len(wants))
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != len(header) {
			t.Fatalf("tla/RUNS.tsv row has %d fields, want %d: %q", len(fields), len(header), line)
		}
		for i, want := range wants {
			if fields[col["config"]] != want.config {
				continue
			}
			found[i] = true
			if got := fields[col["module"]]; got != "ReadOrder" {
				t.Errorf("tla/RUNS.tsv row for %s names module %q, want ReadOrder", want.config, got)
			}
			if got := fields[col["outcome"]]; got != want.outcome {
				t.Errorf("tla/RUNS.tsv row for %s names outcome %q, want %q", want.config, got, want.outcome)
			}
			if got := fields[col["invariant"]]; got != want.invariant {
				t.Errorf("tla/RUNS.tsv row for %s names invariant %q, want %q", want.config, got, want.invariant)
			}
		}
	}
	for i, want := range wants {
		if !found[i] {
			t.Errorf("tla/RUNS.tsv has no row for %s", want.config)
		}
	}
}
