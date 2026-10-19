package ci

// TestSchemaDartGeneratedStrictAnalyzeGate pins the gate that keeps
// unrelated_type_equality_checks on over the generated Dart tables tree
// (docs/SPEC-TABLES.md §3.3): the make fragment names the target, its planted
// control appends an `x != 0` on a non-int to a copy of a generated library and
// requires the analyzer to go red on the lint's own name, the generated tree's
// stamp writes the lint into its analysis options, and the control is
// registered in make/negative-controls.json. It reads files only; the heavy
// Dart run is the make target itself.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaDartGeneratedStrictAnalyzeGate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	gate, err := os.ReadFile(filepath.Join(root, "make", "schema-dart-generated-strict-analyze-gate.mk"))
	if err != nil {
		t.Fatalf("the strict analyze gate's make fragment is missing: %v", err)
	}
	text := string(gate)
	for _, want := range []string{
		"schema-dart-generated-strict-analyze-gate:",
		"schema-dart-generated-strict-analyze-negative-control:",
		"unrelated_type_equality_checks",
		"$(DART) analyze",
		"value != 0",
		"NEGATIVE CONTROL FAILED",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("make/schema-dart-generated-strict-analyze-gate.mk does not carry %q", want)
		}
	}

	// The lint is in the analysis options the tree's stamp writes, so every
	// analyzer run over the tree — the gate and tables-dart-clean — shares it.
	dartMake, err := os.ReadFile(filepath.Join(root, "make", "dart.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dartMake), "- unrelated_type_equality_checks") {
		t.Error("make/dart.mk's build/tables-generated-dart stamp does not write unrelated_type_equality_checks into the tree's analysis options")
	}

	manifest, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Groups []struct {
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range m.Groups {
		for _, target := range g.Targets {
			if target == "schema-dart-generated-strict-analyze-negative-control" {
				found = true
			}
		}
	}
	if !found {
		t.Error("schema-dart-generated-strict-analyze-negative-control is not in make/negative-controls.json")
	}
}
