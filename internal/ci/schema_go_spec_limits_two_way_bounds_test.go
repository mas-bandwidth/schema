package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaGoSpecLimitsWorkflowKeepsRuntimeGateNightly(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-go-spec-limits-two-way-bounds.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, want := range []string{
		"pull_request:",
		"push:",
		"schedule:",
		"workflow_dispatch:",
		"timeout-minutes: 1",
		"make schema-go-spec-limits-two-way-bounds-fast",
		"make schema-go-spec-limits-two-way-bounds",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("spec-limit workflow is missing %q", want)
		}
	}
	nightly := strings.Index(text, "if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'")
	runtime := strings.Index(text, "- run: make schema-go-spec-limits-two-way-bounds\n")
	if nightly < 0 || runtime < 0 || nightly > runtime {
		t.Fatal("runtime limit gate must remain on nightly and explicit dispatch, outside the one-minute fast lane")
	}
}

func TestSchemaGoSpecLimitsGateUsesNestedModule(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	makefile, err := os.ReadFile(filepath.Join(root, "make", "schema-go-spec-limits-two-way-bounds.mk"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(makefile)
	if !strings.Contains(text, "cd test/go-tables &&") {
		t.Fatal("table module gate must change into test/go-tables before running go test")
	}
	if !strings.Contains(text, "sh test/go-tables/spec-limit-negative-control") {
		t.Fatal("nightly gate must prove the nested module runs a planted failing test")
	}
}
