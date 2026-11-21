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

// Every registered target a nightly row runs must be linked into the nightly
// harness. Otherwise a freshly installed port fails its claims before
// exercising code. Every shipped target is built in (Glenn, 2026-11-21: "all
// languages, by default, all the time."), so the nightly harness is a plain
// build with no tag list: its build line carries no -tags, no workflow
// defines HARNESS_TAGS, and a tagged carries registration under compiler/ or
// tagged `implementation --lang` admission under test/conformance/harness/ (a
// `schema new-leg` leg still being built) must not belong to a language with a
// nightly row (test/conformance/<lang>/ci.json, which `harness matrix --ports`
// turns into the nightly matrix), because the plain build would not carry it.
func TestNightlyHarnessIncludesEveryOptionalTarget(t *testing.T) {
	root := repoRoot(t)
	files := workflows(t, root)
	build := regexp.MustCompile(`(?m)^\s*go build ([^\n]*)-o build/nightly-harness \./test/conformance/harness\s*$`).FindStringSubmatch(files["nightly-conformance.yml"])
	if len(build) != 2 {
		t.Fatal("nightly-conformance.yml builds no build/nightly-harness from ./test/conformance/harness")
	}
	if strings.TrimSpace(build[1]) != "" {
		t.Errorf("the nightly harness is built with %q; it is a plain build, every shipped target built in", strings.TrimSpace(build[1]))
	}
	for name, body := range files {
		if strings.Contains(body, "HARNESS_TAGS") {
			t.Errorf("%s names HARNESS_TAGS; the nightly harness takes no tag list", name)
		}
	}
	ports, err := filepath.Glob(filepath.Join(root, "test", "conformance", "*", "ci.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) < 17 {
		t.Fatalf("found %d nightly ports (test/conformance/*/ci.json), want the 17 port legs at least", len(ports))
	}
	nightly := map[string]bool{}
	for _, port := range ports {
		nightly[filepath.Base(filepath.Dir(port))] = true
	}
	paths, err := filepath.Glob(filepath.Join(root, "compiler", "carries_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	admissions, err := filepath.Glob(filepath.Join(root, "test", "conformance", "harness", "implementation_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, admissions...)
	if len(paths) == 0 {
		t.Fatal("no carries registration or harness admission file found")
	}
	optional := regexp.MustCompile(`schema_leg_([a-z0-9]+)`)
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
		for _, m := range optional.FindAllStringSubmatch(first, -1) {
			if nightly[m[1]] {
				t.Errorf("%s registers %s behind a build tag, and %s has a nightly row "+
					"(test/conformance/%s/ci.json) whose plain harness would not carry it", filepath.Base(path), m[1], m[1], m[1])
			}
		}
	}
}
