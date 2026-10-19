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
	for _, line := range strings.Split(makefile, "\n") {
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
	m := regexp.MustCompile(`(?m)^LUA_FUZZ_HARNESS_B64 := ([A-Za-z0-9+/=]+)$`).FindStringSubmatch(mk)
	if m == nil {
		t.Fatal("make/schema-lua-nightly-fuzz.mk does not embed the harness")
	}
	harness, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatalf("decode the embedded harness: %v", err)
	}
	for _, want := range []string{"ReadStrict", "LoadText", "FromJson", "TYPED"} {
		if !strings.Contains(string(harness), want) {
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
}
