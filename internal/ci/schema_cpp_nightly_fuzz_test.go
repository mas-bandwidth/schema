package ci

// TestSchemaCppNightlyFuzzLanded holds the C++ nightly-fuzz card's structure:
// one Makefile target that passes on the corpus, a schema list drawn from the
// tables corpus, a harness with a libFuzzer entry point per reader kind, a
// scheduled workflow whose nightly part is not per push, and a replay target.
//
// The planted-pattern half is the card's proof the check fires: the same
// predicate is run over an in-memory copy of each file with the load-bearing
// line removed, and must name exactly what was planted missing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var cppFuzzRequiredPatterns = []string{
	".PHONY: schema-cpp-nightly-fuzz",
	"CPP_FUZZ_SECONDS ?= 600",
	"CPP_FUZZ_SCHEMA_LIST := test/cpp-fuzz/schemas.txt",
	"-fsanitize=fuzzer,address,undefined",
	"-fsanitize=fuzzer,memory",
	"schema-cpp-fast-fuzz-replay",
	"schema-cpp-nightly-fuzz-negative-control",
	"tool missing:",
}

var cppFuzzWorkflowPatterns = []string{
	"schedule:",
	"workflow_dispatch:",
	"pull_request:",
	"target: schema-cpp-nightly-fuzz\n",
	"target: schema-cpp-nightly-fuzz-msan\n",
	"make schema-cpp-fast-fuzz-replay",
	"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
}

// missingPatterns is the whole predicate. It is a pure function over the text
// so the planted-pattern test can drive it without touching the tree.
func missingPatterns(text string, patterns []string) []string {
	var missing []string
	for _, p := range patterns {
		if !strings.Contains(text, p) {
			missing = append(missing, p)
		}
	}
	return missing
}

func TestSchemaCppNightlyFuzzLanded(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-cpp-nightly-fuzz.mk"))
	if err != nil {
		t.Fatalf("read make file: %v", err)
	}
	if missing := missingPatterns(string(mk), cppFuzzRequiredPatterns); len(missing) != 0 {
		t.Fatalf("make/schema-cpp-nightly-fuzz.mk is missing: %v", missing)
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-cpp-nightly-fuzz.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	if missing := missingPatterns(string(wf), cppFuzzWorkflowPatterns); len(missing) != 0 {
		t.Fatalf("the nightly-fuzz workflow is missing: %v", missing)
	}

	list, err := os.ReadFile(filepath.Join(root, "test", "cpp-fuzz", "schemas.txt"))
	if err != nil {
		t.Fatalf("read schema list: %v", err)
	}
	schemas := 0
	for _, line := range strings.Split(string(list), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		schemas++
		if _, err := os.Stat(filepath.Join(root, line)); err != nil {
			t.Errorf("corpus schema %s: %v", line, err)
		}
	}
	if schemas == 0 {
		t.Fatal("test/cpp-fuzz/schemas.txt lists no schema")
	}

	harness, err := os.ReadFile(filepath.Join(root, "test", "cpp-fuzz", "nightly_fuzz_main.cpp"))
	if err != nil {
		t.Fatalf("read harness: %v", err)
	}
	for _, p := range []string{"LLVMFuzzerTestOneInput", "FUZZ_FIXED", "FUZZ_VAR", "FromJson", "Open("} {
		if !strings.Contains(string(harness), p) {
			t.Errorf("harness does not drive %s", p)
		}
	}
}

// TestSchemaCppNightlyFuzzPlantedPattern is red when the plant is in and green
// when it is out. Each control removes one load-bearing line from an in-memory
// copy and requires the predicate to name that exact line.
func TestSchemaCppNightlyFuzzPlantedPattern(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-cpp-nightly-fuzz.mk"))
	if err != nil {
		t.Fatalf("read make file: %v", err)
	}
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-cpp-nightly-fuzz.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}

	if missing := missingPatterns(string(mk), cppFuzzRequiredPatterns); len(missing) != 0 {
		t.Fatalf("control is not green before the plant: %v", missing)
	}
	if missing := missingPatterns(string(wf), cppFuzzWorkflowPatterns); len(missing) != 0 {
		t.Fatalf("workflow control is not green before the plant: %v", missing)
	}

	plantedMake := strings.Replace(string(mk), "CPP_FUZZ_SECONDS ?= 600", "CPP_FUZZ_SECONDS ?= 0", 1)
	missing := missingPatterns(plantedMake, cppFuzzRequiredPatterns)
	if len(missing) != 1 || missing[0] != "CPP_FUZZ_SECONDS ?= 600" {
		t.Fatalf("planted missing target was not named: %v", missing)
	}

	plantedWorkflow := strings.Replace(string(wf), "schedule:", "schedule-removed:", 1)
	missing = missingPatterns(plantedWorkflow, cppFuzzWorkflowPatterns)
	if len(missing) != 1 || missing[0] != "schedule:" {
		t.Fatalf("planted missing schedule was not named: %v", missing)
	}
}
