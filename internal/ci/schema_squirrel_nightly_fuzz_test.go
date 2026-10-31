package ci

// THE NIGHTLY-FUZZ HARNESS'S OWN GATE (schema-squirrel-nightly-fuzz). The
// campaign in make/schema-squirrel-nightly-fuzz.mk is minutes of foreign
// process time and cannot ride the pull request; this is the half that can.
// It holds the three things a nightly that ran nowhere would still look green
// without: the target and its time budget, the corpus file's provenance from
// the Makefile's own SCHEMAS_* variables, and a workflow whose fuzz job is
// scheduled and never per push. Each property is checked by the same function
// that a planted copy must FAIL, so the gate is shown capable of going red.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nightlyFuzzMake = "make/schema-squirrel-nightly-fuzz.mk"
const nightlyFuzzWorkflow = ".github/workflows/schema-squirrel-nightly-fuzz.yml"

// nightlyFuzzProblems is the gate. An empty result is a pass; every string is
// one broken property, named.
func nightlyFuzzProblems(mk, wf string) []string {
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
	// THE WORKFLOW: nightly and dispatch run the campaign; a pull request runs
	// only the structural half, so the fuzz never becomes per-push CI.
	want(wf, "schedule:", "the workflow has no schedule trigger")
	want(wf, "workflow_dispatch:", "the workflow has no dispatch trigger")
	want(wf, "make schema-squirrel-nightly-fuzz\n", "the workflow does not run the campaign")
	want(wf, "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'", "the campaign is not confined to the nightly triggers")
	want(wf, "pull_request:", "the workflow has no pull-request replay")
	return bad
}

func TestSquirrelNightlyFuzz(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, nightlyFuzzMake))
	if err != nil {
		t.Fatal(err)
	}
	wf, err := os.ReadFile(filepath.Join(root, nightlyFuzzWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	if bad := nightlyFuzzProblems(string(mk), string(wf)); len(bad) > 0 {
		t.Fatalf("the squirrel nightly-fuzz harness is incomplete:\n  %s", strings.Join(bad, "\n  "))
	}

	// THE PLANTED PATTERNS. A copy with the budget, the corpus provenance or
	// the nightly trigger removed must be refused; a copy with the plant out
	// must pass. This is the gate's red line, kept in the test.
	plants := []struct {
		name, mk, wf string
		needle       string // the line the plant removes
	}{
		{"no time budget", strings.Replace(string(mk), "SQUIRREL_FUZZ_SECONDS ?= 540", "", 1), string(wf), "time-budget"},
		{"no corpus provenance", strings.Replace(string(mk), "$(SCHEMAS_TABLES)", "", 1), string(wf), "corpus"},
		{"no strict reader", strings.Replace(string(mk), "Strict", "", 1), string(wf), "strict"},
		{"no text/json reader", strings.Replace(string(mk), "TextJSON", "", 1), string(wf), "text/json"},
		{"no schedule", string(mk), strings.Replace(string(wf), "schedule:", "", 1), "schedule"},
		{"campaign on every push", string(mk), strings.Replace(string(wf), "if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'", "", 1), "trigger"},
	}
	for _, p := range plants {
		bad := nightlyFuzzProblems(p.mk, p.wf)
		if len(bad) == 0 {
			t.Fatalf("PLANTED PATTERN %q left the gate GREEN: %s", p.name, p.needle)
		}
	}
}

// THE CAMPAIGN STAYS NIGHTLY IN GO TOO. The workflow keeps the campaign off the
// pull request, but ci-full.yml's go-test lane runs `go test ./internal/codegen/...`
// with SCHEMA_SLOW=1, which passes slowtest.Gate. A nightly test that forgot its
// campaign gate ran there on 2026-10-21 and timed the package out at ten minutes
// (TestSquirrelNightlyFuzzTextJSON). Every nightly test therefore opens with
// nightlyCampaign(t), which skips unless make schema-squirrel-nightly-fuzz set
// the corpus.
var nightlyFuzzHarness = []string{
	"internal/codegen/squirreltable/nightly_fuzz_test.go",
	"internal/codegen/squirreltable/nightly_fuzz_readers_test.go",
}

// nightlyCampaignProblems names every TestSquirrelNightlyFuzz* function in src
// whose body does not call nightlyCampaign(t).
func nightlyCampaignProblems(src string) []string {
	var bad []string
	parts := strings.Split(src, "\nfunc TestSquirrelNightlyFuzz")
	for _, body := range parts[1:] {
		suffix, _, _ := strings.Cut(body, "(")
		name := "TestSquirrelNightlyFuzz" + suffix
		if end := strings.Index(body, "\n}\n"); end >= 0 {
			body = body[:end]
		}
		if !strings.Contains(body, "nightlyCampaign(t)") {
			bad = append(bad, name+" runs outside its campaign (no nightlyCampaign(t))")
		}
	}
	return bad
}

func TestSquirrelNightlyFuzzTestsStayInTheCampaign(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tests := 0
	for _, rel := range nightlyFuzzHarness {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		tests += strings.Count(string(src), "\nfunc TestSquirrelNightlyFuzz")
		if bad := nightlyCampaignProblems(string(src)); len(bad) > 0 {
			t.Fatalf("%s:\n  %s", rel, strings.Join(bad, "\n  "))
		}
		// THE PLANTED PATTERN: the same file with the gate removed must be refused.
		planted := strings.ReplaceAll(string(src), "nightlyCampaign(t)\n", "\n")
		planted = strings.ReplaceAll(planted, "corpus := nightlyCampaign(t)", "corpus := \"\"")
		if len(nightlyCampaignProblems(planted)) == 0 {
			t.Fatalf("PLANTED PATTERN: %s with nightlyCampaign removed left the gate GREEN", rel)
		}
	}
	if tests != 3 {
		t.Fatalf("found %d TestSquirrelNightlyFuzz* tests, want the campaign's 3 (tolerant, strict, text/json)", tests)
	}
}
