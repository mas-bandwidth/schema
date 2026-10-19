package ci

// TestSchemaCppGeneratedFramaClangReadersIsWired holds the Frama-C/Frama-Clang
// lens on the C++ port's generated readers to the shape its card requires: a
// make target in the card's own include, a negative control the nightly
// manifest runs, and a workflow that keeps the whole-corpus parse off the
// per-push lane. It reads text only: no compiler, clock, socket or tool.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaCppGeneratedFramaClangReadersIsWired(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	mk := filepath.Join(root, "make", "checks", "schema-cpp-generated-frama-clang-readers.mk")
	src, err := os.ReadFile(mk)
	if err != nil {
		t.Fatalf("the card's make include must exist: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		"schema-cpp-generated-frama-clang-readers:",
		"schema-cpp-generated-frama-clang-readers-tool:",
		"schema-cpp-generated-frama-clang-readers-negative-control:",
		"FRAMA_C_VERSION   ?= 29.0",
		"FRAMA_CLANG_VERSION ?= 0.0.17",
		"tool missing: frama-c",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("make/checks/schema-cpp-generated-frama-clang-readers.mk is missing %q", want)
		}
	}
	if strings.Contains(text, "\ntest: schema-cpp-generated-frama-clang-readers") {
		t.Error("the whole-corpus parse must not hang off `make test`: it is nightly, not per-push")
	}

	raw, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("make/negative-controls.json: %v", err)
	}
	registered := false
	for _, g := range manifest.Groups {
		for _, target := range g.Targets {
			if target == "schema-cpp-generated-frama-clang-readers-negative-control" {
				registered = true
				if g.When != "nightly" {
					t.Errorf("the frama-clang control is in group %q (%s); it is heavy and belongs in a nightly group", g.Name, g.When)
				}
			}
		}
	}
	if !registered {
		t.Error("the planted-bad-pattern control is not registered in make/negative-controls.json")
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-cpp-generated-frama-clang-readers.yml"))
	if err != nil {
		t.Fatalf("the card's workflow must exist: %v", err)
	}
	w := string(wf)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"push:",
		"make schema-cpp-generated-frama-clang-readers",
		"make schema-cpp-generated-frama-clang-readers-negative-control",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("the workflow is missing %q", want)
		}
	}
}
