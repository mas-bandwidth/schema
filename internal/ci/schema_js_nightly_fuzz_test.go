// The JavaScript port's nightly fuzz harness, held to the card's shape from
// inside `go test ./...`:
//
//   - the corpus list is real and is the corpus the Makefile's generators read;
//   - the tool is PINNED, and the make target probes for it instead of faking
//     a pass;
//   - the fuzzer is SCHEDULED and is never a per-push job, while the saved
//     inputs are replayed on every push;
//   - the planted-pattern control still has both halves: unplanted replay green,
//     planted replay red on an escaped read.
//
// These are static checks over files the harness owns. The replay itself is
// the make target `schema-js-nightly-fuzz-replay`, which the workflow runs on
// push; it never spends real fuzz time.
package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	jsFuzzDir      = "test/js-nightly-fuzz"
	jsFuzzMakefile = "make/schema-js-nightly-fuzz.mk"
	jsFuzzWorkflow = ".github/workflows/schema-js-nightly-fuzz.yml"
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(body)
}

// TestSchemaJSNightlyFuzzCorpusIsTheGeneratorCorpus pins the list to real
// directories that hold schemas. A path that does not exist would make the
// nightly target fuzz nothing while the seed stage still went green.
func TestSchemaJSNightlyFuzzCorpusIsTheGeneratorCorpus(t *testing.T) {
	t.Parallel()
	body := readRepoFile(t, filepath.Join(jsFuzzDir, "corpus.txt"))
	root := repoRoot(t)
	seen := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seen++
		target := filepath.Join(root, line)
		info, err := os.Stat(target)
		if err != nil {
			t.Errorf("corpus path %q does not exist: %v", line, err)
			continue
		}
		if !info.IsDir() {
			if !strings.HasSuffix(target, ".schema") {
				t.Errorf("corpus path %q is neither a directory nor a .schema file", line)
			}
			continue
		}
		var schemas int
		err = filepath.WalkDir(target, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(p, ".schema") {
				schemas++
			}
			return nil
		})
		if err != nil {
			t.Errorf("walking corpus path %q: %v", line, err)
		}
		if schemas == 0 {
			t.Errorf("corpus path %q holds no .schema file", line)
		}
	}
	if seen < 10 {
		t.Errorf("corpus.txt names %d paths; the JS generators read more shapes than that", seen)
	}
}

// TestSchemaJSNightlyFuzzPinsAndProbesJazzer requires the version pin and the
// missing-tool refusal. A target that reports green on a machine without the
// fuzzer is the failure this test exists to make unrepresentable.
func TestSchemaJSNightlyFuzzPinsAndProbesJazzer(t *testing.T) {
	t.Parallel()
	body := readRepoFile(t, jsFuzzMakefile)
	for _, want := range []string{
		"JAZZER_VERSION ?= 4.0.0",
		"tool missing: jazzer@",
		"command -v $(JAZZER)",
		"SCHEMA_JS_FUZZ_SECONDS ?= 600",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not contain %q", jsFuzzMakefile, want)
		}
	}
}

// TestSchemaJSNightlyFuzzHeavyPartIsScheduled pins the when: the fuzzer runs
// from schedule/workflow_dispatch, and the replay runs from push/pull_request.
func TestSchemaJSNightlyFuzzHeavyPartIsScheduled(t *testing.T) {
	t.Parallel()
	body := readRepoFile(t, jsFuzzWorkflow)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"push:",
		"pull_request:",
		"npm install --global @jazzer.js/core@4.0.0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not contain %q", jsFuzzWorkflow, want)
		}
	}
	fuzzAt := strings.Index(body, "\n  fuzz:")
	if fuzzAt < 0 {
		t.Fatal("the workflow has no fuzz job")
	}
	replayAt := strings.Index(body, "\n  replay:")
	if replayAt < 0 {
		t.Fatal("the workflow has no replay job")
	}
	if fuzzAt < replayAt {
		t.Fatal("the fuzz job must be written after the replay job so the split is readable")
	}
	if !strings.Contains(body[replayAt:fuzzAt], "make schema-js-nightly-fuzz-replay") {
		t.Error("the replay job does not run schema-js-nightly-fuzz-replay")
	}
	if !strings.Contains(body[fuzzAt:], "make schema-js-nightly-fuzz") {
		t.Error("the fuzz job does not run schema-js-nightly-fuzz")
	}
	if strings.Contains(body[replayAt:fuzzAt], "make schema-js-nightly-fuzz\n") {
		t.Error("the replay job runs the nightly fuzzer; the fast part is replay only")
	}
}

// TestSchemaJSNightlyFuzzPlantedControlHasBothHalves requires the control to
// demand the unplanted green BEFORE the planted red, and to name the escaped
// read. A control with only the red half can pass on a harness that is broken
// for an unrelated reason.
func TestSchemaJSNightlyFuzzPlantedControlHasBothHalves(t *testing.T) {
	t.Parallel()
	body := readRepoFile(t, jsFuzzMakefile)
	planted := body[strings.Index(body, "schema-js-nightly-fuzz-negative-control:"):]
	normal := strings.Index(planted, "negative-control/normal/targets")
	red := strings.Index(planted, "the unplanted replay is not green")
	sabotage := strings.Index(planted, "no short-wire bound left the replay green")
	if normal < 0 || red < 0 || sabotage < 0 {
		t.Fatalf("the control is missing a half: normal=%d red=%d sabotage=%d", normal, red, sabotage)
	}
	if !(normal < red && red < sabotage) {
		t.Error("the control must prove the unplanted green before the planted red")
	}
	if !strings.Contains(planted, "RangeError") {
		t.Error("the control does not name the escaped read it requires")
	}
}

// TestSchemaJSNightlyFuzzSavedInputsExist requires a corpus of saved bytes that
// the push-time replay actually reads. An empty directory would let replay
// report success having replayed nothing.
func TestSchemaJSNightlyFuzzSavedInputsExist(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), jsFuzzDir, "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading saved corpus: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Errorf("saved input %s is empty", e.Name())
			continue
		}
		n++
	}
	if n == 0 {
		t.Fatalf("%s holds no saved input; replay would pass on nothing", dir)
	}
	replay := readRepoFile(t, filepath.Join(jsFuzzDir, "replay.mjs"))
	if !strings.Contains(replay, "process.exit(1)") {
		t.Error("replay.mjs does not fail on an escaped read")
	}
}
