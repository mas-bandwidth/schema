package ci

// The lua port's nightly fuzz lane (schema-lua-nightly-fuzz) has three
// invariants that no reader can hold by hand across a make include, a workflow
// and a corpus list:
//
//   - the make targets the lane is run by and the time budget/pin it carries;
//   - the heavy run stays on the schedule and off the push lane, while the
//     replay and the planted control ride the pull request;
//   - the corpus list is the Makefile's own tables_generate corpus (no second
//     corpus), and the harness is a build artifact because this card may not
//     add files to the port's test tree (PATHS-PROPOSED in its report).
//
// This test is milliseconds and rides the existing ci package.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readCI(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// tablesGenerateDirs is the corpus the Makefile's own generate block reads:
// the directory inputs of its tables_generate define block, and nothing else.
func tablesGenerateDirs(t *testing.T, makefile string) []string {
	t.Helper()
	var dirs []string
	in := false
	for line := range strings.SplitSeq(makefile, "\n") {
		switch {
		case strings.HasPrefix(line, "define tables_generate"):
			in = true
		case in && strings.HasPrefix(line, "endef"):
			in = false
		case in:
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			last := fields[len(fields)-1]
			if strings.HasPrefix(last, "tables/") && !strings.Contains(last, ".schema") {
				dirs = append(dirs, last)
			}
		}
	}
	return dirs
}

func TestLuaNightlyFuzzLane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mk := readCI(t, root, "make/schema-lua-nightly-fuzz.mk")
	for _, target := range []string{
		"schema-lua-fuzz-toolchain",
		"schema-lua-nightly-fuzz",
		"schema-lua-fuzz-replay",
		"schema-lua-fuzz-negative-control",
	} {
		if !strings.Contains(mk, target+":") {
			t.Errorf("make/schema-lua-nightly-fuzz.mk has no %s target", target)
		}
	}
	for _, pin := range []string{
		"LUA_FUZZ_SECONDS ?= 600",
		"LUA_FUZZ_HARNESS_REV :=",
		"LUA_VERSION",
		"LUA_FUZZ_CORPUS  := build/lua-fuzz/corpus.txt",
		"LUA_FUZZ_SCRIPT  := build/lua-fuzz/fuzz.lua",
		"define tables_generate",
	} {
		if !strings.Contains(mk, pin) {
			t.Errorf("make/schema-lua-nightly-fuzz.mk does not carry %q", pin)
		}
	}
	// The harness is generated, not committed to the port's test tree, and it
	// carries one entry point per reader kind under the typed-refusal oracle.
	harness := luaFuzzHarness(t, mk)
	for _, want := range []string{"ReadStrict", "LoadText", "FromJson", "TYPED"} {
		if !strings.Contains(harness, want) {
			t.Errorf("the embedded harness does not carry %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "test", "lua-fuzz")); err == nil {
		t.Error("test/lua-fuzz exists; this card may not add files to the port's test tree")
	}

	// THE CORPUS IS THE MAKEFILE'S, not a second one.
	dirs := tablesGenerateDirs(t, readCI(t, root, "Makefile"))
	if len(dirs) == 0 {
		t.Fatal("the Makefile's tables_generate block names no corpus directory")
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] {
			t.Errorf("the Makefile names corpus directory %q twice", dir)
		}
		seen[dir] = true
		if st, err := os.Stat(filepath.Join(root, dir)); err != nil || !st.IsDir() {
			t.Errorf("corpus directory %q does not exist", dir)
		}
	}

	// The workflow: nightly heavy run, fast replay and control on every push
	// and pull request, and the pinned interpreter built rather than assumed.
	wf := readCI(t, root, ".github/workflows/schema-lua-nightly-fuzz.yml")
	for _, want := range []string{
		"cron:", "workflow_dispatch:", "push:", "pull_request:",
		"make dist-lua",
		"make schema-lua-fuzz-replay",
		"make schema-lua-fuzz-negative-control",
		"make schema-lua-nightly-fuzz LUA_FUZZ_SECONDS=600",
		"github.event_name == 'push' || github.event_name == 'pull_request'",
		"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the lua fuzz workflow does not carry %q", want)
		}
	}
	// The heavy run is guarded by the schedule/dispatch condition; the push
	// lane must not invoke it.
	at := strings.Index(wf, "  nightly:")
	if at < 0 {
		t.Fatal("the lua fuzz workflow has no nightly job")
	}
	nightly := wf[at:]
	if !strings.Contains(nightly, "github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'") {
		t.Error("the nightly job is not guarded by schedule/workflow_dispatch")
	}
	fast := wf[strings.Index(wf, "  fast:"):strings.Index(wf, "  nightly:")]
	if strings.Contains(fast, "make schema-lua-nightly-fuzz ") {
		t.Error("the fast lane runs the heavy nightly target; replay only")
	}
}

// luaFuzzHarness is the harness the make include embeds as base64 and decodes
// into build/lua-fuzz/fuzz.lua.
func luaFuzzHarness(t *testing.T, mk string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^LUA_FUZZ_HARNESS_B64 := ([A-Za-z0-9+/=]+)$`).FindStringSubmatch(mk)
	if m == nil {
		t.Fatal("make/schema-lua-nightly-fuzz.mk does not embed the harness")
	}
	harness, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatalf("decode the embedded harness: %v", err)
	}
	return string(harness)
}

// TestLuaNightlyFuzzOracleCarriesEveryRuntimeRefusal holds the harness's
// oracle to the runtime it fuzzes. The oracle accepts a Lua error only when
// its message is a name in the harness's TYPED table; a refusal the runtime
// raises BY NAME that the table does not carry is reported as a raw crash, and
// every input that reaches it becomes a false finding. That happened: the
// strict read gained wire_damaged (and missing_required, blob_shared,
// non_finite, count_refusal) after the harness was written, the planted
// control's fuzz run filled with clean refusals, and the saved case failed
// replay on the clean tree. The names are read from the runtime sources the
// fuzzer's readers reach: refusal.lua's refuse() calls and counters,
// load.lua's strict reasons, and fixed.lua's form-3 errors.
func TestLuaNightlyFuzzOracleCarriesEveryRuntimeRefusal(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	harness := luaFuzzHarness(t, readCI(t, root, "make/schema-lua-nightly-fuzz.mk"))

	start := strings.Index(harness, "local TYPED = {")
	if start < 0 {
		t.Fatal("the embedded harness has no TYPED table")
	}
	end := strings.Index(harness[start:], "\n}")
	if end < 0 {
		t.Fatal("the embedded harness's TYPED table is not closed")
	}
	typed := map[string]bool{}
	for _, m := range regexp.MustCompile(`([a-z_]+) = true`).FindAllStringSubmatch(harness[start:start+end], -1) {
		typed[m[1]] = true
	}

	runtime := "internal/codegen/luatable/runtime/"
	sources := []struct {
		file    string
		pattern string
	}{
		{"refusal.lua", `refuse\("([a-z_]+)"`},
		{"refusal.lua", `name = "([a-z_]+)"`},
		{"load.lua", `then return "([a-z_]+)" end`},
		{"fixed.lua", `error\("([a-z_]+)"`},
		{"text.lua", `error\("([a-z_]+)"`},
	}
	named := 0
	for _, src := range sources {
		text := readCI(t, root, runtime+src.file)
		for _, m := range regexp.MustCompile(src.pattern).FindAllStringSubmatch(text, -1) {
			named++
			if !typed[m[1]] {
				t.Errorf("%s%s refuses by the name %q, which the fuzz harness's TYPED table does not carry: every input reaching it is a false finding", runtime, src.file, m[1])
			}
		}
	}
	if named == 0 {
		t.Fatal("no refusal name read from the runtime; the patterns no longer match it")
	}

	// THE PLANT LANDS. The negative control seds one exact guard line out of
	// the generated load.lua; when the runtime's line changes the control dies
	// at make time with "the plant did not land", so the line is held here.
	plant := regexp.MustCompile(`sed -i 's/\^(  if blen < 9 then)\$\$/`).FindStringSubmatch(readCI(t, root, "make/schema-lua-nightly-fuzz.mk"))
	if plant == nil {
		t.Fatal("the negative control's plant is not the `if blen < 9` guard")
	}
	if !strings.Contains(readCI(t, root, runtime+"load.lua"), "\n"+plant[1]+"\n") {
		t.Errorf("the negative control plants against %q, which load.lua no longer carries", plant[1])
	}
}
