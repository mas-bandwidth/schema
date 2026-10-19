package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCsConformanceRunsUnderCheckedArithmetic(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mkPath := filepath.Join(root, "make", "schema-cs-conformance-checked-overflow.mk")
	mkBytes, err := os.ReadFile(mkPath)
	if err != nil {
		t.Fatalf("read checked-overflow make fragment: %v", err)
	}
	mk := string(mkBytes)
	for _, want := range []string{
		"tables-cs-checked-overflow-conformance:",
		"-p:CheckForOverflowUnderflow=true",
		"build/tables-generated-cs/.stamp",
		"schemaconformance.dll",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not contain %q", mkPath, want)
		}
	}

	// The red half uses the same checked build against a scratch copy of the
	// generated corpus and requires the planted addition to raise overflow.
	for _, want := range []string{
		"tables-cs-checked-overflow-negative-control:",
		"CheckedOverflowPlant.cs",
		"OverflowException",
		"TablesGeneratedDir=",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not pin negative-control token %q", mkPath, want)
		}
	}

	workflowPath := filepath.Join(root, ".github", "workflows", "schema-cs-conformance-checked-overflow.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read checked-overflow workflow: %v", err)
	}
	text := string(workflow)
	for _, target := range []string{"tables-cs-checked-overflow-conformance", "tables-cs-checked-overflow-negative-control"} {
		if !strings.Contains(text, "make "+target) {
			t.Errorf("checked-overflow workflow does not invoke %s", target)
		}
	}
	if strings.Contains(text, "pull_request:") || strings.Contains(text, "push:") {
		t.Error("checked-overflow dotnet gate is heavy and must remain nightly/manual")
	}
}
