package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGeneratedRustKaniReadersWiring holds the Kani lens on the generated Rust
// readers to the three places that make it a gate rather than a wish: the make
// file that probes, generates and runs it over the tables corpus; the workflow
// that runs it on the nightly tier only; and the negative-control manifest
// that names its planted-pattern control. Kani itself is not run here — it is
// a heavy analyzer and belongs behind the slow gate — so this test pins the
// wiring and the plant that the nightly job turns red.
func TestGeneratedRustKaniReadersWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mkPath := filepath.Join(root, "make", "schema-rust-generated-kani-readers.mk")
	mk, err := os.ReadFile(mkPath)
	if err != nil {
		t.Fatalf("read %s: %v", mkPath, err)
	}
	makeText := string(mk)

	for _, want := range []struct{ needle, why string }{
		{"tables-rust-generated-kani-readers:", "the target itself"},
		{"build/tables-generated-rust/.stamp", "the corpus make/rust.mk builds, not a second corpus"},
		{"tool missing: kani", "an absent checker refuses by name instead of passing"},
		{"KANI_VERSION  ?=", "the pinned checker version"},
		{"KANI_READERS_BOUND", "the stated input bound"},
		{"planted_out_of_bounds", "the plant the negative control requires Kani to catch"},
		{"KANI_READERS_PLANT=1", "the test-only switch that plants it"},
		{"pub fn [a-z0-9_]+_load\\(wire: &\\[u8\\]", "the generated reader signatures the harnesses wrap"},
		{"load_measure", "the measure reader beside the load reader"},
	} {
		if !strings.Contains(makeText, want.needle) {
			t.Errorf("make/schema-rust-generated-kani-readers.mk lacks %q (%s)", want.needle, want.why)
		}
	}

	control := "tables-rust-generated-kani-readers-negative-control:"
	if !strings.Contains(makeText, control) {
		t.Errorf("make file defines no %s target, so the plant has no red line", control)
	}

	// The workflow is the nightly home. A push or pull_request trigger would
	// put a nightly analyzer on the per-commit budget the card forbids.
	wfPath := filepath.Join(root, ".github", "workflows", "schema-rust-generated-kani-readers.yml")
	wf, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("read %s: %v", wfPath, err)
	}
	wfText := string(wf)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"make tables-rust-generated-kani-readers",
		"make tables-rust-generated-kani-readers-negative-control",
		"kani-verifier",
	} {
		if !strings.Contains(wfText, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	for _, forbidden := range []string{"pull_request:", "  push:"} {
		if strings.Contains(wfText, forbidden) {
			t.Errorf("workflow carries %q: Kani is heavy and must stay off the per-push lane", forbidden)
		}
	}

	// One version pin, read by both files.
	mkVersion := regexp.MustCompile(`(?m)^KANI_VERSION\s+\?=\s*(\S+)\s*$`).FindStringSubmatch(makeText)
	wfVersion := regexp.MustCompile(`(?m)^\s+KANI_VERSION:\s*(\S+)\s*$`).FindStringSubmatch(wfText)
	if mkVersion == nil || wfVersion == nil {
		t.Fatalf("Kani version pin missing (make %v, workflow %v)", mkVersion, wfVersion)
	}
	if mkVersion[1] != wfVersion[1] {
		t.Errorf("Kani version pins disagree: make %q, workflow %q", mkVersion[1], wfVersion[1])
	}

	// The plant control must run somewhere: the manifest's nightly tier.
	manifestPath := filepath.Join(root, "make", "negative-controls.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	var manifest struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse make/negative-controls.json: %v", err)
	}
	found := false
	for _, g := range manifest.Groups {
		if g.When != "nightly" {
			continue
		}
		for _, target := range g.Targets {
			if target == strings.TrimSuffix(control, ":") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("make/negative-controls.json does not run %s on the nightly tier", strings.TrimSuffix(control, ":"))
	}
}
