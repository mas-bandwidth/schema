package ci

// THE C++ PORT RUNS THE WHOLE SHARED HOSTILE CORPUS
// (schema-cpp-runs-hostile-corpus; docs/HOSTILE-CORPUS.md).
//
// The corpus is one committed manifest and one raw input per case. The runner
// test/tables/hostile_corpus_main.cpp decodes each input through the generated
// C++ reader for the manifest's carrier and prints its verdict, named reason
// and canonical text. This test compares every answer with the manifest, with
// a planted-reader-bug control beside it. It reads files only.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

type cppMismatch struct {
	name, want, got string
}

type cppAnswer struct {
	verdict, reason, text string
}

func cppRunner(t *testing.T) string {
	t.Helper()
	runner := os.Getenv("SCHEMA_CPP_HOSTILE_RUNNER")
	if runner == "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal("SCHEMA_REQUIRE_CORPUS is set and SCHEMA_CPP_HOSTILE_RUNNER names no runner")
		}
		t.Skip("no C++ hostile-corpus runner: see make/schema-cpp-runs-hostile-corpus.mk")
	}
	return runner
}

func cppRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

func parseCppAnswers(out string) map[string]cppAnswer {
	got := map[string]cppAnswer{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 || f[0] != "CASE" {
			continue
		}
		a := cppAnswer{verdict: f[2], reason: f[3]}
		if raw, err := base64.StdEncoding.DecodeString(f[4]); err == nil {
			a.text = string(raw)
		}
		got[f[1]] = a
	}
	return got
}

func runCppCorpus(t *testing.T, planted bool) (map[string]cppAnswer, []hostilecorpus.Case) {
	t.Helper()
	root := cppRepoRoot(t)
	runner := cppRunner(t)
	corpus := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	cmd := exec.Command(runner, corpus)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	if planted {
		cmd.Env = append(cmd.Env, "HOSTILE_CPP_PLANT=1")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ corpus runner did not run: %v\n%s", err, out)
	}
	_, cases, err := hostilecorpus.LoadManifest(corpus)
	if err != nil {
		t.Fatal(err)
	}
	return parseCppAnswers(string(out)), cases
}

func compareCpp(t *testing.T, cases []hostilecorpus.Case, got map[string]cppAnswer) []cppMismatch {
	t.Helper()
	var out []cppMismatch
	for _, c := range cases {
		a, ok := got[c.Name]
		if !ok {
			out = append(out, cppMismatch{c.Name, c.Verdict + "/" + c.Reason, "no answer"})
			continue
		}
		if a.verdict != c.Verdict {
			out = append(out, cppMismatch{c.Name, c.Verdict + "/" + c.Reason, a.verdict + "/" + a.reason})
			continue
		}
		if c.Verdict == hostilecorpus.Refuse {
			if a.reason != c.Reason {
				out = append(out, cppMismatch{c.Name, c.Reason, a.reason})
			}
			continue
		}
		sum := sha256.Sum256([]byte(strings.TrimSuffix(a.text, "\n")))
		if hex.EncodeToString(sum[:]) != c.Digest {
			out = append(out, cppMismatch{c.Name, c.Digest, hex.EncodeToString(sum[:])})
		}
	}
	return out
}

// TestSchemaCppRunsHostileCorpus is the gate. A mismatch here is a finding
// against the C++ reader, not against the corpus.
func TestSchemaCppRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	got, cases := runCppCorpus(t, false)
	bad := compareCpp(t, cases, got)
	if len(bad) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "the C++ port disagrees with %d of %d hostile-corpus cases:", len(bad), len(cases))
		for _, m := range bad {
			fmt.Fprintf(&b, "\n  %s: want %s, got %s", m.name, m.want, m.got)
		}
		t.Fatal(b.String())
	}
	fmt.Printf("--- PASS: %d hostile-corpus cases, every verdict, reason and digest\n", len(cases))
}

// TestSchemaCppHostileCorpusNegativeControl plants a reader bug in the runner
// (scratch only, switched on by the test) and requires the comparison to see
// it. Without the plant the same case must answer its manifest verdict.
func TestSchemaCppHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	clean, cases := runCppCorpus(t, false)
	var cleanAnswer cppAnswer
	foundClean := false
	for _, c := range cases {
		if c.Name == "wire-form-ff" {
			cleanAnswer = clean[c.Name]
			foundClean = true
		}
	}
	if !foundClean {
		t.Fatal("the corpus carries no wire-form-ff case")
	}
	if cleanAnswer.reason != "newer_form" {
		t.Fatalf("without the plant wire-form-ff answered %q, want newer_form", cleanAnswer.reason)
	}
	planted, cases := runCppCorpus(t, true)
	bad := compareCpp(t, cases, planted)
	for _, m := range bad {
		if m.name == "wire-form-ff" {
			t.Logf("planted reader bug seen: %s: want %s, got %s", m.name, m.want, m.got)
			return
		}
	}
	t.Fatal("the planted reader bug (wire-form-ff -> damaged) was not seen")
}
