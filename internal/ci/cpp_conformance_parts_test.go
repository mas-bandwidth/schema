package ci

// THE C++ CONFORMANCE GATE RUNS IN TWO PARTS, AND THE DART FUZZ GATE ON THE
// SAME-RUN DART BUILD (2026-11-24; make/verify-compiled.mk "THE C++
// CONFORMANCE GATE IS TWO PARTS", ci-fast.yml verify-cpp-conformance and
// verify-dart-fuzz). Both were verify-leg rows that ran past two minutes on
// unloaded runners. This holds that the split drops nothing: the two parts'
// prerequisites cover every prerequisite the one-row gate had, the gate is
// both parts, and ci-fast.yml runs each part and the whole fuzz target on
// every pull request that selects the leg.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// cppConformanceWhole is what `_verify-cpp-conformance` built and ran as one
// row before the split, at c1e5f93a, less conformance-rows-cpp (the C++ rows
// run in ci-fast.yml's conformance-rows job, #4920) and less the corpus run
// (`conformance-harness run --only cpp --claims`), which test registry L4
// retired: the schema-test corpus adapter answers those cases, row for row
// (docs/TEST-REGISTRY.md).
var cppConformanceWhole = []string{
	"build/conformance-harness", "build/conformance-cpp", "build/schema_test_cook", "build/schema_test",
	"build/schema_test_ludicrous", "packet-defaults-cpp", "packet-void-cpp", "table-base64-cpp",
	"build/packet-text/cpp/driver", "build/packet-text/harness", "build/schema_test_tables",
	"./build/schema_test", "./build/schema_test_tables", "./build/schema_test_ludicrous",
	"./build/packet-text/harness ./build/packet-text/cpp/driver",
}

// makeRule returns a rule's prerequisites and recipe lines.
func makeRule(mk, target string) ([]string, []string, bool) {
	lines := strings.Split(mk, "\n")
	for i, line := range lines {
		rest, ok := strings.CutPrefix(line, target+":")
		if !ok {
			continue
		}
		var recipe []string
		for _, r := range lines[i+1:] {
			if !strings.HasPrefix(r, "\t") {
				break
			}
			recipe = append(recipe, strings.TrimSpace(strings.TrimPrefix(r, "\t")))
		}
		return strings.Fields(rest), recipe, true
	}
	return nil, nil, false
}

func cppPartsErrors(mk, fast string) []string {
	var errs []string
	whole, _, ok := makeRule(mk, "_verify-cpp-conformance")
	if !ok || !slices.Equal(whole, []string{"_verify-cpp-conformance-1", "_verify-cpp-conformance-2"}) {
		errs = append(errs, fmt.Sprintf("_verify-cpp-conformance must be exactly its two parts, got %v", whole))
	}
	var covered []string
	for _, part := range []string{"_verify-cpp-conformance-1", "_verify-cpp-conformance-2"} {
		pre, recipe, ok := makeRule(mk, part)
		if !ok {
			errs = append(errs, part+" has no rule")
			continue
		}
		covered = append(covered, pre...)
		covered = append(covered, recipe...)
	}
	for _, want := range cppConformanceWhole {
		found := false
		for _, c := range covered {
			if c == want || strings.HasPrefix(c, want+" ") {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("neither part of the C++ conformance gate builds or runs %q: the split dropped it", want))
		}
	}
	jobs := yamlJobs(fast)
	cpp := jobs["verify-cpp-conformance"]
	if !regexp.MustCompile(`(?m)^        part: \[1, 2\]$`).MatchString(cpp) || !strings.Contains(cpp, `make "verify-cpp-conformance-${{ matrix.part }}"`) {
		errs = append(errs, "ci-fast.yml verify-cpp-conformance does not run both parts, one per row (matrix part: [1, 2], make verify-cpp-conformance-<part>)")
	}
	dart := jobs["verify-dart-fuzz"]
	if !strings.Contains(dart, "make/conformance-handoff.py consume") || !strings.Contains(dart, " _verify-dart-fuzz DART=dart") {
		errs = append(errs, "ci-fast.yml verify-dart-fuzz does not run _verify-dart-fuzz whole on the same-run Dart build")
	}
	for _, ex := range []string{"          - leg: cpp\n            gate: conformance\n", "          - leg: dart\n            gate: fuzz\n"} {
		if !strings.Contains(jobs["verify-leg"], ex) {
			errs = append(errs, fmt.Sprintf("verify-leg does not exclude %q, so it runs twice, once past two minutes", strings.Join(strings.Fields(ex), " ")))
		}
	}
	return errs
}

func TestCppConformancePartsCoverTheGate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mkData, err := os.ReadFile(filepath.Join(root, "make", "verify-compiled.mk"))
	if err != nil {
		t.Fatal(err)
	}
	fastData, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	mk, fast := string(mkData), string(fastData)
	for _, e := range cppPartsErrors(mk, fast) {
		t.Error(e)
	}

	plant := func(s, from, to string) string {
		t.Helper()
		if !strings.Contains(s, from) {
			t.Fatalf("control cannot plant: %q not found", from)
		}
		return strings.Replace(s, from, to, 1)
	}
	for _, c := range []struct {
		name     string
		mk, fast string
	}{
		{"part 1 loses schema_test_tables", plant(mk, " build/packet-text/harness build/schema_test_tables\n", " build/packet-text/harness\n"), fast},
		{"part 2 loses table-base64-cpp", plant(mk, " build/schema_test_cook table-base64-cpp\n", " build/schema_test_cook\n"), fast},
		{"the gate runs one part", plant(mk, "_verify-cpp-conformance: _verify-cpp-conformance-1 _verify-cpp-conformance-2", "_verify-cpp-conformance: _verify-cpp-conformance-1"), fast},
		{"ci-fast runs one part", mk, plant(fast, "        part: [1, 2]\n", "        part: [1]\n")},
		{"verify-leg runs cpp conformance again", mk, plant(fast, "          - leg: cpp\n            gate: conformance\n", "")},
		{"the dart fuzz job compiles instead", mk, plant(fast, "make/conformance-handoff.py consume \"${DART_CONFORMANCE_ARTIFACTS:?missing artifact directory}/dart-conformance.tar.gz\"\n          make -j\"$(nproc)\" -o build/conformance-dart _verify-dart-fuzz", "make -j\"$(nproc)\" _verify-dart-fuzz")},
	} {
		if len(cppPartsErrors(c.mk, c.fast)) == 0 {
			t.Errorf("PLANTED %q left the gate green", c.name)
		}
	}
}
