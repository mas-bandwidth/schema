package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLuauGeneratedAnalyzeStrictGateIsWired pins the shape of the Luau
// strict-analyze lens: the card's own make fragment defines the gate and its
// planted-pattern control, the control is in the negative-control plan, the
// card's workflow runs the gate per push and the control on its schedule, and
// the only suppression is the two lints the generated scaffolding earns.
func TestLuauGeneratedAnalyzeStrictGateIsWired(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-luau-generated-analyze-strict-gate.mk"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(mk)
	for _, want := range []string{
		"luau-generated-analyze-strict:",
		"luau-generated-analyze-strict-negative-control:",
		"luau-analyze --mode=strict",
		"tool missing: luau-analyze",
	} {
		if !strings.Contains(makefile, want) {
			t.Errorf("make/schema-luau-generated-analyze-strict-gate.mk does not name %q", want)
		}
	}
	for _, lint := range []string{`"LocalUnused":false`, `"FunctionUnused":false`} {
		if !strings.Contains(makefile, lint) {
			t.Errorf("the gate does not suppress %s explicitly", lint)
		}
	}

	manifest, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Groups []struct {
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(manifest, &plan); err != nil {
		t.Fatalf("make/negative-controls.json: %v", err)
	}
	found := false
	for _, g := range plan.Groups {
		for _, target := range g.Targets {
			if target == "luau-generated-analyze-strict-negative-control" {
				found = true
			}
		}
	}
	if !found {
		t.Error("the planted-pattern control is not in make/negative-controls.json, so no leg runs it")
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-luau-generated-analyze-strict-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(wf)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"pull_request:",
		"push:",
		"make luau-generated-analyze-strict",
		"make luau-generated-analyze-strict-negative-control",
		"make dist-luau",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("the card's workflow does not name %q", want)
		}
	}
}
