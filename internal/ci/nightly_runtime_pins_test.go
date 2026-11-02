package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// Every optional registered target must be linked into the nightly harness.
// Otherwise a freshly installed port fails its claims before exercising code.
func TestNightlyHarnessIncludesEveryOptionalTarget(t *testing.T) {
	root := repoRoot(t)
	files := workflows(t, root)
	row := regexp.MustCompile(`(?m)^  HARNESS_TAGS: ([^\n]+)$`).FindStringSubmatch(files["nightly-conformance.yml"])
	if len(row) != 2 {
		t.Fatal("nightly HARNESS_TAGS missing")
	}
	tags := map[string]bool{}
	for tag := range strings.SplitSeq(strings.TrimSpace(row[1]), ",") {
		tags[tag] = true
	}
	paths, err := filepath.Glob(filepath.Join(root, "compiler", "carries_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	optional := regexp.MustCompile(`schema_leg_[a-z]+`)
	checked := map[string]bool{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(string(b), "\n")
		if !strings.HasPrefix(first, "//go:build ") {
			continue
		}
		for _, tag := range optional.FindAllString(first, -1) {
			checked[tag] = true
			if !tags[tag] {
				t.Errorf("nightly harness omits %s required by %s", tag, filepath.Base(path))
			}
		}
	}
	if len(checked) == 0 {
		t.Fatal("no optional target registrations checked")
	}
}
