package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaLobsterGeneratedStrictGateWiring holds the Lobster strict gate
// (docs/PORTING.md J4) to what makes it a gate rather than a green line: it
// compiles every generated unit and fails on ANY compiler output (Lobster
// prints its warnings and exits 0, so a gate on the status alone is green over
// all of them), its corpus is examples/, examples128/, examples-wide/ and the
// conformance manifest, its control plants a warning in a real generated module
// and requires red on that line alone, the control is registered with the
// negative-control plan at the leg's pinned toolchain, and a workflow runs both.
// Pure text over tracked files: the make target is what runs the compiler.
func TestSchemaLobsterGeneratedStrictGateWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	body, err := os.ReadFile(filepath.Join(root, "make", "schema-lobster-generated-strict-gate.mk"))
	if err != nil {
		t.Fatalf("the gate's make file is missing: %v", err)
	}
	mk := string(body)
	for _, want := range []string{
		".PHONY: schema-lobster-generated-strict-gate tables-lobster-clean",
		"tables-lobster-clean: schema-lobster-generated-strict-gate",
		".PHONY: schema-lobster-generated-strict-gate-negative-control",
		"toolchain-lobster",
		`--pak $(SCHEMA_LOBSTER_STRICT_MAIN)`,
		// the verdict is the output, not the status
		`[ "$$st" -eq 0 ] && [ ! -s "$$u.log" ]`,
		"SCHEMA_LOBSTER_STRICT_TREES  := examples examples128 examples-wide",
		"$(LOBSTER_MANIFEST)",
		"tool missing: lobster",
		// the control: a clean baseline, a plant in a real module, one line
		"the unplanted examples unit does not compile clean",
		"$$nc/examples/Types.lobster",
		"var schema_strict_planted = 1",
		`[ "$$lines" -ne 1 ]`,
		"NEGATIVE CONTROL FAILED: the strict gate stayed green",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/schema-lobster-generated-strict-gate.mk does not carry %q", want)
		}
	}
	for _, forbidden := range []string{"--silent", "grep -v warning", "|| true"} {
		if strings.Contains(mk, forbidden) {
			t.Errorf("make/schema-lobster-generated-strict-gate.mk filters the compiler's output: %q", forbidden)
		}
	}

	manifestBody, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Groups []struct {
			Name    string   `json:"name"`
			Lobster string   `json:"lobster"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}
	registered := false
	for _, g := range manifest.Groups {
		for _, target := range g.Targets {
			if target != "schema-lobster-generated-strict-gate-negative-control" {
				continue
			}
			registered = true
			if g.Lobster != "v2026.8" {
				t.Errorf("control group %q installs lobster %q, want the pin v2026.8", g.Name, g.Lobster)
			}
		}
	}
	if !registered {
		t.Error("the strict gate's negative control is not registered in make/negative-controls.json")
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-lobster-generated-strict-gate.yml"))
	if err != nil {
		t.Fatalf("the gate's workflow is missing: %v", err)
	}
	for _, want := range []string{
		"make dist-lobster",
		"make tables-lobster-clean",
		"make schema-lobster-generated-strict-gate-negative-control",
		"pull_request:",
		"schedule:",
	} {
		if !strings.Contains(string(wf), want) {
			t.Errorf("schema-lobster-generated-strict-gate.yml does not carry %q", want)
		}
	}
}
