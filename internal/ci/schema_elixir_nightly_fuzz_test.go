package ci

import (
	"os"
	"path/filepath"
	"strconv"
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
		"SCHEMA_ELIXIR_FUZZ_SECONDS         ?= 540",
		"$(ELIXIR_TABLE_UNITS)",
		"schema-elixir-nightly-fuzz:",
		"schema-elixir-nightly-fuzz-corpus:",
		"schema-elixir-nightly-fuzz-replay:",
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

	// The timeout (SCHEMA_ELIXIR_FUZZ_TIME) must carry slack over the harness
	// loop (SCHEMA_ELIXIR_FUZZ_SECONDS): the loop runs for SECONDS and then
	// seeds and starts up, so a timeout equal to the loop seconds kills a healthy
	// timed pass (exit 124). The default loop must be strictly shorter than the
	// default timeout.
	timeSec := parseTimeBudget(t, fragment, "SCHEMA_ELIXIR_FUZZ_TIME")
	seconds := parseTimeBudget(t, fragment, "SCHEMA_ELIXIR_FUZZ_SECONDS")
	if timeSec <= seconds {
		t.Errorf("the night fuzz timeout (%ds) carries no slack over the loop (%ds); give the timeout room over SECONDS", timeSec, seconds)
	}
}

// parseTimeBudget reads a make `?=` default for a time budget variable (either
// "600" seconds or "10m" style) out of the fragment.
func parseTimeBudget(t *testing.T, fragment, name string) int64 {
	t.Helper()
	lines := strings.Split(fragment, "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, name) {
			continue
		}
		eq := strings.Index(line, "?=")
		if eq < 0 {
			continue
		}
		val := strings.TrimSpace(line[eq+2:])
		if seconds, err := strconv.ParseInt(val, 10, 64); err == nil {
			return seconds
		}
		if strings.HasSuffix(val, "m") {
			if m, err := strconv.ParseInt(strings.TrimSuffix(val, "m"), 10, 64); err == nil {
				return m * 60
			}
		}
		if strings.HasSuffix(val, "s") {
			if s, err := strconv.ParseInt(strings.TrimSuffix(val, "s"), 10, 64); err == nil {
				return s
			}
		}
		t.Fatalf("could not parse %s default %q", name, val)
	}
	t.Fatalf("%s default not found in fragment", name)
	return 0
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
		"make schema-elixir-nightly-fuzz-ci",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("nightly fuzz workflow does not carry %q", want)
		}
	}
}

// TestSchemaElixirNightlyFuzzPlantedBadPattern is the card's planted-pattern
// test. It reads the harness the make target copies (or the copy a negative
// control plants through SCHEMA_ELIXIR_FUZZ_HARNESS) and requires the three
// reader kinds and the four named mutations to be present. The negative-control
// target removes the JSON reader with sed and requires this test to go red on
// that exact absence; the real harness is green. No toolchain, no clock.
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
		":json,",
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
	} {
		if !strings.Contains(harness, want) {
			t.Errorf("nightly-fuzz pattern missing: %s (in %s)", want, path)
		}
	}
}
