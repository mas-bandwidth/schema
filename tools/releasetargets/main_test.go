package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// dbControl is make's printed database, as GNU Make 4.4.1 prints it, for the
// shapes the planner reads: a define block whose body looks like a rule, a
// `+=` list printed expanded, a prerequisite printed under "# Not a target:",
// a target-specific variable, a rule spelled `name :` in the source (printed
// `name:`), and the leg rules a foreach/eval template made.
const dbControl = `# Variables

# makefile (from 'Makefile', line 3)
WIRE_FUZZ_RELEASE_LEGS := c go
# makefile (from 'Makefile', line 4)
define wfr_leg
tables-wire-fuzz-release-$(1):
	@echo $(1)
endef
# makefile (from 'Makefile', line 13)
R = $(L) z

# Files

tables-go-release: a
#  Implicit rule search has not been done.
#  recipe to execute (from 'Makefile', line 9):
	@echo

# Not a target:
tables-rust-release:
#  Implicit rule search has not been done.

# makefile (from 'Makefile', line 12)
tables-lua-release: V := 1
# Not a target:
tables-lua-release:
#  Implicit rule search has not been done.

tables-c-release:
#  Phony target (prerequisite of .PHONY).

tables-wire-fuzz-release-go:
#  Implicit rule search has not been done.

tables-wire-fuzz-release-c:
#  Implicit rule search has not been done.

tables-wire-fuzz-release-negative-control: build/conformance-harness
#  Implicit rule search has not been done.

tables-cs-wire-fuzz:: build-conformance-cs
#  Implicit rule search has not been done.
`

func mustPlan(t *testing.T, db string, also ...string) ([]string, error) {
	t.Helper()
	parsed, err := parseDatabase(strings.NewReader(db))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return plan(parsed, also)
}

// TestPlanReadsTheExpandedRules: the plan is make's rules, not a spelling.
func TestPlanReadsTheExpandedRules(t *testing.T) {
	t.Parallel()
	got, err := mustPlan(t, dbControl, "tables-cs-wire-fuzz")
	if err != nil {
		t.Fatalf("refused the control: %v", err)
	}
	want := []string{"tables-c-release", "tables-cs-wire-fuzz", "tables-go-release", "tables-wire-fuzz-release-c", "tables-wire-fuzz-release-go"}
	if !slices.Equal(got, want) {
		t.Fatalf("planned %v, want %v (a define body, a `# Not a target:` prerequisite and a target-specific variable are not rules; the negative-control rule is not a leg)", got, want)
	}
}

// TestPlanRefusesByName: every way the plan could hold less than the release
// set refuses, and names what it could not plan.
func TestPlanRefusesByName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		db   string
		also []string
		want string
	}{
		{
			name: "a leg with no rule (printed only as a prerequisite)",
			db:   strings.Replace(dbControl, "WIRE_FUZZ_RELEASE_LEGS := c go", "WIRE_FUZZ_RELEASE_LEGS := c go rust", 1) + "\n# Not a target:\ntables-wire-fuzz-release-rust:\n",
			want: "make defines no rule tables-wire-fuzz-release-rust",
		},
		{
			name: "an -also target with no rule",
			db:   dbControl,
			also: []string{"tables-dart-wire-fuzz"},
			want: "the workflow plans tables-dart-wire-fuzz, and make defines no rule",
		},
		{
			name: "no leg list",
			db:   strings.Replace(dbControl, "WIRE_FUZZ_RELEASE_LEGS := c go\n", "", 1),
			want: "WIRE_FUZZ_RELEASE_LEGS is not defined",
		},
		{
			name: "a recursive leg list make did not expand",
			db:   strings.Replace(dbControl, "WIRE_FUZZ_RELEASE_LEGS := c go", "WIRE_FUZZ_RELEASE_LEGS = $(LEGS)", 1),
			want: "holds an unexpanded reference",
		},
		{
			name: "an empty leg list",
			db:   strings.Replace(dbControl, "WIRE_FUZZ_RELEASE_LEGS := c go", "WIRE_FUZZ_RELEASE_LEGS := ", 1),
			want: "WIRE_FUZZ_RELEASE_LEGS is empty",
		},
		{
			name: "no tables-<lang>-release rule at all",
			db:   strings.NewReplacer("tables-go-release: a\n", "", "tables-c-release:\n", "").Replace(dbControl),
			want: "make defines no tables-<lang>-release rule",
		},
	} {
		got, err := mustPlan(t, tc.db, tc.also...)
		if err == nil {
			t.Errorf("%s: planned %v, want a refusal containing %q", tc.name, got, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refused with %q, want it to name %q", tc.name, err, tc.want)
		}
	}
}

// legacyPlan is the planner certify.yml ran before F-066, transcribed: a grep
// of the make SOURCE for '^tables-[a-z0-9]+-release:', the first
// `WIRE_FUZZ_RELEASE_LEGS :=` line read with sed, and the explicit names added
// unchecked. It is kept here only to prove the committed fixtures defeat it.
func legacyPlan(src string, also []string) []string {
	rule := regexp.MustCompile(`(?m)^(tables-[a-z0-9]+-release):`)
	legs := regexp.MustCompile(`(?m)^WIRE_FUZZ_RELEASE_LEGS *:= *(.*)$`)
	set := map[string]bool{}
	for _, m := range rule.FindAllStringSubmatch(src, -1) {
		set[m[1]] = true
	}
	for _, m := range legs.FindAllStringSubmatch(src, -1) {
		for leg := range strings.SplitSeq(m[1], " ") {
			set[wireFuzzPrefix+leg] = true
		}
	}
	for _, a := range also {
		set[a] = true
	}
	var out []string
	for name := range set {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// TestF066FixturesDefeatTheSourceScrape is F-066's regression, on the
// committed corpus (testdata/f066, which certify.yml replays through the real
// make before it plans): each fixture carries a parseable want, the control
// is planned the same by the old scrape and the want, and each case the
// reviewer found makes the old scrape plan less than the want. The fixtures'
// real make databases are checked by `-fixtures` in certify.yml (no make runs
// in a unit test).
func TestF066FixturesDefeatTheSourceScrape(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "f066")
	files, err := filepath.Glob(filepath.Join(dir, "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no fixtures in %s: the regression would check nothing", dir)
	}
	defeats := map[string]bool{"space-before-colon.mk": true, "split-assignment.mk": true}
	seen := map[string]bool{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		want, refuse, err := fixtureWant(src)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		base := filepath.Base(f)
		seen[base] = true
		if refuse != "" {
			if !strings.HasPrefix(base, "refuse-") {
				t.Errorf("%s wants a refusal and is not named refuse-*", base)
			}
			continue
		}
		old := legacyPlan(string(src), fixtureAlso)
		switch {
		case defeats[base] && slices.Equal(old, want):
			t.Errorf("%s: the old source scrape plans %v, the same as its want: the fixture no longer reproduces F-066", base, old)
		case !defeats[base] && !slices.Equal(old, want):
			t.Errorf("%s (a control): the old scrape plans %v, want %v", base, old, want)
		}
	}
	for _, need := range []string{"control.mk", "space-before-colon.mk", "split-assignment.mk", "refuse-leg-without-rule.mk", "refuse-missing-also.mk"} {
		if !seen[need] {
			t.Errorf("the F-066 corpus lost %s", need)
		}
	}
}

// TestCheckFixtureHoldsThePlanToTheWant: the fixture check itself goes red on
// a plan that differs from the want, and on a refusal that names another
// target (its negative control).
func TestCheckFixtureHoldsThePlanToTheWant(t *testing.T) {
	t.Parallel()
	good := []byte("# want: tables-c-release tables-cs-wire-fuzz tables-go-release\n#       tables-wire-fuzz-release-c tables-wire-fuzz-release-go\n")
	if err := checkFixture([]byte(dbControl), good, fixtureAlso); err != nil {
		t.Fatalf("control: %v", err)
	}
	short := []byte("# want: tables-c-release tables-cs-wire-fuzz tables-go-release\n")
	if err := checkFixture([]byte(dbControl), short, fixtureAlso); err == nil {
		t.Error("NEGATIVE CONTROL FAILED: a plan larger than the want passed")
	}
	wrongRefuse := []byte("# want: refuse tables-wire-fuzz-release-rust\n")
	if err := checkFixture([]byte(dbControl), wrongRefuse, fixtureAlso); err == nil {
		t.Error("NEGATIVE CONTROL FAILED: a want of refusal passed on a plan that does not refuse")
	}
}
