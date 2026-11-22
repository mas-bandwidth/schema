package js

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE SERIALIZE.JS RUNTIME IS PART OF THE NODE PROMISE. The node halves of
// this package import the serialize.js runtime. A lane that says it provides
// node (SCHEMA_REQUIRE_NODE=1, or SCHEMA_REQUIRE_CORPUS) also stages the
// runtime, so a missing runtime there fails and names itself; elsewhere it
// skips with the reason. Every lookup of the runtime goes through
// serializeJSDir.

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
	slowtest.Require(t, "SCHEMA_REQUIRE_NODE", "no serialize.js runtime: set SERIALIZE_JS or stage the sibling checkout")
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

func (r *lookupVerdictTB) Skip(args ...any) {
	r.verdict, r.text = "skip", fmt.Sprint(args...)
}

// TestSerializeJSLookupHoldsTheLanePromise: a lane that declares node and has
// no runtime fails naming the runtime; a lane that does not declare it skips;
// a staged runtime is found from $SERIALIZE_JS or a sibling.
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
	for _, c := range []struct {
		name, lane, env string
		siblings        []string
		verdict, text   string
	}{
		{"node lane, no runtime", "SCHEMA_REQUIRE_NODE", empty, []string{empty}, "fail",
			"SCHEMA_REQUIRE_NODE=1 says this lane provides what the test needs, and it is missing: " + reason},
		{"corpus lane, no runtime", "SCHEMA_REQUIRE_CORPUS", "", []string{empty}, "fail",
			"SCHEMA_REQUIRE_CORPUS is set, so this lane is meant to have what the test needs, and it is missing: " + reason},
		{"tsc lane, no runtime", "SCHEMA_REQUIRE_TSC", "", []string{empty}, "skip", reason},
		{"no lane, no runtime", "", "", []string{empty}, "skip", reason},
		{"node lane, runtime from SERIALIZE_JS", "SCHEMA_REQUIRE_NODE", staged, []string{empty}, "run", staged},
		{"node lane, runtime as a sibling", "SCHEMA_REQUIRE_NODE", "", []string{empty, staged}, "run", staged},
		{"node lane, stale SERIALIZE_JS, sibling staged", "SCHEMA_REQUIRE_NODE", empty, []string{staged}, "run", staged},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, k := range []string{"SCHEMA_REQUIRE_NODE", "SCHEMA_REQUIRE_TSC", "SCHEMA_REQUIRE_CORPUS"} {
				t.Setenv(k, "")
			}
			if c.lane != "" {
				t.Setenv(c.lane, "1")
			}
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
