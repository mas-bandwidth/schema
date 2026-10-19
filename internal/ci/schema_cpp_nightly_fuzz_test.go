package ci

// TestSchemaCppNightlyFuzzLanded holds the C++ nightly-fuzz card's structure:
// one Makefile target that passes on the corpus, a schema list drawn from the
// tables corpus (the tables/* directories, examples-wide and test/tables), a
// harness with a libFuzzer entry point per reader kind that checks typed
// refusals and reader coverage, a scheduled workflow whose nightly part is not
// per push, and a replay target with no paths filter on the fast lane.
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
	"CPP_FUZZ_CXX ?= clang++-18",
	"CPP_FUZZ_SCHEMA_LIST := test/cpp-fuzz/schemas.txt",
	"-fsanitize=fuzzer,address,undefined",
	"-fsanitize=fuzzer,memory",
	"schema-cpp-fast-fuzz-replay",
	"schema-cpp-nightly-fuzz-negative-control",
	"schema-cpp-fuzz-seeds",
	"stage_seeds",
	"gen_roots.sh",
	"tool missing:",
}

var cppFuzzWorkflowPatterns = []string{
	"schedule:",
	"workflow_dispatch:",
	"push:",
	"pull_request:",
	"target: schema-cpp-nightly-fuzz\n",
	"target: schema-cpp-nightly-fuzz-msan\n",
	"make schema-cpp-fast-fuzz-replay",
	"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
	"actions/setup-go@",
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

// cppFuzzCorpusProblems is the corpus half of the predicate: the schema set
// must name at least one test/tables schema and the examples-wide unit, so the
// corpus is the Makefile's own tree and not a second, narrower one.
func cppFuzzCorpusProblems(list string) []string {
	var hasTables, hasWide bool
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "test/tables/") {
			hasTables = true
		}
		if line == "examples-wide" {
			hasWide = true
		}
	}
	var missing []string
	if !hasTables {
		missing = append(missing, "test/tables/")
	}
	if !hasWide {
		missing = append(missing, "examples-wide")
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
	if strings.Contains(string(wf), "paths:") {
		t.Error("the fast replay lane has a paths filter; a reader change must replay on every push")
	}

	list, err := os.ReadFile(filepath.Join(root, "test", "cpp-fuzz", "schemas.txt"))
	if err != nil {
		t.Fatalf("read schema list: %v", err)
	}
	if missing := cppFuzzCorpusProblems(string(list)); len(missing) != 0 {
		t.Fatalf("the schema list omits: %v", missing)
	}
	seen := map[string]bool{}
	schemas := 0
	for _, line := range strings.Split(string(list), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		schemas++
		if seen[line] {
			t.Errorf("corpus schema %s is listed twice", line)
		}
		seen[line] = true
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
	for _, p := range []string{
		"LLVMFuzzerTestOneInput", "FUZZ_FIXED", "FUZZ_VAR", "FromJson", "Open(",
		"FUZZ_OPEN_ROOTS", "FUZZ_ROOT_COUNT", "report.malformed", "report.refused",
		"TableRefuseReason", "refused without naming a reason",
	} {
		if !strings.Contains(string(harness), p) {
			t.Errorf("harness does not drive or check %s", p)
		}
	}

	roots, err := os.ReadFile(filepath.Join(root, "test", "cpp-fuzz", "gen_roots.sh"))
	if err != nil {
		t.Fatalf("read gen_roots: %v", err)
	}
	for _, p := range []string{"no readable root", "FUZZ_ROOT_COUNT", "FUZZ_OPEN_ROOTS"} {
		if !strings.Contains(string(roots), p) {
			t.Errorf("gen_roots.sh does not refuse or count %s", p)
		}
	}

	seeds, err := os.ReadFile(filepath.Join(root, "test", "cpp-fuzz", "stage_seeds", "main.go"))
	if err != nil {
		t.Fatalf("read stage_seeds: %v", err)
	}
	for _, p := range []string{"go test fuzz v1", "strconv.Unquote", "testdata/conformance/tables"} {
		if !strings.Contains(string(seeds), p) {
			t.Errorf("stage_seeds does not decode %s", p)
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
	list, err := os.ReadFile(filepath.Join(root, "test", "cpp-fuzz", "schemas.txt"))
	if err != nil {
		t.Fatalf("read schema list: %v", err)
	}

	if missing := missingPatterns(string(mk), cppFuzzRequiredPatterns); len(missing) != 0 {
		t.Fatalf("control is not green before the plant: %v", missing)
	}
	if missing := missingPatterns(string(wf), cppFuzzWorkflowPatterns); len(missing) != 0 {
		t.Fatalf("workflow control is not green before the plant: %v", missing)
	}
	if missing := cppFuzzCorpusProblems(string(list)); len(missing) != 0 {
		t.Fatalf("corpus control is not green before the plant: %v", missing)
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

	plantedList := strings.ReplaceAll(string(list), "examples-wide", "examples-narrow")
	missing = cppFuzzCorpusProblems(plantedList)
	if len(missing) != 1 || missing[0] != "examples-wide" {
		t.Fatalf("planted missing wide unit was not named: %v", missing)
	}

	var noTables []string
	for _, line := range strings.Split(string(list), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "test/tables/") {
			noTables = append(noTables, line)
		}
	}
	missing = cppFuzzCorpusProblems(strings.Join(noTables, "\n"))
	if len(missing) != 1 || missing[0] != "test/tables/" {
		t.Fatalf("planted missing test/tables corpus was not named: %v", missing)
	}
}

// TestSchemaCppNightlyFuzzSeedDecoder pins the corpus decoder's operand
// grammar: both `[]byte("...")` and `string("...")` decode to the bytes the Go
// fuzzer fed its target, nested corpus directories are walked, and an unknown
// operand is an error rather than a silently missing seed.
func TestSchemaCppNightlyFuzzSeedDecoder(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	seeds, err := os.ReadFile(filepath.Join(root, "test", "cpp-fuzz", "stage_seeds", "main.go"))
	if err != nil {
		t.Fatalf("read stage_seeds: %v", err)
	}
	for _, p := range []string{
		`"[]byte("`,
		`"string("`,
		`WalkDir`,
		"unsupported fuzz operand",
		`"testdata/wire/tables"`,
		`"testdata/conformance/tables/json"`,
		`"testdata/conformance/tables/json-hostile"`,
		`"testdata/conformance/tables/cook-write"`,
		`"internal/fuzz/testdata"`,
		`"internal/tabletext/testdata"`,
		"stageBuiltInFAdd",
		`name.Name != "handSeeds" && name.Name != "tableHandSeeds"`,
		`id.Name != "seeds"`,
	} {
		if !strings.Contains(string(seeds), p) {
			t.Errorf("stage_seeds does not name %s", p)
		}
	}
}
