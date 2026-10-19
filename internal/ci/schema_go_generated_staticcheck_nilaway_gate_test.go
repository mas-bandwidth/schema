package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaGoGeneratedStaticcheckNilawayGate pins the mechanism of the card's
// Go lens (schema-per-language-lenses): the make fragment exists in the tree
// the Makefile's wildcard reaches, both checkers are pinned to the fleet-wide
// versions, the target probes for an installed tool rather than assuming one,
// and the planted-pattern negative control is registered in the control
// registry. The heavy lens itself is nightly; this is the fast proof that its
// blades are in place.
func TestSchemaGoGeneratedStaticcheckNilawayGate(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	mkPath := filepath.Join(root, "make", "schema-go-generated-staticcheck-nilaway-gate.mk")
	mk, err := os.ReadFile(mkPath)
	if err != nil {
		t.Fatalf("the card's make fragment is absent: %v", err)
	}
	text := string(mk)
	for _, want := range []string{
		"STATICCHECK_VERSION ?= v0.8.2",
		"NILAWAY_VERSION     ?= v0.0.0-20260918162853-acb8859b9031",
		"staticcheck_version = $(shell staticcheck -version 2>&1)",
		"nilaway_version     = $(shell nilaway -V=full 2>&1)",
		"schema-go-generated-staticcheck-nilaway-gate:",
		"schema-go-generated-staticcheck-nilaway-gate-negative-control:",
		"tool missing: staticcheck",
		"tool missing: nilaway",
		"PlantedAddr",
		"PlantedNil",
		"grep -q 'SA4001'",
		"Potential nil panic",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("make fragment does not carry %q", want)
		}
	}

	wfPath := filepath.Join(root, ".github", "workflows", "schema-go-generated-staticcheck-nilaway-gate.yml")
	wf, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("the card's workflow is absent: %v", err)
	}
	wfText := string(wf)
	for _, want := range []string{"schedule:", "workflow_dispatch:", "make --no-print-directory schema-go-generated-staticcheck-nilaway-gate", "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'", "runs-on: [self-hosted, Linux, X64, fleet, schema]"} {
		if !strings.Contains(wfText, want) {
			t.Errorf("workflow does not carry %q", want)
		}
	}
	if strings.Contains(wfText, "runs-on: ubuntu-latest") {
		t.Error("workflow still runs jobs on a non-fleet runner")
	}
	if strings.Contains(wfText, "go install") {
		t.Error("workflow installs checkers onto the runner instead of using the fleet-wide ones")
	}

	reg, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatalf("the control registry is unreadable: %v", err)
	}
	if !strings.Contains(string(reg), "schema-go-generated-staticcheck-nilaway-gate-negative-control") {
		t.Error("the planted-pattern control is not registered in make/negative-controls.json")
	}
}
