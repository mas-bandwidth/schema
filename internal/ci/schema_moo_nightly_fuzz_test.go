package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mooNightlyFuzzWiring holds the moo packet-wire fuzz's artefacts to the shape
// that makes the nightly a gate rather than a script (make/moo-fuzz.mk,
// make/schema-moo-nightly-fuzz.mk, .github/workflows/schema-moo-nightly-fuzz.yml):
// a case-count budget and a seed, never a clock; a workflow that fuzzes at depth
// only on the schedule and on dispatch; the findings kept on a red night; the
// planted patterns run beside the long run; and the control's manifest line
// carrying its reason. It returns every way the files fall short.
func mooNightlyFuzzWiring(leg, nightly, workflow, manifest string) []string {
	var bad []string
	need := func(text, file string, wants ...string) {
		for _, w := range wants {
			if !strings.Contains(text, w) {
				bad = append(bad, file+" does not carry "+w)
			}
		}
	}
	need(leg, "make/moo-fuzz.mk",
		"moo-wire-fuzz:",
		"SCHEMA_MOO_FUZZ_SEED=$(MOO_FUZZ_SEED)",
		"SCHEMA_MOO_FUZZ_N=$(MOO_FUZZ_N)",
		"test/slowgate/proof",
		"TestMooWireFuzzAgreesWithTheGoTarget TestMooWireFuzzWriteNegativeControl TestMooWireFuzzReadNegativeControl")
	need(nightly, "make/schema-moo-nightly-fuzz.mk",
		"MOO_NIGHTLY_FUZZ_ROUNDS ?=",
		"MOO_NIGHTLY_FUZZ_N      ?=",
		"schema-moo-nightly-fuzz:",
		"schema-moo-nightly-fuzz-negative-control:",
		"SCHEMA_MOO_FUZZ_ROUNDS=$(MOO_NIGHTLY_FUZZ_ROUNDS)",
		"SCHEMA_MOO_FUZZ_SEED=$(MOO_NIGHTLY_FUZZ_SEED)",
		"SCHEMA_MOO_FUZZ_FAILURES=",
		"TestMooWireFuzzWriteNegativeControl TestMooWireFuzzReadNegativeControl")
	if strings.Contains(nightly, "-fuzztime") || strings.Contains(nightly, "SCHEMA_MOO_FUZZ_TIME") {
		bad = append(bad, "make/schema-moo-nightly-fuzz.mk budgets by a clock; the budget is a case count")
	}
	need(workflow, "schema-moo-nightly-fuzz.yml",
		"schedule:", "workflow_dispatch:", "push:", "pull_request:",
		"if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
		"make schema-moo-nightly-fuzz MOO_NIGHTLY_FUZZ_SEED=${{ github.run_id }}",
		"make schema-moo-nightly-fuzz-negative-control",
		"uses: ./.github/actions/moo",
		"serialize.go.git serialize.go",
		"build/moo-nightly-fuzz/findings/",
		"TestSchemaMooNightlyFuzzWiring")
	var m struct {
		Groups []struct {
			Targets []string `json:"targets"`
		} `json:"groups"`
		Excluded []struct {
			Target string `json:"target"`
			Reason string `json:"reason"`
		} `json:"excluded"`
	}
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		return append(bad, "make/negative-controls.json does not parse: "+err.Error())
	}
	found := false
	for _, e := range m.Excluded {
		if e.Target == "schema-moo-nightly-fuzz-negative-control" {
			found = true
			if !strings.Contains(e.Reason, "TestMooWireFuzzWriteNegativeControl") || !strings.Contains(e.Reason, "schema-moo-nightly-fuzz.yml") {
				bad = append(bad, "the moo fuzz control's exclusion does not name the go tests and the job that run it")
			}
		}
	}
	for _, g := range m.Groups {
		for _, target := range g.Targets {
			if target == "schema-moo-nightly-fuzz-negative-control" {
				bad = append(bad, "the moo fuzz control sits in a manifest group, whose row installs no moo pin")
			}
		}
	}
	if !found {
		bad = append(bad, "make/negative-controls.json does not carry the moo fuzz control's line")
	}
	return bad
}

func readRepoFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSchemaMooNightlyFuzzWiring reads files only: the heavy work is the make
// target the nightly runs; this is the cheap structural half a push can afford.
func TestSchemaMooNightlyFuzzWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wf, ok := workflows(t, root)["schema-moo-nightly-fuzz.yml"]
	if !ok {
		t.Fatal("no .github/workflows/schema-moo-nightly-fuzz.yml")
	}
	for _, b := range mooNightlyFuzzWiring(
		readRepoFile(t, root, "make", "moo-fuzz.mk"),
		readRepoFile(t, root, "make", "schema-moo-nightly-fuzz.mk"),
		wf,
		readRepoFile(t, root, "make", "negative-controls.json")) {
		t.Error(b)
	}
}

// TestSchemaMooNightlyFuzzWiringNegativeControl: the nightly job's depth run
// taken out of the workflow, or a clock put in as the budget, each turns the
// check red for that reason.
func TestSchemaMooNightlyFuzzWiringNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	leg := readRepoFile(t, root, "make", "moo-fuzz.mk")
	nightly := readRepoFile(t, root, "make", "schema-moo-nightly-fuzz.mk")
	wf := workflows(t, root)["schema-moo-nightly-fuzz.yml"]
	manifest := readRepoFile(t, root, "make", "negative-controls.json")
	cut := strings.Replace(wf, "make schema-moo-nightly-fuzz MOO_NIGHTLY_FUZZ_SEED", "echo skipped", 1)
	if cut == wf {
		t.Fatal("the workflow's fuzz line moved; the control patches nothing")
	}
	if got := mooNightlyFuzzWiring(leg, nightly, cut, manifest); len(got) != 1 || !strings.Contains(got[0], "make schema-moo-nightly-fuzz MOO_NIGHTLY_FUZZ_SEED") {
		t.Errorf("a workflow with no depth run: %v", got)
	}
	clock := nightly + "\nSCHEMA_MOO_FUZZ_TIME ?= 600\n"
	if got := mooNightlyFuzzWiring(leg, clock, wf, manifest); len(got) != 1 || !strings.Contains(got[0], "a clock") {
		t.Errorf("a clock budget: %v", got)
	}
}
