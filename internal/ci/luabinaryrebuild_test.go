package ci

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rebuildEpoch is the fixed test clock: 1000.0 seconds, never the wall clock.
var rebuildEpoch = time.Unix(1000, 0)

// TestLuaBinaryRebuildsWhenRuntimeChanges pins bin/schema's input set:
// the binary embeds internal/codegen/luatable/runtime/*.lua and
// internal/codegen/lua/runtime/*.lua by go:embed, so a change to either
// directory must make make rebuild it. The test copies make/lua.mk into a
// TempDir beside a stand-in for the root Makefile's bin/schema rule whose
// recipe is a touch (so no real build runs),
// and reads make's own verdict with -q and -n at a fixed clock.
//
// The reversed witness drops the table-runtime prerequisite from a second copy
// of the rule: the same touched file must then leave the binary up to date, so
// the signal above cannot come from anything but that prerequisite.
func TestLuaBinaryRebuildsWhenRuntimeChanges(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("this gate reads make's prerequisite graph: %v", err)
	}

	tree := newLuaRebuildTree(t)
	if out, err := runMakeIn(t, tree.dir, "bin/schema"); err != nil {
		t.Fatalf("stub bin/schema: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "bin", "schema")); err != nil {
		t.Fatalf("the stub recipe did not create the binary: %v", err)
	}

	inputs := []struct {
		name string
		rel  string
	}{
		{"the first table runtime file", "internal/codegen/luatable/runtime/descriptor.lua"},
		{"a second table runtime file", "internal/codegen/luatable/runtime/load.lua"},
		{"the type-wire runtime file", "internal/codegen/lua/runtime/serialize.lua"},
	}
	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			tree.setTimes(t, rebuildEpoch.Add(time.Second), rebuildEpoch)
			if !makeSaysUpToDate(t, tree.dir) {
				t.Fatalf("bin/schema is out of date before any input moves")
			}
			out, err := runMakeIn(t, tree.dir, "-n", "bin/schema")
			if err != nil {
				t.Fatalf("make -n with nothing touched: %v\n%s", err, out)
			}
			if strings.Contains(out, "touch bin/schema") {
				t.Fatalf("make -n ran the recipe while the binary was up to date:\n%s", out)
			}

			tree.setTimes(t, rebuildEpoch.Add(time.Second), rebuildEpoch)
			tree.touch(t, input.rel, rebuildEpoch.Add(time.Hour))
			if makeSaysUpToDate(t, tree.dir) {
				t.Errorf("touching %s left bin/schema up to date: the rule does not list the embedded input", input.rel)
			}
			out, err = runMakeIn(t, tree.dir, "-n", "bin/schema")
			if err != nil {
				t.Fatalf("make -n after touching %s: %v\n%s", input.rel, err, out)
			}
			if !strings.Contains(out, "touch bin/schema") {
				t.Errorf("make -n after touching %s shows no build recipe:\n%s", input.rel, out)
			}
		})
	}

	t.Run("reversed witness", func(t *testing.T) {
		witness := newLuaRebuildTree(t)
		if out, err := runMakeIn(t, witness.dir, "bin/schema"); err != nil {
			t.Fatalf("stub bin/schema: %v\n%s", err, out)
		}
		makefile := filepath.Join(witness.dir, "Makefile")
		data, err := os.ReadFile(makefile)
		if err != nil {
			t.Fatal(err)
		}
		drop := " $(wildcard internal/codegen/luatable/runtime/*.lua)"
		if !strings.Contains(string(data), drop) {
			t.Fatalf("the copied rule does not list %s; the witness has nothing to drop", strings.TrimSpace(drop))
		}
		if err := os.WriteFile(makefile, []byte(strings.Replace(string(data), drop, "", 1)), 0o644); err != nil {
			t.Fatal(err)
		}

		witness.setTimes(t, rebuildEpoch.Add(time.Second), rebuildEpoch)
		if !makeSaysUpToDate(t, witness.dir) {
			t.Fatalf("the witness binary is out of date before any input moves")
		}
		witness.touch(t, "internal/codegen/luatable/runtime/descriptor.lua", rebuildEpoch.Add(time.Hour))
		if !makeSaysUpToDate(t, witness.dir) {
			t.Fatalf("the witness rebuilt with the table-runtime prerequisite dropped: the test is not reading that prerequisite")
		}
		out, err := runMakeIn(t, witness.dir, "-n", "bin/schema")
		if err != nil {
			t.Fatalf("witness make -n: %v\n%s", err, out)
		}
		if strings.Contains(out, "touch bin/schema") {
			t.Fatalf("the witness ran the recipe after its prerequisite was dropped:\n%s", out)
		}
	})
}

// luaRebuildTree is a TempDir holding the copied lua.mk rule, its Go sources,
// and every embedded runtime file the test moves in time.
type luaRebuildTree struct {
	dir     string
	runtime []string
}

func newLuaRebuildTree(t *testing.T) *luaRebuildTree {
	t.Helper()
	root := repoRoot(t)
	source, err := os.ReadFile(filepath.Join(root, "make", "lua.mk"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	makefile := luaRebuildMakefile(t, string(source))
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatal(err)
	}

	goSources := []string{"go.mod", "cmd/schema/main.go"}
	assets := []string{
		"internal/codegen/luatable/runtime/descriptor.lua",
		"internal/codegen/luatable/runtime/load.lua",
		"internal/codegen/lua/runtime/serialize.lua",
	}
	for _, rel := range append(goSources, assets...) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "-- the embedded runtime under test\n"
		if strings.HasSuffix(rel, ".go") {
			body = "package main\n\nfunc main() {}\n"
		} else if rel == "go.mod" {
			body = "module rebuildfixture\n\ngo 1.26\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &luaRebuildTree{dir: dir, runtime: assets}
}

// luaRebuildMakefile is the copied make/lua.mk with the root Makefile's
// bin/schema rule stood in by a touch of the target: the test measures
// prerequisites, never a Go build. lua.mk adds the embedded runtime directories
// to bin/schema's prerequisites with a recipe-less rule; GO_SOURCES and the
// recipe are fixture definitions because the root Makefile normally owns them.
func luaRebuildMakefile(t *testing.T, source string) string {
	t.Helper()
	n := 0
	for line := range strings.SplitSeq(source, "\n") {
		if strings.HasPrefix(line, "bin/schema:") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("make/lua.mk declares bin/schema's prerequisites %d times, want once", n)
	}
	return "GO_SOURCES := go.mod cmd/schema/main.go\n" +
		"bin/schema: $(GO_SOURCES)\n\t@mkdir -p bin\n\t@touch $@\n" + source
}

// setTimes puts the target one second after every prerequisite, so make -q
// reports up to date without consulting the wall clock.
func (tree *luaRebuildTree) setTimes(t *testing.T, target, prereq time.Time) {
	t.Helper()
	for _, rel := range append([]string{"go.mod", "cmd/schema/main.go"}, tree.runtime...) {
		tree.touch(t, rel, prereq)
	}
	tree.touch(t, filepath.Join("bin", "schema"), target)
}

func (tree *luaRebuildTree) touch(t *testing.T, rel string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(filepath.Join(tree.dir, rel), when, when); err != nil {
		t.Fatal(err)
	}
}

// runMakeIn runs make in dir with --no-print-directory and returns its output.
func runMakeIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("make", append([]string{"--no-print-directory"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// makeSaysUpToDate reads make -q's exit status: 0 is up to date, 1 is out of
// date, anything else is an error the test must not mistake for a verdict.
func makeSaysUpToDate(t *testing.T, dir string) bool {
	t.Helper()
	out, err := runMakeIn(t, dir, "-q", "bin/schema")
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false
	}
	t.Fatalf("make -q bin/schema: %v\n%s", err, out)
	return false
}
