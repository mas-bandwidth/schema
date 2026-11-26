package ci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// prShardJobs is ci-fast.yml's one pr job (schema-test-pr: the registry's pr
// group whole, make happy-<lang> for every language), or none.
func prShardJobs(jobs map[string]string) []string {
	if _, ok := jobs["schema-test-pr"]; !ok {
		return nil
	}
	return []string{"schema-test-pr"}
}

// prJobFetchProblems names every way ci-fast.yml makes the pr job wait for,
// or read, a tree another job built: the pr group is make happy-<lang> for
// every language, which builds what it runs (bin/schema and one small
// generated unit a language), so the workflow has no producer job (the
// fixed corpus, the produced legs and lint trees run nightly) and the pr
// job needs no job and fetches nothing.
func prJobFetchProblems(wf string) []string {
	var bad []string
	jobs := yamlJobs(wf)
	for _, producer := range []string{"fixed-corpus", "produce"} {
		if _, ok := jobs[producer]; ok {
			bad = append(bad, fmt.Sprintf("ci-fast.yml has a %s job, and no pr row reads what it builds: it runs nightly", producer))
		}
	}
	names := prShardJobs(jobs)
	if len(names) == 0 {
		bad = append(bad, "ci-fast.yml has no schema-test-pr job")
	}
	for _, name := range names {
		job := jobs[name]
		for _, unwanted := range []string{"needs:", "uses: ./.github/actions/fixed-corpus", "uses: ./.github/actions/produce"} {
			if strings.Contains(job, unwanted) {
				bad = append(bad, fmt.Sprintf("%s has %q: the pr job builds what it runs, and waits for and fetches nothing", name, unwanted))
			}
		}
	}
	return bad
}

// TestThePRJobBuildsWhatItRunsAndFetchesNothing: ci-fast.yml's pr job runs
// make happy-<lang> for every language, which builds what it runs, so the
// workflow has no fixed-corpus or produce job, and the pr job needs no job
// and fetches nothing. It replaces the fixed corpus's and the produced
// legs' per-PR pipeline tests (TestGoFixedCorpusPipelineKeepsCompleteTestRecipe,
// TestGoFixedCorpusDependencyStates, TestThePRShardsRunOnTheProducedLegs) and
// TestEveryLintGateReadsAProducedTree: no per-PR job reads those trees any
// more. The plants: a producer job back, the pr job waiting for it, and the
// pr job fetching the corpus and the produced trees.
func TestThePRJobBuildsWhatItRunsAndFetchesNothing(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	if bad := prJobFetchProblems(wf); len(bad) > 0 {
		t.Fatalf("the pr job reads what another job built:\n  %s", strings.Join(bad, "\n  "))
	}
	const head = "  schema-test-pr:\n"
	const step = "      - name: schema-test run --group pr\n"
	if !strings.Contains(wf, head) || !strings.Contains(wf, step) {
		t.Fatal("ci-fast.yml has no schema-test-pr job and run step to plant on")
	}
	for _, p := range []struct{ name, wf, want string }{
		{"a producer job back", strings.Replace(wf, head, "  produce:\n    runs-on: x\n    steps:\n      - run: true\n\n"+head, 1), "has a produce job"},
		{"the pr job waits for a producer", strings.Replace(wf, head, head+"    needs: [fixed-corpus]\n", 1), `has "needs:"`},
		{"the pr job fetches the corpus", strings.Replace(wf, step, "      - uses: ./.github/actions/fixed-corpus\n        with:\n          mode: fetch\n"+step, 1), "actions/fixed-corpus"},
		{"the pr job fetches the produced trees", strings.Replace(wf, step, "      - uses: ./.github/actions/produce\n        with:\n          mode: fetch\n"+step, 1), "actions/produce"},
	} {
		if got := strings.Join(prJobFetchProblems(p.wf), "\n"); !strings.Contains(got, p.want) {
			t.Errorf("PLANTED PATTERN %q: the check reported %q, want %q", p.name, got, p.want)
		}
	}
}

// cppBudgetRow is the registry row that holds the C++ conformance driver's
// translation units to their compile budget (Makefile,
// CONFORMANCE_DRIVER_TU_BUDGET).
const cppBudgetRow = "harness-run.makefile.conformance-cpp-tu-budget"

// cppBudgetProblems names what keeps the compile budget from running every
// night: no registry row runs `make conformance-cpp-tu-budget`, the row is
// scoped to a language (the budget is the leg's, read once), or its tier is
// outside the nightly group's. The pr group holds one happy-path row per
// language (make happy-<lang>) and nothing else (tools/schema-test ci), so the
// budget is never per pull request; a row in it is named too.
func cppBudgetProblems(registry string) []string {
	var row map[string]any
	var nightlyTiers []string
	var prNames []string
	for _, line := range strings.Split(registry, "\n") {
		switch {
		case strings.Contains(line, `"key":"`+cppBudgetRow+`"`):
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return []string{cppBudgetRow + ": " + err.Error()}
			}
		case strings.Contains(line, `"rec":"group"`):
			var g struct {
				Name  string
				Query struct {
					Tiers []string
					Names []string
				}
			}
			if err := json.Unmarshal([]byte(line), &g); err != nil {
				return []string{"a group line: " + err.Error()}
			}
			switch g.Name {
			case "nightly":
				nightlyTiers = g.Query.Tiers
			case "pr":
				prNames = g.Query.Names
			}
		}
	}
	if row == nil {
		return []string{"no registry row " + cppBudgetRow + " holds the C++ driver's units to their compile budget"}
	}
	var bad []string
	inputs, _ := row["inputs"].(map[string]any)
	if row["adapter"] != "make" || inputs["target"] != "conformance-cpp-tu-budget" || inputs["file"] != "Makefile" {
		bad = append(bad, cppBudgetRow+" does not run make conformance-cpp-tu-budget")
	}
	if tier, _ := row["tier"].(string); !slices.Contains(nightlyTiers, tier) {
		bad = append(bad, fmt.Sprintf("%s is tier %v, outside the nightly group's tiers %v: the budget would never run", cppBudgetRow, row["tier"], nightlyTiers))
	}
	if row["scope"] != "none" {
		bad = append(bad, fmt.Sprintf("%s is scoped %v: the budget is the cpp leg's, read once, a language-free row", cppBudgetRow, row["scope"]))
	}
	for _, n := range prNames {
		if strings.HasSuffix(n, "."+cppBudgetRow) {
			bad = append(bad, fmt.Sprintf("%s is in the pr group as %s: the pr group is one happy-path row per language, and the budget runs nightly", cppBudgetRow, n))
		}
	}
	return bad
}

// TestTheCppCompileBudgetRunsNightly: the cpp conformance leg's build
// records every driver unit's compile seconds, and the registry holds them
// to the budget every night, in a language-free row of a tier the nightly
// group runs, never in the per-PR job (one happy-path row per language). It replaces TestTheCppCompileBudgetRunsOnTheProducedLeg,
// whose language-free per-PR shards are gone. The plants: the row gone, a
// tier the nightly group does not run, scoped to cpp, running another
// target, and named in the pr group.
func TestTheCppCompileBudgetRunsNightly(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "test/registry/registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	reg := string(data)
	if bad := cppBudgetProblems(reg); len(bad) > 0 {
		t.Fatalf("the compile budget does not run nightly:\n  %s", strings.Join(bad, "\n  "))
	}
	var line string
	for _, l := range strings.Split(reg, "\n") {
		if strings.Contains(l, `"key":"`+cppBudgetRow+`"`) {
			line = l
		}
	}
	inRow := func(old, new string) string {
		return strings.Replace(reg, line, strings.Replace(line, old, new, 1), 1)
	}
	for _, p := range []struct{ name, planted, want string }{
		{"the row gone", strings.Replace(reg, line+"\n", "", 1), "no registry row"},
		{"a tier the nightly group does not run", inRow(`"tier":"functional"`, `"tier":"soak"`), "outside the nightly group's tiers"},
		{"the row scoped to cpp", inRow(`"scope":"none"`, `"scope":"cpp"`), "a language-free row"},
		{"the row runs another target", inRow(`"target":"conformance-cpp-tu-budget"`, `"target":"conformance-leg-cpp"`), "does not run make conformance-cpp-tu-budget"},
		{"the row in the pr group", strings.Replace(reg, `"query":{"names":[`, `"query":{"names":["none.functional.`+cppBudgetRow+`",`, 1), "is in the pr group"},
	} {
		if p.planted == reg {
			t.Errorf("%s: the plant's anchor is not in the registry", p.name)
			continue
		}
		if got := strings.Join(cppBudgetProblems(p.planted), "\n"); !strings.Contains(got, p.want) {
			t.Errorf("PLANTED PATTERN %q: the check reported %q, want %q", p.name, got, p.want)
		}
	}
}
