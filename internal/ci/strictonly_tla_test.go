package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestStrictOnlyModelsAreRecorded holds the strict-only layer 1 models to the
// record the rest of the tree reads: tla/ReadOrder.tla defines OneVerdict, a
// positive config checks it, a reversed witness config turns exactly one fault
// TRUE and checks it, and tla/RUNS.tsv carries a row for each run.
func TestStrictOnlyModelsAreRecorded(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	modelPath := filepath.Join(root, "tla", "ReadOrder.tla")
	model, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read tla/ReadOrder.tla: %v", err)
	}
	oneVerdict := regexp.MustCompile(`(?m)^OneVerdict[ \t]*==`)
	if !oneVerdict.Match(model) {
		t.Fatalf("tla/ReadOrder.tla has no `OneVerdict ==` definition")
	}

	positivePath := filepath.Join(root, "tla", "MCReadOrder.cfg")
	positive, err := readTLAConfig(positivePath)
	if err != nil {
		t.Fatalf("read MCReadOrder.cfg: %v", err)
	}
	if !positive.invariants["OneVerdict"] {
		t.Fatalf("tla/MCReadOrder.cfg does not check OneVerdict")
	}
	for name, value := range positive.constants {
		if value && isTLABooleanFault(name) {
			t.Fatalf("tla/MCReadOrder.cfg sets fault %s TRUE; the positive config holds every fault FALSE", name)
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "tla", "MCReadOrder*.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	reversedName := ""
	for _, path := range paths {
		cfg, err := readTLAConfig(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		if !cfg.invariants["OneVerdict"] {
			continue
		}
		trueFaults := 0
		for name, value := range cfg.constants {
			if value && isTLABooleanFault(name) {
				trueFaults++
			}
		}
		if trueFaults == 1 {
			reversedName = filepath.Base(path)
			break
		}
	}
	if reversedName == "" {
		t.Fatalf("no MCReadOrder*.cfg turns exactly one fault TRUE and checks OneVerdict")
	}

	runs, err := readTLARuns(t, filepath.Join(root, "tla", "RUNS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MCReadOrder.cfg", reversedName} {
		row, ok := runs[name]
		if !ok {
			t.Fatalf("tla/RUNS.tsv has no row for %s", name)
		}
		if row.module != "ReadOrder" {
			t.Errorf("tla/RUNS.tsv row %s names module %q, want ReadOrder", name, row.module)
		}
	}
	if row := runs["MCReadOrder.cfg"]; row.outcome != "exhaustive" {
		t.Errorf("tla/RUNS.tsv row MCReadOrder.cfg outcome is %q, want exhaustive", row.outcome)
	}
	if row := runs[reversedName]; row.outcome != "counterexample" || row.invariant != "OneVerdict" {
		t.Errorf("tla/RUNS.tsv row %s is outcome %q invariant %q, want counterexample OneVerdict", reversedName, row.outcome, row.invariant)
	}
}

type tlaConfig struct {
	constants  map[string]bool
	invariants map[string]bool
}

func readTLAConfig(path string) (tlaConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tlaConfig{}, err
	}
	cfg := tlaConfig{constants: map[string]bool{}, invariants: map[string]bool{}}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "\\*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "INVARIANT" {
			for _, name := range fields[1:] {
				cfg.invariants[name] = true
			}
			continue
		}
		if len(fields) >= 4 && fields[0] == "CONSTANT" && fields[2] == "=" {
			switch fields[3] {
			case "TRUE":
				cfg.constants[fields[1]] = true
			case "FALSE":
				cfg.constants[fields[1]] = false
			}
		}
	}
	return cfg, nil
}

func isTLABooleanFault(name string) bool {
	return name != "Records" && name != "Contents"
}

type tlaRun struct {
	module    string
	outcome   string
	invariant string
}

func readTLARuns(t *testing.T, path string) (map[string]tlaRun, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 {
		return nil, os.ErrInvalid
	}
	header := strings.Split(lines[0], "\t")
	column := map[string]int{}
	for i, name := range header {
		column[name] = i
	}
	for _, name := range []string{"config", "module", "outcome", "invariant"} {
		if _, ok := column[name]; !ok {
			t.Fatalf("tla/RUNS.tsv has no %q column", name)
		}
	}
	runs := map[string]tlaRun{}
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) != len(header) {
			t.Fatalf("tla/RUNS.tsv row %q has %d columns, want %d", line, len(cells), len(header))
		}
		runs[cells[column["config"]]] = tlaRun{
			module:    cells[column["module"]],
			outcome:   cells[column["outcome"]],
			invariant: cells[column["invariant"]],
		}
	}
	return runs, nil
}
