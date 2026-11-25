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
// make/schema-moo-nightly-fuzz.mk): a case-count budget and a seed, never a
// clock; the findings kept on a red night; the planted patterns run beside
// the long run; and the control's manifest line carrying its reason. (Its
// workflow is gone, registry L7: the nightly run is a registry row.) It
// returns every way the files fall short.
func mooNightlyFuzzWiring(leg, nightly, manifest string) []string {
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
			if !strings.Contains(e.Reason, "TestMooWireFuzzWriteNegativeControl") {
				bad = append(bad, "the moo fuzz control's exclusion does not name the go tests that run it")
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
// target the nightly runs; this is the cheap structural half the Go unit tier
// can afford.
func TestSchemaMooNightlyFuzzWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, b := range mooNightlyFuzzWiring(
		readRepoFile(t, root, "make", "moo-fuzz.mk"),
		readRepoFile(t, root, "make", "schema-moo-nightly-fuzz.mk"),
		readRepoFile(t, root, "make", "negative-controls.json")) {
		t.Error(b)
	}
}

// TestSchemaMooNightlyFuzzWiringNegativeControl: a clock put in as the budget,
// or the control's manifest line moved into a group, each turns the check red
// for that reason.
func TestSchemaMooNightlyFuzzWiringNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	leg := readRepoFile(t, root, "make", "moo-fuzz.mk")
	nightly := readRepoFile(t, root, "make", "schema-moo-nightly-fuzz.mk")
	manifest := readRepoFile(t, root, "make", "negative-controls.json")
	clock := nightly + "\nSCHEMA_MOO_FUZZ_TIME ?= 600\n"
	if got := mooNightlyFuzzWiring(leg, clock, manifest); len(got) != 1 || !strings.Contains(got[0], "a clock") {
		t.Errorf("a clock budget: %v", got)
	}
	grouped := strings.Replace(manifest, `"groups": [`, `"groups": [{"targets": ["schema-moo-nightly-fuzz-negative-control"]}, `, 1)
	if grouped == manifest {
		t.Fatal("the manifest's groups list moved; the control patches nothing")
	}
	if got := mooNightlyFuzzWiring(leg, nightly, grouped); len(got) != 1 || !strings.Contains(got[0], "manifest group") {
		t.Errorf("the control in a manifest group: %v", got)
	}
}
