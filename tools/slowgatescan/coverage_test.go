package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Adversarial Control 1: Disabled or unrelated CI steps do not credit required tests.
// A workflow with SCHEMA_SLOW=0 or go test ./unrelated must NOT cover compiler.TestRequiredGate.
func TestAdversarialDisabledOrUnrelatedCIStep(t *testing.T) {
	// A synthetic transcript with one required test in ./compiler
	transcript := strings.Join([]string{
		`{"Action":"output","Package":"github.com/mas-bandwidth/schema/v2/compiler","Test":"TestRequiredGate","Output":"` + gateSkipMarker + `\n"}`,
	}, "\n")

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "plain.log")
	if err := os.WriteFile(logPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("writing test log: %v", err)
	}

	// Case 1A: Disabled CI step (SCHEMA_SLOW: '0') running the matching package
	workflowDisabled := `
name: test
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - name: disabled
        env:
          SCHEMA_SLOW: '0'
        run: go test ./compiler
`
	steps, err := parseWorkflowContent("test.yml", workflowDisabled)
	if err != nil {
		t.Fatalf("parseWorkflowContent: %v", err)
	}
	rc := runCoverage(tmpDir, logPath, nil, nil, steps)
	if rc == 0 {
		t.Errorf("expected runCoverage to fail when CI step has SCHEMA_SLOW: '0', but got exit 0")
	}

	// Case 1B: Enabled CI step running an unrelated package (./unrelated)
	workflowUnrelated := `
name: test
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - name: unrelated
        env:
          SCHEMA_SLOW: '1'
        run: go test ./unrelated -run '^TestOther$'
`
	steps, err = parseWorkflowContent("test.yml", workflowUnrelated)
	if err != nil {
		t.Fatalf("parseWorkflowContent: %v", err)
	}
	rc = runCoverage(tmpDir, logPath, nil, nil, steps)
	if rc == 0 {
		t.Errorf("expected runCoverage to fail when CI step only runs ./unrelated, but got exit 0")
	}
}

// Adversarial Control 2: Same test name in different packages.
// Verifies that package identity prevents cross-package false credit.
func TestAdversarialSameTestNameInDifferentPackages(t *testing.T) {
	// Two tests with the same name "TestRequiredGate": one in ./compiler, one in ./internal/codegen/gotable
	transcript := strings.Join([]string{
		`{"Action":"output","Package":"github.com/mas-bandwidth/schema/v2/compiler","Test":"TestRequiredGate","Output":"` + gateSkipMarker + `\n"}`,
		`{"Action":"output","Package":"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable","Test":"TestRequiredGate","Output":"` + gateSkipMarker + `\n"}`,
	}, "\n")

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "plain.log")
	if err := os.WriteFile(logPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("writing test log: %v", err)
	}

	// Workflow that ONLY runs codegen packages
	workflowCodegenOnly := `
name: test
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - name: codegen only
        env:
          SCHEMA_SLOW: '1'
        run: go test ./internal/codegen/...
`
	steps, err := parseWorkflowContent("test.yml", workflowCodegenOnly)
	if err != nil {
		t.Fatalf("parseWorkflowContent: %v", err)
	}

	// Should fail because compiler.TestRequiredGate is uncovered
	rc := runCoverage(tmpDir, logPath, nil, nil, steps)
	if rc == 0 {
		t.Errorf("expected runCoverage to fail when only codegen is tested, but got exit 0")
	}

	// Add step covering non-codegen packages
	workflowBoth := `
name: test
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - name: codegen
        env:
          SCHEMA_SLOW: '1'
        run: go test ./internal/codegen/...
      - name: rest
        env:
          SCHEMA_SLOW: '1'
        run: go test $(go list ./... | grep -v '/internal/codegen/')
`
	steps, err = parseWorkflowContent("test.yml", workflowBoth)
	if err != nil {
		t.Fatalf("parseWorkflowContent: %v", err)
	}

	rc = runCoverage(tmpDir, logPath, nil, nil, steps)
	if rc != 0 {
		t.Errorf("expected runCoverage to pass when both packages are covered, got exit %d", rc)
	}

	// Verify coverage.tsv has distinct rows for each package
	tsvPath := filepath.Join(tmpDir, "build", "slowgate", "coverage.tsv")
	b, err := os.ReadFile(tsvPath)
	if err != nil {
		t.Fatalf("reading coverage.tsv: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "ci\tcompiler.TestRequiredGate\ttest.yml:") {
		t.Errorf("expected compiler.TestRequiredGate in ci bucket, got:\n%s", content)
	}
	if !strings.Contains(content, "ci\tinternal/codegen/gotable.TestRequiredGate\ttest.yml:") {
		t.Errorf("expected internal/codegen/gotable.TestRequiredGate in ci bucket, got:\n%s", content)
	}
}

// Adversarial Control 3: Restricted -run pattern does not credit non-matching tests.
func TestAdversarialRestrictedRunPattern(t *testing.T) {
	transcript := strings.Join([]string{
		`{"Action":"output","Package":"github.com/mas-bandwidth/schema/v2/compiler","Test":"TestRequiredGate","Output":"` + gateSkipMarker + `\n"}`,
	}, "\n")

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "plain.log")
	if err := os.WriteFile(logPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("writing test log: %v", err)
	}

	// Step specifies -run '^TestOtherOnly$'
	workflowRestricted := `
name: test
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - name: restricted
        env:
          SCHEMA_SLOW: '1'
        run: go test ./compiler -run '^TestOtherOnly$'
`
	steps, err := parseWorkflowContent("test.yml", workflowRestricted)
	if err != nil {
		t.Fatalf("parseWorkflowContent: %v", err)
	}

	rc := runCoverage(tmpDir, logPath, nil, nil, steps)
	if rc == 0 {
		t.Errorf("expected runCoverage to fail when -run does not match test, but got exit 0")
	}
}

// Adversarial Control 4: An actually selecting lane credits the test.
func TestAdversarialActuallySelectingLane(t *testing.T) {
	transcript := strings.Join([]string{
		`{"Action":"output","Package":"github.com/mas-bandwidth/schema/v2/compiler","Test":"TestRequiredGate","Output":"` + gateSkipMarker + `\n"}`,
	}, "\n")

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "plain.log")
	if err := os.WriteFile(logPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("writing test log: %v", err)
	}

	// Step genuinely runs ./compiler with SCHEMA_SLOW=1
	workflowSelecting := `
name: test
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - name: selecting lane
        env:
          SCHEMA_SLOW: '1'
        run: go test ./compiler
`
	steps, err := parseWorkflowContent("test.yml", workflowSelecting)
	if err != nil {
		t.Fatalf("parseWorkflowContent: %v", err)
	}

	rc := runCoverage(tmpDir, logPath, nil, nil, steps)
	if rc != 0 {
		t.Errorf("expected runCoverage to succeed on selecting lane, got exit %d", rc)
	}

	tsvPath := filepath.Join(tmpDir, "build", "slowgate", "coverage.tsv")
	b, err := os.ReadFile(tsvPath)
	if err != nil {
		t.Fatalf("reading coverage.tsv: %v", err)
	}
	if !strings.Contains(string(b), "ci\tcompiler.TestRequiredGate\ttest.yml:") {
		t.Errorf("expected test in ci bucket, got:\n%s", string(b))
	}
}

// TestPlainLogFallback ensures plain text logs (with === RUN and ok/FAIL) are also parsed.
func TestPlainLogFallback(t *testing.T) {
	plainText := strings.Join([]string{
		`=== RUN   compiler.TestRequiredGate`,
		`    gate_test.go:10: ` + gateSkipMarker,
		`--- SKIP: compiler.TestRequiredGate (0.00s)`,
	}, "\n")

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "plain.log")
	if err := os.WriteFile(logPath, []byte(plainText), 0o644); err != nil {
		t.Fatalf("writing test log: %v", err)
	}

	workflowSelecting := `
name: test
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - name: selecting lane
        env:
          SCHEMA_SLOW: '1'
        run: go test ./compiler
`
	steps, err := parseWorkflowContent("test.yml", workflowSelecting)
	if err != nil {
		t.Fatalf("parseWorkflowContent: %v", err)
	}

	rc := runCoverage(tmpDir, logPath, nil, nil, steps)
	if rc != 0 {
		t.Errorf("expected plain text fallback to succeed, got exit %d", rc)
	}
}
