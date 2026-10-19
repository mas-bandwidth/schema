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
// calls every per-port analyzer target that exists on the shared base and skips,
// loudly and by name, those not yet landed; it fails only when a gate exists
// but was left out of the corpus run.
//
// The planted-stray-gate control: writing a gate target outside the port set
// into the make include tree must take the aggregate down and name the plant;
// removing it must leave the aggregate green. That is the gate's proof that it
// fires, and it is the fast part of the card's CI (the aggregate itself is
// nightly).
func TestSchemaSharedAnalyzerCIGateCorpus(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fixture := t.TempDir()
	mk := filepath.Join(fixture, "make")
	if err := os.Mkdir(mk, 0o755); err != nil {
		t.Fatal(err)
	}
	// A minimal Makefile: the aggregate and its negative control live in
	// make/checks/schema-shared-analyzer-ci-gate-corpus.mk (a non-port
	// fragment, so it is not swept up by the wide-scalar refusal gate that
	// enumerates make/*.mk as ports). The fixture includes the checks tree
	// the same way the real Makefile does.
	if err := os.WriteFile(filepath.Join(fixture, "Makefile"),
		[]byte("include $(wildcard make/*.mk)\ninclude $(wildcard make/checks/*.mk)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "make"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".mk") {
			data, err := os.ReadFile(filepath.Join(root, "make", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(mk, e.Name()), data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
	}
	checks := filepath.Join(fixture, "make", "checks")
	if err := os.MkdirAll(checks, 0o755); err != nil {
		t.Fatal(err)
	}
	checkEntries, err := os.ReadDir(filepath.Join(root, "make", "checks"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range checkEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mk") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "make", "checks", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(checks, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("make", append([]string{"--no-print-directory"}, args...)...)
		cmd.Dir = fixture
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	greenOut, err := run("schema-shared-analyzer-ci-gate-corpus")
	if err != nil {
		t.Fatalf("aggregate failed green without a plant: %v\n%s", err, greenOut)
	}
	for _, p := range []string{"c", "cpp", "cs", "dart", "elixir", "go", "java", "js", "rust"} {
		if !strings.Contains(greenOut, "ANALYZER SKIP: (port "+p+")") {
			t.Errorf("aggregate did not skip port %s loudly by name:\n%s", p, greenOut)
		}
	}
	if !strings.Contains(greenOut, "analyzer-summary:") {
		t.Errorf("aggregate did not print one analyzer summary:\n%s", greenOut)
	}

	stray := filepath.Join(mk, "zz-stray.mk")
	strayBody := "SCHEMA_SHARED_ANALYZER_STRAY_GATES += schema-shared-analyzer-gate-stray\n" +
		"schema-shared-analyzer-gate-stray:\n\t@echo stray plant\n"
	if err := os.WriteFile(stray, []byte(strayBody), 0o644); err != nil {
		t.Fatal(err)
	}
	redOut, err := run("schema-shared-analyzer-ci-gate-corpus")
	if err == nil {
		t.Fatalf("aggregate stayed green with a stray analyzer gate planted:\n%s", redOut)
	}
	if !strings.Contains(redOut, "schema-shared-analyzer-gate-stray") {
		t.Errorf("red did not name the plant:\n%s", redOut)
	}
	if !strings.Contains(redOut, "ANALYZER COVERAGE FAILED") {
		t.Errorf("red did not name the coverage check:\n%s", redOut)
	}

	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}
	greenAgain, err := run("schema-shared-analyzer-ci-gate-corpus")
	if err != nil {
		t.Fatalf("aggregate failed green after the plant was removed: %v\n%s", err, greenAgain)
	}
}
