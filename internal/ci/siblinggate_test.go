package ci

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMissingSiblingStopsMakeWithOneLine holds the sibling gate (the Makefile's
// "THE SIBLING GATE"): a goal that needs a sibling runtime checkout stops when
// the Makefile is read, before a single recipe runs, with one line that names
// the command to run. Without the gate each leg failed in the middle of a
// compile (CS2001, a missing include, a cargo path) and named a symptom.
// make reads the Makefile and stops: no network, no clock, nothing outside the
// working directory is written.
func TestMissingSiblingStopsMakeWithOneLine(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	absent := filepath.Join(t.TempDir(), "absent")
	cases := []struct{ goal, variable string }{
		{"test-c", "SERIALIZE_C"},
		{"test-cpp", "SERIALIZE"},
		{"test-cs", "SERIALIZE_CS"},
		{"test-unity", "SERIALIZE_CS"},
		{"test-rust", "SERIALIZE_RS"},
		{"test-js", "SERIALIZE_JS"},
		{"build/conformance-go", "SERIALIZE_GO"},
		{"conformance", "SERIALIZE"},
	}
	for _, c := range cases {
		t.Run(c.goal, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("make", "-n", c.goal, c.variable+"="+absent)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("make %s with %s absent succeeded:\n%s", c.goal, c.variable, out)
			}
			text := string(out)
			if !strings.Contains(text, "run 'make deps'") || !strings.Contains(text, absent) {
				t.Fatalf("make %s did not name the cause and the command:\n%s", c.goal, text)
			}
			if lines := strings.Count(strings.TrimSpace(text), "\n") + 1; lines != 1 {
				t.Fatalf("make %s said %d lines, want one:\n%s", c.goal, lines, text)
			}
		})
	}
}

// TestSiblingGateSkippedLegNeedsNoSibling holds the other half: a leg named
// in SCHEMA_SKIP_LEGS is not run by `conformance`, so it does not need its
// sibling (here the C leg, with the C++ oracle present).
func TestSiblingGateSkippedLegNeedsNoSibling(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	absent := filepath.Join(t.TempDir(), "absent")
	oracle := filepath.Join(t.TempDir(), "serialize")
	cmd := exec.Command("make", "-n", "conformance", "SERIALIZE="+oracle,
		"SERIALIZE_C="+absent, "SCHEMA_SKIP_LEGS=c,cs,unity,go,rust,js")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	// the C++ oracle is still absent here, so the gate must name it and not
	// the skipped legs
	if err == nil {
		t.Fatalf("make conformance with the C++ oracle absent succeeded:\n%s", out)
	}
	text := string(out)
	if !strings.Contains(text, "the cpp leg needs the sibling checkout") || strings.Contains(text, "the c leg") {
		t.Fatalf("the gate named the wrong leg:\n%s", text)
	}
}
