package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoFixedCorpusPipelineKeepsCompleteTestRecipe holds the fixed corpus the
// language-free Go tests read to its pipeline in ci-fast.yml: one producer job
// builds and uploads it through .github/actions/fixed-corpus, and the
// registry's pr shards that read it (lang none) download it and hold it to
// their own checkout (tools/go-ci-corpus.sh verify) before any test runs.
func TestGoFixedCorpusPipelineKeepsCompleteTestRecipe(t *testing.T) {
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
		{"the shards no longer fetch the corpus", strings.Replace(string(wf), fixedCorpusFetch, strings.Replace(fixedCorpusFetch, "mode: fetch", "mode: none", 1), 1), string(action)},
		{"the fetch no longer verifies", string(wf), strings.Replace(string(action), "run: sh tools/go-ci-corpus.sh verify", "run: true", 1)},
		{"the producer is not needed", strings.Replace(string(wf), "    needs: [fixed-corpus, produce]\n", "    needs: [produce]\n", 1), string(action)},
	} {
		if len(goFixedCorpusPipelineProblems(p.wf, p.action)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", p.name)
		}
	}
}

// fixedCorpusFetch is the shards' fetch of the fixed corpus (the produced
// legs' fetch has its own mode: fetch step).
const fixedCorpusFetch = "uses: ./.github/actions/fixed-corpus\n        if: matrix.lang == 'none'\n        with:\n          mode: fetch\n"

// producedFetch is the shards' fetch of the produced trees.
const producedFetch = "uses: ./.github/actions/produce\n        if: matrix.lang == 'none'\n        with:\n          mode: fetch\n"

// prShardJobs are ci-fast.yml's shard jobs (schema-test-pr-1, -2, ...: the
// matrix cut at GitHub's limit on one matrix), in order.
func prShardJobs(jobs map[string]string) []string {
	var names []string
	for i := 1; ; i++ {
		name := fmt.Sprintf("schema-test-pr-%d", i)
		if _, ok := jobs[name]; !ok {
			return names
		}
		names = append(names, name)
	}
}

// goFixedCorpusPipelineProblems names every way ci-fast.yml and the
// fixed-corpus action break the producer and consumer contract.
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
		bad = append(bad, "ci-fast.yml has no schema-test-pr-N shard job")
	}
	for _, name := range shardJobs {
		tests := jobs[name]
		for _, want := range []string{"needs: [fixed-corpus, produce]", "needs.fixed-corpus.result == 'success'", fixedCorpusFetch} {
			if !strings.Contains(tests, want) {
				bad = append(bad, name+" misses "+want)
			}
		}
		if fetch, run := strings.Index(tests, fixedCorpusFetch), strings.Index(tests, "go run ./tools/schema-test run"); fetch < 0 || run < 0 || fetch > run {
			bad = append(bad, name+" can run its tests before the corpus is fetched and verified")
		}
	}
	for _, want := range []string{"tools/go-ci-corpus.sh produce", "actions/upload-artifact@", "actions/download-artifact@", "go-fixed-corpus-${{ github.run_id }}-${{ github.sha }}", "run: sh tools/go-ci-corpus.sh verify"} {
		if !strings.Contains(action, want) {
			bad = append(bad, ".github/actions/fixed-corpus misses "+want)
		}
	}
	return bad
}

// TestGoFixedCorpusDependencyStates: the shards run only on a corpus the
// producer made, and the producer runs on every same-repository event.
func TestGoFixedCorpusDependencyStates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	event := ciEvent{Name: "pull_request", Branch: "dev", HeadRef: "stella/ci-proof", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema"}
	if got, err := jobRunsInWorkflow(string(data), "fixed-corpus", event); err != nil || !got {
		t.Fatalf("producer withheld from the schema-test-pr shards: %v %v", got, err)
	}
	shardJobs := prShardJobs(yamlJobs(string(data)))
	if len(shardJobs) == 0 {
		t.Fatal("ci-fast.yml has no schema-test-pr-N shard job")
	}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.FixedCorpusResult = state
		for _, name := range shardJobs {
			got, err := jobRunsInWorkflow(string(data), name, event)
			if want := state == "success"; err != nil || got != want {
				t.Errorf("%s after producer %s: got %v want %v err %v", name, state, got, want, err)
			}
		}
	}
}

// TestThePRShardsRunOnTheProducedLegs: the produce job builds each
// producer's tree once per run through .github/actions/produce (cached by
// what it is built from, packed and uploaded), and the language-free
// schema-test-pr shards run only on trees it produced, fetched and unpacked
// before the driver runs; a producer that failed or was skipped holds the
// shards back, never lets them build a leg cold inside a shard. The plants:
// the shards no longer fetch, the fetch no longer unpacks, and the producer
// no longer needed.
func TestThePRShardsRunOnTheProducedLegs(t *testing.T) {
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
	shardFetch := "      - uses: ./.github/actions/produce\n        if: matrix.lang == 'none'\n        with:\n          mode: fetch\n"
	for _, p := range []struct{ name, wf, action string }{
		{"the shards no longer fetch the produced trees", strings.Replace(string(wf), shardFetch, "", 1), string(action)},
		{"the fetch no longer unpacks", string(wf), strings.Replace(string(action), "      run: sh tools/produce.sh unpack build/produce\n", "      run: true\n", 2)},
		{"the producer is not needed", strings.Replace(string(wf), "    needs: [fixed-corpus, produce]\n", "    needs: [fixed-corpus]\n", 1), string(action)},
	} {
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
		t.Fatal("ci-fast.yml has no schema-test-pr-N shard job")
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
	for _, want := range []string{"timeout-minutes: 2", `"name":"conformance-leg-go"`, `"name":"conformance-leg-goose"`, "uses: ./.github/actions/produce", "mode: produce", "name: ${{ matrix.name }}", "target: ${{ matrix.target }}", "paths: ${{ matrix.paths }}"} {
		if !strings.Contains(producer, want) {
			bad = append(bad, "the produce job misses "+want)
		}
	}
	shardJobs := prShardJobs(jobs)
	if len(shardJobs) == 0 {
		bad = append(bad, "ci-fast.yml has no schema-test-pr-N shard job")
	}
	for _, name := range shardJobs {
		shards := jobs[name]
		for _, want := range []string{"needs: [fixed-corpus, produce]", "needs.produce.result == 'success'", producedFetch} {
			if !strings.Contains(shards, want) {
				bad = append(bad, name+" misses "+want)
			}
		}
		if fetch, run := strings.Index(shards, producedFetch), strings.Index(shards, "go run ./tools/schema-test run"); fetch < 0 || run < 0 || fetch > run {
			bad = append(bad, name+" can run the driver before the produced trees are unpacked")
		}
	}
	for _, want := range []string{"actions/cache/restore@", "actions/cache/save@", "sh tools/produce.sh identity", "tools/produce.sh pack", "key: produce-${{ inputs.name }}-${{ runner.os }}-${{ env.PRODUCE_ID }}", "build/produce/${{ inputs.name }}.id", "actions/upload-artifact@", "produce-${{ inputs.name }}-${{ github.run_id }}-${{ github.sha }}", "actions/download-artifact@", "pattern: produce-*-${{ github.run_id }}-${{ github.sha }}"} {
		if !strings.Contains(action, want) {
			bad = append(bad, ".github/actions/produce misses "+want)
		}
	}
	if !strings.Contains(action, "      if: inputs.mode == 'fetch'\n      shell: bash\n      run: sh tools/produce.sh unpack build/produce\n") {
		bad = append(bad, ".github/actions/produce's fetch does not unpack the trees it downloaded")
	}
	return bad
}
