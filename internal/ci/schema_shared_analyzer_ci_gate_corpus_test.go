package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaSharedAnalyzerCIGateCorpus proves the aggregate analyzer gate over
// the full test-schema corpus (schema-per-language-lenses, this card). The gate
// discovers the per-port analyzer targets that exist in the make include tree,
// runs them over the corpus the Makefile's generate targets read, and fails
// when a port with an analyzer would be left out.
//
// The fixture copies the real make tree and stubs only the checker recipes, so
// the test proves discovery of the real tables-rust-clippy target without
// generating the corpus. The planted controls prove all three arms: a failing
// wired analyzer takes the aggregate down by name, an analyzer for an
// uncovered port is a coverage failure by name, and removing the plant leaves
// the aggregate green.
func TestSchemaSharedAnalyzerCIGateCorpus(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fixture := t.TempDir()

	copyTree := func(src, dst string) {
		t.Helper()
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".mk") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(fixture, "make"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyTree(filepath.Join(root, "make"), filepath.Join(fixture, "make"))
	copyTree(filepath.Join(root, "make", "checks"), filepath.Join(fixture, "make", "checks"))

	realRust, err := os.ReadFile(filepath.Join(root, "make", "rust.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(realRust), "tables-rust-clippy:") {
		t.Fatalf("the real make/rust.mk no longer defines tables-rust-clippy, so the aggregate has no rust lens to wire")
	}
	if !strings.Contains(string(realRust), "include make/checks/schema-shared-analyzer-ci-gate-corpus.mk") {
		t.Fatalf("the real make/rust.mk no longer includes the aggregate, so the gate is not wired")
	}
	// The fixture keeps the real include wiring but stubs the checker recipe,
	// so discovery sees the same target name without generating the corpus.
	rustStub := "include make/checks/schema-shared-analyzer-ci-gate-corpus.mk\n\n" +
		".PHONY: tables-rust-clippy\ntables-rust-clippy:\n\t@echo stub rust clippy over the corpus\n"
	if err := os.WriteFile(filepath.Join(fixture, "make", "rust.mk"), []byte(rustStub), 0o644); err != nil {
		t.Fatal(err)
	}

	plants := filepath.Join(fixture, "make", "zz-plants.mk")
	writePlants := func(body string) {
		t.Helper()
		if err := os.WriteFile(plants, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePlants("")
	if err := os.WriteFile(filepath.Join(fixture, "Makefile"),
		[]byte("include $(wildcard make/*.mk)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func() (string, error) {
		t.Helper()
		cmd := exec.Command("make", "--no-print-directory", "schema-shared-analyzer-ci-gate-corpus")
		cmd.Dir = fixture
		cmd.Env = append(os.Environ(), "SCHEMA_SHARED_ANALYZER_SKIP_TOOL_PROBE=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	greenOut, err := run()
	if err != nil {
		t.Fatalf("aggregate failed green without a plant: %v\n%s", err, greenOut)
	}
	if !strings.Contains(greenOut, "ANALYZER RUN: tables-rust-clippy") {
		t.Errorf("aggregate did not run the real rust clippy target over the corpus:\n%s", greenOut)
	}
	if strings.Contains(greenOut, "ANALYZER SKIP: (port rust)") {
		t.Errorf("aggregate reported rust skipped even though tables-rust-clippy exists:\n%s", greenOut)
	}
	for _, p := range []string{"c", "cpp", "cs", "dart", "elixir", "go", "java", "js"} {
		if !strings.Contains(greenOut, "ANALYZER SKIP: (port "+p+")") {
			t.Errorf("aggregate did not skip port %s loudly by name:\n%s", p, greenOut)
		}
	}
	if !strings.Contains(greenOut, "analyzer-summary:") {
		t.Errorf("aggregate did not print one analyzer summary:\n%s", greenOut)
	}

	writePlants("tables-go-vet:\n\t@echo plant; exit 1\n")
	redWired, err := run()
	if err == nil {
		t.Fatalf("aggregate stayed green with a failing analyzer target planted:\n%s", redWired)
	}
	if !strings.Contains(redWired, "ANALYZER GATE FAILED: tables-go-vet") {
		t.Errorf("red did not name the failing wired analyzer:\n%s", redWired)
	}

	writePlants("tables-zig-clippy:\n\t@echo plant\n")
	redCoverage, err := run()
	if err == nil {
		t.Fatalf("aggregate stayed green with an analyzer for an uncovered port planted:\n%s", redCoverage)
	}
	if !strings.Contains(redCoverage, "ANALYZER COVERAGE FAILED: tables-zig-clippy") {
		t.Errorf("coverage red did not name the left-out port:\n%s", redCoverage)
	}

	writePlants("")
	greenAgain, err := run()
	if err != nil {
		t.Fatalf("aggregate failed green after the plants were removed: %v\n%s", err, greenAgain)
	}
	if !strings.Contains(greenAgain, "ANALYZER RUN: tables-rust-clippy") {
		t.Errorf("aggregate did not run the rust target after the plants were removed:\n%s", greenAgain)
	}
}

// TestSchemaSharedAnalyzerCIGateCorpusToolMissing pins the "never fake a pass"
// rule: when the pinned checker is not the one the host provides, the aggregate
// must exit non-zero and report tool missing by name rather than analyze with a
// different checker. A fake cargo that answers an unpinned version takes the
// place of the real toolchain.
func TestSchemaSharedAnalyzerCIGateCorpusToolMissing(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, "make", "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"make", "make/checks"} {
		entries, err := os.ReadDir(filepath.Join(root, d))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".mk") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, d, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(fixture, d, e.Name()), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	rustStub := "include make/checks/schema-shared-analyzer-ci-gate-corpus.mk\n\n" +
		".PHONY: tables-rust-clippy\ntables-rust-clippy:\n\t@echo stub rust clippy over the corpus\n"
	if err := os.WriteFile(filepath.Join(fixture, "make", "rust.mk"), []byte(rustStub), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "Makefile"),
		[]byte("include $(wildcard make/*.mk)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(fixture, "fakebin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCargo := filepath.Join(bin, "cargo")
	if err := os.WriteFile(fakeCargo, []byte("#!/bin/sh\necho \"cargo 9.9.9\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("make", "--no-print-directory", "schema-shared-analyzer-ci-gate-corpus")
	cmd.Dir = fixture
	env := []string{}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") {
			env = append(env, "PATH="+bin+":"+strings.TrimPrefix(e, "PATH="))
		} else {
			env = append(env, e)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("aggregate passed with an unpinned fake cargo; it must report the tool missing:\n%s", out)
	}
	if !strings.Contains(string(out), "tool missing:") {
		t.Errorf("aggregate did not report the tool missing with an unpinned cargo:\n%s", out)
	}
}
