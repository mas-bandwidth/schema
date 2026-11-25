package ci

// THE NIGHTLY-FUZZ HARNESS'S OWN GATE (schema-squirrel-nightly-fuzz). The
// campaign in make/schema-squirrel-nightly-fuzz.mk is minutes of foreign
// process time and cannot ride the pull request; this is the half that can.
// It holds the three things a nightly that ran nowhere would still look green
// without: the target and its time budget, the corpus file's provenance from
// the Makefile's own SCHEMAS_* variables, and the control the campaign is
// held to. The campaign runs as a nightly registry row
// (TestRetiredWorkflowsAreRegistryRows). Each property is checked by the same function
// that a planted copy must FAIL, so the gate is shown capable of going red.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nightlyFuzzMake = "make/schema-squirrel-nightly-fuzz.mk"

// nightlyFuzzProblems is the gate. An empty result is a pass; every string is
// one broken property, named.
func nightlyFuzzProblems(mk string) []string {
	var bad []string
	want := func(body, needle, what string) {
		if !strings.Contains(body, needle) {
			bad = append(bad, what)
		}
	}
	// THE TARGET, its budget and its corpus. The budget is a make variable
	// passed as go test's -timeout, so a night's wall bound is an argument.
	want(mk, ".PHONY: schema-squirrel-nightly-fuzz\n", "the fuzz target is not declared")
	want(mk, "SQUIRREL_FUZZ_SECONDS ?=", "the campaign has no time-budget variable")
	want(mk, "-timeout $(SQUIRREL_FUZZ_SECONDS)s ./internal/codegen/squirreltable", "the budget does not bound the campaign")
	want(mk, "SCHEMA_SQUIRREL_FUZZ_CORPUS=", "the harness does not read a corpus file")
	want(mk, "$(SCHEMAS_TABLES)", "the corpus is not drawn from the Makefile's SCHEMAS_TABLES")
	want(mk, "Strict", "the strict-read reader kind is not driven")
	want(mk, "TextJSON", "the text/JSON reader kind is not driven")
	want(mk, "tool missing: sq", "a missing interpreter would not be named")
	want(mk, "schema-squirrel-nightly-fuzz-negative-control", "the control is not defined")
	return bad
}

func TestSquirrelNightlyFuzz(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, nightlyFuzzMake))
	if err != nil {
		t.Fatal(err)
	}
	if bad := nightlyFuzzProblems(string(mk)); len(bad) > 0 {
		t.Fatalf("the squirrel nightly-fuzz harness is incomplete:\n  %s", strings.Join(bad, "\n  "))
	}

	// THE PLANTED PATTERNS. A copy with the budget, the corpus provenance or a
	// reader kind removed must be refused; a copy with the plant out
	// must pass. This is the gate's red line, kept in the test.
	plants := []struct {
		name, mk string
		needle   string // the line the plant removes
	}{
		{"no time budget", strings.Replace(string(mk), "SQUIRREL_FUZZ_SECONDS ?= 540", "", 1), "time-budget"},
		{"no corpus provenance", strings.Replace(string(mk), "$(SCHEMAS_TABLES)", "", 1), "corpus"},
		{"no strict reader", strings.Replace(string(mk), "Strict", "", 1), "strict"},
		{"no text/json reader", strings.Replace(string(mk), "TextJSON", "", 1), "text/json"},
	}
	for _, p := range plants {
		bad := nightlyFuzzProblems(p.mk)
		if len(bad) == 0 {
			t.Fatalf("PLANTED PATTERN %q left the gate GREEN: %s", p.name, p.needle)
		}
	}
}
