package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// readinessEveryTarget is the coverage gate's own list of the 18 targets
// (test/conformance/harness/coverage_complete.go, everyTarget): the list the
// nightly coverage job requires a readiness receipt for.
func readinessEveryTarget(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "test/conformance/harness/coverage_complete.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`var everyTarget = \[\]string\{([^}]*)\}`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("coverage_complete.go declares no everyTarget list")
	}
	var targets []string
	for _, q := range regexp.MustCompile(`"([a-z]+)"`).FindAllSubmatch(m[1], -1) {
		targets = append(targets, string(q[1]))
	}
	if len(targets) != 18 {
		t.Fatalf("everyTarget names %d targets, want 18: %v", len(targets), targets)
	}
	return targets
}

// pythonTableKeys returns the quoted keys of the dict literal NAME = {...} in
// the readiness script: every `"key":` between the opening brace and the
// closing brace at the start of a line.
func pythonTableKeys(t *testing.T, script, name string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(script, "\n"+name+" = {\n")
	if !ok {
		t.Fatalf("the readiness script declares no %s table", name)
	}
	body, _, ok := strings.Cut(rest, "\n}\n")
	if !ok {
		t.Fatalf("the readiness script's %s table does not close", name)
	}
	var keys []string
	for _, q := range regexp.MustCompile(`(?m)(?:^\s*|,\s*)"([a-z]+)":`).FindAllStringSubmatch(body, -1) {
		keys = append(keys, q[1])
	}
	return keys
}

// Every target owes parity (2026-11-21: "100% parity ... all languages,
// always"): the readiness script accepts every one of the 18 targets the
// coverage gate requires a receipt for, and each has its packet/table
// packages, its wire-fuzz driver build, its wire and retention controls and
// its external gates. A target missing from any of them is a receipt the
// nightly can never write, so it fails here, in the default build.
func TestNightlyReadinessScriptCoversEveryTarget(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "test/conformance/nightly-readiness"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	m := regexp.MustCompile(`(?s)\nLANGUAGES = \(([^)]*)\)`).FindStringSubmatch(script)
	if m == nil {
		t.Fatal("the readiness script declares no LANGUAGES tuple")
	}
	var languages []string
	for _, q := range regexp.MustCompile(`"([a-z]+)"`).FindAllStringSubmatch(m[1], -1) {
		languages = append(languages, q[1])
	}
	every := readinessEveryTarget(t)
	for _, lang := range every {
		if !slices.Contains(languages, lang) {
			t.Errorf("test/conformance/nightly-readiness refuses %s: its readiness receipt can never pass", lang)
		}
	}
	for _, lang := range languages {
		if !slices.Contains(every, lang) {
			t.Errorf("test/conformance/nightly-readiness accepts %s, which is not a target", lang)
		}
	}
	for _, table := range []string{"PACKAGES", "DRIVER_BUILD", "WIRE_CONTROLS", "FIXED_GATES"} {
		keys := pythonTableKeys(t, script, table)
		for _, lang := range every {
			if !slices.Contains(keys, lang) {
				t.Errorf("the readiness script's %s table has no entry for %s", table, lang)
			}
		}
	}
	for _, stale := range []string{"original nine", "original-nine", "expansion language"} {
		if strings.Contains(strings.ToLower(script), stale) {
			t.Errorf("the readiness script still scopes acceptance by %q", stale)
		}
	}
	page, err := os.ReadFile(filepath.Join(repoRoot(t), "test/conformance/nightly-readiness.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"original nine", "original-nine", "expansion-language", "expansion language", "past the original"} {
		if strings.Contains(strings.ToLower(string(page)), stale) {
			t.Errorf("nightly-readiness.md still scopes acceptance by %q", stale)
		}
	}
}

// The readiness step is the same for every port leg, and the registry's port
// matrix is every target but the C++ reference, which runs its readiness in
// cpp-writes: a leg dropped from the registry would never write its receipt.
func TestNightlyReadinessRegistryHasEveryPortLeg(t *testing.T) {
	t.Parallel()
	for _, lang := range readinessEveryTarget(t) {
		if lang == "cpp" {
			continue
		}
		if _, err := os.Stat(filepath.Join(repoRoot(t), "test/conformance", lang, "ci.json")); err != nil {
			t.Errorf("%s has no test/conformance/%s/ci.json, so the nightly port matrix never runs its readiness gate: %v", lang, lang, err)
		}
	}
}
