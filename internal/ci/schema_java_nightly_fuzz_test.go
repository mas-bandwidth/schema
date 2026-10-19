package ci

// The nightly Java fuzz leg (Glenn, 2026-10-18) lives in four files that no
// reader holds by hand across a make file, a workflow, a corpus list and a
// generator: a target that loses its time variable, a schedule that turns into
// a per-push run, or a corpus that drifts from make/java.mk would each leave a
// green fuzz over the wrong thing. This gate is the structural half; the make
// target itself is the functional half and runs nightly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaJavaNightlyFuzz(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makePath := filepath.Join(root, "make", "schema-java-nightly-fuzz.mk")
	makeData, err := os.ReadFile(makePath)
	if err != nil {
		t.Fatalf("read %s: %v", makePath, err)
	}
	makeText := string(makeData)

	for _, want := range []string{
		"JAZZER_VERSION ?=",
		"JAZZER_SHA256 ?=",
		"JAVA_FUZZ_TIME    ?= 600",
		"tool missing: jazzer",
		"schema-java-nightly-fuzz:",
		"schema-java-nightly-fuzz-tool:",
		"schema-java-nightly-fuzz-replay:",
		"schema-java-nightly-fuzz-negative-control:",
		"-max_total_time=$$per",
		"SABOTAGED",
	} {
		if !strings.Contains(makeText, want) {
			t.Errorf("make/schema-java-nightly-fuzz.mk does not contain %q", want)
		}
	}
	if strings.Contains(makeText, "JAZZER_SHA256 ?= 0a4f1eaf") {
		t.Error("the Jazzer pin still carries the placeholder sha256")
	}
	// The replay half must not consult a clock: it is the fast, per-push test.
	replayAt := strings.Index(makeText, "schema-java-nightly-fuzz-replay:")
	if replayAt < 0 {
		t.Fatal("missing replay target")
	}
	replay := makeText[replayAt:]
	if next := strings.Index(replay, "\n# "); next >= 0 {
		replay = replay[:next]
	}
	if strings.Contains(replay, "max_total_time") || strings.Contains(replay, "JAVA_FUZZ_TIME") {
		t.Error("the replay target must run no fuzzer time")
	}

	// The corpus list is the set make/java.mk already generates, never a second
	// corpus: every generated Java unit's source path must be named there.
	corpusData, err := os.ReadFile(filepath.Join(root, "test", "java-fuzz", "corpus.txt"))
	if err != nil {
		t.Fatalf("read corpus list: %v", err)
	}
	corpus := map[string]bool{}
	for _, line := range strings.Split(string(corpusData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		corpus[line] = true
	}
	javaData, err := os.ReadFile(filepath.Join(root, "make", "java.mk"))
	if err != nil {
		t.Fatalf("read make/java.mk: %v", err)
	}
	units := 0
	for _, line := range strings.Split(string(javaData), "\n") {
		if !strings.Contains(line, "generate --lang java") || !strings.Contains(line, "tables-generated-java") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		src := fields[len(fields)-1]
		if strings.HasPrefix(src, "$") || strings.HasPrefix(src, "-") {
			continue
		}
		units++
		if !corpus[src] {
			t.Errorf("make/java.mk generates %s, but test/java-fuzz/corpus.txt does not name it", src)
		}
	}
	if units < 30 {
		t.Fatalf("found %d Java table generate units in make/java.mk, expected at least 30 -- the parse, not the corpus, broke", units)
	}
	for path := range corpus {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("corpus path %s does not exist: %v", path, err)
		}
	}

	// The workflow: nightly and dispatch run the fuzzer for the time budget;
	// push and pull_request run only the replay and the structural test.
	wfData, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-java-nightly-fuzz.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	wf := string(wfData)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"pull_request:",
		"make schema-java-nightly-fuzz JAVA=java JAVAC=javac JAVA_FUZZ_TIME=600",
		"make schema-java-nightly-fuzz-replay JAVA=java JAVAC=javac",
		"sha256sum -c -",
		"if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
		"if: github.event_name == 'push' || github.event_name == 'pull_request'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("schema-java-nightly-fuzz.yml does not contain %q", want)
		}
	}

	// The generator names one entry point per reader kind the card holds.
	genData, err := os.ReadFile(filepath.Join(root, "test", "java-fuzz", "gen", "main.go"))
	if err != nil {
		t.Fatalf("read generator: %v", err)
	}
	for _, kind := range []string{"variable", "fixed", "json", "fuzzerTestOneInput"} {
		if !strings.Contains(string(genData), kind) {
			t.Errorf("test/java-fuzz/gen/main.go does not mention %q", kind)
		}
	}
}
