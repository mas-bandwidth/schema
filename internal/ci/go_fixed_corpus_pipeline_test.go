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

// TestGoFixedCorpusPipelineKeepsCompleteTestRecipe holds the fixed corpus's
// producer in ci-fast.yml (one job builds and uploads it through
// .github/actions/fixed-corpus, whose fetch holds it to its own checkout
// with tools/go-ci-corpus.sh verify), and holds the pr job off it: no row of
// the pr group (one compile row and one happy path per language) reads the
// corpus, so the job neither waits for nor downloads it. The plants: the pr
// job fetches the corpus again, waits for its producer again, and the fetch
// no longer verifies.
func TestGoFixedCorpusPipelineKeepsCompleteTestRecipe(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	action, err := os.ReadFile(filepath.Join(root, ".github/actions/fixed-corpus/action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if bad := goFixedCorpusPipelineProblems(string(wf), string(action)); len(bad) > 0 {
		t.Fatalf("the fixed corpus pipeline is incomplete:\n  %s", strings.Join(bad, "\n  "))
	}
	for _, p := range []struct{ name, wf, action string }{
		{"the pr job fetches the corpus again", strings.Replace(string(wf), "      - "+producedFetch, "      - "+fixedCorpusFetch+"      - "+producedFetch, 1), string(action)},
		{"the pr job waits for the corpus again", strings.Replace(string(wf), "    needs: [produce]\n", "    needs: [fixed-corpus, produce]\n", 1), string(action)},
		{"the fetch no longer verifies", string(wf), strings.Replace(string(action), "run: sh tools/go-ci-corpus.sh verify", "run: true", 1)},
	} {
		if p.wf == string(wf) && p.action == string(action) {
			t.Errorf("PLANTED PATTERN %q: its anchor is not in the files", p.name)
			continue
		}
		if len(goFixedCorpusPipelineProblems(p.wf, p.action)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", p.name)
		}
	}
}

// fixedCorpusFetch is a job's fetch of the fixed corpus.
const fixedCorpusFetch = "uses: ./.github/actions/fixed-corpus\n        with:\n          mode: fetch\n"

// producedFetch is the pr job's fetch of the produced conformance legs, and
// of nothing else the produce job makes (the lint trees).
const producedFetch = "uses: ./.github/actions/produce\n        with:\n          mode: fetch\n          names: conformance-leg-*\n"

// prShardJobs is ci-fast.yml's one pr job (schema-test-pr: the registry's pr
// group whole, one compile row and one happy path per language), or none.
func prShardJobs(jobs map[string]string) []string {
	if _, ok := jobs["schema-test-pr"]; !ok {
		return nil
	}
	return []string{"schema-test-pr"}
}

// goFixedCorpusPipelineProblems names every way ci-fast.yml and the
// fixed-corpus action break the producer's contract, and every way the pr
// job reads a corpus none of its rows reads.
func goFixedCorpusPipelineProblems(wf, action string) []string {
	var bad []string
	jobs := yamlJobs(wf)
	producer := jobs["fixed-corpus"]
	for _, want := range []string{"uses: ./.github/actions/fixed-corpus", "mode: produce", "timeout-minutes: 2"} {
		if !strings.Contains(producer, want) {
			bad = append(bad, "the producer job misses "+want)
		}
	}
	shardJobs := prShardJobs(jobs)
	if len(shardJobs) == 0 {
		bad = append(bad, "ci-fast.yml has no schema-test-pr job")
	}
	for _, name := range shardJobs {
		tests := jobs[name]
		for _, unwanted := range []string{"fixed-corpus, ", "needs.fixed-corpus.result", fixedCorpusFetch} {
			if strings.Contains(tests, unwanted) {
				bad = append(bad, fmt.Sprintf("%s has %q: no pr row reads the fixed corpus, so the job neither waits for nor fetches it", name, strings.TrimSpace(unwanted)))
			}
		}
	}
	for _, want := range []string{"tools/go-ci-corpus.sh produce", "actions/upload-artifact@", "actions/download-artifact@", "go-fixed-corpus-${{ github.run_id }}-${{ github.sha }}", "run: sh tools/go-ci-corpus.sh verify"} {
		if !strings.Contains(action, want) {
			bad = append(bad, ".github/actions/fixed-corpus misses "+want)
		}
	}
	return bad
}

// TestGoFixedCorpusDependencyStates: the producer runs on every
// same-repository event, and the pr job runs whatever it did: none of the
// job's rows reads the corpus.
func TestGoFixedCorpusDependencyStates(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	event := ciEvent{Name: "pull_request", Branch: "dev", HeadRef: "stella/ci-proof", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}
	if got, err := jobRunsInWorkflow(string(data), "fixed-corpus", event); err != nil || !got {
		t.Fatalf("the fixed corpus's producer does not run on a pull request: %v %v", got, err)
	}
	shardJobs := prShardJobs(yamlJobs(string(data)))
	if len(shardJobs) == 0 {
		t.Fatal("ci-fast.yml has no schema-test-pr job")
	}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.FixedCorpusResult = state
		for _, name := range shardJobs {
			if got, err := jobRunsInWorkflow(string(data), name, event); err != nil || !got {
				t.Errorf("%s after the fixed corpus %s: got %v want true (no pr row reads it) err %v", name, state, got, err)
			}
		}
	}
}

// TestThePRShardsRunOnTheProducedLegs: the produce job builds each
// producer's tree once per run through .github/actions/produce (cached by
// what it is built from, packed and uploaded), and the pr job runs only on
// the conformance legs it produced, fetched and unpacked before the driver
// runs, and fetches no other producer's tree (the lint trees no pr row
// reads); a producer that failed or was skipped holds the job back, never
// lets it build a leg cold inside its two minutes. The plants: the job no
// longer fetches, fetches every producer's tree, the fetch no longer
// unpacks, and the producer no longer needed.
func TestThePRShardsRunOnTheProducedLegs(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	action, err := os.ReadFile(filepath.Join(root, ".github/actions/produce/action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if bad := producedLegProblems(string(wf), string(action)); len(bad) > 0 {
		t.Fatalf("the produced trees' pipeline is incomplete:\n  %s", strings.Join(bad, "\n  "))
	}
	for _, p := range []struct{ name, wf, action string }{
		{"the job no longer fetches the produced legs", strings.Replace(string(wf), "      - "+producedFetch, "", 1), string(action)},
		{"the job fetches every producer's tree", strings.Replace(string(wf), "          names: conformance-leg-*\n", "", 1), string(action)},
		{"the fetch no longer unpacks", string(wf), strings.Replace(string(action), "      run: sh tools/produce.sh unpack build/produce\n", "      run: true\n", 2)},
		{"the producer is not needed", strings.Replace(string(wf), "    needs: [produce]\n", "    needs: []\n", 1), string(action)},
	} {
		if p.wf == string(wf) && p.action == string(action) {
			t.Errorf("PLANTED PATTERN %q: its anchor is not in the files", p.name)
			continue
		}
		if len(producedLegProblems(p.wf, p.action)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", p.name)
		}
	}
	event := ciEvent{Name: "pull_request", Branch: "dev", HeadRef: "stella/ci-proof", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}
	if got, err := jobRunsInWorkflow(string(wf), "produce", event); err != nil || !got {
		t.Fatalf("produce does not run on a pull request: %v %v", got, err)
	}
	shardJobs := prShardJobs(yamlJobs(string(wf)))
	if len(shardJobs) == 0 {
		t.Fatal("ci-fast.yml has no schema-test-pr job")
	}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.ProduceResult = state
		for _, name := range shardJobs {
			got, err := jobRunsInWorkflow(string(wf), name, event)
			if want := state == "success"; err != nil || got != want {
				t.Errorf("%s after produce %s: got %v want %v err %v", name, state, got, want, err)
			}
		}
	}
}

// producedLegProblems names every way ci-fast.yml and the produce action
// break the producer and consumer contract.
func producedLegProblems(wf, action string) []string {
	var bad []string
	jobs := yamlJobs(wf)
	producer := jobs["produce"]
	for _, want := range []string{"timeout-minutes: 2", `"name":"conformance-leg-go"`, `"name":"conformance-leg-goose"`, `"name":"conformance-leg-cpp","target":"conformance-leg-cpp"`, "uses: ./.github/actions/produce", "mode: produce", "name: ${{ matrix.name }}", "target: ${{ matrix.target }}", "paths: ${{ matrix.paths }}"} {
		if !strings.Contains(producer, want) {
			bad = append(bad, "the produce job misses "+want)
		}
	}
	shardJobs := prShardJobs(jobs)
	if len(shardJobs) == 0 {
		bad = append(bad, "ci-fast.yml has no schema-test-pr job")
	}
	for _, name := range shardJobs {
		job := jobs[name]
		for _, want := range []string{"needs: [produce]", "needs.produce.result == 'success'", producedFetch} {
			if !strings.Contains(job, want) {
				bad = append(bad, name+" misses "+want)
			}
		}
		if fetch, run := strings.Index(job, producedFetch), strings.Index(job, "go run ./tools/schema-test run"); fetch < 0 || run < 0 || fetch > run {
			bad = append(bad, name+" can run the driver before the produced legs are unpacked")
		}
	}
	for _, want := range []string{"actions/cache/restore@", "actions/cache/save@", "sh tools/produce.sh identity", "tools/produce.sh pack", "key: produce-${{ inputs.name }}-${{ runner.os }}-${{ env.PRODUCE_ID }}", "build/produce/${{ inputs.name }}.id", "actions/upload-artifact@", "produce-${{ inputs.name }}-${{ github.run_id }}-${{ github.sha }}", "actions/download-artifact@", "pattern: produce-${{ inputs.names }}-${{ github.run_id }}-${{ github.sha }}"} {
		if !strings.Contains(action, want) {
			bad = append(bad, ".github/actions/produce misses "+want)
		}
	}
	if !strings.Contains(action, "      if: inputs.mode == 'fetch'\n      shell: bash\n      run: sh tools/produce.sh unpack build/produce\n") {
		bad = append(bad, ".github/actions/produce's fetch does not unpack the trees it downloaded")
	}
	return bad
}

// cppBudgetRow is the registry row that holds the C++ conformance driver's
// translation units to their compile budget (Makefile,
// CONFORMANCE_DRIVER_TU_BUDGET).
const cppBudgetRow = "harness-run.makefile.conformance-cpp-tu-budget"

// cppBudgetProblems names what keeps the compile budget from running every
// night: no registry row runs `make conformance-cpp-tu-budget`, the row is
// scoped to a language (the budget is the leg's, read once), or its tier is
// outside the nightly group's. The pr group holds one compile row and one
// happy path per language and nothing else (tools/schema-test ci), so the
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
			bad = append(bad, fmt.Sprintf("%s is in the pr group as %s: the pr group is one compile row and one happy path per language, and the budget runs nightly", cppBudgetRow, n))
		}
	}
	return bad
}

// TestTheCppCompileBudgetRunsNightly: the cpp conformance leg's build
// records every driver unit's compile seconds, and the registry holds them
// to the budget every night, in a language-free row of a tier the nightly
// group runs, never in the per-PR job (one compile row and one happy path
// per language). It replaces TestTheCppCompileBudgetRunsOnTheProducedLeg,
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
