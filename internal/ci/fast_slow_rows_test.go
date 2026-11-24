package ci

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// buildSlowRows builds tools/slowrows once for a test.
func buildSlowRows(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "slowrows")
	cmd := exec.Command("go", "build", "-o", bin, "./tools/slowrows")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./tools/slowrows: %v\n%s", err, out)
	}
	return bin
}

// emitterPackages names every package directory under internal/codegen that
// holds a test file, as ./internal/codegen/... paths.
func emitterPackages(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var pkgs []string
	err := filepath.WalkDir(filepath.Join(root, "internal", "codegen"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			rel, err := filepath.Rel(root, filepath.Dir(p))
			if err != nil {
				return err
			}
			if pkg := "./" + filepath.ToSlash(rel); !slices.Contains(pkgs, pkg) {
				pkgs = append(pkgs, pkg)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) < 30 {
		t.Fatalf("found %d emitter packages with tests under internal/codegen; the walk is broken", len(pkgs))
	}
	return pkgs
}

// TestEveryEmitterPackageHasASlowRow holds ci-fast's go-test-slow to every
// emitter package: tools/slowrows, over ci-full.yml's own go-test rows, must
// select at least one row for each package under internal/codegen that has
// tests, so a pull request touching any of them runs its SCHEMA_SLOW tests.
//
// THE PLANT: the unit-codegen complement learns to drop internal/codegen/rust
// and no row takes it up. The select a pull request touching that package
// would run must then be red, naming it, rather than select nothing.
func TestEveryEmitterPackageHasASlowRow(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	bin := buildSlowRows(t)
	// one select over every emitter package: select refuses, naming each, any
	// package under internal/codegen with tests that no row tests
	cmd := exec.Command(bin, "select", strings.Join(emitterPackages(t), " "))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("a pull request touching an emitter package would select no go-test-slow row for it: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) == "[]" {
		t.Fatal("every emitter package together selects no go-test-slow row")
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	const complementEnd = "| grep -v '/lobstertable$' | grep -v '/lobster$')"
	if !strings.Contains(string(wf), complementEnd) {
		t.Fatalf("ci-full.yml's unit-codegen complement no longer ends %q; move the plant with it", complementEnd)
	}
	planted := strings.Replace(string(wf), complementEnd, "| grep -v '/lobstertable$' | grep -v '/lobster$' | grep -v '/rust$')", 1)
	plantPath := filepath.Join(t.TempDir(), "ci-full.yml")
	if err := os.WriteFile(plantPath, []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(bin, "select", "-workflow", plantPath, "./internal/codegen/rust")
	cmd.Dir = root
	out, err = cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "./internal/codegen/rust") {
		t.Fatalf("PLANT NOT CAUGHT: with internal/codegen/rust in no slow row, select said %v:\n%s", err, out)
	}
}

// TestCIFastRunsTheSlowRowsPerPullRequest holds ci-fast.yml's wiring: the
// plan selects the rows from its finalized go test selection, the
// go-test-slow job runs one row per job inside the two-minute cap with the
// fixed-form oracle, holds each row to its test count and its skips to the
// ledger, and the verdict reads it.
func TestCIFastRunsTheSlowRowsPerPullRequest(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["ci-fast.yml"]
	if !ok {
		t.Fatal("ci-fast.yml is missing")
	}
	jobs := yamlJobs(wf)
	plan, slow := jobs["plan"], jobs["go-test-slow"]
	if slow == "" {
		t.Fatal("ci-fast.yml has no go-test-slow job: no pull request runs the emitter packages' SCHEMA_SLOW tests")
	}
	for _, want := range []string{
		"slow_rows: ${{ steps.slow.outputs.slow_rows }}",
		`go run ./tools/slowrows select $all "$(cat "$RUNNER_TEMP/selected-packages.txt")"`,
		`'^(ir/|compiler/|go\.mod$|SIBLINGS$|tools/slowrows/)'`,
		"SERIALIZE[A-Z_]*_(PIN|TAG|COMMIT)",
	} {
		if !strings.Contains(plan, want) {
			t.Errorf("ci-fast.yml plan lost %q", want)
		}
	}
	for _, want := range []string{
		"needs: [plan, go-fixed-corpus]",
		"timeout-minutes: 2\n",
		"include: ${{ fromJSON(needs.plan.outputs.slow_rows) }}",
		"sh tools/go-ci-corpus.sh verify",
		`go build -o "$RUNNER_TEMP/slowrows" ./tools/slowrows`,
		"name: go-test-slow (${{ matrix.name }})",
		`"$RUNNER_TEMP/slowrows" run -row "$ROW" -part "$PART"`,
		`"$RUNNER_TEMP/slowrows" verify -row "$ROW" -part "$PART" "$PGROUP_GO_TEST_JSON"`,
		"if: always() && steps.tests.outcome != 'skipped'",
		`-provides "platform tool:make tool:git tool:sh short slow sibling corpus tool:node"`,
	} {
		if !strings.Contains(slow, want) {
			t.Errorf("ci-fast.yml go-test-slow lost %q", want)
		}
	}
	if regexp.MustCompile(`timeout-minutes: *([3-9]|[1-9][0-9])`).MatchString(slow) {
		t.Error("go-test-slow carries a limit over two minutes: re-cut the row in ci-full.yml instead")
	}

	ev := ciEvent{
		Name:         "pull_request",
		Branch:       "main",
		HeadRef:      "topic",
		Repo:         "mas-bandwidth/schema",
		HeadRepo:     "mas-bandwidth/schema",
		PlanPkgs:     "./internal/codegen/elixirtable",
		PlanRows:     "[]",
		PlanSlowRows: `[{"name":"unit-elixir-1","row":"unit-elixir-1","part":""}]`,
	}
	for _, job := range []string{"go-fixed-corpus", "go-test-slow"} {
		if ok, err := jobRunsInWorkflow(wf, job, ev); err != nil || !ok {
			t.Errorf("%s did not run for a pull request with slow rows: ok=%v err=%v", job, ok, err)
		}
	}
	fork := ev
	fork.HeadRepo = "fork/schema"
	if ok, err := jobRunsInWorkflow(wf, "go-test-slow", fork); err != nil || ok {
		t.Errorf("go-test-slow ran on a fork pull request: ok=%v err=%v", ok, err)
	}
	none := ev
	none.PlanSlowRows = "[]"
	for _, job := range []string{"go-fixed-corpus", "go-test-slow"} {
		if ok, err := jobRunsInWorkflow(wf, job, none); err != nil || ok {
			t.Errorf("%s ran with no slow row and no go fixed-form row: ok=%v err=%v", job, ok, err)
		}
	}
	for _, state := range []string{"skipped", "failure", "cancelled"} {
		bad := ev
		bad.GoCorpusResult = state
		if ok, err := jobRunsInWorkflow(wf, "go-test-slow", bad); err != nil || ok {
			t.Errorf("go-test-slow ran with its oracle %s: ok=%v err=%v", state, ok, err)
		}
	}

	verdict := jobs["verdict"]
	for _, want := range []string{
		"go-test-slow, ",
		`go-test-slow) [ "$(out slow_rows)" = '[]' ] ;;`,
		`go-fixed-corpus) ! grep -qF '"make":"tables-go-fixedform"' <<<"$(out rows)" && [ "$(out slow_rows)" = '[]' ] ;;`,
	} {
		if !strings.Contains(verdict, want) {
			t.Errorf("ci-fast.yml verdict lost %q", want)
		}
	}
}

// TestSlowRowsInstallWhatCIFullInstalls holds tools/slowrows's per-row
// toolchains (setupFor) to ci-full's go-test job: every language whose rows
// ci-full installs something for is one go-test-slow installs the same thing
// for, or one the fleet runner already carries. A new language row with a
// pin of its own is red here until go-test-slow learns it.
func TestSlowRowsInstallWhatCIFullInstalls(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	full := yamlJobs(wfs["ci-full.yml"])["go-test"]
	if full == "" {
		t.Fatal("ci-full.yml has no go-test job")
	}
	// what go-test-slow does for each language's rows: a dist-pins pin, a
	// step of its own, or (cs, unity) nothing, the fleet SDK's dotnet
	known := map[string]bool{"lua": true, "luau": true, "goose": true, "lobster": true, "squirrel": true, "moo": true, "python": true, "ruby": true, "cs": true, "unity": true}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`startsWith\(matrix\.group, 'unit-codegen-([a-z]+)-'\)`).FindAllStringSubmatch(full, -1) {
		seen[m[1]] = true
		if !known[m[1]] {
			t.Errorf("ci-full's go-test installs something for the unit-codegen-%s- rows that go-test-slow (tools/slowrows setupFor) does not", m[1])
		}
	}
	for lang := range known {
		if !seen[lang] {
			t.Errorf("go-test-slow installs for the %s rows and ci-full's go-test no longer does: drop it from tools/slowrows setupFor and here", lang)
		}
	}
	slow := yamlJobs(wfs["ci-fast.yml"])["go-test-slow"]
	for _, want := range []string{"uses: ./.github/actions/dist-pins", "make dist-squirrel", "uses: ./.github/actions/moo", "uses: ./.github/actions/python", "uses: ./.github/actions/ruby", "make build/conformance-python"} {
		if !strings.Contains(slow, want) {
			t.Errorf("ci-fast.yml go-test-slow lost %q", want)
		}
	}
}
