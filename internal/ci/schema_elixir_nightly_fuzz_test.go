package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaElixirNightlyFuzzMakefileContract holds the nightly fuzz fragment
// to the shape the card promises: a pinned Elixir, a time budget variable whose
// default is ten minutes per port, a corpus written from the port's own
// ELIXIR_TABLE_UNITS (never a second list), the three reader kinds, the four
// named mutations, a saved-input replay arm, and a probe that says
// "tool missing: elixir" rather than pretending to pass.
func TestSchemaElixirNightlyFuzzMakefileContract(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	path := filepath.Join(root, "make", "schema-elixir-nightly-fuzz.mk")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nightly fuzz make fragment missing at %s: %v", path, err)
	}
	fragment := string(data)

	for _, want := range []string{
		"SCHEMA_ELIXIR_FUZZ_ELIXIR_VERSION := 1.20.4",
		"SCHEMA_ELIXIR_FUZZ_TIME            ?= 10m",
		"SCHEMA_ELIXIR_FUZZ_BUDGET",
		"SCHEMA_ELIXIR_FUZZ_SLACK_SECONDS",
		"SCHEMA_ELIXIR_FUZZ_STREAM_DATA_VERSION := 1.4.0",
		"SCHEMA_ELIXIR_FUZZ_STREAM_DATA_EBIN",
		"$(ELIXIR_TABLE_UNITS)",
		"schema-elixir-nightly-fuzz:",
		"schema-elixir-nightly-fuzz-corpus:",
		"schema-elixir-nightly-fuzz-replay:",
		"schema-elixir-nightly-fuzz-smoke:",
		"schema-elixir-nightly-fuzz-ci:",
		"schema-elixir-nightly-fuzz-negative-control:",
		"tool missing: elixir",
		"internal/fuzz/testdata",
		"testdata/conformance/tables",
	} {
		if !strings.Contains(fragment, want) {
			t.Errorf("nightly fuzz make fragment does not carry %q", want)
		}
	}

	if strings.Contains(fragment, "test-elixir:") {
		t.Errorf("the nightly target must not ride test-elixir: the ten-minute pass is scheduled, never per push")
	}

	// The loop must be DERIVED from the chosen budget, not pinned at 540s: a
	// workflow_dispatch time=1m would otherwise ask the harness for nine minutes
	// and die on its own timeout with exit 124, no finding.
	if strings.Contains(fragment, "SCHEMA_ELIXIR_FUZZ_SECONDS         ?=") {
		t.Errorf("the harness loop must be derived from SCHEMA_ELIXIR_FUZZ_TIME, not a fixed ?= default")
	}
	if !strings.Contains(fragment, "SCHEMA_ELIXIR_FUZZ_SECONDS         := $(shell sh $(SCHEMA_ELIXIR_FUZZ_BUDGET)") {
		t.Errorf("the harness loop is not derived from SCHEMA_ELIXIR_FUZZ_TIME through budget.sh")
	}
}

// TestSchemaElixirNightlyFuzzBudgetDerivation pins the override, not just the
// two defaults: the script the make fragment calls derives the loop from any
// budget syntax the dispatch input accepts, always with bounded slack.
func TestSchemaElixirNightlyFuzzBudgetDerivation(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	script := filepath.Join(root, "internal", "ci", "testdata", "schema-elixir-nightly-fuzz", "budget.sh")
	for _, tc := range []struct {
		budget string
		slack  string
		want   string
	}{
		{"10m", "5", "595"},
		{"1m", "5", "55"},
		{"90s", "5", "85"},
		{"600", "5", "595"},
		{"10s", "5", "5"},
	} {
		t.Run(tc.budget+"_"+tc.slack, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("sh", script, tc.budget, tc.slack)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("budget.sh %s %s: %v: %s", tc.budget, tc.slack, err, out)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Errorf("budget.sh %s %s = %s, want %s", tc.budget, tc.slack, got, tc.want)
			}
		})
	}
}

// TestSchemaElixirNightlyFuzzWorkflowContract holds the card's own workflow to
// the trigger split: a nightly schedule and workflow_dispatch for the timed
// pass, push and pull_request only for the fast arms. A scheduled job that ran
// on a push would spend the ten-minute budget per commit.
func TestSchemaElixirNightlyFuzzWorkflowContract(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	path := filepath.Join(root, ".github", "workflows", "schema-elixir-nightly-fuzz.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nightly fuzz workflow missing at %s: %v", path, err)
	}
	workflow := string(data)

	for _, want := range []string{
		"schedule:",
		"cron:",
		"workflow_dispatch:",
		"pull_request:",
		"push:",
		"make schema-elixir-nightly-fuzz",
		"make schema-elixir-nightly-fuzz-smoke",
		"runs-on: [self-hosted, linux, x64, schema]",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("nightly fuzz workflow does not carry %q", want)
		}
	}
}

// TestSchemaElixirNightlyFuzzPlantedBadPattern is the card's planted-pattern
// test. It reads the harness the make target copies (or the copy a negative
// control plants through SCHEMA_ELIXIR_FUZZ_HARNESS) and requires the three
// reader kinds, the four named mutations, the StreamData property runner and
// the :cover feedback path to be present. The negative-control target removes
// the JSON reader with sed and requires this test to go red on that exact
// absence; the real harness is green. No toolchain, no clock.
func TestSchemaElixirNightlyFuzzPlantedBadPattern(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	path := os.Getenv("SCHEMA_ELIXIR_FUZZ_HARNESS")
	if path == "" {
		path = filepath.Join(root, "internal", "ci", "testdata", "schema-elixir-nightly-fuzz", "harness.exs")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nightly fuzz harness missing at %s: %v", path, err)
	}
	harness := string(data)

	for _, want := range []string{
		"@readers [",
		":load,",
		":load_retain,",
		":load_measure,",
		":strict,",
		":strict,\n    :json,\n    :save_retain,",
		":save_retain,",
		":measure_retain,",
		":cook_from,",
		":cook_measure",
		"module.load(table, bytes)",
		"module.load_retain(table, bytes, empty_retain(), empty_report())",
		"module.load_measure(table, bytes)",
		"module.save(table, value)",
		"module.from_json(table, text)",
		"module.save_retain(table, value, empty_retain(), empty_report())",
		"module.measure_retain(table, value, empty_retain())",
		"module.cook_from(table, value, buffer, :little)",
		"module.cook_measure(table, value)",
		"defp bit_flip(",
		"defp truncate(",
		"defp splice(",
		"defp boundary_integer(",
		"defp minimize(",
		"defp replay(",
		"StreamData.check_all(",
		"StreamData.tuple(",
		"StreamData.member_of(",
		"mutation_generator(",
		":cover.start()",
		":cover.compile_beam(module)",
		":cover.analyse(module, :coverage, :line)",
		"defp apply_coverage(",
	} {
		if !strings.Contains(harness, want) {
			t.Errorf("nightly-fuzz pattern missing: %s (in %s)", want, path)
		}
	}
}
