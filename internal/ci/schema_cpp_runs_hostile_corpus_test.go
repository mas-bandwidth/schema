package ci

// THE C++ PORT RUNS THE WHOLE SHARED HOSTILE CORPUS
// (schema-cpp-runs-hostile-corpus; docs/HOSTILE-CORPUS.md).
//
// The corpus is one committed manifest and one raw input per case. The runner
// test/tables/hostile_corpus_main.cpp decodes each input through the generated
// C++ reader for the manifest's carrier and prints its verdict, named reason
// and canonical text. This test compares every answer with the manifest, with
// a planted-reader-bug control beside it. It reads files only.
//
// The manifest is read here with its own line reader (the portable format
// docs/HOSTILE-CORPUS.md names: nine tab-separated fields, '#' comments and
// blank lines ignored), so this file needs nothing from the corpus package.

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
)

type cppCase struct {
	name, verdict, reason, digest string
}

type cppMismatch struct {
	name, want, got string
}

type cppAnswer struct {
	verdict, reason, text string
}

// cppRunner names the runner binary from env. Outside the make target the test
// skips; the make target sets SCHEMA_REQUIRE_CORPUS so a missing runner is red.
func cppRunner(t *testing.T, env string) string {
	t.Helper()
	runner := os.Getenv(env)
	if runner == "" {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s names no runner", env)
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

// parseCppManifest reads the corpus manifest: nine tab-separated fields per
// case (name carrier schema table input provenance verdict reason digest).
func parseCppManifest(raw string) ([]cppCase, error) {
	var cases []cppCase
	seen := map[string]bool{}
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		if f[6] != "accept" && f[6] != "refuse" {
			return nil, fmt.Errorf("manifest line %d: verdict %q", i+1, f[6])
		}
		if seen[f[0]] {
			return nil, fmt.Errorf("manifest line %d: case %q twice", i+1, f[0])
		}
		seen[f[0]] = true
		cases = append(cases, cppCase{name: f[0], verdict: f[6], reason: f[7], digest: f[8]})
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("manifest holds no cases")
	}
	return cases, nil
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

func runCppCorpus(t *testing.T, env string) (map[string]cppAnswer, []cppCase) {
	t.Helper()
	root := cppRepoRoot(t)
	runner := cppRunner(t, env)
	corpus := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	raw, err := os.ReadFile(filepath.Join(corpus, "manifest.tsv"))
	if err != nil {
		t.Fatalf("the shared hostile corpus is not in this checkout: %v", err)
	}
	cases, err := parseCppManifest(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(runner, corpus)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the C++ corpus runner did not run: %v\n%s", err, out)
	}
	return parseCppAnswers(string(out)), cases
}

func compareCpp(cases []cppCase, got map[string]cppAnswer) []cppMismatch {
	var out []cppMismatch
	for _, c := range cases {
		a, ok := got[c.name]
		if !ok {
			out = append(out, cppMismatch{c.name, c.verdict + "/" + c.reason, "no answer"})
			continue
		}
		if a.verdict != c.verdict {
			out = append(out, cppMismatch{c.name, c.verdict + "/" + c.reason, a.verdict + "/" + a.reason})
			continue
		}
		if c.verdict == "refuse" {
			if a.reason != c.reason {
				out = append(out, cppMismatch{c.name, c.reason, a.reason})
			}
			continue
		}
		sum := sha256.Sum256([]byte(strings.TrimSuffix(a.text, "\n")))
		if hex.EncodeToString(sum[:]) != c.digest {
			out = append(out, cppMismatch{c.name, c.digest, hex.EncodeToString(sum[:])})
		}
	}
	return out
}

func TestParseCppManifest(t *testing.T) {
	t.Parallel()
	good := "# comment\n\na\twire\ts\tT\tinputs/a\tp\trefuse\tnewer_form\t-\nb\ttext\ts\tT\tinputs/b\tp\taccept\t-\tabc\n"
	cases, err := parseCppManifest(good)
	if err != nil || len(cases) != 2 || cases[0].reason != "newer_form" || cases[1].digest != "abc" {
		t.Fatalf("parse: %v %+v", err, cases)
	}
	for _, bad := range []string{
		"",
		"a\twire\ts\tT\tinputs/a\tp\trefuse\tnewer_form\n",
		"a\twire\ts\tT\tinputs/a\tp\tmaybe\tnewer_form\t-\n",
		"a\twire\ts\tT\tinputs/a\tp\trefuse\tx\t-\na\twire\ts\tT\tinputs/a\tp\trefuse\tx\t-\n",
	} {
		if _, err := parseCppManifest(bad); err == nil {
			t.Fatalf("parse accepted %q", bad)
		}
	}
	want := []cppCase{{"r", "refuse", "newer_form", "-"}, {"a", "accept", "-", hex.EncodeToString(func() []byte { s := sha256.Sum256([]byte("{}")); return s[:] }())}}
	got := map[string]cppAnswer{"r": {"refuse", "newer_form", ""}, "a": {"accept", "-", "{}\n"}}
	if bad := compareCpp(want, got); len(bad) != 0 {
		t.Fatalf("agreeing answers flagged: %+v", bad)
	}
	got["r"] = cppAnswer{"refuse", "damaged", ""}
	got["a"] = cppAnswer{"accept", "-", "{\"x\":1}"}
	if bad := compareCpp(want, got); len(bad) != 2 {
		t.Fatalf("disagreeing answers not flagged: %+v", bad)
	}
	delete(got, "r")
	if bad := compareCpp(want, got); len(bad) != 2 || bad[0].got != "no answer" {
		t.Fatalf("a missing answer not flagged: %+v", bad)
	}
}

// TestSchemaCppRunsHostileCorpus is the gate. A mismatch here is a finding
// against the C++ reader, not against the corpus.
func TestSchemaCppRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	got, cases := runCppCorpus(t, "SCHEMA_CPP_HOSTILE_RUNNER")
	bad := compareCpp(cases, got)
	if len(bad) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "the C++ port disagrees with %d of %d hostile-corpus cases:", len(bad), len(cases))
		for _, m := range bad {
			fmt.Fprintf(&b, "\n  %s: want %s, got %s", m.name, m.want, m.got)
		}
		t.Fatal(b.String())
	}
	t.Logf("%d hostile-corpus cases agree: every verdict, reason and digest", len(cases))
}

// TestSchemaCppHostileCorpusNegativeControl runs a runner built from a SCRATCH
// COPY of the generated readers with one reader bug planted in it (the make
// target's sed: the generated wire reader names previous_form where it should
// name newer_form). The comparison must see the plant on wire-form-ff, and the
// clean runner must answer that case as the manifest does.
func TestSchemaCppHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	clean, cases := runCppCorpus(t, "SCHEMA_CPP_HOSTILE_RUNNER")
	if a := clean["wire-form-ff"]; a.reason != "newer_form" {
		t.Fatalf("without the plant wire-form-ff answered %q, want newer_form", a.reason)
	}
	planted, cases := runCppCorpus(t, "SCHEMA_CPP_HOSTILE_RUNNER_PLANTED")
	for _, m := range compareCpp(cases, planted) {
		if m.name == "wire-form-ff" {
			t.Logf("planted reader bug seen: %s: want %s, got %s", m.name, m.want, m.got)
			return
		}
	}
	t.Fatal("the planted reader bug (generated examples/PackTable.h: newer_form -> previous_form) was not seen on wire-form-ff")
}
