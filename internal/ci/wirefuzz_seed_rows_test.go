package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wireFuzzSeedRows are whole streams and CI shards of the Python and Ruby
// wire-fuzz make rows. A whole stream runs at the run's seed; a shard is the
// certify gate's fixed partition and runs at the Makefile's SEED.
var wireFuzzSeedRows = []struct {
	row, leg string
	shard    bool
}{
	{"python-wirefuzz-current-normal-plain", "python", false},
	{"python-wirefuzz-floor-dev-retain-shard-7", "python", true},
	{"ruby-wirefuzz-current-mixed", "ruby", false},
	{"ruby-wirefuzz-floor-retain-shard-3", "ruby", true},
}

var wireFuzzSeedArg = regexp.MustCompile(`--seed (\S+)`)

// wireFuzzSeedOf dry-runs row in dir with vars on the command line (make -o
// leaves its builds alone) and returns the one seed it runs at, and the dry
// run's text.
func wireFuzzSeedOf(t *testing.T, dir, row, leg string, args, vars []string) (string, string) {
	t.Helper()
	a := append(append([]string{}, args...), "-n",
		"-o", "build/conformance-harness", "-o", "build/conformance-"+leg, "-o", leg+"-toolchain-check", row)
	out, err := runMakeIn(t, dir, append(a, vars...)...)
	if err != nil {
		t.Fatalf("make -n %s %v: %v\n%s", row, vars, err, out)
	}
	var got []string
	for _, m := range wireFuzzSeedArg.FindAllStringSubmatch(out, -1) {
		got = append(got, m[1])
	}
	if len(got) != 1 {
		return fmt.Sprint(got), out
	}
	return got[0], out
}

// wireFuzzSeedIssues names each row that breaks the seed rules, in dir:
//   - with the run's seed handed in (SCHEMA_TEST_SEED) and no SEED given, a
//     whole stream runs at the run's seed and prints it with the line that
//     re-runs a red; a shard runs at the Makefile's SEED, never the run's;
//   - with SEED given on the command line, every row runs at it: a SEED the
//     caller names is never silently ignored.
func wireFuzzSeedIssues(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	const runSeed, given = "20261125", "5"
	var issues []string
	for _, r := range wireFuzzSeedRows {
		got, out := wireFuzzSeedOf(t, dir, r.row, r.leg, args, []string{"SCHEMA_TEST_SEED=" + runSeed})
		switch {
		case r.shard && (got == runSeed || !regexp.MustCompile(`^[0-9]+$`).MatchString(got)):
			issues = append(issues, fmt.Sprintf("%s, a certify shard, runs at seed %s with the run's seed %s handed in, not the Makefile's SEED", r.row, got, runSeed))
		case !r.shard && got != runSeed:
			issues = append(issues, fmt.Sprintf("%s runs at seed %s, not the run's seed %s", r.row, got, runSeed))
		case !r.shard:
			if want := fmt.Sprintf("%s: seed %s (a red re-runs with: make %s SCHEMA_TEST_SEED=%s)", r.row, runSeed, r.row, runSeed); !strings.Contains(out, want) {
				issues = append(issues, fmt.Sprintf("%s does not print %q", r.row, want))
			}
		}
		if got, _ := wireFuzzSeedOf(t, dir, r.row, r.leg, args, []string{"SCHEMA_TEST_SEED=" + runSeed, "SEED=" + given}); got != given {
			issues = append(issues, fmt.Sprintf("%s ignores SEED=%s on the command line: runs at seed %s", r.row, given, got))
		}
	}
	return issues
}

// TestPythonAndRubyWireFuzzRowsTakeTheRunsSeed: every Python and Ruby
// whole-stream wire-fuzz make row runs at the run's seed (SCHEMA_TEST_SEED;
// make/wire-fuzz-rows.mk, WIRE_FUZZ_RUN_SEED) and prints it with the line
// that re-runs a red by name; every CI shard stays on the Makefile's SEED, so
// certify's verdict at a sha never moves with the date; and a SEED given on
// the command line wins everywhere.
func TestPythonAndRubyWireFuzzRowsTakeTheRunsSeed(t *testing.T) {
	t.Parallel()
	for _, issue := range wireFuzzSeedIssues(t, repoRoot(t)) {
		t.Error(issue)
	}
}

// TestAWireFuzzRowOnTheWrongSeedIsNamed: the check above is red, by row, on
// a copy of the Ruby rows with one rule broken: a whole stream back on the
// Makefile's SEED, a shard on the run's seed, and a whole stream that ignores
// SEED on the command line.
func TestAWireFuzzRowOnTheWrongSeedIsNamed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "make/ruby-wirefuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := os.ReadFile(filepath.Join(root, "make/wire-fuzz-rows.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, file, anchor, with, says string
		n                              int // issues: a rows plant breaks both legs' streams
	}{
		{"stream on SEED", "ruby", "--seed $$(WIRE_FUZZ_RUN_SEED) --n $(N) --failed", "--seed $(SEED) --n $(N) --failed",
			"ruby-wirefuzz-current-mixed runs at seed 24845619678, not the run's seed", 1},
		{"shard on the run's seed", "ruby", "--seed $(SEED) --n $(N) --shard", "--seed $$(WIRE_FUZZ_RUN_SEED) --n $(N) --shard",
			"ruby-wirefuzz-floor-retain-shard-3, a certify shard, runs at seed 20261125", 1},
		{"SEED ignored", "rows", "$(if $(filter command line,$(origin SEED)),$(SEED),$(or $(SCHEMA_TEST_SEED),$(shell date -u +%Y%m%d)))",
			"$(or $(SCHEMA_TEST_SEED),$(shell date -u +%Y%m%d))", "ruby-wirefuzz-current-mixed ignores SEED=5", 2},
	} {
		body := string(src)
		if c.file == "rows" {
			body = string(rows)
		}
		if strings.Count(body, c.anchor) != 1 {
			t.Fatalf("%s: the plant's anchor %q is not once in its file", c.name, c.anchor)
		}
		planted := filepath.Join(t.TempDir(), "planted.mk")
		if err := os.WriteFile(planted, []byte(strings.Replace(body, c.anchor, c.with, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		// the Makefile, then the planted fragment redefining its rows
		issues := wireFuzzSeedIssues(t, root, "-f", "Makefile", "-f", planted)
		if len(issues) != c.n || !strings.Contains(strings.Join(issues, "\n"), c.says) {
			t.Errorf("%s: want %d issues, one saying %q, got %q", c.name, c.n, c.says, issues)
		}
	}
}
