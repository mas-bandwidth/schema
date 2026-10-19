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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sanitizerMakeFile    = "make/schema-cpp-conformance-sanitizers-asan-ubsan-msan.mk"
	sanitizerWorkflow    = ".github/workflows/schema-cpp-conformance-sanitizers-asan-ubsan-msan.yml"
	sanitizerGate        = "cpp-conformance-sanitizers-asan-ubsan"
	sanitizerControl     = "cpp-conformance-sanitizers-asan-ubsan-negative-control"
	sanitizerMSan        = "cpp-conformance-sanitizers-msan"
	sanitizerAbsentCheck = "build/cpp-sanitizers/no-absent"
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
		sanitizerAbsentCheck,
		"build/cpp-sanitizers/msan-drivers.txt",
		"CPP_SAN_SOURCES = $(CONFORMANCE_SOURCES)",
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
	if strings.Contains(mk, "CPP_SAN_SOURCES :=") {
		t.Error("CPP_SAN_SOURCES is immediate, so its maps/lists wildcards expand before build/tables-generated exists and a clean-tree build links nothing")
	}
	if !strings.Contains(wf, "cron:") {
		t.Errorf("%s has a schedule with no cron line, so the nightly sanitizer lane never runs", sanitizerWorkflow)
	}
	if !strings.Contains(wf, "llvm.sh") || !strings.Contains(wf, "clang++-21") {
		t.Errorf("%s installs no clang 21.1.8, so the nightly ASan/MSan lanes run on whatever the runner image carries", sanitizerWorkflow)
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

// TestCppSanitizerAbsentIsNotAPass pins the half the harness cannot pin for a
// substituted registry: `harness run --drivers <file>` treats its first line as
// one leg of a port, not the reference, so it does not fail an ABSENT surface
// or case. This lens IS the reference leg's sanitized twin, so the make gate
// reads the matrix it printed and refuses either absence. The check is built by
// the card's own make target (cheap: it writes one shell script) and exercised
// on three logs: a complete matrix is green, an `absent` cell is red, and a
// `+Na` case count is red.
func TestCppSanitizerAbsentIsNotAPass(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	build := exec.Command("make", "--no-print-directory", sanitizerAbsentCheck)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", sanitizerAbsentCheck, err, out)
	}
	check := filepath.Join(root, filepath.FromSlash(sanitizerAbsentCheck))

	run := func(t *testing.T, log string) (string, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "matrix.log")
		if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(check, path).CombinedOutput()
		return string(out), err
	}

	if out, err := run(t, "TABLES CONFORMANCE\n\nwire          pass 111/111\n"); err != nil {
		t.Fatalf("a complete matrix was refused: %v\n%s", err, out)
	}

	out, err := run(t, "TABLES CONFORMANCE\n\nwire          absent\n")
	if err == nil {
		t.Fatalf("an ABSENT surface passed the gate: a skip is a pass again\n%s", out)
	}
	if !strings.Contains(out, "SURFACE") {
		t.Errorf("the absent-surface refusal does not name the grain:\n%s", out)
	}

	out, err = run(t, "TABLES CONFORMANCE\n\nwire          pass 111/112 +1a\n")
	if err == nil {
		t.Fatalf("an ABSENT case passed the gate: a skip is a pass again\n%s", out)
	}
	if !strings.Contains(out, "CASE") {
		t.Errorf("the absent-case refusal does not name the grain:\n%s", out)
	}
}
