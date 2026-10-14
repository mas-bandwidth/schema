package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
// and each must reach its matrix and its targets through this tool:
// ci-full.yml for the merge tier (the 47 rows are too many runners for the
// per-push fast lane; that split is in ci-fast.yml's header), certify.yml for
// the nightly one, which is where this repository's schedule lives.
func TestTheLegRunsTheManifestAndNotATypedList(t *testing.T) {
	root := testRoot(t)
	for workflow, wants := range map[string][]string{
		"ci-full.yml": {
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

double-negative-control:: prereq
	@echo double

VAR_WITH_COLON = a:b-negative-control
`
	targets, err := targetsIn(body)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, target := range targets {
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
		"a:b-negative-control", "-Ifoo:bar-negative-control",
	} {
		if got[unwanted] {
			t.Errorf("targetsIn invented %s", unwanted)
		}
	}
}

// TestAMarkedHeadThisReaderCannotNameIsRefused is the other half of the parser
// contract. A control whose head is spelled through a variable or as a pattern
// rule has a name this reader cannot resolve, and a reader that drops such a
// head leaves those controls in no group, in no exclusion, and in no job, with
// every test in this file green. So the head is refused by name instead, which
// is the one outcome an author can act on.
func TestAMarkedHeadThisReaderCannotNameIsRefused(t *testing.T) {
	for _, body := range []string{
		"$(GENERATED)-negative-control: prereq\n\t@echo generated\n",
		"pattern-%-negative-control: prereq\n\t@echo pattern\n",
		".PHONY: $(LANGS:%=packet-%-negative-control)\n",
		"head-negative-control \\\n\t$(OTHER)-negative-control: prereq\n\t@echo continued\n",
	} {
		targets, err := targetsIn(body)
		if err == nil {
			t.Errorf("targetsIn read %q and returned %q instead of refusing a head it cannot name", body, targets)
			continue
		}
		if !strings.Contains(err.Error(), marker) {
			t.Errorf("the refusal for %q does not name the head: %v", body, err)
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

// legs are the two jobs that expand this package's plan: one per tier, each in
// the workflow that tier runs on.
var legs = []struct {
	workflow  string
	job       string
	matrixJob string
	command   string
}{
	{"ci-full.yml", "negative-controls", "negative-controls-matrix", "go run ./tools/negativecontrols matrix"},
	{"certify.yml", "negative-controls-nightly", "negative-controls-nightly-matrix", "go run ./tools/negativecontrols matrix nightly"},
}

// TestEachLegExpandsTheToolsMatrix reads the workflows as YAML rather than as
// text. The test above it asks whether the file CONTAINS the tool's commands,
// which a leg that kept the old expression in a comment and typed an include
// list beside it still satisfies. This one asks what the leg actually expands:
// its `strategy.matrix` has to BE the matrix job's output, and it has to
// `needs` that job, or the leg runs whatever somebody remembered rather than
// the plan.
func TestEachLegExpandsTheToolsMatrix(t *testing.T) {
	root := testRoot(t)
	for _, leg := range legs {
		body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", leg.workflow))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := parseWorkflow(string(body))
		if err != nil {
			t.Fatalf(".github/workflows/%s does not parse: %v", leg.workflow, err)
		}

		matrix, err := mappingAt(doc, "jobs", leg.job, "strategy", "matrix")
		if err != nil {
			t.Errorf(".github/workflows/%s: %v", leg.workflow, err)
			continue
		}
		want := fmt.Sprintf("${{ fromJSON(needs.%s.outputs.matrix) }}", leg.matrixJob)
		if matrix != want {
			t.Errorf(".github/workflows/%s: the %s job expands %#v as its matrix, want %q: a matrix written any other way is a target list this package did not enumerate", leg.workflow, leg.job, matrix, want)
		}

		needsValue, err := mappingAt(doc, "jobs", leg.job, "needs")
		if err != nil {
			t.Errorf(".github/workflows/%s: the %s job names no `needs`, so its matrix expression resolves to nothing: %v", leg.workflow, leg.job, err)
			continue
		}
		needs, err := stringsOf(needsValue)
		if err != nil {
			t.Errorf(".github/workflows/%s: the %s job's `needs` is not a job name or a list of them: %v", leg.workflow, leg.job, err)
			continue
		}
		if !slices.Contains(needs, leg.matrixJob) {
			t.Errorf(".github/workflows/%s: the %s job needs %q and not %s", leg.workflow, leg.job, needs, leg.matrixJob)
		}

		// And the other end of the same wire: the matrix job's output is a
		// step's, and that step runs this tool.
		output, err := mappingAt(doc, "jobs", leg.matrixJob, "outputs", "matrix")
		if err != nil {
			t.Errorf(".github/workflows/%s: %v", leg.workflow, err)
			continue
		}
		id, ok := stepIDOf(output)
		if !ok {
			t.Errorf(".github/workflows/%s: the %s job's matrix output is %#v, which names no step", leg.workflow, leg.matrixJob, output)
			continue
		}
		steps, err := mappingAt(doc, "jobs", leg.matrixJob, "steps")
		if err != nil {
			t.Errorf(".github/workflows/%s: %v", leg.workflow, err)
			continue
		}
		run, ok := runOfStep(steps, id)
		if !ok {
			t.Errorf(".github/workflows/%s: the %s job has no step with id %q", leg.workflow, leg.matrixJob, id)
			continue
		}
		if !strings.Contains(run, leg.command) {
			t.Errorf(".github/workflows/%s: step %q of %s does not run %q, so the matrix it publishes is not the plan's:\n%s", leg.workflow, id, leg.matrixJob, leg.command, run)
		}
	}
}

// stepIDOf reads the step id out of a `${{ steps.<id>.outputs.matrix }}`
// expression.
func stepIDOf(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), "${{ steps.")
	if !ok {
		return "", false
	}
	id, _, ok := strings.Cut(rest, ".outputs.matrix }}")
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// runOfStep finds one step of a job by its id and returns its `run` script.
func runOfStep(steps any, id string) (string, bool) {
	items, ok := steps.([]any)
	if !ok {
		return "", false
	}
	for _, item := range items {
		step, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if step["id"] != id {
			continue
		}
		run, ok := step["run"].(string)
		return run, ok
	}
	return "", false
}

// TestParseWorkflowReadsTheShapesAWorkflowWrites pins the reader against the
// spellings these two files use, each of which a line-at-a-time scan gets
// wrong: a scalar carrying an expression, a `needs` written as a list, a step
// sequence whose items are mappings, an inline comment after a value, and a
// `run:` block whose script lines look like mapping entries and sequence items
// and are neither.
func TestParseWorkflowReadsTheShapesAWorkflowWrites(t *testing.T) {
	const body = `name: Example

# a comment: not: a: mapping
on:
  schedule:
    - cron: '17 9 * * *'
  workflow_dispatch:

jobs:
  plan-job:
    runs-on: ubuntu-latest
    outputs:
      matrix: ${{ steps.plan.outputs.matrix }}
    steps:
      - uses: actions/checkout@abc123 # v7.0.1
      - id: plan
        name: the plan, as a matrix
        run: |
          matrix=$(go run ./tools/negativecontrols matrix)
          # - not: a sequence item
          if grep -q 'a: b' out; then echo "::error::no"; fi

  leg:
    needs:
      - plan-job
      - other
    strategy:
      fail-fast: false
      matrix: ${{ fromJSON(needs.plan-job.outputs.matrix) }}
`
	doc, err := parseWorkflow(body)
	if err != nil {
		t.Fatal(err)
	}
	if doc["name"] != "Example" {
		t.Errorf("the document's name is %#v", doc["name"])
	}
	matrix, err := mappingAt(doc, "jobs", "leg", "strategy", "matrix")
	if err != nil {
		t.Fatal(err)
	}
	if want := "${{ fromJSON(needs.plan-job.outputs.matrix) }}"; matrix != want {
		t.Errorf("the leg's matrix reads %#v, want %q", matrix, want)
	}
	needsValue, err := mappingAt(doc, "jobs", "leg", "needs")
	if err != nil {
		t.Fatal(err)
	}
	needs, err := stringsOf(needsValue)
	if err != nil {
		t.Fatal(err)
	}
	if len(needs) != 2 || needs[0] != "plan-job" || needs[1] != "other" {
		t.Errorf("the leg needs %q", needs)
	}
	output, err := mappingAt(doc, "jobs", "plan-job", "outputs", "matrix")
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := stepIDOf(output); !ok || id != "plan" {
		t.Errorf("the output names step %q (read %v)", id, ok)
	}
	steps, err := mappingAt(doc, "jobs", "plan-job", "steps")
	if err != nil {
		t.Fatal(err)
	}
	if items, ok := steps.([]any); !ok || len(items) != 2 {
		t.Fatalf("the plan job has %#v for steps", steps)
	}
	run, ok := runOfStep(steps, "plan")
	if !ok {
		t.Fatal("the plan step has no run script")
	}
	for _, want := range []string{"go run ./tools/negativecontrols matrix", "# - not: a sequence item", "if grep -q 'a: b' out"} {
		if !strings.Contains(run, want) {
			t.Errorf("the run script lost %q:\n%s", want, run)
		}
	}
	// A single-quoted cron value keeps its colon-free text, and a `- ` item
	// under a nested key is a sequence and not a mapping.
	schedule, err := mappingAt(doc, "on", "schedule")
	if err != nil {
		t.Fatal(err)
	}
	if items, ok := schedule.([]any); !ok || len(items) != 1 {
		t.Fatalf("the schedule reads %#v", schedule)
	}
	// A line that opens no block and follows nothing is a refusal, not a
	// silently dropped line.
	if _, err := parseWorkflow("jobs:\n  leg:\n    runs-on: x\n   stray\n"); err == nil {
		t.Error("a line at an indentation no block opened parsed clean")
	}
}

// TestToolchainPinsMatchTheConformanceRegistry holds the plan's toolchain
// versions against test/conformance/<lang>/ci.json, which is where the same
// version is written for the conformance legs. Both files install a toolchain
// for the same generated code, so a bump in one and not the other means a
// negative control runs against a runtime its own conformance leg no longer
// uses, and nothing else in this tree would say so.
func TestToolchainPinsMatchTheConformanceRegistry(t *testing.T) {
	root := testRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "test", "conformance", "*", "ci.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no test/conformance/*/ci.json: this test would pass over an empty set")
	}
	registry := map[string]string{}
	source := map[string]string{}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var entry map[string]string
		if err := json.Unmarshal(body, &entry); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		lang := filepath.Base(filepath.Dir(file))
		for _, field := range versionedToolchains {
			version := entry[field]
			if version == "" {
				continue
			}
			if held, ok := registry[field]; ok && held != version {
				t.Errorf("the registry pins %s at %s in %s and at %s in %s", field, held, source[field], version, lang)
				continue
			}
			registry[field] = version
			source[field] = lang
		}
	}
	m, err := loadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range m.Groups {
		for _, field := range versionedToolchains {
			version := g.toolchain(field)
			if version == "" {
				continue
			}
			held, ok := registry[field]
			if !ok {
				t.Errorf("group %q installs %s %s and no test/conformance/*/ci.json names that toolchain", g.Name, field, version)
				continue
			}
			if held != version {
				t.Errorf("group %q installs %s %s and test/conformance/%s/ci.json pins %s: bump both or neither", g.Name, field, version, source[field], held)
			}
		}
		// The .NET pin is the one version neither file carries: it lives in
		// .github/dotnet-version, and both workflows read it from there. A
		// group that wrote a version here would be a second pin.
		if strings.ContainsAny(g.Dotnet, "0123456789") {
			t.Errorf("group %q pins dotnet at %q: the SDK version lives in .github/dotnet-version, and this field only says the row needs it", g.Name, g.Dotnet)
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
