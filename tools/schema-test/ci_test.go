package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// repoRoot is the repository, from this package's directory.
const repoRoot = "../.."

func loadRepoRegistry(t *testing.T) *testreg.Registry {
	t.Helper()
	reg, err := testreg.Load(filepath.Join(repoRoot, testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// TestTheCIFilesAreCurrent: ci-fast.yml and docs/CI-TIERS.md are exactly
// what the registry generates (schema-test ci). The plant is a hand edit of
// the generated pr job in a copy of the tree: red, by file and line.
func TestTheCIFilesAreCurrent(t *testing.T) {
	t.Parallel()
	reg := loadRepoRegistry(t)
	files, err := ciFiles(reg)
	if err != nil {
		t.Fatal(err)
	}
	if p := diffFiles(repoRoot, files); len(p) > 0 {
		t.Fatalf("the CI files are not the registry's:\n  %s", strings.Join(p, "\n  "))
	}
	// the plant: the matrix edited by hand
	root := t.TempDir()
	for p, data := range files {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if p := diffFiles(root, files); len(p) > 0 {
		t.Fatalf("a copy of the generated files differs from them: %v", p)
	}
	wf := string(files[fastWorkflow])
	edited := strings.Replace(wf, "lang: all\n", "lang: none\n", 1)
	if edited == wf {
		t.Fatal("the generated pr job sets up no language: the plant has nothing to edit")
	}
	if err := os.WriteFile(filepath.Join(root, fastWorkflow), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	want := fastWorkflow + ":"
	p := diffFiles(root, files)
	if len(p) != 1 || !strings.HasPrefix(p[0], want) || !strings.Contains(p[0], "is not what the registry generates") {
		t.Errorf("a hand edit to the generated pr job is not named red by file and line: %v", p)
	}
}

// prRegistry writes a registry of two languages with the given test lines
// and pr group query, and loads it.
func prRegistry(t *testing.T, tests, query string) *testreg.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	lines := `{"rec":"lang","name":"c","write":"assert","adapter":"fake"}
{"rec":"lang","name":"go","write":"refuse","adapter":"fake"}
{"rec":"tier","name":"functional","what":"a toolchain"}
{"rec":"tier","name":"nightly","what":"heavy"}
{"rec":"type","name":"roundtrip","what":"same bytes back"}
{"rec":"type","name":"tooling","what":"policy"}
` + tests + `{"rec":"group","name":"pr","what":"per pull request","query":` + query + `}
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := testreg.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// prTestLines: the happy-path row every language answers, each language
// measured at the seconds given, and beside it a second happy path, a
// conformance round trip, a row measured only as a whole, an unmeasured
// happy path and a language-free row.
func prTestLines(c, gol float64) string {
	return fmt.Sprintf(`{"rec":"test","key":"roundtrip.happy-path.ship","type":"roundtrip","family":"happy-path","case":"ship","tier":"functional","checks":"x","inputs":{"n":1},"expect":"ok","lang_seconds":{"c":%g,"go":%g}}
{"rec":"test","key":"roundtrip.happy-path.second","type":"roundtrip","family":"happy-path","case":"second","tier":"functional","checks":"x","inputs":{"n":2},"expect":"ok","lang_seconds":{"c":1,"go":1}}
{"rec":"test","key":"roundtrip.conformance-wire.full","type":"roundtrip","family":"conformance-wire","case":"full","tier":"functional","checks":"x","inputs":{"n":3},"expect":"same","lang_seconds":{"c":1,"go":1}}
{"rec":"test","key":"roundtrip.happy-path.whole","type":"roundtrip","family":"happy-path","case":"whole","tier":"functional","checks":"x","inputs":{"n":4},"expect":"ok","seconds":1}
{"rec":"test","key":"roundtrip.happy-path.unmeasured","type":"roundtrip","family":"happy-path","case":"unmeasured","tier":"functional","checks":"x","inputs":{"n":5},"expect":"ok"}
{"rec":"test","key":"tooling.fam.free","type":"tooling","family":"fam","case":"free","tier":"functional","scope":"none","adapter":"fake","checks":"x","inputs":{"n":6},"expect":"passes","seconds":1}
`, c, gol)
}

// prFixtureTiming is a pr job of two lanes, 10 s of setup, and a run
// measured at 5 s.
var prFixtureTiming = prRunTiming{Jobs: 2, Setup: 10, Run: 5}

// TestThePRGroupIsOneHappyPathRowPerLanguage: the pr group holds, for every
// language, exactly one happy-path row (make happy-<lang>, the language's
// compile and its happy path), measured by its language's own seconds, and
// the job, its setup and the run's critical path, within the budget; each
// break is named. The plants: a second happy path, a conformance row, a
// language-free row, a language missing its happy path, a row measured only
// as a whole, an unmeasured row, the critical path past the budget, and the
// measured run past it.
func TestThePRGroupIsOneHappyPathRowPerLanguage(t *testing.T) {
	t.Parallel()
	both := []string{"c.functional.roundtrip.happy-path.ship", "go.functional.roundtrip.happy-path.ship"}
	names := func(rows ...string) string {
		var q []string
		for _, n := range rows {
			q = append(q, fmt.Sprintf("%q", n))
		}
		return `{"names":[` + strings.Join(q, ",") + `]}`
	}
	with := func(extra ...string) string { return names(append(append([]string{}, both...), extra...)...) }
	rows, p := prGroupTimed(prRegistry(t, prTestLines(3, 4), with()), prFixtureTiming)
	if len(p) > 0 {
		t.Fatalf("a pr group of one happy path per language: %v", p)
	}
	if len(rows) != 2 || rows[0].Lang != "c" || rows[0].Happy != "c.functional.roundtrip.happy-path.ship" || rows[0].Seconds != 3 || rows[1].Seconds != 4 {
		t.Errorf("the rows: %+v", rows)
	}
	for _, c := range []struct {
		what, tests, query, want string
		timing                   prRunTiming
	}{
		{"a second happy path", prTestLines(3, 4), with("c.functional.roundtrip.happy-path.second"),
			"c.functional.roundtrip.happy-path.ship and c.functional.roundtrip.happy-path.second are both happy paths of c: the pr group holds one per language", prFixtureTiming},
		{"a conformance row", prTestLines(3, 4), with("go.functional.roundtrip.conformance-wire.full"),
			"go.functional.roundtrip.conformance-wire.full is family conformance-wire, and the pr group holds per language one happy-path row (family happy-path: make happy-<lang>, the compile and the happy path), nothing else: it runs nightly", prFixtureTiming},
		{"a language-free row", prTestLines(3, 4), with("none.functional.tooling.fam.free"),
			"none.functional.tooling.fam.free is in the pr group, and the pr group holds per language one happy-path row (family happy-path), never a language-free row: it runs nightly", prFixtureTiming},
		{"a language missing its happy path", prTestLines(3, 4), names("c.functional.roundtrip.happy-path.ship"),
			"go has no happy-path row (family happy-path) in the pr group", prFixtureTiming},
		{"a row measured only as a whole", prTestLines(3, 4), names("c.functional.roundtrip.happy-path.whole", "go.functional.roundtrip.happy-path.ship"),
			"c.functional.roundtrip.happy-path.whole has no measured seconds", prFixtureTiming},
		{"an unmeasured row", prTestLines(3, 4), names("c.functional.roundtrip.happy-path.unmeasured", "go.functional.roundtrip.happy-path.ship"),
			"c.functional.roundtrip.happy-path.unmeasured has no measured seconds", prFixtureTiming},
		{"the critical path past the budget", prTestLines(110.5, 3), with(),
			"the pr job takes 120.5 s, past its 120 s: setup 10.0 s and the run's critical path 110.5 s on 2 lanes", prFixtureTiming},
		{"the measured run past the budget", prTestLines(3, 4), with(),
			"the pr job takes 120.5 s, past its 120 s", prRunTiming{Jobs: 2, Setup: 10, Run: 110.5}},
	} {
		_, p := prGroupTimed(prRegistry(t, c.tests, c.query), c.timing)
		if !strings.Contains(strings.Join(p, "\n"), c.want) {
			t.Errorf("%s: want %q, got %v", c.what, c.want, p)
		}
	}
	// the calls go longest first to the least loaded lane: c 9 s, go 6 s and
	// two of 4 s on two lanes is 9+4 and 6+4, 13 s; one lane is the sum
	for _, c := range []struct {
		jobs int
		want float64
	}{{2, 13}, {1, 23}, {0, 23}, {4, 9}} {
		rows := []prRow{{Lang: "a", Seconds: 4}, {Lang: "c", Seconds: 9}, {Lang: "d", Seconds: 4}, {Lang: "go", Seconds: 6}}
		if got := prCriticalPath(rows, c.jobs); got != c.want {
			t.Errorf("%d lanes: the critical path is %v s, want %v s", c.jobs, got, c.want)
		}
	}
	// the budget is the job's two minutes, never more
	if prJobBudget != 120 {
		t.Errorf("the pr job's budget is %.0f s, and Glenn's is 120 s", prJobBudget)
	}
}

// TestTheRepositorysPRGroupFitsTheJob: the registry's own pr group is one
// compile row and one happy-path row for each of its languages, within the
// job's budget.
func TestTheRepositorysPRGroupFitsTheJob(t *testing.T) {
	t.Parallel()
	reg := loadRepoRegistry(t)
	rows, p := prGroup(reg)
	if len(p) > 0 {
		t.Fatalf("the registry's pr group:\n  %s", strings.Join(p, "\n  "))
	}
	if len(rows) != len(reg.LangNames()) {
		t.Errorf("%d languages in the pr job, and the registry has %d", len(rows), len(reg.LangNames()))
	}
}

// TestCIFastRunsThePRGroupInOneJob: the generated ci-fast.yml runs tests in
// one step, schema-test run --group pr, whole. The plants: a second
// test-running job, a shard of the group, and no run at all.
func TestCIFastRunsThePRGroupInOneJob(t *testing.T) {
	t.Parallel()
	files, err := ciFiles(loadRepoRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(files[fastWorkflow])
	if p := checkFastRuns([]byte(wf)); len(p) > 0 {
		t.Fatalf("the generated ci-fast.yml: %v", p)
	}
	second := wf + "\n  schema-test-pr-2:\n    runs-on: x\n    steps:\n      - name: more\n        run: go run ./tools/schema-test run --group pr --lang c\n"
	sharded := strings.Replace(wf, "run: "+prRunLine+"\n", "run: "+prRunLine+" --shard 1/2\n", 1)
	none := strings.Replace(wf, "run: "+prRunLine+"\n", "run: go run ./tools/schema-test ci --check\n", 1)
	if sharded == wf || none == wf {
		t.Fatal("the generated ci-fast.yml has no pr run line to plant")
	}
	for what, c := range map[string]struct{ text, want string }{
		"a second test-running job": {second, "2 steps run tests per pull request, and the pr group runs in one job, one step"},
		"a shard of the group":      {sharded, `the pr job runs "go run ./tools/schema-test run --group pr", the pr group whole, and nothing else`},
		"no run":                    {none, "no step runs"},
	} {
		if p := checkFastRuns([]byte(c.text)); !strings.Contains(strings.Join(p, "\n"), c.want) {
			t.Errorf("%s: want %q, got %v", what, c.want, p)
		}
	}
}

// cleanWorkflows is a policy-clean pair: a fast lane and the nightly.
func cleanWorkflows() map[string][]byte {
	return map[string][]byte{
		"ci-fast.yml": []byte(`name: CI fast
on:
  pull_request:
jobs:
  a:
    runs-on: x
    defaults:
      run:
        shell: bash
    steps:
      - uses: ./.github/actions/schema-test-setup
      - name: run
        run: go run ./tools/schema-test run --group pr --lang "${{ matrix.lang }}" --shard 1/2
      - name: block
        run: |
          set -euo pipefail
          # a comment
          go run ./tools/schema-test ci --check
`),
		"schema-nightly.yml": []byte("name: n\non:\n  schedule:\n    - cron: '1 2 * * *'\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@abc # v1\n      - run: go run ./tools/fullrun launch --sha \"$GITHUB_SHA\" --part \"${{ matrix.part }}/24\"\n      - uses: actions/upload-artifact@abc # v1\n"),
	}
}

// TestEveryWorkflowStepCallsOnlySchemaTest: in the repository's workflows,
// every step that runs a check calls schema-test (or the full run that
// calls it), with nothing chained after it; only schema-nightly.yml has a
// schedule, and it runs the nightly group. The plants, each red by file and
// line, run before the repository is checked, so a red repository never
// hides a plant that stopped biting.
func TestEveryWorkflowStepCallsOnlySchemaTest(t *testing.T) {
	t.Parallel()
	clean := cleanWorkflows()
	if p := checkWorkflows(".github/workflows", clean); len(p) != 0 {
		t.Fatalf("a clean pair of workflows is red: %v", p)
	}
	fast := string(clean["ci-fast.yml"])
	night := string(clean["schema-nightly.yml"])
	for name, c := range map[string]struct {
		file, text, says string
	}{
		"a go test step": {"ci-fast.yml", fast + "      - run: go test ./internal/ci\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go test ./internal/ci"`},
		"a go test line in a block": {"ci-fast.yml", strings.Replace(fast, "# a comment\n", "# a comment\n          go test -run TestX ./tools/x\n", 1),
			`.github/workflows/ci-fast.yml:18: a step runs "go test -run TestX ./tools/x"`},
		"a make gate": {"ci-fast.yml", fast + "      - run: make schema-c-runs-hostile-corpus\n",
			`.github/workflows/ci-fast.yml:19: a step runs "make schema-c-runs-hostile-corpus"`},
		"a check chained after the driver": {"ci-fast.yml", fast + "      - run: go run ./tools/schema-test list ; make test-c\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go run ./tools/schema-test list ; make test-c"`},
		"the driver's failure swallowed": {"ci-fast.yml", fast + "      - run: go run ./tools/schema-test run --group pr || true\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go run ./tools/schema-test run --group pr || true"`},
		"a check inside an echo": {"ci-fast.yml", fast + "      - run: echo $(make test-c)\n",
			`.github/workflows/ci-fast.yml:19: a step runs "echo $(make test-c)"`},
		"a check after an echo": {"ci-fast.yml", fast + "      - run: echo hi; make test-c\n",
			`.github/workflows/ci-fast.yml:19: a step runs "echo hi; make test-c"`},
		"the driver re-pinning goldens": {"ci-fast.yml", fast + "      - run: go run ./tools/schema-test run --group pr --update\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go run ./tools/schema-test run --group pr --update"`},
		"the driver with a raised limit": {"ci-fast.yml", fast + "      - run: go run ./tools/schema-test run --group pr --timeout 100h\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go run ./tools/schema-test run --group pr --timeout 100h"`},
		"the driver on another tree": {"ci-fast.yml", fast + "      - run: go run ./tools/schema-test ci --check --root ../other\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go run ./tools/schema-test ci --check --root ../other"`},
		"a shell that runs make": {"ci-fast.yml", fast + "      - shell: make -f {0}\n        run: go run ./tools/schema-test list\n",
			`.github/workflows/ci-fast.yml:19: a step runs under shell "make -f {0}"`},
		"set -n ahead of the driver": {"ci-fast.yml", strings.Replace(fast, "set -euo pipefail\n", "set -n\n", 1),
			`.github/workflows/ci-fast.yml:16: a step runs "set -n"`},
		"exit 0 ahead of the driver": {"ci-fast.yml", strings.Replace(fast, "# a comment\n", "exit 0\n", 1),
			`.github/workflows/ci-fast.yml:17: a step runs "exit 0"`},
		"the driver listing, not running": {"ci-fast.yml", strings.Replace(fast, "schema-test ci --check", "schema-test list --group pr", 1),
			`.github/workflows/ci-fast.yml:18: a step runs "go run ./tools/schema-test list --group pr"`},
		"a matrix past GitHub's limit": {"ci-fast.yml", fast + "    strategy:\n      matrix:\n        include: [" + strings.Repeat(`{"lang":"c"},`, maxMatrixJobs) + `{"lang":"c"}]` + "\n",
			fmt.Sprintf(".github/workflows/ci-fast.yml:21: a matrix of %d jobs, past GitHub's %d", maxMatrixJobs+1, maxMatrixJobs)},
		"exit 256, which the shell reads as 0": {"ci-fast.yml", strings.Replace(fast, "# a comment\n", "exit 256\n", 1),
			`.github/workflows/ci-fast.yml:17: a step runs "exit 256"`},
		"a run value on the line below": {"ci-fast.yml", fast + "      - name: x\n        run:\n          make test-c\n",
			`.github/workflows/ci-fast.yml:21: a step runs "make test-c"`},
		"a space before run's colon": {"ci-fast.yml", fast + "      - run : make test-c\n",
			`.github/workflows/ci-fast.yml:19: a step runs "make test-c"`},
		"a block scalar that keeps its newlines": {"ci-fast.yml", fast + "      - run: |+\n          make test-c\n",
			`.github/workflows/ci-fast.yml:20: a step runs "make test-c"`},
		"an input expanded into the driver's line": {"ci-fast.yml", fast + "      - run: go run ./tools/schema-test run ${{ inputs.extra }}\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go run ./tools/schema-test run ${{ inputs.extra }}"`},
		"an expression whose braces nest": {"ci-fast.yml", fast + "      - run: go run ./tools/schema-test run ${{ format('{0}', github.event.pull_request.title) }}\n",
			`.github/workflows/ci-fast.yml:19: a step runs "go run ./tools/schema-test run ${{ format('{0}', github.event.pull_request.title) }}"`},
		"an echo of an expression whose braces nest": {"ci-fast.yml", fast + "      - run: echo ${{ format('{0}', github.event.pull_request.title) }}\n",
			`.github/workflows/ci-fast.yml:19: a step runs "echo ${{ format('{0}', github.event.pull_request.title) }}"`},
		"a comment after run: and the command below it": {"ci-fast.yml", fast + "      - name: x\n        run: # note\n          make test-c\n",
			`.github/workflows/ci-fast.yml:21: a step runs "make test-c"`},
		"a matrix include list that is not JSON": {"ci-fast.yml", fast + "    strategy:\n      matrix:\n        include: [nope]\n",
			".github/workflows/ci-fast.yml:21: a matrix include list that does not read as JSON"},
		"a third-party action": {"ci-fast.yml", fast + "      - uses: golangci/golangci-lint-action@v6\n",
			".github/workflows/ci-fast.yml:19: a step uses golangci/golangci-lint-action, an action that may run a check"},
		"a local action that runs a check": {"ci-fast.yml", fast + "      - uses: ./.github/actions/eslint\n",
			".github/workflows/ci-fast.yml:19: a step uses ./.github/actions/eslint, which is no setup action"},
		"a new scheduled workflow": {"schema-lua-nightly-fuzz.yml", "name: x\non:\n  schedule:\n    - cron: '1 2 * * *'\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: go run ./tools/schema-test run --group nightly\n",
			".github/workflows/schema-lua-nightly-fuzz.yml:3: a schedule or workflow_run trigger: only .github/workflows/schema-nightly.yml runs on a schedule"},
		"a schedule in a flow mapping": {"x.yml", "name: x\non: { schedule: [ { cron: '1 2 * * *' } ] }\njobs: {}\n",
			".github/workflows/x.yml:2: a schedule or workflow_run trigger"},
		"a workflow_run trigger": {"x.yml", "name: x\non:\n  workflow_run:\n    workflows: [CI fast]\njobs: {}\n",
			".github/workflows/x.yml:3: a schedule or workflow_run trigger"},
		"the nightly gone": {"schema-nightly.yml", "name: n\non:\n  workflow_dispatch:\njobs: {}\n",
			".github/workflows/schema-nightly.yml: no scheduled step runs the nightly group"},
		"the nightly with no driver": {"schema-nightly.yml", strings.Replace(night, "go run ./tools/fullrun launch", "echo launch", 1),
			".github/workflows/schema-nightly.yml: no scheduled step runs the nightly group"},
	} {
		files := cleanWorkflows()
		files[c.file] = []byte(c.text)
		p := checkWorkflows(".github/workflows", files)
		if !strings.Contains(strings.Join(p, "\n"), c.says) {
			t.Errorf("%s: want %q in:\n  %s", name, c.says, strings.Join(p, "\n  "))
		}
	}
	// a key after a "- run: |" block is the step's, not the command's
	sibling := cleanWorkflows()
	sibling["ci-fast.yml"] = []byte(fast + "      - run: |\n          go run ./tools/schema-test ci --check\n        shell: bash\n")
	if p := checkWorkflows(".github/workflows", sibling); len(p) != 0 {
		t.Errorf("a shell: key after a run block is read as a command: %v", p)
	}
	// a comment in a run block runs nothing, whatever it holds
	comment := cleanWorkflows()
	comment["ci-fast.yml"] = []byte(strings.Replace(fast, "# a comment\n", "# a note; (with punctuation) | and more\n", 1))
	if p := checkWorkflows(".github/workflows", comment); len(p) != 0 {
		t.Errorf("a comment line in a run block is read as a command: %v", p)
	}
	// the workflows schema-test ci generates hold to the rule: the plant is
	// a go test step added to the generated ci-fast.yml
	files, err := ciFiles(loadRepoRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	gen := generatedWorkflows(files)
	if len(gen) != 3 {
		t.Fatalf("schema-test ci generates %d workflows, want ci-fast.yml, roadmap.yml and schema-nightly.yml", len(gen))
	}
	if p := checkWorkflows(".github/workflows", gen); len(p) > 0 {
		t.Errorf("generated workflow steps that run a check other than schema-test (%d):\n  %s", len(p), strings.Join(p, "\n  "))
	}
	planted := maps.Clone(gen)
	planted["ci-fast.yml"] = append(slices.Clone(gen["ci-fast.yml"]), "      - run: go test ./...\n"...)
	if p := checkWorkflows(".github/workflows", planted); !strings.Contains(strings.Join(p, "\n"), "a step runs \"go test ./...\"") {
		t.Errorf("a go test step in the generated ci-fast.yml is not named: %v", p)
	}
}

// TestTheVerdictNeedsEveryJobGreen: the verdict passes only when every job
// it needs passed, with the fork guard alone skipped; a failed, cancelled or
// skipped job, a missing job, a job the workflow does not generate and an
// unreadable needs context are each named.
func TestTheVerdictNeedsEveryJobGreen(t *testing.T) {
	t.Parallel()
	green := `{"fork-pull-request":{"result":"skipped"},"schema-test":{"result":"success"},"schema-test-pr":{"result":"success"}}`
	if err := verdict([]byte(green)); err != nil {
		t.Fatalf("an all-green run: %v", err)
	}
	for in, says := range map[string]string{
		strings.Replace(green, `"schema-test-pr":{"result":"success"}`, `"schema-test-pr":{"result":"failure"}`, 1):       "schema-test-pr: failure",
		strings.Replace(green, `"schema-test-pr":{"result":"success"}`, `"schema-test-pr":{"result":"skipped"}`, 1):       "schema-test-pr: skipped",
		strings.Replace(green, `"schema-test":{"result":"success"}`, `"schema-test":{"result":"cancelled"}`, 1):           "schema-test: cancelled",
		strings.Replace(green, `}}`, `},"produce":{"result":"success"}}`, 1):                                              "produce: a job ci-fast.yml does not generate",
		strings.Replace(green, `"fork-pull-request":{"result":"skipped"}`, `"fork-pull-request":{"result":"failure"}`, 1): "fork-pull-request: failure",
		strings.Replace(green, `,"schema-test-pr":{"result":"success"}`, "", 1):                                           "has no job schema-test-pr",
		strings.Replace(green, `}}`, `},"schema-test-pr-2":{"result":"success"}}`, 1):                                     "schema-test-pr-2: a job ci-fast.yml does not generate",
		`{"schema-test":{"result":"success"}}`:                                                                            "has no job",
		`not json`:                                                                                                        "does not read",
	} {
		if err := verdict([]byte(in)); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: want %q, got %v", in, says, err)
		}
	}
}

// readWorkflows reads every .yml under root's .github/workflows.
func readWorkflows(root string) (map[string][]byte, error) {
	dir := filepath.Join(root, ".github", "workflows")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = data
	}
	return out, nil
}
