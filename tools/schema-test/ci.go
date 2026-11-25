package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// CI is generated from the registry (docs/TEST-REGISTRY.md, "CI"). Three
// workflows run tests, and every step of theirs that runs one calls only
// schema-test:
//
//   - ci-fast.yml, per pull request and on main: the registry's pr group, one
//     job per (language, shard). This file writes it whole, with
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

// shardBudget is the measured test seconds one ci-fast shard may carry:
// testreg's, the one budget the driver cuts calls by, so the two cannot
// drift. The job's cap is two minutes on a cold runner; the rest is the
// job's setup. A language's leg build is inside its adapter's first call, so
// it is inside the measured seconds.
const shardBudget = testreg.ShardBudget

// shardPlan is what the matrix plans a shard to carry: the fewest shards
// whose every part is at most this many measured seconds. The seconds are
// measured on a warm, quiet bench, and a cold CI runner takes longer, so the
// plan leaves headroom under the limit and buys it with more shards, never
// with lighter rows (2026-11-25). A part of one test, which cannot be cut,
// fits up to the limit. The limit itself, shardBudget, is unchanged.
const shardPlan = 50.0

// shardBudgets are the plan a shard is cut to and the limit no shard may
// pass.
type shardBudgets struct{ plan, limit float64 }

// prShards are the per-PR matrix's.
var prShards = shardBudgets{plan: shardPlan, limit: shardBudget}

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
	"produce":           "builds and shares a producer's tree (a leg the env plants run), held to the checkout's source",
	"main-tree":         "exports main's tree and ledger for schema-test extract",
	"dist-pins":         "the lua, luau, goose and lobster pins",
	"python":            "the python pins",
	"ruby":              "the ruby pins",
	"moo":               "the moo pin",
}

// producer is one row of ci-fast.yml's produce matrix: a tree a later job
// reads, built once per run (.github/actions/produce), never in every shard
// that needs it. Target is the make target that builds it, Paths what it
// makes. The language-free shards run the env plants
// (plant.corpus-plants.*), whose Prepare then finds these legs built.
//
// Measured cold on captain (2026-11-25, load 5-7, 64 cores): the go leg
// 55.0 s, the goose leg 9.3 s; replayed, make rebuilds nothing but the go
// driver's own go build. The cpp leg, the reference driver in its
// translation units (Makefile, CONFORMANCE_DRIVER_TUS), 72.2 s cold under
// make -j4 on 4 of captain's cores; replayed, make rebuilds nothing. Its
// tree carries each unit's recorded compile seconds, which the registry row
// harness-run.makefile.conformance-cpp-tu-budget holds to the budget beside
// that list in a language-free shard, after the fetch: a unit that grows
// back toward the old single one is red by name before the leg stops
// fitting. The lint rows build exactly the tree each lint gate reads
// (internal/ci TestEveryLintGateReadsAProducedTree), so a shard pays the
// lint and never the generation in front of it.
type producer struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Paths  string `json:"paths"`
}

var producers = []producer{
	{"conformance-leg-go", "conformance-leg-go", "bin/schema build/conformance-go build/conformance-harness-vm build/tables-counted-min-variable build/tables-generated-go build/tables-generated-go-announce build/tables-generated-go-framing build/tables-generated-go-r19 build/tables-lists-variable build/tables-r1-variable"},
	{"conformance-leg-goose", "conformance-leg-goose", "bin/schema build/conformance-goose build/conformance-harness-vm build/schema-goose"},
	{"conformance-leg-cpp", "conformance-leg-cpp", "bin/schema build/conformance-cpp build/conformance-cpp-objs build/conformance-harness-vm build/schema_test_cook build/tables-generated"},
	{"lint-c", "tables-c-clean-inputs", "bin/schema build/tables-generated-c generated/c generated/c-ludicrous generated/bench/c generated/bench/tables/c generated/bench/paired/c"},
	{"lint-cpp", "tables-cpp-clean-inputs", "bin/schema build/tables-generated generated/cpp generated/bench/cpp generated/bench/tables/cpp generated/bench/paired/cpp"},
	{"lint-js", "tables-js-clean-inputs", "bin/schema build/tables-generated-js generated/js generated/js-ludicrous generated/bench/js generated/bench/paired/js"},
	{"lint-rust", "tables-rust-clippy-inputs", "bin/schema build/tables-generated-rust build/tables-generated-rust-witnesses"},
	{"lint-lua", "tables-lua-luacheck-inputs", "bin/schema build/lua-luacheck-corpus build/lua-luacheck.rc"},
}

// ciRow is one job of ci-fast.yml's schema-test-pr matrix.
type ciRow struct {
	Lang    string  `json:"lang"`
	Shard   string  `json:"shard"`
	Tests   int     `json:"-"`
	Seconds float64 `json:"-"`
}

// ciMatrix cuts the pr group into rows: per language (and "none" for the
// language-free tests), the fewest shards whose every part carries at most
// budget measured seconds. The parts are testreg.Parts of exactly the list
// `schema-test run --group pr --lang L` selects, so the job runs what this
// counted. A test heavier than a shard is named: it fits no shard, however
// many there are, and is split into rows that fit, never moved out of the
// pull-request tier to fit.
func ciMatrix(reg *testreg.Registry, budget shardBudgets) ([]ciRow, []string) {
	g, ok := reg.Group("pr")
	if !ok {
		return nil, []string{"the registry has no group pr"}
	}
	all, err := reg.Select(g.Query())
	if err != nil {
		return nil, []string{fmt.Sprintf("group pr: %v", err)}
	}
	// a language none of whose tests is in the pr group has no job (the
	// nightly runs them); Select refuses a query that matches nothing
	perLang := map[string]int{}
	for _, inst := range all.Instances {
		perLang[inst.Lang]++
	}
	langs := append(reg.LangNames(), testreg.LangFree)
	var rows []ciRow
	var problems []string
	for _, l := range langs {
		if perLang[l] == 0 {
			continue
		}
		q := g.Query()
		q.Langs = []string{l}
		sel, err := reg.Select(q)
		if err != nil {
			problems = append(problems, fmt.Sprintf("group pr, language %s: %v", l, err))
			continue
		}
		insts := sel.Instances
		heavy := false
		for _, inst := range insts {
			if w := testreg.Weight(inst); w > budget.limit {
				problems = append(problems, fmt.Sprintf("%s weighs %.1f s, past one shard's %.0f s budget: split it into rows that fit (by package or target); a nightly tier is not the way under the budget", inst.Full, w, budget.limit))
				heavy = true
			}
		}
		if heavy {
			continue
		}
		n := leastShards(insts, budget)
		for ; n < len(insts); n++ {
			if fits(testreg.Parts(insts, n), budget) {
				break
			}
		}
		parts := testreg.Parts(insts, n)
		secs := testreg.PartSeconds(parts)
		for i := range parts {
			rows = append(rows, ciRow{Lang: l, Shard: fmt.Sprintf("%d/%d", i+1, n), Tests: len(parts[i]), Seconds: secs[i]})
		}
	}
	if len(problems) == 0 {
		problems = append(problems, matrixAccount(reg, rows, budget)...)
	}
	return rows, problems
}

// matrixAccount runs the matrix as its jobs will (--group pr --lang L
// --shard i/N) and holds it to the pr group: every name in exactly one job,
// none left out, no shard past budget. Each break is named.
func matrixAccount(reg *testreg.Registry, rows []ciRow, budget shardBudgets) []string {
	g, ok := reg.Group("pr")
	if !ok {
		return []string{"the registry has no group pr"}
	}
	all, err := reg.Select(g.Query())
	if err != nil {
		return []string{fmt.Sprintf("group pr: %v", err)}
	}
	var bad []string
	seen := map[string]int{}
	count := map[string]int{} // a language's shard count, from its first row
	// one selection per language: every row of a language selects the same
	// list
	sels := map[string]testreg.Selection{}
	for _, r := range rows {
		sel, ok := sels[r.Lang]
		if !ok {
			q := g.Query()
			q.Langs = []string{r.Lang}
			got, err := reg.Select(q)
			if err != nil {
				bad = append(bad, fmt.Sprintf("%s %s: %v", r.Lang, r.Shard, err))
				continue
			}
			sel = got
			sels[r.Lang] = sel
		}
		var i, n int
		if _, err := fmt.Sscanf(r.Shard, "%d/%d", &i, &n); err != nil {
			bad = append(bad, fmt.Sprintf("%s %s: the shard does not read: %v", r.Lang, r.Shard, err))
			continue
		}
		// the fewest shards that fit: one fewer than n would fit too is a
		// job too many, so the count moves down as rows get lighter or move
		if c, ok := count[r.Lang]; !ok {
			count[r.Lang] = n
			for m := leastShards(sel.Instances, budget); m < n; m++ {
				if fits(testreg.Parts(sel.Instances, m), budget) {
					bad = append(bad, fmt.Sprintf("%s is cut into %d shards, and %d fit: the matrix is the fewest shards that fit", r.Lang, n, m))
					break
				}
			}
		} else if c != n {
			bad = append(bad, fmt.Sprintf("%s %s: the language's shards are cut %d ways and %d ways: one count per language", r.Lang, r.Shard, c, n))
		}
		part, err := testreg.Shard(sel.Instances, i, n)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s %s: %v", r.Lang, r.Shard, err))
			continue
		}
		secs := 0.0
		for _, inst := range part {
			seen[inst.Full]++
			secs += testreg.Weight(inst)
		}
		switch {
		case secs > budget.limit:
			bad = append(bad, fmt.Sprintf("%s %s carries %.1f s, past %.0f s", r.Lang, r.Shard, secs, budget.limit))
		case secs > budget.plan && len(part) > 1:
			bad = append(bad, fmt.Sprintf("%s %s carries %.1f s, past the %.0f s plan (the limit is %.0f s; the plan is headroom for a cold runner, bought with shards)", r.Lang, r.Shard, secs, budget.plan, budget.limit))
		}
	}
	missing := 0
	for _, inst := range all.Instances {
		switch seen[inst.Full] {
		case 1:
		case 0:
			missing++
		default:
			bad = append(bad, fmt.Sprintf("%s runs in %d jobs, not 1", inst.Full, seen[inst.Full]))
		}
	}
	if missing > 0 || len(seen) != len(all.Instances) {
		bad = append(bad, fmt.Sprintf("the jobs run %d of the pr group's %d names: %d left out", len(seen), len(all.Instances), missing))
	}
	return bad
}

// maxMatrixJobs is the most jobs GitHub Actions lets one matrix make; a
// matrix past it fails the whole run before any job starts.
const maxMatrixJobs = 256

// prJobPrefix names the shard jobs: schema-test-pr-1, schema-test-pr-2, ...
const prJobPrefix = "schema-test-pr-"

// prJob is one shard job of ci-fast.yml: its name and its matrix.
type prJob struct {
	Name    string
	Include string
}

// matrixJobs cuts the matrix rows, in order, into jobs of at most
// maxMatrixJobs rows each. A language's shards stay in one job when they fit
// in what is left of it; a language past the limit on its own is split.
func matrixJobs(rows []ciRow) [][]ciRow {
	var jobs [][]ciRow
	var cur []ciRow
	for start := 0; start < len(rows); {
		end := start
		for end < len(rows) && rows[end].Lang == rows[start].Lang {
			end++
		}
		lang := rows[start:end]
		if len(cur)+len(lang) > maxMatrixJobs && len(cur) > 0 {
			jobs = append(jobs, cur)
			cur = nil
		}
		for len(lang) > maxMatrixJobs {
			jobs = append(jobs, lang[:maxMatrixJobs])
			lang = lang[maxMatrixJobs:]
		}
		cur = append(cur, lang...)
		start = end
	}
	if len(cur) > 0 {
		jobs = append(jobs, cur)
	}
	return jobs
}

// leastShards is the fewest parts that could fit insts: no part carries past
// the limit, so fewer than the weight over the limit cannot (and never fewer
// than one). The search for the fewest that fit starts here.
func leastShards(insts []testreg.Instance, budget shardBudgets) int {
	total := 0.0
	for _, inst := range insts {
		total += testreg.Weight(inst)
	}
	return max(1, int(math.Ceil(total/budget.limit)))
}

// fits: every part runs a test and carries at most the plan's seconds, or,
// a part of one test (which cannot be cut), at most the limit.
func fits(parts [][]testreg.Instance, budget shardBudgets) bool {
	for i, s := range testreg.PartSeconds(parts) {
		switch {
		case len(parts[i]) == 0, s > budget.limit:
			return false
		case s > budget.plan && len(parts[i]) > 1:
			return false
		}
	}
	return true
}

// generatedWorkflowFiles are the workflows ciFiles writes, by repository
// path (TestTheBaseLeavesOutExactlyTheGeneratedWorkflows holds the two to
// each other).
var generatedWorkflowFiles = []string{fastWorkflow, roadmapFile, nightlyWorkflow}

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
	rows, problems := ciMatrix(reg, prShards)
	if len(problems) > 0 {
		return nil, fmt.Errorf("the pr group does not cut into shards that fit:\n  %s", strings.Join(problems, "\n  "))
	}
	var jobs []prJob
	for i, chunk := range matrixJobs(rows) {
		include, err := json.Marshal(chunk)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, prJob{Name: fmt.Sprintf("%s%d", prJobPrefix, i+1), Include: string(include)})
	}
	produce, err := json.Marshal(producers)
	if err != nil {
		return nil, err
	}
	var wf bytes.Buffer
	if err := fastTemplate.Execute(&wf, map[string]any{"Jobs": jobs, "Producers": string(produce), "Budget": shardBudget, "Plan": shardPlan, "MaxJobs": maxMatrixJobs}); err != nil {
		return nil, err
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
// tier's counts, and every row of the per-PR matrix with what it carries.
func tiersPage(reg *testreg.Registry, rows []ciRow) []byte {
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
| ` + "`ci-fast.yml`" + ` | every pull request, every push to main, dispatch | ` + "`schema-test run --group pr --lang L --shard i/N`" + `, one job per row below; ` + "`schema-test extract`" + ` against main's ledger; ` + "`schema-test ci --check`" + `; the ` + "`verdict`" + ` job |
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
group, every ready test of tiers unit and functional, and a push to main runs
the same.

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
## The per-PR matrix

The `+"`pr`"+` group is cut per language into the fewest shards whose every part
carries at most %.0f s of measured test time, planned under the %.0f s limit
as headroom for a cold runner; a part of one test may carry up to the limit
(each test's `+"`seconds`"+` in the
registry; a test with none weighs %.0f s until a run measures it, so a shard of
unmeasured tests is held to the budget only once they are measured). Each row is
one job, in jobs of at most `+"`%d`"+` rows (GitHub's limit on one matrix),
`+"`schema-test run --group pr --lang L --shard i/N`"+`.

| language | shard | tests | measured seconds |
|---|---|---|---|
`, shardPlan, shardBudget, testreg.DefaultSeconds, maxMatrixJobs)
	jobs, tests, secs := 0, 0, 0.0
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %s | %d | %.1f |\n", r.Lang, r.Shard, r.Tests, r.Seconds)
		jobs++
		tests += r.Tests
		secs += r.Seconds
	}
	fmt.Fprintf(&b, "| **every row** | **%d jobs** | **%d** | **%.1f** |\n", jobs, tests, secs)
	b.WriteString(`
To change the matrix, change the registry (a row, or its measured
` + "`seconds`" + `: ` + "`schema-test run ... --write-seconds`" + ` on a fleet runner, or
` + "`schema-test seconds LOG...`" + ` from a run's log), then run
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
# THE PER-PR TIER. Every pull request, and every push to main, runs the
# registry's pr group whole: every ready test of tiers unit and functional,
# cut per language into the fewest shards planned at [[.Plan]] s of measured test
# time and none past [[.Budget]] s (an unmeasured test weighs 1 s), each job
# capped at two minutes. Nothing
# selects by path or branch. Every step that runs a test calls schema-test, and only it
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

  # the complete fixed corpus the language-free Go tests read, built once
  fixed-corpus:
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
      - uses: ./.github/actions/fixed-corpus
        with:
          mode: produce

  # the producers' trees (the legs the env plants run), each built once per
  # run, kept in the runner's cache by what it is built from, and shared
  produce:
    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository
    name: produce (${{ matrix.name }})
    runs-on: [self-hosted, Linux, X64, fleet, schema]
    timeout-minutes: 2
    defaults:
      run:
        shell: go run ./tools/pgroup -- bash -e -o pipefail {0}
    strategy:
      fail-fast: false
      matrix:
        include: [[.Producers]]
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
          lang: none
      - uses: ./.github/actions/produce
        with:
          mode: produce
          name: ${{ matrix.name }}
          target: ${{ matrix.target }}
          paths: ${{ matrix.paths }}

[[- range .Jobs]]

  # THE REGISTRY'S PR GROUP, one job per row of docs/CI-TIERS.md's matrix,
  # cut into jobs of at most [[$.MaxJobs]] rows (GitHub's limit on one matrix)
  [[.Name]]:
    needs: [fixed-corpus, produce]
    if: (always() && !cancelled() && needs.fixed-corpus.result == 'success' && needs.produce.result == 'success' && (github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository))
    name: schema-test-pr (${{ matrix.lang }} ${{ matrix.shard }})
    runs-on: [self-hosted, Linux, X64, fleet, schema]
    timeout-minutes: 2
    defaults:
      run:
        shell: go run ./tools/pgroup -- bash -e -o pipefail {0}
    strategy:
      fail-fast: false
      matrix:
        include: [[.Include]]
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
          lang: ${{ matrix.lang }}
      - uses: ./.github/actions/fixed-corpus
        if: matrix.lang == 'none'
        with:
          mode: fetch
      - uses: ./.github/actions/produce
        if: matrix.lang == 'none'
        with:
          mode: fetch
      - name: schema-test run --group pr --lang ${{ matrix.lang }} --shard ${{ matrix.shard }}
        env:
          # tooling.caa.self-test reads main's tip from GitHub, as caa-self-test.yml did
          GITHUB_TOKEN: ${{ github.token }}
        run: go run ./tools/schema-test run --group pr --lang "${{ matrix.lang }}" --shard "${{ matrix.shard }}"
[[- end]]

  # green only when every job above ran and passed: each shard's run fails on
  # expected != ran, and ci --check holds the matrix to the whole pr group
  verdict:
    needs: [fork-pull-request, schema-test, fixed-corpus, produce[[range .Jobs]], [[.Name]][[end]]]
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

// verdict reads the needs context of the verdict job: every job passed, and
// the fork guard alone may be skipped (it runs only for a fork's pull
// request). A job skipped, cancelled or failed is named. shardJobs is how
// many shard jobs the registry cuts the pr group into: the needs context
// holds exactly those, so a dropped last job is named too.
func verdict(needs []byte, shardJobs int) error {
	var m map[string]struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(needs, &m); err != nil {
		return fmt.Errorf("the needs context does not read: %w", err)
	}
	for _, want := range []string{"fork-pull-request", "schema-test", "fixed-corpus", "produce", prJobPrefix + "1"} {
		if _, ok := m[want]; !ok {
			return fmt.Errorf("the needs context has no job %s", want)
		}
	}
	// the shard jobs are numbered 1..shardJobs: a dropped one is named, and
	// so is one the registry does not cut
	shards := 0
	for job := range m {
		if strings.HasPrefix(job, prJobPrefix) {
			shards++
		}
	}
	for i := 1; i <= shardJobs; i++ {
		if _, ok := m[fmt.Sprintf("%s%d", prJobPrefix, i)]; !ok {
			return fmt.Errorf("the needs context has no job %s%d", prJobPrefix, i)
		}
	}
	if shards != shardJobs {
		return fmt.Errorf("the needs context has %d shard jobs, and the registry cuts the pr group into %d", shards, shardJobs)
	}
	var bad []string
	for job, v := range m {
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
		rows, problems := ciMatrix(reg, prShards)
		if len(problems) > 0 {
			return fmt.Errorf("the pr group does not cut into shards that fit:\n  %s", strings.Join(problems, "\n  "))
		}
		if err := verdict([]byte(os.Getenv("NEEDS")), len(matrixJobs(rows))); err != nil {
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
