package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaRustRunsHostileCorpus is the RUST PORT's leg of the shared
// hostile-input corpus (schema-shared-hostile-corpus, docs/HOSTILE-CORPUS.md).
//
// The corpus is committed at internal/ci/hostilecorpus/corpus. The Rust runner
// in test/rust-hostile decodes every raw input through the port's GENERATED
// reader and writes one verdict per case; this test reads that answer back and
// holds it to the manifest: accept with the same canonical-text digest, or
// refuse with the same named reason. It reads files and execs the built runner,
// nothing else.
//
// It is a unit-tier gate: the make target builds the runner first and hands its
// path in SCHEMA_RUST_HOSTILE_RUNNER, so a bare `go test` fails by name rather
// than silently passing with no runner.
func TestSchemaRustRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	corpus := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	runner := os.Getenv("SCHEMA_RUST_HOSTILE_RUNNER")
	if runner == "" {
		runner = filepath.Join(root, "build", "rust-hostile", "target", "debug", "rust-hostile")
	}
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("the Rust hostile-corpus runner is not built at %s: %v (run make schema-rust-runs-hostile-corpus)", runner, err)
	}
	mismatches := runRustHostileCorpus(t, runner, corpus, "")
	if len(mismatches) != 0 {
		for _, m := range mismatches {
			t.Errorf("%s", m)
		}
		t.Fatalf("%d of the committed corpus's cases disagree with the Rust port", len(mismatches))
	}
}

// TestSchemaRustRunsHostileCorpusPlanted is the planted-bad-pattern control.
// The plant is a READER BUG, not a changed manifest: the built runner turns one
// green case's answer wrong when SCHEMA_RUST_HOSTILE_PLANT names it, which is a
// switch the fast gate never sets. With the plant in the same comparison the
// fast gate runs must NAME that case red; with the plant out the same case must
// be green. A gate that cannot watch a changed answer is no gate.
func TestSchemaRustRunsHostileCorpusPlanted(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	corpus := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	runner := os.Getenv("SCHEMA_RUST_HOSTILE_RUNNER")
	if runner == "" {
		runner = filepath.Join(root, "build", "rust-hostile", "target", "debug", "rust-hostile")
	}
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("the Rust hostile-corpus runner is not built at %s: %v (run make schema-rust-runs-hostile-corpus)", runner, err)
	}

	const plantedName = "wire-valid"

	// THE PLANT IN: the runner returns a wrong answer for one green case.
	planted := runRustHostileCorpus(t, runner, corpus, plantedName)
	var seen bool
	for _, m := range planted {
		if strings.HasPrefix(m, plantedName+" ") {
			seen = true
			t.Logf("planted reader bug: %s", m)
		}
	}
	if !seen {
		t.Fatalf("a reader bug on %s left the runner green", plantedName)
	}

	// THE PLANT OUT: the same case is green, so the red above was the plant.
	for _, m := range runRustHostileCorpus(t, runner, corpus, "") {
		if strings.HasPrefix(m, plantedName+" ") {
			t.Fatalf("%s is red with the plant out: %s", plantedName, m)
		}
	}
}

type hostileCase struct {
	name, carrier, schema, table, input, verdict, reason, digest string
}

func loadHostileManifest(t *testing.T, path string) []hostileCase {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []hostileCase
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, hostileCase{
			name: f[0], carrier: f[1], schema: f[2], table: f[3], input: f[4],
			verdict: f[6], reason: f[7], digest: f[8],
		})
	}
	return out
}

// runRustHostileCorpus runs the Rust answer pass and compares it with the
// manifest, returning one line per disagreement.
func runRustHostileCorpus(t *testing.T, runner, corpus, plant string) []string {
	t.Helper()
	out := t.TempDir()
	cmd := exec.Command(runner, corpus, out)
	if plant != "" {
		cmd.Env = append(os.Environ(), "SCHEMA_RUST_HOSTILE_PLANT="+plant)
	}
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the Rust runner failed: %v\n%s", err, stderr.String())
	}
	var bad []string
	for _, c := range loadHostileManifest(t, filepath.Join(corpus, "manifest.tsv")) {
		body, err := os.ReadFile(filepath.Join(out, c.name+".verdict"))
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: no verdict (%v)", c.name, err))
			continue
		}
		parts := strings.SplitN(string(body), "\n", 3)
		if len(parts) < 2 {
			bad = append(bad, fmt.Sprintf("%s: malformed verdict %q", c.name, string(body)))
			continue
		}
		gotVerdict, gotReason := parts[0], parts[1]
		gotDigest := "-"
		if gotVerdict == "accept" {
			sum := sha256.Sum256([]byte(parts[2]))
			gotDigest = hex.EncodeToString(sum[:])
		}
		if gotVerdict != c.verdict || gotReason != c.reason || gotDigest != c.digest {
			bad = append(bad, fmt.Sprintf("%s [%s/%s %s]: want %s %s %s, got %s %s %s",
				c.name, c.carrier, c.schema, c.table, c.verdict, c.reason, c.digest,
				gotVerdict, gotReason, gotDigest))
		}
	}
	return bad
}
