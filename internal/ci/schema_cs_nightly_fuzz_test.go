// Nightly fuzzing for the C# port: the corpus manifest and the planted-bad-
// pattern test that make/schema-cs-nightly-fuzz.mk runs around the fuzzer.
//
// Both tests read what the make fragment wrote under build/, so they are
// functional-tier: the default `go test ./...` skips them through slowtest.
// Under SCHEMA_SLOW=1 alone (ci-full.yml's blanket go test) they skip by name
// when the corpus/readers are not built; under SCHEMA_REQUIRE_CORPUS=1 — which
// `make schema-cs-nightly-fuzz-planted-pattern` sets after generating them — a
// missing build is a hard failure, never a silent green. The planted test's
// contract is the negative control's: the clean reader tree is GREEN, the tree
// with the plant is RED.
package ci

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const csFuzzPlantedMarker = "SABOTAGED: dropped bounds check"

func csFuzzReadersDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("SCHEMA_CS_FUZZ_READERS"); dir != "" {
		return dir
	}
	abs, err := filepath.Abs(filepath.Join("..", "..", "build", "schema-cs-nightly-fuzz", "readers"))
	if err != nil {
		t.Fatalf("resolve readers dir: %v", err)
	}
	return abs
}

func csRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}

// TestSchemaCSNightlyFuzzCorpus covers the card's corpus rule: every schema the
// generation targets read is in the manifest, and every manifest line has a
// generated reader. A corpus directory dropped from the list is the failure
// this test exists to name.
func TestSchemaCSNightlyFuzzCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz corpus")

	root := csRepoRoot(t)
	manifest := filepath.Join(filepath.Dir(csFuzzReadersDir(t)), "corpus.txt")
	f, err := os.Open(manifest)
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("open corpus manifest %s: %v (run make schema-cs-nightly-fuzz-readers)", manifest, err)
		}
		t.Skipf("schema-cs-nightly-fuzz: corpus manifest %s absent; run make schema-cs-nightly-fuzz-readers", manifest)
	}
	defer f.Close()

	seenDir := map[string]bool{}
	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("corpus manifest line %q is not schema<TAB>outdir", line)
		}
		schema, outdir := parts[0], parts[1]
		if !filepath.IsAbs(schema) {
			schema = filepath.Join(root, schema)
		}
		if !filepath.IsAbs(outdir) {
			outdir = filepath.Join(root, outdir)
		}
		if _, err := os.Stat(schema); err != nil {
			t.Errorf("corpus schema %s: %v", schema, err)
		}
		units, err := filepath.Glob(filepath.Join(outdir, "*.cs"))
		if err != nil || len(units) == 0 {
			t.Errorf("corpus schema %s has no generated reader under %s", schema, outdir)
		}
		seenDir[filepath.Dir(strings.TrimPrefix(schema, root+string(os.PathSeparator)))] = true
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read corpus manifest: %v", err)
	}
	if lines == 0 {
		t.Fatal("the corpus manifest is empty")
	}

	for _, dir := range []string{
		"tables/examples", "tables/pointers", "tables/block", "tables/blockhome",
		"tables/messages", "tables/stream", "tables/blobs", "tables/scalars",
		"tables/maps", "tables/lists", "tables/backend",
		"tables/vocab", "tables/vocab9", "examples-wide",
	} {
		if !seenDir[dir] {
			t.Errorf("corpus directory %s contributes no schema to the manifest", dir)
		}
	}
}

// TestSchemaCSNightlyFuzzPlantedPattern is the planted-bad-pattern test. It
// PASSES on the clean reader tree and FAILS the moment the control's plant is
// in, naming the marker and the file. The make target asserts both directions:
// the clean run must print --- PASS and the planted run must be red.
func TestSchemaCSNightlyFuzzPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz planted reader")

	readers := csFuzzReadersDir(t)
	files, err := filepath.Glob(filepath.Join(readers, "*", "*.cs"))
	if err != nil {
		t.Fatalf("list generated readers: %v", err)
	}
	if len(files) == 0 {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("no generated readers under %s (run make schema-cs-nightly-fuzz-readers)", readers)
		}
		t.Skipf("schema-cs-nightly-fuzz: no generated readers under %s; run make schema-cs-nightly-fuzz-readers", readers)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(data), csFuzzPlantedMarker) {
			t.Fatalf("planted reader bug %q present in %s", csFuzzPlantedMarker, file)
		}
	}
}

// TestSchemaCSNightlyFuzzBudgetSeconds pins the campaign's time budget to what
// afl-fuzz actually runs. AFL's -V takes whole seconds (atoi), so the default
// CS_FUZZ_TIME=10m handed over raw fuzzed for ten seconds and the planted
// campaign's 2m for two: AFL's own stats showed run_time 2 and no crash, while
// a real 60 s run found the planted Has() bug in 44 s. `make -n` prints the
// converted budget, so this needs make only (no dotnet, no fuzzer, no clock).
func TestSchemaCSNightlyFuzzBudgetSeconds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz time budget (runs make -n)")

	root := csRepoRoot(t)
	for _, c := range []struct {
		target, budget, want string
	}{
		{"schema-cs-nightly-fuzz", "CS_FUZZ_TIME=10m", "secs=600;"},
		{"schema-cs-nightly-fuzz", "CS_FUZZ_TIME=1h", "secs=3600;"},
		{"schema-cs-nightly-fuzz", "CS_FUZZ_TIME=90s", "secs=90;"},
		{"schema-cs-nightly-fuzz-planted-campaign", "CS_FUZZ_PLANT_TIME=2m", "secs=120;"},
	} {
		cmd := exec.Command("make", "-n", "--no-print-directory", c.target, c.budget, "CS_FUZZ_KIND=load")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make -n %s %s: %v\n%s", c.target, c.budget, err, out)
		}
		text := string(out)
		budgets := 0
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, " -V ") {
				continue
			}
			budgets++
			if !strings.Contains(line, " -V $secs") {
				t.Errorf("%s %s: afl-fuzz is not given the converted seconds: %s", c.target, c.budget, strings.TrimSpace(line))
			}
		}
		if budgets == 0 {
			t.Fatalf("make -n %s %s printed no afl-fuzz -V budget:\n%s", c.target, c.budget, text)
		}
		if !strings.Contains(text, c.want) {
			t.Errorf("%s %s: the budget is not converted to %q seconds", c.target, c.budget, c.want)
		}
	}
}

// csFuzzStandInAFL is a stand-in for afl-fuzz with AFL 4.21c's exit contract
// (measured on amd-1, 2026-10-19): exit 0 when -V runs out, crashes or not,
// with run_time and execs_done in default/fuzzer_stats; exit 1 on a FATAL such
// as a failed fork-server handshake. Its behaviour per reader kind (the last
// argument) comes from FAKE_AFL_MODE_<kind>, else FAKE_AFL_MODE.
const csFuzzStandInAFL = `#!/bin/sh
out=""; secs=""
for a in "$@"; do kind="$a"; done
while [ $# -gt 0 ]; do
	case "$1" in -o) out="$2"; shift;; -V) secs="$2"; shift;; --) break;; esac
	shift
done
eval mode=\${FAKE_AFL_MODE_$kind:-\$FAKE_AFL_MODE}
mkdir -p "$out/default/crashes"
stats() { printf 'run_time          : %s\nexecs_done        : %s\n' "$1" "$2" > "$out/default/fuzzer_stats"; }
case "$mode" in
fatal) echo '[-] PROGRAM ABORT : Fork server handshake failed'; exit 1;;
wedged) exit 124;;
nostats) exit 0;;
short) stats 1 40; exit 0;;
noexecs) stats "$secs" 0; exit 0;;
clean) stats "$secs" 40; exit 0;;
crash) stats "$secs" 40; printf 'boom' > "$out/default/crashes/id:000000,sig:06,src:000000"; exit 0;;
esac
echo "stand-in afl: unknown mode $mode"; exit 9
`

// TestSchemaCSNightlyFuzzCampaignVerdict pins how the nightly campaign tells a
// found crash from a broken fuzzer. Before it, every afl-fuzz exit was
// swallowed with `|| true` and the target printed "campaigns complete", so a
// fork-server failure, a seedless run or an early abort was a green night.
// Now the recipe exits 0 CLEAN, 1 FINDING (crashes saved for the replay), 3
// TOOL FAILURE (AFL non-zero or timed out, no fuzzer_stats receipt, short of
// the budget, or no executions), and a tool failure outranks a finding. It
// drives `make schema-cs-nightly-fuzz-campaign` with the stand-in above, so it
// needs make and sh only: no dotnet, no fuzzer, no clock.
func TestSchemaCSNightlyFuzzCampaignVerdict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz campaign verdict (runs make with a stand-in fuzzer)")

	root := csRepoRoot(t)
	for _, c := range []struct {
		name   string
		kinds  string
		modes  map[string]string
		code   string // the recipe's code in make's "Error <n>" line; "" is success
		status map[string]string
		saved  bool
	}{
		{"clean", "load", map[string]string{"FAKE_AFL_MODE": "clean"}, "", map[string]string{"load": "clean"}, false},
		{"finding", "load", map[string]string{"FAKE_AFL_MODE": "crash"}, "1", map[string]string{"load": "finding crashes=1"}, true},
		{"fork server failure", "load", map[string]string{"FAKE_AFL_MODE": "fatal"}, "3", map[string]string{"load": "tool-failure rc=1"}, false},
		{"wedged", "load", map[string]string{"FAKE_AFL_MODE": "wedged"}, "3", map[string]string{"load": "tool-failure rc=124"}, false},
		{"no receipt", "load", map[string]string{"FAKE_AFL_MODE": "nostats"}, "3", map[string]string{"load": "tool-failure rc=0"}, false},
		{"short of the budget", "load", map[string]string{"FAKE_AFL_MODE": "short"}, "3", map[string]string{"load": "tool-failure rc=0"}, false},
		{"no executions", "load", map[string]string{"FAKE_AFL_MODE": "noexecs"}, "3", map[string]string{"load": "tool-failure rc=0"}, false},
		{"tool failure outranks a finding", "load text",
			map[string]string{"FAKE_AFL_MODE_load": "crash", "FAKE_AFL_MODE_text": "fatal"}, "3",
			map[string]string{"load": "finding crashes=1", "text": "tool-failure rc=1"}, true},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			afl := filepath.Join(dir, "afl-fuzz")
			if err := os.WriteFile(afl, []byte(csFuzzStandInAFL), 0o755); err != nil {
				t.Fatal(err)
			}
			out, save := filepath.Join(dir, "afl"), filepath.Join(dir, "save")
			cmd := exec.Command("make", "-s", "--no-print-directory", "schema-cs-nightly-fuzz-campaign",
				"CS_FUZZ_AFL="+afl, "CS_FUZZ_TMIN=false", "CS_FUZZ_TARGET=true", "CS_FUZZ_TIME=5s",
				"CS_FUZZ_KIND="+c.kinds, "CS_FUZZ_OUT="+out, "CS_FUZZ_SEEDS="+filepath.Join(dir, "seeds"),
				"CS_FUZZ_SAVE="+save)
			cmd.Dir = root
			cmd.Env = os.Environ()
			for k, v := range c.modes {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			text, err := cmd.CombinedOutput()
			if c.code == "" {
				if err != nil {
					t.Fatalf("campaign failed, want CLEAN: %v\n%s", err, text)
				}
			} else {
				if err == nil {
					t.Fatalf("campaign passed, want recipe exit %s:\n%s", c.code, text)
				}
				if !strings.Contains(string(text), "] Error "+c.code) {
					t.Errorf("campaign did not exit %s:\n%s", c.code, text)
				}
			}
			for kind, want := range c.status {
				got, err := os.ReadFile(filepath.Join(out, "status-"+kind))
				if err != nil {
					t.Fatalf("status-%s: %v\n%s", kind, err, text)
				}
				if strings.TrimSpace(string(got)) != want {
					t.Errorf("status-%s = %q, want %q\n%s", kind, strings.TrimSpace(string(got)), want, text)
				}
			}
			_, err = os.Stat(filepath.Join(save, "load", "id:000000,sig:06,src:000000"))
			if c.saved && err != nil {
				t.Errorf("the crash was not saved for the replay: %v\n%s", err, text)
			}
			if !c.saved && err == nil {
				t.Errorf("a crash was saved from a run that found none:\n%s", text)
			}
		})
	}
}

// TestSchemaCSNightlyFuzzWorkflowReplaysEveryChange pins the replay's trigger.
// The push and pull_request triggers once carried path filters naming only the
// fuzz harness's own files, so a change to the C# generator, the runtime it
// emits or the schema corpus never ran the replay of the saved crash inputs.
// The workflow must keep both triggers and filter neither.
func TestSchemaCSNightlyFuzzWorkflowReplaysEveryChange(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(csRepoRoot(t), ".github", "workflows", "schema-cs-nightly-fuzz.yml"))
	if err != nil {
		t.Fatal(err)
	}
	inOn, trigger := false, ""
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inOn = trimmed == "on:"
			continue
		}
		if !inOn {
			continue
		}
		if indent == 2 {
			trigger = strings.TrimSuffix(strings.Fields(trimmed)[0], ":")
			seen[trigger] = true
			continue
		}
		if trigger == "push" || trigger == "pull_request" {
			for _, filter := range []string{"paths:", "paths-ignore:", "branches:", "branches-ignore:"} {
				if strings.HasPrefix(trimmed, filter) {
					t.Errorf("the %s trigger carries a %s filter, so a generator, runtime or corpus change can skip the replay", trigger, strings.TrimSuffix(filter, ":"))
				}
			}
		}
	}
	for _, trigger := range []string{"push", "pull_request", "schedule"} {
		if !seen[trigger] {
			t.Errorf("the workflow has no %s trigger", trigger)
		}
	}
	if !strings.Contains(string(data), "make schema-cs-nightly-fuzz-replay") {
		t.Error("the workflow does not run make schema-cs-nightly-fuzz-replay")
	}
}
