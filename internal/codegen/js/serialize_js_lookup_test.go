package js

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// THE SERIALIZE.JS RUNTIME IS PART OF THE NODE HALF. The node halves of this
// package import the serialize.js runtime, so a missing runtime fails the test
// and names itself, on every lane; nothing skips. Every lookup of the runtime
// goes through serializeJSDir. Before this, a bench with no sibling answered ok
// with every node test skipped (TestJsWriteRefusesEveryValueShape among them,
// #4936).

// serializeJSDir finds the serialize.js runtime checkout: $SERIALIZE_JS
// first, then the sibling checkout beside the repository.
func serializeJSDir(t testing.TB) string {
	t.Helper()
	return serializeJSDirIn(t, os.Getenv("SERIALIZE_JS"), []string{"../../../../serialize.js", "../../../serialize.js"})
}

// serializeJSIndex is the runtime's entry module.
func serializeJSIndex(t testing.TB) string {
	t.Helper()
	return filepath.Join(serializeJSDir(t), "src", "index.js")
}

// serializeJSDirIn is serializeJSDir over an explicit $SERIALIZE_JS and
// sibling list, so the lane promise is testable without the real checkout.
func serializeJSDirIn(t testing.TB, env string, siblings []string) string {
	t.Helper()
	candidates := siblings
	if env != "" {
		candidates = append([]string{env}, siblings...)
	}
	for _, c := range candidates {
		dir, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(filepath.Join(dir, "src", "index.js")); err == nil && !fi.IsDir() {
			return dir
		}
	}
	const reason = "no serialize.js runtime: set SERIALIZE_JS or stage the sibling checkout"
	t.Fatalf("the node half's runtime is missing: %s (make serialize-siblings stages it)", reason)
	return ""
}

// lookupVerdictTB records what a lookup did instead of doing it.
type lookupVerdictTB struct {
	testing.TB
	verdict, text string
}

func (r *lookupVerdictTB) Helper() {}

func (r *lookupVerdictTB) Fatalf(format string, args ...any) {
	r.verdict, r.text = "fail", fmt.Sprintf(format, args...)
}

// TestSerializeJSLookupHoldsTheLanePromise: a missing runtime fails naming the
// runtime; nothing skips; a staged runtime is found from $SERIALIZE_JS or a
// sibling. There are no lanes: no variable makes a missing runtime anything
// but a failure.
func TestSerializeJSLookupHoldsTheLanePromise(t *testing.T) {
	empty := t.TempDir()
	staged := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staged, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "src", "index.js"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	const reason = "no serialize.js runtime: set SERIALIZE_JS or stage the sibling checkout"
	const missing = "the node half's runtime is missing: " + reason + " (make serialize-siblings stages it)"
	for _, c := range []struct {
		name, env     string
		siblings      []string
		verdict, text string
	}{
		{"no runtime", "", []string{empty}, "fail", missing},
		{"SERIALIZE_JS names no runtime, no sibling", empty, []string{empty}, "fail", missing},
		{"runtime from SERIALIZE_JS", staged, []string{empty}, "run", staged},
		{"runtime as a sibling", "", []string{empty, staged}, "run", staged},
		{"stale SERIALIZE_JS, sibling staged", empty, []string{staged}, "run", staged},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := &lookupVerdictTB{TB: t, verdict: "run"}
			got := serializeJSDirIn(rec, c.env, c.siblings)
			if rec.verdict == "run" {
				rec.text = got
			}
			if rec.verdict != c.verdict || rec.text != c.text {
				t.Errorf("got %s %q, want %s %q", rec.verdict, rec.text, c.verdict, c.text)
			}
		})
	}
}
