package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The generated-Go golangci-lint gate (schema-per-language-lenses). These tests
// pin the gate's shape and its refusal to auto-annotate, so a future reader
// cannot silently widen the lens into a blanket nolint/nosec disabler.
func TestSchemaGoGeneratedGolangciGateTargetExists(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "schema-go-generated-golangci-lint-gate.mk"))
	if err != nil {
		t.Fatalf("read gate makefile: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"schema-go-generated-golangci-lint-gate:",
		"schema-go-generated-golangci-lint-gate-negative-control:",
		"schema-go-generated-golangci-lint-gate-probe:",
		"tool missing: golangci-lint",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("gate makefile missing %q", want)
		}
	}
}

func TestSchemaGoGeneratedGolangciGateConfigIsNarrow(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "schema-go-generated-golangci-lint-gate.mk"))
	if err != nil {
		t.Fatalf("read gate makefile: %v", err)
	}
	text := string(data)
	for _, linter := range []string{"errcheck", "gosec", "ineffassign", "govet"} {
		if !strings.Contains(text, "    - "+linter) {
			t.Errorf("gate config does not enable %s", linter)
		}
	}
	// The lens must not drop the generated trees: linters.exclusions.generated
	// must be disabled (the real corpus carries "Code generated ... DO NOT
	// EDIT." headers).
	if !strings.Contains(text, "generated: disable") {
		t.Errorf("gate config does not disable the generated exclusion")
	}
	// The lens reports every finding: no per-linter or per-same-issue cap.
	if !strings.Contains(text, "max-issues-per-linter: 0") {
		t.Errorf("gate config caps noise per linter")
	}
}

// The gate pins per-linter (and gosec per-rule) counts per corpus in a commit
// ratchet TSV. The pin file must exist with the documented column header, and
// the makefile must run the ratchet comparison on both the packet and the table
// (nightly) corpora.
func TestSchemaGoGeneratedGolangciGateRatchetPinned(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "make", "checks", "schema-go-generated-golangci-lint-gate.ratchet.tsv"))
	if err != nil {
		t.Fatalf("read ratchet: %v", err)
	}
	text := string(data)
	wantHeader := "corpus\tlinter\trule\tcount"
	if !strings.Contains(text, wantHeader) {
		t.Errorf("ratchet missing column header %q", wantHeader)
	}
	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-go-generated-golangci-lint-gate.mk"))
	if err != nil {
		t.Fatalf("read gate makefile: %v", err)
	}
	for _, want := range []string{
		"GOLANGCI_GATE_RATCHET",
		"ratchet-counts.tsv",
		"golangci_gate_ratchet_check",
		"jq",
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("gate makefile does not wire the ratchet (%q)", want)
		}
	}
}
func TestSchemaGoGeneratedGolangciGateHasNoAutoAnnotator(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, gen := range []string{
		filepath.Join(root, "internal", "codegen", "golang", "golang.go"),
		filepath.Join(root, "internal", "codegen", "golang", "functions.go"),
		filepath.Join(root, "internal", "codegen", "gotable", "gotable.go"),
	} {
		data, err := os.ReadFile(gen)
		if err != nil {
			t.Fatalf("read generator %s: %v", gen, err)
		}
		text := string(data)
		for _, forbidden := range []string{"golangciLintDirective", "golangciLines", "//nolint:errcheck,gosec", "//nolint:gosec"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("generator %s still auto-annotates (%q)", filepath.Base(gen), forbidden)
			}
		}
	}
}
