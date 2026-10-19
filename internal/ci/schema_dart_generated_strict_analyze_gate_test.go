package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaDartGeneratedStrictAnalyzeGateWiring holds the card's gate to the
// three things that make it a gate rather than a green line: the strict
// language modes and fatal-on-infos invocation, the planted-bad-pattern
// control, and the workflow that runs it. It is pure text over tracked files,
// so it rides `go test ./...` without the Dart SDK; the make target itself is
// what runs the analyzer. At BASE the make file, workflow and control do not
// exist, so this test is red until the card lands them.
func TestSchemaDartGeneratedStrictAnalyzeGateWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makePath := filepath.Join(root, "make", "schema-dart-generated-strict-analyze-gate.mk")
	makeBody, err := os.ReadFile(makePath)
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	mk := string(makeBody)
	for _, want := range []string{
		"strict-casts: true",
		"strict-inference: true",
		"strict-raw-types: true",
		"--fatal-infos",
		"--fatal-warnings",
		"DART_STRICT_SDK_PIN ?= 3.13.2",
		"tool missing: dart",
		".PHONY: schema-dart-generated-strict-analyze-gate",
		".PHONY: schema-dart-generated-strict-analyze-gate-tables",
		".PHONY: schema-dart-generated-strict-analyze-gate-all",
		".PHONY: schema-dart-generated-strict-analyze-gate-negative-control",
		".PHONY: schema-dart-generated-strict-analyze-nested-default-negative-control",
		"value != 0",
		"unrelated_type_equality_checks",
		"generated/dart/Types.dart",
		"strict_raw_type",
		"generated/dart/.stamp",
		"build/tables-generated-dart/.stamp",
		// The three lints the first attempt left out because they fired:
		// they are in the set now, and the table half is green under them
		// (the emitter fix and the narrow per-line ignores in the report).
		"- avoid_dynamic_calls",
		"- literal_only_boolean_expressions",
		"- unrelated_type_equality_checks",
		// The corpus is the Makefile's own generate targets, including the
		// packet-defaults unit, the unit-view corpus, and the table-only
		// scalars unit in full (its form-3 libraries and the Int128 support
		// the packet emitter now writes beside them).
		"build/packet-defaults/dart",
		"build/dart-view",
		"build/tables-generated-dart-scalars/scalars",
		"Int128",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make file does not carry %q", want)
		}
	}
	// No default exclusion and no skip: the whole corpus is analyzed. A prune
	// of a generated file or a `continue` over a VIEW_CORPUS unit is a corpus
	// gap, not a green gate.
	for _, forbidden := range []string{
		"SCHEMA_DART_STRICT_PRUNE_SCALARS",
		"rm -f \"$(1)/$(SCHEMA_DART_STRICT_SCALARS_DIR)",
		"SCHEMA_DART_STRICT_SCALARS_BROKEN",
		"if [ \"$$dir\" = scalars ]; then continue; fi",
	} {
		if strings.Contains(mk, forbidden) {
			t.Errorf("make file still excludes generated output: %q", forbidden)
		}
	}
	// make/dart.mk must include the gate by path; the include is what makes the
	// targets exist.
	dartMK, err := os.ReadFile(filepath.Join(root, "make", "dart.mk"))
	if err != nil {
		t.Fatalf("read make/dart.mk: %v", err)
	}
	if !strings.Contains(string(dartMK), "include make/schema-dart-generated-strict-analyze-gate.mk") {
		t.Error("make/dart.mk does not include the card's gate file")
	}

	// The control must be registered with the repository's negative-control
	// plan, and it must name the Dart version the gate pins.
	manifestBody, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatalf("read negative-controls.json: %v", err)
	}
	var manifest struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Dart    string   `json:"dart"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatalf("parse negative-controls.json: %v", err)
	}
	registered := false
	nestedRegistered := false
	for _, g := range manifest.Groups {
		for _, target := range g.Targets {
			switch target {
			case "schema-dart-generated-strict-analyze-gate-negative-control":
				registered = true
			case "schema-dart-generated-strict-analyze-nested-default-negative-control":
				nestedRegistered = true
			default:
				continue
			}
			if g.When != "nightly" {
				t.Errorf("control group %q runs %q, want nightly", g.Name, g.When)
			}
			if g.Dart != "3.13.2" {
				t.Errorf("control group %q names dart %q, want 3.13.2", g.Name, g.Dart)
			}
		}
	}
	if !registered {
		t.Error("the strict-analyze negative control is not registered in make/negative-controls.json")
	}
	if !nestedRegistered {
		t.Error("the strict-analyze nested-default (object != 0) control is not registered in make/negative-controls.json")
	}

	workflowBody, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-dart-generated-strict-analyze-gate.yml"))
	if err != nil {
		t.Fatalf("the card's workflow is missing: %v", err)
	}
	wf := string(workflowBody)
	for _, want := range []string{
		"schema-dart-generated-strict-analyze-gate DART=dart",
		"schema-dart-generated-strict-analyze-gate-negative-control DART=dart",
		"schema-dart-generated-strict-analyze-gate-all DART=dart",
		"workflow_dispatch:",
		"schedule:",
		"pull_request:",
		"push:",
		"dart-lang/setup-dart@6afc89df92d6eb3834022f73cd65adc8cdfcb92d",
		"sdk: 3.13.2",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow does not carry %q", want)
		}
	}
}
