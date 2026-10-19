package ci

// The lua port's nightly fuzz lane (schema-lua-nightly-fuzz) has three
// invariants that no reader can hold by hand across a make include, a workflow
// and a corpus list:
//
//   - the make targets the lane is run by and the time budget/pin it carries;
//   - the heavy run stays on the schedule and off the push lane, while the
//     replay and the planted control ride the pull request;
//   - the corpus list is the Makefile's own tables_generate corpus (no second
//     corpus), and the planted control is registered in
//     make/negative-controls.json.
//
// This test is milliseconds and rides the existing ci package.

import (
	"os"
	"path/filepath"
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
	if !strings.Contains(mk, "LUA_FUZZ_SECONDS ?= 600") {
		t.Error("the nightly time budget is not pinned at 600 seconds")
	}
	if !strings.Contains(mk, "LUA_FUZZ_HARNESS_REV :=") {
		t.Error("the fuzz harness revision is not pinned")
	}
	if !strings.Contains(mk, "LUA_VERSION") {
		t.Error("the make include does not name the interpreter pin LUA_VERSION")
	}

	corpus := readCI(t, root, "test/lua-fuzz/corpus.txt")
	seen := map[string]bool{}
	units := 0
	for _, line := range strings.Split(corpus, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Errorf("corpus line %q is not <unit> <schema directory>", line)
			continue
		}
		key, dir := fields[0], fields[1]
		if seen[key] {
			t.Errorf("corpus unit %q is listed twice", key)
		}
		seen[key] = true
		if !strings.HasPrefix(dir, "tables/") && !strings.HasPrefix(dir, "test/tables/") {
			t.Errorf("corpus directory %q is not one of the Makefile's generate corpus roots", dir)
		}
		if st, err := os.Stat(filepath.Join(root, dir)); err != nil || !st.IsDir() {
			t.Errorf("corpus directory %q does not exist", dir)
		}
		units++
	}
	if units == 0 {
		t.Error("the lua fuzz corpus list is empty")
	}

	// The workflow: nightly heavy run, fast replay and control on the pull
	// request, and no push trigger.
	wf := readCI(t, root, ".github/workflows/schema-lua-nightly-fuzz.yml")
	for _, want := range []string{
		"cron:", "workflow_dispatch:", "pull_request:",
		"make schema-lua-fuzz-replay",
		"make schema-lua-fuzz-negative-control",
		"make schema-lua-nightly-fuzz LUA_FUZZ_SECONDS=600",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the lua fuzz workflow does not carry %q", want)
		}
	}
	if strings.Contains(wf, "\n  push:") {
		t.Error("the lua fuzz workflow triggers on push; the heavy part is nightly")
	}

	// The planted control is registered in the repository's control ledger.
	controls := readCI(t, root, "make/negative-controls.json")
	if !strings.Contains(controls, `"schema-lua-fuzz-negative-control"`) {
		t.Error("schema-lua-fuzz-negative-control is not registered in make/negative-controls.json")
	}

	// The saved replay corpus is real: the planted control's input is
	// committed, so the fast lane is not replaying an empty directory.
	entries, err := os.ReadDir(filepath.Join(root, "test/lua-fuzz/corpus"))
	if err != nil {
		t.Fatalf("read the replay corpus: %v", err)
	}
	cases := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".case") {
			cases++
		}
	}
	if cases == 0 {
		t.Error("test/lua-fuzz/corpus holds no .case replay input")
	}
}
