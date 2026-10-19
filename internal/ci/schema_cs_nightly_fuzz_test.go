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
// The workflow must keep both triggers and filter neither: no paths, branches,
// tags or types filter, any of which can leave a reader change unreplayed.
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
			for _, filter := range []string{"paths:", "paths-ignore:", "branches:", "branches-ignore:", "tags:", "tags-ignore:", "types:"} {
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
	// A finding's minimised crashes live in the runner's workspace, which the
	// next checkout cleans: the fuzz job must upload them on failure.
	if !strings.Contains(string(data), "uses: actions/upload-artifact@") || !strings.Contains(string(data), "testdata/schema-cs-nightly-fuzz/corpus/${{ matrix.kind }}/") {
		t.Error("the fuzz job does not upload the saved crashes (testdata/schema-cs-nightly-fuzz/corpus/<kind>/) as an artifact")
	}
}

// csFuzzStandInDotnet stands in for `dotnet exec csc.dll ...`: like csc it
// refuses a source path that does not exist (CS2001), and it records every
// source argument, one per line, so the test sees how the recipe split them.
const csFuzzStandInDotnet = `#!/bin/sh
[ "$1" = exec ] || { echo "stand-in dotnet: want exec, got $1"; exit 2; }
shift 2
for a in "$@"; do
	case "$a" in -*) continue;; esac
	if [ ! -f "$a" ]; then echo "error CS2001: Source file '$a' could not be found."; exit 1; fi
	echo "$a" >> "$FAKE_DOTNET_LOG"
done
`

// TestSchemaCSNightlyFuzzCompileRuntime pins how the build hands the
// serialize.cs runtime to csc. The runtime is two files (Serialize.cs and
// Int128Pair.cs); the recipe once quoted them as one word, so on every bench
// with the sibling ../serialize.cs checkout csc looked for one file named by
// both paths and refused the build (CS2001), while a bench without the sibling
// compiled the shim and stayed green. It drives `make
// schema-cs-nightly-fuzz-compile` with a stand-in dotnet that refuses a missing
// source as csc does, both with the runtime present and without it, so it
// needs make and sh only: no dotnet, no clock.
func TestSchemaCSNightlyFuzzCompileRuntime(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz build's runtime arguments (runs make with a stand-in dotnet)")

	root := csRepoRoot(t)
	for _, c := range []struct {
		name    string
		sibling bool
		want    []string // sources csc must receive, as separate arguments, relative to the temp dir
	}{
		{"sibling runtime", true, []string{"serialize.cs/src/Serialize.cs", "serialize.cs/src/Int128Pair.cs"}},
		{"no sibling: the shim", false, []string{"build/harness/Runtime.cs"}},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, f := range []string{
				"sdk/10.0.100/Roslyn/bincore/csc.dll",
				"packs/Microsoft.NETCore.App.Ref/10.0.0/ref/net10.0/System.Runtime.dll",
				"build/readers/unit/UnitTable.cs",
				"build/harness/Program.cs",
			} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if c.sibling {
				for _, f := range []string{"Serialize.cs", "Int128Pair.cs"} {
					if err := os.MkdirAll(filepath.Join(dir, "serialize.cs", "src"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "serialize.cs", "src", f), nil, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			dotnet := filepath.Join(dir, "dotnet")
			if err := os.WriteFile(dotnet, []byte(csFuzzStandInDotnet), 0o755); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(dir, "sources.log")
			cmd := exec.Command("make", "-s", "--no-print-directory", "schema-cs-nightly-fuzz-compile",
				"DOTNET="+dotnet, "CS_FUZZ_BUILD="+filepath.Join(dir, "build"),
				"CS_FUZZ_RUNTIME_DIR="+filepath.Join(dir, "serialize.cs", "src"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "FAKE_DOTNET_LOG="+logPath)
			text, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("compile failed: %v\n%s", err, text)
			}
			got, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			sources := map[string]bool{}
			for _, line := range strings.Split(strings.TrimSpace(string(got)), "\n") {
				sources[line] = true
			}
			for _, w := range c.want {
				if !sources[filepath.Join(dir, w)] {
					t.Errorf("csc did not receive %s as a source argument; got:\n%s", w, got)
				}
			}
			if !sources[filepath.Join(dir, "build", "harness", "Seeder.cs")] {
				t.Errorf("the harness was built without the seeder (build/harness/Seeder.cs); got:\n%s", got)
			}
		})
	}
}

// TestSchemaCSNightlyFuzzSeedCoverage pins the seed-coverage gate. The sibling
// elixir fuzz's seeds never filled a pointer field, so its planted reader bugs
// there survived every campaign. The C# seeder writes one census line per
// root; the gate must refuse a root with no line, a form that saved no seed,
// and a wire seed that lost a shape (table reference, list, map) the root's
// fill set. It drives `make schema-cs-nightly-fuzz-seed-coverage` on stand-in
// files, so it needs make and awk only.
func TestSchemaCSNightlyFuzzSeedCoverage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz seed coverage gate (runs make on stand-in files)")

	root := csRepoRoot(t)
	const roots = "readers/a\tNs\tAlbum\nreaders/a\tNs\tFleet\n"
	const album = "Ns.Album\twire full pointer=1/1 list=1/1 map=0/0\ttext full pointer=1/1 list=1/1 map=0/0\n"
	const fleet = "Ns.Fleet\twire structure pointer=2/2 list=1/1 map=2/2\ttext full pointer=2/2 list=1/1 map=2/2\n"
	for _, c := range []struct {
		name     string
		coverage string
		fail     string // a line the refusal must print; "" is a pass
	}{
		{"every root, every shape", album + fleet, ""},
		{"a root without a seed", album, "SEED COVERAGE: Ns.Fleet: no seed"},
		{"a wire seed without its map", album + strings.Replace(fleet, "wire structure pointer=2/2 list=1/1 map=2/2", "wire structure pointer=2/2 list=1/1 map=0/2", 1), "the wire seed carries no map field"},
		{"a wire seed without its pointer", strings.Replace(album, "wire full pointer=1/1", "wire full pointer=0/1", 1) + fleet, "the wire seed carries no pointer field"},
		{"no wire seed at all", strings.Replace(album, "wire full pointer=1/1 list=1/1 map=0/0", "wire none full:save-refused", 1) + fleet, "Ns.Album: a form saved no seed"},
		{"a census that failed", strings.Replace(album, "map=0/0\ttext", "map=0/0 fallback=census:NullReferenceException\ttext", 1) + fleet, "the full fill's census failed"},
		{"no text seed at all", album + strings.Replace(fleet, "text full pointer=2/2 list=1/1 map=2/2", "text none full:reload-refused", 1), "Ns.Fleet: a form saved no seed"},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			build, seeds := filepath.Join(dir, "build"), filepath.Join(dir, "seeds")
			for _, d := range []string{build, seeds} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(build, "roots.txt"), []byte(roots), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(seeds, "coverage.txt"), []byte(c.coverage), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("make", "-s", "--no-print-directory", "schema-cs-nightly-fuzz-seed-coverage",
				"CS_FUZZ_BUILD="+build, "CS_FUZZ_SEEDS="+seeds)
			cmd.Dir = root
			text, err := cmd.CombinedOutput()
			if c.fail == "" {
				if err != nil {
					t.Fatalf("seed coverage refused a complete census: %v\n%s", err, text)
				}
				return
			}
			if err == nil {
				t.Fatalf("seed coverage passed, want a refusal naming %q:\n%s", c.fail, text)
			}
			if !strings.Contains(string(text), c.fail) {
				t.Errorf("seed coverage refused without naming %q:\n%s", c.fail, text)
			}
		})
	}
}

// TestSchemaCSNightlyFuzzWorkflowFleetPinned pins where and with what the
// workflow runs: the fleet's self-hosted runners only (Glenn 2026-10-18: all CI
// on the fleet), every action at a full commit SHA, and the .NET SDK the
// repository pins in .github/dotnet-version, checked on the runner.
func TestSchemaCSNightlyFuzzWorkflowFleetPinned(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(csRepoRoot(t), ".github", "workflows", "schema-cs-nightly-fuzz.yml"))
	if err != nil {
		t.Fatal(err)
	}
	runsOn := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "runs-on:") {
			runsOn++
			if !strings.Contains(trimmed, "self-hosted") || !strings.Contains(trimmed, "fleet") {
				t.Errorf("a job runs off the fleet: %q", trimmed)
			}
		}
		if strings.Contains(trimmed, "github-hosted") || strings.Contains(trimmed, "ubuntu-") {
			t.Errorf("the workflow names a GitHub-hosted runner: %q", trimmed)
		}
		if i := strings.Index(trimmed, "uses: "); i >= 0 {
			ref := strings.Fields(trimmed[i+len("uses: "):])[0]
			at := strings.LastIndex(ref, "@")
			sha := ""
			if at >= 0 {
				sha = ref[at+1:]
			}
			if len(sha) != 40 || strings.Trim(sha, "0123456789abcdef") != "" {
				t.Errorf("action %q is not pinned to a commit", ref)
			}
		}
	}
	if runsOn == 0 {
		t.Error("the workflow has no runs-on")
	}
	if !strings.Contains(string(data), "cat .github/dotnet-version") || !strings.Contains(string(data), "dotnet --list-sdks") {
		t.Error("the workflow does not check the runner's .NET SDK against .github/dotnet-version")
	}
}
