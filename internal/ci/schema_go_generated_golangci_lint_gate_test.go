package ci

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The generated-Go golangci-lint gate (schema-per-language-lenses). These tests
// pin the gate's shape: its refusal to auto-annotate, the generated-exclusion
// switch that makes the REAL corpus visible, and the ratchet that carries the
// measured residue. A reader cannot silently widen the lens into a blanket
// nolint/nosec disabler, nor turn the ratchet back into a vacuous "zero".
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
		"schema-go-generated-golangci-lint-gate-negative-control-ratchet:",
		"schema-go-generated-golangci-lint-gate-probe:",
		"tool missing: golangci-lint",
		"RATCHET FAILED",
		"make/checks/schema-go-generated-golangci-lint-gate.ratchet.tsv",
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
	// EDIT." headers). The config is written by the printf below; removing that
	// literal line makes this test fail.
	if !strings.Contains(text, "'    generated: disable'") {
		t.Errorf("gate config does not disable the generated exclusion")
	}
	// The lens reports every finding: no per-linter or per-same-issue cap.
	if !strings.Contains(text, "max-issues-per-linter: 0") {
		t.Errorf("gate config caps noise per linter")
	}
}

// The lens must not auto-annotate by text pattern. Every suppression travels
// only with a real, triaged finding. The generators must contain no helper
// that sprinkles //nolint or #nosec on a line because it matches a token or a
// call site.
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

// The ratchet pins the measured residue per corpus, per linter, and (for
// gosec) per G-rule. Zero is not the goal; a new linter or rule has pin zero
// and fails the gate, so the file must name every key the gate currently sees.
func TestSchemaGoGeneratedGolangciGateRatchetPins(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repoRoot(t), "make", "checks", "schema-go-generated-golangci-lint-gate.ratchet.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("read ratchet: %v", err)
	}
	defer f.Close()
	pins := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Errorf("ratchet line %q is not corpus/linter/rule/count", line)
			continue
		}
		n, err := strconv.Atoi(fields[3])
		if err != nil || n < 0 {
			t.Errorf("ratchet line %q has no non-negative count", line)
			continue
		}
		pins[fields[0]+"/"+fields[1]+"/"+fields[2]] = n
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan ratchet: %v", err)
	}
	for _, want := range []string{
		"example/errcheck/-",
		"example/gosec/G115",
		"ludicrous/errcheck/-",
		"ludicrous/gosec/G115",
		"tables/errcheck/-",
		"tables/gosec/G103",
		"tables/gosec/G115",
	} {
		if _, ok := pins[want]; !ok {
			t.Errorf("ratchet does not pin %s", want)
		}
	}
	// A pin of zero on a corpus that has findings would make the ratchet lie;
	// the measured packet corpora are not clean.
	if pins["example/errcheck/-"] == 0 && pins["example/gosec/G115"] == 0 {
		t.Errorf("ratchet pins nothing for the example corpus")
	}
}

// The workflow must run on the fleet only and probe the fleet-pinned checker.
func TestSchemaGoGeneratedGolangciGateWorkflowIsFleetOnly(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "schema-go-generated-golangci-lint-gate.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`runs-on: ["self-hosted", "Linux", "X64", "fleet", "schema"]`,
		"github.event.pull_request.head.repo.full_name == github.repository",
		"TOOL-MISSING: golangci-lint 2.12.2",
		"~/sdk/bin/golangci-lint",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	for _, forbidden := range []string{"ubuntu-latest", "setup-go", "go install golangci-lint", "golangci-lint@", "curl "} {
		if strings.Contains(text, forbidden) {
			t.Errorf("workflow still carries %q", forbidden)
		}
	}
}
