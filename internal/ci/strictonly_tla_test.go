package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	var reversedNames []string
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
			reversedNames = append(reversedNames, filepath.Base(path))
		}
	}
	if len(reversedNames) == 0 {
		t.Fatalf("no MCReadOrder*.cfg turns exactly one fault TRUE and checks OneVerdict")
	}

	runs, err := readTLARuns(t, filepath.Join(root, "tla", "RUNS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	names := append([]string{"MCReadOrder.cfg"}, reversedNames...)
	for _, name := range names {
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
	for _, name := range reversedNames {
		if row := runs[name]; row.outcome != "counterexample" || row.invariant != "OneVerdict" {
			t.Errorf("tla/RUNS.tsv row %s is outcome %q invariant %q, want counterexample OneVerdict", name, row.outcome, row.invariant)
		}
	}
}

// TestTLARunsHashEveryModelAndConfig holds each recorded run to the bytes it
// measured: the run's config and the module it names, plus every local module
// that one EXTENDS and every file no row records, are hashed into the row's
// model_sha256. Editing any of them without recording the run again leaves the
// row stale, and the controls below prove a changed recorded or unrecorded
// model or config is named. tla/run-tlc.sh runs the same inputs on the nightly.
func TestTLARunsHashEveryModelAndConfig(t *testing.T) {
	t.Parallel()
	tlaDir := filepath.Join(repoRoot(t), "tla")
	runs, err := readTLARuns(t, filepath.Join(tlaDir, "RUNS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatal("tla/RUNS.tsv records no runs")
	}
	for _, e := range verifyTLARunHashes(tlaDir, runs) {
		t.Error(e)
	}

	for _, control := range []struct{ name, file string }{
		{"a model file", "FixedBatch.tla"},
		{"a config file", "MCFixedBatch.cfg"},
		{"an unrecorded model file", "CsReadReportIso.tla"},
		{"an unrecorded config file", "MCCsReadReportIsoBroken.cfg"},
	} {
		t.Run(control.name, func(t *testing.T) {
			dir := t.TempDir()
			copyTLAFiles(t, tlaDir, dir)
			path := filepath.Join(dir, control.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("\n\\* edited after the recorded run\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			mutated, err := readTLARuns(t, filepath.Join(dir, "RUNS.tsv"))
			if err != nil {
				t.Fatal(err)
			}
			if errs := verifyTLARunHashes(dir, mutated); len(errs) == 0 {
				t.Fatalf("changing %s left every model_sha256 current", control.file)
			}
		})
	}
}

func verifyTLARunHashes(tlaDir string, runs map[string]tlaRun) []string {
	extra, err := tlaUnrecordedFiles(tlaDir, runs)
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	for config, row := range runs {
		want, err := tlaModelHash(tlaDir, row.module, config, extra)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", config, err))
			continue
		}
		if row.model != want {
			errs = append(errs, fmt.Sprintf("%s: model_sha256 is %q, want %q for its .tla and .cfg files", config, row.model, want))
		}
	}
	sort.Strings(errs)
	return errs
}

// tlaUnrecordedFiles names every .tla or .cfg file no row records. They join
// every row's hash, so adding a model or config without a recorded run, or
// editing one that has none yet, cannot leave the ledger green.
func tlaUnrecordedFiles(tlaDir string, runs map[string]tlaRun) ([]string, error) {
	covered := map[string]bool{}
	for config, row := range runs {
		covered[config] = true
		module := row.module
		if _, err := os.Stat(filepath.Join(tlaDir, "MC"+module+".tla")); err == nil {
			module = "MC" + module
		}
		files, err := tlaModuleFiles(tlaDir, module)
		if err != nil {
			return nil, err
		}
		for _, name := range files {
			covered[name] = true
		}
	}
	all, err := filepath.Glob(filepath.Join(tlaDir, "*.tla"))
	if err != nil {
		return nil, err
	}
	configs, err := filepath.Glob(filepath.Join(tlaDir, "*.cfg"))
	if err != nil {
		return nil, err
	}
	all = append(all, configs...)
	var extra []string
	for _, path := range all {
		name := filepath.Base(path)
		if !covered[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return extra, nil
}

// tlaModelHash is the SHA-256 of the config, the module it runs, every local
// module that one EXTENDS, and the files no row records. A standard module has
// no file under tla/ and reads no tree bytes, so it is not an input.
func tlaModelHash(tlaDir, module, config string, extra []string) (string, error) {
	if _, err := os.Stat(filepath.Join(tlaDir, "MC"+module+".tla")); err == nil {
		module = "MC" + module
	}
	files, err := tlaModuleFiles(tlaDir, module)
	if err != nil {
		return "", err
	}
	files = append(files, config)
	files = append(files, extra...)
	var lines []string
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(tlaDir, name))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, name+" "+hex.EncodeToString(sum[:]))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n") + "\n"))
	return hex.EncodeToString(sum[:]), nil
}

func tlaModuleFiles(tlaDir, module string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	var walk func(string) error
	walk = func(name string) error {
		if seen[name] {
			return nil
		}
		seen[name] = true
		data, err := os.ReadFile(filepath.Join(tlaDir, name+".tla"))
		if err != nil {
			return err
		}
		files = append(files, name+".tla")
		for _, dep := range tlaExtends(string(data)) {
			if _, err := os.Stat(filepath.Join(tlaDir, dep+".tla")); err == nil {
				if err := walk(dep); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(module); err != nil {
		return nil, err
	}
	return files, nil
}

func tlaExtends(src string) []string {
	var out []string
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "\\*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "EXTENDS":
			for _, name := range fields[1:] {
				out = append(out, strings.Trim(name, ","))
			}
		case "INSTANCE":
			out = append(out, strings.Trim(fields[1], ","))
		}
	}
	return out
}

func copyTLAFiles(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
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
	for raw := range strings.SplitSeq(string(data), "\n") {
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
	model     string
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
	for _, name := range []string{"config", "module", "outcome", "invariant", "model_sha256"} {
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
			model:     cells[column["model_sha256"]],
		}
	}
	return runs, nil
}
