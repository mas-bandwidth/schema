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
// manifest with every carrier bound, every FIX-CARD names a case the manifest
// carries with its own input, the probe schema is the corpus's, and the
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
	for _, want := range []string{"manifest.tsv", "FIX-CARD", "STALE", "HOSTILE_CASE_BUDGET_MS", "Worker",
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

	manifestBytes, err := os.ReadFile(filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus", "manifest.tsv"))
	if err != nil {
		t.Fatalf("the committed manifest is missing: %v", err)
	}
	cases := map[string]string{}
	for line := range strings.SplitSeq(string(manifestBytes), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line has %d fields, want 9: %q", len(f), line)
		}
		cases[f[0]] = f[4]
	}

	findings, err := os.ReadFile(filepath.Join(root, "test", "js-hostile", "known-findings.tsv"))
	if err != nil {
		t.Fatalf("the carried-findings list is missing: %v", err)
	}
	for line := range strings.SplitSeq(string(findings), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("carried finding has %d fields, want 4: %q", len(f), line)
		}
		// a packet case carries one row per read tier and mode: <case>@<tier>-<mode>
		name, _, _ := strings.Cut(f[0], "@")
		input, ok := cases[name]
		if !ok {
			t.Errorf("carried finding %q names a case the manifest does not carry", f[0])
			continue
		}
		if f[2] != input {
			t.Errorf("carried finding %q names input %q, manifest says %q", f[0], f[2], input)
		}
		if !strings.HasPrefix(f[1], "js-hostile-") || strings.TrimSpace(f[3]) == "" {
			t.Errorf("carried finding %q has no FIX-CARD slug and one line: %q", f[0], line)
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
