package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wireFuzzSeedRows are a whole stream and a CI shard of each of the Python
// and Ruby wire-fuzz make rows.
var wireFuzzSeedRows = []struct{ row, leg string }{
	{"python-wirefuzz-current-normal-plain", "python"},
	{"python-wirefuzz-floor-dev-retain-shard-7", "python"},
	{"ruby-wirefuzz-current-mixed", "ruby"},
	{"ruby-wirefuzz-floor-retain-shard-3", "ruby"},
}

var wireFuzzSeedArg = regexp.MustCompile(`--seed (\S+)`)

// wireFuzzSeedIssues dry-runs each row in dir with the run's seed handed in
// and the Makefile's fixed SEED set apart from it, and names each row that
// does not run its random pass at the run's seed, or does not print the seed
// and the line that re-runs it. make -o leaves the rows' builds alone.
func wireFuzzSeedIssues(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	const seed = "20261125"
	var issues []string
	for _, r := range wireFuzzSeedRows {
		a := append(append([]string{}, args...), "-n",
			"-o", "build/conformance-harness", "-o", "build/conformance-"+r.leg, "-o", r.leg+"-toolchain-check",
			r.row, "SCHEMA_TEST_SEED="+seed, "SEED=1")
		out, err := runMakeIn(t, dir, a...)
		if err != nil {
			t.Fatalf("make -n %s: %v\n%s", r.row, err, out)
		}
		var got []string
		for _, m := range wireFuzzSeedArg.FindAllStringSubmatch(out, -1) {
			got = append(got, m[1])
		}
		if len(got) != 1 || got[0] != seed {
			issues = append(issues, fmt.Sprintf("%s runs at seed %v, not the run's seed %s", r.row, got, seed))
		}
		if want := fmt.Sprintf("%s: seed %s (a red re-runs with: make %s SCHEMA_TEST_SEED=%s)", r.row, seed, r.row, seed); !strings.Contains(out, want) {
			issues = append(issues, fmt.Sprintf("%s does not print %q", r.row, want))
		}
	}
	return issues
}

// TestPythonAndRubyWireFuzzRowsTakeTheRunsSeed: every Python and Ruby
// wire-fuzz make row, whole stream or CI shard, runs at the run's seed
// (SCHEMA_TEST_SEED; make/wire-fuzz-rows.mk, WIRE_FUZZ_RUN_SEED), never the
// Makefile's fixed SEED, and prints the seed with the line that re-runs a red
// by name.
func TestPythonAndRubyWireFuzzRowsTakeTheRunsSeed(t *testing.T) {
	t.Parallel()
	for _, issue := range wireFuzzSeedIssues(t, repoRoot(t)) {
		t.Error(issue)
	}
}

// TestAWireFuzzRowOnTheFixedSeedIsNamed: the check above is red, by row, on
// a copy of the make rows that runs the Makefile's fixed SEED again.
func TestAWireFuzzRowOnTheFixedSeedIsNamed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "make/ruby-wirefuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "--seed $$(WIRE_FUZZ_RUN_SEED) --n $(N) --failed"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the plant's anchor %q is not once in make/ruby-wirefuzz.mk", anchor)
	}
	planted := filepath.Join(t.TempDir(), "ruby-wirefuzz.mk")
	if err := os.WriteFile(planted, []byte(strings.Replace(string(src), anchor, "--seed $(SEED) --n $(N) --failed", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	// the Makefile, then the planted fragment redefining its ruby rows
	issues := wireFuzzSeedIssues(t, root, "-f", "Makefile", "-f", planted)
	if len(issues) != 1 || !strings.Contains(issues[0], "ruby-wirefuzz-current-mixed runs at seed [1]") {
		t.Errorf("want ruby-wirefuzz-current-mixed named alone, got %q", issues)
	}
}
