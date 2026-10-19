// This file is the wiring gate for the C++ conformance sanitizer lens card
// (schema-cpp-conformance-sanitizers-asan-ubsan-msan): the make fragment, the
// card's own workflow, and the negative-control registry have to name the same
// targets, or a green nightly job is watching something other than the gate
// the card promised. It is cheap (file reads) so it rides the fast lane; the
// sanitized rebuilds themselves are minutes and run on the schedule.
package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sanitizerMakeFile = "make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk"
	sanitizerWorkflow = ".github/workflows/schema-cpp-conformance-sanitizers-asan-ubsan-msan.yml"
	sanitizerGate     = "cpp-conformance-sanitizers-asan-ubsan"
	sanitizerControl  = "cpp-conformance-sanitizers-asan-ubsan-negative-control"
	sanitizerMSan     = "cpp-conformance-sanitizers-msan"
)

func TestCppSanitizerLensWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile, err := os.ReadFile(filepath.Join(root, sanitizerMakeFile))
	if err != nil {
		t.Fatalf("%s is the card's make fragment: %v", sanitizerMakeFile, err)
	}
	mk := string(makeFile)
	for _, want := range []string{
		"CPP_SAN_CLANG_PIN ?= 21.1.8",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
		sanitizerGate + ":",
		sanitizerControl + ":",
		sanitizerMSan + ":",
		"tool missing: msan-instrumented-libc++",
		"schema_sanitizer_plant",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not carry %q: the lens target, its pin, its planted pattern or its honest MSan refusal is missing", sanitizerMakeFile, want)
		}
	}

	workflow, err := os.ReadFile(filepath.Join(root, sanitizerWorkflow))
	if err != nil {
		t.Fatalf("%s is the card's own workflow: %v", sanitizerWorkflow, err)
	}
	wf := string(workflow)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"pull_request:",
		"push:",
		"make " + sanitizerGate,
		"make " + sanitizerControl,
		"make " + sanitizerMSan,
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("%s does not carry %q: the fast lane, the nightly lane or a gate command is missing", sanitizerWorkflow, want)
		}
	}
	if !strings.Contains(wf, "cron:") {
		t.Errorf("%s has a schedule with no cron line, so the nightly sanitizer lane never runs", sanitizerWorkflow)
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
			if target == sanitizerControl {
				registered = true
				if g.When != "nightly" {
					t.Errorf("%s is in group %q (%s); a sanitized rebuild belongs on the nightly tier", sanitizerControl, g.Name, g.When)
				}
			}
		}
	}
	if !registered {
		t.Errorf("%s is defined and not in make/negative-controls.json, so the negative-control registry would report it uncovered", sanitizerControl)
	}
}
