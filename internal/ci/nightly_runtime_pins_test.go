package ci

import (
	"regexp"
	"testing"
)

// Nightly must qualify the same runtime releases as the merge and release gates.
// Its stale Rust3 pin compiled generated code against an incompatible stream API.
func TestNightlyRuntimePinsMatchMergeAndRelease(t *testing.T) {
	files := workflows(t, repoRoot(t))
	pattern := regexp.MustCompile(`(?m)^  (SERIALIZE(?:_[A-Z]+)?_TAG): ([^\s#]+)`)
	read := func(name string) map[string]string {
		t.Helper()
		pins := map[string]string{}
		for _, row := range pattern.FindAllStringSubmatch(files[name], -1) {
			pins[row[1]] = row[2]
		}
		if len(pins) != 6 {
			t.Fatalf("%s: want six runtime pins, got %v", name, pins)
		}
		return pins
	}
	want := read("ci-full.yml")
	for _, name := range []string{"ci-fast.yml", "certify.yml", "leg.yml", "nightly-conformance.yml"} {
		got := read(name)
		for runtime, pin := range want {
			if got[runtime] != pin {
				t.Errorf("%s: %s=%s, merge gate requires %s", name, runtime, got[runtime], pin)
			}
		}
	}
}
