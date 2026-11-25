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
// one generated matrix row in a copy of the tree: red, by file and line.
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
	edited := strings.Replace(wf, `"shard":"1/`, `"shard":"9/`, 1)
	if edited == wf {
		t.Fatal("the generated matrix has no shard 1: the plant has nothing to edit")
	}
	if err := os.WriteFile(filepath.Join(root, fastWorkflow), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	want := fastWorkflow + ":"
	p := diffFiles(root, files)
	if len(p) != 1 || !strings.HasPrefix(p[0], want) || !strings.Contains(p[0], "is not what the registry generates") {
		t.Errorf("a hand edit to the generated matrix is not named red by file and line: %v", p)
	}
}

// TestTheMatrixIsThePRGroup: the generated rows, run as the jobs run them
// (--group pr --lang L --shard i/N), cover the registry's pr group exactly:
// every name once, none twice, none left out, and no shard past its budget
// (matrixAccount, which ci --check runs too). The plants: a row dropped, a
// row doubled, a shard cut too coarse, and a test heavier than a shard.
func TestTheMatrixIsThePRGroup(t *testing.T) {
	t.Parallel()
	reg := loadRepoRegistry(t)
	rows, problems := ciMatrix(reg, prShards)
	if len(problems) > 0 {
		t.Fatalf("the pr group does not cut into shards that fit:\n  %s", strings.Join(problems, "\n  "))
	}
	if p := matrixAccount(reg, rows, prShards); len(p) > 0 {
		t.Fatalf("the matrix is not the pr group:\n  %s", strings.Join(p, "\n  "))
	}
	has := func(p []string, s string) bool { return strings.Contains(strings.Join(p, "\n"), s) }
	// plant: one row dropped
	if p := matrixAccount(reg, rows[1:], prShards); !has(p, "left out") {
		t.Errorf("a matrix one row short is not named: %v", p)
	}
	// plant: one row doubled
	if p := matrixAccount(reg, append([]ciRow{rows[0]}, rows...), prShards); !has(p, "runs in 2 jobs, not 1") {
		t.Errorf("a matrix with a row twice is not named: %v", p)
	}
	// plant: a language's shards merged into one, past the budget
	var coarse []ciRow
	for _, r := range rows {
		if r.Lang != testreg.LangFree {
			coarse = append(coarse, r)
		}
	}
	coarse = append(coarse, ciRow{Lang: testreg.LangFree, Shard: "1/1"})
	if p := matrixAccount(reg, coarse, prShards); !has(p, "none 1/1 carries") {
		t.Errorf("a shard past its budget is not named: %v", p)
	}
	// plant: a language cut into one shard more than it needs (the
	// language-free rows, which every registry has, cut n+1 ways)
	var extra []ciRow
	n := 0
	for _, o := range rows {
		if o.Lang != testreg.LangFree {
			extra = append(extra, o)
		} else {
			n++
		}
	}
	if n == 0 {
		t.Fatal("the matrix has no language-free shard to plant on")
	}
	for i := 1; i <= n+1; i++ {
		extra = append(extra, ciRow{Lang: testreg.LangFree, Shard: fmt.Sprintf("%d/%d", i, n+1)})
	}
	if p := matrixAccount(reg, extra, prShards); !has(p, fmt.Sprintf("%s is cut into %d shards, and %d fit", testreg.LangFree, n+1, n)) {
		t.Errorf("a language cut into more shards than fit is not named: %v", p)
	}
	// plant: one language's shards cut two ways
	if p := matrixAccount(reg, append(append([]ciRow{}, rows...), ciRow{Lang: rows[0].Lang, Shard: "1/999"}), prShards); !has(p, "one count per language") {
		t.Errorf("a language cut two ways is not named: %v", p)
	}
	// plant: a row whose shard does not read
	if p := matrixAccount(reg, append(append([]ciRow{}, rows...), ciRow{Lang: rows[0].Lang, Shard: "x"}), prShards); !has(p, rows[0].Lang+" x: the shard does not read") {
		t.Errorf("a shard that does not read is not named: %v", p)
	}
	// plant: a shard past the language's count
	if p := matrixAccount(reg, append(append([]ciRow{}, rows...), ciRow{Lang: rows[0].Lang, Shard: "9/1"}), prShards); !has(p, rows[0].Lang+" 9/1:") {
		t.Errorf("a shard past its count is not named: %v", p)
	}
	// plant: a test heavier than a shard
	for i, tt := range reg.Tests {
		if tt.Scope == testreg.LangFree && tt.Tier == "unit" {
			was := tt.Seconds
			reg.Tests[i].Seconds = shardBudget + 1
			_, p := ciMatrix(reg, prShards)
			reg.Tests[i].Seconds = was
			if want := "none.unit." + tt.Key + " weighs 61.0 s, past one shard's 60 s budget"; !has(p, want) {
				t.Errorf("a test heavier than a shard was not named (want %q): %v", want, p)
			}
			break
		}
	}
}

// TestTheMatrixPlansHeadroomUnderTheLimit: the generated matrix cuts
// every shard of more than one test to the 50 s plan, under the 60 s limit,
// and a part of one test may carry up to the limit. The plants: the
// repository's matrix cut to the limit alone (no headroom), which the
// account names past the plan; and fits over hand-made parts, by the rule.
func TestTheMatrixPlansHeadroomUnderTheLimit(t *testing.T) {
	t.Parallel()
	if shardPlan >= shardBudget {
		t.Fatalf("the plan (%.0f s) leaves no headroom under the limit (%.0f s)", shardPlan, shardBudget)
	}
	reg := loadRepoRegistry(t)
	rows, problems := ciMatrix(reg, prShards)
	if len(problems) > 0 {
		t.Fatalf("the pr group does not cut into shards that fit:\n  %s", strings.Join(problems, "\n  "))
	}
	for _, r := range rows {
		if r.Seconds > shardPlan && r.Tests > 1 {
			t.Errorf("%s %s carries %.1f s over %d tests, past the %.0f s plan", r.Lang, r.Shard, r.Seconds, r.Tests, shardPlan)
		}
	}
	// plant: the matrix cut to the limit, with no headroom
	tight, problems := ciMatrix(reg, shardBudgets{plan: shardBudget, limit: shardBudget})
	if len(problems) > 0 {
		t.Fatalf("the pr group does not cut to the limit:\n  %s", strings.Join(problems, "\n  "))
	}
	if p := matrixAccount(reg, tight, prShards); !strings.Contains(strings.Join(p, "\n"), "plan") {
		t.Errorf("a matrix cut to the limit, with no headroom, is not named past the plan: %v", p)
	}
	inst := func(secs float64) testreg.Instance {
		return testreg.Instance{Test: testreg.Test{Seconds: secs}}
	}
	for _, tc := range []struct {
		what  string
		parts [][]testreg.Instance
		want  bool
	}{
		{"two tests under the plan", [][]testreg.Instance{{inst(20), inst(25)}}, true},
		{"two tests past the plan, under the limit", [][]testreg.Instance{{inst(30), inst(25)}}, false},
		{"one test past the plan, under the limit", [][]testreg.Instance{{inst(55)}}, true},
		{"one test past the limit", [][]testreg.Instance{{inst(61)}}, false},
		{"an empty part", [][]testreg.Instance{{inst(1)}, {}}, false},
	} {
		if got := fits(tc.parts, prShards); got != tc.want {
			t.Errorf("fits(%s) = %v, want %v", tc.what, got, tc.want)
		}
	}
}

// TestTheShardJobsFitGitHubsMatrixLimit: matrixJobs cuts the rows, in
// order, into jobs of at most maxMatrixJobs each, a language kept whole when
// it fits; the generated workflow's shard jobs are within the limit and,
// joined, are the whole matrix. The plant: a language past the limit alone,
// which is split, never left as one matrix past it.
func TestTheShardJobsFitGitHubsMatrixLimit(t *testing.T) {
	t.Parallel()
	reg := loadRepoRegistry(t)
	rows, problems := ciMatrix(reg, prShards)
	if len(problems) > 0 {
		t.Fatalf("the pr group does not cut into shards that fit:\n  %s", strings.Join(problems, "\n  "))
	}
	check := func(what string, rows []ciRow) [][]ciRow {
		t.Helper()
		jobs := matrixJobs(rows)
		var joined []ciRow
		for i, j := range jobs {
			if len(j) == 0 || len(j) > maxMatrixJobs {
				t.Errorf("%s: job %d has %d rows, want 1 to %d", what, i+1, len(j), maxMatrixJobs)
			}
			joined = append(joined, j...)
		}
		if !slices.Equal(joined, rows) {
			t.Errorf("%s: the jobs joined are not the matrix in order", what)
		}
		return jobs
	}
	check("the registry's matrix", rows)
	var big []ciRow
	for i := range maxMatrixJobs + 10 {
		big = append(big, ciRow{Lang: "c", Shard: fmt.Sprintf("%d/%d", i+1, maxMatrixJobs+10)})
	}
	big = append(big, ciRow{Lang: "go", Shard: "1/1"})
	if jobs := check("a language past the limit", big); len(jobs) != 2 || len(jobs[1]) != 11 {
		t.Errorf("a language past the limit: want jobs of %d and 11 rows, got %d jobs", maxMatrixJobs, len(jobs))
	}
	// a language that does not fit what is left starts the next job whole
	var two []ciRow
	for i := range maxMatrixJobs - 1 {
		two = append(two, ciRow{Lang: "c", Shard: fmt.Sprintf("%d/%d", i+1, maxMatrixJobs-1)})
	}
	two = append(two, ciRow{Lang: "go", Shard: "1/2"}, ciRow{Lang: "go", Shard: "2/2"})
	if jobs := check("a language that does not fit what is left", two); len(jobs) != 2 || len(jobs[1]) != 2 {
		t.Errorf("a language that does not fit what is left: want it whole in a second job, got %d jobs", len(jobs))
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
// skipped job, a missing job and an unreadable needs context are each named.
func TestTheVerdictNeedsEveryJobGreen(t *testing.T) {
	t.Parallel()
	green := `{"fork-pull-request":{"result":"skipped"},"schema-test":{"result":"success"},"fixed-corpus":{"result":"success"},"produce":{"result":"success"},"schema-test-pr-1":{"result":"success"},"schema-test-pr-2":{"result":"success"}}`
	if err := verdict([]byte(green), 2); err != nil {
		t.Fatalf("an all-green run: %v", err)
	}
	for in, says := range map[string]string{
		strings.Replace(green, `"schema-test-pr-2":{"result":"success"}`, `"schema-test-pr-2":{"result":"failure"}`, 1):   "schema-test-pr-2: failure",
		strings.Replace(green, `"schema-test-pr-1":{"result":"success"}`, `"schema-test-pr-1":{"result":"skipped"}`, 1):   "schema-test-pr-1: skipped",
		strings.Replace(green, `"schema-test":{"result":"success"}`, `"schema-test":{"result":"cancelled"}`, 1):           "schema-test: cancelled",
		strings.Replace(green, `"produce":{"result":"success"}`, `"produce":{"result":"failure"}`, 1):                     "produce: failure",
		strings.Replace(green, `"fork-pull-request":{"result":"skipped"}`, `"fork-pull-request":{"result":"failure"}`, 1): "fork-pull-request: failure",
		strings.Replace(green, `,"schema-test-pr-1":{"result":"success"}`, "", 1):                                         "has no job schema-test-pr-1",
		strings.Replace(green, `"schema-test-pr-2":{"result":"success"}}`, `"schema-test-pr-3":{"result":"success"}}`, 1): "has no job schema-test-pr-2",
		strings.Replace(green, `,"schema-test-pr-2":{"result":"success"}`, "", 1):                                         "has no job schema-test-pr-2",
		strings.Replace(green, `}}`, `},"schema-test-pr-3":{"result":"success"}}`, 1):                                     "has 3 shard jobs, and the registry cuts the pr group into 2",
		`{"schema-test":{"result":"success"}}`:                                                                            "has no job",
		`not json`:                                                                                                        "does not read",
	} {
		if err := verdict([]byte(in), 2); err == nil || !strings.Contains(err.Error(), says) {
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
