package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoFixedCorpusPipelineKeepsCompleteTestRecipe(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := yamlJobs(string(data))
	producer, tests := jobs["go-fixed-corpus"], jobs["go-test-touched"]
	for _, want := range []string{"tools/go-ci-corpus.sh produce", "actions/upload-artifact@", "go-fixed-corpus-${{ github.run_id }}-${{ github.sha }}", "timeout-minutes: 2"} {
		if !strings.Contains(producer, want) {
			t.Errorf("producer misses %s", want)
		}
	}
	for _, want := range []string{"needs: [plan, go-fixed-corpus]", "needs.go-fixed-corpus.result == 'skipped'", "actions/download-artifact@", "go-fixed-corpus-${{ github.run_id }}-${{ github.sha }}", "matrix.make == 'tables-go-fixedform'", "tools/go-ci-corpus.sh verify", "make -o tables-fixedform-corpus tables-go-fixedform"} {
		if !strings.Contains(tests, want) {
			t.Errorf("test pipeline misses %s", want)
		}
	}
	if strings.Index(tests, "tools/go-ci-corpus.sh verify") > strings.Index(tests, "make -o tables-fixedform-corpus tables-go-fixedform") {
		t.Error("test recipe can run before artifact verification")
	}
	if !makeTargetSelectsCardAdded(t) {
		t.Fatal("the complete fixed-form recipe no longer selects the required case")
	}
}

func TestGoFixedCorpusDependencyStates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	event := ciEvent{Name: "pull_request", Branch: "dev", HeadRef: "stella/ci-proof", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema", PlanPkgs: "./internal/codegen/gotable"}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.GoCorpusResult = state
		got, err := jobRunsInWorkflow(string(data), "go-test-touched", event)
		want := state == "success"
		if err != nil || got != want {
			t.Errorf("producer %s: got%v want%v err%v", state, got, want, err)
		}
	}
	event.GoCorpusResult = "skipped"
	event.PlanRows = "[]"
	event.PlanPkgs = "./compiler"
	if got, err := jobRunsInWorkflow(string(data), "go-test-touched", event); err != nil || !got {
		t.Fatalf("unrelated test row blocked by skipped producer: %v %v", got, err)
	}
	event.PlanRows = `[{"make":"tables-go-fixedform"}]`
	event.PlanPkgs = "./internal/codegen/gotable"
	event.GoCorpusResult = "success"
	event.PlanResult = "failure"
	if got, err := jobRunsInWorkflow(string(data), "go-test-touched", event); err != nil || got {
		t.Fatalf("failed plan admitted tests: %v %v", got, err)
	}
	event.PlanResult = "success"
	event.PlanRows = `[{"make":"tables-go-fixedform"}]`
	if got, err := jobRunsInWorkflow(string(data), "go-fixed-corpus", event); err != nil || !got {
		t.Fatalf("selected producer missing: %v %v", got, err)
	}
	// the registry's pr shards read the corpus on every run, so the producer
	// runs with no fixed-form row selected too, and the shards run only on
	// a corpus it produced
	event.PlanRows = "[]"
	if got, err := jobRunsInWorkflow(string(data), "go-fixed-corpus", event); err != nil || !got {
		t.Fatalf("producer withheld from the schema-test-pr shards: %v %v", got, err)
	}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.GoCorpusResult = state
		got, err := jobRunsInWorkflow(string(data), "schema-test-pr", event)
		if want := state == "success"; err != nil || got != want {
			t.Errorf("schema-test-pr after producer %s: got %v want %v err %v", state, got, want, err)
		}
	}
}

// TestThePRShardsRunOnTheProducedLegs: the produce job builds each
// producer's tree once per run, caches it, packs it and uploads it, and the
// schema-test-pr shards run only on trees it produced, unpacked before the
// driver runs; a producer that failed or was skipped holds the shards back,
// never lets them build a leg cold inside a shard.
func TestThePRShardsRunOnTheProducedLegs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := yamlJobs(string(data))
	producer, shards := jobs["produce"], jobs["schema-test-pr"]
	for _, want := range []string{"timeout-minutes: 2", "name: conformance-leg-go", "name: conformance-leg-goose", "actions/cache/restore@", "actions/cache/save@", "tools/produce.sh pack", "sh tools/produce.sh identity", "key: produce-${{ matrix.name }}-${{ runner.os }}-${{ env.PRODUCE_ID }}", "build/produce/${{ matrix.name }}.id", "actions/upload-artifact@", "produce-${{ matrix.name }}-${{ github.run_id }}-${{ github.sha }}"} {
		if !strings.Contains(producer, want) {
			t.Errorf("produce misses %s", want)
		}
	}
	for _, want := range []string{"needs: [go-fixed-corpus, produce]", "needs.produce.result == 'success'", "pattern: produce-*-${{ github.run_id }}-${{ github.sha }}", "tools/produce.sh unpack build/produce"} {
		if !strings.Contains(shards, want) {
			t.Errorf("schema-test-pr misses %s", want)
		}
	}
	if strings.Index(shards, "tools/produce.sh unpack build/produce") > strings.Index(shards, "go run ./tools/schema-test run") {
		t.Error("schema-test-pr runs the driver before it unpacks the produced trees")
	}
	event := ciEvent{Name: "pull_request", Branch: "dev", HeadRef: "stella/ci-proof", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema", PlanRows: "[]"}
	if got, err := jobRunsInWorkflow(string(data), "produce", event); err != nil || !got {
		t.Fatalf("produce does not run on a pull request: %v %v", got, err)
	}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.ProduceResult = state
		got, err := jobRunsInWorkflow(string(data), "schema-test-pr", event)
		if want := state == "success"; err != nil || got != want {
			t.Errorf("schema-test-pr after produce %s: got %v want %v err %v", state, got, want, err)
		}
	}
}
