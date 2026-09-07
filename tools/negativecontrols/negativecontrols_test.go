package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryDefinedControlIsInThePlan is the rule the pull-request leg rests on.
// The leg's matrix comes out of make/negative-controls.json, and this test holds
// that file against the Makefile and its includes: a control the makefiles define
// and the plan does not carry fails here, and so does a control the plan names
// and no makefile defines. Adding a negative control therefore costs one line in
// the plan, and forgetting that line is a red test rather than a control that
// runs nowhere.
func TestEveryDefinedControlIsInThePlan(t *testing.T) {
	root := testRoot(t)
	defs, err := enumerate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) == 0 {
		t.Fatal("no negative-control targets found: this test would pass over an empty set")
	}
	m, err := loadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	missing, stale, err := reconcile(defs, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range missing {
		t.Errorf("%s is defined and the pull-request leg does not run it: add it to a group in %s, or to the exclusion list with a reason", target, manifestPath)
	}
	for _, target := range stale {
		t.Errorf("%s is named in %s and no makefile defines it: drop the line", target, manifestPath)
	}
	t.Logf("%d negative controls, all of them in a group or in an explained exclusion", len(defs))
}

// TestEveryExclusionCarriesAReason keeps the exclusion list from becoming a
// silent skip. A control leaves the leg only where somebody wrote down why.
func TestEveryExclusionCarriesAReason(t *testing.T) {
	m, err := loadManifest(testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Skipped {
		if strings.TrimSpace(e.Reason) == "" {
			t.Errorf("%s is excluded from the leg with no reason", e.Target)
		}
	}
}

// TestGroupsAreNamedAndPopulated catches the two shapes of a plan that would
// expand into a matrix nothing runs: an unnamed row, and an empty one.
func TestGroupsAreNamedAndPopulated(t *testing.T) {
	m, err := loadManifest(testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) == 0 {
		t.Fatal("the plan carries no groups, so the leg's matrix is empty")
	}
	names := map[string]bool{}
	for _, g := range m.Groups {
		if strings.TrimSpace(g.Name) == "" {
			t.Error("a group has no name, so its matrix row has no job name")
		}
		if names[g.Name] {
			t.Errorf("two groups are named %q", g.Name)
		}
		names[g.Name] = true
		if len(g.Targets) == 0 {
			t.Errorf("group %q runs no targets", g.Name)
		}
	}
}

// TestEveryGroupRunsInATier is the owner's rule made mechanical. CI that runs
// per commit finishes inside one to two minutes, so a group either fits that
// budget and rides the pull request, or it does not and runs nightly. A group
// that names neither tier appears in no workflow's matrix, which is a control
// running nowhere with a plan entry that looks like coverage.
func TestEveryGroupRunsInATier(t *testing.T) {
	m, err := loadManifest(testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range m.tiers() {
		t.Errorf("group %s runs on neither the pull request nor the nightly: name %q or %q", bad, whenPullRequest, whenNightly)
	}
	for _, when := range []string{whenPullRequest, whenNightly} {
		out, err := m.matrix(when)
		if err != nil {
			t.Fatalf("the %s matrix does not render: %v", when, err)
		}
		if strings.Contains(string(out), `"include":[]`) || strings.Contains(string(out), `"include":null`) {
			t.Errorf("the %s matrix is empty, so its workflow expands to no job", when)
		}
	}
}

// TestTheLegRunsTheManifestAndNotATypedList closes the loop between this
// package and the workflows. The enumeration is only worth its cost while a
// leg's target list IS the manifest: a job that typed its own list would pass
// every test above and still miss a control. So both workflows are read here,
// and each must reach its matrix and its targets through this tool: ci.yml for
// the pull-request tier, certify.yml for the nightly one, which is where this
// repository's schedule lives.
func TestTheLegRunsTheManifestAndNotATypedList(t *testing.T) {
	root := testRoot(t)
	for workflow, wants := range map[string][]string{
		"ci.yml": {
			"go run ./tools/negativecontrols check",
			"go run ./tools/negativecontrols matrix",
			"go run ./tools/negativecontrols targets",
		},
		"certify.yml": {
			"go run ./tools/negativecontrols matrix nightly",
			"go run ./tools/negativecontrols targets",
		},
	} {
		body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", workflow))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(body), want) {
				t.Errorf(".github/workflows/%s does not run %q, so that tier's target list is not the one this package enumerates", workflow, want)
			}
		}
	}
	m, err := loadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	// A group name is a matrix value, so it may not carry shell metacharacters
	// or whitespace: the leg passes it to the tool as one word.
	for _, g := range m.Groups {
		if strings.ContainsAny(g.Name, " \t\"'$`;&|<>()") {
			t.Errorf("group name %q is not a single shell-safe word", g.Name)
		}
	}
}

// TestTargetsInReadsTheRuleHeadsAMakefileWrites pins the parser against the
// spellings this tree actually uses, each of which a simpler reader gets wrong:
// several targets on one head, a head continued over a backslash, a recipe line
// that names a target, a `define` block, and `:=` assignment.
func TestTargetsInReadsTheRuleHeadsAMakefileWrites(t *testing.T) {
	const body = `
# a comment: fake-negative-control: not a rule
NC_FLAGS := -Ifoo:bar-negative-control
.PHONY: phony-negative-control alpha-negative-control

alpha-negative-control beta-negative-control: prereq
	@echo not-a-negative-control-target

gamma-negative-control \
	delta-negative-control: prereq
	@echo two

define A_MACRO
inside-negative-control: nothing
endef

$(GENERATED)-negative-control: prereq
	@echo generated

pattern-%-negative-control: prereq
	@echo pattern

double-negative-control:: prereq
	@echo double

VAR_WITH_COLON = a:b-negative-control
`
	got := map[string]bool{}
	for _, target := range targetsIn(body) {
		got[target] = true
	}
	for _, want := range []string{
		"phony-negative-control", "alpha-negative-control", "beta-negative-control",
		"gamma-negative-control", "delta-negative-control", "double-negative-control",
	} {
		if !got[want] {
			t.Errorf("targetsIn missed %s", want)
		}
	}
	for _, unwanted := range []string{
		"not-a-negative-control-target", "inside-negative-control",
		"$(GENERATED)-negative-control", "pattern-%-negative-control",
		"a:b-negative-control", "-Ifoo:bar-negative-control",
	} {
		if got[unwanted] {
			t.Errorf("targetsIn invented %s", unwanted)
		}
	}
}

// TestEnumerateReadsEveryIncludedFile proves the reader follows the Makefile's
// own include lines rather than a glob typed into this package: every file the
// tree includes contributes, and the per-language includes are where most of
// the toolchain-bound controls live.
func TestEnumerateReadsEveryIncludedFile(t *testing.T) {
	root := testRoot(t)
	files, err := makefileSet(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		seen[rel] = true
	}
	for _, want := range []string{"Makefile", "make/js.mk", "make/checks/packet-arm-defaults.mk"} {
		if !seen[want] {
			t.Errorf("the include set does not carry %s", want)
		}
	}
}

// testRoot is the tree under test: the package runs from its own directory, and
// the makefiles it reads are the repository's.
func testRoot(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}
