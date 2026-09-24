// THE SCAFFOLDING VERB, proven the way a porter meets it (nova-tools#2498 S5).
//
// `schema new-leg <lang>` lays down a language leg's skeleton — the target
// file, the backend package, its fixture test, the leg's make file and one
// fixture unit — so a port starts from a tree that already builds and already
// tests, and the model spends its tokens on the emitter rather than on finding
// the nine registries (docs/CONTRIBUTING.md, "Adding a language").
//
// The DONE-WHEN is a sentence a test can fail: `schema new-leg lua` yields a
// building, testing skeleton. So the test copies the compiler's Go sources and
// the make registry into a scratch tree, runs the verb there, and then asks the
// tree the questions a porter would: does it build, does the new package's test
// pass, does `make registry` discover the leg, does `make test-lua` run green,
// and does `generate --lang lua` emit the fixture. It also holds the verb to
// the repo's write discipline: a second run refuses rather than overwrite, and
// a name that is already a leg is refused by name.
//
// It builds the whole compiler in a fresh tree and runs make there — a minute
// and more of the Go toolchain — so it sits behind slowtest.Gate: the default
// `go test` (ci.yml's two-minute go-test job) passes over it, and `make test`,
// which exports SCHEMA_SLOW=1, runs it whole.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestNewLegLuaYieldsABuildingTestingSkeleton(t *testing.T) {
	slowtest.Gate(t, "go build and make over a copied tree")
	for _, tool := range []string{"make", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on PATH", tool)
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := buildCLI(t)
	tree := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum", "Makefile"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			copyScaffoldFile(t, filepath.Join(root, f), filepath.Join(tree, f))
		}
	}
	// the compiler's Go sources, tests left behind: the question is whether
	// the skeleton builds beside them, not whether their suites pass in a copy
	for _, dir := range []string{"cmd", "compiler", "internal", "ir"} {
		copyScaffoldTree(t, filepath.Join(root, dir), filepath.Join(tree, dir), func(rel string) bool {
			return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
		})
	}
	copyScaffoldTree(t, filepath.Join(root, "tools", "newleg"), filepath.Join(tree, "tools", "newleg"),
		func(rel string) bool { return !strings.HasSuffix(rel, "_test.go") })
	copyScaffoldTree(t, filepath.Join(root, "make"), filepath.Join(tree, "make"), func(string) bool { return true })

	if strings.Contains(runIn(t, tree, "make", "-s", "registry"), "test-lua") {
		t.Fatal("make registry names test-lua before the verb ran — the discovery would not be doing the work")
	}

	out := run(t, bin, "new-leg", "--root", tree, "lua")
	for _, want := range []string{
		"compiler/target_lua.go",
		"internal/codegen/lua/lua.go",
		"internal/codegen/lua/lua_test.go",
		"make/lua.mk",
		"test/lua/Fixture.schema",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("new-leg did not report writing %s:\n%s", want, out)
		}
		if _, err := os.Stat(filepath.Join(tree, filepath.FromSlash(want))); err != nil {
			t.Errorf("new-leg did not write %s: %v", want, err)
		}
	}

	// BUILDING: the whole copied tree, the new target included, and vet clean
	runIn(t, tree, "go", "build", "./...")
	runIn(t, tree, "go", "vet", "./internal/codegen/lua/", "./compiler/")
	// TESTING: the fixture test the skeleton carries passes as laid down
	if got := runIn(t, tree, "go", "test", "-count=1", "./internal/codegen/lua/"); !strings.Contains(got, "ok") {
		t.Errorf("go test of the skeleton did not pass:\n%s", got)
	}
	// the make registry discovers the leg with no shared file edited, and the
	// leg's own target runs green: build, its test, and a generate over the fixture
	if got := runIn(t, tree, "make", "-s", "registry"); !strings.Contains(got, "test-lua") {
		t.Errorf("make registry did not discover the planted leg:\n%s", got)
	}
	runIn(t, tree, "make", "test-lua")
	if _, err := os.Stat(filepath.Join(tree, "build", "lua-skeleton", "Fixture.lua")); err != nil {
		t.Errorf("make test-lua did not generate the fixture: %v", err)
	}

	// the write discipline: never overwrite, never shadow a leg that exists —
	// the planted lua is now a registered target, so the second run is refused
	// by the driver's own name rule before any file is looked at
	if got, err := exec.Command(bin, "new-leg", "--root", tree, "lua").CombinedOutput(); err == nil ||
		!strings.Contains(string(got), "already") {
		t.Errorf("a second new-leg lua did not refuse by name (err %v):\n%s", err, got)
	}
	if got, err := exec.Command(bin, "new-leg", "--root", tree, "go").CombinedOutput(); err == nil ||
		!strings.Contains(string(got), "already") {
		t.Errorf("new-leg go (an existing leg) was not refused (err %v):\n%s", err, got)
	}
	if got, err := exec.Command(bin, "new-leg", "--root", tree, "Lua-5").CombinedOutput(); err == nil ||
		!strings.Contains(string(got), "leg name") {
		t.Errorf("new-leg with a name that is no Go package name was not refused (err %v):\n%s", err, got)
	}
}

// runIn runs a command in dir and fails the test on a nonzero exit, showing
// the output the porter would have seen.
func runIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s in %s: %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func copyScaffoldTree(t *testing.T, from, to string, keep func(rel string) bool) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !keep(filepath.ToSlash(rel)) {
			return nil
		}
		copyScaffoldFile(t, p, filepath.Join(to, rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyScaffoldFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}
