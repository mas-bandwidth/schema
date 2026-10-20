package darttable

// THE DART LEG'S WIRE FUZZER OVER EVERY ROOT (docs/SPEC-TABLES.md §4.2,
// docs/roadmap.sexp vt/wire-fuzzer/dart).
//
// The oracle harness owns the mutants and the comparison; a leg is only the
// thin reader that binds a roster entry to a generated codec. The Card-only
// target (cards-dart-wire-fuzz) names a manifest, so it exercises one root.
// tables-dart-wire-fuzz-all names NO manifest, exactly as the repository's own
// tables-wire-fuzz does, so the harness draws every instance and every report
// case from its default corpus. This test holds the make rule to that shape,
// holds test-dart to running it, and holds the Dart driver's roster to every
// instance and report root the default manifest names.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireFuzzAllTargetDart(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")

	data, err := os.ReadFile(filepath.Join(repo, "make", "dart.mk"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(data)

	block := makeRule(makefile, "tables-dart-wire-fuzz-all")
	if block == "" {
		t.Fatal("make/dart.mk has no tables-dart-wire-fuzz-all rule")
	}
	// THE TARGET IS THE ALL-ROOTS TARGET: no --manifest, the harness's default
	// corpus, the Dart driver, and the three claims cards-dart-wire-fuzz pins.
	for _, want := range []string{
		"build/conformance-harness",
		"build/conformance-dart",
		"wire-fuzz",
		"--driver 'build/conformance-dart wire-fuzz'",
		"grep -q '0 divergences'",
		"grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots'",
		"grep -qE '[1-9][0-9]* mutants'",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("tables-dart-wire-fuzz-all does not hold %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "--manifest") {
		t.Errorf("tables-dart-wire-fuzz-all names a --manifest; it must fuzz the harness's default corpus (every root):\n%s", block)
	}
	if !makeTestDartRuns(makefile, "tables-dart-wire-fuzz-all") {
		t.Error("test-dart does not run tables-dart-wire-fuzz-all")
	}

	// THE ROSTER BINDS EVERY ROOT THE DEFAULT MANIFEST NAMES. An instance or a
	// report row is a form-1 root, so the driver's fileRoots table must carry
	// it; a root the leg carries but the roster refuses would be counted
	// absent and never fuzzed.
	driver, err := os.ReadFile(filepath.Join(repo, "test", "conformance", "dart", "main.dart"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, raw := range strings.Split(string(manifest), "\n") {
		f := strings.Fields(raw)
		if len(f) < 4 || (f[0] != "instance" && f[0] != "report") {
			continue
		}
		key := "'" + f[2] + " " + f[3] + "': _fileRow<"
		if seen[key] {
			continue
		}
		seen[key] = true
		if !strings.Contains(string(driver), key) {
			t.Errorf("test/conformance/dart/main.dart does not bind the default manifest's form-1 root %s %s in its wire-fuzz roster", f[2], f[3])
		}
	}
	if len(seen) == 0 {
		t.Fatal("the default manifest names no form-1 root")
	}
}

// makeRule returns one target's recipe and prerequisites: the rule line and
// every following tab-indented recipe line, up to the next rule.
func makeRule(makefile, target string) string {
	lines := strings.Split(makefile, "\n")
	var b strings.Builder
	collecting := false
	for _, line := range lines {
		if !collecting {
			if strings.HasPrefix(line, target+":") {
				collecting = true
				b.WriteString(line)
				b.WriteByte('\n')
			}
			continue
		}
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// makeTestDartRuns reports whether a test-dart rule names target, either as a
// prerequisite or in a recipe's sub-make.
func makeTestDartRuns(makefile, target string) bool {
	for _, line := range strings.Split(makefile, "\n") {
		if strings.HasPrefix(line, "test-dart:") {
			for _, field := range strings.Fields(strings.TrimPrefix(line, "test-dart:")) {
				if field == target {
					return true
				}
			}
		}
		if strings.Contains(line, "$(MAKE) "+target) {
			return true
		}
	}
	return false
}
