package main

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// CI is generated from the registry (docs/TEST-REGISTRY.md, "CI"). Three
// workflows run tests, and every step of theirs that runs one calls only
// schema-test:
//
//   - ci-fast.yml, per pull request and on main: the registry's pr group in
//     one job, per language one compile row and one happy-path round-trip
//     row and nothing else (prGroup), their recorded seconds summed within
//     the job's two minutes. This file writes it whole, with
//     docs/CI-TIERS.md, from the registry and the seconds its tests were
//     measured at; TestTheCIFilesAreCurrent regenerates both and diffs, so a
//     hand edit is red.
//   - schema-nightly.yml: --group nightly, every tier and every language.
//   - roadmap.yml: the one roadmap row that reads the commit range
//     (none.nightly.tooling.roadmap.check), with full history.
//
// checkWorkflows holds every workflow to that: a step of a test-running
// workflow that runs anything but schema-test is red by file and line, and
// so is a schedule anywhere but schema-nightly.yml.

// THE PER-PR JOB (Glenn, 2026-11-25: "just compile it, run a simple happy
// path per-language"; the whole pr group is one job of two minutes or less,
// about 6.7 s a language). The pr group is exactly, for every language row,
// one instance of the happy-path family (happyFamily: make happy-<lang>,
// which is both the language's compile and its happy path): no other row, no
// language-free row, and none twice. Every other row runs in the nightly
// workflows, by its tier. prGroup holds the
// group to that, and to the job's budget: the job's whole wall time, its
// setup (prTiming.Setup, measured) and the parallel run's critical path,
// at most prJobBudget, each row measured (an unmeasured row weighs nothing
// honest). A language over its serial share (prJobBudget over the
// languages) gets faster (it builds only what its happy path needs, from a
// smaller unit); no row moves out, and the budget is never raised.
const (
	prJobBudget    = 120.0
	prJob          = "schema-test-pr"
	prJobRunPrefix = "go run ./tools/schema-test run"
)

// prRunTiming is what the pr job's wall time is made of besides its rows'
// own seconds.
type prRunTiming struct {
	// Jobs is how many languages' calls the run makes at once (its --jobs,
	// testreg.Runner.Jobs): a fleet runner's share of its host's cores.
	Jobs int
	// Setup is the job's measured time before the run step: the runner's
	// job set-up, the checkout, the merge of main, the pinned siblings, the
	// SDK's environment, and the driver's build.
	Setup float64
	// Run is the run step's measured wall time, which the critical path
	// computed from the rows' seconds is held beside: the builds the run
	// prepares one after another before its calls are in the rows' seconds
	// as shares, so the measured wall is the floor of what the job pays.
	Run float64
}

// prTiming is the pr job's, as measured: PRTIMING_MEASURED.
var prTiming = prRunTiming{Jobs: 4, Setup: 0, Run: 0}

const (
	fastWorkflow    = ".github/workflows/ci-fast.yml"
	nightlyWorkflow = ".github/workflows/schema-nightly.yml"
	tiersDoc        = "docs/CI-TIERS.md"
)

// nonTestWorkflows run no test (docs/TEST-REGISTRY.md, "CI"): each is
// pinned in ci-pins.tsv, so a change to one is a reviewed change, and
// docs/CI-TIERS.md lists them.
var nonTestWorkflows = map[string]string{
	"cla.yml":           "the contributor agreement: an action that reads the pull request's signers, no repository test",
	"release.yml":       "the release: tags and builds a release on a green CI fast run at the commit (the fast lane), never a test of its own",
	"promotion-tag.yml": "tags a commit main promoted; runs no test",
}

// setupActions are the repository's own actions a test-running workflow may
// use: each puts in place what tests need, and none runs a test. A step that
// uses any other local action is held like a run step: red.
var setupActions = map[string]string{
	"merge-main":        "merges current main into a lane pull request's checkout",
	"schema-test-setup": "the sibling runtimes, the runner's SDK and the row's interpreter pins",
	"fixed-corpus":      "builds and shares the fixed corpus, held to the checkout's source",
	"produce":           "builds and shares a producer's tree (a conformance leg), held to the checkout's source",
	"main-tree":         "exports main's tree and ledger for schema-test extract",
	"dist-pins":         "the lua, luau, goose and lobster pins",
	"python":            "the python pins",
	"ruby":              "the ruby pins",
	"moo":               "the moo pin",
}

// prRow is one language's row in the pr group: its happy path, by instance
// name, with its recorded seconds.
type prRow struct {
	Lang    string
	Happy   string
	Seconds float64
}

// prGroup reads the registry's pr group and holds it to the per-PR rule: for
// every language row, exactly one instance of the happy-path family, and
// nothing else; every instance measured, by its language's own seconds; the
// job's setup and the run's critical path within prJobBudget. Each break is
// named. The rows come back in the registry's language order.
func prGroup(reg *testreg.Registry) ([]prRow, []string) {
	return prGroupTimed(reg, prTiming)
}

// prCriticalPath is the parallel run's wall time from the rows' recorded
// seconds: each language's row is one harness call, and the calls go
// longest first to the least loaded of jobs lanes, as the run's shared
// lanes take them; the busiest lane is the path.
func prCriticalPath(rows []prRow, jobs int) float64 {
	secs := make([]float64, 0, len(rows))
	for _, r := range rows {
		secs = append(secs, r.Seconds)
	}
	slices.SortFunc(secs, func(a, b float64) int { return cmp.Compare(b, a) })
	lanes := make([]float64, max(jobs, 1))
	for _, s := range secs {
		least := 0
		for i := range lanes {
			if lanes[i] < lanes[least] {
				least = i
			}
		}
		lanes[least] += s
	}
	return slices.Max(lanes)
}

// prGroupTimed is prGroup with the job's timing given.
func prGroupTimed(reg *testreg.Registry, timing prRunTiming) ([]prRow, []string) {
	g, ok := reg.Group("pr")
	if !ok {
		return nil, []string{"the registry has no group pr"}
	}
	sel, err := reg.Select(g.Query())
	if err != nil {
		return nil, []string{fmt.Sprintf("group pr: %v", err)}
	}
	var problems []string
	byLang := map[string]*prRow{}
	langs := reg.LangNames()
	for _, l := range langs {
		byLang[l] = &prRow{Lang: l}
	}
	total := 0.0
	for _, inst := range sel.Instances {
		r, ok := byLang[inst.Lang]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s is in the pr group, and the pr group holds per language one happy-path row (family %s), never a language-free row: it runs nightly", inst.Full, happyFamily))
			continue
		}
		if !measured(inst) {
			problems = append(problems, fmt.Sprintf("%s has no measured seconds: a pr row is measured alone on a fleet runner, so the job's sum is honest", inst.Full))
		}
		w := testreg.Weight(inst)
		total += w
		r.Seconds += w
		switch {
		case inst.Test.Family != happyFamily:
			problems = append(problems, fmt.Sprintf("%s is family %s, and the pr group holds per language one happy-path row (family %s: make happy-<lang>, the compile and the happy path), nothing else: it runs nightly", inst.Full, inst.Test.Family, happyFamily))
		case r.Happy != "":
			problems = append(problems, fmt.Sprintf("%s and %s are both happy paths of %s: the pr group holds one per language", r.Happy, inst.Full, inst.Lang))
		default:
			r.Happy = inst.Full
		}
	}
	var rows []prRow
	for _, l := range langs {
		r := byLang[l]
		if r.Happy == "" {
			problems = append(problems, fmt.Sprintf("%s has no happy-path row (family %s) in the pr group", l, happyFamily))
		}
		rows = append(rows, *r)
	}
	path := prCriticalPath(rows, timing.Jobs)
	if wall := timing.Setup + max(path, timing.Run); wall > prJobBudget {
		var each []string
		for _, r := range rows {
			if r.Seconds > prJobBudget/float64(len(rows)) {
				each = append(each, fmt.Sprintf("%s %.1f s", r.Lang, r.Seconds))
			}
		}
		problems = append(problems, fmt.Sprintf("the pr job takes %.1f s, past its %.0f s: setup %.1f s and the run's critical path %.1f s on %d lanes (computed %.1f s from the rows, measured %.1f s; the rows sum to %.1f s); a language past its %.1f s serial share gets faster (build only what its happy path needs, from a smaller unit), never moves a row out, and the budget is never raised; past the share: %s",
			wall, prJobBudget, timing.Setup, max(path, timing.Run), timing.Jobs, path, timing.Run, total, prJobBudget/float64(max(1, len(rows))), strings.Join(each, ", ")))
	}
	return rows, problems
}

// measured: the instance's own seconds are recorded: its language's, in a
// union row's Go test or another row's lang_seconds. A row's one number is
// its slowest language's, never this language's own.
func measured(inst testreg.Instance) bool {
	if tests, union, err := testreg.GoTestsOf(inst.Test); union && err == nil {
		return tests[inst.Lang].Seconds > 0
	}
	return inst.Test.LangSeconds[inst.Lang] > 0
}

// maxMatrixJobs is the most jobs GitHub Actions lets one matrix make; a
// matrix past it fails the whole run before any job starts.
const maxMatrixJobs = 256

// generatedWorkflowFiles are the workflows ciFiles writes, by repository
// path (TestTheBaseLeavesOutExactlyTheGeneratedWorkflows holds the two to
// each other): the extract's list, whose jobs as generated are no items.
var generatedWorkflowFiles = extract.GeneratedWorkflows

// pinsBaseRef is the commit a change merges into, which `extract
// --pins-base` holds the not-yet-generated list to, from the GitHub event
// ci-fast.yml runs on: a pull request's target branch (main, or an
// integration branch), a push's previous tip (the payload's before), main
// for a dispatch. Every other event, a pull request with no target, and a
// push with no previous tip (a new branch, or a payload without one) is an
// error naming why: there is always a base, or the step is red.
func pinsBaseRef(event, baseRef string, payload []byte) (string, error) {
	switch event {
	case "pull_request":
		if baseRef == "" {
			return "", fmt.Errorf("--pins-base-ref: a pull_request with no GITHUB_BASE_REF: the branch it merges into is unknown")
		}
		return baseRef, nil
	case "push":
		var p struct {
			Before string `json:"before"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return "", fmt.Errorf("--pins-base-ref: the push payload does not read: %w", err)
		}
		if !sumRe40.MatchString(p.Before) || strings.Trim(p.Before, "0") == "" {
			return "", fmt.Errorf("--pins-base-ref: the push names no previous tip (before %q): the branch's previous tip is the base", p.Before)
		}
		return p.Before, nil
	case "workflow_dispatch":
		return "main", nil
	}
	return "", fmt.Errorf("--pins-base-ref: event %q: ci-fast.yml runs on pull_request, push and workflow_dispatch, and the base of another event is not defined", event)
}

// sumRe40 is a full git commit sha.
var sumRe40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ciFiles are the generated files, by repository path.
func ciFiles(reg *testreg.Registry) (map[string][]byte, error) {
	rows, problems := prGroup(reg)
	if len(problems) > 0 {
		return nil, fmt.Errorf("the pr group is not one happy-path row per language within the job's %.0f s:\n  %s", prJobBudget, strings.Join(problems, "\n  "))
	}
	var wf bytes.Buffer
	if err := fastTemplate.Execute(&wf, map[string]any{"Job": prJob, "Budget": prJobBudget, "Run": prRunLine}); err != nil {
		return nil, err
	}
	if p := checkFastRuns(wf.Bytes()); len(p) > 0 {
		return nil, fmt.Errorf("%s:\n  %s", fastWorkflow, strings.Join(p, "\n  "))
	}
	road, err := roadmapWorkflow(reg)
	if err != nil {
		return nil, err
	}
	night, err := nightlyWorkflowText(reg)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		fastWorkflow:    wf.Bytes(),
		roadmapFile:     road,
		nightlyWorkflow: night,
		tiersDoc:        tiersPage(reg, rows),
	}, nil
}

// ciTemplates are the hand-written workflows schema-test ci generates: the
// text is reviewed in the template, and the registry fills it in.
//
//go:embed ci-templates/*.tmpl
var ciTemplates embed.FS

// roadmapFile runs the one row that reads a commit range, with full
// history: ci-fast.yml's shards check out one commit.
const (
	roadmapFile = ".github/workflows/roadmap.yml"
	roadmapKey  = "tooling.roadmap.check"
)

// nightlyParts: schema-nightly.yml's part jobs, each one part of the full
// run's plan on the runner it lands on (fullrun launch --part I/N).
const nightlyParts = 24

func execTemplate(name string, data any) ([]byte, error) {
	t, err := template.New(name).Delims("[[", "]]").ParseFS(ciTemplates, "ci-templates/"+name)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// roadmapWorkflow is roadmap.yml: the row roadmapKey, run by its instance
// name, which the registry must hold as a language-free row.
func roadmapWorkflow(reg *testreg.Registry) ([]byte, error) {
	for _, t := range reg.Tests {
		if t.Key != roadmapKey {
			continue
		}
		if t.PerLang() {
			return nil, fmt.Errorf("%s: row %s is per-language; roadmap.yml runs one language-free instance", roadmapFile, roadmapKey)
		}
		return execTemplate("roadmap.yml.tmpl", map[string]any{"Key": roadmapKey, "Row": testreg.LangFree + "." + t.Tier + "." + t.Key})
	}
	return nil, fmt.Errorf("%s: the registry has no row %s, which roadmap.yml runs", roadmapFile, roadmapKey)
}

// nightlyWorkflowText is schema-nightly.yml: nightlyParts part jobs of the
// full run, and the verdict that merges them. Every machine property a row
// of the registry needs (Test.Needs) is a runner label each part job asks
// for, and a property its bench declares to fullrun (--bench local=2+P),
// which fullrun probes on the runner: a part lands only on a runner whose
// label says it has the property, and holds it to the machine.
func nightlyWorkflowText(reg *testreg.Registry) ([]byte, error) {
	parts := make([]string, nightlyParts)
	for i := range parts {
		parts[i] = fmt.Sprint(i + 1)
	}
	needs := map[string]bool{}
	for _, t := range reg.Tests {
		for _, n := range t.Needs {
			needs[n] = true
		}
	}
	var labels, provides string
	for _, n := range slices.Sorted(maps.Keys(needs)) {
		labels += ", " + n
		provides += "+" + n
	}
	return execTemplate("schema-nightly.yml.tmpl", map[string]any{"Parts": nightlyParts, "PartList": "[" + strings.Join(parts, ", ") + "]",
		"Labels": labels, "Provides": provides})
}

// tiersPage is docs/CI-TIERS.md: the three test-running workflows, each
// tier's counts, and the per-PR job's rows, per language.
func tiersPage(reg *testreg.Registry, rows []prRow) []byte {
	var b strings.Builder
	b.WriteString(`# CI tiers: what a pull request runs, and what waits for the night

<!-- GENERATED by ` + "`go run ./tools/schema-test ci`" + ` from test/registry/registry.jsonl.
Do not edit: TestTheCIFilesAreCurrent regenerates this page and fails on any
difference. -->

Every test is a row of [the registry](TEST-REGISTRY.md), and CI runs rows by
query through one driver, ` + "`schema-test`" + `. Three workflows run tests, and every
step of theirs that runs one calls only ` + "`schema-test`" + `:

| workflow | when | what it runs |
|---|---|---|
| ` + "`ci-fast.yml`" + ` | every pull request, every push to main, dispatch | ` + "`schema-test run --group pr`" + `, one job: the rows below; ` + "`schema-test extract`" + ` against main's ledger; ` + "`schema-test ci --check`" + `; the ` + "`verdict`" + ` job |
| ` + "`schema-nightly.yml`" + ` | nightly, dispatch | ` + "`schema-test run --group nightly`" + `: every tier, every language, through ` + "`go run ./tools/fullrun`" + `; a language that does not answer a row is red by name |
| ` + "`roadmap.yml`" + ` | pull requests touching the roadmap, pushes to main | ` + "`schema-test run none.nightly.tooling.roadmap.check`" + `, the row that reads the commit range, with full history |

Every other file under ` + "`.github`" + ` is pinned in ` + "`test/registry/ci-pins.tsv`" + `
at its sha256 (` + "`schema-test ci --check`" + `): the test workflows a later layer
generates (not-yet-generated, a list that only shrinks), the setup actions
and their data, and the workflows that run none:
`)
	names := make([]string, 0, len(nonTestWorkflows))
	for n := range nonTestWorkflows {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "- `%s`: %s.\n", n, nonTestWorkflows[n])
	}
	b.WriteString(`
Nothing selects by path or branch: every pull request runs the whole ` + "`pr`" + `
group, and a push to main runs the same. Every other row runs in the nightly
workflows, by its tier.

## The tiers

| tier | tests | what it is |
|---|---|---|
`)
	count := map[string]int{}
	for _, t := range reg.Tests {
		count[t.Tier]++
	}
	for _, t := range reg.Tiers {
		fmt.Fprintf(&b, "| `%s` | %d | %s |\n", t.Name, count[t.Name], cell(t.What))
	}
	fmt.Fprintf(&b, `
## The per-PR job

One job, `+"`%s`"+`, runs `+"`%s`"+`: for every language, one row,
`+"`make happy-<lang>`"+` (`+"`make/happy-path.mk`"+`), which generates one small schema,
compiles it with that language's program and does one round trip: the
language's compile and its happy path, and nothing else. The languages run
side by side, %d at once. The whole job, its setup (%.1f s, measured) and the
run's critical path (the longer of the path the rows' seconds give and the
run's measured %.1f s), is at most %.0f s, the job's two minutes; a language
past its %.1f s serial share gets faster, and no row moves out to fit.

| language | happy-path row | measured seconds |
|---|---|---|
`, prJob, prRunLine, prTiming.Jobs, prTiming.Setup, prTiming.Run, prJobBudget, prJobBudget/float64(max(1, len(rows))))
	secs := 0.0
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | `%s` | %.1f |\n", r.Lang, r.Happy, r.Seconds)
		secs += r.Seconds
	}
	fmt.Fprintf(&b, "| **every language, one after another** | | **%.1f** |\n", secs)
	fmt.Fprintf(&b, "| **the run's critical path, %d at once** | | **%.1f** |\n", prTiming.Jobs, prCriticalPath(rows, prTiming.Jobs))
	b.WriteString(`
To change the job, change the registry's ` + "`pr`" + ` group (its query names
the rows) or a row's measured ` + "`lang_seconds`" + ` (` + "`schema-test run ... --write-seconds`" + `
on a fleet runner, or ` + "`schema-test seconds LOG...`" + ` from a run's log), then run
` + "`go run ./tools/schema-test ci`" + `.
`)
	return []byte(b.String())
}

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// fastTemplate is ci-fast.yml. The steps before a schema-test call only put
// in place what the tests need (actions: the checkout, the sibling runtimes,
// the pins); no step runs a check but schema-test.
var fastTemplate = template.Must(template.New("ci-fast.yml").Delims("[[", "]]").Parse(`# GENERATED by ` + "`go run ./tools/schema-test ci`" + ` from test/registry/registry.jsonl.
# Do not edit: TestTheCIFilesAreCurrent regenerates this file and fails on any
# difference. docs/CI-TIERS.md says what each job runs; docs/TEST-REGISTRY.md
# is the registry and the driver.
#
# THE PER-PR JOB. Every pull request, and every push to main, runs the
# registry's pr group whole in one job, [[.Job]]: for every language one
# compile row and one happy-path round-trip row, and nothing else, the
# languages run side by side, the whole job (setup and run) measured at most
# [[.Budget]] s, and capped at two minutes.
# Every other row runs in the nightly workflows. Nothing selects by path or
# branch. Every step that runs a test calls schema-test, and only it
# (TestEveryWorkflowStepCallsOnlySchemaTest). release.yml's fast lane is
# locked on a green run of this workflow at the commit.
#
# FORK SAFETY: this repository is public and its runners are self-hosted on
# the fleet, so a fork's pull request never runs on fleet hardware: every
# fleet job carries the fork guard, and fork-pull-request fails the run by
# name so a fork's pull request is never green.
name: CI fast

on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:

concurrency:
  group: ${{ github.workflow }}-${{ github.event.pull_request.number || format('{0}-{1}', github.event_name, github.ref) }}${{ github.event_name == 'push' && format('-{0}', github.sha) || '' }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}

env:
  GOFLAGS: -mod=readonly

# the token every job holds reads the repository and nothing more: the shards
# hand it to the tests (the caa self-test reads main's tip with it)
permissions:
  contents: read

jobs:
  fork-pull-request:
    if: (github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name != github.repository)
    runs-on: ubuntu-latest
    timeout-minutes: 2
    permissions: {}
    steps:
      - name: a fork's pull request runs no CI here
        shell: bash
        run: |
          echo "::error title=No CI ran::This pull request comes from a fork. Fork code never runs on this repository's own runners, so no test has run on it. A maintainer who has read the diff pushes its head to a branch in mas-bandwidth/schema and opens the pull request from that branch."
          exit 1

  # the registry itself: every test is a row or in the unmoved ledger, the
  # ledger only shrinks against main's, and this file and docs/CI-TIERS.md
  # are what the registry generates
  schema-test:
    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository
    runs-on: [self-hosted, Linux, X64, fleet, schema]
    timeout-minutes: 2
    defaults:
      run:
        shell: go run ./tools/pgroup -- bash -e -o pipefail {0}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: ./.github/actions/merge-main
      - uses: actions/setup-go@d8cbb8045d7f43706b9177cceb36c1f91a6644d5 # v7.1.0
        if: runner.environment == 'github-hosted'
        with:
          go-version: '1.26'
          cache: false
      - name: the CI files are the registry's, and every workflow calls only schema-test
        run: go run ./tools/schema-test ci --check
      - name: the coverage matrix, every test times every language
        run: go run ./tools/schema-test coverage
      - id: main
        uses: ./.github/actions/main-tree
      - name: schema-test extract, against main's ledger and tree, and the base's not-yet-generated list
        if: steps.main.outputs.ledger != ''
        env:
          MAIN_TREE: ${{ steps.main.outputs.tree }}
          MAIN_LEDGER: ${{ steps.main.outputs.ledger }}
          BASE_TREE: ${{ steps.main.outputs.base }}
        run: go run ./tools/schema-test extract --since "$MAIN_TREE" --pins-base "$BASE_TREE" --shrinks-from "$MAIN_LEDGER"
      - name: schema-test extract, against main's tree (main has no ledger yet), and the base's not-yet-generated list
        if: steps.main.outputs.ledger == ''
        env:
          MAIN_TREE: ${{ steps.main.outputs.tree }}
          BASE_TREE: ${{ steps.main.outputs.base }}
        run: go run ./tools/schema-test extract --since "$MAIN_TREE" --pins-base "$BASE_TREE"

  # THE REGISTRY'S PR GROUP, one job (docs/CI-TIERS.md): every language's
  # compile row and happy-path row
  [[.Job]]:
    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository
    runs-on: [self-hosted, Linux, X64, fleet, schema]
    timeout-minutes: 2
    defaults:
      run:
        shell: go run ./tools/pgroup -- bash -e -o pipefail {0}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: ./.github/actions/merge-main
      - uses: actions/setup-go@d8cbb8045d7f43706b9177cceb36c1f91a6644d5 # v7.1.0
        if: runner.environment == 'github-hosted'
        with:
          go-version: '1.26'
          cache: false
      - uses: ./.github/actions/schema-test-setup
        with:
          lang: all
      - name: schema-test run --group pr
        run: [[.Run]]

  # green only when every job above ran and passed: the pr job's run fails on
  # expected != ran, and ci --check holds the job to the whole pr group
  verdict:
    needs: [fork-pull-request, schema-test, [[.Job]]]
    if: (always() && !cancelled() && (github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository))
    runs-on: [self-hosted, Linux, X64, fleet, schema]
    timeout-minutes: 2
    defaults:
      run:
        shell: go run ./tools/pgroup -- bash -e -o pipefail {0}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: ./.github/actions/merge-main
      - uses: actions/setup-go@d8cbb8045d7f43706b9177cceb36c1f91a6644d5 # v7.1.0
        if: runner.environment == 'github-hosted'
        with:
          go-version: '1.26'
          cache: false
      - name: every job of this run passed
        env:
          NEEDS: ${{ toJSON(needs) }}
        run: go run ./tools/schema-test ci --verdict
`))

// prRunLine is the one test run ci-fast.yml makes: the pr group, whole, in
// one job.
var prRunLine = prJobRunPrefix + " --group pr --jobs " + strconv.Itoa(prTiming.Jobs)

// checkFastRuns holds ci-fast.yml to one test run per pull request: exactly
// one step runs schema-test run, and it runs the pr group whole (no shard,
// no language, no other selection). A second test-running step, which is a
// second per-PR test job or a job split in two, is named by its line.
func checkFastRuns(data []byte) []string {
	var runs []string
	for _, st := range workflowSteps(fastWorkflow, data) {
		for _, c := range st.cmds {
			if strings.HasPrefix(c.text, prJobRunPrefix) {
				runs = append(runs, fmt.Sprintf("%d: %s", c.line, c.text))
			}
		}
	}
	switch {
	case len(runs) == 0:
		return []string{fmt.Sprintf("no step runs %q: the pr group has no job", prRunLine)}
	case len(runs) > 1:
		return []string{fmt.Sprintf("%d steps run tests per pull request, and the pr group runs in one job, one step: %s", len(runs), strings.Join(runs, "; "))}
	}
	if _, text, _ := strings.Cut(runs[0], ": "); text != prRunLine {
		return []string{fmt.Sprintf("line %s: the pr job runs %q, the pr group whole, and nothing else", runs[0], prRunLine)}
	}
	return nil
}

// verdictJobs are the jobs the verdict job needs, every one of ci-fast.yml's
// others.
var verdictJobs = []string{"fork-pull-request", "schema-test", prJob}

// verdict reads the needs context of the verdict job: every job passed, and
// the fork guard alone may be skipped (it runs only for a fork's pull
// request). A job skipped, cancelled or failed is named, and so is a job
// missing from the context or one the workflow does not generate.
func verdict(needs []byte) error {
	var m map[string]struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(needs, &m); err != nil {
		return fmt.Errorf("the needs context does not read: %w", err)
	}
	for _, want := range verdictJobs {
		if _, ok := m[want]; !ok {
			return fmt.Errorf("the needs context has no job %s", want)
		}
	}
	var bad []string
	for job, v := range m {
		if !slices.Contains(verdictJobs, job) {
			bad = append(bad, fmt.Sprintf("%s: a job ci-fast.yml does not generate", job))
			continue
		}
		if job == "fork-pull-request" && v.Result == "skipped" {
			continue
		}
		if v.Result != "success" {
			bad = append(bad, fmt.Sprintf("%s: %s", job, v.Result))
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("not every job of this run passed: %s", strings.Join(bad, "; "))
	}
	return nil
}

// workflowStep is one step's run text, where it is, and its file.
type workflowStep struct {
	file string
	line int // the line of "run:"
	cmds []stepLine
}

type stepLine struct {
	line int
	text string
}

var (
	runKey = regexp.MustCompile(`^(\s*)(?:- )?run\s*:\s*(.*)$`)
	// a block scalar header: | or >, with a chomping or indent indicator.
	blockScalar = regexp.MustCompile(`^[|>][-+0-9]*$`)
	// a mapping key, as under "defaults: run:" (shell:, working-directory:).
	mappingKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*\s*:(\s|$)`)
	// a trigger that starts tests on a clock or after another workflow, in
	// any YAML form (a key, or inside a flow mapping or list).
	clockTrigger = regexp.MustCompile(`(^|[\s{\[,])["']?(schedule|workflow_run)["']?\s*:`)
	// a matrix's include list written inline as JSON, as schema-test ci writes it.
	includeRe = regexp.MustCompile(`^\s*include:\s*(\[.*\])\s*$`)
	usesRe    = regexp.MustCompile(`^\s*(?:- )?uses:\s*["']?([^\s"'#]+)`)
	shellRe   = regexp.MustCompile(`^\s*(?:- )?shell:\s*(.*?)\s*$`)
	// the one expression a step's line may hold: a matrix value, which the
	// generated include list fixes; any other (an input, a step's output, an
	// event field) could expand to shell, so it goes through env: as $NAME.
	matrixExprIn = regexp.MustCompile(`\$\{\{\s*matrix\.[A-Za-z0-9_-]+\s*\}\}`)
	// shell that runs another command: a separator, a pipe, a redirection,
	// a substitution or an escape.
	shellMeta = regexp.MustCompile("[;&|<>`\\\\]|\\$\\(")
	// a line that runs nothing: a comment, a shell option that stops on error
	// (never set -n, which reads the rest without running it), a message, a
	// failing exit, 1 to 255 (exit 0 would end the step green before the
	// driver ran, and so would 256, which the shell reads as 0).
	inertLine = regexp.MustCompile(`^(#.*|set( -[euxo]+( pipefail)?)+|echo .*|exit ([1-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5]))$`)
	// the commands a test workflow's step may run: the driver, and the full
	// run's launcher and merge, which run the driver across the benches.
	// a driver flag a workflow never passes: it re-pins goldens (--update),
	// changes a limit (--timeout), points at another registry or binary, or
	// writes the tree (--write-ledger, --write-seconds, --move).
	driverOverride = regexp.MustCompile(`(^|\s)--?(update|timeout|registry|write-ledger|write-seconds|move|schema-test)(=|\s|$)`)
	// a --root other than the checkout.
	otherRoot  = regexp.MustCompile(`(^|\s)--?root(=|\s+)(?:[^.\s]|\.\S)`)
	driverLine = regexp.MustCompile(`^go run \./tools/(schema-test (run|ci|coverage|extract)|fullrun (launch|merge))( |$)`)
)

// externalActions are the actions from outside the repository a test
// workflow may use, by name before the @: each checks out, sets up Go or
// moves a run's files, and none runs a check.
var externalActions = map[string]bool{
	"actions/checkout":          true,
	"actions/setup-go":          true,
	"actions/upload-artifact":   true,
	"actions/download-artifact": true,
	"actions/cache/restore":     true,
	"actions/cache/save":        true,
}

// stepShells are the shells a test workflow's step may name.
var stepShells = map[string]bool{
	"bash": true,
	"go run ./tools/pgroup -- bash -e -o pipefail {0}": true,
}

// runsNothing: a comment, a shell option, an exit, or an echo whose text
// starts no other command.
func runsNothing(s string) bool {
	if strings.HasPrefix(s, "#") {
		return true // a comment, whatever its text: the shell runs none of it
	}
	return inertLine.MatchString(s) && plainArgs(s)
}

// callsTheDriver: the driver or the full run, and nothing after it: matrix
// values and $NAME variables are arguments, never commands.
func callsTheDriver(s string) bool {
	return driverLine.MatchString(s) && plainArgs(s) && !driverOverride.MatchString(s) && !otherRoot.MatchString(s)
}

// plainArgs: the line chains no other command, and every GitHub expression
// in it is a matrix value (matrixExpr). Any "${{" left once the matrix
// values are taken out is another expression, however its braces nest.
func plainArgs(s string) bool {
	rest := matrixExprIn.ReplaceAllString(s, "x")
	if strings.Contains(rest, "${{") {
		return false
	}
	return !shellMeta.MatchString(rest)
}

// workflowSteps reads every step's run text of a workflow, by line. The
// workflows are YAML read by line: a run: key, its inline value or the block
// below it (every line indented deeper).
func workflowSteps(file string, data []byte) []workflowStep {
	lines := strings.Split(string(data), "\n")
	var out []workflowStep
	for i := 0; i < len(lines); i++ {
		m := runKey.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[2])
		if strings.HasPrefix(v, "#") {
			v = "" // "run: # note": a comment, and the value is below it
		}
		st := workflowStep{file: file, line: i + 1}
		if v != "" && !blockScalar.MatchString(v) {
			st.cmds = append(st.cmds, stepLine{i + 1, v})
			out = append(out, st)
			continue
		}
		// a block scalar, or a plain scalar on the lines below an empty
		// "run:": every line indented deeper is the command
		// the key's column: past the "- " of a list item, so a sibling key
		// (shell:, env:) after the block ends it
		base := len(m[1])
		if strings.HasPrefix(strings.TrimLeft(lines[i], " "), "- ") {
			base += 2
		}
		var below []stepLine
		end := i
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t != "" && len(lines[j])-len(strings.TrimLeft(lines[j], " ")) <= base {
				break
			}
			if t != "" {
				below = append(below, stepLine{j + 1, t})
			}
			end = j
		}
		if v == "" && len(below) > 0 && allKeys(below) {
			continue // "defaults: run:" maps shell: and working-directory:, no command
		}
		st.cmds = below
		i = end
		out = append(out, st)
	}
	return out
}

// allKeys: every line is a mapping key (key: value).
func allKeys(ls []stepLine) bool {
	for _, l := range ls {
		if !mappingKey.MatchString(l.text) {
			return false
		}
	}
	return true
}

// checkWorkflows holds the generated workflows (file name -> content) to the
// CI rule: every line of every run step calls schema-test (or the full run
// that calls it) or runs nothing; each action is one externalActions or
// setupActions names; only schema-nightly.yml has a schedule, and it runs
// the nightly group. Generation fixes what the files say, and pinning fixes
// every other file under .github (checkTree); this holds the generator's
// templates to the rule, in the one form they are written in (block YAML,
// plain keys). Each break is named file:line.
func checkWorkflows(dir string, files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var problems []string
	nightly := false
	for _, n := range names {
		data := files[n]
		path := filepath.ToSlash(filepath.Join(dir, n))
		lines := strings.Split(string(data), "\n")
		clock := false
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			if m := includeRe.FindStringSubmatch(l); m != nil {
				var entries []json.RawMessage
				if err := json.Unmarshal([]byte(m[1]), &entries); err != nil {
					problems = append(problems, fmt.Sprintf("%s:%d: a matrix include list that does not read as JSON: %v", path, i+1, err))
				} else if len(entries) > maxMatrixJobs {
					problems = append(problems, fmt.Sprintf("%s:%d: a matrix of %d jobs, past GitHub's %d: the run fails before any job starts", path, i+1, len(entries), maxMatrixJobs))
				}
			}
			if clockTrigger.MatchString(l) {
				clock = true
				if path != nightlyWorkflow {
					problems = append(problems, fmt.Sprintf("%s:%d: a schedule or workflow_run trigger: only %s runs on a schedule (--group nightly); a nightly check is a registry row of tier nightly", path, i+1, nightlyWorkflow))
				}
			}
		}
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			if m := shellRe.FindStringSubmatch(l); m != nil && m[1] != "" && !stepShells[strings.Trim(m[1], `"'`)] {
				problems = append(problems, fmt.Sprintf("%s:%d: a step runs under shell %q: a test workflow's steps run under bash or tools/pgroup", path, i+1, m[1]))
			}
			m := usesRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			if local, ok := strings.CutPrefix(m[1], "./.github/actions/"); ok {
				if _, ok := setupActions[local]; !ok {
					problems = append(problems, fmt.Sprintf("%s:%d: a step uses ./.github/actions/%s, which is no setup action: a workflow step runs a check only through schema-test", path, i+1, local))
				}
				continue
			}
			name, _, _ := strings.Cut(m[1], "@")
			if !externalActions[name] {
				problems = append(problems, fmt.Sprintf("%s:%d: a step uses %s, an action that may run a check: a test workflow uses only checkout, setup-go, the artifact actions and the repository's setup actions", path, i+1, name))
			}
		}
		for _, st := range workflowSteps(path, data) {
			for _, c := range st.cmds {
				if runsNothing(c.text) || callsTheDriver(c.text) {
					if path == nightlyWorkflow && clock && (strings.Contains(c.text, "--group nightly") || strings.HasPrefix(c.text, "go run ./tools/fullrun launch")) {
						nightly = true
					}
					continue
				}
				problems = append(problems, fmt.Sprintf("%s:%d: a step runs %q: a workflow step runs a check only through schema-test (go run ./tools/schema-test ...), with nothing chained after it; make the check a registry row", path, c.line, firstLine(c.text)))
			}
		}
	}
	if !nightly {
		problems = append(problems, fmt.Sprintf("%s: no scheduled step runs the nightly group (go run ./tools/schema-test run --group nightly, or go run ./tools/fullrun launch): the nightly-tier rows would run nowhere", nightlyWorkflow))
	}
	return problems
}

// ciPinsPath lists every file under .github that schema-test ci does not
// generate, by its content hash: pinned (reviewed as it is), or
// not-yet-generated (a test workflow a later layer generates; the list only
// shrinks, extract holds it to main's).
const ciPinsPath = "test/registry/ci-pins.tsv"

const (
	pinPinned          = "pinned"
	pinNotYetGenerated = "not-yet-generated"
	githubDir          = ".github"
)

// ciPin is one line of ci-pins.tsv: a file, its sha256, and its state.
type ciPin struct {
	Path, Sum, State string
	Line             int
}

var sumRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// readPins reads ci-pins.tsv: "path<TAB>sha256<TAB>state" lines, with #
// comments. A line that does not read, a path twice, a state that is no
// state and a path outside .github are each named.
func readPins(data []byte) ([]ciPin, []string) {
	var pins []ciPin
	var problems []string
	seen := map[string]int{}
	for i, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		at := fmt.Sprintf("%s:%d", ciPinsPath, i+1)
		f := strings.Split(l, "\t")
		switch {
		case len(f) != 3:
			problems = append(problems, fmt.Sprintf("%s: want 3 tab-separated fields (path, sha256, state), got %d", at, len(f)))
			continue
		case !strings.HasPrefix(f[0], githubDir+"/"):
			problems = append(problems, fmt.Sprintf("%s: %s is not under %s/: only .github's files are pinned", at, f[0], githubDir))
			continue
		case !sumRe.MatchString(f[1]):
			problems = append(problems, fmt.Sprintf("%s: %q is no sha256 (64 lowercase hex digits)", at, f[1]))
			continue
		case f[2] != pinPinned && f[2] != pinNotYetGenerated:
			problems = append(problems, fmt.Sprintf("%s: state %q is neither %s nor %s", at, f[2], pinPinned, pinNotYetGenerated))
			continue
		}
		if prev, dup := seen[f[0]]; dup {
			problems = append(problems, fmt.Sprintf("%s: pins %s, which line %d already pins", at, f[0], prev))
			continue
		}
		seen[f[0]] = i + 1
		pins = append(pins, ciPin{Path: f[0], Sum: f[1], State: f[2], Line: i + 1})
	}
	return pins, problems
}

// checkTree holds every file under root's .github to one of two: generated
// by schema-test ci (diffFiles compares it whole), or pinned in ci-pins.tsv
// at its content's sha256. A file that is neither, a pin that moved, a pin
// of a file that is gone and a pin of a generated file are each named, with
// the line that settles it.
func checkTree(root string, generated map[string][]byte, pins []ciPin) ([]string, error) {
	byPath := map[string]ciPin{}
	for _, p := range pins {
		byPath[p.Path] = p
	}
	var problems []string
	found := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, githubDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		found[rel] = true
		if _, gen := generated[rel]; gen {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(data))
		p, ok := byPath[rel]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s: neither generated by schema-test ci nor pinned: generate it, or review it and pin it in %s with the line: %s", rel, ciPinsPath, rel+"\t"+sum+"\t"+pinPinned))
		case p.Sum != sum:
			problems = append(problems, fmt.Sprintf("%s: its content is not what %s:%d pinned: a changed pin is a reviewed change; review it, then update that line to: %s", rel, ciPinsPath, p.Line, rel+"\t"+sum+"\t"+p.State))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, p := range pins {
		name, isWorkflow := strings.CutPrefix(p.Path, ".github/workflows/")
		switch {
		case p.State == pinNotYetGenerated && !isWorkflow:
			problems = append(problems, fmt.Sprintf("%s:%d: %s is not-yet-generated, and it is no workflow: only a test workflow waits to be generated", ciPinsPath, p.Line, p.Path))
		case p.State == pinPinned && isWorkflow:
			if _, ok := nonTestWorkflows[name]; !ok {
				problems = append(problems, fmt.Sprintf("%s:%d: pins the workflow %s, which is none of the workflows that run no test (nonTestWorkflows): a workflow that runs tests is generated by schema-test ci, never pinned", ciPinsPath, p.Line, p.Path))
			}
		}
		if _, gen := generated[p.Path]; gen {
			problems = append(problems, fmt.Sprintf("%s:%d: pins %s, which schema-test ci generates: delete the line", ciPinsPath, p.Line, p.Path))
		} else if !found[p.Path] {
			problems = append(problems, fmt.Sprintf("%s:%d: pins %s, which is not there: delete the line", ciPinsPath, p.Line, p.Path))
		}
	}
	return problems, nil
}

// notYetGeneratedShrinks holds the not-yet-generated list to main's (old):
// no file joins it, and a file that leaves it is generated or deleted,
// never pinned instead (checkTree names a file that is neither).
func notYetGeneratedShrinks(old, now []ciPin) []string {
	was := map[string]bool{}
	for _, p := range old {
		if p.State == pinNotYetGenerated {
			was[p.Path] = true
		}
	}
	var problems []string
	for _, p := range now {
		switch {
		case p.State == pinNotYetGenerated && !was[p.Path]:
			problems = append(problems, fmt.Sprintf("%s:%d: %s joins the not-yet-generated list, which only shrinks: generate it with schema-test ci", ciPinsPath, p.Line, p.Path))
		case p.State == pinPinned && was[p.Path]:
			problems = append(problems, fmt.Sprintf("%s:%d: %s leaves the not-yet-generated list as pinned: a test workflow leaves it only by being generated or deleted", ciPinsPath, p.Line, p.Path))
		}
	}
	return problems
}

// generatedWorkflows: the workflows among the generated files, by name.
func generatedWorkflows(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for p, data := range files {
		if name, ok := strings.CutPrefix(p, ".github/workflows/"); ok {
			out[name] = data
		}
	}
	return out
}

// diffFiles compares generated files with what root holds, naming the first
// line that differs in each.
func diffFiles(root string, want map[string][]byte) []string {
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var problems []string
	for _, p := range paths {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v: run go run ./tools/schema-test ci", p, err))
			continue
		}
		if bytes.Equal(got, want[p]) {
			continue
		}
		g, w := strings.Split(string(got), "\n"), strings.Split(string(want[p]), "\n")
		n := 0
		for n < len(g) && n < len(w) && g[n] == w[n] {
			n++
		}
		problems = append(problems, fmt.Sprintf("%s:%d: is not what the registry generates (a hand edit, or a registry change not regenerated): run go run ./tools/schema-test ci", p, n+1))
	}
	return problems
}

// ciCmd is "schema-test ci": write ci-fast.yml and docs/CI-TIERS.md from the
// registry; with --check, regenerate them and hold the tree to them and to
// the workflow rule instead; with --verdict, judge the verdict job's needs.
func ciCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test ci", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "")
	verdictFlag := fs.Bool("verdict", false, "")
	baseRef := fs.Bool("pins-base-ref", false, "")
	root := fs.String("root", ".", "")
	path := fs.String("registry", testreg.DefaultPath, "")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"ci takes no names"}
	}
	if *baseRef {
		var payload []byte
		if p := os.Getenv("GITHUB_EVENT_PATH"); p != "" {
			var err error
			if payload, err = os.ReadFile(p); err != nil {
				return fmt.Errorf("--pins-base-ref: the event payload: %w", err)
			}
		}
		ref, err := pinsBaseRef(os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_BASE_REF"), payload)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, ref)
		return err
	}
	reg, err := testreg.Load(filepath.Join(*root, *path))
	if err != nil {
		return err
	}
	if *verdictFlag {
		if _, problems := prGroup(reg); len(problems) > 0 {
			return fmt.Errorf("the pr group is not one compile row and one happy-path row per language within the job's %.0f s:\n  %s", prJobBudget, strings.Join(problems, "\n  "))
		}
		if err := verdict([]byte(os.Getenv("NEEDS"))); err != nil {
			return err
		}
		_, err := fmt.Fprintln(stdout, "schema-test ci: every job of this run passed")
		return err
	}
	files, err := ciFiles(reg)
	if err != nil {
		return err
	}
	if !*check {
		for p, data := range files {
			if err := os.WriteFile(filepath.Join(*root, filepath.FromSlash(p)), data, 0o644); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(stdout, "wrote %s\n", p); err != nil {
				return err
			}
		}
		return nil
	}
	problems := diffFiles(*root, files)
	pinData, err := os.ReadFile(filepath.Join(*root, ciPinsPath))
	if err != nil {
		return err
	}
	pins, pp := readPins(pinData)
	problems = append(problems, pp...)
	tp, err := checkTree(*root, files, pins)
	if err != nil {
		return err
	}
	problems = append(problems, tp...)
	problems = append(problems, checkWorkflows(".github/workflows", generatedWorkflows(files))...)
	if len(problems) > 0 {
		return errors.New("schema-test ci --check:\n  " + strings.Join(problems, "\n  "))
	}
	_, err = fmt.Fprintf(stdout, "schema-test ci: the generated files are the registry's, every other .github file is pinned as it is (%s), and the generated workflows run tests only through schema-test\n", ciPinsPath)
	return err
}
