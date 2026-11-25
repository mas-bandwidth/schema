package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

// TestSchemaJsRunsHostileCorpusWiring holds the JavaScript runner's blades
// without needing Node in the Go unit tier: the make target exists and is a
// prerequisite of the port's normal unit target, the runner reads the committed
// manifest with every carrier bound, no list carries a finding, the probe
// schema is the corpus's, and the
// control rides a pull-request negative-control row.
func TestSchemaJsRunsHostileCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile, err := os.ReadFile(filepath.Join(root, "make", "schema-js-runs-hostile-corpus.mk"))
	if err != nil {
		t.Fatalf("the JavaScript hostile-corpus make file is missing: %v", err)
	}
	for _, want := range []string{
		".PHONY: schema-js-runs-hostile-corpus",
		"test-js: schema-js-runs-hostile-corpus",
		"schema-js-runs-hostile-corpus-negative-control: schema-js-runs-hostile-corpus",
		"test/js-hostile/runner.mjs",
		"internal/ci/hostilecorpus/corpus",
	} {
		if !strings.Contains(string(makeFile), want) {
			t.Errorf("make/schema-js-runs-hostile-corpus.mk does not carry %q", want)
		}
	}

	runner, err := os.ReadFile(filepath.Join(root, "test", "js-hostile", "runner.mjs"))
	if err != nil {
		t.Fatalf("the JavaScript runner is missing: %v", err)
	}
	for _, want := range []string{"manifest.tsv", "HOSTILE_CASE_BUDGET_MS", "Worker",
		"FromJson", "Load`", "LoadRetain", "LoadMessages", "AnnounceRead", "ReadStream", "Flat`",
		"\"text\"", "\"wire\"", "\"wire_retain\"", "\"message\"", "\"message_novocab\"", "\"announce\"",
		"\"announce_second\"", "\"announce_bytes\"", "\"announce_entries\"", "\"packet\"", "\"development\"", "\"production\""} {
		if !strings.Contains(string(runner), want) {
			t.Errorf("test/js-hostile/runner.mjs does not carry %q", want)
		}
	}

	probe, err := os.ReadFile(filepath.Join(root, "test", "js-hostile", "probe.schema"))
	if err != nil {
		t.Fatalf("the probe schema is missing: %v", err)
	}
	if string(probe) != hostilecorpus.ProbeSchema {
		t.Error("test/js-hostile/probe.schema is not the corpus's schemas/probe.schema (hostilecorpus.ProbeSchema)")
	}

	// NO LIST CARRIES A FINDING (docs/rulings.json no-skips): every
	// disagreement is red by its case's name, so neither a findings file nor
	// a runner that reads one is wired here
	if _, err := os.Stat(filepath.Join(root, "test", "js-hostile", "known-findings.tsv")); !os.IsNotExist(err) {
		t.Errorf("test/js-hostile/known-findings.tsv is present: a list that carries findings by name (%v)", err)
	}
	for _, carry := range []string{"known-findings", "FIX-CARD", "STALE"} {
		if strings.Contains(string(runner), carry) {
			t.Errorf("test/js-hostile/runner.mjs carries %q: a finding is never carried", carry)
		}
	}

	controls, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(controls), `"schema-js-runs-hostile-corpus-negative-control"`) {
		t.Error("make/negative-controls.json does not register the JavaScript hostile-corpus control")
	}
}
